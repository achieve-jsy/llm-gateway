package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// feedbackAttachmentBodyLimit multipart 请求体上限：单张 2MiB + 1MiB 表单/编码余量。
// 用 http.MaxBytesReader 提前掐断超大请求，避免整包被读进内存或落临时文件。
const feedbackAttachmentBodyLimit = service.FeedbackAttachmentMaxBytes + 1<<20

type FeedbackHandler struct {
	svc *service.FeedbackService
}

func NewFeedbackHandler(svc *service.FeedbackService) *FeedbackHandler {
	return &FeedbackHandler{svc: svc}
}

type CreateFeedbackRequest struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Contact string `json:"contact"`
	// AttachmentIDs 可选：先上传后提交链路上的附件 ID（≤3 张）。
	// 每个 id 必须存在、属于提交人本人且尚未绑定，否则整体 400（不部分绑定）。
	AttachmentIDs []string `json:"attachment_ids"`
}

type UpdateFeedbackStatusRequest struct {
	Status     string `json:"status"`
	HandleNote string `json:"handle_note"`
}

// FeedbackAttachmentResponse 上传附件成功的响应体：只回元数据。
// 单独定义而非直接回 model，是为了让契约字段集合精确固定（绝不回 user_id/feedback_id，更不回 data）。
type FeedbackAttachmentResponse struct {
	ID        uuid.UUID `json:"id"`
	FileName  string    `json:"file_name"`
	Mime      string    `json:"mime"`
	SizeBytes int       `json:"size_bytes"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	CreatedAt time.Time `json:"created_at"`
}

func newFeedbackAttachmentResponse(a *model.FeedbackAttachment) FeedbackAttachmentResponse {
	return FeedbackAttachmentResponse{
		ID:        a.ID,
		FileName:  a.FileName,
		Mime:      a.Mime,
		SizeBytes: a.SizeBytes,
		Width:     a.Width,
		Height:    a.Height,
		CreatedAt: a.CreatedAt,
	}
}

// Create 任意已登录用户提交反馈（可选携带 attachment_ids 绑定已上传的截图）。
func (h *FeedbackHandler) Create(c *gin.Context) {
	var req CreateFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	attachmentIDs := make([]uuid.UUID, 0, len(req.AttachmentIDs))
	for _, raw := range req.AttachmentIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "附件 ID 无效", "code": CodeBadRequest},
			})
			return
		}
		attachmentIDs = append(attachmentIDs, id)
	}

	f, err := h.svc.Submit(
		getUserID(c),
		c.GetString("user_name"),
		c.GetString("user_email"),
		req.Type, req.Content, req.Contact, attachmentIDs...,
	)
	if err != nil {
		if errors.Is(err, service.ErrFeedbackAttachmentInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "附件不合法：最多 3 张，且必须存在、属于本人且尚未绑定", "code": CodeBadRequest},
			})
			return
		}
		if errors.Is(err, service.ErrFeedbackInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "反馈内容不合法，长度需为 5-2000 字符", "code": CodeBadRequest},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "提交反馈失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         f.ID,
		"status":     f.Status,
		"created_at": f.CreatedAt,
	})
}

// UploadAttachment 上传反馈截图（先上传后提交）：multipart/form-data，文件字段名 file，
// 可选表单字段 width/height。
//
// 类型由服务端按魔数嗅探（http.DetectContentType）判定，客户端声明的 Content-Type 一律忽略；
// 空文件 / 超过 2MiB / 类型不在白名单 / 宽高非法统一 400 validation_error。
func (h *FeedbackHandler) UploadAttachment(c *gin.Context) {
	// 限制请求体：超过上限时 ParseMultipartForm 直接报错，不会把超大文件读进内存。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, feedbackAttachmentBodyLimit)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误：缺少 file 文件字段或请求体过大", "code": CodeBadRequest},
		})
		return
	}

	width, ok := parseAttachmentDimension(c.PostForm("width"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "width 必须是整数", "code": CodeBadRequest},
		})
		return
	}
	height, ok := parseAttachmentDimension(c.PostForm("height"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "height 必须是整数", "code": CodeBadRequest},
		})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "读取上传文件失败", "code": CodeBadRequest},
		})
		return
	}
	defer func() { _ = file.Close() }()

	// 多读 1 字节：既能识别「恰好 2MiB」的合法边界，又保证超限时不会无限读下去。
	data, err := io.ReadAll(io.LimitReader(file, service.FeedbackAttachmentMaxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "读取上传文件失败", "code": CodeBadRequest},
		})
		return
	}

	att, err := h.svc.UploadAttachment(getUserID(c), fileHeader.Filename, data, width, height)
	if err != nil {
		if errors.Is(err, service.ErrFeedbackAttachmentInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "validation_error",
					"message": "附件不合法：文件不能为空、单张不超过 2MiB、仅支持 png/jpeg/webp/gif，宽高需在 0-20000 之间",
					"code":    CodeBadRequest,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "上传附件失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, newFeedbackAttachmentResponse(att))
}

// DownloadAttachment 下载附件字节。鉴权规则：请求者是上传者本人，或持有 RBAC 权限
// PermFeedbackManage；其余已登录用户一律 403 feedback_attachment_forbidden，
// 防止任意登录用户遍历 UUID 读到他人截图。
func (h *FeedbackHandler) DownloadAttachment(c *gin.Context) {
	attID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "附件 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	// 这里不能直接挂 rbac Guard：Guard 对权限不足一律回 org.forbidden，
	// 而本端点的 403 是「资源归属」拒绝，契约要求 code=feedback_attachment_forbidden。
	// Actor 已由路由上的 BuildAndInjectActor 中间件注入（与 Guard 同源，不信任 JWT 自证角色）。
	canManage := actorHasPermission(c, rbac.PermFeedbackManage)

	att, err := h.svc.GetAttachmentForDownload(attID, getUserID(c), canManage)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFeedbackAttachmentForbidden):
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{"type": "authorization_error", "message": "无权访问该附件", "code": CodeAttachmentForbidden},
			})
		case errors.Is(err, service.ErrFeedbackAttachmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "附件不存在", "code": CodeAttachmentNotFound},
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"type": "internal_error", "message": "获取附件失败", "code": CodeInternalError},
			})
		}
		return
	}

	// Content-Type 用落库的嗅探结果（不信任客户端声明），并禁止浏览器自行改判类型。
	c.Header("Content-Disposition", `inline; filename="`+escapeAttachmentFileName(att.FileName)+`"`)
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, att.Mime, att.Data)
}

// parseAttachmentDimension 解析 width/height 表单字段：缺省（空串）视为 0，
// 非整数返回 false（负数与超大值交给 service 统一按契约拒绝）。
func parseAttachmentDimension(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// escapeAttachmentFileName 生成 Content-Disposition 文件名前的最后一道防线：
// 双引号/反斜杠替换、控制字符剔除（上传时已归一，这里防御历史数据）。
func escapeAttachmentFileName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '"' || r == '\\':
			return '_'
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, name)
}

// actorHasPermission 按 rbac.Registry 的 ScopeConstraint 判定当前请求者是否满足权限。
// 与 rbac.Checker.Guard 的判定逻辑同源（Actor 由 BuildAndInjectActor 从服务端 user_roles 构建），
// 只是把「不允许」的处置权留给调用方：本端点要回资源归属的 403 code，而不是 org.forbidden。
func actorHasPermission(c *gin.Context, perm rbac.Permission) bool {
	entry, ok := rbac.Registry[perm]
	if !ok {
		return false
	}
	actor := rbac.GetActor(c)
	switch entry.Constraint {
	case rbac.ScopeAny:
		return true
	case rbac.ScopeManage:
		return actor.IsManager()
	case rbac.ScopeStudio:
		return actor.IsGlobal || len(actor.StudioIDs) > 0
	case rbac.ScopeGlobal:
		return actor.IsGlobal
	default:
		return false
	}
}

// List 管理员分页查看反馈。
func (h *FeedbackHandler) List(c *gin.Context) {
	status := c.Query("status")
	ftype := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	feedbacks, total, err := h.svc.List(status, ftype, page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrFeedbackInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "筛选条件不合法", "code": CodeBadRequest},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取反馈列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     feedbacks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UpdateStatus 管理员更新反馈状态。
func (h *FeedbackHandler) UpdateStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "反馈 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	var req UpdateFeedbackStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	f, err := h.svc.UpdateStatus(id, req.Status, req.HandleNote, getUserID(c))
	if err != nil {
		if errors.Is(err, service.ErrFeedbackNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "反馈不存在", "code": "not_found"},
			})
			return
		}
		if errors.Is(err, service.ErrFeedbackInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "状态值无效", "code": CodeBadRequest},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "更新反馈失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, f)
}
