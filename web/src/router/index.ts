import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/access/unified-entry'
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
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

/** 路由切换时更新页面标题 */
router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - LLM Gateway` : 'LLM Gateway Ops Console'
})

export default router
