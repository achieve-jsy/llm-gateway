package handler

import (
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// MaskTracePrompt 按角色裁剪单条 trace 的提示词正文。
//
// 产品口径：提示词（request_body）属敏感内容，除平台 admin（isGlobal=true）外一律不下发；
// 调用记录本身正常展示（响应正文、耗时、token、费用等元数据不受影响）。
// 后端裁剪而非仅前端隐藏，避免非 admin 通过接口直接读到提示词。
func MaskTracePrompt(trace *obsmodel.CallTrace, isGlobal bool) {
	if trace == nil || isGlobal {
		return
	}
	trace.RequestBody = nil
}
