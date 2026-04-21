package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type myBudgetChainReader interface {
	ResolveMyBudgetChain(context.Context, string) ([]repository.MyBudgetScopeAllocation, error)
}

type tierUsageFreshness interface {
	UsageFreshness(context.Context) (time.Time, bool, error)
}

type budgetConstraint struct {
	scope     repository.MyBudgetScopeAllocation
	period    budgetdomain.PeriodType
	tierID    *uuid.UUID
	tierName  string
	base      budgetdomain.Amount
	override  *model.BudgetTemporaryOverride
	effective budgetdomain.Amount
	used      budgetdomain.Amount
	remaining budgetdomain.Amount
	resetAt   time.Time
}

func (s *BudgetTierService) GetMyBudgetTiers(ctx context.Context, actor rbac.Actor) (*MyBudgetTiersResponse, error) {
	now := s.clock()
	if actor.IsGlobal {
		return emptyMyBudgetTiersResponse(now), nil
	}
	if strings.TrimSpace(actor.UserID) == "" {
		return emptyMyBudgetTiersResponse(now), nil
	}
	reader, ok := s.allocations.(myBudgetChainReader)
	if !ok {
		return nil, fmt.Errorf("my budget chain reader not configured")
	}
	scopes, err := reader.ResolveMyBudgetChain(ctx, actor.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrMyBudgetChainUnavailable) {
			return emptyMyBudgetTiersResponse(now), nil
		}
		return nil, err
	}
	data := MyBudgetTiersData{
		EffectiveTotal: []EffectiveTotalPeriod{}, BudgetChain: []MyBudgetChainScope{},
		TierLimits: []MyBudgetTierView{}, DataUpdatedAt: now,
	}
	hasMember, hasShared, hasAnyEnabled := false, false, false
	totalByPeriod := map[budgetdomain.PeriodType][]budgetConstraint{}
	tierByKey := map[string][]budgetConstraint{}
	disabledTierPeriods := map[string]bool{}
	for _, scope := range scopes {
		data.BudgetChain = append(data.BudgetChain, MyBudgetChainScope{
			ScopeType: scope.Allocation.ScopeType, Label: scope.Label, VisibleAmounts: scope.VisibleAmounts,
		})
		if scope.Allocation.Enabled {
			hasAnyEnabled = true
			if scope.Allocation.ScopeType == model.BudgetScopeMember {
				hasMember = true
			} else {
				hasShared = true
			}
		}
		if !scope.Allocation.Enabled || scope.Allocation.ID == uuid.Nil {
			continue
		}
		overrides, listErr := s.repo.ListActiveOverrides(ctx, scope.Allocation.ID, now)
		if listErr != nil {
			return nil, listErr
		}
		if len(overrides) > 0 {
			data.SourceSummary.HasTemporaryOverride = true
		}
		for _, periodType := range []budgetdomain.PeriodType{budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly} {
			base, configured := allocationPeriodAmount(scope.Allocation, periodType)
			if !configured {
				continue
			}
			constraint, buildErr := s.buildConstraint(ctx, scope, periodType, nil, "", base, overrides, nil)
			if buildErr != nil {
				return nil, buildErr
			}
			totalByPeriod[periodType] = append(totalByPeriod[periodType], constraint)
		}
		limits, listErr := s.repo.ListTierLimits(ctx, scope.Allocation.ID)
		if listErr != nil {
			return nil, listErr
		}
		for index := range limits {
			limit := &limits[index]
			key := limit.ModelTierID.String() + ":" + string(limit.PeriodType)
			if !limit.Enabled {
				disabledTierPeriods[key] = true
				continue
			}
			if limit.EffectiveAt.After(now) {
				continue
			}
			tierID := limit.ModelTierID
			constraint, buildErr := s.buildConstraint(ctx, scope, limit.PeriodType, &tierID, limit.ModelTier.Name, limit.LimitUSD, overrides, &limit.EffectiveAt)
			if buildErr != nil {
				return nil, buildErr
			}
			tierByKey[key] = append(tierByKey[key], constraint)
		}
	}
	data.SourceSummary.HasSharedPool = hasShared
	switch {
	case hasMember && hasShared:
		data.SourceSummary.Mode, data.SourceSummary.PrimaryScopeType = SourceModeMixed, model.BudgetScopeMember
	case hasMember:
		data.SourceSummary.Mode, data.SourceSummary.PrimaryScopeType = SourceModePersonal, model.BudgetScopeMember
	default:
		data.SourceSummary.Mode, data.SourceSummary.PrimaryScopeType = SourceModeShared, model.BudgetScopeDeptDaily
	}
	for _, periodType := range []budgetdomain.PeriodType{budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly} {
		period, resolveErr := budgetperiod.Resolve(now, periodType, s.periodConfig)
		if resolveErr != nil {
			return nil, resolveErr
		}
		view := EffectiveTotalPeriod{PeriodType: periodType, ResetAt: period.ResetAt, Status: "unset"}
		if len(scopes) > 0 && !hasAnyEnabled {
			view.Status = "disabled"
		}
		if limiting := minimumConstraint(totalByPeriod[periodType]); limiting != nil {
			view.Configured = true
			view.LimitingSource.ScopeType = limiting.scope.Allocation.ScopeType
			if limiting.scope.VisibleAmounts {
				base, effective, used, remaining := limiting.base, limiting.effective, limiting.used, limiting.remaining
				view.BaseLimitUSD, view.EffectiveLimitUSD = &base, &effective
				view.UsedUSD, view.RemainingUSD = &used, &remaining
				view.LimitingSource.RemainingUSD = &remaining
				if limiting.override != nil {
					target := limiting.override.TargetLimitUSD
					view.TemporaryTargetUSD = &target
				}
				if effective.Decimal.IsZero() {
					view.Status = "blocked"
				} else if remaining.Decimal.IsZero() {
					view.Status = "exhausted"
				} else {
					view.Status = "available"
				}
			} else {
				view.Status = "restricted"
			}
		}
		data.EffectiveTotal = append(data.EffectiveTotal, view)
	}
	keys := make([]string, 0, len(tierByKey))
	for key := range tierByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tierViews := map[uuid.UUID]*MyBudgetTierView{}
	tierOrder := []uuid.UUID{}
	tiers, err := s.tiers.List(ctx, false)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].SortOrder < tiers[j].SortOrder })
	for _, tier := range tiers {
		periods := make([]MyBudgetTierPeriod, 0, 3)
		for _, periodType := range []budgetdomain.PeriodType{budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly} {
			period, resolveErr := budgetperiod.Resolve(now, periodType, s.periodConfig)
			if resolveErr != nil {
				return nil, resolveErr
			}
			status := "unset"
			if !tier.Enabled {
				status = "tier_disabled"
			} else if disabledTierPeriods[tier.ID.String()+":"+string(periodType)] {
				status = "limit_disabled"
			}
			periods = append(periods, MyBudgetTierPeriod{PeriodType: periodType, ResetAt: period.ResetAt, Status: status})
		}
		tierViews[tier.ID] = &MyBudgetTierView{
			ModelTierID: tier.ID, ModelTierName: tier.Name, ModelTierColor: tier.Color,
			Enabled: tier.Enabled, Periods: periods,
		}
		tierOrder = append(tierOrder, tier.ID)
	}
	for _, key := range keys {
		limiting := minimumConstraint(tierByKey[key])
		if limiting == nil || limiting.tierID == nil {
			continue
		}
		view := tierViews[*limiting.tierID]
		if view == nil {
			view = &MyBudgetTierView{ModelTierID: *limiting.tierID, ModelTierName: limiting.tierName, Enabled: true}
			for _, periodType := range []budgetdomain.PeriodType{budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly} {
				period, _ := budgetperiod.Resolve(now, periodType, s.periodConfig)
				view.Periods = append(view.Periods, MyBudgetTierPeriod{PeriodType: periodType, ResetAt: period.ResetAt, Status: "unset"})
			}
			tierViews[*limiting.tierID] = view
			tierOrder = append(tierOrder, *limiting.tierID)
		}
		periodView := MyBudgetTierPeriod{
			PeriodType: limiting.period, Configured: true, ResetAt: limiting.resetAt,
			LimitingSource: LimitingSourceView{ScopeType: limiting.scope.Allocation.ScopeType},
		}
		if limiting.scope.VisibleAmounts {
			base, effective, used, remaining := limiting.base, limiting.effective, limiting.used, limiting.remaining
			periodView.BaseLimitUSD, periodView.EffectiveLimitUSD = &base, &effective
			periodView.UsedUSD, periodView.RemainingUSD = &used, &remaining
			periodView.LimitingSource.RemainingUSD = &remaining
			if limiting.override != nil {
				target := limiting.override.TargetLimitUSD
				periodView.TemporaryTargetUSD = &target
			}
			if effective.Decimal.IsZero() {
				periodView.Status = "blocked"
			} else if remaining.Decimal.IsZero() {
				periodView.Status = "exhausted"
			} else {
				periodView.Status = "available"
			}
		} else {
			periodView.Status = "restricted"
		}
		for index := range view.Periods {
			if view.Periods[index].PeriodType == periodView.PeriodType {
				if !view.Enabled {
					periodView.Status = "tier_disabled"
				}
				view.Periods[index] = periodView
				break
			}
		}
	}
	for _, tierID := range tierOrder {
		data.TierLimits = append(data.TierLimits, *tierViews[tierID])
	}
	if freshness, ok := s.usageRepo.(tierUsageFreshness); ok {
		updatedAt, stale, freshnessErr := freshness.UsageFreshness(ctx)
		if freshnessErr != nil {
			data.Stale = true
		} else {
			data.DataUpdatedAt, data.Stale = updatedAt, stale
		}
	}
	return &MyBudgetTiersResponse{Data: data}, nil
}

func emptyMyBudgetTiersResponse(now time.Time) *MyBudgetTiersResponse {
	return &MyBudgetTiersResponse{Data: MyBudgetTiersData{
		SourceSummary:  MyBudgetSourceSummary{Mode: SourceModeShared, PrimaryScopeType: model.BudgetScopeDeptDaily},
		EffectiveTotal: []EffectiveTotalPeriod{}, BudgetChain: []MyBudgetChainScope{},
		TierLimits: []MyBudgetTierView{}, DataUpdatedAt: now, Stale: false,
	}}
}

func (s *BudgetTierService) buildConstraint(ctx context.Context, scope repository.MyBudgetScopeAllocation, periodType budgetdomain.PeriodType, tierID *uuid.UUID, tierName string, base budgetdomain.Amount, overrides []model.BudgetTemporaryOverride, effectiveAt *time.Time) (budgetConstraint, error) {
	period, err := budgetperiod.Resolve(s.clock(), periodType, s.periodConfig)
	if err != nil {
		return budgetConstraint{}, err
	}
	effective := base
	var activeOverride *model.BudgetTemporaryOverride
	for index := range overrides {
		override := &overrides[index]
		matches := override.PeriodType == periodType
		if tierID == nil {
			matches = matches && override.DimensionType == budgetdomain.DimensionTotal
		} else {
			matches = matches && override.DimensionType == budgetdomain.DimensionTier &&
				override.ModelTierID != nil && *override.ModelTierID == *tierID
		}
		if matches {
			activeOverride = override
			if override.TargetLimitUSD.Decimal.GreaterThan(effective.Decimal) {
				effective = override.TargetLimitUSD
			}
		}
	}
	startAt := period.StartAt
	if effectiveAt != nil {
		startAt = maxUsageStart(startAt, *effectiveAt)
	}
	used, err := s.accountingUsage(ctx, scope.Allocation, tierID, period, startAt, tierID == nil)
	if err != nil {
		return budgetConstraint{}, err
	}
	remaining := amountFromDecimal(decimal.Max(effective.Decimal.Sub(used.Decimal), decimal.Zero))
	return budgetConstraint{
		scope: scope, period: periodType, tierID: tierID, tierName: tierName, base: base,
		override: activeOverride, effective: effective, used: used, remaining: remaining, resetAt: period.ResetAt,
	}, nil
}

func (s *BudgetTierService) accountingUsage(ctx context.Context, allocation model.BudgetAllocation, tierID *uuid.UUID, period budgetperiod.Period, startAt time.Time, total bool) (budgetdomain.Amount, error) {
	zero := amountFromDecimal(decimal.Zero)
	if s.usageRepo == nil {
		return zero, nil
	}
	rows, err := s.usageRepo.SumTierUsage(ctx, repository.TierUsageQuery{
		ScopeType: allocation.ScopeType, ScopeID: allocation.ScopeID, TierID: tierID,
		StartAt: startAt, EndAt: period.ResetAt, Total: total,
	})
	if err != nil || len(rows) == 0 {
		return zero, err
	}
	return rows[0].AccountingCostUSD, nil
}

func allocationPeriodAmount(allocation model.BudgetAllocation, period budgetdomain.PeriodType) (budgetdomain.Amount, bool) {
	value := float64(-1)
	switch period {
	case budgetdomain.PeriodDaily:
		value = allocation.DailyAmountUSD
	case budgetdomain.PeriodWeekly:
		value = allocation.WeeklyAmountUSD
	case budgetdomain.PeriodMonthly:
		value = allocation.AmountUSD
	}
	if !allocation.Enabled || value < 0 {
		return budgetdomain.Amount{}, false
	}
	amount, err := budgetdomain.ParseAmount(fmt.Sprintf("%.6f", value))
	return amount, err == nil
}

func minimumConstraint(items []budgetConstraint) *budgetConstraint {
	if len(items) == 0 {
		return nil
	}
	limiting := items[0]
	for _, item := range items[1:] {
		if item.remaining.Decimal.LessThan(limiting.remaining.Decimal) {
			limiting = item
		}
	}
	return &limiting
}

func amountFromDecimal(value decimal.Decimal) budgetdomain.Amount {
	amount, _ := budgetdomain.ParseAmount(value.String())
	return amount
}
