package llm

// llm.go —— 极简 OpenAI 兼容 /v1/chat/completions 客户端（仅 stdlib）。
// 供 usage-report 在 LLM 分析章节调用；不引第三方依赖，不直连 DB/网关运行时。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout = 60 * time.Second
	defaultModel   = "gpt-5.6-sol" // 未指定 model 时的兜底模型
	maxTokens      = 8000    // 模型分析需生成较长的 Markdown；深度推理模型 reasoning 会吃掉大量 token
	respBodyLimit  = 1 << 20 // 响应体读取上限（1 MiB）
	errBodyLen     = 500     // 错误信息里 body 截断长度
)

// Client 是 OpenAI 兼容 chat/completions 客户端。
type Client struct {
	baseURL string // 如 https://aimeter.xk-devops.com/v1
	apiKey  string
	model   string
	client  *http.Client
	timeout time.Duration
}

// New 构造 LLM 客户端。baseURL 固定为 {service}/v1，末尾多余的 "/" 会被去掉。
func New(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: defaultTimeout},
		timeout: defaultTimeout,
	}
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

// Complete 调用 {base}/chat/completions，返回 assistant 文本。
// base/key 为空时返回 error；model 为空时使用默认模型。
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm client is nil")
	}
	if c.baseURL == "" {
		return "", fmt.Errorf("llm base URL is empty")
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("llm api key is empty")
	}
	model := c.model
	if model == "" {
		model = defaultModel
	}

	payload := chatRequest{
		Model: model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens: maxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm request failed: status=%d body=%s", resp.StatusCode, truncate(string(respBody), errBodyLen))
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", fmt.Errorf("decode response: %w (body=%s)", err, truncate(string(respBody), errBodyLen))
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm response: no choices (body=%s)", truncate(string(respBody), errBodyLen))
	}
	content := cr.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("llm response: empty message content (body=%s)", truncate(string(respBody), errBodyLen))
	}
	return content, nil
}

// truncate 将字符串裁剪到 n 字节并加省略号，用于错误信息中展示响应体片段。
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
