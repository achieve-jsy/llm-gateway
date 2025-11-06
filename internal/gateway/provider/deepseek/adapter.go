package deepseek

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

const (
	defaultBaseURL = "https://api.deepseek.com/v1"
)

// Config DeepSeek 适配器配置
type Config struct {
	APIKey  string   // API Key
	BaseURL string   // 基础 URL，默认为 DeepSeek 官方端点
	Models  []string // 支持的模型列表
}

// Adapter DeepSeek Provider 适配器，实现 Provider interface
// DeepSeek 完全 OpenAI 兼容，复用通用 SSE 解析逻辑
type Adapter struct {
	config    Config
	client    *http.Client
	vendorCfg provider.VendorConfig
}

// New 创建 DeepSeek 适配器实例
func New(cfg Config) provider.Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
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
	return "deepseek"
}

// Chat 普通调用（非流式），POST /chat/completions
//
// 注意：OpenAICompatChat 内部会通过 BuildRequestURL 追加 /chat/completions，
// 因此此处只能传 BaseURL，不能再额外拼接路径，避免双路径 bug。
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return provider.OpenAICompatChat(a.client, a.config.BaseURL, req, a.config.APIKey, a.vendorCfg, ToDeepSeekError)
}

// ChatStream 流式调用，返回 chunk channel
// DeepSeek 完全 OpenAI 兼容，支持 reasoning_content 字段
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	return provider.OpenAICompatStream(a.client, a.config.BaseURL, req, a.config.APIKey, a.vendorCfg, ToDeepSeekError)
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
			OwnedBy: "deepseek",
		})
	}
	return models, nil
}

// HealthCheck 通过 HEAD 请求检查服务连通性
func (a *Adapter) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, a.config.BaseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

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

