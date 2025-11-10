import { describe, it, expect, beforeEach, vi } from 'vitest'
import { isDark, initTheme, toggleTheme } from './useTheme'

const storage: Record<string, string> = {}

vi.stubGlobal('localStorage', {
  getItem: vi.fn((key: string) => storage[key] ?? null),
  setItem: vi.fn((key: string, value: string) => { storage[key] = value }),
  removeItem: vi.fn((key: string) => { delete storage[key] }),
  clear: vi.fn(() => { Object.keys(storage).forEach(k => delete storage[k]) }),
  length: 0,
  key: vi.fn(() => null)
})

describe('useTheme Composable', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    document.documentElement.className = ''
  })

  describe('isDark reactive ref', () => {
    it('should be initialized as false', () => {
      expect(isDark.value).toBe(false)
    })

    it('should be a reactive ref', () => {
      expect(typeof isDark.value).toBe('boolean')
      isDark.value = true
      expect(isDark.value).toBe(true)
      isDark.value = false
      expect(isDark.value).toBe(false)
    })
  })

  describe('initTheme', () => {
    it('should set isDark to true when localStorage contains "dark"', () => {
      localStorage.setItem('llm-gateway-theme', 'dark')
      isDark.value = false

      initTheme()

      expect(isDark.value).toBe(true)
    })

    it('should set isDark to false when localStorage contains "light"', () => {
      localStorage.setItem('llm-gateway-theme', 'light')
      isDark.value = true

      initTheme()

      expect(isDark.value).toBe(false)
    })

    it('should default to light mode when localStorage is empty', () => {
      localStorage.setItem('llm-gateway-theme', '')

      initTheme()

      expect(isDark.value).toBe(false)
    })

    it('should default to light mode despite system dark preference', () => {
      localStorage.setItem('llm-gateway-theme', '')
      isDark.value = true

      initTheme()

      expect(isDark.value).toBe(false)
    })

    it('should add dark class to document element when isDark is true', () => {
      localStorage.setItem('llm-gateway-theme', 'dark')
      isDark.value = false

      initTheme()

      expect(document.documentElement.classList.contains('dark')).toBe(true)
    })

    it('should not have dark class when isDark is false', () => {
      localStorage.setItem('llm-gateway-theme', 'light')
      isDark.value = true
      document.documentElement.classList.add('dark')

      initTheme()

      expect(document.documentElement.classList.contains('dark')).toBe(false)
    })

    it('should handle null localStorage value', () => {
      localStorage.setItem('llm-gateway-theme', '')

      expect(() => initTheme()).not.toThrow()
    })

    it('should default to light when localStorage has invalid value', () => {
      localStorage.setItem('llm-gateway-theme', 'invalid')

      initTheme()

      expect(isDark.value).toBe(false)
    })
  })

  describe('toggleTheme', () => {
    it('should toggle isDark from false to true', () => {
      isDark.value = false

      toggleTheme()

      expect(isDark.value).toBe(true)
    })

    it('should toggle isDark from true to false', () => {
      isDark.value = true

      toggleTheme()

      expect(isDark.value).toBe(false)
    })

    it('should save "dark" to localStorage when toggling to dark', () => {
      isDark.value = false

      toggleTheme()

      expect(localStorage.getItem('llm-gateway-theme')).toBe('dark')
    })

    it('should save "light" to localStorage when toggling to light', () => {
      isDark.value = true
      localStorage.setItem('llm-gateway-theme', 'dark')

      toggleTheme()

      expect(localStorage.getItem('llm-gateway-theme')).toBe('light')
    })

    it('should add dark class to document when toggling to dark', () => {
      isDark.value = false
      document.documentElement.classList.remove('dark')

      toggleTheme()

      expect(document.documentElement.classList.contains('dark')).toBe(true)
    })

    it('should remove dark class from document when toggling to light', () => {
      isDark.value = true
      document.documentElement.classList.add('dark')

      toggleTheme()

      expect(document.documentElement.classList.contains('dark')).toBe(false)
    })

    it('should persist theme preference across multiple toggles', () => {
      isDark.value = false

      toggleTheme()
      expect(localStorage.getItem('llm-gateway-theme')).toBe('dark')
      expect(isDark.value).toBe(true)

      toggleTheme()
      expect(localStorage.getItem('llm-gateway-theme')).toBe('light')
      expect(isDark.value).toBe(false)

      toggleTheme()
      expect(localStorage.getItem('llm-gateway-theme')).toBe('dark')
      expect(isDark.value).toBe(true)
    })

    it('should keep DOM and state in sync', () => {
      isDark.value = false

      toggleTheme()

      expect(isDark.value).toBe(true)
      expect(document.documentElement.classList.contains('dark')).toBe(true)

      toggleTheme()

      expect(isDark.value).toBe(false)
      expect(document.documentElement.classList.contains('dark')).toBe(false)
    })
  })

  describe('theme persistence and initialization', () => {
    it('should restore theme from localStorage on init', () => {
      localStorage.setItem('llm-gateway-theme', 'dark')
      isDark.value = false

      initTheme()

      expect(isDark.value).toBe(true)
      expect(document.documentElement.classList.contains('dark')).toBe(true)
    })

    it('should toggle and maintain persistence', () => {
      localStorage.setItem('llm-gateway-theme', 'dark')
      isDark.value = true
      document.documentElement.classList.add('dark')

      toggleTheme()

      expect(isDark.value).toBe(false)
      expect(localStorage.getItem('llm-gateway-theme')).toBe('light')

      isDark.value = false
      document.documentElement.className = ''
      initTheme()

      expect(isDark.value).toBe(false)
    })
  })

  describe('edge cases', () => {
    it('should handle multiple consecutive toggles', () => {
      isDark.value = false

      for (let i = 0; i < 10; i++) {
        toggleTheme()
      }

      expect(isDark.value).toBe(false)
      expect(document.documentElement.classList.contains('dark')).toBe(false)
    })

    it('should maintain state consistency with classList', () => {
      isDark.value = false

      toggleTheme()

      expect(isDark.value).toBe(true)
      expect(document.documentElement.classList.contains('dark')).toBe(true)

      const hasDarkClass = Array.from(document.documentElement.classList).includes('dark')
      expect(hasDarkClass).toBe(true)
    })
  })
})