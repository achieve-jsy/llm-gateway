package protocol

import (
	"bytes"
	"encoding/json"
)

// internalUsageFields 是 relaykit DTO 中仅服务于其宿主计费的字段，
// 不属于任何公开协议。返回给客户端前必须剥离。
var internalUsageFields = []string{
	"billing_usage",
	"claude_cache_creation_5_m_tokens",
	"claude_cache_creation_1_h_tokens",
}

// responsesChatUsageFields 是 relaykit 把 Chat usage 合并进 Responses usage 时
// 残留的 Chat 风格字段；Responses 协议本身用 input_tokens/output_tokens 表达。
var responsesChatUsageFields = []string{
	"prompt_tokens",
	"completion_tokens",
	"prompt_tokens_details",
	"completion_tokens_details",
}

// normalizeStreamEvent 处理 relaykit 流式结果的两种包装：
//   - Anthropic 事件：Value 本身就是 ClaudeResponse
//   - Responses 事件：Value 是内部结构 ChatToResponsesStreamEvent{Type, Payload}，
//     该结构没有 json tag，直接序列化会向客户端泄漏 Type/Payload 外壳。
//
// 返回 (事件类型, 可直接写给客户端的 JSON 负载)。
func normalizeStreamEvent(marshal func(any) ([]byte, error), value any) (string, []byte) {
	raw, err := marshal(value)
	if err != nil {
		return "", nil
	}

	// 解包 Type/Payload 外壳（字段名大写来自 relaykit 未导出结构）。
	var wrapper struct {
		Type    string          `json:"Type"`
		Payload json.RawMessage `json:"Payload"`
	}
	if len(raw) > 0 && raw[0] == '{' && bytes.Contains(raw, []byte(`"Payload"`)) {
		if json.Unmarshal(raw, &wrapper) == nil && len(wrapper.Payload) > 0 {
			eventType := wrapper.Type
			if eventType == "" {
				var inner struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(wrapper.Payload, &inner) == nil {
					eventType = inner.Type
				}
			}
			return eventType, stripInternalFields(wrapper.Payload)
		}
	}

	eventType := ""
	var meta struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &meta) == nil {
		eventType = meta.Type
	}
	return eventType, stripInternalFields(raw)
}

// MarshalForClient 把 relaykit 产出的响应值序列化为可直接写给客户端的 JSON，
// 并剥离内部计费字段。非流式路径必须经由它输出。
func MarshalForClient(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	// 非流式 Responses 响应同样可能带外壳，但正常情况 Value 就是响应体本身。
	return stripInternalFields(raw), nil
}

// stripInternalFields 删除内部字段；只在确实命中时才重新编码。
func stripInternalFields(raw []byte) []byte {
	if !needsStrip(raw) {
		return raw
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	stripRecursive(payload)

	cleaned, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return cleaned
}

func needsStrip(raw []byte) bool {
	return bytes.Contains(raw, []byte("billing_usage")) ||
		bytes.Contains(raw, []byte("claude_cache_creation"))
}

// stripRecursive 深度清理 usage 对象，兼容顶层 usage / message.usage / response.usage。
func stripRecursive(node map[string]any) {
	for key, child := range node {
		switch v := child.(type) {
		case map[string]any:
			if key == "usage" {
				for _, field := range internalUsageFields {
					delete(v, field)
				}
				// 仅当该 usage 呈现 Responses 语义（已有 input_tokens）时才清理 Chat 残留字段，
				// 避免破坏 Chat 协议本身的 prompt_tokens/completion_tokens。
				if _, isResponses := v["input_tokens"]; isResponses {
					for _, field := range responsesChatUsageFields {
						delete(v, field)
					}
				}
				continue
			}
			stripRecursive(v)
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					stripRecursive(m)
				}
			}
		}
	}
}
