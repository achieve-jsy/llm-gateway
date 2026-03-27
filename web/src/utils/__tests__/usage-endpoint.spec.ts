import { describe, expect, it } from 'vitest'
import {
  parseRerankDocumentsCount,
  resolveRerankDocumentsCount,
  endpointPathToType,
} from '../usage-endpoint'

describe('usage-endpoint', () => {
  it('endpointPathToType 映射常见路径', () => {
    expect(endpointPathToType('/v1/embeddings')).toBe('embedding')
    expect(endpointPathToType('/v1/rerank')).toBe('rerank')
    expect(endpointPathToType('/v1/chat/completions')).toBe('chat')
  })

  it('parseRerankDocumentsCount 从 body 解析', () => {
    expect(parseRerankDocumentsCount({ documents: ['a', 'b'] })).toBe(2)
    expect(parseRerankDocumentsCount({})).toBeNull()
    expect(parseRerankDocumentsCount(null)).toBeNull()
  })

  it('resolveRerankDocumentsCount 优先 documents_count', () => {
    expect(resolveRerankDocumentsCount({ documents_count: 5, request_body: {} })).toBe(5)
    expect(resolveRerankDocumentsCount({ documents_count: 0 })).toBe(0)
    expect(resolveRerankDocumentsCount({ request_body: { documents: ['x'] } })).toBe(1)
    expect(resolveRerankDocumentsCount({})).toBeNull()
  })
})
