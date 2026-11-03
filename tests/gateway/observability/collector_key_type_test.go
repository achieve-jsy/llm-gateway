package observability_test

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

func TestCollector_NewUsageEvent_KeyType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		ctxKeyType string
		want       string
	}{
		{name: "service key", ctxKeyType: "service", want: "service"},
		{name: "personal key", ctxKeyType: "personal", want: "personal"},
		{name: "empty defaults personal", ctxKeyType: "", want: "personal"},
		{name: "unknown defaults personal", ctxKeyType: "other", want: "personal"},
	}

	col := observability.NewCollector(observability.CollectorConfig{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Set("key_type", tt.ctxKeyType)
			c.Set("request_id", "req-1")

			ev := col.NewUsageEvent(c)
			assert.Equal(t, tt.want, ev.KeyType)
		})
	}
}

func TestSanitizeCallLogForWrite_ServiceKeyClearsUser(t *testing.T) {
	cl := obsmodel.CallLog{
		KeyType:  obsmodel.KeyTypeService,
		UserID:   "user-1",
		UserName: "admin",
	}
	observability.SanitizeCallLogForWrite(&cl)
	assert.Empty(t, cl.UserID)
	assert.Empty(t, cl.UserName)

	cl2 := obsmodel.CallLog{
		KeyType:  obsmodel.KeyTypePersonal,
		UserID:   "user-1",
		UserName: "alice",
	}
	observability.SanitizeCallLogForWrite(&cl2)
	assert.Equal(t, "user-1", cl2.UserID)
	assert.Equal(t, "alice", cl2.UserName)
}

func TestCollector_NewUsageEvent_LogsLargePromptCacheMiss(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("request_id", "large-zero-cache")
	c.Set("model", "gpt-5.5")
	c.Set("provider", "azure")
	c.Set("status", 200)
	c.Set("usage", core.Usage{PromptTokens: 120000, CompletionTokens: 42, TotalTokens: 120042})
	c.Set("stream", true)

	_ = observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(c)

	got := buf.String()
	assert.Contains(t, got, "large prompt cache miss observed")
	assert.Contains(t, got, "request_id=large-zero-cache")
	assert.Contains(t, got, "prompt_tokens=120000")
	assert.Contains(t, got, "stream=true")
}

func TestCollector_NewUsageEvent_DoesNotLogSmallOrCachedPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	col := observability.NewCollector(observability.CollectorConfig{}, nil)
	for _, usage := range []core.Usage{
		{PromptTokens: 99999, CompletionTokens: 1, TotalTokens: 100000},
		{PromptTokens: 120000, CompletionTokens: 1, TotalTokens: 120001, PromptTokensDetails: &core.PromptTokensDetails{CachedTokens: 1024}},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Set("request_id", "no-log")
		c.Set("status", 200)
		c.Set("usage", usage)
		_ = col.NewUsageEvent(c)
	}

	assert.False(t, strings.Contains(buf.String(), "large prompt cache miss observed"))
}
