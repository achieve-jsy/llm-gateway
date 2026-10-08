r"""计划 4 / 任务 8 的变异集合 B：A3 的**隔离实例编排**（`deploy/verify/trace-a3/docker-compose.yml`）。

主题：A3 要在**一块专用回环 ext4** 上把盘真正写满，而这块盘、这个实例名、这套连接串
必须与生产/蓝绿**完全错开**。四种走样每一种都必须让**点名用例**转红：

- 水位不压到 99 ⇒ 水位闩锁会抢在真实 ENOSPC 之前 ⇒ A3 的语义被偷换（测的不再是「写盘
  真的失败」）。
- 连接串带上默认值 ⇒ 环境变量写错会被默认值掩盖 ⇒ 存在静默连到别处的可能。
- 容器名撞上控制面（`llm-gw-control-plane`）⇒ `docker compose down` 会**误伤生产组件**。
- 挂载源改成 `/datalog` ⇒ 直接往**生产正文盘**上做填盘（2026-09-24 的事故形态）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 8 步骤 6 的 `p4-task-8-compose.py` 逐字给出四条变异与各自杀手（m01–m04），
本文件逐字采用（`m01` 的值、`m04` 的目标都照抄计划）。**追加**的 6 行静态检查只承载
「某段配置必须存在 / 不得存在」这类**存在性**要求（E62），可失败性由交付报告的
「改坏→转红→还原」探针逐条实测。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

**整个 A3 运行期**（`scratch-prepare` 的真实建盘/挂载、`up -d`、真 ENOSPC、闩锁与
降级的读数、`body_missing` 计数、延迟直方图、恢复、A7 撞锁）**不可能**由本集合证明：
本集合只变异一份 compose 文本，而运行期需要 Linux + root + 回环设备 + docker daemon +
测试 PG/Redis。按全局约束第 55 行，A3 这类「重活」只能走 `cmd/trace-verify` 与手工剧本，
**不得**进任何 `p4-task-*.py` 的清单 ⇒ 运行期证据按「待 A3 宿主执行」登记在
`p4-task-8-a3-deferral-at-*.txt`，**不**标 `EXPECTED_GREEN`。
"""

NAME = "p4-task-8-compose"
TARGET = "deploy/verify/trace-a3/docker-compose.yml"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_a3_compose_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
# （同一个测试文件里还有 3 个 run.sh 用例，归 p4-task-8-runsh.py 的清单）
MANIFEST_DERIVATION = (
    "grep -h '^func TestA3Compose' tests/deploy/trace_a3_compose_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 只读文件文本的 B 层用例：不启 docker、不连 PG/Redis，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 水位 99 → 90（默认档）⇒ 水位闩锁抢在真实 ENOSPC 之前 ⇒ A3 语义被偷换。
#     杀手：TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst
MUTATIONS["m01_水位从99改90"] = [
    ("sub", 'TRACE_BODY_DISK_WATERMARK_PERCENT: "99"', 'TRACE_BODY_DISK_WATERMARK_PERCENT: "90"'),
]

# m02 把 A3_DATABASE_URL 的必填（`:?`）改成给默认值（`:-`）⇒ 环境变量写错会被默认值
#     掩盖（「不可能不小心连到生产」靠结构变成了靠自觉）。
#     杀手：TestA3ComposeHasNoConnectionDefaults
MUTATIONS["m02_连接串改成有默认值"] = [
    ("sub",
     "${A3_DATABASE_URL:?A3_DATABASE_URL required（必须是测试库）}",
     "${A3_DATABASE_URL:-postgres://a3:a3@127.0.0.1:5432/llm_gateway_test}"),
]

# m03 容器名改成控制面名 ⇒ 名字撞上生产组件，`docker compose down` 会误伤它。
#     杀手：TestA3ComposeUsesIsolatedNamesAndPorts
MUTATIONS["m03_容器名撞控制面"] = [
    ("sub", "container_name: llm-a3-gateway", "container_name: llm-gw-control-plane"),
]

# m04 挂载源从专用回环目录改成生产 `/datalog` ⇒ 直接对生产正文盘做填盘（D66）。
#     杀手：TestA3ComposeNeverMountsProductionDatalog
MUTATIONS["m04_挂载源改生产盘"] = [
    ("sub", "source: /srv/trace-a3", "source: /datalog"),
]

CHECKS = [
    {
        "name": "A3编排必须只挂回环目录",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r"(?m)^\s+source: /srv/trace-a3\s*$",
        "expect": "present",
        "why": ("计划任务 8 步骤 4 第 1 步：回环镜像/挂载点都在 `/srv/trace-a3` 下，"
                "**这一整套只允许在 `/srv/trace-a3` 上执行**。**存在性**要求 ⇒ 静态检查"
                "（同一批语义由 `TestA3ComposeNeverMountsProductionDatalog` 承载）。"),
    },
    {
        "name": "A3编排不得定义控制面服务",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r"(?m)^\s+control-plane:\s*$",
        "expect": "absent",
        "why": ("计划任务 8 步骤 4 的编排头注释：本实例**只跑 gateway**（复用测试环境已有的"
                "控制面/Redis/PG，刻意不再定义 cp 服务）⇒ 不存在 8091，`down` 的破坏面有界。"
                "**不存在性**要求 ⇒ 静态检查（服务集合的语义判据由 "
                "`TestA3ComposeUsesIsolatedNamesAndPorts` 承载）。"),
    },
    {
        "name": "A3编排不得出现8091生效赋值",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r"(?m)^\s*[A-Za-z_][A-Za-z0-9_]*:.*8091",
        "expect": "absent",
        "why": ("同上，但**只覆盖非注释行**（注释里合法地说明「不存在 8091」—— E68："
                "文本计数不是门禁）。**不存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "A3编排的正文根必须指向隔离挂载点",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r"(?m)^\s+TRACE_BODY_ROOT: /datalog-a3/trace-archive-v4\s*$",
        "expect": "present",
        "why": ("计划任务 8 步骤 4：正文根必须指向容器内的隔离挂载点（不是 `/datalog`），"
                "否则 ENOSPC 会落在生产盘上。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "A3编排必须显式给实例名",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r"(?m)^\s+TRACE_BODY_INST: blue\s*$",
        "expect": "present",
        "why": ("实例名必须显式（不能靠默认值）；`tt` 目录里 spool 的路径（`inst=blue`）"
                "也被 run.sh 的复核步骤引用。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "A3编排的块必须小到能触发ENOSPC",
        "file": "deploy/verify/trace-a3/docker-compose.yml",
        "pattern": r'(?m)^\s+TRACE_BODY_BLOCK_MB: "64"\s*$',
        "expect": "present",
        "why": ("块必须小到能在 1 GiB 回环盘上真的写满（太大了会在写满前先滚动，"
                "A3 就测不到 ENOSPC）。**存在性**要求 ⇒ 静态检查。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m04 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "水位压到 99 ⇒ **真实 ENOSPC 先于水位闩锁**发生（否则 A3 语义被偷换）",
        "mutations": ["m01_水位从99改90"],
        "checks": [],
        "note": "杀手 = TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst（第一条断言就是水位值）。",
    },
    {
        "plan": "连接串一律 `${VAR:?…}`（必填、**无默认值**）⇒ 不可能静默连到别处",
        "mutations": ["m02_连接串改成有默认值"],
        "checks": [],
        "note": ("杀手 = TestA3ComposeHasNoConnectionDefaults（DATABASE_URL / REDIS_ADDR / REDIS_DB "
                 "三条都钉住，`REDIS_PASSWORD` 允许空默认）。"),
    },
    {
        "plan": "实例名/端口与生产及蓝绿完全错开（`llm-a3-*`、8090、host 网络）、只跑 gateway",
        "mutations": ["m03_容器名撞控制面"],
        "checks": ["A3编排不得定义控制面服务", "A3编排不得出现8091生效赋值"],
        "note": ("杀手 = TestA3ComposeUsesIsolatedNamesAndPorts；「只定义一个服务」的语义判据"
                 "（服务键集合）也在该用例里，两条静态检查承载对应**存在/不存在性**要求。"),
    },
    {
        "plan": "**绝不挂生产 `/datalog`**，只挂专用回环目录 `/srv/trace-a3`（D66）",
        "mutations": ["m04_挂载源改生产盘"],
        "checks": ["A3编排必须只挂回环目录"],
        "note": "杀手 = TestA3ComposeNeverMountsProductionDatalog。",
    },
    {
        "plan": "正文根指向隔离挂载点内 `/datalog-a3/trace-archive-v4`、`TRACE_BODY_INST` 显式",
        "mutations": [],
        "checks": ["A3编排的正文根必须指向隔离挂载点", "A3编排必须显式给实例名"],
        "note": ("**存在性**要求 ⇒ 静态检查；同一批语义由 "
                 "`TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst` 一并断言。"),
    },
    {
        "plan": "正文块必须小到能在 1 GiB 回环盘上触发 ENOSPC",
        "mutations": [],
        "checks": ["A3编排的块必须小到能触发ENOSPC"],
        "note": ("**存在性**要求 ⇒ 静态检查；同一批语义由 "
                 "`TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst` 一并断言。"),
    },
]
