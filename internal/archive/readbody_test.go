// readbody_test.go —— WS-F：v3 分块点读链路（_SUCCESS 门闸 → index → FindPart → 解压 → 字节预筛）。
//
// 契约 §4 ReadBody 流程、§5.2、§8 N10（write=false / read=true 时点读仍可用）。
package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadBodyV3_HitAcrossParts 覆盖 v3 点读主链路：命中首/中/末块，且正文字节原样返回。
func TestReadBodyV3_HitAcrossParts(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19/"

	rids := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		rids = append(rids, fmt.Sprintf("req_%03d", i))
	}
	res := buildDay(t, s, strings.TrimSuffix(dir, "/"), ridLines(t, rids), 2<<10)
	if len(res.Parts) < 3 {
		t.Fatalf("expected >=3 parts, got %d", len(res.Parts))
	}
	markSuccess(t, s, strings.TrimSuffix(dir, "/"))

	for _, rid := range []string{"req_000", "req_017", "req_039"} {
		obj, err := s.ReadBody(ctx, dir, rid)
		if err != nil {
			t.Fatalf("ReadBody(%s): %v", rid, err)
		}
		var rb map[string]any
		if err := json.Unmarshal(obj.RequestBody, &rb); err != nil {
			t.Fatalf("request_body 非法 JSON: %v", err)
		}
		if !strings.Contains(fmt.Sprint(rb["messages"]), rid) {
			t.Fatalf("ReadBody(%s) 返回了别的记录: %s", rid, obj.RequestBody)
		}
		var hdr map[string]any
		if err := json.Unmarshal(obj.RequestHeaders, &hdr); err != nil {
			t.Fatalf("request_headers 非法 JSON: %v", err)
		}
		if hdr["x-rid"] != rid {
			t.Fatalf("headers.x-rid = %v want %s", hdr["x-rid"], rid)
		}
	}
}

// TestReadBodyV3_NotFoundAndInvalidInput 覆盖点读的可区分失败：
//   - 缺 _SUCCESS（未完成归档 / 陈旧标记被清理）→ NotFound，绝不返回「数据不存在」以外的猜测；
//   - 缺 index.jsonl → NotFound；
//   - rid 超出末块 / 块内未命中 → NotFound；
//   - 非法 archive_ref / 空 rid → InvalidArgument。
func TestReadBodyV3_NotFoundAndInvalidInput(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	lines := ridLines(t, []string{"req_a", "req_b", "req_c"})
	buildDay(t, s, dir, lines, 0)

	var se *StoreError

	// ① 未写 _SUCCESS → NotFound（该天不可信）。
	_, err := s.ReadBody(ctx, dir+"/", "req_a")
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("missing _SUCCESS err = %v want CodeNotFound", err)
	}
	if !strings.Contains(err.Error(), SuccessMarkerName) {
		t.Fatalf("错误信息应指出缺少 %s：%v", SuccessMarkerName, err)
	}

	// ② 写 _SUCCESS 后正常；缺 index.jsonl → NotFound。
	markSuccess(t, s, dir)
	if _, err := s.ReadBody(ctx, dir+"/", "req_a"); err != nil {
		t.Fatalf("ReadBody after _SUCCESS: %v", err)
	}
	if err := s.Delete(ctx, joinKey(dir, IndexFileName)); err != nil {
		t.Fatalf("delete index: %v", err)
	}
	_, err = s.ReadBody(ctx, dir+"/", "req_a")
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("missing index err = %v want CodeNotFound", err)
	}

	// ③ 超出末块 → NotFound。
	buildDay(t, s, dir, lines, 0)
	_, err = s.ReadBody(ctx, dir+"/", "req_zzz")
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("beyond-last err = %v want CodeNotFound", err)
	}

	// ④ 块内未命中（落在首末块区间内）→ NotFound。
	_, err = s.ReadBody(ctx, dir+"/", "req_aa")
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("miss err = %v want CodeNotFound", err)
	}

	// ⑤ 非法 ref / 空 rid → InvalidArgument。
	if _, err = s.ReadBody(ctx, "dt=not-a-date/", "req_a"); !errors.As(err, &se) || se.Code != CodeInvalidArgument {
		t.Fatalf("bad ref err = %v want CodeInvalidArgument", err)
	}
	if _, err = s.ReadBody(ctx, dir+"/", ""); !errors.As(err, &se) || se.Code != CodeInvalidArgument {
		t.Fatalf("empty rid err = %v want CodeInvalidArgument", err)
	}
}

// TestReadBodyV3_RIDEscapingAndNeedleFalsePositive 覆盖字节预筛的两个陷阱：
//  1. rid 含 `"` / `<` / `>` / `&` / 非 ASCII 时，needle 转义必须与写入侧一致（否则预筛漏命中，
//     只能靠兜底全量扫描，性能退化）；
//  2. 别的记录正文里出现同样的 `"request_id":"<rid>"` 字样时，必须靠完整反序列化确认 rid 相等。
func TestReadBodyV3_RIDEscapingAndNeedleFalsePositive(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"

	tricky := []string{
		`req_"quote"`,
		"req_<x>&y",
		"req_中文请求",
		"req_emoji_🚀",
	}
	// 排序必须是字节序（与导出侧一致）。
	tricky = sqlCollateCOrder(tricky)

	// 构造一行「正文字节里逐字包含目标 rid 的 needle」的干扰记录（例如正文内嵌了一段原始
	// trace 快照）。它的 rid 按字节序排在最前，因此扫描必然先遇到它。
	decoy := map[string]any{
		"request_id":      "req_!decoy",
		"ts":              "2026-09-19T03:00:00+08:00",
		"request_headers": map[string]any{},
		"request_body":    json.RawMessage(`{"snapshot":{"request_id":"` + tricky[len(tricky)-1] + `"}}`),
		"response_body":   map[string]any{},
		"error_code":      "",
		"error_message":   "",
	}
	decoyLine, err := json.Marshal(decoy)
	if err != nil {
		t.Fatalf("marshal decoy: %v", err)
	}
	if !bytes.Contains(decoyLine, requestIDNeedle(tricky[len(tricky)-1])) {
		t.Fatalf("干扰记录未逐字包含 needle，用例无效: %s", decoyLine)
	}

	lines := [][]byte{decoyLine}
	for _, rid := range tricky {
		lines = append(lines, makeTraceLine(t, rid))
	}
	buildDay(t, s, dir, lines, 0)
	markSuccess(t, s, dir)

	for _, rid := range tricky {
		obj, rerr := s.ReadBody(ctx, dir+"/", rid)
		if rerr != nil {
			t.Fatalf("ReadBody(%q): %v", rid, rerr)
		}
		var rb map[string]any
		if err := json.Unmarshal(obj.RequestBody, &rb); err != nil {
			t.Fatalf("request_body: %v", err)
		}
		messages, _ := json.Marshal(rb["messages"])
		// 同样按 encoding/json 的转义规则比较，避免 rid 里的引号/尖括号造成假失败。
		quoted, _ := json.Marshal(rid)
		want := quoted[1 : len(quoted)-1]
		if !bytes.Contains(messages, want) {
			t.Fatalf("ReadBody(%q) 命中了错误记录: %s", rid, obj.RequestBody)
		}
	}
}

// TestReadBodyV3_CorruptPartIsError 断言：坏块必须返回【非 NotFound】的错误。
// 若把损坏当成 NotFound，上层会把它误判为「数据永久丢失」，触发错误的告警/清理决策。
func TestReadBodyV3_CorruptPartIsError(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	res := buildDay(t, s, dir, ridLines(t, []string{"req_a", "req_b"}), 0)
	markSuccess(t, s, dir)

	bad := []byte("not a zstd frame at all")
	if _, err := s.Put(ctx, joinKey(dir, res.Parts[0].Part), bytes.NewReader(bad), int64(len(bad)), PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put corrupt: %v", err)
	}
	_, err := s.ReadBody(ctx, dir+"/", "req_a")
	if err == nil {
		t.Fatal("ReadBody on corrupt part must fail")
	}
	var se *StoreError
	if errors.As(err, &se) && se.Code == CodeNotFound {
		t.Fatalf("坏块不得报告为 NotFound（会误导上层判定数据丢失）: %v", err)
	}
}

// TestReadBodyV3_ReadGateReleasesAndHonorsContext 覆盖契约 §7.3 并发闸落在 LocalStore 层：
//   - 闸被占满时，取消的 ctx 必须立即失败（不排队挂死）；
//   - 点读结束后闸必须释放（否则点读会逐次泄漏直至完全阻塞）。
func TestReadBodyV3_ReadGateReleasesAndHonorsContext(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	buildDay(t, s, dir, ridLines(t, []string{"req_a"}), 0)
	markSuccess(t, s, dir)

	if got := s.MaxConcurrentReads(); got != DefaultMaxConcurrentReads {
		t.Fatalf("默认并发闸 = %d want %d", got, DefaultMaxConcurrentReads)
	}
	s.SetMaxConcurrentReads(1)

	// 占满唯一的许可。
	if err := s.acquireRead(ctx); err != nil {
		t.Fatalf("acquireRead: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ReadBody(cancelled, dir+"/", "req_a"); err == nil {
		t.Fatal("闸满且 ctx 已取消时 ReadBody 必须立即失败")
	}
	s.releaseRead()

	// 正常点读必须能拿到许可（证明上面的失败没有把许可吞掉）。
	obj, err := s.ReadBody(ctx, dir+"/", "req_a")
	if err != nil {
		t.Fatalf("ReadBody after release: %v", err)
	}
	if obj == nil {
		t.Fatal("nil object")
	}
	// 连续点读 N 次不得耗尽闸。
	for i := 0; i < 5; i++ {
		if _, err := s.ReadBody(ctx, dir+"/", "req_a"); err != nil {
			t.Fatalf("ReadBody #%d 泄漏了并发许可: %v", i, err)
		}
	}
}

// TestN10_ReadPathIndependentOfWriteSwitch 是契约 §8 N10 的存储层断言：
// 「write=false, read=true」时点读仍必须可用 —— 点读器只依赖 root 与只读能力，
// 与写侧开关（WriteEnabled）无关。
//
// 装配层（router.go，WS-E）的接线由 tests/archive 的同名用例覆盖。
func TestN10_ReadPathIndependentOfWriteSwitch(t *testing.T) {
	ctx := context.Background()
	writer, root := newTestStore(t)
	dir := "dt=2026-09-19"
	buildDay(t, writer, dir, ridLines(t, []string{"req_after_switch"}), 0)
	markSuccess(t, writer, dir)

	// 模拟只读进程/只读开关下的构造：仅 root + 解码窗口，不经过任何写侧对象。
	reader := NewLocalStore(root, filepath.Join(root, "_tmp"))
	reader.SetZstdWindowBytes(128 << 20)
	reader.SetMaxConcurrentReads(4)

	obj, err := reader.ReadBody(ctx, "dt=2026-09-19/", "req_after_switch")
	if err != nil {
		t.Fatalf("read=true 时点读必须可用，got %v", err)
	}
	if !bytes.Contains(obj.RequestBody, []byte("req_after_switch")) {
		t.Fatalf("unexpected body: %s", obj.RequestBody)
	}
	// 只读实例不得依赖写路径产物：.spool 中不得残留任何未压缩暂存文件。
	if entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir), SpoolDirName)); err == nil && len(entries) > 0 {
		t.Fatalf("点读不应依赖 .spool 暂存文件，残留 %d 个", len(entries))
	}
}
