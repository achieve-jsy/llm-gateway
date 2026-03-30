import { describe, it, expect } from 'vitest'
import {
  countActiveFilters,
  filterFieldsFor,
  isStatusCompatible,
  statusOptionsFor,
  videoModelNameParam,
} from '../usageFilterConfig'

describe('usageFilterConfig', () => {
  it('mine puts common dims on primary, no more row', () => {
    const f = filterFieldsFor('mine', 'chat')
    expect(f.user).toBe(false)
    expect(f.dept).toBe(false)
    expect(f.keySelect).toBe(true)
    expect(f.group).toBe(true)
    expect(f.model).toBe(true)
    expect(f.moreModel).toBe(false)
  })

  it('mine video exposes provider on primary', () => {
    expect(filterFieldsFor('mine', 'video').provider).toBe(true)
    expect(filterFieldsFor('mine', 'chat').provider).toBe(false)
    expect(filterFieldsFor('mine', 'video').moreProvider).toBe(false)
  })

  it('dashboard compact hides user/keyType', () => {
    expect(filterFieldsFor('dashboard', 'embedding').user).toBe(false)
    expect(filterFieldsFor('dashboard', 'chat').user).toBe(true)
    expect(filterFieldsFor('dashboard', 'chat').keyType).toBe(true)
    expect(filterFieldsFor('dashboard', 'video').provider).toBe(true)
  })

  it('cost keeps org in more and provider primary', () => {
    const f = filterFieldsFor('cost', 'chat')
    expect(f.provider).toBe(true)
    expect(f.user).toBe(true)
    expect(f.keyType).toBe(true)
    expect(f.dept).toBe(true)
    expect(f.moreStatus).toBe(true)
    expect(f.status).toBe(false)
  })

  it('status options switch by capability', () => {
    expect(statusOptionsFor('chat').map(o => o.value)).toEqual(['success', 'error'])
    expect(statusOptionsFor('video').map(o => o.value)).toContain('succeeded')
    expect(isStatusCompatible('video', 'success')).toBe(false)
    expect(isStatusCompatible('chat', 'success')).toBe(true)
  })

  it('videoModelNameParam prefers group', () => {
    expect(videoModelNameParam('seedance-video-test', 'doubao-x')).toBe('seedance-video-test')
    expect(videoModelNameParam('', 'doubao-x')).toBe('doubao-x')
    expect(videoModelNameParam()).toBeUndefined()
  })

  it('countActiveFilters', () => {
    expect(countActiveFilters('mine', 'chat', { groupName: 'g', status: 'success' })).toBe(2)
  })
})
