<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">邮件通知配置</h3>
      <el-button size="small" type="primary" :loading="saving" @click="saveConfig" :disabled="!hasChanges">
        保存
      </el-button>
    </div>

    <div class="email-config-body">
      <!-- 启用状态 -->
      <div class="config-row">
        <div class="config-label">
          <span class="config-label-text">启用邮件通知</span>
          <span class="config-hint">开启后，告警触发时将发送邮件通知</span>
        </div>
        <el-switch v-model="form.enabled" />
      </div>

      <el-divider />

      <!-- 告警收件人 -->
      <div class="config-row config-row--column">
        <div class="config-label">
          <span class="config-label-text">告警收件人</span>
          <span class="config-hint">告警触发时通知的邮箱列表</span>
        </div>
        <div class="email-list-editor">
          <div class="email-tags">
            <el-tag
              v-for="(email, idx) in form.alert_recipients"
              :key="idx"
              closable
              size="default"
              @close="removeAlertRecipient(idx)"
            >
              {{ email }}
            </el-tag>
            <span v-if="form.alert_recipients.length === 0" class="empty-hint">暂无告警收件人</span>
          </div>
          <div class="email-input-row">
            <el-input
              v-model="alertRecipientInput"
              size="small"
              placeholder="输入邮箱后点击添加"
              @keyup.enter="addAlertRecipient"
            />
            <el-button size="small" type="primary" @click="addAlertRecipient">添加</el-button>
            <el-button
              size="small"
              @click="testSend(alertRecipientInput)"
              :disabled="!alertRecipientInput || !isValidEmail(alertRecipientInput)"
              :loading="testSending"
            >
              测试发送
            </el-button>
          </div>
          <span v-if="alertRecipientError" class="form-error">{{ alertRecipientError }}</span>
        </div>
      </div>

      <!-- 报告收件人 -->
      <div class="config-row config-row--column">
        <div class="config-label">
          <span class="config-label-text">报告收件人</span>
          <span class="config-hint">定期报告发送的邮箱列表</span>
        </div>
        <div class="email-list-editor">
          <div class="email-tags">
            <el-tag
              v-for="(email, idx) in form.report_recipients"
              :key="idx"
              closable
              size="default"
              @close="removeReportRecipient(idx)"
            >
              {{ email }}
            </el-tag>
            <span v-if="form.report_recipients.length === 0" class="empty-hint">暂无报告收件人</span>
          </div>
          <div class="email-input-row">
            <el-input
              v-model="reportRecipientInput"
              size="small"
              placeholder="输入邮箱后点击添加"
              @keyup.enter="addReportRecipient"
            />
            <el-button size="small" type="primary" @click="addReportRecipient">添加</el-button>
          </div>
        </div>
      </div>

      <!-- 最小严重度 -->
      <div class="config-row">
        <div class="config-label">
          <span class="config-label-text">最小通知级别</span>
          <span class="config-hint">仅严重度 >= 此等级的告警发送邮件</span>
        </div>
        <el-select v-model="form.min_severity" size="small" class="config-select">
          <el-option label="全部级别" value="" />
          <el-option label="P0 - 紧急" value="P0" />
          <el-option label="P1 - 严重" value="P1" />
          <el-option label="P2 - 警告" value="P2" />
          <el-option label="P3 - 提示" value="P3" />
        </el-select>
      </div>

      <!-- 附加选项 -->
      <div class="config-row">
        <div class="config-label">
          <span class="config-label-text">包含已恢复事件</span>
          <span class="config-hint">告警恢复时也发送通知邮件</span>
        </div>
        <el-switch v-model="form.include_resolved" />
      </div>

      <div class="config-row">
        <div class="config-label">
          <span class="config-label-text">每日摘要</span>
          <span class="config-hint">每天发送一份告警摘要邮件</span>
        </div>
        <el-switch v-model="form.daily_summary" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { EmailNotificationConfig } from '@/api/ops'

const loading = ref(false)
const saving = ref(false)
const testSending = ref(false)

const original = ref<EmailNotificationConfig | null>(null)
const form = ref<EmailNotificationConfig>({
  enabled: false,
  alert_recipients: [],
  report_recipients: [],
  min_severity: '',
  include_resolved: false,
  daily_summary: false
})

const alertRecipientInput = ref('')
const reportRecipientInput = ref('')
const alertRecipientError = ref('')

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim())
}

const hasChanges = computed(() => {
  if (!original.value) return false
  const o = original.value
  const f = form.value
  return (
    o.enabled !== f.enabled ||
    o.min_severity !== f.min_severity ||
    o.include_resolved !== f.include_resolved ||
    o.daily_summary !== f.daily_summary ||
    JSON.stringify(o.alert_recipients) !== JSON.stringify(f.alert_recipients) ||
    JSON.stringify(o.report_recipients) !== JSON.stringify(f.report_recipients)
  )
})

function addAlertRecipient() {
  alertRecipientError.value = ''
  const email = alertRecipientInput.value.trim()
  if (!email) return
  if (!isValidEmail(email)) {
    alertRecipientError.value = '邮箱格式不正确'
    return
  }
  if (form.value.alert_recipients.includes(email)) {
    alertRecipientError.value = '该邮箱已存在'
    return
  }
  form.value.alert_recipients.push(email)
  alertRecipientInput.value = ''
}

function removeAlertRecipient(idx: number) {
  form.value.alert_recipients.splice(idx, 1)
}

function addReportRecipient() {
  const email = reportRecipientInput.value.trim()
  if (!email || !isValidEmail(email)) return
  if (form.value.report_recipients.includes(email)) return
  form.value.report_recipients.push(email)
  reportRecipientInput.value = ''
}

function removeReportRecipient(idx: number) {
  form.value.report_recipients.splice(idx, 1)
}

async function loadConfig() {
  loading.value = true
  try {
    const data = await opsAPI.getEmailNotificationConfig()
    form.value = { ...data }
    original.value = { ...data }
  } catch (err: any) {
    console.error('[EmailNotificationCard] 加载配置失败', err)
    ElMessage.error('加载邮件通知配置失败')
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const data = await opsAPI.updateEmailNotificationConfig(form.value)
    form.value = { ...data }
    original.value = { ...data }
    ElMessage.success('邮件通知配置已保存')
  } catch (err: any) {
    console.error('[EmailNotificationCard] 保存配置失败', err)
    ElMessage.error(err?.response?.data?.error || '保存邮件通知配置失败')
  } finally {
    saving.value = false
  }
}

async function testSend(recipient: string) {
  if (!isValidEmail(recipient)) return
  testSending.value = true
  try {
    await opsAPI.sendTestAlertEmail(recipient.trim())
    ElMessage.success(`测试邮件已发送至 ${recipient.trim()}`)
  } catch (err: any) {
    console.error('[EmailNotificationCard] 测试发送失败', err)
    ElMessage.error(err?.response?.data?.error || '测试发送失败')
  } finally {
    testSending.value = false
  }
}

onMounted(loadConfig)
</script>

<style scoped>



.email-config-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.config-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.config-row--column {
  flex-direction: column;
  align-items: stretch;
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

.config-select {
  width: 180px;
}

.email-list-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: 100%;
  margin-top: var(--space-2);
}

.email-tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  min-height: 28px;
}

.empty-hint {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
}

.email-input-row {
  display: flex;
  gap: var(--space-2);
  max-width: 420px;
}

.email-input-row .el-input {
  flex: 1;
}

.form-error {
  font-size: var(--font-size-xs);
  color: var(--el-color-danger);
}
</style>
