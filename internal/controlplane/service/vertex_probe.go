package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VertexProbeResult Google Vertex (native) 共享探针结果。
type VertexProbeResult struct {
	Success      bool
	ResponseTime int
	StatusCode   int
	Message      string
}

// ProbeVertexGenerate 对 Google Vertex 原生面做一次 generateContent 调用。
// 原生面路径为 {endpoint}/models/{model}:generateContent，鉴权用 x-goog-api-key 头，
// 请求体为 generateContent 格式（contents），而非 OpenAI 兼容的 {endpoint}/chat/completions + Bearer。
//
// 探针一次调用同时验证：网络可达 + Key 有效 + 模型名正确（等价于 OpenAI 兼容的 /chat/completions）。
func ProbeVertexGenerate(ctx context.Context, endpoint, apiKey, model string) VertexProbeResult {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	model = strings.TrimSpace(model)
	if endpoint == "" || model == "" {
		return VertexProbeResult{Success: false, Message: "vertex probe: endpoint and model are required"}
	}
	// 仅对原生 Vertex 面（/publishers/google 结尾）走 generateContent；
	// 其他端点（如 OpenAI 兼容层）交由调用方按 OpenAI 兼容逻辑处理。
	url := endpoint + "/models/" + model + ":generateContent"
	payload := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"generationConfig":{"maxOutputTokens":16}}`)

	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return VertexProbeResult{
			Success:      false,
			ResponseTime: int(time.Since(start).Milliseconds()),
			Message:      "vertex probe: create request failed: " + err.Error(),
		}
	}
	if apiKey != "" {
		req.Header.Set("x-goog-api-key", apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return VertexProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			Message:      fmt.Sprintf("vertex probe: connection failed: %v", err),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	code := resp.StatusCode
	if code < 200 || code >= 300 {
		msg := fmt.Sprintf("vertex probe: HTTP %d: %s", code, truncateStr(string(body), 200))
		if code == http.StatusNotFound {
			msg += " — 请确认 endpoint 为原生 Vertex 面（/publishers/google 结尾）且模型名正确（不要带 gemini 前缀之外的路径）"
		}
		return VertexProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      msg,
		}
	}
	// 校验 candidates 是否含生成结果，避免「200 但无输出」误判为成功
	var vresp struct {
		Candidates []struct {
			Content struct {
				Parts []struct{ Text string `json:"text"` } `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &vresp); err != nil || len(vresp.Candidates) == 0 {
		return VertexProbeResult{
			Success:      false,
			ResponseTime: elapsed,
			StatusCode:   code,
			Message:      "vertex probe: HTTP 200 but empty candidates (unexpected response)",
		}
	}
	return VertexProbeResult{
		Success:      true,
		ResponseTime: elapsed,
		StatusCode:   code,
		Message:      "ok: POST /models/" + model + ":generateContent (x-goog-api-key)",
	}
}
