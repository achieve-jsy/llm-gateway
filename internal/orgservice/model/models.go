package model

import "time"

// ─── 工作室 ───────────────────────────────────────────────────────────────────

// Studio 工作室实体
type Studio struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_studios_name" json:"name"`
	OwnerID   string    `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (Studio) TableName() string { return "studios" }

// ─── 部门（扩展现有 departments 表）────────────────────────────────────────────

// DeptStatus 部门组织归集状态
type DeptStatus string

const (
	DeptStatusActive        DeptStatus = "active"
	DeptStatusPendingStudio DeptStatus = "pending_studio"
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
	RoleUser         RoleType = "user"
	RoleDeptAdmin    RoleType = "dept_admin"
	RoleStudioAdmin  RoleType = "studio_admin"
	RoleAdmin        RoleType = "admin"
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
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      string    `gorm:"type:varchar(64);not null;index:idx_migration_log_user_id" json:"user_id"`
	FromDept    string    `gorm:"type:varchar(64);not null" json:"from_dept"`
	ToDept      string    `gorm:"type:varchar(64);not null" json:"to_dept"`
	MigratedAt  time.Time `gorm:"not null;autoCreateTime" json:"migrated_at"`
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
	DeptID   string // departments.dept_id
	StudioID string // studios.id（空表示 pending_studio）
	IsPending bool
}

// TargetScope 管辖域目标（用于 CheckScope 判定）
type TargetScope struct {
	DeptID   string
	StudioID string
}

// ImportRow 映射导入行
type ImportRow struct {
	DeptID   string
	StudioID string
}

// ImportResult 映射导入结果
type ImportResult struct {
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Backfilled int `json:"backfilled"`
}

// ImportDiff 映射导入预览 diff
type ImportDiff struct {
	Added    []ImportRow `json:"added"`
	Updated  []struct {
		DeptID      string `json:"dept_id"`
		OldStudioID string `json:"old_studio_id"`
		NewStudioID string `json:"new_studio_id"`
	} `json:"updated"`
	Unchanged int `json:"unchanged"`
}

// ─── 员工花名册导入 ───────────────────────────────────────────────────────────

// EmployeeImportRow 花名册 CSV/XLSX 的一行原始数据
type EmployeeImportRow struct {
	RowIndex    int    // 原始行号（从 2 起，跳过表头）
	EmployeeID  string // 工号
	Name        string // 姓名
	Email       string // 邮箱（匹配 users 的主键）
	StudioName  string // 事业部
	DeptName    string // 部门
	JobFamily   string // 岗位族群
	JobCategory string // 岗位类别
	JobTitle    string // 岗位
}

// EmployeeImportRowStatus 导入预览行状态
type EmployeeImportRowStatus string

const (
	ImportStatusLinked      EmployeeImportRowStatus = "linked"       // 邮箱匹配，自动归属
	ImportStatusUnmatched   EmployeeImportRowStatus = "unmatched"    // 邮箱未匹配到用户（需手动指定）
	ImportStatusNewStudio   EmployeeImportRowStatus = "new_studio"   // 工作室将被创建
	ImportStatusNewDept     EmployeeImportRowStatus = "new_dept"     // 部门将被创建
)

// EmployeeImportPreviewRow 预览行（dry_run 响应）
type EmployeeImportPreviewRow struct {
	RowIndex    int                     `json:"row_index"`
	EmployeeID  string                  `json:"employee_id"`
	Name        string                  `json:"name"`
	Email       string                  `json:"email"`
	StudioName  string                  `json:"studio_name"`
	DeptName    string                  `json:"dept_name"`
	JobTitle    string                  `json:"job_title"`
	Status      EmployeeImportRowStatus `json:"status"`
	NewStudio   bool                    `json:"new_studio"`   // 该工作室将被新建
	NewDept     bool                    `json:"new_dept"`     // 该部门将被新建
	// 仅当 Status=linked 时有值
	MatchedUserID   string `json:"matched_user_id,omitempty"`
	MatchedUserName string `json:"matched_user_name,omitempty"`
	// 仅当 Status=unmatched 时，管理员可填入手动指定的 user_id
	ManualUserID string `json:"manual_user_id,omitempty"`
}

// EmployeeImportRequest 正式导入请求体（含手动覆盖）
type EmployeeImportRequest struct {
	// 对 unmatched 行的手动指定：row_index -> user_id
	Overrides map[int]string `json:"overrides"`
}

// EmployeeImportSummary 导入结果汇总
type EmployeeImportSummary struct {
	Total          int      `json:"total"`
	StudiosCreated int      `json:"studios_created"`
	DeptsCreated   int      `json:"depts_created"`
	DeptsMapped    int      `json:"depts_mapped"`
	UsersLinked    int      `json:"users_linked"`
	UsersPending   int      `json:"users_pending"`
	UnmatchedEmails []string `json:"unmatched_emails"`
}
