package model

import (
	"time"

	"github.com/google/uuid"
)

// AIAppStudio AI 应用聚合页的工作室主数据。
//
// 为什么不用 legacy 的 studios 表：studios / dept_studio_map / user_roles 只存在于
// legacy migrations/043，没有纳入 Atlas 迁移目录（见 atlas-migrations 中
// 20261014063200_add_call_logs_call_type.sql 的说明）。直接给它们加字段会让
// atlas diff 产出 CREATE TABLE，无法在已有库上重放。因此聚合页的工作室清单单独维护，
// 由 EHR 同步 + 人工覆盖，与 RBAC 用的 studios 解耦。
type AIAppStudio struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	// DepartmentID EHR 的 departmentId（departmentType=2 的事业部）。手工维护的工作室可以为空，
	// 因此用可空列 + 唯一索引（Postgres 允许多个 NULL）。
	DepartmentID     *string `gorm:"type:varchar(64);uniqueIndex:uniq_ai_app_studios_department_id" json:"department_id"`
	Name             string  `gorm:"type:varchar(128);not null;default:''" json:"name"`
	LeaderName       string  `gorm:"type:varchar(128);not null;default:''" json:"leader_name"`
	LeaderPassport   string  `gorm:"type:varchar(128);not null;default:'';index:idx_ai_app_studios_leader" json:"leader_passport"`
	LeaderEmployeeID string  `gorm:"type:varchar(64);not null;default:''" json:"leader_employee_id"`
	// Source ehr | manual：EHR 同步来的行会被覆盖，manual 行不会被同步任务改动。
	Source string `gorm:"type:varchar(16);not null;default:'ehr'" json:"source"`
	// Visible 删除/排除不物理删：公共关系事业部、元气工作室这类先置 false。
	Visible   bool      `gorm:"not null;default:true" json:"visible"`
	SortOrder int32     `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now()" json:"updated_at"`
}

func (AIAppStudio) TableName() string { return "ai_app_studios" }

// AIAppDepartment EHR 组织节点镜像（部门/分组/事业部/工作室，共 99 条）。
//
// 落本地是为了在 H5 请求时把「用户所在部门」沿 parent_id 上溯到事业部，
// 不必每次实时调 EHR；同时保留 leader 信息用于负责人判定。
// StudioID 在建树时回填：type=2 的节点指向自身对应的 ai_app_studios.id，
// 其余节点指向最近的事业部祖先；没有事业部祖先的为 NULL。
type AIAppDepartment struct {
	DepartmentID   string     `gorm:"column:department_id;type:varchar(64);primaryKey" json:"department_id"`
	Name           string     `gorm:"column:name;type:varchar(128);not null;default:''" json:"name"`
	ParentID       string     `gorm:"column:parent_id;type:varchar(64);not null;default:'';index:idx_ai_app_departments_parent" json:"parent_id"`
	DepartmentType int32      `gorm:"column:department_type;not null;default:0" json:"department_type"`
	DepartmentPath string     `gorm:"column:department_path;type:varchar(512);not null;default:''" json:"department_path"`
	StudioID       *uuid.UUID `gorm:"column:studio_id;type:uuid;index:idx_ai_app_departments_studio" json:"studio_id"`
	LeaderName     string     `gorm:"column:leader_name;type:varchar(128);not null;default:''" json:"leader_name"`
	LeaderPassport string     `gorm:"column:leader_passport;type:varchar(128);not null;default:''" json:"leader_passport"`
	SyncedAt       time.Time  `gorm:"column:synced_at;not null;default:now()" json:"synced_at"`
}

func (AIAppDepartment) TableName() string { return "ai_app_departments" }

// AIApp AI 应用。描述上限 200 字对应需求；热度 0-3；is_new 由运营手工勾选。
type AIApp struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	StudioID    uuid.UUID `gorm:"type:uuid;not null;index:idx_ai_apps_studio_status,priority:1;uniqueIndex:uniq_ai_apps_studio_name,priority:1" json:"studio_id"`
	Name        string    `gorm:"type:varchar(128);not null;uniqueIndex:uniq_ai_apps_studio_name,priority:2" json:"name"`
	Description string    `gorm:"type:varchar(200);not null;default:''" json:"description"`
	IconURL     string    `gorm:"type:varchar(500);not null;default:''" json:"icon_url"`
	URL         string    `gorm:"type:varchar(1000);not null;default:''" json:"url"`
	Heat        int16     `gorm:"type:smallint;not null;default:0;check:heat_between_0_and_3,heat >= 0 AND heat <= 3" json:"heat"`
	IsNew       bool      `gorm:"not null;default:false" json:"is_new"`
	Status      string    `gorm:"type:varchar(16);not null;default:'published';index:idx_ai_apps_studio_status,priority:2" json:"status"` // draft | published | offline，取值在服务层校验
	SortOrder   int32     `gorm:"not null;default:0" json:"sort_order"`
	CreatedBy   string    `gorm:"type:varchar(128);not null;default:''" json:"created_by"`
	UpdatedBy   string    `gorm:"type:varchar(128);not null;default:''" json:"updated_by"`
	CreatedAt   time.Time `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;default:now()" json:"updated_at"`
}

func (AIApp) TableName() string { return "ai_apps" }

// AIAppHubLoginLog H5 首屏取数成功后的访问日志（异步写，不阻塞请求）。
type AIAppHubLoginLog struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string     `gorm:"type:varchar(64);not null;default:''" json:"user_id"`
	Passport  string     `gorm:"type:varchar(128);not null;default:''" json:"passport"`
	UserName  string     `gorm:"type:varchar(128);not null;default:''" json:"user_name"`
	DeptID    string     `gorm:"type:varchar(64);not null;default:''" json:"dept_id"`
	StudioID  *uuid.UUID `gorm:"type:uuid" json:"studio_id"`
	Scope     string     `gorm:"type:varchar(16);not null;default:''" json:"scope"`
	IP        string     `gorm:"type:varchar(64);not null;default:''" json:"ip"`
	UserAgent string     `gorm:"type:varchar(255);not null;default:''" json:"user_agent"`
	CreatedAt time.Time  `gorm:"not null;default:now();index:idx_ai_app_hub_login_logs_created_at" json:"created_at"`
}

func (AIAppHubLoginLog) TableName() string { return "ai_app_hub_login_logs" }
