package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
)

type TierUsageRepository interface {
	SumTierUsage(context.Context, repository.TierUsageQuery) ([]repository.TierUsageAggregate, error)
	UsageFreshness(context.Context) (time.Time, bool, error)
}

type memberScopeResolver interface {
	MemberInDeptIDs(string, []string) (bool, error)
}

type allocationScopeReader interface {
	GetAllocation(string, string) (*model.BudgetAllocation, error)
}

type TierUsageQueryInput struct {
	ScopeType   string
	ScopeID     string
	PeriodType  budgetdomain.PeriodType
	ModelTierID *uuid.UUID
	GroupID     *uint
}

type TierUsageResponseData struct {
	DataAsOf time.Time                       `json:"data_as_of"`
	Stale    bool                            `json:"stale"`
	Items    []repository.TierUsageAggregate `json:"items"`
}
type TierUsageResponse struct {
	Data TierUsageResponseData `json:"data"`
}

func (s *BudgetTierService) WithTierUsageRepository(repo TierUsageRepository) *BudgetTierService {
	s.usageRepo = repo
	return s
}

func (s *BudgetTierService) QueryTierUsage(ctx context.Context, actor rbac.Actor, input TierUsageQueryInput) (*TierUsageResponse, error) {
	if s.usageRepo == nil {
		return nil, fmt.Errorf("tier usage repository not configured")
	}
	allowed, scopeErr := s.canQueryTierUsage(ctx, actor, input.ScopeType, input.ScopeID)
	if scopeErr != nil {
		return nil, scopeErr
	}
	if !allowed {
		return nil, ErrBudgetForbidden
	}
	period, err := budgetperiod.Resolve(s.clock(), input.PeriodType, s.periodConfig)
	if err != nil {
		return nil, ErrInvalidBudgetPeriod
	}
	items, usageErr := s.usageRepo.SumTierUsage(ctx, repository.TierUsageQuery{
		ScopeType: input.ScopeType, ScopeID: input.ScopeID, TierID: input.ModelTierID,
		GroupID: input.GroupID, StartAt: period.StartAt, EndAt: period.ResetAt,
	})
	if usageErr != nil {
		if errors.Is(usageErr, repository.ErrForbidden) {
			return nil, ErrBudgetForbidden
		}
		return nil, usageErr
	}
	dataAsOf, stale, freshnessErr := s.usageRepo.UsageFreshness(ctx)
	if freshnessErr != nil {
		dataAsOf, stale = s.clock(), true
	}
	return &TierUsageResponse{Data: TierUsageResponseData{DataAsOf: dataAsOf, Stale: stale, Items: items}}, nil
}

type BudgetSourceMode string

const (
	SourceModePersonal BudgetSourceMode = "personal"
	SourceModeShared   BudgetSourceMode = "shared"
	SourceModeMixed    BudgetSourceMode = "mixed"
)

type MyBudgetTiersResponse struct {
	Data MyBudgetTiersData `json:"data"`
}
type MyBudgetTiersData struct {
	SourceSummary  MyBudgetSourceSummary  `json:"source_summary"`
	EffectiveTotal []EffectiveTotalPeriod `json:"effective_total"`
	BudgetChain    []MyBudgetChainScope   `json:"budget_chain"`
	TierLimits     []MyBudgetTierView     `json:"tier_limits"`
	DataUpdatedAt  time.Time              `json:"data_updated_at"`
	Stale          bool                   `json:"stale"`
}
type MyBudgetSourceSummary struct {
	Mode                 BudgetSourceMode `json:"mode"`
	PrimaryScopeType     string           `json:"primary_scope_type"`
	HasSharedPool        bool             `json:"has_shared_pool"`
	HasTemporaryOverride bool             `json:"has_temporary_override"`
}
type EffectiveTotalPeriod struct {
	PeriodType         budgetdomain.PeriodType `json:"period_type"`
	Configured         bool                    `json:"configured"`
	BaseLimitUSD       *budgetdomain.Amount    `json:"base_limit_usd,omitempty"`
	TemporaryTargetUSD *budgetdomain.Amount    `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD  *budgetdomain.Amount    `json:"effective_limit_usd,omitempty"`
	UsedUSD            *budgetdomain.Amount    `json:"used_usd,omitempty"`
	RemainingUSD       *budgetdomain.Amount    `json:"remaining_usd,omitempty"`
	ResetAt            time.Time               `json:"reset_at"`
	Status             string                  `json:"status"`
	LimitingSource     LimitingSourceView      `json:"limiting_source"`
}
type MyBudgetChainScope struct {
	ScopeType      string `json:"scope_type"`
	Label          string `json:"label"`
	VisibleAmounts bool   `json:"visible_amounts"`
}
type MyBudgetTierView struct {
	ModelTierID   uuid.UUID            `json:"model_tier_id"`
	ModelTierName string               `json:"model_tier_name"`
	Enabled       bool                 `json:"enabled"`
	Periods       []MyBudgetTierPeriod `json:"periods"`
}
type MyBudgetTierPeriod struct {
	PeriodType         budgetdomain.PeriodType `json:"period_type"`
	Configured         bool                    `json:"configured"`
	BaseLimitUSD       *budgetdomain.Amount    `json:"base_limit_usd,omitempty"`
	TemporaryTargetUSD *budgetdomain.Amount    `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD  *budgetdomain.Amount    `json:"effective_limit_usd,omitempty"`
	UsedUSD            *budgetdomain.Amount    `json:"used_usd,omitempty"`
	RemainingUSD       *budgetdomain.Amount    `json:"remaining_usd,omitempty"`
	ResetAt            time.Time               `json:"reset_at"`
	Status             string                  `json:"status"`
	LimitingSource     LimitingSourceView      `json:"limiting_source"`
}
type LimitingSourceView struct {
	ScopeType    string               `json:"scope_type"`
	RemainingUSD *budgetdomain.Amount `json:"remaining_usd,omitempty"`
}

func (s *BudgetTierService) canQueryTierUsage(ctx context.Context, actor rbac.Actor, scopeType, scopeID string) (bool, error) {
	if actor.IsGlobal {
		return true, nil
	}
	switch scopeType {
	case "studio":
		return actor.HasStudio(scopeID), nil
	case "dept_daily", "dept_service":
		return actor.HasDept(scopeID), nil
	case "member":
		if len(actor.StudioIDs) > 0 || len(actor.DeptIDs) == 0 {
			return false, nil
		}
		resolver, ok := s.allocations.(memberScopeResolver)
		if !ok {
			return false, nil
		}
		return resolver.MemberInDeptIDs(scopeID, actor.DeptIDs)
	default:
		return false, nil
	}
}

func maxUsageStart(periodStart, effectiveAt time.Time) time.Time {
	if effectiveAt.After(periodStart) {
		return effectiveAt
	}
	return periodStart
}
