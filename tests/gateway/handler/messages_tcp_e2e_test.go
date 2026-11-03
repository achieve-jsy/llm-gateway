package handler_test

// 真实 TCP 端到端：httptest.NewServer 起真实 HTTP 服务器，用真实 http.Client 走真实网络栈
// 打到 /v1/messages，验证 legacy 与 relaykit(active) 两条路径在真实 HTTP 传输下的行为一致。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

type tcpOrchestrator struct{}

func (tcpOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		ID: "chatcmpl-tcp", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "TCP hello"}}},
		Usage: core.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10},
	}, nil
}

func (tcpOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	sse := "" +
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"TCP"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":" stream"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}` + "\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse)), nil
}

func startTCPServer(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"tcp-model"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "tcp-model", ModelID: "1", ModelName: "tcp-model", ProviderName: "p",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(tcpOrchestrator{}, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{DefaultMode: mode})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.POST("/v1/messages", h.HandleMessages)
	return httptest.NewServer(r)
}

func TestTCPE2E_LegacyVsActive_NonStream(t *testing.T) {
	body := `{"model":"tcp-model","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`
	client := &http.Client{Timeout: 10 * time.Second}

	results := map[string]map[string]any{}
	for _, mode := range []string{"legacy", "active"} {
		srv := startTCPServer(t, mode)
		resp, err := client.Post(srv.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			srv.Close()
			t.Fatalf("%s: HTTP post failed: %v", mode, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		srv.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", mode, resp.StatusCode, string(raw))
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: invalid JSON: %v", mode, err)
		}
		results[mode] = m
		t.Logf("%s non-stream: type=%v content_present=%v", mode, m["type"], m["content"] != nil)
	}

	for _, mode := range []string{"legacy", "active"} {
		if results[mode]["type"] != "message" {
			t.Errorf("%s: type=%v want message", mode, results[mode]["type"])
		}
		if results[mode]["role"] != "assistant" {
			t.Errorf("%s: role=%v want assistant", mode, results[mode]["role"])
		}
	}
}

func TestTCPE2E_LegacyVsActive_Stream(t *testing.T) {
	body := `{"model":"tcp-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	client := &http.Client{Timeout: 10 * time.Second}

	for _, mode := range []string{"legacy", "active"} {
		srv := startTCPServer(t, mode)
		resp, err := client.Post(srv.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			srv.Close()
			t.Fatalf("%s: post failed: %v", mode, err)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
			t.Errorf("%s: content-type=%q want text/event-stream", mode, ct)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		srv.Close()

		out := string(raw)
		for _, want := range []string{"event: message_start", "event: content_block_delta", "event: message_stop"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q\nbody=%.400s", mode, want, out)
			}
		}
		// 每个 data 行必须是合法 JSON
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			p := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if p == "" || p == "[DONE]" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(p), &v); err != nil {
				t.Errorf("%s: bad JSON: %v payload=%.100s", mode, err, p)
			}
		}
		t.Logf("%s stream over TCP: %d bytes", mode, len(raw))
	}
}
