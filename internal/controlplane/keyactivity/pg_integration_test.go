//go:build integration

// PG 集成测试：验证 Bootstrap 的 FetchRecentHourlyStats（AT TIME ZONE 是 PG-only 语法）
// 以及全链路 Scanner E2E。
//
// 运行前提：
//   - Postgres 已跑通（192.0.2.10:5432，由 config/dev.yaml 或 DATABASE_URL 指定）
//   - Atlas 迁移已执行（三张 key_activity_* 表已建好）
//
// 运行方式：
//   DATABASE_URL="host=<host> port=5432 user=llm_gateway password=<口令> dbname=llm_gateway sslmode=disable" \
//   go test ./internal/controlplane/keyactivity/... -tags=integration -v -run PG -timeout 60s -count=1

package keyactivity_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	ka "github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL 未设置，跳过 PG 集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PG 失败: %v", err)
	}
	return db
}

func TestPG_Repo_FetchRecentHourlyStats(t *testing.T) {
	// 验证 FetchRecentHourlyStats 的 AT TIME ZONE 'Asia/Shanghai' 在真实 PG 上可执行
	db := openPG(t)
	repo := ka.NewRepo(db)

	stats, err := repo.FetchRecentHourlyStats(14)
	if err != nil {
		t.Fatalf("FetchRecentHourlyStats should work on PG with AT TIME ZONE: %v", err)
	}
	// 返回类型正确
	t.Logf("FetchRecentHourlyStats(14d) returned %d keys", len(stats))
	for keyID, hours := range stats {
		if len(hours) == 0 {
			continue
		}
		t.Logf("key=%s has %d hourly buckets", keyID, len(hours))
		break
	}
}

func TestPG_Repo_FetchLastSeenPerKey(t *testing.T) {
	// 验证 FetchLastSeenPerKey 在真实 PG 上可执行
	db := openPG(t)
	repo := ka.NewRepo(db)

	since := time.Now().AddDate(0, 0, -90)
	seen, err := repo.FetchLastSeenPerKey(since)
	if err != nil {
		t.Fatalf("FetchLastSeenPerKey should work on PG: %v", err)
	}
	t.Logf("FetchLastSeenPerKey(90d) returned %d keys", len(seen))
}

func TestPG_Bootstrap_FullFlow(t *testing.T) {
	// 端到端：Bootstrap → 写 seen 表 → 初始化 EWMA → 推进游标 → 标记 done
	// 这是 Bootstrap 测试中唯一能真实走 AT TIME ZONE 路径的测试。
	db := openPG(t)
	repo := ka.NewRepo(db)

	// 清理本轮测试残留
	t.Cleanup(func() {
		// 清理测试写入的数据（key_id 以 pg-test- 开头）
		db.Exec("DELETE FROM key_activity_alert WHERE key_id LIKE 'pg-test-%'")
		db.Exec("DELETE FROM key_activity_state WHERE key_id LIKE 'pg-test-%'")
		db.Exec("DELETE FROM key_source_seen WHERE key_id LIKE 'pg-test-%'")
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.bootstrap_done'")
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.scan_cursor'")
	})

	// 用临时 call_logs 行做测试
	testKey := "pg-test-bootstrap-" + uuid.New().String()[:8]
	now := time.Now().UTC()
	// 写入近期 call_logs（供 backfillSourceSeen）
	for d := 7; d >= 1; d-- {
		ts := now.AddDate(0, 0, -d)
		for i := 0; i < 3; i++ {
			rid := "rid-boot-" + uuid.New().String()[:8]
			db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, key_name, user_agent, ip_address, model, group_name, provider, dept_id, dept_name, studio_id, studio_name, routed_model)
			         VALUES (?, ?, ?, 'BootKey', 'curl/8.0', '10.0.0.1', 'test-model', 'test-group', 'test-provider', '', '', '', '', '')`, rid, ts, testKey)
		}
		t.Cleanup(func() {
			db.Exec("DELETE FROM call_logs WHERE key_id = ?", testKey)
		})
	}

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := ka.RunBootstrap(ctx, repo, cfg)
	if err != nil {
		t.Fatalf("RunBootstrap on real PG: %v", err)
	}

	// 验证：seen 表有数据
	var seenCnt int64
	db.Raw("SELECT COUNT(*) FROM key_source_seen WHERE key_id = ?", testKey).Scan(&seenCnt)
	if seenCnt == 0 {
		t.Error("bootstrap should populate key_source_seen for test key")
	}
	t.Logf("key_source_seen for %s: %d rows", testKey, seenCnt)

	// 验证：bootstrap_done 已标记
	if repo.GetConfigValue("key_activity.bootstrap_done") != "true" {
		t.Error("bootstrap_done should be 'true'")
	}

	// 验证：游标已推进
	cur := repo.LoadCursor()
	if cur.TS.IsZero() {
		t.Error("cursor should be advanced")
	}
	t.Logf("cursor advanced to: %s", cur.TS.Format(time.RFC3339))

	// 验证：全程没有 alert
	var alertCnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ?", testKey).Scan(&alertCnt)
	if alertCnt != 0 {
		t.Errorf("bootstrap should NOT create alerts, got %d", alertCnt)
	}
}

func TestPG_Bootstrap_Idempotent(t *testing.T) {
	// 重复 Bootstrap 幂等：第二次跑，seen 行数不增加
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-idem-" + uuid.New().String()[:8]
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		rid := "rid-idem-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, key_name, user_agent, ip_address, model, group_name, provider, dept_id, dept_name, studio_id, studio_name, routed_model)
		         VALUES (?, ?, ?, 'IdemKey', 'curl/8.0', '10.0.0.1', 'm', 'g', 'p', '', '', '', '', '')`, rid, now.AddDate(0, 0, -1), testKey)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM call_logs WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM key_source_seen WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM key_activity_state WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.bootstrap_done'")
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.scan_cursor'")
	})

	cfg := &ka.KeyActivityConfig{BootstrapDays: 90, IPAsSource: true}
	ctx := context.Background()

	_ = ka.RunBootstrap(ctx, repo, cfg)
	var cnt1 int64
	db.Raw("SELECT COUNT(*) FROM key_source_seen WHERE key_id = ?", testKey).Scan(&cnt1)

	// 清理 bootstrap_done 让它可以重跑
	db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.bootstrap_done'")
	_ = ka.RunBootstrap(ctx, repo, cfg)
	var cnt2 int64
	db.Raw("SELECT COUNT(*) FROM key_source_seen WHERE key_id = ?", testKey).Scan(&cnt2)

	if cnt1 != cnt2 {
		t.Errorf("bootstrap idempotent failed: seen rows %d → %d", cnt1, cnt2)
	}
}

func TestPG_Scanner_E1_OnRealDB(t *testing.T) {
	// 端到端 E1：真实 call_logs → Scanner → 唯一键去重 → alert 落库
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e1-" + uuid.New().String()[:8]
	now := time.Now().UTC()

	// 清理
	t.Cleanup(func() {
		db.Exec("DELETE FROM key_activity_alert WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM key_activity_state WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM key_source_seen WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM call_logs WHERE key_id = ?", testKey)
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.bootstrap_done'")
		db.Exec("DELETE FROM system_configs WHERE key = 'key_activity.scan_cursor'")
	})

	// 已见来源
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now, LastSeen: now, HitCount: 1,
	})
	// 新来源进入 call_logs
	rid := "rid-e1-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, dept_id, dept_name, studio_id, studio_name, routed_model)
	         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '', '', '')`, rid, now.Add(time.Second), testKey, testUserID, "python", "10.0.0.1", "test-model", "test-group", "test-provider", "test-key")

	// 设置 bootstrap_done
	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	// 把游标设到当前时间之前，才能扫到新数据
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	// fakeConfig + fakeNotifier 保持测试隔离
	cfgProv := &staticCfg{enabled: true}
	notif := &recordingNotif{}
	kl := &staticKeyLookup{}

	scanner := ka.NewScanner(repo, notif, cfgProv, kl)
	err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce on real PG: %v", err)
	}

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'new_source'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("E1 should produce exactly 1 alert on PG, got %d", cnt)
	}
	// 通知发出
	if len(notif.calls) != 1 {
		t.Errorf("notification should be sent, got %d", len(notif.calls))
	}
}

// ── E2 / E3 / E4 / 去重 / Shadow / IP 开关 PG 集成测试 ──

func TestPG_Scanner_E2_FirstUse(t *testing.T) {
	// 验证：全新 Key 首用触发 E2，且 dedup_key="first"，只写一条 alert
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e2-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()

	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	rid := "rid-e2-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, dept_id, dept_name, studio_id, studio_name, routed_model)
	         VALUES (?, ?, ?, ?, ?, ?, 'm', 'g', 'p', 'K', '', '', '', '', '')`,
		rid, now.Add(time.Second), testKey, testUserID, "curl", "10.0.0.1")

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	notif := &recordingNotif{}
	scanner := ka.NewScanner(repo, notif, &staticCfg{enabled: true}, &staticKeyLookup{})
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'first_use' AND dedup_key = 'first'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("E2: expected exactly 1 first_use alert with dedup='first', got %d", cnt)
	}
	if len(notif.calls) != 1 || notif.calls[0].title != "Key 首次使用" {
		t.Errorf("E2 notification wrong: %+v", notif.calls)
	}
}

func TestPG_Scanner_E2_DoesNotTriggerE1(t *testing.T) {
	// 验证：E2 触发时不会同时触发 E1（互斥逻辑）
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e2-noe1-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-e2x-"+uuid.New().String()[:8], now.Add(time.Second), testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var newSourceCnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'new_source'", testKey).Scan(&newSourceCnt)
	if newSourceCnt != 0 {
		t.Errorf("E2 should NOT trigger E1 simultaneously, got %d new_source alerts", newSourceCnt)
	}
}

func TestPG_Scanner_E3_WakeupAfter30Days(t *testing.T) {
	// 验证：Key 超过 30 天未使用后突然出现调用，触发 E3
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e3-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	// 已见来源 + state：last_seen_at 在 35 天前
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.AddDate(0, 0, -40), LastSeen: now.AddDate(0, 0, -40), HitCount: 1,
	})
	lastSeen := now.AddDate(0, 0, -35)
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeen, UpdatedAt: lastSeen,
	})

	// 新调用出现
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-e3-"+uuid.New().String()[:8], now, testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	notif := &recordingNotif{}
	scanner := ka.NewScanner(repo, notif, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("E3: expected 1 wakeup alert for 35-day dormant key, got %d", cnt)
	}
}

func TestPG_Scanner_E3_NoWakeupWithin30Days(t *testing.T) {
	// 验证：Key 29 天前使用过，不应触发 E3
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e3-no-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.AddDate(0, 0, -30), LastSeen: now.AddDate(0, 0, -30), HitCount: 1,
	})
	lastSeen := now.AddDate(0, 0, -29)
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeen, UpdatedAt: lastSeen,
	})

	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-e3n-"+uuid.New().String()[:8], now, testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("E3: 29-day dormant key should NOT trigger wakeup, got %d", cnt)
	}
}

func TestPG_Scanner_E4_Spike(t *testing.T) {
	// 验证：用量超过 EWMA 阈值时触发 E4
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	// 已有 baseline：mean=20 dev=5
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 20, EwmaDev: 5, BaselineReady: true, Samples: 10, UpdatedAt: now,
	})
	// threshold = 20 + 3*max(5,5) = 35，构造 100 次远超阈值

	// 构造已结束小时的数据（必须用已结束的整点小时）
	pastCompletedHour := now.Truncate(time.Hour).Add(-2 * time.Hour)
	for i := 0; i < 100; i++ {
		rid := fmt.Sprintf("rid-e4-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, pastCompletedHour.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})

	notif := &recordingNotif{}
	scanner := ka.NewScanner(repo, notif, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt < 1 {
		t.Errorf("E4: 100 calls with baseline mean=20/dev=5 should trigger spike, got %d alerts", cnt)
	}
	if len(notif.calls) == 0 {
		t.Error("E4: notification should be sent")
	}
}

func TestPG_Scanner_E4_NotTriggeredByRegularUsage(t *testing.T) {
	// 验证：稳定用量不触发 E4
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-stable-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 100, EwmaDev: 20, BaselineReady: true, Samples: 20, UpdatedAt: now,
	})
	// threshold = 100 + 3*20 = 160，构造 50 次远低于阈值

	pastCompletedHour := now.Truncate(time.Hour).Add(-2 * time.Hour)
	for i := 0; i < 50; i++ {
		rid := fmt.Sprintf("rid-e4s-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, pastCompletedHour.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("E4: regular usage (50 vs baseline 100) should NOT trigger spike, got %d", cnt)
	}
}

func TestPG_Scanner_E4_LowFreqKeyIgnored(t *testing.T) {
	// 验证：baseline 低于 min_baseline=20 的 Key 不触发 E4
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-low-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	// mean=5 < min_baseline=20
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 5, EwmaDev: 1, BaselineReady: true, Samples: 10, UpdatedAt: now,
	})

	pastCompletedHour := now.Truncate(time.Hour).Add(-2 * time.Hour)
	// 15 次，相对 baseline=5 算高了，但 baseline 太低不应触评
	for i := 0; i < 15; i++ {
		rid := fmt.Sprintf("rid-e4l-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, pastCompletedHour.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("E4: low-freq key (mean=5) should NOT trigger spike, got %d", cnt)
	}
}

func TestPG_Scanner_Dedup_RealDB(t *testing.T) {
	// 验证 PG 唯一键去重：同一事件重复扫描，alert 数不翻倍
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-dedup-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	rid1 := "rid-dedup-" + uuid.New().String()[:8]
	rid2 := "rid-dedup2-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		rid1, now.Add(time.Second), testKey, testUserID)
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		rid2, now.Add(2*time.Second), testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	cfgProv := &staticCfg{enabled: true}
	scanner := ka.NewScanner(repo, &recordingNotif{}, cfgProv, &staticKeyLookup{})

	_ = scanner.ScanOnce(context.Background())
	// 重复扫
	_ = scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ?", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("dedup: expected exactly 1 alert after 2 scans, got %d", cnt)
	}
}

func TestPG_Scanner_ShadowMode(t *testing.T) {
	// 验证 enabled=false 时：PG 上 alert 落库但不发通知
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-shadow-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-shdw-"+uuid.New().String()[:8], now.Add(time.Second), testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	notif := &recordingNotif{}
	scanner := ka.NewScanner(repo, notif, &staticCfg{enabled: false}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var alertCnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ?", testKey).Scan(&alertCnt)
	if alertCnt == 0 {
		t.Error("shadow mode: alert should still be written to PG")
	}
	if len(notif.calls) != 0 {
		t.Errorf("shadow mode: notification should NOT be sent, got %d", len(notif.calls))
	}
}

func TestPG_Scanner_IPAsSourceToggle(t *testing.T) {
	// 验证 ip_as_source=true 时换 IP 触发 E1；ip_as_source=false 时同 UA 换 IP 不触发
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-ip-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	// Case A: ip_as_source=true，已知来源 (curl, 10.0.0.1)，新来源 (curl, 10.0.0.2)
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Hour), HitCount: 1,
	})
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.2', 'm', 'g', 'p', 'K', '')`,
		"rid-ipA-"+uuid.New().String()[:8], now.Add(time.Second), testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	notif := &recordingNotif{}
	cfgTrue := &staticCfg{enabled: true} // IPAsSource: true (default)
	scanner := ka.NewScanner(repo, notif, cfgTrue, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cntA int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'new_source'", testKey).Scan(&cntA)
	if cntA != 1 {
		t.Errorf("ip_as_source=true: new IP should trigger E1, got %d", cntA)
	}

	// Case B: ip_as_source=false，已知来源只按 UA 指纹，同 UA 不同 IP 不触发
	testKey2 := "pg-test-ipB-" + uuid.New().String()[:8]
	t.Cleanup(func() { pgCleanup(t, db, testKey2) })

	// 仅设 ua-fp 指纹（无 IP 组合）
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey2, SourceFP: ka.SourceFP("curl", "", false),
		UAFp: "ua-x", FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Hour), HitCount: 1,
	})
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.99', 'm', 'g', 'p', 'K', '')`,
		"rid-ipB-"+uuid.New().String()[:8], now.Add(time.Second), testKey2, testUserID)

	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})
	cfgFalse := &staticCfgNoIP{enabled: true} // IPAsSource: false
	scanner2 := ka.NewScanner(repo, &recordingNotif{}, cfgFalse, &staticKeyLookup{})
	scanner2.ScanOnce(context.Background())

	var cntB int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'new_source'", testKey2).Scan(&cntB)
	if cntB != 0 {
		t.Errorf("ip_as_source=false: same UA new IP should NOT trigger E1, got %d", cntB)
	}
}

func TestPG_Scanner_E3_SameDayDedup(t *testing.T) {
	// 验证：E3 同一天多次调用不重复触发（dedup_key=唤醒日"2006-01-02"）
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e3-dedup-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	// last_seen_at = 35 天前
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.AddDate(0, 0, -40), LastSeen: now.AddDate(0, 0, -40), HitCount: 1,
	})
	lastSeen := now.AddDate(0, 0, -35)
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeen, UpdatedAt: lastSeen,
	})

	// 今天三条 call_log（不同时间但同一天）
	for i := 0; i < 3; i++ {
		rid := fmt.Sprintf("rid-e3d-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, now.Add(time.Duration(i)*time.Minute), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("E3 same-day dedup: 3 calls on same day should produce exactly 1 wakeup alert, got %d", cnt)
	}
}

func TestPG_Scanner_E3_CrossDayCanBeTriggered(t *testing.T) {
	// 验证：E3 跨日可再次触发（dedup_key 变为第二天日期）
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e3-cross-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	// 预置：35 天前活跃
	lastSeen := now.AddDate(0, 0, -35)
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: lastSeen, LastSeen: lastSeen, HitCount: 1,
	})
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeen, UpdatedAt: lastSeen,
	})

	// 插入"昨天"的 call_log
	yesterday := now.AddDate(0, 0, -1).Truncate(24*time.Hour).Add(12 * time.Hour)
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-cd1-"+uuid.New().String()[:8], yesterday, testKey, testUserID)
	// 插入"今天"的 call_log（last_seen 更新为昨天后，距今 < 30 天，不应触发；
	// 但 last_seen_at 在 evalRow 中是扫描行推进更新的，所以今天调用不会触发 E3）
	// 重新设计：让 last_seen_at 一直停留在 35 天前，模拟"两次唤醒跨天"
	// 方法：state 只初始化一次，不在两次扫描之间更新 last_seen_at → 让 evalRow 每次都看到 35 天前的值

	today := now.Truncate(24 * time.Hour).Add(12 * time.Hour)
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-cd2-"+uuid.New().String()[:8], today, testKey, testUserID)

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	// 用很早的 cursor 确保两天都能扫到
	repo.SaveCursor(ka.Cursor{TS: yesterday.Add(-2 * time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt)
	// evalRow 会更新 last_seen_at，第二条调用（今天）时 last_seen_at 已被第一条（昨天）更新为昨天
	// 距今只 1 天，不会再次触发 E3，所以应该是 1
	// 但注意：两条记录都在一轮扫里，evalRow 顺序处理，
	// 处理完昨天后 last_seen_at = yesterday（30 天内），今天的行距昨天只 1 天 → 不触发
	// 所以这里只能得到 1 个 wakeup，无法测"跨日再触发"
	// 要真的测跨日再触发，需要两轮扫描（中间手动改 state.last_seen 回 35 天前）
	if cnt != 1 {
		t.Errorf("E3 with cross-day calls in one scan: expected 1 wakeup (2nd call too close), got %d", cnt)
	}

	// 模拟两轮扫描：恢复 last_seen 为 30+ 天前，第二轮应再触发
	db.Exec("DELETE FROM key_activity_alert WHERE key_id = ?", testKey)
	lastSeenFar := now.AddDate(0, 0, -40)
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeenFar, UpdatedAt: lastSeenFar,
	})
	repo.SaveCursor(ka.Cursor{TS: yesterday.Add(-time.Minute)})
	scanner.ScanOnce(context.Background())

	// 第一轮：昨天那条触发一次（dedup=昨天日期）
	var cnt1 int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt1)

	// 手动把 last_seen 再调回 40 天前（模拟又休眠了很久）
	lastSeenFar2 := now.AddDate(0, 0, -40)
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, BaselineReady: true, LastSeenAt: &lastSeenFar2, UpdatedAt: lastSeenFar2,
	})

	// 插入另一天的 call_log（明天？不行，ts 在现在之后扫不到；用历史第三天）
	day3 := yesterday.AddDate(0, 0, -2)
	db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
	         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
		"rid-cd3-"+uuid.New().String()[:8], day3, testKey, testUserID)
	repo.SaveCursor(ka.Cursor{TS: day3.Add(-time.Minute)})
	scanner.ScanOnce(context.Background())

	var cnt2 int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'wakeup'", testKey).Scan(&cnt2)
	if cnt2 != 2 {
		t.Errorf("E3 cross-day: two scans with far-apart last_seen should produce 2 wakeup alerts, got %d", cnt2)
	}
}

func TestPG_Scanner_E4_HourBucketDedup(t *testing.T) {
	// 验证：同一小时桶内重复扫描只产生一条 E4 alert
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-hdedup-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	// baseline: mean=25 dev=5 > min_baseline=20 → 阈值 = 25 + 3*5 = 40，50 calls > 40 → 触发
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 25, EwmaDev: 5, BaselineReady: true, Samples: 10, UpdatedAt: now,
	})

	pastCompletedHour := now.Truncate(time.Hour).Add(-2 * time.Hour)

	// 50 calls in the hour
	for i := 0; i < 50; i++ {
		rid := fmt.Sprintf("rid-hd1-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, pastCompletedHour.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})

	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	// 第二轮：重置游标让同一批数据重扫
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("E4 hour-bucket dedup: same hour should produce exactly 1 alert across 2 scans, got %d", cnt)
	}
}

func TestPG_Scanner_E4_EWMANotPulled(t *testing.T) {
	// 验证：EWMA 先判后更——激增值本身不会用来判定自己
	// 具体：mean=20 dev=5, spike=100 → threshold 用 20+3*5=35（旧基线）判断
	// 后 EWMA 更新：new_mean = 0.3*100 + 0.7*20 = 44；PG 上 state.ewma_mean 应该是 44
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-pull-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 20, EwmaDev: 5, BaselineReady: true, Samples: 10, UpdatedAt: now,
	})

	pastCompletedHour := now.Truncate(time.Hour).Add(-2 * time.Hour)
	for i := 0; i < 100; i++ {
		rid := fmt.Sprintf("rid-ewmapull-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, pastCompletedHour.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-24 * time.Hour)})
	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	// 验证触发 spike
	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("E4 spike expected, got %d alerts", cnt)
	}

	// 验证 state.ewma_mean 被更新为 44（0.3*100+0.7*20），而非保留 20 或被拉到 100
	var newMean float64
	db.Raw("SELECT ewma_mean FROM key_activity_state WHERE key_id = ?", testKey).Scan(&newMean)
	if newMean < 40 || newMean > 50 {
		t.Errorf("EWMA mean should be ~44 after spike of 100 with old mean=20 (alpha=0.3); got %v", newMean)
	}
	if newMean > 90 {
		t.Errorf("EWMA mean should NOT have been pulled up toward 100, got %v — judge-before-update broken", newMean)
	}
}

func TestPG_Scanner_E4_SkipsCurrentHour(t *testing.T) {
	// 验证：当前未完成小时的调用不会被计入 E4 评估
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-e4-curh-" + uuid.New().String()[:8]
	testUserID := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: ka.SourceFP("curl", "10.0.0.1", true),
		UAFp: "ua-x", FirstSeen: now.Add(-24*time.Hour), LastSeen: now.Add(-24*time.Hour), HitCount: 1,
	})
	_ = repo.UpdateState(&ka.KeyActivityState{
		KeyID: testKey, EwmaMean: 10, EwmaDev: 2, BaselineReady: true, Samples: 10, UpdatedAt: now,
	})

	// 全部在当前小时内（未完结）
	for i := 0; i < 100; i++ {
		rid := fmt.Sprintf("rid-curh-%s-%d", uuid.New().String()[:6], i)
		db.Exec(`INSERT INTO call_logs (request_id, ts, key_id, user_id, user_agent, ip_address, model, group_name, provider, key_name, routed_model)
		         VALUES (?, ?, ?, ?, 'curl', '10.0.0.1', 'm', 'g', 'p', 'K', '')`,
			rid, now.Add(time.Duration(i)*time.Second), testKey, testUserID)
	}

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	repo.SaveCursor(ka.Cursor{TS: now.Add(-time.Hour)})
	scanner := ka.NewScanner(repo, &recordingNotif{}, &staticCfg{enabled: true}, &staticKeyLookup{})
	scanner.ScanOnce(context.Background())

	var cnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ? AND event_type = 'usage_spike'", testKey).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("E4: current (incomplete) hour should NOT trigger spike, got %d", cnt)
	}
}

func TestPG_Scanner_EmptyBatchDoesNothing(t *testing.T) {
	// 验证：FetchCallLogs 返回空时，ScanOnce 直接退出无副作用
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-empty-" + uuid.New().String()[:8]
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	// cursor = now + 1h → 不会扫到任何数据
	repo.SaveCursor(ka.Cursor{TS: time.Now().UTC().Add(time.Hour)})

	notif := &recordingNotif{}
	scanner := ka.NewScanner(repo, notif, &staticCfg{enabled: true}, &staticKeyLookup{})
	err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce on empty batch should not error: %v", err)
	}
	if len(notif.calls) != 0 {
		t.Errorf("no notifications should be sent on empty batch, got %d", len(notif.calls))
	}
	var alertCnt int64
	db.Raw("SELECT COUNT(*) FROM key_activity_alert WHERE key_id = ?", testKey).Scan(&alertCnt)
	if alertCnt != 0 {
		t.Errorf("empty batch should produce 0 alerts, got %d", alertCnt)
	}
}

func TestPG_Repo_UpsertSeen_AccumulatesHitCount(t *testing.T) {
	// 验证：UpsertSeen 幂等 + hit_count 累加
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-ups-" + uuid.New().String()[:8]
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	fp := ka.SourceFP("curl", "10.0.0.1", true)
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: fp, UAFp: "ua-x",
		FirstSeen: now, LastSeen: now, HitCount: 1,
	})
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: fp, UAFp: "ua-x",
		FirstSeen: now, LastSeen: now, HitCount: 1,
	})
	_ = repo.UpsertSeen(&ka.KeySourceSeen{
		KeyID: testKey, SourceFP: fp, UAFp: "ua-x",
		FirstSeen: now, LastSeen: now, HitCount: 1,
	})

	var rows int64
	db.Raw("SELECT COUNT(*) FROM key_source_seen WHERE key_id = ?", testKey).Scan(&rows)
	if rows != 1 {
		t.Errorf("expected 1 row after 3 upserts, got %d", rows)
	}
	var hitCount int64
	db.Raw("SELECT hit_count FROM key_source_seen WHERE key_id = ?", testKey).Scan(&hitCount)
	if hitCount != 3 {
		t.Errorf("expected hit_count=3 after 3 upserts, got %d", hitCount)
	}
}

func TestPG_Repo_InsertAlert_DedupUniqueKey(t *testing.T) {
	// 验证 key_activity_alert 的 (key_id, event_type, dedup_key) 唯一键去重
	db := openPG(t)
	repo := ka.NewRepo(db)

	testKey := "pg-test-alertdedup-" + uuid.New().String()[:8]
	now := time.Now().UTC()
	t.Cleanup(func() { pgCleanup(t, db, testKey) })

	alert1 := &ka.KeyActivityAlert{
		KeyID: testKey, UserID: "11111111-1111-1111-1111-111111111111",
		EventType: "new_source", DedupKey: "fp-xxx",
		UARaw: "curl", IP: "10.0.0.1", NotifiedAt: now,
	}
	new1, err := repo.InsertAlert(alert1)
	if err != nil || !new1 {
		t.Fatalf("first InsertAlert should be new: new=%v err=%v", new1, err)
	}

	alert2 := &ka.KeyActivityAlert{
		KeyID: testKey, UserID: "11111111-1111-1111-1111-111111111111",
		EventType: "new_source", DedupKey: "fp-xxx",
		UARaw: "curl", IP: "10.0.0.1", NotifiedAt: now,
	}
	new2, err := repo.InsertAlert(alert2)
	if err != nil {
		t.Fatalf("second InsertAlert: %v", err)
	}
	if new2 {
		t.Error("second InsertAlert with same dedup key should NOT be new")
	}

	// 不同 dedup_key 应成功
	alert3 := &ka.KeyActivityAlert{
		KeyID: testKey, UserID: "11111111-1111-1111-1111-111111111111",
		EventType: "new_source", DedupKey: "fp-yyy", NotifiedAt: now,
	}
	new3, err := repo.InsertAlert(alert3)
	if err != nil || !new3 {
		t.Errorf("different dedup key should be new: new=%v err=%v", new3, err)
	}
}

func TestPG_Repo_SaveLoadCursor(t *testing.T) {
	// 验证游标读写（系统级配置项）
	db := openPG(t)
	repo := ka.NewRepo(db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'kaCursorTest.%'")
	})

	// 用 key_activity 自己的游标键（覆盖真实路径）
	repo.SaveCursor(ka.Cursor{TS: time.Now().UTC().Truncate(time.Second), RequestID: "req-abc"})
	loaded := repo.LoadCursor()
	if loaded.RequestID != "req-abc" {
		t.Errorf("cursor roundtrip failed: reqID=%q", loaded.RequestID)
	}
}

// ── helpers ───────────────────────────────

// pgCleanup 清理测试写入的数据
func pgCleanup(t *testing.T, db *gorm.DB, testKey string) {
	t.Helper()
	db.Exec("DELETE FROM key_activity_alert WHERE key_id = ?", testKey)
	db.Exec("DELETE FROM key_activity_state WHERE key_id = ?", testKey)
	db.Exec("DELETE FROM key_source_seen WHERE key_id = ?", testKey)
	db.Exec("DELETE FROM call_logs WHERE key_id = ?", testKey)
	// system_configs 全局键由各测试自己决定是否清理
}

// staticCfg 固定配置（integration 测试用，IPAsSource=true）
type staticCfg struct{ enabled bool }

func (c *staticCfg) GetKeyActivityConfig(_ context.Context) *ka.KeyActivityConfig {
	return &ka.KeyActivityConfig{
		Enabled:         c.enabled,
		IPAsSource:      true,
		DormantDays:     30,
		Alpha:           0.3,
		K:               3.0,
		MinBaseline:     20,
		MinDev:          5,
		BootstrapDays:   90,
		ScanIntervalSec: 60,
	}
}

// staticCfgNoIP 固定配置（integration 测试用，IPAsSource=false）
type staticCfgNoIP struct{ enabled bool }

func (c *staticCfgNoIP) GetKeyActivityConfig(_ context.Context) *ka.KeyActivityConfig {
	return &ka.KeyActivityConfig{
		Enabled:         c.enabled,
		IPAsSource:      false,
		DormantDays:     30,
		Alpha:           0.3,
		K:               3.0,
		MinBaseline:     20,
		MinDev:          5,
		BootstrapDays:   90,
		ScanIntervalSec: 60,
	}
}

// recordingNotif 记录通知（integration 测试用）
type recordingNotif struct {
	calls []struct{ title, refID string }
}

func (n *recordingNotif) Notify(_ uuid.UUID, title, _, _, _, refID string) error {
	n.calls = append(n.calls, struct{ title, refID string }{title, refID})
	return nil
}

// staticKeyLookup PG 集成测试用（简化：直接返回虚拟名字）
type staticKeyLookup struct{}

func (l *staticKeyLookup) FindKeyByID(k string) (string, uuid.UUID, error) {
	return "test-pg-key", uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil
}
