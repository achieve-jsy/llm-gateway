package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// 本文件是「媒体任务维度运维看板」的**只读聚合仓储**（E 队 Batch 3 · E1 任务 4）。
//
// 目标表：`media_tasks`（C1 由 video_tasks 改名扩列；单表承载 image/video，**不需要 union**）
// 与 `call_logs`（唯一持久化「无价拒绝」事实的地方，见 PricingMissingRejections）。
//
// 分层与既有 ops 统计一致：SQL 只出现在 repository，service 负责口径，handler 只做参数校验。
//
// 两条贯穿全文件的硬约束：
//  1. **绝不用字符串拼接外部输入**。时间/状态/kind/scope 一律走 GORM 参数绑定（`?`）；
//     仅有的 fmt.Sprintf 只插值**本文件内的固定常量与整数**（列别名、百分位分母），无注入面。
//  2. **状态词表保留原值**。新链路 queued/in_progress/completed/failed/expired 与
//     legacy Seedance submitted/running/succeeded **并存**，看板同时归组两套词表，
//     且 `expired` 绝不并入 failed（对外归一为 failed 是 OpenAI 兼容需要，运维必须区分）。
//
// 性能：所有查询都强制 `created_at` / `ts` 的**半开区间**谓词（[start, end)），
// 保证走既有时间索引（media_tasks.idx_video_tasks_ts / call_logs.idx_call_logs_ts），
// 永远不会退化成全表扫描。窗口上限由 service 层强制（默认 24h / 最大 90d）。

// ── 聚合行类型（字段名即 API 契约中的 snake_case 字段名）────────────────────────

// MediaTaskKindStatusCountRow kind × status 的任务数（status 保留原值，含 legacy 词）。
type MediaTaskKindStatusCountRow struct {
	Kind   string `gorm:"column:kind"   json:"kind"`
	Status string `gorm:"column:status" json:"status"`
	Count  int64  `gorm:"column:count"  json:"count"`
}

// MediaTaskDurationRow 每个 kind 的 started_at→finished_at 耗时分位与样本数。
type MediaTaskDurationRow struct {
	Kind        string  `gorm:"column:kind"         json:"kind"`
	SampleCount int64   `gorm:"column:sample_count" json:"sample_count"`
	P50Seconds  float64 `gorm:"column:p50_seconds"  json:"p50_seconds"`
	P95Seconds  float64 `gorm:"column:p95_seconds"  json:"p95_seconds"`
	P99Seconds  float64 `gorm:"column:p99_seconds"  json:"p99_seconds"`
}

// MediaTaskKindCountRow 按 kind 分组的单列计数（缺时间戳 / 成功但无成本 共用）。
type MediaTaskKindCountRow struct {
	Kind  string `gorm:"column:kind"  json:"kind"`
	Count int64  `gorm:"column:count" json:"count"`
}

// MediaTaskCostRow 按 kind 分组的成本汇总（整数微美元，绝不经浮点）。
type MediaTaskCostRow struct {
	Kind          string `gorm:"column:kind"            json:"kind"`
	CostMicroUSD  int64  `gorm:"column:cost_micro_usd"  json:"cost_micro_usd"`
	Tasks         int64  `gorm:"column:tasks"           json:"tasks"`
	TasksWithCost int64  `gorm:"column:tasks_with_cost" json:"tasks_with_cost"`
}

// MediaPricingRejectionRow 被媒体准入计价闸门（402 media_pricing_missing）拦下的请求数。
//
// 该事实**不在 media_tasks**（请求在提交上游之前就被拒了，没有任务行），
// 唯一持久化点是 call_logs（A2 的无价留痕），因此按 call_type 而不是 kind 分组。
type MediaPricingRejectionRow struct {
	CallType string `gorm:"column:call_type" json:"call_type"`
	Count    int64  `gorm:"column:count"     json:"count"`
}

// MediaTaskStatsRepo 媒体任务看板的只读聚合仓储。
type MediaTaskStatsRepo struct {
	db *gorm.DB
}

// NewMediaTaskStatsRepo 创建 MediaTaskStatsRepo 实例。
func NewMediaTaskStatsRepo(db *gorm.DB) *MediaTaskStatsRepo {
	return &MediaTaskStatsRepo{db: db}
}

// ── 词表（看板口径；**刻意不复用 sweeper 的列表**）──────────────────────────────

// mediaTaskDashboardTerminalStatuses 已结束任务（success/failed/expired 的并集来源）。
//
// completed 为 succeeded 的 legacy 等价词；failed 两套词表同形；expired 为新链路专有。
//
// ⚠️ 与 repository.MediaTaskActiveStatuses() 是**两个不同用途的列表**，不要合并：
// 那个是 C7 超时 sweeper 的扫描范围，只含新词表（queued/in_progress）——
// 若把 legacy submitted/running 塞进去，sweeper 会开始清扫存量 Seedance 行（回归事故）。
// 本函数只服务看板，含 legacy 词，保证「终态」覆盖两套词表。
func mediaTaskDashboardTerminalStatuses() []string {
	return []string{
		model.MediaTaskStatusCompleted,
		model.MediaTaskLegacyStatusSucceeded,
		model.MediaTaskStatusFailed,
		model.MediaTaskStatusExpired,
	}
}

// MediaPricingGateCallTypes 媒体准入计价闸门**唯一可能生效**的入站端点归一类型
// （observability.DeriveCallType 的取值；「查任务」类端点不产生新用量，不接闸门）。
var MediaPricingGateCallTypes = []string{
	"image.generations",
	"image.edits",
	"video.create",
	"video.submit",
}

// MediaPricingGateInboundEndpoints 入站路径兜底：call_type 为空（历史行/未登记端点）时按路径匹配。
var MediaPricingGateInboundEndpoints = []string{
	"/v1/images/generations",
	"/v1/images/edits",
	"/v1/videos",
	"/v1/contents/generations/tasks",
}

// PricingMissingRejectionCriteria 是「无价拒绝」判据的**可读表达式**，直接进 API 响应，
// 让运维/前端不必读源码就能知道这个数字是怎么数出来的（口径不清是看板的经典争议源）。
const PricingMissingRejectionCriteria = "call_logs.status = 402 AND call_logs.usage_missing = true " +
	"AND COALESCE(call_logs.budget_block_code, '') = '' " +
	"AND (call_logs.call_type IN ('image.generations','image.edits','video.create','video.submit') " +
	"OR call_logs.inbound_endpoint IN ('/v1/images/generations','/v1/images/edits','/v1/videos','/v1/contents/generations/tasks'))"

// ── 查询构造 ───────────────────────────────────────────────────────────────────

// mediaTaskWindowQuery 构造 media_tasks 上带**参数绑定**的时间窗口 + kind + 归属范围查询。
//
// 时间谓词是半开区间 `created_at >= ? AND created_at < ?`：任务按**创建时刻**归属窗口
// （与既有 StatsTrend/StatsDistribution 的口径一致），避免跨窗口重复计数。
func (r *MediaTaskStatsRepo) mediaTaskWindowQuery(start, end time.Time, kind string, scope VideoTaskScope) *gorm.DB {
	query := r.db.Model(&model.VideoTask{}).
		Where("created_at >= ? AND created_at < ?", start, end)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	return applyMediaTaskScope(query, scope)
}

// callLogWindowQuery 构造 call_logs 上带**参数绑定**的时间窗口 + 归属范围查询。
func (r *MediaTaskStatsRepo) callLogWindowQuery(start, end time.Time, scope VideoTaskScope) *gorm.DB {
	query := r.db.Table("call_logs").Where("ts >= ? AND ts < ?", start, end)
	return applyCallLogScope(query, scope)
}

// applyCallLogScope 按角色限定 call_logs 可见范围。
//
// 与 media_tasks 的 applyMediaTaskScope 语义一一对应（admin 全量 / studio / dept / user），
// 但 call_logs 的归属列是**冗余快照列**，谓词形如
// internal/orgservice/rbac/policy/usage_policy.go 的 QueryScope（同一套 dept_id 三态
// 兼容：EhrDeptID / departments.id / users→departments.dept_id），此处复用其语义，
// 避免同一种「部门可见范围」在仓里出现第二套解释。
func applyCallLogScope(query *gorm.DB, scope VideoTaskScope) *gorm.DB {
	if scope.IsGlobal {
		return query
	}
	if scope.UserID != "" {
		return query.Where("call_logs.user_id = ?", scope.UserID)
	}
	if len(scope.StudioIDs) > 0 {
		return query.Where("call_logs.studio_id IN ?", scope.StudioIDs)
	}
	if len(scope.DeptIDs) > 0 {
		return query.Where(
			`call_logs.dept_id IN ? OR call_logs.dept_id IN (SELECT id::text FROM departments WHERE dept_id IN ?) OR call_logs.user_id IN (SELECT u2.id::text FROM users u2 JOIN departments d2 ON d2.id = u2.dept_id WHERE d2.dept_id IN ?)`,
			scope.DeptIDs, scope.DeptIDs, scope.DeptIDs,
		)
	}
	return query.Where("1 = 0")
}

// ── 指标 1：kind × status 任务数分布（status 保留原值）──────────────────────────

// StatusCounts 返回窗口内按 (kind, status) 分组的任务数，status 为数据库原值。
func (r *MediaTaskStatsRepo) StatusCounts(start, end time.Time, kind string, scope VideoTaskScope) ([]MediaTaskKindStatusCountRow, error) {
	var rows []MediaTaskKindStatusCountRow
	err := r.mediaTaskWindowQuery(start, end, kind, scope).
		Select("kind, status, COUNT(*) AS count").
		Group("kind, status").
		Order("kind ASC, status ASC").
		Scan(&rows).Error
	return rows, err
}

// ── 指标 3：started_at → finished_at 耗时分位 ──────────────────────────────────

// mediaTaskDurationSecondsExpr 返回「started_at → finished_at 秒数」的 SQL 表达式。
//
// PG 用 `EXTRACT(EPOCH FROM (finished_at - started_at))`（与既有平均耗时口径同源）；
// SQLite 内存库（仅测试）没有 EXTRACT，退化为 `julianday` 差值。
// 只返回**本文件内的两个固定常量**之一，不接收任何外部输入 —— 无注入面。
func mediaTaskDurationSecondsExpr(db *gorm.DB) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		return "(julianday(finished_at) - julianday(started_at)) * 86400.0"
	}
	return "EXTRACT(EPOCH FROM (finished_at - started_at))"
}

// DurationPercentiles 返回窗口内每个 kind 的耗时 P50/P95/P99 与**参与计算的样本数**。
//
// 缺失口径（写死在 WHERE 里，调用方无法绕过）：**只统计 started_at 与 finished_at
// 均非 NULL 的终态行**。legacy Seedance 行这两个字段基本是 NULL，会被整体排除；
// 被排除的行数由 DurationMissingTimestamps 单独返回，避免运维拿「基于 3 个样本的 P99」做判断。
//
// 分位定义：**nearest-rank（percentile_disc 语义，不做线性插值）**，
// 第 k 位 = 升序第 `ceil(p/100 × N)` 个样本，用整数运算 `(N*p + 99) / 100` 表达
// （PG 与 SQLite 的整数除法都向下取整，故两边结果一致）。
//
// 为什么不用 PG 的 PERCENTILE_CONT：SQLite 内存库没有该函数，用了就只能跳过测试；
// 这里刻意选可移植实现，让**同一段 SQL 在生产与测试里跑**（口径不因方言漂移）。
// 排序窗口函数（ROW_NUMBER/COUNT OVER）PG 8.4+ 与 SQLite 3.25+ 均支持。
func (r *MediaTaskStatsRepo) DurationPercentiles(start, end time.Time, kind string, scope VideoTaskScope) ([]MediaTaskDurationRow, error) {
	expr := mediaTaskDurationSecondsExpr(r.db)

	inner := r.mediaTaskWindowQuery(start, end, kind, scope).
		Select(fmt.Sprintf(
			"kind, %s AS sec, ROW_NUMBER() OVER (PARTITION BY kind ORDER BY %s) AS rn, COUNT(*) OVER (PARTITION BY kind) AS cnt",
			expr, expr,
		)).
		Where("status IN ?", mediaTaskDashboardTerminalStatuses()).
		Where("started_at IS NOT NULL").
		Where("finished_at IS NOT NULL")

	// 百分位分母是**编译期常量**，不来自任何输入。
	percentiles := []struct {
		percent int
		column  string
	}{
		{50, "p50_seconds"},
		{95, "p95_seconds"},
		{99, "p99_seconds"},
	}
	selects := []string{"kind", "COUNT(*) AS sample_count"}
	for _, p := range percentiles {
		selects = append(selects, fmt.Sprintf(
			"COALESCE(MAX(CASE WHEN rn = (cnt * %d + 99) / 100 THEN sec END), 0) AS %s",
			p.percent, p.column,
		))
	}

	var rows []MediaTaskDurationRow
	err := r.db.Table("(?) AS d", inner).
		Select(strings.Join(selects, ", ")).
		Group("kind").
		Order("kind ASC").
		Scan(&rows).Error
	return rows, err
}

// DurationMissingTimestamps 返回窗口内「终态但 started_at 或 finished_at 为 NULL」的任务数。
//
// 这些行**不参与**分位计算；把排除量显式暴露出来，是让运维知道分位背后丢了多少样本。
func (r *MediaTaskStatsRepo) DurationMissingTimestamps(start, end time.Time, kind string, scope VideoTaskScope) ([]MediaTaskKindCountRow, error) {
	var rows []MediaTaskKindCountRow
	err := r.mediaTaskWindowQuery(start, end, kind, scope).
		Select("kind, COUNT(*) AS count").
		Where("status IN ?", mediaTaskDashboardTerminalStatuses()).
		Where("(started_at IS NULL OR finished_at IS NULL)").
		Group("kind").
		Order("kind ASC").
		Scan(&rows).Error
	return rows, err
}

// ── 指标 4：成本汇总（整数微美元）─────────────────────────────────────────────

// CostByKind 返回窗口内按 kind 分组的成本汇总。
//
// cost_micro_usd 是**整数微美元**（1 USD = 1e6），SUM 全程 int64，
// **不做任何浮点换算**（1e9 微美元 ≈ 1000 USD，float64 尚可，但回写/展示链路上
// 任何一次 float 往返都会引入分位误差，运维对账时是实打实的坑）。
// TasksWithCost 用可移植的 SUM(CASE ...) 而非 PG 专有 COUNT(*) FILTER，
// 与 DurationPercentiles 一样是为了生产/测试同一条 SQL。
func (r *MediaTaskStatsRepo) CostByKind(start, end time.Time, kind string, scope VideoTaskScope) ([]MediaTaskCostRow, error) {
	var rows []MediaTaskCostRow
	err := r.mediaTaskWindowQuery(start, end, kind, scope).
		Select(`kind,
			COALESCE(SUM(cost_micro_usd), 0) AS cost_micro_usd,
			COUNT(*) AS tasks,
			SUM(CASE WHEN COALESCE(cost_micro_usd, 0) > 0 THEN 1 ELSE 0 END) AS tasks_with_cost`).
		Group("kind").
		Order("kind ASC").
		Scan(&rows).Error
	return rows, err
}

// ── 指标 5a：成功但无成本（网关没记账）────────────────────────────────────────

// CompletedWithoutCost 返回窗口内 `status = completed` 且成本为 0/NULL 的任务数。
//
// 含义：上游确实产生了费用，但网关侧计价链路没有写入 cost_micro_usd
// —— 可能是用量缺失（上游不回 usage）或单价缺失（catalog 媒体价列为 NULL）。
// 这正是「未计价成功的任务字段为 0」这一运维信号。
//
// ⚠️ 口径刻意**只含新链路 `completed`，不含 legacy `succeeded`**：
// legacy Seedance 行产生于 cost_micro_usd 列存在之前，其成本恒为 0，
// 若一并统计会让该指标背上一个永不消失的存量基线（112 行存量全部误报），
// 把真正的新增异常淹掉。存量行的「无成本」是已知事实，不是待处理异常。
func (r *MediaTaskStatsRepo) CompletedWithoutCost(start, end time.Time, kind string, scope VideoTaskScope) ([]MediaTaskKindCountRow, error) {
	var rows []MediaTaskKindCountRow
	err := r.mediaTaskWindowQuery(start, end, kind, scope).
		Select("kind, COUNT(*) AS count").
		Where("status = ?", model.MediaTaskStatusCompleted).
		Where("COALESCE(cost_micro_usd, 0) = 0").
		Group("kind").
		Order("kind ASC").
		Scan(&rows).Error
	return rows, err
}

// ── 指标 5b：无价拒绝（402 media_pricing_missing）──────────────────────────────

// PricingMissingRejections 统计窗口内被媒体准入计价闸门拦下的请求数，按 call_type 分组。
//
// 事实来源（已核实，见 A2 交付）：
//   - 闸门 middleware.EnsureMediaPricing / RequireMediaPricing 命中「模型无媒体价」时
//     写 402 + `code=media_pricing_missing`，并给 gin context 打 CtxKeyMediaPricingMissing；
//   - usage_collector 是**无条件** Emit 的（A2 把原来 `EstCostUSD > 0` 的门槛改成了
//     hasBillableSignal），因此被拒绝的请求**同样落一行 call_logs**，
//     且 `usage_missing=true`（读ctx标记强制置位，见 usage_collector.go）；
//   - 于是这个数字**不必伪造**：它是可查的真实事实，而不是一个永远为 0 的占位字段。
//
// 判据（三段，缺一不可）：
//  1. status = 402 且 usage_missing = true —— 计费类拒绝 + 无价留痕；
//  2. budget_block_code 为空 —— 排除预算/配额闸门的 402（budget_exceeded /
//     tier_budget_exceeded 也走 402，但一定会写 budget_block_code，见 budget.SetBlockContext）；
//  3. 入站端点属于媒体**创建**端点 —— 闸门只接在创建链路上（「查任务」不产生新用量，不接闸门）。
//     用 call_type 精确匹配，并以 inbound_endpoint 兜底 call_type 为空的历史行。
//
// 已知限制（如实说明，不粉饰）：call_logs 是 1033 万行 / 13GB 的热表，
// 本查询只按 ts 半开区间裁剪（走 idx_call_logs_ts），status/usage_missing/call_type
// 是区间内的**过滤**而非索引前缀。窗口上限 90d 时扫描量不小 —— 若要常态化 90d 查询，
// 需要 `CREATE INDEX CONCURRENTLY`（代价高，需人工决策，本任务不擅自加迁移）。
func (r *MediaTaskStatsRepo) PricingMissingRejections(start, end time.Time, scope VideoTaskScope) ([]MediaPricingRejectionRow, error) {
	var rows []MediaPricingRejectionRow
	err := r.callLogWindowQuery(start, end, scope).
		Select("call_type, COUNT(*) AS count").
		Where("status = ?", 402).
		Where("usage_missing = ?", true).
		Where("COALESCE(budget_block_code, '') = ''").
		Where("call_type IN ? OR inbound_endpoint IN ?", MediaPricingGateCallTypes, MediaPricingGateInboundEndpoints).
		Group("call_type").
		Order("count DESC, call_type ASC").
		Scan(&rows).Error
	return rows, err
}
