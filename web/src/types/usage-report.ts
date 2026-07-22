// Token 使用分析报告 JSON 契约（对齐后端 internal/observability/report 包，schema_version 2.0）。
// 页面 / Markdown / HTML 三份报告同源，只消费本目录类型。字段名 = 后端 json tag（snake_case）。
//
// 铁律（配合后端 report.Validate()）：
//   - Σ UsageIssues[].attributed_cost_usd <= 对应载体的总成本（个人层 <= cost_breakdown 三分法总成本，
//     成员层 <= member.cost_usd），防止费用桶归因重复计入，四象限 Y 轴不失真。
//   - issue_cost_ratio 只聚合五种情况归因；3.6 unrelated_cost 只走 safety_anomaly 布尔描边，绝不相加。
//   - 3.6 严重异常永远只产出 pending_review（待核查），前端绝不渲染「违规/违纪」等定责措辞。

/** 报告层级。 */
export type ReportLevel = 'personal' | 'department' | 'studio'

/** 五种情况类型（§3.1-§3.5）。 */
export type IssueType =
  | 'prefix_pollution' // §3.1 前缀污染（上下文）
  | 'toothpaste' // §3.2 挤牙膏（交互）
  | 'multi_task_session' // §3.3 多任务不切会话（交互）
  | 'output_runaway' // §3.4 输出失控（生成）
  | 'over_model' // §3.5 杀鸡用牛刀（选型）

/** DominantIssue：五种情况类型之一，或 "normal"（全部命中 < 阈值时的兜底）。 */
export type DominantIssue = IssueType | 'normal'

/** 情况判定结论。 */
export type Verdict = 'confirmed' | 'suspected' | 'not_hit' | 'not_computed'

/** 四象限（§6/§4.1）。 */
export type Quadrant = 'priority_improve' | 'coach' | 'routine' | 'high_cost_normal'

/** 严重异常待核查状态机（§4/§8.3 红线：永远不自动认定违纪）。 */
export type SafetyAnomalyStatus =
  | 'pending_review'
  | 'confirmed_ok'
  | 'need_check'
  | 'false_positive'

/** 成本三分法计费桶（用于 Conclusion.cost_main_bucket）。 */
export type CostBucket = 'uncached_input' | 'cache' | 'output'

/** 报告统计周期类型（与后端 period_type 参数对齐）。 */
export type PeriodType = 'week' | 'month' | 'day'

/** Report 统一 JSON 契约根节点（页面/Markdown/HTML 同源）。 */
export interface Report {
  schema_version: string
  meta: Meta
  data_quality: DataQuality
  cost_breakdown: CostBreakdown
  safety_anomaly: SafetyAnomaly

  /** 仅 level=personal 使用；department/studio 走 member_evaluation.members[].usage_issues。 */
  usage_issues?: UsageIssue[]

  /** 仅 level=department|studio 使用。 */
  member_evaluation?: MemberEvaluation

  conclusion: Conclusion
}

/** 报告元信息（报告头）。 */
export interface Meta {
  level: ReportLevel
  scope: Scope
  period: Period
  currency: Currency
  rule_version: string // 如 "v0.6"
}

/** 报告范围（personal: user_id；department: dept_id + 父工作室；studio: studio_id）。 */
export interface Scope {
  id: string
  name: string
  studio_id?: string
  studio_name?: string
}

/** 统计周期（周报默认，支持自定义区间）。 */
export interface Period {
  from: string // "2006-01-02"
  to: string // "2006-01-02"，闭区间
  iso_week?: string // 如 "2026-W33"（按 from 归属周）
}

/** 报告币种与汇率（est_cost 内部为 USD，报告展示按报告币种换算）。 */
export interface Currency {
  report: string // CNY | USD
  usd_cny_rate: number
}

/** 数据质量覆盖率（报告头必带）。 */
export interface DataQuality {
  real_session_coverage: number
  scene_label_coverage: number
  price_catalog_coverage: number
  position_label_coverage: number
  failed_cost_coverage: number
}

/** 成本三分法（§2.2）：非缓存输入 + 缓存 + 输出。 */
export interface CostBreakdown {
  uncached_input_cost_usd: number
  cache_cost_usd: number
  cache_write_included: boolean
  output_cost_usd: number

  /** 无效消耗（失败请求，已计入上面三块，仅打标注，不新增计费块）。 */
  failed_request_cost_usd: number

  prev?: CostBreakdownPrev
  by_model_tier?: ModelTierCost[]
}

/** 上一周期的成本三分法（本周 vs 上周堆叠条对比）。 */
export interface CostBreakdownPrev {
  uncached_input_cost_usd: number
  cache_cost_usd: number
  output_cost_usd: number
}

/** 「成本构成 × 模型档位」切片（§7）。 */
export interface ModelTierCost {
  model: string
  tier: string // 高阶/中/轻量，来自 model_catalog + 组等级
  cost_usd: number
  uncached_input_cost_usd: number
  cache_cost_usd: number
  output_cost_usd: number
}

/** 3.6 严重异常（§4，独立置顶块，永远待核查，不认定违纪）。 */
export interface SafetyAnomaly {
  hit: boolean
  unrelated_cost_ratio?: number
  unrelated_cost_usd?: number
  top_hours?: string[]
  top_model?: string
  top_key?: string
  basis?: string
  excluded?: string[]
	/** 管理报告展示标签，例如“疑似加班未备案”。 */
	review_tag?: string
	/** 面向管理者的核查提示。 */
	hint?: string
	priority?: 'low' | 'medium' | 'high'
	content_signal?: 'work_related' | 'not_work_related' | 'uncertain' | string
	content_confidence?: number

  /** 状态机：pending_review | confirmed_ok | need_check | false_positive。 */
  status?: SafetyAnomalyStatus
}

/** 五种情况（§3）统一判定结构。个人层在 Report.usage_issues，成员层在 member_evaluation.members[].usage_issues。 */
export interface UsageIssue {
  type: IssueType
  verdict: Verdict // confirmed|suspected|not_hit|not_computed

  request_count: number
  session_count: number
  request_share: number
  session_share: number

  /** 进合计、进 Y 轴（费用桶归因清算后的主因费用，§5/§3.6）。 */
  attributed_cost_usd: number
  /** 共同命中、不重复计。 */
  overlap_cost_usd: number
  /** 涉及请求的整笔费用，仅展示影响面，不进 Y 轴。 */
  affected_cost_usd: number

  confidence: number
  evidence?: Record<string, unknown>
  excluded?: string[]

  suggestion_key?: string
  narrative?: {
    finding: string
    reason: string
    impact: string
    advice: string
    semantic_review?: string
  }
}

/** 成员整合评估（§6/§7，仅 department|studio 使用）。 */
export interface MemberEvaluation {
  cost_multiple_baseline: string // 目前固定 "median"
  cost_median_usd: number
  issue_ratio_median: number // Y 轴十字线

  members: MemberPoint[]
  clusters?: MemberCluster[]
}

/** 四象限散点的单个成员评估结果。 */
export interface MemberPoint {
  user_id: string
  name: string
  position: string
  dept_id?: string
  dept_name?: string

  cost_usd: number
  cost_multiple: number // cost_usd / median(members.cost_usd)

  issue_attributed_cost_usd: number
  issue_cost_ratio: number

  quadrant: Quadrant // priority_improve|coach|routine|high_cost_normal
  dominant_issue: DominantIssue
  dominant_issue_cost_usd: number

  /** 安全异常描边标记（透传，不进 Y 轴）。 */
  safety_anomaly: boolean
	safety_review_tag?: string
	safety_hint?: string
	safety_detail?: SafetyAnomaly
  /** 命中情况全为 suspected → 前端画虚边。 */
  suspected_only: boolean

  /** 兼容旧快照；新数据优先读取 safety_detail.unrelated_cost_ratio。 */
  unrelated_cost_ratio?: number

  calls: number

  usage_issues?: UsageIssue[]
}

/** 分群结果（§7，可空）。 */
export interface MemberCluster {
  label: string
  member_ids: string[]
}

/** 机器模板两句话结论（§8.5，只回填变量，不新算）。 */
export interface Conclusion {
  cost_main_bucket: CostBucket // uncached_input|cache|output
  issue_cost_ratio: number
  dominant_issue: DominantIssue
  note: string
}
