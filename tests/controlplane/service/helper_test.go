package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newAIHubTestDBWithSettings 建一个 sqlite 内存库，含聚合页用到的表 + system_configs。
//
// user_roles 的列与 migrations/043_add_org_tables.up.sql 保持一致（user_id，不是 passport）——
// 用错的 schema 会让"列名写错"的缺陷在测试里也绿。
func newAIHubTestDBWithSettings(t *testing.T) (*gorm.DB, *repository.SettingsRepo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	stmts := []string{
		"CREATE TABLE ai_app_studios (id text PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), department_id text UNIQUE, name text NOT NULL DEFAULT '', leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '', leader_employee_id text NOT NULL DEFAULT '', source text NOT NULL DEFAULT 'ehr', visible boolean NOT NULL DEFAULT 1, sort_order integer NOT NULL DEFAULT 0, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE ai_app_departments (department_id text PRIMARY KEY, name text NOT NULL DEFAULT '', parent_id text NOT NULL DEFAULT '', department_type integer NOT NULL DEFAULT 0, department_path text NOT NULL DEFAULT '', studio_id text, leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '', synced_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE system_configs (key text PRIMARY KEY, value text, encrypted boolean, description text, created_at text, updated_at text)",
		"CREATE TABLE user_roles (id integer PRIMARY KEY AUTOINCREMENT, user_id text NOT NULL, role text NOT NULL, scope_type text NOT NULL, scope_id text NOT NULL DEFAULT '', granted_by text NOT NULL, actor_id text, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		// users 只取人员同步用到的列（工号 + passport），列名与线上一致。
		"CREATE TABLE users (id text PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), sub text NOT NULL DEFAULT '', employee_id text, name text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '')",
		// ehr_employees 用线上真实列名（person_name / group_e_number / location），
		// 写错列名在 sqlite 下同样会炸，避免测试假绿。
		"CREATE TABLE ehr_employees (id text PRIMARY KEY, org_id text NOT NULL, employee_id text NOT NULL, person_id text NOT NULL DEFAULT '', passport text NOT NULL DEFAULT '', person_name text NOT NULL DEFAULT '', group_e_number text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', office text NOT NULL DEFAULT '', appoint_name text NOT NULL DEFAULT '', appoint_code text NOT NULL DEFAULT '', department_id text NOT NULL DEFAULT '', department_name text NOT NULL DEFAULT '', person_status integer NOT NULL DEFAULT 0, person_type integer NOT NULL DEFAULT 0, gender integer NOT NULL DEFAULT 0, is_default boolean NOT NULL DEFAULT 0, location text NOT NULL DEFAULT '', manager_employee_id text NOT NULL DEFAULT '', manager_person_id text NOT NULL DEFAULT '', manager_passport text NOT NULL DEFAULT '', manager_name text NOT NULL DEFAULT '', join_date text NOT NULL DEFAULT '', on_board_date text NOT NULL DEFAULT '', ehr_modify_date text NOT NULL DEFAULT '', synced_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(org_id, employee_id))",
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db, repository.NewSettingsRepo(db)
}
