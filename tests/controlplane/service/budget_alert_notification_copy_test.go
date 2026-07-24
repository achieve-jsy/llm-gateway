package service_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/budgetperiod"
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
// 预算告警文案（站内信 + 邮件 + webhook 共用同一份 Title/Content）
//
// 约束来自真实线上问题：告警文案里出现了 scope UUID 与 member/dept_daily 这类
// 内部枚举名，用户完全看不懂。以下用例把「文案不得携带内部标识」固化为回归防线。
// ─────────────────────────────────────────────────────────────────────────────

// uuidPattern 匹配任意 UUID，用于断言告警文案不含内部 ID。
var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// forbiddenTokens 是绝不允许出现在用户文案里的内部枚举名。
var forbiddenTokens = []string{
	"member", "dept_daily", "dept_service", "studio", "company",
	"tier", "total", "daily", "weekly", "monthly",
}

// assertNoInternalIdentifiers 断言给定的用户文案既不含 UUID，也不含内部枚举名。
func assertNoInternalIdentifiers(t *testing.T, scopeID string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		require.NotEmpty(t, text)
		assert.False(t, uuidPattern.MatchString(text), "文案不应包含 UUID：%s", text)
		assert.NotContains(t, text, scopeID, "文案不应包含 scope_id：%s", text)
		for _, token := range forbiddenTokens {
			assert.NotContains(t, text, token, "文案不应包含内部枚举名 %q：%s", token, text)
		}
	}
}

// fakeNamedAllocReader 在 fakeAllocReader 之上实现 ResolveScopeNames，
// 模拟生产环境 *repository.BudgetRepo 的可读名称解析能力。
type fakeNamedAllocReader struct {
	fakeAllocReader
	names map[string]string
}

func (f *fakeNamedAllocReader) ResolveScopeNames(_ string, scopeIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(scopeIDs))
	for _, id := range scopeIDs {
		if name, ok := f.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

// newCopyAlertService 构造带名称解析能力的告警服务，固定一个受告警的负责人，
// 并返回可继续写入 v2 计数器的 Redis 客户端。usedUSD 大于 0 时写入 legacy 计数键。
func newCopyAlertService(
	t *testing.T,
	alloc *model.BudgetAllocation,
	names map[string]string,
	usedUSD float64,
	notifier service.WebhookNotifier,
) (*service.BudgetAlertService, *fakeAlertStore, *fakeNotificationWriter, *redis.Client) {
	t.Helper()
	alertStore := newFakeAlertStore()
	alertStore.ownersResult[alloc.ScopeType+":"+alloc.ScopeID] = []repository.AlertOwnerRow{
		{UserID: uuid.New().String()},
	}
	notifWriter := &fakeNotificationWriter{}
	svc := service.NewBudgetAlertServiceForTest(
		&fakeNamedAllocReader{fakeAllocReader: fakeAllocReader{alloc: alloc}, names: names},
		alertStore,
		notifier,
	)
	svc.SetNotificationRepo(notifWriter)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	if usedUSD > 0 {
		period := time.Now().Format("2006-01")
		key := "budget:used:" + alloc.ScopeType + ":" + alloc.ScopeID + ":" + period
		require.NoError(t, rdb.Set(context.Background(), key, usedUSD, 0).Err())
	}
	svc.SetRedis(rdb)
	return svc, alertStore, notifWriter, rdb
}

// TestBudgetAlertCopy_MemberAlertUsesNameNotID 覆盖线上原样：member 告警必须展示
// 成员姓名，且标题/正文/webhook 均不得出现 scope UUID 或 "member"。
func TestBudgetAlertCopy_MemberAlertUsesNameNotID(t *testing.T) {
	memberID := uuid.New()
	scopeType, scopeID := model.BudgetScopeMember, memberID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 300}

	webhook := &fakeWebhookNotifier{}
	svc, _, notifWriter, _ := newCopyAlertService(t, alloc, map[string]string{scopeID: "芮宇潇"}, 262.5, webhook)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, notifWriter.createdNotifications, 1)
	notification := notifWriter.createdNotifications[0]
	require.Len(t, webhook.sentMessages, 1)

	assertNoInternalIdentifiers(t, scopeID, notification.Title, notification.Content, webhook.sentMessages[0])
	assert.Equal(t, "预算告警：芮宇潇 的个人额度 本月用量已达 80%", notification.Title)
	assert.Contains(t, notification.Content, "**芮宇潇 的个人额度**")
	assert.Contains(t, notification.Content, "本月用量已达 **80%**（当前 87.5%），请及时关注。")
	assert.Contains(t, notification.Content, "- 已用：$262.50 USD")
	assert.Contains(t, notification.Content, "- 额度上限：$300.00 USD")
	assert.Contains(t, notification.Content, "- 剩余可用：$37.50 USD")
	assert.NotContains(t, notification.Content, "告警档位", "档位已并入标题，不应再有冗余条目")
	assert.Contains(t, webhook.sentMessages[0], "芮宇潇 的个人额度 本月用量已达 80%（当前 87.5%）")
}

// TestBudgetAlertCopy_NoNameResolverFallsBackToLabel 断言：名称解析能力缺失时
// 退化为中文类型标签，绝不回落为 scope_id；用量正好等于档位时不重复播报百分比。
func TestBudgetAlertCopy_NoNameResolverFallsBackToLabel(t *testing.T) {
	memberID := uuid.New()
	scopeType, scopeID := model.BudgetScopeMember, memberID.String()
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 300}

	alertStore := newFakeAlertStore()
	alertStore.ownersResult[scopeType+":"+scopeID] = []repository.AlertOwnerRow{{UserID: memberID.String()}}
	notifWriter := &fakeNotificationWriter{}
	// fakeAllocReader 未实现 ResolveScopeNames，走类型标签回退分支。
	svc := service.NewBudgetAlertServiceForTest(&fakeAllocReader{alloc: alloc}, alertStore, nil)
	svc.SetNotificationRepo(notifWriter)
	svc.SetRedis(newAlertRedis(t, scopeType, scopeID, 240))

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, notifWriter.createdNotifications, 1)
	notification := notifWriter.createdNotifications[0]
	assertNoInternalIdentifiers(t, scopeID, notification.Title, notification.Content)
	assert.Equal(t, "预算告警：成员个人额度 本月用量已达 80%", notification.Title)
	assert.Contains(t, notification.Content, "本月用量已达 **80%**，请及时关注。")
	assert.NotContains(t, notification.Content, "（当前", "当前用量等于档位时不应重复播报")
}

// TestBudgetAlertCopy_DailyPeriodWording 断言：日额度告警说「今日」，不能沿用「本月」，
// 且额度上限取日额度而非月额度。
func TestBudgetAlertCopy_DailyPeriodWording(t *testing.T) {
	deptID := uuid.New().String()
	scopeType, scopeID := model.BudgetScopeDeptDaily, deptID
	alloc := &model.BudgetAllocation{
		ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 9000, DailyAmountUSD: 1000,
	}
	svc, _, notifWriter, rdb := newCopyAlertService(t, alloc, map[string]string{deptID: "X-Lab大模型"}, 0, nil)

	// 日额度走 v2 计数器，键里带预算周期 key。
	window, err := budgetperiod.Resolve(time.Now(), budgetdomain.PeriodDaily, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	require.NoError(t, err)
	key := "budget:v2:used:" + scopeType + ":" + scopeID + ":" + window.Key
	require.NoError(t, rdb.Set(context.Background(), key, "850000000", 0).Err())

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID, "daily", "total", "")

	require.Len(t, notifWriter.createdNotifications, 1)
	notification := notifWriter.createdNotifications[0]
	assertNoInternalIdentifiers(t, scopeID, notification.Title, notification.Content)
	assert.Equal(t, "预算告警：X-Lab大模型·成员日常预算 今日用量已达 80%", notification.Title)
	assert.NotContains(t, notification.Title, "本月")
	assert.Contains(t, notification.Content, "**X-Lab大模型·成员日常预算** 今日用量已达 **80%**（当前 85%），请及时关注。")
	assert.Contains(t, notification.Content, "- 额度上限：$1000.00 USD", "应使用日额度上限而不是月额度")
}

// TestBudgetAlertCopy_ExhaustedShowsOverrun 断言：用尽/超额时文案明确说「已用尽」，
// 并给出超出金额，而不是把一个负的「剩余可用」留给用户自己算。
func TestBudgetAlertCopy_ExhaustedShowsOverrun(t *testing.T) {
	deptID := uuid.New().String()
	scopeType, scopeID := model.BudgetScopeDeptService, deptID
	// dept_service 的总预算同时评估 80% 与 100% 两档。
	alloc := &model.BudgetAllocation{ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 300}
	svc, alertStore, notifWriter, _ := newCopyAlertService(t, alloc, map[string]string{deptID: "X-Lab大模型"}, 300.12, nil)

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID)

	require.Len(t, alertStore.createdRecords, 2)
	require.Len(t, notifWriter.createdNotifications, 2)

	titleByLevel := map[string]string{}
	for _, notification := range notifWriter.createdNotifications {
		assertNoInternalIdentifiers(t, scopeID, notification.Title, notification.Content)
		assert.Contains(t, notification.Content, "额度已用尽（当前 100.0%）")
		assert.Contains(t, notification.Content, "- 超出：$0.12 USD")
		assert.NotContains(t, notification.Content, "剩余可用")
		// 已用尽时标题不得再播报「已达 80%/100%」，否则与正文自相矛盾。
		assert.Equal(t, "预算告警：X-Lab大模型·程序服务预算 本月额度已用尽", notification.Title)
		titleByLevel[notification.Level] = notification.Title
	}
	assert.Contains(t, titleByLevel, model.NotificationLevelWarning)
	assert.Contains(t, titleByLevel, model.NotificationLevelCritical)
}

// TestBudgetAlertCopy_TierAlertUsesTierName 断言：模型等级告警展示等级名，
// 且不得出现等级 UUID。
func TestBudgetAlertCopy_TierAlertUsesTierName(t *testing.T) {
	memberID := uuid.New()
	allocationID, tierID := uuid.New(), uuid.New()
	scopeType, scopeID := model.BudgetScopeMember, memberID.String()
	alloc := &model.BudgetAllocation{
		ID: allocationID, ScopeType: scopeType, ScopeID: scopeID, AmountUSD: 300,
	}
	base, err := budgetdomain.ParseAmount("100.000000")
	require.NoError(t, err)

	svc, _, notifWriter, rdb := newCopyAlertService(t, alloc, map[string]string{scopeID: "芮宇潇"}, 0, nil)
	svc.SetBudgetTierRepo(&fakeAlertTierRepo{limits: []model.BudgetTierLimit{{
		BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: base,
		Enabled: true, EffectiveAt: time.Now().Add(-time.Hour),
		ModelTier: model.ModelTier{ID: tierID, Name: "Claude系列"},
	}}})

	window, err := budgetperiod.Resolve(time.Now(), budgetdomain.PeriodMonthly, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	require.NoError(t, err)
	key := "budget:v2:tier:used:" + scopeType + ":" + scopeID + ":" + tierID.String() + ":" + window.Key
	require.NoError(t, rdb.Set(context.Background(), key, "90000000", 0).Err())

	svc.EvaluateAndAlert(context.Background(), scopeType, scopeID, "monthly", "tier", tierID.String())

	require.Len(t, notifWriter.createdNotifications, 1)
	notification := notifWriter.createdNotifications[0]
	assertNoInternalIdentifiers(t, scopeID, notification.Title, notification.Content)
	assert.NotContains(t, notification.Title, tierID.String(), "等级 UUID 不应出现在文案中")
	assert.Equal(t, "预算告警：芮宇潇 的个人额度 · Claude系列 本月用量已达 80%", notification.Title)
}
