package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChannelMonitorService handles channel monitor business logic.
type ChannelMonitorService struct {
	repo     *repository.ChannelMonitorRepo
	runner   MonitorScheduler
	settings SettingsReader
	// opsAlertSender 探活失败时联动 webhook 运维告警（nil 表示未装配）。
	opsAlertSender *OpsAlertSender
}

// SettingsReader reads settings from the key-value store.
type SettingsReader interface {
	GetValue(key string) (string, error)
}

// NewChannelMonitorService creates a new ChannelMonitorService.
func NewChannelMonitorService(repo *repository.ChannelMonitorRepo) *ChannelMonitorService {
	return &ChannelMonitorService{repo: repo}
}

// SetRunner sets the monitor scheduler for dynamic scheduling.
func (s *ChannelMonitorService) SetRunner(runner MonitorScheduler) {
	s.runner = runner
}

// SetSettingsReader sets the settings reader for feature flags.
func (s *ChannelMonitorService) SetSettingsReader(sr SettingsReader) {
	s.settings = sr
}

// SetOpsAlertSender 注入运维告警发送器，探活失败时联动 webhook 告警。
func (s *ChannelMonitorService) SetOpsAlertSender(sender *OpsAlertSender) {
	s.opsAlertSender = sender
}

// ── CRUD ────────────────────────────────────────────────────────────────────────

// Create creates a new channel monitor.
func (s *ChannelMonitorService) Create(ctx context.Context, params ChannelMonitorCreateParams) (*ChannelMonitorView, error) {
	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if err := validateAPIMode(params.Provider, params.APIMode); err != nil {
		return nil, err
	}
	if err := validateEndpoint(params.Endpoint); err != nil {
		return nil, err
	}
	if err := validateInterval(params.IntervalSeconds); err != nil {
		return nil, err
	}

	encryptedKey, err := crypto.Encrypt(params.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	m := &model.ChannelMonitor{
		Name:             params.Name,
		Provider:         params.Provider,
		APIMode:          defaultAPIMode(params.APIMode),
		Endpoint:         normalizeEndpoint(params.Endpoint),
		APIKeyEncrypted:  encryptedKey,
		PrimaryModel:     params.PrimaryModel,
		ExtraModels:      normalizeModels(params.ExtraModels),
		GroupName:        params.GroupName,
		Enabled:          params.Enabled,
		IntervalSeconds:  params.IntervalSeconds,
		CreatedBy:        params.CreatedBy,
		TemplateID:       params.TemplateID,
		ExtraHeaders:     emptyHeadersIfNil(params.ExtraHeaders),
		BodyOverrideMode: defaultBodyMode(params.BodyOverrideMode),
		BodyOverride:     model.JSONRaw(params.BodyOverride),
	}

	if err := s.repo.Create(m); err != nil {
		return nil, fmt.Errorf("create monitor: %w", err)
	}

	view := modelToView(m, params.APIKey)
	if s.runner != nil {
		s.runner.Schedule(view)
	}
	return view, nil
}

// Update updates an existing channel monitor.
func (s *ChannelMonitorService) Update(ctx context.Context, id int64, params ChannelMonitorUpdateParams) (*ChannelMonitorView, error) {
	m, err := s.repo.GetByID(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmtAppError("CHANNEL_MONITOR_NOT_FOUND", "monitor not found")
		}
		return nil, fmt.Errorf("get monitor: %w", err)
	}

	apiKey, _ := crypto.Decrypt(m.APIKeyEncrypted)

	if params.Name != nil {
		m.Name = *params.Name
	}
	if params.Provider != nil {
		if err := validateProvider(*params.Provider); err != nil {
			return nil, err
		}
		m.Provider = *params.Provider
	}
	if params.APIMode != nil {
		if err := validateAPIMode(m.Provider, *params.APIMode); err != nil {
			return nil, err
		}
		m.APIMode = defaultAPIMode(*params.APIMode)
	}
	if params.Endpoint != nil {
		if err := validateEndpoint(*params.Endpoint); err != nil {
			return nil, err
		}
		m.Endpoint = normalizeEndpoint(*params.Endpoint)
	}
	if params.APIKey != nil {
		encryptedKey, err := crypto.Encrypt(*params.APIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt api key: %w", err)
		}
		m.APIKeyEncrypted = encryptedKey
		apiKey = *params.APIKey
	}
	if params.PrimaryModel != nil {
		m.PrimaryModel = *params.PrimaryModel
	}
	if params.ExtraModels != nil {
		m.ExtraModels = normalizeModels(*params.ExtraModels)
	}
	if params.GroupName != nil {
		m.GroupName = *params.GroupName
	}
	if params.Enabled != nil {
		m.Enabled = *params.Enabled
	}
	if params.IntervalSeconds != nil {
		if err := validateInterval(*params.IntervalSeconds); err != nil {
			return nil, err
		}
		m.IntervalSeconds = *params.IntervalSeconds
	}
	if params.TemplateID != nil {
		m.TemplateID = params.TemplateID
	}
	if params.ClearTemplate {
		m.TemplateID = nil
	}
	if params.ExtraHeaders != nil {
		m.ExtraHeaders = emptyHeadersIfNil(*params.ExtraHeaders)
	}
	if params.BodyOverrideMode != nil {
		m.BodyOverrideMode = defaultBodyMode(*params.BodyOverrideMode)
	}
	if params.BodyOverride != nil {
		m.BodyOverride = model.JSONRaw(*params.BodyOverride)
	}

	if err := s.repo.Update(m); err != nil {
		return nil, fmt.Errorf("update monitor: %w", err)
	}

	view := modelToView(m, apiKey)
	if s.runner != nil {
		if m.Enabled {
			s.runner.Schedule(view)
		} else {
			s.runner.Unschedule(m.ID)
		}
	}
	return view, nil
}

// Delete soft-deletes a channel monitor.
func (s *ChannelMonitorService) Delete(ctx context.Context, id int64) error {
	if s.runner != nil {
		s.runner.Unschedule(id)
	}
	return s.repo.Delete(id)
}

// Get returns a single monitor with decrypted API key.
func (s *ChannelMonitorService) Get(ctx context.Context, id int64) (*ChannelMonitorView, error) {
	m, err := s.repo.GetByID(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmtAppError("CHANNEL_MONITOR_NOT_FOUND", "monitor not found")
		}
		return nil, fmt.Errorf("get monitor: %w", err)
	}
	apiKey, err := crypto.Decrypt(m.APIKeyEncrypted)
	view := modelToView(m, apiKey)
	view.APIKeyDecryptFailed = (err != nil)
	return view, nil
}

// List returns a paginated list of monitors.
func (s *ChannelMonitorService) List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitorView, int64, error) {
	items, total, err := s.repo.List(params.Provider, params.Enabled, params.Search, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}

	views := make([]*ChannelMonitorView, 0, len(items))
	for _, m := range items {
		apiKey, err := crypto.Decrypt(m.APIKeyEncrypted)
		view := modelToView(m, apiKey)
		view.APIKeyDecryptFailed = (err != nil)
		views = append(views, view)
	}
	return views, total, nil
}

// ── Check ───────────────────────────────────────────────────────────────────────

// RunCheck runs a single check for all models on a monitor.
func (s *ChannelMonitorService) RunCheck(ctx context.Context, monitorID int64) ([]ChannelCheckResult, error) {
	m, err := s.repo.GetByID(monitorID)
	if err != nil {
		return nil, fmt.Errorf("get monitor: %w", err)
	}

	apiKey, err := crypto.Decrypt(m.APIKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}

	view := modelToView(m, apiKey)
	models := allModels(view)

	var results []ChannelCheckResult
	var historyRows []*model.ChannelMonitorHistory

	for _, modelName := range models {
		result := runCheckForModel(ctx, view, modelName)
		results = append(results, result)

		historyRows = append(historyRows, &model.ChannelMonitorHistory{
			MonitorID:     monitorID,
			Model:         modelName,
			Status:        result.Status,
			LatencyMs:     result.LatencyMs,
			PingLatencyMs: result.PingLatencyMs,
			Message:       truncateMessage(result.Message),
			CheckedAt:     result.CheckedAt,
		})
	}

	// 任一模型探活失败/错误即触发运维告警（degraded 仅慢速不告警）。
	if s.opsAlertSender != nil && monitorHasAnyFailure(results) {
		failures := 0
		firstErr := ""
		for _, r := range results {
			if r.Status == MonitorStatusFailed || r.Status == MonitorStatusError {
				failures++
				if firstErr == "" && r.Message != "" {
					firstErr = r.Message
				}
			}
		}
		s.opsAlertSender.NotifyChannelDown(ctx,
			"channel_monitor", view.Provider, uuid.Nil, view.Name, view.ID,
			failures, len(results), firstErr)
	}

	if err := s.repo.InsertHistoryBatch(historyRows); err != nil {
		slog.Error("channel_monitor: failed to insert history", "monitor_id", monitorID, "error", err)
	}

	now := time.Now()
	if err := s.repo.MarkChecked(monitorID, now); err != nil {
		slog.Error("channel_monitor: failed to mark checked", "monitor_id", monitorID, "error", err)
	}

	return results, nil
}

// ── Maintenance ─────────────────────────────────────────────────────────────────

// RunDailyMaintenance runs daily aggregation and cleanup.
func (s *ChannelMonitorService) RunDailyMaintenance(ctx context.Context) error {
	wm, err := s.repo.LoadAggregationWatermark()
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}

	today := time.Now().Truncate(24 * time.Hour)
	startDate := today
	if wm != nil {
		startDate = wm.Add(24 * time.Hour).Truncate(24 * time.Hour)
	}

	daysProcessed := 0
	for d := startDate; d.Before(today) && daysProcessed < monitorMaintenanceMaxDaysPerRun; d = d.Add(24 * time.Hour) {
		if _, err := s.repo.UpsertDailyRollupsFor(d); err != nil {
			slog.Error("channel_monitor: failed to aggregate day", "date", d.Format("2006-01-02"), "error", err)
			return fmt.Errorf("aggregate %s: %w", d.Format("2006-01-02"), err)
		}
		daysProcessed++
	}

	if daysProcessed > 0 {
		lastDate := startDate.Add(time.Duration(daysProcessed-1) * 24 * time.Hour)
		if err := s.repo.UpdateAggregationWatermark(lastDate); err != nil {
			slog.Error("channel_monitor: failed to update watermark", "error", err)
		}
	}

	cutoff := time.Now().AddDate(0, 0, -monitorHistoryRetentionDays)
	deleted, err := s.repo.DeleteHistoryBefore(cutoff)
	if err != nil {
		slog.Error("channel_monitor: failed to cleanup history", "error", err)
	} else if deleted > 0 {
		slog.Info("channel_monitor: cleaned up old history", "deleted", deleted)
	}

	rollupCutoff := time.Now().AddDate(0, 0, -monitorRollupRetentionDays)
	deletedRollups, err := s.repo.DeleteRollupsBefore(rollupCutoff)
	if err != nil {
		slog.Error("channel_monitor: failed to cleanup rollups", "error", err)
	} else if deletedRollups > 0 {
		slog.Info("channel_monitor: cleaned up old rollups", "deleted", deletedRollups)
	}

	return nil
}

// ── Availability ────────────────────────────────────────────────────────────────

// ComputeAvailability computes availability from daily rollups.
func (s *ChannelMonitorService) ComputeAvailability(monitorID int64, windowDays int) ([]repository.AvailabilityRow, error) {
	return s.repo.ComputeAvailabilityFromRollups(monitorID, windowDays)
}

// ── History ─────────────────────────────────────────────────────────────────────

// GetHistory returns recent check history for a monitor.
func (s *ChannelMonitorService) GetHistory(ctx context.Context, monitorID int64, modelName string, limit int) ([]*model.ChannelMonitorHistory, error) {
	if limit <= 0 || limit > MonitorHistoryMaxLimit {
		limit = MonitorHistoryDefaultLimit
	}
	return s.repo.ListHistory(monitorID, modelName, limit)
}

// ── User-facing ─────────────────────────────────────────────────────────────────

// ListPublic returns enabled monitors for public view.
func (s *ChannelMonitorService) ListPublic(ctx context.Context) ([]UserMonitorView, error) {
	monitors, err := s.repo.ListEnabled()
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	if len(monitors) == 0 {
		return []UserMonitorView{}, nil
	}

	ids := make([]int64, len(monitors))
	primaryModels := make(map[int64]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
		primaryModels[m.ID] = m.PrimaryModel
	}

	statusMap, err := s.repo.ListLatestForMonitorIDs(ids)
	if err != nil {
		slog.Error("channel_monitor: failed to fetch statuses", "error", err)
		statusMap = map[int64][]repository.LatestStatusRow{}
	}

	availMap, err := s.repo.ComputeAvailabilityForMonitors(ids, monitorAvailability7Days)
	if err != nil {
		slog.Error("channel_monitor: failed to fetch availability", "error", err)
		availMap = map[int64][]repository.AvailabilityRow{}
	}

	historyMap, err := s.repo.ListRecentHistoryForMonitors(ids, primaryModels, monitorTimelineMaxPoints)
	if err != nil {
		slog.Error("channel_monitor: failed to fetch history", "error", err)
		historyMap = map[int64][]*model.ChannelMonitorHistory{}
	}

	views := make([]UserMonitorView, 0, len(monitors))
	for _, m := range monitors {
		view := buildUserMonitorView(m, statusMap[m.ID], availMap[m.ID], historyMap[m.ID])
		views = append(views, view)
	}
	return views, nil
}

// GetPublicStatus returns detailed status for a single monitor.
func (s *ChannelMonitorService) GetPublicStatus(ctx context.Context, monitorID int64) (*UserMonitorDetail, error) {
	m, err := s.repo.GetByID(monitorID)
	if err != nil {
		return nil, fmt.Errorf("get monitor: %w", err)
	}

	latestRows, err := s.repo.ListLatestPerModel(monitorID)
	if err != nil {
		return nil, fmt.Errorf("list latest per model: %w", err)
	}

	modelStatusMap := make(map[string]repository.LatestStatusRow)
	for _, row := range latestRows {
		modelStatusMap[row.Model] = row
	}

	avail7d, _ := s.repo.ComputeAvailabilityFromRollups(monitorID, monitorAvailability7Days)
	avail15d, _ := s.repo.ComputeAvailabilityFromRollups(monitorID, monitorAvailability15Days)
	avail30d, _ := s.repo.ComputeAvailabilityFromRollups(monitorID, monitorAvailability30Days)

	avail7dMap := availMap(avail7d)
	avail15dMap := availMap(avail15d)
	avail30dMap := availMap(avail30d)

	models := allModelNames(m)
	details := make([]ModelDetail, 0, len(models))
	for _, modelName := range models {
		detail := ModelDetail{Model: modelName}
		if status, ok := modelStatusMap[modelName]; ok {
			detail.LatestStatus = status.Status
			detail.LatestLatencyMs = status.LatencyMs
		}
		if a, ok := avail7dMap[modelName]; ok {
			detail.Availability7d = a.AvailabilityPct
			detail.AvgLatency7dMs = a.AvgLatencyMs
		}
		if a, ok := avail15dMap[modelName]; ok {
			detail.Availability15d = a.AvailabilityPct
		}
		if a, ok := avail30dMap[modelName]; ok {
			detail.Availability30d = a.AvailabilityPct
		}
		details = append(details, detail)
	}

	return &UserMonitorDetail{
		ID:        m.ID,
		Name:      m.Name,
		Provider:  m.Provider,
		GroupName: m.GroupName,
		Models:    details,
	}, nil
}

// ── Templates ───────────────────────────────────────────────────────────────────

// CreateTemplate creates a request template.
func (s *ChannelMonitorService) CreateTemplate(ctx context.Context, t *model.ChannelMonitorRequestTemplate) error {
	return s.repo.CreateTemplate(t)
}

// UpdateTemplate updates a request template.
func (s *ChannelMonitorService) UpdateTemplate(ctx context.Context, t *model.ChannelMonitorRequestTemplate) error {
	return s.repo.UpdateTemplate(t)
}

// DeleteTemplate soft-deletes a request template.
func (s *ChannelMonitorService) DeleteTemplate(ctx context.Context, id int64) error {
	return s.repo.DeleteTemplate(id)
}

// GetTemplate returns a request template by ID.
func (s *ChannelMonitorService) GetTemplate(ctx context.Context, id int64) (*model.ChannelMonitorRequestTemplate, error) {
	t, err := s.repo.GetTemplateByID(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmtAppError("CHANNEL_MONITOR_TEMPLATE_NOT_FOUND", "template not found")
		}
		return nil, err
	}
	return t, nil
}

// ListTemplates returns a paginated list of templates.
func (s *ChannelMonitorService) ListTemplates(ctx context.Context, provider string, page, pageSize int) ([]*model.ChannelMonitorRequestTemplate, int64, error) {
	return s.repo.ListTemplates(provider, page, pageSize)
}

// ── Helpers ─────────────────────────────────────────────────────────────────────

func modelToView(m *model.ChannelMonitor, apiKey string) *ChannelMonitorView {
	return &ChannelMonitorView{
		ID:                 m.ID,
		Name:               m.Name,
		Provider:           m.Provider,
		APIMode:            m.APIMode,
		Endpoint:           m.Endpoint,
		APIKey:             apiKey,
		PrimaryModel:       m.PrimaryModel,
		ExtraModels:        []string(m.ExtraModels),
		GroupName:          m.GroupName,
		Enabled:            m.Enabled,
		IntervalSeconds:    m.IntervalSeconds,
		LastCheckedAt:      m.LastCheckedAt,
		CreatedBy:          m.CreatedBy,
		ExtraHeaders:       map[string]string(m.ExtraHeaders),
		BodyOverrideMode:   m.BodyOverrideMode,
		BodyOverride:       map[string]interface{}(m.BodyOverride),
		TemplateID:         m.TemplateID,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		APIKeyDecryptFailed: false,
	}
}

func allModels(view *ChannelMonitorView) []string {
	models := []string{view.PrimaryModel}
	for _, m := range view.ExtraModels {
		if m != view.PrimaryModel {
			models = append(models, m)
		}
	}
	return models
}

func allModelNames(m *model.ChannelMonitor) []string {
	models := []string{m.PrimaryModel}
	for _, em := range m.ExtraModels {
		if em != m.PrimaryModel {
			models = append(models, em)
		}
	}
	return models
}

func buildUserMonitorView(
	m *model.ChannelMonitor,
	statuses []repository.LatestStatusRow,
	availRows []repository.AvailabilityRow,
	histories []*model.ChannelMonitorHistory,
) UserMonitorView {
	view := UserMonitorView{
		ID:           m.ID,
		Name:         m.Name,
		Provider:     m.Provider,
		GroupName:    m.GroupName,
		PrimaryModel: m.PrimaryModel,
		ExtraModels:  []ExtraModelStatus{},
		Timeline:     []UserMonitorTimelinePoint{},
	}

	statusMap := make(map[string]repository.LatestStatusRow)
	for _, s := range statuses {
		statusMap[s.Model] = s
	}

	availMap := make(map[string]float64)
	for _, a := range availRows {
		availMap[a.Model] = a.AvailabilityPct
	}

	if s, ok := statusMap[m.PrimaryModel]; ok {
		view.PrimaryStatus = s.Status
		view.PrimaryLatencyMs = s.LatencyMs
		view.PrimaryPingLatencyMs = s.PingLatencyMs
	}
	if a, ok := availMap[m.PrimaryModel]; ok {
		view.Availability7d = a
	}

	for _, modelName := range m.ExtraModels {
		es := ExtraModelStatus{Model: modelName}
		if s, ok := statusMap[modelName]; ok {
			es.Status = s.Status
			es.LatencyMs = s.LatencyMs
		}
		view.ExtraModels = append(view.ExtraModels, es)
	}

	for _, h := range histories {
		view.Timeline = append(view.Timeline, UserMonitorTimelinePoint{
			Status:        h.Status,
			LatencyMs:     h.LatencyMs,
			PingLatencyMs: h.PingLatencyMs,
			CheckedAt:     h.CheckedAt,
		})
	}

	return view
}

func availMap(rows []repository.AvailabilityRow) map[string]repository.AvailabilityRow {
	m := make(map[string]repository.AvailabilityRow, len(rows))
	for _, row := range rows {
		m[row.Model] = row
	}
	return m
}

func truncateMessage(msg string) string {
	if len(msg) > monitorMessageMaxBytes {
		return msg[:monitorMessageMaxBytes]
	}
	return msg
}

// monitorHasAnyFailure 判定一次检测是否存在失败模型：任一模型 failed/error 即视为渠道异常
//（degraded 仅慢速，不算故障，不触发告警）。
func monitorHasAnyFailure(results []ChannelCheckResult) bool {
	for _, r := range results {
		if r.Status == MonitorStatusFailed || r.Status == MonitorStatusError {
			return true
		}
	}
	return false
}