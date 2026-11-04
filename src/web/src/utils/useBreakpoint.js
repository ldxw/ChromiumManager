import { computed, ref } from 'vue'

/**
 * 全局共享的视口宽度，只注册一次 resize 监听。
 * 断点：mobile < 768，tablet < 1024，compact < 1280
 */
const width = ref(typeof window === 'undefined' ? 1440 : window.innerWidth)
let bound = false

const onResize = () => {
  width.value = window.innerWidth
}

const bind = () => {
  if (bound || typeof window === 'undefined') return
  bound = true
  window.addEventListener('resize', onResize, { passive: true })
}

export const useBreakpoint = () => {
  bind()
  return {
    width,
    isMobile: computed(() => width.value < 768),
    isTablet: computed(() => width.value < 1024),
    isCompact: computed(() => width.value < 1280)
  }
}
