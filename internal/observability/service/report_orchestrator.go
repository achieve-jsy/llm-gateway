package service

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// 本文件实现 report 服务层 Orchestrator：把数据层原始数据真正串到 report.Assemble，
// 产出一份已 Validate 的 *Report（t9/api-eng 的 usage/report API 直接消费）。
//
// 分层口径（关键）：
//   - report 是纯库，**不能 import service**（循环依赖）。因此判定五类(§4)/费用归因(§5)/严重异常3.6(§8)
//     都在本 service 层跑完，以 report 级结果（UsageIssues / SafetyAnomaly / CostBreakdown / DataQuality）
//     填入 report.AssembleInput，再交给 report.Assemble 做最终组装 + Validate。
//   - 本文件只负责「编排」，不碰持久化：report_snapshot 缓存读写、baseline 表 → 规则入参的 DB 读取，
//     都是数据层/持久化层的职责（t9/api-eng 管）。因此原始输入经 ReportDataProvider 接口注入，
//     以便 e2e 测试用内存假数据驱动完整管线；DB 实现后面接入。
//
// 原子口径：
//   - total_tokens = prompt + completion + reasoning（不含 cached）。本编排不计算 cost_per_1m/token 占比分母，
//     CostBreakdown 仅按 est_cost_usd 重新聚合（输入/缓存/输出三分）。
//   - issue_cost_ratio（问题消耗占比，五种归因合计/总成本）与 unrelated_cost_ratio（3.6/总成本）
//     **分列、永不相加**：前者进四象限 Y，后者只进安全异常块/描边。

// ReportParams 是 GetReport 的输入：报告范围/周期/币种 + 原始数据源。
type ReportParams struct {
	Level       string
	Scope       report.Scope
	Period      report.Period
	Currency    report.Currency
	RuleVersion string

	// DataSource 提供报告所需的原始数据+基线+档位解析（DB 或内存实现；持久化由 t9/api-eng 管）。
	DataSource ReportDataProvider

	// Semantic 语义增强层网关（nil=关闭，报告行为与 P1 一致；见 semantic_upgrade.go）。
	Semantic *SemanticGateway

	// Concurrency 部门/工作室成员并发分发协程数（默认 0 = 自动选择 4-8，bounded worker pool；1 = 强制串行）。
	Concurrency int
}

// ReportUser 是报告范围内的一个成员。
type ReportUser struct {
	UserID   string
	Name     string
	Position string
	DeptID   string
	DeptName string
}

// ReportDataProvider 抽象出 raw 数据层，供编排器取数。这是 DB 与内存测试的最小公共面。
type ReportDataProvider interface {
	// Users 返回 level 范围内的人（personal：本人；department：成员；studio：成员）。
	Users(ctx context.Context, level string, scope report.Scope) ([]ReportUser, error)
	// CallRows 返回某人在 [from,to] 的调用行（用于会话装配：前缀污染/挤牙膏/多任务）。
	CallRows(ctx context.Context, userID, from, to string) ([]CallLogRow, error)
	// OutputRunawayRows 返回某人的逐请求输出失控判定行（需要 completion_tokens/岗位）。
	OutputRunawayRows(ctx context.Context, userID, from, to string) ([]OutputRunawayRow, error)
	// OverModelRows 返回某人的逐请求选型判定行。
	OverModelRows(ctx context.Context, userID, from, to string) ([]OverModelRow, error)
	// DataQuality 返回报告层数据质量覆盖率。
	DataQuality(ctx context.Context, level string, scope report.Scope) (report.DataQuality, error)
	// OutputRunawayBaselines 返回同岗/同模型 P25/P75 基线（全公司同岗位池）。
	OutputRunawayBaselines(ctx context.Context, positions, models []string) (OutputRunawayBaselines, error)
	// MultiTaskBaselines 返回用户 prompt P90 基线。
	MultiTaskBaselines(ctx context.Context, userIDs []string) (MultiTaskBaselines, error)
	// OverModelBaselines 返回同岗 P25 / completion P50 基线。
	OverModelBaselines(ctx context.Context, positions []string) (OverModelBaselines, error)
	// Resolver 返回杀鸡用牛刀的档位解析器。
	Resolver(ctx context.Context) (OverModelResolver, error)
	// ModelTier 返回某模型的归一档位（用于 CostBreakdown.ByModelTier）。
	ModelTier(ctx context.Context, model string) (ModelTierInfo, error)
	// OffhoursRows 返回某人 3.6 判定的原始行。
	OffhoursRows(ctx context.Context, userID, from, to string) ([]OffhoursCallRow, error)
	// OffhoursFilings 返回已备案窗口（排除优先）。
	OffhoursFilings(ctx context.Context) ([]OffhoursFilingWindow, error)
	// TraceBodies 返回一批 request_id 的 call_traces 正文（语义增强层取证；无正文源返回空 map）。
	TraceBodies(ctx context.Context, reqIDs []string) (map[string]string, error)
}

// GetReport 是 report 装配引擎（provider 注入式，可测可替换）：把 ReportDataProvider 提供的原始数据
// 串成 数据层→规则→归因→严重异常→Assemble 的完整管线，产出一份已 Validate 的 *Report。
//
// handler 调用链：ReportHandler → ReportService.GetReport(level, scopeID, periodStart, periodType)
// → （内部）GetReport(ctx, ReportParams{DataSource: dbReportProvider})。
// 便捷周期入口见 ReportService.GetReport(level, scopeID, periodStart, periodType)（DB 后端）。
func GetReport(ctx context.Context, params ReportParams) (*report.Report, error) {
	if params.DataSource == nil {
		return nil, fmt.Errorf("report orchestrator: DataSource must not be nil")
	}
	meta := report.Meta{
		Level: params.Level, Scope: params.Scope, Period: params.Period,
		Currency: params.Currency, RuleVersion: params.RuleVersion,
	}
	dq, err := params.DataSource.DataQuality(ctx, params.Level, params.Scope)
	if err != nil {
		return nil, fmt.Errorf("report orchestrator: data quality: %w", err)
	}
	users, err := params.DataSource.Users(ctx, params.Level, params.Scope)
	if err != nil {
		return nil, fmt.Errorf("report orchestrator: users: %w", err)
	}

	switch params.Level {
	case report.LevelPersonal:
		// 个人标题展示真实姓名：scope.Name 传的是 user_id，回填 Users() 查到的 name（如有）。
		if len(users) > 0 && users[0].Name != "" {
			meta.Scope.Name = users[0].Name
		}
		return buildPersonalReport(ctx, meta, dq, users, params)
	case report.LevelDepartment, report.LevelStudio:
		// 稳定排序：按 UserID 排序，确保无论数据源返回顺序如何，成员处理与呈现始终保持幂等确定性
		sort.SliceStable(users, func(i, j int) bool {
			return users[i].UserID < users[j].UserID
		})
		return buildGroupReport(ctx, meta, dq, users, params)
	default:
		return nil, fmt.Errorf("report orchestrator: unsupported level %q", params.Level)
	}
}

// buildCostBreakdown 从调用行聚合成本三分法 + 模型档位切片 + prev 环比。
func buildCostBreakdown(ctx context.Context, provider ReportDataProvider, rows, prevRows []CallLogRow) (report.CostBreakdown, error) {
	return buildCostBreakdownWithShared(ctx, provider, rows, prevRows, nil)
}

func buildCostBreakdownWithShared(ctx context.Context, provider ReportDataProvider, rows, prevRows []CallLogRow, shared *groupSharedData) (report.CostBreakdown, error) {
	cb := report.CostBreakdown{CacheWriteIncluded: true}
	cur := seedCostBuckets(rows)
	cb.UncachedInputCostUSD = cur.uncached
	cb.CacheCostUSD = cur.cached
	cb.OutputCostUSD = cur.output
	for _, r := range rows {
		uncached := r.PromptTokens - r.CachedTokens - r.CacheWriteTokens
		if uncached > 0 {
			cb.UncachedInputTokens += uncached
		}
		cb.InputTokens += r.PromptTokens
		cb.CachedTokens += r.CachedTokens
		cb.CacheWriteTokens += r.CacheWriteTokens
		cb.OutputTokens += r.CompletionTokens
		cb.ReasoningTokens += r.ReasoningTokens
		cb.TotalTokens += r.PromptTokens + r.CompletionTokens + r.ReasoningTokens
		if r.Status != 200 {
			failedCost := r.EstCostUSD
			if failedCost == 0 {
				failedCost = r.InputCostUSD + r.CacheReadCostUSD + r.OutputCostUSD
			}
			cb.FailedRequestCostUSD += failedCost
		}
	}
	if prevRows != nil {
		prev := seedCostBuckets(prevRows)
		cb.Prev = &report.CostBreakdownPrev{UncachedInputCostUSD: prev.uncached, CacheCostUSD: prev.cached, OutputCostUSD: prev.output}
	}
	cb.ByModelTier = buildModelTierWithShared(ctx, provider, rows, shared)
	return cb, nil
}

type costBuckets struct{ uncached, cached, output float64 }

func seedCostBuckets(rows []CallLogRow) costBuckets {
	var b costBuckets
	for _, r := range rows {
		b.uncached += r.InputCostUSD
		b.cached += r.CacheReadCostUSD
		b.output += r.OutputCostUSD
		// call_logs 旧结构没有 cache_write_cost_usd，但 est_cost_usd 已包含缓存写入费。
		// 将可解释的正残差并入 v0.6 的“缓存”展示桶，保证三桶总额与请求总成本守恒。
		if residual := r.EstCostUSD - r.InputCostUSD - r.CacheReadCostUSD - r.OutputCostUSD; residual > 0 {
			b.cached += residual
		}
	}
	return b
}

func buildModelTier(ctx context.Context, provider ReportDataProvider, rows []CallLogRow) []report.ModelTierCost {
	return buildModelTierWithShared(ctx, provider, rows, nil)
}

func buildModelTierWithShared(ctx context.Context, provider ReportDataProvider, rows []CallLogRow, shared *groupSharedData) []report.ModelTierCost {
	perModel := map[string]*report.ModelTierCost{}
	var order []string
	for _, r := range rows {
		if r.Model == "" {
			continue
		}
		m := perModel[r.Model]
		if m == nil {
			m = &report.ModelTierCost{Model: r.Model}
			perModel[r.Model] = m
			order = append(order, r.Model)
		}
		m.UncachedInputCostUSD += r.InputCostUSD
		m.CacheCostUSD += r.CacheReadCostUSD
		m.OutputCostUSD += r.OutputCostUSD
	}
	sort.Strings(order)
	out := make([]report.ModelTierCost, 0, len(order))
	for _, name := range order {
		m := perModel[name]
		m.CostUSD = m.UncachedInputCostUSD + m.CacheCostUSD + m.OutputCostUSD

		var info ModelTierInfo
		var err error
		if shared != nil {
			shared.tierMu.RLock()
			cached, ok := shared.tierCache[name]
			shared.tierMu.RUnlock()
			if ok {
				info = cached
			} else {
				info, err = provider.ModelTier(ctx, name)
				if err == nil {
					shared.tierMu.Lock()
					shared.tierCache[name] = info
					shared.tierMu.Unlock()
				}
			}
		} else {
			info, err = provider.ModelTier(ctx, name)
		}

		if err == nil {
			m.Tier = string(info.Tier)
		} else {
			m.Tier = string(TierUnknown)
		}
		out = append(out, *m)
	}
	return out
}

// assembleSessionFacts 把某人的调用行装配为会话事实（前缀污染/挤牙膏/多任务共用）。
func assembleSessionFacts(rows []CallLogRow) []SessionFact {
	return AssembleSessionFacts(rows, DefaultSessionSplitParams())
}

// runRulesForUser 对某人跑五类规则，返回每类 UsageIssue（供归因清算与落位）。
func runRulesForUser(ctx context.Context, params ReportParams, user ReportUser, sessions []SessionFact, from, to string) ([]report.UsageIssue, error) {
	return runRulesForUserWithShared(ctx, params, user, sessions, from, to, nil)
}

func runRulesForUserWithShared(ctx context.Context, params ReportParams, user ReportUser, sessions []SessionFact, from, to string, shared *groupSharedData) ([]report.UsageIssue, error) {
	orb := &userOrchestrator{provider: params.DataSource, user: user, positions: []string{user.Position}, from: from, to: to, shared: shared}
	issues := []report.UsageIssue{
		orb.prefixPollution(sessions),
		orb.toothpaste(sessions),
		orb.multiTask(ctx, sessions),
	}
	outRun, err := orb.outputRunaway(ctx, sessions)
	if err != nil {
		return nil, err
	}
	issues = append(issues, outRun)
	over, err := orb.overModel(ctx)
	if err != nil {
		return nil, err
	}
	issues = append(issues, over)

	// 语义增强层（master 阶段二~四）：只对 P1 封顶 suspected 的类型做取证+GLM 升级。
	// P1 可达 suspected 的类型：挤牙膏(3.2)、多任务(3.3，无 task_kind 用滚雪球突增代理后可达)、
	// 杀鸡(3.5，per-request 单轮请求)。
	if params.Semantic != nil && SemanticEnabled {
		// 会话级升级：按 issue 类型筛命中会话（每个类型只算一次候选集）。
		type upgradeCase struct {
			issueType string
			issueIdx  int
			sessions  []SessionFact
			params    any
		}
		var cases []upgradeCase
		for i := range issues {
			if issues[i].Verdict != report.VerdictSuspected {
				continue
			}
			switch issues[i].Type {
			case report.IssueTypeToothpaste:
				cases = append(cases, upgradeCase{report.IssueTypeToothpaste, i, sessions, DefaultToothpasteParams()})
			case report.IssueTypeMultiTaskSession:
				cases = append(cases, upgradeCase{report.IssueTypeMultiTaskSession, i, sessions, DefaultMultiTaskParams()})
			case report.IssueTypeOverModel:
				// 3.5 走 per-request 复核：语义层读各命中请求正文判 simple_task。
				up, ok := semanticUpgradeOverModel(ctx, params.DataSource, params.Semantic, issues[i])
				if ok {
					issues[i] = up
				}
			}
		}
		for _, c := range cases {
			up, ok := semanticUpgradeIssue(ctx, params.DataSource, params.Semantic,
				c.issueType, issues[c.issueIdx], c.sessions, c.params)
			if ok {
				issues[c.issueIdx] = up
			}
		}
	}
	return issues, nil
}

// userOrchestrator 缓存每人一次取数结果，避免重复调用 provider。
type userOrchestrator struct {
	provider  ReportDataProvider
	user      ReportUser
	positions []string
	from      string
	to        string
	shared    *groupSharedData
}

// prefixPollution 前缀污染（§3.1）。
func (o *userOrchestrator) prefixPollution(sessions []SessionFact) report.UsageIssue {
	return EvaluatePrefixPollution(sessions, DefaultPrefixPollutionParams())
}

// toothpaste 挤牙膏（§3.2）。
func (o *userOrchestrator) toothpaste(sessions []SessionFact) report.UsageIssue {
	return EvaluateToothpaste(sessions, DefaultToothpasteParams())
}

// multiTask 多任务不切会话（§3.3）。
func (o *userOrchestrator) multiTask(ctx context.Context, sessions []SessionFact) report.UsageIssue {
	if o.shared != nil {
		return EvaluateMultiTaskSession(sessions, DefaultMultiTaskParams(), o.shared.multiTaskBaselines)
	}
	b, err := o.provider.MultiTaskBaselines(ctx, []string{o.user.UserID})
	if err != nil || len(b.UserPromptP90) == 0 {
		return EvaluateMultiTaskSession(sessions, DefaultMultiTaskParams(), MultiTaskBaselines{})
	}
	return EvaluateMultiTaskSession(sessions, DefaultMultiTaskParams(), b)
}

// batchRequestIDs 从会话事实中收集被判定为「批量/自动会话」（SplitEvidenceBatch）的请求 ID。
// 这些请求不参与交互类语义判定（如多任务不切会话），输出失控同样应将其排除，保证口径一致。
func batchRequestIDs(sessions []SessionFact) map[string]bool {
	ids := map[string]bool{}
	for _, s := range sessions {
		if !s.IsBatch() {
			continue
		}
		for _, id := range s.ReqIDs {
			ids[id] = true
		}
	}
	return ids
}

// outputRunaway 输出失控（§3.4）。剔除批量/自动会话请求，与多任务共用同一 batch 判定口径。
func (o *userOrchestrator) outputRunaway(ctx context.Context, sessions []SessionFact) (report.UsageIssue, error) {
	rows, err := o.provider.OutputRunawayRows(ctx, o.user.UserID, o.from, o.to)
	if err != nil {
		return report.UsageIssue{}, err
	}
	batchIDs := batchRequestIDs(sessions)
	if len(batchIDs) > 0 {
		kept := rows[:0]
		for _, r := range rows {
			if !batchIDs[r.RequestID] {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	var b OutputRunawayBaselines
	if o.shared != nil {
		b = o.shared.outputRunawayBaselines
	} else {
		models := modelsOf(rows)
		b, err = o.provider.OutputRunawayBaselines(ctx, o.positions, models)
		if err != nil {
			return report.UsageIssue{}, err
		}
	}
	return EvaluateOutputRunaway(rows, b, DefaultOutputRunawayParams()), nil
}

// overModel 杀鸡用牛刀（§3.5）。
func (o *userOrchestrator) overModel(ctx context.Context) (report.UsageIssue, error) {
	rows, err := o.provider.OverModelRows(ctx, o.user.UserID, o.from, o.to)
	if err != nil {
		return report.UsageIssue{}, err
	}
	var b OverModelBaselines
	if o.shared != nil {
		b = o.shared.overModelBaselines
	} else {
		b, err = o.provider.OverModelBaselines(ctx, o.positions)
		if err != nil {
			return report.UsageIssue{}, err
		}
	}
	var resolver OverModelResolver
	if o.shared != nil && o.shared.resolver != nil {
		resolver = o.shared.resolver
	} else {
		resolver, err = o.provider.Resolver(ctx)
		if err != nil {
			return report.UsageIssue{}, err
		}
	}
	return EvaluateOverModel(ctx, rows, resolver, b, DefaultOverModelParams()), nil
}

func modelsOf(rows []OutputRunawayRow) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Model != "" && !seen[r.Model] {
			seen[r.Model] = true
			out = append(out, r.Model)
		}
	}
	sort.Strings(out)
	return out
}

// buildPersonalReport：个人 = 单人五类判定 + 3.6 + Assemble。
func buildPersonalReport(ctx context.Context, meta report.Meta, dq report.DataQuality, users []ReportUser, params ReportParams) (*report.Report, error) {
	if len(users) == 0 {
		return nil, fmt.Errorf("report orchestrator: personal requires exactly one user in scope")
	}
	user := users[0]
	rows, err := params.DataSource.CallRows(ctx, user.UserID, params.Period.From, params.Period.To)
	if err != nil {
		return nil, err
	}
	prevRows, err := params.DataSource.CallRows(ctx, user.UserID, prevPeriodFrom(params.Period).From, prevPeriodFrom(params.Period).To)
	if err != nil {
		return nil, err
	}
	cb, err := buildCostBreakdown(ctx, params.DataSource, rows, prevRows)
	if err != nil {
		return nil, err
	}
	sessions := assembleSessionFacts(rows)
	issues, err := runRulesForUser(ctx, params, user, sessions, params.Period.From, params.Period.To)
	if err != nil {
		return nil, err
	}
	reconciled := reconcileIssues(user.UserID, issues, cb.Total())

	safety, err := buildPersonalSafety(ctx, params, user, cb.Total())
	if err != nil {
		return nil, err
	}

	in := report.AssembleInput{
		DataQuality:    dq,
		CostBreakdown:  cb,
		SafetyAnomaly:  safety,
		PersonalIssues: reconciled,
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		return nil, err
	}

	// v0.6 仅保留客观调用数；不注入 v0.7 A/B/C 评级或亮点。
	r.TotalCalls = int64(len(rows))

	return r, nil
}

// groupSharedData 存储部门/工作室级别的共享基线与缓存，避免各成员计算时重复打库。
type groupSharedData struct {
	filings                []OffhoursFilingWindow
	multiTaskBaselines     MultiTaskBaselines
	overModelBaselines     OverModelBaselines
	outputRunawayBaselines OutputRunawayBaselines
	resolver               OverModelResolver

	runawayMu    sync.RWMutex
	runawayCache map[string]OutputRunawayBaselines

	tierMu    sync.RWMutex
	tierCache map[string]ModelTierInfo
}

func initGroupSharedData(ctx context.Context, params ReportParams, users []ReportUser) *groupSharedData {
	shared := &groupSharedData{
		runawayCache: make(map[string]OutputRunawayBaselines),
		tierCache:    make(map[string]ModelTierInfo),
	}
	if params.DataSource == nil || len(users) == 0 {
		return shared
	}

	// 1. 预读全组织备案窗口（排除优先，共享给所有成员的 3.6 判定）
	if filings, err := params.DataSource.OffhoursFilings(ctx); err == nil {
		shared.filings = filings
	}

	// 2. 批量预读全组织多任务基线（1 次批量查询替代 N 次单人查询）
	userIDs := make([]string, len(users))
	for i, u := range users {
		userIDs[i] = u.UserID
	}
	if mtb, err := params.DataSource.MultiTaskBaselines(ctx, userIDs); err == nil {
		shared.multiTaskBaselines = mtb
	}

	// 3. 预读模型解析器 Resolver（整个组织共享）
	if res, err := params.DataSource.Resolver(ctx); err == nil {
		shared.resolver = res
	}

	// 4. 批量预读全组织所有涉及岗位的选型基线（1 次批量查询替代 N 次单岗查询）
	posSet := make(map[string]bool)
	for _, u := range users {
		if u.Position != "" {
			posSet[u.Position] = true
		}
	}
	var positions []string
	for p := range posSet {
		positions = append(positions, p)
	}
	if len(positions) > 0 {
		if omb, err := params.DataSource.OverModelBaselines(ctx, positions); err == nil {
			shared.overModelBaselines = omb
		}
	}

	// 5. 批量预读全组织涉及岗位的输出失控基线
	if orb, err := params.DataSource.OutputRunawayBaselines(ctx, positions, nil); err == nil {
		shared.outputRunawayBaselines = orb
	}

	return shared
}

// determineGroupWorkers 计算成员并发分发协程数（bounded 4-8）。
func determineGroupWorkers(userCount, customConcurrency int) int {
	if userCount <= 1 {
		return 1
	}
	if customConcurrency == 1 {
		return 1
	}
	if customConcurrency > 1 {
		if customConcurrency > 16 {
			return 16
		}
		if customConcurrency > userCount {
			return userCount
		}
		return customConcurrency
	}

	// 默认自动选择：介于 4 与 8 之间（规范要求 bounded 4-8）
	workers := runtime.GOMAXPROCS(0)
	if workers < 4 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}
	if workers > userCount {
		workers = userCount
	}
	return workers
}

// buildPersonalSafetyWithFilings 用 3.6 时间窗判定产出 personal 的 SafetyAnomaly（Status 恒 pending_review）。
func buildPersonalSafetyWithFilings(ctx context.Context, params ReportParams, user ReportUser, totalCost float64, filings []OffhoursFilingWindow) (report.SafetyAnomaly, error) {
	rows, err := params.DataSource.OffhoursRows(ctx, user.UserID, params.Period.From, params.Period.To)
	if err != nil {
		return report.SafetyAnomaly{}, err
	}
	if len(rows) == 0 {
		return report.SafetyAnomaly{}, nil
	}
	if filings == nil {
		filings, err = params.DataSource.OffhoursFilings(ctx)
		if err != nil {
			return report.SafetyAnomaly{}, err
		}
	}
	judgments := ComputeOffhoursJudgments(rows, filings, DefaultOffhoursJudgmentParams())
	var hit *OffhoursJudgment
	for i := range judgments {
		if judgments[i].UserID == user.UserID {
			hit = &judgments[i]
			break
		}
	}
	if hit == nil {
		return report.SafetyAnomaly{}, nil
	}
	sa := report.SafetyAnomaly{
		Hit:                true,
		UnrelatedCostUSD:   hit.UnrelatedCostUSD,
		UnrelatedCostRatio: hit.UnrelatedRatio,
		TopHours:           hit.TopHours,
		TopModel:           hit.TopModel,
		TopKey:             hit.TopKey,
		Excluded:           hit.ExcludedFilingTypes,
		Status:             report.SafetyAnomalyPendingReview,
		ReviewTag:          "待核查",
		Basis:              fmt.Sprintf("未备案的非工作时段调用费用占个人总成本 %.1f%%，超过 20%% 关注线", hit.UnrelatedRatio*100),
		Hint:               "仅凭凌晨或周末使用不能判断用途，请核实是否属于加班、值班、跨时区或批处理。",
		Priority:           "medium",
	}
	if SemanticEnabled && params.Semantic != nil {
		saUp, _ := semanticUpgradeSafetyAnomaly(ctx, params.DataSource, params.Semantic, sa, hit.HitRows)
		sa = saUp
	}
	return sa, nil
}

// buildPersonalSafety 用 3.6 时间窗判定产出 personal 的 SafetyAnomaly（Status 恒 pending_review）。
func buildPersonalSafety(ctx context.Context, params ReportParams, user ReportUser, totalCost float64) (report.SafetyAnomaly, error) {
	return buildPersonalSafetyWithFilings(ctx, params, user, totalCost, nil)
}

type singleMemberResult struct {
	member report.MemberAssembleInput
	cb     report.CostBreakdown
}

func evalSingleMember(ctx context.Context, params ReportParams, user ReportUser, shared *groupSharedData) (singleMemberResult, error) {
	rows, err := params.DataSource.CallRows(ctx, user.UserID, params.Period.From, params.Period.To)
	if err != nil {
		return singleMemberResult{}, err
	}
	prevRows, err := params.DataSource.CallRows(ctx, user.UserID, prevPeriodFrom(params.Period).From, prevPeriodFrom(params.Period).To)
	if err != nil {
		return singleMemberResult{}, err
	}
	cb, err := buildCostBreakdownWithShared(ctx, params.DataSource, rows, prevRows, shared)
	if err != nil {
		return singleMemberResult{}, err
	}

	sessions := assembleSessionFacts(rows)
	issues, err := runRulesForUserWithShared(ctx, params, user, sessions, params.Period.From, params.Period.To, shared)
	if err != nil {
		return singleMemberResult{}, err
	}
	reconciled := reconcileIssues(user.UserID, issues, cb.Total())

	// per-issue 归因净额（用于 MemberQuadrantInput.IssueAttributed/dominant）。
	byIssue := map[string]float64{}
	for _, u := range reconciled {
		byIssue[u.Type] += u.AttributedCostUSD
	}
	safetyHit := false
	var filings []OffhoursFilingWindow
	if shared != nil {
		filings = shared.filings
	}
	safety, err := buildPersonalSafetyWithFilings(ctx, params, user, cb.Total(), filings)
	if err != nil {
		return singleMemberResult{}, err
	}
	safetyHit = safety.Hit

	quadrant := report.MemberQuadrantInput{
		UserID:          user.UserID,
		Name:            user.Name,
		Position:        user.Position,
		DeptID:          user.DeptID,
		DeptName:        user.DeptName,
		CostUSD:         cb.Total(),
		Calls:           len(rows),
		SafetyAnomaly:   safetyHit,
		SafetyDetail:    safety,
		SafetyReviewTag: safety.ReviewTag,
		SafetyHint:      safety.Hint,
		SuspectedOnly:   suspectedOnly(reconciled),
		IssueAttributed: byIssue,
	}
	return singleMemberResult{
		member: report.MemberAssembleInput{Quadrant: quadrant, CostBreakdown: cb, UsageIssues: reconciled},
		cb:     cb,
	}, nil
}

// buildGroupReport：department|studio = 逐成员判定 → 四象限/分群 + 汇总 CostBreakdown + Assemble。
// 支持 bounded worker pool（并发 4-8）分发成员规则计算与会话装配，并共享部门级基线与缓存。
func buildGroupReport(ctx context.Context, meta report.Meta, dq report.DataQuality, users []ReportUser, params ReportParams) (*report.Report, error) {
	if len(users) == 0 {
		// 空组织：仍给出一个顶层 CostBreakdown/DataQuality + 空 member_evaluation（Assemble 支持空成员）。
		cb, err := buildCostBreakdown(ctx, params.DataSource, nil, nil)
		if err != nil {
			return nil, err
		}
		in := report.AssembleInput{DataQuality: dq, CostBreakdown: cb}
		return report.Assemble(meta, in, report.DefaultRuleSet())
	}

	shared := initGroupSharedData(ctx, params, users)
	numWorkers := determineGroupWorkers(len(users), params.Concurrency)

	type memberResult struct {
		index  int
		member report.MemberAssembleInput
		cb     report.CostBreakdown
	}

	tasks := make(chan int, len(users))
	for i := range users {
		tasks <- i
	}
	close(tasks)

	results := make(chan memberResult, len(users))
	errCh := make(chan error, 1)

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range tasks {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				u := users[idx]
				res, err := evalSingleMember(workerCtx, params, u, shared)
				if err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
				results <- memberResult{
					index:  idx,
					member: res.member,
					cb:     res.cb,
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	members := make([]report.MemberAssembleInput, len(users))
	cbs := make([]report.CostBreakdown, len(users))
	for res := range results {
		members[res.index] = res.member
		cbs[res.index] = res.cb
	}

	var aggCB report.CostBreakdown
	aggCB.CacheWriteIncluded = true
	var aggPrev report.CostBreakdownPrev
	hasPrev := false

	// 按成员原始下标升序汇总成本，确保浮点数累加与串行执行严格一致
	for _, cb := range cbs {
		aggCB.UncachedInputCostUSD += cb.UncachedInputCostUSD
		aggCB.CacheCostUSD += cb.CacheCostUSD
		aggCB.OutputCostUSD += cb.OutputCostUSD
		aggCB.FailedRequestCostUSD += cb.FailedRequestCostUSD
		aggCB.TotalTokens += cb.TotalTokens
		aggCB.InputTokens += cb.InputTokens
		aggCB.UncachedInputTokens += cb.UncachedInputTokens
		aggCB.CachedTokens += cb.CachedTokens
		aggCB.CacheWriteTokens += cb.CacheWriteTokens
		aggCB.OutputTokens += cb.OutputTokens
		aggCB.ReasoningTokens += cb.ReasoningTokens
		if cb.Prev != nil {
			aggPrev.UncachedInputCostUSD += cb.Prev.UncachedInputCostUSD
			aggPrev.CacheCostUSD += cb.Prev.CacheCostUSD
			aggPrev.OutputCostUSD += cb.Prev.OutputCostUSD
			hasPrev = true
		}
	}

	if hasPrev {
		aggCB.Prev = &aggPrev
	}
	// 组织级 by_model_tier 明细由 DB 聚合层提供（此处不重算，成本三分法仍自洽）。

	in := report.AssembleInput{DataQuality: dq, CostBreakdown: aggCB, Members: members}
	return report.Assemble(meta, in, report.DefaultRuleSet())
}

// suspectedOnly 判定该用户命中情况是否全为 suspected（仅用于虚边描边）。
func suspectedOnly(issues []report.UsageIssue) bool {
	var anyConfirmed bool
	var anySuspected bool
	for _, u := range issues {
		if u.Verdict == report.VerdictConfirmed {
			anyConfirmed = true
		}
		if u.Verdict == report.VerdictSuspected {
			anySuspected = true
		}
	}
	return anySuspected && !anyConfirmed
}

// reconcileIssues 对五类归因做「主因择一」清算：调 §5 ReconcileCostAttribution，把每类规则的
// 归因净额/重叠回填到对应的 UsageIssue。
//
// 说明：规则当前只返回聚合 UsageIssue，不暴露逐实体 claim；在数据层提供逐实体 claim 前，
// 本编排把该用户全部命中规则作为同一载体的多个 claim（EntityKey=userID）做「主因择一」，
// 保证 Σ归因≤total（ReconcileCostAttribution 会校验）且 dominant 正确。归因超总成本时回退为
// 各规则自带归因（下游 report.Validate() 兜底，避免整份报告失败）。
func reconcileIssues(userID string, issues []report.UsageIssue, totalCost float64) []report.UsageIssue {
	var claims []CostClaim
	for _, u := range issues {
		if u.Verdict == report.VerdictNotHit || u.Verdict == report.VerdictNotComputed {
			continue
		}
		affected := u.AffectedCostUSD
		if affected < u.AttributedCostUSD {
			affected = u.AttributedCostUSD
		}
		// 语义 refute 清零后仍留 suspected：零成本项不参与归因清算（不占主因槽位、
		// 不进占比与主导），卡片保留供人工复核。
		if u.AttributedCostUSD <= 0 && affected <= 0 {
			continue
		}
		claims = append(claims, CostClaim{
			IssueType: u.Type, EntityKind: EntityKindSession, EntityKey: userID,
			BucketCost: u.AttributedCostUSD, AffectedCost: affected,
		})
	}
	if len(claims) == 0 {
		return issues
	}
	res, err := ReconcileCostAttribution(claims, totalCost)
	if err != nil {
		return issues
	}
	perType := map[string]float64{}
	overlapType := map[string]float64{}
	for _, pi := range res.PerIssue {
		perType[pi.IssueType] = pi.AttributedCostUSD
		overlapType[pi.IssueType] = pi.OverlapCostUSD
	}
	for i := range issues {
		if a, ok := perType[issues[i].Type]; ok {
			issues[i].AttributedCostUSD = a
			issues[i].OverlapCostUSD = overlapType[issues[i].Type]
		}
	}
	return issues
}

// prevPeriodFrom 由当前周期推导上一个周期（同一时长前移；周报=上周，周内=前一等距窗口）。
func prevPeriodFrom(p report.Period) report.Period {
	from, err1 := time.Parse("2006-01-02", p.From)
	to, err2 := time.Parse("2006-01-02", p.To)
	if err1 != nil || err2 != nil {
		return report.Period{}
	}
	span := to.Sub(from)
	prevTo := from.Add(-time.Nanosecond)
	prevFrom := prevTo.Add(-span)
	return report.Period{
		From: prevFrom.Format("2006-01-02"),
		To:   prevTo.Format("2006-01-02"),
	}
}

// ComputeHealthGrade 计算个人健康等级 (A/B/C) 及亮点卡片 Highlights。
// 规则（对齐 v0.7 规范 §3 个人健康体检模式）：
// - 评级判定：
//   - A 级（优秀/标杆）：零输出失控，多任务浪费 <= 5%，综合得分 >= 85
//   - B 级（健康/良好）：失控浪费 <= 15%（或 <= $1.0），综合得分 >= 65
//   - C 级（需关注/风险）：综合得分 < 65，或严重失控（>20%），或多任务浪费严重（>20%）
//
// - 亮点卡片（Highlights）：
//   - “高复用标杆”：杨士俊型，零输出失控，缓存复用率 >= 40%，且存在深度开发或较多调用
//   - “精准提问”：零输出失控，回退次数为 0，调用次数 >= 3
//   - “勤俭节约”：总浪费占总消费比例 < 2%，且调用次数 >= 3
//   - “工程深潜者”：深度开发会话数 >= 1
func ComputeHealthGrade(p report.UserHealthProfile) (string, []string) {
	totalCost := p.TotalCostUSD
	if totalCost <= 0.0001 {
		totalCost = 0.0001
	}

	runawayRatio := p.RunawayWastedCost / totalCost
	multitaskRatio := p.MultitaskWastedCost / totalCost
	totalWasted := p.RunawayWastedCost + p.MultitaskWastedCost

	// 计算基础得分
	score := 100.0

	// 1. 失控扣分
	if p.RunawayWastedCost > 0 {
		if runawayRatio > 0.20 {
			score -= 35.0
		} else if runawayRatio > 0.05 {
			score -= 20.0
		} else {
			score -= 10.0
		}
	}

	// 2. 多任务漂移扣分
	if p.MultitaskWastedCost > 0 {
		if multitaskRatio > 0.15 {
			score -= 25.0
		} else if multitaskRatio > 0.05 {
			score -= 15.0
		} else {
			score -= 5.0
		}
	}

	// 3. 缓存复用奖惩
	if p.TotalCalls >= 10 && p.CacheHitRate < 0.20 {
		score -= 10.0
	} else if p.CacheHitRate >= 0.50 {
		score = math.Min(100.0, score+5.0)
	}

	score = math.Max(0.0, math.Min(100.0, score))

	// 等级判定 (A/B/C)
	grade := string(report.GradeB)
	if p.RunawayWastedCost == 0 && multitaskRatio <= 0.05 && score >= 85 {
		grade = string(report.GradeA)
	} else if (runawayRatio <= 0.15 || p.RunawayWastedCost <= 1.0) && score >= 65 {
		grade = string(report.GradeB)
	} else {
		grade = string(report.GradeC)
	}

	// 亮点卡片生成
	highlights := make([]string, 0)
	if p.RunawayWastedCost == 0 && p.CacheHitRate >= 0.40 && (p.DeepDevSessions >= 1 || p.TotalCalls >= 5) {
		highlights = append(highlights, "高复用标杆")
	}
	if p.RunawayWastedCost == 0 && p.RollbackCount == 0 && p.TotalCalls >= 3 {
		highlights = append(highlights, "精准提问")
	}
	if totalWasted/totalCost < 0.02 && p.TotalCalls >= 3 {
		highlights = append(highlights, "勤俭节约")
	}
	if p.DeepDevSessions >= 1 {
		highlights = append(highlights, "工程深潜者")
	}

	return grade, highlights
}
