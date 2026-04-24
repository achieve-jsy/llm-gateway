package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
)

type VideoTaskHandler struct {
	svc *service.VideoTaskService
}

func NewVideoTaskHandler(svc *service.VideoTaskService) *VideoTaskHandler {
	return &VideoTaskHandler{svc: svc}
}

// CreateRequest gateway 内部写入视频任务的请求体
type CreateVideoTaskRequest struct {
	TaskID       string `json:"task_id" binding:"required"`
	APIKeyID     string `json:"api_key_id"`
	APIKeyName   string `json:"api_key_name"`
	APIKeyType   string `json:"api_key_type"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	Model        string `json:"model" binding:"required"`
	ProviderName string `json:"provider_name" binding:"required"`
	ProviderType string `json:"provider_type" binding:"required"`
	Status       string `json:"status"`
	RequestBody  string `json:"request_body"`
}

// UpdateVideoTaskRequest gateway 内部增量更新任务状态和详情字段。
type UpdateVideoTaskRequest struct {
	Status       *string `json:"status"`
	VideoURL     *string `json:"video_url"`
	ResponseBody *string `json:"response_body"`
	ErrorMessage *string `json:"error_message"`
}

// List 任务列表（管理面，可按 user_id 筛选）
func (h *VideoTaskHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	providerName := c.Query("provider_name")
	modelName := c.Query("model_name")
	status := c.Query("status")
	userID := c.Query("user_id")

	tasks, total, err := h.svc.ListScoped(page, pageSize, providerName, modelName, status, userID, videoTaskScopeFromContext(c))
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

// MyList 当前登录用户的视频任务
func (h *VideoTaskHandler) MyList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	providerName := c.Query("provider_name")
	modelName := c.Query("model_name")
	status := c.Query("status")
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tasks, total, err := h.svc.List(page, pageSize, providerName, modelName, status, userID)
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
	task, err := h.svc.GetByIDScoped(taskID, videoTaskScopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// MyGet 当前登录用户的视频任务详情。
func (h *VideoTaskHandler) MyGet(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	taskID := c.Param("id")
	task, err := h.svc.GetByIDAndUserID(taskID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// Stats 统计数据
func (h *VideoTaskHandler) Stats(c *gin.Context) {
	start, end, userID, providerName, modelName := statsFilters(c)
	stats, err := h.svc.StatsScoped(start, end, userID, providerName, modelName, videoTaskScopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// MyStats 当前登录用户的视频统计数据
func (h *VideoTaskHandler) MyStats(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	start, end, _, providerName, modelName := statsFilters(c)
	stats, err := h.svc.Stats(start, end, userID, providerName, modelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// StatsTrend 视频任务按天趋势
func (h *VideoTaskHandler) StatsTrend(c *gin.Context) {
	start, end, userID, providerName, modelName := statsFilters(c)
	rows, err := h.svc.StatsTrendScoped(start, end, userID, providerName, modelName, videoTaskScopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// MyStatsTrend 当前登录用户的视频任务按天趋势
func (h *VideoTaskHandler) MyStatsTrend(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	start, end, _, providerName, modelName := statsFilters(c)
	rows, err := h.svc.StatsTrend(start, end, userID, providerName, modelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// StatsDistribution 视频任务分布统计
func (h *VideoTaskHandler) StatsDistribution(c *gin.Context) {
	start, end, userID, providerName, modelName := statsFilters(c)
	rows, err := h.svc.StatsDistributionScoped(start, end, userID, providerName, modelName, videoTaskScopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// MyStatsDistribution 当前登录用户的视频任务分布统计
func (h *VideoTaskHandler) MyStatsDistribution(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	start, end, _, providerName, modelName := statsFilters(c)
	rows, err := h.svc.StatsDistribution(start, end, userID, providerName, modelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func statsFilters(c *gin.Context) (start, end, userID, providerName, modelName string) {
	start = firstQuery(c, "start", "from")
	end = firstQuery(c, "end", "to")
	userID = c.Query("user_id")
	providerName = c.Query("provider_name")
	modelName = c.Query("model_name")
	return
}

func firstQuery(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			return value
		}
	}
	return ""
}

func videoTaskScopeFromContext(c *gin.Context) service.VideoTaskScope {
	actor := rbac.GetActor(c)
	if actor.IsGlobal {
		return service.VideoTaskScope{IsGlobal: true}
	}
	if len(actor.StudioIDs) > 0 || len(actor.DeptIDs) > 0 {
		return service.VideoTaskScope{StudioIDs: actor.StudioIDs, DeptIDs: actor.DeptIDs}
	}
	return service.VideoTaskScope{UserID: c.GetString("user_id")}
}

// InternalCreate 供 gateway 写入 video_tasks（X-Internal-Token）
func (h *VideoTaskHandler) InternalCreate(c *gin.Context) {
	var req CreateVideoTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "submitted"
	}
	now := time.Now().UTC()
	task := &model.VideoTask{
		TaskID:       req.TaskID,
		APIKeyID:     req.APIKeyID,
		APIKeyName:   req.APIKeyName,
		APIKeyType:   req.APIKeyType,
		UserID:       req.UserID,
		UserName:     req.UserName,
		Model:        req.Model,
		ProviderName: req.ProviderName,
		ProviderType: req.ProviderType,
		Status:       status,
		RequestBody:  req.RequestBody,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.svc.Create(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// InternalUpdate 供 gateway 增量更新任务状态和详情字段。
func (h *VideoTaskHandler) InternalUpdate(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id required"})
		return
	}
	var req UpdateVideoTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fields := service.VideoTaskUpdateFields{}
	if req.Status != nil {
		fields.Status = strings.TrimSpace(*req.Status)
	}
	if req.VideoURL != nil {
		fields.VideoURL = strings.TrimSpace(*req.VideoURL)
	}
	if req.ResponseBody != nil {
		fields.ResponseBody = *req.ResponseBody
	}
	if req.ErrorMessage != nil {
		fields.ErrorMessage = strings.TrimSpace(*req.ErrorMessage)
	}
	if err := h.svc.Update(taskID, fields); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// InternalUpdateStatus 兼容旧的 /status 路由。
func (h *VideoTaskHandler) InternalUpdateStatus(c *gin.Context) {
	h.InternalUpdate(c)
}

// InternalGetProviderType 供 gateway 查询任务对应的 provider_type（并附带 model，供 call_logs）
func (h *VideoTaskHandler) InternalGetProviderType(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id required"})
		return
	}
	meta, err := h.svc.GetTaskMeta(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider_type": meta.ProviderType,
		"model":         meta.Model,
		"provider_name": meta.ProviderName,
	})
}
