package main

// main.go —— 阶段一离线用量周报工具：HTTP 拉数 → 归一化 → derive → 渲染 JSON/MD/HTML。
// 只读、零库权限、复用现有 RBAC/查询逻辑；核心派生在 internal/observability/report（纯库）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
	"github.com/example/llm-gateway/internal/observability/report/render"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

// shLoc 报告统一时区（Asia/Shanghai）；分钟桶/日切按上海时间判定（口径，与 client.asiaShanghai 一致）。
var shLoc = func() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return l
}()

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "usage-report:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := parseConfig(args)
	if err := validateConfig(cfg); err != nil {
		return err
	}
	ctx := context.Background()
	cli := client.New(cfg.BaseURL, cfg.Token, client.WithMaxLogs(cfg.MaxLogs))

	// 1) 解析范围（名称/studio/dept）
	scope, err := resolveScope(ctx, cli, cfg)
	if err != nil {
		return err
	}

	// 口径④：personal 画像/岗位人均排除 key_type='service' → 按 key_type=personal 过滤 /usage/logs。
	if cfg.Level == "personal" && cfg.KeyType == "" {
		cfg.KeyType = "personal"
	}

	// 2) 拉取调用明细（当前周期 [from,to]，按 level 设过滤；部门/工作室做服务端 dept_name/studio_id 过滤）
	rows, err := fetchScopeLogs(ctx, cli, cfg, scope, cfg.From, cfg.To)
	if err != nil {
		return err
	}
	// 个人级：把范围名解析为人名（默认是 UUID），提升可读性。
	if cfg.Level == "personal" && len(rows) > 0 && rows[0].UserName != "" {
		scope.Name = rows[0].UserName
	}

	// 3) 归一化：CallEvent + DailyMetric（口径：total_tokens=prompt+completion+reasoning）
	allCalls := toEvents(rows)
	// A1–A5 在个人口径排除 service；单元级同样只对成员调用（member）跑 A1–A5，service 不参与异常判定。
	memberCalls := nonService(allCalls)
	metrics := aggregateDaily(allCalls, cfg.Level, scope.ID) // 事实层含 member+service 分离（buildUnitFact 内拆）

	// 3b) 28 天基线物化：期前 28 完整自然日 [from-28d, from-1d]（同 level/scope 过滤），走聚合接口零明细。
	bf, bt := baselineWindow(cfg.From, cfg.To)
	buid, bdept, bstudio := "", "", ""
	switch cfg.Level {
	case "personal":
		buid = scope.ID
	case "department":
		bdept = scope.DeptID
	case "studio":
		bstudio = scope.StudioID
	}
	baselineResp, err := cli.FetchBaseline(ctx, bf, bt, buid, bdept, bstudio)
	if err != nil {
		return err
	}
	// daily → 28 天稳健基线物化（非 service 调用数按天聚合）。
	baselineMetrics := make([]report.DailyMetric, 0, len(baselineResp.Daily))
	for _, d := range baselineResp.Daily {
		baselineMetrics = append(baselineMetrics, report.DailyMetric{
			Date: d.Date, Level: cfg.Level, ScopeID: scope.ID, KeyType: "personal", ReqCount: d.ReqCount,
		})
	}
	// minute_buckets → A1/A2 历史窗口加权滑窗分位重建（非 service，按分钟聚合，零展开）。
	baselineBuckets := make([]report.MinuteBucket, 0, len(baselineResp.MinuteBuckets))
	for _, bk := range baselineResp.MinuteBuckets {
		t, e := time.ParseInLocation("2006-01-02 15:04:05", bk.Minute, shLoc)
		if e != nil {
			continue
		}
		baselineBuckets = append(baselineBuckets, report.MinuteBucket{Minute: t, Count: int(bk.Count), Cost: bk.Cost})
	}

	// 4) 模型能力（A4 有效输入预算）
	models := distinctModels(rows)
	budget := cli.ModelContextBudget(ctx, models)

	// 5) 同岗分位：花名册(岗位/studio) + 每用户效能（成员口径）
	cohort, notComputed := buildCohort(ctx, cli, cfg, scope, memberCalls)

	// 6) 装配派生
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = cfg.USDCNYRate
	now := time.Now()
	periodDays, _ := inclusiveDays(cfg.From, cfg.To)
	meta := report.Meta{
		Level:           cfg.Level,
		Period:          report.Period{From: cfg.From, To: cfg.To, Type: cfg.Period, Days: periodDays, Label: cfg.Period + " " + cfg.From + "~" + cfg.To},
		Scope:           scope,
		GeneratedAt:     now.Format(time.RFC3339),
		DataRetrievedAt: now.Format(time.RFC3339),
		APIS: []string{
			"GET /api/v1/usage/logs|scoped/logs",
			"GET /api/v1/usage/dashboard",
			"GET /api/v1/org/departments",
			"GET /api/v1/org/departments/:dept_id/members",
			"GET /api/v1/model-catalog/search",
		},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: cfg.USDCNYRate, RateSource: "config", RateAt: now.Format(time.RFC3339)},
		QA: report.QA{Alignment: []string{
			"`api_endpoints_used` 为本次流程规划使用的端点清单，不是逐请求审计日志；可选/失败端点仍可能列出。",
			"成功=`status=200`；个人画像/岗位人均排除 `key_type='service'`；部门 Service Key 块仅 `=service` 且归属当前部门。",
			"`total_tokens=prompt+completion+reasoning`（不含 cached/cache_write）；对账基准 `GET /api/v1/usage/dashboard`。",
			"28 天基线采用期前 28 完整自然日（`from-28d ~ from-1d`）物化，口径=成员调用；若基线样本不足则降级为当前周期近似并注明（仅供自一致参考）。",
			"同岗分位覆盖缺口：仅统计挂靠 active 部门的用户；`users.dept_id IS NULL` 用户不入 roster，报告 `peer.supported=false` 时需结合此缺口理解（org-roster.md §4）。",
			"A1–A5 异常在个人口径排除 service；部门/工作室口径仅对成员调用（member）判定，service 不参与异常判定。",
		}, AnomalyEnabled: true, PeerPercentileEnabled: cohort.Enabled},
	}
	in := report.ReportInput{
		Metrics: metrics, Calls: memberCalls, BaselineMetrics: baselineMetrics, BaselineBuckets: baselineBuckets, Cohort: cohort,
		LevelMeta:          scope,
		ModelContextBudget: budget,
	}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		return err
	}
	for _, nc := range notComputed {
		rep.Judgment.NotComputed = append(rep.Judgment.NotComputed, nc)
	}
	// 口径③：以 GetDashboard 为对账基准，校验 total_tokens 同源（均=SUM(prompt+completion+reasoning)，不含 cached；
	// 禁止混拼 /usage/dashboard/users* 存储列）。差异写入 QA.Alignment 说明。
	reconcileWithDashboard(ctx, cli, cfg, scope, rep)

	// 7) 渲染输出：落盘前校验稳定契约，避免生成不可复用的半成品。
	if err := rep.Validate(); err != nil {
		return fmt.Errorf("报告契约校验失败: %w", err)
	}
	if cfg.JSONOut != "" {
		if err := writeJSON(cfg.JSONOut, rep); err != nil {
			return err
		}
	}
	if cfg.MDOut != "" {
		if err := os.WriteFile(cfg.MDOut, []byte(render.RenderMD(rep)), 0o644); err != nil {
			return err
		}
	}
	if cfg.HTMLOut != "" {
		if err := os.WriteFile(cfg.HTMLOut, []byte(render.RenderHTML(rep)), 0o644); err != nil {
			return err
		}
	}
	if cfg.VickyOut != "" {
		if err := os.WriteFile(cfg.VickyOut, []byte(render.RenderVicky(rep)), 0o644); err != nil {
			return err
		}
		if cfg.VickySubmit {
			payload := vickyPayload(cfg, rep)
			if err := writeJSON(cfg.VickyOut+".payload.json", payload); err != nil {
				return err
			}
		}
	}
	if cfg.JSONOut == "" && cfg.MDOut == "" && cfg.HTMLOut == "" && cfg.VickyOut == "" {
		// 默认打印 MD
		fmt.Print(render.RenderMD(rep))
	}
	return nil
}

func validateConfig(cfg Config) error {
	if cfg.Token == "" {
		return fmt.Errorf("缺少 token（--token 或 USAGE_REPORT_TOKEN）")
	}
	if cfg.Scope == "" {
		return fmt.Errorf("缺少 scope（--scope）")
	}
	switch cfg.Level {
	case "personal", "department", "studio":
	default:
		return fmt.Errorf("无效 level %q（应为 personal|department|studio）", cfg.Level)
	}
	switch cfg.Period {
	case "week", "month", "custom":
	default:
		return fmt.Errorf("无效 period %q（应为 week|month|custom）", cfg.Period)
	}
	if cfg.KeyType != "" && cfg.KeyType != "personal" && cfg.KeyType != "service" {
		return fmt.Errorf("无效 key-type %q（应为 personal|service 或空）", cfg.KeyType)
	}
	if cfg.USDCNYRate <= 0 {
		return fmt.Errorf("fx-rate 必须大于 0")
	}
	if cfg.MaxLogs <= 0 {
		return fmt.Errorf("max-logs 必须大于 0")
	}
	if _, err := inclusiveDays(cfg.From, cfg.To); err != nil {
		return err
	}
	return nil
}

func inclusiveDays(from, to string) (int, error) {
	const layout = "2006-01-02"
	start, err := time.Parse(layout, from)
	if err != nil {
		return 0, fmt.Errorf("无效 from 日期 %q（应为 YYYY-MM-DD）", from)
	}
	end, err := time.Parse(layout, to)
	if err != nil {
		return 0, fmt.Errorf("无效 to 日期 %q（应为 YYYY-MM-DD）", to)
	}
	if end.Before(start) {
		return 0, fmt.Errorf("无效日期范围：to %s 早于 from %s", to, from)
	}
	return int(end.Sub(start).Hours()/24) + 1, nil
}

// vickyPayload 组装可 POST <VICKY_ORIGIN>/api/reports 的 JSON 载荷（category/narrative/project 由 config 指定）。
func vickyPayload(cfg Config, rep *report.Report) map[string]interface{} {
	content := render.RenderVicky(rep)
	slug := vickySlug(cfg, rep)
	return map[string]interface{}{
		"title":     vickyTitle(cfg, rep),
		"slug":      slug,
		"category":  cfg.VickyCategory,
		"narrative": cfg.VickyNarrative,
		"project":   cfg.VickyProject,
		"tag":       "META · 用量周报",
		"template":  "book",
		"content":   content,
	}
}

func vickyTitle(cfg Config, rep *report.Report) string {
	name := rep.Meta.Scope.Name
	if name == "" {
		name = rep.Meta.Scope.ID
	}
	return "用量周报 · " + name + "（" + rep.Meta.Period.Label + "）"
}

// vickySlug 生成 url-slug：<from>-usage-<level>-<scope>。
func vickySlug(cfg Config, rep *report.Report) string {
	from := rep.Meta.Period.From
	if from == "" {
		from = "daily"
	}
	lvl := sanitizeSlug(rep.Meta.Level)
	scope := sanitizeSlug(rep.Meta.Scope.ID)
	return fmt.Sprintf("%s-usage-%s-%s", from, lvl, scope)
}

// sanitizeSlug 保留小写字母/数字/连字符，其余转连字符。
func sanitizeSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '_':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "unit"
	}
	return strings.Trim(b.String(), "-")
}

func writeJSON(path string, v interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// resolveScope 解析范围 meta（name/studio_id/dept_id/岗位）。无法解析时退回 id。
func resolveScope(ctx context.Context, cli *client.Client, cfg Config) (report.Scope, error) {
	s := report.Scope{Level: cfg.Level, ID: cfg.Scope, Name: cfg.Scope}
	switch cfg.Level {
	case "department":
		depts, err := cli.FetchDepartments(ctx, "")
		if err != nil {
			return report.Scope{}, fmt.Errorf("解析部门范围失败: %w", err)
		}
		for _, d := range depts {
			if d.DeptID == cfg.Scope || d.ID == cfg.Scope {
				s.ID, s.Name = d.DeptID, d.DeptName
				s.DeptID, s.StudioID = d.DeptID, d.StudioID
				return s, nil
			}
		}
		return report.Scope{}, fmt.Errorf("未找到部门 scope %q", cfg.Scope)
	case "studio":
		studios, err := cli.FetchStudios(ctx)
		if err != nil {
			return report.Scope{}, fmt.Errorf("解析工作室范围失败: %w", err)
		}
		for _, st := range studios {
			if st.ID == cfg.Scope {
				s.ID, s.Name = st.ID, st.Name
				s.StudioID = st.ID
				return s, nil
			}
		}
		return report.Scope{}, fmt.Errorf("未找到工作室 scope %q", cfg.Scope)
	}
	return s, nil
}

// fetchScopeConcurrency 并行拉取的日窗口数上限。实测（X-Lab 3 日窗口样本，固定工作量）吞吐：
// 并发 1→2453, 2→3851, 4→4925, 8→6863 rows/s（峰值）, 12→5340（下滑，服务端吃紧）。
// 故 8 为甜点位：既最大化吞吐，又避免 12+ 压垮服务端/DB。超大部门（数十万行）据此近乎线性加速。
const fetchScopeConcurrency = 8

// fetchScopeLogs 按 level 拉取日志，做客户端侧二次过滤保证口径（部门/工作室再做服务端 dept_name/studio_id 过滤）。
// 大数据集优化：把 [from,to) 按天切成独立日窗口，并发（有限）走各自的 FetchLogs 单链，合并后按
// ts DESC, request_id DESC 重排，保持与单链结果一致的时序。单日/小窗口自动回退单链，不影响结果。
func fetchScopeLogs(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, from, to string) ([]client.LogRow, error) {
	windows, err := dailyWindows(from, to)
	if err != nil {
		return nil, err
	}
	if len(windows) <= 1 {
		return fetchScopeLogsWindow(ctx, cli, cfg, scope, from, to)
	}

	results := make([][]client.LogRow, len(windows))
	errs := make([]error, len(windows))
	sem := make(chan struct{}, fetchScopeConcurrency)
	var wg sync.WaitGroup
	for i, w := range windows {
		wg.Add(1)
		go func(idx int, wf, wt string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				errs[idx] = ctx.Err()
				return
			}
			rows, e := fetchScopeLogsWindow(ctx, cli, cfg, scope, wf, wt)
			results[idx] = rows
			errs[idx] = e
		}(i, w.from, w.to)
	}
	wg.Wait()

	var out []client.LogRow
	for i := range windows {
		if errs[i] != nil {
			return nil, fmt.Errorf("日窗口 %s~%s 拉取失败: %w", windows[i].from, windows[i].to, errs[i])
		}
		out = append(out, results[i]...)
	}
	// 保持报告对周期时序的依赖：按 ts DESC, request_id DESC 重排（与单链一致）。
	sort.SliceStable(out, func(a, b int) bool {
		ta, tb := out[a].TS, out[b].TS
		if ta == tb {
			return out[a].RequestID > out[b].RequestID
		}
		return tsLess(ta, tb)
	})
	return out, nil
}

// fetchScopeLogsWindow 在单个 [from,to) 区间内拉取日志并做客户端二次过滤（单链逻辑）。
func fetchScopeLogsWindow(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, from, to string) ([]client.LogRow, error) {
	deptID, studioID, userID := "", "", ""
	switch cfg.Level {
	case "personal":
		userID = scope.ID
	case "department":
		deptID = scope.DeptID
	case "studio":
		studioID = scope.StudioID
	}
	rows, err := cli.FetchLogs(ctx, from, to, cfg.KeyType, userID, deptID, studioID)
	if err != nil {
		return nil, err
	}
	var out []client.LogRow
	for _, r := range rows {
		switch cfg.Level {
		case "personal":
			// 个人画像排除 service；按 user_id 过滤（server 已按 key_type=personal 过滤，此处再兜底一次）。
			if cfg.Scope != "" && r.UserID != cfg.Scope {
				continue
			}
			if r.KeyType == "service" {
				continue
			}
		case "department", "studio":
			// 范围过滤由服务端 /usage/report/logs 完成：dept_id 双 ID 归一 / studio_id。
			// 客户端不再按单一 dept_id 或可变名称过滤，避免误删合法历史记录。
		}
		out = append(out, r)
	}
	return out, nil
}

// dailyWindow 表示一个 [from,to) 的日粒度闭开区间。
type dailyWindow struct{ from, to string }

// dailyWindows 把 [from,to) 按天切成 N 个不重不漏的日窗口；不足 2 天返回 nil（调用方回退单链）。
// 与报告对账周期（ReportLogs 的 start=?::date AND ts < end?::date + INTERVAL '1 day'）一致。
func dailyWindows(from, to string) ([]dailyWindow, error) {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, fmt.Errorf("invalid from %q: %w", from, err)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("invalid to %q: %w", to, err)
	}
	if t.Before(f) {
		return nil, nil
	}
	if t.Equal(f) {
		return nil, nil
	}
	var ws []dailyWindow
	for cur := f; cur.Before(t); cur = cur.AddDate(0, 0, 1) {
		next := cur.AddDate(0, 0, 1)
		ws = append(ws, dailyWindow{from: cur.Format("2006-01-02"), to: next.Format("2006-01-02")})
	}
	return ws, nil
}

// tsLess 以 UTC 归一比较两条 RFC3339 时间戳，避免不同时区表示（Z vs +08:00）导致的字典序错乱。
func tsLess(a, b string) bool {
	uA, errA := time.Parse(time.RFC3339, a)
	uB, errB := time.Parse(time.RFC3339, b)
	if errA == nil && errB == nil {
		return uA.Before(uB)
	}
	return a < b
}

func toEvents(rows []client.LogRow) []report.CallEvent {
	out := make([]report.CallEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ToCallEvent())
	}
	return out
}

// nonService 剔除 key_type='service' 的调用（A1–A5 与基线用成员口径）。
func nonService(calls []report.CallEvent) []report.CallEvent {
	out := make([]report.CallEvent, 0, len(calls))
	for _, c := range calls {
		if c.KeyType == "service" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// baselineWindow 期前 28 完整自然日：start=from-28d, end=from-1d。
func baselineWindow(from, to string) (string, string) {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		f = time.Now()
	}
	return f.AddDate(0, 0, -28).Format("2006-01-02"), f.AddDate(0, 0, -1).Format("2006-01-02")
}

// aggregateDaily 把 CallEvent 按(日期,模型,key_id,user_id,scope)聚合成 DailyMetric；total=prompt+completion+reasoning。
// level/scopeID 决定 DailyMetric.Level/ScopeID（personal=user_id / department=dept_id / studio=studio_id）。
func aggregateDaily(calls []report.CallEvent, level, scopeID string) []report.DailyMetric {
	type key struct{ date, model, keyID, keyType, userID, scopeID string }
	agg := map[key]*report.DailyMetric{}
	for _, c := range calls {
		date := c.TS.Format("2006-01-02")
		k := key{date, c.Model, c.KeyID, c.KeyType, c.UserID, scopeID}
		m := agg[k]
		if m == nil {
			m = &report.DailyMetric{Date: date, Level: level, ScopeID: scopeID, UserID: c.UserID, KeyID: c.KeyID, KeyType: c.KeyType, Model: c.Model}
			agg[k] = m
		}
		m.ReqCount++
		if c.Status == 200 {
			m.SuccessCount++
		} else {
			m.ErrorCount++
		}
		m.PromptTokens += int64(c.PromptTokens)
		m.CachedTokens += int64(c.CachedTokens)
		m.CompletionTokens += int64(c.CompletionTokens)
		m.ReasoningTokens += int64(c.ReasoningTokens)
		m.EstCostUSD += c.EstCostUSD
		m.TotalTokens += int64(c.PromptTokens) + int64(c.CompletionTokens) + int64(c.ReasoningTokens)
	}
	out := make([]report.DailyMetric, 0, len(agg))
	for _, m := range agg {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func distinctModels(rows []client.LogRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if r.Model != "" && !seen[r.Model] {
			seen[r.Model] = true
			out = append(out, r.Model)
		}
	}
	return out
}

func reportFactParts(rep *report.Report) (memberCalls, memberTokens, serviceCalls, serviceTokens int64) {
	if rep.Fact.Personal != nil {
		return rep.Fact.Personal.ReqCount, rep.Fact.Personal.TotalTokens, 0, 0
	}
	if rep.Fact.Department != nil {
		u := rep.Fact.Department
		return u.Member.ReqCount, u.Member.TotalTokens, u.ServiceKey.ReqCount, u.ServiceKey.TotalTokens
	}
	if rep.Fact.Studio != nil {
		u := rep.Fact.Studio
		return u.Member.ReqCount, u.Member.TotalTokens, u.ServiceKey.ReqCount, u.ServiceKey.TotalTokens
	}
	return 0, 0, 0, 0
}

// reconcileWithDashboard 以 GetDashboard 为对账基准：把报告 total_tokens 与之比较并记录差异（口径说明，不覆盖数据）。
// personal 范围 GetDashboard(scope=user) 会包含该用户 service key 用量，而 personal 画像排除 service → 预期差异，需注明。
func reconcileWithDashboard(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, rep *report.Report) {
	id := scope.ID
	db, err := cli.FetchDashboard(ctx, cfg.Level, id, cfg.From, cfg.To)
	if err != nil {
		rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, "对账：GetDashboard 拉取失败("+err.Error()+")，仅以 /usage/logs 聚合口径为准。")
		return
	}
	memberCalls, memberTokens, serviceCalls, serviceTokens := reportFactParts(rep)
	if db.CallCount == 0 && memberCalls+serviceCalls == 0 {
		rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, "对账(GetDashboard=基准)：报告与 GetDashboard 均为 0 调用。")
		return
	}
	if cfg.Level == "department" {
		dashboardMemberCalls := db.CallCount - db.ServiceCallCount
		dashboardMemberTokens := db.TotalTokens - db.ServiceTotalTokens
		note := fmt.Sprintf("对账(GetDashboard=基准)：Member tokens %d vs %d（差 %d），calls %d vs %d；Service tokens %d vs %d（差 %d），calls %d vs %d；Total tokens %d vs %d（差 %d）。",
			memberTokens, dashboardMemberTokens, memberTokens-dashboardMemberTokens,
			memberCalls, dashboardMemberCalls,
			serviceTokens, db.ServiceTotalTokens, serviceTokens-db.ServiceTotalTokens,
			serviceCalls, db.ServiceCallCount,
			memberTokens+serviceTokens, db.TotalTokens, memberTokens+serviceTokens-db.TotalTokens)
		rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, note)
		return
	}
	// personal/studio 当前 Dashboard 未提供可靠的 service 拆分：按现有同口径总量比较，差异如实呈现，不自动归因。
	note := fmt.Sprintf("对账(GetDashboard=基准)：报告 total_tokens=%d vs GetDashboard total_tokens=%d（差 %d，调用 %d vs %d）；两者均=SUM(prompt+completion+reasoning)，未混拼 /usage/dashboard/users* 存储列。",
		memberTokens+serviceTokens, db.TotalTokens, memberTokens+serviceTokens-db.TotalTokens,
		memberCalls+serviceCalls, db.CallCount)
	rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, note)
}
