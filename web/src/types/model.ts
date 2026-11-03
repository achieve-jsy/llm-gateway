export interface ModelInfo {
  id: string
  object: string
  created: number
  owned_by: string
  type?: 'chat' | 'embedding'
}

export interface ModelsResponse {
  object: string
  data: ModelInfo[]
}

export interface Model {
  id: string
  name: string
  provider_id: string
  model_type: string
  enabled: boolean
  upstream_model_id: string
  description: string
  context_length: number
  max_output_tokens: number
  input_price: number
  output_price: number
  weight: number
  consecutive_failures: number
  last_check_error: string
  response_time: number
  created_at: string
  updated_at: string
  provider?: {
    id: string
    name: string
    definition_id: string
    enabled: boolean
  }
  catalog_id?: string | null
  ctx_overridden?: boolean
  max_output_overridden?: boolean
  catalog?: import('./catalog').ModelCatalog | null
}

export interface ModelPayload {
  name: string
  provider_id: string
  model_type: string
  enabled?: boolean
  upstream_model_id?: string
  description?: string
  context_length?: number
  max_output_tokens?: number
  input_price?: number
  output_price?: number
  weight?: number
}
