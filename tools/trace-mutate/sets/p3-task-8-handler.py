"""计划 3 / 任务 8 的**映射层**变异集合（作者编写的产物 ⇒ 必须 git-tracked）。

工具一次只变异一个 `--target` ⇒ 任务 8 拆两份集合（与计划步骤 3 逐字一致）：
本份 `TARGET = internal/observability/handler/trace_body_error.go`（任务 3 的错误映射），
用例在 `tests/observability/handler/trace_body_error_test.go`；另一份 `p3-task-8.py`
的 `TARGET` 是任务 2 的服务文件。

**为什么任务 8（只写测试）要变异映射层**：A2 的验收命题之一是「四种状态互不混淆」的
**对外表现**。服务层的 `(err, source)` 互异性由 `TestAcceptanceA2StateMatrixMutuallyExclusive`
钉住，而「同一个 `err` 被映射成同一组 status/code」这一层只有**映射层**能承重 ——
若 `ErrNotArchived` 与 `ErrBodyMissing` 落到同一个 `code`，调用方就无法区分
「元数据与存储不一致（需人工介入）」与「正文从未落盘（业务事实）」。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 8 步骤 3 的第二份）的改动（逐条附理由）

计划只给了 `m01`，**逐字采用**（目标串 `case errors.Is(err, ta.ErrNotArchived):` 在本文件里
唯一，实测 `count == 1`）。本集合补两样：

① 集合必备的 `REQUIREMENTS` 映射表与 `CHECKS` 字段（全局约束第 2 条要求 10 个字段；
   计划草稿没给 —— 缺字段会让集合无法被工具加载）；
② `EXPECTED_GREEN` 为空（本集合**无零覆盖条目**）。

## 与 `p3-task-3.py` 的关系（**不是**重复覆盖，不得读成冗余）

`p3-task-3` 的 m01 是「`ErrNotArchived` 降级为 404」（把一个臂**摘掉**），
本集合的 m01 是「`ErrNotArchived` 与 `ErrBodyMissing` **同码**」（把两个臂**合并**）——
前者考「未归档还能不能自成一类」，后者考「未归档与正文缺失还能不能被区分」。
两条各自独立可失败，且杀死的都是 `TestMapTraceBodyError`。**不删任何一条**：
删掉本集合的 m01，就没有任何条目能证明「A2 的四态在对外映射层不混淆」。

## 覆盖边界

- 退出码/状态码的**其余** 12 条映射由 `p3-task-3` 的 m01–m12 承重（含包装、兜底、超时、
  取消、禁用 503 等），本集合**不重复**它们；
- HTTP 端点的入参/归属/可见性顺序由 `p3-task-5` 承重。
"""

NAME = "p3-task-8-handler"
TARGET = "internal/observability/handler/trace_body_error.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_error_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/handler/trace_body_error_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 「四种状态互不混淆」在对外映射层的对应变异：`ErrNotArchived` 与 `ErrBodyMissing`
#     被映射到同一个 code/status ⇒ 调用方无法区分「存储不一致（需人工介入）」与
#     「正文从未落盘（业务事实）」（规格 §8.1 反复强调要消灭的一类错误）。
#     【可编译】无表达式的 switch 里出现两个等值 case 是合法的（Go 只对常量 switch 报
#     duplicate case）⇒ 得到断言红，不是编译红（全局约束第 5 条）。
#     杀手：TestMapTraceBodyError（**必须是顶层函数名**：任务 8 的 A2 用例只断言服务层的
#     (err, source)，不会因映射层改动转红 —— 计划原文已就地订正过这一点）
MUTATIONS["m01_未归档与缺失同码"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):", "case errors.Is(err, ta.ErrBodyMissing):"),
]

CHECKS = []

# 1 条应在原始侧转为断言红（实测见本轮交付报告 §3）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 8 的 A2「四态互不混淆」在对外映射层的对应要求）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "A2 缺口 5 的对外表现：`ErrNotArchived` 与 `ErrBodyMissing` 必须映射到"
                  "**不同**的 code/status（存储不一致 ≠ 正文从未落盘）",
        "mutations": ["m01_未归档与缺失同码"],
        "checks": [],
        "note": (
            "杀手 = TestMapTraceBodyError（顶层用例；未归档行的 code 与 body_missing 行的 code "
            "由同一张表逐行断言）。本集合只有这一条要求 ⇒ `未被引用的变异 = 0`、`无 GAP`。"
        ),
    },
]
