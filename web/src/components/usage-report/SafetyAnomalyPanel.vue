<template>
  <div v-if="anomaly.hit" class="urs">
    <div class="urs__banner">
      <div class="urs__banner-head">
        <el-icon class="urs__icon"><WarningFilled /></el-icon>
        <span class="urs__title">严重异常（待核查）</span>
        <el-tag :type="statusMeta.tag" size="small" effect="light">{{ statusMeta.label }}</el-tag>
      </div>
      <p class="urs__desc">
        {{ interpretation }}
      </p>
    </div>

    <div class="urs__grid">
      <div class="urs__cell">
        <span class="urs__cell-label">未备案非工作时段占比</span>
        <span class="urs__cell-val">{{ pct(anomaly.unrelated_cost_ratio) }}</span>
      </div>
      <div class="urs__cell">
        <span class="urs__cell-label">未备案非工作时段成本</span>
        <span class="urs__cell-val">{{ fmtCost(anomaly.unrelated_cost_usd) }}</span>
      </div>
      <div v-if="anomaly.top_hours?.length" class="urs__cell">
        <span class="urs__cell-label">集中时段</span>
        <span class="urs__cell-val">{{ anomaly.top_hours.join('，') }}</span>
      </div>
      <div v-if="anomaly.top_model" class="urs__cell">
        <span class="urs__cell-label">主要模型</span>
        <span class="urs__cell-val">{{ anomaly.top_model }}</span>
      </div>
      <div v-if="anomaly.top_key" class="urs__cell">
        <span class="urs__cell-label">主要 Key</span>
        <span class="urs__cell-val">{{ displayKey(anomaly.top_key) }}</span>
      </div>
      <div v-if="anomaly.basis" class="urs__cell urs__cell--wide">
        <span class="urs__cell-label">依据</span>
        <span class="urs__cell-val">{{ anomaly.basis }}</span>
      </div>
    </div>

	<div class="urs__reason">
		<p><strong>触发原因：</strong>{{ triggerReason }}</p>
		<p><strong>内容复核：</strong>{{ contentReview }}</p>
		<p><strong>需要核实：</strong>{{ reviewAction }}</p>
	</div>

    <div v-if="excluded.length" class="urs__excluded">
      <span class="urs__excluded-label">已排除因素</span>
      <span class="urs__excluded-val">{{ excluded.join('；') }}</span>
    </div>

    <div class="urs__actions">
      <el-dropdown trigger="click" @command="onCommand">
        <el-button size="small" :disabled="status !== 'pending_review'">
          反馈核查结果 <el-icon><ArrowDown /></el-icon>
        </el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="confirmed_ok">确认无问题</el-dropdown-item>
            <el-dropdown-item command="need_check">需进一步核查</el-dropdown-item>
            <el-dropdown-item command="false_positive" divided>误报</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-tag v-if="status !== 'pending_review'" type="info" size="small" effect="plain">已反馈</el-tag>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { WarningFilled, ArrowDown } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import type { SafetyAnomaly, SafetyAnomalyStatus } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  anomaly: SafetyAnomaly
  currency?: Currency
}>(), { currency: 'CNY' })

const emit = defineEmits<{
  feedback: [status: 'confirmed_ok' | 'need_check' | 'false_positive']
}>()

const settingsStore = useSettingsStore()
const cc = computed<CurrencyConfig>(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const status = computed(() => props.anomaly.status ?? '')

const STATUS_META: Record<SafetyAnomalyStatus, { label: string; tag: string }> = {
  pending_review: { label: '待核查', tag: 'warning' },
  confirmed_ok: { label: '确认无问题', tag: 'success' },
  need_check: { label: '需核查', tag: 'warning' },
  false_positive: { label: '误报', tag: 'info' },
}
const statusMeta = computed(() =>
  STATUS_META[status.value as SafetyAnomalyStatus] ?? { label: '待核查', tag: 'warning' },
)

const excluded = computed(() => props.anomaly.excluded ?? [])

const triggerReason = computed(() => {
	const ratio = pct(props.anomaly.unrelated_cost_ratio)
	const hours = props.anomaly.top_hours?.length ? `，主要集中在 ${props.anomaly.top_hours.join('、')}` : ''
	return `未匹配备案的非工作时段调用占个人总成本 ${ratio}，超过 20% 关注线${hours}。`
})
const contentReview = computed(() => {
	const confidence = props.anomaly.content_confidence
	const confidenceText = confidence ? `（把握度 ${(confidence * 100).toFixed(0)}%）` : ''
	if (props.anomaly.content_signal === 'work_related' || props.anomaly.review_tag === '疑似加班未备案') {
		return `OpenCode 抽查相关请求内容后，判断这些调用更像工作任务${confidenceText}。`
	}
	if (props.anomaly.content_signal === 'not_work_related') {
		return `OpenCode 抽查相关请求内容后，判断这些调用更像私人事务${confidenceText}，仍需人工确认。`
	}
	return '内容复核尚未形成可靠结论，目前只有调用时段和费用占比信号。'
})
const interpretation = computed(() => {
	if (props.anomaly.content_signal === 'work_related' || props.anomaly.review_tag === '疑似加班未备案') {
		return '更可能是正常加班、值班或备案缺失，不代表与工作无关。'
	}
	return '仅凭凌晨或周末使用不能判断用途，也不能直接定责。'
})
const reviewAction = '请确认对应时段是否属于加班、发布值班、跨时区或批处理；如属正常工作，请补充或关联备案。'

function pct(v?: number): string {
  return v === undefined ? '—' : `${(v * 100).toFixed(1)}%`
}
function fmtCost(v?: number): string {
  return v === undefined ? '—' : formatCost(Number(v || 0), cc.value)
}
function displayKey(value?: string): string {
  const key = value?.trim()
  if (!key) return '—'
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
  return uuid.test(key) ? `Key ••••${key.slice(-4)}` : key
}
function onCommand(cmd: string) {
  if (cmd === 'confirmed_ok' || cmd === 'need_check' || cmd === 'false_positive') {
    emit('feedback', cmd)
  }
}
</script>

<style scoped>
.urs {
  display: flex;
  flex-direction: column;
  gap: 10px;
  border: 1px solid var(--el-color-warning);
  border-radius: 12px;
  padding: 12px 14px;
  background: var(--el-color-warning-light-9);
}
.urs__banner-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.urs__icon { color: var(--el-color-warning); font-size: 18px; }
.urs__title { font-size: 15px; font-weight: 600; color: var(--el-text-color-primary); }
.urs__desc {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
.urs__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px 14px;
}
.urs__cell { display: flex; flex-direction: column; gap: 2px; }
.urs__cell--wide { grid-column: 1 / -1; }
.urs__cell-label { font-size: 11px; color: var(--el-text-color-placeholder); }
.urs__cell-val { font-size: 13px; color: var(--el-text-color-primary); word-break: break-all; }
.urs__excluded {
  display: flex; gap: 8px; font-size: 12px;
  border-top: 1px dashed var(--el-border-color-light);
  padding-top: 6px;
}
.urs__reason { display:grid; gap:5px; padding:10px 12px; background:var(--el-bg-color); border:1px solid var(--el-border-color-lighter); border-radius:6px; }
.urs__reason p { margin:0; color:var(--el-text-color-regular); font-size:12px; line-height:1.55; }
.urs__excluded-label { color: var(--el-text-color-placeholder); white-space: nowrap; }
.urs__excluded-val { color: var(--el-text-color-secondary); }
.urs__actions {
  display: flex; align-items: center; gap: 10px;
}
</style>
