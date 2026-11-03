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
        <div v-for="grp in categoryGroups" :key="grp.name" class="category-group">
          <div class="category-header">
            <span class="category-name">{{ grp.displayName }}</span>
            <span class="category-count">{{ grp.types.length }} 个类型</span>
          </div>
          <el-table :data="grp.types" border size="small" row-key="name">
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
        </div>

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

        <!-- Webhook：启用开关 + 端点列表管理器 -->
        <el-form-item v-if="editChannel === 'webhook'" label="启用">
          <el-switch v-model="editForm.enabled" />
        </el-form-item>

        <el-form-item v-if="editChannel === 'webhook'" label="Webhook 端点">
          <div class="target-list">
            <div v-if="editTargets.length === 0" class="target-empty">
              暂无端点。该类型的告警将发送到所有启用的端点，请先添加。
            </div>
            <div v-for="t in editTargets" :key="t._key" class="target-row">
              <div class="target-row-main">
                <el-input v-model="t.name" placeholder="名称(可选)" size="small" class="target-name" />
                <el-input v-model="t.url" placeholder="https://..." size="small" class="target-url" />
              </div>
              <div class="target-row-actions">
                <el-input
                  v-model="t.secret"
                  type="password"
                  show-password
                  size="small"
                  :placeholder="t.id && t.hasSecret ? '已设置，留空保持不变' : 'Secret(可选)'"
                  class="target-secret"
                />
                <el-switch v-model="t.enabled" size="small" />
                <el-button
                  v-if="t.id"
                  link
                  type="primary"
                  size="small"
                  :loading="testingTargetKey === t._key"
                  @click="testTarget(t)"
                >
                  测试
                </el-button>
                <el-button link type="danger" size="small" @click="removeTarget(t._key)">删除</el-button>
              </div>
            </div>
            <el-button size="small" @click="addTarget">添加端点</el-button>
            <div class="template-hint">一条告警发送到该类型下所有启用的端点（fan-out），各端点独立重试；模板为所有端点共享。</div>
          </div>
        </el-form-item>

        <template v-if="editChannel === 'email'">
          <el-form-item label="测试收件人">
            <el-input v-model="testEmailRecipient" placeholder="不填则发给当前登录用户" />
            <span class="form-hint">「测试发送」时的收件人（真实投递仍按通知接收人自动确定）</span>
          </el-form-item>
        </template>

        <el-form-item label="主题模板">
          <el-input v-model="editForm.subject_template" type="textarea" :rows="2" />
          <div class="template-hint">可用变量: {{ dialogVariables.join(', ') || '—' }}</div>
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
  listWebhookTargets,
  createWebhookTarget,
  updateWebhookTarget,
  deleteWebhookTarget,
  testWebhookTarget,
} from '@/services/control-plane'
import type {
  NotificationChannelRule,
  NotificationConfigMetadata,
  NotificationChannelConfig,
  NotificationWebhookTarget,
} from '@/types/settings'

const settingsStore = useSettingsStore()

const loading = ref(false)
const saving = ref(false)
const savingCell = ref('')
const savingRuleInDialog = ref(false)
const testingInDialog = ref(false)
const metadata = ref<NotificationConfigMetadata | null>(null)
const rules = ref<NotificationChannelRule[]>([])
const configs = ref<NotificationChannelConfig[]>([])
const webhookTargets = ref<NotificationWebhookTarget[]>([])

const dialogVisible = ref(false)
const dialogTitle = ref('')
const editForm = ref<Partial<NotificationChannelConfig>>({})
const editChannel = ref('')
const testEmailRecipient = ref('')

// Webhook 端点列表管理器（仅 webhook 对话框使用）
interface EditableTarget {
  _key: string
  id: string // 空 = 新增行
  name: string
  url: string
  secret: string // 用户新输入的 secret（不回显明文）
  hasSecret: boolean // 后端已存在非空 secret
  enabled: boolean
}
const editTargets = ref<EditableTarget[]>([])
const removedTargetIds = ref<string[]>([])
let targetKeySeq = 0

const emailEnabled = computed(() => settingsStore.systemSettings.email_enabled)

interface CategoryGroup {
  name: string
  displayName: string
  types: NonNullable<NotificationConfigMetadata['types']>
}

// 矩阵按业务域分类分组（顺序取 metadata.categories，类型按注册表原序过滤进组）。
const categoryGroups = computed<CategoryGroup[]>(() => {
  const cats = metadata.value?.categories ?? []
  const types = metadata.value?.types ?? []
  const groups: CategoryGroup[] = cats.map(c => ({
    name: c.name,
    displayName: c.displayName,
    types: types.filter(t => t.category === c.name),
  }))
  const covered = new Set(cats.map(c => c.name))
  const uncategorized = types.filter(t => !covered.has(t.category))
  if (uncategorized.length) {
    groups.push({ name: '_other', displayName: '其他', types: uncategorized })
  }
  return groups
})

// 对话框可用变量提示：通用注入变量 + 当前类型的 registry 业务变量（DefaultVariables）。
// 与后端真实渲染（service.renderForChannel 注入 TemplateData + Notification 字段）保持一致。
const dialogVariables = computed(() => {
  const generic = ['Title', 'Content', 'Level', 'NotificationType', 'RefType', 'RefID', 'Timestamp']
  const type = editForm.value.notification_type
  const def = metadata.value?.types.find((t) => t.name === type)
  if (!def || !def.defaultVariables?.length) return generic
  return [...def.defaultVariables, ...generic]
})

async function refresh() {
  loading.value = true
  try {
    const [metaRes, ruleRes, configRes, targetsRes] = await Promise.all([
      getNotificationConfigMetadata(),
      getNotificationChannelRules(),
      listNotificationConfigs(),
      listWebhookTargets(),
    ])
    metadata.value = metaRes
    rules.value = ruleRes
    configs.value = configRes
    webhookTargets.value = targetsRes
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

// webhook 状态聚合：config 行 enabled &&（有任一 enabled 端点 || 无端点但有 legacy URL）→ 生效中。
// 与后端 resolveWebhookDestinations 语义一致：有端点时只看端点（不回退 legacy URL）；无端点时回退 config 行 URL。
function webhookActive(notifType: string): ChannelState {
  const c = configs.value.find(x => x.notification_type === notifType && x.channel === 'webhook')
  if (!c) return 'unset'
  if (!c.enabled) return 'off'
  const targets = webhookTargets.value.filter(t => t.notification_type === notifType)
  if (targets.length === 0) {
    return c.webhook_url ? 'on' : 'off'
  }
  return targets.some(t => t.enabled) ? 'on' : 'off'
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
    ? { ...config, webhook_url: undefined, webhook_secret: undefined }
    : {
        notification_type: notifType,
        channel,
        enabled: true,
        subject_template: '',
        body_template: '',
      }
  editChannel.value = channel
  testEmailRecipient.value = ''
  removedTargetIds.value = []
  editTargets.value = channel === 'webhook'
    ? webhookTargets.value
        .filter(t => t.notification_type === notifType)
        .map(t => ({
          _key: `k${++targetKeySeq}`,
          id: t.id,
          name: t.name,
          url: t.url,
          secret: '',
          hasSecret: !!t.secret,
          enabled: t.enabled,
        }))
    : []
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
    if (channel === 'webhook') {
      // 先校验全部新增端点 URL，避免先写 config 后失败产生半保存状态
      for (const t of editTargets.value) {
        if (!t.id && !t.url) {
          ElMessage.error('新增端点必须填写 URL')
          return
        }
      }
      // config 行：仅 enabled + 模板（URL/secret 已迁移到端点列表，legacy URL 不在此维护）
      await updateNotificationConfig(type, 'webhook', {
        enabled: editForm.value.enabled ?? true,
        subject_template: editForm.value.subject_template ?? '',
        body_template: editForm.value.body_template ?? '',
      })
      for (const t of editTargets.value) {
        if (!t.id) {
          await createWebhookTarget(type, {
            name: t.name,
            url: t.url,
            secret: t.secret || null,
            enabled: t.enabled,
            sort_order: 0,
          })
        } else {
          const body: Record<string, unknown> = { name: t.name, enabled: t.enabled }
          if (t.url) body.url = t.url
          // secret 仅用户显式输入新值时携带；留空 = 保持不变（不回显明文）
          if (t.secret !== '') body.secret = t.secret
          await updateWebhookTarget(type, t.id, body)
        }
      }
      for (const id of removedTargetIds.value) {
        await deleteWebhookTarget(type, id)
      }
    } else {
      await updateNotificationConfig(type, channel, editForm.value)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    await refresh()
  } catch (error: any) {
    ElMessage.error('保存失败: ' + (error?.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

function addTarget() {
  editTargets.value.push({
    _key: `k${++targetKeySeq}`,
    id: '',
    name: '',
    url: '',
    secret: '',
    hasSecret: false,
    enabled: true,
  })
}

function removeTarget(key: string) {
  const idx = editTargets.value.findIndex(t => t._key === key)
  if (idx < 0) return
  const [row] = editTargets.value.splice(idx, 1)
  if (row.id) removedTargetIds.value.push(row.id)
}

const testingTargetKey = ref('')

async function testTarget(t: EditableTarget) {
  const type = editForm.value.notification_type
  if (!type || !t.id) return
  testingTargetKey.value = t._key
  try {
    const result = await testWebhookTarget(type, t.id)
    if (result.success) {
      ElMessage.success(result.message || '测试发送成功')
    } else {
      ElMessage.warning(result.message || '测试发送失败')
    }
  } catch (error: any) {
    ElMessage.error('测试发送失败: ' + (error?.message || '未知错误'))
  } finally {
    testingTargetKey.value = ''
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
    const result = await testNotificationConfig(
      type,
      channel,
      channel === 'email' ? testEmailRecipient.value || undefined : undefined,
    )
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

.category-group {
  margin-bottom: 20px;
}

.category-group:last-child {
  margin-bottom: 0;
}

.category-header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 6px 12px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-bottom: none;
  border-radius: 6px 6px 0 0;
}

.category-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.category-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.category-group .el-table {
  border-radius: 0 0 6px 6px;
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

/* Webhook 端点列表管理器 */
.target-list {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.target-empty {
  font-size: 13px;
  color: #909399;
  padding: 8px 0;
}

.target-row {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.target-row-main {
  display: flex;
  gap: 8px;
}

.target-row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.target-name {
  flex: 0 0 140px;
}

.target-url {
  flex: 1;
}

.target-secret {
  flex: 1;
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