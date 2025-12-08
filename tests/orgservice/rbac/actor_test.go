package rbac_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
)

func TestActor_HasStudio(t *testing.T) {
	cases := []struct {
		name     string
		actor    rbac.Actor
		studioID string
		want     bool
	}{
		{
			name:     "admin 始终返回 true",
			actor:    rbac.Actor{IsGlobal: true},
			studioID: "any-id",
			want:     true,
		},
		{
			name:     "studio_admin 管辖范围内返回 true",
			actor:    rbac.Actor{StudioIDs: []string{"studio-a", "studio-b"}},
			studioID: "studio-a",
			want:     true,
		},
		{
			name:     "studio_admin 管辖范围外返回 false",
			actor:    rbac.Actor{StudioIDs: []string{"studio-a"}},
			studioID: "studio-c",
			want:     false,
		},
		{
			name:     "无权限 actor 返回 false",
			actor:    rbac.Actor{},
			studioID: "studio-a",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.actor.HasStudio(tc.studioID)
			if got != tc.want {
				t.Errorf("HasStudio(%q) = %v, want %v", tc.studioID, got, tc.want)
			}
		})
	}
}

func TestActor_HasDept(t *testing.T) {
	cases := []struct {
		name   string
		actor  rbac.Actor
		deptID string
		want   bool
	}{
		{
			name:   "admin 始终返回 true",
			actor:  rbac.Actor{IsGlobal: true},
			deptID: "dept-1",
			want:   true,
		},
		{
			name:   "dept_admin 直管部门返回 true",
			actor:  rbac.Actor{DeptIDs: []string{"dept-1", "dept-2"}},
			deptID: "dept-1",
			want:   true,
		},
		{
			name:   "studio_admin 展开的部门 ID 也可命中",
			actor:  rbac.Actor{StudioIDs: []string{"studio-a"}, DeptIDs: []string{"dept-3"}},
			deptID: "dept-3",
			want:   true,
		},
		{
			name:   "不在范围内返回 false",
			actor:  rbac.Actor{DeptIDs: []string{"dept-1"}},
			deptID: "dept-99",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.actor.HasDept(tc.deptID)
			if got != tc.want {
				t.Errorf("HasDept(%q) = %v, want %v", tc.deptID, got, tc.want)
			}
		})
	}
}

func TestActor_IsManager(t *testing.T) {
	cases := []struct {
		name  string
		actor rbac.Actor
		want  bool
	}{
		{"admin", rbac.Actor{IsGlobal: true}, true},
		{"studio_admin", rbac.Actor{StudioIDs: []string{"s1"}}, true},
		{"dept_admin", rbac.Actor{DeptIDs: []string{"d1"}}, true},
		{"普通用户", rbac.Actor{UserID: "u1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.actor.IsManager(); got != tc.want {
				t.Errorf("IsManager() = %v, want %v", got, tc.want)
			}
		})
	}
}
