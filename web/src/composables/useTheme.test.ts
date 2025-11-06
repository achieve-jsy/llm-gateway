import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { isDark, initTheme, toggleTheme } from './useTheme'

describe('useTheme Composable', () => {
  beforeEach(() => {
    // Clear localStorage before each test
    localStorage.clear()
    // Reset isDark to false
    isDark.value = false
    // Clear all mocks
    vi.clearAllMocks()
    // Reset DOM classes
    document.documentElement.className = ''
  })

  afterEach(() => {
    localStorage.clear()
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

    it('should follow system preference when localStorage is empty', () => {
      localStorage.removeItem('llm-gateway-theme')

      // Mock matchMedia
      const mockMatchMedia = vi.fn().mockReturnValue({
        matches: true,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn()
      })
      window.matchMedia = mockMatchMedia

      initTheme()

      expect(isDark.value).toBe(true)
    })

    it('should follow system preference for light mode', () => {
      localStorage.removeItem('llm-gateway-theme')

      // Mock matchMedia to return light mode preference
      const mockMatchMedia = vi.fn().mockReturnValue({
        matches: false,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn()
      })
      window.matchMedia = mockMatchMedia

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
      localStorage.removeItem('llm-gateway-theme')

      const mockMatchMedia = vi.fn().mockReturnValue({
        matches: false,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn()
      })
      window.matchMedia = mockMatchMedia

      expect(() => initTheme()).not.toThrow()
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

      toggleTheme() // -> true
      expect(localStorage.getItem('llm-gateway-theme')).toBe('dark')
      expect(isDark.value).toBe(true)

      toggleTheme() // -> false
      expect(localStorage.getItem('llm-gateway-theme')).toBe('light')
      expect(isDark.value).toBe(false)

      toggleTheme() // -> true
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

      // Simulate page reload - reinitialize
      isDark.value = false
      document.documentElement.className = ''
      initTheme()

      expect(isDark.value).toBe(false)
    })
  })

  describe('edge cases', () => {
    it('should handle invalid localStorage values gracefully', () => {
      localStorage.setItem('llm-gateway-theme', 'invalid')

      const mockMatchMedia = vi.fn().mockReturnValue({
        matches: false,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn()
      })
      window.matchMedia = mockMatchMedia

      expect(() => initTheme()).not.toThrow()
    })

    it('should handle multiple consecutive toggles', () => {
      isDark.value = false

      for (let i = 0; i < 10; i++) {
        toggleTheme()
      }

      // After 10 toggles (even number), should be back to false
      expect(isDark.value).toBe(false)
      expect(document.documentElement.classList.contains('dark')).toBe(false)
    })

    it('should maintain state consistency with classList', () => {
      isDark.value = false

      toggleTheme()

      expect(isDark.value).toBe(true)
      expect(document.documentElement.classList.contains('dark')).toBe(true)

      // Manually check that class is present
      const hasDarkClass = Array.from(document.documentElement.classList).includes('dark')
      expect(hasDarkClass).toBe(true)
    })
  })
})
