package planning

// ModelCapabilities 是 planning 层可消费的轻量能力摘要。
// 它不直接绑定 DB/GORM/snapshot 结构，便于 shadow mode 纯函数测试。
type ModelCapabilities struct {
	SupportsTools            bool `json:"supports_tools,omitempty"`
	SupportsReasoningEffort  bool `json:"supports_reasoning_effort,omitempty"`
	SupportsReasoningSummary bool `json:"supports_reasoning_summary,omitempty"`
	SupportsResponses        bool `json:"supports_responses,omitempty"`
	SupportsStreaming        bool `json:"supports_streaming,omitempty"`
	ContextWindow            int  `json:"context_window,omitempty"`
	MaxOutputTokens          int  `json:"max_output_tokens,omitempty"`
}

// TransformOverrides 是受控的转换覆盖声明。Phase 2 只进入 plan/audit，
// 不执行任何请求改写；Force 的值不会进入 ProtocolAudit，避免泄露敏感信息。
type TransformOverrides struct {
	Drop   []string          `json:"drop,omitempty"`
	Rename map[string]string `json:"rename,omitempty"`
	Force  map[string]any    `json:"force,omitempty"`
}

// UpstreamTarget 表示一次路由后“调谁”。
type UpstreamTarget struct {
	PublicModel   string
	LogicalModel  string
	BaseModel     string
	UpstreamModel string

	ModelID      string
	ProviderID   string
	ProviderName string
	ProviderType string
	ChannelID    string

	BaseURL       string
	APIVersion    string
	CredentialRef string

	ProtocolModeOverride    ProtocolMode
	ParamProfileOverride    ParamProfile
	UnsupportedParamPolicy  UnsupportedParamPolicy
	Capabilities            ModelCapabilities
	SupportedParamsOverride []string
	TransformOverrides      TransformOverrides
}

// RouteDecision 是 scheduler/group routing 产物的最小抽象。
type RouteDecision struct {
	ModelID       string
	ModelName     string
	UpstreamModel string
	ProviderID    string
	ProviderName  string
	ProviderType  string
	ChannelID     string
	BaseURL       string
	APIVersion    string
	CredentialRef string
}

// ModelMetadata 是 provider registry / runtime snapshot 可提供的模型绑定元数据。
type ModelMetadata struct {
	ModelID                 string
	ModelName               string
	BaseModel               string
	UpstreamModel           string
	ProviderID              string
	ProviderName            string
	ProviderType            string
	ChannelID               string
	BaseURL                 string
	APIVersion              string
	CredentialRef           string
	ProtocolMode            ProtocolMode
	ParamProfile            ParamProfile
	UnsupportedParamPolicy  UnsupportedParamPolicy
	Capabilities            ModelCapabilities
	SupportedParamsOverride []string
	TransformOverrides      TransformOverrides
}

// RouteInput 是 TargetResolver 的纯函数输入，不依赖 gin、DB 或 provider 实例。
type RouteInput struct {
	Kind         RequestKind
	PublicModel  string
	LogicalModel string
	Decision     RouteDecision
	Metadata     ModelMetadata
}
