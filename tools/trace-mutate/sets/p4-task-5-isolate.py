r"""计划 4 / 任务 5 的变异集合 B：旧归档定时器隔离脚本
（`deploy/systemd/isolate-old-trace-archive.sh`）。

主题：**默认 dry-run 与「先核实再上线」的次序不变量**。这个脚本要 mask 掉生产上每天
04:30 的 `trace-archive.timer`，而那个 timer 同时承担归档**与**保留/清理
（`RETAIN_DAYS=5`）—— 隔离窗口内 `/datalog` 没有 retention（见计划的 R1 风险条）。
因此三条承重性质：① 默认不动手（dry-run 闸门在一切变更命令之前）；② 删除谓词审查命中
新树即 `exit 3`（且审查在 mask 之前）；③ 只做 `disable --now` + `mask`，并打印回滚命令。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：一处**跨文件拆分**、零削弱）

1. **m01–m04 逐条采用**计划任务 5 步骤 6 的 B 集合表格。
2. **m05 落在 `p4-task-5-restore.py`**（跨文件，`TARGET` 是另一个脚本）：与 Task 4 的
   `p4-task-4-lock.py` / `p4-task-4-runner.py` 同形 —— 工具的 `TARGET` 是单值
   （全局约束第 42 条），一个集合只能变异一个文件。计划表格的编号原样保留。
3. **`a` 串一律取「带引号的目标」形态**：脚本里同一个命令出现两次（dry-run 的
   `echo "[dry-run] 将要执行： …"` 与实际命令），`sub` 要求 `a` 在文件里**恰好出现一次**
   （`trace_mutate.py` 的 `apply_mutation` 用 `count != 1` 判定）⇒ m03/m04 的 `a` 都取
   带引号/带完整 echo 前缀的形态，既唯一又恰好打在**实际命令**上。

## 变异测不到、但有静态检查行（CHECKS）

- **`set -euo pipefail` 必须出现**：脚本要一次跑完「留证 → 审查 → 停 → mask → 复核」，
  中间任何一步失败都必须立刻中止（`set -e`），未定义变量（`$NEW_ROOT` 拼错）也必须
  立即暴露（`set -u`）——「哪一步失败了」比「静默跑到底」重要得多。取值要求 ⇒ 静态检查。
- **删除谓词审查必须覆盖 `find … -delete` 与 `rsync … --delete`**：旧脚本的清理形态是
  `find <根> -mtime +5 -delete`；只审 `rm -rf` 会漏掉这条最危险的分支（它一旦作用在
  `/datalog` 上就会删掉新树）⇒ 正则覆盖率是**数值/形态**要求 ⇒ 静态检查；行为面由
  `TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot` 承载。
"""

NAME = "p4-task-5-isolate"
TARGET = "deploy/systemd/isolate-old-trace-archive.sh"
# 一个包（仅公开 API）：被变异文件是**运维脚本文本**，用例只读它。
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

# m01 dry-run 闸门反向 ⇒ 默认执行就动手：一次误敲命令把生产旧归档停掉（而且
#     隔离窗口内没有 retention）
#     杀手：TestIsolateScriptDefaultIsDryRun
MUTATIONS["m01_默认不再dry-run"] = [
    ("sub", '[ "$CONFIRM" != "1" ]', '[ "$CONFIRM" = "1" ]'),
]
# m02 命中新树时不再 STOP（把 exit 3 变成 no-op）⇒ 审查只剩「打印」，脚本继续 mask
#     杀手：TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot
MUTATIONS["m02_命中新树不STOP"] = [
    ("sub", "  exit 3\n", "  true\n"),
]
# m03 把 mask 改成 unmask（语义反转）⇒ 该停的 timer 依然在跑，第二天 04:30 照旧清理
#     杀手：TestIsolateScriptStopsAndMasksTimer
MUTATIONS["m03_mask改unmask"] = [
    ("sub", 'systemctl mask "$OLD_TIMER"', 'systemctl unmask "$OLD_TIMER"'),
]
# m04 删掉回滚提示 ⇒ mask 之后运维手里没有回滚入口（文档里也许有，但现场看的是这一步
#     的输出）—— 「回滚不可省」是这一行存在的唯一理由
#     杀手：TestIsolateScriptStopsAndMasksTimer
MUTATIONS["m04_删掉回滚提示"] = [
    ("sub", 'echo "完成。回滚： bash restore-old-trace-archive.sh --confirm"', 'echo "完成。"'),
]

CHECKS = [
    {
        "name": "隔离脚本必须fail-fast",
        "file": "deploy/systemd/isolate-old-trace-archive.sh",
        "pattern": r"set -euo pipefail",
        "expect": "present",
        "why": ("脚本一次跑完「留证 → 审查 → 停 → mask → 复核」，中间任何一步失败都必须"
                "立刻中止；`-u` 还要让 `$NEW_ROOT` 一类拼错立即暴露。取值要求 ⇒ 静态检查。"),
    },
    {
        "name": "删除谓词审查覆盖find-delete",
        "file": "deploy/systemd/isolate-old-trace-archive.sh",
        "pattern": r"find\[\[:space:\]\]\.\*-delete",
        "expect": "present",
        "why": ("旧脚本的清理形态是 `find <根> -mtime +5 -delete`；只审 `rm -rf` 会漏掉"
                "最危险的那条分支（作用在 /datalog 上就会删掉新树）。行为面由 "
                "TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot 承载。"),
    },
    {
        "name": "删除谓词审查覆盖rsync-delete",
        "file": "deploy/systemd/isolate-old-trace-archive.sh",
        "pattern": r"rsync\[\[:space:\]\]\.\*--delete",
        "expect": "present",
        "why": ("同族的第三种删除形态。正则覆盖率是形态要求 ⇒ 静态检查；行为面同上。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m04 全部断言红。
EXPECTED_GREEN = {}

# 「脚本必须带可执行位」是一条**在册行为用例**（TestIsolateAndRestoreScriptsAreExecutable）
# 承载的要求，但工具只能改**文件内容**（`sub`/`cut` 作用于文本），改不动 git mode ⇒
# 没有任何变异能让它变红。按 E64② 登记在这里（工具读不到该字段，故不写进
# EXPECTED_GREEN，它的键会被当成变异名）。
COVERAGE_GAPS = [
    {
        "id": "gap-1-可执行位无可变异杀手",
        "title": "两个运维脚本的可执行位（mode 0111）在册、承重，但没有变异能改变它",
        "where": [
            "tests/deploy/trace_isolate_script_test.go  TestIsolateAndRestoreScriptsAreExecutable",
            "它断言 deploy/systemd/{isolate,restore}-old-trace-archive.sh 的 st.Mode().Perm()&0o111 != 0",
        ],
        "reachable": (
            "**可达**：模式位是 git 索引里的一等事实（`git archive HEAD` 会带上），用例在 "
            "pristine 树里读的是真实 mode ⇒ 不是平台不可达。"
        ),
        "no_killer": (
            "**没有变异杀手，且这是工具边界**：`trace_mutate.py` 的变异只有 `sub`/`cut` "
            "两种，作用对象是**文本**（`apply_mutation` 返回改过的字符串），无法修改 "
            "文件模式 ⇒「去掉 +x」这条回归在变异层不可表达。"
        ),
        "falsifiability": (
            "**可失败性由人工探针承载**（`chmod -x` 后重跑；探针不动内容 ⇒ 不需要还原内容，"
            "`chmod +x` 即可回到与提交逐字节一致的状态）。"
        ),
        "owner": (
            "本轮只登记、不加变异。若将来工具支持 mode 变异，这条应升格为断言红。"
        ),
    },
]

REQUIREMENTS = [
    {
        "plan": "脚本**默认 dry-run**：`--confirm` 之前不得有任何变更动作（误敲一次不能动生产）",
        "mutations": ["m01_默认不再dry-run"],
        "checks": [],
        "note": ("杀手 = TestIsolateScriptDefaultIsDryRun（它还断言 dry-run 分支 `exit 0`，"
                 "并断言闸门出现在 disable/mask **之前**）。"),
    },
    {
        "plan": "删除谓词审查：命中新树（或 /datalog 顶层）⇒ `exit 3` 直接 STOP，且审查在 mask 之前",
        "mutations": ["m02_命中新树不STOP"],
        "checks": ["删除谓词审查覆盖find-delete", "删除谓词审查覆盖rsync-delete"],
        "note": ("杀手 = TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot；"
                 "两条静态检查覆盖「审查了哪几种删除形态」（rm -rf / find -delete / "
                 "rsync --delete），行为面覆盖「命中就 STOP」。"),
    },
    {
        "plan": "变更面只有 `disable --now` + `mask`，且必须复核 is-active 与 list-timers，并打印回滚命令",
        "mutations": ["m03_mask改unmask", "m04_删掉回滚提示"],
        "checks": [],
        "note": ("杀手 = TestIsolateScriptStopsAndMasksTimer（同时反向断言脚本里没有 "
                 "`systemctl unmask` —— 那是回滚脚本的事）。"),
    },
    {
        "plan": "脚本必须 fail-fast（`set -euo pipefail`）",
        "mutations": [],
        "checks": ["隔离脚本必须fail-fast"],
        "note": "取值要求 ⇒ 静态检查（没有任何变异形态能让它变成断言红）。",
    },
]

# 另有一条在册要求**刻意不写进 REQUIREMENTS**：「两个运维脚本必须带可执行位」由
# TestIsolateAndRestoreScriptsAreExecutable 承载，但它的 `mutations` 与 `checks` 都会是
# 空的 ⇒ 工具会把该条判成 **GAP** 并让整轮失败（`print_mapping` 的 `if not muts and not
# checks`）。按 **E64②** 的裁定，它登记在上面的 `COVERAGE_GAPS` 里（工具读不到的字段），
# 不放进映射表。
