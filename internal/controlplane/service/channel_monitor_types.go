package service

import "time"

// ChannelMonitorView 渠道监控 service 层视图（解密后）
type ChannelMonitorView struct {
	ID              int64
	Name            string
	Provider        string
	APIMode         string
	Endpoint        string
	APIKey          string // 解密后的明文
	PrimaryModel    string
	ExtraModels     []string
	GroupName       string
	Enabled         bool
	IntervalSeconds int
	LastCheckedAt   *time.Time
	CreatedBy       string
	ExtraHeaders     map[string]string
	BodyOverrideMode string
	BodyOverride     map[string]interface{}
	TemplateID       *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	APIKeyDecryptFailed bool
}

// ChannelMonitorListParams 列表查询参数
type ChannelMonitorListParams struct {
	Page     int
	PageSize int
	Provider string
	Enabled  *bool
	Search   string
}

// ChannelMonitorCreateParams 创建参数
type ChannelMonitorCreateParams struct {
	Name             string
	Provider         string
	APIMode          string
	Endpoint         string
	APIKey           string
	PrimaryModel     string
	ExtraModels      []string
	GroupName        string
	Enabled          bool
	IntervalSeconds  int
	CreatedBy        string
	TemplateID       *int64
	ExtraHeaders     map[string]string
	BodyOverrideMode string
	BodyOverride     map[string]interface{}
}

// ChannelMonitorUpdateParams 更新参数
type ChannelMonitorUpdateParams struct {
	Name             *string
	Provider         *string
	APIMode          *string
	Endpoint         *string
	APIKey           *string
	PrimaryModel    *string
	ExtraModels      *[]string
	GroupName        *string
	Enabled          *bool
	IntervalSeconds  *int
	TemplateID       *int64
	ClearTemplate    bool
	ExtraHeaders     *map[string]string
	BodyOverrideMode *string
	BodyOverride     *map[string]interface{}
}

// ChannelCheckResult 单次检测结果
type ChannelCheckResult struct {
	Model         string
	Status        string // operational / degraded / failed / error
	LatencyMs     *int
	PingLatencyMs *int
	Message       string
	CheckedAt     time.Time
}

// UserMonitorView 用户只读视图
type UserMonitorView struct {
	ID                   int64
	Name                 string
	Provider             string
	GroupName            string
	PrimaryModel         string
	PrimaryStatus        string
	PrimaryLatencyMs     *int
	PrimaryPingLatencyMs *int
	Availability7d       float64
	ExtraModels          []ExtraModelStatus
	Timeline             []UserMonitorTimelinePoint
}

// UserMonitorTimelinePoint 用户视图 timeline 单点
type UserMonitorTimelinePoint struct {
	Status        string    `json:"status"`
	LatencyMs     *int      `json:"latency_ms"`
	PingLatencyMs *int      `json:"ping_latency_ms"`
	CheckedAt     time.Time `json:"checked_at"`
}

// ExtraModelStatus 附加模型最近状态
type ExtraModelStatus struct {
	Model     string
	Status    string
	LatencyMs *int
}

// UserMonitorDetail 用户只读详情视图
type UserMonitorDetail struct {
	ID        int64
	Name      string
	Provider  string
	GroupName string
	Models    []ModelDetail
}

// ModelDetail 单个模型可用率/延迟统计
type ModelDetail struct {
	Model           string
	LatestStatus    string
	LatestLatencyMs *int
	Availability7d  float64
	Availability15d float64
	Availability30d float64
	AvgLatency7dMs  *int
}

// MonitorStatusSummary 监控状态聚合（admin list 用）
type MonitorStatusSummary struct {
	PrimaryStatus    string
	PrimaryLatencyMs *int
	Availability7d   float64
	ExtraModels      []ExtraModelStatus
}

// SecretEncryptor 加密接口
type SecretEncryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// MonitorScheduler 调度器接口
type MonitorScheduler interface {
	Schedule(m *ChannelMonitorView)
	Unschedule(id int64)
}