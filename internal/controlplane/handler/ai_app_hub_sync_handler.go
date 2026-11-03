package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// HubSyncService 同步触发依赖的能力。抽成接口便于 handler 单测注入假实现，
// 生产环境传 *service.AIAppHubSyncService。
type HubSyncService interface {
	Sync(ctx context.Context, scope service.SyncScope) (service.HubSyncResult, error)
	Authorize(ctx context.Context, userID, passport string, scope service.SyncScope) (bool, error)
	Status(ctx context.Context) (service.HubSyncStatus, error)
}

type AIAppHubSyncHandler struct {
	svc HubSyncService
}

func NewAIAppHubSyncHandler(svc HubSyncService) *AIAppHubSyncHandler {
	return &AIAppHubSyncHandler{svc: svc}
}

type hubSyncRequest struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

// Sync 手动触发一次同步。scope=all 仅管理员；studio/department 额外允许该工作室负责人。
func (h *AIAppHubSyncHandler) Sync(c *gin.Context) {
	var req hubSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	kind := service.ScopeKind(req.Scope)
	switch kind {
	case service.ScopeAll:
	case service.ScopeDepartment, service.ScopeStudio:
		if req.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope"})
		return
	}
	scope := service.SyncScope{Kind: kind, ID: req.ID}

	allowed, err := h.svc.Authorize(c.Request.Context(), c.GetString("user_id"), c.GetString("user_passport"), scope)
	if err != nil {
		if errors.Is(err, service.ErrScopeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "scope not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authorize failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	result, err := h.svc.Sync(c.Request.Context(), scope)
	if err != nil {
		status, msg := mapSyncError(err)
		if status == http.StatusBadGateway {
			c.JSON(status, gin.H{"error": msg, "detail": err.Error()})
			return
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusOK, result)
}

// mapSyncError 把同步错误翻译成 HTTP 语义。502 保留 detail，
// 前端据此提示"接口中心未授权该路由"。
// Status 管理页同步卡片的数据源：上次同步结果 + 是否正在同步。
// 权限与手动触发一致（scope=all 只给管理员），避免普通用户看到上游错误详情。
func (h *AIAppHubSyncHandler) Status(c *gin.Context) {
	allowed, err := h.svc.Authorize(c.Request.Context(), c.GetString("user_id"), c.GetString("user_passport"),
		service.SyncScope{Kind: service.ScopeAll})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authorize failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	st, err := h.svc.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "status failed"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func mapSyncError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrSyncInProgress):
		return http.StatusConflict, "sync in progress"
	case errors.Is(err, service.ErrScopeNotFound):
		return http.StatusNotFound, "scope not found"
	case errors.Is(err, ehrclient.ErrUnauthorized):
		return http.StatusBadGateway, "ehr unauthorized"
	case errors.Is(err, ehrclient.ErrUpstream):
		return http.StatusBadGateway, "ehr upstream"
	case errors.Is(err, ehrclient.ErrEmptyResult):
		return http.StatusBadGateway, "ehr empty result"
	default:
		return http.StatusInternalServerError, "sync failed"
	}
}
