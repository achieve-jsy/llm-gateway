// 针对个人跑 Token 使用分析报告，输出 Markdown。
//
// 用法：
//
//	go run ./cmd/report-gen -user <user_id> -start 2026-09-01 -period week -level personal
//
// 说明：
//   - -db 默认连本地 llm_gateway 开发库（可用 DATABASE_URL 或 -db 覆盖）。
//   - 走 service.NewReportService(db).GetReport(level, scopeID, periodStart, periodType)
//     → report.RenderMD(r)（新 v2.0 链路：数据层→规则→归因→3.6→Assemble→渲染）。
//   - 输出到 reports/personal/<user>_<periodStart>_<periodType>.md（可 -out 覆盖）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/observability/report/v2"
	"github.com/example/llm-gateway/internal/observability/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	var (
		dsn              string
		level            string
		scopeID          string
		periodType       string
		periodFrom       string
		out              string
		htmlOut          bool
		semantic         bool
		semBackend       string
		semBase          string
		semKey           string
		semModel         string
		opencodeBin      string
		opencodeAgent    string
		opencodeDir      string
		opencodeDataHome string
		opencodePure     bool
	)
	defaultSemBackend := os.Getenv("SEMANTIC_BACKEND")
	if defaultSemBackend == "" {
		defaultSemBackend = service.SemanticBackendOpenCode
	}

	defaultSemBase := os.Getenv("SEMANTIC_GATEWAY_URL")
	if defaultSemBase == "" {
		defaultSemBase = os.Getenv("GATEWAY_URL")
	}
	if defaultSemBase == "" {
		defaultSemBase = "http://127.0.0.1:8080"
	}
	defaultSemKey := os.Getenv("SEMANTIC_GATEWAY_KEY")
	if defaultSemKey == "" {
		defaultSemKey = os.Getenv("INTERNAL_TOKEN")
	}
	defaultSemModel := os.Getenv("SEMANTIC_OPENCODE_MODEL")
	if defaultSemModel == "" && defaultSemBackend == service.SemanticBackendOpenCode {
		defaultSemModel = "biangfeng-gateway/bf-glm-5.3-flash"
	}
	if defaultSemModel == "" && defaultSemBackend == service.SemanticBackendHTTP {
		defaultSemModel = os.Getenv("SEMANTIC_GATEWAY_MODEL")
	}
	if defaultSemModel == "" && defaultSemBackend == service.SemanticBackendHTTP {
		defaultSemModel = "bf-glm-5.3-flash"
	}
	defaultSemantic := os.Getenv("SEMANTIC_ENABLED") == "true" || os.Getenv("SEMANTIC_ENABLED") == "1"

	flag.StringVar(&dsn, "db", defaultDSN(), "Postgres DSN (postgres://...)")
	flag.StringVar(&level, "level", "personal", "报告层级: personal|department|studio")
	flag.StringVar(&scopeID, "user", "", "target user_id / dept_id / studio_id")
	flag.StringVar(&periodType, "period", "week", "周期: week|month|day")
	flag.StringVar(&periodFrom, "start", "2026-09-01", "周期起始日 YYYY-MM-DD")
	flag.StringVar(&out, "out", "", "输出 .md 路径（默认 ./reports/<level>/<level>_<id>_<start>_<period>.md）")
	flag.BoolVar(&htmlOut, "html", false, "额外输出可视化 HTML（同目录 .html，浏览器可直接打开）")
	flag.BoolVar(&semantic, "semantic", defaultSemantic, "开启语义增强层")
	flag.StringVar(&semBackend, "sem-backend", defaultSemBackend, "语义后端: opencode|http")
	flag.StringVar(&semBase, "sem-base", defaultSemBase, "语义层网关地址")
	flag.StringVar(&semKey, "sem-key", defaultSemKey, "HTTP 语义层 service key（明文，仅进程内用）")
	flag.StringVar(&semModel, "sem-model", defaultSemModel, "语义判定模型；OpenCode 格式为 provider/model，空值沿用其默认模型")
	flag.StringVar(&opencodeBin, "opencode-bin", envOr("SEMANTIC_OPENCODE_BIN", "opencode"), "OpenCode CLI 路径")
	flag.StringVar(&opencodeAgent, "opencode-agent", envOr("SEMANTIC_OPENCODE_AGENT", "token-usage-judge"), "OpenCode 专用判定 agent")
	flag.StringVar(&opencodeDir, "opencode-dir", envOr("SEMANTIC_OPENCODE_DIR", "."), "OpenCode 工作目录")
	flag.StringVar(&opencodeDataHome, "opencode-data-home", defaultOpenCodeDataHome(), "OpenCode 隔离数据目录")
	flag.BoolVar(&opencodePure, "opencode-pure", envBool("SEMANTIC_OPENCODE_PURE", false), "禁用 OpenCode 外部插件；动态模型发现 provider 需保持 false")
	flag.Parse()

	if scopeID == "" {
		log.Fatal("必须提供 -user <user_id>")
	}

	var semanticGateway *service.SemanticGateway
	if semantic {
		if semBackend != service.SemanticBackendOpenCode && semBackend != service.SemanticBackendHTTP {
			log.Fatalf("-sem-backend 必须为 opencode 或 http，当前为 %q", semBackend)
		}
		if semBackend == service.SemanticBackendHTTP && semKey == "" {
			log.Fatal("-semantic 需要 -sem-key（或环境变量 SEMANTIC_GATEWAY_KEY / INTERNAL_TOKEN）")
		}
		semanticGateway = &service.SemanticGateway{
			Backend:           semBackend,
			BaseURL:           semBase,
			Key:               semKey,
			Model:             semModel,
			Command:           opencodeBin,
			Agent:             opencodeAgent,
			Directory:         opencodeDir,
			DataHome:          opencodeDataHome,
			Pure:              opencodePure,
			MaxCallsPerReport: 20,
			Timeout:           45 * time.Second,
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := service.ValidateSemanticGatewayConfig(checkCtx, semanticGateway); err != nil {
			cancel()
			log.Fatalf("语义后端自检失败: %v", err)
		}
		cancel()
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	svc := service.NewReportService(db)
	if semanticGateway != nil {
		service.SemanticEnabled = true
		svc.WithSemantic(semanticGateway)
		fmt.Printf("✔ 语义增强层已开启：backend=%s model=%s（规则候选→语义复核→程序合成）\n", semBackend, valueOr(semModel, "OpenCode 默认模型"))
	}
	r, err := svc.GetReport(level, scopeID, periodFrom, periodType)
	if err != nil {
		log.Fatalf("GetReport(%s, %s, %s, %s) 失败: %v", level, scopeID, periodFrom, periodType, err)
	}

	if r == nil {
		log.Fatal("GetReport 返回 nil")
	}

	md := report.RenderMD(r)

	levelFolder := "personal"
	levelLabel := "个人"
	switch level {
	case "department":
		levelFolder = "department"
		levelLabel = "部门"
	case "studio":
		levelFolder = "studio"
		levelLabel = "工作室"
	}

	if out == "" {
		out = fmt.Sprintf("reports/%s/%s_%s_%s_%s.md", levelFolder, levelFolder, scopeID, periodFrom, periodType)
	}
	if err := os.MkdirAll(dirOf(out), 0o755); err != nil {
		log.Fatalf("创建输出目录失败: %v", err)
	}
	if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
		log.Fatalf("写文件失败: %v", err)
	}

	htmlPath := ""
	if htmlOut {
		htmlPath = strings.TrimSuffix(out, ".md") + ".html"
		if err := os.WriteFile(htmlPath, []byte(renderHTML(r)), 0o644); err != nil {
			log.Fatalf("写 HTML 失败: %v", err)
		}
	}

	fmt.Printf("✔ 已生成%s报告: %s\n", levelLabel, out)
	if htmlPath != "" {
		fmt.Printf("✔ 已生成可视化 HTML: %s（浏览器打开）\n", htmlPath)
	}
	fmt.Printf("  level=%s scope=%s period=%s(%s) schema_version=%s\n",
		level, scopeID, periodType, periodFrom, r.SchemaVersion)
	fmt.Printf("  结论: %s (成本主桶=%s / 主导问题=%s)\n",
		conclusionLabel(r), costMainBucket(r), dominantIssue(r))
	fmt.Printf("  ---- 预览前 50 行 ----\n")
	fmt.Println(preview(md))
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func defaultOpenCodeDataHome() string {
	if value := strings.TrimSpace(os.Getenv("SEMANTIC_OPENCODE_DATA_HOME")); value != "" {
		return value
	}
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "llm-gateway", "opencode-data")
	}
	return filepath.Join(os.TempDir(), "llm-gateway-opencode-data")
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://localhost:5432/llm_gateway?sslmode=disable"
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

func conclusionLabel(r *report.Report) string {
	if r != nil && r.Conclusion != (report.Conclusion{}) {
		return r.Conclusion.Note
	}
	return ""
}

func costMainBucket(r *report.Report) string {
	if r != nil && r.Conclusion != (report.Conclusion{}) {
		return r.Conclusion.CostMainBucket
	}
	return ""
}

func dominantIssue(r *report.Report) string {
	if r != nil && r.Conclusion != (report.Conclusion{}) {
		return r.Conclusion.DominantIssue
	}
	return ""
}

func preview(s string) string {
	lines := 0
	out := ""
	for i, line := range splitLines(s) {
		if i >= 50 {
			// 末尾提示还有更多行。
			break
		}
		out += line
		lines++
	}
	if lines >= 50 {
		out += "\n..." // 截断提示
	}
	return out
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
