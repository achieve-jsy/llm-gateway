package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

var ErrBudgetUsageUnavailable = errors.New("budget usage unavailable")

type BudgetRiskEvent struct {
	ID                string    `json:"id"`
	RiskType          string    `json:"risk_type"`
	Severity          string    `json:"severity"`
	ScopeType         string    `json:"scope_type"`
	ScopeID           string    `json:"scope_id"`
	ScopeName         string    `json:"scope_name"`
	PeriodType        string    `json:"period_type"`
	DimensionType     string    `json:"dimension_type"`
	ModelTierID       *string   `json:"model_tier_id"`
	ModelTierName     string    `json:"model_tier_name"`
	EffectiveLimitUSD string    `json:"effective_limit_usd"`
	UsedUSD           string    `json:"used_usd"`
	UsageRatio        string    `json:"usage_ratio"`
	LimitSource       string    `json:"limit_source"`
	BlockedRequests   int64     `json:"blocked_requests"`
	DetectedAt        time.Time `json:"detected_at"`
	DataAsOf          time.Time `json:"data_as_of"`
	Stale             bool      `json:"stale"`
}

type RiskFilter struct {
	ScopeType, ScopeID, PeriodType, DimensionType string
	ModelTierID, RiskType, Severity               string
}

type BudgetRiskPage struct {
	Items    []BudgetRiskEvent `json:"items"`
	Total    int               `json:"total"`
	DataAsOf time.Time         `json:"data_as_of"`
	Stale    bool              `json:"stale"`
}

type BudgetRiskSummary struct {
	Total    int            `json:"total"`
	Warning  int            `json:"warning"`
	Critical int            `json:"critical"`
	ByType   map[string]int `json:"by_type"`
	DataAsOf time.Time      `json:"data_as_of"`
	Stale    bool           `json:"stale"`
}

type BudgetRiskService struct {
	blocks    *repository.BudgetRiskRepo
	budgets   *BudgetService
	tiers     *BudgetTierService
	clock     func() time.Time
	cacheMu   sync.RWMutex
	lastKnown map[string]budgetRiskUsage
}

type budgetRiskUsage struct {
	used     budgetdomain.Amount
	dataAsOf time.Time
}

func NewBudgetRiskService(
	blocks *repository.BudgetRiskRepo,
	budgets *BudgetService,
	tiers *BudgetTierService,
	clock func() time.Time,
) *BudgetRiskService {
	if clock == nil {
		clock = time.Now
	}
	return &BudgetRiskService{
		blocks: blocks, budgets: budgets, tiers: tiers, clock: clock,
		lastKnown: make(map[string]budgetRiskUsage),
	}
}

func (s *BudgetRiskService) List(ctx context.Context, actor ActorScope, filter RiskFilter) (*BudgetRiskPage, error) {
	now := s.clock().UTC()
	scopeTypes, scopeIDs, err := s.allowedRiskScopes(actor)
	if err != nil {
		return nil, err
	}
	if filter.ScopeType != "" {
		if !containsString(scopeTypes, filter.ScopeType) {
			return nil, ErrBudgetForbidden
		}
		scopeTypes = []string{filter.ScopeType}
	}
	if filter.ScopeID != "" {
		if !actor.IsGlobal && !containsString(scopeIDs, filter.ScopeID) {
			return nil, ErrBudgetForbidden
		}
		scopeIDs = []string{filter.ScopeID}
	}
	allocations, err := s.budgets.repo.ListScoped(scopeTypes, scopeIDs)
	if err != nil {
		return nil, err
	}
	items := make([]BudgetRiskEvent, 0)
	stale := false
	allocationByScope := make(map[string]model.BudgetAllocation, len(allocations))
	for _, allocation := range allocations {
		allocationByScope[allocation.ScopeType+":"+allocation.ScopeID] = allocation
		projected, itemStale, projectErr := s.projectAllocation(ctx, allocation, filter, now)
		if projectErr != nil {
			if errors.Is(projectErr, ErrBudgetUsageUnavailable) {
				stale = true
				continue
			}
			return nil, projectErr
		}
		stale = stale || itemStale
		items = append(items, projected...)
	}
	periods := []string{"daily", "weekly", "monthly"}
	if filter.PeriodType != "" {
		periods = []string{filter.PeriodType}
	}
	alerts, err := s.blocks.ListAlertRecords(ctx, scopeTypes, scopeIDs, periods)
	if err != nil {
		return nil, err
	}
	activeByIdentity := make(map[string]int, len(items))
	for index := range items {
		activeByIdentity[riskIdentity(items[index])] = index
	}
	for _, alert := range alerts {
		tierID := ""
		if alert.ModelTierID != nil {
			tierID = alert.ModelTierID.String()
		}
		identity := strings.Join([]string{
			alert.ScopeType, alert.ScopeID, alert.PeriodType, string(alert.DimensionType), tierID,
		}, "|")
		if index, ok := activeByIdentity[identity]; ok && alert.NotifiedAt.Before(items[index].DetectedAt) {
			items[index].DetectedAt = alert.NotifiedAt
		}
	}
	for _, periodType := range periods {
		window, resolveErr := budgetperiod.Resolve(now, budgetdomain.PeriodType(periodType), budgetperiod.Config{
			Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
		})
		if resolveErr != nil {
			continue
		}
		blocked, blockErr := s.blocks.AggregateBlockedRequests(ctx, repository.BlockedRequestQuery{
			From: window.StartAt, To: window.ResetAt, ScopeTypes: scopeTypes,
			ScopeIDs: scopeIDs, PeriodTypes: []string{periodType},
		})
		if blockErr != nil {
			return nil, blockErr
		}
		for _, row := range blocked {
			event, eventStale := s.blockedEvent(ctx, row, allocationByScope, now)
			stale = stale || eventStale
			if riskMatches(event, filter) {
				items = append(items, event)
			}
		}
	}
	for _, scopeType := range scopeTypes {
		names, resolveErr := s.budgets.repo.ResolveScopeNames(scopeType, scopeIDs)
		if resolveErr != nil {
			continue
		}
		for index := range items {
			if items[index].ScopeType == scopeType {
				items[index].ScopeName = names[items[index].ScopeID]
				if items[index].ScopeName == "" {
					items[index].ScopeName = items[index].ScopeID
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RiskType == "request_blocked" && items[j].RiskType != "request_blocked" {
			return true
		}
		if items[i].Severity != items[j].Severity {
			return items[i].Severity == "critical"
		}
		return items[i].DetectedAt.After(items[j].DetectedAt)
	})
	dataAsOf := now
	for _, item := range items {
		if item.DataAsOf.Before(dataAsOf) {
			dataAsOf = item.DataAsOf
		}
	}
	return &BudgetRiskPage{Items: items, Total: len(items), DataAsOf: dataAsOf, Stale: stale}, nil
}

func riskIdentity(event BudgetRiskEvent) string {
	return strings.Join([]string{
		event.ScopeType, event.ScopeID, event.PeriodType, event.DimensionType, valueOrEmpty(event.ModelTierID),
	}, "|")
}

func (s *BudgetRiskService) Summary(ctx context.Context, actor ActorScope, filter RiskFilter) (*BudgetRiskSummary, error) {
	page, err := s.List(ctx, actor, filter)
	if err != nil {
		return nil, err
	}
	summary := &BudgetRiskSummary{
		Total: page.Total, ByType: make(map[string]int),
		DataAsOf: page.DataAsOf, Stale: page.Stale,
	}
	for _, item := range page.Items {
		summary.ByType[item.RiskType]++
		if item.Severity == "critical" {
			summary.Critical++
		} else {
			summary.Warning++
		}
	}
	return summary, nil
}

func (s *BudgetRiskService) projectAllocation(
	ctx context.Context,
	allocation model.BudgetAllocation,
	filter RiskFilter,
	now time.Time,
) ([]BudgetRiskEvent, bool, error) {
	periods := []budgetdomain.PeriodType{budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly}
	if filter.PeriodType != "" {
		periods = []budgetdomain.PeriodType{budgetdomain.PeriodType(filter.PeriodType)}
	}
	items := make([]BudgetRiskEvent, 0)
	stale := false
	overrides, err := s.tiers.repo.ListActiveOverrides(ctx, allocation.ID, now)
	if err != nil {
		return nil, stale, err
	}
	if filter.DimensionType == "" || filter.DimensionType == string(budgetdomain.DimensionTotal) {
		for _, period := range periods {
			limit := totalAllocationLimit(allocation, period)
			source := "base_limit"
			for _, override := range overrides {
				if override.DimensionType == budgetdomain.DimensionTotal && override.PeriodType == period &&
					override.TargetLimitUSD.Decimal.GreaterThan(limit) {
					limit = override.TargetLimitUSD.Decimal
					source = "temporary_override"
				}
			}
			if !limit.IsPositive() {
				continue
			}
			event, itemStale, err := s.thresholdEvent(ctx, allocation, period, budgetdomain.DimensionTotal, nil, "", limit, source, now)
			if err != nil {
				return nil, stale, err
			}
			stale = stale || itemStale
			if event != nil && riskMatches(*event, filter) {
				items = append(items, *event)
			}
		}
	}
	if filter.DimensionType == "" || filter.DimensionType == string(budgetdomain.DimensionTier) {
		limits, err := s.tiers.repo.ListTierLimits(ctx, allocation.ID)
		if err != nil {
			return nil, stale, err
		}
		for _, limit := range limits {
			if !limit.Enabled || (filter.PeriodType != "" && string(limit.PeriodType) != filter.PeriodType) ||
				(filter.ModelTierID != "" && limit.ModelTierID.String() != filter.ModelTierID) {
				continue
			}
			effective := limit.LimitUSD.Decimal
			source := "tier_base_limit"
			for _, override := range overrides {
				if override.ModelTierID != nil && *override.ModelTierID == limit.ModelTierID &&
					override.PeriodType == limit.PeriodType && override.TargetLimitUSD.Decimal.GreaterThan(effective) {
					effective = override.TargetLimitUSD.Decimal
					source = "temporary_override"
				}
			}
			if !effective.IsPositive() {
				continue
			}
			tierID := limit.ModelTierID.String()
			event, itemStale, err := s.thresholdEvent(ctx, allocation, limit.PeriodType, budgetdomain.DimensionTier, &tierID, limit.ModelTier.Name, effective, source, now)
			if err != nil {
				return nil, stale, err
			}
			stale = stale || itemStale
			if event != nil && riskMatches(*event, filter) {
				items = append(items, *event)
			}
		}
	}
	return items, stale, nil
}

func (s *BudgetRiskService) thresholdEvent(
	ctx context.Context,
	allocation model.BudgetAllocation,
	period budgetdomain.PeriodType,
	dimension budgetdomain.DimensionType,
	tierID *string,
	tierName string,
	limit decimal.Decimal,
	limitSource string,
	now time.Time,
) (*BudgetRiskEvent, bool, error) {
	window, err := budgetperiod.Resolve(now, period, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return nil, false, err
	}
	key := fmt.Sprintf("budget:v2:used:%s:%s:%s", allocation.ScopeType, allocation.ScopeID, window.Key)
	if dimension == budgetdomain.DimensionTier && tierID != nil {
		key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s", allocation.ScopeType, allocation.ScopeID, *tierID, window.Key)
	}
	usage, stale, err := s.readRiskUsage(ctx, key, now)
	if err != nil {
		return nil, stale, err
	}
	ratio := decimal.Zero
	if limit.IsPositive() {
		ratio = usage.used.Decimal.Div(limit)
	}
	if ratio.LessThan(decimal.NewFromFloat(0.8)) {
		return nil, stale, nil
	}
	riskType, severity := "threshold_reached", "warning"
	if !ratio.LessThan(decimal.NewFromInt(1)) {
		riskType, severity = "budget_exhausted", "critical"
	}
	event := &BudgetRiskEvent{
		ID:       riskEventID(allocation.ScopeType, allocation.ScopeID, string(period), string(dimension), valueOrEmpty(tierID), riskType),
		RiskType: riskType, Severity: severity, ScopeType: allocation.ScopeType, ScopeID: allocation.ScopeID,
		PeriodType: string(period), DimensionType: string(dimension), ModelTierID: tierID, ModelTierName: tierName,
		EffectiveLimitUSD: limit.Round(6).StringFixed(6), UsedUSD: usage.used.String(),
		UsageRatio: ratio.Round(6).StringFixed(6), LimitSource: limitSource,
		DetectedAt: now, DataAsOf: usage.dataAsOf, Stale: stale,
	}
	return event, stale, nil
}

func (s *BudgetRiskService) blockedEvent(
	ctx context.Context,
	row repository.BlockedRequestAggregate,
	allocations map[string]model.BudgetAllocation,
	now time.Time,
) (BudgetRiskEvent, bool) {
	tierID := ""
	var tierPtr *string
	if row.ModelTierID != nil {
		tierID = row.ModelTierID.String()
		tierPtr = &tierID
	}
	keyWindow, _ := budgetperiod.Resolve(now, budgetdomain.PeriodType(row.PeriodType), budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	key := fmt.Sprintf("budget:v2:used:%s:%s:%s", row.ScopeType, row.ScopeID, keyWindow.Key)
	if row.DimensionType == string(budgetdomain.DimensionTier) {
		key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s", row.ScopeType, row.ScopeID, tierID, keyWindow.Key)
	}
	usage, stale, _ := s.readRiskUsage(ctx, key, now)
	limit, source, tierName := decimal.Zero, "block_event", ""
	if allocation, ok := allocations[row.ScopeType+":"+row.ScopeID]; ok {
		limit, source = s.effectiveRiskLimit(ctx, allocation, budgetdomain.PeriodType(row.PeriodType), row.ModelTierID, now)
		if row.ModelTierID != nil {
			if tier, err := s.tiers.tiers.FindByID(ctx, *row.ModelTierID); err == nil && tier != nil {
				tierName = tier.Name
			}
		}
	}
	ratio := decimal.Zero
	if limit.IsPositive() {
		ratio = usage.used.Decimal.Div(limit)
	}
	return BudgetRiskEvent{
		ID:       riskEventID(row.ScopeType, row.ScopeID, row.PeriodType, row.DimensionType, tierID, "request_blocked"),
		RiskType: "request_blocked", Severity: "critical", ScopeType: row.ScopeType, ScopeID: row.ScopeID,
		PeriodType: row.PeriodType, DimensionType: row.DimensionType, ModelTierID: tierPtr, ModelTierName: tierName,
		EffectiveLimitUSD: limit.Round(6).StringFixed(6), UsedUSD: usage.used.String(),
		UsageRatio: ratio.Round(6).StringFixed(6), LimitSource: source,
		BlockedRequests: row.Count, DetectedAt: row.FirstBlockedAt, DataAsOf: usage.dataAsOf, Stale: stale,
	}, stale
}

func (s *BudgetRiskService) effectiveRiskLimit(
	ctx context.Context,
	allocation model.BudgetAllocation,
	period budgetdomain.PeriodType,
	tierID *uuid.UUID,
	now time.Time,
) (decimal.Decimal, string) {
	limit, source := totalAllocationLimit(allocation, period), "base_limit"
	if tierID != nil {
		limit, source = decimal.Zero, "tier_base_limit"
		limits, _ := s.tiers.repo.ListTierLimits(ctx, allocation.ID)
		for _, item := range limits {
			if item.ModelTierID == *tierID && item.PeriodType == period && item.Enabled {
				limit = item.LimitUSD.Decimal
				break
			}
		}
	}
	overrides, _ := s.tiers.repo.ListActiveOverrides(ctx, allocation.ID, now)
	for _, override := range overrides {
		matches := override.PeriodType == period
		if tierID == nil {
			matches = matches && override.DimensionType == budgetdomain.DimensionTotal
		} else {
			matches = matches && override.DimensionType == budgetdomain.DimensionTier &&
				override.ModelTierID != nil && *override.ModelTierID == *tierID
		}
		if matches && override.TargetLimitUSD.Decimal.GreaterThan(limit) {
			limit, source = override.TargetLimitUSD.Decimal, "temporary_override"
		}
	}
	return limit, source
}

func (s *BudgetRiskService) readRiskUsage(ctx context.Context, key string, now time.Time) (budgetRiskUsage, bool, error) {
	if s.budgets.rdb != nil {
		value, err := s.budgets.rdb.Get(ctx, key).Int64()
		if err == nil || errors.Is(err, redis.Nil) {
			usage := budgetRiskUsage{used: budgetdomain.Amount{Decimal: decimal.New(value, -6)}, dataAsOf: now}
			s.cacheMu.Lock()
			s.lastKnown[key] = usage
			s.cacheMu.Unlock()
			return usage, false, nil
		}
	}
	s.cacheMu.RLock()
	usage, ok := s.lastKnown[key]
	s.cacheMu.RUnlock()
	if !ok {
		return budgetRiskUsage{}, true, ErrBudgetUsageUnavailable
	}
	return usage, true, nil
}

func (s *BudgetRiskService) allowedRiskScopes(actor ActorScope) ([]string, []string, error) {
	if actor.IsGlobal || actor.Role == "admin" {
		return []string{model.BudgetScopeCompany, model.BudgetScopeStudio, model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}, nil, nil
	}
	types := riskScopeTypes(actor.Role)
	ids := append([]string{}, actor.StudioIDs...)
	ids = append(ids, actor.DeptIDs...)
	for _, deptID := range actor.DeptIDs {
		memberIDs, err := s.budgets.repo.ListMemberIDsByDeptID(deptID)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, memberIDs...)
	}
	if actor.Role == "user" {
		ids = []string{actor.UserID}
	}
	return types, ids, nil
}

func riskScopeTypes(role string) []string {
	switch role {
	case "studio_admin":
		return []string{model.BudgetScopeStudio, model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
	case "dept_admin":
		return []string{model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
	default:
		return []string{model.BudgetScopeMember}
	}
}

func totalAllocationLimit(allocation model.BudgetAllocation, period budgetdomain.PeriodType) decimal.Decimal {
	switch period {
	case budgetdomain.PeriodDaily:
		return decimal.NewFromFloat(allocation.DailyAmountUSD)
	case budgetdomain.PeriodWeekly:
		return decimal.NewFromFloat(allocation.WeeklyAmountUSD)
	default:
		return decimal.NewFromFloat(allocation.AmountUSD)
	}
}

func riskEventID(scopeType, scopeID, periodType, dimension, tierID, riskType string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s", scopeType, scopeID, periodType, dimension, tierID, riskType)))
	return hex.EncodeToString(sum[:16])
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func riskMatches(event BudgetRiskEvent, filter RiskFilter) bool {
	return (filter.PeriodType == "" || event.PeriodType == filter.PeriodType) &&
		(filter.DimensionType == "" || event.DimensionType == filter.DimensionType) &&
		(filter.ModelTierID == "" || (event.ModelTierID != nil && *event.ModelTierID == filter.ModelTierID)) &&
		(filter.RiskType == "" || event.RiskType == filter.RiskType) &&
		(filter.Severity == "" || event.Severity == filter.Severity)
}
