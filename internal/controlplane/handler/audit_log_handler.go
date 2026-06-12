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

const (
	// maxAuditTimeSpan 审计查询允许的最大时间跨度（366 天），防止超宽时间范围拖垮 Count/List。
	maxAuditTimeSpan = 366 * 24 * time.Hour
	// maxAuditOffset 审计查询允许的最大分页偏移，兼顾前端 CSV 流式导出与防深分页滥用。
	maxAuditOffset = 200000
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
//	user_id, action, resource_type, dept_id, from (RFC3339), to (RFC3339),
//	search (关键字，匹配 user_name/resource_name/resource_id/client_ip),
//	sort (created_at:asc | created_at:desc), page, page_size
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
	search := c.Query("search")
	sort := c.Query("sort")

	var from, to *time.Time
	if v := c.Query("from"); v != "" {
		from = parseTimeParam(v)
	}
	if v := c.Query("to"); v != "" {
		to = parseTimeParam(v)
	}
	if from != nil && to != nil {
		if to.Before(*from) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "时间范围无效：结束时间不能早于开始时间"})
			return
		}
		if to.Sub(*from) > maxAuditTimeSpan {
			c.JSON(http.StatusBadRequest, gin.H{"error": "时间范围过大：最大支持 366 天"})
			return
		}
	}

	offset := (page - 1) * pageSize
	if offset > maxAuditOffset {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分页偏移过大"})
		return
	}
	items, total, err := h.auditSvc.List(offset, pageSize, userID, action, resourceType, deptID, deptIDs, from, to, search, sort)
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

// parseTimeParam 解析时间查询参数：先按 RFC3339Nano（带时区，兼容可选小数秒，如
// "2024-06-01T10:30:00+08:00" 与 "2024-06-01T00:00:00.000Z"），失败再回退无时区的
// "2006-01-02T15:04:05"（按 UTC 处理），两者都失败返回 nil（忽略该参数）。
func parseTimeParam(v string) *time.Time {
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02T15:04:05", v); err == nil {
		return &t
	}
	return nil
}
