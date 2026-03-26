package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core/video"
	"github.com/gin-gonic/gin"
)

// VideoHandler 视频生成 Handler
type VideoHandler struct {
	relayAdapter      video.VideoProvider
	volcengineAdapter video.VideoProvider
}

// NewVideoHandler 创建视频 Handler
func NewVideoHandler(relayAdapter, volcengineAdapter video.VideoProvider) *VideoHandler {
	return &VideoHandler{
		relayAdapter:      relayAdapter,
		volcengineAdapter: volcengineAdapter,
	}
}

// selectAdapter 根据请求体中的 model 字段选适配器
// 简单实现：根据 model 是否包含 "seedance" 来选择
// 实际生产环境应从模型注册表查询
func (h *VideoHandler) selectAdapter(model string) video.VideoProvider {
	// 默认使用中转适配器
	return h.relayAdapter
}

// CreateTask 创建视频任务
func (h *VideoHandler) CreateTask(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	// 提取 model 字段
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// 选适配器
	adapter := h.selectAdapter(req.Model)

	// 直通转发
	taskID, err := adapter.CreateTask(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// 原样返回
	c.JSON(http.StatusOK, gin.H{"id": taskID})
}

// GetTask 查询任务状态
func (h *VideoHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")

	// 简单实现：默认用中转适配器查询
	// 实际生产环境应从 video_tasks 表查询渠道
	adapter := h.relayAdapter

	result, err := adapter.GetTask(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}
