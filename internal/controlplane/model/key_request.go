package model

import (
	"time"

	"github.com/google/uuid"
)

// Key 创建申请状态
const (
	KeyRequestStatusPending    = "pending"
	KeyRequestStatusProcessing = "processing"
	KeyRequestStatusApproved   = "approved"
	KeyRequestStatusRejected   = "rejected"
	KeyRequestStatusCancelled  = "cancelled"
)

// 部门策略 key
const (
	DeptPolicyKeyCreationPolicy = "key_creation_policy"
)

// 部门策略值
const (
	KeyCreationPolicyFree     = "free"     // 无需审批，自由创建
	KeyCreationPolicyApproval = "approval" // 需要部门负责人审批
)

// DeptPolicy 部门级策略配置（KV，每个部门可有多个策略）。
// DeptID 与 api_keys.dept_id 同语义（SSO dept_id varchar）。
type DeptPolicy struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeptID      string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_dept_policies_uniq" json:"dept_id"`
	PolicyKey   string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_dept_policies_uniq" json:"policy_key"`
	PolicyValue string    `gorm:"type:varchar(255);not null;default:''" json:"policy_value"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	UpdatedBy   uuid.UUID `gorm:"type:uuid" json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (DeptPolicy) TableName() string { return "dept_policies" }

// KeyRequest Key 创建申请单（可附带个人月预算申请）。
// 审批流：仅部门级，由 dept_admin/approver 处理。
type KeyRequest struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	RequesterID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"requester_id"`
	RequesterName  string     `gorm:"type:varchar(128)" json:"requester_name,omitempty"`
	DeptID         string     `gorm:"type:varchar(64);index" json:"dept_id,omitempty"`
	DeptName       string     `gorm:"type:varchar(128)" json:"dept_name,omitempty"`
	KeyName        string     `gorm:"type:varchar(128);not null" json:"key_name"`
	KeyType        string     `gorm:"type:varchar(16);not null;default:'personal'" json:"key_type"`
	Purpose        string     `gorm:"type:text" json:"purpose,omitempty"`
	GroupIDs       string     `gorm:"type:text" json:"group_ids,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	BudgetMonthly  float64    `gorm:"type:numeric(12,4);default:0" json:"budget_monthly"`
	Status         string     `gorm:"type:varchar(16);not null;default:'pending';index" json:"status"`
	ApproverID     *uuid.UUID `gorm:"type:uuid" json:"approver_id,omitempty"`
	ApproverName   string     `gorm:"type:varchar(128)" json:"approver_name,omitempty"`
	ApproveComment string     `gorm:"type:text" json:"approve_comment,omitempty"`
	CreatedKeyID   *uuid.UUID `gorm:"type:uuid" json:"created_key_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (KeyRequest) TableName() string { return "key_requests" }
