r"""计划 4 / 任务 6 的变异集合 C：根编排（`docker-compose.yml`）。

主题：根编排**没有颜色概念**，因此它只给挂载、**不给任何** `TRACE_BODY_*`。一旦在根
编排里设了 `TRACE_BODY_ROOT`，`docker compose up` 就会再造一个写者，而 `TRACE_BODY_INST`
缺失 ⇒ 启动直接失败；把 `TRACE_BODY_INST` 也一起补上则会两个颜色共写一个 inst= 目录
（part 序号与 spool 互相踩）。两条路都错 ⇒ 根编排的正确形态是「零 TRACE_BODY_*」。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 6 步骤 6 的表格里属于本文件的是 **m06**（插入生效的 `TRACE_BODY_ROOT`）与
**m08**（删掉 cp 的挂载），逐字采用。

## 变异测不到、但有静态检查行（CHECKS）

同另外两份颜色文件：根编排也不得出现短语法挂载（计划步骤 6 的 `CHECKS`）。
**注意**：根本文件里还有 otel-collector 的短语法 `- ./deploy/…:/etc/otelcol/config.yaml:ro`
—— 那是既有的可选项挂载、与 `/datalog` 无关，本行只钉 `/datalog` 的短语法形态。
"""

NAME = "p4-task-6-root"
TARGET = "docker-compose.yml"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_compose_mount_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_compose_mount_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m06 在根编排的 gateway env 里插入**生效**的 TRACE_BODY_ROOT ⇒ 根编排也能「开功能」了，
#     但 TRACE_BODY_INST 缺失 ⇒ 启动直接失败；两步之后就会有人把 INST 也补上，于是
#     根编排变成一个与蓝/绿抢同一个 inst= 目录的第三个写者
#     杀手：TestComposeRootComposeDoesNotEnableTraceBody
MUTATIONS["m06_根编排插入生效ROOT"] = [
    ("sub", "      AUTH_URL: http://127.0.0.1:${CONTROL_PLANE_PORT:-8081}",
            "      AUTH_URL: http://127.0.0.1:${CONTROL_PLANE_PORT:-8081}\n"
            "      TRACE_BODY_ROOT: /datalog/trace-archive-v4"),
]
# m08 删掉根编排里 control-plane 的挂载 ⇒ 控制面看不到归档树（点读功能整体失效）
#     杀手：TestComposeControlPlaneMountsDatalogReadOnly
MUTATIONS["m08_删掉根cp挂载"] = [
    ("sub", "    volumes:\n"
            "      # 控制面只点读（冻结包 reader 全程不写文件）⇒ 只读挂载即可。\n"
            "      - type: bind\n        source: /datalog\n        target: /datalog\n"
            "        read_only: true\n",
            ""),
]

CHECKS = [
    {
        "name": "root不得用短语法挂载",
        "file": "docker-compose.yml",
        "pattern": r"(?m)-v\s+/datalog|^\s*-\s*/datalog\s*:",
        "expect": "absent",
        "why": ("计划任务 6 步骤 6 的 CHECKS：不得出现 `-v /datalog` 或 "
                "`volumes: … :/datalog` 的短语法（短语法下 Docker 会替你在**宿主根 FS** 上"
                "创建不存在的挂载源）。**不存在性**要求 ⇒ 静态检查；可失败性由 "
                "`p4-task-6-shortsyntax-probe-at-*.txt` 的临时仓库探针实测。"
                "本行只钉 `/datalog`：根编排里 otel-collector 的既有短语法挂载"
                "（`- ./deploy/…:/etc/otelcol/config.yaml:ro`）与 /datalog 无关，不在射程内。"),
    },
    {
        "name": "root不得有生效TRACE_BODY_赋值",
        "file": "docker-compose.yml",
        "pattern": r"(?m)^\s*TRACE_BODY_[A-Z0-9_]+\s*:",
        "expect": "absent",
        "why": ("**E68①**（门禁口径订正后的辅助读数）：根编排「不启用功能」的判据是"
                "**非注释生效行 = 0** —— 真正的门禁是语义测试 "
                "`TestComposeRootComposeDoesNotEnableTraceBody`（`effectiveAssignments` 只收"
                "非注释行；m06 的杀手），本行只补一条**文本**读数：注释行以 `#` 起头 ⇒ 不匹配本"
                "正则。**`grep -c TRACE_BODY docker-compose.yml` 不是门禁**：注释里可合法出现"
                "该族**前缀**（E68① 订正后 = 1、订正前 = 2，两处都在注释里），它数到的是注释。"
                "**不存在性**要求 ⇒ 静态检查；可失败性由 `p4-task-6-rootcheck-probe-at-*.txt` 的"
                "临时仓库探针实测（在临时克隆里插入一行生效赋值并提交 ⇒ 本行 FAIL、工具以"
                "「静态检查失败 ⇒ 整轮失败」退出 1）。"),
    },
]

# 本集合**没有任何**允许的绿：m06 与 m08 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "根编排不得启用调用链正文直写：不得出现任何生效的 TRACE_BODY_*",
        "mutations": ["m06_根编排插入生效ROOT"],
        "checks": ["root不得有生效TRACE_BODY_赋值"],
        "note": ("杀手 = TestComposeRootComposeDoesNotEnableTraceBody（它断言整文件里"
                 "生效的 TRACE_BODY_* 集合为空 —— 比只钉 TRACE_BODY_ROOT 更贴「根编排没有"
                 "颜色概念」）。**门禁口径 = 非注释生效行 = 0**（**E68①**）：语义测试是真正的"
                 "门禁，文本读数 `root不得有生效TRACE_BODY_赋值` 只是辅助；"
                 "`grep -c TRACE_BODY docker-compose.yml` **不是**门禁。"),
    },
    {
        "plan": "三个编排里 control-plane 必须只读挂载 /datalog（根编排那一半）",
        "mutations": ["m08_删掉根cp挂载"],
        "checks": [],
        "note": "杀手 = TestComposeControlPlaneMountsDatalogReadOnly 的根编排迭代。",
    },
    {
        "plan": "三个编排都不得用 `-v /datalog` / `:/datalog` 短语法挂载",
        "mutations": [],
        "checks": ["root不得用短语法挂载"],
        "note": "不存在性要求 ⇒ 静态检查（本集合负责根编排那一条）。",
    },
]
