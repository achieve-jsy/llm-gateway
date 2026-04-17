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
