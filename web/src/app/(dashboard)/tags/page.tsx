'use client'

import { useState, useEffect } from 'react'
import { Button, Toast, Popconfirm, Form, Modal, Typography, Space, Tag, Skeleton } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { getModelTags, createModelTag, updateModelTag, deleteModelTag } from '@/services/control-plane'
import type { ModelTag, ModelTagPayload } from '@/types/model-tag'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

const PRESET_COLORS = [
  '#2563EB', '#7C3AED', '#16A34A', '#DC2626', '#CA8A04',
  '#0891B2', '#BE185D', '#EA580C', '#65A30D', '#0F766E',
]

export default function TagsPage() {
  const [tags, setTags] = useState<ModelTag[]>([])
  const [loading, setLoading] = useState(false)
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ModelTag | null>(null)
  const [formApi, setFormApi] = useState<any>(null)
  const [submitting, setSubmitting] = useState(false)
  const [selectedColor, setSelectedColor] = useState(PRESET_COLORS[0])

  const load = async () => {
    setLoading(true)
    try { setTags(await getModelTags()) }
    catch { Toast.error('获取标签失败') }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  function openCreate() {
    setEditing(null)
    setSelectedColor(PRESET_COLORS[0])
    setModalVisible(true)
  }

  function openEdit(tag: ModelTag) {
    setEditing(tag)
    setSelectedColor(tag.color || PRESET_COLORS[0])
    setModalVisible(true)
  }

  async function handleDelete(id: string) {
    try { await deleteModelTag(id); Toast.success('已删除'); load() }
    catch { Toast.error('删除失败') }
  }

  async function handleSubmit(values: any) {
    setSubmitting(true)
    try {
      const payload: ModelTagPayload = { name: values.name, color: selectedColor, description: values.description }
      editing ? await updateModelTag(editing.id, payload) : await createModelTag(payload)
      Toast.success(editing ? '已更新' : '已创建')
      setModalVisible(false); load()
    } catch { Toast.error(editing ? '更新失败' : '创建失败') }
    finally { setSubmitting(false) }
  }

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>标签管理</h1>
          <p className={s.pageDesc}>为模型添加分类标签，便于筛选和管理</p>
        </div>
        <Button icon={<IconPlus />} theme="solid" type="primary" onClick={openCreate}>
          新建标签
        </Button>
      </div>

      {loading ? (
        <div className={s.tagGrid}>
          {[1, 2, 3, 4, 5, 6].map(i => (
            <Skeleton key={i} active placeholder={<Skeleton.Image style={{ height: 120, borderRadius: 8 }} />} loading />
          ))}
        </div>
      ) : tags.length === 0 ? (
        <div className={s.emptyState}>
          <div className={s.emptyIcon}>🏷️</div>
          <div className={s.emptyTitle}>暂无标签</div>
          <div className={s.emptyDesc}>点击「新建标签」开始创建</div>
        </div>
      ) : (
        <div className={s.tagGrid}>
          {tags.map(tag => (
            <div key={tag.id} className={s.tagCard}
              style={{ borderLeft: `4px solid ${tag.color || '#2563EB'}` }}>
              <div className={s.tagCardHeader}>
                <span className={s.tagColorDot} style={{ background: tag.color || '#2563EB' }} />
                <Text strong style={{ flex: 1 }}>{tag.name}</Text>
                <div className={s.tagCardActions}>
                  <Button size="small" theme="borderless" type="tertiary"
                    icon={<IconEdit />} onClick={() => openEdit(tag)} />
                  <Popconfirm title="确认删除该标签？"
                    content="删除后相关模型的标签关联也将移除。"
                    onConfirm={() => handleDelete(tag.id)}>
                    <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} />
                  </Popconfirm>
                </div>
              </div>
              <p className={s.tagCardDesc}>
                {tag.description || <Text type="tertiary" size="small">暂无描述</Text>}
              </p>
              <div className={s.tagCardFooter}>
                <Text type="tertiary" size="small">排序：{(tag as any).sort_order ?? 0}</Text>
                <Tag size="small" style={{ background: tag.color + '20', color: tag.color, border: `1px solid ${tag.color}40` }}>
                  {tag.name}
                </Tag>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 新建/编辑弹窗 */}
      <Modal
        title={<Title heading={5}>{editing ? '编辑标签' : '新建标签'}</Title>}
        visible={modalVisible}
        onCancel={() => setModalVisible(false)}
        onOk={() => formApi?.submitForm()}
        okButtonProps={{ loading: submitting }}
        okText={editing ? '保存更改' : '创建标签'}
        width={440}
      >
        <Form getFormApi={api => setFormApi(api)} onSubmit={handleSubmit}
          initValues={editing ? { name: editing.name, description: editing.description } : {}}
          layout="vertical">
          <Form.Input field="name" label="标签名称"
            rules={[{ required: true, message: '请输入名称' }]}
            placeholder="如：GPT-4 系列" />

          {/* 颜色选择器 */}
          <Form.Slot label="标签颜色">
            <Space wrap spacing={8} style={{ marginBottom: 8 }}>
              {PRESET_COLORS.map(c => (
                <button key={c} onClick={() => setSelectedColor(c)}
                  type="button"
                  style={{
                    width: 28, height: 28, borderRadius: '50%', background: c,
                    border: selectedColor === c ? `3px solid ${c}` : '3px solid transparent',
                    boxShadow: selectedColor === c ? `0 0 0 2px white, 0 0 0 4px ${c}` : 'none',
                    cursor: 'pointer', padding: 0, outline: 'none', transition: 'all 0.15s',
                  }} />
              ))}
              {/* 自定义颜色 */}
              <label style={{ position: 'relative', cursor: 'pointer' }}>
                <div style={{
                  width: 28, height: 28, borderRadius: '50%',
                  background: `conic-gradient(red, yellow, lime, cyan, blue, magenta, red)`,
                  border: !PRESET_COLORS.includes(selectedColor) ? `3px solid ${selectedColor}` : '3px solid transparent',
                  boxShadow: !PRESET_COLORS.includes(selectedColor) ? `0 0 0 2px white, 0 0 0 4px ${selectedColor}` : 'none',
                  transition: 'all 0.15s',
                }} />
                <input type="color" value={selectedColor} onChange={e => setSelectedColor(e.target.value)}
                  style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }} />
              </label>
            </Space>
            <Space>
              <Tag size="small" style={{ background: selectedColor + '20', color: selectedColor, border: `1px solid ${selectedColor}40` }}>
                预览标签
              </Tag>
              <Text type="tertiary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>
                {selectedColor}
              </Text>
            </Space>
          </Form.Slot>

          <Form.TextArea field="description" label="描述（可选）"
            autosize={{ minRows: 2 }} placeholder="描述该标签的用途" />
        </Form>
      </Modal>
    </div>
  )
}
