package openai

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatRequestBody_FixedTemperature_DropsEnableThinking(t *testing.T) {
	t.Run("kimi-k3 删除 enable_thinking 即使传了 false", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "kimi-k3",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
				"enable_thinking": json.RawMessage("false"),
				"custom_field":    json.RawMessage(`"value"`),
			}),
		}

		body, err := chatRequestBody(req)
		require.NoError(t, err)

		data, err := json.Marshal(body)
		require.NoError(t, err)

		assert.NotContains(t, string(data), "enable_thinking",
			"kimi-k3 不应包含 enable_thinking 字段")
		assert.Contains(t, string(data), "custom_field",
			"其他 ExtraFields 应保留")
		assert.Contains(t, string(data), `"temperature":0.6`,
			"应注入 temperature=0.6")
	})

	t.Run("kimi-k3 无 enable_thinking 正常处理", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "kimi-k3",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
		}

		body, err := chatRequestBody(req)
		require.NoError(t, err)

		data, err := json.Marshal(body)
		require.NoError(t, err)

		assert.NotContains(t, string(data), "enable_thinking")
		assert.Contains(t, string(data), `"temperature":0.6`)
	})

	t.Run("gpt-4 保留 enable_thinking", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "gpt-4",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
				"enable_thinking": json.RawMessage("false"),
			}),
		}

		body, err := chatRequestBody(req)
		require.NoError(t, err)

		data, err := json.Marshal(body)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"enable_thinking":false`,
			"gpt-4 应保留 enable_thinking 字段")
	})

	t.Run("glm-5.3 删除 enable_thinking（始终思考模型）", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "ZHIPU/GLM-5.3",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
				"enable_thinking":    json.RawMessage("false"),
				"reasoning_effort":   json.RawMessage(`"low"`),
				"custom_field":       json.RawMessage(`"value"`),
			}),
		}

		body, err := chatRequestBody(req)
		require.NoError(t, err)

		data, err := json.Marshal(body)
		require.NoError(t, err)

		assert.NotContains(t, string(data), "enable_thinking",
			"glm-5.3 不应包含 enable_thinking 字段")
		assert.Contains(t, string(data), `"reasoning_effort":"low"`,
			"reasoning_effort 应保留")
		assert.Contains(t, string(data), "custom_field",
			"其他 ExtraFields 应保留")
	})
}
