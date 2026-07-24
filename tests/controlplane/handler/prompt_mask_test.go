package handler_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// 视频任务的提示词正文同样只对 admin 或任务本人下发；
// 状态 / 分辨率 / 视频地址等任务字段必须保留，避免负责人看不了任务本身。
func TestMaskVideoTaskPrompt(t *testing.T) {
	const prompt = `{"model":"doubao-seedance","content":[{"type":"text","text":"一只橘猫"}]}`

	newTask := func(userID string) *model.VideoTask {
		return &model.VideoTask{
			TaskID:      "cgt-" + userID,
			UserID:      userID,
			Status:      "succeeded",
			RequestBody: prompt,
			VideoURL:    "https://example.com/v.mp4",
		}
	}

	t.Run("canView=false 清空提示词，其余字段保留", func(t *testing.T) {
		task := newTask("user-2")
		handler.MaskVideoTaskPrompt(task, false)

		if task.RequestBody != "" {
			t.Fatalf("request_body 应被清空，实际为 %s", task.RequestBody)
		}
		if task.TaskID == "" || task.UserID != "user-2" || task.Status != "succeeded" || task.VideoURL == "" {
			t.Fatalf("任务其余字段不应被裁剪：%+v", task)
		}
	})

	t.Run("canView=true 保留提示词", func(t *testing.T) {
		task := newTask("user-1")
		handler.MaskVideoTaskPrompt(task, true)

		if task.RequestBody != prompt {
			t.Fatalf("应保留 request_body，实际为 %s", task.RequestBody)
		}
	})

	t.Run("列表按归属逐条判定：只看得到自己的提示词", func(t *testing.T) {
		tasks := []model.VideoTask{*newTask("user-1"), *newTask("user-2"), *newTask("user-1")}
		handler.MaskVideoTaskPrompts(tasks, func(owner string) bool { return owner == "user-1" })

		if tasks[0].RequestBody != prompt {
			t.Fatal("自己的任务应保留提示词")
		}
		if tasks[1].RequestBody != "" {
			t.Fatal("别人的任务应清空提示词")
		}
		if tasks[2].RequestBody != prompt {
			t.Fatal("自己的任务应保留提示词")
		}
	})

	t.Run("nil task 不 panic", func(t *testing.T) {
		handler.MaskVideoTaskPrompt(nil, false)
	})
}
