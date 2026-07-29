package tracearchive_test

// 本文件是 B 层测试：封块器的全部断言只用公开 API 表达
//（NewSealer/SealerOptions/Seal/SealTask/ListPendingSeals/Err*，以及规格 §5.2
// 的产物字节格式契约）。唯一必须触达未导出符号的用例是
// TestSealIsAtomicOnCrash（注入 s.failBeforeRename），按全局约束放在
// internal/tracearchive/sealer_internal_test.go 并在那里说明理由。
//
// 辅助函数（writeSpool/requireZstd/fakeZstd）在 helpers_test.go。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

func TestSealProducesArtifactAndRemovesSpool(t *testing.T) {
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("c")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin})
	if err != nil {
		t.Fatalf("建封块器失败: %v", err)
	}
	task := ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0,
		SpoolPath: spool, Rows: len(lines),
	}
	if err := s.Seal(context.Background(), task); err != nil {
		t.Fatalf("封块失败: %v", err)
	}

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
	// I5: 删 spool 必须发生在产物 rename 之后
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatal("封块成功后 spool 应被删除")
	}
	// I2: 不得残留 .tmp
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("不应残留 .tmp 文件")
	}
}

func TestSealRefusesToOverwriteExistingArtifact(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 3, lines)

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 3})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("已有产物，不可覆盖"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 本用例不需要 zstd：产物存在性检查发生在压缩之前
	s, _ := ta.NewSealer(ta.SealerOptions{Root: root})
	err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 3, SpoolPath: spool, Rows: 1,
	})
	if !errors.Is(err, ta.ErrPartExists) {
		t.Fatalf("产物已存在时必须返回 ErrPartExists（宁可留下 spool，不可覆盖唯一副本），实际 %v", err)
	}
	// 原产物必须原封不动
	got, _ := os.ReadFile(partPath)
	if string(got) != "已有产物，不可覆盖" {
		t.Fatal("拒绝封块后原产物不应被改动")
	}
	// spool 必须保留
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("拒绝封块后 spool 应保留以便下轮重试")
	}
	// 不得留下 .tmp
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("拒绝封块不应产生 .tmp")
	}
}

// TestSealRejectsRowCountMismatch 覆盖「spool 行数与调用方记录不符」守卫。
// 该检查在启动子进程之前完成，因此本用例不需要 zstd。
func TestSealRejectsRowCountMismatch(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := ta.NewSealer(ta.SealerOptions{Root: root})
	err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 3, // 实际 2 行
	})
	if !errors.Is(err, ta.ErrRowCountMismatch) {
		t.Fatalf("行数不符应返回 ErrRowCountMismatch，实际 %v", err)
	}
	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("行数不符时不得产生产物")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("行数不符时不得残留 .tmp（检查发生在压缩之前）")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("行数不符时必须保留 spool")
	}
}

// decodeVarints 在测试里**独立实现一遍** trailer 的差分 varint 解码。
//
// 为什么不用实现里的 decodeTrailer：一是它是未导出符号（B 层拿不到），
// 二是独立实现一遍本身就是更强的断言——若实现的 varint 编码写错，
// 这里的解码就对不上，而不是「自己验自己」。
func decodeVarints(b []byte, n int) ([]int64, error) {
	out := make([]int64, 0, n)
	var prev int64
	for i := 0; i < n; i++ {
		var (
			shift uint
			val   uint64
		)
		for {
			if len(b) == 0 {
				return nil, errors.New("varint 流提前结束")
			}
			c := b[0]
			b = b[1:]
			val |= uint64(c&0x7F) << shift
			if c < 0x80 {
				break
			}
			shift += 7
			if shift > 63 {
				return nil, errors.New("varint 过长")
			}
		}
		prev += int64(val)
		out = append(out, prev)
	}
	if len(b) != 0 {
		return nil, errors.New("trailer 存在多余字节")
	}
	return out, nil
}

// TestSealFooterPointsAtValidTrailer 只经**公开格式契约**（规格 §5.2）验证
// 「footer 指向的 trailer 能解出正确的行偏移」，不调用任何未导出符号。
func TestSealFooterPointsAtValidTrailer(t *testing.T) {
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("cc")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin})
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < ta.FooterSize {
		t.Fatalf("产物只有 %d 字节，装不下 %d 字节 footer", len(b), ta.FooterSize)
	}

	// 按规格 §5.2 逐字段解析末尾 32 字节（外部可见的格式契约）
	fb := b[len(b)-ta.FooterSize:]
	if string(fb[0:4]) != "TBA4" {
		t.Fatalf("magic 应为 TBA4，实际 %q", fb[0:4])
	}
	if v := binary.BigEndian.Uint16(fb[4:6]); v != 1 {
		t.Fatalf("format_version 应为 1，实际 %d", v)
	}
	trailerOff := int64(binary.BigEndian.Uint64(fb[6:14]))
	trailerLen := int64(binary.BigEndian.Uint32(fb[14:18]))
	rowCount := int32(binary.BigEndian.Uint32(fb[18:22]))

	// trailer 必须紧邻 footer 之前、且不越界
	if trailerOff < 0 || trailerOff+trailerLen != int64(len(b)-ta.FooterSize) {
		t.Fatalf("trailer 应紧邻 footer 之前: off=%d len=%d footerStart=%d",
			trailerOff, trailerLen, len(b)-ta.FooterSize)
	}
	if int(rowCount) != len(lines) {
		t.Fatalf("footer 的 row_count 应为 %d，实际 %d", len(lines), rowCount)
	}

	tb := append([]byte(nil), b[trailerOff:trailerOff+trailerLen]...)
	offs, err := decodeVarints(tb, len(lines))
	if err != nil {
		t.Fatalf("trailer 无法按公开格式解出 %d 条: %v", len(lines), err)
	}
	// 索引偏移必须精确指向各行首字节（未压缩偏移，与 zstd 产物无关）
	cum := int64(0)
	for i, l := range lines {
		if offs[i] != cum {
			t.Fatalf("第 %d 行偏移应 %d，实际 %d", i, cum, offs[i])
		}
		cum += int64(len(l)) + 1
	}
}

// TestSealFailsOnNonZeroExit 是方案 C 的核心失败语义：
// 子进程非零退出必须被当成封块失败——保留 spool、清理 .tmp、不产生产物。
// 用假 zstd 脚本（exit 3）固定这条断言，不依赖真实 zstd。
func TestSealFailsOnNonZeroExit(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exit 3\n")})
	err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	})
	if !errors.Is(err, ta.ErrSealFailed) {
		t.Fatalf("子进程非零退出应返回 ErrSealFailed，实际 %v", err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("错误链里应能取到退出码 3（这要求用 %%w 包装），实际 %v", err)
	}

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("子进程失败时不得产生产物")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("子进程失败时应清理 .tmp")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("子进程失败时必须保留 spool 以便下轮重试")
	}
}

// TestSealFailsWhenZstdMissing 覆盖「zstd 二进制不存在」（部署事故）：
// 必须报错并保留 spool，而不是静默产出一个空/残缺产物。
//
// 注意这里断言的 exec.ErrNotFound 身份：本机实测（go1.26.1/darwin）
// exec.Command 对【含路径分隔符】的 name 不走 LookPath，cmd.Run 返回的是
// *fs.PathError(ENOENT)，errors.Is(err, exec.ErrNotFound) 为 false。
// 因此实现侧必须把「子进程因二进制缺失而无法启动」归一化到 exec.ErrNotFound，
// 否则调用方（sealer 循环/告警）无法把「部署事故」与「压缩失败」区分开。
func TestSealFailsWhenZstdMissing(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa")})

	missing := filepath.Join(t.TempDir(), "definitely-not-zstd")
	s, _ := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: missing})
	err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 1,
	})
	if !errors.Is(err, ta.ErrSealFailed) {
		t.Fatalf("zstd 缺失应返回 ErrSealFailed，实际 %v", err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("错误链里应保留 exec.ErrNotFound（这要求用 %%w 包装），实际 %v", err)
	}
	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("zstd 缺失时不得产生产物")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("zstd 缺失时必须保留 spool")
	}
}

// TestSealGoldenLayout 把【产物整体布局】逐字节钉死：帧 | trailer | footer。
//
// 为什么必须有它：帧 + trailer + footer 的拼接关系目前只由「解回来一样」保护，
// 一个对称的布局 bug（例如把 trailer 写进帧之前的位置但读端同样处理）会全绿。
// 这里用「直通」假 zstd（exec cat）让帧字节完全可控，因此整个产物可以逐字节比对，
// 且不依赖真实 zstd 的版本。
func TestSealGoldenLayout(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aa"), []byte("bbbb"), []byte("c")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatal(err)
	}

	want := []byte{
		// ① zstd 帧（此处 = spool 原样，共 10 字节）"aa\nbbbb\nc\n"
		0x61, 0x61, 0x0A, 0x62, 0x62, 0x62, 0x62, 0x0A, 0x63, 0x0A,
		// ② trailer：行偏移 0/3/8 → 差分 0/3/5（各 1 字节 varint）
		0x00, 0x03, 0x05,
		// ③ footer：32 字节定长
		0x54, 0x42, 0x41, 0x34, // magic "TBA4"
		0x00, 0x01, // format_version = 1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A, // trailer_offset = 10
		0x00, 0x00, 0x00, 0x03, // trailer_len = 3
		0x00, 0x00, 0x00, 0x03, // row_count = 3
		// frame_sha256("aa\nbbbb\nc\n")[:8]
		0xE3, 0x76, 0xD8, 0xF8, 0xFF, 0x1F, 0x11, 0xE2,
		0x00, 0x00, // reserved
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("产物逐字节不符（长度 %d vs %d）:\n     want % x\n     got  % x", len(want), len(got), want, got)
	}
	// 交叉验证 sha 那一格：它必须等于帧字节的 sha256 前 8 字节。
	//
	// 【落地修正】简报此处写作 want[32:40]。但 footer 从 len(want)-FooterSize=13
	// 起算，帧摘要位于 footer 内 [22:30) 字段，即整个产物的 [35,43)。简报的
	// 32:40 恰好偏移了 len(trailer)=3 字节，读到的是 trailer_len/row_count 与
	// 摘要前 5 字节的混合，该自检恒不成立（与实现无关，纯粹是常量算错）。
	// 这里改按格式契约（规格 §5.2）用 footerStart+22 表达，不再手写魔数。
	frameLen := 10
	footerStart := len(want) - ta.FooterSize
	sum := sha256.Sum256(want[:frameLen])
	if !bytes.Equal(sum[:8], want[footerStart+22:footerStart+30]) {
		t.Fatalf("golden 自身不自洽：帧的 sha256[:8] = % x，footer 内写的是 % x",
			sum[:8], want[footerStart+22:footerStart+30])
	}
}

// TestListPendingSealsSkipsFreshFiles：刚写完的 spool 不能被判定为「已停止追加」。
// 这是「不能封到正在写的 spool」的第一道保证。
func TestListPendingSealsSkipsFreshFiles(t *testing.T) {
	root := t.TempDir()
	writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x")})

	tasks, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("刚写入的 spool 不应进入待封块列表，实际 %d 个: %+v", len(tasks), tasks)
	}
}

// TestListPendingSealsReturnsStaleFiles：mtime 早于 now-minAge 才返回。
func TestListPendingSealsReturnsStaleFiles(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 7, [][]byte{[]byte("x"), []byte("y")})
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(spool, old, old); err != nil {
		t.Fatal(err)
	}

	tasks, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应返回 1 个待封块任务，实际 %d", len(tasks))
	}
	got := tasks[0]
	if got.DT != "2026-09-24" || got.Inst != "blue" || got.Part != 7 || got.SpoolPath != spool {
		t.Fatalf("SealTask 分量不符: %+v", got)
	}
	if got.Rows != 0 {
		t.Fatalf("扫目录时行数未知，Rows 应为 0（由 Seal 扫描后自证），实际 %d", got.Rows)
	}
	if want := int64(len("x\ny\n")); got.UncompressedBytes != want {
		t.Fatalf("UncompressedBytes 应为文件大小 %d，实际 %d", want, got.UncompressedBytes)
	}

	// 幂等且可重启：再扫一次结果相同（纯读，不修改任何状态）
	again, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].SpoolPath != got.SpoolPath {
		t.Fatalf("重复扫描应得到同一结果（幂等），实际 %+v", again)
	}
}

// TestListPendingSealsIgnoresIllegalNames：.tmp、产物名、非 part- 前缀一律不进入列表。
func TestListPendingSealsIgnoresIllegalNames(t *testing.T) {
	root := t.TempDir()
	dir := ta.NewPaths(root).SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{
		"part-000001.jsonl.tmp", // rename 前的半成品
		"part-000002.jsonl.zst", // 产物名混进了 spool 目录
		"index.jsonl",           // 非 part- 前缀
		"part-abc.jsonl",        // 数字段非法
		"part-000003.jsonl.gz",  // 其他压缩扩展名
	} {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("非法文件名不应进入待封块列表，实际 %+v", tasks)
	}
}

// ===========================================================================
// 以下用例为【简报之外的补充】：简报的「变异自检」要求每条守卫都能被用例
// 区分，逐条做删除变异后，下列守卫在简报的用例集下删除后仍全绿（零覆盖），
// 故为每条补一条能区分其存在的用例。用例名与理由一一对应。
// ===========================================================================

// TestSealInvokesZstdCLIWithArgs 用「记录参数」的假 zstd 把【是否真的启动外部
// zstd 进程、以及参数怎么算出来】钉死。它同时覆盖三条守卫：
//
//  1. 编码必须走 os/exec 子进程（方案 C 的承重约束）。若有人退回
//     zstd.NewWriter，本用例根本拿不到参数文件，第一断言即红。这是「不得退回
//     Go 库」这条约束【唯一】可自动化的证据（压缩比类断言本任务不测）。
//  2. 级别默认值必须落实为 CLI 的 -19（删掉 ZstdLevel 默认值会变成 -0）。
//  3. --long 必须由 ZstdWindowBytes 换算，不得硬编码 29：显式传 1<<27 时
//     必须出现 --long=27，硬编码 29 会红。
func TestSealInvokesZstdCLIWithArgs(t *testing.T) {
	cases := []struct {
		name        string
		level       int
		windowBytes int
		wantArgs    []string
	}{
		{name: "默认级别与默认窗口", wantArgs: []string{"-19", "--long=29", "-T1", "-c"}},
		{name: "显式级别与显式窗口", level: 3, windowBytes: 1 << 27, wantArgs: []string{"-3", "--long=27", "-T1", "-c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa")})
			argsFile := filepath.Join(t.TempDir(), "args.txt")
			bin := fakeZstd(t, "printf '%s\\n' \"$@\" > '"+argsFile+"'\nexec cat\n")

			s, err := ta.NewSealer(ta.SealerOptions{
				Root: root, ZstdBinary: bin,
				ZstdLevel: tc.level, ZstdWindowBytes: tc.windowBytes,
			})
			if err != nil {
				t.Fatalf("建封块器失败: %v", err)
			}
			if err := s.Seal(context.Background(), ta.SealTask{
				DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 1,
			}); err != nil {
				t.Fatalf("封块失败: %v", err)
			}

			raw, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("没有记录到 zstd 参数（说明没有真正启动外部 zstd 进程）: %v", err)
			}
			got := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
			if !slices.Equal(got, tc.wantArgs) {
				t.Fatalf("传给 zstd 的参数不符:\n want %q\n got  %q", tc.wantArgs, got)
			}
		})
	}
}

// TestSealDefaultsZstdBinaryFromPath 覆盖「ZstdBinary 留空时回退到 PATH 里的
// zstd」这条默认值守卫。删掉默认值后 exec 的 name 是空串，命令无法启动，
// 本用例转红。用 t.Setenv 把 PATH 指到一个只含假 zstd 的目录，既验证默认名
// 就是 "zstd"，又不依赖真实 zstd 的版本（脚本内用 /bin/cat 绝对路径，
// 因为子进程继承的是被替换过的 PATH）。
func TestSealDefaultsZstdBinaryFromPath(t *testing.T) {
	dir := t.TempDir()
	binDir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\nexec /bin/cat\n"
	if err := os.WriteFile(filepath.Join(binDir, "zstd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa")})

	s, err := ta.NewSealer(ta.SealerOptions{Root: root}) // 刻意不传 ZstdBinary
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 1,
	}); err != nil {
		t.Fatalf("ZstdBinary 留空时必须回退到 PATH 里的 zstd: %v", err)
	}
	if _, err := os.ReadFile(argsFile); err != nil {
		t.Fatalf("默认二进制名不是 \"zstd\"（PATH 里只有 zstd 这个可执行文件）: %v", err)
	}
}

// TestNewSealerValidatesOptions 覆盖 NewSealer 的两条构造期守卫。
// 简报的用例集里没有任何一条构造非法配置，因此删掉任一守卫都仍全绿。
func TestNewSealerValidatesOptions(t *testing.T) {
	t.Run("空 Root 被拒绝", func(t *testing.T) {
		// 空 Root 会让 Paths 指向相对路径（当前工作目录），把正文写到进程 CWD 下。
		if _, err := ta.NewSealer(ta.SealerOptions{}); err == nil {
			t.Fatal("Root 为空必须报错")
		}
	})
	t.Run("非 2 的幂窗口被拒绝", func(t *testing.T) {
		// zstd 的 --long 语义要求窗口是 2 的幂。必须在构造期就报错，
		// 而不是把 bits.Len(1000)-1 的结果当作合法窗口送进 CLI。
		if _, err := ta.NewSealer(ta.SealerOptions{Root: t.TempDir(), ZstdWindowBytes: 1000}); err == nil {
			t.Fatal("非 2 的幂窗口必须报错")
		}
	})
}

// TestSealRejectsUnknownArtifactState 覆盖「产物存在性检查本身失败」这条守卫。
//
// 构造：产物路径上放一个【自引用符号链接】，os.Stat 因 ELOOP 失败 —— 既不是
// 「存在」也不是「不存在」。若实现把这个错误当成「不存在」继续封块，rename
// 会覆盖掉那个符号链接（POSIX rename 不跟随目的端符号链接）并删除 spool，
// 于是本用例观察到「封块成功」；正确行为是报错退出、spool 保留。
// 这是 I4「宁可留下 spool，不可覆盖唯一副本」在「无法判断」情形下的延伸。
func TestSealRejectsUnknownArtifactState(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa")})

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// 自引用符号链接：指向自己的文件名，解析时 ELOOP
	if err := os.Symlink(filepath.Base(partPath), partPath); err != nil {
		t.Skipf("本机不支持符号链接（%v）", err)
	}

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 1,
	})
	if err == nil {
		t.Fatal("产物是否存在无法判断时必须报错并保留 spool，不得当成「不存在」继续封块")
	}
	if errors.Is(err, ta.ErrPartExists) {
		t.Fatalf("这不是「产物已存在」，而是「无法判断」，不应报 ErrPartExists: %v", err)
	}
	if _, statErr := os.Lstat(partPath); statErr != nil {
		t.Fatalf("拒绝封块后原符号链接不应被改动: %v", statErr)
	}
	if _, statErr := os.Stat(spool); statErr != nil {
		t.Fatal("拒绝封块后 spool 应保留")
	}
}

// TestSealFailsWhenSpoolMissing 覆盖 scanSpoolOffsets 的「打开 spool 失败」守卫。
// 真实场景：spool 已在本轮扫描之后被另一个 sealer 实例封掉并删除（幂等重试）。
//
// 断言的哨兵：错误链里必须能取到 os.ErrNotExist。删掉打开失败这条守卫后，
// 空句柄会在读时返回 ErrInvalid，错误链里没有 os.ErrNotExist，本用例转红。
func TestSealFailsWhenSpoolMissing(t *testing.T) {
	root := t.TempDir()
	missing := ta.NewPaths(root).SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: missing, Rows: 1,
	})
	if err == nil {
		t.Fatal("spool 不存在必须报错，不得产出空/残缺产物")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("spool 不存在时错误链里应保留 os.ErrNotExist（调用方可据此跳过已消失的块），实际 %v", err)
	}
	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, statErr := os.Stat(partPath); !os.IsNotExist(statErr) {
		t.Fatal("spool 不存在时不得产生产物")
	}
	if _, statErr := os.Stat(partPath + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatal("spool 不存在时不得残留 .tmp")
	}
}

// TestSealFailsWhenSpoolIsDirectory 覆盖 scanSpoolOffsets 的「读 spool 失败」
// 守卫（非 EOF 的读错误必须上抛，不能被当成文件结束）。
//
// 构造：spool 路径指向一个目录 —— os.Open 成功，但读时 EISDIR。
// 删掉这条守卫后，非 EOF 的读错误会被当成 EOF 反复重试，本用例不是「断言失败」
// 而是【挂死】，由 -timeout 判红（这也是它值得单列一条的原因）。
func TestSealFailsWhenSpoolIsDirectory(t *testing.T) {
	root := t.TempDir()
	spoolDir := ta.NewPaths(root).SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(spoolDir, 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spoolDir, Rows: 1,
	})
	if err == nil {
		t.Fatal("spool 路径是目录（读失败）时必须报错")
	}
	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, statErr := os.Stat(partPath); !os.IsNotExist(statErr) {
		t.Fatal("读 spool 失败时不得产生产物")
	}
}

// TestSealAcceptsRowsZeroFromDiscoveryPath 覆盖「task.Rows == 0 表示行数未知，
// 跳过行数交叉校验」这条守卫 —— 它是 ListPendingSeals 与 Seal 之间唯一的契约
// 连接点。扫目录时行数未知（Rows=0），若把 0 当成「期望 0 行」，目录扫描出来的
// 每一个块都会被 ErrRowCountMismatch 永久拒绝：块永不封、spool 无限增长。
// 这里把两条公开 API 串起来跑一遍（真实 zstd）。
func TestSealAcceptsRowsZeroFromDiscoveryPath(t *testing.T) {
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(spool, old, old); err != nil {
		t.Fatal(err)
	}

	tasks, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应发现 1 个待封块，实际 %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Rows != 0 {
		t.Fatalf("扫目录时行数未知，Rows 应为 0，实际 %d", tasks[0].Rows)
	}

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), tasks[0]); err != nil {
		t.Fatalf("Rows=0（行数未知）时不应被行数交叉校验拒绝: %v", err)
	}
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatal("封块成功后 spool 应被删除")
	}

	b, err := os.ReadFile(ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatal(err)
	}
	fb := b[len(b)-ta.FooterSize:]
	if got := int32(binary.BigEndian.Uint32(fb[18:22])); int(got) != len(lines) {
		t.Fatalf("产物 row_count 应为扫描出的行数 %d，实际 %d", len(lines), got)
	}
}

// TestListPendingSealsHandlesSpoolRootErrors 覆盖两条 spool 根目录守卫：
// 「根目录不存在 = 还没写过正文，正常返回空」与「根目录读失败必须报错」。
func TestListPendingSealsHandlesSpoolRootErrors(t *testing.T) {
	t.Run("spool 根目录不存在返回空而非报错", func(t *testing.T) {
		tasks, err := ta.ListPendingSeals(t.TempDir(), time.Hour)
		if err != nil {
			t.Fatalf("还没写过任何正文时不应报错: %v", err)
		}
		if len(tasks) != 0 {
			t.Fatalf("应为空列表，实际 %+v", tasks)
		}
	})
	t.Run("spool 根目录读失败必须报错", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "spool"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ta.ListPendingSeals(root, time.Hour); err == nil {
			t.Fatal("spool 根目录无法读取时必须报错，静默返回空会让封块永久停摆")
		}
	})
}

// TestListPendingSealsIgnoresNonLayoutEntries 覆盖 ListPendingSeals 的目录形状
// 过滤（简报的 TestListPendingSealsIgnoresIllegalNames 只覆盖了 inst 目录内部的
// 文件名过滤，完全没有覆盖 dt=/inst= 两层目录的形状与前缀过滤）。
//
// 每个非法条目都是为了让某一条过滤器成为唯一拦截点：
//   - spool/notdt/…                  → 只有 "dt=" 前缀过滤能拦（否则 DT="notdt"）
//   - spool/dt=2026-09-25（普通文件） → 只有 dt 的 IsDir 过滤能拦（否则 ReadDir 报错）
//   - spool/dt=…/notinst/…            → 只有 "inst=" 前缀过滤能拦
//   - spool/dt=…/inst=green（普通文件）→ 只有 inst 的 IsDir 过滤能拦
func TestListPendingSealsIgnoresNonLayoutEntries(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)

	writeEntry := func(rel string, isDir bool) {
		t.Helper()
		p := filepath.Join(root, rel)
		if isDir {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	// 全部 stale，因此「没被返回」只可能是因为形状/前缀过滤，而不是 minAge
	writeEntry("spool/notdt/inst=blue/part-000001.jsonl", false)       // dt= 前缀
	writeEntry("spool/dt=2026-09-25", false)                           // dt IsDir
	writeEntry("spool/dt=2026-09-24/notinst/part-000003.jsonl", false) // inst= 前缀
	writeEntry("spool/dt=2026-09-24/inst=green", false)                // inst IsDir
	writeEntry("spool/dt=2026-09-24/inst=blue/part-000004.jsonl", false)

	tasks, err := ta.ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatalf("非布局条目不应让扫描报错（应被静默忽略）: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("只应返回布局内的那一个 spool，实际 %d: %+v", len(tasks), tasks)
	}
	got := tasks[0]
	if got.DT != "2026-09-24" || got.Inst != "blue" || got.Part != 4 {
		t.Fatalf("返回了错误的 spool: %+v", got)
	}
}

// TestSealRejectsTruncatedSpool 覆盖「spool 必须以换行符结尾」守卫。
//
// 为什么必须有它：ListPendingSeals 的发现路径上 Rows==0，行数交叉校验被跳过，
// 残行（写失败留下的部分写入、末尾无换行）没有任何别的拦截点。若放它过去，
// 产物里的行边界会与其后所有行错位，点读将静默返回错误的正文。
// 两条子用例分别覆盖「无交叉校验可用」的发现路径与「纵深防御」的行数已知路径。
// 空 spool 是合法的，见 TestSealAcceptsEmptySpool（两者成对，防止把空文件
// 误判为残行）。
func TestSealRejectsTruncatedSpool(t *testing.T) {
	// "aaa\nbbb"：两行，最后一行没有换行符 —— ENOSPC 下 bufio 直写 fd 的典型残行
	const truncated = "aaa\nbbb"

	cases := []struct {
		name string
		rows int
	}{
		{name: "目录扫描路径（Rows 未知，无交叉校验可用）", rows: 0},
		{name: "行数已知路径（纵深防御，交叉校验本身也拦不住残行）", rows: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			spool := writeSpool(t, root, "2026-09-24", "blue", 0, nil)
			if err := os.WriteFile(spool, []byte(truncated), 0o644); err != nil {
				t.Fatal(err)
			}

			s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
			if err != nil {
				t.Fatal(err)
			}
			err = s.Seal(context.Background(), ta.SealTask{
				DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: tc.rows,
			})
			if !errors.Is(err, ta.ErrSealFailed) {
				t.Fatalf("末尾不是换行符的 spool 必须拒绝封块（ErrSealFailed），实际 %v", err)
			}

			partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
			if _, statErr := os.Stat(partPath); !os.IsNotExist(statErr) {
				t.Fatal("残行被拒绝时不得产生产物（产物行序会错位）")
			}
			if _, statErr := os.Stat(partPath + ".tmp"); !os.IsNotExist(statErr) {
				t.Fatal("残行被拒绝时不得残留 .tmp")
			}
			if _, statErr := os.Stat(spool); statErr != nil {
				t.Fatal("残行被拒绝后 spool 必须保留供人工排查，不得静默删除")
			}
		})
	}
}

// TestSealAcceptsEmptySpool 与 TestSealRejectsTruncatedSpool 成对：
// 0 字节的 spool（row_count=0）是合法状态，不能被「末尾必须是换行符」守卫误判。
//
// 两者的区别在读取语义上：空文件是「EOF 且没有带回任何字节」，残行是
// 「EOF 但还带回了非空字节」。实现必须只拒绝后者。
func TestSealAcceptsEmptySpool(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, nil) // 0 行 → 0 字节
	info, err := os.Stat(spool)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("前置条件不成立：spool 应为 0 字节，实际 %d", info.Size())
	}

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 0,
	}); err != nil {
		t.Fatalf("空 spool 合法（row_count=0），不应被残行守卫拒绝: %v", err)
	}

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	// 假 zstd 直通：0 字节输入 → 0 字节帧 → 0 字节 trailer → 只剩 32 字节 footer
	if len(b) != ta.FooterSize {
		t.Fatalf("空 spool 的产物应只有 %d 字节 footer，实际 %d 字节", ta.FooterSize, len(b))
	}
	fb := b[len(b)-ta.FooterSize:]
	if v := binary.BigEndian.Uint32(fb[14:18]); v != 0 {
		t.Fatalf("空 spool 的 trailer_len 应为 0，实际 %d", v)
	}
	if v := int32(binary.BigEndian.Uint32(fb[18:22])); v != 0 {
		t.Fatalf("空 spool 的 row_count 应为 0，实际 %d", v)
	}
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatal("封块成功后 spool 应被删除")
	}
}

// TestSealerListPendingSealsUsesConfiguredMinAge 覆盖 SealerOptions.MinAge。
//
// 依据（规格 §7.2 / 计划缺陷 D7）：网关自己周期滚动 part 并关闭旧 fd，
// 而 sealer 是独立进程、收不到任何信号；本进程无法区分「已停止追加」与
// 「暂时空闲」，minAge 是唯一的保护，因此它必须是显式配置项且默认不得为 0。
//
// 两条断言：
//  1. 默认 minAge 非 0：1 分钟前写过的 spool 不能被当成「已停止追加」
//     （默认值若为 0，cutoff=now，本用例转红）；
//  2. 显式 minAge 被采纳：minAge=30s 时必须发现该 spool
//     （字段没接到发现逻辑上时转红）。
func TestSealerListPendingSealsUsesConfiguredMinAge(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x")})
	recent := time.Now().Add(-time.Minute)
	if err := os.Chtimes(spool, recent, recent); err != nil {
		t.Fatal(err)
	}

	s, err := ta.NewSealer(ta.SealerOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListPendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("默认 minAge 不得为 0：1 分钟前写过的 spool 不应被判为「已停止追加」，实际 %+v", tasks)
	}

	s2, err := ta.NewSealer(ta.SealerOptions{Root: root, MinAge: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err = s2.ListPendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].SpoolPath != spool {
		t.Fatalf("显式 minAge=30s 应发现 1 分钟前写过的 spool，实际 %+v", tasks)
	}
}
