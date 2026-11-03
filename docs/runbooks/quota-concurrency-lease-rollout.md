# 并发配额租约化 + 配额拒绝落库：上线演练与运行手册（草案）

> 状态：**DRAFT — 待 t5/t7 审查通过后定稿**。
> 本手册由集成任务产出，**仅含命令与核对步骤，不包含任何已经在生产执行过的动作**。
> 所有 `redis-cli` / SQL 命令都由**人工**在生产侧执行；本文档的作者未对生产环境做过任何变更。

涉及三条工作流：

| 工作流 | 交付物 | 影响面 |
|---|---|---|
| A. 并发配额租约化（自愈） | `internal/gateway/quota/{redis_store.go,middleware.go}` + `internal/core/quota/types.go`（键格式共享常量） | 并发闸判定 + Redis 键格式 |
| B. 并发读数只读修复 | `internal/controlplane/service/quota_service.go`（`ZCOUNT` 统计在飞租约） | 管理面「当前并发使用量」不再恒为 0 |
| C. 配额拒绝可观测性 | `internal/gateway/quota/reject.go` + `call_logs` 迁移 + 控制面读取端 | 只增加观测字段，不改判定 |

三条工作流都落在配额子系统，但**互不修改同一文件**，因此可以同批发布；本手册仍给出分阶段核对点，便于出问题时归因。

---

## 0. 一句话结论（上线前必读）

- 并发计数从「裸 `INCR`/`DECR` + 成功即续期 TTL」改为「**ZSET 租约 + 逐条目到期**」。丢失的归还（优雅退出 15s 强断长流式请求、进程被杀）会在**租约到期后自动消失**，不再把用户永久钉死在限额上。
- 并发键前缀由 `quota:conc:` 变为 **`quota:conc:lease:`**。灰度/蓝绿期新旧代码共存时，旧代码的 `INCR` 不会打到新键上（否则会 `WRONGTYPE`），因此新老键互不干扰。
- 旧前缀 `quota:conc:*` 下的历史键**不再被新代码读写**，是纯垃圾数据，按 §4 由人工清理。

---

## 1. 上线前核对（只读）

### 1.1 构建与测试

```bash
# 全量构建必须通过
go build ./...

# 配额两套测试都要跑（make test 只覆盖 ./tests/...）
go test ./internal/gateway/quota/... -count=1
go test ./tests/gateway/quota/... -count=1

# 并发安全
go test -race ./internal/gateway/quota/... -count=1
```

> ⚠️ 工作区若处于多人并行开发状态，`go test ./...` 可能因**与本改动无关**的未完成特性而失败（例如 `tests/gateway/handler`、`tests/gateway/middleware`、`tests/contract`、`test/`、`tests/controlplane/{handler,repository}`、`internal/observability/service` 等的编译/断言失败）。上线判定应以本节三条命令为准，并在发布说明中记录全量测试的真实状态。

### 1.2 真实 Redis 演练（可选但推荐，使用隔离实例）

```bash
# 严禁指向生产 Redis！用本地/隔离实例（示例端口 6399）
redis-server --port 6399 --save '' --appendonly no --daemonize yes
QUOTA_DRILL_REDIS_ADDR=127.0.0.1:6399 \
  go test ./internal/gateway/quota/ -run TestE2EDrill_LeaseConcurrency_RealRedis -count=1 -v
redis-cli -p 6399 shutdown nosave
```

该演练覆盖 6 个场景：占满限额后拒绝、租约 score = 到期时间戳、**丢失归还后退化槽位到期自愈**、重复释放幂等且键被删除、200 并发无超发、新旧前缀互不干扰。

### 1.3 关键参数确认（对照代码）

| 常量 | 期望值 | 位置 | 为什么是这个值 |
|---|---|---|---|
| `concurrencyLease` | `360s` | `internal/gateway/quota/redis_store.go` | 必须 > 上游最长超时 300s（`internal/httpclient/client.go` 的 `defaultLongTimeout`），避免合法长请求在自己的生命周期内被判过期 |
| `concurrencyKeyTTL` | `lease + 60s` | 同上 | 仅 key 级垃圾回收兜底，**不是**占用判定依据；占用判定只认成员自己的 score |
| `concurrencyKeyPrefix` | `quota:conc:lease:` | 同上 | 灰度期与旧 `quota:conc:` 键隔离，避免 `WRONGTYPE` |

### 1.4 迁移侧上线前置核对（只读，发布前必做）

```bash
# ① 迁移目录是否存在「早于目标库已应用最大版本」的新文件 ⇒ migrate-prod 必被挡（§10.1）
#    （需目标库连接串；此处只列口径，具体以 DBA 的只读查询为准）
atlas migrate status --dir "file://atlas-migrations" --url "$TARGET_DATABASE_URL"

# ② 文件数 vs version 数不等 ⇒ 存在被当作独立迁移的配对 down 文件（§10.2）
ls atlas-migrations/*.sql | wc -l
ls atlas-migrations/*.sql | sed -E 's/\.(up|down)\.sql$//' | sort -u | wc -l

# ③ atlas.sum 是否绑定他人未跟踪 WIP（§11）：须与其文件一并提交
git status --porcelain atlas-migrations/atlas.sum atlas-migrations/
```

**判定**：
- ① 若 `status` 显示存在 `Pending` 且其版本 **早于** 库内已应用最大版本 ⇒ **`migrate-prod` 会拒绝执行**（报 `was added out of order`）。**不要**在发布窗口试图绕过，转 §10.1 处置。
- ② 若文件数 ≠ version 数 ⇒ 存在配对 down 文件，**全新库会失败**；发布前确认本次发布**不会**对全新库执行迁移。转 §10.2。
- ③ 若 `atlas.sum` 为 `M` 而其绑定的迁移文件仍为 `??`（未跟踪）⇒ **必须由文件所有者在同一次提交中包含**，否则他人 checkout 会 checksum mismatch。转 §11。

---

## 2. 发布顺序与灰度注意

1. **先发布 control-plane（工作流 B 修复 + C 的迁移）**：`call_logs` 增加列属于向后兼容变更，先于 gateway 发布可保证新写入的配额拒绝字段不会丢；工作流 B 的读路径修复可独立先行发布（只读，无副作用），发布后即使 gateway 尚未升级也只是读到旧键（返回 0），不会报错。
   - 迁移由 Atlas 工作流执行，**不在本手册内自动执行**；请按 `docs/database.md` 的标准流程人工执行并在发布单登记版本号。
2. **再发布 gateway（工作流 A + C 的写入端）**：滚动/蓝绿期间新旧 gateway 并存。
   - 新实例使用 `quota:conc:lease:*`；旧实例继续使用 `quota:conc:*`。
   - 两者对同一用户各自的并发额度是**独立判定**的（灰度期的已知代价），表现为新旧实例合起来可能略超并发额度。灰度窗口应尽量短，且期间不要调高并发限额。
3. **灰度期间不要清理旧键**：等待所有旧实例下线后再执行 §4，否则会干扰仍在运行中的旧实例计数。

---

## 3. 发布后验证（只读）

### 3.1 Redis 侧

```bash
# 1) 新键应为 ZSET，且成员 score 是未来的到期时间戳
redis-cli --scan --pattern 'quota:conc:lease:*' | head
redis-cli TYPE  "quota:conc:lease:user:<userID>"        # 期望 zset
redis-cli ZCARD "quota:conc:lease:user:<userID>"        # 成员总数（含已过期未清理的）
redis-cli ZCOUNT "quota:conc:lease:user:<userID>" "(<now_ms>" +inf   # 真正的“在飞”数
redis-cli ZRANGE "quota:conc:lease:user:<userID>" 0 -1 WITHSCORES | head

# 2) 键级 TTL 只作兜底，必须长于租约
redis-cli TTL "quota:conc:lease:user:<userID>"          # 期望 ~420s 量级，不应是 0/不存在
```

判读要点：

- `ZCARD` = 集合内成员总数（含已过期但尚未清理的）。**判断"当前在飞"请用管理面读数或 `ZCOUNT`**：`ZCARD` 会偏大，属正常现象，不是泄漏。请求结束后应回落；持续不回落说明仍有在途请求或存在泄漏，后者会在租约到期后自动消失。
- 若 `ZCARD` 长时间**等于限额**且用户 429：先看该键的成员 score 是否都在未来——租约式实现下最迟 `concurrencyLease`（6 分钟）后必然自愈，**不需要人工清键**。若超过 6 分钟仍未回落，说明 Redis 侧 `EXPIRE`/时钟异常，按 §5 升级处理。

### 3.2 数据库侧（工作流 C）

```sql
-- 配额拒绝是否可按闸门/层级/限额直接定位（示例：最近 1 小时）
SELECT created_at, subject_id, quota_gate, quota_layer, quota_limit
FROM call_logs
WHERE error_code LIKE '%quota%' OR response_status = 429
ORDER BY created_at DESC
LIMIT 50;
```

期望：能看到 `quota_gate ∈ {concurrency, rpm}`、`quota_layer ∈ {subject, group, model}` 与当次判定的 `quota_limit`。

### 3.3 事故复现口径

出现「用户在低并发下仍被 429」时，按此顺序判定，不再需要反推流量分布：

1. `call_logs` 中该用户的 429 行 → `quota_gate` 是 `concurrency` 还是 `rpm`；
2. 若是 `concurrency`，看 `quota_limit` 与 `quota_layer`，再查 §3.1 的 `ZCARD` 是否真的顶到该 limit；
3. 若 `ZCARD` 明显低于 limit 而仍被拒 → 属于灰度期新旧实例各算一份，或排查后续问题（**不要**先怀疑计数器钉死，该缺陷已由租约到期自愈解决）。

---

## 4. 存量脏键清理（人工执行，灰度结束后）

### 4.1 背景

旧实现把并发计数写成「字符串计数 + TTL」：`quota:conc:<subjectType>:<id>[:<layer>:<resource>]`。
新实现完全不读写该前缀，因此这些键是垃圾数据；但它们仍占用内存，且**会让按前缀总量巡检产生误判**（看起来像新实现的泄漏）。

### 4.2 盘点（只读，先看再动）

```bash
# 旧前缀键总量与样例（生产执行前建议先在从库/备份实例演练）
redis-cli --scan --pattern 'quota:conc:*' | wc -l
redis-cli --scan --pattern 'quota:conc:*' | head -20

# 注意：新前缀也匹配 quota:conc:*，务必区分
redis-cli --scan --pattern 'quota:conc:*' | grep -v '^quota:conc:lease:' | wc -l
```

### 4.3 清理

**前置条件**：①所有旧版本 gateway 实例已下线；②已记录清理前的键数量（用于事后核对）。

```bash
# 方式一：仅清理「值为 0 或 1 的空闲计数」（更保守，需人工逐键确认）
# 若键值为 0，说明旧实例已归还完毕；非 0 的键在旧实例全部下线后也不再权威，可直接删。
redis-cli --scan --pattern 'quota:conc:*' | grep -v '^quota:conc:lease:' > /tmp/legacy_conc_keys.txt
wc -l /tmp/legacy_conc_keys.txt

# 删除（示例；生产请在维护窗口执行，并先确认上一步清单只含 quota:conc: 前缀）
while read -r k; do redis-cli DEL "$k"; done < /tmp/legacy_conc_keys.txt

# 方式二：按前缀批量删除（更快，但误伤的代价更高，务必先核对 pattern）
# redis-cli --scan --pattern 'quota:conc:*' | grep -v '^quota:conc:lease:' | xargs -r redis-cli DEL
```

> ⚠️ 严禁 `FLUSHALL` / `FLUSHDB`：Redis 同时承载 KeyCache、配置快照、限流等多类数据。
> ⚠️ 清理只针对 `quota:conc:`（旧前缀），**绝不要**删除 `quota:conc:lease:`。
>
> ⚠️ **为什么"删除在飞租约"比"留下脏键"更危险（必须理解这一点再动手）**：
> `quota:conc:lease:` 下的每个成员代表**一个正在运行的请求**所占的槽位。`DEL`/`ZREM` 掉这些成员**不会**中断对应请求——请求仍在跑、仍占用真实的上游并发——但配额闸门会**立刻认为该槽位空闲**。结果是：
> - 新请求获得槽位放行，而原请求尚未结束 ⇒ **实际并发上限被静默放大**，可超过配置的 `limit`（默认 user 20）；
> - 该放大**没有任何告警**：读侧 `ZCOUNT` 只会诚实地少算，管理面看起来"并发变低了"，反而像是"清理生效"。
> ⇒ 因此**任何**前缀通配符操作（`DEL`/`UNLINK`/`SCAN | xargs DEL`）都必须**先排除** `^quota:conc:lease:`；确需干预在飞租约时，只能按 §3.1 判定为真异常后再**人工逐键确认**，且应等待租约自然到期（最迟 6 分钟）而不是删键。

### 4.4 事后核对

```bash
redis-cli --scan --pattern 'quota:conc:*' | grep -v '^quota:conc:lease:' | wc -l   # 期望 0
redis-cli --scan --pattern 'quota:conc:lease:*' | wc -l                            # 应为当前真实占用，非 0 属正常
```

---

## 5. 回滚

| 场景 | 动作 | 后果与注意 |
|---|---|---|
| gateway 新版有问题，需回滚到旧版 | 回滚二进制/镜像即可 | 旧版重新读写 `quota:conc:*`（§4 若已清理，旧版从 0 重新计数，安全）；`quota:conc:lease:*` 变为垃圾键，可在稳定后按 §4.3 同样方式清理 |
| 仅工作流 C（拒绝字段落库）有问题 | 回滚 control-plane 或停用读取端 | `call_logs` 新增列为向后兼容，**不要**在回滚时执行 down 迁移，除非确认无任何读取方 |
| 租约自愈行为需要临时关停 | 无运行时开关 | 租约时长是编译期常量（`concurrencyLease`），如需调整必须改代码重新发布；不要把 `concurrencyLease` 调到 ≤300s（会误伤合法长请求） |

---

## 6. 已知风险与待确认项

1. **控制面只读路径（已随本批修复，上线后请核对）**：`internal/controlplane/service/quota_service.go` 的 `GetUserCurrentUsage` 原先按旧前缀 `GET`（字符串）读取并发数，新实现下恒返回 0。现已改为：
   - 键格式取自跨进程共享常量 `internal/core/quota` 的 `ConcurrencyKeyPrefix` / `ConcurrencyKey`（网关与控制面**不允许**任一侧再硬编码字面量，防止再次漂移）；
   - 读数用 `ZCOUNT key (now +inf` 统计 score 严格大于当前时间的成员 = 真正「在飞」的请求数。**不用 `ZCARD`**（会把已过期但尚未清理的成员算进去，向上虚报），也**不做 `ZREMRANGEBYSCORE`**（只读路径不得清理数据）；
   - 键不存在属正常情况，返回 0 且不告警。
   验收口径：打开管理面「用户当前并发使用量」，对同一用户在 Redis 里 `ZCOUNT "quota:conc:lease:user:<id>" "(<now_ms>" +inf` 应得到相同数字。

2. **15s 优雅退出仍会强断长流式请求**：租约化只保证「配额不会被永久钉死」，但发布/重启仍会中断 15s~300s 的长流式响应（用户侧表现为响应截断）。本批**不修改** `cmd/gateway/main.go`；根因分析、落差量化与建议见 **§8**。

3. **灰度期双份额度**：见 §2.2，新旧实例各自限额，建议缩短灰度窗口。

4. **全量 `go test ./...` 的既有失败**：与本批改动无关（详见 §1.1 提示），需要在发布判定中显式豁免并留痕。

---

## 7. 灰度后遗留核查项（速览）

> 本节是**索引**。完整机制、一手复现与处置建议见 **§10**（Atlas 遗留两项）与 **§11**（`atlas.sum` 一致性）。

发布后需要跟进、且会在同一次发布窗口被触发的遗留项（**与配额改动本身无关**）：

| # | 遗留项 | 详见 |
|---|---|---|
| 7.1 | `.up/.down` 成对文件在**两种环境**下都危险（`--exec-order non-linear` 帮不上） | §10.2 |
| 7.2 | `migrate-prod` 会被**乱序文件**挡住（prod 无 `--exec-order non-linear`） | §10.1 |
| 7.3 | `atlas.sum` 已绑定他人未跟踪 WIP，必须一并提交 | §11 |

### 7.1 Atlas 会把 `*.down.sql` 当作独立前向迁移执行（速查；完整复现见 §10.2）

**机制**：Atlas 的迁移目录以**文件**为单位（而非以 version 为单位）加载并按文件名字典序执行。因此同 version 下**配对的** `.down.sql` 与 `.up.sql` 同样会被视为两条独立的前向迁移，且字典序上 `.down.sql` < `.up.sql`（`d` < `u`），即**先执行 down、再执行 up**。

**实测口径**（reviewer 在真实目录 dry-run）：`atlas-migrations/` 下 sql 文件共 **56** 个，而不同 version 只有 **54** 个；`apply` 报告 "56 migrations in total"，与按文件计数一致 —— 可确认 Atlas 按文件而非按 version 计数。

**当前状态**：仓库中有两组「up + down 配对」的未跟踪在飞工作（`git status` 中均为 `??`，即**未被 git 跟踪**）：

| version | 文件 | 幂等保护 |
|---|---|---|
| `20260824093000` | `atlas-migrations/20260824093000_trace_archive.{down,up}.sql` | down 用 `DROP TABLE IF EXISTS` / `DROP COLUMN IF EXISTS`；up 用 `ADD COLUMN IF NOT EXISTS` 等 |
| `20260824093500` | `atlas-migrations/20260824093500_usage_settlement_daily.{down,up}.sql` | down 用 `DROP TABLE IF EXISTS`；up 使用幂等 DDL |

> 说明：截至撰写时 `atlas-migrations/atlas.sum` 为 `M`（已修改未提交），目录内另有 3 个未跟踪迁移文件（`20260902070000_*`、`20260902071500_*`、`20260910125402_add_call_logs_quota_rejection_fields.sql`，后者由本团队的 t2 引入）。它们与本节的 down 配对风险无关，此处仅说明仓库状态，避免读者把两类未跟踪文件混为一谈。

**为何目前未造成破坏（⚠️ 此结论已被 §10.2 的一手复现推翻，务必按 §10.2 为准）**：

> 🔴 **本节以下几句已被本次复核证伪，保留仅为留痕，不得据此行动**：
> ~~「`20260824093000` 因 down 之后紧跟的 up 会**重建**表而侥幸通过」~~——隔离复现显示**该 version 的 up 语句一条都没执行**，不存在「重建表」这一步；
> ~~「`20260824093500` 的 up **只建索引、不建表**」~~——该 up 文件**确实含完整 `CREATE TABLE IF NOT EXISTS`**（`grep -ciE 'create table'` = 1）；
> ~~「最终仅记录 50 条」~~——本次复核实测写 revision 因事务中止（`25P02`）而失败，**记录条数 ≤ 51，不是 50**。
> 正确的现象描述与原始输出**一律以 §10.2 为准**。

截至本次复核，可复用的结论只有：**同一 version 的 `.down.sql` 与 `.up.sql` 共存时，down 的语句照常执行而 up 的语句未完整执行**（成因未定，见 §10.2）；两条配对链**都会中断**，表/列被真实删除。**决定性反事实**：把 2 个 `.down.sql` 移出并重新 `atlas migrate hash` 后，全新库从 0 应用 **54 条全部通过**。⇒ 该组合在两种环境**都**危险，`--exec-order non-linear` **帮不上**。

注意 down 里确实包含**破坏性 DROP**（`20260824093000_trace_archive.down.sql`：1 条 `DROP TABLE IF EXISTS trace_archive_manifests` + 1 条 `DROP INDEX IF EXISTS idx_call_traces_archstatus_ts` + 1 条 `ALTER TABLE call_traces DROP COLUMN IF EXISTS`（**同一语句内删 5 个子列**）；`20260824093500_usage_settlement_daily.down.sql`：1 条 `DROP TABLE IF EXISTS usage_settlement_daily`）。

**风险**：一旦出现以下任一情况，apply 时会真实丢数据：
- **全新库路径**（已实测）：down 先删表、up 不重建 ⇒ 迁移链中断，且**结构已被删掉**；
- 只放 `.down.sql` 而没有配对的 `.up.sql`（结构被删且不会被重建）；
- down 中含**不可逆** DROP（当前两组只删表/列结构，但 `DROP TABLE` 本身在无 up 时即数据丢失）。

**归属与处置建议（重要）**：
- 这两组文件是**仓库使用者自己的未跟踪在飞工作**（`git status` 显示为 `??`，未被 git 跟踪），**不属于本次配额改动的范畴**。
- 建议由**该工作的所有者**确认是否将 up/down 合并为单文件（或移出 Atlas 扫描目录 / 改名使其不被当作前向迁移），再决定是否提交。
- **本团队不修改这两组文件**，发布流程也**不应**在本次发布中执行它们的 down 脚本。

**上线前核对命令（只读）**：

```bash
# 1) 文件数 vs version 数：两者不等即说明存在配对的 down 被当作独立迁移
ls atlas-migrations/*.sql | wc -l
ls atlas-migrations/*.sql | sed -E 's/\.(up|down)\.sql$//' | sort -u | wc -l

# 2) 列出所有 up/down 配对（这些 version 都可能被 down→up 各执行一次）
for v in atlas-migrations/*.up.sql; do d="${v%.up.sql}.down.sql"; [ -f "$d" ] && echo "配对: $(basename "$v")"; done

# 3) 确认 down 文件里是否含破坏性 DDL（人工判断能否在无 up 的情况下执行）
grep -rinE "drop (table|column|schema)" atlas-migrations/*.down.sql | head
```

> ⚠️ 本节仅用于留痕与上线前核对，**不要**据此修改 `atlas-migrations/` 下的任何文件，也**不要**在发布中单独执行 down 脚本。

---

## 8. 根因复盘：15s 优雅关闭 vs 300s 上游超时（本缺陷的运维后果）

### 8.1 缺陷链条（一手代码证据）

| 环节 | 事实 | 位置 |
|---|---|---|
| ① 计数方式 | 旧实现并发计数是**裸 Redis `INCR`/`DECR`**（字符串计数），归还依赖 handler 的 `defer` | 旧 `internal/gateway/quota/redis_store.go` |
| ② 关闭窗口只有 15s | `context.WithTimeout(context.Background(), 15*time.Second)` 后立刻 `httpServer.Shutdown` | `cmd/gateway/main.go:316-317` |
| ③ 上游最长可到 300s | `defaultLongTimeout := 300 * time.Second`（含 `HTTP_TIMEOUT`、`HTTP_RESPONSE_HEADER_TIMEOUT`，均可被环境变量覆盖） | `internal/httpclient/client.go:73,78,82` |
| ④ 结果 | 15s 关闭窗口内无法排空 60~300s 的长流式请求；`Shutdown` 超时后进程退出，**被强断请求的 `DECR` 永久丢失** | ② + ③ |
| ⑤ 兜底失效（关键） | 唯一兜底是 10 分钟**键级 TTL**，而该 TTL 在**每次成功获取时都被 `EXPIRE` 续期** | 旧实现 |
| ⑥ 用户后果 | 只要用户活跃（持续有新请求成功获取），键 TTL 被不断续期 ⇒ 虚高的计数值**永不衰减**；一旦虚高值顶到限额（默认 user 20），用户被**持久锁死**——表现为「低并发下持续 429」 | ⑤ |

> ⚠️ **本缺陷的危险之处在于 ⑤**：租约化解决的是「丢失的归还自动过期」。若只做 ①→④ 的分析就以为「TTL 会兜底」，会得出**错误结论**——旧实现的 TTL 是被续期的，恰好在最需要它的时候（用户活跃期）永不生效。

### 8.2 落差量化（含第二段 drain，此前遗漏）

关闭路径实际有**两段**独立超时，**串行**执行：

| 阶段 | 超时 | 代码位置 | 作用 |
|---|---|---|---|
| ① HTTP 优雅关闭 | **15s** | `cmd/gateway/main.go:316` `context.WithTimeout(..., 15*time.Second)` → `httpServer.Shutdown` | 等在途 HTTP 请求排空 |
| ② 采集器 drain | **8s** | `cmd/gateway/main.go:327` `context.WithTimeout(..., 8*time.Second)` → `collector.Wait` / `budgetAlertDispatcher.Wait` | 等 call log 写入与预算告警派发落盘 |

- 上游最长响应：**300s**（默认，且 `HTTP_TIMEOUT` 可被调高 ⇒ 实际落差可**大于** 285s）。
- 安全下限：① 必须 **≥ 最长上游超时**，否则任何一次发布/重启都会强断长流式请求。
- ⚠️ **总排空预算 ≈ ① + ②**（当前 `15s + 8s`）。把 ① 提到 ≥300s 后总时长约 `300s + 8s`。若新增 `GATEWAY_SHUTDOWN_TIMEOUT`，需明确它指 ① 还是 ①+②；**建议定义为「① ≥ 最长上游超时」，并保留 ② 的 8s 独立**（② 不在请求路径上，不受上游超时影响）。
- ⚠️ `httpServer.Shutdown` 的语义**不是「到点强断」**：它在 ctx 到期时立即返回 `ctx.Err()` 并*不*释放仍在使用的连接，由随后进程退出收尾。因此被截断的流式请求**其 `defer` 归还逻辑不会执行**——这正是旧实现 `DECR` 丢失的成因。
- 与租约的关系：租约 `concurrencyLease = 360s > 300s`（见 §1.3），保证**合法长请求不会在自己的生命周期内被判过期**；但租约只保证「配额终会自愈」，**不保证请求不被强断**（用户侧仍表现为响应截断）。

### 8.3 建议（需单独决策，本批未改 `cmd/gateway/main.go`）

1. **首选**：把 ① 的关闭等待调为 **≥ 最长上游超时**（绝对值建议 `310s`，即 300s + 10s 余量），并做成**可配置**项（如 `GATEWAY_SHUTDOWN_TIMEOUT`），避免与 `HTTP_TIMEOUT` 漂移——两者分别硬编码是当前的根本隐患。
2. **配套**：若 `HTTP_TIMEOUT` 被调高，关闭等待必须同步调高；建议加一条**启动期自检或 CI 断言**：`shutdownTimeout >= httpTimeout`，违反即显著告警（与本批 `lease_guard.go` 校验租约覆盖上游超时的思路一致）。
3. **权衡**：关闭等待变长会显著延长发布/重启的摘流时间（① 从 15s 提到 310s ⇒ 最坏情况每次重启多等近 5 分钟）。折中做法是「先摘流（从负载均衡摘除，不再接新请求）→ 再等待在途请求排空」，而不是单纯把 `Shutdown` 超时调大。若采用该折中，需在部署文档中同步说明。
4. **「可配置」当前不成立（一手核证）**：全仓**不存在**任何关闭超时配置项——`cmd/gateway/main.go` 两处均为字面量；`internal/gateway/` 下无 `SHUTDOWN_TIMEOUT` / `shutdownTimeout` / `ShutdownTimeout` 命中；`.env.example` 亦无该项（仅有无关的 `CATALOG_HTTP_TIMEOUT=15s`）。⇒ **必须新增配置项**才能满足本节建议，不能假定它已可配。

---

## 9. 迁移目录唯一权威与 Atlas 报数口径

### 9.1 `atlas-migrations/` 是唯一生效目录

- **唯一权威**：`atlas.hcl` 中四个 env（`local`/`dev`/`test`/`prod`）的 `migration { dir = "file://atlas-migrations" }` 全部只认 `atlas-migrations/`。
- **CI 只认它**：`.gitlab-ci.yml` 的路径触发为 `atlas-migrations/**/*`（第 42 行），且 `migrate-prod` / `migrate-test` 的 `--dir` 与 `scp` 目标都只搬 `atlas-migrations`。
- **`migrations/` 是历史遗留**：仍有 **45** 个被 git 跟踪的文件，但 **CI 不认、新增进去不会生效**。
- ⚠️ **最危险的误操作**：按 `AGENTS.md` 的旧指引，用 `migrations/NNN_*.sql`（递增序号）命名新迁移——若把它放进 `atlas-migrations/`，数字序号**不符合 `20060102150405` 时间戳格式**，会直接制造版本乱序或使 Atlas 报 `invalid version`。
- 📌 **`AGENTS.md` 的指引已过期**：第 49 行「`migrations/`｜Postgres SQL 迁移（按序号 001..NNN）」与第 114 行「新增 `migrations/NNN_*.sql`（递增序号）」均已不符合现状，**需由仓库所有者单独修正**（本批不改 `AGENTS.md`）。

### 9.2 报数口径：必须给三元组，并用「Executed + Pending」作对账轴

**报数必须写成三元组**（唯一被采纳的写法）：

```
目标库已应用集 + 被检查目录 + N/文件名清单
```

**对账轴（本节的报数基准）**：

```
Executed + Pending = 被检查目录中的文件数
```

- `Executed` 属「**目标库已应用集**」（来自 revisions 表，另加 `--baseline` 声明的起点）。
- `Pending` 才是「**目录有而库未应用**」。
- ⚠️ **但该式的右端必须先声明计数口径**：它只在「按文件计数」这一口径下成立（见下「口径警示」）。**报数时不得只给 `Executed`/`Pending` 两个数**——同一个 `Pending` 在不同目标库上含义完全不同（库 A 已应用 52 条 vs 库 B 全新库）。**必须同时给出被检查目录与文件数**。
- ⚠️ **`Pending` 不是「未应用项清单」的权威来源**：要列未应用项，请**按 `version` 前缀对「目录文件」与「revisions 表」自行求差集**。

> ⚠️ **口径警示（不得省略）**：上式**不是无条件恒等式**——其**右端的计数口径未定**（按文件 vs 按 version）。两种口径都已被独立实测到：
>
> - 按**文件**计数：下表 5 组读数**全部符合**（`1+55=56`、`52+4=56`、`52+0=52`、`50+6=56`、`50+0=50`）；
> - 而另一处独立实测得到 `Executed 52 / Pending 2`，按文件为 `52+2=54 ≠ 56`。该值恰与「56 个文件 = 54 个 version + 2 个 `.down`」吻合——**这只提示一种可能解释（Atlas 按 version 计数），并未证实**。
>
> ⇒ 两组前提至少有一处不同，**根因未定**。因此：**要使用该式对账，必须先声明右端是「文件数」还是「version 数」，并写明被检查目录**；否则不得据它下结论，**更不得据 `Pending` 反推**未应用项。下表为**已实测读数记录**，仅在本口径声明下可用。

| 场景 | Executed | Pending | 目录文件数 | 备注 |
|---|---|---|---|---|
| 全新库 + `--baseline 20260609082623` | 1 | 55 | 56 | 合 `1+55=56` |
| 报称「库已应用 52」 | 52 | 4 | 56 | 合 `52+4=56` |
| 迁移目录仅 52 个文件 | 52 | 0 | 52 | 合 `52+0=52` |
| 迁移目录 56 个文件、库已应用 50 | 50 | 6 | 56 | 合 `50+6=56` |
| 迁移目录仅 50 个文件 | 50 | 0 | 50 | 合 `50+0=50` |
| **另一处独立实测** | 52 | 2 | 56 | **不合**（`52+2=54`）⇒ 右端口径存疑 |

> 上表第一行由本手册作者复现：`atlas migrate status --dir file://atlas-migrations --url <fresh>` 输出
> `Executed Files: 1 / Pending Files: 55`，而 `ls atlas-migrations/*.sql | wc -l` = `56`；其余四组来自本轮核查记录（口径同一），最后一行为独立来源的反例。
>
> ⚠️ 注意 `atlas-migrations/` 下 **56 个文件 ≠ 54 个 version**（差异来自 §10 的两组 `.up`/`.down` 配对）。**报数时务必写清是「文件数」还是「version 数」**。

---

## 10. 灰度后遗留核查项（分两条独立记录）

本节两项**与配额改动本身无关**，但会在同一次发布窗口被触发，故在此留痕。⚠️ **本团队不修改 `atlas-migrations/` 下任何文件**。

### 10.1 遗留项甲：`migrate-prod` 会被乱序文件挡住（prod 无 `--exec-order non-linear`）

**现象**：`migrate-prod`（`.gitlab-ci.yml:287`）执行时报

```
Error: migration file <某个较早版本>.sql was added out of order.
```

**机制（本手册作者在一手复现中确认）**：Atlas 默认按**线性**顺序应用迁移。若「库已应用的最大版本」**大于**「目录中新出现文件的版本」，即该文件是**后补的乱序文件**，线性模式直接**拒绝**执行。

**对照复现（隔离 Postgres 18.3，全部为一次性库，未接触任何真实环境）**：

```text
步骤1：目录含 V1(20260609082623_baseline) + V3(20260611000000)，应用到目标库 → 已应用 2 条
步骤2：目录中补入乱序的 V2(20260610120000_add_model_status)，V2 < 已应用最大版本 V3
  ├─ linear（migrate-prod 默认）      → Error: migration file 20260610120000_add_model_status.sql
  │                                      was added out of order.
  └─ non-linear（migrate-test 用）    → ok (1 migration, 9 sql statements)
```

**结论**：
- `--exec-order non-linear` **能**解决「已应用历史 + 后补乱序文件」这一类问题，这正是 `migrate-test`（`.gitlab-ci.yml:323`）加上它、而 `migrate-prod`（第 287 行）**没加**的原因。
- ⇒ 只要目录里存在「版本早于目标库已应用最大版本」的新文件，**`migrate-prod` 必定失败**，且失败发生在**任何 SQL 执行之前**（安全，但会阻塞发布）。
- ⚠️ **闸门是动态的**：同一个文件，在「已应用历史较少的库」上能过、在「已应用历史较多的库」上被挡——因此**不能用一次成功来推定安全**。
- **处置建议（需所有者决策，本团队不动文件）**：给 `migrate-prod` 也在确认审计后加 `--exec-order non-linear`，或把这些乱序文件从目录中移出、由所有者重新排序后提交。

### 10.2 遗留项乙：`.up/.down` 成对文件在**两种环境**下都危险（`--exec-order non-linear` 帮不上）

**机制**：Atlas 的迁移目录**以文件为单位**（而非以 version 为单位）加载，并按**文件名字典序**执行。因此同一 version 下配对的 `.down.sql` 与 `.up.sql` 会被当成**两条独立的前向迁移**，且字典序 `d < u` ⇒ **先执行 down、再执行 up**。

**实测口径**：`atlas-migrations/` 下 sql 文件共 **56** 个，而不同 version 只有 **54** 个；`apply` 报告 "56 migrations in total"，与按文件计数一致。当前配对两组，**各自 down 中的破坏性语句（逐条核证）**：

| version | 文件 | down 中的破坏性语句 |
|---|---|---|
| `20260824093000` | `..._trace_archive.{down,up}.sql` | `DROP TABLE IF EXISTS trace_archive_manifests` + `DROP INDEX IF EXISTS idx_call_traces_archstatus_ts` + 1 条 `ALTER TABLE call_traces DROP COLUMN IF EXISTS`（**含 5 个子列**） |
| `20260824093500` | `..._usage_settlement_daily.{down,up}.sql` | `DROP TABLE IF EXISTS usage_settlement_daily` |

**为何两种环境都危险（关键复现，本手册作者一手实测）**：

**情形 A：全新库 + 声明 baseline** ⇒ 卡在 baseline 前置依赖

```text
$ atlas migrate apply --dir file://atlas-migrations --url <fresh> --baseline 20260609082623
Migrating to version 20260910125402 from 20260609082623 (55 migrations in total):
  -- migrating version 20260610120000
    -> ALTER TABLE models ADD COLUMN status INTEGER NOT NULL DEFAULT 1;
    pq: relation "models" does not exist (42P01)
Error: ... 1 migration with errors
```

**根因**：`--baseline` 的作用是**把该版本及其之前视为已应用并跳过**（本次实测 `atlas migrate status` 报 `Executed Files: 1 / Pending Files: 55`）。因此 `20260609082623_baseline.sql`（420 行，**确实创建 `models` 表**）**根本不会被执行**，后续 `ALTER TABLE models` 立刻失败。⇒ **`--baseline` 只适用于「目标库已具备 baseline 结构」的既有库；对全新库使用必然失败。**

**情形 B：全新库 + 不声明 baseline（从 0 应用全部）** ⇒ 被 `.down.sql` 打中

真实目录（56 文件）从 0 应用，**实测原始输出**（本次复核重跑，2026-09-10，隔离 Postgres 18.3 一次性库）：

```text
  -- migrating version 20260824093000
    -> DROP TABLE IF EXISTS trace_archive_manifests;                       ← down
    -> CREATE INDEX IF NOT EXISTS idx_manifest_state ON trace_archive_manifests (state);
    -> ...
  -- migrating version 20260824093500
    -> DROP TABLE IF EXISTS usage_settlement_daily;                        ← down
  -- migrating version 20260824093500
    -> CREATE INDEX IF NOT EXISTS idx_settlement_statdate ON usage_settlement_daily (stat_date);
       pq: relation "usage_settlement_daily" does not exist (42P01)
  -- 51 migrations ok, 1 with errors
  -- 274 sql statements ok, 1 with errors
Error: sql/migrate: executing statement "CREATE INDEX IF NOT EXISTS idx_settlement_statdate
  ON usage_settlement_daily (stat_date);" from version "20260824093500": pq: ... 42P01
sql/migrate: write revision: pq: current transaction is aborted ... (25P02)
⇒ 执行后 usage_settlement_daily 表**不存在**（information_schema 计数 = 0）
```

⚠️ **本条与更早记录的差异**：早前记录称「51 ok / 1 error → 最终仅记录 50 条」，本次复核实测为 **ok 51 / error 1、写 revision 因事务已中止而失败**（`25P02`），故**最终记录条数 ≤ 51，并非 50**。报数请以当次实测输出为准，不要沿用旧数字。

**⚠️ 本手册此前给出的两处「机制解释」经复核**为**不成立**，已撤回：

| 已撤回的说法 | 复核结论 |
|---|---|
| 「`20260824093500` 的 up **只建索引、不建表**，所以暴露失败」 | **错。** `grep -ciE 'create table'` 该 up 文件 = **1**，其第 4–34 行是完整的 `CREATE TABLE IF NOT EXISTS usage_settlement_daily (...)`（含 7 列复合主键），其后才是 3 条 `CREATE INDEX`。**该文件并非「只建索引」。** |
| 「`20260824093000` 因为 down 后面紧跟的 up 会**重建**表，所以**侥幸通过**」 | **错（在隔离环境下）。** 单独把该 version 的 down+up 放进空目录跑全新库，**up 的语句一条都没出现**：实测只有 down 的 3 条（`DROP TABLE` / `DROP INDEX` / `ALTER TABLE call_traces DROP COLUMN`），即报 `Error: ... from version "20260824093000"`，`1 migration with errors / 2 sql statements ok, 1 with errors`，执行后 `trace_archive_manifests` 表**不存在**。**并不存在「up 重建表」这一步。** |

⇒ 正确的可复用结论只有一句（**只陈述实测现象，不解释成因**）：
> **同一 version 的 `.down.sql` 与 `.up.sql` 同处一个目录时，down 的语句照常执行，而 up 的语句序列并未完整执行**（`20260824093500` 仅见其第 1 条 `CREATE INDEX`；`20260824093000` 则一条未见）。两条配对链**都会中断**，表/列被真实删除。

**同一文件单独运行则完全正常（关键反证）**：把 `20260824093500_usage_settlement_daily.up.sql` **单独**放进空目录跑全新库：

```text
  -- migrating version 20260824093500
    -> CREATE TABLE IF NOT EXISTS usage_settlement_daily (
    -> CREATE INDEX IF NOT EXISTS idx_settlement_statdate ON usage_settlement_daily (stat_date);
    -> CREATE INDEX IF NOT EXISTS idx_settlement_tier    ON usage_settlement_daily (budget_tier_id, stat_date);
    -> CREATE INDEX IF NOT EXISTS idx_settlement_model   ON usage_settlement_daily (model, stat_date);
  ⇒ 4 条语句全执行、0 错误、usage_settlement_daily 表存在
```

⇒ **up 文件本身是自洽、可成功应用的**；失败只在它与同 version 的 down 文件**共存**时出现。

> ⚠️ **诚实边界：根因仍未定。** 上述「up 的语句未完整执行」是**可复现的实测现象**；其**成因（Atlas 在配对场景下的语句解析/执行语义）本手册未查明**，此前试图解释它的两种说法均已证伪（见上表）。**上机处置时请按现象而非成因行动**：不要依赖「down 后面会跟一个 up 把结构重建回来」这一假设。

**决定性反事实（证明元凶就是 down 文件）**：把 2 个 `.down.sql` 临时移出后重新 `atlas migrate hash`，全新库从 0 应用：

```text
-- 54 migrations ── 295 sql statements        （EXIT=0）
已应用迁移数 = 54；usage_settlement_daily 表存在
```

⇒ **移出 down 文件后全新库完全通过**；保留则必然失败。**这才是本项唯一可据以行动的结论。**

**`--exec-order non-linear` 帮不上（已实测）**：对情形 B 用 `--exec-order non-linear` 重跑，**报错完全相同**（同一句 `CREATE INDEX ... usage_settlement_daily` 的 `42P01`）。因为失败发生在**同 version 内的文件执行**层面，与**版本之间**的执行顺序无关。

**风险与归属**：
- 这 4 个文件（2 version × up/down，`git status` 显示为 `??`）是**仓库使用者自己的未跟踪在飞工作**，**不属于本次配额改动**。
- 一旦出现「只有 down 没有配对 up」，apply 时会**真实丢数据**。
- **处置建议**：由所有者确认是否将 up/down 合并为单文件，或移出 Atlas 扫描目录 / 改名使其不被当作前向迁移，再决定是否提交。**本团队不修改**，发布流程也**不应**执行它们的 down 脚本。

**上线前核对命令（只读）**：

```bash
# 1) 文件数 vs version 数：两者不等即说明存在配对的 down 被当作独立迁移
ls atlas-migrations/*.sql | wc -l
ls atlas-migrations/*.sql | sed -E 's/\.(up|down)\.sql$//' | sort -u | wc -l

# 2) 列出所有 up/down 配对
for v in atlas-migrations/*.up.sql; do d="${v%.up.sql}.down.sql"; [ -f "$d" ] && echo "配对: $(basename "$v")"; done

# 3) 确认 down 文件里是否含破坏性 DDL
grep -rinE "drop (table|column|schema)" atlas-migrations/*.down.sql | head
```

---

## 11. 上线前置条件：`atlas.sum` 的一致性

`atlas.sum` 当前为 **`M`（已修改未提交）**。其中已包含 4 条配对文件的条目：

```bash
grep -cE "trace_archive|usage_settlement_daily" atlas-migrations/atlas.sum   # = 4
```

⚠️ **实际情况比"4 条"更严重**：`atlas.sum` 已绑定 `atlas-migrations/` 下**全部 7 个未跟踪文件**（逐条实测，**7/7 均命中**）：

| # | 被 `atlas.sum` 绑定的未跟踪文件 | 归属 |
|---|---|---|
| 1–4 | `20260824093000_trace_archive.{down,up}.sql`、`20260824093500_usage_settlement_daily.{down,up}.sql` | **他人 WIP**（危害见 §10.2） |
| 5–6 | `20260902070000_add_user_daily_stats_derived_columns.sql`、`20260902071500_add_user_period_stats_baseline_quantiles.sql` | **他人 WIP** |
| 7 | `20260910125402_add_call_logs_quota_rejection_fields.sql` | **本团队 t2 引入**（按 t2 交付节奏提交） |

核对命令（只读）：

```bash
git status --porcelain atlas-migrations/ | grep '^??' | sed 's/^?? //' | while read f; do
  b=$(basename "$f"); grep -q "$b" atlas-migrations/atlas.sum && echo "已绑定: $b" || echo "未绑定: $b"
done
```

- ⚠️ **`atlas.sum` 已绑定这些未跟踪 WIP 文件**。若「本次改动」只提交 `atlas.sum` 而**不一并提交**其对应的 WIP 文件，他人 checkout 后会因**缺少文件**而 checksum mismatch。
- ⇒ **发布前置条件**：涉及 `atlas.sum` 的所有者必须把**其对应的全部迁移文件与 `atlas.sum` 一并提交**（上表 1–6 由各自所有者负责，第 7 项由本团队负责），否则该 CI 步骤（`.gitlab-ci.yml:286` 的 `atlas migrate hash` 之后的 apply）会对所有人失败。
- **已实测的连带影响**：仅把 `.down.sql` 移出目录而**不重跑** `atlas migrate hash`，会立刻得到
  `L50: ..._trace_archive.down.sql was removed ... Error: checksum mismatch`
  ⇒ 任何对迁移目录的增删都必须配套 `atlas migrate hash`。
- **本次发布不应包含的提交**：上表第 1–6 项（**他人 WIP，本团队不提交**）。第 7 项属本团队 t2 的工作，按 t2 交付节奏提交。

---

## 12. 未验证项（诚实边界）

以下内容**未经端到端验证**，不得据此宣称保障已完成：

1. **`--baseline` 的既有库 / prod 路径未验证**：本手册只验证了「全新库 + baseline ⇒ 必然失败」与「既有库 + 乱序文件 ⇒ migrate-prod 被挡」。**真实 prod 库**（已应用历史 + `ATLAS_BASELINE=20260609082623`）上 `migrate-prod` 是否通过，**未验证**——按 §10.1 的结论，若目录存在早于库内最大版本的文件则会被挡。
2. **长请求保活仅由常量关系与注入时钟证明**：`concurrencyLease(360s) > defaultLongTimeout(300s)` 是**常量关系**推理，加上单元测试中的**注入时钟**验证租约到期自愈。**没有**真实 300s 长流式请求 + 真实发布重启的端到端演练。⇒ §8.2 的落差结论成立，但「发布时用户侧截断」的具体比例未测。
3. **M1 运行时不变式仍开放**：租约时长必须覆盖上游超时这一不变式，已由 `internal/gateway/quota/lease_guard.go` 的**启动期校验 + 显著告警**承载（t19），但**运行时**是否会被动态配置破坏（如 `HTTP_TIMEOUT` 被调到 >360s 而无人察觉）**仍为开放项**——当前只在启动时校验，运行中改环境变量需重启才生效。
4. **2 个 `//go:build integration` 测试文件没有执行入口**：全仓检索 `tags=integration` 在 CI 与 `make test` 主线**零命中**（`make test` 只覆盖 `./tests/...`，且不带该 tag）。因此**「配额拒绝字段端到端落库 PG」当前没有自动化保障**，只能靠 §3.2 的人工 SQL 核对。⇒ 这是**观测能力交付**（工作流 C）的主要验证缺口，建议后续补一条带 tag 的 CI job。
5. **`tests/integration` 另有 Docker 依赖**：`go test ./tests/... -tags=integration` 需要 Docker/DB 就绪，本手册未执行。

---

## 13. 本次复核修订记录（2026-09-10）

本节记录本手册的**修订范围、被证伪的说法与复核所用的一次性环境**，供审查与上机前核对。**所有复核均为只读 + 一次性库，未接触任何真实环境，未修改任何他人 WIP 迁移文件。**

### 13.1 复核改动清单

| 节 | 改动 | 依据 |
|---|---|---|
| §7.1 | 划掉三处已被证伪的旧解释（「侥幸通过」/「只建索引不建表」/「仅记录 50 条」），改为指向 §10.2 | 见 §13.2 |
| §8.2 | 补入**第二段 drain（8s）**，明确 ①+② 串行、总排空预算 ≈ ①+②；补 `Shutdown` 到期语义（不释放连接 ⇒ `defer` 不执行） | `cmd/gateway/main.go:316`、`:327` |
| §8.3 | 新增第 4 条：**「可配置」当前不成立**的核证 | 全仓无 `SHUTDOWN_TIMEOUT` 命中；`.env.example` 无该项 |
| §9.2 | 把 `Executed + Pending = 被检查目录文件数` 恢复为**显式对账轴**，同时**强制声明右端口径**（文件数 vs version 数）并保留反例 | 见 §13.3 |
| §10.2 | 用**本次复跑的原始输出**替换旧输出；把两处错误机制解释列入「已撤回」表；新增「同一文件单独运行则完全正常」的关键反证；**明确标注根因仍未定** | 见 §13.2 |
| §10.2 | 修正 down 语句计数（`ALTER TABLE ... DROP COLUMN IF EXISTS` 是 **1 条语句含 5 个子列**，非「5 条」） | 逐行核证 down 文件 |

### 13.2 本次一手复现命令与结果（可复算）

环境：本机 Homebrew PostgreSQL **18.3**、Atlas **v1.2.3-6633e29-canary**；全部使用一次性库 `t14_*`，复核后已全部删除（剩余探针库 = 0、`/tmp` 副本 = 0）。

```bash
# ① 全新库 + 真实目录（56 文件）从 0 应用
atlas migrate apply --dir "file://atlas-migrations" --url "postgres://…/t14_probe?sslmode=disable"
#   ⇒ 20260824093500 处：down 的 DROP TABLE 执行 → 报 42P01（CREATE INDEX ... usage_settlement_daily）
#   ⇒ 51 migrations ok, 1 with errors；274 sql statements ok, 1 with errors
#   ⇒ 执行后 usage_settlement_daily 表存在数 = 0

# ② 只放该 version 的 down+up 到空目录，全新库应用
#   ⇒ 仅见 down 的 DROP TABLE 与 up 的第 1 条 CREATE INDEX；1 ok / 1 error
#   ⇒ up 文件中的 CREATE TABLE 与另 2 条 CREATE INDEX **未出现在执行序列中**

# ③ 只放 up 文件（不配 down）到空目录，全新库应用
#   ⇒ 4 条语句全执行（CREATE TABLE + 3 条 CREATE INDEX），0 错误，表存在
#   ⇒ 证明 up 文件本身自洽可应用

# ④ 只放 trace_archive 的 down+up 到空目录，全新库应用
#   ⇒ 仅见 down 的 3 条（DROP TABLE / DROP INDEX / ALTER TABLE DROP COLUMN）
#   ⇒ 1 migration with errors, 2 sql statements ok, 1 with errors；表存在数 = 0

# ⑤（前轮记录、保留）把 2 个 .down.sql 移出并重新 hash 后，全新库从 0 应用 54 条全部通过
```

**结论（唯一可据以行动者）**：**同一 version 的 `.down.sql`/`.up.sql` 共存时，down 照常执行、up 的语句未完整执行**；**成因未定**，故本手册**不提供成因解释**，只提供「不要依赖 down 后会有 up 把结构重建回来」这一处置禁则。

### 13.3 报数口径的本次核证

```
被检查目录 = atlas-migrations/     文件数 = 56     version 数 = 54
差异来源   = 2 组 .up/.down 配对（§10.2）
```
⇒ 报数时**必须**写清 `56` 是文件数还是 version 数。§9.2 的 `Executed + Pending` **只在「按文件计数」口径下**与 `56` 对得上（已实测 5 组自洽：`1+55`、`52+4`、`52+0`、`50+6`、`50+0`），而另有一组独立实测 `52+2=54` 与之矛盾 ⇒ **口径未定，不得无条件当作恒等式**。

### 13.4 未纳入本次复核的范围

- §10.1 的乱序文件（`migrate-prod` 被挡）**未在本次重跑**，沿用前轮记录（隔离库对照复现）。
- §12 各项未验证项**未新增验证**，仍按原样标注。
- 本次**未修改** `atlas-migrations/` 下任何文件（含 6 个他人未跟踪 WIP 文件）与 `AGENTS.md`。
