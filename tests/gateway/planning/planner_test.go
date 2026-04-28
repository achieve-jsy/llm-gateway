package planning_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/planning"
	"github.com/stretchr/testify/require"
)

func TestTargetResolverMergesRouteAndMetadata(t *testing.T) {
	resolver := planning.NewTargetResolver()
	target := resolver.Resolve(planning.RouteInput{
		Kind:         planning.RequestKindChat,
		PublicModel:  "public-gpt",
		LogicalModel: "group-gpt",
		Decision: planning.RouteDecision{
			ModelID:       "decision-model-id",
			ModelName:     "decision-model",
			UpstreamModel: "decision-upstream",
			ProviderName:  "decision-provider",
			ProviderType:  "openai",
			CredentialRef: "secret-ref-from-decision",
		},
		Metadata: planning.ModelMetadata{
			ModelID:       "metadata-model-id",
			BaseModel:     "gpt-5.5",
			UpstreamModel: "metadata-upstream",
			ProviderName:  "metadata-provider",
			ParamProfile:  planning.ParamProfileOpenAIReasoningChat,
		},
	})

	require.Equal(t, "public-gpt", target.PublicModel)
	require.Equal(t, "group-gpt", target.LogicalModel)
	require.Equal(t, "gpt-5.5", target.BaseModel)
	require.Equal(t, "metadata-upstream", target.UpstreamModel)
	require.Equal(t, "metadata-model-id", target.ModelID)
	require.Equal(t, "metadata-provider", target.ProviderName)
	require.Equal(t, "openai", target.ProviderType)
	require.Equal(t, planning.ProtocolAuto, target.ProtocolModeOverride)
	require.Equal(t, planning.ParamProfileOpenAIReasoningChat, target.ParamProfileOverride)
	require.Equal(t, planning.UnsupportedParamPolicyPreserve, target.UnsupportedParamPolicy)
}

func TestPlannerExplicitOverridesTakePriority(t *testing.T) {
	planner := planning.NewPlanner()
	target := planning.UpstreamTarget{
		PublicModel:          "public-model",
		LogicalModel:         "logical-model",
		BaseModel:            "gpt-4.1",
		UpstreamModel:        "upstream-model",
		ProviderType:         "openai",
		ProtocolModeOverride: planning.ProtocolOpenAIResponses,
		ParamProfileOverride: planning.ParamProfileOpenAIReasoningChat,
	}

	plan := planner.Plan(target, planning.RequestShape{Kind: planning.RequestKindChat})

	require.Equal(t, planning.ProtocolOpenAIResponses, plan.Protocol)
	require.Equal(t, planning.ParamProfileOpenAIReasoningChat, plan.ParamProfile)
	require.Equal(t, "explicit_protocol_override", plan.Audit.ProtocolReason)
	require.Equal(t, "explicit_param_profile_override", plan.Audit.ParamReason)
	require.Equal(t, "/responses", plan.Endpoint)
}

func TestPlannerBaseModelTakesPriorityOverUpstreamModel(t *testing.T) {
	planner := planning.NewPlanner()
	target := planning.UpstreamTarget{
		PublicModel:   "public-model",
		LogicalModel:  "logical-model",
		BaseModel:     "gpt-4.1",
		UpstreamModel: "gpt-5.5-dangerous-fallback-name",
		ProviderType:  "openai",
	}

	plan := planner.Plan(target, planning.RequestShape{
		Kind:               planning.RequestKindChat,
		HasTools:           true,
		HasReasoningEffort: true,
	})

	require.Equal(t, planning.ProtocolOpenAIChat, plan.Protocol)
	require.Equal(t, planning.ParamProfileOpenAIStandard, plan.ParamProfile)
	require.Empty(t, plan.ParamRules, "base_model 是普通模型时，不应被 upstream_model 的 gpt-5 名称误导为 reasoning/responses")
	require.Equal(t, "gpt-4.1", plan.Audit.BaseModel)
	require.Equal(t, "gpt-5.5-dangerous-fallback-name", plan.Audit.UpstreamModel)
}

func TestPlannerAzureDeploymentNameDoesNotAffectGPT5Detection(t *testing.T) {
	planner := planning.NewPlanner()
	target := planning.UpstreamTarget{
		PublicModel:   "public-gpt5",
		LogicalModel:  "team-gpt5",
		BaseModel:     "gpt-5.5",
		UpstreamModel: "prod-westus-deployment-a",
		ProviderType:  "azure",
		APIVersion:    "2025-04-01-preview",
	}

	plan := planner.Plan(target, planning.RequestShape{
		Kind:               planning.RequestKindChat,
		HasTools:           true,
		HasReasoningEffort: true,
	})

	require.Equal(t, planning.ProtocolOpenAIResponses, plan.Protocol)
	require.Equal(t, planning.ParamProfileOpenAIResponses, plan.ParamProfile)
	require.Equal(t, "azure_responses", plan.Adapter)
	require.Equal(t, "gpt5_4_plus_tools_reasoning_effort", plan.Audit.ProtocolReason)
	require.Equal(t, "prod-westus-deployment-a", plan.ModelForUpstream)
}

func TestPlannerStandardOverrideDoesNotAddReasoningTransforms(t *testing.T) {
	planner := planning.NewPlanner()
	target := planning.UpstreamTarget{
		PublicModel:          "public-gpt5",
		BaseModel:            "gpt-5.5",
		UpstreamModel:        "gpt-5.5",
		ProviderType:         "openai",
		ProtocolModeOverride: planning.ProtocolOpenAIChat,
		ParamProfileOverride: planning.ParamProfileOpenAIStandard,
	}

	plan := planner.Plan(target, planning.RequestShape{
		Kind:               planning.RequestKindChat,
		HasReasoningEffort: true,
	})

	require.Equal(t, planning.ProtocolOpenAIChat, plan.Protocol)
	require.Equal(t, planning.ParamProfileOpenAIStandard, plan.ParamProfile)
	require.Empty(t, plan.ParamRules, "openai_standard 显式覆盖时不应生成 reasoning 参数转换规则")
}

func TestPlannerResponsesBridgeConditions(t *testing.T) {
	planner := planning.NewPlanner()

	t.Run("gpt5.4 plus tools and reasoning effort bridges", func(t *testing.T) {
		plan := planner.Plan(planning.UpstreamTarget{
			PublicModel:   "public-gpt5",
			BaseModel:     "gpt-5.4",
			UpstreamModel: "gpt-5.4",
			ProviderType:  "openai",
		}, planning.RequestShape{
			Kind:               planning.RequestKindChat,
			HasTools:           true,
			HasReasoningEffort: true,
		})

		require.Equal(t, planning.ProtocolOpenAIResponses, plan.Protocol)
		require.Equal(t, planning.ParamProfileOpenAIResponses, plan.ParamProfile)
		require.Equal(t, "/responses", plan.Endpoint)
		require.NotEmpty(t, plan.ParamRules)
	})

	t.Run("gpt5.4 plus tools without reasoning effort stays chat", func(t *testing.T) {
		plan := planner.Plan(planning.UpstreamTarget{
			PublicModel:   "public-gpt5",
			BaseModel:     "gpt-5.4",
			UpstreamModel: "gpt-5.4",
			ProviderType:  "openai",
		}, planning.RequestShape{
			Kind:     planning.RequestKindChat,
			HasTools: true,
		})

		require.Equal(t, planning.ProtocolOpenAIChat, plan.Protocol)
		require.Equal(t, planning.ParamProfileOpenAIReasoningChat, plan.ParamProfile)
	})
}

func TestPlanAuditIsSafeSummary(t *testing.T) {
	planner := planning.NewPlanner()
	plan := planner.Plan(planning.UpstreamTarget{
		PublicModel:   "public-gpt5",
		BaseModel:     "gpt-5.5",
		UpstreamModel: "deployment-a",
		ProviderType:  "azure",
		CredentialRef: "sk-should-not-appear",
		TransformOverrides: planning.TransformOverrides{
			Force: map[string]any{
				"Authorization": "Bearer secret-token",
				"api_key":       "secret-value",
			},
		},
	}, planning.RequestShape{
		Kind:               planning.RequestKindChat,
		HasTools:           true,
		HasReasoningEffort: true,
	})

	payload, err := json.Marshal(plan.SafeAudit())
	require.NoError(t, err)
	text := string(payload)
	require.NotContains(t, text, "sk-should-not-appear")
	require.NotContains(t, text, "secret-token")
	require.NotContains(t, text, "secret-value")
	require.Contains(t, text, "force_keys")
	require.True(t, strings.Contains(text, "Authorization") || strings.Contains(text, "api_key"))
}
