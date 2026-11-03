package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrExpansionForbidden           = errors.New("expansion scope is outside actor visibility")
	ErrExpansionTargetRequired      = errors.New("expansion target amount is required")
	ErrExpansionTargetNotRaised     = errors.New("expansion target must exceed current limit")
	ErrExpansionUsageUnavailable    = errors.New("budget usage is unavailable")
	ErrNotInApprovalPool            = errors.New("actor is not in current approval pool")
	ErrApprovalPoolEmpty            = errors.New("current approval pool is empty")
	ErrPrecheckStale                = errors.New("expansion precheck is stale")
	ErrExpansionUsedExceedsTarget   = errors.New("current usage exceeds expansion target")
	ErrExpansionRequestExpired      = errors.New("expansion request has expired")
	ErrExpansionTerminalConflict    = errors.New("terminal expansion request cannot be changed")
	ErrDecisionCommentRequired      = errors.New("decision comment is required")
	ErrConfigSyncPending            = errors.New("budget config sync pending")
	ErrExpansionDurationUnsupported = errors.New("invalid expansion duration")
)

type ApprovalPoolResolver interface {
	Resolve(context.Context, *model.ExpansionRequest) ([]uuid.UUID, error)
	ResolveScopeOwnerRecipients(context.Context, string, string) ([]uuid.UUID, error)
	ResolveParentAdmins(context.Context, *model.ExpansionRequest) ([]uuid.UUID, error)
}

type DecisionInput struct {
	Action          string `json:"action"`
	Comment         string `json:"comment"`
	PrecheckVersion string `json:"precheck_version"`
}

type UpdateExpansionInput struct {
	TargetAmountUSD string
	Reason          string
	UrgentReason    string
}

type ApplyResult struct {
	RequestID      uuid.UUID `json:"request_id"`
	TargetType     string    `json:"target_type"`
	TargetID       uuid.UUID `json:"target_id"`
	AppliedAt      time.Time `json:"applied_at"`
	SyncStatus     string    `json:"sync_status"`
	AlreadyApplied bool      `json:"already_applied"`
}

type ExpansionCapabilities struct {
	CanEdit       bool `json:"can_edit"`
	CanCancel     bool `json:"can_cancel"`
	CanDecide     bool `json:"can_decide"`
	CanReprecheck bool `json:"can_reprecheck"`
}

type ExpansionDetail struct {
	Request           *model.ExpansionRequest       `json:"request"`
	SubmittedSnapshot map[string]string             `json:"submitted_snapshot"`
	LiveSnapshot      *PrecheckResult               `json:"live_snapshot"`
	ParentConstraints []ConstraintView              `json:"parent_constraints"`
	ApprovalPool      []string                      `json:"approval_pool"`
	AffectedGroups    []AffectedGroup               `json:"affected_groups"`
	Steps             []model.ExpansionApprovalStep `json:"steps"`
	Capabilities      ExpansionCapabilities         `json:"capabilities"`
	DataAsOf          time.Time                     `json:"data_as_of"`
}

type NotifyOwnerInput struct {
	ScopeType     string  `json:"scope_type"`
	ScopeID       string  `json:"scope_id"`
	PeriodType    string  `json:"period_type"`
	DimensionType string  `json:"dimension_type"`
	ModelTierID   *string `json:"model_tier_id,omitempty"`
	Message       string  `json:"message"`
}

type ActorScope struct {
	UserID    string
	Role      string
	IsGlobal  bool
	DeptIDs   []string
	StudioIDs []string
}

type expansionApprovalAllocationStore interface {
	GetAllocationForUpdate(context.Context, *gorm.DB, string, string) (*model.BudgetAllocation, error)
	ResolveBudgetChainForUpdate(context.Context, *gorm.DB, string, string) ([]repository.MyBudgetScopeAllocation, error)
}

func (s *ExpansionService) lockApprovalAllocations(
	ctx context.Context,
	tx *gorm.DB,
	request *model.ExpansionRequest,
	store expansionApprovalAllocationStore,
) (*model.BudgetAllocation, []repository.MyBudgetScopeAllocation, error) {
	chain, err := store.ResolveBudgetChainForUpdate(ctx, tx, request.ScopeType, request.ScopeID)
	if err != nil {
		return nil, nil, err
	}
	for index := range chain {
		if chain[index].Allocation.ScopeType == request.ScopeType && chain[index].Allocation.ScopeID == request.ScopeID {
			return &chain[index].Allocation, chain, nil
		}
	}
	return nil, nil, ErrBudgetAllocationNotFound
}

type PrecheckInput struct {
	ScopeType       string  `json:"scope_type"`
	ScopeID         string  `json:"scope_id"`
	PeriodType      string  `json:"period_type"`
	DimensionType   string  `json:"dimension_type"`
	ModelTierID     *string `json:"model_tier_id,omitempty"`
	DurationType    string  `json:"duration_type"`
	Priority        string  `json:"priority"`
	TargetAmountUSD string  `json:"target_amount_usd"`
}

type BlockingReason struct {
	Code        string `json:"code"`
	SourceLevel string `json:"source_level"`
	ScopeType   string `json:"scope_type,omitempty"`
	ScopeID     string `json:"scope_id,omitempty"`
	LimitUSD    string `json:"limit_usd,omitempty"`
	Status      string `json:"status"`
	ResetAt     string `json:"reset_at,omitempty"`
}

type ConstraintView struct {
	AllocationID string `json:"allocation_id,omitempty"`
	Version      string `json:"version,omitempty"`
	SourceLevel  string `json:"source_level"`
	ScopeType    string `json:"scope_type,omitempty"`
	ScopeID      string `json:"scope_id,omitempty"`
	LimitUSD     string `json:"limit_usd,omitempty"`
	UsedUSD      string `json:"used_usd,omitempty"`
	Status       string `json:"status"`
	Freshness    string `json:"freshness"`
	TierStatus   string `json:"tier_status,omitempty"`
	ResetAt      string `json:"reset_at,omitempty"`
}

type AffectedGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ScopeOption struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type PrecheckResult struct {
	CanSubmit         bool             `json:"can_submit"`
	CanNotifyOwner    bool             `json:"can_notify_owner"`
	CurrentLimitUSD   string           `json:"current_limit_usd"`
	CurrentUsedUSD    string           `json:"current_used_usd"`
	TargetAmountUSD   string           `json:"target_amount_usd"`
	IncreaseUSD       string           `json:"increase_usd"`
	EffectiveAfterUSD string           `json:"effective_after_usd"`
	PrecheckVersion   string           `json:"precheck_version"`
	DataAsOf          time.Time        `json:"data_as_of"`
	Stale             bool             `json:"stale"`
	BlockingReason    *BlockingReason  `json:"blocking_reason,omitempty"`
	ParentConstraints []ConstraintView `json:"parent_constraints"`
	AffectedGroups    []AffectedGroup  `json:"affected_groups"`
	AffectedMembers   int              `json:"affected_members"`
	VisibleScopes     []ScopeOption    `json:"visible_scopes"`
}

func (s *ExpansionService) VisibleScopeTypes(role string) []string {
	switch role {
	case "admin":
		return []string{model.BudgetScopeCompany, model.BudgetScopeStudio, model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
	case "studio_admin":
		return []string{model.BudgetScopeStudio, model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
	case "dept_admin":
		return []string{model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
	default:
		return []string{model.BudgetScopeMember}
	}
}

func (s *ExpansionService) Precheck(ctx context.Context, actor ActorScope, input PrecheckInput) (*PrecheckResult, error) {
	if strings.TrimSpace(input.DurationType) == "" {
		input.DurationType = string(budgetdomain.DurationPermanent)
	}
	if err := validatePrecheckInput(actor, input, s.VisibleScopeTypes(actor.Role)); err != nil {
		return nil, err
	}
	if err := s.validateActorScope(ctx, actor, input.ScopeType, input.ScopeID); err != nil {
		return nil, err
	}
	target, err := budgetdomain.ParseAmount(input.TargetAmountUSD)
	if err != nil {
		return nil, ErrExpansionTargetRequired
	}
	now := time.Now().UTC()
	current, used, stale, err := s.readExpansionSnapshot(ctx, input, now)
	if err != nil {
		return nil, err
	}
	increase := target.Decimal.Sub(current.Decimal)
	result := &PrecheckResult{
		CanSubmit:         !stale && increase.IsPositive(),
		CurrentLimitUSD:   current.String(),
		CurrentUsedUSD:    used.String(),
		TargetAmountUSD:   target.String(),
		IncreaseUSD:       fixedAmount(increase),
		EffectiveAfterUSD: target.String(),
		DataAsOf:          now,
		Stale:             stale,
		AffectedGroups:    []AffectedGroup{},
		ParentConstraints: []ConstraintView{},
	}
	for _, scopeType := range s.VisibleScopeTypes(actor.Role) {
		result.VisibleScopes = append(result.VisibleScopes, ScopeOption{Type: scopeType})
	}
	parentConstraints, blockingReason, err := s.parentConstraints(ctx, input, target, now)
	if err != nil {
		if !isMissingBudgetAllocation(err) {
			return nil, err
		}
		parentConstraints, blockingReason = []ConstraintView{}, nil
	}
	result.ParentConstraints, result.BlockingReason = parentConstraints, blockingReason
	if s.budgetSvc != nil {
		if projector, ok := s.budgetSvc.repo.(interface {
			ResolveBudgetImpact(context.Context, string, string, *string, int) (int, []repository.BudgetAffectedGroup, error)
		}); ok {
			count, groups, projectErr := projector.ResolveBudgetImpact(ctx, input.ScopeType, input.ScopeID, input.ModelTierID, 20)
			if projectErr != nil {
				return nil, projectErr
			}
			result.AffectedMembers = count
			for _, group := range groups {
				result.AffectedGroups = append(result.AffectedGroups, AffectedGroup{ID: group.ID, Name: group.Name})
			}
		} else {
			return nil, fmt.Errorf("budget impact projector not configured")
		}
	}
	if blockingReason != nil {
		result.CanNotifyOwner = true
		if actor.Role == "user" {
			result.BlockingReason = nil
			result.ParentConstraints = []ConstraintView{}
			result.AffectedGroups = []AffectedGroup{}
		} else {
			result.CanSubmit = false
		}
	}
	if !increase.IsPositive() && result.BlockingReason == nil {
		result.BlockingReason = &BlockingReason{Code: "TARGET_NOT_ABOVE_CURRENT", SourceLevel: scopeLevel(input.ScopeType), Status: "blocked"}
	}
	result.PrecheckVersion = makePrecheckVersion(input, current, used, parentConstraints, now)
	if actor.Role == "user" {
		result.ParentConstraints = []ConstraintView{}
		result.AffectedGroups = []AffectedGroup{}
	}
	return result, nil
}

func isMissingBudgetAllocation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrBudgetAllocationNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "resolve budget allocation") && strings.Contains(message, "record not found")
}

func (s *ExpansionService) AssertCanDecide(ctx context.Context, requestID, actorID string) error {
	if s.approvalPool == nil {
		return nil
	}
	request, err := s.repo.FindByID(requestID)
	if err != nil {
		return err
	}
	pool, err := s.approvalPool.Resolve(ctx, request)
	if err != nil {
		return err
	}
	if len(pool) == 0 {
		s.notifyApprovalPoolEmpty(ctx, request)
		return ErrApprovalPoolEmpty
	}
	actorUUID, err := uuid.Parse(actorID)
	if err != nil {
		return ErrNotInApprovalPool
	}
	for _, candidate := range pool {
		if candidate == actorUUID {
			return nil
		}
	}
	return ErrNotInApprovalPool
}

func (s *ExpansionService) assertCanDecideInTx(
	ctx context.Context,
	tx *gorm.DB,
	request *model.ExpansionRequest,
	actorID uuid.UUID,
) error {
	if s.approvalPool == nil {
		return nil
	}
	var (
		pool []uuid.UUID
		err  error
	)
	if resolver, ok := s.approvalPool.(interface {
		ResolveInTx(context.Context, *gorm.DB, *model.ExpansionRequest) ([]uuid.UUID, error)
	}); ok {
		pool, err = resolver.ResolveInTx(ctx, tx, request)
	} else {
		pool, err = s.approvalPool.Resolve(ctx, request)
	}
	if err != nil {
		return err
	}
	if len(pool) == 0 {
		return ErrApprovalPoolEmpty
	}
	for _, candidate := range pool {
		if candidate == actorID {
			return nil
		}
	}
	return ErrNotInApprovalPool
}

func enqueueExpansionNotificationInTx(tx *gorm.DB, request *model.ExpansionRequest, userID uuid.UUID, event, title, content string, data map[string]interface{}) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(&model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "expansion:" + request.ID.String() + ":" + event + ":" + userID.String(),
		UserID: userID, Type: event, Level: model.NotificationLevelInfo, Title: title, Content: content,
		RefType: "expansion_request", RefID: request.ID.String(), Status: "pending", NextAttempt: time.Now().UTC(),
		TemplateData: data,
	}).Error
}

func ensureRequestAllocationInTx(ctx context.Context, tx *gorm.DB, request *model.ExpansionRequest, actorID uuid.UUID) error {
	if request == nil || request.ScopeType == "" || request.ScopeID == "" {
		return ErrBudgetAllocationNotFound
	}
	now := time.Now().UTC()
	allocation := model.BudgetAllocation{
		ID: uuid.New(), ScopeType: request.ScopeType, ScopeID: request.ScopeID,
		AmountUSD: 0, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		Period: "monthly", Enabled: true,
		CreatedBy: actorID, UpdatedBy: actorID,
		CreatedAt: now, UpdatedAt: now,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}},
		DoNothing: true,
	}).Create(&allocation).Error
}

func requestAllocationMissingInTx(ctx context.Context, tx *gorm.DB, request *model.ExpansionRequest) (bool, error) {
	if request == nil || request.ScopeType == "" || request.ScopeID == "" {
		return false, ErrBudgetAllocationNotFound
	}
	var count int64
	if err := tx.WithContext(ctx).Model(&model.BudgetAllocation{}).
		Where("scope_type = ? AND scope_id = ? AND enabled = true", request.ScopeType, request.ScopeID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *ExpansionService) rejectInTx(ctx context.Context, tx *gorm.DB, request *model.ExpansionRequest, actorID uuid.UUID, comment string) error {
	next, err := model.NextExpansionStatus(request.Status, model.ExpansionActionReject)
	if err != nil {
		return err
	}
	if err := s.assertCanDecideInTx(ctx, tx, request, actorID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Model(request).Update("status", next).Error; err != nil {
		return err
	}
	if err := s.repo.CreateStepInTx(ctx, tx, &model.ExpansionApprovalStep{
		ID: uuid.New(), RequestID: request.ID, Level: request.CurrentLevel,
		ApproverID: &actorID, Action: model.ExpansionActionReject, Comment: comment,
	}); err != nil {
		return err
	}
	return enqueueExpansionNotificationInTx(tx, request, request.RequesterID, model.NotificationTypeExpansionRejected, "扩容申请已驳回", comment,
		map[string]interface{}{"ExpansionID": request.ID.String(), "Reason": comment})
}

func (s *ExpansionService) DecideWorkflow(
	ctx context.Context,
	requestID string,
	actorID uuid.UUID,
	input DecisionInput,
) (*ApplyResult, error) {
	if (input.Action == model.ExpansionActionReject || input.Action == model.ExpansionActionEscalate) &&
		strings.TrimSpace(input.Comment) == "" {
		return nil, ErrDecisionCommentRequired
	}
	if input.Action != model.ExpansionActionCancel {
		if err := s.AssertCanDecide(ctx, requestID, actorID.String()); err != nil {
			return nil, err
		}
	}
	id, err := uuid.Parse(requestID)
	if err != nil {
		return nil, err
	}
	if input.Action != model.ExpansionActionApprove {
		return s.applyNonApprovalDecision(ctx, id, actorID, input)
	}
	return s.approveWorkflow(ctx, id, actorID, input)
}

func (s *ExpansionService) applyNonApprovalDecision(
	ctx context.Context,
	requestID, actorID uuid.UUID,
	input DecisionInput,
) (*ApplyResult, error) {
	var result ApplyResult
	if input.Action == model.ExpansionActionCancel {
		err := s.repo.WithLockedRequest(ctx, requestID, func(tx *gorm.DB, request *model.ExpansionRequest) error {
			if request.RequesterID != actorID {
				return ErrExpansionForbidden
			}
			if request.Status == model.ExpansionStatusCancelled {
				return nil
			}
			next, err := model.NextExpansionStatus(request.Status, input.Action)
			if err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Model(request).Update("status", next).Error; err != nil {
				return err
			}
			return s.repo.CreateStepInTx(ctx, tx, &model.ExpansionApprovalStep{
				ID: uuid.New(), RequestID: request.ID, Level: request.CurrentLevel,
				ApproverID: &actorID, Action: input.Action, Comment: input.Comment,
			})
		})
		return &result, err
	}
	candidate, err := s.repo.FindByID(requestID.String())
	if err != nil {
		return nil, err
	}
	allocationStore, ok := s.budgetSvc.repo.(expansionApprovalAllocationStore)
	if !ok {
		return nil, fmt.Errorf("budget repository does not support transactional decision")
	}
	stale := false
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		allocation, chain, err := s.lockApprovalAllocations(ctx, tx, candidate, allocationStore)
		if isMissingBudgetAllocation(err) && input.Action == model.ExpansionActionReject {
			request, lockErr := s.repo.FindByIDForUpdate(ctx, tx, requestID)
			if lockErr != nil {
				return lockErr
			}
			return s.rejectInTx(ctx, tx, request, actorID, input.Comment)
		}
		if err != nil {
			return err
		}
		if allocation == nil {
			return ErrBudgetAllocationNotFound
		}
		request, err := s.repo.FindByIDForUpdate(ctx, tx, requestID)
		if err != nil {
			return err
		}
		if err := s.assertCanDecideInTx(ctx, tx, request, actorID); err != nil {
			return err
		}
		latest, err := s.precheckApprovalInTx(ctx, tx, request, allocation, chain, s.clock().UTC())
		if err != nil {
			return err
		}
		if latest.Stale || latest.PrecheckVersion != request.PrecheckVersion || latest.PrecheckVersion != input.PrecheckVersion {
			current, _ := budgetdomain.ParseAmount(latest.CurrentLimitUSD)
			used, _ := budgetdomain.ParseAmount(latest.CurrentUsedUSD)
			if err := s.repo.MarkPrecheckRequired(ctx, tx, request.ID, latest.PrecheckVersion, current, used, latest.DataAsOf); err != nil {
				return err
			}
			stale = true
			return nil
		}
		next, err := model.NextExpansionStatus(request.Status, input.Action)
		if err != nil {
			return err
		}
		level := request.CurrentLevel
		if input.Action == model.ExpansionActionEscalate {
			if level != model.ExpansionLevelStudio {
				return fmt.Errorf("can only escalate from studio level")
			}
			level = model.ExpansionLevelPlatform
		}
		if err := tx.WithContext(ctx).Model(request).Updates(map[string]any{
			"status": next, "current_level": level,
		}).Error; err != nil {
			return err
		}
		if err := s.repo.CreateStepInTx(ctx, tx, &model.ExpansionApprovalStep{
			ID: uuid.New(), RequestID: request.ID, Level: request.CurrentLevel,
			ApproverID: &actorID, Action: input.Action, Comment: input.Comment,
		}); err != nil {
			return err
		}
		event, title := model.NotificationTypeExpansionDecision, "扩容申请状态更新"
		if input.Action == model.ExpansionActionReject {
			event, title = model.NotificationTypeExpansionRejected, "扩容申请已驳回"
		}
		if input.Action == model.ExpansionActionEscalate {
			title = "扩容申请已升级审批"
		}
		return enqueueExpansionNotificationInTx(tx, request, request.RequesterID, event, title, input.Comment,
			map[string]interface{}{"ExpansionID": request.ID.String(), "Decision": input.Action, "Reason": input.Comment})
	})
	if stale {
		return nil, ErrPrecheckStale
	}
	return &result, err
}

func (s *ExpansionService) approveWorkflow(
	ctx context.Context,
	requestID, actorID uuid.UUID,
	input DecisionInput,
) (*ApplyResult, error) {
	result := &ApplyResult{RequestID: requestID}
	stale := false
	candidate, err := s.repo.FindByID(requestID.String())
	if err != nil {
		return nil, err
	}
	allocationStore, ok := s.budgetSvc.repo.(expansionApprovalAllocationStore)
	if !ok {
		return nil, fmt.Errorf("budget repository does not support transactional approval")
	}
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		createdMissingAllocation, err := requestAllocationMissingInTx(ctx, tx, candidate)
		if err != nil {
			return err
		}
		allocation, chain, err := s.lockApprovalAllocations(ctx, tx, candidate, allocationStore)
		if isMissingBudgetAllocation(err) {
			if createErr := ensureRequestAllocationInTx(ctx, tx, candidate, actorID); createErr != nil {
				return createErr
			}
			createdMissingAllocation = true
			allocation, chain, err = s.lockApprovalAllocations(ctx, tx, candidate, allocationStore)
		}
		if err != nil {
			return err
		}
		if allocation == nil {
			return ErrBudgetAllocationNotFound
		}
		request, err := s.repo.FindByIDForUpdate(ctx, tx, requestID)
		if err != nil {
			return err
		}
		if request.ScopeType != candidate.ScopeType || request.ScopeID != candidate.ScopeID {
			return ErrPrecheckStale
		}
		if err := s.assertCanDecideInTx(ctx, tx, request, actorID); err != nil {
			return err
		}
		if request.Status == model.ExpansionStatusApproved && request.AppliedAt != nil && request.AppliedTargetID != nil {
			result.TargetType, result.TargetID, result.AppliedAt = request.AppliedTargetType, *request.AppliedTargetID, *request.AppliedAt
			result.SyncStatus, result.AlreadyApplied = request.SyncStatus, true
			return nil
		}
		now := s.clock().UTC()
		if request.Status == model.ExpansionStatusPendingReconfirmation || request.Status == model.ExpansionStatusPrecheckRequired {
			return ErrPrecheckStale
		}
		if request.Status != model.ExpansionStatusPending || request.ExpiresAt == nil || !request.ExpiresAt.After(now) {
			return ErrExpansionRequestExpired
		}
		latest, err := s.precheckApprovalInTx(ctx, tx, request, allocation, chain, now)
		if err != nil {
			return err
		}
		target, _ := budgetdomain.ParseAmount(latest.TargetAmountUSD)
		used, _ := budgetdomain.ParseAmount(latest.CurrentUsedUSD)
		if used.Decimal.GreaterThan(target.Decimal) {
			return ErrExpansionUsedExceedsTarget
		}
		precheckVersionChanged := latest.PrecheckVersion != input.PrecheckVersion || latest.PrecheckVersion != request.PrecheckVersion
		if createdMissingAllocation && request.PrecheckVersion == input.PrecheckVersion && !latest.Stale && latest.BlockingReason == nil {
			precheckVersionChanged = false
		}
		if precheckVersionChanged || latest.Stale {
			current, _ := budgetdomain.ParseAmount(latest.CurrentLimitUSD)
			if err := s.repo.MarkPrecheckRequired(ctx, tx, request.ID, latest.PrecheckVersion, current, used, latest.DataAsOf); err != nil {
				return err
			}
			stale = true
			return nil
		}
		targetAmount, _ := budgetdomain.ParseAmount(fmt.Sprintf("%.6f", request.RequestedAmount))
		targetID := uuid.New()
		targetType := "temporary_override"
		if request.DurationType == budgetdomain.DurationCurrentPeriod {
			window, resolveErr := budgetperiod.Resolve(now, budgetdomain.PeriodType(request.PeriodType), budgetperiod.Config{
				Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
			})
			if resolveErr != nil {
				return resolveErr
			}
			override := model.BudgetTemporaryOverride{
				ID: uuid.New(), BudgetAllocationID: allocation.ID,
				DimensionType: request.DimensionType, DimensionKey: request.DimensionKey,
				ModelTierID: request.ModelTierID, PeriodType: budgetdomain.PeriodType(request.PeriodType),
				TargetLimitUSD: targetAmount, StartsAt: now, ExpiresAt: window.ResetAt,
				ExpansionRequestID: &request.ID, Status: budgetdomain.OverrideActive,
			}
			if err := tx.WithContext(ctx).Model(&model.BudgetTemporaryOverride{}).
				Where("budget_allocation_id = ? AND dimension_key = ? AND period_type = ? AND status = ?",
					allocation.ID, request.DimensionKey, request.PeriodType, budgetdomain.OverrideActive).
				Updates(map[string]any{"status": budgetdomain.OverrideCancelled, "updated_at": now, "failure_reason": "replaced by expansion approval"}).Error; err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Create(&override).Error; err != nil {
				return err
			}
			targetID = override.ID
		} else if request.DimensionType == budgetdomain.DimensionTier && request.ModelTierID != nil {
			if err := repository.UpsertTierLimitInTx(ctx, tx, repository.UpsertTierLimitParams{
				BudgetAllocationID: allocation.ID, ModelTierID: *request.ModelTierID,
				PeriodType: budgetdomain.PeriodType(request.PeriodType), LimitUSD: targetAmount,
				EffectiveAt: now, Enabled: true,
			}); err != nil {
				return err
			}
			var limit model.BudgetTierLimit
			if err := tx.WithContext(ctx).Where(
				"budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?",
				allocation.ID, *request.ModelTierID, request.PeriodType,
			).First(&limit).Error; err != nil {
				return err
			}
			targetID = limit.ID
			targetType = "tier_base_limit"
		} else {
			updates := totalLimitUpdate(request.PeriodType, request.RequestedAmount)
			if err := tx.WithContext(ctx).Model(&model.BudgetAllocation{}).
				Where("id = ?", allocation.ID).Updates(updates).Error; err != nil {
				return err
			}
			targetID = allocation.ID
			targetType = "total_base_limit"
		}
		if err := s.repo.MarkApplied(ctx, tx, request, targetType, targetID, now); err != nil {
			return err
		}
		if err := s.repo.CreateStepInTx(ctx, tx, &model.ExpansionApprovalStep{
			ID: uuid.New(), RequestID: request.ID, Level: request.CurrentLevel,
			ApproverID: &actorID, Action: model.ExpansionActionApprove, Comment: input.Comment,
		}); err != nil {
			return err
		}
		if err := enqueueExpansionNotificationInTx(tx, request, request.RequesterID, model.NotificationTypeExpansionApproved, "扩容申请已批准并生效", request.Reason,
			map[string]interface{}{"ExpansionID": request.ID.String(), "Reason": request.Reason, "AmountUSD": fmt.Sprintf("%.2f", request.RequestedAmount)}); err != nil {
			return err
		}
		result.TargetType, result.TargetID, result.AppliedAt, result.SyncStatus = targetType, targetID, now, "pending"
		return nil
	})
	if err != nil {
		recordExpansionApply(ctx, string(candidate.DimensionType), string(candidate.DurationType), "failed")
		return nil, err
	}
	if stale {
		recordExpansionApply(ctx, string(candidate.DimensionType), string(candidate.DurationType), "stale")
		return nil, ErrPrecheckStale
	}
	if result.AlreadyApplied {
		recordExpansionApply(ctx, string(candidate.DimensionType), string(candidate.DurationType), "idempotent")
		return result, nil
	}
	if s.snapshotPublisher != nil {
		if err := s.snapshotPublisher.MarkDirtyAndPublish(ctx); err != nil {
			_ = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
				if updateErr := tx.WithContext(ctx).Model(&model.ExpansionRequest{}).Where("id = ?", requestID).Update("sync_status", "failed").Error; updateErr != nil {
					return updateErr
				}
				return enqueueExpansionNotificationInTx(tx, candidate, candidate.RequesterID, model.NotificationTypeExpansionApplyFailed, "扩容申请生效失败", err.Error(),
					map[string]interface{}{"ExpansionID": candidate.ID.String(), "Error": err.Error()})
			})
			result.SyncStatus = "failed"
			recordExpansionApply(ctx, string(candidate.DimensionType), string(candidate.DurationType), "sync_failed")
			return result, ErrConfigSyncPending
		}
	}
	_ = s.repo.UpdateSyncStatus(ctx, requestID, "synced")
	result.SyncStatus = "synced"
	recordExpansionApply(ctx, string(candidate.DimensionType), string(candidate.DurationType), "success")
	return result, nil
}

func (s *ExpansionService) Reprecheck(ctx context.Context, actor ActorScope, requestID string) (*ExpansionDetail, error) {
	id, err := uuid.Parse(requestID)
	if err != nil {
		return nil, err
	}
	err = s.repo.WithLockedRequest(ctx, id, func(tx *gorm.DB, request *model.ExpansionRequest) error {
		if err := s.validateActorScope(ctx, actor, request.ScopeType, request.ScopeID); err != nil {
			return err
		}
		if request.Status != model.ExpansionStatusPending &&
			request.Status != model.ExpansionStatusPrecheckRequired &&
			request.Status != model.ExpansionStatusPendingReconfirmation {
			return ErrExpansionTerminalConflict
		}
		result, precheckErr := s.Precheck(ctx, actor, precheckInputFromRequest(request))
		if precheckErr != nil {
			return precheckErr
		}
		current, _ := budgetdomain.ParseAmount(result.CurrentLimitUSD)
		used, _ := budgetdomain.ParseAmount(result.CurrentUsedUSD)
		return tx.WithContext(ctx).Model(request).Updates(map[string]any{
			"status": model.ExpansionStatusPending, "precheck_version": result.PrecheckVersion,
			"prechecked_at": result.DataAsOf, "current_amount_snapshot": current,
			"used_amount_snapshot": used,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetDetail(ctx, actor, requestID)
}

func (s *ExpansionService) UpdateRequest(
	ctx context.Context,
	requestID, requesterID string,
	input UpdateExpansionInput,
) (*model.ExpansionRequest, error) {
	target, err := budgetdomain.ParseAmount(input.TargetAmountUSD)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrExpansionReasonRequired
	}
	id, err := uuid.Parse(requestID)
	if err != nil {
		return nil, err
	}
	err = s.repo.WithLockedRequest(ctx, id, func(tx *gorm.DB, request *model.ExpansionRequest) error {
		if request.RequesterID.String() != requesterID {
			return ErrExpansionForbidden
		}
		if request.Status != model.ExpansionStatusPending && request.Status != model.ExpansionStatusPrecheckRequired &&
			request.Status != model.ExpansionStatusPendingReconfirmation {
			return ErrExpansionTerminalConflict
		}
		if request.Priority == model.ExpansionPriorityUrgent && strings.TrimSpace(input.UrgentReason) == "" {
			return ErrExpansionReasonRequired
		}
		targetFloat, _ := target.Decimal.Float64()
		level := request.CurrentLevel
		if targetFloat > request.RequestedAmount {
			level = initialApprovalLevel(request.ScopeType)
		}
		return tx.WithContext(ctx).Model(request).Updates(map[string]any{
			"requested_amount": targetFloat, "reason": input.Reason, "urgent_reason": input.UrgentReason,
			"status": model.ExpansionStatusPrecheckRequired, "current_level": level,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(requestID)
}

func (s *ExpansionService) GetDetail(
	ctx context.Context,
	actor ActorScope,
	requestID string,
) (*ExpansionDetail, error) {
	request, err := s.repo.FindByID(requestID)
	if err != nil {
		return nil, err
	}
	if err := s.validateActorScope(ctx, actor, request.ScopeType, request.ScopeID); err != nil {
		return nil, err
	}
	live, err := s.Precheck(ctx, actorForDetail(actor, request), precheckInputFromRequest(request))
	if err != nil && !errors.Is(err, ErrExpansionUsageUnavailable) {
		return nil, err
	}
	steps, err := s.repo.ListSteps(requestID)
	if err != nil {
		return nil, err
	}
	pool := []string{}
	canDecide := false
	if s.approvalPool != nil {
		recipients, resolveErr := s.approvalPool.Resolve(ctx, request)
		if resolveErr != nil {
			return nil, resolveErr
		}
		for _, id := range recipients {
			pool = append(pool, id.String())
			if id.String() == actor.UserID {
				canDecide = true
			}
		}
	}
	detail := &ExpansionDetail{
		Request: request,
		SubmittedSnapshot: map[string]string{
			"current_limit_usd": request.CurrentAmountSnapshot.String(),
			"used_usd":          request.UsedAmountSnapshot.String(),
			"target_amount_usd": fmt.Sprintf("%.6f", request.RequestedAmount),
		},
		LiveSnapshot: live, ApprovalPool: pool, Steps: steps,
		Capabilities: ExpansionCapabilities{
			CanEdit: request.RequesterID.String() == actor.UserID &&
				(request.Status == model.ExpansionStatusPending || request.Status == model.ExpansionStatusPrecheckRequired),
			CanCancel: request.RequesterID.String() == actor.UserID &&
				(request.Status == model.ExpansionStatusPending || request.Status == model.ExpansionStatusPrecheckRequired),
			CanDecide: canDecide && request.Status == model.ExpansionStatusPending &&
				live != nil && !live.Stale && live.PrecheckVersion == request.PrecheckVersion,
			CanReprecheck: request.Status == model.ExpansionStatusPrecheckRequired ||
				(live != nil && live.PrecheckVersion != request.PrecheckVersion),
		},
	}
	if live != nil {
		detail.ParentConstraints, detail.AffectedGroups, detail.DataAsOf =
			live.ParentConstraints, live.AffectedGroups, live.DataAsOf
	}
	return detail, nil
}

func (s *ExpansionService) NotifyOwner(
	ctx context.Context,
	actor ActorScope,
	input NotifyOwnerInput,
) error {
	if err := s.validateActorScope(ctx, actor, input.ScopeType, input.ScopeID); err != nil {
		return err
	}
	if s.approvalPool == nil || s.notifier == nil {
		return nil
	}
	recipients, err := s.approvalPool.ResolveScopeOwnerRecipients(ctx, input.ScopeType, input.ScopeID)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		message := strings.TrimSpace(input.Message)
		if err := s.notifier.Notify(ctx, &model.Notification{
			UserID: recipient, Type: model.NotificationTypeBudgetOwnerAttention, Level: model.NotificationLevelWarning,
			Title: "预算负责人处理提醒",
			RefType: "budget_scope", RefID: input.ScopeType + ":" + input.ScopeID,
			TemplateData: map[string]interface{}{"Message": message},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *ExpansionService) notifyDecisionResult(ctx context.Context, requestID uuid.UUID, action, detail string) {
	if s.notifier == nil {
		return
	}
	request, err := s.repo.FindByID(requestID.String())
	if err != nil {
		return
	}
	title, level := "扩容申请状态更新", model.NotificationLevelInfo
	switch action {
	case model.ExpansionActionApprove:
		title = "扩容申请已批准并生效"
	case model.ExpansionActionReject:
		title, level = "扩容申请已驳回", model.NotificationLevelWarning
	case model.ExpansionActionEscalate:
		title = "扩容申请已升级审批"
	case "apply_failed":
		title, level = "扩容申请生效失败", model.NotificationLevelCritical
	}
	content := title
	if strings.TrimSpace(detail) != "" {
		content += "：" + strings.TrimSpace(detail)
	}
	notification := &model.Notification{
		UserID: request.RequesterID, Type: model.NotificationTypeExpansionDecision, Level: level,
		Title: title, Content: content, RefType: "expansion_request", RefID: request.ID.String(),
		TemplateData: map[string]interface{}{
			"ExpansionID": request.ID.String(),
			"Decision":    action,
			"Reason":      detail,
		},
	}
	for attempt := 0; attempt < 3; attempt++ {
		if s.notifier.Notify(ctx, notification) == nil {
			return
		}
	}
}

func (s *ExpansionService) AssertActorCanAccessRequest(ctx context.Context, actor ActorScope, requestID string) error {
	request, err := s.repo.FindByID(requestID)
	if err != nil {
		return err
	}
	return s.validateActorScope(ctx, actor, request.ScopeType, request.ScopeID)
}

func (s *ExpansionService) validateActorScope(ctx context.Context, actor ActorScope, scopeType, scopeID string) error {
	if actor.IsGlobal || actor.Role == "admin" {
		return nil
	}
	if actor.Role == "user" {
		if scopeType == model.BudgetScopeMember && scopeID == actor.UserID {
			return nil
		}
		return ErrExpansionForbidden
	}
	switch scopeType {
	case model.BudgetScopeStudio:
		if containsString(actor.StudioIDs, scopeID) {
			return nil
		}
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		if containsString(actor.DeptIDs, scopeID) {
			return nil
		}
	case model.BudgetScopeMember:
		if scopeID == actor.UserID {
			return nil
		}
		if s.budgetSvc != nil {
			for _, deptID := range actor.DeptIDs {
				memberIDs, err := s.budgetSvc.ListMemberIDsByDeptID(deptID)
				if err != nil {
					return err
				}
				if containsString(memberIDs, scopeID) {
					return nil
				}
			}
		}
	}
	return ErrExpansionForbidden
}

func actorForDetail(actor ActorScope, request *model.ExpansionRequest) ActorScope {
	if actor.Role != "user" {
		return ActorScope{
			UserID: actor.UserID, Role: roleForRequest(request), IsGlobal: actor.IsGlobal,
			DeptIDs: actor.DeptIDs, StudioIDs: actor.StudioIDs,
		}
	}
	return actor
}

func precheckInputFromRequest(request *model.ExpansionRequest) PrecheckInput {
	target := fmt.Sprintf("%.6f", request.RequestedAmount)
	var tierID *string
	if request.ModelTierID != nil {
		value := request.ModelTierID.String()
		tierID = &value
	}
	return PrecheckInput{
		ScopeType: request.ScopeType, ScopeID: request.ScopeID, PeriodType: request.PeriodType,
		DimensionType: string(request.DimensionType), ModelTierID: tierID,
		DurationType: string(request.DurationType), Priority: request.Priority, TargetAmountUSD: target,
	}
}

func roleForRequest(request *model.ExpansionRequest) string {
	switch request.CurrentLevel {
	case model.ExpansionLevelPlatform:
		return "admin"
	case model.ExpansionLevelStudio:
		return "studio_admin"
	default:
		return "dept_admin"
	}
}

func initialApprovalLevel(scopeType string) string {
	switch scopeType {
	case model.BudgetScopeMember:
		return model.ExpansionLevelDept
	case model.BudgetScopeStudio:
		return model.ExpansionLevelPlatform
	default:
		return model.ExpansionLevelStudio
	}
}

func totalLimitUpdate(periodType string, target float64) map[string]any {
	switch budgetdomain.PeriodType(periodType) {
	case budgetdomain.PeriodDaily:
		return map[string]any{"daily_amount_usd": target}
	case budgetdomain.PeriodWeekly:
		return map[string]any{"weekly_amount_usd": target}
	default:
		return map[string]any{"amount_usd": target}
	}
}

func (s *ExpansionService) notifyApprovalPoolEmpty(ctx context.Context, request *model.ExpansionRequest) {
	if s.notifier == nil || s.approvalPool == nil {
		return
	}
	recipients, err := s.approvalPool.ResolveParentAdmins(ctx, request)
	if err != nil {
		return
	}
	for _, recipient := range recipients {
		_ = s.notifier.Notify(ctx, &model.Notification{
			UserID: recipient, Type: model.NotificationTypeApprovalPoolEmpty, Level: model.NotificationLevelWarning,
			Title:   "扩容审批池为空",
			RefType: "expansion_request", RefID: request.ID.String(),
			TemplateData: map[string]interface{}{"ExpansionID": request.ID.String()},
		})
	}
}

func validatePrecheckInput(actor ActorScope, input PrecheckInput, visible []string) error {
	if !containsString(visible, input.ScopeType) {
		return ErrExpansionForbidden
	}
	if actor.Role == "user" && (input.ScopeType != model.BudgetScopeMember || input.ScopeID != actor.UserID) {
		return ErrExpansionForbidden
	}
	period := budgetdomain.PeriodType(input.PeriodType)
	if !period.Valid() {
		return ErrInvalidExpansionPeriod
	}
	dimension := budgetdomain.DimensionType(input.DimensionType)
	var tierID *uuid.UUID
	if input.ModelTierID != nil {
		id, err := uuid.Parse(*input.ModelTierID)
		if err != nil {
			return ErrInvalidExpansionDimension
		}
		tierID = &id
	}
	if err := budgetdomain.ValidateDimension(dimension, tierID); err != nil {
		return ErrInvalidExpansionDimension
	}
	duration := budgetdomain.DurationType(input.DurationType)
	if !duration.Valid() {
		return fmt.Errorf("invalid expansion duration")
	}
	return nil
}

func (s *ExpansionService) readExpansionSnapshot(ctx context.Context, input PrecheckInput, now time.Time) (budgetdomain.Amount, budgetdomain.Amount, bool, error) {
	if s.budgetSvc == nil {
		return budgetdomain.Amount{}, budgetdomain.Amount{}, true, ErrExpansionUsageUnavailable
	}
	allocation, err := s.budgetSvc.repo.GetAllocation(input.ScopeType, input.ScopeID)
	if err != nil {
		return budgetdomain.Amount{}, budgetdomain.Amount{}, false, err
	}
	periodType := budgetdomain.PeriodType(input.PeriodType)
	current := decimal.Zero
	if allocation != nil {
		if input.DimensionType == string(budgetdomain.DimensionTotal) {
			switch periodType {
			case budgetdomain.PeriodDaily:
				current = decimal.NewFromFloat(allocation.DailyAmountUSD)
			case budgetdomain.PeriodWeekly:
				current = decimal.NewFromFloat(allocation.WeeklyAmountUSD)
			default:
				current = decimal.NewFromFloat(allocation.AmountUSD)
			}
			merged, mergeErr := s.applyActiveOverrideTarget(ctx, allocation.ID, periodType, nil, current, now)
			if mergeErr != nil {
				return budgetdomain.Amount{}, budgetdomain.Amount{}, false, mergeErr
			}
			current = merged
		} else if s.budgetTierSvc != nil && input.ModelTierID != nil {
			tierID, parseErr := uuid.Parse(*input.ModelTierID)
			if parseErr != nil {
				return budgetdomain.Amount{}, budgetdomain.Amount{}, false, ErrInvalidExpansionDimension
			}
			limit, findErr := s.budgetTierSvc.repo.FindTierLimit(ctx, allocation.ID, tierID, periodType)
			if findErr != nil {
				return budgetdomain.Amount{}, budgetdomain.Amount{}, false, findErr
			}
			if limit != nil && limit.Enabled {
				current = limit.LimitUSD.Decimal
			}
			// 无 base 行时仍合并 temporary override，与 my/budget/tiers effective 对齐。
			merged, mergeErr := s.applyActiveOverrideTarget(ctx, allocation.ID, periodType, &tierID, current, now)
			if mergeErr != nil {
				return budgetdomain.Amount{}, budgetdomain.Amount{}, false, mergeErr
			}
			current = merged
		}
	}
	if current.IsNegative() {
		current = decimal.Zero
	}
	window, err := budgetperiod.Resolve(now, periodType, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return budgetdomain.Amount{}, budgetdomain.Amount{}, false, err
	}
	used := decimal.Zero
	stale := s.budgetSvc.rdb == nil
	if s.budgetSvc.rdb != nil {
		key := fmt.Sprintf("budget:v2:used:%s:%s:%s", input.ScopeType, input.ScopeID, window.Key)
		if input.DimensionType == string(budgetdomain.DimensionTier) && input.ModelTierID != nil {
			key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s", input.ScopeType, input.ScopeID, *input.ModelTierID, window.Key)
		}
		value, redisErr := s.budgetSvc.rdb.Get(ctx, key).Int64()
		switch {
		case redisErr == nil:
			used = decimal.New(value, -6)
		case errors.Is(redisErr, redis.Nil):
			stale = false
		default:
			stale = true
		}
	}
	return budgetdomain.Amount{Decimal: current.Round(6)}, budgetdomain.Amount{Decimal: used.Round(6)}, stale, nil
}

// applyActiveOverrideTarget 将 active temporary override 合并进 current（取更大目标额）。
// tierID == nil 表示 total 维度；非 nil 表示对应等级子预算。
func (s *ExpansionService) applyActiveOverrideTarget(
	ctx context.Context,
	allocationID uuid.UUID,
	periodType budgetdomain.PeriodType,
	tierID *uuid.UUID,
	current decimal.Decimal,
	now time.Time,
) (decimal.Decimal, error) {
	if s.budgetTierSvc == nil {
		return current, nil
	}
	overrides, listErr := s.budgetTierSvc.repo.ListActiveOverrides(ctx, allocationID, now)
	if listErr != nil {
		return current, listErr
	}
	for _, override := range overrides {
		if override.PeriodType != periodType {
			continue
		}
		if tierID == nil {
			if override.DimensionType != budgetdomain.DimensionTotal {
				continue
			}
		} else {
			if override.DimensionType != budgetdomain.DimensionTier ||
				override.ModelTierID == nil || *override.ModelTierID != *tierID {
				continue
			}
		}
		if override.TargetLimitUSD.Decimal.GreaterThan(current) {
			current = override.TargetLimitUSD.Decimal
		}
	}
	return current, nil
}

func (s *ExpansionService) precheckApprovalInTx(
	ctx context.Context,
	tx *gorm.DB,
	request *model.ExpansionRequest,
	allocation *model.BudgetAllocation,
	chain []repository.MyBudgetScopeAllocation,
	now time.Time,
) (*PrecheckResult, error) {
	input := precheckInputFromRequest(request)
	current := decimal.Zero
	switch request.DimensionType {
	case budgetdomain.DimensionTier:
		if request.ModelTierID != nil {
			var limit model.BudgetTierLimit
			err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?",
					allocation.ID, *request.ModelTierID, request.PeriodType).
				First(&limit).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			if err == nil && limit.Enabled {
				current = limit.LimitUSD.Decimal
			}
			var overrides []model.BudgetTemporaryOverride
			if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("budget_allocation_id = ? AND dimension_key = ? AND period_type = ? AND status = ?",
					allocation.ID, request.DimensionKey, request.PeriodType, budgetdomain.OverrideActive).
				Find(&overrides).Error; err != nil {
				return nil, err
			}
			for _, override := range overrides {
				if override.TargetLimitUSD.Decimal.GreaterThan(current) {
					current = override.TargetLimitUSD.Decimal
				}
			}
		}
	default:
		current = totalAllocationLimit(*allocation, budgetdomain.PeriodType(request.PeriodType))
	}
	window, err := budgetperiod.Resolve(now, budgetdomain.PeriodType(request.PeriodType), budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return nil, err
	}
	used := decimal.Zero
	usageStale := s.budgetSvc.rdb == nil
	if s.budgetSvc.rdb != nil {
		key := fmt.Sprintf("budget:v2:used:%s:%s:%s", request.ScopeType, request.ScopeID, window.Key)
		if request.DimensionType == budgetdomain.DimensionTier && request.ModelTierID != nil {
			key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s",
				request.ScopeType, request.ScopeID, request.ModelTierID.String(), window.Key)
		}
		value, redisErr := s.budgetSvc.rdb.Get(ctx, key).Int64()
		switch {
		case redisErr == nil:
			used, usageStale = decimal.New(value, -6), false
		case errors.Is(redisErr, redis.Nil):
			usageStale = false
		default:
			usageStale = true
		}
	}
	currentAmount := budgetdomain.Amount{Decimal: current.Round(6)}
	usedAmount := budgetdomain.Amount{Decimal: used.Round(6)}
	target, _ := budgetdomain.ParseAmount(input.TargetAmountUSD)
	constraints, blocking, err := s.parentConstraintsFromChain(ctx, tx, input, target, chain, now)
	if err != nil {
		return nil, err
	}
	return &PrecheckResult{
		CurrentLimitUSD: currentAmount.String(), CurrentUsedUSD: usedAmount.String(),
		TargetAmountUSD: target.String(), EffectiveAfterUSD: target.String(),
		IncreaseUSD: fixedAmount(target.Decimal.Sub(current)), DataAsOf: now,
		Stale: usageStale, ParentConstraints: constraints, BlockingReason: blocking,
		PrecheckVersion: makePrecheckVersion(input, currentAmount, usedAmount, constraints, now),
	}, nil
}

func (s *ExpansionService) parentConstraints(ctx context.Context, input PrecheckInput, target budgetdomain.Amount, now time.Time) ([]ConstraintView, *BlockingReason, error) {
	if input.ScopeType == model.BudgetScopeCompany || s.budgetSvc == nil {
		return []ConstraintView{}, nil, nil
	}
	chain, err := s.budgetSvc.ResolveBudgetChain(ctx, input.ScopeType, input.ScopeID)
	if err != nil {
		return nil, nil, err
	}
	constraints, blocking, err := s.parentConstraintsFromChain(ctx, nil, input, target, chain, now)
	if err != nil {
		return nil, nil, err
	}
	return constraints, blocking, nil
}

func (s *ExpansionService) parentConstraintsFromChain(ctx context.Context, tx *gorm.DB, input PrecheckInput, target budgetdomain.Amount, chain []repository.MyBudgetScopeAllocation, now time.Time) ([]ConstraintView, *BlockingReason, error) {
	constraints := make([]ConstraintView, 0, len(chain))
	for index := len(chain) - 1; index >= 0; index-- {
		node := chain[index]
		if (node.Allocation.ScopeType == input.ScopeType && node.Allocation.ScopeID == input.ScopeID) || node.Allocation.ScopeID == "" {
			continue
		}
		limit := totalAllocationLimit(node.Allocation, budgetdomain.PeriodType(input.PeriodType))
		limitApplies := true
		tierStatus := ""
		if input.DimensionType == string(budgetdomain.DimensionTier) && input.ModelTierID != nil {
			tierID, parseErr := uuid.Parse(*input.ModelTierID)
			if parseErr != nil {
				return nil, nil, parseErr
			}
			var tierLimit *model.BudgetTierLimit
			if tx != nil {
				var row model.BudgetTierLimit
				findErr := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?", node.Allocation.ID, tierID, input.PeriodType).First(&row).Error
				if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
					return nil, nil, findErr
				}
				if findErr == nil {
					tierLimit = &row
				}
			} else {
				var findErr error
				tierLimit, findErr = s.budgetTierSvc.repo.FindTierLimit(ctx, node.Allocation.ID, tierID, budgetdomain.PeriodType(input.PeriodType))
				if findErr != nil {
					return nil, nil, findErr
				}
			}
			switch {
			case tierLimit == nil:
				limit, tierStatus, limitApplies = decimal.Zero, "unset", false
			case !tierLimit.Enabled:
				limit, tierStatus = decimal.Zero, "disabled"
			default:
				limit, tierStatus = tierLimit.LimitUSD.Decimal, "enabled"
			}
		} else if limit.IsNegative() {
			limitApplies = false
		}
		parent := input
		parent.ScopeType, parent.ScopeID = node.Allocation.ScopeType, node.Allocation.ScopeID
		used, stale, readErr := s.readExpansionUsage(ctx, parent, now)
		if readErr != nil {
			return nil, nil, readErr
		}
		status := "available"
		if stale {
			status = "stale"
		} else if limitApplies && (!limit.IsPositive() || !used.Decimal.LessThan(limit) || target.Decimal.GreaterThan(limit)) {
			status = "blocking"
		}
		level := scopeLevel(parent.ScopeType)
		limitUSD := ""
		if limitApplies {
			limitUSD = limit.Round(6).StringFixed(6)
		}
		view := ConstraintView{
			AllocationID: node.Allocation.ID.String(), Version: node.Allocation.UpdatedAt.UTC().Format(time.RFC3339Nano),
			SourceLevel: level, ScopeType: parent.ScopeType, ScopeID: parent.ScopeID,
			LimitUSD: limitUSD, UsedUSD: used.String(), Status: status,
			Freshness: map[bool]string{true: "stale", false: "fresh"}[stale], TierStatus: tierStatus,
		}
		constraints = append(constraints, view)
		if status == "blocking" {
			reason := &BlockingReason{Code: "PARENT_LIMIT_BLOCKING", SourceLevel: level, ScopeType: parent.ScopeType, ScopeID: parent.ScopeID, LimitUSD: limitUSD, Status: status}
			return constraints, reason, nil
		}
	}
	return constraints, nil, nil
}

func (s *ExpansionService) readExpansionUsage(ctx context.Context, input PrecheckInput, now time.Time) (budgetdomain.Amount, bool, error) {
	window, err := budgetperiod.Resolve(now, budgetdomain.PeriodType(input.PeriodType), budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return budgetdomain.Amount{}, false, err
	}
	if s.budgetSvc.rdb == nil {
		return budgetdomain.Amount{Decimal: decimal.Zero}, true, nil
	}
	key := fmt.Sprintf("budget:v2:used:%s:%s:%s", input.ScopeType, input.ScopeID, window.Key)
	if input.DimensionType == string(budgetdomain.DimensionTier) && input.ModelTierID != nil {
		key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s", input.ScopeType, input.ScopeID, *input.ModelTierID, window.Key)
	}
	value, redisErr := s.budgetSvc.rdb.Get(ctx, key).Int64()
	switch {
	case redisErr == nil:
		return budgetdomain.Amount{Decimal: decimal.New(value, -6).Round(6)}, false, nil
	case errors.Is(redisErr, redis.Nil):
		return budgetdomain.Amount{Decimal: decimal.Zero}, false, nil
	default:
		return budgetdomain.Amount{Decimal: decimal.Zero}, true, nil
	}
}

func makePrecheckVersion(input PrecheckInput, current, used budgetdomain.Amount, constraints []ConstraintView, _ time.Time) string {
	parts := []string{input.ScopeType, input.ScopeID, input.PeriodType, input.DimensionType, input.DurationType, current.String(), used.String()}
	if input.ModelTierID != nil {
		parts = append(parts, *input.ModelTierID)
	}
	for _, constraint := range constraints {
		parts = append(parts, constraint.SourceLevel, constraint.ScopeType, constraint.ScopeID,
			constraint.Status, constraint.LimitUSD, constraint.UsedUSD, constraint.TierStatus)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func fixedAmount(value decimal.Decimal) string {
	if value.IsNegative() {
		return "0.000000"
	}
	return value.Round(6).StringFixed(6)
}

func scopeLevel(scopeType string) string {
	switch scopeType {
	case model.BudgetScopeCompany:
		return "company"
	case model.BudgetScopeStudio:
		return "studio"
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return "dept"
	default:
		return "member"
	}
}
