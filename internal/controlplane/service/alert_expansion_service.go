package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// BudgetAlertService 处理告警触发（阈值+去重）与通知发送。
type BudgetAlertService struct {
	budgetRepo *repository.BudgetRepo
	alertRepo  *repository.BudgetAlertRepo
	// notifier 可选：webhook 发送（nil = 仅站内）
	notifier WebhookNotifier
}

// WebhookNotifier 飞书/webhook 通知接口（由 settings_service 实现）。
type WebhookNotifier interface {
	SendText(ctx context.Context, text string) error
}

func NewBudgetAlertService(budgetRepo *repository.BudgetRepo, alertRepo *repository.BudgetAlertRepo, notifier WebhookNotifier) *BudgetAlertService {
	return &BudgetAlertService{
		budgetRepo: budgetRepo,
		alertRepo:  alertRepo,
		notifier:   notifier,
	}
}

// EvaluateAndAlert 在核销后评估是否需要触发告警（阈值 80%/100%）。
// 应在 usage_collector 核销完成后异步调用，避免阻塞请求链路。
func (s *BudgetAlertService) EvaluateAndAlert(ctx context.Context, scopeType, scopeID string) {
	period := time.Now().Format("2006-01")

	alloc, err := s.budgetRepo.GetAllocation(scopeType, scopeID)
	if err != nil || alloc == nil || alloc.AmountUSD <= 0 {
		return
	}

	cons, err := s.budgetRepo.GetConsumption(scopeType, scopeID, period)
	if err != nil || cons == nil {
		return
	}

	ratio := cons.UsedUSD / alloc.AmountUSD

	thresholds := []int{model.AlertThreshold80}
	// 程序服务预算额外添加 100% 阈值
	if scopeType == model.BudgetScopeDeptService {
		thresholds = append(thresholds, model.AlertThreshold100)
	}

	for _, threshold := range thresholds {
		target := float64(threshold) / 100.0
		if ratio < target {
			continue
		}
		// 去重：已发过此 period+threshold 的告警则跳过
		if s.alertRepo.ExistsForPeriod(scopeType, scopeID, period, threshold) {
			continue
		}
		// 写去重记录
		if err := s.alertRepo.Create(&model.BudgetAlertRecord{
			ScopeType:  scopeType,
			ScopeID:    scopeID,
			PeriodType: "monthly",
			Period:     period,
			Threshold:  threshold,
			NotifiedAt: time.Now(),
		}); err != nil {
			slog.Error("budget_alert: failed to create alert record", "error", err)
			continue
		}
		// 发通知（降级：webhook 失败不阻断，仅记录）
		msg := fmt.Sprintf("【预算告警】%s(%s) 本月用量已达 %d%%（$%.2f / $%.2f），请关注并考虑扩容",
			scopeType, scopeID, threshold, cons.UsedUSD, alloc.AmountUSD)
		if s.notifier != nil {
			if err := s.notifier.SendText(ctx, msg); err != nil {
				slog.Warn("budget_alert: webhook notify failed", "error", err)
			}
		}
		slog.Info("budget_alert: sent", "scope_type", scopeType, "scope_id", scopeID,
			"threshold", threshold, "ratio", ratio)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 扩容审批状态机
// ─────────────────────────────────────────────────────────────────────────────

// ExpansionService 处理扩容申请的状态机与升级链。
type ExpansionService struct {
	repo *repository.ExpansionRepo
}

func NewExpansionService(repo *repository.ExpansionRepo) *ExpansionService {
	return &ExpansionService{repo: repo}
}

// CreateRequest 部门负责人发起扩容申请。
func (s *ExpansionService) CreateRequest(ctx context.Context, scopeType, scopeID string, requesterID uuid.UUID, currentAmount, requestedAmount float64, reason string) (*model.ExpansionRequest, error) {
	req := &model.ExpansionRequest{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		RequesterID:     requesterID,
		CurrentAmount:   currentAmount,
		RequestedAmount: requestedAmount,
		Reason:          reason,
		Status:          model.ExpansionStatusPending,
		CurrentLevel:    model.ExpansionLevelStudio, // 默认先到工作室级审批
	}
	if err := s.repo.Create(req); err != nil {
		return nil, fmt.Errorf("create expansion request: %w", err)
	}
	return req, nil
}

// Decide 审批人对申请做决策（approve/reject/escalate/cancel）。
// approverID：审批人；action：approve|reject|escalate|cancel；comment：备注。
func (s *ExpansionService) Decide(ctx context.Context, requestID string, approverID uuid.UUID, action, comment string) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return fmt.Errorf("expansion request not found: %w", err)
	}
	if req.Status != model.ExpansionStatusPending {
		return fmt.Errorf("expansion request is not in pending state")
	}

	var newStatus, newLevel string
	switch action {
	case model.ExpansionActionApprove:
		// approved ≠ 自动改额（FR-4.6）：仅标记同意，需上级手动走 PUT /api/v1/budgets 改额
		newStatus = model.ExpansionStatusApproved
		newLevel = req.CurrentLevel
	case model.ExpansionActionReject:
		newStatus = model.ExpansionStatusRejected
		newLevel = req.CurrentLevel
	case model.ExpansionActionEscalate:
		// 工作室盘子不足 → 升级到平台管理员
		if req.CurrentLevel != model.ExpansionLevelStudio {
			return fmt.Errorf("can only escalate from studio level")
		}
		newStatus = model.ExpansionStatusPending
		newLevel = model.ExpansionLevelPlatform
	case model.ExpansionActionCancel:
		newStatus = model.ExpansionStatusCancelled
		newLevel = req.CurrentLevel
	default:
		return fmt.Errorf("invalid action: %s", action)
	}

	// 更新申请状态
	if err := s.repo.UpdateStatus(requestID, newStatus, newLevel); err != nil {
		return fmt.Errorf("update expansion status: %w", err)
	}

	// 写审批流水
	_ = s.repo.CreateStep(&model.ExpansionApprovalStep{
		RequestID:  uuid.MustParse(requestID),
		Level:      req.CurrentLevel,
		ApproverID: &approverID,
		Action:     action,
		Comment:    comment,
	})
	return nil
}

// ListForRequester 查询申请人发起的申请。
func (s *ExpansionService) ListForRequester(ctx context.Context, requesterID string, status string) ([]model.ExpansionRequest, error) {
	return s.repo.ListByRequester(requesterID, status)
}

// ListPending 查询待某级别审批的申请。
func (s *ExpansionService) ListPending(ctx context.Context, level string) ([]model.ExpansionRequest, error) {
	return s.repo.ListPendingForApprover(level)
}
