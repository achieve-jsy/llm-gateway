import { describe, it, expect } from 'vitest'

describe('ModelDialog 逻辑', () => {
  describe('validateName', () => {
    function validateName(value: string, nameConflict: boolean): string | null {
      if (!value) return '请输入模型名称'
      if (nameConflict) return '模型名称已存在'
      return null
    }

    it('空名称返回错误', () => {
      expect(validateName('', false)).toBe('请输入模型名称')
    })

    it('名称冲突返回错误', () => {
      expect(validateName('gpt-4o', true)).toBe('模型名称已存在')
    })

    it('有效名称返回 null', () => {
      expect(validateName('new-model', false)).toBeNull()
    })
  })

  describe('applyCatalogSelection 字段填充', () => {
    const catalogItem = {
      id: 'cat-1',
      source_model_id: 'gpt-4o',
      context_length: 128000,
      max_output_tokens: 4096,
      input_price_per_million: 2.5,
      output_price_per_million: 10,
    }

    function applyCatalogFields(item: typeof catalogItem) {
      return {
        upstream_model_id: item.source_model_id,
        context_length: item.context_length,
        max_output_tokens: item.max_output_tokens,
        input_price: item.input_price_per_million,
        output_price: item.output_price_per_million,
      }
    }

    it('从 catalog 自动填充 5 个字段', () => {
      const result = applyCatalogFields(catalogItem)
      expect(result.upstream_model_id).toBe('gpt-4o')
      expect(result.context_length).toBe(128000)
      expect(result.max_output_tokens).toBe(4096)
      expect(result.input_price).toBe(2.5)
      expect(result.output_price).toBe(10)
    })
  })

  describe('catalog 脱离检测 (notifyDetach)', () => {
    function shouldNotifyDetach(
      field: string,
      selectedCatalogId: string | null,
      applyingCatalog: boolean,
      detachedFields: Set<string>,
      currentValue: number,
      baselineValue: number
    ): boolean {
      if (!selectedCatalogId) return false
      if (applyingCatalog) return false
      if (detachedFields.has(field)) return false
      return currentValue !== baselineValue
    }

    it('catalog 未关联时不通知', () => {
      expect(shouldNotifyDetach('context_length', null, false, new Set(), 200, 128)).toBe(false)
    })

    it('正在应用 catalog 时跳过检测', () => {
      expect(shouldNotifyDetach('context_length', 'cat-1', true, new Set(), 200, 128)).toBe(false)
    })

    it('已通知过的字段不再重复通知', () => {
      expect(shouldNotifyDetach('context_length', 'cat-1', false, new Set(['context_length']), 200, 128)).toBe(false)
    })

    it('值与 baseline 不同时应通知', () => {
      expect(shouldNotifyDetach('context_length', 'cat-1', false, new Set(), 200, 128)).toBe(true)
    })

    it('值与 baseline 相同时不应通知', () => {
      expect(shouldNotifyDetach('context_length', 'cat-1', false, new Set(), 128, 128)).toBe(false)
    })
  })

  describe('findProviderCatalogKey', () => {
    const providers = [
      { id: 'p1', name: 'OpenAI', definition_id: 'openai' },
      { id: 'p2', name: 'Anthropic', definition_id: 'anthropic' },
      { id: 'p3', name: 'Custom' },
    ]

    function findProviderCatalogKey(providerId: string): string | undefined {
      const p = providers.find(pr => pr.id === providerId) as any
      return p?.definition_id || undefined
    }

    it('有 definition_id 的渠道返回映射值', () => {
      expect(findProviderCatalogKey('p1')).toBe('openai')
    })

    it('无 definition_id 的渠道返回 undefined', () => {
      expect(findProviderCatalogKey('p3')).toBeUndefined()
    })

    it('不存在的渠道返回 undefined', () => {
      expect(findProviderCatalogKey('unknown')).toBeUndefined()
    })
  })

  describe('formatCtxK', () => {
    function formatCtxK(n: number): number {
      return Math.round(n / 1000)
    }

    it('128000 → 128', () => {
      expect(formatCtxK(128000)).toBe(128)
    })

    it('0 → 0', () => {
      expect(formatCtxK(0)).toBe(0)
    })

    it('4096 → 4', () => {
      expect(formatCtxK(4096)).toBe(4)
    })
  })

  describe('boundCatalogLabel', () => {
    function getBoundCatalogLabel(model: any): string {
      if (!model) return ''
      if (model.catalog?.source_model_id) {
        return `${model.catalog.source || 'litellm'} / ${model.catalog.source_model_id}`
      }
      if (model.catalog_id) {
        return `catalog_id: ${model.catalog_id.slice(0, 8)}…`
      }
      return ''
    }

    it('有 catalog 嵌套信息时显示完整标签', () => {
      const model = {
        catalog_id: 'cat-1',
        catalog: { source: 'litellm', source_model_id: 'gpt-4o' },
      }
      expect(getBoundCatalogLabel(model)).toBe('litellm / gpt-4o')
    })

    it('只有 catalog_id 时截短显示', () => {
      const model = { catalog_id: 'abc12345-6789-0abc-def0-1234567890ab' }
      expect(getBoundCatalogLabel(model)).toBe('catalog_id: abc12345…')
    })

    it('null 模型返回空字符串', () => {
      expect(getBoundCatalogLabel(null)).toBe('')
    })

    it('无 catalog 关联时返回空字符串', () => {
      expect(getBoundCatalogLabel({ id: 'm1' })).toBe('')
    })
  })

  describe('提交 payload 构造', () => {
    function buildPayload(formData: {
      name: string
      upstream_model_id: string
      provider_id: string
      model_type: string
      description: string
      context_length: number
      max_output_tokens: number
      input_price: number
      output_price: number
      weight: number
    }) {
      return {
        name: formData.name,
        provider_id: formData.provider_id,
        model_type: formData.model_type,
        upstream_model_id: formData.upstream_model_id || undefined,
        description: formData.description || undefined,
        context_length: formData.context_length || undefined,
        max_output_tokens: formData.max_output_tokens || undefined,
        input_price: formData.input_price || undefined,
        output_price: formData.output_price || undefined,
        weight: formData.weight,
      }
    }

    it('空字符串字段转为 undefined', () => {
      const payload = buildPayload({
        name: 'test-model',
        upstream_model_id: '',
        provider_id: 'p1',
        model_type: 'chat',
        description: '',
        context_length: 0,
        max_output_tokens: 0,
        input_price: 0,
        output_price: 0,
        weight: 1,
      })
      expect(payload.upstream_model_id).toBeUndefined()
      expect(payload.description).toBeUndefined()
      expect(payload.context_length).toBeUndefined()
      expect(payload.max_output_tokens).toBeUndefined()
      expect(payload.input_price).toBeUndefined()
      expect(payload.output_price).toBeUndefined()
    })

    it('有值的字段正常传递', () => {
      const payload = buildPayload({
        name: 'gpt-4o',
        upstream_model_id: 'openai/gpt-4o',
        provider_id: 'p1',
        model_type: 'chat',
        description: 'omni model',
        context_length: 128000,
        max_output_tokens: 4096,
        input_price: 2.5,
        output_price: 10,
        weight: 5,
      })
      expect(payload.upstream_model_id).toBe('openai/gpt-4o')
      expect(payload.context_length).toBe(128000)
      expect(payload.input_price).toBe(2.5)
    })
  })
})