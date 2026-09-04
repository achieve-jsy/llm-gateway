package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/tracearchive"
	"github.com/spf13/viper"
)

// 读侧（control-plane）点读配置的常量。
//
// 【本文件只覆盖读侧需要的东西】root（定位 spool 与产物目录）+ 解码并发 + 解码窗口。
// 写入侧参数（块大小、flush / 滚动间隔、磁盘水位、实例标识）对读侧**没有消费者**：
// 它们由网关侧配置负责（计划 2），读侧再读一遍只会漂移出「同名两个数」。
// 这条由变异集合 p3-task-1.py 的静态检查行（CHECKS#1）钉死。
const (
	// traceBodyReadDefaultMaxConcurrent 是解码并发上限的默认值（规格 §8.4 C5）：
	// 按 600 MB/并发估算，2 个并发槽位落在 control-plane 的 2 GiB 限额内。
	traceBodyReadDefaultMaxConcurrent = 2
	// traceBodyReadMaxConcurrent 是并发上限的硬上界：再大就会撑爆 2 GiB 限额。
	traceBodyReadMaxConcurrent = 16
	// traceBodyReadDefaultWindowMB 是解码窗口的默认值（MiB），必须与写入侧窗口一致。
	traceBodyReadDefaultWindowMB = 512
	// traceBodyWriteSideWindowMB 是写入侧窗口（MiB）的镜像：读侧窗口只允许更大，
	// 更小会让 zstd 解码失败 —— 那会把「配置错」伪装成「产物损坏」。
	traceBodyWriteSideWindowMB = 512
	// tracePartSeqMax 是 part 序号的上界：冻结包 partName 用 %06d，前置约束是 [0,999999]；
	// 越界会产出 7 位文件名，而 parsePartSeq 只认恰好 6 位 ⇒ 错误必须在**拼路径之前**发生。
	tracePartSeqMax = 999999
	// traceBodyReadDateLayout 是 dt 的唯一合法形式（与冻结包 writer.go 的 dateLayout
	// 同一字面量）：严格日期能同时挡住空串、未补零、非法日期与含 "/" 的穿越向量。
	traceBodyReadDateLayout = "2006-01-02"
	// instBlue / instGreen 是实例白名单（与冻结包同一枚举）：蓝绿两实例的目录必须隔离。
	instBlue  = "blue"
	instGreen = "green"
)

// TraceBodyReadConfig 是读侧（control-plane）的点读配置。
//
// 【不包含】任何写入侧参数（块大小、flush / 滚动间隔、磁盘水位、实例标识）——
// 它们对读侧没有消费者，由变异集合的静态检查钉死。
type TraceBodyReadConfig struct {
	// Root 为空 ⇒ 读侧禁用（见本计划全局约束「功能关闭的语义（读侧）」）。
	// 非空时必须 IsAbs 且 Clean != "/"（与网关侧同一判据，理由同计划 2）。
	Root string
	// MaxConcurrent 解码并发上限（规格 §8.4 C5）；默认 2。范围 1..16。
	MaxConcurrent int
	// ZstdWindowBytes 解码窗口，必须 >= 写入侧窗口（默认 512 MiB），
	// 否则解码失败。默认 512 << 20。
	ZstdWindowBytes int
}

// Disabled 报告读侧是否禁用（Root 为空）。调用方据此选择 200/503：
// 禁用**不等于**「正文不存在」，只有「有定位键 + 禁用」才是部署错误（503）。
func (c TraceBodyReadConfig) Disabled() bool { return c.Root == "" }

// LoadTraceBodyReadConfig 从 viper 装配并校验读侧配置。
//
// 校验次序（每一步都要有对应用例）：root trim / 空 ⇒ 禁用 → root 绝对路径 →
// root 不得是文件系统根 → 并发 1..16 → 窗口 >= 写入侧。
//
// 【为什么空 root 直接返回而不报错】「读侧未配置」是合法状态（功能关闭时正文
// 仍走 jsonb 列），把它当成错误会让每一次未启用该功能的启动都失败。
// 【为什么禁用分支也要给出默认并发/窗口】调用方（任务 2 的服务）会无条件读这两个
// 字段；留 0 会让「功能关闭」与「配置为 0」两种状态无法区分。
func LoadTraceBodyReadConfig(v *viper.Viper) (TraceBodyReadConfig, error) {
	// 禁用态的返回值：Root 为空，解码参数一律取默认值。
	disabled := TraceBodyReadConfig{
		Root:            "",
		MaxConcurrent:   traceBodyReadDefaultMaxConcurrent,
		ZstdWindowBytes: traceBodyReadDefaultWindowMB << 20,
	}

	// TrimSpace 之后再判空：全空白的 root（例如 " "）不能被当成「启用了一个空白路径」，
	// 那会走一条失败方式难以诊断的分支。
	root := strings.TrimSpace(v.GetString("trace_body.root"))
	if root == "" {
		return disabled, nil
	}
	if !filepath.IsAbs(root) {
		return TraceBodyReadConfig{}, fmt.Errorf(
			"trace_body.root 必须是绝对路径，实际 %q：相对路径的含义取决于进程工作目录，"+
				"而 /datalog 是要被 bind mount 的绝对路径 —— 相对路径会让正文落到无法预测的位置", root)
	}
	if filepath.Clean(root) == "/" {
		return TraceBodyReadConfig{}, fmt.Errorf(
			"trace_body.root 不得是文件系统根 %q：spool 会被建在宿主根文件系统上（/spool/dt=…），"+
				"这正是 2026-09-24 `/` 被写满的同一类事故", root)
	}

	// 并发：键「未出现」才回落到默认值；**显式配置的 0/-1 必须被拒绝** ——
	// 0 会让信号量容量为 0（等待槽位的请求永久阻塞），而不是「取默认值」。
	c := v.GetInt("trace_body.read_max_concurrent")
	if !v.IsSet("trace_body.read_max_concurrent") {
		c = traceBodyReadDefaultMaxConcurrent
	}
	if c < 1 {
		return TraceBodyReadConfig{}, fmt.Errorf(
			"trace_body.read_max_concurrent 必须 >= 1，实际 %d（0 会让解码并发槽位容量为 0 ⇒ 等待槽位的请求死锁）", c)
	}
	if c > traceBodyReadMaxConcurrent {
		return TraceBodyReadConfig{}, fmt.Errorf(
			"trace_body.read_max_concurrent=%d 超过上界 %d：按 600 MB/并发估算会撑爆 control-plane 的 2 GiB 限额（规格 §8.4 C5）",
			c, traceBodyReadMaxConcurrent)
	}

	// 窗口：单位 MiB，0/负数视为未设置（取默认 512），但**小于写入侧的必须拒绝**。
	mb := v.GetInt("trace_body.read_zstd_window_mb")
	if mb <= 0 {
		mb = traceBodyReadDefaultWindowMB
	}
	if mb < traceBodyWriteSideWindowMB {
		return TraceBodyReadConfig{}, fmt.Errorf(
			"trace_body.read_zstd_window_mb=%d 必须 >= %d MiB（与写入侧窗口一致）："+
				"窗口小于写入侧时 zstd 解码会失败，把配置错伪装成产物损坏",
			mb, traceBodyWriteSideWindowMB)
	}

	return TraceBodyReadConfig{
		Root:            root,
		MaxConcurrent:   c,
		ZstdWindowBytes: mb << 20,
	}, nil
}

// validateReadDT 校验归属天分量（与冻结包 writer.go 的 validateDT 语义等价，
// 同一布局字符串、同一 time.Parse 判据）。
//
// 【为什么不直接调用冻结包的校验】validateDT 未导出，而冻结包的第一硬约束是
// 「本计划不得修改其中任何文件的任何字节」⇒ 只能在调用侧收口（登记 D59）。
func validateReadDT(dt string) error {
	if _, err := time.Parse(traceBodyReadDateLayout, dt); err != nil {
		return fmt.Errorf("定位键的 dt 必须是 %s 形式的合法日期，实际 %q：非法 dt 会拼出 <root> 之外的路径", traceBodyReadDateLayout, dt)
	}
	return nil
}

// ValidateRef 校验定位键的四个分量（计划 1 的 N3：GetBody 自身不校验，
// 冻结包不可改，故在调用侧收口）。任何不合法都必须返回 error 且
// 【不调用 GetBody】——校验先于任何文件系统访问。
//
// 判据（逐条，与冻结包内 validateDT / validateInst 语义等价）：
//
//	DT   : time.Parse("2006-01-02", DT) 必须成功（挡住空串、长度不符、未补零、
//	       非法日期，以及含 "/" 的穿越向量 "2026-09-24/../.."）；
//	Inst : 必须 ∈ {"blue","green"}（白名单，与写入侧同一枚举）；
//	Part : 必须 >= 0 且 <= 999999（partName 用 %06d，前置约束是 [0,999999]）；
//	Row  : 必须 >= 0。
//
// 【顺序固定】dt → inst → part → row：顺序稳定，隔离探针才能按错误文本定位到
// 具体是哪一条守卫承重（计划 1 的 D15/D36 已实证过遮蔽效应）。
// 【纯内存判定】本函数不接收 root，也不接触文件系统：校验必须在拼路径之前完成，
// 否则畸形输入会产生越出 <root> 的产物路径（计划 1 的 D28 实测）。
func ValidateRef(ref tracearchive.Ref) error {
	if err := validateReadDT(ref.DT); err != nil {
		return err
	}
	if ref.Inst != instBlue && ref.Inst != instGreen {
		return fmt.Errorf("定位键的 inst 必须是 %q 或 %q，实际 %q（蓝绿两实例的目录必须隔离）", instBlue, instGreen, ref.Inst)
	}
	if ref.Part < 0 || ref.Part > tracePartSeqMax {
		return fmt.Errorf("定位键的 part 必须在 [0,%d]，实际 %d（越界会产出 7 位文件名，parsePartSeq 会拒绝它）", tracePartSeqMax, ref.Part)
	}
	if ref.Row < 0 {
		return fmt.Errorf("定位键的 row 必须 >= 0，实际 %d", ref.Row)
	}
	return nil
}
