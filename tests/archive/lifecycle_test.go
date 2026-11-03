// lifecycle_test.go —— WS-F：v3 归档「天」生命周期的跨包验收（黑盒：只用导出 API）。
//
// 覆盖：写（分块 zstd + index + manifest）→ 封印（_SUCCESS）→ 离线回读自校验（基准 1 / 基准 2 口径）
// → 点读回填 → 篡改检出（N3 的存储侧）。不依赖真实 PostgreSQL / Redis。
package archive_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/example/llm-gateway/internal/archive"
)

var lifecycleDay = "dt=2026-09-19"

// frozenRIDsSHA256 用 PostgreSQL `string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C")`
// 的等价口径（字节序升序 + \n 串联）独立计算冻结摘要，作为断言的真值来源。
func frozenRIDsSHA256(rids []string) string {
	ordered := append([]string(nil), rids...)
	sort.Strings(ordered) // Go 字符串比较即字节序
	h := sha256.New()
	for i, rid := range ordered {
		if i > 0 {
			_, _ = h.Write([]byte("\n"))
		}
		_, _ = h.Write([]byte(rid))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// traceLine 构造一行归档 JSONL（字段名与导出 SQL 的列别名约定一致）。
func traceLine(rid string) []byte {
	return []byte(fmt.Sprintf(
		`{"request_id":%q,"ts":"2026-09-19T03:00:00Z","request_headers":{"x-rid":%q},"request_body":{"q":%q},"response_body":{"a":%q},"error_code":"","error_message":""}`,
		rid, rid, rid, rid))
}

// writeDay 写入一个完整的天（分块 + _SUCCESS），返回 rid 列表与 writer 统计。
func writeDay(t *testing.T, store archive.ArchiveStore, dir string, rids []string, chunkBytes int64) archive.PartWriterResult {
	t.Helper()
	ctx := context.Background()
	w, err := archive.NewPartWriter(ctx, store, dir, archive.PartWriterOptions{
		ChunkBytes:      chunkBytes,
		ZstdWindowBytes: 8 << 20,
	})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	for _, rid := range rids {
		if err := w.WriteLine(traceLine(rid)); err != nil {
			t.Fatalf("WriteLine(%s): %v", rid, err)
		}
	}
	res, err := w.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.Put(ctx, dir+"/"+archive.SuccessMarkerName, strings.NewReader(""), 0,
		archive.PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put _SUCCESS: %v", err)
	}
	return res
}

func newStore(t *testing.T, root string) *archive.LocalStore {
	t.Helper()
	st := archive.NewLocalStore(root, "")
	st.SetZstdWindowBytes(8 << 20)
	st.SetMaxConcurrentReads(4)
	return st
}

// TestDayLifecycleWriteSealReadBack 覆盖一整天的写→封印→离线自校验→点读。
func TestDayLifecycleWriteSealReadBack(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := newStore(t, root)

	// 用跨块、含大小写/数字/连字符的 rid，顺带覆盖 COLLATE "C" 与字节序的一致性。
	// 导出 SQL 带 `ORDER BY request_id COLLATE "C"`，故文件顺序即字节序（这里是该口径的 Go 等价物）。
	rids := []string{"req_010", "req_002", "Req_abc", "req-9", "req_000", "req_!bang", "req_中"}
	sort.Strings(rids)
	res := writeDay(t, store, lifecycleDay, rids, 256) // 小块 → 必然产生多个 part

	if res.RowCount != int64(len(rids)) {
		t.Fatalf("writer row_count = %d want %d", res.RowCount, len(rids))
	}
	// 契约 §5.1 基准 2：冻结摘要 = 独立计算的 SQL 等价物。
	if want := frozenRIDsSHA256(rids); res.RIDsSHA256 != want {
		t.Fatalf("writer rids_sha256 = %s want %s", res.RIDsSHA256, want)
	}

	parts, err := archive.LoadIndex(ctx, store, lifecycleDay)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(parts) != len(res.Parts) || len(parts) < 2 {
		t.Fatalf("index 块数 = %d，writer parts = %d（应 ≥2）", len(parts), len(res.Parts))
	}
	// 块链不变式：首尾 rid 衔接且按字节序严格递增。
	for i := 1; i < len(parts); i++ {
		if bytes.Compare([]byte(parts[i-1].LastRID), []byte(parts[i].FirstRID)) >= 0 {
			t.Fatalf("块链未按字节序递增: part[%d].LastRID=%q part[%d].FirstRID=%q",
				i-1, parts[i-1].LastRID, i, parts[i].FirstRID)
		}
	}

	mf, err := archive.LoadManifest(ctx, store, lifecycleDay)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if mf.Parts != len(res.Parts) || mf.RowCount != res.RowCount ||
		mf.UncompressedBytes != res.UncompressedBytes || mf.CompressedBytes != res.CompressedBytes ||
		mf.RIDsSHA256 != res.RIDsSHA256 || len(mf.Files) != len(parts) {
		t.Fatalf("manifest.json 与 index/writer 不一致: %+v", mf)
	}

	// 离线回读自校验：行数与 rid 摘要必须与冻结值逐字节一致。
	rb, err := archive.ReadBack(ctx, store, lifecycleDay, parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RowCount != int64(len(rids)) {
		t.Fatalf("readback row_count = %d want %d", rb.RowCount, len(rids))
	}
	if rb.RIDsSHA256 != frozenRIDsSHA256(rids) {
		t.Fatalf("readback rids_sha256 = %s want %s", rb.RIDsSHA256, frozenRIDsSHA256(rids))
	}

	// 点读：每一行都必须原样回填（含跨块与特殊字符 rid）。
	for _, rid := range rids {
		obj, err := store.ReadBody(ctx, lifecycleDay+"/", rid)
		if err != nil {
			t.Fatalf("ReadBody(%s): %v", rid, err)
		}
		if !bytes.Equal(obj.RequestBody, []byte(fmt.Sprintf(`{"q":%q}`, rid))) {
			t.Fatalf("ReadBody(%s) 正文 = %s", rid, obj.RequestBody)
		}
	}

	// 封印后暂存区不得残留文件（.spool 目录本身允许存在）。
	spool := filepath.Join(root, lifecycleDay, archive.SpoolDirName)
	if entries, err := os.ReadDir(spool); err == nil {
		for _, e := range entries {
			t.Fatalf(".spool 封印后残留文件: %s", e.Name())
		}
	}
	// 天目录不得残留 .part/.tmp 中间产物。
	entries, err := os.ReadDir(filepath.Join(root, lifecycleDay))
	if err != nil {
		t.Fatalf("read day dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("天目录残留中间产物: %s", e.Name())
		}
	}
}

// TestDayLifecycleFailsClosedWithoutSuccessMarker 未封印（缺 _SUCCESS）的天不得被点读，
// 且 ReadBody 必须报「不可点读」而不是返回空正文（防「静默返回空」）。
func TestDayLifecycleFailsClosedWithoutSuccessMarker(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := newStore(t, root)

	// 只写分块，不写 _SUCCESS。
	w, err := archive.NewPartWriter(ctx, store, lifecycleDay, archive.PartWriterOptions{ChunkBytes: 1 << 20})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	if err := w.WriteLine(traceLine("req_a")); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	if _, err := w.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	obj, err := store.ReadBody(ctx, lifecycleDay+"/", "req_a")
	if err == nil {
		t.Fatalf("未封印的天不得点读成功（got %+v）", obj)
	}
	if !strings.Contains(err.Error(), archive.SuccessMarkerName) {
		t.Fatalf("错误应指出缺少 _SUCCESS，got %v", err)
	}
}

// TestDayLifecycleTamperDetected 覆盖 N3 的存储侧：
// 删掉 1 行后，冻结 rid 摘要必然不再匹配（基准 1 的「全量行数」口径无法发现这种变化）。
func TestDayLifecycleTamperDetected(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := newStore(t, root)

	rids := []string{"req_a", "req_b", "req_c", "req_d"}
	writeDay(t, store, lifecycleDay, rids, 1<<20)
	want := frozenRIDsSHA256(rids)

	parts, err := archive.LoadIndex(ctx, store, lifecycleDay)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if rb, err := archive.ReadBack(ctx, store, lifecycleDay, parts); err != nil || rb.RIDsSHA256 != want {
		t.Fatalf("基线回读应匹配冻结值: err=%v digest=%s", err, rb.RIDsSHA256)
	}

	// 用生产 codec 参数重压：删掉 1 行（index.jsonl 不动，模拟「文件被改动」）。
	partPath := filepath.Join(root, lifecycleDay, parts[0].Part)
	raw, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	d, err := zstd.NewReader(bytes.NewReader(raw), zstd.WithDecoderMaxWindow(8<<20))
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(d); err != nil {
		t.Fatalf("zstd read: %v", err)
	}
	d.Close()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("块内行数 = %d", len(lines))
	}
	var out bytes.Buffer
	zw, err := zstd.NewWriter(&out)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	for _, line := range lines[1:] {
		if _, err := zw.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			t.Fatalf("zstd write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	if err := os.WriteFile(partPath, out.Bytes(), 0o640); err != nil {
		t.Fatalf("write tampered part: %v", err)
	}

	rb, err := archive.ReadBack(ctx, store, lifecycleDay, parts)
	if err != nil {
		t.Fatalf("ReadBack(篡改后) 不应直接报错而应由摘要比对发现: %v", err)
	}
	if rb.RowCount == int64(len(rids)) {
		t.Fatalf("删 1 行后行数应变为 %d，got %d", len(rids)-1, rb.RowCount)
	}
	if rb.RIDsSHA256 == want {
		t.Fatal("删 1 行后冻结 rid 摘要必须不再匹配（N3 回归）")
	}
	if rb.RIDsSHA256 != frozenRIDsSHA256(rids[1:]) {
		t.Fatalf("篡改后摘要应等于剩余 rid 的摘要: %s", rb.RIDsSHA256)
	}
}
