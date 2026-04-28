package adapters

import (
	"fmt"
	"strings"
	"sync"
)

// ProtocolMode 标识上游协议模式。与 planning 包后续枚举保持字符串兼容。
type ProtocolMode string

const (
	ProtocolAuto             ProtocolMode = "auto"
	ProtocolOpenAIChat       ProtocolMode = "openai_chat"
	ProtocolOpenAICompatible ProtocolMode = "openai_compatible"
	ProtocolPassthrough      ProtocolMode = "passthrough"
)

// ParamProfile 标识参数适配画像。adapter 原型只依赖字符串，避免耦合未完成 planner。
type ParamProfile string

const (
	ParamProfileAuto                ParamProfile = "auto"
	ParamProfileOpenAIStandard      ParamProfile = "openai_standard"
	ParamProfileOpenAIReasoningChat ParamProfile = "openai_reasoning_chat"
	ParamProfilePassthrough         ParamProfile = "passthrough"
)

// Plan 是 adapter 原型需要的最小计划接口，后续可由 gateway/planning.UpstreamPlan 适配实现。
type Plan interface {
	GetProtocolMode() ProtocolMode
	GetParamProfile() ParamProfile
	GetModelForUpstream() string
}

// ChatBodyTransformer 将 Canonical/OpenAI-compatible chat 请求体转换为上游请求体。
type ChatBodyTransformer interface {
	TransformChatBody(plan Plan, request any) (map[string]any, error)
}

// Adapter 是 registry 存储的最小 adapter 类型；具体协议可继续扩展专用接口。
type Adapter interface {
	Name() string
}

type key struct {
	providerType string
	protocolMode ProtocolMode
}

// Registry 按 (provider_type, protocol_mode) 注册与查找 adapter。
type Registry struct {
	mu       sync.RWMutex
	adapters map[key]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[key]Adapter)}
}

func (r *Registry) Register(providerType string, protocolMode ProtocolMode, adapter Adapter) error {
	if r == nil {
		return fmt.Errorf("adapter registry is nil")
	}
	providerType = normalizeProviderType(providerType)
	if providerType == "" {
		return fmt.Errorf("provider_type is required")
	}
	if strings.TrimSpace(string(protocolMode)) == "" {
		return fmt.Errorf("protocol_mode is required")
	}
	if adapter == nil {
		return fmt.Errorf("adapter is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	k := key{providerType: providerType, protocolMode: protocolMode}
	if _, exists := r.adapters[k]; exists {
		return fmt.Errorf("adapter already registered for provider_type=%s protocol_mode=%s", providerType, protocolMode)
	}
	r.adapters[k] = adapter
	return nil
}

func (r *Registry) Lookup(providerType string, protocolMode ProtocolMode) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[key{providerType: normalizeProviderType(providerType), protocolMode: protocolMode}]
	return adapter, ok
}

func normalizeProviderType(providerType string) string {
	return strings.ToLower(strings.TrimSpace(providerType))
}

// StaticPlan 是测试和 adapter 原型可直接使用的轻量 plan。
type StaticPlan struct {
	ProtocolMode     ProtocolMode
	ParamProfile     ParamProfile
	ModelForUpstream string
}

func (p StaticPlan) GetProtocolMode() ProtocolMode { return p.ProtocolMode }
func (p StaticPlan) GetParamProfile() ParamProfile { return p.ParamProfile }
func (p StaticPlan) GetModelForUpstream() string   { return p.ModelForUpstream }
