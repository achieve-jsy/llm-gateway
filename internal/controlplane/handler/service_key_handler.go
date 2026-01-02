package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ServiceKeyHandler 处理程序服务 Key（Service Key）的创建、列表、吊销。
type ServiceKeyHandler struct {
	keySvc *service.KeyService
	rbac   *rbac.Checker
}

func NewServiceKeyHandler(keySvc *service.KeyService, rbacChecker *rbac.Checker) *ServiceKeyHandler {
	return &ServiceKeyHandler{keySvc: keySvc, rbac: rbacChecker}
}

// Create POST /api/v1/service-keys
// 仅部门负责人（dept_admin）或平台 admin 可创建。
// 明文 key 仅此次返回，落库存 hash+加密串。
func (h *ServiceKeyHandler) Create(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		DeptID  string `json:"dept_id" binding:"required"`
		Name    string `json:"name" binding:"required"`
		Purpose string `json:"purpose"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// RBAC 校验：只能为自己管辖的部门创建
	if !actor.IsGlobal && !actor.HasDept(req.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权为该部门创建 Service Key"})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	k, plainKey, err := h.keySvc.CreateServiceKey(c.Request.Context(), actorID, req.DeptID, req.Name, req.Purpose)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         k.ID,
		"key":        plainKey, // 明文仅此一次
		"key_type":   k.KeyType,
		"masked_key": k.KeyPrefix + "****" + k.KeyHint,
		"dept_id":    k.DeptID,
		"dept_name":  k.DeptName,
		"purpose":    k.Purpose,
		"created_by": k.CreatedBy,
	})
}

// List GET /api/v1/service-keys?dept_id=xxx
// 列出 Service Key（不含明文）。
// - admin/studio_admin：dept_id 可选，不传时列出所有可见范围的 Key
// - dept_admin：dept_id 可选，不传时使用 JWT user_dept_id
func (h *ServiceKeyHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	deptID := c.Query("dept_id")

	// 非全局管理员：用 actor.DeptIDs 限制可见范围
	// allowedDepts 为 nil 时表示不加 dept 过滤（全局 admin）
	var allowedDepts []string
	if !actor.IsGlobal {
		allowedDepts = actor.DeptIDs
		if len(allowedDepts) == 0 {
			c.JSON(http.StatusOK, gin.H{"items": []struct{}{}, "total": 0, "page": 1, "page_size": 20})
			return
		}
		// 明确指定 dept_id 时，校验是否在允许范围内
		if deptID != "" {
			found := false
			for _, d := range allowedDepts {
				if d == deptID {
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门的 Service Key"})
				return
			}
		}
	}

	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	items, total, err := h.keySvc.ListServiceKeysPaged(c.Request.Context(), deptID, allowedDepts, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// Toggle PATCH /api/v1/service-keys/:id/status
// 启用或停用 Service Key（部门负责人或 admin）。
func (h *ServiceKeyHandler) Toggle(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该 Service Key"})
		return
	}

	if err := h.keySvc.ToggleServiceKey(c.Request.Context(), keyID, req.Enabled); err != nil {
		if err == service.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": req.Enabled})
}

// Delete DELETE /api/v1/service-keys/:id
// 吊销 Service Key（部门负责人或 admin）。
func (h *ServiceKeyHandler) Delete(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	// 先取 key 的归属部门，再做 RBAC 校验
	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权吊销该 Service Key"})
		return
	}

	if err := h.keySvc.DeleteServiceKey(c.Request.Context(), keyID); err != nil {
		if err == service.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
