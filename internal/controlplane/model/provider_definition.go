package model

import "time"

// CredentialField 凭证字段 schema 定义
type CredentialField struct {
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
}

// CredentialSchema 凭证 schema（字段名 → 字段定义）
type CredentialSchema map[string]CredentialField

// VendorOptions 厂商差异化配置
type VendorOptions struct {
	// URL 构造模式：standard（baseURL+/chat/completions）/ azure（/openai/deployments/{model}/...）/ volcengine（含 model path）
	URLPattern string `json:"url_pattern,omitempty"`
	// 认证 Header 方式：bearer（Authorization: Bearer xxx）/ api-key（api-key: xxx）/ custom
	AuthHeader string `json:"auth_header,omitempty"`
	// 是否需要 api_version 参数（Azure 专用）
	RequiresAPIVersion bool `json:"requires_api_version,omitempty"`
	// api_version 默认值（Azure 等厂商在 URL query 中追加的协议版本号，如 "2025-04-01-preview"）
	APIVersion string `json:"api_version,omitempty"`
	// 最后一个 chunk 是否含 usage 信息
	StreamUsageInLastChunk bool `json:"stream_usage_in_last_chunk,omitempty"`
	// 是否跳过空 choices 的 chunk（Azure/某些厂商会发送）
	EmptyChoicesSkip bool `json:"empty_choices_skip,omitempty"`
	// 是否支持 stream_options（请求中添加 stream_options: {include_usage: true}）
	SupportsStreamOptions bool `json:"supports_stream_options,omitempty"`
	// 是否支持 reasoning_content 字段（DeepSeek/QwQ 等思维链模型）
	SupportsReasoningContent bool `json:"supports_reasoning_content,omitempty"`
	// 自定义请求头（key-value 对，在默认 Header 之外追加）
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	// 跳过转发检测（非 OpenAI 兼容接口，如 AWS Bedrock，无法直接 POST /chat/completions）
	SkipForwardCheck bool `json:"skip_forward_check,omitempty"`
	// 默认 Region（用于自动构造 Endpoint，如 Bedrock）
	DefaultRegion string `json:"default_region,omitempty"`
}

// ProviderDefinition 渠道定义（Registry 条目）
type ProviderDefinition struct {
	ID               string           `gorm:"type:varchar(50);primaryKey" json:"id"`
	DisplayName      string           `gorm:"type:varchar(100);not null" json:"display_name"`
	Adapter          string           `gorm:"type:varchar(50);not null" json:"adapter"`
	BaseURL          string           `gorm:"type:varchar(500);not null;default:''" json:"base_url"`
	CredentialSchema CredentialSchema `gorm:"type:jsonb;not null;default:'{}';serializer:json" json:"credential_schema"`
	VendorOptions    VendorOptions    `gorm:"type:jsonb;not null;default:'{}';serializer:json" json:"vendor_options"`
	Description      string           `gorm:"type:varchar(500);not null;default:''" json:"description"`
	IconURL          string           `gorm:"type:varchar(500);default:''" json:"icon_url"`
	DocURL           string           `gorm:"type:varchar(500);default:''" json:"doc_url"`
	DefaultModels    []string         `gorm:"type:jsonb;not null;default:'[]';serializer:json" json:"default_models"`
	Builtin          bool             `gorm:"not null;default:false" json:"builtin"`
	Enabled          bool             `gorm:"not null;default:true" json:"enabled"`
	CreatedAt        time.Time        `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time        `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ProviderDefinition) TableName() string {
	return "provider_definitions"
}
