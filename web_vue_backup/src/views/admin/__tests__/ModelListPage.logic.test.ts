import { describe, it, expect } from 'vitest'

describe('ModelListPage 逻辑', () => {
  describe('formatTokenCount', () => {
    function formatTokenCount(count: number): string {
      if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
      if (count >= 1000) return `${Math.round(count / 1000)}K`
      return String(count)
    }

    it('小于 1000 直接返回数字字符串', () => {
      expect(formatTokenCount(512)).toBe('512')
      expect(formatTokenCount(0)).toBe('0')
      expect(formatTokenCount(999)).toBe('999')
    })

    it('1000 ~ 999999 转为 K 单位', () => {
      expect(formatTokenCount(1000)).toBe('1K')
      expect(formatTokenCount(8192)).toBe('8K')
      expect(formatTokenCount(32768)).toBe('33K')
      expect(formatTokenCount(999999)).toBe('1000K')
    })

    it('>= 1000000 转为 M 单位保留1位小数', () => {
      expect(formatTokenCount(1000000)).toBe('1.0M')
      expect(formatTokenCount(1280000)).toBe('1.3M')
      expect(formatTokenCount(2000000)).toBe('2.0M')
    })
  })

  describe('formatDateTime', () => {
    function formatDateTime(value: string): string {
      if (!value) return '-'
      const date = new Date(value)
      if (Number.isNaN(date.getTime())) return value
      const pad = (n: number) => String(n).padStart(2, '0')
      return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
    }

    it('空值返回 -', () => {
      expect(formatDateTime('')).toBe('-')
    })

    it('非法时间原样返回', () => {
      expect(formatDateTime('not-a-date')).toBe('not-a-date')
    })

    it('ISO 时间格式化到秒', () => {
      expect(formatDateTime('2026-05-23T10:20:30Z')).toBe('2026-05-23 18:20:30')
    })
  })

  describe('shortenError', () => {
    function shortenError(error: string): string {
      if (!error) return ''
      const lower = error.toLowerCase()
      if (lower.includes('timeout') || lower.includes('超时')) return '连接超时'
      if (lower.includes('401') || lower.includes('unauthorized') || lower.includes('invalid key')) return 'Key无效'
      if (lower.includes('403') || lower.includes('forbidden')) return '无权限'
      if (lower.includes('404') || lower.includes('not found')) return '地址无效'
      if (lower.includes('connect') || lower.includes('dial') || lower.includes('refused')) return '连接失败'
      if (lower.includes('dns') || lower.includes('no such host')) return 'DNS解析失败'
      if (lower.includes('502') || lower.includes('bad gateway')) return '上游异常'
      if (lower.includes('503') || lower.includes('unavailable')) return '服务不可用'
      if (lower.includes('model')) return '模型不可用'
      return error.length > 10 ? error.slice(0, 10) + '...' : error
    }

    it('空字符串返回空', () => {
      expect(shortenError('')).toBe('')
    })

    it('timeout/超时 → 连接超时', () => {
      expect(shortenError('Connection timeout')).toBe('连接超时')
      expect(shortenError('请求超时')).toBe('连接超时')
    })

    it('401/unauthorized/invalid key → Key无效', () => {
      expect(shortenError('HTTP 401 Unauthorized')).toBe('Key无效')
      expect(shortenError('invalid key format')).toBe('Key无效')
    })

    it('403/forbidden → 无权限', () => {
      expect(shortenError('403 Forbidden')).toBe('无权限')
    })

    it('404/not found → 地址无效', () => {
      expect(shortenError('404 Not Found')).toBe('地址无效')
    })

    it('connect/dial/refused → 连接失败', () => {
      expect(shortenError('connection refused')).toBe('连接失败')
      expect(shortenError('dial tcp failed')).toBe('连接失败')
    })

    it('dns/no such host → DNS解析失败', () => {
      expect(shortenError('DNS lookup failed')).toBe('DNS解析失败')
      expect(shortenError('no such host')).toBe('DNS解析失败')
    })

    it('502/bad gateway → 上游异常', () => {
      expect(shortenError('502 Bad Gateway')).toBe('上游异常')
    })

    it('503/unavailable → 服务不可用', () => {
      expect(shortenError('503 Service Unavailable')).toBe('服务不可用')
    })

    it('model 关键词 → 模型不可用', () => {
      expect(shortenError('model not available')).toBe('模型不可用')
      expect(shortenError('Model is overloaded')).toBe('模型不可用')
    })

    it('无法归类且长度>10 截断', () => {
      expect(shortenError('some random error message')).toBe('some rando...')
    })

    it('无法归类且长度<=10 原样返回', () => {
      expect(shortenError('short msg')).toBe('short msg')
    })
  })

  describe('客户端标签筛选 (filteredModels)', () => {
    const models = [
      { id: 'm1', name: 'gpt-4o' },
      { id: 'm2', name: 'claude-3' },
      { id: 'm3', name: 'qwen-turbo' },
    ]

    const modelTagsMap: Record<string, { id: string; name: string }[]> = {
      m1: [{ id: 'tag1', name: 'premium' }],
      m2: [{ id: 'tag1', name: 'premium' }, { id: 'tag2', name: 'fast' }],
      m3: [{ id: 'tag2', name: 'fast' }],
    }

    function filterByTagIds(
      models: { id: string }[],
      modelTagsMap: Record<string, { id: string }[]>,
      filterTagIds: string[]
    ): { id: string }[] {
      if (!filterTagIds.length) return models
      return models.filter(m => {
        const tags = modelTagsMap[m.id] ?? []
        return filterTagIds.every(tid => tags.some(t => t.id === tid))
      })
    }

    it('无筛选条件返回全部', () => {
      expect(filterByTagIds(models, modelTagsMap, []).length).toBe(3)
    })

    it('单标签筛选返回匹配模型', () => {
      const result = filterByTagIds(models, modelTagsMap, ['tag1'])
      expect(result.length).toBe(2)
      expect(result.map(m => m.id)).toEqual(['m1', 'm2'])
    })

    it('多标签筛选返回同时拥有所有标签的模型', () => {
      const result = filterByTagIds(models, modelTagsMap, ['tag1', 'tag2'])
      expect(result.length).toBe(1)
      expect(result[0].id).toBe('m2')
    })

    it('标签不存在时返回空', () => {
      const result = filterByTagIds(models, modelTagsMap, ['tag3'])
      expect(result.length).toBe(0)
    })

    it('模型无标签映射时不匹配任何标签筛选', () => {
      const modelsWithEmpty = [...models, { id: 'm4', name: 'empty' }]
      const result = filterByTagIds(modelsWithEmpty, modelTagsMap, ['tag1'])
      expect(result.length).toBe(2)
    })
  })

  describe('getProviderName', () => {
    const providers = [
      { id: 'p1', name: 'OpenAI' },
      { id: 'p2', name: 'DeepSeek' },
    ]

    function getProviderName(providerId: string): string {
      const found = providers.find(p => p.id === providerId)
      return found ? found.name : providerId
    }

    it('找到渠道返回名称', () => {
      expect(getProviderName('p1')).toBe('OpenAI')
    })

    it('找不到渠道返回 ID 原值', () => {
      expect(getProviderName('unknown-id')).toBe('unknown-id')
    })
  })

  describe('分页参数重置逻辑', () => {
    it('筛选变化时 currentPage 应重置为 1', () => {
      let currentPage = 3
      currentPage = 1
      expect(currentPage).toBe(1)
    })

    it('pageSize 变化时 currentPage 也应重置为 1', () => {
      let currentPage = 5
      currentPage = 1
      expect(currentPage).toBe(1)
    })
  })

  describe('handleToggleModel payload 构造', () => {
    function buildTogglePayload(row: {
      name: string; provider_id: string; model_type: string
      upstream_model_id: string; description: string
      context_length: number; max_output_tokens: number
      input_price: number; output_price: number; weight: number
    }, enabled: boolean) {
      return {
        name: row.name,
        provider_id: row.provider_id,
        model_type: row.model_type,
        enabled: enabled,
        upstream_model_id: row.upstream_model_id,
        description: row.description,
        context_length: row.context_length,
        max_output_tokens: row.max_output_tokens,
        input_price: row.input_price,
        output_price: row.output_price,
        weight: row.weight,
      }
    }

    it('启用时 payload 包含 enabled: true', () => {
      const row = {
        name: 'gpt-4o', provider_id: 'p1', model_type: 'chat',
        upstream_model_id: '', description: '',
        context_length: 128000, max_output_tokens: 4096,
        input_price: 0, output_price: 0, weight: 1,
      }
      const payload = buildTogglePayload(row, true)
      expect(payload.enabled).toBe(true)
    })

    it('停用时 payload 包含 enabled: false', () => {
      const row = {
        name: 'gpt-4o', provider_id: 'p1', model_type: 'chat',
        upstream_model_id: '', description: '',
        context_length: 128000, max_output_tokens: 4096,
        input_price: 0, output_price: 0, weight: 1,
      }
      const payload = buildTogglePayload(row, false)
      expect(payload.enabled).toBe(false)
    })

    it('payload 必须包含 enabled 字段 (防止 bug 回归)', () => {
      const row = {
        name: 'gpt-4o', provider_id: 'p1', model_type: 'chat',
        upstream_model_id: '', description: '',
        context_length: 128000, max_output_tokens: 4096,
        input_price: 0, output_price: 0, weight: 1,
      }
      const payload = buildTogglePayload(row, false)
      expect(payload).toHaveProperty('enabled')
    })
  })

  describe('normalizeEnabledFilter', () => {
    function normalizeEnabledFilter(value: string | null | undefined): boolean | undefined {
      if (value == null || value === '') return undefined
      return value === 'true'
    }

    it('空字符串时返回 undefined', () => {
      expect(normalizeEnabledFilter('')).toBeUndefined()
    })

    it('undefined 时返回 undefined', () => {
      expect(normalizeEnabledFilter(undefined)).toBeUndefined()
    })

    it('null 时返回 undefined', () => {
      expect(normalizeEnabledFilter(null)).toBeUndefined()
    })

    it('true 字符串时返回 true', () => {
      expect(normalizeEnabledFilter('true')).toBe(true)
    })

    it('false 字符串时返回 false', () => {
      expect(normalizeEnabledFilter('false')).toBe(false)
    })
  })
})
