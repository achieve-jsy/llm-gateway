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
	repo      *repository.ExpansionRepo
	budgetSvc *BudgetService
}

func NewExpansionService(repo *repository.ExpansionRepo) *ExpansionService {
	return &ExpansionService{repo: repo}
}

// SetBudgetService 注入预算服务（用于查询使用率）。
func (s *ExpansionService) SetBudgetService(bs *BudgetService) {
	s.budgetSvc = bs
}

// GetBudgetUsedRatio 查询某 scope 当前使用率（供 ListPending 附加信息用）。
func (s *ExpansionService) GetBudgetUsedRatio(ctx context.Context, scopeType, scopeID string) (float64, error) {
	if s.budgetSvc == nil {
		return 0, nil
	}
	item, err := s.budgetSvc.GetBudget(ctx, scopeType, scopeID)
	if err != nil || item == nil {
		return 0, err
	}
	return item.UsedRatio, nil
}

func (s *ExpansionService) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	if s.budgetSvc == nil {
		return false, nil
	}
	return s.budgetSvc.MemberInDeptIDs(memberID, deptIDs)
}

// CreateRequest 部门负责人发起扩容申请。
func (s *ExpansionService) CreateRequest(ctx context.Context, scopeType, scopeID string, requesterID uuid.UUID, currentAmount, requestedAmount float64, periodType, reason string) (*model.ExpansionRequest, error) {
	currentLevel := model.ExpansionLevelStudio
	if scopeType == model.BudgetScopeMember {
		currentLevel = model.ExpansionLevelDept
	} else if scopeType == model.BudgetScopeStudio {
		currentLevel = model.ExpansionLevelPlatform
	}
	if periodType == "" {
		periodType = model.ExpansionPeriodMonthly
	}
	req := &model.ExpansionRequest{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		RequesterID:     requesterID,
		PeriodType:      periodType,
		CurrentAmount:   currentAmount,
		RequestedAmount: requestedAmount,
		Reason:          reason,
		Status:          model.ExpansionStatusPending,
		CurrentLevel:    currentLevel,
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

	// 批准时自动按 period_type 更新对应预算字段
	if action == model.ExpansionActionApprove && s.budgetSvc != nil {
		if err := s.applyApprovedQuota(ctx, req, approverID); err != nil {
			// 非致命：日志记录即可，申请状态已更新
			slog.Warn("expansion: auto-apply quota failed", "request_id", requestID, "error", err)
		}
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

// applyApprovedQuota 批准时自动将 RequestedAmount 写入 BudgetAllocation 对应维度字段。
func (s *ExpansionService) applyApprovedQuota(ctx context.Context, req *model.ExpansionRequest, approverID uuid.UUID) error {
	alloc, err := s.budgetSvc.repo.GetAllocation(req.ScopeType, req.ScopeID)
	if err != nil {
		// 该 scope 尚无预算记录，以请求额度初始化
		alloc = &model.BudgetAllocation{
			ScopeType:       req.ScopeType,
			ScopeID:         req.ScopeID,
			AmountUSD:       0,
			DailyAmountUSD:  -1,
			WeeklyAmountUSD: -1,
		}
	}

	monthly := alloc.AmountUSD
	daily := alloc.DailyAmountUSD
	weekly := alloc.WeeklyAmountUSD

	switch req.PeriodType {
	case model.ExpansionPeriodDaily:
		daily = req.RequestedAmount
	case model.ExpansionPeriodWeekly:
		weekly = req.RequestedAmount
	default: // monthly
		monthly = req.RequestedAmount
	}

	return s.budgetSvc.SetBudget(ctx, req.ScopeType, req.ScopeID, monthly, daily, weekly, approverID)
}

// DecideScoped 按审批人的管辖范围做决策。cancel 仅允许申请人本人取消。
func (s *ExpansionService) DecideScoped(ctx context.Context, requestID string, actorID uuid.UUID, action, comment string, isGlobal bool, deptIDs, studioIDs []string) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return fmt.Errorf("expansion request not found: %w", err)
	}
	if action == model.ExpansionActionCancel {
		if req.RequesterID != actorID {
			return fmt.Errorf("only requester can cancel expansion request")
		}
		return s.Decide(ctx, requestID, actorID, action, comment)
	}
	if !s.canApproveExpansion(req, isGlobal, deptIDs, studioIDs) {
		return fmt.Errorf("无权审批该扩容申请")
	}
	return s.Decide(ctx, requestID, actorID, action, comment)
}

// ListForRequester 查询申请人发起的申请。
func (s *ExpansionService) ListForRequester(ctx context.Context, requesterID string, status string) ([]model.ExpansionRequest, error) {
	return s.repo.ListByRequester(requesterID, status)
}

// ListPending 查询待某级别审批的申请。
func (s *ExpansionService) ListPending(ctx context.Context, level string) ([]model.ExpansionRequest, error) {
	return s.repo.ListPendingForApprover(level)
}

// ListPendingScoped 查询当前审批人管辖范围内的待审批申请。
// requesterName 非空时按申请人姓名模糊过滤。
func (s *ExpansionService) ListPendingScoped(ctx context.Context, level string, isGlobal bool, deptIDs, studioIDs []string, requesterName string) ([]model.ExpansionRequest, error) {
	if isGlobal {
		// 全局管理员不受范围限制，但仍支持姓名过滤
		return s.repo.ListPendingForApproverScoped(level, nil, nil, nil, requesterName)
	}
	memberIDs, err := s.memberIDsForDeptIDs(deptIDs)
	if err != nil {
		return nil, err
	}
	return s.repo.ListPendingForApproverScoped(level, deptIDs, studioIDs, memberIDs, requesterName)
}

// ListSteps 查询某扩容申请的审批流水。
func (s *ExpansionService) ListSteps(ctx context.Context, requestID string) ([]model.ExpansionApprovalStep, error) {
	return s.repo.ListSteps(requestID)
}

func (s *ExpansionService) canApproveExpansion(req *model.ExpansionRequest, isGlobal bool, deptIDs, studioIDs []string) bool {
	if req.CurrentLevel == model.ExpansionLevelDept {
		if isGlobal {
			return true
		}
		if s.budgetSvc == nil {
			return false
		}
		ok, err := s.budgetSvc.MemberInDeptIDs(req.ScopeID, deptIDs)
		return err == nil && ok
	}
	if req.CurrentLevel == model.ExpansionLevelPlatform {
		return isGlobal
	}
	if isGlobal {
		return true
	}
	switch req.ScopeType {
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return containsString(deptIDs, req.ScopeID)
	case model.BudgetScopeStudio:
		return containsString(studioIDs, req.ScopeID)
	default:
		return false
	}
}

func (s *ExpansionService) memberIDsForDeptIDs(deptIDs []string) ([]string, error) {
	if s.budgetSvc == nil {
		return nil, nil
	}
	ids := make([]string, 0)
	for _, deptID := range deptIDs {
		memberIDs, err := s.budgetSvc.ListMemberIDsByDeptID(deptID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, memberIDs...)
	}
	return ids, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
