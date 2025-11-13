import { ToastNotifier } from '../ui/toast-notifier'
import { categorizeModel, formatModelName, extractModelOwner } from '../utils'
import { summarizeProviders, writeDiscoverySnapshot } from '../utils/discovery-report'
import { normalizeBaseURL, discoverModelsFromProvider, discoverModelInfoFromProvider, autoDetectOpenAICompatibleProvider, canDiscoverModels } from '../utils/openai-compatible-api'
import { createModelInfoEnricher, isSupportedModelInfoFormat, type ModelInfoEnricher } from '../utils/model-info'
import { getProviderFilter, getDiscoveryConfig, getModelRegexFilter, getProviderModelRegexFilter, shouldDiscoverModel, shouldDiscoverProviderWithOverride } from '../types/plugin-config'
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
      let displayName = providerName

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
        if (!existingModels[modelKey]) {
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
        p.models = {
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

      // 写快照 + 启动摘要 toast，方便确认能力是否被正确加载（不写 opencode.json）。
      await writeDiscoverySnapshot(openAICompatibleProviders, summary, logger)
      toastNotifier
        .info(
          `发现 ${summary.models} 个模型 · 图像输入 ${summary.vision} · 工具调用 ${summary.toolCall} · 推理 ${summary.reasoning}`,
          'LLM Gateway 模型发现',
        )
        .catch(() => {})
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
