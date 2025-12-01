package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/service"
)

// UsageHandler 用量与成本查询接口
type UsageHandler struct {
	svc *service.RollupService
	db  *gorm.DB
}

// NewUsageHandler 创建 UsageHandler
func NewUsageHandler(svc *service.RollupService, db ...*gorm.DB) *UsageHandler {
	h := &UsageHandler{svc: svc}
	if len(db) > 0 {
		h.db = db[0]
	}
	return h
}

// Trace GET /api/v1/usage/logs/:request_id/trace — 单条请求完整 trace
func (h *UsageHandler) Trace(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "trace storage not configured"})
		return
	}
	rid := c.Param("request_id")
	if rid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id required"})
		return
	}
	var trace obsmodel.CallTrace
	if err := h.db.WithContext(c.Request.Context()).First(&trace, "request_id = ?", rid).Error; err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "trace not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, trace)
}

// Query GET /api/v1/usage — 聚合用量查询
func (h *UsageHandler) Query(c *gin.Context) {
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	params := service.UsageDailyQueryParams{
		From:      from,
		To:        to,
		GroupBy:   c.DefaultQuery("group_by", "day"),
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		DeptName:  c.Query("dept_name"),
		StudioID:  c.Query("studio_id"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.Query(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Options GET /api/v1/usage/options — 返回可用过滤枚举（provider / dept / studio）
func (h *UsageHandler) Options(c *gin.Context) {
	opts, err := h.svc.QueryOptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, opts)
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
		From:      from,
		To:        to,
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		DeptName:  c.Query("dept_name"),
		StudioID:  c.Query("studio_id"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.QueryLogs(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MyQuery GET /api/v1/my/usage — 当前登录用户的聚合用量统计
func (h *UsageHandler) MyQuery(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	params := service.MyStatsQueryParams{
		From:     from,
		To:       to,
		UserID:   userID,
		GroupBy:  c.DefaultQuery("group_by", "day"),
		Model:    c.Query("model_name"), // 前端传 model_name，与 Query 接口对齐
		Page:     page,
		PageSize: pageSize,
	}
	resp, err := h.svc.QueryMyStats(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MyLogs GET /api/v1/my/usage/logs — 当前登录用户的调用明细（user_id 隔离）
func (h *UsageHandler) MyLogs(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	params := service.LogsQueryParams{
		From:      from,
		To:        to,
		UserID:    userID,
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Page:      page,
		PageSize:  pageSize,
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