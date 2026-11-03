package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errReader 在返回 size 字节后抛错，用于模拟写盘中途失败的场景。
type errReader struct {
	n   int
	buf []byte
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, r.err
	}
	n := len(p)
	if n > r.n {
		n = r.n
	}
	copy(p, r.buf[:n])
	r.n -= n
	return n, nil
}

func newTestStore(t *testing.T) (*LocalStore, string) {
	t.Helper()
	root := t.TempDir()
	return NewLocalStore(root, filepath.Join(root, "_tmp")), root
}

// gzipLines 把若干条 JSON 对象打包为一行一个对象的 gzip 字节流。
func gzipLines(t *testing.T, lines []map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal line: %v", err)
		}
		if _, err := gw.Write(append(b, '\n')); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestPutGetHeadExistsDelete(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	key := "dt=2026-06-04/body-2026-06-04.jsonl.gz"
	content := []byte("hello archive")

	// Exists 初始为 false。
	ok, err := s.Exists(ctx, key)
	if err != nil || ok {
		t.Fatalf("Exists before put = %v,%v want false,nil", ok, err)
	}

	// Put。
	res, err := s.Put(ctx, key, bytes.NewReader(content), int64(len(content)), PutOptions{})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if res.Size != int64(len(content)) {
		t.Fatalf("Put size = %d want %d", res.Size, len(content))
	}
	if res.SHA256 == "" {
		t.Fatalf("Put sha256 empty")
	}

	// Exists 为 true。
	ok, err = s.Exists(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Exists after put = %v,%v want true,nil", ok, err)
	}

	// Head。
	info, err := s.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if info.Size != int64(len(content)) {
		t.Fatalf("Head size = %d want %d", info.Size, len(content))
	}

	// Get 流式读回。
	rc, err := s.Get(ctx, key, GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Get content = %q want %q", got, content)
	}

	// Delete。
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ok, err = s.Exists(ctx, key)
	if err != nil || ok {
		t.Fatalf("Exists after delete = %v,%v want false,nil", ok, err)
	}
}

func TestPutRejectsExistingWithoutOverwrite(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	key := "dt=2026-06-04/body-2026-06-04.jsonl.gz"

	if _, err := s.Put(ctx, key, strings.NewReader("a"), 1, PutOptions{}); err != nil {
		t.Fatalf("first put: %v", err)
	}

	_, err := s.Put(ctx, key, strings.NewReader("b"), 1, PutOptions{})
	var se *StoreError
	if !errors.As(err, &se) || se.Code != CodeAlreadyExists {
		t.Fatalf("second put err = %v want CodeAlreadyExists", err)
	}

	// Overwrite=true 允许覆盖。
	if _, err := s.Put(ctx, key, strings.NewReader("b"), 1, PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("overwrite put: %v", err)
	}
	rc, _ := s.Get(ctx, key, GetOptions{})
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "b" {
		t.Fatalf("overwrite content = %q want b", got)
	}
}

func TestPutAtomicNoPartialOnFailure(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)
	key := "dt=2026-06-04/body-2026-06-04.jsonl.gz"

	fail := &errReader{n: 5, buf: []byte("hello"), err: errors.New("boom")}
	_, err := s.Put(ctx, key, fail, 100, PutOptions{})
	if err == nil {
		t.Fatalf("expected error from failing reader")
	}
	var se *StoreError
	if !errors.As(err, &se) || se.Code != CodeRetryable {
		t.Fatalf("err = %v want CodeRetryable", err)
	}

	// 目标不应存在（原子性：失败不留半成品）。
	if ok, _ := s.Exists(ctx, key); ok {
		t.Fatalf("target should not exist after failed put")
	}
	// 临时 .part 应被清理。
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key)+".part")); !os.IsNotExist(err) {
		t.Fatalf(".part residual should be removed, stat err=%v", err)
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	for _, k := range []string{
		"dt=2026-06-04/body-2026-06-04.jsonl.gz",
		"dt=2026-06-04/_SUCCESS",
		"dt=2026-06-05/body-2026-06-05.jsonl.gz",
	} {
		if _, err := s.Put(ctx, k, strings.NewReader("x"), 1, PutOptions{}); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}

	res, err := s.List(ctx, "dt=2026-06-04/", ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Objects) != 2 {
		t.Fatalf("List len = %d want 2 (%+v)", len(res.Objects), res.Objects)
	}
	if res.Objects[0].Key != "dt=2026-06-04/_SUCCESS" || res.Objects[1].Key != "dt=2026-06-04/body-2026-06-04.jsonl.gz" {
		t.Fatalf("unexpected order: %+v", res.Objects)
	}

	// 分页。
	res, err = s.List(ctx, "", ListOptions{MaxKeys: 2})
	if err != nil {
		t.Fatalf("List paged: %v", err)
	}
	if !res.IsTruncated || len(res.Objects) != 2 || res.NextMarker == "" {
		t.Fatalf("paged result = %+v", res)
	}
	res, err = s.List(ctx, "", ListOptions{MaxKeys: 2, Marker: res.NextMarker})
	if err != nil {
		t.Fatalf("List paged 2: %v", err)
	}
	if res.IsTruncated || len(res.Objects) != 1 {
		t.Fatalf("paged2 result = %+v", res)
	}
}

func TestListSkipsTempAndStaging(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)

	if _, err := s.Put(ctx, "dt=2026-06-04/body-2026-06-04.jsonl.gz", strings.NewReader("x"), 1, PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// 手工放下中间产物与暂存文件。
	_ = os.MkdirAll(filepath.Join(root, "_tmp"), 0o750)
	_ = os.WriteFile(filepath.Join(root, "dt=2026-06-04", "body-2026-06-04.jsonl.gz.part"), []byte("part"), 0o640)
	_ = os.WriteFile(filepath.Join(root, "_tmp", "orphan.part"), []byte("orphan"), 0o640)

	res, err := s.List(ctx, "", ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, o := range res.Objects {
		if strings.Contains(o.Key, "_tmp") || strings.HasSuffix(o.Key, ".part") {
			t.Fatalf("List leaked temp/staging object: %s", o.Key)
		}
	}
	if len(res.Objects) != 1 || res.Objects[0].Key != "dt=2026-06-04/body-2026-06-04.jsonl.gz" {
		t.Fatalf("unexpected objects: %+v", res.Objects)
	}
}

func TestKeyPathEscape(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	for _, k := range []string{"", "/abs/path", "../escape", "a/../../b"} {
		_, err := s.Put(ctx, k, strings.NewReader("x"), 1, PutOptions{})
		var se *StoreError
		if !errors.As(err, &se) || se.Code != CodeInvalidArgument {
			t.Fatalf("Put(%q) err = %v want CodeInvalidArgument", k, err)
		}
	}
}

func TestCleanupStaging(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)

	// 造一个陈旧 .part 与最近 .part。
	_ = os.MkdirAll(filepath.Join(root, "dt=2026-06-04"), 0o750)
	stalePart := filepath.Join(root, "dt=2026-06-04", "body-2026-06-04.jsonl.gz.part")
	freshPart := filepath.Join(root, "dt=2026-06-04", "fresh.part")
	_ = os.WriteFile(stalePart, []byte("old"), 0o640)
	_ = os.WriteFile(freshPart, []byte("new"), 0o640)
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(stalePart, old, old)

	res, err := s.CleanupStaging(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaging: %v", err)
	}
	if res.Removed != 1 {
		t.Fatalf("CleanupStaging removed = %d want 1", res.Removed)
	}
	if _, err := os.Stat(stalePart); !os.IsNotExist(err) {
		t.Fatalf("stale .part should be removed")
	}
	if _, err := os.Stat(freshPart); err != nil {
		t.Fatalf("fresh .part should remain, err=%v", err)
	}
}

func TestReadBody(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	dt := "dt=2026-06-04/"
	key := "dt=2026-06-04/body-2026-06-04.jsonl.gz"

	lines := []map[string]any{
		{
			"request_id":      "req_aaa",
			"ts":              "2026-06-04T23:59:00+08:00",
			"request_headers": map[string]any{"x-test": "1"},
			"request_body":    map[string]any{"prompt": "hi"},
			"response_body":   map[string]any{"content": "yo"},
			"error_code":      "",
			"error_message":   "",
		},
		{
			"request_id":      "req_bbb",
			"ts":              "2026-06-04T23:59:10+08:00",
			"request_headers": map[string]any{"x-test": "2"},
			"request_body":    map[string]any{"prompt": "第二"},
			"response_body":   map[string]any{"content": "第二响应"},
			"error_code":      "",
			"error_message":   "",
		},
	}
	gz := gzipLines(t, lines)
	if _, err := s.Put(ctx, key, bytes.NewReader(gz), int64(len(gz)), PutOptions{}); err != nil {
		t.Fatalf("put body: %v", err)
	}

	obj, err := s.ReadBody(ctx, dt, "req_bbb")
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	var rb map[string]any
	if err := json.Unmarshal(obj.RequestBody, &rb); err != nil {
		t.Fatalf("unmarshal request_body: %v", err)
	}
	if rb["prompt"] != "第二" {
		t.Fatalf("request_body.prompt = %v want 第二", rb["prompt"])
	}
	var resp map[string]any
	if err := json.Unmarshal(obj.ResponseBody, &resp); err != nil {
		t.Fatalf("unmarshal response_body: %v", err)
	}
	if resp["content"] != "第二响应" {
		t.Fatalf("response_body.content = %v", resp["content"])
	}
	var hdr map[string]any
	if err := json.Unmarshal(obj.RequestHeaders, &hdr); err != nil {
		t.Fatalf("unmarshal headers: %v", err)
	}
	if hdr["x-test"] != "2" {
		t.Fatalf("headers.x-test = %v want 2", hdr["x-test"])
	}
}

func TestReadBodyNotFoundAndInvalid(t *testing.T) {
	ctx := context.Background()
	s, root := newTestStore(t)

	// 未归档天 → CodeNotFound（文件不存在）。
	_, err := s.ReadBody(ctx, "dt=2026-06-04/", "req_zzz")
	var se *StoreError
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("missing file err = %v want CodeNotFound", err)
	}

	// 解析非法的 archiveRef → CodeInvalidArgument。
	_, err = s.ReadBody(ctx, "dt=not-a-date/", "req_zzz")
	if !errors.As(err, &se) || se.Code != CodeInvalidArgument {
		t.Fatalf("bad ref err = %v want CodeInvalidArgument", err)
	}

	// 写了文件但 request_id 不命中 → CodeNotFound。
	gz := gzipLines(t, []map[string]any{{"request_id": "req_a", "request_body": map[string]any{"p": 1}}})
	key := "dt=2026-06-04/body-2026-06-04.jsonl.gz"
	_ = os.MkdirAll(filepath.Join(root, "dt=2026-06-04"), 0o750)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(key)), gz, 0o640); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err = s.ReadBody(ctx, "dt=2026-06-04/", "req_b")
	if !errors.As(err, &se) || se.Code != CodeNotFound {
		t.Fatalf("no-match err = %v want CodeNotFound", err)
	}
}
