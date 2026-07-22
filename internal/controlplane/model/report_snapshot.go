package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ReportLevel report_snapshot.level 枚举
const (
	ReportLevelPersonal   = "personal"
	ReportLevelDepartment = "department"
	ReportLevelStudio     = "studio"
)

// ReportSnapshot Token 使用分析报告存档（周期性 job 写，页面/MD/HTML 都读它保证一致）
// 参考 docs/token-usage-analysis-report-implementation.md §10.3
// 唯一索引 (level, scope_id, period_start) 保证同一 level+scope+周期只有一份存档（幂等 upsert）。
type ReportSnapshot struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Level        string         `gorm:"type:varchar(16);not null;uniqueIndex:uniq_report_snapshot;check:level IN ('personal','department','studio')" json:"level"`
	ScopeID      string         `gorm:"type:text;not null;uniqueIndex:uniq_report_snapshot" json:"scope_id"`
	PeriodType   string         `gorm:"type:varchar(8);not null;check:period_type IN ('day','week','month')" json:"period_type"`
	PeriodStart  time.Time      `gorm:"type:date;not null;uniqueIndex:uniq_report_snapshot" json:"period_start"`
	PeriodEnd    time.Time      `gorm:"type:date;not null" json:"period_end"`
	ReportJSON   datatypes.JSON `gorm:"column:report_json;type:jsonb;not null;default:'{}'" json:"report_json"`
	SchemaVersion string        `gorm:"column:schema_version;type:varchar(16);not null;default:'2.0'" json:"schema_version"`
	GeneratedAt  time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"generated_at"`
}

func (ReportSnapshot) TableName() string { return "report_snapshot" }
