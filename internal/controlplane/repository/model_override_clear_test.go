package repository_test

import (
	"database/sql"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestUpdateClearsVideoOverrideToNull 钉住 GORM 写 NULL 的行为：
// 模型级 override 是 *bool，Update 走 db.Save 整行写，把 nil 真正落成 NULL
// 才能回到"跟随 catalog"。如果哪天换成 Updates(struct) 或加 omitempty，
// 清除操作会变成静默无效（前端点了"跟随 catalog"却还是强制不支持）。
func TestUpdateClearsVideoOverrideToNull(t *testing.T) {
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
		// 列与 model.Model 的 Save 语句一致：db.Save 会整行 UPDATE，
		// 少一列就直接报 "no such column"。
		"CREATE TABLE models (id TEXT PRIMARY KEY, name TEXT NOT NULL, provider_id TEXT, " +
			"model_type TEXT DEFAULT 'chat', enabled INTEGER DEFAULT 1, status INTEGER DEFAULT 1, " +
			"upstream_model_id TEXT DEFAULT '', description TEXT DEFAULT '', context_length INTEGER DEFAULT 0, " +
			"max_output_tokens INTEGER DEFAULT 0, input_price REAL DEFAULT 0, output_price REAL DEFAULT 0, " +
			"cache_read_price REAL, cache_write_price REAL, price_mode TEXT DEFAULT '', weight INTEGER DEFAULT 0, " +
			"catalog_id TEXT, ctx_overridden INTEGER DEFAULT 0, max_output_overridden INTEGER DEFAULT 0, " +
			"override_supports_video_input INTEGER, video_supported_resolutions TEXT DEFAULT '', " +
			"video_supported_aspect_ratios TEXT DEFAULT '', video_min_duration_seconds INTEGER DEFAULT 0, " +
			"video_max_duration_seconds INTEGER DEFAULT 0, video_supported_actions TEXT DEFAULT '', " +
			"quota_rpm INTEGER, quota_max_concurrency INTEGER, consecutive_failures INTEGER DEFAULT 0, " +
			"consecutive_recovery_successes INTEGER DEFAULT 0, last_check_error TEXT DEFAULT '', " +
			"response_time INTEGER DEFAULT 0, created_at DATETIME, updated_at DATETIME)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	modelID := uuid.New()
	if err := db.Exec("INSERT INTO models (id, name, provider_id, override_supports_video_input, enabled) VALUES (?, 'm', ?, 1, 1)", modelID.String(), uuid.New().String()).Error; err != nil {
		t.Fatalf("insert model: %v", err)
	}

	repo := repository.NewModelRepo(db)
	m, err := repo.GetByID(modelID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if m.OverrideSupportsVideoInput == nil || !*m.OverrideSupportsVideoInput {
		t.Fatalf("初始 override 应为 true，实际 %v", m.OverrideSupportsVideoInput)
	}

	m.OverrideSupportsVideoInput = nil
	if err := repo.Update(m); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var raw sql.NullBool
	if err := db.Raw("SELECT override_supports_video_input FROM models WHERE id = ?", modelID.String()).Row().Scan(&raw); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if raw.Valid {
		t.Fatalf("清除 override 后列必须是 NULL（跟随 catalog），实际 %v", raw.Bool)
	}

	reloaded, err := repo.GetByID(modelID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.OverrideSupportsVideoInput != nil {
		t.Fatalf("重新读取的 override 应为 nil，实际 %v", *reloaded.OverrideSupportsVideoInput)
	}
}
