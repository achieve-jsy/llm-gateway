package observability_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/assert"
)

// TestMediaPriced_AdmissionPredicate 媒体准入判据（A2）的直接单测。
//
// 与 EstimateMediaCost 的区别：后者按「本次实际用量桶」逐桶判定（终态记账），
// 本判据是维度无关的准入判据（准入阶段拿不到真实产物数/时长）。
// 判据必须与记账侧同源，否则会出现「准入判有价、记账判无价」的错配。
func TestMediaPriced_AdmissionPredicate(t *testing.T) {
	tests := []struct {
		name        string
		description string
		pricing     *core.ModelPricing
		want        bool
	}{
		{
			name:        "per_image 正价",
			description: "图像按张计价 → 有价",
			pricing:     &core.ModelPricing{PerImage: obsF64(0.04)},
			want:        true,
		},
		{
			name:        "per_second_output 正价",
			description: "视频按秒计价 → 有价",
			pricing:     &core.ModelPricing{PerSecondOutput: obsF64(0.5)},
			want:        true,
		},
		{
			name:        "只有 token 价",
			description: "终态可回退 token 口径，准入阶段不能误拒（gpt-image 等）",
			pricing:     &core.ModelPricing{InputPerMtok: obsF64(5), OutputPerMtok: obsF64(40)},
			want:        true,
		},
		{
			name:        "媒体价字段存在但为 0（catalog 0 占位）",
			description: "0 不是价，必须判无价，否则会重新引入静默 0 元",
			pricing:     &core.ModelPricing{PerImage: obsF64(0)},
			want:        false,
		},
		{
			name:        "全部媒体价列为 NULL",
			description: "A1 迁移用 NULL 表示无价，必须判无价",
			pricing:     &core.ModelPricing{Currency: "USD"},
			want:        false,
		},
		{
			name:        "负价",
			description: "脏数据（负价）不得当作可计价",
			pricing:     &core.ModelPricing{PerRequest: obsF64(-0.01)},
			want:        false,
		},
		{
			name:        "nil pricing",
			description: "价表查不到（nil）不得 panic，按无价处理",
			pricing:     nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, gwobs.MediaPriced(tt.pricing))
		})
	}
}
