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
        <small>{{ item.requester_name }} · {{ LEVEL_LABELS[item.current_level] }}</small>
      </div>
      <el-tag :type="statusType" size="small">{{ STATUS_LABELS[item.status] }}</el-tag>
    </header>

    <div class="change">
      <div>
        <el-tag v-if="item.model_tier_name" size="small" type="primary">{{ item.model_tier_name }}</el-tag>
        <strong>{{ PERIOD_LABELS[item.period_type] }} {{ money(item.current_amount_snapshot) }} → {{ money(item.target_amount_usd) }}</strong>
      </div>
      <small>{{ stale ? '数据已变化，需重新预检' : `当前已用 ${money(item.used_amount_snapshot)}` }}</small>
    </div>

    <footer>
      <div class="meta">
        <el-tag v-if="item.priority === 'urgent'" size="small" type="danger">紧急</el-tag>
        <el-tag size="small" :type="item.duration_type === 'permanent' ? 'success' : 'primary'">
          {{ item.duration_type === 'permanent' ? '长期' : '本周期' }}
        </el-tag>
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
}>(), {
  selectable: false,
  selected: false,
})

const emit = defineEmits<{
  open: []
  select: [selected: boolean]
}>()

const subject = computed(() => props.item.scope_name || props.item.requester_name || props.item.scope_id)
const stale = computed(() =>
  props.item.status === 'precheck_required' || props.item.status === 'pending_reconfirmation',
)
const actionLabel = computed(() => stale.value ? '重新预检' : '查看详情')
const statusType = computed(() => statusTagType(props.item.status))

function statusTagType(status: ExpansionStatus) {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  if (status === 'cancelled' || status === 'expired') return 'info'
  return 'warning'
}

function money(value: string | number) {
  const number = Number(value)
  return Number.isFinite(number)
    ? `$${number.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
    : '—'
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? value
    : new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
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
