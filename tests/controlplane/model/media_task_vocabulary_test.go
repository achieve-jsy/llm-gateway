package model_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/core"
)

// TestMediaTaskStatusVocabularyMatchesCore 锁死「控制面存储层词表」与 core 对外词表的一致性。
//
// 背景：model 包**刻意不 import internal/core**（core 的依赖闭包 gjson 不在
// tools/atlas-loader 的 go.sum 里，import 会让 `go run main.go` 编译失败 → Atlas 出不来
// 期望 schema）。因此取值以字面量复制 —— 本测试就是那次复制的护栏：
// 任何人改 core 的词表而忘了同步 model，这里立刻红。
func TestMediaTaskStatusVocabularyMatchesCore(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"queued", model.MediaTaskStatusQueued, string(core.MediaStatusQueued)},
		{"in_progress", model.MediaTaskStatusInProgress, string(core.MediaStatusInProgress)},
		{"completed", model.MediaTaskStatusCompleted, string(core.MediaStatusCompleted)},
		{"failed", model.MediaTaskStatusFailed, string(core.MediaStatusFailed)},
		{"kind_image", model.MediaTaskKindImage, string(core.MediaKindImage)},
		{"kind_video", model.MediaTaskKindVideo, string(core.MediaKindVideo)},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: model=%q core=%q —— 两侧词表已漂移", tc.name, tc.got, tc.want)
		}
	}

	if model.MediaTaskStatusExpired != "expired" {
		t.Errorf("expired 是存储层专有词，必须逐字为 \"expired\"，实际 %q", model.MediaTaskStatusExpired)
	}
	// expired 不在 core 的对外词表里（core.MediaStatus.IsValid 必须拒绝它）——
	// 这正是「对调用方归一为 failed」的前提。
	if core.MediaStatus(model.MediaTaskStatusExpired).IsValid() {
		t.Error("expired 不应出现在 core 的对外状态词表里")
	}
	// 除 expired 外，其余四态都必须被 core 接受。
	for _, s := range []string{
		model.MediaTaskStatusQueued, model.MediaTaskStatusInProgress,
		model.MediaTaskStatusCompleted, model.MediaTaskStatusFailed,
	} {
		if !core.MediaStatus(s).IsValid() {
			t.Errorf("状态 %q 不在 core 的对外词表里", s)
		}
	}
}

func TestCallerMediaTaskStatusNormalizesExpiredToFailed(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{model.MediaTaskStatusExpired, model.MediaTaskStatusFailed},
		{model.MediaTaskStatusQueued, model.MediaTaskStatusQueued},
		{model.MediaTaskStatusInProgress, model.MediaTaskStatusInProgress},
		{model.MediaTaskStatusCompleted, model.MediaTaskStatusCompleted},
		{model.MediaTaskStatusFailed, model.MediaTaskStatusFailed},
		// legacy 词表原样透传（旧链路/看板自行展示，不做隐式映射）
		{"submitted", "submitted"},
		{"running", "running"},
		{"succeeded", "succeeded"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := model.CallerMediaTaskStatus(tc.in); got != tc.want {
			t.Errorf("CallerMediaTaskStatus(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestMediaTaskStatusHelpers(t *testing.T) {
	for _, s := range []string{"queued", "in_progress", "completed", "failed", "expired"} {
		if !model.IsMediaTaskStatus(s) {
			t.Errorf("%q 应属于媒体词表", s)
		}
	}
	for _, s := range []string{"submitted", "running", "succeeded", "", "banana"} {
		if model.IsMediaTaskStatus(s) {
			t.Errorf("%q 不应属于媒体词表", s)
		}
	}

	terminal := map[string]bool{"completed": true, "failed": true, "expired": true}
	for _, s := range []string{"queued", "in_progress", "completed", "failed", "expired", "submitted"} {
		if got := model.IsMediaTaskTerminalStatus(s); got != terminal[s] {
			t.Errorf("IsMediaTaskTerminalStatus(%q)=%v want %v", s, got, terminal[s])
		}
	}

	if !model.IsValidMediaTaskKind("image") || !model.IsValidMediaTaskKind("video") {
		t.Error("image/video 都必须合法")
	}
	for _, k := range []string{"", "audio", "IMAGE", "text"} {
		if model.IsValidMediaTaskKind(k) {
			t.Errorf("kind=%q 不应合法", k)
		}
	}
}

// TestMediaTaskTableNamesRenamed 锁死决策 D 的改名触点（结构体名保持 VideoTask）。
func TestMediaTaskTableNamesRenamed(t *testing.T) {
	if got := (model.VideoTask{}).TableName(); got != "media_tasks" {
		t.Errorf("VideoTask.TableName()=%q want media_tasks", got)
	}
	if got := (model.VideoTaskEvent{}).TableName(); got != "media_task_events" {
		t.Errorf("VideoTaskEvent.TableName()=%q want media_task_events", got)
	}
}
