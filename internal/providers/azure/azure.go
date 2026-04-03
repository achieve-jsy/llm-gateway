package azure

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
)

// Azure OpenAI v1：统一走 {resource}/openai/v1/*，不再兼容
// /openai/deployments/{name}/... + 日期 api-version 的旧配法。
// 可选 APIVersion（如 "preview"）才会追加 ?api-version=。

var Registration = providers.Registration{
	Type:                        "azure",
	New:                         New,
	PassthroughSemanticEnricher: openai.Registration.PassthroughSemanticEnricher,
	Discovery: providers.DiscoveryConfig{
		RequireBaseURL:     true,
		SupportsAPIVersion: true, // 可选；默认不追加 dated version
	},
}

type Provider struct {
	*openai.CompatibleProvider
	apiVersion string
}

func New(providerCfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	root := NormalizeAzureResourceRoot(providers.ResolveBaseURL(providerCfg.BaseURL, "https://example.invalid"))
	apiVersion := strings.TrimSpace(providerCfg.APIVersion)
	p := &Provider{apiVersion: apiVersion}
	clientCfg := openai.CompatibleProviderConfig{
		ProviderName: "azure",
		BaseURL:      root + "/openai/v1",
		SetHeaders:   setHeaders,
	}
	p.CompatibleProvider = openai.NewCompatibleProvider(providerCfg.APIKey, opts, clientCfg)
	p.SetRequestMutator(p.mutateRequest)
	p.SetBaseURL(root)
	return p
}

func NewWithHTTPClient(apiKey string, httpClient *http.Client, hooks llmclient.Hooks) *Provider {
	p := &Provider{}
	cfg := openai.CompatibleProviderConfig{
		ProviderName: "azure",
		BaseURL:      "https://example.invalid/openai/v1",
		SetHeaders:   setHeaders,
	}
	p.CompatibleProvider = openai.NewCompatibleProviderWithHTTPClient(apiKey, httpClient, hooks, cfg)
	p.SetRequestMutator(p.mutateRequest)
	return p
}

// SetBaseURL 接受资源根或误填的旧 path，归一化后固定到 /openai/v1。
func (p *Provider) SetBaseURL(baseURL string) {
	root := NormalizeAzureResourceRoot(baseURL)
	p.CompatibleProvider.SetBaseURL(root + "/openai/v1")
}

func (p *Provider) SetAPIVersion(version string) {
	p.apiVersion = strings.TrimSpace(version)
}

func (p *Provider) mutateRequest(req *llmclient.Request) {
	if p.apiVersion == "" {
		return
	}
	endpoint, err := url.Parse(req.Endpoint)
	if err != nil {
		return
	}
	query := endpoint.Query()
	query.Set("api-version", p.apiVersion)
	endpoint.RawQuery = query.Encode()
	req.Endpoint = endpoint.String()
}

func setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("api-key", apiKey)
	if requestID := core.GetRequestID(req.Context()); requestID != "" && isValidClientRequestID(requestID) {
		req.Header.Set("X-Client-Request-Id", requestID)
	}
}

func isValidClientRequestID(id string) bool {
	if len(id) > 512 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] > 127 {
			return false
		}
	}
	return true
}

// NormalizeAzureResourceRoot 将运营误填的完整 path / query 归一为资源根。
// 例如：
//   - https://xxx.openai.azure.com/openai/v1 → https://xxx.openai.azure.com
//   - https://xxx.openai.azure.com/openai/deployments/gpt-5.5 → 资源根
//   - https://xxx.openai.azure.com/openai/responses?api-version=... → 资源根
func NormalizeAzureResourceRoot(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/openai/v1"):
		path = strings.TrimSuffix(path, "/openai/v1")
	case strings.Contains(path, "/openai/deployments/"):
		if idx := strings.Index(path, "/openai/deployments/"); idx >= 0 {
			path = path[:idx]
		}
	case strings.Contains(path, "/deployments/"):
		if idx := strings.Index(path, "/deployments/"); idx >= 0 {
			path = path[:idx]
		}
	case strings.HasSuffix(path, "/openai/responses"):
		path = strings.TrimSuffix(path, "/openai/responses")
	case strings.HasSuffix(path, "/openai"):
		path = strings.TrimSuffix(path, "/openai")
	}

	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}
