<script setup lang="ts">
import CapabilitySection from '@/components/access/CapabilitySection.vue'
import ModelDiscoverySection from '@/components/access/ModelDiscoverySection.vue'
</script>

<template>
  <div class="unified-entry-page">
    <!-- Hero 区域 -->
    <div class="hero-section">
      <div class="hero-content">
        <div class="hero-badge">🚀 统一接入</div>
        <h1 class="hero-title">LLM Gateway</h1>
        <p class="hero-desc">
          标准化 OpenAI 兼容接口，统一对接多种大模型供应商。一次接入即可访问所有已注册模型。
        </p>
        <div class="hero-stats">
          <div class="stat-item">
            <span class="stat-value">5+</span>
            <span class="stat-label">支持厂商</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-item">
            <span class="stat-value">50+</span>
            <span class="stat-label">可接模型</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-item">
            <span class="stat-value">100%</span>
            <span class="stat-label">OpenAI 兼容</span>
          </div>
        </div>
      </div>
      <div class="hero-visual">
        <div class="hero-code">
          <code>POST /v1/chat/completions</code>
          <code>GET  /v1/models</code>
        </div>
      </div>
    </div>

    <!-- 能力说明区 -->
    <CapabilitySection />

    <!-- 模型发现区 -->
    <ModelDiscoverySection />
  </div>
</template>

<style lang="scss" scoped>
.unified-entry-page {
  max-width: 1200px;
}

/* Hero 区域 */
.hero-section {
  background: linear-gradient(135deg, #E8F6F5 0%, #FFFFFF 60%, #FFF8E7 100%);
  border: 1px solid #D1E9E7;
  border-radius: 16px;
  padding: 40px;
  margin-bottom: 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 40px;
  box-shadow: 0 4px 20px rgba(43, 168, 162, 0.08);
  position: relative;
  overflow: hidden;

  &::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    width: 4px;
    height: 100%;
    background: linear-gradient(180deg, #2BA8A2, #FFD23F);
    border-radius: 4px 0 0 4px;
  }
}

.hero-content {
  flex: 1;
}

.hero-badge {
  display: inline-block;
  background: #2BA8A2;
  color: #FFFFFF;
  font-size: 12px;
  font-weight: 700;
  padding: 4px 12px;
  border-radius: 999px;
  letter-spacing: 1px;
  margin-bottom: 16px;
}

.hero-title {
  font-size: 32px;
  font-weight: 800;
  color: #1E8C86;
  letter-spacing: 2px;
  margin: 0 0 12px;
}

.hero-desc {
  font-size: 15px;
  color: #64748B;
  line-height: 1.7;
  margin: 0 0 24px;
  max-width: 500px;
}

.hero-stats {
  display: flex;
  align-items: center;
  gap: 24px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.stat-value {
  font-size: 24px;
  font-weight: 800;
  color: #2BA8A2;
}

.stat-label {
  font-size: 12px;
  color: #94A3B8;
  font-weight: 500;
}

.stat-divider {
  width: 1px;
  height: 32px;
  background: #D1E9E7;
}

.hero-visual {
  flex-shrink: 0;
}

.hero-code {
  background: #1E8C86;
  border-radius: 12px;
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  box-shadow: 0 4px 16px rgba(30, 140, 134, 0.25);

  code {
    font-family: 'Fira Code', monospace;
    font-size: 13px;
    color: #E8F6F5;
    letter-spacing: 0.5px;
  }
}

/* 深色模式适配 */
:global(html.dark) .hero-section {
  background: linear-gradient(135deg, #163B3A 0%, #1E5653 60%, #0F2928 100%);
  border-color: #1E5653;
}

:global(html.dark) .hero-title {
  color: #3CC4BD;
}

:global(html.dark) .hero-desc {
  color: #7BBFBB;
}

:global(html.dark) .stat-value {
  color: #3CC4BD;
}

:global(html.dark) .stat-divider {
  background: #1E5653;
}

:global(html.dark) .hero-code {
  background: #0F2928;
  border: 1px solid #1E5653;
}
</style>
