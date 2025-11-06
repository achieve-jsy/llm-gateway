package litellm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

// Config litellm 通用 OpenAI 兼容适配器配置
type Config struct {
	Endpoint string   // 后端服务 endpoint（如 http://localhost:4000）
	APIKey   string   // API Key（可选）
	Models   []string // 支持的模型列表
}

// Adapter 通用 OpenAI 兼容 Provider 适配器
// 复用通用 SSE 解析逻辑，适用于任何 OpenAI 兼容接口
type Adapter struct {
	config    Config
	client    *http.Client
	vendorCfg provider.VendorConfig
}

// New 创建 litellm 适配器实例
func New(cfg Config) provider.Provider {
	// 去除尾部斜杠
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	return &Adapter{
		config:    cfg,
		vendorCfg: provider.DefaultVendorConfig,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name 返回 Provider 名称
func (a *Adapter) Name() string {
	return "litellm"
}

// Chat 普通调用（非流式），POST {endpoint}/v1/chat/completions
//
// 注意：OpenAICompatChat 内部会通过 BuildRequestURL 追加 /chat/completions，
// 因此 baseURL 只能传到 /v1，不能再额外拼 /chat/completions，避免双路径 bug。
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return provider.OpenAICompatChat(a.client, a.config.Endpoint+"/v1", req, a.config.APIKey, a.vendorCfg, ToLiteLLMError)
}

// ChatStream 流式调用，返回 chunk channel
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	return provider.OpenAICompatStream(a.client, a.config.Endpoint+"/v1", req, a.config.APIKey, a.vendorCfg, ToLiteLLMError)
}

// ListModels 返回配置中的模型列表
func (a *Adapter) ListModels(_ context.Context) ([]model.ModelInfo, error) {
	models := make([]model.ModelInfo, 0, len(a.config.Models))
	now := time.Now().Unix()
	for _, m := range a.config.Models {
		models = append(models, model.ModelInfo{
			ID:      m,
			Object:  "model",
			Created: now,
			OwnedBy: "litellm",
		})
	}
	return models, nil
}

// HealthCheck 通过 HEAD 请求检查服务连通性
func (a *Adapter) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, a.config.Endpoint+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	if a.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}
	return nil
}

