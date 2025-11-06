<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const router = useRouter()

onMounted(() => {
  if (authStore.isAuthenticated) {
    router.replace('/')
    return
  }
  // Auto-redirect to SSO
  authStore.loginRedirect()
})
</script>

<template>
  <div class="login-page">
    <div class="login-container">
      <div class="login-card">
        <div class="login-logo">
          <h1>LLM Gateway</h1>
          <p>统一大模型接入管理平台</p>
        </div>
        <div class="login-loading">
          <el-icon class="is-loading" :size="32">
            <Loading />
          </el-icon>
          <p>正在跳转至 SSO 登录...</p>
        </div>
        <el-button type="primary" size="large" @click="authStore.loginRedirect()">
          使用大群账号登录
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}

.login-card {
  width: 400px;
  padding: 48px 32px;
  background: var(--el-bg-color);
  border-radius: 12px;
  box-shadow: var(--el-box-shadow-light);
  text-align: center;
}

.login-logo h1 {
  font-size: 28px;
  font-weight: 700;
  margin-bottom: 8px;
  color: var(--el-text-color-primary);
}

.login-logo p {
  color: var(--el-text-color-secondary);
  margin-bottom: 32px;
}

.login-loading {
  margin-bottom: 24px;
}

.login-loading .el-icon {
  color: var(--el-color-primary);
  margin-bottom: 12px;
}

.login-loading p {
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
</style>
