package azure

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/example/llm-gateway/internal/adapters"
	openaiadapter "github.com/example/llm-gateway/internal/adapters/openai"
	"github.com/example/llm-gateway/internal/protocol/transform"
)

const AdapterName = "azure_chat"

type ChatAdapter struct{}

var _ adapters.Adapter = (*ChatAdapter)(nil)
var _ adapters.ChatBodyTransformer = (*ChatAdapter)(nil)

func NewChatAdapter() *ChatAdapter { return &ChatAdapter{} }

func (a *ChatAdapter) Name() string { return AdapterName }

func (a *ChatAdapter) TransformChatBody(plan adapters.Plan, request any) (map[string]any, error) {
	raw, err := a.TransformChatBodyRaw(plan, request)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		var decoded any
		if len(value) == 0 {
			out[key] = nil
			continue
		}
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, err
		}
		out[key] = decoded
	}
	return out, nil
}

func (a *ChatAdapter) TransformChatBodyRaw(plan adapters.Plan, request any) (map[string]json.RawMessage, error) {
	raw, err := openaiadapter.TransformChatBodyRaw(plan, request)
	if err != nil {
		return nil, err
	}
	SanitizeChatBodyRaw(raw)
	return raw, nil
}

var azureChatAlwaysDrop = []string{
	"reasoningSummary",
	"reasoning_summary",
	"reasoningEffort",
	"reasoning",
}

// SanitizeChatBodyRaw 剥离 Azure Chat Completions 已知非法字段。
func SanitizeChatBodyRaw(raw map[string]json.RawMessage) {
	for _, key := range azureChatAlwaysDrop {
		delete(raw, key)
	}
}

// SanitizeChatBody 支持独立复用 Azure sanitize，不依赖旧 provider。
func SanitizeChatBody(body any) (map[string]json.RawMessage, error) {
	raw, err := transform.BodyToRawMap(body)
	if err != nil {
		return nil, err
	}
	SanitizeChatBodyRaw(raw)
	return raw, nil
}

// ChatEndpoint 构造 Azure OpenAI v1 chat endpoint；baseURL 归一由 transport 层处理。
func ChatEndpoint(apiVersion string) string {
	endpoint := "/chat/completions"
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return endpoint
	}
	q := url.Values{}
	q.Set("api-version", apiVersion)
	return endpoint + "?" + q.Encode()
}
