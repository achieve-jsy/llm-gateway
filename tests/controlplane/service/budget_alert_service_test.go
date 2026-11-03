package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fakes
// ─────────────────────────────────────────────────────────────────────────────

// fakeAllocReader 模拟 budgetAllocReader，返回预设的额度配置。
type fakeAllocReader struct {
	alloc *model.BudgetAllocation
	err   error
}

func (f *fakeAllocReader) GetAllocation(_, _ string) (*model.BudgetAllocation, error) {
	return f.alloc, f.err
}

// fakeAlertStore 模拟 alertRecordStore，记录所有写入和查询调用。
type fakeAlertStore struct {
	existsResult   map[string]bool
	createdRecords []*model.BudgetAlertRecord
	deletedDims    []repository.AlertDimension
	deletedErrs    error
	createErr      error
	ownersResult   map[string][]repository.AlertOwnerRow
	ownersErr      error
}

type fakeAlertTierRepo struct {
	limits    []model.BudgetTierLimit
	overrides []model.BudgetTemporaryOverride
}

func (f *fakeAlertTierRepo) ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error) {
	return f.limits, nil
}

func (f *fakeAlertTierRepo) ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error) {
	return f.overrides, nil
}

func newFakeAlertStore() *fakeAlertStore {
	return &fakeAlertStore{
		existsResult: make(map[string]bool),
		ownersResult: make(map[string][]repository.AlertOwnerRow),
	}
}

func (f *fakeAlertStore) ExistsForPeriod(_ context.Context, d repository.AlertDimension) (bool, error) {
	key := d.ScopeType + ":" + d.ScopeID + ":" + d.Period + ":" + string(rune(d.Threshold))
	return f.existsResult[key], nil
}

func (f *fakeAlertStore) Create(a *model.BudgetAlertRecord) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdRecords = append(f.createdRecords, a)
	return nil
}

func (f *fakeAlertStore) DeleteByDimensionAndThreshold(_ context.Context, d repository.AlertDimension, _ int) error {
	if f.deletedErrs != nil {
		return f.deletedErrs
	}
	f.deletedDims = append(f.deletedDims, d)
	return nil
}

func (f *fakeAlertStore) FindOwnersByBudgetScope(scopeType, scopeID string) ([]repository.AlertOwnerRow, error) {
	if f.ownersErr != nil {
		return nil, f.ownersErr
	}
	return f.ownersResult[scopeType+":"+scopeID], nil
}

// fakeNotificationWriter 模拟 notificationWriter，记录写入的通知。
type fakeNotificationWriter struct {
	createdNotifications []*model.Notification
	createErr            error
}

func (f *fakeNotificationWriter) Create(n *model.Notification) error {
	if f.createErr != nil {
		return f.createErr
	}
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	f.createdNotifications = append(f.createdNotifications, n)
	return nil
}



// newAlertRedis 启动 miniredis 并预设已用量 key。
func newAlertRedis(t *testing.T, scopeType, scopeID string, usedUSD float64) *redis.Client {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	period := time.Now().Format("2006-01")
	key := "budget:used:" + scopeType + ":" + scopeID + ":" + period
	require.NoError(t, rdb.Set(context.Background(), key, usedUSD, 0).Err())
	return rdb
}

// newAlertService 创建注入了 fake 的 BudgetAlertService。
func newAlertService(
	alloc *model.BudgetAllocation,
	alertStore *fakeAlertStore,
	notifWriter *fakeNotificationWriter,
	rdb *redis.Client,
) *service.BudgetAlertService {
	svc := service.NewBudgetAlertServiceForTest(
		&fakeAllocReader{alloc: alloc},
		alertStore,
	)
	svc.SetNotificationRepo(notifWriter)
	if rdb != nil {
		svc.SetRedis(rdb)
	}
	return svc
}

// ─────────────────────────────────────────────────────────────────────────────
// EvaluateAndAlert 测试
// ─────────────────────────────────────────────────────────────────────────────

// TestEvaluateAndAlert_NoBudgetConfig 验证：未配置预算（GetAllocation 返回 nil）时
// 直接返回，不触发任何告警动作。
func TestEvaluateAndAlert_NoBudgetConfig(t *testing.T) {
	alertStore := newFakeAlertStore()
	notifWriter := &fakeNotificationWriter{}
	svc := newAlertService(nil, alertStore, notifWriter, nil)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "无预算配置不应写去重记录")
	assert.Empty(t, notifWriter.createdNotifications, "无预算配置不应写通知")
}

// TestEvaluateAndAlert_ZeroAmountBudget 验证：预算额度为 0（未启用）时不触发告警。
func TestEvaluateAndAlert_ZeroAmountBudget(t *testing.T) {
	alloc := &model.BudgetAllocation{ScopeType: "dept_daily", ScopeID: "dept-001", AmountUSD: 0}
	alertStore := newFakeAlertStore()
	notifWriter := &fakeNotificationWriter{}
	svc := newAlertService(alloc, alertStore, notifWriter, nil)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "零额度不应写去重记录")
}

// TestEvaluateAndAlert_BelowThreshold 验证：使用率 < 80% 时不告警。
func TestEvaluateAndAlert_BelowThreshold(t *testing.T) {
	alloc := &model.BudgetAllocation{ScopeType: "dept_daily", ScopeID: "dept-001", AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, "dept_daily", "dept-001", 790)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "79% 使用率不应触发告警")
	assert.Empty(t, notifWriter.createdNotifications)
}

// TestEvaluateAndAlert_Exactly80Percent 验证：使用率恰好 80% 时触发告警，
// 写去重记录、写通知（warning 级），并调用 Webhook。
func TestEvaluateAndAlert_Exactly80Percent(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	ownerID := uuid.New()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: ownerID.String()},
	}
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 800)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	// 去重记录
	require.Len(t, alertStore.createdRecords, 1, "应写入 1 条去重记录")
	assert.Equal(t, 80, alertStore.createdRecords[0].Threshold)

	// 通知
	require.Len(t, notifWriter.createdNotifications, 1, "应写入 1 条通知")
	n := notifWriter.createdNotifications[0]
	assert.Equal(t, model.NotificationTypeBudgetAlert, n.Type)
	assert.Equal(t, model.NotificationLevelWarning, n.Level, "80% 告警应为 warning")
	assert.Equal(t, ownerID, n.UserID, "通知应发给对应负责人")
	assert.Equal(t, 80, n.TemplateData["Threshold"], "80% 告警阈值应传入 TemplateData")
	assert.Contains(t, n.TemplateData["ScopeID"], "dept-001")
}

// TestEvaluateAndAlert_DeptServiceHasTwoThresholds 验证：dept_service scope
// 使用率 ≥ 100% 时触发 80% 和 100% 两个阈值。
func TestEvaluateAndAlert_DeptServiceHasTwoThresholds(t *testing.T) {
	scopeType, scopeID := "dept_service", "svc-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 200}
	alertStore := newFakeAlertStore()
	// 预设一个 owner，否则 notification 不会被写入
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 210)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 2, "应触发 80% 和 100% 两个告警")
	thresholds := []int{alertStore.createdRecords[0].Threshold, alertStore.createdRecords[1].Threshold}
	assert.Contains(t, thresholds, 80)
	assert.Contains(t, thresholds, 100)

	require.Len(t, notifWriter.createdNotifications, 2)
	levels := map[string]bool{}
	for _, n := range notifWriter.createdNotifications {
		levels[n.Level] = true
	}
	assert.True(t, levels[model.NotificationLevelWarning], "应有 warning（80%）")
	assert.True(t, levels[model.NotificationLevelCritical], "应有 critical（100%）")
}

// TestEvaluateAndAlert_DeptServiceOnly80TriggersAt80 验证：dept_service 恰好 80% 只触发 80%。
func TestEvaluateAndAlert_DeptServiceOnly80TriggersAt80(t *testing.T) {
	scopeType, scopeID := "dept_service", "svc-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 200}
	alertStore := newFakeAlertStore()
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 160)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 1)
	assert.Equal(t, 80, alertStore.createdRecords[0].Threshold)
}

// TestEvaluateAndAlert_AllScopesGetHundredAtFullUsage 验证：非 dept_service 作用域
// 用量 ≥100% 时同样触发 80% 和 100% 双档（不再只覆盖 tier/dept_service）。
func TestEvaluateAndAlert_AllScopesGetHundredAtFullUsage(t *testing.T) {
	scopeType, scopeID := "studio", "studio-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 1000)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 2, "满额应触发 80% 和 100% 两档")
	thresholds := map[int]bool{}
	for _, r := range alertStore.createdRecords {
		thresholds[r.Threshold] = true
	}
	assert.True(t, thresholds[80])
	assert.True(t, thresholds[100])

	require.Len(t, notifWriter.createdNotifications, 2)
	levels := map[string]bool{}
	for _, n := range notifWriter.createdNotifications {
		levels[n.Level] = true
	}
	assert.True(t, levels[model.NotificationLevelWarning], "应有 warning（80%）")
	assert.True(t, levels[model.NotificationLevelCritical], "满额应有 critical（100%）")
}

func TestEvaluateTierAlert_UsesTemporaryEffectiveLimitAndTwoThresholds(t *testing.T) {
	scopeType, scopeID := "member", "u-1"
	allocationID, tierID := uuid.New(), uuid.New()
	alloc := &model.BudgetAllocation{ID: allocationID, ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 100}
	base, err := budgetdomain.ParseAmount("100.000000")
	require.NoError(t, err)
	target, err := budgetdomain.ParseAmount("200.000000")
	require.NoError(t, err)
	alertStore := newFakeAlertStore()
	svc := newAlertService(alloc, alertStore, &fakeNotificationWriter{}, nil)
	svc.SetBudgetTierRepo(&fakeAlertTierRepo{
		limits: []model.BudgetTierLimit{{
			BudgetAllocationID: allocationID, ModelTierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, LimitUSD: base,
			Enabled: true, EffectiveAt: time.Now().Add(-time.Hour),
		}},
		overrides: []model.BudgetTemporaryOverride{{
			BudgetAllocationID: allocationID, ModelTierID: &tierID,
			DimensionType: budgetdomain.DimensionTier, PeriodType: budgetdomain.PeriodMonthly,
			TargetLimitUSD: target, StartsAt: time.Now().Add(-time.Hour),
			ExpiresAt: time.Now().Add(time.Hour), Status: budgetdomain.OverrideActive,
		}},
	})
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc.SetRedis(rdb)
	period := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01")
	key := "budget:v2:tier:used:" + scopeType + ":" + scopeID + ":" + tierID.String() + ":" + period
	require.NoError(t, rdb.Set(context.Background(), key, "100000000", 0).Err())

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID, "monthly", "tier", tierID.String())
	require.Empty(t, alertStore.createdRecords, "temporary limit 200 means usage 100 is below 80%")

	require.NoError(t, rdb.Set(context.Background(), key, "200000000", 0).Err())
	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID, "monthly", "tier", tierID.String())
	require.Len(t, alertStore.createdRecords, 2)
	require.Equal(t, []int{80, 100}, []int{
		alertStore.createdRecords[0].Threshold,
		alertStore.createdRecords[1].Threshold,
	})
}

// TestEvaluateAndAlert_Deduplication 验证：已存在去重记录时不重复告警。
func TestEvaluateAndAlert_Deduplication(t *testing.T) {
	scopeType, scopeID := "studio", "studio-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	period := time.Now().Format("2006-01")
	alertStore.existsResult[scopeType+":"+scopeID+":"+period+":"+string(rune(80))] = true

	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	assert.Empty(t, alertStore.createdRecords, "去重已存在不应写入")
	assert.Empty(t, notifWriter.createdNotifications, "去重已存在不应发通知")
}

// TestEvaluateAndAlert_AlertRecordCreateFails 验证：写去重记录失败后跳过通知。
func TestEvaluateAndAlert_AlertRecordCreateFails(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.createErr = errors.New("db write failed")
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Empty(t, notifWriter.createdNotifications)
}

// TestEvaluateAndAlert_WebhookFailureIsNonFatal 验证：Webhook 失败不阻断通知。
func TestEvaluateAndAlert_WebhookFailureIsNonFatal(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	// 预设 owner，确保通知会被创建
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Len(t, notifWriter.createdNotifications, 1)
	assert.Len(t, alertStore.createdRecords, 1)
}

// TestEvaluateAndAlert_AllSendsFailRollsBackDedupe 验证：有负责人但通知全部入队失败时，
// 回滚去重记录，使下次评估能重试，告警不会被永久吞掉。
func TestEvaluateAndAlert_AllSendsFailRollsBackDedupe(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	notifWriter := &fakeNotificationWriter{createErr: errors.New("outbox write failed")}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Empty(t, notifWriter.createdNotifications, "全部入队失败不应残留通知")
	require.Len(t, alertStore.createdRecords, 1, "去重记录应先行写入")
	require.Len(t, alertStore.deletedDims, 1, "全部发送失败应回滚去重记录")
	assert.Equal(t, 80, alertStore.deletedDims[0].Threshold)
}

// TestEvaluateAndAlert_RollbackKeepsSucceededThreshold 验证：80% 发送失败回滚时
// 不误删同周期已成功的 100% 档位记录（回滚按档位精确删除）。
func TestEvaluateAndAlert_RollbackKeepsSucceededThreshold(t *testing.T) {
	scopeType, scopeID := "dept_service", "svc-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 200}
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	// 模拟 100% 档已成功发送并留存去重记录
	period := time.Now().Format("2006-01")
	alertStore.existsResult[scopeType+":"+scopeID+":"+period+":"+string(rune(100))] = true

	notifWriter := &fakeNotificationWriter{createErr: errors.New("outbox write failed")}
	rdb := newAlertRedis(t, scopeType, scopeID, 210)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 1, "仅 80% 档新建去重记录")
	assert.Equal(t, 80, alertStore.createdRecords[0].Threshold)
	require.Len(t, alertStore.deletedDims, 1)
	assert.Equal(t, 80, alertStore.deletedDims[0].Threshold, "回滚应精确命中 80% 档")
}
func TestEvaluateAndAlert_NoNotificationRepo(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := service.NewBudgetAlertServiceForTest(&fakeAllocReader{alloc: alloc}, alertStore)
	svc.SetRedis(rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})
}

// TestEvaluateAndAlert_MemberScopeNotifiesMemberOnly 验证：member scope 只通知成员本人。
func TestEvaluateAndAlert_MemberScopeNotifiesMemberOnly(t *testing.T) {
	memberUserID := uuid.New()
	scopeType, scopeID := "member", memberUserID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 100}

	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: memberUserID.String(), Email: "member@example.com"},
	}

	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 85)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, notifWriter.createdNotifications, 1, "应通知成员本人")
	assert.Equal(t, memberUserID, notifWriter.createdNotifications[0].UserID)
}

// TestEvaluateAndAlert_MemberScopeDoesNotNotifyAdmin 验证：member scope 不通知 admin。
func TestEvaluateAndAlert_MemberScopeDoesNotNotifyAdmin(t *testing.T) {
	memberUserID := uuid.New()
	adminUserID := uuid.New()
	scopeType, scopeID := "member", memberUserID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 100}

	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: memberUserID.String()},
	}

	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 85)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, notifWriter.createdNotifications, 1)
	assert.NotEqual(t, adminUserID, notifWriter.createdNotifications[0].UserID)
	assert.Equal(t, memberUserID, notifWriter.createdNotifications[0].UserID)
}

// TestEvaluateAndAlert_MultipleOwners 验证：多负责人时每人收到通知。
func TestEvaluateAndAlert_MultipleOwners(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}

	owner1 := uuid.New()
	owner2 := uuid.New()
	owner3 := uuid.New()
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: owner1.String()},
		{UserID: owner2.String()},
		{UserID: owner3.String()},
	}

	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 850)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, notifWriter.createdNotifications, 3, "3 个负责人都应收到通知")
	receiverIDs := map[uuid.UUID]bool{}
	for _, n := range notifWriter.createdNotifications {
		receiverIDs[n.UserID] = true
	}
	assert.True(t, receiverIDs[owner1])
	assert.True(t, receiverIDs[owner2])
	assert.True(t, receiverIDs[owner3])
}

// TestEvaluateAndAlert_FindOwnersFails 验证：查负责人失败时不阻断其他逻辑。
func TestEvaluateAndAlert_FindOwnersFails(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.ownersErr = errors.New("db query failed")
	notifWriter := &fakeNotificationWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, notifWriter, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Empty(t, notifWriter.createdNotifications, "查负责人失败不应写通知")
	assert.Len(t, alertStore.createdRecords, 1, "去重记录应正常写入")
}
