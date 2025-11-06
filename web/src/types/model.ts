/** 模型信息（对应 /v1/models 响应中的模型条目） */
export interface ModelInfo {
  /** 模型唯一标识 */
  id: string
  /** 对象类型，固定为 "model" */
  object: string
  /** 创建时间戳 */
  created: number
  /** 归属渠道/所有者 */
  owned_by: string
  /** 模型类型：chat 或 embedding */
  type?: 'chat' | 'embedding'
}

/** /v1/models 接口响应 */
export interface ModelsResponse {
  object: string
  data: ModelInfo[]
}

/** 模型注册记录（control-plane 管理用） */
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
  /** 关联的渠道信息 */
  provider?: {
    id: string
    name: string
    definition_id: string
    enabled: boolean
  }
  /** 关联的 model_catalog.id；null 表示纯手工注册、不跟随目录 */
  catalog_id?: string | null
  /** context_length 是否已被人工覆盖（true → 不再随目录自动更新） */
  ctx_overridden?: boolean
  /** max_output_tokens 是否已被人工覆盖 */
  max_output_overridden?: boolean
  /** 嵌套展开的 catalog 快照（GET /models 时由后端 join 返回） */
  catalog?: import('./catalog').ModelCatalog | null
}

/** 创建/编辑模型的请求体 */
export interface ModelPayload {
  name: string
  provider_id: string
  model_type: string
  upstream_model_id?: string
  description?: string
  context_length?: number
  max_output_tokens?: number
  input_price?: number
  output_price?: number
  weight?: number
}
