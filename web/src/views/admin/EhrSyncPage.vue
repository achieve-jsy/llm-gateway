<template>
  <div class="ehr-sync-page">
    <!-- 页面标题 -->
    <header class="page-header">
      <div class="header-meta">
        <div class="header-kicker">运维 · 数据同步</div>
        <h2 class="header-title">组织数据同步</h2>
        <p class="header-desc">
          对接大群 EHR 接口，实时同步组织架构、部门与员工数据
        </p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" @click="refreshAll" :loading="loading">刷新</el-button>
        <el-button
          type="primary"
          :icon="VideoPlay"
          @click="handleTrigger('incremental')"
          :loading="triggering"
        >
          立即增量同步
        </el-button>
        <el-button
          type="warning"
          :icon="RefreshRight"
          @click="handleTrigger('full')"
          :loading="triggering"
        >
          全量对账
        </el-button>
      </div>
    </header>

    <!-- 卡片 1：运行状态 -->
    <section class="section-card status-card">
      <div class="card-title-row">
        <h3 class="card-title">同步状态</h3>
        <el-tag
          :type="schedulerTagType"
          size="small"
          effect="dark"
        >
          {{ schedulerRunning ? '调度器运行中' : '调度器已停止' }}
        </el-tag>
      </div>

      <div class="status-grid">
        <div class="status-block">
          <div class="status-label">最近一次执行</div>
          <div class="status-value" v-if="latestExecution">
            <el-tag :type="statusType(latestExecution.status)" size="small" effect="plain">
              {{ executionStatusText(latestExecution.status) }}
            </el-tag>
            <span class="value-sub">{{ formatTime(latestExecution.started_at) }}</span>
            <span class="value-sub">耗时 {{ latestExecution.duration_ms }}ms</span>
          </div>
          <div class="status-empty" v-else>暂无执行记录</div>
        </div>
        <div class="status-block">
          <div class="status-label">类型 / Batch</div>
          <div class="status-value" v-if="latestExecution">
            <el-tag size="small">{{ latestExecution.sync_type }}</el-tag>
            <span class="value-mono">{{ latestExecution.batch_id }}</span>
          </div>
          <div class="status-empty" v-else>-</div>
        </div>
        <div class="status-block">
          <div class="status-label">本次影响行数</div>
          <div class="status-value" v-if="latestExecution">
            <div class="stat-row">
              <span><b>拉取</b> {{ latestExecution.records_fetched }}</span>
              <span><b>新增</b> {{ latestExecution.records_created }}</span>
              <span><b>更新</b> {{ latestExecution.records_updated }}</span>
            </div>
            <div class="stat-row">
              <span><b>跳过</b> {{ latestExecution.records_skipped }}</span>
              <span><b>删除</b> {{ latestExecution.records_deleted }}</span>
            </div>
          </div>
          <div class="status-empty" v-else>-</div>
        </div>
      </div>

      <div class="status-error" v-if="latestExecution?.error_msg">
        <el-icon color="#F56C6C"><WarningFilled /></el-icon>
        <span>{{ latestExecution.error_msg }}</span>
      </div>
    </section>

    <!-- 卡片 2：数据量统计 -->
    <section class="section-card data-card">
      <h3 class="card-title">数据规模</h3>
      <div class="data-grid" v-if="snapshot">
        <div class="data-tile">
          <div class="tile-value">{{ snapshot.total_organizations }}</div>
          <div class="tile-label">组织</div>
        </div>
        <div class="data-tile">
          <div class="tile-value">{{ snapshot.total_companies }}</div>
          <div class="tile-label">公司</div>
        </div>
        <div class="data-tile">
          <div class="tile-value">{{ snapshot.total_departments }}</div>
          <div class="tile-label">部门</div>
        </div>
        <div class="data-tile">
          <div class="tile-value">{{ snapshot.total_employees }}</div>
          <div class="tile-label">员工总数</div>
        </div>
        <div class="data-tile">
          <div class="tile-value">{{ snapshot.active_employees }}</div>
          <div class="tile-label">在职员工</div>
        </div>
        <div class="data-tile warn">
          <div class="tile-value">{{ snapshot.override_count }}</div>
          <div class="tile-label">手动锁定</div>
        </div>
      </div>
      <div class="status-empty" v-else>尚未产生同步快照</div>

      <div class="snapshot-meta" v-if="snapshot">
        <span>快照时间：{{ formatTime(snapshot.synced_at) }}</span>
        <span>EHR API 调用：<b>{{ snapshot.ehr_call_count }}</b> 次 · 失败 <b class="danger">{{ snapshot.ehr_error_count }}</b> 次</span>
        <span>增量耗时：<b>{{ snapshot.last_incremental_ms }}ms</b></span>
        <span>对账耗时：<b>{{ snapshot.last_full_reconcile_ms }}ms</b></span>
      </div>
    </section>

    <!-- 卡片 3：Override 管理 + 执行日志 -->
    <section class="section-card tabs-card">
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="`Override 管理 (${overrideTotal})`" name="overrides">
          <div class="override-toolbar">
            <el-radio-group v-model="overrideType" size="small">
              <el-radio-button label="all">全部 ({{ overrideTotal }})</el-radio-button>
              <el-radio-button label="department">部门 ({{ overrides.departments.length }})</el-radio-button>
              <el-radio-button label="employee">员工 ({{ overrides.employees.length }})</el-radio-button>
            </el-radio-group>
            <el-button size="small" :icon="Refresh" @click="loadOverrides" :loading="loadingOverrides">刷新</el-button>
          </div>

          <el-table :data="overrideList" stripe style="width: 100%" v-loading="loadingOverrides" empty-text="暂无 override">
            <el-table-column prop="entity_type" label="类型" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.entity_type === 'department' ? 'primary' : 'success'">
                  {{ row.entity_type === 'department' ? '部门' : '员工' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="entity_id" label="ID" width="160" />
            <el-table-column prop="org_id" label="Org ID" width="140" />
            <el-table-column prop="display_name" label="名称" min-width="180" />
            <el-table-column prop="manual_override_by" label="操作人" width="140" />
            <el-table-column prop="manual_override_reason" label="原因" min-width="220" show-overflow-tooltip />
            <el-table-column prop="manual_override_at" label="锁定时间" width="180">
              <template #default="{ row }">{{ formatTime(row.manual_override_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-popconfirm
                  title="确定释放 override？下次同步将恢复 EHR 数据"
                  confirm-button-text="确定"
                  cancel-button-text="取消"
                  @confirm="handleRelease(row)"
                >
                  <template #reference>
                    <el-button size="small" type="warning" :loading="row._releasing">释放</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="执行日志" name="executions">
          <el-table :data="executions" stripe v-loading="loadingExecutions" empty-text="暂无执行记录">
            <el-table-column prop="batch_id" label="Batch ID" width="340">
              <template #default="{ row }">
                <span class="value-mono">{{ row.batch_id }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="sync_type" label="类型" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.sync_type === 'full' ? 'warning' : 'primary'">
                  {{ row.sync_type }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="120">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)" effect="plain">
                  {{ executionStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="started_at" label="开始时间" width="180">
              <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
            </el-table-column>
            <el-table-column prop="duration_ms" label="耗时(ms)" width="110" />
            <el-table-column label="F/U/S/D" width="200">
              <template #default="{ row }">
                <span><b>F</b>{{ row.records_fetched }}</span>
                <span style="margin-left:8px"><b>U</b>{{ row.records_updated }}</span>
                <span style="margin-left:8px"><b>S</b>{{ row.records_skipped }}</span>
                <span style="margin-left:8px"><b>D</b>{{ row.records_deleted }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="error_msg" label="错误" min-width="220" show-overflow-tooltip />
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="变更日志" name="changes">
          <div class="change-toolbar">
            <el-select v-model="changeFilter.entity" placeholder="实体类型" clearable size="small" @change="loadChanges" style="width:140px">
              <el-option label="全部" value="" />
              <el-option label="organization" value="organization" />
              <el-option label="company" value="company" />
              <el-option label="department" value="department" />
              <el-option label="employee" value="employee" />
            </el-select>
            <el-select v-model="changeFilter.change_type" placeholder="变更类型" clearable size="small" @change="loadChanges" style="width:140px">
              <el-option label="全部" value="" />
              <el-option label="created" value="created" />
              <el-option label="modified" value="modified" />
              <el-option label="deleted" value="deleted" />
              <el-option label="transferred" value="transferred" />
            </el-select>
            <el-input v-model.trim="changeFilter.entity_id" placeholder="Entity ID" clearable size="small" style="width:180px" @clear="loadChanges" />
            <el-button size="small" type="primary" @click="loadChanges" :loading="loadingChanges">筛选</el-button>
          </div>

          <el-table :data="changes" stripe v-loading="loadingChanges" empty-text="暂无变更记录">
            <el-table-column prop="occurred_at" label="时间" width="180">
              <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
            </el-table-column>
            <el-table-column prop="entity" label="实体" width="120">
              <template #default="{ row }">
                <el-tag size="small">{{ row.entity }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="change_type" label="类型" width="110">
              <template #default="{ row }">
                <el-tag size="small" :type="changeTypeTag(row.change_type)">{{ row.change_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="entity_id" label="Entity ID" width="200" />
            <el-table-column label="Old → New" min-width="400">
              <template #default="{ row }">
                <pre class="json-snippet">{{ prettyDiff(row.old_value, row.new_value) }}</pre>
              </template>
            </el-table-column>
          </el-table>

          <div class="pager" v-if="changeTotal > changePageSize">
            <el-pagination
              background
              layout="prev, pager, next, total"
              :total="changeTotal"
              :page-size="changePageSize"
              v-model:current-page="changePage"
              @current-change="loadChanges"
            />
          </div>
        </el-tab-pane>

        <el-tab-pane label="错误日志" name="errors">
          <el-table :data="errors" stripe v-loading="loadingErrors" empty-text="暂无错误">
            <el-table-column prop="occurred_at" label="时间" width="180">
              <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
            </el-table-column>
            <el-table-column prop="entity" label="实体" width="120" />
            <el-table-column prop="status_code" label="HTTP" width="90" />
            <el-table-column prop="method" label="方法" min-width="260" show-overflow-tooltip />
            <el-table-column prop="attempt_num" label="第N次" width="90" />
            <el-table-column prop="error_msg" label="错误信息" min-width="320" show-overflow-tooltip />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Refresh, VideoPlay, RefreshRight, WarningFilled
} from '@element-plus/icons-vue'
import type {
  EhrSyncStatus, EhrSyncExecutionLog, EhrSyncStatSnapshot,
  EhrDepartment, EhrEmployee
} from '@/api/ehr'
import {
  getEhrStatus, getEhrOverrides, triggerEhrSync,
  getEhrExecutionLogs, getEhrChangeLogs, getEhrErrorLogs,
  releaseEhrOverride
} from '@/api/ehr'

// ============ 状态 ============

const loading = ref(false)
const triggering = ref(false)
const schedulerRunning = ref(false)

const latestExecution = ref<EhrSyncExecutionLog | null>(null)
const snapshot = ref<EhrSyncStatSnapshot | null>(null)

const activeTab = ref<'overrides' | 'executions' | 'changes' | 'errors'>('overrides')

// Override
const overrides = ref<{ departments: EhrDepartment[]; employees: EhrEmployee[]; total: number }>({
  departments: [], employees: [], total: 0
})
const overrideType = ref<'all' | 'department' | 'employee'>('all')
const loadingOverrides = ref(false)

// Executions / Changes / Errors
const executions = ref<EhrSyncExecutionLog[]>([])
const loadingExecutions = ref(false)

const changes = ref<any[]>([])
const changeTotal = ref(0)
const changePage = ref(1)
const changePageSize = 20
const changeFilter = ref<{ entity: string; change_type: string; entity_id: string }>({
  entity: '', change_type: '', entity_id: ''
})
const loadingChanges = ref(false)

const errors = ref<any[]>([])
const loadingErrors = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null

// ============ computed ============

const schedulerTagType = computed<'success' | 'info'>(() =>
  schedulerRunning.value ? 'success' : 'info'
)

const overrideTotal = computed(() => overrides.value.total)

const overrideList = computed(() => {
  const merged: any[] = []
  if (overrideType.value !== 'employee') {
    for (const d of overrides.value.departments) {
      merged.push({
        entity_type: 'department',
        entity_id: d.department_id,
        org_id: d.org_id,
        display_name: d.department_name,
        manual_override_at: d.manual_override_at,
        manual_override_by: d.manual_override_by,
        manual_override_reason: d.manual_override_reason
      })
    }
  }
  if (overrideType.value !== 'department') {
    for (const e of overrides.value.employees) {
      merged.push({
        entity_type: 'employee',
        entity_id: e.employee_id,
        org_id: e.org_id,
        display_name: e.person_name || e.employee_id,
        manual_override_at: e.manual_override_at,
        manual_override_by: e.manual_override_by,
        manual_override_reason: e.manual_override_reason
      })
    }
  }
  return merged.sort((a, b) => (b.manual_override_at || '').localeCompare(a.manual_override_at || ''))
})

// ============ 数据加载 ============

async function loadStatus() {
  try {
    const res = await getEhrStatus()
    schedulerRunning.value = res.data.scheduler_running
    latestExecution.value = res.data.latest_execution
    snapshot.value = res.data.latest_snapshot
  } catch (e: any) {
    console.error('loadStatus failed', e)
  }
}

async function loadOverrides() {
  loadingOverrides.value = true
  try {
    overrides.value = (await getEhrOverrides()).data
  } catch (e: any) {
    ElMessage.error('加载 override 列表失败：' + (e?.message || e))
  } finally {
    loadingOverrides.value = false
  }
}

async function loadExecutions() {
  loadingExecutions.value = true
  try {
    const res = await getEhrExecutionLogs({ page: 1, page_size: 50 })
    executions.value = (res as any).data || []
  } catch (e: any) {
    console.error('loadExecutions failed', e)
  } finally {
    loadingExecutions.value = false
  }
}

async function loadChanges() {
  loadingChanges.value = true
  try {
    const res = await getEhrChangeLogs({
      entity: changeFilter.value.entity || undefined,
      change_type: changeFilter.value.change_type || undefined,
      entity_id: changeFilter.value.entity_id || undefined,
      page: changePage.value,
      page_size: changePageSize
    })
    changes.value = (res as any).data || []
    changeTotal.value = (res as any).total || 0
  } catch (e: any) {
    console.error('loadChanges failed', e)
  } finally {
    loadingChanges.value = false
  }
}

async function loadErrors() {
  loadingErrors.value = true
  try {
    const res = await getEhrErrorLogs({ page: 1, page_size: 50 })
    errors.value = (res as any).data || []
  } catch (e: any) {
    console.error('loadErrors failed', e)
  } finally {
    loadingErrors.value = false
  }
}

async function refreshAll() {
  loading.value = true
  try {
    await Promise.all([
      loadStatus(),
      loadOverrides(),
      loadExecutions(),
      loadChanges(),
      loadErrors()
    ])
  } finally {
    loading.value = false
  }
}

function handleTabChange(tab: string) {
  if (tab === 'overrides' && overrides.value.total === 0) loadOverrides()
  if (tab === 'executions' && executions.value.length === 0) loadExecutions()
  if (tab === 'changes' && changes.value.length === 0) loadChanges()
  if (tab === 'errors' && errors.value.length === 0) loadErrors()
}

// ============ 操作 ============

async function handleTrigger(type: 'incremental' | 'full') {
  try {
    triggering.value = true
    await triggerEhrSync(type)
    ElMessage.success(`已触发 ${type === 'full' ? '全量对账' : '增量同步'} 任务`)
    // 轮询直到任务完成（最长等 30s）
    startPollUntilDone(30_000)
  } catch (e: any) {
    ElMessage.error('触发同步失败：' + (e?.response?.data?.error?.message || e?.message || e))
  } finally {
    triggering.value = false
  }
}

function startPollUntilDone(timeoutMs: number) {
  if (pollTimer) clearInterval(pollTimer)
  const started = Date.now()
  pollTimer = setInterval(async () => {
    await loadStatus()
    if (Date.now() - started > timeoutMs ||
        (latestExecution.value && latestExecution.value.status !== 'running')) {
      clearInterval(pollTimer!)
      pollTimer = null
      loadExecutions() // 顺便刷执行日志
    }
  }, 2000)
}

async function handleRelease(row: any) {
  row._releasing = true
  try {
    await releaseEhrOverride(row.entity_type, row.org_id, row.entity_id)
    ElMessage.success('已释放 override')
    await loadOverrides()
    await loadStatus()
  } catch (e: any) {
    ElMessage.error('释放失败：' + (e?.response?.data?.error?.message || e?.message || e))
  } finally {
    row._releasing = false
  }
}

// ============ 工具 ============

function formatTime(t: string | null | undefined) {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return t
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function statusType(s: string | undefined): 'success' | 'warning' | 'danger' | 'info' {
  if (!s) return 'info'
  switch (s) {
    case 'completed': return 'success'
    case 'partial_success': return 'warning'
    case 'failed': return 'danger'
    case 'running': return 'info'
    default: return 'info'
  }
}

function executionStatusText(s: string | undefined) {
  if (!s) return '-'
  switch (s) {
    case 'completed': return '已完成'
    case 'partial_success': return '部分成功'
    case 'failed': return '失败'
    case 'running': return '执行中'
    default: return s
  }
}

function changeTypeTag(t: string) {
  switch (t) {
    case 'created': return 'success'
    case 'modified': return 'warning'
    case 'deleted': return 'danger'
    case 'transferred': return 'primary'
    default: return 'info'
  }
}

function prettyDiff(oldVal: string, newVal: string): string {
  try {
    const a = JSON.parse(oldVal || '{}')
    const b = JSON.parse(newVal || '{}')
    if (Object.keys(a).length === 0 && Object.keys(b).length === 0) return '-'
    const parts: string[] = []
    const allKeys = new Set([...Object.keys(a), ...Object.keys(b)])
    for (const k of allKeys) {
      if (JSON.stringify(a[k]) !== JSON.stringify(b[k])) {
        parts.push(`${k}: ${JSON.stringify(a[k] ?? null)} → ${JSON.stringify(b[k] ?? null)}`)
      }
    }
    return parts.join('\n') || '(无差异)'
  } catch {
    return `${oldVal}\n→\n${newVal}`
  }
}

// ============ 生命周期 ============

onMounted(async () => {
  await loadStatus()
  await loadOverrides()
})

onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.ehr-sync-page {
  padding: 24px 28px;
  max-width: 1400px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 24px;
  padding: 20px 24px;
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 10px;
}
.header-kicker {
  font-size: 12px;
  color: #909399;
  letter-spacing: 1px;
  text-transform: uppercase;
  margin-bottom: 4px;
}
.header-title {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
  color: #303133;
}
.header-desc {
  margin: 6px 0 0;
  color: #606266;
  font-size: 13px;
}
.header-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.section-card {
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 10px;
  padding: 18px 22px;
}
.card-title {
  margin: 0 0 14px;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}
.card-title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
}
.card-title-row .card-title { margin: 0; }

.status-grid, .data-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 14px;
}
.status-block {
  padding: 12px 14px;
  background: #f6f8fb;
  border-radius: 8px;
  border: 1px solid #ebeef5;
}
.status-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 6px;
}
.status-value {
  font-size: 13px;
  color: #303133;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.value-sub {
  color: #606266;
}
.value-mono {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 11px;
  color: #606266;
}
.status-empty { color: #b0b4bb; font-size: 13px; }
.stat-row { display: flex; gap: 10px; font-size: 13px; }
.stat-row + .stat-row { margin-top: 4px; }
.stat-row b { color: #303133; margin-right: 3px; }

.status-error {
  margin-top: 14px;
  padding: 10px 14px;
  background: #fef0f0;
  border: 1px solid #fde2e2;
  border-radius: 6px;
  color: #F56C6C;
  font-size: 13px;
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.status-error span { white-space: pre-wrap; word-break: break-all; }

.data-grid { grid-template-columns: repeat(6, 1fr); }
.data-tile {
  text-align: center;
  padding: 16px 10px 14px;
  background: #f6f8fb;
  border: 1px solid #ebeef5;
  border-radius: 8px;
}
.data-tile .tile-value {
  font-size: 28px;
  font-weight: 700;
  color: #409eff;
  font-variant-numeric: tabular-nums;
}
.data-tile.warn .tile-value { color: #e6a23c; }
.data-tile .tile-label {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
}
.snapshot-meta {
  margin-top: 14px;
  padding: 10px 14px;
  background: #f9fafb;
  border-radius: 6px;
  font-size: 12px;
  color: #606266;
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}
.danger { color: #F56C6C; }

/* Tabs card */
.tabs-card { padding: 4px 8px 8px; }
.tabs-card :deep(.el-tabs__header) { margin-bottom: 8px; padding: 0 14px; }
.tabs-card :deep(.el-tabs__content) { padding: 8px 14px 4px; }

.override-toolbar, .change-toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.json-snippet {
  margin: 0;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  color: #606266;
  line-height: 1.5;
  max-height: 200px;
  overflow: auto;
}

.pager { margin-top: 12px; display: flex; justify-content: flex-end; }

/* 适配暗色主题 */
:deep(body.dark-mode) .ehr-sync-page { color: #e8e8e8; }
:deep(body.dark-mode) .page-header,
:deep(body.dark-mode) .section-card {
  background: #1f1f1f;
  border-color: #333;
}
:deep(body.dark-mode) .status-block,
:deep(body.dark-mode) .data-tile {
  background: #262626;
  border-color: #333;
}
:deep(body.dark-mode) .header-title,
:deep(body.dark-mode) .card-title,
:deep(body.dark-mode) .status-value,
:deep(body.dark-mode) .stat-row b,
:deep(body.dark-mode) .data-tile .tile-value { color: #f0f0f0; }

@media (max-width: 900px) {
  .status-grid { grid-template-columns: 1fr; }
  .data-grid { grid-template-columns: repeat(2, 1fr); }
  .page-header { flex-direction: column; }
}
</style>
