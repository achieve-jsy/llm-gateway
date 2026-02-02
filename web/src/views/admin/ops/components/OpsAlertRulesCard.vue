<template>
  <div class="ops-card">
    <div class="ops-card-header">
      <h3 class="ops-card-title">告警规则</h3>
      <el-button size="small" type="primary" @click="openCreate">新建规则</el-button>
    </div>

    <div v-loading="loading" class="ops-rules-body">
      <table class="ops-table" v-if="rules.length > 0">
        <thead>
          <tr>
            <th>规则名称</th>
            <th>指标类型</th>
            <th>条件</th>
            <th>严重程度</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rule in rules" :key="rule.id">
            <td>{{ rule.name }}</td>
            <td><code class="ops-code">{{ rule.metric_type }}</code></td>
            <td>{{ rule.operator }} {{ rule.threshold }}</td>
            <td>
              <el-tag size="small" :type="severityTagType(rule.severity)" effect="dark">{{ rule.severity }}</el-tag>
            </td>
            <td>
              <el-switch
                :model-value="rule.enabled"
                inline-prompt
                active-text="开"
                inactive-text="关"
                @change="toggleEnabled(rule)"
              />
            </td>
            <td class="ops-td-actions">
              <el-button size="small" type="primary" link @click="openEdit(rule)">编辑</el-button>
              <el-button size="small" type="danger" link @click="handleDelete(rule)">删除</el-button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="ops-card-empty">
        {{ loading ? '加载中...' : '暂无规则，点击"新建规则"创建' }}
      </div>
    </div>

    <!-- Edit / Create Dialog -->
    <el-dialog
      :model-value="showDialog"
      :title="editingRule ? '编辑规则' : '新建规则'"
      width="560px"
      @update:model-value="showDialog = $event"
    >
      <el-form :model="form" label-position="top" class="ops-form">
        <el-form-item label="规则名称" required>
          <el-input v-model="form.name" placeholder="例: CPU 使用率过高" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="规则说明（可选）" />
        </el-form-item>
        <el-form-item label="指标类型" required>
          <el-select v-model="form.metric_type" class="ops-form-select" placeholder="选择指标">
            <el-option-group label="系统指标">
              <el-option label="CPU 使用率" value="cpu_usage_percent" />
              <el-option label="内存使用率" value="memory_usage_percent" />
              <el-option label="并发队列深度" value="concurrency_queue_depth" />
            </el-option-group>
            <el-option-group label="渠道组指标">
              <el-option label="渠道组成功率" value="success_rate" />
              <el-option label="渠道组错误率" value="error_rate" />
              <el-option label="上游错误率" value="upstream_error_rate" />
              <el-option label="组可用账号数" value="group_available_accounts" />
              <el-option label="组可用比例" value="group_available_ratio" />
              <el-option label="组限流比例" value="group_rate_limit_ratio" />
            </el-option-group>
            <el-option-group label="账号指标">
              <el-option label="账号限流次数" value="account_rate_limited_count" />
              <el-option label="账号错误次数" value="account_error_count" />
              <el-option label="账号错误率" value="account_error_ratio" />
              <el-option label="过载账号数" value="overload_account_count" />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item label="触发条件" required>
          <div class="ops-form-inline">
            <el-select v-model="form.operator" class="ops-form-select-sm">
              <el-option label=">" value=">" />
              <el-option label=">=" value=">=" />
              <el-option label="<" value="<" />
              <el-option label="<=" value="<=" />
              <el-option label="==" value="==" />
              <el-option label="!=" value="!=" />
            </el-select>
            <el-input-number v-model="form.threshold" :min="0" :step="1" class="ops-form-input-number" />
          </div>
        </el-form-item>
        <div class="ops-form-row">
          <el-form-item label="统计窗口 (分钟)">
            <el-input-number v-model="form.window_minutes" :min="1" :max="60" />
          </el-form-item>
          <el-form-item label="持续触发 (分钟)">
            <el-input-number v-model="form.sustained_minutes" :min="1" :max="60" />
          </el-form-item>
        </div>
        <div class="ops-form-row">
          <el-form-item label="严重程度" required>
            <el-select v-model="form.severity" class="ops-form-select">
              <el-option label="P0 - 紧急" value="P0" />
              <el-option label="P1 - 严重" value="P1" />
              <el-option label="P2 - 警告" value="P2" />
              <el-option label="P3 - 提示" value="P3" />
            </el-select>
          </el-form-item>
          <el-form-item label="冷却时间 (分钟)">
            <el-input-number v-model="form.cooldown_minutes" :min="1" :max="1440" />
          </el-form-item>
        </div>
        <el-form-item label="邮件通知">
          <el-switch v-model="form.notify_email" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { AlertRule, MetricType, Operator, OpsSeverity } from '@/api/ops'

const loading = ref(false)
const saving = ref(false)
const rules = ref<AlertRule[]>([])
const showDialog = ref(false)
const editingRule = ref<AlertRule | null>(null)

function defaultForm(): Omit<AlertRule, 'id' | 'created_at' | 'updated_at'> {
  return {
    name: '',
    description: '',
    enabled: true,
    metric_type: 'cpu_usage_percent' as MetricType,
    operator: '>' as Operator,
    threshold: 80,
    window_minutes: 5,
    sustained_minutes: 3,
    severity: 'P2' as OpsSeverity,
    cooldown_minutes: 15,
    notify_email: true,
  }
}

const form = ref<Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>>(defaultForm())

function severityTagType(sev: OpsSeverity): 'danger' | 'warning' | 'info' {
  if (sev === 'P0' || sev === 'P1') return 'danger'
  if (sev === 'P2') return 'warning'
  return 'info'
}

async function fetchRules() {
  loading.value = true
  try {
    rules.value = await opsAPI.listAlertRules()
  } catch {
    ElMessage.error('加载规则失败')
  } finally {
    loading.value = false
  }
}

onMounted(fetchRules)

function openCreate() {
  editingRule.value = null
  form.value = defaultForm()
  showDialog.value = true
}

function openEdit(rule: AlertRule) {
  editingRule.value = rule
  form.value = { ...rule }
  showDialog.value = true
}

async function handleSave() {
  if (!form.value.name) {
    ElMessage.warning('请填写规则名称')
    return
  }
  saving.value = true
  try {
    if (editingRule.value?.id) {
      await opsAPI.updateAlertRule(editingRule.value.id, form.value)
      ElMessage.success('规则已更新')
    } else {
      await opsAPI.createAlertRule(form.value)
      ElMessage.success('规则已创建')
    }
    showDialog.value = false
    await fetchRules()
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(rule: AlertRule) {
  if (!rule.id) return
  try {
    await opsAPI.updateAlertRule(rule.id, { enabled: !rule.enabled })
    await fetchRules()
  } catch {
    ElMessage.error('更新失败')
  }
}

async function handleDelete(rule: AlertRule) {
  if (!rule.id) return
  try {
    await ElMessageBox.confirm(`确定删除规则「${rule.name}」？删除后不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
    await opsAPI.deleteAlertRule(rule.id)
    ElMessage.success('规则已删除')
    await fetchRules()
  } catch {
    // cancelled or errored
  }
}
</script>

<style scoped>
.ops-card {
  background: var(--bg-card);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-3xl);
  padding: var(--space-4);
}

.ops-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-3);
}

.ops-card-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  margin: 0;
}

.ops-rules-body {
  overflow-x: auto;
}

.ops-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-xs);
}

.ops-table th {
  text-align: left;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-secondary);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

.ops-table td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-primary);
}

.ops-code {
  font-family: var(--font-family-mono);
  font-size: var(--font-size-xxs);
  background: var(--bg-subtle);
  padding: 2px 6px;
  border-radius: 4px;
  color: #14b8a6;
}

.ops-td-actions {
  white-space: nowrap;
}

.ops-card-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 120px;
  color: var(--text-placeholder);
  font-size: var(--font-size-xs);
}

.ops-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ops-form-select {
  width: 100%;
}

.ops-form-select-sm {
  width: 100px;
}

.ops-form-input-number {
  flex: 1;
}

.ops-form-inline {
  display: flex;
  gap: 8px;
  width: 100%;
}

.ops-form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
</style>