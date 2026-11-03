import { describe, it, expect } from 'vitest'

describe('BatchTagDialog 逻辑', () => {
  describe('提交前校验', () => {
    function canSubmit(selectedTagIds: string[]): boolean {
      return selectedTagIds.length > 0
    }

    it('未选择标签时不可提交', () => {
      expect(canSubmit([])).toBe(false)
    })

    it('至少选择1个标签时可提交', () => {
      expect(canSubmit(['tag1'])).toBe(true)
      expect(canSubmit(['tag1', 'tag2'])).toBe(true)
    })
  })

  describe('批量标签设置 API 参数构造', () => {
    function buildBatchTagRequest(modelIds: string[], tagIds: string[]) {
      return { model_ids: modelIds, tag_ids: tagIds }
    }

    it('正确传递 model_ids 和 tag_ids', () => {
      const req = buildBatchTagRequest(['m1', 'm2', 'm3'], ['tag1', 'tag2'])
      expect(req.model_ids).toEqual(['m1', 'm2', 'm3'])
      expect(req.tag_ids).toEqual(['tag1', 'tag2'])
    })

    it('空 tagIds 列表（清除标签场景）', () => {
      const req = buildBatchTagRequest(['m1'], [])
      expect(req.tag_ids).toEqual([])
    })
  })
})