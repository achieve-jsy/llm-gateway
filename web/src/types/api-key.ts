export interface BoundGroup {
  id: number
  name: string
  enabled: boolean
}

export interface ApiKey {
  id: string
  name: string
  masked_key: string
  key_hint: string
  enabled: boolean
  access_all_groups: boolean
  bound_groups: BoundGroup[]
  created_at: string
  updated_at: string
  last_used_at?: string
}

export interface CreateKeyPayload {
  name: string
  access_all_groups?: boolean
  group_ids?: number[]
}

// 后端真实响应: { data: { items, page, page_size, total } }
export interface ListKeysResponse {
  data: {
    items: ApiKey[]
    page: number
    page_size: number
    total: number
  }
}

// 创建 key 响应: { data: { id, name, key, key_hint, ... } }
export interface CreateKeyWrappedResponse {
  data: {
    id: string
    name: string
    key: string          // plain key，仅创建时返回
    key_hint: string
    enabled: boolean
    created_at: string
    updated_at: string
  }
}
