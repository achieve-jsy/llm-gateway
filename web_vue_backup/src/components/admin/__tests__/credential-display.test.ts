import { describe, it, expect } from 'vitest'

/**
 * 凭证回显逻辑 TDD 测试
 * 定义编辑渠道时凭证字段的行为规范
 */

// 与后端 crypto.Mask 一致的脱敏逻辑
function maskCredential(value: string): string {
  if (!value || value.length <= 8) return '****'
  const tail = value.length >= 12 ? 4 : 3
  return value.slice(0, 3) + '****' + value.slice(-tail)
}

// 判断凭证是否被修改（用于决定是否发送到后端）
function isCredentialModified(current: string, masked: string): boolean {
  // 如果当前值为空 → 未修改（保持原值）
  if (!current) return false
  // 如果当前值等于脱敏值 → 未修改（用户没改）
  if (current === masked) return false
  // 其他情况 → 已修改
  return true
}

// 构造提交时的凭证值
function getCredentialForSubmit(current: string, masked: string): string | undefined {
  if (!isCredentialModified(current, masked)) {
    return undefined // 不传给后端，保持原值
  }
  return current // 传新值
}

describe('凭证回显逻辑', () => {
  const originalKey = 'sk-REDACTED'
  const maskedKey = maskCredential(originalKey) // "sk-****a13f"

  describe('脱敏函数', () => {
    it('正常长度字符串返回前3+****+后4', () => {
      expect(maskCredential('sk-REDACTED')).toBe('sk-****a13f')
    })

    it('短字符串返回 ****', () => {
      expect(maskCredential('short')).toBe('****')
      expect(maskCredential('')).toBe('****')
    })

    it('JSON 格式凭证也能脱敏', () => {
      const json = '{"api_key": "sk-REDACTED"}'
      const masked = maskCredential(json)
      expect(masked).toBe('{"a****3f"}')
    })
  })

  describe('修改判断', () => {
    it('空值视为未修改', () => {
      expect(isCredentialModified('', maskedKey)).toBe(false)
    })

    it('值等于脱敏值视为未修改', () => {
      expect(isCredentialModified(maskedKey, maskedKey)).toBe(false)
    })

    it('输入新值视为已修改', () => {
      expect(isCredentialModified('sk-new-key-12345', maskedKey)).toBe(true)
    })

    it('输入原始明文也视为已修改（后端会重新加密）', () => {
      expect(isCredentialModified(originalKey, maskedKey)).toBe(true)
    })
  })

  describe('提交值构造', () => {
    it('未修改时返回 undefined（不传给后端）', () => {
      expect(getCredentialForSubmit('', maskedKey)).toBeUndefined()
      expect(getCredentialForSubmit(maskedKey, maskedKey)).toBeUndefined()
    })

    it('已修改时返回新值', () => {
      expect(getCredentialForSubmit('sk-new-key', maskedKey)).toBe('sk-new-key')
    })
  })

  describe('编辑流程场景', () => {
    it('场景1: 打开编辑 → 不改凭证 → 保存 → 凭证不变', () => {
      const displayValue = maskedKey // 打开时显示脱敏值
      const submitValue = getCredentialForSubmit(displayValue, maskedKey)
      expect(submitValue).toBeUndefined() // 不传凭证
    })

    it('场景2: 打开编辑 → 清空凭证框 → 保存 → 凭证不变', () => {
      const displayValue = '' // 用户清空了
      const submitValue = getCredentialForSubmit(displayValue, maskedKey)
      expect(submitValue).toBeUndefined() // 空值不传
    })

    it('场景3: 打开编辑 → 输入新 Key → 保存 → 凭证更新', () => {
      const displayValue = '{"api_key": "sk-brand-new-key"}'
      const submitValue = getCredentialForSubmit(displayValue, maskedKey)
      expect(submitValue).toBe('{"api_key": "sk-brand-new-key"}')
    })
  })
})
