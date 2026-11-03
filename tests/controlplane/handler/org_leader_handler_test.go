package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 列名对齐生产 schema（org_node_leaders 见 atlas-migrations/20261104100000）。
func newLeaderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	for _, stmt := range []string{
		"CREATE TABLE studios (id text PRIMARY KEY, name text NOT NULL, owner_id text, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, ehr_department_id text, visible boolean NOT NULL DEFAULT 1, source text NOT NULL DEFAULT 'manual', leader_name text NOT NULL DEFAULT '', leader_passport text NOT NULL DEFAULT '', leader_employee_id text NOT NULL DEFAULT '')",
		"CREATE TABLE departments (id text PRIMARY KEY, dept_id text NOT NULL, dept_name text NOT NULL, studio_id text, status text NOT NULL DEFAULT 'pending_studio', owner_id text, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE org_node_leaders (id text PRIMARY KEY, node_type text NOT NULL, node_id text NOT NULL, name text NOT NULL, passport text NOT NULL DEFAULT '', employee_id text NOT NULL DEFAULT '', source text NOT NULL, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE UNIQUE INDEX uniq_org_node_leader ON org_node_leaders (node_type, node_id, (CASE WHEN employee_id <> '' THEN employee_id WHEN passport <> '' THEN passport ELSE name END))",
		"CREATE TABLE ehr_employees (id text PRIMARY KEY, org_id text NOT NULL, employee_id text NOT NULL, person_id text NOT NULL DEFAULT '', passport text NOT NULL DEFAULT '', person_name text NOT NULL DEFAULT '', group_e_number text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', office text NOT NULL DEFAULT '', appoint_name text NOT NULL DEFAULT '', appoint_code text NOT NULL DEFAULT '', department_id text NOT NULL DEFAULT '', department_name text NOT NULL DEFAULT '', person_status integer NOT NULL DEFAULT 0, person_type integer NOT NULL DEFAULT 0, gender integer NOT NULL DEFAULT 0, is_default boolean NOT NULL DEFAULT 0, location text NOT NULL DEFAULT '', manager_employee_id text NOT NULL DEFAULT '', manager_person_id text NOT NULL DEFAULT '', manager_passport text NOT NULL DEFAULT '', manager_name text NOT NULL DEFAULT '', join_date text NOT NULL DEFAULT '', on_board_date text NOT NULL DEFAULT '', ehr_modify_date text NOT NULL DEFAULT '', synced_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(org_id, employee_id))",
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

func leaderRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewOrgLeaderHandler(orgrepo.NewEHRProjectionRepo(db), repository.NewAIAppHubRepo(db))
	r.GET("/leaders", h.List)
	r.POST("/leaders", h.Add)
	return r
}

func postLeader(t *testing.T, r *gin.Engine, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/leaders", bytes.NewReader(buf)))
	return w
}

// 手动加负责人：花名册解析、404、幂等、非法入参。
func TestLeaderHandlerAddResolvesRoster(t *testing.T) {
	db := newLeaderTestDB(t)
	studioID := uuid.New()
	require.NoError(t, db.Exec("INSERT INTO studios (id, name) VALUES (?, ?)", studioID.String(), "技术研究工作室").Error)
	require.NoError(t, db.Create(&cpmodel.EHREmployee{
		ID: uuid.New(), OrgID: "org1", EmployeeID: "emp-1", PersonName: "张三", Passport: "p-zs", GroupENumber: "BF001",
	}).Error)
	r := leaderRouter(t, db)

	// 工号精确命中
	w := postLeader(t, r, map[string]string{"node_type": "studio", "node_id": studioID.String(), "keyword": "BF001"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var rows []orgmodel.OrgNodeLeader
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "张三", rows[0].Name)
	require.Equal(t, orgmodel.LeaderSourceManual, rows[0].Source)

	// 姓名唯一命中 + 重复加同一人 → 仍一行
	w = postLeader(t, r, map[string]string{"node_type": "studio", "node_id": studioID.String(), "keyword": "张三"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "重复加同一人幂等")

	// 花名册查不到 → 404
	w = postLeader(t, r, map[string]string{"node_type": "studio", "node_id": studioID.String(), "keyword": "查无此人"})
	require.Equal(t, http.StatusNotFound, w.Code)

	// 节点不存在 → 404
	w = postLeader(t, r, map[string]string{"node_type": "studio", "node_id": uuid.NewString(), "keyword": "BF001"})
	require.Equal(t, http.StatusNotFound, w.Code)

	// 非法 node_type → 400
	w = postLeader(t, r, map[string]string{"node_type": "bogus", "node_id": studioID.String(), "keyword": "BF001"})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// List 返回
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/leaders", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "张三")
}
