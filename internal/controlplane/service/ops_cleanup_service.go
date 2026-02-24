package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const (
	opsCleanupLeaderKey     = "ops:cleanup:leader"
	opsCleanupLockTTL       = 30 * time.Minute
	opsCleanupLockExtendTTL = 5 * time.Minute
	defaultCleanupSchedule  = "0 3 * * *" // 每天凌晨 3:00
	defaultCallLogRetention = 30
	defaultErrorLogRetention = 90
)

// OpsCleanupService 定期清理过期 call_logs 和 system_logs。
// 使用 robfig/cron 定时调度 + Redis leader lock 保证单实例执行。
type OpsCleanupService struct {
	db       *gorm.DB
	rdb      *redis.Client
	cron     *cron.Cron
	instanceID string

	// retention 配置（运行时从 settings 读取）
	callLogRetentionDays  int
	errorLogRetentionDays int
	cronSchedule          string

	mu sync.RWMutex
}

// NewOpsCleanupService 创建 OpsCleanupService 实例。
func NewOpsCleanupService(db *gorm.DB, rdb *redis.Client) *OpsCleanupService {
	hostname, _ := os.Hostname()
	return &OpsCleanupService{
		db:                    db,
		rdb:                   rdb,
		instanceID:            fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()),
		callLogRetentionDays:  defaultCallLogRetention,
		errorLogRetentionDays: defaultErrorLogRetention,
		cronSchedule:          defaultCleanupSchedule,
	}
}

// SetRetention 设置保留天数（可在 Start 前调用覆盖默认值）。
func (s *OpsCleanupService) SetRetention(callLogDays, errorLogDays int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if callLogDays > 0 {
		s.callLogRetentionDays = callLogDays
	}
	if errorLogDays > 0 {
		s.errorLogRetentionDays = errorLogDays
	}
}

// SetSchedule 设置 cron 表达式（可在 Start 前调用覆盖默认值）。
func (s *OpsCleanupService) SetSchedule(schedule string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if schedule != "" {
		s.cronSchedule = schedule
	}
}

// Start 启动 cron 调度器。
func (s *OpsCleanupService) Start(ctx context.Context) {
	s.cron = cron.New(cron.WithSeconds())

	_, err := s.cron.AddFunc(s.cronSchedule, func() {
		s.runCleanup(ctx)
	})
	if err != nil {
		slog.Error("ops_cleanup: failed to add cron job", "error", err)
		return
	}

	s.cron.Start()
	slog.Info("ops_cleanup_service_started",
		"schedule", s.cronSchedule,
		"call_log_retention_days", s.callLogRetentionDays,
		"error_log_retention_days", s.errorLogRetentionDays,
		"instance_id", s.instanceID,
	)
}

// Stop 停止 cron 调度器。
func (s *OpsCleanupService) Stop() {
	if s.cron != nil {
		s.cron.Stop()
	}
}

// acquireLeaderLock 尝试获取 Redis leader lock。
func (s *OpsCleanupService) acquireLeaderLock(ctx context.Context) bool {
	if s.rdb == nil {
		return false
	}
	ok, err := s.rdb.SetNX(ctx, opsCleanupLeaderKey, s.instanceID, opsCleanupLockTTL).Result()
	if err != nil {
		slog.Warn("ops_cleanup: leader lock SetNX failed", "error", err)
		return false
	}
	return ok
}

// releaseLeaderLock 释放 leader lock（仅当 lock 值匹配时）。
func (s *OpsCleanupService) releaseLeaderLock(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	// 使用 Lua 脚本保证原子性：仅当值匹配时才删除
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)
	result, err := script.Run(ctx, s.rdb, []string{opsCleanupLeaderKey}, s.instanceID).Int64()
	if err != nil {
		slog.Warn("ops_cleanup: leader lock release failed", "error", err)
	}
	if result == 0 {
		slog.Debug("ops_cleanup: lock already released by another instance")
	}
}

// extendLeaderLock 续期 leader lock。
func (s *OpsCleanupService) extendLeaderLock(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)
	_, err := script.Run(ctx, s.rdb, []string{opsCleanupLeaderKey}, s.instanceID, int(opsCleanupLockExtendTTL.Seconds())).Result()
	if err != nil {
		slog.Warn("ops_cleanup: leader lock extend failed", "error", err)
	}
}

// runCleanup 执行清理逻辑。
func (s *OpsCleanupService) runCleanup(ctx context.Context) {
	if !s.acquireLeaderLock(ctx) {
		slog.Debug("ops_cleanup: not leader, skipping cleanup cycle")
		return
	}
	defer s.releaseLeaderLock(ctx)

	slog.Info("ops_cleanup: starting cleanup cycle",
		"call_log_retention_days", s.callLogRetentionDays,
		"error_log_retention_days", s.errorLogRetentionDays,
	)

	// 启动续期 goroutine
	ctxExtend, cancelExtend := context.WithCancel(ctx)
	defer cancelExtend()
	go func() {
		ticker := time.NewTicker(opsCleanupLockExtendTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctxExtend.Done():
				return
			case <-ticker.C:
				s.extendLeaderLock(ctx)
			}
		}
	}()

	// 清理 call_logs
	s.cleanupCallLogs(ctx)

	// 清理 system_logs
	s.cleanupSystemLogs(ctx)

	slog.Info("ops_cleanup: cleanup cycle completed")
}

// cleanupCallLogs 清理过期的 call_logs。
func (s *OpsCleanupService) cleanupCallLogs(ctx context.Context) {
	s.mu.RLock()
	days := s.callLogRetentionDays
	s.mu.RUnlock()

	slog.Info("ops_cleanup: cleaning call_logs", "retention_days", days)

	// 兼容 SQLite / Postgres
	deleteSQL := fmt.Sprintf("DELETE FROM call_logs WHERE ts < NOW() - INTERVAL '%d days'", days)
	if s.db.Dialector.Name() == "sqlite" {
		deleteSQL = fmt.Sprintf("DELETE FROM call_logs WHERE ts < datetime('now','-%d days')", days)
	}
	result := s.db.WithContext(ctx).Exec(deleteSQL)
	if result.Error != nil {
		slog.Error("ops_cleanup: failed to cleanup call_logs", "error", result.Error)
		return
	}
	slog.Info("ops_cleanup: call_logs cleanup done", "deleted_rows", result.RowsAffected)
}

// cleanupSystemLogs 清理过期的 system_logs。
func (s *OpsCleanupService) cleanupSystemLogs(ctx context.Context) {
	s.mu.RLock()
	days := s.errorLogRetentionDays
	s.mu.RUnlock()

	slog.Info("ops_cleanup: cleaning system_logs", "retention_days", days)

	deleteSQL := fmt.Sprintf("DELETE FROM system_logs WHERE ts < NOW() - INTERVAL '%d days'", days)
	if s.db.Dialector.Name() == "sqlite" {
		deleteSQL = fmt.Sprintf("DELETE FROM system_logs WHERE ts < datetime('now','-%d days')", days)
	}
	result := s.db.WithContext(ctx).Exec(deleteSQL)
	if result.Error != nil {
		slog.Error("ops_cleanup: failed to cleanup system_logs", "error", result.Error)
		return
	}
	slog.Info("ops_cleanup: system_logs cleanup done", "deleted_rows", result.RowsAffected)
}

// LoadRetentionFromSettings 从 settings 加载保留天数配置。
func (s *OpsCleanupService) LoadRetentionFromSettings(settingsValue string) {
	if settingsValue == "" {
		return
	}
	var settings struct {
		DataRetentionDays int `json:"data_retention_days"`
		ErrorLogRetention int `json:"error_log_retention_days"`
	}
	if err := json.Unmarshal([]byte(settingsValue), &settings); err != nil {
		slog.Warn("ops_cleanup: failed to parse advanced settings", "error", err)
		return
	}
	s.SetRetention(settings.DataRetentionDays, settings.ErrorLogRetention)
}