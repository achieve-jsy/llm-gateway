package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// seedanceRelayHealthProbeTaskID 假任务 ID，仅用于连通+鉴权探针。
// 不表示真实任务存在；上游在 Key 正确时通常返回业务层「任务不存在」（如 HTTP 404）。
const seedanceRelayHealthProbeTaskID = "__gateway_health_probe__"

// SeedanceRelayProbeResult 景汐（seedance-relay）共享探针结果。
//
// 语义（检测 = 探测，所有入口共用本结果）：
//   - 检测：网络可达 + Bearer Key 被上游接受 + 上游非 5xx
//   - 不检测：视频创建、模型名、素材 API、额度
type SeedanceRelayProbeResult struct {
	Success      bool
	ResponseTime int
	StatusCode   int
	Message      string
}

// ProbeSeedanceRelayConnectivity 景汐中转连通+鉴权探针（唯一实现）。
//
// 请求：
//
//	GET {endpoint}/seedance/api/v3/contents/generations/tasks/__gateway_health_probe__
//	Authorization: Bearer {apiKey}
//
// 判定：
//   - 失败：传输错误、HTTP 401/403、HTTP 5xx
//   - 成功：其余状态码（典型 404 = 鉴权通过后的业务未找到）
//
// 定时健康检查、管理面「测试连接」、模型检测（seedance-relay）必须调用本函数，
// 不得各自再写一套 /models 或 chat 判定。
func ProbeSeedanceRelayConnectivity(ctx context.Context, endpoint, apiKey string) SeedanceRelayProbeResult {
	base := strings.TrimRight(endpoint, "/")
	url := base + "/seedance/api/v3/contents/generations/tasks/" + seedanceRelayHealthProbeTaskID

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
			Message:      "seedance-relay probe: create request failed: " + err.Error(),
		}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			Message:      fmt.Sprintf("seedance-relay probe: connection failed: %v", err),
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
			Message:      fmt.Sprintf("seedance-relay probe: auth failed: HTTP %d: %s", code, truncateStr(string(body), 200)),
		}
	case code >= 500 && code <= 599:
		return SeedanceRelayProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      fmt.Sprintf("seedance-relay probe: upstream unavailable: HTTP %d: %s", code, truncateStr(string(body), 200)),
		}
	default:
		return SeedanceRelayProbeResult{
			Success:      true,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      "ok: connectivity+auth only (GET tasks/__gateway_health_probe__; not video create)",
		}
	}
}
