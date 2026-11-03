import http from './http'
import axios from 'axios'
import type { ModelInfo, ModelsResponse } from '@/types/model'

// ===================== 模型 =====================

/** 获取模型列表（Gateway API /v1/models） */
export async function fetchModels(): Promise<ModelInfo[]> {
  const response = await http.get<ModelsResponse>('/v1/models')
  return response.data.data
}

/** 用指定 API Key 获取该 Key 有权限的模型列表 */
export async function fetchModelsByApiKey(baseURL: string, apiKey: string): Promise<ModelInfo[]> {
  try {
    const url = baseURL.replace(/\/+$/, '') + '/models'
    const response = await axios.get<ModelsResponse>(url, {
      headers: { Authorization: `Bearer ${apiKey}` },
      timeout: 10000,
    })
    return response.data.data ?? []
  } catch {
    return []
  }
}

// ===================== Me Models =====================

export interface MeModelItem {
  id: string
  object: string
  owned_by: string
  type: string
  enabled: boolean
  consecutive_failures: number
  last_check_error: string
  response_time: number
}

export interface MeModelsResponse {
  object: string
  data: MeModelItem[]
}

/** 获取当前用户的模型列表（Gateway API /v1/me/models） */
export async function fetchMyModels(): Promise<MeModelItem[]> {
  const response = await http.get<MeModelsResponse>('/v1/me/models')
  return response.data.data
}
