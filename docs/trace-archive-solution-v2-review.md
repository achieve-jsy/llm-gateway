# 《调用链路正文归档 · 完整方案 v2（审计版）》评审汇总报告

> **评审对象**：`docs/trace-archive-solution-v2.md`（943 行，16 章，11 张图）+ 其证据文件 `docs/trace-archive-p0-measurements.md`
> **评审方式**：8 个独立视角并行评审 → 每条结论交一名独立核验员做证伪核验 → 本报告汇总
> **本报告只做汇总与合并**：所有条目（含引用行号、原文、代码位置）均来自各视角原始结论及其核验结论；核验员对证据的更正已在本报告中采纳
> **核验结论口径**：`confirmed`（成立）/ `uncertain`（部分无法静态核实，保留存疑标注）/ `refuted`（证伪，已剔除）

---

## 0. TL;DR 结论表

| 项 | 结论 |
|---|---|
| **整体是否可用** | **方向可用，但不能按现状实施**。架构选择（分块 zstd + 每日 rid 索引 + manifest 三态机 + 离机副本硬闸）是对的，实测证据链也扎实；但「归档正确性校验 → 置空」这条链路上有 2 条 blocker，且方案自称的校验手段本身不成立，照此实施会静默丢失正文 |
| **Blocker** | **2 条**（均为「唯一副本静默消失」类，不可逆） |
| **Major** | **15 条**（校验/容量/保留期/运维落地路径，含 2 条「按方案实施即触发生产事故」） |
| **Minor / Nit** | **29 条 / 8 条**（文档口径、配置不符、验收不可执行、决策可用性） |
| **被证伪的误报** | **7 条**（已单列于文末，属审计信息而非问题） |
| **最关键 3 条** | ① `BLK-01` 重导覆盖式写盘 + 置空不校验 manifest/归档 → 一天正文在 PG 与归档两侧同时消失；② `BLK-02` 两步校验的分母可缩水到 0、`_SUCCESS` 重导时是陈旧标记 → 校验不构成独立防线；③ `MAJ-01` 全链路无内容级校验（sha256 只写不读、request_id 集合核对被砍、行数来自写侧内存计数） |
| **紧随其后（同属阻塞级，建议一并修）** | `MAJ-07` K1/N1 全流程没有 VACUUM 步骤而验收要求 `/data` 停止增长（`/data` 只剩 54 GiB、2.5~5 天写满）；`MAJ-05` 回填/节流三处互斥导致回填不触发、验收不可达；`MAJ-13` 迁移 062/063 未提交且乱序，生产迁移任务必挂 |
| **一句话给作者** | 方案的**测量与取舍质量很高**，但这 943 行里「可审计」的成色不均匀：§A/§C/§L 的数字大多经得起复算，而 §F/§G/§H/§K/§N 的**校验与运维流程**与现有代码/配置存在系统性偏差，恰恰这一段是数据不丢的唯一防线 |

### 0.1 严重度分布（按核验后的 `corrected_severity` 重排）

| 严重度 | 数量 | 分布 |
|---|---|---|
| Blocker | 2 | 全部在「数据安全与不可逆风险」 |
| Major | 15 | 数据安全 5、生产可行性与运维 4、容量与保留 3、算术一致性 1、代码一致性 1、需求契合 1 |
| Minor | 29 | 分布最广，主要为文档口径、配置/清单不符、验收不可执行 |
| Nit | 8 | 措辞、单位、计数、文档结构 |

### 0.2 诚实性说明

- **不是「处处有问题」**：方案中被核验为**正确**的关键判断有 20 余条（见 §5），包括容量区间的取保守下界、`WithDecoderMaxWindow` 不会自伤、`fast default` 不重写表、`0.878` 换算系数有两条独立来源等。§C/§L 的算术绝大多数复算无误。
- **但也不是「小修即可」**：2 条 blocker 直接推翻 §F2「两步校验是 R4 的唯一自动防线」这一自述；若不修，方案在「归档正确性」上实际处于**无可信校验**状态。
- **不确定性已标注**：`A-07`、`A-12` 在核验中判为 `uncertain`，本报告保留该标注（前者需生产 `du -sb`，后者需现场对账），未强行下结论。

---

## 1. 问题索引（合并后）

> 合并规则：同一机制的不同视角结论合并为一条，保留最完整的证据，严重度取各组成部分的最高 `corrected_severity`。完整映射见 §7.1。

| ID | 严重度 | 标题（简） | 来源视角 |
|---|---|---|---|
| BLK-01 | **blocker** | 重导覆盖式写盘 + 置空不校验 manifest/归档 + 指针按天批量 UPDATE | 数据安全 / 遗漏 / 技术正确 |
| BLK-02 | **blocker** | 两步校验假阳性：缩水分母 + 陈旧 `_SUCCESS` | 数据安全 |
| MAJ-01 | major | 全链路无内容级校验（sha256 只写不读、无巡检、集合核对被砍） | 技术正确 / 数据安全 / 遗漏 |
| MAJ-02 | major | index 二分依赖 DB collation == Go 字节序，排序契约未写死 | 技术正确 |
| MAJ-03 | major | gzip「~236 GiB ⇒ 零收益」不可复现（应 ~64~70 GiB） | 算术一致性 |
| MAJ-04 | major | `archive_retention_days=365` 与可用盘量矛盾；删除语义空缺 | 容量保留 / 遗漏 / 需求契合 |
| MAJ-05 | major | 回填/节流三处互斥；`BackfillOnce` 语义误用；空天配额未定义 | 生产运维 / 容量保留 / 遗漏 / 需求契合 |
| MAJ-06 | major | pg_repack 临时空间口径错误（官方 2×）+ `--tablespace` 语义误读 | 生产运维 / 容量保留 |
| MAJ-07 | major | K1 第 7 步「/data 停止增长」与 §K3 冲突，K1/N1 无 VACUUM 步骤 | 容量保留 / 需求契合 |
| MAJ-08 | major | R8 应急选项 a 与 R4 冲突；永久丢失态无检测/无约束 | 数据安全 / 生产运维 / 需求契合 |
| MAJ-09 | major | 置空前不校验该天归档存在/可读；读侧永久丢失态无告警 | 数据安全 |
| MAJ-10 | major | 离机副本闸仅 `os.Stat`（无 dt/无 sha/无新鲜度），无 RPO/RTO | 数据安全 / 生产运维 / 遗漏 |
| MAJ-11 | major | 磁盘配额闸只挡 archiver，不挡 janitor | 数据安全 |
| MAJ-12 | major | `ARCHIVE_ENABLED=false` 回滚同时关掉点读器，静默空正文 | 数据安全 |
| MAJ-13 | major | 迁移 062/063 未提交且乱序，migrate-prod 缺 `--exec-order` | 生产运维 |
| MAJ-14 | major | leakscan 189 GB 归因未经核实却被当作「可释放」 | 代码一致性 / 算术一致性 |
| MAJ-15 | major | `/datalog ≥ 500 GiB` 前置与现状 368 GiB、U6 互锁 | 算术一致性 / 需求契合 |
| MIN-01 | minor | §H2 未定义二分边界分支，「未命中」与「定位错块」不可区分 | 技术正确 |
| MIN-02 | minor | §H3 漏算块内 JSON 全扫成本，把 CLI 解压吞吐当 Go 解码吞吐 | 技术正确 / 需求契合 |
| MIN-03 | minor | §E4 `WithDecoderMaxWindow` 理由写反；写侧可配、读侧硬编码 | 技术正确 |
| MIN-04 | minor | 分层与改造量低估：读路径不走接口，写路径不经 store | 技术正确 |
| MIN-05 | minor | §I4/§H4 内存数字无实测出处，漏计 2×窗口/导出批次/块暂存 | 技术正确 / 生产运维 / 需求契合 |
| MIN-06 | minor | 现状正文 236/265.84 GiB 两个口径；「11 天」与行数速率不相容 | 算术一致性 |
| MIN-07 | minor | 「82 块」「245 ms」是 256 MiB 当 256 MB 算出来的 | 算术一致性 |
| MIN-08 | minor | 证据文档月增口径「3.04 万行/天 × 30」推不出 59 GiB | 算术一致性 / 容量保留 |
| MIN-09 | minor | §I1「配置项完整表」与代码多处不符（含键名写错会静默失效） | 代码一致性 / 算术一致性 / 遗漏 / 需求契合 |
| MIN-10 | minor | 压缩率口径混用（三种实现/输入规模），分块后未按 256 MiB 验证 | 容量保留 / 算术一致性 / 技术正确 |
| MIN-11 | minor | 容量模型是单日 2% 抽样外推，无跨日/峰值/增长余量 | 容量保留 |
| MIN-12 | minor | §I2 漏列 compose 的 `ARCHIVE_ENABLED:-true`，K1 第 2/3 步无法达成 | 容量保留 / 生产运维 / 代码一致性 |
| MIN-13 | minor | 只写 `go get`，未含 `go mod vendor`（CI/Makefile 用 `-mod=vendor`） | 代码一致性 |
| MIN-14 | minor | §H1 标「不变」的文件恰是并发闸要改的文件 | 代码一致性 / 需求契合 |
| MIN-15 | minor | 改造清单遗漏测试文件与前端/配置载体 | 代码一致性 |
| MIN-16 | minor | index/manifest 未要求原子替换；孤儿块清理未定义 | 数据安全 |
| MIN-17 | minor | 「`atlas_schema_revisions` 不存在 ⇒ 迁移从未执行」可能是误判 | 生产运维 |
| MIN-18 | minor | 归档链路无健康指标/告警出口，N1 第 11 条验收不可执行 | 生产运维 / 需求契合 |
| MIN-19 | minor | §K3 对 VACUUM 的表述与 PG 18 文档不符，因果未证 | 生产运维 |
| MIN-20 | minor | 归档 SQL 非 sargable，062 部分索引服务不了主路径；J3/R7 量级错 | 生产运维 / 遗漏 / 算术一致性 |
| MIN-21 | minor | 应用 062/063 会顺带解除 call_logs 30 天清理闸，未登记 | 生产运维 / 遗漏 |
| MIN-22 | minor | 四条更简替代路线未评估、未否证 | 遗漏 |
| MIN-23 | minor | 分块+索引+并发闸可能过度设计（用户已接受延迟） | 遗漏 |
| MIN-24 | minor | 权限与审计整段缺失（GET 正文无审计、`/my` 路由无属主校验） | 遗漏 |
| MIN-25 | minor | 明文落盘 + 明确不加密，未引用刚发生的泄漏事件与补偿控制 | 遗漏 |
| MIN-26 | minor | 无「删除单条 trace 正文」路径；保留期到期读语义未定义 | 遗漏 / 数据安全 |
| MIN-27 | minor | `archive_ref` 冗余、`archived_at` 语义与文档相反 | 遗漏 |
| MIN-28 | minor | N1 第 3 条「≥ 9x」是空洞验收，防不住 R1 | 需求契合 |
| MIN-29 | minor | 风险登记册遗漏「晚到行」 | 需求契合 |
| NIT-01 | nit | §I4「编码并行 2 块」与 `WithEncoderConcurrency(1)` 矛盾 | 技术正确 |
| NIT-02 | nit | §C1「271 GB = heap + TOAST 265.84 GiB」括注不闭合 | 算术一致性 |
| NIT-03 | nit | §K4 保守月增 59 GiB 偏低；「6~10 个月」前提未标注 | 算术一致性 |
| NIT-04 | nit | §K4 把 189 GB 直接当 GiB 加到 368 GiB（**存疑**） | 算术一致性 |
| NIT-05 | nit | §J2 说迁移 062 是「4 列」，实际 5 列 | 算术一致性 |
| NIT-06 | nit | 未分块延迟 §A3/§B3 写 15~31 s，实测外推为 17~31 s | 算术一致性 |
| NIT-07 | nit | 决策可用性：关键结论埋藏、两套清单、无工作量/责任人 | 需求契合 |
| NIT-08 | nit | 引用与步骤顺序小错（§146 行号当章节号；G2 第 7/8 步与现码相反） | 需求契合 |

---

## 2. Blocker（2 条）

### BLK-01 重导是覆盖式写盘，且置空不校验 manifest/归档前置——一次 manifest 落库失败即可静默丢失一天正文

**严重度**：blocker ｜ **来源视角**：数据安全与不可逆风险（主）、遗漏/更简方案（F10）、技术正确性（T03）、需求契合（F-18 顺序）

**为什么是问题（含触发场景）**

- §F1 明确保留 `Verified --> Archiving: 归档文件损坏/缺失 回退重导` 这条边，§G2/§H2 的重导对同一天使用**固定文件名**并 `os.Rename` 覆盖；全流程**没有任何「该 dt 是否已有已验证归档 / 是否已有置空行」的前置条件**，`ArchiveDay(force=true)` 更是直接绕过 verified/cleaned 短路。
- 重导的导出谓词只取「当前 PG 还有正文且 `archive_status IN (0,1)`」的行 → 已置空(2)的行既不进新文件、也不进 `pg_row_count` 分母 → `lineCount == pgRowCount` 仍可 **0 == 0** 通过 → rename 覆盖旧归档 → 该天正文在 PG 与归档两侧同时消失，manifest 被改写成新行数与新 sha256，事后无法分辨该天是完整还是残缺。
- **真实可达路径（核验员修正后的链条，无需人为误操作）**：
  1. 现码顺序与设计相反——`writeArchivePointers`（`archive_service.go:467`）在 `markManifestVerified`（`:470`）之前，且后者 `Save` 失败只 `slog.Warn`（`:597-599`）不回滚刚写入的 `archive_status=1` → 产生「行=1、manifest=archiving」的持久不一致态（DB 抖动、statement timeout、容器 OOM 杀进程都能制造，方案自己 §R2/§I4 也预测了 OOM）。
  2. janitor 候选集只按行级 `archive_status=1 AND ts<cutoff AND 正文非空`（`archive_janitor.go:154-166`），**完全不查 `trace_archive_manifests.state`**；`offhostCopyOK` 通过后直接置空。
  3. 同一轮 cron 里 archiver 之后无条件跑 janitor（`archive_cron.go:78-86`）。
  4. 结果：**跨 cutoff 边界的「部分置空天」**——`writeArchivePointers` 按天把该天全部有正文行置 1，janitor 只置空 `ts < cutoff` 的部分，残余行使 `pending > 0` → `markCleanedDays` 跳过 → manifest 停在 `archiving`；次日 `exportDay` 只导出残余行、行数自洽、rename 覆盖整天归档 → 被置空那段正文在 PG 与归档两侧同时消失且无任何告警。叠加 MAJ-11（配额闸只挡 archiver）或 `days_per_night` 节流会让该天在 janitor 置空后更长时间得不到重导，路径更宽。
- **同一根因的第二条路径**：`writeArchivePointers` 的 `UPDATE` 是**按天范围**而非与本次导出行集合同源。在「导出扫描结束 → 指针 UPDATE」之间落入该天（ts 早于 waterline，例如跨零点长流式请求或时钟偏移）且 `request_id` 排在游标之前的新行，会拿到 `archive_status=1` + 指向不含它的 `archive_ref`；次日 janitor 到窗口即置空 = 永久丢失。单日归档数分钟、回填数小时（§L3）的窗口并不小。

**证据**

| 类型 | 位置 | 要点 |
|---|---|---|
| 方案 | `docs/trace-archive-solution-v2.md:286` | 「Verified --> Archiving: 归档文件损坏/缺失<br/>回退重导」 |
| 方案 | `:341-345` | G2 步骤 4-8；`:349`「先用临时名写，成功后 rename → 避免半成品被读取」（只约束块，未禁止覆盖已验证归档） |
| 方案 | `:345` | `UPDATE call_traces SET archive_ref='dt=YYYY-MM-DD/', archive_status=1 WHERE ts ∈ 该天 AND archive_status=0` |
| 代码 | `internal/observability/service/archive_service.go:467-470` | 先 `writeArchivePointers`、后 `markManifestVerified`（与 §G2 步骤顺序相反） |
| 代码 | `archive_service.go:597-599` | manifest `Save` 失败仅 `slog.Warn`，不回滚 |
| 代码 | `archive_service.go:448`、`:454`、`:541-554`、`:558-571` | 只比 `lineCount != pgRowCount`；`os.Rename` 无覆盖保护；`pgRowCount` 与导出同谓词；指针 UPDATE 按天范围 |
| 代码 | `archive_janitor.go:154-166`、`:80-93` | 置空只按行级 `archive_status=1`，无 manifest 状态校验 |
| 代码 | `cron/archive_cron.go:78-86` | archiver 与 janitor 同轮背靠背 |

**修复建议**（建议全部采纳）

1. 把 manifest 状态变成置空的**必要条件**：janitor 候选查询 `JOIN/EXISTS trace_archive_manifests` 并要求该天 `state='verified'`；重导开始前显式作废旧 `_SUCCESS`（改名/删除）并把该天从 janitor 候选排除。
2. **重导一律写新版本**（如 `dt=YYYY-MM-DD/v2/` 或带 epoch 后缀的 part/index），旧版本保留到新版本 verified 且完成全量对账后再由显式流程回收；**严禁原地 rename 覆盖已 verified 归档**。
3. 导出前**冻结期望行集合**（该 dt 的 min_rid/max_rid/行数/期望 sha256 或分片清单）；重导必须校验新文件行集包含上次已验证行集，缺行即失败并告警，不得推进 verified。
4. 行指针 `UPDATE` 限定为本次导出的 `request_id`（或至少 `request_id <= 本次导出最大 rid`），并在 UPDATE 后断言 `rowsAffected == lineCount`，不等则回滚本次 verified 并告警。
5. `exportDay` 内**先完成 manifest verified（失败即中止、不写指针）**，修正现码反序；`markManifestVerified` 失败必须返回错误，不得只 `Warn`。
6. 把 §F3 不变量固化成校验查询/单测。

---

### BLK-02 §F2 两步校验可产生假阳性：分母是「当前 PG 仍有正文的行数」（可缩水到 0），且重导时 `_SUCCESS` 是陈旧标记却仍被当作当天完整的证明

**严重度**：blocker ｜ **来源视角**：数据安全与不可逆风险

**为什么是问题（含触发场景）**

- **(a) 分母可缩水**：校验为 `count(归档行) == pg_row_count`，而 `pg_row_count` 的谓词（`archive_service.go:543-549`）与导出谓词（`:527-528`）**完全相同**：`archive_status IN (0,1) AND (request_body IS NOT NULL OR response_body IS NOT NULL)`。因此已置空行、以及从来没有正文的行（生产 6/7/8 月共 3,421,009 行处于此状态）既不在归档里、也不在校验分母里——「归档漏了这些行」在数学上**恒真、永远不会被发现**。极端情形：某天正文全部被置空后重导，`pg_row_count=0`、归档 0 行 → 校验通过 → **空归档被判 verified**。
- **(b) `_SUCCESS` 不失效**：§E1:218 断言「`_SUCCESS` 最后写入，存在即代表当天归档完整」，§F2 第 2 步就是 `Head` 成功。但设计从未要求在开始重导前删除或改名旧 `_SUCCESS`；实现里更是无条件 `os.WriteFile`（`archive_service.go:459-463`），导出开始时既不清理也不加 epoch。于是任何一天的第二次及以后导出，第 2 步校验**恒真**。在分块设计下（§G2 步骤 3a 每块单独 rename），重导中途被杀会留下「新块 + 旧 index + 旧 `_SUCCESS`」的混合目录，对读取端表现为完整。
- **(c) 验收会把假阳性当成功**：archiver 从 `MIN(ts)` 起全量枚举（`archive_service.go:258-298`），会为 6/7/8 月这些正文已全部不存在的天生成「空归档 + `_SUCCESS` + manifest=verified」；而 §N1 第 6 条的验收正是「manifest 全部 verified、行数一致」——**验收会通过，而实际上那 78 天一行正文都没有**。验收标准因此不能证明校验有效。
- **(d) 口径矛盾**：§F2 原文写的是 `count(PG 该天行数)`（不是「有正文的行数」），与实现谓词不一致；若按字面实现，分母含无正文行 → 归档行数恒少于总行数 → **该天永远无法 verified**（失败安全但整条链路停摆）。口径本身也是缺陷。
- *核验员更正*：「写完部分块、index 还没写的情况下被杀就会把 manifest 置 verified」不准确——进程被杀不会执行 manifest 更新；真实后果是**陈旧 `_SUCCESS` 使该目录被判定完整**，以及后续以缩水分母完成的校验。两步校验是 §F3 不变量（`archive_status=2 ⇒ 归档必已 verified`）的唯一执行手段，而同谓词分母 + 陈旧 `_SUCCESS` 使其**不构成独立校验**。

**证据**

| 类型 | 位置 | 要点 |
|---|---|---|
| 方案 | `:305-308` | F2 两步校验：「① count(归档行) == count(PG 该天行数) ② `_SUCCESS` 存在」 |
| 方案 | `:218`、`:223` | 「`_SUCCESS` 最后写入，存在即代表当天归档完整」/「必须最后写，是 §F 两步校验的第一半」 |
| 方案 | `:876` | §N1 第 6 条验收「manifest 全部 verified；`call_traces` 无任何 `archive_status=2`」 |
| 代码 | `archive_service.go:447-451` | 校验①只比 `lineCount != pgRowCount` |
| 代码 | `archive_service.go:459-463` | 无条件 `os.WriteFile(_SUCCESS)`，导出前不删除、不加 epoch |
| 代码 | `archive_service.go:543-549` | `pgRowCount` 谓词排除 `body IS NULL` 与 `archive_status=2`（= 导出谓词） |
| 代码 | `archive_service.go:258-298` | 自 `MIN(ts)` 起全量枚举天 |
| 证据 | `docs/trace-archive-p0-measurements.md:56-61` | 6/7/8 月有正文行数均为 **0** |

**修复建议**

1. 导出开始即把 `_SUCCESS` 改名为 `_SUCCESS.stale-<ts>`（或删除），失败路径保证其不存在；`_SUCCESS` **内容**写入 attempt/epoch 与块清单 sha256，读取端与校验端都要求 epoch 匹配。
2. 校验分母改为该 dt 在**冻结快照时刻的全量行数**（含已置空/无正文行），并配合冻结的期望行集合比对（见 BLK-01 建议 3）；禁止用「当前还有正文的行数」当分母。对 `archive_status=2` 的行单独统计「归档内是否可定位」的数量并纳入验收。
3. §N1/§K1 验收增加「逐天核对 `manifest.row_count == 该天有正文行数`」与「归档内 rid 集合包含 PG 中已置空 rid 集合」，不允许只看 `state=verified`。
4. 统一 §F2 口径表述，明确写出分母定义。

---

## 3. Major（15 条）

### MAJ-01 全流程没有任何环节回读比对 sha256 或内容集合，也没有归档巡检：归档损坏/截断/漏行不可检测

**严重度**：major（由 blocker 下调：属「缺少发现手段」而非设计主动制造丢失）｜**来源**：技术正确性（T01）、数据安全（DS-03）、遗漏（F9）

**为什么是问题**
- **sha256 只写不读**：写入点 `archive_service.go:420/466/586`、`local.go:100-121`；读取端 `readbody.go:39-88` 只做 `Get` + gzip 解压 + 逐行扫描，不读 manifest/index、不比对摘要；§H2 新流程同样没有校验步骤；旧定稿「SHA256 写入 manifest 供后台复核」在 v2 里连「后台复核」这个动作都没有落到任何流程（§O 未决项也没有）。生产 `/datalog` 是单盘 ext4，无副本校验、无 bit-rot 巡检。
- **行数校验建立在写侧内存计数上**：`lineCount := s.streamExport(...)`（`archive_service.go:428`）是流式写出时的累加值，`:448` 用它和 `pgRowCount` 比较，**从不回读已落盘文件重新统计**。§G2 步骤 3a 只写「写临时文件，成功后 rename」，漏掉了现码 `:439-445` 的 `f.Sync()`，也没有 fsync 目录 → ext4 掉电后可能得到「文件在 + `_SUCCESS` 在」的坏档；janitor 置空后 PG 无正文 → 静默且不可恢复。
- **旧设计的 request_id 全量集合串接 sha256 核对被砍掉**（`trace-archive-deletion-migration-design.md:282-284`），而它正是唯一能证明**内容一致**的手段（自述「request_id 仅 ~36B，78 天全量 ~100 MB，比对代价可忽略」）。行数相等不是内容相等：任何「漏一行 + 多一行」「行内容错位」「块与 index 不一致」都能让 count 通过；`index.jsonl` 的 `rows/first_rid/last_rid` 与 manifest 的 `rows/sha256` 之间也没有任何交叉校验。
- §E1:218 进一步断言「`_SUCCESS` 存在即代表当天归档完整」，属过度承诺——它只是零字节标记。
- *核验员更正*：① 「archive_service.go:420-421 对未压缩流求哈希」**不成立**——`:421` 的 `MultiWriter` 是 gzip writer 的**输出端**，`h` 收到的是压缩后落盘字节，恰是可校验的产物哈希；② `local.go:121` 没有「供 manifest 双写」注释（该注释只在 `store.go:106`）；③ 「静默短写」大多会报错（`os.File.Write` 短写返回 `ErrShortWrite`，gzip/zstd `Close` 的错误被检查），残余风险主要是掉电/位翻转类**无错误路径**；§K6 的季度随机恢复演练算抽样兜底，但覆盖不了逐天/逐块完整性。

**证据**：`:305-308`、`:342`、`:218`、`:338-339`；`archive_service.go:428-451`、`:420-421,466,574-586`、`:439-445`；`store.go:106`、`local.go:121`；`readbody.go:39-88`；`trace-archive-deletion-migration-design.md:282-284`；`trace-archive-p0-measurements.md` 全文无 RSS/sha 复核记录。

**修复建议**
1. 每块封块后**回读该 part（zstd 解码）重新统计行数**并与写侧计数比对，至少对末块做。
2. `index.jsonl` 每行加该块 sha256；`_SUCCESS` 只在「逐块回读校验通过」后写；读侧命中后校验该块 sha256（失败至少告警）。
3. 增加**每日归档巡检任务**：逐天/逐块重算 sha256 与 manifest/index 比对，同时校验 index 行数与块内实际行数，失败即告警并把该天 manifest 降级/冻结（禁止置空）。
4. 至少加回 **request_id 全量集合摘要比对**（导出侧与回读侧同算法），或明确写出「不做该核对的理由 + 残余风险」。
5. §G2 明确写 part 后 `fsync(part)` 再 rename、并 `fsync` 目录。若保留 sha256 字段，请给出它的消费方，否则应删除该字段与相关计算，避免「有校验字段但没有校验」的误导。
6. §N2 的回滚承诺改写为「损坏场景只能从异机副本恢复」，以「异机副本 sha256 比对通过」为唯一依据。

---

### MAJ-02 index 二分定位依赖「DB collation == Go 字节序」，而 `request_id` 是客户端可控任意文本，方案未固定排序契约

**严重度**：major ｜**来源**：技术正确性与实现可行性

**为什么是问题**：§E1 用「导出即 `ORDER BY request_id`」来保证 index 首尾 rid 单调，§H2 步骤 4 再在 Go 里用 `last_rid >= requestID` 二分。二者**只有在 DB collation 为 C/字节序时等价**。而 `call_traces.request_id` 是 text 主键（`model/call_trace.go:11`），其值直接取自客户端请求头 `X-Request-ID`（`middleware.go:22-28`：`id := c.GetHeader("X-Request-ID")`，为空才用 `uuid.New().String()`），即可以是任意大小写混排、含标点、非 UUID 的字符串。生产常见 `en_US.UTF-8`/ICU collation 下排序是「忽略/可变权标点 + 大小写三级差异（小写先于大写）」，与 Go 逐字节比较不一致：例如 `'A'(0x41) < 'a'(0x61)` 是字节序，而 collation 认为 `"abc…" < "ABC…"`，同一批数据在两种序下**相对顺序反转**。此时二分可能落到相邻的错块，步骤 6 直接返回 `CodeNotFound`，按 §H5 被解释为「归档未命中（数据异常，需告警）」，而该记录其实就在隔壁块里；janitor 置空后归档是唯一副本，用户侧表现为「正文永久不可回放」。方案全文未出现 collation 字样，实测脚本也只 `SHOW default_toast_compression`，生产库 collation 无记录，§E1 只说「字典序」而非「字节序」。（若生产库确为 C 则实际风险消失，但排序契约仍未写死。）

**证据**：`:222`（「块内按 `request_id` **字典序连续**（导出即 `ORDER BY request_id`），因此 `index.jsonl` 的首尾 rid 单调递增」）、`:431`、`:242`；`archive_service.go:530`（`ORDER BY ct.request_id`，无 COLLATE）、`archive_janitor.go:160-161`（keyset 同）；`middleware.go:22-28`；`model/call_trace.go:11`。

**修复建议**：导出/keyset 统一固定为 C 序——`ORDER BY ct.request_id COLLATE "C"` 与 `ct.request_id > ? COLLATE "C"`（janitor 与 `BackfillTraceKeys` 的游标同理，保持内部一致）；或在写 index 时额外落一个「字节序可比较键」。并在 §E1/§H2 明确写出前提「index 顺序 = C 序 = Go 字节序」，补大小写混排、含 `-`/`_`、非 UUID id 的边界单测。

---

### MAJ-03 §A1/§D3/§L1 的「gzip 方案归档 ~236 GiB ⇒ 零收益」不可复现，与实测 3.47x 直接矛盾（正确值约 64~70 GiB）

**严重度**：major ｜**来源**：算术与文档内部一致性

**为什么是问题**：用文档自己给的比率复算——现存有正文行 1,259,124 行，jsonl 口径 205,879 B/行 ⇒ jsonl 总量 259.2 GB = 241.4 GiB；gzip -6 实测 3.47x，故 gzip 归档应为 **69.6 GiB**（jsonb 文本口径 221.1 GiB ÷ 3.47 = 63.7 GiB），即约 64~70 GiB，相对现状 265.84 GiB 仍是 ~3.8 倍缩减。任何一步都推不出 236 GiB；**236 GiB 恰好等于 §L2「11 天 × 21.4 GiB/天」的现存正文量（235.4 GiB）**，即该数字疑似把「现状存量」误抄成了「gzip 结果」，等价于假定 gzip 压缩比 ≈ 1.0，与实测 3.47x 相差 3.4 倍。而且文档 §3 自己说明「压缩率随文件增大而提升」，440 MiB 样本的 3.47x 对全天文件只会更好。**影响**：codec 切换的核心理由（A1 标「致命」、D3 标题「为什么必须换 codec」）被夸大——gzip 已能把 266 GiB 降到 ~70 GiB，zstd 的收益是再降 2.8~4 倍，而非「从零收益变成有收益」。（核验确认：选 zstd 的方向不变；226,700、188,534×1.092=205,879、1.20 倍关系、14~25 GiB、642 GiB、21.4 GiB/天、1047 MB/s、点读 17/31 s 外推等均复算无误，问题集中在 236 GiB 这一个数。）

**证据**：`:13`「实测 gzip 3.47x vs zstd 19.61x；gzip 方案归档 ≈ 236 GiB ≈ 现状 266 GiB，**零收益**」；`:199`（mermaid「归档 ~236 GiB / ≈ 零收益，项目白做」）；`:759`；`:64`、`:769`；`measurements:213`、`:223`。

**修复建议**：把 A1/D3/L1 的 gzip 值改为按同一 jsonl 语料与 3.47x 实算的 ~70 GiB（文本口径 ~64 GiB），结论改为「gzip 可把 266 GiB 降至 ~70 GiB，zstd 再降 2.8~4 倍」，并把 A1 的「致命」降级为「高」。若坚持 236 GiB，必须给出可复现的推导式。

---

### MAJ-04 建议的 `archive_retention_days=365` 与本文自己的可用盘量自相矛盾；保留期决策缺容量反推与删除语义

**严重度**：major ｜**来源**：容量模型（CAP-01）、遗漏（F1）、需求契合（F-02）

**为什么是问题**
- 365 天 = 12 个月 × 34~59 GiB/月 = **408~708 GiB**；而 §K4 自述 `/datalog` 现状 368 GiB 只能支撑「6~10 个月」、释放遗留后 ~557 GiB 支撑「9~16 个月」。故**保守口径下 708 GiB 在任何情形下都不成立**（708 > 557），**现实口径下 408 GiB 也已超过现行 368 GiB**。建议值没有按可用容量做过任何反推（368 ÷ 59 ≈ 6 个月 ≈ 180 天；÷ 34 ≈ 300 天）。
- §N1 第 11 步与 §R3 缓解都要求按该值实施，而 §O 的 U5 又把业务值完全交回业务确认，**未把「盘量倒推出的上限」列为输入**——这是该决策最关键的一条约束，缺失。只读 §A 决策者会误判 A5（新增保留策略）已闭环；而 A5 的默认值 0=永不删恰是旧定稿拍板、被 P0 判为 6~10 个月填满 `/datalog` 的策略。
- **删除语义无设计**：置空后归档正文是唯一副本，删除后 PG 仍留 `archive_status=2` 指针，违反 §F3「任何时刻 `archive_status=2` ⇒ 归档文件必已 `verified`」与 §B1.4 的「正文要么在 PG、要么在归档」；而 §H5 又把这种设计内行为定义为「`CodeNotFound`（数据异常，需告警）」→ 保留期一生效就会产生告警风暴，且系统无法区分「按策略正常过期」与「归档损坏丢失」。062 DDL 无 `deleted_at`，`config.go` 结构体无该字段，§J2 标为「v2 新增（待写）」。
- *核验员更正*：① 「在任何情形下都不成立」只对保守口径成立——现实口径 408 GiB 在释放遗留后的 557 GiB 下放得下（尚有 ~149 GiB 余量），§K1 的「≥500 GiB」前置在现实口径下也自洽；② 建议里「80%×590 − 保留块 − leakscan − backup」的算法重复扣减（472 是已用阈值，现行已用 193 G 本就在其中），且 `archive_service.go:736` 明确「水位只是告警阈值」、只有 `max_disk_gb>0` 才 blocked。具体天数略有出入，但「365 天所需的 408~708 GiB 从未在 §K4/§U5 写出」这一缺失是真实的。

**证据**：`:531`「0（永不删）/ 建议 365（新增）」；`:727-728`；`:758`；`:730-732`「必须二选一」；`:871`（N1 第 11 步）；`:902`（U5）；`:314`；`:504`；`:17`（A5）；`trace-archive-final-design.md:356`「保留周期 0=永久、不 TTL 删除」。

**修复建议**：在 §K4/§U5 写出容量方程并给出可执行区间，例如「`retention_days ≤ (0.8×盘容量 − 保留块 − 非归档占用 − repack 预留) / 月增 × 30`」；按本文数字即「leakscan 未释放 + 保守口径 ⇒ ≤ ~130 天；释放 leakscan + 现实口径 ⇒ ≤ ~370 天」。把「365 起步」改写为「365 天需要 ≥708 GiB（保守）/408 GiB（现实）」的显式前提，并把 `max_disk_gb` 从 0 改成与保留期配套的具体值。补「归档删除」一节：删除预处理（同一事务内处置 PG 指针）、点读返回（建议 410）、§F3 不变量显式加 retention 例外、manifest 增 `deleted_at/deleted_by/deleted_reason`、新增 `deleting` 态；若决定不删，§A5 就写「决策 = 0 永久 + 6~10 个月后扩容」。把 U5 改为「批准前必答」。

---

### MAJ-05 回填与节流三处互斥（`days_per_night=2` / 「一次窗口回填」/ 「一天后全部 verified」），`BackfillOnce` 语义被误用，空天是否占用配额未定义

**严重度**：major ｜**来源**：生产运维（OPS-09）、容量保留（CAP-03）、遗漏（F2、F-04）、需求契合（F-03）

**为什么是问题**
1. **`BackfillOnce` 与归档回填无关**：`archive.backfill_once` 在代码里只驱动两件事——① 结算 rollup 回填（`router.go:453` → `rollupSvc.BackfillIfNeededOnce`，30 分钟超时）；② `ArchiveService.BackfillTraceTaskFields`（`router.go:643-651`），而后者只回填 `task_id`/`documents_count` 两个标量列（`archive_service.go:176-182`「Phase A」）。**与按天归档正文没有任何代码通路**，且 §I1 自称「配置项完整表」却没列该键。照 §G4「`BackfillOnce=true` 可在一次窗口内完成，不存在多轮慢慢补的问题」与 §K2 使用，**归档回填一天都不会发生**，还附带两个无关重任务（一次 ~126 万行 UPDATE + 一次结算回填）。
2. **`days_per_night=2` 与「一次窗口完成」互斥**：§G3/§I1 新增「每天最多归档 `days_per_night` 天（默认 2）」，§G4/§L3 却称「11 天 ≈ 2 小时、一次窗口完成」。11 天 ÷ 2 = **6 晚**；若按 waterline 起点 `MIN(ts)`（= 2026-06-04，110 天）算则需约 **55 晚**。
3. **验收不可达**：§K1 第 5 步与 §N1 第 6 步把验收定为「观察 1 天：manifest 全部 verified、行数一致」——按 2 天/晚，一晚只能归档 2~3 天，该验收按方案自身配置**不可能通过**。
4. **空天配额头**：现实现从 `MIN((ts AT TIME ZONE tz)::date)` 逐天枚举（`archive_service.go:259-261`、`:298`，无天数上限），06-04~09-09 约 **98 天**正文已被 leakscan 置空 → `pgRowCount=0`、`lineCount(0)==0` 通过 → 照样 rename + 写 `_SUCCESS` + manifest **verified**。这些空天若占配额，走到真有正文的 09-10 需约 **49 晚**；若不占，又违反 §G3 的「每天最多 N 天」。两种读法都推翻「一次窗口完成」，且第 5/6 步验收都不可达。
5. **节流在实现上也拦不住**：`archive_janitor.go` 的 `archiveLeakRows`（`:100-127`，`:120` 直接 `ArchiveDay`）会枚举「所有 `archive_status=0` 且 `ts < cutoff` 且正文非空」的天并逐天归档，**完全不受 `days_per_night` 约束**（且该阶段在 `offhostCopyOK()` 关卡之前执行）。
- *核验员更正/收窄*：§K1 第 4 步（`hot_window_days=15`）时现存正文只有 11 天、全部晚于 cutoff，janitor 当时确实无候选，故「该步立刻把存量天一次性补齐」不成立（要到第 7 步收紧到 3 天后才发生）；同理 §N1 第 6 步「manifest 全部 verified」在只归档 2~3 天时**仍可字面通过**，该子项偏弱。机制与矛盾本身属实。

**证据**：`:398-399`、`:409-411`、`:520`、`:616`、`:654-656`、`:876`；`config.go:94`、`:136-144`、`:234`；`router.go:453-457`、`:643-651`；`archive_service.go:176-182`、`:182-229`、`:258-261`、`:298`；`archive_janitor.go:100-127`；`measurements:212`。

**修复建议**：① 分开命名与说明两件事——「归档回填」单独定义（例如 `archive.backfill_from` 或一次性 CLI/管理接口），§K2 只保留「waterline 自然覆盖」，并把 `ARCHIVE_BACKFILL_ONCE` 的真实语义与副作用（~126 万行 UPDATE）写进配置说明；② 明确 `days_per_night` 口径（建议 `pgRowCount==0` 的天跳过且不计入配额）并给出回填工期算法，同时让它同样作用于 janitor 的补归档阶段（或明确取消该节流）；③ 把 §K1 第 5 步/§N1 第 6 步验收改为「按每晚 N 天推进，第 k 晚的验收范围」与「有正文的天全部 verified」；④ 明确 waterline 起点是表中最早日（110 天）还是存量正文最早日（11 天）。

---

### MAJ-06 pg_repack 临时空间口径错误（官方要求 2×，且 `--tablespace` 语义是把表搬走）；`/datalog` 与归档增长从未合账

**严重度**：major（由 blocker 下调：官方 2× 是经验上界，且 pg_repack 缺空间只会报错中止、不丢数据）｜**来源**：生产运维（OPS-02）、容量保留（CAP-02）

**为什么是问题**
- §K3 表格与 §K1 第 8 步把 pg_repack 的临时空间记为「≈ 266 GiB」，并断言「临时空间必须落在 `/datalog`（368 GiB 可用）」。但 pg_repack 官方文档 Requirements→Disks 原文要求 **「free disk space about twice as large as the target table(s) and its indexes … an additional 2GB of disk space is required」**——目标表 heap 2,060 MB + TOAST 265.84 GiB ≈ 268 GiB，按官方口径需 **约 536 GiB 额外空闲**，而 `/datalog` 只有 368 GiB（即便释放 leakscan 遗留的 189 GB 也只有 ~557 GiB）。因此 §K1 第 8 步与 §N1 第 10 条（验收「/data 已用下降 > 150 GiB」）在当前硬件上**无法执行**，「真正回收」环节断裂，只剩「VACUUM 止涨」。
- **`--tablespace` 的语义**是「把 repack 后的表搬到该表空间」（等价在线 `ALTER TABLE SET TABLESPACE`）；§K3.2 图里写 `--tablespace /datalog` 会把 **call_traces 本体永久搬到归档盘**，而不是只放「工作表」（核验员新发现，比原 finding 更硬）。
- §K3 表格第三行「VACUUM FULL call_traces / ≈266 GiB / 需停机窗口」同样不可行：PostgreSQL 的 VACUUM 语法**没有 tablespace 选项**，FULL 只能把新表写回**原表所在 tablespace**（/data 仅剩 54 GiB）——它不是可与 pg_repack 并列的备选项。方案也未提 pg_repack 需在 PG 容器内 `CREATE EXTENSION pg_repack`。
- **同一块 `/datalog` 被分别完整记账**：§K3 把 368 GiB 当作 repack 工作表预算，§K4 又把同一个 368 GiB 全部算作归档可增长预算（6~10 个月），两处从未放进同一张预算表。按本文数字合账：368 − 266（repack）− ~25（存量归档）= 仅剩 **77 GiB**，即归档开始后 **1.3~2.3 个月**内就必须完成 repack；叠加 MAJ-04（365 天）后该窗口必然关闭。repack 期间的表空间写入还会连带产生 WAL、`--jobs 4` 并行排序溢出 `work_mem` 到 `pg_temp`，两者默认都落在 PGDATA `/data`（仅剩 54 GiB），方案完全未预算；迁移 062 的 `CREATE INDEX ... WHERE archive_status=1` 同理。
- *核验员更正*：266 GiB 是「整表体积」上界，置空后 pg_repack 实际只需重写存活元组（3 天正文 ~64 GiB + heap 2 GiB ≈ 66~86 GiB），故预留量可能高估约 3 倍（仍需实测钉死）；U3 演练在测试库进行、不占用生产 `/datalog`；方案在 `:643/:671/:706` 三处已显式标注该约束，且把 repack 排在收紧之后数天内，「1.3~2.3 个月窗口」只在运维大幅拖延时才咬人。

**证据**：`:668-672`、`:620`、`:643`、`:704`、`:880`、`:900`、`:596-597`；`measurements:285-291`；pg_repack 官方文档 Requirements→Disks / Installation / Details；PostgreSQL 18 `VACUUM` 文档 Synopsis。

**修复建议**：① 把 K3 的临时空间口径改为 pg_repack 官方要求（≈2× 表体积 ≈536 GiB），并给出结论：现有两盘都不满足，必须先加盘/释放 leakscan 189 GB 后再评估，否则「回收 266 GiB」不在本次可交付范围；② 删除或降级 K3 表格中 VACUUM FULL 这一行；③ 修正 `--tablespace` 的描述（它是搬表，不是只放工作表），如需工作表落他盘应改用 `--temp-tablespace`；④ 明确 pg_repack 的前置（容器内安装 + `CREATE EXTENSION pg_repack`、低峰窗口、保守选项），写进 U3；⑤ 增加统一预算表（`leakscan 189 + 归档累计 + repack 预留 + 保留块 29 + backup 2.2 ≤ 561`）并写成硬约束与 `must-run-by` 检查项（N1 第 10 步增加执行时 `df /datalog` 可用 ≥ 300 GiB 的准入）；U3 演练补测置空后真实工作表体积（存活数据 vs 整表）与 repack 期间 `/data` 上 `pg_wal`/`pg_temp` 峰值。

---

### MAJ-07 §K1 第 7 步「收紧窗口 → /data 停止增长」与 §K3「单纯置空并不阻止 /data 增长」直接冲突，且 K1/N1 全流程没有 VACUUM 步骤

**严重度**：major（由 blocker 下调：VACUUM 以「建议顺序」明确给出，且 §R8 已把 /data 时间窗并列呈交）｜**来源**：容量保留（CAP-04）、需求契合（F-01）

**为什么是问题**：§K1 第 7 步（及其流程图 S7、§N1 第 9 条）把「/data 停止增长 / `/data` 增速趋零」列为本步骤的效果；但 §K3 用实测明确否定这一点——「`SET body=NULL` 只把 TOAST 元组标记为死元组 … 这意味着**单纯置空并不阻止 /data 增长**」，并给出「建议顺序：先 `VACUUM` 止涨（立即、零风险）→ 再择窗口 `pg_repack` 回收」，§K3.2 流程图还把「不执行 VACUUM」标为红色失败分支。然而 §K1 的 0~8 步、§K1.1 流程图、§N1 的 11 条验收里**没有任何一步执行 VACUUM**，也没有把 VACUUM 纳入 cron。照 N1/K1 实施，置空后 `/data` 继续以 11~23 GiB/天增长，而 §R8 实测只剩 54 GiB、2.5~5 天写满；pg_repack 是「择窗口」且依赖未验证的 U3 → 窗口到来前 PG 就可能因 `/data` 写满而写入失败，**正是本方案要防的生产事故**。按 §K3 的物理解释，§N1 第 9 条的验收必然失败。控制面水位也帮不上忙（`archive_service.go:736` 明确水位仅告警、只有 `max_disk_gb>0` 才阻断）。

**证据**：`:618-619`、`:636`、`:879`、`:660-661`、`:674`、`:688`、`:701-703`；`archive_janitor.go:72`、`:172-176`（置空语句不含 VACUUM）；`archive_service.go:736`。

**修复建议**：在 §K1 第 7 步之后、第 8 步之前插入显式步骤「执行 `VACUUM (ANALYZE) call_traces`，用 `pg_stat_user_tables.n_dead_tup` 与 `n_tup_upd` 复核死元组清空，并观察 24h 内 `/data` 是否停止增长」；§N1 第 9 条拆成「`archive_status=2` 按天增长」+「VACUUM 后连续 2 天 df 增速趋零」；补一条「autovacuum 是否够用」的观测项（给出 `autovacuum_vacuum_scale_factor` 建议值或把 VACUUM 加进 cron）；把 pg_repack 窗口与 `/data` 余量写成硬前置。

---

### MAJ-08 §R8 的应急选项 a 与 §R4「禁止手工 `SET body=NULL`」直接冲突；它正是制造「永久丢失态」的手段，而该状态全链路无检测、无约束、无指标

**严重度**：major ｜**来源**：数据安全（DS-07）、生产运维（OPS-10）、需求契合（F-11）

**为什么是问题**
1. **同一文档内的直接矛盾**：§R4 缓解写「`archive_status=2` 必须由 janitor 设置；**禁止手工 `SET body=NULL`**」（理由就是置空但归档缺失 → 数据永久丢失），而 §R8 三选一里选项 a 就是「手工置空最老 N 天正文 + VACUUM」，决策树还把它标为「✅ 最快、零风险 ❌ 正文永久丢失」（同一行自相矛盾），§R8 更「建议 a 或 b」。同一份文档里，最高优先风险（R8 是 /data 2.5~5 天写满）的推荐处置**恰恰是**顶层数据丢失风险（R4）的触发条件。
2. **该状态今天已存在且无人盘点**：6/7/8 月共 **3,421,009 行**正文已被 leakscan 置空，而 062 给 `archive_ref` 的默认值是 NULL → 这些行今天就处于「无正文 + 无指针」。§G4 承认「不可恢复」，但 §F/§K/§N 没有任何针对存量丢失态的检测、盘点或告警，§N1 验收里也没有「永久丢失行数」指标。
3. **无技术防线**：062 DDL 无 CHECK/触发器；janitor 的 `UPDATE` 不校验 `archive_ref`；读侧对 ref 为空的永久丢失态只返回 200 + 空正文且不告警（见 MAJ-09）。
4. **选项 a 的效果描述不准确**：`SET body=NULL` 后文件不缩小、df 不会下降，「立即止涨」只可能在 VACUUM 把死元组回收进 FSM 且新写入确实复用这些页之后才成立（§K3 自己承认「不 VACUUM 则新插入仍扩展文件」）；「每清 1 天约省 21 GiB」把「未来可复用的空间」说成「**已释放的空间**」——在 54 GiB 余量、pg_repack 又需要大额空间的语境下，这会让决策者以为清 2 天就能腾出 42 GiB 真实余量。
- *核验员更正*：评审写「6/7/8 月共 403 万行」不准确——`measurements:57-59` 三个月合计 3,421,009 行；403 万是「总行数 − 有正文行数」（5,295,561 − 1,259,124 = 4,036,437，含 9/1–9/9 的无正文行），不影响结论。

**证据**：`:788`、`:810`、`:814`、`:850`、`:408`、`:505`、`:661`、`:667`；`measurements:53-62`、`:244`；062 DDL `:5-17`；`archive_janitor.go:172-176`；`usage_handler.go:131-133`。

**修复建议**：① 从方案中删除/改写 §R8 选项 a，或为应急场景开一条**受控例外**（明确「仅在无归档能力时、经业务书面确认、按天记录 request_id 范围、事后不可恢复」并给出执行脚本与审计要求），在文档层面消除 R4 与 R8 的矛盾；② 增加数据完整性巡检：定期统计 `body 全空 AND archive_status<>2 AND archive_ref IS NULL` 与 `archive_status=2 AND archive_ref IS NULL` 的行数并按天出图告警，把「永久丢失行数 = 0（存量历史另计并单独立档）」纳入 §N1；③ DB 层加 `CHECK (archive_status <> 2 OR archive_ref IS NOT NULL)` 并在 janitor 的 UPDATE 上加 `AND archive_ref IS NOT NULL` 守卫；④ 把选项 a 的效果改为准确口径（不释放空间，只抑制 df 增长，能抑制多少取决于 FSM 复用率，需在低峰用一天实测）；⑤ 读取端补齐「无正文 + 无指针」分支并告警。

---

### MAJ-09 置空前不校验该天归档是否存在/可读；且「已置空 + `archive_ref` 为空 → 永久丢失 → 告警」这条主分支在实现里不存在

**严重度**：major ｜**来源**：数据安全与不可逆风险

**为什么是问题**：janitor 的置空路径上只有两个检查——全库级 marker 存在 + 行级 `archive_status=1`；**没有任何一处去 Head/Stat 该 dt 的归档文件、index、`_SUCCESS`，也不读 manifest 的 files 列表**，§K3.1 流程图同样没有该步骤。不可逆场景：① 主机重启后 `/datalog` 挂载失败（或 sdc 掉盘后以空目录挂上），归档在进程视角全局不可见，但 marker 若在容器/系统盘路径上仍存在 → 闸门通过 → 该轮把所有过热已归档行全部置空；② 运维误删某天目录（清理脚本、`rsync --delete` 方向写反）→ 该天正文被置空、点读永远 `CodeNotFound`，且没有任何「置空前确认归档可读」的防线；③ 保留策略或磁盘满导致的块文件截断同样无法阻断置空。另：§H2.1 时序图 `else ③「已置空 + archive_ref 为空 → 正文永久丢失 → 告警」`与 §H5 该行在实现里不存在——`usage_handler.go:131-133` 只在 `archive_ref` 非空时点读，ref 为空或 `h.arc == nil` 时直接 200 返回空正文，没有任何告警分支；而 §H1:422-423 又声明 `usage_handler` 与 `router.go` 均「不变」——一边把这两条状态写成「需告警」，一边声明不实现它们。

**证据**：`:467-469`、`:498-505`、`:422-423`、`:676-688`；`internal/observability/handler/usage_handler.go:128-148`；`archive_janitor.go:80-96`、`:142-192`；`archive_service.go`（无归档存在性检查）。

**修复建议**：① 置空前对每个 dt 做「归档可读性前置校验」：`Head` 该天 `_SUCCESS` + index + 每一块文件，并抽样回读解压（可选 sha256），任一失败则整轮跳过该天并告警；② 校验 `ARCHIVE_ROOT` 的挂载状态（对比 `/proc/mounts`，或启动时落一个实例 UUID 探针），挂载异常时 janitor 直接拒绝置空；③ 在 `usage_handler` 补上「PG 无正文 + `archive_ref` 为空」分支并接入告警（按 §H5/§H2 的承诺），纳入 §N1 验收；④ 采纳 MAJ-10 的按天离机凭证后，该凭证同时充当「归档已存在且已离机」的存在性证明。

---

### MAJ-10 `require_offhost_copy` 只是标记文件存在性检查（无 dt 粒度、无 sha、无新鲜度），rsync 静默停摆后硬闸永久失效

**严重度**：major（由 blocker 下调：「副本凭证粒度过粗」而非设计主动丢数据）｜**来源**：数据安全（DS-04）、生产运维（OPS-06）、遗漏（F8）

**为什么是问题**：闸门实现为 `os.Stat(j.cfg.offhostMarker)`（`archive_janitor.go:131-140`）——不判断 marker 的 mtime、不判断 marker 内容、不把它与该 dt 的归档、与该天 rsync 是否成功、与副本 sha256 关联。只要该路径上存在一个任意时刻写下的文件，**此后所有天**（含今天刚归档、rsync 尚未覆盖的天）都可以被置空。方案 §K6 的流程顺序本身也暴露了这个洞：§N1 第 8 步「建立异机 rsync + 写 marker」是一次性动作，§K1 第 6 步同样是一次性；之后每天新产生的归档是否已离机、副本是否完整，闸门无从得知。§N1 第 8 步的验收标准甚至写着「`offhost_copy_marker` 文件存在且**时间戳为部署后**」——即验收环节意识到时间戳，但**运行时关卡没有用时间戳**；§K6 也没说需要每天刷新 marker。K6 全文没有 RPO/RTO、没有 rsync 的时间点/频率/失败处理、没有「rsync 未完成则不置空」的联动；仓库里也没有任何 rsync 脚本或计划任务（`grep -rn rsync deploy/` 无结果，全仓只命中两份 markdown）。旧定稿要求「每月一次恢复演练」，本版 §K6:744 降级为「每季度一次」且未说明理由，演练内容只是随机抽样点读，不覆盖副本是否逐天完整。另：marker 的落盘位置在方案里未定义（§I1:528 默认空字符串），若它与归档同在 `/datalog` 则 `/datalog` 未挂载时闸门 fail-safe；若在容器内其它路径，则 `/datalog` 未挂载反而构成「闸门通过 + 归档目录为空」的组合（与 MAJ-09 叠加）。

- *核验员更正*：「今天新归档的天在副本还没做出来之前就被置空是常态」不准确——按 `hot_window_days=3`、`closure_lag_hours=24` 计算，最早被归档的天约为 T-2，设计上给「每日 rsync」留了约 1 天裕度；只有在 rsync 频率低于每日、rsync 失败，或 `days_per_night`/backlog 改变时序时才形成「先置空后离机」。

**证据**：`:681-682`、`:743-746`、`:878`、`:794`、`:528`；`archive_janitor.go:129-140`；`archive_cron.go:78-86`；`trace-archive-final-design.md:309`、`:354`。

**修复建议**：① marker 改为**按天**的离机凭证——每个 dt 记录 `offhost_verified_at` 与该天归档内容 sha256（写入 manifest 或独立 `offhost/dt=YYYY-MM-DD.json`，含 sha256 与副本路径）；janitor 逐天判定，只有该 dt 的离机凭证存在、sha256 与本地归档一致、且凭证时间晚于该天归档最终写入时间，才允许置空该天；② 副本未覆盖的天一律保持 `archive_status=1`、正文留 PG，并在看板上暴露落后天数；③ 在方案里显式定义 RPO ≤ 1 天、RTO（恢复某天某条 trace 的时间）并给出验收方法；④ 把 rsync 脚本/定时任务与失败告警写进交付物；⑤ 恢复演练频率与旧定稿保持一致（每月）或写明降级理由与补偿措施，且演练必须包含「逐天 sha256 比对 + 随机 rid 全文回灌校验」。

---

### MAJ-11 磁盘配额闸只挡住 archiver、挡不住 janitor：配额超限期间正文继续被置空，与 BLK-01 叠加成丢数据链

**严重度**：major ｜**来源**：数据安全与不可逆风险

**为什么是问题**：§F3 写「绝不删数据：`max_disk_gb` 超限只停归档 + 告警」，§K3.1 流程图也把水位检查画在 janitor 流里（F「磁盘水位 ≥ watermark(80%)?」→ G「告警并停止归档，绝不删数据」）。但实现是反的：磁盘守卫只存在于 `ArchiveService.archiveWaterline`（`archive_service.go:282-295`，超限时 `return nil` 跳过本轮归档），`ArchiveJanitor.run` **完全没有磁盘检查**，而 cron 在 archiver 返回后**无条件**调用 janitor（`archive_cron.go:78-86`）。因此当 `/datalog` 写满或超过 `max_disk_gb` 时：**归档停了，置空照跑**。这与 BLK-01 直接叠加成不可逆链——配额拦截/写满期间，任何处于「行=1、manifest=archiving」的天不会被 archiver 修复（归档被闸住），但 janitor 会把它的正文置空；等运维清出空间后 archiver 恢复，对该天重导时 `pg_row_count` 已为 0/残余 → 校验通过 → 覆盖归档 → 该天正文在两侧同时消失。此外，若按 §K3.1 流程图字面实现（水位 ≥80% 就停止），则当前 `/data` 实测已用 84~85%，而默认 watermark 是 80%——该门若作用于 `/data`，会在最需要置空的时刻永久阻止置空，使 `/data` 继续增长到写满；§K3.1 未说明 F 判断的是 `/data` 还是 `/datalog`，属方案的口径缺失。（核验补充：水位只是告警阈值，只有 `max_disk_gb>0` 才 `blocked`，且仅作用于 archiver。）

**证据**：`:316`、`:684-686`、`:530`；`archive_service.go:282-295`、`:700-745`；`archive_janitor.go:71-97`；`archive_cron.go:78-86`；`measurements:43-44`。

**修复建议**：① 明确配额闸的作用域：`max_disk_gb`/水位应作用于归档盘，且「停止写入」与「停止置空」**必须一致**——建议在 janitor 中同样读取同一守卫，配额超限时**同时**停止置空（保持行=1、正文留 PG），并在告警中说明 `/data` 会继续增长，交由人工决策；② 或在 janitor 中明确区分：只有「该 dt 归档已验证 + 已离机 + 可读」才允许在配额超限期间置空，其余一律拒绝；③ §K3.1 的 F 补充判断目标盘（`/datalog` vs `/data`）与阈值来源，避免与 `/data` 84% 的现状冲突。

---

### MAJ-12 §N2 把 `ARCHIVE_ENABLED=false` 当作安全回滚，但点读器也挂在该开关下：关掉后所有已置空 trace 静默返回空正文

**严重度**：major ｜**来源**：数据安全与不可逆风险

**为什么是问题**：§N2 回滚表第一行是「归档服务异常 → `ARCHIVE_ENABLED=false`（cron no-op），PG 正文不受影响」，第三行是「需要恢复已置空正文 → 从 `/datalog` 归档按 request_id 批量回填」。但**点读器也挂在同一个开关下**：`router.go:47-50` 明确 `if cfg.Archive.Enabled { archiveReader = archive.NewLocalStore(...) }`，而 `usage_handler` 的两段式回读要求 `h.arc != nil`（`usage_handler.go:131`）。因此一旦把开关关掉（无论因归档异常回滚，还是 §K1 第 2-3 步的 `ARCHIVE_ENABLED=false` 观察期）：① 已置空的历史 trace **全部读不到正文**（返回 200 + 空 body），前端只显示空正文；② 这个状态与 §H5 的「正文永久丢失」在外部表现上**完全一致**，且不产生任何告警（`ReadBody` 根本不会被调用）；③ 运维在回滚期间会认为「归档正文已丢失」，而实际上文件都在——正是 §R4 想避免的误判方向，却由方案自己的开关导致。§H1:422-423 声明 `usage_handler` 与 `router.go`「不变」，等于把该陷阱固化进 v2。此外，把归档开关与点读开关耦合，也让 §K1「先验证归档、再允许置空」失去一个有用的中间态（只读归档但停止写入）。

**证据**：`:887`、`:889`、`:422-423`、`:612-614`；`router.go:45-50`、`:832`、`:841`；`usage_handler.go:105-107`、`:128-133`。

**修复建议**：① 拆分开关：`archive.write.enabled`（cron 写入）与 `archive.read.enabled`（点读）分离；回滚只关写入，点读保持可用；② 或让点读器独立于 `Enabled` 构造（只要 `archive.root` 存在即可用；root 不可读时返回可观测的错误码并告警）；③ 在 `usage_handler` 中补「body 为空 + `archive_ref` 为空 + 点读器为 nil」的区分与告警，避免把「读路径被关掉」误报成数据丢失；④ §N2 回滚表明确写出点读器不受该开关影响的实现要求，并纳入 §N1 验收。

---

### MAJ-13 K1 步骤 1 的迁移路径不可执行：062/063 从未提交，且相对 HEAD 已是乱序文件；migrate-prod 用默认 linear 顺序会失败并卡住发布

**严重度**：major（由 blocker 下调：后果是发布被挡与返工，非数据丢失；且手工 psql 执行 up.sql 仍可行）｜**来源**：生产可行性与运维

**为什么是问题**
- **062/063 根本不在 git 里**：`git status --porcelain atlas-migrations/` 显示 `20260824093000_trace_archive.{up,down}.sql`、`20260824093500_usage_settlement_daily.{up,down}.sql`（以及另外几个）都是 `??`（未跟踪），`atlas.sum` 是 ` M`。CI 迁移任务在 CI 检出树上执行，**只要不提交，管线永远不会应用 062/063**。
- **一旦提交，它会成为乱序文件**：HEAD（tracked）最大版本是 `20260902072000_add_report_snapshot_anomaly_review_offhours_filing.sql`（`git ls-files atlas-migrations | wc -l` = 50），而 062 的版本号 `20260824093000` **小于**它。Atlas 默认执行顺序 `linear`「expects a linear history and fails if it encounters files that were added out of order」，即报错中止。其报错条件是「库在版本 Z、目录里有版本 Y<Z 的待应用文件」，`ATLAS_BASELINE` 当前值远小于 062，故 062 属待应用而非被 baseline 跳过。
- **两条迁移任务的参数不一致**：`migrate-test` 显式带了 `--exec-order non-linear`（`.gitlab-ci.yml:322-323`），`migrate-prod` **没有**（`:286-287`）——典型「测试能过、生产必挂」。
- **后果会放大**：`deploy-prod` 的 `needs` 含 `migrate-prod`（`:586`），迁移任务失败 → 生产蓝绿发布被阻塞。所以 §K1 步骤 1「执行迁移 062/063」不是一句手工操作，而是需要先修好迁移目录顺序，否则手工执行 `atlas migrate apply` 也会以同一错误失败。
- 仓库另有独立实测旁证：对仓库原目录跑 `atlas migrate status` 得 PENDING 并点名该类文件「were added out of order」，且在全新库上 apply 因 up/down 成对（仓库未配 `format`，`.down.sql` 与 `.up.sql` 被当同版本两条迁移、字典序 down 在前）直接 exit=1。
- *核验员更正*：是否报错取决于目标库当前版本 Z，生产 Z 在只读评审下无法确证。

**证据**：`git ls-files atlas-migrations | wc -l` = 50；`git status --porcelain atlas-migrations/`；`git ls-files atlas-migrations | sed 's#.*/##' | sort | tail -3`（最大 `20260902072000_...`）；`.gitlab-ci.yml:286-287`、`:322-323`、`:586`；Atlas 文档 Exec-Order（`linear` 默认、乱序即失败）；方案 `:611`、`:600-601`、`:905`。

**修复建议**：① 在 K1 步骤 1 之前增加「迁移目录治理」前置步骤：把 062/063（以及 `20260902070000`/`20260902071500` 等乱序文件）的版本号改成晚于当前最大已提交版本，或 `atlas migrate diff` 重新生成，并同步提交 `atlas.sum`；建议同时把 062/063 改成单文件（内容注释保留回滚语句）；② 给 `migrate-prod` 补上 `--exec-order non-linear`（或统一按 linear 治理），使 prod 与 test 参数一致；③ K1 步骤 1 改成「提交迁移 → 打 tag → 在 GitLab 手动触发 migrate-prod」这一条与 CI 一致的路径；④ 增加验收：迁移前先跑 `atlas migrate status` 输出当前版本与待应用列表，把输出贴进变更单。

---

### MAJ-14 「生产尚无任何归档产物」「189 GB 是 leakscan 证据」的归因未经核实，而 §K5 对这 189 GB 给出「可迁出或删除」的建议

**严重度**：major ｜**来源**：与现有代码库的一致性（C2）、算术一致性（A-12）

**为什么是问题**
- **归因在代码库里没有任何支撑**：仓库内没有任何 leakscan 工具或脚本（排除 `.git`/`vendor`/`node_modules` 后全仓 grep 只命中 `trace-archive-p0-measurements.md` 与 `trace-archive-solution-v2.md` 两份文档），`p0:321-325` 的「原始产物位置」也没有针对该目录内容的检验记录；`p0:63`「仅命中行」是推断而非实测。
- **算术强烈指向「它是正文归档」**：6/7/8 月行数 319,979 + 1,076,914 + 2,024,116 = **3,421,009 行** × 205,879 B/行 ≈ **704 GB** 原文，按 gzip -6 实测 3.47x ≈ **203 GB**，与磁盘实测 189 GB 相差约 7%；反过来，若真如 p0 所述「仅命中 8.47 万条」，要凑出 189 GB 需每条命中原文约 7.7 MB（压缩后 2.2 MB），而全库有正文行的均值是 226,700 B/行，差 1~2 个数量级。另有命名/写出口径证据：现有归档写出口径就是 `body-{date}.jsonl.gz`（`archive_service.go:401`）。
- **若成立，后果严重**：§E5「生产尚无任何归档产物……无历史包袱」为假，§A9/§K5「非本设计产物/可评估释放」为假；而它是这些正文的唯一副本（方案自己把 cleaned 态定义为「不可逆、唯一副本」），按 K5「可迁出或删除」执行会造成不可恢复的数据丢失。
- **U6 的核实方式无效**：`solution-v2:903`「安全/合规确认处置是否结案」无法区分「命中行」与「6/7/8 月全量正文」，若无脑照此确认即可能把唯一副本按证据归档迁走/删除。
- *核验员更正*：① `205,879 B/行` 在 `p0:204` 与 `solution:64`，不在 `p0:64`；② 「恰好是 §E1 布局的上一级目录」论证偏弱——§I1:516 定义 `ARCHIVE_ROOT=/datalog/llm-gateway-archive`，旧稿与 `readbody.go` 的两层布局都要求 `dt=YYYY-MM-DD` 层级，观测路径不存在该层级（除非 `p0:51` 的 glob 是深层路径简写）；③ **blocker 定性不成立**——§K5:739 明确「若泄漏处置已结案…需业务/安全确认，不在本方案范围内」，删除/迁出是并列选项且以确认为前提，容量测算也不依赖释放（§L:727 用 368 GiB 作基线）。真问题是「未经核实的归因被当作事实写进 A9/E5/K5，且 U6 提供的核实手段无法排除危险情形」。

**证据**：`:265`、`:736`、`:21`、`:211`、`:401`、`:903`；`measurements:51`、`:63`、`:57-60`、`:204`、`:89`、`:72`、`:321-325`。

**修复建议**：在 §K5 之前加一道**强制核实**并写进结论：① 统计该目录文件覆盖的日期范围与总行数（`ls` + 抽样 `gzip -dc ... | wc -l`）；② 抽样确认行结构是否为 `archiveRecord`（request_id/request_body/response_body）；③ 与 PG 中 6/7/8 月已置空行数对账。核实前把 §E5/§A9/§K5 的措辞从「尚无归档产物/非本设计产物/可释放」降级为「**来源待核实，禁止删除**」；若核实为归档正文，则 §E5 必须整段改写，并把该目录纳入 §K6 的离机副本与恢复演练范围。

---

### MAJ-15 K1 步骤 0 / N1 第 1 步的「`/datalog` ≥ 500 GiB 可用」与 §K4 实测 368 GiB（及 U6）互锁

**严重度**：major ｜**来源**：算术一致性（A-14）、需求契合（F-09）

**为什么是问题**：K1 第 0 步与 N1 第 1 步把「`/datalog` 可用 ≥ 500 GiB」列为**硬前置与验收标准**，但实测可用为 368 GiB；§K4/K5 的保留期分析全部按 368 GiB 展开，并指出只有释放 leakscan 遗留（+189 GB ⇒ 544~557 GiB）才可能达到 500 GiB，而该释放尚需业务/安全确认（U6）。因此该前置**在当前磁盘状态下不可满足**：不释放遗留 → 前置永远不通过；释放遗留 → 决策不在本方案范围内。批准时无法判断前置条件是否可满足，实施者会在第 0 步被卡住或误以为需要扩容。500 GiB 还是旧定稿的遗留口径。

**证据**：`:610`、`:871`、`:723-728`、`:739`、`:903`、`:102`；`measurements:44`；`trace-archive-final-design.md:354`。

**修复建议**：把前置改为条件式——「≥ 368 GiB（现状）；若要求 ≥ 500 GiB，须先释放 leakscan 遗留（见 U6/K5）」，并给出两分支下的 retention 上限；§N1 验收标准采用同一条件；把 189 GB 的释放决策并入 §O 的批准清单，而不是留在「不在本方案范围内」。

---

## 4. Minor（29 条）

> 每条均给出：为什么是问题（含触发）/ 证据 / 修复 / 来源。

**MIN-01 §H2 六步未定义二分边界分支，「块内未命中」与「定位错块」不可区分**（minor｜技术正确）
- 为什么：§H2 步骤 4「定位 `last_rid >= requestID` 的第一个块」+ 步骤 6「块内未命中 → `CodeNotFound`」漏掉三种情况：(a) `requestID < 首块.first_rid`；(b) `requestID >` 所有块的 `last_rid`——**不存在**满足条件的块，二分实现极易越界或返回 nil，方案完全没定义该分支；(c) index 为空/块文件缺失。更重要的是语义：把「块内未命中」直接判为 `CodeNotFound`，会把「索引/排序/定位错误」与「记录真的不存在」混为一谈，而 §H5 对该返回的处置是「数据异常，需告警、且对外表现为正文丢失」——与 MAJ-02 叠加时会把可修复的定位 bug 报成数据丢失。
- 证据：`:431`、`:432-433`、`:500-505`、`:242`；`internal/archive/readbody.go:87`。
- 修复：显式定义——无候选块 / rid 超出 index 覆盖区间 ⇒ `CodeNotFound` 并打 `out_of_range` 指标；块内未命中 ⇒ 再扫相邻块（±1）后判定，若在相邻块命中则记 `index_mismatch` 告警；把 `[首块.first_rid, 末块.last_rid]` 快路径写进方案。
- 来源：T04

**MIN-02 §H3 延迟预算漏算块内 JSON 全扫成本，并把 CLI 解压吞吐当作 Go 解码器吞吐**（minor｜技术正确 + 需求契合）
- 为什么：§H3 把「JSON 扫描命中」估为 ~10 ms、合计 ~0.3 s；但块是 256 MiB 未压缩 JSONL（≈1,300 行 × ~205,879 B/行），§H2 步骤 5 实现为 `json.Unmarshal` 逐行比对，单次点读至少两遍 256 MiB 扫描加数百 MB 拷贝，量级是**数百毫秒到 1 s**，往往比解压更贵，且是纯 CPU。10 ms 意味着 >10 GB/s 的解析吞吐，无依据。同时 1047 MB/s 是 CLI `zstd -d` 的实测，Go 解码器在 128 MB 长窗口下无实测。§N1 第 7 步验收却是「P95 < 1 s」。
- 证据：`:477-483`、`:877`、`:901`；`measurements:184`、`:204`；`internal/archive/readbody.go:69-79`。
- 修复：利用 `archiveTraceLine` 字段顺序固定（request_id 是行首字段）做零解析比较（`bytes.HasPrefix(line, []byte(`{"request_id":"`+rid+`"`))` 命中后再 Unmarshal）；或把每块行级 rid/偏移写进 index 旁文件。并补一次 Go 解码器 + 块内全扫的真实基准，据此校准 §H3 与 §N1 验收值。
- 来源：T06、F-15

**MIN-03 §E4 读侧 `WithDecoderMaxWindow(128<<20)` 的理由写反，且写侧窗口可配、读侧硬编码**（minor｜技术正确）
- 为什么：库默认 `maxWindowSize = MaxWindowSize = 1<<29`（512 MiB），显式设成 128 MiB 是把上限**收紧**；而 §I1 把 `archive.zstd_window_mb` 列为可配项，一旦调到 >128，读侧立刻 `ErrWindowSizeExceeded`（`decoder.go:357`），归档整体不可读。默认 128 与写侧相等是安全的：流式 writer 的帧头 `WindowSize` 恰为 128 MiB，`>` 严格判定通过（核验证实）。真正合理的理由应写成「限制解码窗口占用的内存」。
- 证据：`:256-261`、`:524`；`measurements:139-141`；`klauspost/compress@v1.20.0/zstd/decoder_options.go:31-37`、`framedec.go:41-43`、`decoder.go:357`；`frameenc.go:68-69`、`decodeheader.go:150-153`。
- 修复：读侧窗口从同一份配置读取（或干脆不设，采用 512 MiB 默认，仅用 maxMemory 控内存）；注释改为「限制解码窗口内存」；补一个「写侧窗口 > 读侧上限 ⇒ 解码失败」的负向单测。
- 来源：T07

**MIN-04 分层与改造量被低估：读路径不走 `ArchiveStore` 接口，写路径根本不用 store**（minor｜技术正确）
- 为什么：§A:23 把「`ArchiveStore` 接口隔离」列为仍然有效，§D2:165 称「未来换 S3 只改 store 实现，状态机零改动」，§H1:423 称 `router.go`「不变」，§G1:328 只让 `local.go` 加一个可选 `PutPart`。与现状不符：(1) `ArchiveStore` 接口（`store.go:144-157`）**没有 `ReadBody`**；(2) `ReadBody` 是 `*LocalStore` 的方法（`readbody.go:39`），handler 与 router 直接持有具体类型 `*archive.LocalStore`（`usage_handler.go:87/105`、`router.go:47-49`）；(3) 写路径完全绕过 store，直接 `os.MkdirAll/os.OpenFile/os.Rename/gzip` 写盘（`archive_service.go:395-463`）。所以「换 S3 只改 store 实现」不成立；分块读还把窗口/块大小传进读侧，而 `LocalStore` 只有 root/stagingDir 两个字段、`NewLocalStore` 无该入参（`local.go:25-38`），`router.go:47-49` 的构造签名也得改。
- 证据：`:23`、`:165`、`:419-423`、`:328`；`internal/archive/store.go:144-157`；`readbody.go:39`；`usage_handler.go:87,105`；`router.go:47-49`；`archive_service.go:395-463`。
- 修复：要么把 `ReadBody`（含分块语义所需参数）提升到接口并给 `LocalStore` 增加配置入参，要么在方案中如实写明「读侧仍绑定 `*LocalStore`，换存储需同步改造读路径与装配」，并把 §H1 改动表补全（`router.go`/`usage_handler.go`/接口）；建议写路径改为经 store 落盘（`PutPart`），避免 tmp/fsync/rename/sha256 两处重复实现。
- 来源：T08

**MIN-05 §I4/§H4 内存预算无实测出处，且漏计 2×窗口编码缓冲、导出批次常驻、每块暂存**（minor｜技术正确 + 生产运维 + 需求契合）
- 为什么：§H4:489 写「（实测约 150~250 MB RSS）」，而 `measurements` 全文 grep RSS/内存/memory **无命中**；§I4 的三行也无出处（把估算写成「实测」）。漏项至少三项：① `WithWindowSize(128<<20)` 会把 `maxMatchOff` 设为 128 MiB，`ensureHist` 在 `lowMem=false`（库默认）时按 `maxMatchOff + maxMatchOff` 分配 → **hist cap ≈ 256 MiB**（与块大小无关）；② 导出路径 `fetchBatch` 用 `LIMIT batch_rows` 一次性把整批行的正文扫进切片，生产 `batch_rows=3000` ⇒ **单批 ≈ 570 MB 常驻**（188,534 B/行 × 3000）；③ §G2 步骤 3 的「累积到 ChunkMB 时封块…释放缓冲」意味着每块 256 MiB 未压缩内容在内存暂存。结论方向（2g 过紧、提到 4g）没错，但把估算写成实测、且漏项，会让 K1 第 0 步的决策缺可核验依据。
- 证据：`:559-571`、`:489`、`:337-340`、`:519`（`batch_rows` 写 1000，实际 3000）；`config.go:136`、`config/prod.yaml:68`；`measurements`（无 RSS）；`klauspost/compress@v1.20.0/zstd/enc_base.go:82-91`、`encoder_options.go:45,104-110`；`archive_service.go:519-538`。
- 修复：把「实测」改为「估算（未测）」，补一次真实测量（编码 1 块 / 点读 1 次 / 4 并发点读 / 导出批次，记录 RSS 峰值）；估算中体现 hist cap ≈ 2×window；把 `batch_rows` 降到 100~200 或改服务端游标流式读取，并与 §I1 的默认值对齐；补全「进程基线 + 编码 + 导出批次 + 解码」的推导表，把「容器 2g → 4g」写成 K1 步骤 0 的硬验收（`docker inspect` 校验）。
- 来源：T09、OPS-08、F-07

**MIN-06 现状正文存在 236 GiB 与 265.84 GiB 两个口径；「现存仅 11 天」与行数/速率不相容（实为 12.44 天）；回填量偏低**（minor｜算术一致性）
- 为什么：§C1/§L1/§C5 的现存正文基线是实测 TOAST = 265.84 GiB（1,259,124 × 226,700 B/行，闭式成立）；但 §L2 现状行与 §G4/§D3 又写「11 天 ≈ 236 GiB」（11 × 21.4 GiB/天）。同一时点的「现状 PG 正文」不可能同时是 236 和 265.84 GiB。根因是日期窗口写错：1,259,124 ÷ 101,195 = **12.44 天**，而 §C1 自己标注的区间 2026-09-10 ~ 09-21 含首尾恰为 12 天；若窗口真的只有 11 天，则日均必须是 114,466 行/天（与 101,195 差 13%）。连带：§G4/§K2 的回填总量「11 天 × 20.8 GB ≈ 230 GB jsonl」偏低——按有正文行数实算应为 1,259,124 × 205,879 B = **259.2 GB**（241.4 GiB），约高 13%。另 §L2 表头声明该列为「PG 正文 + 非 TOAST ~22 GiB」，则 11 天行应为 236+22 = 258 GiB（75%），而表中填的是实测 df 值 288 G（85%）——同表其余行都遵守 +22 公式，只有现状行不遵守。
- 证据：`:60`、`:62`、`:756`、`:765`、`:409`、`:655`；`measurements:223`、`:211`。
- 修复：现状正文统一为 265.84 GiB（1,259,124 行），窗口改为约 12.4 天（或明确写 2026-09-10~09-21 共 12 天）；§L2 现状行按公式写；§G4/§K2 回填量改为 ~259 GB（~241 GiB）。若坚持 11 天/236 GiB，须同步把日均行数改成 ~11.4 万行/天并重算 21.4 GiB/天。
- 来源：A-02

**MIN-07 「82 块」与「245 ms」都是把 256 MiB 当 256 MB 算出来的（同一 4% 口径混用）**（minor｜算术一致性）
- 为什么：§E1：一天 20.8 GB（十进制，由 101,195 × 205,879 = 20.83e9 B 得来）÷ 256 MiB（268,435,456 B）= 77.6 ⇒ 应为 **78 块**；只有把块大小按 256 MB（256e6）解释才得 81.4 ≈ 82 块。§H3：256 MiB ÷ 1047 MB/s = 256 ms，文档写 ~245 ms，而 245 ms 恰好来自 256 MB ÷ 1047 MB/s = 244.5 ms。同节「块压缩后约 15 MB @ 19x」也与 19x 不符（256 MiB/19.61 = 13.1 MiB ≈ 13.7 MB；15 MB 对应 17.2x）。影响有限（块数只影响 index 行数与 U4 定标），但会误导块大小定标与 index 体积估算。
- 证据：`:221`、`:480`、`:482`、`:521`、`:212`。
- 修复：明确 `chunk_mb` 的字节定义（建议 256 × 2^20），据此把块数改为 ~78 块、解压耗时改为 ~256 ms、压缩后块大小改为 ~13.5 MB @ 19.6x；或全文统一改 MB。
- 来源：A-05

**MIN-08 证据文档的行数速率标注错误：「月增（3.04 万行/天 × 30）」推不出 59 GiB**（minor｜算术一致性 + 容量保留）
- 为什么：按字面 30,400 行/天 × 30 = 912,000 行 × 21,513 B = 19.6 GB = 18.3 GiB/月，与 59 GiB 相差 3.2 倍。59 GiB 反推需 3,035,850 行/月，即 **101,195 行/天 × 30**，故括号里的「3.04 万行/天」应是「3.04 百万行/月」或「10.1 万行/天 × 30」的误写；而 v2 §K4 只给 59/34 GiB 未写行数速率，导致该数字在 v2 中无法溯源。同一份证据文档还有两处：§1 表把「月增」行写成「~70 GB → 21~23 GiB/天」（月度对每日，量级差 ~10 倍）；§0 结论写「14~26 GiB」而 §7 与 v2 §L1 写「14~25 GiB」（实算 25.23 GiB，25 正确）。这是容量模型里**唯一会把复算引偏约 3.3 倍**的数字。
- 证据：`measurements:212`、`:35`、`:205-206`、`:36`、`:18`；`:720`。
- 修复：把该行改为「月增（101,195 行/天 × 30 ≈ 304 万行/月）：保守 59 GiB / 现实 34 GiB」并注明口径是「行生成速率均值」；把 §1 的「月增」列拆为「月增 21~23 GiB/天（≈650~690 GiB/月）」或改为「日增」；§0 的 14~26 统一为 14~25 GiB。
- 来源：A-11、CAP-07

**MIN-09 §I1「配置项完整表」与代码多处不符，其中 `disk_watermark` 的 yaml 键名写错会导致配置静默失效**（minor｜代码一致性 + 算术一致性 + 遗漏 + 需求契合）
- 为什么：逐条核对——① `archive.disk_watermark_percent`（yaml 列）**不是代码实际读取的键**：代码读 `archive.disk_watermark_mb`（`config.go:147` SetDefault、`:235` GetInt；`prod.yaml:75`、`dev.yaml`、`test.yaml` 同），注释还专门说明「键名沿用历史名 disk_watermark_mb，值语义为百分比」。运维照 §I1 把 `disk_watermark_percent` 写进 yaml 会被 Viper **静默忽略**、落回默认 80，而 §N1 第 11 步的水位告警验收正依赖该阈值（同一错误键名在 §R3:787 再次出现）；② `archive.batch_rows` 的「旧」值写 1000，实际代码默认与 `prod.yaml` 都是 **3000**（`config.go:136`，`archive_service_test.go:81` 直接断言 3000），连它自称的来源旧定稿也写 3000；③ `archive.cron_schedule` 旧值写「—」，实际默认 `0 2 * * *`（`config.go:140`）；④ `archive.closure_lag_hours` 的 env 列写「—」，但 `SetEnvKeyReplacer("."→"_") + AutomaticEnv()` 使 `ARCHIVE_CLOSURE_LAG_HOURS` 实际可注入；⑤ 漏列三个仍在生效的既存键 `archive.max_retry`(5)、`archive.staging_retention_hours`(336)、`archive.backfill_once`(false)——而 §K2 恰恰依赖 `backfill_once`（见 MAJ-05）；⑥ §G1:330 的 `config.go` 改动项只列 3 个新字段，而 §I1 新增键共 6 个。
- 证据：`:529`、`:519`、`:532`、`:526`、`:330`、`:513-532`、`:787`；`config.go:136-148`、`:221-237`；`config/prod.yaml:66,68,75`；`.env.example:81-83`；`archive_service_test.go:81`。
- 修复：以 `config.go` 的 viper key 为准逐条校对 §I1 的键名/旧值/新增标记；yaml 键改为 `archive.disk_watermark_mb` 并注明「键名沿用 mb，值是百分比」；补齐缺失键；§G1 的改动项扩到全部新增键；若确实要改名，必须同时给出代码改动与兼容读取说明。
- 来源：C5、C4、A-15、T11、F-14、F-13

**MIN-10 压缩率口径混用（三种实现/输入规模），且分块后未在 256 MiB 粒度验证**（minor｜容量保留 + 算术一致性 + 技术正确）
- 为什么：§D3:198 把「zstd Best + 128MB 窗口 实测 9.57~19.61x」当成同一配置的区间，但下界是 Go/35.5 MB、上界是 CLI `-22 --ultra`/440 MiB，两个端点既不同实现也不同语料（§C3:81 自己在 142 MB 上实测 Go 37.90x，反而高于该区间上界），却用来生成 §L1 的「14~25 GiB（缩减 10~19 倍）」。§C3 同段还混用了 35.5 MB 的 8.46x 与 142 MB 的 8.81x。更关键的是：`measurements:101-102` 的推断句「压缩率随文件增大而提升…真实全天文件应 **≥ 19.61x**」与同文 §4.2 实测**方向相反**（142 MB 上 CLI 43.41x、Go 37.90x，比 440 MiB 的 19.61x 高一倍以上），真实原因更可能是样本内容分布不同；而 v2 把流拆成 **256 MiB 独立帧**（每块重置窗口，且 256 MiB < 440 MiB），「用 440 MiB 单帧的 19.61x 作为每块下界」在 v2 的分块现实中根本没有被验证——§U4 只把块大小当作点读延迟/并行度定标项，未把压缩率列为定标项。另 142 MB 输入的产物在附录中查不到（`measurements:323` 只有 440 MiB 与 35.5 MB），影响 §4.2 窗口实验的可复现性（但 **88% 系数另有独立来源**：同一 35.5 MB 文件上 Go 9.57x vs CLI 10.90x = 0.878，见 `:121-123`，故该系数不依赖 142 MB 测量）。
- 证据：`:198`、`:80-84`、`:72`、`:66`、`:899`、`:901`、`:212`、`:249-253`；`measurements:96,101-102,121-135,145-146,206,323`。
- 修复：在 §C2/§C3 每个比率后强制标注「实现 + 输入规模」；把 §D3/§L1 的区间改为单一同语料口径（统一按 35.5 MB 的 9.57x 作保守下界，注明上界 19.61x 来自 CLI/440 MiB），或在 U2 闭环前只给保守值 9.57x；把 U4 扩展为「用生产参数压一个 256 MiB 块 + 压一整天（单帧）做对比」，取较低者为预算基准；在附件产物清单里补上 142 MB 输入文件（或标注该测量无法复现）。注意：正因为「越大越好」未被证实，v2 容量表「现实」列的 17.2x（=0.878×19.61）**并非下界而是乐观值**。
- 来源：CAP-05、T12、A-10

**MIN-11 容量模型是「单日 2% 抽样 × 单点比率 × 线性行数」外推，无跨日/峰值/增长余量**（minor｜容量保留）
- 为什么：① 压缩/字节口径来自 2026-09-19 单日 2,380~2,355 行（约 2%）的抽样（`SAMPLE_FRAC=0.02`），却外推到 11 天存量与未来 12 个月，无跨日方差数据；② 峰值余量：同日文档自己给出「近 12 日均值 101,195 行/天（**峰值 182,439**）」，即 1.8 倍，而 `/data` 增长区间 11~23 GiB/天也有 2.1 倍跨度，月增只用了 101,195×30、无 P90/峰值项；③ 增长余量：`/datalog` 上的归档在 retention 触发前**只增不减**，任何流量增长都线性缩短保留期（按 50%/年增长，保守口径 59 → ~89 GiB/月，365 天需求 >1 TB）；④ 热窗口也只用了均值：§L2 的「3 天 → 64 GiB」= 3×21.4 GiB，峰值日的 3 天会是 ~115 GiB。
- 证据：`measurements:35,60,73,212,239-241,325`；`:754-759,761-769,221`。
- 修复：把 §L1/§L2/§K4 的容量表改成三档（P50 均值 / P90 / 峰值日），并显式加「年增长系数」列；在 §U5 的保留期决策表里以联合区间给出保守值，而不是 365 天这样的单值；在实测脚本里补一项跨日采样（如 09-10/09-15/09-19 各取 2%，报告比值方差）。
- 来源：CAP-06

**MIN-12 §I2 漏列 compose 的 `ARCHIVE_ENABLED: ${ARCHIVE_ENABLED:-true}`，使 K1 第 2/3 步与 N1 第 5 条验收无法按文档达成**（minor｜容量保留 + 生产运维 + 代码一致性）
- 为什么：§I2:538 说「已挂 `/datalog` 并注入 `ARCHIVE_ENABLED` / `ARCHIVE_ROOT`，本版需追加 5 项 env」，但漏了关键事实：compose 里 `ARCHIVE_ENABLED` 的默认值是 **true**（`${ARCHIVE_ENABLED:-true}`，blue/green 皆同），而 §I1 写 `archive.enabled`「默认 false」（该说法只对代码默认成立，对容器不成立）；env 优先级高于 yaml（`config/prod.yaml:62` 的 `enabled: false` 会被覆盖）。因此只要按现有 compose 部署，归档会立即以 `enabled=true` 启动，**§K1 第 2 步「保持 ARCHIVE_ENABLED=false」与第 3 步「验证 cron 必须 no-op」、§N1 第 5 条「cron 日志显示 no-op、`/datalog` 无新文件」都做不了、也不成立**，打乱两段式上线节奏。同文件 `memory: 2g` 需按 §I4/K1 提到 4g，也未进 §I2 清单（虽已是 N1 第 1 步的显式项）。核验补充：该 compose 两行本身是本次工作的未提交改动（`git diff` 显示为新增）。后果有限（hot_window_days 仍为 30，且 offhost 空 marker 会使置空整体跳过，不会丢正文），属流程缺陷、改一行即可。
- 证据：`:538-547`、`:515`、`:612-614`、`:875`、`:790`；`deploy/blue-green/docker-compose.blue.yml:96-97,108`；`docker-compose.green.yml:96-97`；`config/prod.yaml:62`。
- 修复：① §I2 的追加清单加入 `ARCHIVE_ENABLED: ${ARCHIVE_ENABLED:-false}`（或把 compose 默认值改为 false、开启成为显式动作）；② 部署清单第 0 步增加状态基线核对：`docker inspect ... | grep ARCHIVE_`，确认线上当前是 true 还是 false，再决定 §J1/§R6 的措辞；③ §K1 每步写清具体的 compose/env 覆盖命令。
- 来源：CAP-08、OPS-01、C6

**MIN-13 新增依赖只写 `go get`，未包含 `go mod vendor`；仓库已启用 vendor 且 CI/Makefile 用 `-mod=vendor`**（minor；由 major 下调：一条幂等命令，首次 CI 即暴露）｜代码一致性
- 为什么：§I3 把依赖变更写成一个命令 `go get github.com/klauspost/compress@v1.20.0`。但本仓库把 `vendor/` 入库（`git ls-files vendor | wc -l` = 4923），CI 与 Makefile 显式使用 `-mod=vendor`（`.gitlab-ci.yml:99/196/479-480`、`Makefile:21`），而 `vendor/github.com/klauspost/` 下只有 `cpuid`、`vendor/modules.txt` 无 `compress`。Go ≥1.14 在存在 `vendor/modules.txt` 时默认走 vendor 模式，因此只执行 `go get` 而不 `go mod vendor`，`readbody.go` import zstd 会在 CI 与本地直接报 **inconsistent vendoring / 找不到包**而构建失败。（版本可用性无问题：`$GOMODCACHE/github.com/klauspost/compress@v1.20.0` 存在。）
- 证据：`:551-555`、`:873`；`.gitlab-ci.yml:99`；`Makefile:21`；`vendor/modules.txt:354-356`；`go.mod:94`。
- 修复：§I3 改为完整流程 `go get ... && go mod tidy && go mod vendor`，并把 `go.mod`/`go.sum`/`vendor/` 一并提交；§N1 第 3 步验收补一条「`go build -mod=vendor ./...` 通过」。
- 来源：C1

**MIN-14 §H1 把 `usage_handler.go` / `router.go` 标为「不变」，但 §H4/§H2.1/§I1 要求在点读链路上新增 `max_concurrent_reads` 信号量**（minor；由 major 下调：该工作项已在三处显式列出，§H1 只是局部自相矛盾）｜代码一致性 + 需求契合
- 为什么：§H1 的「涉及文件」表把 `internal/observability/handler/usage_handler.go`（「两段式读（:127-146）」）与 `internal/controlplane/router/router.go`（「:47-50 构造、:830/:1073 注入」）都标为「**不变**」；但 §H4:495-496 明确写「必须加**信号量**限制并发……（当前 `usage_handler` 无此限制，属**新增需求**）」，§H2.1:455 的时序图把 `Acquire/Release` 画在 `participant H as usage_handler.Trace` 上，§I1:525 又新增了 `archive.max_concurrent_reads`。从代码看，点读器实例是在 `router.go:49` 用 `archive.NewLocalStore(cfg.Archive.Root, ...)` 构造的，`Config.Archive` 根本没进入读路径——要么改 `router.go` 把 `max_concurrent_reads` 注入 `LocalStore`/包装器，要么改 `usage_handler.Trace` 在调用 `ReadBody` 前后加闸，**这两行不可能「不变」**（唯一能让两行都不变的做法是把信号量硬编码在 `readbody.go`，那样 §I1 的配置键就成了摆设）。这直接影响 §I4/§R2 的内存保护能否落地。此外 §H5 的「归档未命中 → 404 + 告警」中，**HTTP 404 属新增**（`readbody.go:87` 返回的是 store 层 `CodeNotFound`；`usage_handler.go:131-148` 对任何错误只 `slog.Error` 后照常返回 200 + 元数据）。
- 证据：`:421-423`、`:489-496`、`:455`、`:525`、`:466`、`:504`；`router.go:47-50`、`:830`、`:1073`；`usage_handler.go:129-149`。
- 修复：把 §H1 两行改为「新增」并写明落点（推荐 `router.go` 注入 `cfg.Archive.MaxConcurrentReads`，`usage_handler.Trace` 在冷路径用信号量包住 `ReadBody`，超时/超限返回 503 + Retry-After）；§H5 的未命中语义表补上 404/429/503 各行并映射到具体代码位置（现状 vs 新增）。
- 来源：C3、F-05

**MIN-15 改造清单遗漏全部测试文件与前端/配置文件载体**（minor｜代码一致性）
- 为什么：§G1/§H1 的「涉及文件」表只列 5 个 Go 文件 + 2 个 compose，但实际必须同步改的载体还有：① **单元测试**——`internal/archive/archive_test.go` 全程用 `compress/gzip` 造数据并测 `ReadBody` 的 gzip 单文件路径（改成 zstd 分块后必失败，`:312/:362` 写 `body-2026-06-04.jsonl.gz`）；`archive_service_test.go:87` 直接断言 `got.gzipLevel != 6`（字段将被删除，**编译都过不去**）与 `:81` 的 `batchRows != 3000`；`archive_cron_test.go` 亦在包内。② **前端**——`web/src/components/usage/UsageLogSection.vue:131-137` 的 `fetchTrace()` 是 trace 详情页唯一拉取入口，全仓在 `web/src` 下 grep `archive_ref|archiveRef|archive_status` **0 命中**，即「正文来自归档 / 已归档 / 点读失败」没有任何展示与错误态代码，而 §H4 新增了 429/503、§H5 新增了 `CodeNotFound` 语义（`usage_handler.go:135` 注释还承诺「正文由前端按 archive_ref 提示『已归档』」——引用了不存在的前端行为）。③ **配置载体**——`.env.example:75-96` 逐键列出全部 `ARCHIVE_*`，`config/{dev,test,prod}.yaml` 各有整段 archive 配置，新增 6 键一个都没列为改动点。另：yaml 会覆盖代码默认值，`config/prod.yaml:65` 仍写 `hot_window_days: 30`，§I1 的「30 → 3」只改代码默认值在 yaml 生效范围内不起作用（实际靠 §I2 新增的 compose env 覆盖，但方案没点明这个依赖关系）。
- 证据：`:322-330`、`:417-423`、`:542`；`archive_test.go:5,43-58,312,362`；`archive_service_test.go:81,87`；`archive_cron_test.go`；`web/src/components/usage/UsageLogSection.vue:131-137`；`.env.example:75-96`；`config/prod.yaml:65,70`；`.gitlab-ci.yml:105,202`。
- 修复：§G1/§H1 表各补一行「测试：三个测试文件（改 zstd 夹具 + 字段重命名）」；补「配置载体：`config/{dev,test,prod}.yaml`、`.env.example`（新增 6 键、废弃 `gzip_level`；并说明 yaml 30 会盖住代码默认 3，必须同时改 yaml 或依赖 §I2 的 env）」；若保留 429/503 与 `CodeNotFound`，补 `web/src`（UsageLogSection/详情抽屉的错误与空态）。
- 来源：C7

**MIN-16 v2 分块格式未要求 `index.jsonl`/`manifest` 原子替换，也未定义孤儿块清理**（minor｜数据安全）
- 为什么：§G2 的关键点只说「先用临时名写，成功后 rename」（:349），紧随其后解释目的为「避免半成品被读取」；但 :341 的确切表述是「全部块落盘后写 `index.jsonl`、`manifest.json`」，**没有为 `index.jsonl` 规定 temp+rename、fsync 或长度自校验**。分块方案下 `index.jsonl` 是点读的唯一入口（§H2 第 2 步），一旦它在重导时被截断替换，该天归档就整体不可定位。同样缺失的是**孤儿块策略**：重导产生更少的块时，旧块不会被删除、index 只列新块 → 旧块成为无索引的孤儿（占盘但永不命中）；反之若 index 多于块（块被删/被截断），点读会 `Get` 失败。两种不一致都不会被 §F2 的两步校验发现。另外 §E1 要求块内 rid 连续且 index 首尾单调，**依赖「request_id 全局唯一」这一前提，但设计未显式声明**（现实现中它成立：`call_traces.request_id` 为主键）。
- 证据：`:339-343`、`:349`、`:428-433`、`:221-223`；`internal/archive/local.go:63-122`；`internal/observability/model/call_trace.go:10-11`。
- 修复：① 明确 `index.jsonl`、`manifest.json`、`_SUCCESS` 全部走 `tmp + fsync + rename`，并在 index 内写入自身行数/校验和；② 定义重导的孤儿块回收（新版本 verified 后按 manifest/files 删除不在清单中的 part 文件，删除需可审计）；③ 点读时校验「index 声明的块数 == 实际存在块数」，不一致即返回可重试错误并告警；④ 在 §E1 补充对 `request_id` 全局唯一的显式依赖。
- 来源：DS-10

**MIN-17 「`atlas_schema_revisions` 表不存在 ⇒ 迁移从未执行/机制不明」很可能是误判**（minor；由 major 下调：无法连生产确证，且「手工 apply 造成双账本」的风险很小）｜生产运维
- 为什么：§J1/A7/U8 与 `p0 §9` 都把「`atlas_schema_revisions` 表不存在」当作「迁移另有机制或从未执行」的证据。但 Atlas 的机制是：当连接串未绑定某个 schema 时，revision 表会被放进**一个与表同名的独立 schema**，即 `atlas_schema_revisions.atlas_schema_revisions`（官方原文：「If the database connection is not bound to a specific schema (e.g. `postgres://user@host/db`), the table is stored in its own schema called `atlas_schema_revisions`」）。本项目 prod 的 DSN（`config/prod.yaml:13`）无 `search_path`，而 CI 又显式传了 `--revisions-schema atlas_schema_revisions`（`.gitlab-ci.yml:287`）——两条路径指向同一位置。因此在默认 `search_path`（public）下探测必然返回不存在，**即使 Atlas 已正常跑了几个月**；而 `deploy-prod` 的 `needs` 又包含 `migrate-prod`（不跑迁移就不能发布），「迁移从未执行」需要更硬的证据。危害：① 若按 K1 步骤 1 手工执行而不带 `--revisions-schema`，可能新建/使用另一张 revision 表形成**双账本**（核验补充：各 env 默认位置与 CI 一致，只有 DSN 带 search_path 时才分叉，本例没有，故风险很小）；② 在「revision 表缺失」的错误前提下用 `--baseline` 重新引入，会把已应用的迁移重新标记/执行。另 `diagnose_trace_archive.sh` 中 `grep -n atlas` 命中 0，说明该结论不是脚本产出、口径不可复查。
- 证据：`:580`、`:905`；`measurements:269`；`config/prod.yaml:13`；`.gitlab-ci.yml:287`、`:586`；Atlas 文档 Schema Revision Information。
- 修复：① 把 U8 的核查方式改为可判定 SQL：`SELECT version, description, applied_at FROM atlas_schema_revisions.atlas_schema_revisions ORDER BY version DESC LIMIT 5;`（及 `SELECT nspname FROM pg_namespace WHERE nspname='atlas_schema_revisions'`）；② K1 步骤 1 必须写明「与 CI 完全一致的调用方式」（同一 `--baseline`、同一 `--revisions-schema`），禁止使用不同参数的手工 apply；③ 若确认 revision 表只在 `public`，再用 `atlas migrate apply <单个版本号>` 只应用 062/063。
- 来源：OPS-04

**MIN-18 归档链路自身没有任何健康指标/告警出口，N1 第 11 条验收不可实现**（minor；由 major 下调：是「缺通道/阈值/载体的设计说明」而非物理不可实现）｜生产运维 + 需求契合
- 为什么：K3.1/K4/N1 反复要求「水位告警」「连续失败告警」「配 `archive_retention_days` + 告警」，但代码里所有「告警」都只是 `slog.Warn/Error`：水位超限仅 `diskGuardOnce` 里的 `slog.Warn("archive: disk usage above watermark, continue")`（`archive_service.go:762-766`），连续失败仅 `slog.Error("archive: continuous failure alert")`（`:306-312`），两者都不产生指标、不写告警事件表、不进入任何通知渠道。控制面也没有暴露归档相关 metrics（`go.mod` 无 prometheus 依赖；`metric.Int64Counter` 只出现在 budget/collector/spool；`AlertRule` 的 `metric_type` 只支持 success_rate/error_rate/upstream_error_rate，无法表达 `/datalog` 水位或 manifest 卡死），仓库里唯一被运维看板消费的健康载体是 `alert_job_heartbeats` 表，而它**只由 `alert_evaluator` 自己写入**（`recordHeartbeatSuccess/Error` 里 job name 恒为 `alertEvaluatorJobName`），归档 cron 不写心跳。因此方案真正需要回答的三件事都不可观测：①「连续几天没归档成功」只能在容器日志里翻；②「点读失败率」`usage_handler` 只在失败时 `slog.Error`、无计数；③「manifest 卡在 archiving」没有任何巡检/告警读取（生产只有 ops_cleanup 的清理闸会读）。§N1 第 11 步的验收「`/datalog` 水位告警可触发」在当前实现下无法执行。
- 证据：`:881`、`:787`、`:316`、`:790`、`:504`；`archive_service.go:762-766,306-312`；`alert_evaluator.go:797-848`、`:449-459`；`ops_handler.go:433-434`；`usage_handler.go:136`。
- 修复：① 明确「告警」的落地通道（复用现有 `alert_rules`/`alert_events` 或 `internal/notification`，或让归档任务写 `alert_job_heartbeats` 并纳入看板），并给出每条告警的触发条件、阈值、通知对象；② 至少落三个指标/心跳：归档任务每日成功心跳（含处理天数、行数、耗时）、点读失败计数与原因分布、`trace_archive_manifests` 中 `state=archiving` 且 `attempt>0` 且 `updated_at` 距今 >24h 的清单数；③ N1 第 11 条改成可执行验证（例如「手工把水位临时调到当前使用率以下，观察到一条 `alert_event` 记录」）。
- 来源：OPS-07、F-08

**MIN-19 §K3 对 VACUUM 的表述与 PostgreSQL 18 文档不符，且「不 VACUUM 则无法复用空闲页」的因果诊断缺少证据**（minor｜生产运维）
- 为什么：① **诊断与证据不匹配**：`measurements §10` 自己写的是「逻辑上：TOAST 页变为死元组，**新正文可复用该空间**（这也是为何本次实测 df 增长与 TOAST 逻辑增长基本同步——新插入在扩展关系）」，两句话在同一段里互相矛盾；旧定稿 §6.3 写的是「依赖 **autovacuum**（或每日一次 VACUUM (ANALYZE) call_traces）复用死元组」——三份文档对同一机制给出三种说法。② 60 秒对账（TOAST +8.18 MB / df +8.45 MB）本身无法区分「FSM 里没空闲页」与「有但这段窗口的写入仍在扩展关系」；要判定必须先看 `pg_stat_all_tables`（n_dead_tup / last_vacuum / last_autovacuum）、FSM 是否启用、autovacuum 是否被关，而 `p0 §12` 的未验证清单里也没有这几项。③ **与官方文档不符**：PG 18 的 `VACUUM` 默认 `PROCESS_TOAST = true`，即 `VACUUM call_traces` 本来就会处理其 TOAST 表并更新其 FSM；同时默认 `TRUNCATE = true`，plain VACUUM 会尝试把文件末尾的空页归还 OS，而该截断需要**短时 ACCESS EXCLUSIVE 锁**（文档原文提示可用 `TRUNCATE false` 避免）——因此 K3 表格里 plain VACUUM「无阻塞」「不缩小文件」两条表述都不准确。
- 证据：`:661`、`:667`；`measurements:280`；`trace-archive-deletion-migration-design.md:377`；PG 18 `VACUUM` 文档（PROCESS_TOAST / TRUNCATE 默认值）。
- 修复：① 把 K3 的因果论证改为「待验证假设」并给出验证 SQL（`pg_stat_all_tables`、`pg_stat_progress_vacuum`、`SHOW autovacuum`）；② 修正表述（VACUUM 默认即处理 TOAST；plain VACUUM 默认会截断尾部空页并短暂持有 ACCESS EXCLUSIVE，如不接受需显式 `VACUUM (TRUNCATE false)`）；③ 在 K3 表格加一列「预期效果与判定方法」。
- 来源：OPS-11

**MIN-20 归档侧 SQL 全部使用非 sargable 谓词，062 的部分索引服务不了主扫描路径；J3/R7 的成本论证与实际瓶颈错位**（minor｜生产运维 + 遗漏 + 算术一致性）
- 为什么：062 建的是 `idx_call_traces_archstatus_ts (archive_status, ts) WHERE archive_status = 1`，但归档服务实际使用的谓词是 `(ts AT TIME ZONE 'Asia/Shanghai')::date = 'YYYY-MM-DD'::date`——对 ts 施加了表达式，规划器无法把它下推为 ts 的范围条件，因此 `archiveWaterline` 的 MIN()、`pgRowCount`、`fetchBatch`、`writeArchivePointers`、`countLeakRows` 都会走 `call_traces` 的顺序扫描（heap ~2 GB / 529 万行，另加 LEFT JOIN call_logs）。该索引真正能服务的是 janitor 的 `archive_status=1 AND ts < cutoff`。也就是说 J3/R7 把注意力放在「全表建索引耗时」并写成「千万行量级」，而实际总行数是 **5,295,561（约 530 万）**，且迁移刚加完列时 `archive_status=1` 的行数为 0（默认 0）——建索引虽需扫全表但排序数据集为空，秒级即可完成；真正的风险是 `CREATE INDEX`（非 CONCURRENTLY）在构建期间持有 SHARE 锁、阻塞 call_traces 的写入。两者不是一回事。
- 证据：`:596`、`:791`；`archive_service.go:259-261,543-549,521-533,560-566,352-358`；`archive_janitor.go:154-163`；`20260824093000_trace_archive.up.sql:16-17`；`measurements:31`。
- 修复：① 把 §J3/R7 的「建索引耗时」改成准确的风险描述（主要风险是 SHARE 锁阻塞写入 + 需低峰窗口；量级按 530 万行/~2 GB heap 扫描估算）；② 提供让归档谓词可索引的方案（改用 `ts >= ? AND ts < ?` 区间谓词，或建表达式索引）；③ 在 §L3 的首跑耗时估算里计入每晚多次全表顺序扫描（110 天首跑时为数百次）的成本。
- 来源：OPS-12、F-12、A-09

**MIN-21 应用 062/063 会顺带解除 `call_logs` 的 30 天清理闸，方案未把这一耦合写进部署顺序与验收**（minor｜生产运维 + 遗漏）
- 为什么：控制面已无条件启动每日 03:00 的 ops_cleanup 任务（`router.go:621-629`；`ops_cleanup_service.go:22`），其 `verifyCleanupGate` 有两道对账：① 从 `MIN(call_logs.ts)` 到 `now - retention` 之间**每天**都要有 `trace_archive_manifests` 且状态为 verified/cleaned（`:310-317`、`:336-360`）；② 同区间的 `usage_settlement_daily.sum(req_count)` 与 call_logs 对得上（`:319-325`、`:363-412`）。当前因为 062/063 未应用，两道闸都因查询报错而中止，所以 `call_logs` **从未被清理**。一旦按本方案应用 062（并让 archiver 生成 manifest）+ 063（并完成结算回填），这道闸就会开始通过，`DELETE FROM call_logs WHERE ts < NOW() - INTERVAL '30 days'` 会真正开始执行——而这属于**数据删除动作**，§J2 只把 063 标注为「清理闸依赖」，K1/N1 里没有任何一条把「先确认 call_logs 清理的影响 / 或先把 retention 调大」列为步骤或验收项。好在代码注释明确 `cleanupCallLogs 仅删 call_logs，不删 call_traces`，所以对 `/data` 的正文问题没有帮助。另 archiver 会把 2026-06-04 起所有历史天（含 0 行正文的天）都写成 verified manifest，这会「填满」闸①所要求的天数覆盖，值得在方案中明示。
- 证据：`:589`、`:609-621`、`:869-882`；`ops_cleanup_service.go:22-24,189,310-325,336-360,363-412`、`:264-265`；`router.go:621-629`。
- 修复：① K1 显式增加一条：「应用 062/063 前先确认 ops_cleanup 的 `call_log_retention_days` 取值与业务要求，并在观察期内临时把 retention 调大或暂停 cleanup」；② N1 增加验收「ops_cleanup 日志中 gate 通过范围符合预期」与「删除量符合预期」；③ §J2 的 063 说明补一句「本迁移会让清理闸从失败转为可通过，进而开始删除 30 天前的 call_logs（不涉及 call_traces）」。
- 来源：OPS-13、F13

**MIN-22 四条更简单的替代路线（分区 + DETACH/DROP、正文独立冷表、列级 lz4、COPY 导出）全被跳过，未给出否证理由**（minor；由 major 下调：属文档完备性/决策留痕，且部分替代方案自身带未评估代价）｜遗漏与过度设计
- 为什么：§B2 非目标只排除了「对象存储/MinIO/S3」「Parquet/DuckDB 列存」「加密」「全文检索」，完全没有讨论：(a) PG 原生 table partitioning + 独立表空间（冷分区 `SET TABLESPACE` 到 `/datalog`，保留期到期 `DETACH`/`DROP PARTITION` 瞬时回收）；(b) 正文独立冷表（把 body 列搬到 `call_trace_bodies` 并 `SET TABLESPACE /datalog`，按天分区）；(c) 列级压缩（§C2 自己实测出「存量 226,700 B/行 > 原文 188,534 B/行，比值 0.83x」，即 pglz 对 jsonb 几乎零净收益；PG 18.4 支持 `ALTER TABLE ... ALTER COLUMN ... SET COMPRESSION lz4`）；(d) 服务端 `COPY (SELECT …) TO PROGRAM 'zstd …'` 导出。方案选择了「自研 jsonl.zst 分块 + 自研索引 + 自研点读 + manifest + 三态机」这条最重的路线，却未把任一条作为对照写入 §B 或 §O。**核验更正**：这些路线并非「完全没出现」（`compression-design.md:32` 已把「PG 表分区改造」列为非目标、`deletion-migration-design.md:388` 写「按 dt 做表分区改造时再议」、`compression-design.md:163` 正是 COPY 导 Parquet 的草案）；且「分区 + SET TABLESPACE 消掉 pg_repack」不成立——把既有 266 GiB 首次搬到 `/datalog` 的 `ALTER TABLE SET TABLESPACE` 本身就是全表重写并全程持 ACCESS EXCLUSIVE，与 VACUUM FULL 同类代价；「COPY 导出更简单」也忽略了 v2 需要 256 MiB 分块 + index.jsonl + `scrubArchiveNUL` + `json.Marshal` 行格式。lz4 全文 0 提及这一点成立。
- 证据：`:36-42`、`:667-672`、`:900`；`measurements:76`；`trace-archive-compression-design.md:32,163`；`trace-archive-deletion-migration-design.md:388`。
- 修复：在 §B 增加「已评估并排除的替代路线」小节，逐条给出结论与量化否证（尤其要写明分区/独立表空间在**存量回收**上与 pg_repack 同等代价，以及 pglz 无压缩导致 `/datalog` 仅够约 27 天的硬伤）；并明确当 U3 失败（pg_repack 不可行）时的 Plan B 是什么——目前回落的 VACUUM FULL 需要 266 GiB 而 `/data` 只有 54 GiB，等于没有 Plan B。
- 来源：F3

**MIN-23 分块 + 每日索引 + 并发内存闸可能是过度设计：用方案自设的「15~31 s 不可接受」否掉了用户明确接受的「可以延迟」**（minor；由 major 下调：「过度设计」是价值判断，选分块可自证，旧定稿也预留了偏移索引）｜遗漏与过度设计
- 为什么：用户原始诉求之一与 §B1.2 均写明「接受秒级延迟（用户明确接受『可以延迟』）」，§B3.2 把「点读有秒级延迟」列为**明确接受的代价**，§B3.2 甚至把「未分块则 15~31 s」一并列入。但 §A3 又把「未分块则 15~31 s」判为「高」严重度、必须分块，理由来自证据文件的判断句「对 trace 详情页不可接受」——这是方案自设的标准，不是用户给的约束，且与 §B1.2/§B3 直接冲突。方案从未给出「用户可接受的延迟上限」这一决策输入，也没有把「异步点读」（返回 202 + 轮询/后台任务，或管理页「导出这条 trace」）当作选项评估。若选异步，可一次性删掉分块切块逻辑、`index.jsonl` 与其二分查找、manifest 的块列表、`_SUCCESS` 的块级语义、`WithWindowSize` 与解压内存约束、以及 §H4/§I4/§R2 的整套并发信号量与 OOM 风险。附带成本对比也未被摆出：分块把详情页从 ~17 s 降到 ~0.3 s，代价是新增一个自研索引格式、一个新的失败面（index 与块不一致）、以及每并发点读约 128~250 MB 常驻内存（2 GB 容器只能放 4~6 个）。
- 证据：`:32`、`:47`、`:95`、`:15`、`:243`；`measurements:167,19`；`trace-archive-final-design.md:21,252,254,312`。
- 修复：把「点读延迟要求」升级为显式决策项（写进 §O 未决项）：同步点读（保留分块 + 索引）还是异步点读（接受 15~31 s，去掉分块与并发闸）。若选异步，§E/§H/§I4/§R2 可整体砍掉；若坚持同步，需给出「为什么 17 s 不可接受而 0.3 s 必须」的依据（例如 nginx/前端请求超时阈值、用户实际查询频次），而不是以「对详情页不可接受」一句话定案。
- 来源：F4

**MIN-24 权限与审计需求整段缺失：GET trace 正文从不进审计，且 `/my/usage/logs/:request_id/trace` 无属主校验**（minor；由 major 下调：该 IDOR 与读操作无审计是既有代码状态，本方案未引入也未加重）｜遗漏与过度设计
- 为什么：方案 §H1 明确把读路径标注为「**不变**」，但归档把 prompt/response 原文从数据库搬到了 `/datalog` **明文文件**，读它的人、读的频次、读了哪条，方案一个字都没有约束。仓库现状存在两个可直接核查的缺口：(1) **审计中间件只记录写操作**——`audit.go:232-236` 在非 POST/PATCH/PUT/DELETE 时直接 `c.Next()` 返回，`auditRouteMap` 里也没有任何 GET 条目 ⇒ 谁看了哪条 trace 的正文，永远没有审计记录；(2) `/api/v1/my/usage/logs/:request_id/trace` 注册时**没有挂 guard**（`router.go:841`），直接复用 `usageH.Trace`，而 `Trace()` 只按 `request_id` 查库、不做属主/管辖范围校验（`usage_handler.go:120-121`）⇒ 任何已登录用户只要拿到（或枚举到）request_id，就能读取他人请求的完整 headers/body/response；归档开启后这些正文来自 `/datalog`，同样不需要任何额外授权。注意 `/usage/logs/:request_id/trace` 是有 guard（`PermUsageViewAll`）的（`router.go:832`），两条路由行为不一致，说明 `/my` 那条更像是漏加。方案的「两段式读」还把「正文来源」变成了对调用方不可见的实现细节（PG 或 `/datalog`），因此新增一个「读归档正文」的权限位或至少审计事件是必要的。（核验更正：「128 MB/次解码可被用来放大 DoS」已被 §H4 的信号量约束，属方案已识别的需求而非缺口。）
- 证据：`audit.go:232-236`、`:22-158`；`router.go:832`、`:841`；`usage_handler.go:120-121`；`:422`。
- 修复：① 给归档正文点读定义权限（复用 `PermUsageViewAll` 或新增专用权限位），并在 `/my/...` 路由补属主/管辖范围校验；② 为 GET `*/trace` 增加审计埋点（扩展 `AuditMiddleware` 支持读敏感资源白名单，或由 handler 显式 `audit.Record`，记录 user_id / request_id / 数据来源是 PG 还是 `/datalog`）；③ 把这两项写入 §N1 验收（可验证为「审计表中能查到某次归档点读记录」）。
- 来源：F5

**MIN-25 把 prompt/response 明文落到 `/datalog` 且明确「不做加密」，却没有引用同一份实测报告里刚发生过的数据泄漏事件，也没有补偿控制清单**（minor；由 major 下调：是否可接受取决于无法静态核实的部署事实，属文档完备性缺陷）｜遗漏与过度设计
- 为什么：§B2 第 3 条把「不做加密」写成非目标，理由只有一句「`ArchiveStore` 预留了将来接 S3-SSE 的缝」。但证据文件同一份报告 §1 明确记录了 2026-09-08/09 的一次数据泄漏扫描与处置：`leakscan` 扫 37.4 万行、命中 8.47 万条，并把证据归档到 `/datalog/archive/call_traces/body-*.jsonl.gz`（189 GB）；6/7/8 月的正文之所以为空，正是这次处置的结果。也就是说：① 这批正文已被证实包含需要泄漏处置的敏感内容；② 处置证据与新归档将落在**同一个挂载点** `/datalog`（§D1 图、§K5）；③ 方案在已知上述事实的前提下，既没有评估全盘加密/文件权限/挂载点隔离，也没有评估「与证据同盘的留存合规性」，还把旧定稿里原本带条件的加密条款（「仅当本地盘未全盘加密且合规明确要求密文时才评估」）直接降级为无条件不做。方案排除了加密，却没有给出替代的补偿控制清单（目录权限、属主、容器挂载范围、运维/local 用户可读性、备份介质加密），交付物里也没有任何「新建归档的合规确认」条目。
- 证据：`:40`、`:133-136`；`measurements:51,57-62`；`trace-archive-final-design.md:293,355`；`trace-archive-deletion-migration-design.md:39`。
- 修复：在 §B2 的「不做加密」旁补上决策依据与补偿控制（引用 leakscan 事实，说明为何明文落盘可接受——例如 `/datalog` 已全盘加密、目录 0700/服务用户专有、容器仅挂载必要子目录、备份介质加密），或改为评估 AES-GCM 方案；把「新建归档的安全/合规确认」加入 §O 未决项与 §N1 验收（文件权限检查、含敏感内容的抽样确认），而不是仅保留旧定稿那句已被删除的条件句。
- 来源：F6

**MIN-26 没有任何「删除单条 trace 正文」的路径（删除权/GDPR 类诉求无解），且保留期到期后的读语义未定义**（minor；由 major 下调：保留期默认 0 为可选开关，后果是告警噪音/语义不清而非数据丢失）｜遗漏 + 数据安全
- 为什么：方案只提供两种时间粒度的处置：① janitor 到热窗口置空（不删归档）；② `archive_retention_days` 到期「删除归档」（§K4 原文只说「到期删除归档并记录」，未定义粒度，从 §E1 的按天目录看只能是整天删除）。同时 §R4 明确规定「`archive_status=2` 必须由 janitor 设置；**禁止手工 `SET body=NULL`**」，§N2 也只说「从归档按 request_id 批量回填（需临时工具）」。于是出现无法回答的场景：某条 trace 的正文含敏感内容、业务/法务要求**只删除这一条**时，归档里怎么删？方案没有任何 API、流程或工具。按 §E1/§E2 的格式，单条删除实际需要：解压该 256 MiB 块 → 摘除该行 → 重新压缩 → 因块大小变化可能触发后续块重排 → 重写 `index.jsonl`（首尾 rid 必须保持单调）→ 重算 manifest 的 `sha256`/`rows`/`bytes` → 重写 `_SUCCESS`/`manifest.json`，且要让并发点读不读到半成品——这套操作方案完全未设计。对照历史：leakscan 正是因为要处置敏感正文才把 6/7/8 月置空，说明这类需求在本系统**真实发生过**。第二个缺口是保留期到期后的读语义：§H5 规定「PG 无正文 + `archive_ref` 有值 + 归档未命中 → `CodeNotFound`（数据异常，需告警）」，一旦 `archive_retention_days>0`，归档被删除后每条按 request_id 的查询都会落到这一行，产生「数据异常」告警风暴，而 `archive_status` 仍是 2、`archive_ref` 仍非空，系统无法区分「归档被保留策略正常删除」与「归档损坏丢失」。
- 证据：`:731`、`:788`、`:502-505`、`:212-218`、`:241-243`；`measurements:51`；atlas 062 DDL（无 `deleted_at`）。
- 修复：新增「单条删除/脱敏」小节：明确支持（给出按 request_id 的删除流程、重写块步骤、校验与并发保护）或不支持（写明业务侧只能按天整段回收，请业务确认可接受）；同时补 `archive_retention_days` 到期时的状态迁移（例如新增 `archive_status=3=已过期删除`，或在 manifest 记 `deleted_at` 并让读端据此返回「已过保留期」而非「数据异常」）；若涉及个保法/GDPR，把「删除权如何落地」写进 §O 并让业务/法务会签。
- 来源：F7、DS-08

**MIN-27 `archive_ref` 可砍、`archived_at` 语义与文档相反**（minor｜遗漏与过度设计）
- 为什么：(1) **`archive_ref` 冗余**：实现里它就是 `"dt=" + d.Format("2006-01-02") + "/"`——纯粹由「天」推导，而「天」由该行的 `ts` 与 `ARCHIVE_TZ` 唯一确定；读侧 `parseArchiveRefDate` 又把它解析回日期。真正携带信息的是 `archive_status`。因此每行多存一列，并为此在 126 万行上多做一次 `UPDATE … SET archive_ref, archive_status, archived_at`（会写 126 万条新元组、放大 WAL 与表膨胀，而 §K3 刚抱怨过置空造成的膨胀与 `/data` 增长）。既然 062 尚未应用到生产，现在砍掉零成本。(2) **`archived_at` 语义与文档相反**：旧设计把它定义为「置空时间（`archive_status=2` 时写）」，但实现是在 `archive_status=1`（归档完成、正文仍在 PG）时 `SET archived_at = NOW()`，而 janitor 真正置空（→2）的那条 UPDATE **根本不写 `archived_at`**。结果是这一列记录的是「归档时间」而非「置空时间」，字段名与注释都在误导后续审计者；v2 全文没有提过 `archived_at`，既未纠正也未使用。
- 证据：`archive_service.go:559-566`；`archive_janitor.go:172-176`；`trace-archive-deletion-migration-design.md:39`；`20260824093000_trace_archive.up.sql:10`；`:579`、`:590`。
- 修复：砍掉 `archive_ref` 列（读侧用 `(ts AT TIME ZONE ARCHIVE_TZ)::date` 拼天目录，或把天目录只记在 manifest），并相应简化 §G2 第 8 步的 UPDATE；若保留 `archived_at`，把它改成只在 status→2 时写（补进 janitor 的 UPDATE）并把注释与文档统一为「置空时间」，或直接删除该列。两项都应在 062 尚未应用前决定。
- 来源：F12

**MIN-28 N1 第 3 条「压缩加点样本 ≥ 9x」是空洞验收，防不住 R1**（minor；由 major 下调：R1 的缓解不止该断言）｜需求契合
- 为什么：R1 的唯一缓解被写成「单元测试断言压缩率 ≥ 阈值」，N1 第 3 步细化为「≥ 9x」。但窗口只有输入足够大才生效：P0 实测同一输入的对比是 35.5 MB 上 8.46x vs 9.57x（差 13%），142 MB 上 8.81x vs 37.90x（**差 4.3 倍**）。若单测夹具是百 KB~MB 级，128 MB 窗口根本用不上，**删掉 `WithWindowSize` 断言仍会通过**；若用 35.5 MB 夹具，9x 阈值与 9.57x 只差 6%，是脆弱边界断言且随语料波动。因此 N1 第 3 条不能作为 R1 的验收。（R1 另有「code review 清单」与 N1 第 6/10 条的间接暴露。）
- 证据：`:785`、`:873`；`measurements:121,133-134,137`。
- 修复：断言改为对 ≥100 MiB 生成语料的压缩率下限（给出基准语料与阈值余量），或直接断言编码帧窗口（用 `WithDecoderMaxWindow` 小值解码必须失败，或校验帧头 windowLog）。
- 来源：F-06

**MIN-29 风险登记册遗漏晚到行：某天 verified 之后到达的行永不归档也永不置空，正文永久留在 PG，且每晚重复失败**（minor；由 major 下调：正常写入路径下晚到行不常见，后果是少量正文长期留在 PG 并被每晚重复告警，不构成方案失效）｜需求契合
- 为什么：§M 的 10 条风险、§F 状态机、§H5 语义表都没有覆盖「晚到行」。现有实现对此有硬行为：某天一旦 verified，若再出现 `archive_status=0` 且有正文的行，`ArchiveDay` 直接返回错误（`archive_service.go:336-343`，注释即 `cannot re-archive in place`），不再补归档；janitor 的 Phase A 每晚对这类天重试并失败告警（`archive_janitor.go:99-127`，`:120-124`），Phase B 又因这些行不是 `status=1` 而不会置空；`markCleanedDays` 因 `pending>0` 永不收敛（`:202-213`）。结果是这些行既不进归档也不出 PG，该天永远无法收敛到 `cleaned`，`/data` 稳态被持续垫高。§G3 引入 `closure_lag_hours=24` 本身就承认数据会晚到，而 `hot_window_days` 默认 3 意味着晚于约 2 天到达即触发。改为分块后本可通过追加新块 + 重算 index 消化，但设计没有这条路径。全仓 grep「晚到/泄漏」确认三处均无对应条目。
- 证据：`:305-316`、`:498-505`、`:781-794`、`:526`、`:518`；`archive_service.go:336-343,350-359`；`archive_janitor.go:99-127,154-163,202-213`。
- 修复：把晚到行写入 §F 状态机（verified 后允许追加 part 并重算 index/manifest，或明确并入次日归档），并在 §M 增加对应风险与 N1 验收，同时说明它是否计入 `days_per_night` 配额。
- 来源：F-10

---

## 5. Nit（8 条）

| ID | 问题 | 为什么是问题（含触发） | 证据 | 修复建议 | 来源 |
|---|---|---|---|---|---|
| NIT-01 | §I4「编码并行 2 块」与 §E4 `WithEncoderConcurrency(1)` 自相矛盾 | §E4:252 固定 `zstd.WithEncoderConcurrency(1)`，库文档明确「For streams, setting a value of 1 will disable async compression」，§G2 流程也是单线程顺序；因此 §I4:561-562 的「编码并行 2 块 ~300~600 MB / 建议上限 2」没有对应机制（要并行只能自行起多个 goroutine + 独立 encoder，方案未设计），§I1 配置表也没有对应并发项。作为内存评估会让读者以为存在一个「并发上限=2」的旋钮 | `:252`、`:561-562`、`:567`；`klauspost/compress@v1.20.0/zstd/encoder_options.go:81-89` | 删除「编码并行 2 块」行，或明确写「多块并行需自行起 goroutine + 独立 encoder，本方案不做」 | T10 |
| NIT-02 | §C1「总大小 271 GB = heap 2,060 MB + TOAST 265.84 GiB」括注在两种单位下都不闭合 | 若 271 是十进制 GB，则仅 TOAST 就 285.44 GB > 271（物理不可能）；若 271 实为 GiB，则 2.01 + 265.84 = 267.85 GiB，仍差 ~3.1 GiB（数值上正好是索引项），而括注只列了 heap + TOAST。属单位与括注不完整 | `:58`；`measurements:29-31` | 改为「~271 GiB（heap 2.0 GiB + TOAST 265.84 GiB + 索引 ~3.1 GiB）」，全文统一 GB/GiB | A-03 |
| NIT-03 | §K4 保守月增 59 GiB 偏低（应 ~61 GiB）；「6~10 个月」只在扣减存量后成立却未标注 | 101,195 × 30 × 21,513 B = 65.31e9 B = **60.83 GiB/月**，文档写 59（低 3%）；月数复核：不含存量时 368/60.83 = 6.05、368/33.84 = 10.88 ⇒ 应为 6~11 个月，含存量（扣 25.2/14.0 GiB）时 5.64~10.46 ⇒ 6~10 个月。`measurements:214` 明确标注「（含存量）」，v2:727 与 :17 均未标注 | `:720`、`:727`、`:17`；`measurements:214` | 保守月增改为 ~61 GiB/月，或在表头注明「已扣减存量归档 14~25 GiB」，否则写 6~11 个月 | A-06 |
| NIT-04 | §K4「释放遗留后 ~557 GiB」把 189 GB 直接当 GiB 加到 368 GiB 上（**存疑**） | 368 GiB + 189 GB = 544.1 GiB（若 189 是 GiB 则 557 成立，但全文其余处一律写作 189 GB）。月数结论 9~16 个月不变，仅影响量级标注（虚高 13 GiB）。**核验判 uncertain**：`measurements:44` 的 df 输出为「193G (35%)」而 §K5 的「193 G ≈ 189 G + 2.2 G + 杂项」只有在 189 为 GiB 量级时才闭合（若 189 GB = 176 GiB，杂项需占 ~15 GiB，与同句自相矛盾），故证据文档很可能把 df 的 189 GiB 写成了「189 GB」。静态只读无法核实该目录真实字节数 | `:723`、`:728`、`:736`；`measurements:44` | 在生产执行 `du -sb /datalog/archive/call_traces` 定 557 还是 544，无论结果都把单位统一为 GiB 后再给数 | A-07 |
| NIT-05 | §J2 说迁移 062 是「4 列」，实际是 5 列，且与 §J1 自相矛盾 | 062 第 5-10 行一次性 ADD 了 task_id、documents_count、archive_ref、archive_status（NOT NULL DEFAULT 0）、archived_at 共 **5 列**；§J1:579 自己就列了 5 个缺失列名，而 §J2:588 摘要写「4 列」。§J2 是给实施者核对迁移范围的清单，少一个会漏掉 `archived_at` 的核对（该列在 janitor 的 cleaned 时间戳语义与 §F1 三态机中都有用） | `:579`、`:588`；`20260824093000_trace_archive.up.sql:6-10` | 把 §J2 的「4 列」改为「5 列（task_id / documents_count / archive_ref / archive_status / archived_at）」 | A-08 |
| NIT-06 | 点读未分块延迟在 §A3/§B3 写 15~31 s，§C4 与实测外推为 16.9~30.8 s | 实测中位 0.34 s × 49.6 = 16.9 s、尾部 0.62 × 49.6 = 30.8 s，§C4:95 写「中位 ~17 s / 最坏 ~31 s」正确；但 §A3:15 与 §B3:47 写「整天 15~31 s」，下界 15 无出处（比实测外推低 12%）。证据文档自身 `:19` 也写 15~31，与同文档 §5 的 17 s 自相矛盾。不影响结论 | `:15`、`:47`、`:95`；`measurements:19,159-165` | 统一为「中位 ~17 s / 最坏 ~31 s（忙日 ~48 s）」 | A-13 |
| NIT-07 | 决策可用性：关键结论不在开头，K1/N1 两套清单，全文无工作量/排期与责任人 | §R8 专项（`/data` 2.5~5 天写满、方案无法在窗口内落地）埋在第 796 行（全文 943 行）且自带「本方案之外，但必须并列决策」；§O 的 U7 同样靠后；§N 无工作量（人日）、无责任人、无冻结时间点；§A 的 9 条差异与 P0 §11 的修正清单、压缩数字在 §C2-C4/§D3/§L1 三处重复。核验收窄：K1 与 N1 除 N1 多出 codec/并发闸/点读/retention 四项外基本同构（N1 ⊃ K1），「必须拼起来才完整」属夸大；「§N1 无 cron no-op 验证」亦不实（N1 第 5 条即该验收） | `:609-621`、`:869-881`、`:796`、`:904`、`:834-838`、`:11-21`；`measurements:297-307` | 在文档开头加 10 行「结论与决策请求」（含 `/data` 时间窗、必须并行的应急处置、需拍板项）；把 K1/N1 合并为单一「实施步骤 + 验收标准」表；补工作量/责任人/冻结时间；删去与 P0 重复的修正清单（改为引用） | F-17 |
| NIT-08 | 引用与顺序小错：把压缩草案的行号写成章节号；§G2 第 7/8 步顺序与现有代码相反且未在差异表标注 | (1) 附表写「早期压缩选型草案（其 §146 的前提已被证伪）」，而该草案是 391 行文档、`measurements:79` 引用的是 `:146` 行号，读者无法定位。(2) §G2:344-345 把「UPDATE manifest SET state=verified」列为第 7 步、「UPDATE call_traces SET archive_ref/status=1」列为第 8 步，而现码顺序相反（先 `writeArchivePointers`、再 `markManifestVerified`），§A 差异表未列这一改动——该调序不只是顺序问题：两步之间崩溃会让 manifest 已 verified 而指针未写，次日落入 `cannot re-archive in place` 死路（与 MIN-29 同一路径） | `:937`、`:344-345`、`:467-470`；`measurements:79` | 把 §146 改为「:146（行号）」或直接引用结论；把 §G2 的步骤顺序与代码对齐，或在 §A 差异表新增一条说明该调整及对崩溃恢复的影响 | F-18 |

---

## 6. 被核验为正确的关键判断（给作者的正反馈）

以下判断在证伪核验中被确认为成立，是这份方案真正的价值所在。**请勿因为 blocker 而推翻它们**。

| # | 被核验为正确的判断 | 支撑 |
|---|---|---|
| 1 | **codec 选型方向正确**：zstd（`SpeedBestCompression` + 128 MB 窗口）优于 gzip，且纯 Go 库可行、版本 `v1.20.0` 可用、无 cgo | `measurements §3/§4`；`MAJ-03` 的核验结论也明确「codec 选型方向不变」 |
| 2 | **`WithWindowSize(128<<20)` 是硬约束**：同输入不加窗口 8.81x → 加了 37.90x，差 4.3 倍且不报错；这条「必须写进代码」的判断成立 | `measurements:133-137`；R1 的识别本身正确 |
| 3 | **`WithDecoderMaxWindow(128<<20)` 与写侧窗口相等不会自伤**：流式 writer 帧头 WindowSize 恰为 128 MiB（`frameenc.go:68-69` 对 `1<<27` 得 windowLog=136，`decodeheader.go:150-153` 反解回 27、mantissa=0），库的 `>` 严格判定通过 | `T07` 核验员的逐行源码核对；`decoder_options.go:35`、`framedec.go:43`、`decoder.go:357` |
| 4 | **容量区间取了保守下界**：`14~25 GiB` 与 `34~59 GiB/月` 的下界用的是 9.57x（35.5 MB 样本）而非 19.61x，方向偏保守，因此 `MAJ-03`/`MIN-10` 的口径问题**没有击穿**容量结论 | `:756-758`、`:205-206` |
| 5 | **`0.878`（Go ≈ CLI 的 87~88%）有两条独立来源**：同一 35.5 MB 文件上 Go 9.57x vs CLI -22 10.90x 已单独给出；§4.4 也是并列引用两个测量。故 142 MB 产物缺失只影响 §4.2 窗口实验的可复现性，不影响该换算系数 | `measurements:121-123`、`:145-146`；`CAP-05` 核验 |
| 6 | **大量算术复算无误**：226,700 = 285,444,734,976/1,259,124（226,701）；188,534×1.092 = 205,879；1.20 倍关系；14~25 GiB；642 GiB；3/7/2 天各行；21.4 GiB/天；1047 MB/s；点读 17/31 s 外推；「30 天不可能」（642 GiB 远超 344 G 容量） | `A-01` 核验员的逐项复算 |
| 7 | **样本日代表性没问题**：留存窗口 11 天共 1,259,124 行（≈114,466 行/天）与 09-19 的 116,891 行基本一致；「9 月只有 1,259,124/1,874,552 有正文」是分母混入了已被 leakscan 清空的 09-01~09-09 | `CAP-06` 核验主动排除该质疑 |
| 8 | **`fast default` 判断正确**：062 的 `ADD COLUMN ... DEFAULT 0/NULL` 在 PG 11+ 属元数据变更、不重写表（生产为 PG 18.4） | `:594-595`；`A-09`/`F-12` 核验均确认 |
| 9 | **`368 + 189 = 557` 的直接相加算术成立**（在 189 即 GiB 的前提下）：df 的 Avail 已扣保留块，删除 189 后保留块不变，故可直接相加 | `CAP-01` 的「已核对为非问题」部分 |
| 10 | **释遗留后 557 GiB → 9~16 个月在两种口径下都成立**（544~557 GiB ÷ 33.8~60.8 GiB/月） | `A-06` 核验 |
| 11 | **`offhost_copy_marker` 缺失时闸门 fail-safe，且这正是现成的「只归档不置空」开关**：`require_offhost_copy=true` + marker 为空 → janitor 整轮跳过置空；K1 把写 marker 排在第 6 步、收紧窗口排在第 7 步，顺序是合理的 | `OPS-05` 核验；`archive_janitor.go:81-88`；`config/prod.yaml:73` |
| 12 | **「单纯置空并不阻止 /data 增长」这一物理事实表述正确**（§K3:660-661），只是证据链与边界条件需要补强 | `CAP-04` 核验 |
| 13 | **方案主动并列呈交 `/data` 时间窗，没有藏风险**：§R8/R8.1 明确算出 2.5~5 天写满、并写「本方案无法在 2.5~5 天内落地 ⇒ 应急处置必须并行」 | `CAP-03` 核验；`:796-841` |
| 14 | **`_SUCCESS` 驱动的增量同步方向、按天 `dt=` 布局、三态机、「指针先置、正文后清」的设计意图本身是合理的**（问题在执行层与校验层，不在意图） | 通读 §D/§E/§F |
| 15 | **`archive.max_concurrent_reads` 信号量的需求识别正确**（§H4:495 指出 5 并发即可 OOM），落点未写明是文档问题而非需求问题 | `C3` 核验 |
| 16 | **`apply` 层的部分索引/快速默认值等技术判断无误**；`A-09`/`F-12` 也只挑出「千万行」量级与「全表建索引」措辞 | `:596`、`:791` |
| 17 | **U4（块大小定标）确属定稿前可闭环的本地基准项**（F-16 的核心结论被证伪，但这一条被核验承认） | `F-16` 核验 |
| 18 | **迁移机制本身可在仓库内读到**：`migrate-prod` 在打 tag 时手动执行、Runner 内直连、`--revisions-schema atlas_schema_revisions`（该点被用于纠正 MIN-17 的误判） | `.gitlab-ci.yml:260-292` |
| 19 | **R1（漏配窗口不报错、静默劣化 4.3 倍）的识别是真问题**，只是验收手段不足 | `:785`、`:137` |
| 20 | **这份文档的测量方法学值得肯定**：所有容量数字都标注来源文件与行号，关键结论同时给出「已实测」与「未验证」两类；`§O` 未决项、`§M` 风险登记册、`§A` 差异表的结构，让 8 个视角的评审得以逐条落地——这在同类设计文档中并不多见 | 全篇 |

---

## 7. 建议作者补充的测量 / 验证

> 只列**真正需要在生产或测试环境执行**的验证项，按优先级排序；括号内标注该验证能关闭哪些问题。

### P0：定稿前必须完成的验证（否则 blocker/major 的修法缺依据）

| # | 验证项 | 方法 | 关闭/校准 |
|---|---|---|---|
| V1 | **逐块回读校验 + 故障注入** | 写侧计数 vs 回读 part 重新统计；故障注入矩阵 W1~W6：封块中 kill、rename 后 kill、`_SUCCESS` 后 kill、指针后 manifest 前 kill、janitor 置空中 kill、重导过程中 kill、截断一个块、`/datalog` 卸载后跑一轮 janitor | BLK-01、BLK-02、MAJ-01、MAJ-09、MIN-16 |
| V2 | **`/datalog` 归档目录内容对账** | `ls` 目录文件数与日期范围；抽样 `gzip -dc body-YYYY-MM-DD.jsonl.gz \| wc -l`；确认行结构是否为 `archiveRecord`；与 PG 中 6/7/8 月已置空行数比对 | MAJ-14、NIT-04 |
| V3 | **pg_repack 测试库演练** | 记录：置空后真实工作表体积（存活元组 vs 整表）、`--tablespace` 实际行为（是否搬表）、TOAST 处理、repack 期间 `/data` 上 `pg_wal`/`pg_temp` 峰值、所需维护窗口 | MAJ-06、MIN-19、MIN-22 |
| V4 | **VACUUM 止涨实测** | `VACUUM (ANALYZE) call_traces` 前后：`pg_stat_all_tables.n_dead_tup`/`n_tup_upd`、`SHOW autovacuum`、FSM 状态；随后 24h 观察 df 是否停止增长 | MAJ-07、MIN-19 |
| V5 | **生产库 collation 与排序一致性验证** | `SHOW lc_collate`；对同一批 request_id 分别按 DB 排序与 Go 字节序排序，检查相对顺序是否一致；补大小写混排、含 `-`/`_`、非 UUID id 的边界单测 | MAJ-02、MIN-01 |
| V6 | **生产 `atlas migrate status` + revision 表定位** | 在 prod DSN 上执行 `SELECT version FROM atlas_schema_revisions.atlas_schema_revisions ORDER BY version DESC LIMIT 5` 与 `SELECT nspname FROM pg_namespace` | MAJ-13、MIN-17 |

### P1：性能/容量标定（决定阈值与预算）

| # | 验证项 | 方法 | 关闭/校准 |
|---|---|---|---|
| V7 | **Go 编码器 + 256 MiB 分块的真实压缩率** | 用生产参数压一个 256 MiB 块 + 压一整天（单帧）做对比，取较低者为预算基准；补 64/128/256/512 MiB 对比 | MIN-10、MIN-11、N1 步骤 3 的验收阈值 |
| V8 | **真实 RSS 峰值测量** | 编码 1 块（含 2×window hist cap 与 256 MiB 块暂存）、点读 1 次、4 并发点读、导出批次（`batch_rows=3000`）、宿主 `free -m` 与容器 `docker stats` | MIN-05、MIN-14 |
| V9 | **块内点读端到端延迟** | 在 256 MiB 块中命中随机 rid（含 `json.Unmarshal` 全扫 vs 前缀比对优化两种实现） | MIN-02、N1 步骤 7 的 P95 阈值 |
| V10 | **跨日抽样方差** | 09-10 / 09-15 / 09-19 各取 2%，报告字节/行宽与压缩率方差；补 P90 与峰值日（182,439 行/天）的容量档 | MIN-11、MAJ-04 |
| V11 | **生产机 Go 编码器真实吞吐** | 在服务端跑最小 Go 程序压一个真实全天文件（§O 的 U1） | MAJ-05 的工期算法 |

### P2：运维与恢复演练（上线前后持续）

| # | 验证项 | 方法 | 关闭/校准 |
|---|---|---|---|
| V12 | **逐天离机副本校验** | 备机上按 dt 校验 `_SUCCESS` 存在 + 归档 sha256 与本地一致，并把结果写入按天 marker | MAJ-10 |
| V13 | **恢复演练（建议每月）** | 从异机副本逐天 sha256 比对 + 随机 rid 全文回灌校验 | MAJ-10、MAJ-01 |
| V14 | **告警通道端到端** | 人为把 `/datalog` 水位临时调到当前使用率以下，确认产生一条可观测的告警记录（`alert_event` 或通知） | MIN-18、N1 步骤 11 |
| V15 | **清理闸影响确认** | 在应用 062/063 前确认 `call_log_retention_days` 取值；上线后核对 ops_cleanup 日志中 gate 通过范围与删除量 | MIN-21 |
| V16 | **回归测试基线** | `go build -mod=vendor ./...` 通过；`archive_test.go`/`archive_service_test.go`/`archive_cron_test.go` 完成 zstd 夹具与字段重命名改造 | MIN-13、MIN-15 |

---

## 8. 被证伪的误报（审计信息，不计入问题）

> 这 7 条原始结论在证伪核验中被推翻。保留在此是为了说明**哪些怀疑方向可以被安全排除**，避免作者在修订时被误导去做无谓改动。

| # | 原提议（严重度） | 证伪理由 | 结论 |
|---|---|---|---|
| R1 | **T05**：封块必须「按整行」未写死，一旦行跨块，两块都解析失败而行数校验仍通过（nit） | 结论建立在未被证实的实现假设（「若实现成按字节/流位置切分…」）上。§E2(225-233) 明确「一行一条、`\n` 分隔」且「格式不变，只改外层容器」，§G2(337) 的「**累积**到 ChunkMB 时封块」语义就是按行攒够再封；现实现 `streamExport`（`archive_service.go:488-506`）也是逐行 `append(b,'\n')` 后写出，任何按行缓冲 + 到阈值 flush 的写法天然不会拆行。其后果推断（解析失败静默跳过）描述准确，但那已被 MAJ-01 覆盖。建议的「单测构造恰好跨过 ChunkMB 的长行」有价值，属 nit 级测试完善 | 剔除；建议作为 MIN-01/MAJ-01 的补充测试用例保留 |
| R2 | **A-04**：§C5 磁盘三元组不自洽（288+54=342≠344、193+368=561≠590），两行百分比分母口径不同（minor） | 中心论断「两行用了不同的百分比分母」不成立。GNU df 的 Use% 用同一公式且**向上取整**（coreutils `df.c`：`pct = u100 / nonroot_total + (u100 % nonroot_total != 0)`）。代入文档的数：/data ⌈28800/342⌉ = ⌈84.21⌉ = 85%（文档 85%）；/datalog ⌈19300/561⌉ = ⌈34.40⌉ = 35%（文档 35%）——两行完全同口径，且就是 `df -h` 的原样输出。三列差不闭合同样是 df 语义（Size − Used − Avail = 保留块，/datalog 约 29 G ≈ ext4 默认 5%，加上 `-h` 取整）。残余仅是同格内「G」与「GiB」混写 | 剔除；单位混写并入 NIT-02 |
| R3 | **DS-11**：`hot_window_days` 30 / `batch_rows` 3000 的默认值不一致，env 漏注入时静默走旧默认，使 §K1 分阶段部署失效（minor） | §I2:540-547 的 compose env 清单**第一条即为** `ARCHIVE_HOT_WINDOW_DAYS: ${ARCHIVE_HOT_WINDOW_DAYS:-3}`（:542）。只要按 §I2 落地，宿主未设置该变量时 compose 也会注入 3；env 在 Viper 中优先级高于 yaml 与代码默认（`config.go:225` + `SetEnvKeyReplacer` + `AutomaticEnv`）。因此「漏洞方向」（hot_window 静默 30 导致 §K1 第 7 步失效、/data 继续增长）不成立。残留两处均属 nit：§I1:519 把 `batch_rows` 的「旧值」写成 1000（实际 3000，且 §I2 未列 `ARCHIVE_BATCH_ROWS`，但 v2 值本就是 1000、不构成数据安全风险）；设计未要求启动时打印/校验最终生效配置 | 剔除；`batch_rows` 旧值并入 MIN-09 |
| R4 | **OPS-05**：K1 步骤 4「hot_window_days=15 ⇒ janitor 无候选」只在 3~4 天内成立，安全闸没有代码级保障（major） | 计时事实对（`archive_janitor.go:72` 确为滚动 `now - hotWindowDays*24h`），但核心结论「安全闸没有代码级保障」「代码里没有任何只归档不置空的开关」是**错的**：`offhostCopyOK()`（:131-140）在 `require_offhost_copy=true` 且 marker 为空时直接返回 false，`run()`（:81-87）随即整轮跳过置空——这正是现成的「只归档不置空」开关，而 `config/prod.yaml:73` 的 `offhost_copy_marker: ""` 就是默认状态。K1 的顺序恰是步骤 4/5 先归档观察、步骤 6 才写 marker、步骤 7 才收紧窗口。因此两个失败模式均不成立。建议步骤 4 直接写 `hot_window_days=3650` 或显式声明「此时 marker 必须为空」 | 剔除 |
| R5 | **F11**：并发内存闸与「读路径不变」矛盾，且两个 `UsageHandler` 实例共享 reader 使全局上限变成 2×4=8（major） | 核心论据不成立。`router.go` 中注册 trace 详情路由的只有一个实例：`:832` 与 `:841` 都用 `usageH.Trace`；`usageH2`（:1072-1084）只注册 `/usage/dashboard*`、`/usage/scoped/logs`、`/usage/report/*`，**从不调用 `.Trace`/`ReadBody`**，因此 `:1073` 的 `usageH2.SetArchiveReader(archiveReader)` 是无效注入（`router.go:46` 注释亦说明该注入只是为两处共用而预留）。故「2×4=8 → 峰值 1.2~2 GB → R2 缓解失效」不会发生。另一半不构成矛盾：`v2:421` 已把 `readbody.go` 列为要改，把闸门实现在 `LocalStore` 层即可与「usage_handler/router 不变」自洽，文档只是没写明闸门归属 | 剔除；「未写明闸门归属」并入 MIN-14 |
| R6 | **F-14**：基线数字自相矛盾：271 GB 小于其括号内的 TOAST 265.84 GiB + heap；有正文天数 11 与 12 不符（minor） | 主指控不成立。271 GB 与 265.84 GiB 同属 `pg_size_pretty` 的 1024 进制显示口径（按此 265.84 GiB + 2,060 MB ≈ 267.9 GiB，再加 TOAST/heap 索引约 3 GiB 恰为 271），故 `:58` 括号内外自洽；「271 GB 小于 TOAST 一项」是把 GB 当 10^9 换算造成的误算。残留两点只够 nit：GB/GiB 混用未说明进制；`11 天` 与 `09-10~09-21`（含端 12 天）口径不一致 | 剔除；两点并入 NIT-02 与 MIN-06 |
| R7 | **F-16**：§O 的 U2、U4、U8 都可在定稿前闭环，却被推给待复核（minor） | 三条里两条不成立、一条证据错误。**U2 的「已存在 day.jsonl（440 MiB）」并非完整一天**——`measurements:87` 记「生产语料 440 MiB / 2,355 行」，`:159` 明确「2026-09-19 = 116,891 行 / 样本 2,355 行，×49.6」，即它是 `SAMPLE_FRAC=0.02` 的约 2% 抽样；U2 问的是 ~20.8 GB 全天文件的压缩率，再压一次 2% 样本不能收敛该值，且产物在生产机 `/datalog` 上。**U8 读 CI 只能回答「怎么执行」**，解释不了 `p0:269`「`atlas_schema_revisions` 不存在」所指向的迁移历史疑点。仅 **U4（块大小定标）建议成立** | 剔除；U4 建议由 §5 第 17 条承接 |

---

## 9. 附录

### 9.1 合并映射与统计口径

| 合并后 ID | 原始 ID（视角） | 最高修正严重度 |
|---|---|---|
| BLK-01 | DS-01(blocker) + F10(major) + T03(minor) + F-18 第 2 点(nit) | blocker |
| BLK-02 | DS-02(blocker) | blocker |
| MAJ-01 | T01(major) + DS-03(major) + F9(major) | major |
| MAJ-02 | T02(major) | major |
| MAJ-03 | A-01(major) | major |
| MAJ-04 | CAP-01(major) + F1(major) + F-02(major) | major |
| MAJ-05 | OPS-09(major) + CAP-03(major) + F2(major) + F-04(major) + F-03(major) | major |
| MAJ-06 | OPS-02(major) + CAP-02(minor) | major |
| MAJ-07 | CAP-04(major) + F-01(major) | major |
| MAJ-08 | DS-07(major) + OPS-10(major) + F-11(minor) | major |
| MAJ-09 | DS-05(major) | major |
| MAJ-10 | DS-04(major) + OPS-06(major) + F8(major) | major |
| MAJ-11 | DS-06(major) | major |
| MAJ-12 | DS-09(major) | major |
| MAJ-13 | OPS-03(major) | major |
| MAJ-14 | C2(major) + A-12(minor/uncertain) | major |
| MAJ-15 | A-14(minor) + F-09(major) | major |
| MIN-01 | T04 | minor |
| MIN-02 | T06 + F-15 | minor |
| MIN-03 | T07 | minor |
| MIN-04 | T08 | minor |
| MIN-05 | T09 + OPS-08 + F-07 | minor |
| MIN-06 | A-02 | minor |
| MIN-07 | A-05 | minor |
| MIN-08 | A-11 + CAP-07 | minor |
| MIN-09 | C5 + C4 + A-15 + T11 + F-14 + F-13 | minor |
| MIN-10 | CAP-05 + T12 + A-10 | minor |
| MIN-11 | CAP-06 | minor |
| MIN-12 | CAP-08 + OPS-01 + C6 | minor |
| MIN-13 | C1 | minor |
| MIN-14 | C3 + F-05 | minor |
| MIN-15 | C7 | minor |
| MIN-16 | DS-10 | minor |
| MIN-17 | OPS-04 | minor |
| MIN-18 | OPS-07 + F-08 | minor |
| MIN-19 | OPS-11 | minor |
| MIN-20 | OPS-12 + F-12 + A-09 | minor |
| MIN-21 | OPS-13 + F13 | minor |
| MIN-22 | F3 | minor |
| MIN-23 | F4 | minor |
| MIN-24 | F5 | minor |
| MIN-25 | F6 | minor |
| MIN-26 | F7 + DS-08 | minor |
| MIN-27 | F12 | minor |
| MIN-28 | F-06 | minor |
| MIN-29 | F-10 | minor |
| NIT-01 | T10 | nit |
| NIT-02 | A-03 + F-14 残留 | nit |
| NIT-03 | A-06 | nit |
| NIT-04 | A-07(uncertain) | nit |
| NIT-05 | A-08 | nit |
| NIT-06 | A-13 | nit |
| NIT-07 | F-17 | nit |
| NIT-08 | F-18 第 1 点 + 第 2 点 | nit |

**统计口径**：原始清单共 **75** 条（68 条存活 + 7 条证伪）。合并后存活 **54** 条：blocker 2、major 15、minor 29、nit 8。严重度取合并组内最高 `corrected_severity`；核验员将 12 条从原判下调（DS-03/DS-04/DS-05 部分、OPS-01/02/03/04/07/08/11、C1/C3、F3/F4/F5/F6/F7/F8/F-01/F-05/F-06/F-07/F-08/F-16、A-02/A-03/A-08/A-10、T03/F-19 等），本报告全部采纳。

### 9.2 八视角各自的有效产出密度（供后续评审组织参考）

| 视角 | 存活条目数（合并前） | Blocker/Major | 特点 |
|---|---|---|---|
| 数据安全与不可逆风险 | 10 | **2 blocker + 5 major** | 产出质量最高、命中率最高；2 条 blocker 全部来自此视角 |
| 生产可行性与运维 | 12 | 4 major | 命中了 pg_repack 空间、迁移乱序、离机闸、配额闸等落地断裂项 |
| 遗漏、更简方案与过度设计 | 13 | 4 major | 命中「校验五项砍到两步」「指针按天批量 UPDATE」；但替代方案论证自身有错，被核验下调 5 条 |
| 算术与文档内部一致性 | 13 | 1 major | 命中 gzip 236 GiB 这一个关键数字错误；其余多为口径类 nit/minor |
| 容量模型与保留策略 | 8 | 3 major | 命中保留期与盘量矛盾、/datalog 重复记账、过渡期峰值缺失 |
| 需求契合度与决策可用性 | 14 | 4 major | 与运维/容量视角高度重叠，独立新发现较少（多数被合并） |
| 与现有代码库的一致性 | 7 | 1 major | 命中 189 GB 归因、vendor、改造清单遗漏；其余为配置表核对 |
| 技术正确性与实现可行性 | 9 | 2 major | 命中 collation 与校验有效性问题，深度最好 |
| **合计** | **86 → 合并后 54** | **2 + 15** | — |

### 9.3 建议的修订顺序

1. **先修数据安全链**（BLK-01 → BLK-02 → MAJ-01/09/10/11/12）：这是唯一「不修就会丢数据」的一组，且相互耦合，应作为一个整体设计（校验建立在落盘产物上 + 置空前置条件 + 按天离机凭证 + 开关拆分）。
2. **再修运维落地路径**（MAJ-07/05/06/13/15 + MIN-12/18/21）：K1/N1 必须重新对齐，把 VACUUM、回填工期、repack 空间、迁移治理、前置条件写成一条可执行、可验收的路径。
3. **重算容量与保留期口径**（MAJ-03/04 + MIN-06/07/08/10/11 + NIT-02/03/04）：把 236 GiB、11 天、82 块、365 天、3.04 万行/天这一批数字统一到一套可复算的口径。
4. **最后收敛文档与配置一致性**（MIN-09/13/14/15/20/27 + NIT-01/05/06/07/08）：这一批不阻塞实施，但直接决定文档能否被当作「可审计」的单一真源。
