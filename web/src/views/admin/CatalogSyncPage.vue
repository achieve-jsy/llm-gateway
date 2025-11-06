<template>
  <div class="catalog-sync-page">
    <!-- 顶部：同步元信息 + 操作按钮 -->
    <div class="page-header">
      <div>
        <h2 class="page-title">Catalog 同步</h2>
        <p class="page-subtitle">
          目录已收录
          <strong>{{ catalogTotal }}</strong>
          个上游模型
          <span v-if="syncedAtText" class="sync-meta">
            · 最近同步于 <span class="sync-time">{{ syncedAtText }}</span>
          </span>
          ·
          <strong>{{ diffRows.length }}</strong>
          个本地模型与目录存在差异（当前筛选 {{ filteredRows.length }} 条）
        </p>
      </div>
      <div class="header-actions">
        <el-button
          type="primary"
          :loading="autoBinding"
          @click="handleAutoBind"
          title="按 name / upstream_model_id 严格同名将本地模型自动绑定到目录条目；同名多个候选只列出候选，不会盲绑。"
          >智能绑定</el-button
        >
        <el-button :loading="refreshing" @click="handleManualRefresh">
          手动刷新目录
        </el-button>
      </div>
    </div>

    <!-- 空状态：完全没有 diff -->
    <el-empty
      v-if="!loading && !diffRows.length"
      description=""
      class="empty-state"
    >
      <template #description>
        <div class="empty-desc">
          <p class="empty-title">当前没有需要处理的差异</p>
          <p class="empty-hint">
            目录中已有 <strong>{{ catalogTotal }}</strong> 个上游模型条目。
            如果还没有本地模型绑定到目录，可点上方「智能绑定」一键尝试匹配，
            或到「<router-link to="/admin/models">模型管理</router-link>」
            手工选择 catalog 条目。
          </p>
        </div>
      </template>
    </el-empty>

    <!-- 筛选区 -->
    <div v-else class="filter-bar">
      <el-input
        v-model="searchKeyword"
        placeholder="搜索本地模型名或目录上游 ID"
        clearable
        :prefix-icon="Search"
        class="search-input"
      />
      <el-select
        v-model="statusFilters"
        placeholder="按状态筛选"
        multiple
        collapse-tags
        collapse-tags-tooltip
        clearable
        class="status-select"
      >
        <el-option label="已脱离目录（override）" value="overridden" />
        <el-option label="已废弃（deprecation）" value="deprecated" />
        <el-option label="涨价（catalog &gt; local）" value="price_up" />
        <el-option label="降价（catalog &lt; local）" value="price_down" />
        <el-option label="本地价未设（local=0）" value="price_unset" />
      </el-select>
      <div class="threshold-control">
        <span class="threshold-label">|Δ| ≥</span>
        <el-input-number
          v-model="minAbsDelta"
          :min="0"
          :max="100"
          :step="5"
          size="default"
          controls-position="right"
        />
        <span class="threshold-label">%</span>
      </div>
      <el-select v-model="sortKey" class="sort-select">
        <el-option label="按 |Δ| 降序（变化大的先看）" value="delta_desc" />
        <el-option label="按 |Δ| 升序" value="delta_asc" />
        <el-option label="按本地模型名（A→Z）" value="name_asc" />
        <el-option label="按目录上游 ID（A→Z）" value="catalog_asc" />
        <el-option label="按目录价（高→低）" value="price_desc" />
      </el-select>
      <el-button @click="resetFilters" :disabled="!hasFilter">重置</el-button>
      <el-button @click="exportCSV" :icon="Download">导出 CSV</el-button>
    </div>

    <!-- 差异表格 -->
    <el-table
      v-if="diffRows.length"
      v-loading="loading"
      :data="pagedRows"
      border
      stripe
      row-key="model_id"
      :empty-text="filteredRows.length ? '当前页无数据' : '没有匹配筛选条件的差异行'"
      @selection-change="handleSelectionChange"
    >
      <el-table-column
        type="selection"
        width="45"
        :reserve-selection="true"
      />
      <el-table-column label="本地模型" prop="model_name" min-width="240">
        <template #default="{ row }">
          <span class="mono">{{ row.model_name }}</span>
          <el-tag
            v-if="row.is_overridden"
            type="danger"
            size="small"
            class="badge-override"
            >已脱离目录</el-tag
          >
          <el-tag
            v-if="isPriceUnset(row)"
            type="warning"
            size="small"
            class="badge-override"
            >价格未设</el-tag
          >
          <el-tag
            v-if="row.catalog_deprecation_date"
            type="warning"
            size="small"
            class="badge-deprecation"
            >⏰ {{ formatDate(row.catalog_deprecation_date) }} 废弃</el-tag
          >
        </template>
      </el-table-column>

      <el-table-column label="目录上游" min-width="220">
        <template #default="{ row }">
          <span class="mono dim">{{ row.catalog_source_model_id }}</span>
        </template>
      </el-table-column>

      <el-table-column label="本地价 (in/out, $/M tok)" width="200" align="right">
        <template #default="{ row }">
          {{ formatPrice(row.local_input_price) }} /
          {{ formatPrice(row.local_output_price) }}
        </template>
      </el-table-column>

      <el-table-column label="目录价 (in/out, $/M tok)" width="200" align="right">
        <template #default="{ row }">
          {{ formatPrice(row.catalog_input_price) }} /
          {{ formatPrice(row.catalog_output_price) }}
        </template>
      </el-table-column>

      <el-table-column label="变化%" width="180" align="center">
        <template #default="{ row }">
          <template v-if="isPriceUnset(row)">
            <span class="delta-flat">未设 → 跟随</span>
          </template>
          <template v-else>
            <span :class="deltaClass(row.input_delta_pct)">
              {{ deltaArrow(row.input_delta_pct) }}
              {{ formatPct(row.input_delta_pct) }}
            </span>
            /
            <span :class="deltaClass(row.output_delta_pct)">
              {{ deltaArrow(row.output_delta_pct) }}
              {{ formatPct(row.output_delta_pct) }}
            </span>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <!-- 底部：分页 + 操作 -->
    <div v-if="diffRows.length" class="footer-bar">
      <el-pagination
        v-if="filteredRows.length > pageSize"
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="filteredRows.length"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        background
      />
      <div class="footer-actions">
        <el-button @click="selectAllFiltered" :disabled="!filteredRows.length"
          >全选当前筛选 ({{ filteredRows.length }})</el-button
        >
        <el-button @click="clearSelection" :disabled="!selectedIds.length"
          >清空选择</el-button
        >
        <el-button type="primary" :loading="applying" @click="handleApply">
          应用选中项{{ selectedIds.length ? `(${selectedIds.length})` : '' }}
        </el-button>
      </div>
    </div>

    <!-- 智能绑定结果弹窗 -->
    <el-dialog
      v-model="autoBindDialogVisible"
      title="智能绑定结果"
      width="640px"
    >
      <div v-if="autoBindResult" class="auto-bind-summary">
        <p>
          <el-tag type="success" effect="dark"
            >已绑定 {{ autoBindResult.matched.length }} 条</el-tag
          >
          <el-tag type="warning" effect="dark" class="ml-8"
            >多候选待选 {{ autoBindResult.ambiguous.length }} 条</el-tag
          >
          <el-tag class="ml-8">未匹配 {{ autoBindResult.unmatched }} 条</el-tag>
        </p>

        <template v-if="autoBindResult.matched.length">
          <h4>已自动绑定</h4>
          <ul class="bind-list">
            <li
              v-for="m in autoBindResult.matched.slice(0, 30)"
              :key="m.model_id"
            >
              <span class="mono">{{ m.model_name }}</span>
              <span class="arrow">→</span>
              <span class="mono dim">{{ m.catalog_source_model_id }}</span>
            </li>
          </ul>
          <p v-if="autoBindResult.matched.length > 30" class="ellipsis">
            …等共 {{ autoBindResult.matched.length }} 条
          </p>
        </template>

        <template v-if="autoBindResult.ambiguous.length">
          <h4>多候选（请逐条选择目录条目，再点「保存已选」批量入库）</h4>
          <table class="ambiguous-table" data-testid="ambiguous-table">
            <thead>
              <tr>
                <th>本地模型</th>
                <th>目录条目</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="a in autoBindResult.ambiguous"
                :key="a.model_id"
                data-testid="ambiguous-row"
              >
                <td class="mono">{{ a.model_name }}</td>
                <td>
                  <el-select
                    v-model="ambiguousSelections[a.model_id]"
                    placeholder="─请选择─"
                    clearable
                    size="small"
                    style="width: 100%"
                    :data-testid="`ambiguous-select-${a.model_id}`"
                  >
                    <el-option
                      v-for="c in a.candidates"
                      :key="c.catalog_id"
                      :label="`${c.provider || '-'} / ${c.catalog_source_model_id}`"
                      :value="c.catalog_id"
                    />
                  </el-select>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="ambiguous-footer">
            <el-checkbox
              v-model="ambiguousResetFields"
              data-testid="ambiguous-reset-fields"
            >
              同时把价格、上下文长度、最大输出、上游模型 ID 重置为目录的当前值
            </el-checkbox>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="autoBindDialogVisible = false">关闭</el-button>
        <el-button
          v-if="autoBindResult && autoBindResult.ambiguous.length"
          type="primary"
          :loading="batchBinding"
          :disabled="ambiguousSelectedCount === 0"
          data-testid="ambiguous-save-btn"
          @click="handleAmbiguousSave"
        >
          保存已选 {{ ambiguousSelectedCount }} 条
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Download } from '@element-plus/icons-vue'
import {
  getCatalogDiff,
  applyCatalogDiff,
  triggerCatalogSync,
  searchCatalog,
  autoBindCatalog,
  batchBindModelCatalog,
  getCatalogBindingStats
} from '@/services/control-plane'
import type { PriceDiffEntry, AutoBindResult, CatalogBindingStats } from '@/types/catalog'

// ===================== 数据 / 状态 =====================
const diffRows = ref<PriceDiffEntry[]>([])
const refreshing = ref(false)
const applying = ref(false)
const loading = ref(false)
const catalogTotal = ref(0)
const lastSyncedAt = ref<string>('')
const autoBinding = ref(false)
const autoBindDialogVisible = ref(false)
const autoBindResult = ref<AutoBindResult | null>(null)

// 目录绑定状态：catalog 总数 + 本地 model 已绑定 / 未绑定计数。
// 空状态区域用它来精准判定"diff 为空到底意味着啥"。
const bindingStats = ref<CatalogBindingStats | null>(null)
// CAT-2: ambiguous 段每个 model_id → 选中的 catalog_id（admin 显式选定）
const ambiguousSelections = ref<Record<string, string>>({})
const ambiguousResetFields = ref(false)
const batchBinding = ref(false)

const ambiguousSelectedCount = computed(() => {
  return Object.values(ambiguousSelections.value).filter((v) => !!v).length
})

// ===================== 筛选 / 排序 / 分页 =====================
type StatusFilter =
  | 'overridden'
  | 'deprecated'
  | 'price_up'
  | 'price_down'
  | 'price_unset'
type SortKey =
  | 'delta_desc'
  | 'delta_asc'
  | 'name_asc'
  | 'catalog_asc'
  | 'price_desc'

const searchKeyword = ref('')
const statusFilters = ref<StatusFilter[]>([])
const minAbsDelta = ref(0)
const sortKey = ref<SortKey>('delta_desc')
const currentPage = ref(1)
const pageSize = ref(20)

// 跨页保留的选中行（map<id, row>），表格本身的 reserve-selection 也会保留，
// 但我们也要在程序化全选/清空时手动管理。
const selectedIds = ref<string[]>([])

const hasFilter = computed(
  () =>
    searchKeyword.value !== '' ||
    statusFilters.value.length > 0 ||
    minAbsDelta.value > 0 ||
    sortKey.value !== 'delta_desc'
)

const syncedAtText = computed(() =>
  lastSyncedAt.value ? new Date(lastSyncedAt.value).toLocaleString() : ''
)

// ===================== 过滤 + 排序 + 分页 =====================
function isPriceUnset(row: PriceDiffEntry): boolean {
  // 本地 in/out 都为 0 但目录至少一个 > 0 → 视为"未设置，建议跟随目录"
  return (
    Number(row.local_input_price) === 0 &&
    Number(row.local_output_price) === 0 &&
    (Number(row.catalog_input_price) > 0 ||
      Number(row.catalog_output_price) > 0)
  )
}

function maxAbsDelta(row: PriceDiffEntry): number {
  return Math.max(
    Math.abs(Number(row.input_delta_pct) || 0),
    Math.abs(Number(row.output_delta_pct) || 0)
  )
}

const filteredRows = computed<PriceDiffEntry[]>(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  const flags = new Set(statusFilters.value)
  const threshold = minAbsDelta.value

  return diffRows.value.filter((r) => {
    if (kw) {
      const a = (r.model_name || '').toLowerCase()
      const b = (r.catalog_source_model_id || '').toLowerCase()
      if (!a.includes(kw) && !b.includes(kw)) return false
    }
    if (flags.size > 0) {
      const matchAny =
        (flags.has('overridden') && r.is_overridden) ||
        (flags.has('deprecated') && r.catalog_deprecation_date) ||
        (flags.has('price_unset') && isPriceUnset(r)) ||
        (flags.has('price_up') &&
          (Number(r.input_delta_pct) > 0 ||
            Number(r.output_delta_pct) > 0)) ||
        (flags.has('price_down') &&
          (Number(r.input_delta_pct) < 0 ||
            Number(r.output_delta_pct) < 0))
      if (!matchAny) return false
    }
    // 价格未设的不参与阈值过滤（它的 delta 计算后是 0，但应该出现在 price_unset 这条规则里）
    if (threshold > 0 && !isPriceUnset(r) && maxAbsDelta(r) < threshold) {
      return false
    }
    return true
  })
})

const sortedRows = computed<PriceDiffEntry[]>(() => {
  const arr = filteredRows.value.slice()
  switch (sortKey.value) {
    case 'delta_desc':
      arr.sort((a, b) => maxAbsDelta(b) - maxAbsDelta(a))
      break
    case 'delta_asc':
      arr.sort((a, b) => maxAbsDelta(a) - maxAbsDelta(b))
      break
    case 'name_asc':
      arr.sort((a, b) => (a.model_name || '').localeCompare(b.model_name || ''))
      break
    case 'catalog_asc':
      arr.sort((a, b) =>
        (a.catalog_source_model_id || '').localeCompare(
          b.catalog_source_model_id || ''
        )
      )
      break
    case 'price_desc':
      arr.sort(
        (a, b) =>
          Number(b.catalog_input_price || 0) -
          Number(a.catalog_input_price || 0)
      )
      break
  }
  return arr
})

const pagedRows = computed<PriceDiffEntry[]>(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return sortedRows.value.slice(start, start + pageSize.value)
})

// 改任何筛选条件都回到第 1 页
watch([searchKeyword, statusFilters, minAbsDelta, sortKey], () => {
  currentPage.value = 1
})

// ===================== 工具函数 =====================
function formatDate(iso: string): string {
  return iso.slice(0, 10)
}

function formatPrice(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(Number(v))) return '—'
  const n = Number(v)
  if (n === 0) return '$0'
  return n < 0.1 ? `$${n.toFixed(4)}` : `$${n.toFixed(2)}`
}

function formatPct(pct: number | null | undefined): string {
  if (pct === null || pct === undefined || Number.isNaN(Number(pct))) return '—'
  return `${Math.abs(Number(pct)).toFixed(1)}%`
}

function deltaArrow(pct: number): string {
  if (!Number.isFinite(pct) || pct === 0) return '='
  return pct > 0 ? '↑' : '↓'
}

function deltaClass(pct: number): string {
  if (!Number.isFinite(pct) || pct === 0) return 'delta-flat'
  return pct > 0 ? 'delta-up' : 'delta-down'
}

function resetFilters() {
  searchKeyword.value = ''
  statusFilters.value = []
  minAbsDelta.value = 0
  sortKey.value = 'delta_desc'
}

// ===================== 数据加载 =====================
async function loadCatalogMeta() {
  // 1) 拿 catalog 总数 + 本地 model 绑定分布（binding-stats 是元信息真源）。
  // 2) 顺便用 searchCatalog({limit:1}) 拿一条最新条目的 synced_at，展示"最近同步于"。
  //    catalogTotal 不再依赖 search 接口的 total（历史 bug：曾把 len(results) 当 total）。
  try {
    const stats = await getCatalogBindingStats()
    bindingStats.value = stats
    catalogTotal.value = stats.catalog_total
  } catch {
    // stats 失败不阻塞主流程，但要把 stats 置空，避免空状态文案乱猜。
    bindingStats.value = null
  }
  try {
    const res = await searchCatalog({ limit: 1 })
    if (res.data.length > 0) lastSyncedAt.value = res.data[0].synced_at
  } catch {
    // searched 失败时同步时间留空即可。
  }
}

async function loadDiff() {
  loading.value = true
  try {
    const result = await getCatalogDiff()
    diffRows.value = result.data
    selectedIds.value = []
  } catch {
    ElMessage.error('加载 Catalog 差异失败')
  } finally {
    loading.value = false
  }
}

// ===================== 选择 / 应用 =====================
function handleSelectionChange(rows: PriceDiffEntry[]) {
  selectedIds.value = rows.map((r) => r.model_id)
}

function selectAllFiltered() {
  selectedIds.value = filteredRows.value.map((r) => r.model_id)
  ElMessage.info(`已选中筛选范围内全部 ${selectedIds.value.length} 条`)
}

function clearSelection() {
  selectedIds.value = []
}

async function handleApply() {
  if (!selectedIds.value.length) {
    ElMessage.warning('请先勾选要应用的差异行')
    return
  }
  applying.value = true
  try {
    const result = await applyCatalogDiff(selectedIds.value)
    const msg =
      `已更新 ${result.updated} 项 · 跳过 ${result.skipped} 项` +
      (result.failed_ids.length ? ` · 失败 ${result.failed_ids.length} 项` : '')
    ElMessage.success(msg)
    selectedIds.value = []
    await loadDiff()
  } catch {
    ElMessage.error('应用差异失败')
  } finally {
    applying.value = false
  }
}

// ===================== 导出 CSV =====================
function exportCSV() {
  const rows = sortedRows.value
  if (!rows.length) {
    ElMessage.warning('当前筛选下没有数据可导出')
    return
  }
  const header = [
    'model_id',
    'model_name',
    'catalog_source_model_id',
    'is_overridden',
    'local_input_price',
    'local_output_price',
    'catalog_input_price',
    'catalog_output_price',
    'input_delta_pct',
    'output_delta_pct',
    'catalog_deprecation_date'
  ]
  const escape = (v: unknown) => {
    if (v === null || v === undefined) return ''
    const s = String(v)
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const csv = [
    header.join(','),
    ...rows.map((r) =>
      [
        r.model_id,
        r.model_name,
        r.catalog_source_model_id,
        r.is_overridden,
        r.local_input_price,
        r.local_output_price,
        r.catalog_input_price,
        r.catalog_output_price,
        r.input_delta_pct,
        r.output_delta_pct,
        r.catalog_deprecation_date ?? ''
      ]
        .map(escape)
        .join(',')
    )
  ].join('\n')
  // BOM + UTF-8，避免 Excel 中文乱码
  const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const ts = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
  link.href = url
  link.download = `catalog-diff-${ts}.csv`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// ===================== 智能绑定 / 手动刷新 =====================
async function handleAutoBind() {
  autoBinding.value = true
  try {
    const res = await autoBindCatalog()
    autoBindResult.value = res
    // 每次重新打开弹窗都清空人工选择，避免上一轮残留
    ambiguousSelections.value = {}
    ambiguousResetFields.value = false
    autoBindDialogVisible.value = true
    if (res.matched.length > 0) {
      ElMessage.success(`已自动绑定 ${res.matched.length} 条本地模型到目录`)
      await loadDiff()
    } else if (res.ambiguous.length > 0) {
      ElMessage.warning(
        `${res.ambiguous.length} 条本地模型有多个目录候选，需要人工选择`
      )
    } else {
      ElMessage.info('当前没有可自动绑定的本地模型（或全部已绑定）')
    }
  } catch {
    ElMessage.error('智能绑定失败，请重试或检查后端日志')
  } finally {
    autoBinding.value = false
  }
}

/**
 * CAT-2：把 ambiguous 段中 admin 已选的条目批量绑定。
 * 单条失败不阻塞，最终把成功项从 ambiguous 列表里抹去；剩下的让 admin 继续处理。
 */
async function handleAmbiguousSave() {
  if (!autoBindResult.value) return
  const bindings = Object.entries(ambiguousSelections.value)
    .filter(([, cid]) => !!cid)
    .map(([modelId, catalogId]) => ({
      model_id: modelId,
      catalog_id: catalogId,
      reset_fields: ambiguousResetFields.value
    }))
  if (!bindings.length) {
    ElMessage.info('请先为至少一行选择目录条目')
    return
  }
  batchBinding.value = true
  try {
    const res = await batchBindModelCatalog(bindings)
    if (res.applied > 0) {
      ElMessage.success(`已成功绑定 ${res.applied} 条本地模型到目录`)
    }
    const failedCount = Object.keys(res.failed_ids).length
    if (failedCount > 0) {
      ElMessage.warning(`${failedCount} 条绑定失败，请查看后端日志`)
    }
    // 把已成功的从 ambiguous 列表抹去 + 把对应的 model_id 移入 matched
    const succeededIds = new Set(
      bindings
        .map((b) => b.model_id)
        .filter((id) => !res.failed_ids[id])
    )
    if (autoBindResult.value && succeededIds.size > 0) {
      const remainingAmbiguous = autoBindResult.value.ambiguous.filter(
        (a) => !succeededIds.has(a.model_id)
      )
      const newlyMatched = autoBindResult.value.ambiguous
        .filter((a) => succeededIds.has(a.model_id))
        .map((a) => {
          const cid = ambiguousSelections.value[a.model_id]
          const cand = a.candidates.find((c) => c.catalog_id === cid)
          return {
            model_id: a.model_id,
            model_name: a.model_name,
            catalog_id: cid,
            catalog_source_model_id: cand?.catalog_source_model_id || ''
          }
        })
      autoBindResult.value = {
        ...autoBindResult.value,
        matched: [...autoBindResult.value.matched, ...newlyMatched],
        ambiguous: remainingAmbiguous
      }
      // 清掉已成功项的选择
      for (const id of succeededIds) {
        delete ambiguousSelections.value[id]
      }
    }
    await loadDiff()
  } catch {
    ElMessage.error('批量绑定失败，请重试或检查后端日志')
  } finally {
    batchBinding.value = false
  }
}

async function handleManualRefresh() {
  refreshing.value = true
  try {
    const job = await triggerCatalogSync()
    ElMessage.success(
      `已触发同步任务（${job.job_id?.slice(0, 8) ?? ''}），稍后将在后台拉取最新目录数据`
    )
    setTimeout(() => {
      loadCatalogMeta()
      loadDiff()
    }, 4000)
  } catch {
    ElMessage.error('触发同步任务失败')
  } finally {
    refreshing.value = false
  }
}

onMounted(() => {
  loadCatalogMeta()
  loadDiff()
})

defineExpose({ selectedIds, loadDiff, filteredRows, sortedRows, pagedRows })
</script>

<style scoped>
.catalog-sync-page {
  padding: 24px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 16px;
  gap: 16px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0 0 6px;
}

.page-subtitle {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.6;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}

.sync-meta {
  color: var(--el-text-color-secondary);
}

.sync-time {
  font-family: monospace;
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  padding: 12px 16px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  margin-bottom: 12px;
}

.search-input {
  width: 280px;
}

.status-select {
  width: 240px;
}

.threshold-control {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.threshold-label {
  flex-shrink: 0;
}

.sort-select {
  width: 240px;
}

.mono {
  font-family: monospace;
  margin-right: 6px;
}

.dim {
  color: var(--el-text-color-secondary);
}

.badge-override {
  margin-left: 6px;
}

.badge-deprecation {
  margin-left: 6px;
}

.delta-up {
  color: #e74c3c;
  font-weight: 600;
}

.delta-down {
  color: #27ae60;
  font-weight: 600;
}

.delta-flat {
  color: #98989d;
}

.footer-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
  gap: 16px;
  flex-wrap: wrap;
}

.footer-actions {
  display: flex;
  gap: 12px;
  margin-left: auto;
}

.empty-state {
  margin: 40px auto;
}

.empty-desc {
  max-width: 560px;
  text-align: left;
  margin: 0 auto;
}

.empty-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 12px;
  text-align: center;
}

.empty-hint {
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
  margin: 6px 0;
}

.empty-hint code {
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 12px;
}

.empty-hint a {
  color: var(--el-color-primary);
}

.auto-bind-summary h4 {
  margin: 16px 0 8px;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.bind-list {
  margin: 0;
  padding-left: 0;
  list-style: none;
  max-height: 240px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
}

.bind-list li {
  padding: 6px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.bind-list li:last-child {
  border-bottom: none;
}

.bind-list .arrow {
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

.ellipsis {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-align: right;
}

.ml-8 {
  margin-left: 8px;
}

.ambiguous-table {
  width: 100%;
  margin-top: 8px;
  border-collapse: collapse;
  font-size: 12px;
  table-layout: fixed;
}

.ambiguous-table thead th {
  text-align: left;
  padding: 6px 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  font-weight: normal;
  font-size: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.ambiguous-table thead th:first-child {
  width: 38%;
}

.ambiguous-table tbody td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  vertical-align: middle;
  word-break: break-all;
}

.ambiguous-footer {
  margin-top: 12px;
  padding-top: 8px;
  border-top: 1px dashed var(--el-border-color-lighter);
}
</style>
