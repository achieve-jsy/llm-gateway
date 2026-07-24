package handler

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// MaskVideoTaskPrompt 按角色裁剪单条视频任务的提示词正文。
//
// 产品口径与 trace 一致：提示词（request_body）除平台 admin（isGlobal=true）外一律不下发；
// 任务记录本身正常展示（状态、分辨率、时长、视频地址、错误信息等不受影响）。
func MaskVideoTaskPrompt(task *model.VideoTask, isGlobal bool) {
	if task == nil || isGlobal {
		return
	}
	task.RequestBody = ""
}

// MaskVideoTaskPrompts 批量版本，就地清空列表内每条任务的提示词正文。
func MaskVideoTaskPrompts(tasks []model.VideoTask, isGlobal bool) {
	if isGlobal {
		return
	}
	for i := range tasks {
		tasks[i].RequestBody = ""
	}
}
