import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { BreadcrumbItem, DashboardResponse, DashScope } from '@/types/dashboard'
import http from "@/services/http"

export const useDashboardStore = defineStore('dashboard', () => {
  const breadcrumbs = ref<BreadcrumbItem[]>([])
  const currentData = ref<DashboardResponse | null>(null)
  const loading = ref(false)
  const period = ref({ start: defaultStartDate(), end: defaultEndDate() })
  const page = ref(1)
  const pageSize = ref(20)

  function defaultStartDate() {
    const d = new Date()
    d.setDate(d.getDate() - 30)
    return d.toISOString().slice(0, 10)
  }

  function defaultEndDate() {
    return new Date().toISOString().slice(0, 10)
  }

  async function fetchDashboard(forceScope?: DashScope, forceID?: string) {
    loading.value = true
    try {
      const last = breadcrumbs.value[breadcrumbs.value.length - 1]
      const scope = forceScope || last?.scope || 'company'
      const id = forceID !== undefined ? forceID : (last?.id || '')
      const params: Record<string, any> = {
        scope, start: period.value.start, end: period.value.end,
        page: page.value, page_size: pageSize.value,
      }
      if (id) params.id = id
      const res = await http.get('/api/v1/usage/dashboard', { params })
      currentData.value = res.data
      return res.data as DashboardResponse
    } finally {
      loading.value = false
    }
  }

  async function drillDown(item: BreadcrumbItem) {
    breadcrumbs.value.push(item)
    page.value = 1
    return fetchDashboard()
  }

  async function popToLevel(index: number) {
    breadcrumbs.value = breadcrumbs.value.slice(0, index + 1)
    page.value = 1
    return fetchDashboard()
  }

  async function resetToTop() {
    breadcrumbs.value = []
    page.value = 1
    return fetchDashboard()
  }

  async function setPeriod(start: string, end: string) {
    period.value = { start, end }
    page.value = 1
    return fetchDashboard()
  }

  return {
    breadcrumbs, currentData, loading, period, page, pageSize,
    fetchDashboard, drillDown, popToLevel, resetToTop, setPeriod,
  }
})
