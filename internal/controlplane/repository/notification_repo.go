package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotificationRepo 站内通知数据访问层。
type NotificationRepo struct {
	db *gorm.DB
}

func NewNotificationRepo(db *gorm.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create 写入一条通知。
func (r *NotificationRepo) Create(n *model.Notification) error {
	if n.DedupeKey != "" {
		// 不指定冲突列，让 Postgres 自动匹配 partial unique index
		// (source, dedupe_key) WHERE dedupe_key IS NOT NULL，
		// 同时兼容 sqlite 不支持 ON CONFLICT ... WHERE 的限制。
		return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(n).Error
	}
	return r.db.Create(n).Error
}

// GetUnread 查询指定用户的未读通知列表，按创建时间倒序。
func (r *NotificationRepo) GetUnread(userID uuid.UUID) ([]model.Notification, error) {
	var items []model.Notification
	err := r.db.Where("user_id = ? AND is_read = false", userID).
		Order("created_at DESC").
		Limit(50).
		Find(&items).Error
	return items, err
}

// GetUnreadCount 查询指定用户的未读通知数量。
func (r *NotificationRepo) GetUnreadCount(userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = false", userID).
		Count(&count).Error
	return count, err
}

// MarkRead 将指定通知标记为已读。
func (r *NotificationRepo) MarkRead(userID, notificationID uuid.UUID) error {
	now := time.Now()
	return r.db.Model(&model.Notification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// MarkAllRead 将指定用户所有未读通知标记为已读。
func (r *NotificationRepo) MarkAllRead(userID uuid.UUID) error {
	now := time.Now()
	return r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = false", userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// GetHistory 查询指定用户的已读通知，最近 30 条。
func (r *NotificationRepo) GetHistory(userID uuid.UUID) ([]model.Notification, error) {
	var items []model.Notification
	err := r.db.Where("user_id = ? AND is_read = true", userID).
		Order("read_at DESC").
		Limit(30).
		Find(&items).Error
	return items, err
}

// adminHistoryBase 管理端通知历史统一视图：站内信(notifications) UNION ALL email/webhook 投递(notification_deliveries)。
// 缺列用 NULL/''/0/false 对齐；用户信息 LEFT JOIN users 冗余。PG 与 sqlite 通用（不依赖 ::cast）。
const adminHistoryBase = `SELECT * FROM (
	SELECT
		'inapp' AS channel,
		n.id AS id,
		n.user_id AS user_id,
		u.name AS user_name,
		u.email AS user_email,
		n.type AS notification_type,
		n.level AS level,
		n.title AS title,
		n.content AS content,
		n.is_read AS is_read,
		n.read_at AS read_at,
		n.created_at AS created_at,
		'' AS delivery_status,
		NULL AS delivered_at,
		'' AS last_error,
		0 AS attempts
	FROM notifications n
	LEFT JOIN users u ON u.id = n.user_id
	UNION ALL
	SELECT
		d.channel AS channel,
		d.id AS id,
		d.user_id AS user_id,
		u.name AS user_name,
		u.email AS user_email,
		d.notification_type AS notification_type,
		'' AS level,
		d.title AS title,
		d.content AS content,
		false AS is_read,
		NULL AS read_at,
		d.created_at AS created_at,
		d.status AS delivery_status,
		d.delivered_at AS delivered_at,
		d.last_error AS last_error,
		d.attempts AS attempts
	FROM notification_deliveries d
	LEFT JOIN users u ON u.id = d.user_id
) t`

// adminHistoryWhere 组装统一视图的过滤条件（channel/type/level 精确，keyword 匹配标题或正文，
// user 匹配用户名或邮箱，start/end 过滤 created_at）。空值条件跳过。返回 WHERE 子句与参数。
func adminHistoryWhere(channel, notifType, level, keyword, user string, startTime, endTime *time.Time) (string, []interface{}) {
	var conds []string
	var args []interface{}
	add := func(cond string, arg ...interface{}) {
		conds = append(conds, cond)
		args = append(args, arg...)
	}
	if channel != "" {
		add("t.channel = ?", channel)
	}
	if notifType != "" {
		add("t.notification_type = ?", notifType)
	}
	if level != "" {
		add("t.level = ?", level)
	}
	if keyword != "" {
		kw := "%" + keyword + "%"
		// 不用 ILIKE：mattn/go-sqlite3 不识别 ILIKE，LOWER() LIKE LOWER() 在 PG/sqlite 通用且大小写不敏感
		add("(LOWER(t.title) LIKE LOWER(?) OR LOWER(t.content) LIKE LOWER(?))", kw, kw)
	}
	if user != "" {
		u := "%" + user + "%"
		add("(LOWER(t.user_name) LIKE LOWER(?) OR LOWER(t.user_email) LIKE LOWER(?))", u, u)
	}
	if startTime != nil {
		add("t.created_at >= ?", *startTime)
	}
	if endTime != nil {
		add("t.created_at <= ?", *endTime)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + joinConds(conds), args
}

func joinConds(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

// ListAllPaged 管理端通知历史分页查询：inapp + email + webhook 三渠道统一视图。
func (r *NotificationRepo) ListAllPaged(
	channel, notifType, level, keyword, user string,
	startTime, endTime *time.Time,
	page, pageSize int,
) ([]model.AdminNotificationRecord, int64, error) {
	where, args := adminHistoryWhere(channel, notifType, level, keyword, user, startTime, endTime)

	var total int64
	if err := r.db.Raw("SELECT COUNT(*) FROM ("+adminHistoryBase+") t "+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	pageArgs := append(append([]interface{}{}, args...), pageSize, (page-1)*pageSize)
	var items []model.AdminNotificationRecord
	pageSQL := adminHistoryBase + " " + where + " ORDER BY t.created_at DESC, t.channel, t.id LIMIT ? OFFSET ?"
	if err := r.db.Raw(pageSQL, pageArgs...).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
