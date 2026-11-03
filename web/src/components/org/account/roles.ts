// roles.ts — 角色文案与徽标配色的唯一来源（统计卡、徽标、提示语共用）
import type { RoleKey } from '@/types/org'

export interface RoleMeta {
  label: string
  type: 'danger' | 'warning' | 'success' | 'info'
}

// 内部 role 值不变，对外文案统一：dept_admin→团队管理员、studio_admin→工作室管理员
export const ROLE_META: Record<RoleKey, RoleMeta> = {
  admin: { label: '平台管理员', type: 'danger' },
  studio_admin: { label: '工作室管理员', type: 'warning' },
  dept_admin: { label: '团队管理员', type: 'success' },
  user: { label: '员工', type: 'info' },
}

// 展示顺序即权限高低，与后端 primary_role 优先级（admin > studio_admin > dept_admin > user）一致
export const ROLE_KEYS: RoleKey[] = ['admin', 'studio_admin', 'dept_admin', 'user']

export function roleLabel(role: RoleKey): string {
  return ROLE_META[role]?.label || role
}
