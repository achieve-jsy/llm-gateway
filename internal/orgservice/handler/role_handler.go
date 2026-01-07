package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/example/llm-gateway/internal/orgservice/service"
	"github.com/gin-gonic/gin"
)

// RoleHandler 角色管理 HTTP handler
type RoleHandler struct {
	roleSvc     *service.RoleService
	mappingRepo *repository.MappingRepo
	orgPolicy   policy.OrgPolicy
}

func NewRoleHandler(roleSvc *service.RoleService, mappingRepo *repository.MappingRepo) *RoleHandler {
	return &RoleHandler{roleSvc: roleSvc, mappingRepo: mappingRepo, orgPolicy: policy.OrgPolicy{}}
}

// GrantRole POST /api/v1/org/roles/grant
func (h *RoleHandler) GrantRole(c *gin.Context) {
	var req struct {
		UserID    string          `json:"user_id" binding:"required"`
		Role      model.RoleType  `json:"role" binding:"required"`
		ScopeType model.ScopeType `json:"scope_type" binding:"required"`
		ScopeID   string          `json:"scope_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}

	operatorID, _ := c.Get("user_id")
	opID, _ := operatorID.(string)

	actor := rbac.GetActor(c)
	if !h.orgPolicy.CanGrant(actor, string(req.Role)) {
		c.JSON(http.StatusForbidden, errorResp("org.forbidden", "无权授予该角色", "org.forbidden"))
		return
	}

	// 被授权人重叠检查：若目标用户已持有更高或等级角色，拒绝授予低级角色（避免冗余记录）
	if warnMsg := h.checkRedundantGrant(c, req.UserID, req.Role); warnMsg != "" {
		c.JSON(http.StatusUnprocessableEntity, errorResp("org.redundant_role", warnMsg, "org.redundant_role"))
		return
	}

	// 从请求的目标 scope 推导鉴权用的 TargetScope
	target, err := h.buildGrantTarget(c, req.ScopeType, req.ScopeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_scope", err.Error(), "bad_request"))
		return
	}

	newRole := &model.UserRole{
		UserID:    req.UserID,
		Role:      req.Role,
		ScopeType: req.ScopeType,
		ScopeID:   req.ScopeID,
		GrantedBy: opID,
		ActorID:   opID,
	}

	if err := h.roleSvc.Grant(c.Request.Context(), opID, target, newRole, actor.IsGlobal); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			c.JSON(http.StatusForbidden, errorResp("org.forbidden", err.Error(), "org.forbidden"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": req.UserID, "role": req.Role, "scope_id": req.ScopeID})
}

// RevokeRole POST /api/v1/org/roles/revoke
func (h *RoleHandler) RevokeRole(c *gin.Context) {
	var req struct {
		UserID  string         `json:"user_id" binding:"required"`
		Role    model.RoleType `json:"role" binding:"required"`
		ScopeID string         `json:"scope_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}

	operatorID, _ := c.Get("user_id")
	opID, _ := operatorID.(string)

	actor := rbac.GetActor(c)

	// 从被撤销角色的类型推导鉴权用的 TargetScope
	target, err := h.buildRevokeTarget(c, req.Role, req.ScopeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_scope", err.Error(), "bad_request"))
		return
	}

	if err := h.roleSvc.Revoke(c.Request.Context(), opID, target, req.UserID, req.Role, req.ScopeID, actor.IsGlobal); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			c.JSON(http.StatusForbidden, errorResp("org.forbidden", err.Error(), "org.forbidden"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": req.UserID, "role": "user"})
}

// GetUserRoles GET /api/v1/org/roles/:user_id
func (h *RoleHandler) GetUserRoles(c *gin.Context) {
	userID := c.Param("user_id")
	roles, err := h.roleSvc.GetUserRoles(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "roles": roles})
}

// GetMyRoles GET /api/v1/org/roles/me — 当前登录用户的 org 角色列表
func (h *RoleHandler) GetMyRoles(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errorResp("unauthorized", "not authenticated", "auth"))
		return
	}
	roles, err := h.roleSvc.GetUserRoles(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	// 计算 effective role：admin > studio_admin > dept_admin > user
	jwtRole := c.GetString("user_role")
	effectiveRole := jwtRole
	if jwtRole != "admin" {
		for _, r := range roles {
			switch r.Role {
			case "admin":
				effectiveRole = "admin"
			case "studio_admin":
				if effectiveRole != "admin" {
					effectiveRole = "studio_admin"
				}
			case "dept_admin":
				if effectiveRole != "admin" && effectiveRole != "studio_admin" {
					effectiveRole = "dept_admin"
				}
			}
		}
	}
	// dev 用户没有 user_roles 记录，用 JWT role 构造一条虚拟 org role，
	// 使前端 myDeptId / myStudioId 能正确从 orgRoles 提取 scope_id
	if len(roles) == 0 && jwtRole != "" && jwtRole != "user" {
		roles = []model.UserRole{{
			UserID:    userID,
			Role:      model.RoleType(jwtRole),
			ScopeType: model.ScopeSelf,
			ScopeID:   "",
		}}
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "roles": roles, "effective_role": effectiveRole})
}

// CheckScope POST /internal/org/auth/check-scope
func (h *RoleHandler) CheckScope(c *gin.Context) {
	var req struct {
		OperatorID string            `json:"operator_id" binding:"required"`
		Target     model.TargetScope `json:"target" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}

	err := h.roleSvc.CheckScope(c.Request.Context(), req.OperatorID, req.Target)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"pass": false, "reason": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pass": true})
}

// ─── 内部辅助 ────────────────────────────────────────────────────────────────

// buildGrantTarget 根据请求的目标 scope 构造鉴权所需的 TargetScope
func (h *RoleHandler) buildGrantTarget(c *gin.Context, scopeType model.ScopeType, scopeID string) (model.TargetScope, error) {
	switch scopeType {
	case model.ScopeCompany:
		return model.TargetScope{}, nil
	case model.ScopeStudio:
		return model.TargetScope{StudioID: scopeID}, nil
	case model.ScopeDept:
		studioID, err := h.lookupDeptStudio(c, scopeID)
		if err != nil {
			return model.TargetScope{DeptID: scopeID}, nil
		}
		return model.TargetScope{DeptID: scopeID, StudioID: studioID}, nil
	default:
		return model.TargetScope{}, fmt.Errorf("无效的 scope_type: %s", scopeType)
	}
}

// buildRevokeTarget 根据被撤销角色的类型推导鉴权用的 TargetScope
func (h *RoleHandler) buildRevokeTarget(c *gin.Context, role model.RoleType, scopeID string) (model.TargetScope, error) {
	switch role {
	case model.RoleAdmin:
		return model.TargetScope{}, nil
	case model.RoleStudioAdmin:
		return model.TargetScope{StudioID: scopeID}, nil
	case model.RoleDeptAdmin:
		studioID, err := h.lookupDeptStudio(c, scopeID)
		if err != nil {
			return model.TargetScope{DeptID: scopeID}, nil
		}
		return model.TargetScope{DeptID: scopeID, StudioID: studioID}, nil
	default:
		return model.TargetScope{}, fmt.Errorf("无效的角色类型: %s", role)
	}
}

// lookupDeptStudio 从映射表查询部门所属工作室 ID
func (h *RoleHandler) lookupDeptStudio(c *gin.Context, deptID string) (string, error) {
	if h.mappingRepo == nil {
		return "", fmt.Errorf("mapping repo unavailable")
	}
	m, err := h.mappingRepo.GetByDeptID(deptID)
	if err != nil || m == nil {
		return "", fmt.Errorf("dept %s not in studio map", deptID)
	}
	return m.StudioID, nil
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// checkRedundantGrant 检查是否已存在冗余授权：若目标用户已有更高或相等级别的角色，
// 返回提示文字；无冗余则返回空字符串。
func (h *RoleHandler) checkRedundantGrant(c *gin.Context, targetUserID string, grantRole model.RoleType) string {
	roles, err := h.roleSvc.GetUserRoles(c.Request.Context(), targetUserID)
	if err != nil || len(roles) == 0 {
		return ""
	}
	for _, r := range roles {
		if r.Role == model.RoleAdmin {
			return "该用户已是平台管理员，无需再授予其他角色"
		}
	}
	if grantRole == model.RoleStudioAdmin {
		for _, r := range roles {
			if r.Role == model.RoleStudioAdmin {
				return "该用户在此工作室已具有工作室负责人角色，请勿重复授权"
			}
		}
	}
	return ""
}
