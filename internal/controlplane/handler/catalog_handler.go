package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CatalogServiceInterface Catalog 模块对 handler 暴露的最小 surface。
//
// 设计要点：
//   - 不暴露 List：列表入口在 search 上（带空 query 即可）；
//   - 用 GetBySourceModelID 而非把 source_model_id 放在路径参数，以避免 '/'（如
//     azure_ai/deepseek-v3.2）被 Gin router 误识别。
type CatalogServiceInterface interface {
	// Search 返回 (results, total, err)：
	// total 是匹配 where 条件的全表总数（不受 limit 影响），
	// 前端用它展示「目录已收录 N 条」这种统计信息。
	Search(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error)
	GetByID(id uuid.UUID) (*model.ModelCatalog, error)
	GetBySourceModelID(source, sourceModelID string) (*model.ModelCatalog, error)
	TriggerAsyncSync(ctx context.Context)
	IsSyncing() bool
	ComputePriceDiff(ctx context.Context) ([]service.PriceDiffEntry, error)
	ApplyPriceDiff(ctx context.Context, ids []uuid.UUID) (int, error)
	AutoBind(ctx context.Context) (*service.AutoBindResult, error)
	// BindingStats 返回 catalog 总数 + 本地 model 绑定分布，
	// 让前端在 diff 为空时能区分 "目录空 / 未绑定 / 全部一致" 这几种语义。
	BindingStats(ctx context.Context) (*service.BindingStats, error)
	// SyncStatus 给前端轮询同步任务的实时状态：
	// 是否在跑、上次开始/结束、耗时、来源（remote/bundled）、错误、目录总数。
	SyncStatus(ctx context.Context) (*service.SyncStatus, error)
}

// CatalogHandler 路由：
//
//	GET  /api/v1/model-catalog/search?q=&provider=&mode=&limit=20
//	GET  /api/v1/model-catalog/by-source?source_model_id=…&source=litellm
//	GET  /api/v1/model-catalog/:id  （UUID 主键）
//	POST /api/v1/model-catalog/sync
//	GET  /api/v1/model-catalog/diff
//	POST /api/v1/model-catalog/apply-diff   body: {"model_ids":[...]}
//
// Detail 路由方案说明：source_model_id 含 '/'，路径参数会被 Gin router 误识别为分隔符，
// 因此保留 :id（UUID）路径参数 + 新增 /by-source query 入口。Frontend 可按需调用任一。
type CatalogHandler struct {
	svc CatalogServiceInterface
}

func NewCatalogHandler(svc CatalogServiceInterface) *CatalogHandler {
	return &CatalogHandler{svc: svc}
}

// Search GET /api/v1/model-catalog/search
func (h *CatalogHandler) Search(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	opts := repository.CatalogSearchOpts{
		Query:    c.Query("q"),
		Provider: c.Query("provider"),
		Mode:     c.Query("mode"),
		Source:   c.Query("source"),
		Limit:    limit,
	}
	results, total, err := h.svc.Search(opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "search catalog failed", "code": CodeInternalError}})
		return
	}
	// 注意：total 是匹配条件下的"全表总数"，不是 len(results)。
	// 之前用 len(results) 时，前端 searchCatalog({limit:1}) 拿到的 total 永远是 1，
	// 误把"目录已收录 N 条"显示成 1，看起来像同步没生效。
	c.JSON(http.StatusOK, gin.H{"data": results, "total": total})
}

// GetBySource GET /api/v1/model-catalog/by-source?source_model_id=…&source=litellm
func (h *CatalogHandler) GetBySource(c *gin.Context) {
	id := c.Query("source_model_id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "source_model_id is required", "code": CodeBadRequest}})
		return
	}
	source := c.DefaultQuery("source", "litellm")
	got, err := h.svc.GetBySourceModelID(source, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "lookup failed", "code": CodeInternalError}})
		return
	}
	if got == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "catalog entry not found", "code": "catalog_not_found"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": got})
}

// GetByID GET /api/v1/model-catalog/:id
func (h *CatalogHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid id", "code": CodeBadRequest}})
		return
	}
	got, err := h.svc.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "lookup failed", "code": CodeInternalError}})
		return
	}
	if got == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "catalog entry not found", "code": "catalog_not_found"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": got})
}

// Sync POST /api/v1/model-catalog/sync
//
// 异步执行 + 立即返回 202；前端拉一下进度就好，避免长链接卡 admin。
func (h *CatalogHandler) Sync(c *gin.Context) {
	h.svc.TriggerAsyncSync(c.Request.Context())
	c.JSON(http.StatusAccepted, gin.H{"data": gin.H{"message": "catalog sync triggered", "syncing": true}})
}

// Diff GET /api/v1/model-catalog/diff
func (h *CatalogHandler) Diff(c *gin.Context) {
	diffs, err := h.svc.ComputePriceDiff(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "compute diff failed", "code": CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": diffs, "total": len(diffs)})
}

// ApplyDiff POST /api/v1/model-catalog/apply-diff
type applyDiffInput struct {
	ModelIDs []string `json:"model_ids" binding:"required,min=1"`
}

func (h *CatalogHandler) ApplyDiff(c *gin.Context) {
	var input applyDiffInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "code": CodeBadRequest}})
		return
	}
	ids := make([]uuid.UUID, 0, len(input.ModelIDs))
	for _, raw := range input.ModelIDs {
		uid, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid model_id: " + raw, "code": CodeBadRequest}})
			return
		}
		ids = append(ids, uid)
	}
	updated, err := h.svc.ApplyPriceDiff(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated": updated, "skipped": 0, "failed_ids": []string{}}})
}

// AutoBind POST /api/v1/model-catalog/auto-bind
//
// 用本地 model 的 name / upstream_model_id（大小写不敏感）严格等于 catalog.source_model_id
// 来精确匹配。唯一命中直接绑定 catalog_id；多重命中作为 candidates 返回，由 admin 后续选择。
//
// 这是个安全的批量动作：
//   - 只动 catalog_id 一列，不改价格/上下文/能力图标；
//   - 已绑定的 model 不在处理范围内；
//   - 无歧义才写库，有歧义只返回候选。
func (h *CatalogHandler) AutoBind(c *gin.Context) {
	res, err := h.svc.AutoBind(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// SyncStatus GET /api/v1/model-catalog/sync-status
//
// 给前端"手动刷新"按钮做轮询。返回结构包含：
//   - running:           当前是否还在跑同步任务
//   - last_started_at:   最近一次开始时间（从未跑过为 null）
//   - last_finished_at:  最近一次结束时间
//   - last_duration_ms:  最近一次耗时
//   - last_source:       "remote" | "bundled" | ""（用了哪条链路）
//   - last_count:        最近一次写库条目数
//   - last_error:        最近一次错误（成功为空字符串）
//   - catalog_total:     当前 catalog 表总条目数
//
// 前端轮询逻辑：running=true 继续轮询；running=false 后根据 last_error 决定
// 提示成功还是失败，再用 last_count / catalog_total 给用户一个明确的"同步了多少"反馈。
func (h *CatalogHandler) SyncStatus(c *gin.Context) {
	status, err := h.svc.SyncStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": status})
}

// BindingStats GET /api/v1/model-catalog/binding-stats
//
// 给前端 CatalogSyncPage 空状态做语义判定：
//
//	catalog_total == 0  → 目录空，提示先去同步；
//	model_total   == 0  → 还没注册本地模型，提示去模型管理；
//	unbound_count > 0   → 提示点「智能绑定」；
//	bound_count   > 0
//	  且 diff 为空      → 已绑定模型与目录完全一致，无需处理。
//
// 不掺 diff 行数，diff 仍单独走 /diff 接口（一个接口一件事）。
func (h *CatalogHandler) BindingStats(c *gin.Context) {
	stats, err := h.svc.BindingStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": stats})
}
