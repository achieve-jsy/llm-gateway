import { describe, it, expect } from 'vitest'
import { maskCredential } from './mask'

describe('maskCredential', () => {
  it('should return **** for empty string', () => {
    expect(maskCredential('')).toBe('****')
  })

  it('should return **** for undefined value', () => {
    expect(maskCredential(undefined as any)).toBe('****')
  })

  it('should return **** for null value', () => {
    expect(maskCredential(null as any)).toBe('****')
  })

  it('should return **** for string with length <= 8', () => {
    expect(maskCredential('short')).toBe('****')
    expect(maskCredential('12345678')).toBe('****')
    expect(maskCredential('1234567')).toBe('****')
  })

  it('should mask string with length 9 correctly', () => {
    expect(maskCredential('123456789')).toBe('123****789')
  })

  it('should mask string with length 10 correctly', () => {
    expect(maskCredential('12345678901')).toBe('123****901')
  })

  it('should mask long api key correctly', () => {
    expect(maskCredential('sk-1234567890123456789')).toBe('sk-****6789')
  })

  it('should mask very long token correctly', () => {
    const longToken = 'abcdefghijklmnopqrstuvwxyz0123456789'
    expect(maskCredential(longToken)).toBe('abc****6789')
  })

  it('should preserve first 3 and last 4 characters exactly', () => {
    const input = 'ABCDEFGHIJabcdefgh'
    expect(maskCredential(input)).toBe('ABC****efgh')
  })

  it('should work with special characters', () => {
    const input = 'sk-proj-!@#$%^&*()'
    expect(maskCredential(input)).toBe('sk-****&*()')
  })

  it('should work with numbers only', () => {
    expect(maskCredential('12345678901234567890')).toBe('123****7890')
  })
})
