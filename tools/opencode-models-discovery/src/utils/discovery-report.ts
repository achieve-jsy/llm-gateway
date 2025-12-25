// 发现结果可见性：把每次发现到的模型及其解析出的能力写一份快照文件，并返回
// 一句话摘要用于启动 toast。这样用户无需翻 opencode.json（发现的模型只在内存、
// 不落 opencode.json）即可确认多模态/工具调用等能力是否被正确加载。
//
// 快照文件：~/.config/opencode/plugin/models-discovery.snapshot.json
// 该文件仅供查看，opencode 不读取它，因此既不影响加载也不影响自动刷新。
import { homedir } from 'node:os'
import { join, dirname } from 'node:path'
import { writeFile, mkdir } from 'node:fs/promises'
import { PLUGIN_VERSION } from '../version'
import type { PluginLogger } from '../plugin/logger'

export interface DiscoveredProviderReport {
  name: string
  baseURL: string
  models: Record<string, any>
}

export interface DiscoverySummary {
  models: number
  vision: number
  toolCall: number
  reasoning: number
}

const SNAPSHOT_PATH = join(homedir(), '.config', 'opencode', 'plugins', 'models-discovery.snapshot.json')

function modelHasVision(model: any): boolean {
  if (model?.attachment === true) return true
  const input = model?.modalities?.input
  return Array.isArray(input) && input.includes('image')
}

export function summarizeProviders(providers: DiscoveredProviderReport[]): DiscoverySummary {
  const summary: DiscoverySummary = { models: 0, vision: 0, toolCall: 0, reasoning: 0 }
  for (const provider of providers) {
    for (const model of Object.values(provider.models)) {
      summary.models++
      if (modelHasVision(model)) summary.vision++
      if ((model as any)?.tool_call === true) summary.toolCall++
      if ((model as any)?.reasoning === true) summary.reasoning++
    }
  }
  return summary
}

// 写发现快照（静默失败，不影响主流程）。返回快照文件路径，失败返回 null。
export async function writeDiscoverySnapshot(
  providers: DiscoveredProviderReport[],
  summary: DiscoverySummary,
  logger: PluginLogger,
): Promise<string | null> {
  const snapshot = {
    generatedAt: new Date().toISOString(),
    pluginVersion: PLUGIN_VERSION,
    summary,
    providers: providers.reduce<Record<string, any>>((acc, provider) => {
      acc[provider.name] = {
        baseURL: provider.baseURL,
        modelCount: Object.keys(provider.models).length,
        models: Object.fromEntries(
          Object.entries(provider.models).map(([id, model]) => [
            id,
            {
              name: (model as any)?.name,
              modalities: (model as any)?.modalities,
              attachment: (model as any)?.attachment === true,
              tool_call: (model as any)?.tool_call === true,
              reasoning: (model as any)?.reasoning === true,
              limit: (model as any)?.limit,
            },
          ]),
        ),
      }
      return acc
    }, {}),
  }

  try {
    await mkdir(dirname(SNAPSHOT_PATH), { recursive: true })
    await writeFile(SNAPSHOT_PATH, JSON.stringify(snapshot, null, 2) + '\n', 'utf8')
    return SNAPSHOT_PATH
  } catch (error) {
    logger.warn('failed to write discovery snapshot', {
      path: SNAPSHOT_PATH,
      error: error instanceof Error ? error.message : String(error),
    })
    return null
  }
}
