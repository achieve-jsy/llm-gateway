// trace-body 是点读服务（internal/observability/service 的 TraceBodyService）的运维与验收 CLI。
//
// 【仅限运维/本机使用，不得暴露为服务】本工具用 `--dsn` 直连 PostgreSQL，**不做**任何
// 归属/权限判定（管理面端点的 RBAC 由服务端做）。它存在的理由有两条（规格 §4.2）：
// ① 生产排障需要一条不经过 HTTP/鉴权的直连路径 —— 否则「归档读不出来」与「HTTP 层的问题」
// 无法分离；② 验收（计划 3 任务 8）需要同一判定顺序的非 HTTP 入口。
//
// 用法:
//
//	trace-body --root <dir> --dsn <postgres-url> --request-id <uuid> [--json|--raw|--ref]
//
// 退出码（与 service.ExitCodeFor 同一张表）:
//
//	0 = 取到正文          1 = 用法/参数错误      2 = 其它错误
//	3 = 正文缺失（body_missing）                 4 = 未归档 / 需要人工介入
//	5 = 读侧未配置（TRACE_BODY_ROOT 为空）
//
// 【复用服务，不自己取数】正文、来源与定位键一律来自 TraceBodyService.GetBody 的
// TraceBodyResult（Body / Source / Ref）：本文件不查 trace 表、不引用冻结包的路径函数、
// 不构造定位键 —— 否则 CLI 会漂移成「第二个读实现」，验收就不再代表服务的行为。
//
// 【不打印 DSN 与正文】DSN 含凭据 ⇒ 不进任何日志与错误文本；正文只写 stdout（或被摘要替代）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/llm-gateway/internal/observability/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// usageText 是 `--help` 与用法错误时的唯一文案来源。
//
// 【为什么明写「仅限运维】】本工具绕过归属判定（DSN 直连），拿到的是提示词正文原字节；
// 告示必须跟着二进制走，而不是只写在某份部署文档里。
const usageText = `trace-body —— 点读服务的运维/验收 CLI（仅限运维、本机使用，不得暴露为服务）

用法:
  trace-body --root <dir> --dsn <postgres-url> --request-id <uuid> [--json|--raw|--ref]

开关:
  --root         归档根目录（绝对路径，等价于读侧的 trace_body.root），必填
  --dsn          PostgreSQL 连接串，必填（仅本机直连；不回显、不写日志）
  --request-id   request_id，必填
  --raw          逐字节把正文写 stdout（默认；可管道给 jq / sha256sum）
  --json         输出一行摘要 {"source":…,"bytes":…,"sha256":…}（不输出正文）
  --ref          只输出定位键四分量 dt/inst/part/row（取自服务导出的结果）

退出码: 0 取到正文 / 1 用法错误 / 2 其它错误 / 3 正文缺失 / 4 未归档·需人工介入 / 5 读侧未配置
`

func main() {
	opts, err := service.ParseTraceBodyCLIOptions("trace-body", os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// --help 不是错误：打印用法后正常退出。
			fmt.Fprint(os.Stdout, usageText)
			return
		}
		fmt.Fprint(os.Stderr, usageText)
		fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
		os.Exit(1)
	}

	// 读侧配置复用生产侧校验（绝对路径、非文件系统根、并发/窗口默认值）。
	cfg, err := service.CLIReadConfig(opts.Root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
		os.Exit(1)
	}

	// Ctrl-C 可中断一次大块解码：Reader.GetBody 会响应 context 取消（规格 §8.4 的并发槽位等待同样如此）。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reader, err := service.OpenArchiveReader(cfg)
	if err != nil {
		// 错误文本只含配置项名与原因，不含 DSN。
		fmt.Fprintf(os.Stderr, "构造归档读取器失败: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = reader.Close() }()

	db, err := gorm.Open(postgres.Open(opts.DSN), &gorm.Config{
		// 【必须静音 gorm 的默认 logger】它的默认输出目标是 **os.Stdout**
		// （vendored `gorm.io/gorm/logger/logger.go:75`：`New(log.New(os.Stdout, "\r\n", log.LstdFlags), …)`）
		// ⇒ 连接失败信息与查询日志会与 `--raw` 的正文**混进同一个 stdout 流**，直接破坏
		// 「逐字节、可管道给 jq / sha256sum」的契约；且它在连接失败时打印连接串的派生信息
		// （`user=… database=…`），与本工具「不回显凭据」的承诺冲突。
		// 诊断由本文件的 stderr 文案 + 服务层返回的错误承担。
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		// 【不得回显 DSN】连接串含凭据。
		fmt.Fprintln(os.Stderr, "连接 PostgreSQL 失败（连接串不回显）")
		os.Exit(2)
	}

	svc, err := service.NewTraceBodyService(db, service.TraceBodyServiceOptions{Config: cfg, Reader: reader})
	if err != nil {
		fmt.Fprintf(os.Stderr, "构造点读服务失败: %v\n", err)
		os.Exit(2)
	}

	res, err := svc.GetBody(ctx, opts.RequestID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "点读失败: %v\n", err)
		os.Exit(service.ExitCodeFor(err))
	}

	switch opts.Mode {
	case service.TraceBodyCLIModeRef:
		ref := res.Ref
		err = service.WriteRef(os.Stdout, ref)
	case service.TraceBodyCLIModeJSON:
		err = service.WriteSummaryJSON(os.Stdout, res)
	default:
		err = service.WriteRawBody(os.Stdout, res.Body)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "写出失败: %v\n", err)
		os.Exit(2)
	}
}
