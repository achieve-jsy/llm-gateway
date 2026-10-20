package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FeedbackAttachmentRepo 反馈附件（截图）仓储。
//
// 图片字节只进 Postgres 的 bytea 列（不写本地文件、不接对象存储），因此这里刻意把
// 「带字节查询」与「只查元数据」分成两组方法：列表类路径一律显式 Select 元数据列，
// 绝不把 data 查出来，避免一次列表把几 MB 的图片读进内存。
type FeedbackAttachmentRepo struct {
	db *gorm.DB
}

func NewFeedbackAttachmentRepo(db *gorm.DB) *FeedbackAttachmentRepo {
	return &FeedbackAttachmentRepo{db: db}
}

// attachmentMetaColumns 附件元数据列（刻意不含 data）。
var attachmentMetaColumns = []string{
	"id", "user_id", "file_name", "mime", "size_bytes", "width", "height", "created_at", "feedback_id",
}

// Create 写入一条附件记录（含图片字节）。此时 FeedbackID 通常为 NULL（先上传后提交）。
func (r *FeedbackAttachmentRepo) Create(a *model.FeedbackAttachment) error {
	return r.db.Create(a).Error
}

// GetByID 读取单条附件（含字节），供下载端点使用；记录不存在时返回 gorm.ErrRecordNotFound。
func (r *FeedbackAttachmentRepo) GetByID(id uuid.UUID) (*model.FeedbackAttachment, error) {
	var a model.FeedbackAttachment
	if err := r.db.First(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// ListMetaByFeedbackIDs 按反馈 ID 批量取附件元数据，返回 feedback_id → 附件列表。
// ids 为空时直接返回空 map，不发查询；查询明确 Select 元数据列，绝不读 data（bytea）。
func (r *FeedbackAttachmentRepo) ListMetaByFeedbackIDs(ids []uuid.UUID) (map[uuid.UUID][]model.FeedbackAttachment, error) {
	result := make(map[uuid.UUID][]model.FeedbackAttachment)
	if len(ids) == 0 {
		return result, nil
	}

	rows := make([]model.FeedbackAttachment, 0)
	err := r.db.Model(&model.FeedbackAttachment{}).
		Select(attachmentMetaColumns).
		Where("feedback_id IN ?", ids).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		// 旧库尚未迁移出 feedback_attachments 表时降级为「无附件元数据」，不让整个反馈列表 500；
		// 其它错误（连接失败/权限等）如实上抛，不掩盖真实故障。
		if !r.db.Migrator().HasTable(&model.FeedbackAttachment{}) {
			return result, nil
		}
		return nil, err
	}

	for _, row := range rows {
		if row.FeedbackID == nil {
			continue
		}
		result[*row.FeedbackID] = append(result[*row.FeedbackID], row)
	}
	return result, nil
}

// BindToFeedback 把一批「属于 userID 且尚未绑定」的附件绑定到 feedbackID，返回实际影响行数。
// 影响行数 < len(attIDs) 说明其中有不存在 / 不属于本人 / 已被绑定的附件，由 service 据此整体回滚。
func (r *FeedbackAttachmentRepo) BindToFeedback(attIDs []uuid.UUID, userID, feedbackID uuid.UUID) (int64, error) {
	return bindFeedbackAttachments(r.db, attIDs, userID, feedbackID)
}

// BindToFeedbackTx 在调用方事务内完成绑定，供 Submit 保证「创建反馈 + 绑定附件」原子生效。
func (r *FeedbackAttachmentRepo) BindToFeedbackTx(tx *gorm.DB, attIDs []uuid.UUID, userID, feedbackID uuid.UUID) (int64, error) {
	return bindFeedbackAttachments(tx, attIDs, userID, feedbackID)
}

// bindFeedbackAttachments 是一条 UPDATE 完成绑定与归属校验：命中条件即「本人的未绑定附件」。
func bindFeedbackAttachments(db *gorm.DB, attIDs []uuid.UUID, userID, feedbackID uuid.UUID) (int64, error) {
	if len(attIDs) == 0 {
		return 0, nil
	}
	res := db.Model(&model.FeedbackAttachment{}).
		Where("id IN ? AND user_id = ? AND feedback_id IS NULL", attIDs, userID).
		Update("feedback_id", feedbackID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// DeleteOrphansBefore 删除 cutoff 之前上传、且至今未绑定到任何反馈的孤儿附件，返回删除行数。
// 已绑定附件不受影响（feedback_id IS NOT NULL 不命中）；TTL 由 service 按 FeedbackAttachmentOrphanTTL 计算。
func (r *FeedbackAttachmentRepo) DeleteOrphansBefore(cutoff time.Time) (int64, error) {
	res := r.db.Where("feedback_id IS NULL AND created_at < ?", cutoff).
		Delete(&model.FeedbackAttachment{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
