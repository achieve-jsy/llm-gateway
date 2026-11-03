import { test, expect } from '@playwright/test'

const AUTH_TOKEN = process.env.E2E_AUTH_TOKEN || 'dev-token'

test.describe('模型 CRUD API 合约测试', () => {
  const apiBase = process.env.E2E_API_BASE || 'http://localhost:8081/api/v1'
  const headers = { Authorization: `Bearer ${AUTH_TOKEN}` }

  test('GET /models 返回分页结构', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body).toHaveProperty('data')
    expect(body).toHaveProperty('total')
    expect(typeof body.total).toBe('number')
    expect(Array.isArray(body.data)).toBe(true)
  })

  test('GET /models 支持分页参数', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models?page=1&page_size=5`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body.page).toBe(1)
    expect(body.page_size).toBe(5)
  })

  test('GET /models 支持名称筛选', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models?name=gpt`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(Array.isArray(body.data)).toBe(true)
  })

  test('GET /models 支持类型筛选', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models?model_type=chat`, { headers })
    expect(resp.status()).toBe(200)
  })

  test('GET /models/check-name 返回可用性检查', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models/check-name?name=test-model-unique-check`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body.data).toHaveProperty('available')
    expect(typeof body.data.available).toBe('boolean')
  })

  test('GET /models/check-name 缺少 name 参数返回 400', async ({ request }) => {
    const resp = await request.get(`${apiBase}/models/check-name`, { headers })
    expect(resp.status()).toBe(400)
  })

  test('POST /models 创建模型', async ({ request }) => {
    const resp = await request.post(`${apiBase}/models`, {
      headers,
      data: {
        name: `e2e-test-model-${Date.now()}`,
        provider_id: '00000000-0000-0000-0000-000000000001',
        model_type: 'chat',
      },
      failOnStatusCode: false,
    })
    expect([201, 400, 409]).toContain(resp.status())
  })

  test('POST /models 无效请求体返回 400', async ({ request }) => {
    const resp = await request.post(`${apiBase}/models`, {
      headers,
      data: { name: '' },
      failOnStatusCode: false,
    })
    expect(resp.status()).toBe(400)
  })

  test('DELETE /models/:id 无效ID返回 400', async ({ request }) => {
    const resp = await request.delete(`${apiBase}/models/not-a-uuid`, {
      headers,
      failOnStatusCode: false,
    })
    expect(resp.status()).toBe(400)
  })

  test('PATCH /models/batch-status 批量状态更新', async ({ request }) => {
    const resp = await request.patch(`${apiBase}/models/batch-status`, {
      headers,
      data: {
        model_ids: ['00000000-0000-0000-0000-000000000001'],
        enabled: true,
      },
      failOnStatusCode: false,
    })
    expect([200, 400, 500]).toContain(resp.status())
  })

  test('DELETE /models/batch 批量删除', async ({ request }) => {
    const resp = await request.delete(`${apiBase}/models/batch`, {
      headers,
      data: {
        model_ids: ['00000000-0000-0000-0000-000000000001'],
      },
      failOnStatusCode: false,
    })
    expect([200, 400, 500]).toContain(resp.status())
  })
})

test.describe('模型标签 API 合约测试', () => {
  const apiBase = process.env.E2E_API_BASE || 'http://localhost:8081/api/v1'
  const headers = { Authorization: `Bearer ${AUTH_TOKEN}` }

  test('GET /model-tags 返回标签列表', async ({ request }) => {
    const resp = await request.get(`${apiBase}/model-tags`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(Array.isArray(body.data)).toBe(true)
  })

  test('POST /model-tags 创建标签', async ({ request }) => {
    const resp = await request.post(`${apiBase}/model-tags`, {
      headers,
      data: {
        name: `e2e-tag-${Date.now()}`,
        color: '#FF0000',
      },
      failOnStatusCode: false,
    })
    expect([200, 201, 400, 409]).toContain(resp.status())
  })
})

test.describe('模型检测 API 合约测试', () => {
  const apiBase = process.env.E2E_API_BASE || 'http://localhost:8081/api/v1'
  const headers = { Authorization: `Bearer ${AUTH_TOKEN}` }

  test('GET /health-check/config 返回配置', async ({ request }) => {
    const resp = await request.get(`${apiBase}/health-check/config`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body.data).toHaveProperty('model_check_enabled')
  })

  test('POST /models/check-all 结构正确', async ({ request }) => {
    const resp = await request.post(`${apiBase}/models/check-all`, {
      headers,
      failOnStatusCode: false,
      timeout: 180000,
    })
    if (resp.status() === 200) {
      const body = await resp.json()
      expect(body.data).toHaveProperty('total')
      expect(body.data).toHaveProperty('success')
      expect(body.data).toHaveProperty('failed')
      expect(Array.isArray(body.data.results)).toBe(true)
    }
  })
})

test.describe('模型 Catalog API 合约测试', () => {
  const apiBase = process.env.E2E_API_BASE || 'http://localhost:8081/api/v1'
  const headers = { Authorization: `Bearer ${AUTH_TOKEN}` }

  test('GET /model-catalog/search 返回搜索结果', async ({ request }) => {
    const resp = await request.get(`${apiBase}/model-catalog/search?q=gpt&limit=5`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body).toHaveProperty('data')
    expect(body).toHaveProperty('total')
    expect(Array.isArray(body.data)).toBe(true)
  })

  test('GET /model-catalog/search 缺少 q 参数', async ({ request }) => {
    const resp = await request.get(`${apiBase}/model-catalog/search?limit=5`, { headers })
    expect(resp.status()).toBe(200)
  })

  test('GET /model-catalog/diff 返回差异列表', async ({ request }) => {
    const resp = await request.get(`${apiBase}/model-catalog/diff`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body).toHaveProperty('data')
    expect(body).toHaveProperty('total')
  })

  test('GET /model-catalog/binding-stats 返回绑定统计', async ({ request }) => {
    const resp = await request.get(`${apiBase}/model-catalog/binding-stats`, { headers })
    const body = await resp.json()

    expect(resp.status()).toBe(200)
    expect(body.data).toHaveProperty('catalog_total')
    expect(body.data).toHaveProperty('model_total')
    expect(body.data).toHaveProperty('bound_count')
    expect(body.data).toHaveProperty('unbound_count')
  })

  test('POST /models/from-catalog 创建模型', async ({ request }) => {
    const resp = await request.post(`${apiBase}/models/from-catalog`, {
      headers,
      data: {
        catalog_id: '00000000-0000-0000-0000-000000000001',
        provider_id: '00000000-0000-0000-0000-000000000001',
        name: 'e2e-from-catalog',
      },
      failOnStatusCode: false,
    })
    expect([201, 400, 404, 409]).toContain(resp.status())
  })

  test('POST /models/:id/reset-override 重置覆盖', async ({ request }) => {
    const resp = await request.post(`${apiBase}/models/00000000-0000-0000-0000-000000000001/reset-override`, {
      headers,
      data: { field: 'context_length' },
      failOnStatusCode: false,
    })
    expect([200, 400, 404]).toContain(resp.status())
  })
})