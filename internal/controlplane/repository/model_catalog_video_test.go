package repository

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestBatchUpsertVideoInput 专门防止"只给 Upsert 加列、忘了 BatchUpsert"的漏改：
// 同步实际走的是 BatchUpsert，这里断言写入后能读回 true。
func TestBatchUpsertVideoInput(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE model_catalog (
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
		supports_function_calling BOOLEAN DEFAULT false,
		supports_vision BOOLEAN DEFAULT false,
		supports_prompt_caching BOOLEAN DEFAULT false,
		supports_response_schema BOOLEAN DEFAULT false,
		supports_tool_choice BOOLEAN DEFAULT false,
		supports_parallel_function_calling BOOLEAN DEFAULT false,
		supports_video_input BOOLEAN NOT NULL DEFAULT false,
		deprecation_date DATETIME,
		raw_payload TEXT,
		synced_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE (source, source_model_id)
	)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewModelCatalogRepo(db)
	entry := &model.ModelCatalog{
		Source:             "litellm",
		SourceModelID:      "gemini/gemini-3-pro",
		Mode:               "chat",
		SupportsVideoInput: true,
	}
	if err := repo.BatchUpsert([]*model.ModelCatalog{entry}); err != nil {
		t.Fatalf("batch upsert: %v", err)
	}

	got, err := repo.GetBySourceModelID("litellm", "gemini/gemini-3-pro")
	if err != nil || got == nil {
		t.Fatalf("get: %v (entry=%v)", err, got)
	}
	if !got.SupportsVideoInput {
		t.Fatal("supports_video_input lost by BatchUpsert")
	}
}
