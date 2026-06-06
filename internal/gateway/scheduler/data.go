package scheduler

import (
	"sync"
	"time"
)

// ControlPlaneConfig control-plane 配置同步的顶层结构
type ControlPlaneConfig struct {
	Groups []GroupConfig `json:"groups"`
	Models []GroupModel  `json:"models"`
	Data   []CPGroupData `json:"data"`
}

// CPGroupData 控制面返回的组数据结构
type CPGroupData struct {
	ID               uint            `json:"id"`
	Name             string          `json:"name"`
	Strategy         RoutingStrategy `json:"strategy"`
	Enabled          bool            `json:"enabled"`
	Description      string          `json:"description"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	StickyEnabled    bool            `json:"sticky_enabled"`
	StickyTTLSeconds int             `json:"sticky_ttl_seconds"`
	Models           []CPModelData   `json:"models"`
}

// CPModelData 控制面返回的组内模型数据
type CPModelData struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ModelType      string `json:"model_type"`
	ProviderID     string `json:"provider_id"`
	ProviderType   string `json:"provider_type"`
	ProviderName   string `json:"provider_name"`
	Endpoint       string `json:"endpoint"`
	Priority       *int   `json:"priority"`
	Weight         *int   `json:"weight"`
	ComplexityTier string `json:"complexity_tier"`
}

// GroupConfig 模型组配置
type GroupConfig struct {
	ID               uint            `json:"id"`
	Name             string          `json:"name"`
	Strategy         RoutingStrategy `json:"strategy"`
	Enabled          bool            `json:"enabled"`
	Description      string          `json:"description"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	StickyEnabled    bool            `json:"sticky_enabled"`
	StickyTTLSeconds int             `json:"sticky_ttl_seconds"`
}

// GroupModel 组内模型关联
type GroupModel struct {
	ID      uint `json:"id"`
	GroupID uint `json:"group_id"`
	ModelID uint `json:"model_id"`
	// ModelUUID 控制面模型 UUID（models.id）。ModelID 是本地合成自增序号（供
	// failover/健康探测/日志使用），而模型层配额快照的键是 UUID，必须用 ModelUUID 匹配。
	ModelUUID      string `json:"model_uuid"`
	ModelName      string `json:"model_name"`
	ModelType      string `json:"model_type"`
	ChannelType    string `json:"channel_type"`
	ProviderName   string `json:"provider_name"`
	Endpoint       string `json:"endpoint"`
	Priority       *int   `json:"priority"`
	Weight         *int   `json:"weight"`
	ComplexityTier string `json:"complexity_tier"`
}

// GroupRoutingConfig 内存中的组路由配置
type GroupRoutingConfig struct {
	GroupID          uint                 `json:"group_id"`
	GroupName        string               `json:"group_name"`
	Strategy         RoutingStrategy      `json:"strategy"`
	Enabled          bool                 `json:"enabled"`
	TimeoutSeconds   int                  `json:"timeout_seconds"`
	StickyEnabled    bool                 `json:"sticky_enabled"`
	StickyTTLSeconds int                  `json:"sticky_ttl_seconds"`
	Models           []*ModelRoutingEntry `json:"models"`
}

// ModelRoutingEntry 组内模型路由条目
type ModelRoutingEntry struct {
	ModelID uint `json:"model_id"`
	// ModelUUID 控制面模型 UUID（models.id），用于模型层配额快照键匹配。
	ModelUUID      string `json:"model_uuid"`
	ModelName      string `json:"model_name"`
	ModelType      string `json:"model_type"`
	ChannelID      uint   `json:"channel_id"`
	ChannelType    string `json:"channel_type"`
	ProviderName   string `json:"provider_name"`
	Endpoint       string `json:"endpoint"`
	Credential     string `json:"credential"`
	Priority       int    `json:"priority"`
	Weight         int    `json:"weight"`
	ComplexityTier string `json:"complexity_tier"`
}

// ChannelEndpoint 渠道连接端点信息
type ChannelEndpoint struct {
	ChannelID   uint   `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	Endpoint    string `json:"endpoint"`
}

// ModelHealthStatus 模型健康状态
type ModelHealthStatus struct {
	ModelID              uint               `json:"model_id"`
	ModelName            string             `json:"model_name"`
	Status               HealthStatus       `json:"status"`
	FailureWindow        *FailureRingBuffer `json:"-"`
	ConsecutiveFailures  int                `json:"consecutive_failures"`
	ConsecutiveSuccesses int                `json:"consecutive_successes"`
	QuickFailCount       int                `json:"quick_fail_count"`
	QuickFailWindowStart time.Time          `json:"quick_fail_window_start"`
	LastProbeTime        time.Time          `json:"last_probe_time"`
	LastFailTime         time.Time          `json:"last_fail_time"`
	LastStateChange      time.Time          `json:"last_state_change"`
	CreatedAt            time.Time          `json:"created_at"`
}

// IsHealthy 判断模型是否健康
func (m *ModelHealthStatus) IsHealthy() bool {
	return m.Status == HealthHealthy
}

// FailureRingBuffer 失败滑动窗口环形缓冲（Stage 2 T005 实现）
type FailureRingBuffer struct {
	mu       sync.Mutex
	buf      []time.Time
	capacity int
	head     int
	size     int
}
