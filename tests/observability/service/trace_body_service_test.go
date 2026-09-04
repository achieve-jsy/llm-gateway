package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 本文件是计划 3/4 任务 2 的 B 层用例：点读服务的**优先级判定**（记录缺失 /
// body_missing / 定位键全 NULL ⇒ jsonb / 全非 NULL ⇒ 归档 / 其余形态 ⇒ 服务层不一致）
// 与**哨兵透传**。它不碰真实产物、不依赖宿主机状态：注入替身 reader，文件系统只用
// t.TempDir() 下的空文件（仅供 Source 诊断字段的两条 os.Stat）。

// fakeReader 是点读原语窄接口（tracearchiveReader）的替身：
// 可编程返回 (bytes, error)，并统计调用次数与收到的定位键。
//
// 【调用次数是承重的】它证明「形态判定与 ValidateRef 先于任何存储访问」
// （TestTraceBodyServicePartialRefsAreInconsistent / TestTraceBodyServiceRejectsIllegalRefBeforeIO
// 的 calls==0 断言；m06 的杀手）。
type fakeReader struct {
	calls int
	refs  []ta.Ref
	body  []byte
	err   error
}

func (f *fakeReader) GetBody(ctx context.Context, ref ta.Ref) ([]byte, error) {
	f.calls++
	f.refs = append(f.refs, ref)
	return f.body, f.err
}

// newTraceBodyTestDB 建内存 sqlite + AutoMigrate(&obsmodel.CallTrace{})。
// DSN 用 t.Name() 保证每个用例（含子用例）一份独立库。
func newTraceBodyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	return db
}

// enabledReadConfig 是「读侧已启用」的配置（root 非空；根目录由调用方给 t.TempDir()）。
func enabledReadConfig(root string) cpconfig.TraceBodyReadConfig {
	return cpconfig.TraceBodyReadConfig{Root: root, MaxConcurrent: 2, ZstdWindowBytes: 512 << 20}
}

// disabledReadConfig 是「读侧禁用」的配置（Root 空 ⇒ Disabled()==true）。
// 这是「功能关闭」在读侧的唯一表达（生产上只能由 YAML 一侧产生，见全局约束 E6）。
func disabledReadConfig() cpconfig.TraceBodyReadConfig {
	return cpconfig.TraceBodyReadConfig{MaxConcurrent: 2, ZstdWindowBytes: 512 << 20}
}

// fullRefRow 返回一行带**完整定位键**的 trace：dt=2026-09-24 / inst=blue / part=0 / row=3，
// body_missing=false。dt 用 time.UTC 构造，读侧的 dt 只由这一列的 Format("2006-01-02") 决定。
func fullRefRow(requestID string) obsmodel.CallTrace {
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	inst := "blue"
	part := 0
	row := 3
	return obsmodel.CallTrace{
		RequestID:   requestID,
		TS:          time.Now(),
		BodyRefDT:   &day,
		BodyRefInst: &inst,
		BodyRefPart: &part,
		BodyRefRow:  &row,
		BodyMissing: false,
	}
}

// TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing ← m01 的杀手
//
// 「4 个定位键全 NULL + body_missing=false」必须解释为「正文在 jsonb 列」并返回 200，
// **不是**「缺失」。计划 2 的功能关闭路径（TRACE_BODY_ROOT 留空 ⇒ 正文仍进 jsonb）
// 与构造失败路径（sink==nil）都落在这个形态；把它读成缺失会让功能关闭变成 404，
// 即静默丢正文（全局约束 D55）。
//
// 两格（判定顺序的 2a / 2b 是同一结果，实现里合并成一处返回）：
//   - 读侧禁用（Root 空）：jsonb 回退是**正常路径**；
//   - 读侧启用：功能关闭期间写下的历史行同样走 jsonb 回退。
func TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing(t *testing.T) {
	cases := []struct {
		name string
		cfg  func() cpconfig.TraceBodyReadConfig
	}{
		{"读侧禁用", disabledReadConfig},
		{"读侧启用", func() cpconfig.TraceBodyReadConfig { return enabledReadConfig(t.TempDir()) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTraceBodyTestDB(t)
			require.NoError(t, db.Create(&obsmodel.CallTrace{
				RequestID:   "req-jsonb",
				TS:          time.Now(),
				RequestBody: datatypes.JSON([]byte(`{"i":1}`)),
			}).Error)
			// 替身故意返回「错误」：jsonb 回退【不得】触碰归档（若触碰，本用例会红在 err 上）
			fake := &fakeReader{err: errors.New("jsonb 回退路径不得访问归档")}
			svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
				Config: tc.cfg(),
				Reader: fake,
			})
			require.NoError(t, err)

			res, err := svc.GetBody(context.Background(), "req-jsonb")
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, obssvc.SourceJSONB, res.Source)
			require.Zero(t, fake.calls, "jsonb 回退不得访问存储")

			var got struct {
				RequestBody map[string]int `json:"request_body"`
			}
			require.NoError(t, json.Unmarshal(res.Body, &got))
			require.Equal(t, map[string]int{"i": 1}, got.RequestBody)

			// 形状与计划 2 的 trace_line.go 一致：NULL 列**不出现键**（omitempty），
			// 而不是出现 "request_headers":null。
			require.NotContains(t, string(res.Body), "request_headers")
			require.NotContains(t, string(res.Body), "response_body")
		})
	}
}

// TestTraceBodyServiceBodyMissingWinsOverEverything ← m02 的杀手
//
//	① body_missing=true 且定位键全 NULL ⇒ ErrBodyMissing（正文从未落盘，404 类）
//	② body_missing=true 却带定位键 ⇒ ErrTraceBodyInconsistent（服务层 500）
//
// ② 是承重的：它不能报成 404 —— 那是把「数据写坏」伪装成业务事实
// （全局约束「四者之外的形态 ⇒ 500 + 明确诊断，绝不报 404」）。
// 两条都必须在**任何存储访问之前**判决（calls==0）。
func TestTraceBodyServiceBodyMissingWinsOverEverything(t *testing.T) {
	db := newTraceBodyTestDB(t)
	require.NoError(t, db.Create(&obsmodel.CallTrace{
		RequestID:   "req-missing-norefs",
		TS:          time.Now(),
		BodyMissing: true,
	}).Error)
	withRefs := fullRefRow("req-missing-withrefs")
	withRefs.BodyMissing = true
	require.NoError(t, db.Create(&withRefs).Error)

	fake := &fakeReader{body: []byte(`{"request_body":{"i":1}}`)}
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(t.TempDir()),
		Reader: fake,
	})
	require.NoError(t, err)

	// ① 从未落盘
	res, err := svc.GetBody(context.Background(), "req-missing-norefs")
	require.Nil(t, res)
	require.ErrorIs(t, err, ta.ErrBodyMissing)

	// ② body_missing 与定位键矛盾 ⇒ 500，且【不得】被伪装成 404
	res2, err2 := svc.GetBody(context.Background(), "req-missing-withrefs")
	require.Nil(t, res2)
	require.ErrorIs(t, err2, obssvc.ErrTraceBodyInconsistent)
	require.NotErrorIs(t, err2, ta.ErrBodyMissing)

	require.Zero(t, fake.calls, "两条判决都必须先于任何存储访问")
}

// TestTraceBodyServiceReadsArchiveWhenRefsPresent ← m04 的杀手
//
// 4 个定位键全非 NULL ⇒ 调用 Reader.GetBody 并**逐字节**返回它给的原字节（归档路径
// 是「写入时那一行的原字节」，规格 G3）；同时钉住 Source 诊断字段的产物 → spool 顺序，
// 以及定位键的构造（dt 来自行内 body_ref_dt 列的布局格式化，读侧零日期计算 E2）。
func TestTraceBodyServiceReadsArchiveWhenRefsPresent(t *testing.T) {
	const wantBody = `{"request_headers":{"h":"v"},"request_body":{"i":1},"response_body":{"o":2}}`
	ref := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 3}

	cases := []struct {
		name       string
		wantSource obssvc.TraceBodySource
		// 布置哪一个文件存在（内容无语义：替身 reader 不读文件系统）
		place func(t *testing.T, paths ta.Paths)
	}{
		{
			name:       "产物存在",
			wantSource: obssvc.SourceArchive,
			place: func(t *testing.T, paths ta.Paths) {
				require.NoError(t, os.MkdirAll(paths.PartDir(ref.DT, ref.Inst), 0o755))
				require.NoError(t, os.WriteFile(paths.PartFile(ref), []byte("part"), 0o644))
			},
		},
		{
			name:       "只有未封块的spool",
			wantSource: obssvc.SourceSpool,
			place: func(t *testing.T, paths ta.Paths) {
				require.NoError(t, os.MkdirAll(paths.SpoolDir(ref.DT, ref.Inst), 0o755))
				require.NoError(t, os.WriteFile(paths.SpoolFile(ref), []byte("spool"), 0o644))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTraceBodyTestDB(t)
			row := fullRefRow("req-archive")
			require.NoError(t, db.Create(&row).Error)

			root := t.TempDir()
			paths := ta.NewPaths(root)
			tc.place(t, paths)

			fake := &fakeReader{body: []byte(wantBody)}
			svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
				Config: enabledReadConfig(root),
				Reader: fake,
			})
			require.NoError(t, err)

			res, err := svc.GetBody(context.Background(), "req-archive")
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, []byte(wantBody), res.Body, "归档路径必须逐字节返回原字节")
			require.Equal(t, tc.wantSource, res.Source)

			require.Equal(t, 1, fake.calls)
			require.Equal(t, ref, fake.refs[0], "定位键必须由列值构造（dt 按日期布局格式化）")
		})
	}
}

// TestTraceBodyServiceExportsRequestRefOnHit ← m09 的杀手（任务 7 的 CLI `--ref` 的承重证据）
//
// 任务 7 的 `--ref` 必须输出 dt/inst/part/row【四分量】且**复用服务层**；若服务层不导出
// 本次实际使用的定位键，CLI 只能自己查 call_traces 的四列再拼 —— 那正是 D59 警告的
// 「同一判据两处实现」（本计划登记 E14）。本用例钉住导出字段 `TraceBodyResult.Ref` 的语义：
//
//	① 归档命中（含未封块 spool）⇒ Ref 四分量与请求行的四列一一对应（dt 是布局格式化），
//	   且**就是**送给 Reader 的那一个（不得另建一个 ref）；
//	② jsonb 回退（四列全 NULL）⇒ 没有定位键可报 ⇒ **零值**（不得拼出半个路径）。
func TestTraceBodyServiceExportsRequestRefOnHit(t *testing.T) {
	wantRef := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 3}

	t.Run("归档命中导出四分量", func(t *testing.T) {
		db := newTraceBodyTestDB(t)
		row := fullRefRow("req-ref-archive")
		require.NoError(t, db.Create(&row).Error)

		root := t.TempDir()
		paths := ta.NewPaths(root)
		require.NoError(t, os.MkdirAll(paths.PartDir(wantRef.DT, wantRef.Inst), 0o755))
		require.NoError(t, os.WriteFile(paths.PartFile(wantRef), []byte("part"), 0o644))

		fake := &fakeReader{body: []byte(`{"request_body":{"i":1}}`)}
		svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
			Config: enabledReadConfig(root),
			Reader: fake,
		})
		require.NoError(t, err)

		res, err := svc.GetBody(context.Background(), "req-ref-archive")
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, wantRef, res.Ref, "归档命中路径必须导出与请求一致的定位键四分量")
		require.Len(t, fake.refs, 1)
		require.Equal(t, wantRef, fake.refs[0], "导出的 Ref 必须就是送给 Reader 的那一个")
	})

	t.Run("jsonb回退导出零值", func(t *testing.T) {
		db := newTraceBodyTestDB(t)
		require.NoError(t, db.Create(&obsmodel.CallTrace{
			RequestID:   "req-ref-jsonb",
			TS:          time.Now(),
			RequestBody: datatypes.JSON([]byte(`{"i":1}`)),
		}).Error)

		fake := &fakeReader{err: errors.New("jsonb 回退不得访问归档")}
		svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
			Config: enabledReadConfig(t.TempDir()),
			Reader: fake,
		})
		require.NoError(t, err)

		res, err := svc.GetBody(context.Background(), "req-ref-jsonb")
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, obssvc.SourceJSONB, res.Source)
		require.Zero(t, res.Ref, "jsonb 回退没有定位键（四列全 NULL）⇒ Ref 必须是零值")
		require.Equal(t, ta.Ref{}, res.Ref, "不得拼出半个路径（例如 dt 空而 part 有值）")
		require.Zero(t, fake.calls)
	})
}

// TestTraceBodyServicePartialRefsAreInconsistent ← m03 的杀手
//
// 4 个定位键的**非空真子集**（单键 / 双键 / 三键，共 14 种）都必须 ErrTraceBodyInconsistent：
// 绝不 jsonb 回退（会把数据写坏伪装成「正文就是空的」）、绝不 404、绝不访问存储。
func TestTraceBodyServicePartialRefsAreInconsistent(t *testing.T) {
	db := newTraceBodyTestDB(t)
	fake := &fakeReader{body: []byte(`{"request_body":{"i":1}}`)}
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(t.TempDir()),
		Reader: fake,
	})
	require.NoError(t, err)

	// mask 的 4 个 bit 分别对应 dt / inst / part / row「非 NULL」。
	// 【订正恒真断言】原稿用 `seen++` + `require.Equal(t, 14, seen)`：`seen` 与循环体同构
	// （每轮恰加一），循环少跑/跳格都不会红 ⇒ 无证伪能力。这里改为两段式：
	//   ① 期望集合由「4 个二值定位键的全部 16 种形态」显式推导（排除 0=全 NULL 与
	//      15=全非 NULL，它们不是「部分 NULL」）；
	//   ② 断言实际驱动的形态集合与期望集合**逐元素相等**（少跑、跳格、多跑都会红），
	//      再用 Len 钉住总数 2^4-2 = 14（防止两段一起被改窄时静默通过）。
	const refKeyCount = 4
	const allMasks = 1 << refKeyCount
	wantMasks := make([]int, 0, allMasks-2)
	for mask := 0; mask < allMasks; mask++ {
		if mask != 0 && mask != allMasks-1 {
			wantMasks = append(wantMasks, mask)
		}
	}
	seenMasks := make([]int, 0, len(wantMasks))
	for _, mask := range wantMasks {
		requestID := fmt.Sprintf("req-partial-%d", mask)
		row := fullRefRow(requestID)
		if mask&1 == 0 {
			row.BodyRefDT = nil
		}
		if mask&2 == 0 {
			row.BodyRefInst = nil
		}
		if mask&4 == 0 {
			row.BodyRefPart = nil
		}
		if mask&8 == 0 {
			row.BodyRefRow = nil
		}
		require.NoError(t, db.Create(&row).Error)

		res, err := svc.GetBody(context.Background(), requestID)
		require.Nil(t, res, "mask=%d 不得回退成 jsonb", mask)
		require.ErrorIs(t, err, obssvc.ErrTraceBodyInconsistent, "mask=%d", mask)
		require.NotErrorIs(t, err, ta.ErrBodyMissing, "mask=%d", mask)
		require.NotErrorIs(t, err, ta.ErrNotArchived, "mask=%d", mask)
		seenMasks = append(seenMasks, mask)
	}
	require.Equal(t, wantMasks, seenMasks,
		"必须逐一驱动四个定位键的全部非空真子集（少跑/跳格即红）")
	require.Len(t, seenMasks, 14, "非空真子集 = 2^4-2 = 14 种（断言实际驱动数，不再自计数）")
	require.Zero(t, fake.calls, "部分 NULL 必须在任何存储访问之前被拦下")
}

// TestTraceBodyServiceDisabledWithRefsIs503 ← m05 的杀手
//
// 读侧禁用（Root 空）但行**带定位键** ⇒ ErrTraceBodyDisabled（503 类）：元数据指向归档
// 而读侧未配置属**部署错误**，既不是 404（正文不存在），也不是 jsonb 回退
// （把部署错误伪装成「正文在 jsonb 列」）。
func TestTraceBodyServiceDisabledWithRefsIs503(t *testing.T) {
	db := newTraceBodyTestDB(t)
	row := fullRefRow("req-disabled")
	require.NoError(t, db.Create(&row).Error)

	// 读侧禁用时不构造 Reader（NewReader 对空 Root 报错）⇒ Reader 保持 nil。
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: disabledReadConfig(),
		Reader: nil,
	})
	require.NoError(t, err, "禁用态允许 Reader 为 nil（有定位键的行会在碰存储前报 503）")

	res, err := svc.GetBody(context.Background(), "req-disabled")
	require.Nil(t, res)
	require.ErrorIs(t, err, obssvc.ErrTraceBodyDisabled)
	require.NotErrorIs(t, err, ta.ErrBodyMissing)
	require.NotErrorIs(t, err, ta.ErrNotArchived)
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
}

// TestTraceBodyServiceRejectsIllegalRefBeforeIO ← m06 的杀手 + 隔离探针
//
// 列值非法（模拟数据被写坏）⇒ ErrTraceBodyInconsistent，且替身 reader 的调用次数必须为 0
// —— 调用次数是承重的：它证明 ValidateRef 先于任何存储访问（纪律 1）。
//
// 【可达的非法向量只有 inst / part / row】dt 列是 `*time.Time`（type:date），
// `.Format("2006-01-02")` 恒产出合法日期 ⇒ 服务层**无法**构造非法 dt（计划文本的
// 「dt 值是 2026-09-24/../..」在 *time.Time 列上不可构造，见登记 E12）。dt 的守卫
// 由任务 1 的配置层用例（ValidateRef 的 dt 子例）承担。
//
// 隔离探针：本用例只覆盖 ValidateRef 这一道守卫；把 ValidateRef 摘掉（m06）之后
// 只有本用例转红，部分 NULL（另一道守卫）的用例仍全绿。
func TestTraceBodyServiceRejectsIllegalRefBeforeIO(t *testing.T) {
	illegal := []struct {
		name  string
		mutit func(row *obsmodel.CallTrace)
	}{
		{"inst穿越向量", func(row *obsmodel.CallTrace) { v := "../../green"; row.BodyRefInst = &v }},
		{"inst空串", func(row *obsmodel.CallTrace) { v := ""; row.BodyRefInst = &v }},
		{"part超上界", func(row *obsmodel.CallTrace) { v := 1000000; row.BodyRefPart = &v }},
		{"part为负", func(row *obsmodel.CallTrace) { v := -1; row.BodyRefPart = &v }},
		{"row为负", func(row *obsmodel.CallTrace) { v := -1; row.BodyRefRow = &v }},
	}
	db := newTraceBodyTestDB(t)
	fake := &fakeReader{body: []byte(`{"request_body":{"i":1}}`)}
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(t.TempDir()),
		Reader: fake,
	})
	require.NoError(t, err)

	for _, tc := range illegal {
		t.Run(tc.name, func(t *testing.T) {
			requestID := "req-illegal-" + tc.name
			row := fullRefRow(requestID)
			tc.mutit(&row)
			require.NoError(t, db.Create(&row).Error)

			res, err := svc.GetBody(context.Background(), requestID)
			require.Nil(t, res)
			require.ErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
			require.NotErrorIs(t, err, ta.ErrNotArchived)
			require.Zero(t, fake.calls, "非法定位键必须在任何存储访问之前被拒绝")
		})
	}
}

// TestTraceBodyServiceRecordNotFound ← 记录缺失的杀手
//
// call_traces 无该 request_id ⇒ 透传 gorm.ErrRecordNotFound（handler 映射 404
// trace_not_found）。它既不是 body_missing，也不是存储不一致。
func TestTraceBodyServiceRecordNotFound(t *testing.T) {
	db := newTraceBodyTestDB(t)
	fake := &fakeReader{body: []byte(`{"request_body":{"i":1}}`)}
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(t.TempDir()),
		Reader: fake,
	})
	require.NoError(t, err)

	res, err := svc.GetBody(context.Background(), "req-absent")
	require.Nil(t, res)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NotErrorIs(t, err, ta.ErrBodyMissing)
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
	require.Zero(t, fake.calls)
}

// TestTraceBodyServicePassthroughSentinels ← m07 的杀手
//
// 冻结包的四类哨兵必须**原样透传**（errors.Is 成立、底层诊断文本保留）：
// 把存储故障换成别的哨兵（或换成服务层的不一致）会让上层无法分类，
// 也把「元数据与存储不一致」伪装成「数据写坏」。
func TestTraceBodyServicePassthroughSentinels(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotArchived", ta.ErrNotArchived},
		{"ErrBodyMissing", ta.ErrBodyMissing},
		{"ErrFooterCorrupt", ta.ErrFooterCorrupt},
		{"ErrTrailerCorrupt", ta.ErrTrailerCorrupt},
	}
	db := newTraceBodyTestDB(t)

	for _, tc := range sentinels {
		t.Run(tc.name, func(t *testing.T) {
			requestID := "req-sentinel-" + tc.name
			// 每格一份替身：断言这一格自己的错误身份，不被上一格遮蔽。
			fake := &fakeReader{err: tc.err}
			env, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
				Config: enabledReadConfig(t.TempDir()),
				Reader: fake,
			})
			require.NoError(t, err)
			row := fullRefRow(requestID)
			require.NoError(t, db.Create(&row).Error)

			res, err := env.GetBody(context.Background(), requestID)
			require.Nil(t, res)
			require.ErrorIs(t, err, tc.err)
			require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
			require.NotErrorIs(t, err, obssvc.ErrTraceBodyDisabled)
			require.Contains(t, err.Error(), tc.err.Error(), "底层诊断文本必须保留（不得换成固定文案）")
			require.Equal(t, 1, fake.calls)
		})
	}
}

// TestTraceBodyServiceConstructorRejectsUnusableOptions 钉住构造期的两道否决：
// 没有 db（点读必须能查 call_traces）与「读侧已启用却没给 Reader」（归档路径会在
// 运行时对 nil 接口调用并 panic）。禁用态允许 Reader 为 nil —— 这正是任务 5 在
// 「读侧禁用」时的接线形态（有定位键的行先报 503，不碰存储）。
func TestTraceBodyServiceConstructorRejectsUnusableOptions(t *testing.T) {
	_, err := obssvc.NewTraceBodyService(nil, obssvc.TraceBodyServiceOptions{
		Config: disabledReadConfig(),
	})
	require.Error(t, err)

	db := newTraceBodyTestDB(t)
	_, err = obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(t.TempDir()),
		Reader: nil,
	})
	require.Error(t, err)

	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: disabledReadConfig(),
		Reader: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, svc)
}
