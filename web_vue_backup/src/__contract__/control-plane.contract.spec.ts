/**
 * FE↔BE 契约测试（Team I）
 *
 * 目的：对 web/src/services/control-plane.ts 中每一个对外函数发起一次 mock 调用，
 *      断言：method / URL（含 :id 占位符还原后的真实 url）/ body keys 与
 *      tests/contract/contracts.json 中 Go 端契约定义匹配。
 *
 * 注意：本测试只断言"前端发出的请求"是否符合 Go 端 handler 实际接受的形状。
 * 不依赖任何真实后端运行。
 */

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// mock http 模块（控制平面客户端唯一的网络出口）
vi.mock('@/services/http', () => {
  const fn = () => vi.fn()
  return {
    default: {
      get: fn(),
      post: fn(),
      put: fn(),
      patch: fn(),
      delete: fn(),
    },
  }
})

// 在 mock 之后再 import 业务模块
import http from '@/services/http'
import * as cp from '@/services/control-plane'
import * as gw from '@/services/gateway-api'

const mockHttp = http as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
  patch: ReturnType<typeof vi.fn>
  delete: ReturnType<typeof vi.fn>
}

// 读取 contracts.json（位置在仓库根下 tests/contract）
// vitest cwd 为 web/，所以向上两级
const contractsPath = resolve(__dirname, '../../../tests/contract/contracts.json')
const contracts = JSON.parse(readFileSync(contractsPath, 'utf-8')) as {
  control_plane: {
    routes: Array<{
      method: string
      path: string
      handler: string
      request_fields_required: string[]
      request_fields_optional: string[]
      request_fields_ignored_by_backend?: string[]
      query_params?: string[]
      response_envelope?: string
    }>
  }
  frontend_clients: {
    'control-plane.ts': {
      calls: Array<{
        fn: string
        method: string
        url: string
        body_fields?: string[]
        query_params?: string[]
      }>
    }
  }
}

/** 把 :id 占位 URL 还原为正则，用于断言 axios 调用的真实 URL */
function urlMatches(actual: string, pattern: string): boolean {
  // 把 :param 替换为非斜杠的字符段
  const re = new RegExp('^' + pattern.replace(/:[A-Za-z_]+/g, '[^/]+') + '$')
  return re.test(actual)
}

/** 在 mockHttp 上找出最近的一次调用并返回 [url, body, config] */
function lastCall(method: 'get' | 'post' | 'put' | 'patch' | 'delete') {
  const calls = (mockHttp[method] as any).mock.calls
  expect(calls.length, `期望 http.${method} 至少被调用一次`).toBeGreaterThan(0)
  return calls[calls.length - 1] as any[]
}

beforeEach(() => {
  // 给所有方法一个空成功响应，避免 await response.data.data 报错
  const ok = (data: any = {}) => Promise.resolve({ data: { data } })
  mockHttp.get.mockReset().mockImplementation(() => ok([]))
  mockHttp.post.mockReset().mockImplementation(() => ok({ id: 'mock-id' }))
  mockHttp.put.mockReset().mockImplementation(() => ok({ id: 'mock-id' }))
  mockHttp.patch.mockReset().mockImplementation(() => ok({ id: 'mock-id' }))
  mockHttp.delete.mockReset().mockImplementation(() => ok({}))
})

// ============================================================
// A. 静态契约自洽性：FE 客户端登记的 url/method 必须能在 BE routes 中找到
// ============================================================
describe('A. FE 客户端登记的 (method,url) 必须存在于 BE 路由表', () => {
  const beRoutes = contracts.control_plane.routes
  for (const call of contracts.frontend_clients['control-plane.ts'].calls) {
    it(`${call.fn}: ${call.method} ${call.url} 必须命中 BE 路由`, () => {
      const matched = beRoutes.find(
        (r) => r.method === call.method && r.path === call.url,
      )
      expect(matched, `BE 路由表中未找到 ${call.method} ${call.url}`).toBeDefined()
    })
  }
})

// ============================================================
// B. 请求字段契约：每个 body_fields 必须落在 (required ∪ optional ∪ ignored_known) 内
//    并暴露"BE 未声明字段"作为可见警告（断言为非空即记到日志）
// ============================================================
describe('B. FE 请求字段必须能被 BE 接受（或显式记录为 ignored）', () => {
  const beRoutes = contracts.control_plane.routes
  for (const call of contracts.frontend_clients['control-plane.ts'].calls) {
    if (!call.body_fields || call.body_fields.length === 0) continue
    const be = beRoutes.find((r) => r.method === call.method && r.path === call.url)!
    it(`${call.fn}: body 字段都已在 BE 契约中（required/optional/ignored）`, () => {
      const allowed = new Set([
        ...(be.request_fields_required || []),
        ...(be.request_fields_optional || []),
        ...(be.request_fields_ignored_by_backend || []),
      ])
      const unknown = call.body_fields!.filter((f) => !allowed.has(f))
      expect(unknown, `FE 字段 ${JSON.stringify(unknown)} 既不在 BE 接受也不在已知忽略列表里`).toEqual([])
    })
  }
})

// ============================================================
// C. 运行时拦截：调用每个 FE 函数 → 断言 method/URL/body
// ============================================================
describe('C. 运行时调用：每个 control-plane.ts 函数发出的请求形状', () => {
  it('getProviderDefinitions → GET /api/v1/provider-definitions', async () => {
    await cp.getProviderDefinitions()
    const [url] = lastCall('get')
    expect(url).toBe('/api/v1/provider-definitions')
  })

  it('getProviders → GET /api/v1/providers', async () => {
    await cp.getProviders()
    const [url] = lastCall('get')
    expect(url).toBe('/api/v1/providers')
  })

  it('getProvider(id) → GET /api/v1/providers/:id', async () => {
    await cp.getProvider('abc-123')
    const [url] = lastCall('get')
    expect(urlMatches(url, '/api/v1/providers/:id')).toBe(true)
    expect(url).toBe('/api/v1/providers/abc-123')
  })

  it('createProvider(payload) → POST /api/v1/providers + 必填字段齐全', async () => {
    const payload = {
      name: 'p1',
      definition_id: 'openai-compatible',
      endpoint: 'https://example.com',
      credentials: '{"api_key":"x"}',
      weight: 1,
      priority: 0,
      timeout: 30,
      max_retries: 2,
    }
    await cp.createProvider(payload as any)
    const [url, body] = lastCall('post')
    expect(url).toBe('/api/v1/providers')
    // 必填字段
    for (const k of ['name', 'definition_id', 'endpoint', 'credentials']) {
      expect(body, `缺少必填字段 ${k}`).toHaveProperty(k)
    }
  })

  it('updateProvider(id, payload) → PUT /api/v1/providers/:id', async () => {
    await cp.updateProvider('p-1', { name: 'p1', definition_id: 'x', endpoint: 'x', credentials: '' } as any)
    const [url, body] = lastCall('put')
    expect(url).toBe('/api/v1/providers/p-1')
    expect(body).toBeTypeOf('object')
  })

  it('deleteProvider(id) → DELETE /api/v1/providers/:id', async () => {
    await cp.deleteProvider('p-9')
    const [url] = lastCall('delete')
    expect(url).toBe('/api/v1/providers/p-9')
  })

  it('toggleProviderStatus(id, enabled) → PATCH /api/v1/providers/:id/status + body.enabled', async () => {
    await cp.toggleProviderStatus('p-1', false)
    const [url, body] = lastCall('patch')
    expect(url).toBe('/api/v1/providers/p-1/status')
    expect(body).toEqual({ enabled: false })
  })

  it('testProvider(id) → POST /api/v1/providers/:id/test', async () => {
    await cp.testProvider('p-1')
    const [url] = lastCall('post')
    expect(url).toBe('/api/v1/providers/p-1/test')
  })

  it('getModels(providerId?) → GET /api/v1/models + 可选 provider_id query', async () => {
    await cp.getModels('p-1')
    const [url, cfg] = lastCall('get')
    expect(url).toBe('/api/v1/models')
    expect(cfg?.params?.provider_id).toBe('p-1')
  })

  it('createModel(payload) → POST /api/v1/models + 必填字段齐全', async () => {
    await cp.createModel({
      name: 'gpt-4o',
      provider_id: 'p-1',
      model_type: 'chat',
      description: 'd',
      context_length: 128000,
    } as any)
    const [url, body] = lastCall('post')
    expect(url).toBe('/api/v1/models')
    for (const k of ['name', 'provider_id', 'model_type']) {
      expect(body, `缺少必填字段 ${k}`).toHaveProperty(k)
    }
  })

  it('updateModel(id, payload) → PUT /api/v1/models/:id', async () => {
    await cp.updateModel('m-1', { name: 'x' } as any)
    const [url] = lastCall('put')
    expect(url).toBe('/api/v1/models/m-1')
  })

  it('deleteModel(id) → DELETE /api/v1/models/:id', async () => {
    await cp.deleteModel('m-1')
    const [url] = lastCall('delete')
    expect(url).toBe('/api/v1/models/m-1')
  })

  it('checkModelNameUnique(name) → GET /api/v1/models/check-name?name=… (S0-3 修复)', async () => {
    mockHttp.get.mockImplementationOnce(() =>
      Promise.resolve({ data: { data: { available: true } } }),
    )
    const ok = await cp.checkModelNameUnique('gpt-4o')
    const [url, cfg] = lastCall('get')
    expect(url).toBe('/api/v1/models/check-name')
    expect(cfg?.params?.name).toBe('gpt-4o')
    expect(ok).toBe(true)
  })

  it('getModelTags → GET /api/v1/model-tags', async () => {
    await cp.getModelTags()
    const [url] = lastCall('get')
    expect(url).toBe('/api/v1/model-tags')
  })

  it('createModelTag(payload) → POST /api/v1/model-tags + body.name/color 必填', async () => {
    await cp.createModelTag({ name: 'vision', color: '#5DADE2', description: '', sort_order: 0 })
    const [url, body] = lastCall('post')
    expect(url).toBe('/api/v1/model-tags')
    expect(body).toHaveProperty('name')
    expect(body).toHaveProperty('color')
  })

  it('updateModelTag(id, payload) → PUT /api/v1/model-tags/:id', async () => {
    await cp.updateModelTag('t-1', { name: 'x', color: '#000' })
    const [url] = lastCall('put')
    expect(url).toBe('/api/v1/model-tags/t-1')
  })

  it('deleteModelTag(id) → DELETE /api/v1/model-tags/:id', async () => {
    await cp.deleteModelTag('t-1')
    const [url] = lastCall('delete')
    expect(url).toBe('/api/v1/model-tags/t-1')
  })

  it('getModelTagsForModel(modelId) → GET /api/v1/models/:id/tags', async () => {
    await cp.getModelTagsForModel('m-1')
    const [url] = lastCall('get')
    expect(url).toBe('/api/v1/models/m-1/tags')
  })

  it('setModelTags(modelId, tagIds) → PUT /api/v1/models/:id/tags + body.tag_ids', async () => {
    await cp.setModelTags('m-1', ['t-1', 't-2'])
    const [url, body] = lastCall('put')
    expect(url).toBe('/api/v1/models/m-1/tags')
    expect(body).toEqual({ tag_ids: ['t-1', 't-2'] })
  })

  it('syncModels(providerId) → POST /api/v1/providers/:id/sync-models', async () => {
    await cp.syncModels('p-1')
    const [url] = lastCall('post')
    expect(url).toBe('/api/v1/providers/p-1/sync-models')
  })

  it('confirmSync(providerId, payload) → POST /api/v1/providers/:id/sync-models/confirm + body.models_to_add', async () => {
    await cp.confirmSync('p-1', { models_to_add: ['m1', 'm2'] })
    const [url, body] = lastCall('post')
    expect(url).toBe('/api/v1/providers/p-1/sync-models/confirm')
    expect(body).toHaveProperty('models_to_add')
    expect(Array.isArray(body.models_to_add)).toBe(true)
  })
})

// ============================================================
// D. gateway-api.ts 契约
// ============================================================
describe('D. gateway-api.ts 客户端契约', () => {
  it('fetchModels → GET /v1/models', async () => {
    // gateway 返回 { object:"list", data:[] }，而非 { data:{data:[]} }
    mockHttp.get.mockImplementationOnce(() =>
      Promise.resolve({ data: { object: 'list', data: [] } }),
    )
    await gw.fetchModels()
    const [url] = lastCall('get')
    expect(url).toBe('/v1/models')
  })
})

// ============================================================
// E. 响应形状反向检查：FE 解构 response.data.data 的形状必须与 BE 包装一致
// ============================================================
describe('E. 响应包装一致性（用 BE 风格响应灌入 mock，断言 FE 能正常解析）', () => {
  it('getProviders 解析 BE 风格 {data:[],total,page} 不抛错', async () => {
    mockHttp.get.mockImplementationOnce(() =>
      Promise.resolve({ data: { data: [{ id: 'p1', name: 'x' }], total: 1, page: 1 } }),
    )
    const items = await cp.getProviders()
    expect(items.length).toBe(1)
  })

  it('createProvider 解析 BE 风格 {data:{...}} 不抛错', async () => {
    mockHttp.post.mockImplementationOnce(() =>
      Promise.resolve({ data: { data: { id: 'new', name: 'x' } } }),
    )
    const created = await cp.createProvider({ name: 'x', definition_id: 'd', endpoint: 'e', credentials: 'c' } as any)
    expect(created.id).toBe('new')
  })

  it('confirmSync 解析 {data:{registered,skipped,disabled}} 不抛错', async () => {
    mockHttp.post.mockImplementationOnce(() =>
      Promise.resolve({ data: { data: { registered: 1, skipped: 0, disabled: 0 } } }),
    )
    const r = await cp.confirmSync('p1', { models_to_add: ['a'] })
    expect(r.registered).toBe(1)
  })
})
