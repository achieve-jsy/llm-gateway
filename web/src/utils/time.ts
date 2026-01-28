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
