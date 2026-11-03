package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type BatchOperation string

const (
	BatchSetFixed      BatchOperation = "set_fixed"
	BatchAdjustPercent BatchOperation = "adjust_percent"
	// BatchClear 删除等级子预算行，恢复为「未配置」（只受总预算约束）。
	BatchClear BatchOperation = "clear"
)

var (
	ErrInvalidBatchRequest = errors.New("invalid batch request")
	ErrBatchPreviewInvalid = errors.New("invalid or expired batch preview")
	ErrBatchPreviewStale   = errors.New("batch preview is stale")
)

type BatchValidationError struct {
	Field   string
	Message string
}

func (e *BatchValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

func (e *BatchValidationError) Unwrap() error {
	return ErrInvalidBatchRequest
}

func newBatchValidationError(field, message string) error {
	return &BatchValidationError{Field: field, Message: message}
}

type BatchTierLimitItem struct {
	ItemID       string                  `json:"item_id"`
	AllocationID uuid.UUID               `json:"allocation_id"`
	ScopeType    string                  `json:"scope_type,omitempty"`
	ScopeID      string                  `json:"scope_id,omitempty"`
	TierID       uuid.UUID               `json:"model_tier_id"`
	PeriodType   budgetdomain.PeriodType `json:"period_type"`
	Direction    string                  `json:"direction,omitempty"`
	Value        budgetdomain.Amount     `json:"value"`
}

type BatchTierLimitInput struct {
	Operation     BatchOperation       `json:"operation"`
	Items         []BatchTierLimitItem `json:"items"`
	EffectiveMode EffectiveMode        `json:"effective_mode,omitempty"`
	Reason        string               `json:"reason"`
	ActorID       uuid.UUID            `json:"-"`
}

type BatchTierLimitPreviewItem struct {
	ItemID              string               `json:"item_id"`
	OldLimitUSD         *budgetdomain.Amount `json:"old_limit_usd"`
	NewLimitUSD         budgetdomain.Amount  `json:"new_limit_usd"`
	DeltaUSD            string               `json:"delta_usd"`
	Status              string               `json:"status"`
	ErrorCode           string               `json:"error_code,omitempty"`
	OverAllocationRatio string               `json:"over_allocation_ratio,omitempty"`
	ConvertToPersonal   bool                 `json:"converts_shared_member_to_independent"`
	CurrentUsedUSD      budgetdomain.Amount  `json:"current_used_usd"`
	BelowUsed           bool                 `json:"below_used"`
	ZeroBlock           bool                 `json:"zero_block"`
	Stale               bool                 `json:"stale"`
}

type BatchTierLimitPreview struct {
	PreviewToken string                      `json:"preview_token"`
	ExpiresAt    time.Time                   `json:"expires_at"`
	DataAsOf     time.Time                   `json:"data_as_of"`
	Stale        bool                        `json:"stale"`
	Items        []BatchTierLimitPreviewItem `json:"items"`
}

type ApplyBatchTierLimitInput struct {
	PreviewToken   string   `json:"preview_token"`
	ConfirmItemIDs []string `json:"confirm_item_ids"`
}

type BatchTierLimitApplyItem struct {
	ItemID    string `json:"item_id"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

type BatchTierLimitApplyResult struct {
	Items       []BatchTierLimitApplyItem `json:"items"`
	SyncPending bool                      `json:"sync_pending"`
	AuditBefore []map[string]any          `json:"-"`
	AuditAfter  []map[string]any          `json:"-"`
	AuditReason string                    `json:"-"`
}

type batchPreviewState struct {
	actor     rbac.Actor
	input     BatchTierLimitInput
	preview   BatchTierLimitPreview
	versions  map[string]string
	inputHash string
	createdAt time.Time
}

type memberScopeChecker interface {
	MemberInDeptIDs(string, []string) (bool, error)
}

type batchAllocationScopeStore interface {
	GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error)
}

type batchPersonalAllocationConverter interface {
	CreatePersonalAllocationWithTierLimit(context.Context, *model.BudgetAllocation, repository.UpsertTierLimitParams) error
}

type batchTierRiskReader interface {
	GetBatchTierRiskSnapshot(context.Context, model.BudgetAllocation, uuid.UUID, budgetdomain.PeriodType) (repository.BatchTierRiskSnapshot, error)
}

type batchAtomicTierStore interface {
	Transaction(context.Context, func(*gorm.DB) error) error
	FindTierLimitForUpdate(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) (*model.BudgetTierLimit, error)
	UpsertTierLimitInTx(context.Context, *gorm.DB, repository.UpsertTierLimitParams) error
	DeleteTierLimitInTx(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) error
	PutPendingScheduleInTx(context.Context, *gorm.DB, *model.BudgetLimitSchedule, bool) error
}

type batchAtomicAllocationStore interface {
	GetAllocationByIDInTx(context.Context, *gorm.DB, uuid.UUID) (*model.BudgetAllocation, error)
	GetAllocationInTx(context.Context, *gorm.DB, string, string) (*model.BudgetAllocation, error)
	GetBatchTierRiskSnapshotInTx(context.Context, *gorm.DB, model.BudgetAllocation, uuid.UUID, budgetdomain.PeriodType) (repository.BatchTierRiskSnapshot, error)
	CreatePersonalAllocationWithTierLimitInTx(context.Context, *gorm.DB, *model.BudgetAllocation, repository.UpsertTierLimitParams) error
}

func (s *BudgetTierService) WithPreviewCacheLimits(maxEntries int, ttl time.Duration) *BudgetTierService {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	s.previewMu.Lock()
	s.previewCacheMax = maxEntries
	s.previewCacheTTL = ttl
	s.cleanupPreviewCacheLocked(s.clock())
	for len(s.previews) > s.previewCacheMax {
		s.evictOldestPreviewLocked()
	}
	s.previewMu.Unlock()
	return s
}

func (s *BudgetTierService) PreviewCacheSize() int {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	s.cleanupPreviewCacheLocked(s.clock())
	return len(s.previews)
}

func (s *BudgetTierService) CachedPreviewCount() int {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	return len(s.previews)
}

func (s *BudgetTierService) StartPreviewCacheCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.previewMu.Lock()
			s.cleanupPreviewCacheLocked(s.clock())
			s.previewMu.Unlock()
		}
	}
}

func (s *BudgetTierService) cleanupPreviewCacheLocked(now time.Time) {
	for token, state := range s.previews {
		if !now.Before(state.preview.ExpiresAt) {
			delete(s.previews, token)
		}
	}
}

func (s *BudgetTierService) evictOldestPreviewLocked() {
	oldestToken := ""
	var oldestAt time.Time
	for token, state := range s.previews {
		if oldestToken == "" || state.createdAt.Before(oldestAt) || (state.createdAt.Equal(oldestAt) && token < oldestToken) {
			oldestToken, oldestAt = token, state.createdAt
		}
	}
	if oldestToken != "" {
		delete(s.previews, oldestToken)
	}
}

func (s *BudgetTierService) PreviewBatch(ctx context.Context, actor rbac.Actor, input BatchTierLimitInput) (*BatchTierLimitPreview, error) {
	if input.EffectiveMode == "" {
		input.EffectiveMode = EffectiveImmediate
	}
	if input.EffectiveMode != EffectiveImmediate && input.EffectiveMode != EffectiveNextPeriod {
		return nil, newBatchValidationError("effective_mode", "must be immediate or next_period")
	}
	if len(input.Items) == 0 || len(input.Items) > 500 {
		return nil, newBatchValidationError("items", "item count must be between 1 and 500")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrBudgetReasonRequired
	}
	if input.Operation != BatchSetFixed && input.Operation != BatchAdjustPercent && input.Operation != BatchClear {
		return nil, newBatchValidationError("operation", "must be set_fixed, adjust_percent, or clear")
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if item.ItemID == "" {
			return nil, newBatchValidationError("items.item_id", "is required")
		}
		if _, duplicate := seen[item.ItemID]; duplicate {
			return nil, newBatchValidationError("items.item_id", "must be unique")
		}
		seen[item.ItemID] = struct{}{}
	}
	now := s.clock()
	result := &BatchTierLimitPreview{
		ExpiresAt: now.Add(s.previewCacheTTL), DataAsOf: now,
		Items: make([]BatchTierLimitPreviewItem, 0, len(input.Items)),
	}
	versions := make(map[string]string, len(input.Items))
	type riskGroup struct {
		parent  budgetdomain.Amount
		total   budgetdomain.Amount
		delta   decimal.Decimal
		indices []int
	}
	riskGroups := map[string]*riskGroup{}
	includeArchived := input.Operation == BatchClear
	activeTiers, err := s.tiers.List(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	tierByID := make(map[uuid.UUID]model.ModelTier, len(activeTiers))
	for _, tier := range activeTiers {
		tierByID[tier.ID] = tier
	}
	for _, item := range input.Items {
		view := BatchTierLimitPreviewItem{ItemID: item.ItemID}
		allocation, convertToPersonal, err := s.resolveBatchAllocation(ctx, input.Operation, item)
		if err != nil {
			view.Status, view.ErrorCode = "invalid", "BUDGET_ALLOCATION_NOT_FOUND"
			result.Items = append(result.Items, view)
			continue
		}
		view.ConvertToPersonal = convertToPersonal
		allowed, scopeErr := s.canManageBatchAllocation(ctx, actor, allocation)
		if scopeErr != nil {
			return nil, scopeErr
		}
		if !allowed {
			view.Status, view.ErrorCode = "forbidden", "ORG_FORBIDDEN"
			result.Items = append(result.Items, view)
			continue
		}
		tier, found := tierByID[item.TierID]
		// clear 允许清理已停用/归档等级上的残留限制；set/adjust 仍要求等级可用
		if input.Operation != BatchClear && (!found || !tier.Enabled || tier.ArchivedAt != nil) {
			view.Status, view.ErrorCode = "invalid", "MODEL_TIER_DISABLED"
			result.Items = append(result.Items, view)
			continue
		}
		if input.Operation == BatchClear && !found {
			view.Status, view.ErrorCode = "invalid", "MODEL_TIER_NOT_FOUND"
			result.Items = append(result.Items, view)
			continue
		}
		if !item.PeriodType.Valid() {
			view.Status, view.ErrorCode = "invalid", "INVALID_BUDGET_PERIOD"
			result.Items = append(result.Items, view)
			continue
		}
		if convertToPersonal && input.Operation == BatchClear {
			view.Status, view.ErrorCode = "invalid", "BUDGET_TIER_LIMIT_NOT_FOUND"
			result.Items = append(result.Items, view)
			continue
		}
		var existing *model.BudgetTierLimit
		if !convertToPersonal {
			existing, err = s.repo.FindTierLimit(ctx, allocation.ID, item.TierID, item.PeriodType)
			if err != nil {
				return nil, err
			}
		}
		risk := repository.BatchTierRiskSnapshot{}
		if riskReader, ok := s.allocations.(batchTierRiskReader); ok {
			risk, err = riskReader.GetBatchTierRiskSnapshot(ctx, *allocation, item.TierID, item.PeriodType)
			if err != nil {
				return nil, err
			}
		}
		versions[item.ItemID] = batchTierLimitVersion(allocation, convertToPersonal, existing, risk.ParentVersion)
		if input.Operation == BatchClear {
			// 幂等：已无行也视为可清除
			if existing != nil {
				old := existing.LimitUSD
				view.OldLimitUSD = &old
				view.DeltaUSD = decimal.Zero.Sub(old.Decimal).StringFixed(6)
			} else {
				view.DeltaUSD = "0.000000"
			}
			view.NewLimitUSD = budgetdomain.Amount{}
			view.CurrentUsedUSD, view.Stale = s.currentTierUsage(ctx, SetTierLimitInput{
				AllocationID: allocation.ID, TierID: item.TierID, PeriodType: item.PeriodType,
			}, now)
			result.Stale = result.Stale || view.Stale
			if risk.ParentLimitUSD != nil && risk.ParentLimitUSD.Decimal.IsPositive() && existing != nil {
				key := risk.ParentVersion + ":" + item.TierID.String() + ":" + string(item.PeriodType)
				group := riskGroups[key]
				if group == nil {
					group = &riskGroup{parent: *risk.ParentLimitUSD, total: risk.SiblingLimitTotalUSD}
					riskGroups[key] = group
				}
				group.delta = group.delta.Sub(existing.LimitUSD.Decimal)
				group.indices = append(group.indices, len(result.Items))
			}
			view.Status = "valid"
			result.Items = append(result.Items, view)
			continue
		}
		newLimit, calcErr := calculateBatchLimit(input.Operation, item, existing)
		if calcErr != "" {
			view.Status, view.ErrorCode = "invalid", calcErr
			result.Items = append(result.Items, view)
			continue
		}
		if existing != nil {
			old := existing.LimitUSD
			view.OldLimitUSD = &old
		}
		view.NewLimitUSD = newLimit
		view.CurrentUsedUSD, view.Stale = s.currentTierUsage(ctx, SetTierLimitInput{
			AllocationID: allocation.ID, TierID: item.TierID, PeriodType: item.PeriodType,
		}, now)
		view.ZeroBlock = newLimit.Decimal.IsZero()
		view.BelowUsed = newLimit.Decimal.LessThan(view.CurrentUsedUSD.Decimal)
		result.Stale = result.Stale || view.Stale
		oldValue := decimal.Zero
		if existing != nil {
			oldValue = existing.LimitUSD.Decimal
		}
		view.DeltaUSD = newLimit.Decimal.Sub(oldValue).StringFixed(6)
		if risk.ParentLimitUSD != nil && risk.ParentLimitUSD.Decimal.IsPositive() {
			key := risk.ParentVersion + ":" + item.TierID.String() + ":" + string(item.PeriodType)
			group := riskGroups[key]
			if group == nil {
				group = &riskGroup{parent: *risk.ParentLimitUSD, total: risk.SiblingLimitTotalUSD}
				riskGroups[key] = group
			}
			group.delta = group.delta.Add(newLimit.Decimal.Sub(oldValue))
			group.indices = append(group.indices, len(result.Items))
		}
		view.Status = "valid"
		result.Items = append(result.Items, view)
	}
	for _, group := range riskGroups {
		ratio := group.total.Decimal.Add(group.delta).Div(group.parent.Decimal).StringFixed(6)
		for _, index := range group.indices {
			result.Items[index].OverAllocationRatio = ratio
		}
	}
	inputHash := canonicalBatchInputHash(actor.UserID, input, versions)
	tokenBytes := sha256.Sum256([]byte(inputHash + "|" + result.ExpiresAt.UTC().Format(time.RFC3339Nano)))
	result.PreviewToken = "sha256:" + hex.EncodeToString(tokenBytes[:])
	s.previewMu.Lock()
	s.cleanupPreviewCacheLocked(now)
	for len(s.previews) >= s.previewCacheMax {
		s.evictOldestPreviewLocked()
	}
	s.previews[result.PreviewToken] = batchPreviewState{
		actor: actor, input: input, preview: *result, versions: versions, inputHash: inputHash, createdAt: now,
	}
	s.previewMu.Unlock()
	return result, nil
}

func (s *BudgetTierService) ApplyBatch(ctx context.Context, actor rbac.Actor, input ApplyBatchTierLimitInput) (*BatchTierLimitApplyResult, error) {
	now := s.clock()
	s.previewMu.Lock()
	s.cleanupPreviewCacheLocked(now)
	state, ok := s.previews[input.PreviewToken]
	if ok {
		delete(s.previews, input.PreviewToken)
	}
	s.previewMu.Unlock()
	if !ok || state.actor.UserID != actor.UserID || !now.Before(state.preview.ExpiresAt) {
		return nil, ErrBatchPreviewInvalid
	}
	if state.preview.Stale && s.rdb != nil {
		return nil, ErrBatchPreviewStale
	}
	if state.inputHash != canonicalBatchInputHash(actor.UserID, state.input, state.versions) {
		return nil, ErrBatchPreviewInvalid
	}
	confirmed := make(map[string]struct{}, len(input.ConfirmItemIDs))
	for _, itemID := range input.ConfirmItemIDs {
		confirmed[itemID] = struct{}{}
	}
	type applyPreflight struct {
		allocation        *model.BudgetAllocation
		current           *model.BudgetTierLimit
		convertToPersonal bool
		allowed           bool
		err               error
		version           string
	}
	preflight := make(map[string]applyPreflight, len(confirmed))
	for index, item := range state.input.Items {
		if state.preview.Items[index].Status != "valid" {
			continue
		}
		if _, shouldApply := confirmed[item.ItemID]; !shouldApply {
			continue
		}
		check := applyPreflight{}
		check.allocation, check.convertToPersonal, check.err = s.resolveBatchAllocation(ctx, state.input.Operation, item)
		if check.err == nil {
			check.allowed, check.err = s.canManageBatchAllocation(ctx, actor, check.allocation)
		}
		if check.err == nil && !check.convertToPersonal {
			check.current, check.err = s.repo.FindTierLimit(ctx, check.allocation.ID, item.TierID, item.PeriodType)
		}
		parentVersion := ""
		if riskReader, ok := s.allocations.(batchTierRiskReader); ok && check.err == nil {
			risk, riskErr := riskReader.GetBatchTierRiskSnapshot(ctx, *check.allocation, item.TierID, item.PeriodType)
			if riskErr != nil {
				check.err = riskErr
			} else {
				parentVersion = risk.ParentVersion
			}
		}
		if check.err == nil {
			check.version = batchTierLimitVersion(check.allocation, check.convertToPersonal, check.current, parentVersion)
		}
		preflight[item.ItemID] = check
	}
	result := &BatchTierLimitApplyResult{
		Items: make([]BatchTierLimitApplyItem, 0, len(state.input.Items)), AuditReason: state.input.Reason,
	}
	persistedAny := false
	for index, item := range state.input.Items {
		previewItem := state.preview.Items[index]
		applied := BatchTierLimitApplyItem{ItemID: item.ItemID, Status: "skipped"}
		if previewItem.Status != "valid" {
			result.Items = append(result.Items, applied)
			continue
		}
		if _, shouldApply := confirmed[item.ItemID]; !shouldApply {
			result.Items = append(result.Items, applied)
			continue
		}
		check := preflight[item.ItemID]
		allocation, convertToPersonal, current := check.allocation, check.convertToPersonal, check.current
		if check.err != nil {
			applied.Status, applied.ErrorCode = "failed", batchErrorCode(check.err)
			result.Items = append(result.Items, applied)
			continue
		}
		if allocation == nil {
			applied.Status, applied.ErrorCode = "failed", "BUDGET_ALLOCATION_NOT_FOUND"
			result.Items = append(result.Items, applied)
			continue
		}
		if !check.allowed {
			applied.Status, applied.ErrorCode = "failed", "ORG_FORBIDDEN"
			result.Items = append(result.Items, applied)
			continue
		}
		atomicStore, hasAtomicStore := s.repo.(batchAtomicTierStore)
		atomicAllocations, hasAtomicAllocations := s.allocations.(batchAtomicAllocationStore)
		handledAtomically := hasAtomicStore && hasAtomicAllocations
		if !handledAtomically && check.version != state.versions[item.ItemID] {
			applied.Status, applied.ErrorCode = "failed", "BUDGET_PREVIEW_STALE"
			result.Items = append(result.Items, applied)
			continue
		}
		var setErr error
		if handledAtomically {
			allocation, convertToPersonal, current, setErr = s.applyBatchTierLimitAtomic(
				ctx, actor, atomicStore, atomicAllocations, state, item, previewItem,
			)
		} else if convertToPersonal {
			if state.input.Operation == BatchClear {
				applied.Status, applied.ErrorCode = "failed", "BUDGET_TIER_LIMIT_NOT_FOUND"
				result.Items = append(result.Items, applied)
				continue
			}
			allocation.ID = uuid.New()
			allocation.AmountUSD, allocation.DailyAmountUSD, allocation.WeeklyAmountUSD = -1, -1, -1
			allocation.Period, allocation.Enabled = "monthly", true
			allocation.CreatedBy, allocation.UpdatedBy = state.input.ActorID, state.input.ActorID
			converter, ok := s.allocations.(batchPersonalAllocationConverter)
			if !ok || state.input.EffectiveMode != EffectiveImmediate {
				applied.Status, applied.ErrorCode = "failed", "BUDGET_CONVERSION_UNSUPPORTED"
				result.Items = append(result.Items, applied)
				continue
			}
			setErr = converter.CreatePersonalAllocationWithTierLimit(ctx, allocation, repository.UpsertTierLimitParams{
				BudgetAllocationID: allocation.ID, ModelTierID: item.TierID, PeriodType: item.PeriodType,
				LimitUSD: previewItem.NewLimitUSD, EffectiveAt: s.clock(), Enabled: true,
			})
		}
		beforeLimit := any(nil)
		if current != nil {
			beforeLimit = current.LimitUSD.String()
		}
		if !handledAtomically && !convertToPersonal {
			if state.input.Operation == BatchClear {
				switch state.input.EffectiveMode {
				case EffectiveImmediate:
					if current == nil {
						setErr = nil
					} else {
						setErr = s.repo.DeleteTierLimit(ctx, allocation.ID, item.TierID, item.PeriodType)
					}
				case EffectiveNextPeriod:
					if current == nil {
						setErr = nil
					} else {
						period, periodErr := budgetperiod.Resolve(s.clock(), item.PeriodType, s.periodConfig)
						if periodErr != nil {
							setErr = periodErr
						} else {
							schedule := &model.BudgetLimitSchedule{
								ID: uuid.New(), BudgetAllocationID: allocation.ID,
								DimensionType: budgetdomain.DimensionTier, DimensionKey: "tier:" + item.TierID.String(), ModelTierID: &item.TierID,
								PeriodType: item.PeriodType, TargetLimitUSD: budgetdomain.Amount{},
								ScheduleOperation: budgetdomain.ScheduleOperationClear,
								EffectiveAt: period.ResetAt, Reason: state.input.Reason, CreatedBy: state.input.ActorID,
								Status: budgetdomain.SchedulePending,
							}
							_, setErr = s.repo.PutPendingSchedule(ctx, schedule, false)
						}
					}
				default:
					setErr = ErrInvalidEffectiveMode
				}
			} else {
				_, setErr = s.setTierLimit(ctx, SetTierLimitInput{
					AllocationID: allocation.ID, TierID: item.TierID, PeriodType: item.PeriodType,
					LimitUSD: previewItem.NewLimitUSD, Enabled: current == nil || current.Enabled,
					EffectiveMode: state.input.EffectiveMode, Reason: state.input.Reason, ActorID: state.input.ActorID,
				}, false)
			}
		}
		persisted := setErr == nil || errors.Is(setErr, ErrBudgetConfigSyncPending)
		if errors.Is(setErr, ErrBudgetConfigSyncPending) {
			result.SyncPending = true
			applied.Status, applied.ErrorCode = "applied", "BUDGET_CONFIG_SYNC_PENDING"
		} else if setErr != nil {
			applied.Status, applied.ErrorCode = "failed", batchErrorCode(setErr)
		} else {
			applied.Status = "applied"
		}
		if persisted {
			persistedAny = true
			afterLimit := any(nil)
			if state.input.Operation != BatchClear {
				afterLimit = previewItem.NewLimitUSD.String()
			}
			result.AuditBefore = append(result.AuditBefore, map[string]any{
				"item_id": item.ItemID, "allocation_id": allocation.ID,
				"scope_type": allocation.ScopeType, "scope_id": allocation.ScopeID, "limit_usd": beforeLimit,
			})
			result.AuditAfter = append(result.AuditAfter, map[string]any{
				"item_id": item.ItemID, "allocation_id": allocation.ID, "limit_usd": afterLimit,
				"scope_type": allocation.ScopeType, "scope_id": allocation.ScopeID,
				"converted_to_personal": convertToPersonal, "cleared": state.input.Operation == BatchClear,
			})
		}
		result.Items = append(result.Items, applied)
	}
	if persistedAny {
		if err := s.publishBudgetSnapshot(ctx, uuid.Nil); err != nil {
			result.SyncPending = true
			for index := range result.Items {
				if result.Items[index].Status == "applied" && result.Items[index].ErrorCode == "" {
					result.Items[index].ErrorCode = "BUDGET_CONFIG_SYNC_PENDING"
				}
			}
		}
	}
	return result, nil
}

func (s *BudgetTierService) applyBatchTierLimitAtomic(
	ctx context.Context,
	actor rbac.Actor,
	store batchAtomicTierStore,
	allocations batchAtomicAllocationStore,
	state batchPreviewState,
	item BatchTierLimitItem,
	previewItem BatchTierLimitPreviewItem,
) (*model.BudgetAllocation, bool, *model.BudgetTierLimit, error) {
	var (
		allocation        *model.BudgetAllocation
		current           *model.BudgetTierLimit
		convertToPersonal bool
	)
	err := store.Transaction(ctx, func(tx *gorm.DB) error {
		var err error
		if item.AllocationID != uuid.Nil {
			allocation, err = allocations.GetAllocationByIDInTx(ctx, tx, item.AllocationID)
		} else {
			allocation, err = allocations.GetAllocationInTx(ctx, tx, item.ScopeType, item.ScopeID)
			if err == nil && allocation == nil {
				allocation = &model.BudgetAllocation{ScopeType: item.ScopeType, ScopeID: item.ScopeID}
				convertToPersonal = true
			}
		}
		if err != nil {
			return err
		}
		if allocation == nil {
			return ErrBudgetAllocationNotFound
		}
		allowed, err := s.canManageBatchAllocation(ctx, actor, allocation)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrBudgetForbidden
		}
		risk, err := allocations.GetBatchTierRiskSnapshotInTx(ctx, tx, *allocation, item.TierID, item.PeriodType)
		if err != nil {
			return err
		}
		if risk.TargetAllocation != nil {
			allocation = risk.TargetAllocation
		}
		current = risk.TargetLimit
		if !convertToPersonal && current == nil {
			current, err = store.FindTierLimitForUpdate(ctx, tx, allocation.ID, item.TierID, item.PeriodType)
			if err != nil {
				return err
			}
		}
		version := batchTierLimitVersion(allocation, convertToPersonal, current, risk.ParentVersion)
		if version != state.versions[item.ItemID] {
			return ErrBatchPreviewStale
		}
		if convertToPersonal {
			if state.input.Operation == BatchClear {
				return ErrBudgetAllocationNotFound
			}
			if state.input.EffectiveMode != EffectiveImmediate {
				return errors.New("personal allocation conversion only supports immediate mode")
			}
			allocation.ID = uuid.New()
			allocation.AmountUSD, allocation.DailyAmountUSD, allocation.WeeklyAmountUSD = -1, -1, -1
			allocation.Period, allocation.Enabled = "monthly", true
			allocation.CreatedBy, allocation.UpdatedBy = state.input.ActorID, state.input.ActorID
			return allocations.CreatePersonalAllocationWithTierLimitInTx(ctx, tx, allocation, repository.UpsertTierLimitParams{
				BudgetAllocationID: allocation.ID, ModelTierID: item.TierID, PeriodType: item.PeriodType,
				LimitUSD: previewItem.NewLimitUSD, EffectiveAt: s.clock(), Enabled: true,
			})
		}
		if state.input.Operation == BatchClear {
			switch state.input.EffectiveMode {
			case EffectiveImmediate:
				if current == nil {
					return nil
				}
				return store.DeleteTierLimitInTx(ctx, tx, allocation.ID, item.TierID, item.PeriodType)
			case EffectiveNextPeriod:
				if current == nil {
					return nil
				}
				period, err := budgetperiod.Resolve(s.clock(), item.PeriodType, s.periodConfig)
				if err != nil {
					return err
				}
				schedule := &model.BudgetLimitSchedule{
					ID: uuid.New(), BudgetAllocationID: allocation.ID,
					DimensionType: budgetdomain.DimensionTier, DimensionKey: "tier:" + item.TierID.String(), ModelTierID: &item.TierID,
					PeriodType: item.PeriodType, TargetLimitUSD: budgetdomain.Amount{},
					ScheduleOperation: budgetdomain.ScheduleOperationClear,
					EffectiveAt: period.ResetAt, Reason: state.input.Reason, CreatedBy: state.input.ActorID,
					Status: budgetdomain.SchedulePending,
				}
				return store.PutPendingScheduleInTx(ctx, tx, schedule, false)
			default:
				return ErrInvalidEffectiveMode
			}
		}
		if state.input.EffectiveMode == EffectiveImmediate {
			return store.UpsertTierLimitInTx(ctx, tx, repository.UpsertTierLimitParams{
				BudgetAllocationID: allocation.ID, ModelTierID: item.TierID, PeriodType: item.PeriodType,
				LimitUSD: previewItem.NewLimitUSD, EffectiveAt: s.clock(), Enabled: current == nil || current.Enabled,
			})
		}
		if state.input.EffectiveMode != EffectiveNextPeriod {
			return ErrInvalidEffectiveMode
		}
		period, err := budgetperiod.Resolve(s.clock(), item.PeriodType, s.periodConfig)
		if err != nil {
			return err
		}
		schedule := &model.BudgetLimitSchedule{
			ID: uuid.New(), BudgetAllocationID: allocation.ID,
			DimensionType: budgetdomain.DimensionTier, DimensionKey: "tier:" + item.TierID.String(), ModelTierID: &item.TierID,
			PeriodType: item.PeriodType, TargetLimitUSD: previewItem.NewLimitUSD,
			ScheduleOperation: budgetdomain.ScheduleOperationSet,
			EffectiveAt: period.ResetAt, Reason: state.input.Reason, CreatedBy: state.input.ActorID,
			Status: budgetdomain.SchedulePending,
		}
		return store.PutPendingScheduleInTx(ctx, tx, schedule, false)
	})
	return allocation, convertToPersonal, current, err
}

func batchErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrBudgetAllocationNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "BUDGET_ALLOCATION_NOT_FOUND"
	case errors.Is(err, ErrBudgetForbidden), errors.Is(err, repository.ErrForbidden):
		return "ORG_FORBIDDEN"
	case errors.Is(err, ErrModelTierNotFound):
		return "MODEL_TIER_NOT_FOUND"
	case errors.Is(err, ErrModelTierDisabled):
		return "MODEL_TIER_DISABLED"
	case errors.Is(err, ErrInvalidBudgetPeriod):
		return "INVALID_BUDGET_PERIOD"
	case errors.Is(err, ErrPendingScheduleExists):
		return "PENDING_BUDGET_SCHEDULE_EXISTS"
	case errors.Is(err, ErrBudgetConfigSyncPending):
		return "BUDGET_CONFIG_SYNC_PENDING"
	case errors.Is(err, ErrBatchPreviewStale):
		return "BUDGET_PREVIEW_STALE"
	case errors.Is(err, repository.ErrUniqueConflict):
		return "BUDGET_ALLOCATION_CONFLICT"
	default:
		return "INTERNAL_ERROR"
	}
}

func (s *BudgetTierService) resolveBatchAllocation(
	ctx context.Context,
	operation BatchOperation,
	item BatchTierLimitItem,
) (*model.BudgetAllocation, bool, error) {
	if item.AllocationID != uuid.Nil {
		allocation, err := s.getAllocation(ctx, item.AllocationID)
		return allocation, false, err
	}
	if operation != BatchSetFixed || item.ScopeType != model.BudgetScopeMember || item.ScopeID == "" {
		return nil, false, ErrBudgetAllocationNotFound
	}
	store, ok := s.allocations.(batchAllocationScopeStore)
	if !ok {
		return nil, false, ErrBudgetAllocationNotFound
	}
	allocation, err := store.GetAllocation(item.ScopeType, item.ScopeID)
	if err != nil {
		return nil, false, mapBudgetRepositoryError(err)
	}
	if allocation != nil {
		return allocation, false, nil
	}
	return &model.BudgetAllocation{ScopeType: item.ScopeType, ScopeID: item.ScopeID}, true, nil
}

func (s *BudgetTierService) canManageBatchAllocation(ctx context.Context, actor rbac.Actor, allocation *model.BudgetAllocation) (bool, error) {
	if actor.IsGlobal {
		return true, nil
	}
	switch allocation.ScopeType {
	case model.BudgetScopeStudio:
		return actor.HasStudio(allocation.ScopeID), nil
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		// 与预算池额度写入（handler.checkBudgetScopeWrite）共用同一判定：
		// 部门预算池由工作室管理员/平台管理员下发，部门管理员只读。
		return actor.CanManageDeptBudgetPool(allocation.ScopeID), nil
	case model.BudgetScopeMember:
		if len(actor.StudioIDs) > 0 || len(actor.DeptIDs) == 0 {
			return false, nil
		}
		checker, ok := s.allocations.(memberScopeChecker)
		if !ok {
			return false, nil
		}
		return checker.MemberInDeptIDs(allocation.ScopeID, actor.DeptIDs)
	default:
		return false, nil
	}
}

func calculateBatchLimit(operation BatchOperation, item BatchTierLimitItem, existing *model.BudgetTierLimit) (budgetdomain.Amount, string) {
	if operation == BatchSetFixed {
		return item.Value, ""
	}
	if existing == nil {
		return budgetdomain.Amount{}, "BUDGET_TIER_LIMIT_NOT_FOUND"
	}
	if item.Direction != "increase" && item.Direction != "decrease" {
		return budgetdomain.Amount{}, "INVALID_ADJUST_DIRECTION"
	}
	factor := decimal.NewFromInt(100)
	if item.Direction == "increase" {
		factor = factor.Add(item.Value.Decimal)
	} else {
		factor = factor.Sub(item.Value.Decimal)
	}
	if factor.IsNegative() {
		return budgetdomain.Amount{}, "INVALID_MONEY_AMOUNT"
	}
	return amountFromDecimal(existing.LimitUSD.Decimal.Mul(factor).Div(decimal.NewFromInt(100))), ""
}

func tierLimitVersion(limit *model.BudgetTierLimit) string {
	if limit == nil {
		return "missing"
	}
	return limit.ID.String() + ":" + limit.UpdatedAt.UTC().Format(time.RFC3339Nano) + ":" + limit.LimitUSD.String()
}

func batchTierLimitVersion(allocation *model.BudgetAllocation, convertToPersonal bool, limit *model.BudgetTierLimit, parentVersion string) string {
	if convertToPersonal {
		return "personal-allocation-missing:" + allocation.ScopeType + ":" + allocation.ScopeID + ":parent:" + parentVersion
	}
	return fmt.Sprintf("%s:%s:%f:%f:%f:%t:%s:parent:%s",
		allocation.ID, allocation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		allocation.AmountUSD, allocation.DailyAmountUSD, allocation.WeeklyAmountUSD, allocation.Enabled,
		tierLimitVersion(limit), parentVersion)
}

func canonicalBatchInputHash(actorID string, input BatchTierLimitInput, versions map[string]string) string {
	type versionEntry struct {
		ItemID  string `json:"item_id"`
		Version string `json:"version"`
	}
	versionList := make([]versionEntry, 0, len(versions))
	for itemID, version := range versions {
		versionList = append(versionList, versionEntry{ItemID: itemID, Version: version})
	}
	sort.Slice(versionList, func(i, j int) bool { return versionList[i].ItemID < versionList[j].ItemID })
	raw, _ := json.Marshal(struct {
		ActorID  string              `json:"actor_id"`
		Input    BatchTierLimitInput `json:"input"`
		Versions []versionEntry      `json:"versions"`
	}{ActorID: actorID, Input: input, Versions: versionList})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
