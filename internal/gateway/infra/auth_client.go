package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/httpclient"
	"golang.org/x/sync/singleflight"
)

// AuthClient HTTP client that calls auth service verify-key endpoint
type AuthClient struct {
	base     string
	token    string
	client   *http.Client
	keyCache *KeyCache
	sf       singleflight.Group // 防缓存穿透：同一 key 同时只有一个飞行中 HTTP 请求
}

// NewAuthClient creates a new AuthClient
func NewAuthClient(baseURL, internalToken string) *AuthClient {
	// 使用项目标准 HTTP Client（MaxIdleConnsPerHost=100），替代裸 http.Client
	cfg := httpclient.DefaultConfig()
	cfg.Timeout = 3 * time.Second
	cfg.ResponseHeaderTimeout = 3 * time.Second

	return &AuthClient{
		base:   strings.TrimRight(baseURL, "/"),
		token:  internalToken,
		client: httpclient.NewHTTPClient(&cfg),
	}
}

// SetKeyCache 设置 KeyCache 以启用双层缓存。
func (c *AuthClient) SetKeyCache(kc *KeyCache) {
	c.keyCache = kc
}

// VerifyKeyResp is the response from auth service verify-key endpoint
type VerifyKeyResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Status          string `json:"status"`
		KeyID           string `json:"key_id,omitempty"`
		KeyName         string `json:"key_name,omitempty"`
		AccessAllGroups bool   `json:"access_all_groups"`
		LogEnabled      *bool  `json:"log_enabled,omitempty"` // nil = 字段缺失（旧缓存），默认 true
		User            struct {
			UserID       string `json:"user_id"`
			Name         string `json:"name"`
			DepartmentID string `json:"department_id,omitempty"`
			DeptName     string `json:"dept_name,omitempty"`
			StudioID     string `json:"studio_id,omitempty"`
			StudioName   string `json:"studio_name,omitempty"`
		} `json:"user,omitempty"`
	} `json:"data"`
}

// VerifyKey calls auth service to verify an API key.
// 查询顺序：本地内存缓存 → Redis 缓存 → singleflight 合并的 HTTP 调用。
// 同一个 key 在缓存到期的瞬间，多个并发请求只会触发一次 HTTP 调用。
func (c *AuthClient) VerifyKey(ctx context.Context, key string) (*VerifyKeyResp, error) {
	// 1. 双层缓存命中，纳秒/微秒级返回
	if c.keyCache != nil {
		if resp, ok := c.keyCache.Get(ctx, key); ok {
			slog.DebugContext(ctx, "auth-client: cache hit", "key_suffix", keySuffix(key))
			return resp, nil
		}
	}

	// 2. 缓存未命中：用 singleflight 合并对同一 key 的并发穿透
	// sfKey 使用 hash 避免原始 key 作为 map key 驻留内存
	sfKey := c.sfKey(key)
	val, err, _ := c.sf.Do(sfKey, func() (any, error) {
		// double-check：等待期间可能已被其他 goroutine 填充
		if c.keyCache != nil {
			if resp, ok := c.keyCache.Get(ctx, key); ok {
				return resp, nil
			}
		}
		resp, err := c.verifyKeyHTTP(ctx, key)
		if err != nil {
			return nil, err
		}
		if c.keyCache != nil {
			c.keyCache.Set(ctx, key, resp)
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*VerifyKeyResp), nil
}

// sfKey 返回用于 singleflight 分组的 key，复用 KeyCache 的 hash 逻辑。
func (c *AuthClient) sfKey(key string) string {
	if c.keyCache != nil {
		return c.keyCache.keyHash(key)
	}
	// 无 KeyCache 时直接用原始 key（不影响正确性）
	return key
}

func (c *AuthClient) verifyKeyHTTP(ctx context.Context, key string) (*VerifyKeyResp, error) {
	targetURL := c.base + "/internal/v1/auth/verify-key"
	slog.DebugContext(ctx, "auth-client: HTTP verify-key", "url", targetURL)

	// 用具名 struct 替代临时 map，避免 map 堆分配
	type verifyKeyReq struct {
		Key string `json:"key"`
	}
	body, _ := json.Marshal(verifyKeyReq{Key: key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("auth service error %d", resp.StatusCode)
	}

	// 流式 JSON 解码，避免 io.ReadAll + string 转换的额外内存拷贝
	var result VerifyKeyResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("auth service response decode failed: %w", err)
	}
	return &result, nil
}

// keySuffix 返回 key 的末 4 位，用于日志（避免打印完整 key）。
func keySuffix(key string) string {
	if len(key) <= 4 {
		return key
	}
	return "..." + key[len(key)-4:]
}
