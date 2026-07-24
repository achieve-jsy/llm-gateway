package rbac_test

import (
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
)

// 提示词可见性口径：用户只能看自己的提示词，别人的看不了；平台 admin 例外，可看全部；
// 程序（服务）Key 发起的调用提示词一律不下发（本人也不行），admin 仍例外。
// 管辖范围（dept/studio）只决定「看得到这条记录」，不决定能否看正文。
func TestCanViewPrompt(t *testing.T) {
	tests := []struct {
		name         string
		actor        rbac.Actor
		jwtUserID    string // 未挂 Guard 的 /my/* 路由回退来源
		owner        string
		isProgramKey bool
		want         bool
	}{
		{
			name:  "admin 可看别人的提示词",
			actor: rbac.Actor{UserID: "admin-1", IsGlobal: true},
			owner: "user-9",
			want:  true,
		},
		{
			name:  "普通用户看自己的提示词",
			actor: rbac.Actor{UserID: "user-1"},
			owner: "user-1",
			want:  true,
		},
		{
			name:  "普通用户看别人的提示词",
			actor: rbac.Actor{UserID: "user-1"},
			owner: "user-2",
			want:  false,
		},
		{
			name:         "程序 Key 调用：本人也看不到自己的提示词",
			actor:        rbac.Actor{UserID: "user-1"},
			owner:        "user-1",
			isProgramKey: true,
			want:         false,
		},
		{
			name:         "程序 Key 调用：别人的记录同样看不到",
			actor:        rbac.Actor{UserID: "user-1"},
			owner:        "user-2",
			isProgramKey: true,
			want:         false,
		},
		{
			name:         "程序 Key 调用：admin 仍可看",
			actor:        rbac.Actor{UserID: "admin-1", IsGlobal: true},
			owner:        "user-9",
			isProgramKey: true,
			want:         true,
		},
		{
			name:  "部门负责人看管辖范围内他人记录：记录可见但提示词不可见",
			actor: rbac.Actor{UserID: "dept-admin-1", DeptIDs: []string{"dept-a"}},
			owner: "user-2",
			want:  false,
		},
		{
			name:  "工作室负责人看管辖范围内他人记录：提示词同样不可见",
			actor: rbac.Actor{UserID: "studio-admin-1", StudioIDs: []string{"studio-a"}},
			owner: "user-2",
			want:  false,
		},
		{
			name:      "未挂 Guard 的 /my 路由：JWT user_id 回退，看自己的",
			jwtUserID: "user-1",
			owner:     "user-1",
			want:      true,
		},
		{
			name:      "未挂 Guard 的 /my 路由：JWT user_id 回退，看别人的",
			jwtUserID: "user-1",
			owner:     "user-2",
			want:      false,
		},
		{
			name:         "未挂 Guard 的 /my 路由：程序 Key 调用本人也看不到",
			jwtUserID:    "user-1",
			owner:        "user-1",
			isProgramKey: true,
			want:         false,
		},
		{
			name:  "归属未知（历史数据无 user_id）：fail closed",
			actor: rbac.Actor{UserID: "user-1"},
			owner: "",
			want:  false,
		},
		{
			name:  "请求者身份缺失：fail closed",
			actor: rbac.Actor{},
			owner: "user-1",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.jwtUserID != "" {
				c.Set("user_id", tt.jwtUserID)
			}
			if tt.actor.UserID != "" {
				rbac.SetActor(c, tt.actor)
			}

			if got := rbac.CanViewPrompt(c, tt.owner, tt.isProgramKey); got != tt.want {
				t.Fatalf("CanViewPrompt(owner=%q, isProgramKey=%v) = %v, want %v",
					tt.owner, tt.isProgramKey, got, tt.want)
			}
		})
	}
}
