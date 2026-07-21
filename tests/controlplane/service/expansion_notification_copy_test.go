package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// stubApprovalPool 供 CreateRequestAdvanced 的提交通知测试使用：
// 始终把某固定审批人当作审批池成员，避免依赖真实组织表。
type stubApprovalPool struct {
	requesterID uuid.UUID
	approverID  uuid.UUID
}

func (s *stubApprovalPool) Resolve(context.Context, *model.ExpansionRequest) ([]uuid.UUID, error) {
	return []uuid.UUID{s.approverID}, nil
}

func (s *stubApprovalPool) ResolveInTx(context.Context, *gorm.DB, *model.ExpansionRequest) ([]uuid.UUID, error) {
	return []uuid.UUID{s.approverID}, nil
}

func (s *stubApprovalPool) ResolveScopeOwnerRecipients(context.Context, string, string) ([]uuid.UUID, error) {
	return nil, nil
}

func (s *stubApprovalPool) ResolveParentAdmins(context.Context, *model.ExpansionRequest) ([]uuid.UUID, error) {
	return nil, nil
}

// failingSnapshotPublisher 使审批通过后配置发布总是失败，从而触发
// expansion_apply_failed 通知（用于验证 level=critical）。
type failingSnapshotPublisher struct{}

func (failingSnapshotPublisher) MarkDirtyAndPublish(context.Context) error {
	return errors.New("snapshot publish boom")
}

// requireChineseBody 校验通知正文为面向用户的中文链路文案：含对象/维度/周期描述与金额。
// wantCurrent 为当前额度段的期望文案片段：current>0 时是「当前上限 $X.XX」，current=0 时是「当前未配置额度」。
func requireChineseBody(t *testing.T, content, wantCurrent, wantDetail string) {
	t.Helper()
	require.Contains(t, content, wantCurrent)
	require.Contains(t, content, "目标上限 $")
	require.Contains(t, content, "额度")
	require.Contains(t, content, "月额度")
	if wantDetail != "" {
		require.Contains(t, content, wantDetail)
	}
}

// TestExpansionSubmitNotificationIsChineseAndInfo 提交扩容申请后，通知审批池的
// outbox 正文应为核心中文文案且级别为 info。
func TestExpansionSubmitNotificationIsChineseAndInfo(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requesterID, approverID := uuid.New(), uuid.New()
	svc.WithApprovalPool(&stubApprovalPool{requesterID: requesterID, approverID: approverID})

	request, err := svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), RequesterID: requesterID,
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
		DurationType:  budgetdomain.DurationPermanent,
		CurrentAmount: 80, RequestedAmount: 120, Reason: "研发需要更大额度",
	})
	require.NoError(t, err)

	var row model.NotificationOutbox
	require.NoError(t, db.Where("user_id = ? AND type = ?", approverID, model.NotificationTypeExpansionSubmitted).
		First(&row).Error)
	require.NotEmpty(t, row.ID)
	require.Equal(t, model.NotificationLevelInfo, row.Level)
	require.Equal(t, "新的扩容审批申请", row.Title)
	require.NotEmpty(t, row.RefID)
	require.Equal(t, request.ID.String(), row.RefID)
	requireChineseBody(t, row.Content, "当前上限 $80.00", "申请原因")
	require.Contains(t, row.Content, "研发需要更大额度")
}

// TestExpansionRejectNotificationIsChineseAndWarning 扩容申请被驳回时，通知申请人的
// outbox 正文应为核心中文文案（含驳回意见）且级别为 warning。
func TestExpansionRejectNotificationIsChineseAndWarning(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID := uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	seedApproverName(t, db)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true).Error)
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "reject copy",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), approverUUID(), service.DecisionInput{
		Action: model.ExpansionActionReject, Comment: "不符合当前预算规划", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)

	var row model.NotificationOutbox
	require.NoError(t, db.Where("user_id = ? AND type = ?", requesterID, model.NotificationTypeExpansionRejected).
		First(&row).Error)
	require.Equal(t, model.NotificationLevelWarning, row.Level)
	require.Equal(t, "扩容申请已驳回", row.Title)
	requireChineseBody(t, row.Content, "当前未配置额度", "不符合当前预算规划")
	require.Contains(t, row.Content, "审批意见：不符合当前预算规划")
	require.Contains(t, row.Content, "审批人：测试审批人")
}

// approverUUID 返回一个固定的审批人 user id；配合 seedApproverName 在 users 表中预置姓名，
// 用于验证通过/驳回通知正文中的「审批人」行。
func approverUUID() uuid.UUID {
	return uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001")
}

func seedApproverName(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id,name) VALUES (?,?,?)`,
		approverUUID().String(), "dept-approver", "测试审批人").Error)
}

// TestExpansionApplyFailedNotificationIsChineseAndCritical 审批通过但配置发布失败时，
// 通知申请人的 outbox 正文应为核心中文文案（含生效失败原因）且级别为 critical。
func TestExpansionApplyFailedNotificationIsChineseAndCritical(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID, approverID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID.String(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true,
		requesterID.String(), requesterID.String()).Error)
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "apply failed copy",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)
	svc.WithSnapshotPublisher(failingSnapshotPublisher{})

	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, Comment: "同意", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.ErrorIs(t, err, service.ErrConfigSyncPending)

	var row model.NotificationOutbox
	require.NoError(t, db.Where("user_id = ? AND type = ?", requesterID, model.NotificationTypeExpansionApplyFailed).
		First(&row).Error)
	require.Equal(t, model.NotificationLevelCritical, row.Level)
	require.Equal(t, "扩容申请生效失败", row.Title)
	requireChineseBody(t, row.Content, "当前未配置额度", "生效失败原因：snapshot publish boom")
}
