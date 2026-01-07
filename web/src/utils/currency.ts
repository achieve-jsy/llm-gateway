export type Currency = 'USD' | 'CNY'

export interface CurrencyConfig {
  currency: Currency
  rate: number // USD→CNY 汇率，仅当 currency='CNY' 时使用
}

export function convertCost(usd: number, cfg: CurrencyConfig): number {
  return cfg.currency === 'CNY' ? usd * cfg.rate : usd
}

export function currencySymbol(currency: Currency): string {
  return currency === 'CNY' ? '¥' : '$'
}

/**
 * 规则与原 fmtCost 一致：≥1 两位小数；
 * ≥0.01 四位小数；≥0.0001 六位小数；否则八位小数。
 * 尾零去除但最少保留两位小数。
 */
export function formatCost(usd: number, cfg: CurrencyConfig): string {
  const n = convertCost(Number(usd || 0), cfg)
  const sym = currencySymbol(cfg.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

export function formatUnitPrice(usd: number, cfg: CurrencyConfig): string {
  if (!usd) return `${currencySymbol(cfg.currency)}0`
  const n = convertCost(usd, cfg)
  const sym = currencySymbol(cfg.currency)
  return sym + (Math.abs(n) >= 1 ? n.toFixed(2) : n.toFixed(4).replace(/0+$/, '').replace(/\.$/,''))
}
