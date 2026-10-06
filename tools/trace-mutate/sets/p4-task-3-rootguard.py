r"""计划 4 / 任务 3 的变异集合 A：根目录身份护栏（`internal/tracesealer/rootguard.go`）。

被变异文件 `internal/tracesealer/rootguard.go`。主题：**「撤掉挂载必须被看见」**——
umount 之后挂载点目录仍然存在（退化成宿主根 FS 上的普通目录），`os.Stat` 照旧成功，
因此只有「身份基线 + 每轮复检」能发现它；而**任何形式的「补一个根目录」都会把消失的
挂载重建到宿主根 FS 上**（2026-09-24 事故的同一类）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：三处**加强**、一处**订正**，零处削弱）

计划任务 3 步骤 6 的 A 集合给了 m01–m06 与两条静态检查。本轮在此之上：

1. **新增 m07**（`Check` 的 stat 失败被静默放行）。计划的两条「运行中根目录消失」用例
   （`TestRootGuardCheckReportsVanishedRoot`）在计划给的 6 条变异里**没有任何杀手**：
   m03 只动身份比对，m04 只动 `covers`、不触 `Check` 的 stat 失败分支，m05/m06 走的是
   构造期。⇒ 加一条直取该分支的变异，让那条用例的断言有在册杀手（E63⑤）。
2. **新增 m08**（`Baseline()` 返回 `root` 而不是身份基线）。计划把
   `TestRootGuardBaselineIsNonEmpty` 登记为 m04 的杀手，但该用例**注入自己的 identity**，
   根本不经过 `covers`/mountinfo ⇒ 它在 m04 下仍绿。⇒ 把「基线必须可抄」这条**取值**
   要求改由一条直取该返回值的变异承载（E63⑤）。
3. **新增一条用例** `TestRootGuardRejectsWhenBaselineCannotBeTaken`（在
   `tests/tracesealer/rootguard_test.go`）。计划把 m05 的杀手写成
   `TestRootGuardBaselineIsNonEmpty`，但那条用例注入的 identity **成功**返回 ⇒ 走不到
   m05 动的那个错误分支。⇒ 补一条「取不到基线必须响亮失败」的行为用例（E63④）。
4. **m04 由包内行为用例杀死**（**E64① 订正**；本轮之前它被登记为「允许的绿」，那是错的）。
   原先登记的理由有两半，**两半都已被独立评审证伪**：
   ① 「本机是 darwin、没有 `/proc/self/mountinfo` ⇒ `covers` 在任何在册用例里都不可达」
   —— `covers` 是**纯字符串函数**，它根本不读 mountinfo；而计划 Q3 明确允许「确需触达
   未导出符号 ⇒ 放包内测试」⇒ 它**不是**平台不可达。
   ② 「在 Linux 上会杀掉依赖真实 Identity 的用例」—— 在册清单里唯一传真实 Identity 的
   三条用例（`tests/tracesealer/rootguard_test.go` 的 `/`、相对路径、缺失目录）**都在调用
   Identity 之前就返回错误** ⇒ 在 Linux 上 m04 同样不会红，该子句**无据**。
   计划点名 m04 的两个杀手（`TestRootGuardCheckDetectsIdentityChange`、
   `TestRootGuardBaselineIsNonEmpty`）**都注入 identity 闭包**、不经过 `covers` ——
   这一半是对的，但它只说明「那两个用例不是杀手」，推不出「没有杀手」。
   ⇒ 按 **E64①**（不得用「本平台不可达」为在册清单从未触达的路径开脱；确属平台限制
   必须给出**在册**证据，否则一律按可杀处理并补测试）补一条**包内行为用例**
   `internal/tracesealer/covers_test.go:TestCoversRootMountCoversPathsUnderRoot`
   （断言 `covers("/", "/tmp/x")` 为真，另有根/前缀边界），m04 因此变成**断言红**，
   本集合 `EXPECTED_GREEN` **清空**（不再有任何允许的绿）。

## 变异测不到、但有静态检查行（CHECKS）

- **根目录护栏侧零建目录调用**：`\bMkdir[A-Za-z]*\s*\(` 在 `rootguard.go` 里 **absent**。
  承重证据是行为用例 `TestRootGuardNeverCreatesRoot` + 变异 m06（**唯一允许的「建目录」
  变异**：把它注入 pristine 的临时副本，证明那条断言真的能看见违规）。本行扫**整个文件
  文本（含注释）**：注释里也不得出现该形态。
- **`covers` 必须特判根挂载点**：`(?m)^\tif mp == "/" \{$` **present**。计划要求的
  「根挂载点必须覆盖其下路径」是**行为**要求 ⇒ 它的承重证据是包内行为用例
  `TestCoversRootMountCoversPathsUnderRoot`（见上 4 与 E64①），m04 是它的断言红杀手；
  本行静态检查**另外**钉住该分支的**存在性**（整段特判被删掉即红，与 m04 只改条件
  的形态互补）。
- **`Options.Identity` 可注入**：`internal/tracesealer/options.go` 里
  `Identity func\(string\) \(string, error\)` **present**。护栏的可测性完全建立在这个注入点
  上（真实身份是「这台机器当前挂载表」的函数，测试无法复现 umount）⇒ 它是可测性的前提。
"""

NAME = "p4-task-3-rootguard"
TARGET = "internal/tracesealer/rootguard.go"
# 两个包：B 层（仅公开 API）+ 包内（`covers` 是未导出函数，见 E64① 与文件头 4）。
# 包内那个目录进 PACKAGES 的第二个作用：它的 `*_test.go` 会进权威表的
# 「测试文件 sha256」段 ⇒ 那条用例属于**证据绑定**的一部分，第三方可复核。
PACKAGES = ["./tests/tracesealer/", "./internal/tracesealer/"]
MANIFEST_FILES = [
    "tests/tracesealer/rootguard_test.go",
    "internal/tracesealer/covers_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/rootguard_test.go "
    "internal/tracesealer/covers_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动：不依赖 zstd / 真实磁盘 / 数据库，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 允许 `/` 做根目录 ⇒ 2026-09-24 那一类事故（把宿主根写满）被放回来
#     杀手：TestRootGuardRejectsFilesystemRoot
MUTATIONS["m01_允许文件系统根"] = [
    ("sub", 'if filepath.Clean(root) == "/" {', "if false {"),
]
# m02 不校验绝对路径 ⇒ systemd 的 WorkingDirectory 决定相对路径落到哪，正文写到没人想到的地方
#     杀手：TestRootGuardRejectsRelativePath
MUTATIONS["m02_允许相对路径"] = [
    ("sub", "if !filepath.IsAbs(root) {", "if false {"),
]
# m03 每轮复检不再比对身份 ⇒ umount 之后 os.Stat 照旧成功，「撤掉挂载」永远不被发现
#     杀手：TestRootGuardCheckDetectsIdentityChange
MUTATIONS["m03_不比对身份"] = [
    ("sub", "if id != g.baseline {", "if false && id != g.baseline {"),
]
# m04 covers 去掉根挂载点特判 ⇒ 根挂载点覆盖不到任何路径（最危险的情形反而取不到身份）
#     杀手：TestCoversRootMountCoversPathsUnderRoot（包内用例 internal/tracesealer/covers_test.go；
#     E64① 订正：原登记「本平台不可达 ⇒ 允许的绿」已证伪 —— covers 是纯字符串函数）
MUTATIONS["m04_去掉根挂载点特判"] = [
    ("sub", 'if mp == "/" {', "if false {"),
]
# m05 取不到启动基线也照样构造 ⇒ 空基线与任何身份都不等 ⇒ 之后每轮都报「身份不符」，
#     把「取基线失败」伪装成「挂载换了」，运维被指向错误的方向
#     杀手：TestRootGuardRejectsWhenBaselineCannotBeTaken
MUTATIONS["m05_取不到基线也放行"] = [
    ("sub", 'return nil, fmt.Errorf("%w: 取启动基线失败: %w", ErrRootIdentityMismatch, err)',
            "return &RootGuard{root: root, identity: identity}, nil"),
]
# m06 【唯一允许的「建目录」变异】在 stat 失败分支前补一个 MkdirAll ⇒ 消失的挂载点被
#     重建到宿主根 FS 上，正文写进那片没人会去看的空间
#     杀手：TestRootGuardNeverCreatesRoot（它断言缺失的根目录**事后仍然不存在**）
MUTATIONS["m06_用建目录修复缺失根"] = [
    ("sub", "\tinfo, err := os.Stat(root)\n\tif err != nil {",
            "\tinfo, err := os.Stat(root)\n\t_ = os.MkdirAll(root, 0o755)\n\tif err != nil {"),
]
# m07 【本轮新增】Check 的 stat 失败被静默放行 ⇒ 「运行中根目录整个消失」读成「本轮没问题」
#     杀手：TestRootGuardCheckReportsVanishedRoot
MUTATIONS["m07_根目录消失静默放行"] = [
    ("sub", "return fmt.Errorf(\"%w: %w\", ErrRootUnavailable, err)", "return nil"),
]
# m08 【本轮新增】Baseline 返回 root 而不是身份基线 ⇒ 排障时抄下来的「启动基线」永远是
#     根路径，与「现在」比对不出任何东西（日志/看板据此判断换盘还是 umount）
#     杀手：TestRootGuardBaselineIsNonEmpty
MUTATIONS["m08_基线返回root"] = [
    ("sub", "func (g *RootGuard) Baseline() string { return g.baseline }",
            "func (g *RootGuard) Baseline() string { return g.root }"),
]

CHECKS = [
    {
        "name": "rootguard不得建目录",
        "file": "internal/tracesealer/rootguard.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("D70 / 2026-09-24 事故同类：替运维「补一个」根目录 = 把已经消失的挂载点在"
                "**宿主根文件系统**上重建，正文随后写进那片没人会去看的空间。这是"
                "**不存在性**要求 ⇒ 只能静态检查；承重证据是行为用例 "
                "TestRootGuardNeverCreatesRoot 与变异 m06（唯一允许的建目录变异，用来证明"
                "那条断言真的能看见违规）。本行扫整个文件文本（含注释）。"),
    },
    {
        "name": "covers必须特判根挂载点",
        "file": "internal/tracesealer/rootguard.go",
        "pattern": r'(?m)^\tif mp == "/" \{$',
        "expect": "present",
        "why": ("根挂载点是唯一的以 \"/\" 结尾的挂载点；不特判就会走 "
                "strings.HasPrefix(p, \"//\") 这条恒假分支 ⇒ 「路径其实就在根文件系统上」"
                "这种最危险的情形反而取不到身份。**它的承重证据是包内行为用例 "
                "internal/tracesealer/covers_test.go 的 TestCoversRootMountCoversPathsUnderRoot**"
                "（`covers` 是未导出函数 ⇒ 按 Q3 放包内；**E64① 订正**：原先「darwin 不可达 ⇒ "
                "只能登记为允许的绿」是错判 —— `covers` 是纯字符串函数，与 "
                "/proc/self/mountinfo 无关），变异 m04 是它的**断言红**杀手；本行静态检查"
                "**另外**钉住该分支的**存在性**（整段特判被删掉即红，与 m04 只改条件的形态互补）。"),
    },
    {
        "name": "Options.Identity可注入",
        "file": "internal/tracesealer/options.go",
        "pattern": r"Identity\s+func\(string\) \(string, error\)",
        "expect": "present",
        "why": ("护栏的可测性完全建立在这个注入点上：真实身份是「这台机器当前的挂载表」的"
                "函数，测试无法为同一路径复现「撤掉挂载」（umount 要 root，且会把整台机器"
                "卷进来）。删掉它 ⇒ 上表所有「运行中身份变化」用例都造不出来。接线要求 ⇒ "
                "静态检查。"),
    },
    {
        "name": "covers_test.go不得建目录",
        "file": "internal/tracesealer/covers_test.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E67** 补齐）：`covers_test.go` 是本计划按 "
                "**E64①** 新增到 `internal/tracesealer/` 的**包内测试文件**（`E64①` 那条"
                "裁定的产物），它是该目录下的第 9 个 `.go` 文件、此前没有任何一行 D70 静态"
                "检查覆盖它 —— 而 `go test` 会编译并执行它，包内测试同样能「用建目录修复"
                "消失的挂载点」。`E66③` 声称覆盖两个目录的每一个源文件 ⇒ **E67** 把测试"
                "文件也纳入，使「每一个 `.go` 文件一行」字面成立。**不存在性**要求 ⇒ 静态"
                "检查；可失败性由 **E67 的临时仓库探针**实测（在 `covers_test.go` 里插入 "
                "`os.MkdirAll(root, 0o755)` 并提交 ⇒ 本行 FAIL、退出 1）。"),
    },
]

# 本集合**没有任何**允许的绿：原先唯一一条（m04「去掉根挂载点特判」）的理由是
# 「本机 darwin 不可达（无 /proc/self/mountinfo）⇒ covers 在任何在册用例里都不可达」，
# 本轮按 **E64①** 证伪并订正 —— `covers` 是纯字符串函数、与 mountinfo 无关，而计划 Q3
# 允许「确需触达未导出符号 ⇒ 放包内测试」⇒ 包内用例
# TestCoversRootMountCoversPathsUnderRoot 直调它，m04 是**断言红**（实测：注入 m04 后该
# 用例在 require.True(covers("/", "/tmp/x")) 处 FAIL）。原登记的另一个子句「在 Linux 上
# 会杀掉依赖真实 Identity 的用例」同样无据：在册清单里三条传真实 Identity 的用例都在
# 调用 Identity 之前就返回错误。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "根目录是文件系统根 `/` 必须在构造期被拒（2026-09-24 事故同类）",
        "mutations": ["m01_允许文件系统根"],
        "checks": [],
        "note": "杀手 = TestRootGuardRejectsFilesystemRoot。",
    },
    {
        "plan": "根目录必须是绝对路径（systemd 的 WorkingDirectory 不等于你以为的那个）",
        "mutations": ["m02_允许相对路径"],
        "checks": [],
        "note": "杀手 = TestRootGuardRejectsRelativePath。",
    },
    {
        "plan": "根目录不存在必须响亮失败，且**绝不得**把它创建出来",
        "mutations": ["m06_用建目录修复缺失根"],
        "checks": ["rootguard不得建目录"],
        "note": ("杀手 = TestRootGuardNeverCreatesRoot（断言缺失的根目录事后仍然不存在）。"
                 "「本文件没有建目录调用」是不存在性要求 ⇒ 另有静态检查行。"),
    },
    {
        "plan": "取不到启动基线必须响亮失败（不得拿空基线继续，把「取基线失败」伪装成「挂载换了」）",
        "mutations": ["m05_取不到基线也放行"],
        "checks": [],
        "note": "杀手 = TestRootGuardRejectsWhenBaselineCannotBeTaken（本轮按 E63④ 新增）。",
    },
    {
        "plan": "每轮复检：目录还在但背后的挂载换了（umount）必须报身份不符",
        "mutations": ["m03_不比对身份"],
        "checks": [],
        "note": "杀手 = TestRootGuardCheckDetectsIdentityChange。",
    },
    {
        "plan": "每轮复检：根目录整个消失必须报「不可用」（与「身份不符」分成两个哨兵）",
        "mutations": ["m07_根目录消失静默放行"],
        "checks": [],
        "note": "杀手 = TestRootGuardCheckReportsVanishedRoot；m07 是本轮按 E63⑤ 新增的变异。",
    },
    {
        "plan": "启动基线必须可抄进日志（排障时对比「启动基线 vs 现在」）",
        "mutations": ["m08_基线返回root"],
        "checks": [],
        "note": "杀手 = TestRootGuardBaselineIsNonEmpty；m08 是本轮按 E63⑤ 新增的变异。",
    },
    {
        "plan": ("Identity 取最长前缀覆盖 path 的挂载点，返回 \"<挂载点>|<设备号>|<文件系统类型>\"；"
                 "根挂载点 \"/\" 也必须覆盖其下路径"),
        "mutations": ["m04_去掉根挂载点特判"],
        "checks": ["covers必须特判根挂载点"],
        "note": ("杀手 = TestCoversRootMountCoversPathsUnderRoot（**包内**用例 "
                 "internal/tracesealer/covers_test.go，断言 covers(\"/\", \"/tmp/x\") 为真 + "
                 "根/前缀边界；E64① 订正：`covers` 是纯字符串函数，可按计划 Q3 在包内直调 "
                 "⇒ 不存在「平台不可达」，m04 为**断言红**）。静态检查 "
                 "covers必须特判根挂载点 另钉住特判分支的**存在性**。"),
    },
    {
        "plan": "身份探针必须可注入（Options.Identity），否则「运行中挂载被撤掉」造不出来",
        "mutations": [],
        "checks": ["Options.Identity可注入"],
        "note": "接线/可测性前提 ⇒ 静态检查（它的运行时效果由上表各「运行中」用例承载）。",
    },
    {
        "plan": "计划 4 全局约束第 27 条（D70 整包零建目录）：internal/tracesealer/covers_test.go",
        "mutations": [],
        "checks": ["covers_test.go不得建目录"],
        "note": ("**E67** 补齐：`E66③` 的「覆盖每一个源文件」名不副实；`covers_test.go` 是 "
                 "**E64①** 新增到该目录的包内测试文件，同样被 `go test` 编译执行、同样能"
                 "「用建目录修复消失的挂载点」，故一并纳入逐文件覆盖。不存在性要求 ⇒ 静态"
                 "检查；可失败性由 E67 的临时仓库探针实测。"),
    },
]
