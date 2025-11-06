package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// HealthCheckHandler 健康检测 Handler
type HealthCheckHandler struct {
	svc *service.HealthCheckService
}

// NewHealthCheckHandler 创建 HealthCheckHandler 实例
func NewHealthCheckHandler(svc *service.HealthCheckService) *HealthCheckHandler {
	return &HealthCheckHandler{svc: svc}
}

// GetConfig GET /api/v1/health-check/config
func (h *HealthCheckHandler) GetConfig(c *gin.Context) {
	config, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": config})
}

// UpdateConfig PUT /api/v1/health-check/config
func (h *HealthCheckHandler) UpdateConfig(c *gin.Context) {
	var input struct {
		Enabled          *bool   `json:"enabled"`
		IntervalSeconds  *int    `json:"interval_seconds"`
		FailureThreshold *int    `json:"failure_threshold"`
		FailureAction    *string `json:"failure_action"`
		CheckProviders   *bool   `json:"check_providers"`
		CheckModels      *bool   `json:"check_models"`
		TimeoutSeconds   *int    `json:"timeout_seconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	config, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}

	if input.Enabled != nil {
		config.Enabled = *input.Enabled
	}
	if input.IntervalSeconds != nil {
		config.IntervalSeconds = *input.IntervalSeconds
	}
	if input.FailureThreshold != nil {
		config.FailureThreshold = *input.FailureThreshold
	}
	if input.FailureAction != nil {
		config.FailureAction = *input.FailureAction
	}
	if input.CheckProviders != nil {
		config.CheckProviders = *input.CheckProviders
	}
	if input.CheckModels != nil {
		config.CheckModels = *input.CheckModels
	}
	if input.TimeoutSeconds != nil {
		config.TimeoutSeconds = *input.TimeoutSeconds
	}

	if err := h.svc.UpdateConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to update config"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": config})
}

// GetLogs GET /api/v1/health-check/logs
func (h *HealthCheckHandler) GetLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	logs, err := h.svc.GetLogs(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get logs"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// GetStatus GET /api/v1/health-check/status
func (h *HealthCheckHandler) GetStatus(c *gin.Context) {
	config, _ := h.svc.GetConfig()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"enabled": config != nil && config.Enabled,
		"running": h.svc.IsRunning(),
	}})
}

// RunNow POST /api/v1/health-check/run
func (h *HealthCheckHandler) RunNow(c *gin.Context) {
	_, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}
	// 异步执行，不阻塞请求
	h.svc.RunNow()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "health check triggered"}})
}
