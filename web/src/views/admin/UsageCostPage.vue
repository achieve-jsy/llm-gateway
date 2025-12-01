<template>
  <div class="up">

    <!-- ═══════════════════════════════════════════
         页头
    ═══════════════════════════════════════════ -->
    <div class="up-header">
      <div class="up-header__left">
        <h2 class="up-title">用量统计</h2>
        <span class="up-badge">Token · 成本（估算）</span>
      </div>
      <div class="up-header__right">
        <el-button :loading="statsLoading" :icon="Refresh" circle size="small" title="刷新" @click="loadAll" />
        <el-button :icon="Download" size="small" :disabled="!logRows.length" @click="handleExport">导出</el-button>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         筛选栏
    ═══════════════════════════════════════════ -->
    <div class="up-filter">
      <!-- 主筛选行：时间 -->
      <div class="up-filter-main">
        <!-- 快捷天数 -->
        <div class="up-presets">
          <button
            v-for="p in datePresets" :key="p.label"
            class="up-preset-btn"
            :class="{ 'up-preset-btn--active': activePreset === p.label }"
            @click="applyPreset(p)"
          >{{ p.label }}</button>
        </div>

        <div class="up-filter-divider" />

        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          format="MM/DD"
          :shortcuts="dateShortcuts"
          class="up-filter__date"
          size="default"
          @change="onDateChange"
        />
        <div class="up-filter-active" v-if="activeFilterCount > 0">
          <el-tag size="small" type="warning" effect="dark" round>
            {{ activeFilterCount }} 个过滤条件
          </el-tag>
          <el-button link size="small" class="up-filter-clear" @click="clearFilters">
            清空
          </el-button>
        </div>
      </div>

      <!-- 次筛选行：各维度 -->
      <div class="up-filter-dims">

        <!-- 模型组 -->
        <div class="up-filter-group">
          <span class="up-filter-label">模型组</span>
          <el-input v-model="filterGroupName" placeholder="搜索模型组" clearable size="small"
            class="up-filter__group" @change="loadAll" @clear="loadAll">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>

        <!-- 模型名 -->
        <div class="up-filter-group">
          <span class="up-filter-label">模型名</span>
          <el-input v-model="filterModelName" placeholder="搜索模型名" clearable size="small"
            class="up-filter__model" @change="loadAll" @clear="loadAll">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>

        <div class="up-filter-sep" />

        <!-- 渠道 -->
        <div class="up-filter-group">
          <span class="up-filter-label">渠道</span>
          <el-select v-model="filterProvider" placeholder="全部" clearable size="small"
            class="up-filter__provider" filterable @change="loadAll" @clear="loadAll">
            <el-option v-for="p in providerOptions" :key="p" :label="p" :value="p" />
          </el-select>
        </div>

        <!-- 状态 -->
        <div class="up-filter-group">
          <span class="up-filter-label">状态</span>
          <el-select v-model="filterStatus" placeholder="全部" clearable size="small"
            class="up-filter__status" @change="loadAll" @clear="loadAll">
            <el-option label="全部" value="" />
            <el-option value="success"><span class="up-status-opt up-status-opt--ok">成功</span></el-option>
            <el-option value="error"><span class="up-status-opt up-status-opt--err">失败</span></el-option>
          </el-select>
        </div>

        <div class="up-filter-sep" />

        <!-- Key 关键词 -->
        <div class="up-filter-group">
          <span class="up-filter-label">Key</span>
          <el-input v-model="filterKeyName" placeholder="搜索 Key 名称" clearable size="small"
            class="up-filter__key" @change="loadAll" @clear="loadAll">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>

        <!-- 用户关键词 -->
        <div class="up-filter-group">
          <span class="up-filter-label">用户</span>
          <el-input v-model="filterUserName" placeholder="搜索用户名" clearable size="small"
            class="up-filter__user" @change="loadAll" @clear="loadAll">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>

        <!-- 部门下拉 -->
        <div class="up-filter-group">
          <span class="up-filter-label">部门</span>
          <el-select v-model="filterDeptName" placeholder="全部" clearable size="small"
            class="up-filter__dept" filterable @change="loadAll" @clear="loadAll">
            <el-option v-for="d in deptOptions" :key="d.value" :label="d.label" :value="d.value" />
          </el-select>
        </div>

        <!-- 事业部（预留，暂无数据） -->
        <div class="up-filter-group">
          <span class="up-filter-label">事业部</span>
          <el-select v-model="filterStudioId" placeholder="全部" clearable size="small"
            class="up-filter__studio" filterable @change="loadAll" @clear="loadAll">
            <el-option v-for="s in studioOptions" :key="s.value" :label="s.label" :value="s.value" />
          </el-select>
        </div>

      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         统计卡片（sub2api 风格）
    ═══════════════════════════════════════════ -->
    <div class="up-stats" v-loading="statsLoading">
      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--blue">
          <el-icon size="22"><DataLine /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">总调用次数</p>
          <p class="up-stat-card__value">{{ fmtNum(statsTotal?.req_count) }}</p>
          <p class="up-stat-card__sub">
            成功
            <span :class="['up-stat-card__rate', successRateClass]">{{ successRate }}%</span>
          </p>
        </div>
      </div>

      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--amber">
          <el-icon size="22"><Coin /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">总 Token</p>
          <p class="up-stat-card__value">{{ fmtTokens((statsTotal?.prompt_tokens ?? 0) + (statsTotal?.completion_tokens ?? 0)) }}</p>
          <p class="up-stat-card__sub">
            输入 <span class="c-blue">{{ fmtTokens(statsTotal?.prompt_tokens) }}</span>
            · 输出 <span class="c-green">{{ fmtTokens(statsTotal?.completion_tokens) }}</span>
          </p>
        </div>
      </div>

      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--cyan">
          <el-icon size="22"><MagicStick /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">缓存命中</p>
          <p class="up-stat-card__value">{{ fmtTokens(statsTotal?.cached_tokens) }}</p>
          <p class="up-stat-card__sub">
            命中率 <span class="c-cyan">{{ cacheHitPct }}%</span>
          </p>
        </div>
      </div>

      <div class="up-stat-card up-stat-card--cost">
        <div class="up-stat-card__icon up-stat-card__icon--green">
          <el-icon size="22"><Money /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">估算成本 (USD)</p>
          <p class="up-stat-card__value c-green">${{ fmtCost(statsTotal?.est_cost_usd) }}</p>
          <p class="up-stat-card__sub">
            均次 <span class="c-orange">${{ avgCostPerReq }}</span>
            <el-tooltip v-if="statsTotal && statsTotal.req_count > 0 && !statsTotal.est_cost_usd" content="当前筛选条件下的模型未配置定价，估算成本为 $0" placement="top">
              <el-icon class="up-no-price-icon"><WarningFilled /></el-icon>
            </el-tooltip>
          </p>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         图表区
    ═══════════════════════════════════════════ -->
    <div class="up-charts" v-if="statsRows.length">
      <!-- 趋势 / Token 分布图 -->
      <div class="up-chart-card">
        <div class="up-chart-card__header">
          <span class="up-chart-card__title">Token 趋势</span>
          <el-radio-group v-model="chartMetric" size="small" @change="renderTrend">
            <el-radio-button value="tokens">Token</el-radio-button>
            <el-radio-button value="reqs">调用</el-radio-button>
            <el-radio-button value="cost">成本</el-radio-button>
          </el-radio-group>
        </div>
        <div class="up-chart-card__body">
          <canvas ref="trendCanvas" />
        </div>
      </div>

      <!-- 模型分布图 -->
      <div class="up-chart-card">
        <div class="up-chart-card__header">
          <span class="up-chart-card__title">Top 模型</span>
          <el-radio-group v-model="distMetric" size="small" @change="renderDist">
            <el-radio-button value="reqs">调用量</el-radio-button>
            <el-radio-button value="tokens">Token</el-radio-button>
          </el-radio-group>
        </div>
        <div class="up-chart-card__body">
          <canvas ref="distCanvas" />
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         汇总表（按天/模型/部门/Key）
    ═══════════════════════════════════════════ -->
    <div class="up-section">
      <div class="up-section__header">
        <span class="up-section__title">用量汇总</span>
        <el-radio-group v-model="groupBy" size="small" @change="loadStats">
          <el-radio-button value="hour">按小时</el-radio-button>
          <el-radio-button value="day">按天</el-radio-button>
          <el-radio-button value="week">按周</el-radio-button>
          <el-radio-button value="month">按月</el-radio-button>
          <el-radio-button value="model">按模型</el-radio-button>
          <el-radio-button value="dept">按部门</el-radio-button>
          <el-radio-button value="key">按 Key</el-radio-button>
        </el-radio-group>
      </div>
      <!-- 实体维度数量提示 -->
      <div v-if="isEntityGroupBy && statsTotalCount > 0" class="up-topn-hint">
        <el-icon><InfoFilled /></el-icon>
        按用量排序，共 {{ statsTotalCount }} 条
      </div>
      <div class="up-table-wrap" v-loading="statsLoading">
        <el-table :data="statsPageRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%"
          :default-sort="{ prop: 'est_cost_usd', order: 'descending' }">
          <el-table-column v-if="groupBy==='hour'" prop="stat_date" label="小时" min-width="140" sortable>
            <template #default="{ row }">{{ row.stat_date }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='day'" prop="stat_date" label="日期" min-width="100" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='week'" prop="stat_date" label="周起始" min-width="110" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='month'" prop="stat_date" label="月份" min-width="90" sortable>
            <template #default="{ row }">{{ row.stat_date }}</template>
          </el-table-column>
          <el-table-column v-if="groupBy==='model'" prop="model" label="模型" min-width="160" sortable>
            <template #default="{ row }"><span class="up-model-name">{{ row.model || '—' }}</span></template>
          </el-table-column>
          <el-table-column v-if="groupBy==='dept'" prop="dept_name" label="部门" min-width="130" sortable>
            <template #default="{ row }">
              <span v-if="row.dept_name" class="up-name">{{ row.dept_name }}</span>
              <span v-else-if="row.dept_id" class="up-mono up-mono--sm">{{ row.dept_id?.substring(0, 8) }}…</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <el-table-column v-if="groupBy==='key'" prop="key_name" label="Key" min-width="160" sortable>
            <template #default="{ row }">
              <el-tooltip v-if="row.key_name" :content="maskKey(row.key_id)" placement="top">
                <span class="up-name">{{ row.key_name }}</span>
              </el-tooltip>
              <span v-else class="up-mono up-mono--sm">{{ maskKey(row.key_id) }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="req_count" label="调用" min-width="80" align="right" sortable>
            <template #default="{ row }">{{ fmtNum(row.req_count) }}</template>
          </el-table-column>
          <el-table-column prop="prompt_tokens" label="输入 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span>{{ fmtTokens(row.prompt_tokens) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--blue" :style="{ width: pct(row.prompt_tokens, maxPrompt) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="cached_tokens" label="缓存命中" min-width="100" align="right" sortable>
            <template #default="{ row }">
              <span class="c-cyan">{{ fmtTokens(row.cached_tokens) }}</span>
              <span class="up-pct" v-if="row.prompt_tokens">{{ Math.round(row.cached_tokens / row.prompt_tokens * 100) }}%</span>
            </template>
          </el-table-column>
          <el-table-column prop="completion_tokens" label="输出 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span>{{ fmtTokens(row.completion_tokens) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--green" :style="{ width: pct(row.completion_tokens, maxCompletion) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="est_cost_usd" label="估算成本" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span :class="costColor(row.est_cost_usd)">${{ fmtCost(row.est_cost_usd) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--red" :style="{ width: pct(row.est_cost_usd, maxCost) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!statsLoading && !statsRows.length" class="up-empty">
          <el-icon size="48" color="#c0c4cc"><Histogram /></el-icon>
          <p>暂无汇总数据</p>
        </div>
      </div>
      <!-- 统计表格分页（仅实体维度且有多页时显示） -->
      <div v-if="isEntityGroupBy && statsTotalCount > statsPageSize" class="up-stats-pager">
        <el-pagination
          v-model:current-page="statsPage"
          :page-size="statsPageSize"
          :total="statsTotalCount"
          layout="prev, pager, next, total"
          small
          background
          @current-change="(p: number) => { statsPage = p; loadStats(false) }"
        />
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         调用明细表（参考 sub2api UsageTable）
    ═══════════════════════════════════════════ -->
    <div class="up-section">
      <div class="up-section__header">
        <span class="up-section__title">调用明细</span>
        <span class="up-section__count">共 {{ logTotal }} 条</span>
      </div>
      <div class="up-table-wrap" v-loading="logLoading">
        <el-table :data="logRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%">
<!-- 时间（移到末尾） -->
          <!-- Key 名称（优先显示名字，无名则脱敏 ID） -->
          <el-table-column prop="key_name" label="Key" min-width="110" fixed="left">
            <template #default="{ row }">
              <span class="up-name">{{ keyLabel(row.key_id, row.key_name) }}</span>
            </template>
          </el-table-column>
          <!-- 用户 -->
          <el-table-column prop="user_name" label="用户" min-width="90" fixed="left">
            <template #default="{ row }">
              <span v-if="row.user_name" class="up-name">{{ row.user_name }}</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 部门 -->
          <el-table-column prop="dept_name" label="部门" min-width="100" fixed="left">
            <template #default="{ row }">
              <span v-if="row.dept_name">{{ row.dept_name }}</span>
              <span v-else-if="row.dept_id" class="up-mono up-mono--sm">{{ row.dept_id?.substring(0, 8) }}…</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 事业部 -->
          <el-table-column prop="studio_name" label="事业部" min-width="90" fixed="left">
            <template #default="{ row }">
              <span v-if="row.studio_name">{{ row.studio_name }}</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 模型 / 路由模型（不用 code 标签） -->
          <el-table-column prop="model" label="模型 / 路由" min-width="160">
            <template #default="{ row }">
              <div class="up-model-cell">
                <span class="up-model-name">{{ row.model || '—' }}</span>
                <div v-if="row.routed_via_group && row.routed_model" class="up-model-routed">
                  <span class="up-arrow">↳</span>
                  <span class="up-model-name up-model-name--muted">{{ row.routed_model }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <!-- 组名 -->
          <el-table-column prop="group_name" label="模型组" min-width="180" class-name="up-col--wrap">
            <template #default="{ row }">
              <span v-if="row.group_name">{{ row.group_name }}</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 渠道 -->
          <el-table-column prop="provider" label="渠道" min-width="90">
            <template #default="{ row }">
              <span v-if="row.provider" class="up-provider">{{ row.provider }}</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 请求类型 -->
          <el-table-column label="类型" min-width="70" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.stream" type="primary" size="small" effect="plain">流式</el-tag>
              <el-tag v-else type="" size="small" effect="plain">同步</el-tag>
            </template>
          </el-table-column>
          <!-- Token 用量 -->
          <el-table-column label="Token 用量" min-width="210">
            <template #default="{ row }">
              <div class="up-token-row">
                <span class="up-token-item up-token-item--in" title="输入">
                  <el-icon class="up-token-icon"><Bottom /></el-icon>{{ fmtTokens(row.prompt_tokens) }}
                </span>
                <span class="up-token-item up-token-item--out" title="输出">
                  <el-icon class="up-token-icon"><Top /></el-icon>{{ fmtTokens(row.completion_tokens) }}
                </span>
                <span v-if="row.cached_tokens" class="up-token-item up-token-item--cache" title="缓存命中">
                  <el-icon class="up-token-icon"><Finished /></el-icon>{{ fmtTokens(row.cached_tokens) }}
                </span>
                <span v-if="row.reasoning_tokens" class="up-token-item up-token-item--reason" title="推理">
                  <el-icon class="up-token-icon"><MagicStick /></el-icon>{{ fmtTokens(row.reasoning_tokens) }}
                </span>
                <el-tooltip placement="top" :show-after="120" popper-class="up-tt-popper">
                  <template #content>
                    <div class="up-tt">
                      <div class="up-tt__title">Token 明细</div>
                      <div class="up-tt__row"><span>输入（含缓存）</span><b>{{ fmtNum(row.prompt_tokens) }}</b></div>
                      <div class="up-tt__row"><span>输出</span><b>{{ fmtNum(row.completion_tokens) }}</b></div>
                      <div class="up-tt__row" v-if="row.reasoning_tokens"><span class="up-tt__sub">└ 推理</span><b class="c-purple">{{ fmtNum(row.reasoning_tokens) }}</b></div>
                      <!-- 缓存专项（始终展示，便于核对缓存命中/写入）-->
                      <div class="up-tt__sep"></div>
                      <div class="up-tt__group"><el-icon class="up-tt__group-ic"><Finished /></el-icon>缓存</div>
                      <div class="up-tt__row"><span>缓存命中（读）</span><b :class="row.cached_tokens ? 'c-cyan' : ''">{{ fmtNum(row.cached_tokens) }}</b></div>
                      <div class="up-tt__row"><span>缓存写入</span><b :class="row.cache_write_tokens ? 'c-amber' : ''">{{ fmtNum(row.cache_write_tokens) }}</b></div>
                      <div class="up-tt__row"><span class="up-tt__sub">未缓存输入</span><b>{{ fmtNum(Math.max((row.prompt_tokens||0) - (row.cached_tokens||0) - (row.cache_write_tokens||0), 0)) }}</b></div>
                      <div class="up-tt__sep"></div>
                      <div class="up-tt__row"><span>总计</span><b class="c-blue">{{ fmtNum(row.total_tokens) }}</b></div>
                    </div>
                  </template>
                  <span class="up-info"><el-icon><InfoFilled /></el-icon></span>
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
          <!-- 首 Token（TTFT） -->
          <el-table-column prop="first_token_ms" label="TTFT" min-width="70" align="right">
            <template #default="{ row }">
              <span v-if="row.first_token_ms != null" class="up-duration">{{ fmtDuration(row.first_token_ms) }}</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <!-- 耗时 -->
          <el-table-column prop="duration_ms" label="耗时" min-width="70" align="right" sortable>
            <template #default="{ row }">
              <span class="up-duration">{{ fmtDuration(row.duration_ms) }}</span>
            </template>
          </el-table-column>
          <!-- 标记 -->
          <el-table-column label="标记" min-width="90" align="center">
            <template #default="{ row }">
              <div class="up-flags">
                <el-tag v-if="row.reasoning_effort" :type="reasoningType(row.reasoning_effort)" size="small" effect="plain" class="up-tag--effort">{{ row.reasoning_effort }}</el-tag>
                <el-tag v-if="row.failover_used" type="warning" size="small" effect="plain">切换</el-tag>
                <el-tag v-if="row.usage_missing" type="info" size="small" effect="plain">无Usage</el-tag>
              </div>
            </template>
          </el-table-column>
          <!-- 成本分析（sub2api 风格 tooltip） -->
          <el-table-column prop="est_cost_usd" label="费用" min-width="140" align="right" sortable>
            <template #default="{ row }">
              <div class="up-cost-cell">
                <span class="up-cost" :class="logCostColor(row.est_cost_usd)">${{ fmtCost(row.est_cost_usd) }}</span>
                <el-tooltip placement="left" :show-after="120" popper-class="up-tt-popper">
                  <template #content>
                    <div class="up-tt">
                      <div class="up-tt__title">成本明细 (USD)</div>
                      <!-- 单价（每 1M tokens）-->
                      <template v-if="(row.input_price_snapshot ?? 0) > 0 || (row.output_price_snapshot ?? 0) > 0">
                        <div class="up-tt__group"><el-icon class="up-tt__group-ic"><Money /></el-icon>单价 / 1M tokens</div>
                        <div class="up-tt__row"><span>输入单价</span><b>${{ fmtPrice(row.input_price_snapshot) }}</b></div>
                        <div class="up-tt__row"><span>输出单价</span><b>${{ fmtPrice(row.output_price_snapshot) }}</b></div>
                        <div class="up-tt__sep"></div>
                      </template>
                      <!-- 费用 -->
                      <div class="up-tt__group"><el-icon class="up-tt__group-ic"><Coin /></el-icon>费用</div>
                      <div class="up-tt__row">
                        <span>输入<span class="up-tt__sub" v-if="row.prompt_tokens"> ×{{ fmtNum(Math.max((row.prompt_tokens||0) - (row.cached_tokens||0) - (row.cache_write_tokens||0), 0)) }}</span></span><b>${{ (row.input_cost_usd || 0).toFixed(6) }}</b>
                      </div>
                      <div class="up-tt__row">
                        <span>输出<span class="up-tt__sub" v-if="row.completion_tokens"> ×{{ fmtNum(row.completion_tokens) }}</span></span><b>${{ (row.output_cost_usd || 0).toFixed(6) }}</b>
                      </div>
                      <!-- 缓存专项（始终展示，与 Token 明细保持一致） -->
                      <div class="up-tt__sep"></div>
                      <div class="up-tt__group"><el-icon class="up-tt__group-ic"><Finished /></el-icon>缓存</div>
                      <div class="up-tt__row">
                        <span>缓存读取<span class="up-tt__sub" v-if="row.cached_tokens"> ×{{ fmtNum(row.cached_tokens) }}</span></span>
                        <b :class="row.cache_read_cost_usd > 0 ? 'c-cyan' : ''">${{ (row.cache_read_cost_usd || 0).toFixed(6) }}</b>
                      </div>
                      <div class="up-tt__sep"></div>
                      <div class="up-tt__row"><span>合计</span><b class="c-green">${{ fmtCost(row.est_cost_usd) }}</b></div>
                    </div>
                  </template>
                  <span class="up-info"><el-icon><InfoFilled /></el-icon></span>
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <!-- User-Agent / IP -->
          <el-table-column label="来源" min-width="190">
            <template #default="{ row }">
              <div class="up-source">
                <el-tooltip v-if="row.user_agent" :content="row.user_agent" placement="top" :show-after="300" popper-class="up-ua-popper">
                  <span class="up-ua">{{ row.user_agent }}</span>
                </el-tooltip>
                <span v-else class="up-dash">—</span>
                <span v-if="row.ip_address" class="up-ip">{{ row.ip_address }}</span>
              </div>
            </template>
          </el-table-column>
          <!-- 时间（末尾） -->
          <el-table-column prop="ts" label="时间" min-width="155" sortable>
            <template #default="{ row }">
              <span class="up-time">{{ fmtDateTime(row.ts) }}</span>
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

        <div v-if="!logLoading && !logRows.length" class="up-empty">
          <el-icon size="48" color="#c0c4cc"><List /></el-icon>
          <p>暂无调用记录</p>
        </div>

        <!-- 分页 -->
        <div v-if="logTotal > 0" class="up-pagination">
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
  <el-drawer v-model="traceVisible" title="" size="780px" direction="rtl" :before-close="closeTrace">
    <template #header>
      <div class="up-trace-header">
        <span class="up-trace-title">请求详情</span>
        <span class="up-trace-id">{{ traceRow?.request_id }}</span>
      </div>
    </template>

    <div v-if="traceLoading" class="up-trace-loading"><el-icon class="is-loading" size="32"><Loading /></el-icon></div>
    <div v-else-if="traceData || traceRow" class="up-trace-body">

      <!-- ── 概要条 ── -->
      <div class="up-trace-meta">
        <el-tag type="info" size="small" effect="plain">{{ traceRow?.model }}</el-tag>
        <el-tag :type="(traceRow?.status || 0) === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ traceRow?.status }}</el-tag>
        <el-tag v-if="traceRow?.stream" type="primary" size="small" effect="plain">流式</el-tag>
        <el-tooltip v-if="traceRow?.failover_used" content="首选模型失败，已自动切换到备用模型" placement="top">
          <el-tag type="warning" size="small" effect="light" class="up-tag-failover">
            <el-icon style="vertical-align:-2px;margin-right:2px"><SwitchButton /></el-icon>故障转移
          </el-tag>
        </el-tooltip>
        <span class="up-trace-time">{{ fmtDateTime(traceRow?.ts) }}</span>
        <span class="up-trace-dur">{{ fmtDuration(traceRow?.duration_ms) }}</span>
      </div>

      <!-- ── Workflow 时间线（始终展示，优先 trace 数据，兜底 call_logs 阶段耗时） ── -->
      <div class="up-timeline">
        <div v-for="stage in timelineStages" :key="stage.key"
             :class="['up-tl-node', stage.status,
                      { 'up-tl-node--total': stage.key === 'done',
                        'up-tl-node--failover': stage.key === 'failover' }]">
          <div class="up-tl-dot">
            <el-icon v-if="stage.key === 'failover'"><SwitchButton /></el-icon>
            <el-icon v-else-if="stage.status === 'ok'"><SuccessFilled /></el-icon>
            <el-icon v-else-if="stage.status === 'error'"><CircleCloseFilled /></el-icon>
            <el-icon v-else><Loading /></el-icon>
          </div>
          <div class="up-tl-label">
            <el-tooltip v-if="stage.desc" :content="stage.desc" placement="top">
              <span class="up-tl-label-text">{{ stage.label }}</span>
            </el-tooltip>
            <span v-else class="up-tl-label-text">{{ stage.label }}</span>
            <span v-if="stage.key === 'done'" class="up-tl-measured">实测</span>
          </div>
          <div class="up-tl-ms" v-if="stage.ms != null">
            {{ stage.ms }}ms
            <el-tooltip v-if="stage.note" :content="stage.note" placement="top">
              <el-icon class="up-tl-warn"><WarningFilled /></el-icon>
            </el-tooltip>
          </div>
          <div class="up-tl-ms" v-else-if="stage.key === 'failover'">
            <span class="up-tl-failover-hint">已切换</span>
          </div>
        </div>
      </div>

      <!-- ── 内容区 Tabs ── -->
      <el-tabs v-model="traceTab" class="up-trace-tabs">
        <!-- 调用元数据 -->
        <el-tab-pane label="调用信息" name="meta">
          <div class="up-meta-grid">
            <!-- 调用者 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">调用者</div>
              <div class="up-meta-row"><span>Key</span><span>{{ keyLabel(traceRow?.key_id || '', traceRow?.key_name || '') }}</span></div>
              <div class="up-meta-row"><span>用户</span><span>{{ traceRow?.user_name || '—' }}</span></div>
              <div class="up-meta-row"><span>部门</span><span>{{ traceRow?.dept_name || traceRow?.dept_id || '—' }}</span></div>
              <div class="up-meta-row"><span>IP</span><code>{{ traceRow?.ip_address || '—' }}</code></div>
              <div class="up-meta-row up-meta-row--wrap"><span>UA</span><code class="up-meta-ua">{{ traceRow?.user_agent || '—' }}</code></div>
            </div>
            <!-- 路由 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">路由</div>
              <div class="up-meta-row"><span>请求模型</span><code>{{ traceRow?.model }}</code></div>
              <div class="up-meta-row"><span>实际模型</span><code>{{ traceRow?.routed_model || '—' }}</code></div>
              <div class="up-meta-row"><span>渠道</span><el-tag size="small" type="info" effect="plain">{{ traceRow?.provider || '—' }}</el-tag></div>
              <div class="up-meta-row"><span>分组</span><code>{{ traceRow?.group_name || '—' }}</code></div>
              <div class="up-meta-row"><span>故障转移</span>
                <el-tag size="small" :type="traceRow?.failover_used ? 'warning' : 'success'" effect="plain">
                  {{ traceRow?.failover_used ? '是' : '否' }}
                </el-tag>
              </div>
              <div class="up-meta-row"><span>流式</span>
                <el-tag size="small" :type="traceRow?.stream ? 'primary' : ''" effect="plain">{{ traceRow?.stream ? '是' : '否' }}</el-tag>
              </div>
            </div>
            <!-- Token 用量 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">Token 用量</div>
              <div class="up-meta-row"><span>输入</span><b>{{ fmtNum(traceRow?.prompt_tokens) }}</b></div>
              <div class="up-meta-row"><span>缓存命中</span><b class="c-cyan">{{ fmtNum(traceRow?.cached_tokens) }}</b></div>
              <div class="up-meta-row"><span>缓存写入</span><b class="c-amber">{{ fmtNum(traceRow?.cache_write_tokens) }}</b></div>
              <div class="up-meta-row"><span>输出</span><b>{{ fmtNum(traceRow?.completion_tokens) }}</b></div>
              <div class="up-meta-row" v-if="(traceRow?.reasoning_tokens || 0) > 0">
                <span>推理</span><b class="c-purple">{{ fmtNum(traceRow?.reasoning_tokens) }}</b>
              </div>
              <div class="up-meta-row up-meta-sep"><span>合计</span><b class="c-blue">{{ fmtNum(traceRow?.total_tokens) }}</b></div>
              <div class="up-meta-row" v-if="traceRow?.usage_missing">
                <span></span><el-tag size="small" type="warning">用量缺失</el-tag>
              </div>
            </div>
            <!-- 费用 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">费用 (USD)</div>
              <div class="up-meta-row" v-if="(traceRow?.input_price_snapshot || 0) > 0 || (traceRow?.output_price_snapshot || 0) > 0">
                <span>输入单价/1M</span><b>${{ fmtPrice(traceRow?.input_price_snapshot) }}</b>
              </div>
              <div class="up-meta-row" v-if="(traceRow?.input_price_snapshot || 0) > 0 || (traceRow?.output_price_snapshot || 0) > 0">
                <span>输出单价/1M</span><b>${{ fmtPrice(traceRow?.output_price_snapshot) }}</b>
              </div>
              <div class="up-meta-row"><span>输入费用</span><b>${{ fmtCost(traceRow?.input_cost_usd) }}</b></div>
              <div class="up-meta-row"><span>输出费用</span><b>${{ fmtCost(traceRow?.output_cost_usd) }}</b></div>
              <div class="up-meta-row" v-if="(traceRow?.cache_read_cost_usd || 0) > 0">
                <span>缓存读取</span><b class="c-cyan">${{ fmtCost(traceRow?.cache_read_cost_usd) }}</b>
              </div>
              <div class="up-meta-row up-meta-sep"><span>合计</span><b class="c-green">${{ fmtCost(traceRow?.est_cost_usd) }}</b></div>
            </div>
          </div>
        </el-tab-pane>

        <!-- 提示词 -->
        <el-tab-pane label="提示词" name="prompt">
          <div v-if="traceMessages.length" class="up-msgs">
            <div v-for="(msg, i) in traceMessages" :key="i"
                 :class="['up-msg', `up-msg--${msg.role}`]">
              <span class="up-msg-role">{{ msg.role }}</span>
              <pre class="up-msg-content">{{ typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content, null, 2) }}</pre>
            </div>
          </div>
          <div v-else class="up-trace-empty">无提示词数据</div>
        </el-tab-pane>

        <!-- 响应 -->
        <el-tab-pane name="response">
          <template #label>
            响应
            <el-tag v-if="traceIsStream" size="small" type="info" style="margin-left:4px;vertical-align:middle">Stream</el-tag>
          </template>
          <div v-if="traceResponseText" class="up-msgs">
            <div class="up-msg up-msg--assistant">
              <span class="up-msg-role">assistant</span>
              <pre class="up-msg-content">{{ traceResponseText }}</pre>
            </div>
          </div>
          <div v-else-if="traceData?.response_body" class="up-json-view">
            <pre>{{ JSON.stringify(traceData.response_body, null, 2) }}</pre>
          </div>
          <div v-else class="up-trace-empty">无响应数据</div>
        </el-tab-pane>

        <!-- 原始 JSON -->
        <el-tab-pane label="原始请求" name="raw_req">
          <div v-if="traceData?.request_body" class="up-json-view">
            <pre>{{ JSON.stringify(traceData.request_body, null, 2) }}</pre>
          </div>
          <div v-else class="up-trace-empty">无原始请求数据</div>
        </el-tab-pane>

        <!-- 请求头 -->
        <el-tab-pane label="请求头" name="headers">
          <div v-if="traceData?.request_headers && Object.keys(traceData.request_headers).length > 0" class="up-headers-table">
            <div v-for="(val, key) in traceData.request_headers" :key="key" class="up-header-row">
              <span class="up-header-key">{{ key }}</span>
              <span class="up-header-val">{{ val }}</span>
            </div>
          </div>
          <div v-else class="up-trace-empty">无请求头数据</div>
        </el-tab-pane>

        <!-- 错误 -->
        <el-tab-pane v-if="traceData?.error_message || (traceRow?.status || 0) >= 400" label="错误" name="error">
          <div class="up-error-block">
            <div v-if="traceData?.error_code" class="up-error-code">{{ traceData.error_code }}</div>
            <pre class="up-error-msg">{{ traceData?.error_message || `HTTP ${traceRow?.status}` }}</pre>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
    <div v-else class="up-trace-empty">暂无 trace 数据</div>
  </el-drawer>

  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick } from 'vue'
import {
  Refresh, Download, Search, Histogram, Money, DataLine, Coin,
  MagicStick, Top, Bottom, Finished, List, InfoFilled,
  View, Loading, SuccessFilled, CircleCloseFilled, WarningFilled, SwitchButton
} from '@element-plus/icons-vue'
import {
  Chart, BarController, LineController, CategoryScale, LinearScale, BarElement, LineElement,
  PointElement, Tooltip, Legend, Filler
} from 'chart.js'
import http from '@/services/http'
import { ElMessage } from 'element-plus'

Chart.register(
  BarController, LineController,
  CategoryScale, LinearScale, BarElement, LineElement, PointElement,
  Tooltip, Legend, Filler,
)

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

// 与主题（Flip7 Teal）一致的图表配色
const CHART_C = {
  input:  '#2BA8A2', // 输入 = 主题青
  output: '#27AE60', // 输出 = 成功绿
  cache:  '#5DADE2', // 缓存 = 信息蓝
  cost:   '#E0A100', // 成本 = 金
  req:    '#E0A100', // 调用 = 金
}
const CHART_PALETTE = ['#2BA8A2', '#27AE60', '#E0A100', '#5DADE2', '#8B5CF6', '#EC4899', '#E67E22', '#14B8A6']

// ── Types ──
interface StatsRow {
  stat_date?: string; model?: string
  dept_id?: string; dept_name?: string
  key_id?: string;   key_name?: string
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number
}
interface StatsTotal { req_count: number; success_count?: number; prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number }
interface LogRow {
  request_id: string; ts: string; key_id: string; key_name?: string
  user_id: string; user_name?: string; dept_id: string; dept_name?: string; studio_name?: string
  model: string; routed_model: string; routed_via_group: boolean; group_name: string; provider: string
  prompt_tokens: number; cached_tokens: number; cache_write_tokens: number
  completion_tokens: number; reasoning_tokens: number; total_tokens: number
  status: number; upstream_status: number; duration_ms: number; first_token_ms?: number | null
  stage_ttfb_ms?: number | null; stage_auth_ms?: number | null; stage_route_ms?: number | null; stage_upstream_ms?: number | null
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
  activePreset.value = ''
  loadAll()
}

// ── Filters ──
const filterModel  = ref('') // 已废弃，保留避免其他引用报错
const filterKeyId    = ref('')
const filterKeyName  = ref('')
const filterUserName = ref('')
const filterDeptName = ref('')
const filterStudioId = ref('')
const filterProvider = ref('')
const filterStatus   = ref('')
const filterModelName = ref('')
const filterGroupName = ref('')

const activeFilterCount = computed(() =>
  [filterModelName.value, filterGroupName.value,
   filterKeyName.value, filterUserName.value, filterDeptName.value,
   filterStudioId.value, filterProvider.value, filterStatus.value]
    .filter(Boolean).length
)

function clearFilters() {
  filterModelName.value = ''
  filterGroupName.value = ''
  filterKeyName.value   = ''
  filterUserName.value  = ''
  filterKeyId.value     = ''
  filterDeptName.value    = ''
  filterStudioId.value  = ''
  filterProvider.value  = ''
  filterStatus.value    = ''
  loadAll()
}
const groupBy = ref('day')

// ── 统计表格分页（后端真实分页）──
const statsPage      = ref(1)
const statsPageSize  = 10
const statsTotalCount = ref(0)        // 后端返回的实体总数
const isEntityGroupBy = computed(() => true) // 所有维度均支持后端分页
const statsPageRows   = computed(() => {
  // 时间维度不分页，直接返回全量
  if (!isEntityGroupBy.value) return statsRows.value
  // 实体维度已由后端分页，返回的就是当页数据
  return statsRows.value
})

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
  return fmtCost(t.est_cost_usd / t.req_count)
})

// ── Table bar helpers ──
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
  // 去掉小数点后的尾零，但至少保留 2 位小数
  return s.replace(/(\.\d{2,}?)0+$/, '$1')
}
const fmtPrice = (n?: number | null) => {
  if (n == null) return '—'
  if (n === 0) return '0'
  // 单价单位：USD / 1M tokens，保留有意义的小数位
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

/** 优先显示 key_name，其次从 keyOptions 查名字，最后回退脱敏 ID */
function keyLabel(keyId: string, keyName: string): string {
  if (keyName) return keyName
  const found = keyOptions.value.find(k => k.value === keyId)
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

// ── Filter options ──
const keyOptions     = ref<{ value: string; label: string }[]>([])
const deptOptions    = ref<{ value: string; label: string }[]>([])
const studioOptions  = ref<{ value: string; label: string }[]>([])
const providerOptions = ref<string[]>([])

async function loadOptions() {
  try {
    const r = await http.get('/api/v1/usage/options')
    providerOptions.value = r.data.providers || []
    if (r.data.depts?.length) {
      deptOptions.value = r.data.depts.map((d: any) => ({
        value: d.id, label: d.name || d.id
      }))
    }
    if (r.data.studios?.length) {
      studioOptions.value = r.data.studios.map((s: any) => ({
        value: s.id, label: s.name || s.id
      }))
    }
  } catch { /* ignore */ }
}

function refreshFilterOptions(rows: StatsRow[], gb: string) {
  if (gb === 'key') {
    const seen = new Set<string>()
    keyOptions.value = rows
      .filter(r => r.key_id && !seen.has(r.key_id) && seen.add(r.key_id))
      .map(r => ({ value: r.key_id!, label: r.key_name || maskKey(r.key_id) }))
  }
}

// ── Load ──
async function loadStats(resetPage = true) {
  if (!dateRange.value?.[0]) return
  statsLoading.value = true
  if (resetPage) statsPage.value = 1  // 切换维度或刷新时重置到第一页
  try {
    const params: Record<string, string> = {
      from: dateRange.value[0], to: dateRange.value[1],
      group_by: groupBy.value,
    }
    // 实体维度传分页参数
    if (isEntityGroupBy.value) {
      params.page      = String(statsPage.value)
      params.page_size = String(statsPageSize)
    }
    if (filterModelName.value) params.model_name = filterModelName.value
    if (filterGroupName.value) params.group_name = filterGroupName.value
    if (filterKeyId.value)     params.key_id     = filterKeyId.value
    if (filterKeyName.value)   params.key_name   = filterKeyName.value
    if (filterUserName.value)  params.user_name  = filterUserName.value
    if (filterDeptName.value)  params.dept_name  = filterDeptName.value
    if (filterStudioId.value)  params.studio_id  = filterStudioId.value
    if (filterProvider.value)  params.provider   = filterProvider.value
    if (filterStatus.value)    params.status     = filterStatus.value
    const r = await http.get('/api/v1/usage', { params })
    statsRows.value       = r.data.rows || []
    statsTotal.value      = r.data.total || null
    statsTotalCount.value = r.data.total_count ?? statsRows.value.length
    refreshFilterOptions(statsRows.value, groupBy.value)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || e?.response?.data?.error || '用量查询失败')
  } finally {
    statsLoading.value = false
  }
  // 图表渲染与数据查询解耦：渲染异常不应误报“查询失败”
  await nextTick()
  try {
    renderTrend()
    renderDist()
  } catch (e) {
    console.error('[usage] chart render failed', e)
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
    if (filterKeyName.value)   params.key_name   = filterKeyName.value
    if (filterUserName.value)  params.user_name  = filterUserName.value
    if (filterDeptName.value)    params.dept_name  = filterDeptName.value
    if (filterStudioId.value)  params.studio_id  = filterStudioId.value
    if (filterProvider.value)  params.provider   = filterProvider.value
    if (filterStatus.value)    params.status     = filterStatus.value
    const r = await http.get('/api/v1/usage-logs', { params })
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
  stage_ttfb_ms?: number; stage_ttft_ms?: number; stage_total_ms?: number
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
    const r = await http.get(`/api/v1/usage/logs/${row.request_id}/trace`, { _silent: true } as any)
    traceData.value = r.data
  } catch {
    traceData.value = null
    traceTab.value = 'meta'
  } finally {
    traceLoading.value = false
  }
}

function closeTrace() { traceVisible.value = false }

// 从 trace_data 中提取 messages
const traceMessages = computed(() => {
  const body = traceData.value?.request_body
  if (!body) return []
  return (body as any).messages || []
})

// 从 trace_data 中提取助手回复文本（支持流式重组响应和普通响应）
const traceResponseText = computed(() => {
  const body = traceData.value?.response_body
  if (!body) return ''
  const choices = (body as any).choices
  if (Array.isArray(choices) && choices.length > 0) {
    // 优先取 message.content（非流式 / 流式重组后），其次 delta.content
    return choices[0]?.message?.content ?? choices[0]?.delta?.content ?? ''
  }
  return ''
})

// 是否流式（重组）响应
const traceIsStream = computed(() => !!(traceData.value?.response_body as any)?.stream)

// 构建时间线 stages
// 优先使用 call_traces（有完整数据时），始终使用 call_logs 中的 stage_*_ms 字段作为兜底
// call_logs 中 duration_ms 是总耗时的权威数据源（由 UsageCollector 精确计算）
//
// 时间点语义（均相对于请求抵达时刻，单位 ms）：
//   t_auth_ms     = 请求到达 → auth 完成（累计时间点）
//   t_route_ms    = 请求到达 → 路由选好 + 上游请求发出（累计时间点）
//   stage_ttfb_ms = 从上游请求发出 → 收到 HTTP 响应头（上游 TTFB，相对上游，仅流式）
//   first_token_ms= 从上游请求发出 → 收到首个 content token（TTFT，相对上游，仅流式）
//   duration_ms   = 请求到达 → 响应完全结束（端到端总耗时）
//
// 可派生的各阶段独立耗时：
//   鉴权        = t_auth_ms
//   路由        = t_route_ms - t_auth_ms
//   上游 TTFB   = stage_ttfb_ms（流式）
//   上游 TTFT   = first_token_ms - stage_ttfb_ms（流式，首字节→首 content token）
//   流式返回    = duration_ms - t_route_ms - first_token_ms（流式，首 token 到结束）
//   非流式上游  = duration_ms - t_route_ms（非流式整体上游耗时）
//   总耗时      = duration_ms
const timelineStages = computed(() => {
  const td = traceData.value
  const row = traceRow.value
  if (!row) return []

  // 各阶段累计时间点（均相对于请求抵达时刻）
  const authMs  = td?.stage_auth_ms     ?? row.stage_auth_ms     ?? null
  const routeMs = td?.stage_route_ms    ?? row.stage_route_ms    ?? null
  // TTFB / TTFT：相对上游发出请求的延迟
  const ttfbMs  = td?.stage_ttfb_ms     ?? row.stage_ttfb_ms     ?? null
  const ttftMs  = td?.stage_ttft_ms     ?? row.first_token_ms    ?? null
  const total   = row.duration_ms       ?? td?.stage_total_ms    ?? null

  const isStream    = !!row.stream
  const isFailover  = !!row.failover_used
  const isError     = (row.status ?? 200) >= 400
  const errorInAuth  = isError && authMs  == null && routeMs == null
  const errorInRoute = isError && routeMs == null && authMs  != null

  // 各阶段独立耗时（时间点差值，保证非负）
  const authDur  = authMs  != null ? Math.max(0, authMs) : null
  const routeDur = (routeMs != null && authMs != null)
    ? Math.max(0, routeMs - authMs)
    : (routeMs != null ? Math.max(0, routeMs) : null)

  // 流式：TTFB = 上游请求发出→响应头，TTFT_delta = 响应头→首个 content token
  const ttfbDur       = ttfbMs != null ? Math.max(0, ttfbMs) : null
  const ttftDeltaDur  = (ttftMs != null && ttfbMs != null)
    ? Math.max(0, ttftMs - ttfbMs)   // 首字节到首 content token
    : (ttftMs != null ? Math.max(0, ttftMs) : null)  // 没有 TTFB 时退回显示总 TTFT

  // 流式返回耗时：从首 token 到结束
  const streamReturnDur = (isStream && ttftMs != null && routeMs != null && total != null)
    ? Math.max(0, total - routeMs - ttftMs)
    : null

  // 非流式：整个上游往返耗时
  const upstreamDur = (!isStream && routeMs != null && total != null)
    ? Math.max(0, total - routeMs)
    : null

  type Status = 'ok' | 'error' | 'skip'
  const stages: { key: string; label: string; ms: number | null; status: Status; note?: string; desc?: string }[] = []

  // ── 1. 鉴权 ──────────────────────────────────────────────────────────
  stages.push({
    key: 'auth', label: '鉴权',
    desc: '请求到达 → API Key 验证完成',
    ms: authDur,
    status: authMs != null ? 'ok' : (errorInAuth ? 'error' : 'skip'),
  })

  // ── 2. 路由 ──────────────────────────────────────────────────────────
  stages.push({
    key: 'route', label: '路由',
    desc: '鉴权完成 → 模型组调度 + 选定上游渠道',
    ms: routeDur,
    status: errorInRoute ? 'error' : routeMs != null ? 'ok' : 'skip',
    note: (routeMs != null && authMs != null && routeMs < authMs) ? '计时异常' : undefined,
  })

  // ── 2.5 故障转移（仅 failover_used=true 时显示）──────────────────────
  if (isFailover) {
    stages.push({
      key: 'failover', label: '故障转移',
      desc: `首选模型调用失败，自动切换到备用模型重试（实际路由至 ${row.routed_model || '备用模型'}）`,
      ms: null,   // 无独立计时，耗时已包含在路由和上游阶段
      status: 'ok',
    })
  }

  if (isStream) {
    // ── 3a. 上游 TTFB（流式）────────────────────────────────────────────
    stages.push({
      key: 'ttfb', label: 'TTFB',
      desc: '上游请求发出 → 收到 HTTP 响应头（网络 + 排队耗时）',
      ms: ttfbDur,
      status: isError ? 'error' : (ttfbDur != null ? 'ok' : 'skip'),
    })

    // ── 3b. TTFT（流式）：响应头→首个 content token ─────────────────────
    stages.push({
      key: 'ttft', label: 'TTFT',
      desc: ttfbMs != null
        ? '收到响应头 → 首个 content token（模型推理生成首字）'
        : '上游请求发出 → 首个 content token',
      ms: ttftDeltaDur,
      status: isError ? 'error' : (ttftDeltaDur != null ? 'ok' : 'skip'),
    })

    // ── 4. 流式返回（首 token → 流式输出结束）────────────────────────────
    stages.push({
      key: 'stream', label: '流式返回',
      desc: '首个 token 返回 → 流式输出全部完成',
      ms: streamReturnDur,
      status: isError ? 'error' : (streamReturnDur != null ? 'ok' : 'skip'),
    })
  } else {
    // ── 3. 上游响应（非流式：整体往返）──────────────────────────────────
    stages.push({
      key: 'upstream', label: '上游响应',
      desc: '上游请求发出 → 收到完整响应',
      ms: upstreamDur,
      status: isError ? 'error' : (upstreamDur != null ? 'ok' : 'skip'),
    })
  }

  // ── 5. 总耗时 ────────────────────────────────────────────────────────
  stages.push({
    key: 'done', label: '总耗时',
    ms: total,
    status: isError ? 'error' : 'ok',
  })

  return stages.filter(s => s.status !== 'skip' || s.key === 'done')
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

  const labels = statsRows.value.map(r => {
    if (groupBy.value === 'hour')  return r.stat_date ?? '—'
    if (groupBy.value === 'day')   return fmtDate(r.stat_date)
    if (groupBy.value === 'week')  return fmtDate(r.stat_date)
    if (groupBy.value === 'month') return r.stat_date?.slice(0, 7) ?? '—'  // "2026-06"
    if (groupBy.value === 'model') return r.model || '未知'
    if (groupBy.value === 'dept')  return r.dept_name || r.dept_id || '未知'
    return r.key_name || maskKey(r.key_id)
  })

  const isLine = ['hour', 'day', 'week', 'month'].includes(groupBy.value) && statsRows.value.length > 1
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

  // 按模型维度聚合
  const modelMap = new Map<string, { reqs: number; tokens: number }>()
  for (const r of statsRows.value) {
    const key = groupBy.value === 'model' ? (r.model || '未知') : (groupBy.value === 'dept' ? (r.dept_name || r.dept_id || '未知') : (groupBy.value === 'key' ? (r.key_name || maskKey(r.key_id)) : fmtDate(r.stat_date)))
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
  Object.assign(document.createElement('a'), { href: URL.createObjectURL(blob), download: `logs_${dateRange.value[0]}_${dateRange.value[1]}.csv` }).click()
}

watch(distMetric, () => { if (statsRows.value.length) renderDist() })

// init
loadOptions()
loadAll()
</script>

<style scoped>
/* ── 调色板（与 Flip7 Teal 主题统一，深浅色自适应） ── */
.up {
  --up-input:  #2BA8A2;
  --up-output: #27AE60;
  --up-cache:  #5DADE2;
  --up-cost:   #C7900A;
  --up-reason: #8B5CF6;
  --up-danger: #E74C3C;
  --up-warn:   #E0A100;

  --up-surface: var(--el-fill-color-blank);
  --up-border:  var(--el-border-color-light);
  --up-muted:   var(--el-text-color-secondary);
  --up-faint:   var(--el-text-color-placeholder);
  --up-ink:     var(--el-text-color-regular);
  --up-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 16px;
}

/* ── 页头 ── */
.up-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.up-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.up-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.up-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.up-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1E8C86));
}
.up-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.up-filter, .up-stat-card, .up-chart-card, .up-section {
  background: var(--up-surface);
  border: 1px solid var(--up-border);
  border-radius: 14px;
}

/* ── 筛选栏 ── */
.up-filter {
  display: flex; flex-direction: column; gap: 0; padding: 0;
  overflow: hidden;
}

/* 主筛选行：日期 + 激活提示 */
.up-filter-main {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 20px 12px;
  border-bottom: 1px solid var(--up-border);
}
.up-filter__date { width: 220px; flex-shrink: 0; }
.up-presets { display: flex; align-items: center; gap: 4px; }
.up-preset-btn {
  padding: 3px 10px;
  border-radius: 20px;
  border: 1px solid #dcdfe6;
  background: #f5f7fa;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  transition: all .15s;
  white-space: nowrap;
  line-height: 1.6;
}
.up-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.up-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.up-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 6px; flex-shrink: 0; }
.up-filter-active { display: flex; align-items: center; gap: 8px; margin-left: 6px; }
.up-filter-clear {
  font-size: 13px; color: var(--el-color-warning); padding: 0;
  height: auto; line-height: 1;
}

/* 次筛选行：维度过滤 */
.up-filter-dims {
  display: flex; align-items: center; gap: 0;
  flex-wrap: wrap; padding: 12px 20px;
  background: var(--el-fill-color-extra-light);
  row-gap: 10px;
}

/* 单个过滤组：label + control */
.up-filter-group {
  display: flex; align-items: center; gap: 7px; flex-shrink: 0;
}
.up-filter-label {
  font-size: 13px; font-weight: 500;
  color: var(--el-text-color-regular);
  white-space: nowrap; flex-shrink: 0;
  letter-spacing: .1px;
}

/* 分隔线 */
.up-filter-sep {
  width: 1px; height: 20px;
  background: var(--up-border);
  margin: 0 14px; flex-shrink: 0;
}

/* 覆写 el-input / el-select 内字体大小（size=small 默认 12px，调到 13px） */
.up-filter-dims :deep(.el-input__inner),
.up-filter-dims :deep(.el-select__selected-item),
.up-filter-dims :deep(.el-select__placeholder) {
  font-size: 13px !important;
}
.up-filter-main :deep(.el-input__inner),
.up-filter-main :deep(.el-range-input) {
  font-size: 13px !important;
}

.up-filter__group    { width: 140px; }
.up-filter__model    { width: 140px; }
.up-filter__provider { width: 120px; }
.up-filter__status   { width: 100px; }
.up-filter__key      { width: 150px; }
.up-filter__user     { width: 130px; }
.up-filter__dept     { width: 130px; }
.up-filter__studio   { width: 130px; }

/* 状态选项着色 */
.up-status-opt { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.up-status-opt--ok::before  { content: ''; display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #4ade80; }
.up-status-opt--err::before { content: ''; display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #f87171; }

/* ── 统计卡片 ── */
.up-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
@media (max-width: 1100px) { .up-stats { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px)  { .up-stats { grid-template-columns: 1fr 1fr; } }

.up-stat-card {
  position: relative; overflow: hidden;
  padding: 18px; display: flex; align-items: flex-start; gap: 14px;
  min-width: 0; transition: box-shadow .25s ease, transform .25s ease, border-color .25s ease;
}
.up-stat-card::after {
  content: ''; position: absolute; inset: 0 0 auto 0; height: 3px;
  background: var(--accent, var(--el-color-primary)); opacity: .9;
}
.up-stat-card:hover {
  box-shadow: 0 8px 24px rgba(43, 168, 162, .14);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}

.up-stat-card__icon {
  width: 46px; height: 46px; border-radius: 13px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.up-stat-card__icon--blue   { background: color-mix(in srgb, var(--up-input) 14%, transparent);  color: var(--up-input); }
.up-stat-card__icon--amber  { background: color-mix(in srgb, var(--up-warn) 16%, transparent);   color: var(--up-warn); }
.up-stat-card__icon--cyan   { background: color-mix(in srgb, var(--up-cache) 16%, transparent);  color: var(--up-cache); }
.up-stat-card__icon--green  { background: color-mix(in srgb, var(--up-output) 14%, transparent); color: var(--up-output); }

.up-stat-card:nth-child(1) { --accent: var(--up-input); }
.up-stat-card:nth-child(2) { --accent: var(--up-warn); }
.up-stat-card:nth-child(3) { --accent: var(--up-cache); }
.up-stat-card:nth-child(4) { --accent: var(--up-output); }

.up-stat-card__body { min-width: 0; flex: 1; }
.up-stat-card__label { font-size: 12px; color: var(--up-muted); margin: 0 0 6px; white-space: nowrap; }
.up-stat-card__value {
  font-size: 25px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 0 0 6px; line-height: 1.15; letter-spacing: -.3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.up-stat-card__sub  { font-size: 12px; color: var(--up-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }
.up-no-price-icon   { color: #f59e0b; cursor: help; font-size: 13px; }
.up-stat-card__rate { font-weight: 600; }
.up-stat-card--cost {}

/* ── 图表区 ── */
.up-charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 900px) { .up-charts { grid-template-columns: 1fr; } }

.up-chart-card { overflow: hidden; }
.up-chart-card__header,
.up-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--up-border); flex-wrap: wrap; gap: 8px;
}
.up-chart-card__title, .up-section__title {
  font-size: 14px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 8px;
}
.up-chart-card__title::before, .up-section__title::before {
  content: ''; width: 7px; height: 7px; border-radius: 999px;
  background: var(--el-color-primary);
}
.up-chart-card__body { padding: 14px 16px; height: 228px; position: relative; }

/* ── 区块 ── */
.up-section { overflow: hidden; }
.up-section__count { font-size: 12px; color: var(--up-muted); }
.up-table-wrap { overflow-x: auto; }

/* ── 表格细节 ── */
.up-table-wrap :deep(.el-table) {
  --el-table-border-color: var(--up-border);
  --el-table-bg-color: var(--up-surface);
  --el-table-tr-bg-color: var(--up-surface);
  --el-table-row-hover-bg-color: var(--el-color-primary-light-9);
  background: var(--up-surface);
}
/* 固定列需不透明底色才能遮住滚动内容 */
.up-table-wrap :deep(.el-table td.el-table__cell) {
  background: var(--up-surface);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.up-table-wrap :deep(.el-table__row:hover > td.el-table__cell) { background: var(--el-color-primary-light-9) !important; }
/* 固定列分隔阴影更柔和 */
.up-table-wrap :deep(.el-table .el-table-fixed-column--left.is-last-column.el-table__cell),
.up-table-wrap :deep(.el-table__fixed) {
  box-shadow: 6px 0 12px -6px rgba(0, 0, 0, .12);
}

/* ── 空态 ── */
.up-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.up-empty p { font-size: 13px; color: var(--up-faint); margin: 0; }
.up-topn-hint { display: flex; align-items: center; gap: 5px; padding: 6px 16px; font-size: 12px; color: var(--el-color-info); background: var(--el-color-info-light-9); border-radius: 4px; margin: 8px 0 2px; }
.up-stats-pager { display: flex; justify-content: flex-end; padding: 10px 4px 4px; }

/* ── 分页 ── */
.up-pagination { padding: 12px 16px; border-top: 1px solid var(--up-border); display: flex; justify-content: flex-end; }

/* ── 等宽文本 ── */
.up-mono { font-family: var(--up-mono); font-size: 11px; color: var(--up-muted); }
.up-mono--sm { font-size: 10px; }

/* ── 模型名 ── */
.up-model-name { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }
.up-model-name--muted { color: var(--up-muted); font-weight: 400; }
.up-model-cell { display: flex; flex-direction: column; gap: 2px; }
.up-model-routed { display: flex; align-items: center; gap: 3px; }
.up-arrow { color: var(--up-faint); font-size: 10px; }

/* ── 模型组列换行 ── */
.up-col--wrap { white-space: normal !important; word-break: break-all !important; overflow: visible !important; text-overflow: clip !important; }
.up-tag { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
.up-tag--group { max-width: none; white-space: normal; word-break: break-all; }
.up-group-name { font-size: 11px; color: var(--up-muted); word-break: break-all; line-height: 1.5; }
.up-provider { font-size: 11px; color: var(--up-muted); }
.up-dash { color: var(--up-faint); }

.up-token-row { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.up-token-item {
  display: inline-flex; align-items: center; gap: 3px; font-size: 11px; font-weight: 600;
  padding: 1px 7px; border-radius: 999px;
}
.up-token-item--in    { color: var(--up-input);  background: color-mix(in srgb, var(--up-input) 12%, transparent); }
.up-token-item--out   { color: var(--up-output); background: color-mix(in srgb, var(--up-output) 12%, transparent); }
.up-token-item--cache { color: var(--up-cache);  background: color-mix(in srgb, var(--up-cache) 14%, transparent); }
.up-token-item--reason{ color: var(--up-reason); background: color-mix(in srgb, var(--up-reason) 12%, transparent); font-size: 11px; font-weight: 600; }
.up-token-icon { font-size: 10px; }

.up-time { font-size: 12px; color: var(--up-muted); white-space: nowrap; font-family: var(--up-mono); }
.up-duration { font-size: 12px; color: var(--up-ink); font-variant-numeric: tabular-nums; }
.up-cost { font-family: var(--up-mono); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.up-flags { display: flex; flex-wrap: wrap; gap: 3px; justify-content: center; }
.up-pct { font-size: 10px; color: var(--up-cache); margin-left: 4px; }

/* ── 迷你进度条 ── */
.up-bar-cell { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.up-minibar { width: 100%; height: 4px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.up-minibar__fill { height: 100%; border-radius: 999px; transition: width .4s ease; }
.up-minibar__fill--blue  { background: linear-gradient(90deg, color-mix(in srgb, var(--up-input) 45%, transparent), var(--up-input)); }
.up-minibar__fill--green { background: linear-gradient(90deg, color-mix(in srgb, var(--up-output) 45%, transparent), var(--up-output)); }
.up-minibar__fill--red   { background: linear-gradient(90deg, color-mix(in srgb, var(--up-cost) 45%, transparent), var(--up-cost)); }

/* ── 颜色工具（与主题统一） ── */
.c-blue   { color: var(--up-input); }
.c-green  { color: var(--up-output); }
.c-cyan   { color: var(--up-cache); }
.c-orange { color: var(--up-warn); }
.c-amber  { color: var(--up-warn); }
.c-purple { color: var(--up-reason); }
.c-red    { color: var(--up-danger); }
.fw6      { font-weight: 600; }

.up-tag--effort { font-size: 10px; }

/* ── 耗时 / TTFT ── */
.up-timing { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; }
.up-ttft { font-size: 10px; color: var(--up-cache); }

/* ── 成本格 ── */
.up-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; white-space: nowrap; }

/* ── 圆形信息触发按钮（参考 sub2api） ── */
.up-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; border-radius: 999px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--up-faint);
  cursor: help; transition: background .15s ease, color .15s ease, transform .15s ease;
}
.up-info:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); transform: scale(1.08); }
.up-info .el-icon { font-size: 11px; }

/* ── UA / IP 来源格 ── */
.up-source { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.up-ua { display: block; font-size: 11px; color: var(--up-muted); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.up-ip { font-family: var(--up-mono); font-size: 10px; color: var(--up-faint); }

/* ── 用户名 ── */
.up-name { font-size: 12px; color: var(--el-text-color-regular); font-weight: 500; }
</style>

<!-- tooltip 样式（全局，el-tooltip popper 不走 scoped）— 参考 sub2api 深色浮层 -->
<style>
.up-tt-popper.el-popper {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
  border-radius: 10px !important;
  box-shadow: 0 12px 32px rgba(0, 0, 0, .35) !important;
  padding: 10px 12px !important;
}
.up-tt-popper.el-popper .el-popper__arrow::before {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
}

.up-tt {
  min-width: 172px;
  font-size: 12px;
  line-height: 1.7;
  font-variant-numeric: tabular-nums;
}
.up-tt__title {
  font-weight: 700;
  font-size: 12px;
  margin-bottom: 7px;
  color: #e5e7eb;
  border-bottom: 1px solid #374151;
  padding-bottom: 5px;
  letter-spacing: .2px;
}
.up-tt__row {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: center;
  color: #9ca3af;
}
.up-tt__sub { color: #6b7280; padding-left: 2px; }
.up-tt__group {
  display: flex;
  align-items: center;
  gap: 4px;
  margin: 2px 0 1px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .3px;
  color: #38bdf8;
}
.up-tt__group-ic { font-size: 12px; }
.up-tt__row b { color: #f9fafb; font-weight: 600; }
.up-tt__row b.c-cyan   { color: #38bdf8; }
.up-tt__row b.c-amber  { color: #fbbf24; }
.up-tt__row b.c-purple { color: #a78bfa; }
.up-tt__row b.c-blue   { color: #5eead4; }
.up-tt__row b.c-green  { color: #4ade80; }
.up-tt__sep { border-top: 1px solid #374151; margin: 6px 0; }

/* 来源 UA 悬浮：完整内容自动换行 */
.up-ua-popper.el-popper {
  max-width: 440px;
  line-height: 1.5;
  word-break: break-all;
}

/* ═══════════════════════════ Trace Drawer ═══════════════════════════ */
.up-trace-header { display: flex; flex-direction: column; gap: 2px; }
.up-trace-title  { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.up-trace-id     { font-size: 11px; color: var(--el-text-color-secondary); font-family: var(--up-mono); word-break: break-all; }

.up-trace-loading { display: flex; justify-content: center; align-items: center; padding: 60px 0; color: var(--el-color-primary); }
.up-trace-body    { display: flex; flex-direction: column; gap: 20px; }
.up-trace-empty   { text-align: center; padding: 40px 0; color: var(--el-text-color-secondary); font-size: 13px; }

.up-trace-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.up-trace-time { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 4px; }
.up-trace-dur  { font-size: 12px; color: var(--el-color-primary); font-weight: 600; }

/* ── Timeline ── */
.up-timeline {
  display: flex; align-items: flex-start; gap: 0;
  background: var(--el-fill-color-light);
  border-radius: 10px; padding: 16px 20px; overflow-x: auto;
}
.up-timeline--placeholder { opacity: .5; pointer-events: none; }
.up-tl-node {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  position: relative; flex: 1; min-width: 72px;
}
.up-tl-node + .up-tl-node::before {
  content: ''; position: absolute; left: -50%; top: 13px;
  width: 100%; height: 2px; background: var(--el-border-color);
  z-index: 0;
}
.up-tl-node.ok    + .up-tl-node::before { background: var(--el-color-success-light-5); }
.up-tl-node.error + .up-tl-node::before { background: var(--el-color-danger-light-5); }
.up-tl-dot {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--el-fill-color); border: 2px solid var(--el-border-color);
  font-size: 14px; position: relative; z-index: 1;
}
.up-tl-node.ok    .up-tl-dot { background: color-mix(in srgb, var(--el-color-success) 12%, #fff); border-color: var(--el-color-success); color: var(--el-color-success); }
.up-tl-node.error .up-tl-dot { background: color-mix(in srgb, var(--el-color-danger)  12%, #fff); border-color: var(--el-color-danger);  color: var(--el-color-danger); }
.up-tl-node--failover .up-tl-dot { background: color-mix(in srgb, var(--el-color-warning) 12%, #fff); border-color: var(--el-color-warning); color: var(--el-color-warning); }
.up-tl-node--failover .up-tl-label { color: var(--el-color-warning-dark-2); }
.up-tl-node--failover + .up-tl-node::before { background: var(--el-color-warning-light-7); }
.up-tl-failover-hint { font-size: 10px; color: var(--el-color-warning-dark-2); font-style: italic; }
.up-tag-failover { cursor: help; }
.up-tl-label { font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600; }
.up-tl-label-text { cursor: default; }
.up-tl-label-text[title], [data-tooltip] .up-tl-label-text { cursor: help; text-decoration: underline dotted; text-underline-offset: 2px; }
.up-tl-ms    { font-size: 11px; color: var(--el-text-color-regular); font-family: var(--up-mono); }

/* ── Content tabs ── */
.up-trace-tabs { margin-top: -8px; }
.up-msgs { display: flex; flex-direction: column; gap: 12px; }
.up-msg  { border-radius: 8px; overflow: hidden; border: 1px solid var(--el-border-color-light); }
.up-msg-role {
  display: block; padding: 5px 12px; font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .05em;
  background: var(--el-fill-color-light); color: var(--el-text-color-secondary);
}
.up-msg--system   .up-msg-role { background: #fef3c7; color: #92400e; }
.up-msg--user     .up-msg-role { background: #dbeafe; color: #1e40af; }
.up-msg--assistant .up-msg-role { background: #d1fae5; color: #065f46; }
.up-msg-content {
  margin: 0; padding: 12px; font-size: 12.5px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-word;
  font-family: var(--up-mono); background: var(--el-bg-color); max-height: 400px; overflow-y: auto;
}
.up-json-view {
  background: var(--el-fill-color-light); border-radius: 8px; padding: 14px;
  max-height: 500px; overflow-y: auto;
}
.up-json-view pre {
  margin: 0; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
  font-family: var(--up-mono);
}
.up-error-block { background: #fef2f2; border: 1px solid #fca5a5; border-radius: 8px; padding: 16px; }
.up-error-code  { font-size: 11px; font-weight: 700; color: #dc2626; text-transform: uppercase; margin-bottom: 8px; }
.up-error-msg   { margin: 0; font-size: 12.5px; color: #7f1d1d; white-space: pre-wrap; word-break: break-word; }

/* ── 调用元数据 grid ── */
.up-meta-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.up-meta-section {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 12px 14px;
  display: flex; flex-direction: column; gap: 6px;
}
.up-meta-heading {
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--el-text-color-secondary);
  margin-bottom: 4px; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 6px;
}
.up-meta-row {
  display: flex; justify-content: space-between; align-items: center; gap: 8px;
  font-size: 12.5px; min-height: 22px;
}
.up-meta-row > span:first-child { color: var(--el-text-color-secondary); flex-shrink: 0; }
.up-meta-row > span:last-child, .up-meta-row > b, .up-meta-row > code { text-align: right; word-break: break-all; }
.up-meta-row--wrap { align-items: flex-start; }
.up-meta-row--wrap > span:first-child { padding-top: 2px; }
.up-meta-sep { border-top: 1px solid var(--el-border-color-lighter); padding-top: 6px; margin-top: 2px; }
.up-meta-ua {
  font-family: var(--up-mono); font-size: 11px;
  color: var(--el-text-color-regular); word-break: break-all; text-align: right;
}
code { font-family: var(--up-mono); font-size: 12px; color: var(--el-text-color-primary); }
.c-cyan   { color: #06b6d4; }
.c-amber  { color: #f59e0b; }
.c-purple { color: #a78bfa; }
.c-blue   { color: #60a5fa; }
.c-green  { color: #4ade80; }

/* Headers Tab */
.up-headers-table { padding: 4px 0; }
.up-header-row {
  display: flex; gap: 12px; align-items: baseline;
  padding: 5px 8px; border-radius: 4px;
}
.up-header-row:hover { background: var(--el-fill-color-light); }
.up-header-key {
  font-family: var(--up-mono); font-size: 12px; color: var(--el-color-primary);
  min-width: 200px; flex-shrink: 0; word-break: break-all;
}
.up-header-val {
  font-family: var(--up-mono); font-size: 12px;
  color: var(--el-text-color-primary); word-break: break-all;
}
</style>