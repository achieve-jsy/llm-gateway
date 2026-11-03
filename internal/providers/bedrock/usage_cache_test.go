package bedrock

import (
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func i32(v int32) *int32 { return &v }

// Bedrock 的 InputTokens 不含缓存命中部分，但 TotalTokens 含。
// 若直接透传 InputTokens，就会出现 prompt+completion ≠ total（xAI on Bedrock 最极端：
// input=0、cacheRead=全部输入），违反 OpenAI 协议并导致计费漏算。
func TestBedrockPromptTokensIncludesCache(t *testing.T) {
	cases := []struct {
		name string
		u    *brtypes.TokenUsage
		want int
	}{
		{
			name: "xAI 全部输入走缓存命中（线上实际观测：p=0 c=38 t=73 cacheRead=35）",
			u:    &brtypes.TokenUsage{InputTokens: i32(0), OutputTokens: i32(38), TotalTokens: i32(73), CacheReadInputTokens: i32(35)},
			want: 35,
		},
		{
			name: "普通命中部分缓存",
			u:    &brtypes.TokenUsage{InputTokens: i32(10), OutputTokens: i32(5), TotalTokens: i32(25), CacheReadInputTokens: i32(10)},
			want: 20,
		},
		{
			name: "无缓存字段（kimi-k3 实测：p=89 c=87 t=176）",
			u:    &brtypes.TokenUsage{InputTokens: i32(89), OutputTokens: i32(87), TotalTokens: i32(176)},
			want: 89,
		},
		{
			name: "cacheRead=0 显式给出",
			u:    &brtypes.TokenUsage{InputTokens: i32(89), CacheReadInputTokens: i32(0)},
			want: 89,
		},
		{
			name: "缓存写入也计入输入",
			u:    &brtypes.TokenUsage{InputTokens: i32(5), CacheWriteInputTokens: i32(100)},
			want: 105,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bedrockPromptTokens(c.u); got != c.want {
				t.Fatalf("bedrockPromptTokens = %d, want %d", got, c.want)
			}
		})
	}
}

// 断言修复后 prompt+completion == total（OpenAI 协议要求）。
func TestBedrockUsageSumsToTotal(t *testing.T) {
	// xAI 场景：total=73 已含 35 个缓存读
	u := &brtypes.TokenUsage{InputTokens: i32(0), OutputTokens: i32(38), TotalTokens: i32(73), CacheReadInputTokens: i32(35)}
	p, c := bedrockPromptTokens(u), int(*u.OutputTokens)
	tt := int(*u.TotalTokens)
	if p+c != tt {
		t.Fatalf("prompt(%d)+completion(%d)=%d != total(%d)", p, c, p+c, tt)
	}
}
