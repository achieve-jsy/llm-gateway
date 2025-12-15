package authclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAuthClient 创建指向 httptest.Server 的 AuthClient。
func newTestAuthClient(server *httptest.Server) *infra.AuthClient {
	return infra.NewAuthClient(server.URL, "test-token")
}

func TestVerifyKey_Success(t *testing.T) {
	// 控制面正常返回 active key
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"key_id":            "key-uuid-001",
				"key_name":          "test",
				"status":            "active",
				"access_all_groups": true,
				"log_enabled":       true,
				"user": map[string]any{
					"user_id": "user-uuid-001",
					"name":    "Alice",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestAuthClient(srv)
	resp, err := client.VerifyKey(context.Background(), "sk-testkey")

	require.NoError(t, err)
	assert.Equal(t, "active", resp.Data.Status)
	assert.Equal(t, "key-uuid-001", resp.Data.KeyID)
	assert.True(t, resp.Data.AccessAllGroups)
}

func TestVerifyKey_500_ReturnsError(t *testing.T) {
	// 控制面 500 时应返回 error，上层给调用方 503
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestAuthClient(srv)
	resp, err := client.VerifyKey(context.Background(), "sk-testkey")

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "500")
}

func TestVerifyKey_403_ReturnsError(t *testing.T) {
	// 控制面 403（internal token 缺失/错误）时应返回 error，而不是静默解析出空 status
	// 修复前：403 被静默解析，Data.Status="" != "active"，gateway 返回误导性 401
	// 修复后：403 返回 error，gateway 返回 503（auth service unavailable）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "forbidden",
				"message": "missing internal token",
				"type":    "authorization_error",
			},
		})
	}))
	defer srv.Close()

	client := newTestAuthClient(srv)
	resp, err := client.VerifyKey(context.Background(), "sk-testkey")

	assert.Error(t, err, "403 应返回 error 而不是静默通过")
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "403")
}

func TestVerifyKey_404_ReturnsError(t *testing.T) {
	// 控制面 404（key 不存在）时应返回 error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "key_not_found",
				"message": "key not found",
				"type":    "not_found",
			},
		})
	}))
	defer srv.Close()

	client := newTestAuthClient(srv)
	resp, err := client.VerifyKey(context.Background(), "sk-testkey")

	assert.Error(t, err, "404 应返回 error")
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "404")
}

func TestVerifyKey_InternalToken_Sent(t *testing.T) {
	// 验证 AuthClient 正确携带 X-Internal-Token header
	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Internal-Token")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"status": "active",
				"user":   map[string]any{"user_id": "u1", "name": "test"},
			},
		})
	}))
	defer srv.Close()

	client := infra.NewAuthClient(srv.URL, "my-secret-token")
	_, err := client.VerifyKey(context.Background(), "sk-testkey")

	require.NoError(t, err)
	assert.Equal(t, "my-secret-token", receivedToken, "X-Internal-Token 应被正确发送")
}

func TestVerifyKey_EmptyToken_NoHeader(t *testing.T) {
	// token 为空时不应发送 X-Internal-Token header
	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Internal-Token")
		w.WriteHeader(http.StatusForbidden) // 模拟控制面拒绝
	}))
	defer srv.Close()

	client := infra.NewAuthClient(srv.URL, "") // 空 token
	_, err := client.VerifyKey(context.Background(), "sk-testkey")

	assert.Error(t, err)
	assert.Empty(t, receivedToken, "token 为空时不应发送 X-Internal-Token")
}

func TestVerifyKey_Unreachable_ReturnsError(t *testing.T) {
	// 控制面不可达时应返回 error
	client := infra.NewAuthClient("http://127.0.0.1:19999", "token")
	resp, err := client.VerifyKey(context.Background(), "sk-testkey")

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unreachable")
}
