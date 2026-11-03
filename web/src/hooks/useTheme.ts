'use client'

import { useEffect, useState } from 'react'

export function useTheme() {
  const [isDark, setIsDark] = useState(false)

  useEffect(() => {
    const stored = localStorage.getItem('theme')
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    const dark = stored ? stored === 'dark' : prefersDark
    setIsDark(dark)
    applyTheme(dark)
  }, [])

  function applyTheme(dark: boolean) {
    if (dark) {
      document.body.setAttribute('theme-mode', 'dark')
    } else {
      document.body.removeAttribute('theme-mode')
    }
  }

  function toggle() {
    const next = !isDark
    setIsDark(next)
    localStorage.setItem('theme', next ? 'dark' : 'light')
    applyTheme(next)
  }

  return { isDark, toggle }
}
