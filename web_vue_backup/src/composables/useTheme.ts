import { ref } from 'vue'

const STORAGE_KEY = 'llm-gateway-theme'

/** 当前是否为深色模式 */
export const isDark = ref(false)

/** 初始化主题（从 localStorage 读取） */
export function initTheme() {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'dark') {
    isDark.value = true
  } else if (stored === 'light') {
    isDark.value = false
  } else {
    // 默认浅色模式
    isDark.value = false
  }
  applyTheme()
}

/** 切换主题 */
export function toggleTheme() {
  isDark.value = !isDark.value
  localStorage.setItem(STORAGE_KEY, isDark.value ? 'dark' : 'light')
  applyTheme()
}

/** 应用主题到 DOM */
function applyTheme() {
  if (isDark.value) {
    document.documentElement.classList.add('dark')
  } else {
    document.documentElement.classList.remove('dark')
  }
}


