<template>
  <div class="ud">

    <!-- ══ 页头 ══ -->
    <div class="ud-header">
      <div class="ud-header__left">
        <h2 class="ud-title">用量看板</h2>
        <span class="ud-badge">{{ scopeLabel }}</span>
      </div>
      <div class="ud-header__right">
        <el-button :loading="summaryLoading || logsLoading" :icon="Refresh"
          circle size="small" title="刷新" @click="loadAll" />
      </div>
    </div>

    <!-- ══ 筛选栏 ══ -->
    <div class="ud-filter">
      <div class="ud-filter-row ud-filter-row--date">
        <div class="ud-presets">
          <button
            v-for="p in datePresets" :key="p.label"
            :class="['ud-preset-btn', { 'ud-preset-btn--active': activePreset === p.label }]"
            @click="applyPreset(p)"
          >{{ p.label }}</button>
        </div>
        <div class="ud-filter-divider" />
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          format="MM/DD"
          class="ud-filter__date"
          size="default"
          @change="onDateChange"
        />
      </div>
      <div class="ud-filter-row ud-filter-row--dims">
        <!-- 成员姓名：非普通用户可见 -->
        <div v-if="authStore.effectiveRole !== 'user'" class="ud-filter-chip">
          <span class="ud-chip-label">成员</span>
          <el-input v-model="filterUserName" placeholder="姓名搜索" clearable size="small"
            class="ud-chip-input" @change="onUserNameChange" @clear="onUserNameClear">
            <template #prefix><el-icon><User /></el-icon></template>
          </el-input>
        </div>
        <!-- 模型 -->
        <div class="ud-filter-chip">
          <span class="ud-chip-label">模型</span>
          <el-input v-model="filterModel" placeholder="搜索模型名" clearable size="small"
            class="ud-chip-input" @change="loadAll" @clear="loadAll">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>
        <!-- 状态 -->
        <div class="ud-filter-chip">
          <span class="ud-chip-label">状态</span>
          <el-select v-model="filterStatus" placeholder="全部" clearable size="small"
            class="ud-chip-select" @change="loadAll" @clear="loadAll">
            <el-option label="全部" value="" />
            <el-option value="success">
              <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--ok" />成功</div>
            </el-option>
            <el-option value="error">
              <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--err" />失败</div>
            </el-option>
          </el-select>
        </div>
        <el-button v-if="activeFilterCount > 0" link size="small" type="warning" @click="clearFilters">
          清空 {{ activeFilterCount }} 个筛选
        </el-button>
      </div>
    </div>

    <!-- ══ KPI 卡片 ══ -->
    <div class="ud-kpi-row" v-loading="summaryLoading">
      <!-- 总成本 -->
      <div class="ud-kpi-card ud-kpi-card--green">
        <div class="ud-kpi__icon"><el-icon size="20"><Money /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">总成本</p>
          <p class="ud-kpi__value">${{ fmtCost(summary?.cost_estimate) }}</p>
          <p class="ud-kpi__foot">
            <template v-if="costChange !== null">
              <el-tooltip :content="prevPeriodLabel" placement="bottom" :show-after="300">
                <span :class="changeClass(costChange, true)" class="ud-change-chip">
                  <el-icon size="11"><component :is="costChange >= 0 ? ArrowUp : ArrowDown" /></el-icon>
                  {{ fmtChange(costChange) }}
                  <span class="ud-kpi__vs">vs 上期</span>
                </span>
              </el-tooltip>
            </template>
            <span v-else class="ud-kpi__sub">暂无上期数据</span>
          </p>
        </div>
      </div>
      <!-- 总调用 -->
      <div class="ud-kpi-card ud-kpi-card--blue">
        <div class="ud-kpi__icon"><el-icon size="20"><DataLine /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">总调用次数</p>
          <p class="ud-kpi__value">{{ fmtNum(summary?.call_count) }}</p>
          <p class="ud-kpi__foot">
            <template v-if="callsChange !== null">
              <el-tooltip :content="prevPeriodLabel" placement="bottom" :show-after="300">
                <span :class="changeClass(callsChange)" class="ud-change-chip">
                  <el-icon size="11"><component :is="callsChange >= 0 ? ArrowUp : ArrowDown" /></el-icon>
                  {{ fmtChange(callsChange) }}
                  <span class="ud-kpi__vs">vs 上期</span>
                </span>
              </el-tooltip>
            </template>
            <span v-else class="ud-kpi__sub">暂无上期数据</span>
          </p>
        </div>
      </div>
      <!-- 成功率 -->
      <div class="ud-kpi-card" :class="successRate >= 99 ? 'ud-kpi-card--green' : successRate >= 95 ? 'ud-kpi-card--amber' : 'ud-kpi-card--red'">
        <div class="ud-kpi__icon"><el-icon size="20"><CircleCheck /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">成功率</p>
          <p class="ud-kpi__value">{{ successRate.toFixed(1) }}%</p>
          <p class="ud-kpi__foot">
            <span class="ud-kpi__sub">失败 {{ fmtNum(summary?.error_count) }} 次</span>
          </p>
        </div>
      </div>
      <!-- 活跃成员 / 个人视角下换成 Token 消耗 -->
      <div class="ud-kpi-card ud-kpi-card--purple">
        <div class="ud-kpi__icon"><el-icon size="20"><component :is="currentScope === 'user' ? List : User" /></el-icon></div>
        <template v-if="currentScope !== 'user'">
          <div class="ud-kpi__body">
            <p class="ud-kpi__label">活跃成员</p>
            <p class="ud-kpi__value">{{ summary?.user_count ?? '—' }}</p>
            <p class="ud-kpi__foot">
              <span class="ud-kpi__sub">均次成本 ${{ avgCostPerCall }}</span>
            </p>
          </div>
        </template>
        <template v-else>
          <div class="ud-kpi__body">
            <p class="ud-kpi__label">Token 消耗</p>
            <p class="ud-kpi__value">{{ summary?.total_tokens != null ? (summary.total_tokens >= 1000000 ? (summary.total_tokens/1000000).toFixed(2)+'M' : (summary.total_tokens/1000).toFixed(1)+'K') : '—' }}</p>
            <p class="ud-kpi__foot">
              <span class="ud-kpi__sub">均次成本 ${{ avgCostPerCall }}</span>
            </p>
          </div>
        </template>
      </div>
    </div>

    <!-- ══ 趋势图 ══ -->
    <div class="ud-section">
      <div class="ud-section__header">
        <span class="ud-section__title">调用趋势</span>
        <div class="ud-trend-tabs">
          <button
            v-for="m in trendMetrics" :key="m.value"
            :class="['ud-trend-tab', { 'ud-trend-tab--active': trendMetric === m.value }]"
            @click="switchTrendMetric(m.value)"
          >{{ m.label }}</button>
        </div>
      </div>
      <div class="ud-chart-wrap" v-loading="trendLoading">
        <canvas ref="trendCanvasRef" class="ud-trend-canvas" />
        <div v-if="!trendLoading && trendData.length === 0" class="ud-chart-empty">暂无趋势数据</div>
      </div>
    </div>

    <!-- ══ 成本归因 + 模型分布 ══ -->
    <div class="ud-main-row">

      <!-- 成本归因排行 -->
      <div class="ud-section">

        <!-- 个人视角提示条 -->
        <div v-if="currentScope === 'user'" class="ud-user-scope-banner">
          <el-icon><User /></el-icon>
          <span>当前仅展示 <strong>{{ breadcrumbs[breadcrumbs.length - 1]?.name }}</strong> 的个人用量数据</span>
          <el-button link size="small" type="primary" style="margin-left:auto" @click="resetToTop">
            退出个人视角
          </el-button>
        </div>

        <div class="ud-section__header">
          <span class="ud-section__title">{{ rankTitle }}</span>
          <div class="ud-breadcrumb">
            <span class="ud-bc-home" @click="resetToTop">{{ topLabel }}</span>
            <template v-for="(crumb, idx) in breadcrumbs" :key="crumb.id">
              <el-icon class="ud-bc-sep"><ArrowRight /></el-icon>
              <span
                :class="['ud-bc-item', { 'ud-bc-current': idx === breadcrumbs.length - 1 }]"
                @click="idx < breadcrumbs.length - 1 && popToLevel(idx)"
              >{{ crumb.name }}</span>
            </template>
          </div>
        </div>

        <div class="ud-table-wrap" v-loading="summaryLoading">
          <table class="ud-rank-table">
            <thead>
              <tr>
                <th class="col-rank">#</th>
                <th class="col-name">名称</th>
                <th>成本占比</th>
                <th class="col-num">成本 (USD)</th>
                <th class="col-num">调用次数</th>
                <!-- user scope 展示 Token 消耗，否则展示活跃用户 -->
                <th class="col-num">{{ currentScope === 'user' ? 'Token 消耗' : '活跃用户' }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, i) in breakdown"
                :key="row.id ?? i"
                :class="{ 'ud-rank-drillable': canDrill }"
              >
                <td class="col-rank">
                  <span :class="['ud-rank-num', i < 3 ? `ud-rank-num--top${i+1}` : '']">{{ row.rank ?? i + 1 }}</span>
                </td>
                <td class="col-name">
                  <span
                    :class="['ud-row-name', { drillable: canDrill }]"
                    @click="canDrill && drillDown(row)"
                  >{{ row.name || row.id || '(未归属)' }}</span>
                </td>
                <td>
                  <div class="ud-bar-wrap">
                    <div class="ud-bar-bg">
                      <div class="ud-bar-fill ud-bar-fill--cost" :style="{ width: barPct(row.cost_estimate, maxCost) + '%' }" />
                    </div>
                    <span class="ud-bar-pct">{{ barPct(row.cost_estimate, maxCost) }}%</span>
                  </div>
                </td>
                <td class="col-num c-green fw6">${{ fmtCost(row.cost_estimate) }}</td>
                <td class="col-num">{{ fmtNum(row.call_count) }}</td>
                <!-- user scope: 展示 token，否则展示活跃用户数 -->
                <td class="col-num">
                  <template v-if="currentScope === 'user'">
                    {{ row.total_tokens != null ? (row.total_tokens >= 1000000 ? (row.total_tokens/1000000).toFixed(1)+'M' : (row.total_tokens/1000).toFixed(1)+'K') : '—' }}
                  </template>
                  <template v-else>{{ row.user_count ?? '—' }}</template>
                </td>
              </tr>
              <tr v-if="!summaryLoading && breakdown.length === 0">
                <td colspan="6" class="ud-empty">暂无数据</td>
              </tr>
            </tbody>
          </table>
          <div v-if="rankTotal > rankPageSize" class="ud-pagination">
            <el-pagination
              v-model:current-page="rankPage"
              :page-size="rankPageSize"
              :total="rankTotal"
              layout="total, prev, pager, next"
              small
              @current-change="loadSummary"
            />
          </div>
        </div>
      </div>

      <!-- 模型分布 -->
      <div class="ud-section">
        <div class="ud-section__header">
          <span class="ud-section__title">模型成本分布</span>
          <span class="ud-section__hint">Top {{ Math.min(modelsData.length, 6) }}{{ modelsData.length > 6 ? ` / 共 ${modelsData.length} 种` : '' }}</span>
        </div>
        <div class="ud-chart-wrap ud-chart-wrap--model" v-loading="modelsLoading">
          <canvas ref="modelCanvasRef" class="ud-model-canvas" />
          <div v-if="!modelsLoading && modelsData.length === 0" class="ud-chart-empty">暂无数据</div>
        </div>
      </div>

    </div>

    <!-- ══ 调用明细 ══ -->
    <div class="ud-section">
      <div class="ud-section__header">
        <span class="ud-section__title">调用明细</span>
        <span class="ud-section__count">共 {{ logsTotal }} 条</span>
      </div>
      <div class="ud-table-wrap" v-loading="logsLoading">
        <el-table :data="logRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%">

          <!-- Key 名称 -->
          <el-table-column label="Key 名称" min-width="120" fixed="left">
            <template #default="{ row }">
              <span class="ud-key-name">{{ row.key_name || '—' }}</span>
            </template>
          </el-table-column>

          <!-- 成员 -->
          <el-table-column label="成员" min-width="110" fixed="left">
            <template #default="{ row }">
              <span class="ud-member-name">{{ row.user_name || '—' }}</span>
            </template>
          </el-table-column>

          <!-- 工作室（仅 admin 可见） -->
          <el-table-column
            v-if="authStore.effectiveRole === 'admin'"
            label="工作室" min-width="110" fixed="left"
          >
            <template #default="{ row }">
              <span class="ud-studio-name">{{ row.studio_name || '—' }}</span>
            </template>
          </el-table-column>

          <!-- 部门（admin / studio_admin 可见） -->
          <el-table-column
            v-if="authStore.effectiveRole === 'admin' || authStore.effectiveRole === 'studio_admin'"
            label="部门" min-width="110" fixed="left"
          >
            <template #default="{ row }">
              <span class="ud-dept-name">{{ row.dept_name || '—' }}</span>
            </template>
          </el-table-column>

          <!-- 模型 / 路由 -->
          <el-table-column prop="model" label="模型 / 路由" min-width="160">
            <template #default="{ row }">
              <div class="ud-model-cell">
                <span class="ud-model-name">{{ row.model || '—' }}</span>
                <div v-if="row.routed_model && row.routed_model !== row.model" class="ud-model-routed">
                  <span class="ud-arrow">↳</span>
                  <span class="ud-model-name ud-model-name--muted">{{ row.routed_model }}</span>
                </div>
              </div>
            </template>
          </el-table-column>

          <!-- 类型 -->
          <el-table-column label="类型" min-width="70" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.stream" type="primary" size="small" effect="plain">流式</el-tag>
              <el-tag v-else size="small" effect="plain">同步</el-tag>
            </template>
          </el-table-column>

          <!-- Token 用量 -->
          <el-table-column label="Token 用量" min-width="200">
            <template #default="{ row }">
              <div class="ud-token-row">
                <span class="ud-token-item ud-token-item--in" title="输入">
                  <el-icon class="ud-token-icon"><Bottom /></el-icon>{{ fmtTokens(row.prompt_tokens) }}
                </span>
                <span class="ud-token-item ud-token-item--out" title="输出">
                  <el-icon class="ud-token-icon"><Top /></el-icon>{{ fmtTokens(row.completion_tokens) }}
                </span>
                <span v-if="row.cached_tokens" class="ud-token-item ud-token-item--cache" title="缓存">
                  <el-icon class="ud-token-icon"><Finished /></el-icon>{{ fmtTokens(row.cached_tokens) }}
                </span>
                <el-tooltip placement="top" :show-after="120" popper-class="ud-tt-popper">
                  <template #content>
                    <div class="ud-tt">
                      <div class="ud-tt__title">Token 明细</div>
                      <div class="ud-tt__row"><span>输入</span><b>{{ fmtNum(row.prompt_tokens) }}</b></div>
                      <div class="ud-tt__row"><span>输出</span><b>{{ fmtNum(row.completion_tokens) }}</b></div>
                      <div class="ud-tt__row" v-if="row.reasoning_tokens"><span class="ud-tt__sub">└ 推理</span><b class="c-purple">{{ fmtNum(row.reasoning_tokens) }}</b></div>
                      <div class="ud-tt__sep"></div>
                      <div class="ud-tt__group"><el-icon class="ud-tt__group-ic"><Finished /></el-icon>缓存</div>
                      <div class="ud-tt__row"><span>缓存命中</span><b :class="row.cached_tokens ? 'c-cyan' : ''">{{ fmtNum(row.cached_tokens) }}</b></div>
                      <div class="ud-tt__row"><span>缓存写入</span><b :class="row.cache_write_tokens ? 'c-amber' : ''">{{ fmtNum(row.cache_write_tokens) }}</b></div>
                      <div class="ud-tt__sep"></div>
                      <div class="ud-tt__row"><span>总计</span><b class="c-blue">{{ fmtNum(row.total_tokens) }}</b></div>
                    </div>
                  </template>
                  <span class="ud-info"><el-icon><InfoFilled /></el-icon></span>
                </el-tooltip>
              </div>
            </template>
          </el-table-column>

          <!-- 状态 -->
          <el-table-column prop="status" label="状态" min-width="70" align="center">
            <template #default="{ row }">
              <el-tag :type="row.status === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ row.status || '—' }}</el-tag>
            </template>
          </el-table-column>

          <!-- TTFT -->
          <el-table-column prop="first_token_ms" label="TTFT" min-width="70" align="right">
            <template #default="{ row }">
              <span v-if="row.first_token_ms != null" class="ud-duration">{{ fmtDuration(row.first_token_ms) }}</span>
              <span v-else class="ud-dash">—</span>
            </template>
          </el-table-column>

          <!-- 耗时 -->
          <el-table-column prop="duration_ms" label="耗时" min-width="70" align="right" sortable>
            <template #default="{ row }">
              <span class="ud-duration">{{ fmtDuration(row.duration_ms) }}</span>
            </template>
          </el-table-column>

          <!-- 标记 -->
          <el-table-column label="标记" min-width="90" align="center">
            <template #default="{ row }">
              <div class="ud-flags">
                <el-tag v-if="row.reasoning_effort" :type="reasoningType(row.reasoning_effort)" size="small" effect="plain" class="ud-tag--effort">{{ row.reasoning_effort }}</el-tag>
                <el-tag v-if="row.failover_used" type="warning" size="small" effect="plain">切换</el-tag>
                <el-tag v-if="row.usage_missing" type="info" size="small" effect="plain">无Usage</el-tag>
              </div>
            </template>
          </el-table-column>

          <!-- 费用 -->
          <el-table-column prop="est_cost_usd" label="费用" min-width="140" align="right" sortable>
            <template #default="{ row }">
              <div class="ud-cost-cell">
                <span class="ud-cost" :class="logCostColor(row.est_cost_usd)">${{ fmtCost(row.est_cost_usd) }}</span>
                <el-tooltip placement="left" :show-after="120" popper-class="ud-tt-popper">
                  <template #content>
                    <div class="ud-tt">
                      <div class="ud-tt__title">成本明细 (USD)</div>
                      <template v-if="(row.input_price_snapshot ?? 0) > 0 || (row.output_price_snapshot ?? 0) > 0">
                        <div class="ud-tt__group"><el-icon class="ud-tt__group-ic"><Money /></el-icon>单价 / 1M tokens</div>
                        <div class="ud-tt__row"><span>输入单价</span><b>${{ fmtPrice(row.input_price_snapshot) }}</b></div>
                        <div class="ud-tt__row"><span>输出单价</span><b>${{ fmtPrice(row.output_price_snapshot) }}</b></div>
                        <div class="ud-tt__sep"></div>
                      </template>
                      <div class="ud-tt__group"><el-icon class="ud-tt__group-ic"><Coin /></el-icon>费用</div>
                      <div class="ud-tt__row"><span>输入</span><b>${{ (row.input_cost_usd || 0).toFixed(6) }}</b></div>
                      <div class="ud-tt__row"><span>输出</span><b>${{ (row.output_cost_usd || 0).toFixed(6) }}</b></div>
                      <div class="ud-tt__sep"></div>
                      <div class="ud-tt__row"><span>合计</span><b class="c-green">${{ fmtCost(row.est_cost_usd) }}</b></div>
                    </div>
                  </template>
                  <span class="ud-info"><el-icon><InfoFilled /></el-icon></span>
                </el-tooltip>
              </div>
            </template>
          </el-table-column>

          <!-- 来源 -->
          <el-table-column label="来源" min-width="160">
            <template #default="{ row }">
              <div class="ud-source">
                <span v-if="row.ip_address" class="ud-ip">{{ row.ip_address }}</span>
                <span v-else class="ud-dash">—</span>
              </div>
            </template>
          </el-table-column>

          <!-- 时间 -->
          <el-table-column prop="ts" label="时间" min-width="155" sortable>
            <template #default="{ row }">
              <span class="ud-time">{{ fmtDateTime(row.ts) }}</span>
            </template>
          </el-table-column>

          <!-- 详情 -->
          <el-table-column label="" width="48" fixed="right">
            <template #default="{ row }">
              <el-button link size="small" title="查看详情" @click="openTrace(row)">
                <el-icon><View /></el-icon>
              </el-button>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="!logsLoading && logRows.length === 0" class="ud-empty-block">
          <el-icon size="40" color="#c0c4cc"><List /></el-icon>
          <p>暂无调用记录</p>
        </div>

        <div v-if="logsTotal > 0" class="ud-pagination">
          <el-pagination
            v-model:current-page="logsPage"
            v-model:page-size="logsPageSize"
            :page-sizes="[20, 50, 100]"
            :total="logsTotal"
            layout="total, sizes, prev, pager, next"
            small
            @size-change="reloadLogs"
            @current-change="loadLogs"
          />
        </div>
      </div>
    </div>

    <!-- ══ Trace Drawer ══ -->
    <el-drawer v-model="traceVisible" title="" size="780px" direction="rtl" :before-close="closeTrace">
      <template #header>
        <div class="ud-trace-header">
          <span class="ud-trace-title">请求详情</span>
          <span class="ud-trace-id">{{ traceRow?.request_id }}</span>
        </div>
      </template>
      <div v-if="traceRow" class="ud-trace-body">
        <div class="ud-trace-meta">
          <el-tag type="info" size="small" effect="plain">{{ traceRow.model }}</el-tag>
          <el-tag :type="(traceRow.status || 0) === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ traceRow.status }}</el-tag>
          <el-tag v-if="traceRow.stream" type="primary" size="small" effect="plain">流式</el-tag>
          <span class="ud-trace-time">{{ fmtDateTime(traceRow.ts) }}</span>
          <span class="ud-trace-dur">{{ fmtDuration(traceRow.duration_ms) }}</span>
        </div>

        <el-tabs v-model="traceTab" class="ud-trace-tabs">
          <el-tab-pane label="调用信息" name="meta">
            <div class="ud-meta-grid">
              <div class="ud-meta-section">
                <div class="ud-meta-heading">调用者</div>
                <div class="ud-meta-row"><span>用户</span><span>{{ traceRow.user_name || '—' }}</span></div>
                <div class="ud-meta-row"><span>部门</span><span>{{ traceRow.dept_name || traceRow.dept_id || '—' }}</span></div>
                <div class="ud-meta-row"><span>工作室</span><span>{{ traceRow.studio_name || '—' }}</span></div>
                <div class="ud-meta-row"><span>IP</span><code>{{ traceRow.ip_address || '—' }}</code></div>
              </div>
              <div class="ud-meta-section">
                <div class="ud-meta-heading">路由</div>
                <div class="ud-meta-row"><span>请求模型</span><code>{{ traceRow.model }}</code></div>
                <div class="ud-meta-row"><span>实际模型</span><code>{{ traceRow.routed_model || '—' }}</code></div>
                <div class="ud-meta-row"><span>渠道</span><el-tag size="small" type="info" effect="plain">{{ traceRow.provider || '—' }}</el-tag></div>
                <div class="ud-meta-row"><span>分组</span><code>{{ traceRow.group_name || '—' }}</code></div>
                <div class="ud-meta-row"><span>故障转移</span>
                  <el-tag size="small" :type="traceRow.failover_used ? 'warning' : 'success'" effect="plain">{{ traceRow.failover_used ? '是' : '否' }}</el-tag>
                </div>
              </div>
              <div class="ud-meta-section">
                <div class="ud-meta-heading">Token 用量</div>
                <div class="ud-meta-row"><span>输入</span><b>{{ fmtNum(traceRow.prompt_tokens) }}</b></div>
                <div class="ud-meta-row"><span>缓存命中</span><b class="c-cyan">{{ fmtNum(traceRow.cached_tokens) }}</b></div>
                <div class="ud-meta-row"><span>缓存写入</span><b class="c-amber">{{ fmtNum(traceRow.cache_write_tokens) }}</b></div>
                <div class="ud-meta-row"><span>输出</span><b>{{ fmtNum(traceRow.completion_tokens) }}</b></div>
                <div class="ud-meta-row ud-meta-sep"><span>合计</span><b class="c-blue">{{ fmtNum(traceRow.total_tokens) }}</b></div>
              </div>
              <div class="ud-meta-section">
                <div class="ud-meta-heading">费用 (USD)</div>
                <div class="ud-meta-row"><span>输入</span><b>${{ fmtCost(traceRow.input_cost_usd) }}</b></div>
                <div class="ud-meta-row"><span>输出</span><b>${{ fmtCost(traceRow.output_cost_usd) }}</b></div>
                <div class="ud-meta-row ud-meta-sep"><span>合计</span><b class="c-green">${{ fmtCost(traceRow.est_cost_usd) }}</b></div>
                <div class="ud-meta-row"><span>TTFT</span><b>{{ fmtDuration(traceRow.first_token_ms) }}</b></div>
                <div class="ud-meta-row"><span>总耗时</span><b>{{ fmtDuration(traceRow.duration_ms) }}</b></div>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="提示词" name="prompt">
            <div v-if="traceMessages.length" class="ud-msgs">
              <div v-for="(msg, i) in traceMessages" :key="i" :class="['ud-msg', `ud-msg--${msg.role}`]">
                <span class="ud-msg-role">{{ msg.role }}</span>
                <pre class="ud-msg-content">{{ typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content, null, 2) }}</pre>
              </div>
            </div>
            <div v-else class="ud-trace-empty">无提示词数据</div>
          </el-tab-pane>

          <el-tab-pane name="response">
            <template #label>
              响应
              <el-tag v-if="traceRow?.stream" size="small" type="info" style="margin-left:4px;vertical-align:middle">Stream</el-tag>
            </template>
            <div v-if="traceResponseText" class="ud-msgs">
              <div class="ud-msg ud-msg--assistant">
                <span class="ud-msg-role">assistant</span>
                <pre class="ud-msg-content">{{ traceResponseText }}</pre>
              </div>
            </div>
            <div v-else-if="traceRow?.response_body" class="ud-json-view">
              <pre>{{ fmtJSON(traceRow.response_body) }}</pre>
            </div>
            <div v-else class="ud-trace-empty">无响应数据</div>
          </el-tab-pane>

          <el-tab-pane label="原始请求" name="raw_req">
            <div v-if="traceRow?.request_body" class="ud-json-view">
              <pre>{{ fmtJSON(traceRow.request_body) }}</pre>
            </div>
            <div v-else class="ud-trace-empty">无原始请求数据</div>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import {
  Refresh, DataLine, Coin, Money, User, ArrowRight, ArrowUp, ArrowDown,
  Bottom, Top, List, Search, View, InfoFilled, Finished, CircleCheck,
} from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'
import { useRoute } from 'vue-router'
import http from '@/services/http'

Chart.register(...registerables)

const authStore = useAuthStore()
const orgStore  = useOrgStore()
const route     = useRoute()

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

// ── 日期 ──────────────────────────────────────────────────────
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }

const dateRange = ref<[string, string]>([fmt(daysAgo(29)), fmt(today)])
const activePreset = ref('近30天')

const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近7天',  days: 6 },
  { label: '近30天', days: 29 },
  { label: '近90天', days: 89 },
]

function applyPreset(p: { label: string; days: number }) {
  dateRange.value = [fmt(daysAgo(p.days)), fmt(today)]
  activePreset.value = p.label
  loadAll()
}

function onDateChange() {
  activePreset.value = ''
  loadAll()
}

// ── 筛选 ──────────────────────────────────────────────────────
const filterUserName = ref('')
const filterModel    = ref('')
const filterStatus   = ref('')

const activeFilterCount = computed(() =>
  [filterUserName.value, filterModel.value, filterStatus.value].filter(Boolean).length
)

function clearFilters() {
  filterUserName.value = ''
  filterModel.value    = ''
  filterStatus.value   = ''
  // 如果当前处于个人视角（从成员详情跳入），同步退出
  if (breadcrumbs.value[breadcrumbs.value.length - 1]?.scope === 'user') {
    breadcrumbs.value = []
  }
  reloadAll()
}

// ── 角色 / 范围标签 ──────────────────────────────────────────
const scopeLabel = computed(() => {
  const map: Record<string, string> = {
    admin: '全局管理员',
    studio_admin: '工作室管理员',
    dept_admin: '部门管理员',
  }
  return map[authStore.effectiveRole || ''] || authStore.userName || '管理视角'
})

const topLabel = computed(() => {
  if (authStore.effectiveRole === 'dept_admin') return '我的部门'
  if (authStore.effectiveRole === 'studio_admin') return '我的工作室'
  return '全公司'
})

const rankTitle = computed(() => {
  const map: Record<string, string> = {
    company: '工作室成本排行',
    studio:  '部门成本排行',
    dept:    '成员成本排行',
    user:    '模型用量',
  }
  return map[currentScope.value] || '成本排行'
})

// ── 汇总 / 排行 ──────────────────────────────────────────────
const summaryLoading = ref(false)
const summary = ref<any>(null)
const breakdown = ref<any[]>([])
const rankPage = ref(1)
const rankPageSize = 20
const rankTotal = ref(0)
const breadcrumbs = ref<Array<{ scope: string; id: string; name: string }>>([])

const currentScope = computed(() => {
  if (breadcrumbs.value.length > 0)
    return breadcrumbs.value[breadcrumbs.value.length - 1].scope
  if (authStore.effectiveRole === 'dept_admin') return 'dept'
  if (authStore.effectiveRole === 'studio_admin') return 'studio'
  return 'company'
})

const canDrill = computed(() =>
  currentScope.value === 'company' || currentScope.value === 'studio'
)

const maxCost = computed(() =>
  Math.max(1, ...breakdown.value.map(r => r.cost_estimate || 0))
)

const avgCostPerCall = computed(() => {
  const s = summary.value
  if (!s?.call_count) return '0.00'
  return fmtCost((s.cost_estimate || 0) / s.call_count)
})

const successRate = computed(() => {
  const s = summary.value
  if (!s?.call_count) return 100
  return ((s.call_count - (s.error_count ?? 0)) / s.call_count) * 100
})

// 环比
const costChange = computed(() => {
  const curr = summary.value?.cost_estimate ?? 0
  const prev = summary.value?.prev_cost_estimate ?? 0
  if (!prev) return null
  return ((curr - prev) / prev) * 100
})

const callsChange = computed(() => {
  const curr = summary.value?.call_count ?? 0
  const prev = summary.value?.prev_call_count ?? 0
  if (!prev) return null
  return ((curr - prev) / prev) * 100
})

function fmtChange(pct: number | null): string {
  if (pct === null) return '—'
  return `${pct >= 0 ? '+' : ''}${pct.toFixed(1)}%`
}

// 上期日期范围标签（用于 tooltip）
const prevPeriodLabel = computed(() => {
  const [start, end] = dateRange.value
  if (!start || !end) return ''
  const s = new Date(start), e = new Date(end)
  const days = Math.round((e.getTime() - s.getTime()) / 86400000) + 1
  const prevEnd = new Date(s); prevEnd.setDate(prevEnd.getDate() - 1)
  const prevStart = new Date(prevEnd); prevStart.setDate(prevStart.getDate() - days + 1)
  const f = (d: Date) => d.toISOString().slice(0, 10)
  return `上期：${f(prevStart)} ~ ${f(prevEnd)}（${days} 天）`
})

function changeClass(pct: number | null, costMode = false): string {
  if (pct === null) return 'ud-change--neutral'
  if (pct === 0) return 'ud-change--neutral'
  const isUp = pct > 0
  if (costMode) return isUp ? 'ud-change--bad' : 'ud-change--good'
  return isUp ? 'ud-change--up' : 'ud-change--down'
}

async function loadSummary() {
  summaryLoading.value = true
  try {
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    const params: Record<string, any> = {
      scope: currentScope.value,
      page: rankPage.value,
      page_size: rankPageSize,
      start: dateRange.value[0],
      end: dateRange.value[1],
    }
    if (last?.id) params.id = last.id
    if (filterModel.value) params.model_name = filterModel.value
    if (filterStatus.value) params.status = filterStatus.value
    const res = await http.get('/api/v1/usage/dashboard', { params })
    summary.value = res.data?.summary ?? null
    breakdown.value = res.data?.breakdown ?? []
    rankTotal.value = res.data?.total ?? 0
  } catch {
    summary.value = null
    breakdown.value = []
  } finally {
    summaryLoading.value = false
  }
}

function drillDown(row: any) {
  const next: Record<string, string> = { company: 'studio', studio: 'dept' }
  const nextScope = next[currentScope.value]
  if (!nextScope) return
  breadcrumbs.value.push({ scope: nextScope, id: row.id, name: row.name || row.id })
  rankPage.value = 1
  loadSummary()
}

function popToLevel(idx: number) {
  breadcrumbs.value = breadcrumbs.value.slice(0, idx + 1)
  rankPage.value = 1
  loadSummary()
}

function resetToTop() {
  breadcrumbs.value = []
  rankPage.value = 1
  loadSummary()
}

// ── 趋势图 ────────────────────────────────────────────────────
const trendLoading = ref(false)
const trendData    = ref<any[]>([])
const trendCanvasRef = ref<HTMLCanvasElement | null>(null)
let trendChartInst: Chart | null = null

const trendMetric = ref<'overview' | 'cost' | 'calls' | 'tokens'>('overview')
const trendMetrics = [
  { value: 'overview', label: '综合' },
  { value: 'cost',     label: '成本' },
  { value: 'calls',    label: '调用量' },
  { value: 'tokens',   label: 'Token' },
] as const

function switchTrendMetric(m: typeof trendMetric.value) {
  trendMetric.value = m
  renderTrendChart()
}

async function loadTrend() {
  trendLoading.value = true
  try {
    const trendParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    if (last?.scope === 'user' && last?.id) trendParams.user_id = last.id
    if (filterModel.value) trendParams.model_name = filterModel.value
    if (filterStatus.value) trendParams.status = filterStatus.value
    const res = await http.get('/api/v1/usage/dashboard/trend', { params: trendParams })
    trendData.value = res.data?.rows ?? []
  } catch {
    trendData.value = []
  } finally {
    trendLoading.value = false
    await nextTick()
    renderTrendChart()
  }
}

function renderTrendChart() {
  if (!trendCanvasRef.value) return
  trendChartInst?.destroy()
  const data = trendData.value
  if (!data.length) return

  const labels     = data.map(d => d.date?.slice(5) ?? '')
  const costs      = data.map(d => d.cost ?? 0)
  const calls      = data.map(d => d.calls ?? 0)
  const errors     = data.map(d => d.errors ?? 0)
  const tokens     = data.map(d => d.total_tokens ?? 0)
  const showPoints = data.length <= 31
  const metric     = trendMetric.value

  // 综合模式：成本（折线，左轴）+ 调用（柱，右轴）
  if (metric === 'overview') {
    trendChartInst = new Chart(trendCanvasRef.value, {
      data: {
        labels,
        datasets: [
          {
            type: 'line' as const, label: '成本 (USD)', data: costs,
            yAxisID: 'yCost', borderColor: '#67c23a', backgroundColor: 'rgba(103,194,58,0.10)',
            fill: true, tension: 0.35, pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2, order: 1,
          },
          {
            type: 'bar' as const, label: '调用次数', data: calls,
            yAxisID: 'yCalls', backgroundColor: 'rgba(64,158,255,0.45)', borderRadius: 3, order: 2,
          },
          {
            type: 'bar' as const, label: '失败次数', data: errors,
            yAxisID: 'yCalls', backgroundColor: 'rgba(245,108,108,0.7)', borderRadius: 3, order: 3,
          },
        ],
      },
      options: {
        responsive: true, maintainAspectRatio: false,
        interaction: { intersect: false, mode: 'index' },
        scales: {
          x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
          yCost: {
            type: 'linear', position: 'left',
            grid: { color: 'rgba(0,0,0,0.05)' },
            ticks: { font: { size: 11 }, callback: (v: any) => `$${fmtCost(v)}` },
          },
          yCalls: {
            type: 'linear', position: 'right',
            grid: { drawOnChartArea: false },
            ticks: { font: { size: 11 }, callback: (v: any) => Number(v).toLocaleString() },
          },
        },
        plugins: {
          legend: { position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
          tooltip: {
            callbacks: {
              label: (ctx: any) => ctx.dataset.yAxisID === 'yCost'
                ? ` 成本: $${fmtCost(ctx.raw)}`
                : ` ${ctx.dataset.label}: ${Number(ctx.raw).toLocaleString()}`,
            },
          },
        },
      },
    } as any)
    return
  }

  // 单指标模式
  const metricCfg = {
    cost:   { data: costs,  color: '#67c23a', bgColor: 'rgba(103,194,58,0.12)', label: '成本 (USD)',  fmt: (v: any) => `$${fmtCost(v)}` },
    calls:  { data: calls,  color: '#409eff', bgColor: 'rgba(64,158,255,0.15)', label: '调用次数',    fmt: (v: any) => Number(v).toLocaleString() },
    tokens: { data: tokens, color: '#8b5cf6', bgColor: 'rgba(139,92,246,0.12)', label: 'Token 消耗',  fmt: (v: any) => fmtTokens(v) },
  }[metric]!

  trendChartInst = new Chart(trendCanvasRef.value, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label: metricCfg.label, data: metricCfg.data,
        borderColor: metricCfg.color, backgroundColor: metricCfg.bgColor,
        fill: true, tension: 0.35,
        pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2,
      }],
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      scales: {
        x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
        y: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: { font: { size: 11 }, callback: metricCfg.fmt },
        },
      },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (ctx: any) => ` ${metricCfg.label}: ${metricCfg.fmt(ctx.raw)}` } },
      },
    },
  } as any)
}

// ── 模型分布图 ────────────────────────────────────────────────
const modelsLoading = ref(false)
const modelsData    = ref<any[]>([])
const modelCanvasRef = ref<HTMLCanvasElement | null>(null)
let modelChartInst: Chart | null = null

const MODEL_COLORS = [
  '#409eff', '#67c23a', '#e6a23c', '#f56c6c',
  '#909399', '#8B5CF6', '#06B6D4', '#F59E0B',
  '#10B981', '#EF4444',
]

async function loadModels() {
  modelsLoading.value = true
  try {
    const modelParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    if (last?.scope === 'user' && last?.id) modelParams.user_id = last.id
    if (filterStatus.value) modelParams.status = filterStatus.value
    const res = await http.get('/api/v1/usage/dashboard/models', { params: modelParams })
    modelsData.value = res.data?.rows ?? []
  } catch {
    modelsData.value = []
  } finally {
    modelsLoading.value = false
    await nextTick()
    renderModelChart()
  }
}

function renderModelChart() {
  if (!modelCanvasRef.value) return
  modelChartInst?.destroy()
  const raw = modelsData.value
  if (!raw.length) return

  // Top 6 + 其他聚合
  const TOP_N = 6
  let data: any[]
  if (raw.length > TOP_N) {
    const top  = raw.slice(0, TOP_N)
    const rest = raw.slice(TOP_N)
    const restCost  = rest.reduce((s: number, d: any) => s + (d.cost ?? 0), 0)
    const restCalls = rest.reduce((s: number, d: any) => s + (d.calls ?? 0), 0)
    data = [...top, { model: `其他 (${rest.length} 种)`, cost: restCost, calls: restCalls }]
  } else {
    data = raw
  }

  const labels = data.map(d => {
    const m = d.model ?? ''
    return m.length > 28 ? m.slice(0, 26) + '…' : m
  })
  const costs  = data.map(d => d.cost ?? 0)
  const colors = data.map((_: any, i: number) => MODEL_COLORS[i % MODEL_COLORS.length])

  modelChartInst = new Chart(modelCanvasRef.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: '成本 (USD)',
        data: costs,
        backgroundColor: colors,
        borderRadius: 4,
        borderSkipped: false,
      }],
    },
    options: {
      indexAxis: 'y' as const,
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (ctx: any) => ` $${fmtCost(ctx.raw)}`,
          },
        },
      },
      scales: {
        x: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: {
            font: { size: 11 },
            callback: (v: any) => `$${fmtCost(v)}`,
          },
        },
        y: {
          grid: { display: false },
          ticks: { font: { size: 11 } },
        },
      },
    },
  })
}

// ── 调用明细 ──────────────────────────────────────────────────
const logsLoading  = ref(false)
const logRows      = ref<any[]>([])
const logsTotal    = ref(0)
const logsPage     = ref(1)
const logsPageSize = ref(50)

const logMaxCost = computed(() => Math.max(...logRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const logCostColor = (v: number) =>
  v > logMaxCost.value * 0.7 ? 'c-red fw6' : v > logMaxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

const reasoningType = (effort?: string) => {
  if (!effort) return ''
  if (effort === 'high') return 'danger'
  if (effort === 'medium') return 'warning'
  return 'info'
}

async function loadLogs() {
  logsLoading.value = true
  try {
    const params: Record<string, any> = {
      start: dateRange.value[0],
      end:   dateRange.value[1],
      page:  logsPage.value,
      page_size: logsPageSize.value,
    }
    // 当面包屑下钻到具体用户时，精确按 user_id 过滤（优先于姓名搜索）
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    if (last?.scope === 'user' && last?.id) {
      params.user_id = last.id
    } else {
      if (filterUserName.value) params.user_name = filterUserName.value
    }
    if (filterModel.value)    params.model_name = filterModel.value
    if (filterStatus.value)   params.status     = filterStatus.value
    const res = await http.get('/api/v1/usage/scoped/logs', { params })
    logRows.value  = res.data?.rows  ?? []
    logsTotal.value = res.data?.total ?? 0
  } catch {
    logRows.value = []
    logsTotal.value = 0
  } finally {
    logsLoading.value = false
  }
}

function reloadLogs() {
  logsPage.value = 1
  loadLogs()
}

async function onUserNameChange() {
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (last?.scope === 'user') {
    breadcrumbs.value = []
  }
  if (filterUserName.value) {
    try {
      const results = await orgStore.searchUsers(filterUserName.value, 1)
      if (results.length > 0) {
        breadcrumbs.value = [{ scope: 'user' as const, id: results[0].id, name: results[0].name }]
      } else {
        // 未匹配到用户 → 设一个不存在的 user_id，让图表返回空
        breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
      }
    } catch {
      breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
    }
  }
  loadAll()
}

function onUserNameClear() {
  // 清空成员姓名时，同样退出 user scope
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (last?.scope === 'user') {
    breadcrumbs.value = []
    loadAll()  // 退出 user scope 后刷新全部数据
    return
  }
  reloadLogs()
}

function loadAll() {
  logsPage.value = 1
  loadSummary()
  loadTrend()
  loadModels()
  loadLogs()
}

// ── Trace Drawer ──────────────────────────────────────────────
const traceVisible = ref(false)
const traceRow     = ref<any | null>(null)
const traceTab     = ref('meta')

function openTrace(row: any) {
  traceRow.value = row
  traceTab.value = row.request_body ? 'prompt' : 'meta'
  traceVisible.value = true
}

function closeTrace() { traceVisible.value = false }

const traceMessages = computed(() => {
  const body = traceRow.value?.request_body
  if (!body) return []
  try {
    const obj = typeof body === 'string' ? JSON.parse(body) : body
    return obj?.messages || []
  } catch { return [] }
})

const traceResponseText = computed(() => {
  const body = traceRow.value?.response_body
  if (!body) return ''
  try {
    const obj = typeof body === 'string' ? JSON.parse(body) : body
    const choices = obj?.choices
    if (Array.isArray(choices) && choices.length > 0) {
      return choices[0]?.message?.content ?? choices[0]?.delta?.content ?? ''
    }
  } catch { /* ignore */ }
  return ''
})

// ── 格式化 ────────────────────────────────────────────────────
const fmtNum = (n?: number | null) => (n ?? 0).toLocaleString()
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const fmtCost = (v?: number | null): string => {
  const n = Number(v || 0)
  if (n === 0) return '0.00'
  if (n >= 1)        return n.toFixed(2)
  if (n >= 0.01)     return n.toFixed(4)
  if (n >= 0.0001)   return n.toFixed(6)
  return n.toFixed(8)
}
const fmtPrice = (n?: number | null) => {
  if (n == null) return '—'
  if (n === 0) return '0'
  return n >= 1 ? n.toFixed(2) : n.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
}
const fmtDateTime = (s?: string) =>
  s ? s.replace('T', ' ').replace(/\.\d+Z$/, '').slice(0, 19) : '—'
const fmtDuration = (ms?: number | null) => {
  if (ms == null) return '—'
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(2)}s`
}
const barPct = (v: number, max: number) => !max || !v ? 0 : Math.round(v / max * 100)
const fmtJSON = (v: any): string => {
  if (v == null) return '(无数据)'
  try {
    const obj = typeof v === 'string' ? JSON.parse(v) : v
    return JSON.stringify(obj, null, 2)
  } catch { return String(v) }
}

onMounted(() => {
  // 从组织架构页跳转时，预填成员筛选
  const { user_id, user_name, start, end } = route.query
  if (user_id && user_name) {
    // 将 breadcrumbs 设置到 user 层级，让看板直接展示该成员视角
    breadcrumbs.value = [{ scope: 'user', id: String(user_id), name: String(user_name) }]
    // 同步回显到筛选栏，让用户看到当前按谁过滤
    filterUserName.value = String(user_name)
  }
  if (start && end) {
    dateRange.value = [String(start), String(end)]
  }
  loadAll()
})
onUnmounted(() => {
  trendChartInst?.destroy()
  modelChartInst?.destroy()
})
</script>

<style scoped>
/* ── 根容器 ── */
.ud {
  --ud-input:  #2BA8A2;
  --ud-output: #27AE60;
  --ud-cache:  #5DADE2;
  --ud-cost:   #C7900A;
  --ud-warn:   #E0A100;
  --ud-danger: #E74C3C;
  --ud-purple: #8B5CF6;
  --ud-amber:  #F59E0B;

  --ud-surface: var(--el-fill-color-blank);
  --ud-border:  var(--el-border-color-light);
  --ud-muted:   var(--el-text-color-secondary);
  --ud-faint:   var(--el-text-color-placeholder);
  --ud-ink:     var(--el-text-color-regular);
  --ud-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 16px;
}

/* ── 页头 ── */
.ud-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.ud-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.ud-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.ud-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.ud-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1E8C86));
}
.ud-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.ud-filter, .ud-section {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 14px;
}

/* ── 筛选栏 ── */
.ud-filter { display: flex; flex-direction: column; overflow: hidden; }

.ud-filter-row {
  display: flex; align-items: center; gap: 10px; padding: 12px 20px;
  flex-wrap: wrap;
}
.ud-filter-row--date { border-bottom: 1px solid var(--ud-border); }
.ud-filter-row--dims { padding-top: 10px; padding-bottom: 10px; gap: 14px; }

.ud-presets { display: flex; align-items: center; gap: 4px; }
.ud-preset-btn {
  padding: 3px 10px; border-radius: 20px;
  border: 1px solid #dcdfe6; background: #f5f7fa;
  color: #606266; font-size: 13px; cursor: pointer;
  transition: all .15s; white-space: nowrap; line-height: 1.6;
}
.ud-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.ud-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.ud-filter__date { width: 220px; flex-shrink: 0; }
.ud-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 4px; flex-shrink: 0; }

.ud-filter-chip { display: flex; align-items: center; gap: 6px; }
.ud-chip-label { font-size: 12px; color: var(--ud-muted); white-space: nowrap; }
.ud-chip-input { width: 140px; }
.ud-chip-select { width: 100px; }

/* ── KPI 卡片 ── */
.ud-kpi-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
}
@media (max-width: 900px) { .ud-kpi-row { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 540px) { .ud-kpi-row { grid-template-columns: 1fr; } }

.ud-kpi-card {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 14px;
  padding: 18px 20px 16px;
  display: flex; align-items: flex-start; gap: 14px;
  position: relative; overflow: hidden;
}
.ud-kpi-card::before {
  content: ''; position: absolute; top: 0; left: 0;
  width: 4px; height: 100%; border-radius: 14px 0 0 14px;
}
.ud-kpi-card--green::before { background: #67c23a; }
.ud-kpi-card--blue::before  { background: #409eff; }
.ud-kpi-card--amber::before { background: #e6a23c; }
.ud-kpi-card--red::before   { background: #f56c6c; }
.ud-kpi-card--purple::before { background: #8B5CF6; }

.ud-kpi__icon {
  width: 40px; height: 40px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.ud-kpi-card--green  .ud-kpi__icon { background: rgba(103,194,58,0.12); color: #67c23a; }
.ud-kpi-card--blue   .ud-kpi__icon { background: rgba(64,158,255,0.12); color: #409eff; }
.ud-kpi-card--amber  .ud-kpi__icon { background: rgba(230,162,60,0.12); color: #e6a23c; }
.ud-kpi-card--red    .ud-kpi__icon { background: rgba(245,108,108,0.12); color: #f56c6c; }
.ud-kpi-card--purple .ud-kpi__icon { background: rgba(139,92,246,0.12); color: #8B5CF6; }

.ud-kpi__body { flex: 1; min-width: 0; }
.ud-kpi__label { font-size: 12px; color: var(--ud-muted); margin: 0 0 4px; }
.ud-kpi__value { font-size: 24px; font-weight: 700; margin: 0 0 6px; color: var(--el-text-color-primary); line-height: 1.2; }
.ud-kpi__foot  { display: flex; align-items: center; gap: 6px; font-size: 12px; margin: 0; }
.ud-kpi__vs    { color: var(--ud-faint); }
.ud-kpi__sub   { color: var(--ud-muted); }

.ud-change-chip     { display: inline-flex; align-items: center; gap: 3px; cursor: default; }
.ud-change--good    { color: #67c23a; font-weight: 600; }
.ud-change--bad     { color: #f56c6c; font-weight: 600; }
.ud-change--up      { color: #409eff; font-weight: 600; }
.ud-change--down    { color: #e6a23c; font-weight: 600; }
.ud-change--neutral { color: var(--ud-muted); }

/* ── Section 通用 ── */
.ud-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 20px 12px; border-bottom: 1px solid var(--ud-border);
  flex-wrap: wrap; gap: 8px;
}
.ud-section__title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-section__count { font-size: 12px; color: var(--ud-muted); }
.ud-section__hint  { font-size: 12px; color: var(--ud-faint); }

/* 趋势图指标切换 */
.ud-trend-tabs { display: flex; gap: 2px; background: var(--el-fill-color); border-radius: 6px; padding: 2px; }
.ud-trend-tab {
  padding: 3px 10px; font-size: 12px; border-radius: 4px; border: none; cursor: pointer;
  background: transparent; color: var(--el-text-color-secondary); transition: all .15s;
}
.ud-trend-tab:hover { color: var(--el-text-color-primary); }
.ud-trend-tab--active {
  background: var(--el-bg-color); color: var(--el-color-primary);
  font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,.1);
}

/* ── 趋势图 ── */
.ud-chart-wrap {
  padding: 16px 20px;
  height: 260px;
  position: relative;
}
.ud-chart-wrap--model { height: 280px; }
.ud-trend-canvas { width: 100% !important; height: 100% !important; }
.ud-model-canvas { width: 100% !important; height: 100% !important; }
.ud-chart-empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--ud-faint); font-size: 13px;
}

/* ── 两列布局 ── */
.ud-main-row {
  display: grid;
  grid-template-columns: 3fr 2fr;
  gap: 16px;
}
@media (max-width: 960px) { .ud-main-row { grid-template-columns: 1fr; } }

/* ── 排行表格 ── */
.ud-table-wrap { padding: 0; overflow: auto; }
.ud-rank-table {
  width: 100%; border-collapse: collapse;
  font-size: 13px; table-layout: auto;
}
.ud-rank-table thead tr { border-bottom: 1px solid var(--ud-border); }
.ud-rank-table th {
  padding: 10px 14px; text-align: left;
  font-weight: 600; font-size: 12px;
  color: var(--ud-muted); white-space: nowrap;
  background: var(--el-fill-color-light);
}
.ud-rank-table td { padding: 10px 14px; border-bottom: 1px solid var(--ud-border); }
.ud-rank-table tbody tr:last-child td { border-bottom: none; }
.ud-rank-table tbody tr:hover td { background: var(--el-fill-color-extra-light); }
.ud-rank-drillable td { cursor: pointer; }

.col-rank { width: 40px; text-align: center; }
.col-name { min-width: 120px; }
.col-num  { text-align: right; white-space: nowrap; }

.ud-rank-num {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: 6px;
  background: var(--el-fill-color); color: var(--ud-muted);
  font-size: 12px; font-weight: 600;
}
.ud-rank-num--top1 { background: #ffd700; color: #7a5f00; }
.ud-rank-num--top2 { background: #c0c0c0; color: #555; }
.ud-rank-num--top3 { background: #cd7f32; color: #fff; }

.ud-row-name { color: var(--el-text-color-primary); }
.ud-row-name.drillable { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-row-name.drillable:hover { text-decoration: underline; }

.ud-bar-wrap { display: flex; align-items: center; gap: 8px; min-width: 120px; }
.ud-bar-bg   { flex: 1; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; }
.ud-bar-fill { height: 100%; border-radius: 3px; transition: width .3s; background: var(--el-color-primary); }
.ud-bar-fill--cost { background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-bar-pct  { font-size: 12px; color: var(--ud-muted); white-space: nowrap; min-width: 36px; }
.ud-empty    { padding: 32px; text-align: center; color: var(--ud-faint); }

/* ── 面包屑 ── */
/* 个人视角提示条 */
.ud-user-scope-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 14px; margin-bottom: 12px;
  background: #ecf5ff; border: 1px solid #b3d8ff;
  border-radius: 6px; font-size: 13px; color: #409eff;
}
.ud-user-scope-banner strong { color: #1a6fd4; }

.ud-breadcrumb { display: flex; align-items: center; gap: 4px; font-size: 13px; flex-wrap: wrap; }
.ud-bc-home { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-bc-home:hover { text-decoration: underline; }
.ud-bc-sep  { color: var(--ud-faint); font-size: 11px; }
.ud-bc-item { color: var(--ud-muted); cursor: pointer; }
.ud-bc-item:hover { color: var(--el-color-primary); }
.ud-bc-current { color: var(--el-text-color-primary); font-weight: 500; cursor: default; }

/* ── 分页 ── */
.ud-pagination { display: flex; justify-content: flex-end; padding: 12px 16px; }

/* ── 调用明细列样式 ── */
.ud-key-name    { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-member-name { font-size: 13px; color: var(--el-text-color-regular); }
.ud-studio-name { font-size: 13px; color: var(--el-text-color-regular); }
.ud-dept-name   { font-size: 13px; color: var(--el-text-color-regular); }

.ud-model-cell { display: flex; flex-direction: column; gap: 2px; }
.ud-model-name { font-size: 13px; color: var(--el-text-color-primary); font-family: var(--ud-mono); }
.ud-model-name--muted { color: var(--ud-muted); font-size: 12px; }
.ud-model-routed { display: flex; align-items: center; gap: 4px; }
.ud-arrow { color: var(--ud-faint); font-size: 11px; }

.ud-token-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ud-token-item { display: inline-flex; align-items: center; gap: 3px; font-size: 12px; }
.ud-token-item--in    { color: var(--ud-input); }
.ud-token-item--out   { color: var(--ud-output); }
.ud-token-item--cache { color: var(--ud-cache); }
.ud-token-icon { font-size: 11px; }
.ud-info { color: var(--ud-faint); cursor: pointer; font-size: 13px; vertical-align: middle; }

.ud-cost-cell { display: flex; align-items: center; justify-content: flex-end; gap: 4px; }
.ud-cost { font-size: 13px; }
.ud-duration { font-size: 12px; font-family: var(--ud-mono); color: var(--ud-muted); }
.ud-dash { color: var(--ud-faint); }
.ud-time { font-size: 12px; color: var(--ud-muted); font-family: var(--ud-mono); }
.ud-ip   { font-size: 12px; font-family: var(--ud-mono); color: var(--ud-muted); }
.ud-flags { display: flex; gap: 4px; flex-wrap: wrap; justify-content: center; }
.ud-tag--effort { text-transform: uppercase; letter-spacing: .3px; }
.ud-source { display: flex; flex-direction: column; gap: 2px; }

/* ── 状态过滤 ── */
.ud-status-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
.ud-status-dot--ok  { background: #67c23a; }
.ud-status-dot--err { background: #f56c6c; }

/* ── 空状态 ── */
.ud-empty-block { padding: 48px; text-align: center; color: var(--ud-faint); }
.ud-empty-block p { margin-top: 12px; font-size: 14px; }

/* ── 颜色工具 ── */
.c-blue   { color: #409eff; }
.c-green  { color: #67c23a; }
.c-orange { color: #e6a23c; }
.c-red    { color: #f56c6c; }
.c-cyan   { color: #5DADE2; }
.c-amber  { color: var(--ud-amber); }
.c-purple { color: var(--ud-purple); }
.fw6      { font-weight: 600; }

/* ── Tooltip ── */
:deep(.ud-tt-popper) { padding: 0 !important; }
.ud-tt { padding: 10px 14px; min-width: 200px; }
.ud-tt__title { font-size: 12px; font-weight: 600; color: var(--el-text-color-primary); margin-bottom: 8px; }
.ud-tt__group { display: flex; align-items: center; gap: 5px; font-size: 11px; color: var(--ud-muted); margin: 6px 0 3px; }
.ud-tt__group-ic { font-size: 12px; }
.ud-tt__row { display: flex; justify-content: space-between; gap: 24px; font-size: 12px; padding: 1px 0; }
.ud-tt__row span { color: var(--ud-muted); }
.ud-tt__row b { font-weight: 600; }
.ud-tt__sub { padding-left: 10px; }
.ud-tt__sep { height: 1px; background: var(--ud-border); margin: 5px 0; }

/* ── Trace Drawer ── */
.ud-trace-header { display: flex; flex-direction: column; gap: 3px; }
.ud-trace-title  { font-size: 15px; font-weight: 600; }
.ud-trace-id     { font-size: 11px; color: var(--ud-muted); font-family: var(--ud-mono); }
.ud-trace-body   { padding: 0; }
.ud-trace-meta   { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 14px 20px; border-bottom: 1px solid var(--ud-border); }
.ud-trace-time   { font-size: 12px; color: var(--ud-muted); margin-left: 6px; }
.ud-trace-dur    { font-size: 12px; color: var(--ud-muted); font-family: var(--ud-mono); }
.ud-trace-tabs   { padding: 0 20px; }
.ud-trace-empty  { padding: 32px; text-align: center; color: var(--ud-faint); }

.ud-meta-grid    { display: grid; grid-template-columns: repeat(2, 1fr); gap: 20px; padding: 16px 0; }
.ud-meta-section { }
.ud-meta-heading { font-size: 12px; font-weight: 600; color: var(--ud-muted); text-transform: uppercase; letter-spacing: .5px; margin-bottom: 8px; }
.ud-meta-row     { display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 4px 0; font-size: 13px; border-bottom: 1px solid var(--ud-border); }
.ud-meta-row span:first-child { color: var(--ud-muted); flex-shrink: 0; }
.ud-meta-sep { border-color: var(--ud-muted) !important; }
.ud-meta-row code { font-family: var(--ud-mono); font-size: 12px; background: var(--el-fill-color); padding: 1px 4px; border-radius: 4px; }

.ud-msgs         { display: flex; flex-direction: column; gap: 8px; padding: 8px 0; }
.ud-msg          { border-radius: 8px; overflow: hidden; }
.ud-msg--system  { background: var(--el-fill-color-light); }
.ud-msg--user    { background: var(--el-color-primary-light-9); }
.ud-msg--assistant { background: var(--el-fill-color); }
.ud-msg-role     { display: block; padding: 4px 10px; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .4px; color: var(--ud-muted); border-bottom: 1px solid var(--ud-border); }
.ud-msg-content  { margin: 0; padding: 10px; font-size: 13px; font-family: var(--ud-mono); white-space: pre-wrap; word-break: break-all; color: var(--el-text-color-primary); }
.ud-json-view    { background: var(--el-fill-color-light); border-radius: 8px; overflow: auto; max-height: 60vh; }
.ud-json-view pre { margin: 0; padding: 14px; font-size: 12px; font-family: var(--ud-mono); white-space: pre-wrap; word-break: break-all; color: var(--el-text-color-primary); }
</style>
