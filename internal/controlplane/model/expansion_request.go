package model

import (
	"time"

	"github.com/google/uuid"
)

// 扩容审批状态
const (
	ExpansionStatusPending   = "pending"
	ExpansionStatusApproved  = "approved"
	ExpansionStatusRejected  = "rejected"
	ExpansionStatusCancelled = "cancelled"
)

// 审批级别
const (
	ExpansionLevelDept     = "dept"     // 部门负责人受理成员额度申请
	ExpansionLevelStudio   = "studio"   // 工作室负责人受理
	ExpansionLevelPlatform = "platform" // 平台管理员受理（工作室盘子不足时升级）
)

// 审批动作
const (
	ExpansionActionApprove  = "approve"
	ExpansionActionReject   = "reject"
	ExpansionActionEscalate = "escalate" // 升级到上一级
	ExpansionActionCancel   = "cancel"
)

// ExpansionRequest 扩容申请主表（自建审批）。
// approved ≠ 自动改额，审批通过后由上级手动走 PUT /api/v1/budgets 改额（FR-4.6）。
type ExpansionRequest struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType       string    `gorm:"type:varchar(16);not null" json:"scope_type"` // dept_daily|dept_service|studio
	ScopeID         string    `gorm:"type:varchar(64);not null;index" json:"scope_id"`
	RequesterID     uuid.UUID `gorm:"type:uuid;not null;index" json:"requester_id"`
	CurrentAmount   float64   `gorm:"type:numeric(12,4)" json:"current_amount"`
	RequestedAmount float64   `gorm:"type:numeric(12,4);not null" json:"requested_amount"`
	Reason          string    `gorm:"type:text" json:"reason"`
	Status          string    `gorm:"type:varchar(16);not null;default:'pending';index" json:"status"` // pending|approved|rejected|cancelled
	CurrentLevel    string    `gorm:"type:varchar(16);not null" json:"current_level"`                  // dept|studio|platform 当前待审级别
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (ExpansionRequest) TableName() string { return "expansion_requests" }

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
