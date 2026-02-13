import type { OpsSeverity } from '@/api/ops'
import type { AlertEventStatus } from '@/api/ops'

export function getSeverityClass(severity: OpsSeverity | string): string {
  const classes: Record<string, string> = {
    P0: 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400',
    P1: 'bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-400',
    P2: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400',
    P3: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400'
  }
  return classes[String(severity || '')] || classes.P3
}

export function getSeverityTagType(severity: OpsSeverity | string): 'danger' | 'warning' | '' | 'info' | 'success' {
  switch (severity) {
    case 'P0': return 'danger'
    case 'P1': return 'warning'
    case 'P2': return ''
    case 'P3': return 'info'
    default: return 'info'
  }
}

export function truncateMessage(msg: string, maxLength = 80): string {
  if (!msg) return ''
  return msg.length > maxLength ? msg.substring(0, maxLength) + '...' : msg
}

export function formatDateTime(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (Number.isNaN(d.getTime())) return dateStr
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mi = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${mi}:${ss}`
}

export function sumNumbers(values: Array<number | null | undefined>): number {
  return values.reduce<number>((acc, v) => {
    const n = typeof v === 'number' && Number.isFinite(v) ? v : 0
    return acc + n
  }, 0)
}

export function parseTimeRangeMinutes(range: string): number {
  const trimmed = (range || '').trim()
  if (!trimmed) return 60
  if (trimmed.endsWith('m')) {
    const v = Number.parseInt(trimmed.slice(0, -1), 10)
    return Number.isFinite(v) && v > 0 ? v : 60
  }
  if (trimmed.endsWith('h')) {
    const v = Number.parseInt(trimmed.slice(0, -1), 10)
    return Number.isFinite(v) && v > 0 ? v * 60 : 60
  }
  if (trimmed.endsWith('d')) {
    const v = Number.parseInt(trimmed.slice(0, -1), 10)
    return Number.isFinite(v) && v > 0 ? v * 1440 : 60
  }
  return 60
}

export function formatHistoryLabel(date: string | undefined, timeRange: string): string {
  if (!date) return ''
  const d = new Date(date)
  if (Number.isNaN(d.getTime())) return ''
  const minutes = parseTimeRangeMinutes(timeRange)
  if (minutes >= 24 * 60) {
    return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export function formatByteRate(bytes: number, windowMinutes: number): string {
  const seconds = Math.max(1, (windowMinutes || 1) * 60)
  const rate = bytes / seconds
  if (rate >= 1024 * 1024) return `${(rate / (1024 * 1024)).toFixed(1)} MB/s`
  if (rate >= 1024) return `${(rate / 1024).toFixed(1)} KB/s`
  return `${rate.toFixed(0)} B/s`
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return '-'
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60000)}m${Math.round((ms % 60000) / 1000)}s`
}

export function formatNumber(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '-'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

/* ========== 告警状态/严重级别/日志级别 → CSS class（配对 global.scss 的 .ops-*-badge 体系） ========== */

export function getAlertStatusClass(status: AlertEventStatus | string): string {
  switch (status) {
    case 'firing':          return 'ops-status-badge ops-status-badge--firing'
    case 'resolved':        return 'ops-status-badge ops-status-badge--resolved'
    case 'manual_resolved': return 'ops-status-badge ops-status-badge--manual-resolved'
    default:                return 'ops-status-badge'
  }
}

export function getAlertStatusLabel(status: AlertEventStatus | string): string {
  switch (status) {
    case 'firing':          return '告警中'
    case 'resolved':        return '已恢复'
    case 'manual_resolved': return '手动恢复'
    default:                return String(status)
  }
}

export function getSeverityBadgeClass(severity: OpsSeverity | string): string {
  const s = String(severity || 'P3')
  return `ops-severity-badge ops-severity-badge--${s}`
}

export function getLogLevelClass(level: string): string {
  const upper = (level || '').toUpperCase()
  switch (upper) {
    case 'DEBUG': return 'ops-log-level ops-log-level--DEBUG'
    case 'INFO':  return 'ops-log-level ops-log-level--INFO'
    case 'WARN':  return 'ops-log-level ops-log-level--WARN'
    case 'ERROR': return 'ops-log-level ops-log-level--ERROR'
    default:      return 'ops-log-level'
  }
}

export function getHttpStatusClass(status: number | null | undefined): string {
  if (status == null) return 'ops-http-status'
  if (status >= 200 && status < 300) return 'ops-http-status ops-http-status--2xx'
  if (status >= 400 && status < 500) return 'ops-http-status ops-http-status--4xx'
  if (status >= 500 && status < 600) return 'ops-http-status ops-http-status--5xx'
  return 'ops-http-status'
}

/* ========== 严重级别/状态 → el-tag type（兼容 Element Plus） ========== */

export function severityToTagType(sev: OpsSeverity | string): 'danger' | 'warning' | 'info' {
  if (sev === 'P0' || sev === 'P1') return 'danger'
  if (sev === 'P2') return 'warning'
  return 'info'
}

export function alertStatusToTagType(st: AlertEventStatus | string): 'danger' | 'success' {
  return st === 'firing' ? 'danger' : 'success'
}

export function logLevelToTagType(level: string): 'info' | 'success' | 'warning' | 'danger' {
  switch ((level || '').toUpperCase()) {
    case 'DEBUG': return 'info'
    case 'INFO':  return 'success'
    case 'WARN':  return 'warning'
    case 'ERROR': return 'danger'
    default:      return 'info'
  }
}

/* ========== 健康阈值色（与 variables.scss --health-ok/warn/critical/idle 对齐） ========== */

/** 健康色常量（与 CSS 变量同名同色值，便于 JS 端内联使用） */
export const HEALTH_COLORS = {
  OK:       '#059669',
  WARN:     '#D97706',
  CRITICAL: '#DC2626',
  IDLE:     '#94A3B8',
} as const

/**
 * 阈值染色：value 越低越好（如 CPU%、内存%、DB 使用率、goroutine 数）。
 * - value <= warn 阈值 → OK
 * - value <= critical 阈值 → WARN
 * - 否则 → CRITICAL
 */
export function getThresholdColor(
  value: number,
  warnThreshold: number,
  criticalThreshold: number,
): string {
  if (!Number.isFinite(value) || value <= warnThreshold) return HEALTH_COLORS.OK
  if (value <= criticalThreshold) return HEALTH_COLORS.WARN
  return HEALTH_COLORS.CRITICAL
}

/**
 * 阈值染色：value 越高越好（如 SLA 成功率、健康评分）。
 * - value >= okThreshold → OK
 * - value >= warnThreshold → WARN
 * - 否则 → CRITICAL
 */
export function getThresholdColorInverse(
  value: number,
  warnThreshold: number,
  okThreshold: number,
): string {
  if (!Number.isFinite(value)) return HEALTH_COLORS.CRITICAL
  if (value >= okThreshold) return HEALTH_COLORS.OK
  if (value >= warnThreshold) return HEALTH_COLORS.WARN
  return HEALTH_COLORS.CRITICAL
}

/** 健康评分（0-100）染色，支持 idle 状态 → 灰色 */
export function getHealthScoreColor(score: number, isIdle = false): string {
  if (isIdle) return HEALTH_COLORS.IDLE
  return getThresholdColorInverse(score, 60, 90)
}
