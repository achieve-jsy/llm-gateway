<template>
  <!-- 成员配置三卡：配额 / 预算规则 / 模型授权（原型图3 行展开） -->
  <div class="mcfg">
    <div class="mcfg-cards">
      <!-- 配额 -->
      <div class="mcfg-card">
        <div class="mcfg-card__head">
          <span class="mcfg-card__title">配额</span>
          <el-tag size="small" :type="quotaTag" effect="plain">{{ quotaTagLabel }}</el-tag>
        </div>
        <div class="mcfg-card__value">{{ quotaText(quota) }}</div>
        <div class="mcfg-card__sub">RPM / 并发上限</div>
        <el-button size="small" data-test="cfg-quota" @click="$emit('edit-quota')">调整配额</el-button>
      </div>

      <!-- 预算规则 -->
      <div class="mcfg-card">
        <div class="mcfg-card__head">
          <span class="mcfg-card__title">预算规则</span>
          <el-tag size="small" :type="budgetTag" effect="plain">{{ budgetTagLabel }}</el-tag>
        </div>
        <div class="mcfg-card__value">{{ budgetText(budget) }}</div>
        <div class="mcfg-card__sub">月 / 日 / 周额度</div>
        <el-button size="small" data-test="cfg-budget" @click="$emit('manage-budget')">管理规则</el-button>
      </div>

      <!-- 模型授权 -->
      <div class="mcfg-card">
        <div class="mcfg-card__head">
          <span class="mcfg-card__title">模型授权</span>
          <el-tag size="small" :type="entitlementTag" effect="plain">{{ entitlementTagLabel }}</el-tag>
        </div>
        <div class="mcfg-card__value">{{ entitlementText(entitlement) }}</div>
        <div class="mcfg-card__sub">继承工作室 / 团队的模型组</div>
        <el-button size="small" data-test="cfg-entitlement" @click="$emit('manage-entitlement')">管理授权</el-button>
      </div>
    </div>
    <p class="mcfg-hint">来自团队默认配额，调整后单独生效</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { budgetText, entitlementText, isQuotaOverride, quotaText } from './memberFormat'
import type { BudgetLike, EntitlementLike, QuotaLike } from './memberFormat'

const props = defineProps<{
  quota?: QuotaLike | null
  budget?: BudgetLike | null
  entitlement?: EntitlementLike | null
}>()

defineEmits<{
  (e: 'edit-quota'): void
  (e: 'manage-budget'): void
  (e: 'manage-entitlement'): void
}>()

const quotaTag = computed<'success' | 'info'>(() => (isQuotaOverride(props.quota) ? 'success' : 'info'))
const quotaTagLabel = computed(() => (isQuotaOverride(props.quota) ? '已单独设置' : '继承'))

const budgetTag = computed<'success' | 'info'>(() => (props.budget?.source === 'member' ? 'success' : 'info'))
const budgetTagLabel = computed(() => {
  const source = props.budget?.source
  if (source === 'member') return '成员单独设置'
  if (source === 'team') return '继承团队'
  return '未设置'
})

const entitlementTag = computed<'success' | 'info'>(() => ((props.entitlement?.group_count ?? 0) > 0 ? 'success' : 'info'))
const entitlementTagLabel = computed(() => ((props.entitlement?.group_count ?? 0) > 0 ? '可用' : '未授权'))
</script>

<style scoped>
.mcfg { padding: 8px 16px 12px 48px; }
.mcfg-cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(180px, 1fr));
  gap: 12px;
}
.mcfg-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
}
.mcfg-card__head { display: flex; align-items: center; gap: 8px; }
.mcfg-card__title { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.mcfg-card__value { font-size: 15px; font-weight: 600; color: var(--el-text-color-primary); }
.mcfg-card__sub { font-size: 12px; color: var(--el-text-color-placeholder); }
.mcfg-hint {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
@media (max-width: 900px) {
  .mcfg { padding-left: 16px; }
  .mcfg-cards { grid-template-columns: 1fr; }
}
</style>
