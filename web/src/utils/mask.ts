/**
 * 凭证脱敏：保留前 3 位 + **** + 末尾若干位。
 *
 * 长度规则：
 * - length <= 8：完全屏蔽，返回 ****
 * - 9 <= length <= 11：保留前 3 + 后 3
 * - length >= 12：保留前 3 + 后 4
 */
export function maskCredential(value: string): string {
  if (!value || value.length <= 8) return '****'
  const tail = value.length >= 12 ? 4 : 3
  return value.slice(0, 3) + '****' + value.slice(-tail)
}
