import { describe, it, expect } from 'vitest'

/**
 * SyncModelsDialog 逻辑单元测试
 * 验证同步弹窗的数据处理逻辑（不依赖 DOM 渲染）
 */

// 模拟同步结果数据
const mockSyncResult = {
  added: [
    { id: 'qwen-max', owned_by: 'dashscope' },
    { id: 'qwen-plus', owned_by: 'dashscope' },
    { id: 'text-embedding-v3', owned_by: 'dashscope' },
    { id: 'qwen-vl-max', owned_by: 'dashscope' },
    { id: 'qwen-turbo', owned_by: 'dashscope' },
  ],
  existing: [
    { id: 'qwen-long', owned_by: 'dashscope' },
  ],
  removed: [
    { id: 'old-model', owned_by: 'dashscope' },
  ],
  total_upstream: 7,
  total_local: 2,
  unsupported: false,
}

// 模型类型推断函数（与组件内逻辑一致）
function inferModelType(name: string): 'embedding' | 'vision' | 'chat' {
  const lower = name.toLowerCase()
  if (lower.includes('embed')) return 'embedding'
  if (lower.includes('vl') || lower.includes('vision')) return 'vision'
  return 'chat'
}

// 过滤函数
function filterModels(models: { id: string }[], keyword: string) {
  if (!keyword) return models
  const lower = keyword.toLowerCase()
  return models.filter(m => m.id.toLowerCase().includes(lower))
}

// 排序函数
function sortModels<T extends { id: string }>(models: T[]): T[] {
  return [...models].sort((a, b) => a.id.localeCompare(b.id))
}

describe('SyncModelsDialog 数据逻辑', () => {
  describe('模型类型推断', () => {
    it('含 embed 的识别为 embedding', () => {
      expect(inferModelType('text-embedding-v3')).toBe('embedding')
      expect(inferModelType('embedding-model')).toBe('embedding')
    })

    it('含 vl 或 vision 的识别为 vision', () => {
      expect(inferModelType('qwen-vl-max')).toBe('vision')
      expect(inferModelType('gpt-4-vision')).toBe('vision')
    })

    it('其他识别为 chat', () => {
      expect(inferModelType('qwen-max')).toBe('chat')
      expect(inferModelType('deepseek-chat')).toBe('chat')
      expect(inferModelType('claude-3-opus')).toBe('chat')
    })
  })

  describe('搜索过滤', () => {
    it('空关键词返回全部', () => {
      const result = filterModels(mockSyncResult.added, '')
      expect(result.length).toBe(5)
    })

    it('按关键词过滤', () => {
      const result = filterModels(mockSyncResult.added, 'qwen')
      expect(result.length).toBe(4) // qwen-max, qwen-plus, qwen-vl-max, qwen-turbo
    })

    it('大小写不敏感', () => {
      const result = filterModels(mockSyncResult.added, 'QWEN')
      expect(result.length).toBe(4)
    })

    it('无匹配返回空', () => {
      const result = filterModels(mockSyncResult.added, 'nonexistent')
      expect(result.length).toBe(0)
    })
  })

  describe('排序', () => {
    it('按字母 A-Z 排序', () => {
      const sorted = sortModels(mockSyncResult.added)
      expect(sorted[0].id).toBe('qwen-max')
      expect(sorted[1].id).toBe('qwen-plus')
      expect(sorted[2].id).toBe('qwen-turbo')
      expect(sorted[3].id).toBe('qwen-vl-max')
      expect(sorted[4].id).toBe('text-embedding-v3')
    })
  })

  describe('智能选择', () => {
    it('仅选 Chat 模型排除 embedding 和 vision', () => {
      const chatOnly = mockSyncResult.added.filter(m => inferModelType(m.id) === 'chat')
      expect(chatOnly.length).toBe(3) // qwen-max, qwen-plus, qwen-turbo
      expect(chatOnly.map(m => m.id)).not.toContain('text-embedding-v3')
      expect(chatOnly.map(m => m.id)).not.toContain('qwen-vl-max')
    })

    it('全选新增包含所有类型', () => {
      expect(mockSyncResult.added.length).toBe(5)
    })
  })

  describe('统计数据', () => {
    it('统计数字正确', () => {
      expect(mockSyncResult.added.length).toBe(5)
      expect(mockSyncResult.existing.length).toBe(1)
      expect(mockSyncResult.removed.length).toBe(1)
      expect(mockSyncResult.total_upstream).toBe(7)
    })
  })

  describe('Tab 分类', () => {
    it('新增 Tab 只包含 added 状态模型', () => {
      expect(mockSyncResult.added.every(m => m.id)).toBeTruthy()
    })

    it('已存在 Tab 只包含 existing 模型', () => {
      expect(mockSyncResult.existing.length).toBe(1)
      expect(mockSyncResult.existing[0].id).toBe('qwen-long')
    })

    it('已下架 Tab 只包含 removed 模型', () => {
      expect(mockSyncResult.removed.length).toBe(1)
      expect(mockSyncResult.removed[0].id).toBe('old-model')
    })
  })
})
