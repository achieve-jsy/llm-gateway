<template>
  <section class="model-discovery-section">
    <!-- 标题行 -->
    <div class="section-header">
      <h3 class="section-title">
        <span class="title-icon">🤖</span>
        可用模型
        <el-tag size="small" effect="plain" round class="model-count" v-if="!loading">
          {{ filteredModels.length }}
        </el-tag>
      </h3>
      <div class="section-actions">
        <el-input
          v-model="searchQuery"
          placeholder="搜索模型..."
          clearable
          size="small"
          class="search-input"
          :prefix-icon="Search"
        />
        <el-button text size="small" @click="loadModels" :loading="loading" class="refresh-btn">
          刷新
        </el-button>
      </div>
    </div>

    <!-- 筛选标签栏 -->
    <div class="filter-bar" v-if="!loading && models.length > 0">
      <!-- 模型标签筛选 -->
      <div class="filter-group" v-if="allTags.length > 0">
        <span class="filter-label">标签</span>
        <div class="filter-tags">
          <span
            class="filter-tag"
            :class="{ active: tagFilter === '' }"
            @click="tagFilter = ''"
          >全部</span>
          <span
            v-for="tag in allTags"
            :key="tag.id"
            class="filter-tag"
            :class="{ active: tagFilter === tag.id }"
            :style="tagFilter === tag.id ? { background: tag.color, borderColor: tag.color } : {}"
            @click="tagFilter = tag.id"
          >
            <span class="tag-color-dot" :style="{ background: tag.color }" v-if="tagFilter !== tag.id"></span>
            {{ tag.name }}
          </span>
        </div>
      </div>

      <!-- 类型筛选 -->
      <div class="filter-group" v-if="modelTypes.length > 1">
        <span class="filter-label">类型</span>
        <div class="filter-tags">
          <span
            class="filter-tag"
            :class="{ active: typeFilter === '' }"
            @click="typeFilter = ''"
          >全部</span>
          <span
            v-for="t in modelTypes"
            :key="t"
            class="filter-tag"
            :class="{ active: typeFilter === t }"
            @click="typeFilter = t"
          >{{ t }}</span>
        </div>
      </div>
    </div>

    <!-- 加载态 -->
    <div v-if="loading" class="skeleton-grid">
      <div v-for="n in 6" :key="n" class="skeleton-card">
        <el-skeleton :rows="2" animated />
      </div>
    </div>

    <!-- 错误态 -->
    <div v-else-if="error" class="error-box">
      <span class="error-icon">⚠️</span>
      <p class="error-text">{{ error }}</p>
      <el-button type="primary" size="small" @click="loadModels">重试</el-button>
    </div>

    <!-- 空态 -->
    <div v-else-if="models.length === 0" class="empty-state">
      <div class="empty-icon">📭</div>
      <p class="empty-text">暂无可用模型</p>
      <p class="empty-hint">请先在模型管理中注册并启用模型</p>
    </div>

    <!-- 模型卡片（固定高度滚动） -->
    <div v-else class="model-scroll-area">
      <div class="model-grid" v-if="filteredModels.length > 0">
        <ModelCard
          v-for="m in filteredModels"
          :key="m.id"
          :model="m"
          :tags="modelTagsMap[m.id] || []"
        />
      </div>
      <div v-else class="no-results">
        <p>无匹配结果</p>
        <el-button text size="small" @click="resetFilters">清除筛选</el-button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { getModels, getModelTags, batchGetModelTags } from '@/services/control-plane'
import type { Model } from '@/types/model'
import type { ModelTag } from '@/types/model-tag'
import ModelCard from './ModelCard.vue'

const models = ref<Model[]>([])
const allTags = ref<ModelTag[]>([])
const modelTagsMap = ref<Record<string, ModelTag[]>>({})
const loading = ref(false)
const error = ref('')
const searchQuery = ref('')
const typeFilter = ref('')
const tagFilter = ref('')

// ─── 计算属性 ─────────────────────────────────────────────────

const modelTypes = computed(() => {
  const types = new Set(models.value.map(m => m.model_type || 'chat'))
  return Array.from(types).sort()
})

const filteredModels = computed(() => {
  let list = models.value

  // 类型筛选
  if (typeFilter.value) {
    list = list.filter(m => (m.model_type || 'chat') === typeFilter.value)
  }

  // 标签筛选
  if (tagFilter.value) {
    list = list.filter(m => {
      const tags = modelTagsMap.value[m.id] || []
      return tags.some(t => t.id === tagFilter.value)
    })
  }

  // 搜索
  const q = searchQuery.value.toLowerCase().trim()
  if (q) {
    list = list.filter(m =>
      m.name.toLowerCase().includes(q) ||
      (m.model_type || '').toLowerCase().includes(q) ||
      (m.provider?.name || '').toLowerCase().includes(q)
    )
  }

  return list
})

// ─── 方法 ─────────────────────────────────────────────────────

function resetFilters() {
  searchQuery.value = ''
  typeFilter.value = ''
  tagFilter.value = ''
}

async function loadModels() {
  loading.value = true
  error.value = ''
  try {
    // 只获取已启用的模型
    const result = await getModels(undefined, { pageSize: 200 })
    models.value = result.data.filter(m => m.enabled)

    // 并行获取标签数据
    const [tags, tagsMap] = await Promise.all([
      getModelTags().catch(() => [] as ModelTag[]),
      models.value.length > 0
        ? batchGetModelTags(models.value.map(m => m.id)).catch(() => ({} as Record<string, ModelTag[]>))
        : Promise.resolve({} as Record<string, ModelTag[]>)
    ])
    allTags.value = tags.sort((a, b) => a.sort_order - b.sort_order)
    modelTagsMap.value = tagsMap
  } catch (e: any) {
    error.value = e?.message || '获取模型列表失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadModels()
})
</script>

<style scoped>
.model-discovery-section {
  margin-top: 8px;
}

/* ─── 标题行 ───────────────────────────────── */
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 3px dashed var(--el-border-color);
}

.section-title {
  font-size: 18px;
  font-weight: 800;
  color: var(--el-text-color-primary);
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
}

.title-icon {
  font-size: 20px;
}

.model-count {
  font-size: 11px;
  font-weight: 500;
}

.section-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.search-input {
  width: 180px;
}

.refresh-btn {
  color: var(--el-color-primary);
}

/* ─── 筛选标签栏 ───────────────────────────── */
.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-bottom: 16px;
  padding: 12px 16px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}

.filter-group {
  display: flex;
  align-items: center;
  gap: 8px;
}

.filter-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.filter-tags {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.filter-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-secondary);
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
  user-select: none;
}

.filter-tag:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.filter-tag.active {
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
  color: #fff;
}

.tag-color-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

/* ─── 滚动区域 ─────────────────────────────── */
.model-scroll-area {
  max-height: 480px;
  overflow-y: auto;
  padding-right: 4px;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}

.model-scroll-area::-webkit-scrollbar {
  width: 6px;
}

.model-scroll-area::-webkit-scrollbar-track {
  background: transparent;
}

.model-scroll-area::-webkit-scrollbar-thumb {
  background: var(--el-border-color);
  border-radius: 3px;
}

.model-scroll-area::-webkit-scrollbar-thumb:hover {
  background: var(--el-color-primary-light-5);
}

/* ─── 卡片网格 ─────────────────────────────── */
.skeleton-grid,
.model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}

.skeleton-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 12px;
  padding: 20px;
}

/* ─── 错误/空态 ─────────────────────────────── */
.error-box {
  background: rgba(231, 76, 60, 0.06);
  border: 1px solid rgba(231, 76, 60, 0.2);
  border-left: 4px solid #E74C3C;
  border-radius: 12px;
  padding: 24px;
  text-align: center;
}

.error-icon {
  font-size: 28px;
  display: block;
  margin-bottom: 8px;
}

.error-text {
  color: #E74C3C;
  font-weight: 500;
  margin: 0 0 12px;
}

.empty-state {
  text-align: center;
  padding: 48px 0;
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 12px;
}

.empty-text {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin: 0 0 4px;
}

.empty-hint {
  font-size: 13px;
  color: var(--el-text-color-placeholder);
  margin: 0;
}

.no-results {
  grid-column: 1 / -1;
  text-align: center;
  padding: 40px;
  color: var(--el-text-color-secondary);
}

.no-results p {
  margin: 0 0 8px;
}
</style>
