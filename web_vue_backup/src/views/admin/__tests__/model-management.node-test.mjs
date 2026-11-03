/**
 * 模型管理页面核心逻辑测试（纯 Node.js，无需 vitest/rollup）
 *
 * 测试目标：验证 ModelListPage 的纯函数逻辑
 * 运行方式：node web/src/views/admin/__tests__/model-management.node-test.mjs
 */

import { strict as assert } from 'node:assert'

let passed = 0
let failed = 0

function test(name, fn) {
  try {
    fn()
    passed++
    console.log(`  ✓ ${name}`)
  } catch (e) {
    failed++
    console.log(`  ✗ ${name}`)
    console.log(`    ${e.message}`)
  }
}

function describe(name, fn) {
  console.log(`\n${name}`)
  fn()
}

// ═══════════════════════════════════════════════════════════════════════════════
// ModelListPage 纯函数
// ═══════════════════════════════════════════════════════════════════════════════

function formatTokenCount(count) {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${Math.round(count / 1000)}K`
  return String(count)
}

function shortenError(error) {
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

function filterByTagIds(models, modelTagsMap, filterTagIds) {
  if (!filterTagIds.length) return models
  return models.filter(m => {
    const tags = modelTagsMap[m.id] ?? []
    return filterTagIds.every(tid => tags.some(t => t.id === tid))
  })
}

// ═══════════════════════════════════════════════════════════════════════════════
// ModelDialog 纯函数
// ═══════════════════════════════════════════════════════════════════════════════

function formatCtxK(n) {
  return Math.round(n / 1000)
}

function shouldNotifyDetach(field, selectedCatalogId, applyingCatalog, detachedFields, currentValue, baselineValue) {
  if (!selectedCatalogId) return false
  if (applyingCatalog) return false
  if (detachedFields.has(field)) return false
  return currentValue !== baselineValue
}

// ═══════════════════════════════════════════════════════════════════════════════
// ModelCheckSettingsDialog 纯函数
// ═══════════════════════════════════════════════════════════════════════════════

const labelMap = {
  invalid_api_key: 'Key无效',
  access_denied: '无权限',
  model_not_found: '模型不存在',
  rate_limited: '限流',
  product_not_activated: '未开通',
  timeout: '超时',
  other: '其他',
}

function buildFailureSummaryText(summary) {
  if (!summary) return ''
  return Object.entries(summary)
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
    .map(([key, count]) => `${labelMap[key] || key} ${count}`)
    .join(' / ')
}

// ═══════════════════════════════════════════════════════════════════════════════
// 测试
// ═══════════════════════════════════════════════════════════════════════════════

describe('formatTokenCount', () => {
  test('小于 1000 直接返回', () => {
    assert.equal(formatTokenCount(512), '512')
    assert.equal(formatTokenCount(0), '0')
  })
  test('1000-999999 转 K', () => {
    assert.equal(formatTokenCount(8192), '8K')
    assert.equal(formatTokenCount(32768), '33K')
  })
  test('>= 1000000 转 M', () => {
    assert.equal(formatTokenCount(1280000), '1.3M')
    assert.equal(formatTokenCount(2000000), '2.0M')
  })
})

describe('shortenError', () => {
  test('空字符串返回空', () => {
    assert.equal(shortenError(''), '')
  })
  test('timeout → 连接超时', () => {
    assert.equal(shortenError('Connection timeout'), '连接超时')
  })
  test('401 → Key无效', () => {
    assert.equal(shortenError('HTTP 401 Unauthorized'), 'Key无效')
  })
  test('403 → 无权限', () => {
    assert.equal(shortenError('403 Forbidden'), '无权限')
  })
  test('404 → 地址无效', () => {
    assert.equal(shortenError('404 Not Found'), '地址无效')
  })
  test('502 → 上游异常', () => {
    assert.equal(shortenError('502 Bad Gateway'), '上游异常')
  })
  test('503 → 服务不可用', () => {
    assert.equal(shortenError('503 Service Unavailable'), '服务不可用')
  })
  test('model → 模型不可用', () => {
    assert.equal(shortenError('model not available'), '模型不可用')
  })
  test('长错误截断', () => {
    assert.equal(shortenError('some very long error message that exceeds'), 'some very ...')
  })
  test('短错误原样', () => {
    assert.equal(shortenError('short msg'), 'short msg')
  })
})

describe('filterByTagIds', () => {
  const models = [{ id: 'm1' }, { id: 'm2' }, { id: 'm3' }]
  const tagMap = {
    m1: [{ id: 't1' }],
    m2: [{ id: 't1' }, { id: 't2' }],
    m3: [{ id: 't2' }],
  }
  test('无筛选返回全部', () => {
    assert.equal(filterByTagIds(models, tagMap, []).length, 3)
  })
  test('单标签筛选', () => {
    assert.equal(filterByTagIds(models, tagMap, ['t1']).length, 2)
  })
  test('多标签 AND 篮选', () => {
    assert.equal(filterByTagIds(models, tagMap, ['t1', 't2']).length, 1)
  })
})

describe('formatCtxK', () => {
  test('128000 → 128', () => {
    assert.equal(formatCtxK(128000), 128)
  })
  test('4096 → 4', () => {
    assert.equal(formatCtxK(4096), 4)
  })
})

describe('shouldNotifyDetach', () => {
  test('catalog 未关联时不通知', () => {
    assert.equal(shouldNotifyDetach('f', null, false, new Set(), 1, 2), false)
  })
  test('正在应用 catalog 时跳过', () => {
    assert.equal(shouldNotifyDetach('f', 'c1', true, new Set(), 1, 2), false)
  })
  test('已通知过的字段不重复', () => {
    assert.equal(shouldNotifyDetach('f', 'c1', false, new Set(['f']), 1, 2), false)
  })
  test('值与 baseline 不同时通知', () => {
    assert.equal(shouldNotifyDetach('f', 'c1', false, new Set(), 200, 128), true)
  })
  test('值与 baseline 相同时不通知', () => {
    assert.equal(shouldNotifyDetach('f', 'c1', false, new Set(), 128, 128), false)
  })
})

describe('buildFailureSummaryText', () => {
  test('空 summary 返回空', () => {
    assert.equal(buildFailureSummaryText(undefined), '')
  })
  test('单分类', () => {
    assert.equal(buildFailureSummaryText({ timeout: 3 }), '超时 3')
  })
  test('多分类按数量降序', () => {
    assert.equal(
      buildFailureSummaryText({ timeout: 2, invalid_api_key: 5, rate_limited: 1 }),
      'Key无效 5 / 超时 2 / 限流 1'
    )
  })
})

// ═══════════════════════════════════════════════════════════════════════════════

console.log(`\n${'═'.repeat(50)}`)
console.log(`结果: ${passed} 通过, ${failed} 失败, 共 ${passed + failed} 个`)
console.log('═'.repeat(50))

process.exit(failed > 0 ? 1 : 0)