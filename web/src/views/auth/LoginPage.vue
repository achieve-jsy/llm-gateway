<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import http from '@/services/http'

const authStore = useAuthStore()
const router = useRouter()
const username = ref('')
const password = ref('')
const loginLoading = ref(false)
const loginError = ref('')

function redirectToSSO() {
  window.location.href = '/api/v1/auth/oidc/login'
}

async function handleDevLogin() {
  if (!username.value || !password.value) return
  loginLoading.value = true
  loginError.value = ''
  try {
    const res = await http.post('/auth/dev/login', { username: username.value, password: password.value })
    authStore.setAuth({
      accessToken: res.data.accessToken,
      refreshToken: res.data.refreshToken || '',
      user: { id: res.data.user?.id || '', name: res.data.user?.name || username.value, email: '', departmentId: '', departmentName: '' },
      role: res.data.user?.role || 'admin',
    })
    router.replace('/home')
  } catch (e: any) {
    loginError.value = e?.response?.data?.error || '登录失败'
  } finally {
    loginLoading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-logo">
        <h1>LLM Gateway</h1>
        <p>统一大模型接入管理平台</p>
      </div>

      <div class="login-form">
        <el-input v-model="username" placeholder="用户名" size="large" clearable />
        <el-input v-model="password" type="password" placeholder="密码" size="large" show-password @keyup.enter="handleDevLogin" />
        <el-button type="primary" size="large" :loading="loginLoading" @click="handleDevLogin" style="width:100%">
          登录
        </el-button>
        <p v-if="loginError" class="login-error">{{ loginError }}</p>
      </div>

      <el-divider>或</el-divider>

      <el-button size="large" @click="redirectToSSO" style="width:100%">
        使用公司账号登录 (SSO)
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.login-page { height: 100vh; display: flex; align-items: center; justify-content: center; background: var(--el-bg-color-page); }
.login-card { width: 400px; padding: 48px 32px; background: var(--el-bg-color); border-radius: 12px; box-shadow: var(--el-box-shadow-light); text-align: center; }
.login-logo h1 { font-size: 28px; font-weight: 700; margin-bottom: 8px; color: var(--el-text-color-primary); }
.login-logo p { color: var(--el-text-color-secondary); margin-bottom: 24px; }
.login-form { display: flex; flex-direction: column; gap: 12px; }
.login-error { color: var(--el-color-danger); font-size: 13px; margin: 0; }
</style>
