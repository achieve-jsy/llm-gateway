package model

import (
	"time"

	"github.com/google/uuid"
)

// FeedbackAttachment 反馈附件（截图）。
//
// 设计取舍：图片字节直接落 Postgres 的 bytea 列，而不是对象存储或本地盘。
// 理由见团队评估——该能力零新基建、多实例天然一致、图随库备份走，且在 docker-compose
// 未给控制面挂持久卷的前提下不会「重建容器就丢图」；代价是库体积，由客户端压缩
// （宽 ≤1600px 转 webp）+ 单图 ≤2MB + 每条反馈 ≤3 张 + 24h 孤儿清理共同兜住。
//
// 生命周期：采用「先上传后提交」——用户选图/贴图时即上传，此时 FeedbackID 为 NULL；
// 提交反馈时带上 attachment_ids 完成绑定。未被绑定的孤儿附件由 service 层按 TTL 清理。
type FeedbackAttachment struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index:idx_feedback_attachments_user_id" json:"user_id"`
	FileName string    `gorm:"type:varchar(255);not null;default:''" json:"file_name"`
	// Mime 由服务端按魔数嗅探（http.DetectContentType）后写入，不信任客户端声明。
	Mime string `gorm:"type:varchar(64);not null" json:"mime"`
	// Data 图片字节。刻意 json:"-"，列表/详情接口只回元数据，字节走带鉴权的下载端点。
	Data      []byte    `gorm:"type:bytea;not null" json:"-"`
	SizeBytes int       `gorm:"not null" json:"size_bytes"`
	Width     int       `gorm:"not null;default:0" json:"width"`
	Height    int       `gorm:"not null;default:0" json:"height"`
	CreatedAt time.Time `gorm:"not null;default:now();index:idx_feedback_attachments_created_at" json:"created_at"`

	// FeedbackID 绑定到的反馈；为空表示尚未提交（孤儿），可被 TTL 清理。
	// 刻意不加 omitempty：前端需要拿到显式 null。
	FeedbackID *uuid.UUID `gorm:"type:uuid;index:idx_feedback_attachments_feedback_id" json:"feedback_id"`

	// 外键关联：反馈删除时附件级联删除（当前无删除接口，属防御性约束，保证不留孤儿行）。
	Feedback *Feedback `gorm:"foreignKey:FeedbackID;constraint:OnDelete:CASCADE" json:"-"`
}

func (FeedbackAttachment) TableName() string { return "feedback_attachments" }
