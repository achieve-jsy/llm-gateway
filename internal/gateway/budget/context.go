package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

const (
	CtxKeyTierContext                     = "budget_tier_context"
	CtxKeyTierAccountingEligible          = "budget_tier_accounting_eligible"
	CtxKeyAccountingPlan                  = "budget_accounting_plan"
	CtxKeyBlockContext                    = "budget_block_context"
	CtxKeyShadowWouldBlock                = "budget_shadow_would_block"
	CtxKeyCheckResult                     = "budget_check_result"
	CtxKeyBillableReservation             = "budget_billable_reservation"
	ctxKeyReservationAccepted             = "budget_billable_reservation_accepted"
	CtxKeyBlockReason                     = "budget_block_reason"
	CtxKeyShadowMode                      = "budget_shadow_mode"
	CtxKeyAdmissionOutcome                = "budget_admission_outcome"
	CtxKeyAdmissionInfrastructureFailOpen = "budget_admission_infrastructure_fail_open"
)

type AdmissionOutcome string

const (
	AdmissionSkipped                AdmissionOutcome = "skipped"
	AdmissionReserved               AdmissionOutcome = "reserved"
	AdmissionRebuildBlocked         AdmissionOutcome = "rebuild_blocked"
	AdmissionCapacityBlocked        AdmissionOutcome = "capacity_blocked"
	AdmissionInfrastructureFailOpen AdmissionOutcome = "infrastructure_fail_open"
)

func GetAdmissionOutcome(ctx *gin.Context) AdmissionOutcome {
	value, _ := ctx.Get(CtxKeyAdmissionOutcome)
	outcome, _ := value.(AdmissionOutcome)
	return outcome
}

type PendingAccount struct {
	Plan         *AccountingPlan
	CostMicroUSD int64
	OnAccounted  func(context.Context)
}

const CtxKeyPendingAccount = "budget_pending_account"

func SetPendingAccount(ctx *gin.Context, pending *PendingAccount) {
	ctx.Set(CtxKeyPendingAccount, pending)
}

func GetPendingAccount(ctx *gin.Context) *PendingAccount {
	value, _ := ctx.Get(CtxKeyPendingAccount)
	pending, _ := value.(*PendingAccount)
	return pending
}

func SetBillableReservation(ctx *gin.Context, reservation *Reservation) {
	ctx.Set(CtxKeyBillableReservation, reservation)
}

func ReleaseBillableReservation(ctx *gin.Context) error {
	value, ok := ctx.Get(CtxKeyBillableReservation)
	if !ok {
		return nil
	}
	reservation, _ := value.(*Reservation)
	if reservation == nil {
		return nil
	}
	ctx.Set(CtxKeyBillableReservation, nil)
	return reservation.Finish(context.Background())
}

func GetBillableReservation(ctx *gin.Context) *Reservation {
	value, _ := ctx.Get(CtxKeyBillableReservation)
	reservation, _ := value.(*Reservation)
	return reservation
}

func GetAccountingPlanGeneration(ctx *gin.Context) string {
	plan, ok := GetAccountingPlan(ctx)
	if !ok || plan == nil {
		return ""
	}
	return plan.Generation
}

type TierContext struct {
	TierID              string
	TierName            string
	GroupName           string
	SnapshotRevision    int64
	AccountingEligible  bool
	EnforcementEligible bool
}

type Dimension string

const (
	DimensionTotal Dimension = "total"
	DimensionTier  Dimension = "tier"
)

type Constraint struct {
	Dimension     Dimension
	ScopeType     string
	ScopeID       string
	Label         string
	TierID        string
	TierName      string
	PeriodType    budgetdomain.PeriodType
	Period        string
	Key           string
	LimitMicroUSD int64
	UsedMicroUSD  int64
	ResetAt       time.Time
	StaticZero    bool
}

type AccountingPlan struct {
	Generation                      string
	Tier                            *TierContext
	Constraints                     []Constraint
	LegacyKeys                      []string
	LegacyScopeArgs                 []any
	TierAccountingEligible          bool
	TierEnforcementEligible         bool
	RedisCheckSucceeded             bool
	RedisFailOpen                   bool
	AdmissionInfrastructureFailOpen bool
	Snapshot                        *configsync.BudgetTierSnapshot
}

type BlockContext struct {
	Code       string
	Dimension  Dimension
	ScopeType  string
	ScopeID    string
	PeriodType budgetdomain.PeriodType
}

func SetTierContext(ctx *gin.Context, tier TierContext) {
	ctx.Set(CtxKeyTierContext, tier)
}

func GetTierContext(ctx *gin.Context) (TierContext, bool) {
	value, ok := ctx.Get(CtxKeyTierContext)
	if !ok {
		return TierContext{}, false
	}
	tier, ok := value.(TierContext)
	return tier, ok
}

func SetBlockContext(ctx *gin.Context, block BlockContext) {
	ctx.Set(CtxKeyBlockContext, block)
}

func GetBlockContext(ctx *gin.Context) (BlockContext, bool) {
	value, ok := ctx.Get(CtxKeyBlockContext)
	if !ok {
		return BlockContext{}, false
	}
	block, ok := value.(BlockContext)
	return block, ok
}

func SetAccountingPlan(ctx *gin.Context, plan *AccountingPlan) {
	ctx.Set(CtxKeyAccountingPlan, plan)
}

func GetAccountingPlan(ctx *gin.Context) (*AccountingPlan, bool) {
	value, ok := ctx.Get(CtxKeyAccountingPlan)
	if !ok {
		return nil, false
	}
	plan, ok := value.(*AccountingPlan)
	return plan, ok
}

func TotalUsedKey(scopeType, scopeID, period string) string {
	return "budget:v2:used:" + scopeType + ":" + scopeID + ":" + period
}

func TierUsedKey(scopeType, scopeID, tierID, period string) string {
	return "budget:v2:tier:used:" + scopeType + ":" + scopeID + ":" + tierID + ":" + period
}

func BuildAccountingPlan(
	identity *gwmodel.UserIdentity,
	tier TierContext,
	now time.Time,
	snapshot *configsync.BudgetTierSnapshot,
) (*AccountingPlan, error) {
	plan := &AccountingPlan{Snapshot: snapshot}
	if tier.TierID != "" {
		copied := tier
		plan.Tier = &copied
	}
	if identity == nil || snapshot == nil {
		return plan, nil
	}
	periodConfig := budgetperiod.Config{Timezone: snapshot.Timezone, WeekStart: snapshot.WeekStart}
	if err := budgetperiod.ValidateConfig(periodConfig); err != nil {
		return nil, err
	}
	tierBaseAccountingEligible := plan.Tier != nil && tier.AccountingEligible &&
		snapshot.MasterEnabled && snapshot.AccountingEnabled
	tierBaseEnforcementEligible := plan.Tier != nil && tier.EnforcementEligible && snapshot.MasterEnabled
	hasApplicableTierConstraint := false

	for _, scope := range identity.BudgetChain {
		monthly, _ := budgetperiod.Resolve(now, budgetdomain.PeriodMonthly, periodConfig)
		weekly, _ := budgetperiod.Resolve(now, budgetdomain.PeriodWeekly, periodConfig)
		daily, _ := budgetperiod.Resolve(now, budgetdomain.PeriodDaily, periodConfig)
		plan.LegacyKeys = append(plan.LegacyKeys,
			"budget:used:"+scope.ScopeType+":"+scope.ScopeID+":"+monthly.Key,
			"budget:used:"+scope.ScopeType+":"+scope.ScopeID+":"+weekly.Key,
			"budget:used:"+scope.ScopeType+":"+scope.ScopeID+":"+daily.Key,
		)
		plan.LegacyScopeArgs = append(plan.LegacyScopeArgs,
			scope.AmountUSD, scope.WeeklyAmountUSD, scope.DailyAmountUSD, scope.Label,
		)

		totalLimits := []struct {
			periodType budgetdomain.PeriodType
			raw        float64
		}{
			{budgetdomain.PeriodMonthly, scope.AmountUSD},
			{budgetdomain.PeriodWeekly, scope.WeeklyAmountUSD},
			{budgetdomain.PeriodDaily, scope.DailyAmountUSD},
		}
		for _, item := range totalLimits {
			if item.raw < 0 {
				continue
			}
			limit, err := MicroUSDFromFloat64(item.raw)
			if err != nil {
				return nil, fmt.Errorf("convert total budget limit: %w", err)
			}
			window, err := budgetperiod.Resolve(now, item.periodType, periodConfig)
			if err != nil {
				return nil, err
			}
			plan.Constraints = append(plan.Constraints, Constraint{
				Dimension: DimensionTotal, ScopeType: scope.ScopeType, ScopeID: scope.ScopeID,
				Label: scope.Label, PeriodType: item.periodType, Period: window.Key,
				Key:           TotalUsedKey(scope.ScopeType, scope.ScopeID, window.Key),
				LimitMicroUSD: limit, ResetAt: window.ResetAt, StaticZero: limit == 0,
			})
		}

		if plan.Tier == nil || !snapshot.MasterEnabled ||
			!tier.AccountingEligible || !tier.EnforcementEligible {
			continue
		}
		for _, limit := range scope.TierLimits {
			if limit.TierID != tier.TierID || !limit.Enabled || limit.EffectiveAt.After(now) ||
				(limit.ExpiresAt != nil && !limit.ExpiresAt.After(now)) {
				continue
			}
			periodType := budgetdomain.PeriodType(limit.PeriodType)
			window, err := budgetperiod.Resolve(now, periodType, periodConfig)
			if err != nil {
				return nil, fmt.Errorf("resolve %s budget period: %w", limit.PeriodType, err)
			}
			micro, err := ParseMicroUSD(limit.EffectiveLimitUSD)
			if err != nil {
				return nil, fmt.Errorf("parse tier budget limit: %w", err)
			}
			plan.Constraints = append(plan.Constraints, Constraint{
				Dimension: DimensionTier, ScopeType: scope.ScopeType, ScopeID: scope.ScopeID,
				Label: scope.Label, TierID: tier.TierID, TierName: tier.TierName,
				PeriodType: periodType, Period: window.Key,
				Key:           TierUsedKey(scope.ScopeType, scope.ScopeID, tier.TierID, window.Key),
				LimitMicroUSD: micro, ResetAt: window.ResetAt, StaticZero: micro == 0,
			})
			hasApplicableTierConstraint = true
		}
	}
	plan.TierAccountingEligible = tierBaseAccountingEligible && hasApplicableTierConstraint
	plan.TierEnforcementEligible = tierBaseEnforcementEligible && hasApplicableTierConstraint
	return plan, nil
}
