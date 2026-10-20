r"""计划 4 / 任务 8 的变异集合 C：A3 的**全流程脚本**（`deploy/verify/trace-a3/run.sh`）。

主题：A3 的每一步都由这个脚本承载（11 步），其中两件事**必须**由脚本自身保证：

1. **清理**：`trap … EXIT` 必须在**第一个破坏性步骤之前**注册 —— 中途失败也要卸载、
   删镜像；否则会留下挂载中的回环设备与占用中的 1 GiB 镜像，下一个跑 A3 的人直接踩雷。
2. **护栏**：`scratch-prepare`（其内部的「挂载后设备号复核」就是护栏）必须**带
   `--confirm`**，并且出现在「复核归档根 → 起实例 → 填盘」之前。

## 与计划文本的一致性

计划任务 8 步骤 6 的 `p4-task-8-runsh.py` 逐字给出三条变异与各自杀手（m01–m03），
本文件逐字采用。**追加**的 13 行静态检查只承载「某一步必须存在 / 某段代码不得存在」
这类**存在性**要求（E62：静态检查只承载存在/不存在类要求，绝不承载行为要求）——
`run.sh` 的 11 步在 darwin 上一步都跑不了（无回环设备 / 无 docker daemon / 无 `/proc`），
所以「每一步都在脚本里」这件事只能由**静态检查 + 文本契约用例**钉住，真实执行留待 A3 宿主。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 关于「护栏的位置」这条边界（照抄计划步骤 6 的注）

用例名是 `TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep`（原名
`…BeforeAnyDestructiveStep` 已被计划明确改名）：**它的守卫范围是「填盘与 cleanup 之前」**，
**不包含** `mount -o loop`。护栏按任务 8 步骤 3 的次序必须在**挂载之后**调用
（挂载前 `/srv/trace-a3` 只是宿主根 FS 上的普通目录 ⇒ 校验必然误拒，看到的也不是回环
ext4 的设备号），因此本集合的静态检查 `工具源里护栏必须在mount之后` 与
`tests/deploy` 的 `TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot`
共同钉住「mount → 护栏 → 建归档根」这条次序。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

**run.sh 的任何一次真实执行**（建盘、挂载、起实例、真 ENOSPC、闩锁/降级/恢复读数、
A7 撞锁）**不可能**由本集合证明：本集合只变异一份 shell 文本，而执行它需要
Linux + root + 回环设备 + docker daemon + 测试 PG/Redis。按全局约束第 55 行，A3 这类
「重活」只能走 `cmd/trace-verify` 与手工剧本、**不得**进任何 `p4-task-*.py` 的清单；
`bash -n` 只证明语法（它不执行任何一行）。因此运行期证据按「待 A3 宿主执行」登记在
`p4-task-8-a3-deferral-at-*.txt`，**不**标 `EXPECTED_GREEN`。
"""

NAME = "p4-task-8-runsh"
TARGET = "deploy/verify/trace-a3/run.sh"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_a3_compose_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
# （同一个测试文件里的 4 个 compose 用例归 p4-task-8-compose.py 的清单）
MANIFEST_DERIVATION = (
    "grep -hE '^func TestA3(RunScript|ScratchPrepare)' tests/deploy/trace_a3_compose_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 只读文件文本的 B 层用例：不执行 run.sh、不启 docker，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 删掉 `trap cleanup EXIT` ⇒ 中途失败不再清理（回环设备与 1 GiB 镜像留在盘上）。
#     杀手：TestA3RunScriptAlwaysCleansUp
MUTATIONS["m01_删掉trap清理"] = [
    ("sub", "trap cleanup EXIT\n", ""),
]

# m02 删掉护栏调用（`scratch-prepare` 整行）⇒ 设备护栏在 A3 流程里彻底消失：
#     没有人再复核「回环 FS ≠ / ≠ /datalog」，填盘直奔未知设备。
#     杀手：TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep
MUTATIONS["m02_删掉护栏调用"] = [
    ("sub",
     "run go run ./cmd/trace-verify scratch-prepare --image /srv/trace-a3.img "
     "--mount /srv/trace-a3 --size 1G --confirm\n",
     ""),
]

# m03 从 `scratch-prepare` 上摘掉 `--confirm` ⇒ 破坏性准备动作不再需要显式确认。
#     杀手：TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep（同一条用例的 `--confirm` 断言）
MUTATIONS["m03_摘掉confirm"] = [
    ("sub",
     "--image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G --confirm",
     "--image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G"),
]

CHECKS = [
    {
        "name": "run.sh必须开严格模式",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"set -euo pipefail",
        "expect": "present",
        "why": ("计划任务 8 步骤 4：中途失败必须**立刻**停下来交给 `trap` 清理（不许带错继续）。"
                "**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh不得递归删除",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"(?m)^(?!\s*(?://|#))(?=.*\brm\b)(?=.*(?:--recursive\b|(?<!-)-[a-zA-Z]*[rR][a-zA-Z]*\b)).*",
        "expect": "absent",
        "why": "删除面必须**有界**：镜像由 `scratch-cleanup` 的 `rm -f <具体文件>` 删、挂载点由 `rmdir` 删（非空即失败）。**不存在性**要求 ⇒ 静态检查；正则刻意**跳过注释行**（E68：注释里说明「为什么不用它」是合法的）。\n【E70① 补齐】与 `p4-task-8-guard.py` 同名条目同因：只钉 `-rf` 会漏掉 `rm -r`、`rm -v -r`、`rm -R`、`rm --recursive`。本轮选择**把正则补全**（语义判据优先，理由见 guard 集同名条目），两个集合的删除面判据保持同一形态。",
    },
    {
        "name": "run.sh必须打印每步命令",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"run\(\) \{",
        "expect": "present",
        "why": ("A3 的证据是「命令 + 真实输出」的逐条抄录 ⇒ 每一步都必须经 `run()` 打印。"
                "**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须给出停止点",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"⛔ 停止点",
        "expect": "present",
        "why": ("计划任务 8 步骤 4 反复强调「只能在测试环境跑」⇒ 脚本必须在关键节点显式停手"
                "让操作者确认环境。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须复核设备号",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"stat -c '%d %n'",
        "expect": "present",
        "why": ("步骤 2 的复核：归档根所在设备号必须与 `/`、`/datalog` **两两不同**"
                "（这是护栏之外的第二道人工可见证据）。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须含归档根复核",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"findmnt -T /srv/trace-a3/trace-archive-v4",
        "expect": "present",
        "why": ("步骤 2 计划原文：复核归档根确实落在回环 FS 上（TARGET 必须是 `/srv/trace-a3`）。"
                "**存在性**要求 ⇒ 静态检查（次序由两个用例的 `indexOf` 断言承载）。"),
    },
    {
        "name": "run.sh必须起隔离实例并等健康",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"up -d --wait",
        "expect": "present",
        "why": ("步骤 3 计划原文：起隔离实例（只跑 gateway）。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须真正填盘",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r'fallocate -l "\$FILL_SIZE" "\$FILL"',
        "expect": "present",
        "why": ("步骤 5 计划原文：先 `fallocate -l 980M` 把 1 GiB 的文件系统占到只剩几十 MB，"
                "再靠写入吃掉最后的空间 —— 这是「真实 ENOSPC」的承重步骤。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须观察闩锁",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"\[trace-body\] ERROR",
        "expect": "present",
        "why": ("步骤 6 计划原文：`docker logs … | grep '[trace-body] ERROR'`（期望**只报一次**）。"
                "**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须观察降级",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"body_missing IS TRUE",
        "expect": "present",
        "why": ("步骤 7 计划原文：`SELECT count(*) … WHERE body_missing = true AND body_ref_part IS NULL`"
                " 与总数对比。**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须观察恢复",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"正文落盘已恢复",
        "expect": "present",
        "why": ("步骤 9 计划原文：删掉 `fill.bin`、等 1–2 个 tick、看闩锁日志出现「已恢复」"
                "（`internal/gateway/observability/trace_event.go:356` 的真实措辞）。"
                "**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "run.sh必须做A7撞锁",
        "file": "deploy/verify/trace-a3/run.sh",
        "pattern": r"已有另一个 sealer 持有锁",
        "expect": "present",
        "why": ("步骤 10 计划原文：第二个 `--once` 必须以退出码 1 失败且 stderr 含该消息"
                "（措辞取自 `internal/tracesealer/lock.go:11` 的 `ErrAnotherSealerRunning`）。"
                "**存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "工具源里护栏必须在mount之后",
        "file": "cmd/trace-verify/main.go",
        "pattern": r'(?s)"mount", "-o", "loop".*?traceverify\.GuardScratchTarget\(',
        "expect": "present",
        "why": ("任务 8 步骤 3 的次序（并见步骤 6 的注）：护栏必须在 `mount -o loop` **成功之后**"
                "调用（挂载前校验必然误拒）。**次序存在性**要求 ⇒ 静态检查；同一批语义由 "
                "`TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot` 承载。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m03 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "`trap` 清理必须在**第一个破坏性步骤之前**注册（中途失败也要卸载/删镜像）",
        "mutations": ["m01_删掉trap清理"],
        "checks": [],
        "note": "杀手 = TestA3RunScriptAlwaysCleansUp（trap 位置 < `scratch-prepare` 位置；并钉住 cleanup 的两条收尾与两条复核）。",
    },
    {
        "plan": "护栏调用必须在「复核归档根 → 起实例 → 填盘」之前（且不得被删掉）",
        "mutations": ["m02_删掉护栏调用"],
        "checks": [],
        "note": ("杀手 = TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep（护栏行必须存在 + 四条 `indexOf` 次序）；"
                 "**连带**：同一条变异也打红 TestA3RunScriptAlwaysCleansUp —— 该用例以 `scratch-prepare` 行的位置作为"
                 "「第一个破坏性步骤」的锚点，锚点被删就无从比较先后 ⇒ 如实登记，不隐瞒连带（表里 m02 的 FAIL=2）。"),
    },
    {
        "plan": "护栏调用必须带 `--confirm`（破坏性准备动作要显式确认）",
        "mutations": ["m03_摘掉confirm"],
        "checks": [],
        "note": "杀手 = 同一条用例的 `Contains(…--confirm)` 断言。",
    },
    {
        "plan": "工具源：护栏必须在 `mount -o loop` **之后**调用（挂载前校验必然误拒）",
        "mutations": [],
        "checks": ["工具源里护栏必须在mount之后"],
        "note": ("**次序存在性**要求 ⇒ 静态检查；行为/次序由在册用例 "
                 "`TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot` 承载。"),
    },
    {
        "plan": "严格模式（`set -euo pipefail`）＋ 每一步都打印真实命令（`run()`）",
        "mutations": [],
        "checks": ["run.sh必须开严格模式", "run.sh必须打印每步命令"],
        "note": "**存在性**要求 ⇒ 静态检查。",
    },
    {
        "plan": "关键节点必须有「停止点」，由操作者确认只在测试环境跑",
        "mutations": [],
        "checks": ["run.sh必须给出停止点"],
        "note": "**存在性**要求 ⇒ 静态检查。",
    },
    {
        "plan": "删除面不得递归（镜像 `rm -f`、挂载点 `rmdir`）",
        "mutations": [],
        "checks": ["run.sh不得递归删除"],
        "note": "**不存在性**要求 ⇒ 静态检查（`rm -rf` 在 run.sh 里不得出现）。",
    },
    {
        "plan": "步骤 2：复核归档根落在回环 FS 上（`findmnt` + 设备号两两不同）",
        "mutations": [],
        "checks": ["run.sh必须含归档根复核", "run.sh必须复核设备号"],
        "note": "**存在性**要求 ⇒ 静态检查；次序由两个用例承载。",
    },
    {
        "plan": "步骤 3：起隔离实例并等健康（`up -d --wait`）",
        "mutations": [],
        "checks": ["run.sh必须起隔离实例并等健康"],
        "note": "**存在性**要求 ⇒ 静态检查；被起的编排本身由 `p4-task-8-compose.py` 钉住。",
    },
    {
        "plan": "步骤 5：先 `fallocate` 占盘再靠写入吃满（真实 ENOSPC）",
        "mutations": [],
        "checks": ["run.sh必须真正填盘"],
        "note": "**存在性**要求 ⇒ 静态检查。",
    },
    {
        "plan": "步骤 6/7/9：观察闩锁（恰好一条 ERROR）、降级（`body_missing`）、恢复（「已恢复」）",
        "mutations": [],
        "checks": ["run.sh必须观察闩锁", "run.sh必须观察降级", "run.sh必须观察恢复"],
        "note": "**存在性**要求 ⇒ 静态检查（读数本身属 A3 宿主的运行期证据，见 COVERAGE_GAPS）。",
    },
    {
        "plan": "步骤 10：A7 进程级撞锁（第二个 `--once` 退出码 1 + 撞锁消息）",
        "mutations": [],
        "checks": ["run.sh必须做A7撞锁"],
        "note": "**存在性**要求 ⇒ 静态检查。",
    },
]
