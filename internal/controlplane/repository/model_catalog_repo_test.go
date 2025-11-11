package repository

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCatalogEntry(id string) *model.ModelCatalog {
	return &model.ModelCatalog{
		ID:                    uuid.New(),
		Source:                "litellm",
		SourceModelID:         id,
		DisplayName:           id,
		Provider:              "openai",
		Mode:                  "chat",
		ContextLength:         128000,
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.5,
		OutputPricePerMillion: 10,
		SyncedAt:              time.Now(),
	}
}

// TestCatalogRepo_Upsert_InsertNew 验证首次 Upsert 是 INSERT。
func TestCatalogRepo_Upsert_InsertNew(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("gpt-4o-2024-11-20")
	require.NoError(t, repo.Upsert(entry))

	got, err := repo.GetBySourceModelID("litellm", "gpt-4o-2024-11-20")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "openai", got.Provider)
	assert.InDelta(t, 2.5, got.InputPricePerMillion, 1e-6)
}

// TestCatalogRepo_Upsert_UpdateExisting 二次 Upsert 同 (source, source_model_id) 应改写而非插入。
func TestCatalogRepo_Upsert_UpdateExisting(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	first := newCatalogEntry("gpt-4o")
	require.NoError(t, repo.Upsert(first))

	second := newCatalogEntry("gpt-4o")
	second.InputPricePerMillion = 99
	second.OutputPricePerMillion = 199
	require.NoError(t, repo.Upsert(second))

	got, err := repo.GetBySourceModelID("litellm", "gpt-4o")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.InDelta(t, 99.0, got.InputPricePerMillion, 1e-6)
	assert.InDelta(t, 199.0, got.OutputPricePerMillion, 1e-6)

	// 总条目仍然只有 1
	all, total, err := repo.List(CatalogListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, all, 1)
}

// TestCatalogRepo_Search_ByQuery 验证 source_model_id / display_name 模糊匹配。
func TestCatalogRepo_Search_ByQuery(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	require.NoError(t, repo.Upsert(newCatalogEntry("gpt-4o-2024-11-20")))
	require.NoError(t, repo.Upsert(newCatalogEntry("gpt-3.5-turbo")))
	require.NoError(t, repo.Upsert(newCatalogEntry("azure_ai/deepseek-v3.2")))

	// 子串匹配
	results, total, err := repo.Search(CatalogSearchOpts{Query: "gpt-4", Limit: 20})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "gpt-4o-2024-11-20", results[0].SourceModelID)

	// '/' 不影响匹配
	results, total, err = repo.Search(CatalogSearchOpts{Query: "deepseek-v3", Limit: 20})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "azure_ai/deepseek-v3.2", results[0].SourceModelID)
}

// TestCatalogRepo_Search_ByProvider 验证按 provider 过滤。
func TestCatalogRepo_Search_ByProvider(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	openai := newCatalogEntry("gpt-4o")
	openai.Provider = "openai"
	require.NoError(t, repo.Upsert(openai))

	az := newCatalogEntry("azure_ai/deepseek-v3.2")
	az.Provider = "azure_ai"
	require.NoError(t, repo.Upsert(az))

	results, total, err := repo.Search(CatalogSearchOpts{Provider: "azure_ai", Limit: 20})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "azure_ai/deepseek-v3.2", results[0].SourceModelID)
}

// TestCatalogRepo_Search_ByMode 验证按 mode 过滤。
func TestCatalogRepo_Search_ByMode(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	chat := newCatalogEntry("gpt-4o")
	chat.Mode = "chat"
	require.NoError(t, repo.Upsert(chat))

	emb := newCatalogEntry("text-embedding-3-small")
	emb.Mode = "embedding"
	require.NoError(t, repo.Upsert(emb))

	results, total, err := repo.Search(CatalogSearchOpts{Mode: "embedding", Limit: 20})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "text-embedding-3-small", results[0].SourceModelID)
}

// TestCatalogRepo_Search_TotalIsFullCountNotPage 回归用例：
//
// Search 返回的 total 必须是匹配条件下的"全表总数"，而不是当前页 len(results)。
// 历史 bug：handler.Search 把 len(results) 当作 total 返回给前端，
// 前端 searchCatalog({limit:1}) 探目录元信息时永远显示「目录已收录 1 条」，
// 给运维造成"同步没生效"的错觉。
func TestCatalogRepo_Search_TotalIsFullCountNotPage(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Upsert(newCatalogEntry("model-"+uuid.NewString()[:6])))
	}

	results, total, err := repo.Search(CatalogSearchOpts{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, results, 1, "limit=1 时当前页只应返回 1 条")
	assert.Equal(t, int64(5), total, "total 必须反映全表总数，与 limit 无关")
}

// TestCatalogRepo_Count 验证 Count 返回 catalog 表当前条目总数。
//
// 这是 CatalogSyncPage 空状态语义判定（修复 D）的真源：
// 前端用它来区分「目录是空的（同步未生效）」vs「目录有数据只是没绑定」。
// 之所以不复用 Search().total，是因为 Search 路径走全字段扫描 + filter，
// 拿元信息成本太高且容易把 filter 算错。
func TestCatalogRepo_Count(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	n, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "空库时 Count 应为 0")

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Upsert(newCatalogEntry("model-"+uuid.NewString()[:6])))
	}

	n, err = repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

// TestCatalogRepo_List_Pagination 验证 List 支持 offset/limit。
func TestCatalogRepo_List_Pagination(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	for i := 0; i < 7; i++ {
		entry := newCatalogEntry("model-" + uuid.NewString()[:6])
		require.NoError(t, repo.Upsert(entry))
	}

	page1, total, err := repo.List(CatalogListFilter{}, 0, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(7), total)
	assert.Len(t, page1, 3)

	page3, _, err := repo.List(CatalogListFilter{}, 6, 3)
	require.NoError(t, err)
	assert.Len(t, page3, 1)
}

// TestCatalogRepo_GetBySourceModelID_NotFound 验证不存在返回 nil, nil（无错误）。
func TestCatalogRepo_GetBySourceModelID_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	got, err := repo.GetBySourceModelID("litellm", "no-such-model")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestCatalogRepo_GetByID_RoundTrip 验证 GetByID 拿到完整字段。
func TestCatalogRepo_GetByID_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("gpt-4o")
	require.NoError(t, repo.Upsert(entry))

	got, err := repo.GetByID(entry.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "gpt-4o", got.SourceModelID)
}

// TestCatalogRepo_GetByID_NotFound 验证按主键查不存在时返回 nil, nil。
func TestCatalogRepo_GetByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	got, err := repo.GetByID(uuid.New())
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestCatalogRepo_BatchUpsert 批量写入并验证每条都存在。
func TestCatalogRepo_BatchUpsert(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entries := []*model.ModelCatalog{
		newCatalogEntry("gpt-4o"),
		newCatalogEntry("claude-3-opus"),
		newCatalogEntry("deepseek-v3"),
	}
	require.NoError(t, repo.BatchUpsert(entries))

	n, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

// TestCatalogRepo_BatchUpsert_Empty 空列表应直接返回 nil。
func TestCatalogRepo_BatchUpsert_Empty(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	require.NoError(t, repo.BatchUpsert(nil))
}

// TestCatalogRepo_DeleteStale 删除旧条目后总数减少。
func TestCatalogRepo_DeleteStale(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	old := newCatalogEntry("old-model")
	old.SyncedAt = time.Now().Add(-2 * time.Hour)
	require.NoError(t, repo.Upsert(old))

	newEntry := newCatalogEntry("new-model")
	newEntry.SyncedAt = time.Now()
	require.NoError(t, repo.Upsert(newEntry))

	cutoff := time.Now().Add(-1 * time.Hour)
	affected, err := repo.DeleteStale("litellm", cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	n, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "only the new entry should remain")
}

// TestCatalogRepo_ListAll 返回全量 catalog 条目。
func TestCatalogRepo_ListAll(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	require.NoError(t, repo.Upsert(newCatalogEntry("a")))
	require.NoError(t, repo.Upsert(newCatalogEntry("b")))
	require.NoError(t, repo.Upsert(newCatalogEntry("c")))

	all, err := repo.ListAll()
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestCatalogRepo_ListAutoBindCandidates(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("gpt-4o")
	entry.Provider = "openai"
	entry.RawPayload = `{"large":"payload"}`
	require.NoError(t, repo.Upsert(entry))

	all, err := repo.ListAutoBindCandidates()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, entry.ID, all[0].ID)
	assert.Equal(t, "gpt-4o", all[0].SourceModelID)
	assert.Equal(t, "openai", all[0].Provider)
	assert.Empty(t, all[0].RawPayload, "auto-bind candidates should not load raw_payload")
}

// TestCatalogRepo_Search_BySource 验证按 source 过滤。
func TestCatalogRepo_Search_BySource(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	lite := newCatalogEntry("gpt-4o")
	lite.Source = "litellm"
	require.NoError(t, repo.Upsert(lite))

	other := newCatalogEntry("custom-model")
	other.Source = "internal"
	require.NoError(t, repo.Upsert(other))

	results, total, err := repo.Search(CatalogSearchOpts{Source: "litellm", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
	assert.Equal(t, "gpt-4o", results[0].SourceModelID)
}

// TestCatalogRepo_Search_CaseInsensitive 模糊匹配应大小写不敏感。
func TestCatalogRepo_Search_CaseInsensitive(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	require.NoError(t, repo.Upsert(newCatalogEntry("GPT-4O-2024-11-20")))

	results, total, err := repo.Search(CatalogSearchOpts{Query: "gpt-4o", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
}

// TestCatalogRepo_Search_CombinedFilters 多个 filter 组合使用。
func TestCatalogRepo_Search_CombinedFilters(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	openaiChat := newCatalogEntry("gpt-4o")
	openaiChat.Provider = "openai"
	openaiChat.Mode = "chat"
	require.NoError(t, repo.Upsert(openaiChat))

	openaiEmb := newCatalogEntry("text-embedding-3-small")
	openaiEmb.Provider = "openai"
	openaiEmb.Mode = "embedding"
	require.NoError(t, repo.Upsert(openaiEmb))

	anthropicChat := newCatalogEntry("claude-3-opus")
	anthropicChat.Provider = "anthropic"
	anthropicChat.Mode = "chat"
	require.NoError(t, repo.Upsert(anthropicChat))

	results, total, err := repo.Search(CatalogSearchOpts{Provider: "openai", Mode: "chat", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
	assert.Equal(t, "gpt-4o", results[0].SourceModelID)
}

// TestCatalogRepo_Search_EmptyQueryReturnsAll 空 query 不做模糊匹配，返回全量（受 limit 控制）。
func TestCatalogRepo_Search_EmptyQueryReturnsAll(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Upsert(newCatalogEntry("model-"+uuid.NewString()[:6])))
	}

	results, total, err := repo.Search(CatalogSearchOpts{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, results, 3)
}

// TestCatalogRepo_Search_DisplayNameFuzzy display_name 也能模糊匹配。
func TestCatalogRepo_Search_DisplayNameFuzzy(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("gpt-4o")
	entry.DisplayName = "GPT-4o Turbo Model"
	require.NoError(t, repo.Upsert(entry))

	results, total, err := repo.Search(CatalogSearchOpts{Query: "turbo", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
}

func TestCatalogRepo_Upsert_AutoGenerateID(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("auto-id-model")
	entry.ID = uuid.Nil
	require.NoError(t, repo.Upsert(entry))

	got, err := repo.GetBySourceModelID("litellm", "auto-id-model")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotEqual(t, uuid.Nil, got.ID, "ID should be auto-generated when nil")
}

func TestCatalogRepo_Upsert_DefaultSource(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("no-source-model")
	entry.Source = ""
	require.NoError(t, repo.Upsert(entry))

	got, err := repo.GetBySourceModelID("litellm", "no-source-model")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "litellm", got.Source, "empty source should default to litellm")
}

func TestCatalogRepo_Upsert_DefaultSyncedAt(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	entry := newCatalogEntry("no-syncedat-model")
	entry.SyncedAt = time.Time{}
	before := time.Now()
	require.NoError(t, repo.Upsert(entry))

	got, err := repo.GetBySourceModelID("litellm", "no-syncedat-model")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.SyncedAt.After(before.Add(-5*time.Second)), "zero synced_at should be auto-set to now")
}

func TestCatalogRepo_Search_LimitBounds(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	for i := 0; i < 25; i++ {
		require.NoError(t, repo.Upsert(newCatalogEntry("model-"+uuid.NewString()[:6])))
	}

	results, total, err := repo.Search(CatalogSearchOpts{Limit: 0})
	require.NoError(t, err)
	assert.Len(t, results, 20, "limit=0 should default to 20")
	assert.Equal(t, int64(25), total)

	results, total, err = repo.Search(CatalogSearchOpts{Limit: 250})
	require.NoError(t, err)
	assert.Len(t, results, 20, "limit>200 should default to 20")
	assert.Equal(t, int64(25), total)
}

func TestCatalogRepo_Search_OrderByCreatedAtDesc(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	require.NoError(t, repo.Upsert(newCatalogEntry("first")))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, repo.Upsert(newCatalogEntry("second")))

	results, _, err := repo.Search(CatalogSearchOpts{Limit: 20})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "second", results[0].SourceModelID, "newest should come first")
	assert.Equal(t, "first", results[1].SourceModelID)
}

func TestCatalogRepo_List_DefaultLimit(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelCatalogRepo(db)

	for i := 0; i < 60; i++ {
		require.NoError(t, repo.Upsert(newCatalogEntry("model-"+uuid.NewString()[:6])))
	}

	results, total, err := repo.List(CatalogListFilter{}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(60), total)
	assert.Len(t, results, 50, "limit=0 should default to 50")
}
