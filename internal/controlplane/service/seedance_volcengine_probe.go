package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	seedancevolcengine "github.com/example/llm-gateway/internal/providers/seedance/volcengine"
)

// SeedanceVolcCreds 方舟官方渠道凭证（包装 volcengine.Creds，保持 CP 调用方类型稳定）。
type SeedanceVolcCreds = seedancevolcengine.Creds

// ProbeSeedanceVolcengineConnectivity 官方 Seedance（火山引擎）连通+鉴权探针。
//
// 请求：
//
//	GET {endpoint}/api/v3/contents/generations/tasks/__gateway_health_probe__
//	（Bearer API Key 或 HMAC-SHA256 AK/SK，不创建视频）
//
// 判定与景汐中转一致：传输失败 / 401 / 403 / 5xx → 失败；其余（典型 404）→ 成功。
func ProbeSeedanceVolcengineConnectivity(ctx context.Context, endpoint, accessKey, secretKey, region string) SeedanceRelayProbeResult {
	return ProbeSeedanceVolcengineConnectivityCreds(ctx, endpoint, SeedanceVolcCreds{
		AuthType:  seedancevolcengine.AuthAKSK,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
	})
}

// ProbeSeedanceVolcengineConnectivityCreds 按 Creds.AuthType 严格鉴权的连通探针。
func ProbeSeedanceVolcengineConnectivityCreds(ctx context.Context, endpoint string, cred SeedanceVolcCreds) SeedanceRelayProbeResult {
	cred = cred.Normalized()
	if cred.Region == "" {
		cred.Region = "cn-beijing"
	}
	base := strings.TrimRight(endpoint, "/")
	url := base + "/api/v3/contents/generations/tasks/" + seedanceRelayHealthProbeTaskID

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: int(time.Since(start).Milliseconds()),
			Message:      "seedance-volcengine probe: create request failed: " + err.Error(),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")

	authMode := string(cred.AuthType)
	switch cred.AuthType {
	case seedancevolcengine.AuthAPIKey:
		if cred.APIKey == "" {
			return SeedanceRelayProbeResult{
				Success:      false,
				ResponseTime: int(time.Since(start).Milliseconds()),
				Message:      "seedance-volcengine: auth_type=api_key but api_key is empty",
			}
		}
		req.Header.Set("Authorization", "Bearer "+cred.APIKey)
		authMode = "Bearer"
	case seedancevolcengine.AuthAKSK:
		if cred.AccessKey == "" || cred.SecretKey == "" {
			return SeedanceRelayProbeResult{
				Success:      false,
				ResponseTime: int(time.Since(start).Milliseconds()),
				Message:      "seedance-volcengine: auth_type=aksk but access_key/secret_key incomplete",
			}
		}
		seedancevolcengine.NewSigner(cred.AccessKey, cred.SecretKey, cred.Region).Sign(req)
		authMode = "HMAC"
	default:
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: int(time.Since(start).Milliseconds()),
			Message:      "seedance-volcengine: missing or unsupported auth_type",
		}
	}

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			Message:      fmt.Sprintf("seedance-volcengine probe: connection failed: %v", err),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	code := resp.StatusCode
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      fmt.Sprintf("seedance-volcengine probe: auth failed: HTTP %d: %s", code, truncateStr(string(body), 200)),
		}
	case code >= 500 && code <= 599:
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      fmt.Sprintf("seedance-volcengine probe: upstream unavailable: HTTP %d: %s", code, truncateStr(string(body), 200)),
		}
	default:
		return SeedanceRelayProbeResult{
			Success:      true,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      fmt.Sprintf("ok: connectivity+auth only (GET tasks/__gateway_health_probe__; %s; not video create)", authMode),
		}
	}
}

// ParseSeedanceVolcCredentials 从渠道凭证 JSON 解析 AK/SK/region（兼容旧调用）。
func ParseSeedanceVolcCredentials(credJSON string) (accessKey, secretKey, region string) {
	c := ParseSeedanceVolcCreds(credJSON)
	return c.AccessKey, c.SecretKey, c.Region
}

// ParseSeedanceVolcCreds 解析方舟凭证（严格按 auth_type；无则推断）。
func ParseSeedanceVolcCreds(credJSON string) SeedanceVolcCreds {
	return seedancevolcengine.ParseCreds(credJSON)
}
