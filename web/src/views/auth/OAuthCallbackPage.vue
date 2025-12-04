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
import { ElNotification } from 'element-plus'
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

    // ── 部门映射状态提示 ──────────────────────────────────────────────
    const ds = authStore.deptStatus
    if (ds === 'none') {
      ElNotification({
        title: '部门信息缺失',
        message: '你的账号暂未关联部门，部分功能可能受限。请联系平台管理员。',
        type: 'warning',
        duration: 8000,
        position: 'top-right',
      })
    } else if (ds === 'pending_studio') {
      ElNotification({
        title: '部门待归属',
        message: '你所在的部门尚未归入任何工作室，数据统计可能不完整。请联系平台管理员完成映射配置。',
        type: 'warning',
        duration: 8000,
        position: 'top-right',
      })
    }

    // ── Bootstrap 管理员提示 ──────────────────────────────────────────
    if (authStore.isBootstrapAdmin) {
      ElNotification({
        title: '你已成为初始管理员',
        message: '请尽快完善「部门 → 工作室」映射，并为各工作室/部门指定负责人。',
        type: 'success',
        duration: 10000,
        position: 'top-right',
      })
    }

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
