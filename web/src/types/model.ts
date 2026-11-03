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
/** 模型类型：chat、embedding、embedding_vl、rerank、rerank_vl 或 video */
  type?: 'chat' | 'embedding' | 'embedding_vl' | 'rerank' | 'rerank_vl' | 'video'
  /** 元数据（能力信息，如 modes、context_window 等） */
  metadata?: ModelMetadata
}

/** 模型元数据（来自 /v1/models 响应的 metadata 字段） */
export interface ModelMetadata {
  display_name?: string
  description?: string
  family?: string
  modes?: string[]
  categories?: ModelCategory[]
  tags?: string[]
  context_window?: number
  max_output_tokens?: number
  capabilities?: Record<string, boolean>
}

/** 模型分类 */
export interface ModelCategory {
  id: string
  name: string
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
  cache_read_price?: number | null
  cache_write_price?: number | null
  /** 定价模式：catalog = 实时读取目录价格；manual = 手动指定固定价格 */
  price_mode: 'catalog' | 'manual'
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
  /** 视频模型参数 */
  video_supported_resolutions?: string[]
  video_supported_aspect_ratios?: string[]
  video_min_duration_seconds?: number
  video_max_duration_seconds?: number
  video_supported_actions?: string[]
  /** 参数适配策略：reasoning = 推理模型；standard = 标准透传；'' = 自动检测 */
  param_style?: string
  /** Upstream Planner v2：基础模型名，用于能力与协议推断 */
  base_model?: string
  /** Upstream Planner v2：协议模式 */
  protocol_mode?: 'auto' | 'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_native' | 'bedrock_converse' | 'openai_compatible' | 'passthrough'
  /** Upstream Planner v2：参数画像 */
  param_profile?: 'auto' | 'openai_standard' | 'openai_reasoning_chat' | 'openai_responses' | 'deepseek_reasoning' | 'gemini_thinking' | 'anthropic_thinking' | 'bedrock_converse' | 'passthrough'
  /** Upstream Planner v2：遇到不支持参数时的策略 */
  unsupported_param_policy?: 'preserve' | 'drop' | 'reject'
  /** Upstream Planner v2：单模型参数支持覆盖 */
  supported_params_override?: Record<string, unknown>
  /** Upstream Planner v2：有限参数转换覆盖 */
  transform_overrides?: Record<string, unknown>
}

/** 创建/编辑模型的请求体 */
export interface ModelPayload {
  name: string
  provider_id: string
  model_type: string
  enabled?: boolean
  upstream_model_id?: string
  description?: string
  context_length?: number
  max_output_tokens?: number
  price_mode?: 'catalog' | 'manual'
  input_price?: number
  output_price?: number
  cache_read_price?: number | null
  cache_write_price?: number | null
  weight?: number
  video_supported_resolutions?: string[]
  video_supported_aspect_ratios?: string[]
  video_min_duration_seconds?: number
  video_max_duration_seconds?: number
  video_supported_actions?: string[]
  /** 参数适配策略：reasoning = 推理模型；standard = 标准透传；'' = 自动检测 */
  param_style?: string
  /** Upstream Planner v2：基础模型名，用于能力与协议推断 */
  base_model?: string
  /** Upstream Planner v2：协议模式 */
  protocol_mode?: 'auto' | 'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_native' | 'bedrock_converse' | 'openai_compatible' | 'passthrough'
  /** Upstream Planner v2：参数画像 */
  param_profile?: 'auto' | 'openai_standard' | 'openai_reasoning_chat' | 'openai_responses' | 'deepseek_reasoning' | 'gemini_thinking' | 'anthropic_thinking' | 'bedrock_converse' | 'passthrough'
  /** Upstream Planner v2：遇到不支持参数时的策略 */
  unsupported_param_policy?: 'preserve' | 'drop' | 'reject'
  /** Upstream Planner v2：单模型参数支持覆盖 */
  supported_params_override?: Record<string, unknown>
  /** Upstream Planner v2：有限参数转换覆盖 */
  transform_overrides?: Record<string, unknown>
}
