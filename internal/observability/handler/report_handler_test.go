package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
)

// 以下用例覆盖参数校验：（非法 level/period → 400；合法但 db 未配置 → 503）。
// 完整装配路径在控制面集成（依赖 Postgres 业务表），此处聚焦 HTTP 契约。

func TestReportHandler_ReportParamValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ReportHandler{}

	// 非法 level → 400
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report?level=company&period_start=2026-08-24", nil)
	h.Report(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid level should 400, got %d body=%s", w.Code, w.Body.String())
	}

	// level 合法但缺 period_start → 400
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report?level=personal", nil)
	h.Report(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("missing period_start should 400, got %d body=%s", w2.Code, w2.Body.String())
	}

	// period_type 非法 → 400
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report?level=personal&period_start=2026-08-24&period_type=quarter", nil)
	h.Report(c3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("invalid period_type should 400, got %d body=%s", w3.Code, w3.Body.String())
	}

	// 参数合法，db 未配置 → 503
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report?level=personal&period_start=2026-08-24&period_type=week", nil)
	h.Report(c4)
	if w4.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil db should 503, got %d body=%s", w4.Code, w4.Body.String())
	}
}

func TestReportHandler_MembersParamValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ReportHandler{}

	// personal 不支持 members → 400
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report/members?level=personal&period_start=2026-08-24", nil)
	h.Members(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("personal members should 400, got %d body=%s", w.Code, w.Body.String())
	}

	// level 合法但缺 period_start → 400
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report/members?level=studio", nil)
	h.Members(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("missing period_start should 400, got %d body=%s", w2.Code, w2.Body.String())
	}

	// 合法但 db 未配置 → 503
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/report/members?level=studio&id=s1&period_start=2026-08-24", nil)
	h.Members(c3)
	if w3.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil db should 503, got %d body=%s", w3.Code, w3.Body.String())
	}
}

func TestReportHandler_AnomalyFeedbackValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ReportHandler{}

	// 缺 user_id → 400
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/usage/report/anomaly//feedback", nil)
	c.Params = gin.Params{{Key: "user_id", Value: ""}}
	h.AnomalyFeedback(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty user_id should 400, got %d body=%s", w.Code, w.Body.String())
	}

	// body JSON 非法 → 400
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/v1/usage/report/anomaly/u1/feedback", strings.NewReader("{bad"))
	c2.Params = gin.Params{{Key: "user_id", Value: "u1"}}
	h.AnomalyFeedback(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("bad body should 400, got %d body=%s", w2.Code, w2.Body.String())
	}

	// body 合法但 status 非法 → db 未配置时返回 503（status 校验在服务层）。
	// 这里带合法 body + db=nil → 503。
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, "/api/v1/usage/report/anomaly/u1/feedback", strings.NewReader(`{"status":"confirmed_ok"}`))
	c3.Params = gin.Params{{Key: "user_id", Value: "u1"}}
	h.AnomalyFeedback(c3)
	if w3.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil db should 503, got %d body=%s", w3.Code, w3.Body.String())
	}
}

// newReportRBACTestDB 建 users / departments / dept_studio_map 三张表（内存 sqlite shared-cache），
// 用于驱动 actorCanViewUser 的组织级越权校验。种子：u2 属 d2 部门，d2 映射/挂靠到 studio-s2；
// 与测试中的 studio_admin（管辖 studio-s1）互不相交，从而「管辖范围外」成立。
func newReportRBACTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:report_rbac_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	ddl := []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', dept_id TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE departments (id TEXT PRIMARY KEY, dept_id TEXT NOT NULL DEFAULT '', studio_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE dept_studio_map (dept_id TEXT NOT NULL DEFAULT '', studio_id TEXT NOT NULL DEFAULT '')`,
	}
	for _, s := range ddl {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("exec ddl: %v", err)
		}
	}
	// 目标用户 u2 在 d2（映射到 studio-s2）；studio_admin 只能管 studio-s1。
	if err := db.Exec(`INSERT INTO users (id, name, dept_id) VALUES ('u2','user2','d2')`).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Exec(`INSERT INTO departments (id, dept_id, studio_id, name) VALUES ('d2','DEPT2','studio-s2','d2')`).Error; err != nil {
		t.Fatalf("seed departments: %v", err)
	}
	if err := db.Exec(`INSERT INTO dept_studio_map (dept_id, studio_id) VALUES ('d2','studio-s2')`).Error; err != nil {
		t.Fatalf("seed dept_studio_map: %v", err)
	}
	return db
}

// TestReportHandler_AnomalyFeedback_RBACForbidden 防 F1 回归：非全局角色的负责人（此处 studio_admin，
// 只管辖 studio-s1）对**管辖范围外**的用户（u2 属 studio-s2）POST /usage/report/anomaly/:user_id/feedback
// 应被拒（403），不得越权写 3.6 待核查终裁。
func TestReportHandler_AnomalyFeedback_RBACForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ReportHandler{db: newReportRBACTestDB(t)}

	// 非全局 studio_admin，只管辖 studio-s1；reviewer 身份为 admin1。
	actor := rbac.Actor{
		UserID:    "admin1",
		IsGlobal:  false,
		StudioIDs: []string{"studio-s1"},
		DeptIDs:   nil,
	}
	reviewer := "admin1"
	targetUser := "u2" // 管辖范围外

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", reviewer)
	rbac.SetActor(c, actor)
	c.Params = gin.Params{{Key: "user_id", Value: targetUser}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/usage/report/anomaly/"+targetUser+"/feedback", strings.NewReader(`{"status":"confirmed_ok"}`))

	h.AnomalyFeedback(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-scope feedback should 403, got %d body=%s", w.Code, w.Body.String())
	}
}
