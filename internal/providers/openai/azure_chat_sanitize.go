package openai

import "encoding/json"

// azureChatAlwaysDrop：Azure Chat Completions 不接受的 SDK/别名字段。
// （stream_options 仅在 Responses 桥接时剥离，Chat 路径保留。）
var azureChatAlwaysDrop = []string{
	"reasoningSummary",
	"reasoning_summary",
	"reasoningEffort",
	"reasoning", // nested reasoning 仅 Responses；effort 应已归一为 reasoning_effort
}

// sanitizeAzureChatBody 在发往 Azure /chat/completions 前剥离已知非法字段。
func sanitizeAzureChatBody(body any) any {
	switch b := body.(type) {
	case map[string]json.RawMessage:
		return sanitizeAzureRawMap(b)
	case map[string]any:
		raw := make(map[string]json.RawMessage, len(b))
		for k, v := range b {
			encoded, err := json.Marshal(v)
			if err != nil {
				continue
			}
			raw[k] = encoded
		}
		return sanitizeAzureRawMap(raw)
	default:
		encoded, err := json.Marshal(body)
		if err != nil {
			return body
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &raw); err != nil {
			return body
		}
		return sanitizeAzureRawMap(raw)
	}
}

func sanitizeAzureRawMap(raw map[string]json.RawMessage) map[string]json.RawMessage {
	if raw == nil {
		return nil
	}
	for _, key := range azureChatAlwaysDrop {
		delete(raw, key)
	}
	return raw
}
