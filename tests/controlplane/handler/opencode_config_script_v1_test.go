package handler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLegacyPluginState 造一份"plugin 数组里还挂着本插件"的老配置 + 一个残留插件文件。
func seedLegacyPluginState(t *testing.T, home string) {
	t.Helper()
	seedOpencodeConfig(t, home, map[string]any{
		"plugin": []any{"opencode-models-discovery", "keep-me"},
	})
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "opencode-models-discovery.js"),
		[]byte("// stale plugin\n"), 0o644))
}

// TestMacScript_V1KeepsDynamicDiscovery 测什么：本机是 OpenCode 1 时，脚本保持原行为——
// 只写 provider 元数据交给插件动态发现，并把新版插件下载到 plugins/。
// 期望：V1 形状（npm + options.modelsDiscovery）、无顶层 providers、插件文件就位、
// plugin 数组里的本插件声明被清掉。
func TestMacScript_V1KeepsDynamicDiscovery(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := t.TempDir()
	seedLegacyPluginState(t, home)
	binDir := writeFakeOpencode(t, home, "1.19.5")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)
	require.Contains(t, out, "V1 模式")

	cfg := readOpencodeConfig(t, home)
	entry := dig(cfg, "provider", "biangfeng-gateway")
	require.NotNil(t, entry, "V1 应写入 provider.biangfeng-gateway")
	assert.Equal(t, "@ai-sdk/openai-compatible", dig(entry, "npm"))
	assert.Equal(t, gw.BaseURL(), dig(entry, "options", "baseURL"))
	assert.Equal(t, "sk-test", dig(entry, "options", "apiKey"))
	assert.Equal(t, true, dig(entry, "options", "modelsDiscovery", "enabled"))
	assert.Nil(t, dig(entry, "models"), "V1 不写死模型，交给插件发现")

	assert.Nil(t, cfg["providers"], "V1 不能出现 V2 形状")
	assert.Equal(t, []any{"keep-me"}, cfg["plugin"])

	pluginFile := filepath.Join(home, ".config", "opencode", "plugins", "opencode-models-discovery.js")
	content, err := os.ReadFile(pluginFile)
	require.NoError(t, err, "V1 应下载插件")
	assert.Equal(t, "// fake plugin\n", string(content))

	assert.Contains(t, gw.callbackStatuses(), "success")
}

// brokenOpencodeDir 造一个"问不出主版本"的 opencode（直接退出、不打印），用来测兜底判定。
// 用假二进制而不是删 PATH：宿主环境（本例 /opt/homebrew/bin）可能本来就有 opencode，
// 只有屏蔽掉才能稳定走到"版本未知"分支。
func brokenOpencodeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "opencode"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	return binDir
}

// TestMacScript_DetectionFallback 测什么：opencode 版本探测不出时的兜底判定。
// 期望：无 cli.json 按 V1 写；有 cli.json（V2 客户端启动过）按 V2 写。两者只出现一种形状。
func TestMacScript_DetectionFallback(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	cases := []struct {
		name       string
		seedCliCfg bool
		wantV2     bool
	}{
		{"探测不出且无 cli.json 按 V1", false, false},
		{"探测不出但有 cli.json 按 V2", true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			if c.seedCliCfg {
				dir := filepath.Join(home, ".config", "opencode")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "cli.json"), []byte("{}\n"), 0o644))
			}

			out, exitCode := runMacScript(t, script, home, brokenOpencodeDir(t))
			require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

			cfg := readOpencodeConfig(t, home)
			if c.wantV2 {
				require.Contains(t, out, "V2 模式")
				require.NotNil(t, dig(cfg, "providers", "biangfeng-gateway"))
				assert.Nil(t, cfg["provider"], "V2 不能写 V1 形状")
			} else {
				require.Contains(t, out, "V1 模式")
				require.NotNil(t, dig(cfg, "provider", "biangfeng-gateway"))
				assert.Nil(t, cfg["providers"], "V1 不能写 V2 形状")
			}
		})
	}
}

// TestMacScript_ForceV1OnV2Binary 测什么：手动 ?ver=v1 覆盖掉 2.x 的自动探测。
// 期望：仍然只写 V1 形状（逃生口生效），不出现 providers。
func TestMacScript_ForceV1OnV2Binary(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "v1")
	require.Equal(t, 200, code)
	require.Contains(t, script, `FORCE_VERSION="v1"`)

	home := t.TempDir()
	binDir := writeFakeOpencode(t, home, "2.0.3")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	cfg := readOpencodeConfig(t, home)
	require.NotNil(t, dig(cfg, "provider", "biangfeng-gateway"))
	assert.Nil(t, cfg["providers"], "强制 V1 时不能出现 V2 形状")
}
