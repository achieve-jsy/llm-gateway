package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 反馈接口测试的固定身份。
// JWT 由 auth 模块单测覆盖，这里只模拟 JWTAuth 注入的上下文，不再重复签发 token。
const (
	feedbackTestOrdinaryUserID = "11111111-1111-1111-1111-111111111111"
	feedbackTestAdminUserID    = "22222222-2222-2222-2222-222222222222"
	// feedbackTestUserHeader 让用例按请求头切换当前登录用户，避免为不同角色重复搭路由。
	feedbackTestUserHeader = "X-Test-User"
)

// feedbackHandlerTestDDL 用原生 DDL 建表，避免 PG 专属的 gen_random_uuid()/uuid/timestamptz。
// feedbacks 对应被测接口；user_roles/users/dev_users/departments 供 RBAC Guard 构建 Actor 使用，
// 与 tests/orgservice/rbac/checker_test.go 的既有约定保持一致。
const feedbackHandlerTestDDL = `
CREATE TABLE feedbacks (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	user_name TEXT NOT NULL DEFAULT '',
	user_email TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL DEFAULT 'suggestion',
	content TEXT NOT NULL,
	contact TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	handled_at DATETIME,
	handled_by TEXT,
	handle_note TEXT NOT NULL DEFAULT '',
	-- 模型上 created_at/updated_at 带 default 标签，GORM 在零值时不会写入这两列，
	-- 因此 sqlite 侧必须提供默认值，否则提交会撞 NOT NULL 约束
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE user_roles (id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text, granted_by text, actor_id text, created_at datetime);
CREATE TABLE users (id text primary key, role text);
CREATE TABLE dev_users (id text primary key, role text);
CREATE TABLE departments (id text primary key, dept_id text, studio_id text)`

// setupFeedbackTestRouter 搭建被测路由：真实 Handler + 真实 Service/Repo（内存 sqlite）+ 真实 RBAC Guard。
// 路由挂载方式与 router.go 的 adminGroup 一致：POST 任意登录用户可提交，GET/PATCH 需要 feedback:manage。
func setupFeedbackTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err, "打开内存 sqlite")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // :memory: 每连接一库，限制单连接保证路由与断言命中同一库
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Exec(feedbackHandlerTestDDL).Error, "建测试表")

	h := handler.NewFeedbackHandler(service.NewFeedbackService(repository.NewFeedbackRepo(db)))
	checker := rbac.NewChecker(orgrepo.NewRoleRepo(db), orgrepo.NewUserOrgRepo(db))

	// injectFeedbackAuth 模拟 JWTAuth：注入 user_id / user_name / user_email
	injectFeedbackAuth := func(c *gin.Context) {
		userID := c.GetHeader(feedbackTestUserHeader)
		if userID == "" {
			userID = feedbackTestOrdinaryUserID
		}
		c.Set("user_id", userID)
		c.Set("user_name", "测试用户")
		c.Set("user_email", "tester@example.com")
	}
	guard := checker.Guard(rbac.PermFeedbackManage)

	r := gin.New()
	r.POST("/api/v1/feedback", injectFeedbackAuth, h.Create)
	r.GET("/api/v1/feedback", injectFeedbackAuth, guard, h.List)
	r.PATCH("/api/v1/feedback/:id", injectFeedbackAuth, guard, h.UpdateStatus)
	return r, db
}

// seedFeedbackAdminRole 给管理员写入 user_roles 记录，Guard 据此判定 IsGlobal=true。
func seedFeedbackAdminRole(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'admin', 'company', '', 'system:test')`,
		feedbackTestAdminUserID,
	).Error, "写入管理员角色")
}

// doFeedbackRequest 发送请求；userID 为空表示使用默认的普通登录用户。
func doFeedbackRequest(r *gin.Engine, method, path, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set(feedbackTestUserHeader, userID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ===================== POST /api/v1/feedback =====================

func TestFeedbackHandlerCreate_Validation(t *testing.T) {
	// 测试目的：提交反馈的入参非法时必须返回 400 validation_error，且不落库。
	tests := []struct {
		name        string
		description string
		body        string
	}{
		{
			name:        "缺少 content 字段",
			description: "请求体只有 type，content 缺失（空串）应返回 400",
			body:        `{"type":"bug","contact":"tester@example.com"}`,
		},
		{
			name:        "content 去空格后不足 5 字符",
			description: "content 只有 4 个字符，应返回 400",
			body:        `{"type":"bug","content":"abcd"}`,
		},
		{
			name:        "content 全为空白字符",
			description: "content 只有空格，去空格后长度为 0，应返回 400",
			body:        `{"type":"suggestion","content":"     "}`,
		},
		{
			name:        "content 超过 2000 字符",
			description: "content 长度 2001 超过上限，应返回 400",
			body:        `{"type":"bug","content":"` + strings.Repeat("a", 2001) + `"}`,
		},
		{
			name:        "type 非契约取值",
			description: "type=feature 不属于 suggestion/bug/usage/other，应返回 400",
			body:        `{"type":"feature","content":"这是一个合法长度的反馈内容"}`,
		},
		{
			name:        "contact 超过 128 字符",
			description: "contact 长度 129 超过上限，应返回 400",
			body:        `{"type":"other","content":"这是一个合法长度的反馈内容","contact":"` + strings.Repeat("c", 129) + `"}`,
		},
		{
			name:        "请求体不是合法 JSON",
			description: "JSON 解析失败应返回 400",
			body:        `{invalid json}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, db := setupFeedbackTestRouter(t)

			w := doFeedbackRequest(r, http.MethodPost, "/api/v1/feedback", tt.body, "")

			require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())

			var resp struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Equal(t, "validation_error", resp.Error.Type)

			// 校验失败不应产生任何记录
			var count int64
			require.NoError(t, db.Model(&model.Feedback{}).Count(&count).Error)
			require.Zero(t, count, "非法入参不应落库")
		})
	}
}

func TestFeedbackHandlerCreate_Success(t *testing.T) {
	// 测试目的：合法提交返回 201，响应体含 id/status/created_at，并落库为当前登录用户。
	r, db := setupFeedbackTestRouter(t)

	w := doFeedbackRequest(r, http.MethodPost, "/api/v1/feedback",
		`{"type":"bug","content":"  提交后页面白屏  ","contact":"tester@example.com"}`, "")

	require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())

	var resp struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// id 必须是可解析的 UUID
	id, err := uuid.Parse(resp.ID)
	require.NoError(t, err, "id 应为合法 UUID")
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, service.FeedbackStatusPending, resp.Status, "初始状态应为 pending")
	// created_at 必须是可解析且非零的 RFC3339 时间戳
	createdAt, err := time.Parse(time.RFC3339, resp.CreatedAt)
	require.NoError(t, err, "created_at 应为 RFC3339")
	require.False(t, createdAt.IsZero(), "created_at 不应为零值")

	// 落库记录：归属提交人，类型/内容/联系方式正确，content 已去首尾空格
	var stored model.Feedback
	require.NoError(t, db.First(&stored, "id = ?", id).Error)
	require.Equal(t, uuid.MustParse(feedbackTestOrdinaryUserID), stored.UserID)
	require.Equal(t, "bug", stored.Type)
	require.Equal(t, "提交后页面白屏", stored.Content)
	require.Equal(t, "tester@example.com", stored.Contact)
	require.Equal(t, service.FeedbackStatusPending, stored.Status)
}

func TestFeedbackHandlerCreate_DefaultTypeIsSuggestion(t *testing.T) {
	// 测试目的：type 缺省（空串）时归一为 suggestion。
	r, db := setupFeedbackTestRouter(t)

	w := doFeedbackRequest(r, http.MethodPost, "/api/v1/feedback",
		`{"content":"没有指定类型的反馈内容"}`, "")
	require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())

	var stored model.Feedback
	require.NoError(t, db.First(&stored).Error)
	require.Equal(t, service.FeedbackTypeSuggestion, stored.Type)
}

// ===================== GET /api/v1/feedback =====================

func TestFeedbackHandlerList_ForbiddenWithoutPermission(t *testing.T) {
	// 测试目的：普通登录用户（user_roles 无记录 → 非全局 admin）访问管理列表应被 Guard 拦截为 403。
	r, _ := setupFeedbackTestRouter(t)

	w := doFeedbackRequest(r, http.MethodGet, "/api/v1/feedback", "", feedbackTestOrdinaryUserID)

	require.Equal(t, http.StatusForbidden, w.Code, "body = %s", w.Body.String())

	var resp struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "org.forbidden", resp.Code)
}

func TestFeedbackHandlerList_SuccessAsAdmin(t *testing.T) {
	// 测试目的：管理员（有 feedback:manage）返回 200，响应体含 items/total/page/page_size，
	// 且 page/page_size 小于下限时归一为 1/20。
	r, db := setupFeedbackTestRouter(t)
	seedFeedbackAdminRole(t, db)

	// 先以普通用户提交两条，走真实写入路径
	for _, body := range []string{
		`{"type":"bug","content":"第一条反馈内容"}`,
		`{"type":"suggestion","content":"第二条反馈内容"}`,
	} {
		w := doFeedbackRequest(r, http.MethodPost, "/api/v1/feedback", body, "")
		require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())
	}

	w := doFeedbackRequest(r, http.MethodGet, "/api/v1/feedback?page=0&page_size=0", "", feedbackTestAdminUserID)
	require.Equal(t, http.StatusOK, w.Code, "body = %s", w.Body.String())

	var resp struct {
		Items    []model.Feedback `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	require.Len(t, resp.Items, 2)
	require.Equal(t, int64(2), resp.Total)
	require.Equal(t, 1, resp.Page, "page<1 应归一为 1")
	require.Equal(t, 20, resp.PageSize, "page_size<1 应归一为 20")

	// 两条提交都在本次用例内写入，created_at 精度只到秒，顺序不稳定，这里只断言集合完整
	contents := make([]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		contents = append(contents, item.Content)
	}
	require.ElementsMatch(t, []string{"第一条反馈内容", "第二条反馈内容"}, contents)
}

func TestFeedbackHandlerList_InvalidFilter(t *testing.T) {
	// 测试目的：管理员传入非法 status 过滤条件时返回 400 validation_error。
	r, db := setupFeedbackTestRouter(t)
	seedFeedbackAdminRole(t, db)

	w := doFeedbackRequest(r, http.MethodGet, "/api/v1/feedback?status=bogus", "", feedbackTestAdminUserID)

	require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
}

// ===================== PATCH /api/v1/feedback/:id =====================

func TestFeedbackHandlerUpdateStatus_InvalidStatus(t *testing.T) {
	// 测试目的：状态值非契约取值时返回 400（状态校验先于存在性校验）。
	tests := []struct {
		name        string
		description string
		body        string
	}{
		{name: "状态为空", description: "status 为空串应返回 400", body: `{"status":""}`},
		{name: "状态不在契约内", description: "status=bogus 应返回 400", body: `{"status":"bogus"}`},
		{name: "状态大小写不匹配", description: "status=Resolved 应返回 400", body: `{"status":"Resolved"}`},
		{name: "请求体缺少 status", description: "只传 handle_note 时 status 为空串，应返回 400", body: `{"handle_note":"已处理"}`},
		{name: "请求体不是合法 JSON", description: "JSON 解析失败应返回 400", body: `{invalid json}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, db := setupFeedbackTestRouter(t)
			seedFeedbackAdminRole(t, db)

			w := doFeedbackRequest(r, http.MethodPatch, "/api/v1/feedback/"+uuid.NewString(), tt.body, feedbackTestAdminUserID)

			require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		})
	}
}

func TestFeedbackHandlerUpdateStatus_NotFound(t *testing.T) {
	// 测试目的：状态合法但 id 不存在时返回 404 not_found。
	r, db := setupFeedbackTestRouter(t)
	seedFeedbackAdminRole(t, db)

	w := doFeedbackRequest(r, http.MethodPatch, "/api/v1/feedback/"+uuid.NewString(),
		`{"status":"resolved","handle_note":"已处理"}`, feedbackTestAdminUserID)

	require.Equal(t, http.StatusNotFound, w.Code, "body = %s", w.Body.String())

	var resp struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "not_found", resp.Error.Type)
}

func TestFeedbackHandlerUpdateStatus_Success(t *testing.T) {
	// 测试目的：管理员更新状态返回 200 与完整 FeedbackItem，resolved 时写入 handled_at/handled_by/handle_note。
	r, db := setupFeedbackTestRouter(t)
	seedFeedbackAdminRole(t, db)

	createW := doFeedbackRequest(r, http.MethodPost, "/api/v1/feedback", `{"type":"bug","content":"提交后页面白屏"}`, "")
	require.Equal(t, http.StatusCreated, createW.Code, "body = %s", createW.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &created))

	w := doFeedbackRequest(r, http.MethodPatch, "/api/v1/feedback/"+created.ID,
		`{"status":"resolved","handle_note":"已修复并发布"}`, feedbackTestAdminUserID)
	require.Equal(t, http.StatusOK, w.Code, "body = %s", w.Body.String())

	var item struct {
		ID         string  `json:"id"`
		Status     string  `json:"status"`
		HandleNote string  `json:"handle_note"`
		HandledAt  *string `json:"handled_at"`
		HandledBy  *string `json:"handled_by"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))

	require.Equal(t, created.ID, item.ID)
	require.Equal(t, service.FeedbackStatusResolved, item.Status)
	require.Equal(t, "已修复并发布", item.HandleNote)
	require.NotNil(t, item.HandledAt, "resolved 时应写入 handled_at")
	require.NotNil(t, item.HandledBy, "resolved 时应写入 handled_by")
	require.Equal(t, feedbackTestAdminUserID, *item.HandledBy)
}

func TestFeedbackHandlerUpdateStatus_ForbiddenWithoutPermission(t *testing.T) {
	// 测试目的：普通登录用户 PATCH 反馈状态应被 Guard 拦截为 403。
	r, _ := setupFeedbackTestRouter(t)

	w := doFeedbackRequest(r, http.MethodPatch, "/api/v1/feedback/"+uuid.NewString(),
		`{"status":"resolved"}`, feedbackTestOrdinaryUserID)

	require.Equal(t, http.StatusForbidden, w.Code, "body = %s", w.Body.String())
}
