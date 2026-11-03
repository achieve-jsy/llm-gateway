package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	riskDetectorInterval       = 5 * time.Minute
	riskDetectorDedupeCooldown = 30 * time.Minute
	counterDiffThreshold       = 0.1 // 10% diff threshold for alerts
)

// PlatformRiskDetector 定期检测平台风险并发送告警
// - 计数器完整性：调用 RebuildCurrentPeriods 检查 drift
// - 周期健康：检查 Redis 中当前周期的 budget key 是否存在
// - Redis 健康：Ping + 连接池状态
type PlatformRiskDetector struct {
	db           *gorm.DB
	rdb          *redis.Client
	rebuildSvc   *BudgetCounterRebuildService
	periodConfig budgetperiod.Config
	notifier     Notifier
	logger       *slog.Logger
	stopCh       chan struct{}
}

func NewPlatformRiskDetector(
	db *gorm.DB,
	rdb *redis.Client,
	rebuildSvc *BudgetCounterRebuildService,
	periodConfig budgetperiod.Config,
	notifier Notifier,
) *PlatformRiskDetector {
	return &PlatformRiskDetector{
		db:           db,
		rdb:          rdb,
		rebuildSvc:   rebuildSvc,
		periodConfig: periodConfig,
		notifier:     notifier,
		logger:       slog.With("component", "platform_risk_detector"),
		stopCh:       make(chan struct{}),
	}
}

func (d *PlatformRiskDetector) Start() {
	d.logger.Info("starting platform risk detector", "interval", riskDetectorInterval)

	ticker := time.NewTicker(riskDetectorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.checkAll()
		case <-d.stopCh:
			d.logger.Info("platform risk detector stopped")
			return
		}
	}
}

func (d *PlatformRiskDetector) Stop() {
	close(d.stopCh)
}

func (d *PlatformRiskDetector) checkAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	d.checkCounterIntegrity(ctx)
	d.checkPeriodHealth(ctx)
	d.checkRedisHealth(ctx)
}

func (d *PlatformRiskDetector) checkCounterIntegrity(ctx context.Context) {
	d.logger.Debug("checking counter integrity")

	result, err := d.rebuildSvc.RebuildCurrentPeriods(ctx, time.Now())
	if err != nil {
		d.logger.Error("rebuild failed", "error", err)
		return
	}

	if result.AllDiffsWithinThreshold {
		d.logger.Debug("all counters within threshold")
		return
	}

	// Check each diff
	allDiffs := append(result.Diffs, result.LegacyV2Diffs...)
	violations := 0
	for _, diff := range allDiffs {
		if !diff.WithinThreshold && diff.DiffRatio > counterDiffThreshold {
			violations++
			d.logger.Warn("counter drift detected",
				"key", diff.Key,
				"expected", diff.ExpectedMicroUSD,
				"actual", diff.ActualMicroUSD,
				"ratio", diff.DiffRatio,
			)
		}
	}

	if violations > 0 {
		d.sendCounterIntegrityAlert(ctx, violations, result)
	}
}

func (d *PlatformRiskDetector) checkPeriodHealth(ctx context.Context) {
	d.logger.Debug("checking period health")

	now := time.Now()
	// 对 daily/weekly/monthly 三种周期都检查 budget key 是否存在
	periodTypes := []budgetdomain.PeriodType{
		budgetdomain.PeriodDaily,
		budgetdomain.PeriodWeekly,
		budgetdomain.PeriodMonthly,
	}

	for _, pt := range periodTypes {
		window, err := budgetperiod.Resolve(now, pt, d.periodConfig)
		if err != nil {
			d.logger.Error("resolve period window failed", "period_type", pt, "error", err)
			continue
		}

		periodKey := fmt.Sprintf("%s:%s", pt, window.Key)
		budgetKey := fmt.Sprintf("budget:v2:used:%s", periodKey)

		exists, err := d.rdb.Exists(ctx, budgetKey).Result()
		if err != nil {
			d.logger.Error("redis exists failed", "key", budgetKey, "error", err)
			continue
		}

		if exists == 0 {
			d.logger.Warn("period budget key missing", "key", budgetKey)
			d.sendPeriodHealthAlert(ctx, periodKey)
		} else {
			d.logger.Debug("period health OK", "key", budgetKey)
		}
	}
}

func (d *PlatformRiskDetector) checkRedisHealth(ctx context.Context) {
	d.logger.Debug("checking Redis health")

	start := time.Now()
	err := d.rdb.Ping(ctx).Err()
	latency := time.Since(start)

	if err != nil {
		d.logger.Error("Redis ping failed", "error", err)
		d.sendRedisHealthAlert(ctx, err)
		return
	}

	stats := d.rdb.PoolStats()
	if stats.IdleConns == 0 && stats.TotalConns > 0 {
		d.logger.Warn("Redis connection pool exhausted",
			"total", stats.TotalConns,
			"idle", stats.IdleConns,
			"stale", stats.StaleConns,
		)
		d.sendRedisHealthAlert(ctx, fmt.Errorf("connection pool exhausted"))
		return
	}

	d.logger.Debug("Redis health OK", "latency_ms", latency.Milliseconds())
}

func (d *PlatformRiskDetector) sendCounterIntegrityAlert(ctx context.Context, violations int, result *RebuildV2Result) {
	dedupeKey := fmt.Sprintf("platform_risk:counter_integrity:%s", time.Now().Format("2006-01-02-15"))

	d.sendNotification(ctx, cpmodel.NotificationTypePlatformRiskCounterIntegrity, cpmodel.NotificationLevelCritical, map[string]interface{}{
		"Violations": violations,
		"SourceRows": result.SourceRows,
		"KeysWritten": result.KeysWritten,
		"Cooldown":   riskDetectorDedupeCooldown.String(),
	}, dedupeKey)
}

func (d *PlatformRiskDetector) sendPeriodHealthAlert(ctx context.Context, periodKey string) {
	dedupeKey := fmt.Sprintf("platform_risk:period_health:%s", periodKey)

	d.sendNotification(ctx, cpmodel.NotificationTypePlatformRiskPeriodHealth, cpmodel.NotificationLevelWarning, map[string]interface{}{
		"PeriodKey": periodKey,
		"Cooldown":  riskDetectorDedupeCooldown.String(),
	}, dedupeKey)
}

func (d *PlatformRiskDetector) sendRedisHealthAlert(ctx context.Context, err error) {
	dedupeKey := fmt.Sprintf("platform_risk:redis_health:%s", time.Now().Format("2006-01-02-15"))

	d.sendNotification(ctx, cpmodel.NotificationTypePlatformRiskRedisHealth, cpmodel.NotificationLevelCritical, map[string]interface{}{
		"Message":  err.Error(),
		"Cooldown": riskDetectorDedupeCooldown.String(),
	}, dedupeKey)
}

func (d *PlatformRiskDetector) sendNotification(ctx context.Context, notifType, level string, data map[string]interface{}, dedupeKey string) {
	notification := &cpmodel.Notification{
		ID:        uuid.New(),
		UserID:    uuid.Nil, // 系统通知
		Type:      notifType,
		Level:     level,
		DedupeKey: dedupeKey,
		CreatedAt: time.Now(),
		TemplateData: data,
	}

	if err := d.notifier.Notify(ctx, notification); err != nil {
		d.logger.Error("send notification failed", "error", err, "dedupe_key", dedupeKey)
	} else {
		d.logger.Info("notification sent", "dedupe_key", dedupeKey)
	}
}
