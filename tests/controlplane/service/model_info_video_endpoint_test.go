package service_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/providers/kling"
	"github.com/example/llm-gateway/internal/providers/vidu"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件冻结「视频组对外报哪个提交端点」这一**对外契约**（Batch 3 · E1-补充）。
//
// 契约（定稿）：
//   - `video_api_endpoint`  string   —— 首选端点（保持单值，向后兼容既有 Seedance 消费方）；
//   - `video_api_endpoints` []string —— 全部可用端点（并集，首选排第一）。
//
// 取值语义：
//   - 组内只有 Seedance 成员（或渠道类型无法判定）→
//     ["POST /v1/contents/generations/tasks"]（与改动前**逐字一致**）；
//   - 组内只有 Kling/Vidu 成员 → ["POST /v1/videos"]；
//   - 混合 → ["POST /v1/videos", "POST /v1/contents/generations/tasks"]。
//
// 这里刻意把端点写成**字面量**而不是引用 service.MediaVideoEndpoint* 常量：
// 本测试是契约钉子，常量被改动时必须让测试失败。

const (
	contractEndpointSeedance = "POST /v1/contents/generations/tasks"
	contractEndpointUnified  = "POST /v1/videos"
)

// videoGroupFixture 构造一个 mode=video 的模型组（成员渠道类型由 providerTypes 指定）。
func videoGroupFixture(t *testing.T, groupName string, providerTypes []string) (service.GroupModelInfo, error) {
	t.Helper()
	groupID := uint(9)
	ids := make([]uuid.UUID, 0, len(providerTypes))
	g := model.Group{ID: groupID, Name: groupName, Enabled: true}
	models := map[uuid.UUID]*model.Model{}
	for i, pt := range providerTypes {
		id := uuid.New()
		ids = append(ids, id)
		g.Models = append(g.Models, model.GroupModel{GroupID: groupID, ModelID: id})
		models[id] = &model.Model{
			ID:        id,
			Name:      groupName + "-m" + string(rune('a'+i)),
			ModelType: "video",
			Enabled:   boolPtr(true),
			Provider:  model.Provider{Type: pt},
			// 带上能力字段，顺带证明 mode 归一后 aggregateVideoCaps 真的被执行了。
			VideoSupportedResolutions: []string{"720p"},
			VideoMaxDurationSeconds:   5,
		}
	}
	svc := service.NewModelInfoService(
		&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
		&fakeModelInfoRepo{getByIDsFunc: func(_ []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
			return models, nil
		}},
	)
	result, err := svc.GetGroupModelInfo(nil)
	require.NoError(t, err)
	require.Contains(t, result, groupName)
	return result[groupName], nil
}

// TestVideoAPIEndpointContract_SeedanceOnlyGroupStaysBackwardCompatible 是**向后兼容钉子**：
// 纯 Seedance 组（含 provider.type 缺失的老数据）读到的 video_api_endpoint 必须与
// 改动前逐字一致，否则线上既有消费方（前端/脚本）会被无声地改道到没有适配器的 /v1/videos。
func TestVideoAPIEndpointContract_SeedanceOnlyGroupStaysBackwardCompatible(t *testing.T) {
	cases := []struct {
		name          string
		providerTypes []string
	}{
		{"单个 seedance-relay 成员", []string{"seedance-relay"}},
		{"seedance-relay + seedance-volcengine 混合", []string{"seedance-relay", "seedance-volcengine"}},
		{"渠道类型为空（老数据/未预加载 Provider）", []string{""}},
		{"渠道类型未知（未接线渠道）", []string{"some-future-vendor"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := videoGroupFixture(t, "seedance-group", tc.providerTypes)
			require.NoError(t, err)
			assert.Equal(t, "video", info.Mode)
			assert.Equal(t, contractEndpointSeedance, info.VideoAPIEndpoint,
				"纯 Seedance 组的首选端点必须保持旧端点（向后兼容）")
			assert.Equal(t, []string{contractEndpointSeedance}, info.VideoAPIEndpoints)
			assert.Equal(t, 5, info.VideoMaxDurationSeconds, "视频能力聚合不得被跳过")
		})
	}
}

// TestVideoAPIEndpointContract_UnifiedChannelOnlyGroup 覆盖只含 Kling/Vidu 成员的组：
// 这类组以前被**写死**报成 Seedance 旧端点（错的，网关侧 /v1/contents/generations/tasks
// 不认 Kling/Vidu），现在必须报通用 /v1/videos。
func TestVideoAPIEndpointContract_UnifiedChannelOnlyGroup(t *testing.T) {
	cases := []struct {
		name          string
		providerTypes []string
	}{
		{"单个 kling 成员", []string{kling.MediaProviderType}},
		{"单个 vidu 成员", []string{vidu.MediaProviderType}},
		{"kling + vidu 混合", []string{kling.MediaProviderType, vidu.MediaProviderType}},
		{"大小写不敏感（Kling）", []string{"Kling"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := videoGroupFixture(t, "unified-group", tc.providerTypes)
			require.NoError(t, err)
			assert.Equal(t, contractEndpointUnified, info.VideoAPIEndpoint)
			assert.Equal(t, []string{contractEndpointUnified}, info.VideoAPIEndpoints)
		})
	}
}

// TestVideoAPIEndpointContract_MixedGroupReportsBothEndpoints 覆盖混合组：
// 两条链路并存（D1 决策），单值字段无法诚实表达 → 并集 + 首选新端点。
func TestVideoAPIEndpointContract_MixedGroupReportsBothEndpoints(t *testing.T) {
	info, err := videoGroupFixture(t, "mixed-group", []string{"seedance-relay", kling.MediaProviderType})
	require.NoError(t, err)

	assert.Equal(t, contractEndpointUnified, info.VideoAPIEndpoint,
		"混合组的首选端点是通用 /v1/videos（新统一端点，带完整任务平台）")
	assert.Equal(t, []string{contractEndpointUnified, contractEndpointSeedance}, info.VideoAPIEndpoints,
		"混合组必须同时报出两个端点，由消费方按落在哪个渠道选择")

	// 顺序不因成员顺序而变（顺序确定性 = 契约确定性）。
	reversed, err := videoGroupFixture(t, "mixed-group-reversed", []string{kling.MediaProviderType, "seedance-relay"})
	require.NoError(t, err)
	assert.Equal(t, info.VideoAPIEndpoints, reversed.VideoAPIEndpoints,
		"端点顺序必须与成员声明顺序无关")
}

// TestVideoAPIEndpointContract_NonVideoGroupHasNoEndpoint 非视频组不得出现端点字段。
func TestVideoAPIEndpointContract_NonVideoGroupHasNoEndpoint(t *testing.T) {
	groupID := uint(11)
	g := model.Group{ID: groupID, Name: "chat-group", Enabled: true,
		Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}}}
	models := map[uuid.UUID]*model.Model{
		modelID1: {ID: modelID1, Name: "c", ModelType: "chat", Enabled: boolPtr(true),
			Provider: model.Provider{Type: kling.MediaProviderType}},
	}
	svc := service.NewModelInfoService(
		&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
		&fakeModelInfoRepo{getByIDsFunc: func(_ []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
	)
	result, err := svc.GetGroupModelInfo(nil)
	require.NoError(t, err)
	info := result["chat-group"]
	assert.Equal(t, "chat", info.Mode)
	assert.Empty(t, info.VideoAPIEndpoint)
	assert.Empty(t, info.VideoAPIEndpoints)
}

// ─────────────────────────── 静态漂移守卫 ───────────────────────────

// mediaAdapterBuildersProviderTypeRe 匹配 gateway 媒体适配器注册表里的渠道类型声明：
//
//	{ProviderType: kling.MediaProviderType, Build: kling.NewMediaAdapter},
var mediaAdapterBuildersProviderTypeRe = regexp.MustCompile(`ProviderType:\s*([A-Za-z_][A-Za-z0-9_]*)\.MediaProviderType`)

// TestMediaVideoUnifiedProviderTypesMatchGatewayAdapterRegistry 是**跨包漂移守卫**：
// 控制面的「哪些渠道走 /v1/videos」集合必须与网关侧 mediaAdapterBuilders 注册的
// 媒体适配器**双向一致**。新增一个媒体渠道却忘了同步控制面时，这个用例会失败并点名
// 缺哪个包 —— 否则新渠道的组会被静默报成 Seedance 旧端点（运维/前端看到错端点）。
func TestMediaVideoUnifiedProviderTypesMatchGatewayAdapterRegistry(t *testing.T) {
	source := filepath.Join(mediaEndpointRepoRoot(t), "internal", "gateway", "server", "media_adapters.go")
	raw, err := os.ReadFile(source)
	require.NoError(t, err, "读不到网关媒体适配器注册表: %s", source)

	// 包别名 → 已注册渠道类型（新渠道必须在这里登记，否则用例失败）。
	// 之所以要这张表：Go 无法按别名字符串反射出包级常量，只能显式登记；
	// 未登记的别名正是「新增渠道未同步」的信号。
	knownPackages := map[string]string{
		"kling": kling.MediaProviderType,
		"vidu":  vidu.MediaProviderType,
	}

	registered := map[string]struct{}{}
	for _, m := range mediaAdapterBuildersProviderTypeRe.FindAllStringSubmatch(string(raw), -1) {
		alias := m[1]
		value, ok := knownPackages[alias]
		if !ok {
			t.Fatalf("网关注册了新的媒体渠道包 %q，但本守卫与控制面的统一端点集合都没登记它。"+
				"请在 service.model_info_service.go 的 mediaVideoUnifiedProviderTypes 里加上"+
				"该渠道的 MediaProviderType，并在本用例的 knownPackages 里补一行", alias)
		}
		registered[strings.ToLower(value)] = struct{}{}
	}
	require.NotEmpty(t, registered, "未从 %s 解析出任何媒体适配器（正则或文件结构变了？）", source)

	declared := service.MediaVideoUnifiedProviderTypes()
	declaredSet := make(map[string]struct{}, len(declared))
	for _, v := range declared {
		declaredSet[v] = struct{}{}
	}

	var missingInControlPlane, missingInGateway []string
	for v := range registered {
		if _, ok := declaredSet[v]; !ok {
			missingInControlPlane = append(missingInControlPlane, v)
		}
	}
	for v := range declaredSet {
		if _, ok := registered[v]; !ok {
			missingInGateway = append(missingInGateway, v)
		}
	}
	sort.Strings(missingInControlPlane)
	sort.Strings(missingInGateway)

	assert.Empty(t, missingInControlPlane,
		"这些渠道在网关已注册媒体适配器，但控制面仍会把它们的组报成 Seedance 旧端点: %v", missingInControlPlane)
	assert.Empty(t, missingInGateway,
		"控制面声称这些渠道走 /v1/videos，但网关侧没有对应适配器（会 501）: %v", missingInGateway)
}

// mediaEndpointRepoRoot 从本测试文件向上找到含 go.mod 的目录。
func mediaEndpointRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("从 %s 向上未找到 go.mod", file)
	return ""
}
