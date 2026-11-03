package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/azure"
)

// AzureProbeResult Azure OpenAI v1 共享探针结果。
type AzureProbeResult struct {
	Success      bool
	ResponseTime int
	StatusCode   int
	Message      string
}

// ProbeAzureConnectivity L1：GET {root}/openai/v1/models + api-key。
// 验证网络可达与 Key；不消耗推理额度。
func ProbeAzureConnectivity(ctx context.Context, endpoint, apiKey string) AzureProbeResult {
	root := azure.NormalizeAzureResourceRoot(endpoint)
	if root == "" {
		return AzureProbeResult{Success: false, Message: "azure probe: empty endpoint"}
	}
	url := root + "/openai/v1/models"

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AzureProbeResult{
			Success:      false,
			ResponseTime: int(time.Since(start).Milliseconds()),
			Message:      "azure probe: create request failed: " + err.Error(),
		}
	}
	req.Header.Set("api-key", apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return AzureProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			Message:      fmt.Sprintf("azure probe: connection failed: %v", err),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	code := resp.StatusCode

	if code < 200 || code >= 300 {
		return AzureProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      fmt.Sprintf("azure probe: HTTP %d: %s", code, truncateStr(string(body), 200)),
		}
	}
	return AzureProbeResult{
		Success:      true,
		ResponseTime: elapsed,
		StatusCode:   code,
		Message:      "ok: GET /openai/v1/models (api-key)",
	}
}

// ProbeAzureChat L2：走 azure adapter 发一次极短的 chat 请求。
//
// 与 L1 的分工不变：L1 只验证网络可达 + Key 有效（GET /openai/v1/models），
// L2 额外验证模型/部署名正确。区别是 L2 不再手拼 URL、鉴权头和请求体 ——
// 这些交给 adapter，探针与线上转发共用同一条代码路径。
func ProbeAzureChat(ctx context.Context, endpoint, apiKey, model string) AzureProbeResult {
	root := azure.NormalizeAzureResourceRoot(endpoint)
	model = strings.TrimSpace(model)
	if root == "" || model == "" {
		return AzureProbeResult{Success: false, Message: "azure chat probe: endpoint and model are required"}
	}

	p := azure.New(providers.ProviderConfig{
		BaseURL: endpoint,
		APIKey:  apiKey,
	}, providers.ProviderOptions{})

	ok, elapsed, errMsg := probeViaAdapter(ctx, p, model)
	if !ok {
		// Azure 对「部署名不存在」和「模型不存在」返回同一个 404，
		// 这里保留原先的排障提示，避免运维把部署名配错当成渠道故障。
		if strings.Contains(errMsg, "DeploymentNotFound") {
			errMsg += " — 请确认 upstream_model_id/test_model 等于 Azure 部署名（如 gpt-5.5），不要带 azure/ 前缀"
		}
		return AzureProbeResult{Success: false, ResponseTime: elapsed, Message: "azure chat probe: " + errMsg}
	}
	return AzureProbeResult{
		Success:      true,
		ResponseTime: elapsed,
		Message:      "ok: chat completion via adapter",
	}
}
