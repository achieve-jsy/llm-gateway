export async function copyToClipboard(text: string): Promise<void> {
  if (typeof text !== 'string' || text.length === 0) {
    throw new Error('clipboard text is empty')
  }

  // 1. Try modern clipboard API first (works on HTTPS or localhost)
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return
    }
  } catch {
    // fall through to fallback
  }

  // 2. Fallback: execCommand('copy')
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '-9999px'
  ta.style.width = '1px'
  ta.style.height = '1px'
  ta.style.opacity = '0'
  ta.readOnly = true
  document.body.appendChild(ta)
  try {
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, ta.value.length)
    const ok = document.execCommand('copy')
    if (!ok) {
      throw new Error('execCommand copy failed')
    }
  } finally {
    document.body.removeChild(ta)
  }
}
