package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// memProvider 是 ReportDataProvider 的内存实现，用于 e2e 驱动完整管线。
type memProvider struct {
	users    map[string]ReportUser
	callRows map[string][]CallLogRow
	outRows  map[string][]OutputRunawayRow
	overRows map[string][]OverModelRow
	offRows  map[string][]OffhoursCallRow
	dq       report.DataQuality
	bodies   map[string]string
}

func (p *memProvider) Users(ctx context.Context, level string, scope report.Scope) ([]ReportUser, error) {
	var out []ReportUser
	switch level {
	case report.LevelPersonal:
		if u, ok := p.users[scope.ID]; ok {
			out = append(out, u)
		}
	case report.LevelDepartment, report.LevelStudio:
		for _, u := range p.users {
			out = append(out, u)
		}
	}
	return out, nil
}

func (p *memProvider) CallRows(ctx context.Context, userID, from, to string) ([]CallLogRow, error) {
	return p.callRows[userID], nil
}
func (p *memProvider) OutputRunawayRows(ctx context.Context, userID, from, to string) ([]OutputRunawayRow, error) {
	return p.outRows[userID], nil
}
func (p *memProvider) OverModelRows(ctx context.Context, userID, from, to string) ([]OverModelRow, error) {
	return p.overRows[userID], nil
}
func (p *memProvider) DataQuality(ctx context.Context, level string, scope report.Scope) (report.DataQuality, error) {
	return p.dq, nil
}
func (p *memProvider) OutputRunawayBaselines(ctx context.Context, positions, models []string) (OutputRunawayBaselines, error) {
	return OutputRunawayBaselines{}, nil
}
func (p *memProvider) MultiTaskBaselines(ctx context.Context, userIDs []string) (MultiTaskBaselines, error) {
	return MultiTaskBaselines{}, nil
}
func (p *memProvider) OverModelBaselines(ctx context.Context, positions []string) (OverModelBaselines, error) {
	return OverModelBaselines{}, nil
}
func (p *memProvider) Resolver(ctx context.Context) (OverModelResolver, error) {
	return noopOverModelResolver{}, nil
}
func (p *memProvider) ModelTier(ctx context.Context, model string) (ModelTierInfo, error) {
	return ModelTierInfo{ModelName: model, Tier: TierLow}, nil
}
func (p *memProvider) OffhoursRows(ctx context.Context, userID, from, to string) ([]OffhoursCallRow, error) {
	return p.offRows[userID], nil
}
func (p *memProvider) OffhoursFilings(ctx context.Context) ([]OffhoursFilingWindow, error) {
	return nil, nil
}
func (p *memProvider) TraceBodies(ctx context.Context, reqIDs []string) (map[string]string, error) {
	if p.bodies != nil {
		out := make(map[string]string)
		for _, rid := range reqIDs {
			if b, ok := p.bodies[rid]; ok {
				out[rid] = b
			}
		}
		return out, nil
	}
	return map[string]string{}, nil
}

// noopOverModelResolver 幂等返回未知档位/无更低档位（over_model 行列空时不触发）。
type noopOverModelResolver struct{}

func (noopOverModelResolver) GetModelTier(ctx context.Context, modelName string) (ModelTierInfo, error) {
	return ModelTierInfo{ModelName: modelName, Tier: TierUnknown}, nil
}
func (noopOverModelResolver) GetLightestAlternative(ctx context.Context, groupID uint, excludeModelName string) (*LightestAlternative, error) {
	return nil, nil
}

var e2eDay = time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

// e2eUserRows 构造某人的调用行（10 条同会话在 30min 窗口内，弱证据，短输入 → 命中挤牙膏）。
func e2eUserRows(userID string) []CallLogRow {
	var rows []CallLogRow
	for i := 0; i < 10; i++ {
		ts := e2eDay.Add(time.Duration(8*i) * time.Minute)
		rows = append(rows, CallLogRow{
			RequestID: userID + "-r" + itoa(i), UserID: userID, KeyID: "k1", TS: ts, Model: "m",
			PromptTokens: 10, CachedTokens: 0, InputCostUSD: 0.1, CacheReadCostUSD: 0, OutputCostUSD: 0,
		})
	}
	return rows
}

// e2eOffRows 构造某人的 3.6 判定行。TS 以 UTC 表示，但在 Asia/Shanghai 时区下分类：
//   - 上海 2026-08-04 10:00（周二）→ 正常工作时段；
//   - 上海 2026-08-04 01:00/02:00/03:00（周二）→ 非工作时段（凌晨）。
func e2eOffRows(userID string, totalOnCost float64) []OffhoursCallRow {
	rows := []OffhoursCallRow{
		{RequestID: userID + "-on-1", UserID: userID, UserName: userID, TS: time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC), Model: "m", KeyID: "k1", CostUSD: totalOnCost}, // Shanghai 10:00（正常）
		{RequestID: userID + "-night-1", UserID: userID, UserName: userID, TS: time.Date(2026, 8, 3, 17, 0, 0, 0, time.UTC), Model: "m", KeyID: "k1", CostUSD: 0.2},     // Shanghai 01:00（凌晨）
		{RequestID: userID + "-night-2", UserID: userID, UserName: userID, TS: time.Date(2026, 8, 3, 18, 0, 0, 0, time.UTC), Model: "m", KeyID: "k1", CostUSD: 0.2},     // Shanghai 02:00（凌晨）
		{RequestID: userID + "-night-3", UserID: userID, UserName: userID, TS: time.Date(2026, 8, 3, 19, 0, 0, 0, time.UTC), Model: "m", KeyID: "k1", CostUSD: 0.2},     // Shanghai 03:00（凌晨）
	}
	return rows
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func e2eParams(level string, scopeID string, provider ReportDataProvider, period report.Period) ReportParams {
	return ReportParams{
		Level: level, Scope: report.Scope{ID: scopeID, Name: scopeID},
		Period: period, Currency: report.Currency{Report: "USD", USDCNYRate: 1.0},
		RuleVersion: "v0.6", DataSource: provider,
	}
}

func e2ePeriod() report.Period {
	return report.Period{From: "2026-08-03", To: "2026-08-09", IsoWeek: "2026-W33"}
}

// e2e：个人报告 - 结构完整、3.6 仅 personal 且 pending_review、两占比分列、Validate 通过。
func TestBuildCostBreakdown_TokensAndFailedCost(t *testing.T) {
	rows := []CallLogRow{
		{PromptTokens: 100, CachedTokens: 30, CacheWriteTokens: 10, CompletionTokens: 20, ReasoningTokens: 5, Status: 200, InputCostUSD: 1, CacheReadCostUSD: .2, OutputCostUSD: .3},
		{PromptTokens: 50, CachedTokens: 10, CompletionTokens: 7, ReasoningTokens: 3, Status: 500, InputCostUSD: .4, CacheReadCostUSD: .1, OutputCostUSD: .2, EstCostUSD: .7},
	}
	cb, err := buildCostBreakdown(context.Background(), &memProvider{}, rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cb.TotalTokens != 185 || cb.InputTokens != 150 || cb.UncachedInputTokens != 100 || cb.CachedTokens != 40 || cb.CacheWriteTokens != 10 || cb.OutputTokens != 27 || cb.ReasoningTokens != 8 {
		t.Fatalf("unexpected token contract: %+v", cb)
	}
	if math.Abs(cb.FailedRequestCostUSD-.7) > 1e-9 || math.Abs(cb.Total()-2.2) > 1e-9 {
		t.Fatalf("failed cost must be marked within three buckets: %+v", cb)
	}
}

func TestBuildCostBreakdown_IncludesLegacyCacheWriteResidual(t *testing.T) {
	rows := []CallLogRow{{
		PromptTokens: 100, CachedTokens: 30, CacheWriteTokens: 10, CompletionTokens: 20,
		Status: 200, InputCostUSD: 1, CacheReadCostUSD: .2, OutputCostUSD: .3, EstCostUSD: 1.7,
	}}
	cb, err := buildCostBreakdown(context.Background(), &memProvider{}, rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(cb.CacheCostUSD-.4) > 1e-9 || math.Abs(cb.Total()-1.7) > 1e-9 {
		t.Fatalf("cache write residual must be included in cache bucket: %+v", cb)
	}
}

func TestGetReport_E2E_Personal(t *testing.T) {
	u := ReportUser{UserID: "u1", Name: "李锦程01", Position: "AI工程师", DeptID: "X-Lab"}
	p := &memProvider{
		users:    map[string]ReportUser{"u1": u},
		callRows: map[string][]CallLogRow{"u1": e2eUserRows("u1")},
		outRows:  map[string][]OutputRunawayRow{},
		overRows: map[string][]OverModelRow{},
		offRows:  map[string][]OffhoursCallRow{"u1": e2eOffRows("u1", 1.0)},
		dq:       report.DataQuality{RealSessionCoverage: 0.7, SceneLabelCoverage: 0.5, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
	}
	r, err := GetReport(context.Background(), e2eParams(report.LevelPersonal, "u1", p, e2ePeriod()))
	if err != nil {
		t.Fatalf("GetReport personal: %v", err)
	}
	if r.Meta.Level != report.LevelPersonal {
		t.Errorf("level=%q", r.Meta.Level)
	}
	if !r.SafetyAnomaly.Hit {
		t.Error("expected safety hit for personal 3.6")
	}
	if r.SafetyAnomaly.Status != report.SafetyAnomalyPendingReview {
		t.Errorf("safety status=%q, want pending_review", r.SafetyAnomaly.Status)
	}
	// 挤牙膏命中 + 归因。其余空规则为 not_hit/not_computed。
	var toothpaste *report.UsageIssue
	for i := range r.UsageIssues {
		if r.UsageIssues[i].Type == report.IssueTypeToothpaste {
			toothpaste = &r.UsageIssues[i]
		}
	}
	if toothpaste == nil || toothpaste.Verdict != report.VerdictSuspected && toothpaste.Verdict != report.VerdictConfirmed {
		t.Errorf("expected toothpaste hit, got issues=%+v", r.UsageIssues)
	}
	if toothpaste == nil || math.Abs(toothpaste.AttributedCostUSD-0.9) > 0.01 {
		t.Errorf("toothpaste attributed=%v, want 0.9", toothpaste.AttributedCostUSD)
	}
	// 铁律：issue_cost_ratio(五类) 与 unrelated_cost_ratio(3.6) 分列、永不相加。
	if math.Abs(r.Conclusion.IssueCostRatio-0.9) > 0.01 {
		t.Errorf("issue_cost_ratio=%v, want 0.9", r.Conclusion.IssueCostRatio)
	}
	if r.SafetyAnomaly.UnrelatedCostRatio <= 0.2 {
		t.Errorf("unrelated_cost_ratio=%v should hit >0.2", r.SafetyAnomaly.UnrelatedCostRatio)
	}
	if r.Conclusion.IssueCostRatio == r.SafetyAnomaly.UnrelatedCostRatio {
		t.Error("issue_cost_ratio and unrelated_cost_ratio must not be same/added")
	}
	if r.Conclusion.DominantIssue != report.IssueTypeToothpaste {
		t.Errorf("dominant=%q, want toothpaste", r.Conclusion.DominantIssue)
	}
	// Validate 已由 Assemble 内部调用；再显式确认契约合法。
	if err := r.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// e2e：部门报告 - member_evaluation 存在、每成员 Σattributed<=member.CostUSD、3.6 不进顶层。
func TestGetReport_E2E_Department(t *testing.T) {
	u1 := ReportUser{UserID: "u1", Name: "甲", Position: "AI工程师", DeptID: "X-Lab"}
	u2 := ReportUser{UserID: "u2", Name: "乙", Position: "测试", DeptID: "X-Lab"}
	p := &memProvider{
		users: map[string]ReportUser{"u1": u1, "u2": u2},
		callRows: map[string][]CallLogRow{
			"u1": e2eUserRows("u1"),
			"u2": e2eUserRows("u2")[:2], // 2 轮 → toothpaste 不命中
		},
		outRows:  map[string][]OutputRunawayRow{},
		overRows: map[string][]OverModelRow{},
		offRows:  map[string][]OffhoursCallRow{"u1": e2eOffRows("u1", 1.0)},
		dq:       report.DataQuality{RealSessionCoverage: 0.7, SceneLabelCoverage: 0.5, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
	}
	r, err := GetReport(context.Background(), e2eParams(report.LevelDepartment, "d1", p, e2ePeriod()))
	if err != nil {
		t.Fatalf("GetReport dept: %v", err)
	}
	if r.Meta.Level != report.LevelDepartment {
		t.Errorf("level=%q", r.Meta.Level)
	}
	if len(r.UsageIssues) != 0 {
		t.Errorf("dept must not set top-level usage_issues, got %d", len(r.UsageIssues))
	}
	if r.SafetyAnomaly.Hit {
		t.Error("dept must not carry top-level safety hit (3.6 is personal-only)")
	}
	me := r.MemberEvaluation
	if me == nil || len(me.Members) == 0 {
		t.Fatalf("dept requires member_evaluation")
	}
	for _, m := range me.Members {
		if m.TokenBreakdown.TotalTokens != int64(m.Calls*10) || m.TokenBreakdown.InputTokens != int64(m.Calls*10) || math.Abs(m.CostBreakdown.Total()-m.CostUSD) > 1e-9 {
			t.Errorf("member %s production token/cost breakdown mismatch: %+v / %+v", m.UserID, m.TokenBreakdown, m.CostBreakdown)
		}
		if m.UserID == "u1" && (!m.SafetyDetail.Hit || m.SafetyDetail.UnrelatedCostUSD == 0 || len(m.SafetyDetail.TopHours) == 0 || m.SafetyDetail.TopModel == "" || m.SafetyDetail.TopKey == "" || m.SafetyDetail.Status != report.SafetyAnomalyPendingReview) {
			t.Errorf("member 3.6 detail incomplete: %+v", m.SafetyDetail)
		}
		attr := 0.0
		for _, u := range m.UsageIssues {
			attr += u.AttributedCostUSD
		}
		if attr > m.CostUSD+0.01 {
			t.Errorf("member %s attributed=%v > cost=%v", m.UserID, attr, m.CostUSD)
		}
	}
}

// e2e：工作室报告 - 成员跨部门、member_evaluation 存在。
func TestGetReport_E2E_Studio(t *testing.T) {
	u1 := ReportUser{UserID: "u1", Name: "甲", Position: "AI工程师", DeptID: "X-Lab"}
	u2 := ReportUser{UserID: "u2", Name: "乙", Position: "后端", DeptID: "公共技术二部"}
	p := &memProvider{
		users: map[string]ReportUser{"u1": u1, "u2": u2},
		callRows: map[string][]CallLogRow{
			"u1": e2eUserRows("u1"),
			"u2": e2eUserRows("u2")[:2],
		},
		outRows: map[string][]OutputRunawayRow{}, overRows: map[string][]OverModelRow{},
		offRows: map[string][]OffhoursCallRow{"u1": e2eOffRows("u1", 1.0)},
		dq:      report.DataQuality{RealSessionCoverage: 0.7, SceneLabelCoverage: 0.5, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
	}
	r, err := GetReport(context.Background(), e2eParams(report.LevelStudio, "s1", p, e2ePeriod()))
	if err != nil {
		t.Fatalf("GetReport studio: %v", err)
	}
	if r.Meta.Level != report.LevelStudio {
		t.Errorf("level=%q", r.Meta.Level)
	}
	if r.MemberEvaluation == nil || len(r.MemberEvaluation.Members) != 2 {
		t.Fatalf("studio member_evaluation wrong")
	}
}

// 公开入口的周期推导：week 7 天、month 整月、day 当天，并推导上一周期。
func TestResolveReportPeriod(t *testing.T) {
	cur, prev := resolveReportPeriod("2026-08-03", "week")
	if cur.From != "2026-08-03" || cur.To != "2026-08-09" {
		t.Errorf("week cur=%v", cur)
	}
	if prev.From != "2026-07-27" || prev.To != "2026-08-02" {
		t.Errorf("week prev=%v", prev)
	}
	if _, err := time.Parse("2006-01-02", resolveReportPeriod2("2026-08-03", "month")); err != nil {
		t.Errorf("month invalid")
	}
}

func resolveReportPeriod2(start, typ string) string {
	p, _ := resolveReportPeriod(start, typ)
	return p.To
}

// TestGetReport_E2E_Personal_SemanticSafetyAnomaly 验证 orchestrator 完整管线下，
// 开启语义增强后，个人报告 3.6 严重异常从待核查识别为「疑似加班未备案」，在报告中提示核实备案。
func TestGetReport_E2E_Personal_SemanticSafetyAnomaly(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}

	body := `{"messages":[{"role":"user","content":"线上服务报 502 错误，排查连接池泄露并提交紧急修复代码"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"work_related\",\"confidence\":0.92,\"reason_tag\":\"bug_fix\"}"}}]}`)
	}))
	defer srv.Close()

	u := ReportUser{UserID: "u1", Name: "李锦程01", Position: "AI工程师", DeptID: "X-Lab"}
	p := &memProvider{
		users:    map[string]ReportUser{"u1": u},
		callRows: map[string][]CallLogRow{"u1": e2eUserRows("u1")},
		outRows:  map[string][]OutputRunawayRow{},
		overRows: map[string][]OverModelRow{},
		offRows:  map[string][]OffhoursCallRow{"u1": e2eOffRows("u1", 1.0)},
		dq:       report.DataQuality{RealSessionCoverage: 0.7, SceneLabelCoverage: 0.5, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
		bodies: map[string]string{
			"u1-night-1": body,
			"u1-night-2": body,
			"u1-night-3": body,
		},
	}

	params := e2eParams(report.LevelPersonal, "u1", p, e2ePeriod())
	params.Semantic = &SemanticGateway{
		BaseURL: srv.URL,
		Key:     "test-key",
		Model:   "bf-glm-5.3-flash",
		Timeout: 5 * time.Second,
	}

	r, err := GetReport(context.Background(), params)
	if err != nil {
		t.Fatalf("GetReport failed: %v", err)
	}

	if !r.SafetyAnomaly.Hit {
		t.Fatal("期望 3.6 命中")
	}
	if r.SafetyAnomaly.ReviewTag != "疑似加班未备案" {
		t.Errorf("ReviewTag = %q, want 疑似加班未备案", r.SafetyAnomaly.ReviewTag)
	}
	if !strings.Contains(r.SafetyAnomaly.Hint, "疑似加班未备案") || !strings.Contains(r.SafetyAnomaly.Hint, "核实备案") {
		t.Errorf("Hint 期望包含提示核实备案, got %q", r.SafetyAnomaly.Hint)
	}
	if r.SafetyAnomaly.Status != report.SafetyAnomalyPendingReview {
		t.Errorf("Status 必须保持 pending_review, got %q", r.SafetyAnomaly.Status)
	}
	if len(r.SafetyAnomaly.ContentSamples) == 0 {
		t.Fatal("期望记录正文内容样本")
	}

	// 验证 Markdown 渲染输出
	md := report.RenderMD(r)
	if !strings.Contains(md, "疑似加班未备案") {
		t.Errorf("Markdown 应包含「疑似加班未备案」, got:\n%s", md)
	}
	if !strings.Contains(md, "核实备案") {
		t.Errorf("Markdown 应包含「核实备案」提示, got:\n%s", md)
	}
}

// TestGetReport_E2E_Department_SemanticSafetyAnomaly 验证 orchestrator 完整管线下，
// 部门报告中各成员的 3.6 严重异常语义标记被正确装配进 MemberEvaluation。
func TestGetReport_E2E_Department_SemanticSafetyAnomaly(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}

	body := `{"messages":[{"role":"user","content":"排查线上故障"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"work_related\",\"confidence\":0.85}"}}]}`)
	}))
	defer srv.Close()

	u1 := ReportUser{UserID: "u1", Name: "甲", Position: "AI工程师", DeptID: "X-Lab"}
	p := &memProvider{
		users:    map[string]ReportUser{"u1": u1},
		callRows: map[string][]CallLogRow{"u1": e2eUserRows("u1")},
		outRows:  map[string][]OutputRunawayRow{},
		overRows: map[string][]OverModelRow{},
		offRows:  map[string][]OffhoursCallRow{"u1": e2eOffRows("u1", 1.0)},
		dq:       report.DataQuality{RealSessionCoverage: 0.7, SceneLabelCoverage: 0.5, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
		bodies: map[string]string{
			"u1-night-1": body,
		},
	}

	params := e2eParams(report.LevelDepartment, "X-Lab", p, e2ePeriod())
	params.Semantic = &SemanticGateway{
		BaseURL: srv.URL,
		Key:     "test-key",
		Model:   "bf-glm-5.3-flash",
		Timeout: 5 * time.Second,
	}

	r, err := GetReport(context.Background(), params)
	if err != nil {
		t.Fatalf("GetReport dept failed: %v", err)
	}
	if r.MemberEvaluation == nil || len(r.MemberEvaluation.Members) == 0 {
		t.Fatal("期望部门包含成员")
	}
	m1 := r.MemberEvaluation.Members[0]
	if !m1.SafetyAnomaly {
		t.Fatal("期望 m1 命中 SafetyAnomaly")
	}
	if m1.SafetyReviewTag != "疑似加班未备案" {
		t.Errorf("m1.SafetyReviewTag = %q, want 疑似加班未备案", m1.SafetyReviewTag)
	}

	md := report.RenderMD(r)
	if !strings.Contains(md, "待核查（疑似加班未备案）") {
		t.Errorf("部门 Markdown 应呈现 待核查（疑似加班未备案）, got:\n%s", md)
	}
}

// ---- T4 部门/工作室大规模数据聚合性能与并发优化测试 ----

// TestDetermineGroupWorkers 验证并发协程数边界控制（bounded 4-8）
func TestDetermineGroupWorkers(t *testing.T) {
	if got := determineGroupWorkers(0, 0); got != 1 {
		t.Errorf("0 users: got %d, want 1", got)
	}
	if got := determineGroupWorkers(1, 0); got != 1 {
		t.Errorf("1 user: got %d, want 1", got)
	}
	if got := determineGroupWorkers(3, 0); got != 3 {
		t.Errorf("3 users with default (capped by count): got %d, want 3", got)
	}
	if got := determineGroupWorkers(10, 1); got != 1 {
		t.Errorf("explicit serial Concurrency=1: got %d, want 1", got)
	}
	if got := determineGroupWorkers(10, 6); got != 6 {
		t.Errorf("custom Concurrency=6: got %d, want 6", got)
	}
	// 默认在 4 到 8 之间
	gotDef := determineGroupWorkers(20, 0)
	if gotDef < 4 || gotDef > 8 {
		t.Errorf("default workers for 20 users should be in [4,8], got %d", gotDef)
	}
}

// TestBuildGroupReport_ConcurrencyEquivalence 验证：
// 并发分发（4-8 workers）与强制串行（Concurrency=1）计算出的报表结果严格一致（成员顺序、归因成本、四象限落位与 Markdown 渲染）。
func TestBuildGroupReport_ConcurrencyEquivalence(t *testing.T) {
	userCount := 8
	users := make(map[string]ReportUser, userCount)
	callRows := make(map[string][]CallLogRow, userCount)
	offRows := make(map[string][]OffhoursCallRow, userCount)
	outRows := make(map[string][]OutputRunawayRow, userCount)
	overRows := make(map[string][]OverModelRow, userCount)

	positions := []string{"AI工程师", "后端", "前端", "算法专家"}

	for i := 0; i < userCount; i++ {
		uid := fmt.Sprintf("u%02d", i)
		pos := positions[i%len(positions)]
		users[uid] = ReportUser{
			UserID:   uid,
			Name:     fmt.Sprintf("成员%02d", i),
			Position: pos,
			DeptID:   "迅猛龙部门",
		}
		// 生成调用行
		var rows []CallLogRow
		for r := 0; r < 50; r++ {
			ts := e2eDay.Add(time.Duration(r*5) * time.Minute)
			rows = append(rows, CallLogRow{
				RequestID:    fmt.Sprintf("%s-r%d", uid, r),
				UserID:       uid,
				KeyID:        "k1",
				TS:           ts,
				Model:        "gpt-4o",
				PromptTokens: int64(100 + (r%5)*50),
				CachedTokens: int64(r % 2 * 50),
				InputCostUSD: 0.05,
			})
		}
		callRows[uid] = rows
		offRows[uid] = e2eOffRows(uid, 0.5)
	}

	p := &memProvider{
		users:    users,
		callRows: callRows,
		outRows:  outRows,
		overRows: overRows,
		offRows:  offRows,
		dq:       report.DataQuality{RealSessionCoverage: 0.8, SceneLabelCoverage: 0.6, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
	}

	// 1. 强制串行 (Concurrency=1)
	pSerial := e2eParams(report.LevelDepartment, "迅猛龙部门", p, e2ePeriod())
	pSerial.Concurrency = 1
	rSerial, err := GetReport(context.Background(), pSerial)
	if err != nil {
		t.Fatalf("serial GetReport failed: %v", err)
	}

	// 2. 默认并发 (Concurrency=0 -> 4-8 workers)
	pDefault := e2eParams(report.LevelDepartment, "迅猛龙部门", p, e2ePeriod())
	pDefault.Concurrency = 0
	rDefault, err := GetReport(context.Background(), pDefault)
	if err != nil {
		t.Fatalf("default concurrent GetReport failed: %v", err)
	}

	// 3. 指定并发 (Concurrency=4)
	pPool4 := e2eParams(report.LevelDepartment, "迅猛龙部门", p, e2ePeriod())
	pPool4.Concurrency = 4
	rPool4, err := GetReport(context.Background(), pPool4)
	if err != nil {
		t.Fatalf("concurrency 4 GetReport failed: %v", err)
	}

	// 校验成本汇总严格一致
	if math.Abs(rSerial.CostBreakdown.Total()-rDefault.CostBreakdown.Total()) > 1e-9 {
		t.Errorf("Total cost mismatch: serial=%v, default=%v", rSerial.CostBreakdown.Total(), rDefault.CostBreakdown.Total())
	}
	if math.Abs(rSerial.CostBreakdown.Total()-rPool4.CostBreakdown.Total()) > 1e-9 {
		t.Errorf("Total cost mismatch: serial=%v, pool4=%v", rSerial.CostBreakdown.Total(), rPool4.CostBreakdown.Total())
	}

	// 校验成员数与顺序严格一致
	if len(rSerial.MemberEvaluation.Members) != len(rDefault.MemberEvaluation.Members) {
		t.Fatalf("Member count mismatch: %d vs %d", len(rSerial.MemberEvaluation.Members), len(rDefault.MemberEvaluation.Members))
	}

	for i := range rSerial.MemberEvaluation.Members {
		s := rSerial.MemberEvaluation.Members[i]
		d := rDefault.MemberEvaluation.Members[i]
		p4 := rPool4.MemberEvaluation.Members[i]

		if s.UserID != d.UserID || s.UserID != p4.UserID {
			t.Errorf("Member index %d UserID mismatch: serial=%s, default=%s, pool4=%s", i, s.UserID, d.UserID, p4.UserID)
		}
		if math.Abs(s.CostUSD-d.CostUSD) > 1e-9 {
			t.Errorf("Member %s CostUSD mismatch: serial=%v, default=%v", s.UserID, s.CostUSD, d.CostUSD)
		}
		if s.Quadrant != d.Quadrant {
			t.Errorf("Member %s Quadrant mismatch: serial=%s, default=%s", s.UserID, s.Quadrant, d.Quadrant)
		}
		if s.SafetyAnomaly != d.SafetyAnomaly {
			t.Errorf("Member %s SafetyAnomaly mismatch: serial=%v, default=%v", s.UserID, s.SafetyAnomaly, d.SafetyAnomaly)
		}
	}

	// 校验渲染出的 Markdown 完全一致
	mdSerial := report.RenderMD(rSerial)
	mdDefault := report.RenderMD(rDefault)
	if mdSerial != mdDefault {
		t.Errorf("Markdown rendering mismatch between serial and concurrent")
	}
}

// TestBuildGroupReport_Scale26kPerformance 性能压测与验收：
// 模拟迅猛龙部门 20 位成员、单周共计 2.6 万条请求规模，
// 验证必须在 10~15 秒内（实测 < 3秒）完成全量规则装配与四象限计算，且 Validate 契约严格通过。
func TestBuildGroupReport_Scale26kPerformance(t *testing.T) {
	const userCount = 20
	const callsPerUser = 1300 // 20 * 1300 = 26,000 请求
	totalCalls := userCount * callsPerUser

	users := make(map[string]ReportUser, userCount)
	callRows := make(map[string][]CallLogRow, userCount)
	offRows := make(map[string][]OffhoursCallRow, userCount)
	outRows := make(map[string][]OutputRunawayRow, userCount)
	overRows := make(map[string][]OverModelRow, userCount)

	positions := []string{"AI工程师", "全栈开发", "测试专家", "算法研究员", "数据分析师"}

	for i := 0; i < userCount; i++ {
		uid := fmt.Sprintf("raptor-%02d", i)
		pos := positions[i%len(positions)]
		users[uid] = ReportUser{
			UserID:   uid,
			Name:     fmt.Sprintf("迅猛龙研发%02d", i),
			Position: pos,
			DeptID:   "迅猛龙部门",
		}

		rows := make([]CallLogRow, 0, callsPerUser)
		for c := 0; c < callsPerUser; c++ {
			// 模拟 7 天内的请求序列
			minuteOffset := (c * 7) % (7 * 24 * 60)
			ts := e2eDay.Add(time.Duration(minuteOffset) * time.Minute)
			prompt := int64(150 + (c%10)*30)
			cached := int64(0)
			if c%3 == 0 {
				cached = prompt - 30
			}
			rows = append(rows, CallLogRow{
				RequestID:     fmt.Sprintf("%s-r%04d", uid, c),
				UserID:        uid,
				KeyID:         fmt.Sprintf("k-%d", i%3),
				TS:            ts,
				Model:         "gpt-4o",
				PromptTokens:  prompt,
				CachedTokens:  cached,
				InputCostUSD:  float64(prompt) * 0.000005,
				OutputCostUSD: 0.0002,
			})
		}
		callRows[uid] = rows
		offRows[uid] = e2eOffRows(uid, 5.0)
	}

	p := &memProvider{
		users:    users,
		callRows: callRows,
		outRows:  outRows,
		overRows: overRows,
		offRows:  offRows,
		dq:       report.DataQuality{RealSessionCoverage: 0.9, SceneLabelCoverage: 0.8, PriceCatalogCoverage: 0.95, PositionLabelCoverage: 1.0},
	}

	params := e2eParams(report.LevelDepartment, "迅猛龙部门", p, e2ePeriod())

	start := time.Now()
	r, err := GetReport(context.Background(), params)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("2.6万条规模 GetReport 失败: %v", err)
	}
	if r == nil {
		t.Fatal("返回 report 为 nil")
	}

	// 性能硬指标：必须在 10~15 秒内装配完成（防止 60 秒 HTTP 超时）
	t.Logf("✔ 2.6万请求规模（%d 用户 × %d 请求 = %d 请求）装配耗时: %v", userCount, callsPerUser, totalCalls, elapsed)
	if elapsed > 10*time.Second {
		t.Errorf("装配耗时 %v 超过了 10 秒性能红线", elapsed)
	}

	// 验证结果契约
	if err := r.Validate(); err != nil {
		t.Fatalf("装配后的 Report Validate() 失败: %v", err)
	}
	if len(r.MemberEvaluation.Members) != userCount {
		t.Errorf("成员数量 = %d, want %d", len(r.MemberEvaluation.Members), userCount)
	}

	// 验证总调用次数等于 26,000
	totalReportCalls := 0
	for _, m := range r.MemberEvaluation.Members {
		totalReportCalls += m.Calls
	}
	if totalReportCalls != totalCalls {
		t.Errorf("报表成员总调用数 = %d, want %d", totalReportCalls, totalCalls)
	}
}
