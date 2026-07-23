package repository

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// sqlCaptureLogger 捕获 GORM 实际生成的 SQL（含 ON CONFLICT 子句），用于离线断言语句形态。
type sqlCaptureLogger struct {
	mu   sync.Mutex
	sqls []string
}

func (l *sqlCaptureLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *sqlCaptureLogger) Info(context.Context, string, ...interface{})     {}
func (l *sqlCaptureLogger) Warn(context.Context, string, ...interface{})     {}
func (l *sqlCaptureLogger) Error(context.Context, string, ...interface{})    {}

func (l *sqlCaptureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sqls = append(l.sqls, sql)
}

func (l *sqlCaptureLogger) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.sqls) == 0 {
		return ""
	}
	return l.sqls[len(l.sqls)-1]
}

// openNotificationTestDB 建一个 sqlite 内存库，表结构与唯一索引与生产对齐：
//
//	uniq_notifications_source_dedupe ON notifications (user_id, source, dedupe_key)
//
// sqlite 的唯一索引语义与 Postgres 一致：空串之间互相冲突，NULL 之间互不冲突，
// 因此可以离线复现「带 dedupe_key 走 ON CONFLICT 推断失败（42P10）」
// 与「空 dedupe_key 写成空串撞唯一索引（23505）」两个生产缺陷。
func openNotificationTestDB(t *testing.T) (*gorm.DB, *sqlCaptureLogger) {
	t.Helper()
	capture := &sqlCaptureLogger{}
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: capture})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE notifications (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		type TEXT NOT NULL,
		level TEXT NOT NULL DEFAULT 'info',
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'direct',
		dedupe_key TEXT,
		ref_type TEXT NOT NULL DEFAULT '',
		ref_id TEXT NOT NULL DEFAULT '',
		is_read INTEGER NOT NULL DEFAULT 0,
		read_at DATETIME,
		created_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create notifications table: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_notifications_source_dedupe
		ON notifications (user_id, source, dedupe_key)`).Error; err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	return db, capture
}

func notificationCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&model.Notification{}).Count(&count).Error; err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return count
}

// TestNotificationRepo_Create_EmptyDedupeKey_OmitsColumnAndWritesNULL
// 缺陷 2 回归：空 dedupe_key 不能写成空串，否则第二条同 user+source 的通知会撞唯一索引；
// 必须让该列不参与 INSERT，落库为 NULL。
func TestNotificationRepo_Create_EmptyDedupeKey_OmitsColumnAndWritesNULL(t *testing.T) {
	db, capture := openNotificationTestDB(t)
	repo := NewNotificationRepo(db)
	userID := uuid.New()

	newNotif := func() *model.Notification {
		return &model.Notification{
			ID:      uuid.New(),
			UserID:  userID,
			Type:    model.NotificationTypeKeyActivity,
			Level:   model.NotificationLevelWarning,
			Title:   "Key 活动提醒",
			Content: "正文",
			Source:  "key_activity",
		}
	}

	if err := repo.Create(newNotif()); err != nil {
		t.Fatalf("第一条空 dedupe_key 通知写入失败: %v", err)
	}
	if sql := capture.last(); strings.Contains(sql, "dedupe_key") {
		t.Errorf("空 dedupe_key 时 INSERT 语句不应包含 dedupe_key 列，实际 SQL: %s", sql)
	}

	if err := repo.Create(newNotif()); err != nil {
		t.Fatalf("第二条空 dedupe_key 通知应写入成功（NULL 之间不冲突），实际失败: %v", err)
	}

	if got := notificationCount(t, db); got != 2 {
		t.Errorf("空 dedupe_key 不做去重，应有 2 条记录，实际 %d", got)
	}
	var nullCount int64
	if err := db.Raw("SELECT COUNT(*) FROM notifications WHERE dedupe_key IS NULL").Scan(&nullCount).Error; err != nil {
		t.Fatalf("query null count: %v", err)
	}
	if nullCount != 2 {
		t.Errorf("dedupe_key 落库必须是 NULL 而不是 ''，实际 NULL 数 = %d", nullCount)
	}
}

// TestNotificationRepo_Create_WithDedupeKey_OnConflictIncludesUserIDSourceDedupeKey
// 缺陷 2 回归：ON CONFLICT 的推断列必须与唯一索引 (user_id, source, dedupe_key) 完全一致，
// 否则 Postgres 报 42P10（索引列不匹配，无法推断）。
func TestNotificationRepo_Create_WithDedupeKey_OnConflictIncludesUserIDSourceDedupeKey(t *testing.T) {
	db, capture := openNotificationTestDB(t)
	repo := NewNotificationRepo(db)
	userID := uuid.New()

	base := &model.Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      model.NotificationTypeBudgetAlert,
		Level:     model.NotificationLevelWarning,
		Title:     "预算告警",
		Content:   "正文",
		Source:    "budget_alert",
		DedupeKey: "budget:2026-07:80:total",
	}

	if err := repo.Create(base); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	sql := capture.last()
	upper := strings.ToUpper(sql)
	idx := strings.Index(upper, "ON CONFLICT")
	if idx < 0 {
		t.Fatalf("带 dedupe_key 的 INSERT 应包含 ON CONFLICT，实际 SQL: %s", sql)
	}
	onConflict := sql[idx:]

	positions := make([]int, 0, 3)
	for _, col := range []string{"user_id", "source", "dedupe_key"} {
		pos := strings.Index(onConflict, col)
		if pos < 0 {
			t.Fatalf("ON CONFLICT 子句缺少列 %q，实际: %s", col, onConflict)
		}
		positions = append(positions, pos)
	}
	if !(positions[0] < positions[1] && positions[1] < positions[2]) {
		t.Errorf("ON CONFLICT 列顺序应与唯一索引一致 (user_id, source, dedupe_key)，实际: %s", onConflict)
	}
	if !strings.Contains(strings.ToUpper(onConflict), "DO NOTHING") {
		t.Errorf("ON CONFLICT 应为 DO NOTHING，实际: %s", onConflict)
	}

	// 行为：同 user + 同 dedupe_key 幂等，不报错也不新增
	dup := *base
	dup.ID = uuid.New()
	if err := repo.Create(&dup); err != nil {
		t.Fatalf("重复 dedupe_key 应被 DO NOTHING 吞掉而不是报错: %v", err)
	}
	if got := notificationCount(t, db); got != 1 {
		t.Errorf("同 dedupe_key 应去重为 1 条，实际 %d", got)
	}

	// 行为：不同 dedupe_key 应可写入
	other := *base
	other.ID = uuid.New()
	other.DedupeKey = "budget:2026-07:90:total"
	if err := repo.Create(&other); err != nil {
		t.Fatalf("不同 dedupe_key 应写入成功: %v", err)
	}
	// 行为：不同用户 + 相同 dedupe_key 应可写入（user_id 参与索引）
	otherUser := *base
	otherUser.ID = uuid.New()
	otherUser.UserID = uuid.New()
	if err := repo.Create(&otherUser); err != nil {
		t.Fatalf("不同用户的相同 dedupe_key 应写入成功: %v", err)
	}
	if got := notificationCount(t, db); got != 3 {
		t.Errorf("应有 3 条记录，实际 %d", got)
	}
}

// TestNotificationRepo_UniqueIndexPremise_EmptyStringCollidesNullDoesNot
// 记录缺陷根因前提：唯一索引把空串视为重复值，而 NULL 之间不冲突。
// 若这个前提不再成立（例如索引回退为部分索引或语义变化），上面的 Omit 方案需要重新评估。
func TestNotificationRepo_UniqueIndexPremise_EmptyStringCollidesNullDoesNot(t *testing.T) {
	db, _ := openNotificationTestDB(t)
	userID := uuid.New()

	rawInsert := func(id string, dedupe interface{}) error {
		return db.Exec(`INSERT INTO notifications
			(id, user_id, type, level, title, content, source, dedupe_key, ref_type, ref_id, is_read, created_at)
			VALUES (?, ?, 'key_activity', 'warning', 't', 'c', 'direct', ?, '', '', 0, ?)`,
			id, userID.String(), dedupe, "2026-07-16 00:00:00").Error
	}

	if err := rawInsert(uuid.NewString(), ""); err != nil {
		t.Fatalf("首次写入空串 dedupe_key 失败: %v", err)
	}
	if err := rawInsert(uuid.NewString(), ""); err == nil {
		t.Error("前提失效：唯一索引应把第二条空串判为重复（这正是生产 23505 的原因）")
	}

	if err := rawInsert(uuid.NewString(), nil); err != nil {
		t.Fatalf("首次写入 NULL dedupe_key 失败: %v", err)
	}
	if err := rawInsert(uuid.NewString(), nil); err != nil {
		t.Errorf("NULL 之间不应冲突，第二条 NULL dedupe_key 应写入成功: %v", err)
	}
}
