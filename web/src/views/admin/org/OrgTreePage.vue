<template>
  <div class="org-layout">

    <!-- ═══ 左侧：组织树 ═══════════════════════════════════════════════════ -->
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
          placeholder="搜索工作室 / 部门"
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
                @click.stop="onAddLeader(data)"
              >+</el-button>
            </span>
          </template>
        </el-tree>

        <!-- 待归属伪节点 -->
        <div
          v-if="unmappedDepts.length > 0"
          :class="['unmapped-node', { 'is-current': selectedType === 'unmapped' }]"
          @click="selectUnmapped"
        >
          <el-icon class="node-icon" color="#e6a23c"><Warning /></el-icon>
          <span class="node-label">待归属部门</span>
          <el-badge :value="unmappedDepts.length" type="warning" class="node-count" />
        </div>
      </el-scrollbar>
    </aside>

    <!-- ═══ 右侧：内容面板 ══════════════════════════════════════════════════ -->
    <main class="content-panel">

      <!-- ─── 工作室视图 ─────────────────────────────────────────────────── -->
      <template v-if="selectedType === 'studio' && selectedStudio">
        <div class="panel-header">
          <div class="panel-info">
            <div class="panel-breadcrumb">公司 <el-icon class="crumb-arrow"><ArrowRight /></el-icon> {{ selectedStudio.name }}</div>
            <div class="panel-title">
              <el-icon class="title-icon"><OfficeBuilding /></el-icon>
              {{ selectedStudio.name }}
            </div>
          </div>
          <div class="panel-actions">
            <!-- 指定负责人：admin 或当前工作室的 studio_admin -->
            <el-button
              v-if="isGlobalAdmin || myStudioIds.includes(selectedStudio?.id ?? '')"
              size="small" @click="openGrantDialog('studio_admin')"
            >指定负责人</el-button>
          </div>
        </div>

        <el-tabs v-model="studioActiveTab" class="panel-tabs">
          <!-- Tab：部门列表 -->
          <el-tab-pane name="depts">
            <template #label>
              部门列表
              <el-badge :value="selectedStudioDepts.length" :max="99" type="info" class="tab-badge" />
            </template>
            <div class="tab-content">
              <div class="org-table-wrap"><el-table :data="selectedStudioDepts" stripe size="small">
                <el-table-column label="部门名称" min-width="160">
                  <template #default="{ row }">
                    <span class="clickable-dept" @click="clickDeptFromStudio(row)">{{ row.dept_name }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="部门ID" min-width="120">
                  <template #default="{ row }">
                    <code class="dept-id-code">{{ row.dept_id }}</code>
                  </template>
                </el-table-column>
                <el-table-column label="状态" width="90">
                  <template #default="{ row }">
                    <el-tag :type="row.status === 'active' ? 'success' : 'warning'" size="small">
                      {{ row.status === 'active' ? '正常' : '待归属' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="部门负责人" min-width="140">
                  <template #default="{ row }">
                    <span v-if="getDeptOwners(row.dept_id).length">
                      <el-tag
                        v-for="o in getDeptOwners(row.dept_id)"
                        :key="o.id"
                        size="small"
                        class="owner-chip"
                        style="margin-right:4px"
                      >{{ o.user_name || o.user_id }}</el-tag>
                    </span>
                    <span v-else class="text-muted">未设置</span>
                  </template>
                </el-table-column>
              </el-table></div>
              <div v-if="selectedStudioDepts.length === 0" class="tab-empty">
                该工作室暂无部门
              </div>
            </div>
          </el-tab-pane>

          <!-- Tab：工作室负责人 -->
          <el-tab-pane name="owner">
            <template #label>
              负责人
              <el-badge v-if="selectedStudioOwners.length" :value="selectedStudioOwners.length" type="primary" class="tab-badge" />
            </template>
            <div class="tab-content">
              <div v-if="selectedStudioOwners.length" class="owners-list">
                <div v-for="owner in selectedStudioOwners" :key="owner.id" class="owner-card">
                  <div class="owner-avatar">{{ owner.user_name?.charAt(0) || '?' }}</div>
                  <div class="owner-info">
                    <div class="owner-name">{{ owner.user_name }}</div>
                    <div class="owner-email">{{ owner.email }}</div>
                    <div class="owner-since">授权于 {{ formatDate(owner.created_at) }}</div>
                  </div>
                  <el-popconfirm
                    v-if="isGlobalAdmin || myStudioIds.includes(selectedStudio?.id ?? '')"
                    :title="`确认卸任「${owner.user_name}」的工作室负责人？`"
                    confirm-button-text="确认卸任"
                    confirm-button-type="danger"
                    @confirm="doRevoke(owner)"
                  >
                    <template #reference>
                      <el-button size="small" type="danger" plain>卸任</el-button>
                    </template>
                  </el-popconfirm>
                </div>
              </div>
              <div v-else class="owner-empty">
                <el-icon size="32" color="#c0c4cc"><UserFilled /></el-icon>
                <p>该工作室尚未指定负责人</p>
                <el-button
                  v-if="isGlobalAdmin || myStudioIds.includes(selectedStudio?.id ?? '')"
                  size="small" type="primary" plain @click="openGrantDialog('studio_admin')"
                >立即指定</el-button>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>

      <!-- ─── 部门视图 ───────────────────────────────────────────────────── -->
      <template v-else-if="selectedType === 'dept' && selectedDept">
        <div class="panel-header">
          <div class="panel-info">
            <div class="panel-breadcrumb">
              公司 <el-icon class="crumb-arrow"><ArrowRight /></el-icon>
              <!-- dept_admin 无法进入工作室视图，面包屑工作室名退化为纯文字 -->
              <span
                v-if="selectedDeptParentStudio"
                :class="(isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio.id)) ? 'crumb-link' : 'crumb-text'"
                @click="(isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio.id)) && clickStudioFromDept()"
              >{{ selectedDeptParentStudio.name }}</span>
              <el-icon v-if="selectedDeptParentStudio" class="crumb-arrow"><ArrowRight /></el-icon>
              {{ selectedDept.dept_name }}
            </div>
            <div class="panel-title">
              <el-icon class="title-icon"><Folder /></el-icon>
              {{ selectedDept.dept_name }}
              <el-tag :type="selectedDept.status === 'active' ? 'success' : 'warning'" size="small" class="title-badge">
                {{ selectedDept.status === 'active' ? '正常' : '待归属' }}
              </el-tag>
            </div>
          </div>
          <div class="panel-actions">
            <!-- 指定部门负责人：admin 或父工作室 studio_admin -->
            <el-button
              v-if="isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio?.id ?? '')"
              size="small" @click="openGrantDialog('dept_admin')"
            >指定负责人</el-button>
          </div>
        </div>

        <el-tabs v-model="deptActiveTab" class="panel-tabs">
          <!-- Tab：成员列表 -->
          <el-tab-pane name="members">
            <template #label>
              成员
              <span v-if="deptMembersTotal > 0" class="tab-count">{{ deptMembersTotal }}</span>
            </template>
            <div class="tab-content">
              <div class="members-toolbar">
                <el-input
                  v-model="memberSearch"
                  placeholder="搜索成员姓名或邮箱..."
                  clearable
                  size="small"
                  style="width: 260px"
                  :prefix-icon="Search"
                  @input="onMemberSearch"
                />
              </div>
              <div class="org-table-wrap"><el-table :data="filteredMembers" stripe size="small" v-loading="deptMembersLoading"
                style="cursor:pointer" @row-click="openMemberDetail">
                <el-table-column label="姓名" min-width="130">
                  <template #default="{ row }">
                    <div class="member-cell">
                      <el-badge
                        :is-dot="row.has_conflict"
                        :offset="[-2, 2]"
                        type="danger"
                      >
                        <div class="member-avatar">{{ row.name?.charAt(0) || '?' }}</div>
                      </el-badge>
                      <span class="member-name">{{ row.name }}</span>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="工号" width="88" prop="employee_id">
                  <template #default="{ row }">
                    <span v-if="row.employee_id" class="text-mono">{{ row.employee_id }}</span>
                    <span v-else class="text-muted">—</span>
                  </template>
                </el-table-column>
                <el-table-column label="岗位" min-width="110" prop="job_title">
                  <template #default="{ row }">
                    <span v-if="row.job_title">{{ row.job_title }}</span>
                    <span v-else class="text-muted">—</span>
                  </template>
                </el-table-column>
                <el-table-column label="邮箱" min-width="170" prop="email" />
                <el-table-column label="归属来源" width="110">
                  <template #default="{ row }">
                    <el-tag v-if="row.login_status === 'pending'" type="warning" size="small">未登录</el-tag>
                    <el-tag
                      v-else
                      :type="row.dept_source === 'manual' ? 'warning' : 'info'"
                      size="small"
                      :title="row.dept_source === 'manual' ? '手动指定，不受 SSO 影响' : '跟随 SSO 自动归集'"
                    >{{ row.dept_source === 'manual' ? '手动' : 'SSO' }}</el-tag>
                    <el-tooltip
                      v-if="row.has_conflict"
                      content="SSO 部门与手动归属不一致，建议复核"
                      placement="top"
                    >
                      <el-icon color="#f56c6c" style="margin-left:4px;cursor:pointer"><Warning /></el-icon>
                    </el-tooltip>
                  </template>
                </el-table-column>
                <el-table-column label="归集时间" width="100" prop="joined_at" />
                <el-table-column label="配额" width="150" align="center">
                  <template #default="{ row }">
                    <span v-if="userQuotaMap[row.id]" class="quota-cell">
                      <template v-if="userQuotaMap[row.id]!.source === 'override'">
                        {{ userQuotaMap[row.id]!.rpm === 0 ? '不限' : userQuotaMap[row.id]!.rpm }} RPM /
                        {{ userQuotaMap[row.id]!.max_concurrency === 0 ? '不限' : userQuotaMap[row.id]!.max_concurrency }} 并发
                      </template>
                      <span v-else class="text-placeholder">继承默认</span>
                    </span>
                    <span v-else class="text-placeholder">—</span>
                  </template>
                </el-table-column>
                <el-table-column
                  v-if="isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio?.id ?? '')"
                  label="操作" width="90" align="center"
                >
                  <template #default="{ row }">
                    <el-button
                      link type="primary" size="small"
                      @click.stop="openUserQuotaDialog(row)"
                    >配额</el-button>
                  </template>
                </el-table-column>
              </el-table></div>
              <div v-if="!deptMembersLoading && filteredMembers.length === 0" class="tab-empty">
                {{ memberSearch ? '未找到匹配成员' : '该部门暂无成员（员工首次登录后自动归集）' }}
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
            </div>
          </el-tab-pane>

          <!-- Tab：部门信息 -->
          <el-tab-pane label="基本信息" name="info">
            <div class="tab-content dept-info-grid">
              <div class="info-row">
                <span class="info-label">部门 ID</span>
                <code class="dept-id-code">{{ selectedDept.dept_id }}</code>
              </div>
              <div class="info-row">
                <span class="info-label">所属工作室</span>
                <span v-if="selectedDeptParentStudio">{{ selectedDeptParentStudio.name }}</span>
                <el-tag v-else type="warning" size="small">未归属</el-tag>
              </div>
              <div class="info-row">
                <span class="info-label">状态</span>
                <el-tag :type="selectedDept.status === 'active' ? 'success' : 'warning'" size="small">
                  {{ selectedDept.status === 'active' ? '正常' : '待归属' }}
                </el-tag>
              </div>
              <div class="info-row">
                <span class="info-label">部门负责人</span>
                <div v-if="selectedDeptOwners.length" class="inline-owners">
                  <span v-for="owner in selectedDeptOwners" :key="owner.id" class="inline-owner">
                    <span>{{ owner.user_name }}</span>
                    <el-popconfirm
                      v-if="isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio?.id ?? '')"
                      :title="`确认卸任「${owner.user_name}」的部门负责人？`"
                      confirm-button-text="确认卸任"
                      confirm-button-type="danger"
                      @confirm="doRevoke(owner)"
                    >
                      <template #reference>
                        <el-button link type="danger" size="small">卸任</el-button>
                      </template>
                    </el-popconfirm>
                  </span>
                </div>
                <div v-else>
                  <span class="text-muted">未设置</span>
                  <el-button
                    v-if="isGlobalAdmin || myStudioIds.includes(selectedDeptParentStudio?.id ?? '')"
                    link type="primary" size="small" @click="openGrantDialog('dept_admin')"
                  >立即指定</el-button>
                </div>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>

      <!-- ─── 待归属视图 ─────────────────────────────────────────────────── -->
      <template v-else-if="selectedType === 'unmapped'">
        <div class="panel-header">
          <div class="panel-info">
            <div class="panel-breadcrumb">公司 <el-icon class="crumb-arrow"><ArrowRight /></el-icon> 待归属部门</div>
            <div class="panel-title">
              <el-icon class="title-icon" color="#e6a23c"><Warning /></el-icon>
              待归属部门
              <el-tag type="warning" size="small" class="title-badge">{{ unmappedDepts.length }}</el-tag>
            </div>
          </div>
        </div>

        <div class="tab-content" style="padding-top: 0">
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
        <el-icon size="52" color="#c0c4cc"><OfficeBuilding /></el-icon>
        <p class="empty-title">选择左侧节点查看详情</p>
        <p class="empty-sub">点击工作室查看部门列表，点击部门查看成员</p>
      </div>

    </main>
  </div>

  <!-- ═══ 授权弹窗 ══════════════════════════════════════════════════════════ -->
  <el-dialog v-model="grantDialogVisible" title="指定负责人" width="460px" @close="resetGrant">
    <div v-if="!grantSelectedUser" class="search-step">
      <div class="step-label">第一步：搜索并选择员工</div>
      <el-alert
        v-if="grantScopeStudioId"
        type="info" :closable="false" show-icon
        style="margin-bottom: 10px; font-size: 12px;"
      >
        仅显示「{{ selectedDeptParentStudio?.name }}」工作室下的员工
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
              {{ grantPresetRole === 'studio_admin' ? '工作室负责人' : '部门负责人' }}
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
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
Chart.register(...registerables)
import { ElMessage, ElMessageBox } from 'element-plus'
import type { ElTree } from 'element-plus'
import {
  OfficeBuilding, Folder, Warning, Search,
  ArrowRight, UserFilled, Close, Money, DataLine, CircleCheck, List,
} from '@element-plus/icons-vue'
import { useOrgStore, type RoleWithUser, type UserSearchResult, type DeptMember } from '@/stores/org'
import { useAuthStore } from '@/stores/auth'
import type { Studio, OrgDepartment, OrgNodeLeader } from '@/types/org'
import http from '@/services/http'
import { useRoute, useRouter } from 'vue-router'
import QuotaEditDialog from '@/components/QuotaEditDialog.vue'
import SyncStatusCard from '@/components/org/SyncStatusCard.vue'
import { QUOTA_DEFAULTS, getUserQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

const orgStore  = useOrgStore()
const authStore = useAuthStore()
const route     = useRoute()
const router    = useRouter()

// ─── 数据 ─────────────────────────────────────────────────────────────────────
const loading    = ref(false)
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
      // dept_admin：只显示包含自己管辖部门的工作室（可能为空工作室占位）
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
        // dept_admin 只能看到自己管辖的部门
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

async function onAddLeader(node: TreeNode) {
  const id = node.type === 'studio' ? node.studioData?.id : node.deptData?.id
  if (!id) return
  let keyword = ''
  try {
    const ret = await ElMessageBox.prompt(
      '按花名册解析：工号精确 / 姓名唯一匹配',
      '给「' + node.label + '」加负责人',
      { confirmButtonText: '添加', cancelButtonText: '取消', inputPlaceholder: '工号 / 姓名' },
    )
    keyword = String(ret.value || '').trim()
  } catch {
    return // 用户取消
  }
  if (!keyword) return
  try {
    await orgStore.addLeader(node.type, id, keyword)
    ElMessage.success('负责人已添加')
    await loadLeaders()
  } catch (e: any) {
    if (e?.response?.status === 404) ElMessage.error('花名册查无此人，或节点不存在')
    else ElMessage.error(e?.response?.data?.error || e?.message || '加负责人失败')
  }
}

function filterNode(value: string, data: TreeNode) {
  if (!value) return true
  return data.label.toLowerCase().includes(value.toLowerCase())
}

function onTreeSearch(val: string) {
  treeRef.value?.filter(val)
}

// ─── 选中状态 ─────────────────────────────────────────────────────────────────
const selectedType         = ref<'studio' | 'dept' | 'unmapped' | null>(null)
const selectedStudio       = ref<Studio | null>(null)
const selectedDept         = ref<OrgDepartment | null>(null)
const selectedDeptParentStudio = ref<Studio | null>(null)

const studioActiveTab = ref('depts')
const deptActiveTab   = ref('members')

function onNodeClick(data: TreeNode) {
  if (data.type === 'studio' && data.studioData) {
    // dept_admin 无工作室管辖权，点击工作室节点时自动跳到该工作室下自己管辖的第一个部门
    if (!isGlobalAdmin.value && myStudioIds.value.length === 0 && myDeptIds.value.length > 0) {
      const firstMyDept = allDepts.value.find(
        d => d.studio_id === data.studioData!.id && myDeptIds.value.includes(d.dept_id)
      )
      if (firstMyDept) {
        selectedType.value  = 'dept'
        selectedDept.value  = firstMyDept
        selectedDeptParentStudio.value = data.studioData
        selectedStudio.value = null
        deptActiveTab.value = 'members'
        treeRef.value?.setCurrentKey(`dept:${firstMyDept.dept_id}`)
        loadDeptMembers(1)
      }
      return
    }
    selectedType.value  = 'studio'
    selectedStudio.value = data.studioData
    selectedDept.value  = null
    selectedDeptParentStudio.value = null
    studioActiveTab.value = 'depts'
  } else if (data.type === 'dept' && data.deptData) {
    selectedType.value  = 'dept'
    selectedDept.value  = data.deptData
    selectedDeptParentStudio.value = data.parentStudio || null
    selectedStudio.value = null
    deptActiveTab.value = 'members'
    loadDeptMembers(1)
  }
}

function applyRouteSelection() {
  const scopeType = String(route.query.scope_type || '')
  const scopeID = String(route.query.scope_id || '')
  const targetTab = String(route.query.tab || '')
  if (!scopeType || !scopeID) return

  if (scopeType === 'studio') {
    const studio = studios.value.find(s => s.id === scopeID)
    if (!studio) return
    selectedType.value = 'studio'
    selectedStudio.value = studio
    selectedDept.value = null
    selectedDeptParentStudio.value = null
    studioActiveTab.value = targetTab === 'owner' ? 'owner' : 'depts'
    nextTick(() => treeRef.value?.setCurrentKey(`studio:${studio.id}`))
    return
  }

  if (scopeType === 'dept' || scopeType === 'dept_daily' || scopeType === 'dept_service') {
    const dept = allDepts.value.find(d => d.dept_id === scopeID)
    if (!dept) return
    const parent = studios.value.find(s => s.id === dept.studio_id) || null
    selectedType.value = 'dept'
    selectedDept.value = dept
    selectedDeptParentStudio.value = parent
    selectedStudio.value = null
    deptActiveTab.value = targetTab === 'info' ? 'info' : 'members'
    nextTick(() => treeRef.value?.setCurrentKey(`dept:${dept.dept_id}`))
    if (deptActiveTab.value === 'members') loadDeptMembers(1)
  }
}

function selectUnmapped() {
  selectedType.value  = 'unmapped'
  selectedStudio.value = null
  selectedDept.value  = null
  selectedDeptParentStudio.value = null
  // 取消树中的高亮
  treeRef.value?.setCurrentKey(undefined)
}

function clickDeptFromStudio(dept: OrgDepartment) {
  const parent = studios.value.find(s => s.id === dept.studio_id) || null
  selectedType.value  = 'dept'
  selectedDept.value  = dept
  selectedDeptParentStudio.value = parent
  selectedStudio.value = null
  deptActiveTab.value = 'members'
  treeRef.value?.setCurrentKey(`dept:${dept.dept_id}`)
  loadDeptMembers(1)
}

function clickStudioFromDept() {
  if (!selectedDeptParentStudio.value) return
  const s = selectedDeptParentStudio.value
  selectedType.value  = 'studio'
  selectedStudio.value = s
  selectedDept.value  = null
  selectedDeptParentStudio.value = null
  treeRef.value?.setCurrentKey(`studio:${s.id}`)
  studioActiveTab.value = 'depts'
}

// ─── 工作室计算属性 ────────────────────────────────────────────────────────────
const selectedStudioDepts = computed(() => {
  const base = allDepts.value.filter(d => d.studio_id === selectedStudio.value?.id)
  // dept_admin 只能看到自己管辖的部门，不能看工作室全部部门列表
  if (!isGlobalAdmin.value && myStudioIds.value.length === 0 && myDeptIds.value.length > 0) {
    return base.filter(d => myDeptIds.value.includes(d.dept_id))
  }
  return base
})

const selectedStudioOwners = computed(() =>
  allRoles.value.filter(r => r.role === 'studio_admin' && r.scope_id === selectedStudio.value?.id)
)

const selectedDeptOwners = computed(() =>
  allRoles.value.filter(r => r.role === 'dept_admin' && r.scope_id === selectedDept.value?.dept_id)
)

function getDeptOwners(deptId: string): RoleWithUser[] {
  return allRoles.value.filter(r => r.role === 'dept_admin' && r.scope_id === deptId)
}

// ─── 部门成员 ─────────────────────────────────────────────────────────────────
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

function onMemberSearch() {
  // 本地过滤，无需防抖
}

// ─── 授权弹窗 ─────────────────────────────────────────────────────────────────
const grantDialogVisible  = ref(false)
const granting            = ref(false)
const grantPresetRole      = ref('')
const grantPresetScopeType = ref('')
const grantPresetScopeId   = ref('')
const grantPresetScopeName = ref('')
// 指定部门负责人时限制只能从父工作室成员里选人；工作室负责人时为空（全公司）
const grantScopeStudioId   = ref('')
const grantSearchKeyword   = ref('')
const grantSearchResults   = ref<UserSearchResult[]>([])
const grantSearchLoading   = ref(false)
const grantSelectedUser    = ref<UserSearchResult | null>(null)

function openGrantDialog(role: 'studio_admin' | 'dept_admin') {
  grantPresetRole.value = role
  if (role === 'studio_admin' && selectedStudio.value) {
    grantPresetScopeType.value = 'studio'
    grantPresetScopeId.value   = selectedStudio.value.id
    grantPresetScopeName.value = `工作室：${selectedStudio.value.name}`
    grantScopeStudioId.value   = '' // 工作室负责人：全公司选人
  } else if (role === 'dept_admin' && selectedDept.value) {
    grantPresetScopeType.value = 'dept'
    grantPresetScopeId.value   = selectedDept.value.dept_id
    grantPresetScopeName.value = `部门：${selectedDept.value.dept_name}`
    // 部门负责人：只能从父工作室的员工里选
    grantScopeStudioId.value   = selectedDeptParentStudio.value?.id ?? ''
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
    const roleLabel = grantPresetRole.value === 'studio_admin' ? '工作室负责人' : '部门负责人'
    ElMessage.success(`已将「${grantSelectedUser.value.name}」设为${roleLabel}`)
    grantDialogVisible.value = false
    allRoles.value = await orgStore.fetchAllRoles()
  } catch {} finally {
    granting.value = false
  }
}

async function doRevoke(role: RoleWithUser) {
  try {
    await orgStore.revokeRole(role.user_id, role.role, role.scope_id)
    ElMessage.success('已卸任')
    allRoles.value = await orgStore.fetchAllRoles()
  } catch {}
}

function resetGrant() {
  grantSearchKeyword.value  = ''
  grantSearchResults.value  = []
  grantSelectedUser.value   = null
  grantScopeStudioId.value  = ''
}

// ─── 辅助 ─────────────────────────────────────────────────────────────────────
function formatDate(str: string) {
  return str ? str.slice(0, 10) : '—'
}

// ─── 数据加载 ─────────────────────────────────────────────────────────────────
async function reload() {
  loading.value = true
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
      if (!selectedStudio.value) selectedType.value = null
    }
    if (selectedDept.value) {
      selectedDept.value = allDepts.value.find(d => d.dept_id === selectedDept.value!.dept_id) || null
      if (!selectedDept.value) selectedType.value = null
    }
    applyRouteSelection()
  } finally {
    loading.value = false
  }
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

function openMemberDetail(member: DeptMember) {
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

.quota-cell { font-size: 13px; }
.text-placeholder { color: var(--el-text-color-placeholder); font-size: 13px; }

/* ─── 左侧树面板 ────────────────────────────────────────────────────────────── */
.tree-panel {
  width: 240px;
  min-width: 200px;
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
  color: #909399;
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
  color: #e6a23c;
}

.unmapped-node:hover { background: var(--el-fill-color-lighter); }
.unmapped-node.is-current { background: #fdf6ec; }

.node-count { flex-shrink: 0; }

/* ─── 右侧内容面板 ──────────────────────────────────────────────────────────── */
.content-panel {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

/* Panel 顶部 header */
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

.crumb-link {
  cursor: pointer;
  color: var(--el-color-primary);
}

.crumb-link:hover { text-decoration: underline; }
.crumb-text { color: var(--el-text-color-regular); cursor: default; }

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

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  padding-top: 26px;
}

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

.tab-badge {
  margin-left: 4px;
  vertical-align: middle;
}

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

/* 部门信息 grid */
.dept-info-grid { display: flex; flex-direction: column; gap: 16px; max-width: 480px; }
.info-row { display: flex; align-items: center; gap: 12px; font-size: 14px; }
.info-label {
  width: 90px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  flex-shrink: 0;
}

.inline-owners { display: flex; flex-direction: column; gap: 6px; }
.inline-owner { display: flex; align-items: center; gap: 8px; }

/* 部门 ID code */
.dept-id-code {
  font-family: monospace;
  font-size: 12px;
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
  color: var(--el-text-color-secondary);
}

/* 点击跳转的部门名称 */
.clickable-dept {
  cursor: pointer;
  color: var(--el-color-primary);
  font-size: 13px;
}
.clickable-dept:hover { text-decoration: underline; }

/* 负责人列表（多人） */
.owners-list { display: flex; flex-direction: column; gap: 12px; }

/* 负责人卡片 */
.owner-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  max-width: 420px;
}

.owner-avatar {
  width: 44px; height: 44px; border-radius: 50%;
  background: var(--el-color-primary-light-5);
  color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; font-weight: 700; flex-shrink: 0;
}

.owner-info { flex: 1; min-width: 0; }
.owner-name { font-size: 15px; font-weight: 600; }
.owner-email { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.owner-since { font-size: 12px; color: var(--el-text-color-placeholder); margin-top: 2px; }

.owner-empty {
  display: flex; flex-direction: column; align-items: center;
  gap: 10px; padding: 40px 0;
  color: var(--el-text-color-secondary);
}

.owner-chip {
  display: inline-flex; align-items: center;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

/* 成员列表 */
.members-toolbar { display: flex; align-items: center; margin-bottom: 12px; }
.member-cell { display: flex; align-items: center; gap: 8px; }
.member-avatar {
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; flex-shrink: 0;
}
.member-name { font-size: 13px; }
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

.text-muted { color: var(--el-text-color-placeholder); font-size: 13px; }

/* Table horizontal scroll wrapper */
.org-table-wrap { width: 100%; }

/* ─── 弹窗：授权 ─────────────────────────────────────────────────────────────── */
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

/* ─── 成员列表 ───────────────────────────────────────────────────────────────── */
.text-mono { font-family: monospace; font-size: 12px; color: var(--el-text-color-secondary); }

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
.md-kpi--green::before { background: #67c23a; }
.md-kpi--blue::before  { background: #409eff; }
.md-kpi--amber::before { background: #e6a23c; }
.md-kpi--red::before   { background: #f56c6c; }
.md-kpi--gray::before  { background: #c0c4cc; }

.md-kpi__icon {
  width: 34px; height: 34px; border-radius: 8px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.md-kpi--green .md-kpi__icon { background: rgba(103,194,58,0.12); color: #67c23a; }
.md-kpi--blue  .md-kpi__icon { background: rgba(64,158,255,0.12);  color: #409eff; }
.md-kpi--amber .md-kpi__icon { background: rgba(230,162,60,0.12);  color: #e6a23c; }
.md-kpi--red   .md-kpi__icon { background: rgba(245,108,108,0.12); color: #f56c6c; }
.md-kpi--gray  .md-kpi__icon { background: var(--el-fill-color);   color: #909399; }

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
.md-log-status--ok  { background: #67c23a; }
.md-log-status--err { background: #f56c6c; }
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
.md-model-bar-fill { height: 100%; background: linear-gradient(90deg, #409eff, #79bbff); border-radius: 3px; transition: width .4s; }
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
  .tree-body { max-height: 240px; }
  .md-kpi-row { grid-template-columns: repeat(2, 1fr); }

  /* Header: stack vertically */
  .panel-header { flex-direction: column; align-items: flex-start; gap: 8px; padding: 16px 16px 0; }
  .panel-actions { width: 100%; padding-top: 0; flex-wrap: wrap; }

  /* Tabs: reduce padding */
  .panel-tabs { padding: 0 16px; }

  /* Tables: overflow-x auto */
  .org-table-wrap { overflow-x: auto; width: 100%; }
  .org-table-wrap :deep(.el-table) { min-width: 680px; }

  /* Member toolbar: full-width */
  .members-toolbar { width: 100%; }
  .members-toolbar .el-input { width: 100% !important; }

  /* Pagination: center */
  .members-pagination { justify-content: center; }

  /* Dept info grid: full width */
  .dept-info-grid { max-width: 100%; }

  /* Owner cards: full width */
  .owner-card { max-width: 100%; }

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

  /* Unmapped table action: full width select */
  .unmapped-actions { flex-direction: column; gap: 8px; }
}

@media (max-width: 480px) {
  .panel-header { padding: 12px 12px 0; }
  .panel-tabs { padding: 0 12px; }
  .panel-title { font-size: 15px; }
  .tree-panel { min-width: 0; }
  :deep(.md-drawer .el-drawer__body) { padding: 12px; }
}
</style>
