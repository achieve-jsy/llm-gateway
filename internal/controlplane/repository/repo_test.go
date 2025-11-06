package repository

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB 启动 sqlite in-memory，手工创建表结构。
// 生产 DDL 使用 Postgres 特性（gen_random_uuid、jsonb）；sqlite 测试用简化 schema。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:repo-" + uuid.NewString() + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	statements := []string{
		`CREATE TABLE providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			definition_id TEXT,
			type TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			encrypted_credentials TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			weight INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			status INTEGER NOT NULL DEFAULT 1,
			test_model TEXT DEFAULT '',
			response_time INTEGER DEFAULT 0,
			balance REAL DEFAULT 0,
			remark TEXT DEFAULT '',
			max_retries INTEGER NOT NULL DEFAULT 3,
			timeout INTEGER NOT NULL DEFAULT 30,
			tag TEXT DEFAULT '',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			last_check_error TEXT DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			provider_id TEXT NOT NULL,
			model_type TEXT NOT NULL DEFAULT 'chat',
			enabled INTEGER NOT NULL DEFAULT 1,
			upstream_model_id TEXT DEFAULT '',
			description TEXT DEFAULT '',
			context_length INTEGER DEFAULT 0,
			max_output_tokens INTEGER DEFAULT 0,
			input_price REAL DEFAULT 0,
			output_price REAL DEFAULT 0,
			weight INTEGER NOT NULL DEFAULT 1,
			catalog_id TEXT,
			ctx_overridden INTEGER NOT NULL DEFAULT 0,
			max_output_overridden INTEGER NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			last_check_error TEXT DEFAULT '',
			response_time INTEGER DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE model_catalog (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			source_model_id TEXT NOT NULL,
			display_name TEXT,
			provider TEXT,
			mode TEXT,
			context_length INTEGER DEFAULT 0,
			max_output_tokens INTEGER DEFAULT 0,
			input_price_per_million REAL DEFAULT 0,
			output_price_per_million REAL DEFAULT 0,
			cache_read_price REAL,
			cache_write_price REAL,
			tier_threshold INTEGER,
			input_price_above_tier REAL,
			output_price_above_tier REAL,
			supports_function_calling INTEGER DEFAULT 0,
			supports_vision INTEGER DEFAULT 0,
			supports_prompt_caching INTEGER DEFAULT 0,
			supports_response_schema INTEGER DEFAULT 0,
			supports_tool_choice INTEGER DEFAULT 0,
			supports_parallel_function_calling INTEGER DEFAULT 0,
			deprecation_date DATETIME,
			raw_payload TEXT,
			synced_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(source, source_model_id)
		)`,
		`CREATE TABLE provider_definitions (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			adapter TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT '',
			credential_schema TEXT NOT NULL DEFAULT '{}',
			vendor_options TEXT NOT NULL DEFAULT '{}',
			description TEXT NOT NULL DEFAULT '',
			icon_url TEXT DEFAULT '',
			doc_url TEXT DEFAULT '',
			default_models TEXT NOT NULL DEFAULT '[]',
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE model_tags (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL DEFAULT '#2BA8A2',
			description TEXT DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE model_tag_relations (
			model_id TEXT NOT NULL,
			tag_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY (model_id, tag_id)
		)`,
	}
	for _, s := range statements {
		require.NoError(t, db.Exec(s).Error)
	}

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	_ = model.Provider{}
	return db
}

func newProvider(name string) *model.Provider {
	return &model.Provider{
		ID:                   uuid.New(),
		Name:                 name,
		DefinitionID:         "openai",
		Type:                 "openai",
		Endpoint:             "https://api.openai.com/v1",
		EncryptedCredentials: "enc",
		Enabled:              true,
	}
}

func newModel(name string, providerID uuid.UUID, enabled bool) *model.Model {
	e := enabled
	return &model.Model{
		ID:         uuid.New(),
		Name:       name,
		ProviderID: providerID,
		ModelType:  "chat",
		Enabled:    &e,
	}
}

// ============ ProviderRepo ============

func TestProviderRepo_CRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewProviderRepo(db)

	p := newProvider("p1")
	require.NoError(t, repo.Create(p))

	got, err := repo.GetByID(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "p1", got.Name)

	// List
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(newProvider("more-"+uuid.New().String()[:6])))
	}
	list, total, err := repo.List(ProviderListFilter{}, 0, 10)
	require.NoError(t, err)
	assert.True(t, total >= 4)
	assert.True(t, len(list) >= 4)

	// Update
	p.Name = "p1-renamed"
	require.NoError(t, repo.Update(p))
	got, _ = repo.GetByID(p.ID)
	assert.Equal(t, "p1-renamed", got.Name)

	// UpdateStatus + UpdateResponseTime
	require.NoError(t, repo.UpdateStatus(p.ID, false))
	require.NoError(t, repo.UpdateResponseTime(p.ID, 123))
	got, _ = repo.GetByID(p.ID)
	assert.False(t, got.Enabled)
	assert.Equal(t, 123, got.ResponseTime)

	// Exists
	exists, err := repo.ExistsByName("p1-renamed")
	require.NoError(t, err)
	assert.True(t, exists)
	exists, _ = repo.ExistsByName("missing")
	assert.False(t, exists)
	exists, err = repo.ExistsByNameExcluding("p1-renamed", p.ID)
	require.NoError(t, err)
	assert.False(t, exists) // 排除自身
	exists, err = repo.ExistsByNameExcluding("p1-renamed", uuid.New())
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete + GetByID 应失败
	require.NoError(t, repo.Delete(p.ID))
	_, err = repo.GetByID(p.ID)
	assert.Error(t, err)
}

// ============ ModelRepo ============

func TestModelRepo_CRUD(t *testing.T) {
	db := newTestDB(t)
	providerRepo := NewProviderRepo(db)
	modelRepo := NewModelRepo(db)

	p := newProvider("p-models")
	require.NoError(t, providerRepo.Create(p))

	m := newModel("gpt-4", p.ID, true)
	require.NoError(t, modelRepo.Create(m))
	// ModelRepo.Create 仅 Select 部分字段，依赖数据库默认主键；
	// SQLite 测试需要把 ID 写回，便于后续 GetByID。
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", "gpt-4").Update("id", m.ID).Error)

	got, err := modelRepo.GetByID(m.ID)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", got.Name)
	assert.Equal(t, p.Name, got.Provider.Name)

	// ExistsByName
	exists, _ := modelRepo.ExistsByName("gpt-4")
	assert.True(t, exists)
	exists, _ = modelRepo.ExistsByName("none")
	assert.False(t, exists)
	exists, _ = modelRepo.ExistsByNameExcluding("gpt-4", m.ID)
	assert.False(t, exists)
	exists, _ = modelRepo.ExistsByNameExcluding("gpt-4", uuid.New())
	assert.True(t, exists)

	// List with providerID
	pid := p.ID
	list, total, err := modelRepo.List(ModelListFilter{ProviderID: &pid}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)

	// List全部
	list, total, err = modelRepo.List(ModelListFilter{}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)

	// List with enabled filter
	enabledTrue := true
	list, total, err = modelRepo.List(ModelListFilter{Enabled: &enabledTrue}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)

	enabledFalse := false
	list, total, err = modelRepo.List(ModelListFilter{Enabled: &enabledFalse}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, list, 0)

	// CountByProviderID
	count, err := modelRepo.CountByProviderID(p.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// ListAvailable
	avail, err := modelRepo.ListAvailable()
	require.NoError(t, err)
	assert.Len(t, avail, 1)

	// Update
	m.Name = "gpt-4o"
	require.NoError(t, modelRepo.Update(m))
	got, _ = modelRepo.GetByID(m.ID)
	assert.Equal(t, "gpt-4o", got.Name)

	// UpdateEnabledByProviderID
	require.NoError(t, modelRepo.UpdateEnabledByProviderID(p.ID, false))
	got, _ = modelRepo.GetByID(m.ID)
	assert.NotNil(t, got.Enabled)
	assert.False(t, *got.Enabled)

	// Provider disabled -> ListAvailable 应为空
	require.NoError(t, providerRepo.UpdateStatus(p.ID, false))
	require.NoError(t, modelRepo.UpdateEnabledByProviderID(p.ID, true))
	avail, _ = modelRepo.ListAvailable()
	assert.Empty(t, avail)

	// Delete
	require.NoError(t, modelRepo.Delete(m.ID))
	_, err = modelRepo.GetByID(m.ID)
	assert.Error(t, err)
}

func TestModelRepo_Update_PreservesCatalogID(t *testing.T) {
	db := newTestDB(t)
	providerRepo := NewProviderRepo(db)
	modelRepo := NewModelRepo(db)
	catalogRepo := NewModelCatalogRepo(db)

	p := newProvider("p-catalog-test")
	require.NoError(t, providerRepo.Create(p))

	cat := &model.ModelCatalog{
		ID:                    uuid.New(),
		Source:                "litellm",
		SourceModelID:         "gpt-4o",
		Provider:              "openai",
		Mode:                  "chat",
		ContextLength:         128000,
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.5,
		OutputPricePerMillion: 10,
		SyncedAt:              time.Now(),
	}
	require.NoError(t, catalogRepo.Upsert(cat))

	enabled := true
	m := &model.Model{
		ID:          uuid.New(),
		Name:        "gpt-4o-test",
		ProviderID:  p.ID,
		ModelType:   "chat",
		Enabled:     &enabled,
		CatalogID:   &cat.ID,
		InputPrice:  2.5,
		OutputPrice: 10,
	}
	require.NoError(t, modelRepo.Create(m))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", "gpt-4o-test").Update("id", m.ID).Error)

	bound, err := modelRepo.ListByCatalogID()
	require.NoError(t, err)
	require.Len(t, bound, 1, "model should appear in ListByCatalogID after binding")

	got, err := modelRepo.GetByID(m.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CatalogID, "GetByID should return catalog_id")

	got.InputPrice = 99
	require.NoError(t, modelRepo.Update(got))

	bound2, err := modelRepo.ListByCatalogID()
	require.NoError(t, err)
	assert.Len(t, bound2, 1, "catalog_id must NOT be nullified by Update(Save)")

	got2, err := modelRepo.GetByID(m.ID)
	require.NoError(t, err)
	require.NotNil(t, got2.CatalogID, "Update must preserve catalog_id")
	assert.Equal(t, cat.ID, *got2.CatalogID)
	assert.InDelta(t, 99.0, got2.InputPrice, 1e-6)
}

func TestModelRepo_List_SortsByCreatedAtDesc(t *testing.T) {
	db := newTestDB(t)
	providerRepo := NewProviderRepo(db)
	modelRepo := NewModelRepo(db)

	p := newProvider("p-sort")
	require.NoError(t, providerRepo.Create(p))

	oldEnabled := newModel("enabled-old", p.ID, true)
	newEnabled := newModel("enabled-new", p.ID, true)
	disabledModel := newModel("disabled-newest", p.ID, false)

	require.NoError(t, modelRepo.Create(oldEnabled))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", oldEnabled.Name).Update("id", oldEnabled.ID).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", oldEnabled.ID).Update("created_at", time.Now().Add(-2*time.Hour)).Error)

	require.NoError(t, modelRepo.Create(newEnabled))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", newEnabled.Name).Update("id", newEnabled.ID).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", newEnabled.ID).Update("created_at", time.Now().Add(-1*time.Hour)).Error)

	require.NoError(t, modelRepo.Create(disabledModel))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", disabledModel.Name).Update("id", disabledModel.ID).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", disabledModel.ID).Update("created_at", time.Now()).Error)

	list, total, err := modelRepo.List(ModelListFilter{}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, list, 3)
	assert.Equal(t, "disabled-newest", list[0].Name)
	assert.Equal(t, "enabled-new", list[1].Name)
	assert.Equal(t, "enabled-old", list[2].Name)
	if assert.NotNil(t, list[0].Enabled) {
		assert.False(t, *list[0].Enabled)
	}
}

func TestModelRepo_List_SortsByNameWhenCreatedAtMatches(t *testing.T) {
	db := newTestDB(t)
	providerRepo := NewProviderRepo(db)
	modelRepo := NewModelRepo(db)

	p := newProvider("p-same-created-at")
	require.NoError(t, providerRepo.Create(p))

	commonTime := time.Now().Add(-time.Hour)
	bModel := newModel("b-model", p.ID, true)
	aModel := newModel("a-model", p.ID, true)

	require.NoError(t, modelRepo.Create(bModel))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", bModel.Name).Update("id", bModel.ID).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", bModel.ID).Update("created_at", commonTime).Error)

	require.NoError(t, modelRepo.Create(aModel))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", aModel.Name).Update("id", aModel.ID).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", aModel.ID).Update("created_at", commonTime).Error)

	list, total, err := modelRepo.List(ModelListFilter{}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, list, 2)
	assert.Equal(t, "a-model", list[0].Name)
	assert.Equal(t, "b-model", list[1].Name)
}

// ============ ProviderDefinitionRepo ============

func TestProviderDefinitionRepo_CRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewProviderDefinitionRepo(db)

	def := &model.ProviderDefinition{
		ID:               "openai",
		DisplayName:      "OpenAI",
		Adapter:          "openai",
		BaseURL:          "https://api.openai.com/v1",
		CredentialSchema: model.CredentialSchema{"api_key": {Type: "string", Required: true}},
		VendorOptions:    model.VendorOptions{URLPattern: "standard"},
		DefaultModels:    []string{"gpt-4"},
		Builtin:          true,
		Enabled:          true,
	}
	require.NoError(t, repo.Create(def))

	got, err := repo.GetByID("openai")
	require.NoError(t, err)
	assert.Equal(t, "OpenAI", got.DisplayName)
	assert.True(t, got.Builtin)

	// ExistsByID
	exists, err := repo.ExistsByID("openai")
	require.NoError(t, err)
	assert.True(t, exists)
	exists, _ = repo.ExistsByID("missing")
	assert.False(t, exists)

	// List
	list, err := repo.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// Update
	got.DisplayName = "OpenAI Renamed"
	require.NoError(t, repo.Update(got))
	again, _ := repo.GetByID("openai")
	assert.Equal(t, "OpenAI Renamed", again.DisplayName)

	// Delete builtin 不允许（builtin=true 时 affected=0，仍无错误）
	require.NoError(t, repo.Delete("openai"))
	_, err = repo.GetByID("openai")
	require.NoError(t, err) // builtin 未被删除

	// 非 builtin 可删
	def2 := &model.ProviderDefinition{ID: "custom", DisplayName: "Custom", Adapter: "openai", Builtin: false, Enabled: true}
	require.NoError(t, repo.Create(def2))
	require.NoError(t, repo.Delete("custom"))
	_, err = repo.GetByID("custom")
	assert.Error(t, err)

	// GetByID not found
	_, err = repo.GetByID("does-not-exist")
	assert.Error(t, err)
}

// ============ ModelTagRepo ============

func TestModelTagRepo_CRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewModelTagRepo(db)
	providerRepo := NewProviderRepo(db)
	modelRepo := NewModelRepo(db)

	tag := &model.ModelTag{
		ID:        uuid.New(),
		Name:      "tag-1",
		Color:     "#fff",
		SortOrder: 1,
	}
	require.NoError(t, repo.CreateTag(tag))

	got, err := repo.GetTagByID(tag.ID)
	require.NoError(t, err)
	assert.Equal(t, "tag-1", got.Name)

	exists, err := repo.ExistsTagByName("tag-1")
	require.NoError(t, err)
	assert.True(t, exists)
	exists, _ = repo.ExistsTagByName("missing")
	assert.False(t, exists)

	tag.Name = "tag-1-renamed"
	require.NoError(t, repo.UpdateTag(tag))

	tags, err := repo.ListTags()
	require.NoError(t, err)
	assert.Len(t, tags, 1)
	assert.Equal(t, "tag-1-renamed", tags[0].Name)

	// SetModelTags
	p := newProvider("p-tag")
	require.NoError(t, providerRepo.Create(p))
	m := newModel("m-tag", p.ID, true)
	require.NoError(t, modelRepo.Create(m))
	require.NoError(t, db.Model(&model.Model{}).Where("name = ?", "m-tag").Update("id", m.ID).Error)

	// 创建另一个 tag
	tag2 := &model.ModelTag{ID: uuid.New(), Name: "tag-2", Color: "#000", SortOrder: 2}
	require.NoError(t, repo.CreateTag(tag2))

	// 绑定两个 tag
	require.NoError(t, repo.SetModelTags(m.ID, []uuid.UUID{tag.ID, tag2.ID}))
	gotTags, err := repo.GetModelTags(m.ID)
	require.NoError(t, err)
	assert.Len(t, gotTags, 2)

	// 全量替换为单个 tag
	require.NoError(t, repo.SetModelTags(m.ID, []uuid.UUID{tag.ID}))
	gotTags, _ = repo.GetModelTags(m.ID)
	assert.Len(t, gotTags, 1)

	// 清空
	require.NoError(t, repo.SetModelTags(m.ID, nil))
	gotTags, _ = repo.GetModelTags(m.ID)
	assert.Empty(t, gotTags)

	// 再绑回去测试 GetModelIDsByTagID / BatchGetModelTags
	require.NoError(t, repo.SetModelTags(m.ID, []uuid.UUID{tag.ID}))
	ids, err := repo.GetModelIDsByTagID(tag.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{m.ID}, ids)

	batch, err := repo.BatchGetModelTags([]uuid.UUID{m.ID})
	require.NoError(t, err)
	assert.Len(t, batch[m.ID], 1)

	// BatchGetModelTags 空入参
	batch, err = repo.BatchGetModelTags(nil)
	require.NoError(t, err)
	assert.Nil(t, batch)

	// DeleteTag 级联删除关联
	require.NoError(t, repo.DeleteTag(tag.ID))
	gotTags, _ = repo.GetModelTags(m.ID)
	assert.Empty(t, gotTags)
}
