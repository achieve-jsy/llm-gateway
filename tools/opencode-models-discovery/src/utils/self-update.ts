// 插件自更新：每次 opencode 启动加载插件时，向网关查询服务端插件版本，
// 若与本地（打进 bundle 的 PLUGIN_VERSION）不同，则下载最新 bundle 覆盖本地
// 插件文件，并提示用户重启 opencode 生效。
//
// 设计要点：
//   - 网关 origin 取自配置里「开启了 modelsDiscovery」的 provider 的 baseURL，
//     版本/下载端点经 nginx 与网关同 origin（/api/...）。
//   - 全程带超时、静默失败，绝不阻塞 opencode 启动与模型发现。
//   - 每个进程只跑一次。
import { homedir } from 'node:os'
import { join, dirname } from 'node:path'
import { writeFile, mkdir, rm } from 'node:fs/promises'
import { PLUGIN_VERSION } from '../version'
import type { ToastNotifier } from '../ui/toast-notifier'
import type { PluginLogger } from '../plugin/logger'

let selfUpdateStarted = false

const VERSION_PATH = '/api/v1/opencode-config/plugin/version'
const PLUGIN_PATH = '/api/v1/opencode-config/plugin'

function originFromBaseURL(baseURL: string): string | null {
  try {
    const u = new URL(baseURL)
    if (!u.protocol || !u.host) return null
    return `${u.protocol}//${u.host}`
  } catch {
    return null
  }
}

function pickGatewayOrigin(config: any): string | null {
  const providers = config?.provider
  if (!providers || typeof providers !== 'object') return null
  for (const value of Object.values(providers)) {
    const p = value as any
    if (p?.options?.modelsDiscovery?.enabled === true && typeof p?.options?.baseURL === 'string') {
      const origin = originFromBaseURL(p.options.baseURL)
      if (origin) return origin
    }
  }
  return null
}

async function fetchText(url: string, timeoutMs: number): Promise<string | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const resp = await fetch(url, { signal: controller.signal })
    if (!resp.ok) return null
    return await resp.text()
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

// 自更新主流程，静默失败。
export async function maybeSelfUpdate(
  config: any,
  toastNotifier: ToastNotifier,
  logger: PluginLogger,
): Promise<void> {
  if (selfUpdateStarted) return
  selfUpdateStarted = true

  const origin = pickGatewayOrigin(config)
  if (!origin) {
    logger.debug('self-update skipped: no discovery-enabled provider with baseURL')
    return
  }

  const remoteRaw = await fetchText(origin + VERSION_PATH, 3000)
  if (!remoteRaw) {
    logger.debug('self-update skipped: version endpoint unavailable', { origin })
    return
  }
  const remote = remoteRaw.trim()
  if (!remote || remote === PLUGIN_VERSION) {
    logger.debug('self-update: already up to date', { version: PLUGIN_VERSION })
    return
  }

  logger.info('self-update: newer plugin version found', { local: PLUGIN_VERSION, remote })

  const code = await fetchText(origin + PLUGIN_PATH, 8000)
  if (!code || code.length < 1024) {
    logger.warn('self-update: failed to download new bundle', { origin })
    return
  }

  // opencode 标准插件目录为复数 plugins/。
  const target = join(homedir(), '.config', 'opencode', 'plugins', 'opencode-models-discovery.js')
  const legacy = join(homedir(), '.config', 'opencode', 'plugin', 'opencode-models-discovery.js')
  try {
    await mkdir(dirname(target), { recursive: true })
    await writeFile(target, code, 'utf8')
    await rm(legacy, { force: true }).catch(() => {})
  } catch (error) {
    logger.warn('self-update: failed to write plugin file', {
      target,
      error: error instanceof Error ? error.message : String(error),
    })
    return
  }

  logger.info('self-update: plugin updated, restart required', { from: PLUGIN_VERSION, to: remote, target })
  toastNotifier
    .info(`模型发现插件已更新 ${PLUGIN_VERSION} → ${remote}，重启 opencode 生效`, 'LLM Gateway', 10000)
    .catch(() => {})
}
