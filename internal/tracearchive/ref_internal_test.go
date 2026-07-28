package tracearchive

import "testing"

// 【为什么破例放包内】parsePartSeq 是未导出的实现细节（写指针靠它从文件名反解块序号），
// 外部测试包无法访问；而它的严格性直接决定「写指针是否会撞上已存在的块」。
func TestParsePartSeq(t *testing.T) {
	// 写指针依赖从文件名反解块序号，因此解析必须严格
	cases := map[string]int{
		"part-000000.jsonl.zst": 0,
		"part-000042.jsonl.zst": 42,
		"part-000007.jsonl":     7,
	}
	for name, want := range cases {
		got, ok := parsePartSeq(name)
		if !ok || got != want {
			t.Fatalf("%s: want (%d,true), got (%d,%v)", name, want, got, ok)
		}
	}
}

// TestParsePartSeqRejectsIllegalNames 逐个分支覆盖 parsePartSeq 的拒绝逻辑。
//
// 【为什么破例放包内】与上面的正例同因：被断言的是未导出的 parsePartSeq，
// 在 B 层（tests/tracearchive，package tracearchive_test）不可见，
// 无法从外部测试包触达这条严格解析路径。
//
// 判据：删掉任一分支后，必须至少有一个子用例转红。
// 每个 bad 名字都标注了它守住的是哪个分支，避免「凑一个非法名」式的假覆盖。
func TestParsePartSeqRejectsIllegalNames(t *testing.T) {
	cases := []struct {
		name string
		why  string
	}{
		{"index.jsonl", "非 part- 前缀（长度检查也会挡下，是行为覆盖而非分支覆盖）"},
		{"000042.jsonl", "缺少 part- 前缀但数字段合法 —— 去掉前缀检查后它会被解析成 42，只有前缀检查能挡住它"},
		{"part-.jsonl.zst", "数字段为空"},
		{"part-abc.jsonl.zst", "数字段非数字（长度检查就挡下了）"},
		{"part-00004a.jsonl", "数字段 6 位但含非数字 —— 长度检查会放行，只有逐位数字校验能挡住它"},
		{"part-+00042.jsonl", "前导加号 —— strconv.Atoi 会接受（+00042 → 42）并与 part-000042 撞序号，只有逐位数字校验能挡住"},
		{"part-000042", "无扩展名 —— 只有扩展名的 default 分支能挡住它"},
		{"part-1234567.jsonl", "数字段 7 位 —— 只有长度检查能挡住它"},
		{"part-1234.jsonl", "数字段 4 位 —— 只有长度检查能挡住它"},
		{"part-000042.jsonl.zst.tmp", ".tmp 后缀（rename 前的半成品，绝不能当产物）"},
		{"part-000042.ZST", "大小写不符的扩展名"},
		{"part--00042.jsonl", "负号混在数字段里 —— 长度恰好 6 位，只有逐位数字校验能挡住它"},
		{"part-000042.jsonl.gz", "其他压缩扩展名"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if seq, ok := parsePartSeq(c.name); ok {
				t.Fatalf("%s 不应被解析为合法块名（%s），实际得到 seq=%d", c.name, c.why, seq)
			}
		})
	}
}
