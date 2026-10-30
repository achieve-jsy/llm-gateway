package handler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedWinPlugin 在两个候选路径都放一份 V1 插件副本，避免断言依赖平台的分隔符语义。
func seedWinPlugin(t *testing.T, profile, content string) {
	t.Helper()
	for _, dir := range winCandidates(t, profile, ".config", "opencode", "plugins") {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, "opencode-models-discovery.js"), []byte(content), 0o644)
	}
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

	cfg := readWinConfig(t, profile)
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

	for _, p := range winCandidates(t, profile, ".config", "opencode", "plugins", "opencode-models-discovery.js") {
		_, err := os.Stat(p)
		assert.True(t, os.IsNotExist(err), "V2 模式下应删除 V1 插件副本: %s", p)
	}
}

// TestWinScript_V1KeepsDynamicDiscovery 测什么：Windows 脚本在 OpenCode 1 下保持动态发现。
// 期望：provider + options.modelsDiscovery、插件文件就位、无顶层 providers、退出码 0。
func TestWinScript_V1KeepsDynamicDiscovery(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "")
	require.Equal(t, 200, code)

	profile := t.TempDir()
	binDir := writeFakeOpencode(t, profile, "1.19.5")

	out, exitCode := runWinScript(t, script, profile, binDir)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	cfg := readWinConfig(t, profile)
	entry := dig(cfg, "provider", "biangfeng-gateway")
	require.NotNil(t, entry, "V1 应写入 provider.biangfeng-gateway")
	assert.Equal(t, "@ai-sdk/openai-compatible", dig(entry, "npm"))
	assert.Equal(t, true, dig(entry, "options", "modelsDiscovery", "enabled"))
	assert.Nil(t, cfg["providers"], "V1 不能出现 V2 形状")

	var found bool
	for _, p := range winCandidates(t, profile, ".config", "opencode", "plugins", "opencode-models-discovery.js") {
		if _, err := os.Stat(p); err == nil {
			found = true
		}
	}
	assert.True(t, found, "V1 应下载插件")
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

	models := dig(readWinConfig(t, profile), "providers", "biangfeng-gateway", "models")
	require.NotNil(t, models)
	assert.Len(t, models, 3)
}
