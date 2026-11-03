'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  Table, Button, Switch, Toast, Popconfirm, Tag,
  Modal, Select, Form, Tooltip, Space, Typography,
  Banner,
} from '@douyinfe/semi-ui'
import { IconPlus, IconDelete, IconCopy } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import {
  listKeys, createKey, revealKey, enableKey, disableKey, deleteKey, resetKey,
  getAvailableGroups, updateKeyGroups, toggleAccessAllGroups,
} from '@/services/control-plane'
import type { ApiKey, BoundGroup } from '@/types/api-key'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

function maskKey(hint: string) { return 'sk-****' + hint }

function fmtDate(v?: string) {
  if (!v) return '—'
  return new Date(v).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

// 「使用密钥」说明弹窗
function UseKeyModal({ visible, keyHint, onClose }: { visible: boolean; keyHint: string; onClose: () => void }) {
  const base = typeof window !== 'undefined' ? window.location.origin : ''
  const snippets = [
    {
      label: 'cURL',
      code: `curl ${base}/v1/chat/completions \\
  -H "Authorization: Bearer <YOUR_KEY>" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'`,
    },
    {
      label: 'Python (openai)',
      code: `from openai import OpenAI
client = OpenAI(api_key="<YOUR_KEY>", base_url="${base}/v1")
resp = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello"}]
)
print(resp.choices[0].message.content)`,
    },
  ]
  return (
    <Modal title={<Title heading={5}>使用密钥接入说明</Title>}
      visible={visible} onCancel={onClose} footer={null} width={600}>
      <Text type="tertiary" size="small">密钥后缀：{keyHint}</Text>
      <div style={{ marginTop: 16 }}>
        {snippets.map(sn => (
          <div key={sn.label} style={{ marginBottom: 20 }}>
            <Text strong style={{ display: 'block', marginBottom: 6 }}>{sn.label}</Text>
            <pre style={{
              background: 'var(--color-border-subtle)', padding: '12px 16px',
              borderRadius: 'var(--radius-md)', fontFamily: 'var(--font-mono)',
              fontSize: 12, overflow: 'auto', border: '1px solid var(--color-border)',
              margin: 0, whiteSpace: 'pre-wrap',
            }}>{sn.code}</pre>
          </div>
        ))}
      </div>
    </Modal>
  )
}

export default function KeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const pageSize = 20

  const [createVisible, setCreateVisible] = useState(false)
  const [createFormApi, setCreateFormApi] = useState<any>(null)
  const [creating, setCreating] = useState(false)
  const [newKeyPlain, setNewKeyPlain] = useState<string | null>(null)
  const [togglingIds, setTogglingIds] = useState<Set<string>>(new Set())

  // 使用密钥弹窗
  const [usageKey, setUsageKey] = useState<ApiKey | null>(null)

  // 组管理
  const [groupModalKey, setGroupModalKey] = useState<ApiKey | null>(null)
  const [availableGroups, setAvailableGroups] = useState<Array<{ id: number; name: string }>>([])
  const [addGroupIds, setAddGroupIds] = useState<number[]>([])
  const [pendingAccessAll, setPendingAccessAll] = useState<boolean | null>(null)
  const [groupSaving, setGroupSaving] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await listKeys(page, pageSize)
      setKeys(r.data?.items ?? [])
      setTotal(r.data?.total ?? 0)
    } catch { Toast.error('获取密钥列表失败') }
    finally { setLoading(false) }
  }, [page])

  useEffect(() => { load() }, [load])

  async function handleToggle(key: ApiKey, enabled: boolean) {
    if (!enabled) {
      Modal.confirm({
        title: '确认禁用密钥',
        content: `禁用后密钥「${key.name}」将立即失效，确认继续？`,
        onOk: async () => {
          setTogglingIds(p => new Set(p).add(key.id))
          setKeys(prev => prev.map(k => k.id === key.id ? { ...k, enabled: false } : k))
          try { await disableKey(key.id) }
          catch {
            setKeys(prev => prev.map(k => k.id === key.id ? { ...k, enabled: true } : k))
            Toast.error('操作失败')
          } finally { setTogglingIds(p => { const s = new Set(p); s.delete(key.id); return s }) }
        },
      })
      return
    }
    setTogglingIds(p => new Set(p).add(key.id))
    setKeys(prev => prev.map(k => k.id === key.id ? { ...k, enabled } : k))
    try { await enableKey(key.id) }
    catch {
      setKeys(prev => prev.map(k => k.id === key.id ? { ...k, enabled: !enabled } : k))
      Toast.error('操作失败')
    } finally { setTogglingIds(p => { const s = new Set(p); s.delete(key.id); return s }) }
  }

  async function handleCopy(key: ApiKey) {
    try {
      const r = await revealKey(key.id)
      await navigator.clipboard.writeText(r.key)
      Toast.success('已复制到剪贴板')
    } catch { Toast.error('复制失败') }
  }

  async function handleReset(key: ApiKey) {
    Modal.confirm({
      title: '确认重置密钥',
      content: `重置后「${key.name}」的旧密钥将立即失效，请及时更新接入配置。`,
      onOk: async () => {
        try {
          const r = await resetKey(key.id)
          setNewKeyPlain(r.data?.key ?? null)
          load()
        } catch { Toast.error('重置失败') }
      },
    })
  }

  async function handleDelete(key: ApiKey) {
    try { await deleteKey(key.id); Toast.success('已删除'); load() }
    catch { Toast.error('删除失败') }
  }

  async function handleCreate(values: any) {
    setCreating(true)
    try {
      const r = await createKey(values.name)
      setNewKeyPlain(r.data?.key ?? null)
      setCreateVisible(false)
      setPage(1); load()
    } catch { Toast.error('创建失败') }
    finally { setCreating(false) }
  }

  async function openGroupModal(key: ApiKey) {
    setGroupModalKey(key)
    setPendingAccessAll(null)
    setAddGroupIds([])
    try {
      const r = await getAvailableGroups()
      setAvailableGroups(r.data?.groups ?? [])
    } catch { Toast.error('加载模型组失败') }
  }

  async function saveGroupBinding() {
    if (!groupModalKey) return
    setGroupSaving(true)
    try {
      if (pendingAccessAll !== null) await toggleAccessAllGroups(groupModalKey.id, pendingAccessAll)
      if (addGroupIds.length > 0) await updateKeyGroups(groupModalKey.id, addGroupIds, [])
      Toast.success('已保存')
      setGroupModalKey(null); load()
    } catch { Toast.error('保存失败') }
    finally { setGroupSaving(false) }
  }

  async function removeGroup(key: ApiKey, groupId: number) {
    try {
      await updateKeyGroups(key.id, [], [groupId])
      Toast.success('已移除'); load()
      const fresh = await listKeys(page, pageSize)
      const updated = (fresh.data?.items ?? []).find(k => k.id === key.id)
      if (updated) setGroupModalKey(updated)
    } catch { Toast.error('移除失败') }
  }

  const columns: ColumnProps<ApiKey>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (v) => <Text strong>{v}</Text>,
    },
    {
      title: 'API 密钥',
      dataIndex: 'key_hint',
      width: 240,
      render: (hint, record) => (
        <Space>
          <code className={s.keyCode}>{maskKey(hint)}</code>
          <Tooltip content="复制完整密钥">
            <Button size="small" theme="borderless" type="tertiary"
              icon={<IconCopy />} onClick={() => handleCopy(record)} />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: '可访问模型组',
      dataIndex: 'access_all_groups',
      width: 240,
      render: (accessAll, record) => {
        const groups = record.bound_groups || []
        return (
          <Space wrap spacing={4}>
            {accessAll ? (
              <Tag color="green" size="small">全部模型组</Tag>
            ) : groups.length === 0 ? (
              <Text type="tertiary" size="small">未分配</Text>
            ) : (
              groups.slice(0, 2).map((g: BoundGroup) => (
                <Tag key={g.id} size="small" color="blue">{g.name}</Tag>
              ))
            )}
            {groups.length > 2 && <Text type="tertiary" size="small">+{groups.length - 2}</Text>}
            <Button size="small" theme="borderless" type="primary"
              onClick={() => openGroupModal(record)}>
              {groups.length === 0 && !accessAll ? '分配' : '管理'}
            </Button>
          </Space>
        )
      },
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 80,
      align: 'center' as const,
      render: (v, record) => (
        <Switch checked={v} size="small"
          disabled={togglingIds.has(record.id)}
          onChange={checked => handleToggle(record, checked)} />
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 150,
      render: (v) => <Text type="tertiary" size="small">{fmtDate(v)}</Text>,
    },
    {
      title: '最后使用',
      dataIndex: 'last_used_at',
      width: 150,
      render: (v) => <Text type="tertiary" size="small">{fmtDate(v)}</Text>,
    },
    {
      title: '操作',
      width: 200,
      render: (_v, record) => (
        <Space>
          <Button size="small" theme="borderless" type="tertiary"
            onClick={() => setUsageKey(record)}>使用密钥</Button>
          <Button size="small" theme="borderless" type="tertiary"
            onClick={() => handleReset(record)}>重置</Button>
          <Popconfirm title={`确认删除密钥「${record.name}」？`}
            content="删除后不可恢复。" onConfirm={() => handleDelete(record)}>
            <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>API Keys</h1>
          <p className={s.pageDesc}>管理访问密钥，控制模型组权限</p>
        </div>
        <Button icon={<IconPlus />} theme="solid" type="primary"
          onClick={() => setCreateVisible(true)}>
          创建密钥
        </Button>
      </div>

      <div className={s.tableCard}>
        <Table columns={columns} dataSource={keys} loading={loading} rowKey="id"
          pagination={total > pageSize ? { total, currentPage: page, pageSize, onPageChange: setPage, showTotal: true } : false}
          empty={
            <div className={s.emptyState}>
              <div className={s.emptyIcon}>🔑</div>
              <div className={s.emptyTitle}>暂无密钥</div>
              <div className={s.emptyDesc}>点击「创建密钥」开始</div>
            </div>
          } />
      </div>

      {/* 创建密钥 */}
      <Modal title={<Title heading={5}>创建 API 密钥</Title>}
        visible={createVisible} onCancel={() => setCreateVisible(false)}
        onOk={() => createFormApi?.submitForm()}
        okButtonProps={{ loading: creating }} okText="创建" width={400}>
        <Form getFormApi={api => setCreateFormApi(api)} onSubmit={handleCreate} layout="vertical">
          <Form.Input field="name" label="密钥名称" rules={[{ required: true, message: '请输入名称' }]}
            placeholder="如：开发测试密钥" />
        </Form>
      </Modal>

      {/* 新密钥展示 */}
      <Modal title={<Title heading={5}>密钥已生成</Title>}
        visible={!!newKeyPlain} onCancel={() => setNewKeyPlain(null)}
        footer={
          <Space>
            <Button icon={<IconCopy />}
              onClick={async () => { if (newKeyPlain) { await navigator.clipboard.writeText(newKeyPlain); Toast.success('已复制') } }}>
              复制密钥
            </Button>
            <Button type="primary" theme="solid" onClick={() => setNewKeyPlain(null)}>我已保存</Button>
          </Space>
        }
        width={480}>
        <div className={s.newKeyWarning}>⚠ 请立即保存，此密钥只显示一次，关闭后无法找回。</div>
        <code className={s.newKeyCode}>{newKeyPlain}</code>
      </Modal>

      {/* 使用密钥说明 */}
      <UseKeyModal
        visible={!!usageKey}
        keyHint={usageKey?.key_hint || ''}
        onClose={() => setUsageKey(null)}
      />

      {/* 管理模型组 */}
      <Modal title={<Title heading={5}>管理可访问模型组</Title>}
        visible={!!groupModalKey} onCancel={() => setGroupModalKey(null)}
        onOk={saveGroupBinding} okButtonProps={{ loading: groupSaving }} okText="保存" width={480}>
        {groupModalKey && (
          <div>
            <div className={s.accessAllRow}>
              <div>
                <Text strong>访问全部模型组</Text>
                <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 2 }}>
                  开启后可访问所有现有和未来新建的模型组
                </Text>
              </div>
              <Switch
                checked={pendingAccessAll !== null ? pendingAccessAll : groupModalKey.access_all_groups}
                onChange={v => setPendingAccessAll(v)}
              />
            </div>

            {!(pendingAccessAll ?? groupModalKey.access_all_groups) && (
              <div>
                <Text strong size="small" style={{ display: 'block', marginBottom: 8 }}>
                  当前绑定（{groupModalKey.bound_groups?.length || 0}）
                </Text>
                <Space wrap spacing={6} style={{ marginBottom: 16 }}>
                  {(groupModalKey.bound_groups || []).length === 0
                    ? <Text type="tertiary" size="small">暂未绑定</Text>
                    : (groupModalKey.bound_groups || []).map(g => (
                      <Tag key={g.id} closable onClose={() => removeGroup(groupModalKey, g.id)}>{g.name}</Tag>
                    ))}
                </Space>
                <div className={s.groupModalSection}>
                  <Text strong size="small" style={{ display: 'block', marginBottom: 8 }}>添加模型组</Text>
                  <Select multiple value={addGroupIds}
                    onChange={v => setAddGroupIds(v as number[])}
                    style={{ width: '100%' }} placeholder="选择要绑定的模型组"
                    optionList={availableGroups
                      .filter(g => !(groupModalKey.bound_groups || []).some(bg => bg.id === g.id))
                      .map(g => ({ label: g.name, value: g.id }))}
                    filter />
                </div>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}
