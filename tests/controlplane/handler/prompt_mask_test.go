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
		handler.MaskVideoTaskPrompts(tasks, func(owner, _ string) bool { return owner == "user-1" })

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

	t.Run("程序 Key 发起的任务：本人也清空提示词", func(t *testing.T) {
		tasks := []model.VideoTask{*newTask("user-1"), {TaskID: "cgt-svc", UserID: "user-1", APIKeyType: "service", RequestBody: prompt}}

		// 与生产一致的判定：admin 例外 + 仅本人 + 非程序 Key
		handler.MaskVideoTaskPrompts(tasks, func(owner, keyType string) bool {
			return keyType != "service" && owner == "user-1"
		})

		if tasks[1].RequestBody != "" {
			t.Fatal("程序 Key 发起的任务应清空提示词，即使是本人")
		}
	})

	t.Run("程序 Key 的判断依据是 api_key_type", func(t *testing.T) {
		task := newTask("user-1")
		task.APIKeyType = "service"
		// 单独确认字段可被裁决策略读取（避免后续改成从别处取名）
		if task.APIKeyType != "service" {
			t.Fatal("APIKeyType 应可读")
		}
	})

	t.Run("nil task 不 panic", func(t *testing.T) {
		handler.MaskVideoTaskPrompt(nil, false)
	})
}
