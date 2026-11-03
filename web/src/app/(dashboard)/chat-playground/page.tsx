'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { Button, Select, Switch, Input, InputNumber, Slider, Toast, Spin, Tag, Collapse, Typography, TextArea, Form } from '@douyinfe/semi-ui'
import { IconDelete, IconSend } from '@douyinfe/semi-icons'
import { getModels } from '@/services/control-plane'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

interface Message {
  role: 'user' | 'assistant'
  content: string
  reasoning_content?: string
}

function fmtTokens(n?: number) {
  if (!n) return ''
  if (n >= 1000000) return `${(n / 1000000).toFixed(0)}M ctx`
  if (n >= 1000) return `${(n / 1000).toFixed(0)}K ctx`
  return `${n} ctx`
}

export default function ChatPlaygroundPage() {
  const [mode, setMode] = useState<'session' | 'apikey'>('session')
  const [models, setModels] = useState<Array<{
    id: string; name: string; upstream_model_id?: string
    context_length?: number; max_output_tokens?: number; provider?: { name: string }
  }>>([])
  const [gatewayModels, setGatewayModels] = useState<string[]>([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [config, setConfig] = useState({
    model: '', apiBase: '', apiKey: '',
    temperature: 0.7, topP: 1.0, maxTokens: 2048,
    stop: '', systemPrompt: '', stream: true,
  })
  const [messages, setMessages] = useState<Message[]>([])
  const [userInput, setUserInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const chatRef = useRef<HTMLDivElement>(null)

  const loadModels = useCallback(async () => {
    setLoadingModels(true)
    try {
      if (mode === 'session') {
        const r = await getModels(undefined, { pageSize: 200 })
        const chatModels = r.data.filter(m => m.model_type === 'chat' && m.enabled)
        setModels(chatModels.map(m => ({
          id: m.id, name: m.name, upstream_model_id: m.upstream_model_id,
          context_length: m.context_length, max_output_tokens: m.max_output_tokens,
          provider: m.provider,
        })))
        // 自动选第一个
        if (!config.model && chatModels.length > 0) {
          setConfig(p => ({ ...p, model: chatModels[0].name }))
        }
      } else if (config.apiKey) {
        const base = config.apiBase || '/v1'
        const r = await fetch(`${base}/models`, {
          headers: { Authorization: `Bearer ${config.apiKey}` },
        })
        const data = await r.json()
        setGatewayModels((data.data || []).map((m: any) => m.id))
      }
    } catch { Toast.error('获取模型列表失败') }
    finally { setLoadingModels(false) }
  }, [mode, config.apiKey, config.apiBase])

  useEffect(() => { loadModels() }, [mode])

  useEffect(() => {
    if (chatRef.current) chatRef.current.scrollTop = chatRef.current.scrollHeight
  }, [messages])

  function setC<K extends keyof typeof config>(key: K, val: (typeof config)[K]) {
    setConfig(p => ({ ...p, [key]: val }))
  }

  function validate(): string | null {
    if (!config.model) return '请先选择模型'
    if (config.temperature < 0 || config.temperature > 2) return '温度需在 0~2 之间'
    if (config.topP < 0 || config.topP > 1) return 'Top P 需在 0~1 之间'
    if (config.maxTokens < 1 || config.maxTokens > 128000) return '最大 Token 需在 1~128000 之间'
    if (mode === 'apikey' && !config.apiKey) return '请输入 API Key'
    return null
  }

  const modelOptions = mode === 'session'
    ? models.map(m => ({
        label: m.provider?.name ? `${m.name} (${m.provider.name})` : m.name,
        value: m.name,
      }))
    : gatewayModels.map(id => ({ label: id, value: id }))

  const currentModel = mode === 'session' ? models.find(m => m.name === config.model) : undefined

  async function handleSend() {
    if (!userInput.trim() || isStreaming) return
    const err = validate()
    if (err) { Toast.warning(err); return }

    const text = userInput.trim()
    setUserInput('')
    const newMessages: Message[] = [...messages, { role: 'user', content: text }]
    setMessages(newMessages)
    setIsStreaming(true)
    setMessages(prev => [...prev, { role: 'assistant', content: '' }])

    try {
      const base = config.apiBase || '/v1'
      const token = typeof window !== 'undefined' ? localStorage.getItem('access_token') : ''
      const authKey = mode === 'session' ? token : config.apiKey
      const body: any = {
        model: config.model, temperature: config.temperature,
        top_p: config.topP, max_tokens: config.maxTokens, stream: config.stream,
        messages: [
          ...(config.systemPrompt ? [{ role: 'system', content: config.systemPrompt }] : []),
          ...newMessages,
        ],
      }
      if (config.stop) body.stop = config.stop.split(',').map((x: string) => x.trim()).filter(Boolean)

      const resp = await fetch(`${base}/chat/completions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${authKey}` },
        body: JSON.stringify(body),
      })

      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}))
        throw new Error(errData.error?.message || resp.statusText)
      }

      if (config.stream && resp.body) {
        const reader = resp.body.getReader()
        const decoder = new TextDecoder()
        let buf = ''
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          const lines = buf.split('\n')
          buf = lines.pop() ?? ''
          for (const line of lines) {
            if (!line.startsWith('data: ')) continue
            const raw = line.slice(6).trim()
            if (raw === '[DONE]') continue
            try {
              const chunk = JSON.parse(raw)
              const delta = chunk.choices?.[0]?.delta
              if (delta?.content) {
                setMessages(prev => {
                  const last = { ...prev[prev.length - 1] }
                  last.content += delta.content
                  return [...prev.slice(0, -1), last]
                })
              }
              if (delta?.reasoning_content) {
                setMessages(prev => {
                  const last = { ...prev[prev.length - 1] }
                  last.reasoning_content = (last.reasoning_content || '') + delta.reasoning_content
                  return [...prev.slice(0, -1), last]
                })
              }
            } catch { /* skip */ }
          }
        }
      } else {
        const data = await resp.json()
        const content = data.choices?.[0]?.message?.content || ''
        const reasoning = data.choices?.[0]?.message?.reasoning_content || ''
        setMessages(prev => [...prev.slice(0, -1), { role: 'assistant', content, reasoning_content: reasoning || undefined }])
      }
    } catch (e: any) {
      setMessages(prev => [...prev.slice(0, -1), { role: 'assistant', content: `❌ ${e.message || '请求失败'}` }])
      Toast.error(e.message || '请求失败')
    } finally {
      setIsStreaming(false)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend() }
  }

  return (
    <div className={s.playground}>
      {/* ── 左侧配置 ── */}
      <div className={s.playgroundSidebar}>
        <div className={s.playgroundSidebarHeader}>
          <Title heading={5} style={{ margin: 0 }}>对话测试</Title>
          <Tag size="small" color="purple" style={{ marginTop: 4 }}>Beta</Tag>
        </div>

        <div className={s.playgroundSidebarBody}>
          {/* 模式切换 */}
          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>接入模式</label>
            <div className={s.modeSwitch}>
              {(['session', 'apikey'] as const).map(m => (
                <button key={m}
                  className={`${s.modeSwitchBtn} ${mode === m ? s['modeSwitchBtn--active'] : s['modeSwitchBtn--inactive']}`}
                  onClick={() => { setMode(m); setC('model', '') }}>
                  {m === 'session' ? '会话模式' : 'API Key'}
                </button>
              ))}
            </div>
          </div>

          {/* API Key（仅 apikey 模式）*/}
          {mode === 'apikey' && (
            <div className={s.fieldConfig}>
              <label className={s.fieldConfigLabel}>API Key</label>
              <Input value={config.apiKey} onChange={v => setC('apiKey', v)}
                mode="password" placeholder="sk-..."
                onBlur={loadModels}
                style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} />
            </div>
          )}

          {/* 模型 */}
          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>模型</label>
            <Select value={config.model} onChange={v => setC('model', v as string)}
              style={{ width: '100%' }} placeholder={loadingModels ? '加载中...' : '选择或输入模型名'}
              optionList={modelOptions} filter allowCreate
              suffix={loadingModels ? <Spin size="small" /> : undefined} />
            {currentModel && (currentModel.context_length || currentModel.max_output_tokens) && (
              <Text type="tertiary" size="small" style={{ fontFamily: 'var(--font-mono)', display: 'block', marginTop: 3 }}>
                {fmtTokens(currentModel.context_length)}
                {currentModel.max_output_tokens ? ` · 最大输出 ${fmtTokens(currentModel.max_output_tokens)}` : ''}
              </Text>
            )}
          </div>

          {/* 系统提示词 */}
          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>系统提示词</label>
            <TextArea value={config.systemPrompt} onChange={v => setC('systemPrompt', v)}
              placeholder="可选，设置助手角色..."
              autosize={{ minRows: 2, maxRows: 5 }} />
          </div>

          {/* 参数 */}
          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>
              温度 <Text type="primary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>{config.temperature}</Text>
            </label>
            <Slider value={config.temperature} onChange={v => setC('temperature', v as number)}
              min={0} max={2} step={0.05} />
          </div>

          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>
              Top P <Text type="primary" size="small" style={{ fontFamily: 'var(--font-mono)' }}>{config.topP}</Text>
            </label>
            <Slider value={config.topP} onChange={v => setC('topP', v as number)}
              min={0} max={1} step={0.05} />
          </div>

          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>最大 Token</label>
            <InputNumber value={config.maxTokens} onChange={v => setC('maxTokens', v as number)}
              min={1} max={128000} step={256} style={{ width: '100%' }} />
          </div>

          <div className={s.fieldConfig}>
            <label className={s.fieldConfigLabel}>停止序列</label>
            <Input value={config.stop} onChange={v => setC('stop', v)}
              placeholder="逗号分隔，如：\n,END" />
          </div>

          {/* 流式 */}
          <div className={s.fieldConfig} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <label className={s.fieldConfigLabel} style={{ margin: 0 }}>流式输出</label>
            <Switch checked={config.stream} size="small" onChange={v => setC('stream', v)} />
          </div>

          {/* 高级配置 */}
          <Collapse>
            <Collapse.Panel header="高级配置" itemKey="adv">
              <div className={s.fieldConfig}>
                <label className={s.fieldConfigLabel}>接口地址</label>
                <Input value={config.apiBase} onChange={v => setC('apiBase', v)}
                  style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} placeholder="/v1" />
              </div>
            </Collapse.Panel>
          </Collapse>
        </div>

        <div className={s.playgroundSidebarFooter}>
          <Button block type="tertiary" theme="borderless" icon={<IconDelete />}
            disabled={isStreaming || messages.length === 0}
            onClick={() => setMessages([])}>
            清空对话
          </Button>
        </div>
      </div>

      {/* ── 右侧聊天区 ── */}
      <div className={s.playgroundChat}>
        <div ref={chatRef} className={s.chatMessages}>
          {messages.length === 0 ? (
            <div className={s.chatEmpty}>
              <div style={{ fontSize: 48, marginBottom: 16 }}>💬</div>
              <Text type="secondary">选择模型，开始对话</Text>
              <Text type="tertiary" size="small" style={{ marginTop: 6 }}>
                Enter 发送，Shift+Enter 换行
              </Text>
            </div>
          ) : (
            <div className={s.messageList}>
              {messages.map((msg, idx) => (
                <div key={idx} className={s.message}>
                  <div className={`${s.messageAvatar} ${msg.role === 'user' ? s['messageAvatar--user'] : s['messageAvatar--assistant']}`}>
                    {msg.role === 'user' ? '我' : '助手'}
                  </div>
                  <div className={s.messageContent}>
                    {msg.reasoning_content && (
                      <details className={s.reasoningBlock}>
                        <summary style={{ fontSize: 12, color: 'var(--color-text-tertiary)', cursor: 'pointer' }}>
                          💭 思考过程
                        </summary>
                        <div className={s.reasoningContent}>{msg.reasoning_content}</div>
                      </details>
                    )}
                    <div className={`${s.messageBubble} ${msg.role === 'user' ? s['messageBubble--user'] : ''}`}>
                      {msg.content || (
                        isStreaming && idx === messages.length - 1 ? (
                          <div className={s.typingIndicator}>
                            <span className={s.typingDot} />
                            <span className={s.typingDot} />
                            <span className={s.typingDot} />
                          </div>
                        ) : ''
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 输入区 */}
        <div className={s.chatInputArea}>
          <div className={s.chatInputInner}>
            <div className={s.chatInputBox}>
              <TextArea
                value={userInput}
                onChange={setUserInput}
                onKeyDown={handleKeyDown}
                placeholder="输入消息... (Enter 发送，Shift+Enter 换行)"
                disabled={isStreaming}
                autosize={{ minRows: 1, maxRows: 6 }}
                style={{ border: 'none', boxShadow: 'none', flex: 1, resize: 'none', padding: 0 }}
              />
              <Button
                type="primary" theme="solid"
                icon={isStreaming ? <Spin size="small" /> : <IconSend />}
                disabled={!userInput.trim() || isStreaming || !config.model}
                onClick={handleSend}
                style={{ flexShrink: 0 }}
              />
            </div>
            <Text type="tertiary" size="small" style={{ marginTop: 6, display: 'block' }}>
              {config.model ? `模型：${config.model}` : '请先选择模型'}
              {isStreaming && <Text type="warning" size="small"> · 生成中...</Text>}
            </Text>
          </div>
        </div>
      </div>
    </div>
  )
}
