package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是计划 3/4 任务 5 的 B 层用例：规格 §8.1 两条端点
// （管理面 GET /api/v1/trace/:request_id/body、内部 GET /internal/v1/trace-body）的
// 鉴权 / 越权 / 状态码 / **200 的响应字节**。
//
// 【断言不依赖宿主机状态】文件系统只用 t.TempDir()（只为 Source 诊断字段的两条 os.Stat），
// 归档取回由替身 reader 提供；sqlite 是内存库；内部 token 由 t.Setenv 注入并当场恢复。
// 不连 Redis / rbac / JWT（归属与可见性都是注入的替身，见 TraceBodyHandler 的构造签名）。
//
// 【200 是正文原字节】断言分三层，任一层都能单独失败：
//  ① Content-Length == len(服务层返回的正文)；
//  ② body 逐字节 == 服务层返回的正文（包壳 `{"body": …}` 会立刻红）；
//  ③ 顶层键集合恰好是正文形状的三个键（`body` / `ref` / `source` 等包装键一律不许出现）。

// tbWantBody 是替身 reader 返回的「正文原字节」：三键，键序与计划 2 的 trace_line.go 相同。
// 它同时是「不包壳」的承重证据 —— 任何包一层都会让字节与键集合两处断言一起红。
var tbWantBody = []byte(`{"request_headers":{"x-request-id":"req-ok"},"request_body":{"messages":[{"role":"user","content":"机密提示词"}]},"response_body":{"choices":[{"index":0}]}}`)

// tbWantKeys 是正文形状的顶层键集合（逐字，排序后比较）。
var tbWantKeys = []string{"request_body", "request_headers", "response_body"}

// tbFakeReader 是点读原语窄接口的替身（与任务 2/4 的 B 层同形）：
// 可编程返回 (bytes, error) 并统计调用次数。
//
// 【调用次数是承重的】它证明「归属判定与可见性判定都先于取正文」——
// 404/403 两条用例都断言 calls==0（越权请求不得触碰存储）。
type tbFakeReader struct {
	calls int
	body  []byte
	err   error
}

func (f *tbFakeReader) GetBody(_ context.Context, _ ta.Ref) ([]byte, error) {
	f.calls++
	return f.body, f.err
}

// tbTestDB 建内存 sqlite + AutoMigrate(&obsmodel.CallTrace{})。
func tbTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	return db
}

// tbSeedArchived 写入一行「正文在归档」的 trace：四个定位键全非 NULL、body_missing=false。
func tbSeedArchived(t *testing.T, db *gorm.DB, requestID string) {
	t.Helper()
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	inst := "blue"
	part := 0
	row := 3
	require.NoError(t, db.Create(&obsmodel.CallTrace{
		RequestID:   requestID,
		TS:          time.Now(),
		BodyRefDT:   &day,
		BodyRefInst: &inst,
		BodyRefPart: &part,
		BodyRefRow:  &row,
		BodyMissing: false,
	}).Error)
}

// tbEnabledConfig 是「读侧已启用」的配置（root 非空 ⇒ Disabled()==false）。
func tbEnabledConfig(t *testing.T) cpconfig.TraceBodyReadConfig {
	t.Helper()
	return cpconfig.TraceBodyReadConfig{Root: t.TempDir(), MaxConcurrent: 2, ZstdWindowBytes: 512 << 20}
}

// tbNewHandler 用给定配置装配真实服务 + 替身 reader + 注入的归属/可见性判定。
func tbNewHandler(t *testing.T, db *gorm.DB, reader *tbFakeReader, cfg cpconfig.TraceBodyReadConfig,
	subject obshandler.TraceBodySubjectFunc, canView obshandler.TraceBodyCanViewFunc) *obshandler.TraceBodyHandler {
	t.Helper()
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{Config: cfg, Reader: reader})
	require.NoError(t, err)
	return obshandler.NewTraceBodyHandler(svc, subject, canView)
}

// tbSubjectOK / tbViewOK 是「归属查到 + 可见」的默认替身。
func tbSubjectOK(context.Context, string) (string, string, bool) { return "user-1", "personal", true }
func tbViewOK(*gin.Context, string, bool) bool                   { return true }
func tbViewDenied(*gin.Context, string, bool) bool               { return false }

// tbAdminGET 把管理面端点挂到 URL 形态与生产一致的路由上并发一次请求。
func tbAdminGET(t *testing.T, h *obshandler.TraceBodyHandler, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/trace/:request_id/body", h.TraceBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/trace/"+requestID+"/body", nil))
	return w
}

// TestTraceBodyHandlerAdminRoute ← m04（成功响应包一层）的杀手
//
// 管理面命中归档 ⇒ 200，且 body / Content-Length / 顶层键集合三处都与
// **服务层返回的正文原字节**逐字一致。包一层 {"body": …} 会让三处中的两处立刻红。
func TestTraceBodyHandlerAdminRoute(t *testing.T) {
	db := tbTestDB(t)
	tbSeedArchived(t, db, "req-ok")
	fake := &tbFakeReader{body: tbWantBody}
	h := tbNewHandler(t, db, fake, tbEnabledConfig(t), tbSubjectOK, tbViewOK)

	w := tbAdminGET(t, h, "req-ok")

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, fake.calls, "归档命中必须真的调用一次点读原语")

	// ① Content-Type / Content-Length
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, strconv.Itoa(len(tbWantBody)), w.Header().Get("Content-Length"),
		"Content-Length 必须等于正文原字节长度")
	require.Equal(t, len(tbWantBody), w.Body.Len())
	// ② 逐字节
	require.Equal(t, tbWantBody, w.Body.Bytes(),
		"200 的 body 必须逐字节等于服务层返回的正文（不得包壳、不得重新序列化）")
	// ③ 顶层键集合：恰好是正文形状的三个键，没有包装键
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	require.Equal(t, tbWantKeys, keys, "响应字段集合必须与正文形状逐字相同（包壳会多出 body 键）")
	require.NotContains(t, got, "body")
	require.NotContains(t, got, "ref")
	require.NotContains(t, got, "source")
}

// TestTraceBodyHandlerForbidsWhenPromptNotVisible ← m02 的杀手
//
// 记录存在（服务本可取到正文）但 canView=false ⇒ **403**，且响应体不含正文任何字节。
// 返回 200 + 空正文会让「无权看」与「正文为空」不可区分（规格 §8.1 的 403 裁定）。
func TestTraceBodyHandlerForbidsWhenPromptNotVisible(t *testing.T) {
	db := tbTestDB(t)
	tbSeedArchived(t, db, "req-secret")
	fake := &tbFakeReader{body: tbWantBody}
	h := tbNewHandler(t, db, fake, tbEnabledConfig(t), tbSubjectOK, tbViewDenied)

	w := tbAdminGET(t, h, "req-secret")

	require.Equal(t, http.StatusForbidden, w.Code, "提示词不可见必须 403，不得 200 + 空正文")
	require.NotContains(t, w.Body.String(), "机密提示词")
	require.NotContains(t, w.Body.String(), string(tbWantBody))
	require.Zero(t, fake.calls, "可见性判定必须先于取正文（不得触碰存储）")
}

// TestTraceBodyHandlerNotFoundForUnknownRequest ← m01 的杀手
//
// 归属反查 found=false ⇒ **404**（不泄露存在性），即使 call_traces 里**确实有**这一行
// （服务层本可 200）。这证明归属判定**先于**取正文 —— 摘掉它即 IDOR。
func TestTraceBodyHandlerNotFoundForUnknownRequest(t *testing.T) {
	db := tbTestDB(t)
	// 关键：行存在，且服务层能取到正文。若归属校验被摘掉，本用例会红在 200 上。
	tbSeedArchived(t, db, "req-other-user")
	fake := &tbFakeReader{body: tbWantBody}
	h := tbNewHandler(t, db, fake, tbEnabledConfig(t),
		func(context.Context, string) (string, string, bool) { return "", "", false },
		tbViewOK)

	w := tbAdminGET(t, h, "req-other-user")

	require.Equal(t, http.StatusNotFound, w.Code, "归属未知必须按不存在处理（404），不得泄露存在性")
	require.NotContains(t, w.Body.String(), "机密提示词")
	require.NotContains(t, w.Body.String(), string(tbWantBody))
	require.Zero(t, fake.calls, "归属判定必须先于取正文（不得触碰存储）")
}

// TestTraceBodyHandlerInternalRouteNeedsToken
//
// 内部端点复用**既有** InternalAuth（不新建鉴权机制）：无 token / 错 token ⇒ 403，
// 对 token ⇒ 200，且 200 同样是正文原字节（与内部端点「不裁剪」的语义一致）。
func TestTraceBodyHandlerInternalRouteNeedsToken(t *testing.T) {
	const token = "trace-body-test-internal-token-9f3a"
	t.Setenv("INTERNAL_TOKEN", token)
	gin.SetMode(gin.TestMode)

	db := tbTestDB(t)
	tbSeedArchived(t, db, "req-internal")
	fake := &tbFakeReader{body: tbWantBody}
	h := tbNewHandler(t, db, fake, tbEnabledConfig(t), tbSubjectOK, tbViewOK)

	r := gin.New()
	g := r.Group("/internal/v1")
	g.Use(middleware.InternalAuth())
	g.GET("/trace-body", h.InternalTraceBody)

	do := func(tokenHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/trace-body?request_id=req-internal", nil)
		if tokenHeader != "" {
			req.Header.Set("X-Internal-Token", tokenHeader)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusForbidden, do("").Code, "缺 X-Internal-Token 必须 403（InternalAuth）")
	require.Equal(t, http.StatusForbidden, do("wrong-token").Code, "错 token 必须 403")

	w := do(token)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, tbWantBody, w.Body.Bytes(), "内部端点的 200 同样必须是正文原字节")
	require.Equal(t, strconv.Itoa(len(tbWantBody)), w.Header().Get("Content-Length"))
}

// TestTraceBodyHandlerMapsErrors ← m05 的杀手
//
// 服务层/存储层的每一类错误都必须经**任务 3 的映射层**变成对应的 HTTP 表现
// （错误一律 500 会让每一格都红）。
func TestTraceBodyHandlerMapsErrors(t *testing.T) {
	type testCase struct {
		name        string
		requestID   string
		seed        func(t *testing.T, db *gorm.DB, requestID string)
		disabled    bool
		readerErr   error
		wantStatus  int
		wantCode    string
		wantMissing bool
		// wantMessageContains 只在需要证明「底层文本原样透出」时设置。
		wantMessageContains string
		// wantCalls 钉住「禁用/缺失路径不得触碰点读原语」。
		wantCalls int
	}

	cases := []testCase{
		{
			name: "记录缺失", requestID: "req-absent",
			seed:       func(*testing.T, *gorm.DB, string) {},
			wantStatus: http.StatusNotFound, wantCode: "trace_not_found",
		},
		{
			name: "body_missing 标记行", requestID: "req-never-written",
			seed: func(t *testing.T, db *gorm.DB, requestID string) {
				require.NoError(t, db.Create(&obsmodel.CallTrace{
					RequestID: requestID, TS: time.Now(), BodyMissing: true,
				}).Error)
			},
			wantStatus: http.StatusNotFound, wantCode: "body_missing", wantMissing: true,
		},
		{
			name: "读侧未配置（带定位键）", requestID: "req-disabled",
			seed:       tbSeedArchivedRow,
			disabled:   true,
			wantStatus: http.StatusServiceUnavailable, wantCode: "trace_body_disabled", wantCalls: 0,
		},
		{
			name: "未归档（底层文本原样透出）", requestID: "req-not-archived",
			seed:       tbSeedArchivedRow,
			readerErr:  fmt.Errorf("%w: %s（块内共 %d 行）", ta.ErrNotArchived, "<ref>", 3),
			wantStatus: http.StatusInternalServerError, wantCode: "not_archived",
			wantMessageContains: "块内共 3 行",
			wantCalls:           1,
		},
		{
			name: "产物损坏", requestID: "req-corrupt",
			seed:       tbSeedArchivedRow,
			readerErr:  ta.ErrFooterCorrupt,
			wantStatus: http.StatusInternalServerError, wantCode: "part_corrupt",
			wantCalls: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := tbTestDB(t)
			tc.seed(t, db, tc.requestID)
			fake := &tbFakeReader{err: tc.readerErr}
			cfg := tbEnabledConfig(t)
			if tc.disabled {
				cfg = cpconfig.TraceBodyReadConfig{MaxConcurrent: 2, ZstdWindowBytes: 512 << 20}
			}
			h := tbNewHandler(t, db, fake, cfg, tbSubjectOK, tbViewOK)

			w := tbAdminGET(t, h, tc.requestID)

			require.Equal(t, tc.wantStatus, w.Code)
			var resp obshandler.TraceBodyErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Equal(t, tc.wantCode, resp.Error.Code)
			require.Equal(t, tc.wantMissing, resp.Error.BodyMissing)
			if tc.wantMessageContains != "" {
				require.Contains(t, resp.Error.Message, tc.wantMessageContains,
					"ErrNotArchived 的底层文本必须原样透出（运维据此分辨三种成因）")
			}
			if tc.wantStatus != http.StatusOK {
				require.NotContains(t, w.Body.String(), "机密提示词")
			}
			require.Equal(t, tc.wantCalls, fake.calls)
		})
	}
}

// tbSeedArchivedRow 是 tbSeedArchived 的「表驱动签名」版本（带 request_id）。
func tbSeedArchivedRow(t *testing.T, db *gorm.DB, requestID string) {
	t.Helper()
	tbSeedArchived(t, db, requestID)
}

// TestTraceBodyHandlerQueryParamMissing ← m03 的杀手
//
// /internal/v1/trace-body 不带 request_id ⇒ **400**（不是 500、不是空查询）。
// 直接放行空串会让服务拿 "" 去查库并把它伪装成「记录不存在」（404）。
func TestTraceBodyHandlerQueryParamMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := tbTestDB(t)
	fake := &tbFakeReader{body: tbWantBody}
	h := tbNewHandler(t, db, fake, tbEnabledConfig(t), tbSubjectOK, tbViewOK)

	r := gin.New()
	r.GET("/internal/v1/trace-body", h.InternalTraceBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/v1/trace-body", nil))

	require.Equal(t, http.StatusBadRequest, w.Code, "缺 request_id 必须 400（入参错误不得伪装成服务故障）")
	require.Zero(t, fake.calls, "缺参数不得触碰服务/存储")
	require.NotContains(t, w.Body.String(), "机密提示词")
}
