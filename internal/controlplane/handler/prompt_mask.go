package handler

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// MaskVideoTaskPrompt 在 canView 为 false 时清空单条视频任务的提示词正文。
//
// 可见性由调用方用 rbac.CanViewPrompt 判定：平台 admin 可见全部，
// 其余角色只能看自己的任务，别人的提示词不下发。
// 只裁剪提示词：状态、分辨率、时长、视频地址、错误信息等字段保持原样。
func MaskVideoTaskPrompt(task *model.VideoTask, canView bool) {
	if task == nil || canView {
		return
	}
	task.RequestBody = ""
}

// MaskVideoTaskPrompts 逐条裁剪列表内的提示词正文。
// canViewFor 接收任务归属 user_id，返回该归属对当前请求者是否可见。
func MaskVideoTaskPrompts(tasks []model.VideoTask, canViewFor func(ownerUserID string) bool) {
	for i := range tasks {
		MaskVideoTaskPrompt(&tasks[i], canViewFor(tasks[i].UserID))
	}
}
