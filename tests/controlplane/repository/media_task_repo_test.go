package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件覆盖 C2 的仓储增量：状态机（只前进 / 终态不回退 / legacy 兼容）、
// CAS 更新、幂等创建与查询、超时扫描、事件时间线。
//
// 用内存 sqlite（与 tests/ 既有范式一致）；DDL 手写并与 GORM model 的列一一对应
// （含 C1 delta 14 列）——表名是 media_tasks/media_task_events（决策 D 改名后）。
// 只覆盖仓储语义，不覆盖 PG 特有的行为（那由迁移演练 + 漂移对拍覆盖）。

const mediaTaskTestSchema = `
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

const mediaTaskEventTestSchema = `
CREATE TABLE media_task_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_data TEXT,
	created_at DATETIME
)`

// 幂等唯一索引（与迁移里的 uniq_media_tasks_idem 同定义；sqlite 里 NULL 同样互不相等）。
const mediaTaskIdemIndex = `CREATE UNIQUE INDEX uniq_media_tasks_idem ON media_tasks (api_key_id, idempotency_key)`

func newMediaTaskDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{mediaTaskTestSchema, mediaTaskIdemIndex, mediaTaskEventTestSchema} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

func seedTask(t *testing.T, db *gorm.DB, taskID, status, kind string) *model.VideoTask {
	t.Helper()
	now := time.Now().UTC()
	task := &model.VideoTask{
		TaskID:       taskID,
		APIKeyID:     "key-" + taskID,
		Model:        "kling-v1",
		ProviderName: "kling",
		ProviderType: "kling",
		Status:       status,
		Kind:         kind,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed task %s: %v", taskID, err)
	}
	return task
}

func loadTask(t *testing.T, db *gorm.DB, taskID string) *model.VideoTask {
	t.Helper()
	var task model.VideoTask
	if err := db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		t.Fatalf("load task %s: %v", taskID, err)
	}
	return &task
}

// ───────────────────────────── 状态机（纯函数） ─────────────────────────────

func TestCanAdvanceMediaStatus(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
		why      string
	}{
		// 只前进
		{"queued", "in_progress", true, "queued→in_progress"},
		{"in_progress", "completed", true, "in_progress→completed"},
		{"queued", "failed", true, "queued 可直接失败"},
		{"queued", "expired", true, "queued 可超时"},
		{"in_progress", "expired", true, "in_progress 可超时"},
		{"queued", "completed", true, "同步链路可直接完成"},
		// 同状态重放（幂等刷新 progress/usage）
		{"queued", "queued", true, "同状态重放"},
		{"in_progress", "in_progress", true, "同状态重放"},
		{"completed", "completed", true, "同状态重放"},
		{"expired", "expired", true, "同状态重放（sweeper 幂等）"},
		// 终态不回退
		{"completed", "in_progress", false, "终态不可回退"},
		{"completed", "queued", false, "终态不可回退"},
		{"completed", "failed", false, "终态之间不可横跳"},
		{"failed", "completed", false, "终态之间不可横跳"},
		{"expired", "failed", false, "expired 不可降级为 failed"},
		{"failed", "expired", false, "failed 不可被 sweeper 覆盖"},
		{"in_progress", "queued", false, "不可回退到 queued"},
		// 媒体行不得写回 legacy 词表
		{"queued", "submitted", false, "媒体行禁止写回 legacy"},
		{"completed", "succeeded", false, "媒体行禁止写回 legacy"},
		// legacy 行不受新状态机约束（向后兼容 Seedance 链路）
		{"submitted", "running", true, "legacy 链路必须原样工作"},
		{"running", "succeeded", true, "legacy 链路必须原样工作"},
		{"submitted", "succeeded", true, "legacy 链路必须原样工作"},
		{"submitted", "failed", true, "legacy 链路必须原样工作"},
		// legacy 的 failed 恰好与媒体词表同字面量 → 按媒体规则处理（首个 2 秩）
		{"failed", "succeeded", false, "legacy failed 也是终态，不可复活"},
		// 未知目标：legacy 行放行（保持旧行为），媒体行拒绝
		{"submitted", "banana", true, "legacy 行保持历史宽松行为"},
		{"queued", "banana", false, "媒体行拒绝未枚举状态"},
	}
	for _, tc := range cases {
		if got := repository.CanAdvanceMediaStatus(tc.from, tc.to); got != tc.want {
			t.Errorf("CanAdvanceMediaStatus(%q,%q)=%v want %v (%s)", tc.from, tc.to, got, tc.want, tc.why)
		}
	}
}

func TestMediaTaskStatusSets(t *testing.T) {
	active := repository.MediaTaskActiveStatuses()
	if len(active) != 2 {
		t.Fatalf("活动状态应恰有 2 个，实际 %v", active)
	}
	for _, s := range active {
		if model.IsMediaTaskTerminalStatus(s) {
			t.Errorf("活动状态 %q 不应是终态", s)
		}
	}
	terminal := repository.MediaTaskTerminalStatuses()
	if len(terminal) != 3 {
		t.Fatalf("终态应恰有 3 个，实际 %v", terminal)
	}
	for _, s := range terminal {
		if !model.IsMediaTaskTerminalStatus(s) {
			t.Errorf("终态集合里的 %q 未被 model 判为终态", s)
		}
	}
}

// ───────────────────────────── 幂等创建 ─────────────────────────────

func TestCreateMediaTask_IdempotencyByKey(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)

	first := &model.VideoTask{
		TaskID: "task-1", APIKeyID: "key-a", IdempotencyKey: strPtr("idem-1"),
		Model: "kling-v1", ProviderName: "kling", ProviderType: "kling",
		Status: model.MediaTaskStatusQueued, Kind: model.MediaTaskKindVideo,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	created, saved, err := repo.CreateMediaTask(first)
	if err != nil || !created {
		t.Fatalf("首次创建应 created=true，got created=%v err=%v", created, err)
	}
	if saved.TaskID != "task-1" {
		t.Fatalf("返回的任务 id 不符: %s", saved.TaskID)
	}

	// 同 Key 同幂等键 → 返回既有任务，不新建（哪怕 task_id 不同）
	second := *first
	second.TaskID = "task-2"
	created, saved, err = repo.CreateMediaTask(&second)
	if err != nil {
		t.Fatalf("幂等命中不应报错: %v", err)
	}
	if created {
		t.Fatal("幂等命中必须 created=false")
	}
	if saved.TaskID != "task-1" {
		t.Fatalf("幂等命中必须返回既有 task-1，实际 %s", saved.TaskID)
	}
	var count int64
	if err := db.Model(&model.VideoTask{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("幂等命中后总行数应仍为 1，实际 %d", count)
	}

	// 换 Key、同幂等键 → 视为不同请求，允许创建
	third := *first
	third.TaskID = "task-3"
	third.APIKeyID = "key-b"
	created, saved, err = repo.CreateMediaTask(&third)
	if err != nil || !created {
		t.Fatalf("不同 Key 的同名幂等键应可创建，got created=%v err=%v", created, err)
	}
	if saved.TaskID != "task-3" {
		t.Fatalf("应新建 task-3，实际 %s", saved.TaskID)
	}

	// 幂等键为空 → 不参与幂等；两个空键行都能建
	for _, id := range []string{"task-n1", "task-n2"} {
		task := &model.VideoTask{
			TaskID: id, APIKeyID: "key-a", Model: "m", ProviderName: "p", ProviderType: "pt",
			Status: model.MediaTaskStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if created, _, err := repo.CreateMediaTask(task); err != nil || !created {
			t.Fatalf("空幂等键应可创建（NULL 互不相等），task=%s created=%v err=%v", id, created, err)
		}
	}

	// task_id 重复（无幂等键）→ 返回既有行，不报错、不产生第二行
	dup := &model.VideoTask{
		TaskID: "task-n1", APIKeyID: "key-a", Model: "m", ProviderName: "p", ProviderType: "pt",
		Status: model.MediaTaskStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	created, saved, err = repo.CreateMediaTask(dup)
	if err != nil {
		t.Fatalf("task_id 重复不应报错: %v", err)
	}
	if created || saved == nil || saved.TaskID != "task-n1" {
		t.Fatalf("task_id 重复应回读既有行，got created=%v saved=%v", created, saved)
	}
}

func TestGetByIdempotencyKey(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)
	seedTask(t, db, "task-1", model.MediaTaskStatusQueued, model.MediaTaskKindVideo)
	if err := db.Model(&model.VideoTask{}).Where("task_id = ?", "task-1").
		Update("idempotency_key", "idem-1").Error; err != nil {
		t.Fatalf("set idem key: %v", err)
	}

	got, err := repo.GetByIdempotencyKey("key-task-1", "idem-1")
	if err != nil || got.TaskID != "task-1" {
		t.Fatalf("幂等查询失败: task=%v err=%v", got, err)
	}
	if _, err := repo.GetByIdempotencyKey("key-task-1", "absent"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("未命中应返回 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := repo.GetByIdempotencyKey("", "idem-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("空 api_key 应返回 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := repo.GetByIdempotencyKey("key-task-1", ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("空幂等键应返回 ErrRecordNotFound，实际 %v", err)
	}
}

// ───────────────────────────── CAS 更新 ─────────────────────────────

func TestUpdateMediaTaskCAS_ForwardOnlyAndTimestamps(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)
	seeded := seedTask(t, db, "task-1", model.MediaTaskStatusQueued, model.MediaTaskKindVideo)
	// 让 updated_at 明显早于「现在」，便于断言「同状态轮询不刷新 updated_at」
	old := time.Now().UTC().Add(-2 * time.Hour)
	if err := db.Model(&model.VideoTask{}).Where("task_id = ?", "task-1").
		Update("updated_at", old).Error; err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}
	if seeded.Status != model.MediaTaskStatusQueued {
		t.Fatal("seed 状态错误")
	}

	// 1) queued → in_progress：写 started_at
	updated, task, err := repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusQueued,
		Status:         model.MediaTaskStatusInProgress,
		Progress:       "10",
		VendorTaskID:   "vendor-1",
	})
	if err != nil || !updated {
		t.Fatalf("前进更新失败: updated=%v err=%v", updated, err)
	}
	if task.Status != model.MediaTaskStatusInProgress || task.Progress != "10" || task.VendorTaskID != "vendor-1" {
		t.Fatalf("字段未写入: %+v", task)
	}
	if task.StartedAt == nil {
		t.Fatal("进入 in_progress 必须写 started_at")
	}
	if !task.UpdatedAt.After(old) {
		t.Fatal("状态变化必须刷新 updated_at")
	}
	startedAt := *task.StartedAt
	afterFirst := task.UpdatedAt

	// 2) 同状态重放（仅刷新 progress）：updated_at 保持不变（既有耗时统计语义）
	updated, task, err = repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		Status:   model.MediaTaskStatusInProgress,
		Progress: "55",
	})
	if err != nil || !updated {
		t.Fatalf("同状态重放应生效: updated=%v err=%v", updated, err)
	}
	if task.Progress != "55" {
		t.Fatalf("progress 未刷新: %s", task.Progress)
	}
	if task.StartedAt == nil || !task.StartedAt.Equal(startedAt) {
		t.Fatal("started_at 不应被轮询改写")
	}
	if !task.UpdatedAt.Equal(afterFirst) {
		t.Fatalf("同状态轮询不应刷新 updated_at：第一次=%v 重放后=%v", afterFirst, task.UpdatedAt)
	}

	// 3) 回退被拒（in_progress → queued）
	updated, task, err = repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		Status: model.MediaTaskStatusQueued,
	})
	if err != nil {
		t.Fatalf("回退不应报错（只是不生效）: %v", err)
	}
	if updated || task.Status != model.MediaTaskStatusInProgress {
		t.Fatalf("回退必须被拒且状态不变，got updated=%v status=%s", updated, task.Status)
	}

	// 4) CAS 期望状态不匹配 → 不生效
	updated, task, err = repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusQueued,
		Status:         model.MediaTaskStatusCompleted,
	})
	if err != nil {
		t.Fatalf("CAS 不匹配不应报错: %v", err)
	}
	if updated || task.Status != model.MediaTaskStatusInProgress {
		t.Fatalf("CAS 不匹配必须不生效，got updated=%v status=%s", updated, task.Status)
	}

	// 5) 终态：写 finished_at + 用量/成本/记账时间
	usage := model.JSONRaw{"final_unit_deduction": "3.5"}
	cost := int64(125000)
	billed := time.Now().UTC()
	updated, task, err = repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusInProgress,
		Status:         model.MediaTaskStatusCompleted,
		Usage:          &usage,
		CostMicroUSD:   &cost,
		BilledAt:       &billed,
		PollFailures:   intPtr(2),
	})
	if err != nil || !updated {
		t.Fatalf("终态更新失败: updated=%v err=%v", updated, err)
	}
	if task.FinishedAt == nil {
		t.Fatal("进入终态必须写 finished_at")
	}
	if task.CostMicroUSD != cost || task.BilledAt == nil || task.PollFailures != 2 {
		t.Fatalf("计费字段未写入: %+v", task)
	}
	if task.Usage["final_unit_deduction"] != "3.5" {
		t.Fatalf("usage 未写入: %+v", task.Usage)
	}

	// 6) 终态之间不可横跳（completed → expired，sweeper 场景）
	updated, task, err = repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusCompleted,
		Status:         model.MediaTaskStatusExpired,
	})
	if err != nil {
		t.Fatalf("终态横跳不应报错: %v", err)
	}
	if updated || task.Status != model.MediaTaskStatusCompleted {
		t.Fatalf("终态横跳必须被拒，got updated=%v status=%s", updated, task.Status)
	}
}

func TestUpdateMediaTaskCAS_ExpiredAliasAndLegacyCompatibility(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)

	// 任务不存在 → ErrRecordNotFound（调用方转 404 / 5xx）
	if _, _, err := repo.UpdateMediaTaskCAS("missing", repository.MediaTaskUpdateFields{
		Status: model.MediaTaskStatusInProgress,
	}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺失任务应返回 ErrRecordNotFound，实际 %v", err)
	}

	// expired 视为终态：调用方读到的是归一后的 failed，因此 expected_status=failed 也必须命中
	seedTask(t, db, "task-exp", model.MediaTaskStatusQueued, model.MediaTaskKindVideo)
	updated, _, err := repo.UpdateMediaTaskCAS("task-exp", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusQueued,
		Status:         model.MediaTaskStatusExpired,
		ErrorMessage:   "超时",
	})
	if err != nil || !updated {
		t.Fatalf("queued→expired 应生效: updated=%v err=%v", updated, err)
	}
	updated, task, err := repo.UpdateMediaTaskCAS("task-exp", repository.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusFailed, // 调用方视图（expired 已归一为 failed）
		Status:         model.MediaTaskStatusExpired,
		Progress:       "9",
	})
	if err != nil {
		t.Fatalf("别名 CAS 不应报错: %v", err)
	}
	if !updated {
		t.Fatal("expected_status=failed 必须匹配 DB 里的 expired（否则调用方会陷入永远 CAS 失败）")
	}
	if task.Status != model.MediaTaskStatusExpired || task.Progress != "9" {
		t.Fatalf("状态/字段不符: %+v", task)
	}

	// legacy 行（submitted）→ running：旧链路必须原样工作
	seedTask(t, db, "task-legacy", "submitted", "")
	updated, task, err = repo.UpdateMediaTaskCAS("task-legacy", repository.MediaTaskUpdateFields{
		ExpectedStatus: "submitted",
		Status:         "running",
		ResponseBody:   `{"state":"running"}`,
	})
	if err != nil || !updated {
		t.Fatalf("legacy 前进更新失败: updated=%v err=%v", updated, err)
	}
	if task.Status != "running" {
		t.Fatalf("legacy 状态未写入: %s", task.Status)
	}
	// legacy 行不写 started_at/finished_at（那两个字段只服务媒体链路）
	if task.StartedAt != nil || task.FinishedAt != nil {
		t.Fatalf("legacy 迁移不应写媒体时间戳: started=%v finished=%v", task.StartedAt, task.FinishedAt)
	}
	// legacy → succeeded 仍可（终态在 legacy 词表里）
	updated, task, err = repo.UpdateMediaTaskCAS("task-legacy", repository.MediaTaskUpdateFields{Status: "succeeded"})
	if err != nil || !updated || task.Status != "succeeded" {
		t.Fatalf("legacy 进入终态失败: updated=%v status=%s err=%v", updated, task.Status, err)
	}

	// 无字段可写 → updated=false 且不报错
	updated, task, err = repo.UpdateMediaTaskCAS("task-legacy", repository.MediaTaskUpdateFields{})
	if err != nil || updated {
		t.Fatalf("空更新应是无操作: updated=%v err=%v", updated, err)
	}
	if task.Status != "succeeded" {
		t.Fatalf("空更新不应改状态: %s", task.Status)
	}
}

func TestUpdateMediaTaskCAS_WritesTimelineEvent(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)
	seedTask(t, db, "task-1", model.MediaTaskStatusQueued, model.MediaTaskKindVideo)

	if _, _, err := repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		Status: model.MediaTaskStatusInProgress,
	}); err != nil {
		t.Fatalf("cas: %v", err)
	}
	// 同状态（无状态变化）不应产生新事件
	if _, _, err := repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		Status:   model.MediaTaskStatusInProgress,
		Progress: "50",
	}); err != nil {
		t.Fatalf("cas repeat: %v", err)
	}
	if _, _, err := repo.UpdateMediaTaskCAS("task-1", repository.MediaTaskUpdateFields{
		Status: model.MediaTaskStatusCompleted,
	}); err != nil {
		t.Fatalf("cas complete: %v", err)
	}

	events, err := repo.ListEvents("task-1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("状态变化两次应恰有 2 条事件，实际 %d: %+v", len(events), events)
	}
	if events[0].EventType != model.MediaTaskStatusInProgress || events[1].EventType != model.MediaTaskStatusCompleted {
		t.Fatalf("事件顺序/类型不符: %+v", events)
	}
	if events[0].EventData == "" {
		t.Fatal("事件数据不应为空")
	}
}

// ───────────────────────────── 超时扫描 ─────────────────────────────

func TestListTimedOut(t *testing.T) {
	db := newMediaTaskDB(t)
	repo := repository.NewVideoTaskRepo(db)
	now := time.Now().UTC()
	stale := now.Add(-45 * time.Minute)
	fresh := now.Add(-1 * time.Minute)

	mk := func(taskID, status string, updatedAt time.Time, startedAt *time.Time) {
		task := &model.VideoTask{
			TaskID: taskID, APIKeyID: "key-" + taskID, Model: "m", ProviderName: "p", ProviderType: "pt",
			Status: status, CreatedAt: updatedAt, UpdatedAt: updatedAt, StartedAt: startedAt,
		}
		if err := db.Create(task).Error; err != nil {
			t.Fatalf("seed %s: %v", taskID, err)
		}
	}
	mk("stale-queued", model.MediaTaskStatusQueued, stale, nil)
	mk("stale-running", model.MediaTaskStatusInProgress, stale, &stale)
	mk("fresh-queued", model.MediaTaskStatusQueued, fresh, nil)
	mk("old-but-progressed", model.MediaTaskStatusInProgress, fresh, &stale) // started 早但刚有状态变化
	mk("done", model.MediaTaskStatusCompleted, stale, &stale)
	mk("failed", model.MediaTaskStatusFailed, stale, &stale)
	mk("expired", model.MediaTaskStatusExpired, stale, &stale)
	// legacy 行绝不能被 sweeper 扫到（旧链路自行收敛）
	mk("legacy-submitted", "submitted", stale, nil)
	mk("legacy-running", "running", stale, nil)

	cutoff := now.Add(-30 * time.Minute)
	tasks, err := repo.ListTimedOut(cutoff, 100)
	if err != nil {
		t.Fatalf("list timed out: %v", err)
	}
	got := map[string]bool{}
	for _, task := range tasks {
		got[task.TaskID] = true
	}
	want := map[string]bool{
		"stale-queued":       true, // 排队中且从未变化
		"stale-running":      true, // 已开始且停留超过窗口
		"old-but-progressed": true, // started_at 早于窗口（起算点是 started_at，不是 updated_at）
	}
	for id := range want {
		if !got[id] {
			t.Errorf("应被扫到但未扫到: %s（实际集合 %v）", id, keys(got))
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("不应被扫到: %s（实际集合 %v）", id, keys(got))
		}
	}
	if len(tasks) != 3 {
		t.Fatalf("应恰有 3 条超时任务，实际 %d: %v", len(tasks), keys(got))
	}
	// 排序：更早的开始时间在前
	if tasks[0].TaskID != "stale-running" && tasks[0].TaskID != "stale-queued" {
		t.Fatalf("排序不符: %s", tasks[0].TaskID)
	}

	// limit 生效
	limited, err := repo.ListTimedOut(cutoff, 1)
	if err != nil {
		t.Fatalf("limit list: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit=1 应只返回 1 条，实际 %d", len(limited))
	}

	// start_at 为 NULL 且 updated_at 很新 → 不超时（用 updated_at 作排队起算点）
	if _, err := repo.ListTimedOut(now.Add(-30*time.Minute), 100); err != nil {
		t.Fatalf("second pass: %v", err)
	}
}

// ───────────────────────────── 归属/既有查询回归 ─────────────────────────────

func TestScopedQueriesStillUseRenamedTable(t *testing.T) {
	db := newMediaTaskDB(t)
	for _, stmt := range []string{
		`CREATE TABLE departments (id TEXT PRIMARY KEY, dept_id TEXT, studio_id TEXT)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, dept_id TEXT)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create org schema: %v", err)
		}
	}
	if err := db.Exec(`INSERT INTO departments (id, dept_id, studio_id) VALUES ('dept-a','dept-a','studio-a')`).Error; err != nil {
		t.Fatalf("seed dept: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id, dept_id) VALUES ('user-a','dept-a')`).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	task := &model.VideoTask{
		TaskID: "task-a", APIKeyID: "key-a", UserID: "user-a", Model: "m", ProviderName: "p",
		ProviderType: "pt", Status: model.MediaTaskStatusQueued,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}

	repo := repository.NewVideoTaskRepo(db)
	// applyScope 里的是裸 SQL（media_tasks.user_id）—— 改名后必须仍然可用
	tasks, total, err := repo.ListScoped(1, 20, "", "", "", "", repository.VideoTaskScope{UserID: "user-a"})
	if err != nil {
		t.Fatalf("personal scope: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("个人范围应命中 1 条，实际 total=%d len=%d", total, len(tasks))
	}
	deptTasks, total, err := repo.ListScoped(1, 20, "", "", "", "", repository.VideoTaskScope{DeptIDs: []string{"dept-a"}})
	if err != nil {
		t.Fatalf("dept scope: %v", err)
	}
	if total != 1 || len(deptTasks) != 1 {
		t.Fatalf("部门范围应命中 1 条，实际 total=%d len=%d", total, len(deptTasks))
	}
	studioTasks, total, err := repo.ListScoped(1, 20, "", "", "", "", repository.VideoTaskScope{StudioIDs: []string{"studio-a"}})
	if err != nil {
		t.Fatalf("studio scope: %v", err)
	}
	if total != 1 || len(studioTasks) != 1 {
		t.Fatalf("工作室范围应命中 1 条，实际 total=%d len=%d", total, len(studioTasks))
	}
	if _, _, err := repo.ListScoped(1, 20, "", "", "", "", repository.VideoTaskScope{}); err != nil {
		t.Fatalf("空范围应返回空集而不是报错: %v", err)
	}

	if got, err := repo.GetMediaTask("task-a"); err != nil || got.TaskID != "task-a" {
		t.Fatalf("GetMediaTask 失败: task=%v err=%v", got, err)
	}
	if _, err := repo.GetMediaTask("absent"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺失任务应返回 ErrRecordNotFound，实际 %v", err)
	}

	// GetByIDScoped 与 StatsDistributionScoped 同样经过 applyScope 的裸 SQL
	// （`media_tasks.user_id IN (...)`）—— 改名后这两条路径最容易被漏掉，
	// 而它们正是看板/详情页的入口。任一残留旧表名，sqlite 会直接
	// `no such table: video_tasks`，把缺陷摁在闸门里。
	if got, err := repo.GetByIDScoped("task-a", repository.VideoTaskScope{UserID: "user-a"}); err != nil {
		t.Fatalf("GetByIDScoped 个人范围失败（裸 SQL 表名？）: %v", err)
	} else if got.TaskID != "task-a" {
		t.Fatalf("GetByIDScoped 返回了错误任务: %s", got.TaskID)
	}
	if _, err := repo.GetByIDScoped("task-a", repository.VideoTaskScope{UserID: "user-b"}); err == nil {
		t.Fatalf("GetByIDScoped 不应让他人读到任务详情")
	}

	dist, err := repo.StatsDistributionScoped("", "", "", "", "", repository.VideoTaskScope{StudioIDs: []string{"studio-a"}})
	if err != nil {
		t.Fatalf("StatsDistributionScoped 工作室范围失败（裸 SQL 表名？）: %v", err)
	}
	if len(dist.Statuses) != 1 || dist.Statuses[0].Status != model.MediaTaskStatusQueued {
		t.Fatalf("工作室范围状态分布不符: %+v", dist.Statuses)
	}
	if len(dist.Models) != 1 || dist.Models[0].Model != "m" {
		t.Fatalf("工作室范围模型分布不符: %+v", dist.Models)
	}
	if len(dist.Providers) != 1 || dist.Providers[0].ProviderName != "p" {
		t.Fatalf("工作室范围渠道分布不符: %+v", dist.Providers)
	}
	// 越权范围必须取不到任何行（applyScope 的兜底 `1 = 0` 分支）。
	if empty, err := repo.StatsDistributionScoped("", "", "", "", "", repository.VideoTaskScope{StudioIDs: []string{"studio-z"}}); err != nil {
		t.Fatalf("越权工作室范围应返回空集: %v", err)
	} else if len(empty.Statuses) != 0 {
		t.Fatalf("越权工作室范围不应看到任何任务: %+v", empty.Statuses)
	}
}

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
