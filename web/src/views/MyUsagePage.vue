<template>
  <div class="mu">

    <!-- ═══════════════════════════════════════════
         页头
    ═══════════════════════════════════════════ -->
    <div class="mu-header">
      <div class="mu-header__left">
        <h2 class="mu-title">我的用量</h2>
        <span class="mu-badge">{{ authStore.userName || '个人记录' }}</span>
      </div>
      <div class="mu-header__right">
        <el-button :loading="statsLoading" :icon="Refresh" circle size="small" title="刷新" @click="loadAll" />
        <el-button :icon="Download" size="small" :disabled="!logRows.length" @click="handleExport">导出</el-button>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         筛选栏
    ═══════════════════════════════════════════ -->
    <div class="mu-filter">

      <!-- 快捷时段 -->
      <div class="mu-presets">
        <button
          v-for="p in datePresets" :key="p.label"
          class="mu-preset-btn"
          :class="{ 'mu-preset-btn--active': activePreset === p.label }"
          @click="applyPreset(p)"
        >{{ p.label }}</button>
      </div>

      <div class="mu-filter-divider" />

      <!-- 自定义日期 -->
      <el-date-picker
        v-model="dateRange"
        type="daterange"
        range-separator="~"
        start-placeholder="开始"
        end-placeholder="结束"
        value-format="YYYY-MM-DD"
        format="MM/DD"
        :shortcuts="dateShortcuts"
        class="mu-filter__date"
        size="small"
        @change="onDateChange"
      />

      <div class="mu-filter-divider" />

      <!-- 中：维度搜索 -->
      <el-input v-model="filterGroupName" placeholder="模型组" clearable size="small"
        class="mu-filter__chip" @change="loadAll" @clear="loadAll">
        <template #prefix><el-icon><Grid /></el-icon></template>
      </el-input>

      <el-input v-model="filterModelName" placeholder="模型名" clearable size="small"
        class="mu-filter__chip" @change="loadAll" @clear="loadAll">
        <template #prefix><el-icon><Cpu /></el-icon></template>
      </el-input>

      <el-select v-model="filterKeyId" placeholder="选择 Key" clearable size="small"
        class="mu-filter__chip mu-filter__chip--key" filterable @change="loadAll" @clear="loadAll">
        <el-option v-for="k in myKeyOptions" :key="k.value" :label="k.label" :value="k.value" />
      </el-select>

      <div class="mu-filter-divider" />

      <!-- 右：状态 + 清空 -->
      <el-select v-model="filterStatus" placeholder="状态" clearable size="small"
        class="mu-filter__status" @change="loadAll" @clear="loadAll">
        <el-option label="全部" value="" />
        <el-option value="success">
          <span class="mu-dot mu-dot--ok" /> 成功
        </el-option>
        <el-option value="error">
          <span class="mu-dot mu-dot--err" /> 失败
        </el-option>
      </el-select>

      <el-button v-if="activeFilterCount > 0" size="small" plain type="warning"
        :icon="Close" @click="clearFilters" class="mu-filter__clear">
        清空 {{ activeFilterCount }}
      </el-button>

    </div>

    <!-- ═══════════════════════════════════════════
         统计卡片
    ═══════════════════════════════════════════ -->
    <div class="mu-stats" v-loading="statsLoading">
      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--blue">
          <el-icon size="22"><DataLine /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">总调用次数</p>
          <p class="mu-stat-card__value">{{ fmtNum(statsTotal?.req_count) }}</p>
          <p class="mu-stat-card__sub">
            成功
            <span :class="['mu-stat-card__rate', successRateClass]">{{ successRate }}%</span>
          </p>
        </div>
      </div>

      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--amber">
          <el-icon size="22"><Coin /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">总 Token</p>
          <p class="mu-stat-card__value">{{ fmtTokens((statsTotal?.prompt_tokens ?? 0) + (statsTotal?.completion_tokens ?? 0)) }}</p>
          <p class="mu-stat-card__sub">
            输入 <span class="c-blue">{{ fmtTokens(statsTotal?.prompt_tokens) }}</span>
            · 输出 <span class="c-green">{{ fmtTokens(statsTotal?.completion_tokens) }}</span>
          </p>
        </div>
      </div>

      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--cyan">
          <el-icon size="22"><MagicStick /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">缓存命中</p>
          <p class="mu-stat-card__value">{{ fmtTokens(statsTotal?.cached_tokens) }}</p>
          <p class="mu-stat-card__sub">
            命中率 <span class="c-cyan">{{ cacheHitPct }}%</span>
          </p>
        </div>
      </div>

      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--green">
          <el-icon size="22"><Money /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">估算成本 (USD)</p>
          <p class="mu-stat-card__value c-green">${{ fmtCost(statsTotal?.est_cost_usd) }}</p>
          <p class="mu-stat-card__sub">
            均次 <span class="c-orange">${{ avgCostPerReq }}</span>
          </p>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         图表区
    ═══════════════════════════════════════════ -->
    <div class="mu-charts" v-if="statsRows.length">
      <div class="mu-chart-card">
        <div class="mu-chart-card__header">
          <span class="mu-chart-card__title">用量趋势</span>
          <el-radio-group v-model="chartMetric" size="small" @change="renderTrend">
            <el-radio-button value="tokens">Token</el-radio-button>
            <el-radio-button value="reqs">调用</el-radio-button>
            <el-radio-button value="cost">成本</el-radio-button>
          </el-radio-group>
        </div>
        <div class="mu-chart-card__body">
          <canvas ref="trendCanvas" />
        </div>
      </div>

      <div class="mu-chart-card">
        <div class="mu-chart-card__header">
          <span class="mu-chart-card__title">Top 模型</span>
          <el-radio-group v-model="distMetric" size="small" @change="renderDist">
            <el-radio-button value="reqs">调用量</el-radio-button>
            <el-radio-button value="tokens">Token</el-radio-button>
          </el-radio-group>
        </div>
        <div class="mu-chart-card__body">
          <canvas ref="distCanvas" />
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         用量汇总表
    ═══════════════════════════════════════════ -->
    <div class="mu-section">
      <div class="mu-section__header">
        <span class="mu-section__title">用量汇总</span>
        <el-radio-group v-model="groupBy" size="small" @change="loadStats">
          <el-radio-button value="day">按天</el-radio-button>
          <el-radio-button value="model">按模型</el-radio-button>
        </el-radio-group>
      </div>
      <div class="mu-table-wrap" v-loading="statsLoading">
        <el-table :data="statsRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%"
          :default-sort="{ prop: 'est_cost_usd', order: 'descending' }">
          <el-table-column v-if="groupBy==='day'" prop="stat_date" label="日期" min-width="110" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-if="groupBy==='model'" prop="model" label="模型" min-width="160" sortable>
            <template #default="{ row }"><span class="mu-model-name">{{ row.model || '—' }}</span></template>
          </el-table-column>
          <el-table-column prop="req_count" label="调用" min-width="80" align="right" sortable>
            <template #default="{ row }">{{ fmtNum(row.req_count) }}</template>
          </el-table-column>
          <el-table-column prop="prompt_tokens" label="输入 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="mu-bar-cell">
                <span>{{ fmtTokens(row.prompt_tokens) }}</span>
                <div class="mu-minibar"><div class="mu-minibar__fill mu-minibar__fill--blue" :style="{ width: pct(row.prompt_tokens, maxPrompt) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="cached_tokens" label="缓存命中" min-width="100" align="right" sortable>
            <template #default="{ row }">
              <span class="c-cyan">{{ fmtTokens(row.cached_tokens) }}</span>
              <span class="mu-pct" v-if="row.prompt_tokens">{{ Math.round(row.cached_tokens / row.prompt_tokens * 100) }}%</span>
            </template>
          </el-table-column>
          <el-table-column prop="completion_tokens" label="输出 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="mu-bar-cell">
                <span>{{ fmtTokens(row.completion_tokens) }}</span>
                <div class="mu-minibar"><div class="mu-minibar__fill mu-minibar__fill--green" :style="{ width: pct(row.completion_tokens, maxCompletion) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="est_cost_usd" label="估算成本" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="mu-bar-cell">
                <span :class="costColor(row.est_cost_usd)">${{ fmtCost(row.est_cost_usd) }}</span>
                <div class="mu-minibar"><div class="mu-minibar__fill mu-minibar__fill--red" :style="{ width: pct(row.est_cost_usd, maxCost) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!statsLoading && !statsRows.length" class="mu-empty">
          <el-icon size="48" color="#c0c4cc"><Histogram /></el-icon>
          <p>暂无汇总数据</p>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         调用明细表
    ═══════════════════════════════════════════ -->
    <div class="mu-section">
      <div class="mu-section__header">
        <span class="mu-section__title">调用明细</span>
        <span class="mu-section__count">共 {{ logTotal }} 条</span>
      </div>
      <div class="mu-table-wrap" v-loading="logLoading">
        <el-table :data="logRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%;cursor:pointer" @row-click="openTrace">
          <!-- Key -->
          <el-table-column prop="key_name" label="Key" min-width="120" fixed="left">
            <template #default="{ row }">
              <span class="mu-name">{{ keyLabel(row.key_id, row.key_name) }}</span>
            </template>
          </el-table-column>
          <!-- 模型 / 路由 -->
          <el-table-column prop="model" label="模型 / 路由" min-width="160">
            <template #default="{ row }">
              <div class="mu-model-cell">
                <span class="mu-model-name">{{ row.model || '—' }}</span>
                <div v-if="row.routed_model && row.routed_model !== row.model" class="mu-model-routed">
                  <span class="mu-arrow">↳</span>
                  <span class="mu-model-name mu-model-name--muted">{{ row.routed_model }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <!-- 模型组 -->
          <el-table-column prop="group_name" label="模型组" min-width="180" class-name="mu-col--wrap">
            <template #default="{ row }">
              <span v-if="row.group_name">{{ row.group_name }}</span>
              <span v-else class="mu-dash">—</span>
            </template>
          </el-table-column>
          <!-- 类型 -->
          <el-table-column label="类型" min-width="70" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.stream" type="primary" size="small" effect="plain">流式</el-tag>
              <el-tag v-else type="" size="small" effect="plain">同步</el-tag>
            </template>
          </el-table-column>
          <!-- Token 用量 -->
          <el-table-column label="Token 用量" min-width="210">
            <template #default="{ row }">
              <div class="mu-token-row">
                <span class="mu-token-item mu-token-item--in" title="输入">
                  <el-icon class="mu-token-icon"><Bottom /></el-icon>{{ fmtTokens(row.prompt_tokens) }}
                </span>
                <span class="mu-token-item mu-token-item--out" title="输出">
                  <el-icon class="mu-token-icon"><Top /></el-icon>{{ fmtTokens(row.completion_tokens) }}
                </span>
                <span v-if="row.cached_tokens" class="mu-token-item mu-token-item--cache" title="缓存命中">
                  <el-icon class="mu-token-icon"><Finished /></el-icon>{{ fmtTokens(row.cached_tokens) }}
                </span>
                <span v-if="row.reasoning_tokens" class="mu-token-item mu-token-item--reason" title="推理">
                  <el-icon class="mu-token-icon"><MagicStick /></el-icon>{{ fmtTokens(row.reasoning_tokens) }}
                </span>
                <el-tooltip placement="top" :show-after="120" popper-class="mu-tt-popper">
                  <template #content>
                    <div class="mu-tt">
                      <div class="mu-tt__title">Token 明细</div>
                      <div class="mu-tt__row"><span>输入（含缓存）</span><b>{{ fmtNum(row.prompt_tokens) }}</b></div>
                      <div class="mu-tt__row"><span>输出</span><b>{{ fmtNum(row.completion_tokens) }}</b></div>
                      <div class="mu-tt__row" v-if="row.reasoning_tokens"><span class="mu-tt__sub">└ 推理</span><b class="c-purple">{{ fmtNum(row.reasoning_tokens) }}</b></div>
                      <div class="mu-tt__sep"></div>
                      <div class="mu-tt__group"><el-icon class="mu-tt__group-ic"><Finished /></el-icon>缓存</div>
                      <div class="mu-tt__row"><span>缓存命中（读）</span><b :class="row.cached_tokens ? 'c-cyan' : ''">{{ fmtNum(row.cached_tokens) }}</b></div>
                      <div class="mu-tt__row"><span>缓存写入</span><b :class="row.cache_write_tokens ? 'c-amber' : ''">{{ fmtNum(row.cache_write_tokens) }}</b></div>
                      <div class="mu-tt__sep"></div>
                      <div class="mu-tt__row"><span>总计</span><b class="c-blue">{{ fmtNum(row.total_tokens) }}</b></div>
                    </div>
                  </template>
                  <span class="mu-info"><el-icon><InfoFilled /></el-icon></span>
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
              <span v-if="row.first_token_ms != null" class="mu-duration">{{ fmtDuration(row.first_token_ms) }}</span>
              <span v-else class="mu-dash">—</span>
            </template>
          </el-table-column>
          <!-- 耗时 -->
          <el-table-column prop="duration_ms" label="耗时" min-width="70" align="right" sortable>
            <template #default="{ row }">
              <span class="mu-duration">{{ fmtDuration(row.duration_ms) }}</span>
            </template>
          </el-table-column>
          <!-- 标记 -->
          <el-table-column label="标记" min-width="90" align="center">
            <template #default="{ row }">
              <div class="mu-flags">
                <el-tag v-if="row.reasoning_effort" :type="reasoningType(row.reasoning_effort)" size="small" effect="plain" class="mu-tag--effort">{{ row.reasoning_effort }}</el-tag>
                <el-tag v-if="row.failover_used" type="warning" size="small" effect="plain">切换</el-tag>
                <el-tag v-if="row.usage_missing" type="info" size="small" effect="plain">无Usage</el-tag>
              </div>
            </template>
          </el-table-column>
          <!-- 费用 -->
          <el-table-column prop="est_cost_usd" label="费用" min-width="140" align="right" sortable>
            <template #default="{ row }">
              <div class="mu-cost-cell">
                <span class="mu-cost" :class="logCostColor(row.est_cost_usd)">${{ fmtCost(row.est_cost_usd) }}</span>
                <el-tooltip placement="left" :show-after="120" popper-class="mu-tt-popper">
                  <template #content>
                    <div class="mu-tt">
                      <div class="mu-tt__title">成本明细 (USD)</div>
                      <template v-if="(row.input_price_snapshot ?? 0) > 0 || (row.output_price_snapshot ?? 0) > 0">
                        <div class="mu-tt__group"><el-icon class="mu-tt__group-ic"><Money /></el-icon>单价 / 1M tokens</div>
                        <div class="mu-tt__row"><span>输入单价</span><b>${{ fmtPrice(row.input_price_snapshot) }}</b></div>
                        <div class="mu-tt__row"><span>输出单价</span><b>${{ fmtPrice(row.output_price_snapshot) }}</b></div>
                        <div class="mu-tt__sep"></div>
                      </template>
                      <div class="mu-tt__group"><el-icon class="mu-tt__group-ic"><Coin /></el-icon>费用</div>
                      <div class="mu-tt__row"><span>输入</span><b>${{ (row.input_cost_usd || 0).toFixed(6) }}</b></div>
                      <div class="mu-tt__row"><span>输出</span><b>${{ (row.output_cost_usd || 0).toFixed(6) }}</b></div>
                      <div class="mu-tt__sep"></div>
                      <div class="mu-tt__group"><el-icon class="mu-tt__group-ic"><Finished /></el-icon>缓存</div>
                      <div class="mu-tt__row"><span>缓存读取</span><b :class="row.cache_read_cost_usd > 0 ? 'c-cyan' : ''">${{ (row.cache_read_cost_usd || 0).toFixed(6) }}</b></div>
                      <div class="mu-tt__sep"></div>
                      <div class="mu-tt__row"><span>合计</span><b class="c-green">${{ fmtCost(row.est_cost_usd) }}</b></div>
                    </div>
                  </template>
                  <span class="mu-info"><el-icon><InfoFilled /></el-icon></span>
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <!-- UA / IP -->
          <el-table-column label="来源" min-width="180">
            <template #default="{ row }">
              <div class="mu-source">
                <el-tooltip v-if="row.user_agent" :content="row.user_agent" placement="top" :show-after="300" popper-class="mu-ua-popper">
                  <span class="mu-ua">{{ row.user_agent }}</span>
                </el-tooltip>
                <span v-else class="mu-dash">—</span>
                <span v-if="row.ip_address" class="mu-ip">{{ row.ip_address }}</span>
              </div>
            </template>
          </el-table-column>
          <!-- 时间 -->
          <el-table-column prop="ts" label="时间" min-width="155" sortable>
            <template #default="{ row }">
              <span class="mu-time">{{ fmtDateTime(row.ts) }}</span>
            </template>
          </el-table-column>
          </el-table>

        <div v-if="!logLoading && !logRows.length" class="mu-empty">
          <el-icon size="48" color="#c0c4cc"><List /></el-icon>
          <p>暂无调用记录</p>
        </div>

        <div v-if="logTotal > 0" class="mu-pagination">
          <el-pagination
            v-model:current-page="logPage"
            v-model:page-size="logPageSize"
            :page-sizes="[20, 50, 100, 200]"
            :total="logTotal"
            layout="total, sizes, prev, pager, next"
            small
            @size-change="loadLogs"
            @current-change="loadLogs"
          />
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         Trace 详情 Drawer
    ═══════════════════════════════════════════ -->
    <el-drawer v-model="traceVisible" title="" size="780px" direction="rtl" :before-close="closeTrace" class="mu-trace-drawer">
      <template #header>
        <div class="mu-trace-header">
          <span class="mu-trace-title">请求详情</span>
          <span class="mu-trace-id">{{ traceRow?.request_id }}</span>
        </div>
      </template>

      <div v-if="traceLoading" class="mu-trace-loading"><el-icon class="is-loading" size="32"><Loading /></el-icon></div>
      <div v-else-if="traceData || traceRow" class="mu-trace-body">

        <div class="mu-trace-meta">
          <el-tag type="info" size="small" effect="plain">{{ traceRow?.model }}</el-tag>
          <el-tag :type="(traceRow?.status || 0) === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ traceRow?.status }}</el-tag>
          <el-tag v-if="traceRow?.stream" type="primary" size="small" effect="plain">流式</el-tag>
          <span class="mu-trace-time">{{ fmtDateTime(traceRow?.ts) }}</span>
          <span class="mu-trace-dur">{{ fmtDuration(traceRow?.duration_ms) }}</span>
        </div>

        <div class="mu-timeline">
          <div v-for="stage in timelineStages" :key="stage.key"
               :class="['mu-tl-node', stage.status, { 'mu-tl-node--total': stage.key === 'done' }]">
            <div class="mu-tl-dot">
              <el-icon v-if="stage.status === 'ok'"><SuccessFilled /></el-icon>
              <el-icon v-else-if="stage.status === 'error'"><CircleCloseFilled /></el-icon>
              <el-icon v-else><Loading /></el-icon>
            </div>
            <div class="mu-tl-label">
              {{ stage.label }}
              <span v-if="stage.key === 'done'" class="mu-tl-measured">实测</span>
            </div>
            <div class="mu-tl-ms" v-if="stage.ms != null">
              {{ stage.ms }}ms
              <el-tooltip v-if="stage.note" :content="stage.note" placement="top">
                <el-icon class="mu-tl-warn"><WarningFilled /></el-icon>
              </el-tooltip>
            </div>
          </div>
        </div>

        <el-tabs v-model="traceTab" class="mu-trace-tabs">
          <el-tab-pane label="调用信息" name="meta">
            <div class="mu-meta-grid">
              <div class="mu-meta-section">
                <div class="mu-meta-heading">路由</div>
                <div class="mu-meta-row"><span>请求模型</span><code>{{ traceRow?.model }}</code></div>
                <div class="mu-meta-row"><span>实际模型</span><code>{{ traceRow?.routed_model || '—' }}</code></div>
                <div class="mu-meta-row"><span>渠道</span><el-tag size="small" type="info" effect="plain">{{ traceRow?.provider || '—' }}</el-tag></div>
                <div class="mu-meta-row"><span>分组</span><code>{{ traceRow?.group_name || '—' }}</code></div>
                <div class="mu-meta-row"><span>故障转移</span>
                  <el-tag size="small" :type="traceRow?.failover_used ? 'warning' : 'success'" effect="plain">{{ traceRow?.failover_used ? '是' : '否' }}</el-tag>
                </div>
                <div class="mu-meta-row"><span>Key</span><span>{{ keyLabel(traceRow?.key_id || '', traceRow?.key_name || '') }}</span></div>
              </div>
              <div class="mu-meta-section">
                <div class="mu-meta-heading">Token 用量</div>
                <div class="mu-meta-row"><span>输入</span><b>{{ fmtNum(traceRow?.prompt_tokens) }}</b></div>
                <div class="mu-meta-row"><span>缓存命中</span><b class="c-cyan">{{ fmtNum(traceRow?.cached_tokens) }}</b></div>
                <div class="mu-meta-row"><span>缓存写入</span><b class="c-amber">{{ fmtNum(traceRow?.cache_write_tokens) }}</b></div>
                <div class="mu-meta-row"><span>输出</span><b>{{ fmtNum(traceRow?.completion_tokens) }}</b></div>
                <div class="mu-meta-row" v-if="(traceRow?.reasoning_tokens || 0) > 0">
                  <span>推理</span><b class="c-purple">{{ fmtNum(traceRow?.reasoning_tokens) }}</b>
                </div>
                <div class="mu-meta-row mu-meta-sep"><span>合计</span><b class="c-blue">{{ fmtNum(traceRow?.total_tokens) }}</b></div>
              </div>
              <div class="mu-meta-section">
                <div class="mu-meta-heading">费用 (USD)</div>
                <div class="mu-meta-row" v-if="(traceRow?.input_price_snapshot || 0) > 0">
                  <span>输入单价/1M</span><b>${{ fmtPrice(traceRow?.input_price_snapshot) }}</b>
                </div>
                <div class="mu-meta-row" v-if="(traceRow?.output_price_snapshot || 0) > 0">
                  <span>输出单价/1M</span><b>${{ fmtPrice(traceRow?.output_price_snapshot) }}</b>
                </div>
                <div class="mu-meta-row"><span>输入费用</span><b>${{ fmtCost(traceRow?.input_cost_usd) }}</b></div>
                <div class="mu-meta-row"><span>输出费用</span><b>${{ fmtCost(traceRow?.output_cost_usd) }}</b></div>
                <div class="mu-meta-row" v-if="(traceRow?.cache_read_cost_usd || 0) > 0">
                  <span>缓存读取</span><b class="c-cyan">${{ fmtCost(traceRow?.cache_read_cost_usd) }}</b>
                </div>
                <div class="mu-meta-row mu-meta-sep"><span>合计</span><b class="c-green">${{ fmtCost(traceRow?.est_cost_usd) }}</b></div>
              </div>
              <div class="mu-meta-section">
                <div class="mu-meta-heading">来源</div>
                <div class="mu-meta-row"><span>IP</span><code>{{ traceRow?.ip_address || '—' }}</code></div>
                <div class="mu-meta-row mu-meta-row--wrap"><span>UA</span><code class="mu-meta-ua">{{ traceRow?.user_agent || '—' }}</code></div>
                <div class="mu-meta-row"><span>请求 ID</span><code class="mu-meta-ua">{{ traceRow?.request_id }}</code></div>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="提示词" name="prompt">
            <div v-if="traceMessages.length" class="mu-msgs">
              <div v-for="(msg, i) in traceMessages" :key="i" :class="['mu-msg', `mu-msg--${msg.role}`]">
                <span class="mu-msg-role">{{ msg.role }}</span>
                <pre class="mu-msg-content">{{ typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content, null, 2) }}</pre>
              </div>
            </div>
            <div v-else class="mu-trace-empty">无提示词数据</div>
          </el-tab-pane>

          <el-tab-pane name="response">
            <template #label>
              响应
              <el-tag v-if="traceIsStream" size="small" type="info" style="margin-left:4px;vertical-align:middle">Stream</el-tag>
            </template>
            <div v-if="traceResponseText" class="mu-msgs">
              <div class="mu-msg mu-msg--assistant">
                <span class="mu-msg-role">assistant</span>
                <pre class="mu-msg-content">{{ traceResponseText }}</pre>
              </div>
            </div>
            <div v-else-if="traceData?.response_body" class="mu-json-view">
              <pre>{{ JSON.stringify(traceData.response_body, null, 2) }}</pre>
            </div>
            <div v-else class="mu-trace-empty">无响应数据</div>
          </el-tab-pane>

          <el-tab-pane label="原始请求" name="raw_req">
            <div v-if="traceData?.request_body" class="mu-json-view">
              <pre>{{ JSON.stringify(traceData.request_body, null, 2) }}</pre>
            </div>
            <div v-else class="mu-trace-empty">无原始请求数据</div>
          </el-tab-pane>

          <el-tab-pane v-if="traceData?.error_message || (traceRow?.status || 0) >= 400" label="错误" name="error">
            <div class="mu-error-block">
              <div v-if="traceData?.error_code" class="mu-error-code">{{ traceData.error_code }}</div>
              <pre class="mu-error-msg">{{ traceData?.error_message || `HTTP ${traceRow?.status}` }}</pre>
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>
      <div v-else class="mu-trace-empty">暂无 trace 数据</div>
    </el-drawer>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import {
  Refresh, Download, Search, Histogram, Money, DataLine, Coin,
  MagicStick, Top, Bottom, Finished, List, InfoFilled,
  Loading, SuccessFilled, CircleCloseFilled,
  Close, Grid, Cpu, Key, WarningFilled
} from '@element-plus/icons-vue'
import {
  Chart, BarController, LineController, CategoryScale, LinearScale, BarElement, LineElement,
  PointElement, Tooltip, Legend, Filler
} from 'chart.js'
import http from '@/services/http'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

Chart.register(
  BarController, LineController,
  CategoryScale, LinearScale, BarElement, LineElement, PointElement,
  Tooltip, Legend, Filler,
)

const authStore = useAuthStore()

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

const CHART_C = {
  input:  '#2BA8A2',
  output: '#27AE60',
  cache:  '#5DADE2',
  cost:   '#E0A100',
  req:    '#E0A100',
}
const CHART_PALETTE = ['#2BA8A2', '#27AE60', '#E0A100', '#5DADE2', '#8B5CF6', '#EC4899', '#E67E22', '#14B8A6']

// ── Types ──
interface StatsRow {
  stat_date?: string; model?: string
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number
}
interface StatsTotal { req_count: number; success_count?: number; prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number }
interface LogRow {
  request_id: string; ts: string; key_id: string; key_name?: string
  model: string; routed_model: string; group_name: string; provider: string
  prompt_tokens: number; cached_tokens: number; cache_write_tokens: number
  completion_tokens: number; reasoning_tokens: number; total_tokens: number
  status: number; upstream_status: number; duration_ms: number; first_token_ms?: number | null
  stage_auth_ms?: number | null; stage_route_ms?: number | null; stage_upstream_ms?: number | null
  stream: boolean; failover_used: boolean; usage_missing: boolean
  reasoning_effort?: string; user_agent?: string; ip_address?: string
  input_cost_usd: number; output_cost_usd: number; cache_read_cost_usd: number; est_cost_usd: number
  input_price_snapshot?: number | null; output_price_snapshot?: number | null
}

// ── Date shortcuts ──
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }
const dateRange = ref<[string, string]>([fmt(daysAgo(6)), fmt(today)])
const dateShortcuts = [
  { text: '今天',       value: () => [fmt(today), fmt(today)] },
  { text: '昨天',       value: () => { const d = fmt(daysAgo(1)); return [d, d] } },
  { text: '最近 7 天',  value: () => [fmt(daysAgo(6)), fmt(today)] },
  { text: '最近 30 天', value: () => [fmt(daysAgo(29)), fmt(today)] },
  { text: '本月',       value: () => [fmt(new Date(today.getFullYear(), today.getMonth(), 1)), fmt(today)] },
]

// ── Date presets ──
const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近3天',  days: 2 },
  { label: '近7天',  days: 6 },
  { label: '近14天', days: 13 },
  { label: '近30天', days: 29 },
]

const activePreset = ref('近7天')

function applyPreset(p: { label: string; days: number }) {
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - p.days)
  dateRange.value = [fmt(start), fmt(end)]
  activePreset.value = p.label
  loadAll()
}

function onDateChange() {
  activePreset.value = '' // 手动改日期时取消高亮
  loadAll()
}

// ── Filters ──
const filterModel     = ref('') // 废弃，保留兼容
const filterKeyId     = ref('')
const filterKeyName   = ref('')
const filterModelName = ref('')
const filterGroupName = ref('')
const filterStatus    = ref('')
const groupBy = ref('day')

const activeFilterCount = computed(() =>
  [filterKeyId.value, filterModelName.value, filterGroupName.value, filterStatus.value].filter(Boolean).length
)

function clearFilters() {
  filterKeyId.value     = ''
  filterModelName.value = ''
  filterGroupName.value = ''
  filterStatus.value    = ''
  loadAll()
}

// ── My Keys ──
const myKeyOptions = ref<{ value: string; label: string }[]>([])

async function loadMyKeys() {
  try {
    const r = await http.get('/api/v1/my/keys')
    const keys: any[] = r.data.keys || []
    myKeyOptions.value = keys
      .filter((k: any) => k.enabled !== false)
      .map((k: any) => ({
        value: k.id,
        label: k.name || k.key_hint || maskKey(k.id),
      }))
  } catch (e) {
    console.warn('[my-keys] failed to load', e)
  }
}

// ── Stats state ──
const statsLoading = ref(false)
const statsRows = ref<StatsRow[]>([])
const statsTotal = ref<StatsTotal | null>(null)
const chartMetric = ref<'tokens' | 'reqs' | 'cost'>('tokens')
const distMetric = ref<'reqs' | 'tokens'>('reqs')

// ── Logs state ──
const logLoading = ref(false)
const logRows = ref<LogRow[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(50)

// ── Derived stats ──
const successRate = computed(() => {
  const t = statsTotal.value
  if (!t?.req_count) return 100
  return Math.round((t.success_count ?? t.req_count) / t.req_count * 100)
})
const successRateClass = computed(() => {
  const r = successRate.value
  return r >= 95 ? 'c-green' : r >= 80 ? 'c-orange' : 'c-red'
})
const cacheHitPct = computed(() => {
  const t = statsTotal.value
  if (!t?.prompt_tokens) return 0
  return Math.round(t.cached_tokens / t.prompt_tokens * 100)
})
const avgCostPerReq = computed(() => {
  const t = statsTotal.value
  if (!t?.req_count) return '0.000000'
  return (t.est_cost_usd / t.req_count).toFixed(6)
})

const maxPrompt     = computed(() => Math.max(...statsRows.value.map(r => r.prompt_tokens || 0), 1))
const maxCompletion = computed(() => Math.max(...statsRows.value.map(r => r.completion_tokens || 0), 1))
const maxCost       = computed(() => Math.max(...statsRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const pct = (v: number, max: number) => Math.min(Math.round(v / max * 100), 100)
const costColor = (v: number) => v > maxCost.value * 0.7 ? 'c-red fw6' : v > maxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'
/** 动态精度：保留足够有效数字，自动去掉尾零 */
const fmtCost = (v?: number | null): string => {
  const n = Number(v || 0)
  if (n === 0) return '0.00'
  let s: string
  if (n >= 1)        s = n.toFixed(2)
  else if (n >= 0.01)     s = n.toFixed(4)
  else if (n >= 0.0001)   s = n.toFixed(6)
  else                    s = n.toFixed(8)
  return s.replace(/(\.\d{2,}?)0+$/, '$1')
}
const fmtPrice = (n?: number | null) => {
  if (n == null) return '—'
  if (n === 0) return '0'
  return n >= 1 ? n.toFixed(2) : n.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
}
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const fmtDate = (s?: string) => s ? s.split('T')[0] : '—'
const fmtDateTime = (s?: string) => {
  if (!s) return '—'
  return s.replace('T', ' ').replace(/\.\d+Z$/, '').replace('Z', '').substring(0, 19)
}
const fmtDuration = (ms?: number) => {
  if (!ms && ms !== 0) return '—'
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(2)}s`
}
const maskKey = (k?: string) => !k ? '—' : k.length > 14 ? k.slice(0, 6) + '···' + k.slice(-4) : k

/** 优先显示 key_name，其次从已加载的 Key 列表查名字，最后回退脱敏 ID */
function keyLabel(keyId: string, keyName: string): string {
  if (keyName) return keyName
  const found = myKeyOptions.value.find(k => k.value === keyId)
  return found?.label || maskKey(keyId)
}

const logMaxCost = computed(() => Math.max(...logRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const logCostColor = (v: number) => v > logMaxCost.value * 0.7 ? 'c-red fw6' : v > logMaxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

const reasoningType = (effort?: string) => {
  if (!effort) return ''
  if (effort === 'high') return 'danger'
  if (effort === 'medium') return 'warning'
  return 'info'
}

// ── Load ──
async function loadStats() {
  if (!dateRange.value?.[0]) return
  statsLoading.value = true
  try {
    const params: Record<string, string> = {
      from: dateRange.value[0], to: dateRange.value[1],
      group_by: groupBy.value,
    }
    if (filterModelName.value) params.model_name = filterModelName.value
    if (filterGroupName.value) params.group_name = filterGroupName.value
    if (filterKeyId.value)     params.key_id     = filterKeyId.value
    if (filterStatus.value)    params.status     = filterStatus.value
    const r = await http.get('/api/v1/my/usage', { params })
    statsRows.value = r.data.rows || []
    statsTotal.value = r.data.total || null
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || e?.response?.data?.error || '用量查询失败')
  } finally {
    statsLoading.value = false
  }
  await nextTick()
  try {
    renderTrend()
    renderDist()
  } catch (e) {
    console.error('[my-usage] chart render failed', e)
  }
}

async function loadLogs() {
  if (!dateRange.value?.[0]) return
  logLoading.value = true
  try {
    const params: Record<string, string | number> = {
      from: dateRange.value[0], to: dateRange.value[1],
      page: logPage.value, page_size: logPageSize.value,
    }
    if (filterModelName.value) params.model_name = filterModelName.value
    if (filterGroupName.value) params.group_name = filterGroupName.value
    if (filterKeyId.value)     params.key_id     = filterKeyId.value
    if (filterStatus.value)    params.status     = filterStatus.value
    const r = await http.get('/api/v1/my/usage/logs', { params })
    logRows.value = r.data.rows || []
    logTotal.value = r.data.total || 0
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || '明细查询失败')
  } finally {
    logLoading.value = false
  }
}

function loadAll() {
  logPage.value = 1
  loadStats()
  loadLogs()
}

// ── Trace Drawer ──
interface TraceData {
  request_id: string; ts: string
  stage_auth_ms?: number; stage_route_ms?: number; stage_upstream_ms?: number
  stage_ttft_ms?: number; stage_total_ms?: number
  request_headers?: Record<string, string>
  request_body?: any; response_body?: any
  error_code?: string; error_message?: string
}

const traceVisible  = ref(false)
const traceLoading  = ref(false)
const traceRow      = ref<LogRow | null>(null)
const traceData     = ref<TraceData | null>(null)
const traceTab      = ref('meta')
const defaultStages = ['接收', '鉴权', '路由', '上游', '返回']

async function openTrace(row: LogRow) {
  traceRow.value  = row
  traceData.value = null
  traceTab.value  = 'prompt'
  traceVisible.value = true
  traceLoading.value = true
    try {
        const r = await http.get(`/api/v1/my/usage/logs/${row.request_id}/trace`, { _silent: true } as any)
        traceData.value = r.data
      } catch {
        traceData.value = null
        traceTab.value = 'meta'
      } finally {
    traceLoading.value = false
  }
}

function closeTrace() { traceVisible.value = false }

const traceMessages = computed(() => {
  const body = traceData.value?.request_body
  if (!body) return []
  return (body as any).messages || []
})

const traceResponseText = computed(() => {
  const body = traceData.value?.response_body
  if (!body) return ''
  const choices = (body as any).choices
  if (Array.isArray(choices) && choices.length > 0) {
    return choices[0]?.message?.content ?? choices[0]?.delta?.content ?? ''
  }
  return ''
})

const traceIsStream = computed(() => !!(traceData.value?.response_body as any)?.stream)

const timelineStages = computed(() => {
  const td = traceData.value
  const row = traceRow.value
  if (!row) return []

  const authMs  = td?.stage_auth_ms     ?? row.stage_auth_ms     ?? null
  const routeMs = td?.stage_route_ms    ?? row.stage_route_ms    ?? null
  const upMs    = td?.stage_upstream_ms ?? row.stage_upstream_ms ?? null
  const ttftMs  = td?.stage_ttft_ms     ?? row.first_token_ms    ?? null
  const total   = row.duration_ms ?? td?.stage_total_ms ?? null

  const isError = (row.status ?? 200) >= 400
  const errorInRoute = isError && upMs == null

  const authDur  = authMs != null ? Math.max(0, authMs) : null
  const routeDur = (routeMs != null && authMs != null) ? Math.max(0, routeMs - authMs) : null
  const upDur    = (upMs != null && total != null) ? Math.max(0, total - upMs) : null

  type Status = 'ok' | 'error' | 'skip'
  const stages: { key: string; label: string; ms: number | null; status: Status; note?: string }[] = [
    { key: 'auth', label: '鉴权', ms: authDur, status: authMs != null ? 'ok' : 'skip' },
    {
      key: 'route', label: '路由',
      ms: routeDur,
      status: errorInRoute ? 'error' : routeMs != null ? 'ok' : 'skip',
      note: (routeMs != null && authMs != null && routeMs < authMs) ? '计时异常' : undefined,
    },
    {
      key: 'upstream', label: '上游',
      ms: upDur,
      status: errorInRoute ? 'skip' : isError ? 'error' : upMs != null ? 'ok' : 'skip',
    },
    { key: 'ttft',  label: 'TTFT',  ms: ttftMs, status: ttftMs != null ? 'ok' : 'skip' },
    { key: 'done',  label: '总耗时', ms: total,  status: isError ? 'error' : 'ok' },
  ].filter(s => s.status !== 'skip' || s.key === 'done')
  return stages
})

// ── Charts ──
const trendCanvas = ref<HTMLCanvasElement | null>(null)
const distCanvas  = ref<HTMLCanvasElement | null>(null)
let trendChart: Chart | null = null
let distChart:  Chart | null = null

function renderTrend() {
  const canvas = trendCanvas.value
  if (!canvas || !statsRows.value.length) return
  trendChart?.destroy()

  const labels = statsRows.value.map(r =>
    groupBy.value === 'day' ? fmtDate(r.stat_date) : (r.model || '未知')
  )

  const isLine = groupBy.value === 'day' && statsRows.value.length > 1
  const type = isLine ? 'line' : 'bar'

  let datasets: any[]
  if (chartMetric.value === 'tokens') {
    datasets = [
      { label: '输入', data: statsRows.value.map(r => r.prompt_tokens), backgroundColor: CHART_C.input + '33', borderColor: CHART_C.input, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '输出', data: statsRows.value.map(r => r.completion_tokens), backgroundColor: CHART_C.output + '33', borderColor: CHART_C.output, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '缓存', data: statsRows.value.map(r => r.cached_tokens), backgroundColor: CHART_C.cache + '22', borderColor: CHART_C.cache, borderWidth: 1.5, borderDash: [4, 3], fill: false, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
    ]
  } else if (chartMetric.value === 'cost') {
    datasets = [{ label: '成本 (USD)', data: statsRows.value.map(r => r.est_cost_usd), backgroundColor: CHART_C.cost + '33', borderColor: CHART_C.cost, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 }]
  } else {
    datasets = [
      { label: '调用', data: statsRows.value.map(r => r.req_count), backgroundColor: CHART_C.req + '33', borderColor: CHART_C.req, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '成功', data: statsRows.value.map(r => r.success_count ?? r.req_count), backgroundColor: CHART_C.output + '22', borderColor: CHART_C.output, borderWidth: 2, fill: false, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
    ]
  }

  trendChart = new Chart(canvas, {
    type,
    data: { labels, datasets },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: { position: 'top', labels: { usePointStyle: true, pointStyle: 'line', padding: 12, color: '#64748B', font: { size: 11 } } },
        tooltip: {
          callbacks: {
            label: ctx => {
              const v = ctx.raw as number
              if (chartMetric.value === 'cost') return `${ctx.dataset.label}: $${v.toFixed(4)}`
              if (chartMetric.value === 'tokens') return `${ctx.dataset.label}: ${fmtTokens(v)}`
              return `${ctx.dataset.label}: ${fmtNum(v)}`
            }
          }
        }
      },
      scales: {
        x: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, maxRotation: 30 } },
        y: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, callback: (v: any) => chartMetric.value === 'tokens' ? fmtTokens(v) : chartMetric.value === 'cost' ? `$${Number(v).toFixed(2)}` : fmtNum(v) } }
      }
    }
  } as any)
}

function renderDist() {
  const canvas = distCanvas.value
  if (!canvas || !statsRows.value.length) return
  distChart?.destroy()

  const modelMap = new Map<string, { reqs: number; tokens: number }>()
  for (const r of statsRows.value) {
    const key = groupBy.value === 'model' ? (r.model || '未知') : fmtDate(r.stat_date)
    const cur = modelMap.get(key) || { reqs: 0, tokens: 0 }
    modelMap.set(key, { reqs: cur.reqs + r.req_count, tokens: cur.tokens + r.prompt_tokens + r.completion_tokens })
  }
  const sorted = [...modelMap.entries()]
    .sort((a, b) => b[1][distMetric.value === 'reqs' ? 'reqs' : 'tokens'] - a[1][distMetric.value === 'reqs' ? 'reqs' : 'tokens'])
    .slice(0, 8)
  const labels = sorted.map(([k]) => k)
  const data = sorted.map(([, v]) => distMetric.value === 'reqs' ? v.reqs : v.tokens)

  distChart = new Chart(canvas, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: distMetric.value === 'reqs' ? '调用次数' : 'Token 用量',
        data,
        backgroundColor: CHART_PALETTE.map(c => c + 'CC'),
        borderColor:     CHART_PALETTE,
        borderWidth: 0, borderRadius: 6, barThickness: 'flex', maxBarThickness: 26
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false, indexAxis: 'y',
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: ctx => distMetric.value === 'reqs' ? `${fmtNum(ctx.raw as number)} 次` : fmtTokens(ctx.raw as number) } }
      },
      scales: {
        x: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, callback: (v: any) => distMetric.value === 'reqs' ? fmtNum(v) : fmtTokens(v) } },
        y: { grid: { display: false }, border: { display: false }, ticks: { color: '#64748B', font: { size: 11 } } }
      }
    }
  })
}

// ── Export ──
function handleExport() {
  if (!logRows.value.length) return
  const keys = Object.keys(logRows.value[0] || {})
  const csv = [keys.join(','), ...logRows.value.map(r => keys.map(k => JSON.stringify((r as any)[k] ?? '')).join(','))].join('\n')
  const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
  Object.assign(document.createElement('a'), { href: URL.createObjectURL(blob), download: `my_logs_${dateRange.value[0]}_${dateRange.value[1]}.csv` }).click()
}

watch(distMetric, () => { if (statsRows.value.length) renderDist() })

// init
loadMyKeys()
loadAll()
</script>

<style scoped>
.mu {
  --mu-input:  #2BA8A2;
  --mu-output: #27AE60;
  --mu-cache:  #5DADE2;
  --mu-cost:   #C7900A;
  --mu-reason: #8B5CF6;
  --mu-danger: #E74C3C;
  --mu-warn:   #E0A100;

  --mu-surface: var(--el-fill-color-blank);
  --mu-border:  var(--el-border-color-light);
  --mu-muted:   var(--el-text-color-secondary);
  --mu-faint:   var(--el-text-color-placeholder);
  --mu-ink:     var(--el-text-color-regular);
  --mu-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 16px;
}

/* ── 页头 ── */
.mu-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.mu-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.mu-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.mu-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.mu-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1E8C86));
}
.mu-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.mu-filter, .mu-stat-card, .mu-chart-card, .mu-section {
  background: var(--mu-surface);
  border: 1px solid var(--mu-border);
  border-radius: 14px;
}

/* ── 筛选栏 ── */
.mu-filter {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 10px 16px;
  background: #fff;
  border-radius: 10px;
  margin-bottom: 16px;
  border: 1px solid #ebeef5;
  box-shadow: 0 1px 4px rgba(0,0,0,.04);
}
.mu-presets { display: flex; align-items: center; gap: 4px; }
.mu-preset-btn {
  padding: 3px 10px;
  border-radius: 20px;
  border: 1px solid #dcdfe6;
  background: #f5f7fa;
  color: #606266;
  font-size: 12px;
  cursor: pointer;
  transition: all .15s;
  white-space: nowrap;
  line-height: 1.6;
}
.mu-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.mu-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.mu-filter__date { width: 195px; flex-shrink: 0; }
.mu-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 4px; flex-shrink: 0; }
.mu-filter__chip { width: 130px; flex-shrink: 0; }
.mu-filter__chip--key { width: 140px; }
.mu-filter__status { width: 92px; flex-shrink: 0; }
.mu-filter__clear { flex-shrink: 0; }
.mu-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-right: 4px; vertical-align: middle; }
.mu-dot--ok  { background: #67c23a; }
.mu-dot--err { background: #f56c6c; }
:deep(.mu-filter__date .el-range-input) { font-size: 12px; }
:deep(.mu-filter__chip .el-input__inner),
:deep(.mu-filter__status .el-input__inner) { font-size: 12px; }

/* ── 统计卡片 ── */
.mu-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
@media (max-width: 1100px) { .mu-stats { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px)  { .mu-stats { grid-template-columns: 1fr 1fr; } }

.mu-stat-card {
  position: relative; overflow: hidden;
  padding: 18px; display: flex; align-items: flex-start; gap: 14px;
  min-width: 0; transition: box-shadow .25s ease, transform .25s ease, border-color .25s ease;
}
.mu-stat-card::after {
  content: ''; position: absolute; inset: 0 0 auto 0; height: 3px;
  background: var(--accent, var(--el-color-primary)); opacity: .9;
}
.mu-stat-card:hover {
  box-shadow: 0 8px 24px rgba(43, 168, 162, .14);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}
.mu-stat-card__icon {
  width: 46px; height: 46px; border-radius: 13px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.mu-stat-card__icon--blue   { background: color-mix(in srgb, var(--mu-input) 14%, transparent);  color: var(--mu-input); }
.mu-stat-card__icon--amber  { background: color-mix(in srgb, var(--mu-warn) 16%, transparent);   color: var(--mu-warn); }
.mu-stat-card__icon--cyan   { background: color-mix(in srgb, var(--mu-cache) 16%, transparent);  color: var(--mu-cache); }
.mu-stat-card__icon--green  { background: color-mix(in srgb, var(--mu-output) 14%, transparent); color: var(--mu-output); }
.mu-stat-card:nth-child(1) { --accent: var(--mu-input); }
.mu-stat-card:nth-child(2) { --accent: var(--mu-warn); }
.mu-stat-card:nth-child(3) { --accent: var(--mu-cache); }
.mu-stat-card:nth-child(4) { --accent: var(--mu-output); }
.mu-stat-card__body { min-width: 0; flex: 1; }
.mu-stat-card__label { font-size: 12px; color: var(--mu-muted); margin: 0 0 6px; white-space: nowrap; }
.mu-stat-card__value {
  font-size: 25px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 0 0 6px; line-height: 1.15; letter-spacing: -.3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.mu-stat-card__sub  { font-size: 12px; color: var(--mu-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; }
.mu-stat-card__rate { font-weight: 600; }

/* ── 图表区 ── */
.mu-charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 900px) { .mu-charts { grid-template-columns: 1fr; } }
.mu-chart-card { overflow: hidden; }
.mu-chart-card__header, .mu-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--mu-border); flex-wrap: wrap; gap: 8px;
}
.mu-chart-card__title, .mu-section__title {
  font-size: 14px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 8px;
}
.mu-chart-card__title::before, .mu-section__title::before {
  content: ''; width: 7px; height: 7px; border-radius: 999px;
  background: var(--el-color-primary);
}
.mu-chart-card__body { padding: 14px 16px; height: 228px; position: relative; }

/* ── 区块 ── */
.mu-section { overflow: hidden; }
.mu-section__count { font-size: 12px; color: var(--mu-muted); }
.mu-table-wrap { overflow-x: auto; }

/* ── 表格细节 ── */
.mu-table-wrap :deep(.el-table) {
  --el-table-border-color: var(--mu-border);
  --el-table-bg-color: var(--mu-surface);
  --el-table-tr-bg-color: var(--mu-surface);
  --el-table-row-hover-bg-color: var(--el-color-primary-light-9);
  background: var(--mu-surface);
}
.mu-table-wrap :deep(.el-table td.el-table__cell) {
  background: var(--mu-surface);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mu-table-wrap :deep(.el-table__row:hover > td.el-table__cell) { background: var(--el-color-primary-light-9) !important; }
.mu-table-wrap :deep(.el-table .el-table-fixed-column--left.is-last-column.el-table__cell),
.mu-table-wrap :deep(.el-table__fixed) { box-shadow: 6px 0 12px -6px rgba(0, 0, 0, .12); }

/* ── 空态 ── */
.mu-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.mu-empty p { font-size: 13px; color: var(--mu-faint); margin: 0; }

/* ── 分页 ── */
.mu-pagination { padding: 12px 16px; border-top: 1px solid var(--mu-border); display: flex; justify-content: flex-end; }

/* ── 等宽文本 ── */
.mu-mono { font-family: var(--mu-mono); font-size: 11px; color: var(--mu-muted); }
.mu-mono--sm { font-size: 10px; }

/* ── 模型名 ── */
.mu-model-name { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }
.mu-model-name--muted { color: var(--mu-muted); font-weight: 400; }
.mu-model-cell { display: flex; flex-direction: column; gap: 2px; }
.mu-model-routed { display: flex; align-items: center; gap: 3px; }
.mu-arrow { color: var(--mu-faint); font-size: 10px; }
.mu-tag { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
/* ── 模型组列换行 ── */
.mu-col--wrap { white-space: normal !important; word-break: break-all !important; overflow: visible !important; text-overflow: clip !important; }
.mu-tag--group { max-width: none; white-space: normal; word-break: break-all; }
.mu-group-name { font-size: 11px; color: var(--mu-muted); word-break: break-all; line-height: 1.5; }
.mu-dash { color: var(--mu-faint); }

.mu-token-row { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.mu-token-item {
  display: inline-flex; align-items: center; gap: 3px; font-size: 11px; font-weight: 600;
  padding: 1px 7px; border-radius: 999px;
}
.mu-token-item--in    { color: var(--mu-input);  background: color-mix(in srgb, var(--mu-input) 12%, transparent); }
.mu-token-item--out   { color: var(--mu-output); background: color-mix(in srgb, var(--mu-output) 12%, transparent); }
.mu-token-item--cache { color: var(--mu-cache);  background: color-mix(in srgb, var(--mu-cache) 14%, transparent); }
.mu-token-item--reason{ color: var(--mu-reason); background: color-mix(in srgb, var(--mu-reason) 12%, transparent); font-size: 11px; }
.mu-token-icon { font-size: 10px; }

.mu-time { font-size: 12px; color: var(--mu-muted); white-space: nowrap; font-family: var(--mu-mono); }
.mu-duration { font-size: 12px; color: var(--mu-ink); font-variant-numeric: tabular-nums; }
.mu-cost { font-family: var(--mu-mono); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.mu-flags { display: flex; flex-wrap: wrap; gap: 3px; justify-content: center; }
.mu-pct { font-size: 10px; color: var(--mu-cache); margin-left: 4px; }

/* ── 迷你进度条 ── */
.mu-bar-cell { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.mu-minibar { width: 100%; height: 4px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.mu-minibar__fill { height: 100%; border-radius: 999px; transition: width .4s ease; }
.mu-minibar__fill--blue  { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-input) 45%, transparent), var(--mu-input)); }
.mu-minibar__fill--green { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-output) 45%, transparent), var(--mu-output)); }
.mu-minibar__fill--red   { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-cost) 45%, transparent), var(--mu-cost)); }

/* ── 颜色工具 ── */
.c-blue   { color: var(--mu-input); }
.c-green  { color: var(--mu-output); }
.c-cyan   { color: var(--mu-cache); }
.c-orange { color: var(--mu-warn); }
.c-amber  { color: var(--mu-warn); }
.c-purple { color: var(--mu-reason); }
.c-red    { color: var(--mu-danger); }
.fw6      { font-weight: 600; }
.mu-tag--effort { font-size: 10px; }

/* ── 成本格 ── */
.mu-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; white-space: nowrap; }

/* ── 圆形信息触发按钮 ── */
.mu-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; border-radius: 999px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--mu-faint);
  cursor: help; transition: background .15s ease, color .15s ease, transform .15s ease;
}
.mu-info:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); transform: scale(1.08); }
.mu-info .el-icon { font-size: 11px; }

/* ── UA / IP 来源格 ── */
.mu-source { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.mu-ua { display: block; font-size: 11px; color: var(--mu-muted); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.mu-ip { font-family: var(--mu-mono); font-size: 10px; color: var(--mu-faint); }

/* ── 用户名 ── */
.mu-name { font-size: 12px; color: var(--el-text-color-regular); font-weight: 500; }

</style>

<!-- Tooltip 全局样式 -->
<style>
.mu-tt-popper.el-popper {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
  border-radius: 10px !important;
  box-shadow: 0 12px 32px rgba(0, 0, 0, .35) !important;
  padding: 10px 12px !important;
}
.mu-tt-popper.el-popper .el-popper__arrow::before {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
}
.mu-tt {
  min-width: 172px; font-size: 12px; line-height: 1.7;
  font-variant-numeric: tabular-nums;
}
.mu-tt__title {
  font-weight: 700; font-size: 12px; margin-bottom: 7px;
  color: #e5e7eb; border-bottom: 1px solid #374151; padding-bottom: 5px; letter-spacing: .2px;
}
.mu-tt__row { display: flex; justify-content: space-between; gap: 18px; align-items: center; color: #9ca3af; }
.mu-tt__sub { color: #6b7280; padding-left: 2px; }
.mu-tt__group { display: flex; align-items: center; gap: 4px; margin: 2px 0 1px; font-size: 11px; font-weight: 700; letter-spacing: .3px; color: #38bdf8; }
.mu-tt__group-ic { font-size: 12px; }
.mu-tt__row b { color: #f9fafb; font-weight: 600; }
.mu-tt__row b.c-cyan   { color: #38bdf8; }
.mu-tt__row b.c-amber  { color: #fbbf24; }
.mu-tt__row b.c-purple { color: #a78bfa; }
.mu-tt__row b.c-blue   { color: #5eead4; }
.mu-tt__row b.c-green  { color: #4ade80; }
.mu-tt__sep { border-top: 1px solid #374151; margin: 6px 0; }
.mu-ua-popper.el-popper { max-width: 440px; line-height: 1.5; word-break: break-all; }

/* ═══════════════════════════ Trace Drawer ═══════════════════════════ */
.mu-trace-header { display: flex; flex-direction: column; gap: 2px; }
.mu-trace-title  { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.mu-trace-id     { font-size: 11px; color: var(--el-text-color-secondary); font-family: 'SFMono-Regular', monospace; word-break: break-all; }
.mu-trace-loading { display: flex; justify-content: center; align-items: center; padding: 60px 0; color: var(--el-color-primary); }
.mu-trace-body    { display: flex; flex-direction: column; gap: 20px; }
.mu-trace-empty   { text-align: center; padding: 40px 0; color: var(--el-text-color-secondary); font-size: 13px; }
.mu-trace-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.mu-trace-time { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 4px; }
.mu-trace-dur  { font-size: 12px; color: var(--el-color-primary); font-weight: 600; }
.mu-timeline {
  display: flex; align-items: flex-start; gap: 0;
  background: var(--el-fill-color-light); border-radius: 10px; padding: 16px 20px; overflow-x: auto;
}
.mu-timeline--placeholder { opacity: .5; pointer-events: none; }
.mu-tl-node {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  position: relative; flex: 1; min-width: 72px;
}
.mu-tl-node + .mu-tl-node::before {
  content: ''; position: absolute; left: -50%; top: 13px;
  width: 100%; height: 2px; background: var(--el-border-color); z-index: 0;
}
.mu-tl-node.ok    + .mu-tl-node::before { background: var(--el-color-success-light-5); }
.mu-tl-node.error + .mu-tl-node::before { background: var(--el-color-danger-light-5); }
.mu-tl-dot {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--el-fill-color); border: 2px solid var(--el-border-color);
  font-size: 14px; position: relative; z-index: 1;
}
.mu-tl-node.ok    .mu-tl-dot { background: color-mix(in srgb, var(--el-color-success) 12%, #fff); border-color: var(--el-color-success); color: var(--el-color-success); }
.mu-tl-node.error .mu-tl-dot { background: color-mix(in srgb, var(--el-color-danger)  12%, #fff); border-color: var(--el-color-danger);  color: var(--el-color-danger); }
.mu-tl-label { font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600; }
.mu-tl-ms    { font-size: 11px; color: var(--el-text-color-regular); font-family: 'SFMono-Regular', monospace; }
.mu-trace-tabs { margin-top: -8px; }
.mu-msgs { display: flex; flex-direction: column; gap: 12px; }
.mu-msg  { border-radius: 8px; overflow: hidden; border: 1px solid var(--el-border-color-light); }
.mu-msg-role {
  display: block; padding: 5px 12px; font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .05em;
  background: var(--el-fill-color-light); color: var(--el-text-color-secondary);
}
.mu-msg--system    .mu-msg-role { background: #fef3c7; color: #92400e; }
.mu-msg--user      .mu-msg-role { background: #dbeafe; color: #1e40af; }
.mu-msg--assistant .mu-msg-role { background: #d1fae5; color: #065f46; }
.mu-msg-content {
  margin: 0; padding: 12px; font-size: 12.5px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-word;
  font-family: 'SFMono-Regular', monospace; background: var(--el-bg-color); max-height: 400px; overflow-y: auto;
}
.mu-json-view {
  background: var(--el-fill-color-light); border-radius: 8px; padding: 14px;
  max-height: 500px; overflow-y: auto;
}
.mu-json-view pre {
  margin: 0; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
  font-family: 'SFMono-Regular', monospace;
}
.mu-error-block { background: #fef2f2; border: 1px solid #fca5a5; border-radius: 8px; padding: 16px; }
.mu-error-code  { font-size: 11px; font-weight: 700; color: #dc2626; text-transform: uppercase; margin-bottom: 8px; }
.mu-error-msg   { margin: 0; font-size: 12.5px; color: #7f1d1d; white-space: pre-wrap; word-break: break-word; }
.mu-meta-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.mu-meta-section { background: var(--el-fill-color-light); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 6px; }
.mu-meta-heading { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--el-text-color-secondary); margin-bottom: 4px; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 6px; }
.mu-meta-row { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 12.5px; min-height: 22px; }
.mu-meta-row > span:first-child { color: var(--el-text-color-secondary); flex-shrink: 0; }
.mu-meta-row > span:last-child, .mu-meta-row > b, .mu-meta-row > code { text-align: right; word-break: break-all; }
.mu-meta-row--wrap { align-items: flex-start; }
.mu-meta-row--wrap > span:first-child { padding-top: 2px; }
.mu-meta-sep { border-top: 1px solid var(--el-border-color-lighter); padding-top: 6px; margin-top: 2px; }
.mu-meta-ua { font-family: 'SFMono-Regular', monospace; font-size: 11px; color: var(--el-text-color-regular); word-break: break-all; text-align: right; }

/* ═══════════════════════════ Responsive ═══════════════════════════ */
@media (max-width: 768px) {
  /* Header */
  .mu-header { flex-direction: column; align-items: flex-start; gap: 10px; }
  .mu-header__right { width: 100%; justify-content: flex-start; }

  /* Filter bar */
  .mu-filter { flex-wrap: wrap; gap: 8px; }
  .mu-presets { flex-wrap: wrap; gap: 4px; }
  .mu-filter__date { width: 100% !important; }
  .mu-filter__chip { width: 100% !important; }
  .mu-filter__chip--key { width: 100% !important; }
  .mu-filter__status { width: 100% !important; }
  .mu-filter__clear { width: 100%; }
  .mu-filter-divider { display: none; }

  /* Stats grid: 2-column */
  .mu-stats { grid-template-columns: repeat(2, 1fr); }

  /* Charts: single column */
  .mu-charts { grid-template-columns: 1fr; }

  /* Tables: overflow-x auto */
  .mu-table-wrap { overflow-x: auto; }
  .mu-table-wrap :deep(.el-table) { min-width: 700px; }

  /* Section header */
  .mu-section__header { flex-direction: column; align-items: flex-start; gap: 8px; }

  /* Pagination */
  .mu-pagination { flex-direction: column; align-items: center; gap: 8px; }

  /* Trace Drawer meta grid: single column */
  .mu-meta-grid { grid-template-columns: 1fr; }

  /* Trace Drawer 全宽 */
  .mu-trace-drawer.el-drawer { width: 100% !important; }

  /* Trace Drawer pre 溢出 */
  .mu-trace-drawer .mu-msg-content {
    font-size: 11px !important; padding: 8px !important;
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .mu-trace-drawer .mu-json-view pre {
    font-size: 11px !important; overflow-x: auto; -webkit-overflow-scrolling: touch;
    white-space: pre;
  }
  .mu-trace-drawer .mu-error-msg { font-size: 11px; }

  /* Trace Drawer tabs 横滑 */
  .mu-trace-drawer .el-tabs__nav-wrap {
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .mu-trace-drawer .el-tabs__nav {
    flex-wrap: nowrap; display: inline-flex;
  }

  /* Trace timeline: allow scroll */
  .mu-timeline { overflow-x: auto; white-space: nowrap; }
  .mu-tl-node { min-width: 60px; }

  /* Chart card header: stack */
  .mu-chart-card__header { flex-direction: column; align-items: flex-start; gap: 8px; }
}

@media (max-width: 480px) {
  .mu-stats { grid-template-columns: 1fr; }
  .mu-stat-card__value { font-size: 18px; }
  .mu-preset-btn { font-size: 12px; padding: 4px 10px; }
}
</style>
