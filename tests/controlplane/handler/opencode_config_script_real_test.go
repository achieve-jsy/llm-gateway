package handler_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真实 E2E 需要真 bundle：V1 用插件契约版，V2 用另一份（契约不兼容）。
const (
	realOpenCodeAssetV1 = "../../../internal/controlplane/handler/assets/opencode-models-discovery.js"
	realOpenCodeAssetV2 = "../../../internal/controlplane/handler/assets/opencode-models-discovery-v2.js"
)

// TestRealOpenCodeEndToEnd 测什么：用**真实** opencode 二进制跑完整链路——脚本安装 →
// opencode 真的加载这份配置 → 模型列表里出现本网关的模型。
// 默认跳过：需要显式提供二进制路径（CI 里没有 opencode）。
//
//	OPENCODE_E2E_V1_BIN=$(command -v opencode)  # 1.x
//	OPENCODE_E2E_V2_BIN=/path/to/opencode2 2.x
//	go test ./tests/controlplane/handler -run TestRealOpenCodeEndToEnd -v -timeout 10m
func TestRealOpenCodeEndToEnd(t *testing.T) {
	cases := []struct {
		name    string
		envKey  string
		wantKey string
		// V2 默认连"后台常驻服务"（固定端口），并发/多次运行会端口打架，
		// 所以测试里用 --standalone 起私有服务，跑完即退。
		extraArgs []string
		// pluginAsset 是对应版本下发的插件 bundle（V1/V2 契约不兼容，不能混用）。
		pluginAsset string
		// dynamicDiscovery 为 true 时追加验证：装完之后网关新增模型，重开 opencode 能自动看到，
		// 不需要重跑安装脚本。V1 插件本来就是这个行为，V2 只有装了 V2 插件才成立。
		dynamicDiscovery bool
	}{
		{"V1 二进制 + 插件动态发现", "OPENCODE_E2E_V1_BIN", "provider", nil, realOpenCodeAssetV1, true},
		{"V2 二进制 + 插件动态发现", "OPENCODE_E2E_V2_BIN", "providers", []string{"--standalone"}, realOpenCodeAssetV2, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := os.Getenv(c.envKey)
			if bin == "" {
				t.Skipf("未设置 %s，跳过真实二进制 E2E", c.envKey)
			}
			bin, err := exec.LookPath(bin)
			require.NoError(t, err, "找不到 opencode 二进制")

			pluginBody, err := os.ReadFile(c.pluginAsset)
			require.NoError(t, err, "读取内嵌插件 bundle 失败")

			gw := newFakeGateway(t)
			gw.PluginBody = pluginBody
			f := tokenFixture{BaseURL: gw.BaseURL(), APIKey: "sk-e2e"}
			r, _ := newScriptEngine(t, f)
			code, script := fetchScript(t, r, "mac", "")
			require.Equal(t, 200, code)

			home := t.TempDir()
			out, exitCode := runMacScript(t, script, home, filepath.Dir(bin))
			require.Equal(t, 0, exitCode, "安装脚本应成功退出，输出:\n%s", out)

			cfg := readOpencodeConfig(t, home)
			require.NotNil(t, dig(cfg, c.wantKey, "biangfeng-gateway"), "配置形状不对")

			// 真正交给 opencode 去读：模型列表里应出现本网关的模型
			runModels := func() string {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, bin, append(append([]string{}, c.extraArgs...), "models")...)
				cmd.Env = mergedEnv(t, map[string]string{
					"HOME": home,
					"PATH": filepath.Dir(bin) + ":/usr/bin:/bin:/usr/sbin:/sbin",
				})
				raw, err := cmd.CombinedOutput()
				require.NoError(t, err, "opencode models 失败，输出:\n%s", raw)
				return string(raw)
			}

			list := runModels()
			assert.True(t, strings.Contains(list, "biangfeng-gateway/middle-level"),
				"opencode 没有列出本网关的模型，输出:\n%s", list)
			assert.True(t, strings.Contains(list, "biangfeng-gateway/vision-pro"), "vision-pro 应出现")
			assert.False(t, strings.Contains(list, "biangfeng-gateway/embed-small"),
				"embedding 模型不应出现在会话模型列表")

			if !c.dynamicDiscovery {
				return
			}
			// 关键回归：网关后台新增模型后，只重启 opencode（不重跑安装脚本）就应该能看到。
			gw.SetModels([]string{"middle-level", "vision-pro", "embed-small", "plain-chat", "hot-added-model"})
			list = runModels()
			assert.True(t, strings.Contains(list, "biangfeng-gateway/hot-added-model"),
				"网关新增的模型没有自动发现（插件没生效），输出:\n%s", list)
		})
	}
}
