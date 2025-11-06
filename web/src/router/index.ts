import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/access/unified-entry'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginPage.vue'),
    meta: { title: '登录', public: true }
  },
  {
    path: '/oauth/callback',
    name: 'OAuthCallback',
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
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

/** 路由守卫：开发模式下直接放行，生产模式未登录跳转登录页 */
router.beforeEach((_to, _from, next) => {
  // 当前开发阶段直接放行，后续对接 SSO 后恢复认证检查
  next()
})

/** 路由切换时更新页面标题 */
router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - LLM Gateway` : 'LLM Gateway Ops Console'
})

export default router
