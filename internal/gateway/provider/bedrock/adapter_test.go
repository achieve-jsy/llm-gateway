package bedrock

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Name ─────────────────────────────────────────────────────────────────

func TestBedrock_Name(t *testing.T) {
	a := New(Config{AccessKeyID: "AKID", SecretAccessKey: "secret"})
	assert.Equal(t, "bedrock", a.Name())
}

// ─── New / defaults ────────────────────────────────────────────────────────

func TestBedrock_New_SetsDefaultRegion(t *testing.T) {
	a := New(Config{AccessKeyID: "AKID", SecretAccessKey: "secret"}).(*Adapter)
	assert.Equal(t, defaultRegion, a.config.Region)
}

func TestBedrock_New_PreservesCustomRegion(t *testing.T) {
	a := New(Config{AccessKeyID: "AKID", SecretAccessKey: "secret", Region: "eu-west-1"}).(*Adapter)
	assert.Equal(t, "eu-west-1", a.config.Region)
}

// ─── ListModels ────────────────────────────────────────────────────────────

func TestBedrock_ListModels_ReturnsConfiguredModels(t *testing.T) {
	a := New(Config{
		AccessKeyID:     "AKID",
		SecretAccessKey: "secret",
		Models:          []string{"anthropic.claude-3-opus-20240229-v1:0", "anthropic.claude-3-sonnet-20240229-v1:0"},
	})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "anthropic.claude-3-opus-20240229-v1:0", models[0].ID)
	assert.Equal(t, "bedrock", models[0].OwnedBy)
	assert.Equal(t, "model", models[0].Object)
}

func TestBedrock_ListModels_Empty(t *testing.T) {
	a := New(Config{AccessKeyID: "AKID", SecretAccessKey: "secret"})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ─── convertToBedrockRequest ───────────────────────────────────────────────

func TestBedrock_ConvertToBedrockRequest_BasicMapping(t *testing.T) {
	maxTok := 512
	req := &model.ChatCompletionRequest{
		Model: "anthropic.claude-3-opus",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: &maxTok,
	}
	br := convertToBedrockRequest(req)

	assert.Equal(t, anthropicVersion, br.AnthropicVersion)
	assert.Equal(t, 512, br.MaxTokens)
	require.Len(t, br.Messages, 1)
	assert.Equal(t, "user", br.Messages[0].Role)
	assert.Equal(t, "Hello", br.Messages[0].Content)
}

func TestBedrock_ConvertToBedrockRequest_ExtractsSystemMessage(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "anthropic.claude-3",
		Messages: []model.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	}
	br := convertToBedrockRequest(req)
	assert.Equal(t, "You are helpful.", br.System)
	require.Len(t, br.Messages, 1)
	assert.Equal(t, "user", br.Messages[0].Role)
}

func TestBedrock_ConvertToBedrockRequest_DefaultMaxTokens(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "anthropic.claude-3",
		Messages: []model.Message{{Role: "user", Content: "x"}},
	}
	br := convertToBedrockRequest(req)
	assert.Equal(t, defaultMaxTokens, br.MaxTokens)
}

func TestBedrock_ConvertToBedrockRequest_MergesAdjacentSameRole(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "anthropic.claude-3",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
			{Role: "user", Content: "How are you?"},
		},
	}
	br := convertToBedrockRequest(req)
	require.Len(t, br.Messages, 1)
	assert.Contains(t, br.Messages[0].Content, "Hello")
	assert.Contains(t, br.Messages[0].Content, "How are you?")
}

func TestBedrock_ConvertToBedrockRequest_PrependsUserMessage_WhenFirstIsAssistant(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "anthropic.claude-3",
		Messages: []model.Message{
			{Role: "assistant", Content: "I am here"},
		},
	}
	br := convertToBedrockRequest(req)
	require.GreaterOrEqual(t, len(br.Messages), 1)
	assert.Equal(t, "user", br.Messages[0].Role)
}

func TestBedrock_ConvertToBedrockRequest_StopSequenceString(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "anthropic.claude-3",
		Messages: []model.Message{{Role: "user", Content: "x"}},
		Stop:     "STOP",
	}
	br := convertToBedrockRequest(req)
	assert.Equal(t, []string{"STOP"}, br.StopSequences)
}

// ─── convertFromBedrockResponse ───────────────────────────────────────────

func TestBedrock_ConvertFromBedrockResponse_TextBlock(t *testing.T) {
	stopReason := "end_turn"
	resp := &bedrockResponse{
		ID:         "msg-001",
		Type:       "message",
		Role:       "assistant",
		Content:    []bedrockBlock{{Type: "text", Text: "Hello from Bedrock"}},
		Model:      "anthropic.claude-3-opus",
		StopReason: &stopReason,
		Usage:      &bedrockUsage{InputTokens: 10, OutputTokens: 6},
	}
	result := convertFromBedrockResponse(resp, "anthropic.claude-3-opus")

	assert.Equal(t, "chat.completion", result.Object)
	assert.Contains(t, result.ID, "chatcmpl-")
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "Hello from Bedrock", result.Choices[0].Message.Content)
	assert.Equal(t, "assistant", result.Choices[0].Message.Role)
	assert.NotNil(t, result.Choices[0].FinishReason)
	assert.Equal(t, "stop", *result.Choices[0].FinishReason) // end_turn → stop
	require.NotNil(t, result.Usage)
	assert.Equal(t, 10, result.Usage.PromptTokens)
	assert.Equal(t, 6, result.Usage.CompletionTokens)
	assert.Equal(t, 16, result.Usage.TotalTokens)
}

func TestBedrock_ConvertFromBedrockResponse_ThinkingBlock(t *testing.T) {
	thinking := "let me reason..."
	resp := &bedrockResponse{
		ID:   "msg-002",
		Type: "message",
		Content: []bedrockBlock{
			{Type: "thinking", Thinking: &thinking},
			{Type: "text", Text: "Answer"},
		},
	}
	result := convertFromBedrockResponse(resp, "anthropic.claude-3")
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "Answer", result.Choices[0].Message.Content)
	assert.Equal(t, "let me reason...", result.Choices[0].Message.ReasoningContent)
}

func TestBedrock_ConvertFromBedrockResponse_NoUsage(t *testing.T) {
	stopReason := "max_tokens"
	resp := &bedrockResponse{
		ID:         "msg-003",
		StopReason: &stopReason,
		Content:    []bedrockBlock{{Type: "text", Text: "x"}},
	}
	result := convertFromBedrockResponse(resp, "model")
	assert.NotNil(t, result.Choices[0].FinishReason)
	assert.Equal(t, "length", *result.Choices[0].FinishReason) // max_tokens → length
	assert.Nil(t, result.Usage)
}

// ─── stopReasonToOpenAI ────────────────────────────────────────────────────

func TestBedrock_StopReasonToOpenAI(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"other_reason", "other_reason"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.out, stopReasonToOpenAI(c.in))
		})
	}
}

// ─── ToBedrockError ────────────────────────────────────────────────────────

func TestBedrock_ToBedrockError_ParsesStructuredError(t *testing.T) {
	body := []byte(`{"__type":"ValidationException","message":"Invalid model ID"}`)
	err := ToBedrockError(400, body)
	require.Error(t, err)

	be, ok := err.(*BedrockError)
	require.True(t, ok)
	assert.Equal(t, 400, be.HTTPStatus)
	assert.Equal(t, "ValidationException", be.Code)
	assert.Equal(t, "Invalid model ID", be.Message)
	assert.Contains(t, be.Error(), "400")
}

func TestBedrock_ToBedrockError_FallsBackToRawBody(t *testing.T) {
	body := []byte(`raw error text`)
	err := ToBedrockError(500, body)
	be := err.(*BedrockError)
	assert.Equal(t, 500, be.HTTPStatus)
	assert.Equal(t, "raw error text", be.Message)
}

func TestBedrock_ToBedrockError_401Unauthorized(t *testing.T) {
	body := []byte(`{"__type":"AccessDeniedException","message":"Access denied"}`)
	err := ToBedrockError(401, body)
	be := err.(*BedrockError)
	assert.Equal(t, 401, be.HTTPStatus)
	assert.Equal(t, "AccessDeniedException", be.Code)
}

func TestBedrock_ToBedrockError_429RateLimit(t *testing.T) {
	body := []byte(`{"__type":"ThrottlingException","message":"Too many requests"}`)
	err := ToBedrockError(429, body)
	be := err.(*BedrockError)
	assert.Equal(t, 429, be.HTTPStatus)
	assert.Equal(t, "ThrottlingException", be.Code)
}

// ─── Chat / ChatStream – AWS SDK calls (negative test only) ───────────────
//
// Chat and ChatStream use the AWS SDK which requires real AWS credentials.
// With fake credentials the SDK will fail during signing / connection.
// We just verify the error is returned and not panicked.

func TestBedrock_Chat_FailsWithFakeCredentials(t *testing.T) {
	a := New(Config{
		AccessKeyID:     "FAKEAKID",
		SecretAccessKey: "FAKESECRET",
		Region:          "us-east-1",
		Models:          []string{"anthropic.claude-3-opus-20240229-v1:0"},
	})

	ctx := context.Background()
	maxTok := 10
	req := &model.ChatCompletionRequest{
		Model:     "anthropic.claude-3-opus-20240229-v1:0",
		Messages:  []model.Message{{Role: "user", Content: "test"}},
		MaxTokens: &maxTok,
	}

	// Should return an error (auth failure, network error, etc.) — not panic.
	_, err := a.Chat(ctx, req)
	assert.Error(t, err)
}
