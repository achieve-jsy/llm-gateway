//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
)

// startPostgresOnly 仅启动 Postgres（本测试只依赖 call_logs，不需要 Redis）。
// 与 startIntegrationInfra 的差异：使用本机已有 postgres:16 镜像，避免额外拉取 redis。
func startPostgresOnly(t *testing.T) *gorm.DB {
	t.Helper()
	postgresID := runContainer(t, "postgres:16",
		"-e", "POSTGRES_PASSWORD=gateway",
		"-e", "POSTGRES_USER=gateway",
		"-e", "POSTGRES_DB=gateway_test",
	)
	postgresAddr := mappedAddress(t, postgresID, "5432/tcp")
	var db *gorm.DB
	require.Eventually(t, func() bool {
		host, port, err := net.SplitHostPort(postgresAddr)
		if err != nil {
			return false
		}
		dsn := fmt.Sprintf("host=%s port=%s user=gateway password=gateway dbname=gateway_test sslmode=disable", host, port)
		candidate, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			return false
		}
		sqlDB, err := candidate.DB()
		if err != nil || sqlDB.Ping() != nil {
			return false
		}
		db = candidate
		return true
	}, 30*time.Second, 100*time.Millisecond)
	applyAtlasSchema(t, postgresAddr, "gateway", "gateway", "gateway_test")
	// atlas baseline 未包含 org-service 的 studios/dept_studio_map 表（定义在旧 migrations/043 中），
	// 而 GetDashboard 的 LEFT JOIN 会引用它们，这里补建空表保证 SQL 可执行。
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS studios (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			owner_id VARCHAR(64),
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS dept_studio_map (
			dept_id VARCHAR(64) PRIMARY KEY,
			studio_id VARCHAR(64) NOT NULL REFERENCES studios(id),
			source VARCHAR(16) NOT NULL DEFAULT 'manual',
			updated_by VARCHAR(64) NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

// TestDashboardDeptScopeSeparatesServiceKeys 验证 dept 范围下：
//  1. 成员排行排除程序调用（Service Key）；
//  2. 程序调用被单独聚合进 summary（service_cost_estimate 等）；
//  3. 总量 = 成员 + 程序，可对账；
//  4. user_id 为空的 personal 调用（未关联）仍保留在成员排行中，不因 service 过滤被误删。
func TestDashboardDeptScopeSeparatesServiceKeys(t *testing.T) {
	db := startPostgresOnly(t)

	require.NoError(t, db.Exec("TRUNCATE call_logs").Error)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -1).Format("2006-01-02")
	end := now.AddDate(0, 0, 1).Format("2006-01-02")

	member := obsmodel.CallLog{
		RequestID:                     "req-member-1",
		TS:                            now,
		KeyID:                         "key-member",
		KeyName:                       "个人Key",
		KeyType:                       obsmodel.KeyTypePersonal,
		UserID:                        "user-001",
		UserName:                      "张三",
		DeptID:                        "dept-001",
		DeptName:                      "测试部门",
		StudioID:                      "studio-001",
		Model:                         "gpt-4o",
		Status:                        200,
		PromptTokens:                  100,
		CompletionTokens:              50,
		TotalTokens:                   150,
		EstCostUSD:                    1.5,
		InputCostUSD:                  1.0,
		OutputCostUSD:                 0.5,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
	}
	orphan := obsmodel.CallLog{
		RequestID:                     "req-orphan-1",
		TS:                            now,
		KeyID:                         "key-orphan",
		KeyName:                       "遗留Key",
		KeyType:                       obsmodel.KeyTypePersonal,
		UserID:                        "",
		UserName:                      "",
		DeptID:                        "dept-001",
		DeptName:                      "测试部门",
		StudioID:                      "studio-001",
		Model:                         "gpt-4o",
		Status:                        200,
		PromptTokens:                  30,
		CompletionTokens:              20,
		TotalTokens:                   50,
		EstCostUSD:                    0.5,
		InputCostUSD:                  0.3,
		OutputCostUSD:                 0.2,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
	}
	service := obsmodel.CallLog{
		RequestID:                     "req-service-1",
		TS:                            now,
		KeyID:                         "key-service",
		KeyName:                       "数据同步机器人",
		KeyType:                       obsmodel.KeyTypeService,
		UserID:                        "",
		UserName:                      "",
		DeptID:                        "dept-001",
		DeptName:                      "测试部门",
		StudioID:                      "studio-001",
		Model:                         "gpt-4o",
		Status:                        200,
		PromptTokens:                  200,
		CompletionTokens:              100,
		TotalTokens:                   300,
		EstCostUSD:                    3.0,
		InputCostUSD:                  2.0,
		OutputCostUSD:                 1.0,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
	}

	require.NoError(t, db.Create(&member).Error)
	require.NoError(t, db.Create(&orphan).Error)
	require.NoError(t, db.Create(&service).Error)

	h := obshandler.NewUsageHandler(nil, db)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	rbac.SetActor(c, rbac.Actor{IsGlobal: true})
	c.Request = httptest.NewRequest(http.MethodGet,
		"/api/v1/usage/dashboard?scope=dept&id=dept-001&start="+start+"&end="+end, nil)

	h.GetDashboard(c)

	require.Equal(t, http.StatusOK, w.Code, "unexpected status: %s", w.Body.String())

	var resp obshandler.DashboardResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// 成员排行：personal 调用（含未关联）保留，service 调用被排除
	require.Len(t, resp.Breakdown, 2, "expected member + orphan rows, got %+v", resp.Breakdown)
	ids := map[string]bool{}
	for _, b := range resp.Breakdown {
		ids[b.ID] = true
	}
	require.True(t, ids["user-001"], "member row missing")
	require.True(t, ids[""], "orphan personal row should be retained")

	// 程序调用被单独聚合
	require.InDelta(t, 3.0, resp.Summary.ServiceCostEstimate, 1e-6)
	require.Equal(t, int64(1), resp.Summary.ServiceCallCount)
	require.Equal(t, int64(300), resp.Summary.ServiceTotalTokens)

	// 总量 = 成员(1.5) + 未关联(0.5) + 程序(3.0)
	require.InDelta(t, 5.0, resp.Summary.CostEstimate, 1e-6)
	require.Equal(t, int64(3), resp.Summary.CallCount)
	require.Equal(t, int64(500), resp.Summary.TotalTokens)

	// 活跃成员：仅统计 personal 且 user_id 非空
	require.Equal(t, 1, resp.Summary.UserCount)
}
