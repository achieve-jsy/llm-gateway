/**
 * 统一数字格式化工具
 *
 * 规范（D-5）：
 * - 货币（USD）：$1,234.56（大额概览可用整数 $12,345）
 * - Token：≥1M 显示 1.2M，<1M 千分位
 * - 百分比：整数 72%（详情页可用 1 位小数 72.3%）
 * - hover tooltip 显示完整精度
 */

/**
 * 格式化 USD 金额
 * @param amount 金额（USD）
 * @param options.compact 是否使用紧凑模式（大额概览，整数）
 * @param options.decimals 小数位数（默认 2，compact 模式下默认 0）
 */
export function formatUSD(
  amount: number | null | undefined,
  options: { compact?: boolean; decimals?: number } = {},
): string {
  if (amount == null || isNaN(amount)) return '—'
  if (amount < 0) return '不限'

  const { compact = false, decimals } = options
  const d = decimals ?? (compact ? 0 : 2)

  return '$' + amount.toLocaleString('en-US', {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  })
}

/**
 * 格式化 USD 金额（完整精度，用于 tooltip）
 */
export function formatUSDFull(amount: number | null | undefined): string {
  if (amount == null || isNaN(amount)) return '—'
  if (amount < 0) return '不限'
  return '$' + amount.toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  })
}

/**
 * 格式化 Token 数量
 * @param count token 数
 * @param options.decimals M 级别时的小数位（默认 1）
 */
export function formatTokens(
  count: number | null | undefined,
  options: { decimals?: number } = {},
): string {
  if (count == null || isNaN(count)) return '—'
  if (count < 0) return '不限'

  const { decimals = 1 } = options

  if (count >= 1_000_000) {
    return (count / 1_000_000).toFixed(decimals) + 'M'
  }
  return count.toLocaleString('en-US')
}

/**
 * 格式化百分比
 * @param ratio 比率（0~1 或 0~100，自动判断）
 * @param options.decimals 小数位数（默认 0，详情页可传 1）
 * @param options.isPercent 输入是否已经是百分比值（true=0~100，false=0~1）
 */
export function formatPercent(
  ratio: number | null | undefined,
  options: { decimals?: number; isPercent?: boolean } = {},
): string {
  if (ratio == null || isNaN(ratio)) return '—'

  const { decimals = 0, isPercent = false } = options
  const pct = isPercent ? ratio : ratio * 100

  return pct.toFixed(decimals) + '%'
}

/**
 * 预算使用率颜色等级
 * @param ratio 使用率（0~1）
 * @returns 'success' | 'warning' | 'danger'
 */
export function budgetColorLevel(ratio: number | null | undefined): 'success' | 'warning' | 'danger' {
  if (ratio == null || isNaN(ratio)) return 'success'
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
}

/**
 * 预算额度显示（-1=不限，0=已禁用，>0=正常金额）
 */
export function formatBudgetAmount(amount: number | null | undefined): string {
  if (amount == null || isNaN(amount)) return '未设置'
  if (amount < 0) return '不限'
  if (amount === 0) return '$0（已禁用）'
  return formatUSD(amount)
}

/**
 * 计算当前月份剩余天数
 */
export function remainingDaysInMonth(): number {
  const now = new Date()
  const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
  return lastDay - now.getDate()
}

/**
 * 格式化当前计费周期标注
 * @returns 如 "2026年6月 · 剩余14天"
 */
export function formatCurrentPeriod(): string {
  const now = new Date()
  const year = now.getFullYear()
  const month = now.getMonth() + 1
  const remaining = remainingDaysInMonth()
  return `${year}年${month}月 · 剩余${remaining}天`
}

/**
 * 超额维度中文标签
 */
export function budgetDimensionLabel(dimension: 'daily' | 'weekly' | 'monthly'): string {
  const labels: Record<string, string> = {
    daily: '日限额',
    weekly: '周限额',
    monthly: '月限额',
  }
  return labels[dimension] || dimension
}

/**
 * 超额恢复时间提示
 */
export function budgetRecoveryHint(dimension: 'daily' | 'weekly' | 'monthly'): string {
  const hints: Record<string, string> = {
    daily: '明天 00:00 自动恢复',
    weekly: '下周一 00:00 自动恢复',
    monthly: '下月 1 号自动恢复，或申请扩容',
  }
  return hints[dimension] || ''
}
