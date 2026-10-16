r"""计划 4 / 任务 11 的变异集合：调用链正文（`/datalog`）运行手册（`docs/deployment.md`）。

主题：手册是**部署产物即契约** —— 它活在 git 仓库的 `docs/` 里（也在蓝绿剧本里被引用），
运维照着它判断「撤掉挂载后闩锁了吗 / 该丢正文了吗 / 要不要重启」。因此被变异对象是
**手册文本本身**：删掉「需要重启才恢复」一句、把「≤1 个 tick」放宽成 10 分钟、删掉
功能关闭的 SQL 比对、把关停预算 6s 写成 60s、插入已被裁定的无效键、删掉前置条件段落、
删掉「禁止切换蓝绿流量」的停止点 —— 这七种走样每一种都必须让**点名用例**转红。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 11 步骤 5 逐字给出七条变异与各自的杀手（m01–m07），本文件逐字采用。

**m06/m07 的效力边界（计划原文，如实照抄）**：这两条验证的是**计划/手册文本里的门禁
陈述是否存在**（`docs/deployment.md` 中必须出现「前置条件」与「禁止切流」两段），
**不是**运行时行为 —— 自动用例钉不住「切流会被阻止」，只能钉住「这句话还在不在」。
因此下面的 `why` / `REQUIREMENTS` 一律写成「门禁声明的存在性」，**不得**声称它证明了
运行时防护；运行时防护由门槛二（摘流留证）与停止点 A（人工值守）承担。删掉 m06/m07
会让这两条门禁失去唯一的自动覆盖 ⇒ 保留。

## 追加的静态检查（E62：行为要求必须有在册行为用例承载）

计划的步骤 5 只登记了 m01–m07，而步骤 1 还要求两条**在册用例**（它们都在名字清单里、
每次运行都真的执行）：`TestRunbookDocumentsRetentionAndSpaceBudget` 与
`TestRunbookDocumentsSealerHeartbeatAndPermanentFailure`。它们没有对应的变异（七条编号
已占满），按**约束 6** 各补一行 `expect=present` 的静态检查承载「手册里必须有这段文字」
的**存在性**要求；两条用例本身是**行为**要求的承载者（E62），静态检查只是同一批文字的
第二条独立读法。它们的可失败性由交付报告里的「改坏→转红→还原」探针逐条实测
（`p4-task-11-runbook-probe-at-*.txt`）。
"""

NAME = "p4-task-11-runbook"
TARGET = "docs/deployment.md"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_runbook_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_runbook_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：不启动 docker、不连生产、不读未导出符号，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 删掉「换盘后必须重启才能恢复写盘」一句 ⇒ 运维以为 remount 就能恢复，实际会一直闩锁
#     （身份基线是启动时取的，dev/fstype 变了就永久不匹配）。
#     杀手：TestRunbookDocumentsDiskSwapNeedsGatewayRestart
MUTATIONS["m01_删掉需要重启一句"] = [
    ("sub", "**必须重启后才能恢复写盘** —— 只重新挂载同一个路径不会重建身份基线。", ""),
]

# m02 把「≤1 个 tick」放宽成「≤10 分钟」⇒ 判据从「一个 flush 周期」变成「十分钟」，
#     读者会把「十分钟内闩锁」当成达标（而真值是 ≤1 个 tick = ≤1m）。
#     杀手：TestRunbookDocumentsMountLossLatchesWithinOneTick
MUTATIONS["m02_把一个tick放宽成十分钟"] = [
    ("sub", "**≤1 个 tick**", "**≤10 分钟**"),
]

# m03 删掉功能关闭「逐字节相同」的可粘贴 SQL 比对 ⇒ 没人能自证「功能关闭 = 与改动前相同」，
#     「最轻的降级」失去可验证的收口条件。
#     杀手：TestRunbookDocumentsFeatureOffIsByteIdentical
MUTATIONS["m03_删掉SQL比对"] = [
    ("sub", """```sql
-- ① body_ref_part IS NULL AND body_missing = false 的行数应等于总行数
SELECT count(*) FILTER (WHERE body_ref_part IS NULL AND body_missing = false) AS plain_rows,
       count(*) AS total_rows
  FROM call_traces
 WHERE ts >= :off_since;
-- ② 正文出现在 jsonb 列
SELECT count(*) AS jsonb_rows
  FROM call_traces
 WHERE ts >= :off_since AND request_body IS NOT NULL;
```

""", ""),
]

# m04 把关停排空预算 6s（traceSpoolCloseWait）写成 60s ⇒ 运维以为还有一分钟，
#     实际关停编排只等 6s，最后一批会被静默降级成 body_missing=true。
#     杀手：TestRunbookDocumentsShutdownSixSecondBound（该用例另有一条 NotContains("60s")）
MUTATIONS["m04_把6s改成60s"] = [
    ("sub", "**`traceSpoolCloseWait` = 6s**", "**`traceSpoolCloseWait` = 60s**"),
    ("sub", "**6s 内没排空**", "**60s 内没排空**"),
    ("sub", "### 关停：6s 排空上限", "### 关停：60s 排空上限"),
]

# m05 插入已被裁定的无效键（D71）⇒ 读者会以为它有用/生效，而它既不报错也不生效。
#     杀手：TestRunbookNeverMentionsSealInterval
MUTATIONS["m05_插入无效键"] = [
    ("sub", "### sealer 侧：心跳、`too_many_rows` 与隔离件\n",
            "### sealer 侧：心跳、`too_many_rows` 与隔离件\n\n- 可用 `TRACE_BODY_SEAL_INTERVAL: \"15m\"` 调整封块节奏。\n"),
]

# m06 删掉整段前置条件（`TRACE_BODY_ROOT` 非空 + root 在场 + 「读侧就绪后启用」）⇒
#     功能关闭时手册的观察断言恒真（什么都没证明），而读者不会再看到这个前提。
#     杀手：TestRunbookDocumentsMountLossPrecondition（门禁声明的存在性，见文件头）
MUTATIONS["m06_删掉前置条件段落"] = [
    ("cut", "### ⛔ 前置条件", "**立即执行**下面这一节。"),
]

# m07 删掉「验证窗口内禁止切换蓝绿流量」的停止点 ⇒ 丢正文防护只剩人工记忆；
#     被 umount 的颜色处在「丢正文」降级，切流到它 = 按行丢正文。
#     杀手：TestRunbookDocumentsMountLossLatchesWithinOneTick（门禁声明的存在性，见文件头）
MUTATIONS["m07_删掉禁止切流停止点"] = [
    ("cut", "⛔ **停止点 A：验证窗口内禁止切换蓝绿流量", "切流到它 = 按行丢正文。"),
]

CHECKS = [
    {
        "name": "手册必须含两种降级不对称与补救动作",
        "file": "docs/deployment.md",
        "pattern": r"两种降级不对称",
        "expect": "present",
        "why": ("计划任务 11 步骤 1 的 `TestRunbookDocumentsMountLossLatchesWithinOneTick` 要求写出"
                "「两种降级不对称」：`sink == nil` ⇒ 无损；spool 失败 / 根身份闩锁 ⇒ 丢正文；"
                "「把写侧关掉让正文回落 jsonb」只是**补救动作**。**存在性**要求 ⇒ 静态检查；"
                "行为要求由在册用例 `TestRunbookDocumentsMountLossLatchesWithinOneTick` 承载"
                "（它在名字清单里，每次运行都真的执行）。"),
    },
    {
        "name": "手册必须含保留与空间预算口径",
        "file": "docs/deployment.md",
        "pattern": r"df -h /datalog",
        "expect": "present",
        "why": ("计划任务 11 步骤 1 的 `TestRunbookDocumentsRetentionAndSpaceBudget` 要求写出保留口径"
                "（minAge 3h × 写入速率 + 产物 + 余量）并给出 `/datalog` 的余量检查命令。**存在性**"
                "要求 ⇒ 静态检查；行为要求由在册用例 `TestRunbookDocumentsRetentionAndSpaceBudget`"
                "承载（它在名字清单里，每次运行都真的执行）。"),
    },
    {
        "name": "手册必须含sealer心跳与永久失败处置",
        "file": "docs/deployment.md",
        "pattern": r"too_many_rows",
        "expect": "present",
        "why": ("计划任务 11 步骤 1 的 `TestRunbookDocumentsSealerHeartbeatAndPermanentFailure` 要求写出"
                "心跳缺失（>3×poll 无「轮次结束」）⇒ 告警、`too_many_rows` ⇒ 人工介入（拆块 / 调小"
                "BLOCK_MB 并重新标定）、隔离件处置。**存在性**要求 ⇒ 静态检查；行为要求由在册用例"
                "`TestRunbookDocumentsSealerHeartbeatAndPermanentFailure` 承载。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m07 全部断言红（且都红成**断言红**，不是编译红）。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "前置条件：`TRACE_BODY_ROOT` 非空且 root 目录在场（构造期 rootExists）；功能关闭时 "
                "sink==nil、TraceSpooler 不构造 ⇒ 观察断言恒真；位置 = 「读侧就绪（D73）→ 启用写侧 → 立即执行」",
        "mutations": ["m06_删掉前置条件段落"],
        "checks": [],
        "note": ("杀手 = TestRunbookDocumentsMountLossPrecondition（m06 整段剪掉 ⇒ 十条断言失去文字）。"
                 "**门禁声明的存在性**：它验证的是手册里「有这段前提」，不是运行时防护。"),
    },
    {
        "plan": "撤掉挂载：≤1 个 tick（= TRACE_BODY_FLUSH_INTERVAL，生产 1m）内闩锁、不再写盘、"
                "正文降级 body_missing=true；可粘贴观察命令（日志正则 + 文件系统断言）",
        "mutations": ["m02_把一个tick放宽成十分钟"],
        "checks": [],
        "note": ("杀手 = TestRunbookDocumentsMountLossLatchesWithinOneTick（m02 把「≤1 个 tick」改成"
                 "「≤10 分钟」⇒ 该断言转红）。"),
    },
    {
        "plan": "停止点：「验证窗口内禁止切换蓝绿流量」是**丢正文防护**（不是仅降级），"
                "窗口内必须有人值守",
        "mutations": ["m07_删掉禁止切流停止点"],
        "checks": [],
        "note": ("杀手 = TestRunbookDocumentsMountLossLatchesWithinOneTick（m07 剪掉停止点 A ⇒"
                 "「禁止切换蓝绿流量」「丢正文防护」「不得运行 deploy-blue-green.sh」三条断言转红）。"
                 "**门禁声明的存在性**：运行时防护由门槛二（摘流留证）与人工值守承担。"),
    },
    {
        "plan": "两种降级不对称：「关掉写侧让正文回落 jsonb」只是**事后补救动作**，"
                "闩锁期间已写下的行无法找回；实验只允许测试环境或无真实流量的生产窗口，"
                "并留证流量为零与累计丢失行数（期望 0）",
        "mutations": [],
        "checks": ["手册必须含两种降级不对称与补救动作"],
        "note": ("**存在性**要求 ⇒ 静态检查（`两种降级不对称`）；"
                 "**行为/文本**要求由在册用例 TestRunbookDocumentsMountLossLatchesWithinOneTick 的"
                 "④⑤ 两组断言承载（测试矩阵、补救动作、测试环境、留证、累计丢失行数、期望增量 = 0）；"
                 "本集合没有为它们单独编号变异（七条编号已占满），其可失败性由探针实测"
                 "（`p4-task-11-runbook-probe-at-*.txt`）。"),
    },
    {
        "plan": "换盘 / 改挂载点（dev/fstype 变化）⇒ 身份基线永久不匹配 ⇒ 一直闩锁到重启；"
                "处置 = 蓝绿切对端颜色 / 重启容器；必须重启后才能恢复写盘",
        "mutations": ["m01_删掉需要重启一句"],
        "checks": [],
        "note": "杀手 = TestRunbookDocumentsDiskSwapNeedsGatewayRestart（m01 删掉该句 ⇒ 断言转红）。",
    },
    {
        "plan": "关停：6s（traceSpoolCloseWait）内没排空 ⇒ 该批降级 body_missing=true + 一条 ERROR；"
                "给出观察命令与处置",
        "mutations": ["m04_把6s改成60s"],
        "checks": [],
        "note": ("杀手 = TestRunbookDocumentsShutdownSixSecondBound（m04 把 6s 改成 60s ⇒"
                 "`Contains(\"traceSpoolCloseWait\\` = 6s\")` 与 `NotContains(\"60s\")` 双双转红）。"),
    },
    {
        "plan": "功能关闭 = 逐字节相同：body_ref_part IS NULL AND body_missing = false 的行数等于总行数、"
                "正文出现在 jsonb 列（可粘贴 SQL）",
        "mutations": ["m03_删掉SQL比对"],
        "checks": [],
        "note": "杀手 = TestRunbookDocumentsFeatureOffIsByteIdentical（m03 删掉 SQL 块 ⇒ 断言转红）。",
    },
    {
        "plan": "保留与空间预算：minAge 3h × 写入速率 + 产物 + 余量；/datalog 的余量检查命令；"
                "引用 2026-10-08 的生产基线（407G 已用 / 153G 可用）时必须标注日期",
        "mutations": [],
        "checks": ["手册必须含保留与空间预算口径"],
        "note": ("**存在性**要求 ⇒ 静态检查（`df -h /datalog`）；行为要求由在册用例 "
                 "TestRunbookDocumentsRetentionAndSpaceBudget 承载（它另断言 2026-10-08 与"
                 "「407G 已用 / 153G 可用」必须同现）。"),
    },
    {
        "plan": "sealer 侧：心跳缺失（>3×poll 无「轮次结束」）⇒ 告警；too_many_rows ⇒ 人工介入"
                "（拆块 / 调小 BLOCK_MB 并重新标定）；隔离件（spool 目录里的明文 .jsonl.zst）处置",
        "mutations": [],
        "checks": ["手册必须含sealer心跳与永久失败处置"],
        "note": ("**存在性**要求 ⇒ 静态检查（`too_many_rows`）；行为要求由在册用例 "
                 "TestRunbookDocumentsSealerHeartbeatAndPermanentFailure 承载（心跳 / journalctl /"
                 "隔离件形态 / ErrNotArchived / 不得当产物解码）。"),
    },
    {
        "plan": "D71：手册里不得出现已被裁定的无效键（封块间隔键）",
        "mutations": ["m05_插入无效键"],
        "checks": [],
        "note": "杀手 = TestRunbookNeverMentionsSealInterval（`require.NotContains`）。",
    },
]
