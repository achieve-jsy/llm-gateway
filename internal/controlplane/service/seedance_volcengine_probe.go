package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	seedancevolcengine "github.com/example/llm-gateway/internal/providers/seedance/volcengine"
)

// ProbeSeedanceVolcengineConnectivity 官方 Seedance（火山引擎）连通+鉴权探针。
//
// 请求：
//
//	GET {endpoint}/api/v3/contents/generations/tasks/__gateway_health_probe__
//	（HMAC-SHA256 AK/SK 签名，不创建视频）
//
// 判定与景汐中转一致：传输失败 / 401 / 403 / 5xx → 失败；其余（典型 404）→ 成功。
func ProbeSeedanceVolcengineConnectivity(ctx context.Context, endpoint, accessKey, secretKey, region string) SeedanceRelayProbeResult {
	if region == "" {
		region = "cn-beijing"
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
	seedancevolcengine.NewSigner(accessKey, secretKey, region).Sign(req)

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
			Message:      "ok: connectivity+auth only (GET tasks/__gateway_health_probe__; HMAC; not video create)",
		}
	}
}

// ParseSeedanceVolcCredentials 从渠道凭证 JSON 解析 AK/SK/region。
func ParseSeedanceVolcCredentials(credJSON string) (accessKey, secretKey, region string) {
	return parseSeedanceVolcCredentials(credJSON)
}

// parseSeedanceVolcCredentials 从渠道凭证 JSON 解析 AK/SK/region。
func parseSeedanceVolcCredentials(credJSON string) (accessKey, secretKey, region string) {
	var m map[string]string
	if err := json.Unmarshal([]byte(credJSON), &m); err != nil {
		return "", "", "cn-beijing"
	}
	accessKey = firstNonEmpty(m["access_key"], m["access_key_id"], m["ak"])
	secretKey = firstNonEmpty(m["secret_key"], m["secret_access_key"], m["sk"])
	region = firstNonEmpty(m["region"], "cn-beijing")
	return accessKey, secretKey, region
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
