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
	require.NoError(t, db.Exec(`CREATE TABLE users (id text primary key, role text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE dev_users (id text primary key, role text)`).Error)
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

func TestRoleRepoSyncUserRoleFieldResetsUsersRoleWhenNoRolesLeft(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO users (id, role) VALUES ('u-user', 'admin')`).Error)
	repo := repository.NewRoleRepo(db)

	// users.role=admin 但 user_roles 无任何管理角色 → 应重置为 user
	require.NoError(t, repo.SyncUserRoleField("u-user"))

	var role string
	require.NoError(t, db.Raw(`SELECT role FROM users WHERE id = 'u-user'`).Scan(&role).Error)
	require.Equal(t, string(model.RoleUser), role)
}

func TestRoleRepoSyncUserRoleFieldKeepsUsersRoleWhenRolesRemain(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO users (id, role) VALUES ('u-admin', 'admin')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES ('u-admin', 'dept_admin', 'dept', 'dept-a', 'tester')`).Error)
	repo := repository.NewRoleRepo(db)

	// 用户仍持有 user_roles 中的管理角色 → users.role 保持不动
	require.NoError(t, repo.SyncUserRoleField("u-admin"))

	var role string
	require.NoError(t, db.Raw(`SELECT role FROM users WHERE id = 'u-admin'`).Scan(&role).Error)
	require.Equal(t, "admin", role)
}

func TestRoleRepoSyncUserRoleFieldFallsBackToDevUsers(t *testing.T) {
	db := openRoleRepoTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO dev_users (id, role) VALUES ('dev-user', 'admin')`).Error)
	repo := repository.NewRoleRepo(db)

	// users 表查不到（dev 账号）→ 回退更新 dev_users.role
	require.NoError(t, repo.SyncUserRoleField("dev-user"))

	var role string
	require.NoError(t, db.Raw(`SELECT role FROM dev_users WHERE id = 'dev-user'`).Scan(&role).Error)
	require.Equal(t, string(model.RoleUser), role)
}
