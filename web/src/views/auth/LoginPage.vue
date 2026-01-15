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

const features = [
  { title: '安全可靠', desc: '多重安全防护' },
  { title: '高效便捷', desc: '一键安全登录' },
  { title: '统一管理', desc: '畅行所有系统' },
]

function redirectToDaqun() {
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
    <!-- 背景：循环播放、静音、自动播放的品牌视频 -->
    <video
      class="login-bg-video"
      src="/images/bg-login.mp4"
      autoplay
      loop
      muted
      playsinline
      preload="auto"
      disablepictureinpicture
    ></video>
    <!-- 左侧可读性渐变遮罩 -->
    <div class="login-overlay"></div>

    <!-- 顶部 Logo -->
    <header class="login-logo-top">
      <img src="/images/logo_example.png" alt="某公司网络" />
    </header>

    <!-- 左侧品牌标语 -->
    <section class="login-hero">
      <h1 class="hero-title">
        <span class="hero-title-1">智联某公司</span>
        <span class="hero-title-2">创领未来</span>
      </h1>
      <p class="hero-subtitle">用技术连接世界 · 用智能驱动创新</p>
    </section>

    <!-- 右侧登录卡片 -->
    <section class="login-card">
      <div class="card-head">
        <span class="card-leaf" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="22" height="22">
            <path
              fill="currentColor"
              d="M21 3c-9 0-15 4-15 11a8 8 0 0 0 1.2 4.3l-2 2A1 1 0 0 0 6.6 21l2-2A8 8 0 0 0 13 20c7 0 8-9 8-17ZM9.3 16.7a1 1 0 0 1-.1-1.3C11 13 13.5 11.3 16.5 10c-3 .4-5.6 1.8-7.5 4a1 1 0 0 1-1.4.1 1 1 0 0 1 .2-1.5C10 14 13.5 12 18 11.4c-3.2 1.8-6 4.3-8 7.4l-.7-2.1Z"
            />
          </svg>
        </span>
        <h2 class="card-title">欢迎登录某公司网络</h2>
      </div>
      <p class="card-subtitle">统一大模型接入管理平台</p>

      <!-- 盾牌装饰 -->
      <div class="card-shield" aria-hidden="true">
        <span class="shield-orbit"></span>
        <span class="shield-badge shield-badge--lock">
          <svg viewBox="0 0 24 24" width="14" height="14"><path fill="currentColor" d="M12 1a5 5 0 0 0-5 5v3H6a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-9a2 2 0 0 0-2-2h-1V6a5 5 0 0 0-5-5Zm3 8H9V6a3 3 0 0 1 6 0v3Z"/></svg>
        </span>
        <span class="shield-badge shield-badge--check">
          <svg viewBox="0 0 24 24" width="14" height="14"><path fill="currentColor" d="M9 16.2 4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4L9 16.2Z"/></svg>
        </span>
        <svg class="shield-svg" viewBox="0 0 120 132" width="120" height="132">
          <defs>
            <linearGradient id="shieldGrad" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stop-color="var(--login-green-light)" />
              <stop offset="100%" stop-color="var(--login-green-deep)" />
            </linearGradient>
          </defs>
          <path
            d="M60 4 8 24v40c0 34 22 54 52 64 30-10 52-30 52-64V24L60 4Z"
            fill="url(#shieldGrad)"
          />
          <circle cx="60" cy="52" r="17" fill="#fff" fill-opacity="0.95" />
          <path
            d="M60 78c-16 0-28 8-28 18 0 1 .4 2 1.2 2h53.6c.8 0 1.2-1 1.2-2 0-10-12-18-28-18Z"
            fill="#fff"
            fill-opacity="0.95"
          />
        </svg>
      </div>

      <!-- 大群登录主按钮 -->
      <button type="button" class="sso-btn" @click="redirectToDaqun">大群登录</button>

      <!-- 特性 -->
      <ul class="card-features">
        <li v-for="f in features" :key="f.title">
          <span class="feature-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="14" height="14"><path fill="currentColor" d="M9 16.2 4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4L9 16.2Z"/></svg>
          </span>
          <strong>{{ f.title }}</strong>
          <small>{{ f.desc }}</small>
        </li>
      </ul>

      <p class="card-help">
        如需帮助，请
        <span class="help-link" @click="toggleDevLogin">{{ showDevLogin ? '收起其他登录方式' : '联系管理员或使用其他登录' }}</span>
      </p>

      <!-- 开发/账号密码登录 -->
      <transition name="slide-down">
        <div v-if="showDevLogin" class="dev-login-section">
          <el-form ref="formRef" :model="form" :rules="rules" class="login-form" @submit.prevent>
            <el-form-item prop="username">
              <el-input v-model="form.username" placeholder="用户名" size="large" clearable />
            </el-form-item>
            <el-form-item prop="password">
              <el-input v-model="form.password" type="password" placeholder="密码" size="large" show-password @keyup.enter="handleDevLogin" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" plain size="large" :loading="loginLoading" @click="handleDevLogin" class="dev-login-btn" style="width:100%">
                账号密码登录
              </el-button>
            </el-form-item>
            <p v-if="loginError" class="login-error">{{ loginError }}</p>
          </el-form>
        </div>
      </transition>
    </section>
  </div>
</template>

<style scoped>
.login-page {
  --login-green: #36b34e;
  --login-green-light: #62c96c;
  --login-green-deep: #2a9c4b;
  --login-green-text: #2f9e4a;
  position: relative;
  width: 100vw;
  height: 100vh;
  overflow: hidden;
  font-family: var(--el-font-family);
}

.login-bg-video {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  z-index: 0;
}

.login-overlay {
  position: absolute;
  inset: 0;
  z-index: 1;
  background:
    linear-gradient(90deg, rgba(255, 255, 255, 0.55) 0%, rgba(255, 255, 255, 0.12) 32%, rgba(255, 255, 255, 0) 55%);
  pointer-events: none;
}

/* 顶部 Logo */
.login-logo-top {
  position: absolute;
  top: 32px;
  left: 48px;
  z-index: 3;
}
.login-logo-top img {
  height: 46px;
  width: auto;
  display: block;
}

/* 左侧品牌标语 */
.login-hero {
  position: absolute;
  top: 50%;
  left: 64px;
  transform: translateY(-50%);
  z-index: 3;
  max-width: 420px;
}
.hero-title {
  margin: 0;
  font-weight: 800;
  line-height: 1.12;
  letter-spacing: 2px;
  display: flex;
  flex-direction: column;
  font-size: 64px;
}
.hero-title-1 {
  background: linear-gradient(90deg, #2f7d3a, #3c9b3e);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  color: #3c9b3e;
}
.hero-title-2 {
  background: linear-gradient(90deg, var(--login-green-light), var(--login-green));
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  color: var(--login-green);
}
.hero-subtitle {
  margin: 22px 0 0;
  font-size: 18px;
  letter-spacing: 1px;
  color: #4a5a4d;
}

/* 右侧登录卡片 */
.login-card {
  position: absolute;
  top: 50%;
  right: 96px;
  transform: translateY(-50%);
  z-index: 3;
  width: 380px;
  padding: 40px 36px 32px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.78);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  border: 1px solid rgba(255, 255, 255, 0.6);
  box-shadow: 0 24px 60px rgba(31, 90, 50, 0.18);
  text-align: center;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.card-leaf {
  color: var(--login-green);
  display: inline-flex;
}
.card-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: #2b3a2e;
}
.card-subtitle {
  margin: 8px 0 0;
  font-size: 14px;
  color: #8a9690;
}

/* 盾牌装饰 */
.card-shield {
  position: relative;
  height: 150px;
  margin: 18px auto 26px;
  width: 150px;
}
.shield-svg {
  position: relative;
  z-index: 1;
  filter: drop-shadow(0 12px 24px rgba(42, 156, 75, 0.3));
}
.shield-orbit {
  position: absolute;
  inset: 8px;
  border-radius: 50%;
  border: 1px dashed rgba(42, 156, 75, 0.35);
}
.shield-badge {
  position: absolute;
  z-index: 2;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  box-shadow: 0 6px 14px rgba(42, 156, 75, 0.25);
}
.shield-badge--lock {
  top: 6px;
  right: 8px;
  background: linear-gradient(135deg, var(--login-green-light), var(--login-green));
}
.shield-badge--check {
  bottom: 16px;
  right: 2px;
  background: linear-gradient(135deg, var(--login-green-light), var(--login-green-deep));
}

/* SSO 主按钮 */
.sso-btn {
  width: 100%;
  height: 50px;
  border: none;
  border-radius: 12px;
  cursor: pointer;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 4px;
  color: #fff;
  background: linear-gradient(135deg, var(--login-green-light) 0%, var(--login-green-deep) 100%);
  box-shadow: 0 12px 24px rgba(42, 156, 75, 0.32);
  transition: transform 0.15s ease, box-shadow 0.15s ease, filter 0.15s ease;
}
.sso-btn:hover {
  filter: brightness(1.04);
  transform: translateY(-1px);
  box-shadow: 0 16px 30px rgba(42, 156, 75, 0.4);
}
.sso-btn:active {
  transform: translateY(0);
}

/* 特性 */
.card-features {
  list-style: none;
  margin: 26px 0 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
}
.card-features li {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
}
.feature-icon {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: rgba(42, 156, 75, 0.12);
  color: var(--login-green-deep);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 2px;
}
.card-features strong {
  font-size: 13px;
  color: #3a4a3d;
  font-weight: 600;
}
.card-features small {
  font-size: 11px;
  color: #97a39c;
}

.card-help {
  margin: 24px 0 0;
  font-size: 12px;
  color: #97a39c;
}
.help-link {
  color: var(--login-green-deep);
  cursor: pointer;
  user-select: none;
}
.help-link:hover {
  text-decoration: underline;
}

/* 账号密码登录 */
.dev-login-section {
  overflow: hidden;
}
.dev-login-btn {
  background: linear-gradient(135deg, var(--login-green-light) 0%, var(--login-green-deep) 100%) !important;
  border-color: var(--login-green) !important;
  color: #fff !important;
}
.dev-login-btn:hover {
  filter: brightness(1.04);
  transform: translateY(-1px);
  box-shadow: 0 8px 20px rgba(42, 156, 75, 0.35);
}
.dev-login-btn:active {
  transform: translateY(0);
}
.login-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 16px;
}
.login-error {
  color: var(--el-color-danger);
  font-size: 13px;
  margin: 0;
}
.slide-down-enter-active,
.slide-down-leave-active {
  transition: all 0.25s ease;
  max-height: 320px;
}
.slide-down-enter-from,
.slide-down-leave-to {
  max-height: 0;
  opacity: 0;
}

/* 响应式 */
@media (max-width: 1180px) {
  .hero-title { font-size: 52px; }
  .login-card { right: 48px; }
}
@media (max-width: 900px) {
  .login-overlay {
    background: rgba(255, 255, 255, 0.42);
  }
  .login-hero {
    left: 50%;
    top: 18%;
    transform: translateX(-50%);
    text-align: center;
    align-items: center;
  }
  .hero-title { align-items: center; font-size: 44px; }
  .login-card {
    right: 50%;
    top: auto;
    bottom: 64px;
    transform: translateX(50%);
    width: min(380px, 88vw);
  }
}
@media (max-width: 480px) {
  .login-logo-top { left: 20px; top: 20px; }
  .login-logo-top img { height: 38px; }
  .hero-title { font-size: 36px; }
  .hero-subtitle { font-size: 14px; }
  .login-card { padding: 28px 22px 24px; }
  .card-shield { height: 120px; width: 120px; }
}
</style>
