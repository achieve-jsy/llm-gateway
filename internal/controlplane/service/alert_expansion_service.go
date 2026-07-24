package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────────────────────────────────────
// 依赖接口（供测试注入 fake）
// ─────────────────────────────────────────────────────────────────────────────

// budgetAllocReader 仅需从 BudgetRepo 读取额度，用最小接口。
type budgetAllocReader interface {
	GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error)
}

// alertRecordStore 告警去重表操作接口。
type alertRecordStore interface {
	ExistsForPeriod(context.Context, repository.AlertDimension) (bool, error)
	Create(a *model.BudgetAlertRecord) error
	FindOwnersByBudgetScope(scopeType, scopeID string) ([]repository.AlertOwnerRow, error)
}

// BudgetAlertService 处理告警触发（阈值+去重）与通知发送。
type BudgetAlertService struct {
	budgetRepo budgetAllocReader
	alertRepo  alertRecordStore
	notifier   Notifier
	rdb        *redis.Client
	tierRepo   interface {
		ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error)
		ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error)
	}
	// webhookNotifier 可选：webhook 发送（nil = 仅站内+邮件）
	webhookNotifier WebhookNotifier
}

type notificationWriter interface {
	Create(*model.Notification) error
}

type notificationWriterAdapter struct {
	writer notificationWriter
}

func (a notificationWriterAdapter) Notify(_ context.Context, notification *model.Notification) error {
	return a.writer.Create(notification)
}

// SetRedis 注入 Redis 客户端，用于读取实时已用量。
func (s *BudgetAlertService) SetRedis(rdb *redis.Client) {
	s.rdb = rdb
}

func (s *BudgetAlertService) SetBudgetTierRepo(repo interface {
	ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error)
	ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error)
}) {
	s.tierRepo = repo
}

// SetNotifier 注入通用 Notifier（站内信+邮件等）。
func (s *BudgetAlertService) SetNotifier(n Notifier) {
	s.notifier = n
}

func (s *BudgetAlertService) SetNotificationRepo(writer notificationWriter) {
	if writer == nil {
		s.notifier = nil
		return
	}
	s.notifier = notificationWriterAdapter{writer: writer}
}

// SetWebhookNotifier 注入 Webhook 通知器（由调用方按 settings 动态构建）。
func (s *BudgetAlertService) SetWebhookNotifier(n WebhookNotifier) {
	s.webhookNotifier = n
}

// WebhookNotifier 飞书/webhook 通知接口（由 settings_service 实现）。
type WebhookNotifier interface {
	SendText(ctx context.Context, text string) error
}

func NewBudgetAlertService(budgetRepo *repository.BudgetRepo, alertRepo *repository.BudgetAlertRepo, webhookNotifier WebhookNotifier) *BudgetAlertService {
	return &BudgetAlertService{
		budgetRepo:      budgetRepo,
		alertRepo:       alertRepo,
		webhookNotifier: webhookNotifier,
	}
}

// NewBudgetAlertServiceForTest 供测试注入自定义 fake 实现，不对外暴露接口类型。
func NewBudgetAlertServiceForTest(budgetRepo budgetAllocReader, alertRepo alertRecordStore, webhookNotifier WebhookNotifier) *BudgetAlertService {
	return &BudgetAlertService{
		budgetRepo:      budgetRepo,
		alertRepo:       alertRepo,
		webhookNotifier: webhookNotifier,
	}
}

// EvaluateAndAlert 在核销后评估是否需要触发告警（阈值 80%/100%）。
// 应在 usage_collector 核销完成后异步调用，避免阻塞请求链路。
func (s *BudgetAlertService) EvaluateAndAlert(ctx context.Context, scopeType, scopeID string, details ...string) {
	periodType := budgetdomain.PeriodMonthly
	dimensionType := budgetdomain.DimensionTotal
	modelTierID := ""
	if len(details) >= 3 {
		periodType = budgetdomain.PeriodType(details[0])
		dimensionType = budgetdomain.DimensionType(details[1])
		modelTierID = details[2]
	}
	window, err := budgetperiod.Resolve(time.Now(), periodType, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return
	}
	period := window.Key

	alloc, err := s.budgetRepo.GetAllocation(scopeType, scopeID)
	if err != nil || alloc == nil {
		return
	}
	limitUSD := alloc.AmountUSD
	if periodType == budgetdomain.PeriodDaily {
		limitUSD = alloc.DailyAmountUSD
	} else if periodType == budgetdomain.PeriodWeekly {
		limitUSD = alloc.WeeklyAmountUSD
	}
	// tierName 仅用于文案展示；解析不到时文案退化为「模型等级」，绝不回落到 UUID。
	tierName := ""
	if dimensionType == budgetdomain.DimensionTier {
		limitUSD = -1
		now := time.Now()
		tierID, parseErr := uuid.Parse(modelTierID)
		if parseErr != nil || s.tierRepo == nil {
			return
		}
		limits, listErr := s.tierRepo.ListTierLimits(ctx, alloc.ID)
		if listErr != nil {
			return
		}
		for _, limit := range limits {
			if limit.ModelTierID == tierID && limit.PeriodType == periodType && limit.Enabled &&
				!limit.EffectiveAt.After(now) {
				limitUSD, _ = limit.LimitUSD.Float64()
				tierName = strings.TrimSpace(limit.ModelTier.Name)
				break
			}
		}
		overrides, overrideErr := s.tierRepo.ListActiveOverrides(ctx, alloc.ID, now)
		if overrideErr != nil {
			return
		}
		for _, override := range overrides {
			if override.DimensionType != budgetdomain.DimensionTier || override.ModelTierID == nil ||
				*override.ModelTierID != tierID || override.PeriodType != periodType {
				continue
			}
			target, _ := override.TargetLimitUSD.Float64()
			if target > limitUSD {
				limitUSD = target
			}
		}
	}
	if limitUSD <= 0 {
		return
	}

	usedUSD := s.readDimensionUsedFromRedis(ctx, scopeType, scopeID, period, dimensionType, modelTierID, len(details) == 0)
	ratio := usedUSD / limitUSD

	// 名称解析要多打一次库，因此惰性构建：只有确实要发告警时才解析，
	// 不把额外查询压到 usage_collector 的核销热路径上。
	var copyMemo *budgetAlertCopy
	alertCopy := func() budgetAlertCopy {
		if copyMemo == nil {
			c := s.newBudgetAlertCopy(scopeType, scopeID, periodType, dimensionType, tierName, usedUSD, limitUSD)
			copyMemo = &c
		}
		return *copyMemo
	}

	thresholds := []int{model.AlertThreshold80}
	// 等级维度对所有 scope 同时评估 80%/100%；总预算保留 legacy 规则。
	if dimensionType == budgetdomain.DimensionTier || scopeType == model.BudgetScopeDeptService {
		thresholds = append(thresholds, model.AlertThreshold100)
	}

	for _, threshold := range thresholds {
		target := float64(threshold) / 100.0
		if ratio < target {
			continue
		}
		// 去重：已发过此 period+threshold 的告警则跳过
		exists, err := s.alertRepo.ExistsForPeriod(ctx, repository.AlertDimension{
			ScopeType: scopeType, ScopeID: scopeID, PeriodType: periodType,
			Period: period, Threshold: threshold, DimensionType: dimensionType,
			ModelTierID: func() *uuid.UUID {
				if dimensionType != budgetdomain.DimensionTier {
					return nil
				}
				id, _ := uuid.Parse(modelTierID)
				return &id
			}(),
		})
		if err != nil || exists {
			continue
		}
		// 写去重记录
		if err := s.alertRepo.Create(&model.BudgetAlertRecord{
			ScopeType:     scopeType,
			ScopeID:       scopeID,
			PeriodType:    string(periodType),
			Period:        period,
			Threshold:     threshold,
			DimensionType: dimensionType,
			DimensionKey: func() string {
				if dimensionType == budgetdomain.DimensionTier {
					return "tier:" + modelTierID
				}
				return "total"
			}(),
			ModelTierID: func() *uuid.UUID {
				if dimensionType != budgetdomain.DimensionTier {
					return nil
				}
				id, _ := uuid.Parse(modelTierID)
				return &id
			}(),
			NotifiedAt: time.Now(),
		}); err != nil {
			slog.Error("budget_alert: failed to create alert record", "error", err)
			continue
		}

		alertMsg := alertCopy()

		// 通道 1（解耦的 Notifier：站内信 + 邮件 + ...）
		s.sendNotifications(ctx, scopeType, scopeID, alertMsg, threshold)

		// 通道 2（Webhook 降级：失败不阻断，仅记录）
		if s.webhookNotifier != nil {
			if err := s.webhookNotifier.SendText(ctx, alertMsg.webhookText(threshold)); err != nil {
				slog.Warn("budget_alert: webhook notify failed", "error", err)
			}
		}
		slog.Info("budget_alert: sent", "scope_type", scopeType, "scope_id", scopeID,
			"threshold", threshold, "ratio", ratio)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 预算告警文案
//
// 站内信与告警邮件共用同一份 Title/Content（EmailNotifier 直接透传），所以这里
// 生成的文本必须同时适合通知列表的轻量 Markdown 和纯文本邮件。
// 铁律：面向用户的文案里不得出现 scope_id、UUID，也不得出现
// member / dept_daily / dept_service / studio / tier / total 这类内部枚举名。
// ─────────────────────────────────────────────────────────────────────────────

// scopeNameResolver 是 budgetAllocReader 的可选扩展：批量把 scope_id 解析为可读名称。
// 生产环境的 *repository.BudgetRepo 实现了它；测试 fake 未实现时自动退化为中文类型标签。
type scopeNameResolver interface {
	ResolveScopeNames(scopeType string, scopeIDs []string) (map[string]string, error)
}

// budgetAlertCopy 是一条预算告警中面向用户的全部文案要素。
type budgetAlertCopy struct {
	object    string  // 告警对象，如「X-Lab大模型·成员日常预算」「芮宇潇 的个人额度」
	period    string  // 周期词，如「本月」「本周」「今日」
	dimension string  // 维度后缀，如「 · Claude系列」；总预算维度为空
	usedUSD   float64 // 已用金额
	limitUSD  float64 // 额度上限
}

// newBudgetAlertCopy 组装文案要素。仅在确实要发告警时调用，避免增加核销热路径开销。
func (s *BudgetAlertService) newBudgetAlertCopy(
	scopeType, scopeID string,
	periodType budgetdomain.PeriodType,
	dimensionType budgetdomain.DimensionType,
	tierName string,
	usedUSD, limitUSD float64,
) budgetAlertCopy {
	return budgetAlertCopy{
		object:    s.budgetAlertObjectLabel(scopeType, scopeID),
		period:    budgetAlertPeriodWord(periodType),
		dimension: budgetAlertDimensionSuffix(dimensionType, tierName),
		usedUSD:   usedUSD,
		limitUSD:  limitUSD,
	}
}

// target 返回「对象 + 维度」的完整展示名。
func (c budgetAlertCopy) target() string { return c.object + c.dimension }

// title 生成通知标题，同时作为告警邮件主题（EmailNotifier 会加 "[LLM Gateway] " 前缀）。
// title 生成站内信/邮件标题。额度已用尽时不再播报告警档位：此时正文必然写
// 「额度已用尽（当前 100.0%）」，标题若还说「已达 80%」就自相矛盾。
func (c budgetAlertCopy) title(threshold int) string {
	if c.exhausted() {
		return fmt.Sprintf("预算告警：%s %s额度已用尽", c.target(), c.period)
	}
	return fmt.Sprintf("预算告警：%s %s用量已达 %d%%", c.target(), c.period, threshold)
}

// content 生成通知正文（轻量 Markdown，通知列表直接渲染）。
func (c budgetAlertCopy) content(threshold int) string {
	var b strings.Builder
	if c.exhausted() {
		fmt.Fprintf(&b, "**%s** %s额度已用尽（当前 %s%%），请及时处理。\n\n",
			c.target(), c.period, c.actualPercent())
	} else {
		fmt.Fprintf(&b, "**%s** %s用量已达 **%d%%**%s，请及时关注。\n\n",
			c.target(), c.period, threshold, c.currentHint(threshold))
	}
	fmt.Fprintf(&b, "- 已用：$%.2f USD\n", c.usedUSD)
	fmt.Fprintf(&b, "- 额度上限：$%.2f USD\n", c.limitUSD)
	if remaining := c.limitUSD - c.usedUSD; remaining >= 0 {
		fmt.Fprintf(&b, "- 剩余可用：$%.2f USD\n", remaining)
	} else {
		fmt.Fprintf(&b, "- 超出：$%.2f USD\n", -remaining)
	}
	b.WriteString("\n")
	if c.exhausted() {
		b.WriteString("> 额度用尽后新的请求会被拦截。如需恢复使用，请提交扩容申请。")
	} else {
		b.WriteString("> 告警不等于拦截：额度真正用尽后才会触发拦截。如需提高额度，请提交扩容申请。")
	}
	return b.String()
}

// webhookText 生成飞书等 webhook 的单行纯文本告警。
func (c budgetAlertCopy) webhookText(threshold int) string {
	if c.exhausted() {
		// 同一行里既说「已达 80%」又说「当前 100.0%」会读不通，用尽时只播报用尽事实。
		return fmt.Sprintf("【预算告警】%s %s额度已用尽（已用 $%.2f / 上限 $%.2f USD），请及时处理并考虑扩容。",
			c.target(), c.period, c.usedUSD, c.limitUSD)
	}
	return fmt.Sprintf("【预算告警】%s %s用量已达 %d%%%s（已用 $%.2f / 上限 $%.2f USD），请及时关注并考虑扩容。",
		c.target(), c.period, threshold, c.currentHint(threshold), c.usedUSD, c.limitUSD)
}

// currentHint 只在当前用量明显偏离告警档位时补充「（当前 x%）」。
// 两者一致时省略，避免「已达 80%（当前 80.0%）」这种同义重复。
func (c budgetAlertCopy) currentHint(threshold int) string {
	percent, ok := c.actualPercentValue()
	if !ok || math.Abs(percent-float64(threshold)) < 0.5 {
		return ""
	}
	return "（当前 " + formatAlertPercent(percent) + "%）"
}

// exhausted 表示已用金额已达到或超过额度上限。
func (c budgetAlertCopy) exhausted() bool { return c.usedUSD >= c.limitUSD }

// actualPercentValue 返回当前实际用量百分比；额度上限无效时 ok 为 false。
func (c budgetAlertCopy) actualPercentValue() (float64, bool) {
	if c.limitUSD <= 0 {
		return 0, false
	}
	percent := c.usedUSD / c.limitUSD * 100
	if percent < 0 {
		percent = 0
	}
	return percent, true
}

// actualPercent 返回当前实际用量百分比，例如 "87.5"、"100"。
func (c budgetAlertCopy) actualPercent() string {
	percent, ok := c.actualPercentValue()
	if !ok {
		return "-"
	}
	return formatAlertPercent(percent)
}

// budgetAlertObjectLabel 把 scope 解析为可读对象名，解析不到时退化为中文类型名。
func (s *BudgetAlertService) budgetAlertObjectLabel(scopeType, scopeID string) string {
	return budgetScopeObjectLabel(scopeType, s.resolveScopeName(scopeType, scopeID))
}

// budgetScopeObjectLabel 把预算 scope 渲染成面向用户的对象名（name 为解析出的可读名称）。
// 名称缺失时退化为中文类型名，绝不回落为 scope_id。
// 站内信/邮件/webhook 与扩容通知共用它，避免各处再拼出不一致的对象名。
func budgetScopeObjectLabel(scopeType, name string) string {
	name = strings.TrimSpace(name)
	switch scopeType {
	case model.BudgetScopeCompany:
		return "全公司"
	case model.BudgetScopeStudio:
		if name != "" {
			return name + "·工作室预算"
		}
		return "工作室预算"
	case model.BudgetScopeDeptDaily:
		if name != "" {
			return name + "·成员日常预算"
		}
		return "部门成员日常预算"
	case model.BudgetScopeDeptService:
		if name != "" {
			return name + "·程序服务预算"
		}
		return "部门程序服务预算"
	case model.BudgetScopeMember:
		if name != "" {
			return name + " 的个人额度"
		}
		return "成员个人额度"
	default:
		if name != "" {
			return name
		}
		return "预算"
	}
}

// resolveScopeName 借助 BudgetRepo 的可读名称解析能力取 scope 名称。
// 解析失败或注入的实现不支持该能力时返回空串，由调用方退化为类型标签。
func (s *BudgetAlertService) resolveScopeName(scopeType, scopeID string) string {
	resolver, ok := s.budgetRepo.(scopeNameResolver)
	if !ok || scopeID == "" {
		return ""
	}
	names, err := resolver.ResolveScopeNames(scopeType, []string{scopeID})
	if err != nil {
		slog.Warn("budget_alert: resolve scope name failed", "scope_type", scopeType, "error", err)
		return ""
	}
	return strings.TrimSpace(names[scopeID])
}

// budgetAlertPeriodWord 返回额度周期的中文词。
func budgetAlertPeriodWord(periodType budgetdomain.PeriodType) string {
	switch periodType {
	case budgetdomain.PeriodDaily:
		return "今日"
	case budgetdomain.PeriodWeekly:
		return "本周"
	default:
		return "本月"
	}
}

// budgetAlertDimensionSuffix 返回维度后缀；总预算维度无需额外说明。
func budgetAlertDimensionSuffix(dimension budgetdomain.DimensionType, tierName string) string {
	if dimension != budgetdomain.DimensionTier {
		return ""
	}
	if tierName != "" {
		return " · " + tierName
	}
	return " · 模型等级"
}

// formatAlertPercent 格式化百分比：接近整数时省略小数，否则保留一位小数
// （例如 80 → "80"，100.04 → "100.0"，87.5 → "87.5"）。
func formatAlertPercent(percent float64) string {
	if percent < 0 {
		percent = 0
	}
	rounded := math.Round(percent)
	if math.Abs(percent-rounded) < 0.005 {
		return strconv.FormatFloat(rounded, 'f', -1, 64)
	}
	return strconv.FormatFloat(percent, 'f', 1, 64)
}

// sendNotifications 向每个负责人各发一条 Notification，由注入的 Notifier 决定走哪些通道。
func (s *BudgetAlertService) sendNotifications(ctx context.Context, scopeType, scopeID string, alert budgetAlertCopy, threshold int) {
	if s.notifier == nil {
		return
	}

	level := model.NotificationLevelWarning
	if threshold >= 100 {
		level = model.NotificationLevelCritical
	}

	title := alert.title(threshold)
	content := alert.content(threshold)

	owners, err := s.alertRepo.FindOwnersByBudgetScope(scopeType, scopeID)
	if err != nil {
		slog.Warn("budget_alert: failed to find owners", "error", err)
		return
	}

	sent := 0
	for _, owner := range owners {
		ownerUUID, err := uuid.Parse(owner.UserID)
		if err != nil {
			continue
		}
		n := &model.Notification{
			UserID:  ownerUUID,
			Type:    model.NotificationTypeBudgetAlert,
			Level:   level,
			Title:   title,
			Content: content,
			RefType: "budget_scope",
			RefID:   scopeType + ":" + scopeID,
		}
		if err := s.notifier.Notify(ctx, n); err != nil {
			slog.Warn("budget_alert: notify failed",
				"user_id", owner.UserID, "error", err)
			continue
		}
		sent++
	}
	slog.Info("budget_alert: notifications sent",
		"scope_type", scopeType, "scope_id", scopeID,
		"threshold", threshold, "sent", sent)
}

// readUsedFromRedis 从 Redis 读取已用量（与 budget_service.readUsedFromRedis 同源）。
func (s *BudgetAlertService) readUsedFromRedis(ctx context.Context, scopeType, scopeID, period string) float64 {
	if s.rdb == nil {
		return 0
	}
	key := fmt.Sprintf("budget:used:%s:%s:%s", scopeType, scopeID, period)
	val, err := s.rdb.Get(ctx, key).Float64()
	if err != nil {
		return 0
	}
	return val
}

func (s *BudgetAlertService) readDimensionUsedFromRedis(
	ctx context.Context,
	scopeType, scopeID, period string,
	dimensionType budgetdomain.DimensionType,
	modelTierID string,
	legacy bool,
) float64 {
	if legacy {
		return s.readUsedFromRedis(ctx, scopeType, scopeID, period)
	}
	if s.rdb == nil {
		return 0
	}
	key := fmt.Sprintf("budget:v2:used:%s:%s:%s", scopeType, scopeID, period)
	if dimensionType == budgetdomain.DimensionTier {
		key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s", scopeType, scopeID, modelTierID, period)
	}
	micro, err := s.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0
	}
	return float64(micro) / 1_000_000
}

// ─────────────────────────────────────────────────────────────────────────────
// 扩容审批状态机
// ─────────────────────────────────────────────────────────────────────────────

// ExpansionService 处理扩容申请的状态机与升级链。
type ExpansionService struct {
	repo              *repository.ExpansionRepo
	budgetSvc         *BudgetService
	budgetTierSvc     *BudgetTierService
	legacyQuotaWriter LegacyExpansionQuotaWriter
	approvalPool      ApprovalPoolResolver
	snapshotPublisher BudgetConfigPublisher
	notifier          Notifier
	memberResolver    MemberByDeptResolver
	clock             func() time.Time
}

// MemberByDeptResolver 解析某部门（SSO dept_id）下的成员 ID 列表。
// 生产环境由 *BudgetService 提供；测试可注入纯内存实现，避免依赖 PG 方言。
type MemberByDeptResolver interface {
	ListMemberIDsByDeptID(deptID string) ([]string, error)
}

type LegacyExpansionQuotaWriter interface {
	ApplyLegacyExpansionQuota(context.Context, *model.ExpansionRequest, uuid.UUID) error
}

var (
	ErrInvalidExpansionDimension    = errors.New("invalid expansion dimension")
	ErrDuplicatePendingExpansion    = errors.New("duplicate pending expansion")
	ErrExpansionApprovalUnsupported = errors.New("expansion approval dimension or duration is unsupported before P2")
	ErrExpansionReasonRequired      = errors.New("expansion application reason is required")
	ErrInvalidExpansionPeriod       = errors.New("invalid expansion period")
)

type CreateExpansionRequestInput struct {
	ScopeType             string
	ScopeID               string
	RequesterID           uuid.UUID
	PeriodType            budgetdomain.PeriodType
	DimensionType         budgetdomain.DimensionType
	ModelTierID           *uuid.UUID
	DurationType          budgetdomain.DurationType
	Priority              string
	CurrentAmount         float64
	RequestedAmount       float64
	CurrentAmountSnapshot budgetdomain.Amount
	UsedAmountSnapshot    budgetdomain.Amount
	PrecheckVersion       string
	PrecheckedAt          time.Time
	ExpiresAt             time.Time
	Reason                string
	UrgentReason          string
}

func (s *ExpansionService) CreateRequestAdvanced(ctx context.Context, input CreateExpansionRequestInput) (*model.ExpansionRequest, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrExpansionReasonRequired
	}
	if input.PeriodType == "" {
		input.PeriodType = budgetdomain.PeriodMonthly
	}
	if !input.PeriodType.Valid() {
		return nil, ErrInvalidExpansionPeriod
	}
	if input.DimensionType == "" {
		input.DimensionType = budgetdomain.DimensionTotal
	}
	if input.DurationType == "" {
		input.DurationType = budgetdomain.DurationPermanent
	}
	if !input.DurationType.Valid() {
		return nil, ErrExpansionDurationUnsupported
	}
	if input.Priority == "" {
		input.Priority = "normal"
	}
	if err := budgetdomain.ValidateDimension(input.DimensionType, input.ModelTierID); err != nil {
		return nil, ErrInvalidExpansionDimension
	}
	exists, err := s.repo.ExistsPending(ctx, repository.PendingExpansionKey{
		RequesterID: input.RequesterID, ScopeType: input.ScopeType, ScopeID: input.ScopeID,
		PeriodType: input.PeriodType, DimensionType: input.DimensionType,
		ModelTierID: input.ModelTierID, DurationType: input.DurationType,
	})
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDuplicatePendingExpansion
	}
	currentLevel := model.ExpansionLevelStudio
	if input.ScopeType == model.BudgetScopeMember {
		currentLevel = model.ExpansionLevelDept
	} else if input.ScopeType == model.BudgetScopeStudio {
		currentLevel = model.ExpansionLevelPlatform
	}
	dimensionKey := "total"
	if input.ModelTierID != nil {
		dimensionKey = "tier:" + input.ModelTierID.String()
	}
	request := &model.ExpansionRequest{
		ScopeType: input.ScopeType, ScopeID: input.ScopeID, RequesterID: input.RequesterID,
		PeriodType: string(input.PeriodType), DimensionType: input.DimensionType, DimensionKey: dimensionKey,
		ModelTierID: input.ModelTierID, DurationType: input.DurationType, Priority: input.Priority,
		CurrentAmount: input.CurrentAmount, RequestedAmount: input.RequestedAmount,
		CurrentAmountSnapshot: input.CurrentAmountSnapshot, UsedAmountSnapshot: input.UsedAmountSnapshot,
		PrecheckVersion: input.PrecheckVersion, PrecheckedAt: &input.PrecheckedAt, ExpiresAt: &input.ExpiresAt,
		Reason: input.Reason, UrgentReason: input.UrgentReason,
		Status: model.ExpansionStatusPending, CurrentLevel: currentLevel,
	}
	if err := s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(request).Error; err != nil {
			return err
		}
		if s.approvalPool == nil {
			return nil
		}
		var recipients []uuid.UUID
		var err error
		if resolver, ok := s.approvalPool.(interface {
			ResolveInTx(context.Context, *gorm.DB, *model.ExpansionRequest) ([]uuid.UUID, error)
		}); ok {
			recipients, err = resolver.ResolveInTx(ctx, tx, request)
		} else {
			return fmt.Errorf("approval pool resolver is not transaction-bound")
		}
		if err != nil {
			return err
		}
		for _, recipient := range recipients {
			// member 级申请的申请人即对象本人（对象行首段已含姓名），非 member 级再附申请人行。
			requesterName := ""
			if request.ScopeType != model.BudgetScopeMember {
				requesterName = s.resolveUserName(ctx, request.RequesterID)
			}
			if err := enqueueExpansionNotificationInTx(tx, request, recipient, model.NotificationTypeExpansionSubmitted, "新的扩容审批申请",
				s.expansionNotifyBody(ctx, request, "申请原因："+request.Reason, requesterName, ""), model.NotificationLevelInfo); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		if errors.Is(err, repository.ErrDuplicatePendingExpansion) {
			return nil, ErrDuplicatePendingExpansion
		}
		return nil, fmt.Errorf("create expansion request: %w", err)
	}
	return request, nil
}

func NewExpansionService(repo *repository.ExpansionRepo) *ExpansionService {
	return &ExpansionService{repo: repo, clock: time.Now}
}

// SetBudgetService 注入预算服务（用于查询使用率）。
func (s *ExpansionService) SetBudgetService(bs *BudgetService) {
	s.budgetSvc = bs
	s.legacyQuotaWriter = bs
}

func (s *ExpansionService) SetBudgetTierService(tierSvc *BudgetTierService) {
	s.budgetTierSvc = tierSvc
}

func (s *ExpansionService) WithApprovalPool(resolver ApprovalPoolResolver) *ExpansionService {
	s.approvalPool = resolver
	return s
}

func (s *ExpansionService) WithSnapshotPublisher(publisher BudgetConfigPublisher) *ExpansionService {
	s.snapshotPublisher = publisher
	return s
}

func (s *ExpansionService) WithNotifier(notifier Notifier) *ExpansionService {
	s.notifier = notifier
	return s
}

func (s *ExpansionService) SetLegacyQuotaWriter(writer LegacyExpansionQuotaWriter) {
	s.legacyQuotaWriter = writer
}

// GetBudgetUsedRatio 查询某 scope 当前使用率（供 ListPending 附加信息用）。
func (s *ExpansionService) GetBudgetUsedRatio(ctx context.Context, scopeType, scopeID string) (float64, error) {
	if s.budgetSvc == nil {
		return 0, nil
	}
	item, err := s.budgetSvc.GetBudget(ctx, scopeType, scopeID)
	if err != nil || item == nil {
		return 0, err
	}
	return item.UsedRatio, nil
}

func (s *ExpansionService) ResolveScopeNames(scopeType string, scopeIDs []string) (map[string]string, error) {
	if s.budgetSvc == nil || len(scopeIDs) == 0 {
		return map[string]string{}, nil
	}
	return s.budgetSvc.repo.ResolveScopeNames(scopeType, scopeIDs)
}

func (s *ExpansionService) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	if s.budgetSvc == nil {
		return false, nil
	}
	return s.budgetSvc.MemberInDeptIDs(memberID, deptIDs)
}

// CreateRequest 部门负责人发起扩容申请。
func (s *ExpansionService) CreateRequest(ctx context.Context, scopeType, scopeID string, requesterID uuid.UUID, currentAmount, requestedAmount float64, periodType, reason string) (*model.ExpansionRequest, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrExpansionReasonRequired
	}
	currentLevel := model.ExpansionLevelStudio
	if scopeType == model.BudgetScopeMember {
		currentLevel = model.ExpansionLevelDept
	} else if scopeType == model.BudgetScopeStudio {
		currentLevel = model.ExpansionLevelPlatform
	}
	if periodType == "" {
		periodType = model.ExpansionPeriodMonthly
	}
	if !budgetdomain.PeriodType(periodType).Valid() {
		return nil, ErrInvalidExpansionPeriod
	}
	req := &model.ExpansionRequest{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		RequesterID:     requesterID,
		PeriodType:      periodType,
		CurrentAmount:   currentAmount,
		RequestedAmount: requestedAmount,
		Reason:          reason,
		Status:          model.ExpansionStatusPending,
		CurrentLevel:    currentLevel,
		DimensionType:   budgetdomain.DimensionTotal,
		DimensionKey:    "total",
		DurationType:    budgetdomain.DurationPermanent,
		Priority:        "normal",
	}
	if err := s.repo.Create(req); err != nil {
		if errors.Is(err, repository.ErrDuplicatePendingExpansion) {
			return nil, ErrDuplicatePendingExpansion
		}
		return nil, fmt.Errorf("create expansion request: %w", err)
	}
	return req, nil
}

// Decide 审批人对申请做决策（approve/reject/escalate/cancel）。
// approverID：审批人；action：approve|reject|escalate|cancel；comment：备注。
func (s *ExpansionService) Decide(ctx context.Context, requestID string, approverID uuid.UUID, action, comment string) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return fmt.Errorf("expansion request not found: %w", err)
	}
	if req.Status != model.ExpansionStatusPending {
		return fmt.Errorf("expansion request is not in pending state")
	}

	var newStatus, newLevel string
	switch action {
	case model.ExpansionActionApprove:
		newStatus = model.ExpansionStatusApproved
		newLevel = req.CurrentLevel
	case model.ExpansionActionReject:
		newStatus = model.ExpansionStatusRejected
		newLevel = req.CurrentLevel
	case model.ExpansionActionEscalate:
		// 工作室盘子不足 → 升级到平台管理员
		if req.CurrentLevel != model.ExpansionLevelStudio {
			return fmt.Errorf("can only escalate from studio level")
		}
		newStatus = model.ExpansionStatusPending
		newLevel = model.ExpansionLevelPlatform
	case model.ExpansionActionCancel:
		newStatus = model.ExpansionStatusCancelled
		newLevel = req.CurrentLevel
	default:
		return fmt.Errorf("invalid action: %s", action)
	}

	if action == model.ExpansionActionApprove &&
		(req.DimensionType != budgetdomain.DimensionTotal || req.DurationType != budgetdomain.DurationPermanent) {
		return ErrExpansionApprovalUnsupported
	}

	// 更新申请状态
	if err := s.repo.UpdateStatus(requestID, newStatus, newLevel); err != nil {
		return fmt.Errorf("update expansion status: %w", err)
	}

	// 写审批流水
	_ = s.repo.CreateStep(&model.ExpansionApprovalStep{
		RequestID:  uuid.MustParse(requestID),
		Level:      req.CurrentLevel,
		ApproverID: &approverID,
		Action:     action,
		Comment:    comment,
	})
	if action == model.ExpansionActionApprove && s.legacyQuotaWriter != nil {
		if err := s.legacyQuotaWriter.ApplyLegacyExpansionQuota(ctx, req, approverID); err != nil {
			slog.Warn("expansion: auto-apply quota failed", "request_id", requestID, "error", err)
		}
	}
	return nil
}

// ApplyLegacyExpansionQuota 保留 P0 既有 total+permanent 审批行为。
// tier 与 current_period 必须等待 P2 的原子审批实现。
func (s *BudgetService) ApplyLegacyExpansionQuota(ctx context.Context, req *model.ExpansionRequest, approverID uuid.UUID) error {
	periodType := budgetdomain.PeriodType(req.PeriodType)
	if !periodType.Valid() {
		return ErrInvalidExpansionPeriod
	}
	alloc, err := s.repo.GetAllocation(req.ScopeType, req.ScopeID)
	if err != nil || alloc == nil {
		alloc = &model.BudgetAllocation{
			ScopeType:       req.ScopeType,
			ScopeID:         req.ScopeID,
			AmountUSD:       0,
			DailyAmountUSD:  -1,
			WeeklyAmountUSD: -1,
		}
	}
	monthly, daily, weekly := alloc.AmountUSD, alloc.DailyAmountUSD, alloc.WeeklyAmountUSD
	switch periodType {
	case budgetdomain.PeriodDaily:
		daily = req.RequestedAmount
	case budgetdomain.PeriodWeekly:
		weekly = req.RequestedAmount
	case budgetdomain.PeriodMonthly:
		monthly = req.RequestedAmount
	}
	return s.SetBudget(ctx, req.ScopeType, req.ScopeID, monthly, daily, weekly, approverID)
}

// DecideScoped 按审批人的管辖范围做决策。cancel 仅允许申请人本人取消。
func (s *ExpansionService) DecideScoped(ctx context.Context, requestID string, actorID uuid.UUID, action, comment string, isGlobal bool, deptIDs, studioIDs []string) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return fmt.Errorf("expansion request not found: %w", err)
	}
	if action == model.ExpansionActionCancel {
		if req.RequesterID != actorID {
			return fmt.Errorf("only requester can cancel expansion request")
		}
		return s.Decide(ctx, requestID, actorID, action, comment)
	}
	if !s.canApproveExpansion(req, isGlobal, deptIDs, studioIDs) {
		return fmt.Errorf("无权审批该扩容申请")
	}
	return s.Decide(ctx, requestID, actorID, action, comment)
}

// ListForRequester 查询申请人发起的申请。
func (s *ExpansionService) ListForRequester(ctx context.Context, requesterID string, status string) ([]model.ExpansionRequest, error) {
	return s.repo.ListByRequester(requesterID, status)
}

func (s *ExpansionService) ListForRequesterFiltered(ctx context.Context, requesterID string, filter repository.ExpansionListFilter) ([]model.ExpansionRequest, error) {
	return s.repo.ListByRequesterFiltered(requesterID, filter)
}

// ListPending 查询待某级别审批的申请。
func (s *ExpansionService) ListPending(ctx context.Context, level string) ([]model.ExpansionRequest, error) {
	return s.repo.ListPendingForApprover(level)
}

// ListPendingScoped 查询当前审批人管辖范围内的待审批申请。
// requesterName 非空时按申请人姓名模糊过滤。
func (s *ExpansionService) ListPendingScoped(ctx context.Context, level string, isGlobal bool, deptIDs, studioIDs []string, requesterName string) ([]model.ExpansionRequest, error) {
	return s.ListPendingScopedFiltered(ctx, level, isGlobal, deptIDs, studioIDs, repository.ExpansionListFilter{Keyword: requesterName})
}

func (s *ExpansionService) ListPendingScopedFiltered(ctx context.Context, level string, isGlobal bool, deptIDs, studioIDs []string, filter repository.ExpansionListFilter) ([]model.ExpansionRequest, error) {
	if isGlobal {
		// 全局管理员不受范围限制，但仍支持姓名过滤
		return s.repo.ListPendingForApproverScopedFiltered(level, nil, nil, nil, filter)
	}
	memberIDs, err := s.memberIDsForDeptIDs(deptIDs)
	if err != nil {
		return nil, err
	}
	return s.repo.ListPendingForApproverScopedFiltered(level, deptIDs, studioIDs, memberIDs, filter)
}

// ListPendingForActorScopedFiltered 按 actor 的真实角色集合聚合待审列表。
// 与 ListPendingScopedFiltered 的单 level 语义不同：一个用户可能同时是部门负责人与工作室负责人，
// 需要同时看到 dept 级成员申请与 studio 级（部门/工作室）申请。
//
//   - admin            → platform 级（全部范围）
//   - dept_admin       → dept 级（按 dept_admin 直管部门过滤成员申请）
//   - studio_admin     → studio 级（按管辖部门 + 工作室过滤）
//   - dept+studio 双角色 → dept 级与 studio 级取并集去重
func (s *ExpansionService) ListPendingForActorScopedFiltered(ctx context.Context, actor ActorScope, filter repository.ExpansionListFilter) ([]model.ExpansionRequest, error) {
	// 先按角色展开需要哪些审批级别；无角色信息时回退到兼容行为。
	levels := make([]string, 0, 3)
	hasAdmin := actor.IsGlobal || containsString(actor.Roles, "admin")
	hasDept := containsString(actor.Roles, "dept_admin")
	hasStudio := containsString(actor.Roles, "studio_admin")
	if !hasAdmin && !hasDept && !hasStudio {
		// 兼容旧的单角色 ActorScope：有工作室 => studio，否则 dept。
		if len(actor.StudioIDs) > 0 {
			hasStudio = true
		} else if len(actor.DeptIDs) > 0 {
			hasDept = true
		}
	}
	if hasAdmin {
		levels = append(levels, model.ExpansionLevelPlatform)
	}
	if hasDept {
		levels = append(levels, model.ExpansionLevelDept)
	}
	if hasStudio {
		levels = append(levels, model.ExpansionLevelStudio)
	}
	if len(levels) == 0 {
		return []model.ExpansionRequest{}, nil
	}

	seen := map[string]bool{}
	result := make([]model.ExpansionRequest, 0)
	for _, level := range levels {
		switch level {
		case model.ExpansionLevelPlatform:
			items, err := s.repo.ListPendingForApproverScopedFiltered(level, nil, nil, nil, filter)
			if err != nil {
				return nil, err
			}
			result = appendUniqueRequests(result, items, seen)
		case model.ExpansionLevelDept:
			// dept 级精确限定在 dept_admin 直管部门，避免工作室展开部门越权。
			deptAdminIDs := actor.DeptAdminIDs
			// 兼容旧的 ActorScope 构造方：旧调用方只有 DeptIDs，且没有 Roles/DeptAdminIDs。
			// 只有在明确没有 studio_admin 角色时才回退，避免把工作室展开部门当成 dept_admin 直管范围。
			if len(deptAdminIDs) == 0 && !hasStudio {
				deptAdminIDs = actor.DeptIDs
			}
			memberIDs, err := s.memberIDsForDeptIDs(deptAdminIDs)
			if err != nil {
				return nil, err
			}
			items, err := s.repo.ListPendingForApproverScopedFiltered(level, deptAdminIDs, nil, memberIDs, filter)
			if err != nil {
				return nil, err
			}
			result = appendUniqueRequests(result, items, seen)
		case model.ExpansionLevelStudio:
			items, err := s.repo.ListPendingForApproverScopedFiltered(level, actor.DeptIDs, actor.StudioIDs, nil, filter)
			if err != nil {
				return nil, err
			}
			result = appendUniqueRequests(result, items, seen)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID.String() < result[j].ID.String()
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// ListSteps 查询某扩容申请的审批流水.
func (s *ExpansionService) ListSteps(ctx context.Context, requestID string) ([]model.ExpansionApprovalStep, error) {
	return s.repo.ListSteps(requestID)
}

func (s *ExpansionService) ListStepsScoped(ctx context.Context, actor ActorScope, requestID string) ([]model.ExpansionApprovalStep, error) {
	request, err := s.repo.FindByID(requestID)
	if err != nil {
		return nil, err
	}
	if request.RequesterID.String() != actor.UserID {
		if err := s.validateExpansionRequestScope(ctx, actor, request); err != nil {
			return nil, err
		}
		if actor.Role == "user" {
			return nil, ErrExpansionForbidden
		}
	}
	return s.repo.ListSteps(requestID)
}

func (s *ExpansionService) canApproveExpansion(req *model.ExpansionRequest, isGlobal bool, deptIDs, studioIDs []string) bool {
	if req.CurrentLevel == model.ExpansionLevelDept {
		if isGlobal {
			return true
		}
		if s.budgetSvc == nil {
			return false
		}
		ok, err := s.budgetSvc.MemberInDeptIDs(req.ScopeID, deptIDs)
		return err == nil && ok
	}
	if req.CurrentLevel == model.ExpansionLevelPlatform {
		return isGlobal
	}
	if isGlobal {
		return true
	}
	switch req.ScopeType {
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return containsString(deptIDs, req.ScopeID)
	case model.BudgetScopeStudio:
		return containsString(studioIDs, req.ScopeID)
	default:
		return false
	}
}

func (s *ExpansionService) memberIDsForDeptIDs(deptIDs []string) ([]string, error) {
	if s.memberResolver == nil && s.budgetSvc == nil {
		return nil, nil
	}
	ids := make([]string, 0)
	for _, deptID := range deptIDs {
		memberIDs, err := s.listMemberIDsByDeptID(deptID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, memberIDs...)
	}
	return ids, nil
}

func (s *ExpansionService) listMemberIDsByDeptID(deptID string) ([]string, error) {
	if s.memberResolver != nil {
		return s.memberResolver.ListMemberIDsByDeptID(deptID)
	}
	return s.budgetSvc.ListMemberIDsByDeptID(deptID)
}

// SetMemberResolver 注入成员解析器（用于测试或替换默认查询实现）。
func (s *ExpansionService) SetMemberResolver(resolver MemberByDeptResolver) {
	s.memberResolver = resolver
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// appendUniqueRequests 按 request.ID 去重追加，避免多级别聚合时重复。
func appendUniqueRequests(dst []model.ExpansionRequest, items []model.ExpansionRequest, seen map[string]bool) []model.ExpansionRequest {
	for _, item := range items {
		key := item.ID.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		dst = append(dst, item)
	}
	return dst
}
