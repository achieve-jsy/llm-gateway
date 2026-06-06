package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type QuotaExemptionHandler struct {
	svc     *service.QuotaExemptionService
	auditor auditRecorder
}

func NewQuotaExemptionHandler(svc *service.QuotaExemptionService) *QuotaExemptionHandler {
	return &QuotaExemptionHandler{svc: svc}
}

func (h *QuotaExemptionHandler) SetAuditRecorder(r auditRecorder) {
	h.auditor = r
}

type createExemptionRequest struct {
	SubjectType string    `json:"subject_type" binding:"required"`
	SubjectID   string    `json:"subject_id" binding:"required"`
	Reason      string    `json:"reason"`
	ExpiresAt   time.Time `json:"expires_at" binding:"required"`
}

// Create POST /api/v1/quota/exemptions
func (h *QuotaExemptionHandler) Create(c *gin.Context) {
	var req createExemptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "请求参数无效: " + err.Error(), "code": CodeBadRequest},
		})
		return
	}

	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	// 创建人优先存显示名称（JWT 里的 user_name），便于列表直接回显人名；
	// 兜底用 user_id（旧逻辑）。dev-login 账号不在 users 表，存 UUID 会解析失败显示 UUID。
	userName, _ := c.Get("user_name")
	uname, _ := userName.(string)
	createdBy := uname
	if createdBy == "" {
		createdBy = uid
	}

	exemption, err := h.svc.CreateExemption(c.Request.Context(), service.CreateExemptionRequest{
		SubjectType: req.SubjectType,
		SubjectID:   req.SubjectID,
		Reason:      req.Reason,
		ExpiresAt:   req.ExpiresAt,
		CreatedBy:   createdBy,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	if h.auditor != nil {
		uid, _ := c.Get("user_id")
		uname, _ := c.Get("user_name")
		uidStr, _ := uid.(string)
		unameStr, _ := uname.(string)
		uidUUID, _ := uuid.Parse(uidStr)
		h.auditor.Record(audit.Input{
			UserID: uidUUID, UserName: unameStr,
			Action: audit.ActionQuotaExemptionCreate, ResourceType: audit.ResourceQuota,
			ResourceID: exemption.ID.String(), ClientIP: c.ClientIP(),
		})
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": exemption})
}

// Revoke DELETE /api/v1/quota/exemptions/:id
func (h *QuotaExemptionHandler) Revoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "无效的豁免 ID", "code": CodeBadRequest},
		})
		return
	}

	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	if err := h.svc.RevokeExemption(c.Request.Context(), id, uid); err != nil {
		if errors.Is(err, service.ErrExemptionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "exemption not found", "code": CodeExemptionNotFound},
			})
			return
		}
		// 其他错误（如 DB 故障）不向外泄露内部细节
		slog.Error("revoke quota exemption failed", "exemption_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "撤销豁免失败", "code": CodeInternalError},
		})
		return
	}

	if h.auditor != nil {
		uid, _ := c.Get("user_id")
		uname, _ := c.Get("user_name")
		uidStr, _ := uid.(string)
		unameStr, _ := uname.(string)
		uidUUID, _ := uuid.Parse(uidStr)
		h.auditor.Record(audit.Input{
			UserID: uidUUID, UserName: unameStr,
			Action: audit.ActionQuotaExemptionRevoke, ResourceType: audit.ResourceQuota,
			ResourceID: id.String(), ClientIP: c.ClientIP(),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "豁免已撤销"})
}

// List GET /api/v1/quota/exemptions
func (h *QuotaExemptionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// 解析搜索和筛选参数
	keyword := c.Query("keyword")
	subjectType := c.Query("subject_type")
	status := c.Query("status")

	// 校验 status 合法值，避免非法值被静默当作「不过滤」返回全部记录
	switch status {
	case "", "active", "expiring_soon", "expired", "all":
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "无效的 status 参数", "code": CodeBadRequest},
		})
		return
	}

	exemptions, total, err := h.svc.ListActiveExemptionsWithNames(c.Request.Context(), page, pageSize, service.ListExemptionFilter{
		Keyword:     keyword,
		SubjectType: subjectType,
		Status:      status,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取豁免列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      0,
		"data":      exemptions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
