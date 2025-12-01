export interface OpenAIModel {
  id: string
  object: string
  created: number
  owned_by: string
}

export interface OpenAIModelsResponse {
  object: string
  data: OpenAIModel[]
}

export interface LiteLLMModelInfo {
  key?: string | null
  mode?: string | null
  max_tokens?: number | null
  max_input_tokens?: number | null
  max_output_tokens?: number | null
  supports_reasoning?: boolean | null
  supports_none_reasoning_effort?: boolean | null
  supports_minimal_reasoning_effort?: boolean | null
  supports_low_reasoning_effort?: boolean | null
  supports_xhigh_reasoning_effort?: boolean | null
  supports_max_reasoning_effort?: boolean | null
  supported_openai_params?: string[] | null
  // Capability flags used to populate OpenCode model fields such as
  // `attachment`, `tool_call`, and `modalities`. Mirrors LiteLLM's
  // model_info fields so an OpenAI-compatible gateway can advertise them.
  supports_vision?: boolean | null
  supports_function_calling?: boolean | null
  supports_tool_choice?: boolean | null
  supports_parallel_function_calling?: boolean | null
  // Optional explicit modality lists. When present these take precedence over
  // the boolean capability flags above.
  input_modalities?: string[] | null
  output_modalities?: string[] | null
  supported_modalities?: string[] | null
}

export interface LiteLLMModelInfoEntry {
  model_name: string
  litellm_params?: {
    model?: string
  }
  model_info?: LiteLLMModelInfo
}

export interface LiteLLMModelInfoResponse {
  data: LiteLLMModelInfoEntry[]
}

export type ModelType = 'chat' | 'embedding' | 'unknown'

export type LoadingStatus = 'not_loaded' | 'loading' | 'loaded' | 'error'

export interface ModelLoadingState {
  status: LoadingStatus
  startTime?: number
  progress?: number
  eta?: number
  error?: string
}

export type LMStudioModel = OpenAIModel
export type LMStudioModelsResponse = OpenAIModelsResponse
