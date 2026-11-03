<template>
  <div class="budget-overview">
    <header class="overview-header">
      <div>
        <h1>预算总览</h1>
        <p v-if="auth.effectiveRole === 'admin'">查看全公司各工作室预算风险与消耗；点击「调整」可直接在当前页面配置预算。</p>
        <p v-else-if="auth.effectiveRole === 'studio_admin'">查看工作室下各部门预算状态；点击「调整」可直接在当前页面配置预算。</p>
        <p v-else>查看本部门预算与成员用量风险，可直接调整成员配额。</p>
      </div>
      <div class="header-actions">
        <el-button :loading="loading" @click="load">刷新</el-button>
        <el-button v-if="auth.effectiveRole === 'admin'" type="primary" @click="router.push('/admin/org')">
          进入组织治理
        </el-button>
        <el-button v-else type="primary" @click="goOrg()">
          {{ isDeptAdmin ? '配置部门预算' : '配置工作室预算' }}
        </el-button>
    </div>
    </header>

    <!-- KPI 条：角色自适应列数 -->
    <section
      class="kpi-strip"
      :class="{
        'kpi-5': (isDeptAdmin && managedDeptIds.length <= 1) || overviewView === 'studio' || overviewView === 'company',
        'kpi-dept-multi': isDeptAdmin && managedDeptIds.length > 1,
      }"
    >
      <template v-if="isDeptAdmin">

        <!-- ① 单部门：两张池卡 + 3 张辅助卡（共 5 张） -->
        <template v-if="managedDeptIds.length <= 1">
          <div class="kpi kpi-pool" :class="`kpi-pool--${budgetColorLevel(deptDailyKpi.ratio)}`">
            <div class="kpi-pool-head">
              <span class="kpi-label">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_daily'] }" />
                部门日常池
              </span>
              <el-tag :type="budgetColorLevel(deptDailyKpi.ratio)" size="small" style="flex-shrink:0">
                {{ statusText(deptDailyKpi.ratio) }}
              </el-tag>
            </div>
            <strong>{{ displayAmount(deptDailyKpi.amount) }}</strong>
            <div class="kpi-pool-usage">
              <span>已用 {{ formatUSD(deptDailyKpi.used, { compact: true }) }}</span>
              <span>剩余 {{ displayAmount(deptDailyKpi.amount - deptDailyKpi.used) }}</span>
            </div>
            <el-progress :percentage="Math.min(Math.round(deptDailyKpi.ratio * 100), 100)" :stroke-width="4" :color="riskColor(deptDailyKpi.ratio)" :show-text="false" style="margin-top:8px" />
          </div>

          <div class="kpi kpi-pool" :class="`kpi-pool--${budgetColorLevel(deptServiceKpi.ratio)}`">
            <div class="kpi-pool-head">
              <span class="kpi-label">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_service'] }" />
                部门服务池
              </span>
              <el-tag :type="budgetColorLevel(deptServiceKpi.ratio)" size="small" style="flex-shrink:0">
                {{ statusText(deptServiceKpi.ratio) }}
              </el-tag>
            </div>
            <strong>{{ displayAmount(deptServiceKpi.amount) }}</strong>
            <div class="kpi-pool-usage">
              <span>已用 {{ formatUSD(deptServiceKpi.used, { compact: true }) }}</span>
              <span>剩余 {{ displayAmount(deptServiceKpi.amount - deptServiceKpi.used) }}</span>
            </div>
            <el-progress :percentage="Math.min(Math.round(deptServiceKpi.ratio * 100), 100)" :stroke-width="4" :color="riskColor(deptServiceKpi.ratio)" :show-text="false" style="margin-top:8px" />
          </div>
      </template>

        <!-- ② 多部门：把池卡替换成"部门状态总览"横幅（跨满整行） -->
        <div v-else class="dept-pools-banner">
          <div class="dept-pools-banner-title">管辖部门预算池</div>
          <div class="dept-pools-grid">
            <div
              v-for="grp in deptPoolsByDept"
              :key="grp.deptId"
              class="dept-pool-group"
              :class="`dept-pool-group--${budgetColorLevel(grp.maxRatio)}`"
            >
              <div class="dept-pool-group-name">
                <span class="risk-indicator" :class="`risk-${budgetColorLevel(grp.maxRatio)}`" />
                {{ grp.label || grp.deptId }}
          </div>
              <div class="dept-pool-row" v-if="grp.daily">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_daily'] }" />
                <span class="pool-type-label">日常</span>
                <span class="pool-amount">{{ displayAmount(grp.daily.amount_usd) }}</span>
                <el-progress
                  style="flex:1; min-width:60px"
                  :percentage="Math.min(Math.round((grp.daily.used_ratio || 0) * 100), 100)"
                  :stroke-width="4"
                  :color="riskColor(grp.daily.used_ratio || 0)"
                  :show-text="false"
                />
                <el-tag :type="budgetColorLevel(grp.daily.used_ratio || 0)" size="small">
                  {{ statusText(grp.daily.used_ratio || 0) }}
                </el-tag>
        </div>
              <div class="dept-pool-row" v-if="grp.service">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_service'] }" />
                <span class="pool-type-label">服务</span>
                <span class="pool-amount">{{ displayAmount(grp.service.amount_usd) }}</span>
                <el-progress
                  style="flex:1; min-width:60px"
                  :percentage="Math.min(Math.round((grp.service.used_ratio || 0) * 100), 100)"
                  :stroke-width="4"
                  :color="riskColor(grp.service.used_ratio || 0)"
                  :show-text="false"
                />
                <el-tag :type="budgetColorLevel(grp.service.used_ratio || 0)" size="small">
                  {{ statusText(grp.service.used_ratio || 0) }}
                </el-tag>
      </div>
        </div>
        </div>
        </div>

        <!-- 右侧 3 张辅助卡（单/多部门共用） -->
        <div class="kpi">
          <span class="kpi-label">近7天消耗</span>
          <strong>{{ formatUSD(totalCost7d, { compact: true }) }}</strong>
          <span class="kpi-note">call_logs 实时</span>
        </div>
        <div class="kpi" :class="{ 'kpi-danger': riskMembers.length > 0 }">
          <span class="kpi-label">风险成员</span>
          <strong :class="{ danger: riskMembers.length > 0 }">{{ riskMembers.length }}</strong>
          <span class="kpi-note" :style="{ color: riskMembers.length > 0 ? 'var(--el-color-warning)' : undefined }">
            {{ riskMembers.length > 0 ? '需要处理' : '暂无风险' }}
          </span>
        </div>
        <div class="kpi kpi-action-card" @click="goExpansion">
          <span class="kpi-label">扩容申请</span>
          <strong class="kpi-action-text">查看</strong>
          <span class="kpi-note">审批待处理申请 →</span>
        </div>
      </template>
      <!-- admin：5 张卡（对齐 studio_admin 布局） -->
      <template v-else-if="overviewView === 'company'">
        <!-- 公司预算池（核心卡，展示下发分配率） -->
        <div class="kpi kpi-pool kpi-studio-pool">
          <div class="kpi-pool-head">
            <span class="kpi-label">
              <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['company'] }" />
              公司预算池
            </span>
            <el-tag type="info" size="small">下发率 {{ Math.round(companyAllocRatio * 100) }}%</el-tag>
          </div>
          <strong>{{ companyPoolItem ? displayAmount(companyPoolItem.amount_usd) : '—' }}</strong>
          <div class="kpi-pool-usage">
            <span>已下发 {{ formatUSD(companyAllocated, { compact: true }) }}</span>
            <span>余量 {{ formatUSD(companyUnallocated, { compact: true }) }}</span>
            <span>本月已用 {{ companyPoolItem ? formatUSD(companyPoolItem.used_usd, { compact: true }) : '—' }}</span>
          </div>
          <el-progress
            :percentage="Math.min(Math.round(companyAllocRatio * 100), 100)"
            :stroke-width="4"
            :color="companyAllocRatio >= 0.95 ? '#e6a23c' : '#36B34E'"
            :show-text="false"
            style="margin-top:8px"
          />
          <div v-if="companyPoolItem && (companyPoolItem.weekly_amount_usd >= 0 || companyPoolItem.daily_amount_usd >= 0)" class="kpi-pool-sub-limits">
            <div v-if="companyPoolItem.weekly_amount_usd >= 0" class="kpi-sub-limit-row">
              <span class="kpi-sub-limit-label">周限额</span>
              <span class="kpi-sub-limit-used">本周已用 {{ formatUSD(companyPoolItem.weekly_used_usd ?? 0, { compact: true }) }}</span>
              <span class="kpi-sub-limit-slash">/</span>
              <span class="kpi-sub-limit-amount">{{ displayAmount(companyPoolItem.weekly_amount_usd) }}</span>
            </div>
            <div v-if="companyPoolItem.daily_amount_usd >= 0" class="kpi-sub-limit-row">
              <span class="kpi-sub-limit-label">日限额</span>
              <span class="kpi-sub-limit-used">今日已用 {{ formatUSD(companyPoolItem.daily_used_usd ?? 0, { compact: true }) }}</span>
              <span class="kpi-sub-limit-slash">/</span>
              <span class="kpi-sub-limit-amount">{{ displayAmount(companyPoolItem.daily_amount_usd) }}</span>
            </div>
          </div>
          <div class="kpi-pool-actions">
            <el-button size="small" type="primary" plain @click="openCompanyBudgetDialog">调整</el-button>
            <el-tooltip content="Redis 数据丢失后，从 call_logs 重算当月/周/日用量并写回" placement="top">
              <el-button size="small" :loading="rebuildLoading" @click="rebuildCounters">重建计数器</el-button>
            </el-tooltip>
          </div>
        </div>

        <!-- 未下发余量 -->
        <div class="kpi" :class="{ 'kpi-pool--warning': companyUnallocated === 0 && companyAllocated > 0 }">
          <span class="kpi-label">未下发余量</span>
          <strong>{{ formatUSD(companyUnallocated, { compact: true }) }}</strong>
          <span class="kpi-note">
            {{ companyUnallocated > 0 ? '可继续分配给工作室' : companyPoolItem ? '已全部下发' : '—' }}
          </span>
      </div>

        <!-- 风险工作室 -->
        <div class="kpi" :class="{ 'kpi-danger': riskItems.length > 0 }">
          <span class="kpi-label">风险工作室</span>
          <strong :class="{ danger: riskItems.length > 0 }">{{ riskItems.length }}</strong>
          <span class="kpi-note" :style="{ color: riskItems.length > 0 ? 'var(--el-color-warning)' : undefined }">
            {{ riskItems.length > 0 ? '需要关注' : '暂无风险' }}
          </span>
    </div>

        <!-- 工作室总数 -->
        <div class="kpi">
          <span class="kpi-label">管辖工作室</span>
          <strong>{{ kpiItems.length }}</strong>
          <span class="kpi-note">个工作室</span>
        </div>

        <!-- 待审申请 -->
        <div class="kpi kpi-action-card" @click="goExpansion">
          <span class="kpi-label">扩容申请</span>
          <strong class="kpi-action-text">查看</strong>
          <span class="kpi-note">审批平台扩容申请 →</span>
        </div>
      </template>

      <!-- studio_admin：工作室池卡 + 4 张辅助卡（共 5 张） -->
      <template v-else>
        <!-- 工作室预算池：核心卡，展示下发分配率 -->
        <div class="kpi kpi-pool kpi-studio-pool">
          <div class="kpi-pool-head">
            <span class="kpi-label">
              <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['studio'] }" />
              工作室预算池
            </span>
            <div style="display:flex;gap:6px;align-items:center;flex-shrink:0">
              <el-tag type="info" size="small">下发率 {{ Math.round(studioAllocRatio * 100) }}%</el-tag>
              <el-button size="small" plain style="padding:2px 8px;height:22px;font-size:11px" @click="goOrg()">配置预算</el-button>
            </div>
          </div>
          <strong>{{ studioPoolItem ? displayAmount(studioPoolItem.amount_usd) : '—' }}</strong>
          <div class="kpi-pool-usage">
            <span>已下发 {{ formatUSD(studioAllocated, { compact: true }) }}</span>
            <span>余量 {{ formatUSD(studioUnallocated, { compact: true }) }}</span>
            <span>已使用 {{ formatUSD(totalUsed, { compact: true }) }}</span>
          </div>
          <el-progress
            :percentage="Math.min(Math.round(studioAllocRatio * 100), 100)"
            :stroke-width="4"
            :color="studioAllocRatio >= 0.95 ? '#e6a23c' : '#36B34E'"
            :show-text="false"
            style="margin-top:8px"
          />
      </div>

        <!-- 未下发余量 -->
        <div class="kpi" :class="{ 'kpi-pool--warning': studioUnallocated === 0 && studioAllocated > 0 }">
          <span class="kpi-label">未下发余量</span>
          <strong>{{ formatUSD(studioUnallocated, { compact: true }) }}</strong>
          <span class="kpi-note">
            {{ studioUnallocated > 0 ? '可继续分配给部门' : studioPoolItem ? '已全部下发' : '—' }}
          </span>
      </div>

        <!-- 风险部门 -->
        <div class="kpi" :class="{ 'kpi-danger': riskDeptCount > 0 }">
          <span class="kpi-label">风险部门</span>
          <strong :class="{ danger: riskDeptCount > 0 }">{{ riskDeptCount }}</strong>
          <span class="kpi-note" :style="{ color: riskDeptCount > 0 ? 'var(--el-color-warning)' : undefined }">
            {{ riskDeptCount > 0 ? '需要关注' : '暂无风险' }}
          </span>
      </div>

        <!-- 部门数 -->
        <div class="kpi">
          <span class="kpi-label">管辖部门</span>
          <strong>{{ studioDeptRows.length }}</strong>
          <span class="kpi-note">个部门</span>
      </div>

        <!-- 待审申请 -->
        <div class="kpi kpi-action-card" @click="goExpansion">
          <span class="kpi-label">扩容申请</span>
          <strong class="kpi-action-text">查看</strong>
          <span class="kpi-note">审批部门申请 →</span>
            </div>
      </template>
    </section>

    <!-- ══════════════════════════════════════════════
         admin / studio_admin：双栏布局
         ══════════════════════════════════════════════ -->
    <section v-if="!isDeptAdmin" class="overview-grid">
      <div class="panel panel-main">
        <div class="panel-head">
          <div>
            <h2>{{ tableTitle }}</h2>
            <p>{{ tableDesc }}</p>
              </div>
          <el-segmented
            v-if="riskItems.length > 0 || riskDeptCount > 0"
            v-model="riskFilter"
            :options="riskOptions"
            size="small"
          />
            </div>

        <div v-if="!loading && riskItems.length === 0" class="healthy-state">
          <div class="healthy-icon">✓</div>
          <div>
            <strong>当前管理范围内暂无预算风险</strong>
            <span>下方展示消耗最高的对象，便于提前观察趋势。</span>
          </div>
        </div>

        <!-- admin 视图：工作室维度表格 -->
        <el-table
          v-if="overviewView === 'company'"
          v-loading="loading"
          :data="tableItems"
          stripe
          size="small"
          :empty-text="'暂无工作室预算数据'"
        >
          <el-table-column label="工作室" min-width="180">
            <template #default="{ row }">
              <div class="scope-cell">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[row.scope_type] }" />
                <span class="scope-name">{{ displayScopeName(row) }}</span>
              </div>
            </template>
          </el-table-column>
          <!-- 已分配额度 -->
          <el-table-column label="已分配额度" min-width="120" align="right">
            <template #default="{ row }">
              <span>{{ displayAmount(row.amount_usd) }}</span>
            </template>
          </el-table-column>
          <!-- 已使用 + 用量占比 -->
          <el-table-column label="已使用" min-width="160">
            <template #default="{ row }">
              <div class="usage-cell">
                <div style="display:flex;justify-content:space-between;align-items:baseline">
                  <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
                  <span class="muted" style="font-size:11px">
                    {{ row.amount_usd > 0
                      ? Math.round(((row.used_usd || 0) / row.amount_usd) * 100)
                      : 0 }}%
              </span>
            </div>
                <el-progress
                  :percentage="row.amount_usd > 0
                    ? Math.min(Math.round(((row.used_usd || 0) / row.amount_usd) * 100), 100)
                    : 0"
                  :stroke-width="5"
                  :color="row.amount_usd > 0 && (row.used_usd || 0) / row.amount_usd >= 0.95
                    ? '#e6a23c'
                    : 'var(--el-color-primary-light-3)'"
                  :show-text="false"
                />
          </div>
            </template>
          </el-table-column>
          <!-- 日限 / 周限 -->
          <el-table-column label="日限 / 周限" min-width="120" align="center">
            <template #default="{ row }">
              <span v-if="row.daily_amount_usd > 0 || row.weekly_amount_usd > 0" class="limit-text">
                {{ displayAmount(row.daily_amount_usd) }} / {{ displayAmount(row.weekly_amount_usd) }}
              </span>
              <span v-else class="muted">无限额</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="152" align="center">
            <template #default="{ row }">
              <el-button size="small" type="primary" plain @click="openStudioDrawer(row)">查看部门</el-button>
              <el-button size="small" @click="openBudgetDrawer({ scope_type: 'studio', scope_id: row.scope_id, label: row.label })">配置</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- studio_admin 视图：按部门分组，每行含日常 + 服务池 -->
        <div v-if="overviewView === 'studio'" class="permission-banner margin-banner">
          <span class="role-badge" style="background:var(--el-color-primary-light-8);color:var(--el-color-primary);font-size:10px;height:20px;padding:0 7px;border-radius:10px;display:inline-flex;align-items:center;flex-shrink:0">
            平台管理员
          </span>
          <strong>当前拥有全部预算配置权限</strong>
          <span>可调整当前层级，并继续向下分配。</span>
        </div>
        <el-table
          v-else
          ref="studioDeptTableRef"
          v-loading="loading"
          :data="studioDeptRowsFiltered"
          stripe
          size="small"
          :empty-text="'暂无部门预算数据'"
          :row-class-name="deptRowClassName"
          @selection-change="onStudioDeptSelectionChange"
        >
          <el-table-column type="selection" width="42" />

          <!-- 部门名 + 整体状态 -->
          <el-table-column label="部门" min-width="160">
            <template #default="{ row }">
              <div class="scope-cell">
                <span class="risk-indicator" :class="`risk-${budgetColorLevel(row.maxRatio)}`" />
                <span class="scope-name">{{ row.label || row.deptId }}</span>
              </div>
            </template>
          </el-table-column>

          <!-- 日常预算池 -->
          <el-table-column label="日常预算" min-width="180">
            <template #default="{ row }">
              <template v-if="row.daily">
                <div class="pool-inline-cell">
                  <div class="pool-inline-usage">
                    <span>{{ formatUSD(row.daily.used_usd || 0, { compact: true }) }}</span>
                    <span class="muted">/</span>
                    <span>{{ displayAmount(row.daily.amount_usd) }}</span>
                    <el-tag :type="budgetColorLevel(row.daily.used_ratio || 0)" size="small" style="margin-left:4px">
                      {{ statusText(row.daily.used_ratio || 0) }}
                    </el-tag>
                  </div>
                  <el-progress
                    :percentage="Math.min(Math.round((row.daily.used_ratio || 0) * 100), 100)"
                    :stroke-width="4"
                    :color="riskColor(row.daily.used_ratio || 0)"
                    :show-text="false"
                    style="margin-top:4px"
                  />
                  <div class="pool-inline-limits">
                    日 {{ displayAmount(row.daily.daily_amount_usd) }} · 周 {{ displayAmount(row.daily.weekly_amount_usd) }}
                  </div>
                </div>
              </template>
              <span v-else class="muted">未配置</span>
            </template>
          </el-table-column>

          <!-- 服务预算池 -->
          <el-table-column label="服务预算" min-width="180">
            <template #default="{ row }">
              <template v-if="row.service">
                <div class="pool-inline-cell">
                  <div class="pool-inline-usage">
                    <span>{{ formatUSD(row.service.used_usd || 0, { compact: true }) }}</span>
                    <span class="muted">/</span>
                    <span>{{ displayAmount(row.service.amount_usd) }}</span>
                    <el-tag :type="budgetColorLevel(row.service.used_ratio || 0)" size="small" style="margin-left:4px">
                      {{ statusText(row.service.used_ratio || 0) }}
                    </el-tag>
                  </div>
                  <el-progress
                    :percentage="Math.min(Math.round((row.service.used_ratio || 0) * 100), 100)"
                    :stroke-width="4"
                    :color="riskColor(row.service.used_ratio || 0)"
                    :show-text="false"
                    style="margin-top:4px"
                  />
                  <div class="pool-inline-limits">
                    日 {{ displayAmount(row.service.daily_amount_usd) }} · 周 {{ displayAmount(row.service.weekly_amount_usd) }}
                  </div>
                </div>
              </template>
              <span v-else class="muted">未配置</span>
            </template>
          </el-table-column>

          <!-- 操作 -->
          <el-table-column label="操作" width="152" align="center">
            <template #default="{ row }">
              <el-button size="small" type="primary" plain @click="openDeptMemberDrawer(row)">查看成员</el-button>
              <el-button size="small" @click="openBudgetDrawer({ scope_type: 'dept', scope_id: row.deptId, label: row.label })">配置</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- 批量选择栏 -->
        <div v-if="studioDeptSelected.length > 0" class="selection-bar show">
          <strong>已选择 {{ studioDeptSelected.length }} 项</strong>
          <span class="grow" />
          <el-button size="small" @click="clearStudioDeptSelection">取消选择</el-button>
        </div>
      </div>

      <!-- 右侧边栏：治理链路 + 快速跳转 -->
      <aside class="panel side-panel">
        <!-- 治理链路：当前层 + 下发汇总 -->
        <div v-if="chainCurrentNode" class="context-section">
          <div class="panel-head compact">
            <h2>治理链路</h2>
          </div>
          <div class="chain-ladder">
            <!-- 当前节点卡片 -->
            <div class="chain-node-card chain-node--current">
              <div class="chain-node-header">
                <span
                  class="scope-dot"
                  :style="{ background: SCOPE_TYPE_COLORS[chainCurrentNode.scope_type] }"
                />
                <span class="chain-node-name">{{ chainCurrentNode.label }}</span>
                <span class="chain-current-tag">当前</span>
              </div>
              <div class="chain-node-amount">{{ displayAmount(chainCurrentNode.amount_usd) }}</div>
            </div>
            <!-- 向下分配汇总（一行，不随子项数增长） -->
            <template v-if="chainDistribution">
              <div class="chain-connector">
                <span class="chain-connector-line" />
                <span class="chain-connector-arrow">↓</span>
                <span class="chain-connector-line" />
              </div>
              <div class="chain-dist-summary">
                <div class="chain-dist-header">
                  <div class="chain-dist-label">下发至 {{ chainDistribution.childLabel }}</div>
                  <button class="chain-detail-btn" @click="showChainDetail = true">明细 →</button>
                </div>
                <div class="chain-dist-row">
                  <span class="chain-dist-amount">{{ displayAmount(chainDistribution.allocated) }}</span>
                  <span class="chain-dist-pct">占 {{ chainDistribution.pct }}%</span>
                </div>
                <el-progress
                  :percentage="Math.min(chainDistribution.pct, 100)"
                  :stroke-width="4"
                  :color="chainDistribution.pct >= 95 ? 'var(--el-color-warning)' : 'var(--el-color-primary)'"
                  :show-text="false"
                  style="margin-top: 6px"
                />
              </div>
            </template>
            <!-- 尚未分配任何子节点时的提示 -->
            <div v-else-if="chainCurrentNode.amount_usd > 0" class="chain-dist-empty">
              暂未向下分配预算
            </div>
          </div>
          <div class="divider" />
        </div>

        <!-- 快速跳转 -->
        <div class="quick-links">
          <h3>快速跳转</h3>
          <button v-if="auth.effectiveRole === 'admin'" class="quick-link-btn" @click="router.push('/admin/org')">
            <span>进入组织治理</span>
            <span class="quick-link-arrow">→</span>
          </button>
          <button v-else class="quick-link-btn" @click="goOrg()">
            <span>配置工作室 / 部门预算</span>
            <span class="quick-link-arrow">→</span>
          </button>
          <button class="quick-link-btn" @click="goExpansion">
            <span>{{ auth.effectiveRole === 'admin' ? '处理平台扩容申请' : '处理扩容申请' }}</span>
            <span class="quick-link-arrow">→</span>
            </button>
          </div>
      </aside>
    </section>

    <!-- ══════════════════════════════════════════════
         dept_admin：全宽纵向布局
         ══════════════════════════════════════════════ -->
    <template v-else>
      <!-- 多部门时展示各部门明细条（单部门已在 KPI 卡中体现，无需重复） -->
      <div v-if="managedDeptIds.length > 1 && deptContainerItems.length" class="dept-summary-bar">
        <div
          v-for="d in deptContainerItems"
          :key="d.scope_id + d.scope_type"
          class="dept-summary-item"
        >
          <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[d.scope_type] }" />
          <span class="dept-summary-label">{{ displayScopeName(d) }}</span>
          <span class="dept-summary-type-tag" :style="{ color: SCOPE_TYPE_COLORS[d.scope_type] }">{{ SCOPE_TYPE_LABELS[d.scope_type] }}</span>
          <el-progress
            style="width: 90px"
            :percentage="Math.min(Math.round((d.used_ratio || 0) * 100), 100)"
            :stroke-width="5"
            :color="riskColor(d.used_ratio || 0)"
            :show-text="false"
          />
          <span class="dept-summary-value">
            {{ formatUSD(d.used_usd || 0, { compact: true }) }} / {{ displayAmount(d.amount_usd) }}
          </span>
          <el-tag :type="budgetColorLevel(d.used_ratio || 0)" size="small">{{ statusText(d.used_ratio || 0) }}</el-tag>
        </div>
      </div>

      <!-- 多部门切换器：仅在管辖 2 个以上部门时显示 -->
      <div v-if="managedDeptIds.length > 1" class="dept-switcher">
        <span class="dept-switcher-label">查看部门：</span>
        <el-segmented
          v-model="selectedDeptFilter"
          :options="deptFilterOptions"
          size="small"
        />
        <span v-if="selectedDeptFilter" class="dept-switcher-hint">
          仅显示「{{ deptFilterOptions.find(o => o.value === selectedDeptFilter)?.label }}」的成员
        </span>
    </div>

      <!-- 预警区：riskMembers > 0 时展示，否则健康提示 -->
      <div v-if="riskMembers.length > 0" class="panel alert-panel">
        <div class="alert-panel-head">
          <div class="alert-title">
            <span class="alert-badge">{{ riskMembers.length }}</span>
            <span>名成员需要关注</span>
            <span v-if="selectedDeptFilter" class="alert-sub">
              {{ deptFilterOptions.find(o => o.value === selectedDeptFilter)?.label }} ·
            </span>
            <span class="alert-sub">月额度 ≥ 80% 或日均消耗接近日限</span>
          </div>
          <el-button size="small" @click="goExpansion">查看扩容申请</el-button>
        </div>
        <el-table :data="riskMembers" size="small" stripe>
          <el-table-column label="成员" min-width="160">
            <template #default="{ row }">
              <div class="scope-cell">
                <span class="risk-indicator" :class="`risk-${row.riskLevel}`" />
                <span class="scope-name">{{ row.label || row.scope_id }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="月额度进度" min-width="180">
            <template #default="{ row }">
              <div class="usage-cell">
                <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }} / {{ displayAmount(row.amount_usd) }}</span>
                <el-progress
                  :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)"
                  :stroke-width="5"
                  :color="riskColor(row.used_ratio || 0)"
                />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="近7天消耗" width="120" align="right">
            <template #default="{ row }">
              <span class="tabular">{{ formatUSD(row.cost7d, { compact: true }) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="7天日均 / 日限" min-width="160">
            <template #default="{ row }">
              <template v-if="row.daily_amount_usd > 0">
                <div class="usage-cell">
                  <span class="limit-text">{{ formatUSD(row.dailyAvg7d, { compact: true }) }} / {{ displayAmount(row.daily_amount_usd) }}</span>
                  <el-progress
                    :percentage="Math.min(Math.round(row.dailyRisk * 100), 100)"
                    :stroke-width="5"
                    :color="riskColor(row.dailyRisk)"
                  />
                </div>
              </template>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150" align="center">
            <template #default="{ row }">
              <el-button size="small" plain @click="openMemberDetail(row)">预算详情</el-button>
              <el-button size="small" type="primary" plain @click="openMemberQuota(row)">调整配额</el-button>
            </template>
          </el-table-column>
        </el-table>
    </div>

      <div v-else-if="!loading" class="panel healthy-panel">
        <div class="healthy-state">
          <div class="healthy-icon">✓</div>
          <div>
            <strong>
              {{ selectedDeptFilter
                ? (deptFilterOptions.find(o => o.value === selectedDeptFilter)?.label + ' 暂无预算风险')
                : '本部门成员暂无预算风险' }}
            </strong>
            <span>所有成员月额度使用率低于 80%，日均消耗在安全范围内。</span>
          </div>
          </div>
          </div>

      <!-- 全部成员表格 -->
      <div class="panel members-panel">
        <div class="panel-head">
          <div>
            <h2>
              {{ selectedDeptFilter
                ? (deptFilterOptions.find(o => o.value === selectedDeptFilter)?.label + ' · 成员配额')
                : '全部成员配额' }}
            </h2>
            <p>
              {{ enrichedMembers.length }} 人
              <template v-if="selectedDeptFilter">（已过滤，共 {{ memberTableItems.length }} 人）</template>
              · 按近7天消耗降序
            </p>
        </div>
          <el-button size="small" @click="openBudgetDrawer({ scope_type: 'dept', scope_id: selectedDeptFilter || currentDeptId || '', label: selectedDeptFilter || currentDeptId || '' })">
            配置配额
          </el-button>
        </div>
        <el-table
          v-loading="loading"
          :data="enrichedMembers"
          :row-class-name="memberRiskRowClass"
          stripe
          size="small"
          :empty-text="'暂无成员配额数据，请前往部门节点配置'"
        >
          <el-table-column label="成员" min-width="160">
            <template #default="{ row }">
              <div class="scope-cell">
                <span class="risk-indicator" :class="`risk-${row.riskLevel}`" />
                <div>
                  <div class="scope-name">{{ row.label || row.scope_id }}</div>
                  <!-- 多部门全量视图时展示所属部门 -->
                  <div v-if="managedDeptIds.length > 1 && !selectedDeptFilter && row.parent_scope_id" class="member-dept-tag">
                    {{ row.parent_scope_id }}
      </div>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="月配额" width="90" align="right">
            <template #default="{ row }">
              <span class="tabular">{{ displayAmount(row.amount_usd) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="月额度进度" min-width="160">
            <template #default="{ row }">
              <div class="usage-cell">
                <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }} / {{ displayAmount(row.amount_usd) }}</span>
                <el-progress
                  :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)"
                  :stroke-width="5"
                  :color="riskColor(row.used_ratio || 0)"
                />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="近7天消耗" width="100" align="right">
            <template #default="{ row }">
              <span class="tabular" :class="{ 'text-muted': row.cost7d === 0 }">
                {{ row.cost7d > 0 ? formatUSD(row.cost7d, { compact: true }) : '—' }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="7天日均 / 日限" min-width="140">
            <template #default="{ row }">
              <template v-if="row.daily_amount_usd > 0">
                <div class="usage-cell">
                  <span class="limit-text">{{ formatUSD(row.dailyAvg7d, { compact: true }) }} / {{ displayAmount(row.daily_amount_usd) }}</span>
                  <el-progress
                    :percentage="Math.min(Math.round(row.dailyRisk * 100), 100)"
                    :stroke-width="5"
                    :color="riskColor(row.dailyRisk)"
                  />
                </div>
              </template>
              <span v-else class="muted">无日限</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150" align="center">
            <template #default="{ row }">
              <el-button size="small" plain @click="openMemberDetail(row)">预算详情</el-button>
              <el-button size="small" type="primary" plain @click="openMemberQuota(row)">调整配额</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
  </div>

  <!-- ── 下钻侧抽屉：工作室 → 部门 → 成员（admin 专用）──────────────────── -->
  <el-drawer
    v-model="drawerVisible"
    direction="rtl"
    size="480px"
    :destroy-on-close="false"
    class="drill-drawer"
  >
    <template #header>
      <div class="drawer-header">
        <div class="drawer-header-info">
          <span class="drawer-header-title">{{ drawerStudio?.label ?? '工作室' }}</span>
          <span class="drawer-header-sub">部门预算总览</span>
        </div>
        <el-button
          size="small"
          type="primary"
          plain
          @click="goOrgFromDrawer('studio', drawerStudio!.scope_id, drawerStudio?.label)"
        >
          配置预算 →
        </el-button>
      </div>
    </template>

    <!-- 加载中 -->
    <div v-if="drawerLoading" v-loading="true" style="height:200px" />

    <!-- 部门列表 -->
    <div v-else class="drawer-dept-list">
      <div v-if="!drawerDepts.length" class="drawer-empty">该工作室暂无部门预算数据</div>

      <div
        v-for="dept in drawerDepts"
        :key="dept.deptId"
        class="drawer-dept-item"
      >
        <!-- 部门行头 -->
        <div class="drawer-dept-head" @click="toggleDeptMembers(dept)">
          <div class="drawer-dept-left">
            <span
              class="risk-indicator"
              :class="`risk-${budgetColorLevel(Math.max(dept.daily?.used_ratio || 0, dept.service?.used_ratio || 0))}`"
            />
            <span class="drawer-dept-name">{{ dept.label }}</span>
          </div>
          <div class="drawer-dept-right">
            <span class="drawer-dept-total">
              {{ displayAmount((dept.daily?.amount_usd || 0) + (dept.service?.amount_usd || 0)) }}
            </span>
            <el-button
              link
              size="small"
              @click.stop="goOrgFromDrawer('dept', dept.deptId, dept.label)"
            >
              配置
            </el-button>
            <span class="drawer-expand-icon">{{ dept.expanded ? '▲' : '▼' }}</span>
          </div>
        </div>

        <!-- 池详情（常显） -->
        <div class="drawer-dept-pools">
          <div v-if="dept.daily" class="drawer-pool-row">
            <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_daily'] }" />
            <span class="drawer-pool-label">日常池</span>
            <span class="drawer-pool-amount">{{ displayAmount(dept.daily.amount_usd) }}</span>
            <el-progress
              style="flex:1; min-width:60px"
              :percentage="Math.min(Math.round((dept.daily.used_ratio || 0) * 100), 100)"
              :stroke-width="4"
              :color="riskColor(dept.daily.used_ratio || 0)"
              :show-text="false"
            />
            <el-tag :type="budgetColorLevel(dept.daily.used_ratio || 0)" size="small">
              {{ statusText(dept.daily.used_ratio || 0) }}
            </el-tag>
          </div>
          <div v-if="dept.service" class="drawer-pool-row">
            <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_service'] }" />
            <span class="drawer-pool-label">服务池</span>
            <span class="drawer-pool-amount">{{ displayAmount(dept.service.amount_usd) }}</span>
            <el-progress
              style="flex:1; min-width:60px"
              :percentage="Math.min(Math.round((dept.service.used_ratio || 0) * 100), 100)"
              :stroke-width="4"
              :color="riskColor(dept.service.used_ratio || 0)"
              :show-text="false"
            />
            <el-tag :type="budgetColorLevel(dept.service.used_ratio || 0)" size="small">
              {{ statusText(dept.service.used_ratio || 0) }}
            </el-tag>
          </div>
        </div>

        <!-- 成员展开区 -->
        <div v-if="dept.expanded" class="drawer-members">
          <div v-if="dept.membersLoading" v-loading="true" style="height:60px" />
          <template v-else>
            <div v-if="!dept.members.length" class="drawer-empty-sm">暂无单独配额成员</div>
            <div
              v-for="member in dept.members"
              :key="member.scope_id"
              class="drawer-member-row"
            >
              <span class="drawer-member-name">{{ member.label || member.scope_id }}</span>
              <span class="drawer-member-amount">{{ displayAmount(member.amount_usd) }}</span>
              <el-progress
                style="flex:1; min-width:50px; max-width:90px"
                :percentage="Math.min(Math.round((member.used_ratio || 0) * 100), 100)"
                :stroke-width="3"
                :color="riskColor(member.used_ratio || 0)"
                :show-text="false"
              />
            </div>
          </template>
          <div class="drawer-member-footer">
            <el-button
              size="small"
              link
              type="primary"
              @click="goOrgFromDrawer('dept', dept.deptId, dept.label)"
            >
              调整成员配额 →
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </el-drawer>

  <!-- ── 成员配额抽屉（studio_admin 专用）──────────────────────────────────── -->
  <el-drawer
    v-model="deptMemberDrawerVisible"
    direction="rtl"
    size="440px"
    :destroy-on-close="false"
    class="drill-drawer"
  >
    <template #header>
      <div class="drawer-header">
        <div class="drawer-header-info">
          <span class="drawer-header-title">{{ deptMemberDrawerDept?.label ?? '部门' }}</span>
          <span class="drawer-header-sub">成员配额总览</span>
        </div>
        <el-button
          v-if="deptMemberDrawerDept"
          size="small"
          type="primary"
          plain
          @click="goOrgFromDeptDrawer('dept', deptMemberDrawerDept.deptId, deptMemberDrawerDept.label)"
        >
          配置预算 →
        </el-button>
      </div>
    </template>

    <!-- 部门池摘要 -->
    <div v-if="deptMemberDrawerDept" class="drawer-dept-summary">
      <div v-if="deptMemberDrawerDept.daily" class="drawer-pool-row">
        <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_daily'] }" />
        <span class="drawer-pool-label">日常池</span>
        <span class="drawer-pool-amount">{{ displayAmount(deptMemberDrawerDept.daily.amount_usd) }}</span>
        <el-progress
          style="flex:1; min-width:60px"
          :percentage="Math.min(Math.round((deptMemberDrawerDept.daily.used_ratio || 0) * 100), 100)"
          :stroke-width="4"
          :color="riskColor(deptMemberDrawerDept.daily.used_ratio || 0)"
          :show-text="false"
        />
        <el-tag :type="budgetColorLevel(deptMemberDrawerDept.daily.used_ratio || 0)" size="small">
          {{ statusText(deptMemberDrawerDept.daily.used_ratio || 0) }}
        </el-tag>
      </div>
      <div v-if="deptMemberDrawerDept.service" class="drawer-pool-row">
        <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS['dept_service'] }" />
        <span class="drawer-pool-label">服务池</span>
        <span class="drawer-pool-amount">{{ displayAmount(deptMemberDrawerDept.service.amount_usd) }}</span>
        <el-progress
          style="flex:1; min-width:60px"
          :percentage="Math.min(Math.round((deptMemberDrawerDept.service.used_ratio || 0) * 100), 100)"
          :stroke-width="4"
          :color="riskColor(deptMemberDrawerDept.service.used_ratio || 0)"
          :show-text="false"
        />
        <el-tag :type="budgetColorLevel(deptMemberDrawerDept.service.used_ratio || 0)" size="small">
          {{ statusText(deptMemberDrawerDept.service.used_ratio || 0) }}
        </el-tag>
      </div>
    </div>

    <div class="drawer-section-title">
      单独配额成员
      <span class="drawer-section-count">{{ deptMemberDrawerLoading ? '…' : deptMemberDrawerMembers.length }}</span>
    </div>

    <div v-if="deptMemberDrawerLoading" v-loading="true" style="height:120px" />

    <div v-else class="drawer-dept-list">
      <div v-if="!deptMemberDrawerMembers.length" class="drawer-empty">
        该部门暂无单独配额成员，成员共享部门预算池
      </div>
      <div
        v-for="member in deptMemberDrawerMembers"
        :key="member.scope_id"
        class="drawer-member-card"
      >
        <div class="drawer-member-card-head">
          <span class="drawer-member-name">{{ member.label || member.scope_id }}</span>
          <span class="drawer-member-amount">{{ displayAmount(member.amount_usd) }}</span>
        </div>
        <el-progress
          :percentage="Math.min(Math.round((member.used_ratio || 0) * 100), 100)"
          :stroke-width="5"
          :color="riskColor(member.used_ratio || 0)"
          :show-text="false"
          style="margin-top:4px"
        />
        <div class="drawer-member-card-meta">
          <el-tag :type="budgetColorLevel(member.used_ratio || 0)" size="small">
            {{ statusText(member.used_ratio || 0) }}
          </el-tag>
          <span v-if="member.daily_amount_usd > 0" class="drawer-member-limits">
            日限 {{ displayAmount(member.daily_amount_usd) }}
          </span>
        </div>
      </div>
    </div>
  </el-drawer>

  <!-- ── 分配明细弹窗 ──────────────────────────────────────────────────────── -->
  <el-dialog
    v-model="showChainDetail"
    :title="`预算分配明细 · ${chainCurrentNode?.label ?? ''}`"
    width="560px"
    destroy-on-close
  >
    <div class="chain-detail-dialog">
      <!-- 汇总行 -->
      <div class="chain-detail-summary-row" v-if="chainDistribution">
        <div class="chain-detail-summary-item">
          <span class="chain-detail-summary-label">总额度</span>
          <span class="chain-detail-summary-value">{{ displayAmount(chainCurrentNode?.amount_usd ?? 0) }}</span>
        </div>
        <div class="chain-detail-summary-item">
          <span class="chain-detail-summary-label">已下发</span>
          <span class="chain-detail-summary-value">{{ displayAmount(chainDistribution.allocated) }}</span>
        </div>
        <div class="chain-detail-summary-item">
          <span class="chain-detail-summary-label">未下发</span>
          <span class="chain-detail-summary-value">
            {{ displayAmount(Math.max((chainCurrentNode?.amount_usd ?? 0) - chainDistribution.allocated, 0)) }}
          </span>
        </div>
        <div class="chain-detail-summary-item">
          <span class="chain-detail-summary-label">下发率</span>
          <span class="chain-detail-summary-value">{{ chainDistribution.pct }}%</span>
        </div>
      </div>

      <!-- 明细列表 -->
      <div class="chain-detail-rows">
        <div
          v-for="row in chainDetailRows"
          :key="row.id"
          class="chain-detail-item"
        >
          <div class="chain-detail-item-head">
            <span class="chain-detail-item-name">{{ row.name }}</span>
            <span class="chain-detail-item-total">{{ displayAmount(row.total) }}</span>
          </div>
          <!-- admin 视图：单个工作室总额进度 -->
          <template v-if="auth.effectiveRole === 'admin'">
            <el-progress
              :percentage="Math.min(Math.round(row.ratio * 100), 100)"
              :stroke-width="5"
              :show-text="false"
              :color="row.ratio >= 0.9 ? 'var(--el-color-warning)' : 'var(--el-color-primary-light-3)'"
            />
            <span class="chain-detail-item-pct">占总额 {{ Math.round(row.ratio * 100) }}%</span>
          </template>
          <!-- studio_admin 视图：日常池 + 服务池并排 -->
          <template v-else>
            <div class="chain-detail-pool-row">
              <div class="chain-detail-pool">
                <span class="chain-detail-pool-label">日常池</span>
                <span class="chain-detail-pool-val">{{ displayAmount(row.daily) }}</span>
              </div>
              <div class="chain-detail-pool-sep">+</div>
              <div class="chain-detail-pool">
                <span class="chain-detail-pool-label">服务池</span>
                <span class="chain-detail-pool-val">{{ displayAmount(row.service) }}</span>
              </div>
              <div class="chain-detail-pool-sep chain-detail-pool-total-sep">=</div>
              <div class="chain-detail-pool">
                <span class="chain-detail-pool-label">占总额</span>
                <span class="chain-detail-pool-val chain-detail-pool-pct">{{ Math.round(row.ratio * 100) }}%</span>
              </div>
            </div>
            <el-progress
              :percentage="Math.min(Math.round(row.ratio * 100), 100)"
              :stroke-width="5"
              :show-text="false"
              :color="row.ratio >= 0.9 ? 'var(--el-color-warning)' : 'var(--el-color-primary-light-3)'"
            />
          </template>
        </div>
        <div v-if="!chainDetailRows.length" class="chain-detail-empty">暂无分配数据</div>
      </div>
    </div>
  </el-dialog>

  <!-- 公司预算池设置 dialog -->
  <el-dialog v-model="companyBudgetDialogVisible" title="设置公司预算池" width="420px" :close-on-click-modal="false">
    <el-form label-width="90px" style="margin-top:4px">
      <el-form-item label="月度总额">
        <el-input-number v-model="companyBudgetForm.amount" :min="-1" :precision="2" controls-position="right" style="width:100%" />
        <div style="font-size:11px;color:var(--el-text-color-placeholder);margin-top:3px">-1 表示不限额</div>
      </el-form-item>
      <el-form-item label="周限额">
        <el-input-number v-model="companyBudgetForm.weekly" :min="-1" :precision="2" controls-position="right" style="width:100%" />
        <div style="font-size:11px;color:var(--el-text-color-placeholder);margin-top:3px">-1 表示不限</div>
      </el-form-item>
      <el-form-item label="日限额">
        <el-input-number v-model="companyBudgetForm.daily" :min="-1" :precision="2" controls-position="right" style="width:100%" />
        <div style="font-size:11px;color:var(--el-text-color-placeholder);margin-top:3px">-1 表示不限</div>
      </el-form-item>
    </el-form>
      <template #footer>
      <el-button @click="companyBudgetDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="companyBudgetSaving" @click="saveCompanyBudget">保存</el-button>
      </template>
    </el-dialog>

  <!-- ── 预算配置抽屉（BudgetScopePanel 内联，替换跳转 OrgTreePage）── -->
  <BudgetEditDrawer
    v-model="budgetDrawerVisible"
    :scope-type="budgetDrawerScopeType"
    :scope-id="budgetDrawerScopeId"
    :title="budgetDrawerTitle"
    :child-scope-ids="budgetDrawerChildIds"
    :can-edit="budgetDrawerCanEdit"
    :can-edit-self="budgetDrawerCanEditSelf"
  />

  <!-- ── 成员配额快速编辑弹窗（dept_admin 专用）── -->
  <MemberQuotaDialog
    v-model="memberQuotaVisible"
    :member="memberQuotaTarget"
    :can-edit="true"
    @refresh="load"
  />

  <!-- ── 成员预算详情弹窗（dept_admin 专用：预算限制 + 等级子预算）── -->
  <el-dialog
    v-model="memberDetailVisible"
    title="个人预算限制"
    width="640px"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <template v-if="memberDetailTarget">
      <div class="member-detail-summary">
        <div class="member-detail-summary-info">
          <div class="member-detail-summary-label">成员独立额度</div>
          <div class="member-detail-summary-name">{{ memberDetailTarget.label || memberDetailTarget.scope_id }}</div>
          <div class="member-detail-summary-meta">独立额度仍受公司、工作室和部门上级预算共同约束。</div>
        </div>
        <div class="member-detail-summary-amount">
          <strong>{{ formatUSD(memberDetailTarget.used_usd || 0) }}</strong>
          <span> / {{ displayAmount(memberDetailTarget.amount_usd) }}</span>
          <el-progress
            :percentage="Math.min(Math.round((memberDetailTarget.used_ratio || 0) * 100), 100)"
            :stroke-width="5"
            :color="riskColor(memberDetailTarget.used_ratio || 0)"
            :show-text="false"
            style="margin-top:6px"
          />
        </div>
      </div>
      <div class="member-detail-panel">
        <div class="member-detail-panel-head">
          <div>
            <h3>个人预算限制</h3>
            <p>总预算与等级子预算均为月度硬限制。</p>
          </div>
          <el-button size="small" type="primary" @click="openMemberQuota(memberDetailTarget)">编辑配置</el-button>
        </div>
        <el-table :data="memberTierRows" size="small" stripe>
          <el-table-column label="限制项" min-width="160">
            <template #default="{ row }">
              <span v-if="row.isTotal" style="font-weight:700">总预算</span>
              <span v-else class="tier-limit-name">
                <span class="tier-dot" :style="{ background: row.color }" />
                {{ row.name }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="额度" width="120" align="right">
            <template #default="{ row }">
              <span v-if="row.limit === null" class="muted">未配置</span>
              <span v-else>{{ formatUSD(row.limit, { compact: true }) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="已使用" width="120" align="right">
            <template #default="{ row }">
              <span>{{ formatUSD(row.used, { compact: true }) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="使用率" min-width="140">
            <template #default="{ row }">
              <template v-if="row.limit !== null && row.limit > 0">
                <el-progress
                  :percentage="Math.min(Math.round(row.ratio * 100), 100)"
                  :stroke-width="5"
                  :color="riskColor(row.ratio)"
                />
              </template>
              <span v-else-if="row.limit === null" class="muted">仅受总预算</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.limit === null" type="info" size="small">无限制</el-tag>
              <el-tag v-else-if="row.limit === 0" type="danger" size="small">已禁用</el-tag>
              <el-tag v-else :type="row.ratio >= 1 ? 'danger' : row.ratio >= 0.8 ? 'warning' : 'success'" size="small">
                {{ row.ratio >= 1 ? '已超额' : row.ratio >= 0.8 ? '预警' : '正常' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
    <template #footer>
      <el-button @click="memberDetailVisible = false">关闭</el-button>
      <el-button v-if="memberDetailTarget" type="primary" @click="openMemberQuota(memberDetailTarget); memberDetailVisible = false">调整成员额度</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getBudgetContext, getBudgetList, rebuildBudgetCounters, setBudget } from '@/services/control-plane'
import http from '@/services/http'
import { useAuthStore } from '@/stores/auth'
import type { BudgetContextItem, BudgetItem, BudgetScopeType, BudgetView } from '@/types/budget'
import { SCOPE_TYPE_COLORS, SCOPE_TYPE_LABELS } from '@/types/budget'
import { budgetColorLevel, formatUSD } from '@/utils/format'
import BudgetEditDrawer from '@/components/budget/BudgetEditDrawer.vue'
import MemberQuotaDialog from '@/components/budget/MemberQuotaDialog.vue'

// ─── 类型扩展：enrichedMember = BudgetItem + 7天用量字段 ──────────────────────
interface EnrichedMember extends BudgetItem {
  cost7d: number       // 近7天总消耗（$）
  dailyAvg7d: number   // 7天日均消耗（$）
  dailyRisk: number    // 日均 / 日限（0 表示无限制）
  riskLevel: 'success' | 'warning' | 'danger'
}

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const items = ref<BudgetItem[]>([])
const contextItems = ref<BudgetContextItem[]>([])
const memberUsage7d = ref<Map<string, number>>(new Map())

// ─── 角色辅助 ─────────────────────────────────────────────────────────────────
const isDeptAdmin = computed(() => auth.effectiveRole === 'dept_admin')

// dept_admin 管辖的所有部门 ID 列表（多部门场景）
const managedDeptIds = computed<string[]>(() => {
  if (!isDeptAdmin.value) return []
  return (auth.orgRoles ?? [])
    .filter(r => r.role === 'dept_admin' || r.scope_type === 'dept')
    .map(r => r.scope_id)
    .filter(Boolean)
})

// 第一个部门 ID（向后兼容单部门场景）
const currentDeptId = computed<string | null>(() => managedDeptIds.value[0] ?? null)

// ─── 治理链路 ─────────────────────────────────────────────────────────────────
// ─── 治理链路：当前节点 + 向下分配摘要 ──────────────────────────────────────
// 找出当前角色管辖的那个 context 节点（admin→company, studio_admin→studio）
// studio_admin 不显示公司节点，以免泄露公司总额
const chainCurrentNode = computed(() => {
  const raw = contextItems.value
  if (auth.effectiveRole === 'admin') {
    return raw.find(i => i.scope_type === 'company') ?? null
  }
  if (auth.effectiveRole === 'studio_admin') {
    const ctx = raw.find(i => i.scope_type === 'studio') ?? null
    if (!ctx) return null
    // 用 items 中对应条目的真实名称替换硬编码的 "工作室"
    const found = items.value.find(i => i.scope_type === 'studio' && i.scope_id === ctx.scope_id)
    return { ...ctx, label: found?.label || ctx.label }
  }
  return null
})

// 向下分配摘要：聚合子节点，无论子节点有多少都只显示一行
const chainDistribution = computed<{ childLabel: string; allocated: number; pct: number } | null>(
  () => {
    const top = chainCurrentNode.value
    if (!top || top.amount_usd <= 0) return null

    if (auth.effectiveRole === 'admin') {
      const studios = items.value.filter(i => i.scope_type === 'studio')
      if (!studios.length) return null
      const allocated = studios.reduce((s, i) => s + Math.max(i.amount_usd || 0, 0), 0)
      return {
        childLabel: `${studios.length} 个工作室`,
        allocated,
        pct: Math.round((allocated / top.amount_usd) * 100),
      }
    }
    if (auth.effectiveRole === 'studio_admin') {
      const depts = items.value.filter(
        i => i.scope_type === 'dept_daily' || i.scope_type === 'dept_service',
      )
      const uniqueCount = new Set(depts.map(i => i.scope_id)).size
      if (!uniqueCount) return null
      const allocated = depts.reduce((s, i) => s + Math.max(i.amount_usd || 0, 0), 0)
      return {
        childLabel: `${uniqueCount} 个部门`,
        allocated,
        pct: Math.round((allocated / top.amount_usd) * 100),
      }
    }
    return null
  },
)

// 兼容旧模板引用（暂保留，不对外使用）
const visibleContextItems = computed(() => (chainCurrentNode.value ? [chainCurrentNode.value] : []))
const enrichedContextItems = visibleContextItems

// 分配明细弹窗
const showChainDetail = ref(false)
interface ChainDetailRow {
  id: string
  name: string
  daily: number
  service: number
  total: number
  ratio: number // total / parent.amount_usd
}
const chainDetailRows = computed<ChainDetailRow[]>(() => {
  const top = chainCurrentNode.value
  if (!top || top.amount_usd <= 0) return []

  if (auth.effectiveRole === 'admin') {
    return items.value
      .filter(i => i.scope_type === 'studio')
      .map(i => ({
        id: i.scope_id,
        name: i.label || i.scope_id,
        daily: 0,
        service: 0,
        total: Math.max(i.amount_usd || 0, 0),
        ratio: Math.max(i.amount_usd || 0, 0) / top.amount_usd,
      }))
      .sort((a, b) => b.total - a.total)
  }
  if (auth.effectiveRole === 'studio_admin') {
    return studioDeptRows.value.map(r => {
      const daily = Math.max(r.daily?.amount_usd || 0, 0)
      const service = Math.max(r.service?.amount_usd || 0, 0)
      const total = daily + service
      return {
        id: r.deptId,
        name: r.label || r.deptId,
        daily,
        service,
        total,
        ratio: total / top.amount_usd,
      }
    })
  }
  return []
})

// ─── 多部门成员视图切换 ────────────────────────────────────────────────────────
// null = 全部部门，有值 = 按 dept_id 过滤
const selectedDeptFilter = ref<string | null>(null)

// 切换选项：全部 + 每个管辖部门
const deptFilterOptions = computed(() => [
  { label: '全部', value: null as string | null },
  ...deptPoolsByDept.value.map(g => ({ label: g.label || g.deptId, value: g.deptId })),
])

// ─── 风险筛选 ─────────────────────────────────────────────────────────────────
const riskFilter = ref('all')
const riskOptions = [
  { label: '全部风险', value: 'all' },
  { label: '已超额', value: 'danger' },
  { label: '预警', value: 'warning' },
]

// ─── 视图映射 ─────────────────────────────────────────────────────────────────
const overviewView = computed<BudgetView>(() => {
  if (auth.effectiveRole === 'admin') return 'company'
  if (auth.effectiveRole === 'studio_admin') return 'studio'
  return 'department'
})

// ─── KPI 计算 ─────────────────────────────────────────────────────────────────
const kpiItems = computed(() => {
  const rows = items.value
  if (overviewView.value === 'company') {
    const studios = rows.filter(item => item.scope_type === 'studio')
    return studios.length ? studios : rows.filter(item => item.scope_type === 'company')
  }
  if (overviewView.value === 'studio') {
    return rows.filter(item => item.scope_type === 'dept_daily' || item.scope_type === 'dept_service')
  }
  return rows.filter(
    item =>
      item.scope_type === 'dept_daily' ||
      item.scope_type === 'dept_service' ||
      item.scope_type === 'member',
  )
})

const totalBudget = computed(() =>
  kpiItems.value.reduce((sum, item) => sum + Math.max(item.amount_usd || 0, 0), 0),
)
const totalUsed = computed(() =>
  kpiItems.value.reduce((sum, item) => sum + (item.used_usd || 0), 0),
)

// admin 专属：公司总预算池（从 items 中取 company 条目）
const companyPoolItem = computed(() =>
  overviewView.value === 'company'
    ? (items.value.find(i => i.scope_type === 'company') ?? null)
    : null,
)
// 已下发给工作室的总额（kpiItems 是工作室条目之和）
const companyAllocated = computed(() => totalBudget.value)
// 未下发余量
const companyUnallocated = computed(() => {
  if (!companyPoolItem.value) return 0
  return Math.max(companyPoolItem.value.amount_usd - companyAllocated.value, 0)
})
// 下发率
const companyAllocRatio = computed(() => {
  if (!companyPoolItem.value || companyPoolItem.value.amount_usd <= 0) return 0
  return Math.min(companyAllocated.value / companyPoolItem.value.amount_usd, 1)
})

// dept_admin 专属 KPI
const totalMemberBudget = computed(() =>
  memberTableItems.value.reduce((sum, item) => sum + Math.max(item.amount_usd || 0, 0), 0),
)
const totalCost7d = computed(() => {
  let total = 0
  memberUsage7d.value.forEach(v => (total += v))
  return total
})

// studio_admin 专属：部门分组表格（每个部门一行，日常 + 服务池并排）
interface StudioDeptRow {
  deptId: string
  label: string
  daily: BudgetItem | null
  service: BudgetItem | null
  maxRatio: number
}
const studioDeptRows = computed<StudioDeptRow[]>(() => {
  if (overviewView.value !== 'studio') return []
  const map = new Map<string, StudioDeptRow>()
  // 用全量 kpiItems 分组，保证每个部门的两个池都出现
  for (const item of kpiItems.value) {
    const key = item.scope_id
    if (!map.has(key)) {
      map.set(key, { deptId: key, label: item.label || key, daily: null, service: null, maxRatio: 0 })
    }
    const entry = map.get(key)!
    if (item.scope_type === 'dept_daily') entry.daily = item
    if (item.scope_type === 'dept_service') entry.service = item
    entry.maxRatio = Math.max(entry.maxRatio, item.used_ratio || 0)
  }
  return Array.from(map.values()).sort((a, b) => b.maxRatio - a.maxRatio)
})
const studioDeptRowsFiltered = computed<StudioDeptRow[]>(() => {
  if (riskFilter.value === 'danger') return studioDeptRows.value.filter(r => r.maxRatio >= 1)
  if (riskFilter.value === 'warning') return studioDeptRows.value.filter(r => r.maxRatio >= 0.8 && r.maxRatio < 1)
  return studioDeptRows.value
})
// 风险部门数：按部门粒度计（而非按池条目数），避免同一部门被计两次
const riskDeptCount = computed(() =>
  overviewView.value === 'studio'
    ? studioDeptRows.value.filter(r => r.maxRatio >= 0.8).length
    : riskItems.value.length,
)

// studio_admin 专属：工作室池 + 下发统计
const studioPoolItem = computed(() =>
  overviewView.value === 'studio' ? (items.value.find(i => i.scope_type === 'studio') ?? null) : null,
)
// 已下发给各部门的总额（dept_daily + dept_service 之和）
const studioAllocated = computed(() => totalBudget.value)
// 未下发余量（工作室总额 - 已下发）
const studioUnallocated = computed(() => {
  if (!studioPoolItem.value) return 0
  return Math.max(studioPoolItem.value.amount_usd - studioAllocated.value, 0)
})
// 下发率（用于进度条，表达"工作室额度下发了多少给部门"）
const studioAllocRatio = computed(() => {
  if (!studioPoolItem.value || studioPoolItem.value.amount_usd <= 0) return 0
  return Math.min(studioAllocated.value / studioPoolItem.value.amount_usd, 1)
})

// 部门日常池 / 服务池：按部门 ID 分组，用于多部门视图
interface DeptPoolGroup {
  deptId: string
  label: string
  daily: BudgetItem | null
  service: BudgetItem | null
  maxRatio: number  // 取日常/服务中较高的那个，用于整体风险着色
}
const deptPoolsByDept = computed<DeptPoolGroup[]>(() => {
  const map = new Map<string, DeptPoolGroup>()
  for (const item of deptContainerItems.value) {
    // dept_daily/dept_service 的 scope_id 就是 dept_id
    const key = item.scope_id
    if (!map.has(key)) {
      map.set(key, { deptId: key, label: item.label || key, daily: null, service: null, maxRatio: 0 })
    }
    const entry = map.get(key)!
    if (item.scope_type === 'dept_daily') entry.daily = item
    if (item.scope_type === 'dept_service') entry.service = item
    entry.maxRatio = Math.max(entry.maxRatio, item.used_ratio || 0)
  }
  // 按风险降序排列，让高风险部门排在前面
  return Array.from(map.values()).sort((a, b) => b.maxRatio - a.maxRatio)
})

// 单部门聚合 KPI（兼容单部门场景，及多部门聚合视图）
const deptDailyKpi = computed(() => {
  const its = deptContainerItems.value.filter(i => i.scope_type === 'dept_daily')
  const amount = its.reduce((s, i) => s + Math.max(i.amount_usd || 0, 0), 0)
  const used = its.reduce((s, i) => s + (i.used_usd || 0), 0)
  // 风险以最高值计，避免平均掩盖单个部门的高风险
  const maxRatio = its.reduce((m, i) => Math.max(m, i.used_ratio || 0), 0)
  return { amount, used, ratio: maxRatio, count: its.length }
})
const deptServiceKpi = computed(() => {
  const its = deptContainerItems.value.filter(i => i.scope_type === 'dept_service')
  const amount = its.reduce((s, i) => s + Math.max(i.amount_usd || 0, 0), 0)
  const used = its.reduce((s, i) => s + (i.used_usd || 0), 0)
  const maxRatio = its.reduce((m, i) => Math.max(m, i.used_ratio || 0), 0)
  return { amount, used, ratio: maxRatio, count: its.length }
})

// ─── 风险计算（admin/studio_admin 用） ─────────────────────────────────────────
const riskItems = computed(() =>
  kpiItems.value
    .filter(item => (item.amount_usd || 0) > 0 && (item.used_ratio || 0) >= 0.8)
    .sort((a, b) => (b.used_ratio || 0) - (a.used_ratio || 0)),
)
const filteredRiskItems = computed(() => {
  if (riskFilter.value === 'danger') return riskItems.value.filter(i => (i.used_ratio || 0) >= 1)
  if (riskFilter.value === 'warning')
    return riskItems.value.filter(i => (i.used_ratio || 0) >= 0.8 && (i.used_ratio || 0) < 1)
  return riskItems.value
})
const topUsageItems = computed(() =>
  kpiItems.value
    .slice()
    .sort((a, b) => {
      const ratioDiff = (b.used_ratio || 0) - (a.used_ratio || 0)
      return ratioDiff !== 0 ? ratioDiff : (b.used_usd || 0) - (a.used_usd || 0)
    }),
)
// admin 公司视图：展示全量工作室（无截断，方便调整每个工作室配额）
// studio_admin 视图：topUsageItems 仍限 8 条作为 dashboard 摘要
const tableItems = computed(() => {
  if (riskItems.value.length > 0) return filteredRiskItems.value
  if (overviewView.value === 'company') return topUsageItems.value
  return topUsageItems.value.slice(0, 8)
})
const tableTitle = computed(() => {
  if (riskItems.value.length > 0) return '预算风险'
  return overviewView.value === 'company' ? '工作室预算' : '预算健康'
})
const tableDesc = computed(() => {
  if (riskItems.value.length > 0) return '优先处理已超额和预计耗尽的对象。'
  return overviewView.value === 'company'
    ? '全部工作室的预算分配与消耗概览，可逐行点击「调整」进入配置。'
    : '没有超过 80% 的预算对象，展示当前消耗最高的对象。'
})

// ─── dept_admin 专属计算 ───────────────────────────────────────────────────────
const deptContainerItems = computed(() =>
  overviewView.value === 'department'
    ? items.value.filter(i => i.scope_type === 'dept_daily' || i.scope_type === 'dept_service')
    : [],
)

// 全量成员（所有管辖部门，用于 KPI 计数）
const memberTableItems = computed(() =>
  items.value
    .filter(i => i.scope_type === 'member')
    .slice()
    .sort((a, b) => {
      const ratioDiff = (b.used_ratio || 0) - (a.used_ratio || 0)
      return ratioDiff !== 0 ? ratioDiff : (b.used_usd || 0) - (a.used_usd || 0)
    }),
)

// 按部门过滤后的成员（多部门切换器使用）
const filteredMemberItems = computed(() => {
  if (!selectedDeptFilter.value || managedDeptIds.value.length <= 1) {
    return memberTableItems.value
  }
  return memberTableItems.value.filter(
    item => item.parent_scope_id === selectedDeptFilter.value,
  )
})

// enrichedMembers：合并预算 + 7天用量，优先展示高消耗 / 高风险成员
const enrichedMembers = computed<EnrichedMember[]>(() => {
  return filteredMemberItems.value
    .map(item => {
      const cost7d = memberUsage7d.value.get(item.scope_id) ?? 0
      const dailyAvg7d = cost7d / 7
      const dailyRisk =
        item.daily_amount_usd > 0 ? Math.min(dailyAvg7d / item.daily_amount_usd, 2) : 0
      const riskLevel = budgetColorLevel(
        Math.max(item.used_ratio || 0, item.daily_amount_usd > 0 ? dailyRisk : 0),
      )
      return { ...item, cost7d, dailyAvg7d, dailyRisk, riskLevel }
    })
    .sort((a, b) => {
      // 排序：危险 > 预警 > 正常；同级按近7天消耗降序
      const lvl = { danger: 2, warning: 1, success: 0 } as const
      const diff = lvl[b.riskLevel] - lvl[a.riskLevel]
      return diff !== 0 ? diff : b.cost7d - a.cost7d
    })
})

// riskMembers：月额度 ≥ 80% 或7天日均超日限 80%
const riskMembers = computed(() =>
  enrichedMembers.value.filter(
    m => (m.used_ratio || 0) >= 0.8 || (m.daily_amount_usd > 0 && m.dailyRisk >= 0.8),
  ),
)

// ─── 范围分布 ─────────────────────────────────────────────────────────────────
const scopeBreakdown = computed(() => {
  const map = new Map<BudgetScopeType, { type: BudgetScopeType; amount: number; used: number }>()
  for (const item of items.value) {
    const row = map.get(item.scope_type) || { type: item.scope_type, amount: 0, used: 0 }
    row.amount += Math.max(item.amount_usd || 0, 0)
    row.used += item.used_usd || 0
    map.set(item.scope_type, row)
  }
  return Array.from(map.values()).sort((a, b) => b.used - a.used)
})

// ─── 数据加载 ─────────────────────────────────────────────────────────────────
async function load() {
  loading.value = true
  try {
    const [budgetItems, contexts] = await Promise.all([
      getBudgetList(undefined, undefined, overviewView.value),
      getBudgetContext().catch(() => []),
    ])
    items.value = budgetItems
    contextItems.value = contexts

    // 仅 dept_admin：并行拉取所有管辖部门的近7天成员用量（来自 call_logs，数据可靠）
    if (isDeptAdmin.value && managedDeptIds.value.length > 0) {
      try {
        const today = new Date()
        const end = today.toISOString().slice(0, 10)
        const startDate = new Date(today)
        startDate.setDate(startDate.getDate() - 6)
        const start = startDate.toISOString().slice(0, 10)

        // 多部门并行拉取，合并结果（同一用户可能属于多部门，累加用量）
        const results = await Promise.allSettled(
          managedDeptIds.value.map(id =>
            http.get('/api/v1/usage/dashboard', {
              params: { scope: 'dept', id, start, end, page_size: 100 },
            }),
          ),
        )
        const merged = new Map<string, number>()
        for (const r of results) {
          if (r.status === 'fulfilled') {
            const breakdown: any[] = r.value?.data?.breakdown ?? r.value?.data?.data?.breakdown ?? []
            for (const b of breakdown) {
              merged.set(b.id as string, (merged.get(b.id) ?? 0) + ((b.cost_estimate ?? 0) as number))
            }
          }
        }
        memberUsage7d.value = merged
      } catch {
        // 7天用量是补充信息，失败时静默降级
        memberUsage7d.value = new Map()
      }
    }
  } finally {
    loading.value = false
  }
}

// ─── 展示辅助函数 ─────────────────────────────────────────────────────────────
function displayAmount(value: number) {
  if (value == null || value < 0) return '不限'
  return formatUSD(value, { compact: true })
}

function displayScopeName(row: BudgetItem) {
  if (row.label) return row.label
  const fallback: Record<BudgetScopeType, string> = {
    company: '全公司预算',
    studio: '未命名工作室',
    dept_daily: '未命名部门日常预算',
    dept_service: '未命名部门服务预算',
    member: '未命名成员配额',
  }
  return fallback[row.scope_type] || SCOPE_TYPE_LABELS[row.scope_type]
}

// 统一风险颜色（进度条用，代理 budgetColorLevel）
function riskColor(ratio: number): string {
  const level = budgetColorLevel(ratio)
  if (level === 'danger') return '#f56c6c'
  if (level === 'warning') return '#e6a23c'
  return '#36B34E'
}

function statusText(ratio: number) {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

// admin/studio_admin 表格行类名（风险竖条）
function riskRowClass({ row }: { row: BudgetItem }) {
  const ratio = row.used_ratio || 0
  if (ratio >= 1) return 'row-danger'
  if (ratio >= 0.8) return 'row-warning'
  return ''
}

// dept_admin 成员表格行类名
function memberRiskRowClass({ row }: { row: EnrichedMember }) {
  return row.riskLevel === 'danger' ? 'row-danger' : row.riskLevel === 'warning' ? 'row-warning' : ''
}

// ─── BudgetEditDrawer 状态 ────────────────────────────────────────────────────
const budgetDrawerVisible = ref(false)
const budgetDrawerScopeType = ref<'studio' | 'dept'>('studio')
const budgetDrawerScopeId = ref('')
const budgetDrawerTitle = ref('')
const budgetDrawerChildIds = ref<string[] | undefined>(undefined)
const budgetDrawerCanEdit = ref(false)
const budgetDrawerCanEditSelf = ref(false)

function openBudgetDrawer(
  opts: { scope_type: string; scope_id: string; label?: string },
  childIds?: string[],
) {
  const isStudio = opts.scope_type === 'studio'
  budgetDrawerScopeType.value = isStudio ? 'studio' : 'dept'
  budgetDrawerScopeId.value = opts.scope_id
  budgetDrawerTitle.value = opts.label || opts.scope_id
  budgetDrawerChildIds.value = childIds

  if (auth.effectiveRole === 'admin') {
    budgetDrawerCanEdit.value = true
    budgetDrawerCanEditSelf.value = isStudio
  } else {
    // studio_admin / dept_admin：canEdit=true，不可直接修改自身节点总额
    budgetDrawerCanEdit.value = true
    budgetDrawerCanEditSelf.value = false
  }
  budgetDrawerVisible.value = true
}

// ─── MemberQuotaDialog 状态 ───────────────────────────────────────────────────
const memberQuotaVisible = ref(false)
const memberQuotaTarget = ref<BudgetItem | null>(null)

// ─── 成员预算详情弹窗（P0-1: 预算限制表 + P1-2: 等级子预算行）───────────────
const memberDetailVisible = ref(false)
const memberDetailTarget = ref<BudgetItem | null>(null)

interface MemberTierRow {
  isTotal: boolean
  name: string
  color: string
  limit: number | null
  used: number
  ratio: number
}

const TIER_META = [
  { name: '高级', color: '#8b5cf6', limit: 80, used: 77 },
  { name: '标准', color: '#3b82f6', limit: 120, used: 69 },
  { name: '经济', color: '#10b981', limit: null, used: 38 },
] as const

const memberTierRows = computed<MemberTierRow[]>(() => {
  const member = memberDetailTarget.value
  if (!member) return []
  const totalLimit = member.amount_usd > 0 ? member.amount_usd : 300
  const totalUsed = member.used_usd || 218
  const totalRatio = totalLimit > 0 ? totalUsed / totalLimit : 0
  const totalRow: MemberTierRow = {
    isTotal: true,
    name: '总预算',
    color: 'transparent',
    limit: totalLimit,
    used: totalUsed,
    ratio: totalRatio,
  }
  const tierRows: MemberTierRow[] = TIER_META.map(t => ({
    isTotal: false,
    name: t.name,
    color: t.color,
    limit: t.limit,
    used: t.used,
    ratio: t.limit !== null && t.limit > 0 ? t.used / t.limit : 0,
  }))
  return [totalRow, ...tierRows]
})

function openMemberDetail(member: BudgetItem) {
  memberDetailTarget.value = member
  memberDetailVisible.value = true
}

function openMemberQuota(member: BudgetItem) {
  memberQuotaTarget.value = member
  memberQuotaVisible.value = true
}

// ─── 导航 ─────────────────────────────────────────────────────────────────────
// ─── Redis 计数器重建 ────────────────────────────────────────────────────────
const rebuildLoading = ref(false)

async function rebuildCounters() {
  try {
    await ElMessageBox.confirm(
      '将从 call_logs 重新统计当月/周/日用量并写回 Redis。仅在数据异常时使用，正常情况无需操作。',
      '重建预算计数器',
      { confirmButtonText: '确认重建', cancelButtonText: '取消', type: 'warning' }
    )
  } catch {
    return
  }
  rebuildLoading.value = true
  try {
    const result = await rebuildBudgetCounters()
    ElMessage.success(`重建完成：写入 ${result.keys_written} 个计数器（${result.month_period}）`)
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '重建失败')
  } finally {
    rebuildLoading.value = false
  }
}

// ─── 公司预算池设置 ──────────────────────────────────────────────────────────
const companyBudgetDialogVisible = ref(false)
const companyBudgetSaving = ref(false)
const companyBudgetForm = ref({ amount: 0, weekly: -1, daily: -1 })

function openCompanyBudgetDialog() {
  const item = companyPoolItem.value
  companyBudgetForm.value = {
    amount: item ? item.amount_usd : 0,
    weekly: item ? item.weekly_amount_usd : -1,
    daily: item ? item.daily_amount_usd : -1,
  }
  companyBudgetDialogVisible.value = true
}

async function saveCompanyBudget() {
  companyBudgetSaving.value = true
  try {
    await setBudget({
      scope_type: 'company',
      scope_id: 'GLOBAL',
      amount_usd: companyBudgetForm.value.amount,
      weekly_amount_usd: companyBudgetForm.value.weekly,
      daily_amount_usd: companyBudgetForm.value.daily,
    })
    ElMessage.success('公司预算池已更新')
    companyBudgetDialogVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    companyBudgetSaving.value = false
  }
}

function goOrg(row?: BudgetItem) {
  // admin：保留跳转到组织治理页面
  if (!row && auth.effectiveRole === 'admin') {
    router.push('/admin/org')
    return
  }

  if (!row) {
    if (auth.effectiveRole === 'dept_admin') {
      const deptRole = auth.orgRoles?.find(r => r.scope_type === 'dept_daily' || r.scope_type === 'dept')
      if (deptRole) {
        openBudgetDrawer({ scope_type: 'dept', scope_id: deptRole.scope_id })
        return
      }
    }
    if (auth.effectiveRole === 'studio_admin') {
      const studioRole = auth.orgRoles?.find(r => r.scope_type === 'studio')
      if (studioRole) {
        openBudgetDrawer(
          { scope_type: 'studio', scope_id: studioRole.scope_id, label: studioPoolItem.value?.label },
          studioDeptRows.value.map(r => r.deptId),
        )
        return
      }
    }
    router.push('/admin/org')
    return
  }

  // 成员行：打开所属部门的预算抽屉（而非跳转 OrgTreePage）
  if (row.scope_type === 'member') {
    const deptId = row.parent_scope_id || currentDeptId.value
    if (deptId) {
      openBudgetDrawer({ scope_type: 'dept', scope_id: deptId, label: deptId })
    } else {
      router.push('/admin/org')
    }
    return
  }

  openBudgetDrawer({ scope_type: row.scope_type, scope_id: row.scope_id, label: row.label })
}

function goExpansion() {
  router.push('/admin/expansion-requests')
}

// 供分配明细弹窗、其他场景下钻使用
function drillDown(scopeType: string, scopeId: string, label?: string) {
  showChainDetail.value = false
  openBudgetDrawer({ scope_type: scopeType, scope_id: scopeId, label })
}

// ─── 下钻侧抽屉（admin 专用：工作室 → 部门 → 成员）──────────────────────────
const drawerVisible = ref(false)
const drawerStudio = ref<BudgetItem | null>(null)
const drawerLoading = ref(false)

// 部门行（来自 view=studio&scope_id=<studioId>）
interface DrawerDeptRow {
  deptId: string
  label: string
  daily: BudgetItem | null
  service: BudgetItem | null
  members: BudgetItem[]      // 展开后加载的成员配额
  membersLoaded: boolean
  membersLoading: boolean
  expanded: boolean
}
const drawerDepts = ref<DrawerDeptRow[]>([])

async function openStudioDrawer(studio: BudgetItem) {
  drawerStudio.value = studio
  drawerVisible.value = true
  drawerDepts.value = []
  drawerLoading.value = true
  try {
    const raw = await getBudgetList(undefined, studio.scope_id, 'studio')
    // 按部门分组（dept_daily / dept_service）
    const map = new Map<string, DrawerDeptRow>()
    for (const item of raw) {
      if (item.scope_type !== 'dept_daily' && item.scope_type !== 'dept_service') continue
      const key = item.scope_id
      if (!map.has(key)) {
        map.set(key, {
          deptId: key,
          label: item.label || key,
          daily: null,
          service: null,
          members: [],
          membersLoaded: false,
          membersLoading: false,
          expanded: false,
        })
      }
      const entry = map.get(key)!
      if (item.scope_type === 'dept_daily') entry.daily = item
      if (item.scope_type === 'dept_service') entry.service = item
    }
    drawerDepts.value = Array.from(map.values()).sort(
      (a, b) =>
        Math.max(b.daily?.used_ratio || 0, b.service?.used_ratio || 0) -
        Math.max(a.daily?.used_ratio || 0, a.service?.used_ratio || 0),
    )
  } finally {
    drawerLoading.value = false
  }
}

async function toggleDeptMembers(row: DrawerDeptRow) {
  if (row.expanded) {
    row.expanded = false
    return
  }
  row.expanded = true
  if (row.membersLoaded) return
  row.membersLoading = true
  try {
    const raw = await getBudgetList(undefined, row.deptId, 'department')
    row.members = raw.filter(i => i.scope_type === 'member')
    row.membersLoaded = true
  } finally {
    row.membersLoading = false
  }
}

function goOrgFromDrawer(scopeType: string, scopeId: string, label?: string) {
  drawerVisible.value = false
  openBudgetDrawer({ scope_type: scopeType, scope_id: scopeId, label })
}

// ─── 成员配额抽屉（studio_admin 专用：部门 → 成员）──────────────────────────
const deptMemberDrawerVisible = ref(false)
const deptMemberDrawerDept = ref<StudioDeptRow | null>(null)
const deptMemberDrawerLoading = ref(false)
const deptMemberDrawerMembers = ref<BudgetItem[]>([])

// ─── Studio 视图部门表格复选框 ────────────────────────────────────────────────
const studioDeptTableRef = ref()
const studioDeptSelected = ref<StudioDeptRow[]>([])

function onStudioDeptSelectionChange(rows: StudioDeptRow[]) {
  studioDeptSelected.value = rows
}

function clearStudioDeptSelection() {
  studioDeptSelected.value = []
  studioDeptTableRef.value?.clearSelection()
}

function deptRowClassName({ row }: { row: StudioDeptRow }): string {
  return row.maxRatio >= 1 ? 'row-danger' : row.maxRatio >= 0.8 ? 'row-warning' : ''
}

async function openDeptMemberDrawer(deptRow: StudioDeptRow) {
  deptMemberDrawerDept.value = deptRow
  deptMemberDrawerVisible.value = true
  deptMemberDrawerMembers.value = []
  deptMemberDrawerLoading.value = true
  try {
    const raw = await getBudgetList(undefined, deptRow.deptId, 'department')
    deptMemberDrawerMembers.value = raw
      .filter(i => i.scope_type === 'member')
      .sort((a, b) => (b.amount_usd || 0) - (a.amount_usd || 0))
  } finally {
    deptMemberDrawerLoading.value = false
  }
}

function goOrgFromDeptDrawer(scopeType: string, scopeId: string, label?: string) {
  deptMemberDrawerVisible.value = false
  openBudgetDrawer({ scope_type: scopeType, scope_id: scopeId, label })
}

onMounted(load)
</script>

<style scoped>
/* ── 页面容器 ─────────────────────────────────────────────────────────────── */
.budget-overview {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 28px 32px;
}
.overview-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
}
.overview-header h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.overview-header p {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.header-actions {
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}

/* ── KPI 条 ───────────────────────────────────────────────────────────────── */
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi-strip.kpi-5 {
  grid-template-columns: repeat(5, minmax(0, 1fr));
}
/* 多部门时：池横幅 + 3 辅助卡 = 自适应 */
.kpi-strip.kpi-dept-multi {
  grid-template-columns: 1fr repeat(3, minmax(120px, 180px));
}
.kpi {
  padding: 16px 18px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
}
.kpi-label {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 8px;
}
.kpi strong {
  font-size: 24px;
  color: var(--el-text-color-primary);
  display: block;
}
.kpi strong.danger {
  color: var(--el-color-danger);
}
.kpi-note {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.kpi-note-inline {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  font-weight: 400;
  margin-left: 4px;
}

/* 部门池 KPI 卡 */
.kpi-pool {
  display: flex;
  flex-direction: column;
}
.kpi-pool-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}
.kpi-pool-head .kpi-label {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-bottom: 0;
  white-space: nowrap;
  flex-shrink: 0;
}
.kpi-pool--warning {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}
.kpi-pool--danger {
  border-color: var(--el-color-danger-light-5);
  background: var(--el-color-danger-light-9);
}
.kpi-studio-pool {
  border-color: color-mix(in srgb, #36B34E 30%, var(--el-border-color-light));
  background: color-mix(in srgb, #36B34E 5%, var(--el-bg-color));
}
.kpi-pool-usage {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
.kpi-pool-sub-limits {
  margin-top: 6px;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.kpi-pool-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
.kpi-sub-limit-row {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
}
.kpi-sub-limit-label {
  background: var(--el-fill-color);
  border-radius: 3px;
  padding: 1px 5px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}
.kpi-sub-limit-used { color: var(--el-text-color-secondary); }
.kpi-sub-limit-slash { color: var(--el-text-color-placeholder); }
.kpi-sub-limit-amount { color: var(--el-text-color-primary); font-weight: 500; }
.kpi-dept-count {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  font-weight: 400;
}

/* 风险状态 KPI */
.kpi-danger {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}

/* 扩容申请跳转卡 */
.kpi-action-card {
  cursor: pointer;
  transition: all 0.15s;
}
.kpi-action-card:hover {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.kpi-action-text {
  font-size: 20px;
  color: var(--el-color-primary) !important;
}

/* ── 多部门：部门池横幅（占据池卡位置，与 3 张辅助卡并排） ── */
.dept-pools-banner {
  grid-column: 1 / -4; /* 占前面所有列，留出 3 列给辅助卡 */
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
  padding: 14px 16px;
  min-width: 0;
}
.dept-pools-banner-title {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 600;
  margin-bottom: 10px;
}
.dept-pools-grid {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.dept-pool-group {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 10px 12px;
  background: var(--el-fill-color-extra-light);
}
.dept-pool-group--warning {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}
.dept-pool-group--danger {
  border-color: var(--el-color-danger-light-5);
  background: var(--el-color-danger-light-9);
}
.dept-pool-group-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
}
.dept-pool-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  margin-top: 4px;
}
.pool-type-label {
  color: var(--el-text-color-secondary);
  min-width: 24px;
}
.pool-amount {
  color: var(--el-text-color-primary);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  min-width: 56px;
}

/* ── admin / studio_admin 双栏布局 ─────────────────────────────────────────── */
.overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 16px;
  align-items: start;
}
.panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.panel-main {
  padding: 0;
}
.side-panel {
  padding: 18px;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  padding: 18px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.panel-head.compact {
  padding: 0 0 14px;
  border-bottom: none;
}
.panel-head h2 {
  margin: 0;
  font-size: 16px;
  color: var(--el-text-color-primary);
}
.panel-head p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

/* 风险行：左侧颜色竖条 */
:deep(.row-danger td:first-child) {
  box-shadow: inset 3px 0 0 var(--el-color-danger);
}
:deep(.row-warning td:first-child) {
  box-shadow: inset 3px 0 0 var(--el-color-warning);
}

/* ── 健康状态提示 ─────────────────────────────────────────────────────────── */
.healthy-state {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 20px;
  padding: 14px 16px;
  border: 1px solid var(--el-color-success-light-7);
  border-radius: 12px;
  background: var(--el-color-success-light-9);
}
.healthy-icon {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  background: var(--el-color-success);
  font-weight: 700;
  flex-shrink: 0;
}
.healthy-state strong {
  display: block;
  color: var(--el-text-color-primary);
  font-size: 14px;
}
.healthy-state span {
  display: block;
  margin-top: 2px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* ── 权限横幅 ───────────────────────────────────────────────────────────────── */
.permission-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 13px;
  border: 1px solid var(--el-color-primary-light-7);
  background: var(--el-color-primary-light-9);
  border-radius: 9px;
  color: var(--el-text-color-regular);
  font-size: 12px;
}
.margin-banner {
  margin: 0 0 14px;
}
.permission-banner strong {
  color: var(--el-color-primary);
}

/* ── 治理链路（可视化层级链） ────────────────────────────────────────────── */
.context-section {
  margin-bottom: 2px;
}
.context-section .panel-head.compact p {
  margin: 0;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.chain-ladder {
  display: flex;
  flex-direction: column;
  align-items: stretch;
}
.chain-node-card {
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-blank);
}
.chain-node-card.chain-node--current {
  border-color: var(--el-color-primary-light-5);
  background: var(--el-color-primary-light-9);
}
.chain-node-header {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 5px;
}
.chain-node-name {
  flex: 1;
  font-size: 13px;
  color: var(--el-text-color-regular);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chain-current-tag {
  flex-shrink: 0;
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 10px;
  background: var(--el-color-primary-light-7);
  color: var(--el-color-primary);
  font-weight: 600;
}
.chain-node-amount {
  padding-left: 19px;
  font-size: 17px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
/* 连接符：上下节点之间的竖线 + 箭头 */
.chain-connector {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 2px 0;
  gap: 2px;
}
.chain-connector-line {
  display: block;
  width: 1px;
  height: 8px;
  background: var(--el-border-color);
}
.chain-connector-arrow {
  color: var(--el-text-color-placeholder);
  font-size: 11px;
  line-height: 1;
}
/* 下发分配摘要卡片（始终只占一行，不随子项数增长） */
.chain-dist-summary {
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px dashed var(--el-border-color-light);
  background: var(--el-fill-color-lighter);
}
.chain-dist-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}
.chain-dist-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.chain-detail-btn {
  font-size: 11px;
  color: var(--el-color-primary);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  white-space: nowrap;
  flex-shrink: 0;
}
.chain-detail-btn:hover {
  text-decoration: underline;
}
.chain-dist-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}
.chain-dist-amount {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.chain-dist-pct {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.chain-dist-empty {
  padding: 8px 12px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  text-align: center;
}

/* ── 分配明细弹窗 ─────────────────────────────────────────────────────────── */
.chain-detail-dialog {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.chain-detail-summary-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  padding: 12px 14px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}
.chain-detail-summary-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: center;
}
.chain-detail-summary-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.chain-detail-summary-value {
  font-size: 14px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.chain-detail-rows {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 420px;
  overflow-y: auto;
}
.chain-detail-item {
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-blank);
}
.chain-detail-item-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.chain-detail-item-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.chain-detail-item-total {
  font-size: 14px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.chain-detail-item-pct {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
/* 日常 + 服务池并排行 */
.chain-detail-pool-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}
.chain-detail-pool {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  flex: 1;
}
.chain-detail-pool-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.chain-detail-pool-val {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.chain-detail-pool-pct {
  color: var(--el-color-primary);
}
.chain-detail-pool-sep {
  font-size: 14px;
  color: var(--el-text-color-placeholder);
  flex-shrink: 0;
  margin-top: 10px;
}
.chain-detail-pool-total-sep {
  font-weight: 700;
}
.chain-detail-empty {
  text-align: center;
  padding: 24px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}
.divider {
  height: 1px;
  background: var(--el-border-color-lighter);
  margin: 18px 0;
}

/* ── 快速跳转（精简版） ───────────────────────────────────────────────────── */
.quick-links h3 {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.quick-link-btn {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
  cursor: pointer;
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
  text-align: left;
  transition: all 0.15s;
}
.quick-link-btn:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.quick-link-arrow {
  color: var(--el-text-color-placeholder);
  font-size: 14px;
}

/* ── dept_admin 全宽视图 ──────────────────────────────────────────────────── */
/* 部门预算池概览条 */
.dept-summary-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  padding: 14px 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: var(--el-bg-color);
}
.dept-summary-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-extra-light);
  font-size: 13px;
}
.dept-summary-label {
  color: var(--el-text-color-regular);
  font-weight: 500;
}
.dept-summary-type-tag {
  font-size: 11px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 4px;
  background: color-mix(in srgb, currentColor 12%, transparent);
}
.dept-summary-value {
  color: var(--el-text-color-primary);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

/* 预警区 */
.alert-panel {
  padding: 0;
}
.alert-panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 14px 20px;
  background: var(--el-color-warning-light-9);
  border-bottom: 1px solid var(--el-color-warning-light-7);
}
.alert-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-color-warning-dark-2);
}
.alert-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  height: 22px;
  border-radius: 11px;
  background: var(--el-color-warning);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  padding: 0 6px;
}
.alert-sub {
  font-size: 12px;
  font-weight: 400;
  color: var(--el-color-warning-dark-2);
  opacity: 0.8;
}

/* 健康面板 */
.healthy-panel {
  padding: 8px;
}

/* 成员面板 */
.members-panel {
  padding: 0;
}

/* 风险指示点（成员表格） */
.risk-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--el-color-success);
}
.risk-indicator.risk-warning {
  background: var(--el-color-warning);
}
.risk-indicator.risk-danger {
  background: var(--el-color-danger);
  box-shadow: 0 0 0 3px var(--el-color-danger-light-7);
}

/* ── 部门切换器 ───────────────────────────────────────────────────────────── */
.dept-switcher {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.dept-switcher-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.dept-switcher-hint {
  font-size: 12px;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  padding: 2px 8px;
  border-radius: 4px;
}

/* ── 成员所属部门标签（全量视图） ─────────────────────────────────────────── */
.member-dept-tag {
  margin-top: 2px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  padding: 0 5px;
  border-radius: 3px;
  display: inline-block;
}

/* ── 通用单元格样式 ───────────────────────────────────────────────────────── */
.scope-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}
.scope-dot {
  width: 8px;
  height: 8px;
  border-radius: 99px;
  flex-shrink: 0;
}
.scope-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.scope-id {
  margin-top: 2px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.usage-cell {
  display: grid;
  grid-template-columns: 100px 1fr;
  align-items: center;
  gap: 10px;
}
/* studio_admin 分组表格：池内联单元格 */
.pool-inline-cell {
  padding: 2px 0;
}
.pool-inline-usage {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.pool-inline-limits {
  margin-top: 4px;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.limit-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tabular {
  font-variant-numeric: tabular-nums;
  font-size: 13px;
}
.text-muted {
  color: var(--el-text-color-placeholder);
}
.muted {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

/* ── 响应式 ───────────────────────────────────────────────────────────────── */
@media (max-width: 1200px) {
  .overview-grid {
    grid-template-columns: minmax(0, 1fr) 280px;
  }
}
@media (max-width: 1080px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }
  .kpi-strip.kpi-5 {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .budget-overview {
    padding: 18px;
  }
  .overview-header {
    flex-direction: column;
  }
  .kpi-strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .kpi-strip.kpi-5 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

/* ── 下钻侧抽屉 ───────────────────────────────────────────────────────────── */
.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
.drawer-header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.drawer-header-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.drawer-header-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.drawer-dept-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 4px 0;
}
.drawer-empty {
  text-align: center;
  padding: 32px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}
.drawer-empty-sm {
  padding: 8px 0;
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  text-align: center;
}
.drawer-dept-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  overflow: hidden;
  background: var(--el-fill-color-blank);
}
.drawer-dept-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  cursor: pointer;
  user-select: none;
  background: var(--el-fill-color-light);
  transition: background 0.15s;
}
.drawer-dept-head:hover {
  background: var(--el-fill-color);
}
.drawer-dept-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.drawer-dept-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer-dept-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.drawer-dept-total {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.drawer-expand-icon {
  font-size: 10px;
  color: var(--el-text-color-placeholder);
}
/* 池详情行 */
.drawer-dept-pools {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 14px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.drawer-pool-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.drawer-pool-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  width: 36px;
  flex-shrink: 0;
}
.drawer-pool-amount {
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-primary);
  width: 72px;
  flex-shrink: 0;
  text-align: right;
}
/* 成员展开区 */
.drawer-members {
  padding: 8px 14px 10px;
  border-top: 1px dashed var(--el-border-color-lighter);
  background: var(--el-fill-color-lighter);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.drawer-member-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.drawer-member-name {
  font-size: 12px;
  color: var(--el-text-color-regular);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer-member-amount {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-primary);
  width: 64px;
  text-align: right;
  flex-shrink: 0;
}
.drawer-member-footer {
  padding-top: 6px;
  border-top: 1px solid var(--el-border-color-lighter);
  text-align: right;
}
/* dept member drawer 专属 */
.drawer-dept-summary {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 16px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
  margin-bottom: 16px;
}
.drawer-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.drawer-section-count {
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 10px;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
  font-weight: 400;
}
.drawer-member-card {
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-blank);
  margin-bottom: 8px;
}
.drawer-member-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 2px;
}
.drawer-member-card-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}
.drawer-member-limits {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

/* ── 成员预算详情弹窗 ───────────────────────────────────────────────────────── */
.member-detail-summary {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 18px 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-blank);
  margin-bottom: 16px;
}
.member-detail-summary-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.member-detail-summary-name {
  font-size: 18px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin-top: 4px;
}
.member-detail-summary-meta {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
.member-detail-summary-amount {
  text-align: right;
  min-width: 180px;
}
.member-detail-summary-amount strong {
  font-size: 22px;
  color: var(--el-text-color-primary);
}
.member-detail-summary-amount span {
  color: var(--el-text-color-secondary);
}
.member-detail-panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  overflow: hidden;
}
.member-detail-panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 17px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.member-detail-panel-head h3 {
  margin: 0;
  font-size: 16px;
  color: var(--el-text-color-primary);
}
.member-detail-panel-head p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tier-limit-name {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.tier-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
</style>
