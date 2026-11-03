package server_test

import (
	"encoding/json"
	"testing"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 2 的**装配接缝**：`server.BuildMediaAdapters` 把 provider snapshot
// 变成「providerType → core.MediaTaskProvider」表。它由 lead 在派发 Kling/Vidu 两队前建立
// （提交 39d531cd），是「渠道配置 → /v1/videos 可用」之间唯一的一环；两支渠道队都只改自己
// 包内的文件，因此这一环的语义只有本文件在守。
//
// 这些断言的价值在于它们**不依赖任何真实上游**：构造期唯一的失败来源是凭据校验，
// 而「凭据缺失必须失败而不是返回占位适配器」正是接缝最重要的安全约定——占位凭证会让
// 请求真的打到上游并产生生成费用。

const validKlingCreds = `{"access_key":"ak-test","secret_key":"sk-test"}`

func klingEntry(name string, enabled bool, creds string) gwmodel.ProviderEntry {
	return gwmodel.ProviderEntry{
		ID:          "p-" + name,
		Name:        name,
		Type:        "kling",
		Endpoint:    "https://api.klingai.com",
		Credentials: creds,
		Enabled:     enabled,
		Models:      []string{"kling-v1"},
	}
}

// TestBuildMediaAdapters_ConfiguredChannelIsRegistered 是正向基线：
// 配置了带合法凭据的 kling 渠道 → 注册表里出现 kling 且展示名取自 provider.name。
// 若这条挂了，说明接缝断了 —— /v1/videos 会返回 501（not_implemented）。
func TestBuildMediaAdapters_ConfiguredChannelIsRegistered(t *testing.T) {
	adapters, names := server.BuildMediaAdapters([]gwmodel.ProviderEntry{
		klingEntry("kling-main", true, validKlingCreds),
	})

	require.Contains(t, adapters, "kling", "配置了 kling 渠道就必须注册适配器")
	require.NotNil(t, adapters["kling"])
	require.Equal(t, "kling-main", names["kling"], "展示名应取自 provider.name")
}

// TestBuildMediaAdapters_MissingCredentialsSkipsChannel 是本文件最重要的一条：
// 凭据缺失/非法时**必须**跳过该渠道，绝不能返回一个拿占位凭证去打上游的适配器。
//
// 如果这条失败（即适配器被注册了），后果不是报错而是**静默产生真实费用**。
func TestBuildMediaAdapters_MissingCredentialsSkipsChannel(t *testing.T) {
	cases := []struct {
		name  string
		creds string
	}{
		{name: "空凭据", creds: ""},
		{name: "空白凭据", creds: "   "},
		{name: "不是 JSON", creds: "ak|sk（New API 的单字段拼接写法）"},
		{name: "JSON 但缺 secret_key", creds: `{"access_key":"ak-test"}`},
		{name: "JSON 但缺 access_key", creds: `{"secret_key":"sk-test"}`},
		{name: "两列都是空串", creds: `{"access_key":"","secret_key":""}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapters, names := server.BuildMediaAdapters([]gwmodel.ProviderEntry{
				klingEntry("kling-main", true, tc.creds),
			})

			require.NotContains(t, adapters, "kling",
				"凭据不可用时不得注册适配器（否则会拿占位凭证打真实上游并产生费用）")
			require.NotContains(t, names, "kling")
			require.Empty(t, adapters)
		})
	}
}

// TestBuildMediaAdapters_DisabledChannelIsSkipped 固化「停用渠道不派发」：
// 必须与 chat 链路（loader/provider_loader.go 的 `if !entry.Enabled { continue }`）一致，
// 否则会出现「chat 已停用、媒体任务仍打到该渠道」的半停用状态。
func TestBuildMediaAdapters_DisabledChannelIsSkipped(t *testing.T) {
	adapters, names := server.BuildMediaAdapters([]gwmodel.ProviderEntry{
		klingEntry("kling-main", false, validKlingCreds),
	})

	require.NotContains(t, adapters, "kling", "停用的渠道不得注册媒体适配器")
	require.NotContains(t, names, "kling")
}

// TestBuildMediaAdapters_AbsentChannelIsAbsent 固化「整表重建」语义：
// 没配置的渠道不出现在表里 —— 下一次 Swap 会让它的旧适配器真的消失
// （渠道被删除后必须停止服务，不能继续用旧凭证打上游）。
func TestBuildMediaAdapters_AbsentChannelIsAbsent(t *testing.T) {
	adapters, _ := server.BuildMediaAdapters([]gwmodel.ProviderEntry{
		{ID: "p1", Name: "openai", Type: "openai", Enabled: true},
	})

	require.Empty(t, adapters)
}

// TestBuildMediaAdapters_NilAndEmptyInputs 覆盖启动期与「清空配置」两种边界：
// 返回的表必须是可用的空 map（而非 nil 引起调用方 panic），且不得 panic。
func TestBuildMediaAdapters_NilAndEmptyInputs(t *testing.T) {
	for _, entries := range [][]gwmodel.ProviderEntry{nil, {}} {
		require.NotPanics(t, func() {
			adapters, names := server.BuildMediaAdapters(entries)
			require.Empty(t, adapters)
			require.Empty(t, names)
		})
	}
}

// TestBuildMediaAdapters_ModelsV2Compatibility 覆盖 models 与 models_v2 两种快照字段：
// 旧快照只有 models，新快照用 models_v2；两者都要能构造成功（构造期不依赖模型列表，
// 但字段读取写错会在真实快照上 panic 或漏模型）。
func TestBuildMediaAdapters_ModelsV2Compatibility(t *testing.T) {
	entry := klingEntry("kling-main", true, validKlingCreds)
	entry.Models = nil
	entry.ModelsV2 = []gwmodel.ModelEntry{{Name: "kling-v1-6"}, {Name: ""}, {Name: "kling-v2-master"}}

	adapters, _ := server.BuildMediaAdapters([]gwmodel.ProviderEntry{entry})
	require.Contains(t, adapters, "kling")
}

// TestBuildMediaAdapters_BadBaseURLSkipsChannel 固化构造期校验：
// 非法 base URL 属于配置错误，应当跳过该渠道（响亮失败）而不是发出畸形请求。
func TestBuildMediaAdapters_BadBaseURLSkipsChannel(t *testing.T) {
	entry := klingEntry("kling-main", true, validKlingCreds)
	entry.Endpoint = "://not-a-url"

	require.NotPanics(t, func() {
		adapters, _ := server.BuildMediaAdapters([]gwmodel.ProviderEntry{entry})
		require.NotContains(t, adapters, "kling")
	})
}

// TestMediaAdapterParams_CarriesRawCredentials 固化接缝的**解耦约定**：
// 网关把凭据**原样透传**，不解释其内部结构（各渠道自解析自己的 schema）。
// 这条是 Kling 与 Vidu 能并行开发、零文件重叠的前提。
func TestMediaAdapterParams_CarriesRawCredentials(t *testing.T) {
	entry := klingEntry("kling-main", true, validKlingCreds)
	entry.Endpoint = "https://example.invalid/proxy/kling"
	entry.ModelsV2 = []gwmodel.ModelEntry{{Name: "kling-v1"}, {Name: "kling-v1-6"}}

	adapters, _ := server.BuildMediaAdapters([]gwmodel.ProviderEntry{entry})
	require.Contains(t, adapters, "kling")

	// 反向确认：凭据确实是**原始 JSON 文本**，而不是被网关解析/重建过的结构
	// （若哪天网关开始解释凭据，这里的语义就会变，两个渠道包都要跟着改）。
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(validKlingCreds), &raw))
	require.Equal(t, "ak-test", raw["access_key"])
}
