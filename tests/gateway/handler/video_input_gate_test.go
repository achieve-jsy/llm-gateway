package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// TestUnsupportedContentPartErrorShape 验证不支持的内容片段返回稳定的
// code + param，便于客户端定位到具体片段（而不是笼统的 request_body_invalid）。
func TestUnsupportedContentPartErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := handler.NewChatHandler(&fakeChatOrchestrator{})
	router.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":[{"type":"pdf","pdf":{"url":"https://a/b.pdf"}}]}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Error struct {
			Code    string  `json:"code"`
			Param   *string `json:"param"`
			Message string  `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal error body: %v (body=%s)", err, w.Body.String())
	}
	if envelope.Error.Code != "unsupported_content_part" {
		t.Fatalf("code = %q, want unsupported_content_part (body=%s)", envelope.Error.Code, w.Body.String())
	}
	if envelope.Error.Param == nil || *envelope.Error.Param != "content[0].type" {
		t.Fatalf("param = %v, want content[0].type", envelope.Error.Param)
	}
	if !strings.Contains(envelope.Error.Message, "pdf") {
		t.Fatalf("message = %q, want it to mention pdf", envelope.Error.Message)
	}
}

// TestVideoInputNotSupportedRenders400 校验 UnifiedError 渲染：形态保持 OpenAI 兼容
// （error.type/code/message），便于客户端按 code 分支处理。
func TestVideoInputNotSupportedRenders400(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := handler.NewChatHandler(&fakeChatOrchestrator{chatErr: model.ErrVideoInputNotSupported})
	router.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
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
	if envelope.Error.Code != "video_input_not_supported" {
		t.Fatalf("code = %q, want video_input_not_supported (body=%s)", envelope.Error.Code, w.Body.String())
	}
	if envelope.Error.Message == "" {
		t.Fatal("message should not be empty")
	}
}
