'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  Table, Button, Tag, Space, Input, Select, Toast, Typography,
  Popconfirm, Tooltip, Form, Spin, SideSheet, Modal, Tabs, TabPane,
  Checkbox, Banner, Progress,
} from '@douyinfe/semi-ui'
import {
  IconSearch, IconPlus, IconRefresh, IconEdit, IconDelete,
  IconPlay, IconPause, IconSetting, IconSync,
} from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import {
  getProviders, getProviderDefinitions, createProvider, updateProvider,
  deleteProvider, toggleProviderStatus, checkProvider, checkAllProviders,
  syncModels, confirmSync, getHealthCheckConfig, updateHealthCheckConfig,
  type HealthCheckConfig,
} from '@/services/control-plane'
import type { Provider, ProviderDefinition, ProviderPayload, CredentialFieldSchema } from '@/types/provider'
import { ProviderStatus } from '@/types/provider'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

function StatusChip({ p }: { p: Provider }) {
  if (!p.enabled) return <Tag color="grey" size="small">已禁用</Tag>
  if (p.status === ProviderStatus.AUTO_DISABLED) return <Tag color="red" size="small">自动禁用</Tag>
  return <Tag color="green" size="small">正常</Tag>
}

function RespTime({ ms }: { ms: number }) {
  if (!ms) return <Text type="tertiary" size="small">—</Text>
  const type = ms < 500 ? 'success' : ms < 2000 ? 'warning' : 'danger'
  return <Text type={type as any} size="small" style={{ fontFamily: 'var(--font-mono)' }}>{ms} ms</Text>
}

/* 根据 schema 动态渲染凭证字段 */
function CredentialFields({ schema }: { schema: Record<string, CredentialFieldSchema> }) {
  if (!schema || !Object.keys(schema).length) {
    return (
      <Form.TextArea field="credentials_raw" label="凭证 JSON"
        placeholder='{"api_key": "sk-..."}'
        autosize={{ minRows: 3, maxRows: 6 }}
        style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} />
    )
  }
  return (
    <>
      {Object.entries(schema).map(([fieldKey, fieldDef]) => (
        <Form.Input
          key={fieldKey}
          field={`cred_${fieldKey}`}
          label={fieldDef.label || fieldKey}
          mode={fieldDef.secret ? 'password' : undefined}
          placeholder={fieldDef.placeholder || ''}
          rules={fieldDef.required ? [{ required: true, message: `请填写 ${fieldDef.label}` }] : []}
        />
      ))}
    </>
  )
}

export default function ProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [definitions, setDefinitions] = useState<ProviderDefinition[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [checkingAll, setCheckingAll] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [search, setSearch] = useState('')
  const [filterDef, setFilterDef] = useState<string | undefined>()
  const [filterTag, setFilterTag] = useState<string | undefined>()

  const [sheetVisible, setSheetVisible] = useState(false)
  const [editing, setEditing] = useState<Provider | null>(null)
  const [formApi, setFormApi] = useState<any>(null)
  const [submitting, setSubmitting] = useState(false)
  const [activeTab, setActiveTab] = useState('basic')

  const [checkingIds, setCheckingIds] = useState<Set<string>>(new Set())
  const [syncingIds, setSyncingIds] = useState<Set<string>>(new Set())

  // 同步结果弹窗
  const [syncResult, setSyncResult] = useState<any>(null)
  const [syncProviderId, setSyncProviderId] = useState<string>('')
  const [syncVisible, setSyncVisible] = useState(false)
  const [confirmingSync, setConfirmingSync] = useState(false)
  const [selectedSyncIds, setSelectedSyncIds] = useState<string[]>([])

  // 健康检测配置弹窗
  const [checkConfigVisible, setCheckConfigVisible] = useState(false)
  const [checkConfig, setCheckConfig] = useState<Partial<HealthCheckConfig>>({})
  const [savingConfig, setSavingConfig] = useState(false)
  const [runningCheck, setRunningCheck] = useState(false)
  const [checkResult, setCheckResult] = useState<any>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getProviders({ page, pageSize, name: search || undefined, definition_id: filterDef, tag: filterTag })
      setProviders(res.data)
      setTotal(res.total)
    } catch { Toast.error('获取渠道列表失败') }
    finally { setLoading(false) }
  }, [page, pageSize, search, filterDef, filterTag])

  useEffect(() => { load() }, [load])
  useEffect(() => { getProviderDefinitions().then(setDefinitions).catch(() => {}) }, [])

  // 从当前列表提取所有 tag 选项
  const tagOptions = Array.from(new Set(providers.map(p => p.tag).filter(Boolean)))
    .map(t => ({ label: t, value: t }))

  async function handleToggle(p: Provider) {
    try {
      await toggleProviderStatus(p.id, !p.enabled)
      Toast.success(p.enabled ? '已禁用' : '已启用'); load()
    } catch { Toast.error('操作失败') }
  }

  async function handleDelete(id: string) {
    try { await deleteProvider(id); Toast.success('已删除'); load() }
    catch (e: any) {
      const code = e?.response?.data?.error?.code
      if (code === 'provider_has_models') Toast.error('该渠道下仍有模型，请先删除关联模型')
      else Toast.error('删除失败')
    }
  }

  async function handleCheck(p: Provider) {
    setCheckingIds(prev => new Set(prev).add(p.id))
    try {
      const r = await checkProvider(p.id)
      r.success ? Toast.success(`检测通过 · ${r.response_time} ms`) : Toast.warning(`检测失败：${r.error || '未知'}`)
      load()
    } catch { Toast.error('检测请求失败') }
    finally { setCheckingIds(prev => { const ns = new Set(prev); ns.delete(p.id); return ns }) }
  }

  async function handleSync(p: Provider) {
    setSyncingIds(prev => new Set(prev).add(p.id))
    try {
      const r = await syncModels(p.id)
      setSyncResult(r)
      setSyncProviderId(p.id)
      // 默认全选 added
      setSelectedSyncIds((r.added || []).map((m: any) => m.id || m))
      setSyncVisible(true)
    } catch { Toast.error('同步失败') }
    finally { setSyncingIds(prev => { const ns = new Set(prev); ns.delete(p.id); return ns }) }
  }

  async function handleConfirmSync() {
    if (!selectedSyncIds.length) { setSyncVisible(false); return }
    setConfirmingSync(true)
    try {
      const r = await confirmSync(syncProviderId, { model_ids: selectedSyncIds })
      Toast.success(`已同步 ${r.synced} 个模型`)
      setSyncVisible(false); load()
    } catch { Toast.error('确认同步失败') }
    finally { setConfirmingSync(false) }
  }

  async function openCheckConfig() {
    try {
      const cfg = await getHealthCheckConfig()
      setCheckConfig(cfg)
      setCheckResult(null)
      setCheckConfigVisible(true)
    } catch { Toast.error('加载检测配置失败') }
  }

  async function saveCheckConfig() {
    setSavingConfig(true)
    try {
      await updateHealthCheckConfig(checkConfig)
      Toast.success('配置已保存')
      setCheckConfigVisible(false)
    } catch { Toast.error('保存失败') }
    finally { setSavingConfig(false) }
  }

  async function handleRunCheckNow() {
    setRunningCheck(true)
    setCheckResult(null)
    try {
      const r = await checkAllProviders()
      setCheckResult(r)
      load()
    } catch { Toast.error('检测失败') }
    finally { setRunningCheck(false) }
  }

  /* 构建提交 payload：把动态凭证字段组装成 JSON 字符串 */
  function buildPayload(values: any): ProviderPayload {
    const def = definitions.find(d => d.id === values.definition_id)
    let credentials = ''

    if (def?.credential_schema && Object.keys(def.credential_schema).length) {
      const credObj: Record<string, string> = {}
      Object.keys(def.credential_schema).forEach(key => {
        const val = values[`cred_${key}`]
        if (val) credObj[key] = val
      })
      credentials = JSON.stringify(credObj)
    } else {
      credentials = values.credentials_raw || ''
    }

    // 编辑时凭证为空则不更新（传空串后端应忽略）
    if (editing && !credentials) credentials = ''

    const tags = Array.isArray(values.tags) ? values.tags.join(',') : (values.tags || '')

    return {
      name: values.name, definition_id: values.definition_id,
      endpoint: values.endpoint, credentials,
      weight: values.weight, priority: values.priority,
      test_model: values.test_model, remark: values.remark,
      max_retries: values.max_retries, timeout: values.timeout,
      tag: tags,
    }
  }

  async function handleSubmit(values: any) {
    setSubmitting(true)
    try {
      const payload = buildPayload(values)
      if (editing) {
        await updateProvider(editing.id, payload)
        Toast.success('渠道已更新')
      } else {
        const created = await createProvider(payload)
        Toast.success('渠道已创建')
        // 预填模型：创建成功后批量注册
        const preModels: string[] = values.selected_models || []
        const extraModels: string[] = (values.extra_models || '').split('\n').map((s: string) => s.trim()).filter(Boolean)
        const allModels = [...new Set([...preModels, ...extraModels])]
        if (allModels.length) {
          const { createModel } = await import('@/services/control-plane')
          await Promise.allSettled(allModels.map(name =>
            createModel({ name, provider_id: created.id, model_type: 'chat' })
          ))
          Toast.info(`已预填 ${allModels.length} 个模型`)
        }
      }
      setSheetVisible(false); load()
    } catch { Toast.error(editing ? '更新失败' : '创建失败') }
    finally { setSubmitting(false) }
  }

  const selectedDef = definitions.find(d => d.id === formApi?.getValue?.('definition_id'))

  const columns: ColumnProps<Provider>[] = [
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 100,
      render: (_v, record) => <StatusChip p={record} />,
    },
    {
      title: '名称 / 备注',
      dataIndex: 'name',
      render: (v, record) => (
        <Space vertical align="start" spacing={2}>
          <Text strong>{v}</Text>
          {record.remark && <Text type="tertiary" size="small">{record.remark}</Text>}
        </Space>
      ),
    },
    {
      title: '类型',
      dataIndex: 'definition_id',
      width: 140,
      render: (v) => {
        const def = definitions.find(d => d.id === v)
        return <Tag size="small">{def?.display_name || v}</Tag>
      },
    },
    {
      title: 'Endpoint',
      dataIndex: 'endpoint',
      width: 200,
      render: (v) => (
        <Tooltip content={v}>
          <Text size="small" style={{ fontFamily: 'var(--font-mono)', display: 'block', maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {v}
          </Text>
        </Tooltip>
      ),
    },
    {
      title: '标签',
      dataIndex: 'tag',
      width: 100,
      render: (v) => v ? <Tag size="small" color="blue">{v}</Tag> : <Text type="tertiary" size="small">—</Text>,
    },
    {
      title: '模型数',
      dataIndex: 'model_count',
      width: 70,
      align: 'center' as const,
      render: (v) => <Text type={v > 0 ? 'primary' : 'tertiary'} size="small" style={{ fontFamily: 'var(--font-mono)' }}>{v || 0}</Text>,
    },
    {
      title: '响应时间',
      dataIndex: 'response_time',
      width: 100,
      render: (v) => <RespTime ms={v} />,
    },
    {
      title: '失败',
      dataIndex: 'consecutive_failures',
      width: 60,
      align: 'center' as const,
      render: (v) => v > 0 ? <Text type="danger" size="small">{v}</Text> : <Text type="tertiary" size="small">—</Text>,
    },
    {
      title: '操作',
      width: 240,
      render: (_v, record) => (
        <div className={s.actions}>
          <Button size="small" theme="borderless" type="tertiary"
            icon={checkingIds.has(record.id) ? <Spin size="small" /> : <IconRefresh />}
            disabled={checkingIds.has(record.id)}
            onClick={() => handleCheck(record)}>检测</Button>
          <Button size="small" theme="borderless" type="tertiary"
            icon={syncingIds.has(record.id) ? <Spin size="small" /> : <IconSync />}
            disabled={syncingIds.has(record.id)}
            onClick={() => handleSync(record)}>同步</Button>
          <Button size="small" theme="borderless" type="tertiary"
            icon={<IconEdit />}
            onClick={() => { setEditing(record); setActiveTab('basic'); setSheetVisible(true) }}>编辑</Button>
          <Button size="small" theme="borderless"
            type={record.enabled ? 'warning' : 'primary'}
            icon={record.enabled ? <IconPause /> : <IconPlay />}
            onClick={() => handleToggle(record)}>
            {record.enabled ? '禁用' : '启用'}
          </Button>
          <Popconfirm title="确认删除该渠道？" content="删除后无法恢复，且渠道下的模型将一并删除。"
            onConfirm={() => handleDelete(record.id)}>
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
          <h1 className={s.pageTitle}>渠道管理</h1>
          <p className={s.pageDesc}>管理上游 LLM API 渠道及其健康状态</p>
        </div>
        <div className={s.pageHeaderActions}>
          <Tooltip content="健康检测配置">
            <Button icon={<IconSetting />} onClick={openCheckConfig} />
          </Tooltip>
          <Button icon={<IconRefresh />} loading={checkingAll}
            onClick={async () => {
              setCheckingAll(true)
              try { const r = await checkAllProviders(); Toast.success(`批量检测完成：${r.success}/${r.total} 通过`) }
              catch { Toast.error('批量检测失败') }
              finally { setCheckingAll(false); load() }
            }}>批量检测</Button>
          <Button icon={<IconPlus />} theme="solid" type="primary"
            onClick={() => { setEditing(null); setActiveTab('basic'); setSheetVisible(true) }}>
            添加渠道
          </Button>
        </div>
      </div>

      <div className={s.toolbar}>
        <Input prefix={<IconSearch />} placeholder="搜索渠道名称..." value={search}
          onChange={v => { setSearch(v); setPage(1) }} style={{ width: 200 }} showClear />
        <Select placeholder="所有类型" value={filterDef}
          onChange={v => { setFilterDef(v as string); setPage(1) }}
          style={{ width: 150 }}
          optionList={definitions.map(d => ({ label: d.display_name, value: d.id }))} showClear />
        <Select placeholder="按标签筛选" value={filterTag}
          onChange={v => { setFilterTag(v as string); setPage(1) }}
          style={{ width: 130 }}
          optionList={tagOptions} showClear />
        <Button icon={<IconRefresh />} theme="borderless" type="tertiary" onClick={load} loading={loading} />
        <div className={s.toolbarSpacer} />
        <Text type="tertiary" size="small">共 {total} 条</Text>
      </div>

      <div className={s.tableCard}>
        <Table columns={columns} dataSource={providers} loading={loading} rowKey="id"
          pagination={{
            total, currentPage: page, pageSize, onPageChange: setPage,
            showTotal: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageSizeChange: (size) => { setPageSize(size); setPage(1) },
          }}
          empty={
            <div className={s.emptyState}>
              <div className={s.emptyIcon}>🔌</div>
              <div className={s.emptyTitle}>暂无渠道</div>
              <div className={s.emptyDesc}>点击「添加渠道」开始配置</div>
            </div>
          } />
      </div>

      {/* ─── 创建/编辑抽屉 ─── */}
      <SideSheet
        title={
          <Space vertical align="start" spacing={2}>
            <Title heading={5}>{editing ? '编辑渠道' : '添加渠道'}</Title>
            <Text type="tertiary" size="small">
              {editing ? '修改渠道配置' : '填写信息以接入上游 LLM API'}
            </Text>
          </Space>
        }
        visible={sheetVisible} onCancel={() => setSheetVisible(false)} width={540}
        footer={
          <Space>
            <Button onClick={() => setSheetVisible(false)}>取消</Button>
            <Button type="primary" theme="solid" loading={submitting} onClick={() => formApi?.submitForm()}>
              {editing ? '保存更改' : '创建渠道'}
            </Button>
          </Space>
        }
      >
        <Form getFormApi={api => setFormApi(api)} onSubmit={handleSubmit}
          initValues={editing ? {
            name: editing.name, definition_id: editing.definition_id,
            endpoint: editing.endpoint,
            weight: editing.weight, priority: editing.priority,
            test_model: editing.test_model, remark: editing.remark,
            max_retries: editing.max_retries, timeout: editing.timeout,
            tags: editing.tag ? editing.tag.split(',') : [],
          } : {
            weight: 1, priority: 0, max_retries: 3, timeout: 30,
          }}
          layout="vertical">
          <Tabs activeKey={activeTab} onChange={setActiveTab} type="line">
            {/* ─── 基本信息 ─── */}
            <TabPane tab="基本信息" itemKey="basic">
              <div style={{ paddingTop: 12 }}>
                <Form.Input field="name" label="渠道名称" rules={[{ required: true, message: '请输入名称' }]} />
                <Form.Select
                  field="definition_id" label="渠道类型"
                  rules={[{ required: true, message: '请选择类型' }]}
                  disabled={!!editing}
                  onChange={(v) => {
                    const def = definitions.find(d => d.id === v)
                    if (def && !editing && !formApi?.getValue('endpoint')) {
                      formApi?.setValue('endpoint', def.base_url)
                    }
                  }}
                  optionList={definitions.map(d => ({
                    label: d.display_name,
                    value: d.id,
                    extra: d.description,
                  }))}
                  renderSelectedItem={(option: any) => option.label}
                  renderOptionItem={(option: any) => (
                    <div>
                      <Text strong size="small">{option.label}</Text>
                      {option.extra && <Text type="tertiary" size="small" style={{ display: 'block' }}>{option.extra}</Text>}
                    </div>
                  )}
                />
                <Form.Input field="endpoint" label="接口地址"
                  rules={[
                    { required: true, message: '请输入接口地址' },
                    { pattern: /^https?:\/\//, message: '必须以 http:// 或 https:// 开头' },
                  ]}
                  placeholder={selectedDef?.base_url || 'https://api.example.com'} />
                <Form.Input field="test_model" label="测试模型（可选）" />
                <Form.TagInput field="tags" label="标签（可多个）" placeholder="输入后回车添加" />
                <Form.TextArea field="remark" label="备注" autosize={{ minRows: 2 }} />
              </div>
            </TabPane>

            {/* ─── 凭证 ─── */}
            <TabPane tab="凭证" itemKey="credentials">
              <div style={{ paddingTop: 12 }}>
                {editing && (
                  <Banner type="info" description="留空则保持原有凭证不变" closeIcon={null} style={{ marginBottom: 12 }} />
                )}
                {selectedDef ? (
                  <CredentialFields schema={selectedDef.credential_schema} />
                ) : (
                  <Text type="tertiary">请先在「基本信息」选择渠道类型</Text>
                )}
              </div>
            </TabPane>

            {/* ─── 高级设置 ─── */}
            <TabPane tab="高级设置" itemKey="advanced">
              <div style={{ paddingTop: 12 }}>
                <div className={s.twoCol}>
                  <Form.InputNumber field="weight" label="权重" min={0} max={100} />
                  <Form.InputNumber field="priority" label="优先级" min={0} />
                  <Form.InputNumber field="max_retries" label="最大重试次数" min={0} max={10} />
                  <Form.InputNumber field="timeout" label="超时（秒）" min={5} max={300} />
                </div>
              </div>
            </TabPane>

            {/* ─── 预填模型（仅创建时）─── */}
            {!editing && (
              <TabPane tab="预填模型" itemKey="models">
                <div style={{ paddingTop: 12 }}>
                  <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 12 }}>
                    创建渠道后，系统将自动为下方勾选的模型名注册模型实例。
                  </Text>
                  {selectedDef?.default_models?.length ? (
                    <Form.CheckboxGroup field="selected_models" label="默认模型"
                      direction="vertical"
                      initValue={selectedDef.default_models}
                      options={selectedDef.default_models.map(m => ({ label: m, value: m }))} />
                  ) : (
                    <Text type="tertiary" size="small">该类型无默认模型列表</Text>
                  )}
                  <Form.TextArea field="extra_models" label="额外模型（每行一个）"
                    autosize={{ minRows: 3 }} placeholder="手动输入模型名，每行一个" style={{ marginTop: 12 }} />
                </div>
              </TabPane>
            )}
          </Tabs>
        </Form>
      </SideSheet>

      {/* ─── 同步结果弹窗 ─── */}
      <Modal
        title={<Title heading={5}>模型同步预览</Title>}
        visible={syncVisible} onCancel={() => setSyncVisible(false)}
        onOk={handleConfirmSync}
        okButtonProps={{ loading: confirmingSync, disabled: !selectedSyncIds.length }}
        okText={`同步选中 (${selectedSyncIds.length})`} width={560}
      >
        {syncResult && (
          <div>
            <Space spacing={16} style={{ marginBottom: 16 }}>
              <Text size="small">上游共 <Text strong>{syncResult.total_upstream}</Text> 个模型</Text>
              <Text size="small">本地已有 <Text strong>{syncResult.total_local}</Text> 个</Text>
            </Space>
            {syncResult.added?.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <Text strong size="small" style={{ display: 'block', marginBottom: 6 }}>
                  新增模型（{syncResult.added.length}）
                </Text>
                {syncResult.added.map((m: any) => (
                  <Checkbox key={m.id || m}
                    checked={selectedSyncIds.includes(m.id || m)}
                    onChange={e => {
                      const id = m.id || m
                      setSelectedSyncIds(prev => e.target.checked ? [...prev, id] : prev.filter(x => x !== id))
                    }}
                    style={{ display: 'block', marginBottom: 4 }}>
                    <Text size="small" style={{ fontFamily: 'var(--font-mono)' }}>{m.id || m}</Text>
                  </Checkbox>
                ))}
              </div>
            )}
            {syncResult.existing?.length > 0 && (
              <div style={{ marginBottom: 8 }}>
                <Text type="tertiary" size="small">已存在 {syncResult.existing.length} 个模型（不重复同步）</Text>
              </div>
            )}
            {syncResult.removed?.length > 0 && (
              <Banner type="warning"
                description={`${syncResult.removed.length} 个模型在上游已不存在`}
                closeIcon={null} />
            )}
            {syncResult.unsupported && (
              <Banner type="info" description="该渠道类型不支持自动同步" closeIcon={null} />
            )}
          </div>
        )}
      </Modal>

      {/* ─── 健康检测配置弹窗 ─── */}
      <Modal
        title={<Title heading={5}>健康检测配置</Title>}
        visible={checkConfigVisible} onCancel={() => setCheckConfigVisible(false)}
        footer={
          <Space>
            <Button onClick={() => setCheckConfigVisible(false)}>关闭</Button>
            <Button loading={runningCheck} onClick={handleRunCheckNow}>立即检测一次</Button>
            <Button type="primary" theme="solid" loading={savingConfig} onClick={saveCheckConfig}>保存配置</Button>
          </Space>
        }
        width={520}
      >
        {checkResult && (
          <Banner type={checkResult.failed > 0 ? 'warning' : 'success'}
            description={`检测完成：成功 ${checkResult.success} / 失败 ${checkResult.failed}${checkResult.auto_disabled ? ` / 自动禁用 ${checkResult.auto_disabled}` : ''}`}
            closeIcon={null} style={{ marginBottom: 16 }} />
        )}
        <Form layout="vertical"
          initValues={checkConfig}
          onValueChange={vals => setCheckConfig(prev => ({ ...prev, ...vals }))}>
          <Form.Switch field="enabled" label="启用自动检测" />
          <Form.Select field="interval_minutes" label="检测间隔"
            optionList={[
              { label: '1 分钟', value: 1 },
              { label: '5 分钟', value: 5 },
              { label: '10 分钟', value: 10 },
              { label: '30 分钟', value: 30 },
              { label: '60 分钟', value: 60 },
            ]} />
          <Form.Select field="check_mode" label="检测模式"
            optionList={[
              { label: '仅连通性（GET /models）', value: 'connectivity' },
              { label: '仅转发检测（实际对话请求）', value: 'model_forward' },
              { label: '两者都做', value: 'both' },
            ]} />
          <Form.InputNumber field="max_failures" label="自动禁用阈值（连续失败次数）" min={1} max={20} />
        </Form>
      </Modal>
    </div>
  )
}
