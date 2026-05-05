<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    :size="size ?? '800px'"
    destroy-on-close
    class="budget-edit-drawer"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="bed-header">
        <div class="bed-header-info">
          <span class="bed-title">配置 · {{ title }}</span>
          <span class="bed-sub">
            {{
              selectedAllocation?.scope_type === 'member'
                ? '成员额度（含等级子预算）'
                : scopeType === 'studio' ? '工作室月预算' : '部门预算'
            }}
            <template v-if="selectedAllocation">
              · 已用 {{ formatHeaderUsed }} / {{ formatHeaderLimit }}
            </template>
          </span>
        </div>
      </div>
    </template>

    <div v-if="loading" v-loading="true" class="bed-loading" />
    <template v-else>
      <TierLimitsEditor
        v-if="selectedAllocation && draft"
        ref="editorRef"
        v-model="draft"
        :allocation="selectedAllocation"
        :tiers="tiers"
        :limits="limits"
        :usages="usages"
        :disabled="!canEdit || saving"
        @dirty-change="dirty = $event"
      />
      <el-empty v-else description="当前范围暂无可编辑预算" />
    </template>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button
        v-if="selectedAllocation && canEdit"
        type="primary"
        :loading="saving"
        :disabled="!dirty"
        @click="save"
      >
        预览并保存
      </el-button>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, toRaw, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import TierLimitsEditor from './TierLimitsEditor.vue'
import {
  adjustBudget,
  applyBudgetTierBatch,
  getBudgetList,
  getBudgetTierUsage,
  listBudgetTierLimits,
  listModelTiers,
  previewBudgetAdjustment,
  previewBudgetTierBatch,
} from '@/services/control-plane'
import type { BudgetItem } from '@/types/budget'
import type { ExpansionPeriod } from '@/types/expansion'
import type {
  BudgetTierLimit,
  BudgetTierUsage,
  ModelTier,
  TierLimitsDraft,
} from '@/types/model-tier'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'

const props = defineProps<{
  modelValue: boolean
  scopeType: 'studio' | 'dept'
  scopeId: string
  title: string
  /** studio 模式下子部门 ID 列表；不传时自动从 API 加载 */
  childScopeIds?: string[]
  canEdit?: boolean
  canEditSelf?: boolean
  size?: string
  initialAllocation?: BudgetItem
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  navigate: [payload: { scopeType: 'dept' | 'member'; scopeId: string; label: string }]
  createMemberRequested: [payload: { deptId: string }]
  saved: []
}>()

const periods: ExpansionPeriod[] = ['daily', 'weekly', 'monthly']
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)
const resolvedChildIds = ref<string[]>(props.childScopeIds ?? [])
const selectedAllocation = ref<BudgetItem | null>(null)
const tiers = ref<ModelTier[]>([])
const limits = ref<BudgetTierLimit[]>([])
const usages = ref<BudgetTierUsage[]>([])
const draft = ref<TierLimitsDraft | null>(null)
const originalDraft = ref<TierLimitsDraft | null>(null)
const editorRef = ref<{ openAdvanced?: () => void } | null>(null)

const formatHeaderUsed = computed(() => formatBudgetUSD(selectedAllocation.value?.used_usd ?? 0))
const formatHeaderLimit = computed(() => (
  formatBudgetUSD(selectedAllocation.value?.amount_usd, { unlimited: true })
))

watch(
  () => [props.modelValue, props.scopeType, props.scopeId] as const,
  async ([visible]) => {
    if (!visible) return
    await load()
  },
  { immediate: true },
)

async function load(): Promise<void> {
  loading.value = true
  try {
    const items = await getBudgetList(
      undefined,
      props.scopeId,
      props.scopeType === 'studio' ? 'studio' : 'department',
    )
    const initial = findInitialAllocation(items)
    await loadAllocation(initial, items)
  } catch (error: any) {
    ElMessage.error(error?.message || '预算配置加载失败')
  } finally {
    loading.value = false
  }
}

function findInitialAllocation(items: BudgetItem[]): BudgetItem | null {
  if (props.initialAllocation) return props.initialAllocation
  if (props.scopeType === 'studio') {
    return items.find(item => item.scope_type === 'studio' && item.scope_id === props.scopeId)
      ?? items.find(item => item.scope_type === 'dept_daily')
      ?? items[0]
      ?? null
  }
  return items.find(item => item.scope_type === 'dept_daily' && item.scope_id === props.scopeId)
    ?? items.find(item => item.scope_type === 'dept_service' && item.scope_id === props.scopeId)
    ?? items[0]
    ?? null
}

async function selectAllocation(item: BudgetItem): Promise<void> {
  loading.value = true
  try {
    await loadAllocation(item)
  } catch (error: any) {
    ElMessage.error(error?.message || '预算配置加载失败')
  } finally {
    loading.value = false
  }
}

async function loadAllocation(item: BudgetItem | null, knownItems?: BudgetItem[]): Promise<void> {
  const allocationId = item?.allocation_id
  const [nextItems, nextTiers, nextLimits, nextUsages] = await Promise.all([
    knownItems
      ? Promise.resolve(knownItems)
      : getBudgetList(
          undefined,
          props.scopeId,
          props.scopeType === 'studio' ? 'studio' : 'department',
        ),
    listModelTiers(false),
    allocationId ? listBudgetTierLimits(allocationId) : Promise.resolve([]),
    item
      ? getBudgetTierUsage({ scope_type: item.scope_type, scope_id: item.scope_id, period_type: 'monthly' })
      : Promise.resolve([]),
  ])

  const refreshedItem = item
    ? nextItems.find(candidate => (
        (item.allocation_id && candidate.allocation_id === item.allocation_id)
        || (
          candidate.scope_type === item.scope_type
          && candidate.scope_id === item.scope_id
        )
      )) ?? item
    : null
  resolvedChildIds.value = resolveChildIds(nextItems)
  selectedAllocation.value = refreshedItem
  tiers.value = nextTiers
  limits.value = nextLimits
  usages.value = nextUsages
  draft.value = refreshedItem ? createDraft(refreshedItem, nextTiers, nextLimits) : null
  originalDraft.value = draft.value ? structuredClone(toRaw(draft.value)) : null
  dirty.value = false
}

function resolveChildIds(items: BudgetItem[]): string[] {
  if (props.childScopeIds?.length) return props.childScopeIds
  if (props.scopeType !== 'studio') return []
  return Array.from(new Set(
    items
      .filter(item => item.scope_type === 'dept_daily' || item.scope_type === 'dept_service')
      .map(item => item.scope_id),
  ))
}

function createDraft(
  allocation: BudgetItem,
  modelTiers: ModelTier[],
  tierLimits: BudgetTierLimit[],
): TierLimitsDraft {
  const total: TierLimitsDraft['total'] = {
    daily: totalEntry(allocation.daily_amount_usd),
    weekly: totalEntry(allocation.weekly_amount_usd),
    monthly: totalEntry(allocation.amount_usd),
  }
  const tierDrafts: TierLimitsDraft['tiers'] = {}

  for (const tier of modelTiers) {
    tierDrafts[tier.id] = {
      daily: tierEntry(tier, tierLimits, 'daily'),
      weekly: tierEntry(tier, tierLimits, 'weekly'),
      monthly: tierEntry(tier, tierLimits, 'monthly'),
    }
  }

  return {
    total,
    tiers: tierDrafts,
    effectiveMode: 'immediate',
    reason: '管理员调整预算',
  }
}

function totalEntry(value: number): TierLimitsDraft['total'][ExpansionPeriod] {
  // API: -1 表示不限；缺失/NaN 才是未配置
  if (!Number.isFinite(value)) {
    return { configured: false, value: '' }
  }
  if (value < 0) {
    return { configured: true, value: '-1' }
  }
  return { configured: true, value: String(value) }
}

function tierEntry(
  tier: ModelTier,
  tierLimits: BudgetTierLimit[],
  period: ExpansionPeriod,
): TierLimitsDraft['tiers'][string][ExpansionPeriod] {
  const limit = tierLimits.find(item => (
    item.model_tier_id === tier.id && item.period_type === period
  ))
  const limitValue = limit?.base_limit_usd ?? limit?.limit_usd
  return {
    enabled: limit?.enabled ?? tier.enabled,
    configured: Boolean(limit) && Number.isFinite(Number(limitValue)),
    value: Number.isFinite(Number(limitValue)) ? String(Number(limitValue)) : '',
  }
}

function isMoney(value: string): boolean {
  return /^\d+(?:\.\d{1,6})?$/.test(value) && Number.isFinite(Number(value))
}

function isLimitValue(value: string): boolean {
  // -1 = 不限
  if (value === '-1') return true
  return isMoney(value)
}

async function save(): Promise<void> {
  if (!selectedAllocation.value || !draft.value || !originalDraft.value) return
  if (!draft.value.reason.trim()) {
    editorRef.value?.openAdvanced?.()
    ElMessage.warning('请填写调整原因')
    return
  }

  const unsupportedTotalRemovals = collectUnsupportedTotalRemovals(draft.value, originalDraft.value)
  if (unsupportedTotalRemovals.length && draft.value.effectiveMode === 'next_period') {
    ElMessage.warning(`总预算改为不限仅支持「立即生效」：${unsupportedTotalRemovals.join('、')}`)
    return
  }

  const changedTotals = periods.filter(period => totalPeriodChanged(draft.value!, originalDraft.value!, period))
  const changedTierItems = collectChangedTierItems(draft.value, originalDraft.value)
  const clearedTierItems = collectClearedTierItems(draft.value, originalDraft.value)

  if (
    changedTotals.some(period => {
      const entry = draft.value!.total[period]
      if (!entry.configured) return false
      return !isLimitValue(entry.value)
    })
    || changedTierItems.some(item => !isMoney(item.value))
  ) {
    ElMessage.warning('额度必须是最多 6 位小数的非负数字，或不限（-1）')
    return
  }
  if ((changedTierItems.length || clearedTierItems.length) && !selectedAllocation.value.allocation_id && !changedTotals.length) {
    ElMessage.warning('请先保存总预算，生成 allocation 后再设置等级子预算')
    return
  }
  if (!changedTotals.length && !changedTierItems.length && !clearedTierItems.length) {
    ElMessage.info('没有需要保存的额度变更')
    return
  }

  saving.value = true
  try {
    // 新建成员额度时尚无 allocation_id：必须先落总预算，再写等级子预算
    const bootstrapAllocation = Boolean(
      changedTierItems.length && !selectedAllocation.value.allocation_id && changedTotals.length,
    )

    if (bootstrapAllocation) {
      const totalPreviews = await Promise.all(changedTotals.map(period => (
        previewBudgetAdjustment(totalPayload(period))
      )))
      if (totalPreviews.some(item => item.stale)) {
        ElMessage.warning('用量数据已过期，请刷新后重新预览')
        return
      }
      await confirmPreview(totalPreviews, null)
      for (const [index, period] of changedTotals.entries()) {
        await adjustBudget({
          ...totalPayload(period),
          preview_version: totalPreviews[index].preview_version,
          confirm_zero_block: totalPreviews[index].zero_block,
          confirm_below_used: totalPreviews[index].below_used,
          confirm_replace_schedule: totalPreviews[index].replaces_pending_schedule,
        })
      }

      const items = await getBudgetList(
        undefined,
        props.scopeId,
        props.scopeType === 'studio' ? 'studio' : 'department',
      )
      const created = items.find(candidate => (
        candidate.scope_type === selectedAllocation.value!.scope_type
        && candidate.scope_id === selectedAllocation.value!.scope_id
      ))
      if (!created?.allocation_id) {
        throw new Error('总预算已保存，但未拿到 allocation_id，请刷新后继续配置等级子预算')
      }
      selectedAllocation.value = created

      const tierItems = collectChangedTierItems(draft.value, originalDraft.value)
      if (tierItems.length) {
        const tierPreview = await previewBudgetTierBatch({
          operation: 'set_fixed',
          items: tierItems,
          effective_mode: draft.value.effectiveMode,
          reason: draft.value.reason.trim(),
        })
        if (tierPreview.stale || tierPreview.items.some(item => item.stale)) {
          ElMessage.warning('用量数据已过期，请刷新后重新预览')
          return
        }
        const invalidBootstrapTier = tierPreview.items.find(item => item.status !== 'valid')
        if (invalidBootstrapTier) {
          throw new Error(invalidBootstrapTier.error_code || '等级子预算预览失败')
        }
        await confirmPreview([], tierPreview)
        const tierApply = await applyBudgetTierBatch({
          preview_token: tierPreview.preview_token,
          confirm_item_ids: tierPreview.items
            .filter(item => item.status === 'valid')
            .map(item => item.item_id),
        })
        const failed = tierApply.items?.find(item => item.status !== 'applied' && item.status !== 'skipped')
        if (failed) {
          throw new Error(failed.error_code || '等级子预算保存失败')
        }
      }

      ElMessage.success('成员总预算与等级子预算已更新')
      emit('saved')
      emit('update:modelValue', false)
      return
    }

    const totalPreviews = await Promise.all(changedTotals.map(period => (
      previewBudgetAdjustment(totalPayload(period))
    )))
    const clearPreview = clearedTierItems.length
      ? await previewBudgetTierBatch({
          operation: 'clear',
          items: clearedTierItems,
          effective_mode: draft.value.effectiveMode,
          reason: draft.value.reason.trim(),
        })
      : null
    const tierPreview = changedTierItems.length
      ? await previewBudgetTierBatch({
          operation: 'set_fixed',
          items: changedTierItems,
          effective_mode: draft.value.effectiveMode,
          reason: draft.value.reason.trim(),
        })
      : null

    if (
      totalPreviews.some(item => item.stale)
      || clearPreview?.stale
      || clearPreview?.items.some(item => item.stale)
      || tierPreview?.stale
      || tierPreview?.items.some(item => item.stale)
    ) {
      ElMessage.warning('用量数据已过期，请刷新后重新预览')
      return
    }
    const invalidClearItem = clearPreview?.items.find(item => item.status !== 'valid')
    if (invalidClearItem) {
      throw new Error(invalidClearItem.error_code || '移除等级限制预览失败')
    }
    const invalidTierItem = tierPreview?.items.find(item => item.status !== 'valid')
    if (invalidTierItem) {
      throw new Error(invalidTierItem.error_code || '等级子预算预览失败')
    }

    await confirmPreview(totalPreviews, tierPreview, clearPreview)
    if (clearPreview) {
      const clearApply = await applyBudgetTierBatch({
        preview_token: clearPreview.preview_token,
        confirm_item_ids: clearPreview.items
          .filter(item => item.status === 'valid')
          .map(item => item.item_id),
      })
      const failed = clearApply.items?.find(item => item.status !== 'applied' && item.status !== 'skipped')
      if (failed) {
        throw new Error(failed.error_code || '移除等级限制失败')
      }
    }
    if (tierPreview) {
      const tierApply = await applyBudgetTierBatch({
        preview_token: tierPreview.preview_token,
        confirm_item_ids: tierPreview.items
          .filter(item => item.status === 'valid')
          .map(item => item.item_id),
      })
      const failed = tierApply.items?.find(item => item.status !== 'applied' && item.status !== 'skipped')
      if (failed) {
        throw new Error(failed.error_code || '等级子预算保存失败')
      }
    }
    for (const [index, period] of changedTotals.entries()) {
      await adjustBudget({
        ...totalPayload(period),
        preview_version: totalPreviews[index].preview_version,
        confirm_zero_block: totalPreviews[index].zero_block,
        confirm_below_used: totalPreviews[index].below_used,
        confirm_replace_schedule: totalPreviews[index].replaces_pending_schedule,
      })
    }

    ElMessage.success('总预算与等级子预算已更新')
    emit('saved')
    emit('update:modelValue', false)
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.message || '预算保存失败')
  } finally {
    saving.value = false
  }
}

function totalPayload(period: ExpansionPeriod) {
  const entry = draft.value!.total[period]
  let targetLimit = ''
  if (!entry.configured) {
    targetLimit = '-1.000000'
  } else {
    const numValue = Number(entry.value)
    targetLimit = Number.isFinite(numValue) && entry.value !== '' ? numValue.toFixed(6) : ''
  }

  return {
    scope_type: selectedAllocation.value!.scope_type,
    scope_id: selectedAllocation.value!.scope_id,
    period_type: period,
    target_limit_usd: targetLimit,
    effective_mode: draft.value!.effectiveMode,
    reason: draft.value!.reason.trim(),
  }
}

function totalPeriodChanged(current: TierLimitsDraft, original: TierLimitsDraft, period: ExpansionPeriod): boolean {
  const next = current.total[period]
  const before = original.total[period]
  if (!before.configured && !next.configured) return false
  if (!next.configured && before.configured) return true
  if (next.configured !== before.configured) return true
  return next.configured && next.value !== before.value
}

function collectChangedTierItems(current: TierLimitsDraft, original: TierLimitsDraft) {
  const allocationId = selectedAllocation.value?.allocation_id ?? ''
  return tiers.value.flatMap(tier => periods.flatMap(period => {
    const next = current.tiers[tier.id][period]
    const before = original.tiers[tier.id][period]
    if (
      !next.configured
      || (next.configured === before.configured && next.value === before.value)
    ) {
      return []
    }

    // 验证数值有效性
    const numValue = Number(next.value)
    if (!Number.isFinite(numValue) || next.value === '') {
      console.warn(`[BudgetEditDrawer] Invalid tier value for ${tier.id}/${period}:`, next.value)
      return []
    }

    return [{
      item_id: `${allocationId}:${tier.id}:${period}`,
      allocation_id: allocationId,
      model_tier_id: tier.id,
      period_type: period,
      value: numValue.toFixed(6),
    }]
  }))
}

function collectClearedTierItems(current: TierLimitsDraft, original: TierLimitsDraft) {
  const allocationId = selectedAllocation.value?.allocation_id ?? ''
  return tiers.value.flatMap(tier => periods.flatMap(period => {
    const next = current.tiers[tier.id][period]
    const before = original.tiers[tier.id][period]
    if (!before.configured || next.configured) return []
    return [{
      item_id: `${allocationId}:${tier.id}:${period}:clear`,
      allocation_id: allocationId,
      model_tier_id: tier.id,
      period_type: period,
    }]
  }))
}

function collectUnsupportedTotalRemovals(
  current: TierLimitsDraft,
  original: TierLimitsDraft,
): string[] {
  const removals: string[] = []
  const periodLabels: Record<ExpansionPeriod, string> = {
    daily: '日',
    weekly: '周',
    monthly: '月',
  }
  for (const period of periods) {
    if (original.total[period].configured && !current.total[period].configured) {
      removals.push(`${periodLabels[period]}总预算`)
    }
  }
  return removals
}

async function confirmPreview(
  totalPreviews: Awaited<ReturnType<typeof previewBudgetAdjustment>>[],
  tierPreview: Awaited<ReturnType<typeof previewBudgetTierBatch>> | null,
  clearPreview: Awaited<ReturnType<typeof previewBudgetTierBatch>> | null = null,
): Promise<void> {
  const allTierItems = [...(tierPreview?.items ?? []), ...(clearPreview?.items ?? [])]
  const warnings: string[] = []
  if (clearPreview?.items.some(item => item.status === 'valid')) {
    warnings.push(draft.value?.effectiveMode === 'next_period'
      ? '下周期将移除等级子预算限制'
      : '移除等级限制后该等级将只受总预算约束')
  }
  if (totalPreviews.some(item => item.zero_block) || allTierItems.some(item => item.zero_block)) {
    warnings.push('$0 将阻断后续请求')
  }
  if (totalPreviews.some(item => item.below_used) || allTierItems.some(item => item.below_used)) {
    warnings.push('新额度低于当前已用量')
  }
  if (totalPreviews.some(item => item.replaces_pending_schedule)) {
    warnings.push('将替换已有下周期排期')
  }
  await ElMessageBox.confirm(
    warnings.length ? `${warnings.join('；')}。是否继续？` : '预览完成，确认应用预算调整？',
    warnings.length ? '确认预算风险' : '确认预算调整',
    { type: warnings.length ? 'warning' : 'info' },
  )
}
</script>

<style scoped>
.bed-header {
  display: flex;
  align-items: center;
  width: 100%;
}
.bed-header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.bed-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.bed-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
:deep(.budget-edit-drawer .el-drawer__body) {
  padding: 20px 24px;
  overflow-y: auto;
}
.bed-loading {
  height: 240px;
}
</style>
