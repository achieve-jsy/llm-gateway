package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/internal/tracearchive"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// readViperEnabled 返回一份「读侧已启用、其余字段全合法」的输入，供各条**隔离探针**
// 只改动自己关心的那一个字段 —— 否则共享断言环里更早的守卫会遮蔽（本计划全局约束的
// 证据制度第 6 条：整条用例转红不能证明某条守卫承重）。
func readViperEnabled() *viper.Viper {
	v := viper.New()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	v.Set("trace_body.read_max_concurrent", 2)
	v.Set("trace_body.read_zstd_window_mb", 512)
	return v
}

// TestTraceBodyReadConfigDefaults 固定三个默认值。
// Root 留空 ⇒ 读侧禁用是**安全前提**：挂载未到位 / 未配置时不得让任何一行正文
// 走进归档路径；并发 2 与窗口 512 MiB 是规格 §8.4 的 C5 与「与写入侧一致」要求。
func TestTraceBodyReadConfigDefaults(t *testing.T) {
	v := viper.New()
	cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
	require.NoError(t, err, "未配置 root 是合法状态（读侧禁用），不是错误")
	require.Empty(t, cfg.Root)
	require.True(t, cfg.Disabled(), "Root 留空必须等于读侧禁用（默认值，安全）")
	require.Equal(t, 2, cfg.MaxConcurrent, "规格 §8.4 C5 的默认并发是 2")
	require.Equal(t, 512<<20, cfg.ZstdWindowBytes, "默认窗口必须与写入侧一致（512 MiB）")
}

// TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot 固定 root 的形态校验
// （与网关侧同一判据，理由同计划 2）：相对路径的含义取决于进程工作目录，而 /datalog
// 是要被 bind mount 的绝对路径；"/" 会让 spool 建在宿主根文件系统上。
// 末尾的反向断言防止「一律拒绝」的假实现。
func TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot(t *testing.T) {
	for _, bad := range []string{"datalog/trace-archive-v4", "./datalog", "../datalog", "datalog/"} {
		v := readViperEnabled()
		v.Set("trace_body.root", bad)
		_, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.Error(t, err, "相对路径 root=%q 必须被拒绝", bad)
	}
	for _, bad := range []string{"/", "//", "/./", "/tmp/.."} {
		v := readViperEnabled()
		v.Set("trace_body.root", bad)
		_, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.Error(t, err, "root=%q（Clean 后为 /）必须被拒绝", bad)
	}

	v := readViperEnabled()
	cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
	require.NoError(t, err, "合法的绝对路径必须被接受")
	require.Equal(t, "/datalog/trace-archive-v4", cfg.Root)
	require.False(t, cfg.Disabled())
}

// TestTraceBodyReadConfigWhitespaceRootIsDisabled 固定「TrimSpace 后为空 = 禁用」：
// 全空白的 root 不能被当成「启用了一个空白路径」，那会走一条失败方式难以诊断的分支；
// 且返回的 Root 必须是 trim 之后的值。
func TestTraceBodyReadConfigWhitespaceRootIsDisabled(t *testing.T) {
	for _, blank := range []string{" ", "   ", "\t", "\n", " \t\n "} {
		v := viper.New()
		v.Set("trace_body.root", blank)
		cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.NoError(t, err, "root=%q 等同于未设置 ⇒ 禁用，不是错误", blank)
		require.True(t, cfg.Disabled(), "root=%q trim 后为空必须等同于未设置", blank)
		require.Empty(t, cfg.Root, "Root 必须是 TrimSpace 之后的值")
		require.Equal(t, 2, cfg.MaxConcurrent)
		require.Equal(t, 512<<20, cfg.ZstdWindowBytes)
	}
}

// TestTraceBodyReadConfigMaxConcurrentBounds 固定并发上界 1..16：
// 0/-1 会让 sem 的容量为 0（等待槽位的请求永久阻塞），17 会按 600 MB/并发
// 撑爆 control-plane 的 2 GiB 限额（规格 §8.4 C5）。
// **显式配置的 0 必须被拒绝**，只有「键未出现」才回落到默认 2。
func TestTraceBodyReadConfigMaxConcurrentBounds(t *testing.T) {
	for _, bad := range []int{-1, 0, 17, 64} {
		v := readViperEnabled()
		v.Set("trace_body.read_max_concurrent", bad)
		_, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.Error(t, err, "read_max_concurrent=%d 必须被拒绝", bad)
	}
	for _, good := range []int{1, 2, 16} {
		v := readViperEnabled()
		v.Set("trace_body.read_max_concurrent", good)
		cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.NoError(t, err, "read_max_concurrent=%d 必须被接受", good)
		require.Equal(t, good, cfg.MaxConcurrent)
	}
}

// TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide 固定窗口下限：
// 解码窗口小于写入侧窗口时 zstd 解码会失败，那会把「配置错」伪装成「产物损坏」
// ⇒ 必须**在配置期**拒绝，且错误文本要带上期望下限（否则运维只能看到解码错误）。
func TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide(t *testing.T) {
	for _, bad := range []int{1, 256, 511} {
		v := readViperEnabled()
		v.Set("trace_body.read_zstd_window_mb", bad)
		_, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.Error(t, err, "read_zstd_window_mb=%d 必须被拒绝（低于写入侧窗口）", bad)
		require.Contains(t, err.Error(), "必须 >= 512 MiB", "错误必须带上期望下限，否则运维看不出这是配置错")
	}
	for _, good := range []int{512, 1024} {
		v := readViperEnabled()
		v.Set("trace_body.read_zstd_window_mb", good)
		cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
		require.NoError(t, err, "read_zstd_window_mb=%d 必须被接受", good)
		require.Equal(t, good<<20, cfg.ZstdWindowBytes, "字段是字节，配置键是 MiB")
	}
}

// TestTraceBodyReadConfigEnvOverridesReadKeys 固定环境变量名（viper 的 "." → "_"）：
// 读侧只认 root 与两个解码参数，键名与网关侧同名同前缀（两侧读的是同一个
// TRACE_BODY_ROOT）。键名若漂移，部署文档里的变量就会静默失效。
func TestTraceBodyReadConfigEnvOverridesReadKeys(t *testing.T) {
	t.Setenv("TRACE_BODY_ROOT", "/datalog/trace-archive-v4")
	t.Setenv("TRACE_BODY_READ_MAX_CONCURRENT", "4")
	t.Setenv("TRACE_BODY_READ_ZSTD_WINDOW_MB", "768")

	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
	require.NoError(t, err)
	require.Equal(t, "/datalog/trace-archive-v4", cfg.Root)
	require.False(t, cfg.Disabled())
	require.Equal(t, 4, cfg.MaxConcurrent)
	require.Equal(t, 768<<20, cfg.ZstdWindowBytes)
}

// TestValidateRefRejectsIllegalComponents 校验定位键的四个分量（计划 1 的 N3：
// GetBody 自身不校验，冻结包不可改 ⇒ 在调用侧收口）。
// 【每个条目一个独立子例】并点名它对应的变异编号，子例可见性是隔离探针的前提。
func TestValidateRefRejectsIllegalComponents(t *testing.T) {
	cases := []struct {
		name    string
		ref     tracearchive.Ref
		wantErr bool
	}{
		// dt：m05 的杀手子例（严格日期，挡住穿越向量）
		{"dt空串", tracearchive.Ref{DT: "", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt未补零", tracearchive.Ref{DT: "2026-9-24", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt无分隔符", tracearchive.Ref{DT: "20260924", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt月份非法", tracearchive.Ref{DT: "2026-13-01", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt日期非法", tracearchive.Ref{DT: "2026-02-30", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt穿越向量", tracearchive.Ref{DT: "2026-09-24/../..", Inst: "blue", Part: 0, Row: 0}, true},
		{"dt相对穿越", tracearchive.Ref{DT: "../x", Inst: "blue", Part: 0, Row: 0}, true},
		// inst：m06 的杀手子例（白名单，不是「非空即可」）
		{"inst空串", tracearchive.Ref{DT: "2026-09-24", Inst: "", Part: 0, Row: 0}, true},
		{"inst非白名单", tracearchive.Ref{DT: "2026-09-24", Inst: "red", Part: 0, Row: 0}, true},
		{"inst大小写不符", tracearchive.Ref{DT: "2026-09-24", Inst: "Blue", Part: 0, Row: 0}, true},
		{"inst穿越向量", tracearchive.Ref{DT: "2026-09-24", Inst: "../../green", Part: 0, Row: 0}, true},
		{"inst带斜杠", tracearchive.Ref{DT: "2026-09-24", Inst: "blue/", Part: 0, Row: 0}, true},
		// part / row：m07 / m08 的杀手子例
		{"part负数", tracearchive.Ref{DT: "2026-09-24", Inst: "blue", Part: -1, Row: 0}, true},
		{"part超上界", tracearchive.Ref{DT: "2026-09-24", Inst: "blue", Part: 1000000, Row: 0}, true},
		{"row负数", tracearchive.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: -1}, true},
		// 合法条目（反向断言，防止「一律拒绝」的假实现）
		{"合法blue下界", tracearchive.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}, false},
		{"合法green上界", tracearchive.Ref{DT: "2026-12-31", Inst: "green", Part: 999999, Row: 8388607}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cpconfig.ValidateRef(tc.ref)
			if tc.wantErr {
				require.Error(t, err, "ref=%s 必须被拒绝", tc.ref)
				return
			}
			require.NoError(t, err, "ref=%s 必须被接受", tc.ref)
		})
	}
}

// TestValidateRefDoesNoFilesystemWork 是 m05 的**隔离探针**载体：只让 dt 非法
// （inst/part/row 全合法），因此它不会因其它守卫在位而通过。
// 它证明的是「非法 dt 被拒 + 合法 ref 被接受」这一**行为**（可被 m05 杀掉）。
// 【E26 / E46】「校验不触碰文件系统」**不是**本用例能证明的：本条原先的
// `os.Stat(root)` 断言恒真、不可能失败（见文件末尾的详细说明），已按 E26 降级删除，
// 该性质改由 p3-task-1.py 的静态检查行「校验不触碰文件系统」承重。
// 保留的结构性事实（解释「为什么校验必须先于拼路径」）：冻结包的 Paths 不做校验，
// 非法 dt 会让 PartDir 逃出 root（计划 1 的 N3）。
func TestValidateRefDoesNoFilesystemWork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "trace-archive-v4")
	p := tracearchive.NewPaths(root)

	for _, badDT := range []string{"", "../x", "2026-09-24/../..", "2026-9-24"} {
		ref := tracearchive.Ref{DT: badDT, Inst: "blue", Part: 0, Row: 0}
		require.Error(t, cpconfig.ValidateRef(ref), "dt=%q 必须在拼路径之前被拒绝", badDT)
	}
	require.NoError(t, cpconfig.ValidateRef(tracearchive.Ref{
		DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0,
	}), "合法 ref 必须通过（否则上面的断言可由「一律拒绝」的假实现满足）")

	// 为什么校验必须先于拼路径：非法 dt 让产物目录逃出 root（root 只参与拼路径、
	// 这里不创建任何路径，因此本断言不产生任何磁盘副作用）。
	escaped := p.PartDir("2026-09-24/../..", "blue")
	require.False(t, strings.HasPrefix(escaped, root),
		"非法 dt 拼出的目录 %q 逃出 root %q —— 这正是校验必须在校验期发生的原因", escaped, root)

	// 【E26 / E46：本条被降级删除，**不得**把它当成「校验不触碰文件系统」的证据】
	// 这里原本还有一条 `_, statErr := os.Stat(root); require.True(t, os.IsNotExist(statErr))`，
	// 声称「校验不得创建任何文件系统对象」。它**恒真、不可能失败**：`root` 是
	// t.TempDir() 下**从未被创建**的子目录，而 `ValidateRef(ref)` 的签名里**根本没有
	// root** ⇒ 不存在任何一条「放宽 / 删除某个判定」的变异能让它转红（计划全局约束
	// 证据制度第 5/6 条要求的证伪能力不成立）⇒ 它不构成证据。
	// 处置（E26 的裁定，本轮执行）：「ValidateRef 是纯内存判定」这一要求**无法在本层
	// 构造出可失败的断言形态**（函数不接收 root，本层也没有「先校验后拼路径」的调用方
	// —— 那是任务 2 服务层的接线），因此**如实降级**：删掉恒真断言，改由**静态检查**承重
	// （`tools/trace-mutate/sets/p3-task-1.py` 的 CHECKS「校验不触碰文件系统」钉住
	//  `internal/controlplane/config/trace_body_read.go` 不引用 os. / ioutil. / syscall.；
	// 不存在性 / 结构性要求只能用静态检查，见计划全局约束第 7 条）。
	// 上面那条 `escaped` 断言只说明「校验为什么必须发生在拼路径之前」这一**结构性事实**
	//（冻结包 `Paths.PartDir` 的拼路径行为），它同样不能被本集合的变异杀掉，不是行为证据。
}
