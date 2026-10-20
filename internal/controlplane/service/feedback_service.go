package service

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrFeedbackInvalidInput 反馈入参校验失败（类型/内容/联系方式/状态非法）。
	ErrFeedbackInvalidInput = errors.New("invalid feedback input")
	// ErrFeedbackNotFound 反馈记录不存在。
	ErrFeedbackNotFound = errors.New("feedback not found")
	// ErrFeedbackAttachmentInvalid 附件入参校验失败：空文件 / 超过 2MiB / 类型不在白名单 /
	// 宽高非法，以及提交反馈时 attachment_ids 数量超限、不属于本人或已被绑定。
	ErrFeedbackAttachmentInvalid = errors.New("invalid feedback attachment")
	// ErrFeedbackAttachmentNotFound 附件记录不存在。
	ErrFeedbackAttachmentNotFound = errors.New("feedback attachment not found")
	// ErrFeedbackAttachmentForbidden 当前请求者既不是上传者本人，也不持有反馈管理权限。
	ErrFeedbackAttachmentForbidden = errors.New("feedback attachment forbidden")
)

// 反馈类型与状态取值，与前端契约保持一致。
const (
	FeedbackTypeSuggestion = "suggestion"
	FeedbackTypeBug        = "bug"
	FeedbackTypeUsage      = "usage"
	FeedbackTypeOther      = "other"

	FeedbackStatusPending    = "pending"
	FeedbackStatusProcessing = "processing"
	FeedbackStatusResolved   = "resolved"
	FeedbackStatusClosed     = "closed"

	feedbackContentMinLen = 5
	feedbackContentMaxLen = 2000
	feedbackContactMaxLen = 128
	feedbackDefaultPage   = 1
	feedbackDefaultSize   = 20
	feedbackMaxPageSize   = 100
)

// 反馈附件（截图）容量与生命周期上限。前端按同一份契约做压缩，服务端这里是不信任客户端声明的硬校验。
const (
	// FeedbackAttachmentMaxBytes 单张图片字节上限 2 MiB。
	FeedbackAttachmentMaxBytes = 2 * 1024 * 1024
	// FeedbackAttachmentMaxCount 每条反馈最多绑定的附件数。
	FeedbackAttachmentMaxCount = 3
	// FeedbackAttachmentOrphanTTL 未绑定附件的保留时长；超时孤儿由上传路径顺带清理。
	FeedbackAttachmentOrphanTTL = 24 * time.Hour

	// feedbackAttachmentMaxDimension 前端上报的原图宽/高上限（像素）。
	feedbackAttachmentMaxDimension = 20000
	// feedbackAttachmentSniffBytes 魔数嗅探窗口，与 http.DetectContentType 的实现一致。
	feedbackAttachmentSniffBytes = 512
	// feedbackAttachmentMaxFileNameLen 文件名落库上限，与 model 的 varchar(255) 对齐。
	feedbackAttachmentMaxFileNameLen = 255
	// feedbackAttachmentDefaultFileName 客户端没给文件名时的兜底名。
	feedbackAttachmentDefaultFileName = "attachment"
)

// allowedAttachmentMimes 允许的图片类型白名单，判定依据是服务端魔数嗅探结果，
// 客户端声明的 Content-Type 一律忽略（防止把 .exe/HTML 改名成 .png 上传）。
var allowedAttachmentMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// FeedbackAttachmentMeta 附件元数据（列表用）：只含契约字段集合。
// 刻意不复用 model.FeedbackAttachment 直接序列化：既从结构上保证不含 data（模型上是 json:"-"），
// 也不会把 user_id 这类内部字段泄露给前端。
type FeedbackAttachmentMeta struct {
	ID         uuid.UUID  `json:"id"`
	FileName   string     `json:"file_name"`
	Mime       string     `json:"mime"`
	SizeBytes  int        `json:"size_bytes"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	CreatedAt  time.Time  `json:"created_at"`
	FeedbackID *uuid.UUID `json:"feedback_id"`
}

func newFeedbackAttachmentMeta(a model.FeedbackAttachment) FeedbackAttachmentMeta {
	return FeedbackAttachmentMeta{
		ID:         a.ID,
		FileName:   a.FileName,
		Mime:       a.Mime,
		SizeBytes:  a.SizeBytes,
		Width:      a.Width,
		Height:     a.Height,
		CreatedAt:  a.CreatedAt,
		FeedbackID: a.FeedbackID,
	}
}

// FeedbackListItem 反馈列表项：反馈本体 + 附件元数据。
// 附件只回元数据（model.Data 是 json:"-"，字节只走带鉴权的下载端点）。
type FeedbackListItem struct {
	model.Feedback
	// Attachments 该反馈挂载的附件元数据；无附件时为显式空数组，前端可直接遍历。
	Attachments []FeedbackAttachmentMeta `json:"attachments"`
}

type FeedbackService struct {
	repo *repository.FeedbackRepo
	// attachmentRepo 与 repo 同库：Submit 的「创建反馈 + 绑定附件」要在同一事务里完成。
	// 由 repository.FeedbackRepo.DB() 派生，构造函数签名保持不变（既有调用方无需感知）。
	attachmentRepo *repository.FeedbackAttachmentRepo
}

func NewFeedbackService(repo *repository.FeedbackRepo) *FeedbackService {
	return &FeedbackService{
		repo:           repo,
		attachmentRepo: repository.NewFeedbackAttachmentRepo(repo.DB()),
	}
}

// isValidFeedbackType 校验反馈类型。
func isValidFeedbackType(t string) bool {
	switch t {
	case FeedbackTypeSuggestion, FeedbackTypeBug, FeedbackTypeUsage, FeedbackTypeOther:
		return true
	default:
		return false
	}
}

// IsValidFeedbackStatus 校验反馈状态，handler 返回 FeedbackItem 时也会用到。
func IsValidFeedbackStatus(s string) bool {
	switch s {
	case FeedbackStatusPending, FeedbackStatusProcessing, FeedbackStatusResolved, FeedbackStatusClosed:
		return true
	default:
		return false
	}
}

// Submit 提交反馈：类型缺省为 suggestion，内容去首尾空格后需 5..2000 字符。
//
// attachmentIDs 为「先上传后提交」链路上待绑定的附件（契约上限 FeedbackAttachmentMaxCount 张）：
// 每个 id 都必须属于提交人本人且尚未绑定，否则整体失败（返回 ErrFeedbackAttachmentInvalid），
// 由 Transaction 回滚成「反馈没落库、附件也没被绑定」的全无状态，绝不半绑定。
//
// 说明：这里用可变参数（而非显式切片参数）承载 attachmentIDs，是为了让既有 6 参调用
// （tests/controlplane/service/feedback_service_test.go）保持可编译；语义与传切片完全一致。
func (s *FeedbackService) Submit(userID uuid.UUID, userName, userEmail, ftype, content, contact string, attachmentIDs ...uuid.UUID) (*model.Feedback, error) {
	if ftype == "" {
		ftype = FeedbackTypeSuggestion
	}
	if !isValidFeedbackType(ftype) {
		return nil, ErrFeedbackInvalidInput
	}

	if len(attachmentIDs) > FeedbackAttachmentMaxCount {
		return nil, ErrFeedbackAttachmentInvalid
	}

	content = strings.TrimSpace(content)
	if n := utf8.RuneCountInString(content); n < feedbackContentMinLen || n > feedbackContentMaxLen {
		return nil, ErrFeedbackInvalidInput
	}

	contact = strings.TrimSpace(contact)
	if utf8.RuneCountInString(contact) > feedbackContactMaxLen {
		return nil, ErrFeedbackInvalidInput
	}

	f := &model.Feedback{
		ID:        uuid.New(),
		UserID:    userID,
		UserName:  userName,
		UserEmail: userEmail,
		Type:      ftype,
		Content:   content,
		Contact:   contact,
		Status:    FeedbackStatusPending,
	}

	// 没有附件时走原来的单条 INSERT，避免为无附件提交额外开事务。
	if len(attachmentIDs) == 0 {
		if err := s.repo.Create(f); err != nil {
			return nil, err
		}
		return f, nil
	}

	err := s.repo.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateInTx(tx, f); err != nil {
			return err
		}
		affected, err := s.attachmentRepo.BindToFeedbackTx(tx, attachmentIDs, userID, f.ID)
		if err != nil {
			return err
		}
		// 绑定行数必须正好等于请求数：少一行就意味着有附件不存在 / 不属于本人 / 已被绑定，
		// 此时回滚整个事务，绝不让反馈留下「少几张附件」的中间态。
		if affected != int64(len(attachmentIDs)) {
			return ErrFeedbackAttachmentInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// UploadAttachment 保存一张反馈截图（先上传后提交链路的第一步）。
//
// 校验顺序：非空 → ≤2MiB → 宽高 → 魔数类型白名单。类型以 http.DetectContentType
// 嗅探前 512 字节的结论为准并落库，客户端声明的 Content-Type / 文件名后缀都不可信。
// 上传成功后 FeedbackID 仍为 NULL，等 Submit 带 attachment_ids 绑定时才置位。
func (s *FeedbackService) UploadAttachment(userID uuid.UUID, fileName string, data []byte, width, height int) (*model.FeedbackAttachment, error) {
	if len(data) == 0 {
		return nil, ErrFeedbackAttachmentInvalid
	}
	if len(data) > FeedbackAttachmentMaxBytes {
		return nil, ErrFeedbackAttachmentInvalid
	}
	if width < 0 || height < 0 || width > feedbackAttachmentMaxDimension || height > feedbackAttachmentMaxDimension {
		return nil, ErrFeedbackAttachmentInvalid
	}
	mime := sniffAttachmentMime(data)
	if !allowedAttachmentMimes[mime] {
		return nil, ErrFeedbackAttachmentInvalid
	}

	// 顺带清理孤儿：用户选图后放弃提交会留下未绑定附件，这里按 TTL 兜底回收。
	// 清理失败只记日志，绝不影响本次上传。
	s.cleanupOrphanAttachments()

	att := &model.FeedbackAttachment{
		ID:        uuid.New(),
		UserID:    userID,
		FileName:  sanitizeAttachmentFileName(fileName),
		Mime:      mime,
		Data:      data,
		SizeBytes: len(data),
		Width:     width,
		Height:    height,
	}
	if err := s.attachmentRepo.Create(att); err != nil {
		return nil, err
	}
	return att, nil
}

// GetAttachmentForDownload 读取附件字节，并做「归属 or 管理权限」的越权判定：
// 只有上传者本人，或持有反馈管理权限（canManage，由 handler 按 RBAC 权限算出）的请求者
// 才能拿到字节；其余已登录用户一律 ErrFeedbackAttachmentForbidden，
// 避免任意登录用户靠遍历 UUID 读到别人的截图。
func (s *FeedbackService) GetAttachmentForDownload(attID, requesterID uuid.UUID, canManage bool) (*model.FeedbackAttachment, error) {
	att, err := s.attachmentRepo.GetByID(attID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFeedbackAttachmentNotFound
		}
		return nil, err
	}
	if !canManage && att.UserID != requesterID {
		return nil, ErrFeedbackAttachmentForbidden
	}
	return att, nil
}

// cleanupOrphanAttachments 删除超过 FeedbackAttachmentOrphanTTL 仍未绑定到任何反馈的附件。
// 这是「先上传后提交」链路的兜底回收，失败只记日志，不影响上传与提交本身。
func (s *FeedbackService) cleanupOrphanAttachments() {
	cutoff := time.Now().Add(-FeedbackAttachmentOrphanTTL)
	n, err := s.attachmentRepo.DeleteOrphansBefore(cutoff)
	if err != nil {
		slog.Warn("清理反馈孤儿附件失败", "error", err)
		return
	}
	if n > 0 {
		slog.Info("已清理反馈孤儿附件", "count", n, "cutoff", cutoff)
	}
}

// sniffAttachmentMime 按魔数嗅探真实类型，只取前 512 字节交给 http.DetectContentType。
func sniffAttachmentMime(data []byte) string {
	if len(data) > feedbackAttachmentSniffBytes {
		data = data[:feedbackAttachmentSniffBytes]
	}
	return http.DetectContentType(data)
}

// sanitizeAttachmentFileName 归一上传文件名：去掉路径部分、剔除控制字符与双引号
// （防止 Content-Disposition 响应头注入）、按 varchar(255) 截断，空名给兜底值。
func sanitizeAttachmentFileName(name string) string {
	// 统一分隔符后再取 basename，兼容 Windows 客户端传来的反斜杠路径。
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return feedbackAttachmentDefaultFileName
	}
	if utf8.RuneCountInString(name) > feedbackAttachmentMaxFileNameLen {
		name = string([]rune(name)[:feedbackAttachmentMaxFileNameLen])
	}
	return name
}

// List 分页查询反馈，page 缺省 1，pageSize 缺省 20、上限 100。
// 每个列表项附带该反馈的附件元数据（只查元数据列，不含图片字节）。
func (s *FeedbackService) List(status, ftype string, page, pageSize int) ([]FeedbackListItem, int64, error) {
	if status != "" && !IsValidFeedbackStatus(status) {
		return nil, 0, ErrFeedbackInvalidInput
	}
	if ftype != "" && !isValidFeedbackType(ftype) {
		return nil, 0, ErrFeedbackInvalidInput
	}
	if page < 1 {
		page = feedbackDefaultPage
	}
	if pageSize < 1 {
		pageSize = feedbackDefaultSize
	}
	if pageSize > feedbackMaxPageSize {
		pageSize = feedbackMaxPageSize
	}

	feedbacks, total, err := s.repo.List(repository.FeedbackListFilter{
		Status:   status,
		Type:     ftype,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, 0, err
	}

	ids := make([]uuid.UUID, 0, len(feedbacks))
	for _, f := range feedbacks {
		ids = append(ids, f.ID)
	}
	meta, err := s.attachmentRepo.ListMetaByFeedbackIDs(ids)
	if err != nil {
		return nil, 0, err
	}

	items := make([]FeedbackListItem, 0, len(feedbacks))
	for _, f := range feedbacks {
		rows := meta[f.ID]
		// 显式空数组：契约要求 items[].attachments 始终存在，前端无需判 null。
		atts := make([]FeedbackAttachmentMeta, 0, len(rows))
		for _, row := range rows {
			atts = append(atts, newFeedbackAttachmentMeta(row))
		}
		items = append(items, FeedbackListItem{Feedback: f, Attachments: atts})
	}
	return items, total, nil
}

// UpdateStatus 更新反馈状态：流转到 resolved/closed 时记录处理时间与处理人，其它状态只更新处理备注。
func (s *FeedbackService) UpdateStatus(id uuid.UUID, status, note string, operatorID uuid.UUID) (*model.Feedback, error) {
	if !IsValidFeedbackStatus(status) {
		return nil, ErrFeedbackInvalidInput
	}

	if _, err := s.repo.GetByID(id); err != nil {
		return nil, ErrFeedbackNotFound
	}

	if err := s.repo.UpdateStatus(id, status, operatorID, note, time.Now()); err != nil {
		return nil, err
	}

	f, err := s.repo.GetByID(id)
	if err != nil {
		return nil, ErrFeedbackNotFound
	}
	return f, nil
}
