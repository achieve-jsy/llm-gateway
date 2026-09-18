package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是计划 3/4 任务 6 的 B 层用例：**既有** trace 端点
// （GET /api/v1/usage/logs/:request_id/trace → Trace/MyTrace、
//   GET /api/v1/usage/scoped/logs/:request_id/trace → ScopedTrace）
// 的正文来源改由点读服务提供之后的回归面。
//
// 【断言不依赖宿主机状态】sqlite 是内存库；正文由替身点读服务（`utaFakeHydrator`）
// 提供；点读端点的归档原语由替身 reader（`utaFakeReader`）提供；slog 输出捕获到内存
// buffer 并在 t.Cleanup 里还原；不连 Redis / PG / rbac 后端。
//
// 【四条承重不变量】
//  1. 归档形态（三个 jsonb 列全 NULL + 四个定位键全非 NULL）⇒ 正文来自点读服务；
//  2. 功能关闭形态（定位键全 NULL）⇒ 正文来自 jsonb，**不调用**点读服务；
//  3. 裁剪（`MaskTracePrompt`）必须作用在**回填之后**：归档提示词不得越过
//     `rbac.CanViewPrompt` 的可见性判定；
//  4. 范围/归属拒绝必须**先于**取正文（替身调用次数 == 0）。

// utaArchiveHeaders / utaArchivePrompt / utaArchiveResponse 是「归档正文」的三个键，
// 逐字对应计划 2 的 trace_line.go 形状与点读服务的 TraceBody 形状。
const (
	utaArchiveHeaders  = `{"x-request-id":"req-archived"}`
	utaArchivePrompt   = `{"messages":[{"role":"user","content":"归档机密提示词"}]}`
	utaArchiveResponse = `{"choices":[{"index":0}]}`
)

// utaJSONBHeaders / utaJSONBPrompt / utaJSONBResponse 是「功能关闭」形态下 jsonb 三列的原值。
// 它们与归档三键**内容不同**，这是「来源是否被替换」的证据（内容比对可判）。
const (
	utaJSONBHeaders  = `{"x-request-id":"req-jsonb"}`
	utaJSONBPrompt   = `{"messages":[{"role":"user","content":"jsonb 提示词"}]}`
	utaJSONBResponse = `{"choices":[{"index":1}]}`
)

// utaArchiveBody 是替身点读服务返回的正文原字节（三键）。
var utaArchiveBody = []byte(`{"request_headers":` + utaArchiveHeaders +
	`,"request_body":` + utaArchivePrompt + `,"response_body":` + utaArchiveResponse + `}`)

// utaFakeHydrator 是点读服务窄接口 `obshandler.TraceBodyHydrator` 的替身：
// 可编程返回 (result, error) 并统计调用次数。
//
// 【调用次数是承重的】它是「功能关闭形态不得调用点读服务」「拒绝先于取正文」
// 两类要求的**隔离探针**：只做内容比对时，两路内容等价会让断言退化成恒真。
type utaFakeHydrator struct {
	calls  int
	result *obssvc.TraceBodyResult
	err    error
}

var _ obshandler.TraceBodyHydrator = (*utaFakeHydrator)(nil)

func (f *utaFakeHydrator) GetBody(_ context.Context, _ string) (*obssvc.TraceBodyResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// utaFakeReader 是点读原语窄接口的替身（与任务 2/4/5 的 B 层同形）。
type utaFakeReader struct {
	calls int
	body  []byte
	err   error
}

func (f *utaFakeReader) GetBody(_ context.Context, _ ta.Ref) ([]byte, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.body, nil
}

// utaTestDB 建内存 sqlite 并迁移 call_traces + call_logs。
//
// 【为什么必须建 call_logs】既有端点的归属反查（`LookupTraceSubject`）查的是
// call_logs（call_traces 没有 user_id / key_type 列）——它正是本任务「同一套判据」
// 的承重路径，缺表即恒缺归属。
func utaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}, &obsmodel.CallLog{}))
	return db
}

// utaSeedTrace 写入一行 call_traces。
// archive=true ⇒ 归档形态（jsonb 三列全 NULL + 四个定位键全非 NULL，body_missing=false）；
// archive=false ⇒ 功能关闭形态（jsonb 三列有值 + 定位键全 NULL）。
func utaSeedTrace(t *testing.T, db *gorm.DB, requestID string, archive bool) {
	t.Helper()
	tr := obsmodel.CallTrace{
		RequestID: requestID,
		TS:        time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}
	if archive {
		day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
		inst, part, row := "blue", 0, 3
		tr.BodyRefDT, tr.BodyRefInst, tr.BodyRefPart, tr.BodyRefRow = &day, &inst, &part, &row
	} else {
		tr.RequestHeaders = datatypes.JSON([]byte(utaJSONBHeaders))
		tr.RequestBody = datatypes.JSON([]byte(utaJSONBPrompt))
		tr.ResponseBody = datatypes.JSON([]byte(utaJSONBResponse))
	}
	require.NoError(t, db.Create(&tr).Error)
}

// utaSeedCallLog 写入一行 call_logs（归属反查的唯一来源）。userID 为空串即模拟
// 「归属未知」的历史数据形态。
func utaSeedCallLog(t *testing.T, db *gorm.DB, requestID, userID, keyType string) {
	t.Helper()
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: requestID,
		TS:        time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		UserID:    userID,
		KeyType:   keyType,
	}).Error)
}

// utaRequest 构造一次带 request_id 路径参数的 gin 上下文与响应记录器。
func utaRequest(actor rbac.Actor, requestID string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "request_id", Value: requestID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/usage/logs/"+requestID+"/trace", nil)
	rbac.SetActor(c, actor)
	return c, w
}

// utaNewUsageHandler 装配既有端点 handler 并注入替身点读服务（可选依赖）。
func utaNewUsageHandler(t *testing.T, db *gorm.DB, fake *utaFakeHydrator) *obshandler.UsageHandler {
	t.Helper()
	h := obshandler.NewUsageHandler(nil, db)
	if fake != nil {
		h.SetTraceBodyService(fake)
	}
	return h
}

// utaEnabledConfig 是「读侧已启用」的配置（root 非空 ⇒ Disabled()==false）。
func utaEnabledConfig(t *testing.T) cpconfig.TraceBodyReadConfig {
	t.Helper()
	return cpconfig.TraceBodyReadConfig{Root: t.TempDir(), MaxConcurrent: 2, ZstdWindowBytes: 512 << 20}
}

// utaTopLevelKeys 返回 200 响应 JSON 的顶层键（排序后），并断言**不存在**本计划可能引入的
// 包装键 / 定位键泄漏字段。排序后逐字比较 ⇒ 键集合多一个、少一个都会红。
func utaTopLevelKeys(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, banned := range []string{"ref", "source", "body", "trace"} {
		require.NotContains(t, m, banned, "响应不得出现包装键 / 定位键泄漏字段")
	}
	return keys
}

// TestUsageTraceHydratesBodyFromArchive ← m01（归档回填被摘掉）的杀手
//
// 归档形态行 + 替身服务返回固定三键 ⇒ 200 响应的三个正文字段等于归档三键的内容，
// 且服务**恰被调用一次**。
func TestUsageTraceHydratesBodyFromArchive(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-hydrate-archive"
	utaSeedCallLog(t, db, rid, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, true)

	fake := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	h := utaNewUsageHandler(t, db, fake)

	c, w := utaRequest(rbac.Actor{IsGlobal: true}, rid)
	h.Trace(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, fake.calls, "归档形态必须调用点读服务一次")
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.JSONEq(t, utaArchiveHeaders, string(got["request_headers"]))
	require.JSONEq(t, utaArchivePrompt, string(got["request_body"]))
	require.JSONEq(t, utaArchiveResponse, string(got["response_body"]))
	require.JSONEq(t, "false", string(got["body_missing"]))
}

// TestUsageTraceKeepsJSONBWhenNoRefs ← m02（无条件回填）的杀手
//
// 功能关闭形态（jsonb 三列有值 + 定位键全 NULL）⇒ 正文**仍来自 jsonb**，
// 且点读服务**调用次数 == 0**（隔离探针：两路内容不同，内容比对未替换）。
func TestUsageTraceKeepsJSONBWhenNoRefs(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-keep-jsonb"
	utaSeedCallLog(t, db, rid, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, false)

	fake := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	h := utaNewUsageHandler(t, db, fake)

	c, w := utaRequest(rbac.Actor{IsGlobal: true}, rid)
	h.Trace(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Zero(t, fake.calls, "功能关闭形态（定位键全 NULL）不得调用点读服务")
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.JSONEq(t, utaJSONBHeaders, string(got["request_headers"]))
	require.JSONEq(t, utaJSONBPrompt, string(got["request_body"]))
	require.JSONEq(t, utaJSONBResponse, string(got["response_body"]))
}

// TestUsageTraceMarksBodyMissing ← m03（body_missing 标志不下发）的杀手
//
// 行本身 body_missing=false（jsonb 全 NULL、定位键全非 NULL），点读服务报
// ErrBodyMissing ⇒ 响应仍 200、正文留空、且既有 `body_missing` 字段**置真**。
//
// 【为什么行本身必须是 false】若种子行 body_missing=true，响应里的 true 来自模型本身，
// 断言就恒真、无法证明 handler 的置真分支承重。
func TestUsageTraceMarksBodyMissing(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-body-missing"
	utaSeedCallLog(t, db, rid, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, true)

	fake := &utaFakeHydrator{err: ta.ErrBodyMissing}
	h := utaNewUsageHandler(t, db, fake)

	c, w := utaRequest(rbac.Actor{IsGlobal: true}, rid)
	h.Trace(c)

	require.Equal(t, http.StatusOK, w.Code, "正文从未落盘不得升级成 5xx（元数据仍有价值）")
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.JSONEq(t, "true", string(got["body_missing"]),
		"ErrBodyMissing ⇒ 既有 body_missing 字段必须置真（既有字段、只改取值语义）")
	require.NotContains(t, got, "request_body")
	require.NotContains(t, got, "response_body")
}

// TestUsageTraceArchiveFailureKeeps200 ← m04（失败不记日志）的杀手
//
// 替身返回 ErrNotArchived ⇒ 仍然 200、正文留空、且**产生一条 WARN 级日志**
// （含 request_id 与定位键）——日志存在性是唯一可观测信号，承重。
func TestUsageTraceArchiveFailureKeeps200(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-archive-failure"
	utaSeedCallLog(t, db, rid, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, true)

	fake := &utaFakeHydrator{err: fmt.Errorf("tracearchive: 产物与暂存均不存在: %w", ta.ErrNotArchived)}
	h := utaNewUsageHandler(t, db, fake)

	var buf bytes.Buffer
	old := slog.Default()
	// 【级别必须是默认的 Info】写死 Level=Debug 会让 m04（Warn→Debug）无法被判红。
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{})))
	t.Cleanup(func() { slog.SetDefault(old) })

	c, w := utaRequest(rbac.Actor{IsGlobal: true}, rid)
	h.Trace(c)

	require.Equal(t, http.StatusOK, w.Code, "存储故障不得把「trace 看不到」升级成 500")
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotContains(t, got, "request_body")
	require.NotContains(t, got, "response_body")

	logs := buf.String()
	require.Contains(t, logs, "trace body hydrate failed", "正文取不到必须留下响亮的可观测信号")
	require.Contains(t, logs, "level=WARN", "信号必须是 WARN 级（Debug 会被默认级别过滤掉）")
	require.Contains(t, logs, "request_id="+rid)
	require.Contains(t, logs, "ref_inst=blue", "日志必须带上定位键，否则无法排障")
}

// TestUsageTracePreservesRBACOnArchivedBody ← m05（回填绕过裁剪）的杀手
//
// 非 admin 且非记录本人 ⇒ canViewPrompt=false ⇒ **归档**提示词不得出现在响应里；
// 但正文**仍应被取回一次**（裁剪作用于新来源，而不是靠「不取正文」来掩盖）。
// 安全承重：正文来源换了，裁剪时机若被挪到取正文之前就会漏。
func TestUsageTracePreservesRBACOnArchivedBody(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-rbac-archive"
	utaSeedCallLog(t, db, rid, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, true)

	fake := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	h := utaNewUsageHandler(t, db, fake)

	// 归属 user-1、请求者 user-2（非全局）⇒ rbac.CanViewPrompt=false。
	c, w := utaRequest(rbac.Actor{UserID: "user-2"}, rid)
	h.Trace(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, fake.calls, "裁剪必须作用于【新来源】：正文应先被取回、再被裁掉")
	require.NotContains(t, w.Body.String(), "归档机密提示词",
		"归档提示词不得越过 rbac.CanViewPrompt 的裁剪")
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotContains(t, got, "request_body")
	require.JSONEq(t, utaArchiveResponse, string(got["response_body"]),
		"裁剪只作用于提示词：响应正文与元数据保持原样")
}

// TestScopedTracePreservesScopeOnArchivedBody ← m06（范围外也取正文）的杀手
//
// 范围外记录 ⇒ 404（不泄露存在性），且**不发生**任何点读调用；
// 阳性对照：同一行、范围内的请求者 ⇒ 200 且恰取回一次（否则 404 断言恒真）。
func TestScopedTracePreservesScopeOnArchivedBody(t *testing.T) {
	db := utaTestDB(t)
	const rid = "req-scoped-archive"
	// 记录归属 user-2；请求者 user-1（非全局、无 studio/dept 管辖）⇒ 范围外。
	utaSeedCallLog(t, db, rid, "user-2", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, rid, true)

	fake := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	h := utaNewUsageHandler(t, db, fake)
	c, w := utaRequest(rbac.Actor{UserID: "user-1"}, rid)
	h.ScopedTrace(c)

	require.Equal(t, http.StatusNotFound, w.Code, "范围外记录必须按不存在处理")
	require.Zero(t, fake.calls, "范围校验必须先于取正文")
	require.NotContains(t, w.Body.String(), "归档机密提示词")

	fakeIn := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	hIn := utaNewUsageHandler(t, db, fakeIn)
	cIn, wIn := utaRequest(rbac.Actor{UserID: "user-2"}, rid)
	hIn.ScopedTrace(cIn)

	require.Equal(t, http.StatusOK, wIn.Code, "范围内必须能读到（否则上面的 404 断言恒真）")
	require.Equal(t, 1, fakeIn.calls)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(wIn.Body.Bytes(), &got))
	require.JSONEq(t, utaArchivePrompt, string(got["request_body"]))
}

// TestUsageTraceResponseKeySetUnchanged — 【E19 承重用例】响应键集合不被本任务动过
//
// 两格固定输入，逐字断言 200 响应的顶层键集合（排序后完整键列表）：
//
//	① 归档形态行 ⇒ 含四个既有的 body_ref_* 键（json tag 早已存在且带 omitempty）；
//	② 功能关闭形态行 ⇒ 不含它们。
//
// 两格都断言**不存在** `ref` / `source` / `body` / `trace` 这些本计划可能引入的包装键。
// 判据：键集合只由 `obsmodel.CallTrace` 的既有 json tag 决定；本任务只改**取值**
// （归档正文回填），不引入任何新键，也不把 `TraceBodyResult.Ref` 并进响应。
func TestUsageTraceResponseKeySetUnchanged(t *testing.T) {
	db := utaTestDB(t)

	const ridArchive = "req-keyset-archive"
	utaSeedCallLog(t, db, ridArchive, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, ridArchive, true)
	hA := utaNewUsageHandler(t, db, &utaFakeHydrator{
		result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive},
	})
	cA, wA := utaRequest(rbac.Actor{IsGlobal: true}, ridArchive)
	hA.Trace(cA)
	require.Equal(t, http.StatusOK, wA.Code)
	require.Equal(t, []string{
		"body_missing", "body_ref_dt", "body_ref_inst", "body_ref_part", "body_ref_row",
		"request_body", "request_headers", "request_id", "response_body", "ts",
	}, utaTopLevelKeys(t, wA), "归档形态的顶层键集合必须与改造前逐字相同")

	const ridJSONB = "req-keyset-jsonb"
	utaSeedCallLog(t, db, ridJSONB, "user-1", obsmodel.KeyTypePersonal)
	utaSeedTrace(t, db, ridJSONB, false)
	hB := utaNewUsageHandler(t, db, &utaFakeHydrator{
		result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive},
	})
	cB, wB := utaRequest(rbac.Actor{IsGlobal: true}, ridJSONB)
	hB.Trace(cB)
	require.Equal(t, http.StatusOK, wB.Code)
	require.Equal(t, []string{
		"body_missing", "request_body", "request_headers", "request_id", "response_body", "ts",
	}, utaTopLevelKeys(t, wB), "功能关闭形态的顶层键集合必须与改造前逐字相同")
}

// TestLookupTraceSubjectRejectsUnknownOrEmptyOwner — 【E32 承接】归属 fail-closed 的行为证据
//
// `LookupTraceSubject` 在此之前**零用例、零变异**（`grep -rn "traceSubject|LookupTraceSubject|
// \.Trace(|ScopedTrace" tests/` ⇒ 0 命中）。本用例固定它的两条 fail-closed 分支，
// 并同时证明「拒绝发生在取正文之前」：
//
//	① call_logs 无该 request_id 行；② 有行但 user_id 为空；
//	既有端点（MyTrace）与点读端点（subject 注入**生产实现** LookupTraceSubject，
//	与 router.go 逐字同形）**都拒绝**（404），且两侧都不发生取正文
//	（替身点读服务 / 替身 reader 的调用次数均为 0）。
//	阳性对照：归属完整 ⇒ MyTrace 200 且恰取回一次（证明两格 404 不是恒真）。
func TestLookupTraceSubjectRejectsUnknownOrEmptyOwner(t *testing.T) {
	db := utaTestDB(t)

	const ridUnknown = "req-subject-unknown"        // ① call_logs 无行
	const ridEmptyOwner = "req-subject-empty-owner" // ② 有行但 user_id 为空
	const ridOK = "req-subject-ok"                  // 阳性对照
	for _, rid := range []string{ridUnknown, ridEmptyOwner, ridOK} {
		utaSeedTrace(t, db, rid, true)
	}
	utaSeedCallLog(t, db, ridEmptyOwner, "", obsmodel.KeyTypePersonal)
	utaSeedCallLog(t, db, ridOK, "user-1", obsmodel.KeyTypePersonal)

	for _, rid := range []string{ridUnknown, ridEmptyOwner} {
		// 既有端点（MyTrace，ownOnly=true）：归属不满足 ⇒ 404，且不触碰点读服务。
		fake := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
		h := utaNewUsageHandler(t, db, fake)
		c, w := utaRequest(rbac.Actor{IsGlobal: true}, rid)
		h.MyTrace(c)
		require.Equal(t, http.StatusNotFound, w.Code,
			"既有端点：归属不满足必须按不存在处理（%s）", rid)
		require.Zero(t, fake.calls, "既有端点：拒绝必须先于取正文（%s）", rid)
		require.NotContains(t, w.Body.String(), "归档机密提示词")

		// 点读端点：subject 注入生产实现 ⇒ 同一判据必须同样拒绝，且 reader 不被调用。
		reader := &utaFakeReader{body: utaArchiveBody}
		svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
			Config: utaEnabledConfig(t),
			Reader: reader,
		})
		require.NoError(t, err)
		subject := func(ctx context.Context, requestID string) (string, string, bool) {
			return obshandler.LookupTraceSubject(ctx, db, requestID)
		}
		tbH := obshandler.NewTraceBodyHandler(svc, subject, func(*gin.Context, string, bool) bool { return true })
		c2, w2 := utaRequest(rbac.Actor{IsGlobal: true}, rid)
		tbH.TraceBody(c2)
		require.Equal(t, http.StatusNotFound, w2.Code,
			"点读端点：同一个归属判据必须同样拒绝（%s）", rid)
		require.Zero(t, reader.calls, "点读端点：拒绝必须先于取正文（%s）", rid)
	}

	// 阳性对照：归属完整（call_logs.user_id 非空）⇒ 既有端点 200 且恰取回一次。
	fakeOK := &utaFakeHydrator{result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive}}
	hOK := utaNewUsageHandler(t, db, fakeOK)
	cOK, wOK := utaRequest(rbac.Actor{IsGlobal: true}, ridOK)
	hOK.MyTrace(cOK)
	require.Equal(t, http.StatusOK, wOK.Code, "归属完整必须能读到（否则上面的 404 断言恒真）")
	require.Equal(t, 1, fakeOK.calls, "归属完整时正文必须真的被取回一次")
}
