/** 从 video_tasks.request_body / response_body 解析展示字段 */

export interface ParsedVideoPayload {
  upstreamModel: string
  prompt: string
  resolution: string
  ratio: string
  duration: string
}

export interface GroupAndModel {
  /** 对外组名；直连上游时为空 */
  groupName: string
  /** 上游模型 ID（优先 request_body.model） */
  modelName: string
}

function asObject(raw?: string | null): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const v = JSON.parse(raw)
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function strField(obj: Record<string, unknown> | null, key: string): string {
  if (!obj) return ''
  const v = obj[key]
  if (v == null || v === '') return ''
  return String(v)
}

/** 抽取 content[] 中 type=text 的文本，多段用换行拼接 */
export function extractPromptFromContent(content: unknown): string {
  if (!Array.isArray(content)) return ''
  const parts: string[] = []
  for (const item of content) {
    if (!item || typeof item !== 'object') continue
    const row = item as Record<string, unknown>
    if (row.type === 'text' && typeof row.text === 'string' && row.text.trim()) {
      parts.push(row.text.trim())
    }
  }
  return parts.join('\n')
}

export function parseVideoRequest(body?: string | null): ParsedVideoPayload {
  const obj = asObject(body)
  const durationRaw = obj?.duration
  let duration = ''
  if (durationRaw != null && durationRaw !== '') {
    duration = typeof durationRaw === 'number' ? `${durationRaw}s` : String(durationRaw)
    if (/^\d+$/.test(duration)) duration = `${duration}s`
  }
  return {
    upstreamModel: strField(obj, 'model'),
    prompt: extractPromptFromContent(obj?.content),
    resolution: strField(obj, 'resolution'),
    ratio: strField(obj, 'ratio'),
    duration,
  }
}

export function parseVideoResponse(body?: string | null): Partial<ParsedVideoPayload> {
  const obj = asObject(body)
  if (!obj) return {}
  const durationRaw = obj.duration
  let duration = ''
  if (durationRaw != null && durationRaw !== '') {
    duration = typeof durationRaw === 'number' ? `${durationRaw}s` : String(durationRaw)
    if (/^\d+$/.test(duration)) duration = `${duration}s`
  }
  return {
    upstreamModel: strField(obj, 'model') || undefined,
    resolution: strField(obj, 'resolution') || undefined,
    ratio: strField(obj, 'ratio') || undefined,
    duration: duration || undefined,
  }
}

/**
 * 组名 vs 上游模型：
 * - 若 request_body.model 存在且与 task.model 不同 → task.model 为组名，body.model 为上游
 * - 否则无独立组名，模型取 body.model || task.model
 */
export function resolveGroupAndModel(task: {
  model?: string
  request_body?: string
}): GroupAndModel {
  const parsed = parseVideoRequest(task.request_body)
  const stored = (task.model || '').trim()
  const upstream = (parsed.upstreamModel || '').trim()

  if (upstream && stored && upstream !== stored) {
    return { groupName: stored, modelName: upstream }
  }
  return {
    groupName: '',
    modelName: upstream || stored,
  }
}

/** 详情展示用：响应优先覆盖分辨率等参数，提示词仍来自请求 */
export function resolveVideoDisplay(task: {
  model?: string
  request_body?: string
  response_body?: string
}): ParsedVideoPayload & GroupAndModel {
  const req = parseVideoRequest(task.request_body)
  const resp = parseVideoResponse(task.response_body)
  const gm = resolveGroupAndModel(task)
  return {
    ...gm,
    prompt: req.prompt,
    resolution: resp.resolution || req.resolution,
    ratio: resp.ratio || req.ratio,
    duration: resp.duration || req.duration,
    upstreamModel: gm.modelName,
  }
}

export function maskKeyId(id?: string | null): string {
  if (!id) return '—'
  if (id.length <= 14) return id
  return `${id.slice(0, 6)}···${id.slice(-4)}`
}

export function resolveKeyLabel(task: {
  api_key_name?: string
  api_key_id?: string
}): string {
  return task.api_key_name || maskKeyId(task.api_key_id) || '—'
}
