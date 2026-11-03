package auth_test

import (
	"testing"

	authmodel "github.com/example/llm-gateway/internal/auth/model"
	authrepo "github.com/example/llm-gateway/internal/auth/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 离职人员被软删除后：普通查询看不到（管理页/组织统计直接消失），
// Unscoped 还能查到 —— SSO 登录就靠这个判断"别给他重建账号"。
func TestSoftDeletedUserHiddenFromNormalQueries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	// 不用 AutoMigrate：model 的 id 默认值是 gen_random_uuid()，sqlite 建表会语法错。
	require.NoError(t, db.Exec("CREATE TABLE users (id text PRIMARY KEY, sub text NOT NULL DEFAULT '', dept_id text, name text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', source text NOT NULL DEFAULT 'oidc', role text NOT NULL DEFAULT 'user', employee_id text, job_family text, job_category text, job_title text, dept_source text NOT NULL DEFAULT 'sso', dept_updated_by text, dept_updated_at datetime, login_status text NOT NULL DEFAULT 'active', quota_rpm integer, quota_max_concurrency integer, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, deleted_at datetime)").Error)

	repo := authrepo.NewUserRepository(db)
	user := &authmodel.User{ID: uuid.New(), Sub: "left.man", Name: "离职的人", Email: "left@example.com", LoginStatus: "active"}
	require.NoError(t, repo.Create(user))

	require.NoError(t, db.Delete(&authmodel.User{}, "id = ?", user.ID).Error)

	gone, err := repo.FindBySub("left.man")
	require.Error(t, err, "软删后普通查询查不到")
	require.Nil(t, gone)

	found, err := repo.FindBySubUnscoped("left.man")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.True(t, found.DeletedAt.Valid, "Unscoped 能查出来，且带 deleted_at")
}
