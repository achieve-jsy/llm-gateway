package main

// config.go —— CLI 参数与配置解析（stdlib flag + 环境变量缺省）。
// 阶段一离线工具：独立入口，不直连 DB、不改 gateway/control-plane 运行时。

import (
	"flag"
	"os"
	"time"
)

// Config 运行配置。
type Config struct {
	BaseURL     string
	Token       string
	Level       string // personal | department | studio
	Scope       string // personal=user_id / department=dept_id / studio=studio_id
	From        string
	To          string
	Period      string // week | month | custom
	KeyType     string
	JobCategory string
	USDCNYRate  float64
	MaxLogs     int
	JSONOut     string
	MDOut       string
	HTMLOut     string
	VickyOut    string
	VickySubmit bool // 是否生成可 POST /api/reports 的 JSON 载荷
	VickyProject string
	VickyCategory string
	VickyNarrative string
	Debug       bool
	Agg         bool // 用成员聚合(/usage/report/members)构造事实层加法核心，不拉全量明细；A1–A5/模型预算/服务Key/Top模型等明细依赖项降级(NotComputed/空+注记)
	All         bool // 批量编排：枚举 level 全体对象，各出报告 + 汇总索引（阶段4）
	OutDir      string // 批量输出目录（每对象报告 + 汇总索引）；默认当前目录
	SummaryMD   string // 批量汇总索引 MD 路径；默认 <outdir>/summary_<from>_<to>.md
}

func parseConfig(args []string) Config {
	var c Config
	fs := flag.NewFlagSet("usage-report", flag.ExitOnError)
	fs.StringVar(&c.BaseURL, "base", envOr("USAGE_REPORT_BASE", "http://127.0.0.1:8081/api/v1"), "控制面 /api/v1 base URL")
	fs.StringVar(&c.Token, "token", os.Getenv("USAGE_REPORT_TOKEN"), "admin JWT(PermUsageViewAll/PermMemberView)")
	fs.StringVar(&c.Level, "level", "personal", "report level: personal|department|studio")
	fs.StringVar(&c.Scope, "scope", "", "scope id (user_id|dept_id|studio_id)")
	fs.StringVar(&c.From, "from", "", "start date YYYY-MM-DD")
	fs.StringVar(&c.To, "to", "", "end date YYYY-MM-DD")
	fs.StringVar(&c.Period, "period", "week", "week|month|custom")
	fs.StringVar(&c.KeyType, "key-type", "", "filter key_type (personal|service|'')")
	fs.StringVar(&c.JobCategory, "job-category", "", "同岗分位用 job_category；空则自适应回退")
	fs.Float64Var(&c.USDCNYRate, "fx-rate", 7.2, "USD→CNY 汇率")
	fs.IntVar(&c.MaxLogs, "max-logs", 20000, "日志拉取上限（条）")
	fs.StringVar(&c.JSONOut, "json-out", "", "输出 report JSON 路径")
	fs.StringVar(&c.MDOut, "md-out", "", "输出 MD 路径")
	fs.StringVar(&c.HTMLOut, "html-out", "", "输出 HTML 路径")
	fs.StringVar(&c.VickyOut, "vicky-out", "", "输出 Vicky 规范 HTML content 路径")
	fs.BoolVar(&c.VickySubmit, "vicky-submit", false, "同时生成可 POST /api/reports 的 JSON 载荷")
	fs.StringVar(&c.VickyProject, "vicky-project", "gateway", "Vicky project slug")
	fs.StringVar(&c.VickyCategory, "vicky-category", "business-report", "Vicky category")
	fs.StringVar(&c.VickyNarrative, "vicky-narrative", "金字塔/结论先行", "Vicky narrative")
	fs.BoolVar(&c.Debug, "debug", false, "打印调试")
	fs.BoolVar(&c.Agg, "agg", false, "用成员聚合(/usage/report/members)构造事实层加法核心，不拉全量明细")
	fs.BoolVar(&c.All, "all", false, "批量编排：枚举 level 全体对象，各出报告 + 汇总索引(建议配 -agg)")
	fs.StringVar(&c.OutDir, "outdir", "", "批量输出目录(每对象报告+汇总索引)；默认当前目录")
	fs.StringVar(&c.SummaryMD, "summary-md", "", "批量汇总索引 MD 路径；默认 <outdir>/summary_<from>_<to>.md")
	_ = fs.Parse(args)
	if c.From == "" || c.To == "" {
		from, to := periodFor(c.Period)
		if c.From == "" {
			c.From = from
		}
		if c.To == "" {
			c.To = to
		}
	}
	if c.USDCNYRate == 0 {
		c.USDCNYRate = 7.2
	}
	return c
}

func periodFor(p string) (string, string) {
	now := time.Now()
	end := now.Format("2006-01-02")
	switch p {
	case "week":
		return now.AddDate(0, 0, -6).Format("2006-01-02"), end
	case "month":
		return now.AddDate(0, -1, 0).Format("2006-01-02"), end
	default:
		return now.AddDate(0, 0, -6).Format("2006-01-02"), end
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
