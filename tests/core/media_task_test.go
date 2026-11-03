// Package core_test 测试 internal/core 的异步媒体任务契约类型（MediaStatus / MediaKind / MediaAction）。
// 这些字面量是对外契约（OpenAI 词表），本文件锁定它们，防止无意改动。
package core_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
)

// fakeMediaTaskProvider 实现 core.MediaTaskProvider，用于编译期校验接口形状。
type fakeMediaTaskProvider struct{}

func (f *fakeMediaTaskProvider) Submit(_ context.Context, _ *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	return &core.MediaSubmitResult{VendorTaskID: "vendor-1", Status: core.MediaStatusQueued}, nil
}

func (f *fakeMediaTaskProvider) Query(_ context.Context, _ string) (*core.MediaTaskStatus, error) {
	return &core.MediaTaskStatus{Status: core.MediaStatusCompleted}, nil
}

// 编译期断言：MediaTaskProvider 必须可以由外部包实现（适配器在 providers/<vendor>/ 下实现它）。
var _ core.MediaTaskProvider = (*fakeMediaTaskProvider)(nil)

// 测试目的：对外状态词表必须与设计文档 §2.1 的 OpenAI 词表逐字一致（改了就是破坏性变更）。
func TestMediaStatus_Vocabulary(t *testing.T) {
	tests := []struct {
		name        string
		description string
		got         string
		want        string
	}{
		{name: "queued", description: "对外已受理状态必须是 queued（OpenAI 语义）", got: string(core.MediaStatusQueued), want: "queued"},
		{name: "in_progress", description: "对外处理中状态必须是 in_progress", got: string(core.MediaStatusInProgress), want: "in_progress"},
		{name: "completed", description: "对外成功终态必须是 completed", got: string(core.MediaStatusCompleted), want: "completed"},
		{name: "failed", description: "对外失败终态必须是 failed", got: string(core.MediaStatusFailed), want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got, tt.description)
		})
	}
}

// 测试目的：IsValid 只接受归一后的词表；上游原始词表（videos 后端/厂商词）不得被判为合法。
func TestMediaStatus_IsValid(t *testing.T) {
	tests := []struct {
		name        string
		description string
		status      core.MediaStatus
		want        bool
	}{
		{name: "queued", description: "queued 属于对外词表", status: core.MediaStatusQueued, want: true},
		{name: "in_progress", description: "in_progress 属于对外词表", status: core.MediaStatusInProgress, want: true},
		{name: "completed", description: "completed 属于对外词表", status: core.MediaStatusCompleted, want: true},
		{name: "failed", description: "failed 属于对外词表", status: core.MediaStatusFailed, want: true},
		{name: "expired 不是对外词表", description: "expired 是存储层状态，对外必须归一为 failed，因此 IsValid 返回 false", status: core.MediaStatus("expired"), want: false},
		{name: "上游 Kling 词 succeed 不合法", description: "上游原始词表未归一（succeed）时不得被判为合法，避免直接透传给客户端", status: core.MediaStatus("succeed"), want: false},
		{name: "空串不合法", description: "零值状态不得被当成合法状态", status: core.MediaStatus(""), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsValid(), tt.description)
		})
	}
}

// 测试目的：终态判定决定「是否停止轮询 / 是否记账」，必须只有 completed 与 failed 为终态。
func TestMediaStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name        string
		description string
		status      core.MediaStatus
		want        bool
	}{
		{name: "completed 是终态", description: "completed 到达后不再推进，调用方可停止轮询并记账", status: core.MediaStatusCompleted, want: true},
		{name: "failed 是终态", description: "failed 到达后不再推进且不记账", status: core.MediaStatusFailed, want: true},
		{name: "queued 不是终态", description: "queued 仍需继续轮询", status: core.MediaStatusQueued, want: false},
		{name: "in_progress 不是终态", description: "in_progress 仍需继续轮询", status: core.MediaStatusInProgress, want: false},
		{name: "未知值不是终态", description: "未知/空值不得被当作终态（否则会静默停止推进）", status: core.MediaStatus("whatever"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsTerminal(), tt.description)
		})
	}
}

// 测试目的：MediaKind 与 MediaAction 的枚举值是对外/存储契约（media_tasks.kind/action）。
func TestMediaKindAndAction_IsValid(t *testing.T) {
	tests := []struct {
		name        string
		description string
		got         bool
		want        bool
	}{
		{name: "kind=image", description: "image 是受支持的媒体类别", got: core.MediaKindImage.IsValid(), want: true},
		{name: "kind=video", description: "video 是受支持的媒体类别", got: core.MediaKindVideo.IsValid(), want: true},
		{name: "kind=audio 未支持", description: "audio 尚未纳入词表，IsValid 返回 false", got: core.MediaKind("audio").IsValid(), want: false},
		{name: "action=text_to_video", description: "文生视频动作合法", got: core.MediaActionTextToVideo.IsValid(), want: true},
		{name: "action=image_to_video", description: "图生视频动作合法", got: core.MediaActionImageToVideo.IsValid(), want: true},
		{name: "action=first_tail_to_video", description: "首尾帧生视频动作合法", got: core.MediaActionFirstTailToVideo.IsValid(), want: true},
		{name: "action=reference_to_video", description: "参考生视频动作合法", got: core.MediaActionReferenceToVideo.IsValid(), want: true},
		{name: "action=text2video 非法", description: "厂商原始动作名（text2video）不是归一动作，必须由适配器映射", got: core.MediaAction("text2video").IsValid(), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got, tt.description)
		})
	}
}
