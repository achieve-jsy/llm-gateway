package ehrsync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

// Scheduler 定时调度 EHR 同步，多实例部署时通过 Redis 分布式锁互斥执行
type Scheduler struct {
	mu          sync.Mutex
	orch        *Orchestrator
	rdb         *redis.Client
	db          interface{ Create(any) error } // 仅用于写 execution_log
	cron        *cron.Cron
	running     bool
	cancelCtx   context.CancelFunc
	locks       *Locker
	logger      *slog.Logger
}

// SchedulerConfig 各实体的 cron 表达式 + 锁 TTL
type SchedulerConfig struct {
	CronOrg           time.Duration // 默认 30min
	CronCompany       time.Duration // 默认 30min
	CronDepartment    time.Duration // 默认 15min
	CronEmployee      time.Duration // 默认 10min
	CronReconcile     time.Duration // 默认 12h (每天两次)
	LockTTL           time.Duration // 默认 10min
	Enabled           bool          // 默认 true
}

// DefaultSchedulerConfig 从环境变量 + 默认值
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		CronOrg:        envDuration("EHR_CRON_ORG", 30*time.Minute),
		CronCompany:    envDuration("EHR_CRON_COMPANY", 30*time.Minute),
		CronDepartment: envDuration("EHR_CRON_DEPARTMENT", 15*time.Minute),
		CronEmployee:   envDuration("EHR_CRON_EMPLOYEE", 10*time.Minute),
		CronReconcile:  envDuration("EHR_CRON_RECONCILE", 12*time.Hour),
		LockTTL:        envDuration("EHR_LOCK_TTL", 10*time.Minute),
		Enabled:        envBool("EHR_SYNC_ENABLED", true),
	}
}

// NewScheduler 构造
func NewScheduler(orch *Orchestrator, rdb *redis.Client, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		orch:   orch,
		rdb:    rdb,
		logger: logger,
		locks:  NewLocker(rdb, DefaultSchedulerConfig().LockTTL),
	}
}

// Start 启动定时调度
func (s *Scheduler) Start(cfg SchedulerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("scheduler already running")
	}
	if !cfg.Enabled {
		s.logger.Info("ehr sync scheduler disabled (EHR_SYNC_ENABLED=false)")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cron = cron.New(cron.WithSeconds())
	s.cancelCtx = cancel

	// 注册 4 个增量任务 + 1 个对账任务
	s.scheduleEvery("organisation", cfg.CronOrg, func() { s.runIncrementalOnce(ctx) })
	s.scheduleEvery("company", cfg.CronCompany, func() { s.runIncrementalOnce(ctx) })
	s.scheduleEvery("department", cfg.CronDepartment, func() { s.runIncrementalOnce(ctx) })
	s.scheduleEvery("employee", cfg.CronEmployee, func() { s.runIncrementalOnce(ctx) })
	s.scheduleEvery("reconcile", cfg.CronReconcile, func() { s.runReconcileOnce(ctx) })

	s.cron.Start()
	s.running = true
	s.logger.Info("ehr sync scheduler started",
		"org", cfg.CronOrg,
		"company", cfg.CronCompany,
		"department", cfg.CronDepartment,
		"employee", cfg.CronEmployee,
		"reconcile", cfg.CronReconcile)
	return nil
}

// Stop 停止调度
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
	}
	if s.cancelCtx != nil {
		s.cancelCtx()
	}
	s.running = false
	s.logger.Info("ehr sync scheduler stopped")
}

// IsRunning 是否在运行
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// TriggerManual 手动触发一次同步
func (s *Scheduler) TriggerManual(ctx context.Context, syncType string) (string, error) {
	if syncType == "" {
		syncType = "incremental"
	}

	batchID := uuid.NewString()
	switch syncType {
	case "incremental":
		_, err := s.acquireAndRun(ctx, batchID, "ehr:sync:incremental", func() error {
			_, err := s.orch.RunIncremental(ctx)
			return err
		})
		return batchID, err
	case "full":
		_, err := s.acquireAndRun(ctx, batchID, "ehr:sync:full", func() error {
			_, err := s.orch.RunFullSync(ctx)
			return err
		})
		return batchID, err
	default:
		return "", fmt.Errorf("unknown sync type: %s", syncType)
	}
}

// ---------- 内部 ----------

// scheduleEvery 按 duration 间隔调度（使用 cron @every 语法）
func (s *Scheduler) scheduleEvery(name string, d time.Duration, fn func()) {
	expr := fmt.Sprintf("@every %s", d.Round(time.Second).String())
	_, err := s.cron.AddFunc(expr, func() {
		lockKey := "ehr:lock:" + name
		s.acquireAndRun(context.Background(), uuid.NewString(), lockKey, func() error {
			fn()
			return nil
		})
	})
	if err != nil {
		s.logger.Error("register cron job failed", "name", name, "expr", expr, "error", err)
	}
}

func (s *Scheduler) runIncrementalOnce(ctx context.Context) {
	started := time.Now()
	res, err := s.orch.RunIncremental(ctx)
	duration := time.Since(started)
	if err != nil {
		s.logger.Error("incremental sync failed", "error", err, "duration", duration)
		return
	}
	s.logger.Info("incremental sync ok",
		"batchId", res.BatchID,
		"durationMs", res.DurationMs,
		"orgs", len(res.ByOrg),
		"calls", res.CallCount,
		"errors", res.ErrorCount)
}

func (s *Scheduler) runReconcileOnce(ctx context.Context) {
	started := time.Now()
	res, err := s.orch.RunFullSync(ctx) // 暂用 FullSync 替代 Reconcile
	duration := time.Since(started)
	if err != nil {
		s.logger.Error("reconcile failed", "error", err, "duration", duration)
		return
	}
	s.logger.Info("reconcile ok",
		"batchId", res.BatchID,
		"durationMs", res.DurationMs)
}

// acquireAndRun 抢占分布式锁 + 执行任务 + 释放锁
func (s *Scheduler) acquireAndRun(ctx context.Context, batchID, lockKey string, fn func() error) (bool, error) {
	acquired, err := s.locks.TryAcquire(ctx, lockKey)
	if err != nil {
		return false, err
	}
	if !acquired {
		s.logger.Info("lock held by another instance, skip", "lock", lockKey)
		return false, nil
	}
	defer func() {
		_ = s.locks.Release(context.Background(), lockKey)
	}()

	if err := fn(); err != nil {
		return true, err
	}
	return true, nil
}

// ---------- Locker ----------

// Locker 基于 Redis SETNX 的分布式锁
type Locker struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewLocker(rdb *redis.Client, ttl time.Duration) *Locker {
	return &Locker{rdb: rdb, ttl: ttl}
}

// TryAcquire 非阻塞获取锁
func (l *Locker) TryAcquire(ctx context.Context, key string) (bool, error) {
	ok, err := l.rdb.SetNX(ctx, key, "1", l.ttl).Result()
	return ok, err
}

// Release 释放锁（不检查持有者，适合短任务）
func (l *Locker) Release(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, key).Err()
}

// ---------- env helpers ----------

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// 编译期占位（避免未使用 model）
var _ = model.EhrSyncExecutionLog{}
