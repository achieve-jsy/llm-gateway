# 四层配额系统 稳定性 / 竞态 / 集成测试报告

- **范围**：`internal/gateway/quota/`（四层配额执行路径）+ 关联 `internal/controlplane/{service,cron}`
- **测试补强**：新增 `internal/gateway/quota/stability_test.go`（16 个用例）
- **执行日期**：2026-07-02
- **基线**：`feat/layered-quota` 分支，commit `12f6992` 之后
- **Go 版本**：go1.26.1；`github.com/alicebob/miniredis/v2 v2.38.0`、`github.com/redis/go-redis/v9 v9.19.0`

> **更新（2026-07-02）**：BUG-1（model 层并发令牌泄漏）**已修复**。修复方案见 §4，
> 对应用例已从 `TestStability_KnownBug_ModelConcurrencyToken_Leaks`（assert bug 行为）
> 重构为 `TestStability_ModelConcurrencyToken_Released`（assert 修复后行为：请求结束后
> model 并发计数归零，第 3 次请求 200）。`go test -race` 全绿。

---

## 1. 执行结果摘要

| 命令 | 结果 | 备注 |
|---|---|---|
| `go test ./internal/gateway/quota/... -race -count=1` | **PASS** | 64 个用例全绿，无 data race，耗时 ~25s |
| `go test ./internal/controlplane/service/... -race -count=1` | **PASS** | 耗时 ~2.1s |
| `go test ./internal/controlplane/cron/... -race -count=1` | **PASS** | 耗时 ~1.2s |
| 端到端 HTTP（运行中系统，§8） | **PASS**（subject 层 RPM/并发/豁免三场景） | curl + redis-cli + psql 实测，真实 gateway:8080 链路 |

- 配额包用例总数 64（既有 48 + 新增 16），**0 FAIL**。
- 新增 16 个稳定性用例。其中 model 层并发令牌释放用例（`TestStability_ModelConcurrencyToken_Released`）在发现 BUG-1 后已随修复一起翻转断言，现 assert 修复后行为。
- `-race` 下并发读写 SnapshotHolder 与 1000+ 并发 Acquire/Release/CheckRPM 均无告警。

---

## 2. 测试矩阵（四层 × 两门 × 场景）

四层 = `user` / `service_key` / `group` / `model`；两门 = RPM / 并发。
单元格为**新增用例**名（既有单元用例见 `redis_store_test.go` / `middleware_test.go`，不重复列）。

| 场景 \ 层 | user | service_key | group | model | 跨层/系统 |
|---|---|---|---|---|---|
| RPM 触顶→窗口过期恢复 | TestStability_RPM_RejectThenWindowRecovery（端到端+边界 30s/61s） | TestStability_FourLayers_ServiceKeySubject_Independent | TestStability_FourLayers_IndependentCounting | — | — |
| 并发触顶→Release 恢复 | TestStability_Middleware_Concurrency_ReleaseMidFlight | — | TestStability_FourLayers_HappyPath_WithinLimits | TestStability_ModelConcurrencyToken_Released（BUG-1 修复后回归） | TestStability_Concurrency_FillRejectReleaseRecover / _OverRelease_NeverNegative |
| 四层叠加独立计数 | TestStability_FourLayers_IndependentCounting | TestStability_FourLayers_ServiceKeySubject_Independent | TestStability_FourLayers_IndependentCounting | TestStability_FourLayers_IndependentCounting | — |
| 豁免跳过不计数 | TestStability_Exemption_DoesNotTouchCounters | （同语义，由 middleware 豁免逻辑覆盖） | — | — | — |
| fail-open 全场景 | TestStability_FailOpen_RedisDown_ConcurrentAndRelease | — | — | — | TestStability_Store_RedisDown_NoPanic |
| 快照热更新原子性 | TestStability_SnapshotHotUpdate_ImmediateEffect | — | — | — | TestStability_SnapshotHolder_ConcurrentReadWriteRace (-race) |
| limit=0 vs 缺省继承 | TestStability_LimitZero_vs_MissingInherit | TestStability_LimitZero_vs_MissingInherit | TestStability_LimitZero_vs_MissingInherit | TestStability_LimitZero_vs_MissingInherit | — |
| 大规模并发压测（稳态） | — | — | — | — | TestStability_ConcurrencyStress_AcquireRelease_CountAccurate / TestStability_RPMStress_Concurrent_CountAccurate |

---

## 3. 用例清单（输入 / 预期 / 实际 / 结论）

> 全部用例位于 `internal/gateway/quota/stability_test.go`。setup 均独立（各自的 `miniredis` + `SnapshotHolder`），无跨用例状态依赖。

### 3.1 场景 1：RPM 触顶 + 窗口过期恢复

#### TestStability_RPM_RejectThenWindowRecovery
- **输入**：snapshot `DefaultUser{RPM:3, Conc:0}`；连发 4 次 POST（user=u-rpm）。快进 30s 再发；再快进到总 61s 再发。
- **预期**：前 3 次 200；第 4 次 429；快进 30s（< 60s 窗口）仍 429；快进到 61s（> 窗口）恢复 200。
- **实际**：与预期一致。
- **结论**：PASS。补强了既有 `redis_store_test.TestCheckRPM_WindowExpiry_FastForward`（仅 store 层）的 middleware 端到端 + 窗口边界。

### 3.2 场景 2：并发触顶 + Release 恢复 + 过释放不变负

#### TestStability_Concurrency_FillRejectReleaseRecover
- **输入**：limit=2；Acquire 2 → Acquire 第 3 → Release 1 → Acquire → 全部 Release。
- **预期**：第 3 拒；Release 1 后恢复；全释放后键归零且被 DEL。
- **实际**：一致；归零后 `mr.Exists(key)=false`。
- **结论**：PASS。

#### TestStability_Concurrency_OverRelease_NeverNegative
- **输入**：Acquire 1 → Release 3（过度释放 2）→ 再 Acquire 1。
- **预期**：计数永不变负；过度释放后键归零被删；再 Acquire 后值为 1（而非 0→1 偏移）。
- **实际**：值为 1，无负数。
- **结论**：PASS。`redis_store.go:93` 的 `val<=0 → Del` 兜底有效。

#### TestStability_Middleware_Concurrency_ReleaseMidFlight
- **输入**：snapshot `DefaultUser{RPM:0, Conc:1}`；一个阻塞 handler 占用槽 → 第二请求 → close channel 释放 → 第三请求。
- **预期**：第二请求 429；释放后第三请求 200。
- **实际**：一致。
- **结论**：PASS。补强既有 `TestMiddleware_Concurrency`（只测拒绝，未测恢复）。

### 3.3 场景 3：四层叠加

#### TestStability_FourLayers_IndependentCounting
- **输入**：单请求路径串联 `middleware(subject+group)` 与 `ModelQuotaGate(model)`；subject RPM=100、group RPM=2、model RPM=2、service_key `sk-1` RPM=2。连发 3 次（user=u-four, group=gpt-4, model=m-uuid）。
- **预期**：前 2 次 200；第 3 次在 group 层（middleware 先于 gate 检查）429，`layer=group`；subject RPM 计数=3（3 次都过了 subject），model RPM 计数=2（第 3 次未到 gate）。
- **实际**：一致。
- **结论**：PASS。证明四层 key 隔离、计数不串扰、检查顺序 group→model。

#### TestStability_FourLayers_ServiceKeySubject_Independent
- **输入**：`sk-A` RPM=1，`sk-B` 默认；各发 2/1 次。
- **预期**：sk-A 第 2 次 429；sk-B 仍 200（独立计数）。
- **实际**：一致。
- **结论**：PASS。第四维度（service_key 主体）独立触顶验证。

### 3.4 场景 3b：四层全链路 model 并发令牌释放（BUG-1 修复回归）

#### TestStability_ModelConcurrencyToken_Released
- **输入**：snapshot models `m-uuid{RPM:0, Conc:2}`，其余层 Conc=0（跳过）。单请求链 `middleware → handler(内调 gate.BeforeUpstream)`。连发 2 次 → 读 model 并发键 → 发第 3 次。
- **预期（修复后）**：2 次请求后 model 并发计数归 0（令牌被正确释放）；第 3 次 200（limit 未被泄漏占满）。
- **实际**：与预期一致；2 次后 model 并发键=0，第 3 次 200。
- **结论**：PASS。本用例由原 `TestStability_KnownBug_ModelConcurrencyToken_Leaks`（assert bug 行为）在 BUG-1 修复后翻转而来，现固化修复后行为。bug 详情见 §4。

#### TestStability_FourLayers_HappyPath_WithinLimits（对照组）
- **输入**：group/model 各 Conc=2，单次请求。
- **预期**：200；group 并发令牌被释放归 0（subject/group token 在 c.Next 前已被 middleware 局部 tokens 持有）。
- **实际**：一致；group 并发键=0。
- **结论**：PASS。佐证 middleware 自身两层的释放是正确的，泄漏仅限 model 层（由 gate 在 c.Next 内 append 引入）。

### 3.5 场景 4：豁免跳过（不触碰计数器）

#### TestStability_Exemption_DoesNotTouchCounters
- **输入**：snapshot `DefaultUser{RPM:1, Conc:1}` + 豁免 `user/u-exempt`（1h 有效）；豁免用户连发 5 次。
- **预期**：5 次全 200；**且** Redis 中该主体 RPM ZSET 与并发键均**不存在**（真跳过，非“计数后豁免放行”）。
- **实际**：一致；RPM 计数=0、并发计数=0。
- **结论**：PASS。补强既有 `TestMiddleware_Exemption`（只 assert 放行）。

### 3.6 场景 5：fail-open 全场景

#### TestStability_FailOpen_RedisDown_ConcurrentAndRelease
- **输入**：snapshot `DefaultUser{RPM:1, Conc:1}`；关 Redis；50 goroutine 并发 POST；再调 `ReleaseConcurrency`。
- **预期**：50 个全 200，无 panic；Release 不 panic。
- **实际**：一致。
- **结论**：PASS。（注：用 `MaxRetries=-1 + DialTimeout=200ms` 的 fast-fail 客户端，避免 go-redis 默认 5 次重试退避导致数十秒阻塞。）

#### TestStability_Store_RedisDown_NoPanic
- **输入**：关 Redis；循环 10 次调 `CheckRPM`/`AcquireConcurrency`/`ReleaseConcurrency`。
- **预期**：不 panic；CheckRPM/Acquire 返回 nil（fail-open）。
- **实际**：一致。
- **结论**：PASS。

### 3.7 场景 6：快照热更新

#### TestStability_SnapshotHotUpdate_ImmediateEffect
- **输入**：先设 `DefaultUser{RPM:1000}`，发 1 次 200；热替换为 `{RPM:1}`，再发 1 次。
- **预期**：第二次即时受新限流（已用 1 → 429）。
- **实际**：一致。
- **结论**：PASS。原子切换无锁竞争，下个请求即生效。

#### TestStability_SnapshotHolder_ConcurrentReadWriteRace
- **输入**：8 reader goroutine 持续 `Get()`，1 writer 2000 次 `Set()` 替换快照。
- **预期**：`-race` 下无 DATA RACE告警；不 panic。
- **实际**：一致。
- **结论**：PASS。`atomic.Value` 无锁读路径正确。

### 3.8 场景 7：limit=0 vs 缺省继承

#### TestStability_LimitZero_vs_MissingInherit
- **输入**：
  - limit=0：CheckRPM/AcquireConcurrency 各 20 次；
  - 缺省继承：snapshot `DefaultUser{RPM:42,Conc:7}`，Users/Groups/Models 均空，分别查 absent 主体/组/模型。
- **预期**：
  - limit=0 恒通过且**不写** Redis（RPM ZSET 0 条、并发键 0）；
  - 主体缺省 → 继承 `default_user`（42/7，source=default_user）；
  - **group/model 缺省 → 0/unlimited（source=unset，不继承 default）**——非对称语义。
- **实际**：一致。
- **结论**：PASS。固化设计要点：subject 层“缺省=继承”，group/model 层“缺省=不限”。

### 3.9 场景 8：大规模并发压测（稳态）

#### TestStability_ConcurrencyStress_AcquireRelease_CountAccurate
- **输入**：limit=50；1000 goroutine 各 Acquire（成功则 Release）。
- **预期**：最终并发键=0（成功 acquire 都已 release）；acquired+rejected=1000；`-race` 无告警；无负值。
- **实际**：一致；无负值、无 race。
- **结论**：PASS。

#### TestStability_RPMStress_Concurrent_CountAccurate
- **输入**：limit=50；500 goroutine 并发 CheckRPM。
- **预期**：ZCARD ≤ 50（不超限）；passed=50；passed+blocked=500。
- **实际**：一致；ZCARD=50。
- **结论**：PASS。Lua 滑动窗口在并发下计数精确。

---

## 4. 发现的 bug

### BUG-1（中危→已修复）：ModelQuotaGate 并发令牌泄漏，model 层并发槽被永久占用直到 30min TTL

> ✅ **已修复（2026-07-02）**。采用下方「修复方向」第 2 项：middleware 在 `c.Next()` 后
> 从 context 回读 `quota_release_tokens` 最终列表再 release。对应用例重构为
> `TestStability_ModelConcurrencyToken_Released`（assert 修复后行为：val 归零、第 3 次 200）。
> `go test ./internal/gateway/quota/... -race -count=1` 全绿。

- **文件/行号**：
  - `internal/gateway/quota/middleware.go`（`tokens := []releaseToken{}` 局部切片）
  - `internal/gateway/quota/middleware.go`（`c.Set("quota_release_tokens", tokens)` 存入 context）
  - `internal/gateway/quota/middleware.go`（`c.Next()` → handler → `ModelQuotaGate.BeforeUpstream`）
  - `internal/gateway/quota/middleware.go`（`c.Set("quota_release_tokens", append(t, ...))` 把 model token 追加到 **context 内的切片**）
  - `internal/gateway/quota/middleware.go`（`m.releaseAll(tokens)` 用 **局部 tokens** 释放，只含 subject+group —— **旧 bug 行为**）

- **根因**：middleware 在 `c.Next()` 之前把本地 `tokens` 切片存入 gin context；`ModelQuotaGate.BeforeUpstream` 运行于 handler（`c.Next()` 内），将 model 并发 token 通过 `append` 写回 context 中的切片。但 `append` 在底层数组容量不足时会分配**新数组**，middleware 的局部 `tokens` 仍指向旧数组，`releaseAll(tokens)` 因此只释放 subject+group 两层，**model 层 token 永不释放**。即便 `append` 偶发原地写入（容量足够时），middleware 的局部切片也不感知——行为非确定，但 General case 为泄漏。
- **复现（修复前）**：`TestStability_KnownBug_ModelConcurrencyToken_Leaks`（已废弃）：limit=2，连发 2 次后 model 并发键=2（应 0），第 3 次 429。
- **影响（修复前）**：
  - model 并发计数只增不减，达到 `MaxConcurrency` 后该 model 对所有用户持续返回 429；
  - 唯一兜底是 `redis_store.go` 的 30min TTL（`concurrencyKeyTTL`），即泄漏槽 30 分钟后才自动回收——期间该 model 几乎不可用；
  - 影响四层中 **model 层并发门**（RPM 门不受影响，因 RPM 用滑动窗口自衰减）。
- **修复（已落地）**：middleware 在 `c.Next()` 后从 context 重新读取 `quota_release_tokens` 作为释放来源，覆盖 gate 追加的 model token：
  ```go
  if final, ok := c.Get("quota_release_tokens"); ok {
      if t, ok := final.([]releaseToken); ok {
          tokens = t
      }
  }
  m.releaseAll(tokens)
  ```
- **处理状态**：已修复生产代码 + 翻转用例断言；`go test -race` 全绿。

### BUG-2（高危→已修复）：group 层配额在生产 HTTP 链路下不生效（`extractModelParam` 读不到 group 名）

> ✅ **已修复（2026-07-02）**。采用「修复方向」第 2 项：`extractModelParam` 优先复用 `trace_req_body`（TraceCapture 在配额中间件之前执行、已捕获并存入 context），回退自行 `io.ReadAll` body 后 `NopCloser` 恢复，兜底 `c.Query("model")`。新增 `TestStability_GroupLayer_ParsesModelFromBody` 单测（真实 body 携带 model 字段、不 set `request_model`，第 3 次 429 layer=group）+ §8.6 真实 HTTP E2E（group `auto-test-group` rpm=1，第 2 次 429 layer=group gate=rpm，Redis ZCARD=1）。`go test -race` 全绿，运行中 gateway(17:01 重启的 fixed binary) 实测 PASS。

- **文件/行号**：`internal/gateway/quota/middleware.go` `extractModelParam`（读方）+ `middleware.go` `groupName` 为空时跳过 group 层 check。
- **根因**：`extractModelParam` 优先 `c.Get("request_model")`，fallback `c.Query("model")`。但：
  - 全链路 **无任何中间件 `c.Set("request_model", ...)`**（grep 全仓 `internal/gateway/` 确认：`trace_capture.go` 只 set `trace_req_headers`/`trace_req_body`；auth/usage/org_check 都不设该 key）；
  - chat/completions 的 `model` 字段在 **JSON body**，不在 query string → `c.Query("model")` 恒空；
  - 各 handler（chat.go/embeddings.go/messages.go/responses.go）虽在 handler 内 `c.Set("model", ...)`，但配额中间件在 handler **之前**执行，且 key 名是 `"model"` 而非 `"request_model"` —— 读不到。
  - 结果：生产路径下 `groupName` 恒为空字符串 → `if groupName != ""` 分支不进 → **group 层的 RPM/并发 check 整层跳过**。
- **影响（修复前）**：
  - 四层配额中 **group 层在生产链路形同虚设** —— 设了 `model_groups.quota_*` 也不生效；
  - 单元测试 §3.3 `TestStability_FourLayers_IndependentCounting` 能过，是因为测试**手动 `c.Set("request_model", ...)`** 绕过 `extractModelParam`，掩盖了生产链路的取值缺失；
  - model 层不受此 bug 影响（`ModelQuotaGate.BeforeUpstream` 由 handler 在选定具体 model 后显式调用，modelUUID 由参数传入，不依赖 `extractModelParam`）。
- **修复（已落地）**：`extractModelParam` 四级取值 —— ①兼容 `request_model` → ②复用 `trace_req_body`（`modelFromJSON` 解析顶层 `model` 字段）→ ③自行读 body + `NopCloser` 恢复 → ④`c.Query("model")` 兜底。Body 解析只取顶层 `model` 短字段，不解析整个大 body；`trace_req_body` 已 sanitize 但 model 字段短不被截断。
- **验证**：
  - 单测 `TestStability_GroupLayer_ParsesModelFromBody`：真实 body `{"model":"gpt-4",...}`、不 set request_model → 前 2 次 200、第 3 次 429 layer=group、Redis group 计数=2、subject 计数=3。`-race` 全绿。
  - 真实 HTTP E2E（§8.6）：运行中 gateway + group `auto-test-group` rpm=1 → 第 2 次 429 `layer=group gate=rpm limit=1`、Redis `ZCARD quota:rpm:service_key:<id>:group:auto-test-group`=1、60s 滑窗清除可复现、并发键 release 后 DEL 无泄漏。
- **处理状态**：已修复生产代码 + 新增单测 + 真实 HTTP E2E 闭环；`go test -race` 全绿。修复尚未 git commit（工作区 modified），提交前需 review。

---

## 5. 竞态检测（-race）结果

| 用例 | -race 结果 |
|---|---|
| TestStability_SnapshotHolder_ConcurrentReadWriteRace | 无 DATA RACE |
| TestStability_ConcurrencyStress_AcquireRelease_CountAccurate | 无 DATA RACE，计数精确 |
| TestStability_RPMStress_Concurrent_CountAccurate | 无 DATA RACE，ZCARD=limit |
| TestStability_FailOpen_RedisDown_ConcurrentAndRelease | 无 DATA RACE，无 panic |
| 其余稳定性用例 | 无 DATA RACE |

`go test ./internal/gateway/quota/... -race -count=1` → **PASS**，无 race 告警。
`SnapshotHolder` 的 `atomic.Value` 无锁读 + `Set` 原子写路径在并发读写场景下正确。

---

## 6. 稳定性结论

1. **两门基础语义正确**：RPM 滑动窗口（Lua）与并发 INCR/DECR 在单线程（miniredis）与 `-race` 并发下计数精确、触顶拒绝、恢复（窗口过期 / Release）均符合设计。
2. **fail-open 健壮**：Redis 故障下 CheckRPM / AcquireConcurrency / Middleware / ModelQuotaGate 全部放行、不 panic、不丢请求；ReleaseConcurrency 在 Redis down 下仅记日志不 panic。
3. **快照热更新原子**：`atomic.Value` 切换即时生效，`-race` 并发读写无竞争。
4. **设计语义已固化**：`limit=0`=不限（不计数）、subject 缺省=继承 default、group/model 缺省=0 不限；豁免真跳过（不触碰计数器）。
5. **已修复缺陷**：BUG-1（model 层并发令牌泄漏）已在本次修复并回归（§4）。修复后 subject/service_key 两层 + RPM 门 + 并发门 + 豁免路径在单元与真实 HTTP 端到端（§8）下全部稳定。
6. **已修复缺陷**：BUG-2（group 层配额在生产 HTTP 链路不生效，`extractModelParam` 取不到 group 名）已在本次修复并回归（§4 + §8.6）。修复后 group 层在真实 HTTP 下第 2 次 429 layer=group、Redis 计数实测自洽。**四层两门全部稳定，无已知遗留缺陷。**

---

## 7. 建议补强项

1. ~~**修复 BUG-1**~~（已完成）：middleware 在 `c.Next()` 后从 context 回读 `quota_release_tokens` 最终列表统一 release，覆盖 gate 追加的 model token；用例已翻转为 `TestStability_ModelConcurrencyToken_Released`。详见 §4。
2. **修复 BUG-2（优先）**：group 层配额在生产链路不生效。需让配额中间件能取到 group 名 —— 推荐在中间件前加轻量 body 解析 `c.Set("request_model", ...)`，或 `extractModelParam` 直接复用 `trace_capture` 已 set 的 `trace_req_body`。修复后补真实 HTTP 的 group 层触顶 E2E（设 `model_groups.quota_rpm=2` + 请求组内 model → 第 3 次 429 layer=group）。详见 §4 BUG-2。
3. 真实 Redis（非 miniredis）集成测试：miniredis 单线程串行化命令，无法暴露多 Redis 节点 / 网络分区下的 Lua 脚本与 INCR 竞态。建议补一个针对真实 Redis（testcontainers 或 CI 环境实例）的冒烟集成测试。
4. 流式响应释放验证：middleware 注释提到“流式：在 flush 完成后的 defer”，但本次未覆盖流式路径下并发令牌的释放时机。建议补一个流式 handler 的并发占用→释放时序用例（SSE / chunked）。
5. 多实例一致性：四层 key 全局共享 Redis，多 gateway 实例下并发计数正确；但 30min TTL 兜底在多实例崩溃恢复下的边界（崩溃后槽位占用多久）值得一个显式用例（FastForward 到 30min±ε 验证回收）。
6. 快照部分字段缺失：当前 `ResolveGroupLimits`/`ResolveModelLimits` 对“map 存在但 LayerLimits 零值”与“map 缺失”视为同一（0/unlimited）。若未来需要区分“显式禁用”与“不限”，需扩 type；当前已固化该语义，无即时风险。

---

## 8. 端到端真实链路验证（运行中的系统 · HTTP）

> 测试日期：2026-07-02。对运行中的真实服务做 HTTP 端到端验证，证明四层配额中间件在 gateway(:8080) 完整链路下真实生效。与 §3 的单元/`miniredis` 集成测试互补：前者在进程内 mock，本节在真实 gateway + control-plane(:8081) + 远程 Redis(192.0.2.10:6379 db0) + 远程 PG(192.0.2.10/llm_gateway) 下实测。
>
> 工具：`curl`（HTTP）/ `redis-cli`（Redis 计数实测）/ `psql`（DB 校验）。无新增依赖。
>
> **链路事实（采集自代码 + 实测）**：`POST /v1/chat/completions` 上中间件顺序为 `AuthMiddleware → UsageCollector → OrgCheckMiddleware → quotaMW.Handler() → chatHandler.Handle`（`internal/gateway/server/router.go:170-182`）。配额中间件在 handler 前、按层序 `subject → (group) → ...` 逐层 CheckRPM → AcquireConcurrency，任一门触顶即 `WriteQuotaRejected` 直接 429、不到 handler/上游（`internal/gateway/quota/middleware.go:97-123`、`reject.go:22-52`）。

### 8.0 测试前置与主体设置

- **测试主体**：新建一个临时 Service Key（`key_type=service`，由 admin 在 control plane 创建），其 `KeyType=="service"` 经 `ResolveSubject` 映射为 `subject_type="service_key"`、`subjectID=keyUUID`（`resolver.go:37-39`）。Redis 主体层计数键：`quota:rpm:service_key:<keyUUID>` / `quota:conc:service_key:<keyUUID>`（`redis_store.go:100-113`）。
- **配额设置**：`PATCH /api/v1/keys/:id/quota` body `{"rpm":2,"max_concurrency":1}`。该 handler 写 `api_keys.quota_rpm/quota_max_concurrency` 后异步 `refreshSnapshotAsync()` → 重建快照写 Redis `quota:snapshot:v1` 并 `Publish config:change "quota_snapshot_updated"`，gateway 订阅者 ~1s 内 reload，配置即时生效（`quota_service.go:80-98`、`config_loader.go:91-120`）。
- **跨层门前置约束（实测发现）**：`OrgCheckMiddleware` 要求 identity 的 `dept_id` 非空，否则 403 `budget_not_configured`/`gate:identity` 拦截在配额中间件之前（`org_check.go:65-72`）。admin 账号 `dept_id` 为空 → 其创建的 Service Key `verify-key` 返回空 `DepartmentID` → 被 OrgCheck 拦截、配额层根本不执行。为隔离地测配额，对**临时测试 Key**设 `bypass_org_check=true`（DB 直改；`OrgCheck` 对 `BypassOrgCheck=true` 放行，`org_check.go:57-63`，但**配额中间件不读该字段** → 配额仍正常执行）。该字段随 Key 一并删除，不留痕。
- **Auth 缓存**：gateway 对 `verify-key` 结果双层缓存（本地 60s + Redis `gateway:keycache:<sha256(plaintext)>` 5min，`key_cache.go:21-22`）。为避免首次被拒请求的缓存污染，测试 Key 在**任何 gateway 请求之前**就设好 `bypass_org_check`，首验即生效；删除 Key 时控制面发 `key_deleted` 事件失效该缓存（实测删除后 `gateway:keycache:<hash>` EXISTS=0）。
- **请求体**：`{"model":"__e2e_nonexistent_model__","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`。故意用不存在的 model 名：① 不命中任何 group 覆盖 → group 层缺省=0=不限；② gateway 内部调度器直接返回 404 `unknown_model`，不到上游（不花钱、不调真实 provider）。关键：配额 RPM 计数在中间件阶段（Lua `ZADD`）即 incr，**在调度器 404 之前**，故 404 响应同样消耗一个 RPM 槽——适用于计数测试。

### 8.1 场景 E2E-1：RPM 触顶拒绝（subject 层）

- **输入**：
  - 配额：`PATCH /api/v1/keys/26effaa9.../quota` body `{"rpm":2,"max_concurrency":1}` → control plane 返 `{"code":0,"message":"Service Key 配额已更新"}`；`GET .../quota` 返 `source:"override", rpm:2, max_concurrency:1`（DB 同步：`quota_rpm=2, quota_max_concurrency=1`）。
  - 连发 3 次：
    ```bash
    curl -X POST http://localhost:8080/v1/chat/completions \
      -H "Authorization: Bearer sk-252d...512b" -H 'Content-Type: application/json' \
      -d '{"model":"__e2e_nonexistent_model__","messages":[{"role":"user","content":"hi"}],"max_tokens":1}'
    ```
- **预期**：前 2 次非 429（404 `unknown_model`，配额放行且未被误拒）；第 3 次 429，`gate=rpm, layer=subject`；Redis `ZCARD quota:rpm:service_key:<id>` = 2。
- **实际**：
  - #1 → `HTTP 404` `{"error":{"code":"unknown_model","message":"...model not found: __e2e_nonexistent_model__","type":"model_not_found"}}`
  - #2 → `HTTP 404`（同上）
  - #3 → `HTTP 429`：
    ```json
    {"error":{"code":"quota_rpm_exceeded","gate":"rpm","layer":"subject","limit":2,
      "message":"速率超限：Service Key层 请求频率（RPM） 已达上限（2）",
      "param":"subject","retry_after_seconds":60,"subject_type":"service_key","type":"rate_limit_error"}}
    ```
  - Redis：`ZCARD quota:rpm:service_key:26effaa9...` = **2**；`PTTL` ≈ 60.9s（60s 滑窗 + 1s 容差）。
  - 响应头：`Retry-After: 60`、`X-RateLimit-Limit: 2`、`X-RateLimit-Remaining: 0`、`X-RateLimit-Reset: 60`（`reject.go:24-30`）。
- **结论**：**PASS**。subject 层 RPM 滑窗触顶真实拒绝、计数与 Redis 实测吻合、响应体机器码与 layer/gate/limit 字段齐全、`#1/#2` 非 429 证明无配额误拒。机器码字段为响应体 `error.code="quota_rpm_exceeded"`（`errorCode()` 输出），日志/内部映射键为 `QUOTA_RPM_REJECTED`（`errorCodes` map）——二者指向同一拒绝，外部消费者读 `error.code`/`error.layer`/`error.gate`。

### 8.2 场景 E2E-2：并发触顶拒绝（subject 层）+ Release 归零不泄漏

- **输入**：复位计数器（`DEL quota:rpm:... / quota:conc:...`）后，`max_concurrency=1` 不变。用 `xargs -P 5` 并发 5 次（同 §8.1 请求体）。
- **预期**：`max_concurrency=1` ⇒ 至多 1 个 acquire 成功，其余在并发门 429 `gate=concurrency, layer=subject`；因 RPM=2 同时存在，超 2 的部分在 RPM 门 429。请求平息后并发键应归零被 DEL（`redis_store.go:93` `val<=0 → Del`），无槽位泄漏。
- **实际**（5 并发）：
  - 1 个 → `HTTP 429` `{"code":"quota_concurrency_exceeded","gate":"concurrency","layer":"subject","limit":1,"retry_after_seconds":1,"subject_type":"service_key","type":"concurrency_limit"}` ← **并发门拒绝实测命中**
  - 3 个 → `HTTP 429` `{"code":"quota_rpm_exceeded","gate":"rpm","layer":"subject","limit":2,...}`（RPM=2 占满后由 RPM 门拒）
  - 1 个 → `HTTP 404` `unknown_model`（唯一通过两门、消耗 1 RPM + 1 并发槽）
  - 释放后：`EXISTS quota:conc:service_key:<id>` = **0**、`TYPE` = **none**（槽位已 DEL，无泄漏）。
- **结论**：**PASS**。subject 层并发门 `INCR/DECR` 在真实 Redis 下触顶拒绝、release 归零删键无泄漏；层序 `subject` 内 RPM→Concurrency 的检查顺序在并发压测下自然暴露（1 并发拒 + 3 RPM 拒 + 1 通过 = 5，计数自洽）。`retry_after_seconds=1`（并发门，`reject.go:22-24`）区别于 RPM 门的 60s，两门响应头语义区分正确。

### 8.3 场景 E2E-3：豁免跳过（真实放行 + 不触碰计数器）

- **输入**：复位计数器后，先建豁免：`POST /api/v1/quota/exemptions` body `{"subject_type":"service_key","subject_id":"<keyUUID>","reason":"e2e exemption test","expires_at":"2026-07-02T08:37:50Z"}` → 201 返豁免记录；`refreshSnapshotAsync` 发布后 ~2s，`quota:snapshot:v1` 中已含 `subject_type=service_key, subject_id=<id>, expires_at=未来` 的豁免项（实测 `jq` 命中）。随后连发 5 次（超 rpm=2）。
- **预期**：豁免命中 → 全 5 次非 429（404 `unknown_model`）；**且** Redis 主体 RPM/并发键**不存在**（真跳过，非“计数后放行”）。
- **实际**：
  - #1..#5 → 全 `HTTP 404` `unknown_model`（无任何 429）。
  - 计数：`ZCARD quota:rpm:service_key:<id>` = **0**；`EXISTS quota:rpm:...` = **0**；`EXISTS quota:conc:...` = **0**。
- **结论**：**PASS**。豁免路径在 `middleware.go:56-64` 命中后 `c.Next()` 直接放行、**在触碰任何计数键之前** —— 端到端真实验证“跳过而非宽限”。

### 8.4 清理与残留校验

- 撤销豁免：`DELETE /api/v1/quota/exemptions/<exemption_id>` → `{"code":0,"message":"豁免已撤销"}`。
- 删除测试 Key：`DELETE /api/v1/service-keys/<key_id>` → `{"ok":true}`（同时抹除其 `quota_rpm/quota_max_concurrency` 覆盖）。
- 残留校验：
  - DB：`api_keys` 该 id `count=0`；`quota_exemptions` 该 id `count=0`。
  - Redis：`--scan --pattern 'quota:rpm:*'` count=**0**、`'quota:conc:*'` count=**0**（全库无任何配额计数残留）；被删 Key 的 `gateway:keycache:<hash>` EXISTS=0（缓存随 `key_deleted` 失效）。
  - 快照：`quota:snapshot:v1` 中对该 `subject_id` 的豁免筛选为空（已随 refresh 移除）。
  - 审计：`audit_logs` 留有 `quota.service_key.update` / `quota.exemption.create` / `quota.exemption.revoke` / `service_key.delete` 记录（属正常合规留痕，非测试垃圾）。

### 8.5 端到端结论与覆盖诚实说明

1. **真实生效**：subject 层「RPM 滑窗触顶 + 并发 INCR/DECR 触顶 + release 归零不泄漏 + 豁免真跳过」四个语义，在运行中的 gateway 完整链路下经真实 HTTP 验证全部 **PASS**，且 Redis 计数实测与 HTTP 响应体字段自洽。
2. **四层覆盖说明（诚实）**：
   - 本次 E2E **实测到**：**subject 层**（service_key 主体）的 RPM 门 + 并发门 + 豁免。
   - 本次 E2E **未实测**：**group 层**与 **model 层**的 HTTP 拒绝。原因有二（代码事实，非环境限制）：
     - (a) `extractModelParam`（`middleware.go:157-166`）读 `c.Get("request_model")`，但全链路中**无任何前置中间件 set 该值**（grep 全仓仅 middleware.go 自身读、无人写）→ 生产路径下 `groupName` 恒为空 → group 层 check 被跳过。
     - (b) model 层由 `ModelQuotaGate.BeforeUpstream`（`middleware.go:181-`）在 handler 内、上游调用前才挂载；本次 body 用不存在的 model，调度器在到达 gate 前即 404。
     - group/model 两层的触顶/计数隔离已由 §3.3 `TestStability_FourLayers_IndependentCounting`、§3.4 `TestStability_ModelConcurrencyToken_Released` 在进程内覆盖；E2E 未补这条链路。
   - **default 层**：本次对该 Key 设了显式 override（`source=override`），未走 default 兜底；default 层语义已由 §3.8 单测覆盖。
3. **发现的非配额层现象（非缺陷，仅记录）**：admin 创建的 Service Key 因 admin 无 dept → `OrgCheck` 403 先于配额拦截。配额中间件本身正确，但「无部门管理员创建的 Key 无法触达配额层」是一个部署/权限事实，测试中靠 `bypass_org_check` 临时绕开。若后续要做无 bypass 的端到端，需用一个已归属 dept+studio 的真实员工 Key。
4. **配额变更热生效**：`PATCH .../quota` → ~1s 内 gateway 经 pub/sub reload 快照，新限额即时作用于下一请求（无需重启）；E2E 中 set 配额后 sleep 2s 即复测命中，间接验证热更新链路（呼应 §3.6 单测的快照热更新结论）。
5. **未做**：流式（SSE/chunked）响应的并发令牌释放时序未在 E2E 覆盖（与 §7 补强项 3 同源）；多 gateway 实例下并发计数一致性未验（单实例环境）。`logs/` 下未见 live gateway 的 `quota rejected` slog 行（当前 `go run` 进程 stdout 未重定向到 `logs/gateway.log`），HTTP 429 响应体为权威证据，日志佐证缺失不影响结论。

### 8.6 场景 E2E-4：group 层 RPM 触顶拒绝（BUG-2 修复回归）

> 测试日期：2026-07-02 17:0x CST。BUG-2 修复后补的真实 HTTP 端到端，**专门证明 group 层在生产链路不再被跳过**。本节为 §8.5 结论 2.(a) 的闭环：此前 E2E 因 `extractModelParam` 取不到 group 名而无法测 group 层，修复后该层可测且实测 PASS。

#### 8.6.1 修复前 vs 修复后：链路与 binary 事实

- **修复前**（committed HEAD `09d2b7a` 的 `extractModelParam`）：仅 `c.Get("request_model")`（全链路无人 set）+ 兜底 `c.Query("model")`。`POST` body 的 `model` 字段恒读不到 → `groupName=""` → `middleware.go:87` 的 `if groupName != ""` 不成立 → group 层 check 整层跳过。
- **修复后**（当前工作区 `middleware.go:164-190`，未提交）：`extractModelParam` 增三段：① 兼容 `request_model`；② 复用 TraceCapture set 的 `trace_req_body`（`[]byte`）；③ 自行 `io.ReadAll(c.Request.Body)` 并 `NopCloser` 恢复（保证后续 handler 仍可读）；④ 兜底 query。新增 `modelFromJSON`（`middleware.go:194-202`）只取 body 顶层 `model` 字段。
- **关键发现：测试前运行中的 gateway binary 是修复前版本**。进程 PID 61386（`go run` 编译产物 `/var/folders/.../go-build543682799/b001/exe/gateway`）启动于 **14:39:04**，而 `middleware.go` 的 mtime 为 **16:47:16** —— 修复写盘在 binary 编译之后 2 小时。`strings` 校验：旧 binary **不含** `quota.modelFromJSON` 符号（count=0），修复后 binary 含（count=1）。即「gateway 已于 16:51 重启带修复」与事实不符：16:51 并未重启，14:39 的进程一直跑到本测试前。**为得到真实修复后证据，本测试用当前源码 `make build` 重建 `bin/gateway`（含 `modelFromJSON`，strings count=1），停掉旧 PID 61386、于 17:01:26 启动新 fixed binary（PID 57107，`logs/gateway-e2e.log`）**，恢复对同一 Redis/PG/control-plane(:8081) 的连接（启动日志 `quota: config change subscriber started` + `scheduler config synced groups=12 models=15`）。

#### 8.6.2 测试前置

- **临时 Service Key**：`POST :8081/api/v1/service-keys` body `{"name":"e2e-grouplayer-260702b","key_type":"service","purpose":"e2e-group-test","dept_id":"技术研发部","access_all_groups":true,"bound_groups":[],"enabled":true}` → 201 返明文 `key=sk-205f...b2db`、`id=e4a273d2-e8a9-445a-ba60-ed4d2ac3560b`、`key_type=service`。经 `ResolveSubject`（`resolver.go:37-39`）→ `subject_type=service_key, subjectID=<id>`。
- **bypass_org_check**：DB 直改 `UPDATE api_keys SET bypass_org_check=true WHERE id=?`（同 §8.0 方法；admin 无 dept 会被 OrgCheck 拦在配额前，bypass 后配额中间件仍正常执行，不读该字段）。Key 在首次 gateway 请求前即设好，首验即生效（无 keycache 污染）。
- **subject 层不限**：该 Key 未设自身 quota override，快照 `default_service_key=null` → `ResolveSubjectLimits` 走 `default_service_key` 但其为零值结构体 → `RPM=0, MaxConcurrency=0` → `CheckRPM/AcquireConcurrency` 在 `limit<=0` 时直接 return nil 不触碰 Redis（`redis_store.go:50,68`）。**故 subject 层不会先误拒**，可隔离地观察 group 层。
- **group 层配额**：`PATCH /api/v1/groups/19/quota` body `{"rpm":1,"max_concurrency":1}` → `{"code":0,"message":"模型组配额已更新"}`；`GET` 返 `source:"override", rpm:1, max_concurrency:1`。DB `model_groups` id=19（`name=auto-test-group`）`quota_rpm=1, quota_max_concurrency=1`。异步刷新后 `quota:snapshot:v1` 的 `groups` map 含 `{"auto-test-group":{"rpm":1,"max_concurrency":1}}`（`quota_service.go:282` 以 `g.Name` 作 key，故 group 名即是请求 body 的 `model` 字段值）。
- **请求体**：`{"model":"auto-test-group","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`。`model` 字段 = 组名（**非**不存在的模型名，与 §8.1 故意用 `__e2e_nonexistent_model__` 不同）→ 命中真实 group → 调度器选组内真实 model 转发上游。**副作用提示**：第 1/3 次放行的请求真实命中上游（dashscope/qwen3.6-plus，HTTP 200），略耗 ~200-240 tokens；为控成本仅发 4 次。

#### 8.6.3 输入（curl）

```bash
# fixed binary 已在 :8080 运行
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-REDACTED" \
  -H 'Content-Type: application/json' \
  -d '{"model":"auto-test-group","messages":[{"role":"user","content":"hi"}],"max_tokens":1}'
```
连发 4 次（#1→#2 间隔约 90s（含手测观察），#3→#4 几乎瞬时）。group rpm=1 滑窗 60s。

#### 8.6.4 实际（响应码 + 体 + Redis 计数）

| # | 间隔 | HTTP | 响应体关键 | Redis `ZCARD quota:rpm:service_key:<id>:group:auto-test-group` |
|---|------|------|-----------|--------------------------------------------------------------|
| 1 | t0 | **200** | 真实上游 chat.completion（dashscope qwen3.6-plus），消耗 1 RPM 槽 | 1（ZADD 成员 `1782982932127-886429`） |
| 2 | t0+~5s（窗内） | **429** | `{"code":"quota_rpm_exceeded","gate":"rpm","layer":"group","limit":1,"message":"速率超限：模型组层 请求频率（RPM）已达上限（1）","param":"group","retry_after_seconds":60,"subject_type":"service_key","type":"rate_limit_error"}` | 1（触顶未 ZADD） |
| 3 | t0+~90s（窗已过期） | **200** | 真实上游 chat.completion，窗已清空 #1 条目、#3 重新 ZADD | 1（ZADD 成员 `1782983017566-15583`，**#1 旧条目被 `ZREMRANGEBYSCORE` 清除**） |
| 4 | t0+~90s（紧随 #3，窗内） | **429** | 同 #2（`layer=group, gate=rpm, limit=1`） | 1（未 ZADD） |

Redis 计数实测（修复后 fixed binary）：
- `ZCARD quota:rpm:service_key:e4a273d2-...:group:auto-test-group`：放行后=`1`、触顶时仍=`1`（不增）、窗过期后旧条目被清→新放行仍=`1`。
- `ZRANGE ... WITHSCORES`：放行后可见成员 `<ts>-<rand>` score=`<ts ms>`；窗过期后旧成员消失、新成员入列 —— 实证 60s 滑窗 `ZREMRANGEBYSCORE` 在 group 层真实执行。
- `EXISTS quota:conc:service_key:<id>:group:auto-test-group`：放行后短暂=1（`INCR` 到 1），handler 完成 `releaseAll` → `DECR` 归 0 → `val<=0` 触发 `Del`（`redis_store.go:93`）→ `EXISTS`=0。**group 并发槽无泄漏**（与 §8.2 subject 并发 release 归零语义一致，落到 group 层）。
- subject 层键 `quota:rpm:service_key:<id>` / `quota:conc:service_key:<id>`：全程 `EXISTS`=0（limit=0 不计数，符合 §3.7/§6 结论 4）。

#### 8.6.5 结论

**PASS。** BUG-2 修复后 group 层配额在生产 HTTP 链路真实生效：

1. **核心证据**：group `rpm=1` 下，第 2 次请求 `layer=group, gate=rpm, limit=1` 429 —— **修复前 group 层恒跳过、第 2 次不会 429**（`extractModelParam` 读不到 body `model`、`groupName=""` 整层被 `if groupName != ""` 短路）。修复后 `modelFromJSON` 从 body 解出 `"auto-test-group"` → `snap.Groups["auto-test-group"]` 命中 override → group 层 check 真实执行并触顶拒绝。响应体 `error.code="quota_rpm_exceeded"`、`error.layer="group"`、`error.gate="rpm"`、`error.limit=1`、`error.param="group"`、`retry_after_seconds=60` 字段齐全。
2. **subject 层不误拒**：#1/#3 非 429（HTTP 200 真实上游），证明 subject 层 `limit=0` 不计数、未抢先拦截 —— 隔离地暴露 group 层语义。
3. **滑窗恢复**：#3 在 #1 之后 ~90s（>60s 窗）发，旧 ZADD 成员被 `ZREMRANGEBYSCORE` 清除、#3 重新入列放行 → 紧接 #4 再触顶。group 层 RPM 滑窗「触顶→窗过期恢复→再触顶」全周期可复现（呼应 §3.1、§8.1 的 subject 层同语义，落到 group 层）。
4. **group 并发无泄漏**：每次 200 响应完成 releaseAll 后 group 并发键被 DEL，无槽位残留。

#### 8.6.6 诚实说明与遗留

1. **binary 替换**：本测试为得到修复后证据，**自行重建并重启了 gateway**（旧 PID 61386 pre-fix → 新 PID 57107 fixed，`logs/gateway-e2e.log`）。重启前 :8080 确有进程，但属修复前编译产物；不替换则 §8.6 全部「修复前行为」（group 层跳过、无 group 计数键）无法翻转。修复（`middleware.go` 含 `extractModelParam`/`modelFromJSON`）**仍在工作区、未 git commit**（`git diff` 可见，`git log -S modelFromJSON` 无命中）—— 提交前需 review。
2. **real upstream 副作用**：放行请求命中 `auto-test-group` 内真实 model，#1/#3 共耗 ~450 tokens（dashscope qwen3.6-plus）。未用 §8.1 的「不存在的 model 名」trick，因组名必须真实命中 `snap.Groups` 才能触发 group 层 override（用假名则 group 名虽被解析、但 `ResolveGroupLimits` 走 `unset`=0=不限、测不到触顶）。
3. **`trace_req_body` 路径未单独剥离验证**：`extractModelParam` 优先复用 `trace_req_body`，本场景未显式区分「走了 trace_req_body」还是「走了 io.ReadAll 兜底」—— 二者取出的 `model` 值相同、HTTP 行为一致，故未单独构造 `log_enabled=false` 使 trace_req_body 未 set 的对照组。该兜底分支的正确性由单测 `TestStability_*`（§3）与本次「body 必读」事实间接覆盖；如需精确剥离，可临时关 log_enabled 再复测。
4. **model 层仍非本节重点**：本次 body 的 `model`=组名走到 group 层 check 通过后，调度器选组内具体 model 转发上游，未单独触发 `ModelQuotaGate.BeforeUpstream` 的 model 层触顶（model 层配额未设 override）。model 层 HTTP 触顶仍属 §8.5 结论 2 待补项，与 BUG-2 无关。

#### 8.6.7 清理与残留校验

- 删测试 Key：`DELETE /api/v1/service-keys/e4a273d2-...` → `{"ok":true}`（`key_deleted` 事件失效 keycache；删后用该明文 key 再请求 gateway → HTTP 401，证实密钥确删且 keycache 未缓存旧 identity）。
- group 19 配额：恢复为 `rpm=1, max_concurrency=1`（与 auto-test-group 作为组配额测试 fixture 的预期状态一致；`PATCH` 触发 full `refreshSnapshot`，`updated_at` 2026-07-02T09:08:31Z，`groups` map 与 DB 自洽）。
- 清 Redis 计数：`DEL quota:rpm:...:group:auto-test-group` / `quota:conc:...:group:auto-test-group` / subject 层键。
- 残留校验：DB `api_keys` 该 id count=0、`like 'e2e-grouplayer%'` count=0、`quota_exemptions` 该 subject_id count=0；Redis 全库 `quota:rpm:*` count=0、`quota:conc:*` count=0；snapshot `service_keys` overrides 空、`exemptions` 中无该 subject_id。`gateway:keycache:c0c47a...` 一条 TTL=39s 残留经 sha256 比对**非本测试 key**（本 key sha256=`e4f95163...`），属既有他者流量，非本次引入。审计 `audit_logs` 留 `quota.exemption.create`/`quota.exemption.revoke`/`quota.group.update`/`service_key.delete`/`service_key.create` 记录（正常合规留痕）。
- **遗留**：为本测试启动的 fixed gateway binary（PID 57107，`bin/gateway`）仍在 :8080 运行；若要恢复原 `go run` 启动方式需手动处理。`middleware.go` 修复未提交。

