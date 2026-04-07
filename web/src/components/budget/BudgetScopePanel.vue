<template>
  <div class="budget-scope-panel" :class="{ 'is-compact': compact }">
    <!-- 完整 summary：非 compact 模式显示 -->
    <div v-if="!compact" class="budget-summary">
      <div class="summary-main">
        <div class="summary-eyebrow">{{ scopeType === 'studio' ? '工作室预算' : '部门预算' }}</div>
        <div class="summary-title">{{ title }}</div>
        <div class="summary-meta">预算按 USD 计费，月额度为硬限制；周/日额度用于节奏控制。</div>
      </div>
      <!-- 部门视图才在顶部 summary 里显示数字，工作室视图由下方池卡片承载 -->
      <div v-if="scopeType !== 'studio'" class="summary-numbers">
        <div class="summary-amount">
          <strong>{{ formatUSD(totalUsed, { compact: true }) }}</strong>
          <span>/ {{ formatUSD(totalAmount, { compact: true }) }}</span>
        </div>
        <el-progress
          :percentage="Math.min(Math.round(totalRatio * 100), 100)"
          :stroke-width="8"
          :color="progressColor(totalRatio)"
        />
      </div>
    </div>

    <!-- compact 模式：仅显示部门日常池一行状态，聚焦成员配额 -->
    <div v-if="compact && scopeType === 'dept'" class="compact-pool-status">
      <template v-for="pool in deptPools" :key="pool.scope_type">
        <div v-if="pool.scope_type === 'dept_daily'" class="compact-pool-row">
          <span class="compact-pool-label">成员日常预算池</span>
          <span class="compact-pool-amount">{{ formatUSD(pool.used_usd || 0, { compact: true }) }} / {{ displayAmount(pool.amount_usd) }}</span>
          <el-progress
            style="width:80px; flex-shrink:0"
            :percentage="Math.min(Math.round((pool.used_ratio || 0) * 100), 100)"
            :stroke-width="4"
            :color="progressColor(pool.used_ratio || 0)"
            :show-text="false"
          />
          <el-tag :type="statusType(pool.used_ratio || 0)" size="small">{{ statusText(pool.used_ratio || 0) }}</el-tag>
        </div>
      </template>
    </div>

    <div v-if="!compact" class="budget-toolbar">
      <div>
        <h3>{{ tableTitle }}</h3>
        <p>{{ tableDesc }}</p>
      </div>
      <!-- 工作室视图刷新按钮已整合到总额度卡片中 -->
      <el-button v-if="scopeType !== 'studio'" :loading="loading" size="small" @click="load">刷新</el-button>
    </div>

    <!-- 完整池卡片：非 compact -->
    <div v-if="scopeType === 'dept' && !compact" class="pool-grid">
      <div v-for="pool in deptPools" :key="pool.scope_type" class="pool-card">
        <div class="pool-head">
          <div>
            <div class="pool-title">{{ SCOPE_TYPE_LABELS[pool.scope_type] }}</div>
            <div class="pool-sub">{{ pool.scope_type === 'dept_service' ? 'Service Key 走程序服务预算池' : '个人 Key 默认走成员日常预算池' }}</div>
          </div>
          <el-tag :type="statusType(pool.used_ratio || 0)" size="small">{{ statusText(pool.used_ratio || 0) }}</el-tag>
        </div>
        <div class="pool-amount">
          <strong>{{ formatUSD(pool.used_usd || 0, { compact: true }) }}</strong>
          <span>/ {{ displayAmount(pool.amount_usd) }}</span>
        </div>
        <el-progress
          :percentage="Math.min(Math.round((pool.used_ratio || 0) * 100), 100)"
          :stroke-width="7"
          :color="progressColor(pool.used_ratio || 0)"
        />
        <div class="pool-limits">
          <span>周 {{ displayAmount(pool.weekly_amount_usd) }}</span>
          <span>日 {{ displayAmount(pool.daily_amount_usd) }}</span>
        </div>
        <el-button v-if="canEdit" link type="primary" size="small" class="pool-edit" @click="openDrawer(pool)">调整额度</el-button>
        <el-button v-else link type="primary" size="small" class="pool-edit" @click="goExpansion(pool)">申请扩容</el-button>
      </div>
    </div>

    <template v-if="scopeType === 'dept'">
      <div class="member-quota-header">
        <div>
          <span class="member-quota-title">{{ compact ? '成员配额' : '成员单独额度' }}</span>
          <span class="member-quota-count">{{ allocationRows.length }} 人</span>
        </div>
        <el-button v-if="canEdit" size="small" type="primary" :plain="!compact" :link="compact" @click="openMemberDrawer">
          {{ compact ? '+ 新增' : '+ 设置额度' }}
        </el-button>
      </div>
      <el-table
        v-loading="loading"
        :data="allocationRows"
        stripe
        size="small"
        class="budget-table"
        empty-text="暂无成员"
        :row-class-name="({ row }: any) => row.isDefaultPool ? 'row-pool-default' : ''"
      >
      <el-table-column label="成员" min-width="180">
        <template #default="{ row }">
          <div class="scope-cell">
            <span class="scope-dot" :style="{ background: row.isDefaultPool ? '#c0c4cc' : SCOPE_TYPE_COLORS[row.scope_type] }" />
            <div>
              <div class="scope-name">{{ row.label || row.scope_id }}</div>
              <div class="scope-id">
                <template v-if="row.isDefaultPool">
                  <el-tag size="small" type="info" effect="plain" style="font-size:10px;padding:0 4px;height:16px;line-height:16px;">共享预算池</el-tag>
                </template>
                <template v-else>成员配额</template>
              </div>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="月额度" width="100" align="right">
        <template #default="{ row }">
          <span v-if="row.isDefaultPool" class="muted">—</span>
          <template v-else>{{ displayAmount(row.amount_usd) }}</template>
        </template>
      </el-table-column>
      <el-table-column v-if="!compact" label="周额度" width="110" align="right">
        <template #default="{ row }">
          <span v-if="row.isDefaultPool" class="muted">—</span>
          <template v-else>{{ displayAmount(row.weekly_amount_usd) }}</template>
        </template>
      </el-table-column>
      <el-table-column v-if="!compact" label="日额度" width="110" align="right">
        <template #default="{ row }">
          <span v-if="row.isDefaultPool" class="muted">—</span>
          <template v-else>{{ displayAmount(row.daily_amount_usd) }}</template>
        </template>
      </el-table-column>
      <el-table-column label="已用 / 使用率" :min-width="compact ? 140 : 180">
        <template #default="{ row }">
          <div v-if="row.isDefaultPool" class="muted" style="font-size:12px;">共享预算池</div>
          <div v-else class="usage-cell">
            <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
            <el-progress
              :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)"
              :stroke-width="6"
              :color="progressColor(row.used_ratio || 0)"
            />
          </div>
        </template>
      </el-table-column>
      <el-table-column v-if="!compact" label="状态" width="80" align="center">
        <template #default="{ row }">
          <el-tag v-if="!row.isDefaultPool" :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" :width="compact ? 120 : 140" fixed="right" align="center">
        <template #default="{ row }">
          <el-button v-if="!row.isDefaultPool" link type="info" size="small" @click="navigateToMember(row)">查看</el-button>
          <template v-if="canEdit">
            <el-button v-if="row.isDefaultPool" link type="primary" size="small" @click="openDrawerForMember(row)">设置额度</el-button>
            <template v-else-if="compact">
              <el-button link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
              <el-dropdown size="small" trigger="click" @command="(cmd: string) => cmd === 'reset' && resetToPool(row)">
                <el-button link size="small" style="padding:0 2px">⋯</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="reset" class="dropdown-danger">还原为共享池</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
            <template v-else>
              <el-button link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
              <el-button link type="danger" size="small" @click="resetToPool(row)">还原</el-button>
            </template>
          </template>
          <template v-else>
            <el-button v-if="!row.isDefaultPool" link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
            <span v-else class="muted">—</span>
          </template>
        </template>
      </el-table-column>
    </el-table>
    </template>

    <!-- 工作室视图：Tab 切换（日常预算 / 服务预算） -->
    <template v-else>
      <!-- 工作室总额度卡片（含已用/限额/月周日限/调整入口） -->
      <div class="studio-alloc-card">
        <div class="studio-alloc-main">
          <div class="studio-alloc-amounts">
            <span class="studio-alloc-used">{{ formatUSD(totalUsed, { compact: true }) }}</span>
            <span class="studio-alloc-sep">/</span>
            <span class="studio-alloc-total">{{ displayAmount(studioAlloc?.amount_usd ?? -1) }}</span>
            <el-tag :type="statusType(totalRatio)" size="small" style="margin-left:8px">{{ statusText(totalRatio) }}</el-tag>
          </div>
          <el-progress
            :percentage="Math.min(Math.round(totalRatio * 100), 100)"
            :stroke-width="6"
            :color="progressColor(totalRatio)"
            style="margin-top:6px"
          />
          <div class="studio-alloc-limits">
            <span>周限 {{ displayAmount(studioAlloc?.weekly_amount_usd ?? -1) }}</span>
            <span>日限 {{ displayAmount(studioAlloc?.daily_amount_usd ?? -1) }}</span>
          </div>
        </div>
        <div class="studio-alloc-actions">
          <el-button v-if="effectiveCanEditSelf" size="small" type="primary" plain @click="openStudioAllocDrawer">调整额度</el-button>
          <el-button v-else size="small" plain @click="goExpansion(studioAllocPlaceholder)">申请扩容</el-button>
          <el-button :loading="loading" size="small" @click="load">刷新</el-button>
        </div>
      </div>
      <div class="dual-table-section" v-loading="loading">
        <el-tabs v-model="poolTab" class="pool-tabs">
          <el-tab-pane name="daily">
            <template #label>
              成员日常预算
              <el-badge v-if="dailyRows.length" :value="dailyRows.length" type="info" class="tab-badge" />
            </template>
            <p class="pool-desc">个人 Key 默认走此预算池，各部门共享。</p>
            <el-table :data="dailyRows" stripe size="small" empty-text="暂无配置">
              <el-table-column label="部门" min-width="160">
                <template #default="{ row }">
                  <span class="dept-name-text">{{ row.label || row.scope_id }}</span>
                </template>
              </el-table-column>
              <el-table-column label="月额度" width="120" align="right">
                <template #default="{ row }">{{ displayAmount(row.amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="周额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.weekly_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="日额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.daily_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="使用率" min-width="150">
                <template #default="{ row }">
                  <div class="usage-cell">
                    <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
                    <el-progress :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)" :stroke-width="6" :color="progressColor(row.used_ratio || 0)" />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="80" align="center">
                <template #default="{ row }">
                  <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="130" align="center">
                <template #default="{ row }">
                  <el-button v-if="canEdit" link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
                  <el-button v-else link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
                  <el-button link type="info" size="small" @click="navigateToDept(row)">成员预算</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane name="service">
            <template #label>
              程序服务预算
              <el-badge v-if="serviceRows.length" :value="serviceRows.length" type="info" class="tab-badge" />
            </template>
            <p class="pool-desc">Service Key 走此预算池，与成员日常互不干扰。</p>
            <el-table :data="serviceRows" stripe size="small" empty-text="暂无配置">
              <el-table-column label="部门" min-width="160">
                <template #default="{ row }">
                  <span class="dept-name-text">{{ row.label || row.scope_id }}</span>
                </template>
              </el-table-column>
              <el-table-column label="月额度" width="120" align="right">
                <template #default="{ row }">{{ displayAmount(row.amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="周额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.weekly_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="日额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.daily_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="使用率" min-width="150">
                <template #default="{ row }">
                  <div class="usage-cell">
                    <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
                    <el-progress :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)" :stroke-width="6" :color="progressColor(row.used_ratio || 0)" />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="80" align="center">
                <template #default="{ row }">
                  <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="80" align="center">
                <template #default="{ row }">
                  <el-button v-if="canEdit" link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
                  <el-button v-else link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </template>

    <el-drawer v-model="drawerVisible" title="调整预算额度" size="380px">
      <div v-if="editingItem" class="budget-drawer">
        <div class="drawer-scope">
          <div class="scope-name">{{ editingItem.label || editingItem.scope_id }}</div>
          <div class="scope-id">{{ SCOPE_TYPE_LABELS[editingItem.scope_type] }} · {{ editingItem.scope_id }}</div>
        </div>
        <el-form label-position="top">
          <el-form-item label="月额度（USD）">
            <el-input-number v-model="editForm.amount" :min="0" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
          <el-form-item label="周额度（USD，-1 表示不限）">
            <el-input-number v-model="editForm.weekly" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
          <el-form-item label="日额度（USD，-1 表示不限）">
            <el-input-number v-model="editForm.daily" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
          <el-form-item label="生效方式">
            <el-radio-group v-model="editEffectiveMode">
              <el-radio value="immediate">立即生效</el-radio>
              <el-radio value="next_period">下周期生效</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="调整原因" required>
            <el-input v-model="editReason" type="textarea" :rows="2" maxlength="200" />
          </el-form-item>
        </el-form>
        <div class="drawer-hint">保存后预算配置立即生效；已有告警去重会随改额重置。</div>
      </div>
      <template #footer>
        <el-button @click="drawerVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-drawer>

    <!-- 添加成员配额弹窗 -->
    <el-dialog v-model="memberDialogVisible" title="设置成员单独额度" width="460px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="选择成员">
          <el-select
            v-model="memberForm.userId"
            filterable
            remote
            :remote-method="searchMembers"
            :loading="memberSearchLoading"
            placeholder="输入姓名搜索"
            style="width: 100%"
            value-key="id"
          >
            <el-option
              v-for="u in memberSearchResults"
              :key="u.id"
              :label="`${u.name}（${u.email || u.id}）`"
              :value="u.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="月限额">
          <el-input-number v-model="memberForm.amount" :min="0" :precision="2" controls-position="right" style="width:100%" />
        </el-form-item>
        <el-form-item label="周限额">
          <el-input-number v-model="memberForm.weekly" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          <div style="font-size:11px;color:var(--el-text-color-placeholder);margin-top:2px">-1 表示不限</div>
        </el-form-item>
        <el-form-item label="日限额">
          <el-input-number v-model="memberForm.daily" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          <div style="font-size:11px;color:var(--el-text-color-placeholder);margin-top:2px">-1 表示不限</div>
        </el-form-item>
        <el-form-item label="调整原因" required>
          <el-input v-model="memberReason" type="textarea" :rows="2" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="memberDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="memberSaving" @click="saveMemberQuota">确认设置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adjustBudget, deleteMemberBudget, getBudgetList, previewBudgetAdjustment } from '@/services/control-plane'
import type { BudgetEffectiveMode, BudgetItem, BudgetView } from '@/types/budget'
import { SCOPE_TYPE_COLORS, SCOPE_TYPE_LABELS } from '@/types/budget'
import { formatUSD } from '@/utils/format'
import { useOrgStore } from '@/stores/org'
import type { DeptMember } from '@/stores/org'

const props = defineProps<{
  scopeType: 'studio' | 'dept'
  scopeId: string
  title: string
  childScopeIds?: string[]
  canEdit?: boolean
  /** 是否可直接修改当前节点自身的额度（工作室总额度）。
   *  不传时与 canEdit 保持一致。
   *  用于区分"admin 可直接改工作室上限"与"studio_admin 只能申请扩容"。 */
  canEditSelf?: boolean
  /** 紧凑模式：用于嵌套抽屉场景，隐藏顶部 summary，池卡片改为单列行内样式 */
  compact?: boolean
}>()
const emit = defineEmits<{
  navigate: [payload: { scopeType: 'dept' | 'member'; scopeId: string; label: string }]
}>()

const router = useRouter()
const loading = ref(false)
const orgStore = useOrgStore()
const items = ref<BudgetItem[]>([])
// 部门全量成员列表（dept 模式使用）
const deptMembers = ref<DeptMember[]>([])
const deptMembersTotal = ref(0)
const drawerVisible = ref(false)
const editingItem = ref<BudgetItem | null>(null)
const saving = ref(false)
const editForm = ref({ amount: 0, weekly: -1, daily: -1 })
const editReason = ref('')
const editEffectiveMode = ref<BudgetEffectiveMode>('immediate')
const poolTab = ref('daily')

// 成员配额
const memberDialogVisible = ref(false)
const memberSaving = ref(false)
const memberSearchLoading = ref(false)
const memberSearchResults = ref<Array<{ id: string; name: string; email?: string }>>([])
const memberForm = ref({ userId: '', amount: 100, weekly: -1, daily: -1 })
const memberReason = ref('')

function openMemberDrawer() {
  memberForm.value = { userId: '', amount: 100, weekly: -1, daily: -1 }
  memberReason.value = ''
  memberSearchResults.value = []
  memberDialogVisible.value = true
}

async function searchMembers(keyword: string) {
  if (!keyword) return
  memberSearchLoading.value = true
  try {
    const results = await orgStore.searchUsers(keyword)
    memberSearchResults.value = results.map(u => ({ id: u.id, name: u.name || u.id, email: u.email || '' }))
  } catch { memberSearchResults.value = [] }
  finally { memberSearchLoading.value = false }
}

async function saveMemberQuota() {
  if (!memberForm.value.userId) { ElMessage.warning('请选择成员'); return }
  if (!memberReason.value.trim()) { ElMessage.warning('请填写调整原因'); return }
  memberSaving.value = true
  try {
    await previewAndApplyScope(
      'member', memberForm.value.userId, memberForm.value,
      memberReason.value.trim(), 'immediate',
    )
    ElMessage.success('成员配额已设置')
    memberDialogVisible.value = false
    await load()
  } catch (e: any) { ElMessage.error(e?.message || '设置失败') }
  finally { memberSaving.value = false }
}

const view = computed<BudgetView>(() => props.scopeType === 'studio' ? 'studio' : 'department')

const currentItems = computed(() => {
  if (props.scopeType === 'studio') {
    return items.value.filter(i => i.scope_type === 'studio' && i.scope_id === props.scopeId)
  }
  return items.value.filter(i =>
    (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId
  )
})

const displayItems = computed(() => {
  if (props.scopeType === 'studio') {
    const ids = new Set(props.childScopeIds || [])
    return items.value.filter(i =>
      (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && ids.has(i.scope_id)
    )
  }
  return items.value.filter(i =>
    ((i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId) ||
    i.scope_type === 'member'
  )
})

const deptPools = computed(() => {
  if (props.scopeType !== 'dept') return []
  return items.value.filter(i =>
    (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId
  )
})

// 部门成员全量列表：以 deptMembers 为基础，已有配额的合并 budget 数据，未配置的显示"共享预算池"
interface MemberRow extends BudgetItem {
  isDefaultPool?: boolean  // true = 未单独配额，使用部门预算池
}

const allocationRows = computed<MemberRow[]>(() => {
  if (props.scopeType !== 'dept') return displayItems.value as MemberRow[]

  const budgetMap = new Map<string, BudgetItem>()
  for (const item of items.value) {
    if (item.scope_type === 'member') budgetMap.set(item.scope_id, item)
  }

  if (deptMembers.value.length === 0) {
    // 后备：没加载到成员列表时只显示已有配额的成员
    return [...budgetMap.values()] as MemberRow[]
  }

  return deptMembers.value.map<MemberRow>(m => {
    const existing = budgetMap.get(m.id)
    if (existing) {
      return { ...existing, isDefaultPool: false }
    }
    return {
      scope_type: 'member',
      scope_id: m.id,
      label: m.name,
      amount_usd: 0,
      weekly_amount_usd: -1,
      daily_amount_usd: -1,
      used_usd: 0,
      used_ratio: 0,
      isDefaultPool: true,
    } as MemberRow
  })
})

// 工作室视图分表：日常预算 / 服务预算
const dailyRows = computed(() => displayItems.value.filter(i => i.scope_type === 'dept_daily'))
const serviceRows = computed(() => displayItems.value.filter(i => i.scope_type === 'dept_service'))

// 工作室自身 allocation（用于总额度卡片）
const studioAlloc = computed(() => currentItems.value[0] ?? null)
// 是否可直接修改当前节点（工作室）自身额度；未传 canEditSelf 时回退到 canEdit
const effectiveCanEditSelf = computed(() => props.canEditSelf ?? props.canEdit ?? false)
const studioAllocPlaceholder = computed<BudgetItem>(() => studioAlloc.value ?? {
  scope_type: 'studio',
  scope_id: props.scopeId,
  label: props.title,
  amount_usd: -1,
  weekly_amount_usd: -1,
  daily_amount_usd: -1,
  used_usd: 0,
  used_ratio: 0,
  period: '',
  enabled: true,
})

function openStudioAllocDrawer() {
  const item = studioAllocPlaceholder.value
  editingItem.value = item
  editForm.value = {
    amount: item.amount_usd < 0 ? 0 : item.amount_usd,
    weekly: item.weekly_amount_usd,
    daily: item.daily_amount_usd,
  }
  editReason.value = ''
  editEffectiveMode.value = 'immediate'
  drawerVisible.value = true
}

const totalAmount = computed(() => {
  const base = currentItems.value.length ? currentItems.value : displayItems.value
  return base.reduce((sum, item) => sum + Math.max(item.amount_usd || 0, 0), 0)
})
const totalUsed = computed(() => {
  const base = currentItems.value.length ? currentItems.value : displayItems.value
  return base.reduce((sum, item) => sum + (item.used_usd || 0), 0)
})
const totalRatio = computed(() => totalAmount.value > 0 ? totalUsed.value / totalAmount.value : 0)

const tableTitle = computed(() => props.scopeType === 'studio' ? '部门预算分配' : '成员单独额度')
const tableDesc = computed(() => props.scopeType === 'studio'
  ? '在当前工作室下给各部门配置日常预算和程序服务预算。'
  : '成员默认共享部门日常预算池；只有需要单独控制时才在这里设置成员额度。'
)

async function load() {
  loading.value = true
  try {
    const [budgetData] = await Promise.all([
      getBudgetList(undefined, props.scopeType === 'dept' ? props.scopeId : undefined, view.value),
      props.scopeType === 'dept' ? loadAllDeptMembers() : Promise.resolve(),
    ])
    items.value = budgetData
  } finally {
    loading.value = false
  }
}

async function loadAllDeptMembers() {
  const pageSize = 100
  let page = 1
  const all: DeptMember[] = []
  while (true) {
    const { members, total } = await orgStore.fetchDeptMembers(props.scopeId, page, pageSize)
    all.push(...members)
    if (all.length >= total || members.length === 0) break
    page++
  }
  deptMembers.value = all
  deptMembersTotal.value = all.length
}

function openDrawer(item: BudgetItem) {
  editingItem.value = item
  editForm.value = {
    amount: item.amount_usd,
    weekly: item.weekly_amount_usd,
    daily: item.daily_amount_usd,
  }
  editReason.value = ''
  editEffectiveMode.value = 'immediate'
  drawerVisible.value = true
}

function openDrawerForMember(row: MemberRow) {
  editingItem.value = { ...row, amount_usd: 0, weekly_amount_usd: -1, daily_amount_usd: -1 }
  editForm.value = { amount: 0, weekly: -1, daily: -1 }
  editReason.value = ''
  editEffectiveMode.value = 'immediate'
  drawerVisible.value = true
}

async function previewAndApplyScope(
  scopeType: BudgetItem['scope_type'],
  scopeId: string,
  values: { amount: number; weekly: number; daily: number },
  reason: string,
  effectiveMode: BudgetEffectiveMode,
) {
  const targets = [
    { period_type: 'monthly' as const, value: values.amount },
    { period_type: 'weekly' as const, value: values.weekly },
    { period_type: 'daily' as const, value: values.daily },
  ].filter(item => item.value >= 0)
  const previews = await Promise.all(targets.map(item => previewBudgetAdjustment({
    scope_type: scopeType,
    scope_id: scopeId,
    period_type: item.period_type,
    target_limit_usd: item.value.toFixed(6),
    effective_mode: effectiveMode,
    reason,
  })))
  if (previews.some(item => item.stale)) {
    ElMessage.warning('用量数据已过期，请刷新后重新预览')
    return
  }
  if (previews.some(item => item.zero_block)) {
    await ElMessageBox.confirm('$0 将阻断后续请求，是否继续？', '确认 $0 风险', { type: 'warning' })
  }
  if (previews.some(item => item.below_used)) {
    await ElMessageBox.confirm('新额度低于当前已用量，是否继续？', '确认低于已用风险', { type: 'warning' })
  }
  if (previews.some(item => item.replaces_pending_schedule)) {
    await ElMessageBox.confirm('本次调整将替换已有排期，是否继续？', '确认替换排期', { type: 'warning' })
  }
  if (!previews.some(item => item.zero_block || item.below_used || item.replaces_pending_schedule)) {
    await ElMessageBox.confirm('预览完成，确认应用预算调整？', '确认预算调整', { type: 'info' })
  }
  await Promise.all(targets.map((item, index) => adjustBudget({
    scope_type: scopeType,
    scope_id: scopeId,
    period_type: item.period_type,
    target_limit_usd: item.value.toFixed(6),
    effective_mode: effectiveMode,
    reason,
    confirm_zero_block: previews[index].zero_block,
    confirm_below_used: previews[index].below_used,
    confirm_replace_schedule: previews[index].replaces_pending_schedule,
    preview_version: previews[index].preview_version,
  })))
}

async function save() {
  if (!editingItem.value) return
  if (!editReason.value.trim()) { ElMessage.warning('请填写调整原因'); return }
  saving.value = true
  try {
    await previewAndApplyScope(
      editingItem.value.scope_type, editingItem.value.scope_id, editForm.value,
      editReason.value.trim(), editEffectiveMode.value,
    )
    ElMessage.success('预算已更新')
    drawerVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function resetToPool(row: MemberRow) {
  try {
    await ElMessageBox.confirm(
      `确认移除「${row.label || row.scope_id}」的单独配额？移除后该成员将恢复共享部门预算池。`,
      '还原到部门预算池',
      { confirmButtonText: '确认移除', cancelButtonText: '取消', type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await deleteMemberBudget(row.scope_type, row.scope_id)
    ElMessage.success('已还原为共享部门预算池')
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '操作失败')
  }
}

function goExpansion(item: BudgetItem) {
  router.push({
    path: '/admin/expansion-requests',
    query: {
      scope_type: item.scope_type,
      scope_id: item.scope_id,
      requested_amount: Math.max(Math.ceil((item.amount_usd || 0) * 1.2), (item.amount_usd || 0) + 100),
    },
  })
}

function navigateToDept(row: BudgetItem) {
  emit('navigate', { scopeType: 'dept', scopeId: row.scope_id, label: row.label || row.scope_id })
}

function navigateToMember(row: BudgetItem) {
  emit('navigate', { scopeType: 'member', scopeId: row.scope_id, label: row.label || row.scope_id })
}

function displayAmount(value: number) {
  if (value == null || value < 0) return '不限'
  return formatUSD(value, { compact: true })
}

function progressColor(ratio: number) {
  if (ratio >= 1) return '#f56c6c'
  if (ratio >= 0.8) return '#e6a23c'
  return '#36B34E'
}

function statusType(ratio: number) {
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
}

function statusText(ratio: number) {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

onMounted(load)
watch(() => [props.scopeType, props.scopeId, props.childScopeIds?.join(',')], load)
</script>

<style scoped>
.budget-scope-panel { display: flex; flex-direction: column; gap: 16px; }
.budget-summary {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  padding: 18px 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
}
.summary-main { min-width: 0; }
.summary-eyebrow { font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 4px; }
.summary-title { font-size: 18px; font-weight: 700; color: var(--el-text-color-primary); }
.summary-meta { margin-top: 4px; font-size: 12px; color: var(--el-text-color-secondary); }
.summary-numbers { width: 260px; flex-shrink: 0; }
.summary-amount { display: flex; align-items: baseline; gap: 4px; margin-bottom: 8px; justify-content: flex-end; }
.summary-amount strong { font-size: 22px; color: var(--el-text-color-primary); }
.summary-amount span { color: var(--el-text-color-secondary); font-size: 13px; }
.budget-toolbar { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; }
.budget-toolbar h3 { margin: 0; font-size: 15px; color: var(--el-text-color-primary); }
.budget-toolbar p { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 12px; }
.budget-table { border: 1px solid var(--el-border-color-lighter); border-radius: 10px; overflow: hidden; }
.member-quota-header { display: flex; align-items: center; justify-content: space-between; margin: 16px 0 8px; }
.member-quota-header > div { display: flex; align-items: center; gap: 8px; }
.member-quota-title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.member-quota-count { font-size: 12px; color: var(--el-text-color-secondary); background: var(--el-fill-color); padding: 1px 8px; border-radius: 10px; }
:deep(.row-pool-default td) { color: var(--el-text-color-secondary) !important; }
.pool-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.pool-card {
  position: relative;
  padding: 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-blank);
}
.pool-head { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
.pool-title { font-weight: 700; color: var(--el-text-color-primary); }
.pool-sub { margin-top: 3px; font-size: 12px; color: var(--el-text-color-secondary); }
.pool-amount { display: flex; align-items: baseline; gap: 5px; margin: 14px 0 8px; }
.pool-amount strong { font-size: 22px; color: var(--el-text-color-primary); }
.pool-amount span { color: var(--el-text-color-secondary); font-size: 13px; }
.pool-limits { display: flex; gap: 12px; margin-top: 8px; color: var(--el-text-color-secondary); font-size: 12px; }
.pool-edit { position: absolute; right: 12px; bottom: 10px; }
.scope-cell { display: flex; align-items: center; gap: 10px; min-width: 0; }
.scope-dot { width: 8px; height: 8px; border-radius: 99px; flex-shrink: 0; }
.scope-name { font-weight: 600; color: var(--el-text-color-primary); }
.scope-id { margin-top: 2px; font-size: 11px; color: var(--el-text-color-secondary); }
.usage-cell { display: grid; grid-template-columns: 70px 1fr; align-items: center; gap: 10px; }
.muted { color: var(--el-text-color-placeholder); }
.budget-drawer { display: flex; flex-direction: column; gap: 18px; }
.drawer-scope { padding-bottom: 12px; border-bottom: 1px solid var(--el-border-color-lighter); }
.drawer-hint { font-size: 12px; line-height: 1.6; color: var(--el-text-color-secondary); background: var(--el-fill-color-light); padding: 10px 12px; border-radius: 8px; }

/* 按部门分组卡片 */
.dept-group-list { display: flex; flex-direction: column; gap: 16px; }
.empty-hint { text-align: center; padding: 32px; color: var(--el-text-color-placeholder); font-size: 13px; }

/* 工作室总额度横向卡片 */
.studio-alloc-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-blank);
  margin-bottom: 16px;
}
.studio-alloc-main { flex: 1; min-width: 0; }
.studio-alloc-amounts { display: flex; align-items: baseline; gap: 5px; }
.studio-alloc-used { font-size: 22px; font-weight: 700; color: var(--el-text-color-primary); }
.studio-alloc-sep { font-size: 14px; color: var(--el-text-color-secondary); }
.studio-alloc-total { font-size: 14px; color: var(--el-text-color-secondary); }
.studio-alloc-limits { display: flex; gap: 14px; margin-top: 6px; font-size: 12px; color: var(--el-text-color-secondary); }
.studio-alloc-actions { display: flex; flex-direction: column; gap: 6px; flex-shrink: 0; }

/* 双表格布局 */
.dual-table-section { }
.pool-tabs { }
.pool-desc { margin: 0 0 12px; font-size: 12px; color: var(--el-text-color-secondary); }
.tab-badge { margin-left: 6px; }
.dept-name-text { font-weight: 500; color: var(--el-text-color-primary); }
@media (max-width: 900px) {
  .budget-summary { flex-direction: column; }
  .summary-numbers { width: 100%; }
  .pool-grid { grid-template-columns: 1fr; }
}

/* ── compact 模式 ─────────────────────────────────────────────────────────── */
.is-compact {
  gap: 12px;
}
/* compact 模式：仅日常池状态一行 */
.compact-pool-status {
  padding: 8px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-light);
}
.compact-pool-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.compact-pool-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.compact-pool-amount {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}

:deep(.dropdown-danger) {
  color: var(--el-color-danger) !important;
}

/* 嵌套部门成员抽屉 header */
.dept-drawer-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
}
.dept-drawer-back {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding: 0;
  height: auto;
}
.dept-drawer-back:hover {
  color: var(--el-color-primary);
}
.dept-drawer-title-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.dept-drawer-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.dept-drawer-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
