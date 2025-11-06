import http from './http'
import type { ModelInfo, ModelsResponse } from '@/types/model'

/** 获取模型列表（Gateway API /v1/models） */
export async function fetchModels(): Promise<ModelInfo[]> {
  const response = await http.get<ModelsResponse>('/v1/models')
  return response.data.data
}
