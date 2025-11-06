export interface ApiKey {
  id: string
  name: string
  maskedKey: string
  keyHint: string
  enabled: boolean
  createdAt: string
  lastUsedAt?: string
  expiresAt?: string
}

export interface RawKeyItem {
  id: string
  name: string
  masked_key: string
  key_hint: string
  enabled: boolean
  created_at: string
  updated_at: string
  last_used_at?: string
  expires_at?: string
}

export function mapKeysResponse(response: ListKeysResponse): ApiKey[] {
  return response.data.items.map((item) => ({
    id: item.id,
    name: item.name,
    maskedKey: item.masked_key,
    keyHint: item.key_hint,
    enabled: item.enabled,
    createdAt: item.created_at,
    lastUsedAt: item.last_used_at,
    expiresAt: item.expires_at,
  }))
}

export interface CreateKeyResponse {
  id: string
  name: string
  key: string
  keyHint: string
}

// Wrapped response from control-plane
export interface ListKeysResponse {
  code: number
  message: string
  data: {
    items: RawKeyItem[]
    total: number
    page: number
    page_size: number
  }
  request_id?: string
}

export interface CreateKeyWrappedResponse {
  code: number
  message: string
  data: {
    id: string
    name: string
    key: string
    key_hint: string
    enabled: boolean
    created_at: string
    updated_at: string
  }
  request_id?: string
}

export function mapCreateKeyResponse(response: CreateKeyWrappedResponse): CreateKeyResponse {
  return {
    id: response.data.id,
    name: response.data.name,
    key: response.data.key,
    keyHint: response.data.key_hint,
  }
}
