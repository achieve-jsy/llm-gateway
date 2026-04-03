package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
		Timeout: 15 * time.Second,
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

// ProbeAzureChat L2：POST {root}/openai/v1/chat/completions，极短请求。
func ProbeAzureChat(ctx context.Context, endpoint, apiKey, model string) AzureProbeResult {
	root := azure.NormalizeAzureResourceRoot(endpoint)
	model = strings.TrimSpace(model)
	if root == "" || model == "" {
		return AzureProbeResult{Success: false, Message: "azure chat probe: endpoint and model are required"}
	}
	url := root + "/openai/v1/chat/completions"
	payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}],"max_completion_tokens":16}`, model)

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return AzureProbeResult{
			Success:      false,
			ResponseTime: int(time.Since(start).Milliseconds()),
			Message:      "azure chat probe: create request failed: " + err.Error(),
		}
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return AzureProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			Message:      fmt.Sprintf("azure chat probe: connection failed: %v", err),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	code := resp.StatusCode
	if code < 200 || code >= 300 {
		msg := fmt.Sprintf("azure chat probe: HTTP %d: %s", code, truncateStr(string(body), 200))
		if strings.Contains(string(body), "DeploymentNotFound") {
			msg += " — 请确认 upstream_model_id/test_model 等于 Azure 部署名（如 gpt-5.5），不要带 azure/ 前缀"
		}
		return AzureProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      msg,
		}
	}
	return AzureProbeResult{
		Success:      true,
		ResponseTime: elapsed,
		StatusCode:   code,
		Message:      "ok: POST /openai/v1/chat/completions",
	}
}
