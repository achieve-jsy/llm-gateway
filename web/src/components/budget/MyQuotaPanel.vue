<template>
  <div class="mq" v-loading="loading">
    <template v-if="quota">
      <!-- KPI 概览行：对标预算页 mb-kpi-strip，补齐「一眼总览」能力 -->
      <section class="mq-kpi-strip">
        <div class="mq-kpi">
          <span>RPM 配额</span>
          <strong>{{ quota.effective.rpm === 0 ? '不限制' : quota.effective.rpm }}</strong>
          <em>每分钟请求数</em>
        </div>
        <div class="mq-kpi">
          <span>最大并发</span>
          <strong>{{ quota.effective.max_concurrency === 0 ? '不限制' : quota.effective.max_concurrency }}</strong>
          <em>同时进行请求数</em>
        </div>
        <div class="mq-kpi">
          <span>配额来源</span>
          <strong class="mq-kpi-source">
            <el-tag :type="sourceTagType(quota.effective.source)" size="small" effect="light">
              {{ sourceLabel(quota.effective.source) }}
            </el-tag>
          </strong>
          <em>{{ sourceDesc }}</em>
        </div>
        <div class="mq-kpi">
          <span>综合使用率</span>
          <strong :class="{ 'is-danger': maxRatio >= 80, 'is-warning': maxRatio >= 60 && maxRatio < 80 }">
            {{ maxRatio === 0 ? '—' : maxRatio + '%' }}
          </strong>
          <em>RPM / 并发较高者</em>
        </div>
      </section>

      <!-- 主面板：对标预算页 mb-panel -->
      <section class="mq-panel">
        <header class="mq-head">
          <div class="mq-head__title">
            <h2>当前生效配额</h2>
            <p>每分钟请求数 / 最大并发数 · 触顶返回 <code>429</code></p>
          </div>
          <div class="mq-head__actions">
            <el-tag :type="sourceTagType(quota.effective.source)" size="small" effect="light">
              {{ sourceLabel(quota.effective.source) }}
            </el-tag>
          </div>
        </header>

        <!-- 平衡块：对标 .mb-balance-block，左侧生效配额数值 + 右侧仪表盘环形 -->
        <div class="mq-balance-block">
          <div class="mq-balance-left">
            <span class="mq-balance-label">生效配额</span>
            <div class="mq-quota-duo">
              <div class="mq-quota-cell">
                <span class="mq-cell-label">每分钟请求数 (RPM)</span>
                <strong class="mq-cell-value">
                  {{ quota.effective.rpm === 0 ? '不限制' : quota.effective.rpm }}
                </strong>
              </div>
              <div class="mq-quota-cell">
                <span class="mq-cell-label">最大并发数</span>
                <strong class="mq-cell-value">
                  {{ quota.effective.max_concurrency === 0 ? '不限制' : quota.effective.max_concurrency }}
                </strong>
              </div>
            </div>
            <div class="mq-balance-sub">
              <template v-if="quota.effective.rpm === 0 && quota.effective.max_concurrency === 0">
                两项均不限制，当前无速率约束
              </template>
              <template v-else-if="quota.effective.rpm === 0">
                RPM 不限制 · 并发上限 {{ quota.effective.max_concurrency }}
              </template>
              <template v-else-if="quota.effective.max_concurrency === 0">
                RPM 上限 {{ quota.effective.rpm }} · 并发不限制
              </template>
              <template v-else>
                两项并行约束，任一触顶即被拒绝
              </template>
            </div>
          </div>

          <!-- 环形仪表盘：复刻 mb-balance-ring（r=26, stroke 5, dasharray pct*1.634 / 163.4） -->
          <div class="mq-balance-ring">
            <svg viewBox="0 0 64 64">
              <circle cx="32" cy="32" r="26" fill="none" stroke="var(--el-fill-color)" stroke-width="5"/>
              <circle
                cx="32" cy="32" r="26" fill="none"
                :stroke="getProgressColor(ringPercentage)"
                stroke-width="5"
                stroke-linecap="round"
                :stroke-dasharray="`${ringPercentage * 1.634} 163.4`"
                transform="rotate(-90 32 32)"
              />
            </svg>
            <span v-if="ringPercentage > 0">{{ ringPercentage }}%</span>
            <span v-else class="mq-ring-na">不限制</span>
          </div>
        </div>
        <!-- 环形副标注：使「综合压力」语义更显性 -->
        <div class="mq-ring-caption">
          <span v-if="ringPercentage > 0">综合使用率 {{ ringPercentage }}%，取 RPM / 并发较高者</span>
          <span v-else>当前无有效配额约束，综合使用率不适用</span>
        </div>

        <!-- period-rows 式指标行：当前使用量，带状态药丸 -->
        <div class="mq-period-rows">
          <div class="mq-row" :class="{ 'is-unlimited': quota.effective.rpm === 0 }">
            <div class="mq-row-name">
              <span>当前 RPM 使用</span>
              <span :class="`mq-pill mq-pill--${usageStatus(rpmPercentage, quota.effective.rpm)}`">
                <template v-if="quota.effective.rpm === 0">不限制</template>
                <template v-else>{{ usageStatusText(rpmPercentage) }}</template>
              </span>
            </div>
            <div class="mq-row-track">
              <el-progress
                v-if="quota.effective.rpm > 0"
                :percentage="rpmPercentage"
                :stroke-width="8"
                :color="getProgressColor(rpmPercentage)"
                :show-text="false"
              />
              <span v-else class="mq-row-unlimited">不限制</span>
            </div>
            <span class="mq-row-value">
              {{ quota.current_usage.rpm }}<template v-if="quota.effective.rpm > 0"> / {{ quota.effective.rpm }}</template>
            </span>
          </div>

          <div class="mq-row" :class="{ 'is-unlimited': quota.effective.max_concurrency === 0 }">
            <div class="mq-row-name">
              <span>当前并发使用</span>
              <span :class="`mq-pill mq-pill--${usageStatus(concPercentage, quota.effective.max_concurrency)}`">
                <template v-if="quota.effective.max_concurrency === 0">不限制</template>
                <template v-else>{{ usageStatusText(concPercentage) }}</template>
              </span>
            </div>
            <div class="mq-row-track">
              <el-progress
                v-if="quota.effective.max_concurrency > 0"
                :percentage="concPercentage"
                :stroke-width="8"
                :color="getProgressColor(concPercentage)"
                :show-text="false"
              />
              <span v-else class="mq-row-unlimited">不限制</span>
            </div>
            <span class="mq-row-value">
              {{ quota.current_usage.max_concurrency }}<template v-if="quota.effective.max_concurrency > 0"> / {{ quota.effective.max_concurrency }}</template>
            </span>
          </div>
        </div>

        <!-- 来源说明：对标 mb-shared-pool-notice 的图标+标题+描述结构 -->
        <div class="mq-source-note">
          <span class="mq-source-note__icon">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg>
          </span>
          <div class="mq-source-note__text">
            <span class="mq-source-note__title">{{ sourceNoteTitle }}</span>
            <span class="mq-source-note__desc">{{ sourceNoteDesc }}</span>
          </div>
        </div>
      </section>

      <!-- 配额 vs 预算 区分提示：对标 mb-monthly-warn -->
      <div class="mq-vs-note">
        <span class="mq-vs-note__icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
        </span>
        <span class="mq-vs-note__text">
          <strong>配额</strong>（RPM / 并发，超限返回 <code>429</code>）与<strong>预算</strong>（费用 / Token 额度，超限返回 <code>402</code>）是两个独立体系，互不影响。
        </span>
      </div>
    </template>

    <!-- 加载失败空状态 -->
    <div v-else-if="!loading" class="mq-empty">
      <el-result icon="warning" title="无法加载配额信息" sub-title="请检查网络后重试">
        <template #extra>
          <el-button type="primary" @click="loadQuota">重新加载</el-button>
        </template>
      </el-result>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getMyQuota } from '@/api/quota'
import type { MyQuotaResponse } from '@/api/quota'

const loading = ref(false)
const quota = ref<MyQuotaResponse | null>(null)

const rpmPercentage = computed(() => {
  if (!quota.value || quota.value.effective.rpm === 0) return 0
  return Math.min(100, Math.round((quota.value.current_usage.rpm / quota.value.effective.rpm) * 100))
})

const concPercentage = computed(() => {
  if (!quota.value || quota.value.effective.max_concurrency === 0) return 0
  return Math.min(100, Math.round((quota.value.current_usage.max_concurrency / quota.value.effective.max_concurrency) * 100))
})

// 综合使用率：取 RPM / 并发较高者；均为不限制（0）时返回 0（KPI 展示为 —）
const maxRatio = computed(() => {
  if (!quota.value) return 0
  return Math.max(rpmPercentage.value, concPercentage.value)
})

// 仪表盘读数：取有限项中使用率最高者，作为整体压力指示；均为不限制（0）时返回 0
const ringPercentage = computed(() => {
  if (!quota.value) return 0
  const candidates: number[] = []
  if (quota.value.effective.rpm > 0) candidates.push(rpmPercentage.value)
  if (quota.value.effective.max_concurrency > 0) candidates.push(concPercentage.value)
  if (candidates.length === 0) return 0
  return Math.max(...candidates)
})

function getProgressColor(percentage: number): string {
  if (percentage > 80) return '#f56c6c'  // 红色
  if (percentage > 60) return '#e6a23c'  // 黄色
  return '#67c23a'  // 绿色
}

async function loadQuota() {
  loading.value = true
  try {
    const res = await getMyQuota()
    // 后端响应为 { code:0, data:{ effective, current_usage } }，真实对象在 res.data.data
    quota.value = (res as any)?.data?.data || null
  } catch {
    quota.value = null
  } finally {
    loading.value = false
  }
}

function sourceLabel(source: string): string {
  const map: Record<string, string> = {
    default_user: '用户默认',
    default_service_key: 'Service Key 默认',
    override: '自定义',
    unset: '未设置（不限）',
  }
  return map[source] || source
}

function sourceTagType(source: string): string {
  switch (source) {
    case 'override': return 'success'      // 绿色
    case 'default_user': return ''         // 蓝色（默认）
    case 'default_service_key': return ''   // 蓝色（默认）
    case 'unset': return 'info'            // 灰色
    default: return 'info'
  }
}

// 来源说明：KPI strip 副标题
const sourceDesc = computed(() => {
  if (!quota.value) return ''
  const s = quota.value.effective.source
  if (s === 'override') return '管理员专属配额'
  if (s === 'default_user') return '全局默认值'
  if (s === 'default_service_key') return 'Service Key 默认值'
  return '未单独设置'
})

// 来源说明：底部标题
const sourceNoteTitle = computed(() => {
  if (!quota.value) return ''
  const s = quota.value.effective.source
  if (s === 'default_user') return '当前使用全局默认配额'
  if (s === 'override') return '管理员已为您配置专属配额'
  return '配额来源：' + sourceLabel(s)
})

// 来源说明：底部描述
const sourceNoteDesc = computed(() => {
  if (!quota.value) return ''
  const e = quota.value.effective
  switch (e.source) {
    case 'default_user':
      return '用户默认：' + (e.default_rpm ?? '-') + ' RPM / ' + (e.default_max_concurrency ?? '-') + ' 并发。管理员可为您单独调整。'
    case 'override':
      return '此配额由管理员单独设定，独立于全局默认值，可随时被调整。'
    default:
      return sourceLabel(e.source)
  }
})

// 状态药丸：limit === 0 视为不限制
function usageStatus(pct: number, limit: number): string {
  if (limit === 0) return 'unlimited'
  if (pct >= 80) return 'danger'
  if (pct >= 60) return 'warning'
  return 'success'
}

// 状态药丸文案（仅有限项调用）
function usageStatusText(pct: number): string {
  if (pct >= 80) return '紧张'
  if (pct >= 60) return '偏高'
  if (pct > 0) return '正常'
  return '空闲'
}

// 暴露给父组件手动刷新
defineExpose({ loadQuota })

onMounted(loadQuota)
</script>

<style scoped>
/* ─────────────── Layout ─────────────── */
.mq {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ─────────────── KPI strip（对标预算页 mb-kpi-strip） ─────────────── */
.mq-kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-border-color-lighter);
}
.mq-kpi {
  padding: 14px 18px;
  background: var(--el-bg-color);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.mq-kpi span {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.mq-kpi strong {
  font-size: 24px;
  color: var(--el-text-color-primary);
  letter-spacing: -.03em;
  line-height: 1.15;
  font-weight: 700;
}
.mq-kpi strong.is-danger { color: var(--el-color-danger); }
.mq-kpi strong.is-warning { color: var(--el-color-warning); }
.mq-kpi-source {
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  height: 28px;
}
.mq-kpi em {
  font-style: normal;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

/* ─────────────── Panel（mirror .mb-panel） ─────────────── */
.mq-panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.mq-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mq-head__title h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.mq-head__title p {
  margin: 3px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mq-head__title code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  background: var(--el-fill-color);
  color: var(--el-color-danger);
  padding: 1px 5px;
  border-radius: 3px;
}
.mq-head__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

/* ─────────────── Balance block（mirror .mb-balance-block） ─────────────── */
.mq-balance-block {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 20px 20px 14px;
}
.mq-balance-left {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.mq-balance-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}
.mq-quota-duo {
  display: flex;
  align-items: baseline;
  gap: 24px;
  flex-wrap: wrap;
}
.mq-quota-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mq-cell-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
/* 纯实色，与预算页 .mb-balance-amount 对齐 */
.mq-cell-value {
  font-size: 32px;
  font-weight: 800;
  color: var(--el-color-primary);
  letter-spacing: -.03em;
  line-height: 1;
}
.mq-balance-sub {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  line-height: 1.6;
}

/* ─────────────── Ring（replicate mb-balance-ring） ─────────────── */
.mq-balance-ring {
  position: relative;
  flex-shrink: 0;
  width: 64px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.mq-balance-ring svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}
.mq-balance-ring span {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  position: relative;
  z-index: 1;
}
.mq-ring-na {
  font-size: 11px;
  font-weight: 700;
  color: var(--el-color-success);
}

/* 环形副标注 */
.mq-ring-caption {
  padding: 0 20px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  text-align: right;
}

/* ─────────────── Period rows（mirror .mb-period-rows / .mb-period-item） ─────────────── */
.mq-period-rows {
  display: flex;
  flex-direction: column;
}
.mq-row {
  display: grid;
  grid-template-columns: minmax(96px, auto) minmax(0, 1fr) auto;
  gap: 16px;
  align-items: center;
  padding: 13px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mq-row:last-of-type {
  border-bottom: 0;
}
.mq-row.is-unlimited .mq-row-name > span:first-child {
  color: var(--el-text-color-secondary);
}
.mq-row-name {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--el-text-color-regular);
  font-size: 13px;
}

/* 状态药丸（对标预算页 .mb-tag，token 词汇与预算页一致） */
.mq-pill {
  display: inline-flex;
  align-items: center;
  align-self: flex-start;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 999px;
  line-height: 1.6;
}
.mq-pill--success   { background: var(--color-success-bg); color: var(--color-success); }
.mq-pill--warning   { background: var(--color-warning-bg); color: var(--color-warning); }
.mq-pill--danger    { background: var(--color-danger-bg);  color: var(--color-danger); }
.mq-pill--unlimited { background: var(--color-info-bg);    color: var(--color-info); }

.mq-row-track {
  min-width: 0;
  display: flex;
  align-items: center;
}
.mq-row-track :deep(.el-progress) {
  width: 100%;
}
.mq-row-track :deep(.el-progress-bar__inner) {
  transition: width 0.4s ease, background-color 0.4s ease;
}
.mq-row-unlimited {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-success);
}
.mq-row-value {
  font-size: 14px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}

/* ─────────────── Source note（对标 mb-shared-pool-notice） ─────────────── */
.mq-source-note {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin: 0 18px 16px;
  padding: 11px 14px;
  background: var(--el-color-info-light-9);
  border: 1px solid var(--el-color-info-light-5);
  border-radius: 10px;
}
.mq-source-note__icon {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  color: var(--el-color-info);
  margin-top: 1px;
}
.mq-source-note__text {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.mq-source-note__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.mq-source-note__desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

/* ─────────────── Quota vs Budget notice（对标 mb-monthly-warn） ─────────────── */
.mq-vs-note {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 11px 14px;
  background: var(--el-color-warning-light-9);
  border: 1px solid var(--el-color-warning-light-5);
  border-radius: 10px;
}
.mq-vs-note__icon {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  color: var(--el-color-warning);
  margin-top: 1px;
}
.mq-vs-note__text {
  font-size: 12px;
  color: var(--el-text-color-regular);
  line-height: 1.7;
}
.mq-vs-note__text strong {
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.mq-vs-note__text code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  background: var(--el-fill-color);
  color: var(--el-color-danger);
  padding: 1px 5px;
  border-radius: 3px;
}

/* ─────────────── Empty ─────────────── */
.mq-empty {
  padding: 48px 20px;
  text-align: center;
}

/* ─────────────── Responsive ─────────────── */
/* ≤768px：KPI → 2x2 */
@media (max-width: 768px) {
  .mq-kpi-strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .mq-balance-ring {
    width: 72px;
    height: 72px;
  }
}

/* ≤720px：面板头换行、平衡块竖排（同预算页处理）、指标行降为两行 */
@media (max-width: 720px) {
  .mq-head {
    flex-wrap: wrap;
    gap: 8px;
  }
  .mq-head__actions {
    width: 100%;
    justify-content: flex-start;
  }

  /* 平衡块竖排，环形靠右上偏移（同预算页 .mb-balance-ring 处理） */
  .mq-balance-block {
    flex-direction: column;
    align-items: flex-start;
    gap: 14px;
  }
  .mq-balance-ring {
    width: 80px;
    height: 80px;
    align-self: flex-end;
    margin-top: -52px;
  }
  .mq-ring-caption {
    text-align: left;
    padding-bottom: 14px;
  }

  /* 指标行：名称与数值同行，进度条独占下一行（grid-template-areas 两行 reflow） */
  .mq-row {
    grid-template-columns: 1fr auto;
    grid-template-areas:
      "name value"
      "track track";
    row-gap: 8px;
  }
  .mq-row-name { grid-area: name; }
  .mq-row-value { grid-area: value; }
  .mq-row-track { grid-area: track; }
}

/* ≤480px */
@media (max-width: 480px) {
  .mq-kpi {
    padding: 12px 14px;
  }
  .mq-kpi strong {
    font-size: 20px;
  }
  .mq-quota-duo {
    gap: 14px;
  }
  .mq-cell-value {
    font-size: 26px;
  }
  .mq-source-note {
    margin-left: 12px;
    margin-right: 12px;
  }
}
</style>