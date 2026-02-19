const GENERIC_UPSTREAM_MESSAGES = new Set([
  'upstream request failed',
  'upstream request failed after retries',
  'upstream gateway error',
  'upstream service temporarily unavailable'
])

interface ParsedGatewayError {
  type: string
  message: string
}

function parseGatewayErrorBody(raw: string): ParsedGatewayError | null {
  const text = String(raw || '').trim()
  if (!text) return null

  try {
    const parsed = JSON.parse(text) as Record<string, unknown>
    const err = parsed?.error as Record<string, unknown> | undefined
    if (!err || typeof err !== 'object') return null

    const type = typeof err.type === 'string' ? (err.type as string).trim() : ''
    const message = typeof err.message === 'string' ? (err.message as string).trim() : ''
    if (!type && !message) return null

    return { type, message }
  } catch {
    return null
  }
}

function isGenericGatewayUpstreamError(raw: string): boolean {
  const parsed = parseGatewayErrorBody(raw)
  if (!parsed) return false
  if (parsed.type !== 'upstream_error') return false
  return GENERIC_UPSTREAM_MESSAGES.has(parsed.message.toLowerCase())
}

interface ErrorDetailLike {
  upstream_error_detail?: string | null
  upstream_errors?: string | null
  upstream_error_message?: string | null
  error_body?: string | null
  error_message?: string | null
}

export function resolveUpstreamPayload(detail: ErrorDetailLike | null | undefined): string {
  if (!detail) return ''

  const candidates = [
    detail.upstream_error_detail,
    detail.upstream_errors,
    detail.upstream_error_message
  ]

  for (const candidate of candidates) {
    const payload = String(candidate || '').trim()
    if (!payload) continue
    if (payload === '[]' || payload === '{}' || payload.toLowerCase() === 'null') continue
    return payload
  }

  return ''
}

export function resolvePrimaryResponseBody(
  detail: ErrorDetailLike | null | undefined,
  errorType?: 'request' | 'upstream'
): string {
  if (!detail) return ''

  const upstreamPayload = resolveUpstreamPayload(detail)
  const errorBody = String(detail.error_body || detail.error_message || '').trim()

  if (errorType === 'upstream') {
    return upstreamPayload || errorBody
  }

  if (!errorBody) {
    return upstreamPayload
  }

  if (upstreamPayload && isGenericGatewayUpstreamError(errorBody)) {
    return upstreamPayload
  }

  return errorBody
}

export function classifyErrorType(status: number | null | undefined): 'client' | 'upstream' | 'system' | 'other' {
  if (status == null) return 'other'
  if (status >= 400 && status < 500) return 'client'
  if (status >= 500 && status < 600) return 'upstream'
  if (status === 0) return 'system'
  return 'other'
}
