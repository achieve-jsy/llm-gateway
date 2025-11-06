import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as controlPlane from './control-plane'
import http from './http'

vi.mock('./http')

const mockHttp = http as any

describe('Control Plane API Service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // ==================== Provider Definitions ====================

  describe('getProviderDefinitions', () => {
    it('should fetch provider definitions', async () => {
      const mockData = [
        {
          id: 'openai',
          display_name: 'OpenAI',
          adapter: 'openai',
          base_url: 'https://api.openai.com/v1',
          credential_schema: { api_key: { type: 'string', required: true, secret: true, label: 'API Key' } },
          vendor_options: {},
          description: 'OpenAI provider',
          icon_url: '',
          doc_url: '',
          default_models: ['gpt-4'],
          builtin: true,
          enabled: true
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockData }
      })

      const result = await controlPlane.getProviderDefinitions()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/provider-definitions')
      expect(result).toEqual(mockData)
    })

    it('should handle empty provider definitions', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: [] }
      })

      const result = await controlPlane.getProviderDefinitions()

      expect(result).toEqual([])
    })
  })

  // ==================== Providers ====================

  describe('getProviders', () => {
    it('should fetch all providers', async () => {
      const mockProviders = [
        {
          id: 'provider1',
          name: 'My OpenAI',
          definition_id: 'openai',
          type: 'LLM',
          endpoint: 'https://api.openai.com',
          credentials: 'sk-xxx',
          enabled: true,
          weight: 100,
          priority: 1,
          status: 1,
          test_model: 'gpt-4',
          response_time: 150,
          balance: 1000,
          remark: '',
          max_retries: 3,
          timeout: 30,
          tag: 'production',
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockProviders }
      })

      const result = await controlPlane.getProviders()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/providers')
      expect(result.data).toEqual(mockProviders)
    })

    it('should pass pagination params when opts provided', async () => {
      mockHttp.get.mockResolvedValueOnce({ data: { data: [] } })

      await controlPlane.getProviders({ page: 2, pageSize: 50 })

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/providers', {
        params: { page: 2, page_size: 50 }
      })
    })

    it('should not pass params when no opts provided (negative test)', async () => {
      mockHttp.get.mockResolvedValueOnce({ data: { data: [] } })

      await controlPlane.getProviders()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/providers')
      // ensure no second argument with params
      const call = mockHttp.get.mock.calls[0]
      expect(call.length).toBe(1)
    })
  })

  describe('getProvider', () => {
    it('should fetch single provider by id', async () => {
      const mockProvider = {
        id: 'provider1',
        name: 'My OpenAI',
        definition_id: 'openai',
        type: 'LLM',
        endpoint: 'https://api.openai.com',
        credentials: 'sk-xxx',
        enabled: true,
        weight: 100,
        priority: 1,
        status: 1,
        test_model: 'gpt-4',
        response_time: 150,
        balance: 1000,
        remark: '',
        max_retries: 3,
        timeout: 30,
        tag: 'production',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockProvider }
      })

      const result = await controlPlane.getProvider('provider1')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/providers/provider1')
      expect(result).toEqual(mockProvider)
    })
  })

  describe('createProvider', () => {
    it('should create a new provider', async () => {
      const payload = {
        name: 'New Provider',
        definition_id: 'openai',
        endpoint: 'https://api.openai.com',
        credentials: 'sk-new',
        weight: 100,
        priority: 1
      }

      const mockCreated = { id: 'new-provider', ...payload }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockCreated }
      })

      const result = await controlPlane.createProvider(payload)

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/providers', payload)
      expect(result).toEqual(mockCreated)
    })
  })

  describe('updateProvider', () => {
    it('should update an existing provider', async () => {
      const payload = {
        name: 'Updated Name',
        definition_id: 'openai',
        endpoint: 'https://api.openai.com',
        credentials: 'sk-updated'
      }

      const mockUpdated = { id: 'provider1', ...payload }

      mockHttp.put.mockResolvedValueOnce({
        data: { data: mockUpdated }
      })

      const result = await controlPlane.updateProvider('provider1', payload)

      expect(mockHttp.put).toHaveBeenCalledWith('/api/v1/providers/provider1', payload)
      expect(result).toEqual(mockUpdated)
    })
  })

  describe('deleteProvider', () => {
    it('should delete a provider', async () => {
      mockHttp.delete.mockResolvedValueOnce({})

      await controlPlane.deleteProvider('provider1')

      expect(mockHttp.delete).toHaveBeenCalledWith('/api/v1/providers/provider1')
    })
  })

  describe('toggleProviderStatus', () => {
    const mockProvider = {
      id: 'provider1',
      name: 'My OpenAI',
      definition_id: 'openai',
      type: 'LLM',
      endpoint: 'https://api.openai.com',
      credentials: '',
      enabled: true,
      weight: 100,
      priority: 1,
      status: 1,
      test_model: 'gpt-4',
      response_time: 150,
      balance: 1000,
      remark: '',
      max_retries: 3,
      timeout: 30,
      tag: '',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z'
    }

    it('should enable a provider and return updated Provider', async () => {
      mockHttp.patch.mockResolvedValueOnce({ data: { data: { ...mockProvider, status: 1 } } })

      const result = await controlPlane.toggleProviderStatus('provider1', true)

      expect(mockHttp.patch).toHaveBeenCalledWith('/api/v1/providers/provider1/status', {
        enabled: true
      })
      expect(result).toMatchObject({ id: 'provider1', status: 1 })
    })

    it('should disable a provider and return updated Provider', async () => {
      mockHttp.patch.mockResolvedValueOnce({ data: { data: { ...mockProvider, status: 2 } } })

      const result = await controlPlane.toggleProviderStatus('provider1', false)

      expect(mockHttp.patch).toHaveBeenCalledWith('/api/v1/providers/provider1/status', {
        enabled: false
      })
      expect(result).toMatchObject({ id: 'provider1', status: 2 })
    })

    it('should return full provider object from data.data', async () => {
      mockHttp.patch.mockResolvedValueOnce({ data: { data: mockProvider } })

      const result = await controlPlane.toggleProviderStatus('provider1', true)

      expect(result.name).toBe('My OpenAI')
      expect(result.definition_id).toBe('openai')
    })
  })

  describe('testProvider', () => {
    it('should test provider connectivity', async () => {
      const mockResult = { response_time: 250 }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockResult }
      })

      const result = await controlPlane.testProvider('provider1')

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/providers/provider1/test', undefined)
      expect(result).toEqual(mockResult)
    })

    it('should pass all TestProviderResult fields through (S1-3)', async () => {
      const fullResult: controlPlane.TestProviderResult = {
        success: true,
        response_time: 180,
        status_code: 200,
        model_count: 12,
        error: undefined,
        chat_success: true,
        chat_response_time: 220,
        chat_model: 'gpt-4o',
        chat_error: undefined
      }

      mockHttp.post.mockResolvedValueOnce({ data: { data: fullResult } })

      const result = await controlPlane.testProvider('provider1', true)

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/providers/provider1/test', { test_chat: true })
      expect(result.success).toBe(true)
      expect(result.response_time).toBe(180)
      expect(result.status_code).toBe(200)
      expect(result.model_count).toBe(12)
      expect(result.chat_success).toBe(true)
      expect(result.chat_response_time).toBe(220)
      expect(result.chat_model).toBe('gpt-4o')
    })

    it('should handle failed connectivity result with error fields', async () => {
      const failResult: controlPlane.TestProviderResult = {
        success: false,
        response_time: 0,
        status_code: 401,
        error: 'Unauthorized',
        chat_success: false,
        chat_error: 'no upstream'
      }

      mockHttp.post.mockResolvedValueOnce({ data: { data: failResult } })

      const result = await controlPlane.testProvider('provider1')

      expect(result.success).toBe(false)
      expect(result.status_code).toBe(401)
      expect(result.error).toBe('Unauthorized')
      expect(result.chat_success).toBe(false)
      expect(result.chat_error).toBe('no upstream')
    })
  })

  // ==================== Models ====================

  describe('getModels', () => {
    it('should fetch all models without filter, returning { data, total }', async () => {
      const mockModels = [
        {
          id: 'model1',
          name: 'GPT-4',
          provider_id: 'provider1',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: 'gpt-4',
          description: 'GPT-4 model',
          context_length: 8192,
          max_output_tokens: 2048,
          input_price: 0.03,
          output_price: 0.06,
          weight: 100,
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockModels, total: 1, page: 1, page_size: 20 }
      })

      const result = await controlPlane.getModels()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models', { params: {} })
      expect(result.data).toEqual(mockModels)
      expect(result.total).toBe(1)
    })

    it('should fetch models filtered by provider_id', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: [], total: 0, page: 1, page_size: 20 }
      })

      await controlPlane.getModels('provider1')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models', {
        params: { provider_id: 'provider1' }
      })
    })

    it('should pass pagination params when opts provided', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: [], total: 0, page: 2, page_size: 50 }
      })

      await controlPlane.getModels(undefined, { page: 2, pageSize: 50 })

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models', {
        params: { page: 2, page_size: 50 }
      })
    })

    it('should merge provider_id filter with pagination params', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: [], total: 0, page: 3, page_size: 20 }
      })

      await controlPlane.getModels('provider1', { page: 3, pageSize: 20 })

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models', {
        params: { provider_id: 'provider1', page: 3, page_size: 20 }
      })
    })
  })

  describe('createModel', () => {
    it('should create a new model', async () => {
      const payload = {
        name: 'GPT-4',
        provider_id: 'provider1',
        model_type: 'chat',
        upstream_model_id: 'gpt-4'
      }

      const mockCreated = { id: 'model1', ...payload }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockCreated }
      })

      const result = await controlPlane.createModel(payload)

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/models', payload)
      expect(result).toEqual(mockCreated)
    })
  })

  describe('updateModel', () => {
    it('should update an existing model', async () => {
      const payload = {
        name: 'GPT-4 Updated',
        provider_id: 'provider1',
        model_type: 'chat'
      }

      const mockUpdated = { id: 'model1', ...payload }

      mockHttp.put.mockResolvedValueOnce({
        data: { data: mockUpdated }
      })

      const result = await controlPlane.updateModel('model1', payload)

      expect(mockHttp.put).toHaveBeenCalledWith('/api/v1/models/model1', payload)
      expect(result).toEqual(mockUpdated)
    })
  })

  describe('deleteModel', () => {
    it('should delete a model', async () => {
      mockHttp.delete.mockResolvedValueOnce({})

      await controlPlane.deleteModel('model1')

      expect(mockHttp.delete).toHaveBeenCalledWith('/api/v1/models/model1')
    })
  })

  describe('checkModelNameUnique', () => {
    it('should call dedicated check-name endpoint with `name` query (S0-3 修复)', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: { available: true } }
      })

      const result = await controlPlane.checkModelNameUnique('GPT-4')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models/check-name', {
        params: { name: 'GPT-4' }
      })
      expect(result).toBe(true)
    })

    it('should return false when model name is taken', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { data: { available: false } }
      })

      const result = await controlPlane.checkModelNameUnique('GPT-4')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models/check-name', {
        params: { name: 'GPT-4' }
      })
      expect(result).toBe(false)
    })
  })

  // ==================== Model Tags ====================

  describe('getModelTags', () => {
    it('should fetch all model tags', async () => {
      const mockTags = [
        {
          id: 'tag1',
          name: 'Premium',
          color: '#FF0000',
          description: 'Premium models',
          sort_order: 1,
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockTags }
      })

      const result = await controlPlane.getModelTags()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/model-tags')
      expect(result).toEqual(mockTags)
    })
  })

  describe('createModelTag', () => {
    it('should create a new tag', async () => {
      const payload = {
        name: 'Premium',
        color: '#FF0000',
        description: 'Premium models'
      }

      const mockCreated = { id: 'tag1', ...payload }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockCreated }
      })

      const result = await controlPlane.createModelTag(payload)

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/model-tags', payload)
      expect(result).toEqual(mockCreated)
    })
  })

  describe('updateModelTag', () => {
    it('should update an existing tag', async () => {
      const payload = {
        name: 'Premium Updated',
        color: '#00FF00'
      }

      const mockUpdated = { id: 'tag1', ...payload }

      mockHttp.put.mockResolvedValueOnce({
        data: { data: mockUpdated }
      })

      const result = await controlPlane.updateModelTag('tag1', payload)

      expect(mockHttp.put).toHaveBeenCalledWith('/api/v1/model-tags/tag1', payload)
      expect(result).toEqual(mockUpdated)
    })
  })

  describe('deleteModelTag', () => {
    it('should delete a tag', async () => {
      mockHttp.delete.mockResolvedValueOnce({})

      await controlPlane.deleteModelTag('tag1')

      expect(mockHttp.delete).toHaveBeenCalledWith('/api/v1/model-tags/tag1')
    })
  })

  describe('getModelTagsForModel', () => {
    it('should fetch tags for a specific model', async () => {
      const mockTags = [
        {
          id: 'tag1',
          name: 'Premium',
          color: '#FF0000',
          description: 'Premium models',
          sort_order: 1,
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: { data: mockTags }
      })

      const result = await controlPlane.getModelTagsForModel('model1')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/models/model1/tags')
      expect(result).toEqual(mockTags)
    })
  })

  describe('setModelTags', () => {
    it('should set tags for a model', async () => {
      const tagIds = ['tag1', 'tag2']
      const mockTags = [
        { id: 'tag1', name: 'Premium', color: '#FF0000', description: '', sort_order: 1, created_at: '', updated_at: '' },
        { id: 'tag2', name: 'New', color: '#00FF00', description: '', sort_order: 2, created_at: '', updated_at: '' }
      ]

      mockHttp.put.mockResolvedValueOnce({
        data: { data: mockTags }
      })

      const result = await controlPlane.setModelTags('model1', tagIds)

      expect(mockHttp.put).toHaveBeenCalledWith('/api/v1/models/model1/tags', {
        tag_ids: tagIds
      })
      expect(result).toEqual(mockTags)
    })

    it('should handle empty tag list', async () => {
      mockHttp.put.mockResolvedValueOnce({
        data: { data: [] }
      })

      const result = await controlPlane.setModelTags('model1', [])

      expect(mockHttp.put).toHaveBeenCalledWith('/api/v1/models/model1/tags', {
        tag_ids: []
      })
      expect(result).toEqual([])
    })
  })

  // ==================== Model Sync ====================

  describe('syncModels', () => {
    it('should fetch sync result preview', async () => {
      const mockResult = {
        added: [{ id: 'model1', owned_by: 'openai' }],
        existing: [{ id: 'model2', owned_by: 'openai' }],
        removed: [],
        total_upstream: 2,
        total_local: 1,
        unsupported: false
      }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockResult }
      })

      const result = await controlPlane.syncModels('provider1')

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/providers/provider1/sync-models')
      expect(result).toEqual(mockResult)
    })
  })

  // ==================== Model Catalog (T7) ====================

  describe('searchCatalog', () => {
    it('calls /api/v1/model-catalog/search with query/provider/mode/limit', async () => {
      const mockData = [
        {
          id: 'cat-1',
          source: 'litellm',
          source_model_id: 'gpt-4o',
          display_name: 'GPT-4o',
          provider: 'openai',
          mode: 'chat',
          context_length: 128000,
          max_output_tokens: 4096,
          input_price_per_million: 2.5,
          output_price_per_million: 10,
          cache_read_price: null,
          cache_write_price: null,
          supports_function_calling: true,
          supports_vision: true,
          supports_prompt_caching: false,
          supports_response_schema: false,
          supports_tool_choice: true,
          deprecation_date: null,
          synced_at: '2026-05-22T00:00:00Z'
        }
      ]
      mockHttp.get.mockResolvedValueOnce({ data: { data: mockData, total: 1 } })

      const result = await controlPlane.searchCatalog({
        q: 'gpt',
        provider: 'openai',
        mode: 'chat',
        limit: 10
      })

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/model-catalog/search', {
        params: { q: 'gpt', provider: 'openai', mode: 'chat', limit: 10 }
      })
      expect(result.data).toEqual(mockData)
      expect(result.total).toBe(1)
    })
  })

  describe('getCatalogBySource', () => {
    it('calls /api/v1/model-catalog/by-source with source_model_id query', async () => {
      const mockItem = { id: 'cat-1', source: 'litellm', source_model_id: 'gpt-4o' }
      mockHttp.get.mockResolvedValueOnce({ data: { data: mockItem } })

      const result = await controlPlane.getCatalogBySource('gpt-4o')

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/model-catalog/by-source', {
        params: { source_model_id: 'gpt-4o' }
      })
      expect(result).toEqual(mockItem)
    })
  })

  describe('triggerCatalogSync', () => {
    it('POSTs /api/v1/model-catalog/sync with empty body and returns job descriptor', async () => {
      const mockJob = { job_id: 'job-123', status: 'queued', source: 'litellm' }
      mockHttp.post.mockResolvedValueOnce({ data: { data: mockJob } })

      const result = await controlPlane.triggerCatalogSync()

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/model-catalog/sync')
      expect(result).toEqual(mockJob)
    })
  })

  describe('getCatalogDiff', () => {
    it('GETs /api/v1/model-catalog/diff and returns {data,total}', async () => {
      const mockDiff = [
        {
          model_id: 'm1',
          model_name: 'gpt-4o',
          catalog_id: 'cat-1',
          catalog_source_model_id: 'gpt-4o',
          local_input_price: 2.5,
          local_output_price: 10,
          catalog_input_price: 2.6,
          catalog_output_price: 10.5,
          is_overridden: false,
          input_delta_pct: 4,
          output_delta_pct: 5,
          catalog_deprecation_date: null
        }
      ]
      mockHttp.get.mockResolvedValueOnce({ data: { data: mockDiff, total: 1 } })

      const result = await controlPlane.getCatalogDiff()

      expect(mockHttp.get).toHaveBeenCalledWith('/api/v1/model-catalog/diff')
      expect(result.data).toEqual(mockDiff)
      expect(result.total).toBe(1)
    })
  })

  describe('applyCatalogDiff', () => {
    it('POSTs /api/v1/model-catalog/apply-diff with model_ids body', async () => {
      const mockResp = { updated: 2, skipped: 1, failed_ids: ['m3'] }
      mockHttp.post.mockResolvedValueOnce({ data: { data: mockResp } })

      const result = await controlPlane.applyCatalogDiff(['m1', 'm2', 'm3'])

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/model-catalog/apply-diff', {
        model_ids: ['m1', 'm2', 'm3']
      })
      expect(result).toEqual(mockResp)
    })
  })

  describe('createModelFromCatalog', () => {
    it('POSTs /api/v1/models/from-catalog with catalog_id/provider_id/name', async () => {
      const payload = { catalog_id: 'cat-1', provider_id: 'p1', name: 'gpt-4o' }
      const mockModel = { id: 'm1', name: 'gpt-4o' }
      mockHttp.post.mockResolvedValueOnce({ data: { data: mockModel } })

      const result = await controlPlane.createModelFromCatalog(payload)

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/models/from-catalog', payload)
      expect(result).toEqual(mockModel)
    })
  })

  describe('resetModelOverride', () => {
    it('POSTs /api/v1/models/:id/reset-override with field body', async () => {
      const mockModel = { id: 'm1', name: 'gpt-4o', ctx_overridden: false }
      mockHttp.post.mockResolvedValueOnce({ data: { data: mockModel } })

      const result = await controlPlane.resetModelOverride('m1', 'context_length')

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/models/m1/reset-override', {
        field: 'context_length'
      })
      expect(result).toEqual(mockModel)
    })

    it('supports max_output_tokens field', async () => {
      mockHttp.post.mockResolvedValueOnce({ data: { data: { id: 'm1' } } })

      await controlPlane.resetModelOverride('m1', 'max_output_tokens')

      expect(mockHttp.post).toHaveBeenCalledWith('/api/v1/models/m1/reset-override', {
        field: 'max_output_tokens'
      })
    })
  })

  describe('confirmSync', () => {
    it('should confirm sync and register models', async () => {
      const payload = {
        models_to_add: ['model1', 'model2'],
        disable_removed: true
      }

      const mockResult = {
        registered: 2,
        skipped: 0,
        disabled: 0
      }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockResult }
      })

      const result = await controlPlane.confirmSync('provider1', payload)

      expect(mockHttp.post).toHaveBeenCalledWith(
        '/api/v1/providers/provider1/sync-models/confirm',
        payload
      )
      expect(result).toEqual(mockResult)
    })

    it('should handle confirm sync without disable_removed', async () => {
      const payload = {
        models_to_add: ['model1']
      }

      const mockResult = {
        registered: 1,
        skipped: 0,
        disabled: 0
      }

      mockHttp.post.mockResolvedValueOnce({
        data: { data: mockResult }
      })

      const result = await controlPlane.confirmSync('provider1', payload)

      expect(mockHttp.post).toHaveBeenCalledWith(
        '/api/v1/providers/provider1/sync-models/confirm',
        payload
      )
      expect(result).toEqual(mockResult)
    })
  })
})
