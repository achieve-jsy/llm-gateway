package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/spf13/viper"
)

// 本文件是点读 CLI（`cmd/trace-body`，规格 §4.2 的运维与验收工具）的**可导入**部分：
// 退出码映射、参数解析、读侧配置装配、三种输出形态的写出。
//
// 【为什么放在这里而不是 package main】变异证据只在 B 层（`tests/...`）产出，而
// `package main` 不可导入 ⇒ 放在 `cmd/trace-body` 里的守卫无法被任何用例杀死。
// `main.go` 只做「拿到 options → 装配依赖 → 调服务 → 按退出码退出」。
//
// 【它只经 TraceBodyService 取正文与定位键】本文件不查库、不拼归档路径、不引用冻结包的
// 路径函数：正文的唯一来源是 `TraceBodyResult.Body`，定位键的唯一来源是
// `TraceBodyResult.Ref`。唯一引用冻结包的地方是 `OpenArchiveReader`（把配置变成服务需要的
// Reader），它不参与「什么算合法定位键」的判定 ⇒ 不构成 D59 警告的第二份判据。
//
// 【安全】CLI 不做任何归属/权限判定（它用 DSN 直连，仅限运维/本机使用，不得暴露为服务）；
// DSN 与正文一律不进日志/错误文本。

// ExitCodeFor 把点读服务的错误映射成 CLI 退出码（纯函数：无 IO、无状态 ⇒ B 层可测）。
//
//	0 = 取到正文
//	3 = 正文缺失（body_missing：写入时即失败，从未落盘）
//	5 = 读侧未配置（元数据指向归档而读侧禁用 ⇒ 部署错误，需人工介入）
//	4 = 未归档 / 需要人工介入（文件不在、行号越界、残行、产物损坏、元数据与存储不一致）
//	2 = 其它错误（记录不存在、DSN/连接失败、写出失败…）
//
// 退出码 1（用法/参数错误）由 `main` 直接判定（缺必填项、未知开关、root 非法），
// 不经过本函数。
//
// 【为什么 3 与 4 必须分开】「从未落盘」是写入侧已交付的业务事实（正文本来就没有），
// 而「未归档」是元数据与存储不一致（正文应该在、但取不回来）—— 两者的处置完全不同，
// 合并成一个退出码会让排障在第一步就失去分辨能力。
// 【为什么 5 必须独立于 4】读侧禁用是**部署错误**（TRACE_BODY_ROOT 没配），报成存储错误
// 会把运维引向查 spool / 产物目录的死路。
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, ta.ErrBodyMissing) {
		return 3
	}
	if errors.Is(err, ErrTraceBodyDisabled) {
		return 5
	}
	// 「元数据说有归档、存储侧却取不回来」的全部响亮失败都归 4：ErrNotArchived 覆盖
	// 产物与 spool 都不存在 / 行号越界 / 目标偏移未命中行首 / spool 残行；两个 Corrupt
	// 是产物损坏；ErrTraceBodyInconsistent 是 PG 行自身矛盾（服务层哨兵）。
	if errors.Is(err, ta.ErrNotArchived) ||
		errors.Is(err, ta.ErrFooterCorrupt) ||
		errors.Is(err, ta.ErrTrailerCorrupt) ||
		errors.Is(err, ErrTraceBodyInconsistent) {
		return 4
	}
	return 2
}

// TraceBodyCLIMode 是 CLI 的输出形态。
type TraceBodyCLIMode string

const (
	// TraceBodyCLIModeRaw 逐字节把正文写 stdout（默认；可直接管道给 jq / sha256sum）。
	TraceBodyCLIModeRaw TraceBodyCLIMode = "raw"
	// TraceBodyCLIModeJSON 写一行摘要 JSON（source/bytes/sha256）——**不含正文**，
	// 便于在终端或工单里核对「拿到了哪一份、多少字节、摘要是什么」而不泄露正文。
	TraceBodyCLIModeJSON TraceBodyCLIMode = "json"
	// TraceBodyCLIModeRef 只输出定位键四分量 dt/inst/part/row（取值自服务导出的 Ref）。
	TraceBodyCLIModeRef TraceBodyCLIMode = "ref"
)

// TraceBodyCLIOptions 是解析并校验后的 CLI 参数。
type TraceBodyCLIOptions struct {
	Root      string
	DSN       string
	RequestID string
	Mode      TraceBodyCLIMode
}

// ParseTraceBodyCLIOptions 解析并校验 CLI 参数（纯内存：不连库、不触文件系统）。
//
// 【为什么返回 error 而不是 panic / os.Exit】退出码 1 是契约的一部分，且 B 层要能直接
// 断言「缺必填项 ⇒ error」。panic 会让 CLI 在运维手里变成一个带堆栈的崩溃，而不是一条
// 可脚本化的用法错误；`flag.ExitOnError` 则会让错误路径直接 `os.Exit(2)`，绕过本函数的契约。
//
// 模式优先级 `--ref` > `--json` > `--raw`（`--raw` 是默认值 ⇒ 它永远「被设置」，
// 不能按「是否显式给出」判冲突）；三者同时给出时按此优先级取最高者。
func ParseTraceBodyCLIOptions(prog string, args []string) (*TraceBodyCLIOptions, error) {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	// 用法与错误由 main 统一打印（退出码 1）；禁止 flag 包自己往 stderr 写一遍。
	fs.SetOutput(io.Discard)
	root := fs.String("root", "", "归档根目录（绝对路径，等价于读侧的 trace_body.root）")
	dsn := fs.String("dsn", "", "PostgreSQL 连接串（仅限运维本机使用，不写入任何日志）")
	requestID := fs.String("request-id", "", "request_id（点读的唯一定位入口）")
	asJSON := fs.Bool("json", false, "输出一行摘要 JSON（source/bytes/sha256）")
	asRaw := fs.Bool("raw", true, "逐字节把正文写 stdout（默认）")
	asRef := fs.Bool("ref", false, "只输出定位键四分量 dt/inst/part/row")
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("解析参数失败: %w", err)
	}

	// 必填项：缺任一项都必须返回用法错误（CLI 据此退出 1），不得带着半个配置继续跑。
	if *root == "" {
		return nil, errors.New("--root 必填（归档根目录）")
	}
	if *dsn == "" {
		return nil, errors.New("--dsn 必填（PostgreSQL 连接串）")
	}
	if *requestID == "" {
		return nil, errors.New("--request-id 必填")
	}

	mode := TraceBodyCLIModeRaw
	switch {
	case *asRef:
		mode = TraceBodyCLIModeRef
	case *asJSON:
		mode = TraceBodyCLIModeJSON
	case *asRaw:
		mode = TraceBodyCLIModeRaw
	}

	return &TraceBodyCLIOptions{
		Root:      *root,
		DSN:       *dsn,
		RequestID: *requestID,
		Mode:      mode,
	}, nil
}

// CLIReadConfig 按 `--root` 装配读侧配置，复用生产侧的 LoadTraceBodyReadConfig 校验
// （绝对路径、不得是文件系统根、并发 1..16、窗口 >= 写入侧）。
//
// 【为什么不在这里手写一份校验】「什么算合法的 root」必须只有一份判据（D59 的同一条纪律）：
// CLI 是**同一个服务**的另一条入口，它的配置校验与 control-plane 启动路径必须同源。
// 【为什么读侧禁用要在这里就拦下】`--root` 必填，因此走到这里 root 必非空；若校验函数
// 仍判为禁用（TrimSpace 后为空，即" "这类纯空白），那是一次真实的用法错误 ⇒ 报错。
func CLIReadConfig(root string) (cpconfig.TraceBodyReadConfig, error) {
	v := viper.New()
	v.Set("trace_body.root", root)
	cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
	if err != nil {
		return cpconfig.TraceBodyReadConfig{}, err
	}
	if cfg.Disabled() {
		return cpconfig.TraceBodyReadConfig{}, errors.New("--root 去空白后为空：读侧未配置")
	}
	return cfg, nil
}

// OpenArchiveReader 按读侧配置构造冻结包的 Reader（root 必须非空）。
//
// 【为什么放在这里而不是 cmd/trace-body】CLI 只允许经服务层接触归档存储：`main.go` 里
// 出现冻结包的路径函数就落进 CHECKS 的禁止项。本函数只做「配置 → Reader」，不做任何
// 路径拼装（目录布局属于冻结包，读侧不复制）。
func OpenArchiveReader(cfg cpconfig.TraceBodyReadConfig) (*ta.Reader, error) {
	if cfg.Disabled() {
		return nil, errors.New("读侧未配置（root 为空）：CLI 必须显式给出 --root")
	}
	r, err := ta.NewReader(ta.ReaderOptions{
		Root:            cfg.Root,
		ZstdWindowBytes: cfg.ZstdWindowBytes,
		MaxConcurrent:   cfg.MaxConcurrent,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 Reader 失败: %w", err)
	}
	return r, nil
}

// WriteRawBody 把正文**逐字节**写进 w（不追加换行、不做任何转换/清洗）。
//
// 【逐字节是契约】CLI 的输出要能直接管道给 jq / sha256sum：多一个换行就会让下游的解析
// 与摘要全部偏一位（这正是变异 m04 要证明的）。
func WriteRawBody(w io.Writer, body []byte) error {
	_, err := w.Write(body)
	return err
}

// TraceBodyCLISummary 是 `--json` 的摘要形状（**不含正文**）。
type TraceBodyCLISummary struct {
	Source string `json:"source"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// WriteSummaryJSON 写一行摘要 JSON：source / bytes / sha256 全部由服务返回的结果算出。
//
// 【为什么用 sha256 而不是长度就够了】运维要拿它跟写入侧留存的摘要比对：
// 只有长度会被「同长度的另一份正文」蒙混过关。
func WriteSummaryJSON(w io.Writer, res *TraceBodyResult) error {
	sum := sha256.Sum256(res.Body)
	line, err := json.Marshal(TraceBodyCLISummary{
		Source: string(res.Source),
		Bytes:  len(res.Body),
		SHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return fmt.Errorf("序列化摘要失败: %w", err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// WriteRef 输出定位键四分量：dt/inst/part/row。
//
// 【取值唯一来源是服务导出】ref 来自 `TraceBodyResult.Ref`（`main.go` 传 `res.Ref`）：
// 本函数不查库、不拼路径、不构造定位键 —— 否则「什么算合法定位键」会出现第二份实现
// （D59 警告的形态），CLI 会漂移成一个「第二个读实现」。
// 【四个分量必须同时可读】运维要拿它去 `<root>/dt=D/inst=I/part-NNNNNN.jsonl[.zst]`
// 里定位文件与行，缺一个就断链（这正是变异 m05 要证明的）。
func WriteRef(w io.Writer, ref ta.Ref) error {
	_, err := fmt.Fprintf(w, "%s/%s/part-%06d#row=%d\n", ref.DT, ref.Inst, ref.Part, ref.Row)
	return err
}
