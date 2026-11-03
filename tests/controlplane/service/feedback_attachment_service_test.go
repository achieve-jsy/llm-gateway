package service_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// feedbackAttachmentTestDDL 是 feedbacks + feedback_attachments 在 sqlite 下的等价建表语句。
// 与既有反馈用例一致，不使用 AutoMigrate（模型带 gen_random_uuid()/uuid/bytea 等 PG 专属写法）。
const feedbackAttachmentTestDDL = `
CREATE TABLE feedbacks (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	user_name TEXT NOT NULL DEFAULT '',
	user_email TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL DEFAULT 'suggestion',
	content TEXT NOT NULL,
	contact TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	handled_at DATETIME,
	handled_by TEXT,
	handle_note TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
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

// openFeedbackAttachmentTestDB 打开一个独立内存 sqlite 库并建好两张表。
func openFeedbackAttachmentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err, "打开内存 sqlite")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// :memory: 每连接一库，限制单连接保证 service 与断言命中同一个库
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Exec(feedbackAttachmentTestDDL).Error, "建测试表")
	return db
}

// newFeedbackAttachmentService 构造被测 service，并返回底层 DB 供断言落库结果。
func newFeedbackAttachmentService(t *testing.T) (*service.FeedbackService, *gorm.DB) {
	t.Helper()
	db := openFeedbackAttachmentTestDB(t)
	return service.NewFeedbackService(repository.NewFeedbackRepo(db)), db
}

// pngBytes 造一段「魔数合法」的 PNG 字节：前 8 字节是 PNG 签名，其余补零。
// http.DetectContentType 只按前 512 字节的魔数判定，因此无需生成真正可解码的图片。
func pngBytes(size int) []byte {
	data := make([]byte, 0, size)
	data = append(data, 0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n')
	for len(data) < size {
		data = append(data, 0)
	}
	return data[:size]
}

// textBytes 一段普通文本，嗅探结果为 text/plain，用于验证白名单拒绝。
func textBytes() []byte { return []byte("这不是图片，只是一段普通文本内容。") }

// seedAttachmentRow 直接落一条附件记录，可指定 created_at / feedback_id，便于构造孤儿与已绑定样本。
func seedAttachmentRow(t *testing.T, db *gorm.DB, userID uuid.UUID, createdAt time.Time, feedbackID *uuid.UUID) model.FeedbackAttachment {
	t.Helper()
	row := model.FeedbackAttachment{
		ID:        uuid.New(),
		UserID:    userID,
		FileName:  "seed.png",
		Mime:      "image/png",
		Data:      pngBytes(64),
		SizeBytes: 64,
		CreatedAt: createdAt,
	}
	row.FeedbackID = feedbackID
	require.NoError(t, db.Create(&row).Error, "写入附件样本")
	return row
}

// ===================== 上传 =====================

func TestFeedbackAttachmentUpload_Success(t *testing.T) {
	// 测试目的：合法 PNG 落库为「未绑定孤儿」，元数据（mime/size/宽高/文件名/时间）正确，字节完整。
	svc, db := newFeedbackAttachmentService(t)
	userID := uuid.New()
	data := pngBytes(1024)

	att, err := svc.UploadAttachment(userID, "  截图.png  ", data, 1600, 900)
	require.NoError(t, err)
	require.NotNil(t, att)

	require.NotEqual(t, uuid.Nil, att.ID)
	require.Equal(t, "image/png", att.Mime, "mime 必须由服务端魔数嗅探得出")
	require.Equal(t, "截图.png", att.FileName, "文件名应去掉首尾空格")
	require.Equal(t, len(data), att.SizeBytes)
	require.Equal(t, 1600, att.Width)
	require.Equal(t, 900, att.Height)
	require.Nil(t, att.FeedbackID, "刚上传的附件必须是未绑定的孤儿")
	require.False(t, att.CreatedAt.IsZero(), "created_at 应被填充")

	stored, err := repository.NewFeedbackAttachmentRepo(db).GetByID(att.ID)
	require.NoError(t, err)
	require.Equal(t, userID, stored.UserID)
	require.Equal(t, data, stored.Data, "字节必须完整落库")
	require.Nil(t, stored.FeedbackID)
}

func TestFeedbackAttachmentUpload_FileNameSanitized(t *testing.T) {
	// 测试目的：文件名归一——剥路径、剔控制字符/引号（防响应头注入）、超长截断、空名兜底。
	tests := []struct {
		name        string
		description string
		input       string
		want        string
	}{
		{name: "剥离 POSIX 路径", description: "../../etc/passwd 应只留 basename", input: "../../etc/passwd", want: "passwd"},
		{name: "剥离 Windows 路径", description: `C:\Users\a\shot.png 只留文件名`, input: `C:\Users\a\shot.png`, want: "shot.png"},
		{name: "剔除双引号", description: "含双引号会破坏 Content-Disposition，应被剔除", input: `a"b.png`, want: "ab.png"},
		{name: "剔除非打印控制字符", description: "含 \\n 的文件名应被剔除换行符", input: "a\nb.png", want: "ab.png"},
		{name: "空名兜底", description: "空文件名使用 attachment 兜底", input: "", want: "attachment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newFeedbackAttachmentService(t)

			att, err := svc.UploadAttachment(uuid.New(), tt.input, pngBytes(32), 1, 1)
			require.NoError(t, err)
			require.Equal(t, tt.want, att.FileName)
		})
	}

	t.Run("超长文件名截断到 255 字符", func(t *testing.T) {
		svc, _ := newFeedbackAttachmentService(t)
		long := ""
		for i := 0; i < 300; i++ {
			long += "图"
		}
		att, err := svc.UploadAttachment(uuid.New(), long, pngBytes(32), 1, 1)
		require.NoError(t, err)
		require.Len(t, []rune(att.FileName), 255)
	})
}

func TestFeedbackAttachmentUpload_ValidationRejects(t *testing.T) {
	// 测试目的：空文件 / 超过 2MiB / 类型不在白名单 / 宽高非法一律 ErrFeedbackAttachmentInvalid，且不落库。
	tests := []struct {
		name        string
		description string
		fileName    string
		data        []byte
		width       int
		height      int
	}{
		{name: "空文件", description: "0 字节文件应被拒绝", fileName: "a.png", data: []byte{}, width: 1, height: 1},
		{name: "超过 2MiB", description: "2MiB+1 字节应被拒绝", fileName: "a.png", data: pngBytes(service.FeedbackAttachmentMaxBytes + 1), width: 1, height: 1},
		{name: "纯文本伪装成 png", description: "文件名后缀不可信，按魔数嗅探为 text/plain 应被拒绝", fileName: "fake.png", data: textBytes(), width: 1, height: 1},
		{name: "HTML 伪装成 gif", description: "HTML 嗅探为 text/html，不属于图片白名单", fileName: "x.gif", data: []byte("<html><body>hi</body></html>"), width: 1, height: 1},
		{name: "width 为负", description: "width=-1 非法", fileName: "a.png", data: pngBytes(32), width: -1, height: 1},
		{name: "height 为负", description: "height=-1 非法", fileName: "a.png", data: pngBytes(32), width: 1, height: -1},
		{name: "width 超过 20000", description: "width=20001 非法", fileName: "a.png", data: pngBytes(32), width: 20001, height: 1},
		{name: "height 超过 20000", description: "height=20001 非法", fileName: "a.png", data: pngBytes(32), width: 1, height: 20001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := newFeedbackAttachmentService(t)

			att, err := svc.UploadAttachment(uuid.New(), tt.fileName, tt.data, tt.width, tt.height)

			require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
			require.Nil(t, att)

			var count int64
			require.NoError(t, db.Model(&model.FeedbackAttachment{}).Count(&count).Error)
			require.Zero(t, count, "校验失败不应留下任何附件行")
		})
	}
}

func TestFeedbackAttachmentUpload_AcceptsWhitelistAndBoundaries(t *testing.T) {
	// 测试目的：四种白名单类型都能过，且「恰好 2MiB」「宽高恰好 20000」是包含边界。
	tests := []struct {
		name        string
		description string
		data        []byte
		wantMime    string
		width       int
		height      int
	}{
		{
			name:        "png 恰好 2MiB",
			description: "恰好 2097152 字节属于上闭区间，应通过",
			data:        pngBytes(service.FeedbackAttachmentMaxBytes),
			wantMime:    "image/png",
			width:       20000,
			height:      20000,
		},
		{
			name:        "jpeg",
			description: "JPEG 魔数 FF D8 FF 应被识别为 image/jpeg",
			data:        append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...),
			wantMime:    "image/jpeg",
		},
		{
			name:        "gif",
			description: "GIF89a 魔数应被识别为 image/gif",
			data:        append([]byte("GIF89a"), make([]byte, 64)...),
			wantMime:    "image/gif",
		},
		{
			name:        "webp",
			description: "RIFF....WEBPVP8 应被识别为 image/webp",
			data:        append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...),
			wantMime:    "image/webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newFeedbackAttachmentService(t)

			att, err := svc.UploadAttachment(uuid.New(), "a.bin", tt.data, tt.width, tt.height)

			require.NoError(t, err)
			require.Equal(t, tt.wantMime, att.Mime, "类型判定只认魔数，与文件名后缀无关")
			require.Equal(t, len(tt.data), att.SizeBytes)
		})
	}
}

// ===================== 下载鉴权（防越权） =====================

func TestFeedbackAttachmentGetForDownload_Authorization(t *testing.T) {
	// 测试目的：只有上传者本人或持有 feedback:manage 的请求者能拿到字节，
	// 其它已登录用户必须被拒（防止遍历 id 越权读他人截图）。
	svc, _ := newFeedbackAttachmentService(t)
	uploader := uuid.New()
	otherUser := uuid.New()
	adminUser := uuid.New()
	data := pngBytes(256)

	att, err := svc.UploadAttachment(uploader, "shot.png", data, 100, 100)
	require.NoError(t, err)

	tests := []struct {
		name        string
		description string
		requester   uuid.UUID
		canManage   bool
		wantErr     error
	}{
		{
			name:        "上传者本人无管理权限",
			description: "本人读自己的附件应放行",
			requester:   uploader,
			canManage:   false,
			wantErr:     nil,
		},
		{
			name:        "他人且无管理权限",
			description: "另一个普通登录用户必须被拒，否则等于可遍历 id 读别人截图",
			requester:   otherUser,
			canManage:   false,
			wantErr:     service.ErrFeedbackAttachmentForbidden,
		},
		{
			name:        "他人但持有 feedback:manage",
			description: "管理员可读任意附件",
			requester:   adminUser,
			canManage:   true,
			wantErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.GetAttachmentForDownload(att.ID, tt.requester, tt.canManage)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, got, "被拒时绝不能把字节返回给调用方")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, data, got.Data)
			require.Equal(t, "image/png", got.Mime)
		})
	}

	t.Run("附件不存在", func(t *testing.T) {
		// 即便持有管理权限，记录不存在也应返回 not found
		got, err := svc.GetAttachmentForDownload(uuid.New(), adminUser, true)
		require.ErrorIs(t, err, service.ErrFeedbackAttachmentNotFound)
		require.Nil(t, got)
	})
}

// ===================== 提交时绑定附件（原子性） =====================

func TestFeedbackSubmit_BindsAttachments(t *testing.T) {
	// 测试目的：attachment_ids 属于本人且未绑定 → 提交成功，且每个附件的 feedback_id 被写入新反馈 id。
	svc, db := newFeedbackAttachmentService(t)
	userID := uuid.New()

	first, err := svc.UploadAttachment(userID, "a.png", pngBytes(64), 1, 1)
	require.NoError(t, err)
	second, err := svc.UploadAttachment(userID, "b.png", pngBytes(96), 2, 2)
	require.NoError(t, err)

	f, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "提交时带两张截图", "", first.ID, second.ID)
	require.NoError(t, err)
	require.NotNil(t, f)

	repo := repository.NewFeedbackAttachmentRepo(db)
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		row, err := repo.GetByID(id)
		require.NoError(t, err)
		require.NotNil(t, row.FeedbackID, "附件应被绑定到新反馈")
		require.Equal(t, f.ID, *row.FeedbackID)
	}
}

func TestFeedbackSubmit_AttachmentBindingRejectedWithoutPartialBinding(t *testing.T) {
	// 测试目的：attachment_ids 里只要有一个不合法（不存在/不属于本人/已绑定/超量），
	// 整体返回 ErrFeedbackAttachmentInvalid，并且事务回滚成「反馈没落库 + 一张附件都没被绑定」。
	t.Run("绑定他人附件", func(t *testing.T) {
		svc, db := newFeedbackAttachmentService(t)
		owner, attacker := uuid.New(), uuid.New()
		foreign, err := svc.UploadAttachment(owner, "other.png", pngBytes(64), 1, 1)
		require.NoError(t, err)

		f, err := svc.Submit(attacker, "攻击者", "attacker@example.com", "bug", "尝试绑定别人的截图", "", foreign.ID)

		require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
		require.Nil(t, f)
		requireFeedbackCount(t, db, 0)
		requireAttachmentUnbound(t, db, foreign.ID)
	})

	t.Run("绑定已绑定附件", func(t *testing.T) {
		svc, db := newFeedbackAttachmentService(t)
		userID := uuid.New()
		att, err := svc.UploadAttachment(userID, "a.png", pngBytes(64), 1, 1)
		require.NoError(t, err)

		first, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "第一次提交并绑定", "", att.ID)
		require.NoError(t, err)

		f, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "第二次重复绑定同一张", "", att.ID)

		require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
		require.Nil(t, f)
		requireFeedbackCount(t, db, 1)
		// 仍然绑在第一次的反馈上，没有被改绑/清空
		row := requireAttachmentBound(t, db, att.ID)
		require.Equal(t, first.ID, *row.FeedbackID)
	})

	t.Run("超过 3 张", func(t *testing.T) {
		svc, db := newFeedbackAttachmentService(t)
		userID := uuid.New()
		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}

		f, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "一次提交四张截图", "", ids...)

		require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
		require.Nil(t, f)
		requireFeedbackCount(t, db, 0)
	})

	t.Run("混合合法与他人的附件不产生半绑定", func(t *testing.T) {
		svc, db := newFeedbackAttachmentService(t)
		userID, attacker := uuid.New(), uuid.New()
		mine, err := svc.UploadAttachment(userID, "mine.png", pngBytes(64), 1, 1)
		require.NoError(t, err)
		foreign, err := svc.UploadAttachment(attacker, "foreign.png", pngBytes(64), 1, 1)
		require.NoError(t, err)

		f, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "一张合法一张他人的截图", "", mine.ID, foreign.ID)

		require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
		require.Nil(t, f)
		requireFeedbackCount(t, db, 0)
		// 关键：合法的那张也不能被绑定，否则就是部分绑定
		requireAttachmentUnbound(t, db, mine.ID)
		requireAttachmentUnbound(t, db, foreign.ID)

		// 回滚后状态仍然一致：单独提交合法附件应成功
		ok, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "重新只提交自己的截图", "", mine.ID)
		require.NoError(t, err, "回滚不应破坏附件可用性")
		require.NotNil(t, ok)
		require.Equal(t, ok.ID, *requireAttachmentBound(t, db, mine.ID).FeedbackID)
	})

	t.Run("bound 失败时反馈不落库（事务回滚）", func(t *testing.T) {
		svc, db := newFeedbackAttachmentService(t)
		// 用一个不存在的附件 id：绑定影响行数 0 != 1，必须回滚掉刚创建的 feedback
		f, err := svc.Submit(uuid.New(), "测试用户", "tester@example.com", "bug", "引用了一个不存在的附件", "", uuid.New())

		require.ErrorIs(t, err, service.ErrFeedbackAttachmentInvalid)
		require.Nil(t, f)
		requireFeedbackCount(t, db, 0)
	})
}

func requireFeedbackCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.Feedback{}).Count(&count).Error)
	require.Equal(t, want, count, "反馈表记录数不符（事务未回滚？）")
}

func requireAttachmentBound(t *testing.T, db *gorm.DB, id uuid.UUID) model.FeedbackAttachment {
	t.Helper()
	row, err := repository.NewFeedbackAttachmentRepo(db).GetByID(id)
	require.NoError(t, err)
	require.NotNil(t, row.FeedbackID, "附件应处于已绑定状态")
	return *row
}

func requireAttachmentUnbound(t *testing.T, db *gorm.DB, id uuid.UUID) {
	t.Helper()
	row, err := repository.NewFeedbackAttachmentRepo(db).GetByID(id)
	require.NoError(t, err)
	require.Nil(t, row.FeedbackID, "附件必须保持未绑定（不能出现部分绑定）")
}

// ===================== 孤儿清理 =====================

func TestFeedbackAttachmentDeleteOrphansBefore(t *testing.T) {
	// 测试目的：只删「未绑定 且 created_at 早于 cutoff」的附件；
	// 已绑定的老附件、未过期的孤儿都必须保留。
	db := openFeedbackAttachmentTestDB(t)
	repo := repository.NewFeedbackAttachmentRepo(db)
	userID := uuid.New()
	now := time.Now()

	orphanExpired := seedAttachmentRow(t, db, userID, now.Add(-25*time.Hour), nil)
	orphanFresh := seedAttachmentRow(t, db, userID, now.Add(-1*time.Hour), nil)
	feedbackID := uuid.New()
	boundExpired := seedAttachmentRow(t, db, userID, now.Add(-48*time.Hour), &feedbackID)

	cutoff := now.Add(-service.FeedbackAttachmentOrphanTTL)
	deleted, err := repo.DeleteOrphansBefore(cutoff)

	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "只应删除 1 条过期孤儿")

	_, err = repo.GetByID(orphanExpired.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "过期孤儿应被删除")

	keptFresh, err := repo.GetByID(orphanFresh.ID)
	require.NoError(t, err, "未过期孤儿必须保留")
	require.Nil(t, keptFresh.FeedbackID)

	keptBound, err := repo.GetByID(boundExpired.ID)
	require.NoError(t, err, "已绑定附件即便很老也必须保留")
	require.NotNil(t, keptBound.FeedbackID)

	// 边界：cutoff 恰好等于 created_at 不删（严格小于）
	edge := seedAttachmentRow(t, db, userID, cutoff, nil)
	deleted, err = repo.DeleteOrphansBefore(cutoff)
	require.NoError(t, err)
	require.Zero(t, deleted, "created_at 恰好等于 cutoff 不是严格早于，应保留")
	_, err = repo.GetByID(edge.ID)
	require.NoError(t, err)
}

func TestFeedbackAttachmentUpload_TriggersOrphanCleanup(t *testing.T) {
	// 测试目的：上传路径顺带清理过期孤儿（契约要求），但不得误删已绑定或未过期的附件。
	svc, db := newFeedbackAttachmentService(t)
	userID := uuid.New()
	now := time.Now()

	expired := seedAttachmentRow(t, db, userID, now.Add(-25*time.Hour), nil)
	fresh := seedAttachmentRow(t, db, userID, now.Add(-time.Hour), nil)
	feedbackID := uuid.New()
	bound := seedAttachmentRow(t, db, userID, now.Add(-72*time.Hour), &feedbackID)

	att, err := svc.UploadAttachment(userID, "new.png", pngBytes(64), 1, 1)
	require.NoError(t, err)

	repo := repository.NewFeedbackAttachmentRepo(db)
	_, err = repo.GetByID(expired.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "上传应顺带删除过期孤儿")
	for _, id := range []uuid.UUID{fresh.ID, bound.ID, att.ID} {
		_, err := repo.GetByID(id)
		require.NoError(t, err, "不该误删的附件被删了: %s", id)
	}
}

// ===================== 列表附元数据 =====================

func TestFeedbackList_IncludesAttachmentMetaWithoutData(t *testing.T) {
	// 测试目的：列表项的 attachments 只含元数据（不含 data、不含 user_id），
	// 无附件的反馈返回显式空数组。
	svc, db := newFeedbackAttachmentService(t)
	userID := uuid.New()

	withAtt, err := svc.Submit(userID, "测试用户", "tester@example.com", "bug", "带两张截图的反馈", "")
	require.NoError(t, err)
	plain, err := svc.Submit(userID, "测试用户", "tester@example.com", "other", "没有截图的反馈", "")
	require.NoError(t, err)

	repo := repository.NewFeedbackAttachmentRepo(db)
	now := time.Now()
	for _, name := range []string{"first.png", "second.jpg"} {
		row := model.FeedbackAttachment{
			ID:         uuid.New(),
			UserID:     userID,
			FileName:   name,
			Mime:       "image/png",
			Data:       pngBytes(128),
			SizeBytes:  128,
			Width:      800,
			Height:     600,
			CreatedAt:  now,
			FeedbackID: &withAtt.ID,
		}
		require.NoError(t, repo.Create(&row))
	}

	items, total, err := svc.List("", "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)

	byID := map[uuid.UUID]service.FeedbackListItem{}
	for _, item := range items {
		byID[item.ID] = item
	}

	withAttachments := byID[withAtt.ID]
	require.Len(t, withAttachments.Attachments, 2)
	for _, att := range withAttachments.Attachments {
		require.NotEqual(t, uuid.Nil, att.ID)
		require.Equal(t, withAtt.ID, *att.FeedbackID)
		require.Equal(t, 128, att.SizeBytes)
		require.Equal(t, 800, att.Width)
		require.Equal(t, 600, att.Height)
		require.Equal(t, "image/png", att.Mime)
		require.False(t, att.CreatedAt.IsZero())
	}

	// 结构上就不含字节：附件元数据 DTO 没有 data / user_id 字段，序列化后也绝不可能出现
	raw, err := json.Marshal(withAttachments.Attachments)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"data"`)
	require.NotContains(t, string(raw), `"user_id"`)
	require.Contains(t, string(raw), `"feedback_id"`)

	require.NotNil(t, byID[plain.ID].Attachments, "无附件时也要是显式空数组而非 null")
	require.Empty(t, byID[plain.ID].Attachments)
}

func TestFeedbackList_AttachmentTableMissingDegrades(t *testing.T) {
	// 测试目的（兼容性护栏）：feedbacks 表存在但 feedback_attachments 尚未迁移时，
	// 列表必须降级为「无附件」而不是整个 500（沿用既有 newFeedbackService(false/true) 夹具）。
	svc, db := newFeedbackService(t, true) // 只建了 feedbacks 表
	seedFeedbacks(t, db, 1, service.FeedbackTypeBug, service.FeedbackStatusPending)

	items, total, err := svc.List("", "", 1, 20)
	require.NoError(t, err, "缺附件表时应降级而不是报错")
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Empty(t, items[0].Attachments)
}

func TestFeedbackAttachmentListMeta_EmptyIDsSkipsQuery(t *testing.T) {
	// 测试目的：ids 为空时不发查询（无表库也不报错），返回空 map。
	db := openFeedbackAttachmentTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&model.FeedbackAttachment{}))
	repo := repository.NewFeedbackAttachmentRepo(db)

	meta, err := repo.ListMetaByFeedbackIDs(nil)
	require.NoError(t, err)
	require.Empty(t, meta)
}
