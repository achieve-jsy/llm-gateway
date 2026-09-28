package tracesealer

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

const (
	DefaultZstdLevel    = 19
	DefaultZstdWindowMB = 512
	DefaultExpectedZstd = "1.4.8"
	DefaultMinAge       = 3 * time.Hour
	DefaultPollInterval = 5 * time.Minute
	DefaultSealTimeout  = 30 * time.Minute

	// GatewayMaxRollInterval 是网关滚动间隔的上界（规格 §15.1 R8）。
	// 冻结包的 minAge 是 sealer 侧**唯一**的保护：网关与 sealer 之间没有信号通道，
	// sealer 封块后会删除 spool，而网关若仍持有同一 part 的 fd，追加会写进已被
	// unlink 的 inode —— 落盘成功但永久不可达（D7）。因此 min-age 必须**严格大于**
	// 这个上界（实际还留了 3 倍余量：3h 对 1h）。
	GatewayMaxRollInterval = time.Hour

	// MaxTrailerEntries 是冻结包 maxTrailerEntries 的镜像值。
	// 【必须由测试钉住】它是未导出的，本包只能复制；一旦冻结包改了常量而这里没跟，
	// A8 的「不超上界」判断就会静默失真 ⇒ tests/tracesealer/constants_contract_test.go
	// 现读 reader.go 的源码比对。镜像值：maxTrailerBytes(64<<20) / 8 = 8,388,608。
	// （本任务只镜像、不使用：A8 的观测在计划 4/4 的任务 10 落地。）
	MaxTrailerEntries = 8_388_608

	// MinAgeFloor 是 --min-age 的强制下界，数值上等于网关滚动间隔上界（1h）；
	// 判据是**严格大于**（`<=` 即拒绝）。见 normalize 的说明。
	MinAgeFloor = time.Hour
)

type Options struct {
	Root                string
	ZstdBinary          string
	ZstdLevel           int
	ZstdWindowMB        int
	MinAge              time.Duration
	ExpectedZstdVersion string
	PollInterval        time.Duration
	SealTimeout         time.Duration
	LockFile            string
	Now                 func() time.Time
	Logf                func(format string, args ...any)
	// Slog 是本进程结构化日志的落地面（任务 2）。优先级与退回见 runner.go 的
	// resolveLogger：Slog > Logf > slog.Default()。
	// 【为什么 Slog 与 Logf 并存】Logf 是任务 1 已有的注入面（normalize 的默认值是
	// log.Printf），删掉它会让既有测试与运维脚本的注入点消失；而日志形态要统一成
	// slog（结构化字段），所以 Logf 只作为 slog 的一个 sink 存在 —— 且**只在
	// Slog == nil 时**被使用，两条并行的打印路径不会同时生效。
	Slog      *slog.Logger
	NewSealer func(ta.SealerOptions) (Sealer, error)
}

// Sealer 是本包对冻结包 (*tracearchive.Sealer) 的窄需求面。
//
// 【为什么定义接口而不是直接用 *tracearchive.Sealer】Round 的编排（逐块、分类、
// 计数、ctx 取消边界）必须在**不触碰真实磁盘与 zstd 子进程**的前提下可测：
// 真实冻结包的失败是「环境的函数」，无法为同一 Ref 复现「成功 + 四类失败」的
// 组合。接口 + Options.NewSealer 注入让这些组合变成确定的。
type Sealer interface {
	ListPendingSeals() ([]ta.SealTask, error)
	Seal(ctx context.Context, task ta.SealTask) error
}

// normalize 填默认值并校验。校验一律**响亮失败**（返回 error）：配置错误静默接受
// 只会把现场推到几小时后的封块失败上，而那时运维已经不知道是谁配错的。
func (o *Options) normalize() error {
	if o.ZstdBinary == "" {
		o.ZstdBinary = "zstd"
	}
	if o.ZstdLevel <= 0 {
		o.ZstdLevel = DefaultZstdLevel
	}
	if o.ZstdWindowMB <= 0 {
		o.ZstdWindowMB = DefaultZstdWindowMB
	}
	if o.MinAge <= 0 {
		o.MinAge = DefaultMinAge
	}
	if o.ExpectedZstdVersion == "" {
		o.ExpectedZstdVersion = DefaultExpectedZstd
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.SealTimeout <= 0 {
		o.SealTimeout = DefaultSealTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	if o.LockFile == "" {
		o.LockFile = filepath.Join(o.Root, ".sealer.lock")
	}
	if o.MinAge <= MinAgeFloor {
		return fmt.Errorf(
			"tracesealer: min-age=%s 必须严格大于网关滚动间隔上界 %s（规格 §15.1 R8）："+
				"sealer 是独立进程、收不到任何信号，它与网关之间唯一的保护就是这个时间差；"+
				"min-age 不大于它就会去封一个网关仍在追加的块（字节写进已 unlink 的 inode ⇒ 永久丢失）",
			o.MinAge, GatewayMaxRollInterval)
	}
	// 窗口必须是 2 的幂（冻结包 zstdLongArg 会拒绝）；先在这里给出更可读的错误，
	// 免得运维在几小时后看到「zstd 窗口必须是 2 的幂」而想不起是哪个 flag。
	if o.ZstdWindowMB&(o.ZstdWindowMB-1) != 0 {
		return fmt.Errorf("tracesealer: zstd-window-mb=%d 必须是 2 的幂（换算为 CLI 的 --long=<log2>）", o.ZstdWindowMB)
	}
	return nil
}
