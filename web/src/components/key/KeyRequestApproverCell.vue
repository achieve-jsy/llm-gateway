<template>
  <span v-if="status === 'pending'" class="text-muted">部门负责人</span>
  <span v-else-if="status === 'cancelled'" class="text-muted">—</span>
  <span v-else-if="displayName">{{ displayName }}</span>
  <span v-else class="text-muted">—</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { KeyRequestStatus } from '@/types/key-request'

const props = defineProps<{
  status: KeyRequestStatus
  approverName?: string
}>()

const displayName = computed(() => props.approverName?.trim() || '')
</script>
