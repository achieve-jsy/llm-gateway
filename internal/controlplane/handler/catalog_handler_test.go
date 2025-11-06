package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCatalogService 满足 handler 需要的最小接口。
type fakeCatalogService struct {
	mu              sync.Mutex
	searchFn        func(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error)
	getByIDFn       func(id uuid.UUID) (*model.ModelCatalog, error)
	getBySrcFn      func(source, id string) (*model.ModelCatalog, error)
	syncFromRemote  func(ctx context.Context) (int, error)
	computeDiffFn   func(ctx context.Context) ([]service.PriceDiffEntry, error)
	applyDiffFn     func(ctx context.Context, ids []uuid.UUID) (int, error)
	autoBindFn      func(ctx context.Context) (*service.AutoBindResult, error)
	bindingStatsFn  func(ctx context.Context) (*service.BindingStats, error)
	syncTriggered   int
	asyncCompletion chan struct{}
}

func (f *fakeCatalogService) Search(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
	if f.searchFn != nil {
		return f.searchFn(opts)
	}
	return nil, 0, nil
}

func (f *fakeCatalogService) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(id)
	}
	return nil, nil
}

func (f *fakeCatalogService) GetBySourceModelID(source, id string) (*model.ModelCatalog, error) {
	if f.getBySrcFn != nil {
		return f.getBySrcFn(source, id)
	}
	return nil, nil
}

func (f *fakeCatalogService) TriggerAsyncSync(ctx context.Context) {
	f.mu.Lock()
	f.syncTriggered++
	f.mu.Unlock()
	go func() {
		if f.syncFromRemote != nil {
			_, _ = f.syncFromRemote(context.Background())
		}
		if f.asyncCompletion != nil {
			f.asyncCompletion <- struct{}{}
		}
	}()
}

func (f *fakeCatalogService) IsSyncing() bool { return false }

func (f *fakeCatalogService) ComputePriceDiff(ctx context.Context) ([]service.PriceDiffEntry, error) {
	if f.computeDiffFn != nil {
		return f.computeDiffFn(ctx)
	}
	return nil, nil
}

func (f *fakeCatalogService) ApplyPriceDiff(ctx context.Context, ids []uuid.UUID) (int, error) {
	if f.applyDiffFn != nil {
		return f.applyDiffFn(ctx, ids)
	}
	return 0, nil
}

func (f *fakeCatalogService) AutoBind(ctx context.Context) (*service.AutoBindResult, error) {
	if f.autoBindFn != nil {
		return f.autoBindFn(ctx)
	}
	return &service.AutoBindResult{}, nil
}

func (f *fakeCatalogService) BindingStats(ctx context.Context) (*service.BindingStats, error) {
	if f.bindingStatsFn != nil {
		return f.bindingStatsFn(ctx)
	}
	return &service.BindingStats{}, nil
}

func newCatalogRouter(svc CatalogServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewCatalogHandler(svc)
	g := r.Group("/api/v1/model-catalog")
	g.GET("/search", h.Search)
	g.GET("/by-source", h.GetBySource)
	g.GET("/:id", h.GetByID)
	g.POST("/sync", h.Sync)
	g.GET("/diff", h.Diff)
	g.POST("/apply-diff", h.ApplyDiff)
	g.POST("/auto-bind", h.AutoBind)
	g.GET("/binding-stats", h.BindingStats)
	return r
}

func TestCatalogHandler_Search_QueryParam(t *testing.T) {
	called := false
	svc := &fakeCatalogService{searchFn: func(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
		called = true
		assert.Equal(t, "gpt", opts.Query)
		assert.Equal(t, "openai", opts.Provider)
		assert.Equal(t, "chat", opts.Mode)
		assert.Equal(t, 10, opts.Limit)
		return []model.ModelCatalog{{ID: uuid.New(), SourceModelID: "gpt-4o"}}, 1, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/search?q=gpt&provider=openai&mode=chat&limit=10", nil))
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCatalogHandler_Search_FuzzyMatchSourceID(t *testing.T) {
	svc := &fakeCatalogService{searchFn: func(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
		assert.Equal(t, "deepseek-v3", opts.Query)
		return []model.ModelCatalog{{ID: uuid.New(), SourceModelID: "azure_ai/deepseek-v3.2"}}, 1, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/search?q=deepseek-v3", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "azure_ai/deepseek-v3.2")
}

// TestCatalogHandler_Search_TotalIsFullCountNotPage 回归用例：
//
// handler 的 JSON.total 必须透传 service 返回的"全表总数"，
// 不能再退化为 len(results)。前端 CatalogSyncPage.vue 依赖这一字段
// 显示「目录已收录 N 条」，否则 searchCatalog({limit:1}) 永远看到 total=1。
func TestCatalogHandler_Search_TotalIsFullCountNotPage(t *testing.T) {
	svc := &fakeCatalogService{searchFn: func(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
		assert.Equal(t, 1, opts.Limit)
		// 当前页只 1 条，但匹配条件下的"全表总数"是 2719（模拟生产数据）。
		return []model.ModelCatalog{{ID: uuid.New(), SourceModelID: "gpt-4o"}}, 2719, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/search?limit=1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":2719`,
		"total 必须等于 service 返回的全表总数，不能退化为 len(results)")
}

// detail 路由方案：选择 GET /by-source?source_model_id=… 而非路径参数，
// 因为 source_model_id 含 '/'（如 azure_ai/deepseek-v3.2）会被 Gin 当作分隔符。
func TestCatalogHandler_GetBySource_404(t *testing.T) {
	svc := &fakeCatalogService{getBySrcFn: func(source, id string) (*model.ModelCatalog, error) {
		return nil, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/by-source?source_model_id=missing-model", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCatalogHandler_GetBySource_OK(t *testing.T) {
	want := &model.ModelCatalog{ID: uuid.New(), SourceModelID: "azure_ai/deepseek-v3.2", Provider: "azure_ai"}
	svc := &fakeCatalogService{getBySrcFn: func(source, id string) (*model.ModelCatalog, error) {
		assert.Equal(t, "litellm", source)
		assert.Equal(t, "azure_ai/deepseek-v3.2", id)
		return want, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/by-source?source_model_id=azure_ai/deepseek-v3.2", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "azure_ai/deepseek-v3.2")
}

func TestCatalogHandler_Sync_AcceptedAndAsync(t *testing.T) {
	done := make(chan struct{}, 1)
	svc := &fakeCatalogService{
		asyncCompletion: done,
		syncFromRemote:  func(ctx context.Context) (int, error) { return 100, nil },
	}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/model-catalog/sync", nil))

	assert.Equal(t, http.StatusAccepted, w.Code, "sync 必须 202 立刻返回，不阻塞 admin 请求")
	svc.mu.Lock()
	assert.Equal(t, 1, svc.syncTriggered, "TriggerAsyncSync 必须被调用一次")
	svc.mu.Unlock()
	<-done
}

func TestCatalogHandler_Diff_ReturnsOverrideFlag(t *testing.T) {
	svc := &fakeCatalogService{computeDiffFn: func(ctx context.Context) ([]service.PriceDiffEntry, error) {
		return []service.PriceDiffEntry{
			{
				ModelID:              uuid.New(),
				ModelName:            "gpt-4o",
				CatalogSourceModelID: "gpt-4o",
				LocalInputPrice:      1.0,
				CatalogInputPrice:    2.5,
				IsOverridden:         true,
				InputDeltaPct:        150,
				OutputDeltaPct:       400,
			},
		}, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/diff", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"is_overridden":true`)
	assert.Contains(t, body, `"local_input_price":1`)
	assert.Contains(t, body, `"catalog_input_price":2.5`)
	assert.Contains(t, body, `"input_delta_pct":150`)
	assert.Contains(t, body, `"catalog_source_model_id":"gpt-4o"`)
}

func TestCatalogHandler_ApplyDiff_PartialSelection(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	gotIDs := []uuid.UUID{}
	svc := &fakeCatalogService{applyDiffFn: func(ctx context.Context, ids []uuid.UUID) (int, error) {
		gotIDs = ids
		return len(ids), nil
	}}
	r := newCatalogRouter(svc)

	body, _ := json.Marshal(map[string]any{"model_ids": []string{id1.String(), id2.String()}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-catalog/apply-diff", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, gotIDs, 2)
	assert.Contains(t, w.Body.String(), `"updated":2`)
}

func TestCatalogHandler_ApplyDiff_InvalidIDs(t *testing.T) {
	svc := &fakeCatalogService{}
	r := newCatalogRouter(svc)

	body := []byte(`{"model_ids": ["not-a-uuid"]}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-catalog/apply-diff", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCatalogHandler_ApplyDiff_ServiceError(t *testing.T) {
	svc := &fakeCatalogService{applyDiffFn: func(ctx context.Context, ids []uuid.UUID) (int, error) {
		return 0, errors.New("boom")
	}}
	r := newCatalogRouter(svc)
	body, _ := json.Marshal(map[string]any{"model_ids": []string{uuid.NewString()}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-catalog/apply-diff", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCatalogHandler_AutoBind_ReturnsResult(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	svc := &fakeCatalogService{autoBindFn: func(ctx context.Context) (*service.AutoBindResult, error) {
		return &service.AutoBindResult{
			Matched: []service.AutoBindMatch{
				{ModelID: mid, ModelName: "gpt-4o", CatalogID: cid, CatalogSourceModelID: "gpt-4o"},
			},
			Ambiguous: []service.AutoBindAmbiguity{
				{ModelID: uuid.New(), ModelName: "deepseek-r1", Candidates: []service.AutoBindCandidate{
					{CatalogID: uuid.New(), CatalogSourceModelID: "azure_ai/deepseek-r1", Provider: "azure_ai"},
					{CatalogID: uuid.New(), CatalogSourceModelID: "fireworks_ai/deepseek-r1", Provider: "fireworks_ai"},
				}},
			},
			Unmatched: 3,
		}, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-catalog/auto-bind", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"matched"`)
	assert.Contains(t, body, `"ambiguous"`)
	assert.Contains(t, body, `"unmatched":3`)
	assert.Contains(t, body, mid.String())
}

func TestCatalogHandler_AutoBind_ServiceError(t *testing.T) {
	svc := &fakeCatalogService{autoBindFn: func(ctx context.Context) (*service.AutoBindResult, error) {
		return nil, errors.New("boom")
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-catalog/auto-bind", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestCatalogHandler_BindingStats_OK 验证 binding-stats endpoint 把 service 返回的
// 四个计数字段如实透传给前端。前端依赖这些字段做空状态语义判定。
func TestCatalogHandler_BindingStats_OK(t *testing.T) {
	svc := &fakeCatalogService{bindingStatsFn: func(ctx context.Context) (*service.BindingStats, error) {
		return &service.BindingStats{
			CatalogTotal: 2719,
			ModelTotal:   12,
			BoundCount:   5,
			UnboundCount: 7,
		}, nil
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/binding-stats", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"catalog_total":2719`)
	assert.Contains(t, body, `"model_total":12`)
	assert.Contains(t, body, `"bound_count":5`)
	assert.Contains(t, body, `"unbound_count":7`)
}

func TestCatalogHandler_BindingStats_ServiceError(t *testing.T) {
	svc := &fakeCatalogService{bindingStatsFn: func(ctx context.Context) (*service.BindingStats, error) {
		return nil, errors.New("boom")
	}}
	r := newCatalogRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-catalog/binding-stats", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
