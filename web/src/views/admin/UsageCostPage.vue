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
      <el-date-picker
        v-model="dateRange"
        type="daterange"
        range-separator="—"
        start-placeholder="开始日期"
        end-placeholder="结束日期"
        value-format="YYYY-MM-DD"
        format="YYYY/MM/DD"
        :shortcuts="dateShortcuts"
        class="up-filter__date"
        @change="loadAll"
      />
      <el-input
        v-model="filterModel"
        placeholder="筛选模型"
        clearable size="small"
        class="up-filter__model"
        @change="loadAll"
        @clear="loadAll"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
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
          <p class="up-stat-card__value c-green">${{ (statsTotal?.est_cost_usd ?? 0).toFixed(4) }}</p>
          <p class="up-stat-card__sub">
            均次 <span class="c-orange">${{ avgCostPerReq }}</span>
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
          <el-radio-button value="day">按天</el-radio-button>
          <el-radio-button value="model">按模型</el-radio-button>
          <el-radio-button value="dept">按部门</el-radio-button>
          <el-radio-button value="key">按 Key</el-radio-button>
        </el-radio-group>
      </div>
      <div class="up-table-wrap" v-loading="statsLoading">
        <el-table :data="statsRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%"
          :default-sort="{ prop: 'est_cost_usd', order: 'descending' }">
          <el-table-column v-if="groupBy==='day'" prop="stat_date" label="日期" min-width="100" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-if="groupBy==='model'" prop="model" label="模型" min-width="160" sortable>
            <template #default="{ row }"><span class="up-model-name">{{ row.model || '—' }}</span></template>
          </el-table-column>
          <el-table-column v-if="groupBy==='dept'" prop="dept_id" label="部门" min-width="130" sortable>
            <template #default="{ row }"><span class="up-mono">{{ row.dept_id || '未知' }}</span></template>
          </el-table-column>
          <el-table-column v-if="groupBy==='key'" prop="key_id" label="Key" min-width="160" sortable>
            <template #default="{ row }"><span class="up-mono up-mono--sm">{{ maskKey(row.key_id) }}</span></template>
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
                <span :class="costColor(row.est_cost_usd)">${{ Number(row.est_cost_usd||0).toFixed(4) }}</span>
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
          <!-- Key（脱敏，不用 code 标签） -->
          <el-table-column prop="key_id" label="Key" min-width="90" fixed="left">
            <template #default="{ row }"><span class="up-mono up-mono--sm">{{ maskKey(row.key_id) }}</span></template>
          </el-table-column>
          <!-- 用户 -->
          <el-table-column prop="user_name" label="用户" min-width="90" fixed="left">
            <template #default="{ row }">
              <span v-if="row.user_name" class="up-name">{{ row.user_name }}</span>
              <span v-else class="up-mono up-mono--sm">{{ maskKey(row.user_id) }}</span>
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
                <div v-if="row.routed_model && row.routed_model !== row.model" class="up-model-routed">
                  <span class="up-arrow">↳</span>
                  <span class="up-model-name up-model-name--muted">{{ row.routed_model }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <!-- 组名 -->
          <el-table-column prop="group_name" label="模型组" min-width="110">
            <template #default="{ row }">
              <el-tag v-if="row.group_name" type="info" size="small" effect="plain" class="up-tag">{{ row.group_name }}</el-tag>
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
          <el-table-column prop="est_cost_usd" label="费用" min-width="120" align="right" sortable>
            <template #default="{ row }">
              <div class="up-cost-cell">
                <span class="up-cost" :class="logCostColor(row.est_cost_usd)">${{ Number(row.est_cost_usd||0).toFixed(4) }}</span>
                <el-tooltip placement="left" :show-after="120" popper-class="up-tt-popper">
                  <template #content>
                    <div class="up-tt">
                      <div class="up-tt__title">成本明细 (USD)</div>
                      <!-- 单价（每 1M tokens）-->
                      <template v-if="row.input_price_snapshot != null || row.output_price_snapshot != null">
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
                      <div class="up-tt__row"><span>合计</span><b class="c-green">${{ row.est_cost_usd.toFixed(6) }}</b></div>
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

  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick } from 'vue'
import {
  Refresh, Download, Search, Histogram, Money, DataLine, Coin,
  MagicStick, Top, Bottom, Finished, List, InfoFilled
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
  stat_date?: string; model?: string; dept_id?: string; key_id?: string
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number
}
interface StatsTotal { req_count: number; success_count?: number; prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number }
interface LogRow {
  request_id: string; ts: string; key_id: string
  user_id: string; user_name?: string; dept_id: string; dept_name?: string; studio_name?: string
  model: string; routed_model: string; group_name: string; provider: string
  prompt_tokens: number; cached_tokens: number; cache_write_tokens: number
  completion_tokens: number; reasoning_tokens: number; total_tokens: number
  status: number; upstream_status: number; duration_ms: number; first_token_ms?: number | null
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

// ── Filters ──
const filterModel = ref('')
const groupBy = ref('day')

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

// ── Table bar helpers ──
const maxPrompt     = computed(() => Math.max(...statsRows.value.map(r => r.prompt_tokens || 0), 1))
const maxCompletion = computed(() => Math.max(...statsRows.value.map(r => r.completion_tokens || 0), 1))
const maxCost       = computed(() => Math.max(...statsRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const pct = (v: number, max: number) => Math.min(Math.round(v / max * 100), 100)
const costColor = (v: number) => v > maxCost.value * 0.7 ? 'c-red fw6' : v > maxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'
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
    if (filterModel.value) params.model = filterModel.value
    const r = await http.get('/api/v1/usage', { params })
    statsRows.value = r.data.rows || []
    statsTotal.value = r.data.total || null
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
    if (filterModel.value) params.model = filterModel.value
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
    if (groupBy.value === 'day') return fmtDate(r.stat_date)
    if (groupBy.value === 'model') return r.model || '未知'
    if (groupBy.value === 'dept') return r.dept_id || '未知'
    return maskKey(r.key_id)
  })

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
        legend: { position: 'top', labels: { usePointStyle: true, pointStyle: 'circle', padding: 12, color: '#64748B', font: { size: 11 } } },
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
    const key = groupBy.value === 'model' ? (r.model || '未知') : (groupBy.value === 'dept' ? (r.dept_id || '未知') : (groupBy.value === 'key' ? maskKey(r.key_id) : fmtDate(r.stat_date)))
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
.up-filter { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 12px 16px; }
.up-filter__date { flex: 1; min-width: 220px; max-width: 300px; }
.up-filter__model { width: 170px; flex-shrink: 0; }

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
.up-stat-card__sub  { font-size: 12px; color: var(--up-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; }
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

.up-tag { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
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
.up-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; }

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
</style>