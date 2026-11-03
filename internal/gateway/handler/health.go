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

// GroupNameCacheStatus 组名缓存就绪状态的最小接口。
// 让 HealthHandler 不直接依赖 cache 包的具体实现，便于测试与 nil 注入。
type GroupNameCacheStatus interface {
	// IsInitialized 组名缓存是否已完成至少一次成功加载
	IsInitialized() bool
}

// HealthHandler 健康检查 Handler
type HealthHandler struct {
	rdb            *redis.Client
	router         ModelsRouterInterface
	state          *provider.RuntimeState
	scheduler      *scheduler.Scheduler
	tierState      *budget.SnapshotHolder
	groupNameCache GroupNameCacheStatus
}

// SetBudgetSnapshotHolder 为就绪探针提供部署 preflight 所需的预算能力与当前模式。
func (h *HealthHandler) SetBudgetSnapshotHolder(holder *budget.SnapshotHolder) {
	h.tierState = holder
}

// groupNameCacheStats 组名缓存的可选观测能力：写入来源与组名数量，
// 用于 /ready 的 details.group_cache_source / group_cache_count。
// *cache.GroupNameCache 同时实现本接口与 GroupNameCacheStatus；测试里的最小 fake 可以不实现，
// 此时对应字段按「未知」呈现，不影响就绪判定。
type groupNameCacheStats interface {
	Len() int
	Source() string
}

// SetGroupNameCache 注入组名缓存状态（可为 nil：未注入时 /ready 的 group_cache 报 unknown）。
//
// 契约（与蓝绿发布门禁的约定）：
//   - 未注入（nil，例如没有组路由的装配）→ /ready 报 unknown 且仍返回 200（向后兼容，不参与就绪判定）；
//   - 已注入且就绪 → ok；已注入但未就绪 → not_initialized 且整体 503。
//   - 部署脚本的切流指纹只接受 ok，因此「漏注入」的装配会表现为「/ready 200 但永不 cutover-ready」，
//     而不会把流量切到组名目录不可用的实例上。
//   - group_cache=ok 只表示「完成过一次成功加载」，不表示与最新控制面数据一致：仅靠 Redis 快照预热
//     时 /ready 也会 ok（否则控制面不可达时无法提供服务）。是否来自旧快照由 details.group_cache_source
//     暴露给运维观察。
func (h *HealthHandler) SetGroupNameCache(groupNameCache GroupNameCacheStatus) {
	h.groupNameCache = groupNameCache
}

// withGroupCacheDetails 在 /ready 的 details 上补充组名缓存观测字段（来源与数量）。
// 只做观测，不改变就绪判定：来源为 redis-snapshot 表示当前内容来自 provider 快照（可能是旧数据，
// 控制面尚不可达），control-plane 表示已由控制面确认；从未写入过时报 unknown。
func (h *HealthHandler) withGroupCacheDetails(details gin.H) gin.H {
	source, count := "unknown", 0
	if stats, ok := h.groupNameCache.(groupNameCacheStats); ok {
		count = stats.Len()
		if s := stats.Source(); s != "" {
			source = s
		}
	}
	details["group_cache_source"] = source
	details["group_cache_count"] = count
	return details
}

// NewHealthHandler 创建 HealthHandler
func NewHealthHandler(rdb *redis.Client, router ModelsRouterInterface, state *provider.RuntimeState, sch *scheduler.Scheduler) *HealthHandler {
	return &HealthHandler{rdb: rdb, router: router, state: state, scheduler: sch}
}

// Health GET /health 存活探针
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// groupCacheCheck 返回 /ready 的 checks.group_cache 取值：
// 未注入 setter → unknown；已注入且就绪 → ok；已注入但未就绪 → not_initialized。
func (h *HealthHandler) groupCacheCheck() string {
	if h.groupNameCache == nil {
		return "unknown"
	}
	if h.groupNameCache.IsInitialized() {
		return "ok"
	}
	return "not_initialized"
}

// Ready GET /ready 就绪探针（检查 Redis 连通性 + scheduler 就绪 + 组名缓存就绪）
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	// 组名缓存检查项：未注入时为 unknown（不参与就绪判定），未就绪时判定为 not_ready
	groupCacheCheck := h.groupCacheCheck()
	groupCacheReady := groupCacheCheck != "not_initialized"

	// 检查 Redis
	if err := h.rdb.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": gin.H{
				"redis":       "unavailable",
				"registry":    "unknown",
				"scheduler":   "unknown",
				"group_cache": groupCacheCheck,
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

	if modelCount == 0 || !loaded || !schedulerReady || !groupCacheReady {
		// 本分支可能由 scheduler 未就绪或组名缓存未就绪触发，因此 registry 必须按实际条件
		// 计算，不能硬编码 empty——否则会出现 checks.registry=empty 而 details.model_count>0
		// 的自相矛盾响应，误导值班人判断根因。
		effectiveModelCount := maxInt(modelCount, stateModelCount)
		registryCheck := "ok"
		if effectiveModelCount == 0 || !loaded {
			registryCheck = "empty"
		}

		checks := gin.H{
			"redis":       "ok",
			"registry":    registryCheck,
			"group_cache": groupCacheCheck,
		}
		schedulerCheck := "ok"
		if !schedulerReady {
			schedulerCheck = "not_ready"
		}
		checks["scheduler"] = schedulerCheck

		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": checks,
			"details": h.withGroupCacheDetails(gin.H{
				"source":      source,
				"loaded_at":   loadedAt,
				"last_error":  lastError,
				"model_count": effectiveModelCount,
				"version":     version,
			}),
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
			"redis":       "ok",
			"registry":    "ok",
			"scheduler":   "ok",
			"group_cache": groupCacheCheck,
		},
		"details": h.withGroupCacheDetails(gin.H{
			"source":      source,
			"loaded_at":   loadedAt,
			"model_count": modelCount,
			"version":     version,
		}),
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
