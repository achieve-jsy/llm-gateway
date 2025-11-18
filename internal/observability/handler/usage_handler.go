package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/observability/service"
)

// UsageHandler 用量与成本查询接口
type UsageHandler struct {
	svc *service.RollupService
}

// NewUsageHandler 创建 UsageHandler
func NewUsageHandler(svc *service.RollupService) *UsageHandler {
	return &UsageHandler{svc: svc}
}

// Query GET /api/v1/usage — 聚合用量查询
func (h *UsageHandler) Query(c *gin.Context) {
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	params := service.UsageDailyQueryParams{
		From:    from,
		To:      to,
		GroupBy: c.DefaultQuery("group_by", "day"),
		KeyID:   c.Query("key_id"),
		DeptID:  c.Query("dept_id"),
		Model:   c.Query("model"),
	}
	resp, err := h.svc.Query(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Logs GET /api/v1/usage/logs — 调用明细查询
func (h *UsageHandler) Logs(c *gin.Context) {
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	params := service.LogsQueryParams{
		From:     from,
		To:       to,
		KeyID:    c.Query("key_id"),
		DeptID:   c.Query("dept_id"),
		Model:    c.Query("model"),
		Page:     page,
		PageSize: pageSize,
	}
	resp, err := h.svc.QueryLogs(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// parseDateRange 解析并校验日期范围参数
func parseDateRange(c *gin.Context) (from, to time.Time, ok bool) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required (YYYY-MM-DD)"})
		return
	}
	var err error
	from, err = time.Parse("2006-01-02", fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date"})
		return
	}
	to, err = time.Parse("2006-01-02", toStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date"})
		return
	}
	if from.After(to) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must be before or equal to to"})
		return
	}
	if to.Sub(from) > 90*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date range must not exceed 90 days"})
		return
	}
	ok = true
	return
}