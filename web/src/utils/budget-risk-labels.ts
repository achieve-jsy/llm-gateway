const LIMIT_SOURCE_LABELS: Record<string, string> = {
  base_limit: '总预算',
  tier_base_limit: '等级预算',
  temporary_override: '临时提额',
  block_event: '阻断事件',
}

/** 将风险事件 limit_source 枚举映射为中文业务文案 */
export function limitSourceLabel(source: string | null | undefined): string {
  if (!source) return '—'
  return LIMIT_SOURCE_LABELS[source] ?? source
}
