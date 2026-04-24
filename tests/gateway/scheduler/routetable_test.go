package scheduler_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intP(n int) *int { return &n }

func TestRouteTable_Get(t *testing.T) {
	tests := []struct {
		description string
		name        string
		groupName   string
		wantNil     bool
	}{
		{
			description: "Get存在的组名返回配置",
			name:        "组存在",
			groupName:   "group-a",
			wantNil:     false,
		},
		{
			description: "Get不存在的组名返回nil",
			name:        "组不存在",
			groupName:   "non-existent",
			wantNil:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := scheduler.NewRouteTable()
			rt.ReplaceAll(&scheduler.ControlPlaneConfig{
				Groups: []scheduler.GroupConfig{
					{ID: 1, Name: "group-a", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
				},
				Models: []scheduler.GroupModel{
					{ID: 1, GroupID: 1, ModelID: 10, ModelName: "m1", ChannelType: "openai", Endpoint: "http://m1"},
				},
			})

			result := rt.Get(tc.groupName)
			if tc.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, "group-a", result.GroupName)
			}
		})
	}
}

func TestRouteTable_ReplaceAll(t *testing.T) {
	tests := []struct {
		description string
		name        string
		config      *scheduler.ControlPlaneConfig
		wantGroups  []string
		wantNil     []string
	}{
		{
			description: "ReplaceAll替换整个表",
			name:        "正常替换",
			config: &scheduler.ControlPlaneConfig{
				Groups: []scheduler.GroupConfig{
					{ID: 1, Name: "new-group", Strategy: scheduler.StrategyPriority, Enabled: true},
				},
				Models: []scheduler.GroupModel{
					{ID: 1, GroupID: 1, ModelID: 20, ModelName: "new-model", ChannelType: "openai", Endpoint: "http://new"},
				},
			},
			wantGroups: []string{"new-group"},
			wantNil:    []string{"old-group"},
		},
		{
			description: "ReplaceAll nil清空表",
			name:        "nil清空",
			config:      nil,
			wantGroups:  []string{},
			wantNil:     []string{"group-a"},
		},
		{
			description: "ReplaceAll过滤Enabled=false的组",
			name:        "过滤禁用组",
			config: &scheduler.ControlPlaneConfig{
				Groups: []scheduler.GroupConfig{
					{ID: 1, Name: "enabled-group", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
					{ID: 2, Name: "disabled-group", Strategy: scheduler.StrategyRoundRobin, Enabled: false},
				},
				Models: []scheduler.GroupModel{
					{ID: 1, GroupID: 1, ModelID: 10, ModelName: "m1", ChannelType: "openai", Endpoint: "http://m1"},
					{ID: 2, GroupID: 2, ModelID: 20, ModelName: "m2", ChannelType: "openai", Endpoint: "http://m2"},
				},
			},
			wantGroups: []string{"enabled-group"},
			wantNil:    []string{"disabled-group"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := scheduler.NewRouteTable()
			rt.ReplaceAll(&scheduler.ControlPlaneConfig{
				Groups: []scheduler.GroupConfig{
					{ID: 99, Name: "old-group", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
				},
				Models: []scheduler.GroupModel{
					{ID: 1, GroupID: 99, ModelID: 1, ModelName: "old", ChannelType: "openai", Endpoint: "http://old"},
				},
			})

			rt.ReplaceAll(tc.config)

			for _, g := range tc.wantGroups {
				assert.NotNil(t, rt.Get(g), "expected group %s to exist", g)
			}
			for _, g := range tc.wantNil {
				assert.Nil(t, rt.Get(g), "expected group %s to be nil", g)
			}
		})
	}
}

func TestRouteTable_ReplaceAll_PriorityWeight(t *testing.T) {
	t.Run("Priority和Weight指针正确解引用", func(t *testing.T) {
		rt := scheduler.NewRouteTable()
		rt.ReplaceAll(&scheduler.ControlPlaneConfig{
			Groups: []scheduler.GroupConfig{
				{ID: 1, Name: "pw-group", Strategy: scheduler.StrategyPriority, Enabled: true},
			},
			Models: []scheduler.GroupModel{
				{ID: 1, GroupID: 1, ModelID: 10, ModelName: "with-ptr", ChannelType: "openai", Endpoint: "http://ptr", Priority: intP(5), Weight: intP(8)},
				{ID: 2, GroupID: 1, ModelID: 11, ModelName: "nil-ptr", ChannelType: "openai", Endpoint: "http://nil"},
			},
		})

		group := rt.Get("pw-group")
		require.NotNil(t, group)
		require.Len(t, group.Models, 2)

		var withPtr, nilPtr *scheduler.ModelRoutingEntry
		for _, m := range group.Models {
			if m.ModelName == "with-ptr" {
				withPtr = m
			} else {
				nilPtr = m
			}
		}

		require.NotNil(t, withPtr)
		require.NotNil(t, nilPtr)
		assert.Equal(t, 5, withPtr.Priority)
		assert.Equal(t, 8, withPtr.Weight)
		assert.Equal(t, 0, nilPtr.Priority)
		assert.Equal(t, 0, nilPtr.Weight)
	})
}

func TestRouteTable_GetAllModels(t *testing.T) {
	tests := []struct {
		description string
		name        string
		config      *scheduler.ControlPlaneConfig
		wantCount   int
	}{
		{
			description: "GetAllModels收集所有组的模型",
			name:        "多组模型汇总",
			config: &scheduler.ControlPlaneConfig{
				Groups: []scheduler.GroupConfig{
					{ID: 1, Name: "g1", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
					{ID: 2, Name: "g2", Strategy: scheduler.StrategyPriority, Enabled: true},
				},
				Models: []scheduler.GroupModel{
					{ID: 1, GroupID: 1, ModelID: 10, ModelName: "m1", ChannelType: "openai", Endpoint: "http://m1"},
					{ID: 2, GroupID: 1, ModelID: 11, ModelName: "m2", ChannelType: "openai", Endpoint: "http://m2"},
					{ID: 3, GroupID: 2, ModelID: 20, ModelName: "m3", ChannelType: "openai", Endpoint: "http://m3"},
				},
			},
			wantCount: 3,
		},
		{
			description: "空路由表返回空列表",
			name:        "空表",
			config:      nil,
			wantCount:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := scheduler.NewRouteTable()
			rt.ReplaceAll(tc.config)

			models := rt.GetAllModels()
			assert.Len(t, models, tc.wantCount)
		})
	}
}

func TestRouteTable_ReplaceAll_PlannerFields(t *testing.T) {
	rt := scheduler.NewRouteTable()
	rt.ReplaceAll(&scheduler.ControlPlaneConfig{
		Groups: []scheduler.GroupConfig{
			{ID: 1, Name: "planner-group", Strategy: scheduler.StrategyPriority, Enabled: true},
		},
		Models: []scheduler.GroupModel{
			{
				ID:                     1,
				GroupID:                1,
				ModelID:                10,
				ModelName:              "deployment-001",
				ChannelType:            "azure",
				Endpoint:               "https://example.openai.azure.com",
				UpstreamModelID:        "deployment-001",
				BaseModel:              "gpt-5",
				ProtocolMode:           "openai_responses",
				ParamProfile:           "openai_responses",
				UnsupportedParamPolicy: "drop",
				SupportedParamsOverride: map[string]any{
					"reasoning": true,
				},
				TransformOverrides: map[string]any{
					"drop": []any{"temperature"},
				},
			},
		},
	})

	group := rt.Get("planner-group")
	require.NotNil(t, group)
	require.Len(t, group.Models, 1)
	entry := group.Models[0]
	assert.Equal(t, "gpt-5", entry.BaseModel)
	assert.Equal(t, "openai_responses", entry.ProtocolMode)
	assert.Equal(t, "openai_responses", entry.ParamProfile)
	assert.Equal(t, "drop", entry.UnsupportedParamPolicy)
	assert.Equal(t, true, entry.SupportedParamsOverride["reasoning"])
	assert.NotEmpty(t, entry.TransformOverrides)
}
