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

// 回归：部门预算池额度（dept_daily / dept_service）属于「部门额度」，由上级按
// 「上级配置下级」原则下发。部门负责人对自己部门的额度只读，扩容须走扩容审批；
// 修复前该判定等价于 HasDept，部门负责人可自行抬高本部门上限。
func TestActor_CanManageDeptBudgetPool(t *testing.T) {
	cases := []struct {
		name   string
		actor  rbac.Actor
		deptID string
		want   bool
	}{
		{
			name:   "平台管理员始终允许",
			actor:  rbac.Actor{IsGlobal: true},
			deptID: "dept-a",
			want:   true,
		},
		{
			name:   "工作室管理员管辖下属部门时允许",
			actor:  rbac.Actor{StudioIDs: []string{"studio-a"}, DeptIDs: []string{"dept-a"}},
			deptID: "dept-a",
			want:   true,
		},
		{
			name:   "工作室管理员管辖范围外拒绝",
			actor:  rbac.Actor{StudioIDs: []string{"studio-a"}, DeptIDs: []string{"dept-a"}},
			deptID: "dept-b",
			want:   false,
		},
		{
			name:   "部门负责人改本部门额度被拒绝",
			actor:  rbac.Actor{Roles: []string{"dept_admin"}, DeptIDs: []string{"dept-a"}, DeptAdminIDs: []string{"dept-a"}},
			deptID: "dept-a",
			want:   false,
		},
		{
			name:   "部门负责人改其他部门额度被拒绝",
			actor:  rbac.Actor{Roles: []string{"dept_admin"}, DeptIDs: []string{"dept-a"}},
			deptID: "dept-b",
			want:   false,
		},
		{
			name:   "无管理角色拒绝",
			actor:  rbac.Actor{UserID: "u1"},
			deptID: "dept-a",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.actor.CanManageDeptBudgetPool(tc.deptID); got != tc.want {
				t.Errorf("CanManageDeptBudgetPool(%q) = %v, want %v", tc.deptID, got, tc.want)
			}
		})
	}
}
