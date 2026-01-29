package ehrclient

import (
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://open.example.com"
	prodAPIPrefix  = "/open-apis/ehr-apic"
	testAPIPrefix  = "/open-apis/ehrapi"
)

// DefaultConfig 默认配置：正式 OpenAPI 基址 + ehr-apic 前缀。
func DefaultConfig() Config {
	return Config{
		BaseURL:    defaultBaseURL,
		APIPrefix:  prodAPIPrefix,
		Timeout:    10 * time.Second,
		MaxRetries: 3,
		RateLimit:  100,
	}
}

// DefaultAPIPrefix 按基址推断接口中心前缀（测试 ehrapi / 正式 ehr-apic）。
func DefaultAPIPrefix(baseURL string) string {
	lower := strings.ToLower(baseURL)
	if strings.Contains(lower, "test.daqun") {
		return testAPIPrefix
	}
	return prodAPIPrefix
}

// ConfigFromEnv 从环境变量加载 EHR 客户端配置。
// 支持 EHR_BASE_URL、EHR_API_TOKEN、EHR_API_PREFIX。
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v := strings.TrimSpace(os.Getenv("EHR_BASE_URL")); v != "" {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("EHR_API_TOKEN")); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("EHR_API_PREFIX")); v != "" {
		cfg.APIPrefix = normalizeAPIPrefix(v)
	} else {
		cfg.APIPrefix = DefaultAPIPrefix(cfg.BaseURL)
	}
	return cfg
}

func normalizeAPIPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return prodAPIPrefix
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

func (c *Client) resourcePath(resource string) string {
	prefix := c.config.APIPrefix
	if prefix == "" {
		prefix = prodAPIPrefix
	}
	resource = strings.TrimPrefix(resource, "/")
	return prefix + "/resource/v1/" + resource
}
