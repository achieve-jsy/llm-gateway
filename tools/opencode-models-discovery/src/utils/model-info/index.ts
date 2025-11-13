import { createLiteLLMModelInfoEnricher } from './litellm'
import type { ModelInfoFormat } from '../../types/plugin-config'
import type { ModelInfoEnricher, ModelInfoEnricherOptions } from './types'

type ModelInfoEnricherFactory = (data: unknown, options: ModelInfoEnricherOptions) => ModelInfoEnricher

const MODEL_INFO_ENRICHERS: Record<string, ModelInfoEnricherFactory> = {
  litellm: createLiteLLMModelInfoEnricher,
}

export function createModelInfoEnricher(
  format: ModelInfoFormat,
  data: unknown,
  options: ModelInfoEnricherOptions
): ModelInfoEnricher | undefined {
  return MODEL_INFO_ENRICHERS[format]?.(data, options)
}

export function isSupportedModelInfoFormat(format: ModelInfoFormat): boolean {
  return MODEL_INFO_ENRICHERS[format] !== undefined
}

export type { ModelInfoEnricher, ModelInfoEnricherOptions }
