/** 从 trace request_body 提取的统一消息结构 */
export interface TraceMessage {
  role: string
  content: string
}

/** 将 message content（string / block[] / 嵌套结构）规范为可读文本 */
export function normalizeTraceContent(content: unknown): string {
  if (content == null) return ''
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === 'string') return part
        if (!part || typeof part !== 'object') return ''
        const p = part as Record<string, unknown>
        if (typeof p.text === 'string') return p.text
        if (typeof p.content === 'string') return p.content
        if (p.type === 'output_text' && typeof p.text === 'string') return p.text
        if (p.type === 'input_text' && typeof p.text === 'string') return p.text
        return ''
      })
      .filter(Boolean)
      .join('\n')
  }
  if (typeof content === 'object') return JSON.stringify(content, null, 2)
  return String(content)
}

function toTraceMessage(raw: unknown): TraceMessage | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  const role = typeof o.role === 'string' ? o.role : 'unknown'
  if (o.content !== undefined) {
    return { role, content: normalizeTraceContent(o.content) }
  }
  if (typeof o.text === 'string') {
    return { role, content: o.text }
  }
  return null
}

/** 从 request_body 提取提示词消息（chat / messages / responses / embeddings） */
export function extractTraceMessages(body: unknown): TraceMessage[] {
  if (!body || typeof body !== 'object') return []
  const b = body as Record<string, unknown>
  const out: TraceMessage[] = []

  if (b.system !== undefined) {
    out.push({ role: 'system', content: normalizeTraceContent(b.system) })
  }

  if (Array.isArray(b.messages)) {
    for (const m of b.messages) {
      const msg = toTraceMessage(m)
      if (msg) out.push(msg)
    }
    return out
  }

  if (b.input !== undefined) {
    if (typeof b.input === 'string') {
      out.push({ role: 'user', content: b.input })
      return out
    }
    if (Array.isArray(b.input)) {
      for (const item of b.input) {
        const msg = toTraceMessage(item)
        if (msg) out.push(msg)
      }
      return out
    }
    out.push({ role: 'user', content: normalizeTraceContent(b.input) })
    return out
  }

  return out
}

/** 从 response_body 提取助手回复文本 */
export function extractTraceResponseText(body: unknown): string {
  if (!body || typeof body !== 'object') return ''
  const b = body as Record<string, unknown>

  const choices = b.choices
  if (Array.isArray(choices) && choices.length > 0) {
    const c0 = choices[0] as Record<string, unknown>
    const msg = c0.message as Record<string, unknown> | undefined
    const delta = c0.delta as Record<string, unknown> | undefined
    const content = msg?.content ?? delta?.content
    if (typeof content === 'string') return content
    if (content != null) return normalizeTraceContent(content)
  }

  if (b.type === 'message' && Array.isArray(b.content)) {
    return normalizeTraceContent(b.content)
  }

  if (Array.isArray(b.content)) {
    const text = normalizeTraceContent(b.content)
    if (text) return text
  }

  if (Array.isArray(b.output)) {
    const parts: string[] = []
    for (const item of b.output) {
      if (!item || typeof item !== 'object') continue
      const o = item as Record<string, unknown>
      if (o.type === 'message' || o.role === 'assistant') {
        const text = normalizeTraceContent(o.content)
        if (text) parts.push(text)
      } else if (o.type === 'reasoning') {
        const text = normalizeTraceContent(o.summary ?? o.content)
        if (text) parts.push(`[推理] ${text}`)
      }
    }
    if (parts.length) return parts.join('\n\n')
  }

  return ''
}

/** 响应是否为流式合成结构 */
export function isTraceStreamResponse(body: unknown): boolean {
  return !!(body && typeof body === 'object' && (body as Record<string, unknown>).stream)
}
