package quota

import (
	"time"

	"github.com/google/uuid"
)

// LayerLimits 单层配额限额（RPM + 并发）。
// 值为 0 表示该层显式不限（跳过检查）。
type LayerLimits struct {
	RPM            int `json:"rpm"`
	MaxConcurrency int `json:"max_concurrency"`
}

// QuotaSnapshot 四层配额快照，由控制面发布到 Redis，gateway 热加载。
type QuotaSnapshot struct {
	Version           int                    `json:"version"`
	DefaultUser       LayerLimits            `json:"default_user"`
	DefaultServiceKey LayerLimits            `json:"default_service_key"`
	Users             map[string]LayerLimits `json:"users"`        // userUUID
	ServiceKeys       map[string]LayerLimits `json:"service_keys"` // keyUUID
	Groups            map[string]LayerLimits `json:"groups"`       // group name
	Models            map[string]LayerLimits `json:"models"`       // model UUID
	Exemptions        []QuotaExemption       `json:"exemptions"`   // 紧急豁免列表
}

// QuotaViolation 配额违规详情，用于 429 响应与结构化日志。
type QuotaViolation struct {
	SubjectType string `json:"subject_type"` // user | service_key
	SubjectID   string `json:"subject_id"`
	Layer       string `json:"layer"` // default | subject | group | model
	Gate        string `json:"gate"`  // rpm | concurrency
	Limit       int    `json:"limit"`
}

// QuotaExemption 紧急豁免记录
type QuotaExemption struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primarykey;default:gen_random_uuid()"`
	SubjectType string    `json:"subject_type" gorm:"type:varchar(32);not null"` // user | service_key
	SubjectID   string    `json:"subject_id" gorm:"type:varchar(255);not null"`  // userID or keyID
	Reason      string    `json:"reason" gorm:"type:text"`                       // 豁免原因
	ExpiresAt   time.Time `json:"expires_at" gorm:"not null"`                    // 过期时间
	CreatedBy   string    `json:"created_by" gorm:"type:varchar(255);not null"`  // 创建人 userID
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (QuotaExemption) TableName() string { return "quota_exemptions" }

// FallbackDefaultUser 快照未就绪时用户默认配额
var FallbackDefaultUser = LayerLimits{RPM: 60, MaxConcurrency: 10}

// FallbackDefaultServiceKey 快照未就绪时 Service Key 默认配额
var FallbackDefaultServiceKey = LayerLimits{RPM: 120, MaxConcurrency: 20}
