/** 能力配置项 */
export interface CapabilityItem {
  /** 能力标识 */
  id: string
  /** 能力名称 */
  title: string
  /** 能力描述 */
  description: string
  /** 图标名（Element Plus 图标组件名） */
  icon: string
}

/** 排除项（不支持的能力） */
export interface ExcludeItem {
  /** 排除标识 */
  id: string
  /** 说明 */
  reason: string
}

/** 能力配置文件根结构 */
export interface CapabilityConfig {
  /** 页面标题 */
  title: string
  /** 页面描述 */
  description: string
  /** 能力列表 */
  capabilities: CapabilityItem[]
  /** 不支持的能力 */
  excludes: ExcludeItem[]
}
