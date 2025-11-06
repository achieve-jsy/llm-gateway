<template>
  <div class="callback-page">
    <el-result v-if="error" icon="error" title="登录失败" :sub-title="error">
      <template #extra><el-button type="primary" @click="$router.push('/login')">重新登录</el-button></template>
    </el-result>
    <el-result v-else icon="success" title="登录成功">
      <template #sub-title>正在跳转...</template>
    </el-result>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const error = ref('')

onMounted(async () => {
  const code = route.query.code as string
  const state = route.query.state as string
  if (!code || !state) { error.value = '缺少认证参数'; return }
  try {
    await authStore.handleCallback(code, state)
    setTimeout(() => router.replace('/home'), 500)
  } catch (e: any) {
    error.value = e?.response?.data?.message || '认证失败，请重试'
  }
})
</script>

<style scoped>
.callback-page {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 60vh;
}
</style>
