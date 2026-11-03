package handler_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTokenFixture() tokenFixture {
	return tokenFixture{
		ProviderID:   "biangfeng-gateway",
		ProviderName: "测试供应商",
		BaseURL:      "http://127.0.0.1:18099/v1",
		APIKey:       "sk-test",
	}
}

// TestGetScript_RendersBothVersionBranches 测什么：两套模板都必须同时带 V1 与 V2 分支
// 骨架（V1 写 provider + modelsDiscovery，V2 写 providers），并把 token、基址、密钥、
// 供应商名注入进去。
// 期望：脚本自身能按环境决定走哪条分支，且没有模板占位符残留。
func TestGetScript_RendersBothVersionBranches(t *testing.T) {
	cases := []struct {
		name         string
		osType       string
		v1BranchMark string
		v2BranchMark string
	}{
		// V1/V2 各取一个只在对应分支出现的骨架标记，避免断言"providers"这种两处都有的词
		{"mac 脚本", "mac", "MODE=\"v1\"", "isinstance(cfg.get(\"providers\")"},
		{"windows 脚本", "windows", "$MODE = \"v1\"", "PSObject.Properties['providers']"},
	}

	f := testTokenFixture()
	r, _ := newScriptEngine(t, f)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := fetchScript(t, r, c.osType, "")
			require.Equal(t, http.StatusOK, code)

			// V1 分支标记 + V1 专属配置节
			assert.Contains(t, body, c.v1BranchMark)
			assert.Contains(t, body, "modelsDiscovery")
			// V2 分支：原生 providers 形状（标记只在 V2 分支里出现）
			assert.Contains(t, body, c.v2BranchMark)
			assert.Contains(t, body, "aisdk:@ai-sdk/openai-compatible")
			// 注入值（不能出现未渲染的占位符）
			assert.Contains(t, body, f.BaseURL)
			assert.Contains(t, body, f.APIKey)
			assert.Contains(t, body, f.ProviderID)
			assert.NotContains(t, body, "{{.")
		})
	}
}

// TestGetScript_ForceVersion 测什么：?ver= 只接受 v1/v2（大小写不敏感），其它值一律忽略，
// 让脚本回到自动探测。
// 期望：模板里的 FORCE_VERSION 等于规范化后的值；非法值渲染成空串。
func TestGetScript_ForceVersion(t *testing.T) {
	cases := []struct {
		name string
		ver  string
		want string
	}{
		{"不传 ver", "", ""},
		{"强制 v1", "v1", "v1"},
		{"强制 v2", "v2", "v2"},
		{"大写也认", "V2", "v2"},
		{"非法数字忽略", "3", ""},
		{"路径穿越忽略", "../x", ""},
	}

	r, _ := newScriptEngine(t, testTokenFixture())

	for _, osType := range []string{"mac", "windows"} {
		for _, c := range cases {
			t.Run(osType+"/"+c.name, func(t *testing.T) {
				code, body := fetchScript(t, r, osType, c.ver)
				require.Equal(t, http.StatusOK, code)

				// 兼容 "FORCE_VERSION=" 与 "$FORCE_VERSION    = " 两种写法
				re := regexp.MustCompile("FORCE_VERSION\\s*=\\s*\"" + regexp.QuoteMeta(c.want) + "\"")
				assert.True(t, re.MatchString(body),
					"期望 FORCE_VERSION=%q，实际脚本里找不到对应渲染", c.want)
			})
		}
	}
}

// TestGetScript_UnknownToken 测什么：token 不存在（过期/伪造）时不能吐脚本。
// 期望：401 且提示与 token 有关。
func TestGetScript_UnknownToken(t *testing.T) {
	r, mr := newScriptEngine(t, testTokenFixture())
	mr.Del("opencode_cfg_token:" + testScriptToken)

	code, body := fetchScript(t, r, "mac", "")
	require.Equal(t, http.StatusUnauthorized, code)
	assert.True(t, strings.Contains(body, "token"), "401 响应应说明 token 无效，实际: %s", body)
}
