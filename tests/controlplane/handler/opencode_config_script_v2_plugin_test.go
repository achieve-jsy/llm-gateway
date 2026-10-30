package handler_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这一组用例覆盖 OpenCode 2.x 的「动态发现插件」：V2 换了插件契约，所以下发的是另一份
// bundle（opencode-models-discovery-v2.js）+ 同目录 sidecar（opencode-models-discovery.json）。
// 真正把插件跑起来验证「网关新增模型能自动出现」的用例在 opencode_config_script_real_test.go。

const (
	v2PluginFile   = "opencode-models-discovery-v2.js"
	v1PluginFile   = "opencode-models-discovery.js"
	pluginSidecar  = "opencode-models-discovery.json"
	pluginDirRel   = ".config/opencode/plugins"
	pluginJSONKeys = 4
)

func readSidecar(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, pluginDirRel, pluginSidecar))
	require.NoError(t, err, "sidecar 未生成")
	var sidecar map[string]any
	require.NoError(t, json.Unmarshal(raw, &sidecar), "sidecar 不是合法 JSON: %s", raw)
	return sidecar
}

// v2Bundle 读真实下发的 V2 插件 bundle：用例要断言「装的是 V2 版」，不能拿假插件凑数。
func v2Bundle(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../internal/controlplane/handler/assets/" + v2PluginFile)
	require.NoError(t, err, "读取 V2 bundle 失败")
	return raw
}

func TestMacScript_V2InstallsDiscoveryPlugin(t *testing.T) {
	gw := newFakeGateway(t)
	gw.PluginBody = v2Bundle(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-e2e"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "mac", "v2")
	require.Equal(t, 200, code)

	home := t.TempDir()
	out, exitCode := runMacScript(t, script, home)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	pluginPath := filepath.Join(home, pluginDirRel, v2PluginFile)
	raw, err := os.ReadFile(pluginPath)
	require.NoError(t, err, "V2 插件文件未生成")
	assert.Contains(t, string(raw), "llmgateway.models-discovery", "下发的应该是 V2 版插件")

	sidecar := readSidecar(t, home)
	assert.Len(t, sidecar, pluginJSONKeys, "sidecar 字段数不对: %v", sidecar)
	assert.Equal(t, "biangfeng-gateway", sidecar["providerID"])
	assert.Equal(t, gw.BaseURL(), sidecar["baseURL"])
	assert.Equal(t, "sk-e2e", sidecar["apiKey"])
	assert.Equal(t, "测试供应商", sidecar["providerName"])

	// V1 插件在 2.x 里不会加载，装 V2 时应顺手清掉
	assert.NoFileExists(t, filepath.Join(home, pluginDirRel, v1PluginFile), "V2 模式不该留 V1 插件")
	assert.Contains(t, out, "V2 版已安装", "输出应说明插件已装（否则用户不知道能自动发现）")
}

func TestMacScript_V2ThenV1RemovesV2Plugin(t *testing.T) {
	gw := newFakeGateway(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-e2e"}
	r, _ := newScriptEngine(t, f)
	home := t.TempDir()

	code, v2Script := fetchScript(t, r, "mac", "v2")
	require.Equal(t, 200, code)
	out, exitCode := runMacScript(t, v2Script, home)
	require.Equal(t, 0, exitCode, out)
	require.FileExists(t, filepath.Join(home, pluginDirRel, v2PluginFile))

	// 换成 V1 再跑一遍：V2 的插件与 sidecar 都要清掉，否则 V1 也会去加载它
	code, v1Script := fetchScript(t, r, "mac", "v1")
	require.Equal(t, 200, code)
	out, exitCode = runMacScript(t, v1Script, home)
	require.Equal(t, 0, exitCode, out)

	assert.NoFileExists(t, filepath.Join(home, pluginDirRel, v2PluginFile), "切回 V1 应删掉 V2 插件")
	assert.NoFileExists(t, filepath.Join(home, pluginDirRel, pluginSidecar), "切回 V1 应删掉 V2 sidecar")
	assert.FileExists(t, filepath.Join(home, pluginDirRel, v1PluginFile))
}

func TestWinScript_V2InstallsDiscoveryPlugin(t *testing.T) {
	gw := newFakeGateway(t)
	gw.PluginBody = v2Bundle(t)
	f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-e2e"}
	r, _ := newScriptEngine(t, f)
	code, script := fetchScript(t, r, "windows", "v2")
	require.Equal(t, 200, code)

	profile := t.TempDir()
	out, exitCode := runWinScript(t, script, profile)
	require.Equal(t, 0, exitCode, "脚本应成功退出，输出:\n%s", out)

	pluginPath := filepath.Join(profile, pluginDirRel, v2PluginFile)
	raw, err := os.ReadFile(pluginPath)
	require.NoError(t, err, "V2 插件文件未生成，脚本输出:\n%s", out)
	assert.Contains(t, string(raw), "llmgateway.models-discovery")

	sidecar := readSidecar(t, profile)
	assert.Equal(t, "biangfeng-gateway", sidecar["providerID"])
	assert.Equal(t, "sk-e2e", sidecar["apiKey"])
}

// TestGetPlugin_ServesVersionSpecificBundle 守住下发的 bundle 与请求的版本对得上：
// V1/V2 插件契约不兼容，发错了只会让插件静默不加载（V2）或直接报错（V1）。
func TestGetPlugin_ServesVersionSpecificBundle(t *testing.T) {
	r, _ := newScriptEngine(t, tokenFixture{BaseURL: "https://gw.example.com/v1", APIKey: "sk-e2e"})

	code, v1Body, v1Version := fetchPlugin(t, r, "")
	require.Equal(t, 200, code)
	code, v2Body, v2Version := fetchPlugin(t, r, "v2")
	require.Equal(t, 200, code)

	assert.NotEqual(t, v1Body, v2Body, "V1/V2 应该是两份不同的 bundle")
	assert.Contains(t, v2Body, "llmgateway.models-discovery", "V2 bundle 没下发对")
	assert.NotContains(t, v1Body, "llmgateway.models-discovery", "V1 bundle 不该含 V2 标记")
	assert.Equal(t, v1Version, v2Version, "同一发包版本下两份 bundle 版本号应一致")

	// 未知/大小写不同的 ver 要退化成 V1，不能 500
	code, unknownBody, _ := fetchPlugin(t, r, "whatever")
	require.Equal(t, 200, code)
	assert.Equal(t, v1Body, unknownBody)
	code, upperBody, _ := fetchPlugin(t, r, "V2")
	require.Equal(t, 200, code)
	assert.Equal(t, v2Body, upperBody, "ver 大小写不该影响结果")
}
