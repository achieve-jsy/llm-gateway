<template>
  <section class="model-discovery-section">
    <h3 class="section-title">
      <span class="title-icon">🤖</span>
      可用模型
      <el-button v-if="!loading && models.length > 0" text size="small" @click="loadModels" class="refresh-btn">
        刷新
      </el-button>
    </h3>

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
      <p class="empty-hint">请先在渠道管理中创建渠道并注册模型</p>
    </div>

    <!-- 正常：模型卡片网格 -->
    <div v-else class="model-grid">
      <ModelCard v-for="model in models" :key="model.id" :model="model" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { fetchModels } from '@/services/gateway-api'
import type { ModelInfo } from '@/types/model'
import ModelCard from './ModelCard.vue'

const models = ref<ModelInfo[]>([])
const loading = ref(false)
const error = ref('')

async function loadModels() {
  loading.value = true
  error.value = ''
  try {
    models.value = await fetchModels()
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

.section-title {
  font-size: 18px;
  font-weight: 800;
  color: #1E8C86;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 3px dashed #D1E9E7;
  display: flex;
  align-items: center;
  gap: 8px;
}

.title-icon {
  font-size: 20px;
}

.refresh-btn {
  margin-left: auto;
  color: #2BA8A2;
}

.skeleton-grid,
.model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 16px;
}

.skeleton-card {
  background: #FFFFFF;
  border: 1px solid #D1E9E7;
  border-radius: 12px;
  padding: 20px;
}

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
  color: #64748B;
  margin: 0 0 4px;
}

.empty-hint {
  font-size: 13px;
  color: #94A3B8;
  margin: 0;
}

/* 深色模式 */
:global(html.dark) .section-title {
  color: #3CC4BD;
  border-bottom-color: #1E5653;
}

:global(html.dark) .skeleton-card {
  background: #163B3A;
  border-color: #1E5653;
}

:global(html.dark) .error-box {
  background: rgba(231, 76, 60, 0.08);
  border-color: rgba(231, 76, 60, 0.3);
}
</style>
