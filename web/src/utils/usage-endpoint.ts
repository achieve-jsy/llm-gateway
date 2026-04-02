/** Usage 页面端点类型（与 activeTab 对齐） */
export type UsageEndpointType = 'chat' | 'embedding' | 'rerank' | 'rerank_vl' | 'video' | ''

/** inbound_endpoint 路径 → UsageEndpointType */
export function endpointPathToType(path?: string): UsageEndpointType {
  if (path === '/v1/chat/completions') return 'chat'
  if (path === '/v1/embeddings') return 'embedding'
  if (path === '/v1/rerank') return 'rerank'
  if (path === '/v1/rerank_vl') return 'rerank_vl'
  if (path === '/v1/contents/generations/tasks') return 'video'
  return ''
}

/** 从 rerank 请求体解析 documents 数量 */
export function parseRerankDocumentsCount(body: unknown): number | null {
  if (!body || typeof body !== 'object') return null
  const docs = (body as Record<string, unknown>).documents
  if (Array.isArray(docs)) return docs.length
  return null
}

/** 列表行文档数：优先 documents_count，回退解析 request_body */
export function resolveRerankDocumentsCount(row: {
  documents_count?: number | null
  request_body?: unknown
}): number | null {
  const n = row.documents_count
  if (typeof n === 'number' && Number.isFinite(n) && n >= 0) return n
  return parseRerankDocumentsCount(row.request_body)
}
