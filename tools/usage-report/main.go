package main

// main.go —— 阶段一离线用量周报工具：HTTP 拉数 → 归一化 → derive → 渲染 JSON/MD/HTML。
// 只读、零库权限、复用现有 RBAC/查询逻辑；核心派生在 internal/observability/report（纯库）。

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
	"github.com/example/llm-gateway/internal/observability/report/render"
	"github.com/example/llm-gateway/tools/usage-report/analysis"
	"github.com/example/llm-gateway/tools/usage-report/client"
	"github.com/example/llm-gateway/tools/usage-report/llm"
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
	cli := client.New(cfg.BaseURL, cfg.Token, client.WithMaxLogs(cfg.MaxLogs), client.WithHTTPTimeout(time.Duration(cfg.HTTPTimeout)*time.Second))

	// 批量编排（--all）：枚举指定 level 的全体对象，各出一份报告 + 一条汇总索引。
	if cfg.All {
		return runAll(ctx, cli, cfg)
	}

	// 1) 解析范围（名称/studio/dept）
	scope, err := resolveScope(ctx, cli, cfg)
	if err != nil {
		return err
	}
	_, err = runOne(ctx, cli, cfg, scope)
	return err
}

// runOne 对单个已解析 scope 生成并落盘一份报告，返回该对象的关键指标（供批量汇总索引）。
// 主体 = 数据获取 → 基线/预算/同岗 → derive → 渲染写盘；scope 解析由调用方完成。
func runOne(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope) (*BatchSummary, error) {
	// 口径④：personal 画像/岗位人均排除 key_type='service' → 按 key_type=personal 过滤 /usage/logs。
	if cfg.Level == "personal" && cfg.KeyType == "" {
		cfg.KeyType = "personal"
	}

	// LLM 模型分析客户端：仅启用时才构造（懒初始化，避免无效连接；失败走降级，绝不中断报告）。
	var llmCli *llm.Client
	if cfg.LLMEnabled {
		llmCli = llm.New(cfg.LLMBase, cfg.LLMKey, cfg.LLMModel, time.Duration(cfg.LLMTimeout)*time.Second, 0)
	}

	// 2) 数据获取：默认拉调用明细（A1–A5/模型预算/ServiceKey/Top模型保真）；
	// `-agg` 则用 member 聚合(/usage/report/members)构造事实层加法核心（零明细），明细依赖项降级。
	var rows []client.LogRow
	var metrics []report.DailyMetric
	var baselineCurrent []report.DailyMetric // -agg 当期按天序列，仅供基线 current，不污染 Metrics
	var currentBuckets []report.MinuteBucket // -agg 当期分钟桶，供 A1/A2 当期窗口判定（calls 空时）
	var memberCalls []report.CallEvent
	var memberAgg []report.MemberAgg
	var prevMemberAgg []report.MemberAgg
	if cfg.Agg {
		aggRows, aggErr := fetchMemberAggForScope(ctx, cli, cfg, scope)
		if aggErr != nil {
			return nil, aggErr
		}
		memberAgg = make([]report.MemberAgg, 0, len(aggRows))
		for _, r := range aggRows {
			memberAgg = append(memberAgg, r.ToMemberAgg())
		}
		// 个人级：把范围名解析为人名（默认是 UUID），提升可读性。
		if cfg.Level == "personal" {
			for _, m := range memberAgg {
				if m.UserID == scope.ID && m.UserName != "" {
					scope.Name = m.UserName
					break
				}
			}
		}
		// 上期数据：prevFrom = from - periodDays 天，prevTo = from - 1天。
		// 与本期同口径（同 deptID/studioID/userID），供 derive 层算周环比。
		if periodDaysVal, pdErr := inclusiveDays(cfg.From, cfg.To); pdErr == nil && periodDaysVal > 0 {
			const layout = "2006-01-02"
			fromT, fErr := time.Parse(layout, cfg.From)
			if fErr == nil {
				prevToT := fromT.AddDate(0, 0, -1)
				prevFromT := fromT.AddDate(0, 0, -periodDaysVal)
				prevCfg := cfg
				prevCfg.From = prevFromT.Format(layout)
				prevCfg.To = prevToT.Format(layout)
				prevRows, prevErr := fetchMemberAggForScope(ctx, cli, prevCfg, scope)
				if prevErr == nil {
					prevMemberAgg = make([]report.MemberAgg, 0, len(prevRows))
					for _, r := range prevRows {
						prevMemberAgg = append(prevMemberAgg, r.ToMemberAgg())
					}
				}
				// 上期拉取失败时静默忽略（derive 层收到空 PrevMemberAgg 即跳过环比）
			}
		}
		// 当期按天序列 + 分钟桶：-agg 成员聚合无逐日明细/逐调用，但 `/usage/report/baseline`
		// 仍可按天返回非 service 调用数（同 level/scope 过滤），并返回 minute_buckets。
		// 按天序列存 BaselineCurrent 供 28 天基线算当期日均；分钟桶存 CurrentBuckets 供
		// A1/A2 当期窗口判定（否则 -agg 下 calls 空、A1/A2 只能降级「样本不足」）。
		// 两者都独立字段，不污染 Metrics（agg 必须保持空，否则 buildFact 误走明细路径）。
		if cuid, cdept, cstudio := baselineScopeIDs(cfg.Level, scope); true {
			if curBaseline, curErr := cli.FetchBaseline(ctx, cfg.From, cfg.To, cuid, cdept, cstudio); curErr == nil && len(curBaseline.Daily) > 0 {
				baselineCurrent = make([]report.DailyMetric, 0, len(curBaseline.Daily))
				for _, d := range curBaseline.Daily {
					baselineCurrent = append(baselineCurrent, report.DailyMetric{
						Date: d.Date, Level: cfg.Level, ScopeID: scope.ID, KeyType: "personal", ReqCount: d.ReqCount,
					})
				}
				// 当期分钟桶 → A1/A2 当期窗口扫描（非 service 调用数/成本按分钟分桶）。
				currentBuckets = make([]report.MinuteBucket, 0, len(curBaseline.MinuteBuckets))
				for _, bk := range curBaseline.MinuteBuckets {
					if t, e := time.ParseInLocation("2006-01-02 15:04:05", bk.Minute, shLoc); e == nil {
						currentBuckets = append(currentBuckets, report.MinuteBucket{Minute: t, Count: int(bk.Count), Cost: bk.Cost})
					}
				}
			} else if cfg.Debug {
				log.Printf("agg 当期 baseline 拉取失败/为空: %v", curErr)
			}
		}
	} else {
		rows, err := fetchScopeLogs(ctx, cli, cfg, scope, cfg.From, cfg.To)
		if err != nil {
			return nil, err
		}
		// 个人级：把范围名解析为人名（默认是 UUID），提升可读性。
		if cfg.Level == "personal" && len(rows) > 0 && rows[0].UserName != "" {
			scope.Name = rows[0].UserName
		}

		// 3) 归一化：CallEvent + DailyMetric（口径：total_tokens=prompt+completion+reasoning）
		allCalls := toEvents(rows)
		// A1–A5 在个人口径排除 service；单元级同样只对成员调用（member）跑 A1–A5，service 不参与异常判定。
		memberCalls = nonService(allCalls)
		metrics = aggregateDaily(allCalls, cfg.Level, scope.ID) // 事实层含 member+service 分离（buildUnitFact 内拆）
	}

	// 3b) 28 天基线物化：期前 28 完整自然日 [from-28d, from-1d]（同 level/scope 过滤），走聚合接口零明细。
	bf, bt := baselineWindow(cfg.From, cfg.To)
	buid, bdept, bstudio := baselineScopeIDs(cfg.Level, scope)
	baselineResp, err := cli.FetchBaseline(ctx, bf, bt, buid, bdept, bstudio)
	if err != nil {
		return nil, err
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

	// 4) 模型能力（A4 有效输入预算）。agg 模式无明细→无模型名清单，A4 降 NotComputed（预算 map 留空）。
	var budget map[string]int64
	if !cfg.Agg {
		budget = cli.ModelContextBudget(ctx, distinctModels(rows))
	}

	// 5) 同岗分位：花名册(岗位/studio) + 每用户效能（成员口径）
	cohort, notComputed := buildCohort(ctx, cli, cfg, scope, memberCalls)

	// 个人报告：从同岗池 target（roster 已查到）回填岗位/工作室到 scope，供标题渲染。
	if cfg.Level == "personal" && cohort.Target.JobTitle+cohort.Target.JobCategory+cohort.Target.JobFamily != "" {
		if scope.JobCategory == "" {
			scope.JobCategory = cohort.Target.JobCategory
		}
		if scope.JobFamily == "" {
			scope.JobFamily = cohort.Target.JobFamily
		}
		if scope.JobTitle == "" {
			scope.JobTitle = cohort.Target.JobTitle
		}
		if scope.StudioID == "" {
			scope.StudioID = cohort.Target.StudioID
		}
	}

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
	// 部门报告补拉工作室级 MemberAgg（用于「工作室内占比」）：仅 agg 模式、department 级、有 studioID 时拉。
	var studioMemberAgg []report.MemberAgg
	if cfg.Agg && cfg.Level == "department" && scope.StudioID != "" {
		studioCfg := cfg
		studioCfg.From, studioCfg.To = cfg.From, cfg.To
		studioRows, studioErr := fetchMemberAggForScope(ctx, cli, studioCfg,
			report.Scope{Level: "studio", ID: scope.StudioID, StudioID: scope.StudioID})
		if studioErr == nil {
			studioMemberAgg = make([]report.MemberAgg, 0, len(studioRows))
			for _, r := range studioRows {
				studioMemberAgg = append(studioMemberAgg, r.ToMemberAgg())
			}
		}
		// 拉取失败时静默忽略，占比字段留空
	}

	// 部门/工作室报告：拉 dashboard/users 补充每用户 Top 模型（用于岗位分布主用模型列）。
	// agg 模式才拉（明细模式有 TopModels 来自明细聚合）；失败静默忽略。
	var userTopModels map[string][]report.CohortTopModel
	if cfg.Agg && (cfg.Level == "department" || cfg.Level == "studio") {
		umRows, umErr := cli.FetchUserMetrics(ctx, cfg.From, cfg.To, "", "")
		if umErr == nil && len(umRows) > 0 {
			userTopModels = make(map[string][]report.CohortTopModel, len(umRows))
			for _, row := range umRows {
				if row.UserID == "" || len(row.TopModels) == 0 {
					continue
				}
				tms := make([]report.CohortTopModel, 0, len(row.TopModels))
				for _, tm := range row.TopModels {
					tms = append(tms, report.CohortTopModel{
						Model: tm.Model, CostUSD: tm.EstCostUSD,
						CallCount: tm.CallCount, TotalTokens: tm.TotalTokens,
					})
				}
				userTopModels[row.UserID] = tms
			}
		}
	}

	// 工作室报告：构建 deptID→deptName 映射，供 derive 层填充 DeptContribution.DeptName。
	// 从部门列表接口拉取，失败静默忽略（DeptName 留空时 derive 降级使用 deptID）。
	var deptNames map[string]string
	if cfg.Agg && cfg.Level == "studio" {
		depts, dErr := cli.FetchDepartments(ctx, "active")
		if dErr == nil && len(depts) > 0 {
			deptNames = make(map[string]string, len(depts))
			for _, d := range depts {
				deptNames[d.DeptID] = d.DeptName
			}
		}
	}

	// 工作室报告（Vicky 四章模板）数据层：全公司同期成本 / 上期每用户 Top 模型 / 最近 4 周成本趋势。
	// 仅在 studio 级别填充；任一拉取失败静默忽略（字段为空即降级），不中断报告。
	var companyCostUSD float64
	var prevUserTopModels map[string][]report.CohortTopModel
	var weeklyTrend []report.WeeklyPoint
	if cfg.Level == "studio" {
		// 全公司同期成本：一页摘要「全公司占比」用（CompanyCostUSD 为 0 时 derive/渲染不下发占比）。
		if db, dErr := cli.FetchDashboard(ctx, "company", "", cfg.From, cfg.To); dErr == nil {
			companyCostUSD = db.CostEstimate
		}
		// 上期（同长度窗口）每用户 Top 模型：变化归因用。复用 prevCfg 窗口口径 [from-periodDays, from-1d]。
		if pFrom, pTo, pErr := prevWindowDates(cfg.From, cfg.To); pErr == nil {
			prRows, umErr := cli.FetchUserMetrics(ctx, pFrom, pTo, "", scope.StudioID)
			if umErr == nil && len(prRows) > 0 {
				prevUserTopModels = make(map[string][]report.CohortTopModel, len(prRows))
				for _, row := range prRows {
					if row.UserID == "" || len(row.TopModels) == 0 {
						continue
					}
					tms := make([]report.CohortTopModel, 0, len(row.TopModels))
					for _, tm := range row.TopModels {
						tms = append(tms, report.CohortTopModel{
							Model: tm.Model, CostUSD: tm.EstCostUSD,
							CallCount: tm.CallCount, TotalTokens: tm.TotalTokens,
						})
					}
					prevUserTopModels[row.UserID] = tms
				}
			}
		}
		// 最近 4 周成本趋势：从 cfg.From 往前推 4 个独立 7 天周窗口（降序，最新在前）。
		weeklyTrend = buildWeeklyTrend(ctx, cli, scope.StudioID, cfg.From)
	}

	// 程序 Service Key 聚合（/usage/report/service-keys）：仅 level != personal 拉取（personal 个人口径恒排除 service）。
	// 按 level 映射 dept/studio 过滤：department→dept_id、studio→studio_id。-agg 场景必须拉（member 聚合恒排除 service，
	// 否则 ServiceKey 块降级为空）；明细模式亦拉以补齐「ServiceKey 表」数据。拉取失败静默忽略（块降级 + Notes）。
	var serviceKeys []report.ServiceKeyAgg
	if cfg.Level != "personal" {
		svcDeptID, svcStudioID := "", ""
		switch cfg.Level {
		case "department":
			svcDeptID = scope.DeptID
		case "studio":
			svcStudioID = scope.StudioID
		}
		if skRows, svcErr := cli.FetchServiceKeys(ctx, cfg.From, cfg.To, svcDeptID, svcStudioID); svcErr == nil {
			serviceKeys = make([]report.ServiceKeyAgg, 0, len(skRows))
			for _, r := range skRows {
				serviceKeys = append(serviceKeys, report.ServiceKeyAgg{
					KeyName:         r.KeyName,
					OwnerName:       r.OwnerName,
					ReqCount:        r.ReqCount,
					SuccessCount:    r.SuccessCount,
					ErrorCount:      r.ErrorCount,
					TotalTokens:     r.TotalTokens,
					CostUSD:         r.CostUSD,
					ModelCount:      r.ModelCount,
					ActiveDays:      r.ActiveDays,
					PrevReqCount:    r.PrevReqCount,
					PrevTotalTokens: r.PrevTotalTokens,
					PrevCostUSD:     r.PrevCostUSD,
					TopModel:        r.TopModel,
				})
			}
		}
	}

	// 匿名横比数据（部门/工作室）：全公司每用户口径（无 dept/studio 过滤），供 derive 按 DeptName 分组算
	// 各部门「单位活跃人成本」并给出等级/工作室的匿名分位（judge.PeerUnitCost）。拉取失败静默忽略（横比降级）。
	var companyUserMetrics []report.CompanyUserMetric
	if cfg.Level == "department" || cfg.Level == "studio" {
		if umRows, umErr := cli.FetchUserMetricsAll(ctx, cfg.From, cfg.To); umErr == nil && len(umRows) > 0 {
			companyUserMetrics = make([]report.CompanyUserMetric, 0, len(umRows))
			for _, r := range umRows {
				companyUserMetrics = append(companyUserMetrics, report.CompanyUserMetric{
					UserName:   r.UserName,
					DeptName:   r.DeptName,
					EstCostUSD: r.EstCostUSD,
					CallCount:  r.CallCount,
					ActiveDays: r.ActiveDays,
				})
			}
		}
	}

	// 预算执行（/usage/report/budget-exec）：仅 studio 级别（Vicky 工作室「一页摘要」预算执行行）。
	// month 取报告期末日所在月（cfg.To）。拉取失败/不可得时 makeBudgetExec 置 Supported=false 降级。
	var budgetExecReport *report.BudgetExec
	if cfg.Level == "studio" {
		budgetExecReport = makeBudgetExec(ctx, cli, scope.StudioID, cfg.To)
	}

	in := report.ReportInput{
		Metrics: metrics, Calls: memberCalls, BaselineMetrics: baselineMetrics, BaselineBuckets: baselineBuckets, Cohort: cohort,
		LevelMeta:          scope,
		ModelContextBudget: budget,
		MemberAgg:          memberAgg,
		PrevMemberAgg:      prevMemberAgg,
		StudioMemberAgg:    studioMemberAgg,
		BaselineCurrent:    baselineCurrent,    // -agg 当期按天序列，仅供基线 current
		CurrentBuckets:     currentBuckets,     // -agg 当期分钟桶，供 A1/A2 当期窗口判定
		UserTopModels:      userTopModels,      // 部门/工作室: 每用户 top 模型（岗位分布/Top变化人员）
		DeptNames:          deptNames,          // 工作室级别: deptID→deptName 映射（DeptContribution.DeptName 用）
		CompanyCostUSD:     companyCostUSD,     // 工作室: 全公司同期成本（一页摘要占比）
		PrevUserTopModels:  prevUserTopModels,  // 工作室: 上期每用户 Top 模型（变化归因）
		WeeklyTrend:        weeklyTrend,        // 工作室: 最近4周成本趋势
		ServiceKeys:        serviceKeys,        // 程序 Service Key 聚合（部门/工作室: 每 Key 一行，供 ServiceKey 块装配与表渲染）
		CompanyUserMetrics: companyUserMetrics, // 全公司每用户口径（部门/工作室: 匿名横比数据）
		BudgetExec:         budgetExecReport,   // 工作室: 某月预算执行（一页摘要「预算执行」行；nil 降级 Supported=false）
	}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		return nil, err
	}
	for _, nc := range notComputed {
		rep.Judgment.NotComputed = append(rep.Judgment.NotComputed, nc)
	}
	// 两阶段按窗精准拉取：后续主因列点位需要 AnomalyHit.Dimensions["user_id"]。A1/A2 命中但尚未回填
	// user_id 时（如粒度/降级使 derive 无法即时归因），按命中窗口 timestamptz 子窗半开区间精准拉取
	// （be-eng t10 的 start_ts/end_ts，只取窗口内几十条）并归因回填，再重跑主因关联。
	// 拉取失败/为空时静默降级（主因列维持「待确认」），绝不中断报告。
	backfillAnomalyWindowUser(ctx, cli, cfg, scope, rep)
	// 口径③：以 GetDashboard 为对账基准，校验 total_tokens 同源（均=SUM(prompt+completion+reasoning)，不含 cached；
	// 禁止混拼 /usage/dashboard/users* 存储列）。差异写入 QA.Alignment 说明。
	reconcileWithDashboard(ctx, cli, cfg, scope, rep)

	// 7) LLM 模型分析注入：仅在启用且客户端已构造时执行；成功回填 LLM 章节，失败/未启用安全降级，
	//    绝不 panic、绝不中断报告。降级说明（Note）也回填，供渲染层展示原因；错误仅在 debug 下记录。
	if cfg.LLMEnabled && llmCli != nil {
		if ma, err := analysis.Generate(ctx, llmCli, cfg.Level, rep, cfg.USDCNYRate); ma != nil {
			rep.ModelAnalysis = ma // 成功=LLM 章节；失败=带降级 Note 的降级对象
			if err != nil && cfg.Debug {
				log.Printf("model analysis skipped: %v", err)
			}
		}
	}

	// 8) 渲染输出：落盘前校验稳定契约，避免生成不可复用的半成品。
	if err := rep.Validate(); err != nil {
		return nil, fmt.Errorf("报告契约校验失败: %w", err)
	}
	if cfg.JSONOut != "" {
		if err := writeJSON(cfg.JSONOut, rep); err != nil {
			return nil, err
		}
	}
	if cfg.MDOut != "" {
		if err := os.WriteFile(cfg.MDOut, []byte(render.RenderMD(rep)), 0o644); err != nil {
			return nil, err
		}
	}
	if cfg.HTMLOut != "" {
		if err := os.WriteFile(cfg.HTMLOut, []byte(render.RenderHTML(rep)), 0o644); err != nil {
			return nil, err
		}
	}
	if cfg.VickyOut != "" {
		if err := os.WriteFile(cfg.VickyOut, []byte(render.RenderVicky(rep)), 0o644); err != nil {
			return nil, err
		}
		if cfg.VickySubmit {
			payload := vickyPayload(cfg, rep)
			if err := writeJSON(cfg.VickyOut+".payload.json", payload); err != nil {
				return nil, err
			}
		}
	}
	// 单对象模式且未指定任何输出路径 → 默认打印 MD（批量模式的汇总索引由 runAll 负责）。
	if !cfg.All && cfg.JSONOut == "" && cfg.MDOut == "" && cfg.HTMLOut == "" && cfg.VickyOut == "" {
		fmt.Print(render.RenderMD(rep))
	}
	return buildSummary(rep), nil
}

// BatchSummary 批量编排中单个对象的报告关键指标摘要（供汇总索引）。
type BatchSummary struct {
	Level        string  `json:"level"`
	ScopeID      string  `json:"scope_id"`
	ScopeName    string  `json:"scope_name"`
	ReqCount     int64   `json:"req_count"`
	SuccessCount int64   `json:"success_count"`
	ErrorCount   int64   `json:"error_count"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	SuccessRate  float64 `json:"success_rate"`
	CostPer1k    float64 `json:"cost_per_1k_tokens"`
	KeyAnomalies int     `json:"key_anomalies"` // A1–A5 命中数（P1+P2），便于汇总里定位高风险对象
}

// buildSummary 从派生报告提取该对象的关键指标；无事实层时返回零值 summary。
func buildSummary(rep *report.Report) *BatchSummary {
	s := &BatchSummary{
		Level:     rep.Meta.Level,
		ScopeID:   rep.Meta.Scope.ID,
		ScopeName: rep.Meta.Scope.Name,
	}
	f := rep.Fact
	switch {
	case f.Personal != nil:
		s.ReqCount, s.SuccessCount, s.ErrorCount = f.Personal.ReqCount, f.Personal.SuccessCount, f.Personal.ErrorCount
		s.TotalTokens, s.CostUSD = f.Personal.TotalTokens, f.Personal.EstCostUSD
	case f.Department != nil:
		m := f.Department.Member
		s.ReqCount, s.SuccessCount, s.ErrorCount = m.ReqCount, m.SuccessCount, m.ErrorCount
		s.TotalTokens, s.CostUSD = m.TotalTokens, m.EstCostUSD
	case f.Studio != nil:
		m := f.Studio.Member
		s.ReqCount, s.SuccessCount, s.ErrorCount = m.ReqCount, m.SuccessCount, m.ErrorCount
		s.TotalTokens, s.CostUSD = m.TotalTokens, m.EstCostUSD
	}
	e := rep.Judgment.Efficiency
	s.SuccessRate = e.SuccessRate
	s.CostPer1k = e.CostPer1kTokens
	for _, a := range rep.Judgment.Anomalies {
		if a.Hit && (a.Severity == "P1" || a.Severity == "P2") {
			s.KeyAnomalies++
		}
	}
	return s
}

// ---- 批量编排（阶段4）----

// runAll 枚举指定 level 的全体对象，逐对象生成报告并输出一份汇总索引。
// 批量默认建议 `-agg`（成员聚合，零明细防 OOM）；明细派生的 A1–A5/模型归因在 agg 下按既有降级处理。
// 输出：每对象一份 <outdir>/<level>_<slug>_<from>_<to>.md；汇总索引写 <outdir>/summary_<from>_<to>.md（+ .json）。
func runAll(ctx context.Context, cli *client.Client, cfg Config) error {
	scopes, err := enumerateScopes(ctx, cli, cfg)
	if err != nil {
		return err
	}
	if len(scopes) == 0 {
		return fmt.Errorf("未枚举到任何对象（level=%s）", cfg.Level)
	}

	outDir := cfg.OutDir
	if outDir == "" {
		outDir = cfg.MDOut
	}
	if outDir == "" {
		outDir = "."
	}
	// 汇总索引默认路径
	summaryMD := cfg.SummaryMD
	if summaryMD == "" {
		summaryMD = filepath.Join(outDir, "summary_"+cfg.From+"_"+cfg.To+".md")
	}

	fmt.Printf("批量编排：level=%s 对象数=%d 输出目录=%s\n", cfg.Level, len(scopes), outDir)
	var summaries []*BatchSummary
	ok, failed := 0, 0
	for _, sc := range scopes {
		// 每个对象一份独立报告，输出路径按约定命名（单对象时若用户给了 MDOut 则覆盖为专属名）。
		scCfg := cfg
		scCfg.Scope = sc.ID
		scCfg.MDOut = filepath.Join(outDir, batchFileName(cfg.Level, sc, cfg.From, cfg.To))
		// 单对象模式下的 JSONOut/HTMLOut 在批量下不逐对象生成（避免大量文件）；汇总索引承载明细。
		scCfg.JSONOut, scCfg.HTMLOut, scCfg.VickyOut = "", "", ""
		scCfg.All = false
		sum, rerr := runOne(ctx, cli, scCfg, sc)
		if rerr != nil {
			failed++
			fmt.Printf("  [x] %s/%s: %v\n", cfg.Level, sc.ID, rerr)
			continue
		}
		summaries = append(summaries, sum)
		ok++
		numPrefix := ""
		if sum.KeyAnomalies > 0 {
			numPrefix = fmt.Sprintf(" (A1-A5命中%d)", sum.KeyAnomalies)
		}
		fmt.Printf("  [o] %s/%s: req=%d cost=%.4f succ=%.1f%%%s\n", cfg.Level, sum.ScopeName, sum.ReqCount, sum.CostUSD, sum.SuccessRate*100, numPrefix)
	}

	// 汇总索引：写 MD + JSON。
	md := renderBatchSummary(cfg, summaries, ok, failed)
	if err := os.WriteFile(summaryMD, []byte(md), 0o644); err != nil {
		return fmt.Errorf("写汇总索引失败: %w", err)
	}
	summaryJSON := summaryMD[:len(summaryMD)-len(filepath.Ext(summaryMD))] + ".json"
	if err := writeJSON(summaryJSON, map[string]interface{}{
		"level": cfg.Level, "from": cfg.From, "to": cfg.To,
		"total": len(scopes), "ok": ok, "failed": failed,
		"items": summaries,
	}); err != nil {
		return fmt.Errorf("写汇总 JSON 失败: %w", err)
	}
	fmt.Printf("汇总索引 %d/%d 对象：%s\n", ok, len(scopes), summaryMD)
	return nil
}

// enumerateScopes 按 level 枚举全部对象（scope）。personal 枚举各活部门的成员；department/studio 枚举组织单元。
func enumerateScopes(ctx context.Context, cli *client.Client, cfg Config) ([]report.Scope, error) {
	switch cfg.Level {
	case "department":
		depts, err := cli.FetchDepartments(ctx, "active")
		if err != nil {
			return nil, err
		}
		out := make([]report.Scope, 0, len(depts))
		for _, d := range depts {
			out = append(out, report.Scope{Level: "department", ID: d.DeptID, Name: d.DeptName, DeptID: d.DeptID, StudioID: d.StudioID})
		}
		return out, nil
	case "studio":
		studios, err := cli.FetchStudios(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]report.Scope, 0, len(studios))
		for _, st := range studios {
			out = append(out, report.Scope{Level: "studio", ID: st.ID, Name: st.Name, StudioID: st.ID})
		}
		return out, nil
	case "personal":
		// 枚举所有活部门的成员去重；dept_id 双 ID 归一（SSO id 或 uuid），以 user_id 去重。
		depts, err := cli.FetchDepartments(ctx, "active")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		out := make([]report.Scope, 0)
		for _, d := range depts {
			members, merr := cli.FetchMembers(ctx, d.DeptID)
			if merr != nil {
				continue // 403/404/429 跳过该部门成员，不中断整体
			}
			for _, mb := range members {
				if mb.ID == "" || seen[mb.ID] {
					continue
				}
				seen[mb.ID] = true
				out = append(out, report.Scope{Level: "personal", ID: mb.ID, Name: mb.Name, DeptID: d.DeptID, StudioID: d.StudioID})
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("批量不支持 level %q", cfg.Level)
	}
}

func batchFileName(level string, sc report.Scope, from, to string) string {
	id := slugID(sc.ID)
	if id == "" {
		// 非 ASCII 的 dept_id（如中文部门名）slug 为空 → 用短 hash 兜底，避免多个对象覆盖同名 "unit" 文件。
		id = "id-" + shortHash(sc.ID)
	}
	name := ""
	if sc.Name != "" && sc.Name != sc.ID {
		if n := slugID(sc.Name); n != "" {
			name = n + "_"
		}
	}
	return fmt.Sprintf("%s_%s%s_%s_%s.md", level, name, id, from, to)
}

// shortHash 取字符串 SHA-256 前 8 字节的十六进制（短唯一标识，供非 ASCII scope id 作文件名兜底）。
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:4])
}

// slugID 把 scope id/name 转成安全文件名的 slug，**保留连字符**（dept_id 形如 d-dev/u-uuid，
// 连字符是标识符的一部分，不容丢失；sanitizeSlug 会把 '-' 丢弃，故这里单独处理）。
// 无可用字符时返回空串，由调用方决定兜底。
func slugID(s string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case r == '-' || r == ' ' || r == '/' || r == '_':
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func renderBatchSummary(cfg Config, items []*BatchSummary, ok, failed int) string {
	var b strings.Builder
	b.WriteString("# 用量报告批量汇总 · " + cfg.Level + " · " + cfg.From + "~" + cfg.To + "\n\n")
	b.WriteString("> 成功 " + itoa(ok) + " / 失败 " + itoa(failed) + "；级别 " + cfg.Level + "；周期 " + cfg.From + " ~ " + cfg.To + "\n")
	if cfg.Agg {
		b.WriteString("> 数据源：成员聚合(`-agg`)，A1–A5/模型归因/ServiceKey 等明细依赖项按既有降级处理。\n")
	}
	b.WriteString("\n| 对象 | 请求数 | 成功 | 失败 | 成功率 | 总Token | 成本USD | 每1k成本 | A1-A5命中 |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, it := range items {
		name := it.ScopeName
		if name == "" {
			name = it.ScopeID
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.1f%% | %d | %.4f | %.4f | %d |\n",
			name, it.ReqCount, it.SuccessCount, it.ErrorCount, it.SuccessRate*100, it.TotalTokens, it.CostUSD, it.CostPer1k, it.KeyAnomalies)
	}
	return b.String()
}

func validateConfig(cfg Config) error {
	if cfg.Token == "" {
		return fmt.Errorf("缺少 token（--token 或 USAGE_REPORT_TOKEN）")
	}
	// 批量编排（--all）无需 scope；单对象模式必须提供 scope。
	if !cfg.All && cfg.Scope == "" {
		return fmt.Errorf("缺少 scope（--scope；或加 --all 走批量编排）")
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

// fetchScopeLogsSubWindow 按 timestamptz 子窗半开区间 [startTS, endTS) 精准拉取调用行（detail 两阶段命中窗口用）。
// 依赖 be-eng t10：/usage/report/logs 支持 start_ts/end_ts（RFC3339 或 2006-01-02 15:04:05），
// 服务端按 call_logs.ts>=?::timestamptz AND ts<?::timestamptz 过滤，只返回窗口内几十条而非整天数万条。
// 与 fetchScopeLogsWindow 同口径：personal 传 user_id；department 传 dept_id；studio 传 studio_id。
func fetchScopeLogsSubWindow(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, startTS, endTS string) ([]client.LogRow, error) {
	userID, deptID, studioID := "", "", ""
	switch cfg.Level {
	case "personal":
		userID = scope.ID
	case "department":
		deptID = scope.DeptID
	case "studio":
		studioID = scope.StudioID
	}
	rows, err := cli.FetchLogsSubWindow(ctx, startTS, endTS, cfg.KeyType, userID, deptID, studioID)
	if err != nil {
		return nil, err
	}
	var out []client.LogRow
	for _, r := range rows {
		switch cfg.Level {
		case "personal":
			if cfg.Scope != "" && r.UserID != cfg.Scope {
				continue
			}
			if r.KeyType == "service" {
				continue
			}
		case "department", "studio":
			// 范围过滤由服务端 /usage/report/logs 完成：dept_id 双 ID 归一 / studio_id。
		}
		out = append(out, r)
	}
	return out, nil
}

// 与 fetchScopeLogsWindow 同口径：personal 传 user_id；department 传 dept_id（服务端做双 ID 归一）；studio 传 studio_id。
// 返回的 MemberAgg 只含加法/计数子集（无日期/模型/key 维度），供人工层「按人统计 + 逐层横向对比」与事实层加法核心；
// A1–A5/模型归因/ServiceKey/TopModels 等明细依赖项在 agg 模式下由 derive 降级（NotComputed/空+注记）。
func fetchMemberAggForScope(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope) ([]client.MemberAggRow, error) {
	userID, deptID, studioID := "", "", ""
	switch cfg.Level {
	case "personal":
		userID = scope.ID
	case "department":
		deptID = scope.DeptID
	case "studio":
		studioID = scope.StudioID
	}
	rows, err := cli.FetchMemberAgg(ctx, cfg.From, cfg.To, userID, deptID, studioID)
	if err != nil {
		return nil, err
	}
	// personal 兜底：/usage/report/members 已按 user_id 过滤并排除 service，此处再确保只留目标人。
	if cfg.Level == "personal" && cfg.Scope != "" {
		var out []client.MemberAggRow
		for _, r := range rows {
			if r.UserID == cfg.Scope {
				out = append(out, r)
			}
		}
		return out, nil
	}
	return rows, nil
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

// getTopUserFromRows 复用 derive 的窗口内成本归因逻辑，对给定调用行集（已按命中窗口拉取）取成本最高 user。
// 供两阶段「按需精准拉取」使用：detail 模式不再依赖整期全量逐调用，命中后按窗口拉取再归因。
func getTopUserFromRows(rows []client.LogRow) (uid string, costShare float64) {
	return derive.TopUserByCost(toEvents(rows))
}

// backfillAnomalyWindowUser 两阶段「按窗精准拉取归因」装配入口（在 derive.Derive 之后调用）。
// 阶段一：A1/A2 判定（含 bucket 模式）产出 AnomalyHit.WindowStart/WindowEnd（结构化命中窗口）；
// 阶段二：命中但 Dimensions["user_id"] 仍空的 A1/A2，用窗口时间 timestamptz 子窗半开区间
//
//	[WindowStart, WindowEnd) 精准拉取该窗口调用行（be-eng t10 的 start_ts/end_ts，只取窗口内几十条），
//	按成本归因到 top user 回填 user_id/user_id_basis/user_cost_share，
//	最后重跑 derive.AnomalyAttribution 使 TopChanged 主因列点亮。
//
// 失败/为空/无归属时静默降级（主因列维持「待确认」），绝不中断报告。
func backfillAnomalyWindowUser(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, rep *report.Report) {
	if rep == nil || len(rep.Judgment.Anomalies) == 0 || cli == nil {
		return
	}
	changed := false
	for i := range rep.Judgment.Anomalies {
		a := &rep.Judgment.Anomalies[i]
		if !a.Hit || (a.Rule != "A1" && a.Rule != "A2") {
			continue
		}
		if a.WindowStart.IsZero() || a.WindowEnd.IsZero() {
			continue
		}
		if a.Dimensions == nil {
			a.Dimensions = map[string]string{}
		}
		if a.Dimensions["user_id"] != "" {
			continue // 已即时归因（detail 全量已有）
		}
		// 窗口时间沿用报告时区（shLoc=Asia/Shanghai）；用 timestamptz 子窗半开区间精准拉取（依赖 be-eng t10 的 start_ts/end_ts），
		// 服务端只返回 [WindowStart, WindowEnd) 内的行（几十条），不再拉整天（可达 15 万条）。
		// 注意：务必用带显式时区偏移的 RFC3339（+08:00）而非裸 "2006-01-02 15:04:05"——服务端 parseReportLogsTS 用
		// time.Parse（UTC），裸格式会把 Shanghai 的 08:58 误解析成 08:58 UTC（=16:58 +08:00），子窗整体漂移 8 小时。
		ws, we := a.WindowStart.In(shLoc), a.WindowEnd.In(shLoc)
		startTS := ws.Format(time.RFC3339)
		endTS := we.Format(time.RFC3339)
		rows, err := fetchScopeLogsSubWindow(ctx, cli, cfg, scope, startTS, endTS)
		if err != nil || len(rows) == 0 {
			if cfg.Debug {
				log.Printf("two-stage subwindow pull: rule %s window %s~%s: %d rows, err=%v", a.Rule, startTS, endTS, len(rows), err)
			}
			continue
		}
		// 子窗已按半开区间返回，端到端复检剔 service（成员口径同 A1–A5），归因到窗口成本最高 user。
		windowCalls := make([]report.CallEvent, 0, len(rows))
		for _, r := range rows {
			ce := r.ToCallEvent()
			if ce.KeyType == "service" {
				continue // service 不参与异常判定/归因（同 derive 对 in.Calls 的 nonService 前提）
			}
			windowCalls = append(windowCalls, ce)
		}
		if len(windowCalls) == 0 {
			continue
		}
		uid, share := derive.TopUserByCost(windowCalls)
		if uid == "" {
			continue
		}
		a.Dimensions["user_id"] = uid
		a.Dimensions["user_id_basis"] = "window_max_cost"
		a.Dimensions["user_cost_share"] = strconv.FormatFloat(share, 'f', 2, 64)
		a.Evidence = append(a.Evidence, report.Evidence{Field: "window_top_user", Value: uid + "(占窗口成本" + strconv.FormatFloat(share*100, 'f', 1, 64) + "%)"})
		changed = true
	}
	if changed {
		// 重跑主因关联，把刚回填的 user_id 映射到 TopChangedMember.AnomalyCause。
		derive.AnomalyAttribution(rep)
	}
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

// baselineScopeIDs 按 level 从 scope 提取 /usage/report/baseline 的过滤参数（user_id/dept_id/studio_id）。
func baselineScopeIDs(level string, scope report.Scope) (userID, deptID, studioID string) {
	switch level {
	case "personal":
		return scope.ID, "", ""
	case "department":
		return "", scope.DeptID, ""
	case "studio":
		return "", "", scope.StudioID
	default:
		return scope.ID, scope.DeptID, scope.StudioID
	}
}

// prevWindowDates 计算与本期同长度窗口的上期窗口范围：[from-periodDays, from-1d]。
// 与 main.go 中 prevCfg 的构造口径一致（供 Vicky 变化归因「上期每用户 Top 模型」使用）。
func prevWindowDates(from, to string) (string, string, error) {
	periodDays, err := inclusiveDays(from, to)
	if err != nil {
		return "", "", err
	}
	const layout = "2006-01-02"
	fromT, err := time.Parse(layout, from)
	if err != nil {
		return "", "", err
	}
	return fromT.AddDate(0, 0, -periodDays).Format(layout), fromT.AddDate(0, 0, -1).Format(layout), nil
}

// buildWeeklyTrend 构建最近 4 周（截至 cfg.From 前一天）的工作室成本/请求趋势序列，降序（最新在前）。
// 每个窗口为 7 天不重叠区间（[from-7(i+1)+1d, from-7i-1d]）；任一窗口拉取失败则跳过该点，不中断整体。
func buildWeeklyTrend(ctx context.Context, cli *client.Client, studioID, from string) []report.WeeklyPoint {
	const layout = "2006-01-02"
	fromT, err := time.Parse(layout, from)
	if err != nil {
		return nil
	}
	out := make([]report.WeeklyPoint, 0, 4)
	for i := 0; i < 4; i++ {
		end := fromT.AddDate(0, 0, -1-7*i)
		start := end.AddDate(0, 0, -6)
		db, dErr := cli.FetchDashboard(ctx, "studio", studioID, start.Format(layout), end.Format(layout))
		if dErr != nil {
			continue // 拉取失败静默忽略，该周点不产出
		}
		out = append(out, report.WeeklyPoint{
			WeekStart: start.Format(layout),
			WeekEnd:   end.Format(layout),
			CostUSD:   db.CostEstimate,
			ReqCount:  db.CallCount,
		})
	}
	return out
}

// makeBudgetExec 拉取工作室某月预算执行：月取报告期末日（to）所在月。失败/不可得置 Supported=false 降级。
func makeBudgetExec(ctx context.Context, cli *client.Client, studioID, to string) *report.BudgetExec {
	const layout = "2006-01-02"
	toT, err := time.Parse(layout, to)
	if err != nil {
		return &report.BudgetExec{Supported: false, Note: "报告期末日无法解析"}
	}
	// 取该日所在自然月（报告窗口为 7 天，跨月时以月末日所在月为预算考核月）。
	month := time.Date(toT.Year(), toT.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01")
	be, err := cli.FetchBudgetExec(ctx, "studio", studioID, month)
	if err != nil {
		return &report.BudgetExec{Supported: false, ScopeType: "studio", ScopeID: studioID, Month: month, Note: "预算执行拉取失败"}
	}
	return &report.BudgetExec{
		Supported:     be.Supported,
		ScopeType:     be.ScopeType,
		ScopeID:       be.ScopeID,
		Month:         be.Month,
		MonthLimitUSD: be.MonthLimitUSD,
		ActualCostUSD: be.ActualCostUSD,
		Percent:       be.Percent,
		Note:          be.Note,
	}
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
