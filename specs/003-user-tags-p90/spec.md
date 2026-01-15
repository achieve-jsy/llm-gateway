# 用户标签 P90 相对阈值改造

## 背景

首页登录后展示的用户标签（施小救の性格档案），当前使用硬编码绝对阈值，导致多个标签触发率过高：

| 标签 | 当前阈值 | 触发人数/总活跃 | 触发率 |
|------|---------|----------------|--------|
| ⚡ 卷王本王 | ≥50次/天(固定除30) | 15/31 | 48% |
| 💎 省钱天才 | ≥25%缓存 | 18/31 | 58% |
| 🦄 模型集邮癖 | ≥5个模型 | 16/31 | 52% |
| 🎯 效率怪物 | ≥4:1输出比 | 1/31 | 3% |
| 🧠 深度思考家 | ≥30%推理 | 4/31 | 13% |

## 目标

改为 **P90 相对阈值**：后端计算全站用户各指标的 P90 值，前端用相对阈值判断，确保每个标签只有 Top 10% 的用户能获得。

## 数据分布（2026-06-22 生产数据）

按活跃天数计算日均（`总调用 / 活跃天数`，而非固定除 30）：

| 指标 | P50 | P75 | P90 | Max |
|------|-----|-----|-----|-----|
| 日均调用 | 237.8 | 595.4 | **790.9** | 3286 |
| 缓存命中率 | 37.9% | 62.8% | **71.0%** | 90.9% |
| 输出输入比 | 0.01 | 0.06 | **0.19** | 5.89 |
| 不同模型数 | 5 | 8.5 | **15** | 26 |
| 推理占比 | 0% | 2.8% | **45.5%** | 89.1% |

## 现状代码

### 前端标签逻辑

文件：`web/src/views/access/UnifiedEntryPage.vue`（第 235-347 行）

```typescript
// 当前：硬编码绝对阈值
if (dailyAvgCalls.value >= 50) {          // 卷王本王
if (cacheHitPct.value >= 25) {            // 省钱天才
if (outRatio.value >= 4) {                // 效率怪物
if (uniqueModelCount.value >= 5) {        // 模型集邮癖
if (reasoningShare.value >= 0.30) {       // 深度思考家
```

### 前端数据计算

```typescript
// 当前：固定除 30
const dailyAvgCalls = computed(() => 
  Math.round((total.value?.req_count || 0) / RANGE_DAYS)  // RANGE_DAYS = 30
)
```

### 后端 API

文件：`internal/observability/handler/usage_handler.go`

当前 `GET /api/v1/my/usage` 只返回当前用户数据，没有全站百分位。

## 设计方案

### 后端：新增百分位 API

**端点**：`GET /api/v1/my/usage/percentiles`

**响应**：
```json
{
  "data": {
    "p90_daily_avg_calls": 791,
    "p90_cache_hit_pct": 71.0,
    "p90_output_ratio": 0.19,
    "p90_unique_models": 15,
    "p90_reasoning_share": 0.455,
    "calculated_at": "2026-06-22T16:41:00+08:00"
  }
}
```

**SQL 实现**：
```sql
WITH user_stats AS (
  SELECT 
    user_id,
    SUM(req_count)::float / COUNT(DISTINCT stat_date) as daily_avg,
    CASE WHEN SUM(prompt_tokens) > 0 
         THEN (SUM(cached_tokens)::float / SUM(prompt_tokens)) * 100 
         ELSE 0 END as cache_pct,
    CASE WHEN SUM(prompt_tokens - cached_tokens) > 0 
         THEN SUM(completion_tokens)::float / SUM(prompt_tokens - cached_tokens) 
         ELSE 0 END as out_ratio,
    COUNT(DISTINCT model) as model_cnt,
    CASE WHEN SUM(completion_tokens) > 0 
         THEN SUM(reasoning_tokens)::float / SUM(completion_tokens) 
         ELSE 0 END as reasoning_share
  FROM usage_daily 
  WHERE stat_date >= CURRENT_DATE - INTERVAL '30 days'
  GROUP BY user_id 
  HAVING SUM(req_count) > 0
)
SELECT 
  ROUND(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY daily_avg)::numeric, 0) as p90_daily_avg_calls,
  ROUND(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY cache_pct)::numeric, 1) as p90_cache_hit_pct,
  ROUND(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY out_ratio)::numeric, 2) as p90_output_ratio,
  PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY model_cnt) as p90_unique_models,
  ROUND(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY reasoning_share)::numeric, 3) as p90_reasoning_share
FROM user_stats
```

**路由注册**：`internal/controlplane/router/router.go`，在 `/api/v1/my/usage` 旁增加 `/api/v1/my/usage/percentiles`

**Handler**：在 `internal/observability/handler/usage_handler.go` 新增 `MyPercentiles` 方法

**缓存**：百分位数据变化缓慢，可 Redis 缓存 1 小时

### 前端：改用相对阈值

**1. 调用百分位 API**

```typescript
// UnifiedEntryPage.vue
const percentiles = ref({
  p90_daily_avg_calls: 500,  // 默认值，API 返回后覆盖
  p90_cache_hit_pct: 60,
  p90_output_ratio: 0.5,
  p90_unique_models: 10,
  p90_reasoning_share: 0.3
})

onMounted(async () => {
  try {
    const res = await usageApi.getPercentiles()
    percentiles.value = res.data
  } catch (e) {
    console.warn('Failed to load percentiles, using defaults')
  }
})
```

**2. 修改日均计算**

```typescript
// 改为按活跃天数计算
const dailyAvgCalls = computed(() => {
  const totalCalls = total.value?.req_count || 0
  const activeDays = dayRows.value?.length || 1  // 有数据的天数
  return Math.round(totalCalls / activeDays)
})
```

**3. 改用 P90 阈值**

```typescript
// 卷王本王
if (dailyAvgCalls.value >= percentiles.value.p90_daily_avg_calls) {

// 省钱天才
if (cacheHitPct.value >= percentiles.value.p90_cache_hit_pct) {

// 效率怪物
if (outRatio.value >= percentiles.value.p90_output_ratio) {

// 模型集邮癖
if (uniqueModelCount.value >= percentiles.value.p90_unique_models) {

// 深度思考家
if (reasoningShare.value >= percentiles.value.p90_reasoning_share) {
```

**4. API 层**

```typescript
// web/src/api/usage.ts
export const usageApi = {
  // ... 现有方法
  
  getPercentiles() {
    return request.get('/my/usage/percentiles')
  }
}
```

### 时间类标签不变

深夜哲学家、晨型打工人、专一战士基于用户自身行为模式（高峰时段、模型集中度），不涉及全站对比，保持绝对阈值。

## 验收标准

1. 后端 API `/api/v1/my/usage/percentiles` 返回正确的 P90 值
2. 前端标签判断使用 P90 相对阈值
3. 日均调用改为按活跃天数计算
4. 标签触发率降至约 10%（P90）
5. 首页标签展示正常，无报错

## 技术要点

- 后端：新增 handler 方法，复用现有 `usage_daily` 表
- 前端：新增 API 调用，修改阈值判断逻辑
- 保持时间类标签（深夜/晨型/专一）不变
- 百分位数据可缓存 1 小时（用户增长不会剧烈波动）
