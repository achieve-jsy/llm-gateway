<template>
  <div class="settings-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">系统设置</h2>
        <p class="page-desc">管理站点品牌标识与 API 接入配置</p>
      </div>
    </div>

    <div v-loading="settingsStore.loading" class="settings-content">
      <!-- Tab Navigation -->
      <div class="tab-nav-wrap">
        <nav class="tab-nav">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            :class="['tab-btn', activeTab === tab.key && 'tab-btn-active']"
            @click="activeTab = tab.key"
          >
            <span class="tab-btn-icon" v-html="tab.icon"></span>
            <span>{{ tab.label }}</span>
          </button>
        </nav>
      </div>

      <!-- 站点品牌 -->
      <div v-show="activeTab === 'brand'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>
            </span>
            <h3 class="card-title">站点品牌</h3>
          </div>
          <p class="card-desc">自定义站点的视觉标识，Logo 将显示在侧边栏顶部</p>
        </div>

        <div class="card-body">
          <!-- Logo -->
          <div class="form-group">
            <label class="form-label">站点 Logo</label>
            <ImageUpload
              v-model="form.site_logo"
              upload-label="上传 Logo"
              remove-label="移除"
              hint="支持 PNG、JPG、SVG 格式，建议正方形图片，最大 300KB"
              :max-size="300 * 1024"
            />
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label">站点名称</label>
              <el-input
                v-model="form.site_name"
                placeholder="LLM Gateway"
                maxlength="64"
                class="form-input"
              />
              <p class="form-hint">显示在页面标题、侧边栏和 CCSwitch 导入时</p>
            </div>
            <div class="form-group">
              <label class="form-label">站点副标题</label>
              <el-input
                v-model="form.site_subtitle"
                placeholder="LLM API 统一网关"
                maxlength="128"
                class="form-input"
              />
              <p class="form-hint">侧边栏下方或首页的描述性文字</p>
            </div>
          </div>
        </div>
      </div>

      <!-- 接入配置 -->
      <div v-show="activeTab === 'access'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
            </span>
            <h3 class="card-title">接入配置</h3>
          </div>
          <p class="card-desc">配置对外暴露的 API 地址，供客户端工具自动发现</p>
        </div>

        <div class="card-body">
          <div class="form-group">
            <label class="form-label">供应商名称</label>
            <el-input
              v-model="form.supplier_name"
              placeholder="某公司"
              maxlength="64"
              class="form-input"
            />
            <p class="form-hint">用户下载配置脚本时显示的供应商名称，默认"某公司"</p>
          </div>

          <div class="form-group">
            <label class="form-label">API 基础 URL</label>
            <el-input
              v-model="form.api_base_url"
              placeholder="留空则自动使用当前访问域名"
              class="form-input mono"
            >
              <template #prefix>
                <span class="input-prefix-icon">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                </span>
              </template>
            </el-input>
            <p class="form-hint">客户端（如 CCSwitch、Codex CLI）将通过此地址连接网关 API。例如 <code class="inline-code">https://api.example.com</code></p>
          </div>

          <!-- 预览 -->
          <div class="preview-box">
            <div class="preview-label">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              接入地址预览
            </div>
            <code class="preview-url">{{ resolvedBaseUrl }}/v1</code>
          </div>
        </div>
      </div>

      <!-- 并发管控 -->
      <div v-show="activeTab === 'concurrency'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
            </span>
            <h3 class="card-title">并发管控</h3>
          </div>
          <p class="card-desc">控制 API 请求频率与并发数，防止资源过载，0 表示不限制</p>
        </div>

        <div class="card-body">
          <div class="form-row form-row-3col">
            <div class="form-group">
              <label class="form-label">每分钟请求上限</label>
              <el-input-number
                v-model="form.rate_limit_per_minute"
                :min="0"
                :max="10000"
                :step="10"
                class="form-input-number"
              />
              <p class="form-hint">每个 API Key 每分钟最大请求数</p>
            </div>
            <div class="form-group">
              <label class="form-label">单 Key 最大并发</label>
              <el-input-number
                v-model="form.max_concurrency_per_key"
                :min="0"
                :max="1000"
                :step="1"
                class="form-input-number"
              />
              <p class="form-hint">同一 Key 同时处理的最大请求数</p>
            </div>
          </div>
        </div>
      </div>

      <!-- OIDC 配置 -->
      <div v-show="activeTab === 'oidc'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
            </span>
            <h3 class="card-title">OIDC 配置</h3>
          </div>
          <p class="card-desc">配置 OIDC/SSO 单点登录参数，修改后需重启服务生效</p>
        </div>

        <div class="card-body">
          <div class="form-group inline-toggle">
            <label class="form-label">启用 OIDC 登录</label>
            <el-switch v-model="form.oidc_enabled" />
          </div>

          <div v-show="form.oidc_enabled" class="form-section">
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">Issuer URL</label>
                <el-input v-model="form.oidc_issuer_url" placeholder="https://sso.example.com" class="form-input mono" />
                <p class="form-hint">OIDC Provider 的 issuer 地址</p>
              </div>
              <div class="form-group">
                <label class="form-label">Redirect URL</label>
                <el-input v-model="form.oidc_redirect_url" placeholder="http://localhost:8082/auth/oidc/callback" class="form-input mono" />
                <p class="form-hint">OAuth2 回调地址</p>
              </div>
            </div>
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">Client ID</label>
                <el-input v-model="form.oidc_client_id" placeholder="your-client-id" class="form-input" />
              </div>
              <div class="form-group">
                <label class="form-label">Client Secret</label>
                <el-input v-model="form.oidc_client_secret" placeholder="已保存时为空" type="password" show-password class="form-input" />
                <p class="form-hint">留空则不修改已保存的 Secret</p>
              </div>
            </div>
            <div class="form-group">
              <label class="form-label">Scopes</label>
              <el-input v-model="form.oidc_scopes" placeholder="openid profile email" class="form-input" />
              <p class="form-hint">OAuth2 授权范围，空格分隔</p>
            </div>
          </div>
        </div>
      </div>

      <!-- Webhook 通知 -->
      <div v-show="activeTab === 'webhook'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
            </span>
            <h3 class="card-title">Webhook 通知</h3>
          </div>
          <p class="card-desc">配置事件通知 Webhook，当监控事件发生时自动推送</p>
        </div>

        <div class="card-body">
          <div class="form-group inline-toggle">
            <label class="form-label">启用 Webhook</label>
            <el-switch v-model="form.webhook_enabled" />
          </div>

          <div v-show="form.webhook_enabled" class="form-section">
            <div class="form-group">
              <label class="form-label">Webhook URL</label>
              <el-input v-model="form.webhook_url" placeholder="https://hooks.example.com/notify" class="form-input mono" />
              <p class="form-hint">接收通知的目标地址</p>
            </div>
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">签名密钥</label>
                <el-input v-model="form.webhook_secret" placeholder="留空则不签名" type="password" show-password class="form-input mono" />
                <p class="form-hint">用于 HMAC-SHA256 签名的密钥</p>
              </div>
              <div class="form-group">
                <label class="form-label">通知事件</label>
                <el-select v-model="selectedEvents" multiple placeholder="选择事件类型" class="form-select">
                  <el-option label="健康检查失败" value="health_check_failed" />
                  <el-option label="健康检查恢复" value="health_check_recovered" />
                  <el-option label="Key 即将过期" value="key_expiring" />
                  <el-option label="Key 已过期" value="key_expired" />
                </el-select>
                <p class="form-hint">选择需要通知的事件类型，可多选</p>
              </div>
            </div>
            <el-button :loading="testingWebhook" @click="handleTestWebhook">
              <template #icon>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
              </template>
              发送测试通知
            </el-button>
          </div>
        </div>
      </div>

      <!-- 故障默认配置 -->
      <div v-show="activeTab === 'failover'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
            </span>
            <h3 class="card-title">模型组故障默认配置</h3>
          </div>
          <p class="card-desc">模型组不设置时默认使用此全局配置，单个模型组可独立覆盖</p>
        </div>

        <div class="card-body">
          <div class="form-row form-row-3col">
            <div class="form-group">
              <label class="form-label">默认最大切换次数</label>
              <el-input-number
                v-model="form.default_max_switches"
                :min="1"
                :max="10"
                :step="1"
                class="form-input-number"
              />
              <p class="form-hint">单次请求最多切换几个模型</p>
            </div>
            <div class="form-group">
              <label class="form-label">默认同模型重试</label>
              <el-input-number
                v-model="form.default_same_model_retries"
                :min="0"
                :max="5"
                :step="1"
                class="form-input-number"
              />
              <p class="form-hint">同一模型失败后重试几次</p>
            </div>
            <div class="form-group">
              <label class="form-label">默认冷却时间(秒)</label>
              <el-input-number
                v-model="form.default_failover_cooldown_seconds"
                :min="10"
                :max="600"
                :step="10"
                class="form-input-number"
              />
              <p class="form-hint">模型切换后冷却多久</p>
            </div>
          </div>
        </div>
      </div>

      <!-- 数据管理 -->
      <div v-show="activeTab === 'data'" class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
            </span>
            <h3 class="card-title">数据管理</h3>
          </div>
          <p class="card-desc">导出当前所有设置项为 JSON 备份文件，或从备份文件恢复设置</p>
        </div>

        <div class="card-body">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">导出配置</label>
              <el-button :loading="exporting" @click="handleExport">
                <template #icon>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                </template>
                导出 JSON
              </el-button>
              <p class="form-hint">下载当前所有设置项为 JSON 文件</p>
            </div>
            <div class="form-group">
              <label class="form-label">导入配置</label>
              <div class="import-group">
                <el-button :loading="importing" @click="triggerImport">
                  <template #icon>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                  </template>
                  导入 JSON
                </el-button>
                <input ref="importInputRef" type="file" accept=".json" class="hidden-input" @change="handleImport" />
              </div>
              <p class="form-hint">选择备份的 JSON 文件恢复设置（合并模式，不删除已有配置）</p>
            </div>
          </div>
        </div>
      </div>

      <!-- 操作栏 -->
      <div class="action-bar">
        <div class="action-status">
          <span v-if="formChanged" class="change-indicator">有未保存的更改</span>
          <span v-else-if="settingsStore.loaded" class="saved-indicator">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>
            所有更改已保存
          </span>
        </div>
        <div class="action-buttons">
          <el-button @click="handleReset" :disabled="!formChanged">重置</el-button>
          <el-button type="primary" :loading="saving" @click="handleSave" :disabled="!formChanged">
            保存设置
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '@/stores/settings'
import { exportSettings, importSettings } from '@/services/control-plane'
import ImageUpload from '@/components/common/ImageUpload.vue'

const settingsStore = useSettingsStore()
const saving = ref(false)
const testingWebhook = ref(false)
const exporting = ref(false)
const importing = ref(false)
const importInputRef = ref<HTMLInputElement>()
const activeTab = ref('brand')

const tabs = [
  { key: 'brand', label: '站点品牌', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>' },
  { key: 'access', label: '接入配置', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10"/></svg>' },
  { key: 'concurrency', label: '并发管控', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>' },
  { key: 'oidc', label: 'OIDC 配置', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>' },
  { key: 'webhook', label: 'Webhook', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>' },
  { key: 'failover', label: '故障默认配置', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>' },
  { key: 'data', label: '数据管理', icon: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>' },
]

const form = reactive({
  site_name: '',
  site_logo: '',
  site_subtitle: '',
  api_base_url: '',
  supplier_name: '某公司',
  rate_limit_per_minute: 60,
  max_concurrency_per_key: 10,
  oidc_issuer_url: '',
  oidc_client_id: '',
  oidc_client_secret: '',
  oidc_redirect_url: '',
  oidc_scopes: 'openid profile email',
  oidc_enabled: false,
  webhook_enabled: false,
  webhook_url: '',
  webhook_secret: '',
  webhook_events: 'health_check_failed',
  default_max_switches: 2,
  default_same_model_retries: 1,
  default_failover_cooldown_seconds: 60,
})

const initialForm = reactive({
  site_name: '',
  site_logo: '',
  site_subtitle: '',
  api_base_url: '',
  supplier_name: '某公司',
  rate_limit_per_minute: 60,
  max_concurrency_per_key: 10,
  oidc_issuer_url: '',
  oidc_client_id: '',
  oidc_client_secret: '',
  oidc_redirect_url: '',
  oidc_scopes: 'openid profile email',
  oidc_enabled: false,
  webhook_enabled: false,
  webhook_url: '',
  webhook_secret: '',
  webhook_events: 'health_check_failed',
  default_max_switches: 2,
  default_same_model_retries: 1,
  default_failover_cooldown_seconds: 60,
})

const selectedEvents = computed({
  get: () => form.webhook_events ? form.webhook_events.split(',').filter(Boolean) : [],
  set: (val: string[]) => { form.webhook_events = val.join(',') }
})

const resolvedBaseUrl = computed(() => {
  const url = form.api_base_url.trim()
  if (url) return url.replace(/\/+$/, '')
  return window.location.origin
})

const formChanged = computed(() => {
  return (
    form.site_name !== initialForm.site_name ||
    form.site_logo !== initialForm.site_logo ||
    form.site_subtitle !== initialForm.site_subtitle ||
    form.api_base_url !== initialForm.api_base_url ||
    form.supplier_name !== initialForm.supplier_name ||
    form.rate_limit_per_minute !== initialForm.rate_limit_per_minute ||
    form.max_concurrency_per_key !== initialForm.max_concurrency_per_key ||
    form.oidc_issuer_url !== initialForm.oidc_issuer_url ||
    form.oidc_client_id !== initialForm.oidc_client_id ||
    form.oidc_client_secret !== initialForm.oidc_client_secret ||
    form.oidc_redirect_url !== initialForm.oidc_redirect_url ||
    form.oidc_scopes !== initialForm.oidc_scopes ||
    form.oidc_enabled !== initialForm.oidc_enabled ||
    form.webhook_enabled !== initialForm.webhook_enabled ||
    form.webhook_url !== initialForm.webhook_url ||
    form.webhook_secret !== initialForm.webhook_secret ||
    form.webhook_events !== initialForm.webhook_events ||
    form.default_max_switches !== initialForm.default_max_switches ||
    form.default_same_model_retries !== initialForm.default_same_model_retries ||
    form.default_failover_cooldown_seconds !== initialForm.default_failover_cooldown_seconds
  )
})

function setFormValues(source: typeof form) {
  form.site_name = source.site_name
  form.site_logo = source.site_logo
  form.site_subtitle = source.site_subtitle
  form.api_base_url = source.api_base_url
  form.supplier_name = source.supplier_name
  form.rate_limit_per_minute = source.rate_limit_per_minute
  form.max_concurrency_per_key = source.max_concurrency_per_key
  form.oidc_issuer_url = source.oidc_issuer_url
  form.oidc_client_id = source.oidc_client_id
  form.oidc_client_secret = source.oidc_client_secret
  form.oidc_redirect_url = source.oidc_redirect_url
  form.oidc_scopes = source.oidc_scopes
  form.oidc_enabled = source.oidc_enabled
  form.webhook_enabled = source.webhook_enabled
  form.webhook_url = source.webhook_url
  form.webhook_secret = source.webhook_secret
  form.webhook_events = source.webhook_events
  form.default_max_switches = source.default_max_switches
  form.default_same_model_retries = source.default_same_model_retries
  form.default_failover_cooldown_seconds = source.default_failover_cooldown_seconds
}

function captureInitial() {
  initialForm.site_name = form.site_name
  initialForm.site_logo = form.site_logo
  initialForm.site_subtitle = form.site_subtitle
  initialForm.api_base_url = form.api_base_url
  initialForm.supplier_name = form.supplier_name
  initialForm.rate_limit_per_minute = form.rate_limit_per_minute
  initialForm.max_concurrency_per_key = form.max_concurrency_per_key
  initialForm.oidc_issuer_url = form.oidc_issuer_url
  initialForm.oidc_client_id = form.oidc_client_id
  initialForm.oidc_client_secret = form.oidc_client_secret
  initialForm.oidc_redirect_url = form.oidc_redirect_url
  initialForm.oidc_scopes = form.oidc_scopes
  initialForm.oidc_enabled = form.oidc_enabled
  initialForm.webhook_enabled = form.webhook_enabled
  initialForm.webhook_url = form.webhook_url
  initialForm.webhook_secret = form.webhook_secret
  initialForm.webhook_events = form.webhook_events
  initialForm.default_max_switches = form.default_max_switches
  initialForm.default_same_model_retries = form.default_same_model_retries
  initialForm.default_failover_cooldown_seconds = form.default_failover_cooldown_seconds
}

onMounted(async () => {
  await settingsStore.loadSystemSettings()
  const s = settingsStore.systemSettings
  if (s) {
    form.site_name = s.site_name || ''
    form.site_logo = s.site_logo || ''
    form.site_subtitle = s.site_subtitle || ''
    form.api_base_url = s.api_base_url || ''
    form.supplier_name = s.supplier_name || '某公司'
    form.rate_limit_per_minute = s.rate_limit_per_minute ?? 60
    form.max_concurrency_per_key = s.max_concurrency_per_key ?? 10
    form.oidc_issuer_url = s.oidc_issuer_url || ''
    form.oidc_client_id = s.oidc_client_id || ''
    form.oidc_redirect_url = s.oidc_redirect_url || ''
    form.oidc_scopes = s.oidc_scopes || 'openid profile email'
    form.oidc_enabled = s.oidc_enabled ?? false
    form.webhook_enabled = s.webhook_enabled ?? false
    form.webhook_url = s.webhook_url || ''
    form.webhook_events = s.webhook_events || 'health_check_failed'
    form.default_max_switches = s.default_max_switches ?? 2
    form.default_same_model_retries = s.default_same_model_retries ?? 1
    form.default_failover_cooldown_seconds = s.default_failover_cooldown_seconds ?? 60
  }
  captureInitial()
})

function isValidHttpUrl(url: string): boolean {
  if (!url) return true
  return /^https?:\/\/.+/.test(url)
}

function validateForm(): string | null {
  if (form.api_base_url && !isValidHttpUrl(form.api_base_url)) {
    return 'API 基础 URL 格式不正确，请输入以 http:// 或 https:// 开头的地址'
  }
  if (form.oidc_enabled) {
    if (form.oidc_issuer_url && !isValidHttpUrl(form.oidc_issuer_url)) {
      return 'OIDC Issuer URL 格式不正确，请输入以 http:// 或 https:// 开头的地址'
    }
    if (form.oidc_redirect_url && !isValidHttpUrl(form.oidc_redirect_url)) {
      return 'OIDC Redirect URL 格式不正确，请输入以 http:// 或 https:// 开头的地址'
    }
  }
  if (form.webhook_enabled) {
    if (!form.webhook_url) {
      return '启用 Webhook 时请填写 Webhook URL'
    }
    if (!isValidHttpUrl(form.webhook_url)) {
      return 'Webhook URL 格式不正确，请输入以 http:// 或 https:// 开头的地址'
    }
  }
  return null
}

async function handleSave() {
  const error = validateForm()
  if (error) {
    ElMessage.warning(error)
    return
  }
  saving.value = true
  try {
    const ok = await settingsStore.saveSettings({
      site_name: form.site_name.trim(),
      site_logo: form.site_logo,
      site_subtitle: form.site_subtitle.trim(),
      api_base_url: form.api_base_url.trim(),
      supplier_name: form.supplier_name.trim(),
      rate_limit_per_minute: form.rate_limit_per_minute,
      max_concurrency_per_key: form.max_concurrency_per_key,
      oidc_issuer_url: form.oidc_issuer_url.trim(),
      oidc_client_id: form.oidc_client_id.trim(),
      oidc_client_secret: form.oidc_client_secret,
      oidc_redirect_url: form.oidc_redirect_url.trim(),
      oidc_scopes: form.oidc_scopes.trim(),
      oidc_enabled: form.oidc_enabled,
      webhook_enabled: form.webhook_enabled,
      webhook_url: form.webhook_url.trim(),
      webhook_secret: form.webhook_secret,
      webhook_events: form.webhook_events,
      default_max_switches: form.default_max_switches,
      default_same_model_retries: form.default_same_model_retries,
      default_failover_cooldown_seconds: form.default_failover_cooldown_seconds,
    })
    if (ok) {
      ElMessage.success('设置已保存')
      captureInitial()
    } else {
      ElMessage.error('保存失败，请重试')
    }
  } catch {
    ElMessage.error('保存失败，请重试')
  } finally {
    saving.value = false
  }
}

function handleReset() {
  setFormValues(initialForm)
}

async function handleTestWebhook() {
  if (!form.webhook_url) {
    ElMessage.warning('请先填写 Webhook URL')
    return
  }
  testingWebhook.value = true
  try {
    await settingsStore.testWebhook()
    ElMessage.success('测试通知发送成功')
  } catch {
    ElMessage.error('测试通知发送失败，请检查 URL 是否正确')
  } finally {
    testingWebhook.value = false
  }
}

async function handleExport() {
  exporting.value = true
  try {
    const data = await exportSettings()
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `llm-gateway-settings-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('配置已导出')
  } catch {
    ElMessage.error('导出失败')
  } finally {
    exporting.value = false
  }
}

function triggerImport() {
  importInputRef.value?.click()
}

async function handleImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  importing.value = true
  try {
    const text = await file.text()
    const settings = JSON.parse(text)
    if (typeof settings !== 'object' || Array.isArray(settings)) {
      ElMessage.error('无效的 JSON 格式，应为 key-value 对象')
      return
    }
    await importSettings(settings)
    ElMessage.success('配置已导入，正在刷新...')
    setTimeout(() => { window.location.reload() }, 800)
  } catch {
    ElMessage.error('导入失败，请检查文件格式')
  } finally {
    importing.value = false
    input.value = ''
  }
}
</script>

<style scoped>
.settings-page {
  padding: 0;
  width: 100%;
}

.page-header {
  margin-bottom: 28px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: 24px;
  font-weight: 700;
  margin: 0 0 6px;
  letter-spacing: -0.3px;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  margin: 0;
}

/* ===== Tab 导航 ===== */
.tab-nav-wrap {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--el-bg-color);
  margin: 0 -24px;
  padding: 0 24px;
}

.tab-nav {
  display: flex;
  gap: 4px;
  overflow-x: auto;
  border-bottom: 2px solid var(--el-border-color-lighter);
  padding-bottom: 0;
  scrollbar-width: none;
}

.tab-nav::-webkit-scrollbar {
  display: none;
}

.tab-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 10px 16px;
  border: none;
  background: none;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -2px;
  white-space: nowrap;
  transition: color 0.15s, border-color 0.15s;
}

.tab-btn:hover {
  color: var(--el-text-color-primary);
}

.tab-btn-active {
  color: var(--el-color-primary);
  border-bottom-color: var(--el-color-primary);
}

.tab-btn-icon {
  display: flex;
  align-items: center;
  opacity: 0.7;
}

.tab-btn-active .tab-btn-icon {
  opacity: 1;
}

.tab-nav + .settings-card {
  margin-top: 20px;
}

/* ===== 卡片 ===== */
.settings-content {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.settings-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  transition: border-color 0.15s;
}

.settings-card:hover {
  border-color: var(--el-border-color);
}

.card-header {
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.card-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.card-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.card-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.card-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 8px 0 0 42px;
  line-height: 1.5;
}

.card-body {
  padding: 20px 24px 24px;
}

/* ===== 表单 ===== */
.form-group {
  margin-bottom: 20px;
}

.form-group:last-child {
  margin-bottom: 0;
}

.form-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}

.form-input :deep(.el-input__wrapper) {
  border-radius: 8px;
  box-shadow: 0 0 0 1px var(--el-border-color) inset;
  transition: box-shadow 0.2s, border-color 0.2s;
}

.form-input :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--el-border-color-darker) inset;
}

.form-input :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
}

.form-input.mono :deep(.el-input__inner) {
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
}

.form-input-number {
  width: 100%;
}

.form-input-number :deep(.el-input__wrapper) {
  border-radius: 8px;
  box-shadow: 0 0 0 1px var(--el-border-color) inset;
}

.form-input-number :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--el-border-color-darker) inset;
}

.form-input-number :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
}

.input-prefix-icon {
  display: flex;
  align-items: center;
  color: var(--el-text-color-placeholder);
  margin-right: 6px;
}

.form-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 6px 0 0;
  line-height: 1.5;
}

.inline-code {
  display: inline;
  padding: 1px 5px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 11px;
  background: var(--el-fill-color-dark);
  border-radius: 4px;
  color: var(--el-text-color-secondary);
}

.inline-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.inline-toggle .form-label {
  margin-bottom: 0;
}

.form-section {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.form-select {
  width: 100%;
}

.form-select :deep(.el-select__wrapper) {
  border-radius: 8px;
  box-shadow: 0 0 0 1px var(--el-border-color) inset;
}

.import-group {
  display: flex;
  align-items: center;
}

.hidden-input {
  display: none;
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

.form-row-3col {
  grid-template-columns: 1fr 1fr;
}

@media (max-width: 768px) {
  .form-row,
  .form-row-3col {
    grid-template-columns: 1fr;
  }
}

/* ===== 预览 ===== */
.preview-box {
  margin-top: 20px;
  padding: 14px 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  overflow-x: auto;
}

.preview-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.preview-url {
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  color: var(--el-color-primary);
  word-break: break-all;
}

/* ===== 操作栏 ===== */
.action-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 24px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  position: sticky;
  bottom: 20px;
  backdrop-filter: blur(8px);
}

.action-status {
  display: flex;
  align-items: center;
}

.change-indicator {
  font-size: 13px;
  color: var(--el-color-warning);
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 6px;
}

.change-indicator::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-warning);
}

.saved-indicator {
  font-size: 13px;
  color: var(--el-color-success);
  display: flex;
  align-items: center;
  gap: 6px;
}

.action-buttons {
  display: flex;
  gap: 8px;
}

/* ===== 响应式 ===== */

/* 平板端 (≤1024px) */
@media (max-width: 1024px) {
  .card-header {
    padding: 16px 20px 14px;
  }

  .card-body {
    padding: 16px 20px 20px;
  }

  .card-desc {
    margin-left: 42px;
  }

  .form-row {
    gap: 16px;
  }

  .action-bar {
    padding: 14px 20px;
  }
}

/* 小屏端 (≤768px) */
@media (max-width: 768px) {
  .page-header {
    margin-bottom: 20px;
  }

  .page-title {
    font-size: 20px;
  }

  .page-desc {
    font-size: 13px;
  }

  .tab-nav-wrap {
    margin: 0 -16px;
    padding: 0 16px;
  }

  .tab-btn {
    padding: 8px 12px;
    font-size: 12px;
    gap: 4px;
  }

  .tab-btn svg {
    width: 14px;
    height: 14px;
  }

  .settings-content {
    gap: 16px;
  }

  .settings-card {
    border-radius: 10px;
  }

  .card-header {
    padding: 14px 16px 12px;
  }

  .card-icon {
    width: 28px;
    height: 28px;
    border-radius: 7px;
  }

  .card-icon svg {
    width: 15px;
    height: 15px;
  }

  .card-title {
    font-size: 15px;
  }

  .card-desc {
    font-size: 12px;
    margin-left: 38px;
  }

  .card-body {
    padding: 14px 16px 18px;
  }

  .form-group {
    margin-bottom: 16px;
  }

  .action-bar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
    padding: 14px 16px;
    border-radius: 10px;
  }

  .action-buttons {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }

  .action-buttons :deep(.el-button) {
    width: 100%;
    justify-content: center;
  }

  .preview-box {
    padding: 12px 14px;
  }

  .preview-url {
    font-size: 12px;
  }
}

/* 手机端 (≤480px) */
@media (max-width: 480px) {
  .page-title {
    font-size: 18px;
  }

  .settings-content {
    gap: 12px;
  }

  .settings-card {
    border-radius: 8px;
  }

  .card-header {
    padding: 12px 12px 10px;
  }

  .card-icon {
    width: 24px;
    height: 24px;
    border-radius: 6px;
  }

  .card-icon svg {
    width: 13px;
    height: 13px;
  }

  .card-title {
    font-size: 14px;
  }

  .card-desc {
    font-size: 12px;
    margin-left: 34px;
  }

  .card-body {
    padding: 12px 12px 16px;
  }

  .form-label {
    font-size: 12px;
  }

  .form-input :deep(.el-input__inner) {
    font-size: 14px;
  }

  .preview-url {
    font-size: 11px;
  }

  .action-bar {
    padding: 12px;
    border-radius: 8px;
    bottom: 12px;
  }

  .action-buttons {
    grid-template-columns: 1fr;
  }

  .action-status {
    justify-content: center;
    font-size: 12px;
  }
}
</style>