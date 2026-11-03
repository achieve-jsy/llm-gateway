//go:build integration

// AI 应用同步的真实链路 E2E：HTTP（JWT 中间件）→ handler → service → Postgres → EHR 接口中心。
//
// 跑法（需要一个装了 ai_app_hub 表的库，见文件末尾的准备说明）：
//
//	E2E_DSN="host=/tmp dbname=ai_app_hub_e2e user=$USER sslmode=disable" \
//	JWT_SECRET=... EHR_BASE_URL=https://open.example.com \
//	EHR_PREFIX=/open-apis/ehr-apic/open-apis/resource/v1 \
//	EHR_API_TOKEN=... EHR_ORG_ID=1428195512246472704 \
//	go test -tags=integration ./tests/e2e/ -run TestAIAppHubSyncE2E -v
package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	aiapphub "github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func e2eToken(t *testing.T, secret, userID, passport string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":  userID,
		"passport": passport,
		"exp":      time.Now().Add(time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

func TestAIAppHubSyncE2E(t *testing.T) {
	dsn := os.Getenv("E2E_DSN")
	secret := os.Getenv("JWT_SECRET")
	for _, key := range []string{"EHR_BASE_URL", "EHR_API_TOKEN", "EHR_ORG_ID"} {
		if os.Getenv(key) == "" {
			t.Skipf("%s 未设置，跳过真实链路 E2E", key)
		}
	}
	if dsn == "" || secret == "" {
		t.Skip("E2E_DSN / JWT_SECRET 未设置，跳过真实链路 E2E")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 每次从干净状态开始：清表比 drop database 稳（连接池还握着库）。
	for _, table := range []string{"ai_app_departments", "ai_app_studios", "system_configs", "user_roles"} {
		require.NoError(t, db.Exec("TRUNCATE "+table).Error)
	}

	settingsRepo := repository.NewSettingsRepo(db)
	hubRepo := repository.NewAIAppHubRepo(db)
	fetcher := aiapphub.New(aiapphub.Config{
		BaseURL: os.Getenv("EHR_BASE_URL"),
		Prefix:  os.Getenv("EHR_PREFIX"),
		Token:   os.Getenv("EHR_API_TOKEN"),
		OrgID:   os.Getenv("EHR_ORG_ID"),
	})
	svc := service.NewAIAppHubSyncService(fetcher, hubRepo, settingsRepo, service.DefaultSyncRules())

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/sync", middleware.JWTAuth(), handler.NewAIAppHubSyncHandler(svc).Sync)
	srv := httptest.NewServer(engine)
	defer srv.Close()

	adminID := uuid.NewString()
	require.NoError(t, db.Exec("INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'admin', 'company', '', 'e2e')", adminID).Error)
	adminToken := e2eToken(t, secret, adminID, "e2e-admin")

	call := func(tok, body string) (int, map[string]any) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/sync", bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	t.Run("未认证与越权", func(t *testing.T) {
		code, _ := call("", `{"scope":"all"}`)
		require.Equal(t, http.StatusUnauthorized, code)

		nobody := e2eToken(t, secret, uuid.NewString(), "e2e-nobody")
		code, _ = call(nobody, `{"scope":"all"}`)
		require.Equal(t, http.StatusForbidden, code)
	})

	t.Run("全量同步落库", func(t *testing.T) {
		code, body := call(adminToken, `{"scope":"all"}`)
		require.Equal(t, http.StatusOK, code, "%v", body)
		require.Equal(t, float64(99), body["departments"])
		require.Equal(t, float64(15), body["studios"])
		require.Equal(t, float64(2), body["hidden"])

		var depts, studios, hidden int64
		db.Table("ai_app_departments").Count(&depts)
		db.Table("ai_app_studios").Count(&studios)
		db.Table("ai_app_studios").Where("visible = ?", false).Count(&hidden)
		require.Equal(t, int64(99), depts)
		require.Equal(t, int64(15), studios)
		require.Equal(t, int64(2), hidden)

		var renamed string
		db.Table("ai_app_studios").Select("name").
			Where("department_id = ?", "53E83585-6CE3-48EF-8255-520BBAFCB156").Scan(&renamed)
		require.Equal(t, "技术研究工作室", renamed)

		// 只有真的没有事业部祖先的节点（暂存部门子树等 7 个）才允许 studio_id 为空。
		var orphan int64
		db.Raw("SELECT count(*) FROM ai_app_departments d WHERE d.department_type <> 2 AND d.studio_id IS NULL").Scan(&orphan)
		require.Equal(t, int64(7), orphan)

		var status, scope string
		db.Table("system_configs").Select("value").Where("key = ?", "ai_app_hub.last_sync_status").Scan(&status)
		db.Table("system_configs").Select("value").Where("key = ?", "ai_app_hub.last_sync_scope").Scan(&scope)
		require.Equal(t, "success", status)
		require.Equal(t, "all", scope)
	})

	var studioID, leaderPassport string
	db.Raw("SELECT id::text, leader_passport FROM ai_app_studios WHERE department_id IS NOT NULL AND leader_passport <> '' LIMIT 1").
		Row().Scan(&studioID, &leaderPassport)

	t.Run("单部门同步不清空 studio_id", func(t *testing.T) {
		var target string
		db.Raw("SELECT department_id FROM ai_app_departments WHERE department_type <> 2 AND studio_id IS NOT NULL LIMIT 1").Scan(&target)
		require.NotEmpty(t, target)

		var before, after string
		db.Table("ai_app_departments").Select("studio_id").Where("department_id = ?", target).Scan(&before)
		code, body := call(adminToken, fmt.Sprintf(`{"scope":"department","id":"%s"}`, target))
		require.Equal(t, http.StatusOK, code, "%v", body)
		db.Table("ai_app_departments").Select("studio_id").Where("department_id = ?", target).Scan(&after)
		require.NotEmpty(t, before)
		require.Equal(t, before, after)
	})

	t.Run("单工作室同步与负责人权限", func(t *testing.T) {
		require.NotEmpty(t, studioID)
		code, body := call(adminToken, fmt.Sprintf(`{"scope":"studio","id":"%s"}`, studioID))
		require.Equal(t, http.StatusOK, code, "%v", body)

		leader := e2eToken(t, secret, uuid.NewString(), leaderPassport)
		code, body = call(leader, fmt.Sprintf(`{"scope":"studio","id":"%s"}`, studioID))
		require.Equal(t, http.StatusOK, code, "%v", body)

		other := e2eToken(t, secret, uuid.NewString(), "someone-else")
		code, _ = call(other, fmt.Sprintf(`{"scope":"studio","id":"%s"}`, studioID))
		require.Equal(t, http.StatusForbidden, code)
	})

	t.Run("手工工作室与不存在的部门都返回 404", func(t *testing.T) {
		manualID := uuid.NewString()
		require.NoError(t, db.Exec("INSERT INTO ai_app_studios (id, department_id, name, source, visible, sort_order) VALUES (?, NULL, '手工工作室', 'manual', true, 0)", manualID).Error)
		code, _ := call(adminToken, fmt.Sprintf(`{"scope":"studio","id":"%s"}`, manualID))
		require.Equal(t, http.StatusNotFound, code)

		code, _ = call(adminToken, `{"scope":"department","id":"00000000-0000-0000-0000-0000000000ff"}`)
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("并发触发只有一个拿到锁", func(t *testing.T) {
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				codes[i], _ = call(adminToken, `{"scope":"all"}`)
			}(i)
		}
		wg.Wait()
		got := []int{codes[0], codes[1]}
		winner := (got[0] == http.StatusOK && got[1] == http.StatusConflict) ||
			(got[0] == http.StatusConflict && got[1] == http.StatusOK)
		require.True(t, winner, "期望一个 200 一个 409，实际 %v", got)
	})
}
