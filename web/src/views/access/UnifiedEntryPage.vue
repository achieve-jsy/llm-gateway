<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { CopyDocument, Check, Connection, Key, ChatDotRound } from '@element-plus/icons-vue'
import ModelDiscoverySection from '@/components/access/ModelDiscoverySection.vue'
import { getPublicSettings } from '@/services/control-plane'

const apiBaseUrl = ref('')
const siteName = ref('LLM Gateway')
const copied = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | null = null

const displayUrl = computed(() => {
  const base = apiBaseUrl.value || 'https://your-gateway.example.com'
  return base.endsWith('/v1') ? base : base.replace(/\/$/, '') + '/v1'
})

import { copyToClipboard } from '@/utils/clipboard'

function copyUrl() {
  copyToClipboard(displayUrl.value)
  copied.value = true
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copied.value = false }, 2200)
}

onMounted(async () => {
  try {
    const res = await getPublicSettings()
    apiBaseUrl.value = res.data.api_base_url || ''
    if (res.data.site_name) siteName.value = res.data.site_name
  } catch { /* noop */ }
})

const STEPS = [
  { icon: Key,         num: '1', title: '获取 API Key',   desc: '在「我的 Key」页面创建密钥' },
  { icon: Connection,  num: '2', title: '配置 Base URL',  desc: '将上方地址设为客户端 base_url' },
  { icon: ChatDotRound,num: '3', title: '选择模型组调用', desc: '将模型组名填入 model 字段即可' },
]
</script>

<template>
  <div class="uep">

    <!-- ── Hero ─────────────────────────────────────────── -->
    <div class="hero">
      <!-- 左侧文字 -->
      <div class="hero-text">
        <span class="hero-tag">统一接入</span>
        <h1 class="hero-h1">{{ siteName }}</h1>
        <p class="hero-sub">
          OpenAI 兼容 API 网关，一套接口对接多家大模型。<br>
          将下方 Base URL 填入任何 OpenAI 客户端，即刻开始调用。
        </p>

        <!-- URL 复制框 -->
        <div class="url-box">
          <span class="url-prefix">Base URL</span>
          <span class="url-value">{{ displayUrl }}</span>
          <button class="url-btn" :class="{ ok: copied }" @click="copyUrl">
            <el-icon><component :is="copied ? Check : CopyDocument" /></el-icon>
            {{ copied ? '已复制' : '复制' }}
          </button>
        </div>

        <!-- 三步 -->
        <div class="steps">
          <div v-for="s in STEPS" :key="s.num" class="step">
            <span class="step-n">{{ s.num }}</span>
            <el-icon class="step-ico"><component :is="s.icon" /></el-icon>
            <div class="step-txt">
              <b>{{ s.title }}</b>
              <span>{{ s.desc }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧代码 -->
      <div class="hero-code">
        <div class="code-win">
          <div class="code-bar">
            <span class="cb-dot" style="background:#FF5F57"></span>
            <span class="cb-dot" style="background:#FEBC2E"></span>
            <span class="cb-dot" style="background:#28C840"></span>
            <span class="cb-name">quick_start.py</span>
          </div>
          <pre class="code-body"><span class="t-comment"># pip install openai</span>
<span class="t-kw">from</span> openai <span class="t-kw">import</span> OpenAI

client = OpenAI(
    base_url=<span class="t-str">"{{ displayUrl }}"</span>,
    api_key=<span class="t-str">"sk-..."</span>,
)

resp = client.chat.completions.create(
    model=<span class="t-str">"&lt;模型组名&gt;"</span>,
    messages=[{
        <span class="t-str">"role"</span>: <span class="t-str">"user"</span>,
        <span class="t-str">"content"</span>: <span class="t-str">"你好！"</span>,
    }],
    stream=<span class="t-kw">True</span>,
)
<span class="t-kw">for</span> chunk <span class="t-kw">in</span> resp:
    print(chunk.choices[<span class="t-num">0</span>].delta.content, end=<span class="t-str">""</span>)</pre>
        </div>
      </div>
    </div>

    <!-- ── 模型组 ─────────────────────────────────────────── -->
    <div class="sec">
      <div class="sec-hd">
        <div class="sec-hd-left">
          <h2 class="sec-title">可用模型组</h2>
          <p class="sec-desc">下方每个模型组名即为 API 调用时 <code>model</code> 参数的值，复制后直接使用</p>
        </div>
      </div>
      <ModelDiscoverySection />
    </div>

  </div>
</template>

<style lang="scss" scoped>
/* ── 根 ─────────────────────────────────────────── */
.uep {
  width: min(100%, 1080px);
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 32px;
}

/* ══ Hero ══════════════════════════════════════════ */
.hero {
  display: grid;
  grid-template-columns: 1fr 400px;
  gap: 40px;
  align-items: start;
  background: linear-gradient(145deg, #F0FAFA 0%, #FFFFFF 55%, #F7F9FF 100%);
  border: 1px solid #D8EEEC;
  border-radius: 18px;
  padding: 40px 44px;
  position: relative;

  // 左上角装饰
  &::before {
    content: '';
    position: absolute;
    top: -60px; left: -60px;
    width: 260px; height: 260px;
    background: radial-gradient(circle, rgba(43,168,162,0.12) 0%, transparent 65%);
    pointer-events: none;
  }
}

/* 左侧 */
.hero-text {
  display: flex;
  flex-direction: column;
  gap: 18px;
  position: relative;
  z-index: 1;
}

.hero-tag {
  display: inline-block;
  background: rgba(43,168,162,0.1);
  color: #1A8B85;
  border: 1px solid rgba(43,168,162,0.22);
  font-size: 11.5px;
  font-weight: 700;
  letter-spacing: 1px;
  text-transform: uppercase;
  padding: 3px 10px;
  border-radius: 99px;
  width: fit-content;
}

.hero-h1 {
  font-size: 38px;
  font-weight: 900;
  letter-spacing: -1px;
  color: #0D2B2A;
  margin: 0;
  line-height: 1.1;
}

.hero-sub {
  font-size: 14.5px;
  color: #5A7A7A;
  line-height: 1.7;
  margin: 0;
}

/* URL 框 */
.url-box {
  display: flex;
  align-items: center;
  background: #FFFFFF;
  border: 1.5px solid #C8E4E2;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(43,168,162,0.06);
  transition: border-color 0.2s, box-shadow 0.2s;

  &:hover {
    border-color: #2BA8A2;
    box-shadow: 0 0 0 3px rgba(43,168,162,0.08);
  }
}

.url-prefix {
  padding: 0 12px;
  font-size: 11px;
  font-weight: 700;
  color: #2BA8A2;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  border-right: 1px solid #D8EEEC;
  height: 44px;
  display: flex;
  align-items: center;
  white-space: nowrap;
  background: rgba(43,168,162,0.04);
}

.url-value {
  flex: 1;
  padding: 0 14px;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 13px;
  color: #1A5C58;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  user-select: all;
}

.url-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 0 16px;
  height: 44px;
  background: #2BA8A2;
  border: none;
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  letter-spacing: 0.3px;
  transition: background 0.15s;
  white-space: nowrap;

  .el-icon { font-size: 14px; }

  &:hover { background: #229A94; }
  &.ok { background: #27AE60; }
}

/* 三步 */
.steps {
  display: flex;
  flex-direction: column;
  gap: 0;
  background: #fff;
  border: 1px solid #E0EEEC;
  border-radius: 12px;
  overflow: hidden;
}

.step {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid #F0F8F7;

  &:last-child { border-bottom: none; }
}

.step-n {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: rgba(43,168,162,0.1);
  border: 1.5px solid rgba(43,168,162,0.25);
  color: #2BA8A2;
  font-size: 12px;
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.step-ico {
  font-size: 17px;
  color: #2BA8A2;
  flex-shrink: 0;
}

.step-txt {
  display: flex;
  flex-direction: column;
  gap: 1px;

  b {
    font-size: 13px;
    font-weight: 700;
    color: #0D2B2A;
  }

  span {
    font-size: 12px;
    color: #7A9898;
  }
}

/* 代码窗口 */
.hero-code {
  position: relative;
  z-index: 1;
}

.code-win {
  background: #131E2B;
  border-radius: 14px;
  overflow: visible;
  box-shadow: 0 20px 60px rgba(13, 43, 42, 0.18), 0 4px 12px rgba(0,0,0,0.12);
}

.code-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 12px 16px;
  background: #0E1720;
  border-bottom: 1px solid rgba(255,255,255,0.06);
  border-radius: 14px 14px 0 0;
}

.cb-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.cb-name {
  margin-left: 8px;
  font-size: 11.5px;
  color: #4A6A7A;
  font-family: 'Fira Code', monospace;
}

.code-body {
  padding: 20px 22px;
  margin: 0;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12.5px;
  line-height: 1.75;
  color: #9BB8C8;
  overflow-x: auto;
  tab-size: 4;
  border-radius: 0 0 14px 14px;
  -webkit-overflow-scrolling: touch;

  .t-comment { color: #3D6070; font-style: italic; }
  .t-kw      { color: #7EB8E0; }
  .t-str     { color: #A8D498; }
  .t-num     { color: #D4A8A8; }
}

/* ══ 模型组区 ═════════════════════════════════════ */
.sec {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.sec-hd {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
}

.sec-hd-left {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sec-title {
  font-size: 22px;
  font-weight: 800;
  color: var(--el-text-color-primary);
  margin: 0;
  letter-spacing: -0.3px;
}

.sec-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0;
  line-height: 1.5;

  code {
    font-family: 'Fira Code', monospace;
    font-size: 12px;
    background: rgba(43,168,162,0.08);
    color: #1A8B85;
    padding: 1px 5px;
    border-radius: 4px;
    border: 1px solid rgba(43,168,162,0.15);
  }
}

/* ══ 响应式 ═══════════════════════════════════════ */
@media (max-width: 1000px) {
  .hero {
    grid-template-columns: 1fr;
    padding: 32px 30px;
    gap: 28px;
  }
}

@media (max-width: 640px) {
  .uep { width: 100%; gap: 24px; }
  .hero { padding: 24px 20px; border-radius: 14px; }
  .hero-h1 { font-size: 28px; }
  .sec-title { font-size: 18px; }

  /* URL 框：小屏幕竖排 */
  .url-box {
    flex-wrap: wrap;
  }
  .url-prefix {
    width: 100%;
    border-right: none;
    border-bottom: 1px solid #D8EEEC;
    padding: 8px 12px;
    height: auto;
  }
  .url-value {
    flex: 1;
    min-width: 0;
    padding: 10px 14px;
    font-size: 12px;
    white-space: normal;
    word-break: break-all;
    overflow: visible;
  }
  .url-btn {
    flex-shrink: 0;
    padding: 0 14px;
  }

  /* 代码窗口：小屏幕字体缩小 + 滚动 */
  .code-body {
    font-size: 11px;
    padding: 16px;
    line-height: 1.65;
    word-break: break-all;
  }
  .code-bar {
    padding: 10px 14px;
    border-radius: 12px 12px 0 0;
  }
  .code-win {
    border-radius: 12px;
  }
  .code-body {
    border-radius: 0 0 12px 12px;
  }
}

/* ══ 深色模式 ════════════════════════════════════ */
:global(html.dark) .hero {
  background: linear-gradient(145deg, #102826 0%, #0E1F1E 55%, #131820 100%);
  border-color: #1A3A38;

  &::before {
    background: radial-gradient(circle, rgba(43,168,162,0.15) 0%, transparent 65%);
  }
}

:global(html.dark) .hero-h1 { color: #E0F5F3; }
:global(html.dark) .hero-sub { color: #5A8888; }
:global(html.dark) .hero-tag { background: rgba(43,168,162,0.12); border-color: rgba(43,168,162,0.25); }

:global(html.dark) .url-box {
  background: #0A1B1A;
  border-color: #1A3A38;
  box-shadow: none;
}
:global(html.dark) .url-prefix { background: rgba(43,168,162,0.07); border-color: #1A3A38; }
:global(html.dark) .url-value { color: #4DC8C2; }

:global(html.dark) .steps { background: #0A1B1A; border-color: #1A3A38; }
:global(html.dark) .step { border-bottom-color: #132524; }
:global(html.dark) .step-txt b { color: #D0EEEC; }
:global(html.dark) .step-txt span { color: #3A6868; }
</style>
