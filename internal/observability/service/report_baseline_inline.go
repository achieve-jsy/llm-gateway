package service

// 本文件实现「期内现算降级」：当 baseline_quantiles 未物化（或为空）时，报告链路的
// 输出失控(3.4)/多任务(3.3)/杀鸡用牛刀(3.5) 直接从 call_logs 现算群体分位，参与分析。
// 设计对齐 docs §6.3 / baseline_service.go 的 percentile_cont 分组算法与过滤约定：
//   - 只读 call_logs（+岗位维度 LEFT JOIN users），排除 Service Key；
//   - 分组键：model=cl.model / position=岗位归因 / user=cl.user_id；
//   - metric 值列：prompt_tokens / completion_tokens（成功请求 status=200）或 est_cost_usd。
// 窗口：由调用方给定参考窗（闭区间日期），查询映射为 [from, to+1d) 半开（与 periodRange 口径一致）。
// 本实现不写任何表；物化表存在时优先读表（口径统一），不存在/空时走这里。

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

const (
	reportBaselineWindowDays   = 56
	reportPositionScopeKeyExpr = `COALESCE(NULLIF(u.job_category, ''), NULLIF(u.job_family, ''), NULLIF(u.job_title, ''), '')`
	reportPositionJoinSQL      = `LEFT JOIN users u ON u.id::text = cl.user_id`
)

// inlineBaselineScope 现算维度。
type inlineBaselineScope int

const (
	inlineScopeModel inlineBaselineScope = iota
	inlineScopePosition
	inlineScopeUser
)

// inlineScopeKeyExpr 返回某维度的分组键表达式（对齐 baseline_service.go 的 scopeKeyExpr）。
func (s inlineBaselineScope) keyExpr() string {
	switch s {
	case inlineScopeModel:
		return "cl.model"
	case inlineScopePosition:
		return reportPositionScopeKeyExpr // COALESCE(job_category→job_family→job_title)
	case inlineScopeUser:
		return "COALESCE(cl.user_id, 'unknown')"
	}
	return "cl.model"
}

func (s inlineBaselineScope) joinSQL() string {
	if s == inlineScopePosition {
		return reportPositionJoinSQL // LEFT JOIN users u ON u.id::text = cl.user_id
	}
	return ""
}

// String 供错误信息使用。
func (s inlineBaselineScope) String() string {
	switch s {
	case inlineScopeModel:
		return "model"
	case inlineScopePosition:
		return "position"
	case inlineScopeUser:
		return "user"
	}
	return "model"
}

// inlineMetric 现算指标：值列 + 附加过滤。
type inlineMetric int

const (
	inlineMetricPrompt inlineMetric = iota
	inlineMetricCompletion
)

func (m inlineMetric) col() string {
	switch m {
	case inlineMetricPrompt:
		return "cl.prompt_tokens"
	case inlineMetricCompletion:
		return "cl.completion_tokens"
	}
	return "cl.prompt_tokens"
}

// String 供错误信息使用。
func (m inlineMetric) String() string {
	switch m {
	case inlineMetricPrompt:
		return "prompt"
	case inlineMetricCompletion:
		return "completion"
	}
	return "prompt"
}

// computeInlineQuantiles 对参考窗内的 call_logs 按 scope 分组，现算 p25/p50/p75/p90 分位。
// 返回 map[scopeKey]map[quantile]float64；只返回关注 keys 命中的组（keys 为空则查全部分组）。
// 全程只读，不依赖任何新表。
func computeInlineQuantiles(ctx context.Context, db *gorm.DB, scope inlineBaselineScope, metric inlineMetric, from, to string, keys []string) (map[string]map[string]float64, error) {
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("inline baseline: parse to %q: %w", to, err)
	}
	toExcl := end.AddDate(0, 0, 1).Format("2006-01-02")

	metricCol := metric.col()
	// 成功请求才算分位（与物化 SQL 的 extraFilter="cl.status = 200" 一致）。
	extraFilter := "cl.status = 200"

	keyExpr := scope.keyExpr()
	join := scope.joinSQL()

	// keys 过滤放进 base WHERE（该层 cl.model / u.job_* 可见）；无 keys → 全部分组。
	// 用 GORM 的 `IN ?` 展开语法（项目惯例，见 key_org_syncer.go:103），避免数组驱动差异。
	keyFilter := ""
	if len(keys) > 0 {
		keyFilter = "\n  AND (" + keyExpr + ") IN ?"
	}

	sql := fmt.Sprintf(`
WITH base AS (
	SELECT %s AS scope_key, %s AS metric_val
	FROM call_logs cl
	%s
	WHERE (cl.ts AT TIME ZONE 'Asia/Shanghai')::date >= ?
	  AND (cl.ts AT TIME ZONE 'Asia/Shanghai')::date < ?
	  AND %s
	  AND %s IS NOT NULL
	  AND %s%s
)
SELECT b.scope_key,
	percentile_cont(0.25) WITHIN GROUP (ORDER BY b.metric_val) AS p25,
	percentile_cont(0.50) WITHIN GROUP (ORDER BY b.metric_val) AS p50,
	percentile_cont(0.75) WITHIN GROUP (ORDER BY b.metric_val) AS p75,
	percentile_cont(0.90) WITHIN GROUP (ORDER BY b.metric_val) AS p90
FROM base b
GROUP BY b.scope_key
`, keyExpr, metricCol, join,
		obsmodel.WhereExcludeServiceKeyLogsCL, metricCol, extraFilter, keyFilter)

	args := []any{from, toExcl}
	if len(keys) > 0 {
		args = append(args, keys)
	}

	type row struct {
		ScopeKey           string
		P25, P50, P75, P90 float64
	}
	var rows []row
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("inline baseline: compute %s×%s: %w", scope, metric, err)
	}

	out := make(map[string]map[string]float64, len(rows))
	for _, r := range rows {
		out[r.ScopeKey] = map[string]float64{
			"p25": r.P25, "p50": r.P50, "p75": r.P75, "p90": r.P90,
		}
	}
	return out, nil
}

// inlineBaselineWindow 由报告期起点 from（YYYY-MM-DD）推导参考窗 [from-winDays-1d, from-1d]（历史滚动 8~12 周，
// 不含报告期，避免自我参照）。from 非法/空 → 最近 winDays 天。
func inlineBaselineWindow(from string) (string, string) {
	winDays := reportBaselineWindowDays
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		today := time.Now()
		return today.AddDate(0, 0, -winDays).Format("2006-01-02"), today.AddDate(0, 0, -1).Format("2006-01-02")
	}
	winEnd := start.AddDate(0, 0, -1)
	winStart := start.AddDate(0, 0, -1-winDays)
	return winStart.Format("2006-01-02"), winEnd.Format("2006-01-02")
}

// inlineOutputRunawayBaselines 现算 3.4 输出失控基线：分模型 prompt P25 / completion P75 + 同岗位 prompt P25。
func inlineOutputRunawayBaselines(ctx context.Context, db *gorm.DB, from string, positions, models []string) (OutputRunawayBaselines, error) {
	out := OutputRunawayBaselines{
		ModelPromptP25:    map[string]float64{},
		ModelOutputP75:    map[string]float64{},
		PositionPromptP25: map[string]float64{},
	}
	winFrom, winTo := inlineBaselineWindow(from)
	if len(models) > 0 {
		mp, err := computeInlineQuantiles(ctx, db, inlineScopeModel, inlineMetricPrompt, winFrom, winTo, models)
		if err != nil {
			return out, err
		}
		mc, err := computeInlineQuantiles(ctx, db, inlineScopeModel, inlineMetricCompletion, winFrom, winTo, models)
		if err != nil {
			return out, err
		}
		for m, q := range mp {
			out.ModelPromptP25[m] = q["p25"]
		}
		for m, q := range mc {
			out.ModelOutputP75[m] = q["p75"]
		}
	}
	if len(positions) > 0 {
		pp, err := computeInlineQuantiles(ctx, db, inlineScopePosition, inlineMetricPrompt, winFrom, winTo, positions)
		if err != nil {
			return out, err
		}
		for pos, q := range pp {
			out.PositionPromptP25[pos] = q["p25"]
		}
	}
	return out, nil
}

// inlineMultiTaskBaselines 现算 3.3 多任务基线：每用户 prompt P90（近似；无会话末轮信息时以请求级 P90 代理）。
func inlineMultiTaskBaselines(ctx context.Context, db *gorm.DB, from string, userIDs []string) (MultiTaskBaselines, error) {
	out := MultiTaskBaselines{UserPromptP90: map[string]float64{}}
	if len(userIDs) == 0 {
		return out, nil
	}
	winFrom, winTo := inlineBaselineWindow(from)
	uq, err := computeInlineQuantiles(ctx, db, inlineScopeUser, inlineMetricPrompt, winFrom, winTo, userIDs)
	if err != nil {
		return out, err
	}
	for u, q := range uq {
		out.UserPromptP90[u] = q["p90"]
	}
	return out, nil
}

// inlineOverModelBaselines 现算 3.5 杀鸡基线：同岗位 prompt P25 / completion P50。
func inlineOverModelBaselines(ctx context.Context, db *gorm.DB, from string, positions []string) (OverModelBaselines, error) {
	out := OverModelBaselines{
		PositionPromptP25:     map[string]float64{},
		PositionCompletionP50: map[string]float64{},
	}
	if len(positions) == 0 {
		return out, nil
	}
	winFrom, winTo := inlineBaselineWindow(from)
	pp, err := computeInlineQuantiles(ctx, db, inlineScopePosition, inlineMetricPrompt, winFrom, winTo, positions)
	if err != nil {
		return out, err
	}
	pc, err := computeInlineQuantiles(ctx, db, inlineScopePosition, inlineMetricCompletion, winFrom, winTo, positions)
	if err != nil {
		return out, err
	}
	for pos, q := range pp {
		out.PositionPromptP25[pos] = q["p25"]
	}
	for pos, q := range pc {
		out.PositionCompletionP50[pos] = q["p50"]
	}
	return out, nil
}

// ---- 报告即时基线 ----

// loadOutputRunawayBaselines 从历史参考窗现算输出失控基线。
func loadOutputRunawayBaselines(ctx context.Context, db *gorm.DB, from string, positions, models []string) (OutputRunawayBaselines, error) {
	return inlineOutputRunawayBaselines(ctx, db, from, positions, models)
}

// loadMultiTaskBaselines 从历史参考窗现算每人 prompt P90 基线。
func loadMultiTaskBaselines(ctx context.Context, db *gorm.DB, from string, userIDs []string) (MultiTaskBaselines, error) {
	return inlineMultiTaskBaselines(ctx, db, from, userIDs)
}

// loadOverModelBaselines 从历史参考窗现算同岗位 prompt P25 与 completion P50 基线。
func loadOverModelBaselines(ctx context.Context, db *gorm.DB, from string, positions []string) (OverModelBaselines, error) {
	return inlineOverModelBaselines(ctx, db, from, positions)
}
