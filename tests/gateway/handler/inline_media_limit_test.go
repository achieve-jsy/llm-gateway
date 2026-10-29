package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
)

// videoRequestBody 构造一个带内联（data: URL）视频片段的请求体，
// padding 用来把请求体撑到指定体积以上。
func videoRequestBody(padding int) string {
	pad := strings.Repeat("x", padding)
	return `{"model":"gpt-4","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"` + pad + `"},` +
		`{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AAAA"}}]}]}`
}

// TestInlineMediaSizeLimit 内联媒体超过上限时立即拒绝（不进入鉴权/配额/上游），
// 并提示改用 http(s) URL；未超限则照常透传。
func TestInlineMediaSizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := handler.NewChatHandler(&fakeChatOrchestrator{
		chatResp: &core.ChatResponse{ID: "ok", Model: "gpt-4"},
	})
	h.SetMaxInlineMediaBytes(1024)
	router.POST("/v1/chat/completions", h.Handle)

	// 超限：约 4KB 的请求体 + 视频片段 → 400 inline_media_too_large
	bigBody := videoRequestBody(4096)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(bigBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, w.Body.String())
	}
	if envelope.Error.Code != "inline_media_too_large" {
		t.Fatalf("code = %q, want inline_media_too_large (body=%s)", envelope.Error.Code, w.Body.String())
	}
	if !strings.Contains(envelope.Error.Message, "http") {
		t.Fatalf("message should suggest http(s) URL: %q", envelope.Error.Message)
	}

	// 未超限：正常透传到（假）上游
	smallBody := videoRequestBody(16)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(smallBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("small body status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestInlineMediaSizeLimitIgnoresTextOnly 纯文本大请求不受此限制约束
// （文本上限属于另一层 concern；这里只针对内联媒体）。
func TestInlineMediaSizeLimitIgnoresTextOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := handler.NewChatHandler(&fakeChatOrchestrator{
		chatResp: &core.ChatResponse{ID: "ok", Model: "gpt-4"},
	})
	h.SetMaxInlineMediaBytes(64)
	router.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"` + strings.Repeat("y", 2048) + `"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("text-only status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}
