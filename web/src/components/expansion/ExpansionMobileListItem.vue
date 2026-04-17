<template>
  <article
    data-test="expansion-mobile-item"
    class="mobile-item"
    :class="{ 'is-stale': stale }"
    role="button"
    tabindex="0"
    :aria-label="`${actionLabel} ${subject} 的扩容申请`"
    @click="emit('open')"
    @keydown.enter="emit('open')"
    @keydown.space.prevent="emit('open')"
  >
    <header>
      <label v-if="selectable" class="mobile-select" @click.stop>
        <input
          type="checkbox"
          :checked="selected"
          :aria-label="`选择 ${subject}`"
          @change="emit('select', ($event.target as HTMLInputElement).checked)"
        >
      </label>
      <span class="avatar">{{ subject.slice(0, 1) }}</span>
      <div class="subject">
        <strong>{{ subject }}</strong>
        <small>{{ requesterCaption }}</small>
      </div>
      <el-tag :type="statusType" size="small">{{ STATUS_LABELS[item.status] }}</el-tag>
    </header>

    <div class="change">
      <div>
        <el-tag :type="item.dimension_type === 'tier' ? 'primary' : 'success'" size="small">{{ limitItem }}</el-tag>
        <strong>{{ money(item.current_amount_snapshot) }} → {{ money(targetAmount) }}</strong>
      </div>
      <small>{{ PERIOD_LABELS[item.period_type] }} · 增加 {{ money(increaseAmount) }} · 已用 {{ money(item.used_amount_snapshot) }}</small>
      <small class="risk-copy">{{ riskCopy }}</small>
    </div>

    <footer>
      <div class="meta">
        <el-tag v-if="item.priority === 'urgent'" size="small" type="danger">紧急</el-tag>
        <el-tag size="small" :type="item.duration_type === 'current_period' ? 'warning' : 'success'">{{ durationLabel }}</el-tag>
        <span>{{ LEVEL_LABELS[item.current_level] }}</span>
        <span>{{ formatTime(item.created_at) }}</span>
      </div>
      <el-button link :type="stale ? 'warning' : 'primary'" @click.stop="emit('open')">
        {{ actionLabel }}
      </el-button>
    </footer>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  LEVEL_LABELS,
  PERIOD_LABELS,
  STATUS_LABELS,
  type ExpansionRequest,
  type ExpansionStatus,
} from '@/types/expansion'

const props = withDefaults(defineProps<{
  item: ExpansionRequest
  selectable?: boolean
  selected?: boolean
  showRequester?: boolean
}>(), {
  selectable: false,
  selected: false,
  showRequester: true,
})

const emit = defineEmits<{
  open: []
  select: [selected: boolean]
}>()

const subject = computed(() => {
  if (props.item.scope_name && !isUUIDLike(props.item.scope_name)) return props.item.scope_name
  if (props.item.scope_type === 'member') return requesterName.value
  return scopeCaption.value
})
const scopeCaptionMap: Record<string, string> = {
  member: '个人额度',
  dept_daily: '成员日常预算池',
  dept_service: '程序服务预算池',
  studio: '工作室预算',
  company: '公司预算',
}
const scopeCaption = computed(() => scopeCaptionMap[props.item.scope_type] || props.item.scope_type)
const requesterName = computed(() => {
  if (props.item.requester_name && !isUUIDLike(props.item.requester_name)) return props.item.requester_name
  return props.item.requester_id ? `用户 ${props.item.requester_id.slice(0, 8)}` : '未知申请人'
})
const requesterCaption = computed(() => props.showRequester
  ? `${scopeCaption.value} · 申请人 ${requesterName.value}`
  : scopeCaption.value)
const limitItem = computed(() => props.item.dimension_type === 'tier'
  ? `${props.item.model_tier_name || '模型等级'}额度`
  : '总预算')
const durationLabel = computed(() => props.item.duration_type === 'current_period' ? '临时调整' : '长期调额')
const stale = computed(() =>
  props.item.status === 'precheck_required' || props.item.status === 'pending_reconfirmation',
)
const increaseAmount = computed(() => {
  const current = Number(props.item.current_amount_snapshot)
  const target = targetAmount.value
  return Number.isFinite(current) && Number.isFinite(target) ? Math.max(target - current, 0) : Number.NaN
})
const targetAmount = computed(() => {
  const target = Number(props.item.target_amount_usd)
  if (Number.isFinite(target) && target > 0) return target
  const requested = Number(props.item.requested_amount)
  return Number.isFinite(requested) ? requested : Number.NaN
})
const targetBelowUsed = computed(() => {
  const used = Number(props.item.used_amount_snapshot)
  const target = targetAmount.value
  return Number.isFinite(used) && Number.isFinite(target) && used > target
})
const riskCopy = computed(() => {
  if (targetBelowUsed.value) return '目标低于当前已用，需提高目标'
  return props.item.reason || '未填写原因'
})
const actionLabel = computed(() => stale.value ? '刷新数据' : '查看详情')
const statusType = computed(() => statusTagType(props.item.status))

function statusTagType(status: ExpansionStatus) {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  if (status === 'cancelled' || status === 'expired') return 'info'
  return 'warning'
}

function isUUIDLike(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function money(value: string | number) {
  const number = Number(value)
  return Number.isFinite(number)
    ? `$${number.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
    : '—'
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
</script>

<style scoped>
.mobile-item {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-3);
  background: var(--budget-panel-bg);
  border: 1px solid var(--budget-panel-border);
  border-radius: var(--budget-panel-radius);
  cursor: pointer;
}

.mobile-item.is-stale {
  background: var(--budget-stale-bg);
  border-color: var(--color-warning);
}

header,
footer,
.meta,
.change > div {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

header .subject {
  display: grid;
  min-width: 0;
  flex: 1;
}

.subject strong,
.subject small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.subject small,
.change small,
.meta span {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.risk-copy {
  color: var(--color-warning);
}

.avatar {
  display: grid;
  width: 32px;
  height: 32px;
  flex: none;
  place-items: center;
  color: var(--brand-green-text);
  background: var(--el-color-primary-light-9);
  border-radius: var(--radius-full);
}

.change {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-2);
  background: var(--bg-subtle);
  border-radius: var(--radius-sm);
}

footer {
  justify-content: space-between;
}

.meta {
  flex-wrap: wrap;
}
</style>
