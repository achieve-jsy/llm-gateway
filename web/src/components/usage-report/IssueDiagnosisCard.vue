<template>
  <div
    class="urd"
    :class="`urd--${state}`"
    :title="collapsed ? '点击展开' : '点击收起'"
    @click="collapsed = !collapsed"
  >
    <div class="urd__head">
      <span class="urd__type">{{ typeLabel }}</span>
      <el-tag :type="verdictTag" size="small" effect="light">{{ verdictLabel }}</el-tag>
      <span v-if="!collapsed" class="urd__share">
        请求 {{ pct(issue.request_share) }} · 会话 {{ pct(issue.session_share) }}
      </span>
      <span class="urd__toggle">{{ collapsed ? '▼' : '▲' }}</span>
    </div>

    <div v-if="!collapsed" class="urd__body">
      <div class="urd__explanation">
        <p><strong>发现：</strong>{{ narrative.finding }}</p>
        <p><strong>为什么：</strong>{{ narrative.reason }}</p>
        <p v-if="narrative.semantic_review"><strong>模型复核：</strong>{{ narrative.semantic_review }}</p>
        <p><strong>这意味着：</strong>{{ narrative.impact }}</p>
      </div>

      <div class="urd__grid">
        <div class="urd__cell">
          <span class="urd__cell-label">命中</span>
          <span class="urd__cell-val">{{ issue.request_count }} 请求 / {{ issue.session_count }} 会话</span>
        </div>
        <div class="urd__cell">
          <span class="urd__cell-label">估算多花费用</span>
          <span class="urd__cell-val">{{ fmtCost(issue.attributed_cost_usd) }}</span>
        </div>
        <div v-if="issue.overlap_cost_usd > 0" class="urd__cell">
          <span class="urd__cell-label">重复部分（未重复计）</span>
          <span class="urd__cell-val">{{ fmtCost(issue.overlap_cost_usd) }}</span>
        </div>
        <div class="urd__cell">
          <span class="urd__cell-label">涉及请求总费用</span>
          <span class="urd__cell-val">{{ fmtCost(issue.affected_cost_usd) }}</span>
        </div>
        <div class="urd__cell">
          <span class="urd__cell-label">判断把握</span>
          <span class="urd__cell-val">{{ pct(issue.confidence) }}</span>
        </div>
      </div>

      <div class="urd__advice">
        <strong>建议：</strong>{{ narrative.advice }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import type { UsageIssue, IssueType } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  issue: UsageIssue
  currency?: Currency
}>(), { currency: 'CNY' })

const settingsStore = useSettingsStore()
const cc = computed<CurrencyConfig>(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

// not_hit 默认折叠；其余默认展开
const collapsed = ref(props.issue.verdict === 'not_hit')

const TYPE_LABEL: Record<IssueType, string> = {
  prefix_pollution: '前缀污染（上下文）',
  toothpaste: '挤牙膏（交互）',
  multi_task_session: '多任务不切会话（交互）',
  output_runaway: '输出失控（生成）',
  over_model: '杀鸡用牛刀（选型）',
}

const VERDICT_META: Record<string, { label: string; tag: 'success' | 'warning' | 'info' | 'danger' }> = {
  confirmed: { label: '证据较充分', tag: 'success' },
  suspected: { label: '疑似', tag: 'warning' },
  not_computed: { label: '数据不足', tag: 'info' },
  not_hit: { label: '未命中', tag: 'info' },
}

const typeLabel = computed(() => TYPE_LABEL[props.issue.type] ?? props.issue.type)
const verdictLabel = computed(() => VERDICT_META[props.issue.verdict]?.label ?? props.issue.verdict)
const verdictTag = computed(() => VERDICT_META[props.issue.verdict]?.tag ?? 'info')

// 四态卡片外观：
//   confirmed  实心边框 + 主色淡底
//   suspected  虚边 + 警示淡底
//   not_computed 灰字（虚线弱化）
//   not_hit    折叠（仅头）
const state = computed(() => {
  switch (props.issue.verdict) {
    case 'confirmed': return 'confirmed'
    case 'suspected': return 'suspected'
    case 'not_computed': return 'not_computed'
    default: return 'not_hit'
  }
})

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}
function fmtCost(v: number): string {
  return formatCost(Number(v || 0), cc.value)
}

// 建议文案由渲染层回填模板：只出「定性/疑似/待核查/仅供参考」，绝不出现「违规/违纪/滥用」。
const narrative = computed(() => props.issue.narrative ?? {
  finding: props.issue.verdict === 'not_hit' ? `本期未发现${typeLabel.value}。` : `系统发现可能存在${typeLabel.value}。`,
  reason: props.issue.verdict === 'not_computed' ? '当前数据不足，暂不作判断。' : '系统根据调用次数、Token 与会话特征进行了判断。',
  impact: props.issue.attributed_cost_usd > 0 ? '相关费用已计入问题消耗。' : '本项暂不计入问题消耗。',
  advice: '结合具体任务复核后，再决定是否调整使用方式。',
})
</script>

<style scoped>
.urd {
  border-radius: 10px;
  padding: 10px 12px;
  cursor: pointer;
  border: 1px solid var(--el-border-color-light);
  background: var(--el-bg-color);
  transition: background .15s, border-color .15s;
}
.urd:hover { border-color: var(--el-color-primary-light-5); }

/* confirmed：实心边框 */
.urd--confirmed {
  border: 1.5px solid var(--el-color-success);
  background: var(--el-color-success-light-9);
}
/* suspected：虚边 */
.urd--suspected {
  border: 1.5px dashed var(--el-color-warning);
  background: var(--el-color-warning-light-9);
}
/* not_computed：灰字弱化 */
.urd--not_computed,
.urd--not_hit {
  color: var(--el-text-color-placeholder);
  background: var(--el-fill-color-lighter);
}

.urd__head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.urd__type { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.urd--not_computed .urd__type,
.urd--not_hit .urd__type { color: var(--el-text-color-placeholder); }
.urd__share { font-size: 12px; color: var(--el-text-color-secondary); }
.urd--not_computed .urd__share,
.urd--not_hit .urd__share { color: var(--el-text-color-placeholder); }
.urd__toggle { margin-left: auto; font-size: 11px; color: var(--el-text-color-placeholder); }

.urd__body { margin-top: 8px; display: flex; flex-direction: column; gap: 8px; }
.urd__explanation { display: grid; gap: 5px; }
.urd__explanation p { margin: 0; font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.55; }
.urd__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 6px 12px;
}
.urd__cell { display: flex; flex-direction: column; gap: 2px; }
.urd__cell-label { font-size: 11px; color: var(--el-text-color-placeholder); }
.urd__cell-val { font-size: 13px; color: var(--el-text-color-primary); }
.urd__advice {
  border-top: 1px dashed var(--el-border-color-light);
  padding-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-primary);
}
</style>
