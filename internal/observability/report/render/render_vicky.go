package render

// render_vicky.go —— 把 Report 渲染成 Vicky 知识平台规范 HTML（book 模板 content 字段）。
//
// 唯一规范见 <VICKY_ORIGIN>/api/guide（本文件是它的投影）：
//   - category=business-report（面向管理层）；narrative=金字塔/结论先行；project 由调用方传入。
//   - content 由若干 <section class="reveal"><div class="wrap"><p class="section-label">…</p><h2>…</h2>…</div></section> 组成。
//   - 门禁：禁止裸 <table>（只用 .data-table / .cmp-table）；cmp-table 必须带 .cmp-verdict；
//     禁用弃用类名（.ladder-* / .quote-block / .concern-box / .phase）。
//   - 业务报告禁止实施代码/API/SQL/部署配置；只讲业务风险/成本；结论先行、量化、给选择题。
//
// 本文件只读 Report，不在模板内二次计算；所有展示值在 Go 侧格式化后注入模板（html/template 自动转义）。

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// ---- 渲染数据（全部在 Go 侧预格式化，模板只做展示） ----

type kv struct{ Label, Value string }

type vMetric struct{ Label, Value string }

type vAnomaly struct {
	Rule, Name, Severity, SevClass, Window string
	Actual, Threshold, Baseline, Impact, Excess string
	Matched string
	Dims     []kv
	Evidence []kv
	Note     string
}

type vBaseline struct {
	Current, Median, MAD, SigmaRob, DevSigma, Percentile, Trend, Window, Note string
}

type vPeer struct {
	Pool, Current, P25, P50, P75, P90, Band, VsPeer, Note string
}

type vickyData struct {
	Level, ScopeName, PeriodLabel, Date, CurrencyLine string
	Headline   string
	Metrics    []vMetric
	KeyFacts   []kv
	Models     []kv
	HasBase    bool
	Base       vBaseline
	HasPeer    bool
	Peer       vPeer
	Anomalies  []vAnomaly
	NotComputed []kv
	Risks      []string
	Actions    []string
	QA         []string
	QANote     string
}

// RenderVicky 渲染 Vicky 规范 HTML content（<section> 片段，可直接 POST /api/reports）。
func RenderVicky(r *report.Report) string {
	d := toVicky(r)
	var buf strings.Builder
	if err := vickyTmpl.Execute(&buf, d); err != nil {
		return "<pre>render error: " + template.HTMLEscapeString(err.Error()) + "</pre>"
	}
	return buf.String()
}

func toVicky(r *report.Report) vickyData {
	// 表达层：先过幻觉校验（裸数字降级），再拿 sanitize 后的措辞。
	expr := validateExpression(r)
	actions := expr.Suggestions
	if actions == nil {
		actions = []string{}
	}

	d := vickyData{
		Level:        levelLabel(r.Meta.Level),
		ScopeName:    scopeLabel(r.Meta.Scope),
		PeriodLabel:  r.Meta.Period.Label,
		Date:         r.Meta.GeneratedAt,
		CurrencyLine: fmt.Sprintf("%s → %s（汇率 %s）", r.Meta.Currency.Base, r.Meta.Currency.ReportCurrency, fmt2(r.Meta.Currency.USDCNYRate)),
		Headline:     expr.Headline,
		QA:           r.Meta.QA.Alignment,
		QANote:       r.Meta.QA.Note,
		Actions:      actions,
		Risks:        risks(r),
		NotComputed:  notComputed(r),
	}

	// 事实层 → 概况 + 关键依据 + 模型分布。
	d.Metrics, d.KeyFacts, d.Models = factsToVicky(r)

	// 判断层 → 基线 / 同岗分位 / 异常。
	if r.Judgment.Baseline.Supported {
		d.HasBase = true
		b := r.Judgment.Baseline
		d.Base = vBaseline{
			Current:    fmt2(b.Current) + " " + unit(b.Metric),
			Median:     fmt2(b.Median) + " " + unit(b.Metric),
			MAD:        fmt2(b.MAD),
			SigmaRob:   fmt2(b.SigmaRob),
			DevSigma:   fmt2(b.DevSigma) + "σ",
			Percentile: pctP(b.Percentile),
			Trend:      b.Trend,
			Window:     b.CurrentWindow,
			Note:       b.Note,
		}
	}
	if r.Judgment.Peer.Supported {
		d.HasPeer = true
		p := r.Judgment.Peer
		d.Peer = vPeer{
			Pool:    p.PoolType,
			Current: fmt2(p.Current),
			P25:     fmt2(p.P25), P50: fmt2(p.P50), P75: fmt2(p.P75), P90: fmt2(p.P90),
			Band:    p.Band,
			VsPeer:  p.VsPeer,
			Note:    p.Note,
		}
	}
	d.Anomalies = anomaliesToVicky(r.Judgment.Anomalies)
	return d
}

func unit(metric string) string {
	if strings.Contains(metric, "cost") {
		return "USD"
	}
	return ""
}

func factsToVicky(r *report.Report) (metrics []vMetric, facts []kv, models []kv) {
	switch {
	case r.Fact.Personal != nil:
		p := r.Fact.Personal
		metrics = append(metrics,
			vMetric{"调用数", fmt.Sprintf("%d", p.ReqCount)},
			vMetric{"成功率", pct(p.SuccessCount, p.ReqCount)},
			vMetric{"总 Token", fmt.Sprintf("%d", p.TotalTokens)},
			vMetric{"成本", fmt2(p.EstCostUSD) + " USD"},
		)
		facts = append(facts,
			kv{"调用数", itoa(p.ReqCount)}, kv{"成功数", itoa(p.SuccessCount)}, kv{"失败数", itoa(p.ErrorCount)},
			kv{"成功率", pct(p.SuccessCount, p.ReqCount)},
			kv{"Prompt Token", itoa(p.PromptTokens)}, kv{"Completion Token", itoa(p.CompletionTokens)},
			kv{"Reasoning Token", itoa(p.ReasoningTokens)}, kv{"Total Token(=prompt+completion+reasoning)", itoa(p.TotalTokens)},
			kv{"成本(USD)", fmt2(p.EstCostUSD)}, kv{"活跃天数", itoa(int64(p.ActiveDays))},
			kv{"模型数", itoa(int64(p.ModelCount))}, kv{"平均耗时", fmt2(p.AvgDurationMs) + "ms"},
		)
		if r.Judgment.Efficiency.CostUSDCNY > 0 {
			facts = append(facts, kv{"成本(CNY)", fmt2(r.Judgment.Efficiency.CostUSDCNY)})
		}
		for _, m := range p.TopModels {
			models = append(models, kv{m.Model, fmt.Sprintf("%d 次 · %d tok · $%s", m.Calls, m.TotalTokens, fmt2(m.CostUSD))})
		}
	case r.Fact.Department != nil:
		u := r.Fact.Department.Member
		metrics = append(metrics,
			vMetric{"成员数", itoa(int64(u.UserCount))},
			vMetric{"活跃成员", itoa(int64(u.ActiveUserCount))},
			vMetric{"调用数", itoa(u.ReqCount)},
			vMetric{"成本", fmt2(u.EstCostUSD) + " USD"},
		)
		facts = append(facts,
			kv{"成员数", itoa(int64(u.UserCount))}, kv{"活跃成员", itoa(int64(u.ActiveUserCount))},
			kv{"调用数", itoa(u.ReqCount)}, kv{"成功率", pct(u.SuccessCount, u.ReqCount)},
			kv{"Total Token", itoa(u.TotalTokens)}, kv{"成本(USD)", fmt2(u.EstCostUSD)},
			kv{"人均调用", itoa(u.PerCapita.ReqCount)}, kv{"人均成本", fmt2(u.PerCapita.EstCostUSD) + " USD"},
		)
	case r.Fact.Studio != nil:
		u := r.Fact.Studio.Member
		metrics = append(metrics,
			vMetric{"成员数", itoa(int64(u.UserCount))},
			vMetric{"活跃成员", itoa(int64(u.ActiveUserCount))},
			vMetric{"调用数", itoa(u.ReqCount)},
			vMetric{"成本", fmt2(u.EstCostUSD) + " USD"},
		)
		facts = append(facts,
			kv{"成员数", itoa(int64(u.UserCount))}, kv{"活跃成员", itoa(int64(u.ActiveUserCount))},
			kv{"调用数", itoa(u.ReqCount)}, kv{"成功率", pct(u.SuccessCount, u.ReqCount)},
			kv{"Total Token", itoa(u.TotalTokens)}, kv{"成本(USD)", fmt2(u.EstCostUSD)},
		)
	}
	return metrics, facts, models
}

func anomaliesToVicky(hits []report.AnomalyHit) []vAnomaly {
	out := make([]vAnomaly, 0, len(hits))
	for _, a := range hits {
		v := vAnomaly{
			Rule: a.Rule, Name: a.Name, Severity: a.Severity,
			SevClass: severityClass(a.Severity),
			Window:   a.Window,
			Actual:   fmt2(a.ActualValue), Threshold: fmt2(a.ThresholdValue), Baseline: fmt2(a.BaselineValue),
			Impact: fmt2(a.ImpactCostUSD), Excess: fmt2(a.ExcessCostUSD),
			Matched: itoa(int64(a.MatchedCount)), Note: a.Note,
		}
		for k, val := range a.Dimensions {
			v.Dims = append(v.Dims, kv{k, val})
		}
		for _, e := range a.Evidence {
			v.Evidence = append(v.Evidence, kv{e.Field, e.Value})
		}
		out = append(out, v)
	}
	return out
}

func severityClass(s string) string {
	switch s {
	case "P0", "P1":
		return "warn"
	case "P2", "P3":
		return "note"
	default:
		return "note"
	}
}

func notComputed(r *report.Report) []kv {
	out := make([]kv, 0, len(r.Judgment.NotComputed))
	for _, n := range r.Judgment.NotComputed {
		out = append(out, kv{n.Metric, n.Reason})
	}
	return out
}

func risks(r *report.Report) []string {
	var out []string
	if b := r.Judgment.Baseline; b.Supported && strings.Contains(b.CurrentWindow, "近似") {
		out = append(out, "28 天基线为「当前周期近似」，仅供自一致参考，未与真实期前历史对账。")
	}
	if r.Judgment.Peer.Supported && strings.Contains(r.Judgment.Peer.Note, "分位") {
		// 提示同岗分位为匿名比较，非个人可定位。
		out = append(out, "同岗分位为匿名比较，不定位到个人；样本不足时按 NotComputed 处理，不下结论。")
	}
	for _, n := range r.Judgment.NotComputed {
		out = append(out, n.Metric+"："+n.Reason)
	}
	if len(out) == 0 {
		out = append(out, "未见需单独提示的风险点；异常命中均已附证据。")
	}
	return out
}

func itoa(n int64) string { return fmt.Sprintf("%d", n) }

// ---- 模板 ----

var vickyTmpl = template.Must(template.New("vicky").Funcs(template.FuncMap{
	"join": strings.Join,
	"add":  func(a, b int) int { return a + b },
}).Parse(vickyBody))

const vickyBody = `
<section class="reveal">
  <div class="wrap">
    <p class="section-label">00 · 一句话结论</p>
    <h2>一句话结论</h2>
    <blockquote>{{.Headline}}</blockquote>
    <p>级别：{{.Level}} · 范围：{{.ScopeName}} · 周期：{{.PeriodLabel}} · 币种：{{.CurrencyLine}}</p>
  </div>
</section>

<section class="reveal">
  <div class="wrap">
    <p class="section-label">01 · 使用概览</p>
    <h2>使用概览</h2>
    <div class="stats-row">
      {{range .Metrics}}<div class="metric">{{.Label}}<b>{{.Value}}</b></div>{{end}}
    </div>
    <blockquote>以上为本周用量核心数字；数据口径见文末。</blockquote>
  </div>
</section>

<section class="reveal">
  <div class="wrap">
    <p class="section-label">02 · 关键依据</p>
    <h2>关键依据</h2>
    <blockquote>以下为支撑上述结论的可核对数字（单位与口径见文末）。</blockquote>
    <table class="data-table">
      <thead><tr><th>指标</th><th>数值</th></tr></thead>
      <tbody>{{range .KeyFacts}}<tr><td>{{.Label}}</td><td>{{.Value}}</td></tr>{{end}}</tbody>
    </table>
    {{if .Models}}
    <h3>模型分布</h3>
    <table class="data-table">
      <thead><tr><th>模型</th><th>调用 · Token · 成本</th></tr></thead>
      <tbody>{{range .Models}}<tr><td>{{.Label}}</td><td>{{.Value}}</td></tr>{{end}}</tbody>
    </table>
    {{end}}
  </div>
</section>

{{if .HasBase}}
<section class="reveal">
  <div class="wrap">
    <p class="section-label">03 · 纵向基线（28 天）</p>
    <h2>纵向基线（28 天）</h2>
    <blockquote>当前位于 {{.Base.Percentile}}，趋势 {{.Base.Trend}}；{{.Base.Window}}。</blockquote>
    <table class="data-table">
      <thead><tr><th>基线项</th><th>值</th></tr></thead>
      <tbody>
        <tr><td>当前</td><td>{{.Base.Current}}</td></tr>
        <tr><td>中位数</td><td>{{.Base.Median}}</td></tr>
        <tr><td>MAD</td><td>{{.Base.MAD}}</td></tr>
        <tr><td>稳健 σ</td><td>{{.Base.SigmaRob}}</td></tr>
        <tr><td>偏离</td><td>{{.Base.DevSigma}}</td></tr>
      </tbody>
    </table>
    {{if .Base.Note}}<p class="fig-note">{{.Base.Note}}</p>{{end}}
  </div>
</section>
{{end}}

{{if .HasPeer}}
<section class="reveal">
  <div class="wrap">
    <p class="section-label">04 · 同岗分位</p>
    <h2>同岗分位</h2>
    <blockquote>当前成本效率处于同岗位「{{.Peer.Band}}」档位：{{.Peer.VsPeer}}。</blockquote>
    <table class="data-table">
      <thead><tr><th>分位</th><th>值</th></tr></thead>
      <tbody>
        <tr><td>当前</td><td>{{.Peer.Current}}</td></tr>
        <tr><td>P25</td><td>{{.Peer.P25}}</td></tr>
        <tr><td>P50</td><td>{{.Peer.P50}}</td></tr>
        <tr><td>P75</td><td>{{.Peer.P75}}</td></tr>
        <tr><td>P90</td><td>{{.Peer.P90}}</td></tr>
      </tbody>
    </table>
    <p class="fig-note">池：{{.Peer.Pool}}；{{.Peer.Note}}</p>
  </div>
</section>
{{end}}

<section class="reveal">
  <div class="wrap">
    <p class="section-label">05 · 异常（A1–A5）</p>
    <h2>异常（A1–A5）</h2>
    {{if .Anomalies}}
      {{range .Anomalies}}
      <blockquote>[{{.Rule}}] {{.Name}}（{{.Severity}}）：实际 {{.Actual}} / 阈值 {{.Threshold}} / 基线 {{.Baseline}}；影响 ${{.Impact}}，超支 ${{.Excess}}。</blockquote>
      {{if .Evidence}}
      <table class="data-table">
        <thead><tr><th>证据字段</th><th>值</th></tr></thead>
        <tbody>{{range .Evidence}}<tr><td>{{.Label}}</td><td>{{.Value}}</td></tr>{{end}}</tbody>
      </table>
      {{end}}
      <div class="callout {{.SevClass}}">{{.Rule}} · {{.Name}}（{{.Severity}}）{{if .Window}} · 窗口 {{.Window}}{{end}}{{if .Matched}} · 命中 {{.Matched}} 次{{end}}{{if .Note}} · {{.Note}}{{end}}</div>
      {{end}}
    {{else}}
      <blockquote>本周期未命中 A1–A5 异常，用量平稳。</blockquote>
    {{end}}
  </div>
</section>

{{if .Risks}}
<section class="reveal">
  <div class="wrap">
    <p class="section-label">06 · 风险与例外</p>
    <h2>风险与例外</h2>
    <div class="callout warn">{{range .Risks}}{{.}}<br>{{end}}</div>
  </div>
</section>
{{end}}

{{if .NotComputed}}
<section class="reveal">
  <div class="wrap">
    <p class="section-label">07 · 未计算项</p>
    <h2>未计算项（无依据不下结论）</h2>
    <table class="data-table">
      <thead><tr><th>项</th><th>原因</th></tr></thead>
      <tbody>{{range .NotComputed}}<tr><td>{{.Label}}</td><td>{{.Value}}</td></tr>{{end}}</tbody>
    </table>
  </div>
</section>
{{end}}

<section class="reveal">
  <div class="wrap">
    <p class="section-label">08 · 行动建议</p>
    <h2>行动建议</h2>
    {{if .Actions}}
    <div class="steps">
      {{range $i, $a := .Actions}}<div class="step"><div class="step-num">{{printf "%d" (add $i 1)}}</div><div class="step-content">{{$a}}</div></div>{{end}}
    </div>
    {{else}}
    <blockquote>本周无明确优化建议；可按需关注模型/长上下文策略。</blockquote>
    {{end}}
  </div>
</section>

<section class="reveal">
  <div class="wrap">
    <p class="section-label">09 · 口径与对账</p>
    <h2>口径与对账</h2>
    <table class="data-table">
      <thead><tr><th>口径</th></tr></thead>
      <tbody>{{range .QA}}<tr><td>{{.}}</td></tr>{{end}}</tbody>
    </table>
    {{if .QANote}}<p class="fig-note">{{.QANote}}</p>{{end}}
  </div>
</section>
`
