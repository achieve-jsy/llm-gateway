package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitRedis_Success(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb, err := InitRedis(config.RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)
	defer rdb.Close()

	// 验证连接可用
	require.NoError(t, rdb.Ping(context.Background()).Err())
}

func TestInitRedis_PingFailure(t *testing.T) {
	// 使用一个肯定无法连接的地址（保留端口 0/1）
	rdb, err := InitRedis(config.RedisConfig{Addr: "127.0.0.1:1"})
	require.Error(t, err)
	assert.Nil(t, rdb)
	assert.Contains(t, err.Error(), "ping redis")
}

func TestInitTracer_Disabled(t *testing.T) {
	shutdown, err := InitTracer(config.OTLPConfig{Enabled: false})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	shutdown() // 调用 shutdown 不应 panic
}

func TestLoadFromControlPlane_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/internal/v1/models/available"))
		assert.Equal(t, "internal-token", r.Header.Get("X-Internal-Token"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{
					"id": "model-a", "name": "gpt-4", "model_type": "chat",
					"provider_id": "p1", "provider_name": "openai", "provider_type": "openai",
					"provider_endpoint": "https://api.openai.com", "provider_credentials": "sk-1",
				},
				{
					"id": "model-b", "name": "gpt-3.5", "model_type": "chat",
					"provider_id": "p1", "provider_name": "openai", "provider_type": "openai",
					"provider_endpoint": "https://api.openai.com", "provider_credentials": "sk-1",
				},
				{
					"id": "model-c", "name": "claude-3", "model_type": "chat",
					"provider_id": "p2", "provider_name": "anthropic", "provider_type": "anthropic",
					"provider_endpoint": "https://api.anthropic.com", "provider_credentials": "anth-1",
				},
			},
		})
	}))
	defer server.Close()

	entries, err := LoadFromControlPlane(server.URL, "internal-token")
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// 按 provider 聚合
	byName := map[string][]string{}
	for _, e := range entries {
		assert.True(t, e.Enabled)
		byName[e.Name] = e.Models
	}
	assert.ElementsMatch(t, []string{"gpt-4", "gpt-3.5"}, byName["openai"])
	assert.ElementsMatch(t, []string{"claude-3"}, byName["anthropic"])
}

func TestLoadFromControlPlane_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no auth", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := LoadFromControlPlane(server.URL, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestLoadFromControlPlane_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	_, err := LoadFromControlPlane(server.URL, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestLoadFromControlPlane_NetworkError(t *testing.T) {
	_, err := LoadFromControlPlane("http://127.0.0.1:1", "token")
	require.Error(t, err)
}

func TestSubscriber_StartAndReceive(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	received := make(chan ChangeEvent, 1)
	sub := NewSubscriber(rdb, func(e ChangeEvent) {
		received <- e
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sub.Start(ctx)

	// 等待订阅就绪
	// miniredis 是即时的；多发几次确保被消费
	deadline := time.After(2 * time.Second)
	for {
			ev := ChangeEvent{Type: "provider_created", ResourceType: "provider", ResourceID: "id-1", SnapshotVersion: 7, Timestamp: time.Now().UTC()}
			payload, _ := json.Marshal(ev)
			require.NoError(t, rdb.Publish(ctx, ConfigChangeChannel, string(payload)).Err())

		select {
		case got := <-received:
			assert.Equal(t, ev.Type, got.Type)
			assert.Equal(t, ev.ResourceID, got.ResourceID)
			assert.Equal(t, ev.SnapshotVersion, got.SnapshotVersion)
			return
		case <-time.After(100 * time.Millisecond):
			// 重试
		case <-deadline:
			t.Fatal("did not receive event within deadline")
		}
	}
}

func TestSubscriber_HandleMessage_InvalidJSON(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()

	called := false
	sub := NewSubscriber(rdb, func(e ChangeEvent) { called = true })
	sub.handleMessage(&redis.Message{Payload: "not-json"})
	assert.False(t, called)
}

func TestSubscriber_HandleMessage_Valid(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()

	var got ChangeEvent
	sub := NewSubscriber(rdb, func(e ChangeEvent) { got = e })
	payload, _ := json.Marshal(ChangeEvent{Type: "model_created", ResourceID: "abc", SnapshotVersion: 11})
	sub.handleMessage(&redis.Message{Payload: string(payload)})
	assert.Equal(t, "model_created", got.Type)
	assert.Equal(t, "abc", got.ResourceID)
	assert.Equal(t, int64(11), got.SnapshotVersion)
}
