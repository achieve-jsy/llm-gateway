package handler

import (
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// MaskTracePrompt 在 canView 为 false 时清空提示词正文 request_body。
//
// 可见性由调用方用 rbac.CanViewPrompt 判定：平台 admin 可见全部，
// 其余角色只能看自己的记录，别人的提示词不下发。
// 只裁剪提示词：response_body 与耗时 / token / 费用等元数据保持原样，
// 且后端裁剪而非仅前端隐藏，避免直接调接口读到别人的提示词。
func MaskTracePrompt(trace *obsmodel.CallTrace, canView bool) {
	if trace == nil || canView {
		return
	}
	trace.RequestBody = nil
}
