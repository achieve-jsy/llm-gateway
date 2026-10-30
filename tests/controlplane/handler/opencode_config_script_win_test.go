package handler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// winPluginFile 是 Windows 脚本在用户目录下的插件落点（PowerShell 在 Unix 上会把
// 反斜杠路径归一成 /，实测两者等价，统一用正斜杠）。
func winPluginFile(profile string) string {
	return filepath.Join(profile, ".config", "opencode", "plugins", "opencode-models-discovery.js")
}

// seedWinPlugin 放一份残留的 V1 插件副本，用来验证 V2 会删掉它。
func seedWinPlugin(t *testing.T, profile, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(winPluginFile(profile)), 0o755))
	require.NoError(t, os.WriteFile(winPluginFile(profile), []byte(content), 0o644))
}

// TestWinScript_V2WritesNativeShape 测什么：Windows 脚本在 OpenCode 2 下同样写原生 V2 形状。
// 期望：providers 形状 + 能力映射 + 3 个模型，残留的 V1 插件副本被删除，退出码 0。
func TestWinScript_V2WritesNativeShape(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "")
	require.Equal(t, 200, code)

	profile := t.TempDir()
	seedWinPlugin(t, profile, "// stale v1 plugin\n")
	binDir := writeFakeOpencode(t, profile, "2.0.3")

	out, exitCode := runWinScript(t, script, profile, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	cfg := readOpencodeConfig(t, profile)
	entry := dig(cfg, "providers", "biangfeng-gateway")
	require.NotNil(t, entry, "V2 应写入 providers.biangfeng-gateway")
	assert.Equal(t, "aisdk:@ai-sdk/openai-compatible", dig(entry, "package"))
	assert.Equal(t, gw.BaseURL(), dig(entry, "settings", "baseURL"))
	assert.Equal(t, "sk-test", dig(entry, "settings", "apiKey"))
	assert.Nil(t, dig(cfg, "provider"), "V2 不能留下 V1 形状")

	models, ok := dig(entry, "models").(map[string]any)
	require.True(t, ok)
	require.Len(t, models, 3, "embedding 模型不应写入")
	assert.Equal(t, true, dig(models["middle-level"], "capabilities", "tools"))
	assert.Equal(t, float64(1000000), dig(models["middle-level"], "limit", "context"))

	_, err := os.Stat(winPluginFile(profile))
	assert.True(t, os.IsNotExist(err), "V2 模式下应删除 V1 插件副本")
}

// TestWinScript_V1KeepsDynamicDiscovery 测什么：Windows 脚本在 OpenCode 1 下保持动态发现。
// 期望：provider + options.modelsDiscovery、插件文件就位、无顶层 providers、退出码 0，
// 且 plugin 与 plugins 两套声明里的本插件都被清掉。
func TestWinScript_V1KeepsDynamicDiscovery(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "")
	require.Equal(t, 200, code)

	profile := t.TempDir()
	seedOpencodeConfig(t, profile, map[string]any{
		"plugin":  []any{"opencode-models-discovery", "keep-me"},
		"plugins": []any{map[string]any{"package": "opencode-models-discovery"}, map[string]any{"package": "keep-me-2"}},
	})
	binDir := writeFakeOpencode(t, profile, "1.19.5")

	out, exitCode := runWinScript(t, script, profile, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	cfg := readOpencodeConfig(t, profile)
	entry := dig(cfg, "provider", "biangfeng-gateway")
	require.NotNil(t, entry, "V1 应写入 provider.biangfeng-gateway")
	assert.Equal(t, "@ai-sdk/openai-compatible", dig(entry, "npm"))
	assert.Equal(t, true, dig(entry, "options", "modelsDiscovery", "enabled"))
	assert.Nil(t, cfg["providers"], "V1 不能出现 V2 形状")

	assert.Equal(t, []any{"keep-me"}, cfg["plugin"], "plugin 数组里的本插件应被清掉")
	plugins, ok := cfg["plugins"].([]any)
	require.True(t, ok, "plugins 数组应保留其它插件")
	require.Len(t, plugins, 1)
	assert.Equal(t, "keep-me-2", dig(plugins[0], "package"))

	_, err := os.Stat(winPluginFile(profile))
	require.NoError(t, err, "V1 应下载插件")
}

// TestWinScript_UserProfileWithSpaces 测什么：USERPROFILE 含空格与中文时仍能写对文件。
// 期望：退出码 0，模型写出。
func TestWinScript_UserProfileWithSpaces(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "")
	require.Equal(t, 200, code)

	profile := filepath.Join(t.TempDir(), "用户 目录")
	binDir := writeFakeOpencode(t, profile, "2.0.3")

	out, exitCode := runWinScript(t, script, profile, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	models := dig(readOpencodeConfig(t, profile), "providers", "biangfeng-gateway", "models")
	require.NotNil(t, models)
	assert.Len(t, models, 3)
}

// TestWinScript_OpenCodeBinDirFallback 测什么：OpenCode 不在 PATH 但装在
// %USERPROFILE%\.opencode\bin 时（curl 装的 V2）仍要判成 V2，与 mac 脚本对齐。
// 期望：走 V2 形状，不写 V1 的 provider 键。
func TestWinScript_OpenCodeBinDirFallback(t *testing.T) {
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if _, err := os.Stat(filepath.Join(dir, "opencode")); err == nil {
			t.Skipf("%s 里有真实 opencode，构造不出兜底场景", dir)
		}
	}

	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "")
	require.Equal(t, 200, code)

	profile := t.TempDir()
	writeFakeOpencode(t, filepath.Join(profile, ".opencode"), "2.0.3")

	// 不把任何目录塞进 PATH：openocde 只能靠 $env:USERPROFILE\.opencode\bin 兜底找到
	out, exitCode := runWinScript(t, script, profile)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	cfg := readOpencodeConfig(t, profile)
	require.NotNil(t, dig(cfg, "providers", "biangfeng-gateway"), "应判成 V2")
	assert.Nil(t, cfg["provider"])
}
