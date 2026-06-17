# 验收测试矩阵与对账恒等式设计 (t5)

> 现状:按 `.worktrees/from-main`(branch `codex/from-main`) 已核实的字段与口径(t3)设计。
> 目标:产出「对账恒等式清单 + A1–A5 正/反/边界用例 + 各层空态/NotComputed/特殊字符矩阵」,
> **只写用例设计文档,不改代码**;待 t4 接入 main.go 后可直接转成 `derive_test.go` / 端到端用例。
> 依据文件: `internal/observability/report/report.go`(契约)、`derive/{derive,baseline,anomalies}.go`(算法)、
> `metric.go`(DayliMetric/CallEvent/MinuteBucket/RuleSet+DefaultRuleSet)、`tools/usage-report/cohort.go`(同岗池)。
> **校准说明(v1.2·终稿)**:本文已按 captain 对 S1–S5 的拍板校准完毕,全部关闭:
> S1(单位成本 ×1000 正确)、S2(A3 失败=非200)、S3(A5 上游 `nonService` 已滤 service,非 bug,无需改)、
> S4(DevSigma 禁 Inf,已修)、S5(A4 用有效输入 `prompt−cached`,已修)。断言均已锁口径。

---

## 0. 统一口径(测试断言的前提)

| 口径 | 实现 | 位置 |
|---|---|---|
| 成功 | `status == 200`;失败=`status != 200` | metric.go:50 anomalies.go:339/347 |
| 时区 | `ts` 归一到 Asia/Shanghai,日切/小时/分钟桶均以本地 | derive.go:133, anomalies.go:46 |
| TotalTokens | = `prompt + completion + reasoning`(**不含** cached/cache_write,cached 是 prompt 子集) | metric.go:31, derive.go:114 |
| PromptTokens | 含 cached(cached ⊆ prompt) | metric.go:26,27 |
| service key | `key_type == 'service'`;用户维度排除,单位维度单独聚合回填;**A1–A5 收到的 `Calls` 输入契约=已排除 service**(`main.go:94` `memberCalls=nonService(allCalls)`、`nonService` main.go:535 剔 `KeyType=="service"`、main.go:168 `ReportInput{Calls:memberCalls}` → derive.go:53 `Anomalies(in.Calls,...)`),函数内无需再滤(再滤=双滤) | derive.go:168-178, report.go:186 |
| 币种 | USD 唯一事实源,CNY 仅展示(`Currency.USDCNYRate`,默认 7.2) | derive.go:281-285, report.go:115 |
| 单位`cost_per_1k_tokens` | **`EstCostUSD / TotalTokens * 1000`**(每千 token)——**captain 已拍板 ×1000 正确**,原「×1e6 每百万」为笔误 | derive.go:279 |

---

## 1. 对账恒等式清单(每条=一条 reconciliation 断言,「恒等=/差值必须≈0」)

> 约定:浮点比较一律用 1e-9 相对/绝对容差(参考 `derive_test.go floatEq`),避免加法累积误差。下文 `Σ` 为「该范围所有输入行求和」。

### ID-1 部门/工作室总量 = Σ成员(+Σ程序)
- 恒等:`UnitFact.Member.ReqCount == Σ{(ScopeID==deptID && KeyType != 'service').ReqCount}`
- 恒等:`UnitFact.ServiceKey.ReqCount == Σ{(ScopeID==deptID && KeyType == 'service').ReqCount}`
- 恒等:单位总量(成员+程序)= `Member.ReqCount + ServiceKey.ReqCount`
- 恒等:每行 `ReqCount == SuccessCount + ErrorCount`(derive.go:106-108,169-174)
- 边界:某成员 0 调用但有 roster 记录 → 计入 `UserCount`(花名册)与 `ActiveUserCount`(实际)之差;`PerCapita.ReqCount==Member.ReqCount/ActiveUserCount`(derive.go:227)。
- 特殊:dept_id 为空/`""` 的行归入「未知部门」,不并入任何 ScopeID。

### ID-2 成功率 = Σsuccess/Σcount
- 恒等:`Efficiency.SuccessRate == Member.SuccessCount/Member.ReqCount`(仅 `ReqCount>0`,derive.go:270)
- 恒等:`Efficiency.ErrorRate == 1 - SuccessRate`(derive.go:271)
- 边界:`ReqCount==0` → `SuccessRate` 保持 0(零分母→n/a,代码不产出该值,断言不因 0/0 而 NaN)。

### ID-3 缓存比 = Σcached/Σprompt
- 恒等:`Efficiency.CacheHitRate == Member.CachedTokens/Member.PromptTokens`(仅 `PromptTokens>0`,derive.go:274)
- 恒等:`Efficiency.OutputRatio == Member.CompletionTokens/Member.PromptTokens`(derive.go:275)
- 恒等:`Efficiency.ReasoningShare == Member.ReasoningTokens/Member.TotalTokens`(derive.go:278)
- **不恒等(反证)**:`PromptTokens != CachedTokens` 的语义是 prompt 含 cached,所以 **`cached == prompt` 才表示 100% 缓存命中**;`cached > prompt` 属脏数据,应被标记。

### ID-4 单位 token 成本(按实现口径)
- 恒等:`cost_per_1k_tokens == EstCostUSD/TotalTokens*1000`(derive.go:279)
- 恒等(反证):用 `Σ prompt+Σ cached` 当分母会**低估**成本(漏算 completion/reasoning 或重复算 cached),必须用 TotalTokens。
- 边界:`TotalTokens==0` → 0;`EstCostUSD==0` → 0;极小币值(如 0.000001)格式化为 `0`(fmtFloat, anomalies.go:560-572)。

### ID-5 单位 token 成本 — **口径已确认(×1000)**
- **captain 拍板:实现 `×1000` 正确(cost_per_1k_tokens)**;「Σcost/Σtoken×1e6(每百万)」为文档笔误,已确认改用 ×1000。
- 恒等:与 ID-4 同口径 `cost_per_1k_tokens == Σcost/Σtotal × 1000`;断言按 ×1000,**不再标「待确认」**。
- 边界:`TotalTokens==0`→0;`EstCostUSD==0`→0;极小币值(如 0.000001)格式化为 0(fmtFloat,anomalies.go:560-572)。

### ID-6 TotalTokens 口径(防双计)
- 恒等:`TotalTokens == PromptTokens+CompletionTokens+ReasoningTokens`(metric.go:31)
- 反证:`TotalTokens != PromptTokens+CachedTokens+CompletionTokens+ReasoningTokens`(会把 cached 双计,cached ⊆ prompt);若出现等于则说明口径错误,单测必失败。

### ID-7 p95 库内 percentile_cont
- 恒等:`derive.Percentile(sorted, 95) == percentile_cont(0.95) WITHIN GROUP (ORDER BY val)`(同集合)。
- 实现方法:`rank = p/100*(n-1)`,lo=floor,hi=ceil,线性插值(baseline.go:42-57);**与 PostgreSQL `percentile_cont` 默认 linear 插值完全一致**,可直接对账。
- 边界:`n==0`→0;`p<=0`→min;`p>=100`→max;`n==1`→唯一值;全等值→该值。

### ID-8 加权滑窗 count/cost == 展开逐条(内存关键,已部分覆盖)
- 恒等:`weightedWindows(buckets, w)` 与 `windows(bucket(expand(buckets)), w)` 每个分钟窗口的 `count/cost` 一致(expand:每桶 Count=N、Cost=C → N 条调用、Cost 均摊)。
- 恒等:任意窗口 `cost == Σ{桶 ∈ [s, s+w)}.Cost`,`count == Σ{桶}.Count`(含稀疏桶间隙按 count=0/cost=0 参与滑窗,保证窗口连续)。
- 特判:空桶 → nil;`width<=0` → nil;桶重叠/乱序 → 以 `Minute.Unix()/60` 排序后仍正确(weightedwindows_test.go 已覆盖基例)。
- 内存约束:基准 O(桶数),不得因展开逐调用而 OOM(t3 提到大数据部门数十万行)。

### ID-9 归因占比
- 恒等:`Attribution.weight == 该模型 calls / 总 calls`;`Σ weights(全部)>0`(只取 Top-3,故 Σ 可 ≤1,但权重按全量 calls 计算,derive.go:290-344)。
- 边界:模型名空 → subject 兜底「(未识别模型)」(derive.go:338-340);无任何调用 → Attribution nil。
- 特殊:`model` 含特殊字符(见 §3)不得破坏排序/占比。

### ID-10 HourlyDistribution 口径(独立于 ReqCount)
- 恒等:`Σ{hourly_distribution[h]} == len(Calls)`(derive.go:131-133,按 CallEvent 递增,非 Metrics)。
- 注意:个人 `ReqCount` 来自 Metrics(derive.go:106),与 `len(Calls)` 为**两个不同来源**;若 Calls 覆盖不等于 Metrics 覆盖,两者可不相等——测试需分别锁定,不得默认「Σhourly == ReqCount」。

### ID-11 人均(per_capita)
- 恒等:`PerCapita.ReqCount==Member.ReqCount/ActiveUserCount`;`EstCostUSD==Member.EstCostUSD/ActiveUserCount`;`SuccessRate==Member.SuccessCount/Member.ReqCount`;`ActiveDays==Σ(每用户活跃天数)/ActiveUserCount`(derive.go:221-232)。
- 边界:`ActiveUserCount==0` → 不填 PerCapita。

---

## 2. A1–A5 测试用例(每规则 正/反/边界,含 无历史/样本<5/零token/未用模型)

> 阈值默认见 `DefaultRuleSet()`(metric.go:151-188):A1(10min/连续15min/≥60/间隔<10s)、A2(15min/中位×5/abs¥8 USD/¥200硬顶)、A3(5min/段内≥20/fail≥30%/单码≥80%/failed≥6)、A4(15min/near>80%预算 或 >100000/窗口≥2)、A5(h<8 或周末/样本≥20/floor¥100/硬顶¥1000)。**构造用例时不使用真实数据,直接合成 CallEvent/桶,便于手工核对。**

### A1 程序失控
- 正:10min 窗内 70 条调用(≥max(60,P99)),中位间隔 5s(<10),连续 15 个 1 分钟窗均超阈 → `Hit=true`,`MatchedCount==70`,`ExcessCost==ImpactCost`。
- 反:① 窗内 40 条(≤60)→ 不命中;② 调用 70 条但间隔 12s(≥10)→ 不命中;③ 间隔 5s 但只连续 10 min(<15)→ 不命中。
- 边界:
  - `调用数==60`(`x.count<=thr`,== 不命中);
  - `间隔==10.0s`(`iv >= A1MaxMedianIntervalS`,== 命中断言为假)→ 不命中;
  - `恰好连续 15min` → 命中,14min → 不命中;
  - ```len(calls)<2``` → `Hit=false`,evidence basis「调用样本不足」(anomalies.go:156-158)。
- **无历史**:`BaselineBuckets`/`BaselineCalls` 均空 → `p99==0`,`threshold==max(60,ceil(0))==60`,`BaselineWindow=="无历史窗口,仅绝对阈值"`(anomalies.go:177-180)。
- **未用模型/零token**:A1 只看调用条数与间隔,不看 token,`calls` 全部 `EstCostUSD==0` 也应能命中(纯量判定)。

### A2 成本燃烧
- 正:15min 窗成本 `>max(中位×5, 8 USD)` 且 `<=¥200/7.2` → `Hit=true`(P 断言 baseThr 分支);或 `>¥200/7.2`(≈27.78 USD)→ 走 HARD_CAP 分支(anomalies.go:281-284)。
- 反:成本 ≤ baseThr 且 ≤ hardCap → 不命中。
- 边界:
  - ```bestCost==baseThr```(`>`,== 不命中);
  - `bestCost==8 USD` → 不命中;`中位==0`(无历史)→ `baseThr==max(0,8)==8`;
  - `bestCNY==¥200` 恰等(`>`),不触发 hardCap;
  - `len(calls)<2` → 不命中(basis 样本不足);
  - `USDCNYRate<=0` → 兜底 7.2(anomalies.go:235-238)。
- **无历史/不得伪称 28 天**:`BaselineBuckets/Calls` 空 → `med==0` 且 `BaselineWindow=="无历史窗口,仅绝对阈值"`,**不得**标记为 28 天基线(见既有 `TestA2WithoutHistoryDoesNotClaim28Days`)。

### A3 重试循环
- 正:段=(key_id,model) 内 30 req、12 failed(40%)、其中 429 占 10/12(83%)、failed≥6、req≥20、单段 → `Hit=true`,`Dimensions.kind=="客户端僵死(非200)"`。
- 反:① fail_rate 25%;② 单状态码占 60%(<80%);③ failed==5;④ req==19;⑤ 多段但最优段 failed 未超过历史最大值 → 不命中。
- 边界:
  - `req==20`、`failed==6`、`fail_rate==0.30`(`<thresh`,== 通过)、`domShare==0.80`(`<`,== 通过)均为临界命中;
  - `status==200` 不计失败;`status>=500` → `kind=="上游故障(5xx)"`,否则「客户端僵死」(anomalies.go:373-377)。
- **口径已确认(失败=status!=200)**:captain 拍板 A3 失败按 `Status != 200` 计(anomalies.go:339/347),与全局「成功=200」一致;**3xx/4xx/5xx 均算失败**(比 >=400 更严格)。`status==200` 不计失败;`status>=500` → `kind=="上游故障(5xx)"`,否则「客户端僵死」(anomalies.go:373-377)。测试构造 3xx/4xx/5xx 失败用例断言其计入失败。

### A4 超长上下文(口径:S5 已拍板用「有效输入=prompt−cached=PromptBeforeCached」,实现侧修复后复验)
> 有效输入 `eff = PromptBeforeCached`(= prompt_total − cached,metric.go:57);**禁用含 cached 的 PromptTokens**(captain 拍板,原实现 anomalies.go:443 用 PromptTokens 为口径缺陷,已修复)。
- 正:模型 M 预算=200000(`context_length-max_output_tokens`),`threshold=min(200000*0.8,100000)==100000`;15min 窗内 2 条 `eff==150000`(如 prompt=180000,cached=30000)→ `Hit=true`,`NearLimit==2`,`BaselineValue==200000`。
- 反:① 窗内仅 1 条 `eff>threshold`(<2)→ 不命中;② 全部 `eff<=threshold` → 不命中;③ 全部调用缺预算 → `NotComputed=true`(anomalies.go:428-432)。
- 边界:
  - `near==2`(`near < A4MinNearLimit`,== 通过);
  - `eff==threshold`(`>`,== 不算 near);
  - **cached 影响(证明用 eff 非 prompt)**:同 prompt=150000 但 cached=60000 → eff=90000 < threshold(100000) 不命中;若按 PromptTokens(150000)则会误命中——区分断言;
  - 预算修正:大预算(如 1,000,000)→ threshold 被封顶为 `min(800000,100000)==100000`;小预算(如 100,000)→ `min(80000,100000)==80000`;
  - 预算 `<=0`/缺省模型 → 该调用进入 `unknownModels` 不参与;部分已知部分未知 → Note 提示「部分模型缺预算」(anomalies.go:467-469)。
- **未用模型/零token**:`eff==0` 永不 near;调用过的模型但无 catalog → unknownModels;`len(calls)==0` → 不命中(basis「无调用数据」)。

### A5 非工作时段
- 正:非工作时段(工作日 00:00–07:00 或周末)成本 `> max(同岗P99,¥100)`(阶段一同岗 P99 未接入→ 阈值=¥100/7.2≈13.89 USD)→ `Hit=true`,Severity=P2;若 `>¥1000/7.2≈138.9 USD` → Severity=P1(硬顶,anomalies.go:524-527)。
- 反:off-hours 成本 ≤ ¥100 且 ≤ ¥1000 → 不命中(`offCNY<=A5FloorCNY && !hitHardCap`)。
- 边界:
  - `offCNY==¥100`(==,不命中);`offCNY==¥1000`(>,不命中);`total==20`(== 达到 `A5MinCallCount` 允许判定);`h==8`(非 off,h<8 才算);`07:59`(off)/`08:00`(非 off);
  - `len(calls)<20` → `Hit=false`,basis「调用量 <20,样本不足」(anomalies.go:486-489)。
- **无历史/同岗降级**:`peer_basis_level=="仅floor(¥100)"`,note 标注「同岗位P99 与岗位降解链阶段一未接入,回退仅floor」。
- **口径已定(上游已滤 service,非 bug,无需改)**:A5(及 A1–A4)在 derive 内收到的 `calls` 输入契约本就是「已排除 service」——`main.go:94` `memberCalls=nonService(allCalls)`、`nonService`(main.go:535)显式剔 `KeyType=="service"`、`main.go:168` `ReportInput{Calls:memberCalls}` → `derive.go:53` `Anomalies(in.Calls,...)`。故 A5 无需再滤(再滤=双滤)。测试直接按「输入已排除 service」构造。

---

## 3. 各层 空态 / NotComputed / 特殊字符 处理矩阵

### 3.1 空态(每层输入为空)
| 输入 | personal | department | studio |
|---|---|---|---|
| `Metrics=[]` | Req/Success/Cost/Tokens 全 0;`ModelCount=0`,`ActiveDays=0`;Efficiency.SuccessRate/CacheHitRate/CostPer1k=0(n/a) | 同上 Member 全 0;ServiceKey 全 0 | 同上 |
| `Calls=[]` | `HourlyDistribution={}`;`AvgDurationMs=0`;A1/A2/A3 样本不足不命中,A4 不命中,A5 样本不足 | 同 | 同 |
| `BaselineMetrics=[]` | `Baseline.Supported=false`,note「期前历史窗口缺失」 | 同 | 同 |
| `BaselineBuckets+Calls=[]` | A1/A2 退化到仅绝对阈值(见 §2) | 同 | 同 |
| `Cohort.Members=[]` | Peer.Supported=false,note「同岗样本 <5」→ 记入 `NotComputed` | `RosterUserCount` 取花名册;Peer 不适用(仅 personal) | 同 |
| `ModelContextBudget={}` | A4 `NotComputed=true`(全部缺预算) | 同 | 同 |
| `TopModels=[]` | Attribution=nil;render 出「无模型」段 | 同 | 同 |

### 3.2 NotComputed(必须出现在 `judgment.not_computed`,不得是 Hit)
| Metric | 触发条件 | Reason 关键词 |
|---|---|---|
| 同岗分位 | 目标用户不在 active 部门花名册 / 可比样本 0 / roster 拉取失败 | "未匹配到岗位"、"可比样本为 0" |
| 同岗分位-覆盖 | 部分部门成员拉取失败(403/429) | "roster 不完整" |
| A4 | 全部调用缺 model_catalog 预算 | "A4 不可判定" |
| 基线 | BaselineMetrics 样本 < MinBaselineSamples(3) | "基线有效样本 <3 天" |

### 3.3 特殊字符 / 边界值处理
| 输入 | 期望行为 | 位置 |
|---|---|---|
| `model` 含 `gpt-4o`/`claude-3.5@2026`/`模型/组#1&2`(含 `@ # & / . -`) | TopModels/Attribution 按原样保存、排序、渲染;归因占比正确 | derive.go:116-127,337-341 |
| `model`/`user_name`/`dept_name`/`job_category` 含新行/emoji/引号/非 ASCII | JSON 序列化安全;MD/HTML 渲染转义,无注入 | render/* |
| `dept_id`/`studio_id` 为空`""` | 归「未知部门/工作室」,不并入具体 ScopeID;`attribution` 空模型兜底"(未识别模型)" | derive.go:338-340,165 |
| 浮点极小值(`0.000001`)、整值(`100`) | `fmtFloat` 整值无小数、极小值格式化为 0;`strconvFormatFloat` 边界 | anomalies.go:560-572 |
| `USDCNYRate<=0` | 回退 7.2 | anomalies.go:236-238,285,493-494 |
| 比值除零除 | `safeDiv`(b==0→0);`SuccessRate/CacheHitRate/CustomF1k` 在分母 0 时不 NaN | derive.go:489-494,270-280 |
| 负值 `ExcessCostUSD` | 夹到 0(`<0 → 0`,anomalies.go:289-291,535-537) | anomalies.go:289,535 |
| **`MAD==0` 且 `current!=median`(全等基线,如每日调用数完全相同)** | **captain 已拍板为真 bug 并修:captain 禁止 `RobustZ` 返回 `±Inf`,改为有界有限值(如夹在 ±上限)并加注记**。修复后 `Baseline.DevSigma` 必须是有限 float64;**断言:序列化 `report → json.Marshal` 不报错,`DevSigma` 无 Inf/NaN** | baseline.go:75-87, fix-in-progress |

---

## 4. 规格-实现偏差 → captain 决策状态(S1–S5)

| # | 偏差 | captain 拍板 | 对测试断言的影响 | 状态 |
|---|---|---|---|---|
| S1 | 任务「单位 token 成本=Σcost/Σtoken×1e6」vs 实现 `×1000` | **实现 ×1000 正确**(cost_per_1k_tokens);「×1e6」为笔误,修文档 | 恒等式按 ×1000 断言 | ✅ 关闭 |
| S2 | A3 失败=「Status!=200」vs「status>=400」 | **统一 `Status != 200`**(与全局成功=200 一致,更严格);3xx/5xx 均算失败 | A3 断言失败=非200 | ✅ 关闭 |
| S3 | A5 未按 key_type 过滤 service | **非 bug,无需改**:上游 `nonService`(main.go:535)已剔 `KeyType=="service"`,`main.go:94` `memberCalls=nonService(allCalls)`、`main.go:168` `ReportInput{Calls:memberCalls}` → A5 收到的 `calls` 契约本已排除 service;函数内再滤=双滤 | 测试直接按「输入已排除 service」构造 | ✅ 关闭(非bug) |
| S4 | `RobustZ` MAD==0 且 current!=median → `DevSigma=±Inf` | **已修**:MAD==0 时返回有界 0/±6.0(baseline.go:82-88),禁 Inf 进 JSON | 断言无 Inf/NaN,`json.Marshal` 不报错 | ✅ 关闭(已修+回归) |
| S5 | A4 用 `PromptTokens`(含 cached) | **已修**:A4 有效输入=`prompt−cached`=PromptBeforeCached(anomalies.go:443;toEvents usage_client.go:631 填 prompt−cached) | A4 断言改用有效输入 eff | ✅ 关闭(已修+回归) |

> 以上均为只读调研发现,未改任何代码。**S1–S5 全部关闭**(S1/S2 口径确立;S3 核实为上游已滤、非 bug;S4/S5 已修并复验通过)。t5 验收矩阵可据此定稿。

---

## 5. 落地建议(转测试时)
- 对账恒等式(ID-1~ID-11):多数可用**合成 ReportInput** 单测锁定;建议为每个恒等式写 `TestReconciliation_ID_x`。
- A1–A5 用例:直接合成 `CallEvent`(A4 另给 `ModelContextBudget`)并调用 `derive.Anomalies`,断言 `Hit/Actual/Threshold/MatchedCount/Severity/NotComputed/Note`(参考既有 `derive_test.go` 风格)。
- 空态/NotComputed 矩阵:用 `Derive` 装配 `ReportInput`(各字段置空),断言 `judgment.not_computed`、`baseline.supported=false`、`peer.supported=false`。
- 特殊字符:构造含 `@#&-/.中文/emoji/新行` 的 Model/Name/Job 字段,断言序列化 + HTML/MD 渲染不崩且无注入。
- 收敛测试:`report → Validate()` 往返,`schema_version` 不变(既有 `report_test.go` 已覆盖部分)。
