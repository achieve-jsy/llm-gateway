package infra

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ModelHealthEntry model health info from control-plane
type ModelHealthEntry struct {
	Name                string `json:"name"`
	ModelType           string `json:"model_type"`
	Enabled             *bool  `json:"enabled"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastCheckError      string `json:"last_check_error"`
	ResponseTime        int    `json:"response_time"`
}

// LoadModelHealth fetches health data for given model names from control-plane
func LoadModelHealth(controlPlaneURL, internalToken string, names []string) (map[string]ModelHealthEntry, error) {
	if len(names) == 0 {
		return map[string]ModelHealthEntry{}, nil
	}

	query := url.Values{}
	for _, name := range names {
		query.Add("names", name)
	}

	urlStr := strings.TrimRight(controlPlaneURL, "/") + "/internal/v1/models/health?" + query.Encode()

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Token", internalToken)

	client := &http.Client{Timeout: 5 * time.Second}
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

	var result struct {
		Data map[string]ModelHealthEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result.Data, nil
}