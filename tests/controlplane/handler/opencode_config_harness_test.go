package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// testScriptToken 是 opencode 配置脚本相关测试共用的 token。
const testScriptToken = "tok-test"

// tokenFixture 对应 handler 在 Redis 里存的 token 记录（GetScript 靠它渲染脚本）。
type tokenFixture struct {
	ProviderID   string
	ProviderName string
	BaseURL      string
	APIKey       string
	Status       string
}

// fakeSettings 顶掉 SettingsServiceProvider —— 脚本只需要一个供应商名称。
type fakeSettings struct {
	Name string
}

func (f fakeSettings) GetSupplierName(context.Context) string { return f.Name }

// newScriptEngine 起 miniredis、写入 token 记录，返回挂好 /script 路由的 gin 引擎。
// 返回的 miniredis 句柄供需要直接改 Redis（例如删掉 token）的用例使用。
func newScriptEngine(t *testing.T, f tokenFixture) (*gin.Engine, *miniredis.Miniredis) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})

	if f.ProviderID == "" {
		f.ProviderID = "biangfeng-gateway"
	}
	if f.Status == "" {
		f.Status = "pending"
	}
	rec := map[string]string{
		"key_id":        "key-1",
		"provider_id":   f.ProviderID,
		"provider_name": f.ProviderName,
		"base_url":      f.BaseURL,
		"api_key":       f.APIKey,
		"status":        f.Status,
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, mr.Set("opencode_cfg_token:"+testScriptToken, string(raw)))

	h := handler.NewOpencodeConfigHandler(rdb, fakeSettings{Name: "测试供应商"})

	r := gin.New()
	r.GET("/api/v1/opencode-config/script", h.GetScript)
	return r, mr
}

// fetchScript 取一次安装脚本；ver 为空时不带该查询参数（模拟前端默认行为）。
func fetchScript(t *testing.T, r *gin.Engine, osType, ver string) (int, string) {
	t.Helper()
	q := url.Values{}
	q.Set("token", testScriptToken)
	q.Set("os", osType)
	if ver != "" {
		q.Set("ver", ver)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/opencode-config/script?"+q.Encode(), nil)
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}
