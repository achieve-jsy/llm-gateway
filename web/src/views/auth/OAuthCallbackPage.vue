<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const error = ref('')

onMounted(async () => {
  const token = route.query.token as string

  if (!token) {
    error.value = '登录失败：未获取到认证令牌'
    return
  }

  // Save token and fetch user info
  authStore.setToken(token)
  await authStore.fetchCurrentUser()

  if (authStore.isAuthenticated) {
    const redirect = localStorage.getItem('auth_redirect') || '/'
    localStorage.removeItem('auth_redirect')
    router.replace(redirect)
  } else {
    error.value = '登录失败：令牌验证未通过'
  }
})
</script>

<template>
  <div class="callback-page">
    <div class="callback-container">
      <template v-if="error">
        <el-result icon="error" :title="error">
          <template #extra>
            <el-button type="primary" @click="authStore.loginRedirect()">
              重新登录
            </el-button>
          </template>
        </el-result>
      </template>
      <template v-else>
        <div class="callback-loading">
          <el-icon class="is-loading" :size="48">
            <Loading />
          </el-icon>
          <p>登录成功，正在跳转...</p>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.callback-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}

.callback-container {
  text-align: center;
}

.callback-loading .el-icon {
  color: var(--el-color-primary);
  margin-bottom: 16px;
}

.callback-loading p {
  color: var(--el-text-color-secondary);
  font-size: 16px;
}
</style>
