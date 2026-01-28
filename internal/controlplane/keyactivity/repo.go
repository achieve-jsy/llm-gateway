package keyactivity

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repo Key 活动提醒数据访问层，持有三张表的 CRUD 操作。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建 Repo 实例
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// UpsertSeen 写已见来源：INSERT ON CONFLICT (key_id, source_fp) DO UPDATE last_seen + hit_count
// 用于 E1/E2 扫描阶段（DO UPDATE，累加计数）
func (r *Repo) UpsertSeen(row *KeySourceSeen) error {
	return r.db.Exec(`
		INSERT INTO key_source_seen (key_id, source_fp, ua_fp, ua_raw, ip, first_seen, last_seen, hit_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key_id, source_fp) DO UPDATE SET
			last_seen = EXCLUDED.last_seen,
			hit_count = key_source_seen.hit_count + 1
	`, row.KeyID, row.SourceFP, row.UAFp, row.UARaw, row.IP, row.FirstSeen, row.LastSeen, row.HitCount).Error
}

// BatchUpsertSeen 批量写已见来源：INSERT ON CONFLICT DO NOTHING（冷启动回灌，保证幂等）
func (r *Repo) BatchUpsertSeen(rows []KeySourceSeen) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key_id"}, {Name: "source_fp"}},
		DoNothing: true,
	}).Create(&rows).Error
}

// SeenExists 判断 (key_id, source_fp) 是否已在已见表中（E1 判定核心，O(1) 索引查询）
func (r *Repo) SeenExists(keyID, sourceFP string) (bool, error) {
	var exists bool
	err := r.db.Raw(
		"SELECT EXISTS(SELECT 1 FROM key_source_seen WHERE key_id = $1 AND source_fp = $2)",
		keyID, sourceFP,
	).Scan(&exists).Error
	return exists, err
}

// AnySeen 判断该 Key 是否有任何已见来源（E2 判定：空=true → 首次使用）
func (r *Repo) AnySeen(keyID string) (bool, error) {
	var exists bool
	err := r.db.Raw(
		"SELECT EXISTS(SELECT 1 FROM key_source_seen WHERE key_id = $1)",
		keyID,
	).Scan(&exists).Error
	return exists, err
}

// TouchSeen 仅更新已知来源的 last_seen 和 hit_count
func (r *Repo) TouchSeen(keyID, sourceFP string, ts time.Time) error {
	return r.db.Model(&KeySourceSeen{}).
		Where("key_id = ? AND source_fp = ?", keyID, sourceFP).
		Updates(map[string]interface{}{
			"last_seen": ts,
			"hit_count": gorm.Expr("hit_count + 1"),
		}).Error
}

// GetOrInitState 获取 Key 状态，不存在则懒初始化（baseline_ready=false、EWMA=0）
func (r *Repo) GetOrInitState(keyID string) (*KeyActivityState, error) {
	var state KeyActivityState
	err := r.db.Where("key_id = ?", keyID).First(&state).Error
	if err == gorm.ErrRecordNotFound {
		now := time.Now()
		state = KeyActivityState{
			KeyID:         keyID,
			EwmaMean:      0,
			EwmaDev:       0,
			LastSeenAt:    nil,
			LastSpikeHour: nil,
			Samples:       0,
			BaselineReady: false,
			UpdatedAt:     now,
		}
		if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
			return nil, err
		}
		// ON CONFLICT DO NOTHING 后 state 可能仍为零值（已存在则未返回），再 SELECT 保证完整
		return r.GetOrInitState(keyID)
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// UpdateState 更新 EWMA 状态到数据库
// 使用 upsert（INSERT ON CONFLICT DO UPDATE），不存在时自动插入，已存在时更新。
// 调用方可直接传新构造的 state 而无需先调 GetOrInitState。
func (r *Repo) UpdateState(state *KeyActivityState) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"ewma_mean":       state.EwmaMean,
			"ewma_dev":        state.EwmaDev,
			"last_seen_at":    state.LastSeenAt,
			"last_spike_hour": state.LastSpikeHour,
			"samples":         state.Samples,
			"baseline_ready":  state.BaselineReady,
			"updated_at":      state.UpdatedAt,
		}),
	}).Create(state).Error
}

// InsertAlert 写入提醒记录，ON CONFLICT DO NOTHING（唯一键去重）
// 返回 true 表示新插入（RowsAffected=1，需要发通知）；false 表示已存在（幂等，跳过通知）
// 兼容 Postgres（JSONB 列需显式 cast）和 SQLite（测试用，存 TEXT）
func (r *Repo) InsertAlert(alert *KeyActivityAlert) (bool, error) {
	detail := alert.Detail
	if detail == "" {
		detail = "{}"
	}
	jsonParam := "?"
	if r.db.Dialector.Name() == "postgres" {
		jsonParam = "?::jsonb"
	}
	result := r.db.Exec(`
		INSERT INTO key_activity_alert (key_id, user_id, event_type, dedup_key, ua_raw, ip, detail, notified_at)
		VALUES (?, ?, ?, ?, ?, ?, `+jsonParam+`, ?)
		ON CONFLICT (key_id, event_type, dedup_key) DO NOTHING
	`, alert.KeyID, alert.UserID, alert.EventType, alert.DedupKey, alert.UARaw, alert.IP, detail, alert.NotifiedAt)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// FetchCallLogs 增量扫描：复合水位 (ts, request_id)，依赖 idx_call_logs_ts 索引
func (r *Repo) FetchCallLogs(cursor Cursor, batch int) ([]CallLogRow, error) {
	var rows []struct {
		RequestID string
		TS        time.Time
		KeyID     string
		KeyName   string
		UserID    string
		UserAgent string
		IPAddress string
	}
	err := r.db.Raw(`
		SELECT request_id, ts,
		       COALESCE(key_id, '')   AS key_id,
		       COALESCE(key_name, '') AS key_name,
		       COALESCE(user_id, '')  AS user_id,
		       COALESCE(user_agent, '') AS user_agent,
		       COALESCE(ip_address, '') AS ip_address
		FROM call_logs
		WHERE ts > ? OR (ts = ? AND request_id > ?)
		ORDER BY ts ASC, request_id ASC
		LIMIT ?
	`, cursor.TS, cursor.TS, cursor.RequestID, batch).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]CallLogRow, len(rows))
	for i, row := range rows {
		result[i] = CallLogRow{
			RequestID: row.RequestID,
			TS:        row.TS,
			KeyID:     row.KeyID,
			KeyName:   row.KeyName,
			UserID:    row.UserID,
			UserAgent: row.UserAgent,
			IPAddress: row.IPAddress,
		}
	}
	return result, nil
}

// SaveCursor 将扫描游标写入 system_configs
func (r *Repo) SaveCursor(cur Cursor) {
	data, _ := json.Marshal(cur)
	r.db.Exec(
		"INSERT INTO system_configs (key, value) VALUES ('key_activity.scan_cursor', ?) "+
			"ON CONFLICT (key) DO UPDATE SET value = ?",
		string(data), string(data),
	)
}

// LoadCursor 从 system_configs 读取扫描游标；无记录则返回零值（从头开始扫描）
func (r *Repo) LoadCursor() Cursor {
	var val string
	err := r.db.Raw("SELECT value FROM system_configs WHERE key = 'key_activity.scan_cursor'").Scan(&val).Error
	if err != nil || val == "" {
		return Cursor{}
	}
	var cur Cursor
	if err := json.Unmarshal([]byte(val), &cur); err != nil {
		return Cursor{}
	}
	return cur
}

// GetConfigValue 读 system_configs 某项值（供 Scanner/Cron 使用）
func (r *Repo) GetConfigValue(key string) string {
	var val string
	_ = r.db.Raw("SELECT value FROM system_configs WHERE key = ?", key).Scan(&val).Error
	return val
}

// SetConfigValue 写 system_configs 某项值（供 Bootstrap 标记完成状态）
func (r *Repo) SetConfigValue(key, value string) {
	r.db.Exec(
		"INSERT INTO system_configs (key, value) VALUES (?, ?) "+
			"ON CONFLICT (key) DO UPDATE SET value = ?",
		key, value, value,
	)
}

// FindKeyByID 通过 key_id（text）查找 api_keys 记录，返回 name 和 user_id（uuid.UUID）
func (r *Repo) FindKeyByID(keyIDStr string) (keyName string, userID uuid.UUID, err error) {
	parsedID, err := uuid.Parse(keyIDStr)
	if err != nil {
		return "", uuid.UUID{}, err
	}
	var row struct {
		Name   string
		UserID uuid.UUID
	}
	err = r.db.Raw("SELECT name, user_id FROM api_keys WHERE id = ?", parsedID).Scan(&row).Error
	if err != nil {
		return "", uuid.UUID{}, err
	}
	return row.Name, row.UserID, nil
}

// BatchFetchCallLogsForBootstrap 冷启动回灌：扫描近 N 天 call_logs，分批返回 CallLogRow
// 调用方可指定 since 时间；每批 batchSize 行，按 ts 升序；返回空切片表示已全部处理完
func (r *Repo) BatchFetchCallLogsForBootstrap(since time.Time, batchSize int, lastCursor Cursor) ([]CallLogRow, error) {
	var rows []struct {
		RequestID string
		TS        time.Time
		KeyID     string
		KeyName   string
		UserID    string
		UserAgent string
		IPAddress string
	}

	query := `
		SELECT request_id, ts,
		       COALESCE(key_id, '') AS key_id,
		       COALESCE(user_id, '') AS user_id,
		       COALESCE(user_agent, '') AS user_agent,
		       COALESCE(ip_address, '') AS ip_address,
		       '' AS key_name
		FROM call_logs
		WHERE ts >= ? AND key_id IS NOT NULL AND key_id != ''
	`
	args := []interface{}{since}

	// 断点续扫：如果 lastCursor 非空，从上次位置继续
	if !lastCursor.TS.IsZero() {
		query += " AND (ts > ? OR (ts = ? AND request_id > ?))"
		args = append(args, lastCursor.TS, lastCursor.TS, lastCursor.RequestID)
	}

	query += " ORDER BY ts ASC, request_id ASC LIMIT ?"
	args = append(args, batchSize)

	err := r.db.Raw(query, args...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]CallLogRow, len(rows))
	for i, row := range rows {
		result[i] = CallLogRow{
			RequestID: row.RequestID,
			TS:        row.TS,
			KeyID:     row.KeyID,
			UserID:    row.UserID,
			UserAgent: row.UserAgent,
			IPAddress: row.IPAddress,
		}
	}
	return result, nil
}

// FetchRecentHourlyStats 冷启动 EWMA 初始化：获取近 N 天每 Key 每小时调用数直方图
// 返回 map[keyID] → map[hourBucket] → callCount
func (r *Repo) FetchRecentHourlyStats(days int) (map[string]map[time.Time]float64, error) {
	type row struct {
		KeyID     string
		HourBucket time.Time
		Cnt       float64
	}
	var rows []row
	err := r.db.Raw(`
		SELECT key_id,
		       date_trunc('hour', ts AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai' AS hour_bucket,
		       COUNT(*)::float8 AS cnt
		FROM call_logs
		WHERE ts > now() - make_interval(days => ?)
		  AND key_id IS NOT NULL AND key_id != ''
		GROUP BY key_id, hour_bucket
		ORDER BY key_id, hour_bucket
	`, days).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[time.Time]float64)
	for _, row := range rows {
		if _, ok := result[row.KeyID]; !ok {
			result[row.KeyID] = make(map[time.Time]float64)
		}
		result[row.KeyID][row.HourBucket] = row.Cnt
	}
	return result, nil
}

// FetchLastSeenPerKey 冷启动初始化 last_seen_at：获取每个 key 最近一次调用时间
func (r *Repo) FetchLastSeenPerKey(since time.Time) (map[string]time.Time, error) {
	type row struct {
		KeyID    string
		LastSeen time.Time
	}
	var rows []row
	err := r.db.Raw(`
		SELECT key_id, MAX(ts) AS last_seen
		FROM call_logs
		WHERE ts >= ?
		  AND key_id IS NOT NULL AND key_id != ''
		GROUP BY key_id
	`, since).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		result[row.KeyID] = row.LastSeen
	}
	return result, nil
}
