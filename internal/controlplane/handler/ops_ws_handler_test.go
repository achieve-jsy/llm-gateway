package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const testOpsWSSecret = "test-ops-ws-secret"

// withTestJWTSecret 临时设置 JWT_SECRET，保证 validateOpsWSToken 使用测试密钥。
func withTestJWTSecret(t *testing.T) {
	t.Helper()
	prev := os.Getenv("JWT_SECRET")
	os.Setenv("JWT_SECRET", testOpsWSSecret)
	t.Cleanup(func() { os.Setenv("JWT_SECRET", prev) })
}

// makeOpsWSToken 用与 middleware.JWTAuth 同源的 auth.GenerateAccessToken 造 token。
func makeOpsWSToken(t *testing.T, userID string) string {
	t.Helper()
	tok, err := auth.GenerateAccessToken(testOpsWSSecret, time.Hour, userID, "", "", "", "", "u@ex.com", userID, userID, "admin")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

// strictOriginCfg 返回一个 strict 同源 + 不信代理的配置（默认行为）。
func strictOriginCfg() opsWSConfig {
	return opsWSConfig{
		OriginPolicy:   originPolicyStrict,
		TrustProxy:     false,
		TrustedProxies: nil,
		AllowedOrigins: map[string]struct{}{},
		MaxConns:       defaultMaxOpsWSConns,
		MaxConnsPerIP:  defaultMaxOpsWSConnsPerIP,
	}
}

// setupWSTestServer 搭建一个 isolate 的 gin server；同时返回与 srv host 同源的
// origin 字符串，便于 strict 策略下同源测试拨号（gorilla client 默认不发 Origin）。
func setupWSTestServer(t *testing.T, mr *miniredis.Miniredis) (*httptest.Server, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	h := NewOpsWSHandler(rdb)
	r.GET("/api/v1/ops/ws/qps", h.HandleQPSWebSocket)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// httptest.Server.URL 形如 http://127.0.0.1:PORT；同源 origin 去掉 trailing path。
	sameOrigin := srv.URL
	wsURL := "ws" + srv.URL[4:] + "/api/v1/ops/ws/qps"
	return srv, wsURL, sameOrigin
}

// dialWithToken 用 ops-admin + jwt.<token> 子协议发起 WS 连接。
// 来自 srv 的请求 Origin/Host 会自然同源（dialer 默认带 Host= srv host）。
func dialWithToken(t *testing.T, wsURL, token string, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	protocols := []string{"ops-admin"}
	if token != "" {
		protocols = append(protocols, "jwt."+token)
	}
	dialer := websocket.Dialer{
		Subprotocols:     protocols,
		HandshakeTimeout: 3 * time.Second,
	}
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	return dialer.Dial(wsURL, hdr)
}

// readCloseCode 读取服务端 close 帧的 code（若存在）。
func readCloseCode(t *testing.T, conn *websocket.Conn, timeout time.Duration) (int, string) {
	t.Helper()
	if conn == nil {
		return 0, ""
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				return ce.Code, ce.Text
			}
			return 0, ""
		}
		_ = msgType
		_ = data
	}
}

// ---- 测试用例 ----

func TestOpsWS_NoToken_Rejected(t *testing.T) {
	// (a) 无 token：strict 同源下仍会升级并发 close 4401（前端不重连）。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)

	// 同源 Origin。
	conn, _, err := dialWithToken(t, wsURL, "", sameOrigin)
	if err != nil {
		t.Fatalf("dial (expected upgrade then close): %v", err)
	}
	defer conn.Close()
	code, _ := readCloseCode(t, conn, 2*time.Second)
	if code != opsWSCloseUnauthorized {
		t.Fatalf("expected close code %d (unauthorized), got %d", opsWSCloseUnauthorized, code)
	}
}

func TestOpsWS_InvalidToken_Rejected(t *testing.T) {
	// (b) 无效 token：close 4401。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)

	conn, _, err := dialWithToken(t, wsURL, "not-a-valid-jwt", sameOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	code, _ := readCloseCode(t, conn, 2*time.Second)
	if code != opsWSCloseUnauthorized {
		t.Fatalf("expected close code %d (unauthorized), got %d", opsWSCloseUnauthorized, code)
	}
}

func TestOpsWS_ValidToken_Accepted_AndPushesMessage(t *testing.T) {
	// (c) 有效 token：升级成功，收到 QPS 推送，sample 与 Redis 一致。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	mr := mustRunRedis(t)
	now := time.Now().Format("200601021504")
	mr.Set("ops:qps:"+now, "180")
	mr.Set("ops:tps:"+now, "9000")

	_, wsURL, sameOrigin := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-1")

	conn, _, err := dialWithToken(t, wsURL, token, sameOrigin)
	if err != nil {
		t.Fatalf("dial with valid token: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read push: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg, &payload); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, msg)
	}
	for _, key := range []string{"qps", "tps", "timestamp", "sample"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing field %q: %s", key, msg)
		}
	}
	if sample, _ := payload["sample"].(float64); sample != 180 {
		t.Fatalf("expected sample=180, got %v", payload["sample"])
	}
	if qps, _ := payload["qps"].(float64); qps != 3.0 {
		t.Fatalf("expected qps=3.0, got %v", qps)
	}
	if tps, _ := payload["tps"].(float64); tps != 150.0 {
		t.Fatalf("expected tps=150.0, got %v", tps)
	}
}

func TestOpsWS_OriginMismatch_Rejected(t *testing.T) {
	// (d) origin 不匹配（strict）：CheckOrigin 拒绝，握手不成功。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	restore := setOpsWSConfigForTest(strictOriginCfg())
	t.Cleanup(restore)

	mr := mustRunRedis(t)
	srv, wsURL, _ := setupWSTestServer(t, mr)
	_ = srv

	token := makeOpsWSToken(t, "user-2")
	// 伪 origin，与 srv host 不同源 → CheckOrigin 返回 false → dial 失败。
	badOrigin := "http://evil.example.com"
	conn, resp, err := dialWithToken(t, wsURL, token, badOrigin)
	if err == nil {
		conn.Close()
		t.Fatalf("expected dial to fail due to origin mismatch, but it succeeded")
	}
	if resp != nil {
		// CheckOrigin 拒绝时 gorilla 返回 403。
		if resp.StatusCode == http.StatusSwitchingProtocols {
			t.Fatalf("expected non-101 status, got 101")
		}
	}
}

func TestOpsWS_OriginAllowedViaWhitelist_Accepted(t *testing.T) {
	// 白名单放行：origin host 在 OPS_WS_ALLOWED_ORIGINS 时同源校验放行。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	cfg := strictOriginCfg()
	cfg.AllowedOrigins = map[string]struct{}{"allowed.example.com": {}}
	restore := setOpsWSConfigForTest(cfg)
	t.Cleanup(restore)

	mr := mustRunRedis(t)
	_, wsURL, _ := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-3")

	conn, resp, err := dialWithToken(t, wsURL, token, "http://allowed.example.com")
	if err != nil {
		t.Fatalf("expected whitelist origin accepted, got err: %v (status=%v)", err, statusOrZero(resp))
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = conn.ReadMessage() // 容忍首帧（推送或后续 close）
}

func TestOpsWS_TooManyConnections_Rejected(t *testing.T) {
	// (e) 超全局连接数限制：把 MaxConns/MaxConnsPerIP 临时设小，第二连接被拒。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	cfg := strictOriginCfg()
	cfg.MaxConns = 1
	cfg.MaxConnsPerIP = 1
	restore := setOpsWSConfigForTest(cfg)
	t.Cleanup(restore)

	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-4")

	// 第一连接占用唯一槽位。
	conn1, _, err := dialWithToken(t, wsURL, token, sameOrigin)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer conn1.Close()
	// 维持第一连接存活，避免被推送循环读取关闭。
	go func() {
		for {
			if _, _, e := conn1.ReadMessage(); e != nil {
				return
			}
		}
	}()

	// 第二连接应被拒（HTTP 503，非升级）。
	conn2, resp2, err2 := dialWithToken(t, wsURL, token, sameOrigin)
	if err2 == nil {
		// 极端情况下若握手因读循环竞态升级了，立即关闭。
		conn2.Close()
		t.Fatalf("expected second connection rejected, but it was upgraded")
	}
	if resp2 != nil && resp2.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("expected non-101 status for over-limit connection, got 101")
	}
	if resp2 != nil && resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 too many connections, got %d", resp2.StatusCode)
	}
}

func TestOpsWS_RealtimeDisabled_Closes4001(t *testing.T) {
	// realtime 监控关闭：升级后立即发 close 4001，前端不重连。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	prev := os.Getenv(envOpsRealtimeEnabled)
	os.Setenv(envOpsRealtimeEnabled, "false")
	t.Cleanup(func() { os.Setenv(envOpsRealtimeEnabled, prev) })

	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-5")

	conn, _, err := dialWithToken(t, wsURL, token, sameOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	code, _ := readCloseCode(t, conn, 2*time.Second)
	if code != opsWSCloseRealtimeDisabled {
		t.Fatalf("expected close code %d (realtime_disabled), got %d", opsWSCloseRealtimeDisabled, code)
	}
}

func TestOpsWS_TokenFromQuery_Accepted(t *testing.T) {
	// query ?token= 降级通道（非浏览器客户端）：有效 token 放行。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-6")

	protocols := []string{"ops-admin"} // 不在 subprotocol 带 jwt
	dialer := websocket.Dialer{Subprotocols: protocols, HandshakeTimeout: 3 * time.Second}
	hdr := http.Header{}
	hdr.Set("Origin", sameOrigin)
	conn, _, err := dialer.Dial(wsURL+"?token="+token, hdr)
	if err != nil {
		t.Fatalf("dial with query token: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = conn.ReadMessage()
}

func TestOpsWS_RedisDown_PushesZero(t *testing.T) {
	// Redis 无 key：handler 仍发 qps=0/tps=0 不 crash（用有效 token 解锁升级）。
	withTestJWTSecret(t)
	resetOpsWSConnCountersForTest()
	mr := mustRunRedis(t)
	_, wsURL, sameOrigin := setupWSTestServer(t, mr)
	token := makeOpsWSToken(t, "user-7")

	conn, _, err := dialWithToken(t, wsURL, token, sameOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
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

// ---- helpers ----

func mustRunRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mr.Close() })
	return mr
}

func statusOrZero(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
