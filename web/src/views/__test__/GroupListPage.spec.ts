import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import GroupListPage from '../GroupListPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('@/api/groups', () => ({
  groupsApi: {
    listGroups: vi.fn(),
    deleteGroup: vi.fn(),
    updateGroup: vi.fn(),
    createGroup: vi.fn(),
    listGroupModels: vi.fn(),
    addModelsToGroup: vi.fn(),
    removeModelFromGroup: vi.fn()
  }
}))

import { groupsApi } from '@/api/groups'

const mockedListGroups = vi.mocked(groupsApi.listGroups)
const mockedDeleteGroup = vi.mocked(groupsApi.deleteGroup)
const mockedUpdateGroup = vi.mocked(groupsApi.updateGroup)
const mockedListGroupModels = vi.mocked(groupsApi.listGroupModels)

function mountPage() {
  return mount(GroupListPage, {
    global: {
      stubs: {
        ...elementStubsKebab,
        GroupEditDialog: {
          template: '<div class="group-edit-dialog-stub"></div>',
          emits: ['close', 'saved'],
          props: ['visible', 'group']
        }
      }
    }
  })
}

async function settle(wrapper: any) {
  await flushPromises()
  await wrapper.vm.$nextTick()
}

const groupItems = [
  {
    id: 1,
    name: 'GPT Group',
    strategy: 'round_robin' as const,
    strategy_label: '轮询',
    enabled: true,
    model_count: 3,
    description: 'desc',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z'
  },
  {
    id: 2,
    name: 'Claude Group',
    strategy: 'priority' as const,
    strategy_label: '优先级',
    enabled: false,
    model_count: 5,
    description: '',
    created_at: '2025-06-15T10:30:00Z',
    updated_at: '2025-06-15T10:30:00Z'
  }
]

describe('GroupListPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedListGroups.mockResolvedValue({
      data: {
        data: groupItems,
        total: 2,
        page: 1,
        page_size: 10
      }
    } as any)
    mockedDeleteGroup.mockResolvedValue(undefined as any)
    mockedUpdateGroup.mockResolvedValue({ data: {} } as any)
    mockedListGroupModels.mockResolvedValue({ data: { data: [] } } as any)
  })

  it('renders create button', async () => {
    const wrapper = mountPage()
    await settle(wrapper)
    const buttons = wrapper.findAll('button')
    const createBtn = buttons.find(b => b.text() === '创建模型组')
    expect(createBtn).toBeTruthy()
  })

  it('renders empty state when no groups', async () => {
    mockedListGroups.mockResolvedValue({
      data: {
        data: [],
        total: 0,
        page: 1,
        page_size: 10
      }
    } as any)
    const wrapper = mountPage()
    await settle(wrapper)
    expect(wrapper.find('.el-empty-stub').exists()).toBe(true)
  })

  it('renders loading state (skeleton)', async () => {
    let resolveList: (v: any) => void
    mockedListGroups.mockReturnValue(
      new Promise(resolve => { resolveList = resolve })
    )
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.el-skeleton-stub').exists()).toBe(true)
    resolveList!({ data: { data: [], total: 0, page: 1, page_size: 10 } } as any)
    await settle(wrapper)
  })

  it('renders error state with retry button', async () => {
    mockedListGroups.mockRejectedValue(new Error('fail'))
    const wrapper = mountPage()
    await settle(wrapper)
    expect(wrapper.find('.el-result-stub').exists()).toBe(true)
    const retryBtn = wrapper.find('.el-result-stub').find('button')
    expect(retryBtn.exists()).toBe(true)
    expect(retryBtn.text()).toBe('重新加载')
  })

  it('renders table with group data', async () => {
    const wrapper = mountPage()
    await settle(wrapper)
    expect(wrapper.find('.el-table-stub').exists()).toBe(true)
    const rows = wrapper.findAll('.el-table-row')
    expect(rows.length).toBe(2)
  })

  it('search input exists', async () => {
    const wrapper = mountPage()
    await settle(wrapper)
    const input = wrapper.find('.header-actions').find('input')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toBe('搜索模型组名称')
  })

  it('delete popconfirm triggers delete', async () => {
    const wrapper = mountPage()
    await settle(wrapper)
    const popconfirm = wrapper.find('.el-popconfirm-stub')
    expect(popconfirm.exists()).toBe(true)
    await popconfirm.trigger('click')
    await settle(wrapper)
    expect(mockedDeleteGroup).toHaveBeenCalledWith(1)
  })

  it('pagination renders', async () => {
    const wrapper = mountPage()
    await settle(wrapper)
    expect(wrapper.find('.el-pagination-stub').exists()).toBe(true)
  })
})
