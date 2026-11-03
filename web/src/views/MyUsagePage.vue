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
        <el-radio-group v-model="currency" size="small" style="margin-right: 12px;">
          <el-radio-button value="USD">$ USD</el-radio-button>
          <el-radio-button value="CNY">¥ CNY</el-radio-button>
        </el-radio-group>
        <el-button :loading="statsLoading" :icon="Refresh" circle size="small" title="刷新" @click="loadAll" />
        <el-button :icon="Download" size="small" :disabled="!logRows.length" @click="handleExport">导出 ({{ logRows.length }})</el-button>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         筛选栏
    ═══════════════════════════════════════════ -->
    <div class="mu-filter">
      <div class="mu-presets">
        <button
          v-for="p in datePresets" :key="p.label"
          class="mu-preset-btn"
          :class="{ 'mu-preset-btn--active': activePreset === p.label }"
          @click="applyPreset(p)"
        >{{ p.label }}</button>
      </div>
      <div class="mu-filter-divider" />
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        range-separator="~"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        value-format="YYYY-MM-DD HH:mm"
        format="MM/DD HH:mm"
        :shortcuts="dateShortcuts"
        class="mu-filter__date"
        size="small"
        @change="onDateChange"
      />
      <div class="mu-filter-divider" />
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
      <el-select v-model="filterStatus" placeholder="状态" clearable size="small"
        class="mu-filter__status" @change="loadAll" @clear="loadAll">
        <el-option label="全部" value="" />
        <el-option value="success"><span class="mu-dot mu-dot--ok" /> 成功</el-option>
        <el-option value="error"><span class="mu-dot mu-dot--err" /> 失败</el-option>
      </el-select>
      <el-button v-if="activeFilterCount > 0" size="small" plain type="warning"
        :icon="Close" @click="clearFilters" class="mu-filter__clear">
        清空 {{ activeFilterCount }}
      </el-button>
    </div>

    <!-- ═══════════════════════════════════════════
         Section 2: 💰 花费总览
    ═══════════════════════════════════════════ -->
    <div class="mu-sec-label"><el-icon style="vertical-align:middle"><Coin /></el-icon> 花费总览</div>

    <div class="mu-stats" v-loading="statsLoading">
      <!-- 总花费 -->
      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--amber">
          <el-icon size="22"><Money /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">{{ periodLabel }}花费</p>
          <p class="mu-stat-card__value c-amber">{{ fmtCny(statsTotal?.est_cost_usd) }}</p>
          <p class="mu-stat-card__sub">
            <span v-if="costChangePct != null" :class="['mu-trend', costChangePct > 0 ? 'mu-trend--up' : 'mu-trend--down']">
              {{ costChangePct > 0 ? '↑' : '↓' }} {{ Math.abs(costChangePct) }}% 较上期
            </span>
            <span v-else-if="(statsTotal?.est_cost_usd ?? 0) > 0" class="c-cyan">新增记录</span>
            <span v-else class="mu-muted-text">暂无对比</span>
          </p>
        </div>
      </div>

      <!-- 日均花费 -->
      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--blue">
          <el-icon size="22"><DataLine /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">日均花费</p>
          <p class="mu-stat-card__value">{{ fmtCny(dailyAvgCost) }}</p>
          <p class="mu-stat-card__sub">
            共 <span class="c-blue">{{ rangeDays }}</span> 天统计周期
          </p>
        </div>
      </div>

      <!-- 总请求数 -->
      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--green">
          <el-icon size="22"><Histogram /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">总请求数</p>
          <p class="mu-stat-card__value">{{ fmtNum(statsTotal?.req_count) }}</p>
          <p class="mu-stat-card__sub">
            日均 <span class="c-green">{{ fmtNum(Math.round(dailyAvgCalls)) }}</span> 次
          </p>
        </div>
      </div>

      <!-- 总 Token -->
      <div class="mu-stat-card">
        <div class="mu-stat-card__icon mu-stat-card__icon--cyan">
          <el-icon size="22"><Coin /></el-icon>
        </div>
        <div class="mu-stat-card__body">
          <p class="mu-stat-card__label">总 Token</p>
          <p class="mu-stat-card__value">{{ fmtTokens(totalTokens) }}</p>
          <p class="mu-stat-card__sub">
            输入 <span class="c-blue">{{ fmtTokens(statsTotal?.prompt_tokens) }}</span>
            · 输出 <span class="c-green">{{ fmtTokens(statsTotal?.completion_tokens) }}</span>
          </p>
        </div>
      </div>
    </div>

    <!-- 每日花费趋势 -->
    <div v-if="dayStatsRows.length" class="mu-chart-card mu-cost-trend-card">
      <div class="mu-chart-card__header">
        <span class="mu-chart-card__title">每日花费趋势</span>
        <span class="mu-chart-card__hint">¥ CNY · 点击图例可切换</span>
      </div>
      <div class="mu-chart-card__body mu-chart-card__body--trend">
        <canvas ref="costTrendCanvas" />
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         ② 调用明细
    ═══════════════════════════════════════════ -->
    <UsageRecordTable
      :rows="logRows"
      :loading="logLoading"
      :total="logTotal"
      :page="logPage"
      :pageSize="logPageSize"
      :showKey="true"
      :showGroup="true"
      showSource="full"
      :pageSizes="[20, 50, 100, 200]"
      paginationLayout="total, sizes, prev, pager, next, jumper"
      :currency="currency"
      traceMetaMode="personal"
      :traceEndpoint="fetchTrace"
      :keyResolver="(row) => keyLabel(row.key_id, row.key_name || '')"
      @update:page="(p) => { logPage = p; loadLogs() }"
      @update:pageSize="(s) => { logPageSize = s; loadLogs() }"
    />

    <!-- ═══════════════════════════════════════════
         ②-B KEY 花费分布（默认折叠）
    ═══════════════════════════════════════════ -->
    <button
      type="button"
      class="mu-analysis-toggle"
      :aria-expanded="keyDistOpen"
      aria-controls="mu-key-dist-body"
      @click="keyDistOpen = !keyDistOpen"
    >
      <span class="mu-analysis-toggle__title">🔑 KEY 花费分布</span>
      <span class="mu-analysis-toggle__hint">{{ keyDistOpen ? '点击收起' : '查看各 Key 的花销占比' }}</span>
      <el-icon class="mu-analysis-toggle__chev" :class="{ 'is-open': keyDistOpen }" aria-hidden="true"><ArrowDown /></el-icon>
    </button>

    <div v-if="keyDistOpen" id="mu-key-dist-body" class="mu-analysis-body">
      <div v-if="keyDistLoading" v-loading="true" style="height: 200px;" />
      <div v-else-if="!keyDistRows.length" class="mu-empty-state">当前筛选条件下暂无数据</div>
      <template v-else>
        <div class="mu-key-dist-grid">
          <!-- KEY 花费分布 Doughnut -->
          <div class="mu-chart-card">
            <div class="mu-chart-card__header">
              <span class="mu-chart-card__title">花费分布</span>
              <span class="mu-chart-card__hint">点击扇形可筛选</span>
            </div>
            <div class="mu-chart-card__body mu-chart-card__body--doughnut">
              <canvas ref="keyDistCanvas" />
            </div>
          </div>

          <!-- KEY 花费排行 -->
          <div class="mu-chart-card">
            <div class="mu-chart-card__header">
              <span class="mu-chart-card__title">花费排行</span>
              <span class="mu-chart-card__hint">按总花费降序</span>
            </div>
            <div class="mu-rank-list-wrap">
              <div v-for="(row, idx) in keyDistSorted" :key="row.key_id || idx" class="mu-rank-item">
                <span class="mu-rank-idx" :class="['mu-rank-idx--' + (idx < 3 ? idx+1 : 'rest')]">{{ idx+1 }}</span>
                <div class="mu-rank-info">
                  <span class="mu-rank-model" :title="row.key_name || row.key_id">{{ row.key_name || maskKey(row.key_id) }}</span>
                  <div class="mu-rank-bar-wrap">
                    <div class="mu-rank-bar"
                         :style="{ width: pct(row.est_cost_usd, maxKeyCost) + '%', background: rankBarColor(idx) }" />
                  </div>
                </div>
                <span class="mu-rank-cost">{{ fmtPreciseCny(row.est_cost_usd) }}</span>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- ═══════════════════════════════════════════
         ③ 习惯与深度分析（默认折叠，懒加载图表）
    ═══════════════════════════════════════════ -->
    <button
      type="button"
      class="mu-analysis-toggle"
      :aria-expanded="analysisOpen"
      aria-controls="mu-analysis-body"
      @click="analysisOpen = !analysisOpen"
    >
      <span class="mu-analysis-toggle__title">📊 习惯与深度分析</span>
      <span class="mu-analysis-toggle__hint">{{ analysisOpen ? '点击收起' : '模型偏好 · 时段分布 · 效率洞察' }}</span>
      <el-icon class="mu-analysis-toggle__chev" :class="{ 'is-open': analysisOpen }" aria-hidden="true"><ArrowDown /></el-icon>
    </button>

    <div v-if="analysisOpen" id="mu-analysis-body" class="mu-analysis-body">

    <!-- ═══════════════════════════════════════════
         模型偏好
    ═══════════════════════════════════════════ -->
    <template v-if="modelStatsRows.length">
      <div class="mu-sec-label">🤖 模型偏好</div>
      <div class="mu-model-pref-grid">
        <!-- 模型请求分布 Doughnut -->
        <div class="mu-chart-card">
          <div class="mu-chart-card__header">
            <span class="mu-chart-card__title">请求分布</span>
            <span class="mu-chart-card__hint">点击扇形可筛选</span>
          </div>
          <div class="mu-chart-card__body mu-chart-card__body--doughnut">
            <canvas ref="distCanvas" />
          </div>
        </div>

        <!-- 模型花费排行 -->
        <div class="mu-chart-card">
          <div class="mu-chart-card__header">
            <span class="mu-chart-card__title">花费排行</span>
            <span class="mu-chart-card__hint">按总花费降序</span>
          </div>
          <div class="mu-rank-list-wrap">
            <div v-for="(row, idx) in modelByCost" :key="row.model || idx" class="mu-rank-item">
              <span class="mu-rank-idx" :class="['mu-rank-idx--' + (idx < 3 ? idx+1 : 'rest')]">{{ idx+1 }}</span>
              <div class="mu-rank-info">
                <span class="mu-rank-model" :title="row.model">{{ row.model || '未知' }}</span>
                <div class="mu-rank-bar-wrap">
                  <div class="mu-rank-bar"
                       :style="{ width: pct(row.est_cost_usd, maxModelCost) + '%', background: rankBarColor(idx) }" />
                </div>
              </div>
              <span class="mu-rank-cost">{{ fmtPreciseCny(row.est_cost_usd) }}</span>
            </div>
            <div v-if="!modelByCost.length" class="mu-rank-empty">暂无数据</div>
          </div>
        </div>
      </div>

      <!-- 性价比对比 bar chart -->
      <div v-if="costPerMRows.length" class="mu-chart-card">
        <div class="mu-chart-card__header">
          <span class="mu-chart-card__title">性价比对比</span>
          <span class="mu-chart-card__hint">每百万 Token 花费 ({{ currency }})，越低越划算</span>
        </div>
        <div class="mu-chart-card__body" :style="{ height: Math.max(180, costPerMRows.length * 36 + 30) + 'px' }">
          <canvas ref="costPerMCanvas" />
        </div>
      </div>
    </template>

    <!-- ═══════════════════════════════════════════
         Section 4: 📅 时间习惯
    ═══════════════════════════════════════════ -->
    <template v-if="hourStatsRows.length || dayStatsRows.length">
      <div class="mu-sec-label">📅 时间习惯</div>
      <div class="mu-time-grid">
        <!-- 24 小时活跃热力图 -->
        <div class="mu-chart-card">
          <div class="mu-chart-card__header">
            <span class="mu-chart-card__title">24 小时活跃分布</span>
            <span class="mu-chart-card__hint" v-if="peakHour.count > 0">峰值 {{ peakHour.hour }}:00 · {{ fmtNum(peakHour.count) }} 次</span>
          </div>
          <div class="mu-heatmap-body">
            <div class="mu-heatmap-grid">
              <el-tooltip
                v-for="cell in heatmapData" :key="cell.hour"
                :content="`${cell.hour}:00–${cell.hour+1}:00  ${fmtNum(cell.count)} 次调用`"
                placement="top"
                :show-after="80"
              >
                <div class="mu-heatmap-cell"
                     :style="{ background: `rgba(59,130,246,${Math.max(cell.opacity, cell.count > 0 ? 0.12 : 0.04)})`, outline: cell.hour === peakHour.hour && peakHour.count > 0 ? '2px solid rgba(59,130,246,0.6)' : 'none' }" />
              </el-tooltip>
            </div>
            <div class="mu-heatmap-labels">
              <span v-for="cell in heatmapData" :key="cell.hour" class="mu-heatmap-label">
                {{ cell.hour % 6 === 0 ? cell.hour : '' }}
              </span>
            </div>
            <div v-if="!hourStatsRows.length" class="mu-heatmap-empty">当前筛选条件下暂无小时级数据</div>
          </div>
        </div>

        <!-- 工作日 vs 周末 -->
        <div class="mu-chart-card">
          <div class="mu-chart-card__header">
            <span class="mu-chart-card__title">工作日 vs 周末</span>
          </div>
          <div class="mu-daytype-body">
            <div class="mu-daytype-col">
              <div class="mu-daytype-label">🗓 工作日</div>
              <div class="mu-daytype-val">{{ fmtNum(weekdayVsWeekend.weekday.calls) }}</div>
              <div class="mu-daytype-sub">次调用</div>
              <div class="mu-daytype-avg">日均 {{ fmtNum(Math.round(weekdayVsWeekend.weekday.avgCalls)) }} 次</div>
              <div class="mu-daytype-cost">{{ fmtCny(weekdayVsWeekend.weekday.cost) }}</div>
              <div class="mu-daytype-days">{{ weekdayVsWeekend.weekday.days }} 天</div>
            </div>
            <div class="mu-daytype-divider" />
            <div class="mu-daytype-col">
              <div class="mu-daytype-label">🏖 周末</div>
              <div class="mu-daytype-val">{{ fmtNum(weekdayVsWeekend.weekend.calls) }}</div>
              <div class="mu-daytype-sub">次调用</div>
              <div class="mu-daytype-avg">日均 {{ fmtNum(Math.round(weekdayVsWeekend.weekend.avgCalls)) }} 次</div>
              <div class="mu-daytype-cost">{{ fmtCny(weekdayVsWeekend.weekend.cost) }}</div>
              <div class="mu-daytype-days">{{ weekdayVsWeekend.weekend.days }} 天</div>
            </div>
          </div>
          <div class="mu-daytype-bar-row" v-if="weekdayVsWeekend.weekday.calls + weekdayVsWeekend.weekend.calls > 0">
            <div class="mu-daytype-bar-wd" :style="{ flex: weekdayVsWeekend.weekday.calls || 0.01 }" />
            <div class="mu-daytype-bar-we" :style="{ flex: weekdayVsWeekend.weekend.calls || 0.01 }" />
          </div>
        </div>
      </div>
    </template>

    <!-- ═══════════════════════════════════════════
         Section 5: ⚡ 效率洞察
    ═══════════════════════════════════════════ -->
    <div class="mu-sec-label"><el-icon style="vertical-align:middle"><Lightning /></el-icon> 效率洞察</div>
    <div class="mu-efficiency-grid" v-loading="statsLoading">
      <!-- 平均响应时间 -->
      <div class="mu-eff-card">
        <div class="mu-eff-card__icon" style="color: #3b82f6;">⏱</div>
        <div class="mu-eff-card__body">
          <div class="mu-eff-card__label">平均响应时间</div>
          <div class="mu-eff-card__value">{{ avgDurationMs != null ? fmtDuration(avgDurationMs) : '—' }}</div>
          <div class="mu-eff-card__desc">端到端总耗时均值（近 200 条）</div>
        </div>
      </div>

      <!-- 缓存命中率 -->
      <div class="mu-eff-card">
        <div class="mu-eff-card__icon" style="color: #10b981;">💾</div>
        <div class="mu-eff-card__body">
          <div class="mu-eff-card__label">缓存命中率</div>
          <div class="mu-eff-card__value">{{ cacheHitPct }}<span class="mu-eff-unit">%</span></div>
          <div class="mu-eff-cache-bar">
            <div class="mu-eff-cache-fill" :style="{ width: cacheHitPct + '%' }" />
          </div>
          <div class="mu-eff-card__desc">命中 {{ fmtTokens(statsTotal?.cached_tokens) }} / 输入 {{ fmtTokens(statsTotal?.prompt_tokens) }}</div>
        </div>
      </div>

      <!-- 输入输出比 -->
      <div class="mu-eff-card">
        <div class="mu-eff-card__icon" style="color: #8b5cf6;">⇄</div>
        <div class="mu-eff-card__body">
          <div class="mu-eff-card__label">输入输出比</div>
          <div class="mu-eff-card__value">{{ inputOutputRatio }}</div>
          <div class="mu-eff-card__desc">
            净输入 {{ fmtTokens(netInputTokens) }} → 输出 {{ fmtTokens(statsTotal?.completion_tokens) }}
          </div>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         Section 6: 📋 模型明细（可折叠）
    ═══════════════════════════════════════════ -->
    <div class="mu-section">
      <div class="mu-section__header mu-section-toggle" @click="modelDetailOpen = !modelDetailOpen">
        <span class="mu-section__title">📋 模型明细</span>
        <div class="mu-toggle-row">
          <el-radio-group v-if="modelDetailOpen" v-model="groupBy" size="small"
            @change="loadStats" @click.stop>
            <el-radio-button value="day">按天</el-radio-button>
            <el-radio-button value="model">按模型</el-radio-button>
            <el-radio-button value="key">按 Key</el-radio-button>
          </el-radio-group>
          <span class="mu-toggle-btn">{{ modelDetailOpen ? '收起 ▲' : '展开 ▼' }}</span>
        </div>
      </div>
      <div v-show="modelDetailOpen" class="mu-table-wrap" v-loading="statsLoading">
        <el-table :data="statsRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%"
          :default-sort="{ prop: 'est_cost_usd', order: 'descending' }">
          <el-table-column v-if="groupBy==='day'" prop="stat_date" label="日期" min-width="110" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-if="groupBy==='model'" prop="model" label="模型" min-width="160" sortable>
            <template #default="{ row }"><span class="mu-model-name">{{ row.model || '—' }}</span></template>
          </el-table-column>
          <el-table-column v-if="groupBy==='key'" prop="key_name" label="Key 名称" min-width="180" sortable>
            <template #default="{ row }">
              <span v-if="row.key_name">{{ row.key_name }}</span>
              <span v-else-if="row.key_id && row.key_id !== 'unknown'" class="mu-muted-text">{{ row.key_id.slice(0, 8) }}…</span>
              <span v-else class="mu-muted-text">未知 Key</span>
            </template>
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
          <el-table-column prop="est_cost_usd" label="估算花费" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="mu-bar-cell">
                <span :class="costColor(row.est_cost_usd)">{{ fmtPreciseCny(row.est_cost_usd) }}</span>
                <div class="mu-minibar"><div class="mu-minibar__fill mu-minibar__fill--red" :style="{ width: pct(row.est_cost_usd, maxCost) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!statsLoading && !statsRows.length" class="mu-empty">
          <el-icon size="48" color="var(--el-text-color-placeholder)"><Histogram /></el-icon>
          <p>暂无汇总数据</p>
        </div>
      </div>
    </div>

    </div>
    <!-- /③ 习惯与深度分析 -->


  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import {
  Refresh, Download, Histogram, Money, DataLine, Coin,
  Close, Grid, Cpu, ArrowDown, Lightning
} from '@element-plus/icons-vue'
import UsageRecordTable from '@/components/usage/UsageRecordTable.vue'
import type { LogRow, TraceData } from '@/components/usage/UsageTraceDrawer.vue'
import {
  Chart, LineController, DoughnutController, BarController, CategoryScale, LinearScale,
  LineElement, PointElement, ArcElement, BarElement,
  Tooltip, Legend, Filler
} from 'chart.js'
import http from '@/services/http'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { isDark } from '@/composables/useTheme'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import { fmtDateTime, fmtDate, fmtDuration } from '@/utils/time'

Chart.register(
  LineController, DoughnutController, BarController,
  CategoryScale, LinearScale, LineElement, PointElement, ArcElement, BarElement,
  Tooltip, Legend, Filler,
)

const authStore = useAuthStore()
const settingsStore = useSettingsStore()

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

const CHART_PALETTE = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#f97316']
const RANK_COLORS = ['#f59e0b', '#94a3b8', '#92400e', '#3b82f6', '#10b981', '#8b5cf6', '#06b6d4', '#ec4899']

const currency = ref<Currency>('CNY')
const cc = computed(() => ({
  currency: currency.value,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const toCny = (usd: number | undefined | null) => convertCost(usd ?? 0, cc.value)
const toCnyFormatted = (usd: number | undefined | null) => {
  const n = convertCost(usd ?? 0, cc.value)
  return `${currencySymbol(cc.value.currency)}${n.toFixed(6)}`
}
const fmtCny = (usd: number | undefined | null) => {
  const n = convertCost(usd ?? 0, cc.value)
  const sym = currencySymbol(cc.value.currency)
  return `${sym}${n.toFixed(2)}`
}

function chartTheme() {
  const dark = isDark.value
  return {
    textSecondary: dark ? 'rgba(255,255,255,0.5)' : '#94a3b8',
    textRegular:   dark ? 'rgba(255,255,255,0.7)' : '#606266',
    grid:          dark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.06)',
    primary: '#3b82f6',
    success: '#10b981',
    warning: '#f59e0b',
  }
}

// ── Types ──
interface StatsRow {
  stat_date?: string; model?: string
  key_id?: string; key_name?: string
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number
  reasoning_tokens?: number; est_cost_usd: number
}
interface StatsTotal {
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number
  reasoning_tokens?: number; est_cost_usd: number
}

// ── Date helpers ──
const fmtLocal = (d: Date) => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const fmtLocalDateTime = (d: Date) => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hour = String(d.getHours()).padStart(2, '0')
  const minute = String(d.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day} ${hour}:${minute}`
}
const parseLocalDate = (s: string) => {
  // 支持 "YYYY-MM-DD" 和 "YYYY-MM-DD HH:mm" 格式
  const [datePart] = s.split(' ')
  const [y, m, d] = datePart.split('-').map(Number)
  return new Date(y, m - 1, d)
}
const parseLocalDateTime = (s: string) => {
  // 支持 "YYYY-MM-DD" 和 "YYYY-MM-DD HH:mm" 格式
  const parts = s.split(' ')
  const [y, m, d] = parts[0].split('-').map(Number)
  if (parts[1]) {
    const [h, mi] = parts[1].split(':').map(Number)
    return new Date(y, m - 1, d, h, mi)
  }
  return new Date(y, m - 1, d)
}
const initEnd = new Date()
const initStart = new Date()
initStart.setDate(initEnd.getDate() - 6)
initStart.setHours(0, 0, 0, 0)
initEnd.setHours(23, 59, 59, 999)
const dateRange = ref<[string, string]>([fmtLocalDateTime(initStart), fmtLocalDateTime(initEnd)])
const dateShortcuts = [
  { text: '今天',     value: () => { const t = new Date(); const s = new Date(t); s.setHours(0,0,0,0); return [fmtLocalDateTime(s), fmtLocalDateTime(t)] as [string, string] } },
  { text: '昨天',     value: () => { const d = new Date(); d.setDate(d.getDate() - 1); d.setHours(0,0,0,0); const e = new Date(d); e.setHours(23,59,59,999); return [fmtLocalDateTime(d), fmtLocalDateTime(e)] as [string, string] } },
  { text: '最近 7 天', value: () => { const end = new Date(); const start = new Date(); start.setDate(end.getDate() - 6); start.setHours(0,0,0,0); return [fmtLocalDateTime(start), fmtLocalDateTime(end)] as [string, string] } },
  { text: '最近 30 天', value: () => { const end = new Date(); const start = new Date(); start.setDate(end.getDate() - 29); start.setHours(0,0,0,0); return [fmtLocalDateTime(start), fmtLocalDateTime(end)] as [string, string] } },
  { text: '本月',     value: () => { const end = new Date(); const start = new Date(end.getFullYear(), end.getMonth(), 1); start.setHours(0,0,0,0); return [fmtLocalDateTime(start), fmtLocalDateTime(end)] as [string, string] } },
]
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
  start.setHours(0, 0, 0, 0)
  dateRange.value = [fmtLocalDateTime(start), fmtLocalDateTime(end)]
  activePreset.value = p.label
  loadAll()
}
function onDateChange() {
  activePreset.value = ''
  loadAll()
}

// ── Filters ──
const filterKeyId     = ref('')
const filterModelName = ref('')
const filterGroupName = ref('')
const filterStatus    = ref('')
const groupBy = ref('day')
const modelDetailOpen = ref(false)
const analysisOpen = ref(false)
const keyDistOpen = ref(false)

const activeFilterCount = computed(() =>
  [filterKeyId.value, filterModelName.value, filterGroupName.value, filterStatus.value].filter(Boolean).length
)
function clearFilters() {
  filterKeyId.value = filterModelName.value = filterGroupName.value = filterStatus.value = ''
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
      .map((k: any) => ({ value: k.id, label: k.name || k.key_hint || maskKey(k.id) }))
  } catch (e) { console.warn('[my-keys] failed to load', e) }
}

// ── State ──
const statsLoading = ref(false)
const statsRows = ref<StatsRow[]>([])
const statsTotal = ref<StatsTotal | null>(null)
const dayStatsRows = ref<StatsRow[]>([])
const modelStatsRows = ref<StatsRow[]>([])
const hourStatsRows = ref<StatsRow[]>([])
const prevPeriodTotal = ref<StatsTotal | null>(null)
const avgDurationMs = ref<number | null>(null)
const logLoading = ref(false)
const logRows = ref<LogRow[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(50)

// ── KEY 花费分布 ──
const keyDistLoading = ref(false)
const keyDistRows = ref<StatsRow[]>([])

// ── Derived stats ──
const rangeDays = computed(() => {
  const [from, to] = dateRange.value
  if (!from || !to) return 1
  const fromDt = parseLocalDateTime(from)
  const toDt = parseLocalDateTime(to)
  const diff = toDt.getTime() - fromDt.getTime()
  // 至少 1 天，小时精度下用小数
  return Math.max(1, diff / 86400000)
})

const periodLabel = computed(() => {
  const [from, to] = dateRange.value
  if (!from || !to) return '选定期间'
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const todayStr = fmtLocal(today)
  // 提取日期部分进行比较
  const fromDate = from.split(' ')[0]
  const toDate = to.split(' ')[0]
  if (fromDate === toDate && fromDate === todayStr) return '今日'
  const yesterday = new Date(today); yesterday.setDate(yesterday.getDate() - 1)
  if (fromDate === toDate && fromDate === fmtLocal(yesterday)) return '昨日'
  const monthStart = new Date(today.getFullYear(), today.getMonth(), 1)
  if (fromDate === fmtLocal(monthStart) && toDate === todayStr) return '本月'
  const d7 = new Date(today); d7.setDate(d7.getDate() - 6)
  if (fromDate === fmtLocal(d7) && toDate === todayStr) return '近 7 天'
  const d30 = new Date(today); d30.setDate(d30.getDate() - 29)
  if (fromDate === fmtLocal(d30) && toDate === todayStr) return '近 30 天'
  // 显示完整日期时间范围
  return `${from} ~ ${to}`
})

const dailyAvgCalls = computed(() => (statsTotal.value?.req_count ?? 0) / rangeDays.value)
const dailyAvgCost = computed(() => (statsTotal.value?.est_cost_usd ?? 0) / rangeDays.value)

const costChangePct = computed(() => {
  const cur = statsTotal.value?.est_cost_usd ?? 0
  const prev = prevPeriodTotal.value?.est_cost_usd ?? 0
  if (prev <= 0) return null
  return Math.round((cur - prev) / prev * 100)
})

const totalTokens = computed(() => {
  const t = statsTotal.value
  return (t?.prompt_tokens || 0) + (t?.completion_tokens || 0)
})

const netInputTokens = computed(() => {
  const t = statsTotal.value
  return Math.max((t?.prompt_tokens || 0) - (t?.cached_tokens || 0), 0)
})

const cacheHitPct = computed(() => {
  const t = statsTotal.value
  if (!t?.prompt_tokens) return 0
  return Math.round(t.cached_tokens / t.prompt_tokens * 100)
})

const inputOutputRatio = computed(() => {
  const t = statsTotal.value
  if (!t) return '—'
  const input = Math.max((t.prompt_tokens || 0) - (t.cached_tokens || 0), 1)
  const output = t.completion_tokens || 0
  if (!output) return '—'
  const ratio = output / input
  return `1:${ratio.toFixed(1)}`
})

// ── Heatmap ──
const heatmapData = computed(() => {
  const buckets = Array(24).fill(0)
  for (const row of hourStatsRows.value) {
    const match = (row.stat_date || '').match(/[T ](\d{2})/)
    if (match) {
      const hour = parseInt(match[1])
      if (hour >= 0 && hour < 24) buckets[hour] += row.req_count || 0
    }
  }
  const max = Math.max(...buckets, 1)
  return buckets.map((count, hour) => ({ hour, count, opacity: count / max }))
})

const peakHour = computed(() => {
  if (!heatmapData.value.length) return { hour: 0, count: 0 }
  return heatmapData.value.reduce((a, b) => b.count > a.count ? b : a, { hour: 0, count: 0 })
})

// ── Weekday vs Weekend ──
const weekdayVsWeekend = computed(() => {
  let wdCalls = 0, weCalls = 0, wdCost = 0, weCost = 0, wdDays = 0, weDays = 0
  for (const row of dayStatsRows.value) {
    if (!row.stat_date) continue
    const dateStr = row.stat_date.split('T')[0]
    const d = parseLocalDate(dateStr)
    const dow = d.getDay()
    if (dow === 0 || dow === 6) {
      weCalls += row.req_count || 0; weCost += row.est_cost_usd || 0; weDays++
    } else {
      wdCalls += row.req_count || 0; wdCost += row.est_cost_usd || 0; wdDays++
    }
  }
  return {
    weekday: { calls: wdCalls, cost: wdCost, days: wdDays, avgCalls: wdDays ? wdCalls / wdDays : 0 },
    weekend: { calls: weCalls, cost: weCost, days: weDays, avgCalls: weDays ? weCalls / weDays : 0 },
  }
})

// ── Model ranking ──
const modelByCost = computed(() => {
  return [...modelStatsRows.value]
    .sort((a, b) => (b.est_cost_usd || 0) - (a.est_cost_usd || 0))
    .slice(0, 8)
})
const maxModelCost = computed(() => Math.max(...modelByCost.value.map(r => r.est_cost_usd || 0), 0.0001))

// ── KEY 花费分布 ──
const keyDistSorted = computed(() => {
  return [...keyDistRows.value]
    .sort((a, b) => (b.est_cost_usd || 0) - (a.est_cost_usd || 0))
    .slice(0, 8)
})
const maxKeyCost = computed(() => Math.max(...keyDistSorted.value.map(r => r.est_cost_usd || 0), 0.0001))

function rankBarColor(idx: number) {
  return RANK_COLORS[idx] || '#3b82f6'
}

// ── Cost per M tokens ──
const costPerMRows = computed(() => {
  return [...modelStatsRows.value]
    .map(r => {
      const total = (r.prompt_tokens || 0) + (r.completion_tokens || 0)
      return { ...r, cpm: total ? (r.est_cost_usd || 0) / total * 1e6 : 0 }
    })
    .filter(r => r.cpm > 0)
    .sort((a, b) => b.cpm - a.cpm)
    .slice(0, 8)
})

// ── Table bar helpers ──
const maxPrompt     = computed(() => Math.max(...statsRows.value.map(r => r.prompt_tokens || 0), 1))
const maxCompletion = computed(() => Math.max(...statsRows.value.map(r => r.completion_tokens || 0), 1))
const maxCost       = computed(() => Math.max(...statsRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const pct = (v: number, max: number) => Math.min(Math.round(v / max * 100), 100)
const costColor = (v: number) => v > maxCost.value * 0.7 ? 'c-red fw6' : v > maxCost.value * 0.3 ? 'c-amber fw6' : 'c-green'

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'
const fmtPreciseCny = (v?: number | null): string => {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}
const fmtPrice = (n?: number | null) => {
  const sym = currencySymbol(cc.value.currency)
  if (n == null) return '—'
  if (n === 0) return `${sym}0`
  const v = convertCost(n, cc.value)
  return sym + (Math.abs(v) >= 1 ? v.toFixed(2) : v.toFixed(4).replace(/0+$/, '').replace(/\.$/, ''))
}
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const maskKey = (k?: string) => !k ? '—' : k.length > 14 ? k.slice(0, 6) + '···' + k.slice(-4) : k
function keyLabel(keyId: string, keyName: string): string {
  if (keyName) return keyName
  const found = myKeyOptions.value.find(k => k.value === keyId)
  return found?.label || maskKey(keyId)
}

// ── Build params ──
function buildStatsParams(groupByVal: string): Record<string, string> {
  const params: Record<string, string> = {
    from: dateRange.value[0], to: dateRange.value[1], group_by: groupByVal,
  }
  if (filterModelName.value) params.model_name = filterModelName.value
  if (filterGroupName.value) params.group_name = filterGroupName.value
  if (filterKeyId.value)     params.key_id     = filterKeyId.value
  if (filterStatus.value)    params.status     = filterStatus.value
  return params
}

// ── Load prev period ──
async function loadPrevPeriodStats() {
  const [from, to] = dateRange.value
  if (!from || !to) return
  const fromD = parseLocalDateTime(from); const toD = parseLocalDateTime(to)
  const diffMs = toD.getTime() - fromD.getTime()
  const prevEnd = new Date(fromD.getTime() - 1) // 当前期间开始的前一毫秒
  const prevStart = new Date(prevEnd.getTime() - diffMs)
  try {
    const params = buildStatsParams('day')
    params.from = fmtLocalDateTime(prevStart); params.to = fmtLocalDateTime(prevEnd)
    const r = await http.get('/api/v1/my/usage', { params })
    prevPeriodTotal.value = r.data.total || null
  } catch { prevPeriodTotal.value = null }
}

// ── Load avg duration ──
async function loadAvgDuration() {
  if (!dateRange.value?.[0]) return
  try {
    const params: Record<string, string | number> = {
      from: dateRange.value[0], to: dateRange.value[1], page: 1, page_size: 200,
    }
    if (filterModelName.value) params.model_name = filterModelName.value
    if (filterGroupName.value) params.group_name = filterGroupName.value
    if (filterKeyId.value)     params.key_id     = filterKeyId.value
    if (filterStatus.value)    params.status     = filterStatus.value
    const r = await http.get('/api/v1/my/usage/logs', { params, _silent: true } as any)
    const rows: LogRow[] = r.data.rows || []
    avgDurationMs.value = rows.length
      ? Math.round(rows.reduce((s, row) => s + (row.duration_ms || 0), 0) / rows.length)
      : null
  } catch { avgDurationMs.value = null }
}

// ── Load stats ──
async function loadStats() {
  if (!dateRange.value?.[0]) return
  statsLoading.value = true
  try {
    const [tableRes, dayRes, modelRes, hourRes] = await Promise.all([
      http.get('/api/v1/my/usage', { params: buildStatsParams(groupBy.value) }),
      http.get('/api/v1/my/usage', { params: buildStatsParams('day') }),
      http.get('/api/v1/my/usage', { params: buildStatsParams('model') }),
      http.get('/api/v1/my/usage', { params: buildStatsParams('hour') }),
    ])
    statsRows.value      = tableRes.data.rows || []
    statsTotal.value     = tableRes.data.total || null
    dayStatsRows.value   = dayRes.data.rows || []
    modelStatsRows.value = modelRes.data.rows || []
    hourStatsRows.value  = hourRes.data.rows || []
    await loadPrevPeriodStats()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || e?.response?.data?.error || '用量查询失败')
  } finally {
    statsLoading.value = false
  }
  await nextTick()
  try {
    renderCostTrend()
    if (analysisOpen.value) {
      renderDist()
      renderCostPerM()
    }
  } catch (e) {
    console.error('[my-usage] chart render failed', e)
  }
}

// ── Load logs ──
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
    logRows.value  = r.data.rows || []
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
  loadAvgDuration()
  if (keyDistOpen.value) loadKeyDist()
}

// ── Load KEY 花费分布 ──
async function loadKeyDist() {
  if (!dateRange.value?.[0]) return
  keyDistLoading.value = true
  try {
    const r = await http.get('/api/v1/my/usage', { params: buildStatsParams('key') })
    keyDistRows.value = r.data.rows || []
    await nextTick()
    renderKeyDist()
  } catch (e: any) {
    console.error('[my-usage] key dist load failed', e)
  } finally {
    keyDistLoading.value = false
  }
}

// ── Charts ──
const costTrendCanvas = ref<HTMLCanvasElement | null>(null)
const distCanvas      = ref<HTMLCanvasElement | null>(null)
const costPerMCanvas  = ref<HTMLCanvasElement | null>(null)
const keyDistCanvas   = ref<HTMLCanvasElement | null>(null)
let costTrendChart: Chart | null = null
let distChart: Chart | null = null
let costPerMChart: Chart | null = null
let keyDistChart: Chart | null = null

// Doughnut center plugin
const doughnutCenterPlugin = {
  id: 'doughnutCenter',
  afterDraw(chart: Chart) {
    const ds = chart.data.datasets[0]
    if (!ds?.data?.length) return
    const total = (ds.data as number[]).reduce((a, b) => a + b, 0)
    const { left, right, top, bottom } = chart.chartArea
    const x = (left + right) / 2; const y = (top + bottom) / 2
    const colors = chartTheme(); const ctx = chart.ctx
    ctx.save()
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle'
    ctx.font = 'bold 18px sans-serif'
    ctx.fillStyle = colors.textRegular
    ctx.fillText(fmtNum(total), x, y - 6)
    ctx.font = '11px sans-serif'
    ctx.fillStyle = colors.textSecondary
    ctx.fillText('总调用', x, y + 14)
    ctx.restore()
  },
}

const segmentLabelPlugin = {
  id: 'segmentLabel',
  afterDraw(chart: Chart) {
    if (chart.config.type !== 'doughnut') return
    const ds = chart.data.datasets[0]
    if (!ds?.data?.length) return
    const total = (ds.data as number[]).reduce((a, b) => a + b, 0)
    if (!total) return
    const meta = chart.getDatasetMeta(0); const ctx = chart.ctx
    ctx.save(); ctx.textAlign = 'center'; ctx.textBaseline = 'middle'; ctx.font = 'bold 11px sans-serif'
    meta.data.forEach((arc: any, i: number) => {
      const value = (ds.data[i] as number) || 0
      const pctV = Math.round(value / total * 100)
      if (pctV < 5) return
      const { x, y } = arc.tooltipPosition()
      ctx.fillStyle = '#fff'
      ctx.fillText(`${pctV}%`, x, y)
    })
    ctx.restore()
  },
}

function renderCostTrend() {
  const canvas = costTrendCanvas.value
  if (!canvas || !dayStatsRows.value.length) return
  costTrendChart?.destroy()

  const colors = chartTheme()
  const sorted = [...dayStatsRows.value].sort((a, b) => (a.stat_date || '').localeCompare(b.stat_date || ''))
  const labels = sorted.map(r => fmtDate(r.stat_date))

  costTrendChart = new Chart(canvas, {
    type: 'line',
    data: {
      labels,
      datasets: [
        {
          label: `花费 (${currency.value})`,
          data: sorted.map(r => toCny(r.est_cost_usd)),
          borderColor: isDark.value ? '#60a5fa' : '#3b82f6',
          backgroundColor: isDark.value ? 'rgba(96,165,250,0.15)' : 'rgba(59,130,246,0.12)',
          borderWidth: 2,
          fill: true,
          tension: 0.4,
          pointRadius: sorted.length > 20 ? 0 : 3,
          pointHoverRadius: 5,
        },
        {
          label: '调用次数',
          data: sorted.map(r => r.req_count),
          yAxisID: 'y1',
          borderColor: isDark.value ? '#34d399' : '#10b981',
          backgroundColor: isDark.value ? 'rgba(52,211,153,0.10)' : 'rgba(16,185,129,0.08)',
          borderWidth: 1.5,
          fill: false,
          tension: 0.4,
          pointRadius: 0,
          pointHoverRadius: 4,
          borderDash: [4, 3],
        } as any,
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: {
          position: 'top',
          labels: { usePointStyle: true, pointStyle: 'line', padding: 16, color: colors.textRegular, font: { size: 11 } },
        },
        tooltip: {
          callbacks: {
            label: ctx => {
              if (ctx.datasetIndex === 0) return `花费: ${currencySymbol(cc.value.currency)}${Number(ctx.raw as number).toFixed(2)}`
              return `调用: ${fmtNum(ctx.raw as number)} 次`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { color: colors.grid },
          border: { display: false },
          ticks: { color: colors.textSecondary, font: { size: 10 }, maxRotation: 30 },
        },
        y: {
          type: 'linear', position: 'left',
          grid: { color: colors.grid },
          border: { display: false },
          ticks: { color: colors.textSecondary, font: { size: 10 }, callback: (v: any) => `${currencySymbol(cc.value.currency)}${Number(v).toFixed(2)}` },
        },
        y1: {
          type: 'linear', position: 'right',
          grid: { drawOnChartArea: false },
          border: { display: false },
          ticks: { color: colors.textSecondary, font: { size: 10 }, callback: (v: any) => fmtNum(v as number) },
        },
      },
    },
  } as any)
}

function renderDist() {
  const canvas = distCanvas.value
  if (!canvas || !modelStatsRows.value.length) return
  distChart?.destroy()

  const colors = chartTheme()
  const sorted = [...modelStatsRows.value].sort((a, b) => b.req_count - a.req_count).slice(0, 8)
  const labels = sorted.map(r => r.model || '未知')
  const data = sorted.map(r => r.req_count)
  const total = data.reduce((a, b) => a + b, 0)
  const palette = CHART_PALETTE.map(c => c + 'CC')

  distChart = new Chart(canvas, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{ data, backgroundColor: palette.slice(0, data.length), borderColor: CHART_PALETTE.slice(0, data.length), borderWidth: 1 }],
    },
    plugins: [doughnutCenterPlugin, segmentLabelPlugin],
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout: '62%',
      onClick: (_evt: any, elements: any[]) => {
        if (!elements.length || !distChart) return
        const idx = elements[0].index
        const model = (distChart.data.labels as string[])[idx]
        if (!model) return
        filterModelName.value = model
        loadAll()
      },
      plugins: {
        legend: {
          position: 'right',
          labels: {
            color: colors.textRegular, font: { size: 11 }, padding: 10, usePointStyle: true,
            generateLabels(chart) {
              const ds = chart.data.datasets[0]
              return (chart.data.labels || []).map((label, i) => {
                const value = (ds.data[i] as number) || 0
                const pctV = total ? Math.round(value / total * 100) : 0
                return { text: `${label} (${pctV}%)`, fillStyle: (ds.backgroundColor as string[])[i], strokeStyle: (ds.borderColor as string[])[i], lineWidth: 1, hidden: false, index: i, pointStyle: 'circle' }
              })
            },
          },
        },
        tooltip: {
          callbacks: {
            label: ctx => {
              const v = ctx.raw as number
              return `${ctx.label}: ${fmtNum(v)} 次 (${total ? Math.round(v / total * 100) : 0}%)`
            },
          },
        },
      },
    },
  })
}

function renderCostPerM() {
  const canvas = costPerMCanvas.value
  const rows = costPerMRows.value
  if (!canvas || !rows.length) return
  costPerMChart?.destroy()

  const colors = chartTheme()

  costPerMChart = new Chart(canvas, {
    type: 'bar',
    data: {
      labels: rows.map(r => r.model || '未知'),
      datasets: [{
        label: `每百万 Token 花费 (${currency.value})`,
        data: rows.map(r => toCny(r.cpm)),
        backgroundColor: CHART_PALETTE.map(c => c + 'CC'),
        borderColor: CHART_PALETTE,
        borderWidth: 0,
        borderRadius: 4,
        maxBarThickness: 24,
      }],
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: { label: ctx => `${currencySymbol(cc.value.currency)}${Number(ctx.raw as number).toFixed(4)} / M tokens` },
        },
      },
      scales: {
        x: {
          grid: { color: colors.grid },
          border: { display: false },
          ticks: { color: colors.textSecondary, font: { size: 10 }, callback: (v: any) => `${currencySymbol(cc.value.currency)}${Number(v).toFixed(2)}` },
        },
        y: {
          grid: { display: false },
          border: { display: false },
          ticks: { color: colors.textRegular, font: { size: 11 } },
        },
      },
    },
  })
}

function renderKeyDist() {
  const canvas = keyDistCanvas.value
  if (!canvas || !keyDistRows.value.length) return
  keyDistChart?.destroy()

  const colors = chartTheme()
  const sorted = [...keyDistRows.value].sort((a, b) => b.est_cost_usd - a.est_cost_usd).slice(0, 8)
  const labels = sorted.map(r => r.key_name || maskKey(r.key_id))
  const data = sorted.map(r => toCny(r.est_cost_usd))
  const total = data.reduce((a, b) => a + b, 0)
  const palette = CHART_PALETTE.map(c => c + 'CC')

  keyDistChart = new Chart(canvas, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{ data, backgroundColor: palette.slice(0, data.length), borderColor: CHART_PALETTE.slice(0, data.length), borderWidth: 1 }],
    },
    plugins: [doughnutCenterPlugin, segmentLabelPlugin],
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout: '62%',
      onClick: (_evt: any, elements: any[]) => {
        if (!elements.length || !keyDistChart) return
        const idx = elements[0].index
        const keyId = sorted[idx]?.key_id
        if (!keyId) return
        filterKeyId.value = keyId
        loadAll()
      },
      plugins: {
        legend: {
          position: 'right',
          labels: {
            color: colors.textRegular, font: { size: 11 }, padding: 10, usePointStyle: true,
            generateLabels(chart) {
              const ds = chart.data.datasets[0]
              return (chart.data.labels || []).map((label, i) => {
                const value = (ds.data[i] as number) || 0
                const pctV = total ? Math.round(value / total * 100) : 0
                return { text: `${label} (${pctV}%)`, fillStyle: (ds.backgroundColor as string[])[i], strokeStyle: (ds.borderColor as string[])[i], lineWidth: 1, hidden: false, index: i, pointStyle: 'circle' }
              })
            },
          },
        },
        tooltip: {
          callbacks: {
            label: ctx => {
              const v = ctx.raw as number
              return `${ctx.label}: ${currencySymbol(cc.value.currency)}${v.toFixed(4)} (${total ? Math.round(v / total * 100) : 0}%)`
            },
          },
        },
      },
    },
  })
}

function rerenderCharts() {
  if (dayStatsRows.value.length) renderCostTrend()
  if (analysisOpen.value && modelStatsRows.value.length) {
    renderDist()
    if (costPerMRows.value.length) renderCostPerM()
  }
  if (keyDistOpen.value && keyDistRows.value.length) renderKeyDist()
}

watch(isDark, () => nextTick(() => rerenderCharts()))
watch(currency, () => nextTick(() => rerenderCharts()))

// 懒加载：首次展开「习惯与深度分析」时再渲染该区图表
watch(analysisOpen, (open) => {
  if (!open) return
  nextTick(() => {
    if (modelStatsRows.value.length) renderDist()
    if (costPerMRows.value.length) renderCostPerM()
  })
})

// 懒加载：首次展开「KEY 花费分布」时加载数据并渲染
watch(keyDistOpen, (open) => {
  if (!open) return
  if (!keyDistRows.value.length) {
    loadKeyDist()
  } else {
    nextTick(() => renderKeyDist())
  }
})

// ── Trace ──
async function fetchTrace(requestId: string): Promise<TraceData> {
  const res = await http.get(`/api/v1/my/usage/logs/${requestId}/trace`, { _silent: true } as any)
  return res.data
}

// ── Export ──
const EXPORT_COLUMNS: { key: string; label: string }[] = [
  { key: 'request_id', label: '请求ID' }, { key: 'ts', label: '时间' },
  { key: 'model', label: '模型' }, { key: 'key_id', label: 'Key' },
  { key: 'status', label: '状态' }, { key: 'duration_ms', label: '耗时ms' },
  { key: 'prompt_tokens', label: '输入Token' }, { key: 'completion_tokens', label: '输出Token' },
  { key: 'cached_tokens', label: '缓存Token' }, { key: 'est_cost_usd', label: '成本' },
]
function csvEscape(v: unknown): string {
  const s = v == null ? '' : String(v)
  if (/[",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`
  return s
}
function handleExport() {
  if (!logRows.value.length) return
  const header = EXPORT_COLUMNS.map(c => csvEscape(c.label)).join(',')
  const body = logRows.value.map(row => EXPORT_COLUMNS.map(c => csvEscape((row as any)[c.key])).join(',')).join('\n')
  const blob = new Blob(['\ufeff' + [header, body].join('\n')], { type: 'text/csv;charset=utf-8;' })
  Object.assign(document.createElement('a'), {
    href: URL.createObjectURL(blob),
    download: `my_logs_${currency.value}_${dateRange.value[0]}_${dateRange.value[1]}.csv`,
  }).click()
  ElMessage.success(`已导出 ${logRows.value.length} 条记录`)
}

let themeObserver: MutationObserver | null = null
onUnmounted(() => {
  costTrendChart?.destroy()
  distChart?.destroy()
  costPerMChart?.destroy()
  themeObserver?.disconnect()
})

loadMyKeys()
loadAll()

if (typeof MutationObserver !== 'undefined') {
  themeObserver = new MutationObserver(() => rerenderCharts())
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
}
</script>

<style scoped>
.mu {
  --mu-input:   #3b82f6;
  --mu-output:  #10b981;
  --mu-cache:   #06b6d4;
  --mu-cost:    #f59e0b;
  --mu-reason:  #8b5cf6;
  --mu-danger:  #ef4444;
  --mu-warn:    #f59e0b;

  --mu-surface: var(--el-fill-color-blank);
  --mu-border:  var(--el-border-color-light);
  --mu-muted:   var(--el-text-color-secondary);
  --mu-faint:   var(--el-text-color-placeholder);
  --mu-ink:     var(--el-text-color-regular);
  --mu-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 14px;
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
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1F8A3B));
}
.mu-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.mu-filter, .mu-stat-card, .mu-chart-card, .mu-section, .mu-eff-card {
  background: var(--mu-surface);
  border: 1px solid var(--mu-border);
  border-radius: 12px;
}

/* ── 筛选栏 ── */
.mu-filter {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 16px;
}
.mu-presets { display: flex; align-items: center; gap: 4px; }
.mu-preset-btn {
  padding: 3px 10px; border-radius: 20px;
  border: 1px solid var(--el-border-color);
  background: var(--el-fill-color-light);
  color: var(--el-text-color-regular);
  font-size: 12px; cursor: pointer; transition: all .15s; white-space: nowrap; line-height: 1.6;
}
.mu-preset-btn:hover { border-color: var(--el-color-primary); color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.mu-preset-btn--active { border-color: var(--el-color-primary); color: #fff; background: var(--el-color-primary); font-weight: 500; }
.mu-filter__date { width: 195px; flex-shrink: 0; }
.mu-filter-divider { width: 1px; height: 22px; background: var(--el-border-color); margin: 0 4px; flex-shrink: 0; }
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

/* ── Section 标签 ── */
.mu-sec-label {
  font-size: 14px; font-weight: 700; color: var(--el-text-color-primary);
  display: flex; align-items: center; gap: 6px;
  padding: 2px 0 0;
  margin-top: 4px;
}

/* ── ③ 习惯与深度分析 折叠区 ── */
.mu-analysis-toggle {
  display: flex; align-items: center; gap: 10px; width: 100%;
  padding: 12px 16px; cursor: pointer; text-align: left;
  background: var(--mu-surface); border: 1px solid var(--mu-border); border-radius: 12px;
  transition: border-color .15s ease, background-color .15s ease;
}
.mu-analysis-toggle:hover { border-color: var(--el-color-primary-light-5); background: var(--el-fill-color-extra-light); }
.mu-analysis-toggle:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.mu-analysis-toggle__title { font-size: 14px; font-weight: 700; color: var(--el-text-color-primary); flex-shrink: 0; }
.mu-analysis-toggle__hint { font-size: 12px; color: var(--mu-muted); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mu-analysis-toggle__chev { color: var(--mu-muted); transition: transform .2s ease; flex-shrink: 0; }
.mu-analysis-toggle__chev.is-open { transform: rotate(180deg); }
.mu-analysis-body { display: flex; flex-direction: column; gap: 14px; }

/* ── KPI 卡片 ── */
.mu-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
@media (max-width: 1100px) { .mu-stats { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px)  { .mu-stats { grid-template-columns: 1fr 1fr; } }

.mu-stat-card {
  position: relative; overflow: hidden;
  padding: 16px 18px; display: flex; align-items: flex-start; gap: 14px;
  min-width: 0; transition: box-shadow .25s ease, transform .25s ease, border-color .25s ease;
}
.mu-stat-card::after {
  content: ''; position: absolute; inset: 0 0 auto 0; height: 3px;
  background: var(--accent, var(--el-color-primary)); opacity: .9;
}
.mu-stat-card:hover {
  box-shadow: 0 6px 20px rgba(0,0,0,.08);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}
.mu-stat-card:nth-child(1) { --accent: var(--mu-cost); }
.mu-stat-card:nth-child(2) { --accent: var(--mu-input); }
.mu-stat-card:nth-child(3) { --accent: var(--mu-output); }
.mu-stat-card:nth-child(4) { --accent: var(--mu-cache); }

.mu-stat-card__icon {
  width: 44px; height: 44px; border-radius: 12px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.mu-stat-card__icon--amber { background: color-mix(in srgb, var(--mu-cost) 16%, transparent);  color: var(--mu-cost); }
.mu-stat-card__icon--blue  { background: color-mix(in srgb, var(--mu-input) 14%, transparent); color: var(--mu-input); }
.mu-stat-card__icon--green { background: color-mix(in srgb, var(--mu-output) 14%, transparent); color: var(--mu-output); }
.mu-stat-card__icon--cyan  { background: color-mix(in srgb, var(--mu-cache) 16%, transparent); color: var(--mu-cache); }

.mu-stat-card__body { min-width: 0; flex: 1; }
.mu-stat-card__label { font-size: 12px; color: var(--mu-muted); margin: 0 0 5px; white-space: nowrap; }
.mu-stat-card__value {
  font-size: 24px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 0 0 5px; line-height: 1.15; letter-spacing: -.3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.mu-stat-card__sub { font-size: 12px; color: var(--mu-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }

/* 趋势指示器 */
.mu-trend { font-size: 11px; font-weight: 600; }
.mu-trend--up   { color: var(--mu-danger); }
.mu-trend--down { color: var(--mu-output); }
.mu-muted-text  { font-size: 11px; color: var(--mu-faint); }

/* ── 图表卡片通用 ── */
.mu-chart-card { overflow: hidden; }
.mu-chart-card__header, .mu-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; border-bottom: 1px solid var(--mu-border); flex-wrap: wrap; gap: 8px;
}
.mu-chart-card__title, .mu-section__title {
  font-size: 13px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 7px;
}
.mu-chart-card__title::before, .mu-section__title::before {
  content: ''; width: 6px; height: 6px; border-radius: 999px;
  background: var(--el-color-primary);
}
.mu-chart-card__hint { font-size: 11px; color: var(--mu-muted); }
.mu-chart-card__body { padding: 12px 16px; height: 220px; position: relative; }
.mu-chart-card__body--trend   { height: 200px; }
.mu-chart-card__body--doughnut { height: 260px; }

/* 花费趋势全宽 */
.mu-cost-trend-card {}

/* ── Section 3: 模型偏好 ── */
.mu-model-pref-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 12px;
}
@media (max-width: 900px) { .mu-model-pref-grid { grid-template-columns: 1fr; } }

.mu-key-dist-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 12px;
}
@media (max-width: 900px) { .mu-key-dist-grid { grid-template-columns: 1fr; } }

/* 模型排行列表 */
.mu-rank-list-wrap { padding: 12px 16px; display: flex; flex-direction: column; gap: 8px; min-height: 200px; }
.mu-rank-item { display: flex; align-items: center; gap: 10px; }
.mu-rank-idx {
  width: 22px; height: 22px; border-radius: 50%; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 700;
}
.mu-rank-idx--1 { background: #f59e0b; color: #fff; }
.mu-rank-idx--2 { background: #94a3b8; color: #fff; }
.mu-rank-idx--3 { background: #b45309; color: #fff; }
.mu-rank-idx--rest { background: var(--el-fill-color); color: var(--mu-muted); }
.mu-rank-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.mu-rank-model {
  font-size: 12px; font-weight: 500; color: var(--el-text-color-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.mu-rank-bar-wrap { height: 5px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.mu-rank-bar { height: 100%; border-radius: 999px; transition: width .4s ease; }
.mu-rank-cost { font-size: 12px; font-weight: 600; color: var(--mu-cost); white-space: nowrap; font-variant-numeric: tabular-nums; flex-shrink: 0; }
.mu-rank-empty { font-size: 13px; color: var(--mu-faint); text-align: center; padding: 24px 0; }

/* ── Section 4: 时间习惯 ── */
.mu-time-grid {
  display: grid; grid-template-columns: 3fr 2fr; gap: 12px;
}
@media (max-width: 900px) { .mu-time-grid { grid-template-columns: 1fr; } }

/* 24h 热力图 */
.mu-heatmap-body { padding: 16px; }
.mu-heatmap-grid {
  display: grid; grid-template-columns: repeat(24, 1fr); gap: 4px;
}
.mu-heatmap-cell {
  aspect-ratio: 1; border-radius: 4px; cursor: default;
  min-height: 18px; transition: transform .1s;
}
.mu-heatmap-cell:hover { transform: scale(1.15); }
.mu-heatmap-labels {
  display: grid; grid-template-columns: repeat(24, 1fr); gap: 4px;
  margin-top: 4px;
}
.mu-heatmap-label {
  font-size: 10px; color: var(--mu-faint); text-align: center; line-height: 1.4;
}
.mu-heatmap-empty { text-align: center; font-size: 12px; color: var(--mu-faint); padding: 24px 0 8px; }

/* 工作日 vs 周末 */
.mu-daytype-body {
  display: flex; align-items: stretch; gap: 0;
  padding: 20px 16px; flex: 1;
}
.mu-daytype-col { flex: 1; display: flex; flex-direction: column; align-items: center; gap: 4px; }
.mu-daytype-divider { width: 1px; background: var(--mu-border); margin: 0 16px; flex-shrink: 0; }
.mu-daytype-label { font-size: 14px; font-weight: 700; color: var(--el-text-color-primary); }
.mu-daytype-val { font-size: 28px; font-weight: 700; color: var(--el-text-color-primary); font-variant-numeric: tabular-nums; line-height: 1.2; }
.mu-daytype-sub { font-size: 12px; color: var(--mu-muted); }
.mu-daytype-avg { font-size: 11px; color: var(--el-color-primary); font-weight: 600; }
.mu-daytype-cost { font-size: 13px; font-weight: 600; color: var(--mu-cost); }
.mu-daytype-days { font-size: 11px; color: var(--mu-faint); }
.mu-daytype-bar-row {
  display: flex; padding: 0 16px 14px; gap: 3px; align-items: center;
}
.mu-daytype-bar-wd {
  height: 8px; border-radius: 4px; background: var(--mu-input);
  min-width: 4px; transition: flex .4s ease;
}
.mu-daytype-bar-we {
  height: 8px; border-radius: 4px; background: #8b5cf6;
  min-width: 4px; transition: flex .4s ease;
}

/* ── Section 5: 效率洞察 ── */
.mu-efficiency-grid {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px;
}
@media (max-width: 900px) { .mu-efficiency-grid { grid-template-columns: 1fr 1fr; } }
@media (max-width: 560px)  { .mu-efficiency-grid { grid-template-columns: 1fr; } }

.mu-eff-card {
  padding: 18px; display: flex; align-items: flex-start; gap: 14px;
  transition: box-shadow .2s ease;
}
.mu-eff-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,.06); }
.mu-eff-card__icon { font-size: 28px; flex-shrink: 0; line-height: 1; margin-top: 2px; }
.mu-eff-card__body { flex: 1; min-width: 0; }
.mu-eff-card__label { font-size: 12px; color: var(--mu-muted); margin-bottom: 4px; }
.mu-eff-card__value { font-size: 26px; font-weight: 700; color: var(--el-text-color-primary); line-height: 1.2; font-variant-numeric: tabular-nums; }
.mu-eff-unit { font-size: 16px; font-weight: 600; margin-left: 1px; }
.mu-eff-card__desc { font-size: 11px; color: var(--mu-faint); margin-top: 4px; line-height: 1.5; }

/* 缓存命中率进度条 */
.mu-eff-cache-bar {
  height: 5px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; margin: 6px 0 4px;
}
.mu-eff-cache-fill {
  height: 100%; border-radius: 999px;
  background: linear-gradient(90deg, #10b981, #06b6d4);
  transition: width .5s ease;
}

/* ── Section 6: 模型明细 (collapsible) ── */
.mu-section { overflow: hidden; }
.mu-section-toggle { cursor: pointer; user-select: none; }
.mu-section-toggle:hover { background: var(--el-fill-color-extra-light); }
.mu-section__header { padding: 12px 16px; border-bottom: 1px solid var(--mu-border); }
.mu-toggle-row { display: flex; align-items: center; gap: 12px; }
.mu-toggle-btn { font-size: 12px; color: var(--mu-muted); white-space: nowrap; }
.mu-section__count { font-size: 12px; color: var(--mu-muted); }

/* ── 表格 ── */
.mu-table-wrap { overflow-x: auto; }
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
.mu-table-wrap :deep(.el-table__row:hover > td.el-table__cell) {
  background: color-mix(in srgb, var(--el-color-primary) 8%, var(--mu-surface)) !important;
}
.mu-table-wrap :deep(.el-table .el-table-fixed-column--left.is-last-column.el-table__cell),
.mu-table-wrap :deep(.el-table__fixed) { box-shadow: 6px 0 12px -6px rgba(0,0,0,.12); }

/* ── 空态 ── */
.mu-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.mu-empty p { font-size: 13px; color: var(--mu-faint); margin: 0; }

/* ── 分页 ── */
.mu-pagination { padding: 12px 16px; border-top: 1px solid var(--mu-border); display: flex; justify-content: flex-end; }

/* ── 模型名 ── */
.mu-model-name { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }
.mu-model-name--muted { color: var(--mu-muted); font-weight: 400; }
.mu-model-cell { display: flex; flex-direction: column; gap: 2px; }
.mu-model-routed { display: flex; align-items: center; gap: 3px; }
.mu-arrow { color: var(--mu-faint); font-size: 10px; }
.mu-col--wrap { white-space: normal !important; word-break: break-all !important; overflow: visible !important; text-overflow: clip !important; }
.mu-dash { color: var(--mu-faint); }
.mu-name { font-size: 12px; color: var(--el-text-color-regular); font-weight: 500; }

/* ── Token badge ── */
.mu-token-row { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.mu-token-item {
  display: inline-flex; align-items: center; gap: 3px; font-size: 11px; font-weight: 600;
  padding: 1px 7px; border-radius: 999px;
}
.mu-token-item--in    { color: var(--mu-input);  background: color-mix(in srgb, var(--mu-input) 12%, transparent); }
.mu-token-item--out   { color: var(--mu-output); background: color-mix(in srgb, var(--mu-output) 12%, transparent); }
.mu-token-item--cache { color: var(--mu-cache);  background: color-mix(in srgb, var(--mu-cache) 14%, transparent); }
.mu-token-item--reason { color: var(--mu-reason); background: color-mix(in srgb, var(--mu-reason) 12%, transparent); }
.mu-token-icon { font-size: 10px; }

/* ── 格子工具 ── */
.mu-time { font-size: 12px; color: var(--mu-muted); white-space: nowrap; font-family: var(--mu-mono); }
.mu-duration { font-size: 12px; color: var(--mu-ink); font-variant-numeric: tabular-nums; }
.mu-cost { font-family: var(--mu-mono); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.mu-flags { display: flex; flex-wrap: wrap; gap: 3px; justify-content: center; }
.mu-pct { font-size: 10px; color: var(--mu-cache); margin-left: 4px; }
.mu-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; white-space: nowrap; }
.mu-source { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.mu-ua { display: block; font-size: 11px; color: var(--mu-muted); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.mu-ip { font-family: var(--mu-mono); font-size: 10px; color: var(--mu-faint); }

/* ── 迷你进度条 ── */
.mu-bar-cell { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.mu-minibar { width: 100%; height: 4px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.mu-minibar__fill { height: 100%; border-radius: 999px; transition: width .4s ease; }
.mu-minibar__fill--blue  { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-input) 45%, transparent), var(--mu-input)); }
.mu-minibar__fill--green { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-output) 45%, transparent), var(--mu-output)); }
.mu-minibar__fill--red   { background: linear-gradient(90deg, color-mix(in srgb, var(--mu-cost) 45%, transparent), var(--mu-cost)); }

/* ── 信息按钮 ── */
.mu-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; border-radius: 999px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--mu-faint);
  cursor: help; transition: background .15s ease, color .15s ease;
}
.mu-info:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }
.mu-info .el-icon { font-size: 11px; }

/* ── 颜色工具 ── */
.c-blue   { color: var(--mu-input); }
.c-green  { color: var(--mu-output); }
.c-cyan   { color: var(--mu-cache); }
.c-orange { color: var(--mu-warn); }
.c-amber  { color: var(--mu-cost); }
.c-purple { color: var(--mu-reason); }
.c-red    { color: var(--mu-danger); }
.fw6      { font-weight: 600; }
.mu-tag--effort { font-size: 10px; }

/* ── Responsive ── */
@media (max-width: 768px) {
  .mu-header { flex-direction: column; align-items: flex-start; gap: 10px; }
  .mu-header__right { width: 100%; justify-content: flex-start; }
  .mu-filter { flex-wrap: wrap; gap: 8px; }
  .mu-presets { flex-wrap: wrap; gap: 4px; }
  .mu-filter__date,
  .mu-filter__chip,
  .mu-filter__chip--key,
  .mu-filter__status { width: 100% !important; }
  .mu-filter__clear { width: 100%; }
  .mu-filter-divider { display: none; }
  .mu-stats { grid-template-columns: repeat(2, 1fr); }
  .mu-model-pref-grid,
  .mu-time-grid { grid-template-columns: 1fr; }
  .mu-efficiency-grid { grid-template-columns: 1fr; }
  .mu-table-wrap { overflow-x: auto; }
  .mu-table-wrap :deep(.el-table) { min-width: 700px; }
  .mu-section__header { flex-direction: column; align-items: flex-start; gap: 8px; }
  .mu-pagination { flex-direction: column; align-items: center; gap: 8px; }
  .mu-meta-grid { grid-template-columns: 1fr; }
  .mu-trace-drawer.el-drawer { width: 100% !important; }
  .mu-trace-drawer .mu-msg-content { font-size: 11px !important; padding: 8px !important; }
  .mu-trace-drawer .mu-json-view pre { font-size: 11px !important; white-space: pre; overflow-x: auto; }
  .mu-trace-drawer .el-tabs__nav-wrap { overflow-x: auto; }
  .mu-trace-drawer .el-tabs__nav { flex-wrap: nowrap; display: inline-flex; }
  .mu-timeline { overflow-x: auto; white-space: nowrap; }
  .mu-tl-node { min-width: 60px; }
}

@media (max-width: 480px) {
  .mu-stats { grid-template-columns: 1fr; }
  .mu-stat-card__value { font-size: 20px; }
  .mu-preset-btn { font-size: 11px; padding: 3px 8px; }
  .mu-heatmap-cell { min-height: 14px; }
}

/* ── 尊重用户的减少动态偏好 ── */
@media (prefers-reduced-motion: reduce) {
  .mu *, .mu *::before, .mu *::after {
    transition-duration: .01ms !important;
    animation-duration: .01ms !important;
  }
  .mu-stat-card:hover { transform: none; }
  .mu-heatmap-cell:hover { transform: none; }
}
</style>

<!-- Tooltip + Trace Drawer 全局样式（非 scoped） -->
<style>
.mu-tt-popper.el-popper {
  background: var(--el-bg-color-overlay, #1f2937) !important;
  border: 1px solid var(--el-border-color, #374151) !important;
  border-radius: 10px !important;
  box-shadow: 0 12px 32px rgba(0,0,0,.35) !important;
  padding: 10px 12px !important;
}
.mu-tt-popper.el-popper .el-popper__arrow::before {
  background: var(--el-bg-color-overlay, #1f2937) !important;
  border: 1px solid var(--el-border-color, #374151) !important;
}
.mu-tt { min-width: 172px; font-size: 12px; line-height: 1.7; font-variant-numeric: tabular-nums; }
.mu-tt__title { font-weight: 700; font-size: 12px; margin-bottom: 7px; color: var(--el-text-color-primary, #e5e7eb); border-bottom: 1px solid var(--el-border-color, #374151); padding-bottom: 5px; }
.mu-tt__row { display: flex; justify-content: space-between; gap: 18px; align-items: center; color: var(--el-text-color-regular, #b0b8c4); }
.mu-tt__sub { color: var(--el-text-color-secondary, #9ca3af); padding-left: 2px; }
.mu-tt__group { display: flex; align-items: center; gap: 4px; margin: 2px 0 1px; font-size: 11px; font-weight: 700; letter-spacing: .3px; color: var(--el-color-primary, #38bdf8); }
.mu-tt__group-ic { font-size: 12px; }
.mu-tt__row b { color: var(--el-text-color-primary, #f9fafb); font-weight: 600; }
.mu-tt__row b.c-cyan   { color: #38bdf8; }
.mu-tt__row b.c-amber  { color: #fbbf24; }
.mu-tt__row b.c-purple { color: #a78bfa; }
.mu-tt__row b.c-blue   { color: #5eead4; }
.mu-tt__row b.c-green  { color: #4ade80; }
.mu-tt__sep { border-top: 1px solid var(--el-border-color, #374151); margin: 6px 0; }
.mu-ua-popper.el-popper { max-width: 440px; line-height: 1.5; word-break: break-all; }

/* ═══════════ Trace Drawer ═══════════ */
.mu-trace-header { display: flex; flex-direction: column; gap: 2px; }
.mu-trace-title  { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.mu-trace-id     { font-size: 11px; color: var(--el-text-color-secondary); font-family: 'SFMono-Regular', monospace; word-break: break-all; }
.mu-trace-loading { display: flex; justify-content: center; align-items: center; padding: 60px 0; color: var(--el-color-primary); }
.mu-trace-body    { display: flex; flex-direction: column; gap: 20px; }
.mu-trace-empty   { text-align: center; padding: 40px 0; color: var(--el-text-color-secondary); font-size: 13px; }
.mu-trace-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.mu-trace-time { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 4px; }
.mu-trace-dur  { font-size: 12px; color: var(--el-color-primary); font-weight: 600; }

.mu-timeline { display: flex; align-items: flex-start; gap: 0; background: var(--el-fill-color-light); border-radius: 10px; padding: 16px 20px; overflow-x: auto; }
.mu-tl-node { display: flex; flex-direction: column; align-items: center; gap: 4px; position: relative; flex: 1; min-width: 72px; }
.mu-tl-node + .mu-tl-node::before { content: ''; position: absolute; left: -50%; top: 13px; width: 100%; height: 2px; background: var(--el-border-color); z-index: 0; }
.mu-tl-node.ok    + .mu-tl-node::before { background: var(--el-color-success-light-5); }
.mu-tl-node.error + .mu-tl-node::before { background: var(--el-color-danger-light-5); }
.mu-tl-dot {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--el-fill-color); border: 2px solid var(--el-border-color);
  font-size: 14px; position: relative; z-index: 1;
}
.mu-tl-node.ok    .mu-tl-dot { background: color-mix(in srgb, var(--el-color-success) 12%, transparent); border-color: var(--el-color-success); color: var(--el-color-success); }
.mu-tl-node.error .mu-tl-dot { background: color-mix(in srgb, var(--el-color-danger)  12%, transparent); border-color: var(--el-color-danger);  color: var(--el-color-danger); }
.mu-tl-label { font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600; }
.mu-tl-measured { font-size: 10px; color: var(--el-color-primary); margin-left: 3px; }
.mu-tl-ms    { font-size: 11px; color: var(--el-text-color-regular); font-family: 'SFMono-Regular', monospace; }
.mu-tl-warn  { color: var(--el-color-warning); font-size: 12px; vertical-align: middle; }

.mu-trace-tabs { margin-top: -8px; }
.mu-msgs { display: flex; flex-direction: column; gap: 12px; }
.mu-msg  { border-radius: 8px; overflow: hidden; border: 1px solid var(--el-border-color-light); }
.mu-msg-role {
  display: block; padding: 5px 12px; font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .05em;
  background: var(--el-fill-color-light); color: var(--el-text-color-secondary);
}
.mu-msg--system    .mu-msg-role { background: var(--el-color-warning-light-9); color: var(--el-color-warning-dark-2); }
.mu-msg--user      .mu-msg-role { background: var(--el-color-primary-light-9); color: var(--el-color-primary-dark-2); }
.mu-msg--assistant .mu-msg-role { background: var(--el-color-success-light-9); color: var(--el-color-success-dark-2); }
.mu-msg-content {
  margin: 0; padding: 12px; font-size: 12.5px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-word;
  font-family: 'SFMono-Regular', monospace; background: var(--el-bg-color); max-height: 400px; overflow-y: auto;
}
.mu-json-view { background: var(--el-fill-color-light); border-radius: 8px; padding: 14px; max-height: 500px; overflow-y: auto; }
.mu-json-view pre { margin: 0; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all; font-family: 'SFMono-Regular', monospace; }
.mu-error-block { background: var(--el-color-danger-light-9); border: 1px solid var(--el-color-danger-light-5); border-radius: 8px; padding: 16px; }
.mu-error-code  { font-size: 11px; font-weight: 700; color: var(--el-color-danger); text-transform: uppercase; margin-bottom: 8px; }
.mu-error-msg   { margin: 0; font-size: 12.5px; color: var(--el-color-danger-dark-2); white-space: pre-wrap; word-break: break-word; }

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
code { font-family: 'SFMono-Regular', monospace; font-size: 12px; color: var(--el-text-color-primary); }

.c-cyan   { color: #06b6d4; }
.c-amber  { color: #f59e0b; }
.c-purple { color: #a78bfa; }
.c-blue   { color: #60a5fa; }
.c-green  { color: #4ade80; }
</style>
