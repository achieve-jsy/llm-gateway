/**
 * OpenCode V2 插件：启动时向网关动态发现模型，把模型（含能力/上下文上限）注入已有的 provider。
 *
 * 为什么 V1 插件在 V2 不能用：2.x 换掉了整套插件契约。V2 的入口是
 * `export default { id, setup(ctx) }`，provider/model 通过 transform 钩子写入注册表：
 *   - `ctx.provider.transform(registry => ...)` 里用 `registry.models.update(pid, id, mutate)`
 *     写模型（其余字段由 V2 补默认值）；
 *   - `setup` 的返回值会被 await，所以可以「先抓模型清单，再注册 transform」，
 *     第一次 transform 执行时数据就已就绪，不需要再 reload（少一轮竞态）。
 *
 * 两个实测坑（改这块前务必知道）：
 *   1. 不要自己 `registry.add({info})` 造 provider：配置里的 provider 由内置的
 *      `opencode.config.provider` 插件合并进注册表，我们再加一份会让它 transform 崩溃（/api/model 500）。
 *      所以 provider（baseURL/apiKey/name）永远由安装脚本写进 opencode.json，插件只补模型。
 *   2. 模型的 `capabilities.output` 是必填键，整块覆盖 capabilities 时漏了它，V2 编码响应会直接 500。
 *
 * 网关地址与 key 由安装脚本写在与本文件同目录的 opencode-models-discovery.json：
 * 插件执行时 opencode.json 里的 provider 还没进注册表，读不到。
 */
import { appendFileSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildAPIURL, discoverModelsFromProvider } from '../utils/openai-compatible-api'

const SIDECAR_FILE = 'opencode-models-discovery.json'
const MODEL_INFO_ENDPOINT = '/v1/model/info'
const FETCH_TIMEOUT_MS = 15000
const DEFAULT_MODALITIES = ['text']

export interface DiscoveryConfig {
  providerID: string
  providerName?: string
  baseURL: string
  apiKey?: string
}

export interface V2Model {
  id: string
  name: string
  capabilities: Record<string, unknown>
  limit?: { context: number; output: number }
}

function sidecarPath(): string {
  try {
    return join(dirname(fileURLToPath(import.meta.url)), SIDECAR_FILE)
  } catch {
    return SIDECAR_FILE
  }
}

/** 读取安装脚本写下的网关信息；缺失/损坏返回 null（插件静默不生效）。 */
export function readDiscoveryConfig(): DiscoveryConfig | null {
  try {
    const raw = JSON.parse(readFileSync(sidecarPath(), 'utf8'))
    if (!raw || typeof raw.providerID !== 'string' || typeof raw.baseURL !== 'string') return null
    return {
      providerID: raw.providerID,
      providerName: typeof raw.providerName === 'string' ? raw.providerName : raw.providerID,
      baseURL: raw.baseURL,
      apiKey: typeof raw.apiKey === 'string' ? raw.apiKey : undefined,
    }
  } catch {
    return null
  }
}

function debug(message: string): void {
  if (!process.env.OPENCODE_DISCOVERY_DEBUG) return
  try {
    appendFileSync('/tmp/v2plugin.log', new Date().toISOString() + ' ' + message + '\n')
  } catch {
    // 调试输出失败不影响主流程
  }
}

function usable(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function reasoningEfforts(mi: Record<string, unknown>): string[] | null {
  // 与 V1 插件、Go 侧静态快照同一套判定：只有暴露了 reasoning_effort 才给 effort 变体。
  if (mi.supports_reasoning !== true) return null
  const params = Array.isArray(mi.supported_openai_params) ? mi.supported_openai_params : []
  if (!params.includes('reasoning_effort')) return null
  const efforts: string[] = []
  if (mi.supports_none_reasoning_effort === true) efforts.push('none')
  if (mi.supports_minimal_reasoning_effort === true) efforts.push('minimal')
  if (mi.supports_low_reasoning_effort !== false) efforts.push('low')
  efforts.push('medium', 'high')
  if (mi.supports_xhigh_reasoning_effort === true) efforts.push('xhigh')
  if (mi.supports_max_reasoning_effort === true) efforts.push('max')
  return efforts
}

/** 把 LiteLLM 的 model_info 映射成 V2 的 capabilities/limit（与 Go 侧静态快照同构）。 */
export function buildV2Model(modelId: string, mi: Record<string, unknown>): V2Model | null {
  const mode = mi.mode
  // 只注入 chat 模型：V2 只把 chat 模型提供进会话。
  if (typeof mode === 'string' && mode.length > 0 && mode !== 'chat') return null

  const caps: Record<string, unknown> = {}
  if (mi.supports_function_calling === true || mi.supports_tool_choice === true) caps.tools = true

  const declared = mi.input_modalities ?? mi.supported_modalities
  if (Array.isArray(declared) && declared.length > 0) {
    const inputs = declared.map((x) => String(x).toLowerCase())
    caps.input = inputs.includes('text') ? inputs : ['text', ...inputs]
  } else if (mi.supports_vision === true) {
    caps.input = ['text', 'image']
  } else if (mi.supports_vision === false) {
    caps.input = ['text']
  }

  const declaredOutput = mi.output_modalities
  caps.output =
    Array.isArray(declaredOutput) && declaredOutput.length > 0
      ? declaredOutput.map((x) => String(x).toLowerCase())
      : DEFAULT_MODALITIES

  const efforts = reasoningEfforts(mi)
  if (efforts) caps.reasoning = { supported: true, effort: efforts }

  const model: V2Model = { id: modelId, name: modelId, capabilities: caps }

  const ctxLimit = usable(mi.max_input_tokens) ? mi.max_input_tokens : mi.max_tokens
  const outLimit = usable(mi.max_output_tokens) ? mi.max_output_tokens : mi.max_tokens
  if (usable(ctxLimit) && usable(outLimit)) {
    model.limit = { context: Math.trunc(ctxLimit), output: Math.trunc(outLimit) }
  }
  return model
}

async function fetchModelInfo(config: DiscoveryConfig): Promise<Map<string, Record<string, unknown>>> {
  const map = new Map<string, Record<string, unknown>>()
  try {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (config.apiKey) headers.Authorization = `Bearer ${config.apiKey}`
    const response = await fetch(buildAPIURL(config.baseURL, MODEL_INFO_ENDPOINT), {
      method: 'GET',
      headers,
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (!response.ok) return map
    const payload = (await response.json()) as { data?: Array<Record<string, any>> }
    for (const entry of Array.isArray(payload?.data) ? payload.data : []) {
      const mi = (entry?.model_info ?? {}) as Record<string, unknown>
      const key = (entry?.model_name as string) || (mi.key as string)
      if (!key) continue
      map.set(key, mi)
      map.set(key.toLowerCase(), mi)
    }
  } catch {
    // 拿不到能力就退化成 V2 默认假设，不影响模型本身被发现。
  }
  return map
}

/** 发现结果：模型清单 + 能力富化。网关不可达时返回空数组（保留配置里的静态快照）。 */
export async function discoverModels(config: DiscoveryConfig): Promise<V2Model[]> {
  const discovered = await discoverModelsFromProvider(config.baseURL, config.apiKey)
  if (!discovered.ok) return []
  const info = await fetchModelInfo(config)
  const models: V2Model[] = []
  for (const entry of discovered.models) {
    const id = entry?.id
    if (!id) continue
    const mi = info.get(id) ?? info.get(id.toLowerCase()) ?? {}
    const model = buildV2Model(id, mi)
    if (model) models.push(model)
  }
  return models
}

const PLUGIN_ID = 'llmgateway.models-discovery'

export default {
  id: PLUGIN_ID,
  async setup(ctx: any) {
    const config = readDiscoveryConfig()
    if (!config) return

    const models = await discoverModels(config)
    debug('discovered ' + models.length + ': ' + models.map((m) => m.id).join(','))
    if (models.length === 0) return

    ctx.provider.transform((registry: any) => {
      // 配置文件里的 provider 此时还没并入注册表（实测 transform 里看不到），
      // 所以这里自己 add 一份；随后内置的 opencode.config.provider 插件会把配置里的
      // 设置合并回来（同 id 合并，不会出现重复 provider）。
      if (!registry.get(config.providerID)) {
        registry.add({
          info: {
            id: config.providerID,
            name: config.providerName ?? config.providerID,
            package: '@opencode/ai/providers/openai-compatible',
            activation: 'enabled',
            headers: {},
            settings: { baseURL: config.baseURL, apiKey: config.apiKey ?? '' },
          },
          models: [],
        })
        debug('added provider ' + config.providerID)
      }
      for (const model of models) {
        registry.models.update(config.providerID, model.id, (target: any) => {
          const current = target.capabilities ?? {}
          const incoming = model.capabilities ?? {}
          target.modelID = model.id
          target.name = model.name
          target.capabilities = {
            ...current,
            ...incoming,
            tools: incoming.tools ?? current.tools ?? false,
            input: incoming.input ?? current.input ?? DEFAULT_MODALITIES,
            output: incoming.output ?? current.output ?? DEFAULT_MODALITIES,
          }
          if (model.limit) target.limit = { ...(target.limit ?? {}), ...model.limit }
        })
      }
    })
  },
}