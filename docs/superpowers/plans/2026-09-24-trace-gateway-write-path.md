# 网关写入侧接入（正文直写 /datalog）实施计划（计划 2/4）

> **面向 Agent 执行者：** 必需子技能：使用 superpower-subagent-driven-development（推荐）或 superpower-executing-plans 按任务逐项执行本计划。步骤使用复选框（`- [ ]`）语法进行跟踪。

**目标：** 让网关把 `call_traces` 的正文写进 `/datalog` 上的 spool（一行 JSONL 一条），PG 只保留元数据与定位键 `(dt, inst, part, row)`；并在磁盘满/不可写时退化为「只记元数据 + `body_missing=true`」而不拖垮服务。

**架构：** 复用计划 1 已冻结的 `internal/tracearchive` 写入原语（`Writer.Append` 顺序追加、`PendingSeals` 无条件 flush、`MarkSealed` 滚动）。**网关内不做任何压缩**（方案 C：压缩由独立 sealer 进程调用 C 版 zstd CLI 完成，属计划 4）。网关侧新增四个部件：① `TraceBodyConfig`（`TRACE_BODY_*` 解析 + `dt` 口径）；② `traceEventLine`（把 `TraceEvent` 序列化成**一整行** JSON）；③ `SpoolGate`（磁盘水位 + 写失败闩锁，即 R9 背压）；④ `TraceSpooler`（周期 flush + 滚动 part，即 R8）。**写次序是承重不变量**：先追加 spool、再写 PG 元数据行（规格 §10.1 I1）。

**技术栈：** Go 1.26、`github.com/spf13/viper`（既有配置体系）、`github.com/example/llm-gateway/internal/tracearchive`（计划 1，**只读复用、不改行为**）、`gorm.io/gorm` + `gorm.io/driver/sqlite`（测试）、`github.com/stretchr/testify`、Atlas（`make atlas-diff` / `atlas migrate hash` / `validate`）。

**规格：** `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md`（论证以规格为准，执行者需同时阅读规格与本计划）

## 全局约束

- **范围**：本计划 = ① PG 迁移（规格 §6 的 6 个新列，走 `atlas-db-migration` skill）+ ② 网关写入侧接入。**点读服务（读侧）属计划 3，不在本计划**；**sealer 进程、systemd unit、`/datalog` bind mount、蓝绿切换属计划 4**。本计划**不改** `deploy/blue-green/docker-compose.*.yml`。
- **计划 1 的包已冻结**：`internal/tracearchive` 已附 142 条变异证据（R11 v3，冻结提交 `ea962a70`）。**本计划不得修改其中任何文件的行为**，也不得新增/改名其中的文件；只在网关侧以公开 API 复用。若确需新增类型，必须写进交付报告而不是直接改。
- **压缩参数终值（逐字，规格 §5.3.3 / §5.3.1.1）**：编码器 = C 版 zstd CLI、参数 **`-19 --long=29`**（窗口 512 MiB）、编码器并发 `1`（CLI 侧 `-T1`）、块大小 **1536 MB**、时间阈值 **15 分钟**（`defaultSealInterval`）。**这些参数在本计划里只作为「网关传给 `WriterOptions` 的阈值」出现**：`BlockBytes = TRACE_BODY_BLOCK_MB × 1 MiB`、滚动间隔见下条。网关侧**不读** `TRACE_BODY_ZSTD_LEVEL` / `TRACE_BODY_ZSTD_WINDOW_MB`（方案 C 下编码不在网关，见 D43）。
- **滚动与 flush（规格 §15.1 R8 / §7.1；数值与推导逐字）**：
  - **滚动间隔 ≤ 1 小时，且严格小于 sealer 的 `minAge` = 3 小时**。本计划默认 `TRACE_BODY_SEAL_INTERVAL = "1h"`，理由：`minAge = 3 × 1h = 3h` 的推导链要求「网关滚动间隔取 ~1 小时，让**大小**阈值（1536 MB）先触发，从而保住已标定的 **10.26×** 压缩比（滚动过短会把块截成 ~400 MB，压缩比退到未标定的区间）」。**15m 也满足「≤1h」，但会让块只有 ~400 MB、加密约 10.26× 的标定失效 ⇒ 不采用**（规格 §11.1 的 `"15m"` 与之冲突，登记为 D44）。
  - **flush 周期 ≤ 1 分钟**，默认 `TRACE_BODY_FLUSH_INTERVAL = "1m"`，且**必须无条件执行**（不设「本轮是否要封块」的前置条件），入口是 `(*tracearchive.Writer).PendingSeals()`——它的 `Flush` 在空块提前返回（`writer.go:377`）与阈值提前返回（`writer.go:380-384`）**之前**执行；只有 `cur == nil`（`:358`）与 `poisoned`（`:365`，故意不 flush）两条路径不 flush。
- **数据流与次序（规格 §4.1、§10.1 I1）**：一条事件在网关内依次是 ① `Emit`（非阻塞，不变）→ ② 唯一写入协程里序列化成一行 JSON → ③ **追加 spool** → ④ **写 PG 元数据行**。**③ 必须早于 ④**。失败次序：③ 失败 ⇒ 该行 `body_missing=true`、定位键全 NULL、不写 spool、**不重试**；④ 沿用现有降级（批量失败逐条重试，坏记录跳过）。
- **新行不再写入正文（规格 §6、目标 G1）**：`request_headers` / `request_body` / `response_body` 对新行**恒为 NULL**；改为写 `body_ref_dt` / `body_ref_inst` / `body_ref_part` / `body_ref_row` / `body_missing` / `body_bytes`。
- **`body_bytes` 口径（规格 §6 自相矛盾，本计划裁定，登记 D42）**：取 **`len(RequestBodyRaw) + len(ResponseBodyRaw)`**，**不含 headers**（即复用既有的 `traceEventBodyBytes`）。理由：① 与既有字节闸门（`maxQueueBytes` / `maxBatchBytes`）同口径，避免「同名两个数」；② 请求头经 `scrub` 过滤且体积为 KB 级，对「压缩比核算」影响可忽略；③ 只保留**一个**计数器，不会漂移。规格 §6 的字面公式含 headers，**以本计划为准**（规格待订正）。
- **`dt` 口径（规格未定义，本计划定死）**：`dt` = 事件 `TS` 在 **UTC+8** 下的日期（`2006-01-02`）。理由：① 归档行里的 `ts` 惯例是 `+08:00`（与 `/datalog/emergency-archive` 的存量格式一致），二者必须指向同一天，否则「元数据说这天、目录在那天」；② 运维与保留期讨论全部按北京时间；③ 与存量归档脚本的口径一致。实现用**固定偏移** `time.FixedZone("CST", 8*60*60)`，**不用** `time.LoadLocation`——容器里没有 tzdata，`LoadLocation` 会失败；中国自 1991 年起无夏令时，固定 +08:00 与真实时区等价。**`dt` 选错会让正文落进错误的 `dt=` 目录 ⇒ 按 `dt` 查不到**，因此必须有边界测试（见任务 2）。
- **`inst` 来源（规格 §5.1 目录布局）**：新增环境变量 `TRACE_BODY_INST`，取值必须 ∈ {`blue`, `green`}。**非法值在启动时响亮失败**（返回 error，网关不启动）；未设置时：若功能开启 ⇒ 启动失败（没有实例标识就无法隔离写入）；若功能关闭 ⇒ 允许。
- **功能的开关语义（本计划的安全前提，必须实现）**：`TRACE_BODY_ROOT` **为空 ⇒ 功能关闭**，网关走**既有路径**（正文仍写进 PG 的 jsonb 列），行为与本计划之前**逐字节相同**。默认值为空。理由：`/datalog` 的 bind mount 属**计划 4**，而本计划的代码会随 `main` 进入部署流水线；若默认开启，在挂载到位前部署会让网关把**所有**正文标成 `body_missing` 且不再写 PG ⇒ 静默丢失全部正文。计划 4 的部署动作是「加挂载 + 设 `TRACE_BODY_ROOT`」**同时**发生。**本计划的目标状态是开启**，关闭路径只是过渡期的安全网，不得被当作验收目标。
- **降级可观测性（规格 §9.3）**：磁盘水位超限或写盘失败时，必须有明确可观测信号——闩锁跳变时**恰好一条** `ERROR` 级日志，闩锁期间按周期输出一条汇总日志，并暴露计数器（`body_missing` 行数 / 闩锁次数 / 闩锁累计秒数）。现有 `flush` 失败只有 `log.Printf`、**无重试、无告警、无指标**（`trace_event.go:151-160`），本计划一并补上。
- **协议/接口零变更**：不改任何 HTTP 路由、不改 `TraceEvent` 的字段、不改 `Emit` 的签名与语义。`tests/contract/contracts.json` 无需更新。
- **保留期**：本计划**不新增**任何 `call_traces` 淘汰逻辑（全仓 `DELETE FROM call_traces` 0 命中，规格 D1「不删除」）。
- **不碰的东西**：`AGENTS.md` 的「gateway 不要直连数据库」与现状矛盾（`main.go:260` 的 `NewTraceWriterWithLimits(traceDB, …)` + `main.go:263` 自报 `"usage tracking enabled (direct-DB mode)"`）⇒ **本计划延续现状**（只改写入的字段），**不修** `AGENTS.md`，只登记为 D46。
- **测试分层（规格 §14.1；与计划 1 同一判据）**：需要触达**未导出**标识符的测试放包内（`internal/gateway/observability/`，`package observability`）；否则一律放 `tests/gateway/observability/`（`package observability_test`，仅公开 API）。**优先后者**（`docs/guardrails.md` Q3）。因此本计划新增的网关侧类型**一律导出**（`TraceSpooler` / `SpoolGate` / `TraceBodySink` …）——它们在 `internal/` 下，导出不扩大对外 API，却让 B 层测试可构造。每个包内测试函数必须在注释里说明为何必须触达未导出符号。
- **配置读取惯例（`internal/gateway/config/config.go`）**：`v.SetDefault("<key>", <值>)` + `v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))` + `v.AutomaticEnv()`（`:140-141`）⇒ Viper key `trace_body.root` 对应的环境变量就是 **`TRACE_BODY_ROOT`**（与规格 §11.1 的名字逐字一致）。**不要**把新键放进 `observability.*`（那会得到 `OBSERVABILITY_TRACE_BODY_ROOT`，与规格不符）。
- **Atlas 迁移铁律**：迁移**不许手写时间戳**，必须 `make atlas-diff name=add_trace_body_ref` 由 GORM model 生成；`atlas.sum` 必须一并提交；`atlas migrate hash --env local` + `atlas migrate validate --env local` 必须通过。具体流程见 `atlas-db-migration` skill §0.3。
- **证据制度（本仓库 CI **完全不跑** `go test`，`.gitlab-ci.yml` 里只有三处 `CGO_ENABLED=0 go build` ⇒ 变异证据是唯一验证手段）**：本计划沿用计划 1 的工具与判据（`tools/trace-mutate/README.md`、`tools/trace-mutate/trace_mutate.py`）。要点：
  1. 变异集合**必须 git-tracked**，本计划的集合命名 `tools/trace-mutate/sets/p2-task-<N>.py`（**不能用 `task-<N>.py`**：那个命名空间已被计划 1 的 `task-2..task-8` 占用）。集合必须声明 `NAME` / `TARGET` / `PACKAGES` / `MANIFEST_FILES` / `MANIFEST_DERIVATION` / `SKIP_WHITELIST` / `MUTATIONS` / `CHECKS` / `REQUIREMENTS` / `EXPECTED_GREEN`。
  2. 每条变异跑完必须核**三断言**：顶层 `=== RUN` 名字集合 == 清单（去掉所有空白后 diff 为空）；顶层 `PASS+FAIL+SKIP` 计数 == 清单条数；**全层级** SKIP == 0。**计数不等即为无效；计数为 0 即为无效**；`[no tests to run]` 绝不能算绿。
  3. **表驱动/共享断言环的遮蔽效应**：新增一条守卫时，「整条用例转红」**不能**证明新守卫承重（环里更早的条目会遮蔽）⇒ 必须用**只含该条目的隔离探针**给出两截证据（守卫在位 ⇒ PASS；摘掉 ⇒ FAIL **于该条目**）。
  4. **两类证据分开记**：变异（行为）与静态检查（结构不变量）。变异测不到的要求（例如「网关侧不读 `TRACE_BODY_ZSTD_*`」、「`body_missing` 列名」）必须有 `CHECKS` 行，写明命令与真实输出。
  5. **证据绑定提交**：工具的头部会打印 `git rev-parse HEAD` / `git status --porcelain`（脏树拒绝运行）/ 工具与集合与清单的 sha256 / 被测文件 sha256 ⇒ 汇报时必须原样引用。
  6. `FAIL` 之外还要区分**崩溃/超时红**（编译失败、panic、30s 超时）与**断言红**，两者在汇报里分开计数。
- **本计划登记的规格缺陷（编号自成一套，从 D42 起；计划 1 用到 D41）**：见文末「规格缺陷登记」一节。**执行者不得因为「规格这么写」而按缺陷描述实现**——每条缺陷都给了本计划的裁定。

---

## 文件结构

| 文件 | 职责 | 规模估计 |
|---|---|---|
| `internal/observability/model/call_trace.go` | **修改**：`CallTrace` 增加 6 个新列（4 个定位键 + `body_missing` + `body_bytes`） | +18 行 |
| `atlas-migrations/<timestamp>_add_trace_body_ref.sql` | **新建**（由 `make atlas-diff` 生成，不许手写）：`ALTER TABLE call_traces ADD COLUMN` × 6 | ~10 行 |
| `atlas-migrations/atlas.sum` | **修改**：`atlas migrate hash` 产出 | — |
| `internal/gateway/config/trace_body.go` | **新建**：`TraceBodyConfig` + `LoadTraceBodyConfig` + `TraceBodyZone` + `DayOf` + 全部校验 | ~150 行 |
| `internal/gateway/config/config.go` | **修改**：新增 `trace_body.*` 默认值 + `Config.TraceBody` 字段 | +25 行 |
| `.env.example` | **修改**：补 `TRACE_BODY_*` 模板（默认全部注释掉/留空 ⇒ 功能关闭） | +8 行 |
| `internal/gateway/observability/trace_line.go` | **新建**：`traceEventLine`（一整行 JSON，键序对齐归档格式） | ~110 行 |
| `internal/gateway/observability/spool_gate.go` | **新建**：`SpoolGate`（磁盘水位 + 写失败闩锁 + 探针恢复） | ~190 行 |
| `internal/gateway/observability/spooler.go` | **新建**：`TraceSpooler`（`TraceBodySink` 实现：周期 flush + 滚动 + 统计） | ~230 行 |
| `internal/gateway/observability/trace_event.go` | **修改**：`TraceWriter` 增 `sink`/`dayOf` 两个可注入字段；`flush` 闭包改为「先 spool 后 PG」；`traceEventToModel` 区分开启/关闭两条路径 | +90/-25 行 |
| `cmd/gateway/main.go` | **修改**：按配置构造 `TraceSpooler` 并接线；关闭时保持既有行为 | +40 行 |
| `tests/gateway/config/trace_body_test.go` | **B 层**：默认值 / env 覆盖 / 非法值 / `DayOf` 边界 | ~180 行 |
| `tests/gateway/observability/trace_line_test.go` | **B 层**：序列化逐字节断言（键序、`+08:00`、无裸换行、scrub/包装规则） | ~200 行 |
| `tests/gateway/observability/spool_gate_test.go` | **B 层**：水位闩锁、写失败闩锁、闩锁期不写盘、探针恢复、无真实盘满 | ~200 行 |
| `tests/gateway/observability/spooler_test.go` | **B 层**：flush 可见性、按大小滚动、按时间滚动、**空块不滚动**、`dt` 跨界换块 | ~280 行 |
| `tests/gateway/observability/trace_event_spool_test.go` | **B 层**：**次序（先 spool 后 PG）**、降级列、旧路径回归 | ~260 行 |
| `tests/gateway/observability/trace_degradation_test.go` | **B 层**：`body_missing` 计数器、闩锁日志恰好一条、汇总日志节流 | ~150 行 |
| `tests/gateway/observability/trace_body_acceptance_test.go` | **B 层**：A1 写入半部 + A3 注入式退化 | ~220 行 |
| `tools/trace-mutate/sets/p2-task-2.py` … `p2-task-8.py` | **新建**（git-tracked）：各任务的变异集合（见各任务末步） | 每个 ~120-260 行 |

**每个文件单一职责。** `trace_line.go` 是纯函数（无 IO，最易测）；`spool_gate.go` 只做「能不能写」的判定，不碰具体文件；`spooler.go` 只做 flush/滚动；`trace_event.go` 只做「按次序调用它们并组装 PG 行」。**不要把三个部件合并成一个文件**——它们的失败面完全不同（序列化 = 纯函数；闩锁 = 状态机；滚动 = 与 sealer 的时间契约）。

---

### Task 1：PG schema —— `call_traces` 定位键 6 列 + Atlas 迁移

**文件：**
- 修改：`internal/observability/model/call_trace.go`（现有 33 行，`TableName()` 在 `:33`）
- 新建（**由工具生成，不许手写**）：`atlas-migrations/<timestamp>_add_trace_body_ref.sql`
- 修改：`atlas-migrations/atlas.sum`（`atlas migrate hash` 产出）
- 测试：`tests/gateway/observability/trace_body_columns_test.go`（B 层，sqlite 往返）

**接口：**
- 依赖输入：无（本计划第一个任务）
- 对外产出：`obsmodel.CallTrace` 的 6 个新字段（**后续任务的 `traceEventToModel` 直接按这些字段名赋值**）：
  ```go
  BodyRefDT     *string `gorm:"column:body_ref_dt;type:date"          json:"body_ref_dt,omitempty"`
  BodyRefInst   *string `gorm:"column:body_ref_inst;type:text"        json:"body_ref_inst,omitempty"`
  BodyRefPart   *int    `gorm:"column:body_ref_part;type:integer"     json:"body_ref_part,omitempty"`
  BodyRefRow    *int    `gorm:"column:body_ref_row;type:integer"      json:"body_ref_row,omitempty"`
  BodyMissing   bool    `gorm:"column:body_missing;not null;default:false" json:"body_missing"`
  BodyBytes     *int    `gorm:"column:body_bytes;type:integer"        json:"body_bytes,omitempty"`
  ```
  **全部用指针（`BodyMissing` 除外）**：NULL 与零值必须可区分——`body_ref_part = 0` 是**合法**的 part 序号，若用 `int` 则「没有定位键」与「part 0」无法区分。

- [ ] **步骤 1：改 GORM model**

在 `internal/observability/model/call_trace.go` 的 `ErrorMessage` 之后、`}` 之前插入：

```go
	// 正文定位键（规格 §6）：正文不再写进本表的 jsonb 列，而是落 /datalog 上的 spool，
	// 这里只记录「去哪儿找」。四个分量在写入追加时即可确定，无需封块后回填。
	//
	// 【为什么全用指针】NULL 与零值必须可区分：part 0 是合法序号，
	// 若 BodyRefPart 用 int，则「正文缺失（无定位键）」与「part 0 的第 0 行」同名。
	BodyRefDT   *string `gorm:"column:body_ref_dt;type:date" json:"body_ref_dt,omitempty"`
	BodyRefInst *string `gorm:"column:body_ref_inst;type:text" json:"body_ref_inst,omitempty"`
	BodyRefPart *int    `gorm:"column:body_ref_part;type:integer" json:"body_ref_part,omitempty"`
	BodyRefRow  *int    `gorm:"column:body_ref_row;type:integer" json:"body_ref_row,omitempty"`

	// BodyMissing 正文在写入时即失败（盘满/不可写），从未落盘（规格 §9.1）。
	// NOT NULL DEFAULT false：历史行与「已成功落盘」的行都为 false。
	BodyMissing bool `gorm:"column:body_missing;not null;default:false" json:"body_missing"`

	// BodyBytes 原始正文字节数 = len(request_body) + len(response_body)，**不含 headers**
	//（口径裁定见计划 D42；用途是审计与压缩比核算，不参与业务逻辑）。
	BodyBytes *int `gorm:"column:body_bytes;type:integer" json:"body_bytes,omitempty"`
```

- [ ] **步骤 2：本地确认 model 能编译**

运行：`go build ./internal/observability/...`
预期：无输出、退出码 0

- [ ] **步骤 3：生成迁移（先读 skill §0.3，禁止手写 SQL）**

运行：`make atlas-diff name=add_trace_body_ref`
预期：`atlas-migrations/` 下出现一个新的 `<timestamp>_add_trace_body_ref.sql`，内容形如：

```sql
-- Add column "body_ref_dt" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_ref_dt" date NULL;
-- Add column "body_ref_inst" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_ref_inst" text NULL;
-- Add column "body_ref_part" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_ref_part" integer NULL;
-- Add column "body_ref_row" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_ref_row" integer NULL;
-- Add column "body_missing" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_missing" boolean DEFAULT false NOT NULL;
-- Add column "body_bytes" to table: "call_traces"
ALTER TABLE "call_traces" ADD COLUMN "body_bytes" integer NULL;
```

**若生成结果里出现任何 `DROP COLUMN` / `DROP INDEX` / 与上述 6 行无关的语句 ⇒ 立刻停止，不要提交**，回到步骤 1 检查是否误删了既有字段或 tag（`atlas-db-migration` skill §4 问题 1）。

- [ ] **步骤 4：人工审查 + 重算校验和 + 校验**

运行：
```bash
cat atlas-migrations/*_add_trace_body_ref.sql
atlas migrate hash --env local
atlas migrate validate --env local
```
预期：SQL 恰为上述 6 个 `ADD COLUMN`（`atlas.sum` 被更新）；`validate` 无输出、退出码 0。

- [ ] **步骤 5：写失败的测试（列存在且可往返）**

新建 `tests/gateway/observability/trace_body_columns_test.go`：

```go
package observability_test

import (
	"strings"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCallTraceBodyRefColumnsRoundTrip 固定规格 §6 的 6 个新列：
// 四个定位键必须能存 NULL 与 0（不能把「没有定位键」和「part 0」混为一谈），
// body_missing 必须 NOT NULL DEFAULT false，body_bytes 可空。
func TestCallTraceBodyRefColumnsRoundTrip(t *testing.T) {
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))

	part, row, bytes := 0, 0, 4096
	dt, inst := "2026-09-24", "blue"
	in := obsmodel.CallTrace{
		RequestID:   "req-1",
		TS:          time.Now(),
		BodyRefDT:   &dt,
		BodyRefInst: &inst,
		BodyRefPart: &part,
		BodyRefRow:  &row,
		BodyMissing: false,
		BodyBytes:   &bytes,
	}
	require.NoError(t, db.Create(&in).Error)

	var out obsmodel.CallTrace
	require.NoError(t, db.First(&out, "request_id = ?", "req-1").Error)
	require.NotNil(t, out.BodyRefPart)
	require.Equal(t, 0, *out.BodyRefPart, "part 0 必须读出 0，而不是 NULL")
	require.NotNil(t, out.BodyRefDT)
	require.Equal(t, dt, *out.BodyRefDT)

	// 没有定位键的行：四个指针全 nil，body_missing 为 false（默认值）
	require.NoError(t, db.Create(&obsmodel.CallTrace{RequestID: "req-2", TS: time.Now()}).Error)
	var none obsmodel.CallTrace
	require.NoError(t, db.First(&none, "request_id = ?", "req-2").Error)
	require.Nil(t, none.BodyRefDT)
	require.Nil(t, none.BodyRefPart)
	require.False(t, none.BodyMissing)
	require.Nil(t, none.BodyBytes)
}
```

- [ ] **步骤 6：跑测试并确认它失败（此时列还没进 sqlite？——不，AutoMigrate 会照 struct 建表）**

运行：`go test ./tests/gateway/observability/ -run 'TestCallTraceBodyRefColumnsRoundTrip' -v`
预期：**PASS**。本任务的「失败」证据不在这一步，而在**步骤 7 的 SQL 静态断言**——因为 `AutoMigrate` 从 struct 建表，struct 改了它必然有列，这个测试固定的是**指针语义**（NULL vs 0），不是迁移文件。若它 FAIL，说明指针类型或 tag 写错。

- [ ] **步骤 7：写失败的测试（迁移文件本身的内容）**

在同一个文件追加：

```go
// TestAddTraceBodyRefMigrationContent 固定迁移文件的**内容**：
// 恰好 6 个 ADD COLUMN，且**不含任何 DROP**（规格 §6 要求「保留列、不删」，
// 破坏性语句一旦混进来就是数据丢失）。
func TestAddTraceBodyRefMigrationContent(t *testing.T) {
	matches, err := filepath.Glob("../../../atlas-migrations/*_add_trace_body_ref.sql")
	require.NoError(t, err)
	require.Len(t, matches, 1, "必须恰好有一个 add_trace_body_ref 迁移文件")

	raw, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	sql := string(raw)

	want := []string{
		`ADD COLUMN "body_ref_dt" date`,
		`ADD COLUMN "body_ref_inst" text`,
		`ADD COLUMN "body_ref_part" integer`,
		`ADD COLUMN "body_ref_row" integer`,
		`ADD COLUMN "body_missing" boolean DEFAULT false NOT NULL`,
		`ADD COLUMN "body_bytes" integer`,
	}
	for _, w := range want {
		require.Contains(t, sql, w)
	}
	require.NotContains(t, sql, "DROP", "迁移里不得出现任何 DROP（规格 §6：保留既有列）")
	require.Equal(t, 6, strings.Count(sql, "ALTER TABLE"), "只应有 6 条 ALTER TABLE")
}
```

（该文件顶部需补 `"os"`、`"path/filepath"` 两个 import。）

- [ ] **步骤 8：跑测试并确认它失败**

把步骤 7 的 `want` 里任一条改动一个字（例如把 `date` 改成 `timestamp`）后运行：
`go test ./tests/gateway/observability/ -run 'TestAddTraceBodyRefMigrationContent' -v`
预期：**FAIL**，提示 `Should contain: "ADD COLUMN \"body_ref_dt\" date"`。**改回**后再跑，预期 PASS。这一步证明该断言真的在字符串级别盯着迁移内容（不是恒真）。

- [ ] **步骤 9：提交**

```bash
git add internal/observability/model/call_trace.go atlas-migrations/ tests/gateway/observability/trace_body_columns_test.go
git commit -m "feat(db): call_traces 增加正文定位键 6 列（计划 2/4 任务 1）

body_ref_dt/inst/part/row + body_missing + body_bytes，走 atlas migrate diff 生成。
规格 §6：保留既有三个 jsonb 列不删，新行恒为 NULL；不加新索引。
body_bytes 口径取 len(request_body)+len(response_body)（不含 headers，见计划 D42）。"
```

---

### Task 2：`TRACE_BODY_*` 配置解析与 `dt` 口径

**文件：**
- 新建：`internal/gateway/config/trace_body.go`
- 修改：`internal/gateway/config/config.go`（默认值块约 `:95-110`、`Config` 结构体与其装配处约 `:175-186`）
- 修改：`.env.example`（`OBSERVABILITY_*` 那几行之后，约 `:37-40`）
- 测试：`tests/gateway/config/trace_body_test.go`（B 层）

**接口：**
- 依赖输入：`*viper.Viper`（由 `config.Load()` 内部构造，见 `config.go:140-141`）
- 对外产出（后续任务直接使用）：
  ```go
  type TraceBodyConfig struct {
      Enabled              bool          // 由 Root != "" 推导，**不单独配置**
      Root                 string        // TRACE_BODY_ROOT，如 /datalog/trace-archive-v4
      Inst                 string        // TRACE_BODY_INST，只能是 "blue" / "green"
      BlockMB              int64         // TRACE_BODY_BLOCK_MB，默认 1536
      FlushInterval        time.Duration // TRACE_BODY_FLUSH_INTERVAL，默认 1m
      RollInterval         time.Duration // TRACE_BODY_SEAL_INTERVAL，默认 1h（= 滚动间隔）
      DiskWatermarkPercent int           // TRACE_BODY_DISK_WATERMARK_PERCENT，默认 90
  }
  func LoadTraceBodyConfig(v *viper.Viper) (TraceBodyConfig, error)
  func (c TraceBodyConfig) DayOf(ts time.Time) string   // dt 口径：UTC+8 的日期
  var TraceBodyZone = time.FixedZone("CST", 8*60*60)
  ```

- [ ] **步骤 1：写失败的测试**

新建 `tests/gateway/config/trace_body_test.go`：

```go
package config_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func newTraceViper() *viper.Viper {
	v := viper.New()
	v.SetDefault("trace_body.block_mb", int64(1536))
	v.SetDefault("trace_body.flush_interval", "1m")
	v.SetDefault("trace_body.seal_interval", "1h")
	v.SetDefault("trace_body.disk_watermark_percent", 90)
	return v
}

// TestTraceBodyConfigDisabledByDefault 是**安全前提**的测试：
// 未设 ROOT ⇒ 功能关闭 ⇒ 网关走既有路径（正文仍写 PG）。
// 若把默认值改成开启，挂载到位前的部署会静默丢掉所有正文。
func TestTraceBodyConfigDisabledByDefault(t *testing.T) {
	cfg, err := config.LoadTraceBodyConfig(newTraceViper())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Empty(t, cfg.Root)
	require.Equal(t, int64(1536), cfg.BlockMB)
	require.Equal(t, time.Minute, cfg.FlushInterval)
	require.Equal(t, time.Hour, cfg.RollInterval)
	require.Equal(t, 90, cfg.DiskWatermarkPercent)
}

// TestTraceBodyConfigRollIntervalMustBeUnderOneHour 固定 R8 的上界与
// 「严格小于 sealer 的 minAge」这一条：1h 是默认值，配置允许更小但不允许更大。
func TestTraceBodyConfigRollIntervalMustBeUnderOneHour(t *testing.T) {
	v := newTraceViper()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	v.Set("trace_body.inst", "blue")
	v.Set("trace_body.seal_interval", "2h")
	_, err := config.LoadTraceBodyConfig(v)
	require.Error(t, err, "滚动间隔 > 1h 必须被拒绝（规格 §15.1 R8）")

	v.Set("trace_body.seal_interval", "3h")
	_, err = config.LoadTraceBodyConfig(v)
	require.Error(t, err, "滚动间隔 >= minAge(3h) 必须被拒绝")
}

// TestTraceBodyConfigInstIsWhitelisted 固定 §5.1 的目录布局：inst ∈ {blue,green}。
func TestTraceBodyConfigInstIsWhitelisted(t *testing.T) {
	v := newTraceViper()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	for _, bad := range []string{"", "red", "Blue", "blue/../green"} {
		v.Set("trace_body.inst", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "inst=%q 必须被拒绝", bad)
	}
	v.Set("trace_body.inst", "green")
	cfg, err := config.LoadTraceBodyConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, "green", cfg.Inst)
}

// TestDayOfUsesUTCPlus8 pin 死 dt 口径。生产库 TimeZone=UTC、容器无 TZ，
// 因此时区**不能**依赖 time.Local；固定 +08:00 且跨日边界必须正确。
func TestDayOfUsesUTCPlus8(t *testing.T) {
	cfg := config.TraceBodyConfig{Root: "/x", Inst: "blue"}
	cases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"北京 23:59:59 仍属当天", time.Date(2026, 9, 24, 23, 59, 59, 0, config.TraceBodyZone), "2026-09-24"},
		{"北京 00:00:00 进入次日", time.Date(2026, 9, 25, 0, 0, 0, 0, config.TraceBodyZone), "2026-09-25"},
		{"UTC 16:00 已是北京次日", time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC), "2026-09-25"},
		{"UTC 15:59:59 仍是北京当天", time.Date(2026, 9, 24, 15, 59, 59, 0, time.UTC), "2026-09-24"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, cfg.DayOf(c.ts))
		})
	}
}
```

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./tests/gateway/config/ -run 'TestTraceBodyConfig|TestDayOfUsesUTCPlus8' -v`
预期：**编译失败**，`undefined: config.LoadTraceBodyConfig`、`undefined: config.TraceBodyZone`。

- [ ] **步骤 3：写最小实现**

新建 `internal/gateway/config/trace_body.go`：

```go
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// TraceBodyZone 是 dt 口径使用的时区：固定 UTC+8。
//
// 【为什么不用 time.LoadLocation("Asia/Shanghai")】网关容器里没有 tzdata，
// LoadLocation 会返回错误；而中国自 1991 年起不再使用夏令时，
// 固定 +08:00 与真实时区在任何我们关心的日期上都等价。
//
// 【为什么不用 time.Local】生产 compose 未设 TZ（容器是 UTC），
// 依赖 time.Local 会让 dt 随宿主机/容器环境漂移 —— 而 dt 是**目录名**，
// 漂移会让同一 request_id 的正文落在另一个 dt 下，按 dt 查不到。
var TraceBodyZone = time.FixedZone("CST", 8*60*60)

// traceBodyMaxRollInterval 是滚动间隔的硬上界（规格 §15.1 R8：≤ 1h）。
const traceBodyMaxRollInterval = time.Hour

// traceBodyMinAge 是 sealer 侧 minAge 的镜像（规格 §7.2：3h）。
// 滚动间隔必须**严格小于**它：sealer 只靠 mtime + minAge 判断「已停止追加」，
// 网关若在 sealer 已封块并删除 spool 之后仍持有同一 part 的 fd，
// 追加会写进已被 unlink 的 inode —— 落盘成功但永久不可达（D7）。
const traceBodyMinAge = 3 * time.Hour

// TraceBodyConfig 是正文直写 /datalog 的网关侧配置（规格 §11.1）。
type TraceBodyConfig struct {
	// Enabled 由 Root != "" 推导，没有独立开关：一个「开着但没有根目录」的
	// 状态没有意义，多一个布尔量就多一种自相矛盾的组合。
	Enabled bool

	Root                 string
	Inst                 string
	BlockMB              int64
	FlushInterval        time.Duration
	RollInterval         time.Duration
	DiskWatermarkPercent int
}

// LoadTraceBodyConfig 从 viper 读取 trace_body.* 配置并做完整校验。
//
// 环境变量名与规格 §11.1 逐字一致（viper 的 "." → "_" 替换规则给出
// trace_body.root → TRACE_BODY_ROOT）：不要把这些键放进 observability.*，
// 那会得到 OBSERVABILITY_TRACE_BODY_ROOT，与部署文档不符。
//
// 校验策略分两类（这个区分是刻意的）：
//   - 「字段本身非法」（inst 不在白名单、间隔为负、位数为 0）⇒ **一律报错**，
//     无论功能是否开启。因为这类值一定是配置错误，静默接受只会掩盖问题。
//   - 「功能开启所必需但可以缺省」（inst 为空、root 为空）⇒ 关闭时允许、
//     开启时必须报错。root 为空是**关闭**的表达方式，不是错误。
func LoadTraceBodyConfig(v *viper.Viper) (TraceBodyConfig, error) {
	cfg := TraceBodyConfig{
		Root:                 v.GetString("trace_body.root"),
		Inst:                 v.GetString("trace_body.inst"),
		BlockMB:              v.GetInt64("trace_body.block_mb"),
		FlushInterval:        v.GetDuration("trace_body.flush_interval"),
		RollInterval:         v.GetDuration("trace_body.seal_interval"),
		DiskWatermarkPercent: v.GetInt("trace_body.disk_watermark_percent"),
	}
	cfg.Enabled = cfg.Root != ""

	if cfg.Inst != "" && cfg.Inst != "blue" && cfg.Inst != "green" {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.inst 只能是 blue 或 green，实际 %q（规格 §5.1 的目录布局要求实例隔离）", cfg.Inst)
	}
	if cfg.Enabled && cfg.Inst == "" {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.root 已设置（功能开启）但 trace_body.inst 为空：没有实例标识就无法隔离写入")
	}
	if cfg.BlockMB <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.block_mb 必须为正，实际 %d", cfg.BlockMB)
	}
	if cfg.FlushInterval <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.flush_interval 必须为正，实际 %s", cfg.FlushInterval)
	}
	if cfg.RollInterval <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.seal_interval 必须为正，实际 %s", cfg.RollInterval)
	}
	if cfg.RollInterval > traceBodyMaxRollInterval {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.seal_interval=%s 超过 1h 上界（规格 §15.1 R8：滚动间隔 ≤ 1h）；"+
				"滚动过慢会让 sealer 在网关仍持有 fd 时封块并删除 spool", cfg.RollInterval)
	}
	if cfg.RollInterval >= traceBodyMinAge {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.seal_interval=%s 必须严格小于 sealer 的 minAge=%s（规格 §7.2）", cfg.RollInterval, traceBodyMinAge)
	}
	if cfg.FlushInterval >= cfg.RollInterval {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.flush_interval=%s 必须远小于滚动间隔=%s（规格 §7.1：sealer 只能看到已 write(2) 的字节）",
			cfg.FlushInterval, cfg.RollInterval)
	}
	if cfg.DiskWatermarkPercent < 1 || cfg.DiskWatermarkPercent > 99 {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.disk_watermark_percent 必须在 1..99，实际 %d（0 会让每个请求都降级，100 等于不设水位）",
			cfg.DiskWatermarkPercent)
	}
	return cfg, nil
}

// DayOf 返回事件归属的 dt（目录名分量）。
//
// 口径固定为「TS 在 UTC+8 下的日期」，理由见 TraceBodyZone 与规格 §5.1：
// 归档行里的 ts 惯例是 +08:00，dt 必须与它指向同一天。
func (c TraceBodyConfig) DayOf(ts time.Time) string {
	return ts.In(TraceBodyZone).Format("2006-01-02")
}
```

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./tests/gateway/config/ -run 'TestTraceBodyConfig|TestDayOfUsesUTCPlus8' -v`
预期：PASS（4 个顶层用例，`TestDayOfUsesUTCPlus8` 下 4 个子用例）。

- [ ] **步骤 5：接进 `config.go`（默认值 + 结构体字段）**

在 `internal/gateway/config/config.go` 的 `v.SetDefault("observability.trace_max_base64_len", 64)` 之后插入：

```go
	// 正文直写 /datalog（规格 §11.1）。root 默认为**空** ⇒ 功能关闭，
	// 网关走既有路径（正文仍写 PG）。计划 4 的部署动作是
	// 「加 /datalog 挂载 + 设 TRACE_BODY_ROOT」同时发生。
	v.SetDefault("trace_body.root", "")
	v.SetDefault("trace_body.inst", "")
	v.SetDefault("trace_body.block_mb", int64(1536))
	v.SetDefault("trace_body.flush_interval", "1m")
	v.SetDefault("trace_body.seal_interval", "1h")
	v.SetDefault("trace_body.disk_watermark_percent", 90)
```

在 `Config` 结构体里（`Observability ObservabilityConfig` 之后）加字段：

```go
	TraceBody TraceBodyConfig
```

在装配 `Observability: ObservabilityConfig{…}` 的那个复合字面量之后，加：

```go
		TraceBody: func() TraceBodyConfig {
			tb, err := LoadTraceBodyConfig(v)
			if err != nil {
				// 配置错误必须在启动时响亮失败，而不是让网关带着非法 inst 跑起来
				// （规格 §5.1：inst 是路径分量，非法值会让正文落到错误的目录）。
				panic(fmt.Errorf("trace_body 配置非法: %w", err))
			}
			return tb
		}(),
```

**说明**：`config.Load()` 现有签名不返回 error（`config.go` 里只有 `panic`/`log.Printf` 级的失败），因此这里用 `panic` 与既有风格一致；**不要**改 `Load()` 的签名（会波及全仓调用点，超出本计划范围）。`"fmt"` 若未 import 需补上。

- [ ] **步骤 6：确认整个 config 包与网关能编译**

运行：
```bash
go build ./internal/gateway/config/ ./cmd/gateway/
go test ./tests/gateway/config/ -count=1
```
预期：两条都无输出/退出码 0（第二条会跑既有配置测试，必须**全绿**——本步骤同时是「没有破坏既有配置行为」的回归）。

- [ ] **步骤 7：补 `.env.example`（默认关闭）**

在 `.env.example` 的 `OBSERVABILITY_BILLABLE_RESERVATION_TTL=10m` 之后追加：

```bash
# ── 调用链正文直写 /datalog（计划 2/4；规格 docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md）──
# 留空 = 功能关闭，正文仍写进 call_traces 的 jsonb 列（默认值，安全）。
# 开启必须与 /datalog 的 bind mount 同时生效，否则所有正文会退化为 body_missing。
# TRACE_BODY_ROOT=/datalog/trace-archive-v4
# TRACE_BODY_INST=blue                 # 只能是 blue / green，由部署脚本按当前环境注入
# TRACE_BODY_BLOCK_MB=1536             # 块大小阈值（规格 §5.3.3）
# TRACE_BODY_FLUSH_INTERVAL=1m         # 周期 flush（规格 §7.1：必须 ≤1m 且远小于滚动间隔）
# TRACE_BODY_SEAL_INTERVAL=1h          # 滚动间隔（规格 §15.1 R8：≤1h 且严格小于 minAge 3h）
# TRACE_BODY_DISK_WATERMARK_PERCENT=90 # 磁盘水位（本计划新增，登记 D45）
```

- [ ] **步骤 8：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-2.py`（**必须 git-tracked**；命名理由见全局约束）：

```python
"""计划 2 / 任务 2 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见 D17）。

被变异文件是网关侧配置校验。这一批变异的共同主题：
**每一条校验都必须能被测出来**——删掉它之后，对应用例必须转红。
"""

NAME = "p2-task-2"
TARGET = "internal/gateway/config/trace_body.go"
PACKAGES = ["./tests/gateway/config/"]
MANIFEST_FILES = ["tests/gateway/config/trace_body_test.go"]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/config/trace_body_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []

MUTATIONS = {}

# inst 白名单：放宽成「非空即可」⇒ TestTraceBodyConfigInstIsWhitelisted 必须红
MUTATIONS["m01_inst白名单放宽为非空"] = [
    ("sub", 'if cfg.Inst != "" && cfg.Inst != "blue" && cfg.Inst != "green" {',
            'if false {'),
]

# 滚动间隔上界（≤1h）⇒ TestTraceBodyConfigRollIntervalMustBeUnderOneHour 必须红
MUTATIONS["m02_删除滚动间隔1h上界"] = [
    ("sub", "if cfg.RollInterval > traceBodyMaxRollInterval {", "if false {"),
]

# 「滚动间隔必须严格小于 minAge」⇒ 同上用例必须红
MUTATIONS["m03_删除minAge严格小于校验"] = [
    ("sub", "if cfg.RollInterval >= traceBodyMinAge {", "if false {"),
]

# flush 必须远小于滚动间隔（规格 §7.1）⇒ 需要一条对应用例
MUTATIONS["m04_删除flush与滚动间隔的关系校验"] = [
    ("sub", "if cfg.FlushInterval >= cfg.RollInterval {", "if false {"),
]

# 水位范围 1..99 ⇒ 需要一条对应用例（0 会让每个请求都降级）
MUTATIONS["m05_删除水位范围校验"] = [
    ("sub", "if cfg.DiskWatermarkPercent < 1 || cfg.DiskWatermarkPercent > 99 {", "if false {"),
]

# dt 口径：把固定 +08:00 换成 time.Local ⇒ TestDayOfUsesUTCPlus8 必须红
MUTATIONS["m06_dt口径改用time.Local"] = [
    ("sub", 'return ts.In(TraceBodyZone).Format("2006-01-02")',
            'return ts.In(time.Local).Format("2006-01-02")'),
]

CHECKS = [
    {
        "name": "doc-默认关闭",
        "file": ".env.example",
        "pattern": r"(?m)^# TRACE_BODY_ROOT=",
        "expect": "present",
        "why": (
            "规格 §11.1 与计划全局约束要求「留空 = 关闭」是**默认值**；"
            "变异测不到「默认值是什么」，只能静态检查模板里确实写成了注释态。"
        ),
    },
]

REQUIREMENTS = [
    {"plan": "计划 2 任务 2 步骤 1：inst ∈ {blue,green}，非法值启动即失败",
     "mutations": ["m01_inst白名单放宽为非空"], "checks": []},
    {"plan": "计划 2 任务 2 步骤 1：滚动间隔 ≤ 1h 且严格小于 minAge 3h（规格 §15.1 R8 / §7.2）",
     "mutations": ["m02_删除滚动间隔1h上界", "m03_删除minAge严格小于校验"], "checks": []},
    {"plan": "计划 2 任务 2 步骤 1：flush 周期必须远小于滚动间隔（规格 §7.1）",
     "mutations": ["m04_删除flush与滚动间隔的关系校验"], "checks": []},
    {"plan": "计划 2 任务 2 步骤 1：水位 1..99（登记 D45）",
     "mutations": ["m05_删除水位范围校验"], "checks": []},
    {"plan": "计划 2 任务 2 步骤 1 + 全局约束：dt = TS 在 UTC+8 下的日期",
     "mutations": ["m06_dt口径改用time.Local"], "checks": []},
    {"plan": "计划 2 全局约束：TRACE_BODY_ROOT 留空 = 功能关闭（默认值）",
     "mutations": [], "checks": ["doc-默认关闭"]},
]

EXPECTED_GREEN = {}
```

**注意**：`m04` / `m05` 要求测试里有对应的**隔离探针**用例（只含该条校验的输入），否则会因「环里更早的条目先失败」而无法证明新守卫承重（全局约束的证据制度第 3 条）。因此步骤 1 的测试文件在实现时需**为每条校验各加 1 个独立用例**：

```go
func TestTraceBodyConfigFlushMustBeFarBelowRoll(t *testing.T) { /* flush=1h, roll=1h ⇒ error */ }
func TestTraceBodyConfigWatermarkRange(t *testing.T)         { /* 0 与 100 都必须 error */ }
```

- [ ] **步骤 9：跑变异集合（`--check` 先验定位唯一）**

运行：
```bash
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest /tmp/p2-task-2-names.txt \
  --mutation-set tools/trace-mutate/sets/p2-task-2.py --check
```
（`/tmp/p2-task-2-names.txt` 由 `MANIFEST_DERIVATION` 那条 grep 命令产出）
预期：`--check` 对 6 条变异全部报「唯一定位」。**若某条报定位到 0 处或多处 ⇒ 修正变异字符串（不要改测试）**。

再跑权威表：
```bash
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest /tmp/p2-task-2-names.txt \
  --mutation-set tools/trace-mutate/sets/p2-task-2.py
```
预期：退出码 0；6 条全部**断言支撑的红**；三断言全过（`names==ran==清单`、顶层 PASS+FAIL+SKIP == 清单条数、全层级 SKIP == 0）；`未登记的绿` 为空；静态检查 PASS。

- [ ] **步骤 10：提交**

```bash
git add internal/gateway/config/trace_body.go internal/gateway/config/config.go .env.example \
        tests/gateway/config/trace_body_test.go tools/trace-mutate/sets/p2-task-2.py
git commit -m "feat(gateway): TRACE_BODY_* 配置解析与 dt 口径（计划 2/4 任务 2）

- Viper key 用顶层 trace_body.*，环境变量名与规格 §11.1 逐字一致
- root 留空 = 功能关闭（默认），避免挂载到位前部署静默丢正文
- inst 白名单 blue/green；滚动间隔 ≤1h 且严格小于 minAge 3h；flush 远小于滚动间隔
- dt = TS 在 UTC+8 下的日期，用固定偏移而非 LoadLocation（容器无 tzdata）"
```

---

### Task 3：spool 行序列化（`traceEventLine`）

**文件：**
- 新建：`internal/gateway/observability/trace_line.go`
- 测试：`tests/gateway/observability/trace_line_test.go`（B 层）

**接口：**
- 依赖输入：`TraceEvent`（`internal/gateway/observability/trace_event.go:17-41`，字段不变）、`TraceBodyZone`（任务 2 产出）
- 对外产出：
  ```go
  func traceEventLine(e TraceEvent) []byte   // 一整行 JSON，**不含**结尾换行
  ```
  **不导出**：它是包内实现细节，B 层测试通过 `TraceWriter` 的落盘结果间接观察（见任务 6）；本任务若要直接测它，用**包内**测试文件 `internal/gateway/observability/trace_line_internal_test.go`（`package observability`），并在每个测试函数注释里写明「为什么必须触达未导出的 `traceEventLine`」。

  **键序与既有归档格式一致（规格 §4.1 步骤①）**：`request_id`, `ts`, `request_headers`, `request_body`, `response_body`, `error_code`, `error_message`。

- [ ] **步骤 1：写失败的测试（包内，因为要直接调未导出函数）**

新建 `internal/gateway/observability/trace_line_internal_test.go`：

```go
package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 本文件是 A 层（包内）：被断言的 traceEventLine 是未导出函数，
// package observability_test 无法访问。分成 A 层的必要性在「逐字节键序」这一条上最强：
// 键序是对外格式契约，用 json.Unmarshal 到 map 断言会丢掉顺序信息。
//
// 测试用的时区固定为 UTC+8，与生产 dt/ts 口径一致。

func fixedEvent() TraceEvent {
	return TraceEvent{
		RequestID:         "req-1",
		TS:                time.Date(2026, 9, 24, 12, 0, 0, 0, TraceBodyZoneForTest),
		RequestHeadersRaw: []byte(`{"x-a":"1"}`),
		RequestBodyRaw:    []byte(`{"model":"gpt"}`),
		ResponseBodyRaw:   []byte(`{"ok":true}`),
	}
}

// TestTraceEventLineKeyOrderAndTSFormat 固定对外格式：
// 键序与归档格式一致，ts 必须是 +08:00，且整行不含裸换行
//（裸换行会让 Writer.Append 的 validateLine 拒绝该行 —— 那是承重不变量。
//  「一次 Append == 一条物理行」的入口侧见 internal/tracearchive/writer.go:101-110）。
func TestTraceEventLineKeyOrderAndTSFormat(t *testing.T) {
	line := traceEventLine(fixedEvent())

	wantOrder := []string{"request_id", "ts", "request_headers", "request_body", "response_body", "error_code", "error_message"}
	got := keyOrder(t, line)
	if len(got) != len(wantOrder) {
		t.Fatalf("键数不符：want %v got %v", wantOrder, got)
	}
	for i := range wantOrder {
		if got[i] != wantOrder[i] {
			t.Fatalf("键序不符：want %v got %v", wantOrder, got)
		}
	}

	if bytes.ContainsAny(line, "\n\r") {
		t.Fatalf("行内不得有裸换行：%q", line)
	}

	var probe struct {
		TS string `json:"ts"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("必须是一行合法 JSON：%v", err)
	}
	if !strings.HasSuffix(probe.TS, "+08:00") {
		t.Fatalf("ts 必须以 +08:00 结尾，实际 %q", probe.TS)
	}
	if probe.TS != "2026-09-24T12:00:00+08:00" {
		t.Fatalf("ts 格式不符：%q", probe.TS)
	}
}

// TestTraceEventLineScrubsNUL 固定既有的 NUL 转义清洗规则
//（PG jsonb 拒绝 \u0000，SQLSTATE 22P05；见 trace_event.go:237-245）。
// 这条规则从「写 jsonb 列」平移到「写 spool 行」时必须保持，否则新路径会把
// 一个历史上被清洗掉的坏字节重新引入。
func TestTraceEventLineScrubsNUL(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte(`{"a":"x\u0000y"}`)
	line := traceEventLine(e)
	if bytes.Contains(line, []byte(`\u0000`)) {
		t.Fatalf("NUL 转义必须被替换：%s", line)
	}
	if !bytes.Contains(line, []byte(`\ufffd`)) {
		t.Fatalf("应替换为 U+FFFD：%s", line)
	}
}

// TestTraceEventLineWrapsInvalidRequestBody 固定「非法请求体包装」规则
//（trace_event.go:222-229）：sanitize 兜底路径下原始请求体不是合法 JSON，
// 包成 {"_raw_truncated": "..."} 且最多 256 字节，保证字段不为 null。
func TestTraceEventLineWrapsInvalidRequestBody(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte(strings.Repeat("x", 300))
	line := traceEventLine(e)
	if !bytes.Contains(line, []byte(`_raw_truncated`)) {
		t.Fatalf("非法请求体必须被包装：%s", line)
	}
	var probe struct {
		RequestBody struct {
			Raw string `json:"_raw_truncated"`
		} `json:"request_body"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("包装后必须仍是合法 JSON：%v", err)
	}
	if len(probe.RequestBody.Raw) != 256 {
		t.Fatalf("截断到 256 字节，实际 %d", len(probe.RequestBody.Raw))
	}
}

// TestTraceEventLineUntrustedBodyStaysRaw 固定 BodyRawUntrusted 的语义
//（trace_event.go:34-36、:215-221）：大正文直存路径不做 json.Valid 全量扫描，
// 因此**合法**的原始字节必须原样进入行内（逐字节相等），不能被重新编码。
func TestTraceEventLineUntrustedBodyStaysRaw(t *testing.T) {
	e := fixedEvent()
	raw := []byte(`{"messages":[{"role":"user","content":"你好"}]}`)
	e.RequestBodyRaw = raw
	e.BodyRawUntrusted = true
	line := traceEventLine(e)
	if !bytes.Contains(line, raw) {
		t.Fatalf("合法的大正文必须逐字节原样嵌入：%s", line)
	}
}

// TestTraceEventLineInvalidUntrustedBodyFallsBackToString 是上面那条的**反面**：
// BodyRawUntrusted 只表示「没校验过」，不表示「一定合法」。若原始字节非法，
// 直接以 json.RawMessage 嵌入会让 json.Marshal 整体失败 ——
// 那会让**整条 trace 丢失**（比丢一个字段严重得多）。
// 因此非法字节必须降级成字符串字段。
func TestTraceEventLineInvalidUntrustedBodyFallsBackToString(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte("not json at all")
	e.BodyRawUntrusted = true
	line := traceEventLine(e)
	var probe map[string]any
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("非法正文不得让整行失效：%v（line=%s）", err, line)
	}
	if _, ok := probe["request_body"]; !ok {
		t.Fatalf("request_body 键必须存在（规格要求字段不为 null）：%s", line)
	}
}

// TestTraceEventLineOmitsInvalidResponseBody 固定响应体的既有规则
//（trace_event.go:231-233）：响应体从不 sanitize，非法时**整体省略**该字段，
// 而不是包装 —— 与请求体规则不同，这是既有行为，不得在平移时改变。
func TestTraceEventLineOmitsInvalidResponseBody(t *testing.T) {
	e := fixedEvent()
	e.ResponseBodyRaw = []byte("{invalid")
	line := traceEventLine(e)
	var probe struct {
		ResponseBody *json.RawMessage `json:"response_body"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("非法响应体不得让整行失效：%v", err)
	}
	if probe.ResponseBody != nil {
		t.Fatalf("非法响应体应省略（nil），实际 %s", string(*probe.ResponseBody))
	}
}

// keyOrder 用 json.Decoder 的 Token 流取出顶层键顺序（json.Unmarshal 到 map 会丢顺序）。
func keyOrder(t *testing.T, line []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("读 JSON 起始符失败：%v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("行必须是 JSON 对象，实际起始符 %v", tok)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatalf("读键失败：%v", err)
		}
		keys = append(keys, k.(string))
		if err := skipValue(dec); err != nil {
			t.Fatalf("跳过值失败：%v", err)
		}
	}
	return keys
}

// skipValue 消费掉当前值的全部 token（含嵌套对象/数组）。
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || (d != '{' && d != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}
```

（`TraceBodyZoneForTest` 不是生产符号：在本文件里加 `var TraceBodyZoneForTest = time.FixedZone("CST", 8*60*60)`。**不要**去 import `internal/gateway/config`——`observability` 包不该依赖 config 包，时区应由调用方（任务 6 的 `TraceBodySink.DayOf`）注入。）

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./internal/gateway/observability/ -run 'TestTraceEventLine' -v`
预期：**编译失败**，`undefined: traceEventLine`。

- [ ] **步骤 3：写最小实现**

新建 `internal/gateway/observability/trace_line.go`：

```go
package observability

import (
	"bytes"
	"encoding/json"
	"time"
)

// traceArchiveTSLayout 是归档行里 ts 的格式。带 +08:00 偏移，
// 与 /datalog/emergency-archive 的存量格式一致（规格 §4.1 步骤①：字段与顺序对齐现有归档格式）。
const traceArchiveTSLayout = "2006-01-02T15:04:05-07:00"

// traceLineZone 是行内 ts 使用的时区：固定 UTC+8。
//
// 【为什么这里再定义一份，而不是 import config.TraceBodyZone】
// internal/gateway/observability 不该依赖 internal/gateway/config：
// 依赖方向一旦反过来，observability 的测试就要跟着配置体系走，
// 而它本身是纯序列化逻辑。时区由调用方通过 TraceBodySink 注入语义，
// 这里只固定「行内 ts 用 +08:00」这一条格式契约（与 dt 口径同源）。
var traceLineZone = time.FixedZone("CST", 8*60*60)

// traceRawTruncateLen 是非法请求体包装后的最大长度（沿用 trace_event.go 的 256 字节契约）。
const traceRawTruncateLen = 256

// traceEventLine 把一条事件序列化成**一整行** JSON（规格 §4.1 步骤①）。
//
// 返回的字节**不含**结尾换行（Writer.Append 自己补），且**内部不含裸换行** ——
// 后者是承重不变量：internal/tracearchive 的 validateLine 会拒绝行内裸换行，
// 因为「一次 Append == 一条物理行」是行号与 row_seq 对应的前提
//（writer.go:88-110）。JSON 序列化本身不会产生裸换行（正文里的 \n 编码成 \\n），
// 所以这一条由构造保证，但必须有测试盯着（本文件 A 层用例）。
//
// 字段规则与 traceEventToModel **逐条对应**（这不是巧合：两条路径共用同一件事的
// 两种表达，规则必须同源，否则「关闭功能时写 PG、开启时写 spool」会给出两套数据）：
//   - request_headers：仅当 json.Valid 时写入，并做 NUL 清洗；
//   - request_body：BodyRawUntrusted 或 json.Valid 时原样（清洗 NUL）；
//     否则包装成 {"_raw_truncated": "前 256 字节"}；
//     两者都不可用时降级成字符串，**绝不让整行失效**；
//   - response_body：仅当 json.Valid 时写入并清洗 —— 与请求体不同，
//     响应体非法时**整体省略**（沿用 trace_event.go:231-233 的既有行为）。
func traceEventLine(e TraceEvent) []byte {
	type line struct {
		RequestID      string          `json:"request_id"`
		TS             string          `json:"ts"`
		RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
		RequestBody    json.RawMessage `json:"request_body,omitempty"`
		ResponseBody   json.RawMessage `json:"response_body,omitempty"`
		ErrorCode      string          `json:"error_code"`
		ErrorMessage   string          `json:"error_message"`
	}

	out := line{
		RequestID:    e.RequestID,
		TS:           e.TS.In(traceLineZone).Format(traceArchiveTSLayout),
		ErrorCode:    e.ErrorCode,
		ErrorMessage: e.ErrorMessage,
	}

	if len(e.RequestHeadersRaw) > 0 && json.Valid(e.RequestHeadersRaw) {
		out.RequestHeaders = rawOrQuoted(scrubNULEscapes(e.RequestHeadersRaw))
	}
	if len(e.RequestBodyRaw) > 0 {
		switch {
		case (e.BodyRawUntrusted || json.Valid(e.RequestBodyRaw)) && json.Valid(e.RequestBodyRaw):
			out.RequestBody = rawOrQuoted(scrubNULEscapes(e.RequestBodyRaw))
		case e.BodyRawUntrusted:
			// 未校验且确实非法：不能以 RawMessage 嵌入（会让整行 Marshal 失败）。
			out.RequestBody = quotedJSON(truncateForRaw(e.RequestBodyRaw))
		default:
			wrapped, err := json.Marshal(map[string]string{
				"_raw_truncated": string(truncateForRaw(e.RequestBodyRaw)),
			})
			if err == nil {
				out.RequestBody = rawOrQuoted(scrubNULEscapes(wrapped))
			}
		}
	}
	if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {
		out.ResponseBody = rawOrQuoted(scrubNULEscapes(e.ResponseBodyRaw))
	}

	buf, err := json.Marshal(out)
	if err != nil {
		// 不可达：所有字段都已保证是合法 JSON 或普通字符串。保留兜底是为了
		// 「宁可行内少字段，也不能让整条 trace 消失」——但不能静默，
		// 因此把错误编码进行内（调用方与测试都能看见）。
		return []byte(`{"request_id":` + strconv.Quote(e.RequestID) + `,"_line_error":` + strconv.Quote(err.Error()) + `}`)
	}
	return buf
}

// truncateForRaw 截断到 traceRawTruncateLen 字节（按**字节**截断，与既有实现一致）。
func truncateForRaw(b []byte) []byte {
	if len(b) > traceRawTruncateLen {
		return b[:traceRawTruncateLen]
	}
	return b
}

// rawOrQuoted 在字节是合法 JSON 时返回原样 RawMessage，否则降级成 JSON 字符串。
//
// 【为什么要这一层】json.RawMessage 在 Marshal 时会做 compact 校验，
// 非法字节会让**整个对象**的 Marshal 失败。降级成字符串保证了
// 「一个坏字段不会带走整条记录」——这正是 2026-09-23 事故里
// 「单条坏记录连坐整批」教训的同一类防御（trace_event.go:152-160）。
func rawOrQuoted(b []byte) json.RawMessage {
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	return quotedJSON(b)
}

func quotedJSON(b []byte) json.RawMessage {
	q, err := json.Marshal(string(b))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(q)
}
```

（`strconv`、`bytes` 按实际使用补 import；`bytes` 只有在实现里用到时才加。）

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./internal/gateway/observability/ -run 'TestTraceEventLine' -v`
预期：PASS（7 个顶层用例；`TestTraceEventLineKeyOrderAndTSFormat` 内无子用例）。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-3.py`。**注意 `PACKAGES` 必须是 `./internal/gateway/observability/`**（本任务用例是 A 层，在包内）：

```python
NAME = "p2-task-3"
TARGET = "internal/gateway/observability/trace_line.go"
PACKAGES = ["./internal/gateway/observability/"]
MANIFEST_FILES = ["internal/gateway/observability/trace_line_internal_test.go"]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' internal/gateway/observability/trace_line_internal_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []

MUTATIONS = {}

# 行内 ts 的 +08:00 契约 ⇒ TestTraceEventLineKeyOrderAndTSFormat 必须红
MUTATIONS["m01_ts改用UTC"] = [
    ("sub", "e.TS.In(traceLineZone).Format(traceArchiveTSLayout)",
            "e.TS.In(time.UTC).Format(traceArchiveTSLayout)"),
]

# 「一个坏字段不得带走整条记录」⇒ TestTraceEventLineInvalidUntrustedBodyFallsBackToString 必须红
MUTATIONS["m02_非法字段不再降级为字符串"] = [
    ("sub", "if json.Valid(b) {\n\t\treturn json.RawMessage(b)\n\t}\n\treturn quotedJSON(b)",
            "return json.RawMessage(b)"),
]

# NUL 清洗 ⇒ TestTraceEventLineScrubsNUL 必须红（两条路径都要盯）
MUTATIONS["m03_请求头不清洗NUL"] = [
    ("sub", "out.RequestHeaders = rawOrQuoted(scrubNULEscapes(e.RequestHeadersRaw))",
            "out.RequestHeaders = rawOrQuoted(e.RequestHeadersRaw)"),
]
MUTATIONS["m04_请求体不清洗NUL"] = [
    ("sub", "out.RequestBody = rawOrQuoted(scrubNULEscapes(e.RequestBodyRaw))",
            "out.RequestBody = rawOrQuoted(e.RequestBodyRaw)"),
]

# 响应体非法时必须整体省略（与请求体的包装规则不同）⇒
# TestTraceEventLineOmitsInvalidResponseBody 必须红
MUTATIONS["m05_非法响应体改为包装"] = [
    ("sub", "if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {",
            "if len(e.ResponseBodyRaw) > 0 {"),
]

CHECKS = [
    {
        "name": "键序-与归档格式一致",
        "file": "internal/gateway/observability/trace_line.go",
        "pattern": r'json:"request_id"[\s\S]*json:"ts"[\s\S]*json:"request_headers"[\s\S]*json:"request_body"[\s\S]*json:"response_body"[\s\S]*json:"error_code"[\s\S]*json:"error_message"',
        "expect": "present",
        "why": "规格 §4.1 步骤① 的「字段与顺序对齐现有归档格式」是**源码结构**要求，变异测不到（改键序不会让行为测试转红，除非有一条专门的键序断言——本任务确实有一条 A 层用例，因此这条 check 是冗余的第二道）。",
    },
]

REQUIREMENTS = [
    {"plan": "计划 2 任务 3：行内 ts 必须是 +08:00（规格 §4.1 与存量归档格式一致）",
     "mutations": ["m01_ts改用UTC"], "checks": []},
    {"plan": "计划 2 任务 3：非法字段必须降级，绝不让整行失效（继承 2026-09-23 事故的教训）",
     "mutations": ["m02_非法字段不再降级为字符串"], "checks": []},
    {"plan": "计划 2 任务 3：NUL 清洗规则从 jsonb 写入平移到 spool 行（PG 拒绝 \\u0000，22P05）",
     "mutations": ["m03_请求头不清洗NUL", "m04_请求体不清洗NUL"], "checks": []},
    {"plan": "计划 2 任务 3：非法响应体整体省略（沿用既有行为，不得改成包装）",
     "mutations": ["m05_非法响应体改为包装"], "checks": []},
    {"plan": "规格 §4.1 步骤①：键序与归档格式一致",
     "mutations": [], "checks": ["键序-与归档格式一致"]},
]

EXPECTED_GREEN = {}
```

- [ ] **步骤 6：跑变异集合**

运行：
```bash
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest /tmp/p2-task-3-names.txt \
  --mutation-set tools/trace-mutate/sets/p2-task-3.py --check
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest /tmp/p2-task-3-names.txt \
  --mutation-set tools/trace-mutate/sets/p2-task-3.py
```
预期：`--check` 5 条全部唯一定位；权威表退出码 0、5 条断言红、三断言过、无未登记绿。

- [ ] **步骤 7：提交**

```bash
git add internal/gateway/observability/trace_line.go \
        internal/gateway/observability/trace_line_internal_test.go \
        tools/trace-mutate/sets/p2-task-3.py
git commit -m "feat(gateway): traceEventLine 序列化一整行 JSON（计划 2/4 任务 3）

键序与 /datalog 存量归档格式一致；ts 固定 +08:00；继承 NUL 清洗、
非法请求体包装、非法响应体省略三条既有规则；任何坏字段都降级为字符串，
绝不让整行失效（2026-09-23 单条坏记录连坐整批的教训）。"
```

---

### Task 4：磁盘水位与闩锁（`SpoolGate`，规格 §15.1 R9）

**文件：**
- 新建：`internal/gateway/observability/spool_gate.go`
- 测试：`tests/gateway/observability/spool_gate_test.go`（B 层）

**接口：**
- 依赖输入：无（纯状态机 + 两个可注入探针）
- 对外产出：
  ```go
  type SpoolGateOptions struct {
      Root                 string
      DiskWatermarkPercent int
      ProbeInterval        time.Duration                    // 闰锁中的探针周期（默认 1m）
      Now                  func() time.Time                 // 可注入（默认 time.Now）
      UsagePercent         func(root string) (float64, error) // 可注入（默认 statfs）
      ProbeWrite           func(root string) error          // 可注入（默认写 .probe 文件）
  }
  type SpoolGate struct{ /* 私有字段 */ }
  func NewSpoolGate(opts SpoolGateOptions) *SpoolGate
  func (g *SpoolGate) Allow() bool                  // 未闰锁 ⇒ true
  func (g *SpoolGate) Trip(reason string, cause error) bool  // 返回是否**本次**跳变（调用方据此只打印一条日志）
  func (g *SpoolGate) Tick(now time.Time)           // 未闰锁：查水位；已闰锁：到点探针、成功则解锁
  func (g *SpoolGate) Latched() bool
  func (g *SpoolGate) Reason() string
  func (g *SpoolGate) Stats() GateStats             // Trips / ProbeFailures / LatchedSeconds / UsagePercent
  var ErrSpoolLatched = errors.New("...")           // 供 AppendLine 包装
  ```
- [ ] **步骤 1：写失败的测试**

新建 `tests/gateway/observability/spool_gate_test.go`（完整的 6 个用例在实现指导下写；结构如下，**每一条都是一条 R9 要求**）：

```go
package observability_test

import (
	"errors"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/require"
)

// fakeClock 让闰锁的时间语义（探针周期、累计秒数）可确定性断言。
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// countingUsage 记录 statfs 被调用的次数：闰锁期间**不得**再有文件系统调用。
type countingUsage struct {
	calls int
	pct   float64
	err   error
}

func (u *countingUsage) usage(root string) (float64, error) {
	u.calls++
	return u.pct, u.err
}

type countingProbe struct {
	calls int
	fail  error
}

func (p *countingProbe) probe(root string) error {
	p.calls++
	return p.fail
}

func newGate(t *testing.T, pct float64, u *countingUsage, p *countingProbe, clk *fakeClock) *gwobs.SpoolGate {
	t.Helper()
	return gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 "/datalog/trace-archive-v4",
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Minute,
		Now:                  clk.now,
		UsagePercent:         u.usage,
		ProbeWrite:           p.probe,
	})
}

// TestGateTripsOnDiskWatermark 水位超限 ⇒ 闰锁。
func TestGateTripsOnDiskWatermark(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 91}
	p := &countingProbe{}
	g := newGate(t, 0, u, p, clk)
	require.True(t, g.Allow())
	g.Tick(clk.now())
	require.False(t, g.Allow(), "水位 91%% > 90%% ⇒ 必须闰锁")
	require.Contains(t, g.Reason(), "watermark")
	require.Equal(t, int64(1), g.Stats().Trips)
}

// TestGateTripIsIdempotentAndReportedOnce 跳变只报一次：Trip 的返回值是
// 「本次是否跳变」，日志据此只打一条（否则每行一次 ERROR 会淹没日志）。
func TestGateTripIsIdempotentAndReportedOnce(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	g := newGate(t, 0, &countingUsage{pct: 0}, &countingProbe{}, clk)
	require.True(t, g.Trip("append_failed", errors.New("no space")))
	require.False(t, g.Trip("append_failed", errors.New("no space")), "第二次不得再报跳变")
	require.Equal(t, int64(1), g.Stats().Trips)
}

// TestGateLatchedDoesNoFilesystemWork 闰锁期间：Allow() 为假、statfs 与探针
// 都**只在到点那一刻**调用一次。这一条就是 R9：「不要每个请求都重试」。
func TestGateLatchedDoesNoFilesystemWork(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := newGate(t, 0, u, p, clk)
	g.Tick(clk.now())
	require.False(t, g.Allow())
	usageAfterTrip, probeAfterTrip := u.calls, p.calls

	// 闰锁期间大量请求：只读 Allow()，不得触发任何文件系统调用
	for i := 0; i < 1000; i++ {
		require.False(t, g.Allow())
	}
	require.Equal(t, usageAfterTrip, u.calls, "Allow() 不得触发 statfs")
	require.Equal(t, probeAfterTrip, p.calls, "Allow() 不得触发探针")

	// 未到周期：Tick 也不得探针
	clk.advance(30 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, probeAfterTrip, p.calls, "未到探针周期不得探针")
}

// TestGateProbeFailureKeepsLatched 探针失败 ⇒ 保持闰锁，并计入 ProbeFailures。
func TestGateProbeFailureKeepsLatched(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := &countingProbe{fail: errors.New("read-only file system")}
	u := &countingUsage{pct: 95}
	g := newGate(t, 0, u, p, clk)
	g.Tick(clk.now())
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.False(t, g.Allow())
	require.Equal(t, 1, p.calls)
	require.Equal(t, int64(1), g.Stats().ProbeFailures)
}

// TestGateProbeSuccessUnlatches 恢复条件必须显式：探针成功（且水位低于阈值）⇒ 解锁。
func TestGateProbeSuccessUnlatches(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := newGate(t, 0, u, p, clk)
	g.Tick(clk.now())
	require.False(t, g.Allow())

	u.pct = 50 // 空间已释放
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.True(t, g.Allow(), "探针成功且水位已回落 ⇒ 必须解锁")
	require.Empty(t, g.Reason())
}

// TestGateWatermarkAloneDoesNotUnlatch：水位落回但探针仍失败 ⇒ 保持闰锁。
// 「水位」与「可写」是两件事：水位可能因别的进程写入而回落，但本进程仍无权限。
func TestGateWatermarkAloneDoesNotUnlatch(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{fail: errors.New("permission denied")}
	g := newGate(t, 0, u, p, clk)
	g.Tick(clk.now())
	u.pct = 10
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.False(t, g.Allow())
}
```

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./tests/gateway/observability/ -run 'TestGate' -v`
预期：**编译失败**，`undefined: gwobs.SpoolGate` / `gwobs.SpoolGateOptions`。

- [ ] **步骤 3：写最小实现**

新建 `internal/gateway/observability/spool_gate.go`：

```go
package observability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ErrSpoolLatched 表示正文落盘已被闰锁关闭：调用方**不得**再尝试写 spool，
// 而应把该行标为 body_missing=true（规格 §15.1 R9 的降级约定）。
var ErrSpoolLatched = errors.New("observability: 正文落盘已闰锁")

// GateStats 是闰锁状态的可观测快照（规格 §9.3：降级必须有明确可观测信号）。
type GateStats struct {
	Latched        bool
	Reason         string
	Trips          int64   // 累计跳变次数（不是行数）
	ProbeFailures  int64
	LatchedSeconds int64   // 累计闰锁秒数
	UsagePercent   float64 // 最近一次观测到的水位
}

// SpoolGateOptions 描述水位与闰锁行为。三个函数型字段都可注入，
// 这是「测试不得依赖真实盘满」的实现方式（注入水位/探针，而非 mock 文件系统）。
type SpoolGateOptions struct {
	Root                 string
	DiskWatermarkPercent int
	ProbeInterval        time.Duration
	Now                  func() time.Time
	UsagePercent         func(root string) (float64, error)
	ProbeWrite           func(root string) error
}

// SpoolGate 是「要不要写 spool」的唯一判定者（规格 §15.1 R9）。
//
// 【为什么必须有闩锁，而不是每次写入失败就跳过这一行】写失败（典型 ENOSPC）后，
// tracearchive.Writer 会把当前块毒化隔离并**在下次 Append 时**换新 part，
// 而算新序号要 os.ReadDir(spool 目录) —— 每请求一次失败就付出一次 O(n) 目录扫描，
// 同时 spool 目录里的 part 序号与 dentry 都会无界增长。闩锁把「每请求重试」
// 变成「每 ProbeInterval 探一次」。
type SpoolGate struct {
	opts SpoolGateOptions

	mu             sync.Mutex
	latched        bool
	reason         string
	latchedAt      time.Time
	lastProbe      time.Time
	trips          int64
	probeFailures  int64
	latchedSeconds int64
	usagePercent   float64
}

func NewSpoolGate(opts SpoolGateOptions) *SpoolGate {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ProbeInterval <= 0 {
		opts.ProbeInterval = time.Minute
	}
	if opts.UsagePercent == nil {
		opts.UsagePercent = defaultUsagePercent
	}
	if opts.ProbeWrite == nil {
		opts.ProbeWrite = defaultProbeWrite
	}
	return &SpoolGate{opts: opts}
}

// Allow 是热路径唯一会调用的方法：只读一个布尔量，**不做任何 IO**。
func (g *SpoolGate) Allow() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.latched
}

// Trip 进入闰锁。返回值表示「本次是否发生了跳变」——调用方据此恰好打印一条
// ERROR 日志；闰锁期间的每一行都打日志会淹没日志（规格 §9.3 要的是信号，不是噪音）。
func (g *SpoolGate) Trip(reason string, cause error) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.latched {
		return false
	}
	g.latched = true
	g.reason = reason
	if cause != nil {
		g.reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	g.latchedAt = g.opts.Now()
	g.lastProbe = g.latchedAt
	g.trips++
	return true
}

// Tick 是周期评估入口（由 spooler 的 flush 循环调用，**不在请求路径上**）。
//
//   - 未闰锁：查一次水位，超限则闰锁；
//   - 已闰锁：到点做一次探针；**水位回落**与**探针成功**必须同时成立才解锁。
//
// 【为什么解锁要两个条件】水位回落只说明盘上有空间了，不说明本进程能写
// （权限、只读挂载、目录被删都可能）。反之探针成功也可能只是写进了别的文件系统。
// 两个条件同时成立才恢复，是 R9「恢复条件必须显式」的落地。
func (g *SpoolGate) Tick(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.latched {
		if pct, err := g.opts.UsagePercent(g.opts.Root); err == nil {
			g.usagePercent = pct
			if pct >= float64(g.opts.DiskWatermarkPercent) {
				g.latched = true
				g.reason = fmt.Sprintf("disk_watermark: %s 已用 %.1f%% ≥ %d%%",
					g.opts.Root, pct, g.opts.DiskWatermarkPercent)
				g.latchedAt = now
				g.lastProbe = now
				g.trips++
			}
		}
		// statfs 失败（例如根目录不存在）本身不闰锁：真正的判据是「能不能写」，
		// 由第一次 Append 失败或探针来定。这里保持沉默是**故意**的——
		// 把「statfs 失败」也当闰锁会让一个权限问题伪装成磁盘问题。
		return
	}

	g.latchedSeconds += int64(now.Sub(g.latchedAt).Seconds()) - g.latchedSeconds
	if now.Sub(g.lastProbe) < g.opts.ProbeInterval {
		return
	}
	g.lastProbe = now

	probeErr := g.opts.ProbeWrite(g.opts.Root)
	pct, usageErr := g.opts.UsagePercent(g.opts.Root)
	if usageErr == nil {
		g.usagePercent = pct
	}
	if probeErr != nil {
		g.probeFailures++
		return
	}
	if usageErr == nil && pct >= float64(g.opts.DiskWatermarkPercent) {
		g.probeFailures++
		return
	}
	g.latched = false
	g.reason = ""
	g.latchedSeconds = 0
}

func (g *SpoolGate) Latched() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.latched }

func (g *SpoolGate) Reason() string { g.mu.Lock(); defer g.mu.Unlock(); return g.reason }

func (g *SpoolGate) Stats() GateStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return GateStats{
		Latched: g.latched, Reason: g.reason, Trips: g.trips,
		ProbeFailures: g.probeFailures, LatchedSeconds: g.latchedSeconds,
		UsagePercent: g.usagePercent,
	}
}

// defaultUsagePercent 用 statfs 算「已用百分比」。
//
// 取 Bavail（普通用户可用块）而不是 Bfree：容器里进程常以 root 跑，
// 但用 Bavail 是保守的一侧 —— 水位线宁可早触发也不该晚触发。
func defaultUsagePercent(root string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", root, err)
	}
	total := uint64(st.Blocks) * uint64(st.Bsize)
	if total == 0 {
		return 0, fmt.Errorf("statfs %s: 总块数为 0", root)
	}
	avail := uint64(st.Bavail) * uint64(st.Bsize)
	return float64(total-avail) / float64(total) * 100, nil
}

// defaultProbeWrite 是恢复探针：在 spool 目录下建一个临时文件、写一个字节、删掉。
//
// 【为什么探针不放在 <root> 根目录】根目录可能是个只读挂载点而 spool 子目录可写，
// 反之亦然；探针必须落在**真正要写的那棵树**里。目录用 MkdirAll 创建：
// 与 Writer.ensureBlockLocked 的行为一致（writer.go:489）。
//
// 【为什么探针只写 1 字节】恢复判定要的是「写权限 + 空间」这两件事，
// 不需要复现真实写入量；写多了会在接近满盘时把水位推得更高，形成抖动。
func defaultProbeWrite(root string) error {
	dir := filepath.Join(root, "spool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("建 spool 目录失败: %w", err)
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return fmt.Errorf("建探针文件失败: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write([]byte{'\n'}); err != nil {
		f.Close()
		return fmt.Errorf("写探针失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync 探针失败: %w", err)
	}
	return f.Close()
}
```

（`.probe-*` 不会被 sealer 当成待封块：计划 1 的 `ListPendingSeals` 只收 `part-NNNNNN.jsonl` 形式的文件名，`.probe-` 前缀不合 `parsePartSeq` 的六位数字要求。）

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./tests/gateway/observability/ -run 'TestGate' -v`
预期：PASS（6 个顶层用例）。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-4.py`，`TARGET = "internal/gateway/observability/spool_gate.go"`、`PACKAGES = ["./tests/gateway/observability/"]`。必须包含（每条都对应上面一个用例）：

```python
MUTATIONS = {}

# 水位闩锁 ⇒ TestGateTripsOnDiskWatermark 必须红
MUTATIONS["m01_水位比较改为永不触发"] = [
    ("sub", "if pct >= float64(g.opts.DiskWatermarkPercent) {", "if false {"),
]
# 跳变幂等 ⇒ TestGateTripIsIdempotentAndReportedOnce 必须红
MUTATIONS["m02_Trip不再幂等"] = [
    ("sub", "if g.latched {\n\t\treturn false\n\t}", "if false {\n\t\treturn false\n\t}"),
]
# Allow 不做 IO（R9 的核心）⇒ TestGateLatchedDoesNoFilesystemWork 必须红
MUTATIONS["m03_Allow每次查水位"] = [
    ("sub", "return !g.latched", "_, _ = g.opts.UsagePercent(g.opts.Root); return !g.latched"),
]
# 探针周期 ⇒ 同上用例必须红
MUTATIONS["m04_探针周期改为0"] = [
    ("sub", "if now.Sub(g.lastProbe) < g.opts.ProbeInterval {", "if false {"),
]
# 恢复条件必须是「探针成功 + 水位回落」两个条件 ⇒ 两条用例分别必须红
MUTATIONS["m05_解锁只看探针"] = [
    ("sub", "if usageErr == nil && pct >= float64(g.opts.DiskWatermarkPercent) {", "if false {"),
]
MUTATIONS["m06_解锁只看水位"] = [
    ("sub", "if probeErr != nil {\n\t\tg.probeFailures++\n\t\treturn\n\t}", "if false {\n\t\tg.probeFailures++\n\t\treturn\n\t}"),
]
```

`EXPECTED_GREEN = {}`。**注意**：`m02` / `m03` / `m06` 的 `sub` 目标串含缩进与换行，必须与实现逐字一致；`--check` 报「定位到 0/多处」时就地修正**变异字符串**（不要改实现去迁就）。

- [ ] **步骤 6：跑变异集合**

运行：`--check` 然后权威表（命令同任务 2 步骤 9，换集合与清单名）。
预期：6 条唯一定位；权威表退出码 0、6 条断言红、三断言过、无未登记绿。

- [ ] **步骤 7：提交**

```bash
git add internal/gateway/observability/spool_gate.go \
        tests/gateway/observability/spool_gate_test.go \
        tools/trace-mutate/sets/p2-task-4.py
git commit -m "feat(gateway): SpoolGate 磁盘水位与写失败闰锁（计划 2/4 任务 4）

规格 §15.1 R9：盘满后必须放弃正文并置 body_missing=true，而不是每请求重试
（每次重试都会付出一次 O(n) 目录扫描，part 序号与 dentry 无界增长）。
热路径 Allow() 只读一个布尔量、零 IO；恢复条件 = 探针可写 且 水位回落。
探针与水位函数可注入，测试不依赖真实盘满。"
```

---

### Task 5：周期 flush 与滚动（`TraceSpooler`，规格 §15.1 R8 / §7.1）

**文件：**
- 新建：`internal/gateway/observability/spooler.go`
- 测试：`tests/gateway/observability/spooler_test.go`（B 层）

**接口：**
- 依赖输入：`internal/tracearchive` 的公开 API（**不改动**）：`NewWriter(WriterOptions)`、`Append`、`PendingSeals`、`MarkSealed`、`Close`；`WriterOptions{Root, Inst, BlockBytes, SealInterval}`；`Ref{DT, Inst, Part, Row}`；`Paths.SpoolFile`
- 对外产出：
  ```go
  type TraceSpoolerOptions struct {
      Root          string
      Inst          string
      BlockBytes    int64         // 字节（调用方负责 MB → 字节换算）
      RollInterval  time.Duration // = WriterOptions.SealInterval，≤1h 且 < minAge 3h
      FlushInterval time.Duration // ≤1m
      Gate          *SpoolGate    // nil ⇒ 内部建一个默认的
      Now           func() time.Time
  }
  type TraceSpooler struct{ /* 私有 */ }
  func NewTraceSpooler(opts TraceSpoolerOptions) (*TraceSpooler, error)
  func (s *TraceSpooler) AppendLine(dt string, line []byte) (ta.Ref, error) // 闰锁中返回 ErrSpoolLatched
  func (s *TraceSpooler) FlushAndRoll() error   // 无条件 flush + 对达阈值块滚动
  func (s *TraceSpooler) Run(ctx context.Context) // 周期循环，退出时 Close
  func (s *TraceSpooler) Close() error
  func (s *TraceSpooler) Gate() *SpoolGate
  func (s *TraceSpooler) Stats() SpoolerStats  // Flushes / Rolls / Gate
  ```

- [ ] **步骤 1：写失败的测试**

新建 `tests/gateway/observability/spooler_test.go`。**七个用例，其中「空块不滚动」与「滚动前必须先落盘」是承重的**：

```go
package observability_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/require"
)

func newSpooler(t *testing.T, blocks int64, roll, flush time.Duration) (*gwobs.TraceSpooler, string) {
	t.Helper()
	root := t.TempDir()
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue",
		BlockBytes: blocks, RollInterval: roll, FlushInterval: flush,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, root
}

// TestSpoolerFlushMakesBufferedLinesVisible：Append 只是进 bufio 缓冲，
// 周期 flush 之后 sealer 才能看到完整字节（规格 §7.1）。
func TestSpoolerFlushMakesBufferedLinesVisible(t *testing.T) {
	s, root := newSpooler(t, 1<<30, time.Hour, time.Minute)
	for _, l := range []string{`{"a":1}`, `{"a":2}`} {
		_, err := s.AppendLine("2026-09-24", []byte(l))
		require.NoError(t, err)
	}
	spool := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl")
	before, err := os.ReadFile(spool)
	require.NoError(t, err)
	require.Empty(t, before, "未 flush 前缓冲不得落盘（这正是「不 flush 会丢正文」的机理）")

	require.NoError(t, s.FlushAndRoll())
	after, err := os.ReadFile(spool)
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"a\":2}\n", string(after))
}

// TestSpoolerRollsOnBlockBytes：达到块大小阈值 ⇒ 滚动到 part-000001。
func TestSpoolerRollsOnBlockBytes(t *testing.T) {
	const line = `{"pad":"0123456789012345678901234567890123456789"}`
	s, root := newSpooler(t, 1<<10 /* 1 KiB */, time.Hour, time.Minute)
	for i := 0; i < 64; i++ {
		_, err := s.AppendLine("2026-09-24", []byte(line))
		require.NoError(t, err)
	}
	require.NoError(t, s.FlushAndRoll())
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl"))
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"),
		"达到 1 KiB 后必须滚动，新块必须是 part-000001（不是复用 000000）")
}

// TestSpoolerRollsOnRollInterval：到滚动间隔（≤1h，默认 1h）⇒ 滚动。
func TestSpoolerRollsOnRollInterval(t *testing.T) {
	s, root := newSpooler(t, 1<<30, 30*time.Millisecond, time.Minute)
	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, s.FlushAndRoll())
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"))
}

// TestSpoolerDoesNotRollEmptyBlock 是**承重**用例：空块上到点不滚动。
// 若在空块上 MarkSealed，会留下 0 字节 spool；sealer 按文件名照样能发现它，
// 可能封出 0 行产物，使该 part 的所有点读变成「行号越界」（而不是响亮缺失）。
func TestSpoolerDoesNotRollEmptyBlock(t *testing.T) {
	s, root := newSpooler(t, 1<<30, 10*time.Millisecond, time.Minute)
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, s.FlushAndRoll())
	require.NoFileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl"))

	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl"),
		"空块上不得滚动 ⇒ 首次写入必须落在 part-000000")
}

// TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain 固定「先滚动、后封块」的次序
//（规格 §15.1 R8、计划 1 `MarkSealed` 的契约）：滚动之后，sealer 可能随时封块并删除
// spool；若网关仍持有该 part 的 fd 继续追加，写会落进已被 unlink 的 inode ——
// write(2) 返回成功、正文永久不可达（D7）。本用例断言两件事：
//   ① 被滚动的文件在滚动那一刻是**完整**的（最后一行有换行）；
//   ② 之后再 Append，老文件逐字节不变（新内容进 part-000001）。
func TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain(t *testing.T) {
	s, root := newSpooler(t, 1<<10, time.Hour, time.Minute)
	p0 := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl")
	for i := 0; i < 64; i++ {
		_, err := s.AppendLine("2026-09-24", []byte(`{"pad":"0123456789012345678901234567890123456789"}`))
		require.NoError(t, err)
	}
	require.NoError(t, s.FlushAndRoll())

	rolled, err := os.ReadFile(p0)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(rolled), "\n"), "滚动后的 spool 必须是完整行结尾")
	require.NotContains(t, string(rolled), "\x00")

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":"after-roll"}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())

	again, err := os.ReadFile(p0)
	require.NoError(t, err)
	require.Equal(t, rolled, again, "滚动之后老 part 必须逐字节不再变化")
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"))
}

// TestSpoolerRollsOnDTChange 固定跨日行为：dt 变 ⇒ 换到新 dt 目录的 part-000000，
// 老 dt 的 spool 被正常关闭（不隔离、不删除 —— 它是合法正文，由 sealer 按 mtime 封块）。
func TestSpoolerRollsOnDTChange(t *testing.T) {
	s, root := newSpooler(t, 1<<30, time.Hour, time.Minute)
	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	_, err = s.AppendLine("2026-09-25", []byte(`{"a":2}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())

	oldSpool := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl")
	newSpool := filepath.Join(root, "spool", "dt=2026-09-25", "inst=blue", "part-000000.jsonl")
	require.Equal(t, "{\"a\":1}\n", mustRead(t, oldSpool))
	require.Equal(t, "{\"a\":2}\n", mustRead(t, newSpool))
}

// TestSpoolerRefusesMissingRoot 是**安全**用例：Root 不存在时构造失败，
// 绝不隐式创建。理由见全局约束：容器里以 root 跑，MkdirAll 会在容器可写层
// 建出一个不在挂载点上的 /datalog，把正文写到宿主根文件系统上 ——
// 这正是 2026-09-24 `/` 盘被写满的同一类事故。
func TestSpoolerRefusesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	_, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
	})
	require.Error(t, err)
	require.NoDirExists(t, root, "构造失败不得留下自建的目录")
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}
```

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./tests/gateway/observability/ -run 'TestSpooler' -v`
预期：**编译失败**，`undefined: gwobs.TraceSpooler`。

- [ ] **步骤 3：写最小实现**

新建 `internal/gateway/observability/spooler.go`：

```go
package observability

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// TraceSpoolerOptions 是网关侧 spool 的构造参数。
type TraceSpoolerOptions struct {
	Root          string
	Inst          string
	BlockBytes    int64
	RollInterval  time.Duration
	FlushInterval time.Duration
	Gate          *SpoolGate
	Now           func() time.Time
}

// SpoolerStats 是 spool 侧的可观测快照。
type SpoolerStats struct {
	Flushes int64
	Rolls   int64
	Gate    GateStats
}

// TraceSpooler 是网关对计划 1 写入原语的**唯一**使用者（规格 §4.2 的方案 C 边界）。
//
// 它做三件事：① 把正文行交给 Writer 顺序追加；② 按 ≤1m 周期无条件 flush；
// ③ 对达到阈值的块执行滚动（MarkSealed），使 sealer 之后可以安全封块并删除 spool。
// 它**不压缩、不写产物、不封块** —— 封块是计划 4 的独立 sealer 进程的职责。
type TraceSpooler struct {
	w             *ta.Writer
	gate          *SpoolGate
	flushInterval time.Duration
	now           func() time.Time

	mu      sync.Mutex
	flushes int64
	rolls   int64
}

// NewTraceSpooler 构造 spool 写入器。
//
// 【为什么这里必须 os.Stat 而不是直接开工】网关容器以 root 运行，
// os.MkdirAll 会在**容器可写层**建出一个不在挂载点上的 /datalog ——
// 正文会写到宿主根文件系统上，把 `/` 写满。R8/D7 的修复目标是「不再丢正文」，
// 而写错文件系统是把事故换个方向重演。因此：根目录必须**已经存在**且是目录，
// 否则构造失败，由 main 记录 ERROR 并退回既有路径（正文仍写 PG，不丢数据）。
func NewTraceSpooler(opts TraceSpoolerOptions) (*TraceSpooler, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Gate == nil {
		opts.Gate = NewSpoolGate(SpoolGateOptions{Root: opts.Root, DiskWatermarkPercent: 90})
	}
	w, err := ta.NewWriter(ta.WriterOptions{
		Root:         opts.Root,
		Inst:         opts.Inst,
		BlockBytes:   opts.BlockBytes,
		SealInterval: opts.RollInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 tracearchive.Writer 失败: %w", err)
	}
	info, err := os.Stat(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("正文根目录 %s 不可用（未挂载？）: %w", opts.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("正文根目录 %s 不是目录", opts.Root)
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = time.Minute
	}
	return &TraceSpooler{
		w:             w,
		gate:          opts.Gate,
		flushInterval: opts.FlushInterval,
		now:           opts.Now,
	}, nil
}

func (s *TraceSpooler) Gate() *SpoolGate { return s.gate }

// AppendLine 追加一行正文，返回定位键。
//
// 闰锁中直接返回 ErrSpoolLatched，**不触碰文件系统**：调用方据此把该行标成
// body_missing=true（规格 §15.1 R9）。
func (s *TraceSpooler) AppendLine(dt string, line []byte) (ta.Ref, error) {
	if !s.gate.Allow() {
		return ta.Ref{}, fmt.Errorf("%w（原因：%s）", ErrSpoolLatched, s.gate.Reason())
	}
	ref, err := s.w.Append(dt, line)
	if err != nil {
		// 写失败 ⇒ 立即闰锁。下一个事件不会再试（R9），
		// 直到探针确认可写为止；这段时间里所有行都走 body_missing。
		s.gate.Trip("append_failed", err)
		return ta.Ref{}, err
	}
	return ref, nil
}

// FlushAndRoll 无条件 flush 一次，并对**已达阈值**的块执行滚动。
//
// 【为什么用 PendingSeals 做周期 flush 的入口】它的 Flush 发生在空块提前返回
//（writer.go:377）与阈值提前返回（:380-384）**之前**；只有 cur == nil（:358）
// 与 poisoned（:365，故意不 flush）两条路径不 flush。因此定时调它即可满足
// 规格 §7.1 的「无条件执行」。
//
// 【为什么消费它返回的任务，而不是忽略】返回的 SealTask 是公开 API 里唯一能
// 观察到「当前块是哪个 (dt, part)」的通道，滚动需要这个标识。这**不是**
// 「网关自己封块」：网关只调 MarkSealed 关掉自己的句柄、让下一个 Append 换新 part；
// 压缩与产物生成仍是计划 4 那个进程外 sealer 的职责（方案 C）。
//
// 【空块为什么不滚动 —— 承重】PendingSeals 对 rows == 0 的块返回 nil
//（writer.go:377-379），所以这里天然不会在空块上滚动。这一点必须有测试盯着
//（spooler_test.go 的 TestSpoolerDoesNotRollEmptyBlock）：MarkSealed 只关句柄，
// 下一次 Append 才建新 part；若在空块上滚动，会留下 0 字节 spool 文件，
// sealer 按文件名照样能发现它，可能封出 0 行产物，使该 part 的所有点读
// 从「响亮缺失」退化成「行号越界」。
//
// 【为什么必须先滚动、后让 sealer 看见（R8/D7）】sealer 只按 mtime + minAge(3h)
// 判断「已停止追加」。滚动间隔 ≤1h 且严格小于 3h ⇒ 任何一块在被 sealer 判定
// 「静止」之前就已被网关滚掉，网关手里不再有它的 fd，追加不会落进已被 unlink
// 的 inode。次序错误在这里没有守卫可加（正常流程 sealer 确实已删 spool，
// 「stat 文件是否存在」的守卫会在正常路径上误报）—— 只能靠**次序保证**，
// 因此本函数的实现次序（先 flush、再 MarkSealed）是承重的，
// 且由 TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain 固定。
func (s *TraceSpooler) FlushAndRoll() error {
	tasks, err := s.w.PendingSeals()
	if err != nil {
		// flush 失败必须上抛：sealer 直接读 spool，缓冲没落盘它会把
		// 不完整的块当完整的封掉（漏行）。
		return fmt.Errorf("周期 flush 失败: %w", err)
	}
	s.mu.Lock()
	s.flushes++
	s.mu.Unlock()

	for _, task := range tasks {
		// 无返回值：签名是 func (w *Writer) MarkSealed(dt string, part int)。
		// close/Flush 的错误只能丢弃（writer.go:395-419 的契约注释已登记这一点，
		// 与「次序错误丢正文」是两件事）。
		s.w.MarkSealed(task.DT, task.Part)
		s.mu.Lock()
		s.rolls++
		s.mu.Unlock()
	}
	return nil
}

// Run 是周期循环：每个 FlushInterval 做一次「探针评估 + 无条件 flush + 滚动判定」。
//
// 这两个动作合并在同一个 tick 里是刻意的：滚动必须发生在 flush 之后
//（先落盘、再让 sealer 可能看见），而 flush 本身由 PendingSeals 完成。
func (s *TraceSpooler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = s.Close()
			return
		case now := <-ticker.C:
			s.gate.Tick(now)
			if err := s.FlushAndRoll(); err != nil {
				log.Printf("[trace-spool] 周期 flush/滚动失败: %v", err)
			}
		}
	}
}

func (s *TraceSpooler) Close() error { return s.w.Close() }

func (s *TraceSpooler) Stats() SpoolerStats {
	s.mu.Lock()
	flushes, rolls := s.flushes, s.rolls
	s.mu.Unlock()
	return SpoolerStats{Flushes: flushes, Rolls: rolls, Gate: s.gate.Stats()}
}
```

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./tests/gateway/observability/ -run 'TestSpooler' -v`
预期：PASS（7 个顶层用例）。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-5.py`（`TARGET = "internal/gateway/observability/spooler.go"`、`PACKAGES = ["./tests/gateway/observability/"]`）：

```python
MUTATIONS = {}

# 根目录必须存在 ⇒ TestSpoolerRefusesMissingRoot 必须红
MUTATIONS["m01_删除根目录存在性检查"] = [
    ("cut", "\tinfo, err := os.Stat(opts.Root)", "\treturn &TraceSpooler{w: w, gate: opts.Gate, flushInterval: opts.FlushInterval, now: opts.Now}, nil\n\tinfo, err := os.Stat(opts.Root)"),
]
# 闰锁时不得触碰文件系统 ⇒
# TestSpoolerLatchedAppendDoesNotTouchDisk 必须红
MUTATIONS["m02_闰锁时仍然尝试写入"] = [
    ("sub", "if !s.gate.Allow() {", "if false {"),
]
# 写失败必须闰锁 ⇒ 同用例必须红
MUTATIONS["m03_写失败不闰锁"] = [
    ("sub", "s.gate.Trip(\"append_failed\", err)", "_ = err"),
]
# 周期 flush 必须真的调 PendingSeals ⇒ TestSpoolerFlushMakesBufferedLinesVisible 必须红
MUTATIONS["m04_FlushAndRoll不再flush"] = [
    ("sub", "tasks, err := s.w.PendingSeals()", "var tasks []ta.SealTask; var err error"),
]
# 滚动（MarkSealed）⇒ TestSpoolerRollsOnBlockBytes / OnRollInterval / DTChange 必须红
MUTATIONS["m05_不再滚动"] = [
    ("sub", "s.w.MarkSealed(task.DT, task.Part)", "_ = task"),
]
```

**注意 `m05` 与「空块不滚动」的关系**：`m05` 摘掉滚动之后，`TestSpoolerDoesNotRollEmptyBlock` 仍会**绿**（它断言的是「不滚动」）—— 这是正常的：该用例由 `PendingSeals` 自身的行为（计划 1 已冻结）保证，本集合不为它登记变异，但**必须在 `REQUIREMENTS` 里登记为 `checks`**：

```python
CHECKS = [
    {
        "name": "空块不滚动的守卫来自 PendingSeals（计划 1 冻结行为）",
        "file": "internal/tracearchive/writer.go",
        "pattern": r"if cur\.rows == 0 \{\s*\n\s*return nil, nil",
        "expect": "present",
        "why": (
            "「空块不滚动」由计划 1 的 PendingSeals 空块守卫提供（本计划不得修改该文件），"
            "因此这里用静态检查固定它的存在；若计划 1 的守卫被删，本检查会红。"
        ),
    },
]
```

（该 `pattern` 需按 `writer.go:375-380` 的实际文本核定后填写；`--check` 不校验 `CHECKS`，但权威表会跑它们。）

- [ ] **步骤 6：跑变异集合**

预期：5 条变异全部断言红；`CHECKS` 通过；三断言过；无未登记绿。
**`m01` 若 `--check` 报「定位到 0 处」**：改用更稳的写法——把 `info, err := os.Stat(opts.Root)` 这一整段用 `("cut", …)` 删掉并在前面插入一条 `if false { … }`，或直接把 `if !info.IsDir() {` 改为 `if false {`（后者更简单，但只覆盖「不是目录」那一半）。**两种都必须让 `TestSpoolerRefusesMissingRoot` 转红**；若其中一种不转红，说明用例只覆盖了一半，需要拆成两个用例。

- [ ] **步骤 7：提交**

```bash
git add internal/gateway/observability/spooler.go \
        tests/gateway/observability/spooler_test.go \
        tools/trace-mutate/sets/p2-task-5.py
git commit -m "feat(gateway): TraceSpooler 周期 flush 与 part 滚动（计划 2/4 任务 5）

规格 §7.1 周期 flush（≤1m，无条件）+ §15.1 R8 滚动（≤1h 且严格小于 minAge 3h）。
MarkSealed 无返回值、只关句柄；空块上不滚动（由 PendingSeals 的空块守卫保证），
避免留下 0 字节 spool 被封成 0 行产物。根目录必须已存在，绝不隐式 MkdirAll
（否则 root 进程会在容器可写层建出不在挂载点上的 /datalog，把 / 写满）。"
```

---

### Task 6：`TraceWriter` 写入路径接入（次序承重：先 spool 后 PG）

**文件：**
- 修改：`internal/gateway/observability/trace_event.go`（`TraceWriter` 结构 `:44-57`、`NewTraceWriterWithLimits` `:77`、`flush` 闭包 `:144-162`、`traceEventToModel` `:196-235`）
- 修改：`cmd/gateway/main.go`（`:253-264` 的 `traceWriter` 构造段）
- 测试：`tests/gateway/observability/trace_event_spool_test.go`（B 层）

**接口：**
- 依赖输入：任务 2 的 `config.TraceBodyConfig`（`Enabled` / `Inst` / `BlockMB` / `FlushInterval` / `RollInterval` / `DiskWatermarkPercent` / `DayOf`）、任务 3 的 `traceEventLine`、任务 5 的 `*TraceSpooler`
- 对外产出：
  ```go
  // TraceBodySink 是正文落盘目标（生产实现 *TraceSpooler）。
  // 用接口是为了让「先 spool 后 PG」这条次序能被测试替身观测到。
  type TraceBodySink interface {
      AppendLine(dt string, line []byte) (ta.Ref, error)
      DayOf(ts time.Time) string
  }
  // 新增构造器（保留既有 NewTraceWriter / NewTraceWriterWithLimits 不变）
  func NewTraceWriterWithSink(db *gorm.DB, bufSize, flushN int, interval time.Duration,
      limits TraceWriterLimits, sink TraceBodySink) *TraceWriter
  ```

- [ ] **步骤 1：写失败的测试（次序 + 降级 + 旧路径回归）**

新建 `tests/gateway/observability/trace_event_spool_test.go`：

```go
package observability_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// orderRecorder 用一个原子计数器把「两个动作发生的相对次序」变成可断言的事实。
// 这是本文件存在的首要理由：规格 §10.1 I1 要求「元数据行存在 ⇒ 产物 XOR spool 存在」，
// 它只在「先追加 spool、再写 PG」的次序下成立；反序会留下一条指向不存在正文的元数据行。
type orderRecorder struct {
	seq     atomic.Int64
	appends []string // 记录 dt，供 dt 口径断言
	mu      sync.Mutex
}

func (r *orderRecorder) next() int64 { return r.seq.Add(1) }

// fakeSink 实现 gwobs.TraceBodySink：记录追加次序，不碰磁盘。
type fakeSink struct {
	rec     *orderRecorder
	appendI []int64
	err     error
	missing bool
	zone    *time.Location
}

func (s *fakeSink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	s.appendI = append(s.appendI, s.rec.next())
	s.rec.mu.Lock()
	s.rec.appends = append(s.rec.appends, dt)
	s.rec.mu.Unlock()
	if s.err != nil {
		return ta.Ref{}, s.err
	}
	return ta.Ref{DT: dt, Inst: "blue", Part: 0, Row: len(s.appendI) - 1}, nil
}

func (s *fakeSink) DayOf(ts time.Time) string {
	loc := s.zone
	if loc == nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	return ts.In(loc).Format("2006-01-02")
}

// newSpoolTestDB 建内存 sqlite + 记录每次 INSERT 的序号。
func newSpoolTestDB(t *testing.T, rec *orderRecorder) (*gorm.DB, *[]int64) {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	var insertI []int64
	db.Callback().Create().Before("gorm:create").Register("test:order", func(tx *gorm.DB) {
		insertI = append(insertI, rec.next())
	})
	return db, &insertI
}

// TestTraceWriterAppendsSpoolBeforeMetadataRow 是**承重**用例（规格 §10.1 I1）：
// 每一次 flush 里，所有 AppendLine 必须先于任何一次 INSERT 发生。
// 若实现把次序写反，appendI 里会出现比它自己的 insert 更大的序号。
func TestTraceWriterAppendsSpoolBeforeMetadataRow(t *testing.T) {
	rec := &orderRecorder{}
	db, insertI := newSpoolTestDB(t, rec)
	sink := &fakeSink{rec: rec}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 3; i++ {
		w.Emit(gwobs.TraceEvent{
			RequestID:      "req-" + string(rune('a'+i)),
			TS:             time.Date(2026, 9, 24, 12, 0, i, 0, time.FixedZone("CST", 8*60*60)),
			RequestBodyRaw: []byte(`{"a":1}`),
		})
	}
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 3
	}, 5*time.Second, 10*time.Millisecond)

	require.Len(t, sink.appendI, 3)
	require.Len(t, *insertI, 3)
	for i, app := range sink.appendI {
		require.Less(t, app, (*insertI)[i], "第 %d 行：spool 追加（seq=%d）必须先于 PG 插入（seq=%d）", i, app, (*insertI)[i])
	}

	var rows []obsmodel.CallTrace
	require.NoError(t, db.Find(&rows).Error)
	for _, row := range rows {
		require.Nil(t, row.RequestBody, "新行不得再写正文 jsonb 列")
		require.Nil(t, row.RequestHeaders)
		require.Nil(t, row.ResponseBody)
		require.False(t, row.BodyMissing)
		require.NotNil(t, row.BodyRefDT)
		require.NotNil(t, row.BodyRefPart)
		require.NotNil(t, row.BodyRefRow)
		require.NotNil(t, row.BodyBytes)
		require.Equal(t, "2026-09-24", *row.BodyRefDT, "dt 必须按 UTC+8 口径")
		require.Equal(t, "blue", *row.BodyRefInst)
	}
}

// TestTraceWriterMarksBodyMissingWhenSpoolFails 固定 R9 的降级列：
// spool 失败 ⇒ body_missing=true、定位键全 NULL、PG 行**照常写入**。
func TestTraceWriterMarksBodyMissingWhenSpoolFails(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &fakeSink{rec: rec, err: errors.New("no space left on device")}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-missing", TS: time.Now(), RequestBodyRaw: []byte(`{"a":1}`)})

	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-missing").Error)
	require.True(t, row.BodyMissing)
	require.Nil(t, row.BodyRefDT)
	require.Nil(t, row.BodyRefInst)
	require.Nil(t, row.BodyRefPart)
	require.Nil(t, row.BodyRefRow)
	require.NotNil(t, row.BodyBytes, "body_bytes 是审计口径，缺失行也应记录原正文大小")
	require.Nil(t, row.RequestBody)
}

// TestTraceWriterLegacyPathKeepsBodiesInPG 是**回归**用例：
// sink == nil（功能关闭）时必须与本计划之前的行为逐字段相同 —— 正文仍进 jsonb 列。
func TestTraceWriterLegacyPathKeepsBodiesInPG(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{
		RequestID: "req-legacy", TS: time.Now(),
		RequestHeadersRaw: []byte(`{"h":"1"}`),
		RequestBodyRaw:    []byte(`{"a":1}`),
		ResponseBodyRaw:   []byte(`{"ok":true}`),
	})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-legacy").Error)
	require.NotNil(t, row.RequestHeaders)
	require.NotNil(t, row.RequestBody)
	require.NotNil(t, row.ResponseBody)
	require.False(t, row.BodyMissing)
	require.Nil(t, row.BodyRefDT)
}
```

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./tests/gateway/observability/ -run 'TestTraceWriter(Appends|Marks|Legacy)' -v`
预期：**编译失败**，`undefined: gwobs.TraceWriter` 的 `NewTraceWriterWithSink`。

- [ ] **步骤 3：实现 `TraceWriter` 侧改动**

在 `internal/gateway/observability/trace_event.go`：

**(a) import 增加** `ta "github.com/example/llm-gateway/internal/tracearchive"`。

**(b) 在 `TraceWriter` 结构体里加两个字段**（`flushN` 附近）：

```go
	// sink 是正文落盘目标。nil ⇒ 功能关闭，走既有路径（正文写进本表的 jsonb 列）。
	sink TraceBodySink
```

**(c) 新增 `TraceBodySink` 与 `NewTraceWriterWithSink`**：

```go
// TraceBodySink 是正文落盘目标（生产实现是 *TraceSpooler，见 spooler.go）。
//
// 【为什么用接口】「先追加 spool、再写 PG 元数据行」是规格 §10.1 I1 的前提
//（元数据行存在 ⇒ 产物 XOR spool 存在 或 body_missing=true），
// 必须有测试能观测到两个动作的相对次序。接口让测试可以注入一个记录次序的替身
//（tests/gateway/observability/trace_event_spool_test.go 的 orderRecorder），
// 而不必依赖真实磁盘的时序。
type TraceBodySink interface {
	AppendLine(dt string, line []byte) (ta.Ref, error)
	DayOf(ts time.Time) string
}

// NewTraceWriterWithSink 构造带正文落盘目标的写入器。
//
// sink 为 nil 时行为与 NewTraceWriterWithLimits 完全一致（功能关闭路径）。
func NewTraceWriterWithSink(db *gorm.DB, bufSize, flushN int, interval time.Duration,
	limits TraceWriterLimits, sink TraceBodySink) *TraceWriter {
	w := NewTraceWriterWithLimits(db, bufSize, flushN, interval, limits)
	w.sink = sink
	return w
}
```

**(d) 把 `traceEventToModel` 拆成「元数据部分」+「正文部分」**：把现有函数体的前半段（`m := obsmodel.CallTrace{RequestID: …, TS: …, 各计数与错误字段}`，即 `:197-213` 附近）抽成

```go
// traceEventToModelMeta 组装与正文无关的元数据字段（两条路径共用）。
func traceEventToModelMeta(e TraceEvent) obsmodel.CallTrace { /* 原样搬移，不改变任何赋值 */ }
```

并新增两个组装函数：

```go
// traceEventToModelWithRef 组装「正文在 spool 里」的元数据行（规格 §6）：
// 三个 jsonb 列恒为 NULL，定位键与 body_bytes 由本函数写入。
//
// BodyRefPart/BodyRefRow/BodyRefDT/BodyRefInst 取指针：part 0 与 row 0 都是
// 合法值，用值类型会让「定位键缺失」与「第 0 块第 0 行」不可区分。
func traceEventToModelWithRef(e TraceEvent, ref ta.Ref, bodyBytes int) obsmodel.CallTrace {
	m := traceEventToModelMeta(e)
	dt, inst, part, row := ref.DT, ref.Inst, ref.Part, ref.Row
	m.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &dt, &inst, &part, &row
	m.BodyBytes = &bodyBytes
	return m
}

// traceEventToModelMissing 组装「正文从未落盘」的元数据行（规格 §9.1 / §15.1 R9）：
// body_missing=true、四个定位键全 NULL。
func traceEventToModelMissing(e TraceEvent, bodyBytes int) obsmodel.CallTrace {
	m := traceEventToModelMeta(e)
	m.BodyMissing = true
	m.BodyBytes = &bodyBytes
	return m
}
```

**(e) 重写 `flush` 闭包内的组装循环**（`:144-162`），保持既有的「按字节限批 + 失败逐条降级」逻辑不动，只把「组装一行」换成次序承重的三段：

```go
		for i := range batch {
			e := batch[i]
			bodyBytes := int(traceEventBodyBytes(e))

			if tw.sink == nil {
				// 功能关闭：既有路径，逐字段不变。
				models = append(models, traceEventToModel(e))
				continue
			}

			// ① 先落 spool。次序**承重**（规格 §10.1 I1）：
			//    先写 PG 再落 spool，中间崩溃会留下一条「元数据说正文在某处、
			//    而那个地方什么都没有」的行 —— 读侧会报 ErrNotArchived（500），
			//    把一个正常降级伪装成存储故障。
			line := traceEventLine(e)
			dt := tw.sink.DayOf(e.TS)
			ref, err := tw.sink.AppendLine(dt, line)
			if err != nil {
				// ② 失败 ⇒ body_missing=true，定位键全 NULL，不重试（R9）。
				models = append(models, traceEventToModelMissing(e, bodyBytes))
				continue
			}
			// ③ 成功之后才组装带定位键的元数据行。
			models = append(models, traceEventToModelWithRef(e, ref, bodyBytes))
		}
```

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./tests/gateway/observability/ -run 'TestTraceWriter(Appends|Marks|Legacy)' -v`
预期：PASS（3 个顶层用例）。

- [ ] **步骤 5：接线 `cmd/gateway/main.go`**

把 `:252-264` 的 `var traceWriter *gwobs.TraceWriter … }` 段改成：

```go
	var traceWriter *gwobs.TraceWriter
	if db, err := cpinfra.InitPostgres(cfg.Database.URL); err != nil {
		slog.Warn("request trace persistence unavailable at startup; call logs remain spooled", "error", err)
	} else {
		traceDB := db.Session(&gorm.Session{Logger: newTraceDBLogger()})

		// 正文直写 /datalog（规格 §4.2 方案 C）。默认关闭（TRACE_BODY_ROOT 为空），
		// 此时 sink == nil，写入路径与本改动之前逐字节相同。
		var sink gwobs.TraceBodySink
		var spooler *gwobs.TraceSpooler
		if cfg.TraceBody.Enabled {
			gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
				Root:                 cfg.TraceBody.Root,
				DiskWatermarkPercent: cfg.TraceBody.DiskWatermarkPercent,
				ProbeInterval:        cfg.TraceBody.FlushInterval,
			})
			s, serr := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
				Root:          cfg.TraceBody.Root,
				Inst:          cfg.TraceBody.Inst,
				BlockBytes:    cfg.TraceBody.BlockMB << 20,
				RollInterval:  cfg.TraceBody.RollInterval,
				FlushInterval: cfg.TraceBody.FlushInterval,
				Gate:          gate,
			})
			if serr != nil {
				// 可服务性优先（规格 §9.3 G4）：网关继续启动，正文退回 PG 的 jsonb 列，
				// 而不是把正文标成 body_missing（那才是真的丢）。响亮记录。
				slog.Error("正文直写不可用，已退回 JSONB 正文列；请检查 /datalog 挂载与 TRACE_BODY_*", "error", serr)
			} else {
				sink, spooler = s, s
				slog.Info("trace body direct write enabled",
					"root", cfg.TraceBody.Root, "inst", cfg.TraceBody.Inst,
					"block_mb", cfg.TraceBody.BlockMB,
					"flush_interval", cfg.TraceBody.FlushInterval.String(),
					"roll_interval", cfg.TraceBody.RollInterval.String())
			}
		}

		traceWriter = gwobs.NewTraceWriterWithSink(traceDB, 1024, 50, 2*time.Second, gwobs.TraceWriterLimits{}, sink)
		go traceWriter.Run(ctx)
		if spooler != nil {
			go spooler.Run(ctx)
		}

		slog.Info("usage tracking enabled (direct-DB mode)")
	}
```

**注意**：`cfg.TraceBody.Inst` 的合法性在 `config.LoadTraceBodyConfig` 里已校验（非法值 panic ⇒ 启动失败），因此这里不做二次校验。

- [ ] **步骤 6：跑本包全部测试 + 全仓构建**

运行：
```bash
go build ./cmd/gateway/ ./internal/gateway/...
go test ./tests/gateway/observability/ -count=1
```
预期：构建 0 输出；测试**全绿**（含既有 8 个测试文件 —— 这一步同时是「没有破坏既有 trace 写入行为」的回归证据）。**注意**：`go build ./...` 在 `test/bedrock_model_id_check.go:98:23` 处**预先就失败**（与本次改动无关，已在基线 `1cc0ea8c` 上复现过），因此只构建受影响的包。

- [ ] **步骤 7：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-6.py`（`TARGET = "internal/gateway/observability/trace_event.go"`、`PACKAGES = ["./tests/gateway/observability/"]`）：

```python
MUTATIONS = {}

# 次序：先 spool 后 PG ⇒ TestTraceWriterAppendsSpoolBeforeMetadataRow 必须红。
# 把「先落 spool」改成「先组装元数据行」（即把 AppendLine 移到组装之后），
# 是这条不变量唯一的破坏方式。
MUTATIONS["m01_先写PG后落spool"] = [
    ("sub", "ref, err := tw.sink.AppendLine(dt, line)", "var ref ta.Ref; var err error\n\t\t\t_ = dt"),
]

# 降级列 ⇒ TestTraceWriterMarksBodyMissingWhenSpoolFails 必须红
MUTATIONS["m02_落盘失败不标body_missing"] = [
    ("sub", "models = append(models, traceEventToModelMissing(e, bodyBytes))",
            "models = append(models, traceEventToModelWithRef(e, ta.Ref{DT: dt, Inst: \"blue\"}, bodyBytes))"),
]

# 新行不得写正文列 ⇒ TestTraceWriter(Appends|Marks) 必须红
MUTATIONS["m03_新行仍然写正文列"] = [
    ("sub", "m := traceEventToModelMeta(e)\n\tdt, inst, part, row := ref.DT, ref.Inst, ref.Part, ref.Row",
            "m := traceEventToModel(e)\n\tdt, inst, part, row := ref.DT, ref.Inst, ref.Part, ref.Row"),
]

# 功能关闭时必须走旧路径 ⇒ TestTraceWriterLegacyPathKeepsBodiesInPG 必须红
MUTATIONS["m04_关闭时也走新路径"] = [
    ("sub", "if tw.sink == nil {", "if false {"),
]

# dt 必须来自 sink 的口径（UTC+8），不是 UTC 的日期 ⇒
# TestTraceWriterAppendsSpoolBeforeMetadataRow 的 dt 断言必须红
MUTATIONS["m05_dt改用UTC"] = [
    ("sub", "dt := tw.sink.DayOf(e.TS)", "dt := e.TS.UTC().Format(\"2006-01-02\")"),
]
```

- [ ] **步骤 8：跑变异集合**

预期：5 条断言红；三断言过；无未登记绿。
**`m01` 的写法风险**：`("sub", …)` 只替换调用点，不搬动语句 ⇒ 变量 `ref`/`err` 会变成零值，后续分支会走「失败」路径。这条变异仍然让次序用例转红（因为 `appendI` 为空 ⇒ `require.Len` 失败），但它同时让另外两个用例也红——**这不是问题**（变异可以杀死多条用例），**但**必须在 `note` 里说明它同时改变了控制流，否则会被误读成「次序守卫承重」。若希望更干净的次序变异，用 `("cut", "ref, err := tw.sink.AppendLine(dt, line)", "")` 并同时用第二条变异搬动插入位置；两种都必须在报告里写清自证串。

- [ ] **步骤 9：提交**

```bash
git add internal/gateway/observability/trace_event.go cmd/gateway/main.go \
        tests/gateway/observability/trace_event_spool_test.go \
        tools/trace-mutate/sets/p2-task-6.py
git commit -m "feat(gateway): TraceWriter 接入正文直写（先 spool 后 PG）（计划 2/4 任务 6）

- 次序承重（规格 §10.1 I1）：先 appendLine、再组装带定位键的元数据行
- 新行三列正文恒 NULL，改写 body_ref_dt/inst/part/row + body_bytes
- spool 失败 ⇒ body_missing=true、定位键全 NULL、不重试（§15.1 R9）
- sink == nil（TRACE_BODY_ROOT 为空）时逐字段保持既有行为；新增回归用例
- main.go：按配置接线，spooler 构造失败只记 ERROR 并退回 jsonb，不阻断启动"
```

---

### Task 7：降级可观测性（`body_missing` 信号，规格 §9.3）

**文件：**
- 修改：`internal/gateway/observability/trace_event.go`
- 测试：`tests/gateway/observability/trace_degradation_test.go`（B 层）

**接口：**
- 依赖输入：任务 6 的 `flush` 循环与 `ErrSpoolLatched`
- 对外产出：
  ```go
  type DegradationStats struct {
      MissingRows   int64      // 累计被标 body_missing 的行数
      SpoolFailures int64      // 累计 spool 写入失败次数（含闰锁期间的每一行）
      Latched       bool
      LatchedSince  time.Time
  }
  // 零值 = log.Printf / time.Now / 5m。测试注入日志与时钟。
  type DegradationOptions struct {
      Logf            func(format string, args ...any)
      Now             func() time.Time
      SummaryInterval time.Duration
  }
  func (w *TraceWriter) ConfigureDegradation(opts DegradationOptions)
  func (w *TraceWriter) DegradationStats() DegradationStats
  ```

**为什么单列一个任务**：现有 `flush` 失败路径**只有** `log.Printf`（`trace_event.go:151-160`），**无重试、无告警、无指标**。规格 §9.3 明确要求降级有可观测信号；而「降级本身写对了」与「降级能被运维看见」是两个可以分别做对/做错的验收面，因此分开。

- [ ] **步骤 1：写失败的测试**

新建 `tests/gateway/observability/trace_degradation_test.go`：

```go
package observability_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/require"
)

// logCapture 收集写入的日志行；配合注入的时钟做节流断言。
type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *logCapture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *logCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func (c *logCapture) countContaining(sub string) int {
	n := 0
	for _, l := range c.snapshot() {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// TestDegradationLogsLatchExactlyOnce：闰锁跳变必须恰好一条 ERROR，
// 而不是每一行一条 —— 否则盘满时日志自己的写入会把盘写得更满
//（2026-09-24 那次 `/` 盘被写满正是「失败路径打全量日志」造成的，
// 事后用 log_min_error_statement='panic' 才止住）。
func TestDegradationLogsLatchExactlyOnce(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	now := time.Unix(0, 0)
	sink := &fakeSink{rec: rec, err: fmt.Errorf("%w", gwobs.ErrSpoolLatched)}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{
		Logf: capture.logf, Now: func() time.Time { return now }, SummaryInterval: 5 * time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 20; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: time.Now()})
	}
	require.Eventually(t, func() bool {
		return w.DegradationStats().MissingRows == 20
	}, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, 1, capture.countContaining("闰锁"), "跳变日志必须恰好一条")
	require.Equal(t, 1, capture.countContaining("降级为 body_missing"), "只允许一条进入降级的 ERROR")
	st := w.DegradationStats()
	require.True(t, st.Latched)
	require.Equal(t, int64(20), st.MissingRows)
	require.Equal(t, int64(20), st.SpoolFailures)
}

// TestDegradationLogsRecovery：恢复（重新可写）也必须有一条明确信号。
func TestDegradationLogsRecovery(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	sink := &fakeSink{rec: rec, err: fmt.Errorf("%w", gwobs.ErrSpoolLatched)}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-1", TS: time.Now()})
	require.Eventually(t, func() bool { return w.DegradationStats().Latched }, 5*time.Second, 10*time.Millisecond)

	sink.err = nil // 空间已释放
	w.Emit(gwobs.TraceEvent{RequestID: "req-2", TS: time.Now()})
	require.Eventually(t, func() bool { return !w.DegradationStats().Latched }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, capture.countContaining("恢复"), "恢复必须有且只有一条信号")
}

// TestDegradationSummaryIsThrottled：闰锁期间按 SummaryInterval 输出汇总，
// 而不是每个 flush 一条。
func TestDegradationSummaryIsThrottled(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	now := time.Unix(0, 0)
	sink := &fakeSink{rec: rec, err: fmt.Errorf("%w", gwobs.ErrSpoolLatched)}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{
		Logf: capture.logf, Now: func() time.Time { return now }, SummaryInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 5; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: time.Now()})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 5 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond) // 期间会跑很多个 flush 周期
	require.LessOrEqual(t, capture.countContaining("汇总"), 1, "未到 SummaryInterval 不得重复输出汇总")

	now = now.Add(2 * time.Hour)
	require.Eventually(t, func() bool { return capture.countContaining("汇总") >= 1 }, 5*time.Second, 10*time.Millisecond)
}
```

- [ ] **步骤 2：运行测试并确认它失败**

运行：`go test ./tests/gateway/observability/ -run 'TestDegradation' -v`
预期：**编译失败**，`undefined: gwobs.DegradationOptions` / `DegradationStats` / `w.ConfigureDegradation`。

- [ ] **步骤 3：写最小实现**

在 `internal/gateway/observability/trace_event.go` 的 `TraceWriter` 结构体加字段：

```go
	// 降级可观测性（规格 §9.3）：跳变/恢复各一条日志，闰锁期间按周期一条汇总。
	degLogf      func(format string, args ...any)
	degNow       func() time.Time
	degSummary   time.Duration
	degLatched   bool
	degSince     time.Time
	degLastSum   time.Time
	missingRows  atomic.Int64
	spoolFailure atomic.Int64
```

**注意**：`sync/atomic` 需要 Go 1.19+ 的原子类型（本项目为 Go 1.26，可直接用 `atomic.Int64`）；`TraceWriter` 目前按值/指针使用需确认不会触发 copylock 告警（它是一个被 `go` 出去的指针，安全）。

新增：

```go
// DegradationOptions 让测试注入日志与时钟；零值 = log.Printf / time.Now / 5m。
type DegradationOptions struct {
	Logf            func(format string, args ...any)
	Now             func() time.Time
	SummaryInterval time.Duration
}

// ConfigureDegradation 覆盖降级观测的日志与时钟（生产不调用，用零值）。
func (w *TraceWriter) ConfigureDegradation(opts DegradationOptions) {
	if opts.Logf != nil {
		w.degLogf = opts.Logf
	}
	if opts.Now != nil {
		w.degNow = opts.Now
	}
	if opts.SummaryInterval > 0 {
		w.degSummary = opts.SummaryInterval
	}
}

// DegradationStats 返回降级计数（规格 §9.3：降级必须有明确可观测信号）。
func (w *TraceWriter) DegradationStats() DegradationStats {
	return DegradationStats{
		MissingRows:   w.missingRows.Load(),
		SpoolFailures: w.spoolFailure.Load(),
		Latched:       w.degLatched,
		LatchedSince:  w.degSince,
	}
}

// noteSpoolFailure 记录一次 spool 失败，并按状态机输出日志：
//   未闰锁 → 闰锁：一条 ERROR（说明原因、影响与恢复条件）
//   闰锁中      ：按 SummaryInterval 输出汇总
//   闰锁 → 恢复：一条 INFO
//
// 【为什么必须节流】2026-09-24 的事故里，失败路径对每个请求打印完整语句，
// 一天写了 244 GB —— 日志本身成了把盘写满的原因。降级日志必须与降级同量级：
// 盘满时每秒可能有上千行降级，绝不能每行一条。
func (w *TraceWriter) noteSpoolFailure(err error) {
	w.spoolFailure.Add(1)
	w.missingRows.Add(1)

	now := w.now()
	latched := errors.Is(err, ErrSpoolLatched)

	if !w.degLatched {
		w.degLatched, w.degSince, w.degLastSum = true, now, now
		w.logf("[trace-body] ERROR 正文落盘已闰锁，开始降级为 body_missing=true（网关继续服务）：%v；"+
			"恢复条件 = 探针可写 且 磁盘水位回落，按周期自动探测", err)
		return
	}
	if !latched && now.Sub(w.degLastSum) >= w.summary() {
		w.degLastSum = now
		w.logf("[trace-body] WARN 正文落盘仍不可用（已闰锁 %s）：累计 body_missing 行=%d，"+
			"恢复条件 = 探针可写 且 水位回落",
			now.Sub(w.degSince).Truncate(time.Second), w.missingRows.Load())
	}
	_ = latched
}

// noteSpoolSuccess 在恢复后写入第一行成功时调用。
func (w *TraceWriter) noteSpoolSuccess() {
	if !w.degLatched {
		return
	}
	w.degLatched = false
	w.logf("[trace-body] INFO 正文落盘已恢复，body_missing 降级结束（闰锁时长 %s，累计缺失 %d 行）",
		w.now().Sub(w.degSince).Truncate(time.Second), w.missingRows.Load())
}

func (w *TraceWriter) now() time.Time {
	if w.degNow != nil {
		return w.degNow()
	}
	return time.Now()
}

func (w *TraceWriter) summary() time.Duration {
	if w.degSummary > 0 {
		return w.degSummary
	}
	return 5 * time.Minute
}

func (w *TraceWriter) logf(format string, args ...any) {
	if w.degLogf != nil {
		w.degLogf(format, args...)
		return
	}
	log.Printf(format, args...)
}
```

并在任务 6 的 `flush` 循环里接上两处调用（成功与失败各一处）：

```go
			if err != nil {
				tw.noteSpoolFailure(err)
				models = append(models, traceEventToModelMissing(e, bodyBytes))
				continue
			}
			tw.noteSpoolSuccess()
```

- [ ] **步骤 4：运行测试并确认它通过**

运行：`go test ./tests/gateway/observability/ -run 'TestDegradation' -v`
预期：PASS（3 个顶层用例）。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-7.py`（`TARGET = "internal/gateway/observability/trace_event.go"`）。四条变异：

```python
MUTATIONS = {}

# 跳变只报一次 ⇒ TestDegradationLogsLatchExactlyOnce 必须红
MUTATIONS["m01_每次失败都打跳变日志"] = [
    ("sub", "if !w.degLatched {\n\t\tw.degLatched, w.degSince, w.degLastSum = true, now, now",
            "if true {\n\t\tw.degLatched, w.degSince, w.degLastSum = true, now, now"),
]
# 恢复信号 ⇒ TestDegradationLogsRecovery 必须红
MUTATIONS["m02_恢复不记录"] = [
    ("sub", "if !w.degLatched {\n\t\treturn\n\t}\n\tw.degLatched = false", "if true {\n\t\treturn\n\t}\n\tw.degLatched = false"),
]
# 汇总节流 ⇒ TestDegradationSummaryIsThrottled 必须红
MUTATIONS["m03_汇总不节流"] = [
    ("sub", "if !latched && now.Sub(w.degLastSum) >= w.summary() {", "if true {"),
]
# 计数器必须真的累加 ⇒ TestDegradationLogsLatchExactlyOnce 的计数断言必须红
MUTATIONS["m04_不累加missingRows"] = [
    ("sub", "w.missingRows.Add(1)", "_ = w.missingRows"),
]
```

预期：4 条断言红；三断言过；无未登记绿。

- [ ] **步骤 6：提交**

```bash
git add internal/gateway/observability/trace_event.go \
        tests/gateway/observability/trace_degradation_test.go \
        tools/trace-mutate/sets/p2-task-7.py
git commit -m "feat(gateway): body_missing 降级信号与节流日志（计划 2/4 任务 7）

规格 §9.3：降级必须有明确可观测信号。闰锁跳变/恢复各一条日志，
闰锁期间按 SummaryInterval（默认 5m）一条汇总，绝不每行一条 ——
2026-09-24 的 / 盘写满正是失败路径全量打日志造成的（244 GB/天）。"
```

---

### Task 8：验收 —— A1 写入半部 + A3 退化（注入式）

**文件：**
- 测试：`tests/gateway/observability/trace_body_acceptance_test.go`（B 层）

**验收范围（**本计划只承担这两条，其余显式排除**）**：

| 规格 §14.2 | 内容 | 归属 |
|---|---|---|
| **A1（写入半部）** | 事件 → spool 行的**逐字节**对应 + 元数据行定位键正确 | **本计划**（读回半部见计划 3） |
| A2 | 封块产物可点读、索引正确 | 计划 3 |
| **A3** | 填满 `/datalog` ⇒ 退化到 `body_missing`、网关继续服务、延迟无劣化 | **本计划（注入式等价用例）+ 计划 4（真实盘满）** |
| A6 | 压缩比与解码内存实测 | 计划 4 |
| A7 | 跨进程（sealer 与网关并发）验证 | 计划 4 |
| A8 | 行数分布（防止超 `maxTrailerEntries`） | 计划 4 |
| A9 | 冷热分层 | 未来计划 |

- [ ] **步骤 1：写 A1 写入半部用例**

```go
// TestAcceptanceA1WriteHalf 是规格 §14.2 A1 的**写入半部**：
// 用真实 TraceSpooler（临时目录，非替身）+ 内存 sqlite，逐条比对
// 「事件 → spool 行」的字节与「事件 → 元数据行」的定位键。
//
// 读回半部（按 (dt, inst, part, row) 取回正文并逐字节比对）属计划 3 的点读服务，
// 本用例只断言「写进去的字节与写出来的键是对得上的」：
// 这是读写两侧唯一能在写入侧独立固定的契约。
func TestAcceptanceA1WriteHalf(t *testing.T) {
	const n = 2000
	root := t.TempDir()
	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue",
		BlockBytes: 1 << 20, RollInterval: time.Hour, FlushInterval: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })
	require.NoError(t, spooler.FlushAndRoll())

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	// 用真实 spooler 作为 sink：DayOf 由测试侧的 config 口径提供。
	sink := &sinkWithDayOf{spooler: spooler, zone: time.FixedZone("CST", 8*60*60)}
	w := gwobs.NewTraceWriterWithSink(db, 4096, 200, 50*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	base := time.Date(2026, 9, 24, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	for i := 0; i < n; i++ {
		w.Emit(gwobs.TraceEvent{
			RequestID:      fmt.Sprintf("req-%05d", i),
			TS:             base.Add(time.Duration(i) * time.Second),
			RequestHeadersRaw: []byte(`{"x-user":"u"}`),
			RequestBodyRaw:    []byte(fmt.Sprintf(`{"i":%d}`, i)),
			ResponseBodyRaw:   []byte(`{"ok":true}`),
		})
	}
	require.Eventually(t, func() bool {
		var c int64
		db.Model(&obsmodel.CallTrace{}).Count(&c)
		return c == n
	}, 30*time.Second, 50*time.Millisecond)
	require.NoError(t, spooler.FlushAndRoll())

	var rows []obsmodel.CallTrace
	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, n)

	// ① 每一行都必须有完整定位键，且不得有正文列、不得是 body_missing
	for i, row := range rows {
		require.False(t, row.BodyMissing, "第 %d 行不应降级", i)
		require.Nil(t, row.RequestBody)
		require.Nil(t, row.ResponseBody)
		require.NotNil(t, row.BodyRefDT)
		require.NotNil(t, row.BodyRefInst)
		require.NotNil(t, row.BodyRefPart)
		require.NotNil(t, row.BodyRefRow)
		require.Equal(t, "2026-09-24", *row.BodyRefDT, "第 %d 行的 dt 必须是 UTC+8 的日期", i)
	}

	// ② 定位键必须能落回真实存在的 spool 行，且字节与事件一致（逐字节）
	for i, row := range rows {
		spoolPath := filepath.Join(root, "spool",
			"dt="+*row.BodyRefDT, "inst="+*row.BodyRefInst,
			fmt.Sprintf("part-%06d.jsonl", *row.BodyRefPart))
		lines := splitLines(t, spoolPath)
		require.Greater(t, len(lines), *row.BodyRefRow, "第 %d 行：row=%d 越界（块内共 %d 行）",
			i, *row.BodyRefRow, len(lines))
		require.Contains(t, lines[*row.BodyRefRow], fmt.Sprintf(`"i":%d`, i),
			"第 %d 行：定位键指到的行不是这一条事件", i)
	}

	// ③ body_bytes 口径不含 headers（登记 D42）
	for i, row := range rows {
		require.NotNil(t, row.BodyBytes)
		want := len(fmt.Sprintf(`{"i":%d}`, i)) + len(`{"ok":true}`)
		require.Equal(t, want, *row.BodyBytes, "第 %d 行 body_bytes 口径不符", i)
	}
}

// sinkWithDayOf 把真实 spooler 与测试侧的 dt 口径装在一起（生产里这两件事
// 由 main.go 组装：TraceSpooler 实现 AppendLine，config.TraceBodyConfig 提供 DayOf）。
type sinkWithDayOf struct {
	spooler *gwobs.TraceSpooler
	zone    *time.Location
}

func (s *sinkWithDayOf) AppendLine(dt string, line []byte) (ta.Ref, error) {
	return s.spooler.AppendLine(dt, line)
}

func (s *sinkWithDayOf) DayOf(ts time.Time) string { return ts.In(s.zone).Format("2006-01-02") }

func splitLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
```

- [ ] **步骤 2：写 A3 注入式退化用例**

```go
// TestAcceptanceA3DegradesWithoutDiskPressure 是规格 §14.2 A3 的**注入式等价用例**。
//
// 【为什么不真的填满盘】真实盘满不可复现、且会把跑测试的机器写坏；
// 「填满 /datalog」这一条本身属计划 4 的生产验证。这里用可注入的水位函数
// 把系统推到与盘满**同一状态**：闸门闰锁 ⇒ 每一行 body_missing。
// 等价的是「状态」，不是「成因」——报告里必须这样写，不得宣称已验证真实盘满。
func TestAcceptanceA3DegradesWithoutDiskPressure(t *testing.T) {
	root := t.TempDir()
	gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 root,
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Minute,
		UsagePercent:         func(string) (float64, error) { return 99, nil }, // 恒超水位
		ProbeWrite:           func(string) error { return nil },
	})
	gate.Tick(time.Now()) // 未闰锁 → 闰锁

	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute, Gate: gate,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })

	rec := &orderRecorder{}
	db, insertI := newSpoolTestDB(t, rec)
	sink := &sinkWithDayOf{spooler: spooler, zone: time.FixedZone("CST", 8*60*60)}
	w := gwobs.NewTraceWriterWithSink(db, 1024, 50, 50*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	const n = 500
	start := time.Now()
	for i := 0; i < n; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: time.Now(), RequestBodyRaw: []byte(`{"a":1}`)})
	}
	emitElapsed := time.Since(start)

	require.Eventually(t, func() bool {
		var c int64
		db.Model(&obsmodel.CallTrace{}).Count(&c)
		return c == n
	}, 30*time.Second, 20*time.Millisecond)

	// ① 网关继续服务：元数据行照常写入（不是丢掉整条 trace）
	var rows []obsmodel.CallTrace
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, n)
	for i, row := range rows {
		require.True(t, row.BodyMissing, "第 %d 行必须降级为 body_missing", i)
		require.Nil(t, row.BodyRefDT)
		require.Nil(t, row.BodyRefPart)
		require.Nil(t, row.BodyRefRow)
		require.Nil(t, row.BodyBytes == nil) // 仅确保字段可读；口径断言在 A1
	}
	require.Len(t, *insertI, n, "每个事件都必须有一条元数据行")

	// ② 闰锁期间不得有任何 spool 落盘（R9：不重试、不目录扫描）
	require.NoFileExists(t, filepath.Join(root, "spool", "dt="+time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")))

	// ③ 延迟无劣化：热路径 Emit 是纯内存入队（500 ms 阻塞上限是既有契约），
	//    500 次 Emit 的总耗时必须在毫秒量级；真正的落盘全在写入协程里。
	require.Less(t, emitElapsed, 500*time.Millisecond,
		"Emit 不得因为落盘失败而退化：热路径必须仍是内存入队（实测 %s）", emitElapsed)

	st := w.DegradationStats()
	require.True(t, st.Latched)
	require.Equal(t, int64(n), st.MissingRows)
}
```

- [ ] **步骤 3：运行测试**

运行：`go test ./tests/gateway/observability/ -run 'TestAcceptanceA' -v`
预期：先**编译失败**（还没写 `sinkWithDayOf` 以外的实现时），实现齐全后 **PASS**（2 个顶层用例）。
**若 `TestAcceptanceA1WriteHalf` 的 ② 或 ③ 转红**：不要放宽断言 —— 那正是 A1 要抓的东西（定位键错位 / `body_bytes` 口径漂移）。先定位是实现问题还是规格缺陷（若是规格问题，补进「规格缺陷登记」再裁定）。

- [ ] **步骤 4：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p2-task-8.py`（`TARGET = "internal/gateway/observability/trace_event.go"`，本任务只写测试、无新生产代码 ⇒ 与计划 1 的 `task-8.py` 同一形态：变异打在既有生产代码上）：

```python
MUTATIONS = {}

# 定位键必须真的写进元数据行 ⇒ A1 必须红
MUTATIONS["m01_不写定位键"] = [
    ("sub", "m.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &dt, &inst, &part, &row",
            "_ = dt; _ = inst; _ = part; _ = row"),
]
# body_bytes 口径 ⇒ A1 ③ 必须红
MUTATIONS["m02_body_bytes含headers"] = [
    ("sub", "bodyBytes := int(traceEventBodyBytes(e))",
            "bodyBytes := int(traceEventBodyBytes(e)) + len(e.RequestHeadersRaw)"),
]
# 降级行不得带定位键 ⇒ A3 必须红
MUTATIONS["m03_降级行也带定位键"] = [
    ("sub", "m.BodyMissing = true", "m.BodyMissing = true\n\tdt0, inst0, p0, r0 := \"x\", \"blue\", 0, 0\n\tm.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &dt0, &inst0, &p0, &r0"),
]
# 闰锁期间不得落盘（R9）⇒ A3 ② 必须红
MUTATIONS["m04_闰锁期间仍写盘"] = [
    ("sub", "if !s.gate.Allow() {", "if false {"),
]
```

- [ ] **步骤 5：跑变异集合**

预期：4 条断言红（其中 `m04` 的目标在 `spooler.go`，需把本集合的 `TARGET` 设为 `spooler.go`，或拆成 `p2-task-8-spooler.py` 两份 —— **与计划 1 的 `task-8.py` + `task-8-reader.py` 同一形态**：工具一次只接受一个 `--target`）；三断言过；无未登记绿。

- [ ] **步骤 6：提交**

```bash
git add tests/gateway/observability/trace_body_acceptance_test.go \
        tools/trace-mutate/sets/p2-task-8*.py
git commit -m "test(gateway): A1 写入半部与 A3 注入式退化验收（计划 2/4 任务 8）

A1：2000 条事件逐条比对定位键与 spool 行字节，body_bytes 口径不含 headers。
A3：注入水位把系统推到与盘满同一状态（不是真实盘满，报告须写明），
断言每行 body_missing、闰锁期间零落盘、Emit 仍在毫秒量级。
真实盘满的 A3 与 A2/A6/A7/A8/A9 分别属计划 3 / 计划 4。"
```

---

### Task 9：分支级证据重跑（R11 式）

**文件：**
- 新建（证据目录，gitignored）：`.superpowers/sdd/trace-write-path/R11-v1.md`
- 不改任何生产代码

**为什么必须有这一步**：CI 完全不跑 `go test`（`.gitlab-ci.yml` 只有三处 `CGO_ENABLED=0 go build`），因此**逐任务**的变异表是过程证据，而**同一棵树上的全任务重跑**才是最终结论——它同时自查「后续任务的改动有没有让前面任务的守卫失效」。

- [ ] **步骤 1：冻结提交**

确认工作树干净、HEAD 即本轮结论所绑定的提交：

```bash
git status --porcelain          # 必须为空
git rev-parse HEAD              # 记下这个哈希，写进报告头部
git log --oneline -12
```

- [ ] **步骤 2：逐集合跑权威表**

对 `tools/trace-mutate/sets/p2-task-{2,3,4,5,6,7,8,8-spooler}.py` **各跑一次**（每次都要它自己的名字清单）：

```bash
for s in 2 3 4 5 6 7 8 8-spooler; do
  set=tools/trace-mutate/sets/p2-task-$s.py
  [ -f "$set" ] || continue
  python3 tools/trace-mutate/trace_mutate.py --repo . \
    --manifest .superpowers/sdd/trace-write-path/p2-task-$s-names.txt \
    --mutation-set "$set" > /tmp/p2-task-$s.out 2>&1
  echo "task-$s exit=$?"
done
```

- [ ] **步骤 3：写报告 `.superpowers/sdd/trace-write-path/R11-v1.md`**

报告固定结构（**事实，不是声明**）：

1. **绑定**：`git rev-parse HEAD`、`git status --porcelain` 为空、工具/集合/清单三者 sha256（从工具头部原样抄）、pristine 树来源。
2. **计数口径**：断言支撑的红 / 崩溃超时红 / 允许的绿 / 未登记的绿 / 证据无效，逐集合列出（**不要把崩溃红与断言红合并计数**）。
3. **三断言**：逐集合声明 `names==ran==清单`、`PASS+FAIL+SKIP == 清单条数`、全层级 `SKIP == 0`。
4. **静态检查**：逐条抄 `CHECKS` 的命令与真实输出。
5. **覆盖缺口**（**必须诚实列出，不得留白**）：
   - `FlushAndRoll` 上抛 flush 错误的分支未被新用例覆盖（触发它需要在 fd 打开之后制造写失败，如真实 ENOSPC/EMFILE；该分支的行为由计划 1 对 `PendingSeals` 的既有证据覆盖）；
   - A3 为注入式等价（真实盘满属计划 4）；
   - 跨进程（sealer 并发）与压缩比实测属计划 4。
6. **与计划 1 证据的关系**：本计划**未修改** `internal/tracearchive/` 下任何文件（`git diff --stat 3a2301ad..HEAD -- internal/tracearchive/` 必须为空）⇒ R11 v3（142 条，冻结提交 `ea962a70`）的被测文件哈希未变，**其绑定仍然有效**，无需重跑。这一条必须以命令输出为证，不能只写结论。

- [ ] **步骤 4：把报告与最终提交一起交付**

```bash
git diff --stat 3a2301ad..HEAD -- internal/tracearchive/   # 必须为空
git log --oneline 3a2301ad..HEAD
```

（`.superpowers/` 是 gitignored 的证据目录，不进版本库；报告以文件路径交付给发起者。）

---

## 规格缺陷登记（本计划自成一套编号，自 D42 起）

> 每条给出：规格原文位置 → 冲突/缺口 → **本计划的裁定**。执行者按裁定实现；若认为裁定有误，写进交付报告，不要改动本计划。

| # | 位置 | 问题 | 本计划裁定 |
|---|---|---|---|
| **D42** | §6（`body_bytes` 定义） | 规格写 `len(request_headers) + len(request_body) + len(response_body)`（**含** headers），而既有 `traceEventBodyBytes`（`trace_event.go:97-99`）是 `len(RequestBodyRaw) + len(ResponseBodyRaw)`（**不含**）。同一名字两个口径。 | 取**不含 headers** 的既有口径，复用 `traceEventBodyBytes`。理由：① 与既有的字节闸门（`maxQueueBytes`/`maxBatchBytes`）同口径，避免同名两个数；② headers 经 scrub 过滤、体积为 KB 级，对压缩比核算影响可忽略；③ 只有一个计数器才不会漂移。**规格待订正为同一公式**（订正属发起者决策，不在本计划内）。 |
| **D43** | §11.1（网关环境变量清单） | 清单里有 `TRACE_BODY_ZSTD_LEVEL` / `TRACE_BODY_ZSTD_WINDOW_MB`，但方案 C 下**编码不在网关**（§4.2 / §5.3.1.1），网关侧无消费者。 | 网关**不读**这两个变量；它们属于计划 4 的 sealer 进程。计划 2 的实现与测试都不涉及。 |
| **D44** | §11.1 vs §7.2/§15.1 | §11.1 的示例给 `TRACE_BODY_SEAL_INTERVAL: "15m"`，而 §7.2 的推导要求「网关滚动间隔取 ~1 小时，让**大小**阈值先触发，以保住已标定的 10.26× 压缩比（滚动过短会把块截成 ~400 MB）」。15m 满足 R8 的 ≤1h，但破坏压缩比标定。 | 默认 **`1h`**：`SealInterval` 同时是滚动间隔；大小阈值（1536 MB）在满速下约 1h 触发，二者一致。§11.1 的 `"15m"` 视为示例值错误。 |
| **D45** | §9.2 / §15.1 R9 | 提到 `disk_watermark_percent`，但**全仓不存在**该配置（`.env.example` / `config/*.yaml` / Go 代码 0 命中），规格也没给默认值、单位与生效位置。 | 新增 `TRACE_BODY_DISK_WATERMARK_PERCENT`，**默认 90**，单位 = `TRACE_BODY_ROOT` 所在文件系统的已用百分比（`statfs` 的 `(Blocks-Bavail)/Blocks`），**每 `FlushInterval` 评估一次**（不在请求路径上）。范围校验 1..99：0 会让每个请求都降级，100 等于不设水位。 |
| **D46** | `AGENTS.md` vs `cmd/gateway/main.go:248-264` | `AGENTS.md` 写「gateway **不要**直连数据库」，而 `main.go:260` 构造 `NewTraceWriterWithLimits(traceDB, …)` 直写 PG，且 `main.go:263` 自报 `"usage tracking enabled (direct-DB mode)"`。 | **记录，不修**。本计划延续现状（只改写入的字段），不改 `AGENTS.md`、不动 `NewTraceWriter` 的既有 DB 依赖。订正文档属发起者决策。 |
| **D47** | §4.1（数据流 5 步） | 步骤③「PG 元数据行」与步骤④「写 PG」的**相对次序**没有写死；而 §10.1 I1（元数据行存在 ⇒ 产物 XOR spool 存在 或 body_missing=true）**只有在「先落盘、后写元数据」下才成立**。反序会在中间崩溃时留下「元数据指向不存在正文」的行，读侧报 `ErrNotArchived`（500），把降级伪装成存储故障。 | 次序**写死为本计划的任务 6**：先 `AppendLine`、再组装并插入元数据行；由 `TestTraceWriterAppendsSpoolBeforeMetadataRow` 用次序记录器固定。规格建议补一句显式次序要求。 |
| **D48** | §14.2 A3 / A1 | A3「填满 `/datalog`」与 A1 的 100 万行往返在**本计划范围内不可完全自动化**（真实盘满不可复现；100 万行往返还依赖读侧）。 | A3 在本计划内实现为**注入式等价用例**（断言「与盘满同一状态」下的 `body_missing`、零落盘、延迟不劣化），并在报告与提交信息里**明确不得宣称已验证真实盘满**；真实盘满、A1 读回半部、A2/A6/A7/A8/A9 归计划 3/4。 |

---

## 自检结果

**规格覆盖**（逐条对照规格 §2/§4/§6/§7/§9/§10/§11/§14/§15/§16）：

| 规格条目 | 落在哪个任务 |
|---|---|
| §6 六列 schema | 任务 1 |
| §4.2 组件命名与边界（`internal/gateway/observability/trace_event.go` 是改造点） | 任务 6（改造点）+ 任务 5（新增 spooler，依赖方向仍为 gateway → tracearchive，单向） |
| §4.1 步骤①（整行 JSON、字段与顺序对齐归档格式） | 任务 3 |
| §4.1 步骤②③④ 与 §10.1 I1（先落盘后写元数据） | 任务 6（次序断言） |
| §5.1 目录布局（`dt`/`inst` 分量） | 任务 2（`inst` 白名单 + `dt` 口径）+ 任务 5（`Paths` 由 tracearchive 提供） |
| §7.1 周期 flush（≤1m、无条件） | 任务 5（`FlushAndRoll` + `Run`） |
| §7.3 写指针（目录即指针） | 不实现（计划 1 已冻结，任务 5 只调用） |
| §9.1/§9.2 失败语义（降级 `body_missing`、恢复） | 任务 4（闰锁与恢复条件）+ 任务 6（列语义）+ 任务 7（信号） |
| §9.3 可观测性 | 任务 7 |
| §11.1 部署必改项（`/datalog` 挂载、`TRACE_BODY_FLUSH_INTERVAL`） | 本计划实现 env 读取（任务 2）；**挂载与 compose 改动属计划 4**，计划 2 只记录依赖 |
| §14.1 分层（3 条方案 C 防线） | 任务 4（水位+闰锁）、任务 5（根目录必须存在）、任务 6（次序） |
| §14.2 A3 + A1 写入半部 | 任务 8（其余显式排除，见表） |
| §15.1 R8（滚动 ≤1h < minAge） | 任务 2（校验）+ 任务 5（实现 + 承重用例） |
| §15.1 R9（闰锁、不按请求重试） | 任务 4 + 任务 6 + 任务 8（A3） |
| §15.2 P1–P3 生产检查 | 计划 4 |
| §16 验收 7 项 | 见任务 8 的归属表 |

**占位符扫描**：全文无「待定」「TODO」「类似任务 N」「参照上文」「按需补充」。每处引用都是**具体文件 + 行号 + 真实符号**（`PendingSeals` `writer.go:353-393`、`MarkSealed` `writer.go:420-426`、`ensureBlockLocked` `writer.go:471-521`、`nextSeqLocked` `writer.go:527-544`、`traceEventBodyBytes` `trace_event.go:97`、`flush` 闭包 `trace_event.go:144-162`、`traceEventToModel` `trace_event.go:196-235`、`ObservabilityConfig` `config.go:103-104`/`:182-183`、接线点 `main.go:253-264`、迁移基线 `atlas-migrations/20260609082623_baseline.sql:113-131`）。所有变异集合都给了可据以编写的确切目标串或明确的目标语句。

**类型一致性**：`TraceBodySink.AppendLine` 返回 `(ta.Ref, error)`，与 `(*TraceSpooler).AppendLine` 一致；`DayOf(ts time.Time) string` 与 `config.TraceBodyConfig.DayOf` 一致；`ta.WriterOptions{Root, Inst, BlockBytes, SealInterval}` 与 `spooler.go` 的传入一致（字段名逐个核对过计划 1 的 `writer.go:113-120`）；`ta.Ref{DT, Inst, Part, Row}` 四个分量在任务 1 的四个指针列里一一对应；`PendingSeals()` 返回 `[]SealTask{DT, Inst, Part, SpoolPath, Rows, UncompressedBytes}`，任务 5 只读 `DT`/`Part`；`MarkSealed(dt string, part int)` **无返回值**——全文没有任何 `err := MarkSealed(...)` 的写法。

**范围自检**：本计划**不修改** `internal/tracearchive/` 任何文件（任务 9 步骤 3 用 `git diff --stat` 作为证据），不改 compose、不改 `AGENTS.md`、不新增 `call_traces` 保留策略、不动任何 HTTP 路由与 `tests/contract/contracts.json`。

---

## 执行顺序与依赖

```
任务 1（迁移，无依赖）
任务 2（配置/时区，无依赖）
任务 3（序列化，无依赖）—— A 层测试依赖任务 2 的时区常量
任务 4（闩锁，无依赖）
任务 5（spooler，依赖任务 4）
任务 6（写入路径接入，依赖任务 2/3/5）
任务 7（降级信号，依赖任务 6）
任务 8（验收，依赖任务 6/7）
任务 9（全任务证据重跑，依赖全部）
```

任务 1~4 相互独立，可并行；任务 5 起必须串行。
