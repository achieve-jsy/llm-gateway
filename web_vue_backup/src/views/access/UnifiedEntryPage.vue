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
  width: min(100%, 1040px);
  margin: 0 auto;
}

/* Hero 区域 */
.hero-section {
  background: linear-gradient(135deg, #EAF8F7 0%, #FFFFFF 66%, #FFF8E7 100%);
  border: 1px solid #D1E9E7;
  border-radius: 12px;
  padding: 24px 28px;
  margin-bottom: 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  box-shadow: 0 8px 24px rgba(43, 168, 162, 0.08);
  position: relative;
  overflow: hidden;

  &::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    width: 3px;
    height: 100%;
    background: linear-gradient(180deg, #2BA8A2, #FFD23F);
    border-radius: 3px 0 0 3px;
  }
}

.hero-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.hero-badge {
  display: inline-block;
  background: linear-gradient(135deg, #2BA8A2, #3CC4BD);
  color: #FFFFFF;
  font-size: 11px;
  font-weight: 700;
  padding: 3px 9px;
  border-radius: 999px;
  letter-spacing: 0.8px;
  margin-bottom: 10px;
  box-shadow: 0 4px 12px rgba(43, 168, 162, 0.18);
}

.hero-title {
  font-size: 24px;
  font-weight: 800;
  color: #1E8C86;
  letter-spacing: 0.6px;
  line-height: 1.15;
  margin: 0 0 10px;
}

.hero-desc {
  font-size: 13px;
  color: #64748B;
  line-height: 1.55;
  margin: 0 0 18px;
  max-width: 500px;
}

.hero-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, auto));
  gap: 14px;
  align-items: stretch;
  width: fit-content;
}

.stat-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  min-width: 84px;
}

.stat-value {
  font-size: 19px;
  font-weight: 800;
  color: #2BA8A2;
  line-height: 1;
  margin-bottom: 1px;
}

.stat-label {
  font-size: 11px;
  color: #94A3B8;
  font-weight: 500;
}

.stat-divider {
  display: none;
}

.hero-visual {
  flex-shrink: 0;
  align-self: stretch;
  display: flex;
}

.hero-code {
  background: linear-gradient(180deg, rgba(43, 168, 162, 0.10), rgba(43, 168, 162, 0.04));
  border: 1px solid rgba(43, 168, 162, 0.18);
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 5px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.45);
  min-width: 228px;
  align-self: center;

  code {
    font-family: 'Fira Code', monospace;
    font-size: 12px;
    color: #1E706B;
    letter-spacing: 0.3px;
    line-height: 1.45;
  }
}

@media (max-width: 900px) {
  .hero-section {
    padding: 22px 20px;
    gap: 16px;
    flex-direction: column;
    align-items: flex-start;
  }

  .hero-desc {
    max-width: none;
  }

  .hero-stats {
    grid-template-columns: repeat(2, minmax(84px, auto));
  }

  .hero-visual {
    width: 100%;
  }

  .hero-code {
    min-width: 0;
  }
}

@media (max-width: 640px) {
  .unified-entry-page {
    width: 100%;
  }

  .hero-section {
    padding: 18px 16px;
    margin-bottom: 20px;
  }

  .hero-title {
    font-size: 22px;
  }

  .hero-stats {
    gap: 10px;
    grid-template-columns: 1fr;
    width: 100%;
  }

  .hero-code {
    width: 100%;
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
  background: rgba(15, 41, 40, 0.92);
  border: 1px solid #1E5653;
}

:global(html.dark) .hero-badge {
  background: linear-gradient(135deg, #2BA8A2, #3CC4BD);
  color: #FFFFFF;
}

:global(html.dark) .hero-code code {
  color: #D6F2F0;
}
</style>
