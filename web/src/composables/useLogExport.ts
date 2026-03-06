import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'
import type { LogRow } from '@/types/usage-log'

/**
 * CSV 导出列定义
 * - key: LogRow 上的字段名
 * - label: CSV 表头显示文本
 * - format: 可选格式化函数，默认原值输出
 */
export interface ExportColumn {
  key: keyof LogRow | string
  label: string
  format?: (value: unknown, row: LogRow) => string
}

/**
 * 默认导出列 —— 基于 MyUsagePage 的最佳实践（预定义有意义的列）
 */
export const DEFAULT_EXPORT_COLUMNS: ExportColumn[] = [
  { key: 'request_id', label: '请求ID' },
  { key: 'ts', label: '时间' },
  { key: 'model', label: '模型' },
  { key: 'key_name', label: 'Key' },
  { key: 'key_type', label: 'Key类型' },
  { key: 'user_name', label: '用户' },
  { key: 'status', label: '状态' },
  { key: 'duration_ms', label: '耗时(ms)' },
  { key: 'first_token_ms', label: 'TTFT(ms)' },
  { key: 'prompt_tokens', label: '输入Token' },
  { key: 'completion_tokens', label: '输出Token' },
  { key: 'cached_tokens', label: '缓存Token' },
  { key: 'est_cost_usd', label: '成本(USD)' },
]

/**
 * CSV 字段转义 —— 处理引号、逗号、换行符
 */
function csvEscape(v: unknown): string {
  const s = v == null ? '' : String(v)
  if (/["",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`
  return s
}

/**
 * 调用记录 CSV 导出 composable
 *
 * @param rows - 当前页面展示的调用记录（响应式）
 * @param options.columns - 导出列定义，默认使用 DEFAULT_EXPORT_COLUMNS
 * @param options.filename - 导出文件名（不含 .csv），支持函数动态生成
 */
export function useLogExport(
  rows: Ref<LogRow[]>,
  options: {
    columns?: ExportColumn[]
    filename?: string | (() => string)
  } = {},
) {
  const columns = options.columns ?? DEFAULT_EXPORT_COLUMNS

  function getFilename(): string {
    if (typeof options.filename === 'function') return options.filename()
    return options.filename ?? `logs_${new Date().toISOString().slice(0, 10)}`
  }

  function handleExport() {
    if (!rows.value.length) return

    const header = columns.map(c => csvEscape(c.label)).join(',')
    const body = rows.value
      .map(row =>
        columns
          .map(c => {
            const raw = (row as any)[c.key]
            const formatted = c.format ? c.format(raw, row) : raw
            return csvEscape(formatted)
          })
          .join(','),
      )
      .join('\n')

    // BOM 头确保 Excel 正确识别 UTF-8
    const blob = new Blob(
      ['\ufeff' + [header, body].join('\n')],
      { type: 'text/csv;charset=utf-8;' },
    )

    Object.assign(document.createElement('a'), {
      href: URL.createObjectURL(blob),
      download: `${getFilename()}.csv`,
    }).click()

    ElMessage.success(`已导出 ${rows.value.length} 条记录`)
  }

  return { handleExport }
}
