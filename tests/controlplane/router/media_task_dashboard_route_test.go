package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件覆盖 E 队 Batch 3 · E1 任务 4「媒体任务维度运维看板」在**真实 `router.Setup`
// 装配下**的行为：路径、鉴权、参数校验、窗口钳制与响应契约（字段名/类型）。
//
// 【效力边界】内存 sqlite + 不可达 Redis：证明的是「装配后的路由/鉴权/契约形状」，
// 不是生产 PG 下的行为（PG 专属行为由既有迁移演练与 GORM↔SQL 对拍覆盖）。
// SQL 聚合语义与口径分别由 tests/controlplane/repository 与 tests/controlplane/service 覆盖。

const mediaTaskDashboardPath = "/api/v1/ops/media-tasks/dashboard"

func mediaTaskDashboardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := assemblyDB(t) // call_logs / user_roles / call_traces（复用同包既有装配）
	// 复用同包 media_task_internal_test.go 的 DDL 常量：表名是改名后的 media_tasks。
	require.NoError(t, db.Exec(mediaInternalTaskDDL).Error)
	return db
}

func mediaTaskDashboardEngine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	t.Setenv("TRACE_BODY_ROOT", t.TempDir())
	return assemblyEngine(t, db)
}

// seedDashboardMediaTask 落一条媒体任务（时间全部落在默认 24h 窗口内）。
func seedDashboardMediaTask(t *testing.T, db *gorm.DB, taskID, kind, status string, costMicroUSD int64, createdAt time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&model.VideoTask{
		TaskID:       taskID,
		APIKeyID:     "key-" + taskID,
		Model:        "kling-v1",
		ProviderName: "kling",
		ProviderType: "kling",
		Status:       status,
		Kind:         kind,
		CostMicroUSD: costMicroUSD,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}).Error)
}

func dashboardGet(t *testing.T, engine *gin.Engine, target, bearer string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func dashboardData(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return data
}

func dashboardRows(t *testing.T, data map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := data[key].([]any)
	if !ok {
		t.Fatalf("响应缺少 %s 数组: %v", key, data)
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s 的元素不是对象: %v", key, item)
		}
		rows = append(rows, row)
	}
	return rows
}

func requireKeys(t *testing.T, obj map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := obj[key]; !ok {
			t.Errorf("缺少字段 %q（契约漂移）：%v", key, obj)
		}
	}
}

// ───────────────────────────── 鉴权（沿用既有 guard）─────────────────────────────

func TestMediaTaskDashboardRouteAuthGate(t *testing.T) {
	db := mediaTaskDashboardDB(t)
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: assemblyAdminUserID, Role: orgmodel.RoleAdmin,
		ScopeType: orgmodel.ScopeCompany, GrantedBy: "dashboard-test",
	}).Error)
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: assemblyPlainUserID, Role: orgmodel.RoleUser,
		ScopeType: orgmodel.ScopeSelf, GrantedBy: "dashboard-test",
	}).Error)
	engine := mediaTaskDashboardEngine(t, db)

	t.Run("无 token", func(t *testing.T) {
		code, _ := dashboardGet(t, engine, mediaTaskDashboardPath, "")
		require.Equal(t, http.StatusUnauthorized, code, "无 token 必须被既有 JWTAuth 拦下")
	})

	t.Run("有 token 但无 usage:view:scoped", func(t *testing.T) {
		code, resp := dashboardGet(t, engine, mediaTaskDashboardPath, assemblyJWT(t, assemblyPlainUserID))
		require.Equal(t, http.StatusForbidden, code, "普通用户必须被既有 RBAC guard 拒绝，响应 %v", resp)
		require.Equal(t, "org.forbidden", resp["code"], "拒绝必须来自既有 guard（不得新建鉴权体系）")
	})

	t.Run("admin 放行", func(t *testing.T) {
		code, resp := dashboardGet(t, engine, mediaTaskDashboardPath, assemblyJWT(t, assemblyAdminUserID))
		require.Equal(t, http.StatusOK, code, "admin 必须放行（门不是恒拒绝），响应 %v", resp)
	})
}

// ───────────────────────────── 冻结契约（字段名/类型）─────────────────────────────

func TestMediaTaskDashboardRouteContract(t *testing.T) {
	db := mediaTaskDashboardDB(t)
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: assemblyAdminUserID, Role: orgmodel.RoleAdmin,
		ScopeType: orgmodel.ScopeCompany, GrantedBy: "dashboard-test",
	}).Error)

	now := time.Now().UTC()
	seedDashboardMediaTask(t, db, "v-done", model.MediaTaskKindVideo, model.MediaTaskStatusCompleted, 1_234_567, now)
	seedDashboardMediaTask(t, db, "v-expired", model.MediaTaskKindVideo, model.MediaTaskStatusExpired, 0, now)
	seedDashboardMediaTask(t, db, "i-done", model.MediaTaskKindImage, model.MediaTaskStatusCompleted, 40_000, now)
	seedDashboardMediaTask(t, db, "legacy-ok", "", model.MediaTaskLegacyStatusSucceeded, 0, now)
	// 成功但无成本（新链路）
	seedDashboardMediaTask(t, db, "v-nocost", model.MediaTaskKindVideo, model.MediaTaskStatusCompleted, 0, now)
	// 无价拒绝只存在于 call_logs（media_tasks 里没有对应行）
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "rej-1", TS: now, Status: 402, UsageMissing: true,
		CallType: "image.generations", InboundEndpoint: "/v1/images/generations",
	}).Error)

	engine := mediaTaskDashboardEngine(t, db)
	code, resp := dashboardGet(t, engine, mediaTaskDashboardPath, assemblyJWT(t, assemblyAdminUserID))
	require.Equal(t, http.StatusOK, code, "响应 %v", resp)
	data := dashboardData(t, resp)

	// 顶层字段名冻结（snake_case）。
	requireKeys(t, data, "window", "totals", "status_distribution", "success_rate",
		"duration_seconds", "cost_micro_usd", "anomalies", "caliber")

	// window
	window := data["window"].(map[string]any)
	requireKeys(t, window, "start", "end", "range", "max_range", "range_defaulted", "clamped")
	require.Equal(t, "24h", window["range"], "未传时间参数时默认 24h")
	require.Equal(t, "90d", window["max_range"], "必须回显强制上限")
	require.Equal(t, true, window["range_defaulted"], "必须标记用了默认窗口")
	require.Equal(t, false, window["clamped"], "默认窗口不应被标记截断")

	// totals
	totals := data["totals"].(map[string]any)
	requireKeys(t, totals, "tasks", "terminal_tasks", "active_tasks")
	require.Equal(t, float64(5), totals["tasks"])
	require.Equal(t, float64(5), totals["terminal_tasks"])

	// status_distribution：保留原始状态词（含 legacy 与 expired）。
	statusSeen := map[string]bool{}
	for _, row := range dashboardRows(t, data, "status_distribution") {
		requireKeys(t, row, "kind", "status", "count")
		statusSeen[row["status"].(string)] = true
	}
	for _, want := range []string{"completed", "expired", "succeeded"} {
		require.True(t, statusSeen[want], "status_distribution 必须保留原值 %q：%v", want, data["status_distribution"])
	}

	// success_rate：expired 独立成桶。
	rateRows := dashboardRows(t, data, "success_rate")
	var videoRate map[string]any
	for _, row := range rateRows {
		requireKeys(t, row, "kind", "completed", "failed", "expired", "active",
			"terminal_total", "unclassified", "success_rate")
		if row["kind"] == "video" {
			videoRate = row
		}
	}
	require.NotNil(t, videoRate, "必须有 video 分组：%v", rateRows)
	require.Equal(t, float64(2), videoRate["completed"], "video completed 应为 2（v-done + v-nocost）")
	require.Equal(t, float64(0), videoRate["failed"], "failed 不得吞掉 expired")
	require.Equal(t, float64(1), videoRate["expired"], "expired 必须独立可见")
	require.Equal(t, float64(3), videoRate["terminal_total"])
	require.Equal(t, float64(2)/float64(3), videoRate["success_rate"], "成功率 = completed/(completed+failed+expired)")

	// duration_seconds：样本数与被排除行数都必须暴露。
	for _, row := range dashboardRows(t, data, "duration_seconds") {
		requireKeys(t, row, "kind", "sample_count", "p50_seconds", "p95_seconds", "p99_seconds",
			"excluded_missing_timestamps")
	}

	// cost_micro_usd：整数微美元；legacy 行也要有零值行（数组对齐）。
	costRows := dashboardRows(t, data, "cost_micro_usd")
	costByKind := map[string]float64{}
	for _, row := range costRows {
		requireKeys(t, row, "kind", "cost_micro_usd", "tasks", "tasks_with_cost")
		costByKind[row["kind"].(string)] = row["cost_micro_usd"].(float64)
	}
	require.Equal(t, float64(1_234_567), costByKind["video"], "video 成本必须是整数微美元精确值")
	require.Equal(t, float64(40_000), costByKind["image"])
	require.Equal(t, float64(0), costByKind[""], "legacy（kind 空串）必须有零值行")

	// anomalies
	anomalies := data["anomalies"].(map[string]any)
	requireKeys(t, anomalies, "completed_without_cost", "pricing_missing_rejections")

	noCost := anomalies["completed_without_cost"].(map[string]any)
	requireKeys(t, noCost, "total", "by_kind")
	require.Equal(t, float64(1), noCost["total"], "成功但无成本应为 1（v-nocost）")

	rej := anomalies["pricing_missing_rejections"].(map[string]any)
	requireKeys(t, rej, "available", "source", "total", "by_call_type", "criteria", "note")
	require.Equal(t, true, rej["available"], "无价拒绝必须标记为可得（事实在 call_logs）")
	require.Equal(t, "call_logs", rej["source"])
	require.Equal(t, float64(1), rej["total"])
	require.NotEmpty(t, rej["criteria"], "判据必须随响应下发")

	// caliber：口径必须逐字可见。
	caliber := data["caliber"].(map[string]any)
	requireKeys(t, caliber, "success_rate_formula", "completed_definition", "active_definition", "expired_handling",
		"duration_basis", "duration_statuses", "percentile_method", "cost_unit",
		"legacy_kind_note", "completed_without_cost_definition", "window_note")
	require.Equal(t, "completed / (completed + failed + expired)", caliber["success_rate_formula"])
}

// ───────────────────────────── 参数校验 ─────────────────────────────

func TestMediaTaskDashboardRouteParamValidation(t *testing.T) {
	db := mediaTaskDashboardDB(t)
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: assemblyAdminUserID, Role: orgmodel.RoleAdmin,
		ScopeType: orgmodel.ScopeCompany, GrantedBy: "dashboard-test",
	}).Error)
	engine := mediaTaskDashboardEngine(t, db)
	bearer := assemblyJWT(t, assemblyAdminUserID)

	cases := []struct {
		name   string
		query  string
		reason string
	}{
		{"非法 kind", "?kind=audio", "kind 只允许 image|video"},
		{"非法 range", "?range=365d", "range 是白名单，超上限的跨度不得接受"},
		{"非法 start", "?start=not-a-time", "时间格式必须是 RFC3339 / YYYY-MM-DD"},
		{"非法 end", "?end=13/45/2026", "时间格式必须是 RFC3339 / YYYY-MM-DD"},
		{"start 晚于 end", "?start=2026-10-16T00:00:00Z&end=2026-10-15T00:00:00Z", "窗口必须左闭右开且非空"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := dashboardGet(t, engine, mediaTaskDashboardPath+tc.query, bearer)
			require.Equal(t, http.StatusBadRequest, code, "%s：%v", tc.reason, resp)
			require.NotEmpty(t, resp["error"], "400 必须带可读错误信息")
		})
	}
}

// ───────────────────────────── 窗口上限与来源 ─────────────────────────────

func TestMediaTaskDashboardRouteWindowClampAndOverride(t *testing.T) {
	db := mediaTaskDashboardDB(t)
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: assemblyAdminUserID, Role: orgmodel.RoleAdmin,
		ScopeType: orgmodel.ScopeCompany, GrantedBy: "dashboard-test",
	}).Error)

	now := time.Now().UTC()
	seedDashboardMediaTask(t, db, "recent", model.MediaTaskKindVideo, model.MediaTaskStatusCompleted, 1, now)
	// 100 天前的任务：任何窗口都不得越过 90d 上限把它捞回来（防全表扫描的硬护栏）。
	seedDashboardMediaTask(t, db, "ancient", model.MediaTaskKindVideo, model.MediaTaskStatusCompleted, 2, now.Add(-100*24*time.Hour))

	engine := mediaTaskDashboardEngine(t, db)
	bearer := assemblyJWT(t, assemblyAdminUserID)

	t.Run("超上限被截断", func(t *testing.T) {
		start := now.Add(-200 * 24 * time.Hour).Format(time.RFC3339)
		code, resp := dashboardGet(t, engine, mediaTaskDashboardPath+"?start="+start, bearer)
		require.Equal(t, http.StatusOK, code, "响应 %v", resp)
		data := dashboardData(t, resp)
		window := data["window"].(map[string]any)
		require.Equal(t, true, window["clamped"], "超过 90d 必须标记截断：%v", window)

		startAt, err := time.Parse(time.RFC3339, window["start"].(string))
		require.NoError(t, err)
		endAt, err := time.Parse(time.RFC3339, window["end"].(string))
		require.NoError(t, err)
		require.LessOrEqual(t, endAt.Sub(startAt), 90*24*time.Hour, "生效窗口不得超过 90d：%v → %v", startAt, endAt)

		require.Equal(t, float64(1), data["totals"].(map[string]any)["tasks"],
			"100 天前的任务必须落在窗口外（上限拦截生效）")
	})

	t.Run("start/end 覆盖 range 并回显 custom", func(t *testing.T) {
		start := now.Add(-2 * time.Hour).Format(time.RFC3339)
		// end 刻意留在过去：未来时刻会被钳到「现在」并置 clamped=true（另一条已覆盖的行为）。
		end := now.Add(-time.Minute).Format(time.RFC3339)
		code, resp := dashboardGet(t, engine,
			mediaTaskDashboardPath+"?range=7d&start="+start+"&end="+end, bearer)
		require.Equal(t, http.StatusOK, code, "响应 %v", resp)
		window := dashboardData(t, resp)["window"].(map[string]any)
		require.Equal(t, "custom", window["range"], "显式 start/end 优先于 range")
		require.Equal(t, false, window["range_defaulted"])
		require.Equal(t, false, window["clamped"])
	})

	t.Run("kind 过滤只回一个分组", func(t *testing.T) {
		code, resp := dashboardGet(t, engine, mediaTaskDashboardPath+"?kind=image", bearer)
		require.Equal(t, http.StatusOK, code, "响应 %v", resp)
		data := dashboardData(t, resp)
		rows := dashboardRows(t, data, "success_rate")
		require.Len(t, rows, 1, "显式 kind 过滤时各分组数组只应有一行（缺失也补零值）：%v", rows)
		require.Equal(t, "image", rows[0]["kind"])
		require.Equal(t, float64(0), rows[0]["terminal_total"], "窗口内没有 image 任务 → 零值行")
	})
}
