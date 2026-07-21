package repository_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openRoleRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE user_roles (id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text, granted_by text, actor_id text, created_at datetime)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE users (id text primary key, role text, name text, email text, dept_id text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE dev_users (id text primary key, role text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE departments (id text primary key, dept_name text)`).Error)
	return db
}

func TestRoleRepoFallsBackToLegacyUsersRole(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO users (id, role) VALUES ('u-admin', 'admin')`).Error)
	repo := repository.NewRoleRepo(db)

	roles, err := repo.GetByUserID("u-admin")

	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, model.RoleAdmin, roles[0].Role)
}

func TestRoleRepoFallsBackToLegacyDevUsersRole(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO dev_users (id, role) VALUES ('dev-admin', 'admin')`).Error)
	repo := repository.NewRoleRepo(db)

	roles, err := repo.GetByUserID("dev-admin")

	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, model.RoleAdmin, roles[0].Role)
}

func TestRoleRepoPrefersExplicitUserRoles(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO users (id, role) VALUES ('u-admin', 'admin')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES ('u-admin', 'dept_admin', 'dept', 'dept-a', 'tester')`).Error)
	repo := repository.NewRoleRepo(db)

	roles, err := repo.GetByUserID("u-admin")

	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, model.RoleDeptAdmin, roles[0].Role)
}

func TestRoleRepoListManagementRolesByActorPagedFiltersVisibleRows(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO departments (id, dept_name) VALUES ('d-1', '平台研发部'), ('d-2', '数据团队')`).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO users (id, name, email, dept_id) VALUES
		('u-alice', 'Alice', 'alice@example.com', 'd-1'),
		('u-bob', 'Bob', 'bob@example.com', 'd-2'),
		('u-charlie', 'Charlie', 'charlie@example.com', 'd-1')
	`).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by, created_at) VALUES
		('u-alice', 'dept_admin', 'dept', 'dept-a', 'tester', '2026-09-01T00:00:00Z'),
		('u-bob', 'studio_admin', 'studio', 'studio-a', 'tester', '2026-09-02T00:00:00Z'),
		('u-charlie', 'dept_admin', 'dept', 'dept-b', 'tester', '2026-09-03T00:00:00Z')
	`).Error)

	repo := repository.NewRoleRepo(db)
	items, total, err := repo.ListManagementRolesByActorPaged(
		func(db *gorm.DB) *gorm.DB { return db.Where("ur.scope_id IN ?", []string{"dept-a", "studio-a"}) },
		repository.RoleListFilter{Keyword: "example.com"}, 2, 1,
	)

	require.NoError(t, err)
	require.EqualValues(t, 2, total, "过滤和数据范围应在分页前生效")
	require.Len(t, items, 1)
	require.Equal(t, "u-alice", items[0].UserID, "第二页应按授权时间倒序返回 Alice")
}

func TestRoleRepoListManagementRolesByActorPagedFiltersRoleAndScopeKeyword(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO departments (id, dept_name) VALUES ('d-1', '平台研发部')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users (id, name, email, dept_id) VALUES ('u-alice', 'Alice', 'alice@example.com', 'd-1')`).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by, created_at) VALUES
		('u-alice', 'dept_admin', 'dept', 'dept-platform', 'tester', '2026-09-01T00:00:00Z'),
		('u-alice', 'studio_admin', 'studio', 'studio-platform', 'tester', '2026-09-02T00:00:00Z')
	`).Error)

	repo := repository.NewRoleRepo(db)
	items, total, err := repo.ListManagementRolesByActorPaged(
		func(db *gorm.DB) *gorm.DB { return db },
		repository.RoleListFilter{Role: "dept_admin", Keyword: "platform"}, 1, 20,
	)

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, "dept_admin", items[0].Role)
	require.Equal(t, "dept-platform", items[0].ScopeID)
}
