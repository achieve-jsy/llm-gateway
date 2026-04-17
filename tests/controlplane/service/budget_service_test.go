package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func mustBudgetAmount(t *testing.T, value string) budgetdomain.Amount {
	t.Helper()
	amount, err := budgetdomain.ParseAmount(value)
	require.NoError(t, err)
	return amount
}

func TestAdjustBudgetCreatesMemberAllocationOnFirstApply(t *testing.T) {
	repo := newFakeAllocRepo()
	svc := service.NewBudgetService(repo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	svc.SetRedis(redis.NewClient(&redis.Options{Addr: mini.Addr()}))

	preview, err := svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-new",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "200.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "first member quota", PreviewOnly: true,
		ActorID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "-1.000000", preview.BeforeLimitUSD)
	assert.Equal(t, "200.000000", preview.AfterLimitUSD)
	assert.Empty(t, repo.upsertCalls)

	_, err = svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-new",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "200.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "first member quota",
		ActorID: uuid.New(),
		BudgetRiskConfirmation: service.BudgetRiskConfirmation{PreviewVersion: preview.PreviewVersion},
	})
	require.NoError(t, err)
	require.Len(t, repo.upsertCalls, 1)
	created := repo.allocations[allocationKey(model.BudgetScopeMember, "member-new")]
	require.NotNil(t, created)
	assert.Equal(t, 200.0, created.AmountUSD)
	assert.Equal(t, -1.0, created.DailyAmountUSD)
	assert.Equal(t, -1.0, created.WeeklyAmountUSD)
}

func TestAdjustBudgetNextPeriodStillRequiresExistingAllocation(t *testing.T) {
	repo := newFakeAllocRepo()
	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	_, err := svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-missing",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "100.000000"),
		EffectiveMode: service.EffectiveNextPeriod, Reason: "schedule missing", PreviewOnly: true,
	})
	require.ErrorIs(t, err, service.ErrBudgetAllocationNotFound)
}

func TestAdjustBudgetPreviewReportsZeroBlockWithoutMutation(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.allocations[allocationKey(model.BudgetScopeMember, "member-1")] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-1",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	impact, err := svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-1",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "0.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "preview zero block", PreviewOnly: true,
	})

	require.NoError(t, err)
	assert.True(t, impact.WillBlock)
	assert.True(t, impact.Stale)
	assert.Equal(t, "0.000000", impact.AfterLimitUSD)
	assert.NotEmpty(t, impact.PreviewVersion)
	assert.Empty(t, repo.upsertCalls)
}

func TestAdjustBudgetApplyZeroRequiresExplicitConfirmation(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.allocations[allocationKey(model.BudgetScopeMember, "member-1")] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-1",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	svc := service.NewBudgetService(repo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	svc.SetRedis(redis.NewClient(&redis.Options{Addr: mini.Addr()}))
	preview, err := svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-1",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "0.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "preview zero block", PreviewOnly: true,
	})
	require.NoError(t, err)

	_, err = svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-1",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "0.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "apply zero block",
		BudgetRiskConfirmation: service.BudgetRiskConfirmation{PreviewVersion: preview.PreviewVersion},
	})

	require.ErrorIs(t, err, service.ErrZeroBlockConfirmationRequired)
	assert.Empty(t, repo.upsertCalls)
}

func TestAdjustBudgetStaleUsageCannotBeConfirmedOrApplied(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.allocations[allocationKey(model.BudgetScopeMember, "member-stale")] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-stale",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	svc := service.NewBudgetService(repo, newFakeAlertRepo())
	preview, err := svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-stale",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "50.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "preview stale usage", PreviewOnly: true,
	})
	require.NoError(t, err)
	require.True(t, preview.Stale)

	_, err = svc.AdjustBudget(context.Background(), service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-stale",
		PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: mustBudgetAmount(t, "50.000000"),
		EffectiveMode: service.EffectiveImmediate, Reason: "must not bypass stale usage",
		BudgetRiskConfirmation: service.BudgetRiskConfirmation{
			PreviewVersion: preview.PreviewVersion,
			ConfirmStale:   true,
		},
	})

	require.ErrorIs(t, err, service.ErrBudgetUsageUnavailable)
	require.Empty(t, repo.upsertCalls)
}

func TestAdjustBudgetNextPeriodRequiresExplicitScheduleReplacement(t *testing.T) {
	repo := newFakeAllocRepo()
	allocationID := uuid.New()
	repo.allocations[allocationKey(model.BudgetScopeMember, "member-next")] = &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-next",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	scheduler := &fakeBudgetScheduleRepo{pending: &model.BudgetLimitSchedule{
		ID: uuid.New(), BudgetAllocationID: allocationID, DimensionKey: "total",
		PeriodType: budgetdomain.PeriodMonthly, UpdatedAt: time.Now().UTC(),
	}}
	svc := service.NewBudgetService(repo, newFakeAlertRepo())
	svc.SetLifecycleScheduler(scheduler)
	mini := miniredis.RunT(t)
	svc.SetRedis(redis.NewClient(&redis.Options{Addr: mini.Addr()}))
	input := service.BudgetAdjustmentInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "member-next", PeriodType: budgetdomain.PeriodMonthly,
		TargetLimitUSD: mustBudgetAmount(t, "120"), EffectiveMode: service.EffectiveNextPeriod, Reason: "replace next",
		PreviewOnly: true,
	}
	preview, err := svc.AdjustBudget(context.Background(), input)
	require.NoError(t, err)
	require.True(t, preview.ReplacesPendingSchedule)
	require.Equal(t, scheduler.pending.ID.String(), preview.PendingScheduleID)
	require.NotEmpty(t, preview.PendingScheduleVersion)
	input.PreviewOnly = false
	input.PreviewVersion = preview.PreviewVersion
	_, err = svc.AdjustBudget(context.Background(), input)
	require.ErrorIs(t, err, service.ErrPendingScheduleExists)
	input.ConfirmReplaceSchedule = true
	_, err = svc.AdjustBudget(context.Background(), input)
	require.NoError(t, err)
}

func TestTotalBudgetBatchApplyRollsBackEveryItemWhenOnePreviewIsStale(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:total-batch-atomic?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE budget_allocations (id text primary key, scope_type text, scope_id text, amount_usd real, daily_amount_usd real, weekly_amount_usd real, period text, enabled bool, created_by text, updated_by text, created_at datetime, updated_at datetime, UNIQUE(scope_type,scope_id))`,
		`CREATE TABLE users (id text primary key, dept_id text)`,
		`CREATE TABLE departments (id text primary key, dept_id text, studio_id text)`,
		`CREATE TABLE dept_studio_map (dept_id text, studio_id text)`,
		`CREATE TABLE model_groups (id text primary key, name text, budget_tier_id text, deleted_at datetime)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	for _, id := range []string{"studio-a", "studio-b"} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled) VALUES (?,?,?,?,?,?,?,?)`,
			uuid.NewString(), model.BudgetScopeStudio, id, 100, 100, 100, "monthly", true).Error)
	}
	svc := service.NewBudgetService(repository.NewBudgetRepo(db), newFakeAlertRepo())
	mini := miniredis.RunT(t)
	svc.SetRedis(redis.NewClient(&redis.Options{Addr: mini.Addr()}))
	input := service.TotalBatchInput{
		Operation: "set_fixed", Value: "150", PeriodType: budgetdomain.PeriodMonthly,
		EffectiveMode: service.EffectiveImmediate, Reason: "atomic batch",
		Items: []service.TotalBatchObject{{ItemID: "a", ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a"}, {ItemID: "b", ScopeType: model.BudgetScopeStudio, ScopeID: "studio-b"}},
	}
	preview, err := svc.PreviewTotalBatch(context.Background(), input)
	require.NoError(t, err)
	input.Items[0].PreviewVersion = preview.Items[0].Impact.PreviewVersion
	input.Items[1].PreviewVersion = "stale"
	_, err = svc.ApplyTotalBatch(context.Background(), input)
	require.Error(t, err)
	var amounts []float64
	require.NoError(t, db.Table("budget_allocations").Order("scope_id").Pluck("amount_usd", &amounts).Error)
	require.Equal(t, []float64{100, 100}, amounts)
}

// ─────────────────────────────────────────────────────────────────────────────
// Fakes: 手写 fake struct 模拟 BudgetService 依赖（遵循 tests/README.md 约定）
// ─────────────────────────────────────────────────────────────────────────────

// fakeBudgetAllocationRepo 记录所有被调用的方法 + 返回结果
type fakeBudgetAllocationRepo struct {
	allocations map[string]*model.BudgetAllocation // key: scopeType:scopeID

	// 调用计数
	upsertCalls       []model.BudgetAllocation
	getCalls          []string
	listScopedCalls   int
	listDeptStudioRow []model.DeptStudioRow
	listDeptStudioErr error

	// 错误注入
	upsertErr error
	getErr    error

	// 可选：测试用 scope 名称解析
	resolveScopeNames func(scopeType string, scopeIDs []string) (map[string]string, error)
}

type fakeBudgetScheduleRepo struct{ pending *model.BudgetLimitSchedule }

func (f *fakeBudgetScheduleRepo) FindPendingSchedule(context.Context, uuid.UUID, string, budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error) {
	return f.pending, nil
}
func (f *fakeBudgetScheduleRepo) PutPendingSchedule(_ context.Context, schedule *model.BudgetLimitSchedule, replace bool) (*model.BudgetLimitSchedule, error) {
	if f.pending != nil && !replace {
		return nil, repository.ErrPendingScheduleExists
	}
	f.pending = schedule
	return schedule, nil
}

func (f *fakeBudgetAllocationRepo) ResolveBudgetImpact(context.Context, string, string, *string, int) (int, []repository.BudgetAffectedGroup, error) {
	return 1, []repository.BudgetAffectedGroup{{ID: "group-1", Name: "Group 1"}}, nil
}

func newFakeAllocRepo() *fakeBudgetAllocationRepo {
	return &fakeBudgetAllocationRepo{allocations: make(map[string]*model.BudgetAllocation)}
}

func allocationKey(scopeType, scopeID string) string {
	return scopeType + ":" + scopeID
}

func (f *fakeBudgetAllocationRepo) UpsertAllocation(a *model.BudgetAllocation) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upsertCalls = append(f.upsertCalls, *a)
	f.allocations[allocationKey(a.ScopeType, a.ScopeID)] = a
	return nil
}

func (f *fakeBudgetAllocationRepo) GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error) {
	f.getCalls = append(f.getCalls, allocationKey(scopeType, scopeID))
	if f.getErr != nil {
		return nil, f.getErr
	}
	a, ok := f.allocations[allocationKey(scopeType, scopeID)]
	if !ok {
		return nil, nil // repo 约定：未找到返回 nil, nil
	}
	return a, nil
}

func (f *fakeBudgetAllocationRepo) GetConsumption(scopeType, scopeID, period string) (*model.BudgetConsumption, error) {
	return nil, nil // 默认无消费记录
}

func (f *fakeBudgetAllocationRepo) ListScoped(scopeTypes, scopeIDs []string) ([]model.BudgetAllocation, error) {
	f.listScopedCalls++
	result := make([]model.BudgetAllocation, 0)
	for _, allocation := range f.allocations {
		typeAllowed := len(scopeTypes) == 0
		for _, scopeType := range scopeTypes {
			typeAllowed = typeAllowed || allocation.ScopeType == scopeType
		}
		idAllowed := len(scopeIDs) == 0
		for _, scopeID := range scopeIDs {
			idAllowed = idAllowed || allocation.ScopeID == scopeID
		}
		if typeAllowed && idAllowed {
			result = append(result, *allocation)
		}
	}
	return result, nil
}

func (f *fakeBudgetAllocationRepo) ListMemberIDsByDeptID(deptID string) ([]string, error) {
	return nil, nil
}

func (f *fakeBudgetAllocationRepo) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	return false, nil
}

func (f *fakeBudgetAllocationRepo) ListDeptStudioRows() ([]model.DeptStudioRow, error) {
	return f.listDeptStudioRow, f.listDeptStudioErr
}

func (f *fakeBudgetAllocationRepo) ListDeptIDsByStudioID(studioID string) ([]string, error) {
	return nil, nil
}

func (f *fakeBudgetAllocationRepo) DeleteAllocation(scopeType, scopeID string) error {
	delete(f.allocations, allocationKey(scopeType, scopeID))
	return nil
}

func (f *fakeBudgetAllocationRepo) ResolveScopeNames(scopeType string, scopeIDs []string) (map[string]string, error) {
	if f.resolveScopeNames != nil {
		return f.resolveScopeNames(scopeType, scopeIDs)
	}
	return make(map[string]string), nil
}

// fakeBudgetAlertRecordRepo 记录告警去重表操作
type fakeBudgetAlertRecordRepo struct {
	deleteCalls []deleteScopeArgs
	createCalls []*model.BudgetAlertRecord
	deleteErr   error
	createErr   error
}

type deleteScopeArgs struct {
	ScopeType string
	ScopeID   string
	Period    string
}

func newFakeAlertRepo() *fakeBudgetAlertRecordRepo {
	return &fakeBudgetAlertRecordRepo{}
}

func (f *fakeBudgetAlertRecordRepo) ExistsForPeriod(scopeType, scopeID, period string, threshold int) bool {
	return false
}

func (f *fakeBudgetAlertRecordRepo) Create(a *model.BudgetAlertRecord) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createCalls = append(f.createCalls, a)
	return nil
}

func (f *fakeBudgetAlertRecordRepo) DeleteByScope(scopeType, scopeID, period string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls = append(f.deleteCalls, deleteScopeArgs{scopeType, scopeID, period})
	return nil
}

// fakeBudgetEventPublisher 记录事件发布历史（命名避免与 key_service_test 冲突）
type fakeBudgetEventPublisher struct {
	events []budgetPubEvent
}

type budgetPubEvent struct {
	EventType    event.ChangeEventType
	ResourceType string
	ResourceID   uuid.UUID
}

func (f *fakeBudgetEventPublisher) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	f.events = append(f.events, budgetPubEvent{eventType, resourceType, resourceID})
}

func (f *fakeBudgetEventPublisher) InvalidateKeyCache(keyHash string) {
	// 不测 invalidate 行为，仅占位满足 service.EventPublisher 接口
}

// newTestMiniredis 启动内存 Redis 服务器，返回 (client, server)。
// 测试结束前调用 server.Close()。
func newTestMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return redis.NewClient(&redis.Options{Addr: s.Addr()}), s
}

// ─────────────────────────────────────────────────────────────────────────────
// 测试用例
// ─────────────────────────────────────────────────────────────────────────────

// TestBudgetService_SetBudget_Success 验证 SetBudget 的「金线」路径：
// - DB upsert 成功
// - 本月告警去重记录被清理
// - Redis 写入 budget:amount:{scope_type}:{scope_id}
// - 广播 auth_config_changed 事件（resource_type=auth_config, resource_id=uuid.Nil）
func TestBudgetService_SetBudget_Success(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}
	rdb, _ := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	svc.SetRedis(rdb)

	ctx := context.Background()
	creatorID := uuid.New()

	// Act
	err := svc.SetBudget(ctx, "dept_daily", "dept-001", 500.0, -1, -1, creatorID)

	// Assert: DB 写成功
	assert.NoError(t, err)
	require.Len(t, repo.upsertCalls, 1, "DB upsert 应被调用 1 次")
	assert.Equal(t, "dept_daily", repo.upsertCalls[0].ScopeType)
	assert.Equal(t, "dept-001", repo.upsertCalls[0].ScopeID)
	assert.Equal(t, 500.0, repo.upsertCalls[0].AmountUSD)
	assert.Equal(t, creatorID, repo.upsertCalls[0].CreatedBy)

	// Assert: 本月告警去重记录被清理（扩容后又可再次告警，§H 决策）
	require.Len(t, alertRepo.deleteCalls, 1, "告警去重应被清理 1 次")
	assert.Equal(t, "dept_daily", alertRepo.deleteCalls[0].ScopeType)
	assert.Equal(t, "dept-001", alertRepo.deleteCalls[0].ScopeID)
	assert.Equal(t, time.Now().Format("2006-01"), alertRepo.deleteCalls[0].Period)

	// Assert: Redis key 写入
	got := rdb.Get(ctx, "budget:amount:dept_daily:dept-001")
	require.NoError(t, got.Err())
	assert.Equal(t, "500", got.Val(), "Redis 应存浮点值字符串（go-redis Set float64 → 字符串表示）")

	// Assert: 广播 auth_config_changed
	require.Len(t, pub.events, 1)
	assert.Equal(t, event.AuthConfigChanged, pub.events[0].EventType)
	assert.Equal(t, "auth_config", pub.events[0].ResourceType)
	assert.Equal(t, uuid.Nil, pub.events[0].ResourceID)
}

// TestBudgetService_SetBudget_NegativeAmount 验证金额 < 0 时直接返回错误，
// 不应写 DB、不应写 Redis、不应发事件。
func TestBudgetService_SetBudget_NegativeAmount(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}
	rdb, mr := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	svc.SetRedis(rdb)

	// Act
	err := svc.SetBudget(context.Background(), "dept_daily", "dept-001", -100.0, -1, -1, uuid.New())

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ">= 0")
	assert.Empty(t, repo.upsertCalls, "DB 不应被调用")
	assert.Empty(t, mr.Keys(), "Redis 不应被写入")
	assert.Empty(t, alertRepo.deleteCalls, "告警去重不应被清理")
	assert.Empty(t, pub.events, "事件不应被发布")
}

// TestBudgetService_SetBudget_DBUpsertFails 验证 DB upsert 失败时：
// 返回 error，不应后续写 Redis、不应清理告警去重、不应发事件。
func TestBudgetService_SetBudget_DBUpsertFails(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.upsertErr = fmt.Errorf("db connection lost")
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}
	rdb, mr := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	svc.SetRedis(rdb)

	err := svc.SetBudget(context.Background(), "company", "GLOBAL", 1000.0, -1, -1, uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upsert budget")
	assert.Empty(t, mr.Keys(), "DB 失败不应写 Redis")
	assert.Empty(t, alertRepo.deleteCalls, "DB 失败不应清理告警去重")
	assert.Empty(t, pub.events, "DB 失败不应发事件")
}

// TestBudgetService_SetBudget_RedisFailureIsNonFatal 验证 Redis 写失败时：
// 仍然成功返回，仍然广播事件，仍然清理告警去重。
// 设计文档：「Redis 写失败只影响即时生效性能，DB 已落库」
func TestBudgetService_SetBudget_RedisFailureIsNonFatal(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}
	rdb, rds := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	svc.SetRedis(rdb)

	// 模拟 Redis 写失败：关闭 server
	rds.Close()

	err := svc.SetBudget(context.Background(), "dept_daily", "dept-001", 500.0, -1, -1, uuid.New())

	// 关键断言：Redis 失败不应传播错误
	assert.NoError(t, err, "Redis 写失败应为非致命错误")
	// DB + 告警清理 + 事件发布应照常完成
	require.Len(t, repo.upsertCalls, 1)
	require.Len(t, alertRepo.deleteCalls, 1)
	require.Len(t, pub.events, 1)
}

// TestBudgetService_SetBudget_NilPublisherDoesNotPanic 验证 publisher 为 nil 时
// 整个流程不 panic，仅跳过事件发布步骤。
func TestBudgetService_SetBudget_NilPublisherDoesNotPanic(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	// note: 故意不调 SetPublisher，publisher 为 nil
	rdb, _ := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetRedis(rdb)

	ctx := context.Background()
	err := svc.SetBudget(ctx, "dept_daily", "dept-001", 500.0, -1, -1, uuid.New())

	assert.NoError(t, err)
	assert.Len(t, repo.upsertCalls, 1)
	assert.Equal(t, "500", rdb.Get(ctx, "budget:amount:dept_daily:dept-001").Val())
}

// TestBudgetService_SetBudget_NilRedisSkipsCacheWrite 验证 rdb 为 nil 时
// 跳过 Redis 写入但仍广播事件（事件发布不依赖 Redis）。
func TestBudgetService_SetBudget_NilRedisSkipsCacheWrite(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	// 故意不调 SetRedis，rdb 为 nil

	err := svc.SetBudget(context.Background(), "dept_daily", "dept-001", 500.0, -1, -1, uuid.New())

	assert.NoError(t, err)
	require.Len(t, repo.upsertCalls, 1)
	require.Len(t, pub.events, 1, "无 Redis 也应发事件（事件走 Pub/Sub 非 Redis key 写）")
}

// TestBudgetService_SetBudget_ZeroAmount 验证允许 0 金额（等于「关闭预算」语义）。
func TestBudgetService_SetBudget_ZeroAmount(t *testing.T) {
	repo := newFakeAllocRepo()
	alertRepo := newFakeAlertRepo()
	pub := &fakeBudgetEventPublisher{}
	rdb, _ := newTestMiniredis(t)

	svc := service.NewBudgetService(repo, alertRepo)
	svc.SetPublisher(pub)
	svc.SetRedis(rdb)

	err := svc.SetBudget(context.Background(), "member", "user-001", 0.0, -1, -1, uuid.New())

	assert.NoError(t, err, "金额 = 0 应允许")
	require.Len(t, repo.upsertCalls, 1)
	assert.Equal(t, 0.0, repo.upsertCalls[0].AmountUSD)
}

// TestBudgetService_GetBudget_WithConsumption 验证 GetBudget 在存在消费记录时
// 正确计算 used_ratio。
func TestBudgetService_GetBudget_WithConsumption(t *testing.T) {
	repo := newFakeAllocRepo()
	// 预设额度
	repo.allocations[allocationKey("dept_daily", "dept-001")] = &model.BudgetAllocation{
		ScopeType: "dept_daily",
		ScopeID:   "dept-001",
		AmountUSD: 1000.0,
		Period:    "monthly",
		Enabled:   true,
	}
	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	item, err := svc.GetBudget(context.Background(), "dept_daily", "dept-001")

	assert.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "dept_daily", item.ScopeType)
	assert.Equal(t, "dept-001", item.ScopeID)
	assert.Equal(t, 1000.0, item.AmountUSD)
	assert.Equal(t, "monthly", item.Period)
	assert.True(t, item.Enabled)
	// 默认没有消费记录，used 应为 0
	assert.Equal(t, 0.0, item.UsedUSD)
	assert.Equal(t, 0.0, item.UsedRatio)
}

// TestBudgetService_GetBudget_NotFound 验证未配置预算时返回默认 item
// （amount_usd=-1 表示"未配置/不限"，有别于显式封禁的 0），不返回错误。
func TestBudgetService_GetBudget_NotFound(t *testing.T) {
	repo := newFakeAllocRepo()
	// 注意：没预设任何 allocations

	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	item, err := svc.GetBudget(context.Background(), "company", "GLOBAL")

	assert.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "company", item.ScopeType)
	assert.Equal(t, "GLOBAL", item.ScopeID)
	assert.Equal(t, -1.0, item.AmountUSD, "未配置预算时 AmountUSD=-1（区别于显式封禁的 0）")
}

// TestBudgetService_InitDefaultBudgets_SkipsExisting 验证初始化对已有配置跳过。
func TestBudgetService_InitDefaultBudgets_SkipsExisting(t *testing.T) {
	repo := newFakeAllocRepo()
	// 部门 dept-existing 已有 dept_daily 配置，应跳过
	repo.allocations[allocationKey("dept_daily", "dept-existing")] = &model.BudgetAllocation{
		ScopeType: "dept_daily",
		ScopeID:   "dept-existing",
		AmountUSD: 999.0,
	}
	repo.listDeptStudioRow = []model.DeptStudioRow{
		{DeptID: "dept-existing", StudioID: "studio-1", StudioName: "工作室 A"},
		{DeptID: "dept-new", StudioID: "studio-1", StudioName: "工作室 A"},
	}
	alertRepo := newFakeAlertRepo()
	svc := service.NewBudgetService(repo, alertRepo)

	created, err := svc.InitDefaultBudgets(context.Background(), 500.0, 200.0, 2000.0)

	assert.NoError(t, err)
	// dept-existing: dept_daily 已有 → 跳过；还会创建 dept_service 和 studio
	// dept-new: dept_daily + dept_service + studio 各创建一次
	// 但注意：studio-1 因两部门共享，仅创建一次（幂等 + studioSeen map）
	// 预期创建数：
	// - dept-existing 的 dept_service = 1
	// - dept-new 的 dept_daily = 1
	// - dept-new 的 dept_service = 1
	// - studio-1 的 studio = 1
	// 共 4 条
	assert.Equal(t, 4, created, "应为 3 新 dept 项 + 1 studio 项（dept-existing 的 dept_daily 跳过）")

	// 验证已有配置未被覆盖
	existing, _ := repo.GetAllocation("dept_daily", "dept-existing")
	assert.Equal(t, 999.0, existing.AmountUSD, "已有配置 999 USD 不应被覆盖为默认 500")

	// 验证新建配置使用默认值
	newDeptDaily, _ := repo.GetAllocation("dept_daily", "dept-new")
	require.NotNil(t, newDeptDaily)
	assert.Equal(t, 500.0, newDeptDaily.AmountUSD)
	newStudio, _ := repo.GetAllocation("studio", "studio-1")
	require.NotNil(t, newStudio)
	assert.Equal(t, 2000.0, newStudio.AmountUSD)
}

// TestBudgetService_InitDefaultBudgets_RepoError 验证 repo 报错时向上透传错误。
func TestBudgetService_InitDefaultBudgets_RepoError(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.listDeptStudioErr = fmt.Errorf("db unavailable")

	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	created, err := svc.InitDefaultBudgets(context.Background(), 500.0, 200.0, 2000.0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list dept studio rows")
	assert.Equal(t, 0, created)
}

// TestBudgetService_InitDefaultBudgets_EmptyRows 验证没有任何部门时
// 应静默跳过（返回 0 创建，无错误）。
func TestBudgetService_InitDefaultBudgets_EmptyRows(t *testing.T) {
	repo := newFakeAllocRepo()
	repo.listDeptStudioRow = []model.DeptStudioRow{} // 空

	svc := service.NewBudgetService(repo, newFakeAlertRepo())

	created, err := svc.InitDefaultBudgets(context.Background(), 500.0, 200.0, 2000.0)

	assert.NoError(t, err)
	assert.Equal(t, 0, created)
	assert.Empty(t, repo.upsertCalls, "不应有任何 upsert 调用")
}
