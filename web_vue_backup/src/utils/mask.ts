/**
 * 凭证脱敏：与后端 crypto.Mask 保持一致。
 *
 * 长度规则：
 * - 空值或长度 <= 8：完全屏蔽，返回 ****
 * - 9 <= 长度 <= 11：保留前 3 + 后 3
 * - 长度 >= 12：保留前 3 + 后 4
 */
export function maskCredential(value: string): string {
  if (!value || value.length <= 8) return '****'
  const tail = value.length >= 12 ? 4 : 3
  return value.slice(0, 3) + '****' + value.slice(-tail)
}
