import { describe, expect, it } from 'vitest'
import {
  formatBudgetUSD,
  getBudgetSourceLabel,
  getBudgetStatus,
  getTierTagTone,
  toUsagePercent,
} from '../useBudgetDisplay'

describe('useBudgetDisplay', () => {
  it('区分未配置、零阻断、停用和 stale', () => {
    expect(getBudgetStatus({ configured: false, enabled: true })).toMatchObject({
      label: '未配置',
      tone: 'neutral',
      blocked: false,
    })
    expect(getBudgetStatus({ configured: true, enabled: true, limit: 0 })).toMatchObject({
      label: '$0 · 不可使用',
      tone: 'danger',
      blocked: true,
    })
    expect(getBudgetStatus({ configured: true, enabled: false, limit: 100 })).toMatchObject({
      label: '已停用',
      tone: 'info',
      blocked: false,
    })
    expect(getBudgetStatus({ configured: true, enabled: true, limit: 100, stale: true })).toMatchObject({
      label: '数据已过期',
      tone: 'warning',
      blocked: false,
    })
  })

  it('区分超额、预警和正常状态', () => {
    expect(getBudgetStatus({ configured: true, enabled: true, limit: 100, usageRatio: 1 })).toEqual({
      label: '已超额',
      tone: 'danger',
      blocked: true,
    })
    expect(getBudgetStatus({ configured: true, enabled: true, limit: 100, usageRatio: 0.8 })).toEqual({
      label: '预警',
      tone: 'warning',
      blocked: false,
    })
    expect(getBudgetStatus({ configured: true, enabled: true, limit: 100, usageRatio: 0.79 })).toEqual({
      label: '正常',
      tone: 'success',
      blocked: false,
    })
  })

  it('统一金额格式并安全处理不限和非法值', () => {
    expect(formatBudgetUSD('1200.500000')).toBe('$1,200.50')
    expect(formatBudgetUSD(12.345, { maximumFractionDigits: 3 })).toBe('$12.345')
    expect(formatBudgetUSD(-1, { unlimited: true })).toBe('不限')
    expect(formatBudgetUSD('invalid')).toBe('—')
    expect(formatBudgetUSD('   ')).toBe('—')
    expect(formatBudgetUSD(null)).toBe('—')
  })

  it('安全钳制非法或过大的最大小数位数', () => {
    expect(formatBudgetUSD(1.234, { maximumFractionDigits: Number.NaN })).toBe('$1.23')
    expect(formatBudgetUSD(1.234, { maximumFractionDigits: Number.POSITIVE_INFINITY })).toBe('$1.23')
    expect(formatBudgetUSD(1.234, { maximumFractionDigits: -1 })).toBe('$1.23')
    expect(formatBudgetUSD(1 / 3, { maximumFractionDigits: 1000 })).toBe('$0.3333333333333333')
  })

  it('空白限额不触发零预算阻断', () => {
    expect(getBudgetStatus({ configured: true, enabled: true, limit: '   ' })).toEqual({
      label: '正常',
      tone: 'success',
      blocked: false,
    })
  })

  it('将使用比例限制在零到一百并安全处理无效限额', () => {
    expect(toUsagePercent('82', '100')).toBe(82)
    expect(toUsagePercent(120, 100)).toBe(100)
    expect(toUsagePercent(-10, 100)).toBe(0)
    expect(toUsagePercent(10, 0)).toBe(0)
    expect(toUsagePercent(10, -1)).toBe(0)
    expect(toUsagePercent('invalid', 100)).toBe(0)
  })

  it('为预算来源提供稳定中文文案', () => {
    expect(getBudgetSourceLabel('member', true)).toBe('个人临时额度')
    expect(getBudgetSourceLabel('dept_daily')).toBe('部门共享子预算')
    expect(getBudgetSourceLabel('company')).toBe('公司总预算')
    expect(getBudgetSourceLabel('studio')).toBe('工作室预算')
    expect(getBudgetSourceLabel('dept_service')).toBe('部门服务预算')
    expect(getBudgetSourceLabel('member')).toBe('个人预算')
    expect(getBudgetSourceLabel()).toBe('未配置')
  })

  it('等级标签按名称上色，仅超额覆盖为红', () => {
    expect(getTierTagTone('高级')).toBe('purple')
    expect(getTierTagTone('标准')).toBe('blue')
    expect(getTierTagTone('经济')).toBe('green')
    expect(getTierTagTone('高级', 'warning')).toBe('purple')
    expect(getTierTagTone('标准', 'blocked')).toBe('red')
    expect(getTierTagTone('未知', 'disabled')).toBe('gray')
  })
})
