//go:build integration

// 每日定时同步 + 删除对账的真实链路 E2E：HTTP（JWT）→ handler → service → Postgres → EHR 客户端。
//
// 与 ai_app_hub_sync_e2e_test.go 的区别：那条钉真实接口中心的 99 条组织树，必须有 EHR 凭据；
// 这条把 EHR 换成本地 stub，因此任何环境都能验证"撤销 → 隐藏 → 不丢应用"和"空结果/部分树不隐藏"。
//
// 跑法：
//
//	E2E_DSN="host=/tmp dbname=ai_app_hub_e2e sslmode=disable" JWT_SECRET=dev \
//	go test -tags=integration ./tests/e2e/ -run TestAIAppHubDailySyncE2E -v
package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	aiapphub "github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAIAppHubDailySyncE2E(t *testing.T) {
	dsn := os.Getenv("E2E_DSN")
	secret := os.Getenv("JWT_SECRET")
	if dsn == "" || secret == "" {
		t.Skip("E2E_DSN / JWT_SECRET 未设置，跳过每日同步 E2E")
	}
	t.Setenv("JWT_SECRET", secret)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	for _, table := range []string{"ai_app_departments", "ai_app_studios", "ai_apps", "system_configs", "user_roles"} {
		require.NoError(t, db.Exec("TRUNCATE "+table+" CASCADE").Error)
	}

	// 假 EHR：内容由 current 决定，每轮用例自己设置
	var mu sync.Mutex
	var current []aiapphub.Department
	setItems := func(items ...aiapphub.Department) {
		mu.Lock()
		current = items
		mu.Unlock()
	}
	ehrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer e2e-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		items := append([]aiapphub.Department(nil), current...)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer ehrSrv.Close()

	fetcher := aiapphub.New(aiapphub.Config{
		BaseURL: ehrSrv.URL,
		Prefix:  "/open-apis/ehr-apic/open-apis/resource/v1",
		Token:   "e2e-token",
		OrgID:   "org-e2e",
	})
	hubRepo := repository.NewAIAppHubRepo(db)
	settings := repository.NewSettingsRepo(db)
	svc := service.NewAIAppHubSyncService(fetcher, hubRepo, settings, service.DefaultSyncRules())

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/sync", middleware.JWTAuth(), handler.NewAIAppHubSyncHandler(svc).Sync)
	api := httptest.NewServer(engine)
	defer api.Close()

	adminID := uuid.NewString()
	require.NoError(t, db.Exec("INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'admin', 'company', '', 'e2e')", adminID).Error)
	token := e2eToken(t, secret, adminID, "e2e-admin")

	call := func(body string) (int, map[string]any) {
		req, err := http.NewRequest(http.MethodPost, api.URL+"/sync", bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	syncAll := func() (int, map[string]any) { return call(`{"scope":"all"}`) }
	visibleOf := func(deptID string) bool {
		var v bool
		require.NoError(t, db.Table("ai_app_studios").Select("visible").Where("department_id = ?", deptID).Scan(&v).Error)
		return v
	}

	keep := aiapphub.Department{ID: "studio-keep", Name: "保留事业部", Type: 2, LeaderPassport: "leader-keep"}
	child := aiapphub.Department{ID: "dept-keep-child", Name: "保留部门", ParentID: "studio-keep", Type: 1}
	gone := aiapphub.Department{ID: "studio-gone", Name: "撤销事业部", Type: 2}

	// 1. 首次全量：两棵事业部子树各建一行工作室
	setItems(keep, child, gone)
	code, body := syncAll()
	require.Equal(t, http.StatusOK, code, "%v", body)
	require.Equal(t, float64(3), body["departments"])
	require.Equal(t, float64(2), body["studios"])
	require.Equal(t, float64(0), body["deleted_studios"], "首轮没有可隐藏的行")
	require.True(t, visibleOf("studio-gone"))

	// 撤销事业部下挂一个应用：隐藏工作室不能连带删应用
	var goneStudioID string
	require.NoError(t, db.Raw("SELECT id::text FROM ai_app_studios WHERE department_id = ?", "studio-gone").Scan(&goneStudioID).Error)
	require.NotEmpty(t, goneStudioID)
	require.NoError(t, db.Exec("INSERT INTO ai_apps (id, studio_id, name, status) VALUES (gen_random_uuid(), ?, '撤掉事业部的应用', 'published')", goneStudioID).Error)

	// 2. EHR 撤销该事业部 → 隐藏，但应用行还在
	setItems(keep, child)
	code, body = syncAll()
	require.Equal(t, http.StatusOK, code, "%v", body)
	require.Equal(t, float64(1), body["deleted_studios"])
	require.False(t, visibleOf("studio-gone"), "EHR 删掉的部门要隐藏")
	require.True(t, visibleOf("studio-keep"), "还在的部门不受影响")
	var appCount int64
	require.NoError(t, db.Table("ai_apps").Where("studio_id = ?", goneStudioID).Count(&appCount).Error)
	require.Equal(t, int64(1), appCount, "应用不得因工作室隐藏被删除")

	var deletedKey, deptKey string
	db.Table("system_configs").Select("value").Where("key = ?", "ai_app_hub.last_sync_deleted_studios").Scan(&deletedKey)
	db.Table("system_configs").Select("value").Where("key = ?", "ai_app_hub.last_sync_departments").Scan(&deptKey)
	require.Equal(t, "1", deletedKey)
	require.Equal(t, "2", deptKey)

	// 3. 上游返回空 → 502，且一个人都不隐藏（数据丢失护栏）
	require.NoError(t, db.Exec("UPDATE ai_app_studios SET visible = true WHERE department_id = ?", "studio-gone").Error)
	require.True(t, visibleOf("studio-gone"))
	setItems()
	code, body = syncAll()
	require.Equal(t, http.StatusBadGateway, code, "%v", body)
	require.True(t, visibleOf("studio-gone"), "空结果绝不允许批量隐藏")

	// 4. 部分树护栏：上次 40 个部门，这次只回 1 个 → 判定上游异常，不做对账
	require.NoError(t, settings.SetMultiple(map[string]string{"ai_app_hub.last_sync_departments": "40"}))
	setItems(keep)
	code, body = syncAll()
	require.Equal(t, http.StatusOK, code, "%v", body)
	require.Equal(t, float64(0), body["deleted_studios"], "部门数骤降时不得批量隐藏")
	require.True(t, visibleOf("studio-gone"), "护栏要保住已存在的可见工作室")
}