import { ElMessage } from 'element-plus'
import { useSettingsStore } from '@/stores/settings'

export function getBaseUrl(): string {
  const settingsStore = useSettingsStore()
  return settingsStore.getBaseUrl()
}

export function buildCcsUsageScript(): string {
  return `({
    request: {
      url: "{{baseUrl}}/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`
}

export function executeCcsImport(apiKey: string, clientType: string = 'claude') {
  const settingsStore = useSettingsStore()
  const baseUrl = settingsStore.getBaseUrl()
  const providerName = settingsStore.getSiteName()

  const usageScript = buildCcsUsageScript()

  const params = new URLSearchParams({
    resource: 'provider',
    app: clientType,
    name: providerName,
    homepage: baseUrl,
    endpoint: baseUrl,
    apiKey: apiKey,
    configFormat: 'json',
    usageEnabled: 'true',
    usageScript: btoa(usageScript),
    usageAutoInterval: '30'
  })

  const deeplink = `ccswitch://v1/import?${params.toString()}`

  try {
    window.open(deeplink, '_self')

    setTimeout(() => {
      if (document.hasFocus()) {
        ElMessage.error('CCSwitch 未安装或无法响应，请确认已安装 CC-Switch 桌面应用')
      }
    }, 100)
  } catch {
    ElMessage.error('CCSwitch 未安装或无法响应，请确认已安装 CC-Switch 桌面应用')
  }
}