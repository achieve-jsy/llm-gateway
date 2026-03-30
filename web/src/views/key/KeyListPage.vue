<template>
  <div class="key-list-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">API 密钥</h2>
        <p class="page-desc">管理你的 Key{{ authStore.isManager ? '，审批团队成员的申请' : '' }}</p>
        <div class="base-url-inline">
          <span class="base-url-inline__label">Base URL</span>
          <code class="base-url-inline__value">{{ displayBaseUrl }}</code>
          <button class="base-url-inline__btn" :class="{ ok: urlCopied }" @click="copyBaseUrl">
            <el-icon><component :is="urlCopied ? Check : CopyDocument" /></el-icon>
            {{ urlCopied ? '已复制' : '复制' }}
          </button>
        </div>
      </div>
      <div class="header-actions">
        <el-button type="primary" @click="handleCreateClick">{{ createKeyButtonLabel }}</el-button>
      </div>
    </div>

    <el-alert
      v-if="needsApplyToCreate"
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom:16px"
    >
      本部门开启了 Key 创建审批，新 Key 需经部门负责人审批后才会生效。
    </el-alert>

    <!-- 多 Tab：存在「我的申请」「审批管理」任一时展示 -->
    <!-- 角色未加载完时保守渲染多 Tab 分支，避免首帧错位 -->
    <el-tabs v-if="hasMultiTab" v-model="activeTab">
      <el-tab-pane label="我的密钥" name="keys">
        <!-- key table -->
        <div class="key-table-wrap">
          <el-empty v-if="keys.length === 0 && !loading" description="暂无 API 密钥">
            <el-button type="primary" @click="handleCreateClick">{{ createFirstKeyButtonLabel }}</el-button>
          </el-empty>
          <el-table v-else v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
            <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />
            <el-table-column label="API 密钥" min-width="160">
              <template #default="{ row }"><span class="key-masked">{{ row.maskedKey }}</span><el-button size="small" text type="primary" @click.stop="copyFullKey(row)">复制</el-button></template>
            </el-table-column>
            <el-table-column label="我的模型" min-width="140">
              <template #default="{ row }">
                <template v-if="row.accessAllGroups"><el-tag size="small" type="success">全部模型</el-tag></template>
                <template v-else-if="row.boundGroups?.length"><el-tag v-for="g in row.boundGroups" :key="g.id" size="small" type="info" style="margin-right:4px;margin-bottom:2px">{{ g.name }}</el-tag></template>
                <span v-else class="text-muted">未分配</span>
                <el-button size="small" text type="primary" @click.stop="openGroupDialog(row)">管理</el-button>
              </template>
            </el-table-column>
            <el-table-column label="过期" width="150"><template #default="{ row }">{{ row.expiresAt ? formatDate(row.expiresAt) : '永久' }}</template></el-table-column>
            <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" inline-prompt active-text="活跃" inactive-text="禁用" @change="(v: boolean) => handleToggle(row, v)" /></template></el-table-column>
            <el-table-column label="日志" width="95" align="center"><template #default="{ row }"><el-switch v-model="row.logEnabled" size="small" inline-prompt active-text="记录" inactive-text="关闭" @change="(v: boolean) => handleToggleLog(row, v)" /></template></el-table-column>
            <el-table-column label="创建时间" width="155" sortable prop="createdAt"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="320" fixed="right" align="center">
              <template #default="{ row }">
                <el-button size="small" text type="primary" @click.stop="openUseKey(row)">使用密钥</el-button>
                <el-button size="small" text type="primary" @click.stop="openOpenCode(row)">配置 OpenCode</el-button>
                <el-button size="small" text type="warning" @click.stop="handleReset(row)">重置</el-button>
                <el-button size="small" text type="danger" @click.stop="handleDelete(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-if="total > 0" class="pagination-wrapper">
          <el-pagination
            v-model:current-page="currentPage"
            :page-size="pageSize"
            :total="total"
            :hide-on-single-page="false"
            layout="total, prev, pager, next"
            @current-change="loadKeys"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="showMyApplicationsTab" name="my-applications">
        <template #label>我的申请<el-badge v-if="myAppSummary.pending>0" :value="myAppSummary.pending" type="warning" /></template>

        <div class="my-app-panel">
          <div class="my-app-summary">
            <div class="my-app-stat">
              <span class="my-app-stat__value">{{ myAppSummary.pending }}</span>
              <span class="my-app-stat__label">审批中</span>
            </div>
            <div class="my-app-stat my-app-stat--success">
              <span class="my-app-stat__value">{{ myAppSummary.approved }}</span>
              <span class="my-app-stat__label">已通过</span>
            </div>
            <div class="my-app-stat my-app-stat--danger">
              <span class="my-app-stat__value">{{ myAppSummary.rejected }}</span>
              <span class="my-app-stat__label">已拒绝</span>
            </div>
            <div class="my-app-stat my-app-stat--muted">
              <span class="my-app-stat__value">{{ myAppTotal }}</span>
              <span class="my-app-stat__label">全部记录</span>
            </div>
          </div>

          <div v-if="myAppAllTotal > 0" class="key-request-filter-bar">
            <el-select
              v-model="myAppStatusFilter"
              placeholder="申请状态"
              clearable
              style="width: 140px"
            >
              <el-option label="审批中" value="pending" />
              <el-option label="已通过" value="approved" />
              <el-option label="已拒绝" value="rejected" />
              <el-option label="已取消" value="cancelled" />
            </el-select>
          </div>

          <el-empty
            v-if="!myAppLoading && !myApplications.length && myAppTotal === 0"
            :description="myAppStatusFilter ? '没有符合筛选条件的申请' : '暂无申请记录'"
          >
            <el-button type="primary" @click="handleCreateClick">申请创建 Key</el-button>
          </el-empty>

          <el-table
            v-else
            v-loading="myAppLoading"
            :data="myApplications"
            class="my-app-table"
            size="default"
            stripe
            :border="false"
          >
            <el-table-column label="Key 名称" min-width="120" prop="key_name" show-overflow-tooltip />
            <el-table-column label="状态" width="92" align="center">
              <template #default="{ row }">
                <el-tag :type="STATUS_COLORS[row.status]" size="small" effect="light">
                  {{ STATUS_LABELS[row.status] }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="预算申请" min-width="158" align="center" class-name="key-request-budget-col">
              <template #default="{ row }">
                <KeyRequestBudgetTag :budget-monthly="row.budget_monthly" :show-icon="false" />
              </template>
            </el-table-column>
            <el-table-column label="用途说明" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="row.purpose">{{ row.purpose }}</span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="申请时间" width="150">
              <template #default="{ row }">
                <span class="time-text">{{ formatDate(row.created_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="处理时间" width="150">
              <template #default="{ row }">
                <span v-if="row.status !== 'pending'" class="time-text">{{ formatDate(row.updated_at) }}</span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="审批人" width="110" show-overflow-tooltip>
              <template #default="{ row }">
                <KeyRequestApproverCell :status="row.status" :approver-name="row.approver_name" />
              </template>
            </el-table-column>
            <el-table-column label="审批备注" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="row.approve_comment">{{ row.approve_comment }}</span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="center" fixed="right">
              <template #default="{ row }">
                <el-popconfirm
                  v-if="row.status === 'pending'"
                  title="确定要取消这个申请吗？"
                  confirm-button-text="确认取消"
                  cancel-button-text="再想想"
                  width="220"
                  @confirm="handleCancelApp(row.id)"
                >
                  <template #reference>
                    <el-button size="small" text type="danger">取消</el-button>
                  </template>
                </el-popconfirm>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
          </el-table>

          <div v-if="myAppTotal > 0" class="pagination-wrapper">
            <el-pagination
              v-model:current-page="myAppPage"
              :page-size="myAppPageSize"
              :total="myAppTotal"
              :hide-on-single-page="false"
              layout="total, prev, pager, next"
              @current-change="loadMyApplications"
            />
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="authStore.isManager" name="approval">
        <template #label>审批管理<el-badge v-if="pendingCount>0" :value="pendingCount" type="danger" /></template>

        <div class="approval-panel">
          <div v-if="canManageDeptPolicy" class="policy-card policy-card--compact" :class="{ 'policy-card--on': deptPolicy === 'approval' }">
            <div class="policy-card-head">
              <div class="policy-card-info">
                <div class="policy-icon" :class="{ 'policy-icon--on': deptPolicy === 'approval' }">
                  <el-icon :size="20"><Key /></el-icon>
                </div>
                <div>
                  <h3 class="policy-title">
                    部门 Key 创建审批
                    <el-tag v-if="deptPolicy==='approval'" size="small" type="success" effect="dark">已开启</el-tag>
                    <el-tag v-else size="small" effect="light">未开启</el-tag>
                  </h3>
                  <p class="policy-sub">
                    <span class="policy-dept-label">管辖部门：</span>
                    <b>{{ policyDeptName }}</b>
                  </p>
                </div>
              </div>
              <el-switch
                :model-value="deptPolicy==='approval'"
                :loading="policySaving"
                :disabled="policySaving"
                size="large"
                inline-prompt
                active-text="开"
                inactive-text="关"
                @change="onPolicySwitchChange"
              />
            </div>
          </div>
          <el-alert
            v-else-if="authStore.effectiveRole === 'dept_admin'"
            type="warning"
            :closable="false"
            show-icon
            class="approval-role-alert"
          >
            未识别到管辖部门，暂无法配置审批策略；你仍可处理下方待办申请。
          </el-alert>

          <div class="approval-summary approval-summary--three">
            <div class="approval-stat approval-stat--warn">
              <span class="approval-stat__value">{{ pendingCount }}</span>
              <span class="approval-stat__label">待审批</span>
            </div>
            <div
              class="approval-stat approval-stat--budget"
              :class="{ 'is-active': pendingBudgetOnly, 'is-disabled': pendingBudgetCount <= 0 }"
              :role="pendingBudgetCount > 0 ? 'button' : undefined"
              :tabindex="pendingBudgetCount > 0 ? 0 : undefined"
              :title="pendingBudgetCount > 0 ? '点击筛选含预算申请' : undefined"
              @click="togglePendingBudgetOnly"
              @keyup.enter="togglePendingBudgetOnly"
            >
              <span class="approval-stat__value">{{ pendingBudgetCount }}</span>
              <span class="approval-stat__label">含预算待办</span>
            </div>
            <div class="approval-stat">
              <span class="approval-stat__value">{{ historyDisplayTotal }}</span>
              <span class="approval-stat__label">历史记录</span>
            </div>
          </div>

          <div class="approval-sub">
            <el-radio-group v-model="approvalSub" size="small">
              <el-radio-button value="pending">
                待审批
                <el-badge v-if="pendingCount>0" :value="pendingCount" type="danger" style="margin-left:4px" />
              </el-radio-button>
              <el-radio-button value="history">审批历史</el-radio-button>
            </el-radio-group>
          </div>

          <div
            v-if="(approvalSub === 'pending' && pendingCount > 0) || (approvalSub === 'history' && historyAllTotal > 0)"
            class="key-request-filter-bar"
          >
            <template v-if="approvalSub === 'pending'">
              <el-check-tag
                v-if="pendingBudgetOnly"
                class="pending-budget-tag"
                checked
                @change="(v: boolean) => { if (!v) { pendingBudgetOnly = false; triggerPendingSearch() } }"
              >
                仅含预算
              </el-check-tag>
              <el-input
                v-model="pendingNameFilter"
                placeholder="搜索申请人或 Key 名称"
                clearable
                style="width: 260px"
                @keyup.enter="triggerPendingSearch"
                @clear="triggerPendingSearch"
              >
                <template #append>
                  <el-button :icon="Search" @click="triggerPendingSearch" />
                </template>
              </el-input>
            </template>
            <template v-else>
              <el-input
                v-model="historyNameFilter"
                placeholder="搜索申请人或 Key 名称"
                clearable
                style="width: 260px"
                @keyup.enter="triggerHistorySearch"
                @clear="triggerHistorySearch"
              >
                <template #append>
                  <el-button :icon="Search" @click="triggerHistorySearch" />
                </template>
              </el-input>
              <el-select
                v-model="historyStatusFilter"
                placeholder="审批结果"
                clearable
                style="width: 140px"
              >
                <el-option label="已通过" value="approved" />
                <el-option label="已拒绝" value="rejected" />
              </el-select>
            </template>
          </div>

          <template v-if="approvalSub === 'pending'">
            <el-empty v-if="!pendingLoading && !pendingItems.length" description="暂无待审批申请" />

            <el-table
              v-else
              v-loading="pendingLoading"
              :data="pendingItems"
              class="approval-table"
              stripe
              :border="false"
            >
              <el-table-column label="Key 名称" min-width="110" prop="key_name" show-overflow-tooltip />
              <el-table-column label="申请人" width="110" show-overflow-tooltip>
                <template #default="{ row }">{{ row.requester_name || '未知' }}</template>
              </el-table-column>
              <el-table-column label="部门" width="120" show-overflow-tooltip>
                <template #default="{ row }">
                  <span v-if="row.dept_name">{{ row.dept_name }}</span>
                  <span v-else class="text-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="预算申请" min-width="158" align="center" class-name="key-request-budget-col">
                <template #default="{ row }">
                  <KeyRequestBudgetTag :budget-monthly="row.budget_monthly" :show-icon="false" />
                </template>
              </el-table-column>
              <el-table-column label="用途说明" min-width="140" show-overflow-tooltip>
                <template #default="{ row }">
                  <span v-if="row.purpose">{{ row.purpose }}</span>
                  <span v-else class="text-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="申请时间" width="150">
                <template #default="{ row }">
                  <span class="time-text">{{ formatDate(row.created_at) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="140" align="center" fixed="right">
                <template #default="{ row }">
                  <div class="approval-table-actions">
                    <el-popconfirm
                      :title="approveConfirmTitle(row)"
                      confirm-button-text="确认通过"
                      width="280"
                      @confirm="handleApprove(row.id)"
                    >
                      <template #reference>
                        <el-button type="primary" size="small">通过</el-button>
                      </template>
                    </el-popconfirm>
                    <el-button size="small" plain type="danger" @click="openReject(row)">驳回</el-button>
                  </div>
                </template>
              </el-table-column>
            </el-table>

            <div v-if="pendingTotal > 0" class="pagination-wrapper approval-pagination">
              <el-pagination
                v-model:current-page="pendingPage"
                :page-size="pendingPageSize"
                :total="pendingTotal"
                :hide-on-single-page="false"
                layout="total, prev, pager, next"
                @current-change="loadPending"
              />
            </div>
          </template>

          <template v-else-if="approvalSub === 'history'">
            <el-empty v-if="!historyLoading && !historyItems.length && historyDisplayTotal === 0" description="暂无历史" />
            <el-table
              v-else
              v-loading="historyLoading"
              :data="historyItems"
              row-key="id"
              class="approval-table"
              stripe
              :border="false"
            >
              <el-table-column label="申请人" width="100" show-overflow-tooltip>
                <template #default="{ row }">{{ row.requester_name || '未知' }}</template>
              </el-table-column>
              <el-table-column label="Key 名称" min-width="110" prop="key_name" show-overflow-tooltip />
              <el-table-column label="部门" width="120" show-overflow-tooltip>
                <template #default="{ row }">
                  <span v-if="row.dept_name">{{ row.dept_name }}</span>
                  <span v-else class="text-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="预算申请" min-width="158" align="center" class-name="key-request-budget-col">
                <template #default="{ row }">
                  <KeyRequestBudgetTag :budget-monthly="row.budget_monthly" :show-icon="false" />
                </template>
              </el-table-column>
              <el-table-column label="用途说明" min-width="120" show-overflow-tooltip>
                <template #default="{ row }">
                  <span v-if="row.purpose">{{ row.purpose }}</span>
                  <span v-else class="text-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="申请时间" width="140">
                <template #default="{ row }">
                  <span class="time-text">{{ formatDate(row.created_at) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="处理时间" width="140">
                <template #default="{ row }">
                  <span class="time-text">{{ formatDate(row.updated_at) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="88" align="center">
                <template #default="{ row }">
                  <el-tag :type="STATUS_COLORS[row.status]" size="small">{{ STATUS_LABELS[row.status] }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="审批人" width="110" show-overflow-tooltip>
                <template #default="{ row }">
                  <KeyRequestApproverCell :status="row.status" :approver-name="row.approver_name" />
                </template>
              </el-table-column>
              <el-table-column label="审批备注" min-width="140" show-overflow-tooltip>
                <template #default="{ row }">
                  <span v-if="row.approve_comment">{{ row.approve_comment }}</span>
                  <span v-else class="text-muted">—</span>
                </template>
              </el-table-column>
            </el-table>

            <div v-if="historyDisplayTotal > 0" class="pagination-wrapper approval-pagination">
              <el-pagination
                v-model:current-page="historyPage"
                :page-size="historyPageSize"
                :total="historyDisplayTotal"
                :hide-on-single-page="false"
                layout="total, prev, pager, next"
                @current-change="loadHistory"
              />
            </div>
          </template>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 单 Tab：只展示「我的密钥」，无 Tabs 栏 -->
    <template v-else>
      <div class="key-table-wrap">
        <el-empty v-if="keys.length === 0 && !loading" description="暂无 API 密钥">
          <el-button type="primary" @click="handleCreateClick">{{ createFirstKeyButtonLabel }}</el-button>
        </el-empty>
        <el-table v-else v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
          <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />
          <el-table-column label="API 密钥" min-width="160">
            <template #default="{ row }"><span class="key-masked">{{ row.maskedKey }}</span><el-button size="small" text type="primary" @click.stop="copyFullKey(row)">复制</el-button></template>
          </el-table-column>
          <el-table-column label="我的模型" min-width="140">
            <template #default="{ row }">
              <template v-if="row.accessAllGroups"><el-tag size="small" type="success">全部模型</el-tag></template>
              <template v-else-if="row.boundGroups?.length"><el-tag v-for="g in row.boundGroups" :key="g.id" size="small" type="info" style="margin-right:4px;margin-bottom:2px">{{ g.name }}</el-tag></template>
              <span v-else class="text-muted">未分配</span>
              <el-button size="small" text type="primary" @click.stop="openGroupDialog(row)">管理</el-button>
            </template>
          </el-table-column>
          <el-table-column label="过期" width="150"><template #default="{ row }">{{ row.expiresAt ? formatDate(row.expiresAt) : '永久' }}</template></el-table-column>
          <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" inline-prompt active-text="活跃" inactive-text="禁用" @change="(v: boolean) => handleToggle(row, v)" /></template></el-table-column>
          <el-table-column label="日志" width="95" align="center"><template #default="{ row }"><el-switch v-model="row.logEnabled" size="small" inline-prompt active-text="记录" inactive-text="关闭" @change="(v: boolean) => handleToggleLog(row, v)" /></template></el-table-column>
          <el-table-column label="创建时间" width="155" sortable prop="createdAt"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
          <el-table-column label="操作" width="320" fixed="right" align="center">
            <template #default="{ row }">
              <el-button size="small" text type="primary" @click.stop="openUseKey(row)">使用密钥</el-button>
              <el-button size="small" text type="primary" @click.stop="openOpenCode(row)">配置 OpenCode</el-button>
              <el-button size="small" text type="warning" @click.stop="handleReset(row)">重置</el-button>
              <el-button size="small" text type="danger" @click.stop="handleDelete(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div v-if="total > 0" class="pagination-wrapper">
        <el-pagination
          v-model:current-page="currentPage"
          :page-size="pageSize"
          :total="total"
          :hide-on-single-page="false"
          layout="total, prev, pager, next"
          @current-change="loadKeys"
        />
      </div>
    </template>

    <KeyCreateDialog v-model="showCreateDialog" @success="onKeyCreated" />
    <KeyApplyDialog v-model="showApplyDialog" @submitted="onAppSubmitted" />
    <KeyAutoApproveDialog
      v-model="showAutoApproveDialog"
      :loading="autoApproveLoading"
      :pending-count="pendingCount"
      :result="autoApproveResult"
      @closed="autoApproveResult = null"
    />
    <UseKeyModal v-model="showUsageModal" :api-key-id="usageKeyId" :key-hint="usageKeyHint" />
    <OpenCodeConfigDialog v-model:visible="showOpenCode" :api-key-id="openCodeKeyId" :masked-key="openCodeMaskedKey" />

    <el-dialog v-model="showGroupDialog" title="管理我的模型" width="500px" destroy-on-close @opened="loadGroupDialogData">
      <div v-if="editGroupKey">
        <div class="group-section"><h4>当前可用模型</h4>
          <el-tag v-if="editGroupKey.accessAllGroups" size="large" type="success" closable @close="handleAccessAll(false)">全部模型</el-tag>
          <div v-else-if="editGroupKey.boundGroups?.length" class="bound-tags"><el-tag v-for="g in editGroupKey.boundGroups" :key="g.id" closable size="large" type="info" @close="removeGroup(g)">{{ g.name }}</el-tag></div>
          <span v-else class="text-muted">未分配</span>
        </div>
        <div class="group-section"><h4>添加模型</h4>
          <el-select v-model="addGroupIds" multiple filterable placeholder="选择模型" style="width:100%" :loading="groupLoading" @change="handleAddGroupChange">
            <el-option key="__all__" label="全部模型（不限制）" value="__all__" :disabled="editGroupKey.accessAllGroups" />
            <el-option v-for="g in availableAddGroups" :key="g.id" :label="g.name" :value="g.id" :disabled="editGroupKey.accessAllGroups" />
          </el-select>
        </div>
      </div>
      <template #footer><el-button @click="showGroupDialog=false">取消</el-button><el-button type="primary" :loading="groupSaving" @click="saveGroupChanges">保存</el-button></template>
    </el-dialog>

    <el-dialog v-model="showReject" title="驳回申请" width="420px">
      <p style="margin-bottom:8px;color:var(--el-text-color-secondary)">请填写驳回原因：</p>
      <el-input v-model="rejectReason" type="textarea" :rows="3" placeholder="请填写驳回原因..." maxlength="200" show-word-limit />
      <template #footer><el-button @click="showReject=false">取消</el-button><el-button type="danger" :disabled="!rejectReason.trim()" @click="confirmReject">确认驳回</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Key, Search, CopyDocument, Check } from '@element-plus/icons-vue'
import {
  listKeys, disableKey, enableKey, deleteKey, resetKey, revealKey,
  updateKeyGroups, getAvailableGroups, toggleAccessAllGroups, toggleLogEnabled,
  getKeyCreationPolicy, getMyKeyRequests, getMyKeyRequestSummary,
  decideKeyRequest, getPendingKeyRequests, getPendingKeyRequestCounts,
  getKeyRequestHistory, getDeptKeyCreationPolicy, setDeptKeyCreationPolicy, cancelKeyRequest,
} from '@/services/control-plane'
import { mapKeysResponse, mapCreateKeyResponse, type BoundGroupInfo } from '@/types/api-key'
import type { ApiKey, AvailableGroup } from '@/types/api-key'
import type { KeyRequest, KeyRequestAutoApproveResult } from '@/types/key-request'
import { KEY_REQUEST_STATUS_LABELS as STATUS_LABELS, KEY_REQUEST_STATUS_COLORS as STATUS_COLORS, formatKeyRequestBudget, keyRequestHasBudget } from '@/types/key-request'
import KeyCreateDialog from '@/components/key/KeyCreateDialog.vue'
import KeyApplyDialog from '@/components/key/KeyApplyDialog.vue'
import KeyAutoApproveDialog from '@/components/key/KeyAutoApproveDialog.vue'
import KeyRequestBudgetTag from '@/components/key/KeyRequestBudgetTag.vue'
import KeyRequestApproverCell from '@/components/key/KeyRequestApproverCell.vue'
import UseKeyModal from '@/components/key/UseKeyModal.vue'
import OpenCodeConfigDialog from '@/components/key/OpenCodeConfigDialog.vue'
import { copyToClipboard } from '@/utils/clipboard'
import { useSettingsStore } from '@/stores/settings'
import { useKeyCacheStore } from '@/stores/key-cache'
import { useAuthStore } from '@/stores/auth'

type KeyListTab = 'keys' | 'my-applications' | 'approval'
type ApprovalSubTab = 'pending' | 'history'

function parseTabFromQuery(query: LocationQuery): KeyListTab {
  const t = String(query.tab || '')
  if (t === 'my-applications' || t === 'approval' || t === 'keys') return t
  return 'keys'
}

function parseSubFromQuery(query: LocationQuery): ApprovalSubTab {
  return String(query.sub || '') === 'history' ? 'history' : 'pending'
}

function buildKeyListQuery(tab: KeyListTab, sub: ApprovalSubTab): Record<string, string> {
  const q: Record<string, string> = {}
  if (tab !== 'keys') q.tab = tab
  if (tab === 'approval' && sub === 'history') q.sub = 'history'
  return q
}

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${p(d.getMonth()+1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const keyCache = useKeyCacheStore()
const settingsStore = useSettingsStore()

/* ── Base URL 复制条 ── */
const urlCopied = ref(false)
let urlCopyTimer: ReturnType<typeof setTimeout> | null = null
const displayBaseUrl = computed(() => {
  const base = settingsStore.getBaseUrl()
  return base.endsWith('/v1') ? base : base.replace(/\/+$/, '') + '/v1'
})
function copyBaseUrl() {
  copyToClipboard(displayBaseUrl.value)
  urlCopied.value = true
  if (urlCopyTimer) clearTimeout(urlCopyTimer)
  urlCopyTimer = setTimeout(() => { urlCopied.value = false }, 2200)
}
const tabPrefsHydrated = ref(false)
let skipTabRouteSync = false
const activeTab = ref<KeyListTab>(parseTabFromQuery(route.query))
const loading = ref(false)
const keys = ref<ApiKey[]>([])
const currentPage = ref(1)
const total = ref(0)
const pageSize = 20
const showCreateDialog = ref(false)
const showApplyDialog = ref(false)
const requiresApproval = ref(false)
const showUsageModal = ref(false)
const usageKeyId = ref<string | null>(null)
const usageKeyHint = ref('')
const showOpenCode = ref(false)
const openCodeKeyId = ref('')
const openCodeMaskedKey = ref('')
const showGroupDialog = ref(false)
const editGroupKey = ref<ApiKey | null>(null)
const addGroupIds = ref<(string|number)[]>([])
const removedGroupIds = ref<number[]>([])
const availableAddGroups = ref<AvailableGroup[]>([])
const groupLoading = ref(false)
const groupSaving = ref(false)
const pendingAccessAll = ref<boolean|null>(null)
const myAppLoading = ref(false)
const myApplications = ref<KeyRequest[]>([])
const myAppPage = ref(1)
const myAppTotal = ref(0)
const myAppAllTotal = ref(0)
const myAppPageSize = 10
const myAppStatusFilter = ref('')
const myAppSummary = ref({ pending: 0, approved: 0, rejected: 0 })
const myPendingCount = computed(() => myAppSummary.value.pending)
const currentDeptId = ref('')
const deptPolicy = ref<'free'|'approval'>('free')
const policySaving = ref(false)
const approvalSub = ref<ApprovalSubTab>(parseSubFromQuery(route.query))
const pendingNameFilter = ref('')
const historyNameFilter = ref('')
const historyStatusFilter = ref<'approved' | 'rejected' | ''>('')
const pendingLoading = ref(false)
const pendingItems = ref<KeyRequest[]>([])
const pendingCount = ref(0)
const pendingBudgetCount = ref(0)
const pendingBudgetOnly = ref(false)
const pendingPage = ref(1)
const pendingTotal = ref(0)
const pendingPageSize = 10
const historyLoading = ref(false)
const historyItems = ref<KeyRequest[]>([])
const historyPage = ref(1)
const historyTotal = ref(0)
const historyAllTotal = ref(0)
const historyPageSize = 10
let historyLoadSeq = 0
let pendingLoadSeq = 0
let myAppLoadSeq = 0
const showReject = ref(false)
const rejectReason = ref('')
const rejectingItem = ref<KeyRequest|null>(null)

const showAutoApproveDialog = ref(false)
const autoApproveLoading = ref(false)
const autoApproveResult = ref<KeyRequestAutoApproveResult | null>(null)

// 普通成员：部门开审批时展示；负责人/管理员：若仍有历史或进行中的申请也展示
const showMyApplicationsTab = computed(() =>
  (requiresApproval.value && !authStore.isManager)
  || myAppTotal.value > 0
  || myAppSummary.value.pending > 0
  || myAppSummary.value.approved > 0
  || myAppSummary.value.rejected > 0,
)

// 部门已开审批，但负责人/管理员可直接创建，无需走申请
const needsApplyToCreate = computed(() => requiresApproval.value && !authStore.isManager)
const createKeyButtonLabel = computed(() => needsApplyToCreate.value ? '申请创建 Key' : '创建 Key')
const createFirstKeyButtonLabel = computed(() => needsApplyToCreate.value ? '申请创建第一个 Key' : '创建第一个 Key')

// ── 关键：角色未加载完时保守渲染多 Tab 分支 ──
// 避免：首帧 single-tab 分支渲染后 isManager 才变 true 触发重渲染导致「审批管理」缺失
const hasMultiTab = computed(() => {
  if (authStore.effectiveRole === null) return true
  return showMyApplicationsTab.value || authStore.isManager
})

const pendingDisplayTotal = computed(() => Math.max(pendingTotal.value, pendingCount.value))
const historyDisplayTotal = computed(() => historyTotal.value)

const canManageDeptPolicy = computed(() => authStore.effectiveRole === 'dept_admin' && !!currentDeptId.value)

const policyDeptName = computed(() => {
  if (authStore.user?.departmentName) return authStore.user.departmentName
  const fromPending = pendingItems.value.find(i => i.dept_name)?.dept_name
  if (fromPending) return fromPending
  const fromHistory = historyItems.value.find(i => i.dept_name)?.dept_name
  if (fromHistory) return fromHistory
  return currentDeptId.value || '未分配部门'
})

function resolvePolicyDeptId(): string {
  const profileDept = (authStore.user as any)?.departmentId || getJwtDeptId() || ''
  if (authStore.effectiveRole === 'dept_admin') {
    const roleDept = authStore.orgRoles.find(r => r.role === 'dept_admin')?.scope_id
    return roleDept || profileDept
  }
  return profileDept
}

function syncTabToRoute(tab: KeyListTab, sub: ApprovalSubTab) {
  if (
    parseTabFromQuery(route.query) === tab
    && parseSubFromQuery(route.query) === sub
  ) return
  skipTabRouteSync = true
  void router.replace({ path: route.path, query: buildKeyListQuery(tab, sub) }).finally(() => {
    skipTabRouteSync = false
  })
}

function applyRouteToTabs() {
  const tab = parseTabFromQuery(route.query)
  const sub = parseSubFromQuery(route.query)
  if (tab === activeTab.value && sub === approvalSub.value) return
  skipTabRouteSync = true
  activeTab.value = tab
  approvalSub.value = sub
  nextTick(() => { skipTabRouteSync = false })
}

function reconcileActiveTab() {
  let tab = activeTab.value
  if (tab === 'approval' && !authStore.isManager) tab = 'keys'
  if (tab === 'my-applications' && !showMyApplicationsTab.value) tab = 'keys'
  if (tab !== activeTab.value) activeTab.value = tab
  syncTabToRoute(activeTab.value, approvalSub.value)
}

function loadActiveTabData() {
  if (activeTab.value === 'keys') loadKeys()
  else if (activeTab.value === 'my-applications') loadMyApplications()
  else if (activeTab.value === 'approval') {
    if (approvalSub.value === 'history') {
      void Promise.all([refreshPendingCounts(), loadHistory()])
    } else {
      loadPending()
    }
  }
}

function applyPendingCounts(counts: { count: number; budgetCount: number }) {
  pendingCount.value = Number(counts.count) || 0
  pendingBudgetCount.value = Number(counts.budgetCount) || 0
  if (pendingBudgetOnly.value && pendingBudgetCount.value === 0) {
    pendingBudgetOnly.value = false
  }
}

async function refreshPendingCounts() {
  if (!authStore.isManager) return
  try {
    applyPendingCounts(await getPendingKeyRequestCounts())
  } catch {
    pendingCount.value = 0
    pendingBudgetCount.value = 0
  }
}

watch(showMyApplicationsTab, (v) => {
  if (!tabPrefsHydrated.value) return
  if (!v && activeTab.value === 'my-applications') {
    activeTab.value = 'keys'
  }
})

watch(activeTab, (tab) => {
  if (!skipTabRouteSync) syncTabToRoute(tab, approvalSub.value)
  if (!tabPrefsHydrated.value) return
  if (tab === 'keys') loadKeys()
  else if (tab === 'my-applications') loadMyApplications()
  else if (tab === 'approval') {
    if (approvalSub.value === 'history') {
      void Promise.all([refreshPendingCounts(), loadHistory()])
    } else {
      loadPending()
    }
  }
})

watch(approvalSub, (sub) => {
  if (!skipTabRouteSync && activeTab.value === 'approval') {
    syncTabToRoute(activeTab.value, sub)
  }
  if (!tabPrefsHydrated.value || activeTab.value !== 'approval') return
  if (sub === 'history') {
    void Promise.all([refreshPendingCounts(), loadHistory(1)])
  } else {
    loadPending(1)
  }
})

watch(myAppStatusFilter, () => {
  if (!tabPrefsHydrated.value || activeTab.value !== 'my-applications') return
  myAppPage.value = 1
  loadMyApplications()
})

watch(historyStatusFilter, () => {
  if (!tabPrefsHydrated.value || activeTab.value !== 'approval' || approvalSub.value !== 'history') return
  historyPage.value = 1
  loadHistory()
})

let pendingNameDebounce: ReturnType<typeof setTimeout> | null = null
watch(pendingNameFilter, () => {
  if (!tabPrefsHydrated.value || activeTab.value !== 'approval' || approvalSub.value !== 'pending') return
  if (pendingNameDebounce) clearTimeout(pendingNameDebounce)
  pendingNameDebounce = setTimeout(() => triggerPendingSearch(), 350)
})

let historyNameDebounce: ReturnType<typeof setTimeout> | null = null
watch(historyNameFilter, () => {
  if (!tabPrefsHydrated.value || activeTab.value !== 'approval' || approvalSub.value !== 'history') return
  if (historyNameDebounce) clearTimeout(historyNameDebounce)
  historyNameDebounce = setTimeout(() => triggerHistorySearch(), 350)
})

watch(
  () => [route.query.tab, route.query.sub] as const,
  () => {
    if (skipTabRouteSync) return
    const tab = parseTabFromQuery(route.query)
    const sub = parseSubFromQuery(route.query)
    if (tab === activeTab.value && sub === approvalSub.value) return
    applyRouteToTabs()
    if (!tabPrefsHydrated.value) return
    loadActiveTabData()
  },
)

function avatarColor(n?: string): string {
  const c = ['#409EFF','#67C23A','#E6A23C','#F56C6C','#909399','#8B5CF6','#EC4899','#14B8A6']
  if (!n) return c[0]; let h = 0; for (let i = 0; i < n.length; i++) h = n.charCodeAt(i) + ((h << 5) - h)
  return c[Math.abs(h) % c.length]
}

function resolvePageArg(page: number | undefined, pageRef: { value: number }): number {
  if (typeof page === 'number' && Number.isFinite(page) && page >= 1) {
    pageRef.value = page
  }
  return pageRef.value
}

async function loadKeys(page?: number) {
  const p = resolvePageArg(page, currentPage)
  loading.value = true
  try {
    const r = await listKeys(p, pageSize)
    keys.value = mapKeysResponse(r)
    total.value = Number(r.data.total) || 0
  } catch {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}
async function copyFullKey(row: ApiKey) { try { const r = await revealKey(row.id); await copyToClipboard(r.key); ElMessage.success('已复制') } catch { ElMessage.warning('获取失败') } }
function openUseKey(row: ApiKey) { usageKeyId.value = row.id; usageKeyHint.value = row.keyHint; showUsageModal.value = true }
function openOpenCode(row: ApiKey) { openCodeKeyId.value = row.id; openCodeMaskedKey.value = row.maskedKey; showOpenCode.value = true }
async function handleToggle(row: ApiKey, en: boolean) { try { if (en) await enableKey(row.id); else { await ElMessageBox.confirm('确定禁用「'+row.name+'」？','确认',{type:'warning'}); await disableKey(row.id) } ElMessage.success(en?'已启用':'已禁用'); await loadKeys() } catch { row.enabled = !en } }
async function handleToggleLog(row: ApiKey, v: boolean) { try { await toggleLogEnabled(row.id, v); ElMessage.success(v?'已开启日志':'已关闭日志') } catch { row.logEnabled = !v; ElMessage.error('操作失败') } }
async function handleReset(row: ApiKey) { try { await ElMessageBox.confirm('重置「'+row.name+'」，旧密钥立即失效。确定？','确认',{type:'warning'}); const r = await resetKey(row.id); const m = mapCreateKeyResponse(r); keyCache.cacheKey(m.id,m.key); await ElMessageBox.alert('<pre style="background:var(--el-fill-color-light);padding:10px;border-radius:6px;font-size:13px;white-space:pre-wrap;word-break:break-all">'+m.key+'</pre>','新密钥',{dangerouslyUseHTMLString:true,confirmButtonText:'关闭'}); await loadKeys() } catch {} }
async function handleDelete(row: ApiKey) { try { await ElMessageBox.confirm('删除「'+row.name+'」不可恢复，确定？','确认',{type:'error'}); await deleteKey(row.id); keyCache.clearKey(row.id); ElMessage.success('已删除'); await loadKeys() } catch {} }
function onKeyCreated() { showCreateDialog.value = false; currentPage.value = 1; loadKeys() }

function openGroupDialog(row: ApiKey) { editGroupKey.value = { ...row, boundGroups: [...(row.boundGroups||[])] }; addGroupIds.value = []; removedGroupIds.value = []; pendingAccessAll.value = null; showGroupDialog.value = true }
async function loadGroupDialogData() { groupLoading.value = true; try { const r = await getAvailableGroups(); const all = r.data.groups||[]; const bound = new Set((editGroupKey.value?.boundGroups||[]).map((g:BoundGroupInfo)=>g.id)); availableAddGroups.value = all.filter((g:AvailableGroup)=>!bound.has(g.id)) } catch { ElMessage.error('加载失败') } finally { groupLoading.value = false } }
function removeGroup(g: BoundGroupInfo) { if(!editGroupKey.value) return; editGroupKey.value.boundGroups = (editGroupKey.value.boundGroups||[]).filter(x=>x.id!==g.id); removedGroupIds.value.push(g.id); addGroupIds.value = addGroupIds.value.filter(id=>id!==g.id) }
function handleAddGroupChange(vals: (string|number)[]) { if(vals.includes('__all__')) { addGroupIds.value = []; pendingAccessAll.value = true; if(editGroupKey.value){ for(const g of editGroupKey.value.boundGroups||[]) removedGroupIds.value.push(g.id); editGroupKey.value.boundGroups = []; editGroupKey.value.accessAllGroups = true } } }
function handleAccessAll(v: boolean) { if(!editGroupKey.value) return; pendingAccessAll.value = v; editGroupKey.value.accessAllGroups = v }
async function saveGroupChanges() { if(!editGroupKey.value) return; groupSaving.value = true; try { if(pendingAccessAll.value!==null) await toggleAccessAllGroups(editGroupKey.value.id,pendingAccessAll.value); const add = addGroupIds.value.filter((v):v is number=>typeof v==='number'); const remove = removedGroupIds.value; if(add.length>0||remove.length>0) await updateKeyGroups(editGroupKey.value.id,add,remove); ElMessage.success('已更新'); showGroupDialog.value = false; await loadKeys() } catch(e:any){ ElMessage.error(e?.response?.data?.message||'更新失败') } finally { groupSaving.value = false } }

async function handleCreateClick() { if (authStore.isManager) { showCreateDialog.value = true; return }; try { const p = await getKeyCreationPolicy(); if(p.requires_approval) showApplyDialog.value = true; else showCreateDialog.value = true } catch { showCreateDialog.value = true } }

async function loadMyApplications(page?: number) {
  const p = resolvePageArg(page, myAppPage)
  const seq = ++myAppLoadSeq
  myAppLoading.value = true
  try {
    const r = await getMyKeyRequests({
      page: p,
      pageSize: myAppPageSize,
      status: myAppStatusFilter.value || undefined,
    })
    if (seq !== myAppLoadSeq) return
    myApplications.value = r.items
    myAppTotal.value = Number(r.total) || 0
    if (!myAppStatusFilter.value) {
      myAppAllTotal.value = myAppTotal.value
    }
  } catch {
    if (seq !== myAppLoadSeq) return
    myApplications.value = []
    myAppTotal.value = 0
    if (!myAppStatusFilter.value) {
      myAppAllTotal.value = 0
    }
  } finally {
    if (seq === myAppLoadSeq) {
      myAppLoading.value = false
    }
  }
}
async function loadRequestSummary() {
  try {
    const s = await getMyKeyRequestSummary()
    myAppSummary.value = {
      pending: s.pending || 0,
      approved: s.approved || 0,
      rejected: s.rejected || 0,
    }
  } catch {
    myAppSummary.value = { pending: 0, approved: 0, rejected: 0 }
  }
}
function triggerPendingSearch() {
  pendingPage.value = 1
  loadPending()
}
function togglePendingBudgetOnly() {
  if (pendingBudgetCount.value <= 0) return
  pendingBudgetOnly.value = !pendingBudgetOnly.value
  pendingPage.value = 1
  loadPending()
}
function triggerHistorySearch() {
  historyPage.value = 1
  loadHistory()
}
async function handleCancelApp(id: string) {
  try {
    await cancelKeyRequest(id)
    ElMessage.success('已取消申请')
    await Promise.all([loadMyApplications(), loadRequestSummary()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '取消失败')
  }
}
function onAppSubmitted() {
  showApplyDialog.value = false
  myAppPage.value = 1
  loadMyApplications()
  loadRequestSummary()
}

function getJwtDeptId(): string { try { const t = localStorage.getItem('access_token'); if(!t) return ''; const p = t.split('.')[1].replace(/-/g,'+').replace(/_/g,'/'); return JSON.parse(decodeURIComponent(atob(p).split('').map(c=>'%'+('00'+c.charCodeAt(0).toString(16)).slice(-2)).join(''))).dept_id||'' } catch { return '' } }
async function loadPolicy() {
  await authStore.fetchOrgRoles().catch(() => {})
  const id = resolvePolicyDeptId()
  currentDeptId.value = id
  if (!id) return
  try {
    const r = await getDeptKeyCreationPolicy(id)
    deptPolicy.value = (r.key_creation_policy as 'free' | 'approval') || 'free'
  } catch {
    deptPolicy.value = 'free'
  }
}
async function loadRequiresApproval() {
  try {
    const p = await getKeyCreationPolicy()
    // policy 反映部门策略；requires_approval 反映当前用户是否需申请（负责人为 false）
    requiresApproval.value = p.policy === 'approval' || p.requires_approval
  } catch {}
}
async function refreshAfterPolicyChange() {
  await Promise.all([
    loadPending(),
    loadHistory(),
    loadKeys(),
    loadMyApplications(),
    loadRequestSummary(),
    loadRequiresApproval(),
  ])
}

async function confirmDeptKeyPolicyChange(enabling: boolean, pending: number): Promise<boolean> {
  const title = enabling ? '开启部门 Key 审批' : '关闭部门 Key 审批'
  let message = enabling
    ? '开启后，部门成员创建 Key 需先提交申请，由部门负责人审批通过后方可生成 Key。'
    : '关闭后，部门成员可直接创建 Key，无需审批。'
  if (!enabling && pending > 0) {
    message += ` 当前有 ${pending} 条待审批申请，关闭后将自动通过并生成 Key。`
  }
  try {
    await ElMessageBox.confirm(message, title, {
      type: enabling ? 'info' : 'warning',
      confirmButtonText: enabling ? '确认开启' : '确认关闭',
      cancelButtonText: '取消',
    })
    return true
  } catch {
    return false
  }
}

async function onPolicySwitchChange(enabled: boolean) {
  if (!currentDeptId.value) return
  const nv: 'free' | 'approval' = enabled ? 'approval' : 'free'
  if (nv === deptPolicy.value) return
  const ok = await confirmDeptKeyPolicyChange(enabled, pendingCount.value)
  if (!ok) return
  await applyDeptPolicy(nv)
}

async function applyDeptPolicy(nv: 'free' | 'approval') {
  if (!currentDeptId.value) return
  const prev = deptPolicy.value
  const isDisabling = nv === 'free'
  deptPolicy.value = nv
  policySaving.value = true
  if (isDisabling && pendingCount.value > 0) {
    showAutoApproveDialog.value = true
    autoApproveLoading.value = true
    autoApproveResult.value = null
  }
  try {
    const res = await setDeptKeyCreationPolicy(currentDeptId.value, nv)
    requiresApproval.value = nv === 'approval'
    if (res.auto_approve && res.auto_approve.total > 0) {
      autoApproveResult.value = res.auto_approve
      await refreshAfterPolicyChange()
    } else {
      showAutoApproveDialog.value = false
      ElMessage.success(isDisabling ? '已关闭部门 Key 审批' : '已开启部门 Key 审批')
      if (authStore.isManager) await loadPending()
    }
  } catch (e: any) {
    deptPolicy.value = prev
    showAutoApproveDialog.value = false
    autoApproveResult.value = null
    ElMessage.error(e?.response?.data?.error || '更新失败')
  } finally {
    policySaving.value = false
    autoApproveLoading.value = false
  }
}

async function loadPending(page?: number) {
  const p = resolvePageArg(page, pendingPage)
  const seq = ++pendingLoadSeq
  pendingLoading.value = true
  try {
    const [r, counts] = await Promise.all([
      getPendingKeyRequests({
        page: p,
        pageSize: pendingPageSize,
        name: pendingNameFilter.value.trim() || undefined,
        hasBudget: pendingBudgetOnly.value || undefined,
      }),
      getPendingKeyRequestCounts(),
    ])
    if (seq !== pendingLoadSeq) return
    pendingItems.value = r.items || []
    pendingTotal.value = Number(r.total) || 0
    applyPendingCounts(counts)
  } catch {
    if (seq !== pendingLoadSeq) return
    pendingItems.value = []
    pendingTotal.value = 0
    pendingCount.value = 0
    pendingBudgetCount.value = 0
  } finally {
    if (seq === pendingLoadSeq) {
      pendingLoading.value = false
    }
  }
}
async function loadHistory(page?: number) {
  const p = resolvePageArg(page, historyPage)
  const seq = ++historyLoadSeq
  historyLoading.value = true
  try {
    const r = await getKeyRequestHistory({
      page: p,
      pageSize: historyPageSize,
      name: historyNameFilter.value.trim() || undefined,
      status: historyStatusFilter.value || undefined,
    })
    if (seq !== historyLoadSeq) return
    historyItems.value = r.items || []
    historyTotal.value = Number(r.total) || 0
    if (!historyNameFilter.value.trim() && !historyStatusFilter.value) {
      historyAllTotal.value = historyTotal.value
    }
  } catch {
    if (seq !== historyLoadSeq) return
    historyItems.value = []
    historyTotal.value = 0
    if (!historyNameFilter.value.trim() && !historyStatusFilter.value) {
      historyAllTotal.value = 0
    }
  } finally {
    if (seq === historyLoadSeq) {
      historyLoading.value = false
    }
  }
}
function approveConfirmTitle(item: KeyRequest): string {
  if (keyRequestHasBudget(item.budget_monthly)) {
    return `确认通过？将自动创建 Key，并配置个人月预算 ${formatKeyRequestBudget(item.budget_monthly)}。`
  }
  return '确认通过？将自动创建 Key（本次未申请预算）。'
}
async function handleApprove(id: string) { try { await decideKeyRequest(id,'approve'); ElMessage.success('已通过'); await Promise.all([loadPending(),loadHistory()]) } catch(e:any){ ElMessage.error(e?.response?.data?.error||'操作失败') } }
function openReject(item: KeyRequest) { rejectingItem.value = item; rejectReason.value = ''; showReject.value = true }
async function confirmReject() { if(!rejectingItem.value||!rejectReason.value.trim()) return; try { await decideKeyRequest(rejectingItem.value.id,'reject',rejectReason.value.trim()); ElMessage.success('已驳回'); showReject.value = false; await Promise.all([loadPending(),loadHistory()]) } catch(e:any){ ElMessage.error(e?.response?.data?.error||'操作失败') } finally { rejectingItem.value = null } }

onMounted(async () => {
  useSettingsStore().loadPublicSettings()
  await Promise.all([loadRequiresApproval(), loadRequestSummary(), loadMyApplications()])
  if (!authStore.effectiveRole) {
    await authStore.fetchOrgRoles().catch(() => {})
  }
  reconcileActiveTab()
  tabPrefsHydrated.value = true
  if (authStore.isManager) {
    void refreshPendingCounts()
  }
  loadActiveTabData()
  loadPolicy()
})
watch(() => authStore.isManager, (v) => { if (v) refreshPendingCounts() })
watch(() => authStore.effectiveRole, (role) => {
  if (!role || !['admin', 'studio_admin', 'dept_admin'].includes(role)) return
  if (role === 'dept_admin') loadPolicy()
  if (activeTab.value === 'approval') {
    if (approvalSub.value === 'pending') loadPending()
    else void Promise.all([refreshPendingCounts(), loadHistory()])
  } else if (authStore.isManager) {
    refreshPendingCounts()
  }
})
</script>

<style scoped>
.key-list-page { padding: 0; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.page-title { font-size: 22px; font-weight: 700; margin: 0; }
.page-desc { font-size: 13px; color: var(--el-text-color-secondary); margin: 4px 0 0; }
.header-actions { flex-shrink: 0; }
.key-cell { display: flex; align-items: center; gap: 6px; }
.key-masked { font-family: 'SF Mono',monospace; font-size: 13px; background: var(--el-fill-color-light); padding: 2px 10px; border-radius: 4px; color: var(--el-text-color-secondary); }
.text-muted { color: var(--el-text-color-secondary); font-size: 13px; }
.pagination-wrapper { display: flex; justify-content: flex-end; margin-top: 16px; }
.bound-tags { min-height: 32px; }
.group-section { margin-bottom: 16px; }
.group-section h4 { margin: 0 0 8px; font-size: 14px; }
.key-table-wrap { width: 100%; }
.app-list { display: flex; flex-direction: column; gap: 12px; margin-top: 8px; }
.app-card { border: 1px solid var(--el-border-color-lighter); border-radius: 8px; padding: 16px 20px; background: var(--el-bg-color); }
.app-card:hover { border-color: var(--el-border-color); }
.app-card--approved { border-left: 3px solid #10b981; }
.app-card--rejected { border-left: 3px solid #ef4444; }
.app-card--pending  { border-left: 3px solid #f59e0b; }
.app-card--cancelled { border-left: 3px solid #6b7280; }
.app-card-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.app-key-name { font-weight: 600; font-size: 15px; }
.applicant-info { display: flex; align-items: center; gap: 10px; }
.applicant-name { font-weight: 500; }
.app-card-body { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
.info-row { display: flex; gap: 6px; }
.info-label { color: var(--el-text-color-secondary); flex-shrink: 0; min-width: 60px; }
.app-comment { background: var(--el-fill-color-lighter); padding: 2px 6px; border-radius: 4px; font-size: 12px; }
.app-card-foot { flex-shrink: 0; }

/* ── 策略卡片 ── */
.policy-card {
  margin-bottom: 20px;
  padding: 20px 24px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 12px;
  background: var(--el-bg-color);
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}
.policy-card--on {
  border-color: var(--el-color-primary-light-5);
  background: linear-gradient(
    135deg,
    var(--el-color-primary-light-9) 0%,
    var(--el-bg-color) 100%
  );
  box-shadow: 0 2px 8px color-mix(in srgb, var(--el-color-primary) 8%, transparent);
}
.policy-card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.policy-card-info {
  display: flex;
  align-items: center;
  gap: 14px;
  flex: 1;
  min-width: 0;
}
.policy-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  transition: background 0.2s ease, color 0.2s ease;
}
.policy-icon--on {
  background: var(--el-color-primary);
  color: var(--el-color-white);
}
.policy-title {
  font-size: 15px;
  font-weight: 600;
  margin: 0 0 4px 0;
  color: var(--el-text-color-primary);
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.policy-sub {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0;
}
.policy-dept-label {
  color: var(--el-text-color-secondary);
}
.policy-sub b {
  color: var(--el-text-color-primary);
  font-weight: 500;
}
.policy-hint {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
  margin: 14px 0 0 0;
  padding-top: 14px;
  border-top: 1px dashed var(--el-border-color-lighter);
}
.policy-hint .el-icon {
  flex-shrink: 0;
  margin-top: 3px;
  color: var(--el-color-info);
}

.approval-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.policy-card--compact {
  margin-bottom: 0;
  padding: 16px 20px;
}

.approval-role-alert {
  margin-bottom: 0;
}

.approval-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.approval-summary--three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.approval-stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-blank);
}

.approval-stat__value {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.2;
  color: var(--el-text-color-primary);
}

.approval-stat__label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.approval-stat--warn .approval-stat__value {
  color: var(--el-color-warning);
}

.approval-stat--budget {
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.approval-stat--budget:hover {
  border-color: var(--el-color-primary-light-5);
}

.approval-stat--budget.is-active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.approval-stat--budget:not(.is-disabled) .approval-stat__value {
  color: var(--el-color-primary);
}

.approval-stat--budget.is-disabled {
  cursor: default;
}

.approval-stat--budget.is-disabled:hover {
  border-color: var(--el-border-color-lighter);
}

.approval-stat--budget.is-disabled .approval-stat__value {
  color: var(--el-text-color-secondary);
}

.pending-budget-tag {
  margin-right: 4px;
}

.approval-sub {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
}

.approval-table {
  width: 100%;
}

.approval-table :deep(.el-table__header th) {
  background: var(--el-fill-color-light);
  font-weight: 600;
}

.approval-table-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
}

.approval-pagination {
  margin-top: 0;
}

@media (max-width: 768px) {
  .approval-summary {
    grid-template-columns: 1fr;
  }
}

.my-app-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.key-request-filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}

.my-app-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.my-app-stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-blank);
}

.my-app-stat__value {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.2;
  color: var(--el-text-color-primary);
}

.my-app-stat__label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.my-app-stat--success .my-app-stat__value { color: var(--el-color-success); }
.my-app-stat--danger .my-app-stat__value { color: var(--el-color-danger); }
.my-app-stat--muted .my-app-stat__value { color: var(--el-text-color-regular); }

.my-app-table {
  width: 100%;
}

.my-app-table :deep(.el-table__header th) {
  background: var(--el-fill-color-light);
  font-weight: 600;
}

@media (max-width: 768px) {
  .my-app-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.budget-text {
  color: var(--el-color-primary);
  font-weight: 500;
}

.time-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.comment-text {
  font-size: 12px;
  color: var(--el-text-color-regular);
  line-height: 1.4;
}

/* ── Base URL inline 组件 ── */
.base-url-inline {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 6px 12px;
  background: #f5f7fa;
  border-radius: 6px;
  font-size: 13px;
}
.base-url-inline__label {
  color: #909399;
  font-weight: 500;
}
.base-url-inline__value {
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  color: #303133;
  font-size: 13px;
  user-select: all;
}
.base-url-inline__btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  margin-left: 4px;
  background: #36B34E;
  border: none;
  border-radius: 4px;
  color: #fff;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  &:hover { background: #229A94; }
  &.ok { background: #0EA371; }
  .el-icon { font-size: 13px; }
}

:global(html.dark) .base-url-inline {
  background: #1a1a1a;
  .base-url-inline__label { color: #a3a3a3; }
  .base-url-inline__value { color: #e5e5e5; }
}
</style>
