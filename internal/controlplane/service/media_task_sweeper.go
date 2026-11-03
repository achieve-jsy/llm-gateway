package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// C7（控制面部分）：媒体任务超时清扫。
//
// 【为什么落在控制面】gateway 不直连 Postgres（防护栏 A1），只有控制面能看到 media_tasks，
// 因此「把超过 MEDIA_TASK_TIMEOUT_MINUTES 仍非终态的任务置 expired」只能由控制面 cron 执行。
//
// 【语义】expired = 视同 failed 但**不记账**（设计文档 §6.4）：
//   - 只扫媒体词表的非终态（queued / in_progress）；legacy Seedance 行（submitted/running）
//     不在范围内，由旧链路自行收敛 —— 这是「收窄扫描面换零回归」的取舍；
//   - 写入走 CAS（expected_status = 读到的状态），与网关轮询并发时不会互相覆盖；
//   - 状态机保证 queued/in_progress → expired 合法，且 expired 之后不可再回退。

// MediaTaskSweeperConfig 清扫参数（来自 viper media_task.*，环境变量优先）。
type MediaTaskSweeperConfig struct {
	// TimeoutMinutes 非终态任务存活上限（分钟）；<=0 时取默认 30。
	TimeoutMinutes int
	// SweepIntervalSeconds 扫描周期（秒）；<=0 时取默认 60。
	SweepIntervalSeconds int
	// SweepBatch 单轮最多处理条数；<=0 时取默认 200。
	SweepBatch int
}

// 默认值（与 internal/controlplane/config 的 SetDefault 一致；这里再兜一层，
// 让直接构造的调用方/测试不会因为零值而退化成「永不超时」或「死循环」）。
const (
	DefaultMediaTaskTimeoutMinutes       = 30
	DefaultMediaTaskSweepIntervalSeconds = 60
	DefaultMediaTaskSweepBatch           = 200
)

// MediaTaskSweeper 超时任务清扫器。
type MediaTaskSweeper struct {
	svc *VideoTaskService
	cfg MediaTaskSweeperConfig
	now func() time.Time
}

// NewMediaTaskSweeper 创建清扫器（零值配置自动回落到默认值）。
func NewMediaTaskSweeper(svc *VideoTaskService, cfg MediaTaskSweeperConfig) *MediaTaskSweeper {
	if cfg.TimeoutMinutes <= 0 {
		cfg.TimeoutMinutes = DefaultMediaTaskTimeoutMinutes
	}
	if cfg.SweepIntervalSeconds <= 0 {
		cfg.SweepIntervalSeconds = DefaultMediaTaskSweepIntervalSeconds
	}
	if cfg.SweepBatch <= 0 {
		cfg.SweepBatch = DefaultMediaTaskSweepBatch
	}
	return &MediaTaskSweeper{svc: svc, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

// Timeout 返回解析后的超时窗口。
func (s *MediaTaskSweeper) Timeout() time.Duration {
	return time.Duration(s.cfg.TimeoutMinutes) * time.Minute
}

// Start 启动后台循环（阻塞调用方之前先起 goroutine，与 keyactivity.Cron 同范式）。
func (s *MediaTaskSweeper) Start(ctx context.Context) {
	if s == nil || s.svc == nil {
		return
	}
	go func() {
		interval := time.Duration(s.cfg.SweepIntervalSeconds) * time.Second
		slog.Info("media_task_sweeper_started",
			"interval", interval.String(),
			"timeout_minutes", s.cfg.TimeoutMinutes,
			"batch", s.cfg.SweepBatch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("media_task_sweeper_stopped")
				return
			case <-ticker.C:
				if n, err := s.RunOnce(ctx); err != nil {
					slog.Error("media_task_sweep_error", "error", err)
				} else if n > 0 {
					slog.Info("media_task_swept", "count", n)
				}
			}
		}
	}()
}

// RunOnce 执行一轮清扫，返回被置为 expired 的任务数。
func (s *MediaTaskSweeper) RunOnce(ctx context.Context) (int, error) {
	if s == nil || s.svc == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.SweepOnce(s.now().Add(-s.Timeout()))
}

// SweepOnce 把 cutoff 之前仍未进入终态的媒体任务置为 expired。
//
// 逐条 CAS：`expected_status` 取读到的状态 ⇒ 若期间网关已把任务推进到终态，
// RowsAffected=0，本轮跳过（绝不覆盖新状态、绝不把已完成的任务标成超时）。
func (s *MediaTaskSweeper) SweepOnce(cutoff time.Time) (int, error) {
	tasks, err := s.svc.ListTimedOut(cutoff, s.cfg.SweepBatch)
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	swept := 0
	for i := range tasks {
		task := tasks[i]
		updated, _, err := s.svc.UpdateMediaTaskCAS(task.TaskID, MediaTaskUpdateFields{
			ExpectedStatus: task.Status,
			Status:         model.MediaTaskStatusExpired,
			ErrorMessage: fmt.Sprintf(
				"媒体任务超时：提交后 %d 分钟内未进入终态，已置为 expired（视同失败，不记账）",
				s.cfg.TimeoutMinutes),
		})
		if err != nil {
			// 单条失败不中断整轮：DB 抖动不应让后续超时任务一直不被清理。
			slog.Error("media_task_sweep_item_error", "task_id", task.TaskID, "error", err)
			continue
		}
		if updated {
			swept++
		}
	}
	return swept, nil
}
