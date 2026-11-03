import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useKeyCacheStore = defineStore('keyCache', () => {
  const rawKeys = ref<Map<string, string>>(new Map())

  function cacheKey(keyId: string, rawKey: string) {
    rawKeys.value.set(keyId, rawKey)
  }

  function getRawKey(keyId: string): string | undefined {
    return rawKeys.value.get(keyId)
  }

  function hasRawKey(keyId: string): boolean {
    return rawKeys.value.has(keyId)
  }

  function clearKey(keyId: string) {
    rawKeys.value.delete(keyId)
  }

  return { rawKeys, cacheKey, getRawKey, hasRawKey, clearKey }
})