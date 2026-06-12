package render

// render_html.go —— 自包含 inline CSS/SVG HTML 输出（只读 Report，不在模板内二次计算）。
//
// 表达层校验：任何 LLM/措辞文本中的裸数字（不在 {} 引用内）视为幻觉，降级为模板兜底。

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/example/llm-gateway/internal/observability/report"
)

// digest 把 Report 缩为一个只含叶子数值的 digest map（供 HTML 卡片引用，避免模板二次计算）。
type digest struct {
	Level, ScopeName, PeriodLabel string
	ReqCount, TotalTokens         int64
	SuccessRate                   string
	CostUSD                       string
	BaselineSupported             bool
	BaselineMedian, BaselinePct   float64
	BaselineWindow                string
	PeerSupported                 bool
	PeerBand                      string
	Anomalies                     []report.AnomalyHit
	Insights, Suggestions         []string
	QA                            []string
}

func toDigest(r *report.Report) digest {
	d := digest{
		Level:             levelLabel(r.Meta.Level),
		ScopeName:         scopeLabel(r.Meta.Scope),
		PeriodLabel:       r.Meta.Period.Label,
		BaselineSupported: r.Judgment.Baseline.Supported,
		BaselineMedian:    r.Judgment.Baseline.Median,
		BaselinePct:       r.Judgment.Baseline.Percentile * 100,
		BaselineWindow:    r.Judgment.Baseline.CurrentWindow,
		PeerSupported:     r.Judgment.Peer.Supported,
		PeerBand:          r.Judgment.Peer.Band,
		Anomalies:         r.Judgment.Anomalies,
		Insights:          r.Expression.Insights,
		Suggestions:       r.Expression.Suggestions,
		QA:                r.Meta.QA.Alignment,
	}
	if r.Fact.Personal != nil {
		p := r.Fact.Personal
		d.ReqCount = p.ReqCount
		d.TotalTokens = p.TotalTokens
		d.SuccessRate = pct(p.SuccessCount, p.ReqCount)
		d.CostUSD = fmt2(p.EstCostUSD)
	} else if r.Fact.Department != nil {
		u := r.Fact.Department
		d.ReqCount = u.Member.ReqCount
		d.TotalTokens = u.Member.TotalTokens
		d.SuccessRate = pct(u.Member.SuccessCount, u.Member.ReqCount)
		d.CostUSD = fmt2(u.Member.EstCostUSD)
	}
	return d
}

func fmt2(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// RenderHTML 渲染自包含 HTML。
func RenderHTML(r *report.Report) string {
	// 表达层幻觉校验
	expr := validateExpression(r)
	d := toDigest(r)
	d.Insights = expr.Insights
	d.Suggestions = expr.Suggestions

	var buf bytes.Buffer
	if err := htmlTmpl.Execute(&buf, d); err != nil {
		return "<pre>render error: " + template.HTMLEscapeString(err.Error()) + "</pre>"
	}
	return buf.String()
}

// validateExpression 校验表达层文本：任何数字序列必须落在 {ref} 内，否则视为幻觉并降级。
func validateExpression(r *report.Report) report.Expression {
	expr := r.Expression
	expr.Insights = sanitize(expr.Insights)
	expr.Suggestions = sanitize(expr.Suggestions)
	return expr
}

// sanitize 过滤含"裸数字"的措辞（数字不在 {} 内即幻觉）。空/纯标签文本保留。
func sanitize(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if containsBareDigit(s) {
			continue // 降级：丢弃幻觉措辞
		}
		out = append(out, s)
	}
	return out
}

func containsBareDigit(s string) bool {
	// 数字出现在 {..} 之外 → true
	inRef := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			inRef = true
		case '}':
			inRef = false
		default:
			if s[i] >= '0' && s[i] <= '9' && !inRef {
				return true
			}
		}
	}
	return false
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"pctSign": func(v float64) string { return pctP(v) },
}).Parse(`
<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>用量周报 · {{.ScopeName}}</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:-apple-system,"PingFang SC","Hiragino Sans GB",sans-serif;background:#f5f5f7;color:#1a1a1a;padding:32px}
  .container{max-width:960px;margin:0 auto}
  h1{font-size:1.6rem;margin-bottom:6px}
  .meta{color:#666;font-size:.85rem;margin-bottom:22px;line-height:1.7}
  h2{font-size:1.15rem;margin:26px 0 10px;border-bottom:1px solid #eee;padding-bottom:6px}
  h3{font-size:1rem;margin:16px 0 8px}
  .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:12px;margin:12px 0}
  .card{background:#fff;border-radius:8px;padding:14px 18px;box-shadow:0 1px 3px rgba(0,0,0,.06)}
  .card .l{font-size:.72rem;color:#888;text-transform:uppercase;letter-spacing:.5px}
  .card .v{font-size:1.5rem;font-weight:700;margin-top:2px}
  ul{margin:6px 0 12px 18px} li{font-size:.9rem;margin:4px 0}
  .an{background:#fff;border:1px solid #eee;border-left:4px solid #f59e0b;border-radius:6px;padding:10px 14px;margin:8px 0}
  .an b{color:#b45309}.hint{color:#6b7280;font-size:.82rem}
  .expr{background:#fff;border-radius:8px;padding:12px 16px;box-shadow:0 1px 3px rgba(0,0,0,.06)}
  .footer{margin-top:24px;font-size:.75rem;color:#aaa;text-align:center}
  .sev-P0{border-left-color:#dc2626}.sev-P1{border-left-color:#f59e0b}.sev-P2{border-left-color:#3b82f6}.sev-P3{border-left-color:#6b7280}
  .hint b{color:#6b7280}
</style>
</head>
<body>
<div class="container">
  <h1>用量周报 · {{.ScopeName}}</h1>
  <div class="meta">
    级别：{{.Level}} · 范围：{{.ScopeName}}<br>
    周期：{{.PeriodLabel}} · 货币：CNY（USD→CNY）<br>
    成功＝status=200 · total_tokens＝prompt+completion+reasoning（不含 cached）
  </div>

  <h2>概况</h2>
  <div class="cards">
    <div class="card"><div class="l">调用数</div><div class="v">{{.ReqCount}}</div></div>
    <div class="card"><div class="l">成功率</div><div class="v">{{.SuccessRate}}</div></div>
    <div class="card"><div class="l">总 Token</div><div class="v">{{.TotalTokens}}</div></div>
    <div class="card"><div class="l">成本</div><div class="v">{{.CostUSD}} USD</div></div>
  </div>

  <h2>判断</h2>
  {{if .BaselineSupported}}<h3>28 天基线{{if .BaselineWindow}}（{{.BaselineWindow}}）{{end}}</h3><ul>
    <li>中位数 {{.BaselineMedian}} · 当前处于基线 P{{printf "%.0f" .BaselinePct}}</li>
  </ul>{{end}}

  {{if .PeerSupported}}<h3>同岗分位</h3><ul><li>位于 {{.PeerBand}}</li></ul>{{end}}

  <h3>异常（A1–A5）</h3>
  {{range .Anomalies}}
  {{if .Hit}}
  <div class="an sev-{{.Severity}}">
    <b>[{{.Rule}}] {{.Name}}</b>（{{.Severity}}）：actual={{.ActualValue}} / threshold={{.ThresholdValue}} / baseline={{.BaselineValue}}<br>
    <span class="hint">影响成本 {{.ImpactCostUSD}} USD · 超支 {{.ExcessCostUSD}} USD</span>
  </div>
  {{else}}
  <div class="an"><span class="hint">[{{.Rule}}] {{.Name}}：未命中</span></div>
  {{end}}
  {{end}}

  {{if .Insights}}<h2>解读</h2><div class="expr"><ul>{{range .Insights}}<li>{{.}}</li>{{end}}</ul></div>{{end}}

  <h2>建议</h2>
  <div class="expr"><ul>{{range .Suggestions}}<li>{{.}}</li>{{end}}</ul></div>

  {{if .QA}}<h2>口径与对账</h2><ul>{{range .QA}}<li>{{.}}</li>{{end}}</ul>{{end}}

  <div class="footer">LLM Gateway · 用量周报 · 事实层/判断层/表达层分层输出</div>
</div>
</body>
</html>
`))
