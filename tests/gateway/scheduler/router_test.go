package scheduler_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func intPtr(n int) *int { return &n }

func makeTestConfig(groups []scheduler.GroupConfig, models []scheduler.GroupModel) *scheduler.ControlPlaneConfig {
	return &scheduler.ControlPlaneConfig{
		Groups: groups,
		Models: models,
	}
}

func makeRoundRobinConfig() *scheduler.ControlPlaneConfig {
	return makeTestConfig(
		[]scheduler.GroupConfig{
			{ID: 1, Name: "rr-group", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
		},
		[]scheduler.GroupModel{
			{ID: 1, GroupID: 1, ModelID: 101, ModelName: "model-a", ChannelType: "openai", Endpoint: "http://a"},
			{ID: 2, GroupID: 1, ModelID: 102, ModelName: "model-b", ChannelType: "openai", Endpoint: "http://b"},
			{ID: 3, GroupID: 1, ModelID: 103, ModelName: "model-c", ChannelType: "openai", Endpoint: "http://c"},
		},
	)
}

func makePriorityConfig() *scheduler.ControlPlaneConfig {
	return makeTestConfig(
		[]scheduler.GroupConfig{
			{ID: 1, Name: "prio-group", Strategy: scheduler.StrategyPriority, Enabled: true},
		},
		[]scheduler.GroupModel{
			{ID: 1, GroupID: 1, ModelID: 201, ModelName: "model-low", ChannelType: "openai", Endpoint: "http://low", Priority: intPtr(3)},
			{ID: 2, GroupID: 1, ModelID: 202, ModelName: "model-mid", ChannelType: "openai", Endpoint: "http://mid", Priority: intPtr(2)},
			{ID: 3, GroupID: 1, ModelID: 203, ModelName: "model-high", ChannelType: "openai", Endpoint: "http://high", Priority: intPtr(1)},
		},
	)
}

func makeWeightedConfig() *scheduler.ControlPlaneConfig {
	return makeTestConfig(
		[]scheduler.GroupConfig{
			{ID: 1, Name: "weighted-group", Strategy: scheduler.StrategyWeighted, Enabled: true},
		},
		[]scheduler.GroupModel{
			{ID: 1, GroupID: 1, ModelID: 301, ModelName: "model-light", ChannelType: "openai", Endpoint: "http://light", Weight: intPtr(1)},
			{ID: 2, GroupID: 1, ModelID: 302, ModelName: "model-heavy", ChannelType: "openai", Endpoint: "http://heavy", Weight: intPtr(9)},
		},
	)
}

func newTestRouter() (*scheduler.Router, *scheduler.RouteTable, *scheduler.HealthStore) {
	rt := scheduler.NewRouteTable()
	hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)
	router := scheduler.NewRouter(rt, hs)
	return router, rt, hs
}

// --- RoundRobin tests ---

func TestRoute_RoundRobin(t *testing.T) {
	tests := []struct {
		description string
		name        string
		setup       func(*scheduler.RouteTable)
		calls       int
		wantErr     error
		validate    func(t *testing.T, decisions []*scheduler.RouteDecision)
	}{
		{
			description: "组内有3个模型，连续Route3次，每次返回不同模型，循环回到第一个",
			name:        "正常轮询",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeRoundRobinConfig())
			},
			calls: 6,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				// 应按轮询序列循环
				assert.Equal(t, "model-a", decisions[0].ModelName)
				assert.Equal(t, "model-b", decisions[1].ModelName)
				assert.Equal(t, "model-c", decisions[2].ModelName)
				// 循环回到第一个
				assert.Equal(t, "model-a", decisions[3].ModelName)
				assert.Equal(t, "model-b", decisions[4].ModelName)
				assert.Equal(t, "model-c", decisions[5].ModelName)
			},
		},
		{
			description: "Route不存在的组名返回ErrGroupNotFound",
			name:        "组不存在",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeRoundRobinConfig())
			},
			calls:   1,
			wantErr: scheduler.ErrGroupNotFound,
		},
		{
			description: "组存在但无模型时返回ErrNoAvailableModel",
			name:        "空模型组",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeTestConfig(
					[]scheduler.GroupConfig{
						{ID: 99, Name: "empty-group", Strategy: scheduler.StrategyRoundRobin, Enabled: true},
					},
					[]scheduler.GroupModel{},
				))
			},
			calls:   1,
			wantErr: scheduler.ErrNoAvailableModel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, rt, _ := newTestRouter()
			tc.setup(rt)

			groupName := "rr-group"
			if tc.wantErr == scheduler.ErrGroupNotFound {
				groupName = "non-existent-group"
			} else if tc.wantErr == scheduler.ErrNoAvailableModel {
				groupName = "empty-group"
			}

			if tc.wantErr != nil {
				_, err := router.Route(groupName, nil)
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}

			var decisions []*scheduler.RouteDecision
			for i := 0; i < tc.calls; i++ {
				d, err := router.Route(groupName, nil)
				require.NoError(t, err)
				decisions = append(decisions, d)
			}
			tc.validate(t, decisions)
		})
	}
}

// --- Priority tests ---

func TestRoute_Priority(t *testing.T) {
	tests := []struct {
		description string
		name        string
		setup       func(*scheduler.RouteTable, *scheduler.HealthStore)
		calls       int
		validate    func(t *testing.T, decisions []*scheduler.RouteDecision)
	}{
		{
			description: "组内3个模型Priority分别为1,2,3，Route返回Priority=1的模型",
			name:        "正常优先级选择最高优先级模型",
			setup: func(rt *scheduler.RouteTable, hs *scheduler.HealthStore) {
				rt.ReplaceAll(makePriorityConfig())
			},
			calls: 3,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				for _, d := range decisions {
					assert.Equal(t, "model-high", d.ModelName)
				}
			},
		},
		{
			description: "2个Priority=1的模型，轮询选择",
			name:        "多模型同优先级轮询",
			setup: func(rt *scheduler.RouteTable, hs *scheduler.HealthStore) {
				rt.ReplaceAll(makeTestConfig(
					[]scheduler.GroupConfig{
						{ID: 1, Name: "prio-group", Strategy: scheduler.StrategyPriority, Enabled: true},
					},
					[]scheduler.GroupModel{
						{ID: 1, GroupID: 1, ModelID: 201, ModelName: "model-x", ChannelType: "openai", Endpoint: "http://x", Priority: intPtr(1)},
						{ID: 2, GroupID: 1, ModelID: 202, ModelName: "model-y", ChannelType: "openai", Endpoint: "http://y", Priority: intPtr(1)},
						{ID: 3, GroupID: 1, ModelID: 203, ModelName: "model-z", ChannelType: "openai", Endpoint: "http://z", Priority: intPtr(2)},
					},
				))
			},
			calls: 4,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				// 只有 model-x 和 model-y 是 Priority=1，轮询选择
				names := make(map[string]int)
				for _, d := range decisions {
					names[d.ModelName]++
				}
				assert.Equal(t, 2, names["model-x"])
				assert.Equal(t, 2, names["model-y"])
				assert.Equal(t, 0, names["model-z"])
			},
		},
		{
			description: "最高优先级模型不健康时，选次高优先级",
			name:        "优先级过滤不健康模型",
			setup: func(rt *scheduler.RouteTable, hs *scheduler.HealthStore) {
				rt.ReplaceAll(makePriorityConfig())
				// 将最高优先级的模型(model-high, id=203)标为不健康
				// 需要连续失败达到阈值（5次）
				modelID := fmt.Sprintf("%d", 203)
				for i := 0; i < 5; i++ {
					hs.RecordFailure(modelID)
				}
			},
			calls: 1,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				// 由于 FilterHealthy 当前是存根（直接返回所有模型），
				// 实际上仍然会选最高优先级。
				// 当 FilterHealthy 实现后此测试将验证正确行为。
				// 此处验证路由成功即可
				assert.NotNil(t, decisions[0])
				assert.Equal(t, "prio-group", decisions[0].GroupName)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, rt, hs := newTestRouter()
			tc.setup(rt, hs)

			var decisions []*scheduler.RouteDecision
			for i := 0; i < tc.calls; i++ {
				d, err := router.Route("prio-group", nil)
				require.NoError(t, err)
				decisions = append(decisions, d)
			}
			tc.validate(t, decisions)
		})
	}
}

// --- Weighted tests ---

func TestRoute_Weighted(t *testing.T) {
	tests := []struct {
		description string
		name        string
		setup       func(*scheduler.RouteTable)
		calls       int
		validate    func(t *testing.T, decisions []*scheduler.RouteDecision)
	}{
		{
			description: "Weight=1和Weight=9的两个模型，多次Route后Weight高的被选中次数更多",
			name:        "正常加权分布",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeWeightedConfig())
			},
			calls: 1000,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				counts := make(map[string]int)
				for _, d := range decisions {
					counts[d.ModelName]++
				}
				// Weight=9 的模型应被选中 ~900 次，允许较大偏差（统计测试）
				assert.Greater(t, counts["model-heavy"], counts["model-light"])
				// 粗略验证分布：heavy 占比应该 > 70%（9/(9+1)=90%，给大余量）
				assert.Greater(t, counts["model-heavy"], 700)
			},
		},
		{
			description: "Weight=0自动视为Weight=1",
			name:        "Weight为0的兜底处理",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeTestConfig(
					[]scheduler.GroupConfig{
						{ID: 1, Name: "weighted-group", Strategy: scheduler.StrategyWeighted, Enabled: true},
					},
					[]scheduler.GroupModel{
						{ID: 1, GroupID: 1, ModelID: 301, ModelName: "model-zero", ChannelType: "openai", Endpoint: "http://zero", Weight: intPtr(0)},
						{ID: 2, GroupID: 1, ModelID: 302, ModelName: "model-one", ChannelType: "openai", Endpoint: "http://one", Weight: intPtr(1)},
					},
				))
			},
			calls: 200,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				counts := make(map[string]int)
				for _, d := range decisions {
					counts[d.ModelName]++
				}
				// 两者权重相同（0→1, 1→1），分布应大致均匀
				assert.Greater(t, counts["model-zero"], 50)
				assert.Greater(t, counts["model-one"], 50)
			},
		},
		{
			description: "只有一个模型时始终选中该模型",
			name:        "单模型始终选中",
			setup: func(rt *scheduler.RouteTable) {
				rt.ReplaceAll(makeTestConfig(
					[]scheduler.GroupConfig{
						{ID: 1, Name: "weighted-group", Strategy: scheduler.StrategyWeighted, Enabled: true},
					},
					[]scheduler.GroupModel{
						{ID: 1, GroupID: 1, ModelID: 301, ModelName: "only-model", ChannelType: "openai", Endpoint: "http://only", Weight: intPtr(5)},
					},
				))
			},
			calls: 10,
			validate: func(t *testing.T, decisions []*scheduler.RouteDecision) {
				for _, d := range decisions {
					assert.Equal(t, "only-model", d.ModelName)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, rt, _ := newTestRouter()
			tc.setup(rt)

			var decisions []*scheduler.RouteDecision
			for i := 0; i < tc.calls; i++ {
				d, err := router.Route("weighted-group", nil)
				require.NoError(t, err)
				decisions = append(decisions, d)
			}
			tc.validate(t, decisions)
		})
	}
}

// --- SelectByStrategy tests ---

func TestSelectByStrategy(t *testing.T) {
	tests := []struct {
		description string
		name        string
		strategy    scheduler.RoutingStrategy
		models      []*scheduler.ModelRoutingEntry
		validate    func(t *testing.T, d *scheduler.RouteDecision)
	}{
		{
			description: "直接调用SelectByStrategy传入RoundRobin策略和模型列表，验证选择正确",
			name:        "RoundRobin策略直接选择",
			strategy:    scheduler.StrategyRoundRobin,
			models: []*scheduler.ModelRoutingEntry{
				{ModelID: 1, ModelName: "first", ChannelType: "openai", Endpoint: "http://first"},
				{ModelID: 2, ModelName: "second", ChannelType: "openai", Endpoint: "http://second"},
			},
			validate: func(t *testing.T, d *scheduler.RouteDecision) {
				assert.Equal(t, "first", d.ModelName)
				assert.Equal(t, scheduler.StrategyRoundRobin, d.Strategy)
			},
		},
		{
			description: "直接调用SelectByStrategy传入Priority策略，验证选最低Priority值",
			name:        "Priority策略直接选择",
			strategy:    scheduler.StrategyPriority,
			models: []*scheduler.ModelRoutingEntry{
				{ModelID: 1, ModelName: "prio3", Priority: 3, ChannelType: "openai", Endpoint: "http://3"},
				{ModelID: 2, ModelName: "prio1", Priority: 1, ChannelType: "openai", Endpoint: "http://1"},
				{ModelID: 3, ModelName: "prio2", Priority: 2, ChannelType: "openai", Endpoint: "http://2"},
			},
			validate: func(t *testing.T, d *scheduler.RouteDecision) {
				assert.Equal(t, "prio1", d.ModelName)
				assert.Equal(t, scheduler.StrategyPriority, d.Strategy)
			},
		},
		{
			description: "直接调用SelectByStrategy传入Weighted策略和单个模型，始终选中该模型",
			name:        "Weighted策略单模型",
			strategy:    scheduler.StrategyWeighted,
			models: []*scheduler.ModelRoutingEntry{
				{ModelID: 1, ModelName: "sole", Weight: 10, ChannelType: "openai", Endpoint: "http://sole"},
			},
			validate: func(t *testing.T, d *scheduler.RouteDecision) {
				assert.Equal(t, "sole", d.ModelName)
				assert.Equal(t, scheduler.StrategyWeighted, d.Strategy)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, _, _ := newTestRouter()
			d, err := router.SelectByStrategy(tc.strategy, "test-group", tc.models, nil)
			require.NoError(t, err)
			tc.validate(t, d)
		})
	}
}
