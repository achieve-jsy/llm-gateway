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
	case lower == "embedding_vl":
		return probeEmbeddingVL(ctx, endpoint, modelName, apiKey)

	case strings.Contains(lower, "embed"):
		return probeEmbedding(ctx, endpoint, modelName, apiKey)

	case lower == "image_generation" || lower == "image":
		return probeImageGeneration(ctx, endpoint, modelName, apiKey)

	case lower == "rerank" || lower == "rerank_vl":
		return probeRerank(ctx, endpoint, modelName, apiKey)

	case lower == "audio_transcription" || lower == "audio_speech" || lower == "audio":
		return false, true, 0, "audio model: skipped"

	case lower == "video" || lower == "video_generation":
		// 视频模型不走 /chat/completions；seedance-relay 请用渠道级 ProbeSeedanceRelayConnectivity
		return false, true, 0, "video model: skipped (use seedance-relay connectivity+auth probe)"

	default:
		return probeChatCompletion(ctx, endpoint, modelName, apiKey)
	}
}

// ─── 通用探针：Chat / Completion / Reasoning ──────────────────────────────

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

	if strings.Contains(lower, "enable_thinking") && strings.Contains(lower, "true") {
		thinkingReq := cloneProbeRequest(baseReq)
		thinkingReq["enable_thinking"] = true
		ok, t, thinkingErr := doProbeRequest(ctx, chatEndpoint, apiKey, thinkingReq, false)
		if ok {
			return true, false, t, ""
		}
		return false, false, t, thinkingErr
	}

	if strings.Contains(lower, "only support stream mode") || strings.Contains(lower, "stream mode") {
		streamReq := cloneProbeRequest(baseReq)
		streamReq["stream"] = true
		ok, t, streamErr := doProbeRequest(ctx, chatEndpoint, apiKey, streamReq, true)
		if ok {
			return true, false, t, ""
		}
		return false, false, t, streamErr
	}

	if isIncompatibleProbeError(lower) {
		return false, true, elapsed, errMsg
	}

	// 新模型拒绝 max_tokens：改用 max_completion_tokens 重试一次。
	if strings.Contains(lower, "max_completion_tokens") && strings.Contains(lower, "max_tokens") {
		retryReq := cloneProbeRequest(baseReq)
		if mt, ok := retryReq["max_tokens"]; ok {
			retryReq["max_completion_tokens"] = mt
			delete(retryReq, "max_tokens")
		}
		ok, t, retryErr := doProbeRequest(ctx, chatEndpoint, apiKey, retryReq, false)
		if ok {
			return true, false, t, ""
		}
		return false, false, t, retryErr
	}

	return false, false, elapsed, errMsg
}

// ─── Embedding 探针 ────────────────────────────────────────────────────────

// probeEmbedding 探测 embedding 模型可用性。
// DashScope 兼容模式不支持，需要用原生 text-embedding API 探针。
func probeEmbedding(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	if isDashScope(endpoint) {
		return probeDashScopeTextEmbedding(ctx, modelName, apiKey)
	}

	embEndpoint := strings.TrimRight(endpoint, "/") + "/embeddings"
	req := map[string]interface{}{"model": modelName, "input": "test"}
	success, elapsed, errMsg := doProbeRequest(ctx, embEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// probeEmbeddingVL 探测多模态 embedding 模型可用性（qwen3-vl-embedding 等）。
// DashScope 需要走原生 multimodal-embedding API。
func probeEmbeddingVL(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	if isDashScope(endpoint) {
		return probeDashScopeMultimodalEmbedding(ctx, modelName, apiKey)
	}

	// 通用渠道：多模态 embedding 走标准 /embeddings 端点
	embEndpoint := strings.TrimRight(endpoint, "/") + "/embeddings"
	req := map[string]interface{}{"model": modelName, "input": "test"}
	success, elapsed, errMsg := doProbeRequest(ctx, embEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// ─── Rerank 探针 ──────────────────────────────────────────────────────────

// probeRerank 探测 rerank 模型可用性。
// DashScope 兼容模式没有 /rerank 端点，需要用原生 API 探针。
func probeRerank(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	if isDashScope(endpoint) {
		return probeDashScopeRerank(ctx, modelName, apiKey)
	}

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

// ─── 图像生成探针 ──────────────────────────────────────────────────────────

func probeImageGeneration(ctx context.Context, endpoint, modelName, apiKey string) (bool, bool, int, string) {
	imgEndpoint := strings.TrimRight(endpoint, "/") + "/images/generations"
	req := map[string]interface{}{"model": modelName, "prompt": "a cat", "n": 1, "size": "256x256"}
	success, elapsed, errMsg := doProbeRequest(ctx, imgEndpoint, apiKey, req, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// ─── DashScope 原生 API 探针 ──────────────────────────────────────────────
// DashScope 兼容模式不支持 rerank 和部分 embedding 模型，
// 需要走原生 API 进行健康检测。

const dashScopeNativeBase = "https://dashscope.aliyuncs.com"

func isDashScope(endpoint string) bool {
	return strings.Contains(endpoint, "dashscope.aliyuncs.com")
}

// probeDashScopeTextEmbedding 使用原生 text-embedding API 探测。
func probeDashScopeTextEmbedding(ctx context.Context, modelName, apiKey string) (bool, bool, int, string) {
	success, elapsed, errMsg := doProbeRequest(ctx,
		dashScopeNativeBase+"/api/v1/services/embeddings/text-embedding/text-embedding",
		apiKey,
		map[string]interface{}{
			"model": modelName,
			"input": map[string]interface{}{"texts": []string{"test"}},
		}, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// probeDashScopeMultimodalEmbedding 使用多模态 embedding API 探测。
func probeDashScopeMultimodalEmbedding(ctx context.Context, modelName, apiKey string) (bool, bool, int, string) {
	success, elapsed, errMsg := doProbeRequest(ctx,
		dashScopeNativeBase+"/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding",
		apiKey,
		map[string]interface{}{
			"model": modelName,
			"input": map[string]interface{}{"contents": []map[string]interface{}{{"text": "test"}}},
		}, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// probeDashScopeRerank 使用原生 rerank API 探测。
func probeDashScopeRerank(ctx context.Context, modelName, apiKey string) (bool, bool, int, string) {
	success, elapsed, errMsg := doProbeRequest(ctx,
		dashScopeNativeBase+"/api/v1/services/rerank/text-rerank/text-rerank",
		apiKey,
		map[string]interface{}{
			"model": modelName,
			"input": map[string]interface{}{
				"query":     "test",
				"documents": []string{"hello world"},
			},
			"parameters": map[string]interface{}{"top_n": 1},
		}, false)
	if success {
		return true, false, elapsed, ""
	}
	return false, false, elapsed, errMsg
}

// ─── 通用 HTTP 请求 ───────────────────────────────────────────────────────

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
		if len(respBody) == 0 {
			return false, elapsed, "stream 响应体为空"
		}
		return true, elapsed, ""
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