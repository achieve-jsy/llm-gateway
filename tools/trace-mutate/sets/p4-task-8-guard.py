r"""计划 4 / 任务 8 的变异集合 A：A3 的**目标护栏**（`internal/traceverify/guard.go`）。

主题：A3 要「真的把盘写满」，而这条路线上唯一的安全边界就是
`GuardScratchTarget` —— 它必须拒绝「落在文件系统根」与「与 `/` 或 `/datalog`
**同一个设备**」的目标，并要求显式确认。四种走样每一种都必须让**点名用例**转红。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 8 步骤 6 的 `p4-task-8-guard.py` 逐字给出四条变异与各自杀手（m01–m04），
本文件逐字采用。**追加**的 5 行静态检查只承载「某段代码必须存在 / 不得存在」这类
**存在性**要求（E62：静态检查只承载存在/不存在类要求，绝不承载行为要求），
它们与 Go 用例是同一批语义的两条独立读法；**可失败性**由交付报告的
「改坏→转红→还原」探针逐条实测（`p4-task-8-probe-at-*.txt`）。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

1. **cmd/trace-verify 的线序**（容量预检 → 建镜像 → mkfs → 建挂载点 → `mount -o loop`
   → 护栏 → 建归档根 → `df`/`findmnt`）：它**不可能**由本集合的变异承载
   （`TARGET` 只有 `guard.go`，工具一次只能变异一个文件）。它的证据是
   ① `tests/deploy` 里对 **工具源文本**的次序用例
   `TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot`（在册，运行于
   `p4-task-8-runsh.py` 的清单里）② `run.sh` 的顺序契约用例 ③ **A3 宿主上的手工执行**。
   ③ 属「重活」：全局约束第 55 行明确要求 A3 真实盘满只能走 `cmd/trace-verify` 与手工
   剧本、**不得**进任何 `p4-task-*.py` 的清单；在 darwin 上它连一步都跑不了
   （无回环设备/无 docker daemon/无 `/proc`）⇒ 这一半按「待 A3 宿主执行」登记，
   证据落在 `p4-task-8-a3-deferral-at-*.txt`，**不**标 `EXPECTED_GREEN`。
2. **A6 的解码内存标定**（`decode-bench`）：属任务 9；本集合不声明它。
"""

NAME = "p4-task-8-guard"
TARGET = "internal/traceverify/guard.go"
PACKAGES = ["./tests/traceverify/"]
MANIFEST_FILES = ["tests/traceverify/guard_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/traceverify/guard_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部注入驱动：不依赖真实磁盘 / 设备号 / root / procfs，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 去掉「目标是 `/`」的判断 ⇒ `/` 可以被当成填盘目标（2026-09-24 的同一类事故）。
#     杀手：TestGuardRejectsFilesystemRoot
#     （`&& false` 形态：保住 `filepath.Clean` 的引用 ⇒ 是断言红，不是编译红）
MUTATIONS["m01_去掉根目录判断"] = [
    ("sub", 'if filepath.Clean(target) == "/" {', 'if filepath.Clean(target) == "/" && false {'),
]

# m02 去掉设备号比较 ⇒ 与 `/` 或 `/datalog` 同设备也放行 ⇒ A3 会填满 `/` 或生产盘。
#     杀手：TestGuardRejectsSameDeviceAsForbidden
#     （本条同时打红 TestGuardRejectsTargetOnSameDeviceAsForbiddenSelf —— 若 forbidden
#      里包含目标自己，唯一的拒绝理由也是这条比较；两条都是断言红。）
MUTATIONS["m02_去掉设备号比较"] = [
    ("sub", "if dev == fd {", "if dev == fd && false {"),
]

# m03 去掉 confirm 检查 ⇒ 破坏性动作可以**没有显式确认**就开始（连准备动作都不该做）。
#     杀手：TestGuardRequiresExplicitConfirm
MUTATIONS["m03_去掉confirm检查"] = [
    ("sub", "if !confirm {", "if !confirm && false {"),
]

# m04 设备号比较用 `!=` 反向 ⇒ **专用**回环 ext4（与 / 和 /datalog 都不同设备）反被拒绝
#     ⇒ A3 一步都跑不了（唯一合法的目标被护栏自己挡掉）。
#     杀手：TestGuardAcceptsDedicatedFilesystem
MUTATIONS["m04_设备比较反向"] = [
    ("sub", "if dev == fd {", "if dev != fd {"),
]

CHECKS = [
    {
        "name": "护栏不得跳过读不到设备号的路径",
        "file": "internal/traceverify/guard.go",
        "pattern": r"的设备号读不到",
        "expect": "present",
        "why": ("计划任务 8 步骤 3 的保守拒绝口径：拿不到设备号就**无法证明**目标与 `/`、"
                "`/datalog` 不同设备 ⇒ 必须拒绝（「读不到」必须红，不许放行、不许跳过）。"
                "**存在性**要求 ⇒ 静态检查；同一批语义的行为由在册用例 "
                "`TestGuardRejectsWhenDeviceCannotBeRead`（三条路径：目标读不到 / forbidden "
                "读不到 / 回调为 nil）承载。"),
    },
    {
        "name": "容量预检必须解析尺寸后缀",
        "file": "internal/traceverify/guard.go",
        "pattern": r"func ParseSize",
        "expect": "present",
        "why": ("计划任务 8 步骤 3 第 1 步「容量预检（statfs 断言可用空间 ≥ size + 1 GiB 余量；"
                "见 R2）」的前置：`--size 1G` 必须被解析成 bytes。**存在性**要求 ⇒ 静态检查"
                "（行为由 `TestParseSizeAcceptsBinarySuffixes` / `TestParseSizeRejectsMalformed` 承载）。"),
    },
    {
        "name": "容量判据必须含1GiB余量",
        "file": "internal/traceverify/guard.go",
        "pattern": r"ScratchCapacityMarginBytes int64 = 1 << 30",
        "expect": "present",
        "why": ("风险登记 R2：护栏只护**挂载点**，镜像文件（约 1 GiB）落在 `/srv` 所在文件系统上、"
                "不受设备护栏保护 ⇒ 容量预检是它唯一的前置防线，余量口径必须是 **1 GiB**。"
                "**存在性**要求 ⇒ 静态检查（边界两侧由 `TestScratchCapacityMarginIncludesReserve` 承载）。"),
    },
    {
        "name": "trace-verify必须调用护栏",
        "file": "cmd/trace-verify/main.go",
        "pattern": r"traceverify\.GuardScratchTarget",
        "expect": "present",
        "why": ("计划任务 8 步骤 3：`scratch-prepare` 必须在 `mount -o loop` **成功之后**调 "
                "`GuardScratchTarget` 复核设备号。没有这次调用，A3 的安全边界就只是文档。"
                "**存在性**要求 ⇒ 静态检查；**次序**（在 mount 之后、建归档根之前）另由 "
                "`TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot` 与静态检查 "
                "`工具源里护栏必须在mount之后`（`p4-task-8-runsh.py`）钉住。"),
    },
    {
        "name": "trace-verify破坏性删除面不得递归",
        "file": "cmd/trace-verify/main.go",
        "pattern": r"(?m)^(?!\s*(?://|#)).*\brm\s+-[a-zA-Z]*r[a-zA-Z]*f\b",
        "expect": "absent",
        "why": ("计划任务 8 步骤 3：`scratch-cleanup` 是唯一允许删镜像/目录的入口，删除面必须**有界**"
                "（只删命令行给出的那一个镜像文件 + `rmdir` 空挂载点 —— `rmdir` 对非空目录会失败）。"
                "**不存在性**要求 ⇒ 静态检查。正则刻意**跳过注释行**：源码注释里说明「刻意不用它」"
                "是合法且有价值的（E68：文本计数不是门禁 —— 第一版写法 `rm -rf` 正是踩了这个坑，"
                "只命中了两行注释，见本轮 `fix(trace-mutate)` 提交）。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m04 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "护栏：目标是文件系统根（`/`）⇒ 拒（2026-09-24 的事故形态）",
        "mutations": ["m01_去掉根目录判断"],
        "checks": [],
        "note": "杀手 = TestGuardRejectsFilesystemRoot（该用例的 forbidden 不含 `/`，设备号刻意取成不同值 ⇒ 唯一能拒它的就是这条判据）。",
    },
    {
        "plan": "护栏：目标与 forbidden 之一同一个设备 ⇒ 拒（含 forbidden 里包含目标自己的情形）",
        "mutations": ["m02_去掉设备号比较"],
        "checks": [],
        "note": ("杀手 = TestGuardRejectsSameDeviceAsForbidden；同时打红 "
                 "TestGuardRejectsTargetOnSameDeviceAsForbiddenSelf（两条都是断言红 —— 如实登记，不隐瞒连带）。"),
    },
    {
        "plan": "护栏：没有 confirm ⇒ 拒（连准备动作都不许做）",
        "mutations": ["m03_去掉confirm检查"],
        "checks": [],
        "note": ("杀手 = TestGuardRequiresExplicitConfirm（两条断言：必须报错 + **stat 一次都不许调用**）。"),
    },
    {
        "plan": "护栏：专用文件系统（与全部 forbidden 不同设备）＋ confirm ⇒ 通过",
        "mutations": ["m04_设备比较反向"],
        "checks": [],
        "note": "杀手 = TestGuardAcceptsDedicatedFilesystem（反向比较会把唯一合法的回环 ext4 也拒掉）。",
    },
    {
        "plan": "护栏：设备号读不到（stat 失败 / 回调为 nil）⇒ 保守拒绝（读不到必须红，不许放行）",
        "mutations": [],
        "checks": ["护栏不得跳过读不到设备号的路径"],
        "note": ("**存在性**要求 ⇒ 静态检查；行为由在册用例 `TestGuardRejectsWhenDeviceCannotBeRead` "
                 "（三条路径）承载，可失败性由探针实测。"),
    },
    {
        "plan": "容量预检：`--size` 支持 K/M/G/T（1024 进制），非法 / 溢出必须报错",
        "mutations": [],
        "checks": ["容量预检必须解析尺寸后缀"],
        "note": ("**存在性**要求 ⇒ 静态检查；行为由在册用例 `TestParseSizeAcceptsBinarySuffixes` 与 "
                 "`TestParseSizeRejectsMalformed`（10 个非法输入）承载。"),
    },
    {
        "plan": "容量预检：可用空间 ≥ 镜像 + **1 GiB 余量**（R2）",
        "mutations": [],
        "checks": ["容量判据必须含1GiB余量"],
        "note": ("**存在性**要求 ⇒ 静态检查；边界两侧由在册用例 "
                 "`TestScratchCapacityMarginIncludesReserve` 承载。"),
    },
    {
        "plan": "`cmd/trace-verify scratch-prepare` 必须真正调用护栏（否则 A3 一步都跑不了）",
        "mutations": [],
        "checks": ["trace-verify必须调用护栏"],
        "note": ("**存在性**要求 ⇒ 静态检查；次序要求见 `p4-task-8-runsh.py` 的两条静态检查与"
                 "`TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot`。"),
    },
    {
        "plan": "`scratch-cleanup` 是唯一允许删镜像/目录的入口，删除面有界（`rm -f` 具体镜像 + `rmdir`）",
        "mutations": [],
        "checks": ["trace-verify破坏性删除面不得递归"],
        "note": "**不存在性**要求 ⇒ 静态检查（**非注释行**里不得出现递归删除的调用形态；注释里的说明文字不算）。",
    },
]
