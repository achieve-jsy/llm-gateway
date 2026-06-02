package rbac_test

import (
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openCheckerTestDB 用内存 sqlite 建 user_roles/users/dev_users 表，与 tests/orgservice/repository 的既有约定一致。
// users/dev_users 供 RoleRepo.GetByUserID 的遗留 role 兜底查询使用。
func openCheckerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE user_roles (id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text, granted_by text, actor_id text, created_at datetime)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE users (id text primary key, role text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE dev_users (id text primary key, role text)`).Error)
	return db
}

func newChecker(t *testing.T, seed func(db *gorm.DB)) *rbac.Checker {
	t.Helper()
	db := openCheckerTestDB(t)
	if seed != nil {
		seed(db)
	}
	return rbac.NewChecker(repository.NewRoleRepo(db), repository.NewUserOrgRepo(db))
}

// buildActor 模拟 JWTAuth middleware 注入 user_id / user_role 后的上下文，走 Guard 同款
// BuildAndInjectActor → GetActor 公共路径构建 Actor。
func buildActor(t *testing.T, ch *rbac.Checker, userID, userRole string) rbac.Actor {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", userID)
	if userRole != "" {
		c.Set("user_role", userRole)
	}
	ch.BuildAndInjectActor(c)
	return rbac.GetActor(c)
}

// 核心回归：JWT 自证 user_role=admin 但 user_roles 无该用户记录 → 不得成为全局 admin。
// 修复前该用例会返回 IsGlobal=true（伪造 token 提权），修复后必须为 false。
func TestBuildActor_ClaimAdminWithoutUserRolesRow_NotGlobal(t *testing.T) {
	ch := newChecker(t, nil)

	actor := buildActor(t, ch, "u-forged", "admin")

	require.False(t, actor.IsGlobal)
	require.Empty(t, actor.StudioIDs)
	require.Empty(t, actor.DeptIDs)
}

// 合法路径：user_roles 存在 admin 记录（bootstrap CheckAndGrant 也会写入该行）→ 全局 admin。
func TestBuildActor_UserRolesAdminRow_IsGlobal(t *testing.T) {
	ch := newChecker(t, func(db *gorm.DB) {
		require.NoError(t, db.Exec(
			`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES ('u-admin', 'admin', 'company', '', 'system:bootstrap')`,
		).Error)
	})

	actor := buildActor(t, ch, "u-admin", "admin")

	require.True(t, actor.IsGlobal)
}

// 权限只来自 user_roles：即使 JWT 未带 admin claim，DB 有 admin 记录仍为全局 admin（不锁死 bootstrap 管理员）。
func TestBuildActor_UserRolesAdminRow_WithoutClaim_IsGlobal(t *testing.T) {
	ch := newChecker(t, func(db *gorm.DB) {
		require.NoError(t, db.Exec(
			`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES ('u-admin', 'admin', 'company', '', 'system:bootstrap')`,
		).Error)
	})

	actor := buildActor(t, ch, "u-admin", "")

	require.True(t, actor.IsGlobal)
}

// JWT claim=admin 不能把 dept_admin 提升为全局：权限严格以 user_roles 记录为准。
func TestBuildActor_ClaimAdminCannotEscalateDeptAdmin(t *testing.T) {
	ch := newChecker(t, func(db *gorm.DB) {
		require.NoError(t, db.Exec(
			`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES ('u-dept', 'dept_admin', 'dept', 'dept-a', 'admin')`,
		).Error)
	})

	actor := buildActor(t, ch, "u-dept", "admin")

	require.False(t, actor.IsGlobal)
	require.Equal(t, []string{"dept-a"}, actor.DeptIDs)
}
