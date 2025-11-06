package infra

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// AvailableModel 从 control-plane 获取的可用模型信息
type AvailableModel struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	ModelType           string `json:"model_type"`
	ProviderID          string `json:"provider_id"`
	ProviderName        string `json:"provider_name"`
	ProviderType        string `json:"provider_type"`
	ProviderEndpoint    string `json:"provider_endpoint"`
	ProviderCredentials string `json:"provider_credentials"`
}

// LoadFromControlPlane 从 control-plane 拉取所有可用模型配置
func LoadFromControlPlane(controlPlaneURL, internalToken string) ([]model.ProviderEntry, error) {
	url := strings.TrimRight(controlPlaneURL, "/") + "/internal/v1/models/available"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Token", internalToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request control-plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("control-plane returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// control-plane 返回格式: {"data": [...]}
	var result struct {
		Data []AvailableModel `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// 按 provider 聚合模型
	providerMap := make(map[string]*model.ProviderEntry)
	for _, m := range result.Data {
		key := m.ProviderID
		entry, ok := providerMap[key]
		if !ok {
			entry = &model.ProviderEntry{
				ID:          m.ProviderID,
				Name:        m.ProviderName,
				Type:        m.ProviderType,
				Endpoint:    m.ProviderEndpoint,
				Credentials: m.ProviderCredentials,
				Enabled:     true,
				Models:      []string{},
			}
			providerMap[key] = entry
		}
		entry.Models = append(entry.Models, m.Name)
	}

	entries := make([]model.ProviderEntry, 0, len(providerMap))
	for _, entry := range providerMap {
		entries = append(entries, *entry)
	}

	return entries, nil
}
