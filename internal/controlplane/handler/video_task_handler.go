package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type VideoTaskHandler struct {
	svc *service.VideoTaskService
}

func NewVideoTaskHandler(svc *service.VideoTaskService) *VideoTaskHandler {
	return &VideoTaskHandler{svc: svc}
}

// CreateVideoTaskRequest gateway 内部写入媒体任务的请求体。
//
// 既有字段保持原样（旧网关 client 逐字段兼容），后面是 C1 delta 与冻结契约的新字段。
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

	// ── C1 delta（冻结契约新增字段）──
	Kind           string        `json:"kind"`            // image|video
	Action         string        `json:"action"`          // 渠道无关的归一动作
	VendorTaskID   string        `json:"vendor_task_id"`  // 上游任务 id（禁止对外暴露）
	Progress       string        `json:"progress"`        // 上游进度透传
	IdempotencyKey string        `json:"idempotency_key"` // 幂等键（与 api_key_id 组合唯一）
	PrivateData    model.JSONRaw `json:"private_data"`    // 禁止对外返回的数据
	DeptID         string        `json:"dept_id"`
	StudioID       string        `json:"studio_id"`
}

// UpdateVideoTaskRequest gateway 内部增量更新任务状态和详情字段。
//
// 指针字段的 nil 语义 = 「不修改」；usage 的 *JSONRaw 亦同（显式传 null 视为不修改）。
type UpdateVideoTaskRequest struct {
	Status       *string `json:"status"`
	VideoURL     *string `json:"video_url"`
	ResponseBody *string `json:"response_body"`
	ErrorMessage *string `json:"error_message"`

	// ── 冻结契约新增字段 ──
	Progress       *string        `json:"progress"`
	Usage          *model.JSONRaw `json:"usage"`
	CostMicroUSD   *int64         `json:"cost_micro_usd"` // 整数微美元，避免浮点
	BilledAt       *time.Time     `json:"billed_at"`      // RFC3339
	PollFailures   *int           `json:"poll_failures"`
	VendorTaskID   *string        `json:"vendor_task_id"`
	ExpectedStatus *string        `json:"expected_status"` // 非空 → 启用 CAS
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

	// 提示词正文只对 admin 或任务本人（且非程序 Key 调用）下发；
	// 管理员面按管辖范围可见任务，但看不到别人的、以及程序 Key 的提示词。
	MaskVideoTaskPrompts(tasks, func(owner, keyType string) bool {
		return rbac.CanViewPrompt(c, owner, keyType == model.KeyTypeService)
	})
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

	// 提示词正文只对 admin 或任务本人（且非程序 Key 调用）下发；
	// 管理员面按管辖范围可见任务，但看不到别人的、以及程序 Key 的提示词。
	MaskVideoTaskPrompts(tasks, func(owner, keyType string) bool {
		return rbac.CanViewPrompt(c, owner, keyType == model.KeyTypeService)
	})
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

	// 提示词正文只对 admin 或任务本人（且非程序 Key 调用）下发。
	MaskVideoTaskPrompt(task, rbac.CanViewPrompt(c, task.UserID, task.APIKeyType == model.KeyTypeService))
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

	// 提示词正文只对 admin 或任务本人（且非程序 Key 调用）下发。
	MaskVideoTaskPrompt(task, rbac.CanViewPrompt(c, task.UserID, task.APIKeyType == model.KeyTypeService))
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

// InternalCreate 供 gateway 写入 media_tasks（必须走 InternalAuth 的 X-Internal-Token）。
//
// 幂等（冻结契约，关键）：若 (api_key_id, idempotency_key) 已存在且 idempotency_key 非空
// → 返回 **200** + 既有任务，**不新建、不打上游**；新建成功返回 **201**。
//
// 响应形状 = 既有 `{ok}` + 新增 `created` / `task`（task 为调用方视图，status 已归一）。
func (h *VideoTaskHandler) InternalCreate(c *gin.Context) {
	var req CreateVideoTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "" && !model.IsValidMediaTaskKind(kind) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid kind: must be image or video"})
		return
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	vendorTaskID := strings.TrimSpace(req.VendorTaskID)
	if len(idempotencyKey) > mediaTaskVarcharLen || len(vendorTaskID) > mediaTaskVarcharLen {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "idempotency_key and vendor_task_id must be at most 191 bytes",
		})
		return
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		// 旧链路（不带 kind）保持历史默认 submitted；新链路（带 kind）默认 queued。
		// 这样 Seedance 看板的 submitted 口径与 sweeper 的扫描范围都不受影响。
		status = "submitted"
		if kind != "" {
			status = model.MediaTaskStatusQueued
		}
	}
	now := time.Now().UTC()
	task := &model.VideoTask{
		TaskID:         req.TaskID,
		APIKeyID:       req.APIKeyID,
		APIKeyName:     req.APIKeyName,
		APIKeyType:     req.APIKeyType,
		UserID:         req.UserID,
		UserName:       req.UserName,
		Model:          req.Model,
		ProviderName:   req.ProviderName,
		ProviderType:   req.ProviderType,
		Status:         status,
		RequestBody:    req.RequestBody,
		Kind:           kind,
		Action:         strings.TrimSpace(req.Action),
		VendorTaskID:   vendorTaskID,
		Progress:       strings.TrimSpace(req.Progress),
		IdempotencyKey: optionalTrimmedString(idempotencyKey),
		PrivateData:    req.PrivateData,
		DeptID:         strings.TrimSpace(req.DeptID),
		StudioID:       strings.TrimSpace(req.StudioID),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, saved, err := h.svc.CreateMediaTask(task)
	if err != nil {
		// 写失败必须响亮失败（网关侧会转 5xx 并放弃本次提交），不得 fire-and-forget。
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	statusCode := http.StatusOK
	if created {
		statusCode = http.StatusCreated
	}
	c.JSON(statusCode, gin.H{"ok": true, "created": created, "task": internalTaskView(saved)})
}

// InternalUpdate 供 gateway 以 CAS 语义增量更新任务状态和详情字段。
//
// 状态机：queued→in_progress→completed|failed|expired，**只前进、终态不回退**；
// legacy 词表行（submitted/running/succeeded）不施加前进限制（向后兼容旧链路）。
// expected_status 非空时启用 CAS，仅当前状态相等才写入；CAS 不匹配不是错误，
// 返回 200 + {updated:false, task:当前快照}，由调用方读 task 决定下一步。
//
// 响应：`{ok, updated: bool, task: {...}}`（status 已对调用方归一，expired → failed）。
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
	fields := service.MediaTaskUpdateFields{
		Usage:        req.Usage,
		CostMicroUSD: req.CostMicroUSD,
		BilledAt:     req.BilledAt,
		PollFailures: req.PollFailures,
	}
	if req.Status != nil {
		fields.Status = strings.TrimSpace(*req.Status)
	}
	if req.ExpectedStatus != nil {
		fields.ExpectedStatus = strings.TrimSpace(*req.ExpectedStatus)
	}
	if req.Progress != nil {
		fields.Progress = strings.TrimSpace(*req.Progress)
	}
	if req.VendorTaskID != nil {
		fields.VendorTaskID = strings.TrimSpace(*req.VendorTaskID)
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

	updated, task, err := h.svc.UpdateMediaTaskCAS(taskID, fields)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "updated": false, "error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": updated, "task": internalTaskView(task)})
}

// InternalUpdateStatus 兼容旧的 /status 路由：**语义逐字不变**（不走 CAS、不走新状态机、
// 不因任务缺失返 404，与改动前的 InternalUpdate 完全一致）。
// 现行网关 client 用的是 PATCH /:id，本端点仅为旧版本调用方保留。
func (h *VideoTaskHandler) InternalUpdateStatus(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id required"})
		return
	}
	// 请求体与字段映射逐字对齐改名前的 InternalUpdate（旧版 /status 就是直接转发给它）：
	// 只认 legacy 四件套，不认 C1 新字段、不做 CAS、不做状态机；成功恒为 200 {"ok":true}。
	var req struct {
		Status       *string `json:"status"`
		VideoURL     *string `json:"video_url"`
		ResponseBody *string `json:"response_body"`
		ErrorMessage *string `json:"error_message"`
	}
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

// InternalGet 按 task_id 读单条全量（网关侧 retrieve 用）。
//
// 返回 `{ok, task}`；task 含 provider_type/kind/action/vendor_task_id/status/progress/
// usage/cost_micro_usd/billed_at/started_at/finished_at/poll_failures 等，
// status 已对调用方归一（expired → failed）。
// private_data 带 json:"-"，任何 HTTP 出口都不会序列化它。
func (h *VideoTaskHandler) InternalGet(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id required"})
		return
	}
	task, err := h.svc.GetMediaTask(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "task": internalTaskView(task)})
}

// mediaTaskVarcharLen 与迁移里的 varchar(191) 对齐（超长在 DB 层会报错，这里提前 400）。
const mediaTaskVarcharLen = 191

// optionalTrimmedString 空串 → nil（写库即 NULL）。
//
// 只用于 idempotency_key：它参与 (api_key_id, idempotency_key) 唯一索引，
// 若把空串写进库，同一个 Key 下所有「不带幂等键」的任务会互相撞唯一键。
func optionalTrimmedString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// internalTaskView 生成**面向调用方**的任务视图：
//   - status 归一（expired → failed，见 model.CallerMediaTaskStatus）；
//   - 不带 events（内部端点不需要时间线，省一次查询）。
//
// 复制而非原地改：绝不能把归一后的状态写回实体（会污染 CAS 的期望状态判定）。
func internalTaskView(task *model.VideoTask) *model.VideoTask {
	if task == nil {
		return nil
	}
	view := *task
	view.Status = model.CallerMediaTaskStatus(task.Status)
	view.Events = nil
	return &view
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
