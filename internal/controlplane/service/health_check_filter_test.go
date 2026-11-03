package service

import (
	"testing"

	"github.com/google/uuid"
)

// 模型级检测必须跳过「已知不可用渠道」下的模型，
// 但对未知渠道保持放行——查询失败时不能把全部模型当成死的。
func TestModelDetectableUnderProvider(t *testing.T) {
	usableID := uuid.New()
	disabledID := uuid.New()
	disabled := uuid.New()

	usable := map[uuid.UUID]bool{
		usableID:   true,
		disabledID: false,
	}

	tests := []struct {
		name       string
		providerID uuid.UUID
		want       bool
	}{
		{"可用渠道下的模型应探测", usableID, true},
		{"禁用渠道下的模型应跳过", disabledID, false},
		{"未知渠道默认放行", disabled, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelDetectableUnderProvider(tc.providerID, usable); got != tc.want {
				t.Fatalf("modelDetectableUnderProvider(%s) = %v, 期望 %v", tc.providerID, got, tc.want)
			}
		})
	}

	// 空映射（渠道查询失败）时必须全部放行
	if !modelDetectableUnderProvider(disabledID, nil) {
		t.Fatal("空映射时应放行，否则一次查询失败会让全部模型停止检测")
	}
}
