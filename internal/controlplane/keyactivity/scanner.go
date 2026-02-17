package keyactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ConfigProvider 提供 key_activity 配置的接口（避免直接依赖 service 包造成循环引用）
type ConfigProvider interface {
	GetKeyActivityConfig(ctx context.Context) *KeyActivityConfig
}

// KeyActivityRepo Scanner/Bootstrap 所需的仓储接口
// 真实实现见 Repo（repo.go）；测试使用 fakeRepo。
type KeyActivityRepo interface {
	UpsertSeen(row *KeySourceSeen) error
	BatchUpsertSeen(rows []KeySourceSeen) error
	SeenExists(keyID, sourceFP string) (bool, error)
	AnySeen(keyID string) (bool, error)
	TouchSeen(keyID, sourceFP string, ts time.Time) error
	GetOrInitState(keyID string) (*KeyActivityState, error)
	UpdateState(state *KeyActivityState) error
	InsertAlert(alert *KeyActivityAlert) (bool, error)
	FetchCallLogs(cursor Cursor, batch int) ([]CallLogRow, error)
	SaveCursor(cur Cursor)
	LoadCursor() Cursor
	GetConfigValue(key string) string
	SetConfigValue(key, value string)
	FetchLastSeenPerKey(since time.Time) (map[string]time.Time, error)
	FetchRecentHourlyStats(days int) (map[string]map[time.Time]float64, error)
	BatchFetchCallLogsForBootstrap(since time.Time, batchSize int, lastCursor Cursor) ([]CallLogRow, error)
}

// Notifier 发送站内信通知的接口（真实实现写 notifications 表）
type Notifier interface {
	Notify(userUUID uuid.UUID, title, body, level, refType, refID string) error
}

// KeyLookup 查询 api_keys 表（返回 name 和 user_id）
type KeyLookup interface {
	FindKeyByID(keyIDStr string) (string, uuid.UUID, error)
}

const defaultBatchSize = 2000

// Scanner Key 活动扫描器：增量扫 call_logs，判定 E1-E4 四类事件
// 完全离线运行，不影响 gateway 热路径。
type Scanner struct {
	kaRepo   KeyActivityRepo
	notifier Notifier
	cfgProv  ConfigProvider
	keyRepo  KeyLookup
	keyCache map[string]keyInfo
}

type keyInfo struct {
	name   string
	userID uuid.UUID
}

// NewScanner 创建 Scanner 实例
func NewScanner(kaRepo KeyActivityRepo, notifier Notifier, cfgProv ConfigProvider, keyRepo KeyLookup) *Scanner {
	return &Scanner{
		kaRepo:   kaRepo,
		notifier: notifier,
		cfgProv:  cfgProv,
		keyRepo:  keyRepo,
		keyCache: make(map[string]keyInfo),
	}
}

// IsEnabled 判断 key_activity.enabled 总开关（每轮扫描都读最新值）
func (s *Scanner) IsEnabled(ctx context.Context) bool {
	cfg := s.cfgProv.GetKeyActivityConfig(ctx)
	return cfg != nil && cfg.Enabled
}

// ScanOnce 执行一轮扫描：冷启动检查 → 拉增量 → 逐行 E1/E2/E3 → 批量 E4 → 保存游标
func (s *Scanner) ScanOnce(ctx context.Context) error {
	if !s.bootstrapDone() {
		return s.bootstrap(ctx)
	}

	cur := s.kaRepo.LoadCursor()
	cfg := s.cfgProv.GetKeyActivityConfig(ctx)
	if cfg == nil {
		return fmt.Errorf("key_activity: config unavailable")
	}
	rows, err := s.kaRepo.FetchCallLogs(cur, defaultBatchSize)
	if err != nil {
		return fmt.Errorf("key_activity: fetch_call_logs: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	s.keyCache = make(map[string]keyInfo)

	touched := make(map[string]bool, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.KeyID == "" {
			continue
		}
		s.evalRow(ctx, row, cfg)
		touched[row.KeyID] = true
		cur = Cursor{TS: row.TS, RequestID: row.RequestID}
	}

	s.evalUsageSpike(ctx, rows, touched, cfg)
	s.kaRepo.SaveCursor(cur)

	slog.Info("key_activity_scan",
		"rows", len(rows),
		"keys_touched", len(touched),
		"cursor_ts", cur.TS.Format(time.RFC3339),
		"lag_sec", int(time.Since(cur.TS).Seconds()),
	)
	return nil
}

// evalRow 逐行评估 E1/E2/E3，并更新 state.last_seen_at
func (s *Scanner) evalRow(ctx context.Context, row *CallLogRow, cfg *KeyActivityConfig) {
	fp := SourceFP(row.UserAgent, row.IPAddress, cfg.IPAsSource)

	anySeen, err := s.kaRepo.AnySeen(row.KeyID)
	if err != nil {
		slog.Warn("key_activity: AnySeen failed", "key_id", row.KeyID, "error", err)
		return
	}

	keyName := s.getKeyName(row)

	if !anySeen {
		s.upsertSeen(row, fp)
		s.emitAlert(ctx, EventTypeFirstUse, *row, fp, "first", keyName, "")
	} else {
		exists, err := s.kaRepo.SeenExists(row.KeyID, fp)
		if err != nil {
			slog.Warn("key_activity: SeenExists failed", "key_id", row.KeyID, "error", err)
			return
		}
		if !exists {
			s.upsertSeen(row, fp)
			s.emitAlert(ctx, EventTypeNewSource, *row, fp, fp, keyName, "")
		} else {
			_ = s.kaRepo.TouchSeen(row.KeyID, fp, row.TS)
		}
	}

	state, err := s.kaRepo.GetOrInitState(row.KeyID)
	if err != nil {
		slog.Warn("key_activity: GetOrInitState failed", "key_id", row.KeyID, "error", err)
		return
	}
	if state.LastSeenAt != nil {
		dormancy := time.Duration(cfg.DormantDays) * 24 * time.Hour
		if row.TS.Sub(*state.LastSeenAt) >= dormancy {
			dormantDays := int(row.TS.Sub(*state.LastSeenAt).Hours() / 24)
			dedup := row.TS.Format("2006-01-02")
			detail := fmt.Sprintf(`{"dormant_days":%d}`, dormantDays)
			s.emitAlert(ctx, EventTypeWakeup, *row, fp, dedup, keyName, detail)
		}
	}

	ts := row.TS
	state.LastSeenAt = &ts
	state.UpdatedAt = time.Now()
	_ = s.kaRepo.UpdateState(state)
}

// evalUsageSpike 对本批涉及的 key 按小时桶聚合后评估 E4（EWMA 先判后更）
func (s *Scanner) evalUsageSpike(ctx context.Context, rows []CallLogRow, touched map[string]bool, cfg *KeyActivityConfig) {
	type hourEntry struct {
		keyID string
		hour  time.Time
		count float64
	}

	hourCounts := make(map[string]map[time.Time]float64)
	nowHour := hourBucketTime(time.Now())

	for _, row := range rows {
		if row.KeyID == "" {
			continue
		}
		hb := hourBucketTime(row.TS)
		if !hb.Before(nowHour) {
			continue
		}
		if _, ok := hourCounts[row.KeyID]; !ok {
			hourCounts[row.KeyID] = make(map[time.Time]float64)
		}
		hourCounts[row.KeyID][hb]++
	}

	var entries []hourEntry
	for keyID, hours := range hourCounts {
		for h, c := range hours {
			entries = append(entries, hourEntry{keyID: keyID, hour: h, count: c})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].keyID != entries[j].keyID {
			return entries[i].keyID < entries[j].keyID
		}
		return entries[i].hour.Before(entries[j].hour)
	})

	stateCache := make(map[string]*KeyActivityState)
	getState := func(keyID string) (*KeyActivityState, error) {
		if st, ok := stateCache[keyID]; ok {
			return st, nil
		}
		st, err := s.kaRepo.GetOrInitState(keyID)
		if err != nil {
			return nil, err
		}
		stateCache[keyID] = st
		return st, nil
	}

	for _, e := range entries {
		st, err := getState(e.keyID)
		if err != nil {
			slog.Warn("key_activity: get state failed", "key_id", e.keyID, "error", err)
			continue
		}

		if st.BaselineReady && st.EwmaMean >= cfg.MinBaseline {
			dev := math.Max(st.EwmaDev, cfg.MinDev)
			threshold := st.EwmaMean + cfg.K*dev
			if e.count > threshold {
				hbKey := hourBucketKey(e.hour)
				detail, _ := json.Marshal(map[string]interface{}{
					"count": int(e.count), "mean": st.EwmaMean, "dev": st.EwmaDev, "threshold": threshold,
				})
				keyName := s.getCachedKeyName(e.keyID)
				vRow := CallLogRow{
					KeyID:  e.keyID,
					UserID: s.getCachedUserID(e.keyID),
					TS:     e.hour,
				}
				s.emitAlert(ctx, EventTypeSpike, vRow, "", hbKey, keyName, string(detail))
			}
		}

		prevMean := st.EwmaMean
		st.EwmaMean = cfg.Alpha*e.count + (1-cfg.Alpha)*st.EwmaMean
		st.EwmaDev = cfg.Alpha*math.Abs(e.count-prevMean) + (1-cfg.Alpha)*st.EwmaDev
		st.Samples++
		st.UpdatedAt = time.Now()
		_ = s.kaRepo.UpdateState(st)
	}
}

func (s *Scanner) bootstrap(ctx context.Context) error {
	cfg := s.cfgProv.GetKeyActivityConfig(ctx)
	if cfg == nil {
		return fmt.Errorf("key_activity: config unavailable for bootstrap")
	}
	return RunBootstrap(ctx, s.kaRepo, cfg)
}

func (s *Scanner) bootstrapDone() bool {
	return s.kaRepo.GetConfigValue("key_activity.bootstrap_done") == "true"
}

func (s *Scanner) upsertSeen(row *CallLogRow, fp string) {
	uaFP := UAFingerprint(row.UserAgent)
	record := &KeySourceSeen{
		KeyID:     row.KeyID,
		SourceFP:  fp,
		UAFp:      uaFP,
		UARaw:     row.UserAgent,
		IP:        row.IPAddress,
		FirstSeen: row.TS,
		LastSeen:  row.TS,
		HitCount:  1,
	}
	if err := s.kaRepo.UpsertSeen(record); err != nil {
		slog.Warn("key_activity: UpsertSeen failed", "key_id", row.KeyID, "error", err)
	}
}

func (s *Scanner) getKeyName(row *CallLogRow) string {
	if row.KeyName != "" {
		if uid, err := uuid.Parse(row.UserID); err == nil {
			s.keyCache[row.KeyID] = keyInfo{name: row.KeyName, userID: uid}
		}
		return row.KeyName
	}
	if info, ok := s.keyCache[row.KeyID]; ok {
		return info.name
	}
	name, uid, err := s.keyRepo.FindKeyByID(row.KeyID)
	if err != nil {
		return ""
	}
	s.keyCache[row.KeyID] = keyInfo{name: name, userID: uid}
	return name
}

func (s *Scanner) getCachedKeyName(keyID string) string {
	if info, ok := s.keyCache[keyID]; ok {
		return info.name
	}
	name, uid, err := s.keyRepo.FindKeyByID(keyID)
	if err != nil {
		return ""
	}
	s.keyCache[keyID] = keyInfo{name: name, userID: uid}
	return name
}

func (s *Scanner) getCachedUserID(keyID string) string {
	if info, ok := s.keyCache[keyID]; ok {
		return info.userID.String()
	}
	name, uid, err := s.keyRepo.FindKeyByID(keyID)
	if err != nil {
		return ""
	}
	s.keyCache[keyID] = keyInfo{name: name, userID: uid}
	return uid.String()
}
