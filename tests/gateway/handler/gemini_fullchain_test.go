package handler_test

// Gemini 原生旁路的真链路：真实 GeminiHandler → 真实 ChatOrchestrator
// → 真实 providers.Router → 真实 vertex.Provider → 假 Vertex 上游。
//
// 与其他 Gemini 用例的分工：
//   - gemini_native_e2e_test.go 用 fake orchestrator，只验证 handler 分支；
//   - tests/providers/router_native_gemini_test.go 从 Router 起步；
// 这里把整条链一次贯通，只伪造最上游的 Vertex HTTP 端点。

import (
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
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/vertex"
	"github.com/gin-gonic/gin"
)

// geminiChainLookup 是单模型注册表，把 gemini-2.5-pro 指向真的 vertex provider。
type geminiChainLookup struct{ provider core.Provider }

func (l *geminiChainLookup) Supports(string) bool                 { return true }
func (l *geminiChainLookup) GetProvider(string) core.Provider     { return l.provider }
func (l *geminiChainLookup) GetProviderType(string) string        { return "vertex" }
func (l *geminiChainLookup) GetProviderName(string) string        { return "vertex-main" }
func (l *geminiChainLookup) GetProviderNameForType(string) string { return "vertex-main" }
func (l *geminiChainLookup) GetProviderTypeForName(string) string { return "vertex" }
func (l *geminiChainLookup) ListModels() []core.Model             { return []core.Model{{ID: "gemini-2.5-pro"}} }
func (l *geminiChainLookup) ModelCount() int                      { return 1 }

// newGeminiFullChainRouter 组装「GeminiHandler + 真 orchestrator + 真 router + 真 vertex」。
func newGeminiFullChainRouter(t *testing.T, upstreamURL string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	p := vertex.New(providers.ProviderConfig{
		Type:     "vertex",
		BaseURL:  upstreamURL + "/publishers/google",
		APIKey:   "fake-key",
		AuthType: "api_key",
		APIMode:  "native",
	}, providers.ProviderOptions{})

	router, err := providers.NewRouter(&geminiChainLookup{provider: p})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	orch := orchestrator.NewChatOrchestrator(router)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"gemini-2.5-pro"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "gemini-2.5-pro", ModelID: "1", ModelName: "gemini-2.5-pro", ProviderName: "vertex-main",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewGeminiHandlerWithGroup(orch, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1beta/models/:modelAction", h.Handle)
	r.POST("/v1/models/:modelAction", h.Handle)
	return r
}

// TestGeminiFullChain_NativePassthrough 端到端验证 Gemini 请求零转换到 Vertex。
func TestGeminiFullChain_NativePassthrough(t *testing.T) {
	var gotPath string
	var gotBody []byte
	var gotKey string
	upstreamResp := `{"candidates":[{"content":{"parts":[{"text":"native hi"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4,"totalTokenCount":15}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamResp)
	}))
	defer upstream.Close()

	r := newGeminiFullChainRouter(t, upstream.URL)

	// 带 Gemini 才有的原生字段：generationConfig.topK / safetySettings
	reqBody := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"topK":40,"topP":0.95,"temperature":0.7},"safetySettings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_NONE"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// 1. 打到 Vertex 原生端点
	if want := "/publishers/google/models/gemini-2.5-pro:generateContent"; gotPath != want {
		t.Errorf("upstream path = %q, want %q", gotPath, want)
	}
	if gotKey != "fake-key" {
		t.Errorf("x-goog-api-key = %q, want fake-key", gotKey)
	}

	// 2. 原生字段全部保留（这就是零转换的证据）
	var up map[string]any
	if err := json.Unmarshal(gotBody, &up); err != nil {
		t.Fatalf("upstream body 不是 JSON: %v", err)
	}
	if _, exists := up["model"]; exists {
		t.Error("model 字段应被摘除（Gemini 只认 URL 里的模型名）")
	}
	gc, ok := up["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("generationConfig 丢失")
	}
	if v, _ := gc["topK"].(float64); v != 40 {
		t.Errorf("topK = %v, want 40", gc["topK"])
	}
	if _, ok := up["safetySettings"]; !ok {
		t.Error("safetySettings 丢失：这是 Gemini 原生字段，经 Chat 转换会被丢弃")
	}

	// 3. 客户端收到上游原文
	if got := w.Body.String(); got != upstreamResp {
		t.Errorf("响应被改写: got=%.200s", got)
	}

	// 4. 计费：usageMetadata 必须被回填（否则这条路径按 0 计费）
	var clientResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &clientResp); err != nil {
		t.Fatalf("客户端响应不是 JSON: %v", err)
	}
	um, ok := clientResp["usageMetadata"].(map[string]any)
	if !ok {
		t.Fatal("usageMetadata 应原样返回给客户端")
	}
	if v, _ := um["promptTokenCount"].(float64); v != 11 {
		t.Errorf("promptTokenCount = %v, want 11", um["promptTokenCount"])
	}
}

// TestGeminiFullChain_StreamPassthrough 流式真链路。
func TestGeminiFullChain_StreamPassthrough(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"native"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":2,"totalTokenCount":13}}`,
		`data: {"candidates":[{"content":{"parts":[{"text":" hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4,"totalTokenCount":15}}`,
	}, "\n\n") + "\n\n"

	var gotPath, gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frames)
	}))
	defer upstream.Close()

	r := newGeminiFullChainRouter(t, upstream.URL)

	reqBody := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"topK":40}}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if want := "/publishers/google/models/gemini-2.5-pro:streamGenerateContent"; gotPath != want {
		t.Errorf("upstream path = %q, want %q", gotPath, want)
	}
	if gotQuery != "alt=sse" {
		t.Errorf("upstream query = %q, want alt=sse", gotQuery)
	}
	if got := w.Body.String(); got != frames {
		t.Errorf("SSE 被改写: got=%q", got)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
}
