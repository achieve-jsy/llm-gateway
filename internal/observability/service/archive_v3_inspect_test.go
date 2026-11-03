// archive_v3_inspect_test.go —— WS-F：巡检（v3 §3.5）对归档篡改的检出能力（N3 的巡检侧）。
//
// N3 在验收上要求「归档被删 1 行 → rid 摘要与冻结值不等 → 校验失败」。本文件用生产巡检
// （verifyDayArchive：消费 manifest.files 的 per-part sha256 + ReadBack 重算 rid 摘要）验证该断言，
// 并额外覆盖「per-part sha256 缺失时仍必须拦住『删 1 行』与『行数不变但内容被换』」。
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"gorm.io/datatypes"

	"github.com/example/llm-gateway/internal/archive"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

// ─── 测试用 zstd 编解码（复用生产存储格式；解码窗口与 LocalStore 配置一致）──────

func decodePartForTest(t *testing.T, root, dir string, entry archive.IndexEntry) [][]byte {
	t.Helper()
	store := archive.NewLocalStore(root, "")
	store.SetZstdWindowBytes(8 << 20)
	rc, err := store.Get(context.Background(), dir+"/"+entry.Part, archive.GetOptions{})
	if err != nil {
		t.Fatalf("get part %s: %v", entry.Part, err)
	}
	defer rc.Close()
	d, err := zstd.NewReader(rc)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer d.Close()
	raw, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("zstd read: %v", err)
	}
	var out [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, line)
		}
	}
	return out
}

func encodePartForTest(t *testing.T, lines [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	for _, line := range lines {
		if _, err := w.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			t.Fatalf("zstd write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	return buf.Bytes()
}

// putPartForTest 覆写某块（index.jsonl / manifest.files 保持原样），模拟归档被改动。
func putPartForTest(t *testing.T, root, dir string, entry archive.IndexEntry, content []byte) {
	t.Helper()
	store := archive.NewLocalStore(root, "")
	store.SetZstdWindowBytes(8 << 20)
	if _, err := store.Put(context.Background(), dir+"/"+entry.Part, bytes.NewReader(content),
		int64(len(content)), archive.PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put part %s: %v", entry.Part, err)
	}
}

// ─── 夹具 ────────────────────────────────────────────────────────────────────

// newInspectFixture 造一个「已 verified 的天 + 生产 janitor」（巡检只读盘与 manifest 字段，不需要 DB）。
func newInspectFixture(t *testing.T) (*ArchiveJanitor, *cpmodel.TraceArchiveManifest, string, []string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	rids := writeJanitorTestDay(t, root, archiveTestDay, false)

	cfg := archiveTestConfig(root)
	cfg.VerifySampleRows = 2
	jan := NewTraceArchiveJanitor(nil, nil, nil, cfg)
	if jan.store == nil {
		t.Fatalf("janitor store 未构造（root=%s）", root)
	}

	dir := "dt=" + archiveTestDay.Format("2006-01-02")
	parts, err := archive.LoadIndex(ctx, jan.store, dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("夹具未写出分块")
	}
	files, err := json.Marshal(parts)
	if err != nil {
		t.Fatalf("marshal parts: %v", err)
	}
	sha := frozenSHA(rids...)
	m := &cpmodel.TraceArchiveManifest{
		DT:                     archiveTestDay,
		State:                  cpmodel.ArchiveStateVerified,
		ExpectedBodyCount:      int64(len(rids)),
		ExpectedBodyRIDsSHA256: &sha,
		Files:                  datatypes.JSON(files),
		VerifyState:            cpmodel.ArchiveVerifyOK,
	}
	return jan, m, dir, rids
}

// firstPart 取 manifest.files 的第一块。
func firstPart(t *testing.T, m *cpmodel.TraceArchiveManifest) archive.IndexEntry {
	t.Helper()
	var parts []archive.IndexEntry
	if err := json.Unmarshal(m.Files, &parts); err != nil {
		t.Fatalf("unmarshal manifest files: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("manifest.files 为空")
	}
	return parts[0]
}

// clearPartSHA 返回 per-part sha256 被清空的 manifest 副本（模拟 index sha 缺失/历史数据）。
func clearPartSHA(t *testing.T, m *cpmodel.TraceArchiveManifest) *cpmodel.TraceArchiveManifest {
	t.Helper()
	cp := *m
	var parts []archive.IndexEntry
	if err := json.Unmarshal(m.Files, &parts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i := range parts {
		parts[i].SHA256 = ""
	}
	b, err := json.Marshal(parts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cp.Files = datatypes.JSON(b)
	return &cp
}

// ─── N3 ─────────────────────────────────────────────────────────────────────

// TestN3_InspectDetectsDeletedLine 覆盖 N3：删掉 1 行后巡检必须失败。
//   - 保留 per-part sha256 时：压缩字节哈希不一致即被拦下；
//   - 清空 per-part sha256 时：必须由 ReadBack 的 rid 摘要/行数拦住。
func TestN3_InspectDetectsDeletedLine(t *testing.T) {
	ctx := context.Background()
	jan, m, dir, _ := newInspectFixture(t)

	// 基线与冻结值一致 → 巡检必须通过（防止用例因别的原因失败而"假绿"）。
	if err := jan.verifyDayArchive(ctx, m); err != nil {
		t.Fatalf("健康归档的巡检必须通过: %v", err)
	}

	entry := firstPart(t, m)
	lines := decodePartForTest(t, jan.cfg.root, dir, entry)
	if len(lines) < 3 {
		t.Fatalf("块内行数 = %d，不足以做删行测试", len(lines))
	}
	putPartForTest(t, jan.cfg.root, dir, entry, encodePartForTest(t, lines[1:])) // 删掉第 1 行

	if err := jan.verifyDayArchive(ctx, m); err == nil {
		t.Fatal("删掉 1 行后巡检必须失败（N3 回归）")
	}

	// 清空 per-part sha256：只能靠回读行数/rid 摘要，仍必须失败。
	err := jan.verifyDayArchive(ctx, clearPartSHA(t, m))
	if err == nil {
		t.Fatal("per-part sha256 缺失时，必须由回读行数/rid 摘要拦住删行（N3 回归）")
	}
	if !strings.Contains(err.Error(), "行数") && !strings.Contains(err.Error(), "rid 摘要") {
		t.Fatalf("失败原因应指向回读行数/rid 摘要，got %v", err)
	}
}

// TestN3_InspectDigestCatchesRowSubstitution 覆盖「行数不变但内容被替换」：
// 只用行数校验会漏检，必须由 rid 摘要检出（防自证循环 / BLK-02）。
func TestN3_InspectDigestCatchesRowSubstitution(t *testing.T) {
	ctx := context.Background()
	jan, m, dir, _ := newInspectFixture(t)

	entry := firstPart(t, m)
	lines := decodePartForTest(t, jan.cfg.root, dir, entry)
	if len(lines) < 3 {
		t.Fatalf("块内行数 = %d", len(lines))
	}
	// 删 1 行 + 复制 1 行：总行数不变，但 rid 集合被改。
	swapped := [][]byte{lines[0], lines[0]}
	swapped = append(swapped, lines[2:]...)
	putPartForTest(t, jan.cfg.root, dir, entry, encodePartForTest(t, swapped))

	err := jan.verifyDayArchive(ctx, clearPartSHA(t, m))
	if err == nil {
		t.Fatal("行数不变但 rid 重复时巡检必须失败（只用行数校验会漏检）")
	}
	if !strings.Contains(err.Error(), "rid 摘要") {
		t.Fatalf("该场景必须由 rid 摘要检出，got %v", err)
	}
}

// TestN3_InspectDetectsCorruptedPart 坏块（非 zstd / 截断）必须让巡检失败，修复后重新通过。
func TestN3_InspectDetectsCorruptedPart(t *testing.T) {
	ctx := context.Background()
	jan, m, dir, _ := newInspectFixture(t)
	entry := firstPart(t, m)

	path := filepath.Join(jan.cfg.root, filepath.FromSlash(dir), entry.Part)
	rawOrig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	if err := os.WriteFile(path, []byte("not-a-zstd-frame"), 0o640); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if err := jan.verifyDayArchive(ctx, m); err == nil {
		t.Fatal("坏块必须让巡检失败（不得当成 0 行通过）")
	}

	// 逐字节还原后必须重新通过（证明上面的失败确实来自篡改，而非夹具本身不可信）。
	if err := os.WriteFile(path, rawOrig, 0o640); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := jan.verifyDayArchive(ctx, m); err != nil {
		t.Fatalf("还原后巡检应通过: %v", err)
	}
}

// TestN3_InspectCorruptPartWithMatchingSHAFails 覆盖「per-part sha256 与盘上坏块自洽」的情形：
// 此时只有真正解压回读才能发现块不可解，巡检必须失败（防「只看索引哈希」的假通过）。
func TestN3_InspectCorruptPartWithMatchingSHAFails(t *testing.T) {
	ctx := context.Background()
	jan, m, dir, _ := newInspectFixture(t)

	bad := []byte("truncated-frame-not-decodable")
	entry := firstPart(t, m)
	path := filepath.Join(jan.cfg.root, filepath.FromSlash(dir), entry.Part)
	if err := os.WriteFile(path, bad, 0o640); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	// 让 manifest.files 的 sha256 与盘上坏块一致：索引哈希这一层"自洽"，
	// 唯一能拦住它的就是 ReadBack。
	sum := sha256.Sum256(bad)
	m2 := withPartSHA(t, m, hex.EncodeToString(sum[:]))

	err := jan.verifyDayArchive(ctx, m2)
	if err == nil {
		t.Fatal("索引哈希自洽但块无法解压时，巡检必须失败（ReadBack 不可被跳过）")
	}
	if !strings.Contains(err.Error(), "ReadBack") && !strings.Contains(err.Error(), "失败") {
		t.Fatalf("错误应来自解压回读，got %v", err)
	}
}

// withPartSHA 返回 manifest 副本，其第一块 sha256 被替换为给定值。
func withPartSHA(t *testing.T, m *cpmodel.TraceArchiveManifest, sha string) *cpmodel.TraceArchiveManifest {
	t.Helper()
	cp := *m
	var parts []archive.IndexEntry
	if err := json.Unmarshal(m.Files, &parts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	parts[0].SHA256 = sha
	b, err := json.Marshal(parts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cp.Files = datatypes.JSON(b)
	return &cp
}

// TestInspectMissingSuccessMarkerFails 巡检必须把「缺 _SUCCESS」视为不可信（不得静默通过）。
func TestInspectMissingSuccessMarkerFails(t *testing.T) {
	ctx := context.Background()
	jan, m, dir, _ := newInspectFixture(t)
	if err := jan.store.Delete(ctx, dir+"/"+archive.SuccessMarkerName); err != nil {
		t.Fatalf("delete marker: %v", err)
	}
	err := jan.verifyDayArchive(ctx, m)
	if err == nil {
		t.Fatal("_SUCCESS 缺失时巡检必须失败")
	}
	if !strings.Contains(err.Error(), archive.SuccessMarkerName) {
		t.Fatalf("错误应指出缺少 _SUCCESS，got %v", err)
	}
}

// TestInspectFrozenDigestMismatchFails 冻结摘要被改（PG 侧基准与实际归档不符）时必须失败。
func TestInspectFrozenDigestMismatchFails(t *testing.T) {
	ctx := context.Background()
	jan, m, _, _ := newInspectFixture(t)

	// 基线通过。
	if err := jan.verifyDayArchive(ctx, m); err != nil {
		t.Fatalf("基线巡检应通过: %v", err)
	}

	// 归档不变、冻结基准被改（模拟 PG 与归档不一致）→ 清空 per-part sha 后由 rid 摘要拦下。
	wrong := strings.Repeat("f", 64)
	bad := *m
	bad.ExpectedBodyRIDsSHA256 = &wrong
	err := jan.verifyDayArchive(ctx, clearPartSHA(t, &bad))
	if err == nil {
		t.Fatal("冻结 rid 摘要与归档不一致时巡检必须失败")
	}
	if !strings.Contains(err.Error(), "rid 摘要") {
		t.Fatalf("该场景必须由 rid 摘要检出，got %v", err)
	}
}
