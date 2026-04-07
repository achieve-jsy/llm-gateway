package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type myBudgetReader struct {
	scopes []repository.MyBudgetScopeAllocation
}

func (r myBudgetReader) GetAllocationByID(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return nil, nil
}

func (r myBudgetReader) ResolveMyBudgetChain(context.Context, string) ([]repository.MyBudgetScopeAllocation, error) {
	return r.scopes, nil
}

func (r myBudgetReader) GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error) {
	for index := range r.scopes {
		allocation := &r.scopes[index].Allocation
		if allocation.ScopeType == scopeType && allocation.ScopeID == scopeID {
			return allocation, nil
		}
	}
	return nil, nil
}

func (r myBudgetReader) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	return memberID == "member-a" && len(deptIDs) == 1 && deptIDs[0] == "dept-a", nil
}

type myBudgetStore struct {
	limits    map[uuid.UUID][]model.BudgetTierLimit
	overrides map[uuid.UUID][]model.BudgetTemporaryOverride
}

func (s *myBudgetStore) ListTierLimits(_ context.Context, allocationID uuid.UUID) ([]model.BudgetTierLimit, error) {
	return s.limits[allocationID], nil
}
func (s *myBudgetStore) FindTierLimit(context.Context, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) (*model.BudgetTierLimit, error) {
	return nil, nil
}
func (s *myBudgetStore) UpsertTierLimit(context.Context, repository.UpsertTierLimitParams) (*model.BudgetTierLimit, error) {
	return nil, nil
}
func (s *myBudgetStore) ListActiveOverrides(_ context.Context, allocationID uuid.UUID, _ time.Time) ([]model.BudgetTemporaryOverride, error) {
	return s.overrides[allocationID], nil
}
func (s *myBudgetStore) FindPendingSchedule(context.Context, uuid.UUID, string, budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error) {
	return nil, nil
}
func (s *myBudgetStore) ReplacePendingSchedule(context.Context, *model.BudgetLimitSchedule) (*model.BudgetLimitSchedule, error) {
	return nil, nil
}
func (s *myBudgetStore) PutPendingSchedule(context.Context, *model.BudgetLimitSchedule, bool) (*model.BudgetLimitSchedule, error) {
	return nil, nil
}

type myBudgetUsage struct {
	amount  budgetdomain.Amount
	rows    []repository.TierUsageAggregate
	asOf    time.Time
	stale   bool
	queries *[]repository.TierUsageQuery
}

func (u myBudgetUsage) SumTierUsage(_ context.Context, query repository.TierUsageQuery) ([]repository.TierUsageAggregate, error) {
	if u.queries != nil {
		*u.queries = append(*u.queries, query)
	}
	if u.rows != nil {
		return u.rows, nil
	}
	return []repository.TierUsageAggregate{{AccountingCostUSD: u.amount}}, nil
}
func (u myBudgetUsage) UsageFreshness(context.Context) (time.Time, bool, error) {
	return u.asOf, u.stale, nil
}

func TestMyBudgetTiersHidesAliasesAndKeepsCanonicalEnvelope(t *testing.T) {
	svc := service.NewBudgetTierService(&fakeBudgetTierStore{}, myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{
		{Allocation: model.BudgetAllocation{ScopeType: model.BudgetScopeMember}, Label: "我的额度", VisibleAmounts: true},
		{Allocation: model.BudgetAllocation{ScopeType: model.BudgetScopeDeptDaily}, Label: "部门预算"},
		{Allocation: model.BudgetAllocation{ScopeType: model.BudgetScopeStudio}, Label: "工作室预算"},
		{Allocation: model.BudgetAllocation{ScopeType: model.BudgetScopeCompany}, Label: "公司预算"},
	}}, newFakeModelTierRepo(), func() time.Time {
		return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	})
	got, err := svc.GetMyBudgetTiers(context.Background(), rbac.Actor{UserID: "member-a", StudioIDs: []string{"studio-a"}, DeptIDs: []string{"dept-a"}})
	require.NoError(t, err)
	require.Len(t, got.Data.BudgetChain, 4)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"source_mode"`)
	require.NotContains(t, string(raw), `"tiers"`)
	require.NotContains(t, string(raw), `"data_as_of"`)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	for _, key := range []string{"source_summary", "effective_total", "budget_chain", "tier_limits", "data_updated_at", "stale"} {
		require.Contains(t, envelope.Data, key)
	}
}

func TestMyBudgetTiersComputesRestrictedUpstreamLimitingSource(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	memberAllocationID, studioAllocationID, tierID := uuid.New(), uuid.New(), uuid.New()
	memberLimit := model.BudgetTierLimit{
		BudgetAllocationID: memberAllocationID, ModelTierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveAt: now.Add(-time.Hour),
		ModelTier: model.ModelTier{ID: tierID, Name: "高级", Enabled: true},
	}
	studioLimit := memberLimit
	studioLimit.BudgetAllocationID = studioAllocationID
	studioLimit.LimitUSD = mustAmount(t, "80")
	override := model.BudgetTemporaryOverride{
		BudgetAllocationID: memberAllocationID, DimensionType: budgetdomain.DimensionTier,
		DimensionKey: "tier:" + tierID.String(), ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
		TargetLimitUSD: mustAmount(t, "120"), StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: budgetdomain.OverrideActive,
	}
	reader := myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{
		{Allocation: model.BudgetAllocation{ID: memberAllocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-a", AmountUSD: 300, Enabled: true}, Label: "我的额度", VisibleAmounts: true},
		{Allocation: model.BudgetAllocation{ID: studioAllocationID, ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a", AmountUSD: 200, Enabled: true}, Label: "工作室预算", VisibleAmounts: false},
	}}
	store := &myBudgetStore{
		limits:    map[uuid.UUID][]model.BudgetTierLimit{memberAllocationID: {memberLimit}, studioAllocationID: {studioLimit}},
		overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{memberAllocationID: {override}},
	}
	svc := service.NewBudgetTierService(store, reader, newFakeModelTierRepo(), func() time.Time { return now }).
		WithTierUsageRepository(myBudgetUsage{amount: mustAmount(t, "20"), asOf: now.Add(-time.Minute)})
	got, err := svc.GetMyBudgetTiers(context.Background(), rbac.Actor{UserID: "member-a"})
	require.NoError(t, err)
	require.Equal(t, service.SourceModeMixed, got.Data.SourceSummary.Mode)
	require.True(t, got.Data.SourceSummary.HasTemporaryOverride)
	require.Equal(t, now.Add(-time.Minute), got.Data.DataUpdatedAt)
	require.False(t, got.Data.Stale)
	require.Len(t, got.Data.TierLimits, 1)
	period := got.Data.TierLimits[0].Periods[2]
	require.Equal(t, "restricted", period.Status)
	require.Equal(t, model.BudgetScopeStudio, period.LimitingSource.ScopeType)
	require.Nil(t, period.LimitingSource.RemainingUSD)
	require.Nil(t, period.BaseLimitUSD)
	require.Nil(t, period.EffectiveLimitUSD)
	require.Nil(t, period.UsedUSD)
	require.Nil(t, period.RemainingUSD)
}

func TestMyBudgetTiersIncludesEnabledDisabledAndUnsetPeriods(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	allocationID, enabledID, disabledID := uuid.New(), uuid.New(), uuid.New()
	tiers := newFakeModelTierRepo()
	tiers.tiers[enabledID] = &model.ModelTier{ID: enabledID, Name: "标准", Enabled: true}
	tiers.tiers[disabledID] = &model.ModelTier{ID: disabledID, Name: "停用", Enabled: false}
	store := &myBudgetStore{
		limits: map[uuid.UUID][]model.BudgetTierLimit{allocationID: {
			{
				BudgetAllocationID: allocationID, ModelTierID: enabledID,
				PeriodType: budgetdomain.PeriodWeekly, LimitUSD: mustAmount(t, "50"),
				Enabled: false, EffectiveAt: now.Add(-time.Hour),
				ModelTier: model.ModelTier{ID: enabledID, Name: "标准", Enabled: true},
			},
			{
				BudgetAllocationID: allocationID, ModelTierID: enabledID,
				PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "0"),
				Enabled: true, EffectiveAt: now.Add(-time.Hour),
				ModelTier: model.ModelTier{ID: enabledID, Name: "标准", Enabled: true},
			},
		}},
		overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{},
	}
	reader := myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{{
		Allocation: model.BudgetAllocation{
			ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
			AmountUSD: 300, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
		},
		VisibleAmounts: true,
	}}}
	svc := service.NewBudgetTierService(store, reader, tiers, func() time.Time { return now }).
		WithTierUsageRepository(myBudgetUsage{amount: mustAmount(t, "0")})

	got, err := svc.GetMyBudgetTiers(context.Background(), rbac.Actor{UserID: "member-a"})
	require.NoError(t, err)
	require.Len(t, got.Data.TierLimits, 2)
	byID := map[uuid.UUID]service.MyBudgetTierView{}
	for _, tier := range got.Data.TierLimits {
		byID[tier.ModelTierID] = tier
		require.Len(t, tier.Periods, 3)
	}
	require.True(t, byID[enabledID].Enabled)
	require.Equal(t, "unset", byID[enabledID].Periods[0].Status)
	require.Equal(t, "limit_disabled", byID[enabledID].Periods[1].Status)
	require.Equal(t, "blocked", byID[enabledID].Periods[2].Status)
	require.False(t, byID[disabledID].Enabled)
	for _, period := range byID[disabledID].Periods {
		require.Equal(t, "tier_disabled", period.Status)
	}
}

func TestTierUsageUsesFullPeriodWhileMyBudgetStartsAtLimitEffectiveAt(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	allocationID, tierID := uuid.New(), uuid.New()
	effectiveAt := now.Add(-2 * time.Hour)
	allocation := model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-a", AmountUSD: 300, Enabled: true,
	}
	limit := model.BudgetTierLimit{
		BudgetAllocationID: allocationID, ModelTierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveAt: effectiveAt,
		ModelTier: model.ModelTier{ID: tierID, Name: "高级", Enabled: true},
	}
	reader := myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{{Allocation: allocation, VisibleAmounts: true}}}
	store := &myBudgetStore{
		limits:    map[uuid.UUID][]model.BudgetTierLimit{allocationID: {limit}},
		overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{},
	}
	queries := []repository.TierUsageQuery{}
	svc := service.NewBudgetTierService(store, reader, newFakeModelTierRepo(), func() time.Time { return now }).
		WithTierUsageRepository(myBudgetUsage{amount: mustAmount(t, "20"), queries: &queries})

	_, err := svc.QueryTierUsage(context.Background(), rbac.Actor{DeptIDs: []string{"dept-a"}}, service.TierUsageQueryInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-a", PeriodType: budgetdomain.PeriodMonthly,
		ModelTierID: &tierID,
	})
	require.NoError(t, err)
	require.True(t, queries[0].StartAt.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))))

	queries = nil
	_, err = svc.GetMyBudgetTiers(context.Background(), rbac.Actor{UserID: "member-a"})
	require.NoError(t, err)
	require.Equal(t, effectiveAt, queries[len(queries)-1].StartAt)
}

func TestTierUsageRejectsUnmanagedMember(t *testing.T) {
	svc := service.NewBudgetTierService(&myBudgetStore{}, myBudgetReader{}, newFakeModelTierRepo(), time.Now).
		WithTierUsageRepository(myBudgetUsage{amount: mustAmount(t, "0")})
	_, err := svc.QueryTierUsage(context.Background(), rbac.Actor{DeptIDs: []string{"dept-a"}}, service.TierUsageQueryInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-b", PeriodType: budgetdomain.PeriodMonthly,
	})
	require.ErrorIs(t, err, service.ErrBudgetForbidden)
}

func TestTierUsageIncludesReportedSnapshotsWithoutConfiguredLimitsAndUsesFreshness(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	asOf := now.Add(-15 * time.Minute)
	archivedTierID, disabledTierID := uuid.New(), uuid.New()
	queries := []repository.TierUsageQuery{}
	usage := myBudgetUsage{
		asOf: asOf, stale: true, queries: &queries,
		rows: []repository.TierUsageAggregate{
			{TierID: &archivedTierID, ReportedCostUSD: mustAmount(t, "9"), AccountingCostUSD: mustAmount(t, "0")},
			{TierID: &disabledTierID, ReportedCostUSD: mustAmount(t, "4"), AccountingCostUSD: mustAmount(t, "1")},
		},
	}
	svc := service.NewBudgetTierService(
		&myBudgetStore{},
		myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{{
			Allocation: model.BudgetAllocation{ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal},
		}}},
		newFakeModelTierRepo(),
		func() time.Time { return now },
	).WithTierUsageRepository(usage)

	got, err := svc.QueryTierUsage(context.Background(), rbac.Actor{IsGlobal: true}, service.TierUsageQueryInput{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: budgetdomain.PeriodMonthly,
	})
	require.NoError(t, err)
	require.Len(t, got.Data.Items, 2)
	require.Equal(t, "9.000000", got.Data.Items[0].ReportedCostUSD.String())
	require.Equal(t, "0.000000", got.Data.Items[0].AccountingCostUSD.String())
	require.Equal(t, asOf, got.Data.DataAsOf)
	require.True(t, got.Data.Stale)
	require.Len(t, queries, 1)
	require.Nil(t, queries[0].TierID)
}

func TestMyBudgetTiersSummarizesRestrictedUpstreamTotalWithoutAmounts(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	reader := myBudgetReader{scopes: []repository.MyBudgetScopeAllocation{
		{Allocation: model.BudgetAllocation{ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-a", AmountUSD: 300, Enabled: true}, Label: "我的额度", VisibleAmounts: true},
		{Allocation: model.BudgetAllocation{ID: uuid.New(), ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a", AmountUSD: 10, Enabled: true}, Label: "工作室预算", VisibleAmounts: false},
	}}
	svc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		reader,
		newFakeModelTierRepo(),
		func() time.Time { return now },
	).WithTierUsageRepository(myBudgetUsage{amount: mustAmount(t, "5"), asOf: now})

	got, err := svc.GetMyBudgetTiers(context.Background(), rbac.Actor{UserID: "member-a"})
	require.NoError(t, err)
	monthly := got.Data.EffectiveTotal[2]
	require.Equal(t, "restricted", monthly.Status)
	require.Equal(t, model.BudgetScopeStudio, monthly.LimitingSource.ScopeType)
	require.Nil(t, monthly.LimitingSource.RemainingUSD)
	require.Nil(t, monthly.BaseLimitUSD)
	require.Nil(t, monthly.EffectiveLimitUSD)
	require.Nil(t, monthly.UsedUSD)
	require.Nil(t, monthly.RemainingUSD)
}
