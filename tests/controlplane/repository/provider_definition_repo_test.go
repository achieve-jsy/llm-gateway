package repository_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupProviderDefinitionRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.Exec(`
		CREATE TABLE provider_definitions (
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
			created_at DATETIME,
			updated_at DATETIME
		);
		CREATE TABLE providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			definition_id TEXT DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
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
			created_at DATETIME,
			updated_at DATETIME
		);
	`).Error)
	return db
}

func TestProviderDefinitionRepo_ListFallsBackToExistingProvidersWhenDefinitionsEmpty(t *testing.T) {
	// 中文说明：历史库可能只有 providers，没有 provider_definitions seed；快速接入仍需要可选供应商候选。
	db := setupProviderDefinitionRepoDB(t)
	require.NoError(t, db.Exec(`
		INSERT INTO providers (id, name, definition_id, type, endpoint, encrypted_credentials, enabled, status)
		VALUES
			('p1', 'qwen-prod-001', 'qwen', 'dashscope', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'encrypted', 1, 1),
			('p2', 'qwen-prod-002', 'qwen', 'dashscope', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'encrypted', 1, 1),
			('p3', 'deepseek-dev-001', 'deepseek', 'deepseek', 'https://api.deepseek.com', 'encrypted', 1, 1);
	`).Error)

	defs, err := repository.NewProviderDefinitionRepo(db).List()
	require.NoError(t, err)
	require.Len(t, defs, 2)

	assert.Equal(t, "deepseek", defs[0].ID)
	assert.Equal(t, "DeepSeek", defs[0].DisplayName)
	assert.Equal(t, "deepseek", defs[0].Adapter)
	assert.Equal(t, "https://api.deepseek.com", defs[0].BaseURL)
	assert.Equal(t, "API Key", defs[0].CredentialSchema["api_key"].Label)

	assert.Equal(t, "qwen", defs[1].ID)
	assert.Equal(t, "千问", defs[1].DisplayName)
	assert.Equal(t, "dashscope", defs[1].Adapter)
	assert.Equal(t, "https://dashscope.aliyuncs.com/compatible-mode/v1", defs[1].BaseURL)
	assert.True(t, defs[1].Builtin)
}
