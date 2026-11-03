package model

import (
	"time"

	"gorm.io/datatypes"
)

// 归档清单状态常量（见 docs/trace-archive-solution-v3.md §3）。
//
//	archiving → verified → cleaned
//	empty    ：该天无正文可归档（expected_body_count==0），不写指针、不置空、不计 KPI
//	suspect  ：巡检发现归档不可信，禁止进一步清理与删除
const (
	ArchiveStateArchiving = "archiving"
	ArchiveStateVerified  = "verified"
	ArchiveStateCleaned   = "cleaned"
	ArchiveStateEmpty     = "empty"
	ArchiveStateSuspect   = "suspect"
)

// 归档巡检状态常量。
const (
	ArchiveVerifyOK      = "ok"
	ArchiveVerifySuspect = "suspect"
	ArchiveVerifyUnknown = "unknown"
)

// TraceArchiveManifest 记录某一天的归档校验清单（双写：PG + 盘上 manifest.json）。
//
// v3 关键设计：expected_* 三列构成【独立于导出谓词】的冻结期望集，
// 用于消除 v2「校验分母与导出谓词同源」导致的自证循环（评审 BLK-02）。
// 见 docs/trace-archive-v3-implementation-contract.md §1.2。
type TraceArchiveManifest struct {
	DT        time.Time `gorm:"type:date;primaryKey"                                                   json:"dt"`
	State     string    `gorm:"type:varchar(16);not null;default:'archiving';index:idx_manifest_state" json:"state"`
	StateRank int16     `gorm:"not null;default:0"                                                     json:"state_rank"`

	// —— v3 冻结期望集（独立基准：谓词不含 body 非空）——
	ExpectedTotal          int64   `gorm:"not null;default:0" json:"expected_total"`
	ExpectedBodyCount      int64   `gorm:"not null;default:0" json:"expected_body_count"`
	ExpectedBodyRIDsSHA256 *string `gorm:"column:expected_body_rids_sha256;type:text" json:"expected_body_rids_sha256,omitempty"`

	// —— 归档产物 ——
	Files             datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"files"`
	RowCount          int64          `gorm:"not null;default:0"               json:"row_count"`
	UncompressedBytes int64          `gorm:"not null;default:0"               json:"uncompressed_bytes"`
	CompressedBytes   int64          `gorm:"not null;default:0"               json:"compressed_bytes"`
	SHA256            *string        `gorm:"type:text"                        json:"sha256,omitempty"`
	MinTS             *time.Time     `gorm:"type:timestamptz"                 json:"min_ts,omitempty"`
	MaxTS             *time.Time     `gorm:"type:timestamptz"                 json:"max_ts,omitempty"`

	// —— 状态时间 ——
	VerifiedAt *time.Time `gorm:"type:timestamptz" json:"verified_at,omitempty"`
	CleanedAt  *time.Time `gorm:"type:timestamptz" json:"cleaned_at,omitempty"`

	// —— v3 巡检（sha256 必须被消费，见 v3 §3.5）——
	VerifyState     string     `gorm:"type:varchar(16);not null;default:'unknown'" json:"verify_state"`
	LastInspectedAt *time.Time `gorm:"type:timestamptz"                            json:"last_inspected_at,omitempty"`
	InspectError    *string    `gorm:"type:text"                                   json:"inspect_error,omitempty"`

	// —— 重试 ——
	Attempt   int32     `gorm:"not null;default:0"     json:"attempt"`
	LastError *string   `gorm:"type:text"              json:"last_error,omitempty"`
	CreatedAt time.Time `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now()" json:"updated_at"`
}

func (TraceArchiveManifest) TableName() string { return "trace_archive_manifests" }
