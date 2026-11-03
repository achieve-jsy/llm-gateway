package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/report"
)

// DBReportDataProvider 是 report_orchestrator.ReportDataProvider 的 DB 实现，供 api-eng 的
// usage/report HTTP 端点注入（service.GetReport(ctx, ReportParams{DataSource: provider})）。
//
// 相对 orchestrator 同包的基础 dbReportProvider（report_service.go，仅 DB 取数），本实现额外：
//   - 支持 WithRBACScope：把 UsagePolicy.QueryScope(actor) 的行级过滤（call_logs. 前缀）应用到
//     所有 call_logs 查询，越权访问天然落空（RBAC 下沉到数据层）。
//   - 闭区间 [from,to] 用 [from, to+1d) 半开查询，避免漏掉周期末日；排除 Service Key。
//   - 通过 PositionAttributionService 给 ReportUser / 判定行注入岗位归因（job_category/job_family/job_title）。
//
// 基线（baseline_quantiles）可选：物化表存在时规则读表（口径统一）；未物化时本实现在期内现算
// 群体分位参与分析（report_baseline_inline.go），不阻塞报告、不依赖新表。
type DBReportDataProvider struct {
	db *gorm.DB
	// from/to 是闭区间周期 [from,to]（Users() 无周期入参，需由 handler 预置）。
	from, to string
	// rbacScope 是 UsagePolicy.QueryScope(actor) 返回的 GORM scope；nil = 不限（全局 admin）。
	rbacScope func(*gorm.DB) *gorm.DB
}

// NewDBReportDataProvider 创建 DB 版报告数据源。
func NewDBReportDataProvider(db *gorm.DB) *DBReportDataProvider {
	return &DBReportDataProvider{db: db}
}

// WithPeriod 预置闭区间周期 [from, to]（YYYY-MM-DD，含端点），供 Users() 枚举在期成员。
func (p *DBReportDataProvider) WithPeriod(from, to string) *DBReportDataProvider {
	p.from, p.to = from, to
	return p
}

// WithRBACScope 预置行级 RBAC 过滤（作用于 call_logs 的 GORM scope）。
func (p *DBReportDataProvider) WithRBACScope(scope func(*gorm.DB) *gorm.DB) *DBReportDataProvider {
	p.rbacScope = scope
	return p
}

// rbac 返回追加了 RBAC scope 的查询。
func (p *DBReportDataProvider) rbac(q *gorm.DB) *gorm.DB {
	if p.rbacScope != nil {
		return q.Scopes(p.rbacScope)
	}
	return q
}

// periodRange 把闭区间 [from, to]（含端点）映射为 [from, to+1d) 半开查询，并排除 Service Key。
func (p *DBReportDataProvider) periodRange(q *gorm.DB, from, to string) *gorm.DB {
	end, _ := time.Parse("2006-01-02", to)
	toExcl := end.AddDate(0, 0, 1).Format("2006-01-02")
	return q.Where("ts >= ? AND ts < ?", from, toExcl).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
}

// userRow 是 Users 的 call_logs 去重投影行。
type userRow struct {
	UserID   string `gorm:"column:user_id"`
	UserName string `gorm:"column:user_name"`
	DeptID   string `gorm:"column:dept_id"`
	DeptName string `gorm:"column:dept_name"`
	StudioID string `gorm:"column:studio_id"`
}

// Users 返回 level 范围内的人（personal：本人；department：成员；studio：成员）。
// 部门/工作室从 call_logs 按范围去重；个人若当期无调用记录，回退为「仅身份」的零用量成员，
// 保证仍能产出一份空量化报告（cost=0、规则不命中），而不是 500。
func (p *DBReportDataProvider) Users(ctx context.Context, level string, scope report.Scope) ([]ReportUser, error) {
	col := "user_id"
	switch level {
	case report.LevelDepartment:
		col = "dept_id"
	case report.LevelStudio:
		col = "studio_id"
	}
	toExcl := ""
	if p.to != "" {
		if end, err := time.Parse("2006-01-02", p.to); err == nil {
			toExcl = end.AddDate(0, 0, 1).Format("2006-01-02")
		}
	}
	q := p.rbac(p.db.WithContext(ctx).Table("call_logs")).
		Select("DISTINCT user_id, user_name, dept_id, dept_name, studio_id")
	if p.from != "" {
		q = q.Where("ts >= ?", p.from)
	}
	if toExcl != "" {
		q = q.Where("ts < ?", toExcl)
	}
	if scope.ID != "" {
		q = q.Where(col+" = ?", scope.ID)
	}
	var rows []userRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("report provider: users: %w", err)
	}

	// 个人零用量回退：仍给该成员一个身份，报告按零成本组装。
	if level == report.LevelPersonal && len(rows) == 0 {
		return []ReportUser{{UserID: scope.ID, Name: scope.Name}}, nil
	}
	out := make([]ReportUser, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReportUser{UserID: r.UserID, Name: r.UserName, DeptID: r.DeptID, DeptName: r.DeptName})
		ids = append(ids, r.UserID)
	}
	// 批量岗位归因，填充 Position（供同岗基线分组）。
	if pos, err := NewPositionAttributionService(p.db).GetUserPositions(ctx, ids); err == nil {
		for i := range out {
			out[i].Position = pos[out[i].UserID].Position
		}
	}
	return out, nil
}

// CallRows 返回某人在 [from,to] 的调用行（供会话装配/成本聚合）。
func (p *DBReportDataProvider) CallRows(ctx context.Context, userID, from, to string) ([]CallLogRow, error) {
	var logs []obsmodel.CallLog
	q := p.rbac(p.db.WithContext(ctx)).Where("user_id = ?", userID)
	q = p.periodRange(q, from, to)
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

// OutputRunawayRows 返回某人逐请求输出失控判定行（status=200 成功请求）。
func (p *DBReportDataProvider) OutputRunawayRows(ctx context.Context, userID, from, to string) ([]OutputRunawayRow, error) {
	var logs []obsmodel.CallLog
	q := p.rbac(p.db.WithContext(ctx)).Where("user_id = ? AND status = ?", userID, 200)
	q = p.periodRange(q, from, to)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: output_rows: %w", err)
	}
	pos, _ := NewPositionAttributionService(p.db).GetUserPosition(ctx, userID)
	out := make([]OutputRunawayRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, OutputRunawayRow{
			RequestID: l.RequestID, UserID: l.UserID, SessionID: l.RequestID, Model: l.Model,
			Position:     pos.Position,
			PromptTokens: int64(l.PromptTokens), CachedTokens: int64(l.CachedTokens), CompletionTokens: int64(l.CompletionTokens),
			OutputCostUSD: l.OutputCostUSD,
		})
	}
	return out, nil
}

// OverModelRows 返回某人逐请求选型判定行（turns==1 视为单轮会话）。
func (p *DBReportDataProvider) OverModelRows(ctx context.Context, userID, from, to string) ([]OverModelRow, error) {
	var logs []obsmodel.CallLog
	q := p.rbac(p.db.WithContext(ctx)).Where("user_id = ? AND status = ?", userID, 200)
	q = p.periodRange(q, from, to)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: over_model_rows: %w", err)
	}
	pos, _ := NewPositionAttributionService(p.db).GetUserPosition(ctx, userID)
	out := make([]OverModelRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, OverModelRow{
			RequestID: l.RequestID, UserID: l.UserID, SessionID: l.RequestID, Model: l.Model,
			Position: pos.Position, SessionTurns: 1,
			PromptTokens: int64(l.PromptTokens), CachedTokens: int64(l.CachedTokens), CompletionTokens: int64(l.CompletionTokens),
			ActualCostUSD: l.EstCostUSD,
		})
	}
	return out, nil
}

// DataQuality 覆盖率：保守默认。
func (p *DBReportDataProvider) DataQuality(ctx context.Context, level string, scope report.Scope) (report.DataQuality, error) {
	return report.DataQuality{
		RealSessionCoverage:   1.0,
		SceneLabelCoverage:    0.0,
		PriceCatalogCoverage:  0.0,
		PositionLabelCoverage: 0.0,
	}, nil
}

// OutputRunawayBaselines 优先从 baseline_quantiles 读取同岗/同模型基线，缺失时优雅回退当期现算。
func (p *DBReportDataProvider) OutputRunawayBaselines(ctx context.Context, positions, models []string) (OutputRunawayBaselines, error) {
	return loadOutputRunawayBaselines(ctx, p.db, p.from, positions, models)
}

// MultiTaskBaselines 优先从 baseline_quantiles 读取用户 prompt P90 基线，缺失时优雅回退当期现算。
func (p *DBReportDataProvider) MultiTaskBaselines(ctx context.Context, userIDs []string) (MultiTaskBaselines, error) {
	return loadMultiTaskBaselines(ctx, p.db, p.from, userIDs)
}

// OverModelBaselines 优先从 baseline_quantiles 读取同岗 P25 / completion P50 基线，缺失时优雅回退当期现算。
func (p *DBReportDataProvider) OverModelBaselines(ctx context.Context, positions []string) (OverModelBaselines, error) {
	return loadOverModelBaselines(ctx, p.db, p.from, positions)
}

// Resolver 返回杀鸡用牛刀的档位解析器。
func (p *DBReportDataProvider) Resolver(ctx context.Context) (OverModelResolver, error) {
	return NewModelTierLookupService(p.db), nil
}

// ModelTier 返回某模型归一档位。
func (p *DBReportDataProvider) ModelTier(ctx context.Context, model string) (ModelTierInfo, error) {
	return NewModelTierLookupService(p.db).GetModelTier(ctx, model)
}

// OffhoursRows 返回某人 3.6 判定的原始行。
func (p *DBReportDataProvider) OffhoursRows(ctx context.Context, userID, from, to string) ([]OffhoursCallRow, error) {
	var logs []obsmodel.CallLog
	q := p.rbac(p.db.WithContext(ctx)).Where("user_id = ?", userID)
	q = p.periodRange(q, from, to)
	if err := q.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("report provider: offhours_rows: %w", err)
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
func (p *DBReportDataProvider) OffhoursFilings(ctx context.Context) ([]OffhoursFilingWindow, error) {
	var filings []cpmodel.OffhoursFiling
	if err := p.db.WithContext(ctx).Find(&filings).Error; err != nil {
		// 缺表（offhours_filing 迁移未跑）时降级为「无备案窗口」：不中断报告，3.6 不做备案排除。
		if isTableNotExistError(err) {
			return []OffhoursFilingWindow{}, nil
		}
		return nil, fmt.Errorf("report provider: offhours_filings: %w", err)
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

// TraceBodies 返回一批 request_id 的正文（语义增强层取证；RBAC 范围内）。
func (p *DBReportDataProvider) TraceBodies(ctx context.Context, reqIDs []string) (map[string]string, error) {
	if len(reqIDs) == 0 {
		return map[string]string{}, nil
	}
	q := p.rbac(p.db.Table("call_traces"))
	var rows []struct {
		RequestID string `gorm:"column:request_id"`
		Body      string `gorm:"column:request_body"`
	}
	err := q.Select("request_id, request_body::text AS request_body").
		Where("request_id IN ?", reqIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("trace bodies: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.RequestID] = r.Body
	}
	return out, nil
}
