r"""计划 4 / 任务 6 的变异集合 A：蓝编排（`deploy/blue-green/docker-compose.blue.yml`）。

主题：**蓝文件的挂载与 env 契约**。gateway 读写挂 `/datalog`（spool 是功能开启的
必要条件），control-plane 只读挂；`TRACE_BODY_INST=blue` 与颜色一一对应；四个非开关
参数与规格/计划 2 的默认值一致地**显式**写出（C2：生产配置不靠代码默认值）；
`TRACE_BODY_ROOT` 保持注释（功能关闭），一旦生效则其父目录必须正好是某个挂载目标。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 6 步骤 6 的表格里属于本文件的是 **m01 / m02 / m03 / m04 / m07 / m09** 共
**6** 条，逐字采用（m05 属 `docker-compose.green.yml`、m06/m08 属 `docker-compose.yml`
⇒ 按全局约束第 42 条「一个集合只变异一个文件」分到另外两个集合）。

## 变异测不到、但有静态检查行（CHECKS）

计划步骤 6 的 `CHECKS` 要求：三个文件里都**不得**出现 `-v /datalog` 或
`volumes: … :/datalog` 的短语法形态（长语法是「不许 Docker 代建宿主路径」的可读证据链
之一）。这是**不存在性**要求 ⇒ 只能静态检查；行为面由 `TestComposeGatewayMountsDatalogReadWrite`
的 `mountTo` 断言（挂载必须是 `type: bind`）承载。
可失败性由本目录的 `p4-task-6-shortsyntax-probe-at-*.txt` 实测（把这一行换成短语法
`- /datalog:/datalog` 并提交到临时克隆 ⇒ 本行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」
退出 1）。
"""

NAME = "p4-task-6-blue"
TARGET = "deploy/blue-green/docker-compose.blue.yml"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_compose_mount_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_compose_mount_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：不执行 docker、不解析网络，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 网关的挂载目标改成 /datalog2 ⇒ 容器里根本没有 /datalog，spool 写不进去
#     （而宿主上的 /datalog 依然“看起来”是挂着的 —— 这正是最容易被忽略的形态）
#     杀手：TestComposeGatewayMountsDatalogReadWrite
MUTATIONS["m01_网关挂载目标改错"] = [
    ("sub", "        target: /datalog\n    healthcheck:",
            "        target: /datalog2\n    healthcheck:"),
]
# m02 删掉控制面的 read_only ⇒ 控制面拿到写权限（它与网关同时写同一棵归档树）
#     杀手：TestComposeControlPlaneMountsDatalogReadOnly
MUTATIONS["m02_删掉控制面只读"] = [
    ("sub", "      - type: bind\n        source: /datalog\n        target: /datalog\n        read_only: true",
            "      - type: bind\n        source: /datalog\n        target: /datalog"),
]
# m03 蓝文件的 inst 改成 green ⇒ 两个颜色写进同一个 inst= 目录，part 序号与 spool 互相踩
#     杀手：TestComposeBluePinsInstBlue
MUTATIONS["m03_蓝inst改成green"] = [
    ("sub", "      TRACE_BODY_INST: blue", "      TRACE_BODY_INST: green"),
]
# m04 滚动区间 1h → 2h ⇒ 与规格/计划 2 的默认值不一致（生产配置必须显式且一致）
#     杀手：TestComposeColorFilesCarrySpecTraceBodyValues
MUTATIONS["m04_ROLL改2h"] = [
    ("sub", "      TRACE_BODY_ROLL_INTERVAL: 1h", "      TRACE_BODY_ROLL_INTERVAL: 2h"),
]
# m07 把 TRACE_BODY_ROOT 取消注释成 /datalog-typo/trace-archive-v4 ⇒ 父目录 /datalog-typo
#     没有任何挂载 ⇒ Docker 会在**宿主根文件系统**上现建一个目录，正文写到根盘上
#     （2026-09-24 `/` 被写满的同一类事故；D70）
#     杀手：TestComposeTraceBodyRootParentIsMounted
MUTATIONS["m07_ROOT指到未挂目录"] = [
    ("sub", "      # TRACE_BODY_ROOT: /datalog/trace-archive-v4",
            "      TRACE_BODY_ROOT: /datalog-typo/trace-archive-v4"),
]
# m09 插入无效键 TRACE_BODY_SEAL_INTERVAL ⇒ 运维会以为「改了就生效」，实际网关读的是
#     TRACE_BODY_ROLL_INTERVAL（D64/D71：假配置比缺失配置更危险）
#     杀手：TestComposeNeverCarriesSealIntervalKey
MUTATIONS["m09_插入无效SEAL键"] = [
    ("sub", "      TRACE_BODY_ROLL_INTERVAL: 1h",
            "      TRACE_BODY_ROLL_INTERVAL: 1h\n      TRACE_BODY_SEAL_INTERVAL: 15m"),
]

CHECKS = [
    {
        "name": "blue不得用短语法挂载",
        "file": "deploy/blue-green/docker-compose.blue.yml",
        "pattern": r"(?m)-v\s+/datalog|^\s*-\s*/datalog\s*:",
        "expect": "absent",
        "why": ("计划任务 6 步骤 6 的 CHECKS：不得出现 `-v /datalog` 或 "
                "`volumes: … :/datalog` 的短语法。短语法把 source/target 挤成一个不透明的"
                "字符串，且**当 source 不存在时 Docker 会替你建目录**（正是在宿主根 FS 上"
                "现建挂载源的那条路）。**不存在性**要求 ⇒ 静态检查；行为面由 "
                "TestComposeGatewayMountsDatalogReadWrite 的 `mountTo`（要求 `type: bind`）"
                "承载。可失败性由 `p4-task-6-shortsyntax-probe-at-*.txt` 的临时仓库探针实测。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m04 / m07 / m09 全部断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "三个编排里 gateway 都必须读写挂载 /datalog（长语法 type: bind，target=/datalog）",
        "mutations": ["m01_网关挂载目标改错"],
        "checks": [],
        "note": "杀手 = TestComposeGatewayMountsDatalogReadWrite（对蓝/绿/根各断言一次）。",
    },
    {
        "plan": "三个编排里 control-plane 必须只读挂载 /datalog",
        "mutations": ["m02_删掉控制面只读"],
        "checks": [],
        "note": "杀手 = TestComposeControlPlaneMountsDatalogReadOnly（对蓝/绿/根各断言一次）。",
    },
    {
        "plan": "蓝文件的 TRACE_BODY_INST 必须是 blue（颜色↔inst 一一对应）",
        "mutations": ["m03_蓝inst改成green"],
        "checks": [],
        "note": "杀手 = TestComposeBluePinsInstBlue（并反向断言块内不出现 green 的 inst）。",
    },
    {
        "plan": "蓝/绿必须显式写出四个非开关参数且与规格/计划 2 默认值一致（BLOCK_MB 1536 / FLUSH 1m / ROLL 1h / WATERMARK 90）",
        "mutations": ["m04_ROLL改2h"],
        "checks": [],
        "note": ("杀手 = TestComposeColorFilesCarrySpecTraceBodyValues（蓝色迭代；绿色迭代的"
                 "同类变异在 `p4-task-6-green.py` 的 m05b —— 计划表格只给了蓝侧 m04，"
                 "绿侧那一半由本轮补强，避免「同一断言在另一份颜色文件上从未被证明可失败」）。"),
    },
    {
        "plan": "生效的 TRACE_BODY_ROOT 的父目录必须正好是同文件里的某个挂载目标（D70 的机器可判定形态）",
        "mutations": ["m07_ROOT指到未挂目录"],
        "checks": [],
        "note": "杀手 = TestComposeTraceBodyRootParentIsMounted（对蓝/绿/根各扫一次生效的 TRACE_BODY_ROOT）。",
    },
    {
        "plan": "编排里不得出现无效键 TRACE_BODY_SEAL_INTERVAL（D64/D71）",
        "mutations": ["m09_插入无效SEAL键"],
        "checks": [],
        "note": "杀手 = TestComposeNeverCarriesSealIntervalKey（对三个文件各断言一次）。",
    },
    {
        "plan": "三个编排都不得用 `-v /datalog` / `:/datalog` 短语法挂载（长语法是「不许 Docker 代建宿主路径」的证据链）",
        "mutations": [],
        "checks": ["blue不得用短语法挂载"],
        "note": ("不存在性要求 ⇒ 静态检查（本集合负责蓝文件那一条；绿/根各在各自集合里）。"
                 "可失败性由临时仓库探针实测。"),
    },
]
