<template>
  <div class="urq">
    <!-- 真实会话覆盖率不足 → 判定为疑似（红线：只出"待核查/仅供参考"） -->
    <el-alert
      v-if="lowCoverage"
      type="warning"
      :closable="false"
      show-icon
      title="会话数据不足，判定为疑似"
      :description="lowCoverageNote"
      role="status"
    />

    <div class="urq__tags">
      <span v-for="item in coverageItems" :key="item.key" class="urq__item">
        <span class="urq__label">{{ item.label }}</span>
        <el-tag :type="tagType(item.value)" size="small" effect="light">
          {{ pct(item.value) }}
        </el-tag>
      </span>
    </div>

    <p v-if="!lowCoverage" class="urq__hint">
      全部覆盖率达标，判定结果可直接采信；任一覆盖率偏低时，对应情况会降级为「疑似」。
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { DataQuality } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  dataQuality: DataQuality
}>(), {})

// 覆盖率项：key 对应 DataQuality 字段，label 为前端展示名
const coverageItems = computed(() => [
  { key: 'real_session_coverage', label: '真实会话', value: props.dataQuality.real_session_coverage },
  { key: 'scene_label_coverage', label: '场景标注', value: props.dataQuality.scene_label_coverage },
  { key: 'price_catalog_coverage', label: '价目目录', value: props.dataQuality.price_catalog_coverage },
  { key: 'position_label_coverage', label: '岗位标注', value: props.dataQuality.position_label_coverage },
  { key: 'failed_cost_coverage', label: '失败成本', value: props.dataQuality.failed_cost_coverage },
])

// 覆盖率阈值语义：>=0.9 达标(success)，>=0.5 中(warning)，<0.5 低(danger)
function tagType(v: number): 'success' | 'warning' | 'danger' {
  if (v >= 0.9) return 'success'
  if (v >= 0.5) return 'warning'
  return 'danger'
}

function pct(v: number): string {
  return `${(v * 100).toFixed(0)}%`
}

// 红线文案：只说"疑似/待核查/仅供参考"，绝不出现"违规/违纪/滥用"
const lowCoverage = computed(() => (props.dataQuality.real_session_coverage || 0) < 0.5)
const lowCoverageNote = computed(() =>
  `真实会话覆盖率 ${pct(props.dataQuality.real_session_coverage)}，不足 50%：` +
  `「挤牙膏」「多任务不切会话」判定为疑似，仅供参考。`,
)
</script>

<style scoped>
.urq {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.urq__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 18px;
}
.urq__item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.urq__label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.urq__hint {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
</style>
