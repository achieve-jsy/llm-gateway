<template>
  <div class="org-layout">

    <!-- ═══ 左侧：组织树 + 下属团队卡 ═════════════════════════════════════════ -->
    <aside class="tree-panel">
      <!-- EHR 同步状态 / 手动触发：只有全局管理员能点（后端同样按 scope=all 校验） -->
      <SyncStatusCard
        v-if="isGlobalAdmin"
        :status="orgStore.syncStatus"
        :syncing="syncInFlight"
        @trigger="onManualSync"
      />

      <div class="tree-toolbar">
        <span class="tree-title">组织架构</span>
      </div>

      <div class="tree-search">
        <el-input
          v-model="treeSearch"
          placeholder="搜索工作室 / 团队"
          clearable
          size="small"
          :prefix-icon="Search"
          @input="onTreeSearch"
        />
      </div>

      <el-scrollbar class="tree-body">
        <el-tree
          ref="treeRef"
          :data="treeNodes"
          node-key="key"
          :props="{ label: 'label', children: 'children' }"
          highlight-current
          :expand-on-click-node="false"
          :default-expanded-keys="defaultExpandedKeys"
          :filter-node-method="filterNode"
          @node-click="onNodeClick"
        >
          <template #default="{ data }">
            <span class="tree-node-content">
              <el-icon v-if="data.type === 'studio'" class="node-icon studio-icon"><OfficeBuilding /></el-icon>
              <el-icon v-else class="node-icon dept-icon"><Folder /></el-icon>
              <span class="node-label">{{ data.label }}</span>
              <el-tag
                v-if="data.type === 'dept' && data.deptData?.status === 'pending_studio'"
                type="warning" size="small" class="node-badge"
              >待归属</el-tag>
              <span v-if="leadersOf(data).length" class="node-leaders">
                负责人：{{ leadersOf(data).map(l => l.name).join('、') }}
              </span>
              <el-button
                link type="primary" size="small" class="node-add-leader"
                title="加负责人（花名册工号/姓名）"
                @click.stop="onTreeAddLeader(data)"
              >+</el-button>
            </span>
          </template>
        </el-tree>

        <!-- 选中工作室/团队下的「下属团队」卡片（原型图3 左栏） -->
        <TeamCardList
          v-if="workbenchStudio"
          :teams="visibleTeams"
          :active-dept-id="selectedDept?.dept_id"
          :can-manage="canManageStudio"
          @select="onTeamCardSelect"
          @set-admin="onTeamCardSetAdmin"
          @add-leader="onTeamCardAddLeader"
        />

        <!-- 待归属伪节点 -->
        <div
          v-if="unmappedDepts.length > 0"
          :class="['unmapped-node', { 'is-current': unmappedSelected }]"
          @click="selectUnmapped"
        >
          <el-icon class="node-icon" color="var(--el-color-warning)"><Warning /></el-icon>
          <span class="node-label">待归属部门</span>
          <el-badge :value="unmappedDepts.length" type="warning" class="node-count" />
        </div>
      </el-scrollbar>
    </aside>

    <!-- ═══ 右侧：工作室工作台 ═══════════════════════════════════════════════ -->
    <main class="content-panel">

      <!-- ─── 工作台：工作室头部 + 团队/成员 tabs ────────────────────────── -->
      <template v-if="workbenchStudio">
        <TeamHeader
          :studio-name="workbenchStudio.name"
          :member-count="studioMemberCount"
          :admins="studioAdmins"
          :leaders="studioLeaders"
          :can-manage="canManageStudio"
          :team="headerTeam"
          :can-manage-team="canManageStudio"
          @set-studio-admin="openGrantDialog('studio_admin')"
          @revoke-studio-admin="onRevokeStudioAdmin"
          @add-studio-leader="onAddStudioLeader"
          @set-team-admin="openGrantDialog('dept_admin')"
          @revoke-team-admin="onRevokeTeamAdmin"
          @add-team-leader="onAddTeamLeader"
        />

        <el-tabs v-model="activeTab" class="panel-tabs">
          <!-- Tab：团队/成员 -->
          <el-tab-pane name="members">
            <template #label>
              团队/成员
              <span v-if="deptMembersTotal > 0" class="tab-count">{{ deptMembersTotal }}</span>
            </template>
            <div class="tab-content">
              <div v-if="!selectedDept" class="tab-empty">从左侧选择一个团队查看成员</div>
              <template v-else>
                <div class="members-toolbar">
                  <el-input
                    v-model="memberSearch"
                    placeholder="搜索成员姓名或邮箱..."
                    clearable
                    size="small"
                    style="width: 260px"
                    :prefix-icon="Search"
                  />
                  <span class="members-hint">{{ selectedDept.dept_name }}</span>
                </div>
                <div class="org-table-wrap">
                  <MemberTable
                    :members="memberRows"
                    :quota-map="userQuotaMap"
                    :loading="deptMembersLoading"
                    :can-manage="canManageStudio"
                    @edit-quota="openUserQuotaDialog"
                    @view-usage="openMemberDetail"
                    @manage-budget="goBudget"
                    @manage-entitlement="goEntitlement"
                  />
                </div>
                <div v-if="!deptMembersLoading && filteredMembers.length === 0" class="tab-empty">
                  {{ memberSearch ? '未找到匹配成员' : '该团队暂无成员（员工首次登录后自动归集）' }}
                </div>
                <el-pagination
                  v-if="deptMembersTotal > deptMembersPageSize"
                  v-model:current-page="deptMembersPage"
                  :page-size="deptMembersPageSize"
                  :total="deptMembersTotal"
                  layout="prev, pager, next"
                  small
                  class="members-pagination"
                  @current-change="loadDeptMembers"
                />
              </template>
            </div>
          </el-tab-pane>

          <!-- Tab：Service Key / 模型授权 / 团队用量 / 团队预算 / 申请审批
               这些能力已有独立页面，这里只做导航，不复制业务逻辑 -->
          <el-tab-pane v-for="t in NAV_TABS" :key="t.name" :name="t.name" :label="t.label">
            <div class="tab-content">
              <div class="nav-placeholder">
                <el-icon size="30" color="var(--el-text-color-placeholder)"><Link /></el-icon>
                <p class="nav-placeholder__title">{{ t.label }}</p>
                <p class="nav-placeholder__sub">{{ t.hint }}</p>
                <el-button type="primary" plain size="small" @click="gotoNavTab(t)">
                  打开{{ t.label }}页面
                </el-button>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>

      <!-- ─── 待归属视图 ─────────────────────────────────────────────────── -->
      <template v-else-if="unmappedSelected">
        <div class="panel-header">
          <div class="panel-info">
            <div class="panel-breadcrumb">公司 <el-icon class="crumb-arrow"><ArrowRight /></el-icon> 待归属部门</div>
            <div class="panel-title">
              <el-icon class="title-icon" color="var(--el-color-warning)"><Warning /></el-icon>
              待归属部门
              <el-tag type="warning" size="small" class="title-badge">{{ unmappedDepts.length }}</el-tag>
            </div>
          </div>
        </div>

        <div class="tab-content nav-body">
          <el-alert
            title="以下部门在 EHR 中无对应上级工作室，员工登录后无法使用模型。请联系 EHR 管理员补齐组织关系。"
            type="warning"
            :closable="false"
            show-icon
            style="margin-bottom: 16px"
          />
          <div class="org-table-wrap"><el-table :data="unmappedDepts" stripe size="small">
            <el-table-column label="部门名称" min-width="160" prop="dept_name" />
            <el-table-column label="部门ID" min-width="140">
              <template #default="{ row }">
                <code class="dept-id-code">{{ row.dept_id }}</code>
              </template>
            </el-table-column>
          </el-table></div>
        </div>
      </template>

      <!-- ─── 空态 ───────────────────────────────────────────────────────── -->
      <div v-else class="content-empty">
        <el-icon size="52" color="var(--el-text-color-placeholder)"><OfficeBuilding /></el-icon>
        <p class="empty-title">选择左侧节点查看详情</p>
        <p class="empty-sub">点击工作室查看团队与成员，点击「待归属部门」处理未归属团队</p>
      </div>

    </main>
  </div>

  <!-- ═══ 管理员设置弹窗 ═════════════════════════════════════════════════════ -->
  <el-dialog v-model="grantDialogVisible" title="设置管理员" width="460px" @close="resetGrant">
    <div v-if="!grantSelectedUser" class="search-step">
      <div class="step-label">第一步：搜索并选择员工</div>
      <el-alert
        v-if="grantScopeStudioId"
        type="info" :closable="false" show-icon
        style="margin-bottom: 10px; font-size: 12px;"
      >
        仅显示「{{ workbenchStudio?.name }}」工作室下的员工
      </el-alert>
      <el-input
        v-model="grantSearchKeyword"
        :placeholder="grantScopeStudioId ? '在工作室成员中搜索姓名或邮箱...' : '输入姓名或邮箱搜索...'"
        :suffix-icon="Search"
        clearable
        @input="onGrantSearch"
      />
      <div v-if="grantSearchLoading" class="search-hint">搜索中...</div>
      <div v-else-if="grantSearchResults.length > 0" class="search-results">
        <div
          v-for="u in grantSearchResults"
          :key="u.id"
          class="search-result-item"
          @click="grantSelectedUser = u"
        >
          <div class="result-avatar">{{ u.name.charAt(0) }}</div>
          <div class="result-info">
            <span class="result-name">{{ u.name }}</span>
            <span class="result-meta">{{ u.email }}<span v-if="u.dept_name"> · {{ u.dept_name }}</span></span>
          </div>
        </div>
      </div>
      <div v-else-if="grantSearchKeyword.length >= 1 && !grantSearchLoading" class="search-hint">未找到员工</div>
    </div>

    <div v-else class="grant-step">
      <div class="selected-user">
        <div class="selected-avatar">{{ grantSelectedUser.name.charAt(0) }}</div>
        <div class="selected-info">
          <span class="selected-name">{{ grantSelectedUser.name }}</span>
          <span class="selected-meta">{{ grantSelectedUser.email }}</span>
        </div>
        <el-button link size="small" @click="grantSelectedUser = null; grantSearchKeyword = ''">更换</el-button>
      </div>

      <el-divider style="margin: 12px 0" />

      <div class="step-label">第二步：确认角色与管辖范围</div>
      <div class="grant-scope-info">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="角色">
            <el-tag :type="grantPresetRole === 'studio_admin' ? 'warning' : 'info'" size="small">
              {{ grantPresetRole === 'studio_admin' ? '工作室管理员' : '团队管理员' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="管辖范围">{{ grantPresetScopeName }}</el-descriptions-item>
        </el-descriptions>
      </div>
    </div>

    <template #footer>
      <el-button @click="grantDialogVisible = false">取消</el-button>
      <el-button
        v-if="grantSelectedUser"
        type="primary"
        :loading="granting"
        @click="doGrant"
      >确认授权</el-button>
    </template>
  </el-dialog>

  <!-- ═══ 成员用量快览抽屉 ════════════════════════════════════════════════════ -->
  <el-drawer
    v-model="memberDrawerVisible"
    direction="rtl"
    size="640px"
    destroy-on-close
    :with-header="false"
    class="md-drawer"
  >
    <template v-if="memberDrawerTarget">

      <!-- ── 头部 Profile ── -->
      <div class="md-header">
        <div class="md-avatar">{{ memberDrawerTarget.name?.charAt(0)?.toUpperCase() || '?' }}</div>
        <div class="md-info">
          <div class="md-name">{{ memberDrawerTarget.name }}</div>
          <div class="md-email">{{ memberDrawerTarget.email }}</div>
          <div class="md-tags">
            <el-tag v-if="memberDrawerTarget.job_title" size="small" type="info" effect="plain">{{ memberDrawerTarget.job_title }}</el-tag>
            <el-tag v-if="memberDrawerTarget.employee_id" size="small" type="info" effect="plain">工号 {{ memberDrawerTarget.employee_id }}</el-tag>
            <el-tag v-if="memberDrawerTarget.job_family" size="small" type="" effect="plain">{{ memberDrawerTarget.job_family }}</el-tag>
          </div>
        </div>
        <el-button link class="md-close" @click="memberDrawerVisible=false">
          <el-icon size="18"><Close /></el-icon>
        </el-button>
      </div>

      <div class="md-divider" />

      <!-- ── 4 个 KPI 卡片 ── -->
      <div class="md-section-label">近 30 天数据概览</div>
      <div class="md-kpi-grid" v-loading="mdLoading">
        <div class="md-kpi-card md-kpi--green">
          <div class="md-kpi__icon"><el-icon size="18"><Money /></el-icon></div>
          <div class="md-kpi__body">
            <p class="md-kpi__label">成本估算</p>
            <p class="md-kpi__value">${{ mdKpi.cost.toFixed(3) }}</p>
            <p class="md-kpi__sub">USD</p>
          </div>
        </div>
        <div class="md-kpi-card md-kpi--blue">
          <div class="md-kpi__icon"><el-icon size="18"><DataLine /></el-icon></div>
          <div class="md-kpi__body">
            <p class="md-kpi__label">调用次数</p>
            <p class="md-kpi__value">{{ mdKpi.callCount.toLocaleString() }}</p>
            <p class="md-kpi__sub">均耗时 {{ mdKpi.avgDuration > 0 ? mdKpi.avgDuration.toFixed(0)+'ms' : '—' }}</p>
          </div>
        </div>
        <div class="md-kpi-card"
          :class="mdKpi.callCount === 0 ? 'md-kpi--gray' : mdKpi.successRate >= 95 ? 'md-kpi--green' : mdKpi.successRate >= 90 ? 'md-kpi--amber' : 'md-kpi--red'">
          <div class="md-kpi__icon"><el-icon size="18"><CircleCheck /></el-icon></div>
          <div class="md-kpi__body">
            <p class="md-kpi__label">成功率</p>
            <p class="md-kpi__value">{{ mdKpi.callCount === 0 ? '—' : mdKpi.successRate.toFixed(1) + '%' }}</p>
            <p class="md-kpi__sub">{{ mdKpi.errorCount > 0 ? mdKpi.errorCount + ' 次失败' : '无失败记录' }}</p>
          </div>
        </div>
        <div class="md-kpi-card md-kpi--gray">
          <div class="md-kpi__icon"><el-icon size="18"><List /></el-icon></div>
          <div class="md-kpi__body">
            <p class="md-kpi__label">Token 消耗</p>
            <p class="md-kpi__value">{{ mdKpi.totalTokens >= 1000000 ? (mdKpi.totalTokens/1000000).toFixed(2)+'M' : (mdKpi.totalTokens/1000).toFixed(1)+'K' }}</p>
            <p class="md-kpi__sub">过去 30 天</p>
          </div>
        </div>
      </div>

      <div class="md-divider" />

      <!-- ── 调用趋势迷你图 ── -->
      <div class="md-section-label">调用趋势（近 30 天）</div>
      <div class="md-trend-wrap" v-loading="mdTrendLoading">
        <canvas ref="mdTrendCanvasRef" class="md-trend-canvas" />
        <div v-if="!mdTrendLoading && mdTrendData.length === 0" class="md-empty" style="height:100px">暂无趋势数据</div>
      </div>

      <div class="md-divider" />

      <!-- ── Top 模型 ── -->
      <div class="md-section-label">常用模型（按调用量）</div>
      <div v-if="mdTrendLoading" v-loading="true" style="height:60px" />
      <div v-else-if="mdTopModels.length === 0" class="md-empty">暂无模型数据</div>
      <div v-else class="md-models-list">
        <div v-for="(m, i) in mdTopModels" :key="m.model" class="md-model-row">
          <span class="md-model-rank">{{ i + 1 }}</span>
          <div class="md-model-info">
            <div class="md-model-name-row">
              <span class="md-model-name">{{ m.model }}</span>
              <span class="md-model-calls">{{ m.calls.toLocaleString() }} 次</span>
            </div>
            <div class="md-model-bar-bg">
              <div class="md-model-bar-fill" :style="{ width: mdTopModels[0]?.calls ? Math.round(m.calls / mdTopModels[0].calls * 100) + '%' : '0%' }" />
            </div>
          </div>
          <span class="md-model-cost">${{ m.cost.toFixed(3) }}</span>
        </div>
      </div>

      <div class="md-divider" />

      <!-- ── 最近调用记录 ── -->
      <div class="md-section-label">最近调用记录</div>
      <div v-if="mdLogsLoading" v-loading="true" style="height:120px" />
      <div v-else-if="mdLogs.length === 0" class="md-empty">近 30 天暂无调用记录</div>
      <div v-else class="md-log-list">
        <div v-for="row in mdLogs" :key="row.request_id" class="md-log-row">
          <span class="md-log-status" :class="row.status === 200 ? 'md-log-status--ok' : 'md-log-status--err'" />
          <div class="md-log-main">
            <span class="md-log-model" :title="row.model">{{ row.model }}</span>
            <span class="md-log-time">{{ row.ts?.slice(0,16).replace('T',' ') }}</span>
          </div>
          <div class="md-log-right">
            <span class="md-log-tokens">{{ ((row.prompt_tokens||0)+(row.completion_tokens||0)).toLocaleString() }} tk</span>
            <span class="md-log-cost">${{ (row.est_cost_usd||0).toFixed(4) }}</span>
          </div>
        </div>
      </div>

      <!-- ── 底部操作 ── -->
      <div class="md-footer">
        <el-button type="primary" style="width:100%" @click="goToMemberDashboard">
          查看完整用量分析 →
        </el-button>
      </div>

    </template>
  </el-drawer>

  <!-- ═══ 用户配额编辑对话框 ════════════════════════════════════════════════════ -->
  <QuotaEditDialog
    v-model:visible="userQuotaDialogVisible"
    subject-type="user"
    :subject-id="userQuotaSubjectId"
    :subject-label="userQuotaSubjectLabel"
    :default-rpm="QUOTA_DEFAULTS.user.rpm"
    :default-concurrency="QUOTA_DEFAULTS.user.max_concurrency"
    @saved="onUserQuotaSaved"
  />
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { Chart, registerables } from 'chart.js'
Chart.register(...registerables)
import { ElMessage, ElMessageBox } from 'element-plus'
import type { ElTree } from 'element-plus'
import {
  OfficeBuilding, Folder, Warning, Search, ArrowRight, Link,
  Close, Money, DataLine, CircleCheck, List,
} from '@element-plus/icons-vue'
import { useOrgStore, type RoleWithUser, type UserSearchResult, type DeptMember } from '@/stores/org'
import { useAuthStore } from '@/stores/auth'
import type {
  Studio, OrgDepartment, OrgNodeLeader, AccountBrief, OrgLeaderBrief, TeamCard, StudioOverview,
} from '@/types/org'
import http from '@/services/http'
import { useRoute, useRouter } from 'vue-router'
import QuotaEditDialog from '@/components/QuotaEditDialog.vue'
import SyncStatusCard from '@/components/org/SyncStatusCard.vue'
import TeamHeader from '@/components/org/team/TeamHeader.vue'
import TeamCardList from '@/components/org/team/TeamCardList.vue'
import MemberTable from '@/components/org/team/MemberTable.vue'
import type { MemberRow, TeamHeaderTeam } from '@/components/org/team/memberFormat'
import { QUOTA_DEFAULTS, getUserQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

const orgStore  = useOrgStore()
const authStore = useAuthStore()
const route     = useRoute()
const router    = useRouter()

// ─── 数据 ─────────────────────────────────────────────────────────────────────
const studios    = ref<Studio[]>([])
const allDepts   = ref<OrgDepartment[]>([])
const unmappedDepts = ref<OrgDepartment[]>([])
const allRoles   = ref<RoleWithUser[]>([])

// 当前登录用户的 org 角色（用于视图过滤）
const myOrgRoles  = ref<RoleWithUser[]>([])
const myStudioIds = computed(() =>
  myOrgRoles.value.filter(r => r.role === 'studio_admin').map(r => r.scope_id)
)
const myDeptIds = computed(() =>
  myOrgRoles.value.filter(r => r.role === 'dept_admin').map(r => r.scope_id)
)
// 是否为全局 admin（JWT role=admin 或 org role=admin）
const isGlobalAdmin = computed(() =>
  authStore.isAdmin || myOrgRoles.value.some(r => r.role === 'admin')
)

// ─── EHR 同步（状态卡片 + 防重复触发）────────────────────────────────────────
// 三层防重：本地锁（连点只发一次）、后端 409（cron/他人正在跑）、running 状态（刷新后按钮仍灰）。
const localSyncing = ref(false)
const syncInFlight = computed(() => localSyncing.value || Boolean(orgStore.syncStatus?.running))

async function loadSyncStatus() {
  if (!isGlobalAdmin.value) return
  try {
    await orgStore.fetchSyncStatus()
  } catch {
    // 权限不足或后端未就绪：卡片安静显示"尚未同步"，不打扰用户
  }
}

async function onManualSync() {
  if (syncInFlight.value) return
  localSyncing.value = true
  try {
    await orgStore.triggerSync()
    ElMessage.success('EHR 数据同步完成')
  } catch (e: any) {
    const code = e?.response?.status
    if (code === 409) {
      ElMessage.info('已有同步在执行（可能是凌晨定时任务），请稍后再试')
    } else if (e?.code === 'ECONNABORTED' || e?.code === 'ETIMEDOUT') {
      ElMessage.warning('同步请求超过 60 秒未返回，最终结果以后端状态为准')
    } else {
      ElMessage.error(e?.response?.data?.message || e?.message || '同步失败')
    }
  } finally {
    localSyncing.value = false
    await loadSyncStatus()
    void loadLeaders()
    void loadOverview(true)
  }
}

// ─── 树 ───────────────────────────────────────────────────────────────────────
const treeRef    = ref<InstanceType<typeof ElTree>>()
const treeSearch = ref('')
const defaultExpandedKeys = ref<string[]>([])

interface TreeNode {
  key: string
  label: string
  type: 'studio' | 'dept'
  studioData?: Studio
  deptData?: OrgDepartment
  parentStudio?: Studio
  children?: TreeNode[]
}

const treeNodes = computed<TreeNode[]>(() => {
  // 可见工作室：admin 看全部；studio_admin 只看自己管辖的；dept_admin 按所属工作室归组
  let visibleStudios = studios.value
  if (!isGlobalAdmin.value) {
    if (myStudioIds.value.length > 0) {
      // studio_admin：只显示自己管辖的工作室
      visibleStudios = studios.value.filter(s => myStudioIds.value.includes(s.id))
    } else if (myDeptIds.value.length > 0) {
      // dept_admin：只显示包含自己管辖部门的团队（可能为空工作室占位）
      const studioIdsForMyDepts = new Set(
        allDepts.value.filter(d => myDeptIds.value.includes(d.dept_id)).map(d => d.studio_id).filter(Boolean)
      )
      visibleStudios = studios.value.filter(s => studioIdsForMyDepts.has(s.id))
    }
  }

  return visibleStudios.map(s => ({
    key: `studio:${s.id}`,
    label: s.name,
    type: 'studio' as const,
    studioData: s,
    children: allDepts.value
      .filter(d => {
        if (d.studio_id !== s.id) return false
        // dept_admin 只能看到自己管辖的团队
        if (!isGlobalAdmin.value && myStudioIds.value.length === 0 && myDeptIds.value.length > 0) {
          return myDeptIds.value.includes(d.dept_id)
        }
        return true
      })
      .map(d => ({
        key: `dept:${d.dept_id}`,
        label: d.dept_name,
        type: 'dept' as const,
        deptData: d,
        parentStudio: s,
        children: [],
      })),
  }))
})

// ─── 负责人（增量覆盖与保留：同步只追加，手动加的人跨同步保留）──────────────
const leadersByNode = computed(() => {
  const m = new Map<string, OrgNodeLeader[]>()
  for (const l of orgStore.leaders) {
    const k = l.node_type + ':' + l.node_id
    const arr = m.get(k) || []
    arr.push(l)
    m.set(k, arr)
  }
  return m
})

// EHR 负责人（只读、多值）：本地 leaders 兜底
function leadersOfNode(nodeType: 'studio' | 'dept', nodeId: string): OrgLeaderBrief[] {
  return (leadersByNode.value.get(nodeType + ':' + nodeId) || [])
    .map(l => ({ name: l.name, source: l.source }))
}

function leadersOf(node: TreeNode): OrgNodeLeader[] {
  const id = node.type === 'studio' ? node.studioData?.id : node.deptData?.id
  return id ? leadersByNode.value.get(node.type + ':' + id) || [] : []
}

async function loadLeaders() {
  try {
    await orgStore.fetchLeaders()
  } catch {
    // 负责人读失败不挡组织树
  }
}

// 唯一手动入口：按花名册关键字解析后追加负责人（EHR 同步不覆盖手动记录）
async function addLeaderPrompt(nodeType: 'studio' | 'dept', nodeId: string, label: string) {
  let keyword = ''
  try {
    const ret = await ElMessageBox.prompt(
      '按花名册解析：工号精确 / 姓名唯一匹配',
      '给「' + label + '」加负责人',
      { confirmButtonText: '添加', cancelButtonText: '取消', inputPlaceholder: '工号 / 姓名' },
    )
    keyword = String(ret.value || '').trim()
  } catch {
    return // 用户取消
  }
  if (!keyword) return
  try {
    await orgStore.addLeader(nodeType, nodeId, keyword)
    ElMessage.success('负责人已添加')
    await loadLeaders()
    void loadOverview(true)
  } catch (e: any) {
    if (e?.response?.status === 404) ElMessage.error('花名册查无此人，或节点不存在')
    else ElMessage.error(e?.response?.data?.error || e?.message || '加负责人失败')
  }
}

function onTreeAddLeader(node: TreeNode) {
  const id = node.type === 'studio' ? node.studioData?.id : node.deptData?.id
  if (id) void addLeaderPrompt(node.type, id, node.label)
}

function filterNode(value: string, data: TreeNode) {
  if (!value) return true
  return data.label.toLowerCase().includes(value.toLowerCase())
}

function onTreeSearch(val: string) {
  treeRef.value?.filter(val)
}

// ─── 选中状态（工作室工作台：工作室 + 当前团队）───────────────────────────────
const unmappedSelected       = ref(false)
const selectedStudio         = ref<Studio | null>(null)
const selectedDept           = ref<OrgDepartment | null>(null)
const selectedDeptParentStudio = ref<Studio | null>(null)
const activeTab              = ref('members')

// 工作台所在工作室：点工作室节点或团队节点都成立
const workbenchStudio = computed<Studio | null>(
  () => selectedStudio.value ?? selectedDeptParentStudio.value ?? null
)
// 指定/卸任管理员：admin 或该工作室的 studio_admin
const canManageStudio = computed(() => {
  const studio = workbenchStudio.value
  return !!studio && (isGlobalAdmin.value || myStudioIds.value.includes(studio.id))
})

// ─── 工作室概览（头部人数/管理员/负责人 + 下属团队卡）────────────────────────
// 后端 overview 接口并行开发中：失败或字段缺失时全部由本地数据兜底，页面照常可用。
const overview = ref<StudioOverview | null>(null)
const overviewLoadedId = ref('')   // 已成功加载的工作室
let overviewFetchingId = ''        // 在途请求（同一工作室只发一次）

async function loadOverview(force = false) {
  const studio = workbenchStudio.value
  if (!studio) {
    overview.value = null
    overviewLoadedId.value = ''
    overviewFetchingId = ''
    return
  }
  if (!force && (overviewLoadedId.value === studio.id || overviewFetchingId === studio.id)) return
  if (overview.value?.studio_id !== studio.id) overview.value = null
  // dept_admin 无工作室管辖权：不请求概览，避免无谓 403
  if (!isGlobalAdmin.value && !myStudioIds.value.includes(studio.id)) return
  overviewFetchingId = studio.id
  try {
    const data = await orgStore.fetchStudioOverview(studio.id)
    // 期间切了工作室就丢弃（避免串号）
    if (workbenchStudio.value?.id === studio.id) {
      overview.value = data
      overviewLoadedId.value = studio.id
    }
  } catch {
    if (workbenchStudio.value?.id === studio.id) overview.value = null
  } finally {
    if (overviewFetchingId === studio.id) overviewFetchingId = ''
  }
}

const studioInfo = computed<StudioOverview | null>(() =>
  overview.value && overview.value.studio_id === workbenchStudio.value?.id ? overview.value : null
)
const studioMemberCount = computed<number | null>(() => studioInfo.value?.member_count ?? null)

function briefOfRoles(role: 'studio_admin' | 'dept_admin', scopeId: string): AccountBrief[] {
  return allRoles.value
    .filter(r => r.role === role && r.scope_id === scopeId)
    .map(r => ({ user_id: r.user_id, name: r.user_name }))
}

const studioAdmins = computed<AccountBrief[]>(() => {
  const studio = workbenchStudio.value
  if (!studio) return []
  const fromOverview = studioInfo.value?.admins
  if (fromOverview && fromOverview.length) return fromOverview
  return briefOfRoles('studio_admin', studio.id)
})

const studioLeaders = computed<OrgLeaderBrief[]>(() => {
  const studio = workbenchStudio.value
  if (!studio) return []
  const fromOverview = studioInfo.value?.leaders
  if (fromOverview && fromOverview.length) return fromOverview
  return leadersOfNode('studio', studio.id)
})

// dept_admin（无工作室管辖权）只看自己管辖的团队
function canSeeDeptId(deptId: string): boolean {
  if (isGlobalAdmin.value) return true
  if (myStudioIds.value.length > 0) return true
  return myDeptIds.value.includes(deptId)
}

// 下属团队：优先 overview.teams；缺失时用本地部门列表兜底（人数/管理员/负责人同样兜底）
function teamsOf(studio: Studio): TeamCard[] {
  const fromOverview = studioInfo.value?.teams
  const depts = allDepts.value.filter(d => d.studio_id === studio.id)
  const base: TeamCard[] = fromOverview && fromOverview.length
    ? fromOverview
    : depts.map(d => ({
      dept_id: d.dept_id,
      dept_uuid: d.id,
      dept_name: d.dept_name,
      member_count: d.member_count ?? 0,
      admins: [],
      leaders: [],
    }))
  return base
    .filter(t => canSeeDeptId(t.dept_id))
    .map(t => ({
      ...t,
      member_count: t.member_count ?? depts.find(d => d.dept_id === t.dept_id)?.member_count ?? 0,
      admins: t.admins?.length ? t.admins : briefOfRoles('dept_admin', t.dept_id),
      leaders: t.leaders?.length ? t.leaders : leadersOfNode('dept', t.dept_uuid),
    }))
}

const visibleTeams = computed<TeamCard[]>(() =>
  workbenchStudio.value ? teamsOf(workbenchStudio.value) : []
)

// 团队卡 → 部门行（overview 里出现但本地部门列表没有时兜一个只读视图）
function deptOfTeam(team: TeamCard): OrgDepartment {
  const existing = allDepts.value.find(d => d.dept_id === team.dept_id)
  if (existing) return existing
  return {
    id: team.dept_uuid,
    dept_id: team.dept_id,
    dept_name: team.dept_name,
    studio_id: workbenchStudio.value?.id,
    status: 'active',
    created_at: '',
  }
}

const activeTeam = computed<TeamCard | null>(() => {
  const dept = selectedDept.value
  if (!dept) return null
  return visibleTeams.value.find(t => t.dept_id === dept.dept_id) ?? null
})

// 头部展示的「当前团队」：名称/状态/人数/管理员/负责人
const headerTeam = computed<TeamHeaderTeam | null>(() => {
  const dept = selectedDept.value
  if (!dept) return null
  const team = activeTeam.value
  return {
    deptId: dept.dept_id,
    name: dept.dept_name,
    status: dept.status,
    memberCount: deptMembersTotal.value || team?.member_count || 0,
    admins: team?.admins?.length ? team.admins : briefOfRoles('dept_admin', dept.dept_id),
    leaders: team?.leaders?.length ? team.leaders : leadersOfNode('dept', dept.id),
  }
})

// ─── 独立页面导航（不复制业务逻辑，只跳转）────────────────────────────────────
const NAV_TABS = [
  { name: 'service-key', label: 'Service Key', hint: 'Service Key 在独立页面按主体维护与查看。', path: '/admin/service-keys' },
  { name: 'entitlement', label: '模型授权', hint: '模型组授权按工作室 / 团队两级维护。', path: '/admin/entitlements' },
  // 用量看板当前只认 user_id/user_name/start/end，不认 studio_id/dept_id，所以裸路径跳转后需在看板内下钻
  { name: 'usage', label: '团队用量', hint: '分层用量看板：工作室 → 团队 → 成员。', path: '/admin/usage/dashboard' },
  { name: 'budget', label: '团队预算', hint: '团队预算池与成员预算规则在预算管理页维护。', path: '/admin/budget' },
  { name: 'requests', label: '申请审批', hint: '扩容 / 预算申请在审批页处理。', path: '/admin/expansion-requests' },
] as const

function gotoNavTab(tab: typeof NAV_TABS[number]) {
  if (tab.name === 'budget') {
    goBudget()
    return
  }
  router.push(tab.path)
}

// 预算页支持 scope 钻取（scope_type/scope_id/scope_label），团队/工作室上下文直接带上
function goBudget() {
  const studio = workbenchStudio.value
  const dept = selectedDept.value
  if (dept && studio) {
    router.push({
      path: '/admin/budget',
      query: {
        scope_type: 'dept',
        scope_id: dept.dept_id,
        scope_label: dept.dept_name,
        studio_id: studio.id,
        studio_label: studio.name,
      },
    })
    return
  }
  if (studio) {
    router.push({ path: '/admin/budget', query: { scope_type: 'studio', scope_id: studio.id, scope_label: studio.name } })
    return
  }
  router.push('/admin/budget')
}

function goEntitlement() {
  // ponytail: 模型授权只有工作室/团队两级且页面在 /admin/entitlements，暂无团队过滤查询参数；
  // 需要"只看本团队授权"时再给该页加 studio_id/dept_id query。
  router.push('/admin/entitlements')
}

// ─── 选中动作 ─────────────────────────────────────────────────────────────────
function selectStudio(studio: Studio) {
  unmappedSelected.value = false
  selectedStudio.value = studio
  selectedDeptParentStudio.value = null
  selectedDept.value = null
  deptMembers.value = []
  deptMembersTotal.value = 0
  activeTab.value = 'members'
  treeRef.value?.setCurrentKey(`studio:${studio.id}`)
  void loadOverview()
  const first = visibleTeams.value[0]
  if (first) selectTeam(deptOfTeam(first))
}

function selectTeam(dept: OrgDepartment) {
  unmappedSelected.value = false
  selectedDept.value = dept
  // 从团队节点进入时没有工作室上下文，用父工作室补齐（工作台头部需要）
  if (!selectedStudio.value) {
    selectedDeptParentStudio.value = studios.value.find(s => s.id === dept.studio_id) || null
  }
  activeTab.value = 'members'
  void loadOverview()
  loadDeptMembers(1)
}

function onNodeClick(data: TreeNode) {
  if (data.type === 'studio' && data.studioData) {
    selectStudio(data.studioData)
    return
  }
  if (data.type === 'dept' && data.deptData) {
    selectedStudio.value = data.parentStudio ?? null
    selectedDeptParentStudio.value = data.parentStudio ?? null
    selectTeam(data.deptData)
  }
}

function onTeamCardSelect(team: TeamCard) {
  selectTeam(deptOfTeam(team))
  treeRef.value?.setCurrentKey(`dept:${team.dept_id}`)
}

function onTeamCardSetAdmin(team: TeamCard) {
  selectTeam(deptOfTeam(team))
  openGrantDialog('dept_admin')
}

function onTeamCardAddLeader(team: TeamCard) {
  void addLeaderPrompt('dept', team.dept_uuid, team.dept_name)
}

function onAddStudioLeader() {
  const studio = workbenchStudio.value
  if (studio) void addLeaderPrompt('studio', studio.id, studio.name)
}

function onAddTeamLeader() {
  const dept = selectedDept.value
  if (dept) void addLeaderPrompt('dept', dept.id, dept.dept_name)
}

function onRevokeStudioAdmin(admin: AccountBrief) {
  const studio = workbenchStudio.value
  if (studio) void revokeManager(admin, 'studio_admin', studio.id)
}

function onRevokeTeamAdmin(admin: AccountBrief) {
  const dept = selectedDept.value
  if (dept) void revokeManager(admin, 'dept_admin', dept.dept_id)
}

function applyRouteSelection() {
  const scopeType = String(route.query.scope_type || '')
  const scopeID = String(route.query.scope_id || '')
  if (!scopeType || !scopeID) return
  const targetTab = String(route.query.tab || '')
  if (targetTab && NAV_TABS.some(t => t.name === targetTab)) activeTab.value = targetTab

  if (scopeType === 'studio') {
    const studio = studios.value.find(s => s.id === scopeID)
    if (studio) selectStudio(studio)
    return
  }

  if (scopeType === 'dept' || scopeType === 'dept_daily' || scopeType === 'dept_service') {
    const dept = allDepts.value.find(d => d.dept_id === scopeID)
    if (!dept) return
    selectedStudio.value = studios.value.find(s => s.id === dept.studio_id) || null
    selectedDeptParentStudio.value = selectedStudio.value
    selectTeam(dept)
    nextTick(() => treeRef.value?.setCurrentKey(`dept:${dept.dept_id}`))
  }
}

function selectUnmapped() {
  unmappedSelected.value = true
  selectedStudio.value = null
  selectedDept.value = null
  selectedDeptParentStudio.value = null
  overview.value = null
  // 取消树中的高亮
  treeRef.value?.setCurrentKey(undefined)
}

// ─── 团队成员 ─────────────────────────────────────────────────────────────────
const deptMembers        = ref<DeptMember[]>([])
const deptMembersTotal   = ref(0)
const deptMembersPage    = ref(1)
const deptMembersPageSize = 20
const deptMembersLoading = ref(false)
const memberSearch       = ref('')

const filteredMembers = computed(() => {
  if (!memberSearch.value) return deptMembers.value
  const kw = memberSearch.value.toLowerCase()
  return deptMembers.value.filter(m =>
    m.name.toLowerCase().includes(kw) || m.email.toLowerCase().includes(kw)
  )
})
// 后端在既有成员字段上补 is_dept_admin 与 config{quota,budget,entitlement}，字段缺失按 undefined 兜底
const memberRows = computed<MemberRow[]>(() => filteredMembers.value as MemberRow[])

async function loadDeptMembers(page = 1) {
  if (!selectedDept.value) return
  deptMembersPage.value = page
  deptMembersLoading.value = true
  try {
    const res = await orgStore.fetchDeptMembers(selectedDept.value.dept_id, page, deptMembersPageSize)
    deptMembers.value = res.members
    deptMembersTotal.value = res.total
    // 并行加载成员配额，供列表「配额」列回显
    loadDeptMembersQuotas(res.members)
  } catch {
    // 错误由 http 拦截器统一处理
  } finally {
    deptMembersLoading.value = false
  }
}

// ─── 管理员（RBAC，可授可撤）──────────────────────────────────────────────────
const grantDialogVisible  = ref(false)
const granting            = ref(false)
const grantPresetRole      = ref('')
const grantPresetScopeType = ref('')
const grantPresetScopeId   = ref('')
const grantPresetScopeName = ref('')
// 团队管理员只能从父工作室成员里选人；工作室管理员为空（全公司）
const grantScopeStudioId   = ref('')
const grantSearchKeyword   = ref('')
const grantSearchResults   = ref<UserSearchResult[]>([])
const grantSearchLoading   = ref(false)
const grantSelectedUser    = ref<UserSearchResult | null>(null)

function openGrantDialog(role: 'studio_admin' | 'dept_admin') {
  grantPresetRole.value = role
  if (role === 'studio_admin' && workbenchStudio.value) {
    grantPresetScopeType.value = 'studio'
    grantPresetScopeId.value   = workbenchStudio.value.id
    grantPresetScopeName.value = `工作室：${workbenchStudio.value.name}`
    grantScopeStudioId.value   = ''
  } else if (role === 'dept_admin' && selectedDept.value) {
    grantPresetScopeType.value = 'dept'
    grantPresetScopeId.value   = selectedDept.value.dept_id
    grantPresetScopeName.value = `团队：${selectedDept.value.dept_name}`
    grantScopeStudioId.value   = workbenchStudio.value?.id ?? ''
  }
  grantDialogVisible.value = true
}

let grantSearchTimer: ReturnType<typeof setTimeout> | null = null
function onGrantSearch() {
  if (grantSearchTimer) clearTimeout(grantSearchTimer)
  if (!grantSearchKeyword.value.trim()) { grantSearchResults.value = []; return }
  grantSearchTimer = setTimeout(async () => {
    grantSearchLoading.value = true
    try {
      grantSearchResults.value = await orgStore.searchUsers(
        grantSearchKeyword.value,
        20,
        grantScopeStudioId.value,
      )
    } finally {
      grantSearchLoading.value = false
    }
  }, 300)
}

async function doGrant() {
  if (!grantSelectedUser.value) return
  granting.value = true
  try {
    await orgStore.grantRole(
      grantSelectedUser.value.id,
      grantPresetRole.value,
      grantPresetScopeType.value,
      grantPresetScopeId.value,
    )
    const roleLabel = grantPresetRole.value === 'studio_admin' ? '工作室管理员' : '团队管理员'
    ElMessage.success(`已将「${grantSelectedUser.value.name}」设为${roleLabel}`)
    grantDialogVisible.value = false
    await refreshManagers()
  } catch {} finally {
    granting.value = false
  }
}

async function revokeManager(admin: AccountBrief, role: 'studio_admin' | 'dept_admin', scopeId: string) {
  const roleLabel = role === 'studio_admin' ? '工作室管理员' : '团队管理员'
  try {
    await ElMessageBox.confirm(`确认卸任「${admin.name}」的${roleLabel}？`, '卸任确认', {
      type: 'warning', confirmButtonText: '卸任', cancelButtonText: '取消',
    })
  } catch {
    return // 用户取消
  }
  try {
    await orgStore.revokeRole(admin.user_id, role, scopeId)
    ElMessage.success('已卸任')
    await refreshManagers()
  } catch {}
}

// 授权变更后：角色清单（树/兜底）+ 工作室概览（头部/团队卡）一起刷新
async function refreshManagers() {
  allRoles.value = await orgStore.fetchAllRoles()
  await loadOverview(true)
}

function resetGrant() {
  grantSearchKeyword.value  = ''
  grantSearchResults.value  = []
  grantSelectedUser.value   = null
  grantScopeStudioId.value  = ''
}

// ─── 数据加载 ─────────────────────────────────────────────────────────────────
async function reload() {
  try {
    const userId = authStore.user?.id
    const [, , , r, myR] = await Promise.all([
      orgStore.fetchStudios(),
      orgStore.fetchDepartments(),
      orgStore.fetchUnmapped(),
      orgStore.fetchAllRoles(),
      userId ? orgStore.getUserRoles(userId) : Promise.resolve({ roles: [] }),
    ])
    studios.value      = orgStore.studios
    allDepts.value     = orgStore.departments
    unmappedDepts.value = orgStore.unmappedDepts
    allRoles.value     = r
    myOrgRoles.value   = myR?.roles || []

    // 默认展开全部工作室节点
    defaultExpandedKeys.value = studios.value.map(s => `studio:${s.id}`)

    // 刷新后同步当前选中对象
    if (selectedStudio.value) {
      selectedStudio.value = studios.value.find(s => s.id === selectedStudio.value!.id) || null
    }
    if (selectedDept.value) {
      const nextDept = allDepts.value.find(d => d.dept_id === selectedDept.value!.dept_id) || null
      if (nextDept) selectTeam(nextDept)
      else {
        selectedDept.value = null
        deptMembers.value = []
        deptMembersTotal.value = 0
      }
    }
    applyRouteSelection()
  } finally {
    // 加载态由各列表自身的 v-loading 承担（页面级无整块遮罩）
  }
  await loadOverview(true)
}

onMounted(async () => {
  await reload()
  void loadSyncStatus()
  void loadLeaders()
})

// immediate: true 保证跨页首次进入时（数据已缓存）也能正确选中节点；
// reload() 末尾亦调用，两次调用互不干扰（未找到时直接返回）。
watch(
  () => [route.query.scope_type, route.query.scope_id, route.query.tab],
  () => {
    // 若数据尚未加载（reload 正在进行），稍后由 reload 末尾的调用补偿
    if (allDepts.value.length > 0 || studios.value.length > 0) {
      applyRouteSelection()
    }
  },
  { immediate: true },
)

// ─── 成员用量快览抽屉 ──────────────────────────────────────────────────────────
const memberDrawerVisible = ref(false)
const memberDrawerTarget  = ref<DeptMember | null>(null)

const mdLoading     = ref(false)
const mdLogsLoading = ref(false)
const mdTrendLoading = ref(false)
const mdKpi      = ref({ callCount: 0, cost: 0, successRate: 0, totalTokens: 0, errorCount: 0, avgDuration: 0 })
const mdLogs     = ref<any[]>([])
const mdTrendData = ref<any[]>([])
const mdTopModels = ref<any[]>([])
const mdTrendCanvasRef = ref<HTMLCanvasElement | null>(null)
let mdTrendChart: Chart | null = null

async function fetchMemberDetail() {
  if (!memberDrawerTarget.value) return
  const uid   = memberDrawerTarget.value.id
  const end   = new Date().toISOString().slice(0, 10)
  const start = new Date(Date.now() - 29 * 86400000).toISOString().slice(0, 10)

  mdLoading.value      = true
  mdLogsLoading.value  = true
  mdTrendLoading.value = true

  const [dashRes, logsRes, trendRes, modelsRes] = await Promise.allSettled([
    http.get('/api/v1/usage/dashboard', { params: { scope: 'user', id: uid, start, end, page_size: 1 } }),
    http.get('/api/v1/usage/scoped/logs', { params: { user_id: uid, start, end, page: 1, page_size: 10 } }),
    http.get('/api/v1/usage/dashboard/trend', { params: { user_id: uid, start, end } }),
    http.get('/api/v1/usage/dashboard/models', { params: { user_id: uid, start, end, limit: 5 } }),
  ])

  if (dashRes.status === 'fulfilled') {
    const s = dashRes.value.data?.summary || {}
    mdKpi.value = {
      callCount:   s.call_count    || 0,
      cost:        s.cost_estimate || 0,
      successRate: s.success_rate  || 0,
      totalTokens: s.total_tokens  || 0,
      errorCount:  s.error_count   || 0,
      avgDuration: 0,
    }
  }
  mdLoading.value = false

  if (logsRes.status === 'fulfilled') {
    mdLogs.value = logsRes.value.data?.rows || []
    if (mdLogs.value.length > 0) {
      const total = mdLogs.value.reduce((s: number, r: any) => s + (r.duration_ms || 0), 0)
      mdKpi.value.avgDuration = total / mdLogs.value.length
    }
  }
  mdLogsLoading.value = false

  if (trendRes.status === 'fulfilled') {
    mdTrendData.value = trendRes.value.data?.rows || []
  }
  if (modelsRes.status === 'fulfilled') {
    mdTopModels.value = modelsRes.value.data?.rows || []
  }
  mdTrendLoading.value = false
  await nextTick()
  renderMdTrendChart()
}

function renderMdTrendChart() {
  if (!mdTrendCanvasRef.value || mdTrendData.value.length === 0) return
  mdTrendChart?.destroy()
  const labels = mdTrendData.value.map((d: any) => d.date?.slice(5) ?? '')
  const calls  = mdTrendData.value.map((d: any) => d.calls ?? 0)
  const errors = mdTrendData.value.map((d: any) => d.errors ?? 0)
  mdTrendChart = new Chart(mdTrendCanvasRef.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [
        { label: '成功', data: calls.map((c: number, i: number) => c - errors[i]), backgroundColor: 'rgba(64,158,255,0.55)', borderRadius: 3, stack: 'a' },
        { label: '失败', data: errors, backgroundColor: 'rgba(245,108,108,0.7)', borderRadius: 3, stack: 'a' },
      ],
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: { legend: { position: 'bottom', labels: { boxWidth: 10, padding: 12, font: { size: 11 } } } },
      scales: {
        x: { grid: { display: false }, ticks: { maxTicksLimit: 10, font: { size: 10 } } },
        y: { grid: { color: 'rgba(0,0,0,0.04)' }, ticks: { font: { size: 10 } }, stacked: true },
      },
    },
  } as any)
}

function openMemberDetail(member: MemberRow) {
  memberDrawerTarget.value  = member
  memberDrawerVisible.value = true
  mdKpi.value       = { callCount: 0, cost: 0, successRate: 0, totalTokens: 0, errorCount: 0, avgDuration: 0 }
  mdLogs.value      = []
  mdTrendData.value = []
  mdTopModels.value = []
  mdTrendChart?.destroy()
  mdTrendChart = null
  fetchMemberDetail()
}

onUnmounted(() => { mdTrendChart?.destroy() })

function goToMemberDashboard() {
  if (!memberDrawerTarget.value) return
  const end   = new Date().toISOString().slice(0, 10)
  const start = new Date(Date.now() - 29 * 86400000).toISOString().slice(0, 10)
  router.push({
    name: 'UsageDashboard',
    query: {
      user_id:   memberDrawerTarget.value.id,
      user_name: memberDrawerTarget.value.name,
      start,
      end,
    },
  })
  memberDrawerVisible.value = false
}

// ─── 用户配额编辑 ────────────────────────────────────────────────────────────────
const userQuotaDialogVisible = ref(false)
const userQuotaSubjectId = ref('')
const userQuotaSubjectLabel = ref('')
const userQuotaMap = ref<Record<string, QuotaEffective>>({})

function openUserQuotaDialog(member: DeptMember) {
  userQuotaSubjectId.value = member.id
  userQuotaSubjectLabel.value = `用户：${member.name}（${member.email}）`
  userQuotaDialogVisible.value = true
}

// 后端响应为 { code:0, data:{...} }，真实对象在 res.data.data
async function loadUserQuota(userId: string) {
  if (!userId) return
  try {
    const res = await getUserQuota(userId)
    const data = (res as any)?.data?.data
    if (data) userQuotaMap.value[userId] = data
  } catch {
    // 404 或错误：不填充，列显示占位符
  }
}

async function loadDeptMembersQuotas(members: DeptMember[]) {
  await Promise.all(members.map(m => loadUserQuota(m.id)))
}

function onUserQuotaSaved() {
  // 配额保存后刷新该用户的配额，使列表列即时回显
  if (userQuotaSubjectId.value) {
    loadUserQuota(userQuotaSubjectId.value)
  }
}
</script>

<style scoped>
/* ─── 整体布局：取消外层 page-container padding，铺满高度 ──────────────────── */
.org-layout {
  display: flex;
  height: calc(100vh - 52px);
  margin: -20px -24px;
  overflow: hidden;
  background: var(--el-bg-color-page);
}

/* ─── 左侧树面板 ────────────────────────────────────────────────────────────── */
.tree-panel {
  width: 260px;
  min-width: 210px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
  flex-shrink: 0;
  overflow: hidden;
}

.tree-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px 8px;
  flex-shrink: 0;
}

.tree-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  letter-spacing: .3px;
}

.tree-search {
  padding: 0 10px 8px;
  flex-shrink: 0;
}

.tree-body {
  flex: 1;
  overflow-y: auto;
}

/* el-tree 节点样式 */
:deep(.el-tree-node__content) {
  height: 36px;
  padding-right: 8px;
}

:deep(.el-tree-node.is-current > .el-tree-node__content) {
  background-color: var(--el-color-primary-light-9);
}

.node-leaders {
  margin-left: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.node-add-leader {
  margin-left: 2px;
  padding: 0 2px;
  font-size: 12px;
}

.tree-node-content {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  font-size: 13px;
}

.node-icon { flex-shrink: 0; }
.studio-icon { color: var(--el-color-primary); }
.dept-icon { color: var(--el-text-color-placeholder); }

.node-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-badge { flex-shrink: 0; }

/* 待归属伪节点 */
.unmapped-node {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 26px 8px 18px;
  font-size: 13px;
  cursor: pointer;
  border-top: 1px solid var(--el-border-color-extra-light);
  margin-top: 4px;
  transition: background .15s;
  color: var(--el-color-warning);
}

.unmapped-node:hover { background: var(--el-fill-color-lighter); }
.unmapped-node.is-current { background: var(--el-color-warning-light-9); }

.node-count { flex-shrink: 0; }

/* ─── 右侧内容面板 ──────────────────────────────────────────────────────────── */
.content-panel {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

/* 待归属视图头部（工作室头部在 TeamHeader 组件内） */
.panel-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 24px 0;
  flex-shrink: 0;
}

.panel-info { min-width: 0; }

.panel-breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.crumb-arrow { font-size: 10px; }

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 18px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.title-icon { color: var(--el-color-primary); font-size: 18px; }
.title-badge { margin-left: 4px; }

/* Tabs */
.panel-tabs {
  flex: 1;
  padding: 0 24px;
  margin-top: 16px;
}

:deep(.el-tabs__content) {
  overflow: visible;
}

.tab-content {
  padding: 16px 0;
}

.nav-body { padding-top: 0; }

.tab-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  margin-left: 4px;
  font-size: 11px;
  background: var(--el-fill-color-dark);
  color: var(--el-text-color-secondary);
  border-radius: 9px;
  vertical-align: middle;
}

.tab-empty {
  padding: 32px 0;
  text-align: center;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

/* 独立页面导航占位（不复制业务逻辑） */
.nav-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 48px 16px;
  border: 1px dashed var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
}
.nav-placeholder__title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.nav-placeholder__sub {
  margin: 0 0 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

/* 成员列表 */
.members-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.members-hint { font-size: 12px; color: var(--el-text-color-secondary); }
.members-pagination { display: flex; justify-content: center; margin-top: 16px; }

/* 空态 */
.content-empty {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; flex: 1;
  gap: 10px; padding: 60px 0;
  color: var(--el-text-color-secondary);
}
.empty-title { font-size: 16px; font-weight: 600; margin: 0; }
.empty-sub { font-size: 13px; color: var(--el-text-color-placeholder); margin: 0; }

/* Table horizontal scroll wrapper */
.org-table-wrap { width: 100%; }

/* 部门 ID code */
.dept-id-code {
  font-family: monospace;
  font-size: 12px;
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
  color: var(--el-text-color-secondary);
}

/* ─── 弹窗：设置管理员 ───────────────────────────────────────────────────────── */
.step-label { font-size: 13px; font-weight: 600; color: var(--el-text-color-secondary); margin-bottom: 10px; }
.search-step { min-height: 180px; }
.search-results {
  margin-top: 8px; border: 1px solid var(--el-border-color);
  border-radius: 6px; overflow: hidden; max-height: 220px; overflow-y: auto;
}
.search-result-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; cursor: pointer;
  border-bottom: 1px solid var(--el-border-color-extra-light);
  transition: background .15s;
}
.search-result-item:last-child { border-bottom: none; }
.search-result-item:hover { background: var(--el-fill-color-light); }
.result-avatar {
  width: 32px; height: 32px; border-radius: 50%;
  background: var(--el-color-primary-light-8); color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 13px; font-weight: 600; flex-shrink: 0;
}
.result-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.result-name { font-size: 14px; font-weight: 500; }
.result-meta { font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-hint { padding: 12px 0; font-size: 13px; color: var(--el-text-color-placeholder); text-align: center; }

.grant-step { display: flex; flex-direction: column; gap: 0; }
.selected-user {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; background: var(--el-fill-color-light); border-radius: 6px;
}
.selected-avatar {
  width: 36px; height: 36px; border-radius: 50%;
  background: var(--el-color-primary-light-5); color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 15px; font-weight: 600; flex-shrink: 0;
}
.selected-info { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.selected-name { font-size: 14px; font-weight: 600; }
.selected-meta { font-size: 12px; color: var(--el-text-color-secondary); }

.grant-scope-info { margin-top: 12px; }

code { background: var(--el-fill-color); padding: 1px 4px; border-radius: 3px; font-size: 12px; }

/* ─── 成员用量快览抽屉 ────────────────────────────────────────────────────────── */
:deep(.md-drawer .el-drawer__body) { padding: 20px; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; }

/* 头部 */
.md-header {
  display: flex; align-items: flex-start; gap: 12px;
}
.md-avatar {
  width: 44px; height: 44px; border-radius: 50%; flex-shrink: 0;
  background: var(--el-color-primary-light-7); color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; font-weight: 700; border: 2px solid var(--el-color-primary-light-5);
}
.md-info    { flex: 1; min-width: 0; }
.md-name    { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.md-email   { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.md-tags    { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 6px; }
.md-close   { margin-left: auto; flex-shrink: 0; color: var(--el-text-color-secondary); }

.md-divider { height: 1px; background: var(--el-border-color-light); }

/* 分区标签（与主看板 section title 同风格） */
.md-section-label {
  font-size: 12px; font-weight: 600; color: var(--el-text-color-secondary);
  display: flex; align-items: center; gap: 6px;
}
.md-section-label::before {
  content: ''; width: 3px; height: 12px; border-radius: 2px;
  background: var(--el-color-primary); display: inline-block; flex-shrink: 0;
}

/* KPI 卡片 2×2 网格 —— 与主看板风格一致 */
.md-kpi-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; }
.md-kpi-card {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  padding: 14px 12px 12px;
  display: flex; align-items: flex-start; gap: 10px;
  position: relative; overflow: hidden;
}
.md-kpi-card::before {
  content: ''; position: absolute; top: 0; left: 0;
  width: 3px; height: 100%; border-radius: 12px 0 0 12px;
}
.md-kpi--green::before { background: var(--el-color-success); }
.md-kpi--blue::before  { background: var(--el-color-primary); }
.md-kpi--amber::before { background: var(--el-color-warning); }
.md-kpi--red::before   { background: var(--el-color-danger); }
.md-kpi--gray::before  { background: var(--el-text-color-placeholder); }

.md-kpi__icon {
  width: 34px; height: 34px; border-radius: 8px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.md-kpi--green .md-kpi__icon { background: var(--el-color-success-light-9); color: var(--el-color-success); }
.md-kpi--blue  .md-kpi__icon { background: var(--el-color-primary-light-9);   color: var(--el-color-primary); }
.md-kpi--amber .md-kpi__icon { background: var(--el-color-warning-light-9);  color: var(--el-color-warning); }
.md-kpi--red   .md-kpi__icon { background: var(--el-color-danger-light-9);   color: var(--el-color-danger); }
.md-kpi--gray  .md-kpi__icon { background: var(--el-fill-color);   color: var(--el-text-color-secondary); }

.md-kpi__body  { flex: 1; min-width: 0; }
.md-kpi__label { font-size: 11px; color: var(--el-text-color-secondary); margin: 0 0 3px; }
.md-kpi__value { font-size: 20px; font-weight: 700; margin: 0 0 2px; line-height: 1.2; color: var(--el-text-color-primary); }
.md-kpi__sub   { font-size: 11px; color: var(--el-text-color-placeholder); margin: 0; }

/* 最近调用列表 */
.md-empty { color: var(--el-text-color-placeholder); font-size: 13px; text-align: center; padding: 20px 0; }
.md-log-list { display: flex; flex-direction: column; gap: 1px; border: 1px solid var(--el-border-color-light); border-radius: 10px; overflow: hidden; }
.md-log-row {
  display: flex; align-items: center; gap: 10px;
  padding: 9px 12px; background: var(--el-fill-color-blank);
  border-bottom: 1px solid var(--el-border-color-lighter);
  transition: background .1s;
}
.md-log-row:last-child { border-bottom: none; }
.md-log-row:hover { background: var(--el-fill-color-light); }
.md-log-status { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.md-log-status--ok  { background: var(--el-color-success); }
.md-log-status--err { background: var(--el-color-danger); }
.md-log-main  { display: flex; flex-direction: column; gap: 1px; flex: 1; min-width: 0; }
.md-log-model { font-size: 12px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; color: var(--el-text-color-primary); }
.md-log-time  { font-size: 11px; color: var(--el-text-color-placeholder); }
.md-log-cost  { font-size: 12px; font-family: monospace; color: var(--el-text-color-secondary); flex-shrink: 0; }

/* 趋势图 */
.md-trend-wrap { height: 140px; position: relative; }
.md-trend-canvas { width: 100% !important; height: 100% !important; }

/* Top 模型列表 */
.md-models-list { display: flex; flex-direction: column; gap: 10px; }
.md-model-row { display: flex; align-items: center; gap: 10px; }
.md-model-rank {
  width: 20px; height: 20px; border-radius: 6px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--el-text-color-secondary);
  display: flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 700;
}
.md-model-info { flex: 1; min-width: 0; }
.md-model-name-row { display: flex; justify-content: space-between; margin-bottom: 4px; }
.md-model-name  { font-size: 12px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 220px; }
.md-model-calls { font-size: 11px; color: var(--el-text-color-secondary); flex-shrink: 0; }
.md-model-bar-bg { height: 5px; background: var(--el-fill-color); border-radius: 3px; overflow: hidden; }
.md-model-bar-fill { height: 100%; background: var(--el-color-primary-light-5); border-radius: 3px; transition: width .4s; }
.md-model-cost { font-size: 12px; font-family: monospace; color: var(--el-text-color-secondary); flex-shrink: 0; min-width: 60px; text-align: right; }

/* 调用记录 */
.md-log-right { display: flex; flex-direction: column; align-items: flex-end; gap: 1px; flex-shrink: 0; }
.md-log-tokens { font-size: 10px; color: var(--el-text-color-placeholder); }

/* 底部按钮 */
.md-footer { margin-top: auto; }

/* ─── 响应式 ─────────────────────────────────────────────────────────────────── */
@media (max-width: 768px) {
  .org-layout { flex-direction: column; height: auto; }
  .tree-panel { width: 100%; border-right: none; border-bottom: 1px solid var(--el-border-color); }
  .tree-body { max-height: 280px; }

  /* Header: stack vertically */
  .panel-header { flex-direction: column; align-items: flex-start; gap: 8px; padding: 16px 16px 0; }

  /* Tabs: reduce padding */
  .panel-tabs { padding: 0 16px; }

  /* Tables: overflow-x auto */
  .org-table-wrap { overflow-x: auto; width: 100%; }
  .org-table-wrap :deep(.el-table) { min-width: 680px; }

  /* Member toolbar: full-width */
  .members-toolbar { flex-direction: column; align-items: flex-start; }
  .members-toolbar .el-input { width: 100% !important; }

  /* Pagination: center */
  .members-pagination { justify-content: center; }

  /* Drawer: use narrower size on mobile */
  :deep(.el-drawer) { width: 100% !important; max-width: 100vw; }
  :deep(.md-drawer .el-drawer__body) { padding: 16px; }

  /* KPI grid in drawer: single column */
  .md-kpi-grid { grid-template-columns: 1fr; }

  /* Dialog: responsive width */
  :deep(.el-dialog) { width: 92% !important; max-width: 92vw !important; }

  /* Breadcrumb: reduce size */
  .panel-breadcrumb { font-size: 11px; }
  .panel-title { font-size: 16px; }
}

@media (max-width: 480px) {
  .panel-header { padding: 12px 12px 0; }
  .panel-tabs { padding: 0 12px; }
  .panel-title { font-size: 15px; }
  .tree-panel { min-width: 0; }
  :deep(.md-drawer .el-drawer__body) { padding: 12px; }
}
</style>
