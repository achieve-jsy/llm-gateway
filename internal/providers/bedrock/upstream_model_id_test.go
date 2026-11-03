package bedrock

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// TestUpstreamModelIDMapping 验证 upstream_model_id 映射功能
func TestUpstreamModelIDMapping(t *testing.T) {
	tests := []struct {
		name             string
		modelUpstreamIDs map[string]string
		requestModel     string
		expectedModelID  string
	}{
		{
			name: "使用 upstream_model_id",
			modelUpstreamIDs: map[string]string{
				"global.anthropic.claude-sonnet-4-6": "us.anthropic.claude-sonnet-4-6-20250514-v1:0",
			},
			requestModel:    "global.anthropic.claude-sonnet-4-6",
			expectedModelID: "us.anthropic.claude-sonnet-4-6-20250514-v1:0",
		},
		{
			name:             "无映射时使用原始名称",
			modelUpstreamIDs: map[string]string{},
			requestModel:     "claude-sonnet-4-6",
			expectedModelID:  "claude-sonnet-4-6",
		},
		{
			name: "未配置的模型使用原始名称",
			modelUpstreamIDs: map[string]string{
				"model-a": "upstream-a",
			},
			requestModel:    "model-b",
			expectedModelID: "model-b",
		},
		{
			name: "upstream_model_id 为空时使用原始名称",
			modelUpstreamIDs: map[string]string{
				"model-a": "",
			},
			requestModel:    "model-a",
			expectedModelID: "model-a",
		},
		{
			name: "带 bedrock/ 前缀的模型剥离前缀后查找",
			modelUpstreamIDs: map[string]string{
				"bedrock/global.anthropic.claude-sonnet-4-6": "us.anthropic.claude-sonnet-4-6-20250514-v1:0",
			},
			requestModel:    "bedrock/global.anthropic.claude-sonnet-4-6",
			expectedModelID: "us.anthropic.claude-sonnet-4-6-20250514-v1:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建 Provider 实例
			p := &Provider{
				modelUpstreamIDs: tt.modelUpstreamIDs,
			}

			// 创建请求
			req := &core.ChatRequest{
				Model: tt.requestModel,
				Messages: []core.Message{
					{Role: "user", Content: "test"},
				},
			}

			// 调用 buildConverseParts
			parts, err := p.buildConverseParts(req)
			if err != nil {
				t.Fatalf("buildConverseParts failed: %v", err)
			}

			// 验证 modelID
			if parts.modelID == nil {
				t.Fatal("modelID is nil")
			}
			actualModelID := *parts.modelID
			if actualModelID != tt.expectedModelID {
				t.Errorf("Expected modelID %q, got %q", tt.expectedModelID, actualModelID)
			}
		})
	}
}

// TestProviderConfigTransmitsUpstreamIDs 验证 ProviderConfig 正确传递 ModelUpstreamIDs
func TestProviderConfigTransmitsUpstreamIDs(t *testing.T) {
	cfg := providers.ProviderConfig{
		Type:   "bedrock",
		APIKey: "",
		ModelUpstreamIDs: map[string]string{
			"model-a": "upstream-a",
		},
	}

	p := New(cfg, providers.ProviderOptions{})
	bedrockProvider, ok := p.(*Provider)
	if !ok {
		t.Fatal("New() did not return a *Provider")
	}

	if bedrockProvider.modelUpstreamIDs == nil {
		t.Fatal("modelUpstreamIDs not set")
	}

	if bedrockProvider.modelUpstreamIDs["model-a"] != "upstream-a" {
		t.Errorf("Expected upstream-a, got %v", bedrockProvider.modelUpstreamIDs["model-a"])
	}
}
