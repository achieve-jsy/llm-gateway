package volcengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core/video"
)

// Adapter 火山引擎官方适配器。
// 鉴权由 Creds.AuthType 决定：api_key → Bearer；aksk → HMAC。不做静默回退。
type Adapter struct {
	name     string
	baseURL  string
	authType AuthType
	apiKey   string
	signer   *Signer
	client   *http.Client
}

// NewAdapter 创建官方适配器（HMAC AK/SK）。
func NewAdapter(name, baseURL, accessKey, secretKey, region string) *Adapter {
	return NewAdapterFromCreds(name, baseURL, Creds{
		AuthType:  AuthAKSK,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
	})
}

// NewBearerAdapter 创建方舟 API Key（Bearer）适配器。
func NewBearerAdapter(name, baseURL, apiKey string) *Adapter {
	return NewAdapterFromCreds(name, baseURL, Creds{
		AuthType: AuthAPIKey,
		APIKey:   apiKey,
		Region:   "cn-beijing",
	})
}

// NewAdapterWithAuth 兼容旧调用：按字段推断 auth_type 后构建（有 api_key 则 Bearer）。
func NewAdapterWithAuth(name, baseURL, apiKey, accessKey, secretKey, region string) *Adapter {
	c := Creds{
		APIKey:    apiKey,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
	}
	if strings.TrimSpace(apiKey) != "" {
		c.AuthType = AuthAPIKey
	} else {
		c.AuthType = AuthAKSK
	}
	return NewAdapterFromCreds(name, baseURL, c)
}

// NewAdapterFromCreds 按显式/规范化后的 Creds 构建适配器。
func NewAdapterFromCreds(name, baseURL string, cred Creds) *Adapter {
	baseURL = strings.TrimSuffix(baseURL, "/")
	cred = cred.Normalized()
	a := &Adapter{
		name:     name,
		baseURL:  baseURL,
		authType: cred.AuthType,
		apiKey:   cred.APIKey,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	if cred.AuthType == AuthAKSK && cred.AccessKey != "" && cred.SecretKey != "" {
		a.signer = NewSigner(cred.AccessKey, cred.SecretKey, cred.Region)
	}
	return a
}

// Name 返回适配器名称
func (a *Adapter) Name() string {
	return a.name
}

func (a *Adapter) applyAuth(req *http.Request) error {
	switch a.authType {
	case AuthAPIKey:
		if a.apiKey == "" {
			return fmt.Errorf("seedance-volcengine: auth_type=api_key but api_key is empty")
		}
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
		return nil
	case AuthAKSK:
		if a.signer == nil {
			return fmt.Errorf("seedance-volcengine: auth_type=aksk but access_key/secret_key incomplete")
		}
		a.signer.Sign(req)
		return nil
	default:
		return fmt.Errorf("seedance-volcengine: missing or unsupported auth_type")
	}
}

// CreateTask 创建视频任务
func (a *Adapter) CreateTask(ctx context.Context, req json.RawMessage) (string, error) {
	url := a.baseURL + "/api/v3/contents/generations/tasks"

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	if err := a.applyAuth(httpReq); err != nil {
		return "", err
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.ID, nil
}

// GetTask 查询任务状态
func (a *Adapter) GetTask(ctx context.Context, taskID string) (json.RawMessage, error) {
	url := a.baseURL + "/api/v3/contents/generations/tasks/" + taskID

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	if err := a.applyAuth(httpReq); err != nil {
		return nil, err
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DoAction 执行素材操作
func (a *Adapter) DoAction(ctx context.Context, action string, req json.RawMessage) (json.RawMessage, error) {
	url := a.baseURL + "/assets/" + action

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	if err := a.applyAuth(httpReq); err != nil {
		return nil, err
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// Ensure Adapter implements video.VideoProvider and video.AssetProvider
var _ video.VideoProvider = (*Adapter)(nil)
var _ video.AssetProvider = (*Adapter)(nil)
