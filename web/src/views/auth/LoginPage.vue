<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { type FormInstance, type FormRules } from 'element-plus'
import http from '@/services/http'

const authStore = useAuthStore()
const router = useRouter()
const formRef = ref<FormInstance>()

const form = reactive({
  username: '',
  password: '',
})

const rules = reactive<FormRules>({
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

const loginLoading = ref(false)
const loginError = ref('')
const showDevLogin = ref(false)

function redirectToSSO() {
  window.location.href = '/api/v1/auth/oidc/login'
}

function toggleDevLogin() {
  showDevLogin.value = !showDevLogin.value
  loginError.value = ''
}

async function handleDevLogin() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loginLoading.value = true
  loginError.value = ''
  try {
    const res = await http.post('/auth/dev/login', { username: form.username, password: form.password })
    authStore.setAuth({
      accessToken: res.data.accessToken,
      refreshToken: res.data.refreshToken || '',
      user: { id: res.data.user?.id || '', name: res.data.user?.name || form.username, email: '', departmentId: '', departmentName: '' },
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

      <el-button type="primary" size="large" @click="redirectToSSO" class="sso-btn">
        使用公司账号登录 (SSO)
      </el-button>

      <div class="dev-login-toggle">
        <span @click="toggleDevLogin" class="toggle-link">
          {{ showDevLogin ? '收起' : '其他登录方式' }}
        </span>
      </div>

      <transition name="slide-down">
        <div v-if="showDevLogin" class="dev-login-section">
          <el-divider />
          <el-form ref="formRef" :model="form" :rules="rules" class="login-form" @submit.prevent>
            <el-form-item prop="username">
              <el-input v-model="form.username" placeholder="用户名" size="large" clearable />
            </el-form-item>
            <el-form-item prop="password">
              <el-input v-model="form.password" type="password" placeholder="密码" size="large" show-password @keyup.enter="handleDevLogin" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" plain size="large" :loading="loginLoading" @click="handleDevLogin" style="width:100%">
                账号密码登录
              </el-button>
            </el-form-item>
            <p v-if="loginError" class="login-error">{{ loginError }}</p>
          </el-form>
        </div>
      </transition>
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
  padding: 48px 32px 40px;
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

.sso-btn {
  width: 100%;
}

.dev-login-toggle {
  margin-top: 20px;
}

.toggle-link {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  cursor: pointer;
  user-select: none;
  transition: color 0.2s;
}

.toggle-link:hover {
  color: var(--el-text-color-secondary);
}

.dev-login-section {
  overflow: hidden;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.login-error {
  color: var(--el-color-danger);
  font-size: 13px;
  margin: 0;
}

.slide-down-enter-active,
.slide-down-leave-active {
  transition: all 0.25s ease;
  max-height: 300px;
}

.slide-down-enter-from,
.slide-down-leave-to {
  max-height: 0;
  opacity: 0;
}

@media (max-width: 768px) {
  .login-card {
    width: 90%;
    max-width: 400px;
    padding: 32px 20px 28px;
    margin: 0 16px;
  }
  .login-logo h1 { font-size: 24px; }
  .login-logo p { font-size: 14px; }
}

@media (max-width: 480px) {
  .login-card {
    padding: 24px 16px 20px;
    margin: 0 12px;
  }
  .login-logo h1 { font-size: 22px; }
  .sso-btn { font-size: 13px; }
}
</style>
