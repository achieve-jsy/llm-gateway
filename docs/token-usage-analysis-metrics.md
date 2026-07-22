# Token 使用分析 · 指标字典（对齐 v0.6）

> 配套：`docs/token-usage-analysis-report-design.md`（规范）、`...-sample.md`（样张）、`...-implementation.md`（实现）。
> 本文回答一个问题：**v0.6 要生成哪些指标，每个怎么算、来自哪、在哪一层、服务哪一节。**
> 分层：L0 原子字段 → L1 派生指标 → L2 基线/分位 → L3 判定指标（五种情况+严重异常）→ L4 评估指标（四象限）→ L5 报告展示指标。
> 口径铁律：成功=`status=200`；个人口径排除 `key_type='service'`；`total_tokens = prompt + completion + reasoning`（不含 cached/cache_write）；时区 `Asia/Shanghai`；`est_cost` 为估算非结算；CNY=USD×rate；阈值先当口径，8–12 周后校准。

---

## L0 原子字段（直接取自 call_logs，不算，只取）

这些是所有指标的输入。全部已在 `call_logs`（除标「需采集」）。

| 字段 | 含义 | 来源 | 状态 |
| --- | --- | --- | --- |
| `prompt_tokens` | 输入总量（含缓存） | call_logs | ✅ |
| `cached_tokens` | 缓存读取 Token | call_logs | ✅ |
| `cache_write_tokens` | 缓存写入 Token | call_logs | ✅ |
| `completion_tokens` | 输出 Token | call_logs | ✅ |
| `reasoning_tokens` | 推理 Token | call_logs | ✅ |
| `input_cost_usd` | 非缓存输入费 | call_logs | ✅ |
| `cache_read_cost_usd` | 缓存读费 | call_logs | ✅ |
| `output_cost_usd` | 输出费 | call_logs | ✅ |
| `est_cost_usd` | 估算总费 | call_logs | ✅ |
| `status` | 成功/失败（200=成功） | call_logs | ✅ |
| `model` | 真实模型 | call_logs | ✅ |
| `key_type` | personal/service | call_logs | ✅ |
| `user_id/dept_id/studio_id` | 主体身份 | call_logs | ✅ |
| `ts` | 时间戳 | call_logs | ✅ |
| `session_id` | 会话号 | conversation_id | ⚠️ 需落库 |
| `session_id_source` | client/inferred | 采集 | ⚠️ 需采集 |
| `turn_index` | 会话内轮次序号 | 采集 | ⚠️ 需采集 |
| `reset_flag` | 该轮是否清空历史 | 采集 | ⚠️ 需采集 |
| `task_kind`/`scene` | 任务/场景标签 | inbound_endpoint 映射 | ⚠️ 需采集 |
| `job_category/family/title` | 岗位 | users 表 JOIN | ✅（047） |
| 模型档位 tier | 高阶/中/轻量 | model_catalog + 模型组等级 | ✅ |
| 模型单价 | 输入/缓存/输出单价 | model_catalog | ✅ |

> 交互三类（3.2/3.3）与挤牙膏强依赖 `session_id/turn_index/reset_flag/task_kind`；缺失时用「同 user+同 key+30min 切分」近似，结论一律「疑似」。

---

## L1 派生指标（第 2 节：成本 + 效率的基础量）

### L1-A 成本三分（第 2 节「成本按计费项」）

| 指标 | 公式 | 口径 | 服务 |
| --- | --- | --- | --- |
| `uncached_input_cost` | `SUM(input_cost_usd)` | 非缓存输入块 | 成本构成 |
| `cache_cost` | `SUM(cache_read_cost_usd)`（+可选 cache write 费） | 缓存块 | 成本构成 |
| `output_cost` | `SUM(output_cost_usd)` | 输出块 | 成本构成 |
| `total_cost` | `SUM(est_cost_usd)` | 总成本 | 全部 |
| `failed_cost` | `SUM(est_cost_usd) FILTER (status<>200)` | 无效消耗（标记，不扣减、不新增块） | 成本构成注记 |
| `cost_bucket_share` | 各块 / total_cost | 三块占比 | 成本构成条 |
| `cost_main_bucket` | argmax(三块) | 钱主要落哪块 | 结论第一句 |
| `wow_cost_bucket_delta` | 本周块 − 上周块 | 相对上周变化 | 成本构成 |

> **模型不是第四块**：模型是单价放大器，用「成本构成 × 模型档位」切片表达（见 L1-D）。

### L1-B Token 结构（第 5 节基础数据）

| 指标 | 公式 | 口径 |
| --- | --- | --- |
| `total_tokens` | `prompt + completion + reasoning`（不含 cached/cache_write） | 三处统一 |
| `input_tokens_uncached` | `prompt - cached - cache_write`（≥0） | 非缓存输入量 |
| `cached_tokens` / `output_tokens` / `reasoning_tokens` | 直接聚合 | 结构分量 |
| `token_bucket_share` | 各分量 / total_tokens | Token 结构条 |

### L1-C 效率基础量（第 2 节「效率按浪费机制」）

| 指标 | 公式 | 落在哪块钱 | 服务 |
| --- | --- | --- | --- |
| `cache_hit_rate` | `cached_tokens / NULLIF(prompt_tokens,0)` | 输入/缓存 | 上下文(3.1) |
| `avg_input_ctx` | `SUM(prompt_tokens) / req_count` | 输入/缓存 | 上下文(3.1) |
| `output_input_ratio` | `completion_tokens / NULLIF(prompt-cached,0)` | 输出 | 生成(3.4) |
| `reasoning_share` | `reasoning_tokens / NULLIF(completion_tokens,0)` | 输出 | 生成(3.4) |
| `cost_per_1m` | `est_cost_usd / NULLIF(total_tokens/1e6,0)` | — | 选型(3.5) |
| `high_tier_cost_ratio` | 高阶模型成本 / total_cost | 三块乘贵单价 | 选型(3.5) |

### L1-D 模型档位切片（选型放大器）

| 指标 | 公式 | 服务 |
| --- | --- | --- |
| `model_cost` | `SUM(est_cost_usd) GROUP BY model` | 成本构成×档位表 |
| `model_bucket_split` | 每模型的 input/cache/output 费 | 表列 |
| `model_tier` | model → 高阶/中/轻量（catalog+组等级） | 表首标注 |
| `model_cost_share` | model_cost / total_cost | 各模型费用占比（第 5 节基础数据） |

### L1-E 会话维度指标（第 2 节「交互必须在会话上量」）

| 指标 | 公式 | 服务 |
| --- | --- | --- |
| `session_count` | 去重会话数 | 交互(3.2/3.3) |
| `turns_per_session` | 会话内请求数 | 交互 |
| `turns_median / turns_p90` | 会话轮次中位/P90 | 挤牙膏(3.2) |
| `session_prompt_seq` | 会话内每轮 prompt_tokens 序列 | 3.1/3.3 |
| `session_input_monotonic` | prompt 序列单调不降比例 | 3.3 |
| `session_cache_hit` | 会话内 `Σcached/Σprompt` | 3.3 |
| `session_has_reset` | 任一轮 reset_flag=true | 3.3 |
| `session_task_switches` | 相邻 task_kind 不同次数 | 3.3 |
| `session_short_input_ratio` | 短输入轮(<30)占比 | 3.2 |
| `session_cost` | 会话内 est_cost / input / cache / output 费 | 交互费用 |

### L1-F 基础运营量（第 5 节基础数据）

| 指标 | 公式 |
| --- | --- |
| `req_count` | `COUNT(*)` |
| `success_count` | `COUNT(*) FILTER (status=200)` |
| `error_count` | `COUNT(*) FILTER (status<>200)` |
| `active_days` | `COUNT(DISTINCT date(ts))` |
| `model_count` | `COUNT(DISTINCT model)` |
| `active_users`（单元） | `COUNT(DISTINCT user_id)` |
| `per_capita_cost`（单元） | `total_cost / active_users` |
| `service_cost`（单元） | 程序 Service Key 成本，单列 |

---

## L2 基线 / 分位（第 2 节「对照本人历史和同岗位」）

| 指标 | 公式 | 口径 | 服务 |
| --- | --- | --- | --- |
| `self_28d_baseline` | 本人近 28 天中位 + 1.4826×MAD | 纵向 | 分析顺序、结论 |
| `model_prompt_p25` | 分模型 `percentile_cont(0.25) prompt` | 分模型 | 输出失控(3.4) |
| `model_completion_p75` | 分模型 `percentile_cont(0.75) completion` | 分模型 | 输出失控(3.4) |
| `position_prompt_p25/p50` | 分岗位输入分位 | 同岗 | 杀鸡用牛刀(3.5) |
| `position_cost_median` | 同岗成本中位 | 同岗 | 对照，不跨岗位比绝对值 |
| `org_cost_median` | 组织成员成本中位 | 部门/工作室 | 四象限 X 轴归一 |
| `org_issue_ratio_median` | 组织成员问题占比中位 | 部门/工作室 | 四象限 Y 十字线 |

> 全部用**相对基准**，不用绝对阈值；`org_*` 部门图用部门中位、工作室图用工作室中位。

---

## L3 判定指标（第 3 节：五种情况 + 严重异常）

每种情况统一产出：`hit_count`（次数）、`share`（占比）、`attributed_cost`（归因费用）、`verdict`（confirmed/suspected/not_hit/not_computed）、`evidence`、`excluded`。**看次数占比和费用占比，不看绝对值。**

### 3.1 前缀污染（上下文）

| 指标 | 定义 |
| --- | --- |
| 判定 | 输入 >500k 且连续约 3 次命中率 <30% 且这几轮输入不短 |
| `hit_count` | 命中段请求数 |
| `share` | 命中段请求数 ÷ 总请求数 |
| `attributed_cost` | 命中段 input_cost + cache_cost |
| 看 | 输入 Token、`cache_hit_rate`、连续低命中段费用 |
| 排除 | 超长但命中稳定 → 转 3.3 |

### 3.2 挤牙膏（交互）

| 指标 | 定义 |
| --- | --- |
| 判定 | 当轮输入 <30 且会话轮次 ≥8 且同一件事 |
| `hit_count` | 命中会话数 |
| `share` | 命中会话数 ÷ 总会话数 |
| `attributed_cost` | 会话额外轮次的 input+cache 费 |
| 看 | 当轮输入、会话轮次、这类会话总费用 |
| 排除 | 超短但一轮结束 = 正常短问 |

### 3.3 多任务不切会话（交互）

| 指标 | 定义 |
| --- | --- |
| 判定 | 超长 且 命中不差 且 无 reset 且 任务在切 |
| `hit_count` | 命中会话数 |
| `share` | 命中会话数 ÷ 总会话数 |
| `attributed_cost` | 任务切换后继承旧历史的 input+cache 费 |
| 看 | 输入单调上升、reset、缓存命中、是否另起一件事、输入+缓存费 |
| 排除 | 同任务连续工具轮（主题未切）= 正常复用 |
| 疑似 | 缺意图/任务标签只标疑似 |

### 3.4 输出失控（生成）

| 指标 | 定义 |
| --- | --- |
| 判定 | 输入 < 同模型/同岗 P25 且 输出 > 同模型 P75，或输出/输入 >20；反复出现或叠贵模型 |
| `hit_count` | 命中请求数 |
| `share` | 命中请求数 ÷ 总请求数 |
| `attributed_cost` | 超基准输出量的输出费 |
| 看 | 输入、输出、输出/输入比(分模型 P50/P90)、输出费 |
| 排除 | 一次成功长输出 + 轻量模型 = 可接受 |

### 3.5 杀鸡用牛刀（选型）

| 指标 | 定义 |
| --- | --- |
| 判定 | 像简单任务（输入短、输出不长、一轮结束）且走高阶模型 |
| `hit_count` | 疑似简单请求中高阶次数 |
| `share` | 疑似简单请求中高阶次数占比 |
| `attributed_cost` | 相对轻量模型的价格溢价（非整笔） |
| 看 | 是否简单任务、是否高阶、相对轻量模型溢价 |
| 排除 | 长上下文/高轮次/复杂任务用高阶 = 合理 |
| 疑似 | 无场景标签只标疑似 |

### 3.6 严重异常（与工作无关占比）

| 指标 | 定义 |
| --- | --- |
| `unrelated_cost` | 可判定与工作无关的消耗金额 |
| `unrelated_cost_ratio` | `unrelated_cost / 个人总成本` |
| 判定 | 占比 > 20% |
| 展示 | 金额、占比、主要时段/模型/Key |
| 口径 | 凌晨大量任务未备案、明显非工作场景（可计算） |
| 状态 | `pending_review` 待核查（不自动认定违纪） |
| 排除 | 已备案夜间批处理/跨时区/发布值班 |

### 判定的横切规则（第 2 节）

- **单信号只标记，两个条件同时成立才定性。**
- 缺会话/场景标签 → `verdict=suspected`。
- 字段覆盖不足 → `verdict=not_computed`（≠ 未命中）。
- **归因费用防重复**：`Σ attributed_cost ≤ total_cost`，重叠费用记 `overlap_cost` 不进合计（详见实现文档 §5）。

---

## L4 评估指标（第 4 节：成员整合评估 / 四象限）

| 指标 | 公式 | 服务 |
| --- | --- | --- |
| `cost_multiple` | 个人 total_cost ÷ 组织成员成本中位数 | X 轴（避免极端值撑开） |
| `issue_cost_ratio` | 五种情况 `Σ attributed_cost` ÷ 个人 total_cost | Y 轴（问题消耗占比） |
| `dominant_issue` | 五种情况中 attributed_cost 最大者；全低 → 用法正常 | 点颜色 |
| `dominant_issue_cost` | 主导类型对应费用 | 点选详情 |
| `quadrant` | f(cost_multiple ⋛ 1, issue_cost_ratio ⋛ Y中位) | 右下/右上/左上/左下 |
| `safety_anomaly` | 是否命中 3.6 | 点描边（不混进五色） |
| `suspected_only` | 命中情况全为 suspected | 虚边 |
| `calls` | 调用次数 | 点大小（可选） |
| `position` | 岗位 | 岗位分面 |
| `dept`（工作室） | 部门 | 点形区分 |
| `cluster`（分群） | 五种情况费用占比向量聚类 | 补充图 4.2 |

- 十字线：X 用中位倍数 `=1`，Y 用**组织问题占比中位数**（不是固定 50%）。
- 四象限读法（第 4.1 表）：右下=活多用法正常（不干预）、右上=优先看（按颜色对上第 3 节动作）、左上=教用法不问责、左下=不作重点。
- **不按总成本排序当结论**；结论写「谁在哪个象限、主导哪种情况」。

---

## L5 报告展示指标（第 5 节：三份报告）

### 5.0 三处相同的基础数据（个人/部门/工作室口径一致）

`total_cost`、`uncached_input/cache/output` Token 及费用、各模型费用占比(`model_cost_share`)、`req_count`。部门/工作室可附 `active_users`、`per_capita_cost`。

### 5.1 个人报告

| 块 | 用到的指标 |
| --- | --- |
| 基础数据 | L1-A total_cost、L1-B Token 结构、model_cost_share、req_count |
| 严重异常（有才出现） | L3-3.6：unrelated_cost / ratio / 时段/模型/Key / pending_review |
| 成本构成 | L1-A 三块 + share + wow 变化 |
| 效率与情况 | L3-3.1~3.5：命中类、share、attributed_cost、一句建议；都未命中→用法正常 |
| 结论 | cost_main_bucket + issue_cost_ratio + dominant_issue（两句话） |

### 5.2 部门报告

| 块 | 用到的指标 |
| --- | --- |
| 基础数据 | 5.0 + active_users + per_capita_cost + service_cost（单列） |
| 严重异常置顶 | 命中 3.6 者每人一行：姓名、unrelated_cost、ratio、待核查 |
| 整合评估图 | L4 全部（相对部门中位）+ 分群可选 |
| 存在问题的人 | 每人：quadrant、total_cost、issue_cost_ratio、dominant_issue、dominant_issue_cost、建议 |
| 正常使用的人 | 姓名、total_cost、total_tokens、req_count、结论「用法正常」（不展开五类） |

### 5.3 工作室报告

| 块 | 用到的指标 |
| --- | --- |
| 工作室基础数据 | 5.0 |
| 各部门基础数据 | 每部门 5.0 同字段（对照构成，不排名） |
| 严重异常置顶 | 全工作室命中 3.6 者 + 所属部门，口径同 5.2 |
| 整合评估图 | L4（相对工作室中位）+ 点形区分部门 |
| 各部门存在问题的人 | 按部门分组，字段同 5.2「存在问题的人」 |

### 人群划分指标（第 5 节）

| 指标 | 定义 |
| --- | --- |
| `is_problem_member` | `issue_cost_ratio > org 中位`（右上/左上）或被 3.1–3.5 定性 或 命中 3.6 |
| `is_normal_member` | 其余；右下（成本高、问题低）算正常，不进问题清单 |

---

## 指标总表（一屏速查）

```text
L0 原子:  prompt/cached/cache_write/completion/reasoning
          input_cost/cache_read_cost/output_cost/est_cost
          status/model/key_type/user/dept/studio/ts
          [需采集] session_id/source/turn_index/reset_flag/task_kind
          [JOIN] job_*  [catalog] model_tier/单价

L1 派生:  成本三分(uncached_input/cache/output/failed/total + share + main + wow)
          Token结构(total/uncached_input/cached/output/reasoning + share)
          效率(cache_hit_rate/avg_input_ctx/output_input_ratio/reasoning_share/
               cost_per_1m/high_tier_cost_ratio)
          模型切片(model_cost/bucket_split/tier/cost_share)
          会话(session_count/turns_median/p90/prompt_seq/monotonic/cache_hit/
               has_reset/task_switches/short_input_ratio/session_cost)
          运营(req/success/error/active_days/model_count/active_users/per_capita/service_cost)

L2 基线:  self_28d_baseline / model_prompt_p25 / model_completion_p75
          position_prompt_p25_p50 / position_cost_median
          org_cost_median / org_issue_ratio_median

L3 判定:  3.1前缀污染 3.2挤牙膏 3.3多任务不切会话 3.4输出失控 3.5杀鸡用牛刀
          → 每类: hit_count / share / attributed_cost / verdict / evidence / excluded
          3.6严重异常: unrelated_cost / ratio(>20%) / 时段模型Key / pending_review
          横切: overlap_cost / 双条件定性 / 疑似 / not_computed / Σattributed≤total

L4 评估:  cost_multiple(X) / issue_cost_ratio(Y) / dominant_issue(色) /
          dominant_issue_cost / quadrant / safety_anomaly(描边) /
          suspected_only(虚边) / calls(大小) / position(分面) / dept(点形) / cluster

L5 报告:  基础数据(5.0) + 个人(5.1) + 部门(5.2) + 工作室(5.3)
          人群: is_problem_member / is_normal_member
```

---

## 覆盖校验：v0.6 每条 → 指标

| v0.6 | 对应指标 |
| --- | --- |
| §1 成本维度 | L1-A 成本三分 |
| §1 效率维度 | L3 五种情况 attributed_cost |
| §2 成本按计费项（输入/缓存/输出+无效消耗） | uncached_input/cache/output_cost + failed_cost |
| §2 模型是放大器非第四块 | L1-D 模型切片 + high_tier_cost_ratio |
| §2 效率四角度（上下文/交互/生成/选型） | cache_hit_rate·avg_input_ctx / 会话指标 / output_input_ratio / cost_per_1m·tier |
| §2 交互在会话上量、30min 近似 | L1-E 会话指标 + session_id_source=inferred |
| §2 分析顺序、单信号只标记、双条件定性、对本人+同岗、疑似 | L2 基线 + L3 verdict 横切规则 |
| §3.1–3.5 看/判定/排除/占比 | L3 各条 hit_count/share/attributed_cost/evidence/excluded |
| §3.6 严重异常 20% 待核查 | unrelated_cost_ratio + pending_review |
| §4.1 两轴/中位十字/四象限/颜色/描边/大小/分面/点选 | L4 全部 |
| §4.2 分群/疑似虚边 | cluster + suspected_only |
| §5.0 三处相同基础数据 | 5.0 指标组 |
| §5.1/5.2/5.3 三份报告块 | L5 各表 |
| 全程不排序当结论、只给线索 | is_problem_member 用问题占比中位、3.6 pending_review |
```
