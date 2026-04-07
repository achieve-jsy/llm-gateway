package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// HealthHandler 健康检查 Handler
type HealthHandler struct {
	rdb       *redis.Client
	router    ModelsRouterInterface
	state     *provider.RuntimeState
	scheduler *scheduler.Scheduler
	tierState *budget.SnapshotHolder
}

// SetBudgetSnapshotHolder 为就绪探针提供部署 preflight 所需的预算能力与当前模式。
func (h *HealthHandler) SetBudgetSnapshotHolder(holder *budget.SnapshotHolder) {
	h.tierState = holder
}

// NewHealthHandler 创建 HealthHandler
func NewHealthHandler(rdb *redis.Client, router ModelsRouterInterface, state *provider.RuntimeState, sch *scheduler.Scheduler) *HealthHandler {
	return &HealthHandler{rdb: rdb, router: router, state: state, scheduler: sch}
}

// Health GET /health 存活探针
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready GET /ready 就绪探针（检查 Redis 连通性 + scheduler 就绪）
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	// 检查 Redis
	if err := h.rdb.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": gin.H{
				"redis":     "unavailable",
				"registry":  "unknown",
				"scheduler": "unknown",
			},
		})
		return
	}

	// 检查 scheduler
	schedulerReady := true
	if h.scheduler != nil && !h.scheduler.IsReady() {
		schedulerReady = false
	}

	// 获取模型数量
	modelCount := 0
	if h.router != nil {
		if resp, err := h.router.ListModels(ctx); err == nil && resp != nil {
			modelCount = len(resp.Data)
		}
	}

	loaded, source, loadedAt, lastError, stateModelCount, version := false, "", time.Time{}, "", 0, int64(0)
	if h.state != nil {
		loaded, source, loadedAt, lastError, stateModelCount, version = h.state.Snapshot()
	}

	if modelCount == 0 || !loaded || !schedulerReady {
		checks := gin.H{
			"redis":    "ok",
			"registry": "empty",
		}
		schedulerCheck := "ok"
		if !schedulerReady {
			schedulerCheck = "not_ready"
		}
		checks["scheduler"] = schedulerCheck

		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": checks,
			"details": gin.H{
				"source":      source,
				"loaded_at":   loadedAt,
				"last_error":  lastError,
				"model_count": maxInt(modelCount, stateModelCount),
				"version":     version,
			},
		})
		return
	}

	capabilities := gin.H{
		"tier_snapshot_schema": 1,
		"redis_v2":             true,
	}
	if h.tierState != nil {
		if snapshot := h.tierState.Get(); snapshot != nil {
			capabilities["master_enabled"] = snapshot.MasterEnabled
			capabilities["accounting_enabled"] = snapshot.AccountingEnabled
			capabilities["positive_enforcement_enabled"] = snapshot.PositiveEnforcementEnabled
			capabilities["zero_block_enabled"] = snapshot.ZeroBlockEnabled
			capabilities["shadow_enabled"] = snapshot.ShadowEnabled
			capabilities["v2_total_enforcement_enabled"] = snapshot.V2TotalEnforcementEnabled
			capabilities["legacy_total_write_enabled"] = snapshot.LegacyTotalWriteEnabled
			capabilities["snapshot_revision"] = snapshot.Revision
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":       "ready",
		"capabilities": capabilities,
		"checks": gin.H{
			"redis":     "ok",
			"registry":  "ok",
			"scheduler": "ok",
		},
		"details": gin.H{
			"source":      source,
			"loaded_at":   loadedAt,
			"model_count": modelCount,
			"version":     version,
		},
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
