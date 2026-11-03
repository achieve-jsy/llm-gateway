// analytics-tier.ts — 模型等级枚举与标签（对齐原型 model-analytics.js 的 TIER_LABEL）
// 等级值对应后端 call_logs.budget_tier_name_snapshot（空值落库为 'unclassified'）。

export const ANALYTICS_TIERS = [
  'L3-Premium',
  'L2-Advanced',
  'L1-Basic',
  'unclassified',
] as const

export type AnalyticsTier = (typeof ANALYTICS_TIERS)[number]

/** 等级枚举 → 展示标签（原型对齐：L3 · 高级 等） */
export const TIER_LABEL: Record<AnalyticsTier, string> = {
  'L3-Premium': 'L3 · 高级',
  'L2-Advanced': 'L2 · 进阶',
  'L1-Basic': 'L1 · 基础',
  'unclassified': '未分级',
}

/** 筛选下拉选项 */
export const TIER_OPTIONS: Array<{ label: string; value: string }> = ANALYTICS_TIERS.map(t => ({
  label: TIER_LABEL[t],
  value: t,
}))

/** 未知等级回退为原值 */
export function tierLabel(t: string): string {
  return TIER_LABEL[t as AnalyticsTier] ?? t
}
