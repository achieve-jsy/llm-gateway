'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  Table, Button, Input, Switch, Toast, Popconfirm, Space, Typography,
  Form, SideSheet, Modal, Select, Tag, Skeleton, InputNumber,
} from '@douyinfe/semi-ui'
import { IconSearch, IconPlus, IconEdit, IconDelete, IconRefresh } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import { groupsApi } from '@/api/groups'
import { checkGroup, checkAllGroups } from '@/services/control-plane'
import type { Group, GroupPayload } from '@/types/group'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

// 真实 API 字段
interface GroupFull extends Group {
  strategy: string
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
}

interface GroupModel {
  id: number
  group_id: number
  model_id: string
  model_name: string
  priority: number | null
  weight: number | null
  created_at: string
}

const STRATEGY_MAP: Record<string, { label: string; color: 'blue' | 'green' | 'orange' }> = {
  round_robin: { label: '轮询',   color: 'blue' },
  priority:    { label: '优先级', color: 'green' },
  weighted:    { label: '加权',   color: 'orange' },
}

function fmtDate(v: string) {
  if (!v) return '—'
  return new Date(v).toLocaleDateString('zh-CN')
}

export default function GroupsPage() {
  const [groups, setGroups] = useState<GroupFull[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [search, setSearch] = useState('')
  const [sheetVisible, setSheetVisible] = useState(false)
  const [editing, setEditing] = useState<GroupFull | null>(null)
  const [formApi, setFormApi] = useState<any>(null)
  const [submitting, setSubmitting] = useState(false)
  const [checkingIds, setCheckingIds] = useState<Set<number>>(new Set())
  const [checkingAll, setCheckingAll] = useState(false)
  const [defaults, setDefaults] = useState<{ max_switches: number; same_model_retries: number; failover_cooldown_seconds: number } | null>(null)

  // 模型管理
  const [modelModalVisible, setModelModalVisible] = useState(false)
  const [editingGroup, setEditingGroup] = useState<GroupFull | null>(null)
  const [groupModels, setGroupModels] = useState<GroupModel[]>([])
  const [allModels, setAllModels] = useState<Array<{ id: string; name: string }>>([])
  const [modelLoading, setModelLoading] = useState(false)
  const [addingModelIds, setAddingModelIds] = useState<string[]>([])
  const [modelSaving, setModelSaving] = useState(false)
  // 优先级/权重编辑
  const [modelConfigs, setModelConfigs] = useState<Record<string, { priority?: number; weight?: number }>>({})

  const load = useCallback(async () => {
    setLoading(true); setError(false)
    try {
      const res = await groupsApi.listGroups({ page, pageSize, name: search || undefined })
      setGroups(res.data as GroupFull[])
      setTotal(res.total)
    } catch { setError(true); Toast.error('获取模型组失败') }
    finally { setLoading(false) }
  }, [page, pageSize, search])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    groupsApi.getGroupDefaults().then(d => setDefaults(d)).catch(() => {})
  }, [])

  async function handleToggle(g: GroupFull, enabled: boolean) {
    setGroups(prev => prev.map(r => r.id === g.id ? { ...r, enabled } : r))
    try { await groupsApi.updateGroup(g.id, { enabled } as GroupPayload) }
    catch {
      setGroups(prev => prev.map(r => r.id === g.id ? { ...r, enabled: !enabled } : r))
      Toast.error('操作失败')
    }
  }

  async function handleDelete(id: number) {
    try { await groupsApi.deleteGroup(id); Toast.success('已删除'); load() }
    catch { Toast.error('删除失败') }
  }

  async function handleCheck(g: GroupFull) {
    setCheckingIds(p => new Set(p).add(g.id))
    try {
      const r = await checkGroup(g.id)
      r.success ? Toast.success(`检测通过 · ${r.response_time} ms`) : Toast.warning(`检测失败：${r.error || '未知'}`)
    } catch { Toast.error('检测失败') }
    finally { setCheckingIds(p => { const ns = new Set(p); ns.delete(g.id); return ns }) }
  }

  async function handleSubmit(values: any) {
    setSubmitting(true)
    try {
      const payload: any = {
        name: values.name, description: values.description,
        enabled: values.enabled ?? true,
        strategy: values.strategy || 'round_robin',
        max_switches: values.max_switches,
        same_model_retries: values.same_model_retries,
        failover_cooldown_seconds: values.failover_cooldown_seconds,
      }
      editing ? await groupsApi.updateGroup(editing.id, payload) : await groupsApi.createGroup(payload)
      Toast.success(editing ? '已更新' : '已创建')
      setSheetVisible(false); load()
    } catch { Toast.error(editing ? '更新失败' : '创建失败') }
    finally { setSubmitting(false) }
  }

  async function openModelModal(g: GroupFull) {
    setEditingGroup(g)
    setModelModalVisible(true)
    setModelLoading(true)
    setModelConfigs({})
    try {
      const [models, all] = await Promise.all([
        groupsApi.listGroupModels(g.id),
        groupsApi.listAllModels(),
      ])
      // listGroupModels 返回带 priority/weight 的数据
      const gms = models as any[] as GroupModel[]
      setGroupModels(gms)
      // 初始化配置
      const cfg: Record<string, { priority?: number; weight?: number }> = {}
      gms.forEach(m => { cfg[m.model_id] = { priority: m.priority ?? undefined, weight: m.weight ?? undefined } })
      setModelConfigs(cfg)
      setAllModels(all.map(m => ({ id: m.id, name: m.name })))
    } catch { Toast.error('加载模型失败') }
    finally { setModelLoading(false) }
  }

  async function handleAddModels() {
    if (!editingGroup || !addingModelIds.length) return
    setModelSaving(true)
    try {
      await groupsApi.addModelsToGroup(editingGroup.id, addingModelIds)
      Toast.success(`已添加 ${addingModelIds.length} 个模型`)
      setAddingModelIds([])
      const updated = await groupsApi.listGroupModels(editingGroup.id) as any[] as GroupModel[]
      setGroupModels(updated)
    } catch { Toast.error('添加失败') }
    finally { setModelSaving(false) }
  }

  async function handleRemoveModel(modelId: string) {
    if (!editingGroup) return
    try {
      await groupsApi.removeModelFromGroup(editingGroup.id, modelId)
      setGroupModels(prev => prev.filter(m => m.model_id !== modelId))
      Toast.success('已移除')
    } catch { Toast.error('移除失败') }
  }

  const addableModels = allModels.filter(m => !groupModels.some(gm => gm.model_id === m.id))
  const currentStrategy = editing?.strategy || formApi?.getValue?.('strategy') || 'round_robin'

  const columns: ColumnProps<GroupFull>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (v, record) => (
        <Space vertical align="start" spacing={2}>
          <Text strong>{v}</Text>
          {record.description && <Text type="tertiary" size="small">{record.description}</Text>}
        </Space>
      ),
    },
    {
      title: '路由策略',
      dataIndex: 'strategy',
      width: 100,
      render: (v) => {
        const info = STRATEGY_MAP[v] || { label: v, color: 'blue' as const }
        return <Tag color={info.color} size="small">{info.label}</Tag>
      },
    },
    {
      title: '模型数',
      dataIndex: 'model_count',
      width: 70,
      align: 'center' as const,
      render: (v) => <Text type={v > 0 ? 'primary' : 'tertiary'} size="small">{v || 0}</Text>,
    },
    {
      title: '最大切换',
      dataIndex: 'max_switches',
      width: 80,
      align: 'center' as const,
      render: (v) => <Text size="small">{v}</Text>,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 70,
      align: 'center' as const,
      render: (v, record) => (
        <Switch checked={v} size="small" onChange={checked => handleToggle(record, checked)} />
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 110,
      render: (v) => <Text type="tertiary" size="small">{fmtDate(v)}</Text>,
    },
    {
      title: '操作',
      width: 240,
      render: (_v, record) => (
        <div className={s.actions}>
          <Button size="small" theme="borderless" type="tertiary"
            loading={checkingIds.has(record.id)}
            onClick={() => handleCheck(record)}>检测</Button>
          <Button size="small" theme="borderless" type="tertiary"
            onClick={() => openModelModal(record)}>管理模型</Button>
          <Button size="small" theme="borderless" type="tertiary" icon={<IconEdit />}
            onClick={() => { setEditing(record); setSheetVisible(true) }}>编辑</Button>
          <Popconfirm title="确认删除该模型组？" onConfirm={() => handleDelete(record.id)}>
            <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} />
          </Popconfirm>
        </div>
      ),
    },
  ]

  const initValues = editing
    ? {
        name: editing.name, description: editing.description, enabled: editing.enabled,
        strategy: editing.strategy,
        max_switches: editing.max_switches,
        same_model_retries: editing.same_model_retries,
        failover_cooldown_seconds: editing.failover_cooldown_seconds,
      }
    : {
        enabled: true, strategy: 'round_robin',
        max_switches: defaults?.max_switches ?? 2,
        same_model_retries: defaults?.same_model_retries ?? 1,
        failover_cooldown_seconds: defaults?.failover_cooldown_seconds ?? 60,
      }

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>模型组</h1>
          <p className={s.pageDesc}>配置路由策略与故障转移规则</p>
        </div>
        <div className={s.pageHeaderActions}>
          <Button loading={checkingAll} icon={<IconRefresh />}
            onClick={async () => {
              setCheckingAll(true)
              try { const r = await checkAllGroups(); Toast.success(`检测完成：${r.success}/${r.total}`) }
              catch { Toast.error('检测失败') }
              finally { setCheckingAll(false) }
            }}>批量检测</Button>
          <Button icon={<IconPlus />} theme="solid" type="primary"
            onClick={() => { setEditing(null); setSheetVisible(true) }}>
            创建模型组
          </Button>
        </div>
      </div>

      <div className={s.toolbar}>
        <Input prefix={<IconSearch />} placeholder="搜索模型组名称..."
          value={search} onChange={v => { setSearch(v); setPage(1) }}
          style={{ width: 220 }} showClear />
        <Button icon={<IconRefresh />} theme="borderless" type="tertiary" onClick={load} loading={loading} />
        <div className={s.toolbarSpacer} />
        <Text type="tertiary" size="small">共 {total} 条</Text>
      </div>

      {error ? (
        <div className={s.emptyState}>
          <div className={s.emptyIcon}>⚠️</div>
          <div className={s.emptyTitle}>加载失败</div>
          <Button style={{ marginTop: 12 }} onClick={load}>重新加载</Button>
        </div>
      ) : (
        <div className={s.tableCard}>
          <Table columns={columns} dataSource={groups} loading={loading} rowKey="id"
            pagination={{ total, currentPage: page, pageSize, onPageChange: setPage, showTotal: true }}
            empty={
              <div className={s.emptyState}>
                <div className={s.emptyIcon}>🗂️</div>
                <div className={s.emptyTitle}>暂无模型组</div>
                {search && <div className={s.emptyDesc}>未找到包含 "{search}" 的模型组</div>}
              </div>
            } />
        </div>
      )}

      {/* 编辑抽屉 */}
      <SideSheet
        title={
          <Space vertical align="start" spacing={2}>
            <Title heading={5}>{editing ? '编辑模型组' : '创建模型组'}</Title>
            {defaults && !editing && (
              <Text type="tertiary" size="small">
                全局默认：最大切换 {defaults.max_switches} 次 / 同模型重试 {defaults.same_model_retries} 次 / 冷却 {defaults.failover_cooldown_seconds}s
              </Text>
            )}
          </Space>
        }
        visible={sheetVisible} onCancel={() => setSheetVisible(false)} width={480}
        footer={
          <Space>
            <Button onClick={() => setSheetVisible(false)}>取消</Button>
            <Button type="primary" theme="solid" loading={submitting} onClick={() => formApi?.submitForm()}>
              {editing ? '保存更改' : '创建'}
            </Button>
          </Space>
        }
      >
        <Form getFormApi={api => setFormApi(api)} onSubmit={handleSubmit}
          initValues={initValues} layout="vertical">
          <Form.Input field="name" label="组名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Select field="strategy" label="路由策略"
            optionList={[
              { label: '轮询（Round Robin）', value: 'round_robin' },
              { label: '优先级（Priority）', value: 'priority' },
              { label: '加权（Weighted）', value: 'weighted' },
            ]} />
          <div className={s.twoCol}>
            <div>
              <label className={s.fieldLabel}>最大切换次数</label>
              <Form.InputNumber field="max_switches" noLabel min={1} max={10} />
            </div>
            <div>
              <label className={s.fieldLabel}>同模型重试次数</label>
              <Form.InputNumber field="same_model_retries" noLabel min={0} max={5} />
            </div>
            <div>
              <label className={s.fieldLabel}>冷却时间（秒）</label>
              <Form.InputNumber field="failover_cooldown_seconds" noLabel min={10} max={600} />
            </div>
          </div>
          <Form.Switch field="enabled" label="启用" />
          <Form.TextArea field="description" label="描述" autosize={{ minRows: 2 }} />
        </Form>
      </SideSheet>

      {/* 模型管理弹窗 */}
      <Modal title={<Title heading={5}>管理组内模型</Title>}
        visible={modelModalVisible}
        onCancel={() => { setModelModalVisible(false); setAddingModelIds([]) }}
        footer={null} width={620}>
        {modelLoading ? (
          <Skeleton active placeholder={<Skeleton.Paragraph rows={4} />} loading />
        ) : (
          <div>
            {/* 已加入模型 + 优先级/权重编辑 */}
            <Text strong size="small" style={{ display: 'block', marginBottom: 8 }}>
              已加入模型（{groupModels.length}）
            </Text>
            {groupModels.length === 0 ? (
              <Text type="tertiary" size="small">暂无模型</Text>
            ) : (
              <Table
                size="small"
                rowKey="model_id"
                dataSource={groupModels}
                pagination={false}
                columns={[
                  {
                    title: '模型名称',
                    dataIndex: 'model_name',
                    render: (v) => <Text size="small" style={{ fontFamily: 'var(--font-mono)' }}>{v}</Text>,
                  },
                  ...(currentStrategy === 'priority' ? [{
                    title: '优先级',
                    dataIndex: 'priority',
                    width: 100,
                    render: (_v: any, record: GroupModel) => (
                      <InputNumber
                        value={modelConfigs[record.model_id]?.priority ?? undefined}
                        onChange={v => setModelConfigs(prev => ({ ...prev, [record.model_id]: { ...prev[record.model_id], priority: v as number } }))}
                        min={0} size="small" style={{ width: 80 }} />
                    ),
                  }] : []),
                  ...(currentStrategy === 'weighted' ? [{
                    title: '权重',
                    dataIndex: 'weight',
                    width: 100,
                    render: (_v: any, record: GroupModel) => (
                      <InputNumber
                        value={modelConfigs[record.model_id]?.weight ?? undefined}
                        onChange={v => setModelConfigs(prev => ({ ...prev, [record.model_id]: { ...prev[record.model_id], weight: v as number } }))}
                        min={0} size="small" style={{ width: 80 }} />
                    ),
                  }] : []),
                  {
                    title: '操作',
                    width: 70,
                    render: (_v: any, record: GroupModel) => (
                      <Button size="small" theme="borderless" type="danger"
                        onClick={() => handleRemoveModel(record.model_id)}>移除</Button>
                    ),
                  },
                ]} />
            )}

            {/* 添加模型 */}
            <div className={s.groupModalSection}>
              <Text strong size="small" style={{ display: 'block', marginBottom: 8 }}>添加模型</Text>
              <Space style={{ width: '100%' }}>
                <Select multiple value={addingModelIds}
                  onChange={v => setAddingModelIds(v as string[])}
                  style={{ flex: 1 }} placeholder="选择要添加的模型"
                  optionList={addableModels.map(m => ({ label: m.name, value: m.id }))} filter />
                <Button loading={modelSaving} type="primary" theme="solid"
                  disabled={!addingModelIds.length} onClick={handleAddModels}>添加</Button>
              </Space>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}
