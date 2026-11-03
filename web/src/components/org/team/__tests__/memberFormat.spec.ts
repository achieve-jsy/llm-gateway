import { describe, expect, it } from 'vitest'
import { budgetText, configOf, entitlementText, isQuotaOverride, leadersText, quotaText } from '../memberFormat'

describe('memberFormat', () => {
  it('配额：override 显示具体值，0 视为不限，inherit 显示继承', () => {
    expect(quotaText({ rpm: 1000, max_concurrency: 200, source: 'override' })).toBe('1000 RPM / 200 并发')
    expect(quotaText({ rpm: 0, max_concurrency: 0, source: 'override' })).toBe('不限 RPM / 不限 并发')
    expect(quotaText({ rpm: 60, max_concurrency: 10, source: 'inherit' })).toBe('继承')
    expect(quotaText(null)).toBe('—')
    expect(isQuotaOverride({ rpm: 60, max_concurrency: 10, source: 'inherit' })).toBe(false)
    expect(isQuotaOverride({ rpm: 60, max_concurrency: 10, source: 'override' })).toBe(true)
  })

  it('预算规则：team 继承团队，member 列出非 0 周期，none 显示占位', () => {
    expect(budgetText({ monthly: 1840, daily: 800, weekly: 0, source: 'member' })).toBe('月 ¥1840 / 日 ¥800')
    expect(budgetText({ monthly: 0, daily: 0, weekly: 0, source: 'team' })).toBe('继承团队')
    expect(budgetText({ monthly: 0, daily: 0, weekly: 0, source: 'none' })).toBe('—')
    expect(budgetText(null)).toBe('—')
  })

  it('模型授权：0 组或 none 显示 —，否则显示可用模型数', () => {
    expect(entitlementText({ group_count: 3, source: 'studio_dept' })).toBe('3 模型可用')
    expect(entitlementText({ group_count: 0, source: 'studio_dept' })).toBe('—')
    expect(entitlementText({ group_count: 2, source: 'none' })).toBe('—')
    expect(entitlementText()).toBe('—')
  })

  it('负责人（EHR，多值）：单人「X负责」，多人「X等N人负责」，空为未设置', () => {
    expect(leadersText([{ name: '周乐心' }])).toBe('周乐心负责')
    expect(leadersText([{ name: '傅彭彭' }, { name: '张三' }, { name: '李四' }])).toBe('傅彭彭等3人负责')
    expect(leadersText([])).toBe('未设置负责人')
  })

  it('configOf 兼容 config 嵌套与字段平铺两种返回', () => {
    const nested = { config: { quota: { rpm: 1, max_concurrency: 2, source: 'override' } } } as any
    expect(configOf(nested).quota?.rpm).toBe(1)
    const flat = { quota: { rpm: 9, max_concurrency: 9, source: 'inherit' } } as any
    expect(configOf(flat).quota?.rpm).toBe(9)
  })
})
