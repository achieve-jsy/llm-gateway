package tracearchive_test

import (
	"errors"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

func TestPathsLayout(t *testing.T) {
	p := ta.NewPaths("/datalog/trace-archive-v4")
	ref := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 42, Row: 7}

	wantPart := filepath.Join("/datalog/trace-archive-v4", "dt=2026-09-24", "inst=blue", "part-000042.jsonl.zst")
	if got := p.PartFile(ref); got != wantPart {
		t.Fatalf("PartFile:\n want %s\n got  %s", wantPart, got)
	}
	wantSpool := filepath.Join("/datalog/trace-archive-v4", "spool", "dt=2026-09-24", "inst=blue", "part-000042.jsonl")
	if got := p.SpoolFile(ref); got != wantSpool {
		t.Fatalf("SpoolFile:\n want %s\n got  %s", wantSpool, got)
	}
}

func TestPartNameZeroPadsSixDigits(t *testing.T) {
	p := ta.NewPaths("/x")
	// 99999 是 5 位，补零到 6 位即 099999（不是 009999：那是 9999 的补零结果）。
	// 位数必须恒为 6，否则 parsePartSeq 的长度检查会拒掉自产的文件名。
	for part, want := range map[int]string{
		0:     "part-000000.jsonl.zst",
		1:     "part-000001.jsonl.zst",
		99999: "part-099999.jsonl.zst",
	} {
		got := filepath.Base(p.PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: part}))
		if got != want {
			t.Fatalf("part=%d: want %s, got %s", part, want, got)
		}
	}
}

// TestPartDirAndSpoolDir 单独锁定两个目录方法：PartFile/SpoolFile 的期望值
// 也隐含了目录布局，但若目录方法被改错而文件名方法恰好补偿，上面的用例仍会绿。
// 这里直接断言目录本身，错误信息才能指出究竟哪一层错了。
func TestPartDirAndSpoolDir(t *testing.T) {
	p := ta.NewPaths("/datalog/trace-archive-v4")

	wantPartDir := filepath.Join("/datalog/trace-archive-v4", "dt=2026-09-24", "inst=green")
	if got := p.PartDir("2026-09-24", "green"); got != wantPartDir {
		t.Fatalf("PartDir:\n want %s\n got  %s", wantPartDir, got)
	}
	// spool 必须与产物目录分处两棵子树：未封块的近期正文从这里读，
	// 若两者混同，sealer 扫描产物目录时会把 .jsonl 暂存文件误当产物。
	wantSpoolDir := filepath.Join("/datalog/trace-archive-v4", "spool", "dt=2026-09-24", "inst=green")
	if got := p.SpoolDir("2026-09-24", "green"); got != wantSpoolDir {
		t.Fatalf("SpoolDir:\n want %s\n got  %s", wantSpoolDir, got)
	}
	if p.PartDir("2026-09-24", "green") == p.SpoolDir("2026-09-24", "green") {
		t.Fatalf("产物目录与暂存目录不得相同")
	}
}

// TestRefString 锁定日志/错误信息里的定位键可读形式。
func TestRefString(t *testing.T) {
	ref := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 42, Row: 7}
	want := "2026-09-24/blue/part-000042#row=7"
	if got := ref.String(); got != want {
		t.Fatalf("Ref.String:\n want %s\n got  %s", want, got)
	}
}

// TestSentinelErrorsAreDistinct 锁定 6 个哨兵两两互异。
//
// 判据不是「哨兵非空」，而是「分类不会静默合并」：若 ErrBodyMissing 与
// ErrNotArchived 被复制粘贴成同一个值，读取侧就再也分不清
// 「正文从未落盘（404）」与「元数据与存储不一致（500）」，
// 而 Task 7 的 errors.Is 会把两者一起命中、测试依旧全绿。
// 该断言同时覆盖「哨兵为 nil」——errors.Is(nil, nil) 为 true，会被这里判失败。
func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrPartNotFound", ta.ErrPartNotFound},
		{"ErrBodyMissing", ta.ErrBodyMissing},
		{"ErrNotArchived", ta.ErrNotArchived},
		{"ErrPartExists", ta.ErrPartExists},
		{"ErrRowCountMismatch", ta.ErrRowCountMismatch},
		{"ErrSealFailed", ta.ErrSealFailed},
	}
	for i := range sentinels {
		for j := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(sentinels[i].err, sentinels[j].err) {
				t.Errorf("%s 与 %s 被 errors.Is 判为同一错误，分类会静默合并两类语义不同的失败",
					sentinels[i].name, sentinels[j].name)
			}
		}
	}
}
