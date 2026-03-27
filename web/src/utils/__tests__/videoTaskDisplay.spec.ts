import { describe, it, expect } from 'vitest'
import {
  extractPromptFromContent,
  parseVideoRequest,
  resolveGroupAndModel,
  resolveVideoDisplay,
  resolveKeyLabel,
} from '../videoTaskDisplay'

describe('videoTaskDisplay', () => {
  const groupBody = JSON.stringify({
    model: 'doubao-seedance-2-0-260128',
    content: [{ type: 'text', text: '一只橘猫在阳光下缓慢走过' }],
    resolution: '720p',
    ratio: '16:9',
    duration: 5,
  })

  it('extractPromptFromContent joins text parts', () => {
    expect(extractPromptFromContent([
      { type: 'text', text: 'a' },
      { type: 'image_url', image_url: { url: 'x' } },
      { type: 'text', text: 'b' },
    ])).toBe('a\nb')
  })

  it('parseVideoRequest reads prompt and params', () => {
    const p = parseVideoRequest(groupBody)
    expect(p.upstreamModel).toBe('doubao-seedance-2-0-260128')
    expect(p.prompt).toBe('一只橘猫在阳光下缓慢走过')
    expect(p.resolution).toBe('720p')
    expect(p.ratio).toBe('16:9')
    expect(p.duration).toBe('5s')
  })

  it('resolveGroupAndModel: group call stores group in model, upstream in body', () => {
    const gm = resolveGroupAndModel({
      model: 'seedance-video-test',
      request_body: groupBody,
    })
    expect(gm.groupName).toBe('seedance-video-test')
    expect(gm.modelName).toBe('doubao-seedance-2-0-260128')
  })

  it('resolveGroupAndModel: direct upstream has no group', () => {
    const gm = resolveGroupAndModel({
      model: 'doubao-seedance-2-0-260128',
      request_body: groupBody,
    })
    expect(gm.groupName).toBe('')
    expect(gm.modelName).toBe('doubao-seedance-2-0-260128')
  })

  it('resolveGroupAndModel: empty body falls back to task.model', () => {
    const gm = resolveGroupAndModel({ model: 'seedance-video-test' })
    expect(gm.groupName).toBe('')
    expect(gm.modelName).toBe('seedance-video-test')
  })

  it('resolveVideoDisplay prefers response params', () => {
    const d = resolveVideoDisplay({
      model: 'seedance-video-test',
      request_body: groupBody,
      response_body: JSON.stringify({
        model: 'doubao-seedance-2-0-260128',
        resolution: '1080p',
        ratio: '9:16',
        duration: 8,
      }),
    })
    expect(d.groupName).toBe('seedance-video-test')
    expect(d.prompt).toBe('一只橘猫在阳光下缓慢走过')
    expect(d.resolution).toBe('1080p')
    expect(d.ratio).toBe('9:16')
    expect(d.duration).toBe('8s')
  })

  it('resolveKeyLabel prefers name', () => {
    expect(resolveKeyLabel({ api_key_name: 'test', api_key_id: '3b3dece5-059b-4e9c-bbee-0b6e0c4e6ee9' })).toBe('test')
    expect(resolveKeyLabel({ api_key_id: '3b3dece5-059b-4e9c-bbee-0b6e0c4e6ee9' })).toBe('3b3dec···6ee9')
  })
})
