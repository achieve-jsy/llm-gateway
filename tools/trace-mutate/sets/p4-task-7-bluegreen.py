r"""计划 4 / 任务 7 的变异集合：蓝绿切换与回滚剧本（`deploy/blue-green/README.md`）。

主题：剧本是**部署产物即契约** —— 它随 `scp -r deploy/blue-green/*` 落到生产宿主，
运维在机器上读到的就是它。因此被变异对象是**剧本文本本身**：删掉硬门、把「sealer 必须
继续运行」改成「可随功能停掉」、删掉逐字节等价的 SQL 比对、把 `body_ref_inst` 换成
「配置里的 inst」、插入已被裁定的无效键 —— 这五种走样每一种都必须让**点名用例**转红。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 7 步骤 5 逐字给出五条变异与各自的杀手（m01–m05），本文件逐字采用；
步骤 3 的静态检查（`deploy/blue-green/deploy-blue-green.sh` 内 `TRACE_BODY` absent）
逐字采用。**追加**的六行静态检查只承载「剧本必须含某段文字」这类**存在性**要求
（E62：静态检查只承载存在/不存在类要求），它们与 Go 用例是同一批文字的两条独立读法。

## 变异测不到、但有静态检查行（CHECKS）

剧本里「颜色↔inst 的静态对应」「切换与 .active-color 抄录」「旧颜色 drain 无损」
「禁止事项」四类要求由 `tests/deploy/trace_bluegreen_runbook_test.go` 的在册用例断言，
但计划步骤 5 没有为它们登记变异（五条变异已占满 m01–m05）⇒ 按约束 6 各补一行
`expect=present` 的静态检查；它们的**可失败性**由交付报告里的「改坏→转红→还原」探针
逐条实测（`p4-task-7-bluegreen-probe-at-*.txt`）。
"""

NAME = "p4-task-7-bluegreen"
TARGET = "deploy/blue-green/README.md"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_bluegreen_runbook_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_bluegreen_runbook_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 删掉整节硬门（D73）⇒ 「同一 root 的读侧未就绪不得取消注释写侧开关」消失，
#     剧本只剩下「取消注释即可开启」，读者会照做。
#     杀手：TestBlueGreenRunbookHasReadSideHardGate
MUTATIONS["m01_删掉硬门段落"] = [
    ("sub", """## 4. 硬门：取消注释写侧开关之前（`⛔ 停止点 1`，D73）

**同一 root 的读侧可用之前，不得设置写侧开关**。取消注释 `TRACE_BODY_ROOT` 之前必须同时满足：

1. **① 同一 root 的读侧已可用**：读侧服务（计划 3）已落地，并且在 nginx 后面的管理面用一条真实
   `request_id` 点读成功（200 + 正文）。
2. **② 挂载正常**：`findmnt -T /datalog --output TARGET,SOURCE,FSTYPE` 的 `TARGET` 正好是 `/datalog`，
   且 `FSTYPE` 是真实文件系统（不是 `/`、不是容器层）。
3. **③ 容量足够**：`/datalog` 剩余空间 ≥ **5 天保留量**（口径见 `docs/deployment.md` 的运行手册）。

以上任一不满足 ⇒ `⛔` **停下来**，不得取消注释：取消注释是**开启静默丢正文**的唯一开关 ——
读侧未就绪时正文只进 `/datalog`，管理面与任何 API 都取不到（`body_ref_*` 有值、jsonb 列为 NULL）。

""", ""),
]

# m02 把「sealer 必须继续运行」改成「sealer 可随功能一并停掉」⇒ 回滚读者会停掉 sealer，
#     未封的 spool 永久留在磁盘上（已写出的 body_ref_* 从此点不动）。
#     杀手：TestBlueGreenRunbookCoversRollback
MUTATIONS["m02_回滚可停sealer"] = [
    ("sub", """- **sealer 必须继续运行**：功能回滚**不**停止 `trace-sealer.service`；未封的 spool 必须继续被封，
  否则已写出的 `body_ref_*` 会永久停在 spool 上（读侧永远点不动）。
""", """- **sealer 可随功能一并停掉**：回滚时可以直接 `systemctl stop trace-sealer`。
"""),
]

# m03 删掉「逐字节等价」的可粘贴 SQL 比对 ⇒ 功能关闭后没人能自证「行为与开启前相同」，
#     「最轻的回滚」失去可验证的收口条件。
#     杀手：TestBlueGreenRunbookCoversFeatureOffByteIdentical
MUTATIONS["m03_删掉SQL比对"] = [
    ("sub", """```sql
-- ① 期望 0：功能关闭后的行不得留下任何定位键
SELECT count(*) AS ref_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND (body_ref_dt IS NOT NULL OR body_ref_inst IS NOT NULL
        OR body_ref_part IS NOT NULL OR body_ref_row IS NOT NULL);

-- ② 期望 0：功能关闭后不得出现写入期失败标记
SELECT count(*) AS missing_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND body_missing IS TRUE;

-- ③ 期望 0：功能关闭后每一行都必须在 jsonb 列里有正文（不许只留 NULL 的壳）
SELECT count(*) AS jsonb_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND request_body IS NULL AND response_body IS NULL;
```

""", ""),
]

# m04 把「读侧必须用行上的 body_ref_inst 定位」改成「直接用配置里的 inst 定位」⇒
#     切换期间两个 inst 并存，读侧会把旧 inst 的正文读成「找不到」（H2 契约被违反）。
#     杀手：TestBlueGreenRunbookCoversSwitch
MUTATIONS["m04_改用配置里的inst"] = [
    ("sub", """4. **切换期间的读侧口径**：切换期间两个 inst 并存 ⇒ 读侧必须用行上的 `body_ref_inst` 定位，
   绝不能用配置里的 `inst`（跨计划契约 H2 的落点）。旧颜色 drain 完成前旧 inst 仍可能有流量；
   配置里的 `inst` 只描述「当前活跃颜色」，用它定位会把旧 inst 的正文读成「找不到」。""",
            """4. **切换期间的读侧口径**：切换期间两个 inst 并存 ⇒ 读侧直接用配置里的 `inst` 定位即可
   （跨计划契约 H2 的落点）。旧颜色 drain 完成前旧 inst 仍可能有流量。"""),
]

# m05 插入已被裁定的无效键（D71）⇒ 读者会以为它有用/生效，而它既不报错也不生效。
#     杀手：TestBlueGreenRunbookNeverMentionsSealInterval
MUTATIONS["m05_插入无效键"] = [
    ("sub", """## 6. 禁止事项（逐条）

""", """## 6. 禁止事项（逐条）

- 可用 `TRACE_BODY_SEAL_INTERVAL: "15m"` 调整封块节奏。
"""),
]

CHECKS = [
    {
        "name": "蓝绿脚本不得出现TRACE_BODY键族",
        "file": "deploy/blue-green/deploy-blue-green.sh",
        "pattern": r"TRACE_BODY",
        "expect": "absent",
        "why": ("计划任务 7 步骤 3.1：`deploy/blue-green/deploy-blue-green.sh` **不需要任何改动**"
                "（它只管容器与 nginx，不认识任何 TRACE_BODY_ 键）⇒ 该脚本里不得出现这一族键名。"
                "**不存在性**要求 ⇒ 静态检查。"),
    },
    {
        "name": "剧本必须钉住颜色↔inst静态对应",
        "file": "deploy/blue-green/README.md",
        "pattern": r"TRACE_BODY_INST=blue",
        "expect": "present",
        "why": ("计划任务 7 步骤 3.1：blue 文件 ⇒ `TRACE_BODY_INST=blue`；green 文件 ⇒ `green`。"
                "**存在性**要求 ⇒ 静态检查（同一批文字另由 TestBlueGreenRunbookCoversSwitch 断言）。"),
    },
    {
        "name": "剧本必须含读侧硬门D73",
        "file": "deploy/blue-green/README.md",
        "pattern": r"同一 root 的读侧可用之前，不得设置写侧开关",
        "expect": "present",
        "why": ("计划任务 7 步骤 3.5 + D73：读侧未就绪前不得取消注释写侧开关。"
                "**存在性**要求 ⇒ 静态检查（可失败性另由 m01 与探针承载）。"),
    },
    {
        "name": "剧本必须含功能关闭的SQL比对",
        "file": "deploy/blue-green/README.md",
        "pattern": r"SELECT count\(\*\) AS ref_rows",
        "expect": "present",
        "why": ("计划任务 7 步骤 3.4：功能关闭必须逐字节等价，并给出可粘贴的 SQL 比对。"
                "**存在性**要求 ⇒ 静态检查（可失败性另由 m03 与探针承载）。"),
    },
    {
        "name": "剧本必须含旧颜色drain无损",
        "file": "deploy/blue-green/README.md",
        "pattern": r"drain 期间旧颜色功能仍是关闭的",
        "expect": "present",
        "why": ("计划任务 7 步骤 3.6：drain 期间旧颜色功能仍关闭 ⇒ 正文继续进 jsonb ⇒ 无数据损失。"
                "**存在性**要求 ⇒ 静态检查（可失败性由探针实测）。"),
    },
    {
        "name": "剧本必须含禁止事项",
        "file": "deploy/blue-green/README.md",
        "pattern": r"不得在两个颜色里设同一个 `inst`",
        "expect": "present",
        "why": ("计划任务 7 步骤 3.7：不得在两个颜色里设同一个 inst / 不得把 TRACE_BODY_* 放进"
                "config/prod.yaml / 不得在功能开启后停止 sealer。**存在性**要求 ⇒ 静态检查"
                "（可失败性由探针实测）。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m05 都断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "颜色↔inst 的静态对应（blue ⇒ inst=blue、green ⇒ inst=green），且 deploy-blue-green.sh 零改动",
        "mutations": [],
        "checks": ["剧本必须钉住颜色↔inst静态对应", "蓝绿脚本不得出现TRACE_BODY键族"],
        "note": ("**存在性/不存在性**要求 ⇒ 两行静态检查；同一批文字另由 "
                 "TestBlueGreenRunbookCoversSwitch 的在册断言承载（颜色写反 ⇒ 该用例转红）。"),
    },
    {
        "plan": "切换：次序（起新 → /health+/ready → nginx 原子切流 → drain 旧 → 停旧）、抄录 .active-color、读侧按 body_ref_inst 定位（H2）",
        "mutations": ["m04_改用配置里的inst"],
        "checks": [],
        "note": ("杀手 = TestBlueGreenRunbookCoversSwitch（m04 把 `body_ref_inst` 换成「配置里的 inst」⇒"
                 "该用例在 H2 那条 `require.Contains` 处转红；次序与 .active-color 由同一用例的其他断言承载）。"),
    },
    {
        "plan": "回滚：`--rollback` 切回旧颜色 + 旧 inst 数据仍可读 + **sealer 不随功能回滚而停止**",
        "mutations": ["m02_回滚可停sealer"],
        "checks": [],
        "note": ("杀手 = TestBlueGreenRunbookCoversRollback（m02 把「sealer 必须继续运行」改成「可随功能"
                 "一并停掉」⇒ 该用例在「功能回滚**不**停止」那条断言处转红）。"),
    },
    {
        "plan": "功能降级：逐字节等价（body_ref_* 全 NULL + body_missing=false + 正文在 jsonb 列）+ 可粘贴的 SQL 比对",
        "mutations": ["m03_删掉SQL比对"],
        "checks": ["剧本必须含功能关闭的SQL比对"],
        "note": ("杀手 = TestBlueGreenRunbookCoversFeatureOffByteIdentical（m03 删掉 SQL 块 ⇒ `SELECT count(*) "
                 "AS ref_rows` 断言转红）。"),
    },
    {
        "plan": "硬门（⛔ 停止点）：同一 root 的读侧未就绪前不得取消注释写侧开关（D73），含 findmnt 与 ≥5 天保留量",
        "mutations": ["m01_删掉硬门段落"],
        "checks": ["剧本必须含读侧硬门D73"],
        "note": "杀手 = TestBlueGreenRunbookHasReadSideHardGate（m01 删掉整节 ⇒ 五条断言全部失去文字）。",
    },
    {
        "plan": "旧颜色处置：drain 期间功能仍关闭 ⇒ 正文继续进 jsonb ⇒ 无数据损失；drain 后按新编排重建",
        "mutations": [],
        "checks": ["剧本必须含旧颜色drain无损"],
        "note": ("**存在性**要求 ⇒ 静态检查；另由 TestBlueGreenRunbookCoversOldColorDrain 的在册断言承载"
                 "（可失败性由探针实测）。"),
    },
    {
        "plan": "禁止事项：不得两个颜色同一个 inst、不得把 TRACE_BODY_* 放进 config/prod.yaml、不得在功能开启后停止 sealer",
        "mutations": [],
        "checks": ["剧本必须含禁止事项"],
        "note": "**存在性**要求 ⇒ 静态检查（可失败性由探针实测）。",
    },
    {
        "plan": "D71：剧本里不得出现已被裁定的无效键（封块间隔键）",
        "mutations": ["m05_插入无效键"],
        "checks": [],
        "note": "杀手 = TestBlueGreenRunbookNeverMentionsSealInterval（`require.NotContains`）。",
    },
]
