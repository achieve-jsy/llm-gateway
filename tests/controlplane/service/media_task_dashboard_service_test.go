package service_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件覆盖 E 队 Batch 3 · E1 任务 4 的**看板口径**（service 层）。
//
// 用内存 sqlite（AutoMigrate 直接由 GORM model 建表，不手写 DDL，避免与 model 漂移），
// 绝不依赖真实 Postgres。
//
// 重点不是「SQL 能不能跑」（那由 repository 测试覆盖），而是**口径**：
//   - expired 独立成桶、绝不并入 failed（这条一旦错，运维会把「超时清扫」当成「上游故障」）；
//   - legacy succeeded 折算成 completed 参与成功率分子；
//   - 未结束任务不进分母；未知状态词落 unclassified 且不进分母；
//   - 各分组数组对齐到同一组 kind（前端可直接下标 zip）；
//   - 「无价拒绝」来自 call_logs 而非 media_tasks，且是可查的真实事实而非伪造的 0。

func init() {
	gin.SetMode(gin.TestMode)
}

func newDashboardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// ⚠️ 不能用 AutoMigrate(&model.VideoTask{})：model 的 `default:now()` 在 SQLite 上是语法错误
	// （与 tests/controlplane/repository/media_task_repo_test.go 同因，那里也是手写 DDL）。
	if err := db.Exec(dashboardMediaTaskSchema).Error; err != nil {
		t.Fatalf("create media_tasks schema: %v", err)
	}
	if err := db.AutoMigrate(&dashboardCallLog{}); err != nil {
		t.Fatalf("auto-migrate call_logs: %v", err)
	}
	return db
}

// dashboardMediaTaskSchema 与 model.VideoTask 的列一一对应（表名 = 改名后的 media_tasks）。
const dashboardMediaTaskSchema = `
CREATE TABLE media_tasks (
	task_id TEXT PRIMARY KEY,
	api_key_id TEXT NOT NULL,
	api_key_name TEXT,
	api_key_type TEXT,
	user_id TEXT,
	user_name TEXT,
	model TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	provider_type TEXT NOT NULL,
	status TEXT NOT NULL,
	video_url TEXT,
	request_body TEXT,
	response_body TEXT,
	error_message TEXT,
	created_at DATETIME,
	updated_at DATETIME,
	kind TEXT,
	action TEXT,
	vendor_task_id TEXT,
	progress TEXT,
	usage TEXT,
	cost_micro_usd INTEGER,
	billed_at DATETIME,
	started_at DATETIME,
	finished_at DATETIME,
	poll_failures INTEGER,
	idempotency_key TEXT,
	private_data TEXT,
	dept_id TEXT,
	studio_id TEXT
)`

// dashboardCallLog 只映射看板用到的 call_logs 列。
type dashboardCallLog struct {
	RequestID       string    `gorm:"primaryKey"`
	TS              time.Time `gorm:"column:ts"`
	UserID          string    `gorm:"column:user_id"`
	CallType        string    `gorm:"column:call_type"`
	InboundEndpoint string    `gorm:"column:inbound_endpoint"`
	Status          int       `gorm:"column:status"`
	UsageMissing    bool      `gorm:"column:usage_missing"`
	BudgetBlockCode *string   `gorm:"column:budget_block_code"`
}

func (dashboardCallLog) TableName() string { return "call_logs" }

type dashboardSeed struct {
	taskID       string
	kind         string
	status       string
	costMicroUSD int64
	createdAt    time.Time
	durationSec  int // >0 时同时写 started_at / finished_at
}

func seedDashboardTask(t *testing.T, db *gorm.DB, s dashboardSeed) {
	t.Helper()
	task := &model.VideoTask{
		TaskID:       s.taskID,
		APIKeyID:     "key-" + s.taskID,
		Model:        "kling-v1",
		ProviderName: "kling",
		ProviderType: "kling",
		Status:       s.status,
		Kind:         s.kind,
		CostMicroUSD: s.costMicroUSD,
		CreatedAt:    s.createdAt,
		UpdatedAt:    s.createdAt,
	}
	if s.durationSec > 0 {
		started := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
		finished := started.Add(time.Duration(s.durationSec) * time.Second)
		task.StartedAt = &started
		task.FinishedAt = &finished
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed dashboard task %s: %v", s.taskID, err)
	}
}

func dashboardQuery(t *testing.T, db *gorm.DB, kind string) *service.MediaTaskDashboard {
	t.Helper()
	now := time.Now().UTC()
	svc := service.NewMediaTaskDashboardService(repository.NewMediaTaskStatsRepo(db))
	result, err := svc.GetDashboard(service.MediaTaskDashboardQuery{
		Start: now.Add(-24 * time.Hour),
		End:   now.Add(time.Hour),
		Kind:  kind,
		Scope: repository.VideoTaskScope{IsGlobal: true},
		Range: service.MediaTaskDashboardDefaultRange,
	})
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	return result
}

func successRateOf(t *testing.T, d *service.MediaTaskDashboard, kind string) service.MediaTaskKindSuccessRate {
	t.Helper()
	for _, row := range d.SuccessRate {
		if row.Kind == kind {
			return row
		}
	}
	t.Fatalf("success_rate 缺少 kind=%q 的行：%+v", kind, d.SuccessRate)
	return service.MediaTaskKindSuccessRate{}
}

// ───────────────────── 词表与成功率口径 ─────────────────────

func TestMediaTaskDashboardService_SuccessRateKeepsExpiredSeparateFromFailed(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()

	// video：completed 1 + failed 1 + expired 2 → 成功率 = 1/4，且 expired 必须仍是 2、failed 仍是 1。
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-completed", kind: "video", status: "completed", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-failed", kind: "video", status: "failed", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-expired-1", kind: "video", status: "expired", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-expired-2", kind: "video", status: "expired", createdAt: now})
	// 未结束任务：不计入分母。
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-queued", kind: "video", status: "queued", createdAt: now})
	// legacy 词表：succeeded 折算成 completed，submitted/running 折算成 active。
	seedDashboardTask(t, db, dashboardSeed{taskID: "l-succeeded", kind: "", status: "succeeded", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "l-running", kind: "", status: "running", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "l-submitted", kind: "", status: "submitted", createdAt: now})

	d := dashboardQuery(t, db, "")

	video := successRateOf(t, d, "video")
	if video.Completed != 1 || video.Failed != 1 || video.Expired != 2 {
		t.Fatalf("video 分桶 = completed:%d failed:%d expired:%d，want 1/1/2（expired 不得并入 failed）",
			video.Completed, video.Failed, video.Expired)
	}
	if video.Active != 1 {
		t.Errorf("video active = %d，want 1（queued 不计入分母）", video.Active)
	}
	if video.TerminalTotal != 4 {
		t.Errorf("video terminal_total = %d，want 4", video.TerminalTotal)
	}
	if video.SuccessRate != 0.25 {
		t.Errorf("video success_rate = %v，want 0.25 = 1/(1+1+2)", video.SuccessRate)
	}
	if video.Unclassified != 0 {
		t.Errorf("video unclassified = %d，want 0", video.Unclassified)
	}

	legacy := successRateOf(t, d, "")
	if legacy.Completed != 1 || legacy.Active != 2 || legacy.TerminalTotal != 1 {
		t.Fatalf("legacy(kind='') 行 = completed:%d active:%d terminal:%d，want 1/2/1",
			legacy.Completed, legacy.Active, legacy.TerminalTotal)
	}
	if legacy.SuccessRate != 1 {
		t.Errorf("legacy success_rate = %v，want 1（succeeded 折算成 completed）", legacy.SuccessRate)
	}

	// 原始分布必须同时保留两套词表，且 expired 原值可见。
	rawStatuses := map[string]int64{}
	for _, row := range d.StatusDistribution {
		rawStatuses[row.Status] += row.Count
	}
	for _, want := range []string{"completed", "failed", "expired", "queued", "succeeded", "running", "submitted"} {
		if rawStatuses[want] == 0 {
			t.Errorf("status_distribution 缺少原始状态词 %q（分布必须保留原值）：%+v", want, d.StatusDistribution)
		}
	}
	if rawStatuses["expired"] != 2 {
		t.Errorf("status_distribution 的 expired = %d，want 2", rawStatuses["expired"])
	}

	// kind 顺序统一：image → video → legacy(空串)。
	if got := kindOrder(d); !equalStrings(got, []string{"video", ""}) {
		t.Errorf("success_rate 的 kind 顺序 = %v，want [video \"\"]", got)
	}
}

func TestMediaTaskDashboardService_UnknownStatusGoesToUnclassified(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-completed", kind: "video", status: "completed", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-weird", kind: "video", status: "on_hold_v2", createdAt: now})

	d := dashboardQuery(t, db, "")
	video := successRateOf(t, d, "video")
	if video.Unclassified != 1 {
		t.Fatalf("未知状态必须落 unclassified（否则会被静默算进成功率）：%+v", video)
	}
	// 未知状态**不进分母**：成功率仍是 1/1，而不是 1/2。
	if video.TerminalTotal != 1 || video.SuccessRate != 1 {
		t.Errorf("unclassified 不得进成功率分母：terminal=%d rate=%v", video.TerminalTotal, video.SuccessRate)
	}
}

func TestMediaTaskDashboardService_SuccessRateZeroDenominator(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-queued", kind: "video", status: "queued", createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-progress", kind: "video", status: "in_progress", createdAt: now})

	d := dashboardQuery(t, db, "")
	video := successRateOf(t, d, "video")
	if video.TerminalTotal != 0 {
		t.Fatalf("terminal_total = %d，want 0", video.TerminalTotal)
	}
	if video.SuccessRate != 0 {
		t.Errorf("分母为 0 时成功率必须是 0（不能是 NaN/null）：%v", video.SuccessRate)
	}
}

// ───────────────────── 数组对齐与零值补行 ─────────────────────

func TestMediaTaskDashboardService_AllKindArraysAligned(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	seedDashboardTask(t, db, dashboardSeed{taskID: "i-1", kind: "image", status: "completed", costMicroUSD: 40_000, createdAt: now, durationSec: 10})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-1", kind: "video", status: "failed", costMicroUSD: 7, createdAt: now, durationSec: 20})
	seedDashboardTask(t, db, dashboardSeed{taskID: "l-1", kind: "", status: "succeeded", createdAt: now})

	d := dashboardQuery(t, db, "")

	want := []string{"image", "video", ""}
	if got := kindOrder(d); !equalStrings(got, want) {
		t.Fatalf("success_rate kind 顺序 = %v，want %v", got, want)
	}
	if got := durationOrder(d); !equalStrings(got, want) {
		t.Fatalf("duration_seconds kind 顺序 = %v，want %v", got, want)
	}
	if got := costOrder(d); !equalStrings(got, want) {
		t.Fatalf("cost_micro_usd kind 顺序 = %v，want %v", got, want)
	}
	if got := anomalyOrder(d); !equalStrings(got, want) {
		t.Fatalf("completed_without_cost.by_kind 顺序 = %v，want %v", got, want)
	}

	// 成本：全程整数微美元。
	costByKind := map[string]int64{}
	for _, row := range d.CostMicroUSD {
		costByKind[row.Kind] = row.CostMicroUSD
	}
	if costByKind["image"] != 40_000 || costByKind["video"] != 7 || costByKind[""] != 0 {
		t.Fatalf("成本分组 = %v，want image=40000 video=7 legacy=0", costByKind)
	}

	// 耗时：legacy 行没有时间戳 → 样本数 0，但**行仍然存在**（对齐），并暴露被排除行数。
	durationByKind := map[string]service.MediaTaskKindDuration{}
	for _, row := range d.DurationSeconds {
		durationByKind[row.Kind] = row
	}
	if got := durationByKind["image"].SampleCount; got != 1 {
		t.Errorf("image 耗时样本数 = %d，want 1", got)
	}
	if got := durationByKind["video"].P50Seconds; got != 20 {
		t.Errorf("video p50 = %v，want 20（分位值四舍五入到毫秒，必须无浮点尾差）", got)
	}
	legacy := durationByKind[""]
	if legacy.SampleCount != 0 {
		t.Errorf("legacy 耗时样本数 = %d，want 0", legacy.SampleCount)
	}
	if legacy.ExcludedMissingTimestamps != 1 {
		t.Errorf("legacy 被排除的缺时间戳行数 = %d，want 1（必须显式暴露）", legacy.ExcludedMissingTimestamps)
	}

	// totals：image completed + video failed + legacy succeeded 都是终态 → 3。
	if d.Totals.Tasks != 3 || d.Totals.TerminalTasks != 3 || d.Totals.ActiveTasks != 0 {
		t.Errorf("totals = %+v，want tasks=3 terminal=3 active=0", d.Totals)
	}
}

func TestMediaTaskDashboardService_KindFilterEmitsZeroRowWhenEmpty(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	seedDashboardTask(t, db, dashboardSeed{taskID: "i-1", kind: "image", status: "completed", createdAt: now})

	d := dashboardQuery(t, db, "video")

	if got := kindOrder(d); !equalStrings(got, []string{"video"}) {
		t.Fatalf("显式过滤 kind=video 时各数组应回一行零值：%v", got)
	}
	video := successRateOf(t, d, "video")
	if video.TerminalTotal != 0 || video.SuccessRate != 0 {
		t.Errorf("零值行 = %+v", video)
	}
	if len(d.StatusDistribution) != 0 {
		t.Errorf("零值 kind 不应造出假的 status 分布行：%+v", d.StatusDistribution)
	}
	if d.Totals.Tasks != 0 {
		t.Errorf("totals.tasks = %d，want 0", d.Totals.Tasks)
	}
}

// ───────────────────── 异常计数 ─────────────────────

func TestMediaTaskDashboardService_CompletedWithoutCostIgnoresLegacySucceeded(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-anon", kind: "video", status: "completed", costMicroUSD: 0, createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "v-priced", kind: "video", status: "completed", costMicroUSD: 1, createdAt: now})
	seedDashboardTask(t, db, dashboardSeed{taskID: "l-succeeded", kind: "", status: "succeeded", costMicroUSD: 0, createdAt: now})

	d := dashboardQuery(t, db, "")
	if d.Anomalies.CompletedWithoutCost.Total != 1 {
		t.Fatalf("成功但无成本 = %d，want 1（legacy succeeded 不纳入，避免存量基线永久误报）",
			d.Anomalies.CompletedWithoutCost.Total)
	}
	byKind := map[string]int64{}
	for _, row := range d.Anomalies.CompletedWithoutCost.ByKind {
		byKind[row.Kind] = row.Count
	}
	if byKind["video"] != 1 || byKind[""] != 0 {
		t.Errorf("成功但无成本 by_kind = %v，want video=1 legacy=0", byKind)
	}
}

func TestMediaTaskDashboardService_PricingMissingRejectionsComeFromCallLogs(t *testing.T) {
	db := newDashboardDB(t)
	now := time.Now().UTC()
	blocked := "tier_budget_exceeded"

	// 关键：**media_tasks 一行都没有** —— 被拒请求不落任务表，指标必须仍能查出数来。
	callLogs := []dashboardCallLog{
		{RequestID: "rej-1", TS: now, CallType: "image.generations", InboundEndpoint: "/v1/images/generations", Status: 402, UsageMissing: true},
		{RequestID: "rej-2", TS: now, CallType: "image.generations", InboundEndpoint: "/v1/images/generations", Status: 402, UsageMissing: true},
		{RequestID: "rej-3", TS: now, CallType: "video.submit", InboundEndpoint: "/v1/videos", Status: 402, UsageMissing: true},
		{RequestID: "budget", TS: now, CallType: "video.submit", InboundEndpoint: "/v1/videos", Status: 402, UsageMissing: true, BudgetBlockCode: &blocked},
		{RequestID: "admitted", TS: now, CallType: "video.submit", InboundEndpoint: "/v1/videos", Status: 200, UsageMissing: true},
	}
	for _, row := range callLogs {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed call_log %s: %v", row.RequestID, err)
		}
	}

	d := dashboardQuery(t, db, "")
	rej := d.Anomalies.PricingMissingRejections

	if !rej.Available {
		t.Fatal("无价拒绝必须标记 available=true（事实被持久化在 call_logs，不是不可得）")
	}
	if rej.Source != "call_logs" {
		t.Errorf("source = %q，want call_logs", rej.Source)
	}
	if rej.Total != 3 {
		t.Fatalf("无价拒绝总数 = %d，want 3（预算 402 与放行都不算）：%+v", rej.Total, rej.ByCallType)
	}
	byType := map[string]int64{}
	for _, row := range rej.ByCallType {
		byType[row.CallType] = row.Count
	}
	if byType["image.generations"] != 2 || byType["video.submit"] != 1 {
		t.Errorf("by_call_type = %v，want image.generations=2 video.submit=1", byType)
	}
	if rej.Criteria == "" {
		t.Error("判据必须随响应下发（口径不清是看板争议源）")
	}
	if d.Totals.Tasks != 0 {
		t.Errorf("被拒请求不落 media_tasks，totals.tasks 应为 0，got %d", d.Totals.Tasks)
	}
}

// ───────────────────── 口径自述与窗口回显 ─────────────────────

func TestMediaTaskDashboardService_CaliberIsExplicit(t *testing.T) {
	db := newDashboardDB(t)
	svc := service.NewMediaTaskDashboardService(repository.NewMediaTaskStatsRepo(db))
	now := time.Now().UTC()
	result, err := svc.GetDashboard(service.MediaTaskDashboardQuery{
		Start:          now.Add(-time.Hour),
		End:            now,
		Scope:          repository.VideoTaskScope{IsGlobal: true},
		Range:          "1h",
		RangeDefaulted: false,
		Clamped:        true,
	})
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}

	caliber := result.Caliber
	if caliber.SuccessRateFormula != "completed / (completed + failed + expired)" {
		t.Errorf("成功率公式必须逐字冻结：%q", caliber.SuccessRateFormula)
	}
	if caliber.ActiveDefinition == "" || caliber.ExpiredHandling == "" || caliber.DurationBasis == "" || caliber.CostUnit == "" ||
		caliber.PercentileMethod == "" || caliber.CompletedWithoutCostDefinition == "" {
		t.Errorf("口径自述不得留空：%+v", caliber)
	}
	if len(caliber.DurationStatuses) != 4 {
		t.Errorf("耗时统计的状态集必须显式列出（新链路 + legacy）：%v", caliber.DurationStatuses)
	}

	// 窗口与钳制标记必须原样回显，前端据此提示「已按上限截断」。
	if !result.Window.Clamped || result.Window.Range != "1h" {
		t.Errorf("window = %+v，want range=1h clamped=true", result.Window)
	}
	if result.Window.MaxRange != service.MediaTaskDashboardMaxRange {
		t.Errorf("max_range = %q，want %q", result.Window.MaxRange, service.MediaTaskDashboardMaxRange)
	}
	if !result.Window.Start.Equal(now.Add(-time.Hour)) || !result.Window.End.Equal(now) {
		t.Errorf("window 时间未回显：%+v", result.Window)
	}
}

// ───────────────────── 小工具 ─────────────────────

func kindOrder(d *service.MediaTaskDashboard) []string {
	out := make([]string, 0, len(d.SuccessRate))
	for _, row := range d.SuccessRate {
		out = append(out, row.Kind)
	}
	return out
}

func durationOrder(d *service.MediaTaskDashboard) []string {
	out := make([]string, 0, len(d.DurationSeconds))
	for _, row := range d.DurationSeconds {
		out = append(out, row.Kind)
	}
	return out
}

func costOrder(d *service.MediaTaskDashboard) []string {
	out := make([]string, 0, len(d.CostMicroUSD))
	for _, row := range d.CostMicroUSD {
		out = append(out, row.Kind)
	}
	return out
}

func anomalyOrder(d *service.MediaTaskDashboard) []string {
	out := make([]string, 0, len(d.Anomalies.CompletedWithoutCost.ByKind))
	for _, row := range d.Anomalies.CompletedWithoutCost.ByKind {
		out = append(out, row.Kind)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
