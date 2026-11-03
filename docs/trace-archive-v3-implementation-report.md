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

## 5. 残留风险与未决项（诚实清单）

| # | 项 | 性质 | 建议 |
|---|---|---|---|
| **P1** | **正文回填通道未实现**。契约声明了 `archive.backfill.enabled` / `max_days`，但**无任何工作流拥有回填实现文件**（契约自身的所有权缺口），两个配置项当前是死配置 | v3 §7.3 要求的子系统缺位 | 需你决策：是否本期实现，或明确记为下期 |
| P3 | 归档链路的 Go↔PG 集成仅由**假 DB 驱动**测试覆盖（SQL 语句形状已在真实 PG 手工验证，但无 CI 守门） | 测试强度 | 建议补一条 `-tags=integration` 的真实 PG 用例 |
| R1 | F6 的 fail-closed 会造成**活性风险**：若归档对象里存在 SQL 非 NULL 但 JSON 字面量 `null` 的正文，门闸④ 永不通过 → 该天永久无法置空（不丢数据，但链路停摆且静默） | 设计取舍 | 建议加可观测计数/告警 |
| R2 | F1 的两道防线都是**计数相等**而非集合逐行相等。要绕过需 `archive_status=2` 且正文非空，而唯一的 `=2` 写入点与正文置空是同一条语句 → 当前**不可达**，但无 DB 约束兜底 | 理论 | 可考虑加 `CHECK (archive_status <> 2 OR (request_body IS NULL AND response_body IS NULL))` |
| R3 | F2b 守门只判 `index.jsonl` 是否存在（part 文件仍在而 index 被人工删除时不触发） | 低概率 | 可扩展为「index 或任一 part 存在」 |
| P6 | `ArchiveCron` 的「按天显式编排」分支是死代码（`ArchiveService` 未实现 `PendingDays`），生产恒走 `RunCycle` | 冗余 | 清理或补实现 |
| P7 | `TraceArchiveInspector` 无生产调用方（巡检实际由 janitor 内部 `RunInspectCycle` 完成，功能生效） | 冗余 | 清理 |

---

## 6. 顺带发现的**仓库既有问题**（非本特性引入，建议单独处理）

| # | 问题 | 证据 |
|---|---|---|
| **D1** | **main 的迁移目录与 GORM model 严重漂移**：`channel_monitors`（5 张表）、`quota_rpm`、`model_status_logs` 等模型**没有任何迁移文件**。用 `atlas migrate diff`（dir→desired）会一次性生成 18 KB 含 15 条 `DROP INDEX` 与大量 `ALTER COLUMN TYPE` 的无关变更 | 我在纯净 main worktree 复现；正因如此本特性的迁移改用 `atlas schema diff`（main model schema → 本分支 model schema）语义求差，**只含本特性增量** |
| **D2** | **`tools/atlas-loader` 输出非确定性**：同一 worktree 连续两次 `go run main.go` 差 12 行（GORM 约束顺序随 map 迭代变化），会让 `atlas migrate diff` 产生伪 DDL | 实测两次输出 `diff` |
| **D3** | `test/bedrock_model_id_check.go:98` 编译失败（`bedrock.NewListFoundationModelsPaginator` 未定义） | `go build ./...` |
| **D4** | `internal/controlplane/repository` 的 `TestListHistoryPaged_respectsPageSize` 失败（GORM sqlite dialect 无法生成 `gen_random_uuid()`） | `go test`，在 HEAD 复现一致 |
| **D5** | `tests/` 下 4 个包 `go vet` 失败（`fakeScheduler` 缺 `UpdateSticky` 等接口漂移） | 在 HEAD 临时 worktree 复跑，输出逐字相同 |
| **D6** | 生产库 `atlas_schema_revisions` 表**不存在** → 迁移从未通过 Atlas 应用（或 baseline 未标记）。直接跑 `migrate-prod` 会尝试从 baseline 重放全部迁移 | 需先查明并按 `scripts/atlas-baseline.sh` 标记 |
| **D7** | `.gitlab-ci.yml` 的 `migrate-prod` 缺 `--exec-order non-linear`（`migrate-test` 有） | 本次迁移时间戳已 > main 最大值，线性有序，**未修改 CI** |

> D7 说明：v3 §6.1 曾建议给 prod 补 `--exec-order non-linear`。我**没有改**——因为加它会让迁移乱序变成静默通过，掩盖 D1/D2 这类真问题；正确做法是保证时间戳有序（本次已保证）。

---

## 7. 上线前置清单

1. **迁移**：先查明生产 `atlas_schema_revisions` 缺失原因（D6）→ 必要时 `./scripts/atlas-baseline.sh prod` → 再应用 `20260921064216_add_trace_archive.sql`。该迁移在 5.29M 行的 `call_traces` 上使用 PG 11+ **快速默认值**，不触发表重写。
2. **开关**：`archive.write.enabled=false`、`archive.read.enabled=true` 上线，验证 writer no-op 后再开写。
3. **磁盘**：`/datalog` 需合账（归档 + leakscan 189 GB + 保留期），`archive.retention_days` 建议 180 而非 365（v3 §7.2）。
4. **/data 时间窗**：`VACUUM call_traces` 是止涨的关键动作，优先于 `pg_repack`（后者需约 532 GiB，当前不可行 —— v3 §6.3）。
5. **决策**：D-1/D-2/D-3 三项（见 v3 §2）与 P1 回填是否纳入本期。
