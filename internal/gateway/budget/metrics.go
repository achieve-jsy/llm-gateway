package budget

import (
	"log/slog"
	"sync"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	budgetMetricsOnce       sync.Once
	checkCounter            metric.Int64Counter
	rejectCounter           metric.Int64Counter
	tierRejectCounter       metric.Int64Counter
	failOpenCounter         metric.Int64Counter
	shadowCounter           metric.Int64Counter
	snapshotNotReadyCounter metric.Int64Counter
	admissionRejectCounter  metric.Int64Counter
)

func ensureBudgetMetrics() {
	budgetMetricsOnce.Do(func() {
		meter := otel.Meter("llm-gateway/budget")
		var err error
		checkCounter, err = meter.Int64Counter("gateway_budget_check_total")
		if err != nil {
			slog.Warn("budget check metric init failed", "error", err)
		}
		tierRejectCounter, _ = meter.Int64Counter("gateway_budget_tier_reject_total")
		rejectCounter, _ = meter.Int64Counter("gateway_budget_reject_total")
		failOpenCounter, _ = meter.Int64Counter("gateway_budget_fail_open_total")
		shadowCounter, _ = meter.Int64Counter("gateway_budget_shadow_would_block_total")
		snapshotNotReadyCounter, _ = meter.Int64Counter("gateway_budget_snapshot_not_ready_total")
		admissionRejectCounter, _ = meter.Int64Counter("gateway_budget_admission_reject_total")
	})
}

func recordAdmissionReject(ctx *gin.Context, reason string) {
	ensureBudgetMetrics()
	if admissionRejectCounter != nil {
		admissionRejectCounter.Add(ctx.Request.Context(), 1, metric.WithAttributes(
			attribute.String("reason", reason),
		))
	}
	slog.WarnContext(ctx.Request.Context(), "budget admission rejected",
		"request_id", ctx.GetString("request_id"),
		"group_name", ctx.GetString("group_name"),
		"tier_id", tierIDFromContext(ctx),
		"reason", reason,
		"result", "reject",
	)
}

func recordCheck(ctx *gin.Context, constraint Constraint, result string) {
	ensureBudgetMetrics()
	path := decisionPath(constraint)
	requestContext := ctx.Request.Context()
	if checkCounter != nil {
		checkCounter.Add(requestContext, 1, metric.WithAttributes(
			attribute.String("dimension", string(constraint.Dimension)),
			attribute.String("result", result),
			attribute.String("period", string(constraint.PeriodType)),
			attribute.String("scope_type", constraint.ScopeType),
		))
	}
	slog.InfoContext(requestContext, "budget decision",
		"request_id", ctx.GetString("request_id"),
		"group_name", ctx.GetString("group_name"),
		"path", path,
		"dimension", constraint.Dimension,
		"scope_type", constraint.ScopeType,
		"scope_id", constraint.ScopeID,
		"tier_id", constraint.TierID,
		"period", constraint.PeriodType,
		"used_micro_usd", constraint.UsedMicroUSD,
		"limit_micro_usd", constraint.LimitMicroUSD,
		"redis_key", constraint.Key,
		"result", result,
		"fail_open", false,
		"shadow_mode", ctx.GetBool(CtxKeyShadowMode),
	)
}

func recordTierReject(ctx *gin.Context, constraint Constraint) {
	ensureBudgetMetrics()
	recordReject(ctx, constraint)
	if tierRejectCounter != nil {
		tierRejectCounter.Add(ctx.Request.Context(), 1, metric.WithAttributes(
			attribute.String("tier_id", constraint.TierID),
			attribute.String("scope_type", constraint.ScopeType),
			attribute.String("period", string(constraint.PeriodType)),
		))
	}
}

func recordReject(ctx *gin.Context, constraint Constraint) {
	ensureBudgetMetrics()
	if rejectCounter != nil {
		rejectCounter.Add(ctx.Request.Context(), 1, metric.WithAttributes(
			attribute.String("path", decisionPath(constraint)),
			attribute.String("dimension", string(constraint.Dimension)),
			attribute.String("scope_type", constraint.ScopeType),
			attribute.String("period", string(constraint.PeriodType)),
		))
	}
}

func recordFailOpen(ctx *gin.Context, dimension Dimension, path string) {
	ensureBudgetMetrics()
	if failOpenCounter != nil {
		failOpenCounter.Add(ctx.Request.Context(), 1, metric.WithAttributes(
			attribute.String("dimension", string(dimension)),
		))
	}
	slog.WarnContext(ctx.Request.Context(), "budget decision",
		"request_id", ctx.GetString("request_id"),
		"group_name", ctx.GetString("group_name"),
		"path", path,
		"dimension", dimension,
		"tier_id", tierIDFromContext(ctx),
		"result", "fail_open",
		"fail_open", true,
		"shadow_mode", ctx.GetBool(CtxKeyShadowMode),
	)
}

func recordShadowWouldBlock(ctx *gin.Context, constraint Constraint) {
	ensureBudgetMetrics()
	if shadowCounter != nil {
		shadowCounter.Add(ctx.Request.Context(), 1, metric.WithAttributes(
			attribute.String("dimension", string(constraint.Dimension)),
			attribute.String("tier_id", constraint.TierID),
		))
	}
}

func recordSnapshotNotReady(ctx *gin.Context) {
	ensureBudgetMetrics()
	if snapshotNotReadyCounter != nil {
		snapshotNotReadyCounter.Add(ctx.Request.Context(), 1)
	}
	slog.WarnContext(ctx.Request.Context(), "budget tier snapshot not ready",
		"request_id", ctx.GetString("request_id"),
		"group_name", ctx.GetString("group_name"),
		"tier_id", tierIDFromContext(ctx),
		"result", "snapshot_not_ready",
	)
}

func tierIDFromContext(ctx *gin.Context) string {
	tier, _ := GetTierContext(ctx)
	return tier.TierID
}

func decisionPath(constraint Constraint) string {
	if constraint.Key == "" {
		return "legacy"
	}
	return "v2"
}
