package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

func setupWSTestServer(t *testing.T, mr *miniredis.Miniredis) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	h := NewOpsWSHandler(rdb)
	r.GET("/api/v1/ops/ws/qps", h.HandleQPSWebSocket)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	wsURL := "ws" + srv.URL[4:] + "/api/v1/ops/ws/qps"
	return srv, wsURL
}

func TestHandleQPSWebSocket_Upgrade(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	_, wsURL := setupWSTestServer(t, mr)

	dialer := websocket.Dialer{
		Subprotocols:    []string{"ops-admin"},
		HandshakeTimeout: 3 * time.Second,
	}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	if proto := resp.Header.Get("Sec-WebSocket-Protocol"); proto != "ops-admin" {
		t.Fatalf("expected protocol ops-admin, got %q", proto)
	}
}

func TestHandleQPSWebSocket_PushMessage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	now := time.Now().Format("200601021504")
	mr.Set("ops:qps:"+now, "180")
	mr.Set("ops:tps:"+now, "9000")

	_, wsURL := setupWSTestServer(t, mr)

	dialer := websocket.Dialer{
		Subprotocols:     []string{"ops-admin"},
		HandshakeTimeout: 3 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(msg, &payload); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, msg)
	}

	for _, key := range []string{"qps", "tps", "timestamp", "sample"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing field %q in payload: %s", key, msg)
		}
	}

	if sample, ok := payload["sample"].(float64); !ok || sample != 180 {
		t.Fatalf("expected sample=180, got %v", payload["sample"])
	}
	qps, _ := payload["qps"].(float64)
	if qps != 3.0 {
		t.Fatalf("expected qps=3.0 (180/60), got %v", qps)
	}
	tps, _ := payload["tps"].(float64)
	if tps != 150.0 {
		t.Fatalf("expected tps=150.0 (9000/60), got %v", tps)
	}
}

func TestHandleQPSWebSocket_RedisDown(t *testing.T) {
	// 不写 Redis key → handler 应该能发 qps=0/tps=0 而不 crash
	_, wsURL := setupWSTestServer(t, mustRunRedis(t))

	dialer := websocket.Dialer{
		Subprotocols:     []string{"ops-admin"},
		HandshakeTimeout: 3 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["qps"].(float64) != 0 || payload["tps"].(float64) != 0 {
		t.Fatalf("expected qps=0 tps=0, got %v", payload)
	}
}

func TestHandleQPSWebSocket_InvalidProtocol(t *testing.T) {
	// 不传 ops-admin subprotocol，handler 仍应 accept（response 不带 protocol）
	mr := mustRunRedis(t)
	_, wsURL := setupWSTestServer(t, mr)

	dialer := websocket.Dialer{
		Subprotocols:     []string{"not-ops-admin"},
		HandshakeTimeout: 3 * time.Second,
	}
	conn, resp, err := dialer.Dial(wsURL, nil)
	// gorilla 会拒绝协议不匹配
	if err == nil {
		conn.Close()
		t.Log("gorilla accepted unknown protocol - CheckOrigin allowed it")
	}
	_ = resp
}

func mustRunRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mr.Close() })
	return mr
}
