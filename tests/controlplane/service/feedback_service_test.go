package service_test

import (
	"strings"
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

// feedbackServiceTestDDL 是 feedbacks 表在 sqlite 下的等价建表语句。
// 不使用 AutoMigrate：模型带 gen_random_uuid()/uuid/timestamptz 等 PG 专属写法，
// 与 tests/controlplane/handler、tests/orgservice 既有用例一致，这里用原生 DDL。
const feedbackServiceTestDDL = `
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
	-- 模型上 created_at/updated_at 带 default 标签，GORM 在零值时不会写入这两列，
	-- 因此 sqlite 侧必须提供默认值，否则提交会撞 NOT NULL 约束
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

// openFeedbackServiceTestDB 打开一个独立的内存 sqlite 库；withTable=false 时故意不建 feedbacks 表。
// 不建表的用途：校验必须在访问 repo 之前完成，一旦校验被绕过就会拿到 DB 错误，
// 这样「纯逻辑用例」既能覆盖错误分支，又不会因为 repo 被调用而 panic。
func openFeedbackServiceTestDB(t *testing.T, withTable bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err, "打开内存 sqlite")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// :memory: 每个连接一个库，限制为单连接保证所有查询命中同一个库
	sqlDB.SetMaxOpenConns(1)

	if withTable {
		require.NoError(t, db.Exec(feedbackServiceTestDDL).Error, "建 feedbacks 表")
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// newFeedbackService 构造被测 service，并返回底层 DB 供断言落库结果。
func newFeedbackService(t *testing.T, withTable bool) (*service.FeedbackService, *gorm.DB) {
	t.Helper()
	db := openFeedbackServiceTestDB(t, withTable)
	return service.NewFeedbackService(repository.NewFeedbackRepo(db)), db
}

// seedFeedbacks 批量写入 count 条反馈，created_at 依次递增，便于验证分页偏移。
func seedFeedbacks(t *testing.T, db *gorm.DB, count int, ftype, status string) {
	t.Helper()
	rows := make([]model.Feedback, 0, count)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		rows = append(rows, model.Feedback{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			UserName:  "测试用户",
			UserEmail: "tester@example.com",
			Type:      ftype,
			Content:   "测试反馈内容",
			Status:    status,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
			UpdatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	require.NoError(t, db.CreateInBatches(rows, 100).Error, "批量写入反馈")
}

// ===================== Submit 校验（纯逻辑，不访问 repo） =====================

func TestFeedbackSubmit_Validation(t *testing.T) {
	// 测试目的：入参校验必须发生在落库之前，非法入参统一返回 ErrFeedbackInvalidInput。
	tests := []struct {
		name        string
		description string
		ftype       string
		content     string
		contact     string
	}{
		{
			name:        "非法 type",
			description: "type 传未定义值，应返回 ErrFeedbackInvalidInput",
			ftype:       "feature",
			content:     "这是一个合法长度的反馈内容",
		},
		{
			name:        "content 去空格后不足 5 字符",
			description: "content 只有 4 个字符，去空格后长度 4 < 5，应返回 ErrFeedbackInvalidInput",
			ftype:       "bug",
			content:     "abcd",
		},
		{
			name:        "content 全为空白字符",
			description: "content 只有空格，去空格后长度为 0，应返回 ErrFeedbackInvalidInput",
			ftype:       "suggestion",
			content:     "     ",
		},
		{
			name:        "content 超过 2000 字符",
			description: "content 长度 2001 > 2000，应返回 ErrFeedbackInvalidInput",
			ftype:       "other",
			content:     strings.Repeat("a", 2001),
		},
		{
			name:        "contact 超过 128 字符",
			description: "contact 长度 129 > 128，应返回 ErrFeedbackInvalidInput",
			ftype:       "usage",
			content:     "这是一个合法长度的反馈内容",
			contact:     strings.Repeat("a", 129),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// withTable=false：repo 指向没有 feedbacks 表的库，校验若被绕过会返回 DB 错误而非本错误
			svc, _ := newFeedbackService(t, false)

			f, err := svc.Submit(uuid.New(), "测试用户", "tester@example.com", tt.ftype, tt.content, tt.contact)

			require.ErrorIs(t, err, service.ErrFeedbackInvalidInput)
			require.Nil(t, f)
		})
	}
}

// ===================== Submit 成功路径 =====================

func TestFeedbackSubmit_TypeNormalization(t *testing.T) {
	// 测试目的：type 缺省为空时归一为 suggestion，显式传入的合法 type 原样保留。
	tests := []struct {
		name        string
		description string
		ftype       string
		wantType    string
	}{
		{
			name:        "type 为空归一为 suggestion",
			description: "请求未带 type，落库类型应为 suggestion",
			ftype:       "",
			wantType:    service.FeedbackTypeSuggestion,
		},
		{
			name:        "显式 bug 保留",
			description: "请求 type=bug，落库类型应为 bug",
			ftype:       service.FeedbackTypeBug,
			wantType:    service.FeedbackTypeBug,
		},
		{
			name:        "显式 usage 保留",
			description: "请求 type=usage，落库类型应为 usage",
			ftype:       service.FeedbackTypeUsage,
			wantType:    service.FeedbackTypeUsage,
		},
		{
			name:        "显式 other 保留",
			description: "请求 type=other，落库类型应为 other",
			ftype:       service.FeedbackTypeOther,
			wantType:    service.FeedbackTypeOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := newFeedbackService(t, true)
			userID := uuid.New()

			f, err := svc.Submit(userID, "测试用户", "tester@example.com", tt.ftype, "这是一个合法长度的反馈内容", "")
			require.NoError(t, err)
			require.NotNil(t, f)

			// 返回值与落库记录都应是归一后的 type，初始状态固定为 pending
			require.Equal(t, tt.wantType, f.Type)
			require.Equal(t, service.FeedbackStatusPending, f.Status)
			require.Equal(t, userID, f.UserID)

			stored, err := repository.NewFeedbackRepo(db).GetByID(f.ID)
			require.NoError(t, err)
			require.Equal(t, tt.wantType, stored.Type)
			require.Equal(t, service.FeedbackStatusPending, stored.Status)
		})
	}
}

func TestFeedbackSubmit_LengthBoundariesAccepted(t *testing.T) {
	// 测试目的：content 合法长度边界（去空格后恰好 5 / 2000）与 contact 恰好 128 字符都应通过。
	tests := []struct {
		name        string
		description string
		content     string
		wantContent string
		contact     string
	}{
		{
			name:        "content 去空格后恰好 5 字符",
			description: "content 为 5 字符（含首尾空格），去空格后长度 5，应通过并保存去空格结果",
			content:     "  hello  ",
			wantContent: "hello",
		},
		{
			name:        "content 恰好 2000 字符",
			description: "content 长度 2000 处于上边界，应通过",
			content:     strings.Repeat("a", 2000),
			wantContent: strings.Repeat("a", 2000),
		},
		{
			name:        "contact 恰好 128 字符",
			description: "contact 长度 128 处于上边界，应通过",
			content:     "这是一个合法长度的反馈内容",
			wantContent: "这是一个合法长度的反馈内容",
			contact:     strings.Repeat("c", 128),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newFeedbackService(t, true)

			f, err := svc.Submit(uuid.New(), "测试用户", "tester@example.com", "suggestion", tt.content, tt.contact)
			require.NoError(t, err)
			require.NotNil(t, f)
			require.Equal(t, tt.wantContent, f.Content, "content 应保存去首尾空格后的结果")
		})
	}
}

// ===================== 状态校验 =====================

func TestIsValidFeedbackStatus(t *testing.T) {
	// 测试目的：合法状态放行、非法状态拒绝，handler 的 400/200 分支依赖该判定。
	tests := []struct {
		name        string
		description string
		status      string
		want        bool
	}{
		{name: "pending 合法", description: "pending 是契约内的合法状态", status: "pending", want: true},
		{name: "processing 合法", description: "processing 是契约内的合法状态", status: "processing", want: true},
		{name: "resolved 合法", description: "resolved 是契约内的合法状态", status: "resolved", want: true},
		{name: "closed 合法", description: "closed 是契约内的合法状态", status: "closed", want: true},
		{name: "空字符串非法", description: "空状态不属于契约取值，应拒绝", status: "", want: false},
		{name: "未定义状态非法", description: "bogus 不属于契约取值，应拒绝", status: "bogus", want: false},
		{name: "大小写敏感", description: "Pending 不等于 pending，应拒绝", status: "Pending", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, service.IsValidFeedbackStatus(tt.status))
		})
	}
}

func TestFeedbackUpdateStatus_InvalidStatusRejected(t *testing.T) {
	// 测试目的：状态非法时必须在查库之前返回 ErrFeedbackInvalidInput。
	for _, status := range []string{"", "bogus", "REJECTED"} {
		t.Run("状态="+status, func(t *testing.T) {
			svc, _ := newFeedbackService(t, false) // 无 feedbacks 表，绕过校验就会拿到 DB 错误

			f, err := svc.UpdateStatus(uuid.New(), status, "备注", uuid.New())

			require.ErrorIs(t, err, service.ErrFeedbackInvalidInput)
			require.Nil(t, f)
		})
	}
}

func TestFeedbackUpdateStatus_NotFound(t *testing.T) {
	// 测试目的：状态合法但记录不存在时，应返回 ErrFeedbackNotFound（handler 据此返回 404）。
	svc, _ := newFeedbackService(t, true)

	f, err := svc.UpdateStatus(uuid.New(), service.FeedbackStatusResolved, "已处理", uuid.New())

	require.ErrorIs(t, err, service.ErrFeedbackNotFound)
	require.Nil(t, f)
}

func TestFeedbackUpdateStatus_ResolvedWritesHandlerFields(t *testing.T) {
	// 测试目的：流转到 resolved 时写入 handled_at / handled_by / handle_note（契约要求）。
	svc, db := newFeedbackService(t, true)
	operatorID := uuid.New()

	created, err := svc.Submit(uuid.New(), "测试用户", "tester@example.com", "bug", "页面在提交后白屏", "")
	require.NoError(t, err)
	require.Nil(t, created.HandledAt)
	require.Nil(t, created.HandledBy)

	updated, err := svc.UpdateStatus(created.ID, service.FeedbackStatusResolved, "已修复并发布", operatorID)
	require.NoError(t, err)

	require.Equal(t, service.FeedbackStatusResolved, updated.Status)
	require.Equal(t, "已修复并发布", updated.HandleNote)
	require.NotNil(t, updated.HandledAt, "resolved 时应写入处理时间")
	require.NotNil(t, updated.HandledBy, "resolved 时应写入处理人")
	require.Equal(t, operatorID, *updated.HandledBy)

	// 再确认一次落库结果，排除仅返回值被改写的情况
	stored, err := repository.NewFeedbackRepo(db).GetByID(created.ID)
	require.NoError(t, err)
	require.Equal(t, service.FeedbackStatusResolved, stored.Status)
	require.NotNil(t, stored.HandledAt)
	require.Equal(t, operatorID, *stored.HandledBy)
}

// ===================== List 筛选与分页归一 =====================

func TestFeedbackList_InvalidFiltersRejected(t *testing.T) {
	// 测试目的：过滤条件非法时必须在查库之前返回 ErrFeedbackInvalidInput。
	tests := []struct {
		name        string
		description string
		status      string
		ftype       string
	}{
		{name: "非法 status", description: "status=bogus 不属于契约取值，应拒绝", status: "bogus"},
		{name: "非法 type", description: "type=feature 不属于契约取值，应拒绝", ftype: "feature"},
		{name: "status 与 type 均非法", description: "两个过滤条件都非法，仍应返回校验错误", status: "closed_x", ftype: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newFeedbackService(t, false) // 无 feedbacks 表，绕过校验就会拿到 DB 错误

			items, total, err := svc.List(tt.status, tt.ftype, 1, 20)

			require.ErrorIs(t, err, service.ErrFeedbackInvalidInput)
			require.Nil(t, items)
			require.Zero(t, total)
		})
	}
}

func TestFeedbackList_ValidFiltersPass(t *testing.T) {
	// 测试目的：合法 status/type 组合应正常放行，并按条件过滤。
	svc, db := newFeedbackService(t, true)
	seedFeedbacks(t, db, 3, service.FeedbackTypeBug, service.FeedbackStatusPending)
	seedFeedbacks(t, db, 2, service.FeedbackTypeBug, service.FeedbackStatusResolved)
	seedFeedbacks(t, db, 4, service.FeedbackTypeSuggestion, service.FeedbackStatusPending)

	tests := []struct {
		name        string
		description string
		status      string
		ftype       string
		wantTotal   int64
	}{
		{name: "不传过滤条件", description: "status/type 均为空表示不过滤，应返回全部 9 条", wantTotal: 9},
		{name: "按 status 过滤", description: "status=pending 命中 bug 3 条 + suggestion 4 条", status: service.FeedbackStatusPending, wantTotal: 7},
		{name: "按 type 过滤", description: "type=bug 命中全部 5 条", ftype: service.FeedbackTypeBug, wantTotal: 5},
		{name: "status + type 组合过滤", description: "status=resolved 且 type=bug 命中 2 条", status: service.FeedbackStatusResolved, ftype: service.FeedbackTypeBug, wantTotal: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := svc.List(tt.status, tt.ftype, 1, 20)
			require.NoError(t, err)
			require.Equal(t, tt.wantTotal, total)
			require.Len(t, items, int(tt.wantTotal))
		})
	}
}

func TestFeedbackList_PaginationNormalization(t *testing.T) {
	// 测试目的：page < 1 归一为 1、pageSize < 1 归一为 20、pageSize > 100 截断为 100。
	// 归一结果无法直接从返回值读出，因此用「返回条数」间接断言：共 120 条，不同入参下命中数不同。
	svc, db := newFeedbackService(t, true)
	seedFeedbacks(t, db, 120, service.FeedbackTypeSuggestion, service.FeedbackStatusPending)

	tests := []struct {
		name        string
		description string
		page        int
		pageSize    int
		wantLen     int
	}{
		{
			name:        "page 与 pageSize 均小于 1",
			description: "page=0/pageSize=0 归一到 1/20，返回最新 20 条",
			page:        0,
			pageSize:    0,
			wantLen:     20,
		},
		{
			name:        "page 为负数",
			description: "page=-5 归一到 1，pageSize=3 时返回最新 3 条",
			page:        -5,
			pageSize:    3,
			wantLen:     3,
		},
		{
			name:        "pageSize 超过上限",
			description: "pageSize=500 截断为 100，返回 100 条",
			page:        1,
			pageSize:    500,
			wantLen:     100,
		},
		{
			name:        "pageSize 等于上限",
			description: "pageSize=100 无需截断，返回 100 条",
			page:        1,
			pageSize:    100,
			wantLen:     100,
		},
		{
			name:        "第二页取剩余记录",
			description: "page=2/pageSize=100，偏移 100 后只剩 20 条",
			page:        2,
			pageSize:    100,
			wantLen:     20,
		},
		{
			name:        "超出末页返回空列表",
			description: "page=99 已越界，应返回空切片且 total 仍为 120",
			page:        99,
			pageSize:    20,
			wantLen:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := svc.List("", "", tt.page, tt.pageSize)

			require.NoError(t, err)
			require.Equal(t, int64(120), total)
			require.Len(t, items, tt.wantLen)
		})
	}
}
