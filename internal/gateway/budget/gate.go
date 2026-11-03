package budget

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type LegacyTotalAdapter interface {
	Check(ctx context.Context, plan *AccountingPlan) (*Constraint, error)
	Add(ctx context.Context, keys []string, scopeArgs []any, costUSD float64) error
}

type LegacyTotalFuncs struct {
	CheckFunc func(context.Context, *AccountingPlan) (*Constraint, error)
	AddFunc   func(context.Context, []string, []any, float64) error
}

func (f LegacyTotalFuncs) Check(ctx context.Context, plan *AccountingPlan) (*Constraint, error) {
	if f.CheckFunc == nil {
		return nil, nil
	}
	return f.CheckFunc(ctx, plan)
}

func (f LegacyTotalFuncs) Add(ctx context.Context, keys []string, scopeArgs []any, costUSD float64) error {
	if f.AddFunc == nil {
		return nil
	}
	return f.AddFunc(ctx, keys, scopeArgs, costUSD)
}

type CheckOutcome struct {
	Plan       *AccountingPlan
	WouldBlock []Constraint
	FailOpen   bool
}

type AccountResult struct {
	V2Error     error
	LegacyError error
}

type PublicConstraint struct {
	Dimension   Dimension               `json:"dimension"`
	PeriodType  budgetdomain.PeriodType `json:"period_type"`
	SourceLevel string                  `json:"source_level"`
}

type ExceededError struct {
	Code       string
	Primary    Constraint
	Additional []PublicConstraint
}

type UnavailableError struct {
	Code       string
	StatusCode int
}

type HTTPError interface {
	error
	WriteHTTPResponse(*gin.Context)
}

func WriteHTTPError(ctx *gin.Context, err error) bool {
	var httpError HTTPError
	if !errors.As(err, &httpError) {
		return false
	}
	httpError.WriteHTTPResponse(ctx)
	return true
}

func (e *UnavailableError) Error() string { return e.Code }

func (e *UnavailableError) WriteHTTPResponse(ctx *gin.Context) {
	ctx.AbortWithStatusJSON(e.StatusCode, gin.H{"error": gin.H{
		"message": e.Code,
		"type":    e.Code,
		"code":    e.Code,
		"gate":    "budget",
	}})
}

func (e *ExceededError) Error() string {
	return e.Code
}

func (e *ExceededError) WriteHTTPResponse(ctx *gin.Context) {
	body := gin.H{
		"error": gin.H{
			"message": ExceededMessage(e.Primary),
			"type":    e.Code,
			"code":    e.Code,
			"gate":    "budget",
		},
	}
	if len(e.Additional) > 0 {
		body["additional_constraints"] = e.Additional
	}
	ctx.AbortWithStatusJSON(http.StatusPaymentRequired, body)
}

type Gate struct {
	holder      *SnapshotHolder
	store       *RedisStore
	legacyTotal LegacyTotalAdapter
	now         func() time.Time
}

func NewGate(
	holder *SnapshotHolder,
	store *RedisStore,
	legacyTotal LegacyTotalAdapter,
	now func() time.Time,
) *Gate {
	if now == nil {
		now = time.Now
	}
	return &Gate{holder: holder, store: store, legacyTotal: legacyTotal, now: now}
}

func (g *Gate) Check(ctx *gin.Context) (*CheckOutcome, error) {
	var identity *gwmodel.UserIdentity
	if value, ok := ctx.Get("identity"); ok {
		identity, _ = value.(*gwmodel.UserIdentity)
	}
	tier, _ := GetTierContext(ctx)
	var snapshot *configsync.BudgetTierSnapshot
	if g.holder != nil {
		snapshot = g.holder.Get()
	}
	snapshotReady := snapshot != nil
	if snapshot == nil {
		snapshot = &configsync.BudgetTierSnapshot{
			SchemaVersion: 1, LegacyTotalWriteEnabled: true,
			Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
		}
	}
	plan, err := BuildAccountingPlan(identity, tier, g.now(), snapshot)
	if err != nil {
		return nil, err
	}
	SetAccountingPlan(ctx, plan)
	ctx.Set(CtxKeyTierAccountingEligible, plan.TierAccountingEligible)
	outcome := &CheckOutcome{Plan: plan}

	actual := staticBlockers(plan, snapshot)
	if len(actual) > 0 {
		for _, constraint := range actual {
			recordCheck(ctx, constraint, "block")
		}
	}
	useLegacyTotalCheck := !snapshotReady || !snapshot.V2TotalEnforcementEnabled
	if g.legacyTotal != nil && useLegacyTotalCheck {
		constraint, legacyErr := g.legacyTotal.Check(ctx.Request.Context(), plan)
		if legacyErr == nil && constraint != nil {
			actual = append(actual, *constraint)
			recordCheck(ctx, *constraint, "block")
		} else if legacyErr == nil {
			for _, total := range plan.Constraints {
				if total.Dimension != DimensionTotal {
					continue
				}
				legacyConstraint := total
				legacyConstraint.Key = ""
				recordCheck(ctx, legacyConstraint, "pass")
			}
		}
		if legacyErr != nil {
			outcome.FailOpen = true
			plan.RedisFailOpen = true
			recordFailOpen(ctx, DimensionTotal, "legacy")
		}
	}
	if len(actual) == 0 {
		admission, err := g.beginReservation(ctx, plan)
		ctx.Set(CtxKeyAdmissionOutcome, admission)
		if err != nil {
			return outcome, err
		}
		if admission == AdmissionInfrastructureFailOpen {
			outcome.FailOpen = true
			plan.RedisFailOpen = true
			plan.AdmissionInfrastructureFailOpen = true
			ctx.Set(CtxKeyAdmissionInfrastructureFailOpen, true)
			recordFailOpen(ctx, DimensionTotal, "admission")
		}
		defer func() {
			if !ctx.GetBool(ctxKeyReservationAccepted) {
				_ = ReleaseBillableReservation(ctx)
			}
		}()
	}
	if !snapshotReady {
		plan.RedisCheckSucceeded = false
		if len(actual) > 0 {
			return outcome, g.reject(ctx, uniqueBlockers(actual))
		}
		ctx.Set(CtxKeyCheckResult, "snapshot_not_ready")
		recordSnapshotNotReady(ctx)
		return g.accept(ctx, outcome)
	}

	checkPlan := planForRedisCheck(plan, snapshot)
	if len(checkPlan.Constraints) == 0 || g.store == nil {
		plan.RedisCheckSucceeded = true
		if len(actual) > 0 {
			return outcome, g.reject(ctx, uniqueBlockers(actual))
		}
		ctx.Set(CtxKeyCheckResult, "pass")
		return g.accept(ctx, outcome)
	}
	result := g.store.Check(ctx.Request.Context(), checkPlan)
	if result.RedisError != nil {
		plan.RedisCheckSucceeded = false
		plan.RedisFailOpen = true
		outcome.FailOpen = true
		for _, constraint := range checkPlan.Constraints {
			recordCheck(ctx, constraint, "error")
			recordFailOpen(ctx, constraint.Dimension, "v2")
		}
		if len(actual) > 0 {
			return outcome, g.reject(ctx, uniqueBlockers(actual))
		}
		ctx.Set(CtxKeyCheckResult, "fail_open")
		return g.accept(ctx, outcome)
	}
	plan.RedisCheckSucceeded = true

	for _, blocker := range result.Blockers {
		recordCheck(ctx, blocker, "block")
		switch blocker.Dimension {
		case DimensionTotal:
			if snapshot.V2TotalEnforcementEnabled {
				actual = append(actual, blocker)
			}
		case DimensionTier:
			if snapshot.ShadowEnabled {
				outcome.WouldBlock = append(outcome.WouldBlock, blocker)
				recordShadowWouldBlock(ctx, blocker)
			} else if snapshot.PositiveEnforcementEnabled {
				actual = append(actual, blocker)
			}
		}
	}
	if len(outcome.WouldBlock) > 0 {
		ctx.Set(CtxKeyShadowWouldBlock, outcome.WouldBlock)
		ctx.Set(CtxKeyShadowMode, true)
		ctx.Set(CtxKeyCheckResult, "shadow_would_block")
	}
	if len(actual) > 0 {
		return outcome, g.reject(ctx, uniqueBlockers(actual))
	}
	if len(outcome.WouldBlock) == 0 {
		ctx.Set(CtxKeyCheckResult, "pass")
	}
	blockedKeys := make(map[string]struct{}, len(result.Blockers))
	for _, blocker := range result.Blockers {
		blockedKeys[blocker.Key] = struct{}{}
	}
	for _, constraint := range checkPlan.Constraints {
		if _, blocked := blockedKeys[constraint.Key]; !blocked {
			recordCheck(ctx, constraint, "pass")
		}
	}
	return g.accept(ctx, outcome)
}

func (g *Gate) accept(ctx *gin.Context, outcome *CheckOutcome) (*CheckOutcome, error) {
	ctx.Set(ctxKeyReservationAccepted, true)
	return outcome, nil
}

func (g *Gate) beginReservation(ctx *gin.Context, plan *AccountingPlan) (AdmissionOutcome, error) {
	if g == nil || g.store == nil {
		return AdmissionSkipped, nil
	}
	requestID, _ := ctx.Get("request_id")
	requestIDString, _ := requestID.(string)
	if requestIDString == "" {
		return AdmissionSkipped, nil
	}
	if keyName, _ := ctx.Get("key_name"); strings.HasPrefix(fmt.Sprint(keyName), "__system_") {
		return AdmissionSkipped, nil
	}
	reservation, err := g.store.ReserveWithGeneration(ctx.Request.Context(), requestIDString, g.now())
	if err != nil {
		switch {
		case errors.Is(err, ErrRebuildInProgress):
			recordAdmissionReject(ctx, "rebuild_in_progress")
			return AdmissionRebuildBlocked, &UnavailableError{Code: "budget_rebuild_in_progress", StatusCode: http.StatusServiceUnavailable}
		case errors.Is(err, ErrSpoolCapacity):
			recordAdmissionReject(ctx, "spool_capacity")
			return AdmissionCapacityBlocked, &UnavailableError{Code: "billing_fact_store_capacity", StatusCode: http.StatusServiceUnavailable}
		default:
			return AdmissionInfrastructureFailOpen, nil
		}
	}
	plan.Generation = reservation.Generation()
	SetBillableReservation(ctx, reservation)
	return AdmissionReserved, nil
}

func uniqueBlockers(blockers []Constraint) []Constraint {
	result := make([]Constraint, 0, len(blockers))
	seen := make(map[string]struct{}, len(blockers))
	for _, blocker := range blockers {
		key := fmt.Sprintf("%s:%s:%s:%s", blocker.Dimension, blocker.ScopeType, blocker.ScopeID, blocker.PeriodType)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, blocker)
	}
	return result
}

func (g *Gate) Account(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) AccountResult {
	var result AccountResult
	if plan == nil || costMicroUSD <= 0 {
		return result
	}
	if plan.RedisCheckSucceeded && !plan.AdmissionInfrastructureFailOpen && g.store != nil {
		writePlan := planForAccounting(plan)
		result.V2Error = g.store.Add(ctx, writePlan, costMicroUSD)
	}
	if plan.Snapshot != nil && plan.Snapshot.LegacyTotalWriteEnabled && g.legacyTotal != nil {
		result.LegacyError = g.legacyTotal.Add(
			ctx, plan.LegacyKeys, plan.LegacyScopeArgs, float64(costMicroUSD)/float64(microsPerUSD),
		)
	}
	return result
}

func staticBlockers(plan *AccountingPlan, snapshot *configsync.BudgetTierSnapshot) []Constraint {
	if snapshot == nil {
		return nil
	}
	var blockers []Constraint
	for _, constraint := range plan.Constraints {
		if !constraint.StaticZero {
			continue
		}
		if constraint.Dimension == DimensionTier && snapshot.ZeroBlockEnabled && plan.TierEnforcementEligible {
			blockers = append(blockers, constraint)
		}
		// $0 是静态禁用，无论总预算检查仍由 legacy 还是已切到 v2，
		// 都必须在内存中 fail-closed，不能依赖 Redis 可用性。
		if constraint.Dimension == DimensionTotal {
			blockers = append(blockers, constraint)
		}
	}
	return blockers
}

func planForRedisCheck(plan *AccountingPlan, snapshot *configsync.BudgetTierSnapshot) *AccountingPlan {
	filtered := *plan
	filtered.Constraints = nil
	for _, constraint := range plan.Constraints {
		if constraint.StaticZero {
			continue
		}
		if constraint.Dimension == DimensionTotal ||
			(constraint.Dimension == DimensionTier && plan.TierAccountingEligible &&
				(snapshot.ShadowEnabled || snapshot.PositiveEnforcementEnabled)) {
			filtered.Constraints = append(filtered.Constraints, constraint)
		}
	}
	return &filtered
}

func planForAccounting(plan *AccountingPlan) *AccountingPlan {
	filtered := *plan
	filtered.Constraints = nil
	for _, constraint := range plan.Constraints {
		if constraint.Dimension == DimensionTotal ||
			(constraint.Dimension == DimensionTier && plan.TierAccountingEligible) {
			filtered.Constraints = append(filtered.Constraints, constraint)
		}
	}
	return &filtered
}

func PlanForAccounting(plan *AccountingPlan) *AccountingPlan {
	if plan == nil {
		return nil
	}
	return planForAccounting(plan)
}

func ShouldEmitThresholdAlerts(plan *AccountingPlan) bool {
	return plan != nil && plan.RedisCheckSucceeded && !plan.RedisFailOpen &&
		!plan.AdmissionInfrastructureFailOpen
}

func (g *Gate) AccountLegacy(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) error {
	if plan == nil || costMicroUSD <= 0 || plan.Snapshot == nil ||
		!plan.Snapshot.LegacyTotalWriteEnabled || g.legacyTotal == nil {
		return nil
	}
	return g.legacyTotal.Add(
		ctx, plan.LegacyKeys, plan.LegacyScopeArgs, float64(costMicroUSD)/float64(microsPerUSD),
	)
}

func (g *Gate) reject(ctx *gin.Context, blockers []Constraint) error {
	sort.SliceStable(blockers, func(i, j int) bool {
		if blockers[i].StaticZero != blockers[j].StaticZero {
			return blockers[i].StaticZero
		}
		if !blockers[i].ResetAt.Equal(blockers[j].ResetAt) {
			return blockers[i].ResetAt.After(blockers[j].ResetAt)
		}
		return fmt.Sprintf("%s:%s:%s:%s", blockers[i].ScopeType, blockers[i].ScopeID, blockers[i].PeriodType, blockers[i].Dimension) <
			fmt.Sprintf("%s:%s:%s:%s", blockers[j].ScopeType, blockers[j].ScopeID, blockers[j].PeriodType, blockers[j].Dimension)
	})
	primary := blockers[0]
	code := "budget_exceeded"
	if primary.Dimension == DimensionTier {
		code = "tier_budget_exceeded"
		recordTierReject(ctx, primary)
	} else {
		recordReject(ctx, primary)
	}
	SetBlockContext(ctx, BlockContext{
		Code: code, Dimension: primary.Dimension, ScopeType: primary.ScopeType,
		ScopeID: primary.ScopeID, PeriodType: primary.PeriodType,
	})
	ctx.Set(CtxKeyCheckResult, "block")
	ctx.Set(CtxKeyBlockReason, code)
	additional := make([]PublicConstraint, 0, len(blockers)-1)
	for _, constraint := range blockers[1:] {
		additional = append(additional, PublicConstraint{
			Dimension: constraint.Dimension, PeriodType: constraint.PeriodType, SourceLevel: constraint.ScopeType,
		})
	}
	return &ExceededError{Code: code, Primary: primary, Additional: additional}
}
