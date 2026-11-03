<template>
  <div class="notification-channels-page">
    <div class="page-header">
      <h2>通知渠道管理</h2>
      <p class="page-desc">统一管理各通知类型的发送开关、Webhook/邮件渠道配置和消息模板</p>
    </div>

    <el-card class="section-card">
      <template #header>
        <div class="card-header">
          <span>通知渠道矩阵</span>
          <el-button size="small" :icon="Refresh" circle @click="refresh" :loading="loading" />
        </div>
      </template>

      <div v-if="loading" v-loading="true" style="min-height: 120px" />
      <template v-else>
        <el-table :data="typeRows" border size="small" row-key="name">
          <el-table-column label="通知类型" min-width="200">
            <template #default="{ row }">
              <div class="type-cell">
                <span class="type-name">{{ row.displayName }}</span>
                <span class="type-desc">{{ row.description }}</span>
              </div>
            </template>
          </el-table-column>

          <!-- 站内信：直接开关 -->
          <el-table-column width="100" align="center">
            <template #header>
              <span>站内信</span>
            </template>
            <template #default="{ row }">
              <span v-if="!row.channels.includes('inapp')" class="na">—</span>
              <el-switch
                v-else
                :model-value="ruleEnabled(row.name, 'inapp')"
                :loading="savingCell === `${row.name}:inapp`"
                @change="(v: boolean | string | number) => saveRule(row.name, 'inapp', !!v)"
              />
            </template>
          </el-table-column>

          <!-- 邮件：状态圆点 + 配置 -->
          <el-table-column width="100" align="center">
            <template #header>
              <span>邮件</span>
            </template>
            <template #default="{ row }">
              <span v-if="!row.channels.includes('email')" class="na">—</span>
              <div v-else class="channel-cell">
                <el-tooltip :content="stateLabel(emailEffective(row.name))" placement="top">
                  <span class="status-dot" :class="emailEffective(row.name)" />
                </el-tooltip>
                <el-button link type="primary" size="small" @click="openEdit(row.name, 'email')">
                  配置
                </el-button>
              </div>
            </template>
          </el-table-column>

          <!-- Webhook：状态圆点 + 配置 -->
          <el-table-column width="100" align="center">
            <template #header>
              <span>Webhook</span>
            </template>
            <template #default="{ row }">
              <span v-if="!row.channels.includes('webhook')" class="na">—</span>
              <div v-else class="channel-cell">
                <el-tooltip :content="stateLabel(webhookActive(row.name))" placement="top">
                  <span class="status-dot" :class="webhookActive(row.name)" />
                </el-tooltip>
                <el-button link type="primary" size="small" @click="openEdit(row.name, 'webhook')">
                  配置
                </el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <p v-if="!emailEnabled" class="hint">
          邮件总开关未开启，邮件列状态均为关闭。请先到「系统设置 → 邮件通知」开启。
        </p>
        <p class="hint">
          绿色圆点 = 该渠道当前生效中；红色 = 已关闭；灰色 = 未配置。悬停圆点可查看状态，点击「配置」编辑。
        </p>
      </template>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="600px" destroy-on-close>
      <el-form :model="editForm" label-width="110px">
        <el-form-item label="渠道">
          <el-tag :type="editChannel === 'webhook' ? 'primary' : 'success'" effect="light" round>
            {{ editChannel === 'webhook' ? 'Webhook' : '邮件' }}
          </el-tag>
        </el-form-item>

        <!-- 邮件：总开关 + 投递开关 -->
        <template v-if="editChannel === 'email'">
          <el-form-item label="总开关">
            <el-switch
              :model-value="editRuleEnabled"
              :loading="savingRuleInDialog"
              @change="toggleRuleInDialog"
            />
            <span class="form-hint">控制是否允许该类型通过邮件发送（全局级）</span>
          </el-form-item>
          <el-form-item label="投递开关">
            <el-switch v-model="editForm.enabled" />
            <span class="form-hint">关闭则此类型邮件不再投递，即使总开关已开</span>
          </el-form-item>
        </template>

        <!-- Webhook：启用开关 -->
        <el-form-item v-if="editChannel === 'webhook'" label="启用">
          <el-switch v-model="editForm.enabled" />
        </el-form-item>

        <template v-if="editChannel === 'webhook'">
          <el-form-item label="Webhook URL">
            <el-input v-model="editForm.webhook_url" placeholder="https://..." />
          </el-form-item>
          <el-form-item label="Secret">
            <el-input
              v-model="editForm.webhook_secret"
              type="password"
              show-password
              placeholder="可选，用于签名验证"
            />
          </el-form-item>
        </template>

        <template v-if="editChannel === 'email'">
          <el-form-item label="收件人">
            <div class="readonly-hint">
              收件人由系统按通知接收人自动确定（无需在此指定）。邮件会发给该通知对应的用户。
            </div>
          </el-form-item>
        </template>

        <el-form-item label="主题模板">
          <el-input v-model="editForm.subject_template" type="textarea" :rows="2" />
          <div class="template-hint">可用变量: {{ TEMPLATE_VARIABLES.join(', ') || '—' }}</div>
        </el-form-item>

        <el-form-item label="正文模板">
          <el-input v-model="editForm.body_template" type="textarea" :rows="6" />
          <div class="template-hint">留空则使用系统默认模板（Go 模板语法）</div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button @click="restoreDefault">恢复默认模板</el-button>
        <el-button type="warning" :loading="testingInDialog" @click="handleTestFromDialog">测试发送</el-button>
        <el-button type="primary" :loading="saving" @click="saveConfig">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import {
  getNotificationChannelRules,
  updateNotificationChannelRules,
  getNotificationConfigMetadata,
  listNotificationConfigs,
  updateNotificationConfig,
  testNotificationConfig,
} from '@/services/control-plane'
import type {
  NotificationChannelRule,
  NotificationConfigMetadata,
  NotificationChannelConfig,
} from '@/types/settings'

const settingsStore = useSettingsStore()

// 模板可用变量：与后端真实渲染（service.renderTemplateString 注入的 Notification 字段）一致。
// 不要在此混入 registry 的业务变量（scope_id/percentage 等）——那些不会被渲染，会造成误导。
const TEMPLATE_VARIABLES = [
  'Title', 'Content', 'Level', 'NotificationType', 'RefType', 'RefID', 'Timestamp',
]

const loading = ref(false)
const saving = ref(false)
const savingCell = ref('')
const savingRuleInDialog = ref(false)
const testingInDialog = ref(false)
const metadata = ref<NotificationConfigMetadata | null>(null)
const rules = ref<NotificationChannelRule[]>([])
const configs = ref<NotificationChannelConfig[]>([])

const dialogVisible = ref(false)
const dialogTitle = ref('')
const editForm = ref<Partial<NotificationChannelConfig>>({})
const editChannel = ref('')

const emailEnabled = computed(() => settingsStore.systemSettings.email_enabled)

const typeRows = computed(() => metadata.value?.types ?? [])

async function refresh() {
  loading.value = true
  try {
    const [metaRes, ruleRes, configRes] = await Promise.all([
      getNotificationConfigMetadata(),
      getNotificationChannelRules(),
      listNotificationConfigs(),
    ])
    metadata.value = metaRes
    rules.value = ruleRes
    configs.value = configRes
  } catch {
    ElMessage.error('加载通知渠道设置失败')
  } finally {
    loading.value = false
  }
}

function ruleEnabled(notifType: string, channel: 'inapp' | 'email'): boolean {
  const r = rules.value.find(x => x.notification_type === notifType && x.channel === channel)
  return r ? r.enabled : true
}

type ChannelState = 'on' | 'off' | 'unset'

// 状态点 tooltip 文案
function stateLabel(state: ChannelState): string {
  if (state === 'on') return '生效中'
  if (state === 'off') return '已关闭'
  return '未配置'
}

// webhook 状态：有 config 行且 enabled 且 URL 非空 → 生效中；有行但关闭/无 URL → 已关闭；无行 → 未配置。
function webhookActive(notifType: string): ChannelState {
  const c = configs.value.find(x => x.notification_type === notifType && x.channel === 'webhook')
  if (!c) return 'unset'
  if (c.enabled && !!c.webhook_url) return 'on'
  return 'off'
}

// email 状态：全局总开关关闭 → off；rules 关闭 → off；config 行 enabled 决定 on/off；无行 → 未配置。
function emailEffective(notifType: string): ChannelState {
  if (!emailEnabled.value) return 'off'
  if (!ruleEnabled(notifType, 'email')) return 'off'
  const c = configs.value.find(x => x.notification_type === notifType && x.channel === 'email')
  if (c) return c.enabled ? 'on' : 'off'
  return 'unset'
}

async function saveRule(notifType: string, channel: 'inapp' | 'email', enabled: boolean) {
  savingCell.value = `${notifType}:${channel}`
  try {
    await updateNotificationChannelRules([{ notification_type: notifType, channel, enabled }])
    await refresh()
    ElMessage.success(enabled ? '已开启' : '已关闭')
  } catch {
    ElMessage.error('更新失败，请重试')
  } finally {
    savingCell.value = ''
  }
}

const editRuleEnabled = computed(() => {
  if (!editForm.value.notification_type) return true
  return ruleEnabled(editForm.value.notification_type, 'email')
})

async function toggleRuleInDialog(enabled: boolean) {
  const type = editForm.value.notification_type
  if (!type) return
  savingRuleInDialog.value = true
  try {
    await updateNotificationChannelRules([{ notification_type: type, channel: 'email', enabled }])
    await refresh()
    ElMessage.success(enabled ? '总开关已开启' : '总开关已关闭')
  } catch {
    ElMessage.error('更新失败')
  } finally {
    savingRuleInDialog.value = false
  }
}

function openEdit(notifType: string, channel: string) {
  const config = configs.value.find(x => x.notification_type === notifType && x.channel === channel)
  editForm.value = config
    ? { ...config }
    : {
        notification_type: notifType,
        channel,
        enabled: true,
        webhook_url: '',
        webhook_secret: '',
        subject_template: '',
        body_template: '',
      }
  editChannel.value = channel
  const t = metadata.value?.types.find(x => x.name === notifType)
  dialogTitle.value = `${t?.displayName ?? notifType} - ${channel === 'webhook' ? 'Webhook' : '邮件'}`
  dialogVisible.value = true
}

const saveConfig = async () => {
  const type = editForm.value.notification_type
  const channel = editForm.value.channel
  if (!type || !channel) {
    ElMessage.error('请填写完整信息')
    return
  }
  saving.value = true
  try {
    await updateNotificationConfig(type, channel, editForm.value)
    ElMessage.success('保存成功')
    dialogVisible.value = false
    await refresh()
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

const handleTestFromDialog = async () => {
  const type = editForm.value.notification_type
  const channel = editForm.value.channel
  if (!type || !channel) {
    ElMessage.warning('请先保存配置后再测试')
    return
  }
  testingInDialog.value = true
  try {
    const result = await testNotificationConfig(type, channel)
    if (result.success) {
      ElMessage.success(result.message || '测试发送成功')
    } else {
      ElMessage.warning(result.message || '测试发送失败')
    }
  } catch (error: any) {
    ElMessage.error('测试发送失败: ' + (error?.message || '未知错误'))
  } finally {
    testingInDialog.value = false
  }
}

const restoreDefault = () => {
  editForm.value.subject_template = ''
  editForm.value.body_template = ''
  ElMessage.info('已清空模板，保存后将使用系统默认模板')
}

onMounted(async () => {
  await settingsStore.loadSystemSettings()
  await refresh()
})
</script>

<style scoped>
.notification-channels-page {
  margin: -20px -24px;
  padding: 24px;
  min-height: calc(100vh - 120px);
}

@media (max-width: 768px) {
  .notification-channels-page {
    margin: -16px -16px;
    padding: 16px;
    min-height: calc(100vh - 100px);
  }
}

.page-header {
  margin-bottom: 24px;
}

@media (max-width: 768px) {
  .page-header {
    margin-bottom: 16px;
  }
}

.page-header h2 {
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 600;
}

.page-desc {
  margin: 0;
  color: #666;
  font-size: 14px;
}

@media (max-width: 768px) {
  .page-desc {
    font-size: 13px;
  }
}

.section-card {
  margin-bottom: 24px;
}

@media (max-width: 768px) {
  .section-card {
    margin-bottom: 16px;
  }
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

@media (max-width: 768px) {
  .card-header {
    flex-wrap: wrap;
    gap: 8px;
  }
}

.type-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 0;
}

.type-name {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}

.type-desc {
  font-size: 12px;
  color: #909399;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.channel-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.na {
  color: #c0c4cc;
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
}

.status-dot.on {
  background: #67c23a;
  box-shadow: 0 0 4px rgba(103, 194, 58, 0.5);
}

.status-dot.off {
  background: #f56c6c;
}

.status-dot.unset {
  background: #c0c4cc;
}

.hint {
  margin-top: 8px;
  font-size: 13px;
  color: #909399;
}

.form-hint {
  margin-left: 8px;
  font-size: 12px;
  color: #909399;
}

.readonly-hint {
  font-size: 13px;
  color: #909399;
  line-height: 1.5;
}

.template-hint {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
}

@media (max-width: 768px) {
  .section-card :deep(.el-table) {
    overflow-x: auto;
    display: block;
    width: 100%;
  }

  .section-card :deep(.el-table__body-wrapper) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .section-card :deep(.el-table__header-wrapper) {
    overflow-x: auto;
  }
}
</style>