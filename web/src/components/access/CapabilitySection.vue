<template>
  <section class="capability-section">
    <h3 class="section-title">
      <span class="title-icon">✨</span>
      支持能力
    </h3>
    <div class="capability-grid">
      <div
        v-for="item in config.capabilities"
        :key="item.id"
        class="capability-card"
      >
        <div class="card-accent"></div>
        <div class="card-body">
          <div class="card-header">
            <el-icon class="card-icon"><component :is="item.icon" /></el-icon>
            <span class="card-title">{{ item.title }}</span>
          </div>
          <p class="card-desc">{{ item.description }}</p>
        </div>
      </div>
    </div>

    <h3 class="section-title exclude-title">
      <span class="title-icon">🚫</span>
      暂不支持
    </h3>
    <div class="exclude-list">
      <div
        v-for="item in config.excludes"
        :key="item.id"
        class="exclude-item"
      >
        <el-icon class="exclude-icon"><CircleClose /></el-icon>
        <span class="exclude-text">{{ item.reason }}</span>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { CircleClose } from '@element-plus/icons-vue'
import config from '@/assets/capability.json'
</script>

<style scoped>
.capability-section {
  margin-bottom: 32px;
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

.exclude-title {
  margin-top: 32px;
}

.capability-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.capability-card {
  background: #FFFFFF;
  border: 1px solid #D1E9E7;
  border-radius: 12px;
  display: flex;
  overflow: hidden;
  transition: all 0.2s;
  box-shadow: 0 2px 10px rgba(43, 168, 162, 0.06);
}

.capability-card:hover {
  box-shadow: 0 4px 20px rgba(43, 168, 162, 0.15);
  transform: translateY(-2px);
}

.card-accent {
  width: 4px;
  background: linear-gradient(180deg, #2BA8A2, #3CC4BD);
  flex-shrink: 0;
}

.card-body {
  padding: 16px 20px;
  flex: 1;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.card-icon {
  font-size: 20px;
  color: #2BA8A2;
}

.card-title {
  font-size: 15px;
  font-weight: 700;
  color: #1E8C86;
}

.card-desc {
  font-size: 13px;
  color: #64748B;
  line-height: 1.6;
  margin: 0;
}

.exclude-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.exclude-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  background: #FFF8E7;
  border-radius: 8px;
  border: 1px solid #FFE47A;
}

.exclude-icon {
  font-size: 16px;
  color: #E6B800;
}

.exclude-text {
  font-size: 14px;
  color: #64748B;
  font-weight: 500;
}

/* 深色模式 */
:global(html.dark) .capability-card {
  background: #163B3A;
  border-color: #1E5653;
}

:global(html.dark) .card-title {
  color: #3CC4BD;
}

:global(html.dark) .card-desc {
  color: #7BBFBB;
}

:global(html.dark) .section-title {
  color: #3CC4BD;
  border-bottom-color: #1E5653;
}

:global(html.dark) .exclude-item {
  background: #1E2A1E;
  border-color: #3D4A2E;
}
</style>
