'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Table, Button, Input, Select, Toast, Tag, Tabs, TabPane,
  Typography, Spin, Modal, Checkbox, Space, Banner, Progress,
  Popover,
} from '@douyinfe/semi-ui'
import { IconRefresh, IconSearch, IconLink, IconDownload, IconFilter } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import {
  getCatalogDiff, applyCatalogDiff, triggerCatalogSync, getCatalogSyncStatus,
  getCatalogBindingStats, autoBindCatalog, batchBindModelCatalog,
} from '@/services/control-plane'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

// 真实 API 响应类型
interface DiffRow {
  model_id: string
  model_name: string
  catalog_id: string
  catalog_source_model_id: string
  local_input_price: number
  local_output_price: number
  catalog_input_price: number
  catalog_output_price: number
  is_overridden: boolean
  input_delta_pct: number
  output_delta_pct: number
  catalog_deprecation_date: string | null
}

interface BindingStats {
  catalog_total: number
  model_total: number
  bound_count: number
  unbound_count: number
}

interface SyncStatus {
  running: boolean
  last_started_at: string
  last_finished_at: string
  last_duration_ms: number
  last_source: string
  last_count: number
  last_error: string
  catalog_total: number
}

interface AutoBindResult {
  matched: Array<{ model_id: string; model_name: string; catalog_id: string; catalog_source_model_id: string }>
  ambiguous: Array<{ model_id: string; model_name: string; candidates: Array<{ catalog_id: string; source_model_id: string }> }> | null
  unmatched: number
}

function fmtPrice(n: number) {
  if (!n && n !== 0) return '—'
  return `$${n.toFixed(4)}`
}

function DeltaCell({ local, catalog }: { local: number; catalog: number }) {
  const delta = catalog - local
  const pct = local > 0 ? ((delta / local) * 100) : null
  const isUp = delta > 0; const isDown = delta < 0
  if (!local && !catalog) return <Text type="tertiary" size="small">—</Text>
  return (
    <Space vertical align="start" spacing={0}>
      <Text size="small" type={isUp ? 'danger' : isDown ? 'success' : 'secondary'}
        style={{ fontFamily: 'var(--font-mono)' }}>
        {isUp ? '↑' : isDown ? '↓' : ''} {fmtPrice(catalog)}
      </Text>
      <Text type="tertiary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>
        原 {fmtPrice(local)}{pct !== null && pct !== 0 ? ` (${pct > 0 ? '+' : ''}${pct.toFixed(1)}%)` : ''}
      </Text>
    </Space>
  )
}

// 5种空状态
type EmptyKind = 'catalog_empty' | 'no_local_models' | 'all_unbound' | 'no_diff' | 'all_ok'
function getEmptyKind(stats: BindingStats | null, diffTotal: number): EmptyKind {
  if (!stats || stats.catalog_total === 0) return 'catalog_empty'
  if (stats.model_total === 0) return 'no_local_models'
  if (stats.bound_count === 0) return 'all_unbound'
  if (diffTotal === 0) return 'all_ok'
  return 'no_diff'
}

const EMPTY_CONFIG = {
  catalog_empty: { icon: '📭', title: '目录尚未同步', desc: '请先到「目录更新」Tab 触发同步', action: 'sync' },
  no_local_models: { icon: '📦', title: '暂无本地模型', desc: '请先在「模型管理」中注册模型', action: null },
  all_unbound: { icon: '🔗', title: '模型尚未绑定目录', desc: '请使用「智能绑定」自动匹配模型与目录', action: 'bind' },
  no_diff: { icon: '✅', title: '价格与目录一致', desc: '所有已绑定模型的价格与目录参考值一致', action: null },
  all_ok: { icon: '✅', title: '价格与目录一致', desc: '所有已绑定模型的价格与目录参考值一致', action: null },
}

const STATUS_FILTERS = [
  { label: '待补价格', value: 'price_unset' },
  { label: '价格偏离', value: 'price_diff' },
  { label: '已脱离目录', value: 'overridden' },
  { label: '废弃提醒', value: 'deprecated' },
]

const SORT_OPTIONS = [
  { label: '差异最大优先', value: 'delta_desc' },
  { label: '差异最小优先', value: 'delta_asc' },
  { label: '按模型名', value: 'name_asc' },
  { label: '价格最高', value: 'price_desc' },
]

export default function CatalogSyncPage() {
  const [tab, setTab] = useState('sync')
  const [diffRows, setDiffRows] = useState<DiffRow[]>([])
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [autoBinding, setAutoBinding] = useState(false)
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null)
  const [stats, setStats] = useState<BindingStats | null>(null)

  // 筛选/排序
  const [search, setSearch] = useState('')
  const [statusFilters, setStatusFilters] = useState<string[]>([])
  const [sortKey, setSortKey] = useState('delta_desc')
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)

  // 智能绑定弹窗
  const [bindResultVisible, setBindResultVisible] = useState(false)
  const [bindResult, setBindResult] = useState<AutoBindResult | null>(null)
  const [ambiguousSelections, setAmbiguousSelections] = useState<Record<string, string>>({})
  const [savingAmbiguous, setSavingAmbiguous] = useState(false)
  const [resetFields, setResetFields] = useState(false)

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      const [diff, bindStats, syncSt] = await Promise.all([
        getCatalogDiff(),
        getCatalogBindingStats(),
        getCatalogSyncStatus(),
      ])
      setDiffRows(diff.data as any)
      setStats(bindStats as any)
      setSyncStatus(syncSt as any)
    } catch { Toast.error('加载失败') }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { loadAll() }, [loadAll])

  function startPoll() {
    if (pollRef.current) return
    let count = 0
    pollRef.current = setInterval(async () => {
      count++
      try {
        const st = await getCatalogSyncStatus() as any as SyncStatus
        setSyncStatus(st)
        if (!st.running || count > 40) {
          clearInterval(pollRef.current!); pollRef.current = null
          if (!st.running) loadAll()
        }
      } catch { clearInterval(pollRef.current!); pollRef.current = null }
    }, 1500)
  }

  useEffect(() => () => { if (pollRef.current) clearInterval(pollRef.current) }, [])

  async function handleSync() {
    setSyncing(true)
    try {
      await triggerCatalogSync()
      setSyncStatus(prev => ({ ...(prev || {} as any), running: true }))
      startPoll()
    } catch { Toast.error('触发同步失败') }
    finally { setSyncing(false) }
  }

  async function handleAutoBind() {
    setAutoBinding(true)
    try {
      const r = await autoBindCatalog() as any as AutoBindResult
      setBindResult(r)
      setAmbiguousSelections({})
      setBindResultVisible(true)
      loadAll()
    } catch { Toast.error('自动绑定失败') }
    finally { setAutoBinding(false) }
  }

  async function handleSaveAmbiguous() {
    const bindings = Object.entries(ambiguousSelections)
      .filter(([, catalogId]) => catalogId)
      .map(([modelId, catalogId]) => ({ model_id: modelId, catalog_id: catalogId }))
    if (!bindings.length) return
    setSavingAmbiguous(true)
    try {
      await batchBindModelCatalog(bindings)
      // 从弹窗中移出已绑定的
      if (bindResult) {
        const savedIds = new Set(bindings.map(b => b.model_id))
        const newMatched = [...(bindResult.matched || []), ...bindings.map(b => {
          const found = (bindResult.ambiguous || []).find(a => a.model_id === b.model_id)
          return { model_id: b.model_id, model_name: found?.model_name || b.model_id, catalog_id: b.catalog_id, catalog_source_model_id: b.catalog_id }
        })]
        const newAmbiguous = (bindResult.ambiguous || []).filter(a => !savedIds.has(a.model_id))
        setBindResult({ ...bindResult, matched: newMatched, ambiguous: newAmbiguous.length ? newAmbiguous : null })
      }
      Toast.success(`已绑定 ${bindings.length} 个模型`)
      setAmbiguousSelections({})
      loadAll()
    } catch { Toast.error('保存失败') }
    finally { setSavingAmbiguous(false) }
  }

  async function handleApplyDiff() {
    if (!selectedIds.length) return
    setApplying(true)
    try {
      const r = await applyCatalogDiff(selectedIds) as any
      const msg = [`已更新 ${r.applied} 项`]
      if (r.skipped) msg.push(`跳过 ${r.skipped} 项`)
      Toast.success(msg.join(' · '))
      setSelectedIds([]); loadAll()
    } catch { Toast.error('应用失败') }
    finally { setApplying(false) }
  }

  // 多条件过滤 + 排序
  const filteredRows = diffRows.filter(r => {
    if (search && !r.model_name?.toLowerCase().includes(search.toLowerCase()) &&
      !r.catalog_source_model_id?.toLowerCase().includes(search.toLowerCase())) return false
    if (statusFilters.includes('price_unset') && (r.local_input_price > 0 || r.local_output_price > 0)) return false
    if (statusFilters.includes('overridden') && !r.is_overridden) return false
    if (statusFilters.includes('deprecated') && !r.catalog_deprecation_date) return false
    if (statusFilters.includes('price_diff') && r.input_delta_pct === 0 && r.output_delta_pct === 0) return false
    return true
  }).sort((a, b) => {
    const aDelta = Math.abs(a.input_delta_pct) + Math.abs(a.output_delta_pct)
    const bDelta = Math.abs(b.input_delta_pct) + Math.abs(b.output_delta_pct)
    if (sortKey === 'delta_desc') return bDelta - aDelta
    if (sortKey === 'delta_asc') return aDelta - bDelta
    if (sortKey === 'name_asc') return (a.model_name || '').localeCompare(b.model_name || '')
    if (sortKey === 'price_desc') return (b.catalog_input_price + b.catalog_output_price) - (a.catalog_input_price + a.catalog_output_price)
    return 0
  })

  // 分组
  const priceUnsetRows = filteredRows.filter(r => r.local_input_price === 0 && r.local_output_price === 0 && (r.catalog_input_price > 0 || r.catalog_output_price > 0))
  const followupRows = filteredRows.filter(r => !(r.local_input_price === 0 && r.local_output_price === 0 && (r.catalog_input_price > 0 || r.catalog_output_price > 0)))
  const deprecatedCount = filteredRows.filter(r => r.catalog_deprecation_date).length
  const overriddenCount = filteredRows.filter(r => r.is_overridden).length

  const pagedRows = filteredRows.slice((page - 1) * pageSize, page * pageSize)

  function exportCSV() {
    const headers = ['模型名称', '目录上游ID', '本地输入价', '目录输入价', '输入变化%', '本地输出价', '目录输出价', '输出变化%', '已脱离目录', '废弃日期']
    const rows = filteredRows.map(r => [
      r.model_name, r.catalog_source_model_id,
      r.local_input_price, r.catalog_input_price, r.input_delta_pct.toFixed(2) + '%',
      r.local_output_price, r.catalog_output_price, r.output_delta_pct.toFixed(2) + '%',
      r.is_overridden ? '是' : '否',
      r.catalog_deprecation_date || '',
    ])
    const csv = '\ufeff' + [headers, ...rows].map(r => r.join(',')).join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a'); a.href = url
    a.download = `catalog-diff-${new Date().toISOString().slice(0, 10)}.csv`
    a.click(); URL.revokeObjectURL(url)
  }

  const emptyKind = getEmptyKind(stats, diffRows.length)
  const emptyCfg = EMPTY_CONFIG[emptyKind]

  const diffColumns: ColumnProps<DiffRow>[] = [
    {
      title: '本地模型',
      dataIndex: 'model_name',
      render: (v, record) => (
        <Space vertical align="start" spacing={2}>
          <Text size="small" strong style={{ fontFamily: 'var(--font-mono)' }}>{v}</Text>
          {record.is_overridden && <Tag color="red" size="small">已脱离目录</Tag>}
          {record.catalog_deprecation_date && (
            <Tag color="orange" size="small">⏰ 废弃 {record.catalog_deprecation_date.slice(0, 10)}</Tag>
          )}
        </Space>
      ),
    },
    {
      title: '目录上游 ID',
      dataIndex: 'catalog_source_model_id',
      render: (v) => <Text type="tertiary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>{v}</Text>,
    },
    {
      title: '输入价格',
      dataIndex: 'catalog_input_price',
      width: 150,
      render: (_v, record) => (
        <DeltaCell local={record.local_input_price} catalog={record.catalog_input_price} />
      ),
    },
    {
      title: '输出价格',
      dataIndex: 'catalog_output_price',
      width: 150,
      render: (_v, record) => (
        <DeltaCell local={record.local_output_price} catalog={record.catalog_output_price} />
      ),
    },
  ]

  function DiffTable({ rows, title, onSelectAll }: { rows: DiffRow[]; title: string; onSelectAll: () => void }) {
    if (!rows.length) return null
    return (
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
          <Text strong>{title}（{rows.length}）</Text>
          <Button size="small" theme="borderless" type="primary" onClick={onSelectAll}>
            选中这组 ({rows.length})
          </Button>
        </div>
        <div className={s.tableCard}>
          <Table columns={diffColumns} dataSource={rows} loading={loading}
            rowKey="model_id" pagination={false} size="small"
            rowSelection={{
              selectedRowKeys: selectedIds,
              onChange: keys => setSelectedIds(keys as string[]),
            }} />
        </div>
      </div>
    )
  }

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>Catalog 同步</h1>
          <p className={s.pageDesc}>同步上游模型目录，保持价格与能力信息最新</p>
        </div>
      </div>

      {/* 统计卡片 */}
      {stats && (
        <div className={s.catalogStats} style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
          {[
            { label: '目录总数', value: stats.catalog_total, color: 'var(--color-primary)' },
            { label: '本地模型', value: stats.model_total, color: 'var(--color-text-secondary)' },
            { label: '已绑定', value: stats.bound_count, color: 'var(--color-success)' },
            { label: '未绑定', value: stats.unbound_count, color: stats.unbound_count > 0 ? 'var(--color-warning)' : 'var(--color-text-tertiary)' },
          ].map(card => (
            <div key={card.label} className={s.catalogStatCard}>
              <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 4 }}>{card.label}</Text>
              <Text strong style={{ fontFamily: 'var(--font-mono)', fontSize: 22, color: card.color }}>{card.value}</Text>
            </div>
          ))}
        </div>
      )}

      <Tabs activeKey={tab} onChange={setTab} type="card">
        {/* ─── 目录更新 ─── */}
        <TabPane tab="目录更新" itemKey="sync">
          <div style={{ paddingTop: 20 }}>
            {/* 同步状态横幅 */}
            {syncStatus?.running && (
              <Banner type="info" description="目录同步中，请稍候..." closeIcon={null} style={{ marginBottom: 16 }} />
            )}
            {syncStatus && !syncStatus.running && syncStatus.last_error && (
              <Banner type="danger" description={`上次同步失败：${syncStatus.last_error}`} closeIcon={null} style={{ marginBottom: 16 }} />
            )}
            {syncStatus && !syncStatus.running && !syncStatus.last_error && syncStatus.last_count > 0 && (
              <Banner type={syncStatus.last_source === 'bundled' ? 'warning' : 'success'}
                description={
                  syncStatus.last_source === 'bundled'
                    ? `使用内置备用数据（GitHub 链路不可达）：收录 ${syncStatus.last_count} 个模型，耗时 ${(syncStatus.last_duration_ms / 1000).toFixed(1)}s`
                    : `同步成功：收录 ${syncStatus.last_count} 个模型，来源：${syncStatus.last_source}，耗时 ${(syncStatus.last_duration_ms / 1000).toFixed(1)}s`
                }
                closeIcon={null} style={{ marginBottom: 16 }} />
            )}

            <div className={s.syncCards}>
              <div className={s.syncCard}>
                <Title heading={6} style={{ marginBottom: 6 }}>刷新模型目录</Title>
                <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 16, lineHeight: 1.6 }}>
                  从上游来源拉取最新模型信息、价格和能力数据。
                  {syncStatus?.last_started_at && (
                    <><br />最近同步：{new Date(syncStatus.last_started_at).toLocaleString('zh-CN')}</>
                  )}
                </Text>
                <Button loading={syncing || !!syncStatus?.running} icon={<IconRefresh />}
                  type="primary" theme="solid" onClick={handleSync}>立即同步</Button>
              </div>

              <div className={s.syncCard}>
                <Title heading={6} style={{ marginBottom: 6 }}>自动绑定模型</Title>
                <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 16, lineHeight: 1.6 }}>
                  按名称自动匹配本地模型与目录条目。
                  当前未绑定 <Text strong>{stats?.unbound_count ?? 0}</Text> 个模型。
                </Text>
                <Button loading={autoBinding} icon={<IconLink />} onClick={handleAutoBind}>智能绑定</Button>
              </div>
            </div>
          </div>
        </TabPane>

        {/* ─── 价格差异 ─── */}
        <TabPane
          tab={`价格差异${diffRows.length > 0 ? ` (${diffRows.length})` : ''}`}
          itemKey="diff">
          <div style={{ paddingTop: 20 }}>
            {/* 差异摘要统计 */}
            {diffRows.length > 0 && (
              <Space spacing={16} style={{ marginBottom: 16 }}>
                {priceUnsetRows.length > 0 && (
                  <Tag color="orange">待补价格 {priceUnsetRows.length}</Tag>
                )}
                {overriddenCount > 0 && (
                  <Tag color="red">已脱离目录 {overriddenCount}</Tag>
                )}
                {deprecatedCount > 0 && (
                  <Tag color="purple">废弃提醒 {deprecatedCount}</Tag>
                )}
              </Space>
            )}

            {/* 高级筛选工具栏 */}
            <div className={s.toolbar}>
              <Input prefix={<IconSearch />} placeholder="搜索模型名 / 目录ID..."
                value={search} onChange={v => { setSearch(v); setPage(1) }}
                style={{ width: 220 }} showClear />
              <Select multiple placeholder="状态筛选" value={statusFilters}
                onChange={v => { setStatusFilters(v as string[]); setPage(1) }}
                style={{ width: 160 }} optionList={STATUS_FILTERS} showClear />
              <Select placeholder="排序方式" value={sortKey}
                onChange={v => setSortKey(v as string)}
                style={{ width: 150 }} optionList={SORT_OPTIONS} />
              {(search || statusFilters.length > 0) && (
                <Button theme="borderless" type="tertiary"
                  onClick={() => { setSearch(''); setStatusFilters([]); setPage(1) }}>
                  重置筛选
                </Button>
              )}
              <Button icon={<IconRefresh />} theme="borderless" type="tertiary"
                onClick={loadAll} loading={loading} />
              <div className={s.toolbarSpacer} />
              {selectedIds.length > 0 && (
                <>
                  <Button theme="borderless" type="tertiary"
                    onClick={() => setSelectedIds(filteredRows.map(r => r.model_id))}>
                    全选筛选 ({filteredRows.length})
                  </Button>
                  <Button theme="borderless" type="tertiary"
                    onClick={() => setSelectedIds([])}>清空选择</Button>
                  <Button type="primary" theme="solid" loading={applying}
                    onClick={handleApplyDiff}>
                    采用目录价格 ({selectedIds.length})
                  </Button>
                </>
              )}
              {diffRows.length > 0 && (
                <Button icon={<IconDownload />} onClick={exportCSV}>导出 CSV</Button>
              )}
            </div>

            {/* 空状态 */}
            {filteredRows.length === 0 && !loading ? (
              <div className={s.emptyState}>
                <div className={s.emptyIcon}>{emptyCfg.icon}</div>
                <div className={s.emptyTitle}>{emptyCfg.title}</div>
                <div className={s.emptyDesc}>{emptyCfg.desc}</div>
                {emptyCfg.action === 'sync' && (
                  <Button style={{ marginTop: 12 }} onClick={() => setTab('sync')}>前往同步</Button>
                )}
                {emptyCfg.action === 'bind' && (
                  <Button style={{ marginTop: 12 }} loading={autoBinding} onClick={handleAutoBind}>
                    智能绑定
                  </Button>
                )}
              </div>
            ) : (
              <>
                {/* 分组表格 */}
                <DiffTable
                  rows={priceUnsetRows} title="待补价格"
                  onSelectAll={() => setSelectedIds(prev => [...new Set([...prev, ...priceUnsetRows.map(r => r.model_id)])])} />
                <DiffTable
                  rows={followupRows} title="已偏离参考价 / 需复核"
                  onSelectAll={() => setSelectedIds(prev => [...new Set([...prev, ...followupRows.map(r => r.model_id)])])} />
              </>
            )}
          </div>
        </TabPane>
      </Tabs>

      {/* 智能绑定结果弹窗 */}
      <Modal
        title={<Title heading={5}>智能绑定结果</Title>}
        visible={bindResultVisible}
        onCancel={() => setBindResultVisible(false)}
        onOk={handleSaveAmbiguous}
        okButtonProps={{ loading: savingAmbiguous, disabled: Object.keys(ambiguousSelections).filter(k => ambiguousSelections[k]).length === 0 }}
        okText="保存已选" cancelText="关闭" width={600}
      >
        {bindResult && (
          <div>
            {/* 已匹配 */}
            {bindResult.matched.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <Tag color="green" style={{ marginBottom: 8 }}>已自动绑定 {bindResult.matched.length} 个</Tag>
                <div style={{ maxHeight: 150, overflowY: 'auto', border: '1px solid var(--color-border)', borderRadius: 8, padding: 8 }}>
                  {bindResult.matched.map(m => (
                    <div key={m.model_id} style={{ fontSize: 12, fontFamily: 'var(--font-mono)', padding: '2px 0', color: 'var(--color-text-secondary)' }}>
                      {m.model_name} → {m.catalog_source_model_id}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* 多候选 */}
            {bindResult.ambiguous && bindResult.ambiguous.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <Tag color="orange" style={{ marginBottom: 8 }}>
                  {bindResult.ambiguous.length} 个模型有多个候选，请手动选择
                </Tag>
                {bindResult.ambiguous.map(item => (
                  <div key={item.model_id} style={{ marginBottom: 12 }}>
                    <Text strong size="small" style={{ display: 'block', marginBottom: 4 }}>{item.model_name}</Text>
                    <Select
                      value={ambiguousSelections[item.model_id] || ''}
                      onChange={v => setAmbiguousSelections(prev => ({ ...prev, [item.model_id]: v as string }))}
                      style={{ width: '100%' }} placeholder="选择匹配的目录条目"
                      optionList={(item.candidates || []).map(c => ({
                        label: c.source_model_id,
                        value: c.catalog_id,
                      }))} />
                  </div>
                ))}
                <Checkbox checked={resetFields} onChange={e => setResetFields(!!e.target.checked)}>
                  同时将价格、上下文、最大输出、上游模型ID重置为目录值
                </Checkbox>
              </div>
            )}

            {/* 未匹配 */}
            {bindResult.unmatched > 0 && (
              <Text type="tertiary" size="small">另有 {bindResult.unmatched} 个模型未找到匹配的目录条目</Text>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}
