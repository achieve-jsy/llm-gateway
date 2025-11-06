import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as gatewayApi from './gateway-api'
import http from './http'

vi.mock('./http')

const mockHttp = http as any

describe('Gateway API Service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('fetchModels', () => {
    it('should fetch models from gateway API', async () => {
      const mockModels = [
        {
          id: 'gpt-4',
          object: 'model',
          created: 1687882411,
          owned_by: 'openai'
        },
        {
          id: 'gpt-3.5-turbo',
          object: 'model',
          created: 1677649963,
          owned_by: 'openai',
          type: 'chat'
        },
        {
          id: 'text-embedding-ada-002',
          object: 'model',
          created: 1671217299,
          owned_by: 'openai',
          type: 'embedding'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: mockModels
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(mockHttp.get).toHaveBeenCalledWith('/v1/models')
      expect(result).toEqual(mockModels)
      expect(result).toHaveLength(3)
    })

    it('should return empty array when no models available', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: []
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(result).toEqual([])
    })

    it('should preserve model type when present', async () => {
      const mockModels = [
        {
          id: 'gpt-4',
          object: 'model',
          created: 1687882411,
          owned_by: 'openai',
          type: 'chat'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: mockModels
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(result[0].type).toBe('chat')
    })

    it('should handle models without type field', async () => {
      const mockModels = [
        {
          id: 'custom-model',
          object: 'model',
          created: 1687882411,
          owned_by: 'custom-provider'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: mockModels
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(result[0]).toEqual(mockModels[0])
      expect(result[0].type).toBeUndefined()
    })

    it('should handle large model list', async () => {
      const mockModels = Array.from({ length: 100 }, (_, i) => ({
        id: `model-${i}`,
        object: 'model',
        created: 1687882411 + i,
        owned_by: `provider-${Math.floor(i / 10)}`
      }))

      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: mockModels
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(result).toHaveLength(100)
      expect(result[0].id).toBe('model-0')
      expect(result[99].id).toBe('model-99')
    })

    it('should extract data from response correctly', async () => {
      const mockModels = [
        {
          id: 'gpt-4',
          object: 'model',
          created: 1687882411,
          owned_by: 'openai'
        }
      ]

      const mockResponse = {
        data: {
          object: 'list',
          data: mockModels
        }
      }

      mockHttp.get.mockResolvedValueOnce(mockResponse)

      const result = await gatewayApi.fetchModels()

      expect(result).toBe(mockModels)
    })

    it('should make GET request to correct endpoint', async () => {
      mockHttp.get.mockResolvedValueOnce({
        data: { object: 'list', data: [] }
      })

      await gatewayApi.fetchModels()

      expect(mockHttp.get).toHaveBeenCalledTimes(1)
      expect(mockHttp.get).toHaveBeenCalledWith('/v1/models')
    })

    it('should handle different owned_by values', async () => {
      const mockModels = [
        {
          id: 'gpt-4',
          object: 'model',
          created: 1687882411,
          owned_by: 'openai'
        },
        {
          id: 'claude-3',
          object: 'model',
          created: 1687882412,
          owned_by: 'anthropic'
        },
        {
          id: 'gemini-pro',
          object: 'model',
          created: 1687882413,
          owned_by: 'google'
        }
      ]

      mockHttp.get.mockResolvedValueOnce({
        data: {
          object: 'list',
          data: mockModels
        }
      })

      const result = await gatewayApi.fetchModels()

      expect(result).toHaveLength(3)
      expect(result.map(m => m.owned_by)).toEqual(['openai', 'anthropic', 'google'])
    })
  })
})
