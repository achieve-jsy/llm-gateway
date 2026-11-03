package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// ChannelMonitorServiceInterface defines the service methods needed by the admin handler.
type ChannelMonitorServiceInterface interface {
	Create(ctx context.Context, params service.ChannelMonitorCreateParams) (*service.ChannelMonitorView, error)
	Update(ctx context.Context, id int64, params service.ChannelMonitorUpdateParams) (*service.ChannelMonitorView, error)
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*service.ChannelMonitorView, error)
	List(ctx context.Context, params service.ChannelMonitorListParams) ([]*service.ChannelMonitorView, int64, error)
	RunCheck(ctx context.Context, monitorID int64) ([]service.ChannelCheckResult, error)
	GetHistory(ctx context.Context, monitorID int64, modelName string, limit int) ([]*model.ChannelMonitorHistory, error)
	CreateTemplate(ctx context.Context, t *model.ChannelMonitorRequestTemplate) error
	UpdateTemplate(ctx context.Context, t *model.ChannelMonitorRequestTemplate) error
	DeleteTemplate(ctx context.Context, id int64) error
	GetTemplate(ctx context.Context, id int64) (*model.ChannelMonitorRequestTemplate, error)
	ListTemplates(ctx context.Context, provider string, page, pageSize int) ([]*model.ChannelMonitorRequestTemplate, int64, error)
}

// ChannelMonitorHandler handles admin CRUD for channel monitors.
type ChannelMonitorHandler struct {
	svc ChannelMonitorServiceInterface
}

// NewChannelMonitorHandler creates a new ChannelMonitorHandler.
func NewChannelMonitorHandler(svc ChannelMonitorServiceInterface) *ChannelMonitorHandler {
	return &ChannelMonitorHandler{svc: svc}
}

// ── Request types ──────────────────────────────────────────────────────────────

type createMonitorReq struct {
	Name             string                 `json:"name" binding:"required"`
	Provider         string                 `json:"provider" binding:"required"`
	APIMode          string                 `json:"api_mode"`
	Endpoint         string                 `json:"endpoint" binding:"required"`
	APIKey           string                 `json:"api_key" binding:"required"`
	PrimaryModel     string                 `json:"primary_model" binding:"required"`
	ExtraModels      []string               `json:"extra_models"`
	GroupName        string                 `json:"group_name"`
	Enabled          bool                   `json:"enabled"`
	IntervalSeconds  int                    `json:"interval_seconds"`
	TemplateID       *int64                 `json:"template_id"`
	ExtraHeaders     map[string]string      `json:"extra_headers"`
	BodyOverrideMode string                 `json:"body_override_mode"`
	BodyOverride     map[string]interface{} `json:"body_override"`
}

type updateMonitorReq struct {
	Name             *string                 `json:"name"`
	Provider         *string                 `json:"provider"`
	APIMode          *string                 `json:"api_mode"`
	Endpoint         *string                 `json:"endpoint"`
	APIKey           *string                 `json:"api_key"`
	PrimaryModel     *string                 `json:"primary_model"`
	ExtraModels      *[]string               `json:"extra_models"`
	GroupName        *string                 `json:"group_name"`
	Enabled          *bool                   `json:"enabled"`
	IntervalSeconds  *int                    `json:"interval_seconds"`
	TemplateID       *int64                  `json:"template_id"`
	ClearTemplate    bool                    `json:"clear_template"`
	ExtraHeaders     *map[string]string      `json:"extra_headers"`
	BodyOverrideMode *string                 `json:"body_override_mode"`
	BodyOverride     *map[string]interface{} `json:"body_override"`
}

type createTemplateReq struct {
	Name             string                 `json:"name" binding:"required"`
	Provider         string                 `json:"provider" binding:"required"`
	APIMode          string                 `json:"api_mode"`
	Description      string                 `json:"description"`
	ExtraHeaders     map[string]string      `json:"extra_headers"`
	BodyOverrideMode string                 `json:"body_override_mode"`
	BodyOverride     map[string]interface{} `json:"body_override"`
}

type updateTemplateReq struct {
	Name             *string                 `json:"name"`
	Provider         *string                 `json:"provider"`
	APIMode          *string                 `json:"api_mode"`
	Description      *string                 `json:"description"`
	ExtraHeaders     *map[string]string      `json:"extra_headers"`
	BodyOverrideMode *string                 `json:"body_override_mode"`
	BodyOverride     *map[string]interface{} `json:"body_override"`
}

// ── Monitor CRUD ──────────────────────────────────────────────────────────────

// List monitors
func (h *ChannelMonitorHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var enabled *bool
	if v := c.Query("enabled"); v != "" {
		b := v == "true"
		enabled = &b
	}

	params := service.ChannelMonitorListParams{
		Page:     page,
		PageSize: pageSize,
		Provider: c.Query("provider"),
		Enabled:  enabled,
		Search:   c.Query("search"),
	}

	items, total, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取监控列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Create monitor
func (h *ChannelMonitorHandler) Create(c *gin.Context) {
	var req createMonitorReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	_, userName := extractOperator(c)
	params := service.ChannelMonitorCreateParams{
		Name:             req.Name,
		Provider:         req.Provider,
		APIMode:          req.APIMode,
		Endpoint:         req.Endpoint,
		APIKey:           req.APIKey,
		PrimaryModel:     req.PrimaryModel,
		ExtraModels:      req.ExtraModels,
		GroupName:        req.GroupName,
		Enabled:          req.Enabled,
		IntervalSeconds:  req.IntervalSeconds,
		CreatedBy:        userName,
		TemplateID:       req.TemplateID,
		ExtraHeaders:     req.ExtraHeaders,
		BodyOverrideMode: req.BodyOverrideMode,
		BodyOverride:     req.BodyOverride,
	}

	view, err := h.svc.Create(c.Request.Context(), params)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": view})
}

// Get monitor
func (h *ChannelMonitorHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	view, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": view})
}

// Update monitor
func (h *ChannelMonitorHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	var req updateMonitorReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	params := service.ChannelMonitorUpdateParams{
		Name:             req.Name,
		Provider:         req.Provider,
		APIMode:          req.APIMode,
		Endpoint:         req.Endpoint,
		APIKey:           req.APIKey,
		PrimaryModel:     req.PrimaryModel,
		ExtraModels:      req.ExtraModels,
		GroupName:        req.GroupName,
		Enabled:          req.Enabled,
		IntervalSeconds:  req.IntervalSeconds,
		TemplateID:       req.TemplateID,
		ClearTemplate:    req.ClearTemplate,
		ExtraHeaders:     req.ExtraHeaders,
		BodyOverrideMode: req.BodyOverrideMode,
		BodyOverride:     req.BodyOverride,
	}

	view, err := h.svc.Update(c.Request.Context(), id, params)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": view})
}

// Delete monitor
func (h *ChannelMonitorHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "监控已删除"})
}

// RunCheck triggers a manual check
func (h *ChannelMonitorHandler) RunCheck(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	results, err := h.svc.RunCheck(c.Request.Context(), id)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": results})
}

// GetHistory returns check history for a monitor
func (h *ChannelMonitorHandler) GetHistory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	modelName := c.Query("model")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit < 1 || limit > 1000 {
		limit = 100
	}

	history, err := h.svc.GetHistory(c.Request.Context(), id, modelName, limit)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": history})
}

// ── Template CRUD ──────────────────────────────────────────────────────────────

// ListTemplates returns templates
func (h *ChannelMonitorHandler) ListTemplates(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	items, total, err := h.svc.ListTemplates(c.Request.Context(), c.Query("provider"), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取模板列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// CreateTemplate creates a template
func (h *ChannelMonitorHandler) CreateTemplate(c *gin.Context) {
	var req createTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	t := &model.ChannelMonitorRequestTemplate{
		Name:             req.Name,
		Provider:         req.Provider,
		APIMode:          req.APIMode,
		Description:      req.Description,
		ExtraHeaders:     model.JSONMap(req.ExtraHeaders),
		BodyOverrideMode: req.BodyOverrideMode,
		BodyOverride:     model.JSONRaw(req.BodyOverride),
	}
	if t.APIMode == "" {
		t.APIMode = "chat_completions"
	}
	if t.BodyOverrideMode == "" {
		t.BodyOverrideMode = "off"
	}

	if err := h.svc.CreateTemplate(c.Request.Context(), t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "创建模板失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": t})
}

// GetTemplate returns a template
func (h *ChannelMonitorHandler) GetTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "模板 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	t, err := h.svc.GetTemplate(c.Request.Context(), id)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": t})
}

// UpdateTemplate updates a template
func (h *ChannelMonitorHandler) UpdateTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "模板 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	t, err := h.svc.GetTemplate(c.Request.Context(), id)
	if err != nil {
		handleMonitorError(c, err)
		return
	}

	var req updateTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Provider != nil {
		t.Provider = *req.Provider
	}
	if req.APIMode != nil {
		t.APIMode = *req.APIMode
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.ExtraHeaders != nil {
		t.ExtraHeaders = model.JSONMap(*req.ExtraHeaders)
	}
	if req.BodyOverrideMode != nil {
		t.BodyOverrideMode = *req.BodyOverrideMode
	}
	if req.BodyOverride != nil {
		t.BodyOverride = model.JSONRaw(*req.BodyOverride)
	}

	if err := h.svc.UpdateTemplate(c.Request.Context(), t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "更新模板失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": t})
}

// DeleteTemplate deletes a template
func (h *ChannelMonitorHandler) DeleteTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "模板 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.DeleteTemplate(c.Request.Context(), id); err != nil {
		handleMonitorError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "模板已删除"})
}

// ── Error handling ─────────────────────────────────────────────────────────────

func handleMonitorError(c *gin.Context, err error) {
	if appErr, ok := err.(interface {
		Error() string
		Code() string
	}); ok {
		code := appErr.Code()
		switch code {
		case "CHANNEL_MONITOR_NOT_FOUND", "CHANNEL_MONITOR_TEMPLATE_NOT_FOUND":
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": err.Error(), "code": code},
			})
		case "CHANNEL_MONITOR_INVALID_PROVIDER", "CHANNEL_MONITOR_INVALID_API_MODE",
			"CHANNEL_MONITOR_INVALID_INTERVAL", "CHANNEL_MONITOR_INVALID_ENDPOINT",
			"CHANNEL_MONITOR_ENDPOINT_SCHEME", "CHANNEL_MONITOR_ENDPOINT_PATH",
			"CHANNEL_MONITOR_ENDPOINT_UNREACHABLE", "CHANNEL_MONITOR_ENDPOINT_PRIVATE":
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
			})
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
			})
		}
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{"type": "internal_error", "message": "服务器内部错误", "code": CodeInternalError},
	})
}