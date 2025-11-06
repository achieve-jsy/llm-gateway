package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// HealthCheckHandler 健康检测 Handler
type HealthCheckHandler struct {
	svc *service.HealthCheckService
}

// NewHealthCheckHandler 创建 HealthCheckHandler 实例
func NewHealthCheckHandler(svc *service.HealthCheckService) *HealthCheckHandler {
	return &HealthCheckHandler{svc: svc}
}

// GetConfig GET /api/v1/health-check/config — 获取检测配置
func (h *HealthCheckHandler) GetConfig(c *gin.Context) {
	config, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}
	checkMode := config.CheckMode
	if checkMode == "" {
		checkMode = "connectivity"
	}
	// 模型检测独立配置：使用默认值兜底
	modelCheckIntervalSeconds := config.ModelCheckIntervalSeconds
	if modelCheckIntervalSeconds <= 0 {
		modelCheckIntervalSeconds = 300
	}
	modelCheckFailureThreshold := config.ModelCheckFailureThreshold
	if modelCheckFailureThreshold <= 0 {
		modelCheckFailureThreshold = 3
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"interval_minutes":               config.IntervalSeconds / 60,
		"interval_seconds":               config.IntervalSeconds,
		"max_failures":                   config.FailureThreshold,
		"enabled":                        config.Enabled,
		"check_providers":                config.CheckProviders,
		"check_models":                   config.CheckModels,
		"timeout_seconds":                config.TimeoutSeconds,
		"failure_action":                 config.FailureAction,
		"check_mode":                     checkMode,
		"model_check_enabled":            config.ModelCheckEnabled,
		"model_check_interval_minutes":   modelCheckIntervalSeconds / 60,
		"model_check_interval_seconds":   modelCheckIntervalSeconds,
		"model_check_failure_threshold":  modelCheckFailureThreshold,
	}})
}

// UpdateConfig PUT /api/v1/health-check/config — 更新检测配置
func (h *HealthCheckHandler) UpdateConfig(c *gin.Context) {
	var input struct {
		Enabled          *bool   `json:"enabled"`
		IntervalSeconds  *int    `json:"interval_seconds"`
		IntervalMinutes  *int    `json:"interval_minutes"`
		FailureThreshold *int    `json:"failure_threshold"`
		MaxFailures      *int    `json:"max_failures"`
		FailureAction    *string `json:"failure_action"`
		CheckProviders   *bool   `json:"check_providers"`
		CheckModels      *bool   `json:"check_models"`
		CheckMode        *string `json:"check_mode"`
		TimeoutSeconds   *int    `json:"timeout_seconds"`

		// 模型检测独立配置
		ModelCheckEnabled          *bool `json:"model_check_enabled"`
		ModelCheckIntervalMinutes  *int  `json:"model_check_interval_minutes"`
		ModelCheckIntervalSeconds  *int  `json:"model_check_interval_seconds"`
		ModelCheckFailureThreshold *int  `json:"model_check_failure_threshold"`
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
	if input.IntervalMinutes != nil {
		config.IntervalSeconds = *input.IntervalMinutes * 60
	}
	if input.FailureThreshold != nil {
		config.FailureThreshold = *input.FailureThreshold
	}
	if input.MaxFailures != nil {
		config.FailureThreshold = *input.MaxFailures
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
	if input.CheckMode != nil {
		config.CheckMode = *input.CheckMode
	}
	if input.TimeoutSeconds != nil {
		config.TimeoutSeconds = *input.TimeoutSeconds
	}

	// 模型检测独立配置
	if input.ModelCheckEnabled != nil {
		config.ModelCheckEnabled = *input.ModelCheckEnabled
	}
	if input.ModelCheckIntervalSeconds != nil {
		config.ModelCheckIntervalSeconds = *input.ModelCheckIntervalSeconds
	}
	if input.ModelCheckIntervalMinutes != nil {
		config.ModelCheckIntervalSeconds = *input.ModelCheckIntervalMinutes * 60
	}
	if input.ModelCheckFailureThreshold != nil {
		config.ModelCheckFailureThreshold = *input.ModelCheckFailureThreshold
	}

	if err := h.svc.UpdateConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to update config"}})
		return
	}

	checkMode := config.CheckMode
	if checkMode == "" {
		checkMode = "connectivity"
	}
	modelCheckIntervalSeconds := config.ModelCheckIntervalSeconds
	if modelCheckIntervalSeconds <= 0 {
		modelCheckIntervalSeconds = 300
	}
	modelCheckFailureThreshold := config.ModelCheckFailureThreshold
	if modelCheckFailureThreshold <= 0 {
		modelCheckFailureThreshold = 3
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"interval_minutes":               config.IntervalSeconds / 60,
		"interval_seconds":               config.IntervalSeconds,
		"max_failures":                   config.FailureThreshold,
		"enabled":                        config.Enabled,
		"check_providers":                config.CheckProviders,
		"check_models":                   config.CheckModels,
		"timeout_seconds":                config.TimeoutSeconds,
		"failure_action":                 config.FailureAction,
		"check_mode":                     checkMode,
		"model_check_enabled":            config.ModelCheckEnabled,
		"model_check_interval_minutes":   modelCheckIntervalSeconds / 60,
		"model_check_interval_seconds":   modelCheckIntervalSeconds,
		"model_check_failure_threshold":  modelCheckFailureThreshold,
	}})
}

// GetLogs GET /api/v1/health-check/logs — 获取检测记录
func (h *HealthCheckHandler) GetLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	logs, err := h.svc.GetLogs(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get logs"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// GetStatus GET /api/v1/health-check/status — 获取调度器运行状态
func (h *HealthCheckHandler) GetStatus(c *gin.Context) {
	config, _ := h.svc.GetConfig()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"enabled": config != nil && config.Enabled,
		"running": h.svc.IsRunning(),
	}})
}

// RunNow POST /api/v1/health-check/run — 异步触发一次检测
func (h *HealthCheckHandler) RunNow(c *gin.Context) {
	if _, err := h.svc.GetConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}
	h.svc.RunNow()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "health check triggered"}})
}

// CheckAll POST /api/v1/providers/check-all — 批量检测所有启用渠道（同步返回结果）
func (h *HealthCheckHandler) CheckAll(c *gin.Context) {
	result := h.svc.CheckAll(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// CheckOne POST /api/v1/providers/:id/check — 检测单个渠道
func (h *HealthCheckHandler) CheckOne(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid provider id"}})
		return
	}

	result, err := h.svc.CheckOneByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
