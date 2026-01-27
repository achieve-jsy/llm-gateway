package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditLogHandler 管理面审计日志查询入口
type AuditLogHandler struct {
	auditSvc *service.AuditLogService
	rbac     *rbac.Checker
}

func NewAuditLogHandler(auditSvc *service.AuditLogService, rbacChecker *rbac.Checker) *AuditLogHandler {
	return &AuditLogHandler{auditSvc: auditSvc, rbac: rbacChecker}
}

// List GET /api/v1/audit-logs
// 查询参数：
//
//	user_id, action, resource_type, dept_id, from (RFC3339), to (RFC3339), page, page_size
func (h *AuditLogHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	deptID := c.Query("dept_id")
	var deptIDs []string
	if !actor.IsGlobal {
		if deptID != "" {
			if !actor.HasDept(deptID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门的审计日志"})
				return
			}
		} else {
			deptIDs = actor.DeptIDs
			if len(deptIDs) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "您没有被授权查看任何部门的审计日志"})
				return
			}
		}
	}

	page := 1
	pageSize := 50
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			pageSize = n
		}
	}

	var userID *uuid.UUID
	if uid := c.Query("user_id"); uid != "" {
		if u, err := uuid.Parse(uid); err == nil {
			userID = &u
		}
	}
	action := c.Query("action")
	resourceType := c.Query("resource_type")

	var from, to *time.Time
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = &t
		}
	}

	offset := (page - 1) * pageSize
	items, total, err := h.auditSvc.List(offset, pageSize, userID, action, resourceType, deptID, deptIDs, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
