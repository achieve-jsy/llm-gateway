package handler

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// MaskVideoTaskPrompt 在 canView 为 false 时清空单条视频任务的提示词正文。
//
// 可见性由调用方用 rbac.CanViewPrompt 判定：平台 admin 可见全部；
// 其余角色只能看自己的、且非程序（服务）Key 发起任务的提示词。
// 只裁剪提示词：状态、分辨率、时长、视频地址、错误信息等字段保持原样。
func MaskVideoTaskPrompt(task *model.VideoTask, canView bool) {
	if task == nil || canView {
		return
	}
	task.RequestBody = ""
}

// MaskVideoTaskPrompts 逐条裁剪列表内的提示词正文。
// canViewFor 接收任务归属 user_id 与 Key 类型，返回该任务的提示词对当前请求者是否可见。
func MaskVideoTaskPrompts(tasks []model.VideoTask, canViewFor func(ownerUserID, keyType string) bool) {
	for i := range tasks {
		MaskVideoTaskPrompt(&tasks[i], canViewFor(tasks[i].UserID, tasks[i].APIKeyType))
	}
}
