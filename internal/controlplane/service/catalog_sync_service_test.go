package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCatalogRepo 在内存里维护 catalog 集合，避免在每个 sync test 里开 sqlite。
type fakeCatalogRepo struct {
	mu      sync.Mutex
	entries map[string]*model.ModelCatalog // 键 = source|source_model_id
	upserts int                            // 计数：测试断言"是否真的写库"
}

func newFakeCatalogRepo() *fakeCatalogRepo {
	return &fakeCatalogRepo{entries: map[string]*model.ModelCatalog{}}
}

func (f *fakeCatalogRepo) BatchUpsert(items []*model.ModelCatalog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range items {
		key := item.Source + "|" + item.SourceModelID
		copy := *item
		f.entries[key] = &copy
		f.upserts++
	}
	return nil
}

func (f *fakeCatalogRepo) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ID == id {
			c := *e
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeCatalogRepo) GetBySourceModelID(source, id string) (*model.ModelCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.entries[source+"|"+id]; ok {
		c := *e
		return &c, nil
	}
	return nil, nil
}

func (f *fakeCatalogRepo) ListAll() ([]model.ModelCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.ModelCatalog, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, *e)
	}
	return out, nil
}

func (f *fakeCatalogRepo) Count() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.entries)), nil
}

// fakeCatalogModelRepo 实现 catalog 自动同步测试中需要的最小 ModelRepo 子集。
type fakeCatalogModelRepo struct {
	mu     sync.Mutex
	models map[uuid.UUID]*model.Model
}

func newFakeCatalogModelRepo() *fakeCatalogModelRepo {
	return &fakeCatalogModelRepo{models: map[uuid.UUID]*model.Model{}}
}

func (f *fakeCatalogModelRepo) ListByCatalogID() ([]model.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Model, 0, len(f.models))
	for _, m := range f.models {
		if m.CatalogID != nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (f *fakeCatalogModelRepo) GetByID(id uuid.UUID) (*model.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.models[id]; ok {
		c := *m
		return &c, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeCatalogModelRepo) Update(m *model.Model) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *m
	f.models[m.ID] = &c
	return nil
}

func (f *fakeCatalogModelRepo) ListUnboundFromCatalog() ([]model.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Model, 0, len(f.models))
	for _, m := range f.models {
		if m.CatalogID == nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (f *fakeCatalogModelRepo) SetCatalogID(id, catalogID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.models[id]
	if !ok {
		return errors.New("not found")
	}
	cid := catalogID
	m.CatalogID = &cid
	return nil
}

func (f *fakeCatalogModelRepo) put(m *model.Model) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *m
	f.models[m.ID] = &c
}

const minimalRemoteJSON = `{
  "sample_spec": {"input_cost_per_token": 0.0, "litellm_provider": "x", "max_input_tokens": "doc-string", "mode": "chat"},
  "remote-only-model": {
    "input_cost_per_token": 1e-06,
    "output_cost_per_token": 2e-06,
    "max_input_tokens": 32000,
    "max_output_tokens": 4096,
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`

// TestCatalogSync_FromRemote_HappyPath 远程返回 200 时正常落盘。
func TestCatalogSync_FromRemote_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "llm-gateway-catalog-sync/1.0", r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPTimeout: 2 * time.Second,
	})

	count, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)
	assert.Greater(t, count, 0)
	got, err := repo.GetBySourceModelID("litellm", "remote-only-model")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "openai", got.Provider)
}

// TestCatalogSync_FromRemote_FallsBackToBundled 远程错误时自动回退到 bundled。
func TestCatalogSync_FromRemote_FallsBackToBundled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	bundled := []byte(minimalRemoteJSON)
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:    ts.URL,
		HTTPTimeout:  500 * time.Millisecond,
		BundledBytes: bundled,
	})

	count, err := svc.SyncFromRemote(context.Background())
	// 远程失败但回退成功，整体不应报错
	require.NoError(t, err)
	assert.Greater(t, count, 0)
	got, err := repo.GetBySourceModelID("litellm", "remote-only-model")
	require.NoError(t, err)
	require.NotNil(t, got, "bundled fallback should have populated catalog")
}

// TestCatalogSync_FromBundled_HappyPath 直接从 bundled 加载也走得通。
func TestCatalogSync_FromBundled_HappyPath(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		BundledBytes: []byte(minimalRemoteJSON),
	})

	count, err := svc.SyncFromBundled()
	require.NoError(t, err)
	assert.Greater(t, count, 0)
}

// TestCatalogSync_RemoteEmpty_NoBundled 远程失败且没有 bundled fallback 时返回错误。
// 注意：构造函数会用 seed.LiteLLMCatalogJSON 兜底；这里显式传入 []byte{}（非 nil）
// 来表达"绝对不要兜底"的意图。
func TestCatalogSync_RemoteEmpty_NoBundled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{
		RemoteURL:    ts.URL,
		HTTPTimeout:  500 * time.Millisecond,
		BundledBytes: []byte{}, // 显式空，跳过默认兜底
	})

	_, err := svc.SyncFromRemote(context.Background())
	require.Error(t, err)
}

func makeCatalogEntry(id string, provider string, ctxLen, maxOut int64, in, out float64) *model.ModelCatalog {
	return &model.ModelCatalog{
		ID:                    uuid.New(),
		Source:                "litellm",
		SourceModelID:         id,
		DisplayName:           id,
		Provider:              provider,
		Mode:                  "chat",
		ContextLength:         ctxLen,
		MaxOutputTokens:       maxOut,
		InputPricePerMillion:  in,
		OutputPricePerMillion: out,
		SyncedAt:              time.Now(),
	}
}

// TestApplyAutoUpdates_SyncsContextLength catalog 上下文/输出长度变了，且 model 没被锁，应自动同步。
func TestApplyAutoUpdates_SyncsContextLength(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		ContextLength: 8000, MaxOutputTokens: 1024, // 老值
		CatalogID: &cat.ID,
	})

	count, err := svc.ApplyAutoUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	got, _ := mr.GetByID(mid)
	assert.Equal(t, 128000, got.ContextLength)
	assert.Equal(t, 16384, got.MaxOutputTokens)
}

// TestApplyAutoUpdates_SkipsWhenOverridden 任一 override flag 为 true 都应跳过那一字段。
func TestApplyAutoUpdates_SkipsWhenOverridden(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		ContextLength: 8000, MaxOutputTokens: 1024,
		CtxOverridden:       true, // 锁定 context
		MaxOutputOverridden: false,
		CatalogID:           &cat.ID,
	})

	_, err := svc.ApplyAutoUpdates(context.Background())
	require.NoError(t, err)

	got, _ := mr.GetByID(mid)
	assert.Equal(t, 8000, got.ContextLength, "ctx_overridden=true 时不能动 context_length")
	assert.Equal(t, 16384, got.MaxOutputTokens, "max_output_overridden=false 时仍然同步 max_output_tokens")
}

// TestApplyAutoUpdates_NeverTouchesPrice 价格永不被自动同步动到。
func TestApplyAutoUpdates_NeverTouchesPrice(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 99, 199)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 1, OutputPrice: 2, // 老值
		CatalogID: &cat.ID,
	})

	_, err := svc.ApplyAutoUpdates(context.Background())
	require.NoError(t, err)

	got, _ := mr.GetByID(mid)
	assert.InDelta(t, 1.0, got.InputPrice, 1e-6)
	assert.InDelta(t, 2.0, got.OutputPrice, 1e-6)
}

// TestComputePriceDiff_DetectsOverride 价格不一致时 IsOverride=true。
func TestComputePriceDiff_DetectsOverride(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 1.0, OutputPrice: 2.0,
		CatalogID: &cat.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	d := diffs[0]
	assert.Equal(t, mid, d.ModelID)
	assert.True(t, d.IsOverridden)
	assert.InDelta(t, 1.0, d.LocalInputPrice, 1e-6)
	assert.InDelta(t, 2.5, d.CatalogInputPrice, 1e-6)
	assert.InDelta(t, 2.0, d.LocalOutputPrice, 1e-6)
	assert.InDelta(t, 10.0, d.CatalogOutputPrice, 1e-6)
	// catalog=2.5 vs local=1 → +150%；catalog=10 vs local=2 → +400%
	assert.InDelta(t, 150.0, d.InputDeltaPct, 0.01)
	assert.InDelta(t, 400.0, d.OutputDeltaPct, 0.01)
	assert.Equal(t, "gpt-4o", d.CatalogSourceModelID)
	assert.Nil(t, d.CatalogDeprecationDate)
}

// TestComputePriceDiff_FiltersOutEqual 价格完全一致且无 deprecation 时不应出现在 diff 列表里。
func TestComputePriceDiff_FiltersOutEqual(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 2.5, OutputPrice: 10,
		CatalogID: &cat.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

// TestComputePriceDiff_KeepsDeprecated 价格相同但 catalog 标了废弃时仍要返回，提醒用户。
func TestComputePriceDiff_KeepsDeprecated(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	dep := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	cat.DeprecationDate = &dep
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 2.5, OutputPrice: 10,
		CatalogID: &cat.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	assert.False(t, diffs[0].IsOverridden)
	require.NotNil(t, diffs[0].CatalogDeprecationDate)
	assert.Equal(t, dep, *diffs[0].CatalogDeprecationDate)
}

// TestApplyPriceDiff_BatchUpdate apply 后价格同步到 catalog 的当前值。
func TestApplyPriceDiff_BatchUpdate(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 99, 199)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 0, OutputPrice: 0,
		CatalogID: &cat.ID,
	})

	count, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{mid})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	got, _ := mr.GetByID(mid)
	assert.InDelta(t, 99.0, got.InputPrice, 1e-6)
	assert.InDelta(t, 199.0, got.OutputPrice, 1e-6)
}

// TestApplyPriceDiff_InvalidIDs apply 时无效 ID 应返回错误。
func TestApplyPriceDiff_InvalidIDs(t *testing.T) {
	svc := NewCatalogSyncService(newFakeCatalogRepo(), newFakeCatalogModelRepo(), CatalogSyncOpts{})
	_, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{uuid.New()})
	require.Error(t, err)
}

// TestAutoBind_TailSegmentSingleMatch 严格无匹配但去前缀后唯一命中应自动绑定。
// 例：本地 vanchin/deepseek-chat ↔ catalog deepseek-chat。
func TestAutoBind_TailSegmentSingleMatch(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("deepseek-chat", "deepseek", 131072, 8192, 0.28, 0.42)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "vanchin/deepseek-chat", ProviderID: uuid.New(), ModelType: "chat",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Matched, 1, "去前缀同名且 catalog 仅 1 条应自动绑")
	got, _ := mr.GetByID(mid)
	require.NotNil(t, got.CatalogID)
	assert.Equal(t, cat.ID, *got.CatalogID)
}

// TestAutoBind_TailSegmentAmbiguous 去前缀同名但目录里多 provider 副本时进入候选名单。
// 例：本地 deepseek-r1 ↔ catalog [azure_ai/deepseek-r1, fireworks_ai/deepseek-r1]
func TestAutoBind_TailSegmentAmbiguous(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	c1 := makeCatalogEntry("azure_ai/deepseek-r1", "azure_ai", 128000, 8192, 1, 2)
	c2 := makeCatalogEntry("fireworks_ai/deepseek-r1", "fireworks_ai", 128000, 8192, 1.1, 2.2)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{c1, c2}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "deepseek-r1", ProviderID: uuid.New(), ModelType: "chat",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched, "多候选不写库")
	require.Len(t, res.Ambiguous, 1)
	assert.Len(t, res.Ambiguous[0].Candidates, 2)
}

// TestAutoBind_OneToOneMatch name 大小写不敏感、严格唯一命中应自动绑定。
func TestAutoBind_OneToOneMatch(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "GPT-4O", ProviderID: uuid.New(), ModelType: "chat",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Matched, 1)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 0, res.Unmatched)
	assert.Equal(t, mid, res.Matched[0].ModelID)
	assert.Equal(t, cat.ID, res.Matched[0].CatalogID)

	got, _ := mr.GetByID(mid)
	require.NotNil(t, got.CatalogID)
	assert.Equal(t, cat.ID, *got.CatalogID)
}

// TestAutoBind_AmbiguousNoBind 多个候选时不写库，列出候选。
func TestAutoBind_AmbiguousNoBind(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	// 同名两条 catalog（不同 source）— 这里用 makeCatalogEntry 默认 source="litellm"，
	// 所以构造两条不同 source 才有效；为了不破坏 makeCatalogEntry 签名，这里直接构造 entry。
	now := time.Now()
	c1 := &model.ModelCatalog{
		ID: uuid.New(), Source: "litellm", SourceModelID: "deepseek-r1", Provider: "openai",
		ContextLength: 128000, MaxOutputTokens: 8192, InputPricePerMillion: 1, OutputPricePerMillion: 2,
		SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	c2 := &model.ModelCatalog{
		ID: uuid.New(), Source: "litellm-mirror", SourceModelID: "deepseek-r1", Provider: "azure",
		ContextLength: 128000, MaxOutputTokens: 8192, InputPricePerMillion: 1.1, OutputPricePerMillion: 2.2,
		SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{c1, c2}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "deepseek-r1", ProviderID: uuid.New(), ModelType: "chat",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched, "歧义时不应写库")
	require.Len(t, res.Ambiguous, 1)
	assert.Equal(t, mid, res.Ambiguous[0].ModelID)
	assert.Len(t, res.Ambiguous[0].Candidates, 2)
	assert.Equal(t, 0, res.Unmatched)

	got, _ := mr.GetByID(mid)
	assert.Nil(t, got.CatalogID, "歧义时 catalog_id 应保持 NULL")
}

// TestAutoBind_FallbackToUpstreamModelID name 不匹配时用 upstream_model_id 兜底。
func TestAutoBind_FallbackToUpstreamModelID(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("claude-3-opus-20240229", "anthropic", 200000, 4096, 15, 75)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "我们的-claude-opus", ProviderID: uuid.New(), ModelType: "chat",
		UpstreamModelID: "claude-3-opus-20240229",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Matched, 1)
	assert.Equal(t, cat.ID, res.Matched[0].CatalogID)
}

// TestAutoBind_SkipsAlreadyBound 已绑定的不应被重新处理。
func TestAutoBind_SkipsAlreadyBound(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	otherID := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		CatalogID: &otherID, // 已经绑了别的 catalog
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 0, res.Unmatched, "已绑定的根本不应进入处理流程")

	got, _ := mr.GetByID(mid)
	require.NotNil(t, got.CatalogID)
	assert.Equal(t, otherID, *got.CatalogID, "已绑定的 catalog_id 必须保持原状")
}

// TestAutoBind_NoMatch 没匹配上的计入 unmatched。
func TestAutoBind_NoMatch(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mr.put(&model.Model{
		ID: uuid.New(), Name: "我家私有模型", ProviderID: uuid.New(), ModelType: "chat",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 1, res.Unmatched)
}

// ===================== BindingStats =====================

// TestBindingStats_EmptyEverything 全空时各计数都是 0。
func TestBindingStats_EmptyEverything(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	got, err := svc.BindingStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.CatalogTotal)
	assert.Equal(t, int64(0), got.ModelTotal)
	assert.Equal(t, int64(0), got.BoundCount)
	assert.Equal(t, int64(0), got.UnboundCount)
}

// TestBindingStats_MixedBoundUnbound 同时有已绑/未绑 model 时分别计入。
func TestBindingStats_MixedBoundUnbound(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	// 目录 3 条
	cats := []*model.ModelCatalog{
		makeCatalogEntry("gpt-4o", "openai", 0, 0, 0, 0),
		makeCatalogEntry("claude-3-opus", "anthropic", 0, 0, 0, 0),
		makeCatalogEntry("deepseek-v3", "deepseek", 0, 0, 0, 0),
	}
	require.NoError(t, repo.BatchUpsert(cats))

	// 本地 4 个 model：2 绑定、2 未绑定
	c1 := cats[0].ID
	c2 := cats[1].ID
	mr.put(&model.Model{ID: uuid.New(), Name: "m1", ProviderID: uuid.New(), ModelType: "chat", CatalogID: &c1})
	mr.put(&model.Model{ID: uuid.New(), Name: "m2", ProviderID: uuid.New(), ModelType: "chat", CatalogID: &c2})
	mr.put(&model.Model{ID: uuid.New(), Name: "m3", ProviderID: uuid.New(), ModelType: "chat"})
	mr.put(&model.Model{ID: uuid.New(), Name: "m4", ProviderID: uuid.New(), ModelType: "chat"})

	got, err := svc.BindingStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.CatalogTotal)
	assert.Equal(t, int64(4), got.ModelTotal)
	assert.Equal(t, int64(2), got.BoundCount)
	assert.Equal(t, int64(2), got.UnboundCount)
}

// TestBindingStats_ModelRepoNil 没注入 model repo 时返回明确错误，不要 panic。
func TestBindingStats_ModelRepoNil(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{})

	_, err := svc.BindingStats(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model repo not configured")
}

// 防止未使用 import 的烦恼
var _ = strings.Contains
