package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFloat64_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    JSONFloat64
		expected string
	}{
		{"整数值 1 序列化为 1.0", JSONFloat64(1), "1.0"},
		{"整数值 0 序列化为 0.0", JSONFloat64(0), "0.0"},
		{"整数值 2 序列化为 2.0", JSONFloat64(2), "2.0"},
		{"小数值 0.7 保持原样", JSONFloat64(0.7), "0.7"},
		{"小数值 0.95 保持原样", JSONFloat64(0.95), "0.95"},
		{"小数值 1.5 保持原样", JSONFloat64(1.5), "1.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestJSONFloat64_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected JSONFloat64
	}{
		{"解析整数 1", "1", JSONFloat64(1)},
		{"解析浮点 0.7", "0.7", JSONFloat64(0.7)},
		{"解析浮点 1.0", "1.0", JSONFloat64(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v JSONFloat64
			err := json.Unmarshal([]byte(tt.input), &v)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, v)
		})
	}
}

func TestChatCompletionRequest_TemperatureSerialization(t *testing.T) {
	temp := JSONFloat64(1)
	topP := JSONFloat64(0.9)
	req := ChatCompletionRequest{
		Model:       "test-model",
		Messages:    []Message{{Role: "user", Content: "hello"}},
		Temperature: &temp,
		TopP:        &topP,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	jsonStr := string(data)
	// temperature 必须是 1.0 而非 1
	assert.Contains(t, jsonStr, `"temperature":1.0`)
	assert.Contains(t, jsonStr, `"top_p":0.9`)
}

func TestChatCompletionRequest_NilTemperature(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	jsonStr := string(data)
	// omitempty 时不应包含 temperature 和 top_p
	assert.NotContains(t, jsonStr, "temperature")
	assert.NotContains(t, jsonStr, "top_p")
}
