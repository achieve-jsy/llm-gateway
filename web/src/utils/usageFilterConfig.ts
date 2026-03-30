/** 用量三页筛选：按页面角色 + 能力 Tab 控制字段显隐与状态枚举 */

export type UsagePageRole = 'mine' | 'dashboard' | 'cost'
export type UsageCapability = 'chat' | 'embedding' | 'rerank' | 'video'

export interface StatusOption {
  label: string
  value: string
}

export interface UsageFilterFields {
  /** 常驻 */
  group: boolean
  model: boolean
  keySelect: boolean
  keyName: boolean
  status: boolean
  provider: boolean
  /** 更多 */
  moreModel: boolean
  moreProvider: boolean
  moreStatus: boolean
  user: boolean
  keyType: boolean
  dept: boolean
  studio: boolean
}

const HTTP_STATUS: StatusOption[] = [
  { label: '成功', value: 'success' },
  { label: '失败', value: 'error' },
]

const VIDEO_STATUS: StatusOption[] = [
  { label: '排队中', value: 'submitted' },
  { label: '生成中', value: 'running' },
  { label: '成功', value: 'succeeded' },
  { label: '失败', value: 'failed' },
]

export function statusOptionsFor(capability: UsageCapability): StatusOption[] {
  return capability === 'video' ? VIDEO_STATUS : HTTP_STATUS
}

export function isStatusCompatible(capability: UsageCapability, status: string): boolean {
  if (!status) return true
  return statusOptionsFor(capability).some(o => o.value === status)
}

/** 字段矩阵：常用维度常驻一行；仅组织/低频项进「更多」 */
export function filterFieldsFor(
  role: UsagePageRole,
  capability: UsageCapability,
): UsageFilterFields {
  const isVideo = capability === 'video'
  const compact = capability === 'embedding' || capability === 'rerank'

  if (role === 'mine') {
    // 个人页字段少：全部常驻，避免「更多」拆出空荡第一行
    return {
      group: true,
      model: true,
      keySelect: true,
      keyName: false,
      status: true,
      provider: isVideo,
      moreModel: false,
      moreProvider: false,
      moreStatus: false,
      user: false,
      keyType: false,
      dept: false,
      studio: false,
    }
  }

  if (role === 'dashboard') {
    // 看板：模型/状态/用户/Key 类型常驻；Video 再加渠道
    return {
      group: false,
      model: true,
      keySelect: false,
      keyName: false,
      status: true,
      provider: isVideo,
      moreModel: false,
      moreProvider: false,
      moreStatus: false,
      user: !compact,
      keyType: !compact,
      dept: false,
      studio: false,
    }
  }

  // cost：组/模型/渠道/用户/Key 类型常驻；Key 名、状态、组织进更多
  return {
    group: true,
    model: true,
    keySelect: false,
    keyName: true,
    status: false,
    provider: true,
    moreModel: false,
    moreProvider: false,
    moreStatus: true,
    user: true,
    keyType: true,
    dept: true,
    studio: true,
  }
}

export function countActiveFilters(
  role: UsagePageRole,
  capability: UsageCapability,
  values: Record<string, string | undefined>,
): number {
  const f = filterFieldsFor(role, capability)
  const keys: string[] = []
  if (f.group || false) keys.push('groupName')
  if (f.model || f.moreModel) keys.push('modelName')
  if (f.keySelect) keys.push('keyId')
  if (f.keyName) keys.push('keyName')
  if (f.status || f.moreStatus) keys.push('status')
  if (f.provider || f.moreProvider) keys.push('provider')
  if (f.user) keys.push('userName')
  if (f.keyType) keys.push('keyType')
  if (f.dept) keys.push('deptName')
  if (f.studio) keys.push('studioId')
  return keys.filter(k => !!values[k]).length
}

/** Video 列表/统计：组名优先写入 model_name（落库字段），否则用模型名 */
export function videoModelNameParam(groupName?: string, modelName?: string): string | undefined {
  const g = (groupName || '').trim()
  const m = (modelName || '').trim()
  return g || m || undefined
}
