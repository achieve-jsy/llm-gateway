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

func (f *fakeCatalogRepo) ListAutoBindCandidates() ([]model.ModelCatalog, error) {
	return f.ListAll()
}

func (f *fakeCatalogRepo) Count() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.entries)), nil
}

func (f *fakeCatalogRepo) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := map[uuid.UUID]*model.ModelCatalog{}
	for _, id := range ids {
		for _, e := range f.entries {
			if e.ID == id {
				c := *e
				result[id] = &c
				break
			}
		}
	}
	return result, nil
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

func (f *fakeCatalogModelRepo) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := map[uuid.UUID]*model.Model{}
	for _, id := range ids {
		if m, ok := f.models[id]; ok {
			c := *m
			result[id] = &c
		}
	}
	return result, nil
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

// TestComputePriceDiff_PriceUnsetIsNotOverridden 本地价格 $0/$0（从未设置）时，
// is_overridden 必须为 false，不能把"价格未设"误标记为"已脱离目录"。
//
// 历史 bug：ComputePriceDiff 把 priceMismatch 直接赋给 IsOverridden，
// 导致 local=$0、catalog>0 的 model 全部显示"已脱离目录"标签，
// 造成 admin 以为这些 model 是主动背离目录价格的，实际上只是从未填过价格。
func TestComputePriceDiff_PriceUnsetIsNotOverridden(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("qwen-turbo-latest", "dashscope", 128000, 16384, 0.05, 0.20)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	// 本地价格 $0/$0 ← 注册模型时从未填过价格
	mr.put(&model.Model{
		ID: mid, Name: "qwen-turbo-latest", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice:  0,
		OutputPrice: 0,
		CatalogID:   &cat.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	require.Len(t, diffs, 1, "价格未设的 model 仍应出现在 diff 列表（提醒 admin 去设置）")
	d := diffs[0]
	// 核心断言：is_overridden 必须是 false
	assert.False(t, d.IsOverridden,
		"local=$0 表示从未设置价格，不是 admin 主动背离目录，不能标记为 is_overridden=true")
	// 顺带验证价格字段值正确透传
	assert.InDelta(t, 0, d.LocalInputPrice, 1e-9)
	assert.InDelta(t, 0.05, d.CatalogInputPrice, 1e-9)
	assert.InDelta(t, 0, d.LocalOutputPrice, 1e-9)
	assert.InDelta(t, 0.20, d.CatalogOutputPrice, 1e-9)
}

// TestComputePriceDiff_PriceSetAndDifferentIsOverridden admin 之前 apply 过一次价格
// （local > 0），但后来 catalog 更新了，这才是真正的"已脱离目录"。
func TestComputePriceDiff_PriceSetAndDifferentIsOverridden(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o-mini", "openai", 128000, 16384, 0.15, 0.60)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	// admin 之前 apply 了旧价格，catalog 后来更新了，本地没有跟上
	mr.put(&model.Model{
		ID: mid, Name: "gpt-4o-mini", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice:  0.10, // 旧价格，现在 catalog 是 0.15
		OutputPrice: 0.40, // 旧价格，现在 catalog 是 0.60
		CatalogID:   &cat.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	d := diffs[0]
	// local > 0 且与 catalog 不同 → 这才是真正的"已脱离目录"
	assert.True(t, d.IsOverridden,
		"local>0 且与 catalog 不同，说明 admin 曾经 apply 过价格但 catalog 又更新了，应标记为 is_overridden=true")
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

// ===================== SyncStatus =====================

// TestSyncStatus_InitiallyEmpty 从未跑过同步时，时间字段应为 nil（前端能干净判定）。
func TestSyncStatus_InitiallyEmpty(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{})

	st, err := svc.SyncStatus(context.Background())
	require.NoError(t, err)
	assert.False(t, st.Running)
	assert.Nil(t, st.LastStartedAt, "从未跑过时 LastStartedAt 必须是 nil")
	assert.Nil(t, st.LastFinishedAt)
	assert.Equal(t, int64(0), st.LastDurationMs)
	assert.Equal(t, "", st.LastSource)
	assert.Equal(t, 0, st.LastCount)
	assert.Equal(t, "", st.LastError)
	assert.Equal(t, int64(0), st.CatalogTotal)
}

// TestSyncStatus_AfterRemoteSuccess 远程同步成功后，状态字段应反映 "remote" 来源 + 条目数。
func TestSyncStatus_AfterRemoteSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPTimeout: 2 * time.Second,
	})

	beforeStart := time.Now()
	n, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)
	require.Greater(t, n, 0)

	st, err := svc.SyncStatus(context.Background())
	require.NoError(t, err)
	assert.False(t, st.Running, "同步已结束，Running 必须翻为 false")
	require.NotNil(t, st.LastStartedAt)
	require.NotNil(t, st.LastFinishedAt)
	assert.False(t, st.LastStartedAt.Before(beforeStart.Add(-time.Second)),
		"LastStartedAt 必须落在最近一次调用之内")
	assert.GreaterOrEqual(t, st.LastDurationMs, int64(0))
	assert.Equal(t, "remote", st.LastSource)
	assert.Equal(t, n, st.LastCount)
	assert.Equal(t, "", st.LastError)
	assert.Equal(t, int64(n), st.CatalogTotal,
		"BatchUpsert 后 CatalogTotal 应等于这次写入条目数")
}

// TestSyncStatus_AfterFallback 远程失败、走 bundled 兜底也算成功，但 LastSource = "bundled"
// 让 admin 看到"这次走的是回退链路"。
func TestSyncStatus_AfterFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:    ts.URL,
		HTTPTimeout:  500 * time.Millisecond,
		BundledBytes: []byte(minimalRemoteJSON),
	})

	n, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)
	require.Greater(t, n, 0)

	st, err := svc.SyncStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bundled", st.LastSource, "回退到 bundled 必须明确告诉前端，不能伪装成 remote")
	assert.Equal(t, "", st.LastError, "回退成功不应残留 error 字段，避免 UI 误报")
	assert.Equal(t, n, st.LastCount)
}

// TestSyncStatus_AfterTotalFailure 远程失败 + 无 bundled，必须返回 err 并把 LastError 写入。
func TestSyncStatus_AfterTotalFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:    ts.URL,
		HTTPTimeout:  500 * time.Millisecond,
		BundledBytes: []byte{}, // 显式无兜底
	})

	_, err := svc.SyncFromRemote(context.Background())
	require.Error(t, err)

	st, err := svc.SyncStatus(context.Background())
	require.NoError(t, err)
	assert.False(t, st.Running)
	assert.Equal(t, "", st.LastSource, "彻底失败时 LastSource 不应是 remote 也不应是 bundled")
	assert.NotEmpty(t, st.LastError, "彻底失败必须把错误写进 LastError 让前端能展示")
	assert.Equal(t, 0, st.LastCount)
}

// TestSyncStatus_ConcurrentTriggerIsRejected 两次并发 SyncFromRemote，第二次必须立刻拒绝，
// 否则会出现 last* 字段被竞争写坏的情况。
func TestSyncStatus_ConcurrentTriggerIsRejected(t *testing.T) {
	// 用一个会卡 200ms 的 server 模拟"上次还没跑完"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPTimeout: 2 * time.Second,
	})

	done := make(chan error, 1)
	go func() {
		_, e := svc.SyncFromRemote(context.Background())
		done <- e
	}()

	// 给第一个 goroutine 一点时间进到 syncing=true
	time.Sleep(30 * time.Millisecond)
	_, err := svc.SyncFromRemote(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")

	require.NoError(t, <-done)
}

// TestSyncStatus_RepoCountError 当 repo.Count 失败时，SyncStatus 必须把错误冒出来，
// 不能静默把 CatalogTotal 写成 0 让用户以为"目录是空的"。
func TestSyncStatus_RepoCountError(t *testing.T) {
	repo := &countErrCatalogRepo{fakeCatalogRepo: newFakeCatalogRepo()}
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{})

	_, err := svc.SyncStatus(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count catalog for sync status")
}

// countErrCatalogRepo: Count 永远报错，其余继承 fakeCatalogRepo
type countErrCatalogRepo struct{ *fakeCatalogRepo }

func (r *countErrCatalogRepo) Count() (int64, error) {
	return 0, errors.New("db down")
}

// 防止未使用 import 的烦恼
var _ = strings.Contains

// ===================== parsedToModel =====================

func TestParsedToModel_FieldsCopied(t *testing.T) {
	p := &ModelCatalogParsed{
		SourceModelID:           "gpt-4o",
		Provider:                "openai",
		Mode:                    "chat",
		DisplayName:             "gpt-4o",
		ContextLength:           128000,
		MaxOutputTokens:         16384,
		InputPricePerMillion:    2.5,
		OutputPricePerMillion:   10,
		SupportsFunctionCalling: true,
		SupportsVision:          true,
		RawPayload:              "{}",
	}
	now := time.Now()
	m := parsedToModel(p, "litellm", now)
	assert.NotEqual(t, uuid.Nil, m.ID, "ID should be auto-generated")
	assert.Equal(t, "litellm", m.Source)
	assert.Equal(t, "gpt-4o", m.SourceModelID)
	assert.Equal(t, "openai", m.Provider)
	assert.Equal(t, int64(128000), m.ContextLength)
	assert.Equal(t, int64(16384), m.MaxOutputTokens)
	assert.InDelta(t, 2.5, m.InputPricePerMillion, 1e-6)
	assert.True(t, m.SupportsFunctionCalling)
	assert.True(t, m.SupportsVision)
	assert.Equal(t, now, m.SyncedAt)
}

func TestParsedToModel_TierPricing(t *testing.T) {
	threshold := int64(200000)
	inputAbove := 6.0
	outputAbove := 30.0
	p := &ModelCatalogParsed{
		SourceModelID:       "claude-3-5-sonnet-200k",
		TierThreshold:       &threshold,
		InputPriceAboveTier: &inputAbove,
		OutputPriceAboveTier: &outputAbove,
	}
	m := parsedToModel(p, "litellm", time.Now())
	require.NotNil(t, m.TierThreshold)
	assert.Equal(t, int64(200000), *m.TierThreshold)
	require.NotNil(t, m.InputPriceAboveTier)
	assert.InDelta(t, 6.0, *m.InputPriceAboveTier, 1e-6)
}

// ===================== deltaPct / floatEqual =====================

func TestDeltaPct_FromZero(t *testing.T) {
	assert.Equal(t, 0.0, deltaPct(0, 10))
	assert.Equal(t, 0.0, deltaPct(0, 0))
}

func TestDeltaPct_Normal(t *testing.T) {
	assert.InDelta(t, 150.0, deltaPct(1, 2.5), 0.01)
	assert.InDelta(t, -50.0, deltaPct(2, 1), 0.01)
}

func TestFloatEqual(t *testing.T) {
	assert.True(t, floatEqual(1.0, 1.0))
	assert.True(t, floatEqual(1.0, 1.0+1e-10))
	assert.False(t, floatEqual(1.0, 1.01))
}

// ===================== lastSegment =====================

func TestLastSegment(t *testing.T) {
	assert.Equal(t, "deepseek-r1", lastSegment("azure_ai/deepseek-r1"))
	assert.Equal(t, "kimi-k2.5", lastSegment("baseten/moonshotai/Kimi-K2.5"))
	assert.Equal(t, "deepseek-chat", lastSegment("deepseek-chat"))
	assert.Equal(t, "gpt-4o", lastSegment("  GPT-4O  "))
}

// ===================== SyncFromBundled empty =====================

func TestCatalogSync_SyncFromBundled_EmptyBytes(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{BundledBytes: []byte{}})
	_, err := svc.SyncFromBundled()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no bundled")
}

// ===================== ApplyPriceDiff edge cases =====================

func TestApplyPriceDiff_NoModelRepo(t *testing.T) {
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	_, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{uuid.New()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model repo not configured")
}

func TestApplyPriceDiff_NoCatalogLink(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	mid := uuid.New()
	mr.put(&model.Model{ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat"})

	_, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{mid})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog link")
}

func TestApplyPriceDiff_EmptyIDs(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})
	_, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no model ids provided")
}

// ===================== ApplyAutoUpdates nil modelRepo =====================

func TestApplyAutoUpdates_NoModelRepo(t *testing.T) {
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	_, err := svc.ApplyAutoUpdates(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model repo not configured")
}

// ===================== ComputePriceDiff nil modelRepo =====================

func TestComputePriceDiff_NoModelRepo(t *testing.T) {
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	_, err := svc.ComputePriceDiff(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model repo not configured")
}

// ===================== AutoBind nil modelRepo =====================

func TestAutoBind_NoModelRepo(t *testing.T) {
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	_, err := svc.AutoBind(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model repo not configured")
}

// ===================== AutoBind SetCatalogID failure =====================

func TestAutoBind_SetCatalogIDFailure_FallsToUnmatched(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := &setCatalogIDFailRepo{fakeCatalogModelRepo: newFakeCatalogModelRepo()}
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{ID: mid, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat"})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched, "SetCatalogID fail should not add to matched")
	assert.Equal(t, 1, res.Unmatched, "should fall into unmatched when SetCatalogID fails")
}

type setCatalogIDFailRepo struct{ *fakeCatalogModelRepo }

func (r *setCatalogIDFailRepo) SetCatalogID(_, _ uuid.UUID) error {
	return errors.New("write failed")
}

// ===================== applyBytes invalid JSON =====================

func TestCatalogSync_ApplyBytes_InvalidJSON(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{})

	_, err := svc.applyBytes([]byte("not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode catalog payload")
}

// ===================== applyBytes tolerant parsing =====================

func TestCatalogSync_ApplyBytes_TolerantParsing(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{})

	payload := `{
  "bad-entry": {"max_input_tokens": "string-not-number", "mode": "chat", "litellm_provider": "unknown"},
  "good-entry": {"max_input_tokens": 32000, "mode": "chat", "litellm_provider": "openai", "input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06}
}`
	count, err := svc.applyBytes([]byte(payload))
	require.NoError(t, err)
	assert.Equal(t, 2, count, "parser tolerates bad fields, both entries get through")
	got, _ := repo.GetBySourceModelID("litellm", "good-entry")
	require.NotNil(t, got)
	assert.Equal(t, int64(32000), got.ContextLength)
}

func TestCatalogSync_FetchRemote_RetryOnTransientError(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			http.Error(w, "temp error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPTimeout: 2 * time.Second,
		MaxRetries:  2,
	})

	count, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)
	assert.Greater(t, count, 0)
	assert.Equal(t, 3, callCount, "should retry 2 extra times (total 3 calls)")
}

func TestCatalogSync_IsSyncing_ReflectsState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPTimeout: 2 * time.Second,
	})

	assert.False(t, svc.IsSyncing(), "initially not syncing")

	done := make(chan struct{})
	go func() {
		_, _ = svc.SyncFromRemote(context.Background())
		done <- struct{}{}
	}()

	time.Sleep(30 * time.Millisecond)
	assert.True(t, svc.IsSyncing(), "should be syncing during remote fetch")

	<-done
	assert.False(t, svc.IsSyncing(), "should not be syncing after completion")
}

func TestCatalogSync_HTTPClientInjection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	repo := newFakeCatalogRepo()
	customClient := &http.Client{Timeout: 5 * time.Second}
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		RemoteURL:   ts.URL,
		HTTPClient:  customClient,
		HTTPTimeout: 2 * time.Second,
	})

	count, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)
	assert.Greater(t, count, 0)
}

func TestAutoBind_UpstreamModelIDTailSegment(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("anthropic/claude-3-opus", "anthropic", 200000, 4096, 15, 75)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "my-claude-opus", ProviderID: uuid.New(), ModelType: "chat",
		UpstreamModelID: "vanchin/claude-3-opus",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Matched, 1, "tail segment match on upstream_model_id should bind")
	assert.Equal(t, cat.ID, res.Matched[0].CatalogID)
}

func TestAutoBind_EmptyNameAndUpstream(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat}))

	mr.put(&model.Model{
		ID: uuid.New(), Name: "", ProviderID: uuid.New(), ModelType: "chat",
		UpstreamModelID: "",
	})

	res, err := svc.AutoBind(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Matched)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 1, res.Unmatched, "model with empty name and upstream_model_id should be unmatched")
}

func TestApplyAutoUpdates_MultipleModels(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat1 := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	cat2 := makeCatalogEntry("claude-3-opus", "anthropic", 200000, 4096, 15, 75)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat1, cat2}))

	mid1 := uuid.New()
	mid2 := uuid.New()
	mr.put(&model.Model{
		ID: mid1, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		ContextLength: 8000, MaxOutputTokens: 1024,
		CatalogID: &cat1.ID,
	})
	mr.put(&model.Model{
		ID: mid2, Name: "claude-3-opus", ProviderID: uuid.New(), ModelType: "chat",
		ContextLength: 5000, MaxOutputTokens: 500,
		CatalogID: &cat2.ID,
	})

	count, err := svc.ApplyAutoUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count, "both models should be updated")

	got1, _ := mr.GetByID(mid1)
	assert.Equal(t, 128000, got1.ContextLength)
	got2, _ := mr.GetByID(mid2)
	assert.Equal(t, 200000, got2.ContextLength)
}

func TestComputePriceDiff_MultipleModels(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cat1 := makeCatalogEntry("gpt-4o", "openai", 128000, 16384, 2.5, 10)
	cat2 := makeCatalogEntry("claude-3-opus", "anthropic", 200000, 4096, 15, 75)
	require.NoError(t, repo.BatchUpsert([]*model.ModelCatalog{cat1, cat2}))

	mid1 := uuid.New()
	mid2 := uuid.New()
	mr.put(&model.Model{
		ID: mid1, Name: "gpt-4o", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 1, OutputPrice: 5,
		CatalogID: &cat1.ID,
	})
	mr.put(&model.Model{
		ID: mid2, Name: "claude-3-opus", ProviderID: uuid.New(), ModelType: "chat",
		InputPrice: 15, OutputPrice: 75,
		CatalogID: &cat2.ID,
	})

	diffs, err := svc.ComputePriceDiff(context.Background())
	require.NoError(t, err)
	require.Len(t, diffs, 1, "only mismatched model should appear in diff")
	assert.Equal(t, mid1, diffs[0].ModelID)
	assert.True(t, diffs[0].IsOverridden)
}

func TestApplyPriceDiff_CatalogNotFound(t *testing.T) {
	repo := newFakeCatalogRepo()
	mr := newFakeCatalogModelRepo()
	svc := NewCatalogSyncService(repo, mr, CatalogSyncOpts{})

	cid := uuid.New()
	mid := uuid.New()
	mr.put(&model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat",
		CatalogID: &cid,
	})

	_, err := svc.ApplyPriceDiff(context.Background(), []uuid.UUID{mid})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog for model")
}

func TestCatalogSync_SyncFromBundled_InvalidJSON(t *testing.T) {
	repo := newFakeCatalogRepo()
	svc := NewCatalogSyncService(repo, nil, CatalogSyncOpts{
		BundledBytes: []byte("not-json"),
	})
	_, err := svc.SyncFromBundled()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode catalog payload")
}
