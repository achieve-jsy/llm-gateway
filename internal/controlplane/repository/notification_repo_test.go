package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newHistoryTestDB 内存 sqlite，手工建通知历史三表（users/notifications/notification_deliveries）。
// 不用 AutoMigrate：模型的 default:gen_random_uuid() 在 sqlite 下生成的 DDL 非法。
func newHistoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE users (
			id text PRIMARY KEY,
			name text NOT NULL DEFAULT '',
			email text NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE notifications (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			type varchar(32) NOT NULL,
			level varchar(16) NOT NULL DEFAULT 'info',
			title varchar(200) NOT NULL,
			content text NOT NULL,
			source varchar(32) NOT NULL DEFAULT 'direct',
			dedupe_key varchar(255),
			ref_type varchar(32) NOT NULL DEFAULT '',
			ref_id varchar(64) NOT NULL DEFAULT '',
			is_read numeric NOT NULL DEFAULT 0,
			read_at datetime,
			created_at datetime NOT NULL
		)`,
		`CREATE TABLE notification_deliveries (
			id text PRIMARY KEY,
			dedupe_key varchar(255) NOT NULL,
			channel varchar(32) NOT NULL,
			status varchar(16) NOT NULL DEFAULT 'pending',
			attempts integer NOT NULL DEFAULT 0,
			last_error text NOT NULL DEFAULT '',
			delivered_at datetime,
			notification_type varchar(32) NOT NULL DEFAULT '',
			title varchar(200) NOT NULL DEFAULT '',
			content text NOT NULL DEFAULT '',
			user_id text,
			created_at datetime
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	return db
}

// seedHistoryRows 填充三类渠道各一条记录：
//  1. inapp：u1 的 budget_alert（未读）
//  2. email：u1 的 budget_alert（delivered）
//  3. webhook：u2 的 system_startup（failed，无 notifications 行）
func seedHistoryRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	u1 := uuid.New().String()
	u2 := uuid.New().String()
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	exec := []string{
		`INSERT INTO users (id, name, email) VALUES (?, '张三', 'zhang@example.com')`,
		`INSERT INTO users (id, name, email) VALUES (?, '李四', 'li@example.com')`,
		`INSERT INTO notifications (id, user_id, type, level, title, content, is_read, created_at)
		 VALUES (?, ?, 'budget_alert', 'warning', '预算告警', '预算使用率超过 80%', 0, ?)`,
		`INSERT INTO notification_deliveries (id, dedupe_key, channel, status, attempts, last_error, delivered_at, notification_type, title, content, user_id, created_at)
		 VALUES (?, 'k:budget', 'email', 'delivered', 1, '', ?, 'budget_alert', '预算告警', '邮件正文', ?, ?)`,
		`INSERT INTO notification_deliveries (id, dedupe_key, channel, status, attempts, last_error, delivered_at, notification_type, title, content, user_id, created_at)
		 VALUES (?, 'k:startup', 'webhook', 'failed', 2, 'timeout', NULL, 'system_startup', '系统启动', '网关已启动', ?, ?)`,
	}
	args := [][]interface{}{
		{u1},
		{u2},
		{uuid.New().String(), u1, base.Add(-2 * time.Hour)},
		{uuid.New().String(), base.Add(-1 * time.Hour), u1, base.Add(-1 * time.Hour)},
		{uuid.New().String(), u2, base},
	}
	for i, s := range exec {
		if err := db.Exec(s, args[i]...).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func TestNotificationRepo_ListAllPaged_NoFilter(t *testing.T) {
	db := newHistoryTestDB(t)
	seedHistoryRows(t, db)
	repo := NewNotificationRepo(db)

	items, total, err := repo.ListAllPaged("", "", "", "", "", nil, nil, nil, 1, 20)
	if err != nil {
		t.Fatalf("ListAllPaged: %v", err)
	}
	if total != 3 {
		t.Fatalf("total: want 3, got %d", total)
	}
	if len(items) != 3 {
		t.Fatalf("items: want 3, got %d", len(items))
	}
	// ORDER BY created_at DESC：webhook(10:00) → email(09:00) → inapp(08:00)
	wantChannels := []string{"webhook", "email", "inapp"}
	for i, ch := range wantChannels {
		if items[i].Channel != ch {
			t.Errorf("items[%d].channel: want %s, got %s", i, ch, items[i].Channel)
		}
	}
	// 渠道各自字段完整性
	wh := items[0]
	if wh.NotificationType != "system_startup" || wh.DeliveryStatus != "failed" || wh.Attempts != 2 || wh.LastError != "timeout" {
		t.Errorf("webhook row wrong: %+v", wh)
	}
	if wh.UserName != "李四" {
		t.Errorf("webhook user_name: want 李四, got %q", wh.UserName)
	}
	inapp := items[2]
	if inapp.Channel != "inapp" || inapp.IsRead || inapp.DeliveryStatus != "" {
		t.Errorf("inapp row wrong: %+v", inapp)
	}
}

func TestNotificationRepo_ListAllPaged_Filters(t *testing.T) {
	db := newHistoryTestDB(t)
	seedHistoryRows(t, db)
	repo := NewNotificationRepo(db)

	cases := []struct {
		name           string
		channel        string
		notifType      string
		level          string
		keyword        string
		user           string
		wantTotal      int64
		wantFirstTitle string
	}{
		{"channel inapp", "inapp", "", "", "", "", 1, "预算告警"},
		{"channel email", "email", "", "", "", "", 1, "预算告警"},
		{"type budget_alert", "", "budget_alert", "", "", "", 2, "预算告警"},
		{"level warning", "", "", "warning", "", "", 1, "预算告警"},
		{"keyword 启动", "", "", "", "启动", "", 1, "系统启动"},
		{"keyword 80%", "", "", "", "80%", "", 1, "预算告警"},
		{"user 张三", "", "", "", "", "张三", 2, "预算告警"},
		{"user email", "", "", "", "", "li@example.com", 1, "系统启动"},
		{"no match", "", "", "", "不存在的词", "", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := repo.ListAllPaged(tc.channel, tc.notifType, tc.level, tc.keyword, tc.user, nil, nil, nil, 1, 20)
			if err != nil {
				t.Fatalf("ListAllPaged: %v", err)
			}
			if total != tc.wantTotal {
				t.Fatalf("total: want %d, got %d", tc.wantTotal, total)
			}
			if tc.wantFirstTitle != "" {
				if len(items) == 0 || items[0].Title != tc.wantFirstTitle {
					t.Fatalf("first title: want %q, got %+v", tc.wantFirstTitle, items)
				}
			}
		})
	}
}

func TestNotificationRepo_ListAllPaged_ByTypes(t *testing.T) {
	db := newHistoryTestDB(t)
	seedHistoryRows(t, db)
	repo := NewNotificationRepo(db)

	// 单类型集合：budget_alert 命中站内信 + email 两条。
	items, total, err := repo.ListAllPaged("", "", "", "", "", []string{"budget_alert"}, nil, nil, 1, 20)
	if err != nil {
		t.Fatalf("ListAllPaged: %v", err)
	}
	if total != 2 {
		t.Fatalf("types [budget_alert] total: want 2, got %d", total)
	}
	for _, it := range items {
		if it.NotificationType != "budget_alert" {
			t.Errorf("types [budget_alert] row type: want budget_alert, got %q", it.NotificationType)
		}
	}

	// 多类型集合（分类展开后）：system_startup 命中一条。
	items, total, err = repo.ListAllPaged("", "", "", "", "", []string{"system_startup", "ops_channel_down"}, nil, nil, 1, 20)
	if err != nil {
		t.Fatalf("ListAllPaged multi: %v", err)
	}
	if total != 1 || items[0].NotificationType != "system_startup" {
		t.Fatalf("types multi: want 1 system_startup, got total=%d items=%+v", total, items)
	}

	// 分类无命中：类型集合不在数据中 → 0 条。
	items, total, err = repo.ListAllPaged("", "", "", "", "", []string{"ops_channel_down", "ops_channel_recovered"}, nil, nil, 1, 20)
	if err != nil {
		t.Fatalf("ListAllPaged empty: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("types no-match: want 0, got total=%d items=%+v", total, items)
	}

	// 单类型等值优先于类型集合：notifType=budget_alert 时忽略 types。
	items, total, err = repo.ListAllPaged("", "budget_alert", "", "", "", []string{"system_startup"}, nil, nil, 1, 20)
	if err != nil {
		t.Fatalf("ListAllPaged precedence: %v", err)
	}
	if total != 2 {
		t.Fatalf("notifType precedence total: want 2, got %d", total)
	}
}

func TestNotificationRepo_ListAllPaged_PaginationAndTimeRange(t *testing.T) {
	db := newHistoryTestDB(t)
	seedHistoryRows(t, db)
	repo := NewNotificationRepo(db)

	// 分页：page_size=2 → 第 1 页 2 条、第 2 页 1 条
	page1, total, err := repo.ListAllPaged("", "", "", "", "", nil, nil, nil, 1, 2)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 3 || len(page1) != 2 {
		t.Fatalf("page1: total=%d items=%d", total, len(page1))
	}
	page2, _, err := repo.ListAllPaged("", "", "", "", "", nil, nil, nil, 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].Channel != "inapp" {
		t.Fatalf("page2: want 1 inapp, got %+v", page2)
	}

	// 时间范围：end_time 截到 09:30 → 只剩 inapp(08:00) 与 email(09:00)
	end := time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC)
	items, total, err := repo.ListAllPaged("", "", "", "", "", nil, nil, &end, 1, 20)
	if err != nil {
		t.Fatalf("time range: %v", err)
	}
	if total != 2 {
		t.Fatalf("time range total: want 2, got %d", total)
	}
	for _, it := range items {
		if it.Channel == "webhook" {
			t.Errorf("time range should exclude webhook(10:00), got %+v", it)
		}
	}

	// 时间范围：start_time 从 09:30 起 → 只剩 webhook(10:00)
	start := time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC)
	items, total, err = repo.ListAllPaged("", "", "", "", "", nil, &start, nil, 1, 20)
	if err != nil {
		t.Fatalf("start range: %v", err)
	}
	if total != 1 || items[0].Channel != "webhook" {
		t.Fatalf("start range: want 1 webhook, got total=%d items=%+v", total, items)
	}
}
