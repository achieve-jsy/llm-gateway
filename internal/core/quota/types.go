package quota

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
}

// QuotaViolation 配额违规详情，用于 429 响应与结构化日志。
type QuotaViolation struct {
	SubjectType string `json:"subject_type"` // user | service_key
	SubjectID   string `json:"subject_id"`
	Layer       string `json:"layer"` // default | subject | group | model
	Gate        string `json:"gate"`  // rpm | concurrency
	Limit       int    `json:"limit"`
}

// FallbackDefaultUser 快照未就绪时用户默认配额
var FallbackDefaultUser = LayerLimits{RPM: 60, MaxConcurrency: 10}

// FallbackDefaultServiceKey 快照未就绪时 Service Key 默认配额
var FallbackDefaultServiceKey = LayerLimits{RPM: 120, MaxConcurrency: 20}
