package handler

import (
	"net/http"
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

	exemption, err := h.svc.CreateExemption(c.Request.Context(), service.CreateExemptionRequest{
		SubjectType: req.SubjectType,
		SubjectID:   req.SubjectID,
		Reason:      req.Reason,
		ExpiresAt:   req.ExpiresAt,
		CreatedBy:   uid,
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
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": err.Error(), "code": CodeBadRequest},
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
	exemptions, err := h.svc.ListActiveExemptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取豁免列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": exemptions})
}
