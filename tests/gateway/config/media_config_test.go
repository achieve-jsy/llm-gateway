package config_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/stretchr/testify/require"
)

// TestMediaConfig_RequirePricingDefault 媒体准入开关（A2）的默认值必须是保守的 true。
//
// 为什么给「配置文件加载」写测试（tests/README.md 一般不建议测 config/）：
// 这个开关决定「无价媒体请求是拒绝还是静默放行」，一旦默认值或 env 键名写错，
// 故障形态是**静默漏计费**（而不是启动报错），属于必须由测试钉死的资金安全约束。
//
// 覆盖：环境变量 > 默认值（YAML 层没有 media.require_pricing，等价于未配置）。
func TestMediaConfig_RequirePricingDefault(t *testing.T) {
	tests := []struct {
		name        string
		description string
		envValue    *string
		want        bool
	}{
		{
			name:        "未设置环境变量 → 默认 true（拒绝无价）",
			description: "配置缺失时必须走保守默认值；false 会让无价媒体请求静默按 0 元放行",
			envValue:    nil,
			want:        true,
		},
		{
			name:        "MEDIA_REQUIRE_PRICING=false → 关闭拒绝（放行 + usage_missing）",
			description: "显式关闭开关用于应急，必须能通过环境变量生效（Viper BindEnv 键名正确）",
			envValue:    strPtr("false"),
			want:        false,
		},
		{
			name:        "MEDIA_REQUIRE_PRICING=true → 显式打开（与默认一致）",
			description: "显式 true 必须仍为 true，不能因为类型转换问题变成 false",
			envValue:    strPtr("true"),
			want:        true,
		},
		{
			name:        "MEDIA_REQUIRE_PRICING 为空串 → 落回默认 true",
			description: "viper 的 getEnv 认为空串等于未设置，空值不得把开关意外翻成 false（危险方向）",
			envValue:    strPtr(""),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != nil {
				t.Setenv("MEDIA_REQUIRE_PRICING", *tt.envValue)
			}
			cfg := config.Load()
			require.Equal(t, tt.want, cfg.Media.RequirePricing,
				"MEDIA_REQUIRE_PRICING 必须按「环境变量 > YAML > 默认值」生效")
		})
	}
}

func strPtr(v string) *string { return &v }
