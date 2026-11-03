package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/providers/openai"
)

// runCheckForModel sends an HTTPS request (with challenge prompt) to the target
// endpoint and validates the response. Returns a ChannelCheckResult.
func runCheckForModel(ctx context.Context, view *ChannelMonitorView, modelName string) ChannelCheckResult {
	now := time.Now()
	result := ChannelCheckResult{
		Model:     modelName,
		Status:    MonitorStatusError,
		Message:   "",
		CheckedAt: now,
	}

	// Step 1: Ping (HEAD request to origin)
	pingLatency, pingErr := pingEndpointOrigin(ctx, view)
	if pingErr != nil {
		result.Message = fmt.Sprintf("ping failed: %s", pingErr.Error())
	}
	if pingLatency > 0 {
		ms := int(pingLatency.Milliseconds())
		result.PingLatencyMs = &ms
	}

	// Step 2: Build request body
	challenge := generateChallenge()
	reqBody := buildRequestBody(view, modelName, challenge)
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		result.Message = fmt.Sprintf("marshal request body: %s", err.Error())
		return result
	}

	// Step 3: Build URL
	apiPath := buildAPIPath(view)
	url := view.Endpoint + apiPath

	// Step 4: Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		result.Message = fmt.Sprintf("create request: %s", err.Error())
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+view.APIKey)
	for k, v := range view.ExtraHeaders {
		req.Header.Set(k, v)
	}
	if view.Provider == MonitorProviderAnthropic {
		req.Header.Set("anthropic-version", monitorAnthropicAPIVersion)
	}

	// Step 5: Create HTTP client with SSRF-safe dialer
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext:           safeDialContext,
		IdleConnTimeout:       monitorIdleConnTimeout,
		TLSHandshakeTimeout:   monitorTLSHandshakeTimeout,
		ResponseHeaderTimeout: monitorResponseHeaderTimeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   monitorRequestTimeout,
	}

	// Step 6: Execute request
	execStart := time.Now()
	resp, err := client.Do(req)
	latencyMs := int(time.Since(execStart).Milliseconds())
	result.LatencyMs = &latencyMs

	if err != nil {
		result.Message = fmt.Sprintf("request failed: %s", err.Error())
		return result
	}
	defer resp.Body.Close()

	// Step 7: Read response body (capped)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, monitorResponseMaxBytes))
	if err != nil {
		result.Message = fmt.Sprintf("read response: %s", err.Error())
		return result
	}

	// Step 8: Check HTTP status
	if resp.StatusCode >= 500 {
		result.Status = MonitorStatusFailed
		result.Message = fmt.Sprintf("upstream error: HTTP %d", resp.StatusCode)
		return result
	}
	if resp.StatusCode >= 400 {
		result.Status = MonitorStatusError
		snippet := string(bodyBytes)
		if len(snippet) > monitorErrorBodySnippetMaxBytes {
			snippet = snippet[:monitorErrorBodySnippetMaxBytes]
		}
		result.Message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet)
		return result
	}

	// Step 9: Validate challenge response
	responseText := extractResponseText(view.Provider, view.APIMode, bodyBytes)
	if !validateChallenge(responseText, challenge.Expected) {
		result.Status = MonitorStatusFailed
		result.Message = fmt.Sprintf("challenge failed: expected %s, got %q", challenge.Expected, truncateMsg(responseText, 100))
		return result
	}

	// Step 10: Determine status based on latency
	if latencyMs >= int(monitorDegradedThreshold.Milliseconds()) {
		result.Status = MonitorStatusDegraded
		result.Message = fmt.Sprintf("latency %dms exceeds threshold %dms", latencyMs, int(monitorDegradedThreshold.Milliseconds()))
	} else {
		result.Status = MonitorStatusOperational
	}
	return result
}

// pingEndpointOrigin sends a HEAD request to the endpoint origin.
func pingEndpointOrigin(ctx context.Context, view *ChannelMonitorView) (time.Duration, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext:           safeDialContext,
		IdleConnTimeout:       monitorIdleConnTimeout,
		TLSHandshakeTimeout:   monitorTLSHandshakeTimeout,
		ResponseHeaderTimeout: monitorPingTimeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   monitorPingTimeout,
	}

	ctx, cancel := context.WithTimeout(ctx, monitorPingTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, view.Endpoint, nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return latency, err
	}
	defer resp.Body.Close()
	// Drain small body
	io.Copy(io.Discard, io.LimitReader(resp.Body, monitorPingDiscardMaxBytes))
	return latency, nil
}

// buildRequestBody constructs the request body based on provider and api_mode.
func buildRequestBody(view *ChannelMonitorView, modelName string, challenge monitorChallenge) map[string]interface{} {
	base := buildBaseBody(view.Provider, view.APIMode, modelName, challenge)

	if view.BodyOverrideMode == MonitorBodyOverrideModeReplace {
		merged := map[string]interface{}(view.BodyOverride)
		merged["model"] = modelName
		return merged
	}
	if view.BodyOverrideMode == MonitorBodyOverrideModeMerge {
		merged := make(map[string]interface{})
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range view.BodyOverride {
			merged[k] = v
		}
		merged["model"] = modelName
		return merged
	}
	return base
}

func buildBaseBody(provider, apiMode, modelName string, challenge monitorChallenge) map[string]interface{} {
	switch provider {
	case MonitorProviderOpenAI:
		return buildOpenAIBody(apiMode, modelName, challenge)
	case MonitorProviderAnthropic:
		return buildAnthropicBody(modelName, challenge)
	case MonitorProviderGemini:
		return buildGeminiBody(modelName, challenge)
	default:
		return buildOpenAIBody(apiMode, modelName, challenge)
	}
}

func buildOpenAIBody(apiMode, modelName string, challenge monitorChallenge) map[string]interface{} {
	if apiMode == MonitorAPIModeResponses {
		return map[string]interface{}{
			"model": modelName,
			"input": challenge.Prompt,
			"max_output_tokens": monitorChallengeMaxTokens,
		}
	}
	body := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": challenge.Prompt},
		},
		"max_tokens": monitorChallengeMaxTokens,
	}
	if temp := openai.FixedTemperatureForModel(modelName); temp != nil {
		body["temperature"] = *temp
	} else {
		body["temperature"] = 0
	}
	return body
}

func buildAnthropicBody(modelName string, challenge monitorChallenge) map[string]interface{} {
	return map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": challenge.Prompt},
		},
		"max_tokens": monitorChallengeMaxTokens,
	}
}

func buildGeminiBody(modelName string, challenge monitorChallenge) map[string]interface{} {
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": challenge.Prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": monitorChallengeMaxTokens,
		},
	}
}

// buildAPIPath returns the API path for the given provider + api_mode.
func buildAPIPath(view *ChannelMonitorView) string {
	switch view.Provider {
	case MonitorProviderOpenAI:
		if view.APIMode == MonitorAPIModeResponses {
			return providerOpenAIResponsesPath
		}
		return providerOpenAIPath
	case MonitorProviderAnthropic:
		return providerAnthropicPath
	case MonitorProviderGemini:
		return fmt.Sprintf(providerGeminiPathTemplate, view.PrimaryModel)
	default:
		return providerOpenAIPath
	}
}

// extractResponseText extracts the response text from the provider-specific JSON.
func extractResponseText(provider, apiMode string, body []byte) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body)
	}

	switch provider {
	case MonitorProviderOpenAI:
		if apiMode == MonitorAPIModeResponses {
			// OpenAI Responses API: {"output": [{"type": "message", "content": [{"type": "output_text", "text": "..."}]}]}
			if output, ok := parsed["output"].([]interface{}); ok && len(output) > 0 {
				if msg, ok := output[0].(map[string]interface{}); ok {
					if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
						if c, ok := content[0].(map[string]interface{}); ok {
							if t, ok := c["text"].(string); ok {
								return t
							}
						}
					}
				}
			}
		}
		// OpenAI Chat Completions: {"choices": [{"message": {"content": "..."}}]}
		if choices, ok := parsed["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := msg["content"].(string); ok {
						return content
					}
				}
			}
		}
	case MonitorProviderAnthropic:
		// Anthropic: {"content": [{"type": "text", "text": "..."}]}
		if content, ok := parsed["content"].([]interface{}); ok && len(content) > 0 {
			if c, ok := content[0].(map[string]interface{}); ok {
				if t, ok := c["text"].(string); ok {
					return t
				}
			}
		}
	case MonitorProviderGemini:
		// Gemini: {"candidates": [{"content": {"parts": [{"text": "..."}]}}]}
		if candidates, ok := parsed["candidates"].([]interface{}); ok && len(candidates) > 0 {
			if c, ok := candidates[0].(map[string]interface{}); ok {
				if content, ok := c["content"].(map[string]interface{}); ok {
					if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
						if p, ok := parts[0].(map[string]interface{}); ok {
							if t, ok := p["text"].(string); ok {
								return t
							}
						}
					}
				}
			}
		}
	}
	return string(body)
}

func truncateMsg(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}