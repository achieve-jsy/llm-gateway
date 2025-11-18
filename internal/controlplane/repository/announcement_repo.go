package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AnnouncementRepo struct {
	db *gorm.DB
}

func NewAnnouncementRepo(db *gorm.DB) *AnnouncementRepo {
	return &AnnouncementRepo{db: db}
}

func (r *AnnouncementRepo) Create(a *model.Announcement) error {
	return r.db.Create(a).Error
}

func (r *AnnouncementRepo) GetByID(id uuid.UUID) (*model.Announcement, error) {
	var a model.Announcement
	err := r.db.First(&a, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AnnouncementRepo) List(status string, page, pageSize int) ([]model.Announcement, int64, error) {
	announcements := make([]model.Announcement, 0)
	var total int64

	query := r.db.Model(&model.Announcement{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("sort_order DESC, created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&announcements).Error

	return announcements, total, err
}

func (r *AnnouncementRepo) ListActive() ([]model.Announcement, error) {
	announcements := make([]model.Announcement, 0)
	err := r.db.Where("status = ?", "published").
		Order("sort_order DESC, published_at DESC").
		Find(&announcements).Error
	return announcements, err
}

func (r *AnnouncementRepo) ListActivePaged(page, pageSize int) ([]model.Announcement, int64, error) {
	announcements := make([]model.Announcement, 0)
	var total int64
	q := r.db.Model(&model.Announcement{}).Where("status = ?", "published")
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("sort_order DESC, published_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&announcements).Error
	return announcements, total, err
}

func (r *AnnouncementRepo) Update(a *model.Announcement) error {
	return r.db.Save(a).Error
}

func (r *AnnouncementRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.Announcement{}, "id = ?", id).Error
}

func (r *AnnouncementRepo) UpdateStatus(id uuid.UUID, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "published" {
		updates["published_at"] = gorm.Expr("NOW()")
	}
	return r.db.Model(&model.Announcement{}).Where("id = ?", id).Updates(updates).Error
}

func (r *AnnouncementRepo) MarkRead(userID, announcementID uuid.UUID) error {
	now := time.Now()
	return r.db.Where(model.AnnouncementRead{UserID: userID, AnnouncementID: announcementID}).
		Assign(model.AnnouncementRead{IsRead: true, ReadAt: &now}).
		FirstOrCreate(&model.AnnouncementRead{}).Error
}

func (r *AnnouncementRepo) GetUnreadCount(userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Raw(`
		SELECT COUNT(*) FROM announcements a
		WHERE a.status = 'published'
		AND NOT EXISTS (
			SELECT 1 FROM announcement_reads ar
			WHERE ar.announcement_id = a.id
			AND ar.user_id = ?
			AND ar.is_read = true
		)
	`, userID).Scan(&count).Error
	return count, err
}

func (r *AnnouncementRepo) GetUnread(userID uuid.UUID) ([]model.Announcement, error) {
	announcements := make([]model.Announcement, 0)
	err := r.db.Raw(`
		SELECT a.* FROM announcements a
		WHERE a.status = 'published'
		AND NOT EXISTS (
			SELECT 1 FROM announcement_reads ar
			WHERE ar.announcement_id = a.id
			AND ar.user_id = ?
			AND ar.is_read = true
		)
		ORDER BY a.sort_order DESC, a.published_at DESC
	`, userID).Scan(&announcements).Error
	return announcements, err
}

func (r *AnnouncementRepo) GetReadHistory(userID uuid.UUID) ([]model.Announcement, error) {
	announcements := make([]model.Announcement, 0)
	err := r.db.Raw(`
		SELECT a.* FROM announcements a
		INNER JOIN announcement_reads ar ON ar.announcement_id = a.id
		WHERE ar.user_id = ? AND ar.is_read = true
		AND a.status != 'archived'
		ORDER BY ar.read_at DESC
		LIMIT 30
	`, userID).Scan(&announcements).Error
	return announcements, err
}