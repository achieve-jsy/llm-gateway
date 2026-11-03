package model

import (
	"time"

	"gorm.io/datatypes"
)

// TraceArchiveManifest 记录某一天正文归档的校验清单（双写：PG + 盘上 _manifest/manifest-*.json）。
// 状态机 3 态：archiving / verified / cleaned（见 docs/trace-archive-final-design.md §2.1/§5）。
type TraceArchiveManifest struct {
	DT         time.Time      `gorm:"type:date;primaryKey"                    json:"dt"`
	State      string         `gorm:"type:varchar(16);not null;default:'archiving';index:idx_manifest_state" json:"state"`
	StateRank  int16          `gorm:"not null;default:0"                       json:"state_rank"`
	Files      datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"         json:"files"`
	RowCount   int64          `gorm:"not null;default:0"                       json:"row_count"`
	PgRowCount int64          `gorm:"not null;default:0"                       json:"pg_row_count"`
	SHA256     *string        `gorm:"type:text"                                json:"sha256,omitempty"`
	MinTS      *time.Time     `gorm:"type:timestamptz"                         json:"min_ts,omitempty"`
	MaxTS      *time.Time     `gorm:"type:timestamptz"                         json:"max_ts,omitempty"`
	VerifiedAt *time.Time     `gorm:"type:timestamptz"                         json:"verified_at,omitempty"`
	CleanedAt  *time.Time     `gorm:"type:timestamptz"                         json:"cleaned_at,omitempty"`
	Attempt    int32          `gorm:"not null;default:0"                       json:"attempt"`
	LastError  *string        `gorm:"type:text"                                json:"last_error,omitempty"`
	CreatedAt  time.Time      `gorm:"not null;default:now()"                   json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now()"                   json:"updated_at"`
}

func (TraceArchiveManifest) TableName() string { return "trace_archive_manifests" }
