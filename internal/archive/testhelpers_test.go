// testhelpers_test.go —— WS-F 测试公共夹具（仅测试使用）。
//
// 这里实现的关键「参照实现」是 **SQL 侧冻结算术的本地等价物**：
//
//	encode(sha256(convert_to(string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C"), 'UTF8')), 'hex')
//	= sha256( 按字节序排序后的 rid 以 "\n" 串联（无尾随换行） ) 的十六进制
//
// 契约 §5.1 固定了该口径，导出侧与回读侧都必须与之逐字节等价（N3/N8 的判定基准）。
package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// loadTraceSample 读取真实感 JSONL 样本（testdata/trace_sample.jsonl），返回去空行后的各行。
func loadTraceSample(t *testing.T) [][]byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/trace_sample.jsonl")
	if err != nil {
		t.Fatalf("read trace sample: %v", err)
	}
	var out [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, append([]byte(nil), line...))
	}
	if len(out) == 0 {
		t.Fatal("trace sample is empty")
	}
	return out
}

// replicatedSample 把样本复制 reps 份，并改写 request_id / ts 等字段，
// 使每行都不同（避免把「同一行重复」误当成真实数据的压缩率）。
func replicatedSample(t *testing.T, reps int) [][]byte {
	t.Helper()
	base := loadTraceSample(t)
	out := make([][]byte, 0, len(base)*reps)
	for rep := 0; rep < reps; rep++ {
		for i, line := range base {
			var m map[string]any
			if err := json.Unmarshal(line, &m); err != nil {
				t.Fatalf("decode sample line: %v", err)
			}
			m["request_id"] = fmt.Sprintf("req_%03d_%02d_%v", rep, i, m["request_id"])
			m["ts"] = fmt.Sprintf("2026-09-%02dT%02d:%02d:%02d+08:00",
				(rep%28)+1, i%24, (i*7)%60, (rep*13)%60)
			if rb, ok := m["request_body"].(map[string]any); ok {
				rb["temperature"] = float64(rep%10) / 10.0
			}
			if resp, ok := m["response_body"].(map[string]any); ok {
				resp["created"] = 1789000000 + rep*100 + i
				if u, ok := resp["usage"].(map[string]any); ok {
					u["prompt_tokens"] = 3200 + rep*17 + i
					u["completion_tokens"] = 900 + rep*23 + i
				}
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal sample line: %v", err)
			}
			out = append(out, b)
		}
	}
	return out
}

// makeTraceLine 生成一行结构完整的最小 trace JSONL（字段与 archiveRecord 对齐）。
func makeTraceLine(t *testing.T, rid string) []byte {
	t.Helper()
	rec := map[string]any{
		"request_id":      rid,
		"ts":              "2026-09-19T03:00:00+08:00",
		"request_headers": map[string]any{"content-type": "application/json", "x-rid": rid},
		"request_body":    map[string]any{"model": "gpt-4o-mini", "messages": []any{map[string]any{"role": "user", "content": "问题 " + rid}}},
		"response_body":   map[string]any{"id": "chatcmpl-" + rid, "content": "回答 " + rid},
		"error_code":      "",
		"error_message":   "",
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal trace line: %v", err)
	}
	return b
}

// ridLines 按给定 rid 生成 JSONL 行。
func ridLines(t *testing.T, rids []string) [][]byte {
	t.Helper()
	out := make([][]byte, 0, len(rids))
	for _, rid := range rids {
		out = append(out, makeTraceLine(t, rid))
	}
	return out
}

// bytewiseLess 手写字节序比较（不复用 bytes.Compare，作为独立参照实现）。
func bytewiseLess(a, b string) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// sqlCollateCOrder 模拟 PostgreSQL `ORDER BY request_id COLLATE "C"`：纯字节序升序。
func sqlCollateCOrder(rids []string) []string {
	out := append([]string(nil), rids...)
	sort.Slice(out, func(i, j int) bool { return bytewiseLess(out[i], out[j]) })
	return out
}

// localeOrder 模拟「按人类习惯/大小写不敏感」的排序（本地化 collation + 数字自然序）。
// 它只用于证明 N8 的测试数据确实能区分字节序与本地序（否则该用例是无效的）。
func localeOrder(rids []string) []string {
	out := append([]string(nil), rids...)
	key := func(s string) []string {
		s = strings.ToLower(s)
		var parts []string
		cur := strings.Builder{}
		digit := false
		for _, r := range s {
			isDigit := r >= '0' && r <= '9'
			if cur.Len() > 0 && isDigit != digit {
				parts = append(parts, cur.String())
				cur.Reset()
			}
			digit = isDigit
			cur.WriteRune(r)
		}
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
		}
		return parts
	}
	sort.SliceStable(out, func(i, j int) bool {
		ki, kj := key(out[i]), key(out[j])
		if ki[0] != kj[0] {
			return ki[0] < kj[0]
		}
		return ki[0] != "" && strings.Join(ki, "\x00") < strings.Join(kj, "\x00")
	})
	return out
}

// frozenRIDsDigest 计算「SQL 侧冻结期望摘要」的等价物（契约 §5.1 基准 2）。
func frozenRIDsDigest(rids []string) string {
	ordered := sqlCollateCOrder(rids)
	h := sha256.New()
	for i, rid := range ordered {
		if i > 0 {
			_, _ = h.Write([]byte("\n"))
		}
		_, _ = h.Write([]byte(rid))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hookStore 是可注入故障的 ArchiveStore 包装（假 store）：
// putHook/getHook 返回非 nil 时该 key 的操作失败，用于验证失败语义
// （落盘失败 → fail-closed / Abort 清理 / 校验必须中断）。
type hookStore struct {
	ArchiveStore
	putHook func(key string) error
	getHook func(key string) error
}

func (s *hookStore) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error) {
	if s.putHook != nil {
		if err := s.putHook(key); err != nil {
			return PutResult{}, err
		}
	}
	return s.ArchiveStore.Put(ctx, key, r, size, opts)
}

func (s *hookStore) Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, error) {
	if s.getHook != nil {
		if err := s.getHook(key); err != nil {
			return nil, err
		}
	}
	return s.ArchiveStore.Get(ctx, key, opts)
}

// buildDay 用生产分块写入器（PartWriter）在 store 上写出一天的分块 + index + manifest。
// chunkBytes <= 0 时使用默认块大小。
func buildDay(t *testing.T, store ArchiveStore, dir string, lines [][]byte, chunkBytes int64) PartWriterResult {
	t.Helper()
	ctx := context.Background()
	w, err := NewPartWriter(ctx, store, dir, PartWriterOptions{ChunkBytes: chunkBytes})
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
		t.Fatalf("PartWriter.Close: %v", err)
	}
	return res
}

// markSuccess 写 _SUCCESS（点读的前置条件）。
func markSuccess(t *testing.T, store ArchiveStore, dir string) {
	t.Helper()
	key := joinKey(dir, SuccessMarkerName)
	if _, err := store.Put(context.Background(), key, bytes.NewReader(nil), 0, PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("write _SUCCESS: %v", err)
	}
}

// readPartLines 真正解压读盘，返回某块的原始行（用于篡改与断言）。
func readPartLines(t *testing.T, store ArchiveStore, dir string, entry IndexEntry) [][]byte {
	t.Helper()
	ctx := context.Background()
	pr, err := openPartReader(ctx, store, joinKey(dir, entry.Part), zstdWindowOf(store))
	if err != nil {
		t.Fatalf("openPartReader(%s): %v", entry.Part, err)
	}
	defer pr.Close()
	var out [][]byte
	for {
		line, rerr := pr.readLine()
		if len(line) > 0 {
			out = append(out, append([]byte(nil), line...))
		}
		if rerr != nil {
			break
		}
	}
	return out
}

// writePartLines 用生产 codec 参数把行重新压成同名 part（覆盖写）。
// 目的：模拟「归档文件被篡改/损坏，而 index.jsonl 仍保持原样」的场景（N3）。
func writePartLines(t *testing.T, store ArchiveStore, dir string, entry IndexEntry, lines [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	level, err := parseZstdLevel("best")
	if err != nil {
		t.Fatalf("parseZstdLevel: %v", err)
	}
	zw, err := zstd.NewWriter(&buf, buildEncoderOptions(level, DefaultZstdWindowBytes, 1)...)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	for _, line := range lines {
		if _, err := zw.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			t.Fatalf("zstd write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	if _, err := store.Put(context.Background(), joinKey(dir, entry.Part), bytes.NewReader(buf.Bytes()), int64(buf.Len()), PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put tampered part: %v", err)
	}
}
