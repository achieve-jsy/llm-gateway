import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyToClipboard } from '../clipboard'

describe('copyToClipboard', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('execCommand 返回 false 时抛错，避免误提示已复制', async () => {
    vi.stubGlobal('navigator', {})
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    Object.defineProperty(document, 'execCommand', { value: vi.fn(), configurable: true })
    vi.spyOn(document, 'execCommand').mockReturnValue(false)

    await expect(copyToClipboard('sk-test')).rejects.toThrow('execCommand copy failed')
  })

  it('fallback 复制成功时不抛错并清理 textarea', async () => {
    vi.stubGlobal('navigator', {})
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    Object.defineProperty(document, 'execCommand', { value: vi.fn(), configurable: true })
    vi.spyOn(document, 'execCommand').mockReturnValue(true)

    await expect(copyToClipboard('sk-test')).resolves.toBeUndefined()
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('空文本直接失败', async () => {
    await expect(copyToClipboard('')).rejects.toThrow('clipboard text is empty')
  })
})
