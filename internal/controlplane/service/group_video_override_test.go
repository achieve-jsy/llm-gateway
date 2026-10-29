package service

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// 组级「视频输入」开关的语义：**手动覆盖模式下，组成员跟随组开关**。
//
// 为什么需要它：网关是按成员（scheduler.GroupModel.SupportsVideoInput）在路由期过滤
// 视频请求的，如果只改组级标志而不影响下发到网关的成员标志，管理员打开开关后请求仍会
// 被过滤成"无可用模型"，开关就变成装饰品。
func TestEffectiveMemberVideoInputManualMode(t *testing.T) {
	textOnly := &model.Model{
		Catalog: &model.ModelCatalog{SupportsVideoInput: false},
	}
	video := &model.Model{
		Catalog: &model.ModelCatalog{SupportsVideoInput: true},
	}
	manualOn := &model.Group{IsCapabilityOverride: true, OverrideSupportsVideoInput: true}
	manualOff := &model.Group{IsCapabilityOverride: true, OverrideSupportsVideoInput: false}
	auto := &model.Group{IsCapabilityOverride: false, OverrideSupportsVideoInput: true}

	if !EffectiveMemberVideoInput(manualOn, textOnly) {
		t.Fatal("手动开启后，即使 catalog 声明不支持，成员也应被视为支持视频")
	}
	if EffectiveMemberVideoInput(manualOff, video) {
		t.Fatal("手动关闭后，即使 catalog 声明支持，成员也应被视为不支持")
	}
	// 自动模式下组开关不生效，逐成员按 catalog/override 判定（保守 AND 的前提）
	if !EffectiveMemberVideoInput(auto, video) {
		t.Fatal("自动模式应跟随成员自身的视频能力（true）")
	}
	if EffectiveMemberVideoInput(auto, textOnly) {
		t.Fatal("自动模式应跟随成员自身的视频能力（false）")
	}
}

func TestEffectiveMemberVideoInputNilGuards(t *testing.T) {
	if EffectiveMemberVideoInput(nil, &model.Model{Catalog: &model.ModelCatalog{SupportsVideoInput: true}}) == false {
		t.Fatal("组为空时应退化为成员自身能力")
	}
	if EffectiveMemberVideoInput(&model.Group{IsCapabilityOverride: true, OverrideSupportsVideoInput: true}, nil) {
		t.Fatal("成员为空时不应被判为支持视频")
	}
}

// 组级对外能力（/v1/model/info）也要反映开关，否则插件/前端看到的仍是 AND 聚合结果。
func TestApplyGroupOverrideSetsVideoInput(t *testing.T) {
	info := GroupModelInfo{SupportsVideoInput: false}
	got := applyGroupOverride(info, &model.Group{
		IsCapabilityOverride:       true,
		OverrideSupportsVideoInput: true,
		OverrideSupportsVision:     true,
	})
	if !got.SupportsVideoInput {
		t.Fatal("手动覆盖应把组级 supports_video_input 置为开关值")
	}

	got = applyGroupOverride(GroupModelInfo{SupportsVideoInput: true}, &model.Group{
		IsCapabilityOverride:       true,
		OverrideSupportsVideoInput: false,
	})
	if got.SupportsVideoInput {
		t.Fatal("手动覆盖关闭时应把组级 supports_video_input 置为 false")
	}
}
