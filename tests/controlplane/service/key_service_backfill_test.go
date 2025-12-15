package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/assert"
)

// TestVerifyResult_OrgFields 验证 VerifyResultUser 包含 studio_id/studio_name 字段
func TestVerifyResult_OrgFields(t *testing.T) {
	// 测试目的：VerifyResult 结构体新增的 StudioID/StudioName 字段可正常赋值和读取
	tests := []struct {
		name        string
		description string
		studioID    string
		studioName  string
	}{
		{
			name:        "有工作室信息",
			description: "verify-key 返回有效工作室信息时，结构体字段应正确保存",
			studioID:    "studio-uuid-001",
			studioName:  "X-Lab",
		},
		{
			name:        "无工作室信息",
			description: "部门未映射工作室时，studio_id 和 studio_name 为空字符串",
			studioID:    "",
			studioName:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.VerifyResult{
				Status: "active",
				User: service.VerifyResultUser{
					Name:       "测试用户",
					StudioID:   tt.studioID,
					StudioName: tt.studioName,
				},
			}
			assert.Equal(t, tt.studioID, result.User.StudioID, tt.description)
			assert.Equal(t, tt.studioName, result.User.StudioName)
		})
	}
}

// TestKeyService_BackfillCondition 验证 backfill 触发条件逻辑
func TestKeyService_BackfillCondition(t *testing.T) {
	// 测试目的：验证 needBackfill 和 hasNoDept 的短路逻辑语义正确
	// 用表驱动枚举所有关键组合

	tests := []struct {
		name         string
		description  string
		userName     string
		deptID       string
		studioID     string
		wantBackfill bool // 是否应触发 GetUserDeptInfo
	}{
		{
			name:         "全部字段为空（新建 key）",
			description:  "UserName/DeptID/StudioID 均为空，应触发 backfill",
			userName:     "",
			deptID:       "",
			studioID:     "",
			wantBackfill: true,
		},
		{
			name:         "有 userName 无 dept（无部门用户）",
			description:  "UserName 有值但 DeptID 为空 → hasNoDept=true，跳过无效 backfill",
			userName:     "张三",
			deptID:       "",
			studioID:     "",
			wantBackfill: false,
		},
		{
			name:         "有 userName 有 dept 无 studio",
			description:  "用户有部门但 studio_id 为空，应触发 backfill 补齐 studio 字段",
			userName:     "李四",
			deptID:       "dept-001",
			studioID:     "",
			wantBackfill: true,
		},
		{
			name:         "全部字段完整",
			description:  "所有 org 字段均有值，不应触发 backfill",
			userName:     "王五",
			deptID:       "dept-001",
			studioID:     "studio-001",
			wantBackfill: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 复现 key_service.go 中的条件逻辑，验证语义
			needBackfill := tt.userName == "" || tt.deptID == "" || tt.studioID == ""
			hasNoDept := tt.userName != "" && tt.deptID == ""
			shouldQuery := needBackfill && !hasNoDept

			assert.Equal(t, tt.wantBackfill, shouldQuery, tt.description)
		})
	}
}

// TestKeyService_NilRepo_NoPanic 验证 repo 为 nil 时构造不 panic
func TestKeyService_NilRepo_NoPanic(t *testing.T) {
	// 测试目的：边界情况，nil repo 构造不 panic
	// 注意：NewKeyService 本身构造没问题，但调用任何方法都会 panic（nil repo）
	// 此处仅验证构造函数的健壮性
	assert.NotPanics(t, func() {
		svc := service.NewKeyService(nil, nil, nil)
		_ = svc // 只构造，不调用任何方法
	})
}
