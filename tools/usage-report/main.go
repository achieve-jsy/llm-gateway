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
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
	"github.com/example/llm-gateway/internal/observability/report/render"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "usage-report:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := parseConfig(args)
	if cfg.Token == "" {
		return fmt.Errorf("缺少 token（--token 或 USAGE_REPORT_TOKEN）")
	}
	ctx := context.Background()
	cli := client.New(cfg.BaseURL, cfg.Token)

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

	// 3b) 28 天基线物化：拉期前 28 完整自然日 [from-28d, from-1d]（同 level/scope 过滤），成员口径日聚合。
	bf, bt := baselineWindow(cfg.From, cfg.To)
	baselineRows, err := fetchScopeLogs(ctx, cli, cfg, scope, bf, bt)
	if err != nil {
		return err
	}
	baselineMetrics := aggregateDaily(nonService(toEvents(baselineRows)), cfg.Level, scope.ID)

	// 4) 模型能力（A4 有效输入预算）
	models := distinctModels(rows)
	budget := cli.ModelContextBudget(ctx, models)

	// 5) 同岗分位：花名册(岗位/studio) + 每用户效能（成员口径）
	cohort, notComputed := buildCohort(ctx, cli, cfg, scope, memberCalls)

	// 6) 装配派生
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = cfg.USDCNYRate
	meta := report.Meta{
		Level:    cfg.Level,
		Period:   report.Period{From: cfg.From, To: cfg.To, Type: cfg.Period, Label: cfg.Period + " " + cfg.From + "~" + cfg.To},
		Scope:    scope,
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: cfg.USDCNYRate, RateSource: "config", RateAt: time.Now().Format(time.RFC3339)},
		QA: report.QA{Alignment: []string{
			"成功=`status=200`；个人画像/岗位人均排除 `key_type='service'`；部门 Service Key 块仅 `=service` 且归属当前部门。",
			"`total_tokens=prompt+completion+reasoning`（不含 cached/cache_write）；对账基准 `GET /api/v1/usage/dashboard`。",
			"28 天基线采用期前 28 完整自然日（`from-28d ~ from-1d`）物化，口径=成员调用；若基线样本不足则降级为当前周期近似并注明（仅供自一致参考）。",
			"同岗分位覆盖缺口：仅统计挂靠 active 部门的用户；`users.dept_id IS NULL` 用户不入 roster，报告 `peer.supported=false` 时需结合此缺口理解（org-roster.md §4）。",
			"A1–A5 异常在个人口径排除 service；部门/工作室口径仅对成员调用（member）判定，service 不参与异常判定。",
		}, AnomalyEnabled: true, PeerPercentileEnabled: cohort.Enabled},
	}
	in := report.ReportInput{
		Metrics: metrics, Calls: memberCalls, BaselineMetrics: baselineMetrics, Cohort: cohort,
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

	// 7) 渲染输出
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

// vickyPayload 组装可 POST <VICKY_ORIGIN>/api/reports 的 JSON 载荷（category/narrative/project 由 config 指定）。
func vickyPayload(cfg Config, rep *report.Report) map[string]interface{} {
	content := render.RenderVicky(rep)
	slug := vickySlug(cfg, rep)
	return map[string]interface{}{
		"title":    vickyTitle(cfg, rep),
		"slug":     slug,
		"category": cfg.VickyCategory,
		"narrative": cfg.VickyNarrative,
		"project":  cfg.VickyProject,
		"tag":      "META · 用量周报",
		"template": "book",
		"content":  content,
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
		if err == nil {
			for _, d := range depts {
				if d.DeptID == cfg.Scope || d.ID == cfg.Scope {
					s.ID, s.Name = d.DeptID, d.DeptName
					s.DeptID, s.StudioID = d.DeptID, d.StudioID
					break
				}
			}
		}
	case "studio":
		studios, err := cli.FetchStudios(ctx)
		if err == nil {
			for _, st := range studios {
				if st.ID == cfg.Scope {
					s.ID, s.Name = st.ID, st.Name
					s.StudioID = st.ID
					break
				}
			}
		}
	}
	return s, nil
}

// fetchScopeLogs 按 level 拉取日志，做客户端侧二次过滤保证口径（部门/工作室再做服务端 dept_name/studio_id 过滤）。
func fetchScopeLogs(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, from, to string) ([]client.LogRow, error) {
	deptName, studioID, userID := "", "", ""
	switch cfg.Level {
	case "personal":
		userID = scope.ID
	case "department":
		deptName = scope.Name
	case "studio":
		studioID = scope.StudioID
	}
	rows, err := cli.FetchLogs(ctx, from, to, cfg.KeyType, userID, deptName, studioID)
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
		case "department":
			// dept_id 归一：匹配合法 dept_id 或 departments.id
			if scope.DeptID != "" && r.DeptID != scope.DeptID {
				continue
			}
		case "studio":
			// studio 由查询参数过滤；此处按 StudioName 兜底（LogRow 无 studio_id）。
			if scope.StudioID != "" && r.StudioName != scope.Name {
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
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

func reportFactTotals(rep *report.Report) (calls int64, tokens int64) {
	if rep.Fact.Personal != nil {
		return rep.Fact.Personal.ReqCount, rep.Fact.Personal.TotalTokens
	}
	if rep.Fact.Department != nil {
		return rep.Fact.Department.Member.ReqCount, rep.Fact.Department.Member.TotalTokens
	}
	if rep.Fact.Studio != nil {
		return rep.Fact.Studio.Member.ReqCount, rep.Fact.Studio.Member.TotalTokens
	}
	return 0, 0
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
	// 报告自身口径（/usage/logs 聚合，已排除 service 且 total=prompt+completion+reasoning）
	rcalls, rtokens := reportFactTotals(rep)
	if db.CallCount == 0 {
		rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, "对账：GetDashboard 返回 0 调用，跳过对账。")
		return
	}
	diff := rtokens - db.TotalTokens
	note := fmt.Sprintf("对账(GetDashboard=基准)：报告 total_tokens=%d vs GetDashboard total_tokens=%d（差 %d，调用 %d vs %d）；两者均=SUM(prompt+completion+reasoning)，未混拼 /usage/dashboard/users* 存储列。", rtokens, db.TotalTokens, diff, rcalls, db.CallCount)
	if cfg.Level == "personal" {
		note += " personal 范围 GetDashboard(scope=user) 含该用户 service key，而 personal 画像排除 key_type='service'；差异应≈service key 贡献。"
	}
	rep.Meta.QA.Alignment = append(rep.Meta.QA.Alignment, note)
}
