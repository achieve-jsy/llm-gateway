package repository_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestModelGetByIDWithCatalog 钉住"读能力用 GetByIDWithCatalog、写路径用 GetByID"：
// /api/v1/internal/groups 的路由 payload 是逐模型 GetByID 后调
// EffectiveSupportsVideoInput 得到 supports_video_input 的；少了 Preload，
// catalog 已声明的视频能力会一律算成 false，网关永远收不到"支持视频"。
func TestModelGetByIDWithCatalog(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	for _, stmt := range []string{
		"CREATE TABLE providers (id TEXT PRIMARY KEY, name TEXT, type TEXT, definition_id TEXT, status INTEGER DEFAULT 1, enabled INTEGER DEFAULT 1, created_at DATETIME, updated_at DATETIME)",
		"CREATE TABLE model_catalog (id TEXT PRIMARY KEY, source TEXT NOT NULL, source_model_id TEXT NOT NULL, display_name TEXT, provider TEXT, mode TEXT, context_length INTEGER DEFAULT 0, max_output_tokens INTEGER DEFAULT 0, supports_vision BOOLEAN DEFAULT false, supports_video_input BOOLEAN NOT NULL DEFAULT false)",
		"CREATE TABLE models (id TEXT PRIMARY KEY, name TEXT NOT NULL, provider_id TEXT, model_type TEXT DEFAULT 'chat', enabled INTEGER DEFAULT 1, status INTEGER DEFAULT 1, catalog_id TEXT, override_supports_video_input INTEGER, created_at DATETIME, updated_at DATETIME)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	catalogID := uuid.New()
	if err := db.Exec("INSERT INTO model_catalog (id, source, source_model_id, provider, supports_video_input) VALUES (?, 'litellm', 'vertex_ai/gemini-2.0-flash', 'vertex_ai', 1)", catalogID.String()).Error; err != nil {
		t.Fatalf("insert catalog: %v", err)
	}

	modelID := uuid.New()
	if err := db.Exec("INSERT INTO models (id, name, provider_id, catalog_id, enabled) VALUES (?, 'gemini-video', ?, ?, 1)", modelID.String(), uuid.New().String(), catalogID.String()).Error; err != nil {
		t.Fatalf("insert model: %v", err)
	}

	repo := repository.NewModelRepo(db)

	// 能力判定必须用带 catalog 的读路径
	got, err := repo.GetByIDWithCatalog(modelID)
	if err != nil {
		t.Fatalf("GetByIDWithCatalog: %v", err)
	}
	if got.Catalog == nil {
		t.Fatal("GetByIDWithCatalog 必须预加载 Catalog（否则 supports_video_input 永远下发 false）")
	}
	if !got.Catalog.SupportsVideoInput {
		t.Fatal("catalog 的 supports_video_input 未读回 true")
	}
	if !service.EffectiveSupportsVideoInput(got) {
		t.Fatal("catalog 声明支持视频且无 override 时，有效能力必须为 true")
	}

	// 写路径复用的 GetByID 必须**不**预加载 Catalog：UpdateModel 用 db.Save(m)，
	// 预加载关联会让 Save 顺带 upsert 可能过期的 catalog 快照。
	writePath, err := repo.GetByID(modelID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if writePath.Catalog != nil {
		t.Fatal("GetByID 不应预加载 Catalog（写路径 Save 会级联 upsert）")
	}
}
