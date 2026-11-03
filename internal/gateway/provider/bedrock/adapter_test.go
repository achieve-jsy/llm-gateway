package bedrock

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
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
		AccessKeyID:    "AKID",
		SecretAccessKey: "secret",
		Models:         []string{"anthropic.claude-3-opus-20240229-v1:0", "amazon.nova-pro-v1:0"},
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

// ─── convertMessages ──────────────────────────────────────────────────────

func TestBedrock_ConvertMessages_BasicMapping(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "Hello"},
	}
	messages, system := convertMessages(msgs)

	assert.Empty(t, system)
	require.Len(t, messages, 1)
	assert.Equal(t, brtypes.ConversationRoleUser, messages[0].Role)
	require.Len(t, messages[0].Content, 1)
	textBlock, ok := messages[0].Content[0].(*brtypes.ContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "Hello", textBlock.Value)
}

func TestBedrock_ConvertMessages_ExtractsSystem(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi"},
	}
	messages, system := convertMessages(msgs)

	require.Len(t, system, 1)
	sysBlock, ok := system[0].(*brtypes.SystemContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "You are helpful.", sysBlock.Value)
	require.Len(t, messages, 1)
	assert.Equal(t, brtypes.ConversationRoleUser, messages[0].Role)
}

func TestBedrock_ConvertMessages_MergesAdjacentSameRole(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "How are you?"},
	}
	messages, _ := convertMessages(msgs)

	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 2)
}

func TestBedrock_ConvertMessages_PrependsUserWhenFirstIsAssistant(t *testing.T) {
	msgs := []model.Message{
		{Role: "assistant", Content: "I am here"},
	}
	messages, _ := convertMessages(msgs)

	require.GreaterOrEqual(t, len(messages), 2)
	assert.Equal(t, brtypes.ConversationRoleUser, messages[0].Role)
}

// ─── buildInferenceConfig ─────────────────────────────────────────────────

func TestBedrock_BuildInferenceConfig_DefaultMaxTokens(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "test-model",
		Messages: []model.Message{{Role: "user", Content: "x"}},
	}
	cfg := buildInferenceConfig(req)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.MaxTokens)
	assert.Equal(t, int32(defaultMaxTokens), *cfg.MaxTokens)
}

func TestBedrock_BuildInferenceConfig_CustomMaxTokens(t *testing.T) {
	maxTok := 512
	req := &model.ChatCompletionRequest{
		Model:     "test-model",
		Messages:  []model.Message{{Role: "user", Content: "x"}},
		MaxTokens: &maxTok,
	}
	cfg := buildInferenceConfig(req)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.MaxTokens)
	assert.Equal(t, int32(512), *cfg.MaxTokens)
}

func TestBedrock_BuildInferenceConfig_Temperature(t *testing.T) {
	temp := model.JSONFloat64(0.7)
	req := &model.ChatCompletionRequest{
		Model:       "test-model",
		Messages:    []model.Message{{Role: "user", Content: "x"}},
		Temperature: &temp,
	}
	cfg := buildInferenceConfig(req)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Temperature)
	assert.InDelta(t, float32(0.7), *cfg.Temperature, 0.001)
}

func TestBedrock_BuildInferenceConfig_StopSequence(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "test-model",
		Messages: []model.Message{{Role: "user", Content: "x"}},
		Stop:     "STOP",
	}
	cfg := buildInferenceConfig(req)
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"STOP"}, cfg.StopSequences)
}

// ─── convertConverseOutput ────────────────────────────────────────────────

func TestBedrock_ConvertConverseOutput_TextContent(t *testing.T) {
	output := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "Hello from Bedrock"},
				},
			},
		},
		StopReason: brtypes.StopReasonEndTurn,
		Usage: &brtypes.TokenUsage{
			InputTokens:  aws.Int32(10),
			OutputTokens: aws.Int32(6),
			TotalTokens:  aws.Int32(16),
		},
	}
	result := convertConverseOutput(output, "test-model")

	assert.Equal(t, "chat.completion", result.Object)
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "Hello from Bedrock", result.Choices[0].Message.Content)
	assert.Equal(t, "assistant", result.Choices[0].Message.Role)
	require.NotNil(t, result.Choices[0].FinishReason)
	assert.Equal(t, "stop", *result.Choices[0].FinishReason)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 10, result.Usage.PromptTokens)
	assert.Equal(t, 6, result.Usage.CompletionTokens)
	assert.Equal(t, 16, result.Usage.TotalTokens)
}

func TestBedrock_ConvertConverseOutput_NoUsage(t *testing.T) {
	output := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "x"},
				},
			},
		},
		StopReason: brtypes.StopReasonMaxTokens,
	}
	result := convertConverseOutput(output, "model")
	require.NotNil(t, result.Choices[0].FinishReason)
	assert.Equal(t, "length", *result.Choices[0].FinishReason)
	assert.Nil(t, result.Usage)
}

// ─── converseStopReasonToOpenAI ───────────────────────────────────────────

func TestBedrock_ConverseStopReasonToOpenAI(t *testing.T) {
	cases := []struct {
		in  brtypes.StopReason
		out string
	}{
		{brtypes.StopReasonEndTurn, "stop"},
		{brtypes.StopReasonStopSequence, "stop"},
		{brtypes.StopReasonMaxTokens, "length"},
		{brtypes.StopReasonToolUse, "tool_calls"},
		{brtypes.StopReason("other_reason"), "other_reason"},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			assert.Equal(t, c.out, converseStopReasonToOpenAI(c.in))
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

func TestBedrock_Chat_FailsWithFakeCredentials(t *testing.T) {
	a := New(Config{
		AccessKeyID:    "FAKEAKID",
		SecretAccessKey: "FAKESECRET",
		Region:         "us-east-1",
		Models:         []string{"anthropic.claude-3-opus-20240229-v1:0"},
	})

	ctx := context.Background()
	maxTok := 10
	req := &model.ChatCompletionRequest{
		Model:     "anthropic.claude-3-opus-20240229-v1:0",
		Messages:  []model.Message{{Role: "user", Content: "test"}},
		MaxTokens: &maxTok,
	}

	_, err := a.Chat(ctx, req)
	assert.Error(t, err)
}
