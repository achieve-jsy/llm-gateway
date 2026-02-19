<template>
  <section class="capability-section">
    <h3 class="section-title">
      <MagicStick class="title-icon" />
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
      <CircleClose class="title-icon" />
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
import { CircleClose, MagicStick } from '@element-plus/icons-vue'
import config from '@/assets/capability.json'
</script>

<style scoped>
.capability-section {
  margin-bottom: 28px;
}

.section-title {
  font-size: 16px;
  font-weight: 800;
  color: #1F8A3B;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid #D9E6E5;
  display: flex;
  align-items: center;
  gap: 8px;
}

.title-icon {
  font-size: 18px;
}

.exclude-title {
  margin-top: 28px;
}

.capability-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 12px;
}

.capability-card {
  background: #FFFFFF;
  border: 1px solid #D9E6E5;
  border-radius: 8px;
  display: flex;
  overflow: hidden;
  transition: all 0.2s;
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.04);
}

.capability-card:hover {
  box-shadow: 0 6px 18px rgba(15, 23, 42, 0.07);
  transform: translateY(-1px);
  border-color: rgba(54, 179, 78, 0.28);
}

.card-accent {
  width: 3px;
  background: linear-gradient(180deg, #36B34E, #5DC370);
  flex-shrink: 0;
}

.card-body {
  padding: 14px 16px;
  flex: 1;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.card-icon {
  font-size: 18px;
  color: #36B34E;
}

.card-title {
  font-size: 14px;
  font-weight: 700;
  color: #1F8A3B;
}

.card-desc {
  font-size: 12px;
  color: #64748B;
  line-height: 1.55;
  margin: 0;
}

.exclude-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.exclude-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  background: #FAFBF9;
  border-radius: 8px;
  border: 1px solid #E7ECE8;
}

.exclude-icon {
  font-size: 16px;
  color: #C49A3A;
}

.exclude-text {
  font-size: 13px;
  color: #64748B;
  font-weight: 500;
}

@media (max-width: 640px) {
  .capability-grid {
    grid-template-columns: 1fr;
  }
}

/* 深色模式 */
:global(html.dark) .capability-card {
  background: #143523;
  border-color: #1F5230;
}

:global(html.dark) .card-title {
  color: #5DC370;
}

:global(html.dark) .card-desc {
  color: #84C592;
}

:global(html.dark) .section-title {
  color: #5DC370;
  border-bottom-color: #1F5230;
}

:global(html.dark) .exclude-item {
  background: #1E2A1E;
  border-color: #3D4A2E;
}
</style>
