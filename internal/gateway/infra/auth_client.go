package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthClient HTTP client that calls auth service verify-key endpoint
type AuthClient struct {
	base   string
	token  string
	client *http.Client
}

// NewAuthClient creates a new AuthClient
func NewAuthClient(baseURL, internalToken string) *AuthClient {
	return &AuthClient{
		base:   strings.TrimRight(baseURL, "/"),
		token:  internalToken,
		client: &http.Client{Timeout: 500 * time.Millisecond},
	}
}

// VerifyKeyResp is the response from auth service verify-key endpoint
type VerifyKeyResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Status string `json:"status"`
		KeyID  string `json:"key_id,omitempty"`
		User   struct {
			UserID       string `json:"user_id"`
			Name         string `json:"name"`
			DepartmentID string `json:"department_id,omitempty"`
		} `json:"user,omitempty"`
	} `json:"data"`
}

// VerifyKey calls auth service to verify an API key
func (c *AuthClient) VerifyKey(ctx context.Context, key string) (*VerifyKeyResp, error) {
	body, _ := json.Marshal(map[string]string{"key": key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/v1/auth/verify-key", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("auth service error %d", resp.StatusCode)
	}
	var result VerifyKeyResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
