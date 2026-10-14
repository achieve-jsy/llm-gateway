r"""计划 4 / 任务 10 的变异集合 C：A8/A6 的**观测面**（`internal/tracesealer/runner.go` 的 `observeSealedPart`）。

主题：`observeSealedPart` 是 A8 行数分布与 A6 压缩比的**持续观测面**（每个成功封出的 part 一行 INFO、
每轮一行汇总）。它有三个必须钉住的行为面：

  * **预警线**（m01：50% 阈值 → 100%）⇒ 余量已经不足 2 倍而**没有任何提示**；越界后才发现的代价是
    该块永远不会被封出、spool 无限增长；
  * **更新方向**（m02：`MinBytesPerRow` 的 `<` 改成 `>`）⇒ 「每行最少多少字节」变成最大值，容量账按
    **偏乐观**的一侧算；
  * **分母**（m03：`rows` → `rows+1`）⇒ 每个块的 B/行读数都偏小，与压缩比口径不再自洽；
  * **失败语义**（m04：读不出 footer 时返回 `rows = 1` 而不是跳过）⇒ 把一次**读取失败**伪装成一个
    **读数** —— A8 的分布与 A6 的压缩比从此不可信，而且没有任何东西报警；
  * **非法分母**（m05：守卫里摘掉 `rows <= 0`）⇒ 0 行的块被当成样本，凭空造出一个压缩比读数。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：两处**新增**，零处削弱）

计划任务 10 步骤 5 的 `p4-task-10-observability.py` 给出 m01–m03 与各自杀手，本文件逐字采用，并**新增**：

1. **m04**（读不出 footer 时 `rows = 1`）：计划步骤 3 没有明写失败语义，但「读不到就报错 / 就跳过，
   绝不返回 0 冒充读数」是本计划贯穿全局的一条判据（同族：任务 10 步骤 1 的
   `TestFooterRejectsShortFile`「不是返回 0」）。这条**行为**要求必须有直取它的变异，否则
   `TestObserveSkipsUnreadableArtifact` 无在册杀手（E62）。
2. **m05**（守卫里摘掉 `rows <= 0`）：同上，「非法分母不发明读数」是 `TestObserveSkipsDegenerateDenominator`
   承载的行为要求，必须有直取它的变异。
   > **形态说明（避免编译红 / 崩溃红，E70 约定 2）**：`bytesPerRow` 的计算写成
   > `if rows > 0 { bytesPerRow = … }`，因此摘掉守卫**不会**变成「整数除零 panic」
   > （那是崩溃红，不是断言红）。这是刻意选定的实现形态：它让这条变异红成
   > `TestObserveSkipsDegenerateDenominator` 的断言，而不是让工具表里多一条崩溃红。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。m01–m05 全部必须红成**断言红**。
"""

NAME = "p4-task-10-observability"
TARGET = "internal/tracesealer/runner.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/observability_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/observability_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动 + 手工拼的 32 字节产物：不依赖 zstd、不依赖真实流量，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}
EXPECTED_GREEN = {}

MUTATIONS = {}

# m01 预警阈值从 50% 改成 100%（等于取消预警）⇒ 余量只剩 2 倍时不再有任何提示；而冻结包超限时
#     返回 ErrTooManyRows ⇒ 该块永远不会被封出、spool 无限增长。边界仍是 `>`，所以「等于一半」不预警。
#     杀手：TestObserveWarnsAtHalfOfRowCap
MUTATIONS["m01_预警阈值改成100%"] = [
    ("sub", "rows > MaxTrailerEntries/2", "rows > MaxTrailerEntries"),
]

# m02 MinBytesPerRow 用 `>` 而不是 `<` 更新（变成最大值）⇒ 「每行最少多少字节」取的是**最优**的那一块，
#     容量账按偏乐观的一侧算。
#     杀手：TestObserveTracksMinimumBytesPerRow
MUTATIONS["m02_每行字节数取成最大值"] = [
    ("sub", "bytesPerRow < res.MinBytesPerRow", "bytesPerRow > res.MinBytesPerRow"),
]

# m03 bytesPerRow 的分母用 rows+1 ⇒ 每个块的 B/行读数都偏小（行数越多偏得越少，但恒偏小），
#     与压缩比口径不再自洽。
#     杀手：TestObserveTracksMinimumBytesPerRow
MUTATIONS["m03_每行字节数分母加一"] = [
    ("sub", "task.UncompressedBytes / int64(rows)", "task.UncompressedBytes / int64(rows+1)"),
]

# m04 读不出 footer 时把行数当成 1（而不是跳过本块）⇒ 一次**读取失败**被伪装成一个**读数**：
#     A8 的分布与 A6 的压缩比从此不可信，而且没有 ERROR 报警。
#     【为什么不是「把整个错误分支删掉」】那会让 rows 保持 0 并直接走到除法 ⇒ 若守卫也一起改动就是
#     崩溃红；本形态（rows = 1）红成断言，符合 E70 约定 2。
#     杀手：TestObserveSkipsUnreadableArtifact
MUTATIONS["m04_读不出footer当成1行"] = [
    ("sub", '\t\tr.logger.Error("trace-sealer 读不出产物的行数（本块的观测量不计入本轮；产物本身已经封好）",\n'
            '\t\t\t"reason", "footer_unreadable", "ref", refStr, "part_path", partPath, "err", err)\n'
            '\t\treturn',
            "\t\trows = 1"),
]

# m05 非法分母守卫里摘掉 `rows <= 0 ||` ⇒ 0 行的块被当成样本：少一条 WARN、多一条 per-part INFO、
#     且 MinRatio 被写成一个**真实读数**（凭空造出来的压缩比）。
#     杀手：TestObserveSkipsDegenerateDenominator
MUTATIONS["m05_非法分母守卫摘掉0行"] = [
    ("sub", "if rows <= 0 || task.UncompressedBytes <= 0 {", "if task.UncompressedBytes <= 0 {"),
]

# 变异测不到的两条要求各配一条**存在性**静态检查（E62：静态检查只承载存在/不存在类要求；行为由在册
# 用例承载，可失败性由人工探针承载 —— 见 COVERAGE_GAPS 的 falsifiability）。
CHECKS = [
    {
        "name": "最大行数与最小压缩比用例在册",
        "file": "tests/tracesealer/observability_test.go",
        "pattern": r"func TestObserveTracksMaxRowsAndMinRatio\(",
        "expect": "present",
        "why": ("「MaxRows 取最大、MinRatio 取最小」是**行为**要求，由该用例承载；本集合 m01–m05 都不翻转这两个"
                "量的更新方向 ⇒ 无杀手。**存在性**要求 ⇒ 静态检查钉住（删掉该用例就会 FAIL、整轮失败）。"),
    },
    {
        "name": "每块一行INFO与轮次汇总用例在册",
        "file": "tests/tracesealer/observability_test.go",
        "pattern": r"func TestObserveLogsOneInfoLinePerSealedPart\(",
        "expect": "present",
        "why": ("「每个成功块一行 INFO + 每轮一行汇总带三个量」是**行为**要求，由该用例承载；它的失效形态是"
                "「删掉/改错一条日志调用」，不是「改坏一条判据」⇒ 本集合的变异杀不到它。存在性由静态检查钉住。"),
    },
]

REQUIREMENTS = [
    {
        "plan": "行数严格超过 MaxTrailerEntries/2 ⇒ 打 WARN（带 rows / dt-inst-part / 占上界的百分比）；刚好等于一半不预警",
        "mutations": ["m01_预警阈值改成100%"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestObserveWarnsAtHalfOfRowCap`（按**结构化日志字段**断言，不 grep 文本）。",
    },
    {
        "plan": "MinBytesPerRow 取**最小**的「未压缩字节/行」（= 最小值，不是最大值）",
        "mutations": ["m02_每行字节数取成最大值", "m03_每行字节数分母加一"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestObserveTracksMinimumBytesPerRow`（两个块给出不同的 B/行）。",
    },
    {
        "plan": "MaxRows 取最大行数、MinRatio 取最小压缩比（压缩比最小 = 最难压的那一块）",
        "mutations": [],
        "checks": ["最大行数与最小压缩比用例在册"],
        "note": ("**行为**要求 ⇒ 在册用例 `TestObserveTracksMaxRowsAndMinRatio`。这两条**没有**专属变异："
                 "TARGET 的 m01–m05 都不翻转这两个量的更新方向 ⇒ 按 E64② 登记进文末 `COVERAGE_GAPS` 并给出"
                 "人工探针；存在性由同名静态检查钉住。"),
    },
    {
        "plan": "每个成功读到行数的 part 恰好一行 INFO；每轮汇总（trace-sealer round）带上 max_rows / min_bytes_per_row / min_ratio",
        "mutations": [],
        "checks": ["每块一行INFO与轮次汇总用例在册"],
        "note": ("**行为**要求 ⇒ 在册用例 `TestObserveLogsOneInfoLinePerSealedPart`（3 个块 ⇒ 3 行 INFO + 1 行汇总，"
                 "并断言汇总里的三个量与 RoundResult 一致）。无专属变异（m01 只改变预警条数，不改变正常块的 INFO 条数）"
                 "⇒ 按 E64② 登记进 `COVERAGE_GAPS` + 人工探针；存在性由同名静态检查钉住。"),
    },
    {
        "plan": "行数读不出来（产物不足 32 字节）⇒ 本轮无样本（三个量保持零值）+ 一条 reason=footer_unreadable 的 ERROR；Sealed 计数不受影响",
        "mutations": ["m04_读不出footer当成1行"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestObserveSkipsUnreadableArtifact`。",
    },
    {
        "plan": "非法分母（块内 0 行 / 未压缩字节 ≤ 0）⇒ 跳过本块 + reason=observation_skipped 的 WARN，绝不发明读数",
        "mutations": ["m05_非法分母守卫摘掉0行"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestObserveSkipsDegenerateDenominator`。",
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**要求/用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-MaxRows与MinRatio更新方向无专属变异",
        "title": ("`MaxRows` 取最大、`MinRatio` 取最小这两条更新方向由 `TestObserveTracksMaxRowsAndMinRatio` "
                  "在册承载，但本集合 m01–m05 没有一条翻转它们"),
        "where": [
            "tests/tracesealer/observability_test.go  TestObserveTracksMaxRowsAndMinRatio",
            "断言：MaxRows == 1000（块 1）且 MinRatio ≈ 4.0（块 0，难压的那一块）",
        ],
        "reachable": "**可达且确定**：替身 + 手工拼的 32 字节产物，pristine 侧每次运行都执行并 PASS。",
        "no_killer": ("要杀它们必须把 `if rows > res.MaxRows` 改成 `>=` 或把 `ratio < res.MinRatio` 改成 `>`。"
                      "本集合刻意**没有**加这两条变异：m02 已经直取同一函数里**同类**的「更新方向」判据"
                      "（`MinBytesPerRow` 的 `<`），再加两条形态几乎相同的变异只会让表更长而不增加信息。"
                      "【如实登记工具表的读数】m02/m03 会**顺带**杀死 `TestObserveTracksMaxRowsAndMinRatio`"
                      "（该用例同时断言 `MinBytesPerRow`，见权威表的「杀死的用例」列），但那条用例的**点名杀手**"
                      "是 `TestObserveTracksMinimumBytesPerRow`；**没有任何**一条变异直取 `MaxRows` / `MinRatio` "
                      "的更新方向。⇒ 缺口**可闭合**（加两条变异即可），本轮选择不加并**如实登记**："
                      "这不是「允许的绿」，而是「要求有在册用例、但没有点名杀手」。"),
        "falsifiability": ("本轮落盘了**人工探针**（改坏 → 转红 → 还原，见 `p4-task-10-probe-at-*.txt`）："
                           "把 `if rows > res.MaxRows` 改成 `if false`、把 `ratio < res.MinRatio` 改成 "
                           "`ratio > res.MinRatio`，逐条确认 `TestObserveTracksMaxRowsAndMinRatio` 转红 ⇒ "
                           "断言不是恒真。"),
        "owner": "本轮只登记。升格路径：在本集合补 m06/m07 两条变异（本轮不做，避免与 m02 重复）。",
    },
    {
        "id": "gap-2-per-part-INFO与轮次汇总字段无专属变异",
        "title": ("「每个成功块一行 INFO」与「轮次汇总带三个量」由 `TestObserveLogsOneInfoLinePerSealedPart` "
                  "在册承载，但本集合没有一条变异能杀它"),
        "where": [
            "tests/tracesealer/observability_test.go  TestObserveLogsOneInfoLinePerSealedPart",
            "断言：3 个块 ⇒ 3 行 `trace-sealer sealed part`（每条含 rows/bytes_per_row/ratio/row_cap_pct）"
            "+ 1 行 `trace-sealer round`（含与 RoundResult 一致的三个量）",
        ],
        "reachable": "**可达且确定**：全替身驱动，pristine 侧每次运行都执行并 PASS。",
        "no_killer": ("日志的**存在性与字段集**由「接线」保证：per-part 那条与轮次汇总那条分别写在 "
                      "`observeSealedPart` 与 `Round` 的固定位置。能杀它的变异形态是「删掉这条日志调用」"
                      "（那属于「删掉一个副作用」，不是「改坏一条判据」），而 m01–m05 都保留了两条日志调用。"
                      "⇒ 本集合内无杀手。轮次汇总的字段集另有 `p4-task-2-alert` 的静态检查 + 用例覆盖"
                      "（十个键恒在），不在本任务的边界内。"),
        "falsifiability": ("本轮落盘了**人工探针**（改坏 → 转红 → 还原，见 `p4-task-10-probe-at-*.txt`）："
                           "① 删掉 per-part 的 `r.logger.Info(\"trace-sealer sealed part\", …)` ⇒ 该用例在"
                           "`require.Len(t, parts, 3)` 处转红；② 把 `Fields` 里的 `max_rows` 键换成 "
                           "`max_rows_x` ⇒ 该用例在轮次汇总断言处转红（并同时打红 `p4-task-2-alert` 的清单）。"),
        "owner": "本轮只登记。",
    },
]
