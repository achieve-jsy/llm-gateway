package bailian

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

const (
	// 百炼 DashScope OpenAI 兼容模式端点
	defaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

// Config 百炼适配器配置
type Config struct {
	APIKey  string   // API Key
	BaseURL string   // 基础 URL，默认为 DashScope 兼容端点
	Models  []string // 支持的模型列表
}

// Adapter 百炼 Provider 适配器，实现 Provider interface
// 百炼走 DashScope OpenAI 兼容模式，完全复用 OpenAI 兼容的 SSE 解析逻辑
type Adapter struct {
	config    Config
	client    *http.Client
	vendorCfg provider.VendorConfig
}

// New 创建百炼适配器实例
func New(cfg Config) provider.Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Adapter{
		config:    cfg,
		vendorCfg: provider.DefaultVendorConfig,
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
			},
		},
	}
}

// Name 返回 Provider 名称
func (a *Adapter) Name() string {
	return "bailian"
}

// Chat 普通调用（非流式），POST /chat/completions
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return provider.OpenAICompatChatWithContext(ctx, a.client, a.config.BaseURL, req, a.config.APIKey, a.vendorCfg, ToBailianError)
}

// ChatStream 流式调用，返回 chunk channel
// 使用改进的 SSE 解析逻辑：
// - bufio.Scanner 逐行读取
// - 处理 data: [DONE] 终止
// - 跳过空 choices chunk
// - 兼容 reasoning_content 字段
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	return provider.OpenAICompatStreamWithContext(ctx, a.client, a.config.BaseURL, req, a.config.APIKey, a.vendorCfg, ToBailianError)
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
			OwnedBy: "bailian",
		})
	}
	return models, nil
}

// HealthCheck 通过 HEAD 请求检查百炼服务连通性
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

