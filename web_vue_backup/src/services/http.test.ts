import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import axios from 'axios'
import { ElMessage } from 'element-plus'

vi.mock('element-plus', () => ({
  ElMessage: {
    error: vi.fn(),
    warning: vi.fn(),
    success: vi.fn()
  }
}))

vi.mock('axios', () => {
  const mockAxios: any = vi.fn()
  mockAxios.create = vi.fn()
  return {
    default: mockAxios
  }
})

interface InterceptorPair {
  success?: any
  error?: any
}

/**
 * 安装一组拦截器捕获器，并在 import('./http') 时由 mock create 触发
 */
function setupInterceptors() {
  const requestPair: InterceptorPair = {}
  const responsePair: InterceptorPair = {}
  const fakeInstance = {
    interceptors: {
      request: {
        use: (success: any, error: any) => {
          requestPair.success = success
          requestPair.error = error
        }
      },
      response: {
        use: (success: any, error: any) => {
          responsePair.success = success
          responsePair.error = error
        }
      }
    }
  }
  vi.mocked(axios.create).mockReturnValue(fakeInstance as any)
  return { requestPair, responsePair, fakeInstance }
}

describe('HTTP Service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.resetModules()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  describe('axios instance creation', () => {
    it('should create axios instance with correct default config', async () => {
      setupInterceptors()
      await import('./http')

      expect(axios.create).toHaveBeenCalledWith({
        timeout: 15000,
        headers: {
          'Content-Type': 'application/json'
        }
      })
    })
  })

  describe('request interceptor', () => {
    it('should add Authorization header with token from localStorage', async () => {
      localStorage.setItem('auth_token', 'custom-token')
      const { requestPair } = setupInterceptors()
      await import('./http')

      const config: any = { headers: {} }
      const result = requestPair.success!(config)
      expect(result.headers.Authorization).toBe('Bearer custom-token')
    })

    it('should use dev-token as fallback when auth_token is not in localStorage', async () => {
      const { requestPair } = setupInterceptors()
      await import('./http')

      const config: any = { headers: {} }
      const result = requestPair.success!(config)
      expect(result.headers.Authorization).toBe('Bearer dev-token')
    })

    it('should preserve existing headers', async () => {
      const { requestPair } = setupInterceptors()
      await import('./http')

      const config: any = { headers: { 'Custom-Header': 'value' } }
      const result = requestPair.success!(config)
      expect(result.headers['Custom-Header']).toBe('value')
      expect(result.headers.Authorization).toContain('Bearer')
    })

    it('should reject error in request interceptor error handler', async () => {
      const { requestPair } = setupInterceptors()
      await import('./http')

      const error = new Error('Request error')
      await expect(requestPair.error!(error)).rejects.toThrow('Request error')
    })
  })

  describe('response interceptor', () => {
    it('should pass through successful response', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const response: any = {
        status: 200,
        data: { code: 0, message: 'success', data: {} }
      }
      const result = responsePair.success!(response)
      expect(result).toBe(response)
    })

    it('should show error message for non-409 errors', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 500, data: { message: 'Internal Server Error' } }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).toHaveBeenCalledWith('[500] Internal Server Error')
    })

    it('should not show error message for 409 status', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 409, data: { message: 'Conflict' } }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).not.toHaveBeenCalled()
    })

    it('should use default message when error.message is not available', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 400, data: {} }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('请求失败'))
    })

    it('should reject error promise', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 500, data: { message: 'Error' } }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
    })

    it('should handle error without response object', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error = new Error('Network error')
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('Network error'))
    })
  })

  describe('http export', () => {
    it('should export http instance', async () => {
      setupInterceptors()
      const http = await import('./http').then((m) => m.default)
      expect(http).toBeDefined()
    })
  })

  describe('machine code → Chinese copy (S3-2)', () => {
    const cases: [string, string][] = [
      ['provider_name_conflict',  '渠道名称已存在，请换一个'],
      ['model_name_conflict',     '模型名称已存在，请换一个'],
      ['provider_has_models',     '该渠道下仍有模型，无法删除'],
      ['provider_disabled',       '渠道已禁用，请先启用'],
      ['provider_not_found',      '渠道不存在或已被删除'],
      ['model_not_found',         '模型不存在或已被删除'],
      ['model_provider_invalid',  '所选渠道无效或已禁用'],
      ['sync_not_supported',      '该渠道类型不支持模型自动同步'],
      ['upstream_error',          '上游服务暂时不可用，请稍后重试'],
      ['invalid_tag_id',          '提交的标签 ID 不是有效 UUID'],
      ['invalid_model_id',        '提交的模型 ID 不是有效 UUID'],
      ['bad_request',             '请求参数有误'],
      ['internal_error',          '服务器开小差了，请稍后重试'],
    ]

    it('should have all 13 error codes in ERROR_MESSAGES map', async () => {
      setupInterceptors()
      const { ERROR_MESSAGES } = await import('./http')
      expect(Object.keys(ERROR_MESSAGES)).toHaveLength(13)
      cases.forEach(([code, msg]) => {
        expect(ERROR_MESSAGES[code]).toBe(msg)
      })
    })

    cases.forEach(([code, expectedMsg]) => {
      it(`should show Chinese copy for ${code}`, async () => {
        const { responsePair } = setupInterceptors()
        await import('./http')

        const error: any = {
          response: { status: 400, data: { error: { code } } }
        }
        await expect(responsePair.error!(error)).rejects.toBe(error)
        expect(ElMessage.error).toHaveBeenCalledWith(
          expect.stringContaining(expectedMsg)
        )
      })
    })

    it('should show Chinese copy even for 409 when machine code is present', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 409, data: { error: { code: 'provider_has_models' } } }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).toHaveBeenCalledWith(
        expect.stringContaining('该渠道下仍有模型')
      )
    })

    it('should fall back to data.message when error code is unknown', async () => {
      const { responsePair } = setupInterceptors()
      await import('./http')

      const error: any = {
        response: { status: 500, data: { error: { code: 'unknown_code' }, message: 'fallback msg' } }
      }
      await expect(responsePair.error!(error)).rejects.toBe(error)
      expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('fallback msg'))
    })

    it('resolveErrorMessage returns Chinese string for known code', async () => {
      setupInterceptors()
      const { resolveErrorMessage } = await import('./http')
      const err = { response: { data: { error: { code: 'model_name_conflict' } } } }
      expect(resolveErrorMessage(err)).toBe('模型名称已存在，请换一个')
    })

    it('resolveErrorMessage returns null for unknown code', async () => {
      setupInterceptors()
      const { resolveErrorMessage } = await import('./http')
      const err = { response: { data: { error: { code: 'some_unknown' } } } }
      expect(resolveErrorMessage(err)).toBeNull()
    })

    it('resolveErrorMessage returns null when no error code present', async () => {
      setupInterceptors()
      const { resolveErrorMessage } = await import('./http')
      const err = { response: { data: { message: 'oops' } } }
      expect(resolveErrorMessage(err)).toBeNull()
    })
  })
})
