<template>
  <el-tag :type="meta.type" size="small" disable-transitions class="role-badge">{{ meta.label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { RoleKey } from '@/types/org'
import { ROLE_META } from './roles'

const props = defineProps<{ role: RoleKey }>()
const meta = computed(() => ROLE_META[props.role] || ROLE_META.user)
</script>

<style scoped>
.role-badge { font-weight: 500; }
</style>
