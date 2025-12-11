import { ToastNotifier } from '../ui/toast-notifier'
import { categorizeModel, formatModelName, extractModelOwner } from '../utils'
import { summarizeProviders, writeDiscoverySnapshot, readPreviousSnapshot, findNewModels } from '../utils/discovery-report'
import { normalizeBaseURL, discoverModelsFromProvider, discoverModelInfoFromProvider, autoDetectOpenAICompatibleProvider, canDiscoverModels } from '../utils/openai-compatible-api'
import { createModelInfoEnricher, isSupportedModelInfoFormat, type ModelInfoEnricher } from '../utils/model-info'
import { getProviderFilter, getDiscoveryConfig, getModelRegexFilter, getProviderModelRegexFilter, shouldDiscoverModel, shouldDiscoverProviderWithOverride } from '../types/plugin-config'
import { PLUGIN_VERSION } from '../version'
import type { PluginLogger } from './logger'
import type { PluginInput } from '@opencode-ai/plugin'
import type { OpenAIModel } from '../types'
import type { PluginConfig } from '../types/plugin-config'

interface DiscoveredProvider {
  name: string
  baseURL: string
  models: Record<string, any>
}

export async function enhanceConfig(
  config: any,
  client: PluginInput['client'],
  toastNotifier: ToastNotifier,
  pluginConfig: PluginConfig,
  logger: PluginLogger
): Promise<void> {
  try {
    const providers = config.provider || {}
    const openAICompatibleProviders: DiscoveredProvider[] = []
    const providerFilter = getProviderFilter(pluginConfig)
    const modelRegexFilter = getModelRegexFilter(pluginConfig, logger.child({ category: 'filtering' }))
    const discoveryConfig = getDiscoveryConfig(pluginConfig)
    const globalDiscoveryEnabled = discoveryConfig.enabled

    for (const [providerName, providerConfig] of Object.entries(providers)) {
      const p = providerConfig as any
      const providerDiscoveryConfig = p.options?.modelsDiscovery ?? {}
      const modelsEndpoint = providerDiscoveryConfig.endpoint ?? '/v1/models'
      const modelInfoEndpoint = providerDiscoveryConfig.modelInfoEndpoint
      const modelInfoFormat = providerDiscoveryConfig.modelInfoFormat
      const filterNonChat = providerDiscoveryConfig.filterNonChat !== false
      const forceDiscoveryEnabled = providerDiscoveryConfig.enabled === true

      if (!forceDiscoveryEnabled && !canDiscoverModels(p)) {
        continue
      }

      if (!shouldDiscoverProviderWithOverride(providerName, providerFilter, globalDiscoveryEnabled, providerDiscoveryConfig)) {
        logger.debug(`Provider ${providerName} model discovery disabled by configuration`)
        continue
      }

      let baseURL: string
      // 优先用 provider 配置里的 name（即网关下发的供应商名称，如"某公司"），
      // 没有时回退到 provider 的 key 名。
      let displayName = (typeof p.name === 'string' && p.name.trim()) ? p.name.trim() : providerName

      if (p.options?.baseURL) {
        baseURL = normalizeBaseURL(p.options.baseURL)
      } else {
        continue
      }

      const apiKey = p.options?.apiKey

      let models: OpenAIModel[]
      const discovery = await discoverModelsFromProvider(baseURL, apiKey, modelsEndpoint)
      if (!discovery.ok) {
        logger.warn('Provider model discovery failed', {
          provider: providerName,
          baseURL,
          endpoint: modelsEndpoint,
        })
        continue
      }

      models = discovery.models

      if (models.length === 0) {
        continue
      }

      let modelInfoEnricher: ModelInfoEnricher | undefined
      if (modelInfoFormat && !isSupportedModelInfoFormat(modelInfoFormat)) {
        logger.warn('Unsupported provider model info format', {
          provider: providerName,
          format: modelInfoFormat,
        })
      } else if (typeof modelInfoEndpoint === 'string' && modelInfoEndpoint.length > 0 && modelInfoFormat) {
        const modelInfoDiscovery = await discoverModelInfoFromProvider(baseURL, apiKey, modelInfoEndpoint)
        if (modelInfoDiscovery.ok) {
          modelInfoEnricher = createModelInfoEnricher(modelInfoFormat, modelInfoDiscovery.data, { filterNonChat })
        } else {
          logger.warn('Provider model info discovery failed', {
            provider: providerName,
            baseURL,
            endpoint: modelInfoEndpoint,
            format: modelInfoFormat,
          })
        }
      }

      const existingModels = p.models || {}
      const discoveredModels: Record<string, any> = {}
      let chatModelsCount = 0

      const hasProviderModelRegexFilter = !!providerDiscoveryConfig.models?.includeRegex?.length || !!providerDiscoveryConfig.models?.excludeRegex?.length
      const providerModelRegexFilter = getProviderModelRegexFilter(providerDiscoveryConfig, logger.child({ category: 'filtering' }))
      let smartModelNameEnabled = providerDiscoveryConfig.smartModelName
      if (smartModelNameEnabled === undefined) {
        smartModelNameEnabled = pluginConfig.smartModelName
      }

      for (const model of models) {
        const modelKey = model.id
        // 对显式开启发现的 provider（即网关下发、modelsDiscovery.enabled=true 的渠道），
        // 即使配置里已存在同名模型（通常是旧安装脚本静态写入、且缺少能力信息）也要
        // 重新发现并富化，保证每次启动都动态加载模型、注入能力并弹出发现 toast；
        // 其它 provider 仍沿用「跳过已存在模型」的保守行为，避免覆盖用户自定义。
        if (forceDiscoveryEnabled || !existingModels[modelKey]) {
          const activeModelRegexFilter = hasProviderModelRegexFilter ? providerModelRegexFilter : modelRegexFilter
          if (!shouldDiscoverModel(model.id, activeModelRegexFilter)) {
            continue
          }

          if (modelInfoEnricher?.shouldSkipModel(model.id)) {
            continue
          }

          const modelType = categorizeModel(model.id)
          if (modelType === 'embedding') {
            continue
          }

          const owner = extractModelOwner(model.id)
          const modelConfig: any = {
            id: model.id,
            name: smartModelNameEnabled ? formatModelName(model) : model.id,
          }

          if (owner) {
            modelConfig.organizationOwner = owner
          }

          if (modelType === 'chat') {
            chatModelsCount++
          }

          // Capabilities (modalities, attachment, tool_call, reasoning, limits)
          // are derived from the provider's model info when available so we
          // advertise real capabilities instead of assuming every chat model
          // accepts image input.
          modelInfoEnricher?.applyModelInfo(modelConfig, model.id)

          // Fallback when no model info is available: declare a conservative
          // text-only modality set for chat models rather than guessing image
          // support.
          if (!modelConfig.modalities && modelType === 'chat') {
            modelConfig.modalities = {
              input: ["text"],
              output: ["text"],
            }
          }

          discoveredModels[modelKey] = modelConfig
        }
      }

      if (Object.keys(discoveredModels).length > 0) {
        // 显式开启发现时用新发现结果整体替换 models，丢弃旧的静态写入条目
        //（避免残留缺能力信息的旧模型）；其它 provider 与既有模型合并。
        p.models = forceDiscoveryEnabled
          ? discoveredModels
          : {
              ...existingModels,
              ...discoveredModels,
            }

        openAICompatibleProviders.push({
          name: displayName,
          baseURL,
          models: discoveredModels
        })
      }
    }

    if (openAICompatibleProviders.length > 0) {
      const summary = summarizeProviders(openAICompatibleProviders)
      logger.info('Provider model discovery completed', {
        providerCount: openAICompatibleProviders.length,
        modelCount: summary.models,
        vision: summary.vision,
        toolCall: summary.toolCall,
        reasoning: summary.reasoning,
      })

      const previousSnapshot = await readPreviousSnapshot()
      const newModels = findNewModels(openAICompatibleProviders, previousSnapshot)

      await writeDiscoverySnapshot(openAICompatibleProviders, summary, logger)
      
      setTimeout(() => {
        for (const prov of openAICompatibleProviders) {
          const provSummary = summarizeProviders([prov])
          
          let message = `发现 ${provSummary.models} 个模型`
          if (provSummary.vision > 0) message += ` · 图像 ${provSummary.vision}`
          if (provSummary.toolCall > 0) message += ` · 工具 ${provSummary.toolCall}`
          if (provSummary.reasoning > 0) message += ` · 推理 ${provSummary.reasoning}`

          toastNotifier
            .info(message, `${prov.name} · v${PLUGIN_VERSION}`, 10000)
            .catch(() => {})
        }

        if (newModels.length > 0) {
          const modelList = newModels.length <= 5
            ? newModels.join(', ')
            : `${newModels.slice(0, 5).join(', ')} 等 ${newModels.length} 个`
          toastNotifier
            .success(`网关上新: ${modelList}`, '新模型', 15000)
            .catch(() => {})
        }
      }, 5000)
    }

    if (Object.keys(providers).length === 0) {
      const detected = await autoDetectOpenAICompatibleProvider()
      if (detected) {
        logger.info('Detected OpenAI-compatible provider but found no configured providers', {
          provider: detected.name,
          baseURL: detected.baseURL,
        })
      }
    }

  } catch (error) {
    logger.error('Unexpected error in enhanceConfig', {
      error: error instanceof Error ? error.message : String(error),
    })
    toastNotifier.warning("Plugin configuration failed", "Configuration Error").catch(() => { })
  }
}
