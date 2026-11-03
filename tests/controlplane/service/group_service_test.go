package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/assert"
)

func TestNewGroupService_NilRepo(t *testing.T) {
	t.Run("repo为nil时构造不panic", func(t *testing.T) {
		svc := service.NewGroupService(nil)
		assert.NotNil(t, svc)
	})
}

func TestGroupServiceErrors(t *testing.T) {
	tests := []struct {
		name        string
		description string
		err         error
		wantMsg     string
	}{
		{
			name:        "ErrGroupNotFound",
			description: "group not found 错误值存在且消息正确",
			err:         service.ErrGroupNotFound,
			wantMsg:     "group not found",
		},
		{
			name:        "ErrGroupNameExists",
			description: "group name already exists 错误值存在且消息正确",
			err:         service.ErrGroupNameExists,
			wantMsg:     "group name already exists",
		},
		{
			name:        "ErrGroupDeleted",
			description: "group has been deleted 错误值存在且消息正确",
			err:         service.ErrGroupDeleted,
			wantMsg:     "group has been deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.err)
			assert.Equal(t, tt.wantMsg, tt.err.Error())
		})
	}
}

func TestGroupService_FailoverDefaults(t *testing.T) {
	t.Run("默认failover参数", func(t *testing.T) {
		svc := service.NewGroupService(nil)
		defaults := svc.GetFailoverDefaults()
		assert.Equal(t, 2, defaults.MaxSwitches)
		assert.Equal(t, 1, defaults.SameModelRetries)
		assert.Equal(t, 60, defaults.CooldownSeconds)
	})

	t.Run("自定义failover参数", func(t *testing.T) {
		svc := service.NewGroupService(nil)
		svc.SetFailoverDefaults(5, 3, 120)
		defaults := svc.GetFailoverDefaults()
		assert.Equal(t, 5, defaults.MaxSwitches)
		assert.Equal(t, 3, defaults.SameModelRetries)
		assert.Equal(t, 120, defaults.CooldownSeconds)
	})
}

func TestCreateGroupRequest_Structure(t *testing.T) {
	t.Run("请求结构体字段赋值", func(t *testing.T) {
		maxSwitches := 3
		req := service.CreateGroupRequest{
			Name:        "test-group",
			Description: "test desc",
			Strategy:    "round_robin",
			MaxSwitches: &maxSwitches,
		}
		assert.Equal(t, "test-group", req.Name)
		assert.Equal(t, "round_robin", req.Strategy)
		assert.Equal(t, 3, *req.MaxSwitches)
	})
}

func TestUpdateGroupRequest_Structure(t *testing.T) {
	t.Run("更新请求可选字段", func(t *testing.T) {
		name := "updated"
		enabled := false
		req := service.UpdateGroupRequest{
			Name:    &name,
			Enabled: &enabled,
		}
		assert.Equal(t, "updated", *req.Name)
		assert.False(t, *req.Enabled)
	})
}
