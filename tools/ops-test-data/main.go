package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/datatypes"

	"github.com/example/llm-gateway/internal/observability/model"
	cpModel "github.com/example/llm-gateway/internal/controlplane/model"
)

// 默认 provider/model/group 组合（数据库没有真实数据时兜底使用）
var defaultCombos = []struct {
	Provider  string
	Model     string
	GroupName string
}{
	{"azure-openai", "gpt-4", "gpt4-group"},
	{"azure-openai", "gpt-4-turbo", "gpt4-group"},
	{"azure-openai", "gpt-3.5-turbo", "gpt35-group"},
	{"anthropic", "claude-3-5-sonnet-20241022", "claude-sonnet-group"},
	{"anthropic", "claude-3-opus-20240229", "claude-opus-group"},
	{"anthropic", "claude-3-haiku-20240307", "claude-haiku-group"},
	{"google-vertex", "gemini-2.0-flash-exp", "gemini-group"},
	{"google-vertex", "gemini-1.5-pro", "gemini-group"},
	{"openrouter", "openai/gpt-4o", "gpt4o-group"},
	{"openai-official", "gpt-4o-mini", "gpt4o-mini-group"},
}

// 用户池（模拟真实团队）
var userPool = []struct {
	UserID   string
	UserName string
	DeptID   string
	DeptName string
	StudioID string
	StudioName string
}{
	{"u-001", "张三", "d-sales", "销售部", "s-01", "销售工作室一"},
	{"u-002", "李四", "d-sales", "销售部", "s-01", "销售工作室一"},
	{"u-003", "王五", "d-dev", "研发部", "s-02", "研发工作室"},
	{"u-004", "赵六", "d-dev", "研发部", "s-02", "研发工作室"},
	{"u-005", "钱七", "d-ops", "运维部", "s-03", "运维工作室"},
	{"u-006", "孙八", "d-data", "数据部", "s-04", "数据工作室"},
	{"u-007", "周九", "d-pm", "产品部", "s-05", "产品工作室"},
	{"u-008", "吴十", "d-sales", "销售部", "s-01", "销售工作室二"},
	{"u-009", "郑一一", "d-dev", "研发部", "s-02", "研发工作室"},
	{"u-010", "冯一二", "d-dev", "研发部", "s-02", "研发工作室"},
}

// 状态码权重（模拟真实分布）
var statusWeights = map[int]float64{
	200: 92.0,  // 成功
	201: 1.0,
	400: 1.2,  // 客户端错误
	401: 0.8,  // 鉴权失败
	403: 0.5,  // 权限不足
	429: 1.5,  // 限流
	500: 1.0,  // 上游内部错误
	502: 0.8,  // 网关错误
	503: 0.5,  // 上游不可用
	504: 0.7,  // 上游超时
}

// 系统日志组件池
var sysLogComponents = []string{
	"gateway", "scheduler", "auth", "provider", "control-plane",
	"router", "middleware", "orchestrator",
}

var sysLogLevels = []string{"INFO", "WARN", "ERROR", "DEBUG"}

func main() {
	cleanFlag := flag.Bool("clean", false, "清理之前生成的种子数据")
	hoursFlag := flag.Int("hours", 24, "生成数据的时间跨度（小时）")
	countFlag := flag.Int("count", 3000, "生成的 call_logs 记录数")
	dsn := flag.String("dsn", "", "数据库 DSN（默认读取 DATABASE_URL 环境变量）")
	flag.Parse()

	// 解析数据库连接
	dsnStr := resolveDSN(*dsn)
	if dsnStr == "" {
		log.Fatal("请设置 DATABASE_URL 环境变量或传入 --dsn 参数")
	}

	db, err := gorm.Open(postgres.Open(dsnStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	if *cleanFlag {
		clean(db)
		return
	}

	generate(db, *hoursFlag, *countFlag)
}

func resolveDSN(flag string) string {
	if env := os.Getenv("DATABASE_URL"); env != "" {
		return env
	}
	return flag
}

func clean(db *gorm.DB) {
	log.Println("开始清理所有种子数据...")

	tables := []struct {
		Name string
		SQL  string
	}{
		{"call_logs", "DELETE FROM call_logs WHERE key_name LIKE '__seed_%'"},
		{"system_logs", "DELETE FROM system_logs WHERE extra::text LIKE '%__seed_%'"},
		{"alert_events", "DELETE FROM alert_events WHERE dimensions::text LIKE '%__seed_%'"},
		{"alert_rules", "DELETE FROM alert_rules WHERE name LIKE 'SLA 成功率下降' OR name LIKE 'CPU 使用率过高' OR name LIKE '上游错误激增' OR name LIKE '限流比例过高' OR name LIKE '内存使用率过高'"},
	}

	for _, t := range tables {
		r := db.Exec(t.SQL)
		if r.Error != nil {
			log.Printf("清理 %s 失败: %v", t.Name, r.Error)
		} else {
			log.Printf("已清理 %s: %d 条", t.Name, r.RowsAffected)
		}
	}

	log.Println("清理完成")
}

func generate(db *gorm.DB, hours, count int) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	seedKeyPrefix := fmt.Sprintf("__seed_%d", rng.Intn(100000))

	combos := loadRealCombos(db)
	if len(combos) == 0 {
		log.Println("数据库无真实 provider/model，使用默认组合")
		combos = make([]realCombo, 0, len(defaultCombos))
		for _, d := range defaultCombos {
			combos = append(combos, realCombo{
				Provider:  d.Provider,
				Model:     d.Model,
				GroupName: d.GroupName,
			})
		}
	}

	// 生成时间分布（模拟真实流量模式）
	now := time.Now()
	startTime := now.Add(-time.Duration(hours) * time.Hour)
	timestamps := generateTimestamps(rng, count, startTime, now)

	// 按时间排序后批量生成
	type logEntry struct {
		ts       time.Time
		combo    realCombo
		user     struct{ UserID, UserName, DeptID, DeptName, StudioID, StudioName string }
		status   int
	}
	entries := make([]logEntry, 0, count)

	for _, ts := range timestamps {
		combo := combos[rng.Intn(len(combos))]
		user := userPool[rng.Intn(len(userPool))]
		status := chooseStatus(rng)
		entries = append(entries, logEntry{ts, combo, user, status})
	}

	// 构造 call_logs 记录
	logs := make([]model.CallLog, 0, count)
	for i, e := range entries {
		keyName := fmt.Sprintf("%s-%d", seedKeyPrefix, i)

		isErr := e.status >= 400
		promptTokens := 0
		completionTokens := 0
		durationMs := 0
		var firstToken *int

		if !isErr {
			promptTokens = 500 + rng.Intn(4500)
			completionTokens = 200 + rng.Intn(2800)
			durationMs = lognormal(rng, 7.0, 0.8) // 峰值 ~1100ms
			if durationMs > 20000 {
				durationMs = 20000
			}
			if durationMs < 50 {
				durationMs = 50 + rng.Intn(200)
			}
			ttft := 50 + rng.Intn(int(float64(durationMs) * 0.4))
			firstToken = &ttft
		} else {
			// 错误请求：token 少, 延迟短（客户端错误）或长（超时）
			promptTokens = rng.Intn(500)
			completionTokens = 0
			if e.status == 504 {
				durationMs = 30000 + rng.Intn(30000) // 上游超时 30-60s
			} else if e.status >= 500 {
				durationMs = 2000 + rng.Intn(8000)
			} else {
				durationMs = 10 + rng.Intn(500)
			}
		}

		stream := rng.Float64() < 0.75 // 75% 流式
		totalTokens := promptTokens + completionTokens

		logs = append(logs, model.CallLog{
			RequestID:        fmt.Sprintf("%s-req-%d", seedKeyPrefix, i),
			TS:               e.ts,
			KeyID:            fmt.Sprintf("key-%s", e.user.UserID),
			KeyName:          keyName,
			UserID:           e.user.UserID,
			UserName:         e.user.UserName,
			DeptID:           e.user.DeptID,
			DeptName:         e.user.DeptName,
			StudioID:         e.user.StudioID,
			StudioName:       e.user.StudioName,
			Model:            e.combo.Model,
			RoutedModel:      e.combo.Model,
			GroupName:        e.combo.GroupName,
			Provider:         e.combo.Provider,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			Status:           e.status,
			UpstreamStatus:   e.status,
			DurationMs:       durationMs,
			FirstTokenMs:     firstToken,
			Stream:           stream,
			FailoverUsed:     false,
			RoutedViaGroup:   true,
		})
	}

	// 批量插入（每次 200 条）
	batchSize := 200
	inserted := 0
	for i := 0; i < len(logs); i += batchSize {
		end := i + batchSize
		if end > len(logs) {
			end = len(logs)
		}
		batch := logs[i:end]
		if err := db.CreateInBatches(batch, batchSize).Error; err != nil {
			log.Fatalf("批量插入 call_logs 失败: %v", err)
		}
		inserted += len(batch)
		if inserted%1000 == 0 || inserted == len(logs) {
			log.Printf("已插入 %d / %d 条 call_logs", inserted, len(logs))
		}
	}

	// 生成 system_logs
	generateSystemLogs(db, rng, seedKeyPrefix, hours)

	// 生成告警规则与事件
	generateAlerts(db, rng, seedKeyPrefix, hours)

	log.Printf("生成完成: %d 条 call_logs, key_name 前缀 %s", inserted, seedKeyPrefix)
	log.Printf("可通过 `go run main.go --clean` 或直接 DELETE WHERE key_name LIKE '%s%%' 清理", seedKeyPrefix)
}

type realCombo struct {
	Provider  string
	Model     string
	GroupName string
}

func loadRealCombos(db *gorm.DB) []realCombo {
	var rows []struct {
		Provider string `gorm:"column:provider"`
		Model    string `gorm:"column:model"`
		GroupName string `gorm:"column:group_name"`
	}
	err := db.Raw(`
		SELECT provider, model, group_name, COUNT(*) as cnt
		FROM call_logs
		WHERE provider != '' AND model != '' AND key_name NOT LIKE '__seed_%'
		GROUP BY provider, model, group_name
		ORDER BY cnt DESC
		LIMIT 20
	`).Scan(&rows).Error
	if err != nil {
		log.Printf("查询真实 provider/model 失败: %v", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	out := make([]realCombo, 0, len(rows))
	for _, r := range rows {
		out = append(out, realCombo{r.Provider, r.Model, r.GroupName})
	}
	return out
}

// 生成模拟真实业务节奏的时间戳：工作时间多，夜晚少
func generateTimestamps(rng *rand.Rand, n int, start, end time.Time) []time.Time {
	hours := end.Sub(start).Hours()
	ts := make([]time.Time, 0, n)

	for i := 0; i < n; i++ {
		var t time.Time
		for {
			offsetSec := rng.Float64() * hours * 3600
			candidate := start.Add(time.Duration(offsetSec) * time.Second)
			hour := candidate.Hour()
			weekday := candidate.Weekday()

			// 权重：工作时间 x3，夜晚 x0.3，周末 x0.5
			weight := 1.0
			if hour >= 9 && hour <= 18 {
				weight = 3.0
			} else if hour >= 0 && hour <= 7 {
				weight = 0.3
			}
			if weekday == time.Saturday || weekday == time.Sunday {
				weight *= 0.5
			}

			if rng.Float64() < weight/3.0 {
				t = candidate
				break
			}
		}
		ts = append(ts, t)
	}

	// 按时间排序
	for i := 0; i < len(ts); i++ {
		for j := i + 1; j < len(ts); j++ {
			if ts[j].Before(ts[i]) {
				ts[i], ts[j] = ts[j], ts[i]
			}
		}
	}
	return ts
}

// 按权重随机选状态码
func chooseStatus(rng *rand.Rand) int {
	total := 0.0
	for _, w := range statusWeights {
		total += w
	}
	r := rng.Float64() * total
	cum := 0.0
	for code, w := range statusWeights {
		cum += w
		if r < cum {
			return code
		}
	}
	return 200
}

// 对数正态分布
func lognormal(rng *rand.Rand, mu, sigma float64) int {
	z := rng.NormFloat64()
	v := math.Exp(mu + sigma*z)
	return int(v)
}

// 生成 system_logs（基于 call_logs 错误事件 + 系统级事件）
func generateSystemLogs(db *gorm.DB, rng *rand.Rand, seedKeyPrefix string, hours int) {
	count := 300 // 系统日志量
	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
	logs := make([]cpModel.SystemLog, 0, count)

	msgTemplates := map[string][]string{
		"INFO": {
			"请求处理完成",
			"provider 健康检查通过",
			"调度成功: provider=%s model=%s",
			"auth 验证通过 user=%s",
			"批量写入完成: %d 条",
			"配置热更新生效",
		},
		"WARN": {
			"provider 响应慢: %dms",
			"限流触发: rate_limit_reset=%d",
			"连接池接近上限: %d/%d",
			"重试 %s: 第 %d 次",
			"fallback 切换到备用 provider",
		},
		"ERROR": {
			"上游请求失败: %s",
			"timeout after 30s: provider=%s",
			"认证失败: invalid key",
			"路由无可用模型: group=%s",
			"连接断开: connection reset by peer",
		},
	}

	for i := 0; i < count; i++ {
		offsetSec := rng.Float64() * float64(hours) * 3600
		ts := startTime.Add(time.Duration(offsetSec) * time.Second)
		level := sysLogLevels[rng.Intn(len(sysLogLevels))]
		component := sysLogComponents[rng.Intn(len(sysLogComponents))]

		msgs := msgTemplates[level]
		msg := msgs[rng.Intn(len(msgs))]

		user := userPool[rng.Intn(len(userPool))]
		combo := defaultCombos[rng.Intn(len(defaultCombos))]

		reqID := uuid.New().String()[:16]
		extra := datatypes.JSON(fmt.Sprintf(`{"seed":"%s"}`, seedKeyPrefix))

		status := 0
		switch level {
		case "WARN":
			status = 429
		case "ERROR":
			status = 500
		}

		durationMs := 50 + rng.Intn(3000)

		logs = append(logs, cpModel.SystemLog{
			Ts:              ts,
			Level:           level,
			Component:       component,
			Message:         msg,
			RequestID:       &reqID,
			UserID:          &user.UserID,
			Provider:        &combo.Provider,
			Model:           &combo.Model,
			Group:           &combo.GroupName,
			DurationMs:      &durationMs,
			Status:          &status,
			Extra:           extra,
		})
	}

	if err := db.CreateInBatches(logs, 100).Error; err != nil {
		log.Printf("生成 system_logs 失败: %v", err)
	} else {
		log.Printf("已插入 %d 条 system_logs", len(logs))
	}
}

// 生成告警规则与事件（让告警相关面板有内容）
func generateAlerts(db *gorm.DB, rng *rand.Rand, seedKeyPrefix string, hours int) {
	rules := []cpModel.AlertRule{
		{Name: "SLA 成功率下降", Description: "SLA 成功率低于 99%", Enabled: true,
			MetricType: "success_rate", Operator: "<", Threshold: 99, Severity: "P1",
			WindowMinutes: 15, SustainedMinutes: 5, CooldownMinutes: 30, NotifyEmail: true},
		{Name: "CPU 使用率过高", Description: "CPU > 85% 持续 5 分钟", Enabled: true,
			MetricType: "cpu_usage_percent", Operator: ">", Threshold: 85, Severity: "P2",
			WindowMinutes: 5, SustainedMinutes: 5, CooldownMinutes: 15, NotifyEmail: false},
		{Name: "上游错误激增", Description: "上游错误数 > 50/5min", Enabled: true,
			MetricType: "upstream_error_rate", Operator: ">", Threshold: 10, Severity: "P1",
			WindowMinutes: 5, SustainedMinutes: 2, CooldownMinutes: 20, NotifyEmail: true},
		{Name: "限流比例过高", Description: "限流账户比例 > 30%", Enabled: true,
			MetricType: "account_rate_limited_count", Operator: ">", Threshold: 5, Severity: "P2",
			WindowMinutes: 10, SustainedMinutes: 3, CooldownMinutes: 15, NotifyEmail: false},
		{Name: "内存使用率过高", Description: "内存 > 90%", Enabled: true,
			MetricType: "memory_usage_percent", Operator: ">", Threshold: 90, Severity: "P3",
			WindowMinutes: 5, SustainedMinutes: 3, CooldownMinutes: 30, NotifyEmail: false},
	}

	if err := db.Create(&rules).Error; err != nil {
		log.Printf("插入告警规则失败: %v", err)
		return
	}
	log.Printf("已插入 %d 条告警规则", len(rules))

	// 生成告警事件
	events := make([]cpModel.AlertEvent, 0, 80)
	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
	statuses := []string{"firing", "resolved", "manual_resolved"}

	for i := 0; i < 80; i++ {
		rule := rules[rng.Intn(len(rules))]
		offsetSec := rng.Float64() * float64(hours) * 3600
		firedAt := startTime.Add(time.Duration(offsetSec) * time.Second)
		status := statuses[rng.Intn(len(statuses))]

		var resolvedAt *time.Time
		if status != "firing" {
			rt := firedAt.Add(time.Duration(5+rng.Intn(60)) * time.Minute)
			resolvedAt = &rt
		}

		metricValue := rule.Threshold * (0.8 + rng.Float64()*0.5)
		dimensions := datatypes.JSON(fmt.Sprintf(`{"provider":"%s","seed":"%s"}`,
			defaultCombos[rng.Intn(len(defaultCombos))].Provider, seedKeyPrefix))

		events = append(events, cpModel.AlertEvent{
			RuleID:         rule.ID,
			Severity:       rule.Severity,
			Status:         status,
			Title:          fmt.Sprintf("%s 触发告警", rule.Name),
			Description:    fmt.Sprintf("%s = %.2f (阈值 %.2f)", rule.MetricType, metricValue, rule.Threshold),
			MetricValue:    metricValue,
			ThresholdValue: rule.Threshold,
			Dimensions:     dimensions,
			FiredAt:        firedAt,
			ResolvedAt:     resolvedAt,
			EmailSent:      rule.NotifyEmail,
		})
	}

	if err := db.CreateInBatches(events, 40).Error; err != nil {
		log.Printf("插入告警事件失败: %v", err)
	} else {
		log.Printf("已插入 %d 条告警事件", len(events))
	}
}
