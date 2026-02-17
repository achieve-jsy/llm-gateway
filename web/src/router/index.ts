import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/access/unified-entry'
  },
  { path: '/keys', name: 'KeyList', component: () => import('@/views/key/KeyListPage.vue'), meta: { title: '我的 Key' } },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginPage.vue'),
    meta: { title: '登录', public: true }
  },
  {
    path: '/auth/callback',
    name: 'AuthCallback',
    component: () => import('@/views/auth/OAuthCallbackPage.vue'),
    meta: { title: '登录中...', public: true }
  },
  {
    path: '/access/unified-entry',
    name: 'UnifiedEntry',
    component: () => import('@/views/access/UnifiedEntryPage.vue'),
    meta: { title: '首页' }
  },
  // ─── 管理/运维页面（仅 admin）────────────────────────────────────────────
  {
    path: '/admin/providers',
    name: 'ProviderList',
    component: () => import('@/views/admin/ProviderListPage.vue'),
    meta: { title: '渠道管理', requiresAdmin: true }
  },
  {
    path: '/admin/models',
    name: 'ModelList',
    component: () => import('@/views/admin/ModelListPage.vue'),
    meta: { title: '模型管理', requiresAdmin: true }
  },
  {
    path: '/admin/groups',
    name: 'Groups',
    component: () => import('@/views/GroupListPage.vue'),
    meta: { title: '模型组', requiresAdmin: true }
  },
  {
    path: '/admin/groups/:id',
    name: 'GroupDetail',
    component: () => import('@/views/admin/GroupDetailPage.vue'),
    meta: { title: '模型组详情', requiresAdmin: true }
  },
  {
    path: '/admin/entitlements',
    name: 'Entitlements',
    component: () => import('@/views/admin/EntitlementsPage.vue'),
    meta: { title: '模型组授权', requiresManager: true }
  },
  {
    path: '/admin/tags',
    name: 'TagList',
    component: () => import('@/views/admin/TagListPage.vue'),
    meta: { title: '标签管理', requiresAdmin: true }
  },
  {
    path: '/admin/catalog-sync',
    name: 'CatalogSync',
    component: () => import('@/views/admin/CatalogSyncPage.vue'),
    meta: { title: 'Catalog 同步', requiresAdmin: true }
  },
  {
    path: '/admin/chat-playground',
    name: 'ChatPlayground',
    component: () => import('@/views/admin/ChatPlaygroundPage.vue'),
    meta: { title: '对话测试', requiresAdmin: true }
  },
  {
    path: '/admin/health-monitor',
    name: 'HealthMonitor',
    component: () => import('@/views/admin/HealthMonitorPage.vue'),
    meta: { title: '模型健康监控', requiresAdmin: true }
  },
  // 模型组健康所有登录用户可见（帮助员工了解哪些模型可用）
  {
    path: '/admin/group-health-monitor',
    name: 'GroupHealthMonitor',
    component: () => import('@/views/admin/GroupHealthMonitorPage.vue'),
    meta: { title: '模型组健康监控' }
  },
  {
    path: '/admin/performance',
    name: 'Performance',
    component: () => import('@/views/admin/GroupHealthMonitorPage.vue'),
    meta: { title: '模型性能看板' }
  },
  {
    path: '/admin/ops',
    name: 'OpsDashboard',
    component: () => import('@/views/admin/ops/OpsDashboardPage.vue'),
    meta: { title: '运维监控', requiresAdmin: true }
  },
  {
    path: '/admin/usage-cost',
    name: 'UsageCost',
    component: () => import('@/views/admin/UsageCostPage.vue'),
    meta: { title: '用量与成本', requiresAdmin: true }
  },
  {
    path: '/my/usage',
    name: 'MyUsage',
    component: () => import('@/views/MyUsagePage.vue'),
    meta: { title: '我的用量' }
  },
  {
    path: '/model-square',
    name: 'ModelSquare',
    component: () => import('@/views/ModelSquarePage.vue'),
    meta: { title: '模型广场' }
  },
  {
    path: '/my/budget',
    name: 'MyBudget',
    component: () => import('@/views/MyBudgetPage.vue'),
    meta: { title: '我的预算' }
  },
  {
    path: '/admin/budget',
    name: 'BudgetManage',
    component: () => import('@/views/admin/BudgetManagePage.vue'),
    meta: { title: '预算管理', requiresManager: true }
  },
  {
    path: '/admin/service-keys',
    name: 'ServiceKeys',
    component: () => import('@/views/admin/ServiceKeyPage.vue'),
    meta: { title: 'Service Key', requiresManager: true }
  },
  {
    path: '/admin/audit-logs',
    name: 'AuditLogs',
    component: () => import('@/views/admin/AuditLogPage.vue'),
    meta: { title: '审计日志', requiresManager: true }
  },
  {
    path: '/admin/audit-logs',
    name: 'AuditLogs',
    component: () => import('@/views/admin/AuditLogPage.vue'),
    meta: { title: '审计日志', requiresManager: true }
  },
  {
    path: '/admin/group-entitlements',
    name: 'GroupEntitlements',
    redirect: '/admin/groups',
    meta: { title: '模型组授权', requiresAdmin: true }
  },
  {
    path: '/admin/expansion-requests',
    name: 'ExpansionRequests',
    component: () => import('@/views/admin/ExpansionRequestPage.vue'),
    meta: { title: '扩容申请' }
  },
  {
    path: '/admin/settings',
    name: 'Settings',
    component: () => import('@/views/admin/SettingsPage.vue'),
    meta: { title: '系统设置', requiresAdmin: true }
  },
  {
    path: '/admin/announcements',
    name: 'AnnouncementList',
    component: () => import('@/views/admin/AnnouncementListPage.vue'),
    meta: { title: '公告管理', requiresAdmin: true }
  },
  {
    path: '/admin/model-status-logs',
    name: 'ModelStatusLogs',
    component: () => import('@/views/admin/ModelStatusLogPage.vue'),
    meta: { title: '模型状态变更日志' }
  },
  {
    path: '/announcements',
    name: 'AnnouncementFeed',
    component: () => import('@/views/AnnouncementFeedPage.vue'),
    meta: { title: '公告中心' }
  },
  {
    path: '/announcements/:id',
    name: 'AnnouncementDetail',
    component: () => import('@/views/AnnouncementDetailPage.vue'),
    meta: { title: '公告详情' }
  },
  // ─── 组织架构（管理角色可访问，页面内再按 scope 过滤数据）───────────────
  {
    path: '/admin/org',
    name: 'OrgTree',
    component: () => import('@/views/admin/org/OrgTreePage.vue'),
    meta: { title: '组织架构', requiresManager: true }
  },
  // ─── 角色管理（仅 admin 可管理）──────────────────────────────────────────
  {
    path: '/admin/org/roles',
    name: 'OrgRoles',
    component: () => import('@/views/admin/org/RolePage.vue'),
    meta: { title: '角色管理', requiresAdmin: true }
  },
  // ─── 分层用量看板（管理角色可见）────────────────────────────────────────
  {
    path: '/admin/usage/dashboard',
    name: 'UsageDashboard',
    component: () => import('@/views/admin/usage/UsageDashboardPage.vue'),
    meta: { title: '用量看板', requiresManager: true }
  },
  // ─── 权限不足兜底页（不加 public，已登录用户需保留侧边栏）───────────────────
  {
    path: '/403',
    name: 'Forbidden',
    component: { template: '<div style="padding:60px;text-align:center;color:#909399"><h2 style="font-size:48px;margin:0">403</h2><p style="margin:12px 0 0">权限不足，无法访问该页面</p></div>' },
    meta: { title: '无权限' }
  },
  { path: '/:pathMatch(.*)*', redirect: '/access/unified-entry' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, _from, next) => {
  if (to.meta.public) { next(); return }
  const authStore = useAuthStore()
  if (!authStore.isLoggedIn) { next('/login'); return }

  // admin 专属路由（effectiveRole 含 org 层角色提升）
  if (to.meta.requiresAdmin && authStore.effectiveRole !== 'admin') {
    next('/403'); return
  }
  // 管理角色路由（dept_admin / studio_admin / admin）
  const managerRoles = ['dept_admin', 'studio_admin', 'admin']
  if (to.meta.requiresManager && !managerRoles.includes(authStore.effectiveRole || '')) {
    next('/403'); return
  }

  next()
})

/** 路由切换时更新页面标题 */
router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - LLM Gateway` : 'LLM Gateway Ops Console'
})

export default router
