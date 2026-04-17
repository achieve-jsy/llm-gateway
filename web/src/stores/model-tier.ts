import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listModelTiers } from '@/services/control-plane'
import type { ModelTier } from '@/types/model-tier'

export const useModelTierStore = defineStore('modelTier', () => {
  const items = ref<ModelTier[]>([])
  const loading = ref(false)
  const loaded = ref(false)

  async function load(includeArchived = false) {
    loading.value = true
    try {
      items.value = await listModelTiers(includeArchived)
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  function byID(id: string | null | undefined) {
    return items.value.find(item => item.id === id)
  }

  return { items, loading, loaded, load, byID }
})
