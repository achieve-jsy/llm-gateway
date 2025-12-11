package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/redis/go-redis/v9"
)

const (
	roleCacheKeyPrefix = "org:role:"
	roleCacheTTL       = 60 * time.Second
)

var (
	ErrForbidden     = errors.New("无权操作该组织范围")
	ErrRoleNotFound  = errors.New("角色记录不存在")
	ErrUserNotFound  = errors.New("用户不存在")
	ErrInvalidScope  = errors.New("scope_id 指向不存在的组织节点")
)

// RoleService RBAC 角色派发与鉴权服务
type RoleService struct {
	roleRepo *repository.RoleRepo
	rdb      *redis.Client
}

// NewRoleService 创建 RoleService
func NewRoleService(roleRepo *repository.RoleRepo, rdb *redis.Client) *RoleService {
	return &RoleService{roleRepo: roleRepo, rdb: rdb}
}

// CheckScope 检查 operatorID 是否有权操作 targetScope
// 返回 nil 表示通过，返回 ErrForbidden 表示越权
func (s *RoleService) CheckScope(ctx context.Context, operatorID string, target model.TargetScope) error {
	roles, err := s.getRoles(ctx, operatorID)
	if err != nil {
		return fmt.Errorf("获取角色失败: %w", err)
	}

	for _, r := range roles {
		switch r.Role {
		case model.RoleAdmin:
			return nil // admin 全通过
		case model.RoleStudioAdmin:
			if target.StudioID != "" && target.StudioID == r.ScopeID {
				return nil
			}
		case model.RoleDeptAdmin:
			if target.DeptID != "" && target.DeptID == r.ScopeID {
				return nil
			}
		}
	}
	return ErrForbidden
}

// Grant 派发角色（幂等）
// isGlobalAdmin 表示操作者是系统管理员（JWT user_role=admin），跳过管辖域检查
func (s *RoleService) Grant(ctx context.Context, operatorID string, operatorTarget model.TargetScope, role *model.UserRole, isGlobalAdmin bool) error {
	if !isGlobalAdmin {
		if err := s.CheckScope(ctx, operatorID, operatorTarget); err != nil {
			return err
		}
	}
	// 派发者只能授予 <= 自己级别的角色
	if role.GrantedBy == "" {
		role.GrantedBy = operatorID
	}
	if role.ActorID == "" {
		role.ActorID = operatorID
	}
	if err := s.roleRepo.Grant(role); err != nil {
		return err
	}
	// 主动失效缓存
	s.invalidateCache(ctx, role.UserID)
	return nil
}

// Revoke 卸任角色
// isGlobalAdmin 表示操作者是系统管理员（JWT user_role=admin），跳过管辖域检查
func (s *RoleService) Revoke(ctx context.Context, operatorID string, operatorTarget model.TargetScope, userID string, role model.RoleType, scopeID string, isGlobalAdmin bool) error {
	if !isGlobalAdmin {
		if err := s.CheckScope(ctx, operatorID, operatorTarget); err != nil {
			return err
		}
	}
	if err := s.roleRepo.Revoke(userID, role, scopeID); err != nil {
		return err
	}
	s.invalidateCache(ctx, userID)
	return nil
}

// GetUserRoles 查询用户所有角色
func (s *RoleService) GetUserRoles(ctx context.Context, userID string) ([]model.UserRole, error) {
	return s.getRoles(ctx, userID)
}

// getRoles 带 Redis 缓存的角色查询
func (s *RoleService) getRoles(ctx context.Context, userID string) ([]model.UserRole, error) {
	cacheKey := roleCacheKeyPrefix + userID

	// 1. 尝试读缓存
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, cacheKey).Result()
		if err == nil {
			var roles []model.UserRole
			if jsonErr := json.Unmarshal([]byte(val), &roles); jsonErr == nil {
				return roles, nil
			}
		}
	}

	// 2. 缓存未命中，查 PG
	roles, err := s.roleRepo.GetByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 3. 写缓存（Redis 不可用时降级，不影响主流程）
	if s.rdb != nil {
		data, _ := json.Marshal(roles)
		if setErr := s.rdb.Set(ctx, cacheKey, data, roleCacheTTL).Err(); setErr != nil {
			slog.WarnContext(ctx, "[orgservice] failed to set role cache", "user_id", userID, "error", setErr)
		}
	}
	return roles, nil
}

// invalidateCache 主动失效用户角色缓存
func (s *RoleService) invalidateCache(ctx context.Context, userID string) {
	if s.rdb == nil {
		return
	}
	cacheKey := roleCacheKeyPrefix + userID
	if err := s.rdb.Del(ctx, cacheKey).Err(); err != nil {
		slog.WarnContext(ctx, "[orgservice] failed to invalidate role cache", "user_id", userID, "error", err)
	}
}
