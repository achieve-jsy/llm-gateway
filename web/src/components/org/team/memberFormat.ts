// 成员配置三块摘要（配额 / 预算规则 / 模型授权）与负责人文案的格式化。
// 页面与卡片共用，避免模板里堆三元判断。纯函数，便于单测。
import type { DeptMember } from '@/stores/org'
import type { AccountBrief, DeptStatus, MemberConfigSummary, OrgLeaderBrief } from '@/types/org'

export interface QuotaLike { rpm: number; max_concurrency: number; source: string }
export interface BudgetLike { monthly: number; daily: number; weekly: number; source: string }
export interface EntitlementLike { group_count: number; source: string }

// 后端 members[] 在既有字段上补 is_dept_admin 与 config{quota,budget,entitlement}（见契约 org_workspace）；
// 同时兼容字段平铺在行上的旧写法。
export type MemberConfigParts = Pick<MemberConfigSummary, 'quota' | 'budget' | 'entitlement'>

export interface MemberRow extends DeptMember {
  is_dept_admin?: boolean
  config?: MemberConfigParts
  quota?: QuotaLike
  budget?: BudgetLike
  entitlement?: EntitlementLike
}

export function configOf(m: MemberRow): MemberConfigParts {
  return (m.config ?? m) as MemberConfigParts
}

// 工作台头部展示的"当前团队"视图模型（overview.teams 或本地部门兜底都能映射成它）
export interface TeamHeaderTeam {
  deptId: string
  name: string
  status?: DeptStatus
  memberCount?: number
  admins: AccountBrief[]
  leaders: OrgLeaderBrief[]
}

function limitText(v: number): string {
  return v > 0 ? String(v) : '不限'
}

// 配额：只有 override 才显示具体值，继承一律显示「继承」。
export function quotaText(q?: QuotaLike | null): string {
  if (!q) return '—'
  if (q.source !== 'override') return '继承'
  return `${limitText(Number(q.rpm || 0))} RPM / ${limitText(Number(q.max_concurrency || 0))} 并发`
}

export function isQuotaOverride(q?: QuotaLike | null): boolean {
  return !!q && q.source === 'override'
}

// 预算规则：team=继承团队池（dept_daily），member=成员单独设置（只列非 0 周期）。
export function budgetText(b?: BudgetLike | null): string {
  if (!b || b.source === 'none') return '—'
  if (b.source === 'team') return '继承团队'
  const parts: string[] = []
  if (b.monthly > 0) parts.push(`月 ¥${b.monthly}`)
  if (b.daily > 0) parts.push(`日 ¥${b.daily}`)
  if (b.weekly > 0) parts.push(`周 ¥${b.weekly}`)
  return parts.length ? parts.join(' / ') : '—'
}

// 模型授权：只有工作室/团队两级，成员行显示继承后的可用模型组数。
export function entitlementText(e?: EntitlementLike | null): string {
  if (!e || e.source === 'none' || !(e.group_count > 0)) return '—'
  return `${e.group_count} 模型可用`
}

// 负责人来自 EHR（只读、多值）：单人「张三负责」，多人「张三等3人负责」。
export function leadersText(leaders: Array<{ name: string }> = []): string {
  if (!leaders.length) return '未设置负责人'
  if (leaders.length === 1) return `${leaders[0].name}负责`
  return `${leaders[0].name}等${leaders.length}人负责`
}
