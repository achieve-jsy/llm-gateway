// Package core defines the core interfaces and types for the LLM gateway.
package core

import (
	"context"
	"errors"
	"io"
)

// Provider defines the interface for LLM providers
type Provider interface {
	// ChatCompletion executes a chat completion request
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// StreamChatCompletion returns a raw SSE stream (caller must close)
	StreamChatCompletion(ctx context.Context, req *ChatRequest) (io.ReadCloser, error)

	// ListModels returns the list of available models
	ListModels(ctx context.Context) (*ModelsResponse, error)

	// Responses executes a Responses API request (OpenAI-compatible)
	Responses(ctx context.Context, req *ResponsesRequest) (*ResponsesResponse, error)

	// StreamResponses returns a raw SSE stream for Responses API (caller must close)
	StreamResponses(ctx context.Context, req *ResponsesRequest) (io.ReadCloser, error)

	// Embeddings sends an embeddings request to the provider
	Embeddings(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)

	// Rerank sends a rerank request to the provider
	Rerank(ctx context.Context, req *RerankRequest) (*RerankResponse, error)
}

// ErrNativeNotSupported 表示目标渠道不支持请求的原生协议格式，调用方应降级到常规 Chat 路径。
//
// 放在 core 而非 providers 包，是因为 handler 需要 errors.Is 判定降级，
// 而 handler 不应该 import providers。
var ErrNativeNotSupported = errors.New("provider does not support native relay format")

// NativeRelayFormat 标识 provider 能直接接受的上游协议。
// 只列网关真正会走零转换路径的协议，不要预先枚举用不到的。
type NativeRelayFormat string

const (
	// NativeRelayClaude 表示 provider 原生接受 Anthropic Messages 请求体。
	NativeRelayClaude NativeRelayFormat = "claude"
	// NativeRelayGemini 表示 provider 原生接受 Gemini generateContent 请求体。
	NativeRelayGemini NativeRelayFormat = "gemini"
	// NativeRelayOpenAIResponses 表示 provider 原生接受 OpenAI Responses 请求体。
	NativeRelayOpenAIResponses NativeRelayFormat = "openai_responses"
	// NativeRelayOpenAIChat 表示 provider 原生接受 OpenAI Chat Completions 请求体。
	// Chat 渠道数量最多，声明它能让 Chat 入站也走零转换旁路。
	NativeRelayOpenAIChat NativeRelayFormat = "openai"
)

// NativeRelayProvider 由能直接接收某协议原生请求体的 provider 实现。
//
// 与 Provider.ChatCompletion 的关键区别：请求体和响应体都不塌缩进
// ChatRequest/ChatResponse，因此协议独有字段（top_k、cache_control、
// metadata、Anthropic thinking 签名等）不会在两次转换中被丢弃。
//
// 用 []byte 传递而非 DTO，是为了让 providers 层不依赖协议转换库。
type NativeRelayProvider interface {
	// NativeRelayFormatFor 报告 model 能否零转换接收指定协议，返回该原生格式。
	//
	// **默认必须按模型判断，不能按渠道**：同一个渠道可能同时挂原生模型和通用模型
	// （例如 AWS 渠道既有 anthropic.claude-* 也有 openai.gpt-*），
	// 后者只能走 Converse，不能声明成原生。
	//
	// 唯一的例外是「该渠道对这条 format 下的全部模型都支持」——例如
	// deepseek / groq / xai / ollama 这类纯 OpenAI 兼容服务，名下所有模型都吃
	// /chat/completions 与 /responses，渠道内不存在能力差异，逐模型判断等于恒真。
	// 这种实现可以忽略 model 参数，但必须在实现处写明依据，且该依据一旦失效
	// （新增只支持 Chat 或只支持 Responses 的模型）就要就地改成按模型分支——
	// 不能留给调用方各自打补丁。
	// format 不匹配时返回 ok=false，调用方回退到常规 Chat 路径。
	NativeRelayFormatFor(model string, format NativeRelayFormat) (NativeRelayFormat, bool)
	// RelayNative 执行非流式原生调用，返回上游原始响应体。
	RelayNative(ctx context.Context, body []byte) ([]byte, error)
	// RelayNativeStream 执行流式原生调用，返回上游原始 SSE 流。
	RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error)
}

// NativeBatchProvider is implemented by providers that support native discounted batching.
// This is intentionally separate from Provider so unsupported providers can still implement
// regular synchronous APIs without batch capabilities.
type NativeBatchProvider interface {
	CreateBatch(ctx context.Context, req *BatchRequest) (*BatchResponse, error)
	GetBatch(ctx context.Context, id string) (*BatchResponse, error)
	ListBatches(ctx context.Context, limit int, after string) (*BatchListResponse, error)
	CancelBatch(ctx context.Context, id string) (*BatchResponse, error)
	GetBatchResults(ctx context.Context, id string) (*BatchResultsResponse, error)
}

// BatchCreateHintAwareProvider is an optional native batch extension for
// providers that need gateway persistence for per-item endpoint hints.
type BatchCreateHintAwareProvider interface {
	CreateBatchWithHints(ctx context.Context, req *BatchRequest) (*BatchResponse, map[string]string, error)
}

// BatchResultHintAwareProvider is an optional native batch extension for
// providers that need persisted per-item endpoint hints to shape results.
type BatchResultHintAwareProvider interface {
	GetBatchResultsWithHints(ctx context.Context, id string, endpointByCustomID map[string]string) (*BatchResultsResponse, error)
	ClearBatchResultHints(batchID string)
}

// NativeBatchRoutableProvider extends routing with native batch operations.
type NativeBatchRoutableProvider interface {
	CreateBatch(ctx context.Context, providerType string, req *BatchRequest) (*BatchResponse, error)
	GetBatch(ctx context.Context, providerType, id string) (*BatchResponse, error)
	ListBatches(ctx context.Context, providerType string, limit int, after string) (*BatchListResponse, error)
	CancelBatch(ctx context.Context, providerType, id string) (*BatchResponse, error)
	GetBatchResults(ctx context.Context, providerType, id string) (*BatchResultsResponse, error)
}

// NativeBatchProviderTypeLister exposes registered provider types that support
// native batch operations.
type NativeBatchProviderTypeLister interface {
	NativeBatchProviderTypes() []string
}

// NativeBatchHintRoutableProvider is an optional routing extension for
// providers that can consume persisted per-item endpoint hints.
type NativeBatchHintRoutableProvider interface {
	CreateBatchWithHints(ctx context.Context, providerType string, req *BatchRequest) (*BatchResponse, map[string]string, error)
	GetBatchResultsWithHints(ctx context.Context, providerType, id string, endpointByCustomID map[string]string) (*BatchResultsResponse, error)
	ClearBatchResultHints(providerType, batchID string)
}

// NativeFileProvider is implemented by providers that support OpenAI-compatible files APIs.
type NativeFileProvider interface {
	CreateFile(ctx context.Context, req *FileCreateRequest) (*FileObject, error)
	ListFiles(ctx context.Context, purpose string, limit int, after string) (*FileListResponse, error)
	GetFile(ctx context.Context, id string) (*FileObject, error)
	DeleteFile(ctx context.Context, id string) (*FileDeleteResponse, error)
	GetFileContent(ctx context.Context, id string) (*FileContentResponse, error)
}

// NativeFileRoutableProvider extends routing with provider-native file operations.
type NativeFileRoutableProvider interface {
	CreateFile(ctx context.Context, providerType string, req *FileCreateRequest) (*FileObject, error)
	ListFiles(ctx context.Context, providerType, purpose string, limit int, after string) (*FileListResponse, error)
	GetFile(ctx context.Context, providerType, id string) (*FileObject, error)
	DeleteFile(ctx context.Context, providerType, id string) (*FileDeleteResponse, error)
	GetFileContent(ctx context.Context, providerType, id string) (*FileContentResponse, error)
}

// NativeResponseLifecycleProvider is implemented by providers that support
// OpenAI-compatible Responses lifecycle operations.
type NativeResponseLifecycleProvider interface {
	GetResponse(ctx context.Context, id string, params ResponseRetrieveParams) (*ResponsesResponse, error)
	ListResponseInputItems(ctx context.Context, id string, params ResponseInputItemsParams) (*ResponseInputItemListResponse, error)
	CancelResponse(ctx context.Context, id string) (*ResponsesResponse, error)
	DeleteResponse(ctx context.Context, id string) (*ResponseDeleteResponse, error)
}

// NativeResponseUtilityProvider is implemented by providers that support
// OpenAI-compatible Responses utility operations.
type NativeResponseUtilityProvider interface {
	CountResponseInputTokens(ctx context.Context, req *ResponsesRequest) (*ResponseInputTokensResponse, error)
	CompactResponse(ctx context.Context, req *ResponsesRequest) (*ResponseCompactResponse, error)
}

// NativeResponseLifecycleRoutableProvider extends routing with provider-native
// Responses lifecycle operations.
type NativeResponseLifecycleRoutableProvider interface {
	GetResponse(ctx context.Context, providerType, id string, params ResponseRetrieveParams) (*ResponsesResponse, error)
	ListResponseInputItems(ctx context.Context, providerType, id string, params ResponseInputItemsParams) (*ResponseInputItemListResponse, error)
	CancelResponse(ctx context.Context, providerType, id string) (*ResponsesResponse, error)
	DeleteResponse(ctx context.Context, providerType, id string) (*ResponseDeleteResponse, error)
}

// NativeResponseUtilityRoutableProvider extends routing with provider-native
// Responses utility operations.
type NativeResponseUtilityRoutableProvider interface {
	CountResponseInputTokens(ctx context.Context, providerType string, req *ResponsesRequest) (*ResponseInputTokensResponse, error)
	CompactResponse(ctx context.Context, providerType string, req *ResponsesRequest) (*ResponseCompactResponse, error)
}

// NativeFileProviderTypeLister exposes registered provider types that support
// native file operations. This is an internal capability inventory and must not
// depend on the public model catalog.
type NativeFileProviderTypeLister interface {
	NativeFileProviderTypes() []string
}

// NativeResponseProviderTypeLister exposes registered provider types that
// support native Responses lifecycle operations.
type NativeResponseProviderTypeLister interface {
	NativeResponseProviderTypes() []string
}

// RoutableProvider extends Provider with routing capability.
// This is implemented by the Router which uses a model registry
// to determine if a model is supported.
type RoutableProvider interface {
	Provider

	Supports(model string) bool
	GetProviderType(model string) string
}

// ProviderNameResolver is an optional interface for components that can map a
// routed model selector back to the concrete configured provider instance name.
type ProviderNameResolver interface {
	GetProviderName(model string) string
}

// ProviderTypeNameResolver is an optional interface for components that can map
// a provider type such as "openai" to the concrete configured provider
// instance name used for routing, such as "openai_primary".
type ProviderTypeNameResolver interface {
	GetProviderNameForType(providerType string) string
}

// ProviderNameTypeResolver is an optional interface for components that can map
// a concrete configured provider instance name such as "openai_primary" back to
// its provider type, such as "openai".
type ProviderNameTypeResolver interface {
	GetProviderTypeForName(providerName string) string
}

// AvailabilityChecker is an optional interface for providers that can report
// backend reachability during startup diagnostics.
type AvailabilityChecker interface {
	// CheckAvailability verifies the provider's backend service is reachable.
	// Returns nil if available, error otherwise. Initialization logs failures but
	// keeps the provider registered so later refreshes can retry discovery.
	CheckAvailability(ctx context.Context) error
}

// ModelLookup defines the interface for looking up models and their providers.
// This abstraction allows the Router to be decoupled from the concrete ModelRegistry implementation.
type ModelLookup interface {
	// Supports returns true if the registry has a provider for the given model
	Supports(model string) bool

	// GetProvider returns the provider for the given model, or nil if not found
	GetProvider(model string) Provider

	// GetProviderType returns the provider type string for the given model.
	// Returns empty string if the model is not found.
	GetProviderType(model string) string

	// ListModels returns all models in the registry
	ListModels() []Model

	// ModelCount returns the number of registered models
	ModelCount() int

	// GetProviderName maps a model selector back to the concrete configured
	// provider instance name. Implementations that have no such mapping return
	// an empty string. Same shape as the optional ProviderNameResolver
	// interface used elsewhere for provider-side type assertions.
	GetProviderName(model string) string

	// GetProviderNameForType maps a provider type such as "openai" to the
	// concrete configured instance name chosen for routing, e.g.
	// "openai-primary". Returns empty when no mapping exists.
	GetProviderNameForType(providerType string) string

	// GetProviderTypeForName maps a concrete configured instance name back to
	// its provider type. Returns empty when no mapping exists.
	GetProviderTypeForName(providerName string) string
}
