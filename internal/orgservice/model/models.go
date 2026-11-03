package model

import "time"

// ─── 工作室 ───────────────────────────────────────────────────────────────────

// Studio 工作室实体
type Studio struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_studios_name" json:"name"`
	OwnerID   string    `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`

	// EHRDepartmentID EHR 事业部部门 ID：同步的唯一匹配键。手工建的工作室为 NULL。
	// 一旦回填终身不变 —— api_keys / user_roles / call_logs / usage_daily 里的
	// studio_id 快照全靠 studios.id 稳定，改名只能改 Name。
	EHRDepartmentID *string `gorm:"type:varchar(64);uniqueIndex:uniq_studios_ehr_dept" json:"ehr_department_id,omitempty"`
	// Visible 组织视图是否可见。同步只会把它置 false（EHR 里消失），永不自动置回 true。
	Visible bool `gorm:"not null;default:true" json:"visible"`
	// Source 数据来源：ehr（同步维护）/ manual（手工残留，同步完全忽略）。
	Source           string `gorm:"type:varchar(16);not null;default:manual" json:"source"`
	LeaderName       string `gorm:"type:varchar(128);not null;default:''" json:"leader_name,omitempty"`
	LeaderPassport   string `gorm:"type:varchar(64);not null;default:''" json:"leader_passport,omitempty"`
	LeaderEmployeeID string `gorm:"type:varchar(64);not null;default:''" json:"leader_employee_id,omitempty"`
}

func (Studio) TableName() string { return "studios" }

// ─── 部门（扩展现有 departments 表）────────────────────────────────────────────

// DeptStatus 部门组织归集状态
type DeptStatus string

const (
	DeptStatusActive        DeptStatus = "active"
	DeptStatusPendingStudio DeptStatus = "pending_studio"
	// DeptStatusDeleted EHR 里已经不存在的部门：软删除，行保留，用量 join 仍可命中。
	DeptStatusDeleted DeptStatus = "deleted"
)

// OrgDepartment 组织视角的部门（扩展字段，与 auth 包中的 Department 共享同一张表）
// 只操作新增字段；对现有字段只读（由 auth 包管理）
// id 与数据库保持一致：uuid PRIMARY KEY DEFAULT gen_random_uuid()
type OrgDepartment struct {
	ID        string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeptID    string     `gorm:"type:varchar(64);not null;uniqueIndex" json:"dept_id"` // SSO dept_id
	DeptName  string     `gorm:"type:varchar(128);not null" json:"dept_name"`
	StudioID  *string    `gorm:"type:uuid;index:idx_departments_studio_id" json:"studio_id,omitempty"`
	Status    DeptStatus `gorm:"type:varchar(32);not null;default:pending_studio;index:idx_departments_status" json:"status"`
	OwnerID   *string    `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	CreatedAt time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (OrgDepartment) TableName() string { return "departments" }

func (d *OrgDepartment) GetStudioID() string {
	if d.StudioID == nil {
		return ""
	}
	return *d.StudioID
}

// ─── 部门↔工作室映射 ─────────────────────────────────────────────────────────

type DeptStudioMap struct {
	DeptID    string    `gorm:"type:varchar(64);primaryKey" json:"dept_id"`
	StudioID  string    `gorm:"type:uuid;not null;index:idx_dept_studio_map_studio_id" json:"studio_id"`
	Source    string    `gorm:"type:varchar(16);not null;default:manual" json:"source"` // import | manual
	UpdatedBy string    `gorm:"type:varchar(64);not null" json:"updated_by"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (DeptStudioMap) TableName() string { return "dept_studio_map" }

// ─── RBAC 角色授权 ───────────────────────────────────────────────────────────

// RoleType 角色类型
type RoleType string

const (
	RoleUser        RoleType = "user"
	RoleDeptAdmin   RoleType = "dept_admin"
	RoleStudioAdmin RoleType = "studio_admin"
	RoleAdmin       RoleType = "admin"
)

// ScopeType 管辖域类型
type ScopeType string

const (
	ScopeSelf    ScopeType = "self"
	ScopeDept    ScopeType = "dept"
	ScopeStudio  ScopeType = "studio"
	ScopeCompany ScopeType = "company"
)

// UserRole RBAC 角色授权记录
type UserRole struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:varchar(64);not null;index:idx_user_roles_user_id" json:"user_id"`
	Role      RoleType  `gorm:"type:varchar(32);not null" json:"role"`
	ScopeType ScopeType `gorm:"type:varchar(32);not null;index:idx_user_roles_scope,composite:scope_idx" json:"scope_type"`
	ScopeID   string    `gorm:"type:varchar(64);not null;default:'';index:idx_user_roles_scope,composite:scope_idx" json:"scope_id"`
	GrantedBy string    `gorm:"type:varchar(64);not null" json:"granted_by"`
	ActorID   string    `gorm:"type:varchar(64)" json:"actor_id,omitempty"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (UserRole) TableName() string { return "user_roles" }

// ─── 迁移日志 ────────────────────────────────────────────────────────────────

// OrgMigrationLog 用户部门迁移历史
type OrgMigrationLog struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     string    `gorm:"type:varchar(64);not null;index:idx_migration_log_user_id" json:"user_id"`
	FromDept   string    `gorm:"type:varchar(64);not null" json:"from_dept"`
	ToDept     string    `gorm:"type:varchar(64);not null" json:"to_dept"`
	MigratedAt time.Time `gorm:"not null;autoCreateTime" json:"migrated_at"`
}

func (OrgMigrationLog) TableName() string { return "org_migration_log" }

// ─── 业务 DTO ────────────────────────────────────────────────────────────────

// AggregateRequest SSO 归集请求
type AggregateRequest struct {
	UserID   string // users.id (UUID string)
	Email    string
	DeptID   string // SSO dept_id（EhrDeptID）
	DeptName string
}

// AggregateResult SSO 归集结果
type AggregateResult struct {
	DeptID    string // departments.dept_id
	StudioID  string // studios.id（空表示 pending_studio）
	IsPending bool
}

// TargetScope 管辖域目标（用于 CheckScope 判定）
type TargetScope struct {
	DeptID   string
	StudioID string
}
