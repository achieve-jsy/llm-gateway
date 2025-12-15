package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ModelStatusLogHandler 模型状态变更日志 Handler
type ModelStatusLogHandler struct {
	logRepo *repository.ModelStatusLogRepo
}

func NewModelStatusLogHandler(logRepo *repository.ModelStatusLogRepo) *ModelStatusLogHandler {
	return &ModelStatusLogHandler{logRepo: logRepo}
}

// ListModelStatusLogs GET /api/v1/model-status-logs
func (h *ModelStatusLogHandler) ListModelStatusLogs(c *gin.Context) {
	var modelID, providerID *uuid.UUID
	var changeType *string

	if v := c.Query("model_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": "invalid model_id format",
			})
			return
		}
		modelID = &id
	}
	if v := c.Query("provider_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": "invalid provider_id format",
			})
			return
		}
		providerID = &id
	}
	if v := c.Query("change_type"); v != "" {
		changeType = &v
	}

	var startTime, endTime *time.Time
	if v := c.Query("start_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1002,
				"message": "invalid time format",
			})
			return
		}
		startTime = &t
	}
	if v := c.Query("end_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1002,
				"message": "invalid time format",
			})
			return
		}
		endTime = &t
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	logs, total, err := h.logRepo.List(modelID, providerID, changeType, startTime, endTime, offset, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    5001,
			"message": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"items":     logs,
		},
	})
}

// GetModelStatusLogs GET /api/v1/models/:id/status-logs
func (h *ModelStatusLogHandler) GetModelStatusLogs(c *gin.Context) {
	modelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "invalid model_id format",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 50 {
		limit = 10
	}

	logs, err := h.logRepo.ListByModelID(modelID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    5001,
			"message": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    logs,
	})
}