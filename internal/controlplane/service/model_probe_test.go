package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeModelForward_ClassifyErrorPatterns(t *testing.T) {
	patterns := []struct {
		errMsg string
		match  bool
	}{
		{"the voice property is required", true},
		{"field required: input.text", true},
		{"input.text is missing", true},
		{"input should be a valid list", true},
		{"invalid message type", true},
		{"current user api does not support http call", true},
		{"selected model does not support image or text inputs", true},
		{"dedicated task `asr`", true},
		{"content parameter's length invalid", true},
		{"range of max_tokens should be [10, 2048]", true},
		{"range of max_tokens should be [10, 8192]", true},
		{"range of max_tokens should be [10, 65536]", true},
		{"HTTP 401 Unauthorized", false},
		{"timeout exceeded", false},
		{"some other error", false},
	}

	for _, p := range patterns {
		got := isIncompatibleProbeError(strings.ToLower(p.errMsg))
		assert.Equal(t, p.match, got, "isIncompatibleProbeError(%q)", p.errMsg)
	}
}

func TestTruncateProbeString(t *testing.T) {
	assert.Equal(t, truncateProbeString("short", 200), "short")
	assert.Equal(t, truncateProbeString("abcdefghijklmnopqrstuvwxyz", 10), "abcdefghij...")
	assert.Equal(t, truncateProbeString("exact", 5), "exact")
}

func TestCloneProbeRequest(t *testing.T) {
	src := map[string]interface{}{
		"model":      "gpt-4o",
		"max_tokens": 10,
		"stream":     false,
	}
	dst := cloneProbeRequest(src)

	assert.Equal(t, src["model"], dst["model"])
	assert.Equal(t, src["max_tokens"], dst["max_tokens"])

	dst["model"] = "claude-3"
	assert.Equal(t, "gpt-4o", src["model"])
	assert.Equal(t, "claude-3", dst["model"])
}

func TestProbeModelForward_EndpointConstruction(t *testing.T) {
	endpoints := []struct {
		input    string
		expected string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
	}

	for _, e := range endpoints {
		result := strings.TrimRight(e.input, "/") + "/chat/completions"
		assert.Equal(t, e.expected, result)
	}
}