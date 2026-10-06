r"""计划 4 / 任务 5 的变异集合 C：旧归档定时器**回滚**脚本
（`deploy/systemd/restore-old-trace-archive.sh`）。

主题：**回滚也必须默认 dry-run，且判据是「重新出现在 list-timers 里」而不是「命令没报错」**。
隔离脚本 mask 掉 `trace-archive.timer` 之后，`/datalog` 在隔离窗口内没有 retention
（见计划的 R1 风险条），所以回滚的**唯一**可判定的成功条件就是旧 timer 又能被
`systemctl list-timers` 看到、并且 `is-enabled` 与 `is-active` 都为真。

## 与计划文本的一致性（如实登记：**跨文件拆分**、零削弱）

计划任务 5 步骤 6 的 B 集合表格把 m05（`unmask` 改回 `mask`）写在 isolate 集合里，但
`TARGET` 是单值（全局约束第 42 条：工具一次只能变异一个文件）⇒ m05 就地落到本集合
（与 `p4-task-4-lock.py` / `p4-task-4-runner.py` 同形）。计划表格的编号原样保留，
本集合的条目名带 `m05` 前缀以便与计划表格对号。

## 变异测不到、但有静态检查行（CHECKS）

- **`set -euo pipefail` 必须出现**：回滚一次跑完「留证 → unmask → enable --now → 复核」，
  中间任何一步失败都必须立刻中止 —— 特别是「enable 失败但脚本继续往下跑、最后 grep 出
  一行旧输出就宣告成功」这种假绿。取值要求 ⇒ 静态检查。
- **必须复核 `list-timers`**：`systemctl enable --now` 返回 0 不等于 timer 真的排上了
  （mask 残留、unit 名写错都可能返回 0 或留下 inactive）。正则断言「复核命令存在」是
  形态要求 ⇒ 静态检查；行为面由 `TestRestoreScriptUnmasksAndEnables` 承载（它同时断言
  `is-enabled --quiet` 与 `is-active --quiet` 两条复核都在）。
"""

NAME = "p4-task-5-restore"
TARGET = "deploy/systemd/restore-old-trace-archive.sh"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_isolate_script_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_isolate_script_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：脚本**不执行**（它会动 systemd，只能在生产机上经人工确认点执行）
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m05 回滚脚本把 unmask 写成 mask（语义反转）⇒ 「回滚」执行之后旧 timer 反而被再次
#     mask 掉：运维以为回滚完成了，实际隔离被加固了
#     杀手：TestRestoreScriptUnmasksAndEnables
MUTATIONS["m05_unmask改回mask"] = [
    ("sub", 'systemctl unmask "$OLD_TIMER"', 'systemctl mask "$OLD_TIMER"'),
]

CHECKS = [
    {
        "name": "回滚脚本必须fail-fast",
        "file": "deploy/systemd/restore-old-trace-archive.sh",
        "pattern": r"set -euo pipefail",
        "expect": "present",
        "why": ("回滚一次跑完「留证 → unmask → enable --now → 复核」，任何一步失败都必须"
                "立刻中止；否则会出现「enable 失败但脚本继续跑完并打印完成」的假绿。"
                "取值要求 ⇒ 静态检查。"),
    },
    {
        "name": "回滚必须复核list-timers",
        "file": "deploy/systemd/restore-old-trace-archive.sh",
        "pattern": r'systemctl list-timers --all --no-pager \| grep -F "\$OLD_TIMER"',
        "expect": "present",
        "why": ("`enable --now` 返回 0 不等于 timer 真的排上了。判据是「重新出现在 "
                "list-timers 里」，不是「命令没报错」⇒ 形态要求 ⇒ 静态检查；行为面由 "
                "TestRestoreScriptUnmasksAndEnables 承载。"),
    },
]

# 本集合**没有任何**允许的绿：m05 是断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "回滚必须真的解除隔离：`unmask` → `enable --now`，且默认 dry-run（闸门在变更之前）",
        "mutations": ["m05_unmask改回mask"],
        "checks": [],
        "note": ("杀手 = TestRestoreScriptUnmasksAndEnables（它还断言 `[ \"$CONFIRM\" != \"1\" ]` "
                 "闸门出现在 unmask 之前，并反向断言回滚脚本里没有 `systemctl mask`）。"),
    },
    {
        "plan": "回滚的判据是「旧 timer 重新出现在 list-timers / is-enabled / is-active 里」，不是「命令没报错」",
        "mutations": [],
        "checks": ["回滚必须复核list-timers"],
        "note": "形态要求 ⇒ 静态检查；行为面由 TestRestoreScriptUnmasksAndEnables 承载。",
    },
    {
        "plan": "回滚脚本必须 fail-fast（`set -euo pipefail`）",
        "mutations": [],
        "checks": ["回滚脚本必须fail-fast"],
        "note": "取值要求 ⇒ 静态检查（没有任何变异形态能让它变成断言红）。",
    },
]
