<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">告警运行时设置</h3>
      <div class="ops-card-actions">
        <el-button size="small" @click="resetForm" :disabled="!hasChanges">重置</el-button>
        <el-button size="small" type="primary" :loading="saving" @click="saveSettings" :disabled="!hasChanges || !validation.valid">
          保存
        </el-button>
      </div>
    </div>

    <div v-if="!validation.valid && validation.errors.length > 0" class="validation-errors">
      <span v-for="(err, i) in validation.errors" :key="i">{{ err }}</span>
    </div>

    <div class="runtime-config-body">
      <!-- 评估间隔 -->
      <div class="config-section">
        <div class="config-section-title">评估参数</div>
        <div class="config-row">
          <div class="config-label">
            <span class="config-label-text">评估间隔（秒）</span>
            <span class="config-hint">告警评估器的运行间隔，范围 1~86400</span>
          </div>
          <el-input-number
            v-model="form.evaluation_interval_seconds"
            :min="1"
            :max="86400"
            :step="10"
            size="small"
          />
        </div>
      </div>

      <!-- 阈值覆写 -->
      <div class="config-section">
        <div class="config-section-title">阈值覆写</div>
        <div class="config-grid">
          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">SLA 下限 (%)</span>
              <span class="config-hint">低于此值触发 SLA 告警</span>
            </div>
            <el-input-number
              v-model="form.thresholds.sla_percent_min"
              :min="0"
              :max="100"
              :precision="2"
              :step="0.5"
              size="small"
            />
          </div>

          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">TTFT P99 上限 (ms)</span>
              <span class="config-hint">首字延迟 P99 超过此值告警</span>
            </div>
            <el-input-number
              v-model="form.thresholds.ttft_p99_ms_max"
              :min="0"
              :step="500"
              size="small"
            />
          </div>

          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">请求错误率上限 (%)</span>
              <span class="config-hint">超过此比例触发错误率告警</span>
            </div>
            <el-input-number
              v-model="form.thresholds.request_error_rate_percent_max"
              :min="0"
              :max="100"
              :precision="2"
              :step="0.5"
              size="small"
            />
          </div>

          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">上游错误率上限 (%)</span>
              <span class="config-hint">超过此比例触发上游错误告警</span>
            </div>
            <el-input-number
              v-model="form.thresholds.upstream_error_rate_percent_max"
              :min="0"
              :max="100"
              :precision="2"
              :step="0.5"
              size="small"
            />
          </div>
        </div>
      </div>

      <!-- 分布式锁 -->
      <div class="config-section">
        <div class="config-section-title">分布式锁（多实例部署）</div>
        <div class="config-row">
          <div class="config-label">
            <span class="config-label-text">启用分布式锁</span>
            <span class="config-hint">多实例部署时防止重复评估</span>
          </div>
          <el-switch v-model="form.distributed_lock.enabled" />
        </div>

        <template v-if="form.distributed_lock.enabled">
          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">锁 Key</span>
              <span class="config-hint">必须以 ops: 开头</span>
            </div>
            <el-input
              v-model="form.distributed_lock.key"
              size="small"
              placeholder="ops:alert_eval_lock"
              class="config-input"
            />
          </div>

          <div class="config-row">
            <div class="config-label">
              <span class="config-label-text">锁 TTL（秒）</span>
              <span class="config-hint">锁自动过期时间，范围 1~86400</span>
            </div>
            <el-input-number
              v-model="form.distributed_lock.ttl_seconds"
              :min="1"
              :max="86400"
              :step="10"
              size="small"
            />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { OpsAlertRuntimeSettings } from '@/api/ops'

const loading = ref(false)
const saving = ref(false)

const original = ref<OpsAlertRuntimeSettings | null>(null)
const form = ref<OpsAlertRuntimeSettings>({
  evaluation_interval_seconds: 60,
  thresholds: {
    sla_percent_min: 99.0,
    ttft_p99_ms_max: 5000,
    request_error_rate_percent_max: 5.0,
    upstream_error_rate_percent_max: 10.0
  },
  distributed_lock: {
    enabled: false,
    key: 'ops:alert_eval_lock',
    ttl_seconds: 30
  }
})

type ValidationResult = { valid: boolean; errors: string[] }

const validation = computed<ValidationResult>(() => {
  const errors: string[] = []
  const s = form.value

  if (!Number.isFinite(s.evaluation_interval_seconds) || s.evaluation_interval_seconds < 1 || s.evaluation_interval_seconds > 86400) {
    errors.push('评估间隔必须在 1~86400 秒之间')
  }

  const t = s.thresholds
  if (t.sla_percent_min != null && (t.sla_percent_min < 0 || t.sla_percent_min > 100)) {
    errors.push('SLA 下限必须在 0~100 之间')
  }
  if (t.ttft_p99_ms_max != null && t.ttft_p99_ms_max < 0) {
    errors.push('TTFT P99 上限不能为负')
  }
  if (t.request_error_rate_percent_max != null && (t.request_error_rate_percent_max < 0 || t.request_error_rate_percent_max > 100)) {
    errors.push('请求错误率上限必须在 0~100 之间')
  }
  if (t.upstream_error_rate_percent_max != null && (t.upstream_error_rate_percent_max < 0 || t.upstream_error_rate_percent_max > 100)) {
    errors.push('上游错误率上限必须在 0~100 之间')
  }

  if (s.distributed_lock?.enabled) {
    if (!s.distributed_lock.key || s.distributed_lock.key.trim().length < 3) {
      errors.push('锁 Key 不能为空且长度 >= 3')
    } else if (!s.distributed_lock.key.startsWith('ops:')) {
      errors.push('锁 Key 必须以 ops: 开头')
    }
    if (!Number.isFinite(s.distributed_lock.ttl_seconds) || s.distributed_lock.ttl_seconds < 1 || s.distributed_lock.ttl_seconds > 86400) {
      errors.push('锁 TTL 必须在 1~86400 秒之间')
    }
  }

  return { valid: errors.length === 0, errors }
})

const hasChanges = computed(() => {
  if (!original.value) return false
  return JSON.stringify(original.value) !== JSON.stringify(form.value)
})

function resetForm() {
  if (original.value) {
    form.value = JSON.parse(JSON.stringify(original.value))
  }
}

async function loadSettings() {
  loading.value = true
  try {
    const data = await opsAPI.getAlertRuntimeSettings()
    form.value = { ...data, thresholds: { ...data.thresholds }, distributed_lock: { ...data.distributed_lock } }
    original.value = JSON.parse(JSON.stringify(data))
  } catch (err: any) {
    console.error('[RuntimeSettings] 加载配置失败', err)
    ElMessage.error('加载告警运行时设置失败')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  if (!validation.value.valid) return
  saving.value = true
  try {
    const data = await opsAPI.updateAlertRuntimeSettings(form.value)
    form.value = { ...data, thresholds: { ...data.thresholds }, distributed_lock: { ...data.distributed_lock } }
    original.value = JSON.parse(JSON.stringify(data))
    ElMessage.success('告警运行时设置已保存')
  } catch (err: any) {
    console.error('[RuntimeSettings] 保存失败', err)
    ElMessage.error(err?.response?.data?.error || '保存告警运行时设置失败')
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>

<style scoped>



.ops-card-actions {
  display: flex;
  gap: var(--space-2);
}

.validation-errors {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--space-2) var(--space-3);
  background: var(--el-color-danger-light-9, #fef0f0);
  border-radius: var(--radius-base);
  font-size: var(--font-size-xs);
  color: var(--el-color-danger);
  margin-bottom: var(--space-3);
}

.runtime-config-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.config-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.config-section-title {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-primary);
  padding-bottom: var(--space-1);
  border-bottom: 1px dashed var(--border-default);
}

.config-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-3);
}

.config-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.config-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.config-label-text {
  font-size: var(--font-size-sm);
  font-weight: 500;
  color: var(--text-primary);
}

.config-hint {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
}

.config-input {
  width: 220px;
}

@media (max-width: 900px) {
  .config-grid {
    grid-template-columns: 1fr;
  }
}
</style>
