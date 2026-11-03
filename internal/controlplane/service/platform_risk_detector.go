package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	// lastPoolTimeouts 记录上次采样的连接池等待超时累计值，仅检测器单 goroutine 访问。
	lastPoolTimeouts uint32
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
	// 先建立连接池等待超时基线：Timeouts 是单调累计值，基线之前的历史不算本周期告警。
	d.lastPoolTimeouts = d.rdb.PoolStats().Timeouts
	d.logger.Info("starting platform risk detector", "interval", riskDetectorInterval)

	// 启动立即先跑一次，消除首检 5 分钟冷启动盲区（进程刚启动的故障不可感知）。
	d.checkAll()

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

	// 预算计数器按作用域存储（budget:v2:used:<scopeType>:<scopeID>:<windowKey>），不存在单一周期级 key。
	// 周期健康只能遍历启用预算的真实作用域，检查各自当前周期的计数器 key 是否已写入。
	var allocs []cpmodel.BudgetAllocation
	if err := d.db.WithContext(ctx).Where("enabled = ?", true).Find(&allocs).Error; err != nil {
		d.logger.Error("period health: load budget allocations failed", "error", err)
		return
	}
	if len(allocs) == 0 {
		return
	}

	now := time.Now()
	type scopeCheck struct {
		periodKey string // pt:windowKey，如 daily:2026-08-03
		label     string // 作用域标识，如 dept_daily:xxx
		key       string // 真实 Redis key
	}
	var checks []scopeCheck
	collect := func(scopeType, scopeID string, pt budgetdomain.PeriodType, enabled bool) {
		if !enabled {
			return
		}
		window, err := budgetperiod.Resolve(now, pt, d.periodConfig)
		if err != nil {
			d.logger.Error("resolve period window failed", "period_type", pt, "error", err)
			return
		}
		checks = append(checks, scopeCheck{
			periodKey: fmt.Sprintf("%s:%s", pt, window.Key),
			label:     fmt.Sprintf("%s:%s", scopeType, scopeID),
			key:       fmt.Sprintf("budget:v2:used:%s:%s:%s", scopeType, scopeID, window.Key),
		})
	}

	for _, a := range allocs {
		collect(a.ScopeType, a.ScopeID, budgetdomain.PeriodDaily, a.DailyAmountUSD >= 0)
		collect(a.ScopeType, a.ScopeID, budgetdomain.PeriodWeekly, a.WeeklyAmountUSD >= 0)
		collect(a.ScopeType, a.ScopeID, budgetdomain.PeriodMonthly, a.AmountUSD >= 0)
	}
	if len(checks) == 0 {
		return
	}

	pipe := d.rdb.TxPipeline()
	cmds := make([]*redis.IntCmd, len(checks))
	for i, c := range checks {
		cmds[i] = pipe.Exists(ctx, c.key)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		d.logger.Error("period health: redis exists batch failed", "error", err)
		return
	}

	// 按周期窗口聚合缺失作用域，每个周期独立去重，避免单 scope 缺失误伤整个周期。
	missing := map[string][]string{}
	for i, c := range checks {
		if cmds[i].Val() == 0 {
			missing[c.periodKey] = append(missing[c.periodKey], c.label)
		}
	}
	for periodKey, scopes := range missing {
		d.sendPeriodHealthAlert(ctx, periodKey, scopes)
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
	// IdleConns==0 是高负载健康池的正常瞬时状态，不能当耗尽信号。
	// 用「本周期内是否发生连接等待超时」判定池耗尽：Timeouts 单调累计，取与上次采样的增量。
	if stats.Timeouts > d.lastPoolTimeouts {
		d.lastPoolTimeouts = stats.Timeouts
		d.logger.Warn("Redis connection pool wait timeout",
			"total", stats.TotalConns,
			"idle", stats.IdleConns,
			"timeouts", stats.Timeouts,
		)
		d.sendRedisHealthAlert(ctx, fmt.Errorf("connection pool wait timeout: %d (累计)", stats.Timeouts))
		return
	}
	d.lastPoolTimeouts = stats.Timeouts

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

func (d *PlatformRiskDetector) sendPeriodHealthAlert(ctx context.Context, periodKey string, missingScopes []string) {
	dedupeKey := fmt.Sprintf("platform_risk:period_health:%s", periodKey)

	// 限制 scope 明细条数，避免大量缺失时通知文本爆炸；统计仍保留总数。
	scopes := missingScopes
	if len(scopes) > 10 {
		scopes = append(scopes[:10], fmt.Sprintf("等 %d 个", len(missingScopes)))
	}

	d.sendNotification(ctx, cpmodel.NotificationTypePlatformRiskPeriodHealth, cpmodel.NotificationLevelWarning, map[string]interface{}{
		"PeriodKey":     periodKey,
		"MissingScopes": strings.Join(scopes, ", "),
		"Cooldown":      riskDetectorDedupeCooldown.String(),
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
