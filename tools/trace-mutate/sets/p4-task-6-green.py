r"""计划 4 / 任务 6 的变异集合 B：绿编排（`deploy/blue-green/docker-compose.green.yml`）。

主题与蓝集合同构，但**颜色↔inst 的对应关系是反向的**：绿文件的
`TRACE_BODY_INST` 必须是 `green`。两个颜色共用一个全局 YAML 时会让它们写进同一个
`inst=` 目录（part 序号与 spool 互相踩）⇒ 这条一一对应必须被钉住。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 6 步骤 6 的表格里属于本文件的是 **m05**（`TRACE_BODY_INST: green` → `blue`），
逐字采用。**本轮补强一条（登记备查，不是计划 m01–m09 的条目）**：`m05b_绿ROLL改2h` ——
计划的要求「蓝/绿的四个非开关参数必须与规格一致地显式写出」是**对两份颜色文件**说的，
而表格只给了蓝侧的 **m04** ⇒ 绿色迭代的同一断言从未被证明可失败。按 **E64**「缺口可
闭合的情形不得放过」，本轮补一条确定性变异把它闭合（`PACKAGES`/名字清单不变，
所以它不改变权威表的执行集合，只多一行变异行）。

## 变异测不到、但有静态检查行（CHECKS）

同蓝集合：green 文件不得出现短语法挂载（计划步骤 6 的 `CHECKS`）。不存在性 ⇒ 静态检查。
"""

NAME = "p4-task-6-green"
TARGET = "deploy/blue-green/docker-compose.green.yml"
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

# m05 绿文件的 inst 改成 blue ⇒ 两个颜色写进同一个 inst= 目录
#     杀手：TestComposeGreenPinsInstGreen
MUTATIONS["m05_绿inst改成blue"] = [
    ("sub", "      TRACE_BODY_INST: green", "      TRACE_BODY_INST: blue"),
]
# m05b 【本轮补强】滚动区间 1h → 2h ⇒ 绿色迭代的「参数必须与规格一致」也有一条确定性杀手
#     杀手：TestComposeColorFilesCarrySpecTraceBodyValues
MUTATIONS["m05b_绿ROLL改2h"] = [
    ("sub", "      TRACE_BODY_ROLL_INTERVAL: 1h", "      TRACE_BODY_ROLL_INTERVAL: 2h"),
]

CHECKS = [
    {
        "name": "green不得用短语法挂载",
        "file": "deploy/blue-green/docker-compose.green.yml",
        "pattern": r"(?m)-v\s+/datalog|^\s*-\s*/datalog\s*:",
        "expect": "absent",
        "why": ("计划任务 6 步骤 6 的 CHECKS：不得出现 `-v /datalog` 或 "
                "`volumes: … :/datalog` 的短语法（短语法下 Docker 会替你创建不存在的"
                "宿主路径）。**不存在性**要求 ⇒ 静态检查；可失败性由 "
                "`p4-task-6-shortsyntax-probe-at-*.txt` 的临时仓库探针实测。"),
    },
]

# 本集合**没有任何**允许的绿：m05 与 m05b 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "绿文件的 TRACE_BODY_INST 必须是 green（颜色↔inst 一一对应）",
        "mutations": ["m05_绿inst改成blue"],
        "checks": [],
        "note": "杀手 = TestComposeGreenPinsInstGreen（并反向断言块内不出现 blue 的 inst）。",
    },
    {
        "plan": "蓝/绿必须显式写出四个非开关参数且与规格/计划 2 默认值一致（绿侧那一半）",
        "mutations": ["m05b_绿ROLL改2h"],
        "checks": [],
        "note": ("杀手 = TestComposeColorFilesCarrySpecTraceBodyValues 的**绿色迭代**"
                 "（本轮补强；计划表格的 m04 只覆盖蓝色迭代）。"),
    },
    {
        "plan": "三个编排都不得用 `-v /datalog` / `:/datalog` 短语法挂载",
        "mutations": [],
        "checks": ["green不得用短语法挂载"],
        "note": "不存在性要求 ⇒ 静态检查（本集合负责绿文件那一条）。",
    },
]
