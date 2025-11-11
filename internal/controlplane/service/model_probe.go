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
// modelType 对应 models.model_type 字段，决定使用哪个探针接口。
//
// 返回值：
//   - success=true: 检测成功
//   - skipped=true: 当前模型类型不适合通用探针，跳过且不计失败
func ProbeModelForward(ctx context.Context, endpoint, modelName, apiKey, modelType string) (success bool, skipped bool, responseTime int, errMsg string) {
	lower := strings.ToLower(strings.TrimSpace(modelType))

	switch {
	case strings.Contains(lower, "embed"):
		return probeEmbedding(ctx, endpoint, modelName, apiKey)

	case lower == "image_generation" || lower == "image":
		return probeImageGeneration(ctx, endpoint, modelName, apiKey)

	case lower == "rerank":
		return probeRerank(ctx, endpoint, modelName, apiKey)

	case lower == "audio_transcription" || lower == "audio_speech" || lower == "audio":
		// 音频类模型需要上传文件，无法用轻量探针，直接跳过
		return false, true, 0, "audio model: skipped"

	default:
		// chat / completion / reasoning / 未知类型 → 走 /chat/completions
		return probeChatCompletion(ctx, endpoint, modelName, apiKey)
	}
}

// probeChatCompletion POST /chat/completions，适用于 chat/reasoning 模型
func probeChatCompletion(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	chatEndpoint := strings.TrimRight(endpoint, "/") + "/chat/completions"

	baseReq := map[string]interface{}{
		"model":           modelName,
		"messages":        []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens":      10,
		"enable_thinking": false,
	}

	success, elapsed, errMsg := doProbeRequest(ctx, chatEndpoint, apiKey, baseReq, false)
	if success {
		return true, false, elapsed, ""
	}

	lower := strings.ToLower(errMsg)

	// Qwen thinking 类模型要求 enable_thinking=true
	if strings.Contains(lower, "enable_thinking") && strings.Contains(lower, "true") {
		thinkingReq := cloneProbeRequest(baseReq)
		thinkingReq["enable_thinking"] = true
		ok, t, thinkingErr := doProbeRequest(ctx, chatEndpoint, apiKey, thinkingReq, false)
		if ok {
			return true, false, t, ""
		}
		return false, false, t, thinkingErr
	}

	// 某些模型仅支持 stream 模式
	if strings.Contains(lower, "only support stream mode") || strings.Contains(lower, "stream mode") {
		streamReq := cloneProbeRequest(baseReq)
		streamReq["stream"] = true
		ok, t, streamErr := doProbeRequest(ctx, chatEndpoint, apiKey, streamReq, true)
		if ok {
			return true, false, t, ""
		}
		return false, false, t, streamErr
	}

	// 不兼容错误 → skipped
	if isIncompatibleProbeError(lower) {
		return false, true, elapsed, errMsg
	}

	return false, false, elapsed, errMsg
}

// probeEmbedding POST /embeddings，适用于 embedding 模型
func probeEmbedding(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	embEndpoint := strings.TrimRight(endpoint, "/") + "/embeddings"

	req := map[string]interface{}{
		"model": modelName,
		"input": "test",
	}

	success, elapsed, errMsg := doProbeRequest(ctx, embEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// probeImageGeneration POST /images/generations，适用于图像生成模型
func probeImageGeneration(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	imgEndpoint := strings.TrimRight(endpoint, "/") + "/images/generations"

	req := map[string]interface{}{
		"model":  modelName,
		"prompt": "a cat",
		"n":      1,
		"size":   "256x256",
	}

	success, elapsed, errMsg := doProbeRequest(ctx, imgEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// probeRerank POST /rerank，适用于重排序模型
func probeRerank(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	rerankEndpoint := strings.TrimRight(endpoint, "/") + "/rerank"

	req := map[string]interface{}{
		"model":     modelName,
		"query":     "test",
		"documents": []string{"hello world"},
		"top_n":     1,
	}

	success, elapsed, errMsg := doProbeRequest(ctx, rerankEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
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

	// 非 2xx 明确失败
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, elapsed, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateProbeString(string(respBody), 200))
	}

	if streamMode {
		if len(respBody) == 0 {
			return false, elapsed, "stream 响应体为空"
		}
		return true, elapsed, ""
	}

	// HTTP 2xx 即成功，不检查响应内容格式
	// reasoning/thinking 模型 choices 为空也算通过，参考 GoModel 设计
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
