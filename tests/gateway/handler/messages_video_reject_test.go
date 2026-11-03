package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
)

// TestMessagesVideoBlockRejectedWith400 钉住 /v1/messages 的视频拒绝形态：
// Anthropic 协议没有视频输入片段，遇到 video 块必须回 400，而不是把块丢掉
// 让模型凭空作答（静默丢内容的用户体感是"模型在胡说"）。
func TestMessagesVideoBlockRejectedWith400(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := handler.NewMessagesHandler(&capturingMessagesOrchestrator{})
	router.POST("/v1/messages", h.HandleMessages)

	body := `{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":[
		{"type":"text","text":"这段视频讲了什么"},
		{"type":"video_url","video_url":{"url":"https://a/b.mp4"}}
	]}]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal error body: %v (body=%s)", err, w.Body.String())
	}
	if envelope.Error.Message == "" {
		t.Fatalf("error.message 不能为空 (body=%s)", w.Body.String())
	}
	if !strings.Contains(envelope.Error.Message, "视频") {
		t.Fatalf("message = %q, want 提到视频输入不支持", envelope.Error.Message)
	}
}
