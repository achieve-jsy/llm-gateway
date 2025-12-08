package rbac_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
)

var orgPol = policy.OrgPolicy{}

// ─── OrgPolicy.CanChangeMemberDept ───────────────────────────────────────────

func TestOrgPolicy_CanChangeMemberDept(t *testing.T) {
	cases := []struct {
		name            string
		actor           rbac.Actor
		targetStudioID  string
		currentStudioID string
		want            bool
	}{
		{
			name:            "admin 跨工作室调整",
			actor:           rbac.Actor{IsGlobal: true},
			targetStudioID:  "studio-x",
			currentStudioID: "studio-y",
			want:            true,
		},
		{
			name:            "studio_admin 同工作室内调整",
			actor:           rbac.Actor{StudioIDs: []string{"studio-a"}},
			targetStudioID:  "studio-a",
			currentStudioID: "studio-a",
			want:            true,
		},
		{
			name:            "studio_admin 目标工作室不在管辖内",
			actor:           rbac.Actor{StudioIDs: []string{"studio-a"}},
			targetStudioID:  "studio-b",
			currentStudioID: "studio-a",
			want:            false,
		},
		{
			name:            "studio_admin 跨工作室抢人（当前工作室不在管辖）",
			actor:           rbac.Actor{StudioIDs: []string{"studio-a"}},
			targetStudioID:  "studio-a",
			currentStudioID: "studio-b",
			want:            false,
		},
		{
			name:            "studio_admin 调整未分配工作室的成员（currentStudioID 为空）",
			actor:           rbac.Actor{StudioIDs: []string{"studio-a"}},
			targetStudioID:  "studio-a",
			currentStudioID: "",
			want:            true,
		},
		{
			name:            "dept_admin 无权（无 studio 范围）",
			actor:           rbac.Actor{DeptIDs: []string{"dept-1"}},
			targetStudioID:  "studio-a",
			currentStudioID: "",
			want:            false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orgPol.CanChangeMemberDept(tc.actor, tc.targetStudioID, tc.currentStudioID)
			if got != tc.want {
				t.Errorf("CanChangeMemberDept() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── OrgPolicy.CanGrant ───────────────────────────────────────────────────────

func TestOrgPolicy_CanGrant(t *testing.T) {
	cases := []struct {
		name       string
		actor      rbac.Actor
		targetRole string
		want       bool
	}{
		{"admin 可授任意角色", rbac.Actor{IsGlobal: true}, "studio_admin", true},
		{"admin 可授 admin", rbac.Actor{IsGlobal: true}, "admin", true},
		{"studio_admin 只能授 dept_admin", rbac.Actor{StudioIDs: []string{"s1"}}, "dept_admin", true},
		{"studio_admin 不能授 studio_admin", rbac.Actor{StudioIDs: []string{"s1"}}, "studio_admin", false},
		{"普通用户无授权权限", rbac.Actor{UserID: "u1"}, "dept_admin", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orgPol.CanGrant(tc.actor, tc.targetRole)
			if got != tc.want {
				t.Errorf("CanGrant(%q) = %v, want %v", tc.targetRole, got, tc.want)
			}
		})
	}
}

// ─── UsagePolicy.EffectiveScope ───────────────────────────────────────────────

func TestUsagePolicy_EffectiveScope(t *testing.T) {
	pol := policy.UsagePolicy{}

	cases := []struct {
		name       string
		actor      rbac.Actor
		reqScope   string
		reqID      string
		userID     string
		wantScope  string
		wantID     string
		wantRestr  bool
	}{
		{
			name:      "admin 自由使用前端传入的 scope",
			actor:     rbac.Actor{IsGlobal: true},
			reqScope:  "company",
			reqID:     "",
			userID:    "u1",
			wantScope: "company",
			wantID:    "",
			wantRestr: false,
		},
		{
			name:      "studio_admin 请求 ID 在管辖内",
			actor:     rbac.Actor{StudioIDs: []string{"studio-a", "studio-b"}},
			reqScope:  "studio",
			reqID:     "studio-a",
			userID:    "u1",
			wantScope: "studio",
			wantID:    "studio-a",
			wantRestr: true,
		},
		{
			name:      "studio_admin 请求 ID 不在管辖内，自动替换为第一个工作室",
			actor:     rbac.Actor{StudioIDs: []string{"studio-a"}},
			reqScope:  "studio",
			reqID:     "studio-x",
			userID:    "u1",
			wantScope: "studio",
			wantID:    "studio-a",
			wantRestr: true,
		},
		{
			name:      "普通用户只能看自己",
			actor:     rbac.Actor{UserID: "u2"},
			reqScope:  "company",
			reqID:     "",
			userID:    "u2",
			wantScope: "user",
			wantID:    "u2",
			wantRestr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotScope, gotID, gotRestr := pol.EffectiveScope(tc.actor, tc.reqScope, tc.reqID, tc.userID)
			if gotScope != tc.wantScope || gotID != tc.wantID || gotRestr != tc.wantRestr {
				t.Errorf("EffectiveScope() = (%q, %q, %v), want (%q, %q, %v)",
					gotScope, gotID, gotRestr, tc.wantScope, tc.wantID, tc.wantRestr)
			}
		})
	}
}
