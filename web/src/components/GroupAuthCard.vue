<template>
  <div
    class="gc"
    :class="[`gc-accent-${accent}`, { 'gc-clickable': clickable }]"
    :role="clickable ? 'button' : undefined"
    @click="clickable && emit('click')"
  >
    <!-- 顶部状态色条 -->
    <div class="gc-stripe"></div>

    <div class="gc-body">
      <!-- 名称行 + 状态标签 -->
      <div class="gc-name-row">
        <span class="gc-name">{{ group.name }}</span>
        <span class="gc-status" :class="`st-${accent}`">{{ statusText }}</span>
      </div>

      <!-- 描述 -->
      <p class="gc-desc">{{ group.description || '暂无描述' }}</p>

      <!-- 单价（每百万 tokens） -->
      <div
        v-if="cap && (cap.input_price_per_million > 0 || cap.output_price_per_million > 0)"
        class="gc-price"
      >
        <span class="gc-price__item">
          <i class="gc-price__dot in" /> 输入
          <b>{{ formatPrice(cap.input_price_per_million) }}</b>
        </span>
        <span class="gc-price__item">
          <i class="gc-price__dot out" /> 输出
          <b>{{ formatPrice(cap.output_price_per_million) }}</b>
        </span>
        <span class="gc-price__unit">/ 1M tokens</span>
      </div>

      <!-- 能力 + 元信息 -->
      <div class="gc-foot">
        <div class="gc-caps">
          <template v-if="cap">
            <span class="cap cap-mode">{{ (MODE_LABEL[cap.mode] ?? cap.mode) || 'chat' }}</span>
            <span v-if="cap.supports_vision" class="cap cap-v">视觉</span>
            <span v-if="cap.supports_function_calling" class="cap cap-f">工具</span>
            <span v-if="cap.max_input_tokens > 0" class="cap cap-c">{{ formatTokens(cap.max_input_tokens) }}</span>
          </template>
          <span v-else class="cap cap-loading">—</span>
        </div>
        <div class="gc-meta">
          <span class="gc-badge" :class="`badge-${group.strategy}`">
            {{ STRATEGY_LABEL[group.strategy] ?? group.strategy }}
          </span>
          <span class="gc-models">{{ group.model_count ?? 0 }} 个模型</span>
        </div>
      </div>

      <!-- 授权提示（如覆盖范围 / 状态说明） -->
      <div v-if="hint" class="gc-hint">
        <span class="gc-hint__dot" :class="`dot-${accent}`"></span>
        <span>{{ hint }}</span>
      </div>

      <!-- 操作区（由父级注入） -->
      <div v-if="$slots.footer" class="gc-action" @click.stop>
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { GroupListItem } from '@/types/group'
import type { GroupCapability } from '@/api/groups'

defineProps<{
  group: GroupListItem
  cap?: GroupCapability | null
  statusText: string
  accent: 'success' | 'warning' | 'muted'
  hint?: string
  clickable?: boolean
}>()

const emit = defineEmits<{ click: [] }>()

const STRATEGY_LABEL: Record<string, string> = {
  round_robin: '轮询',
  priority: '优先级',
  weighted: '加权',
}

const MODE_LABEL: Record<string, string> = {
  chat: '对话',
  completion: '补全',
  embedding: '向量',
  image_generation: '图像生成',
  audio_transcription: '语音转写',
  audio_speech: '语音合成',
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`
  return `${n}`
}

function formatPrice(n: number): string {
  if (!n || n <= 0) return '免费'
  if (n < 0.01) return `$${n.toFixed(4)}`
  if (n < 1) return `$${n.toFixed(3)}`
  return `$${n.toFixed(2)}`
}
</script>

<style scoped>
.gc {
  background: #ffffff;
  border: 1px solid #e8f0ef;
  border-radius: 14px;
  overflow: hidden;
  transition: box-shadow 0.22s, transform 0.22s, border-color 0.22s;
  position: relative;
  display: flex;
  flex-direction: column;
}

.gc-clickable {
  cursor: pointer;
}

.gc-clickable:hover {
  box-shadow: 0 10px 36px rgba(54, 179, 78, 0.12), 0 2px 8px rgba(0, 0, 0, 0.05);
  transform: translateY(-3px);
  border-color: #a8dad7;
}

/* 顶部状态色条 */
.gc-stripe {
  height: 3px;
  background: #d8eeec;
}

.gc-accent-success .gc-stripe { background: linear-gradient(90deg, #0EA371, #5FD3A8); }
.gc-accent-warning .gc-stripe { background: linear-gradient(90deg, #e67e22, #f0a96a); }
.gc-accent-muted .gc-stripe { background: #d8e2e1; }

.gc-body {
  padding: 16px 18px 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
}

.gc-name-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.gc-name {
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 14px;
  font-weight: 700;
  color: #0d4a47;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gc-accent-muted .gc-name { color: #5a7a78; }

/* 状态标签 */
.gc-status {
  flex-shrink: 0;
  font-size: 11px;
  font-weight: 700;
  padding: 2px 9px;
  border-radius: 99px;
  border: 1px solid;
  white-space: nowrap;
}

.st-success { color: #1e8e54; background: rgba(14, 163, 113, 0.1); border-color: rgba(14, 163, 113, 0.25); }
.st-warning { color: #ca6f1e; background: rgba(230, 126, 34, 0.1); border-color: rgba(230, 126, 34, 0.25); }
.st-muted { color: #8a9a98; background: #f4f7f7; border-color: #dde6e5; }

/* 描述 */
.gc-desc {
  font-size: 12.5px;
  color: #7a9a98;
  line-height: 1.6;
  margin: 0;
  overflow: hidden;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  min-height: 2em;
}

/* 单价 */
.gc-price {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 12px;
  padding: 7px 10px;
  background: #f6fbfb;
  border: 1px solid #e2f1f0;
  border-radius: 8px;
  font-size: 11.5px;
  color: #6a8e8c;
}

.gc-price__item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.gc-price__item b {
  color: #0d4a47;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.gc-price__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  display: inline-block;
}

.gc-price__dot.in { background: #3498db; }
.gc-price__dot.out { background: #36B34E; }

.gc-price__unit {
  margin-left: auto;
  font-size: 10.5px;
  color: #a8c4c2;
}

/* 能力 + 元信息 */
.gc-foot {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 8px;
  margin-top: 2px;
  flex-wrap: wrap;
}

.gc-caps {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.cap {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 5px;
  line-height: 1.6;
  border: 1px solid;
}

.cap-mode { color: #1A7A37; background: rgba(54, 179, 78, 0.08); border-color: rgba(54, 179, 78, 0.2); }
.cap-v { color: #2471a3; background: rgba(36, 113, 163, 0.08); border-color: rgba(36, 113, 163, 0.2); }
.cap-f { color: #7d3c98; background: rgba(125, 60, 152, 0.07); border-color: rgba(125, 60, 152, 0.18); }
.cap-c { color: #8a9a98; background: #f5fafa; border-color: #d8eeec; font-variant-numeric: tabular-nums; }
.cap-loading { color: #b0cccb; background: transparent; border-color: transparent; }

.gc-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
  flex-shrink: 0;
}

.gc-badge {
  font-size: 10.5px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 99px;
  border: 1px solid;
  white-space: nowrap;
}

.badge-round_robin { color: #2471a3; background: rgba(36, 113, 163, 0.08); border-color: rgba(36, 113, 163, 0.2); }
.badge-priority { color: #7d3c98; background: rgba(125, 60, 152, 0.08); border-color: rgba(125, 60, 152, 0.2); }
.badge-weighted { color: #ca6f1e; background: rgba(202, 111, 30, 0.08); border-color: rgba(202, 111, 30, 0.2); }

.gc-models {
  font-size: 11px;
  color: #a8c4c2;
}

/* 提示行 */
.gc-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #7a9a98;
  margin-top: 2px;
}

.gc-hint__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.dot-success { background: #0EA371; }
.dot-warning { background: #e67e22; }
.dot-muted { background: #b0c4c2; }

/* 操作区 */
.gc-action {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  padding-top: 10px;
  border-top: 1px solid #eef5f4;
  min-height: 22px;
}

/* ── 深色模式 ───────────────────────────── */
:global(html.dark) .gc { background: #0e1d1c; border-color: #1a3230; }
:global(html.dark) .gc-clickable:hover { border-color: #36B34E; box-shadow: 0 10px 36px rgba(0, 0, 0, 0.4); }
:global(html.dark) .gc-name { color: #a0e0dc; }
:global(html.dark) .gc-accent-muted .gc-name { color: #5a8a88; }
:global(html.dark) .gc-desc { color: #4a7a78; }
:global(html.dark) .gc-models { color: #2a5a58; }
:global(html.dark) .gc-hint { color: #4a7a78; }
:global(html.dark) .gc-action { border-color: #15302e; }
:global(html.dark) .gc-price { background: #0b1817; border-color: #1a3230; color: #4a7a78; }
:global(html.dark) .gc-price__item b { color: #a0e0dc; }
:global(html.dark) .gc-price__unit { color: #2a5a58; }
:global(html.dark) .st-success { color: #5fd99a; background: rgba(14, 163, 113, 0.12); border-color: rgba(14, 163, 113, 0.28); }
:global(html.dark) .st-warning { color: #f0b27a; background: rgba(230, 126, 34, 0.12); border-color: rgba(230, 126, 34, 0.28); }
:global(html.dark) .st-muted { color: #6a8a88; background: #112221; border-color: #1f3a38; }
:global(html.dark) .cap-mode { color: #5DC370; background: rgba(54, 179, 78, 0.1); border-color: rgba(54, 179, 78, 0.25); }
:global(html.dark) .cap-v { color: #5dade2; background: rgba(52, 152, 219, 0.1); border-color: rgba(52, 152, 219, 0.22); }
:global(html.dark) .cap-f { color: #bb8fce; background: rgba(155, 89, 182, 0.1); border-color: rgba(155, 89, 182, 0.22); }
:global(html.dark) .cap-c { color: #2a6a68; background: transparent; border-color: #1a3230; }
:global(html.dark) .badge-round_robin { color: #5dade2; background: rgba(52, 152, 219, 0.1); border-color: rgba(52, 152, 219, 0.22); }
:global(html.dark) .badge-priority { color: #bb8fce; background: rgba(155, 89, 182, 0.1); border-color: rgba(155, 89, 182, 0.22); }
:global(html.dark) .badge-weighted { color: #f0b27a; background: rgba(230, 126, 34, 0.1); border-color: rgba(230, 126, 34, 0.22); }
</style>
