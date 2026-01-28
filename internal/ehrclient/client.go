package ehrclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Config EHR Client 配置
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
	RateLimit  float64 // requests/sec, 0 表示不限流
}

// DefaultConfig 默认配置：10s 超时、3 次重试、100 QPS 限流
func DefaultConfig() Config {
	return Config{
		BaseURL:    "https://open.example.com",
		Timeout:    10 * time.Second,
		MaxRetries: 3,
		RateLimit:  100,
	}
}

// Option 选项函数
type Option func(*Client)

// WithHTTPClient 替换底层 http.Client
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// Client EHR API 客户端
type Client struct {
	config     Config
	httpClient *http.Client
	tokenMgr   *TokenManager
	limiter    *RateLimiter
}

// NewClient 构造函数
func NewClient(cfg Config, opts ...Option) *Client {
	c := &Client{
		config:     cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		tokenMgr:   NewTokenManager(cfg.Token),
	}
	for _, opt := range opts {
		opt(c)
	}
	if cfg.RateLimit > 0 {
		c.limiter = NewRateLimiter(cfg.RateLimit)
	}
	return c
}

// TokenManager 暴露给外部，以便接入配置热加载
func (c *Client) TokenManager() *TokenManager { return c.tokenMgr }

// doRequest 核心 HTTP 请求 + 重试逻辑
func (c *Client) doRequest(ctx context.Context, method, path string, query string) ([]byte, error) {
	fullURL := strings.TrimRight(c.config.BaseURL, "/") + path
	if query != "" {
		fullURL = fullURL + "?" + query
	}

	var lastErr error
	tokenReloaded := false

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			if !sleepWithContext(ctx, backoff(attempt-1)) {
				return nil, fmt.Errorf("context cancelled during backoff: %w", lastErr)
			}
		}

		if c.limiter != nil {
			if !c.limiter.Wait(ctx.Done()) {
				return nil, fmt.Errorf("context cancelled while rate limiting")
			}
		}

		tok, err := c.tokenMgr.Acquire(ctx)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue // 网络错误，重试
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		httpErr := &ErrHTTP{StatusCode: resp.StatusCode, Method: method, URL: fullURL, Body: truncate(string(body), 2048)}

		switch {
		case httpErr.IsAuthError():
			if tokenReloaded {
				return nil, httpErr
			}
			c.tokenMgr.ReloadFromConfig()
			tokenReloaded = true
			lastErr = httpErr
			continue // 用新 token 重试一次
		case httpErr.IsRateLimited():
			if d := parseRetryAfter(resp.Header); d > 0 {
				if !sleepWithContext(ctx, d) {
					return nil, fmt.Errorf("context cancelled during rate limit wait: %w", httpErr)
				}
			}
			lastErr = httpErr
			continue
		case httpErr.IsServerError():
			lastErr = httpErr
			continue
		case httpErr.IsForbidden():
			return nil, httpErr
		default:
			return nil, httpErr
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// get 通用 GET 封装
func (c *Client) get(ctx context.Context, path string, params map[string]string, out any) error {
	body, err := c.doRequest(ctx, http.MethodGet, path, encodeQuery(params))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w (body=%s)", err, truncate(string(body), 256))
	}
	return nil
}

// ---------- public API ----------

func (c *Client) ListOrganizations(ctx context.Context) (*ListOrganizationReply, error) {
	var out ListOrganizationReply
	if err := c.get(ctx, "/open-apis/resource/v1/organizations", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListCompanies(ctx context.Context, orgID string) (*ListCompanyReply, error) {
	var out ListCompanyReply
	if err := c.get(ctx, "/open-apis/resource/v1/companies", map[string]string{"orgId": orgID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListDepartments(ctx context.Context, orgID string, deptIDs []string) (*ListDepartmentReply, error) {
	params := map[string]string{"orgId": orgID}
	var out ListDepartmentReply
	if err := c.get(ctx, "/open-apis/resource/v1/departments", withSliceParam(params, "departmentIds", deptIDs), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListEmployees(ctx context.Context, orgID string, deptIDs []string) (*ListEmployeeReply, error) {
	params := map[string]string{"orgId": orgID}
	var out ListEmployeeReply
	if err := c.get(ctx, "/open-apis/resource/v1/employees", withSliceParam(params, "departmentIds", deptIDs), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListEmployeesBatch(ctx context.Context, orgID string, employeeIDs []string) (*ListEmployeeReply, error) {
	params := map[string]string{"orgId": orgID}
	var out ListEmployeeReply
	if err := c.get(ctx, "/open-apis/resource/v1/employees/batch", withSliceParam(params, "employeeIds", employeeIDs), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListPositions(ctx context.Context, orgID string) (*ListPositionReply, error) {
	var out ListPositionReply
	if err := c.get(ctx, "/open-apis/resource/v1/positions", map[string]string{"orgId": orgID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- helpers ----------

func encodeQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	var b strings.Builder
	sep := ""
	for k, v := range params {
		if v == "" {
			continue
		}
		b.WriteString(sep)
		b.WriteString(httpEncode(k))
		b.WriteString("=")
		b.WriteString(httpEncode(v))
		sep = "&"
	}
	return b.String()
}

func withSliceParam(params map[string]string, key string, vals []string) map[string]string {
	out := make(map[string]string, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	if len(vals) > 0 {
		out[key] = strings.Join(vals, ",")
	}
	return out
}

func httpEncode(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "&", "%26")
	s = strings.ReplaceAll(s, "=", "%3D")
	return s
}

func backoff(attempt int) time.Duration {
	d := time.Second * (1 << attempt) // 1, 2, 4, 8, 16
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(time.Second))) - time.Second/2
	return d + jitter
}

func parseRetryAfter(h http.Header) time.Duration {
	// 优先识别 Retry-After 头（秒级或 HTTP 日期）
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	return 0
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
