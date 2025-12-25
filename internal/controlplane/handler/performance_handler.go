package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

type PerformanceHandler struct {
	svc *service.PerformanceService
}

func NewPerformanceHandler(svc *service.PerformanceService) *PerformanceHandler {
	return &PerformanceHandler{svc: svc}
}

func (h *PerformanceHandler) GetPerformance(c *gin.Context) {
	window := c.DefaultQuery("window", "1h")
	view := c.DefaultQuery("view", "group")
	search := c.Query("search")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if window != "1h" && window != "6h" && window != "24h" {
		window = "1h"
	}
	if view != "group" && view != "model" {
		view = "group"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	result, err := h.svc.GetPerformance(window, view, search, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get performance data"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
