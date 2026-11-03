// analytics/format.ts — 独立 AI 分析看板格式化工具（对齐原型 model-analytics.js 的 fmtN/fmtC/deltaPct/median）

/** 大数缩写：1234567 → 1.2M，12345 → 12.3K */
export function fmtN(n: number): string {
  if (!isFinite(n)) return '0'
  const v = Number(n) || 0
  if (v >= 1e6) return (v / 1e6).toFixed(2) + 'M'
  if (v >= 1e3) return (v / 1e3).toFixed(1) + 'K'
  return String(Math.round(v))
}

/** 美元金额格式化：$1234.56 */
export function fmtC(v: number): string {
  const n = Number(v) || 0
  return '$' + n.toFixed(2)
}

/** 环比百分比：b=0 → 0 */
export function deltaPct(a: number, b: number): number {
  return b > 0 ? ((a - b) / b) * 100 : 0
}

/** 中位数（对齐原型 median） */
export function median(arr: number[]): number {
  if (!arr.length) return 0
  const s = [...arr].sort((x, y) => x - y)
  const m = Math.floor(s.length / 2)
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2
}

/** 环比展示：<0.5% 持平；否则 ↑/↓ */
export function deltaText(d: number): { cls: string; txt: string } {
  const a = Math.abs(d)
  if (!isFinite(a) || a < 0.5) return { cls: 'flat', txt: '持平' }
  return { cls: d > 0 ? 'up' : 'down', txt: `${d > 0 ? '↑' : '↓'} ${a.toFixed(1)}%` }
}

/** 百分比展示：带一位小数 */
export function pctStr(v: number, total: number): string {
  return total > 0 ? ((v / total) * 100).toFixed(1) + '%' : '0%'
}
