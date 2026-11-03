package providers

import (
	"encoding/json"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

// IsJSONSchemaResponseFormat reports whether the request asks for a
// response_format of type json_schema.
func IsJSONSchemaResponseFormat(req *core.ChatRequest) bool {
	if req == nil {
		return false
	}
	raw := req.ExtraFields.Lookup("response_format")
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var format struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return false
	}
	return strings.TrimSpace(format.Type) == "json_schema"
}

// DowngradeJSONSchemaResponseFormat rewrites a response_format of type json_schema
// to json_object for providers that support json_object but not native json_schema
// structured output. It returns true when a change was applied.
func DowngradeJSONSchemaResponseFormat(req *core.ChatRequest) bool {
	if !IsJSONSchemaResponseFormat(req) {
		return false
	}
	req.ExtraFields.Set("response_format", json.RawMessage(`{"type":"json_object"}`))
	return true
}

// IsDeepSeekModel reports whether a model id belongs to the DeepSeek family,
// ignoring any provider-qualified prefix (e.g. "openrouter/deepseek-chat").
func IsDeepSeekModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return strings.HasPrefix(m, "deepseek")
}
