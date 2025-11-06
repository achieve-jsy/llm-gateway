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
