package service

import (
	"context"
	"net/url"
	"strings"
)

// validateProvider checks if the provider string is supported.
func validateProvider(p string) error {
	if !isSupportedProvider(p) {
		return fmtAppError("CHANNEL_MONITOR_INVALID_PROVIDER", "provider must be one of openai/anthropic/gemini")
	}
	return nil
}

// isSupportedProvider checks if the provider is in the adapter table.
func isSupportedProvider(p string) bool {
	switch p {
	case MonitorProviderOpenAI, MonitorProviderAnthropic, MonitorProviderGemini:
		return true
	}
	return false
}

// validateAPIMode validates provider + api_mode combination.
func validateAPIMode(provider, apiMode string) error {
	apiMode = defaultAPIMode(apiMode)
	switch apiMode {
	case MonitorAPIModeChatCompletions:
		return nil
	case MonitorAPIModeResponses:
		if provider == "" || provider == MonitorProviderOpenAI {
			return nil
		}
		return fmtAppError("CHANNEL_MONITOR_INVALID_API_MODE", "api_mode must be chat_completions or responses; responses is only supported for openai")
	default:
		return fmtAppError("CHANNEL_MONITOR_INVALID_API_MODE", "api_mode must be chat_completions or responses")
	}
}

// validateInterval checks interval_seconds range.
func validateInterval(sec int) error {
	if sec < monitorMinIntervalSeconds || sec > monitorMaxIntervalSeconds {
		return fmtAppError("CHANNEL_MONITOR_INVALID_INTERVAL", "interval_seconds must be in [15, 3600]")
	}
	return nil
}

// validateEndpoint validates endpoint:
//   - scheme must be https
//   - must be origin only (no path/query/fragment)
//   - hostname cannot be localhost/metadata/private IP
func validateEndpoint(ep string) error {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return fmtAppError("CHANNEL_MONITOR_INVALID_ENDPOINT", "endpoint must be a valid https URL")
	}
	u, err := url.Parse(ep)
	if err != nil {
		return fmtAppError("CHANNEL_MONITOR_INVALID_ENDPOINT", "endpoint must be a valid https URL")
	}
	if u.Scheme != "https" {
		return fmtAppError("CHANNEL_MONITOR_ENDPOINT_SCHEME", "endpoint must use https scheme")
	}
	if u.Host == "" {
		return fmtAppError("CHANNEL_MONITOR_INVALID_ENDPOINT", "endpoint must be a valid https URL")
	}
	if u.Path != "" && u.Path != "/" {
		return fmtAppError("CHANNEL_MONITOR_ENDPOINT_PATH", "endpoint must be base origin only (no path/query/fragment)")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmtAppError("CHANNEL_MONITOR_ENDPOINT_PATH", "endpoint must be base origin only (no path/query/fragment)")
	}

	hostname := u.Hostname()
	ctx, cancel := context.WithTimeout(context.Background(), monitorEndpointResolveTimeout)
	defer cancel()
	blocked, err := isPrivateOrLoopbackHost(ctx, hostname)
	if err != nil {
		return fmtAppError("CHANNEL_MONITOR_ENDPOINT_UNREACHABLE", "endpoint hostname could not be resolved")
	}
	if blocked {
		return fmtAppError("CHANNEL_MONITOR_ENDPOINT_PRIVATE", "endpoint must be a public host")
	}
	return nil
}

// normalizeEndpoint trims whitespace and trailing slash.
func normalizeEndpoint(ep string) string {
	return strings.TrimRight(strings.TrimSpace(ep), "/")
}

// normalizeModels removes duplicates and empty strings.
func normalizeModels(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// defaultAPIMode normalizes empty string to chat_completions.
func defaultAPIMode(apiMode string) string {
	if strings.TrimSpace(apiMode) == "" {
		return MonitorAPIModeChatCompletions
	}
	return strings.TrimSpace(apiMode)
}

// defaultBodyMode normalizes empty string to off.
func defaultBodyMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return MonitorBodyOverrideModeOff
	}
	return strings.TrimSpace(mode)
}

// emptyHeadersIfNil returns empty map for nil.
func emptyHeadersIfNil(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

// fmtAppError creates a simple application error.
func fmtAppError(code, message string) error {
	return &appError{code: code, message: message}
}

type appError struct {
	code    string
	message string
}

func (e *appError) Error() string { return e.message }
func (e *appError) Code() string  { return e.code }