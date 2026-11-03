package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/anthropic"
	"github.com/example/llm-gateway/internal/providers/gemini"
	"github.com/example/llm-gateway/internal/providers/openai"
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
		"model":      modelName,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 10,
	}
	// 固定 temperature 模型（如 kimi-k3）优先注入 temperature；
	// 「始终思考」模型（如 ZHIPU GLM-5.3）第一次探测就直接给 reasoning_effort，
	// 不发送 enable_thinking:false（上游会拒绝并报 code 1210）。
	if temp := openai.FixedTemperatureForModel(modelName); temp != nil {
		baseReq["temperature"] = *temp
	} else if effort := openai.ReasoningEffortForModel(modelName); effort != nil {
		baseReq["reasoning_effort"] = *effort
	} else {
		baseReq["enable_thinking"] = false
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

	// 模型「始终思考」，不允许关闭 thinking（如 ZHIPU GLM-5.x 经 DashScope 中转）。
	// 错误形如：该模型始终思考，不支持关闭思考；请使用 low、high 或 max。
	// 首次探测带 enable_thinking:false 被拒；重试必须去掉 enable_thinking 并
	// 显式给 reasoning_effort=low（最低档、探测成本最小）否则模型会被自动禁用。
	if isAlwaysThinkingError(lower) {
		thinkingReq := cloneProbeRequest(baseReq)
		delete(thinkingReq, "enable_thinking")
		thinkingReq["reasoning_effort"] = "low"
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

	// 某些模型要求固定 temperature（如 kimi-k3 要求 0.6），
	// 探测请求未带 temperature 时会被拒绝，提取要求值后重试。
	// 重试时同时移除 enable_thinking：固定 temperature 模型（kimi-k3）
	// 会拒绝 enable_thinking+temperature 组合，返回同样误导性的 400。
	if strings.Contains(lower, "invalid temperature") && strings.Contains(lower, "only") {
		if tempVal := ExtractRequiredTemperature(lower); tempVal != nil {
			retryReq := cloneProbeRequest(baseReq)
			retryReq["temperature"] = *tempVal
			delete(retryReq, "enable_thinking")
			ok, t, retryErr := doProbeRequest(ctx, chatEndpoint, apiKey, retryReq, false)
			if ok {
				return true, false, t, ""
			}
			return false, false, t, retryErr
		}
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
		Timeout: 120 * time.Second,
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

// isAlwaysThinkingError 判断是否为「模型始终思考，不支持关闭思考，请用 low/high/max」类错误。
// 典型模型：ZHIPU GLM-5.x 等 reasoning-only 模型（经 DashScope compatible-mode 中转）。
// 中文：该模型始终思考，不支持关闭思考；请使用 low、high 或 max。
// 英文：reasoning is always enabled / cannot disable thinking; use low, high or max.
// 需要同时命中思考类标记与低/高/最大三档 effort 提示，避免误判其它 400。
func isAlwaysThinkingError(lower string) bool {
	thinkingMarker := strings.Contains(lower, "始终思考") ||
		strings.Contains(lower, "不支持关闭思考") ||
		strings.Contains(lower, "cannot disable thinking") ||
		strings.Contains(lower, "thinking is always enabled") ||
		strings.Contains(lower, "does not support disabling thinking")
	if !thinkingMarker {
		return false
	}
	return strings.Contains(lower, "low") && strings.Contains(lower, "high") && strings.Contains(lower, "max") ||
		strings.Contains(lower, "reasoning_effort")
}

func truncateProbeString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ExtractRequiredTemperature 从错误信息中提取模型要求的固定 temperature 值。
// 匹配 "only 0.6 is allowed" 这类模式，返回 float64 指针。
func ExtractRequiredTemperature(lower string) *float64 {
	idx := strings.Index(lower, "only ")
	if idx == -1 {
		return nil
	}
	rest := lower[idx+5:]
	end := strings.IndexFunc(rest, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	})
	if end == -1 {
		end = len(rest)
	}
	val, err := strconv.ParseFloat(rest[:end], 64)
	if err != nil || end == 0 {
		return nil
	}
	return &val
}

// ─── 原生协议探针：Anthropic / Gemini ─────────────────────────────────────
//
// 这两类渠道不吃 /chat/completions，通用探针只会拿到 404，
// 进而在「连续 N 次失败」后被自动禁用 —— 即使渠道本身完全正常。
// 探针必须按各家原生协议发最小请求，并模拟 adapter 真正使用的鉴权头。

const (
	// probeAdapterMaxTokens 探针请求的 token 上限。
	// 取 16 是为了兼容最严格的上游（阿里百炼要求 ≥ 16，Bedrock GPT 系列同样要求 ≥ 16）。
	probeAdapterMaxTokens = 16
	probeAdapterTimeout   = 120 * time.Second
)

// probeViaAdapter 用真实 adapter 发一条最小 chat 请求，作为渠道探针。
//
// 探测器不自己拼 URL / header / body，而是复用 adapter 的实现：路径、鉴权头、
// API 版本常量、参数改写、超时全部与线上转发同源。协议升级时探针自动跟随，
// 不会出现「探测通过但真实转发失败」这种最难排查的错配。
//
// 故意不继承生产的重试与熔断：探测需要快速给出确定结论，
// 重试会把一次失败拖成 3 倍超时，反而掩盖真实状态。
func probeViaAdapter(ctx context.Context, p core.Provider, model string) (bool, int, string) {
	ctx, cancel := context.WithTimeout(ctx, probeAdapterTimeout)
	defer cancel()

	maxTokens := probeAdapterMaxTokens
	start := time.Now()
	_, err := p.ChatCompletion(ctx, &core.ChatRequest{
		Model:     model,
		MaxTokens: &maxTokens,
		Messages:  []core.Message{{Role: "user", Content: "hi"}},
	})
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return false, elapsed, "adapter probe: " + truncateProbeString(err.Error(), 200)
	}
	return true, elapsed, ""
}

// ProbeAnthropicMessages 用真实 Anthropic adapter 探测渠道。
// 构造参数与生产一致（provider_loader.go 的 ProviderConfig 只设 APIKey / BaseURL），
// 否则会探测到与线上不同的协议变体。
func ProbeAnthropicMessages(ctx context.Context, endpoint, apiKey, model string) (bool, int, string) {
	return probeViaAdapter(ctx,
		anthropic.New(
			providers.ProviderConfig{APIKey: apiKey, BaseURL: endpoint},
			providers.ProviderOptions{},
		), model)
}

// ProbeGeminiGenerate 用真实 Gemini adapter 探测渠道，构造参数同上。
func ProbeGeminiGenerate(ctx context.Context, endpoint, apiKey, model string) (bool, int, string) {
	return probeViaAdapter(ctx,
		gemini.New(
			providers.ProviderConfig{APIKey: apiKey, BaseURL: endpoint},
			providers.ProviderOptions{},
		), model)
}
