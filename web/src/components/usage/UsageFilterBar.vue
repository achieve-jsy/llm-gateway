<template>
  <div class="ufb">
    <div class="ufb-row ufb-row--date">
      <div class="ufb-presets">
        <button
          v-for="p in datePresets"
          :key="p.label"
          type="button"
          class="ufb-preset"
          :class="{ 'ufb-preset--active': activePreset === p.label }"
          @click="emit('applyPreset', p)"
        >{{ p.label }}</button>
      </div>
      <div class="ufb-divider" />
      <el-date-picker
        v-model="dateRange"
        :type="dateMode === 'datetime' ? 'datetimerange' : 'daterange'"
        range-separator="至"
        start-placeholder="开始"
        end-placeholder="结束"
        :value-format="dateMode === 'datetime' ? 'YYYY-MM-DD HH:mm' : 'YYYY-MM-DD'"
        :format="dateMode === 'datetime' ? 'MM/DD HH:mm' : 'MM/DD'"
        class="ufb-date"
        size="small"
        @change="onDatePickerChange"
      />
      <el-button
        v-if="hasMoreFields"
        class="ufb-more-btn"
        link
        size="small"
        @click="showMore = !showMore"
      >
        {{ showMore ? '收起' : '更多' }}
        <el-icon class="ufb-more-icon"><component :is="showMore ? ArrowUp : ArrowDown" /></el-icon>
      </el-button>
      <el-button
        v-if="activeCount > 0"
        size="small"
        plain
        type="warning"
        class="ufb-clear"
        @click="emit('clear')"
      >
        清空 {{ activeCount }}
      </el-button>
    </div>

    <div v-if="$slots.tabs" class="ufb-tabs">
      <slot name="tabs" />
    </div>

    <div class="ufb-row ufb-row--dims">
      <div v-if="fields.group" class="ufb-chip">
        <span class="ufb-label">模型组</span>
        <el-input
          v-model="groupName"
          placeholder="模型组"
          clearable
          size="small"
          class="ufb-input"
          @change="emit('change')"
          @clear="emit('change')"
        />
      </div>
      <div v-if="fields.model" class="ufb-chip">
        <span class="ufb-label">模型</span>
        <el-select
          v-if="modelOptions.length"
          v-model="modelName"
          placeholder="全部"
          clearable
          filterable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="m in modelOptions" :key="m" :label="m" :value="m" />
        </el-select>
        <el-input
          v-else
          v-model="modelName"
          placeholder="模型名"
          clearable
          size="small"
          class="ufb-input"
          @change="emit('change')"
          @clear="emit('change')"
        />
      </div>
      <div v-if="fields.keySelect" class="ufb-chip">
        <span class="ufb-label">Key</span>
        <el-select
          v-model="keyId"
          placeholder="选择 Key"
          clearable
          filterable
          size="small"
          class="ufb-select ufb-select--key"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="k in keyOptions" :key="k.value" :label="k.label" :value="k.value" />
        </el-select>
      </div>
      <div v-if="fields.provider" class="ufb-chip">
        <span class="ufb-label">渠道</span>
        <el-select
          v-model="provider"
          placeholder="全部"
          clearable
          filterable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="p in providerOptions" :key="p" :label="p" :value="p" />
        </el-select>
      </div>
      <div v-if="fields.status" class="ufb-chip">
        <span class="ufb-label">状态</span>
        <el-select
          v-model="status"
          placeholder="全部"
          clearable
          size="small"
          class="ufb-select ufb-select--status"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option
            v-for="o in statusOptions"
            :key="o.value"
            :label="o.label"
            :value="o.value"
          />
        </el-select>
      </div>
      <div v-if="fields.user" class="ufb-chip">
        <span class="ufb-label">用户</span>
        <el-input
          v-model="userName"
          placeholder="用户名"
          clearable
          size="small"
          class="ufb-input"
          @change="emit('change')"
          @clear="emit('change')"
        />
      </div>
      <div v-if="fields.keyType" class="ufb-chip">
        <span class="ufb-label">主体</span>
        <el-select
          v-model="keyType"
          placeholder="全部"
          clearable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option label="成员" value="personal" />
          <el-option label="程序" value="service" />
        </el-select>
      </div>
    </div>

    <div v-show="showMore && hasMoreFields" class="ufb-row ufb-row--more">
      <div v-if="fields.moreModel" class="ufb-chip">
        <span class="ufb-label">模型</span>
        <el-input
          v-model="modelName"
          placeholder="模型名"
          clearable
          size="small"
          class="ufb-input"
          @change="emit('change')"
          @clear="emit('change')"
        />
      </div>
      <div v-if="fields.moreProvider" class="ufb-chip">
        <span class="ufb-label">渠道</span>
        <el-select
          v-model="provider"
          placeholder="全部"
          clearable
          filterable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="p in providerOptions" :key="p" :label="p" :value="p" />
        </el-select>
      </div>
      <div v-if="fields.moreStatus" class="ufb-chip">
        <span class="ufb-label">状态</span>
        <el-select
          v-model="status"
          placeholder="全部"
          clearable
          size="small"
          class="ufb-select ufb-select--status"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option
            v-for="o in statusOptions"
            :key="o.value"
            :label="o.label"
            :value="o.value"
          />
        </el-select>
      </div>
      <div v-if="fields.keyName" class="ufb-chip">
        <span class="ufb-label">Key</span>
        <el-input
          v-model="keyName"
          placeholder="Key 名称"
          clearable
          size="small"
          class="ufb-input"
          @change="emit('change')"
          @clear="emit('change')"
        />
      </div>
      <div v-if="fields.dept" class="ufb-chip">
        <span class="ufb-label">部门</span>
        <el-select
          v-model="deptName"
          placeholder="全部"
          clearable
          filterable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="d in deptOptions" :key="d.value" :label="d.label" :value="d.value" />
        </el-select>
      </div>
      <div v-if="fields.studio" class="ufb-chip">
        <span class="ufb-label">事业部</span>
        <el-select
          v-model="studioId"
          placeholder="全部"
          clearable
          filterable
          size="small"
          class="ufb-select"
          @change="emit('change')"
          @clear="emit('change')"
        >
          <el-option v-for="s in studioOptions" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import {
  filterFieldsFor,
  statusOptionsFor,
  type UsageCapability,
  type UsagePageRole,
} from '@/utils/usageFilterConfig'

defineOptions({ name: 'UsageFilterBar' })

const props = withDefaults(defineProps<{
  pageRole: UsagePageRole
  capability: UsageCapability
  datePresets: Array<{ label: string; days: number }>
  dateMode?: 'date' | 'datetime'
  modelOptions?: string[]
  keyOptions?: Array<{ label: string; value: string }>
  providerOptions?: string[]
  deptOptions?: Array<{ label: string; value: string }>
  studioOptions?: Array<{ label: string; value: string }>
  activeCount?: number
}>(), {
  dateMode: 'date',
  modelOptions: () => [],
  keyOptions: () => [],
  providerOptions: () => [],
  deptOptions: () => [],
  studioOptions: () => [],
  activeCount: 0,
})

const dateRange = defineModel<[string, string] | null>('dateRange', { required: true })
const activePreset = defineModel<string>('activePreset', { required: true })
const groupName = defineModel<string>('groupName', { default: '' })
const modelName = defineModel<string>('modelName', { default: '' })
const keyId = defineModel<string>('keyId', { default: '' })
const keyName = defineModel<string>('keyName', { default: '' })
const keyType = defineModel<string>('keyType', { default: '' })
const status = defineModel<string>('status', { default: '' })
const provider = defineModel<string>('provider', { default: '' })
const userName = defineModel<string>('userName', { default: '' })
const deptName = defineModel<string>('deptName', { default: '' })
const studioId = defineModel<string>('studioId', { default: '' })

const emit = defineEmits<{
  applyPreset: [preset: { label: string; days: number }]
  change: []
  dateChange: []
  clear: []
}>()

const showMore = ref(false)

function onDatePickerChange() {
  activePreset.value = ''
  emit('dateChange')
}

const fields = computed(() => filterFieldsFor(props.pageRole, props.capability))
const statusOptions = computed(() => statusOptionsFor(props.capability))

const hasMoreFields = computed(() => {
  const f = fields.value
  // user / keyType 已常驻；「更多」只收低频项
  return f.moreModel || f.moreProvider || f.moreStatus || f.keyName || f.dept || f.studio
})

watch(() => props.capability, () => {
  // 换 Tab 后「更多」默认收起
  showMore.value = false
})
</script>

<style scoped>
.ufb {
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  margin-bottom: 12px;
}
.ufb-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  padding: 10px 14px;
}
.ufb-row--date {
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.ufb-tabs {
  padding: 8px 14px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.ufb-row--more {
  border-top: 1px dashed var(--el-border-color-lighter);
  background: var(--el-fill-color-blank);
}
.ufb-presets { display: flex; gap: 4px; flex-wrap: wrap; }
.ufb-preset {
  padding: 3px 10px;
  border-radius: 16px;
  border: 1px solid var(--el-border-color);
  background: var(--el-fill-color-light);
  color: var(--el-text-color-regular);
  font-size: 12px;
  cursor: pointer;
  line-height: 1.6;
}
.ufb-preset:hover { border-color: var(--el-color-primary); color: var(--el-color-primary); }
.ufb-preset--active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary);
  color: #fff;
  font-weight: 500;
}
.ufb-divider {
  width: 1px;
  height: 20px;
  background: var(--el-border-color);
  flex-shrink: 0;
}
.ufb-date { width: 220px; flex-shrink: 0; }
.ufb-more-btn { margin-left: auto; }
.ufb-more-icon { margin-left: 2px; }
.ufb-clear { flex-shrink: 0; }
.ufb-chip { display: flex; align-items: center; gap: 6px; }
.ufb-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.ufb-input { width: 130px; }
.ufb-select { width: 120px; }
.ufb-select--key { width: 150px; }
.ufb-select--status { width: 110px; }

@media (max-width: 640px) {
  .ufb-divider { display: none; }
  .ufb-date, .ufb-input, .ufb-select, .ufb-select--key, .ufb-select--status {
    width: 100% !important;
  }
  .ufb-chip { width: 100%; }
  .ufb-more-btn { margin-left: 0; }
}
</style>
