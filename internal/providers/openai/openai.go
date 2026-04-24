// Package openai provides OpenAI API integration for the LLM gateway.
package openai

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
)

// Registration provides factory registration for the OpenAI provider.
var Registration = providers.Registration{
	Type:                        "openai",
	New:                         New,
	PassthroughSemanticEnricher: passthroughSemanticEnricher{},
	Discovery: providers.DiscoveryConfig{
		DefaultBaseURL: defaultBaseURL,
	},
}

const (
	defaultBaseURL = "https://api.openai.com/v1"
)

// Provider implements the core.Provider interface for OpenAI
type Provider struct {
	*CompatibleProvider
}

// New creates a new OpenAI provider.
func New(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := providers.ResolveBaseURL(cfg.BaseURL, defaultBaseURL)
	return &Provider{
		CompatibleProvider: NewCompatibleProvider(cfg.APIKey, opts, CompatibleProviderConfig{
			ProviderName: "openai",
			BaseURL:      baseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

// NewWithHTTPClient creates a new OpenAI provider with a custom HTTP client.
// If httpClient is nil, http.DefaultClient is used.
func NewWithHTTPClient(apiKey string, httpClient *http.Client, hooks llmclient.Hooks) *Provider {
	return &Provider{
		CompatibleProvider: NewCompatibleProviderWithHTTPClient(apiKey, httpClient, hooks, CompatibleProviderConfig{
			ProviderName: "openai",
			BaseURL:      defaultBaseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

// setHeaders sets the required headers for OpenAI API requests
func setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Forward request ID if present in context using OpenAI's X-Client-Request-Id header.
	// OpenAI requires ASCII-only characters and max 512 bytes, otherwise returns 400.
	if requestID := core.GetRequestID(req.Context()); requestID != "" && isValidClientRequestID(requestID) {
		req.Header.Set("X-Client-Request-Id", requestID)
	}
}

// isValidClientRequestID checks if the request ID is valid for OpenAI's X-Client-Request-Id header.
// OpenAI requires: ASCII characters only, max 512 characters.
func isValidClientRequestID(id string) bool {
	if len(id) > 512 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] > 127 {
			return false
		}
	}
	return true
}

func normalizeModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return m
}

// isOSeriesModel reports whether the model is an OpenAI o-series model
// (o1, o3, o4) that requires max_completion_tokens instead of max_tokens
// and does not support the temperature parameter.
func isOSeriesModel(model string) bool {
	m := normalizeModelName(model)
	// Match o1, o3, o4 families (e.g. o3-mini, o4-mini, o3, o1-preview).
	// Non-reasoning models like gpt-4o start with "gpt-", not "o".
	return len(m) >= 2 && m[0] == 'o' && m[1] >= '0' && m[1] <= '9'
}

// isGPT5ChatModel reports gpt-5-chat* variants that are regular chat models
// (support temperature, no reasoning_effort). Aligns with LiteLLM OpenAIGPT5Config.
func isGPT5ChatModel(model string) bool {
	return strings.HasPrefix(normalizeModelName(model), "gpt-5-chat")
}

func isGPT5Model(model string) bool {
	m := normalizeModelName(model)
	if isGPT5ChatModel(m) {
		return false
	}
	// 匹配 gpt-5、gpt-5-*（连字符）和 gpt-5.*（点号，如 gpt-5.5）
	return m == "gpt-5" || strings.HasPrefix(m, "gpt-5-") || strings.HasPrefix(m, "gpt-5.")
}

// isGPT54PlusModel reports gpt-5.4 / gpt-5.5 / gpt-5.6…（含 -pro 等后缀）。
// Aligns with LiteLLM OpenAIGPT5Config.is_model_gpt_5_4_plus_model.
func isGPT54PlusModel(model string) bool {
	m := normalizeModelName(model)
	if !strings.HasPrefix(m, "gpt-5.") {
		return false
	}
	version := strings.TrimPrefix(m, "gpt-5.")
	if i := strings.IndexAny(version, ".-"); i >= 0 {
		version = version[:i]
	}
	major, err := strconv.Atoi(version)
	if err != nil {
		return false
	}
	return major >= 4
}

// isReasoningChatModel reports whether the model follows OpenAI's reasoning
// chat parameter rules for max_completion_tokens and temperature handling.
func isReasoningChatModel(model string) bool {
	return isOSeriesModel(model) || isGPT5Model(model)
}

// ShouldBridgeChatToResponses reports whether a Chat Completions request must
// be routed through the Responses API, matching LiteLLM get_chat_completion_mode:
//
//	OpenAI/Azure GPT-5（非 search）且 reasoning_effort 存在，并且：
//	  - 带 reasoning summary 别名，或
//	  - gpt-5.4+ 且带 tools
func ShouldBridgeChatToResponses(req *core.ChatRequest) bool {
	if req == nil {
		return false
	}
	if !isGPT5Model(req.Model) || isGPT5SearchModel(req.Model) {
		return false
	}
	if !hasChatReasoningEffort(req) {
		return false
	}
	if hasChatReasoningSummary(req) {
		return true
	}
	return isGPT54PlusModel(req.Model) && len(req.Tools) > 0
}

func isGPT5SearchModel(model string) bool {
	m := strings.ToLower(normalizeModelName(model))
	return strings.Contains(m, "gpt-5") && strings.Contains(m, "search")
}

func hasChatReasoningEffort(req *core.ChatRequest) bool {
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Effort) != "" {
		return true
	}
	for _, key := range []string{"reasoning_effort", "reasoningEffort"} {
		if raw := req.ExtraFields.Lookup(key); len(raw) > 0 && string(raw) != "null" {
			return true
		}
	}
	return false
}

func hasChatReasoningSummary(req *core.ChatRequest) bool {
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Summary) != "" {
		return true
	}
	for _, key := range []string{"reasoningSummary", "reasoning_summary"} {
		if raw := req.ExtraFields.Lookup(key); len(raw) > 0 && string(raw) != "null" {
			return true
		}
	}
	return false
}

// adaptForReasoningChat rewrites a ChatRequest body for OpenAI reasoning chat
// models, mapping max_tokens -> max_completion_tokens and dropping temperature
// while preserving all unknown top-level JSON fields.
//
// 同时清洗 OpenCode / AI SDK 注入的 Chat Completions 非法字段：
//   - reasoningSummary / reasoning_summary：仅 Responses / 部分官方 SDK 语义，Chat 会 400
//   - reasoningEffort（camelCase）→ reasoning_effort
//   - reasoning:{effort}（Responses 形态）→ reasoning_effort
//   - reasoning_effort dict → string（Chat Completions 只接受 string）
// 与 LiteLLM drop_params / OpenCode 兼容策略对齐。
func adaptForReasoningChat(req *core.ChatRequest) (any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to marshal reasoning request: "+err.Error(), err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, core.NewInvalidRequestError("failed to decode reasoning request payload: "+err.Error(), err)
	}
	if maxTokens, ok := raw["max_tokens"]; ok {
		if _, has := raw["max_completion_tokens"]; !has {
			raw["max_completion_tokens"] = maxTokens
		}
		delete(raw, "max_tokens")
	}
	delete(raw, "temperature")
	normalizeReasoningChatParams(raw)
	return raw, nil
}

// normalizeReasoningChatParams 将 AI SDK / OpenCode 别名收敛为 Chat Completions 合法字段。
func normalizeReasoningChatParams(raw map[string]json.RawMessage) {
	// Chat Completions 不接受这些 summary 字段（Azure/OpenAI 均会 unknown_parameter）。
	delete(raw, "reasoningSummary")
	delete(raw, "reasoning_summary")

	if _, has := raw["reasoning_effort"]; !has {
		if effort, ok := raw["reasoningEffort"]; ok {
			raw["reasoning_effort"] = effort
		} else if nested, ok := raw["reasoning"]; ok {
			var obj map[string]json.RawMessage
			if json.Unmarshal(nested, &obj) == nil {
				if effort, ok := obj["effort"]; ok {
					raw["reasoning_effort"] = effort
				}
			}
		}
	}
	delete(raw, "reasoningEffort")
	delete(raw, "reasoning")

	// Chat Completions 要求 reasoning_effort 为 string；dict 形态只保留 effort。
	if effortRaw, ok := raw["reasoning_effort"]; ok {
		if normalized := normalizeReasoningEffortJSON(effortRaw); normalized != nil {
			raw["reasoning_effort"] = normalized
		}
	}
}

func normalizeReasoningEffortJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return raw
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	if effort, ok := obj["effort"]; ok {
		return effort
	}
	return nil
}

// chatRequestBody returns the appropriate request body for the model.
// Reasoning models get parameter adaptation; others pass through as-is.
func chatRequestBody(req *core.ChatRequest) (any, error) {
	// Prefer LogicalModel (group name, e.g. "gpt-5.6-sol") for capability
	// detection, falling back to Model (deployment name, e.g. "4476-gpt-56sol-xlab").
	modelForDetection := req.LogicalModel
	if modelForDetection == "" {
		modelForDetection = req.Model
	}
	if isReasoningChatModel(modelForDetection) {
		return adaptForReasoningChat(req)
	}
	return req, nil
}
