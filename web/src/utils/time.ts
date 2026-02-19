const TZ = 'Asia/Shanghai'

/**
 * 格式化 ISO 时间字符串为北京时间
 * 例: "2026-06-24T08:00:00Z" → "2026-06-24 16:00:00"
 */
export function fmtDateTime(s?: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleString('sv-SE', { timeZone: TZ })
}

/**
 * 提取日期部分
 * 例: "2026-06-24T08:00:00Z" → "2026-06-24"
 */
export function fmtDate(s?: string | null): string {
  if (!s) return '—'
  return s.split('T')[0]
}

/**
 * 格式化毫秒为可读时长
 * 例: 1234 → "1.2s", 45 → "45ms"
 */
export function fmtDuration(ms?: number | null): string {
  if (ms == null || ms < 0) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

/**
 * 格式化毫秒为紧凑时长（性能指标专用）
 * ≥1000ms 显示为秒（如 "1.2s"），否则显示毫秒（如 "856ms"）
 * 与 fmtDuration 逻辑一致，但不接受 null
 */
export function formatMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

/**
 * 获取 ISO 周标签
 * 例: new Date('2026-06-28') → "2026-W26"
 */
export function getISOWeekLabel(date: Date): string {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() + 3 - (d.getDay() + 6) % 7)
  const year = d.getFullYear()
  const week1 = new Date(year, 0, 4)
  const weekNum = 1 + Math.round(((d.getTime() - week1.getTime()) / 86400000 - 3 + (week1.getDay() + 6) % 7) / 7)
  return `${year}-W${String(weekNum).padStart(2, '0')}`
}

const MONTH_ABBR = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']

/**
 * 获取月份标签
 * 例: new Date('2026-06-28') → "Jun 2026"
 */
export function getMonthLabel(date: Date): string {
  return `${MONTH_ABBR[date.getMonth()]} ${date.getFullYear()}`
}

export interface TrendDataPoint {
  date: string
  cost: number
  calls: number
  errors: number
  total_tokens: number
  [key: string]: any
}

/**
 * 按周/月聚合趋势数据
 * granularity='day' 时直接返回原数据
 */
export function aggregateTrendData(
  data: TrendDataPoint[],
  granularity: 'day' | 'week' | 'month',
): TrendDataPoint[] {
  if (granularity === 'day' || !data.length) return data
  const groups = new Map<string, TrendDataPoint>()
  for (const d of data) {
    const dateStr = d.date
    if (!dateStr) continue
    const date = new Date(dateStr)
    const key = granularity === 'week' ? getISOWeekLabel(date) : getMonthLabel(date)
    if (!groups.has(key)) {
      groups.set(key, { date: key, cost: 0, calls: 0, errors: 0, total_tokens: 0 })
    }
    const g = groups.get(key)!
    g.cost         += d.cost         ?? 0
    g.calls        += d.calls        ?? 0
    g.errors       += d.errors       ?? 0
    g.total_tokens += d.total_tokens ?? 0
  }
  return Array.from(groups.values())
}
