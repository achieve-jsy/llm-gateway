package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrDepartment struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID            string     `gorm:"type:varchar(64);not null;index:idx_ehr_dept_org_id" json:"org_id"`
	DepartmentID     string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_dept_org_dept" json:"department_id"`
	DepartmentCode   string     `gorm:"type:varchar(64);not null;default:''" json:"department_code"`
	DepartmentName   string     `gorm:"type:varchar(255);not null" json:"department_name"`
	DepartmentType   int32      `gorm:"not null;default:0" json:"department_type"`
	DepartmentTypeName string   `gorm:"type:varchar(64);not null;default:''" json:"department_type_name"`
	ParentID         string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_dept_parent" json:"parent_id"`
	LeaderName       string     `gorm:"type:varchar(128);not null;default:''" json:"leader_name"`
	LeaderPassport   string     `gorm:"type:varchar(128);not null;default:''" json:"leader_passport"`
	LeaderPersonID   string     `gorm:"type:varchar(64);not null;default:''" json:"leader_person_id"`
	LeaderEmployeeID string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_dept_leader_emp" json:"leader_employee_id"`
	ManagerName      string     `gorm:"type:varchar(128);not null;default:''" json:"manager_name"`
	ManagerPassport  string     `gorm:"type:varchar(128);not null;default:''" json:"manager_passport"`
	ManagerPersonID  string     `gorm:"type:varchar(64);not null;default:''" json:"manager_person_id"`
	ManagerEmployeeID string   `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_dept_manager_emp" json:"manager_employee_id"`
	HrBpName         string     `gorm:"type:varchar(128);not null;default:''" json:"hr_bp_name"`
	HrBpEmployeeID   string     `gorm:"type:varchar(64);not null;default:''" json:"hr_bp_employee_id"`
	EhrModifyDate    string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_dept_modify_date" json:"ehr_modify_date"`
	ManualOverrideAt *time.Time `gorm:"column:manual_override_at;type:timestamp with time zone;serializer:tztime" json:"manual_override_at,omitempty"`
	ManualOverrideBy string     `gorm:"type:varchar(128);not null;default:''" json:"manual_override_by"`
	ManualOverrideReason string `gorm:"type:varchar(500);not null;default:''" json:"manual_override_reason"`
	SyncedAt         time.Time  `gorm:"not null;autoCreateTime" json:"synced_at"`
	CreatedAt        time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EhrDepartment) TableName() string { return "ehr_departments" }

// GetOverride 满足 ehrsync.HasOverride 接口，用于 skip entry 填充 override 时间
func (m *EhrDepartment) GetOverride() *time.Time { return m.ManualOverrideAt }

func (m *EhrDepartment) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
