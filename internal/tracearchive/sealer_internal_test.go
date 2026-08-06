package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

// 【为什么破例放包内（A 层例外，评审第 ⑧ 条补）】parsePartSeq 未导出（它是
// 文件名约定的实现细节，规格不要求它成为生产 API），而封块器必须与
// (*Sealer).ListPendingSeals 共用同一套「恰好 6 位」的块名解析约定 —— 这条交叉
// 验证只能在包内表达：B 层（package tracearchive_test）无法构造这个未导出符号的
// 输入，只能通过文件系统间接验证，那验的是「有没有被扫到」而不是解析函数本身。
// 上层已有的同类校验在 B 层：《TestListPendingSealsIgnoresIllegalNames》
// （tests/tracearchive/sealer_test.go）覆盖了同一个约定在真实目录扫描下的行为，
// 本条只做「.tmp 不被当成合法块名」的定点确认。
func TestParsePartSeqUsedBySealer(t *testing.T) {
	// 封块器同样依赖严格的块名解析，此处交叉验证一次
	if _, ok := parsePartSeq("part-000000.jsonl.zst.tmp"); ok {
		t.Fatal(".tmp 不应被当作合法产物名")
	}
}

// TestSealRefusesSpoolOverTrailerEntryBound 锁定 Seal 的「行数上界」守卫
// （聚焦复审缺陷 A，F3 修）：行数 > maxTrailerEntries 时必须在【删 spool / 写产物
// 之前】拒绝，且诊断身份是 ErrTooManyRows 而不是「行数不符」或「产物损坏」。
//
// 【为什么放 A 层（与 TestSealIsAtomicOnCrash 同一判据）】反向用例要构造「恰好
// 等于上界」的 spool，必须引用未导出常量 maxTrailerEntries —— 在 B 层只能硬编码
// 8388608，与常量的推导（maxTrailerBytes/8）脱钩，上界一旦重标定用例就会静默测错
// 数。同先例：读侧的同一条上界由 A 层的
// reader_internal_test.go:TestReaderRejectsTrailerEntryCountOverBound 覆盖。
//
// 【修前实测（本用例的杀伤力来源）】删掉 sealer.go 的这条守卫后，本用例在
// 「必须返回错误」上转红，且 spool 已被删除（唯一副本丢失）——那正是缺陷 A 的
// 复现结论：Seal 返回 nil、产物落地、随后 GetBody 得 ErrTrailerCorrupt。
func TestSealRefusesSpoolOverTrailerEntryBound(t *testing.T) {
	// 复现输入与缺陷报告一致：每行 `{"a":1}` = 8 字节/行 ⇒ 上界+1 行恰为 64 MiB，
	// 远小于 1.5 GiB（触发条件与块字节大小无关，见 sealer.go 的守卫注释）。
	line := []byte("{\"a\":1}\n")

	newSpool := func(t *testing.T, root string, rows int) string {
		t.Helper()
		spool := NewPaths(root).SpoolFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
		if err := os.MkdirAll(filepath.Dir(spool), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(spool)
		if err != nil {
			t.Fatal(err)
		}
		w := bufio.NewWriterSize(f, 1<<20)
		chunk := bytes.Repeat(line, 1024)
		for written := 0; written < rows; written += 1024 {
			n := rows - written
			if n > 1024 {
				n = 1024
			}
			if _, err := w.Write(chunk[:n*len(line)]); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return spool
	}

	ref := Ref{DT: "2026-09-24", Inst: "blue", Part: 0}

	// ① 超限：必须拒绝、保留 spool、不产生任何产物（含 .tmp 与产物目录）。
	//
	// 两个变体的差别只在编码器：第二个用【不存在的二进制】，用来钉住守卫的位置
	// ——它与行数交叉校验同属「纯判定 + 顺序读」，必须先于任何对编码器的接触
	//（与 sealer.go 里版本守卫的位置说明同理：编码器配错不该掩盖 spool 自身的
	// 问题）。若把守卫挪到版本探测之后，第二个变体会转红。
	for _, tc := range []struct {
		name string
		bin  func(t *testing.T) string
	}{
		{"可用编码器", func(t *testing.T) string { return fakeZstd(t, "exec cat\n") }},
		{"编码器不可用", func(t *testing.T) string { return filepath.Join(t.TempDir(), "无此-zstd") }},
	} {
		t.Run("超限/"+tc.name, func(t *testing.T) {
			root := t.TempDir()
			spool := newSpool(t, root, maxTrailerEntries+1)

			s, err := NewSealer(SealerOptions{Root: root, ZstdBinary: tc.bin(t)})
			if err != nil {
				t.Fatal(err)
			}
			sealErr := s.Seal(context.Background(), SealTask{
				DT: ref.DT, Inst: ref.Inst, Part: ref.Part, SpoolPath: spool,
				Rows: maxTrailerEntries + 1, // 调用方记录与实测【一致】：不得报「行数不符」
			})
			if sealErr == nil {
				t.Fatal("行数超上界必须拒绝封块，实际返回 nil（spool 会被删掉，正文永久不可达）")
			}
			if !errors.Is(sealErr, ErrTooManyRows) {
				t.Fatalf("超限必须返回 ErrTooManyRows，实际 %v", sealErr)
			}
			// 诊断身份是契约：这三类都不是本形态的成因（误报会让运维查错方向）。
			for _, other := range []error{ErrRowCountMismatch, ErrTrailerCorrupt, ErrSealFailed} {
				if errors.Is(sealErr, other) {
					t.Fatalf("超限不得被判成 %v（真实成因为「块内行太密」，实际错误：%v）", other, sealErr)
				}
			}
			// spool 是正文的唯一副本（规格 §7.3）：被拒的调用不得动它。
			if _, err := os.Stat(spool); err != nil {
				t.Fatalf("超限被拒时必须保留 spool（否则正文永久不可达）: %v", err)
			}
			// 「未产生任何产物」必须在 MkdirAll 之前就成立。
			partPath := NewPaths(root).PartFile(ref)
			if _, err := os.Stat(partPath); !os.IsNotExist(err) {
				t.Fatalf("被拒的封块不得产出产物 %s（stat 得 %v）", partPath, err)
			}
			if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
				t.Fatalf("被拒的封块不得留下 .tmp（stat 得 %v）", err)
			}
			if _, err := os.Stat(filepath.Dir(partPath)); !os.IsNotExist(err) {
				t.Fatalf("守卫在 MkdirAll 之前：被拒时不得创建产物目录 %s（stat 得 %v）", filepath.Dir(partPath), err)
			}
		})
	}

	// ② 反向断言：恰好等于上界必须仍能封块（上界是【闭区间】）。
	// 若守卫写成 `>=`（或上界被改小），本用例转红；若守卫被删，本用例仍绿但 ① 红。
	t.Run("恰好等于上界/可封块", func(t *testing.T) {
		root := t.TempDir()
		spool := newSpool(t, root, maxTrailerEntries)

		// 直通假 zstd：本用例断言的是守卫的边界，不是压缩（真编码 64 MiB 会拖慢
		// 整个 task-6 集合的每一次变异运行）。
		s, err := NewSealer(SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Seal(context.Background(), SealTask{
			DT: ref.DT, Inst: ref.Inst, Part: ref.Part, SpoolPath: spool, Rows: maxTrailerEntries,
		}); err != nil {
			t.Fatalf("恰好等于上界必须可封块（守卫是 `>` 而非 `>=`）: %v", err)
		}
		partPath := NewPaths(root).PartFile(ref)
		if _, err := os.Stat(partPath); err != nil {
			t.Fatalf("恰好等于上界时应产出 %s: %v", partPath, err)
		}
		if _, err := os.Stat(spool); !os.IsNotExist(err) {
			t.Fatalf("封块成功后 spool 应被删除（I5）: stat 得 %v", err)
		}

		// 写侧接受的边界必须恰好是读侧接受的边界（两处用同一个常量）：
		// footer 的 row_count == 上界时 decodeTrailer 必须接受，否则这条产物
		// 会"封得出、读不回" —— 那正是缺陷 A 的形态。
		f, err := os.Open(partPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		fb := make([]byte, FooterSize)
		if _, err := f.ReadAt(fb, st.Size()-FooterSize); err != nil {
			t.Fatal(err)
		}
		ft, err := decodeFooter(fb)
		if err != nil {
			t.Fatalf("decodeFooter: %v", err)
		}
		if int(ft.RowCount) != maxTrailerEntries {
			t.Fatalf("footer.row_count 应为上界 %d，实际 %d", maxTrailerEntries, ft.RowCount)
		}
		tb := make([]byte, ft.TrailerLen)
		if _, err := f.ReadAt(tb, ft.TrailerOffset); err != nil {
			t.Fatal(err)
		}
		offsets, err := decodeTrailer(tb, int(ft.RowCount))
		if err != nil {
			t.Fatalf("读侧必须接受恰好等于上界的条目数（写侧上界不得严于读侧）: %v", err)
		}
		if len(offsets) != maxTrailerEntries {
			t.Fatalf("trailer 条目数应为 %d，实际 %d", maxTrailerEntries, len(offsets))
		}
	})
}
