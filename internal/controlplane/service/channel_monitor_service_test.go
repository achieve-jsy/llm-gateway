package service

import (
	"encoding/json"
	"testing"
)

// ── Challenge tests ────────────────────────────────────────────────────────────

func TestGenerateChallenge(t *testing.T) {
	for i := 0; i < 100; i++ {
		c := generateChallenge()
		if c.Prompt == "" {
			t.Fatal("prompt should not be empty")
		}
		if c.Expected == "" {
			t.Fatal("expected should not be empty")
		}
	}
}

func TestValidateChallenge(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected string
		want     bool
	}{
		{"exact match", "8", "8", true},
		{"contains number", "The answer is 42.", "42", true},
		{"partial match", "42 is the answer", "42", true},
		{"negative number", "-5", "-5", true},
		{"no match", "hello", "42", false},
		{"empty response", "", "8", false},
		{"empty expected", "8", "", false},
		{"wrong number", "99", "42", false},
		{"multiple numbers first match", "42 99", "42", true},
		{"multiple numbers second match", "10 42", "42", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateChallenge(tt.response, tt.expected)
			if got != tt.want {
				t.Errorf("validateChallenge(%q, %q) = %v, want %v", tt.response, tt.expected, got, tt.want)
			}
		})
	}
}

// ── Body building tests ────────────────────────────────────────────────────────

func TestBuildOpenAIBodyChatCompletions(t *testing.T) {
	challenge := monitorChallenge{Prompt: "What is 3+5?", Expected: "8"}
	body := buildOpenAIBody(MonitorAPIModeChatCompletions, "gpt-4", challenge)

	if body["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", body["model"])
	}
	if body["temperature"] != 0 {
		t.Errorf("temperature = %v, want 0", body["temperature"])
	}
	messages, ok := body["messages"].([]map[string]string)
	if !ok || len(messages) != 1 {
		t.Fatal("messages should be a slice with 1 element")
	}
	if messages[0]["role"] != "user" {
		t.Errorf("role = %v, want user", messages[0]["role"])
	}
	if messages[0]["content"] != challenge.Prompt {
		t.Errorf("content = %v, want %v", messages[0]["content"], challenge.Prompt)
	}
}

func TestBuildOpenAIBodyResponses(t *testing.T) {
	challenge := monitorChallenge{Prompt: "Calculate 12-7", Expected: "5"}
	body := buildOpenAIBody(MonitorAPIModeResponses, "gpt-4o", challenge)

	if body["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", body["model"])
	}
	if body["input"] != challenge.Prompt {
		t.Errorf("input = %v, want %v", body["input"], challenge.Prompt)
	}
}

func TestBuildOpenAIBodyChatCompletions_KimiK3(t *testing.T) {
	challenge := monitorChallenge{Prompt: "What is 3+5?", Expected: "8"}
	body := buildOpenAIBody(MonitorAPIModeChatCompletions, "kimi-k3", challenge)

	if body["temperature"] != 0.6 {
		t.Errorf("temperature = %v, want 0.6 for kimi-k3", body["temperature"])
	}
}

func TestBuildAnthropicBody(t *testing.T) {
	challenge := monitorChallenge{Prompt: "Count to 5", Expected: "5"}
	body := buildAnthropicBody("claude-3", challenge)

	if body["model"] != "claude-3" {
		t.Errorf("model = %v, want claude-3", body["model"])
	}
	messages, ok := body["messages"].([]map[string]string)
	if !ok || len(messages) != 1 {
		t.Fatal("messages should be a slice with 1 element")
	}
	if messages[0]["role"] != "user" {
		t.Errorf("role = %v, want user", messages[0]["role"])
	}
}

func TestBuildGeminiBody(t *testing.T) {
	challenge := monitorChallenge{Prompt: "What is 2+2?", Expected: "4"}
	body := buildGeminiBody("gemini-pro", challenge)

	contents, ok := body["contents"].([]map[string]interface{})
	if !ok || len(contents) != 1 {
		t.Fatal("contents should be a slice with 1 element")
	}
	parts, ok := contents[0]["parts"].([]map[string]string)
	if !ok || len(parts) != 1 {
		t.Fatal("parts should be a slice with 1 element")
	}
	if parts[0]["text"] != challenge.Prompt {
		t.Errorf("text = %v, want %v", parts[0]["text"], challenge.Prompt)
	}
}

// ── API path tests ─────────────────────────────────────────────────────────────

func TestBuildAPIPath(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		apiMode  string
		model    string
		want     string
	}{
		{"openai chat", MonitorProviderOpenAI, MonitorAPIModeChatCompletions, "gpt-4", "/v1/chat/completions"},
		{"openai responses", MonitorProviderOpenAI, MonitorAPIModeResponses, "gpt-4o", "/v1/responses"},
		{"anthropic", MonitorProviderAnthropic, MonitorAPIModeChatCompletions, "claude-3", "/v1/messages"},
		{"gemini", MonitorProviderGemini, MonitorAPIModeChatCompletions, "gemini-pro", "/v1beta/models/gemini-pro:generateContent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := &ChannelMonitorView{
				Provider:     tt.provider,
				APIMode:      tt.apiMode,
				PrimaryModel: tt.model,
			}
			got := buildAPIPath(view)
			if got != tt.want {
				t.Errorf("buildAPIPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── Response text extraction tests ─────────────────────────────────────────────

func TestExtractResponseTextOpenAI(t *testing.T) {
	resp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"content": "42",
				},
			},
		},
	}
	body, _ := json.Marshal(resp)
	text := extractResponseText(MonitorProviderOpenAI, MonitorAPIModeChatCompletions, body)
	if text != "42" {
		t.Errorf("extractResponseText() = %q, want %q", text, "42")
	}
}

func TestExtractResponseTextAnthropic(t *testing.T) {
	resp := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "8",
			},
		},
	}
	body, _ := json.Marshal(resp)
	text := extractResponseText(MonitorProviderAnthropic, MonitorAPIModeChatCompletions, body)
	if text != "8" {
		t.Errorf("extractResponseText() = %q, want %q", text, "8")
	}
}

func TestExtractResponseTextGemini(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"text": "15",
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(resp)
	text := extractResponseText(MonitorProviderGemini, MonitorAPIModeChatCompletions, body)
	if text != "15" {
		t.Errorf("extractResponseText() = %q, want %q", text, "15")
	}
}

func TestExtractResponseTextInvalid(t *testing.T) {
	body := []byte("not valid json")
	text := extractResponseText(MonitorProviderOpenAI, MonitorAPIModeChatCompletions, body)
	if text != "not valid json" {
		t.Errorf("extractResponseText() = %q, want raw body", text)
	}
}

// ── Validate tests ─────────────────────────────────────────────────────────────

func TestValidateProvider(t *testing.T) {
	tests := []struct {
		provider string
		wantErr  bool
	}{
		{"openai", false},
		{"anthropic", false},
		{"gemini", false},
		{"azure", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			err := validateProvider(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateProvider(%q) error = %v, wantErr = %v", tt.provider, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAPIMode(t *testing.T) {
	tests := []struct {
		provider string
		apiMode  string
		wantErr  bool
	}{
		{MonitorProviderOpenAI, MonitorAPIModeChatCompletions, false},
		{MonitorProviderOpenAI, MonitorAPIModeResponses, false},
		{MonitorProviderAnthropic, MonitorAPIModeChatCompletions, false},
		{MonitorProviderAnthropic, MonitorAPIModeResponses, true},
		{MonitorProviderGemini, MonitorAPIModeChatCompletions, false},
		{MonitorProviderGemini, MonitorAPIModeResponses, true},
		{"", "", false},
		{"", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"_"+tt.apiMode, func(t *testing.T) {
			err := validateAPIMode(tt.provider, tt.apiMode)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPIMode(%q, %q) error = %v, wantErr = %v", tt.provider, tt.apiMode, err, tt.wantErr)
			}
		})
	}
}

func TestValidateInterval(t *testing.T) {
	tests := []struct {
		sec     int
		wantErr bool
	}{
		{15, false},
		{60, false},
		{3600, false},
		{10, true},
		{3601, true},
		{0, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			err := validateInterval(tt.sec)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInterval(%d) error = %v, wantErr = %v", tt.sec, err, tt.wantErr)
			}
		})
	}
}

// ── Normalize helpers ──────────────────────────────────────────────────────────

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://api.example.com/", "https://api.example.com"},
		{"https://api.example.com", "https://api.example.com"},
		{"  https://api.example.com/  ", "https://api.example.com"},
	}

	for _, tt := range tests {
		got := normalizeEndpoint(tt.input)
		if got != tt.want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeModels(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{nil, []string{}},
		{[]string{}, []string{}},
		{[]string{"gpt-4", "gpt-3.5"}, []string{"gpt-4", "gpt-3.5"}},
		{[]string{"gpt-4", "gpt-4", "gpt-3.5"}, []string{"gpt-4", "gpt-3.5"}},
		{[]string{"gpt-4", "", "gpt-3.5"}, []string{"gpt-4", "gpt-3.5"}},
		{[]string{"  gpt-4  ", " gpt-3.5 "}, []string{"gpt-4", "gpt-3.5"}},
	}

	for _, tt := range tests {
		got := normalizeModels(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("normalizeModels(%v) len = %d, want %d", tt.input, len(got), len(tt.want))
			return
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("normalizeModels(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDefaultAPIMode(t *testing.T) {
	if got := defaultAPIMode(""); got != MonitorAPIModeChatCompletions {
		t.Errorf("defaultAPIMode(\"\") = %q, want %q", got, MonitorAPIModeChatCompletions)
	}
	if got := defaultAPIMode("  "); got != MonitorAPIModeChatCompletions {
		t.Errorf("defaultAPIMode(\"  \") = %q, want %q", got, MonitorAPIModeChatCompletions)
	}
	if got := defaultAPIMode("responses"); got != "responses" {
		t.Errorf("defaultAPIMode(\"responses\") = %q, want %q", got, "responses")
	}
}

func TestDefaultBodyMode(t *testing.T) {
	if got := defaultBodyMode(""); got != MonitorBodyOverrideModeOff {
		t.Errorf("defaultBodyMode(\"\") = %q, want %q", got, MonitorBodyOverrideModeOff)
	}
	if got := defaultBodyMode("merge"); got != "merge" {
		t.Errorf("defaultBodyMode(\"merge\") = %q, want %q", got, "merge")
	}
}

func TestTruncateMessage(t *testing.T) {
	short := "short message"
	if got := truncateMessage(short); got != short {
		t.Errorf("truncateMessage(%q) = %q, want %q", short, got, short)
	}

	long := make([]byte, monitorMessageMaxBytes+10)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateMessage(string(long))
	if len(got) != monitorMessageMaxBytes {
		t.Errorf("truncateMessage(len=%d) len = %d, want %d", len(long), len(got), monitorMessageMaxBytes)
	}
}

func TestTruncateMsg(t *testing.T) {
	if got := truncateMsg("hello", 3); got != "hel" {
		t.Errorf("truncateMsg(\"hello\", 3) = %q, want %q", got, "hel")
	}
	if got := truncateMsg("hi", 10); got != "hi" {
		t.Errorf("truncateMsg(\"hi\", 10) = %q, want %q", got, "hi")
	}
}

// ── All models helper ──────────────────────────────────────────────────────────

func TestAllModels(t *testing.T) {
	view := &ChannelMonitorView{
		PrimaryModel: "gpt-4",
		ExtraModels:  []string{"gpt-3.5", "gpt-4"},
	}
	models := allModels(view)
	if len(models) != 2 {
		t.Errorf("allModels() len = %d, want 2", len(models))
	}
	if models[0] != "gpt-4" {
		t.Errorf("allModels()[0] = %q, want gpt-4", models[0])
	}
	if models[1] != "gpt-3.5" {
		t.Errorf("allModels()[1] = %q, want gpt-3.5", models[1])
	}
}

// ── Body override mode tests ───────────────────────────────────────────────────

func TestBuildRequestBodyReplace(t *testing.T) {
	view := &ChannelMonitorView{
		Provider:         MonitorProviderOpenAI,
		APIMode:          MonitorAPIModeChatCompletions,
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride: map[string]interface{}{
			"custom_field": "custom_value",
			"temperature":  0.5,
		},
	}
	challenge := monitorChallenge{Prompt: "test", Expected: "0"}
	body := buildRequestBody(view, "gpt-4", challenge)

	if body["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", body["model"])
	}
	if body["custom_field"] != "custom_value" {
		t.Errorf("custom_field = %v, want custom_value", body["custom_field"])
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", body["temperature"])
	}
	// messages should NOT be present in replace mode
	if _, ok := body["messages"]; ok {
		t.Error("messages should not be present in replace mode")
	}
}

func TestBuildRequestBodyMerge(t *testing.T) {
	view := &ChannelMonitorView{
		Provider:         MonitorProviderOpenAI,
		APIMode:          MonitorAPIModeChatCompletions,
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]interface{}{
			"temperature": 0.5,
		},
	}
	challenge := monitorChallenge{Prompt: "test", Expected: "0"}
	body := buildRequestBody(view, "gpt-4", challenge)

	if body["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", body["model"])
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", body["temperature"])
	}
	// messages should be present in merge mode (from base)
	if _, ok := body["messages"]; !ok {
		t.Error("messages should be present in merge mode")
	}
}

// ── AppError tests ─────────────────────────────────────────────────────────────

func TestAppError(t *testing.T) {
	err := fmtAppError("TEST_CODE", "test message")
	if err.Error() != "test message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test message")
	}

	ae, ok := err.(*appError)
	if !ok {
		t.Fatal("err should be *appError")
	}
	if ae.Code() != "TEST_CODE" {
		t.Errorf("Code() = %q, want %q", ae.Code(), "TEST_CODE")
	}
}

// ── Build base body tests ──────────────────────────────────────────────────────

func TestBuildBaseBody(t *testing.T) {
	challenge := monitorChallenge{Prompt: "test", Expected: "0"}

	// Unknown provider should default to OpenAI
	body := buildBaseBody("unknown", MonitorAPIModeChatCompletions, "test-model", challenge)
	if _, ok := body["messages"]; !ok {
		t.Error("unknown provider should default to OpenAI body format")
	}
}