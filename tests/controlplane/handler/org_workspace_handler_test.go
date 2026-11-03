package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	orghandler "github.com/example/llm-gateway/internal/orgservice/handler"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 组织工作台 handler 测试库：列名对齐生产 schema（studios/departments/users/user_roles/...）
func openOrgWorkspaceDB(t *testing.T) *gorm.DB {
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
		`CREATE TABLE system_configs (key text PRIMARY KEY, value text, encrypted boolean NOT NULL DEFAULT 0,
			description text, created_at text, updated_at text)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}

	for _, stmt := range []string{
		`INSERT INTO studios (id, name, source) VALUES ('s1', '战龙工作室', 'ehr')`,
		`INSERT INTO departments (id, dept_id, dept_name, studio_id, status) VALUES
			('dp-1', 'dept-a', '技术部', 's1', 'active'),
			('dp-2', 'dept-b', '数据组', 's1', 'active'),
			('dp-9', 'dept-z', '待归集部门', NULL, 'pending_studio')`,
		`INSERT INTO users (id, name, email, dept_id, employee_id, job_title, created_at) VALUES
			('u-admin', '张三', 'zhang@example.com', NULL, 'emp-001', '平台工程师', '2026-01-10 00:00:00'),
			('u-dept', '王五', 'wang@example.com', 'dp-1', 'emp-002', '团队负责人', '2026-03-10 00:00:00'),
			('u-plain', '赵六', 'zhao@example.com', 'dp-2', 'emp-003', '后端工程师', '2026-04-10 00:00:00')`,
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
			('u-admin', 'admin', 'company', '', 'tester'),
			('u-dept', 'dept_admin', 'dept', 'dept-a', 'tester')`,
		`INSERT INTO org_node_leaders (id, node_type, node_id, name, source) VALUES
			('l-1', 'studio', 's1', '傅彭彭', 'ehr'),
			('l-2', 'dept', 'dp-1', '甲方负责人', 'ehr')`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

func workspaceRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	accountH := orghandler.NewAccountHandler(orgrepo.NewAccountRepo(db))
	overviewH := orghandler.NewOrgOverviewHandler(orgrepo.NewOrgOverviewRepo(db))

	// guard 由 router 层负责；这里直接用 SetActor 模拟注入（admin / 团队管理员两种）
	r.GET("/accounts", func(c *gin.Context) { rbac.SetActor(c, rbac.Actor{IsGlobal: true}) }, accountH.List)
	r.GET("/accounts-scoped", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{DeptIDs: []string{"dept-b"}})
	}, accountH.List)
	r.GET("/studios/:id/overview", overviewH.Studio)
	return r
}

func getJSON(t *testing.T, r *gin.Engine, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return w, body
}

// 账号页：一人一行 + 主角色 + roles + role_counts 的形状。
func TestAccountHandlerListShape(t *testing.T) {
	r := workspaceRouter(t, openOrgWorkspaceDB(t))

	w, body := getJSON(t, r, "/accounts")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, float64(3), body["total"])
	require.Equal(t, float64(1), body["page"])
	require.Equal(t, float64(20), body["page_size"])

	accounts, ok := body["accounts"].([]any)
	require.True(t, ok)
	require.Len(t, accounts, 3)

	first := accounts[0].(map[string]any)
	for _, key := range []string{"user_id", "name", "employee_id", "job_title", "email", "created_at",
		"studio_id", "studio_name", "dept_id", "dept_name", "primary_role", "roles"} {
		require.Contains(t, first, key, "契约字段缺失: %s", key)
	}
	require.NotNil(t, first["roles"], "roles 必须是数组，不能是 null")

	counts, ok := body["role_counts"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), counts["admin"])
	require.Equal(t, float64(0), counts["studio_admin"])
	require.Equal(t, float64(1), counts["dept_admin"])
	require.Equal(t, float64(1), counts["user"])
}

// 筛选参数：role 白名单、创建时间格式、区间过滤组合生效。
func TestAccountHandlerFilters(t *testing.T) {
	r := workspaceRouter(t, openOrgWorkspaceDB(t))

	w, body := getJSON(t, r, "/accounts?role=user")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, float64(1), body["total"])
	require.Equal(t, "赵六", body["accounts"].([]any)[0].(map[string]any)["name"])
	require.Equal(t, float64(1), body["role_counts"].(map[string]any)["admin"], "role_counts 不受角色筛选影响")

	w, body = getJSON(t, r, "/accounts?keyword=emp-002")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, float64(1), body["total"])

	w, body = getJSON(t, r, "/accounts?created_from=2026-03-01&created_to=2026-03-10")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, float64(1), body["total"], "含当日：2026-03-10 创建的也算")
	require.Equal(t, "王五", body["accounts"].([]any)[0].(map[string]any)["name"])

	w, body = getJSON(t, r, "/accounts?role=bogus")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "invalid_filter", body["code"])

	w, body = getJSON(t, r, "/accounts?created_from=2026-13-01")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "invalid_filter", body["code"])

	// 非 admin：只看自己管辖部门的人
	w, body = getJSON(t, r, "/accounts-scoped")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, float64(1), body["total"])
	require.Equal(t, "赵六", body["accounts"].([]any)[0].(map[string]any)["name"])
}

// 工作室概览：扁平结构 + 团队卡里的管理员/负责人。
func TestOrgOverviewHandlerStudio(t *testing.T) {
	r := workspaceRouter(t, openOrgWorkspaceDB(t))

	w, body := getJSON(t, r, "/studios/s1/overview")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "s1", body["studio_id"])
	require.Equal(t, "战龙工作室", body["studio_name"])
	require.Equal(t, float64(2), body["member_count"])
	require.Equal(t, float64(1), body["pending_dept_count"])
	for _, key := range []string{"admins", "leaders", "teams"} {
		require.Contains(t, body, key)
	}
	leaders := body["leaders"].([]any)
	require.Len(t, leaders, 1)
	require.Equal(t, "傅彭彭", leaders[0].(map[string]any)["name"])
	require.Equal(t, "ehr", leaders[0].(map[string]any)["source"])

	teams := body["teams"].([]any)
	require.Len(t, teams, 2)
	team := teams[0].(map[string]any)
	for _, key := range []string{"dept_id", "dept_uuid", "dept_name", "member_count", "admins", "leaders"} {
		require.Contains(t, team, key, "团队卡契约字段缺失: %s", key)
	}

	w, body = getJSON(t, r, "/studios/nope/overview")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "studio_not_found", body["code"])
}
