package handler_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// 视频任务的提示词（request_body）同样只对平台 admin 下发；
// 状态/分辨率等任务字段必须保留，避免负责人看不了任务本身。
func TestMaskVideoTaskPrompt(t *testing.T) {
	const prompt = `{"model":"doubao-seedance","content":[{"type":"text","text":"一只橘猫"}]}`

	newTask := func() *model.VideoTask {
		return &model.VideoTask{TaskID: "cgt-1", Status: "succeeded", RequestBody: prompt, VideoURL: "https://example.com/v.mp4"}
	}

	t.Run("非 admin 清空提示词", func(t *testing.T) {
		task := newTask()
		handler.MaskVideoTaskPrompt(task, false)

		if task.RequestBody != "" {
			t.Fatalf("非 admin 的 request_body 应被清空，实际为 %s", task.RequestBody)
		}
		if task.TaskID != "cgt-1" || task.Status != "succeeded" || task.VideoURL == "" {
			t.Fatalf("任务其余字段不应被裁剪：%+v", task)
		}
	})

	t.Run("admin 保留提示词", func(t *testing.T) {
		task := newTask()
		handler.MaskVideoTaskPrompt(task, true)

		if task.RequestBody != prompt {
			t.Fatalf("admin 应保留 request_body，实际为 %s", task.RequestBody)
		}
	})

	t.Run("批量裁剪", func(t *testing.T) {
		tasks := []model.VideoTask{*newTask(), *newTask()}
		handler.MaskVideoTaskPrompts(tasks, false)

		for i, task := range tasks {
			if task.RequestBody != "" {
				t.Fatalf("第 %d 条任务的 request_body 应被清空", i)
			}
		}
	})

	t.Run("nil task 不 panic", func(t *testing.T) {
		handler.MaskVideoTaskPrompt(nil, false)
	})
}
