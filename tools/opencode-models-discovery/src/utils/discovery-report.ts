import { homedir } from 'node:os'
import { join, dirname } from 'node:path'
import { writeFile, mkdir, readFile } from 'node:fs/promises'
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

export interface PreviousSnapshot {
  pluginVersion?: string
  providers?: Record<string, { models?: Record<string, any> }>
}

export async function readPreviousSnapshot(): Promise<PreviousSnapshot | null> {
  try {
    const content = await readFile(SNAPSHOT_PATH, 'utf8')
    return JSON.parse(content)
  } catch {
    return null
  }
}

export function findNewModels(
  currentProviders: DiscoveredProviderReport[],
  previousSnapshot: PreviousSnapshot | null
): string[] {
  if (!previousSnapshot?.providers) {
    return []
  }

  const newModels: string[] = []
  const previousModelIds = new Set<string>()

  for (const provData of Object.values(previousSnapshot.providers)) {
    if (provData.models) {
      for (const modelId of Object.keys(provData.models)) {
        previousModelIds.add(modelId)
      }
    }
  }

  for (const provider of currentProviders) {
    for (const modelId of Object.keys(provider.models)) {
      if (!previousModelIds.has(modelId)) {
        newModels.push(modelId)
      }
    }
  }

  return newModels
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
