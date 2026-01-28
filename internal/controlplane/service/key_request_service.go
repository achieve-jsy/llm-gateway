package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrKeyRequestNotFound  = errors.New("key request not found")
	ErrPolicyNotConfigured = errors.New("key creation policy not configured for department")
)

// keyRequestNotifier 站内通知写入（可选注入）。
type keyRequestNotifier interface {
	Create(n *model.Notification) error
}

// AutoApproveItemResult 单条待审批申请的自动通过结果。
type AutoApproveItemResult struct {
	RequestID     string  `json:"request_id"`
	KeyName       string  `json:"key_name"`
	RequesterName string  `json:"requester_name"`
	BudgetMonthly float64 `json:"budget_monthly"`
	Status        string  `json:"status"` // approved | failed
	Error         string  `json:"error,omitempty"`
	CreatedKeyID  string  `json:"created_key_id,omitempty"`
	Notified      bool    `json:"notified"`
}

// AutoApproveResult 关闭审批时批量自动通过的汇总结果。
type AutoApproveResult struct {
	Total   int                     `json:"total"`
	Success int                     `json:"success"`
	Failed  int                     `json:"failed"`
	Items   []AutoApproveItemResult `json:"items"`
}

// SetDeptPolicyResult 设置部门 Key 创建策略的返回。
type SetDeptPolicyResult struct {
	DeptID      string             `json:"dept_id"`
	Policy      string             `json:"policy"`
	AutoApprove *AutoApproveResult `json:"auto_approve,omitempty"`
}

// 供注入的 KeyService 接口（避免循环依赖具体 KeyService 结构体）。
type KeyCreator interface {
	CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time, groupIDs []uint) (*model.ApiKey, string, []BoundGroupItem, error)
	UpdateAccessAllGroups(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error)
}

// KeyRequestService Key 申请创建/审批/列表的核心业务逻辑。
type KeyRequestService struct {
	repo         *repository.KeyRequestRepo
	policyRepo   *repository.DeptPolicyRepo
	settingsRepo SettingsRepository
	keyCreator   KeyCreator
	budgetSvc    *BudgetService
	notifier     keyRequestNotifier
}

func NewKeyRequestService(
	repo *repository.KeyRequestRepo,
	policyRepo *repository.DeptPolicyRepo,
	settingsRepo SettingsRepository,
) *KeyRequestService {
	return &KeyRequestService{
		repo:         repo,
		policyRepo:   policyRepo,
		settingsRepo: settingsRepo,
	}
}

func (s *KeyRequestService) SetKeyCreator(creator KeyCreator)   { s.keyCreator = creator }
func (s *KeyRequestService) SetBudgetService(bs *BudgetService) { s.budgetSvc = bs }
func (s *KeyRequestService) SetNotificationRepo(n keyRequestNotifier) {
	s.notifier = n
}

// RequiresApproval 查询某部门的 Key 创建策略：true = 需要审批，false = 自由创建。
// 优先取部门负责人设置的策略；若未配置则回落 system_configs 全局默认值。
func (s *KeyRequestService) RequiresApproval(ctx context.Context, deptID string) (bool, error) {
	if deptID == "" {
		return s.defaultRequiresApproval(ctx), nil
	}
	p, err := s.policyRepo.GetPolicy(deptID, model.DeptPolicyKeyCreationPolicy)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.defaultRequiresApproval(ctx), nil
		}
		return true, fmt.Errorf("get dept policy: %w", err)
	}
	return p.PolicyValue == model.KeyCreationPolicyApproval, nil
}

func (s *KeyRequestService) defaultRequiresApproval(ctx context.Context) bool {
	if s.settingsRepo == nil {
		return false
	}
	val, err := s.settingsRepo.GetValue("default_key_creation_policy")
	if err != nil {
		return false
	}
	return val == model.KeyCreationPolicyApproval
}

// CreateRequest 由普通用户发起 Key 创建申请。
// requesterName/deptID/deptName 从 user 表查询后由 handler 注入，避免 service 直接调 userRepo。
func (s *KeyRequestService) CreateRequest(
	ctx context.Context,
	requesterID uuid.UUID,
	requesterName, deptID, deptName string,
	keyName string,
	purpose string,
	groupIDs []uint,
	expiresAt *time.Time,
	budgetMonthly float64,
) (*model.KeyRequest, error) {
	gj := ""
	if len(groupIDs) > 0 {
		b, _ := json.Marshal(groupIDs)
		gj = string(b)
	}
	req := &model.KeyRequest{
		RequesterID:   requesterID,
		RequesterName: requesterName,
		DeptID:        deptID,
		DeptName:      deptName,
		KeyName:       keyName,
		KeyType:       model.KeyTypePersonal,
		Purpose:       purpose,
		GroupIDs:      gj,
		ExpiresAt:     expiresAt,
		BudgetMonthly: budgetMonthly,
		Status:        model.KeyRequestStatusPending,
	}
	if err := s.repo.Create(req); err != nil {
		return nil, fmt.Errorf("create key request: %w", err)
	}
	return req, nil
}

// Decide 审批：approve/reject/cancel。
// approve 时调用 KeyService 创建 Key，并按 budget_monthly 创建个人月预算。
func (s *KeyRequestService) Decide(
	ctx context.Context,
	requestID string,
	approverID uuid.UUID,
	approverName string,
	actorID uuid.UUID,
	action string,
	comment string,
	isGlobal bool,
	deptIDs []string,
) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return ErrKeyRequestNotFound
	}
	if req.Status != model.KeyRequestStatusPending {
		return errors.New("key request is not in pending state")
	}

	if action == model.ExpansionActionCancel {
		if req.RequesterID != actorID {
			return errors.New("only requester can cancel")
		}
		return s.repo.Cancel(requestID)
	}

	// approve/reject 仅限有权的部门负责人或全局 admin
	if !s.canApprove(req, isGlobal, deptIDs) {
		return errors.New("无权审批该 Key 申请")
	}

	if action == model.ExpansionActionApprove {
		createdKeyID, err := s.executeApproval(ctx, req, approverID)
		if err != nil {
			return fmt.Errorf("执行审批后操作失败: %w", err)
		}
		if err := s.repo.Approve(requestID, approverID, createdKeyID, approverName, comment); err != nil {
			return err
		}
		s.notifyRequesterApproved(req, false)
		return nil
	}
	if action == model.ExpansionActionReject {
		return s.repo.Reject(requestID, approverID, approverName, comment)
	}
	return fmt.Errorf("invalid action: %s", action)
}

// CancelMine 申请人取消自己的待审批申请。
func (s *KeyRequestService) CancelMine(ctx context.Context, requestID string, requesterID uuid.UUID) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return ErrKeyRequestNotFound
	}
	if req.RequesterID != requesterID {
		return errors.New("只有申请人本人可以取消")
	}
	if req.Status != model.KeyRequestStatusPending {
		return errors.New("只能取消审批中的申请")
	}
	return s.repo.Cancel(requestID)
}

// executeApproval 通过后自动创建 Key，并按 budget_monthly 写个人月预算。
func (s *KeyRequestService) executeApproval(ctx context.Context, req *model.KeyRequest, approverID uuid.UUID) (uuid.UUID, error) {
	if s.keyCreator == nil {
		return uuid.Nil, fmt.Errorf("key creator not configured")
	}
	var groupIDs []uint
	if req.GroupIDs != "" && req.GroupIDs != "[]" {
		_ = json.Unmarshal([]byte(req.GroupIDs), &groupIDs)
	}
	createdKey, _, _, err := s.keyCreator.CreateKey(ctx, req.RequesterID, req.KeyName, req.ExpiresAt, groupIDs)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create key on approval: %w", err)
	}

	// 没选具体模型组（"all" sentinel）→ 授权全部模型组
	if req.GroupIDs == "[]" || (req.GroupIDs == "" && len(groupIDs) == 0) {
		if _, err := s.keyCreator.UpdateAccessAllGroups(ctx, req.RequesterID, createdKey.ID, true); err != nil {
			slog.Warn("key request approval: set access_all_groups failed",
				"request_id", req.ID, "error", err)
		}
	}

	if req.BudgetMonthly > 0 && s.budgetSvc != nil {
		if err := s.budgetSvc.SetBudget(
			ctx,
			model.BudgetScopeMember, req.RequesterID.String(),
			req.BudgetMonthly, -1, -1,
			approverID,
		); err != nil {
			slog.Warn("key request approval: budget SetBudget failed",
				"request_id", req.ID, "budget_monthly", req.BudgetMonthly, "error", err)
		}
	}
	return createdKey.ID, nil
}

func (s *KeyRequestService) canApprove(req *model.KeyRequest, isGlobal bool, deptIDs []string) bool {
	if isGlobal {
		return true
	}
	for _, id := range deptIDs {
		if id == req.DeptID {
			return true
		}
	}
	return false
}

// ListMine 查询当前用户发起的申请（分页）。
func (s *KeyRequestService) ListMine(ctx context.Context, requesterID, status string, page, pageSize int) ([]model.KeyRequest, int64, error) {
	return s.repo.ListByRequesterPaged(requesterID, status, page, pageSize)
}

// ListPending 审批人查询待审批申请（按管辖部门过滤，分页）。
func (s *KeyRequestService) ListPending(ctx context.Context, isGlobal bool, deptIDs []string, requesterName string, hasBudget bool, page, pageSize int) ([]model.KeyRequest, int64, error) {
	return s.repo.ListPendingPaged(isGlobal, deptIDs, requesterName, hasBudget, page, pageSize)
}

// CountMine 查询我的申请数量。
func (s *KeyRequestService) CountMine(ctx context.Context, requesterID string) (pending, approved, rejected int64, err error) {
	pending, err1 := s.repo.CountByStatus(requesterID, model.KeyRequestStatusPending)
	approved, err2 := s.repo.CountByStatus(requesterID, model.KeyRequestStatusApproved)
	rejected, err3 := s.repo.CountByStatus(requesterID, model.KeyRequestStatusRejected)
	if err1 != nil { return 0, 0, 0, err1 }
	if err2 != nil { return 0, 0, 0, err2 }
	return pending, approved, rejected, err3
}

// CountPendingForDept 统计管辖范围内待审批的申请数量。
func (s *KeyRequestService) CountPendingForDept(ctx context.Context, isGlobal bool, deptIDs []string) (int64, int64, error) {
	return s.repo.CountPendingScoped(isGlobal, deptIDs)
}

// ListHistoryForApprover 查询当前审批人已处理的申请（分页）。
func (s *KeyRequestService) ListHistoryForApprover(ctx context.Context, isGlobal bool, deptIDs []string, requesterName, status string, page, pageSize int) ([]model.KeyRequest, int64, error) {
	return s.repo.ListHistoryPaged(isGlobal, deptIDs, requesterName, status, page, pageSize)
}

// SetDeptPolicy 设置部门 Key 创建策略。
// 当策略从 approval 切换为 free 时，自动批量通过该部门所有 pending 申请。
func (s *KeyRequestService) SetDeptPolicy(ctx context.Context, deptID, policyValue string, updatedBy uuid.UUID, updatedByName string) (*SetDeptPolicyResult, error) {
	result := &SetDeptPolicyResult{DeptID: deptID, Policy: policyValue}
	if policyValue != model.KeyCreationPolicyFree && policyValue != model.KeyCreationPolicyApproval {
		return nil, fmt.Errorf("invalid policy value: %s", policyValue)
	}

	oldValue := model.KeyCreationPolicyFree
	if p, err := s.policyRepo.GetPolicy(deptID, model.DeptPolicyKeyCreationPolicy); err == nil && p != nil {
		oldValue = p.PolicyValue
	}

	if oldValue == model.KeyCreationPolicyApproval && policyValue == model.KeyCreationPolicyFree {
		autoResult, err := s.BatchApprovePendingForDept(ctx, deptID, updatedBy, updatedByName)
		if err != nil {
			slog.Warn("batch approve on policy downgrade failed", "dept_id", deptID, "error", err)
		} else if autoResult != nil && autoResult.Total > 0 {
			result.AutoApprove = autoResult
			slog.Info("key request policy changed to free, auto-approved pending requests",
				"dept_id", deptID, "success", autoResult.Success, "failed", autoResult.Failed, "approver_id", updatedBy)
		}
	}

	if err := s.policyRepo.Upsert(deptID, model.DeptPolicyKeyCreationPolicy, policyValue, updatedBy); err != nil {
		return result, fmt.Errorf("upsert dept policy: %w", err)
	}
	return result, nil
}

// BatchApprovePendingForDept 批量通过某部门所有 pending 的 Key 申请。
// 单条失败不阻塞其余，返回逐条结果。成功项会向申请人发送站内通知。
func (s *KeyRequestService) BatchApprovePendingForDept(ctx context.Context, deptID string, approverID uuid.UUID, approverName string) (*AutoApproveResult, error) {
	reqs, err := s.repo.ListPendingByDept(deptID)
	if err != nil {
		return nil, fmt.Errorf("list pending requests: %w", err)
	}
	out := &AutoApproveResult{
		Total: len(reqs),
		Items: make([]AutoApproveItemResult, 0, len(reqs)),
	}
	for _, req := range reqs {
		item := AutoApproveItemResult{
			RequestID:     req.ID.String(),
			KeyName:       req.KeyName,
			RequesterName: req.RequesterName,
			BudgetMonthly: req.BudgetMonthly,
		}
		createdKeyID, err := s.executeApproval(ctx, &req, approverID)
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			out.Failed++
			out.Items = append(out.Items, item)
			slog.Warn("batch approve: create key failed, skipping",
				"request_id", req.ID, "requester_id", req.RequesterID, "error", err)
			continue
		}
		if err := s.repo.Approve(req.ID.String(), approverID, createdKeyID, approverName, "部门已关闭 Key 审批，系统自动通过"); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			out.Failed++
			out.Items = append(out.Items, item)
			slog.Warn("batch approve: update status failed",
				"request_id", req.ID, "error", err)
			continue
		}
		item.Status = "approved"
		item.CreatedKeyID = createdKeyID.String()
		item.Notified = s.notifyRequesterApproved(&req, true)
		out.Success++
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (s *KeyRequestService) notifyRequesterApproved(req *model.KeyRequest, auto bool) bool {
	if s.notifier == nil || req == nil {
		return false
	}
	title := "Key 申请已通过"
	content := fmt.Sprintf("你的 Key「%s」申请已通过，Key 已创建，可在「我的密钥」中查看。", req.KeyName)
	if auto {
		content = fmt.Sprintf("负责人已关闭 Key 审批，你的「%s」申请已自动通过，Key 已创建，可在「我的密钥」中查看。", req.KeyName)
	}
	n := &model.Notification{
		UserID:  req.RequesterID,
		Type:    model.NotificationTypeKeyRequestApproved,
		Level:   model.NotificationLevelInfo,
		Title:   title,
		Content: content,
		RefType: "key_request",
		RefID:   req.ID.String(),
	}
	if err := s.notifier.Create(n); err != nil {
		slog.Warn("key request: notify requester failed",
			"request_id", req.ID, "requester_id", req.RequesterID, "error", err)
		return false
	}
	return true
}

// GetDeptPolicy 查询部门 Key 创建策略；若未配置返回空字符串。
func (s *KeyRequestService) GetDeptPolicy(ctx context.Context, deptID string) (string, error) {
	p, err := s.policyRepo.GetPolicy(deptID, model.DeptPolicyKeyCreationPolicy)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return p.PolicyValue, nil
}
