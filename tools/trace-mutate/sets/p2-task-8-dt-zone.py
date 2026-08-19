r"""计划 2 / 任务 8 的变异集合（**dt 日期口径**）—— 作者编写的产物，必须 git-tracked。

拆成三份的理由同另外两份：变异工具一次只接受一个 `--target`。本文件针对
`internal/gateway/config/trace_body.go`（`DayStart` / `TraceBodyZone`：全局约束里
**唯一**的日期入口），用同一份名字清单产出第三张权威表。

【为什么 dt 口径必须有自己的一张表（A1 的 ① 里那两条断言能不能红）】
A1 要断言「元数据行的 `body_ref_dt` 是事件在 **UTC+8** 下的日历日、且是该日的
**UTC 零点**」。这两条断言只有在**跨日形状**的时间戳下才对时区口径敏感：
本任务的 A1 取「北京 2026-09-25 00:00 起、每条 +1s」——这 2000 条事件的 UTC 日期
全是 09-24，UTC+8 日期是 09-25。于是：
  - 时间戳若取北京 09-24 10:00（简报原文），其 UTC 日期**也是** 09-24 ⇒
    口径退回 UTC 的实现会**全绿**（假绿）。这是本任务实测发现并修正的一处假绿，
    记在报告里；
  - 取跨日形状后，口径漂移立刻可见（下面两条变异就是它的杀手）。

【与 p2-task-2 的集合的关系（如实标注，不是重复劳动）】p2-task-2 的集合钉的是
「配置校验」这套结构不变量，杀手是它自己的配置用例；本集合钉的是**验收用例**
（TestAcceptanceA1WriteHalf）对同一口径的敏感度 —— 两张表的证据对象不同。
"""

NAME = "p2-task-8-dt-zone"
TARGET = "internal/gateway/config/trace_body.go"
# 同另外两份：只列 B 层包，否则 go test 会出现 [no tests to run] ⇒ 证据无效。
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = ["tests/gateway/observability/trace_body_acceptance_test.go"]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/trace_body_acceptance_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① dt 口径退回 UTC（偏移 +08:00 → 00:00）----
# 这是全局约束点名要防的缺陷（「dt 选错会让正文落进错误的 dt= 目录」）：
# 跨日事件（UTC 日期与 UTC+8 日期不同）会落进前一天。
# 杀手 TestAcceptanceA1WriteHalf 的 ①：`require.Equal("2026-09-25", BodyRefDT.Format(...))`
# 实测得到 2026-09-24。
MUTATIONS["m01_dt口径退回UTC"] = [
    ("sub", 'var TraceBodyZone = time.FixedZone("CST", 8*60*60)',
            'var TraceBodyZone = time.FixedZone("CST", 0)'),
]

# ---- ② DayStart 不再归一到 UTC 零点（直接返回带 +08:00 的本地零点）----
# `body_ref_dt` 是 PG `date` 列，驱动按**会话时区**（生产 = UTC）折算：
# 传 `2026-09-25T00:00:00+08:00` 会落成 09-24（差一天）。
# 杀手 TestAcceptanceA1WriteHalf 的 ①：`require.Equal(time.Date(2026,9,25,0,0,0,0,time.UTC), row.BodyRefDT.UTC())`
# —— Format 那一条**仍然通过**（日期字符串一样），只有 UTC 零点这一条能看见差别。
MUTATIONS["m02_DayStart不归一为UTC零点"] = [
    ("sub", "\treturn time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)",
            "\treturn t"),
]

CHECKS = []
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "全局约束：dt = 事件 TS 在 UTC+8 下的日期（唯一的日期入口 DayStart/DayOf）",
        "mutations": ["m01_dt口径退回UTC", "m02_DayStart不归一为UTC零点"],
        "checks": [],
        "note": (
            "两条的杀手都是 TestAcceptanceA1WriteHalf 的 ①（dt 的字符串口径与 UTC 零点口径"
            "各一条）。m02 说明「Format 相等」与「UTC 零点相等」是两条**互补**的断言："
            "只留前者时 m02 会变成假绿。"
        ),
    },
]
