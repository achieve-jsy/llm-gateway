package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

type VideoTaskHandler struct {
	svc *service.VideoTaskService
}

func NewVideoTaskHandler(svc *service.VideoTaskService) *VideoTaskHandler {
	return &VideoTaskHandler{svc: svc}
}

// List 任务列表
func (h *VideoTaskHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	providerName := c.Query("provider_name")
	modelName := c.Query("model_name")
	status := c.Query("status")

	tasks, total, err := h.svc.List(page, pageSize, providerName, modelName, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      tasks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Get 任务详情
func (h *VideoTaskHandler) Get(c *gin.Context) {
	taskID := c.Param("id")
	task, err := h.svc.GetByID(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// Stats 统计数据
func (h *VideoTaskHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}
