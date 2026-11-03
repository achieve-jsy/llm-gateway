package repository_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openWorkspaceTestDB 建组织工作台相关的表（列名逐字对齐生产 schema，
// 见 atlas-migrations 里的 studios/departments/users/user_roles/budget_allocations/group_entitlements）。
// 单测跑 sqlite，生产跑 Postgres：Raw SQL 里的方言差异只允许出现在 textCol/timeCol 里。
func openWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE studios (id text PRIMARY KEY, name text NOT NULL, owner_id text,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, ehr_department_id text,
			visible boolean NOT NULL DEFAULT 1, source text NOT NULL DEFAULT 'manual',
			leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '',
			leader_employee_id text NOT NULL DEFAULT '')`,
		`CREATE TABLE departments (id text PRIMARY KEY, dept_id text NOT NULL, dept_name text NOT NULL,
			studio_id text, status text NOT NULL DEFAULT 'pending_studio', owner_id text,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE users (id text PRIMARY KEY, sub text NOT NULL DEFAULT '', name text NOT NULL,
			email text NOT NULL DEFAULT '', dept_id text, source text NOT NULL DEFAULT 'oidc',
			role text NOT NULL DEFAULT 'user', employee_id text, job_family text, job_category text,
			job_title text, dept_source text NOT NULL DEFAULT 'sso', dept_updated_by text,
			dept_updated_at datetime, login_status text NOT NULL DEFAULT 'active',
			quota_rpm integer, quota_max_concurrency integer,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, deleted_at datetime)`,
		`CREATE TABLE user_roles (id integer PRIMARY KEY AUTOINCREMENT, user_id text NOT NULL,
			role text NOT NULL, scope_type text NOT NULL, scope_id text NOT NULL DEFAULT '',
			granted_by text NOT NULL DEFAULT '', actor_id text,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE org_node_leaders (id text PRIMARY KEY, node_type text NOT NULL, node_id text NOT NULL,
			name text NOT NULL, passport text NOT NULL DEFAULT '', employee_id text NOT NULL DEFAULT '',
			source text NOT NULL, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE budget_allocations (id text PRIMARY KEY, scope_type text NOT NULL,
			scope_id text NOT NULL, amount_usd real NOT NULL DEFAULT 0,
			daily_amount_usd real NOT NULL DEFAULT -1, weekly_amount_usd real NOT NULL DEFAULT -1,
			period text NOT NULL DEFAULT 'monthly', enabled boolean NOT NULL DEFAULT 1,
			created_by text, updated_by text, created_at datetime, updated_at datetime,
			UNIQUE(scope_type, scope_id))`,
		`CREATE TABLE group_entitlements (id text PRIMARY KEY, group_id integer NOT NULL,
			scope_type text NOT NULL, scope_id text NOT NULL, enabled boolean NOT NULL DEFAULT 1,
			granted_by text, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(group_id, scope_type, scope_id))`,
		`CREATE TABLE system_configs (key text PRIMARY KEY, value text, encrypted boolean NOT NULL DEFAULT 0,
			description text, created_at text, updated_at text)`,
		`CREATE TABLE user_dept_conflicts (id integer PRIMARY KEY AUTOINCREMENT, user_id text,
			current_dept_id text, sso_dept_id text, sso_dept_name text, detected_at datetime)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}
