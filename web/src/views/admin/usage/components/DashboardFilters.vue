<template>
  <div class="ud-filter">
    <div class="ud-filter-row ud-filter-row--date">
      <div class="ud-presets">
        <button
          v-for="p in datePresets" :key="p.label"
          :class="['ud-preset-btn', { 'ud-preset-btn--active': activePreset === p.label }]"
          @click="$emit('applyPreset', p)"
        >{{ p.label }}</button>
      </div>
      <div class="ud-filter-divider" />
      <el-date-picker
        v-model="dateRange"
        type="daterange"
        range-separator="至"
        start-placeholder="开始日期"
        end-placeholder="结束日期"
        value-format="YYYY-MM-DD"
        format="MM/DD"
        class="ud-filter__date"
        size="default"
        @change="$emit('dateChange')"
      />
      <!-- 移动端筛选折叠按钮 -->
      <button class="ud-filter-toggle" @click="showFilters = !showFilters">
        <el-icon size="14"><component :is="showFilters ? 'ArrowUp' : 'ArrowDown'" /></el-icon>
        筛选
        <span v-if="activeFilterCount > 0" class="ud-filter-badge">{{ activeFilterCount }}</span>
      </button>
    </div>
    <Transition name="ud-filter-collapse">
      <div v-show="showFilters" class="ud-filter-row ud-filter-row--dims">
        <!-- 成员姓名：非普通用户可见 -->
        <div v-if="effectiveRole !== 'user'" class="ud-filter-chip">
          <span class="ud-chip-label">成员</span>
          <el-input v-model="filterUserName" placeholder="姓名搜索" clearable size="small"
            class="ud-chip-input" @change="$emit('userNameChange')" @clear="$emit('userNameClear')">
            <template #prefix><el-icon><User /></el-icon></template>
          </el-input>
        </div>
        <!-- 模型：下拉选择 -->
        <div class="ud-filter-chip">
          <span class="ud-chip-label">模型</span>
          <el-select v-model="filterModel" placeholder="全部模型" clearable filterable size="small"
            class="ud-chip-select" @change="$emit('loadAll')" @clear="$emit('loadAll')">
            <el-option v-for="m in modelOptions" :key="m" :label="m" :value="m" />
          </el-select>
        </div>
        <!-- 状态 -->
        <div class="ud-filter-chip">
          <span class="ud-chip-label">状态</span>
          <el-select v-model="filterStatus" placeholder="全部" clearable size="small"
            class="ud-chip-select" @change="$emit('loadAll')" @clear="$emit('loadAll')">
            <el-option label="全部" value="" />
            <el-option value="success">
              <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--ok" />成功</div>
            </el-option>
            <el-option value="error">
              <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--err" />失败</div>
            </el-option>
          </el-select>
        </div>
        <!-- Key 类型 -->
        <div v-if="effectiveRole !== 'user'" class="ud-filter-chip">
          <span class="ud-chip-label">Key 类型</span>
          <el-select v-model="filterKeyType" placeholder="全部" clearable size="small"
            class="ud-chip-select" @change="$emit('loadAll')" @clear="$emit('loadAll')">
            <el-option label="全部" value="" />
            <el-option label="个人 Key" value="personal" />
            <el-option label="程序 Key" value="service" />
          </el-select>
        </div>
        <!-- 端点 -->
        <div class="ud-filter-chip">
          <span class="ud-chip-label">端点</span>
          <el-select v-model="filterEndpoint" placeholder="全部" clearable size="small"
            class="ud-chip-select" @change="$emit('loadAll')" @clear="$emit('loadAll')">
            <el-option label="全部" value="" />
            <el-option label="Chat" value="/v1/chat/completions" />
            <el-option label="Embedding" value="/v1/embeddings" />
            <el-option label="Rerank" value="/v1/rerank" />
          </el-select>
        </div>
        <el-button v-if="activeFilterCount > 0" link size="small" type="warning" @click="$emit('clearFilters')">
          清空 {{ activeFilterCount }} 个筛选
        </el-button>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { User, ArrowUp, ArrowDown } from '@element-plus/icons-vue'

defineOptions({ name: 'DashboardFilters' })

defineProps<{
  datePresets: Array<{ label: string; days: number }>
  modelOptions: string[]
  effectiveRole: string
  activeFilterCount: number
}>()

const dateRange = defineModel<[string, string]>('dateRange', { required: true })
const activePreset = defineModel<string>('activePreset', { required: true })
const filterUserName = defineModel<string>('filterUserName', { required: true })
const filterModel = defineModel<string>('filterModel', { required: true })
const filterStatus = defineModel<string>('filterStatus', { required: true })
const filterKeyType = defineModel<string>('filterKeyType', { required: true })
const filterEndpoint = defineModel<string>('filterEndpoint', { required: true })
const showFilters = defineModel<boolean>('showFilters', { required: true })

defineEmits<{
  applyPreset: [preset: { label: string; days: number }]
  dateChange: []
  userNameChange: []
  userNameClear: []
  loadAll: []
  clearFilters: []
}>()
</script>

<style scoped>
/* ── 筛选栏 ── */
.ud-filter { display: flex; flex-direction: column; overflow: hidden; background: var(--ud-surface); border: 1px solid var(--ud-border); border-radius: 14px; }

.ud-filter-row {
  display: flex; align-items: center; gap: 10px; padding: 10px 16px;
  flex-wrap: wrap;
}
.ud-filter-row--date { border-bottom: 1px solid var(--ud-border); }
.ud-filter-row--dims { padding-top: 10px; padding-bottom: 10px; gap: 14px; }

.ud-presets { display: flex; align-items: center; gap: 4px; }
.ud-preset-btn {
  padding: 3px 10px; border-radius: 20px;
  border: 1px solid #dcdfe6; background: #f5f7fa;
  color: #606266; font-size: 13px; cursor: pointer;
  transition: all .15s; white-space: nowrap; line-height: 1.6;
}
.ud-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.ud-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.ud-filter__date { width: 220px; flex-shrink: 0; }
.ud-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 4px; flex-shrink: 0; }

.ud-filter-chip { display: flex; align-items: center; gap: 6px; }
.ud-chip-label { font-size: 12px; color: var(--ud-muted); white-space: nowrap; }
.ud-chip-input { width: 140px; }
.ud-chip-select { width: 100px; }

/* 移动端筛选折叠按钮 - 桌面端隐藏 */
.ud-filter-toggle {
  display: none;
  align-items: center;
  gap: 4px;
  padding: 6px 12px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #f5f7fa;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  margin-left: auto;
  white-space: nowrap;
}
.ud-filter-toggle:hover { border-color: #409eff; color: #409eff; }
.ud-filter-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: #409eff;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}

/* 折叠动画 */
.ud-filter-collapse-enter-active,
.ud-filter-collapse-leave-active {
  transition: all 0.25s ease;
  overflow: hidden;
}
.ud-filter-collapse-enter-from,
.ud-filter-collapse-leave-to {
  opacity: 0;
  max-height: 0;
  padding-top: 0;
  padding-bottom: 0;
}
.ud-filter-collapse-enter-to,
.ud-filter-collapse-leave-from {
  opacity: 1;
  max-height: 200px;
}

/* 状态过滤 */
.ud-status-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
.ud-status-dot--ok  { background: #67c23a; }
.ud-status-dot--err { background: #f56c6c; }

/* Mobile responsive */
@media (max-width: 640px) {
  .ud-filter-row--date {
    flex-wrap: wrap;
    gap: 6px;
  }
  .ud-filter-toggle {
    display: flex;
  }
  .ud-filter-divider {
    display: none;
  }
  .ud-presets {
    flex-wrap: wrap;
  }
  .ud-filter-row--dims {
    flex-wrap: wrap;
    gap: 8px;
  }
  .ud-filter-chip {
    width: 100%;
  }
  .ud-chip-input,
  .ud-chip-select {
    width: 100% !important;
  }
}
</style>