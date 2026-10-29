package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

const videoFieldName = "supports_video_input"

func boolPtr(v bool) *bool { return &v }

// TestGroupModelInfoVideoInput 覆盖视频能力位的组级聚合与模型级 override 三态语义。
func TestGroupModelInfoVideoInput(t *testing.T) {
	cases := []struct {
		name    string
		members []*model.Model
		want    bool
	}{
		{
			name: "both support",
			members: []*model.Model{
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: true}},
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: true}},
			},
			want: true,
		},
		{
			name: "one of two supports",
			members: []*model.Model{
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: true}},
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: false}},
			},
			want: false,
		},
		{
			name: "catalog unbound has no support",
			members: []*model.Model{
				{Enabled: boolPtr(true)},
			},
			want: false,
		},
		{
			name: "override turns unsupported catalog on",
			members: []*model.Model{
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: false}, OverrideSupportsVideoInput: boolPtr(true)},
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: true}},
			},
			want: true,
		},
		{
			name: "override turns supported catalog off",
			members: []*model.Model{
				{Enabled: boolPtr(true), Catalog: &model.ModelCatalog{SupportsVideoInput: true}, OverrideSupportsVideoInput: boolPtr(false)},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modelsByID := map[uuid.UUID]*model.Model{}
			members := make([]model.GroupModel, 0, len(tc.members))
			for _, m := range tc.members {
				m.ID = uuid.New()
				modelsByID[m.ID] = m
				members = append(members, model.GroupModel{ModelID: m.ID})
			}

			svc := NewModelInfoService(
				&fakeGroupInfoRepo{groups: []model.Group{{ID: 1, Name: "g", Models: members}}},
				&fakeModelInfoRepo{models: modelsByID},
			)

			result, err := svc.GetGroupModelInfo(nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := result["g"].SupportsVideoInput; got != tc.want {
				t.Fatalf("supports_video_input = %v, want %v", got, tc.want)
			}

			// 跨进程契约：字段必须真的序列化成 supports_video_input，否则网关侧静默读不到。
			raw, err := json.Marshal(result["g"])
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(raw), videoFieldName) {
				t.Fatalf("payload misses %s: %s", videoFieldName, raw)
			}
		})
	}
}
