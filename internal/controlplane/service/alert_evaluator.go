package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	alertEvaluatorJobName     = "alert_evaluator"
	alertEvaluatorLockKey     = "ops:alert:evaluator:leader"
	alertEvaluatorLockTTL     = 90 * time.Second
	alertEvaluatorTimeout     = 45 * time.Second
	defaultEvalInterval       = 60 * time.Second
	settingKeyAlertRuntime    = "ops_alert_runtime_settings"
	alertEvaluatorSkipLogInterval = 1 * time.Minute
)

// Lua 脚本：原子释放锁（只释放自己持有的锁）
var alertEvaluatorReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// validMetricTypes 所有支持的 metric_type 枚举（13 种）
var validMetricTypes = map[string]bool{
	"success_rate":              true,
	"error_rate":                true,
	"upstream_error_rate":       true,
	"cpu_usage_percent":         true,
	"memory_usage_percent":      true,
	"concurrency_queue_depth":   true,
	"group_available_accounts":  true,
	"group_available_ratio":     true,
	"group_rate_limit_ratio":    true,
	"account_rate_limited_count": true,
	"account_error_count":       true,
	"account_error_ratio":       true,
	"overload_account_count":    true,
}

// implementedMetricTypes 本期已实现真实查询的 metric_type
var implementedMetricTypes = map[string]bool{
	"success_rate":        true,
	"error_rate":          true,
	"upstream_error_rate": true,
}

// alertRuntimeSettings 告警运行时设置（从 system_configs 读取）
type alertRuntimeSettings struct {
	EvaluationInterval int                   `json:"evaluation_interval_seconds"`
	DistributedLock    distributedLockConfig `json:"distributed_lock"`
}

type distributedLockConfig struct {
	Enabled    bool   `json:"enabled"`
	Key        string `json:"key"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// ruleState 规则评估状态（内存中跟踪 sustained/cooldown）
type ruleState struct {
	lastEvaluatedAt       time.Time
	consecutiveBreaches   int
	consecutiveRecoveries int
}

// AlertEvaluator 告警评估器：定期评估告警规则、创建/恢复事件
type AlertEvaluator struct {
	db           *gorm.DB
	rdb          *redis.Client
	settingsRepo *repository.SettingsRepo

	instanceID string
	stopCh     chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	wg         sync.WaitGroup

	mu         sync.Mutex
	ruleStates map[uint]*ruleState

	skipLogMu       sync.Mutex
	skipLogAt       time.Time
	warnNoRedisOnce sync.Once
}

// NewAlertEvaluator 创建告警评估器
func NewAlertEvaluator(db *gorm.DB, rdb *redis.Client, settingsRepo *repository.SettingsRepo) *AlertEvaluator {
	return &AlertEvaluator{
		db:           db,
		rdb:          rdb,
		settingsRepo: settingsRepo,
		instanceID:   uuid.NewString(),
		ruleStates:   make(map[uint]*ruleState),
	}
}

// Start 启动评估器 goroutine
func (e *AlertEvaluator) Start(ctx context.Context) {
	if e == nil {
		return
	}
	e.startOnce.Do(func() {
		if e.stopCh == nil {
			e.stopCh = make(chan struct{})
		}
		e.wg.Add(1)
		go e.run(ctx)
		slog.Info("alert_evaluator: started", "instance_id", e.instanceID)
	})
}

// Stop 停止评估器
func (e *AlertEvaluator) Stop() {
	if e == nil {
		return
	}
	e.stopOnce.Do(func() {
		if e.stopCh != nil {
			close(e.stopCh)
		}
	})
	e.wg.Wait()
	slog.Info("alert_evaluator: stopped", "instance_id", e.instanceID)
}

// run 评估循环
func (e *AlertEvaluator) run(ctx context.Context) {
	defer e.wg.Done()

	timer := time.NewTimer(0) // 启动后立即评估一次
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			interval := e.getInterval()
			e.evaluateOnce(ctx, interval)
			timer.Reset(interval)
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// getInterval 从 AlertRuntimeSettings 读取评估间隔，默认 60s
func (e *AlertEvaluator) getInterval() time.Duration {
	settings := e.loadRuntimeSettings()
	if settings.EvaluationInterval <= 0 || settings.EvaluationInterval > 86400 {
		return defaultEvalInterval
	}
	return time.Duration(settings.EvaluationInterval) * time.Second
}

// loadRuntimeSettings 从 system_configs 读取告警运行时设置
func (e *AlertEvaluator) loadRuntimeSettings() alertRuntimeSettings {
	val, err := e.settingsRepo.GetValue(settingKeyAlertRuntime)
	if err != nil || val == "" {
		return alertRuntimeSettings{EvaluationInterval: 60}
	}
	var settings alertRuntimeSettings
	if err := json.Unmarshal([]byte(val), &settings); err != nil {
		return alertRuntimeSettings{EvaluationInterval: 60}
	}
	if settings.EvaluationInterval <= 0 {
		settings.EvaluationInterval = 60
	}
	return settings
}

// evaluateOnce 执行一次评估周期
func (e *AlertEvaluator) evaluateOnce(ctx context.Context, interval time.Duration) {
	evalCtx, cancel := context.WithTimeout(ctx, alertEvaluatorTimeout)
	defer cancel()

	runtimeCfg := e.loadRuntimeSettings()

	// 尝试获取分布式锁
	release, ok := e.tryAcquireLeaderLock(evalCtx, runtimeCfg.DistributedLock)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	startedAt := time.Now().UTC()
	runAt := startedAt

	// 加载启用的规则
	rules, err := e.loadEnabledRules(evalCtx)
	if err != nil {
		e.recordHeartbeatError(runAt, time.Since(startedAt), err)
		slog.Error("alert_evaluator: load rules failed", "error", err)
		return
	}

	rulesTotal := len(rules)
	rulesEnabled := 0
	rulesEvaluated := 0
	eventsCreated := 0
	eventsResolved := 0

	now := time.Now().UTC()
	safeEnd := now.Truncate(time.Minute)
	if safeEnd.IsZero() {
		safeEnd = now
	}

	// 清理已删除规则的过期状态
	e.pruneRuleStates(rules)

	for _, rule := range rules {
		if !rule.Enabled || rule.ID <= 0 {
			continue
		}
		rulesEnabled++

		// 检查 metric_type 是否有效
		if !validMetricTypes[strings.TrimSpace(rule.MetricType)] {
			slog.Warn("alert_evaluator: unknown metric_type, skipping rule",
				"rule_id", rule.ID,
				"metric_type", rule.MetricType,
			)
			e.resetRuleState(rule.ID, now)
			continue
		}

		// 解析 filters 获取 platform/group/model
		platform, groupName, modelName := parseAlertRuleFilters(rule.Filters)

		// 检查静默（silence）
		if e.isRuleSilenced(evalCtx, rule.ID, platform, groupName, now) {
			e.resetRuleState(rule.ID, now)
			continue
		}

		windowMinutes := rule.WindowMinutes
		if windowMinutes <= 0 {
			windowMinutes = 1
		}
		windowStart := safeEnd.Add(-time.Duration(windowMinutes) * time.Minute)
		windowEnd := safeEnd

		// 计算指标值
		metricValue, ok := e.computeRuleMetric(evalCtx, rule, windowStart, windowEnd, platform, groupName, modelName)
		if !ok {
			e.resetRuleState(rule.ID, now)
			continue
		}
		rulesEvaluated++

		// 判断是否触发阈值
		breachedNow := compareMetric(metricValue, rule.Operator, rule.Threshold)
		required := requiredSustainedBreaches(rule.SustainedMinutes, interval)
		consecutiveBreaches, consecutiveRecoveries := e.updateRuleBreaches(rule.ID, now, interval, breachedNow)

		// 查找当前活跃的告警事件
		activeEvent, err := e.getActiveEvent(evalCtx, rule.ID)
		if err != nil {
			slog.Error("alert_evaluator: get active event failed",
				"rule_id", rule.ID, "error", err,
			)
			continue
		}

		// breached: 检查是否需要创建事件
		if breachedNow && consecutiveBreaches >= required {
			if activeEvent != nil {
				// 已有活跃事件，不重复创建
				continue
			}

			// 检查 cooldown
			if e.isInCooldown(evalCtx, rule.ID, rule.CooldownMinutes, now) {
				continue
			}

			// 创建告警事件
			if err := e.createAlertEvent(evalCtx, rule, metricValue, platform, groupName, modelName, now); err != nil {
				slog.Error("alert_evaluator: create event failed",
					"rule_id", rule.ID, "error", err,
				)
				continue
			}
			eventsCreated++
			continue
		}

		// 未触发：检查是否需要自动恢复
		if activeEvent != nil && consecutiveRecoveries >= required {
			resolvedAt := now
			if err := e.resolveAlertEvent(evalCtx, activeEvent.ID, &resolvedAt); err != nil {
				slog.Error("alert_evaluator: resolve event failed",
					"event_id", activeEvent.ID, "error", err,
				)
			} else {
				eventsResolved++
			}
		}
	}

	result := fmt.Sprintf("rules=%d enabled=%d evaluated=%d created=%d resolved=%d",
		rulesTotal, rulesEnabled, rulesEvaluated, eventsCreated, eventsResolved,
	)
	e.recordHeartbeatSuccess(runAt, time.Since(startedAt), result)

	slog.Info("alert_evaluator: cycle completed",
		"rules_total", rulesTotal,
		"rules_enabled", rulesEnabled,
		"rules_evaluated", rulesEvaluated,
		"events_created", eventsCreated,
		"events_resolved", eventsResolved,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
}

// loadEnabledRules 从数据库加载所有启用的告警规则
func (e *AlertEvaluator) loadEnabledRules(ctx context.Context) ([]model.AlertRule, error) {
	var rules []model.AlertRule
	err := e.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error
	return rules, err
}

// parseAlertRuleFilters 解析规则的 Filters JSON 字段，提取 platform/group/model
func parseAlertRuleFilters(filters datatypes.JSON) (platform, groupName, modelName string) {
	if len(filters) == 0 {
		return "", "", ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(filters, &m); err != nil {
		return "", "", ""
	}
	if v, ok := m["platform"]; ok {
		if s, ok := v.(string); ok {
			platform = strings.TrimSpace(s)
		}
	}
	if v, ok := m["group_name"]; ok {
		if s, ok := v.(string); ok {
			groupName = strings.TrimSpace(s)
		}
	}
	if v, ok := m["model"]; ok {
		if s, ok := v.(string); ok {
			modelName = strings.TrimSpace(s)
		}
	}
	// 兼容 group_id 数字格式
	if groupName == "" {
		if v, ok := m["group_id"]; ok {
			groupName = fmt.Sprintf("%v", v)
		}
	}
	return
}

// computeRuleMetric 根据 metric_type 计算指标值
// 返回 (value, ok)。ok=false 表示无法计算（无数据/不支持）
func (e *AlertEvaluator) computeRuleMetric(
	ctx context.Context,
	rule model.AlertRule,
	windowStart, windowEnd time.Time,
	platform, groupName, modelName string,
) (float64, bool) {
	metricType := strings.TrimSpace(rule.MetricType)

	// 对于非本期实现的 metric_type，返回 false
	if !implementedMetricTypes[metricType] {
		slog.Warn("alert_evaluator: metric_type not yet implemented (TODO)",
			"rule_id", rule.ID,
			"metric_type", metricType,
		)
		return 0, false
	}

	// 本期实现的三种指标：从 call_logs 查询
	return e.queryCallLogsMetric(ctx, metricType, windowStart, windowEnd, platform, groupName, modelName)
}

// queryCallLogsMetric 从 call_logs 表查询指标值
func (e *AlertEvaluator) queryCallLogsMetric(
	ctx context.Context,
	metricType string,
	windowStart, windowEnd time.Time,
	platform, groupName, modelName string,
) (float64, bool) {
	// 构建动态查询
	type row struct {
		Total       int64 `gorm:"column:total"`
		SuccessCount int64 `gorm:"column:success_count"`
		UpstreamErr  int64 `gorm:"column:upstream_err"`
	}

	selectClauses := []string{
		"COUNT(*) AS total",
		"SUM(CASE WHEN status >= 200 AND status < 300 THEN 1 ELSE 0 END) AS success_count",
		"SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END) AS upstream_err",
	}

	query := e.db.WithContext(ctx).
		Table("call_logs").
		Select(strings.Join(selectClauses, ", "))

	query = query.Where("ts >= ? AND ts <= ?", windowStart, windowEnd)

	if platform != "" {
		query = query.Where("provider = ?", platform)
	}
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}

	var r row
	if err := query.Scan(&r).Error; err != nil {
		slog.Error("alert_evaluator: query call_logs failed",
			"metric_type", metricType, "error", err,
		)
		return 0, false
	}

	if r.Total == 0 {
		// 无数据：不触发告警
		return 0, false
	}

	switch metricType {
	case "success_rate":
		return (float64(r.SuccessCount) / float64(r.Total)) * 100, true
	case "error_rate":
		errorCount := r.Total - r.SuccessCount
		return (float64(errorCount) / float64(r.Total)) * 100, true
	case "upstream_error_rate":
		return (float64(r.UpstreamErr) / float64(r.Total)) * 100, true
	default:
		return 0, false
	}
}

// compareMetric 比较指标值与阈值
func compareMetric(value float64, operator string, threshold float64) bool {
	switch strings.TrimSpace(operator) {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

// requiredSustainedBreaches 计算 sustained_minutes 对应的评估周期数
func requiredSustainedBreaches(sustainedMinutes int, interval time.Duration) int {
	if sustainedMinutes <= 0 {
		return 1
	}
	if interval <= 0 {
		return sustainedMinutes
	}
	required := int(math.Ceil(float64(sustainedMinutes*60) / interval.Seconds()))
	if required < 1 {
		return 1
	}
	return required
}

// updateRuleBreaches 更新规则触发计数，返回 (breaches, recoveries)
func (e *AlertEvaluator) updateRuleBreaches(ruleID uint, now time.Time, interval time.Duration, breached bool) (int, int) {
	if ruleID <= 0 {
		return 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.ruleStates[ruleID]
	if !ok {
		state = &ruleState{}
		e.ruleStates[ruleID] = state
	}

	// 如果距离上次评估超过 2 倍间隔，重置状态
	if !state.lastEvaluatedAt.IsZero() && interval > 0 {
		if now.Sub(state.lastEvaluatedAt) > interval*2 {
			state.consecutiveBreaches = 0
			state.consecutiveRecoveries = 0
		}
	}

	state.lastEvaluatedAt = now
	if breached {
		state.consecutiveBreaches++
		state.consecutiveRecoveries = 0
	} else {
		state.consecutiveBreaches = 0
		state.consecutiveRecoveries++
	}
	return state.consecutiveBreaches, state.consecutiveRecoveries
}

// resetRuleState 重置规则状态
func (e *AlertEvaluator) resetRuleState(ruleID uint, now time.Time) {
	if ruleID <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.ruleStates[ruleID]
	if !ok {
		state = &ruleState{}
		e.ruleStates[ruleID] = state
	}
	state.lastEvaluatedAt = now
	state.consecutiveBreaches = 0
	state.consecutiveRecoveries = 0
}

// pruneRuleStates 清理已删除规则的状态
func (e *AlertEvaluator) pruneRuleStates(rules []model.AlertRule) {
	e.mu.Lock()
	defer e.mu.Unlock()

	live := make(map[uint]struct{}, len(rules))
	for _, r := range rules {
		if r.ID > 0 {
			live[r.ID] = struct{}{}
		}
	}
	for id := range e.ruleStates {
		if _, ok := live[id]; !ok {
			delete(e.ruleStates, id)
		}
	}
}

// getActiveEvent 获取规则当前活跃（firing）的告警事件
func (e *AlertEvaluator) getActiveEvent(ctx context.Context, ruleID uint) (*model.AlertEvent, error) {
	var event model.AlertEvent
	err := e.db.WithContext(ctx).
		Where("rule_id = ? AND status = ?", ruleID, "firing").
		Order("fired_at DESC").
		First(&event).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// isInCooldown 检查规则是否在 cooldown 期内
func (e *AlertEvaluator) isInCooldown(ctx context.Context, ruleID uint, cooldownMinutes int, now time.Time) bool {
	if cooldownMinutes <= 0 {
		return false
	}

	var latestEvent model.AlertEvent
	err := e.db.WithContext(ctx).
		Where("rule_id = ?", ruleID).
		Order("fired_at DESC").
		First(&latestEvent).Error
	if err == gorm.ErrRecordNotFound {
		return false
	}
	if err != nil {
		slog.Error("alert_evaluator: check cooldown failed",
			"rule_id", ruleID, "error", err,
		)
		return false
	}

	cooldownDuration := time.Duration(cooldownMinutes) * time.Minute
	return now.Sub(latestEvent.FiredAt) < cooldownDuration
}

// isRuleSilenced 检查规则是否被静默
func (e *AlertEvaluator) isRuleSilenced(ctx context.Context, ruleID uint, platform, groupName string, now time.Time) bool {
	var count int64
	query := e.db.WithContext(ctx).Model(&model.AlertSilence{}).
		Where("rule_id = ? AND until > ?", ruleID, now)

	if platform != "" {
		query = query.Where("platform = ? OR platform = ''", platform)
	}
	if groupName != "" {
		// AlertSilence 的 GroupID 是 *int 类型，groupName 是字符串
		// 如果 filters 里是 group_name 字符串，我们尝试匹配
		var groupID int
		if _, err := fmt.Sscanf(groupName, "%d", &groupID); err == nil && groupID > 0 {
			query = query.Where("(group_id = ? OR group_id IS NULL)", groupID)
		}
	}

	if err := query.Count(&count).Error; err != nil {
		slog.Error("alert_evaluator: check silence failed",
			"rule_id", ruleID, "error", err,
		)
		return false
	}
	return count > 0
}

// createAlertEvent 创建告警事件
func (e *AlertEvaluator) createAlertEvent(
	ctx context.Context,
	rule model.AlertRule,
	metricValue float64,
	platform, groupName, modelName string,
	now time.Time,
) error {
	metricType := strings.TrimSpace(rule.MetricType)
	severity := strings.TrimSpace(rule.Severity)
	if severity == "" {
		severity = "P1"
	}

	title := fmt.Sprintf("%s: %s", severity, strings.TrimSpace(rule.Name))
	description := buildAlertEventDescription(rule, metricValue, platform, groupName, modelName)
	dimensions := buildAlertEventDimensions(platform, groupName, modelName)

	event := model.AlertEvent{
		RuleID:         rule.ID,
		Severity:       severity,
		Status:         "firing",
		Title:          title,
		Description:    description,
		MetricValue:    metricValue,
		ThresholdValue: rule.Threshold,
		Dimensions:     dimensions,
		FiredAt:        now,
		CreatedAt:      now,
	}

	if err := e.db.WithContext(ctx).Create(&event).Error; err != nil {
		return err
	}

	slog.Info("alert_evaluator: event created",
		"event_id", event.ID,
		"rule_id", rule.ID,
		"metric_type", metricType,
		"metric_value", fmt.Sprintf("%.2f", metricValue),
		"threshold", fmt.Sprintf("%.2f", rule.Threshold),
		"severity", severity,
	)
	return nil
}

// resolveAlertEvent 将告警事件标记为 resolved
func (e *AlertEvaluator) resolveAlertEvent(ctx context.Context, eventID uint, resolvedAt *time.Time) error {
	updates := map[string]interface{}{
		"status":      "resolved",
		"resolved_at": resolvedAt,
	}
	if err := e.db.WithContext(ctx).Model(&model.AlertEvent{}).Where("id = ?", eventID).Updates(updates).Error; err != nil {
		return err
	}
	slog.Info("alert_evaluator: event resolved", "event_id", eventID)
	return nil
}

// buildAlertEventDescription 构建告警事件描述
func buildAlertEventDescription(rule model.AlertRule, value float64, platform, groupName, modelName string) string {
	scope := "overall"
	if platform != "" {
		scope = fmt.Sprintf("platform=%s", platform)
	}
	if groupName != "" {
		scope = fmt.Sprintf("%s group=%s", scope, groupName)
	}
	if modelName != "" {
		scope = fmt.Sprintf("%s model=%s", scope, modelName)
	}

	windowMinutes := rule.WindowMinutes
	if windowMinutes <= 0 {
		windowMinutes = 1
	}

	return fmt.Sprintf("%s %s %.2f (current %.2f) over last %dm (%s)",
		strings.TrimSpace(rule.MetricType),
		strings.TrimSpace(rule.Operator),
		rule.Threshold,
		value,
		windowMinutes,
		strings.TrimSpace(scope),
	)
}

// buildAlertEventDimensions 构建告警事件维度
func buildAlertEventDimensions(platform, groupName, modelName string) datatypes.JSON {
	dims := map[string]interface{}{}
	if platform != "" {
		dims["platform"] = platform
	}
	if groupName != "" {
		dims["group_name"] = groupName
	}
	if modelName != "" {
		dims["model"] = modelName
	}
	if len(dims) == 0 {
		return nil
	}
	b, _ := json.Marshal(dims)
	return b
}

// tryAcquireLeaderLock 尝试获取 Redis 分布式锁
// 返回 (releaseFunc, acquired)。releaseFunc 用于释放锁，nil 表示无需释放。
func (e *AlertEvaluator) tryAcquireLeaderLock(ctx context.Context, lockCfg distributedLockConfig) (func(), bool) {
	if !lockCfg.Enabled {
		return nil, true
	}
	if e.rdb == nil {
		e.warnNoRedisOnce.Do(func() {
			slog.Warn("alert_evaluator: redis not configured; running without distributed lock")
		})
		return nil, true
	}

	key := strings.TrimSpace(lockCfg.Key)
	if key == "" {
		key = alertEvaluatorLockKey
	}
	ttl := time.Duration(lockCfg.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = alertEvaluatorLockTTL
	}

	ok, err := e.rdb.SetNX(ctx, key, e.instanceID, ttl).Result()
	if err != nil {
		e.warnNoRedisOnce.Do(func() {
			slog.Warn("alert_evaluator: leader lock SetNX failed, skipping cycle",
				"error", err,
			)
		})
		return nil, false
	}
	if !ok {
		e.maybeLogSkip(key)
		return nil, false
	}

	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = alertEvaluatorReleaseScript.Run(releaseCtx, e.rdb, []string{key}, e.instanceID).Result()
	}, true
}

// maybeLogSkip 限频打印跳过日志（每分钟最多一次）
func (e *AlertEvaluator) maybeLogSkip(key string) {
	e.skipLogMu.Lock()
	defer e.skipLogMu.Unlock()

	now := time.Now()
	if !e.skipLogAt.IsZero() && now.Sub(e.skipLogAt) < alertEvaluatorSkipLogInterval {
		return
	}
	e.skipLogAt = now
	slog.Info("alert_evaluator: leader lock held by another instance, skipping",
		"lock_key", key,
	)
}

// recordHeartbeatSuccess 记录成功心跳
func (e *AlertEvaluator) recordHeartbeatSuccess(runAt time.Time, duration time.Duration, result string) {
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := strings.TrimSpace(result)
	if msg == "" {
		msg = "ok"
	}
	if len(msg) > 2048 {
		msg = msg[:2048]
	}

	e.upsertHeartbeat(&model.AlertJobHeartbeat{
		Name:           alertEvaluatorJobName,
		Status:         "running",
		LastDurationMs: int(durMs),
		LastSuccessAt:  &now,
		LastResult:     msg,
		UpdatedAt:      now,
	})
}

// recordHeartbeatError 记录错误心跳
func (e *AlertEvaluator) recordHeartbeatError(runAt time.Time, duration time.Duration, err error) {
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	errMsg := err.Error()
	if len(errMsg) > 2048 {
		errMsg = errMsg[:2048]
	}

	e.upsertHeartbeat(&model.AlertJobHeartbeat{
		Name:           alertEvaluatorJobName,
		Status:         "error",
		LastDurationMs: int(durMs),
		LastErrorAt:    &now,
		LastError:      errMsg,
		UpdatedAt:      now,
	})
}

// upsertHeartbeat 插入或更新心跳记录
func (e *AlertEvaluator) upsertHeartbeat(hb *model.AlertJobHeartbeat) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := e.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "last_duration_ms", "last_success_at", "last_error_at", "last_result", "last_error", "updated_at"}),
	}).Create(hb).Error; err != nil {
		slog.Error("alert_evaluator: upsert heartbeat failed", "error", err)
	}
}