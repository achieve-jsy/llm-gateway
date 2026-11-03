package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// C7（控制面部分）：媒体任务超时清扫器的行为验证。
//
// 口径依据设计文档 §6.4：expired 视同 failed 但**不记账**（不写 cost_micro_usd / billed_at）。
// 扫描面**刻意收窄**到媒体词表的非终态（queued / in_progress），legacy Seedance 行不动 —— 零回归。

const sweeperTaskSchema = `
CREATE TABLE media_tasks (
	task_id TEXT PRIMARY KEY,
	api_key_id TEXT NOT NULL,
	model TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	provider_type TEXT NOT NULL,
	status TEXT NOT NULL,
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
	studio_id TEXT,
	error_message TEXT,
	video_url TEXT,
	request_body TEXT,
	response_body TEXT,
	api_key_name TEXT,
	api_key_type TEXT,
	user_id TEXT,
	user_name TEXT
)`

const sweeperEventSchema = `
CREATE TABLE media_task_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_data TEXT,
	created_at DATETIME
)`

func newSweeperFixture(t *testing.T) (*gorm.DB, *service.MediaTaskSweeper) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{sweeperTaskSchema, sweeperEventSchema} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	svc := service.NewVideoTaskService(repository.NewVideoTaskRepo(db))
	// TimeoutMinutes=30；period/batch 用默认值兜底
	return db, service.NewMediaTaskSweeper(svc, service.MediaTaskSweeperConfig{TimeoutMinutes: 30})
}

func seedSweeperTask(t *testing.T, db *gorm.DB, taskID, status string, age time.Duration, started bool) {
	t.Helper()
	ts := time.Now().UTC().Add(-age)
	task := &model.VideoTask{
		TaskID: taskID, APIKeyID: "key-" + taskID, Model: "kling-v1", ProviderName: "kling",
		ProviderType: "kling", Status: status, CreatedAt: ts, UpdatedAt: ts,
	}
	if started {
		task.StartedAt = &ts
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

func TestMediaTaskSweeper_ExpiresOnlyStaleActiveMediaTasks(t *testing.T) {
	db, sweeper := newSweeperFixture(t)

	seedSweeperTask(t, db, "stale-queued", model.MediaTaskStatusQueued, 45*time.Minute, false)
	seedSweeperTask(t, db, "stale-running", model.MediaTaskStatusInProgress, 45*time.Minute, true)
	seedSweeperTask(t, db, "fresh-queued", model.MediaTaskStatusQueued, time.Minute, false)
	seedSweeperTask(t, db, "done", model.MediaTaskStatusCompleted, 45*time.Minute, true)
	seedSweeperTask(t, db, "failed", model.MediaTaskStatusFailed, 45*time.Minute, true)
	seedSweeperTask(t, db, "legacy-submitted", "submitted", 45*time.Minute, false)
	seedSweeperTask(t, db, "legacy-running", "running", 45*time.Minute, false)

	swept, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if swept != 2 {
		t.Fatalf("应恰好清扫 2 条（stale-queued / stale-running），实际 %d", swept)
	}

	expectStatus := func(taskID, want string) {
		t.Helper()
		var task model.VideoTask
		if err := db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
			t.Fatalf("load %s: %v", taskID, err)
		}
		if task.Status != want {
			t.Errorf("%s 状态=%q want %q", taskID, task.Status, want)
		}
	}
	expectStatus("stale-queued", model.MediaTaskStatusExpired)
	expectStatus("stale-running", model.MediaTaskStatusExpired)
	// 未超时 / 已终态 / legacy 行一律不动
	expectStatus("fresh-queued", model.MediaTaskStatusQueued)
	expectStatus("done", model.MediaTaskStatusCompleted)
	expectStatus("failed", model.MediaTaskStatusFailed)
	expectStatus("legacy-submitted", "submitted")
	expectStatus("legacy-running", "running")

	var expired model.VideoTask
	if err := db.Where("task_id = ?", "stale-queued").First(&expired).Error; err != nil {
		t.Fatalf("load expired: %v", err)
	}
	if expired.ErrorMessage == "" {
		t.Error("超时任务必须写 error_message 说明原因（否则用户看到的 failed 没有理由）")
	}
	if expired.FinishedAt == nil {
		t.Error("进入终态必须写 finished_at")
	}
	// 不记账（设计 §6.4：expired 视同 failed 但不记账）
	if expired.CostMicroUSD != 0 || expired.BilledAt != nil {
		t.Errorf("超时不应记账：cost=%d billed_at=%v", expired.CostMicroUSD, expired.BilledAt)
	}
	// 状态迁移必须留痕在时间线里
	var events int64
	if err := db.Model(&model.VideoTaskEvent{}).Where("task_id = ?", "stale-queued").Count(&events).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events == 0 {
		t.Error("状态变化必须写事件（时间线）")
	}
}

func TestMediaTaskSweeper_IsIdempotentAndDoesNotOverwriteTerminal(t *testing.T) {
	db, sweeper := newSweeperFixture(t)
	seedSweeperTask(t, db, "stale-queued", model.MediaTaskStatusQueued, 45*time.Minute, false)

	if swept, err := sweeper.RunOnce(context.Background()); err != nil || swept != 1 {
		t.Fatalf("第一轮应清扫 1 条，got swept=%d err=%v", swept, err)
	}
	// 第二轮：已终态 → 不再扫到（幂等）
	if swept, err := sweeper.RunOnce(context.Background()); err != nil || swept != 0 {
		t.Fatalf("第二轮应为 0，got swept=%d err=%v", swept, err)
	}

	var task model.VideoTask
	if err := db.Where("task_id = ?", "stale-queued").First(&task).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if task.Status != model.MediaTaskStatusExpired {
		t.Fatalf("状态应为 expired，实际 %s", task.Status)
	}

	// 期间被推进到 completed 的任务，sweeper 不得覆盖（CAS 保护）
	seedSweeperTask(t, db, "raced", model.MediaTaskStatusInProgress, 45*time.Minute, true)
	svc := service.NewVideoTaskService(repository.NewVideoTaskRepo(db))
	if updated, _, err := svc.UpdateMediaTaskCAS("raced", service.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusInProgress,
		Status:         model.MediaTaskStatusCompleted,
	}); err != nil || !updated {
		t.Fatalf("模拟并发完成失败: updated=%v err=%v", updated, err)
	}
	// 此时 sweeper 的期望状态（读到 in_progress）已过期 → 必须拒绝写入
	updated, fresh, err := svc.UpdateMediaTaskCAS("raced", service.MediaTaskUpdateFields{
		ExpectedStatus: model.MediaTaskStatusInProgress,
		Status:         model.MediaTaskStatusExpired,
	})
	if err != nil {
		t.Fatalf("CAS 不应报错: %v", err)
	}
	if updated || fresh.Status != model.MediaTaskStatusCompleted {
		t.Fatalf("竞态下 sweeper 不得覆盖 completed：updated=%v status=%s", updated, fresh.Status)
	}
}

func TestMediaTaskSweeper_ConfigDefaultsAndTimeout(t *testing.T) {
	db, _ := newSweeperFixture(t)
	svc := service.NewVideoTaskService(repository.NewVideoTaskRepo(db))

	// 零值配置必须回落到安全默认值（否则会退化成「零分钟超时」把在跑的任务全杀）
	zero := service.NewMediaTaskSweeper(svc, service.MediaTaskSweeperConfig{})
	if got := zero.Timeout(); got != 30*time.Minute {
		t.Errorf("零值 TimeoutMinutes 应回落 30 分钟，实际 %v", got)
	}

	custom := service.NewMediaTaskSweeper(svc, service.MediaTaskSweeperConfig{TimeoutMinutes: 5})
	if got := custom.Timeout(); got != 5*time.Minute {
		t.Errorf("TimeoutMinutes=5 应为 5 分钟，实际 %v", got)
	}
	// 5 分钟窗口：10 分钟前的任务应被扫到，1 分钟前的不应
	seedSweeperTask(t, db, "ten-min-old", model.MediaTaskStatusQueued, 10*time.Minute, false)
	seedSweeperTask(t, db, "one-min-old", model.MediaTaskStatusQueued, time.Minute, false)
	swept, err := custom.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if swept != 1 {
		t.Fatalf("5 分钟窗口应只扫到 1 条，实际 %d", swept)
	}
	var old, fresh model.VideoTask
	_ = db.Where("task_id = ?", "ten-min-old").First(&old).Error
	_ = db.Where("task_id = ?", "one-min-old").First(&fresh).Error
	if old.Status != model.MediaTaskStatusExpired {
		t.Errorf("10 分钟前的任务应被置 expired，实际 %s", old.Status)
	}
	if fresh.Status != model.MediaTaskStatusQueued {
		t.Errorf("1 分钟前的任务不应被扫到，实际 %s", fresh.Status)
	}
}

func TestMediaTaskSweeper_RunOnceHonorsCancelledContext(t *testing.T) {
	_, sweeper := newSweeperFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sweeper.RunOnce(ctx); err == nil {
		t.Fatal("已取消的 context 应立即返回错误，不应继续扫库")
	}
}
