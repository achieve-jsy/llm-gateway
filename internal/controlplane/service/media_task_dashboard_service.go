package service

import (
	"sort"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
)

// 本文件是「媒体任务维度运维看板」的业务层（E 队 Batch 3 · E1 任务 4）。
//
// 分层：handler 只做参数校验与响应组装；**口径与聚合语义在本层**；SQL 在
// repository.MediaTaskStatsRepo。响应结构体即**冻结的 API 契约**（字段名 snake_case，
// 与既有 ops API 一致），前端队直接按本文件的结构体实现。
//
// 三处口径是本任务的争议高发点，全部显式写死（代码常量 + 响应里的 caliber 块）：
//  1. 成功率分母排除未结束任务，`expired` **单独成桶、绝不并入 failed**；
//  2. 耗时分位只算 started_at/finished_at 均非 NULL 的终态行，并**同时返回样本数**
//     与被排除行数（避免拿 3 个样本的 P99 做决策）；
//  3. 成本是**整数微美元**，全程 int64 求和，不经浮点。

// 窗口口径常量（handler 用它们做参数校验，响应里原样回显）。
const (
	// MediaTaskDashboardDefaultRange 未指定时间参数时的默认窗口。
	MediaTaskDashboardDefaultRange = "24h"
	// MediaTaskDashboardMaxRange 强制上限：任何查询都不会跨越超过 90d 的窗口
	// （超限按上限截断，见 handler 的窗口解析）。这不是安全边界，而是**禁止全表扫描**的性能护栏。
	MediaTaskDashboardMaxRange = "90d"
)

// MediaTaskDashboardQuery 已解析并钳制完成的看板查询条件（参数校验在 handler）。
type MediaTaskDashboardQuery struct {
	// Start / End 是**生效窗口**（半开区间 [Start, End)），已由 handler 按上限钳制。
	Start time.Time
	End   time.Time
	// Kind 可选过滤（image|video）；空串 = 不过滤。
	Kind string
	// Scope 归属范围（admin / studio / dept / user）。
	Scope repository.VideoTaskScope
	// Range 生效的预置跨度名（回显用）；RangeDefaulted / Clamped 说明窗口是怎么来的。
	Range          string
	RangeDefaulted bool
	Clamped        bool
}

// MediaTaskDashboardService 媒体任务看板服务。
type MediaTaskDashboardService struct {
	stats *repository.MediaTaskStatsRepo
}

// NewMediaTaskDashboardService 创建 MediaTaskDashboardService 实例。
func NewMediaTaskDashboardService(stats *repository.MediaTaskStatsRepo) *MediaTaskDashboardService {
	return &MediaTaskDashboardService{stats: stats}
}

// ───────────────────────────── 冻结的响应契约 ─────────────────────────────

// MediaTaskDashboard 看板响应（`GET /api/v1/ops/media-tasks/dashboard` 的 `data`）。
type MediaTaskDashboard struct {
	Window             MediaTaskDashboardWindow                 `json:"window"`
	Totals             MediaTaskDashboardTotals                 `json:"totals"`
	StatusDistribution []repository.MediaTaskKindStatusCountRow `json:"status_distribution"`
	SuccessRate        []MediaTaskKindSuccessRate               `json:"success_rate"`
	DurationSeconds    []MediaTaskKindDuration                  `json:"duration_seconds"`
	CostMicroUSD       []repository.MediaTaskCostRow            `json:"cost_micro_usd"`
	Anomalies          MediaTaskDashboardAnomalies              `json:"anomalies"`
	Caliber            MediaTaskDashboardCaliber                `json:"caliber"`
}

// MediaTaskDashboardWindow 本次查询**实际生效**的时间窗口，供前端标注「已截断/已用默认值」。
type MediaTaskDashboardWindow struct {
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Range          string    `json:"range"`
	MaxRange       string    `json:"max_range"`
	RangeDefaulted bool      `json:"range_defaulted"`
	Clamped        bool      `json:"clamped"`
}

// MediaTaskDashboardTotals 窗口内任务总数与终态/未结束的拆分。
type MediaTaskDashboardTotals struct {
	Tasks         int64 `json:"tasks"`
	TerminalTasks int64 `json:"terminal_tasks"`
	ActiveTasks   int64 `json:"active_tasks"`
}

// MediaTaskKindSuccessRate 单个 kind 的成功率明细（分子分母全部展开，便于前端复核）。
type MediaTaskKindSuccessRate struct {
	Kind string `json:"kind"`
	// Completed = status ∈ {completed, succeeded(legacy)}，两套词表都算成功。
	Completed int64 `json:"completed"`
	// Failed 只含 status = failed；**不含 expired**。
	Failed  int64 `json:"failed"`
	Expired int64 `json:"expired"`
	// Active = 未结束任务（queued/in_progress/submitted/running），不计入成功率分母。
	Active int64 `json:"active"`
	// TerminalTotal = Completed + Failed + Expired（成功率的**分母**）。
	TerminalTotal int64 `json:"terminal_total"`
	// Unclassified = 不属于上述任何词表的任务数（新状态词上线时的安全网），同样不计入分母。
	Unclassified int64 `json:"unclassified"`
	// SuccessRate = Completed / TerminalTotal；分母为 0 时返回 0（不是 NaN）。
	SuccessRate float64 `json:"success_rate"`
}

// MediaTaskKindDuration 单个 kind 的耗时统计（started_at → finished_at）。
type MediaTaskKindDuration struct {
	Kind        string  `json:"kind"`
	SampleCount int64   `json:"sample_count"`
	P50Seconds  float64 `json:"p50_seconds"`
	P95Seconds  float64 `json:"p95_seconds"`
	P99Seconds  float64 `json:"p99_seconds"`
	// ExcludedMissingTimestamps = 终态但 started_at / finished_at 缺失而被排除的行数。
	// 运维必须能看到「分位背后丢了多少样本」，否则 P99 的可信度无从判断。
	ExcludedMissingTimestamps int64 `json:"excluded_missing_timestamps"`
}

// MediaTaskDashboardAnomalies 运维必须能看到的两个异常计数。
type MediaTaskDashboardAnomalies struct {
	CompletedWithoutCost     MediaTaskKindCountAnomaly    `json:"completed_without_cost"`
	PricingMissingRejections MediaPricingRejectionAnomaly `json:"pricing_missing_rejections"`
}

// MediaTaskKindCountAnomaly 按 kind 分组的异常计数。
type MediaTaskKindCountAnomaly struct {
	Total  int64                              `json:"total"`
	ByKind []repository.MediaTaskKindCountRow `json:"by_kind"`
}

// MediaPricingRejectionAnomaly 「无价拒绝」计数。
//
// 该事实**不在 media_tasks**（请求在提交上游前已被拒，没有任务行），来源是 call_logs 的
// 无价留痕，因此按 call_type 而不是 kind 分组。
type MediaPricingRejectionAnomaly struct {
	Available  bool                                  `json:"available"`
	Source     string                                `json:"source"`
	Total      int64                                 `json:"total"`
	ByCallType []repository.MediaPricingRejectionRow `json:"by_call_type"`
	Criteria   string                                `json:"criteria"`
	Note       string                                `json:"note"`
}

// MediaTaskDashboardCaliber 口径自述（随响应下发，避免「这个成功率怎么算的」反复扯皮）。
type MediaTaskDashboardCaliber struct {
	SuccessRateFormula             string   `json:"success_rate_formula"`
	CompletedDefinition            string   `json:"completed_definition"`
	ExpiredHandling                string   `json:"expired_handling"`
	DurationBasis                  string   `json:"duration_basis"`
	DurationStatuses               []string `json:"duration_statuses"`
	PercentileMethod               string   `json:"percentile_method"`
	CostUnit                       string   `json:"cost_unit"`
	LegacyKindNote                 string   `json:"legacy_kind_note"`
	CompletedWithoutCostDefinition string   `json:"completed_without_cost_definition"`
	WindowNote                     string   `json:"window_note"`
}

// MediaTaskDashboardCaliberText 返回当前生效的口径自述。
func MediaTaskDashboardCaliberText() MediaTaskDashboardCaliber {
	return MediaTaskDashboardCaliber{
		SuccessRateFormula:  "completed / (completed + failed + expired)",
		CompletedDefinition: "completed = status ∈ {completed, succeeded}; succeeded 是 legacy Seedance 词表中 completed 的等价词",
		ExpiredHandling:     "expired 单独计数，绝不并入 failed：对外（网关 /v1/videos）把 expired 归一为 failed 是为了 OpenAI 兼容，运维必须能区分「超时被 sweeper 清扫」与「上游失败」",
		DurationBasis:       "started_at → finished_at（上游处理耗时；不含排队等待，因为排队时长由队列/背压解释，是另一个信号）",
		DurationStatuses: []string{
			model.MediaTaskStatusCompleted,
			model.MediaTaskLegacyStatusSucceeded,
			model.MediaTaskStatusFailed,
			model.MediaTaskStatusExpired,
		},
		PercentileMethod: "nearest_rank（percentile_disc 语义，无线性插值）：第 k 位 = 升序第 ceil(p/100 × N) 个样本；分母 N = sample_count",
		CostUnit:         "整数微美元（micro USD，1 USD = 1_000_000）；SUM 全程 int64，不做浮点换算",
		LegacyKindNote:   "kind 为空串 = C1 扩列前创建的 legacy 行（全部是视频任务，刻意不回溯填充）；按 kind=image|video 过滤时这些行会被排除，看全量请不要传 kind",
		CompletedWithoutCostDefinition: "status = completed 且 COALESCE(cost_micro_usd, 0) = 0（含义：上游已产生费用但网关未记账 → 用量缺失或单价缺失）；" +
			"口径刻意只含新链路 completed，不含 legacy succeeded —— 后者产生于成本列存在之前、成本恒为 0，纳入会形成永不消失的存量基线，把新增异常淹掉",
		WindowNote: "任务按 created_at 归属窗口（半开区间 [start, end)）；上限 " + MediaTaskDashboardMaxRange + "，超限自动截断并在 window.clamped 标记",
	}
}

// ───────────────────────────── 聚合 ─────────────────────────────

// dashboardKindAgg 单个 kind 在窗口内的状态计数。
type dashboardKindAgg struct {
	statusCounts map[string]int64
	total        int64
	completed    int64
	failed       int64
	expired      int64
	active       int64
}

// terminalTotal 成功率的**分母**。
func (a dashboardKindAgg) terminalTotal() int64 {
	return a.completed + a.failed + a.expired
}

// unclassified 不属于任何已登记词表的任务数（新状态词上线时的安全网）。
func (a dashboardKindAgg) unclassified() int64 {
	return a.total - a.active - a.terminalTotal()
}

// successRate 成功率；分母为 0 时返回 0（不是 NaN，避免前端渲染出 null）。
func (a dashboardKindAgg) successRate() float64 {
	denom := a.terminalTotal()
	if denom <= 0 {
		return 0
	}
	return float64(a.completed) / float64(denom)
}

// GetDashboard 组装媒体任务看板数据。
//
// 所有分组数组都覆盖**同一个 kind 集合**（= 窗口内出现过的 kind ∪ 显式过滤的 kind），
// 缺数据的 kind 补零值行，这样前端可以按数组下标直接 zip，不必处理「某个数组少了某个 kind」。
func (s *MediaTaskDashboardService) GetDashboard(q MediaTaskDashboardQuery) (*MediaTaskDashboard, error) {
	aggs, kinds, err := s.collectStatusCounts(q)
	if err != nil {
		return nil, err
	}

	result := &MediaTaskDashboard{
		Window: MediaTaskDashboardWindow{
			Start:          q.Start,
			End:            q.End,
			Range:          q.Range,
			MaxRange:       MediaTaskDashboardMaxRange,
			RangeDefaulted: q.RangeDefaulted,
			Clamped:        q.Clamped,
		},
		Caliber: MediaTaskDashboardCaliberText(),
	}

	// 指标 1：kind × status 原始分布（status 保留原值，含 legacy 词与 expired）。
	result.StatusDistribution = make([]repository.MediaTaskKindStatusCountRow, 0, len(kinds))
	for _, kind := range kinds {
		statuses := make([]string, 0, len(aggs[kind].statusCounts))
		for status := range aggs[kind].statusCounts {
			statuses = append(statuses, status)
		}
		sort.Strings(statuses)
		for _, status := range statuses {
			result.StatusDistribution = append(result.StatusDistribution, repository.MediaTaskKindStatusCountRow{
				Kind:   kind,
				Status: status,
				Count:  aggs[kind].statusCounts[status],
			})
		}
	}

	// 指标 2：成功率（口径见 MediaTaskDashboardCaliberText）。
	result.SuccessRate = make([]MediaTaskKindSuccessRate, 0, len(kinds))
	var totalTasks, totalTerminal, totalActive int64
	for _, kind := range kinds {
		agg := aggs[kind]
		result.SuccessRate = append(result.SuccessRate, MediaTaskKindSuccessRate{
			Kind:          kind,
			Completed:     agg.completed,
			Failed:        agg.failed,
			Expired:       agg.expired,
			Active:        agg.active,
			TerminalTotal: agg.terminalTotal(),
			Unclassified:  agg.unclassified(),
			SuccessRate:   agg.successRate(),
		})
		totalTasks += agg.total
		totalTerminal += agg.terminalTotal()
		totalActive += agg.active
	}
	result.Totals = MediaTaskDashboardTotals{
		Tasks:         totalTasks,
		TerminalTasks: totalTerminal,
		ActiveTasks:   totalActive,
	}

	// 指标 3：耗时分位（缺失行数单独暴露）。
	durations, err := s.stats.DurationPercentiles(q.Start, q.End, q.Kind, q.Scope)
	if err != nil {
		return nil, err
	}
	missing, err := s.stats.DurationMissingTimestamps(q.Start, q.End, q.Kind, q.Scope)
	if err != nil {
		return nil, err
	}
	result.DurationSeconds = mergeDurationStats(kinds, durations, missing)

	// 指标 4：成本汇总（整数微美元）。
	costs, err := s.stats.CostByKind(q.Start, q.End, q.Kind, q.Scope)
	if err != nil {
		return nil, err
	}
	result.CostMicroUSD = mergeCostRows(kinds, costs)

	// 指标 5a：成功但无成本。
	noCost, err := s.stats.CompletedWithoutCost(q.Start, q.End, q.Kind, q.Scope)
	if err != nil {
		return nil, err
	}
	result.Anomalies.CompletedWithoutCost = MediaTaskKindCountAnomaly{
		ByKind: mergeKindCountRows(kinds, noCost),
	}
	for _, row := range result.Anomalies.CompletedWithoutCost.ByKind {
		result.Anomalies.CompletedWithoutCost.Total += row.Count
	}

	// 指标 5b：无价拒绝（来源 call_logs；该事实确实被持久化，不是伪造的 0）。
	rejections, err := s.stats.PricingMissingRejections(q.Start, q.End, q.Scope)
	if err != nil {
		return nil, err
	}
	rejectionTotal := int64(0)
	for _, row := range rejections {
		rejectionTotal += row.Count
	}
	if rejections == nil {
		rejections = []repository.MediaPricingRejectionRow{}
	}
	result.Anomalies.PricingMissingRejections = MediaPricingRejectionAnomaly{
		Available:  true,
		Source:     "call_logs",
		Total:      rejectionTotal,
		ByCallType: rejections,
		Criteria:   repository.PricingMissingRejectionCriteria,
		Note: "被拒请求在提交上游之前就被拦下，不落 media_tasks（所以不能从这个表里找）；" +
			"来源是 call_logs 的无价留痕（status=402 + usage_missing=true）。" +
			"按 call_type 分组而非 kind：拒绝发生在任务创建之前，没有 kind。",
	}

	return result, nil
}

// collectStatusCounts 拉取 kind × status 计数并折算成每 kind 的聚合，返回**统一排序**的 kind 列表。
//
// 列表统一来自本次窗口（或显式过滤的 kind），保证下游各数组能对齐到同一组 kind。
func (s *MediaTaskDashboardService) collectStatusCounts(q MediaTaskDashboardQuery) (map[string]dashboardKindAgg, []string, error) {
	rows, err := s.stats.StatusCounts(q.Start, q.End, q.Kind, q.Scope)
	if err != nil {
		return nil, nil, err
	}

	aggs := map[string]dashboardKindAgg{}
	for _, row := range rows {
		agg, ok := aggs[row.Kind]
		if !ok {
			agg = dashboardKindAgg{statusCounts: map[string]int64{}}
		}
		agg.statusCounts[row.Status] += row.Count
		agg.total += row.Count
		switch row.Status {
		case model.MediaTaskStatusCompleted, model.MediaTaskLegacyStatusSucceeded:
			agg.completed += row.Count
		case model.MediaTaskStatusFailed:
			agg.failed += row.Count
		case model.MediaTaskStatusExpired:
			// ⚠️ expired 单独成桶：绝不并进 failed。
			agg.expired += row.Count
		case model.MediaTaskStatusQueued, model.MediaTaskStatusInProgress,
			model.MediaTaskLegacyStatusSubmitted, model.MediaTaskLegacyStatusRunning:
			agg.active += row.Count
		}
		aggs[row.Kind] = agg
	}

	kinds := make([]string, 0, len(aggs)+1)
	if q.Kind != "" {
		// 显式过滤：即使窗口内没有任何任务，也回一行零值，前端直接渲染 0 而不是空态。
		if _, ok := aggs[q.Kind]; !ok {
			aggs[q.Kind] = dashboardKindAgg{statusCounts: map[string]int64{}}
		}
	}
	for kind := range aggs {
		kinds = append(kinds, kind)
	}
	sortKinds(kinds)
	return aggs, kinds, nil
}

// mergeDurationStats 把分位行与「缺失时间戳」行对齐到统一的 kind 列表。
func mergeDurationStats(kinds []string, durations []repository.MediaTaskDurationRow, missing []repository.MediaTaskKindCountRow) []MediaTaskKindDuration {
	byKind := make(map[string]repository.MediaTaskDurationRow, len(durations))
	for _, row := range durations {
		byKind[row.Kind] = row
	}
	missingByKind := make(map[string]int64, len(missing))
	for _, row := range missing {
		missingByKind[row.Kind] = row.Count
	}

	out := make([]MediaTaskKindDuration, 0, len(kinds))
	for _, kind := range kinds {
		row := byKind[kind] // 缺失时为零值（sample_count=0，分位全 0）
		out = append(out, MediaTaskKindDuration{
			Kind:                      kind,
			SampleCount:               row.SampleCount,
			P50Seconds:                row.P50Seconds,
			P95Seconds:                row.P95Seconds,
			P99Seconds:                row.P99Seconds,
			ExcludedMissingTimestamps: missingByKind[kind],
		})
	}
	return out
}

// mergeCostRows 把成本行对齐到统一的 kind 列表。
func mergeCostRows(kinds []string, costs []repository.MediaTaskCostRow) []repository.MediaTaskCostRow {
	byKind := make(map[string]repository.MediaTaskCostRow, len(costs))
	for _, row := range costs {
		byKind[row.Kind] = row
	}
	out := make([]repository.MediaTaskCostRow, 0, len(kinds))
	for _, kind := range kinds {
		row := byKind[kind]
		row.Kind = kind
		out = append(out, row)
	}
	return out
}

// mergeKindCountRows 把单列计数行对齐到统一的 kind 列表。
func mergeKindCountRows(kinds []string, rows []repository.MediaTaskKindCountRow) []repository.MediaTaskKindCountRow {
	byKind := make(map[string]int64, len(rows))
	for _, row := range rows {
		byKind[row.Kind] = row.Count
	}
	out := make([]repository.MediaTaskKindCountRow, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, repository.MediaTaskKindCountRow{Kind: kind, Count: byKind[kind]})
	}
	return out
}

// mediaTaskKindRank 决定各分组数组里 kind 的**统一顺序**（所有数组共用，前端可直接 zip）。
//
// image / video 是两类真实媒体任务，排在最前；legacy 行 kind 为空串（C1 扩列前创建，
// 不回溯填充是刻意的），单独排在最后；其余未知取值按字典序居中。
func mediaTaskKindRank(kind string) int {
	switch kind {
	case model.MediaTaskKindImage:
		return 0
	case model.MediaTaskKindVideo:
		return 1
	case "":
		return 3
	default:
		return 2
	}
}

// sortKinds 按统一顺序排序 kind 列表（稳定、确定，便于对拍与快照测试）。
func sortKinds(kinds []string) {
	sort.Slice(kinds, func(i, j int) bool {
		ri, rj := mediaTaskKindRank(kinds[i]), mediaTaskKindRank(kinds[j])
		if ri != rj {
			return ri < rj
		}
		return kinds[i] < kinds[j]
	})
}
