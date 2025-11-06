import { describe, it, expect } from 'vitest'
import type {
  ApiResponse,
  PaginatedResponse
} from './api'
import type {
  Provider,
  ProviderDefinition,
  ProviderPayload,
  CredentialFieldSchema,
  CredentialSchema,
  VendorOptions
} from './provider'
import type {
  Model,
  ModelInfo,
  ModelPayload,
  ModelsResponse
} from './model'
import type {
  ModelTag,
  ModelTagPayload
} from './model-tag'
import type {
  SyncResult,
  SyncModelItem,
  ConfirmSyncPayload,
  ConfirmSyncResult
} from './sync'

describe('API Type Definitions', () => {
  describe('ApiResponse', () => {
    it('should allow valid ApiResponse with number code', () => {
      const response: ApiResponse<string> = {
        code: 0,
        message: 'success',
        data: 'test'
      }

      expect(response.code).toBe(0)
      expect(response.message).toBe('success')
      expect(response.data).toBe('test')
    })

    it('should allow ApiResponse with object data', () => {
      const response: ApiResponse<{ id: string; name: string }> = {
        code: 0,
        message: 'ok',
        data: { id: '1', name: 'test' }
      }

      expect(response.data.id).toBe('1')
      expect(response.data.name).toBe('test')
    })

    it('should allow ApiResponse with array data', () => {
      const response: ApiResponse<string[]> = {
        code: 0,
        message: 'ok',
        data: ['a', 'b', 'c']
      }

      expect(response.data).toHaveLength(3)
      expect(response.data[0]).toBe('a')
    })

    it('should allow ApiResponse with error code', () => {
      const response: ApiResponse<null> = {
        code: 500,
        message: 'Internal Server Error',
        data: null as any
      }

      expect(response.code).toBe(500)
    })

    it('should allow ApiResponse without code/message (success response shape)', () => {
      // BE does not return code/message on success — both must be optional
      const response: ApiResponse<string> = { data: 'hello' }
      expect(response.data).toBe('hello')
      expect(response.code).toBeUndefined()
      expect(response.message).toBeUndefined()
    })

    it('should allow PaginatedResponse without code/message', () => {
      const response: PaginatedResponse<{ id: string }> = {
        data: [],
        total: 0,
        page: 1,
        page_size: 10
      }
      expect(response.total).toBe(0)
      expect(response.code).toBeUndefined()
      expect(response.message).toBeUndefined()
    })
  })

  describe('PaginatedResponse', () => {
    it('should contain pagination fields', () => {
      const response: PaginatedResponse<{ id: string }> = {
        code: 0,
        message: 'success',
        data: [{ id: '1' }, { id: '2' }],
        total: 100,
        page: 1,
        page_size: 10
      }

      expect(response.total).toBe(100)
      expect(response.page).toBe(1)
      expect(response.page_size).toBe(10)
      expect(response.data).toHaveLength(2)
    })

    it('should handle empty data with pagination info', () => {
      const response: PaginatedResponse<{ id: string }> = {
        code: 0,
        message: 'success',
        data: [],
        total: 0,
        page: 1,
        page_size: 10
      }

      expect(response.data).toEqual([])
      expect(response.total).toBe(0)
    })
  })

  describe('Provider Types', () => {
    it('should create valid CredentialFieldSchema', () => {
      const schema: CredentialFieldSchema = {
        type: 'string',
        required: true,
        secret: true,
        label: 'API Key',
        placeholder: 'sk-...',
        default: ''
      }

      expect(schema.type).toBe('string')
      expect(schema.required).toBe(true)
      expect(schema.secret).toBe(true)
    })

    it('should create CredentialSchema as record', () => {
      const schema: CredentialSchema = {
        api_key: {
          type: 'string',
          required: true,
          secret: true,
          label: 'API Key'
        },
        organization: {
          type: 'string',
          required: false,
          secret: false,
          label: 'Organization'
        }
      }

      expect(schema.api_key.required).toBe(true)
      expect(schema.organization.required).toBe(false)
    })

    it('should create valid VendorOptions', () => {
      const options: VendorOptions = {
        url_pattern: 'https://api.example.com/v1',
        auth_header: 'Authorization',
        requires_api_version: true,
        stream_usage_in_last_chunk: false,
        empty_choices_skip: true,
        supports_stream_options: true,
        supports_reasoning_content: false,
        extra_headers: {
          'X-Custom': 'value'
        }
      }

      expect(options.url_pattern).toBe('https://api.example.com/v1')
      expect(options.requires_api_version).toBe(true)
    })

    it('should create valid ProviderDefinition', () => {
      const definition: ProviderDefinition = {
        id: 'openai',
        display_name: 'OpenAI',
        adapter: 'openai',
        base_url: 'https://api.openai.com/v1',
        credential_schema: {
          api_key: {
            type: 'string',
            required: true,
            secret: true,
            label: 'API Key'
          }
        },
        vendor_options: {},
        description: 'OpenAI provider',
        icon_url: 'https://example.com/openai.png',
        doc_url: 'https://docs.openai.com',
        default_models: ['gpt-4', 'gpt-3.5-turbo'],
        builtin: true,
        enabled: true
      }

      expect(definition.id).toBe('openai')
      expect(definition.builtin).toBe(true)
      expect(definition.default_models).toHaveLength(2)
    })

    it('should create valid Provider', () => {
      const provider: Provider = {
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
        remark: 'Production provider',
        max_retries: 3,
        timeout: 30,
        tag: 'production',
        consecutive_failures: 0,
        last_check_error: '',
        model_count: 5,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }

      expect(provider.name).toBe('My OpenAI')
      expect(provider.enabled).toBe(true)
      expect(provider.weight).toBe(100)
    })

    it('should create valid ProviderPayload', () => {
      const payload: ProviderPayload = {
        name: 'New Provider',
        definition_id: 'openai',
        endpoint: 'https://api.openai.com',
        credentials: 'sk-new',
        weight: 100,
        priority: 1,
        test_model: 'gpt-4',
        remark: 'Test',
        max_retries: 3,
        timeout: 30,
        tag: 'test'
      }

      expect(payload.name).toBe('New Provider')
      expect(payload.weight).toBe(100)
    })
  })

  describe('Model Types', () => {
    it('should create valid ModelInfo', () => {
      const model: ModelInfo = {
        id: 'gpt-4',
        object: 'model',
        created: 1687882411,
        owned_by: 'openai',
        type: 'chat'
      }

      expect(model.id).toBe('gpt-4')
      expect(model.type).toBe('chat')
    })

    it('should create ModelInfo without type field', () => {
      const model: ModelInfo = {
        id: 'custom-model',
        object: 'model',
        created: 1687882411,
        owned_by: 'custom'
      }

      expect(model.type).toBeUndefined()
    })

    it('should create valid ModelsResponse', () => {
      const response: ModelsResponse = {
        object: 'list',
        data: [
          {
            id: 'gpt-4',
            object: 'model',
            created: 1687882411,
            owned_by: 'openai'
          }
        ]
      }

      expect(response.object).toBe('list')
      expect(response.data).toHaveLength(1)
    })

    it('should create valid Model', () => {
      const model: Model = {
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

      expect(model.name).toBe('GPT-4')
      expect(model.context_length).toBe(8192)
    })

    it('should create Model with provider information', () => {
      const model: Model = {
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
        updated_at: '2024-01-01T00:00:00Z',
        provider: {
          id: 'provider1',
          name: 'My OpenAI',
          definition_id: 'openai',
          enabled: true
        }
      }

      expect(model.provider?.name).toBe('My OpenAI')
    })

    it('should create valid ModelPayload', () => {
      const payload: ModelPayload = {
        name: 'GPT-4',
        provider_id: 'provider1',
        model_type: 'chat',
        upstream_model_id: 'gpt-4',
        description: 'GPT-4 model',
        context_length: 8192,
        max_output_tokens: 2048,
        input_price: 0.03,
        output_price: 0.06,
        weight: 100
      }

      expect(payload.name).toBe('GPT-4')
      expect(payload.context_length).toBe(8192)
    })
  })

  describe('ModelTag Types', () => {
    it('should create valid ModelTag', () => {
      const tag: ModelTag = {
        id: 'tag1',
        name: 'Premium',
        color: '#FF0000',
        description: 'Premium models',
        sort_order: 1,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }

      expect(tag.name).toBe('Premium')
      expect(tag.color).toBe('#FF0000')
    })

    it('should create valid ModelTagPayload', () => {
      const payload: ModelTagPayload = {
        name: 'Premium',
        color: '#FF0000',
        description: 'Premium models',
        sort_order: 1
      }

      expect(payload.name).toBe('Premium')
    })

    it('should allow ModelTagPayload without optional fields', () => {
      const payload: ModelTagPayload = {
        name: 'New Tag',
        color: '#00FF00'
      }

      expect(payload.description).toBeUndefined()
      expect(payload.sort_order).toBeUndefined()
    })
  })

  describe('Sync Types', () => {
    it('should create valid SyncModelItem', () => {
      const item: SyncModelItem = {
        id: 'gpt-4',
        owned_by: 'openai'
      }

      expect(item.id).toBe('gpt-4')
      expect(item.owned_by).toBe('openai')
    })

    it('should create valid SyncResult', () => {
      const result: SyncResult = {
        added: [{ id: 'model1', owned_by: 'openai' }],
        existing: [{ id: 'model2', owned_by: 'openai' }],
        removed: [],
        total_upstream: 2,
        total_local: 1,
        unsupported: false
      }

      expect(result.added).toHaveLength(1)
      expect(result.total_upstream).toBe(2)
    })

    it('should create valid ConfirmSyncPayload', () => {
      const payload: ConfirmSyncPayload = {
        models_to_add: ['model1', 'model2'],
        disable_removed: true
      }

      expect(payload.models_to_add).toHaveLength(2)
      expect(payload.disable_removed).toBe(true)
    })

    it('should allow ConfirmSyncPayload without disable_removed', () => {
      const payload: ConfirmSyncPayload = {
        models_to_add: ['model1']
      }

      expect(payload.disable_removed).toBeUndefined()
    })

    it('should create valid ConfirmSyncResult', () => {
      const result: ConfirmSyncResult = {
        registered: 2,
        skipped: 0,
        disabled: 1
      }

      expect(result.registered).toBe(2)
      expect(result.skipped).toBe(0)
      expect(result.disabled).toBe(1)
    })
  })

  describe('Type Compatibility', () => {
    it('should accept nested types in ApiResponse', () => {
      const response: ApiResponse<{
        provider: Provider
        models: Model[]
      }> = {
        code: 0,
        message: 'ok',
        data: {
          provider: {
            id: 'p1',
            name: 'Provider',
            definition_id: 'openai',
            type: 'LLM',
            endpoint: 'https://api.example.com',
            credentials: 'key',
            enabled: true,
            weight: 100,
            priority: 1,
            status: 1,
            test_model: 'gpt-4',
            response_time: 100,
            balance: 1000,
            remark: '',
            max_retries: 3,
            timeout: 30,
            tag: '',
            created_at: '',
            updated_at: ''
          },
          models: []
        }
      }

      expect(response.data.provider.name).toBe('Provider')
    })

    it('should accept PaginatedResponse with complex types', () => {
      const response: PaginatedResponse<Model> = {
        code: 0,
        message: 'ok',
        data: [],
        total: 0,
        page: 1,
        page_size: 10
      }

      expect(response.total).toBe(0)
    })
  })
})
