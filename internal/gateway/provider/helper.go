package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// VendorConfig 通用适配器运行时配置（从 ProviderDefinition.VendorOptions 映射而来）
type VendorConfig struct {
	// URL 构造模式：standard / azure / volcengine / custom
	URLPattern string
	// 认证 Header：bearer / api-key / custom
	AuthHeader string
	// Azure 等需要的 API 版本
	APIVersion string
	// 是否支持 stream_options（请求中添加 include_usage）
	SupportsStreamOptions bool
	// 是否跳过空 choices chunk
	EmptyChoicesSkip bool
	// 最后 chunk 含 usage
	StreamUsageInLastChunk bool
	// 支持 reasoning_content
	SupportsReasoningContent bool
	// 自定义额外请求头
	ExtraHeaders map[string]string
}

// DefaultVendorConfig 默认配置（适用于大多数 OpenAI 兼容厂商）
var DefaultVendorConfig = VendorConfig{
	URLPattern:       "standard",
	AuthHeader:       "bearer",
	EmptyChoicesSkip: true,
}

// BuildRequestURL 根据 VendorConfig 构造请求 URL
func BuildRequestURL(baseURL, path string, cfg VendorConfig, modelName string) string {
	baseURL = strings.TrimRight(baseURL, "/")

	switch cfg.URLPattern {
	case "azure":
		// Azure: /openai/deployments/{model}/chat/completions?api-version=xxx
		model := strings.ReplaceAll(modelName, ".", "")
		version := cfg.APIVersion
		if version == "" {
			version = "2024-02-01"
		}
		return fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s", baseURL, model, path, version)
	case "volcengine":
		// 火山引擎/豆包: baseURL 已经包含模型路径
		return baseURL + path
	case "custom":
		// 自定义：直接拼接
		return baseURL + path
	default:
		// standard: baseURL + path（如 https://api.deepseek.com/v1 + /chat/completions）
		return baseURL + path
	}
}

// SetAuthHeader 根据 VendorConfig 设置认证头
func SetAuthHeader(req *http.Request, apiKey string, cfg VendorConfig) {
	switch cfg.AuthHeader {
	case "api-key":
		// Azure 风格
		req.Header.Set("api-key", apiKey)
	case "x-api-key":
		// Anthropic 风格
		req.Header.Set("x-api-key", apiKey)
	default:
		// bearer（默认，适用于大多数厂商）
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// 追加自定义额外请求头
	for k, v := range cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

// OpenAICompatChat 通用 OpenAI 兼容非流式调用（配置驱动版本）
func OpenAICompatChat(client *http.Client, baseURL string, req *model.ChatCompletionRequest, apiKey string, cfg VendorConfig, parseError func(int, []byte) error) (*model.ChatCompletionResponse, error) {
	return OpenAICompatChatWithContext(context.Background(), client, baseURL, req, apiKey, cfg, parseError)
}

// OpenAICompatChatWithContext 通用 OpenAI 兼容非流式调用（带 context 版本）
func OpenAICompatChatWithContext(ctx context.Context, client *http.Client, baseURL string, req *model.ChatCompletionRequest, apiKey string, cfg VendorConfig, parseError func(int, []byte) error) (*model.ChatCompletionResponse, error) {
	// 确保非流式
	reqCopy := *req
	reqCopy.Stream = false

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := BuildRequestURL(baseURL, "/chat/completions", cfg, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	SetAuthHeader(httpReq, apiKey, cfg)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// 非 2xx 状态码处理
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp.StatusCode, respBody)
	}

	var result model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

// OpenAICompatStream 通用 OpenAI 兼容流式调用（配置驱动版本）
func OpenAICompatStream(client *http.Client, baseURL string, req *model.ChatCompletionRequest, apiKey string, cfg VendorConfig, parseError func(int, []byte) error) (<-chan model.StreamChunk, error) {
	return OpenAICompatStreamWithContext(context.Background(), client, baseURL, req, apiKey, cfg, parseError)
}

// OpenAICompatStreamWithContext 通用 OpenAI 兼容流式调用（带 context 版本）
func OpenAICompatStreamWithContext(ctx context.Context, client *http.Client, baseURL string, req *model.ChatCompletionRequest, apiKey string, cfg VendorConfig, parseError func(int, []byte) error) (<-chan model.StreamChunk, error) {
	// 确保流式
	reqCopy := *req
	reqCopy.Stream = true

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := BuildRequestURL(baseURL, "/chat/completions", cfg, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	SetAuthHeader(httpReq, apiKey, cfg)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	// 非 2xx 状态码处理
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseError(resp.StatusCode, respBody)
	}

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		scanner.Split(bufio.ScanLines)

		for scanner.Scan() {
			line := scanner.Text()

			// 跳过空行和 SSE 注释行
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			// 只处理 data: 前缀
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			// 提取 data 内容，兼容 "data: " 和 "data:" 两种格式
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)

			if data == "" {
				continue
			}

			// 检测流结束标志
			if data == "[DONE]" {
				ch <- model.StreamChunk{Done: true}
				return
			}

			// 解析 chunk JSON
			var chunk model.ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// 解析失败不中断流，跳过该 chunk（参考 new-api 容错处理）
				continue
			}

			// 跳过空 choices 的 chunk（Azure/某些厂商发送的 keep-alive 或纯 usage chunk）
			if cfg.EmptyChoicesSkip && len(chunk.Choices) == 0 {
				// 但如果 chunk 中含 usage 信息（最后一个 chunk），仍然发送
				// 暂时跳过，后续可增强
				continue
			}

			ch <- model.StreamChunk{Data: &chunk}
		}

		if err := scanner.Err(); err != nil {
			ch <- model.StreamChunk{Error: fmt.Errorf("read stream: %w", err)}
			return
		}

		// 流正常结束但未收到 [DONE] 标记
		ch <- model.StreamChunk{Done: true}
	}()

	return ch, nil
}

