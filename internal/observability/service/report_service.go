package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/report"
)

// 本文件是 report 服务层的 **公开 DB 入口**：ReportService.GetReport(level, scopeID, periodStart, periodType)。
//
// 分层口径：
//   - 原始数据从 call_logs / offhours_filing（+基础档位 baseline_quantiles）读取，复用 rollup_service 的
//     过滤约定（ExplainSeedanceLogFilter / 排除 Service Key），不另造 SQL。
//   - 真正的管线（会话装配/五类判定/归因清算/3.6/Assemble）在 buildReport（report_orchestrator.go）里，
//     由本入口把 DB 数据适配成 ReportDataProvider 后驱动。e2e 用内存 provider 直接测 buildReport。
//   - report_snapshot 缓存读写**不归本服务**：api-eng 的 t9 负责持久化；本服务只产出 *Report。
//
// 注意：基础档位 baseline 采用「期内现算」降级（report_baseline_inline.go 从 call_logs 现算群体分位），
// 不依赖 baseline_quantiles 物化表；物化表存在时由外层决定是否优先读表。
// 组织级 by_model_tier 明细由 DB 聚合层提供，本入口只保证成本三分法自洽。

// ReportService 是 report 服务（持有 DB，供 t9 / control-plane 调用）。
type ReportService struct {
	db       *gorm.DB
	semantic *SemanticGateway
}

// NewReportService 创建 ReportService。默认根据环境变量/服务密钥配置接入语义增强网关。
func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{
		db:       db,
		semantic: DefaultSemanticGateway(),
	}
}

// WithSemantic 注入语义增强层网关（nil/不调=关闭，报告行为与 P1 一致）。
func (s *ReportService) WithSemantic(gw *SemanticGateway) *ReportService {
	s.semantic = gw
	return s
}

// GetReport 生成一份已 Validate 的 *Report（t9 / api-eng 直接调用本方法）。
//
// sign：GetReport(level, scopeID, periodStart, periodType)。
//   - level：personal | department | studio。
//   - scopeID：personal→user_id；department→dept_id；studio→studio_id。
//   - periodStart：起始日 "2006-01-02"（Asia/Shanghai）。
//   - periodType：week | month | day（决定周期时长与上一周期推导）。
func (s *ReportService) GetReport(level, scopeID, periodStart, periodType string) (*report.Report, error) {
	period, _ := resolveReportPeriod(periodStart, periodType)
	provider := &dbReportProvider{db: s.db, from: period.From, to: period.To}
	resolvedScope := report.Scope{ID: scopeID, Name: scopeID}
	if level == report.LevelDepartment && scopeID != "" && s.db != nil {
		var deptName string
		if err := s.db.Table("departments").Where("dept_id ILIKE ? OR id::text ILIKE ? OR dept_name ILIKE ?", scopeID, scopeID, scopeID).Pluck("dept_name", &deptName).Error; err == nil && deptName != "" {
			resolvedScope.Name = deptName
		}
		var realDeptID string
		if err := s.db.Table("departments").Where("dept_id ILIKE ? OR id::text ILIKE ? OR dept_name ILIKE ?", scopeID, scopeID, scopeID).Pluck("dept_id", &realDeptID).Error; err == nil && realDeptID != "" {
			resolvedScope.ID = realDeptID
		}
	} else if level == report.LevelStudio && scopeID != "" && s.db != nil {
		var studioName string
		if err := s.db.Table("studios").Where("id::text = ? OR name ILIKE ?", scopeID, scopeID).Pluck("name", &studioName).Error; err == nil && studioName != "" {
			resolvedScope.Name = studioName
		}
		var realStudioID string
		if err := s.db.Table("studios").Where("name ILIKE ?", scopeID).Pluck("id", &realStudioID).Error; err == nil && realStudioID != "" {
			resolvedScope.ID = realStudioID
		}
	}
	params := ReportParams{
		Level: level,
		Scope: resolvedScope,
		Period: report.Period{
			From: period.From, To: period.To, IsoWeek: periodLabel(period.From),
		},
		Currency:    report.Currency{Report: "USD", USDCNYRate: 1.0},
		RuleVersion: "v0.6",
		DataSource:  provider,
		Semantic:    s.semantic,
	}
	ctx := context.Background()
	return GetReport(ctx, params)
}

// resolveReportPeriod 由起始日 + 周期类型推导当前周期与上一周期（闭区间日期）。
func resolveReportPeriod(periodStart, periodType string) (report.Period, report.Period) {
	from, err := time.Parse("2006-01-02", periodStart)
	if err != nil {
		// 非法日期退回当天。
		from = time.Now()
	}
	var to time.Time
	switch periodType {
	case "month":
		to = time.Date(from.Year(), from.Month(), daysInMonth(from.Year(), from.Month()), 0, 0, 0, 0, time.UTC)
	case "day":
		to = from
	default: // week
		to = from.AddDate(0, 0, 6)
	}
	cur := report.Period{From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
	span := to.Sub(from)
	prevTo := from.Add(-time.Nanosecond)
	prevFrom := prevTo.Add(-span)
	prev := report.Period{From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02")}
	// 无 prev（最早周期）时让 provider 返回空。
	return cur, prev
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func periodLabel(from string) string {
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		return from
	}
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// dbReportProvider 是 ReportDataProvider 的 DB 实现（从 call_logs / offhours_filing 读取）。原始数据。
type dbReportProvider struct {
	db       *gorm.DB
	from, to string
}

// toExclusiveEnd 把闭区间结束日（含当日）映射为半开查询上界（次日 00:00），
// 与 report_provider.go 的 periodRange 口径一致：调用方传闭区间 [from, to]，DB 层查 [from, to+1d)。
func toExclusiveEnd(to string) string {
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return to
	}
	return end.AddDate(0, 0, 1).Format("2006-01-02")
}

func (p *dbReportProvider) Users(ctx context.Context, level string, scope report.Scope) ([]ReportUser, error) {
	col := "user_id"
	switch level {
	case report.LevelDepartment:
		col = "dept_id"
	case report.LevelStudio:
		col = "studio_id"
	}
	q := p.db.WithContext(ctx).Table("call_logs").
		Select("DISTINCT user_id, user_name, dept_id, dept_name, studio_id").
		Where(obsmodel.WhereExcludeServiceKeyLogs).
		Where("ts >= ? AND ts < ?", p.from, toExclusiveEnd(p.to)).
		Where("user_id != '' AND user_id IS NOT NULL AND user_name != '' AND LOWER(user_id) != 'null' AND LOWER(user_id) != 'unknown'")
	if scope.ID != "" {
		sid := strings.TrimSpace(scope.ID)
		q = q.Where(col+" = ? OR LOWER("+col+") = LOWER(?)", sid, sid)
	}
	type userRow struct {
		UserID, UserName, DeptID, DeptName, StudioID string
	}
	var rows []userRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("report provider: users: %w", err)
	}
	out := make([]ReportUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReportUser{UserID: r.UserID, Name: r.UserName, DeptID: r.DeptID, DeptName: r.DeptName})
	}
	return out, nil
}

// CallRows 返回某人当期调用行（供会话装配/成本聚合）。
func (p *dbReportProvider) CallRows(ctx context.Context, userID, from, to string) ([]CallLogRow, error) {
	var logs []obsmodel.CallLog
	q := p.db.WithContext(ctx).
		Select("request_id, user_id, key_id, ts, model, routed_model, prompt_tokens, cached_tokens, cache_write_tokens, completion_tokens, reasoning_tokens, status, input_cost_usd, cache_read_cost_usd, output_cost_usd, est_cost_usd, user_agent, ip_address").
		Where("user_id = ? AND ts >= ? AND ts < ?", userID, from, toExclusiveEnd(to)).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: call_rows: %w", err)
	}
	out := make([]CallLogRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, CallLogRow{
			RequestID: l.RequestID, UserID: l.UserID, KeyID: l.KeyID, TS: l.TS, Model: l.Model,
			PromptTokens: int64(l.PromptTokens), CachedTokens: int64(l.CachedTokens), CacheWriteTokens: int64(l.CacheWriteTokens),
			CompletionTokens: int64(l.CompletionTokens), ReasoningTokens: int64(l.ReasoningTokens), Status: l.Status,
			InputCostUSD: l.InputCostUSD, CacheReadCostUSD: l.CacheReadCostUSD, OutputCostUSD: l.OutputCostUSD, EstCostUSD: l.EstCostUSD,
			UserAgent: l.UserAgent, IPAddress: l.IPAddress,
		})
	}
	return out, nil
}

// OutputRunawayRows 返回某人逐请求输出失控判定行。
func (p *dbReportProvider) OutputRunawayRows(ctx context.Context, userID, from, to string) ([]OutputRunawayRow, error) {
	var logs []obsmodel.CallLog
	q := p.db.WithContext(ctx).
		Select("request_id, user_id, model, prompt_tokens, cached_tokens, completion_tokens, output_cost_usd").
		Where("user_id = ? AND ts >= ? AND ts < ?", userID, from, toExclusiveEnd(to)).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: output rows: %w", err)
	}
	out := make([]OutputRunawayRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, OutputRunawayRow{
			RequestID: l.RequestID, UserID: l.UserID, SessionID: l.RequestID, Model: l.Model,
			PromptTokens: int64(l.PromptTokens), CachedTokens: int64(l.CachedTokens), CompletionTokens: int64(l.CompletionTokens),
			OutputCostUSD: l.OutputCostUSD,
		})
	}
	return out, nil
}

// OverModelRows 返回某人逐请求选型判定行（turns==1 视为单轮会话）。
func (p *dbReportProvider) OverModelRows(ctx context.Context, userID, from, to string) ([]OverModelRow, error) {
	var logs []obsmodel.CallLog
	q := p.db.WithContext(ctx).
		Select("request_id, user_id, model, prompt_tokens, cached_tokens, completion_tokens, est_cost_usd").
		Where("user_id = ? AND ts >= ? AND ts < ?", userID, from, toExclusiveEnd(to)).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: over_model rows: %w", err)
	}
	out := make([]OverModelRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, OverModelRow{
			RequestID: l.RequestID, UserID: l.UserID, SessionID: l.RequestID, Model: l.Model,
			SessionTurns: 1, PromptTokens: int64(l.PromptTokens), CachedTokens: int64(l.CachedTokens), CompletionTokens: int64(l.CompletionTokens),
			ActualCostUSD: l.EstCostUSD,
		})
	}
	return out, nil
}

// OffhoursRows 返回某人 3.6 判定的原始行（正常+非工作时段）。
func (p *dbReportProvider) OffhoursRows(ctx context.Context, userID, from, to string) ([]OffhoursCallRow, error) {
	var logs []obsmodel.CallLog
	q := p.db.WithContext(ctx).
		Select("request_id, user_id, user_name, dept_id, ts, model, key_id, key_name, est_cost_usd").
		Where("user_id = ? AND ts >= ? AND ts < ?", userID, from, toExclusiveEnd(to)).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: offhours rows: %w", err)
	}
	out := make([]OffhoursCallRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, OffhoursCallRow{
			RequestID: l.RequestID, UserID: l.UserID, UserName: l.UserName, DeptID: l.DeptID, TS: l.TS,
			Model: l.Model, KeyID: l.KeyID, KeyName: l.KeyName, CostUSD: l.EstCostUSD,
		})
	}
	return out, nil
}

// OffhoursFilings 返回已备案窗口（排除优先）。
func (p *dbReportProvider) OffhoursFilings(ctx context.Context) ([]OffhoursFilingWindow, error) {
	var filings []cpmodel.OffhoursFiling
	if err := p.db.WithContext(ctx).Find(&filings).Error; err != nil {
		// 缺表（offhours_filing 迁移未跑）时降级为「无备案窗口」：不中断报告，3.6 不做备案排除。
		if isTableNotExistError(err) {
			return []OffhoursFilingWindow{}, nil
		}
		return nil, fmt.Errorf("report provider: offhours filings: %w", err)
	}
	out := make([]OffhoursFilingWindow, 0, len(filings))
	for _, f := range filings {
		out = append(out, OffhoursFilingWindow{
			SubjectType: f.SubjectType, SubjectID: f.SubjectID,
			StartAt: f.StartAt, EndAt: f.EndAt, FilingType: f.FilingType,
		})
	}
	return out, nil
}

// DataQuality 覆盖率：best-effort。真实会话/场景/价格/岗位标签覆盖率需统计层聚合；
// 未接入时给保守默认，保证报告可解释（覆盖率低会触发「疑似」降级说明）。
func (p *dbReportProvider) DataQuality(ctx context.Context, level string, scope report.Scope) (report.DataQuality, error) {
	// 缺省：真实会话/标签覆盖率为占位值，字段覆盖不足标 0（触发数据质量说明）。
	return report.DataQuality{
		RealSessionCoverage:   1.0,
		SceneLabelCoverage:    0.0,
		PriceCatalogCoverage:  0.0,
		PositionLabelCoverage: 0.0,
	}, nil
}

// 基线：优先从 baseline_quantiles 物化表读取，表缺失或数据不全时优雅回退当期现算。
func (p *dbReportProvider) OutputRunawayBaselines(ctx context.Context, positions, models []string) (OutputRunawayBaselines, error) {
	return loadOutputRunawayBaselines(ctx, p.db, p.from, positions, models)
}

func (p *dbReportProvider) MultiTaskBaselines(ctx context.Context, userIDs []string) (MultiTaskBaselines, error) {
	return loadMultiTaskBaselines(ctx, p.db, p.from, userIDs)
}

func (p *dbReportProvider) OverModelBaselines(ctx context.Context, positions []string) (OverModelBaselines, error) {
	return loadOverModelBaselines(ctx, p.db, p.from, positions)
}

// Resolver 返回杀鸡用牛刀档位解析器（复用 ModelTierLookupService）。
func (p *dbReportProvider) Resolver(ctx context.Context) (OverModelResolver, error) {
	return NewModelTierLookupService(p.db), nil
}

// ModelTier 返回某模型归一档位。
func (p *dbReportProvider) ModelTier(ctx context.Context, model string) (ModelTierInfo, error) {
	return NewModelTierLookupService(p.db).GetModelTier(ctx, model)
}

// TraceBodies 返回一批 request_id 的正文（语义增强层取证；缺正文返回空 map，调用方降级 uncertain）。
func (p *dbReportProvider) TraceBodies(ctx context.Context, reqIDs []string) (map[string]string, error) {
	if len(reqIDs) == 0 {
		return map[string]string{}, nil
	}
	return loadTraceBodies(ctx, p.db, reqIDs)
}
