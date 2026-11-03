// partwriter_test.go —— WS-F：分块写入 / 索引不变量 / 回读校验（含 N3、N8、N9 负向与正向验收）。
//
// 契约 §3.2 不变量、§4 API、§8 N3/N8/N9。测试只用可注入的假 store（LocalStore + hookStore），
// 不依赖 PostgreSQL / Redis。
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

	"github.com/klauspost/compress/zstd"
)

// TestPartWriterRoundTripIndexAndReadBack 覆盖契约 §3.2 的全部索引不变量：
//   - index.jsonl 与 Close 返回值逐条一致；manifest.json 与 index 一致；
//   - 块内字节序连续升序；index[i].LastRID < index[i+1].FirstRID；
//   - Close 不写 _SUCCESS；.spool 暂存被清理；
//   - ReadBack 真正解压读盘，行数与 rid 摘要与写入侧一致。
func TestPartWriterRoundTripIndexAndReadBack(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)
	dir := "dt=2026-09-19"

	rids := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		rids = append(rids, fmt.Sprintf("req_%04d_%s", i, strings.Repeat("x", i%7)))
	}
	res := buildDay(t, s, dir, ridLines(t, rids), 64<<10)
	if res.RowCount != 300 {
		t.Fatalf("RowCount = %d want 300", res.RowCount)
	}
	if len(res.Parts) < 2 {
		t.Fatalf("expected multiple parts with 64KB chunk, got %d", len(res.Parts))
	}

	// 索引与 manifest 可读且一致。
	parts, err := LoadIndex(ctx, s, dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(parts) != len(res.Parts) {
		t.Fatalf("index parts = %d want %d", len(parts), len(res.Parts))
	}
	for i := range parts {
		if parts[i] != res.Parts[i] {
			t.Fatalf("index[%d] = %+v want %+v", i, parts[i], res.Parts[i])
		}
	}
	mf, err := LoadManifest(ctx, s, dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if mf.RowCount != res.RowCount || len(mf.Files) != len(parts) {
		t.Fatalf("manifest = %+v want row_count=%d parts=%d", mf, res.RowCount, len(parts))
	}
	if mf.RIDsSHA256 != res.RIDsSHA256 {
		t.Fatalf("manifest rids_sha256 = %s want %s", mf.RIDsSHA256, res.RIDsSHA256)
	}

	// 不变量：块内升序 + 块间不重叠。
	var total int64
	prevLast := ""
	for i, p := range parts {
		if p.Rows <= 0 {
			t.Fatalf("part[%d] rows = %d", i, p.Rows)
		}
		if bytewiseLess(p.LastRID, p.FirstRID) {
			t.Fatalf("part[%d] FirstRID %q > LastRID %q", i, p.FirstRID, p.LastRID)
		}
		if i > 0 && !bytewiseLess(prevLast, p.FirstRID) {
			t.Fatalf("index[%d].LastRID %q >= index[%d].FirstRID %q", i-1, prevLast, i, p.FirstRID)
		}
		prevLast = p.LastRID
		total += p.Rows
	}
	if total != res.RowCount {
		t.Fatalf("sum(parts.Rows) = %d want %d", total, res.RowCount)
	}

	// Close 不写 _SUCCESS（契约 §4：由调用方在校验通过后写）。
	if ok, _ := s.Exists(ctx, dir+"/"+SuccessMarkerName); ok {
		t.Fatal("PartWriter.Close must not write _SUCCESS")
	}
	// .spool 暂存清理干净。
	spool := filepath.Join(root, filepath.FromSlash(dir), SpoolDirName)
	if entries, err := os.ReadDir(spool); err == nil && len(entries) > 0 {
		t.Fatalf(".spool not cleaned: %d entries", len(entries))
	}

	// ReadBack 与写入侧一致，且与 SQL 冻结口径一致。
	rb, err := ReadBack(ctx, s, dir, parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RowCount != res.RowCount {
		t.Fatalf("ReadBack rows = %d want %d", rb.RowCount, res.RowCount)
	}
	if rb.RIDsSHA256 != res.RIDsSHA256 {
		t.Fatalf("ReadBack rids = %s want %s", rb.RIDsSHA256, res.RIDsSHA256)
	}
	if want := frozenRIDsDigest(rids); rb.RIDsSHA256 != want {
		t.Fatalf("ReadBack rids = %s want SQL-frozen %s", rb.RIDsSHA256, want)
	}
}

// TestPartWriterNeverSplitsLine 覆盖契约 §3.2「一行绝不跨块：只在一整行写完后判断是否封块；
// 单行 > chunk 时该行独立成块」。
func TestPartWriterNeverSplitsLine(t *testing.T) {
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"

	big := makeTraceLine(t, "req_big")
	big = append(big, bytes.Repeat([]byte(" "), 8<<10)...) // 8KB 单行，远超 1KB 块目标
	smallA := makeTraceLine(t, "req_a")
	smallB := makeTraceLine(t, "req_b")

	// 超长行放在最前面：写完即封块，必须独立成块。
	res := buildDay(t, s, dir, [][]byte{big, smallA, smallB}, 1<<10)
	if res.RowCount != 3 {
		t.Fatalf("RowCount = %d want 3", res.RowCount)
	}
	if res.Parts[0].Rows != 1 || res.Parts[0].FirstRID != "req_big" {
		t.Fatalf("part[0] = %+v want 单行独立成块 req_big", res.Parts[0])
	}

	// 每块解压后每行都必须是【完整合法】的 JSON：被截断的行会在这里暴露。
	seen := map[string]int{}
	for i, p := range res.Parts {
		for j, line := range readPartLines(t, s, dir, p) {
			rid, err := extractRequestID(line)
			if err != nil {
				t.Fatalf("part[%d] line[%d] 不是完整 JSON 行（疑似跨块切断）: %v", i, j, err)
			}
			seen[rid]++
		}
	}
	if seen["req_big"] != 1 || seen["req_a"] != 1 || seen["req_b"] != 1 {
		t.Fatalf("行分布异常: %v（跨块切断会让某行缺失或重复）", seen)
	}
	// 超长行逐字节完整保留。
	got := readPartLines(t, s, dir, res.Parts[0])
	if len(got) != 1 || !bytes.Equal(bytes.TrimRight(got[0], "\r\n"), big) {
		t.Fatalf("big line not preserved intact (lines=%d len=%d)", len(got), len(got[0]))
	}
}

// TestPartWriterAbortCleansSpool 覆盖契约 §4 Abort 语义：删除 .spool 与 .part/.tmp 残留，
// 不写 index.jsonl，且 Abort 幂等。
func TestPartWriterAbortCleansSpool(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)
	dir := "dt=2026-09-19"

	w, err := NewPartWriter(ctx, s, dir, PartWriterOptions{ChunkBytes: 1 << 20})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := w.WriteLine(makeTraceLine(t, fmt.Sprintf("req_abort_%d", i))); err != nil {
			t.Fatalf("WriteLine: %v", err)
		}
	}
	if err := w.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := w.Abort(ctx); err != nil {
		t.Fatalf("Abort should be idempotent: %v", err)
	}

	spool := filepath.Join(root, filepath.FromSlash(dir), SpoolDirName)
	if entries, err := os.ReadDir(spool); err == nil && len(entries) > 0 {
		t.Fatalf(".spool not cleaned by Abort: %d entries", len(entries))
	}
	if ok, _ := s.Exists(ctx, dir+"/"+IndexFileName); ok {
		t.Fatal("Abort must not leave index.jsonl (未校验的天不得有索引)")
	}
	// Abort 后写入器毒性化：拒绝继续写。
	if err := w.WriteLine(makeTraceLine(t, "req_after_abort")); err == nil {
		t.Fatal("WriteLine after Abort must fail")
	}
}

// TestPartWriterFailClosedOnPutError 覆盖契约 §6：分块写盘失败必须中止，
// 后续写入/Close 一律返回错误（不得把残缺块当成完整 part 提交给校验）。
func TestPartWriterFailClosedOnPutError(t *testing.T) {
	ctx := context.Background()
	ls, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	boom := errors.New("disk full")
	fs := &hookStore{ArchiveStore: ls, putHook: func(key string) error {
		if strings.Contains(key, ".jsonl.zst") {
			return boom
		}
		return nil
	}}

	w, err := NewPartWriter(ctx, fs, dir, PartWriterOptions{ChunkBytes: 1 << 20})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	if err := w.WriteLine(makeTraceLine(t, "req_x")); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	// 封块（Put part 失败）→ Close 必须失败。
	if _, err := w.Close(ctx); err == nil {
		t.Fatal("Close must fail when part Put fails")
	}
	if ok, _ := ls.Exists(ctx, dir+"/"+IndexFileName); ok {
		t.Fatal("failed Close must not leave index.jsonl")
	}
	// 失败后写入器毒性化。
	if err := w.WriteLine(makeTraceLine(t, "req_y")); err == nil {
		t.Fatal("WriteLine after failed seal must fail (fail-closed)")
	}
}

// TestPartWriterFailClosedOnSpoolError 覆盖未压缩暂存写失败（Put 失败）时的 fail-closed。
func TestPartWriterFailClosedOnSpoolError(t *testing.T) {
	ctx := context.Background()
	ls, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	fs := &hookStore{ArchiveStore: ls, putHook: func(key string) error {
		if strings.Contains(key, SpoolDirName) {
			return errors.New("spool io error")
		}
		return nil
	}}
	w, err := NewPartWriter(ctx, fs, dir, PartWriterOptions{})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	// spool 是通过 io.Pipe 流式写入的：首次写入可能先返回 nil，由封块暴露错误。
	err = w.WriteLine(makeTraceLine(t, "req_spool"))
	if err == nil {
		_, err = w.Close(ctx)
	}
	if err == nil {
		t.Fatal("expected error when spool Put fails")
	}
}

// TestPartWriterRejectsBadLine 覆盖入参非法：非 JSON 行 / 缺 request_id 必须报错且 fail-closed。
func TestPartWriterRejectsBadLine(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	w, err := NewPartWriter(ctx, s, "dt=2026-09-19", PartWriterOptions{})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	if err := w.WriteLine([]byte("{not json")); err == nil {
		t.Fatal("WriteLine must reject invalid JSON")
	}
	if err := w.WriteLine(makeTraceLine(t, "req_ok")); err == nil {
		t.Fatal("WriteLine must stay failed after a decode error")
	}
}

// TestN3_ReadBackDetectsTamperedPart 是契约 §8 N3 的核心负向测试（防 BLK-02 回归）：
// 归档文件被改动 1 行时，ReadBack 的 rid 摘要必须与【冻结值】不等，四步校验①必须失败。
//
// 三种篡改都覆盖，其中「替换成重复行」专门证明**行数校验不足以发现问题**，
// 摘要校验才是防丢行/防重复的承重墙。
func TestN3_ReadBackDetectsTamperedPart(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"

	rids := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		rids = append(rids, fmt.Sprintf("req_%03d_%s", i, strings.Repeat("y", i%5)))
	}
	res := buildDay(t, s, dir, ridLines(t, rids), 8<<10)
	if len(res.Parts) < 3 {
		t.Fatalf("expected >=3 parts, got %d", len(res.Parts))
	}
	frozen := frozenRIDsDigest(rids)

	// 冻结基准必须与导出侧、回读侧逐字节一致（否则后续断言无意义）。
	if res.RIDsSHA256 != frozen {
		t.Fatalf("writer rids_sha256 = %s want frozen %s（导出顺序必须等于 COLLATE \"C\" 顺序）",
			res.RIDsSHA256, frozen)
	}
	rb, err := ReadBack(ctx, s, dir, res.Parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RIDsSHA256 != frozen {
		t.Fatalf("baseline ReadBack rids = %s want %s", rb.RIDsSHA256, frozen)
	}

	target := res.Parts[1]
	origLines := readPartLines(t, s, dir, target)
	if len(origLines) < 3 {
		t.Fatalf("target part rows = %d, need >=3", len(origLines))
	}

	check := func(name string, wantCountMatch bool) {
		t.Helper()
		got, err := ReadBack(ctx, s, dir, res.Parts)
		if err != nil {
			t.Fatalf("%s: ReadBack: %v", name, err)
		}
		if got.RIDsSHA256 == frozen {
			t.Fatalf("%s: rid 摘要仍等于冻结值，篡改未被检出（N3 回归）", name)
		}
		if countMatches := got.RowCount == int64(len(rids)); countMatches != wantCountMatch {
			t.Fatalf("%s: RowCount=%d (冻结=%d) countMatches=%v want %v",
				name, got.RowCount, len(rids), countMatches, wantCountMatch)
		}
	}

	// 场景 A：删掉 1 行（契约 N3 原文）→ 行数与摘要都变。
	if err := s.Delete(ctx, joinKey(dir, target.Part)); err != nil {
		t.Fatalf("delete part: %v", err)
	}
	writePartLines(t, s, dir, target, origLines[1:])
	check("A: 删除 1 行", false)

	// 场景 B：删 1 行 + 复制 1 行（行数不变）→ 只有摘要能发现（BLK-02 防回归的关键）。
	replaced := make([][]byte, 0, len(origLines))
	replaced = append(replaced, origLines[0])
	replaced = append(replaced, origLines[0]) // 重复第 0 行，丢掉第 1 行
	replaced = append(replaced, origLines[2:]...)
	if err := s.Delete(ctx, joinKey(dir, target.Part)); err != nil {
		t.Fatalf("delete part: %v", err)
	}
	writePartLines(t, s, dir, target, replaced)
	check("B: 行数不变但 rid 重复", true)

	// 场景 C：块内两行交换顺序（行数不变）→ 摘要必须变（防 R11 排序回归）。
	swapped := append([][]byte(nil), origLines...)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	if err := s.Delete(ctx, joinKey(dir, target.Part)); err != nil {
		t.Fatalf("delete part: %v", err)
	}
	writePartLines(t, s, dir, target, swapped)
	check("C: 块内顺序颠倒", true)
}

// TestN8_SQLCollateCEqualsGoByteOrder 是契约 §8 N8：
// SQL `ORDER BY request_id COLLATE "C"` 的顺序必须等于 Go 的字节序顺序（含大小写/数字/连字符/非 ASCII）。
//
// 断言链条：
//  1. LocalStore 侧：PartWriter 写入顺序 → 块内升序 + 块间不重叠（FindPart 二分可用）；
//  2. 摘要侧：冻结摘要（SQL 语义）== 写入侧摘要 == ReadBack 摘要；
//  3. 判别力：本用例的 rid 集合在「本地化/自然序」下顺序不同（否则本用例无效）。
func TestN8_SQLCollateCEqualsGoByteOrder(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"

	rids := []string{
		"A", "AB", "A-b", "a", "a b", "a-b", "a.b", "a_b", "ab", "Apple", "apple",
		"2", "10", "10a", "0009", "9", "zz", "Zz", "ZZ",
		"req-001", "req-01", "req-1", "req--1", "req/1",
		"中文请求", "日本語リクエスト", "Ωmega", "ünïcode", "emoji-🚀-x", "café", "cafe\u0301",
		strings.Repeat("z", 64), strings.Repeat("z", 64) + "0",
	}
	ordered := sqlCollateCOrder(rids)
	if got := localeOrder(rids); fmt.Sprint(got) == fmt.Sprint(ordered) {
		t.Fatalf("测试数据无效：本地化序与字节序相同，无法区分 collation（%v）", ordered)
	}
	// 独立参照：Go 字符串比较即字节序，必须与本文件手写实现一致。
	for i := 1; i < len(ordered); i++ {
		if !bytewiseLess(ordered[i-1], ordered[i]) {
			t.Fatalf("手写字节序参照自相矛盾: %q !< %q", ordered[i-1], ordered[i])
		}
	}

	res := buildDay(t, s, dir, ridLines(t, ordered), 4<<10)
	if len(res.Parts) < 3 {
		t.Fatalf("expected >=3 parts, got %d", len(res.Parts))
	}
	parts := res.Parts

	// 1) 块内升序：逐块解压，断言每行 rid 严格大于上一行（字节序）。
	for i, p := range parts {
		lines := readPartLines(t, s, dir, p)
		if int64(len(lines)) != p.Rows {
			t.Fatalf("part[%d] rows=%d but index says %d", i, len(lines), p.Rows)
		}
		for j, line := range lines {
			rid, err := extractRequestID(line)
			if err != nil {
				t.Fatalf("part[%d] line[%d]: %v", i, j, err)
			}
			if j > 0 {
				prev, _ := extractRequestID(lines[j-1])
				if !bytewiseLess(prev, rid) {
					t.Fatalf("part[%d] 块内非字节序升序: %q 后出现 %q", i, prev, rid)
				}
			}
			if j == 0 && rid != p.FirstRID {
				t.Fatalf("part[%d] FirstRID=%q 但首行=%q", i, p.FirstRID, rid)
			}
			if j == len(lines)-1 && rid != p.LastRID {
				t.Fatalf("part[%d] LastRID=%q 但末行=%q", i, p.LastRID, rid)
			}
		}
	}

	// 2) FindPart 二分定位：每个 rid 必须定位到「包含它的块」，且不能再往前一块。
	for _, rid := range ordered {
		idx := FindPart(parts, rid)
		if idx < 0 {
			t.Fatalf("FindPart(%q) = -1，但该 rid 已归档", rid)
		}
		if idx > 0 && !bytewiseLess(parts[idx-1].LastRID, rid) {
			t.Fatalf("FindPart(%q) = %d 偏后：part[%d].LastRID=%q 已 >= rid",
				rid, idx, idx-1, parts[idx-1].LastRID)
		}
		if bytewiseLess(parts[idx].LastRID, rid) {
			t.Fatalf("FindPart(%q) = %d 偏前：part[%d].LastRID=%q < rid",
				rid, idx, idx, parts[idx].LastRID)
		}
		if len(parts) > idx+1 && !bytewiseLess(rid, parts[idx+1].FirstRID) {
			t.Fatalf("FindPart(%q) = %d 不唯一：part[%d].FirstRID=%q <= rid",
				rid, idx, idx+1, parts[idx+1].FirstRID)
		}
	}
	// 超出末块的 rid → -1（点读据此直接返回 NotFound）。
	if idx := FindPart(parts, ordered[len(ordered)-1]+"\xff"); idx != -1 {
		t.Fatalf("FindPart(beyond last) = %d want -1", idx)
	}
	if idx := FindPart(nil, "req_x"); idx != -1 {
		t.Fatalf("FindPart(empty index) = %d want -1", idx)
	}

	// 3) 摘要一致：SQL 冻结口径 == 写入侧 == 回读侧。
	frozen := frozenRIDsDigest(ordered)
	if res.RIDsSHA256 != frozen {
		t.Fatalf("writer rids_sha256 = %s want frozen %s", res.RIDsSHA256, frozen)
	}
	rb, err := ReadBack(ctx, s, dir, parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RIDsSHA256 != frozen {
		t.Fatalf("ReadBack rids = %s want frozen %s", rb.RIDsSHA256, frozen)
	}
}

// TestN9_CompressionRatioAndCodecWiring 是契约 §8 N9：
// 真实 JSONL 样本经生产 codec 路径的压缩率必须 >= 9x（防漏配 WithWindowSize 造成静默劣化）。
//
// 除阈值断意外还断言 codec 接线真的生效：
//   - 窗口参数必须到达编码器：长程冗余样本上 window=1MB 的压缩率必须显著劣于 128MB（若
//     buildEncoderOptions 丢掉 WithWindowSize，两次运行都会落到默认 8MB，比值退化为 1.0 → 本断言失败）；
//   - 级别参数必须生效：fastest 的压缩率必须低于 best。
func TestN9_CompressionRatioAndCodecWiring(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const dir = "dt=2026-09-19"

	// ① 真实样本（复刻 ~7MB 一天的量，行内容各不相同）。
	sample := replicatedSample(t, 40)
	res := buildDay(t, s, dir, sample, 0)
	ratio := float64(res.UncompressedBytes) / float64(res.CompressedBytes)
	t.Logf("N9 真实样本: lines=%d uncompressed=%d compressed=%d ratio=%.2fx",
		res.RowCount, res.UncompressedBytes, res.CompressedBytes, ratio)
	if ratio < 9.0 {
		t.Fatalf("真实样本压缩率 %.2fx < 9x（N9 回归：WithWindowSize / SpeedBestCompression 可能漏配）", ratio)
	}

	// ② 窗口参数接线：构造「同一块在 2.5MB 之外重复出现」的长程冗余样本（真实日志里同样存在，
	// 例如同一份检索文档/系统提示反复出现）。距离 > 1MB 窗口、< 8MB 默认窗口。
	longRange := longRangeSample(t)
	big, _ := newTestStore(t)
	small, _ := newTestStore(t)
	resBig := buildDayWithOptions(t, big, dir, longRange, PartWriterOptions{
		ChunkBytes: 256 << 20, ZstdWindowBytes: 128 << 20, ZstdLevel: "best", EncoderConcurrency: 1,
	})
	resSmall := buildDayWithOptions(t, small, dir, longRange, PartWriterOptions{
		ChunkBytes: 256 << 20, ZstdWindowBytes: 1 << 20, ZstdLevel: "best", EncoderConcurrency: 1,
	})
	rBig := float64(resBig.UncompressedBytes) / float64(resBig.CompressedBytes)
	rSmall := float64(resSmall.UncompressedBytes) / float64(resSmall.CompressedBytes)
	t.Logf("N9 长程冗余样本: window=128MB ratio=%.2fx; window=1MB ratio=%.2fx", rBig, rSmall)
	if rBig < rSmall*1.5 {
		t.Fatalf("窗口参数未生效：128MB(%.2fx) 未显著优于 1MB(%.2fx)，说明 WithWindowSize 可能丢失",
			rBig, rSmall)
	}

	// ③ 级别接线：fastest 必须劣于 best。
	fast := buildDayWithOptions(t, mustStore(t), dir, longRange, PartWriterOptions{
		ChunkBytes: 256 << 20, ZstdWindowBytes: 128 << 20, ZstdLevel: "fastest",
	})
	rFast := float64(fast.UncompressedBytes) / float64(fast.CompressedBytes)
	t.Logf("N9 级别对比: best=%.2fx fastest=%.2fx", rBig, rFast)
	if rFast >= rBig {
		t.Fatalf("压缩级别未生效：fastest(%.2fx) >= best(%.2fx)", rFast, rBig)
	}

	// ④ 明文样本必须可解压还原（压缩率断言不能以损坏数据为代价）。
	rb, err := ReadBack(ctx, big, dir, resBig.Parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RowCount != resBig.RowCount {
		t.Fatalf("ReadBack rows = %d want %d", rb.RowCount, resBig.RowCount)
	}
}

// longRangeSample 构造长程冗余样本：一份 ~512KB 的【整行】块在 2.5MB 之外重复出现
// （真实日志里同样存在，例如同一份检索文档/系统提示反复出现在多天/多请求中）。
func longRangeSample(t *testing.T) [][]byte {
	t.Helper()
	base := replicatedSample(t, 40)
	pick := func(from, want int) (int, int) {
		size, i := 0, from
		for ; i < len(base) && size < want; i++ {
			size += len(base[i]) + 1
		}
		return from, i
	}
	_, blockEnd := pick(0, 512<<10)
	sepStart, sepEnd := pick(blockEnd, 2500<<10)
	if sepEnd <= sepStart {
		t.Fatalf("sample too small for long-range construction (lines=%d)", len(base))
	}
	var out [][]byte
	for rep := 0; rep < 3; rep++ {
		out = append(out, base[:blockEnd]...)
		out = append(out, base[sepStart:sepEnd]...)
	}
	return out
}

func buildDayWithOptions(t *testing.T, store ArchiveStore, dir string, lines [][]byte, opts PartWriterOptions) PartWriterResult {
	t.Helper()
	ctx := context.Background()
	w, err := NewPartWriter(ctx, store, dir, opts)
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	for i, line := range lines {
		if err := w.WriteLine(line); err != nil {
			t.Fatalf("WriteLine(%d): %v", i, err)
		}
	}
	res, err := w.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	return res
}

func mustStore(t *testing.T) *LocalStore {
	t.Helper()
	s, _ := newTestStore(t)
	return s
}

// TestPartWriterZstdLevelValidation 覆盖 codec 参数校验（非法级别/窗口必须报错，而不是静默回退）。
func TestPartWriterZstdLevelValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	if _, err := NewPartWriter(ctx, s, "dt=2026-09-19", PartWriterOptions{ZstdLevel: "no-such-level"}); err == nil {
		t.Fatal("invalid zstd level must be rejected")
	}
	if _, err := NewPartWriter(ctx, s, "dt=2026-09-19", PartWriterOptions{ZstdWindowBytes: 1000}); err == nil {
		t.Fatal("non power-of-two window must be rejected")
	}
}

// TestReadBackDetectsMissingAndCorruptPart 覆盖回读的失败语义：
// 缺块/坏块必须返回错误（而不是静默按 0 行通过校验）。
func TestReadBackDetectsMissingAndCorruptPart(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	res := buildDay(t, s, dir, ridLines(t, []string{"req_a", "req_b", "req_c"}), 0)

	// 缺块 → 错误（不能当成 0 行）。
	if err := s.Delete(ctx, joinKey(dir, res.Parts[0].Part)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ReadBack(ctx, s, dir, res.Parts); err == nil {
		t.Fatal("ReadBack must fail when a part is missing")
	}

	// 坏块（非 zstd 字节）→ 错误。
	res2 := buildDay(t, s, "dt=2026-09-20", ridLines(t, []string{"req_d", "req_e"}), 0)
	bad := []byte("this is not a zstd frame")
	if _, err := s.Put(ctx, joinKey("dt=2026-09-20", res2.Parts[0].Part), bytes.NewReader(bad), int64(len(bad)), PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put corrupt part: %v", err)
	}
	if _, err := ReadBack(ctx, s, "dt=2026-09-20", res2.Parts); err == nil {
		t.Fatal("ReadBack must fail on a corrupt part")
	}
}

// TestIndexJSONFieldNames 冻结 index.jsonl 的 JSON 字段名（契约 §3.2：Go 与 JSON 字段名必须完全一致）。
func TestIndexJSONFieldNames(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dir := "dt=2026-09-19"
	res := buildDay(t, s, dir, ridLines(t, []string{"req_a"}), 0)

	rc, err := s.Get(ctx, joinKey(dir, IndexFileName), GetOptions{})
	if err != nil {
		t.Fatalf("get index: %v", err)
	}
	defer rc.Close()
	var raw map[string]json.RawMessage
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatalf("read index: %v", err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &raw); err != nil {
		t.Fatalf("index is not a JSON object: %v", err)
	}
	want := []string{"part", "first_rid", "last_rid", "rows", "uncompressed_bytes", "compressed_bytes", "sha256"}
	for _, k := range want {
		if _, ok := raw[k]; !ok {
			t.Fatalf("index.jsonl 缺少字段 %q（实际 %v）", k, keysOf(raw))
		}
	}
	if len(raw) != len(want) {
		t.Fatalf("index.jsonl 字段数 = %d want %d (%v)", len(raw), len(want), keysOf(raw))
	}
	// 索引必须无多余空格/换行歧义：每块一行。
	if bytes.Count(buf.Bytes(), []byte("\n")) != len(res.Parts) {
		t.Fatalf("index.jsonl 行数 = %d want %d", bytes.Count(buf.Bytes(), []byte("\n")), len(res.Parts))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestZstdDecoderWindowMatchesWriter 覆盖契约 §3.3：写入侧窗口 > 解码侧窗口上限时，
// 解码必须失败（配置必须两侧一致），且默认值必须是 128 MiB。
func TestZstdDecoderWindowMatchesWriter(t *testing.T) {
	const window = 64 << 20 // 64MB 帧窗口（> 默认解码上限？默认即 128MB，故需用更小上限验证）
	if DefaultZstdWindowBytes != 128<<20 {
		t.Fatalf("DefaultZstdWindowBytes = %d want 128MiB", DefaultZstdWindowBytes)
	}
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, buildEncoderOptions(zstd.SpeedBestCompression, window, 1)...)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := zw.Write(bytes.Repeat([]byte("abcdefgh"), 1<<16)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 解码上限小于帧窗口 → 必须报错。
	small, err := zstd.NewReader(bytes.NewReader(buf.Bytes()), buildDecoderOptions(1<<20)...)
	if err == nil {
		_, derr := small.DecodeAll(buf.Bytes(), nil)
		small.Close()
		if derr == nil {
			t.Fatal("decoder with smaller max window must reject a larger frame")
		}
	}
	// 足够大的上限 → 成功。
	big, err := zstd.NewReader(bytes.NewReader(buf.Bytes()), buildDecoderOptions(window)...)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer big.Close()
	out, err := big.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatalf("decode with matching window: %v", err)
	}
	if len(out) != 8*(1<<16) {
		t.Fatalf("decoded len = %d", len(out))
	}
}
