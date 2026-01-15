package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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
	// ExistsForPeriod 的返回值（key: "scopeType:scopeID:period:threshold"）
	existsResult map[string]bool

	// 记录 Create 调用
	createdRecords []*model.BudgetAlertRecord
	createErr      error

	// FindOwnersByBudgetScope 的返回值（key: "scopeType:scopeID"）
	ownersResult map[string][]repository.AlertOwnerRow
	ownersErr    error
}

func newFakeAlertStore() *fakeAlertStore {
	return &fakeAlertStore{
		existsResult: make(map[string]bool),
		ownersResult: make(map[string][]repository.AlertOwnerRow),
	}
}

func (f *fakeAlertStore) ExistsForPeriod(scopeType, scopeID, period string, threshold int) bool {
	key := scopeType + ":" + scopeID + ":" + period + ":" + string(rune(threshold))
	return f.existsResult[key]
}

func (f *fakeAlertStore) Create(a *model.BudgetAlertRecord) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdRecords = append(f.createdRecords, a)
	return nil
}

func (f *fakeAlertStore) FindOwnersByBudgetScope(scopeType, scopeID string) ([]repository.AlertOwnerRow, error) {
	if f.ownersErr != nil {
		return nil, f.ownersErr
	}
	return f.ownersResult[scopeType+":"+scopeID], nil
}

// fakeAnnouncementWriter 模拟 announcementWriter，记录写入的公告和预写未读记录。
type fakeAnnouncementWriter struct {
	createdAnnouncements []*model.Announcement
	createErr            error

	prewriteCalls []prewriteCall
	prewriteErr   error
}

type prewriteCall struct {
	UserID         uuid.UUID
	AnnouncementID uuid.UUID
}

func (f *fakeAnnouncementWriter) Create(a *model.Announcement) error {
	if f.createErr != nil {
		return f.createErr
	}
	// 模拟 DB 自动生成 UUID
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.createdAnnouncements = append(f.createdAnnouncements, a)
	return nil
}

func (f *fakeAnnouncementWriter) PrewriteUnread(userID, announcementID uuid.UUID) error {
	if f.prewriteErr != nil {
		return f.prewriteErr
	}
	f.prewriteCalls = append(f.prewriteCalls, prewriteCall{userID, announcementID})
	return nil
}

// fakeWebhookNotifier 记录所有 SendText 调用。
type fakeWebhookNotifier struct {
	sentMessages []string
	sendErr      error
}

func (f *fakeWebhookNotifier) SendText(_ context.Context, text string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sentMessages = append(f.sentMessages, text)
	return nil
}

// newAlertRedis 启动 miniredis 并预设已用量 key，返回 client。
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

// newAlertService 创建一个注入了 fake 的 BudgetAlertService，
// 方便各测试直接拿来用，只需覆盖自己关心的字段。
func newAlertService(
	alloc *model.BudgetAllocation,
	alertStore *fakeAlertStore,
	annWriter *fakeAnnouncementWriter,
	notifier service.WebhookNotifier,
	rdb *redis.Client,
) *service.BudgetAlertService {
	svc := service.NewBudgetAlertServiceForTest(
		&fakeAllocReader{alloc: alloc},
		alertStore,
		notifier,
	)
	svc.SetAnnouncementRepo(annWriter)
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
	annWriter := &fakeAnnouncementWriter{}
	svc := newAlertService(nil, alertStore, annWriter, nil, nil)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "无预算配置不应写去重记录")
	assert.Empty(t, annWriter.createdAnnouncements, "无预算配置不应写站内信")
}

// TestEvaluateAndAlert_ZeroAmountBudget 验证：预算额度为 0（未启用）时
// 不触发告警。
func TestEvaluateAndAlert_ZeroAmountBudget(t *testing.T) {
	alloc := &model.BudgetAllocation{ScopeType: "dept_daily", ScopeID: "dept-001", AmountUSD: 0}
	alertStore := newFakeAlertStore()
	annWriter := &fakeAnnouncementWriter{}
	svc := newAlertService(alloc, alertStore, annWriter, nil, nil)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "零额度不应写去重记录")
}

// TestEvaluateAndAlert_BelowThreshold 验证：使用率 < 80% 时不告警。
func TestEvaluateAndAlert_BelowThreshold(t *testing.T) {
	alloc := &model.BudgetAllocation{ScopeType: "dept_daily", ScopeID: "dept-001", AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	annWriter := &fakeAnnouncementWriter{}
	// 已用 790 USD，使用率 79%，未达 80% 阈值
	rdb := newAlertRedis(t, "dept_daily", "dept-001", 790)
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), "dept_daily", "dept-001")

	assert.Empty(t, alertStore.createdRecords, "79% 使用率不应触发告警")
	assert.Empty(t, annWriter.createdAnnouncements)
}

// TestEvaluateAndAlert_Exactly80Percent 验证：使用率恰好 80% 时触发告警，
// 写去重记录、写站内信（warning 级），并调用 Webhook。
func TestEvaluateAndAlert_Exactly80Percent(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	annWriter := &fakeAnnouncementWriter{}
	notifier := &fakeWebhookNotifier{}
	rdb := newAlertRedis(t, scopeType, scopeID, 800) // 恰好 80%
	svc := newAlertService(alloc, alertStore, annWriter, notifier, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	// 去重记录
	require.Len(t, alertStore.createdRecords, 1, "应写入 1 条去重记录")
	assert.Equal(t, 80, alertStore.createdRecords[0].Threshold)
	assert.Equal(t, scopeType, alertStore.createdRecords[0].ScopeType)

	// 站内信
	require.Len(t, annWriter.createdAnnouncements, 1, "应写入 1 条站内信")
	ann := annWriter.createdAnnouncements[0]
	assert.Equal(t, "alert", ann.Category)
	assert.Equal(t, "warning", ann.Level, "80% 告警应为 warning 级")
	assert.Equal(t, "published", ann.Status)
	assert.Contains(t, ann.Title, "80%")

	// 定向投递给负责人
	assert.Len(t, annWriter.prewriteCalls, 1, "应向 1 个负责人投递站内信")

	// Webhook
	require.Len(t, notifier.sentMessages, 1, "应发 1 条 Webhook 消息")
	assert.Contains(t, notifier.sentMessages[0], "80%")
}

// TestEvaluateAndAlert_DeptServiceHasTwoThresholds 验证：dept_service scope
// 同时配置了 80% 和 100% 两个阈值，使用率 ≥ 100% 时两个阈值都触发。
func TestEvaluateAndAlert_DeptServiceHasTwoThresholds(t *testing.T) {
	scopeType, scopeID := "dept_service", "svc-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 200}
	alertStore := newFakeAlertStore()
	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 210) // 105%，超过两个阈值
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	// 应产生 80% 和 100% 两条去重记录
	require.Len(t, alertStore.createdRecords, 2, "dept_service 应触发 80% 和 100% 两个告警")
	thresholds := []int{alertStore.createdRecords[0].Threshold, alertStore.createdRecords[1].Threshold}
	assert.Contains(t, thresholds, 80)
	assert.Contains(t, thresholds, 100)

	// 100% 的站内信应为 critical 级
	require.Len(t, annWriter.createdAnnouncements, 2)
	levels := map[string]bool{}
	for _, a := range annWriter.createdAnnouncements {
		levels[a.Level] = true
	}
	assert.True(t, levels["warning"], "应有 warning 级告警（80%）")
	assert.True(t, levels["critical"], "应有 critical 级告警（100%）")
}

// TestEvaluateAndAlert_DeptServiceOnly100TriggersAt100 验证：dept_service scope
// 使用率恰好达到 80% 时，只触发 80% 告警，不触发 100%。
func TestEvaluateAndAlert_DeptServiceOnly80TriggersAt80(t *testing.T) {
	scopeType, scopeID := "dept_service", "svc-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 200}
	alertStore := newFakeAlertStore()
	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 160) // 恰好 80%
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 1, "仅触发 80% 告警")
	assert.Equal(t, 80, alertStore.createdRecords[0].Threshold)
}

// TestEvaluateAndAlert_Deduplication 验证：同一 scope/period/threshold 已存在去重记录时
// 不重复触发告警、不重复写站内信。
func TestEvaluateAndAlert_Deduplication(t *testing.T) {
	scopeType, scopeID := "studio", "studio-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()

	// 预设去重记录已存在（用 period 精确匹配）
	period := time.Now().Format("2006-01")
	alertStore.existsResult[scopeType+":"+scopeID+":"+period+":"+string(rune(80))] = true

	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900) // 90%，超阈值
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	assert.Empty(t, alertStore.createdRecords, "去重记录已存在，不应再写入")
	assert.Empty(t, annWriter.createdAnnouncements, "去重存在，不应写站内信")
}

// TestEvaluateAndAlert_AlertRecordCreateFails 验证：写去重记录失败时
// 跳过通知（不写站内信、不发 Webhook），不 panic。
func TestEvaluateAndAlert_AlertRecordCreateFails(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.createErr = errors.New("db write failed")
	annWriter := &fakeAnnouncementWriter{}
	notifier := &fakeWebhookNotifier{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, annWriter, notifier, rdb)

	// 不应 panic
	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Empty(t, annWriter.createdAnnouncements, "写去重失败后不应写站内信")
	assert.Empty(t, notifier.sentMessages, "写去重失败后不应发 Webhook")
}

// TestEvaluateAndAlert_WebhookFailureIsNonFatal 验证：Webhook 发送失败时
// 不影响站内信写入，整体流程不返回错误。
func TestEvaluateAndAlert_WebhookFailureIsNonFatal(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	annWriter := &fakeAnnouncementWriter{}
	notifier := &fakeWebhookNotifier{sendErr: errors.New("webhook timeout")}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, annWriter, notifier, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	// 站内信应正常写入，Webhook 失败不阻断
	assert.Len(t, annWriter.createdAnnouncements, 1, "Webhook 失败不应阻断站内信写入")
	assert.Len(t, alertStore.createdRecords, 1, "Webhook 失败不应阻断去重记录写入")
}

// TestEvaluateAndAlert_NoAnnouncementRepo 验证：未注入 AnnouncementRepo 时
// 不 panic，Webhook 仍正常发送。
func TestEvaluateAndAlert_NoAnnouncementRepo(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	notifier := &fakeWebhookNotifier{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)

	// 不注入 AnnouncementRepo
	svc := service.NewBudgetAlertServiceForTest(
		&fakeAllocReader{alloc: alloc},
		alertStore,
		notifier,
	)
	svc.SetRedis(rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	assert.Len(t, notifier.sentMessages, 1, "无 AnnouncementRepo 时 Webhook 仍应发送")
}

// ─────────────────────────────────────────────────────────────────────────────
// member scope 负责人查找逻辑测试
// ─────────────────────────────────────────────────────────────────────────────

// TestEvaluateAndAlert_MemberScopeNotifiesMemberOnly 验证：member scope 触发告警时
// 站内信只投递给成员本人（scopeID 即 user_id），不投递给 admin。
func TestEvaluateAndAlert_MemberScopeNotifiesMemberOnly(t *testing.T) {
	memberUserID := uuid.New()
	scopeType, scopeID := "member", memberUserID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 100}

	alertStore := newFakeAlertStore()
	// FindOwnersByBudgetScope 对 member scope 只返回成员本人
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: memberUserID.String(), Email: "member@example.com"},
	}

	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 85) // 85%，超 80% 阈值
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, annWriter.prewriteCalls, 1, "应向且只向成员本人投递站内信")
	assert.Equal(t, memberUserID, annWriter.prewriteCalls[0].UserID,
		"投递目标应为成员本人的 user_id")
}

// TestEvaluateAndAlert_MemberScopeDoesNotNotifyAdmin 验证：member scope 触发告警时
// 当 FindOwnersByBudgetScope 只返回成员自己（不含 admin）时，站内信不投递给 admin。
func TestEvaluateAndAlert_MemberScopeDoesNotNotifyAdmin(t *testing.T) {
	memberUserID := uuid.New()
	adminUserID := uuid.New()
	scopeType, scopeID := "member", memberUserID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 100}

	alertStore := newFakeAlertStore()
	// 确保 owners 里只有成员本人，没有 admin
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{
		{UserID: memberUserID.String()},
	}

	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 85)
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	// 确认 admin 不在投递列表中
	for _, call := range annWriter.prewriteCalls {
		assert.NotEqual(t, adminUserID, call.UserID,
			"admin 不应收到 member scope 的告警站内信")
	}
	// 确认成员本人在投递列表中
	require.Len(t, annWriter.prewriteCalls, 1)
	assert.Equal(t, memberUserID, annWriter.prewriteCalls[0].UserID)
}

// TestEvaluateAndAlert_MultipleOwners 验证：dept_daily scope 有多个负责人时
// 每人都收到定向站内信（PrewriteUnread 被调用多次，且所有 announcement_id 一致）。
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

	annWriter := &fakeAnnouncementWriter{}
	rdb := newAlertRedis(t, scopeType, scopeID, 850) // 85%
	svc := newAlertService(alloc, alertStore, annWriter, nil, rdb)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, annWriter.prewriteCalls, 3, "3 个负责人都应收到定向站内信")

	// 所有投递都指向同一条公告
	annID := annWriter.prewriteCalls[0].AnnouncementID
	for _, call := range annWriter.prewriteCalls {
		assert.Equal(t, annID, call.AnnouncementID, "所有投递应指向同一条告警公告")
	}

	// 三个负责人都在投递列表中
	receiverIDs := map[uuid.UUID]bool{}
	for _, call := range annWriter.prewriteCalls {
		receiverIDs[call.UserID] = true
	}
	assert.True(t, receiverIDs[owner1])
	assert.True(t, receiverIDs[owner2])
	assert.True(t, receiverIDs[owner3])
}

// TestEvaluateAndAlert_FindOwnersFails 验证：FindOwnersByBudgetScope 失败时
// 去重记录已写入、Webhook 仍发送，仅站内信投递静默失败（不 panic）。
func TestEvaluateAndAlert_FindOwnersFails(t *testing.T) {
	scopeType, scopeID := "dept_daily", "dept-001"
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 1000}
	alertStore := newFakeAlertStore()
	alertStore.ownersErr = errors.New("db query failed")
	annWriter := &fakeAnnouncementWriter{}
	notifier := &fakeWebhookNotifier{}
	rdb := newAlertRedis(t, scopeType, scopeID, 900)
	svc := newAlertService(alloc, alertStore, annWriter, notifier, rdb)

	assert.NotPanics(t, func() {
		svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)
	})

	// 公告本身写入成功，只是没有投递（owners 查询失败）
	assert.Len(t, annWriter.createdAnnouncements, 1, "查负责人失败不影响公告写入")
	assert.Empty(t, annWriter.prewriteCalls, "查负责人失败则无法投递，prewrite 调用为空")
	assert.Len(t, notifier.sentMessages, 1, "查负责人失败不阻断 Webhook")
}
