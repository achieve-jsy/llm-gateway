package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AuthClient HTTP client that calls auth service verify-key endpoint
type AuthClient struct {
	base     string
	token    string
	client   *http.Client
	keyCache *KeyCache
}

// NewAuthClient creates a new AuthClient
func NewAuthClient(baseURL, internalToken string) *AuthClient {
	return &AuthClient{
		base:   strings.TrimRight(baseURL, "/"),
		token:  internalToken,
		client: &http.Client{Timeout: 3 * time.Second},
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
		AccessAllGroups bool  `json:"access_all_groups"`
		LogEnabled      *bool `json:"log_enabled,omitempty"` // nil = 字段缺失（旧缓存），默认 true
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
// When KeyCache is configured, it first checks the local+Redis cache.
func (c *AuthClient) VerifyKey(ctx context.Context, key string) (*VerifyKeyResp, error) {
	if c.keyCache != nil {
		if resp, ok := c.keyCache.Get(ctx, key); ok {
			log.Printf("[auth-client] cache HIT for key=...%s log_enabled=%v", key[len(key)-4:], resp.Data.LogEnabled)
			return resp, nil
		}
	}

	resp, err := c.verifyKeyHTTP(ctx, key)
	if err != nil {
		return nil, err
	}
	log.Printf("[auth-client] HTTP response for key=...%s log_enabled=%v", key[len(key)-4:], resp.Data.LogEnabled)

	if c.keyCache != nil {
		c.keyCache.Set(ctx, key, resp)
	}
	return resp, nil
}

func (c *AuthClient) verifyKeyHTTP(ctx context.Context, key string) (*VerifyKeyResp, error) {
	targetURL := c.base + "/internal/v1/auth/verify-key"
	log.Printf("[auth-client] HTTP call to: %s", targetURL)
	body, _ := json.Marshal(map[string]string{"key": key})
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
	rawBody, _ := io.ReadAll(resp.Body)
	log.Printf("[auth-client] raw response: %s", string(rawBody))
	var result VerifyKeyResp
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
