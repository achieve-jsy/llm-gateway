package handler

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
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

// healthCheckConfigResponse 把配置转成前端契约格式。
//
// GET 与 PUT 返回同一份结构，集中在这里避免两处各写一遍导致漂移。
// 渠道级检测（interval/max_failures/check_mode/check_providers/...）已移除，
// 这些键不再返回。
func healthCheckConfigResponse(config *model.HealthCheckConfig) gin.H {
	return gin.H{
		"enabled":                          config.Enabled,
		"model_check_enabled":              config.ModelCheckEnabled,
		"model_check_interval_minutes":     config.ModelCheckIntervalSeconds / 60,
		"model_check_interval_seconds":     config.ModelCheckIntervalSeconds,
		"model_check_failure_threshold":    config.ModelCheckFailureThreshold,
		"model_recovery_enabled":           config.ModelRecoveryEnabled,
		"model_recovery_success_threshold": config.ModelRecoverySuccessThreshold,
		"group_check_enabled":              config.GroupCheckEnabled,
		"group_check_interval_minutes":     config.GroupCheckIntervalSeconds / 60,
		"group_check_interval_seconds":     config.GroupCheckIntervalSeconds,
	}
}

// GetConfig GET /api/v1/health-check/config — 获取检测配置
func (h *HealthCheckHandler) GetConfig(c *gin.Context) {
	config, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get config"}})
		return
	}
	// GetConfig 已经过 ApplyDefaults，无需在这里再兜底。
	c.JSON(http.StatusOK, gin.H{"data": healthCheckConfigResponse(config)})
}

// UpdateConfig PUT /api/v1/health-check/config — 更新检测配置
func (h *HealthCheckHandler) UpdateConfig(c *gin.Context) {
	var input struct {
		Enabled *bool `json:"enabled"`

		// 模型检测独立配置
		ModelCheckEnabled          *bool `json:"model_check_enabled"`
		ModelCheckIntervalMinutes  *int  `json:"model_check_interval_minutes"`
		ModelCheckIntervalSeconds  *int  `json:"model_check_interval_seconds"`
		ModelCheckFailureThreshold *int  `json:"model_check_failure_threshold"`

		// 模型组检测独立配置
		GroupCheckEnabled         *bool `json:"group_check_enabled"`
		GroupCheckIntervalMinutes *int  `json:"group_check_interval_minutes"`
		GroupCheckIntervalSeconds *int  `json:"group_check_interval_seconds"`

		// 模型自动恢复配置
		ModelRecoveryEnabled          *bool `json:"model_recovery_enabled"`
		ModelRecoverySuccessThreshold *int  `json:"model_recovery_success_threshold"`
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

	// 模型组检测独立配置
	if input.GroupCheckEnabled != nil {
		config.GroupCheckEnabled = *input.GroupCheckEnabled
	}
	if input.GroupCheckIntervalSeconds != nil {
		config.GroupCheckIntervalSeconds = *input.GroupCheckIntervalSeconds
	}
	if input.GroupCheckIntervalMinutes != nil {
		config.GroupCheckIntervalSeconds = *input.GroupCheckIntervalMinutes * 60
	}

	// 模型自动恢复配置
	if input.ModelRecoveryEnabled != nil {
		config.ModelRecoveryEnabled = *input.ModelRecoveryEnabled
	}
	if input.ModelRecoverySuccessThreshold != nil {
		config.ModelRecoverySuccessThreshold = *input.ModelRecoverySuccessThreshold
	}

	// 最小间隔校验：不低于 60 秒
	if config.ModelCheckIntervalSeconds < 60 {
		config.ModelCheckIntervalSeconds = 60
	}
	if config.GroupCheckIntervalSeconds < 60 {
		config.GroupCheckIntervalSeconds = 60
	}

	if err := h.svc.UpdateConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to update config"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": healthCheckConfigResponse(config)})
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

// GetTimeline GET /api/v1/health-check/timeline — 获取健康监控时间线数据
func (h *HealthCheckHandler) GetTimeline(c *gin.Context) {
	lookback := c.DefaultQuery("lookback", "6h")
	if lookback != "6h" && lookback != "12h" && lookback != "24h" && lookback != "7d" {
		lookback = "6h"
	}
	search := c.Query("search")
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	result, err := h.svc.GetTimeline(lookback, search, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get timeline"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetGroupTimeline GET /api/v1/health-check/group-timeline — 获取按模型组聚合的健康时间线
func (h *HealthCheckHandler) GetGroupTimeline(c *gin.Context) {
	lookback := c.DefaultQuery("lookback", "6h")
	if lookback != "6h" && lookback != "12h" && lookback != "24h" && lookback != "7d" {
		lookback = "6h"
	}
	search := c.Query("search")
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	result, err := h.svc.GetGroupTimeline(lookback, search, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get group timeline"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
