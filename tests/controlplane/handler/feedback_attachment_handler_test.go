package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 附件接口测试的固定身份。与 feedback_handler_test.go 一样，用请求头切换登录用户，
// 避免为不同角色重复搭路由；JWT 本身由 auth 模块单测覆盖。
const (
	feedbackAttachmentOwnerID = "33333333-3333-3333-3333-333333333333"
	feedbackAttachmentOtherID = "44444444-4444-4444-4444-444444444444"
	feedbackAttachmentAdminID = "55555555-5555-5555-5555-555555555555"
	feedbackAttachmentUserHdr = "X-Test-User"
)

// feedbackAttachmentHandlerDDL 复用既有反馈用例的建表语句（feedbacks + RBAC 相关表），
// 只额外补一张 feedback_attachments，保证下载端点的 Actor 构建走真实 RBAC 链路。
const feedbackAttachmentHandlerDDL = feedbackHandlerTestDDL + `;
CREATE TABLE feedback_attachments (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	file_name TEXT NOT NULL DEFAULT '',
	mime TEXT NOT NULL,
	data BLOB NOT NULL,
	size_bytes INTEGER NOT NULL,
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	feedback_id TEXT
)`

// setupFeedbackAttachmentTestRouter 搭建被测路由：真实 Handler + 真实 Service/Repo（内存 sqlite）
// + 真实 RBAC Checker。路由挂载方式与 router.go 的 adminGroup 一致：
// 上传/提交任意登录用户可用，列表需要 feedback:manage；下载端点只注入 Actor，
// 鉴权（本人 or 管理权限）由 handler 内部判定。
func setupFeedbackAttachmentTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err, "打开内存 sqlite")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // :memory: 每连接一库，限制单连接保证路由与断言命中同一库
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Exec(feedbackAttachmentHandlerDDL).Error, "建测试表")

	h := handler.NewFeedbackHandler(service.NewFeedbackService(repository.NewFeedbackRepo(db)))
	checker := rbac.NewChecker(orgrepo.NewRoleRepo(db), orgrepo.NewUserOrgRepo(db))
	guard := checker.Guard(rbac.PermFeedbackManage)

	injectAuth := func(c *gin.Context) {
		userID := c.GetHeader(feedbackAttachmentUserHdr)
		if userID == "" {
			userID = feedbackAttachmentOwnerID
		}
		c.Set("user_id", userID)
		c.Set("user_name", "测试用户")
		c.Set("user_email", "tester@example.com")
	}
	injectActor := func(c *gin.Context) { checker.BuildAndInjectActor(c) }

	r := gin.New()
	r.POST("/api/v1/feedback", injectAuth, h.Create)
	r.GET("/api/v1/feedback", injectAuth, guard, h.List)
	r.POST("/api/v1/feedback/attachments", injectAuth, h.UploadAttachment)
	r.GET("/api/v1/feedback/attachments/:id", injectAuth, injectActor, h.DownloadAttachment)
	return r, db
}

// seedFeedbackAttachmentAdminRole 给管理员写入 user_roles 记录，Checker 据此判定 IsGlobal=true。
func seedFeedbackAttachmentAdminRole(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'admin', 'company', '', 'system:test')`,
		feedbackAttachmentAdminID,
	).Error, "写入管理员角色")
}

// attachmentPNG 造一段魔数合法的 PNG 字节（DetectContentType 只看前 512 字节的魔数）。
func attachmentPNG(size int) []byte {
	data := make([]byte, 0, size)
	data = append(data, 0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n')
	for len(data) < size {
		data = append(data, 0)
	}
	return data[:size]
}

// doAttachmentUpload 发一次 multipart 上传请求（文件字段名 file，可选 width/height 表单字段）。
func doAttachmentUpload(t *testing.T, r *gin.Engine, userID, fileName string, data []byte, width, height string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = fw.Write(data)
	require.NoError(t, err)
	if width != "" {
		require.NoError(t, mw.WriteField("width", width))
	}
	if height != "" {
		require.NoError(t, mw.WriteField("height", height))
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback/attachments", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(feedbackAttachmentUserHdr, userID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doAttachmentJSON 发送 JSON 请求（提交反馈 / 列表等）。
func doAttachmentJSON(r *gin.Engine, method, path, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set(feedbackAttachmentUserHdr, userID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doAttachmentDownload 发送下载请求。
func doAttachmentDownload(r *gin.Engine, attID, userID string) *httptest.ResponseRecorder {
	return doAttachmentJSON(r, http.MethodGet, "/api/v1/feedback/attachments/"+attID, "", userID)
}

// uploadAttachmentOK 走真实上传接口拿到附件元数据，作为下载/绑定用例的前置数据。
func uploadAttachmentOK(t *testing.T, r *gin.Engine, userID, fileName string, data []byte) handler.FeedbackAttachmentResponse {
	t.Helper()
	w := doAttachmentUpload(t, r, userID, fileName, data, "1600", "900")
	require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())

	var att handler.FeedbackAttachmentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &att))
	require.NotEqual(t, uuid.Nil, att.ID)
	return att
}

// ===================== POST /api/v1/feedback/attachments =====================

func TestFeedbackAttachmentHandlerUpload_Success(t *testing.T) {
	// 测试目的：合法 PNG 上传返回 201 与契约字段集合，且响应体绝不含 data（字节只走鉴权下载端点）。
	r, db := setupFeedbackAttachmentTestRouter(t)
	data := attachmentPNG(2048)

	w := doAttachmentUpload(t, r, feedbackAttachmentOwnerID, "shot.png", data, "1600", "900")
	require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())

	var resp map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, key := range []string{"id", "file_name", "mime", "size_bytes", "width", "height", "created_at"} {
		require.Contains(t, resp, key, "响应体缺少契约字段 %s", key)
	}
	// 关键断言：图片字节（data）与内部字段绝不能出现在 JSON 里
	for _, forbidden := range []string{"data", "user_id", "feedback_id"} {
		require.NotContains(t, resp, forbidden, "响应体不应包含 %s", forbidden)
	}

	var att struct {
		ID        string `json:"id"`
		FileName  string `json:"file_name"`
		Mime      string `json:"mime"`
		SizeBytes int    `json:"size_bytes"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		CreatedAt string `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &att))
	require.Equal(t, "shot.png", att.FileName)
	require.Equal(t, "image/png", att.Mime, "mime 必须是服务端魔数嗅探结果")
	require.Equal(t, len(data), att.SizeBytes)
	require.Equal(t, 1600, att.Width)
	require.Equal(t, 900, att.Height)
	require.NotEmpty(t, att.CreatedAt)

	// 落库：归属当前用户、字节完整、仍为未绑定孤儿
	id := uuid.MustParse(att.ID)
	stored, err := repository.NewFeedbackAttachmentRepo(db).GetByID(id)
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(feedbackAttachmentOwnerID), stored.UserID)
	require.Equal(t, data, stored.Data)
	require.Nil(t, stored.FeedbackID)
}

func TestFeedbackAttachmentHandlerUpload_Validation(t *testing.T) {
	// 测试目的：空文件 / 超 2MiB / 类型不在白名单 / 宽高非法 / 缺 file 字段
	// 一律 400 validation_error，并且不落库（code=bad_request）。
	tests := []struct {
		name        string
		description string
		fileName    string
		data        []byte
		width       string
		height      string
	}{
		{
			name:        "超过 2MiB",
			description: "2MiB+1 字节应返回 400",
			fileName:    "big.png",
			data:        attachmentPNG(service.FeedbackAttachmentMaxBytes + 1),
			width:       "1",
			height:      "1",
		},
		{
			name:        "纯文本伪装成 png",
			description: "魔数嗅探为 text/plain，不属于图片白名单",
			fileName:    "fake.png",
			data:        []byte("这不是图片，只是一段普通文本。"),
			width:       "1",
			height:      "1",
		},
		{
			name:        "width 非整数",
			description: "width=abc 无法解析为整数应返回 400",
			fileName:    "a.png",
			data:        attachmentPNG(64),
			width:       "abc",
			height:      "1",
		},
		{
			name:        "width 为负",
			description: "width=-1 非法",
			fileName:    "a.png",
			data:        attachmentPNG(64),
			width:       "-1",
			height:      "1",
		},
		{
			name:        "height 超过 20000",
			description: "height=20001 非法",
			fileName:    "a.png",
			data:        attachmentPNG(64),
			width:       "1",
			height:      "20001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, db := setupFeedbackAttachmentTestRouter(t)

			w := doAttachmentUpload(t, r, feedbackAttachmentOwnerID, tt.fileName, tt.data, tt.width, tt.height)

			require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
			var resp struct {
				Error struct {
					Type string `json:"type"`
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Equal(t, "validation_error", resp.Error.Type)
			require.Equal(t, handler.CodeBadRequest, resp.Error.Code)

			var count int64
			require.NoError(t, db.Model(&model.FeedbackAttachment{}).Count(&count).Error)
			require.Zero(t, count, "非法上传不应落库")
		})
	}

	t.Run("缺少 file 字段", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		// 不建 multipart，直接发 JSON：FormFile 必然失败
		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback/attachments", `{"file":"x"}`, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		var count int64
		require.NoError(t, db.Model(&model.FeedbackAttachment{}).Count(&count).Error)
		require.Zero(t, count)
	})
}

// ===================== GET /api/v1/feedback/attachments/:id =====================

func TestFeedbackAttachmentHandlerDownload_Authorization(t *testing.T) {
	// 测试目的（关键防越权用例）：只有上传者本人或持有 feedback:manage 的请求者能读到字节；
	// 另一个普通登录用户必须拿到 403 feedback_attachment_forbidden，而不是 200/org.forbidden。
	r, db := setupFeedbackAttachmentTestRouter(t)
	seedFeedbackAttachmentAdminRole(t, db)
	data := attachmentPNG(512)
	att := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "shot.png", data)
	attID := att.ID.String()

	t.Run("上传者本人 200 且响应头符合契约", func(t *testing.T) {
		w := doAttachmentDownload(r, attID, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusOK, w.Code, "body = %s", w.Body.String())
		require.Equal(t, data, w.Body.Bytes(), "必须原样返回字节")
		require.Equal(t, "image/png", w.Header().Get("Content-Type"))
		require.Equal(t, `inline; filename="shot.png"`, w.Header().Get("Content-Disposition"))
		require.Equal(t, "private, max-age=3600", w.Header().Get("Cache-Control"))
		require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	})

	t.Run("另一个普通用户 403 且拿不到字节", func(t *testing.T) {
		w := doAttachmentDownload(r, attID, feedbackAttachmentOtherID)

		require.Equal(t, http.StatusForbidden, w.Code, "body = %s", w.Body.String())

		var resp struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "authorization_error", resp.Error.Type)
		require.Equal(t, "无权访问该附件", resp.Error.Message)
		require.Equal(t, handler.CodeAttachmentForbidden, resp.Error.Code)
		require.Equal(t, "feedback_attachment_forbidden", resp.Error.Code)
		// 拒绝时响应体里不能夹带任何图片字节
		require.NotContains(t, w.Body.String(), string(data))
	})

	t.Run("持有 feedback:manage 的管理员 200", func(t *testing.T) {
		w := doAttachmentDownload(r, attID, feedbackAttachmentAdminID)

		require.Equal(t, http.StatusOK, w.Code, "body = %s", w.Body.String())
		require.Equal(t, data, w.Body.Bytes())
	})

	t.Run("记录不存在返回 404", func(t *testing.T) {
		w := doAttachmentDownload(r, uuid.NewString(), feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusNotFound, w.Code, "body = %s", w.Body.String())
		var resp struct {
			Error struct {
				Type string `json:"type"`
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "not_found", resp.Error.Type)
		require.Equal(t, handler.CodeAttachmentNotFound, resp.Error.Code)
		require.Equal(t, "feedback_attachment_not_found", resp.Error.Code)
	})

	t.Run("id 非法返回 400", func(t *testing.T) {
		w := doAttachmentDownload(r, "not-a-uuid", feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
	})
}

// ===================== POST /api/v1/feedback（附件绑定） =====================

func TestFeedbackHandlerCreate_WithAttachments(t *testing.T) {
	// 测试目的：attachment_ids 属于本人且未绑定 → 201 且附件 feedback_id 指向新反馈。
	r, db := setupFeedbackAttachmentTestRouter(t)
	first := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "a.png", attachmentPNG(128))
	second := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "b.png", attachmentPNG(128))

	body := `{"type":"bug","content":"带两张截图的反馈","attachment_ids":["` + first.ID.String() + `","` + second.ID.String() + `"]}`
	w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID)
	require.Equal(t, http.StatusCreated, w.Code, "body = %s", w.Body.String())

	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	feedbackID := uuid.MustParse(created.ID)

	repo := repository.NewFeedbackAttachmentRepo(db)
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		row, err := repo.GetByID(id)
		require.NoError(t, err)
		require.NotNil(t, row.FeedbackID, "附件应被绑定")
		require.Equal(t, feedbackID, *row.FeedbackID)
	}
}

func TestFeedbackHandlerCreate_AttachmentBindingRejected(t *testing.T) {
	// 测试目的：绑定他人附件 / 已绑定附件 / 超过 3 张 / 非法 uuid 一律 400 validation_error，
	// 且不产生部分绑定（反馈不落库、附件保持未绑定）。
	t.Run("绑定他人附件", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		foreign := uploadAttachmentOK(t, r, feedbackAttachmentOtherID, "other.png", attachmentPNG(64))

		body := `{"type":"bug","content":"尝试绑定别人的截图","attachment_ids":["` + foreign.ID.String() + `"]}`
		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		var resp struct {
			Error struct {
				Type string `json:"type"`
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "validation_error", resp.Error.Type)
		require.Equal(t, handler.CodeBadRequest, resp.Error.Code)

		var feedbackCount int64
		require.NoError(t, db.Model(&model.Feedback{}).Count(&feedbackCount).Error)
		require.Zero(t, feedbackCount, "绑定失败不得落库")

		row, err := repository.NewFeedbackAttachmentRepo(db).GetByID(foreign.ID)
		require.NoError(t, err)
		require.Nil(t, row.FeedbackID)
	})

	t.Run("已绑定附件重复提交", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		att := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "a.png", attachmentPNG(64))
		body := `{"type":"bug","content":"第一次提交并绑定","attachment_ids":["` + att.ID.String() + `"]}`
		require.Equal(t, http.StatusCreated,
			doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID).Code)

		again := `{"type":"bug","content":"第二次想重复绑定同一张","attachment_ids":["` + att.ID.String() + `"]}`
		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", again, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		var feedbackCount int64
		require.NoError(t, db.Model(&model.Feedback{}).Count(&feedbackCount).Error)
		require.Equal(t, int64(1), feedbackCount, "第二次提交不得落库")
	})

	t.Run("超过 3 张", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()}
		body := `{"type":"bug","content":"一次提交四张截图","attachment_ids":["` + strings.Join(ids, `","`) + `"]}`

		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		var feedbackCount int64
		require.NoError(t, db.Model(&model.Feedback{}).Count(&feedbackCount).Error)
		require.Zero(t, feedbackCount)
	})

	t.Run("attachment_ids 里有非法 uuid", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		body := `{"type":"bug","content":"带一个非法附件 id","attachment_ids":["not-a-uuid"]}`

		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID)

		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())
		var feedbackCount int64
		require.NoError(t, db.Model(&model.Feedback{}).Count(&feedbackCount).Error)
		require.Zero(t, feedbackCount)
	})

	t.Run("混合合法与他人的附件不产生半绑定", func(t *testing.T) {
		r, db := setupFeedbackAttachmentTestRouter(t)
		mine := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "mine.png", attachmentPNG(64))
		foreign := uploadAttachmentOK(t, r, feedbackAttachmentOtherID, "foreign.png", attachmentPNG(64))

		body := `{"type":"bug","content":"一张合法一张他人的截图","attachment_ids":["` + mine.ID.String() + `","` + foreign.ID.String() + `"]}`
		w := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", body, feedbackAttachmentOwnerID)
		require.Equal(t, http.StatusBadRequest, w.Code, "body = %s", w.Body.String())

		repo := repository.NewFeedbackAttachmentRepo(db)
		var feedbackCount int64
		require.NoError(t, db.Model(&model.Feedback{}).Count(&feedbackCount).Error)
		require.Zero(t, feedbackCount, "整体失败时不得落库")
		// 关键：合法的那张也不能被绑定（事务回滚），否则就是部分绑定
		for _, id := range []uuid.UUID{mine.ID, foreign.ID} {
			row, err := repo.GetByID(id)
			require.NoError(t, err)
			require.Nil(t, row.FeedbackID, "附件 %s 必须保持未绑定", id)
		}

		// 回滚后状态一致：只提交自己的附件应成功
		ok := `{"type":"bug","content":"重新只提交自己的截图","attachment_ids":["` + mine.ID.String() + `"]}`
		okW := doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", ok, feedbackAttachmentOwnerID)
		require.Equal(t, http.StatusCreated, okW.Code, "body = %s", okW.Body.String())
		row, err := repo.GetByID(mine.ID)
		require.NoError(t, err)
		require.NotNil(t, row.FeedbackID)
	})
}

// ===================== GET /api/v1/feedback（列表带附件元数据） =====================

func TestFeedbackHandlerList_IncludesAttachmentMetaWithoutData(t *testing.T) {
	// 测试目的：列表每个 item 带 attachments 元数据（id/file_name/mime/size_bytes/width/height/
	// created_at/feedback_id），绝不回字节；无附件的反馈返回空数组。
	r, db := setupFeedbackAttachmentTestRouter(t)
	seedFeedbackAttachmentAdminRole(t, db)

	att := uploadAttachmentOK(t, r, feedbackAttachmentOwnerID, "shot.png", attachmentPNG(256))
	withAtt := `{"type":"bug","content":"带一张截图的反馈","attachment_ids":["` + att.ID.String() + `"]}`
	require.Equal(t, http.StatusCreated,
		doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", withAtt, feedbackAttachmentOwnerID).Code)
	require.Equal(t, http.StatusCreated,
		doAttachmentJSON(r, http.MethodPost, "/api/v1/feedback", `{"type":"other","content":"没有截图的反馈"}`, feedbackAttachmentOwnerID).Code)

	w := doAttachmentJSON(r, http.MethodGet, "/api/v1/feedback", "", feedbackAttachmentAdminID)
	require.Equal(t, http.StatusOK, w.Code, "body = %s", w.Body.String())

	var resp struct {
		Items []struct {
			ID          string           `json:"id"`
			Content     string           `json:"content"`
			Attachments []map[string]any `json:"attachments"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, int64(2), resp.Total)
	require.Len(t, resp.Items, 2)

	var attachmentItems []map[string]any
	foundWithout := false
	for _, item := range resp.Items {
		if item.Content == "带一张截图的反馈" {
			attachmentItems = item.Attachments
			continue
		}
		if item.Content == "没有截图的反馈" {
			foundWithout = true
			// 无附件的反馈 attachments 必须是显式空数组而非 null
			require.NotNil(t, item.Attachments, "无附件时也应是空数组")
			require.Empty(t, item.Attachments)
		}
	}
	require.True(t, foundWithout, "应有一条不带附件的反馈")
	require.Len(t, attachmentItems, 1, "带附件反馈应回 1 条附件元数据")

	meta := attachmentItems[0]
	for _, key := range []string{"id", "file_name", "mime", "size_bytes", "width", "height", "created_at", "feedback_id"} {
		require.Contains(t, meta, key, "附件元数据缺少契约字段 %s", key)
	}
	// 关键：列表绝不回字节，也不泄露附件上传者
	require.NotContains(t, meta, "data")
	require.NotContains(t, meta, "user_id")
	require.Equal(t, att.ID.String(), meta["id"])
	require.Equal(t, "shot.png", meta["file_name"])
	require.Equal(t, "image/png", meta["mime"])
}
