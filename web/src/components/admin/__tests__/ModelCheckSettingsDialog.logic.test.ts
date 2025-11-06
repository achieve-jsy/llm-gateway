import { describe, it, expect } from 'vitest'

describe('ModelCheckSettingsDialog 逻辑', () => {
  describe('failureSummaryText 构造', () => {
    const labelMap: Record<string, string> = {
      invalid_api_key: 'Key无效',
      access_denied: '无权限',
      model_not_found: '模型不存在',
      rate_limited: '限流',
      product_not_activated: '未开通',
      timeout: '超时',
      other: '其他',
    }

    function buildFailureSummaryText(
      summary: Record<string, number> | undefined
    ): string {
      if (!summary) return ''
      const parts = Object.entries(summary)
        .filter(([, count]) => count > 0)
        .sort((a, b) => b[1] - a[1])
        .map(([key, count]) => `${labelMap[key] || key} ${count}`)
      return parts.join(' / ')
    }

    it('空 summary 返回空字符串', () => {
      expect(buildFailureSummaryText(undefined)).toBe('')
      expect(buildFailureSummaryText({})).toBe('')
    })

    it('单一分类', () => {
      expect(buildFailureSummaryText({ timeout: 3 })).toBe('超时 3')
    })

    it('多分类按数量降序排列', () => {
      const result = buildFailureSummaryText({
        timeout: 2,
        invalid_api_key: 5,
        rate_limited: 1,
      })
      expect(result).toBe('Key无效 5 / 超时 2 / 限流 1')
    })

    it('未知分类使用原始 key', () => {
      expect(buildFailureSummaryText({ unknown_error: 1 })).toBe('unknown_error 1')
    })

    it('count 为 0 的分类被过滤掉', () => {
      const result = buildFailureSummaryText({ timeout: 3, invalid_api_key: 0 })
      expect(result).toBe('超时 3')
    })
  })

  describe('autoDisabledCount 计算', () => {
    function countAutoDisabled(results: { auto_disabled: boolean }[]): number {
      return results.filter(r => r.auto_disabled).length
    }

    it('无自动禁用返回 0', () => {
      expect(countAutoDisabled([{ auto_disabled: false }, { auto_disabled: false }])).toBe(0)
    })

    it('有自动禁用时正确计数', () => {
      expect(countAutoDisabled([
        { auto_disabled: true },
        { auto_disabled: false },
        { auto_disabled: true },
      ])).toBe(2)
    })
  })

  describe('autoReenabledCount 计算', () => {
    function countAutoReenabled(results: { auto_reenabled: boolean }[]): number {
      return results.filter(r => r.auto_reenabled).length
    }

    it('无自动恢复返回 0', () => {
      expect(countAutoReenabled([{ auto_reenabled: false }])).toBe(0)
    })

    it('有自动恢复时正确计数', () => {
      expect(countAutoReenabled([
        { auto_reenabled: true },
        { auto_reenabled: true },
        { auto_reenabled: false },
      ])).toBe(2)
    })
  })

  describe('检测配置加载与保存', () => {
    function mergeConfigWithDefaults(config: {
      model_check_enabled?: boolean | null
      model_check_interval_minutes?: number | null
      model_check_failure_threshold?: number | null
    }) {
      return {
        model_check_enabled: config.model_check_enabled ?? true,
        model_check_interval_minutes: config.model_check_interval_minutes || 5,
        model_check_failure_threshold: config.model_check_failure_threshold || 3,
      }
    }

    it('null 字段使用默认值', () => {
      const result = mergeConfigWithDefaults({})
      expect(result.model_check_enabled).toBe(true)
      expect(result.model_check_interval_minutes).toBe(5)
      expect(result.model_check_failure_threshold).toBe(3)
    })

    it('非 null 字段使用传入值', () => {
      const result = mergeConfigWithDefaults({
        model_check_enabled: false,
        model_check_interval_minutes: 30,
        model_check_failure_threshold: 10,
      })
      expect(result.model_check_enabled).toBe(false)
      expect(result.model_check_interval_minutes).toBe(30)
      expect(result.model_check_failure_threshold).toBe(10)
    })
  })

  describe('intervalOptions 验证', () => {
    const intervalOptions = [
      { label: '每 5 分钟', value: 5 },
      { label: '每 10 分钟', value: 10 },
      { label: '每 30 分钟', value: 30 },
      { label: '每 60 分钟', value: 60 },
    ]

    it('包含 4 个选项', () => {
      expect(intervalOptions.length).toBe(4)
    })

    it('每个选项有 label 和 value', () => {
      for (const opt of intervalOptions) {
        expect(opt.label).toBeTruthy()
        expect(opt.value).toBeGreaterThan(0)
      }
    })

    it('value 从小到大排列', () => {
      const values = intervalOptions.map(o => o.value)
      for (let i = 1; i < values.length; i++) {
        expect(values[i]).toBeGreaterThan(values[i - 1])
      }
    })
  })
})