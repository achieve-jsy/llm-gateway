package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRequiredTemperature(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   *float64
	}{
		{
			name:   "kimi-k3 实际错误格式",
			errMsg: "http 400: {\"error\":{\"message\":\"invalid temperature: only 0.6 is allowed for this model\",\"type\":\"InvalidParameter\"}}",
			want:   floatPtr(0.6),
		},
		{
			name:   "整数 temperature",
			errMsg: "invalid temperature: only 1 is allowed",
			want:   floatPtr(1.0),
		},
		{
			name:   "不匹配的错误信息",
			errMsg: "model not found",
			want:   nil,
		},
		{
			name:   "有 invalid temperature 但无 only",
			errMsg: "invalid temperature parameter",
			want:   nil,
		},
		{
			name:   "only 后跟非数字",
			errMsg: "invalid temperature: only abc is allowed",
			want:   nil,
		},
		{
			name:   "空字符串",
			errMsg: "",
			want:   nil,
		},
		{
			name:   "only 0.6 在字符串末尾",
			errMsg: "invalid temperature: only 0.6",
			want:   floatPtr(0.6),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRequiredTemperature(tt.errMsg)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, *tt.want, *got, 0.0001)
			}
		})
	}
}

func TestTruncateProbeString(t *testing.T) {
	t.Run("短字符串不变", func(t *testing.T) {
		s := "short error"
		assert.Equal(t, s, truncateProbeString(s, 200))
	})
	t.Run("长字符串截断", func(t *testing.T) {
		s := string(make([]byte, 300))
		got := truncateProbeString(s, 200)
		assert.Len(t, got, 203) // 200 + "..."
		assert.Contains(t, got, "...")
	})
}

func TestCloneProbeRequest(t *testing.T) {
	src := map[string]interface{}{
		"model":       "kimi-k3",
		"temperature": 0.6,
		"max_tokens":  10,
	}
	dst := cloneProbeRequest(src)
	dst["temperature"] = 1.0
	dst["extra"] = "field"

	assert.Equal(t, 0.6, src["temperature"], "修改 clone 不应影响原始 map")
	_, hasExtra := src["extra"]
	assert.False(t, hasExtra, "clone 新增 key 不应影响原始 map")
}

// ─── 探针集成测试（httptest mock server）──────────────────────────────────

// newKimiK3Server 模拟 kimi-k3 上游：无 temperature=0.6 时返回 400。
func newKimiK3Server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		temp, hasTemp := body["temperature"]
		if !hasTemp || temp != 0.6 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "invalid temperature: only 0.6 is allowed for this model",
					"type":    "InvalidParameter",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "hi"}}},
		})
	}))
}

func TestProbeChatCompletion_KimiK3_ProactiveInjection(t *testing.T) {
	srv := newKimiK3Server(t)
	defer srv.Close()

	success, skipped, _, errMsg := probeChatCompletion(context.Background(), srv.URL, "kimi-k3", "test-key")
	assert.True(t, success, "kimi-k3 探测应成功（主动注入 temperature=0.6）")
	assert.False(t, skipped)
	assert.Empty(t, errMsg)
}

func TestProbeChatCompletion_KimiK3_WithPrefix(t *testing.T) {
	srv := newKimiK3Server(t)
	defer srv.Close()

	success, skipped, _, errMsg := probeChatCompletion(context.Background(), srv.URL, "kimi/kimi-k3", "test-key")
	assert.True(t, success, "kimi/kimi-k3 带前缀也应成功")
	assert.False(t, skipped)
	assert.Empty(t, errMsg)
}

func TestProbeChatCompletion_KimiK3_ErrorBasedRetry(t *testing.T) {
	// 模拟第一次请求不带 temperature 被拒，但重试带 temperature 成功
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		temp, hasTemp := body["temperature"]
		if !hasTemp || temp != 0.6 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "invalid temperature: only 0.6 is allowed for this model",
					"type":    "InvalidParameter",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	// 用一个不匹配 FixedTemperatureForModel 的名字来触发 error-based retry 路径
	// 但上游仍然要求 0.6
	success, skipped, _, errMsg := probeChatCompletion(context.Background(), srv.URL, "custom-kimi-k3-alias", "test-key")
	assert.True(t, success, "error-based retry 应成功")
	assert.False(t, skipped)
	assert.Empty(t, errMsg)
	assert.Equal(t, 2, callCount, "应该发了两次请求（首次失败 + 重试成功）")
}

func TestProbeChatCompletion_KimiK3_NoEnableThinking(t *testing.T) {
	var receivedEnableThinking interface{}
	var receivedTemp interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		receivedEnableThinking = body["enable_thinking"]
		receivedTemp = body["temperature"]
		if body["enable_thinking"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "invalid temperature: only 0.6 is allowed for this model",
					"type":    "InvalidParameter",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	success, skipped, _, errMsg := probeChatCompletion(context.Background(), srv.URL, "kimi/kimi-k3", "test-key")
	assert.True(t, success, "kimi-k3 不应发送 enable_thinking 字段")
	assert.False(t, skipped)
	assert.Empty(t, errMsg)
	assert.Nil(t, receivedEnableThinking, "enable_thinking 不应出现在请求体中")
	assert.Equal(t, 0.6, receivedTemp, "temperature 应为 0.6")
}

func TestProbeChatCompletion_NonKimiModel_HasEnableThinking(t *testing.T) {
	var receivedEnableThinking interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		receivedEnableThinking = body["enable_thinking"]
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	success, _, _, _ := probeChatCompletion(context.Background(), srv.URL, "gpt-4", "test-key")
	assert.True(t, success)
	assert.Equal(t, false, receivedEnableThinking, "非固定 temperature 模型应保留 enable_thinking=false")
}

func TestProbeChatCompletion_NonKimiModel_NoTemperature(t *testing.T) {
	var receivedTemp interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		receivedTemp = body["temperature"]
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	success, _, _, _ := probeChatCompletion(context.Background(), srv.URL, "gpt-4", "test-key")
	assert.True(t, success)
	assert.Nil(t, receivedTemp, "gpt-4 不应注入 temperature")
}

func TestProbeModelForward_KimiK3_EndToEnd(t *testing.T) {
	srv := newKimiK3Server(t)
	defer srv.Close()

	success, skipped, _, errMsg := ProbeModelForward(context.Background(), srv.URL, "kimi-k3", "test-key", "chat")
	require.True(t, success, "ProbeModelForward 对 kimi-k3 应成功")
	assert.False(t, skipped)
	assert.Empty(t, errMsg)
}

func floatPtr(f float64) *float64 {
	return &f
}