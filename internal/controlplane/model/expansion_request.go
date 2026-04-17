package model

import (
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/google/uuid"
)

// 扩容审批状态
const (
	ExpansionStatusPending               = "pending"
	ExpansionStatusPrecheckRequired      = "precheck_required"
	ExpansionStatusPendingReconfirmation = "pending_reconfirmation"
	ExpansionStatusApproved              = "approved"
	ExpansionStatusRejected              = "rejected"
	ExpansionStatusCancelled             = "cancelled"
	ExpansionStatusExpired               = "expired"
)

// 审批级别
const (
	ExpansionLevelDept     = "dept"     // 部门负责人受理成员额度申请
	ExpansionLevelStudio   = "studio"   // 工作室负责人受理
	ExpansionLevelPlatform = "platform" // 平台管理员受理（工作室盘子不足时升级）
)

// 审批动作
const (
	ExpansionActionApprove            = "approve"
	ExpansionActionReject             = "reject"
	ExpansionActionEscalate           = "escalate" // 升级到上一级
	ExpansionActionCancel             = "cancel"
	ExpansionActionReprecheckRequired = "require_reprecheck"
	ExpansionActionConfirmPrecheck    = "confirm_precheck"
)

const (
	ExpansionDimensionTotal = "total"
	ExpansionDimensionTier  = "tier"

	ExpansionDurationPermanent   = "permanent"
	ExpansionDurationCurrentTerm = "current_period"

	ExpansionPriorityNormal = "normal"
	ExpansionPriorityUrgent = "urgent"
)

// 申请的额度周期维度
const (
	ExpansionPeriodDaily   = "daily"
	ExpansionPeriodWeekly  = "weekly"
	ExpansionPeriodMonthly = "monthly"
)

// ExpansionRequest 扩容申请主表（自建审批）。
// approved ≠ 自动改额，审批通过后由上级手动走 PUT /api/v1/budgets 改额（FR-4.6）。
type ExpansionRequest struct {
	ID                    uuid.UUID                  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType             string                     `gorm:"type:varchar(16);not null;uniqueIndex:uniq_expansion_request_pending,priority:2,where:(status = 'pending' OR status = 'precheck_required')" json:"scope_type"`
	ScopeID               string                     `gorm:"type:varchar(64);not null;index;uniqueIndex:uniq_expansion_request_pending,priority:3,where:(status = 'pending' OR status = 'precheck_required')" json:"scope_id"`
	RequesterID           uuid.UUID                  `gorm:"type:uuid;not null;index;uniqueIndex:uniq_expansion_request_pending,priority:1,where:(status = 'pending' OR status = 'precheck_required')" json:"requester_id"`
	PeriodType            string                     `gorm:"type:varchar(16);not null;default:'monthly';check:period_type IN ('daily','weekly','monthly');uniqueIndex:uniq_expansion_request_pending,priority:4,where:(status = 'pending' OR status = 'precheck_required')" json:"period_type"`
	DimensionType         budgetdomain.DimensionType `gorm:"type:varchar(8);not null;default:'total';index" json:"dimension_type"`
	DimensionKey          string                     `gorm:"type:varchar(48);not null;default:'total';index;check:chk_expansion_dimension_key,(dimension_type = 'total' AND dimension_key = 'total') OR (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text));uniqueIndex:uniq_expansion_request_pending,priority:5,where:(status = 'pending' OR status = 'precheck_required')" json:"dimension_key"`
	ModelTierID           *uuid.UUID                 `gorm:"type:uuid;index;check:chk_expansion_dimension,(dimension_type = 'total' AND model_tier_id IS NULL) OR (dimension_type = 'tier' AND model_tier_id IS NOT NULL)" json:"model_tier_id,omitempty"`
	ModelTier             *ModelTier                 `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	DurationType          budgetdomain.DurationType  `gorm:"type:varchar(16);not null;default:'permanent';index;check:duration_type IN ('permanent','current_period');uniqueIndex:uniq_expansion_request_pending,priority:6,where:(status = 'pending' OR status = 'precheck_required')" json:"duration_type"`
	Priority              string                     `gorm:"type:varchar(16);not null;default:'normal';check:priority IN ('normal','urgent')" json:"priority"`
	CurrentAmount         float64                    `gorm:"type:numeric(12,4)" json:"current_amount"`
	RequestedAmount       float64                    `gorm:"type:numeric(12,4);not null" json:"requested_amount"`
	CurrentAmountSnapshot budgetdomain.Amount        `gorm:"type:numeric(18,6);not null;default:0" json:"current_amount_snapshot"`
	UsedAmountSnapshot    budgetdomain.Amount        `gorm:"type:numeric(18,6);not null;default:0" json:"used_amount_snapshot"`
	PrecheckVersion       string                     `gorm:"type:varchar(64)" json:"precheck_version,omitempty"`
	PrecheckedAt          *time.Time                 `json:"prechecked_at,omitempty"`
	ExpiresAt             *time.Time                 `json:"expires_at,omitempty"`
	AppliedAt             *time.Time                 `json:"applied_at,omitempty"`
	AppliedTargetType     string                     `gorm:"type:varchar(32)" json:"applied_target_type,omitempty"`
	AppliedTargetID       *uuid.UUID                 `gorm:"type:uuid" json:"applied_target_id,omitempty"`
	SyncStatus            string                     `gorm:"type:varchar(16);not null;default:'pending';check:sync_status IN ('pending','synced','failed')" json:"sync_status"`
	Reason                string                     `gorm:"type:text" json:"reason"`
	UrgentReason          string                     `gorm:"type:text;not null;default:''" json:"urgent_reason"`
	Status                string                     `gorm:"type:varchar(24);not null;default:'pending';index" json:"status"`
	CurrentLevel          string                     `gorm:"type:varchar(16);not null" json:"current_level"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
}

func (ExpansionRequest) TableName() string { return "expansion_requests" }

// NextExpansionStatus 返回扩容申请在动作执行后的合法状态。
func NextExpansionStatus(current, action string) (string, error) {
	transitions := map[string]map[string]string{
		ExpansionStatusPending: {
			ExpansionActionApprove:            ExpansionStatusApproved,
			ExpansionActionReject:             ExpansionStatusRejected,
			ExpansionActionCancel:             ExpansionStatusCancelled,
			ExpansionActionEscalate:           ExpansionStatusPending,
			ExpansionActionReprecheckRequired: ExpansionStatusPrecheckRequired,
		},
		ExpansionStatusPrecheckRequired: {
			ExpansionActionConfirmPrecheck: ExpansionStatusPending,
			ExpansionActionCancel:          ExpansionStatusCancelled,
		},
	}
	next, ok := transitions[current][action]
	if !ok {
		return "", fmt.Errorf("invalid expansion transition: %s -> %s", current, action)
	}
	return next, nil
}

// ExpansionApprovalStep 审批流水（历史/审计，为 v0.8 预留）。
type ExpansionApprovalStep struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	RequestID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"request_id"`
	Level      string     `gorm:"type:varchar(16);not null" json:"level"` // dept|studio|platform
	ApproverID *uuid.UUID `gorm:"type:uuid" json:"approver_id,omitempty"`
	Action     string     `gorm:"type:varchar(16);not null" json:"action"` // approve|reject|escalate|cancel
	Comment    string     `gorm:"type:text" json:"comment"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (ExpansionApprovalStep) TableName() string { return "expansion_approval_steps" }
