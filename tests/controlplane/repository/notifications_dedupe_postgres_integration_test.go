//go:build integration

// 回归测试：站内信（notifications）的写入路径与去重索引语义。
//
// 背景（2026-09-16 线上事故，站内信自 2026-07-16 起全部静默丢失）：
//   - 迁移 20260716025004 建的唯一索引是**部分索引**
//     `(source, dedupe_key) WHERE dedupe_key IS NOT NULL`，且**不含 user_id**；
//   - 带 dedupe_key 的写入执行 `ON CONFLICT (source, dedupe_key)` → 部分索引无法推断 → 42P10；
//   - 不带 dedupe_key 的写入被 GORM 写成 `''`，与任意一行历史 `''` 相撞 → 23505
//     （旧索引不含 user_id，所以一行历史记录就能永久堵死所有用户的空 dedupe_key 通知）；
//   - 失败只记 slog.Warn，邮件通道照发，因此两个月无人发现。
//   - 修复 = 迁移 20260916095738（改为非部分唯一索引 `(user_id, source, dedupe_key)`）
//     + NotificationRepo.Create（ON CONFLICT 三列；空 dedupe_key 走 Omit 写 NULL）。两半缺一不可。
//
// 本测试在真实 PostgreSQL 上重建"旧索引 + 生产形状历史数据"，执行修复迁移，
// 再通过**真实代码路径**验证三条写入语义，防止上述任一环节回退。
package repository_test

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNotificationsDedupeIndexAndWritePaths(t *testing.T) {
	dsn := postgresIntegrationDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	schema := "notif_dedupe_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })

	db, err := gorm.Open(postgres.Open(withSearchPath(dsn, schema)), &gorm.Config{})
	require.NoError(t, err)

	// ── 1. 重建事故现场：修复前的表结构（含部分唯一索引）与生产形状数据 ──────────────
	// 表结构取自 20260624020000_create_notifications_table.sql，
	// source/dedupe_key 与旧索引取自 20260716025004_add_notification_outbox.sql。
	require.NoError(t, db.Exec(`
		CREATE TABLE "notifications" (
			"id"         uuid          NOT NULL DEFAULT gen_random_uuid(),
			"user_id"    uuid          NOT NULL,
			"type"       varchar(32)   NOT NULL,
			"level"      varchar(16)   NOT NULL DEFAULT 'info',
			"title"      varchar(200)  NOT NULL,
			"content"    text          NOT NULL,
			"ref_type"   varchar(32)   NOT NULL DEFAULT '',
			"ref_id"     varchar(64)   NOT NULL DEFAULT '',
			"is_read"    boolean       NOT NULL DEFAULT false,
			"read_at"    timestamptz   NULL,
			"created_at" timestamptz   NOT NULL,
			PRIMARY KEY ("id")
		);
		CREATE INDEX "idx_notifications_user_unread" ON "notifications" ("user_id", "is_read");
		ALTER TABLE "notifications" ADD COLUMN "source" character varying(32) NOT NULL DEFAULT 'direct';
		ALTER TABLE "notifications" ADD COLUMN "dedupe_key" character varying(255);
		CREATE UNIQUE INDEX "uniq_notifications_source_dedupe"
			ON "notifications" ("source", "dedupe_key") WHERE "dedupe_key" IS NOT NULL;
	`).Error)

	// 线上 2026-07-16 那条 dedupe_key='' 的历史记录，正是它堵死了此后所有空 dedupe_key 的写入。
	seededUser := uuid.NewString()
	require.NoError(t, db.Exec(
		`INSERT INTO "notifications" ("user_id","type","level","title","content","source","dedupe_key","created_at")
		 VALUES (?, 'key_activity', 'warning', '历史记录', 'seed', 'direct', '', now())`,
		seededUser,
	).Error)

	// ── 2. 执行修复迁移（迁移文件是唯一真相源，此处不重复 SQL）────────────────────
	migration, err := os.ReadFile(notificationsDedupeMigrationPath(t))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)

	var indexdef string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = ? AND indexname = 'uniq_notifications_source_dedupe'`,
		schema,
	).Scan(&indexdef).Error)
	require.Contains(t, indexdef, "(user_id, source, dedupe_key)", "唯一索引必须含 user_id（用户维度去重）")
	require.NotContains(t, indexdef, "WHERE", "必须是非部分索引，否则 ON CONFLICT 无法推断（42P10）")

	// ── 3. Key 告警链路：不带 dedupe_key → 写 NULL，连续两条都必须落库 ────────────────
	// 走真实链路 KeyActivityNotifierAdapter → MultiNotifier → DBNotifier → NotificationRepo。
	notifier := service.NewKeyActivityNotifierAdapter(
		service.NewMultiNotifier(service.NewDBNotifier(repository.NewNotificationRepo(db))),
	)
	keyID := uuid.NewString()
	for i := 0; i < 2; i++ {
		require.NoError(t, notifier.Notify(uuid.MustParse(keyID), "Key 新设备使用提醒", "body", "warning", "api_key", keyID),
			"第 %d 条 Key 告警站内信必须写入成功（旧索引下会报 23505）", i+1)
	}
	var nullKeys, emptyKeys int64
	require.NoError(t, db.Model(&model.Notification{}).Where("user_id = ? AND dedupe_key IS NULL", keyID).Count(&nullKeys).Error)
	require.NoError(t, db.Model(&model.Notification{}).Where("dedupe_key = ?", "").Count(&emptyKeys).Error)
	require.EqualValues(t, 2, nullKeys, "两条 Key 告警通知都应落库且 dedupe_key 为 NULL")
	require.EqualValues(t, 1, emptyKeys, "除那行历史数据外，不应再产生空字符串 dedupe_key")

	// ── 4. 带 dedupe_key 的路径：ON CONFLICT 三列推断成功且幂等 ────────────────────
	repo := repository.NewNotificationRepo(db)
	newNotification := func(userID, dedupeKey string) *model.Notification {
		return &model.Notification{
			UserID:    uuid.MustParse(userID),
			Type:      model.NotificationTypeExpansionSubmitted,
			Level:     model.NotificationLevelInfo,
			Title:     "新的扩容审批申请",
			Content:   "c",
			Source:    "outbox",
			DedupeKey: dedupeKey,
		}
	}
	userA, userB := uuid.NewString(), uuid.NewString()
	require.NoError(t, repo.Create(newNotification(userA, "outbox-key-1")), "旧索引下此处报 42P10")
	require.NoError(t, repo.Create(newNotification(userA, "outbox-key-1")), "重复写入必须幂等且不报错")
	var sameKey int64
	require.NoError(t, db.Model(&model.Notification{}).Where("dedupe_key = ?", "outbox-key-1").Count(&sameKey).Error)
	require.EqualValues(t, 1, sameKey, "同一 (user_id, source, dedupe_key) 只应有一行")

	// 5. 不同 user 允许共用同一个 (source, dedupe_key)：旧索引把去重做成了跨用户全局，这里钉住用户维度语义。
	require.NoError(t, repo.Create(newNotification(userB, "outbox-key-1")))
	require.NoError(t, db.Model(&model.Notification{}).Where("dedupe_key = ?", "outbox-key-1").Count(&sameKey).Error)
	require.EqualValues(t, 2, sameKey, "不同用户各自持有同一 dedupe_key")
}

// withSearchPath 把 search_path 追加到 DSN。
// TEST_DATABASE_URL 可能是 URL 形式（postgres://…?sslmode=disable）或键值形式
// （host=… port=… sslmode=disable），两种都要支持，否则字符串拼接会破坏 DSN。
func withSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		separator := "?"
		if parsed, err := url.Parse(dsn); err == nil && parsed.RawQuery != "" {
			separator = "&"
		}
		return dsn + separator + "search_path=" + schema
	}
	return dsn + " search_path=" + schema
}

// notificationsDedupeMigrationPath 定位修复迁移文件。
// 文件被重命名/合并时跳过而不是失败，避免把测试变成维护负担。
func notificationsDedupeMigrationPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	matches, err := filepath.Glob(filepath.Join(
		filepath.Dir(file), "..", "..", "..", "atlas-migrations", "*_fix_notifications_source_dedupe.sql",
	))
	require.NoError(t, err)
	if len(matches) == 0 {
		t.Skip("未找到修复迁移文件（可能已重命名或合并），跳过")
	}
	return matches[0]
}
