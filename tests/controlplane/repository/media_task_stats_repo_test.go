package repository_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"gorm.io/gorm"
)

// 本文件覆盖 E 队 Batch 3 · E1 任务 4「媒体任务维度运维看板」的**只读聚合仓储**。
//
// 用内存 sqlite（与 tests/ 既有范式一致，绝不依赖真实 Postgres）。
// 表结构手写并与 model.VideoTask 的列一一对应；表名是改名后的 media_tasks。
//
// 覆盖点：
//   - status 词表**保留原值**（含 legacy submitted/running/succeeded 与 expired 独立成桶）；
//   - 成本为整数微美元、SUM 全程 int64；
//   - 「成功但无成本」只算新链路 completed（legacy succeeded 不纳入，见仓储注释）；
//   - 耗时分位的 nearest-rank 语义 + 「缺时间戳」行的排除与单独暴露；
//   - 时间窗口是半开区间 [start, end) 且按 created_at 归属；
//   - kind 过滤 + 归属范围（scope）过滤；
//   - 「无价拒绝」从 call_logs 取数（402 + usage_missing + 非预算拦截 + 媒体创建端点）。

// mediaTaskStatsCallLogSchema 只建看板用到的 call_logs 列（够用即可，不复制整表）。
const mediaTaskStatsCallLogSchema = `
CREATE TABLE call_logs (
	request_id TEXT PRIMARY KEY,
	ts DATETIME NOT NULL,
	user_id TEXT,
	dept_id TEXT,
	studio_id TEXT,
	call_type TEXT,
	inbound_endpoint TEXT,
	status INTEGER NOT NULL DEFAULT 0,
	usage_missing INTEGER NOT NULL DEFAULT 0,
	budget_block_code TEXT
)`

func newMediaTaskStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newMediaTaskDB(t) // 复用同包既有的 media_tasks / media_task_events 建表
	if err := db.Exec(mediaTaskStatsCallLogSchema).Error; err != nil {
		t.Fatalf("create call_logs schema: %v", err)
	}
	return db
}

// mediaTaskStatsSeed 是看板测试的任务种子（只填统计关心的字段）。
type mediaTaskStatsSeed struct {
	taskID       string
	kind         string
	status       string
	costMicroUSD int64
	createdAt    time.Time
	startedAt    *time.Time
	finishedAt   *time.Time
	userID       string
}

func seedStatsTask(t *testing.T, db *gorm.DB, s mediaTaskStatsSeed) {
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
		StartedAt:    s.startedAt,
		FinishedAt:   s.finishedAt,
		UserID:       s.userID,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed stats task %s: %v", s.taskID, err)
	}
}

// statsTestWindow 返回一个把「现在」包在中间的半开窗口。
func statsTestWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	return now.Add(-24 * time.Hour), now.Add(time.Hour)
}

var statsGlobalScope = repository.VideoTaskScope{IsGlobal: true}

// ───────────────────── 指标 1：kind × status 原始分布 ─────────────────────

func TestMediaTaskStatsRepo_StatusCountsPreserveRawStatusWords(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	// 新链路词表 + legacy Seedance 词表**并存**，看板必须两套都认且原值返回。
	seeds := []mediaTaskStatsSeed{
		{taskID: "v-queued", kind: "video", status: model.MediaTaskStatusQueued, createdAt: now},
		{taskID: "v-in-progress", kind: "video", status: model.MediaTaskStatusInProgress, createdAt: now},
		{taskID: "v-completed", kind: "video", status: model.MediaTaskStatusCompleted, createdAt: now},
		{taskID: "v-failed", kind: "video", status: model.MediaTaskStatusFailed, createdAt: now},
		{taskID: "v-expired", kind: "video", status: model.MediaTaskStatusExpired, createdAt: now},
		{taskID: "i-completed", kind: "image", status: model.MediaTaskStatusCompleted, createdAt: now},
		// legacy Seedance 行：kind 为空串（扩列前创建，刻意不回溯填充）。
		{taskID: "legacy-submitted", kind: "", status: model.MediaTaskLegacyStatusSubmitted, createdAt: now},
		{taskID: "legacy-running", kind: "", status: model.MediaTaskLegacyStatusRunning, createdAt: now},
		{taskID: "legacy-succeeded", kind: "", status: model.MediaTaskLegacyStatusSucceeded, createdAt: now},
	}
	for _, s := range seeds {
		seedStatsTask(t, db, s)
	}

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.StatusCounts(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("StatusCounts: %v", err)
	}

	got := map[string]int64{}
	for _, row := range rows {
		got[row.Kind+"|"+row.Status] = row.Count
	}
	want := map[string]int64{
		"video|queued":      1,
		"video|in_progress": 1,
		"video|completed":   1,
		"video|failed":      1,
		"video|expired":     1,
		"image|completed":   1,
		"|submitted":        1,
		"|running":          1,
		"|succeeded":        1,
	}
	if len(got) != len(want) {
		t.Fatalf("分组数不符：got %d 组 %v，want %d 组", len(got), got, len(want))
	}
	for key, count := range want {
		if got[key] != count {
			t.Errorf("分组 %q 计数 = %d，want %d", key, got[key], count)
		}
	}

	// 🔴 expired 必须能独立读出，不能被合并进 failed。
	if got["video|expired"] != 1 || got["video|failed"] != 1 {
		t.Fatalf("expired 与 failed 必须分别计数：expired=%d failed=%d", got["video|expired"], got["video|failed"])
	}
}

func TestMediaTaskStatsRepo_StatusCountsKindFilterAndHalfOpenWindow(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()

	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "in-window-left", kind: "video", status: "completed", createdAt: start})
	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "in-window-right", kind: "video", status: "completed", createdAt: end.Add(-time.Second)})
	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "before-window", kind: "video", status: "completed", createdAt: start.Add(-time.Second)})
	// 右端**不含**（半开区间）：恰好落在 end 的行必须被排除，否则相邻窗口会重复计数。
	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "at-end", kind: "video", status: "completed", createdAt: end})
	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "image-in-window", kind: "image", status: "completed", createdAt: start.Add(time.Minute)})

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.StatusCounts(start, end, "video", statsGlobalScope)
	if err != nil {
		t.Fatalf("StatusCounts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("kind=video 应只有一组，got %+v", rows)
	}
	if rows[0].Kind != "video" || rows[0].Count != 2 {
		t.Fatalf("kind=video 窗口内计数 = %+v，want kind=video count=2（左闭右开）", rows[0])
	}
}

func TestMediaTaskStatsRepo_ScopeFiltersByUser(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "u1-a", kind: "video", status: "completed", createdAt: now, userID: "u1"})
	seedStatsTask(t, db, mediaTaskStatsSeed{taskID: "u2-a", kind: "video", status: "failed", createdAt: now, userID: "u2"})

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.StatusCounts(start, end, "", repository.VideoTaskScope{UserID: "u1"})
	if err != nil {
		t.Fatalf("StatusCounts: %v", err)
	}
	if len(rows) != 1 || rows[0].Count != 1 || rows[0].Status != "completed" {
		t.Fatalf("scope=user:u1 应只看到自己的 1 条 completed，got %+v", rows)
	}

	// 空 scope（既非全局、又没有任何归属）必须收敛为「看不到任何数据」，不能默认放开。
	empty, err := repo.StatusCounts(start, end, "", repository.VideoTaskScope{})
	if err != nil {
		t.Fatalf("StatusCounts(empty scope): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空 scope 必须查不到数据（1=0 护栏），got %+v", empty)
	}
}

// ───────────────────── 指标 4：成本汇总（整数微美元）─────────────────────

func TestMediaTaskStatsRepo_CostByKindIntegerSum(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	seeds := []mediaTaskStatsSeed{
		{taskID: "v-1", kind: "video", status: "completed", costMicroUSD: 1_234_567, createdAt: now},
		{taskID: "v-2", kind: "video", status: "completed", costMicroUSD: 2, createdAt: now},
		{taskID: "v-3", kind: "video", status: "failed", costMicroUSD: 0, createdAt: now},
		{taskID: "i-1", kind: "image", status: "completed", costMicroUSD: 40_000, createdAt: now},
		{taskID: "l-1", kind: "", status: "succeeded", costMicroUSD: 0, createdAt: now},
	}
	for _, s := range seeds {
		seedStatsTask(t, db, s)
	}

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.CostByKind(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("CostByKind: %v", err)
	}
	byKind := map[string]repository.MediaTaskCostRow{}
	for _, row := range rows {
		byKind[row.Kind] = row
	}

	if got := byKind["video"].CostMicroUSD; got != 1_234_569 {
		t.Errorf("video 成本 = %d，want 1234569（整数微美元精确求和）", got)
	}
	if got := byKind["video"].Tasks; got != 3 {
		t.Errorf("video 任务数 = %d，want 3", got)
	}
	if got := byKind["video"].TasksWithCost; got != 2 {
		t.Errorf("video 有成本任务数 = %d，want 2", got)
	}
	if got := byKind["image"].CostMicroUSD; got != 40_000 {
		t.Errorf("image 成本 = %d，want 40000", got)
	}
	if got := byKind[""].CostMicroUSD; got != 0 {
		t.Errorf("legacy（kind 空串）成本 = %d，want 0", got)
	}
}

// ───────────────────── 指标 5a：成功但无成本 ─────────────────────

func TestMediaTaskStatsRepo_CompletedWithoutCostExcludesLegacySucceeded(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	seeds := []mediaTaskStatsSeed{
		{taskID: "anon-1", kind: "video", status: "completed", costMicroUSD: 0, createdAt: now},
		{taskID: "anon-2", kind: "video", status: "completed", costMicroUSD: 0, createdAt: now},
		{taskID: "priced", kind: "video", status: "completed", costMicroUSD: 500_000, createdAt: now},
		{taskID: "failed", kind: "video", status: "failed", costMicroUSD: 0, createdAt: now},
		// legacy succeeded 成本恒为 0（成本列存在之前的历史行）——**不得**计入异常，
		// 否则该指标会背上永不消失的存量基线。
		{taskID: "legacy", kind: "", status: "succeeded", costMicroUSD: 0, createdAt: now},
		{taskID: "legacy-no-kind", kind: "", status: "succeeded", costMicroUSD: 0, createdAt: now},
	}
	for _, s := range seeds {
		seedStatsTask(t, db, s)
	}

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.CompletedWithoutCost(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("CompletedWithoutCost: %v", err)
	}
	if len(rows) != 1 || rows[0].Kind != "video" || rows[0].Count != 2 {
		t.Fatalf("成功但无成本应只统计新链路 completed：got %+v，want [{video 2}]", rows)
	}
}

// ───────────────────── 指标 3：耗时分位 ─────────────────────

func TestMediaTaskStatsRepo_DurationPercentilesNearestRank(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	created := time.Now().UTC().Add(-time.Hour)

	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	withDuration := func(taskID, kind, status string, seconds int) mediaTaskStatsSeed {
		started := base
		finished := base.Add(time.Duration(seconds) * time.Second)
		return mediaTaskStatsSeed{
			taskID: taskID, kind: kind, status: status, createdAt: created,
			startedAt: &started, finishedAt: &finished,
		}
	}

	// video：20 个样本，耗时 1..20 秒。
	// nearest-rank：P50 = ceil(10)=10 → 10s；P95 = ceil(19)=19 → 19s；P99 = ceil(19.8)=20 → 20s。
	for i := 1; i <= 20; i++ {
		seedStatsTask(t, db, withDuration("v-dur-"+itoaTest(i), "video", "completed", i))
	}
	// image：4 个样本，耗时 100/200/300/400。
	// P50 = ceil(2)=2 → 200；P95 = ceil(3.8)=4 → 400；P99 = 4 → 400。
	for i, seconds := range []int{100, 200, 300, 400} {
		seedStatsTask(t, db, withDuration("i-dur-"+itoaTest(i), "image", "failed", seconds))
	}
	// 参与集合之外的行：
	//   1) 未结束任务（in_progress）——不是终态，不进耗时统计；
	//   2) 终态但缺时间戳——被排除，且必须在 DurationMissingTimestamps 里被单独暴露。
	seedStatsTask(t, db, withDuration("v-active", "video", "in_progress", 5))
	seedStatsTask(t, db, mediaTaskStatsSeed{
		taskID: "v-missing", kind: "video", status: "expired", createdAt: created,
		startedAt: nil, finishedAt: nil,
	})
	seedStatsTask(t, db, mediaTaskStatsSeed{
		taskID: "v-half-missing", kind: "video", status: "completed", createdAt: created,
		startedAt: &base, finishedAt: nil,
	})

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.DurationPercentiles(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("DurationPercentiles: %v", err)
	}
	byKind := map[string]repository.MediaTaskDurationRow{}
	for _, row := range rows {
		byKind[row.Kind] = row
	}

	video := byKind["video"]
	if video.SampleCount != 20 {
		t.Fatalf("video 样本数 = %d，want 20（只算 started_at/finished_at 均非 NULL 的终态行）", video.SampleCount)
	}
	assertSeconds(t, "video p50", video.P50Seconds, 10)
	assertSeconds(t, "video p95", video.P95Seconds, 19)
	assertSeconds(t, "video p99", video.P99Seconds, 20)

	image := byKind["image"]
	if image.SampleCount != 4 {
		t.Fatalf("image 样本数 = %d，want 4", image.SampleCount)
	}
	assertSeconds(t, "image p50", image.P50Seconds, 200)
	assertSeconds(t, "image p95", image.P95Seconds, 400)
	assertSeconds(t, "image p99", image.P99Seconds, 400)

	missing, err := repo.DurationMissingTimestamps(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("DurationMissingTimestamps: %v", err)
	}
	if len(missing) != 1 || missing[0].Kind != "video" || missing[0].Count != 2 {
		t.Fatalf("被排除的缺时间戳终态行 = %+v，want [{video 2}]（expired + 只有 started_at 的 completed）", missing)
	}
}

func TestMediaTaskStatsRepo_DurationPercentilesSingleSample(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	finished := base.Add(42 * time.Second)
	seedStatsTask(t, db, mediaTaskStatsSeed{
		taskID: "only-one", kind: "video", status: "completed", createdAt: time.Now().UTC(),
		startedAt: &base, finishedAt: &finished,
	})

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.DurationPercentiles(start, end, "", statsGlobalScope)
	if err != nil {
		t.Fatalf("DurationPercentiles: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应有一行，got %+v", rows)
	}
	// 单样本时三个分位都必须等于该样本（ceil(p/100 × 1) = 1），不能返回 0。
	assertSeconds(t, "p50", rows[0].P50Seconds, 42)
	assertSeconds(t, "p95", rows[0].P95Seconds, 42)
	assertSeconds(t, "p99", rows[0].P99Seconds, 42)
}

// ───────────────────── 指标 5b：无价拒绝（call_logs）─────────────────────

func TestMediaTaskStatsRepo_PricingMissingRejections(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	type callLogSeed struct {
		requestID       string
		ts              time.Time
		callType        string
		inboundEndpoint string
		status          int
		usageMissing    bool
		budgetBlockCode *string
	}
	blockExceeded := "budget_exceeded"
	seeds := []callLogSeed{
		// ✅ 真·无价拒绝：402 + usage_missing + 非预算拦截 + 媒体创建端点。
		{requestID: "rej-img-1", ts: now, callType: "image.generations", inboundEndpoint: "/v1/images/generations", status: 402, usageMissing: true},
		{requestID: "rej-img-2", ts: now, callType: "image.generations", inboundEndpoint: "/v1/images/generations", status: 402, usageMissing: true},
		{requestID: "rej-vid-1", ts: now, callType: "video.submit", inboundEndpoint: "/v1/videos", status: 402, usageMissing: true},
		// ✅ call_type 为空（历史行/未登记）但入站路径是媒体创建端点 → 兜底命中。
		{requestID: "rej-fallback", ts: now, callType: "", inboundEndpoint: "/v1/contents/generations/tasks", status: 402, usageMissing: true},
		// ❌ 预算闸门的 402（budget_block_code 非空）→ 必须排除，否则会把预算拦截算成无价拒绝。
		{requestID: "budget-402", ts: now, callType: "image.generations", inboundEndpoint: "/v1/images/generations", status: 402, usageMissing: true, budgetBlockCode: &blockExceeded},
		// ❌ 无价但**放行**（开关关闭，MEDIA_REQUIRE_PRICING=false）→ 不是拒绝，不计入。
		{requestID: "admitted-unpriced", ts: now, callType: "image.generations", inboundEndpoint: "/v1/images/generations", status: 200, usageMissing: true},
		// ❌ 已计价成功 → usage_missing=false。
		{requestID: "priced", ts: now, callType: "image.generations", inboundEndpoint: "/v1/images/generations", status: 200, usageMissing: false},
		// ❌ 配额 429 → 状态码不符。
		{requestID: "quota-429", ts: now, callType: "video.submit", inboundEndpoint: "/v1/videos", status: 429, usageMissing: true},
		// ❌ 非媒体端点（chat）被 402（理论上不存在，仍要确保不被计入）。
		{requestID: "chat-402", ts: now, callType: "chat", inboundEndpoint: "/v1/chat/completions", status: 402, usageMissing: true},
		// ❌ 窗口外。
		{requestID: "out-of-window", ts: start.Add(-time.Hour), callType: "image.edits", inboundEndpoint: "/v1/images/edits", status: 402, usageMissing: true},
	}
	for _, s := range seeds {
		if err := db.Exec(
			`INSERT INTO call_logs (request_id, ts, call_type, inbound_endpoint, status, usage_missing, budget_block_code)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			s.requestID, s.ts, s.callType, s.inboundEndpoint, s.status, s.usageMissing, s.budgetBlockCode,
		).Error; err != nil {
			t.Fatalf("seed call_log %s: %v", s.requestID, err)
		}
	}

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.PricingMissingRejections(start, end, statsGlobalScope)
	if err != nil {
		t.Fatalf("PricingMissingRejections: %v", err)
	}

	total := int64(0)
	byCallType := map[string]int64{}
	for _, row := range rows {
		total += row.Count
		byCallType[row.CallType] = row.Count
	}
	if total != 4 {
		t.Fatalf("无价拒绝总数 = %d，want 4（by_call_type=%v）", total, byCallType)
	}
	if byCallType["image.generations"] != 2 {
		t.Errorf("image.generations 拒绝数 = %d，want 2", byCallType["image.generations"])
	}
	if byCallType["video.submit"] != 1 {
		t.Errorf("video.submit 拒绝数 = %d，want 1", byCallType["video.submit"])
	}
	if byCallType[""] != 1 {
		t.Errorf("call_type 空串（inbound_endpoint 兜底）拒绝数 = %d，want 1", byCallType[""])
	}
	if repository.PricingMissingRejectionCriteria == "" {
		t.Error("判据表达式不能为空：它要随响应下发给运维")
	}
}

func TestMediaTaskStatsRepo_PricingMissingRejectionsScopedByUser(t *testing.T) {
	db := newMediaTaskStatsDB(t)
	start, end := statsTestWindow()
	now := time.Now().UTC()

	for _, seed := range []struct {
		requestID string
		userID    string
	}{
		{requestID: "u1-rej", userID: "u1"},
		{requestID: "u2-rej", userID: "u2"},
	} {
		if err := db.Exec(
			`INSERT INTO call_logs (request_id, ts, user_id, call_type, inbound_endpoint, status, usage_missing)
			 VALUES (?, ?, ?, 'image.generations', '/v1/images/generations', 402, ?)`,
			seed.requestID, now, seed.userID, true,
		).Error; err != nil {
			t.Fatalf("seed call_log %s: %v", seed.requestID, err)
		}
	}

	repo := repository.NewMediaTaskStatsRepo(db)
	rows, err := repo.PricingMissingRejections(start, end, repository.VideoTaskScope{UserID: "u1"})
	if err != nil {
		t.Fatalf("PricingMissingRejections: %v", err)
	}
	total := int64(0)
	for _, row := range rows {
		total += row.Count
	}
	if total != 1 {
		t.Fatalf("scope=user:u1 的无价拒绝数 = %d，want 1（归属范围必须生效）", total)
	}
}

// assertSeconds 断言秒数（sqlite 的 julianday 有极小浮点误差，故带容差）。
func assertSeconds(t *testing.T, label string, got, want float64) {
	t.Helper()
	if diff := got - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("%s = %v，want %v", label, got, want)
	}
}

// itoaTest 避免与同包其它测试文件的辅助函数重名。
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
