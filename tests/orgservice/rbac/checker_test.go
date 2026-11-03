package rbac_test

import (
	"net/http"
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
	require.NoError(t, db.Exec(`CREATE TABLE departments (id text primary key, dept_id text, studio_id text)`).Error)
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

func TestBuildActor_MultipleRolesKeepsRolesAndExpandsStudioDepartments(t *testing.T) {
	ch := newChecker(t, func(db *gorm.DB) {
		require.NoError(t, db.Exec(`
			INSERT INTO departments (id, dept_id, studio_id) VALUES
			('dept-row-own', 'dept-own', 'studio-a'),
			('dept-row-peer', 'dept-peer', 'studio-a'),
			('dept-row-other', 'dept-other', 'studio-b')
		`).Error)
		require.NoError(t, db.Exec(`
			INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
			('u-manager', 'dept_admin', 'dept', 'dept-own', 'admin'),
			('u-manager', 'studio_admin', 'studio', 'studio-a', 'admin')
		`).Error)
	})

	actor := buildActor(t, ch, "u-manager", "")

	require.False(t, actor.IsGlobal)
	require.ElementsMatch(t, []string{"dept_admin", "studio_admin"}, actor.Roles)
	require.Equal(t, []string{"dept-own"}, actor.DeptAdminIDs)
	require.ElementsMatch(t, []string{"dept-own", "dept-peer", "dept-row-own", "dept-row-peer"}, actor.DeptIDs)
	require.Equal(t, []string{"studio-a"}, actor.StudioIDs)
	require.ElementsMatch(t, []string{"dept-own", "dept-peer"}, actor.ApprovalDeptIDs)
}

// 工作室范围展开失败时，不能只保留部门负责人范围继续进行审批。
// 否则同一用户的审批能力会在基础组织数据异常时被悄悄缩小，形成不完整的授权判断。
func TestGuard_StudioScopeResolutionFailureBlocksScopedOperation(t *testing.T) {
	db := openCheckerTestDB(t)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
		('u-manager', 'dept_admin', 'dept', 'dept-own', 'admin'),
		('u-manager', 'studio_admin', 'studio', 'studio-a', 'admin')
	`).Error)
	require.NoError(t, db.Exec(`DROP TABLE departments`).Error)

	checker := rbac.NewChecker(repository.NewRoleRepo(db), repository.NewUserOrgRepo(db))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "u-manager")
	})
	router.GET("/pending", checker.Guard(rbac.PermKeyRequestApprove), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pending", nil))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.JSONEq(t, `{"code":"org.scope_unavailable","message":"审批范围暂时不可用，请稍后重试"}`, w.Body.String())
}

// 范围展开错误必须先于角色不足处理。否则工作室负责人访问平台级路由时会得到 403，
// 并掩盖组织范围数据不可用这个需要重试的真实状态。
func TestGuard_StudioScopeResolutionFailureBlocksGlobalOperation(t *testing.T) {
	db := openCheckerTestDB(t)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
		('u-studio-manager', 'studio_admin', 'studio', 'studio-a', 'admin')
	`).Error)
	require.NoError(t, db.Exec(`DROP TABLE departments`).Error)

	checker := rbac.NewChecker(repository.NewRoleRepo(db), repository.NewUserOrgRepo(db))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "u-studio-manager")
	})
	router.GET("/global", checker.Guard(rbac.PermUsageViewAll), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/global", nil))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.JSONEq(t, `{"code":"org.scope_unavailable","message":"审批范围暂时不可用，请稍后重试"}`, w.Body.String())
}

// 用量统计：跨用户视角（用量看板 / 使用分析报告 / 调用明细）仅 admin。
// dept_admin / studio_admin 访问 PermUsageViewAll 必须 403；而 /usage/dashboard 的
// PermUsageViewScoped（预算管理页成员用量列依赖）仍应放行。
func TestGuard_UsageStatsAdminOnlyButScopedDashboardAllowed(t *testing.T) {
	db := openCheckerTestDB(t)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
		('u-dept', 'dept_admin', 'dept', 'dept-a', 'admin'),
		('u-studio', 'studio_admin', 'studio', 'studio-a', 'admin')
	`).Error)

	newRouter := func(userID string) *gin.Engine {
		checker := rbac.NewChecker(repository.NewRoleRepo(db), repository.NewUserOrgRepo(db))
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("user_id", userID) })
		router.GET("/usage-all", checker.Guard(rbac.PermUsageViewAll), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		router.GET("/usage-dashboard", checker.Guard(rbac.PermUsageViewScoped), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		return router
	}

	for _, userID := range []string{"u-dept", "u-studio"} {
		router := newRouter(userID)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/usage-all", nil))
		require.Equal(t, http.StatusForbidden, w.Code, "%s 不应能访问跨用户用量端点", userID)

		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/usage-dashboard", nil))
		require.Equal(t, http.StatusNoContent, w.Code, "%s 应能访问管辖范围内的 /usage/dashboard", userID)
	}
}
