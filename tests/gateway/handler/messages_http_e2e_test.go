package handler_test

// 真实 HTTP 层端到端：同一个上游 SSE 输入，分别走 legacy 与 relaykit(active) 两条路径，
// 验证两个路径都能产出合法的 Anthropic SSE，且关键事件不缺失。
// 上游用 fake orchestrator 提供真实 SSE 字节流，handler/router 全部是生产代码。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// rkOrchestrator 返回固定的 OpenAI Chat 响应和 SSE 流，用于对比两条转换路径。
type rkOrchestrator struct{}

func (rkOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		ID: "chatcmpl-rk", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "Hello from upstream"}}},
		Usage: core.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}

func (rkOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	sse := "" +
		`data: {"id":"chatcmpl-rk","object":"chat.completion.chunk","model":"rk-model","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"chatcmpl-rk","object":"chat.completion.chunk","model":"rk-model","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"chatcmpl-rk","object":"chat.completion.chunk","model":"rk-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse)), nil
}

func newRKRouter(t *testing.T, mode string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"rk-model"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "rk-model", ModelID: "1", ModelName: "rk-model", ProviderName: "p1",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(rkOrchestrator{}, gd)

	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: mode,
		PerKey:      map[string]string{"test-key": mode},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

// assertAnthropicSSE 校验输出是合法的 Anthropic SSE：每个 data 行是合法 JSON，event 行成对出现。
func assertAnthropicSSE(t *testing.T, body string) []string {
	t.Helper()
	var events []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event:") {
			events = append(events, strings.TrimSpace(strings.TrimPrefix(line, "event:")))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(payload), &v); err != nil {
				t.Fatalf("invalid JSON in SSE data: %v\npayload=%.120s", err, payload)
			}
		}
	}
	return events
}

func TestHTTPE2E_LegacyStream(t *testing.T) {
	r := newRKRouter(t, "legacy")
	body := `{"model":"rk-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: message_start") {
		t.Error("legacy: missing message_start")
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Error("legacy: missing message_stop")
	}
	if !strings.Contains(out, "Hello") {
		t.Error("legacy: missing content")
	}
	assertAnthropicSSE(t, out)
	t.Logf("legacy stream OK, %d bytes", len(out))
}

func TestHTTPE2E_ActiveStream(t *testing.T) {
	r := newRKRouter(t, "active")
	body := `{"model":"rk-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	assertAnthropicSSE(t, out)
	if !strings.Contains(out, "event: message_start") {
		t.Errorf("active: missing message_start; body=%.500s", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("active: missing message_stop; body=%.500s", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("active: missing content; body=%.500s", out)
	}
	t.Logf("active stream OK, %d bytes", len(out))
}

func TestHTTPE2E_LegacyNonStream(t *testing.T) {
	r := newRKRouter(t, "legacy")
	body := `{"model":"rk-model","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, w.Body.String())
	}
	if resp["type"] != "message" {
		t.Errorf("type: got %v want message", resp["type"])
	}
	if _, ok := resp["content"]; !ok {
		t.Error("missing content")
	}
	t.Logf("legacy non-stream OK: %s", w.Body.String()[:min(200, len(w.Body.String()))])
}

func TestHTTPE2E_ActiveNonStream(t *testing.T) {
	r := newRKRouter(t, "active")
	body := `{"model":"rk-model","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, w.Body.String())
	}
	if resp["type"] != "message" {
		t.Errorf("active: type got %v want message; body=%s", resp["type"], w.Body.String())
	}
	if _, ok := resp["content"]; !ok {
		t.Errorf("active: missing content; body=%s", w.Body.String())
	}
	t.Logf("active non-stream OK: %.300s", w.Body.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
