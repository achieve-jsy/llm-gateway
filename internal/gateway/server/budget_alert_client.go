package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type BudgetAlertClient struct {
	httpClient      *http.Client
	controlPlaneURL string
	internalToken   string
}

func NewBudgetAlertClient(httpClient *http.Client, controlPlaneURL, internalToken string) *BudgetAlertClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &BudgetAlertClient{
		httpClient: httpClient, controlPlaneURL: strings.TrimRight(controlPlaneURL, "/"),
		internalToken: internalToken,
	}
}

func (c *BudgetAlertClient) Send(ctx context.Context, request BudgetAlertRequest) error {
	if c == nil || c.controlPlaneURL == "" {
		return fmt.Errorf("budget alert control-plane URL is empty")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal budget alert: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.controlPlaneURL+"/internal/v1/budget/evaluate-alert", bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create budget alert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.internalToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send budget alert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("budget alert returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
