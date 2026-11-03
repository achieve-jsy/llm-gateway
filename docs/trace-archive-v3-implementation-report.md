# trace 归档 v3 · 实现报告

> 分支 `feat/trace-archive-v3`，worktree `.worktrees/trace-archive-v3`（基线 main `1cc0ea8c`）。
> 设计：`docs/trace-archive-solution-v3.md`；接口基准：`docs/trace-archive-v3-implementation-contract.md`。
> 实现方式：10 个智能体分工（6 实现 + 1 集成 + 3 对抗校验），再 4 个智能体修复校验发现。

---

## 1. 提交历史

| commit | 内容 |
|---|---|
| `3c041d24` | chore: 忽略本地 `.worktrees/` |
| `9114761b` | feat(archive): 归档实现移植到 main 基线（自包含存储层 + 模型 + 写入侧抽列） |
| `50ba8745` | feat(db): 归档 schema（`call_traces` 归档列 + `trace_archive_manifests`） |
| `2cede695` | feat(archive): 归档 v3 实现（分块 zstd + 独立冻结期望集 + 四步校验） |
| （本次） | fix(archive): 接线 `CleanupStaging`（P2）+ 修正契约迁移编号（P4） |

---

## 2. 实现清单（对照 v3 要求）

| v3 要求 | 实现 | 位置 |
|---|---|---|
| §3.1 独立冻结期望行集 | 基准1 全量行数（**不含 body 谓词**）+ 基准2 有正文行 rid 的 C 序 sha256 | `archive_service.go` `freezeExpectations` |
| §3.2 四步校验 | ① `archive.ReadBack` 真读盘重算 ② 全量覆盖 ③ 无陈旧 `_SUCCESS` ④ 结构一致 | `archive_service.go` |
| §3.3 失败语义 | manifest 落库失败 / `markManifestVerified` 失败 **均终止返回 error** | `archive_service.go` |
| §3.4 置空门闸 | 7 项 + 本轮新增第 ⑧ 项 | `archive_inspect.go` `checkNullifyGates` |
| §3.5 巡检消费 sha256 | 轮转巡检回读重算比对；失败 `suspect` 并冻结清理 | `archive_inspect.go` `RunInspectCycle` |
| §3.6 保留期删除前置 | cleaned + 巡检 ok + 新鲜 + 保护窗 + **索引可读性（R5）** | `archive_inspect.go` `retentionGate` |
| §4.1 分块 zstd | 按整行封块、单行超块独占、spool 先落盘再流式压缩、显式 `WithWindowSize` | `internal/archive/partwriter.go` |
| §4.3 排序契约 | 导出/游标/索引二分/摘要一律 `COLLATE "C"` 或 `bytes.Compare` | 多处 |
| §5.1 开关拆分 | `archive.write.enabled` / `archive.read.enabled` 独立；点读器与写侧解耦 | `router.go` |
| §5.2 点读字节预筛 | `bytes.Contains` 定位候选行，仅候选行反序列化 | `readbody.go` |
| §5.3 并发闸落点 | 信号量在 `LocalStore`，解码前 acquire | `local.go` |
| §7.2 四态可区分 | 热路径 / 归档命中 404+`archive_miss` / 点读被禁 **503+`read_disabled`** / 永久丢失 | `usage_handler.go` |
| §11 负向测试 N3–N10 | 21 条，全部做过变异验证 | 见 §4 |

---

## 3. 对抗校验发现并已修复的缺陷（F1–F6）

> 由独立审查智能体给出（每条含 `文件:行号`），4 个修复智能体落实，全部经**变异测试**证明：
> 回退修复 → 对应用例必红 → 还原 → sha256 逐字节一致。

| # | 级别 | 缺陷 | 修法 |
|---|---|---|---|
| **F1** | **严重·结构性·不可逆** | 盖戳谓词（ts 区间 + status）比导出谓词（含 body 非空 + 冻结快照）**宽** → 行可被盖上 `status=1` 却不在归档里，随后被无差别置空，正文永久丢失且归档无副本 | ① 盖戳改为**按本轮导出的 rid 列表**（`WHERE request_id IN (...)`）；② `assertPointerCoverage` 断言覆盖一致；③ 门闸⑧ 二次独立比对 `count(status=1 且有正文) == expected_body_count` |
| **F2** | 高·触发式·不可逆 | 盘上 `index.jsonl` 丢失/损坏 → 点读全 miss 但对用户表现为「数据不存在」；巡检只 `Warn` 后仍判 `ok`；保留期删除再把唯一副本删掉 | ① `LoadIndex` 失败改为 `slog.Error` + 判 `suspect`；② 逐条比对盘上 index 与 PG 的 `Part/FirstRID/LastRID/Rows` 及顺序；③ 新增 R5：删除前校验目录、`_SUCCESS`、`index.jsonl` 可读非空 |
| **F2b** | 高·触发式 | 导出先删 `_SUCCESS`；若该天随后被判 `empty` 则**不恢复标记**，而 `empty` 天不进巡检候选集 → 该天永久不可读 | 把「盘上已有归档产物」检查**提前到删除 `_SUCCESS` 之前**；命中则保留标记、标 `suspect`、报错交人工 |
| **F3** | 中·概率式·不可逆 | 门闸④只抽 20+2 条，未抽中的坏块不拦置空 | 门闸④ 先对**全部块**逐块校验 sha256，再做抽样回读；概率保证升级为逐块确定性 |
| **F4** | 中·功能停摆 | `verified` 天有 leak 行 → 直接 `return error`，使「verified 落库后指针写入失败」的天**永远**无法补写指针 | `verified` + leak → **整日重归档**（正文仍在 PG）；`cleaned` + leak → 报错不重归档 |
| **F5** | 中·潜在 | `nullifyDay` 游标缺 `COLLATE "C"`，而同一语句 `ORDER BY` 用了 C | 游标显式加 `COLLATE "C"` |
| **F6** | 低·安全网误触发 | 门闸④抽样候选无 body 谓词 → 抽到无正文行必然 `NotFound`，整天被跳过 | 抽样候选加 `(request_body IS NOT NULL OR response_body IS NOT NULL)` |

**F1 的端到端证据**（修复后）：

```
链 A（写路径）：exportDay 因 pointer coverage mismatch 拒绝成功 → 本轮 SET body=NULL 次数 = 0
链 B（置空侧）：门闸⑧ 拦下 → SET body=NULL 次数 = 0；审计留痕
              {"dt":"2026-09-19","gate":"⑧","reason":"已盖戳且有正文的行数 4 != expected_body_count 3..."}
反事实：摘掉任一层防线，同一场景立刻复现漏洞（用例转红）
```

---

## 4. 验证证据

### 4.1 构建与测试（我本人独立复核）

```
go build ./internal/... ./cmd/...            exit=0
go vet ./internal/archive/... ./internal/observability/... ./internal/controlplane/...  干净
go test -count=1 ./internal/archive/... ./internal/observability/... ./tests/archive/...  全 ok
```

### 4.2 真实 PostgreSQL 验证（本机 PG 18.3 临时集群）

把实现用到的 **9 条 PG 专有 SQL** 全部在真实 PG 上执行通过：基准1/基准2 摘要、`COLLATE "C"` 导出与 keyset 游标、按 rid 列表盖戳、覆盖断言、`(ts AT TIME ZONE ?)::date` 日期谓词、抽样候选、幂等置空。第 ⑨ 步确认：**只有真正归档的 3 行被置空，2 行无正文行保持 `status=0`**（F1 修复的可观测证据）。

**关键风险解除**：`sha256()` 是 **PG 11+ 内置**（实测 `sha256('abc')` = `ba7816bf...`，无需 pgcrypto）。原先 WS-B 标记的「可能依赖 pgcrypto」不成立。

**F5 诚实标注**：本机库默认 collation 为 `zh_CN.UTF-8`，实测 `'a-b' > 'ab'`、`'req-a' > 'req-A'`、UUID 场景下默认 collation 与字节序**结果一致**，**F5 无法在本机 locale 复现**。修复是必要的防御性一致化（与写路径写法统一），但**不应宣称它是已证实的生产 bug**。

### 4.3 变异测试

- 负向测试 N3–N10：11 处变异 **11/11 全部被捕获**
- 修复回归 F1–F6：11 处变异 **11/11 全部变红**，变异前后文件 sha256 逐字节一致

---

### 4.4 真实 PG + 真实 Redis 端到端（补齐此前的最大验证缺口）

**背景**：在本节之前，本特性的全部测试都用脚本化假 DB 驱动——「Go 代码 ↔ 真实 PG」这一层
（GORM 参数绑定、`AT TIME ZONE ?` 传参、`COLLATE "C"` 游标、keyset 分页循环、事务与 jsonb 往返）
一次都没真跑过，而历史缺陷恰恰都藏在这条接缝上。

新增 `tests/archive/pg_e2e_integration_test.go`（`-tags=integration`，缺环境变量则跳过），
核心断言是【唯一安全不变量】的端到端编码：

> **凡 `archive_status=2`（正文已从 PG 离场）的行，其正文必须能从归档回读出来。**

任何「置空了但读不回来」的组合都会在此失败，因此它同时是 F1（盖戳集合 ⊋ 归档集合）
与 F2（索引不可读却被判 ok）的探测器。

**执行位置与结果**

| 环境 | PG | collation | 结果 |
|---|---|---|---|
| 本机 | 18.3 | `zh_CN.UTF-8` | 3/3 PASS |
| **测试环境 191.121** | **18.4（与生产同版）** | `en_US.utf8` | **3/3 PASS** |

测试环境上验证的实际内容（真实 PG + 真实 Redis + 真实文件系统）：

- 迁移在真实 PG 18.4 上从「迁移前 schema」一路应用成功：5 个归档列、`trace_archive_manifests`、部分索引
- 独立冻结期望集落库正确：`expected_total=300`、`expected_body_count=240`、rid 摘要非空
- 多块 zstd 归档：`chunk_mb=1` 下切出 **4 个块**，`.spool` 无残留
- 索引 C 序且块间不重叠：`first_rid > last_rid` 与块间重叠均被断言拦下
- janitor 全周期：巡检 `verify_state=ok` → 置空 → `state=cleaned`
- **241 个置空行全部从归档回读成功**（含跨天补归档的行）
- 天边界正确：相邻天的行各自落到自己的 `dt`，本天归档指针下无更早时间的数据
- 无正文的 60 行**始终未被盖戳**（F1 的直接体现）
- 点读 fail-closed：索引缺失、块文件缺失、`_SUCCESS` 缺失三种情况下均**明确报错**，
  不会把「归档不可读」误呈现为「数据不存在」

**测试本身有牙齿（变异测试证明）**

把 `readbody.go` 快路径的 `return obj, nil` 改成 `return &ArchiveObject{}, nil`
（模拟"归档静默不可读"）：

```
变异后: --- FAIL: TestArchiveEndToEndRealPostgres
        --- FAIL: TestArchiveLateArrivalIsNotLost
        【不可逆丢失】late-arrival 回读到的正文为空
还原后: 与变异前逐字节一致，3/3 全绿
```

**过程中修正的 3 处测试假设（都是测试错，不是代码错）**

1. 归档落在 `{root}/dt=<date>/`，**无** `call_traces/` 前缀——我最初路径写错
2. 相邻天的行会被 janitor 的**补归档**阶段各自归档（设计行为），
   因此不能断言它们保持 `archive_status=0`；正确断言是"落到各自的 dt"
3. 归档后落入的 `late-arrival` 被置空是**正确**的——F4 修复触发了整日重归档把它收进了归档，
   且回读成功。因此该用例的正确断言是"离场即可回读"，而不是"必须保持热存"

**测试环境安全性**（全程未触碰共享资源，已复核）

| 资源 | 测试前 | 测试后 |
|---|---|---|
| 共享库 `llm_gateway_test.call_traces` | 1087 行 / 925 有正文 / 0 归档列 | **完全一致** |
| Redis DB 0（169 万键） | 1690156 | **1690156** |
| 独立 scratch 库 / 归档目录 | — | **已删除** |
| Redis DB 15（测试专用） | 0 | 0（leader 锁自行释放） |

> 说明：测试环境 `/` 分区已 100% 占满（0 字节可用），因此二进制与归档数据全部走 `/data`（92G 空闲）。

---

## 5. 残留风险与未决项（诚实清单）

| # | 项 | 性质 | 建议 |
|---|---|---|---|
| **P1** | **正文回填通道未实现**。契约声明了 `archive.backfill.enabled` / `max_days`，但**无任何工作流拥有回填实现文件**（契约自身的所有权缺口），两个配置项当前是死配置 | v3 §7.3 要求的子系统缺位 | 需你决策：是否本期实现，或明确记为下期 |
| ~~P3~~ | ~~归档链路的 Go↔PG 集成仅由假 DB 驱动覆盖~~ | **已关闭** | 见 §4.4：真实 PG 18.4 + 真实 Redis 端到端 3/3 通过，并附变异测试证明 |
| R1 | F6 的 fail-closed 会造成**活性风险**：若归档对象里存在 SQL 非 NULL 但 JSON 字面量 `null` 的正文，门闸④ 永不通过 → 该天永久无法置空（不丢数据，但链路停摆且静默） | 设计取舍 | 建议加可观测计数/告警 |
| R2 | F1 的两道防线都是**计数相等**而非集合逐行相等。要绕过需 `archive_status=2` 且正文非空，而唯一的 `=2` 写入点与正文置空是同一条语句 → 当前**不可达**，但无 DB 约束兜底 | 理论 | 可考虑加 `CHECK (archive_status <> 2 OR (request_body IS NULL AND response_body IS NULL))` |
| R3 | F2b 守门只判 `index.jsonl` 是否存在（part 文件仍在而 index 被人工删除时不触发） | 低概率 | 可扩展为「index 或任一 part 存在」 |
| P6 | `ArchiveCron` 的「按天显式编排」分支是死代码（`ArchiveService` 未实现 `PendingDays`），生产恒走 `RunCycle` | 冗余 | 清理或补实现 |
| P7 | `TraceArchiveInspector` 无生产调用方（巡检实际由 janitor 内部 `RunInspectCycle` 完成，功能生效） | 冗余 | 清理 |

---

## 6. 顺带发现的**仓库既有问题**（非本特性引入，建议单独处理）

| # | 问题 | 证据 |
|---|---|---|
| **D1** | **main 的迁移目录与 GORM model 漂移**：`channel_monitors`（5 张表）、`quota_rpm` 等模型无迁移文件。根因见 §6.1——这是**有意删除已应用迁移**的后果，不是疏忽 | 我在纯净 main worktree 复现 18 KB 伪 DDL；正因如此本特性的迁移改用 `atlas schema diff`（main model schema → 本分支 model schema）语义求差，**只含本特性增量** |
| **D2** | **`tools/atlas-loader` 输出非确定性**：同一 worktree 连续两次 `go run main.go` 差 12 行（GORM 约束顺序随 map 迭代变化），会让 `atlas migrate diff` 产生伪 DDL | 实测两次输出 `diff` |
| **D3** | `test/bedrock_model_id_check.go:98` 编译失败（`bedrock.NewListFoundationModelsPaginator` 未定义） | `go build ./...` |
| **D4** | `internal/controlplane/repository` 的 `TestListHistoryPaged_respectsPageSize` 失败（GORM sqlite dialect 无法生成 `gen_random_uuid()`） | `go test`，在 HEAD 复现一致 |
| **D5** | `tests/` 下 4 个包 `go vet` 失败（`fakeScheduler` 缺 `UpdateSticky` 等接口漂移） | 在 HEAD 临时 worktree 复跑，输出逐字相同 |
| ~~D6~~ | ~~生产库 `atlas_schema_revisions` 不存在~~ → **判断有误，已推翻** | **无阻断**：见下方修正，生产 revisions 正常（55 条、0 错误） |
| ~~D7~~ | ~~`migrate-prod` 缺 `--exec-order non-linear`~~ → **已纠正** | prod 严格线性有序属**有意设计**，**不应改 CI**，见下方修正 |

### 6.1 生产迁移状态核查（D6/D7/D1 的修正，仅有只读查询，未写生产）

**D6 我原来的判断是错的，必须更正。** 我此前只查了 `public` 里有没有名为
`atlas_schema_revisions` 的**表**，而 CI 用的参数是 `--revisions-schema atlas_schema_revisions`
——它是一个**同名 schema**。生产实际状态：

```
schema:  atlas_schema_revisions   ← 存在
table:   atlas_schema_revisions.atlas_schema_revisions
已应用:  55 条，error 全为空，无失败记录
```

**结论：生产 Atlas 迁移机制完全正常工作，不存在部署阻断。** 原判断中"迁移从未通过 Atlas 应用"
与"直接跑 migrate-prod 会从 baseline 重放"两句均不成立。

**逐版本比对（生产 55 条 vs 仓库 54 个文件）**

| 类别 | 数量 | 明细 |
|---|---|---|
| main 的迁移 | 53 | **全部已应用** —— 无一条待应用 |
| 本次新增 | 1 | `20260921064216_add_trace_archive.sql` —— **唯一待应用** |
| 生产独有的孤儿版本 | 2 | `20260630120000`、`20260703110000`（仓库已无文件） |

```
生产 max = 20260916095738
本分支 max = 20260921064216   →  严格大于，线性有序
atlas migrate validate --dir file://atlas-migrations  →  exit=0
```

**D7 纠正**：`.gitlab-ci.yml` 的 `migrate-prod` 缺 `--exec-order non-linear`（`migrate-test` 有）
**不是疏漏，而是有意设计**。仓库历史给出了原因——提交 `43395327`（2026-07-06）：

```
fix(migrations): 删除乱序的 atlas 迁移文件避免生产部署失败
删除: 20260630100000_ehr_sync_tables.sql
      20260630120000_add_call_logs_key_type.sql
      20260703110000_create_channel_monitor_tables.sql
```

团队对"乱序迁移"的处置方式是**删掉文件**，而不是放宽 prod 的执行顺序。因此：

- prod 保持严格线性有序是刻意选择，**不应修改 CI**
- 本分支迁移的时间戳已刻意保证 > 生产 max，符合该约定

**孤儿版本被容忍的实证**：删除发生在 2026-07-06，而生产在那之后**成功应用了 5 次迁移**
（2026-08-21、08-31、09-07、09-11、09-17）。这说明 **Atlas 容忍「已应用但文件已不存在」的版本**，
本次应用不会因这 2 个孤儿而失败。

**D1 根因也随之解释**：`channel_monitors`（5 张表）、`quota_rpm` 之所以"有 model 无迁移"，
正是上述删除动作的直接后果——那条迁移当初已应用到生产，随后文件被删。
所以「迁移目录与 model 漂移」不是疏漏，而是这个仓库迁移策略的固有代价；
它也再次印证：本特性用 `atlas schema diff` 而非 `atlas migrate diff` 生成迁移是必要的。


---

### 6.2 U3 结论：TOAST 空间可被 VACUUM 回收（**是**），但默认 autovacuum 节奏跟不上

**U3 是**。在测试环境 **PG 18.4（与生产同版）** 上用同形替身表做了 A/B 对照实验
（唯一变量 = 是否 VACUUM；`STORAGE EXTERNAL` 关闭压缩以确保正文真的进 TOAST，
并关闭 autovacuum 以免干扰）：

| 表 | 置空后 TOAST | VACUUM 后 | 插入第二批 100 MB 后 |
|---|---|---|---|
| `u3_novac`（不 VACUUM） | 104 MB | — | **208 MB（翻倍，零复用）** |
| `u3_vac`（VACUUM） | 104 MB | **0 KB（文件被截断）** | **104 MB（零增长）** |

部分置空模型（4000 行中仅 1000 行有正文，贴近生产 24% 的形态）结果一致：
不 VACUUM → 208 MB；VACUUM 后 → 0 KB，再插入恰好回到一批的量。

三条被实测证实的机制（此前都是假设）：

1. `UPDATE SET body=NULL` **不会**让 TOAST 变小（104 MB → 104 MB，纹丝不动）
2. `VACUUM <表>` **会递归处理 TOAST 子表**——VACUUM VERBOSE 直接打印
   `INFO: vacuuming "pg_toast.pg_toast_307884"`，且报告 `142 currently deleted, 142 reusable`
3. `vacuum_truncate=on` 时，全死的 TOAST 文件**会被截断并把空间还给操作系统**

**生产侧的回收可行性（只读核查，未执行 VACUUM）**

| 检查项 | 结果 | 含义 |
|---|---|---|
| 复制槽 | 无 | 不拖住 xmin |
| 从库 | 无 | 不拖住 xmin |
| 两阶段事务 | 0 | 不拖住 xmin |
| 最长运行事务 | 0 秒 | 不拖住 xmin |
| `vacuum_truncate` | on | 尾部空页可还给 OS |
| `call_traces` 预留 | total 286 GB（heap 2.1 GB + **TOAST 280 GB**） | 可回收量级 ≈ 280 GB |
| `/data` | 344G，已用 299G，**仅剩 43G（88%）** | 约 4 天见底 |

→ **没有任何阻断因素，VACUUM 在生产上能正常回收。**

**⚠️ 但这里有一个会让整个方案失效的运维陷阱**

`autovacuum_vacuum_scale_factor = 0.2`（全局默认，`call_traces` 无表级覆盖），
表内 533 万行 → 自动清理触发阈值 **≈ 50 + 0.2 × 5,333,741 ≈ 107 万死元组**。

而归档是**按天**置空，每天产生约 **10 万**死元组（生产实测近期行几乎行行有正文）。
两者一比：

```
每天产生死元组      ≈ 10.1 万
autovacuum 触发阈值 ≈ 107 万
→ 攒够一次要        ≈ 10.6 天
```

**即：默认配置下，每约 10 天才自动回收一次，而 /data 每天涨 11 GB —— 这 10 天里会涨掉约 110 GB，
远超当前仅剩的 43 GB。方案会在回收生效前就把盘写满。**

**必须做的补救（二选一，建议都做）**

1. **表级 autovacuum 调参**（推荐，一次投入长期生效）——让回收节奏跟上置空节奏：
   ```sql
   ALTER TABLE call_traces SET (
     autovacuum_vacuum_scale_factor = 0.0,
     autovacuum_vacuum_threshold    = 20000
   );
   ```
   调后约每 2 万死元组触发一次（≈ 数小时一次），与每日置空节奏匹配。

2. **归档周期内显式执行 VACUUM**——在 janitor 完成当日置空后执行
   `VACUUM (ANALYZE) call_traces`。手工 VACUUM 不受 autovacuum 的 cost 限流，
   速度更快；但要注意它与 autovacuum 的并发争抢。

   > 注：`maintenance_work_mem` 当前为默认 **64 MB**，对 280 GB 级 TOAST 偏小。
   > 首次大规模回收建议在会话内临时提高（如 `SET maintenance_work_mem='1GB'`）以缩短单次耗时。

**首次回收的量级预期**：若把 126 万个有正文行的正文全部置空后 VACUUM，
TOAST 中已无活引用，`vacuum_truncate=on` 会把大部分文件截断还给 OS ——
`/data` 可用空间有望从 43 GB 回升到 **300 GB 量级**，而非仅仅"止住增长"。
（实际值取决于置空完成度与截断能覆盖的尾部空页范围。）

---

## 7. 上线前置清单

1. **迁移**：**无前置阻断**（D6 已推翻，见 §6.1）——生产 revisions 正常，唯一待应用的就是
   `20260921064216_add_trace_archive.sql`，且时间戳严格大于生产 max。直接走 CI 的 `migrate-prod` 即可。
   该迁移在 5,315,983 行的 `call_traces` 上使用 PG 11+ **快速默认值**，不触发表重写。
2. **开关**：`archive.write.enabled=false`、`archive.read.enabled=true` 上线，验证 writer no-op 后再开写。
3. **磁盘**：`/datalog` 需合账（归档 + leakscan 189 GB + 保留期），`archive.retention_days` 建议 180 而非 365（v3 §7.2）。
4. **`/data` 止血（U3 已验，见 §6.2）**：`VACUUM call_traces` 能真正回收 TOAST 空间，
   优先于 `pg_repack`（后者需约 532 GiB，当前不可行 —— v3 §6.3）。**但必须同时调 autovacuum**：
   默认阈值约 107 万死元组、而每天只产生约 10 万 → 攒够一次要 10 天，
   这 10 天里 `/data` 会涨掉约 110 GB，**超过当前仅剩的 43 GB**。
   上线前必须执行 §6.2 的表级 autovacuum 调参，否则方案会在回收生效前把盘写满。
5. **决策**：D-1/D-2/D-3 三项（见 v3 §2）与 P1 回填是否纳入本期。

---

## 8. 部署就绪度核查（2026-09-21，结论：**当前不能直接部署**）

### 8.1 阻断项

**① 没有任何可部署的产物包含归档接线**

`deploy/blue-green/docker-compose.{blue,green}.yml` 里的 `/datalog` 挂载与 `ARCHIVE_ENABLED`
**在 main 上不存在**，只是主工作区的**未提交改动**；`config/prod.yaml` 的 `archive` 段同样未提交。
旁证：当前生产在跑的 `llm-{blue,green}-control-plane` 容器**没有任何 `ARCHIVE_*` 环境变量**。

更危险的是主工作区的状态：所在分支是 **`fix/budget-next-period-409`**（与 main 无关），
**219 个未提交改动文件** + 3 个 stash。从这个状态发布是不可接受的。

**② `ARCHIVE_ENABLED:-true` 是一个真实陷阱**

那份未提交的 compose 写着 `ARCHIVE_ENABLED: ${ARCHIVE_ENABLED:-true}`，而 v3 保留了
`archive.enabled` 作兼容读取：

```go
// internal/controlplane/config/config.go:222-225
archiveWriteEnabled := v.GetBool("archive.write.enabled")   // 默认 false
if v.IsSet("archive.enabled") && v.GetBool("archive.enabled") {
    archiveWriteEnabled = true     // 兼容映射 → 写路径开启
}
```

环境变量优先级高于 `config/prod.yaml`，所以 `ARCHIVE_ENABLED=true` 会**覆盖** yaml 里的
`archive.enabled: false` → **部署即开启写路径**。

所幸门闸⑥ 会硬挡置空（`archive_inspect.go:736`）：

```go
if strings.TrimSpace(j.cfg.offhostMarker) == "" {
    return false, "offhost_copy_marker 未配置"
}
```

而 prod 配置是 `require_offhost_copy: true` + `offhost_copy_marker: ""`。
**净结果：开始往 /datalog 写归档，但永远不会置空 —— `/data` 照涨，`/datalog` 白占空间。**
不会丢数据，但**完全解决不了问题，还消耗窗口**。

**③ `config/prod.yaml` 是 v2 schema，与 v3 代码不匹配（静默失效）**

| prod.yaml 的 key | 在 v3 里的状态 |
|---|---|
| `gzip_level: 6` | **已不存在**（v3 用 zstd）→ 静默忽略 |
| `disk_watermark_mb: 80` | **已改名为** `disk_watermark_percent`（默认 80）→ 静默忽略旧名 |
| `backfill_once: false` | v3 已废弃（B14 修复改为独立回填通道）→ 静默忽略 |
| `enabled: false` | 兼容映射到 `WriteEnabled`，但被 ② 的 env 覆盖 |
| 缺失 v3 所需的 | `write.enabled` / `read.enabled` / `chunk_mb` / `zstd_level` / `zstd_window_mb` / `days_per_night` / `encoder_concurrency` / `max_concurrent_reads` / `verify_sample_rows` / `inspect_freshness_hours` / `inspect_per_cycle` / `retention_days` / `disk_watermark_percent` / `offhost_marker_freshness_hours` |

viper 对未知 key 不报错 → **全部走代码默认值**。`archive.read.enabled` 默认 false，
意味着置空后点读会返回 503（当前因门闸⑥ 挡住置空而未触发）。

**④ 迁移未应用**：生产 `call_traces` 无归档列、无 `trace_archive_manifests` → 必须迁移先行。

**⑤ autovacuum 未调参** → 即使置空跑通，回收节奏也跟不上（§6.2）。

**⑥ 容器化部署从未验证过**：CI、镜像构建、env 注入、`/datalog` 卷权限、cron 真实触发，全都未验。

**⑦ P1 回填缺位；D-1/D-2/D-3 未确认。**

**已评估并排除的候选阻断项 —— §10（导出临时文件溢出）**

§10 曾被判定为归档的致命隐患（「整天批处理会反复产生 GB 级临时文件」）。
**该判断已于 2026-09-22 用对照实验推翻**：归档的 `fetchBatch` 只取原始列，在同等参数下
实测溢出仅 **2.8 MB**；GB 级溢出只发生在**对正文做投影**的查询上（详见 §10.2）。
因此 **§10 不构成归档的上线阻断**。但**建议**落实 `temp_file_limit` 安全网——
它防的是运维/排查时写的即席查询，那类查询在生产上确已写满过一次 `/data`（§10.1）。

### 8.2 时间窗口：`/data` 已无腾挪余地

```
/data      344G  已用 299G  可用 43G (88%)   ← 约 4 天
  └ /data/1panel  299G
      └ PG 数据目录 298.4G（pg_wal 仅 1.0G）
  非 PG 内容：仅 /data/web 108M + /data/litellm 47M
/datalog   590G  已用 193G  可用 368G (35%)
/          57G   已用 23G   可用 32G (42%)  ← 镜像拉取无虞
```

**`/data` 上 299G 里 299G 都是 PG，没有任何可搬走的东西。**
唯一出路就是在 PG 内部回收 —— 即 归档 → 置空 → VACUUM，这需要功能完整上线。

### 8.3 最短安全路径

1. 迁移先行（加列/建表/建部分索引；该索引初建时 `archive_status=1` 行数为 0，代价极小）
2. 归档接线**先提交**（compose 的 `/datalog` 挂载 + `ARCHIVE_ENABLED` 显式设为 `false`、
   `config/prod.yaml` 的 archive 段按 **v3 schema** 重写）
3. 以 `write=false` 部署，验证容器可写 `/datalog`、cron 触发、无 SQL 报错
4. 开启 write → 归档历史天到 `/datalog`
5. **建立真实的离机副本**并配置 `offhost_copy_marker`（门闸⑥ 的硬性前置）
6. 调 autovacuum（§6.2）→ 置空 → VACUUM
7. 首次大回收建议会话内 `SET maintenance_work_mem='1GB'` 以提高单次效率

> **关键提醒**：第 5 步需要一个真实的离机备份通道。在它建立之前，门闸⑥ 会一直挡住置空，
> `/data` 不会有任何缓解。**这个通道本身可能就是最长的一条关键路径，需要尽早决策。**

---

## 9. `/data` 腾挪可行性（2026-09-21，用户提议「把 /data 一部分迁到 /datalog」）

### 9.1 物理事实

**没有闲置盘**：`sda` 60G（`/`、`/boot`）、`sdb` 350G（`/data`）、`sdc` 600G（`/datalog`），全部已挂载。
`/data` 上非 PG 内容只有 155M（web 108M + litellm 47M）——**没有可搬走的他物**。

```
两盘合计容量  991G
已用          515G（/ 23G + /data 299G + /datalog 193G）
空闲          476G  → 按 11G/天 计，合并看约 43 天
```

**空间总量是够的。问题是分布：增长最快的 PG（11G/天）落在最小的盘（350G）上，而 600G 盘上放着 189G 静态数据。**
所以在两盘之间搬数据本身不产生空间，但**放对位置能把 4 天变成约 27 天**。

### 9.2 `/datalog/archive` 的 189G 不是垃圾，是唯一副本

```
/datalog/archive/call_traces/  9 个 gzip jsonl，共 189G，mtime 2026-09-08 ~ 09-09
  body-0/1/2.jsonl.gz、body-recent-2/3.jsonl.gz、body-r3-new.jsonl.gz …
```

这是 **09-08/09 导出的 call_traces 正文**。而 PG 里**只有 09-10 之后的正文**（更早的天正文数为 0）
—— 即这 189G 极可能是那些历史天的**唯一副本，不可删除**。但它可以搬家（静态只读数据）。

> **兼容性缺口（需单独处理）**：v3 代码读不了这个旧格式——目录是 `body-N.jsonl.gz`，
> 没有 `index.jsonl` / `manifest.json` / `_SUCCESS`，且 root 是 `/datalog/archive`
> 而非默认的 `/datalog/llm-gateway-archive`。**历史正文还在，但新代码读不出来。**

### 9.3 已验证的技术前提：`SET TABLESPACE` 会连 TOAST 一起搬

在测试环境 PG 18.4 上实测（1 GB 规模）：

```
搬迁前 | probe            tablespace=pg_default  size=592 kB
搬迁前 | pg_toast_326439  tablespace=pg_default  size=1016 MB
ALTER TABLE probe SET TABLESPACE ts_probe;   → Time: 4201 ms   (≈245 MB/s)
搬迁后 | probe            tablespace=ts_probe   size=592 kB
搬迁后 | pg_toast_326439  tablespace=ts_probe   size=1016 MB
物理路径 | probe -> pg_tblspc/326437/PG_18_.../336448
物理路径 | pg_toast_326439 -> pg_tblspc/326437/PG_18_.../336449    数据完好 rows=10000
```

**TOAST 确实跟着走**（这是方案可行的前提——TOAST 占 `call_traces` 286G 里的 280G）。
按 245 MB/s 外推，286 GB ≈ **20 分钟**。

### 9.4 搬迁方案与账（不删任何数据）

| 步骤 | /data 可用 | /datalog 可用 |
|---|---|---|
| 现在 | 43G | 368G |
| ① `call_traces`（286G）`SET TABLESPACE` 到 /datalog | **331G** | 111G |
| ② 189G 旧归档 rsync 到 /data | **142G**（放新归档） | **300G**（PG 增长） |

**最终：PG 增长落在 /datalog 的 300G 上 → 约 27 天**（只做 ① 则 111G → 10 天）。

### 9.5 代价与风险

1. **20 分钟 `call_traces` 的 ACCESS EXCLUSIVE 锁**。影响面已查证——trace 写入是
   **非阻塞 + 过载丢弃**设计（`trace_event.go:109-112`：「优先非阻塞快速路径…trace 数据非关键路径，
   不无限阻塞请求」）→ **不会阻塞用户请求**，代价是丢失约 20 分钟的 trace 数据。可接受。
2. **⚠️ 最大障碍：生产 PG 容器没有挂 `/datalog`。**
   ```
   bind /opt/1panel/apps/postgresql/postgresql/data -> /var/lib/postgresql
   ```
   只挂了数据目录。把 tablespace 放到 `/var/lib/postgresql/ts_x` 实际落在 `/opt/1panel/...`（在 `/` 上，仅 32G 空闲），
   **不是 /datalog**。要落到 /datalog 必须**给该容器加 bind mount 并重启 PG 容器**。
   而该容器由 **1Panel 管理**（`createdBy=Apps`，compose project `postgresql`），
   **在 1Panel 之外改挂载有被覆盖或管理冲突的风险。**
3. 索引不跟着搬：`call_traces_pkey` 456M + `idx_call_traces_ts` 114M 留在 /data（量级可忽略）。
4. 搬 189G 需 rsync + 校验后再删源（无停机）。

### 9.6 诚实结论

**这是买时间，不是解决问题**——11G/天照旧，27 天后仍会写满。
且它并不比「直接把归档功能跑起来」更接近目标：**归档 + 置空 + VACUUM 会真正释放约 280 GB。**

两条路线对比：

| | 搬迁腾挪 | 手工/正式跑归档 |
|---|---|---|
| 收益 | 4 天 → 27 天 | 释放约 280 GB，可重复 |
| 主要风险 | 改 1Panel 管理的容器挂载；20 分钟锁 | 需接受 `require_offhost_copy=false`（唯一副本在 /datalog） |
| 依赖 | 1Panel 容器配置可改 | 迁移 + 可运行的归档进程 |
| 是否解决根因 | 否 | 是 |



---

## 10. ⚠️ 生产测量事故与「排序 + 正文投影」隐患（2026-09-21；结论于 09-22 用对照实验修正）

### 10.1 事故经过（我方造成，已自动恢复）

为测量真实压缩比，在**生产**上执行了一条**只读**查询：

```sql
COPY (SELECT json_build_object(…)::text FROM call_traces
       WHERE ts >= '2026-09-10' AND ts < '2026-09-11' AND (request_body IS NOT NULL OR response_body IS NOT NULL)
       ORDER BY request_id COLLATE "C" LIMIT 25000) TO STDOUT
```

结果：

```
ERROR: could not write to file "base/pgsql_tmp/pgsql_tmp979357.14": No space left on device
```

**ORDER BY 触发排序，PG 把临时文件溢出到 `/data` 上的 `base/pgsql_tmp/`，撞了 ENOSPC。**
查询中止后 PG 自动删除临时文件，空间已回收。

恢复后的实测状态：`/data` 可用 **42G**（进入前 43G）、PG 容器 **Up (healthy)**、
`SELECT`/写操作正常、负载 5.07（8 核）。**无数据损坏，服务未中断。**

> 教训：在这张表上「只读」不等于「无害」。任何 `ORDER BY` / 聚合都可能产生 GB 级磁盘溢出。

### 10.2 ⚠️ 重大修正：归档自身**不在**该隐患的主路径上（2026-09-22 对照实验推翻原判断）

本节旧版曾断言「归档自己的查询带着同一个隐患，整天批处理会反复产生 GB 级临时文件」。
**这个结论是错的。** 下面是对照实验，它同时给出了正确结论。

实验设计（测试环境 PG 18.4；30000 行 × 226KB 正文；`SET STORAGE EXTERNAL` 强制正文走行外
TOAST，实测 TOAST 7.0 GB / heap 3.4 MB；两个查询都带 `ORDER BY request_id COLLATE "C"`
与相同 `LIMIT`；会话内 `SET temp_file_limit='3GB'` 作保险丝，超限即报错，既安全又能证明
「它到底想要多少」）：

| 查询形状 | LIMIT | 临时文件 | 说明 |
|---|---|---|---|
| `json_build_object(…)::text`，**对正文做投影** | 25000 | **> 3 GB**（撞保险丝：`temporary file size exceeds "temp_file_limit"`） | 复现了 §10.1 事故的形状 |
| 归档 `fetchBatch` 的**原始列**（无任何投影） | 25000 | **2.8 MB** | 归档的真实形状 |

**结论（基于证据，不是推理）：**

1. GB 级溢出**是真实的**，但触发条件是「**排序的输入里已经含有展开后的正文**」，
   即查询在排序路径上对正文做了**投影或计算**（如 `json_build_object(...)::text`）。
   §10.1 的事故正是这种形状——**溢出是我那条测量查询自己造成的**。
2. **归档的 `fetchBatch` 只取原始列**（`request_id, ts, request_headers, request_body,
   response_body, error_code, error_message`），排序中流动的是含 TOAST 指针的小元组
   （EXPLAIN 实测 `width=103`；正文只在行被投影出去时逐行 detoast）。生产 `archive.batch_rows`
   默认 **1000**（实测 `config/prod.yaml` 未覆盖该键），实测量级是 **MB 而非 GB**。
3. 因此**归档没有这个致命隐患**。真正的隐患属于**运维/排查场景**：
   任何人在这张表上写「`ORDER BY` + 对正文投影」的即席查询（这正是排查问题时最自然的写法），
   都可能写满 `/data`。**§10.1 的事故应当被记作「运维查询隐患」，而不是「归档缺陷」。**
4. 并行计划本身不足以造成溢出：归档的导出查询在 20 万行量级**确实会走**
   `Gather Merge → Sort → Parallel Seq Scan`（已实测），但只要排序的输入是原始列，
   溢出就只有 MB 级。

### 10.3 应对（按性价比排序）

| 措施 | 权限 | 定位与说明 |
|---|---|---|
| 全局 `temp_file_limit`（如 5–10 GB） | **需超级用户**（app 角色实测 `permission denied to set parameter`） | **这才是真正的安全网。** 它把「写满 `/data` 可能导致 PG 崩溃」降级为「查询报错」，且防的是**任何**查询——包括人写的即席查询——而不只是归档。**建议优先落实。**（测试环境已验证 `ALTER SYSTEM SET … ; SELECT pg_reload_conf();` 可即时生效、无需重启，`RESET` 可干净回退。） |
| 归档导出会话 `SET LOCAL max_parallel_workers_per_gather = 0` | 普通角色可改（`context = user`） | **纵深防御，不是修复。** 归档本来只溢出 MB 级；此举把那点开销也去掉，并在将来有人往 `SELECT` 列表里加计算列时依然成立。已实现于 `exportReadSession`（`internal/observability/service/archive_service.go`），并有两条例行回归测试守护（经变异验证）。实现注释已按本节证据改写。 |
| 建 `(request_id COLLATE "C")` 索引 | schema 变更 | 从根上消除 Sort。但 `ts` 范围过滤与 `request_id` 排序无法共用同一棵 btree，需权衡收益。 |

**归档不再因为本节的隐患而阻断上线**（此前 §8 把 §10 列为阻断项 ⑤，该判断已撤销）。
但 `temp_file_limit` 安全网**强烈建议**在任何人在生产上做正文排查之前落实。

### 10.4 伴随发现的残留项（未验证，待确认）

导出的每一个批次，GORM 的 `Scan(&rows)` 会把整批行连同正文一次性收进 Go 内存：
生产 `batch_rows = 1000`，正文约 226 KB/行 → **单批约 226 MB 的 Go 堆**。
`streamExport` 是逐批处理（每批写完即释放），因此这是**有界的**，但 226 MB 的瞬时占用
对控制面进程仍值得确认。**上生产前建议实测一次控制面的 RSS 峰值**；
若偏高，调小 `archive.batch_rows` 即可线性降低（代价是批次数变多）。

### 10.5 压缩测量结果（**保真度有保留**）

带熔断的安全测量（去掉 ORDER BY，10000 行）：

```
原始 jsonl   = 5,498,054,293 字节 (5.12 GiB)
zstd 压缩后  =    88,543,554 字节 (0.08 GiB)
压缩比       = 62.09x
耗时         = 1m22.972s    →  吞吐 66.3 MB/s
前后 /data 可用：43G → 42G（无净消耗）
```

**⚠️ 62.09x 不可直接采用，原因**：测量用的 `json_build_object(…)::text` 会把
`request_headers` 等文本字段再转义一层，输入被撑大约 2.4 倍（549,805 B/行 vs 早前测得的 ~205,879 B/行），
而转义产生的反斜杠冗余会**虚高压缩比**。该口径与归档实际写出的 jsonl 不等价。

**规划应采用设计自身已验证的口径**：`partwriter_test.go:451` 断言真实 jsonl 经生产 codec 压缩比 **≥ 9x**，
早前实测 **9.57x** → `241.4 GiB` jsonl ≈ **25 GiB**。

**综合结论（这才是决策需要的）**：

```
投入 /datalog   ≈ 25 GiB（归档全部正文）
释放 /data      ≈ 280 GB（TOAST，置空 + VACUUM 后）
```

净收益约 **10 倍**，方向完全正确——但**必须先完成置空与 VACUUM，压缩本身不释放任何 /data 空间**。
