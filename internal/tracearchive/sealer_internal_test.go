package tracearchive

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 【为什么破例放包内（A 层例外）】本用例靠注入 s.failBeforeRename 制造
// 「rename 之前一步失败」，从而在**不真的崩溃**的前提下验证 I2（产物只经
// rename 出现）。该钩子刻意不导出（规格不要求它成为生产 API），因此 B 层的
// 外部测试包（package tracearchive_test）无法表达这条断言 —— 这是全局约束下
// 必须说明的「为什么必须触达未导出符号」。
//
// 优先用 B 层表达的检查已经放在 tests/tracearchive/sealer_test.go 里；
// 本文件只剩这一条注入用例与一条 parsePartSeq 的交叉验证。
func TestSealIsAtomicOnCrash(t *testing.T) {
	// 模拟压缩中途崩溃：产物目录不得出现半成品
	bin, ver := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	// 注入错误：让 rename 之前的一步失败
	s.failBeforeRename = func() error { return os.ErrPermission }
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 2,
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("注入的失败应被原样传播（errors.Is 必须可识别），实际 %v", err)
	}
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("失败时不得出现产物文件")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("失败时应清理 .tmp")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("失败时 spool 必须保留以便重试")
	}
}

func TestParsePartSeqUsedBySealer(t *testing.T) {
	// 封块器同样依赖严格的块名解析，此处交叉验证一次
	if _, ok := parsePartSeq("part-000000.jsonl.zst.tmp"); ok {
		t.Fatal(".tmp 不应被当作合法产物名")
	}
}
