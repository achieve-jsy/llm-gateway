package handler

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
)

// ─── AI 分析卡 · 确定性规则引擎 ─────────────────────────────────────────────
// POST /api/v1/analytics/insight
// 三段式「事实 / 原因 / 建议」，每条文本锚定真实聚合数字并带下钻信息。
// 逻辑对齐 web/prototype/model-analytics.js 的 renderInsight；MVP 用确定性规则
// 引擎（不接真 LLM），保证数字与页面真实计算一致、不幻觉。

// budgetAllocationRow 预算分配表(budget_allocations)查询行。
// 限额字段 -1 表示不限；区间预算按 日→月→周 优先折算。
type budgetAllocationRow struct {
	AmountUSD       float64
	WeeklyAmountUSD float64
	DailyAmountUSD  float64
}

// highTierName 高阶模型等级标识，对齐原型 HIGH_TIERS={'L3-Premium'}。
const highTierName = "L3-Premium"

// tierLabel 等级展示名，对齐原型 TIER_LABEL。
var tierLabel = map[string]string{
	"L3-Premium":   "L3 · 高级",
	"L2-Advanced":  "L2 · 进阶",
	"L1-Basic":     "L1 · 基础",
	"unclassified": "未分级",
}

// ─── 请求 / 响应类型 ────────────────────────────────────────────────────────

// InsightScope 洞察范围：kind=company|dept|model|tier，key 为对应筛选值。
type InsightScope struct {
	Kind string `json:"kind"`
	Key  string `json:"key,omitempty"`
}

// InsightRequest POST body。
type InsightRequest struct {
	Start string       `json:"start"`
	End   string       `json:"end"`
	Scope InsightScope `json:"scope"`
}

// Drill 下钻路径（前端用于回填筛选/滚动定位）。
type Drill struct {
	Dept   string `json:"dept,omitempty"`
	Model  string `json:"model,omitempty"`
	Scroll string `json:"scroll,omitempty"`
}

// Segment 一条洞察文本；Est=true 表示该数字为估算值。
type Segment struct {
	Text  string `json:"text"`
	Drill *Drill `json:"drill,omitempty"`
	Est   *bool  `json:"est,omitempty"`
}

// InsightScopeInfo 响应中的范围信息。
type InsightScopeInfo struct {
	Kind  string `json:"kind"`
	Key   string `json:"key,omitempty"`
	Label string `json:"label"`
}

// InsightMetrics 指标锚点，与文本内数字保持一致。
type InsightMetrics struct {
	Cost             float64 `json:"cost"`
	PrevCost         float64 `json:"prev_cost"`
	DeltaPct         float64 `json:"delta_pct"`
	BudgetPct        float64 `json:"budget_pct"`
	Budget           float64 `json:"budget"`
	BudgetConfigured bool    `json:"budget_configured"`
	HighTierPct      float64 `json:"high_tier_pct"`
	CacheRate        float64 `json:"cache_rate"`
}

// InsightSegments 三段式输出。
type InsightSegments struct {
	Facts       []Segment `json:"facts"`
	Causes      []Segment `json:"causes"`
	Suggestions []Segment `json:"suggestions"`
}

// InsightResponse 完整响应。
type InsightResponse struct {
	Scope    InsightScopeInfo `json:"scope"`
	Gmed     float64          `json:"gmed"`
	Metrics  InsightMetrics   `json:"metrics"`
	Segments InsightSegments  `json:"segments"`
}

// ─── 规则引擎输入聚合结构 ───────────────────────────────────────────────────

type insightModel struct {
	Model    string
	Tier     string
	Cost     float64
	PrevCost float64
}

func (m insightModel) Delta() float64 { return m.Cost - m.PrevCost }

func (m insightModel) DeltaPct() float64 {
	if m.PrevCost > 0 {
		return (m.Cost - m.PrevCost) / m.PrevCost * 100
	}
	return 0
}

type insightDept struct {
	Dept      string
	Cost      float64
	UserCount int
}

type insightDeptDelta struct {
	Dept     string
	Cost     float64
	PrevCost float64
}

func (d insightDeptDelta) Delta() float64 { return d.Cost - d.PrevCost }

type insightUser struct {
	UserID   string
	UserName string
	DeptName string
	Cost     float64
}

type insightUserModel struct {
	Model string
	Tier  string
	Cost  float64
}

type insightMetrics struct {
	ScopeKind    string
	ScopeKey     string
	ScopeLabel   string
	ScopeDisplay string
	RangeLabel   string
	Days         int
	Budget       float64
	BudgetConfigured bool
	Cost         float64
	PrevCost     float64
	HighCost     float64
	InTokens     int64
	CachedTokens int64
	Gmed         float64

	Models []insightModel // 当前范围按费用降序
	Depts  []insightDept  // 当前范围按费用降序
	Users  []insightUser  // 当前范围按费用降序

	DeptMovers []insightDeptDelta // 仅 model/tier 范围：按 |Δcost| 降序

	TopUser         insightUser
	TopUserModels   []insightUserModel // 头部用户模型分布，按费用降序
	TopUserPeakHour int
	TopUserPeakCost float64
}

// ─── 格式化辅助 ─────────────────────────────────────────────────────────────

func fmtCost(v float64) string { return fmt.Sprintf("$%.2f", v) }
func fmtPct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func fmtPct1(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// fmtDeltaPct 环比百分比：>=0 加 "+"，负数用其绝对值（对齐原型 delta>=0?'+':''+absD）。
func fmtDeltaPct(delta float64) string {
	s := fmt.Sprintf("%.1f%%", math.Abs(delta))
	if delta >= 0 {
		return "+" + s
	}
	return s
}

// fmtSignedCost 区间变动金额：正数 "+$X"，负数 "−$X"（U+2212，对齐原型）。
func fmtSignedCost(d float64) string {
	s := fmt.Sprintf("$%.2f", math.Abs(d))
	if d >= 0 {
		return "+" + s
	}
	return "−" + s
}

// fmtSignedPct 该模型环比百分比：>=0 加 "+"。
func fmtSignedPct(p float64) string {
	if p >= 0 {
		return "+" + fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

func fmtHour(h int) string { return fmt.Sprintf("%02d:00", h) }

func deltaWord(delta float64) string {
	if delta >= 0 {
		return "上涨"
	}
	return "下降"
}

func tierDisplay(tier string) string {
	if l, ok := tierLabel[tier]; ok {
		return l
	}
	return tier
}

// drillHTML 生成下钻锚点 HTML，对齐原型 drillHtml：data-dept/data-model/data-scroll 属性。
// 前端组件读取 dataset 回填筛选并滚动定位；结构化 Drill 字段仍随响应下发用于 emit。
func drillHTML(d *Drill) string {
	if d == nil {
		return ""
	}
	dept := ""
	if d.Dept != "" {
		dept = `data-dept="` + d.Dept + `" `
	}
	model := ""
	if d.Model != "" {
		model = `data-model="` + d.Model + `" `
	}
	scroll := d.Scroll
	if scroll == "" {
		scroll = "overview"
	}
	return ` <a class="ai-drill" ` + dept + model + `data-scroll="` + scroll + `">下钻</a>`
}

// scopeInfo 由 kind/key 生成响应标签与正文展示名。
func scopeInfo(kind, key string) (label, display string) {
	switch kind {
	case "dept":
		return "部门：" + key, key
	case "model":
		return "模型：" + key, key
	case "tier":
		l := tierDisplay(key)
		return "等级：" + l, l
	default:
		return "全司", "全司"
	}
}

// medianOf 返回升序数值切片的中位数；空切片返回 0。
func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := make([]float64, len(values))
	copy(s, values)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// ─── 规则引擎：buildInsight ─────────────────────────────────────────────────

// buildInsight 把区间聚合指标翻译为三段式洞察。纯函数，便于确定性单测。
// 对齐 web/prototype/model-analytics.js 的 renderInsight；每条文本锚定真实数字。
func buildInsight(m insightMetrics) InsightResponse {
	resp := InsightResponse{
		Scope: InsightScopeInfo{Kind: m.ScopeKind, Key: m.ScopeKey, Label: m.ScopeLabel},
		Gmed:  m.Gmed,
	}

	cost := m.Cost
	pCost := m.PrevCost
	delta := 0.0
	if pCost > 0 {
		delta = (cost - pCost) / pCost * 100
	}
	absD := math.Abs(delta)
	budPct := 0.0
	if cost > 0 && m.BudgetConfigured && m.Budget > 0 {
		budPct = cost / m.Budget * 100
	}
	hp := 0.0
	if cost > 0 {
		hp = m.HighCost / cost * 100
	}
	cacheRate := 0.0
	if m.InTokens+m.CachedTokens > 0 {
		cacheRate = float64(m.CachedTokens) / float64(m.InTokens+m.CachedTokens) * 100
	}

	resp.Metrics = InsightMetrics{
		Cost: cost, PrevCost: pCost, DeltaPct: delta,
		BudgetPct: budPct, Budget: m.Budget, BudgetConfigured: m.BudgetConfigured,
		HighTierPct: hp, CacheRate: cacheRate,
	}

	var topM *insightModel
	if len(m.Models) > 0 {
		topM = &m.Models[0]
	}

	// 环比主因 = |Δcost| 最大的模型
	var mover *insightModel
	if len(m.Models) > 0 {
		mover = &m.Models[0]
		for i := range m.Models {
			if math.Abs(m.Models[i].Delta()) > math.Abs(mover.Delta()) {
				mover = &m.Models[i]
			}
		}
	}

	var topD *insightDept
	if len(m.Depts) > 0 {
		topD = &m.Depts[0]
	}
	topDAvg := 0.0
	if topD != nil && topD.UserCount > 0 {
		topDAvg = topD.Cost / float64(topD.UserCount)
	}
	ratio := 0.0
	if m.Gmed > 0 {
		ratio = topDAvg / m.Gmed
	}

	var topU *insightUser
	if len(m.Users) > 0 {
		topU = &m.Users[0]
	}
	topUShare := 0.0
	if topU != nil && cost > 0 {
		topUShare = topU.Cost / cost * 100
	}

	var topUM *insightUserModel
	if len(m.TopUserModels) > 0 {
		topUM = &m.TopUserModels[0]
	}

	topUDeptTot := 0.0
	if topU != nil {
		for _, d := range m.Depts {
			if d.Dept == topU.DeptName {
				topUDeptTot = d.Cost
				break
			}
		}
	}
	topUDeptShare := 0.0
	if topU != nil && topUDeptTot > 0 {
		topUDeptShare = topU.Cost / topUDeptTot * 100
	}
	topUModelConc := 0.0
	if topU != nil && topUM != nil && topU.Cost > 0 {
		topUModelConc = topUM.Cost / topU.Cost * 100
	}

	saveEst := 0.0
	if topUM != nil && topUM.Tier == highTierName {
		saveEst = topUM.Cost * 0.3
	}

	estTrue := true
	drill := func(dept, model, scroll string) *Drill {
		if dept == "" && model == "" {
			return nil
		}
		return &Drill{Dept: dept, Model: model, Scroll: scroll}
	}

	var facts, causes, suggestions []Segment

	// ── 事实 ──
	if cost <= 0 && len(m.Users) == 0 {
		facts = append(facts, Segment{Text: "当前范围无数据，调整筛选后再看分析。"})
	} else {
		switch m.ScopeKind {
		case "company":
			budgetTxt := "未配置"
			if m.BudgetConfigured {
				budgetTxt = fmtPct0(budPct)
			}
			facts = append(facts, Segment{Text: fmt.Sprintf("区间费用 %s / 预算 %s / 环比 %s，高阶模型（L3）占 %s。",
				fmtCost(cost), budgetTxt, fmtDeltaPct(delta), fmtPct0(hp))})
			if topM != nil && cost > 0 {
				facts = append(facts, Segment{
					Text:  fmt.Sprintf("费用大头在 %s（%s，%s，占 %s）。", topM.Model, tierDisplay(topM.Tier), fmtCost(topM.Cost), fmtPct0(topM.Cost/cost*100)),
					Drill: drill("", topM.Model, "overview"),
				})
			}
			if ratio >= 2 && topD != nil && topU != nil {
				facts = append(facts, Segment{
					Text:  fmt.Sprintf("%s 人均 %s，超全司中位数 %.1fx；%s 占全司 %s。", topD.Dept, fmtCost(topDAvg), ratio, topU.UserName, fmtPct0(topUShare)),
					Drill: drill(topD.Dept, "", "dept"),
				})
			}
		case "dept":
			uavg := 0.0
			if len(m.Users) > 0 {
				uavg = cost / float64(len(m.Users))
			}
			uratio := 0.0
			if m.Gmed > 0 {
				uratio = uavg / m.Gmed
			}
			facts = append(facts, Segment{Text: fmt.Sprintf("%s 区间费用 %s / 环比 %s / 高阶模型占 %s。",
				m.ScopeDisplay, fmtCost(cost), fmtDeltaPct(delta), fmtPct0(hp))})
			if topM != nil && cost > 0 {
				facts = append(facts, Segment{
					Text:  fmt.Sprintf("本部门费用大头在 %s（占 %s）。", topM.Model, fmtPct0(topM.Cost/cost*100)),
					Drill: drill("", topM.Model, "overview"),
				})
			}
			if topU != nil {
				facts = append(facts, Segment{
					Text:  fmt.Sprintf("部门人均 %s，超全司中位数 %.1fx；%s 一人占本部门 %s。", fmtCost(uavg), uratio, topU.UserName, fmtPct0(topUDeptShare)),
					Drill: drill(m.ScopeKey, "", "dept"),
				})
			}
		default: // model / tier
			facts = append(facts, Segment{Text: fmt.Sprintf("%s 区间费用 %s / 环比 %s / 缓存命中 %s。",
				m.ScopeDisplay, fmtCost(cost), fmtDeltaPct(delta), fmtPct1(cacheRate))})
			if topU != nil {
				facts = append(facts, Segment{
					Text:  fmt.Sprintf("由 %s（%s）消耗最多，占该维度费用 %s。", topU.UserName, topU.DeptName, fmtPct0(topUShare)),
					Drill: drill("", "", "users"),
				})
			}
		}
	}

	// ── 原因 ──
	if cost > 0 {
		switch m.ScopeKind {
		case "company":
			if mover != nil {
				causes = append(causes, Segment{
					Text:  fmt.Sprintf("环比%s %s，主因 %s（区间 %s，该模型环比 %s）。", deltaWord(delta), fmtPct1(absD), mover.Model, fmtSignedCost(mover.Delta()), fmtSignedPct(mover.DeltaPct())),
					Drill: drill("", mover.Model, "overview"),
				})
			}
			if ratio >= 2 && topD != nil && topU != nil && topUM != nil {
				causes = append(causes, Segment{
					Text:  fmt.Sprintf("%s 人均偏高的主因是 %s 一人占部门费用 %s，且其调用 %s 集中在 %s（%s）。", topD.Dept, topU.UserName, fmtPct0(topUDeptShare), fmtPct0(topUModelConc), topUM.Model, tierDisplay(topUM.Tier)),
					Drill: drill(topD.Dept, "", "dept"),
				})
			}
			if topU != nil {
				causes = append(causes, Segment{Text: fmt.Sprintf("%s 的调用集中在工作时段，%s 单小时达 %s，峰值明显放大。",
					topU.UserName, fmtHour(m.TopUserPeakHour), fmtCost(m.TopUserPeakCost))})
			}
		case "dept":
			if mover != nil {
				causes = append(causes, Segment{
					Text:  fmt.Sprintf("本部门环比%s %s，主因 %s（区间 %s）。", deltaWord(delta), fmtPct1(absD), mover.Model, fmtSignedCost(mover.Delta())),
					Drill: drill("", mover.Model, "overview"),
				})
			}
			if topU != nil && topUM != nil && topUDeptShare >= 40 {
				causes = append(causes, Segment{Text: fmt.Sprintf("人均偏高的主因是 %s 一人占部门 %s，其调用 %s 集中在 %s（%s）。",
					topU.UserName, fmtPct0(topUDeptShare), fmtPct0(topUModelConc), topUM.Model, tierDisplay(topUM.Tier))})
			}
			if topU != nil {
				causes = append(causes, Segment{Text: fmt.Sprintf("%s 高峰在 %s，单小时 %s。",
					topU.UserName, fmtHour(m.TopUserPeakHour), fmtCost(m.TopUserPeakCost))})
			}
		default: // model / tier
			var dMover *insightDeptDelta
			if len(m.DeptMovers) > 0 {
				dMover = &m.DeptMovers[0]
			}
			if dMover != nil {
				causes = append(causes, Segment{
					Text:  fmt.Sprintf("该维度环比%s %s，主因 %s（区间 %s）。", deltaWord(delta), fmtPct1(absD), dMover.Dept, fmtSignedCost(dMover.Delta())),
					Drill: drill(dMover.Dept, "", "dept"),
				})
			}
			if topU != nil {
				causes = append(causes, Segment{Text: fmt.Sprintf("%s 高峰在 %s，单小时 %s。",
					topU.UserName, fmtHour(m.TopUserPeakHour), fmtCost(m.TopUserPeakCost))})
			}
		}
	}

	// ── 建议 ──
	if cost > 0 {
		if saveEst > 0 && topU != nil {
			suggestions = append(suggestions, Segment{
				Text:  fmt.Sprintf("对 %s / %s 非关键链路的 %s 调用降至 L2/L1，按 30%% 可降级估，区间约省 %s。", topU.DeptName, topU.UserName, topUM.Model, fmtCost(saveEst)),
				Drill: drill(topU.DeptName, "", "dept"),
				Est:   &estTrue,
			})
		}
		if cacheRate < 40 {
			suggestions = append(suggestions, Segment{Text: fmt.Sprintf("整体缓存命中率仅 %s，对高频重复输入开启 prompt 缓存可压降输入费用。", fmtPct1(cacheRate))})
		}
		if topU != nil {
			suggestions = append(suggestions, Segment{
				Text:  fmt.Sprintf("下钻 %s（%s）%s 小时级明细，确认 %s 峰值调用是否可控/可回收。", topU.UserName, topU.DeptName, m.RangeLabel, fmtHour(m.TopUserPeakHour)),
				Drill: drill(topU.DeptName, "", "dept"),
			})
		}
	}

	// 每条带 Drill 的文本追加下钻锚点（data-dept/data-model/data-scroll），对齐原型 drillHtml。
	for _, arr := range [][]Segment{facts, causes, suggestions} {
		for i := range arr {
			if arr[i].Drill != nil {
				arr[i].Text += drillHTML(arr[i].Drill)
			}
		}
	}

	resp.Segments = InsightSegments{Facts: facts, Causes: causes, Suggestions: suggestions}
	return resp
}

// ─── HTTP handler ───────────────────────────────────────────────────────────

// AnalyticsInsight POST /api/v1/analytics/insight
func (h *UsageHandler) AnalyticsInsight(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	var req InsightRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	from, to, ok := parseInsightDates(c, req.Start, req.End)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)

	scope := req.Scope
	if scope.Kind == "" {
		scope.Kind = "company"
	}
	switch scope.Kind {
	case "company", "dept", "model", "tier":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope kind"})
		return
	}

	db := h.db.WithContext(c.Request.Context())
	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	days := int(to.Sub(from).Hours()/24) + 1
	budget, budgetConfigured := resolveBudget(db, scope.Kind, scope.Key, from, days)

	modelFilter, tierFilter, deptFilter := "", "", ""
	switch scope.Kind {
	case "model":
		modelFilter = scope.Key
	case "tier":
		tierFilter = scope.Key
	case "dept":
		deptFilter = scope.Key
	}

	scopeLabel, scopeDisplay := scopeInfo(scope.Kind, scope.Key)

	// 上一区间 = 与当前区间等长的前一段
	prevFrom := from.Add(-time.Duration(days) * 24 * time.Hour)
	prevToExcl := toExcl.Add(-time.Duration(days) * 24 * time.Hour)

	scopedWhere := `ts >= ? AND ts < ?
		AND (` + obsmodel.WhereExcludeServiceKeyLogs + `)
		AND (? = '' OR model ILIKE ?)
		AND (? = '' OR COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = ?)
		AND (? = '' OR COALESCE(dept_name, '') ILIKE ?)`
	scopeArgs := func(f, t time.Time) []interface{} {
		return []interface{}{f, t, modelFilter, "%" + modelFilter + "%", tierFilter, tierFilter, deptFilter, "%" + deptFilter + "%"}
	}

	// 区间汇总（当前范围）
	var tot struct {
		Cost     float64 `gorm:"column:cost"`
		InTok    int64   `gorm:"column:in_tokens"`
		Cached   int64   `gorm:"column:cached_tokens"`
		HighCost float64 `gorm:"column:high_cost"`
	}
	sqlTot := `SELECT
		COALESCE(SUM(est_cost_usd), 0) AS cost,
		COALESCE(SUM(prompt_tokens), 0) AS in_tokens,
		COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
		COALESCE(SUM(CASE WHEN COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = '` + highTierName + `' THEN est_cost_usd ELSE 0 END), 0) AS high_cost
	FROM call_logs WHERE ` + scopedWhere
	if err := db.Raw(sqlTot, scopeArgs(from, toExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&tot).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 上一区间总费用（当前范围）
	var prevTot struct {
		Cost float64 `gorm:"column:cost"`
	}
	if err := db.Raw(`SELECT COALESCE(SUM(est_cost_usd), 0) AS cost FROM call_logs WHERE `+scopedWhere,
		scopeArgs(prevFrom, prevToExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&prevTot).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 全局中位数 gmed：当前区间按 user_id 汇总费用的中位数，不受 scope 筛选影响
	var userCosts []struct {
		UserID string  `gorm:"column:user_id"`
		Cost   float64 `gorm:"column:cost"`
	}
	sqlGmed := `SELECT COALESCE(user_id, 'unknown') AS user_id, COALESCE(SUM(est_cost_usd), 0) AS cost
		FROM call_logs WHERE ts >= ? AND ts < ? AND (` + obsmodel.WhereExcludeServiceKeyLogs + `)
		GROUP BY COALESCE(user_id, 'unknown')`
	if err := db.Raw(sqlGmed, from, toExcl).Scopes(usagePol.QueryScope(actor)).Scan(&userCosts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	vals := make([]float64, 0, len(userCosts))
	for _, u := range userCosts {
		vals = append(vals, u.Cost)
	}
	gmed := medianOf(vals)

	// 模型分布（当前范围）
	var mRows []struct {
		Model string  `gorm:"column:model"`
		Tier  string  `gorm:"column:tier"`
		Cost  float64 `gorm:"column:cost"`
	}
	sqlModels := `SELECT model,
		COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') AS tier,
		COALESCE(SUM(est_cost_usd), 0) AS cost
		FROM call_logs WHERE ` + scopedWhere + `
		GROUP BY model, COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified')
		ORDER BY cost DESC`
	if err := db.Raw(sqlModels, scopeArgs(from, toExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&mRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	models := make([]insightModel, 0, len(mRows))
	seenM := map[string]bool{}
	for _, r := range mRows {
		if seenM[r.Model] {
			continue
		}
		seenM[r.Model] = true
		models = append(models, insightModel{Model: r.Model, Tier: r.Tier, Cost: r.Cost})
	}

	// 模型分布（上一区间，用于环比主因）
	var pmRows []struct {
		Model string  `gorm:"column:model"`
		Cost  float64 `gorm:"column:cost"`
	}
	if err := db.Raw(`SELECT model, COALESCE(SUM(est_cost_usd), 0) AS cost FROM call_logs WHERE `+scopedWhere+` GROUP BY model`,
		scopeArgs(prevFrom, prevToExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&pmRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prevModelCost := map[string]float64{}
	for _, r := range pmRows {
		prevModelCost[r.Model] = r.Cost
	}
	for i := range models {
		if pc, ok := prevModelCost[models[i].Model]; ok {
			models[i].PrevCost = pc
		}
	}

	// 部门分布（当前范围）
	var dRows []struct {
		Dept      string  `gorm:"column:dept_name"`
		UserCount int     `gorm:"column:user_count"`
		Cost      float64 `gorm:"column:cost"`
	}
	sqlDepts := `SELECT COALESCE(NULLIF(dept_name, ''), 'unknown') AS dept_name,
		COUNT(DISTINCT COALESCE(user_id, '')) AS user_count,
		COALESCE(SUM(est_cost_usd), 0) AS cost
		FROM call_logs WHERE ` + scopedWhere + `
		GROUP BY COALESCE(NULLIF(dept_name, ''), 'unknown')
		ORDER BY cost DESC`
	if err := db.Raw(sqlDepts, scopeArgs(from, toExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&dRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	depts := make([]insightDept, 0, len(dRows))
	for _, r := range dRows {
		depts = append(depts, insightDept{Dept: r.Dept, UserCount: r.UserCount, Cost: r.Cost})
	}

	// 用户分布（当前范围）
	var uRows []struct {
		UserID   string  `gorm:"column:user_id"`
		UserName string  `gorm:"column:user_name"`
		DeptName string  `gorm:"column:dept_name"`
		Cost     float64 `gorm:"column:cost"`
	}
	sqlUsers := `SELECT COALESCE(user_id, 'unknown') AS user_id,
		COALESCE(user_name, '') AS user_name,
		COALESCE(dept_name, '') AS dept_name,
		COALESCE(SUM(est_cost_usd), 0) AS cost
		FROM call_logs WHERE ` + scopedWhere + `
		GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		ORDER BY cost DESC`
	if err := db.Raw(sqlUsers, scopeArgs(from, toExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&uRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	users := make([]insightUser, 0, len(uRows))
	for _, r := range uRows {
		users = append(users, insightUser{UserID: r.UserID, UserName: r.UserName, DeptName: r.DeptName, Cost: r.Cost})
	}

	// 头部用户模型分布 + 高峰小时（当前范围）
	var topUserModels []insightUserModel
	peakHour, peakCost := 0, 0.0
	if len(users) > 0 {
		topU := users[0]
		sqlUM := `SELECT model,
			COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') AS tier,
			COALESCE(SUM(est_cost_usd), 0) AS cost
			FROM call_logs WHERE ` + scopedWhere + ` AND user_id = ?
			GROUP BY model, COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified')
			ORDER BY cost DESC`
		umArgs := append(scopeArgs(from, toExcl), topU.UserID)
		var umRows []struct {
			Model string  `gorm:"column:model"`
			Tier  string  `gorm:"column:tier"`
			Cost  float64 `gorm:"column:cost"`
		}
		if err := db.Raw(sqlUM, umArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&umRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		seenUM := map[string]bool{}
		for _, r := range umRows {
			if seenUM[r.Model] {
				continue
			}
			seenUM[r.Model] = true
			topUserModels = append(topUserModels, insightUserModel{Model: r.Model, Tier: r.Tier, Cost: r.Cost})
		}

		sqlPeak := `SELECT EXTRACT(HOUR FROM ts AT TIME ZONE 'Asia/Shanghai')::int AS hour,
			COALESCE(SUM(est_cost_usd), 0) AS cost
			FROM call_logs WHERE ` + scopedWhere + ` AND user_id = ?
			GROUP BY 1 ORDER BY cost DESC LIMIT 1`
		peakArgs := append(scopeArgs(from, toExcl), topU.UserID)
		var peak struct {
			Hour int     `gorm:"column:hour"`
			Cost float64 `gorm:"column:cost"`
		}
		if err := db.Raw(sqlPeak, peakArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&peak).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		peakHour, peakCost = peak.Hour, peak.Cost
	}

	// 部门环比主因（仅 model/tier 范围需要上一区间部门分布）
	var deptMovers []insightDeptDelta
	if scope.Kind == "model" || scope.Kind == "tier" {
		sqlPrevDepts := `SELECT COALESCE(NULLIF(dept_name, ''), 'unknown') AS dept_name, COALESCE(SUM(est_cost_usd), 0) AS cost
			FROM call_logs WHERE ` + scopedWhere + ` GROUP BY COALESCE(NULLIF(dept_name, ''), 'unknown')`
		var pdRows []struct {
			Dept string  `gorm:"column:dept_name"`
			Cost float64 `gorm:"column:cost"`
		}
		if err := db.Raw(sqlPrevDepts, scopeArgs(prevFrom, prevToExcl)...).Scopes(usagePol.QueryScope(actor)).Scan(&pdRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		prevDeptCost := map[string]float64{}
		for _, r := range pdRows {
			prevDeptCost[r.Dept] = r.Cost
		}
		deptMovers = make([]insightDeptDelta, 0, len(depts))
		for _, d := range depts {
			deptMovers = append(deptMovers, insightDeptDelta{Dept: d.Dept, Cost: d.Cost, PrevCost: prevDeptCost[d.Dept]})
		}
		sort.Slice(deptMovers, func(i, j int) bool {
			return math.Abs(deptMovers[i].Delta()) > math.Abs(deptMovers[j].Delta())
		})
	}

	metrics := insightMetrics{
		ScopeKind:    scope.Kind,
		ScopeKey:     scope.Key,
		ScopeLabel:   scopeLabel,
		ScopeDisplay: scopeDisplay,
		RangeLabel:   from.Format("01-02") + "~" + to.Format("01-02"),
		Days:         days,
		Budget:       budget,
		BudgetConfigured: budgetConfigured,
		Cost:         tot.Cost,
		PrevCost:     prevTot.Cost,
		HighCost:     tot.HighCost,
		InTokens:     tot.InTok,
		CachedTokens: tot.Cached,
		Gmed:         gmed,
		Models:       models,
		Depts:        depts,
		Users:        users,
		DeptMovers:   deptMovers,
	}
	if len(users) > 0 {
		metrics.TopUser = users[0]
		metrics.TopUserPeakHour = peakHour
		metrics.TopUserPeakCost = peakCost
	}
	metrics.TopUserModels = topUserModels

	c.JSON(http.StatusOK, buildInsight(metrics))
}

// resolveBudget 按分析范围解析区间预算（USD）。
// company → budget_allocations scope_type='company' scope_id='GLOBAL'；
// dept   → scope_type='dept_daily' scope_id=<dept_id>；
// model/tier 无预算分配 → (0, false)。
// 折算优先级：日限额 > 0 → 日×天数；否则月限额 → 月×天数/当月天数；否则周限额 → 周×天数/7。
func resolveBudget(db *gorm.DB, scopeKind, scopeKey string, from time.Time, days int) (budget float64, configured bool) {
	scopeType, scopeID := "", ""
	switch scopeKind {
	case "company":
		scopeType, scopeID = "company", "GLOBAL"
	case "dept":
		scopeType, scopeID = "dept_daily", scopeKey
	default:
		return 0, false
	}
	var row budgetAllocationRow
	if err := db.Table("budget_allocations").
		Select("amount_usd, weekly_amount_usd, daily_amount_usd").
		Where("scope_type = ? AND scope_id = ? AND enabled", scopeType, scopeID).
		Take(&row).Error; err != nil {
		return 0, false
	}
	switch {
	case row.DailyAmountUSD > 0:
		return row.DailyAmountUSD * float64(days), true
	case row.AmountUSD > 0:
		return row.AmountUSD * float64(days) / float64(daysInMonth(from)), true
	case row.WeeklyAmountUSD > 0:
		return row.WeeklyAmountUSD * float64(days) / 7, true
	default:
		return 0, false
	}
}

// daysInMonth 返回 t 所在月的天数。
func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

// parseInsightDates 解析 start/end：优先 JSON body，缺省回退 query 参数。
// 复用 parseDateRangeFromTo 的解析格式与 90 天上限校验。
func parseInsightDates(c *gin.Context, start, end string) (from, to time.Time, ok bool) {
	if start == "" && end == "" {
		return parseDateRangeFromTo(c)
	}
	q := c.Request.URL.Query()
	if start != "" {
		q.Set("from", start)
	}
	if end != "" {
		q.Set("to", end)
	}
	c.Request.URL.RawQuery = q.Encode()
	return parseDateRangeFromTo(c)
}
