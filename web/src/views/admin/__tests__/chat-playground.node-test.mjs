/**
 * ChatPlaygroundPage 核心逻辑测试（纯 Node.js，无需 vitest/rollup）
 *
 * 测试目标：验证请求体构建、参数传递、流式解析等核心逻辑
 * 运行方式：node src/views/admin/__tests__/chat-playground.node-test.mjs
 */

import { strict as assert } from 'node:assert'
import { TextEncoder } from 'node:util'

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

async function testAsync(name, fn) {
  try {
    await fn()
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
// 模拟 ChatPlaygroundPage 的核心逻辑（从 .vue 文件提取）
// ═══════════════════════════════════════════════════════════════════════════════

function buildRequestBody(config, messages) {
  const reqMessages = []
  if (config.systemPrompt) {
    reqMessages.push({ role: 'system', content: config.systemPrompt })
  }
  reqMessages.push(...messages.filter(m => m.role !== 'system'))

  const stopSeqs = config.stop.trim()
    ? config.stop.split(',').map(s => s.trim()).filter(Boolean)
    : undefined

  return {
    model: config.model,
    messages: reqMessages.map(m => ({ role: m.role, content: m.content })),
    stream: config.stream,
    temperature: config.temperature,
    top_p: config.topP,
    max_tokens: config.maxTokens,
    ...(stopSeqs && { stop: stopSeqs }),
  }
}

function buildUrl(apiBase) {
  const baseUrl = (apiBase || '/v1').replace(/\/$/, '')
  return `${baseUrl}/chat/completions`
}

function buildHeaders(apiKey, authToken) {
  const token = apiKey || authToken || 'dev-token'
  return {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  }
}

function parseSSELine(line) {
  if (!line.startsWith('data: ')) return null
  const data = line.slice(6).trim()
  if (data === '[DONE]') return { done: true }
  try {
    const chunk = JSON.parse(data)
    const delta = chunk.choices?.[0]?.delta
    return { content: delta?.content || '', reasoning_content: delta?.reasoning_content || '' }
  } catch {
    return null
  }
}

function parseSSEBuffer(buffer) {
  const results = { content: '', reasoning_content: '' }
  const lines = buffer.split('\n')
  for (const line of lines) {
    const parsed = parseSSELine(line)
    if (parsed && !parsed.done) {
      results.content += parsed.content
      results.reasoning_content += parsed.reasoning_content
    }
  }
  return results
}

function computeModelHint(meta) {
  if (!meta) return ''
  const parts = []
  if (meta.context_length) parts.push(`上下文 ${(meta.context_length / 1024).toFixed(0)}K`)
  if (meta.max_output_tokens) parts.push(`最大输出 ${(meta.max_output_tokens / 1024).toFixed(0)}K`)
  return parts.join(' / ')
}

// ═══════════════════════════════════════════════════════════════════════════════
// 测试
// ═══════════════════════════════════════════════════════════════════════════════

describe('请求体构建', () => {
  test('包含所有基本参数', () => {
    const config = {
      model: 'qwen3-next-80b',
      temperature: 0.7,
      topP: 0.9,
      maxTokens: 4096,
      stop: '',
      systemPrompt: '',
      stream: true,
    }
    const messages = [{ role: 'user', content: '你好' }]
    const body = buildRequestBody(config, messages)

    assert.equal(body.model, 'qwen3-next-80b')
    assert.equal(body.temperature, 0.7)
    assert.equal(body.top_p, 0.9)
    assert.equal(body.max_tokens, 4096)
    assert.equal(body.stream, true)
    assert.equal(body.stop, undefined)
    assert.equal(body.messages.length, 1)
    assert.equal(body.messages[0].role, 'user')
    assert.equal(body.messages[0].content, '你好')
  })

  test('包含 system prompt 时插入 system 消息', () => {
    const config = {
      model: 'gpt-4',
      temperature: 0.5,
      topP: 1.0,
      maxTokens: 2048,
      stop: '',
      systemPrompt: '你是一个翻译助手',
      stream: true,
    }
    const messages = [{ role: 'user', content: 'translate this' }]
    const body = buildRequestBody(config, messages)

    assert.equal(body.messages.length, 2)
    assert.equal(body.messages[0].role, 'system')
    assert.equal(body.messages[0].content, '你是一个翻译助手')
    assert.equal(body.messages[1].role, 'user')
  })

  test('停止序列正确解析（逗号分隔）', () => {
    const config = {
      model: 'test',
      temperature: 0.7,
      topP: 1.0,
      maxTokens: 2048,
      stop: 'END, STOP, ###',
      systemPrompt: '',
      stream: true,
    }
    const body = buildRequestBody(config, [{ role: 'user', content: 'hi' }])
    assert.deepEqual(body.stop, ['END', 'STOP', '###'])
  })

  test('停止序列为空时不包含 stop 字段', () => {
    const config = {
      model: 'test',
      temperature: 0.7,
      topP: 1.0,
      maxTokens: 2048,
      stop: '   ',
      systemPrompt: '',
      stream: true,
    }
    const body = buildRequestBody(config, [{ role: 'user', content: 'hi' }])
    assert.equal(body.stop, undefined)
    assert.equal('stop' in body, false)
  })

  test('过滤 assistant 消息中的 system role', () => {
    const config = {
      model: 'test',
      temperature: 0.7,
      topP: 1.0,
      maxTokens: 2048,
      stop: '',
      systemPrompt: 'sys',
      stream: true,
    }
    const messages = [
      { role: 'user', content: 'q1' },
      { role: 'assistant', content: 'a1' },
      { role: 'user', content: 'q2' },
    ]
    const body = buildRequestBody(config, messages)
    // system + 3 history messages
    assert.equal(body.messages.length, 4)
    assert.equal(body.messages[0].role, 'system')
  })

  test('非流式模式时 stream=false', () => {
    const config = {
      model: 'test',
      temperature: 0.7,
      topP: 1.0,
      maxTokens: 2048,
      stop: '',
      systemPrompt: '',
      stream: false,
    }
    const body = buildRequestBody(config, [{ role: 'user', content: 'hi' }])
    assert.equal(body.stream, false)
  })
})

describe('URL 构建', () => {
  test('默认使用 /v1', () => {
    assert.equal(buildUrl(''), '/v1/chat/completions')
  })

  test('自定义 API Base', () => {
    assert.equal(buildUrl('https://api.example.com/v1'), 'https://api.example.com/v1/chat/completions')
  })

  test('移除末尾斜杠', () => {
    assert.equal(buildUrl('https://api.example.com/v1/'), 'https://api.example.com/v1/chat/completions')
  })
})

describe('请求头构建', () => {
  test('使用自定义 API Key', () => {
    const h = buildHeaders('sk-123', 'auth-token')
    assert.equal(h['Authorization'], 'Bearer sk-123')
  })

  test('无自定义 Key 时使用 auth token', () => {
    const h = buildHeaders('', 'my-auth-token')
    assert.equal(h['Authorization'], 'Bearer my-auth-token')
  })

  test('均为空时使用 dev-token', () => {
    const h = buildHeaders('', '')
    assert.equal(h['Authorization'], 'Bearer dev-token')
  })
})

describe('SSE 流式解析', () => {
  test('解析普通 content', () => {
    const buffer = 'data: {"choices":[{"delta":{"content":"你好"}}]}\ndata: {"choices":[{"delta":{"content":"世界"}}]}\n'
    const result = parseSSEBuffer(buffer)
    assert.equal(result.content, '你好世界')
  })

  test('解析 reasoning_content', () => {
    const buffer = 'data: {"choices":[{"delta":{"reasoning_content":"思考中"}}]}\ndata: {"choices":[{"delta":{"content":"答案"}}]}\n'
    const result = parseSSEBuffer(buffer)
    assert.equal(result.reasoning_content, '思考中')
    assert.equal(result.content, '答案')
  })

  test('[DONE] 不产生内容', () => {
    const buffer = 'data: {"choices":[{"delta":{"content":"hi"}}]}\ndata: [DONE]\n'
    const result = parseSSEBuffer(buffer)
    assert.equal(result.content, 'hi')
  })

  test('忽略非 data: 前缀的行', () => {
    const buffer = ': comment\nevent: message\ndata: {"choices":[{"delta":{"content":"ok"}}]}\n'
    const result = parseSSEBuffer(buffer)
    assert.equal(result.content, 'ok')
  })

  test('处理格式错误的 JSON', () => {
    const buffer = 'data: {invalid json}\ndata: {"choices":[{"delta":{"content":"valid"}}]}\n'
    const result = parseSSEBuffer(buffer)
    assert.equal(result.content, 'valid')
  })
})

describe('模型元数据提示', () => {
  test('正确计算上下文窗口提示', () => {
    const hint = computeModelHint({ context_length: 131072, max_output_tokens: 8192 })
    assert.equal(hint, '上下文 128K / 最大输出 8K')
  })

  test('只有 context_length', () => {
    const hint = computeModelHint({ context_length: 32768, max_output_tokens: 0 })
    assert.equal(hint, '上下文 32K')
  })

  test('无元数据时返回空字符串', () => {
    assert.equal(computeModelHint(null), '')
  })
})

// ═══════════════════════════════════════════════════════════════════════════════

console.log(`\n${'═'.repeat(50)}`)
console.log(`结果: ${passed} 通过, ${failed} 失败, 共 ${passed + failed} 个`)
console.log('═'.repeat(50))

process.exit(failed > 0 ? 1 : 0)
