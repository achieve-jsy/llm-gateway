package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newAIHubTestDBWithSettings 建一个 sqlite 内存库，含聚合页用到的表 + system_configs。
func newAIHubTestDBWithSettings(t *testing.T) (*gorm.DB, *repository.SettingsRepo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	stmts := []string{
		"CREATE TABLE ai_app_studios (id text PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), department_id text UNIQUE, name text NOT NULL DEFAULT '', leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '', leader_employee_id text NOT NULL DEFAULT '', source text NOT NULL DEFAULT 'ehr', visible boolean NOT NULL DEFAULT 1, sort_order integer NOT NULL DEFAULT 0, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE ai_app_departments (department_id text PRIMARY KEY, name text NOT NULL DEFAULT '', parent_id text NOT NULL DEFAULT '', department_type integer NOT NULL DEFAULT 0, department_path text NOT NULL DEFAULT '', studio_id text, leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '', synced_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE system_configs (key text PRIMARY KEY, value text, encrypted boolean, description text, created_at text, updated_at text)",
		"CREATE TABLE user_roles (id text PRIMARY KEY, passport text, role text, scope_type text, scope_id text)",
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db, repository.NewSettingsRepo(db)
}
