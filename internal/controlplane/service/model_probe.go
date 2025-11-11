package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeModelForward 用最小请求探测模型可用性。
//
// 返回值：
//   - success=true: 检测成功
//   - skipped=true: 当前模型类型不适合用 chat/completions 通用探针，跳过且不计失败
func ProbeModelForward(ctx context.Context, endpoint, modelName, apiKey string) (success bool, skipped bool, responseTime int, errMsg string) {
	endpoint = strings.TrimRight(endpoint, "/") + "/chat/completions"

	// 默认先用标准非流式 chat 探针。
	baseReq := map[string]interface{}{
		"model":           modelName,
		"messages":        []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens":      10,
		"enable_thinking": false,
	}

	success, responseTime, errMsg = doProbeRequest(ctx, endpoint, apiKey, baseReq, false)
	if success {
		return true, false, responseTime, ""
	}

	lower := strings.ToLower(errMsg)

	// Qwen thinking 类模型要求 enable_thinking=true。
	if strings.Contains(lower, "enable_thinking") && strings.Contains(lower, "true") {
		thinkingReq := cloneProbeRequest(baseReq)
		thinkingReq["enable_thinking"] = true
		thinkingReq["max_tokens"] = 10
		ok, elapsed, thinkingErr := doProbeRequest(ctx, endpoint, apiKey, thinkingReq, false)
		if ok {
			return true, false, elapsed, ""
		}
		return false, false, elapsed, thinkingErr
	}

	// 某些模型仅支持 stream 模式。
	if strings.Contains(lower, "only support stream mode") || strings.Contains(lower, "stream mode") {
		streamReq := cloneProbeRequest(baseReq)
		streamReq["stream"] = true
		ok, elapsed, streamErr := doProbeRequest(ctx, endpoint, apiKey, streamReq, true)
		if ok {
			return true, false, elapsed, ""
		}
		return false, false, elapsed, streamErr
	}

	// 这些错误说明模型不是标准 chat 模型，跳过而不是记失败。
	if isIncompatibleProbeError(lower) {
		return false, true, responseTime, errMsg
	}

	return false, false, responseTime, errMsg
}

func cloneProbeRequest(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func doProbeRequest(ctx context.Context, endpoint, apiKey string, payload map[string]interface{}, streamMode bool) (bool, int, string) {
	body, _ := json.Marshal(payload)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return false, elapsed, fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, elapsed, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateProbeString(string(respBody), 200))
	}

	if streamMode {
		// SSE 场景只要返回 2xx 且 body 非空即可认为模型支持该模式。
		if len(respBody) == 0 {
			return false, elapsed, "stream 响应体为空"
		}
		return true, elapsed, ""
	}

	var chatResp struct {
		Choices []interface{} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return false, elapsed, fmt.Sprintf("响应体解析失败: %v", err)
	}
	if len(chatResp.Choices) == 0 {
		return false, elapsed, "响应 choices 为空"
	}

	return true, elapsed, ""
}

func isIncompatibleProbeError(lower string) bool {
	patterns := []string{
		"the voice property is required",
		"field required: input.text",
		"input.text",
		"input should be a valid list",
		"invalid message type",
		"current user api does not support http call",
		"selected model does not support image or text inputs",
		"dedicated task `asr`",
		"content parameter's length invalid",
		"range of max_tokens should be [10, 2048]",
		"range of max_tokens should be [10, 8192]",
		"range of max_tokens should be [10, 65536]",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func truncateProbeString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
