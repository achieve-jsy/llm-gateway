// Package httpclient provides a centralized HTTP client factory with unified configuration.
package httpclient

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// ClientConfig holds configuration options for creating HTTP clients
type ClientConfig struct {
	// MaxIdleConns controls the maximum number of idle (keep-alive) connections across all hosts
	MaxIdleConns int

	// MaxIdleConnsPerHost controls the maximum idle (keep-alive) connections to keep per-host
	MaxIdleConnsPerHost int

	// IdleConnTimeout is the maximum amount of time an idle (keep-alive) connection will remain idle before closing itself
	IdleConnTimeout time.Duration

	// Timeout specifies a time limit for requests made by the client
	Timeout time.Duration

	// DialTimeout is the maximum amount of time a dial will wait for a connect to complete
	DialTimeout time.Duration

	// KeepAlive specifies the interval between keep-alive probes for an active network connection
	KeepAlive time.Duration

	// TLSHandshakeTimeout specifies the maximum amount of time to wait for a TLS handshake
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout specifies the amount of time to wait for a server's response headers
	ResponseHeaderTimeout time.Duration
}

// getEnvDuration reads a duration from an environment variable, returning the default if not set or invalid.
// Accepts either plain integers (interpreted as seconds) or Go duration strings (e.g., "10m", "1h30m").
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	// Try parsing as integer seconds first (simpler for env config)
	if secs, err := strconv.Atoi(val); err == nil {
		return time.Duration(secs) * time.Second
	}
	// Fall back to Go duration format (e.g., "10m", "1h30m")
	if d, err := time.ParseDuration(val); err == nil {
		return d
	}
	return defaultVal
}

// DefaultConfig returns a ClientConfig with sensible defaults for API clients.
// Timeout values match OpenAI/Anthropic SDK defaults (10 minutes).
// Can be overridden via environment variables (values in seconds, or Go duration format):
//   - HTTP_TIMEOUT: overall request timeout (default: 600)
//   - HTTP_RESPONSE_HEADER_TIMEOUT: time to wait for response headers (default: 600)
//
// Note: These env vars are also documented in config.HTTPConfig. The env var bridge
// here works correctly because the llm-gateway entrypoint loads .env before config and
// providers are initialized.
func DefaultConfig() ClientConfig {
	defaultLongTimeout := 600 * time.Second
	return ClientConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		Timeout:               getEnvDuration("HTTP_TIMEOUT", defaultLongTimeout),
		DialTimeout:           30 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: getEnvDuration("HTTP_RESPONSE_HEADER_TIMEOUT", defaultLongTimeout),
	}
}

// NewHTTPClient creates a new HTTP client with the provided configuration.
// If config is nil, DefaultConfig() is used.
func NewHTTPClient(config *ClientConfig) *http.Client {
	if config == nil {
		cfg := DefaultConfig()
		config = &cfg
	}

	transport := &http.Transport{
		Proxy: proxyFunc(),
		DialContext: (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: config.KeepAlive,
		}).DialContext,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ForceAttemptHTTP2:     true,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}
}

// NewDefaultHTTPClient creates a new HTTP client with default configuration.
// This is a convenience function equivalent to NewHTTPClient(nil).
func NewDefaultHTTPClient() *http.Client {
	return NewHTTPClient(nil)
}

// proxyFunc returns the proxy function for LLM outbound requests.
//
// Priority:
//  1. LLM_PROXY env var — a fixed proxy URL used only for LLM API requests,
//     e.g. LLM_PROXY=http://user:pass@192.0.2.10:18080
//     This does NOT affect any other HTTP clients in the process (auth, control-plane, etc.)
//  2. Falls back to http.ProxyFromEnvironment (HTTP_PROXY / HTTPS_PROXY / NO_PROXY)
//     when LLM_PROXY is not set.
func proxyFunc() func(*http.Request) (*url.URL, error) {
	raw := os.Getenv("LLM_PROXY")
	if raw == "" {
		return http.ProxyFromEnvironment
	}
	proxyURL, err := url.Parse(raw)
	if err != nil || proxyURL.Host == "" {
		return http.ProxyFromEnvironment
	}
	return http.ProxyURL(proxyURL)
}
