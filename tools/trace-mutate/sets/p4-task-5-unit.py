r"""计划 4 / 任务 5 的变异集合 A：systemd unit（`deploy/systemd/trace-sealer.service`）。

主题：**部署产物即契约**。unit 的每条关键指令都是一条对外承诺 —— 形态（常驻 vs
oneshot）、重启策略、编码器参数（绝对路径 + 版本）、min-age 下界、挂载依赖、可写面
收窄、资源限额、日志去向。全部由 `tests/deploy/trace_sealer_unit_test.go` 的顶层用例
（`package deploy_test`，只读文件）钉住（全局约束第 58 条）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：**零削弱、两处加强**）

1. **计划任务 5 步骤 6 的 m01–m12 逐条采用**，语义与杀手与计划表格一致。
2. **新增 m13/m14**（计划写着「**至少**」，因此允许加）：计划表格只把
   `--zstd-level 19` / `--zstd-window-mb 512` 交给静态检查（全局约束第 37 条；
   本集合的 `CHECKS` 也照此保留两行），但这两条同样是**取值**要求、有确定的杀手
   （`TestSealerUnitPinsZstdBinaryLevelAndVersion`）⇒ 按 **E62**「行为要求必须由在册
   行为用例承载」把它们也做成断言红，比只留静态检查更强。
3. **失败测试的落点（步骤 3 的形态）**：`readRepoFile` 对缺失的产物**直接失败**
   （`require.NoError`），因此「产物文件不存在 ⇒ `go test ./tests/deploy/` FAIL」这条
   步骤 3 的行为是可复现的（本轮以人工探针实测，见报告；不是「跳过」）。

## 变异测不到、但有静态检查行（CHECKS）

- **`--zstd-level 19` 与 `--zstd-window-mb 512` 必须出现**（全局约束第 37 条：编码器
  参数终值逐字钉住）。m13/m14 已覆盖行为面，这两行是同一要求的**文本**面（两者互补，
  不存在只靠静态检查的缺口）。
- **不得出现 `ExecStartPre`**（含 `ExecStartPost`）：「启动前先建目录/先写文件」正是把
  消失的挂载点重建到**宿主根文件系统**上的入口（2026-09-24 `/` 被写满的同族事故）。
  让 systemd 在启动前建目录会绕过进程内的根目录身份护栏 ⇒ **不存在性**要求 ⇒ 只能静态
  检查（变异无法证明「没写」）。同一条要求的行为面由
  `TestSealerUnitNeverPreparesFilesystem` 承载（它同时也断言了 `mkdir` 与
  `TRACE_BODY_ROOT` 不出现）。
- **不得出现 `TRACE_BODY_ROOT`**：sealer 用 `--root`；`TRACE_BODY_ROOT` 是**网关侧**的
  接口名（计划 2）。在 unit 里写它等于把「谁决定 root」这件事复制成两份，而且会让运维
  以为改 unit 就能改 sealer 的 root ⇒ 不存在性要求 ⇒ 静态检查。
"""

NAME = "p4-task-5-unit"
TARGET = "deploy/systemd/trace-sealer.service"
# 一个包（仅公开 API）：本集合的被变异文件是**部署产物文本**，用例只读它。
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_sealer_unit_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_sealer_unit_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：不执行二进制、不碰磁盘、不依赖 zstd/生产机，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 常驻形态改成 oneshot（配 timer 用）⇒ Restart=on-failure 失去意义，且「timer 被
#     mask 了」只能靠 systemctl list-timers 才发现
#     杀手：TestSealerUnitIsResidentLoopWithRestart
MUTATIONS["m01_常驻改oneshot"] = [
    ("sub", "Type=simple", "Type=oneshot"),
]
# m02 失败不再重启 ⇒ 致命错误（zstd 版本不符、挂载消失）之后 sealer 静默停在那里
#     杀手：TestSealerUnitIsResidentLoopWithRestart
MUTATIONS["m02_失败不重启"] = [
    ("sub", "Restart=on-failure", "Restart=no"),
]
# m03 期望 zstd 版本漂移 ⇒ 10.26× 的压缩比标定（v1.4.8 专属）失效，且启动期版本校验
#     会放行一个未标定的编码器
#     杀手：TestSealerUnitPinsZstdBinaryLevelAndVersion
MUTATIONS["m03_zstd版本漂移"] = [
    ("sub", "--expected-zstd-version 1.4.8", "--expected-zstd-version 1.5.7"),
]
# m04 不给绝对路径 ⇒ 走 PATH；宿主机 PATH 漂移（多版本 zstd、conda、自编译）会让版本
#     探测与压缩比同时失效
#     杀手：TestSealerUnitPinsZstdBinaryLevelAndVersion
MUTATIONS["m04_zstd走PATH"] = [
    ("sub", "    --zstd-binary /usr/bin/zstd \\\n", ""),
]
# m05 min-age 掉到网关滚动上界（1h）之下 ⇒ 网关还在往这一块追加时就封块，产物残缺
#     杀手：TestSealerUnitPinsMinAgeAboveGatewayRollBound
MUTATIONS["m05_min-age过小"] = [
    ("sub", "--min-age 3h", "--min-age 30m"),
]
# m06 挂载依赖指到一个不存在的路径 ⇒ 「挂载不在场就不启动」这道纵深防御失效
#     （注意：`RequiresMountsFor=/datalog` 的「保留前缀」改写必须能被断言抓到 ——
#      所以用例用**整行相等**而不是子串）
#     杀手：TestSealerUnitRequiresDatalogMount
MUTATIONS["m06_挂载依赖错位"] = [
    ("sub", "RequiresMountsFor=/datalog", "RequiresMountsFor=/datalog/other"),
]
# m07 可写面放大到宿主根 ⇒ 「因为建目录/写日志把 `/` 写满」这类事故（2026-09-24）直接
#     重新打开
#     杀手：TestSealerUnitConfinesWritesToDatalog
MUTATIONS["m07_可写面放大到根"] = [
    ("sub", "ReadWritePaths=/datalog", "ReadWritePaths=/"),
]
# m08 关掉整个文件系统保护 ⇒ PrivateTmp/ProtectHome 一类收窄全部失效
#     杀手：TestSealerUnitConfinesWritesToDatalog
MUTATIONS["m08_取消ProtectSystem"] = [
    ("sub", "ProtectSystem=strict", "ProtectSystem=no"),
]
# m09 去掉内存上限 ⇒ 512 MiB 窗口的编码器（RSS ≈ 570 MB）无上限，和网关抢内存
#     杀手：TestSealerUnitLimitsMemoryCpuAndIO
MUTATIONS["m09_去掉内存上限"] = [
    ("sub", "MemoryMax=2G\n", ""),
]
# m10 改 syslog 标识 ⇒ 按 SyslogIdentifier 匹配的告警规则全部失配（日志还在，告警没了）
#     杀手：TestSealerUnitLogsToJournal
MUTATIONS["m10_改syslog标识"] = [
    ("sub", "SyslogIdentifier=trace-sealer", "SyslogIdentifier=trace-sealer2"),
]
# m11 塞进一个**不存在的**配置键（TRACE_BODY_SEAL_INTERVAL 是规格 §11.1 的旧名，D64/D71
#     已裁定它不是有效键名）⇒ 运维以为改 unit 就改了封块间隔，实际毫无作用
#     杀手：TestSealerUnitNeverUsesSealIntervalKey
MUTATIONS["m11_塞入无效配置键"] = [
    ("sub", "StandardOutput=journal\n",
            "Environment=TRACE_BODY_SEAL_INTERVAL=15m\nStandardOutput=journal\n"),
]
# m12 锁文件挪到 root 之外（/tmp）⇒ root 之外没有可写保证；且 PrivateTmp=true 下
#     /tmp 是每个 unit 私有的 ⇒ 两个 sealer 会各自加锁成功
#     杀手：TestSealerUnitKnowsItsRootAndLockFile
MUTATIONS["m12_锁文件挪出root"] = [
    ("sub", "--lock-file /datalog/trace-archive-v4/.sealer.lock", "--lock-file /tmp/sealer.lock"),
]
# m13 【本轮新增】zstd 级别改小 ⇒ 体积契约（压缩比标定）失效
#     杀手：TestSealerUnitPinsZstdBinaryLevelAndVersion
MUTATIONS["m13_zstd级别改小"] = [
    ("sub", "--zstd-level 19", "--zstd-level 3"),
]
# m14 【本轮新增】窗口改小 ⇒ `--long=29` 的前提（512 MiB 窗口）不成立，块内偏移表
#     的解码内存预算与压缩比同时失真
#     杀手：TestSealerUnitPinsZstdBinaryLevelAndVersion
MUTATIONS["m14_zstd窗口改小"] = [
    ("sub", "--zstd-window-mb 512", "--zstd-window-mb 64"),
]

CHECKS = [
    {
        "name": "unit钉住zstd级别",
        "file": "deploy/systemd/trace-sealer.service",
        "pattern": r"--zstd-level 19",
        "expect": "present",
        "why": ("全局约束第 37 条要求编码器参数终值逐字钉住（`-19 --long=29 -T1`）。"
                "m13 已覆盖行为面，这一行是同一要求的文本面（进程侧的默认值另有 "
                "`internal/tracesealer` 的常量契约用例）。"),
    },
    {
        "name": "unit钉住zstd窗口",
        "file": "deploy/systemd/trace-sealer.service",
        "pattern": r"--zstd-window-mb 512",
        "expect": "present",
        "why": ("`--long=29` 的前提是窗口 512 MiB；窗口是**取值**要求 ⇒ 静态检查（m14 "
                "覆盖行为面）。"),
    },
    {
        "name": "unit不得有ExecStartPre",
        "file": "deploy/systemd/trace-sealer.service",
        "pattern": r"ExecStartPre",
        "expect": "absent",
        "why": ("「启动前先建目录/先写文件」会把消失的挂载点重建到**宿主根文件系统**上"
                "（2026-09-24 `/` 被写满的同族事故），并且绕过进程内的根目录身份护栏 ⇒ "
                "**不存在性**要求 ⇒ 只能静态检查（变异无法证明「没写」）。行为面由 "
                "TestSealerUnitNeverPreparesFilesystem 承载。"),
    },
    {
        "name": "unit不得出现TRACE_BODY_ROOT",
        "file": "deploy/systemd/trace-sealer.service",
        "pattern": r"TRACE_BODY_ROOT",
        "expect": "absent",
        "why": ("sealer 用 `--root`；`TRACE_BODY_ROOT` 是网关侧（计划 2）的接口名。写进 "
                "unit 等于把「谁决定 root」复制成两份，并让运维误以为改 unit 就能改 "
                "sealer 的 root ⇒ 不存在性要求 ⇒ 静态检查（行为面由 "
                "TestSealerUnitNeverPreparesFilesystem 承载）。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m14 全部断言红。
EXPECTED_GREEN = {}
REQUIREMENTS = [
    {
        "plan": "sealer 是**常驻循环**（Type=simple）而不是 oneshot+timer，且致命错误后重试（Restart=on-failure）",
        "mutations": ["m01_常驻改oneshot", "m02_失败不重启"],
        "checks": [],
        "note": ("杀手 = TestSealerUnitIsResidentLoopWithRestart（它同时断言了 "
                 "RestartSec= 与 ExecStart 的绝对路径，并反向断言 oneshot / always 不出现）。"),
    },
    {
        "plan": "编码器参数逐字锁定：`--zstd-binary /usr/bin/zstd --zstd-level 19 --zstd-window-mb 512 --expected-zstd-version 1.4.8`（换版本必须重新标定）",
        "mutations": ["m03_zstd版本漂移", "m04_zstd走PATH", "m13_zstd级别改小", "m14_zstd窗口改小"],
        "checks": ["unit钉住zstd级别", "unit钉住zstd窗口"],
        "note": ("杀手 = TestSealerUnitPinsZstdBinaryLevelAndVersion（四条变异都杀它）。"
                 "级别/窗口另有两行静态检查（全局约束第 37 条），与行为面互补。"),
    },
    {
        "plan": "R8：min-age 取 3h（网关滚动上界 1h1m 的 3 倍），不得被静默改小",
        "mutations": ["m05_min-age过小"],
        "checks": [],
        "note": "杀手 = TestSealerUnitPinsMinAgeAboveGatewayRollBound（另有反向断言 30m/1h 不出现）。",
    },
    {
        "plan": "root 与锁文件必须在同一个 root 里（锁在 root 之外没有可写保证）",
        "mutations": ["m12_锁文件挪出root"],
        "checks": [],
        "note": "杀手 = TestSealerUnitKnowsItsRootAndLockFile。",
    },
    {
        "plan": "挂载依赖（纵深防御）：`RequiresMountsFor=/datalog` + `After=local-fs.target`",
        "mutations": ["m06_挂载依赖错位"],
        "checks": [],
        "note": "杀手 = TestSealerUnitRequiresDatalogMount（整行相等，防止「保留前缀」的改写逃逸）。",
    },
    {
        "plan": "可写面收窄（承重）：除 /datalog 之外整个文件系统只读 + NoNewPrivileges/PrivateTmp/ProtectHome",
        "mutations": ["m07_可写面放大到根", "m08_取消ProtectSystem"],
        "checks": [],
        "note": "杀手 = TestSealerUnitConfinesWritesToDatalog。",
    },
    {
        "plan": "资源限额（规格 §11.2 V1）：MemoryMax/MemoryHigh/CPUWeight/IOWeight/IOScheduling*/Nice/LimitNOFILE",
        "mutations": ["m09_去掉内存上限"],
        "checks": [],
        "note": "杀手 = TestSealerUnitLimitsMemoryCpuAndIO。",
    },
    {
        "plan": "日志全部进 journald 且标识固定（告警规则按 SyslogIdentifier 匹配）",
        "mutations": ["m10_改syslog标识"],
        "checks": [],
        "note": "杀手 = TestSealerUnitLogsToJournal。",
    },
    {
        "plan": "D64/D71：unit 里不得出现 `SEAL_INTERVAL` / `--seal-interval`（不是有效键名，会冒充有效配置）",
        "mutations": ["m11_塞入无效配置键"],
        "checks": [],
        "note": "杀手 = TestSealerUnitNeverUsesSealIntervalKey。",
    },
    {
        "plan": "D70 同族：启动前不得有任何建目录/写文件的形态（ExecStartPre 一类）",
        "mutations": [],
        "checks": ["unit不得有ExecStartPre"],
        "note": ("不存在性要求 ⇒ 静态检查；行为面由 TestSealerUnitNeverPreparesFilesystem "
                 "承载（它还断言 `mkdir` / ExecStartPost 不出现）。"),
    },
    {
        "plan": "sealer 用 `--root` 而不是环境变量：unit 里不得出现网关侧的 `TRACE_BODY_ROOT`",
        "mutations": [],
        "checks": ["unit不得出现TRACE_BODY_ROOT"],
        "note": "不存在性要求 ⇒ 静态检查；行为面同上一条用例。",
    },
]
