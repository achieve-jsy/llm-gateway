package handler_test

import (
	"testing"

	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"gorm.io/datatypes"
)

// 提示词正文只对 admin 或记录本人下发（canView 由 rbac.CanViewPrompt 判定）；
// 调用记录本身（响应正文等元数据）必须原样保留。
func TestMaskTracePrompt(t *testing.T) {
	prompt := datatypes.JSON([]byte(`{"messages":[{"role":"user","content":"机密提示词"}]}`))
	response := datatypes.JSON([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))

	newTrace := func() *obsmodel.CallTrace {
		return &obsmodel.CallTrace{RequestBody: prompt, ResponseBody: response}
	}

	tests := []struct {
		name           string
		canView        bool
		wantPromptKept bool
	}{
		{name: "admin 或记录本人：保留提示词", canView: true, wantPromptKept: true},
		{name: "他人记录：清空提示词", canView: false, wantPromptKept: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTrace()
			obshandler.MaskTracePrompt(tr, tt.canView)

			if tt.wantPromptKept {
				if len(tr.RequestBody) == 0 {
					t.Fatal("应保留 request_body（提示词）")
				}
			} else if tr.RequestBody != nil {
				t.Fatalf("request_body 应被清空，实际为 %s", tr.RequestBody)
			}
			if len(tr.ResponseBody) == 0 {
				t.Fatal("response_body 不应被裁剪")
			}
		})
	}

	t.Run("nil trace 不 panic", func(t *testing.T) {
		obshandler.MaskTracePrompt(nil, false)
	})
}
