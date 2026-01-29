package keyactivity_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	ka "github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/google/uuid"
)

const (
	testTestKey    = "test-key-001"
	testTestKey2   = "test-key-002"
	testTestUserID = "11111111-1111-1111-1111-111111111111"
)

// ═══════════════════════════════════════════════════════════════════════════════
// Fake Implementations (手写 fake，不用 mock 框架)
// ═══════════════════════════════════════════════════════════════════════════════

// fakeKeyActivityRepo 模拟 KeyActivityRepo 接口，用于测试 Scanner 与 Bootstrap 行为
type fakeKeyActivityRepo struct {
	seenRows    map[string]*ka.KeySourceSeen
	stateMap    map[string]*ka.KeyActivityState
	alerts      []*ka.KeyActivityAlert
	alertDedup  map[string]bool
	callLogs    []ka.CallLogRow
	cursor      ka.Cursor
	configVals  map[string]string
	touchCounts map[string]int

	bootstrapDoneVal string
	bootstrapRows    []ka.CallLogRow
	lastSeenMap      map[string]time.Time
	hourlyStats      map[string]map[time.Time]float64

	insertAlertErr error
}

func newFakeRepo() *fakeKeyActivityRepo {
	return &fakeKeyActivityRepo{
		seenRows:    make(map[string]*ka.KeySourceSeen),
		stateMap:    make(map[string]*ka.KeyActivityState),
		alertDedup:  make(map[string]bool),
		configVals:  make(map[string]string),
		touchCounts: make(map[string]int),
	}
}

func (r *fakeKeyActivityRepo) seenKey(keyID, sourceFP string) string {
	return keyID + "|" + sourceFP
}

func (r *fakeKeyActivityRepo) alertKey(keyID, eventType, dedupKey string) string {
	return keyID + "|" + eventType + "|" + dedupKey
}

func (r *fakeKeyActivityRepo) UpsertSeen(row *ka.KeySourceSeen) error {
	key := r.seenKey(row.KeyID, row.SourceFP)
	if _, exists := r.seenRows[key]; !exists {
		r.seenRows[key] = row
	}
	return nil
}

func (r *fakeKeyActivityRepo) BatchUpsertSeen(rows []ka.KeySourceSeen) error {
	for _, row := range rows {
		key := r.seenKey(row.KeyID, row.SourceFP)
		if _, exists := r.seenRows[key]; !exists {
			rCopy := row
			r.seenRows[key] = &rCopy
		}
	}
	return nil
}

func (r *fakeKeyActivityRepo) SeenExists(keyID, sourceFP string) (bool, error) {
	return r.seenRows[r.seenKey(keyID, sourceFP)] != nil, nil
}

func (r *fakeKeyActivityRepo) AnySeen(keyID string) (bool, error) {
	for _, row := range r.seenRows {
		if row.KeyID == keyID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeKeyActivityRepo) TouchSeen(keyID, sourceFP string, ts time.Time) error {
	r.touchCounts[r.seenKey(keyID, sourceFP)]++
	return nil
}

func (r *fakeKeyActivityRepo) GetOrInitState(keyID string) (*ka.KeyActivityState, error) {
	if st, ok := r.stateMap[keyID]; ok {
		cp := *st
		return &cp, nil
	}
	now := time.Now()
	st := &ka.KeyActivityState{KeyID: keyID, UpdatedAt: now}
	r.stateMap[keyID] = st
	cp := *st
	return &cp, nil
}

func (r *fakeKeyActivityRepo) UpdateState(state *ka.KeyActivityState) error {
	cp := *state
	r.stateMap[state.KeyID] = &cp
	return nil
}

func (r *fakeKeyActivityRepo) InsertAlert(alert *ka.KeyActivityAlert) (bool, error) {
	if r.insertAlertErr != nil {
		return false, r.insertAlertErr
	}
	key := r.alertKey(alert.KeyID, alert.EventType, alert.DedupKey)
	if r.alertDedup[key] {
		return false, nil
	}
	r.alertDedup[key] = true
	cp := *alert
	r.alerts = append(r.alerts, &cp)
	return true, nil
}

func (r *fakeKeyActivityRepo) FetchCallLogs(cursor ka.Cursor, batch int) ([]ka.CallLogRow, error) {
	var result []ka.CallLogRow
	for _, row := range r.callLogs {
		if row.TS.After(cursor.TS) || (row.TS.Equal(cursor.TS) && row.RequestID > cursor.RequestID) {
			result = append(result, row)
			if len(result) >= batch {
				break
			}
		}
	}
	return result, nil
}

func (r *fakeKeyActivityRepo) SaveCursor(cur ka.Cursor) {
	r.cursor = cur
}

func (r *fakeKeyActivityRepo) LoadCursor() ka.Cursor {
	return r.cursor
}

func (r *fakeKeyActivityRepo) GetConfigValue(key string) string {
	return r.configVals[key]
}

func (r *fakeKeyActivityRepo) SetConfigValue(key, value string) {
	r.configVals[key] = value
}

func (r *fakeKeyActivityRepo) FetchLastSeenPerKey(since time.Time) (map[string]time.Time, error) {
	return r.lastSeenMap, nil
}

func (r *fakeKeyActivityRepo) FetchRecentHourlyStats(days int) (map[string]map[time.Time]float64, error) {
	return r.hourlyStats, nil
}

func (r *fakeKeyActivityRepo) BatchFetchCallLogsForBootstrap(since time.Time, batchSize int, lastCursor ka.Cursor) ([]ka.CallLogRow, error) {
	if !lastCursor.TS.IsZero() {
		return nil, nil
	}
	return r.bootstrapRows, nil
}

// fakeConfigProvider 模拟固定配置
type fakeConfigProvider struct {
	cfg *ka.KeyActivityConfig
}

func (c *fakeConfigProvider) GetKeyActivityConfig(_ context.Context) *ka.KeyActivityConfig {
	return c.cfg
}

// enabledConfig 返回默认开启配置（DormantDays=30, Alpha=0.3, K=3, MinBaseline=20, MinDev=5）
func enabledConfig() *fakeConfigProvider {
	return &fakeConfigProvider{cfg: &ka.KeyActivityConfig{
		Enabled:         true,
		IPAsSource:      true,
		DormantDays:     30,
		Alpha:           0.3,
		K:               3.0,
		MinBaseline:     20,
		MinDev:          5,
		ScanIntervalSec: 60,
	}}
}

// fakeNotifier 记录所有发出的通知
type fakeNotifier struct {
	calls []notifCall
}

type notifCall struct {
	userID  uuid.UUID
	title   string
	body    string
	level   string
	refType string
	refID   string
}

func (n *fakeNotifier) Notify(userUUID uuid.UUID, title, body, level, refType, refID string) error {
	n.calls = append(n.calls, notifCall{userUUID, title, body, level, refType, refID})
	return nil
}

// fakeKeyLookup 模拟 Key 查询：返回固定 name 和 UUID
type fakeKeyLookup struct {
	name   string
	userID uuid.UUID
	err    error
}

func defaultKeyLookup() *fakeKeyLookup {
	return &fakeKeyLookup{
		name:   "test-key-name",
		userID: uuid.MustParse(testTestUserID),
	}
}

func (k *fakeKeyLookup) FindKeyByID(keyID string) (string, uuid.UUID, error) {
	return k.name, k.userID, k.err
}

// ═══════════════════════════════════════════════════════════════════════════════
// Helper 函数
// ═══════════════════════════════════════════════════════════════════════════════

// pastHourRow 构造一条发生在 2 小时前的 call_log（确保在已完成小时内）
func pastHourRow(keyID, userAgent, ipAddress, requestID string) ka.CallLogRow {
	return ka.CallLogRow{
		RequestID: requestID,
		TS:        time.Now().Add(-2 * time.Hour),
		KeyID:     keyID,
		UserID:    testTestUserID,
		UserAgent: userAgent,
		IPAddress: ipAddress,
	}
}

// countAlertsByType 统计指定事件类型的 alert 数量
func countAlertsByType(alerts []*ka.KeyActivityAlert, eventType string) int {
	count := 0
	for _, a := range alerts {
		if a.EventType == eventType {
			count++
		}
	}
	return count
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：IsEnabled
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_IsEnabled_True(t *testing.T) {
	// 测试目的：配置 enabled=true 时，Scanner 应报告已启用
	repo := newFakeRepo()
	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	if !scanner.IsEnabled(context.Background()) {
		t.Error("Scanner.IsEnabled() should be true when config.Enabled=true")
	}
}

func TestScanner_IsEnabled_False(t *testing.T) {
	// 测试目的：配置 enabled=false 时，Scanner 应报告未启用
	repo := newFakeRepo()
	cfgProv := &fakeConfigProvider{cfg: &ka.KeyActivityConfig{Enabled: false}}
	scanner := ka.NewScanner(repo, &fakeNotifier{}, cfgProv, defaultKeyLookup())
	if scanner.IsEnabled(context.Background()) {
		t.Error("Scanner.IsEnabled() should be false when config.Enabled=false")
	}
}

func TestScanner_IsEnabled_NilConfig(t *testing.T) {
	// 测试目的：配置为 nil 时，Scanner 应返回 false（不 panic）
	repo := newFakeRepo()
	cfgProv := &fakeConfigProvider{cfg: nil}
	scanner := ka.NewScanner(repo, &fakeNotifier{}, cfgProv, defaultKeyLookup())
	if scanner.IsEnabled(context.Background()) {
		t.Error("Scanner.IsEnabled() should be false when config is nil")
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：E2 首次使用
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_E2_FirstUse_CreatesAlert(t *testing.T) {
	// 测试目的：全新 Key 首次出现时触发 E2（first_use），
	// 写入一条 alert 并发出一条通知；不触发 E1（new_source）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-001"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}

	if count := countAlertsByType(repo.alerts, ka.EventTypeFirstUse); count != 1 {
		t.Errorf("expected exactly 1 first_use alert, got %d", count)
	}
	if count := countAlertsByType(repo.alerts, ka.EventTypeNewSource); count != 0 {
		t.Errorf("expected 0 new_source alerts for first use, got %d", count)
	}
	if len(notif.calls) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notif.calls))
	}
	if len(notif.calls) > 0 && notif.calls[0].title != "Key 首次使用" {
		t.Errorf("expected title 'Key 首次使用', got %q", notif.calls[0].title)
	}
}

func TestScanner_E2_FirstUse_SeenRecordCreated(t *testing.T) {
	// 测试目的：E2 处理后 key_source_seen 应有记录
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-002"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	anySeen, _ := repo.AnySeen(testTestKey)
	if !anySeen {
		t.Error("E2 should create a key_source_seen record")
	}
}

func TestScanner_E2_SkipsEmptyKeyID(t *testing.T) {
	// 测试目的：key_id 为空字符串的行应跳过评估，不触发任何 alert
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow("", "curl/8.0", "10.0.0.1", "req-empty"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if len(repo.alerts) != 0 {
		t.Errorf("empty key_id row should not trigger alerts, got %d", len(repo.alerts))
	}
}

func TestScanner_E2_UpdatesLastSeenAt(t *testing.T) {
	// 测试目的：E2 处理后 state.last_seen_at 应被更新
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-003"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	st := repo.stateMap[testTestKey]
	if st == nil || st.LastSeenAt == nil {
		t.Error("E2 should update state.last_seen_at")
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：E1 新来源
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_E1_NewSource_CreatesAlert(t *testing.T) {
	// 测试目的：已知 Key 被新来源（不同 UA）使用时触发 E1（new_source），
	// 写入一条 alert，通知标题为"Key 新设备使用提醒"
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	knownFP := ka.SourceFP("curl/8.0", "10.0.0.1", true)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "python-requests/2.31.0", "10.0.0.2", "req-e1"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}

	if count := countAlertsByType(repo.alerts, ka.EventTypeNewSource); count != 1 {
		t.Errorf("expected 1 new_source alert, got %d", count)
	}
	if len(notif.calls) != 1 || notif.calls[0].title != "Key 新设备使用提醒" {
		title := ""
		if len(notif.calls) > 0 {
			title = notif.calls[0].title
		}
		t.Errorf("expected 'Key 新设备使用提醒', got %q", title)
	}
}

func TestScanner_E1_KnownSource_NoAlert(t *testing.T) {
	// 测试目的：已知来源（同 UA+IP）再次出现时不触发任何 alert，仅更新 TouchSeen
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	knownFP := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, ip, "req-known"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if len(repo.alerts) != 0 {
		t.Errorf("known source should not trigger alerts, got %d", len(repo.alerts))
	}
	touchKey := repo.seenKey(testTestKey, knownFP)
	if repo.touchCounts[touchKey] != 1 {
		t.Errorf("TouchSeen should be called once, got %d", repo.touchCounts[touchKey])
	}
}

func TestScanner_E2_MultipleUnknownKeys_OnlyTriggersOnce(t *testing.T) {
	// 测试目的：同一 Key 多行调用在一轮扫描中，E2 只触发一次（去重）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	pastTime := time.Now().Add(-2 * time.Hour)
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-multi-1", TS: pastTime, KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
		{RequestID: "req-multi-2", TS: pastTime.Add(time.Second), KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
		{RequestID: "req-multi-3", TS: pastTime.Add(2 * time.Second), KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeFirstUse); count != 1 {
		t.Errorf("E2 should trigger exactly once for multiple calls to same new key, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：E3 唤醒
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_E3_WakeupAfterDormantPeriod(t *testing.T) {
	// 测试目的：Key 超过 DormantDays（30天）未使用后重新出现，触发 E3（wakeup）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().AddDate(0, 0, -40), LastSeen: time.Now().AddDate(0, 0, -40), HitCount: 1,
	}

	dormantTime := time.Now().AddDate(0, 0, -35)
	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, LastSeenAt: &dormantTime, BaselineReady: false, Samples: 1,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, ip, "req-e3"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeWakeup); count != 1 {
		t.Errorf("expected 1 wakeup alert after 35 days dormant, got %d", count)
	}
	if len(notif.calls) != 1 || notif.calls[0].title != "Key 重新启用提醒" {
		title := ""
		if len(notif.calls) > 0 {
			title = notif.calls[0].title
		}
		t.Errorf("expected 'Key 重新启用提醒', got %q", title)
	}
}

func TestScanner_E3_WithinDormantThreshold_NoAlert(t *testing.T) {
	// 测试目的：Key 29 天前使用过（< DormantDays=30），再次出现不触发 E3
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().AddDate(0, 0, -30), LastSeen: time.Now().AddDate(0, 0, -30), HitCount: 1,
	}

	recentTime := time.Now().AddDate(0, 0, -29)
	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, LastSeenAt: &recentTime, BaselineReady: false, Samples: 1,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, ip, "req-e3-near"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeWakeup); count != 0 {
		t.Errorf("29-day dormant key should NOT trigger wakeup (< 30 days), got %d", count)
	}
}

func TestScanner_E3_NilLastSeenAt_NoAlert(t *testing.T) {
	// 测试目的：state.LastSeenAt 为 nil 时（首次见到该 Key 但未通过 E2 路径），不触发 E3
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	// 已知来源已存在，不触发 E1/E2；但 state.LastSeenAt 为 nil
	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}
	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, LastSeenAt: nil, BaselineReady: false,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, ip, "req-e3-nil"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeWakeup); count != 0 {
		t.Errorf("nil LastSeenAt should NOT trigger wakeup, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：E4 用量激增
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_E4_Spike_TriggersAlert(t *testing.T) {
	// 测试目的：EWMA 基线 mean=20/dev=5（threshold=20+3*5=35），
	// 已过去小时有 100 次调用（远超阈值），触发 E4（usage_spike）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 20, EwmaDev: 5,
		BaselineReady: true, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 100; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-spike-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID:     testTestKey, UserID: testTestUserID, UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 1 {
		t.Errorf("expected 1 usage_spike alert (100 > threshold 35), got %d", count)
	}
	if len(notif.calls) != 1 {
		t.Errorf("expected 1 notification for spike, got %d", len(notif.calls))
	}
	if len(notif.calls) > 0 && notif.calls[0].title != "Key 用量异常提醒" {
		t.Errorf("expected 'Key 用量异常提醒', got %q", notif.calls[0].title)
	}
}

func TestScanner_E4_NotTriggered_RegularUsage(t *testing.T) {
	// 测试目的：EWMA 基线 mean=100/dev=20（threshold=100+3*20=160），
	// 50 次调用远低于阈值，不触发 E4
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 100, EwmaDev: 20,
		BaselineReady: true, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 50; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-regular-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 0 {
		t.Errorf("regular usage (50 vs threshold 160) should NOT trigger spike, got %d", count)
	}
}

func TestScanner_E4_LowBaseline_Ignored(t *testing.T) {
	// 测试目的：EWMA 基线 mean=5（< MinBaseline=20），无论调用量多高都不评估 E4
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 5, EwmaDev: 1,
		BaselineReady: true, Samples: 5,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 500; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-low-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Millisecond),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 0 {
		t.Errorf("low baseline (mean=5 < MinBaseline=20) should NOT trigger spike, got %d", count)
	}
}

func TestScanner_E4_BaselineNotReady_Skipped(t *testing.T) {
	// 测试目的：BaselineReady=false 时 E4 跳过评估，即使调用量很高
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 100, EwmaDev: 10,
		BaselineReady: false, Samples: 3,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 200; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-nobl-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 0 {
		t.Errorf("E4 should be skipped when BaselineReady=false, got %d alerts", count)
	}
}

func TestScanner_E4_CurrentHourExcluded(t *testing.T) {
	// 测试目的：当前未完成小时内的调用不计入 E4 评估（防止小时未结束就判定激增）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 20, EwmaDev: 5,
		BaselineReady: true, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	now := time.Now()
	var logs []ka.CallLogRow
	for i := 0; i < 200; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-curr-%d", i),
			TS:        now.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 0 {
		t.Errorf("current (incomplete) hour should NOT trigger spike, got %d", count)
	}
}

func TestScanner_E4_MinDevOverridesZero(t *testing.T) {
	// 测试目的：dev 极小（0.1）时 minDev=10 兜底，threshold=1+3*10=31，
	// 50 次调用超过阈值，触发 E4
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 1, EwmaDev: 0.1,
		BaselineReady: false, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 50; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-mindev-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	// 注意：BaselineReady=false，E4 不评估
	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 0 {
		t.Errorf("E4 with BaselineReady=false should be skipped, got %d", count)
	}
}

func TestScanner_E4_Dedup_SameHourTwice(t *testing.T) {
	// 测试目的：同一小时桶重复扫描只产生一条 E4 alert（hour bucket 去重）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 25, EwmaDev: 5,
		BaselineReady: true, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 100; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-dedup-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())

	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("first ScanOnce: %v", err)
	}
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("second ScanOnce: %v", err)
	}

	if count := countAlertsByType(repo.alerts, ka.EventTypeSpike); count != 1 {
		t.Errorf("E4 with same hour bucket should deduplicate to 1, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：去重（Dedup）
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_Dedup_FirstUseAcrossTwoScans(t *testing.T) {
	// 测试目的：同一新 Key 在两轮扫描中各出现一次，E2 只在第一轮触发（唯一键去重）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	now := time.Now()
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-d1", TS: now.Add(-2 * time.Hour), KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
		{RequestID: "req-d2", TS: now.Add(-1 * time.Hour), KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeFirstUse); count != 1 {
		t.Errorf("E2 dedup across 2 scans should yield exactly 1 alert, got %d", count)
	}
}

func TestScanner_Dedup_SameNewSourceTwice(t *testing.T) {
	// 测试目的：同一新来源重复出现，E1 只触发一次
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	knownFP := ka.SourceFP("curl/8.0", "10.0.0.1", true)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-ds1", TS: pastTime, KeyID: testTestKey, UserID: testTestUserID, UserAgent: "python", IPAddress: "10.0.0.2"},
		{RequestID: "req-ds2", TS: pastTime.Add(time.Second), KeyID: testTestKey, UserID: testTestUserID, UserAgent: "python", IPAddress: "10.0.0.2"},
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeNewSource); count != 1 {
		t.Errorf("E1: same new source in same scan should dedup to 1, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：Shadow 模式（enabled=false）
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_ShadowMode_AlertWrittenButNoNotification(t *testing.T) {
	// 测试目的：enabled=false 时 alert 仍写入数据库（落库），但不发送通知
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-shadow"),
	}

	disabledCfg := &fakeConfigProvider{cfg: &ka.KeyActivityConfig{
		Enabled: false, IPAsSource: true, DormantDays: 30, ScanIntervalSec: 60,
	}}
	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, disabledCfg, defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if len(repo.alerts) == 0 {
		t.Error("shadow mode: alert should still be written to DB")
	}
	if len(notif.calls) != 0 {
		t.Errorf("shadow mode: notification should NOT be sent, got %d", len(notif.calls))
	}
}

func TestScanner_ShadowMode_E4_AlertButNoNotification(t *testing.T) {
	// 测试目的：enabled=false 时 E4 spike 仍落库但不发通知
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 20, EwmaDev: 5,
		BaselineReady: true, Samples: 10,
	}

	ua := "curl/8.0"
	ip := "10.0.0.1"
	fp := ka.SourceFP(ua, ip, true)
	repo.seenRows[repo.seenKey(testTestKey, fp)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: fp, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 100; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-shdw4-%d", i), TS: pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID, UserAgent: ua, IPAddress: ip,
		})
	}
	repo.callLogs = logs

	disabledCfg := &fakeConfigProvider{cfg: &ka.KeyActivityConfig{
		Enabled: false, IPAsSource: true, DormantDays: 30, ScanIntervalSec: 60,
	}}
	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, disabledCfg, defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	spikeAlerts := countAlertsByType(repo.alerts, ka.EventTypeSpike)
	if spikeAlerts != 1 {
		t.Errorf("shadow mode E4: should still write 1 spike alert, got %d", spikeAlerts)
	}
	if len(notif.calls) != 0 {
		t.Errorf("shadow mode E4: no notifications expected, got %d", len(notif.calls))
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：空批次处理
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_EmptyBatch_NoSideEffects(t *testing.T) {
	// 测试目的：FetchCallLogs 返回空时 ScanOnce 直接返回，不写任何 alert、不发通知
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(time.Hour)}
	repo.callLogs = nil

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce on empty batch should not error: %v", err)
	}
	if len(repo.alerts) != 0 {
		t.Errorf("empty batch should produce 0 alerts, got %d", len(repo.alerts))
	}
	if len(notif.calls) != 0 {
		t.Errorf("empty batch should send 0 notifications, got %d", len(notif.calls))
	}
}

func TestScanner_EmptyBatch_DoesNotCrashWithNilRepo(t *testing.T) {
	// 测试目的：空批次 + 配置正常应无 panic
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(time.Hour)}
	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：IP 开关行为
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_IPAsSourceTrue_NewIPTriggersE1(t *testing.T) {
	// 测试目的：IPAsSource=true 时，同 UA 换 IP 视为新来源，触发 E1
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	ua := "curl/8.0"
	knownFP := ka.SourceFP(ua, "10.0.0.1", true)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, "10.0.0.2", "req-ip-true"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeNewSource); count != 1 {
		t.Errorf("IPAsSource=true, new IP should trigger E1, got %d", count)
	}
}

func TestScanner_IPAsSourceFalse_SameUANewIPNoE1(t *testing.T) {
	// 测试目的：IPAsSource=false 时，同 UA 换 IP 不视为新来源，不触发 E1
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	ua := "curl/8.0"
	knownFP := ka.SourceFP(ua, "", false)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().Add(-time.Hour), LastSeen: time.Now().Add(-time.Hour), HitCount: 1,
	}

	cfgNoIP := &fakeConfigProvider{cfg: &ka.KeyActivityConfig{
		Enabled: true, IPAsSource: false, DormantDays: 30, ScanIntervalSec: 60,
	}}

	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, ua, "10.0.0.99", "req-ip-false"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, cfgNoIP, defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if count := countAlertsByType(repo.alerts, ka.EventTypeNewSource); count != 0 {
		t.Errorf("IPAsSource=false, same UA with new IP should NOT trigger E1, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：KeyLookup 异常处理
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_KeyLookupError_AlertsStillCreated(t *testing.T) {
	// 测试目的：KeyLookup.FindKeyByID 返回 error 时，alert 仍正常创建（key_name 为空），
	// 通知仍能发送（user_id 来自 call_log，非 KeyLookup）
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-kl-err"),
	}

	notif := &fakeNotifier{}
	errLookup := &fakeKeyLookup{name: "", userID: uuid.UUID{}, err: fmt.Errorf("db error")}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), errLookup)
	scanner.ScanOnce(context.Background())

	if len(repo.alerts) != 1 {
		t.Errorf("alert should still be created when KeyLookup fails, got %d", len(repo.alerts))
	}
	if len(notif.calls) != 1 {
		t.Errorf("notification should still be sent when KeyLookup fails, got %d", len(notif.calls))
	}
}

func TestScanner_InvalidUserID_NotificationSkipped(t *testing.T) {
	// 测试目的：call_log 的 user_id 无法解析为 UUID 时，通知跳过（不报错），alert 仍落库
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-bad-uid", TS: time.Now().Add(-2 * time.Hour),
			KeyID: testTestKey, UserID: "not-a-uuid", UserAgent: "curl", IPAddress: "10.0.0.1"},
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if len(repo.alerts) != 1 {
		t.Errorf("alert should still be written for invalid user_id, got %d", len(repo.alerts))
	}
	if len(notif.calls) != 0 {
		t.Errorf("invalid user_id should skip notification, got %d", len(notif.calls))
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：多 Key 混合场景
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_MultipleKeys_IndependentAlerts(t *testing.T) {
	// 测试目的：一轮扫描包含两个不同 Key，各自独立触发 E2（first_use），共 2 条 alert
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	pastTime := time.Now().Add(-2 * time.Hour)
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-k1", TS: pastTime, KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
		{RequestID: "req-k2", TS: pastTime.Add(time.Second), KeyID: testTestKey2, UserID: testTestUserID, UserAgent: "python", IPAddress: "10.0.0.2"},
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	totalFirstUse := countAlertsByType(repo.alerts, ka.EventTypeFirstUse)
	if totalFirstUse != 2 {
		t.Errorf("expected 2 first_use alerts (one per key), got %d", totalFirstUse)
	}
	if len(notif.calls) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(notif.calls))
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Bootstrap 测试
// ═══════════════════════════════════════════════════════════════════════════════

func TestBootstrap_FullFlow(t *testing.T) {
	// 测试目的：Bootstrap 三阶段全流程：
	// 1) 历史 call_logs 回灌 key_source_seen
	// 2) 初始化 EWMA 基线（baseline_ready=true）
	// 3) 写 last_seen_at + bootstrap_done=true
	repo := newFakeRepo()
	now := time.Now()

	logTime := now.AddDate(0, 0, -3)
	repo.bootstrapRows = []ka.CallLogRow{
		{RequestID: "req-b1", TS: logTime, KeyID: "key-a", UserAgent: "curl/8.0", IPAddress: "10.0.0.1"},
		{RequestID: "req-b2", TS: logTime.Add(time.Second), KeyID: "key-a", UserAgent: "curl/8.0", IPAddress: "10.0.0.1"},
		{RequestID: "req-b3", TS: logTime.Add(2 * time.Second), KeyID: "key-b", UserAgent: "python", IPAddress: "10.0.0.2"},
	}

	hour1 := now.AddDate(0, 0, -3).Truncate(time.Hour).Add(10 * time.Hour)
	hour2 := hour1.Add(time.Hour)
	repo.hourlyStats = map[string]map[time.Time]float64{
		"key-a": {hour1: 50.0, hour2: 70.0},
		"key-b": {hour1: 10.0},
	}
	repo.lastSeenMap = map[string]time.Time{
		"key-a": now.AddDate(0, 0, -1),
		"key-b": now.AddDate(0, 0, -2),
	}

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	if err := ka.RunBootstrap(context.Background(), repo, cfg); err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}

	if len(repo.seenRows) < 2 {
		t.Errorf("expected at least 2 seen records (key-a + key-b), got %d", len(repo.seenRows))
	}
	stA := repo.stateMap["key-a"]
	if stA == nil || !stA.BaselineReady {
		t.Error("key-a state should have BaselineReady=true after Bootstrap")
	}
	if stA != nil && stA.LastSeenAt == nil {
		t.Error("key-a last_seen_at should be set")
	}
	stB := repo.stateMap["key-b"]
	if stB == nil || !stB.BaselineReady {
		t.Error("key-b state should have BaselineReady=true after Bootstrap")
	}
	if repo.configVals["key_activity.bootstrap_done"] != "true" {
		t.Error("bootstrap_done should be 'true'")
	}
	if repo.cursor.TS.IsZero() {
		t.Error("cursor should be advanced to current time")
	}
}

func TestBootstrap_NoAlertsWritten(t *testing.T) {
	// 测试目的：Bootstrap 全程不写任何 key_activity_alert
	repo := newFakeRepo()
	repo.bootstrapRows = []ka.CallLogRow{
		{RequestID: "req-ba1", TS: time.Now().AddDate(0, 0, -1), KeyID: "key-x",
			UserAgent: "curl", IPAddress: "10.0.0.1"},
	}
	repo.hourlyStats = map[string]map[time.Time]float64{}
	repo.lastSeenMap = map[string]time.Time{}

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	if err := ka.RunBootstrap(context.Background(), repo, cfg); err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}
	if len(repo.alerts) != 0 {
		t.Errorf("Bootstrap should NOT write any alerts, got %d", len(repo.alerts))
	}
}

func TestBootstrap_NoHistoricalData(t *testing.T) {
	// 测试目的：无历史数据时 Bootstrap 仍正常完成并标记 bootstrap_done
	repo := newFakeRepo()
	repo.bootstrapRows = nil
	repo.hourlyStats = nil
	repo.lastSeenMap = nil

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	if err := ka.RunBootstrap(context.Background(), repo, cfg); err != nil {
		t.Fatalf("RunBootstrap with no data should not error: %v", err)
	}
	if repo.configVals["key_activity.bootstrap_done"] != "true" {
		t.Error("bootstrap_done should be 'true' even with no data")
	}
}

func TestBootstrap_ContextCancelled(t *testing.T) {
	// 测试目的：ctx 取消时 Bootstrap 立即返回错误（不继续处理）
	repo := newFakeRepo()

	// 大量数据，确保在第一次 BatchFetch 后检查 ctx
	var manyLogs []ka.CallLogRow
	for i := 0; i < 100; i++ {
		manyLogs = append(manyLogs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-cancel-%d", i),
			TS:        time.Now().AddDate(0, 0, -1),
			KeyID:     "key-cancel", UserAgent: "curl", IPAddress: "10.0.0.1",
		})
	}
	repo.bootstrapRows = manyLogs

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ka.RunBootstrap(ctx, repo, cfg)
	if err == nil {
		t.Error("Bootstrap with cancelled context should return error")
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Scanner 测试：Bootstrap 集成
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_FirstScanTriggersBootstrap(t *testing.T) {
	// 测试目的：bootstrap_done=false 时首次 ScanOnce 自动触发 Bootstrap，
	// 然后第二轮转为正常扫描
	repo := newFakeRepo()
	repo.bootstrapRows = nil
	repo.lastSeenMap = nil
	repo.hourlyStats = nil

	if err := func() error {
		scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
		return scanner.ScanOnce(context.Background())
	}(); err != nil {
		t.Fatalf("first ScanOnce (triggers bootstrap): %v", err)
	}
	if repo.configVals["key_activity.bootstrap_done"] != "true" {
		t.Fatal("bootstrap should complete on first ScanOnce")
	}

	// Bootstrap 把游标推到当前时间，需回拨才能扫到旧数据
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl", "10.0.0.1", "req-boot-scan"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("second ScanOnce (normal scan): %v", err)
	}
	if len(repo.alerts) == 0 {
		t.Error("normal scan after bootstrap should create alert for new key")
	}
}

func TestScanner_SecondScanSkipsBootstrap(t *testing.T) {
	// 测试目的：bootstrap_done=true 时 ScanOnce 跳过 Bootstrap 直接进入正常扫描
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl", "10.0.0.1", "req-skip-bs"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	if repo.configVals["key_activity.bootstrap_done"] != "true" {
		t.Error("bootstrap_done should remain 'true'")
	}
	if len(repo.alerts) != 1 {
		t.Errorf("expected 1 alert from normal scan, got %d", len(repo.alerts))
	}
}

func TestBootstrap_CursorAdvanced(t *testing.T) {
	// 测试目的：Bootstrap 完成后游标推至当前时间（不回看历史）
	repo := newFakeRepo()
	repo.bootstrapRows = nil
	repo.hourlyStats = nil
	repo.lastSeenMap = nil

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	beforeBootstrap := time.Now().Add(-time.Second)
	if err := ka.RunBootstrap(context.Background(), repo, cfg); err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}

	if repo.cursor.TS.Before(beforeBootstrap) {
		t.Errorf("cursor should be at or after bootstrap start; got %v", repo.cursor.TS)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Cron 测试
// ═══════════════════════════════════════════════════════════════════════════════

func TestCron_RunOnce_DelegatesToScanner(t *testing.T) {
	// 测试目的：Cron.RunOnce 委托给 Scanner.ScanOnce，空批次不报错
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(time.Hour)}
	repo.callLogs = nil

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	cron := ka.NewCron(scanner, enabledConfig())

	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("Cron.RunOnce() on empty batch should not error: %v", err)
	}
}

func TestCron_RunOnce_WithCallLogs(t *testing.T) {
	// 测试目的：Cron.RunOnce 有数据时正常执行扫描并产生 alert
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "curl/8.0", "10.0.0.1", "req-cron-data"),
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	cron := ka.NewCron(scanner, enabledConfig())

	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("Cron.RunOnce(): %v", err)
	}
	if len(repo.alerts) != 1 {
		t.Errorf("Cron.RunOnce should produce 1 alert for new key, got %d", len(repo.alerts))
	}
}

func TestCron_StartAndCancel(t *testing.T) {
	// 测试目的：Cron.Start 启动后台 goroutine，ctx 取消后应优雅退出
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(time.Hour)}
	repo.callLogs = nil

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	cfgProv := &fakeConfigProvider{cfg: &ka.KeyActivityConfig{
		Enabled: true, ScanIntervalSec: 1,
	}}
	cron := ka.NewCron(scanner, cfgProv)

	ctx, cancel := context.WithCancel(context.Background())
	cron.Start(ctx)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// ═══════════════════════════════════════════════════════════════════════════════
// Helper 函数测试（补充 FormatNotification / ParseDormantDays / ParseSpikeDetail）
// ═══════════════════════════════════════════════════════════════════════════════

func TestFormatNotification_AllEventTypes(t *testing.T) {
	// 测试目的：四种事件类型均有正确的标题和非空正文
	ts := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		eventType string
		detail    string
		wantTitle string
		wantPart  string
		description string
	}{
		{
			name: "E1 新来源", eventType: ka.EventTypeNewSource,
			detail: "", wantTitle: "Key 新设备使用提醒",
			wantPart: "新设备", description: "E1 应提示新设备使用",
		},
		{
			name: "E2 首次使用", eventType: ka.EventTypeFirstUse,
			detail: "", wantTitle: "Key 首次使用",
			wantPart: "首次", description: "E2 应提示 Key 首次使用",
		},
		{
			name: "E3 唤醒", eventType: ka.EventTypeWakeup,
			detail: `{"dormant_days":60}`, wantTitle: "Key 重新启用提醒",
			wantPart: "60 天", description: "E3 应包含休眠天数",
		},
		{
			name: "E4 激增", eventType: ka.EventTypeSpike,
			detail: `{"count":500,"mean":50.0}`, wantTitle: "Key 用量异常提醒",
			wantPart: "500", description: "E4 应包含调用次数",
		},
		{
			name: "未知事件类型", eventType: "unknown_event",
			detail: "", wantTitle: "Key 活动提醒",
			wantPart: "", description: "未知事件类型应使用通用标题",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := ka.FormatNotification(tt.eventType, "TestKey", "curl", "1.2.3.4", ts, tt.detail)
			if title != tt.wantTitle {
				t.Errorf("title: got %q, want %q", title, tt.wantTitle)
			}
			if tt.wantPart != "" && !contains(body, tt.wantPart) {
				t.Errorf("body should contain %q: %s", tt.wantPart, body)
			}
			if !contains(body, "TestKey") {
				t.Errorf("body should contain key name 'TestKey': %s", body)
			}
		})
	}
}

func TestParseDormantDays_TableDriven(t *testing.T) {
	// 测试目的：验证各种 JSON 格式下 dormant_days 解析的正确性和健壮性
	tests := []struct {
		name   string
		detail string
		want   int
	}{
		{"正常 JSON", `{"dormant_days":45}`, 45},
		{"零值", `{"dormant_days":0}`, 0},
		{"空对象", `{}`, 0},
		{"无效 JSON", `invalid`, 0},
		{"空字符串", ``, 0},
		{"其他字段", `{"other":1}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ka.ParseDormantDays(tt.detail); got != tt.want {
				t.Errorf("ParseDormantDays(%q) = %d, want %d", tt.detail, got, tt.want)
			}
		})
	}
}

func TestParseSpikeDetail_TableDriven(t *testing.T) {
	// 测试目的：验证 count 和 mean 从 spike detail JSON 中正确解析
	tests := []struct {
		name   string
		detail string
		count  int
		mean   float64
	}{
		{"完整 JSON", `{"count":100,"mean":30.5}`, 100, 30.5},
		{"空字符串", ``, 0, 0},
		{"无效 JSON", `{bad`, 0, 0},
		{"只有 count", `{"count":99}`, 99, 0},
		{"只有 mean", `{"mean":77.7}`, 0, 77.7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, m := ka.ParseSpikeDetail(tt.detail)
			if c != tt.count || m != tt.mean {
				t.Errorf("ParseSpikeDetail(%q) = (%d, %.1f), want (%d, %.1f)",
					tt.detail, c, m, tt.count, tt.mean)
			}
		})
	}
}

func TestEventLevel_AllTypes(t *testing.T) {
	// 测试目的：E2（first_use）为 info，其余为 warning
	cases := map[string]string{
		ka.EventTypeFirstUse:  "info",
		ka.EventTypeNewSource: "warning",
		ka.EventTypeWakeup:   "warning",
		ka.EventTypeSpike:    "warning",
		"unknown":            "warning",
	}
	for event, want := range cases {
		got := ka.EventLevel(event)
		if got != want {
			t.Errorf("EventLevel(%q) = %q, want %q", event, got, want)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// 综合场景：E2 + E3 + E4 同时触发
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_Comprehensive_E2AndE4BothTrigger(t *testing.T) {
	// 测试目的：一轮扫描中 E2（first_use）和 E4（spike）同时触发，
	// 验证两类事件互不干扰
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, EwmaMean: 20, EwmaDev: 5,
		BaselineReady: true, Samples: 10,
	}
	// E4 需要 known source 或 unknown source（都会触发 E2 或 E1）
	// 不预设 seen → E2 触发；同批 call_logs 进入已完成小时 → E4 触发

	pastTime := time.Now().Add(-2 * time.Hour)
	var logs []ka.CallLogRow
	for i := 0; i < 100; i++ {
		logs = append(logs, ka.CallLogRow{
			RequestID: fmt.Sprintf("req-comp-%d", i),
			TS:        pastTime.Add(time.Duration(i) * time.Second),
			KeyID: testTestKey, UserID: testTestUserID,
			UserAgent: "curl", IPAddress: "10.0.0.1",
		})
	}
	repo.callLogs = logs

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	e2Count := countAlertsByType(repo.alerts, ka.EventTypeFirstUse)
	e4Count := countAlertsByType(repo.alerts, ka.EventTypeSpike)
	if e2Count != 1 {
		t.Errorf("expected 1 first_use alert, got %d", e2Count)
	}
	if e4Count != 1 {
		t.Errorf("expected 1 spike alert (100 calls, threshold=35), got %d", e4Count)
	}
	if len(notif.calls) != 2 {
		t.Errorf("expected 2 notifications (E2 + E4), got %d", len(notif.calls))
	}
}

func TestScanner_Comprehensive_E1AndE3BothTrigger(t *testing.T) {
	// 测试目的：已知来源 + 休眠唤醒同时发生，E1（新来源）和 E3（唤醒）各触发一条
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}

	// 已知来源：curl from 10.0.0.1（30天前）
	knownFP := ka.SourceFP("curl/8.0", "10.0.0.1", true)
	repo.seenRows[repo.seenKey(testTestKey, knownFP)] = &ka.KeySourceSeen{
		KeyID: testTestKey, SourceFP: knownFP, UAFp: "ua-curl",
		FirstSeen: time.Now().AddDate(0, 0, -40), LastSeen: time.Now().AddDate(0, 0, -40), HitCount: 1,
	}

	dormantTime := time.Now().AddDate(0, 0, -35)
	repo.stateMap[testTestKey] = &ka.KeyActivityState{
		KeyID: testTestKey, LastSeenAt: &dormantTime, BaselineReady: false, Samples: 1,
	}

	// 新来源：python from 10.0.0.2（同时满足 E1 和 E3）
	repo.callLogs = []ka.CallLogRow{
		pastHourRow(testTestKey, "python/3.0", "10.0.0.2", "req-e1e3"),
	}

	notif := &fakeNotifier{}
	scanner := ka.NewScanner(repo, notif, enabledConfig(), defaultKeyLookup())
	scanner.ScanOnce(context.Background())

	e1Count := countAlertsByType(repo.alerts, ka.EventTypeNewSource)
	e3Count := countAlertsByType(repo.alerts, ka.EventTypeWakeup)
	if e1Count != 1 {
		t.Errorf("E1: new source should trigger, got %d", e1Count)
	}
	if e3Count != 1 {
		t.Errorf("E3: wakeup after 35 days should trigger, got %d", e3Count)
	}
	if len(notif.calls) != 2 {
		t.Errorf("expected 2 notifications (E1 + E3), got %d", len(notif.calls))
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// insertAlert 失败处理
// ═══════════════════════════════════════════════════════════════════════════════

func TestScanner_InsertAlertError_ContinuesScanning(t *testing.T) {
	// 测试目的：InsertAlert 返回 error 时扫描不中断，后续行继续处理
	repo := newFakeRepo()
	repo.configVals["key_activity.bootstrap_done"] = "true"
	repo.cursor = ka.Cursor{TS: time.Now().Add(-24 * time.Hour)}
	repo.insertAlertErr = fmt.Errorf("db write failed")

	pastTime := time.Now().Add(-2 * time.Hour)
	repo.callLogs = []ka.CallLogRow{
		{RequestID: "req-err1", TS: pastTime, KeyID: testTestKey, UserID: testTestUserID, UserAgent: "curl", IPAddress: "10.0.0.1"},
	}

	scanner := ka.NewScanner(repo, &fakeNotifier{}, enabledConfig(), defaultKeyLookup())
	err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce should not return error on insert failure: %v", err)
	}
	if len(repo.alerts) != 0 {
		t.Errorf("insert error should prevent alert from being written, got %d", len(repo.alerts))
	}
}
