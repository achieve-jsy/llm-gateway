'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Table, Button, Input, Select, Switch, Toast, Popconfirm,
  Tag, Space, Typography, Tooltip, Form, Spin, SideSheet, Modal,
  AutoComplete, Divider,
} from '@douyinfe/semi-ui'
import { IconSearch, IconPlus, IconRefresh, IconEdit, IconDelete, IconSetting, IconLink, IconUnlink } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import {
  getModels, getProviders, createModel, updateModel, deleteModel,
  batchUpdateModelStatus, batchDeleteModels, checkModel, getModelTags,
  batchGetModelTags, batchSetModelTags, resetModelOverride, checkAllModels,
  getHealthCheckConfig, updateHealthCheckConfig, checkModelNameUnique,
  searchCatalog, createModelFromCatalog, bindModelCatalog, unbindModelCatalog,
  type HealthCheckConfig,
} from '@/services/control-plane'
import type { Model, ModelPayload } from '@/types/model'
import type { ModelTag } from '@/types/model-tag'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

const MODEL_TYPES = ['chat', 'embedding', 'image', 'rerank', 'audio', 'video']

function fmtTokens(n: number) {
  if (!n) return '—'
  if (n >= 1000000) return (n / 1000000).toFixed(0) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(0) + 'K'
  return String(n)
}

function shortenError(msg: string): string {
  if (!msg) return ''
  const m = msg.toLowerCase()
  if (m.includes('timeout') || m.includes('超时')) return '超时'
  if (m.includes('401') || m.includes('invalid api key') || m.includes('invalid key') || m.includes('authentication')) return 'Key 无效'
  if (m.includes('403') || m.includes('access denied') || m.includes('forbidden')) return '无权限'
  if (m.includes('404') || m.includes('not found') || m.includes('does not exist')) return '模型不存在'
  if (m.includes('429') || m.includes('rate limit')) return '限速'
  if (m.includes('connection') || m.includes('refused') || m.includes('network')) return '网络错误'
  return msg.slice(0, 30) + (msg.length > 30 ? '…' : '')
}

function HealthCell({ m }: { m: Model }) {
  const f = m.consecutive_failures ?? 0
  const rt = m.response_time ?? 0
  const max = 3
  if (f >= max) return (
    <Tooltip content={m.last_check_error || '连续失败'}>
      <Tag color="red" size="small">{shortenError(m.last_check_error) || `失败 ${f}/${max}`}</Tag>
    </Tooltip>
  )
  if (f > 0) return (
    <Tooltip content={m.last_check_error || '有失败记录'}>
      <Tag color="orange" size="small">{`${f}/${max}`}</Tag>
    </Tooltip>
  )
  if (rt > 0) {
    const type = rt < 500 ? 'success' : rt < 2000 ? 'warning' : 'danger'
    return <Text type={type as any} size="small">{rt} ms</Text>
  }
  return <Text type="tertiary" size="small">未检测</Text>
}

function fmtPrice(v: number) {
  if (!v) return <Text type="tertiary" size="small">—</Text>
  return <Text size="small">${v.toFixed(4)}</Text>
}

function fmtDate(s: string) {
  if (!s) return '—'
  return new Date(s).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit', year: 'numeric' })
}

export default function ModelsPage() {
  const [models, setModels] = useState<Model[]>([])
  const [providers, setProviders] = useState<Array<{ id: string; name: string }>>([])
  const [allTags, setAllTags] = useState<ModelTag[]>([])
  const [tagsMap, setTagsMap] = useState<Record<string, ModelTag[]>>({})
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [filterName, setFilterName] = useState('')
  const [filterProvider, setFilterProvider] = useState<string | undefined>()
  const [filterEnabled, setFilterEnabled] = useState<string | undefined>()
  const [filterTags, setFilterTags] = useState<string[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [checkingIds, setCheckingIds] = useState<Set<string>>(new Set())
  const [checkingAll, setCheckingAll] = useState(false)
  const [sheetVisible, setSheetVisible] = useState(false)
  const [editing, setEditing] = useState<Model | null>(null)
  const [formApi, setFormApi] = useState<any>(null)
  const [submitting, setSubmitting] = useState(false)

  // 检测设置弹窗
  const [checkSettingsVisible, setCheckSettingsVisible] = useState(false)
  const [checkConfig, setCheckConfig] = useState<Partial<HealthCheckConfig>>({})
  const [savingConfig, setSavingConfig] = useState(false)

  // 批量设置标签弹窗
  const [batchTagVisible, setBatchTagVisible] = useState(false)
  const [batchTagIds, setBatchTagIds] = useState<string[]>([])
  const [savingBatchTag, setSavingBatchTag] = useState(false)

  // 注册弹窗内的 Catalog 联想
  const [catalogSuggestions, setCatalogSuggestions] = useState<any[]>([])
  const catalogTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const opts: any = { page, pageSize }
      if (filterName) opts.name = filterName
      if (filterEnabled !== undefined && filterEnabled !== '') opts.enabled = filterEnabled === 'true'
      const res = await getModels(filterProvider, opts)
      setModels(res.data)
      setTotal(res.total)
      if (res.data.length) {
        batchGetModelTags(res.data.map(m => m.id)).then(setTagsMap).catch(() => {})
      }
    } catch { Toast.error('获取模型列表失败') }
    finally { setLoading(false) }
  }, [page, pageSize, filterName, filterProvider, filterEnabled])

  useEffect(() => { load() }, [load])
  useEffect(() => {
    getProviders({ pageSize: 1000 }).then(r => setProviders(r.data)).catch(() => {})
    getModelTags().then(setAllTags).catch(() => {})
  }, [])

  const displayModels = filterTags.length
    ? models.filter(m => {
        const mt = tagsMap[m.id] ?? []
        return filterTags.every(tid => mt.some(t => t.id === tid))
      })
    : models

  async function handleCheck(m: Model) {
    setCheckingIds(p => new Set(p).add(m.id))
    try {
      const r = await checkModel(m.id)
      if (r.success) Toast.success(`检测通过 · ${r.response_time} ms`)
      else Toast.warning(`检测失败：${shortenError(r.error || '')}`)
      if (r.auto_disabled) {
        load()
      } else {
        setModels(prev => prev.map(row => row.id === m.id
          ? { ...row, response_time: r.response_time, consecutive_failures: r.success ? 0 : (row.consecutive_failures || 0) + 1 }
          : row))
      }
    } catch { Toast.error('检测失败') }
    finally { setCheckingIds(p => { const s = new Set(p); s.delete(m.id); return s }) }
  }

  async function handleToggle(m: Model, enabled: boolean) {
    setModels(prev => prev.map(r => r.id === m.id ? { ...r, enabled } : r))
    try { await batchUpdateModelStatus([m.id], enabled) }
    catch {
      setModels(prev => prev.map(r => r.id === m.id ? { ...r, enabled: !enabled } : r))
      Toast.error('操作失败')
    }
  }

  async function handleDelete(id: string) {
    try { await deleteModel(id); Toast.success('已删除'); load() }
    catch { Toast.error('删除失败') }
  }

  async function handleBatchStatus(enabled: boolean) {
    Modal.confirm({
      title: `确认批量${enabled ? '启用' : '禁用'}`,
      content: `即将${enabled ? '启用' : '禁用'} ${selected.length} 个模型，确认继续？`,
      onOk: async () => {
        try {
          await batchUpdateModelStatus(selected, enabled)
          Toast.success(`已${enabled ? '启用' : '禁用'} ${selected.length} 个模型`)
          setSelected([]); load()
        } catch { Toast.error('操作失败') }
      },
    })
  }

  async function handleBatchDelete() {
    Modal.confirm({
      title: '确认批量删除',
      content: `即将删除 ${selected.length} 个模型，此操作不可撤销！`,
      onOk: async () => {
        try {
          await batchDeleteModels(selected)
          Toast.success(`已删除 ${selected.length} 个模型`)
          setSelected([]); load()
        } catch { Toast.error('删除失败') }
      },
    })
  }

  async function handleBatchTag() {
    setSavingBatchTag(true)
    try {
      await batchSetModelTags(selected, batchTagIds)
      Toast.success(`已为 ${selected.length} 个模型设置标签`)
      setBatchTagVisible(false)
      setBatchTagIds([])
      load()
    } catch { Toast.error('设置标签失败') }
    finally { setSavingBatchTag(false) }
  }

  // Catalog 联想搜索（防抖）
  async function handleCatalogSearch(q: string) {
    if (catalogTimerRef.current) clearTimeout(catalogTimerRef.current)
    if (!q || q.length < 2) { setCatalogSuggestions([]); return }
    catalogTimerRef.current = setTimeout(async () => {
      try {
        const r = await searchCatalog({ q, limit: 8 })
        setCatalogSuggestions(r.data)
      } catch { /* ignore */ }
    }, 300)
  }

  function handleCatalogSelect(item: any) {
    if (!item || !formApi) return
    formApi.setValue('upstream_model_id', item.model_id)
    formApi.setValue('context_length', item.context_length)
    formApi.setValue('max_output_tokens', item.max_output_tokens)
    formApi.setValue('input_price', item.input_price)
    formApi.setValue('output_price', item.output_price)
    Toast.info('已填充 Catalog 数据，修改价格/上下文后将脱离目录')
  }

  async function handleSubmit(values: any) {
    // 名称唯一性校验（新建时）
    if (!editing) {
      try {
        const available = await checkModelNameUnique(values.name)
        if (!available) { Toast.error('模型名称已存在，请换一个'); return }
      } catch { /* 接口失败忽略，由后端兜底 */ }
    }

    setSubmitting(true)
    try {
      const payload: ModelPayload = {
        name: values.name, provider_id: values.provider_id,
        model_type: values.model_type, enabled: values.enabled ?? true,
        upstream_model_id: values.upstream_model_id,
        description: values.description, context_length: values.context_length,
        max_output_tokens: values.max_output_tokens,
        input_price: values.input_price, output_price: values.output_price,
        weight: values.weight,
      }
      let modelId = editing?.id || ''
      if (editing) {
        await updateModel(editing.id, payload)
      } else {
        const created = await createModel(payload)
        modelId = created.id
      }
      // 保存标签
      const tagIds: string[] = values.tag_ids || []
      if (tagIds.length > 0 && modelId) {
        await batchSetModelTags([modelId], tagIds).catch(() => {})
      }
      Toast.success(editing ? '已更新' : '已创建')
      setSheetVisible(false); load()
    } catch { Toast.error(editing ? '更新失败' : '创建失败') }
    finally { setSubmitting(false) }
  }

  async function openCheckSettings() {
    try {
      const cfg = await getHealthCheckConfig()
      setCheckConfig(cfg)
      setCheckSettingsVisible(true)
    } catch { Toast.error('加载检测配置失败') }
  }

  async function saveCheckSettings() {
    setSavingConfig(true)
    try {
      await updateHealthCheckConfig(checkConfig)
      Toast.success('检测配置已保存')
      setCheckSettingsVisible(false)
    } catch { Toast.error('保存失败') }
    finally { setSavingConfig(false) }
  }

  const columns: ColumnProps<Model>[] = [
    {
      title: '模型名称',
      dataIndex: 'name',
      render: (v, record) => (
        <Space vertical align="start" spacing={2}>
          <Text strong style={{ fontFamily: 'var(--font-mono)', fontSize: 13 }}>{v}</Text>
          {record.upstream_model_id && record.upstream_model_id !== v && (
            <Text type="tertiary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>
              → {record.upstream_model_id}
            </Text>
          )}
        </Space>
      ),
    },
    {
      title: '渠道',
      dataIndex: 'provider',
      width: 120,
      render: (v) => v
        ? <Text size="small" type="secondary">{v.name}</Text>
        : <Text type="tertiary" size="small">—</Text>,
    },
    {
      title: '类型',
      dataIndex: 'model_type',
      width: 90,
      render: (v) => (
        <Tag
          color={v === 'chat' ? 'blue' : v === 'embedding' ? 'green' : 'grey'}
          size="small"
        >{v}</Tag>
      ),
    },
    {
      title: '标签',
      dataIndex: 'id',
      width: 160,
      render: (id) => {
        const tags = tagsMap[id] ?? []
        if (!tags.length) return <Text type="tertiary" size="small">—</Text>
        return (
          <Space spacing={4} wrap>
            {tags.slice(0, 3).map(t => (
              <Tag key={t.id} size="small" style={{ background: t.color + '20', color: t.color, border: `1px solid ${t.color}40` }}>
                {t.name}
              </Tag>
            ))}
            {tags.length > 3 && <Text type="tertiary" size="small">+{tags.length - 3}</Text>}
          </Space>
        )
      },
    },
    {
      title: '上下文',
      dataIndex: 'context_length',
      width: 100,
      render: (v, record) => (
        <Space spacing={4}>
          <Text size="small" style={{ fontFamily: 'var(--font-mono)' }}>{fmtTokens(v)}</Text>
          {record.ctx_overridden && (
            <Tooltip content="已脱离目录，点击恢复跟随">
              <Tag
                color="orange" size="small" style={{ cursor: 'pointer' }}
                onClick={() => resetModelOverride(record.id, 'context_length').then(load).catch(() => Toast.error('操作失败'))}
              >脱离</Tag>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: '输入价格',
      dataIndex: 'input_price',
      width: 100,
      render: (v) => fmtPrice(v),
    },
    {
      title: '输出价格',
      dataIndex: 'output_price',
      width: 100,
      render: (v) => fmtPrice(v),
    },
    {
      title: '健康',
      dataIndex: 'response_time',
      width: 110,
      render: (_v, record) => <HealthCell m={record} />,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 70,
      align: 'center' as const,
      render: (v, record) => (
        <Switch checked={v} size="small" onChange={(checked) => handleToggle(record, checked)} />
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 110,
      render: (v) => <Text type="tertiary" size="small">{fmtDate(v)}</Text>,
    },
    {
      title: '操作',
      width: 160,
      render: (_v, record) => (
        <div className={s.actions}>
          <Button size="small" theme="borderless" type="tertiary"
            icon={checkingIds.has(record.id) ? <Spin size="small" /> : <IconRefresh />}
            disabled={checkingIds.has(record.id)}
            onClick={() => handleCheck(record)}>检测</Button>
          <Button size="small" theme="borderless" type="tertiary"
            icon={<IconEdit />}
            onClick={() => { setEditing(record); setSheetVisible(true) }}>编辑</Button>
          <Popconfirm title="确认删除？" content="删除后不可恢复。" onConfirm={() => handleDelete(record.id)}>
            <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} />
          </Popconfirm>
        </div>
      ),
    },
  ]

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>模型管理</h1>
          <p className={s.pageDesc}>注册和管理模型实例，监控健康状态</p>
        </div>
        <div className={s.pageHeaderActions}>
          {selected.length > 0 && (
            <>
              <Button size="small" onClick={() => handleBatchStatus(true)}>
                批量启用 ({selected.length})
              </Button>
              <Button size="small" type="warning" onClick={() => handleBatchStatus(false)}>
                批量禁用 ({selected.length})
              </Button>
              <Button size="small" onClick={() => { setBatchTagIds([]); setBatchTagVisible(true) }}>
                设置标签 ({selected.length})
              </Button>
              <Button size="small" type="danger" onClick={handleBatchDelete}>
                批量删除 ({selected.length})
              </Button>
            </>
          )}
          <Button
            loading={checkingAll} icon={<IconRefresh />}
            onClick={async () => {
              setCheckingAll(true)
              try {
                const r = await checkAllModels()
                Toast.success(`批量检测完成：${r.success}/${r.total} 通过`)
                load()
              } catch { Toast.error('批量检测失败') }
              finally { setCheckingAll(false) }
            }}>全量检测</Button>
          <Tooltip content="检测参数设置">
            <Button icon={<IconSetting />} onClick={openCheckSettings} />
          </Tooltip>
          <Button icon={<IconPlus />} theme="solid" type="primary"
            onClick={() => { setEditing(null); setSheetVisible(true) }}>
            注册模型
          </Button>
        </div>
      </div>

      <div className={s.toolbar}>
        <Input prefix={<IconSearch />} placeholder="搜索模型名称..." value={filterName}
          onChange={v => { setFilterName(v); setPage(1) }}
          style={{ width: 200 }} showClear />
        <Select placeholder="所有渠道" value={filterProvider}
          onChange={v => { setFilterProvider(v as string); setPage(1) }}
          style={{ width: 140 }}
          optionList={providers.map(p => ({ label: p.name, value: p.id }))} showClear />
        <Select placeholder="所有状态" value={filterEnabled}
          onChange={v => { setFilterEnabled(v as string); setPage(1) }}
          style={{ width: 120 }}
          optionList={[{ label: '已启用', value: 'true' }, { label: '已禁用', value: 'false' }]} showClear />
        <Select placeholder="按标签筛选" multiple value={filterTags}
          onChange={v => setFilterTags(v as string[])}
          style={{ width: 160 }}
          optionList={allTags.map(t => ({ label: t.name, value: t.id }))} showClear />
        <Button icon={<IconRefresh />} theme="borderless" type="tertiary" onClick={load} loading={loading} />
        <div className={s.toolbarSpacer} />
        <Text type="tertiary" size="small">共 {total} 条</Text>
      </div>

      <div className={s.tableCard}>
        <Table columns={columns} dataSource={displayModels} loading={loading} rowKey="id"
          rowSelection={{ selectedRowKeys: selected, onChange: keys => setSelected(keys as string[]) }}
          pagination={{
            total, currentPage: page, pageSize,
            onPageChange: setPage,
            showTotal: true,
            pageSizeOpts: [10, 20, 50, 100, 200],
            onPageSizeChange: (size) => { setPageSize(size); setPage(1) },
          }}
          empty={
            <div className={s.emptyState}>
              <div className={s.emptyIcon}>📦</div>
              <div className={s.emptyTitle}>暂无模型</div>
              <div className={s.emptyDesc}>点击「注册模型」开始配置</div>
            </div>
          } />
      </div>

      {/* 注册/编辑抽屉 */}
      <SideSheet
        title={
          <Space vertical align="start" spacing={2}>
            <Title heading={5}>{editing ? '编辑模型' : '注册模型'}</Title>
            <Text type="tertiary" size="small">{editing ? '修改模型配置' : '向系统注册一个新的模型实例'}</Text>
          </Space>
        }
        visible={sheetVisible} onCancel={() => setSheetVisible(false)} width={480}
        footer={
          <Space>
            <Button onClick={() => setSheetVisible(false)}>取消</Button>
            <Button type="primary" theme="solid" loading={submitting} onClick={() => formApi?.submitForm()}>
              {editing ? '保存更改' : '注册模型'}
            </Button>
          </Space>
        }
      >
        <Form getFormApi={api => setFormApi(api)} onSubmit={handleSubmit}
          initValues={editing ? {
            name: editing.name, provider_id: editing.provider_id,
            model_type: editing.model_type, enabled: editing.enabled,
            upstream_model_id: editing.upstream_model_id,
            description: editing.description, context_length: editing.context_length,
            max_output_tokens: editing.max_output_tokens,
            input_price: editing.input_price, output_price: editing.output_price,
            weight: editing.weight,
            tag_ids: tagsMap[editing.id]?.map(t => t.id) || [],
          } : { enabled: true, weight: 1, model_type: 'chat' }}
          layout="vertical">
          {/* 模型名称 + Catalog 联想（仅新建） */}
          {!editing ? (
            <div style={{ marginBottom: 16 }}>
              <label className={s.fieldLabel}>模型名称 <span style={{ color: 'var(--color-error)' }}>*</span></label>
              <AutoComplete
                data={catalogSuggestions.map(c => ({
                  value: c.model_id,
                  label: c.model_id,
                  extra: c,
                }))}
                value={formApi?.getValue('name') || ''}
                onChange={v => {
                  formApi?.setValue('name', v)
                  handleCatalogSearch(String(v))
                }}
                onSelect={(option: any) => {
                  const item = typeof option === 'object' ? option : { value: option, extra: null }
                  formApi?.setValue('name', item.value)
                  if (item.extra) handleCatalogSelect(item.extra)
                }}
                renderSelectedItem={(option: any) => option.value || option}
                renderItem={(option: any) => {
                  const c = option.extra
                  return (
                    <div style={{ padding: '4px 0' }}>
                      <Text strong size="small" style={{ fontFamily: 'var(--font-mono)' }}>{option.value}</Text>
                      {c && (
                        <Text type="tertiary" size="small" style={{ display: 'block' }}>
                          {c.context_length ? `${Math.round(c.context_length / 1000)}K ctx` : ''}
                          {c.input_price ? ` · $${c.input_price} in` : ''}
                        </Text>
                      )}
                    </div>
                  )
                }}
                placeholder="输入模型名，将从 Catalog 自动联想"
                style={{ width: '100%' }}
              />
              <Text type="tertiary" size="small" style={{ marginTop: 4, display: 'block' }}>
                选中 Catalog 条目后将自动填充价格、上下文等字段
              </Text>
            </div>
          ) : (
            <Form.Input field="name" label="模型名称" rules={[{ required: true, message: '请输入名称' }]} />
          )}
          <Form.Select field="provider_id" label="所属渠道" rules={[{ required: true }]}
            optionList={providers.map(p => ({ label: p.name, value: p.id }))} />
          <Form.Select field="model_type" label="模型类型" rules={[{ required: true }]}
            optionList={MODEL_TYPES.map(t => ({ label: t, value: t }))} />
          <Form.Input field="upstream_model_id" label="上游模型 ID（可选）" placeholder="留空则使用模型名称" />
          <Form.Switch field="enabled" label="启用" />
          <Space>
            <Form.InputNumber field="context_length" label="上下文长度" min={0} step={1024} />
            <Form.InputNumber field="max_output_tokens" label="最大输出 Token" min={0} step={1024} />
          </Space>
          <Space>
            <Form.InputNumber field="input_price" label="输入价格（$/1M token）" min={0} step={0.1} precision={6} />
            <Form.InputNumber field="output_price" label="输出价格（$/1M token）" min={0} step={0.1} precision={6} />
          </Space>
          <Form.Select field="tag_ids" label="模型标签" multiple
            optionList={allTags.map(t => ({
              label: t.name,
              value: t.id,
            }))}
            renderSelectedItem={(option: any) => {
              const tag = allTags.find(t => t.id === option.value)
              return tag ? (
                <Tag size="small" style={{ background: tag.color + '20', color: tag.color, border: `1px solid ${tag.color}40` }}>
                  {tag.name}
                </Tag>
              ) : option.label
            }} />
          <Form.InputNumber field="weight" label="权重" min={0} max={100} />
          <Form.TextArea field="description" label="描述" autosize={{ minRows: 2 }} />
        </Form>
      </SideSheet>

      {/* 检测设置弹窗 */}
      <Modal
        title={<Title heading={5}>模型检测设置</Title>}
        visible={checkSettingsVisible}
        onCancel={() => setCheckSettingsVisible(false)}
        onOk={saveCheckSettings}
        okButtonProps={{ loading: savingConfig }}
        okText="保存" width={480}
      >
        <Form layout="vertical" initValues={checkConfig}
          onValueChange={vals => setCheckConfig(prev => ({ ...prev, ...vals }))}>
          <Form.Switch field="model_check_enabled" label="启用自动模型检测" />
          <Form.InputNumber field="model_check_interval_seconds" label="检测间隔（秒）" min={30} />
          <Form.InputNumber field="model_check_failure_threshold" label="自动禁用阈值（连续失败次数）" min={1} max={20} />
        </Form>
      </Modal>

      {/* 批量设置标签弹窗 */}
      <Modal
        title={<Title heading={5}>批量设置标签</Title>}
        visible={batchTagVisible}
        onCancel={() => setBatchTagVisible(false)}
        onOk={handleBatchTag}
        okButtonProps={{ loading: savingBatchTag }}
        okText="确认设置" width={400}
      >
        <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 12 }}>
          已选择 <Text strong>{selected.length}</Text> 个模型，将为这些模型统一设置以下标签（会覆盖原有标签）：
        </Text>
        <Select
          multiple
          value={batchTagIds}
          onChange={v => setBatchTagIds(v as string[])}
          style={{ width: '100%' }}
          placeholder="选择要设置的标签"
          optionList={allTags.map(t => ({ label: t.name, value: t.id }))}
          renderSelectedItem={(option: any) => {
            const tag = allTags.find(t => t.id === option.value)
            return tag ? (
              <Tag size="small" style={{ background: tag.color + '20', color: tag.color }}>
                {tag.name}
              </Tag>
            ) : option.label
          }}
        />
      </Modal>
    </div>
  )
}
