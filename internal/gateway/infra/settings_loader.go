package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GatewaySettings struct {
	RateLimitPerMinute   int `json:"rate_limit_per_minute"`
	MaxConcurrencyPerKey int `json:"max_concurrency_per_key"`
	// v0.7 分层限速：全局/工作室/部门/Key 四层最严胜出，0 = 不限
	GlobalRPM int `json:"global_rpm,omitempty"`
	StudioRPM int `json:"studio_rpm,omitempty"`
	DeptRPM   int `json:"dept_rpm,omitempty"`
	KeyRPM    int `json:"key_rpm,omitempty"`
}

type publicSettingsResponse struct {
	Code int             `json:"code"`
	Data GatewaySettings `json:"data"`
}

func LoadGatewaySettings(controlPlaneURL string) (*GatewaySettings, error) {
	url := strings.TrimRight(controlPlaneURL, "/") + "/api/v1/settings/public"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var settingsResp publicSettingsResponse
	if err := json.Unmarshal(body, &settingsResp); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}

	return &settingsResp.Data, nil
}