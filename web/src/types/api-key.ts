export interface ApiKey {
  id: string
  name: string
  maskedKey: string
  keyHint: string
  enabled: boolean
  logEnabled: boolean
  accessAllGroups?: boolean
  createdAt: string
  lastUsedAt?: string
  expiresAt?: string
  boundGroups?: BoundGroupInfo[]
}

export interface BoundGroupInfo {
  id: number
  name: string
}

export interface AvailableGroup {
  id: number
  name: string
  description: string
  strategy: string
  budget_tier_id?: string
  budget_tier_name?: string
}

export interface RawKeyItem {
  id: string
  name: string
  masked_key: string
  key_hint: string
  enabled: boolean
  log_enabled: boolean
  access_all_groups?: boolean
  created_at: string
  updated_at: string
  last_used_at?: string
  expires_at?: string
  bound_groups?: RawBoundGroup[]
}

export interface RawBoundGroup {
  id: number
  name: string
}

export function mapKeysResponse(response: ListKeysResponse): ApiKey[] {
  return response.data.items.map((item) => ({
    id: item.id,
    name: item.name,
    maskedKey: item.masked_key,
    keyHint: item.key_hint,
    enabled: item.enabled,
    logEnabled: item.log_enabled ?? true,
    accessAllGroups: item.access_all_groups || false,
    createdAt: item.created_at,
    lastUsedAt: item.last_used_at,
    expiresAt: item.expires_at,
    boundGroups: (item.bound_groups || []).map((g) => ({ id: g.id, name: g.name })),
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
    bound_groups?: RawBoundGroup[]
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

export interface AvailableGroupsResponse {
  data: {
    groups: AvailableGroup[]
  }
}
