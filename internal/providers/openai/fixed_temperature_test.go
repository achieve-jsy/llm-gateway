package openai

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixedTemperatureForModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  *float64
	}{
		{"kimi-k3", "kimi-k3", floatPtr(0.6)},
		{"kimi-k3 带前缀", "baseten/kimi-k3", floatPtr(0.6)},
		{"kimi-k3 带前缀和大小写", "BaseTen/Kimi-K3", floatPtr(0.6)},
		{"gpt-4 无限制", "gpt-4", nil},
		{"kimi-k2 无限制", "kimi-k2", nil},
		{"空字符串", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FixedTemperatureForModel(tt.model)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.InDelta(t, *tt.want, *got, 0.0001)
			}
		})
	}
}

func TestChatRequestBody_FixedTemperature(t *testing.T) {
	t.Run("kimi-k3 未传 temperature 自动补 0.6", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "kimi-k3",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
		}
		body, err := chatRequestBody(req)
		require.NoError(t, err)
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &parsed))
		assert.Equal(t, 0.6, parsed["temperature"])
	})

	t.Run("kimi-k3 传了错误 temperature 自动修正为 0.6", func(t *testing.T) {
		t0 := 0.0
		req := &core.ChatRequest{
			Model:       "kimi-k3",
			Messages:    []core.Message{{Role: "user", Content: "hi"}},
			Temperature: &t0,
		}
		body, err := chatRequestBody(req)
		require.NoError(t, err)
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &parsed))
		assert.Equal(t, 0.6, parsed["temperature"])
	})

	t.Run("gpt-4 未传 temperature 保持不传", func(t *testing.T) {
		req := &core.ChatRequest{
			Model:    "gpt-4",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
		}
		body, err := chatRequestBody(req)
		require.NoError(t, err)
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &parsed))
		_, hasTemp := parsed["temperature"]
		assert.False(t, hasTemp, "gpt-4 should not force temperature")
	})
}

func floatPtr(f float64) *float64 {
	return &f
}

func TestReasoningEffortForModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  *string
	}{
		{"glm-5.3", "ZHIPU/GLM-5.3", strPtr("low")},
		{"glm-5.3 带前缀", "zhipu/glm-5.3", strPtr("low")},
		{"glm-5.3 大小写", "Zhipu/GLM-5.3", strPtr("low")},
		{"glm-5.4 预期同类", "glm-5.4", strPtr("low")},
		{"glm-5 非始终思考", "glm-5", nil},
		{"glm-5.1 非始终思考", "glm-5.1", nil},
		{"glm-5.2 非始终思考", "glm-5.2", nil},
		{"gpt-4 无限制", "gpt-4", nil},
		{"空字符串", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReasoningEffortForModel(tt.model)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, *tt.want, *got)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}