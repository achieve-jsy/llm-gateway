import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/access/unified-entry'
  },
  { path: '/home', name: 'Home', component: () => import('@/views/home/HomePage.vue'), meta: { title: '首页' } },
  { path: '/keys', name: 'KeyList', component: () => import('@/views/key/KeyListPage.vue'), meta: { title: '我的 Key' } },
  { path: '/models', name: 'MeModels', component: () => import('@/views/model/ModelListPage.vue'), meta: { title: '可用模型' } },
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
    meta: { title: '统一接入能力' }
  },
  {
    path: '/admin/providers',
    name: 'ProviderList',
    component: () => import('@/views/admin/ProviderListPage.vue'),
    meta: { title: '渠道管理' }
  },
  {
    path: '/admin/models',
    name: 'ModelList',
    component: () => import('@/views/admin/ModelListPage.vue'),
    meta: { title: '模型管理' }
  },
  {
    path: '/admin/tags',
    name: 'TagList',
    component: () => import('@/views/admin/TagListPage.vue'),
    meta: { title: '标签管理' }
  },
  {
    path: '/admin/catalog-sync',
    name: 'CatalogSync',
    component: () => import('@/views/admin/CatalogSyncPage.vue'),
    meta: { title: 'Catalog 同步' }
  },
  {
    path: '/admin/chat-playground',
    name: 'ChatPlayground',
    component: () => import('@/views/admin/ChatPlaygroundPage.vue'),
    meta: { title: '对话测试' }
  },
  {
    path: '/admin/health-monitor',
    name: 'HealthMonitor',
    component: () => import('@/views/admin/HealthMonitorPage.vue'),
    meta: { title: '健康监控' }
  },
  {
    path: '/admin/settings',
    name: 'Settings',
    component: () => import('@/views/admin/SettingsPage.vue'),
    meta: { title: '系统设置' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, _from, next) => {
  if (to.meta.public) { next(); return }
  const authStore = useAuthStore()
  if (!authStore.isLoggedIn) { next('/login'); return }
  next()
})

/** 路由切换时更新页面标题 */
router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - LLM Gateway` : 'LLM Gateway Ops Console'
})

export default router
