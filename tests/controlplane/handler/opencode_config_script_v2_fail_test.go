package handler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMacScript_V2ModelInfoFailure 测什么：/v1/models 正常但 /v1/model/info 挂了。
// 期望：模型清单照样写进去（只是没有 capabilities/limit），不能让整次安装失败。
func TestMacScript_V2ModelInfoFailure(t *testing.T) {
	gw := newFakeGateway(t)
	gw.InfoStatus = 500

	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := t.TempDir()
	binDir := writeFakeOpencode(t, home, "2.0.3")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.Equal(t, 0, exitCode, "能力接口失败不应中断安装，输出:\n%s", out)

	models, ok := dig(readOpencodeConfig(t, home), "providers", "biangfeng-gateway", "models").(map[string]any)
	require.True(t, ok, "应仍然写入模型清单")
	// 能力接口挂了就没法判定 chat/embedding（mode 只在 model_info 里），
	// 脚本只能把 /v1/models 的全部模型都写进去 —— 这是有意降级，不是漏过滤。
	require.Len(t, models, 4)
	for id, entry := range models {
		assert.Equal(t, id, dig(entry, "name"), "退化为只有 name 的条目")
		assert.Nil(t, dig(entry, "capabilities"), "%s 不应凭空造出能力", id)
		assert.Nil(t, dig(entry, "limit"), "%s 不应凭空造出 token 上限", id)
	}
	assert.Contains(t, gw.callbackStatuses(), "success")
}

// TestMacScript_V2GatewayUnreachable 测什么：网关整个不可达（模型接口也拿不到）。
// 期望：非零退出，且本地已有配置原封不动（不能写半截）。
func TestMacScript_V2GatewayUnreachable(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := t.TempDir()
	binDir := writeFakeOpencode(t, home, "2.0.3")
	seedLegacyV1State(t, home)

	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	before, err := os.ReadFile(configPath)
	require.NoError(t, err)

	gw.Server.Close()
	out, exitCode := runMacScript(t, script, home, binDir)
	require.NotEqual(t, 0, exitCode, "网关不可达必须失败退出，输出:\n%s", out)

	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "失败时不允许改动已有配置")
}

// TestMacScript_V2EmptyModelList 测什么：Key 看不到任何可用模型。
// 期望：非零退出、不生成配置文件、回调上报 failed（否则前端会轮询到超时）。
func TestMacScript_V2EmptyModelList(t *testing.T) {
	gw := newFakeGateway(t)
	gw.Models = nil

	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	home := t.TempDir()
	binDir := writeFakeOpencode(t, home, "2.0.3")

	out, exitCode := runMacScript(t, script, home, binDir)
	require.NotEqual(t, 0, exitCode, "没有模型必须失败退出，输出:\n%s", out)

	_, err := os.Stat(filepath.Join(home, ".config", "opencode", "opencode.json"))
	assert.True(t, os.IsNotExist(err), "失败时不应生成配置文件")
	assert.Contains(t, gw.callbackStatuses(), "failed")
}

// TestMacScript_InvalidExistingConfig 测什么：本地 opencode.json 不是合法 JSON 时
// 不能"当成空配置"继续写（那会把用户的其它 provider/plugin 全清掉）。
// 期望：非零退出、文件逐字节不变、回调 failed；V1/V2 两个分支行为一致。
func TestMacScript_InvalidExistingConfig(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-test"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "")
	require.Equal(t, 200, code)

	cases := []struct {
		name    string
		version string
	}{
		{"V1 分支", "1.19.5"},
		{"V2 分支", "2.0.3"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			configDir := filepath.Join(home, ".config", "opencode")
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			broken := []byte("{\"provider\": {oops}\n")
			configPath := filepath.Join(configDir, "opencode.json")
			require.NoError(t, os.WriteFile(configPath, broken, 0o644))

			binDir := writeFakeOpencode(t, home, c.version)

			out, exitCode := runMacScript(t, script, home, binDir)
			require.NotEqual(t, 0, exitCode, "坏配置必须失败退出，输出:\n%s", out)

			after, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Equal(t, broken, after, "失败时不能改动用户配置")
			assert.Contains(t, gw.callbackStatuses(), "failed")
		})
	}
}
