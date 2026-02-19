/**
 * usePanelManager.ts — 面板展开/收起状态管理
 *
 * 核心规则：
 * - 同时只展开一个面板（互斥）
 * - 新面板展开时自动收起旧面板
 * - 支持交叉跳转：用户详情面板点模型 → 切换到模型详情面板
 * - 提供 Transition 组件用的 key 以触发 slide 动画
 */
import { ref, computed } from 'vue'
import type { ActivePanel, PanelType } from '@/types/dashboard-enhanced'

export function usePanelManager() {
  const activePanel = ref<ActivePanel | null>(null)

  /** 当前展开的面板类型（用于 v-if 条件渲染） */
  const userType = computed(() =>
    activePanel.value?.type === 'user' ? activePanel.value : null
  )
  const modelType = computed(() =>
    activePanel.value?.type === 'model' ? activePanel.value : null
  )

  /** 是否有面板处于展开状态 */
  const hasPanel = computed(() => activePanel.value !== null)

  /** 当前展开的用户 ID（快捷访问） */
  const activeUserId = computed(() =>
    activePanel.value?.type === 'user' ? activePanel.value.id : null
  )

  /** 当前展开的模型名（快捷访问） */
  const activeModelName = computed(() =>
    activePanel.value?.type === 'model' ? activePanel.value.id : null
  )

  /**
   * 展开或切换面板
   * - 点击同一个面板 → 收起
   * - 点击不同面板 → 切换
   */
  function togglePanel(type: PanelType, id: string, name: string) {
    const current = activePanel.value
    if (current && current.type === type && current.id === id) {
      // 点击同一个 → 收起
      activePanel.value = null
    } else {
      // 展开新的（自动替换旧的）
      activePanel.value = { type, id, name }
    }
  }

  /** 关闭当前面板 */
  function closePanel() {
    activePanel.value = null
  }

  /**
   * 交叉跳转：从用户详情面板跳到模型详情
   * 无论当前状态如何，都展开模型面板
   */
  function jumpToModel(modelName: string) {
    activePanel.value = { type: 'model', id: modelName, name: modelName }
  }

  /**
   * 交叉跳转：从模型详情面板跳到用户详情
   */
  function jumpToUser(userId: string, userName: string) {
    activePanel.value = { type: 'user', id: userId, name: userName }
  }

  return {
    activePanel,
    userType,
    modelType,
    hasPanel,
    activeUserId,
    activeModelName,
    togglePanel,
    closePanel,
    jumpToModel,
    jumpToUser,
  }
}
