package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/example/llm-gateway/internal/orgservice/service"
	"github.com/gin-gonic/gin"
)

// OrgHandler 组织管理 HTTP handler
type OrgHandler struct {
	orgSvc      *service.OrgService
	studioSvc   *service.StudioService
	userOrgRepo *repository.UserOrgRepo
	roleRepo    *repository.RoleRepo
	orgPolicy   policy.OrgPolicy
}

// NewOrgHandler 创建 OrgHandler
func NewOrgHandler(orgSvc *service.OrgService, studioSvc *service.StudioService, userOrgRepo *repository.UserOrgRepo, roleRepo *repository.RoleRepo) *OrgHandler {
	return &OrgHandler{orgSvc: orgSvc, studioSvc: studioSvc, userOrgRepo: userOrgRepo, roleRepo: roleRepo, orgPolicy: policy.OrgPolicy{}}
}

// GetOrgTree GET /internal/org/tree
func (h *OrgHandler) GetOrgTree(c *gin.Context) {
	tree, err := h.orgSvc.GetOrgTree(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// ListStudios GET /api/v1/org/studios
func (h *OrgHandler) ListStudios(c *gin.Context) {
	page, pageSize := parsePagination(c)
	studios, total, err := h.studioSvc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"studios":   studios,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// CreateStudio POST /api/v1/org/studios
func (h *OrgHandler) CreateStudio(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}
	// ID 由数据库 gen_random_uuid() 自动生成，不需要传入
	studio := &model.Studio{Name: req.Name}
	if err := h.studioSvc.Create(c.Request.Context(), studio); err != nil {
		if errors.Is(err, service.ErrStudioDuplicate) {
			c.JSON(http.StatusUnprocessableEntity, errorResp("org.duplicate_name", err.Error(), "org.duplicate_name"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": studio})
}

// GetStudio GET /api/v1/org/studios/:id
func (h *OrgHandler) GetStudio(c *gin.Context) {
	id := c.Param("id")
	studio, err := h.studioSvc.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResp("org.studio.not_found", "工作室不存在", "org.studio.not_found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": studio})
}

// DeleteStudio DELETE /api/v1/org/studios/:id
func (h *OrgHandler) DeleteStudio(c *gin.Context) {
	id := c.Param("id")
	if err := h.studioSvc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrStudioHasDepartments) {
			c.JSON(http.StatusConflict, errorResp("org.studio.has_departments", err.Error(), "org.studio.has_departments"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	// 注意：DeleteStudio 要求工作室下无部门（HasDepartments 校验），
	// 部门移出工作室时 SetDeptStudio("") 已触发 OnDeptStudioChanged 同步，
	// 此处无需再调 OnStudioDeleted。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// SearchUsers GET /api/v1/org/users?keyword=&limit=&studio_id=
// studio_id 不为空时只返回该工作室下的员工，用于指定部门负责人时约束选人范围
func (h *OrgHandler) SearchUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	if keyword == "" {
		c.JSON(http.StatusOK, gin.H{"users": []any{}})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	studioID := c.Query("studio_id")
	users, err := h.userOrgRepo.SearchUsers(keyword, limit, studioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// ListAllRoles GET /api/v1/org/all-roles — 列出管理角色，支持服务端搜索、角色筛选和分页。
func (h *OrgHandler) ListAllRoles(c *gin.Context) {
	actor := rbac.GetActor(c)
	page, pageSize := parsePagination(c)
	scope := h.orgPolicy.RoleListScope(actor)
	roles, total, err := h.roleRepo.ListManagementRolesByActorPaged(scope, repository.RoleListFilter{
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Role:    strings.TrimSpace(c.Query("role")),
	}, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	counts, err := h.roleRepo.ListManagementRoleCountsByActor(scope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"roles":       roles,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"role_counts": counts,
	})
}

// GetDeptMembers GET /api/v1/org/departments/:dept_id/members
// :dept_id 接受 SSO dept_id（前端传入）或 UUID（兼容）
func (h *OrgHandler) GetDeptMembers(c *gin.Context) {
	rawID := c.Param("dept_id")
	actor := rbac.GetActor(c)

	// 先按 SSO dept_id 查；若不存在再按 UUID 查（兼容两种调用方）
	dept, err := h.userOrgRepo.GetDeptBySSO(rawID)
	if err != nil || dept == nil {
		dept, err = h.userOrgRepo.GetDeptByUUID(rawID)
	}
	if err != nil || dept == nil {
		c.JSON(http.StatusNotFound, errorResp("dept_not_found", "部门不存在", "not_found"))
		return
	}
	deptStudioID := ""
	if dept.StudioID != nil {
		deptStudioID = *dept.StudioID
	}

	// 权限校验：使用 SSO dept_id（Actor.DeptIDs 统一存储 SSO dept_id）
	if !h.orgPolicy.CanViewDeptMembers(actor, dept.DeptID, deptStudioID) {
		c.JSON(http.StatusForbidden, errorResp("forbidden", "无权查看该部门成员", "forbidden"))
		return
	}

	page, pageSize := parsePagination(c)
	members, total, err := h.userOrgRepo.ListMembersByDeptID(dept.DeptID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"members":   members,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func parsePagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return
}

func errorResp(errType, message, code string) gin.H {
	return gin.H{"code": code, "message": message, "error": gin.H{"type": errType, "message": message, "code": code}}
}
