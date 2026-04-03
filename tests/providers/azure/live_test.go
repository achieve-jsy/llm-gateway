//go:build live

package azure_test

import (
	"context"
	"os"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/azure"
	"github.com/stretchr/testify/require"
)

// 用法：
//   AZURE_API_KEY=... AZURE_ENDPOINT=https://examplewestus.openai.azure.com \
//     go test ./tests/providers/azure/ -tags=live -count=1 -v -run Live

func TestLiveAzureV1_ChatAndResponses(t *testing.T) {
	key := os.Getenv("AZURE_API_KEY")
	endpoint := os.Getenv("AZURE_ENDPOINT")
	if key == "" || endpoint == "" {
		t.Skip("set AZURE_API_KEY and AZURE_ENDPOINT")
	}

	p := azure.New(providers.ProviderConfig{
		APIKey:  key,
		BaseURL: endpoint,
	}, providers.ProviderOptions{})

	maxTok := 16
	chat, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:     "gpt-5.5",
		Messages:  []core.Message{{Role: "user", Content: "ping"}},
		MaxTokens: &maxTok,
	})
	require.NoError(t, err)
	require.NotEmpty(t, chat.Choices)

	resp, err := p.Responses(context.Background(), &core.ResponsesRequest{
		Model: "gpt-5.5",
		Input: "ping",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	probe := service.ProbeAzureConnectivity(context.Background(), endpoint, key)
	require.True(t, probe.Success, probe.Message)

	chatProbe := service.ProbeAzureChat(context.Background(), endpoint, key, "gpt-5.5")
	require.True(t, chatProbe.Success, chatProbe.Message)
}
