import { ref, onMounted, onUnmounted } from 'vue'

/**
 * 移动端断点检测 composable
 * 宽度 < 768px (Tailwind md断点) 时返回 true
 */
export function useIsMobile(breakpoint = 768) {
  const isMobile = ref(false)

  function check() {
    isMobile.value = window.innerWidth < breakpoint
  }

  onMounted(() => {
    check()
    window.addEventListener('resize', check)
  })

  onUnmounted(() => {
    window.removeEventListener('resize', check)
  })

  return { isMobile }
}