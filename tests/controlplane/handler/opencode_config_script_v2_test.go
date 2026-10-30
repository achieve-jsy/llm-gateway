package handler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLegacyV1State 造一份"老 V1 形状 + 两套 plugin 声明 + 另一个 provider"的配置，
// 外加一个残留的 V1 插件文件，用来验证 V2 写入时的清理行为。
func seedLegacyV1State(t *testing.T, home string) {
	t.Helper()
	seedOpencodeConfig(t, home, map[string]any{
		"provider": map[string]any{
			"biangfeng-gateway": map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"options": map[string]any{"baseURL": "http://old.example/v1"},
			},
		},
		"providers": map[string]any{
			"other-gateway": map[string]any{"name": "别人的网关"},
		},
		"plugin":  []any{"opencode-models-discovery", "keep-me"},
		"plugins": []any{map[string]any{"package": "keep-me-2"}},
	})
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "opencode-models-discovery.js"),
		[]byte("// stale v1 plugin\n"), 0o644))
}

// TestMacScript_V2WritesNativeShape 测什么：本机是 OpenCode 2 时，脚本要写出原生 V2 形状
// （providers + package + settings + 静态 models），并把能力映射到位、清掉老的 V1 残留。
// 期望：模型 3 个（embedding 被跳过）；能力/上限与 /v1/model/info 一致；旧 provider 键消失、
// 别的 provider 与别的插件原样保留。
func TestMacScript_V2WritesNativeShape(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{
		ProviderID:   "biangfeng-gateway",
		ProviderName: "测试供应商",
		BaseURL:      gw.BaseURL(),
		APIKey:       "sk-test",
	}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := t.TempDir()
	seedLegacyV1State(t, home)
	binDir := writeFakeOpencode(t, home, "2.0.3")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)
	require.Contains(t, out, "V2 模式")

	cfg := readOpencodeConfig(t, home)
	entry := dig(cfg, "providers", "biangfeng-gateway")
	require.NotNil(t, entry, "V2 应写入 providers.biangfeng-gateway")
	assert.Equal(t, "aisdk:@ai-sdk/openai-compatible", dig(entry, "package"))
	assert.Equal(t, gw.BaseURL(), dig(entry, "settings", "baseURL"))
	assert.Equal(t, "sk-test", dig(entry, "settings", "apiKey"))

	models, ok := dig(entry, "models").(map[string]any)
	require.True(t, ok, "models 应为对象")
	require.Len(t, models, 3, "embedding 模型不应写入")
	assert.NotContains(t, models, "embed-small")

	ml := models["middle-level"]
	require.NotNil(t, ml)
	assert.Equal(t, true, dig(ml, "capabilities", "tools"))
	assert.Equal(t, []any{"text", "image"}, dig(ml, "capabilities", "input"))
	assert.Equal(t, []any{"low", "medium", "high"}, dig(ml, "capabilities", "reasoning", "effort"))
	assert.Equal(t, float64(1000000), dig(ml, "limit", "context"))
	assert.Equal(t, float64(65536), dig(ml, "limit", "output"))

	vp := models["vision-pro"]
	require.NotNil(t, vp)
	assert.Equal(t, []any{"text", "image"}, dig(vp, "capabilities", "input"),
		"源数据只有 image 时要补上 text")
	assert.Nil(t, dig(vp, "capabilities", "tools"), "不支持工具调用的模型不应有 tools")

	pc := models["plain-chat"]
	require.NotNil(t, pc)
	assert.Equal(t, []any{"text"}, dig(pc, "capabilities", "input"))
	assert.Nil(t, dig(pc, "limit"), "没有 token 上限信息时不应编造 limit")

	// 旧 V1 形状与插件声明被清掉，别的东西不动
	assert.Nil(t, dig(cfg, "provider"), "顶层 provider（V1 形状）应被清空删除")
	assert.Equal(t, "别人的网关", dig(cfg, "providers", "other-gateway", "name"))
	assert.Equal(t, []any{"keep-me"}, cfg["plugin"])
	plugins, ok := cfg["plugins"].([]any)
	require.True(t, ok)
	require.Len(t, plugins, 1)
	assert.Equal(t, "keep-me-2", dig(plugins[0], "package"))

	// V1 插件文件不能留在 V2 用户的插件目录里
	_, err := os.Stat(filepath.Join(home, ".config", "opencode", "plugins", "opencode-models-discovery.js"))
	assert.True(t, os.IsNotExist(err), "V2 模式下应删除 V1 插件副本")

	assert.Contains(t, gw.callbackStatuses(), "success")
}

// TestMacScript_HomePathWithSpacesAndUnicode 测什么：HOME 含空格和中文时脚本仍写对文件。
// 期望：退出码 0，模型照常写入。
func TestMacScript_HomePathWithSpacesAndUnicode(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := filepath.Join(t.TempDir(), "用户 目录")
	binDir := writeFakeOpencode(t, home, "2.0.3")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	models := dig(readOpencodeConfig(t, home), "providers", "biangfeng-gateway", "models")
	require.NotNil(t, models)
	assert.Len(t, models, 3)
}
