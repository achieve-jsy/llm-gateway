package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRerankOrchestrator is a mock implementation of RerankOrchestratorInterface.
type mockRerankOrchestrator struct {
	rerankFn func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error)
}

func (m *mockRerankOrchestrator) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	return m.rerankFn(ctx, req)
}

// setupRerankTest creates a gin test context and recorder with the given JSON body.
func setupRerankTest(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func TestRerankHandlerSuccess(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":["doc1","doc2","doc3"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			assert.Equal(t, "test-model", req.Model)
			assert.Equal(t, "what is AI?", req.Query)
			assert.Len(t, req.Documents, 3)
			return &core.RerankResponse{
				ID:    "rerank-123",
				Model: "test-model",
				Results: []core.RerankResult{
					{Index: 0, RelevanceScore: 0.95},
					{Index: 2, RelevanceScore: 0.72},
					{Index: 1, RelevanceScore: 0.31},
				},
				Usage: core.RerankUsage{TotalTokens: 100},
			}, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp core.RerankResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "rerank-123", resp.ID)
	assert.Len(t, resp.Results, 3)
	assert.Equal(t, 0.95, resp.Results[0].RelevanceScore)
	assert.Equal(t, 100, resp.Usage.TotalTokens)
}

func TestRerankHandlerEmptyDocuments(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":[]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for empty documents")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerExceedDocuments(t *testing.T) {
	// Create 1001 documents (exceeds maxDocuments = 1000)
	docs := make([]any, 1001)
	for i := range docs {
		docs[i] = "doc"
	}
	req := core.RerankRequest{
		Model:     "test-model",
		Query:     "query",
		Documents: docs,
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for exceeded documents")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(string(reqBytes))

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerInvalidJSON(t *testing.T) {
	reqBody := `not-valid-json`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for invalid JSON")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerMissingModel(t *testing.T) {
	reqBody := `{"query":"what is AI?","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for missing model")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerOrchestratorError(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			return nil, model.ErrInternal
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRerankHandlerOrchestratorUnifiedError(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			return nil, model.ErrModelNotFound
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRerankHandlerOrchestratorGenericError(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			return nil, errors.New("some generic error")
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	// Generic errors fall through to respondError(c, model.ErrInternal)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRerankHandlerMissingDocumentsField(t *testing.T) {
	// "documents" field is completely absent from the JSON
	reqBody := `{"model":"test-model","query":"what is AI?"}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	// Empty documents defaults to len==0, which triggers the empty-documents check
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerEmptyQuery(t *testing.T) {
	reqBody := `{"model":"test-model","query":"","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for empty query")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerTopNZero(t *testing.T) {
	reqBody := `{"model":"test-model","query":"what is AI?","documents":["doc1","doc2"],"top_n":0}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for top_n=0")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRerankHandlerEmptyModel(t *testing.T) {
	reqBody := `{"model":"","query":"what is AI?","documents":["doc1","doc2"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called for empty model")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)

	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestValidateRerankQueryAndDocuments(t *testing.T) {
	assert.NoError(t, validateRerankQuery("hello"))
	assert.NoError(t, validateRerankQuery(map[string]any{"image": "https://example.com/q.png"}))
	assert.Error(t, validateRerankQuery(""))
	assert.Error(t, validateRerankQuery(map[string]any{"video": "https://example.com/q.mp4"}))
	assert.Error(t, validateRerankQuery(map[string]any{"text": "a", "image": "b"}))
	assert.Error(t, validateRerankQuery(123))

	assert.NoError(t, validateRerankDocuments([]any{"doc1", map[string]any{"image": "https://example.com/d.png"}}))
	assert.NoError(t, validateRerankDocuments([]any{map[string]any{"video": "https://example.com/d.mp4"}}))
	assert.Error(t, validateRerankDocuments(nil))
	assert.Error(t, validateRerankDocuments([]any{""}))
	assert.Error(t, validateRerankDocuments([]any{map[string]any{}}))
}

func TestIsMultimodalRerankInput(t *testing.T) {
	assert.False(t, isMultimodalRerankInput("q", []any{"d"}))
	assert.False(t, isMultimodalRerankInput(map[string]any{"text": "q"}, []any{map[string]any{"text": "d"}}))
	assert.True(t, isMultimodalRerankInput(map[string]any{"image": "https://x"}, []any{"d"}))
	assert.True(t, isMultimodalRerankInput("q", []any{map[string]any{"video": "https://x"}}))
}

func TestRerankHandlerMultimodalRequiresRerankVL(t *testing.T) {
	reqBody := `{"model":"gte-rerank-v2","query":{"image":"https://example.com/q.png"},"documents":["doc1"]}`

	called := false
	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			called = true
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)
	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, called)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "model_capability_mismatch", errObj["code"])
}

func TestRerankHandlerTextObjectAllowedWithoutRerankVL(t *testing.T) {
	reqBody := `{"model":"gte-rerank-v2","query":{"text":"what is AI?"},"documents":[{"text":"doc1"}]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			return &core.RerankResponse{
				Model:   "gte-rerank-v2",
				Results: []core.RerankResult{{Index: 0, RelevanceScore: 0.9}},
			}, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)
	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRerankHandlerInvalidMultimodalShape(t *testing.T) {
	// 双 key 会被兼容层收敛为单 key（优先 image）；无 group 时因 ModelType 空触发 capability mismatch
	reqBody := `{"model":"qwen3-vl-rerank","query":{"image":"https://x","text":"y"},"documents":["doc1"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			t.Fatal("orchestrator should not be called without rerank_vl ModelType")
			return nil, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)
	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "model_capability_mismatch", errObj["code"], "双 key 应先被整形，再因能力门禁失败，而非 shape 校验失败")
}

func TestRerankHandlerImageURLAlias(t *testing.T) {
	reqBody := `{"model":"qwen3-vl-rerank","query":{"image_url":"https://example.com/q.png"},"documents":["doc1"]}`

	mock := &mockRerankOrchestrator{
		rerankFn: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			q, ok := req.Query.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "https://example.com/q.png", q["image"])
			return &core.RerankResponse{
				Model:   "qwen3-vl-rerank",
				Results: []core.RerankResult{{Index: 0, RelevanceScore: 0.5}},
			}, nil
		},
	}

	handler := NewRerankHandler(mock)
	c, w := setupRerankTest(reqBody)
	handler.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code) // ModelType 空 → capability mismatch
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "model_capability_mismatch", errObj["code"])
}

func TestNormalizeRerankRequest(t *testing.T) {
	req := &core.RerankRequest{
		Query: []any{map[string]any{"image_url": "https://q", "text": "ignore"}},
		Documents: []any{
			"plain",
			map[string]any{"image": "https://d", "text": "x"},
		},
	}
	require.NoError(t, normalizeRerankRequest(req))
	q := req.Query.(map[string]any)
	assert.Equal(t, "https://q", q["image"])
	assert.Equal(t, "plain", req.Documents[0])
	d1 := req.Documents[1].(map[string]any)
	assert.Equal(t, "https://d", d1["image"])
	_, hasText := d1["text"]
	assert.False(t, hasText)
}