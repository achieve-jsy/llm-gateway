# ops-console 详细设计

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.0 | 2026-05-21 | 初稿创建 | OpenCode |
| v1.1 | 2026-05-23 | 新增渠道管理页、模型管理页（CHG-20260521-002） | OpenCode |
| v1.2 | 2026-05-25 | 新增员工自服务页面详细设计 — CHG-20260523-001 | OpenCode |
| v1.3 | 2026-05-26 | 新增模型组管理组件详细设计 — CHG-20260525-001 | OpenCode |

---

## 1. 背景与目标

### 1.1 业务背景

公司统一大模型网关（llm-gateway）需要一个管理后台入口，让平台管理员快速了解网关统一接入能力范围，并实时查看当前可用模型列表。本模块为 ops-console 中的"统一接入"菜单页，是管理后台的新增页面。

### 1.2 设计目标

- 提供统一接入能力说明的可视化展示，包含支持能力列表和明确不做项列表
- 提供模型发现承接区，实时展示 gateway-api 当前注册的可用模型
- 页面以信息展示为主，交互简洁，加载快速
- 复用后台已有认证和导航框架，零额外基础设施开销

### 1.3 非目标

- 不涉及模型的创建、编辑、删除等管理操作
- 不涉及 Prompt 管理、Agent 编排等功能
- 不涉及用量配额与限流配置

---

## 2. 模块职责边界

| 维度 | 范围 |
| :--- | :--- |
| 所属系统 | ops-console（运维管理后台） |
| 页面路由 | /access/unified-entry |
| 核心职责 | 展示统一接入能力说明（静态）+ 展示可用模型列表（动态） |
| 数据来源 | 能力说明：前端静态 JSON 配置；模型列表：GET /v1/models（gateway-api） |
| 认证方式 | 复用后台 SSO + JWT Cookie，无额外认证逻辑 |
| 与其他模块关系 | 作为 gateway-api 的前端消费方，仅读取数据不写入 |
| 不负责 | 模型管理 CRUD、权限配置、日志审计、监控告警 |

---

## 3. 技术选型

| 类别 | 选型 | 版本 | 选型理由 |
| :--- | :--- | :--- | :--- |
| 框架 | Vue 3 | 3.x | 后台系统统一技术栈 |
| 语言 | TypeScript | 5.x | 类型安全，提升代码可维护性 |
| UI 组件库 | Element Plus | 最新稳定版 | 后台系统统一组件库 |
| 构建工具 | Vite | 5.x | 开发体验好，构建速度快 |
| 状态管理 | Pinia（备选） | 最新稳定版 | 本页面状态简单，实际使用组件内 ref 管理 |
| 路由 | Vue Router | 4.x | 后台系统统一路由方案 |
| HTTP 客户端 | Axios | 1.x | 后台系统统一 HTTP 库，含拦截器机制 |

---

## 4. 界面设计

### 4.1 整体布局结构

页面运行在后台管理系统的主内容区内，左侧为导航栏，右侧为内容区。采用 12 列网格布局，间距为 8px 倍数。

```
┌──────────┬──────────────────────────────────────────────────────┐
│          │                                                      │
│  后台    │  主内容区                                             │
│  Sidebar │  ┌──────────────────────────────────────────────┐   │
│          │  │  页面标题：统一接入                             │   │
│  ┌────┐  │  │  Inter Semi Bold 24px  #FFFFFF                │   │
│  │统一│  │  ├──────────────────────────────────────────────┤   │
│  │接入│  │  │                                              │   │
│  │    │  │  │  能力说明区（CapabilitySection）               │   │
│  └────┘  │  │  背景 #1E1E1E  border-radius 16px           │   │
│          │  │  padding 20px  border 1px solid #2C2C2E      │   │
│  其他    │  │                                              │   │
│  菜单    │  ├──────────────────────────────────────────────┤   │
│  ...     │  │                                              │   │
│          │  │  模型发现承接区（ModelDiscoverySection）        │   │
│          │  │  卡片网格  背景 #121212                       │   │
│          │  │                                              │   │
│          │  └──────────────────────────────────────────────┘   │
│          │                                                      │
└──────────┴──────────────────────────────────────────────────────┘
```

**视觉规范**：

| 元素 | 样式 |
| :--- | :--- |
| 主背景色 | #121212 |
| 卡片表面色 | #1E1E1E |
| 强调色（主按钮/链接） | #00E5FF |
| 正常状态色 | #32D74B |
| 告警/异常色 | #FF453A |
| 主文字色 | #FFFFFF |
| 次要标签色 | #98989D |
| 卡片圆角 | border-radius: 16px |
| 卡片内边距 | padding: 20px |
| 卡片边框 | 1px solid #2C2C2E |
| 表格行底边框 | 1px solid #2C2C2E |
| 表格 hover 背景 | #252525 |
| 标题字体 | Inter Semi Bold 24px |
| 数值字体 | JetBrains Mono Bold 32px |
| 正文字体 | Inter Regular 14px |
| 网格系统 | 12 列，间距 8px 倍数 |
| 告警背景 | #3A1C1C，文字 #FF453A，左边框 4px solid #FF453A |

### 4.2 核心页面与功能区

**统一接入能力页线框图**：

```
┌─────────────────────────────────────────────────────────────────┐
│  统一接入                                    Inter Semi Bold 24px│
│  #FFFFFF                                                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │  卡片：能力说明区   #1E1E1E  radius:16px  border:#2C2C2E  │ │
│  │                                                           │ │
│  │  ┌─────────────────────────────────────────────────────┐ │ │
│  │  │  ✓ 本次支持能力                                      │ │ │
│  │  │  ├─ ✅ OpenAI 兼容接口                               │ │ │
│  │  │  │     支持 /v1/chat/completions 等标准接口           │ │ │
│  │  │  ├─ ✅ 多厂商模型统一接入                             │ │ │
│  │  │  │     通过统一网关调用多家大模型厂商服务               │ │ │
│  │  │  └─ ✅ 模型路由与负载均衡                             │ │ │
│  │  │        按模型名称路由，具备基础负载均衡                  │ │ │
│  │  └─────────────────────────────────────────────────────┘ │ │
│  │                                                           │ │
│  │  ┌─────────────────────────────────────────────────────┐ │ │
│  │  │  ✗ 明确不做项                                        │ │ │
│  │  │  ├─ ❌ Prompt 管理（不在本次交付范围内）               │ │ │
│  │  │  ├─ ❌ Agent/工作流编排（不在本次交付范围内）          │ │ │
│  │  │  └─ ❌ 用量配额与限流配置（由 control-plane 承接）    │ │ │
│  │  └─────────────────────────────────────────────────────┘ │ │
│  └───────────────────────────────────────────────────────────┘ │
│                                                                 │
│  模型发现                                   Inter Semi Bold 20px│
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │ #1E1E1E      │  │ #1E1E1E      │  │ #1E1E1E      │         │
│  │              │  │              │  │              │         │
│  │  gpt-4o      │  │  qwen-turbo  │  │  glm-4       │         │
│  │  #FFFFFF     │  │  #FFFFFF     │  │  #FFFFFF     │         │
│  │              │  │              │  │              │         │
│  │  openai      │  │  alibaba     │  │  zhipu       │         │
│  │  #98989D     │  │  #98989D     │  │  #98989D     │         │
│  │              │  │              │  │              │         │
│  │  radius:16px │  │  radius:16px │  │  radius:16px │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**功能区说明**：

| 功能区 | 数据来源 | 展示内容 | 交互行为 |
| :--- | :--- | :--- | :--- |
| 页面标题区 | 静态 | "统一接入"标题文字 | 无 |
| 能力说明区 | 静态 JSON 配置 | 支持能力列表 + 不做项列表 | 纯展示，无交互 |
| 模型发现区 | GET /v1/models | 模型卡片网格（名称+厂商） | 加载态/空态/错误态切换；错误态提供刷新按钮 |

### 4.3 关键交互设计

| 交互场景 | 触发条件 | UI 行为 | 结果状态 |
| :--- | :--- | :--- | :--- |
| 进入页面 | 用户点击 Sidebar"统一接入"菜单 | 路由跳转，能力区即时渲染，模型区展示骨架屏 | 能力区就绪 + 模型区加载中 |
| 模型加载成功 | /v1/models 返回非空列表 | 骨架屏消失，模型卡片网格渲染 | 正常展示态 |
| 模型列表为空 | /v1/models 返回空数组 | 展示空态提示"暂无可用模型信息" | 空态 |
| 模型加载失败 | 网络异常或服务端错误 | 展示告警提示 + 刷新按钮 | 错误态 |
| 点击刷新 | 用户在错误态点击刷新按钮 | 重新进入加载态，再次请求 /v1/models | 回到加载态 |
| 认证过期 | /v1/models 返回 401 | 后台统一拦截器重定向到登录页 | 离开当前页 |

### 4.4 响应式与适配

仅桌面端。本页面运行于后台管理系统，仅面向桌面端浏览器（最小宽度 1200px），不做移动端适配。卡片网格固定 4 列布局（el-col :span="6"）。

### 4.5 渠道管理页（/admin/providers）

**页面线框图**：

```
┌─────────────────────────────────────────────────────────────────────┐
│  渠道管理                                      Inter Semi Bold 24px  │
│  #FFFFFF                                                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─── 操作栏 ─────────────────────────────────────────────────────┐│
│  │  [+ 创建渠道]  #00E5FF 主按钮                                    ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
│  ┌─── 渠道列表表格  #1E1E1E  radius:16px  border:#2C2C2E ────────┐│
│  │ 渠道名称     │ 类型      │ 状态   │ 创建时间        │ 操作           ││
│  ├──────────────┼───────────┼────────┼─────────────────┼─────────────────┤│
│  │ OpenAI生产   │ openai    │ 🟢启用 │ 2026-05-01      │ 编辑│同步│禁用 ││
│  │ Azure备份    │ azure     │ 🔴禁用 │ 2026-05-10      │ 编辑│同步(灰)│启用││
│  │ 千问主力     │ alibaba   │ 🟢启用 │ 2026-05-15      │ 编辑│同步│禁用 ││
│  └──────────────┴───────────┴────────┴─────────────────┴──────────┘│
│                                                                      │
│  状态指示灯：🟢 #32D74B（启用）  🔴 #FF453A（禁用）                 │
│  凭证列显示：sk-****abcd（仅展示尾4位）                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**创建/编辑渠道对话框**：

```
┌───────────────────────────────────────────────┐
│  创建渠道 / 编辑渠道           [×]            │
├───────────────────────────────────────────────┤
│                                               │
│  渠道名称 *   [____________________________] │
│                                               │
│  渠道类型 *   [ openai       ▾ ]             │
│               可选: openai / azure / alibaba  │
│               / zhipu / anthropic / custom    │
│                                               │
│  Endpoint *   [____________________________] │
│               示例: https://api.openai.com    │
│                                               │
│  凭证 (API Key) * [________________________] │
│               编辑模式: 显示 sk-****abcd      │
│               留空则不修改                     │
│                                               │
│  启用状态     [■ 启用] 开关                   │
│                                               │
├───────────────────────────────────────────────┤
│              [取消]     [确认提交] #00E5FF     │
└───────────────────────────────────────────────┘
```

**禁用确认弹窗**：

```
┌───────────────────────────────────────┐
│  ⚠️ 确认禁用                          │
├───────────────────────────────────────┤
│  禁用渠道「{name}」后，其下所有模型   │
│  将不可用，是否确认？                  │
├───────────────────────────────────────┤
│        [取消]      [确认禁用] #FF453A  │
└───────────────────────────────────────┘
```

**同步模型弹窗（SyncModelsDialog）**：

```
┌─────────────────────────────────────────────┐
│ 同步模型 — 百炼                        [×] │
├─────────────────────────────────────────────┤
│ 新增 238 个 │ 已存在 5 个 │ 已下架 1 个   │
├─────────────────────────────────────────────┤
│ 🔍 [搜索模型名称...                     ]  │
├─────────────────────────────────────────────┤
│ ☑ qwen-max                      [新增]    │
│ ☑ qwen-plus                     [新增]    │
│ ☐ qwen-turbo              [已存在] (禁用) │
│ ☑ qwen-vl-max                   [新增]    │
│ ...（滚动）                                │
├─────────────────────────────────────────────┤
│ [☑ 一键全选]        [取消]  [确认注册(3)]  │
└─────────────────────────────────────────────┘
```

**删除确认弹窗**：

```
┌───────────────────────────────────────┐
│  ⚠️ 确认删除                          │
├───────────────────────────────────────┤
│  ❌ 该渠道下仍有 {n} 个模型，         │
│  请先迁移或删除所有模型后再删除渠道。  │
│  （有模型时阻断删除操作）              │
│                                        │
│  ──────── 或 ────────                  │
│                                        │
│  确定删除渠道「{name}」？              │
│  此操作不可恢复。                      │
├───────────────────────────────────────┤
│        [取消]      [确认删除] #FF453A  │
└───────────────────────────────────────┘
```

**功能区说明**：

| 功能区 | 数据来源 | 展示内容 | 交互行为 |
| :--- | :--- | :--- | :--- |
| 操作栏 | 无 | 创建渠道按钮 | 点击打开创建对话框 |
| 渠道列表表格 | GET /api/v1/providers | 渠道名称、类型、状态指示灯、创建时间、操作按钮 | 行 hover 高亮；操作列：编辑/启用禁用/删除 |
| 创建/编辑对话框 | POST/PUT /api/v1/providers | 表单：名称、类型选择、Endpoint、凭证、启用开关 | 必填校验；编辑时凭证脱敏显示，留空不修改 |
| 禁用确认弹窗 | 无 | 影响提示文案 | 确认后 PUT 修改状态 |
| 删除确认弹窗 | GET /api/v1/providers/{id}/models 计数 | 有模型时阻断提示，无模型时确认提示 | 阻断态仅关闭；无模型态确认后 DELETE |
| 同步模型弹窗 | POST /api/v1/providers/{id}/sync-models | 标题含渠道名、统计栏（新增/已存在/已下架）、搜索框、勾选列表、一键全选+确认注册 | 搜索实时过滤；"已存在"checkbox 禁用；一键全选仅选"新增"；确认后 POST confirm 接口 |

### 4.6 模型管理页（/admin/models）

**页面线框图**：

```
┌─────────────────────────────────────────────────────────────────────┐
│  模型管理                                      Inter Semi Bold 24px  │
│  #FFFFFF                                                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─── 操作栏 ─────────────────────────────────────────────────────┐│
│  │  渠道筛选: [ 全部渠道 ▾ ]       [+ 注册模型]  #00E5FF 主按钮   ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
│  ┌─── 模型列表表格  #1E1E1E  radius:16px  border:#2C2C2E ────────┐│
│  │ 模型名称       │ 归属渠道   │ 类型   │ 状态   │ 操作           ││
│  ├────────────────┼────────────┼────────┼────────┼────────────────┤│
│  │ gpt-4o         │ OpenAI生产 │ chat   │ 🟢启用 │ 编辑 │ 删除    ││
│  │ gpt-4o-mini    │ OpenAI生产 │ chat   │ 🟢启用 │ 编辑 │ 删除    ││
│  │ qwen-turbo     │ 千问主力   │ chat   │ 🟢启用 │ 编辑 │ 删除    ││
│  │ text-embedding │ OpenAI生产 │ embed  │ 🔴禁用 │ 编辑 │ 删除    ││
│  └────────────────┴────────────┴────────┴────────┴────────────────┘│
│                                                                      │
│  分页: [< 1 2 3 ... >]   共 {total} 条                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**注册/编辑模型对话框**：

```
┌───────────────────────────────────────────────┐
│  注册模型 / 编辑模型           [×]            │
├───────────────────────────────────────────────┤
│                                               │
│  模型名称 *   [____________________________] │
│               实时校验唯一性（防抖 300ms）     │
│               ✓ 名称可用 / ✗ 名称已存在       │
│                                               │
│  归属渠道 *   [ 请选择渠道     ▾ ]           │
│               下拉加载已启用的渠道列表         │
│                                               │
│  模型类型 *   [ chat           ▾ ]           │
│               可选: chat / completion /       │
│               embedding / image / audio       │
│                                               │
│  启用状态     [■ 启用] 开关                   │
│                                               │
├───────────────────────────────────────────────┤
│              [取消]     [确认提交] #00E5FF     │
└───────────────────────────────────────────────┘
```

**功能区说明**：

| 功能区 | 数据来源 | 展示内容 | 交互行为 |
| :--- | :--- | :--- | :--- |
| 渠道筛选下拉 | GET /api/v1/providers（缓存） | 渠道名称列表 + "全部渠道"选项 | 选择后触发列表刷新，传 provider_id 参数 |
| 注册模型按钮 | 无 | 主操作按钮 | 点击打开注册对话框 |
| 模型列表表格 | GET /api/v1/models?provider_id= | 模型名称、归属渠道名称、类型标签、状态指示灯、操作按钮 | 支持分页；行 hover 高亮 |
| 注册/编辑对话框 | POST/PUT /api/v1/models | 表单：名称（实时校验）、渠道选择、类型选择、启用开关 | 名称唯一性实时校验；归属渠道仅显示已启用的渠道 |
| 删除确认弹窗 | 无 | "确定删除模型 {name}？此操作不可恢复。" | 确认后 DELETE /api/v1/models/{id} |

---

## 5. 架构概述

```mermaid
graph TD
    App["App.vue（后台根组件）"]
    Sidebar["Sidebar 导航菜单"]
    Router["Vue Router"]
    Page["UnifiedEntryPage.vue（页面容器）"]
    CapSection["CapabilitySection.vue（能力说明区）"]
    ModelSection["ModelDiscoverySection.vue（模型发现区）"]
    ModelCard["ModelCard.vue（模型卡片）"]
    StaticJSON["capability.json（静态配置）"]
    API["gateway-api /v1/models"]

    ProvPage["ProviderListPage.vue（渠道管理页）"]
    ProvTable["ProviderTable.vue（渠道列表表格）"]
    ProvDialog["ProviderFormDialog.vue（创建/编辑对话框）"]
    SyncDlg["SyncModelsDialog.vue（同步结果弹窗）"]
    ProvAPI["control-plane /api/v1/providers"]

    ModelPage["ModelListPage.vue（模型管理页）"]
    ModelTable["ModelTable.vue（模型列表表格）"]
    ModelDialog["ModelFormDialog.vue（注册/编辑对话框）"]
    ModelAPI["control-plane /api/v1/models"]

    App --> Sidebar
    Sidebar -->|"点击统一接入"| Router
    Sidebar -->|"点击渠道管理"| Router
    Sidebar -->|"点击模型管理"| Router
    Router -->|"/access/unified-entry"| Page
    Router -->|"/admin/providers"| ProvPage
    Router -->|"/admin/models"| ModelPage
    Page --> CapSection
    Page --> ModelSection
    ModelSection --> ModelCard
    CapSection -.->|"读取"| StaticJSON
    Page -.->|"GET 请求"| API

    ProvPage --> ProvTable
    ProvPage --> ProvDialog
    ProvPage --> SyncDlg
    ProvPage -.->|"CRUD 请求"| ProvAPI
    SyncDlg -.->|"同步确认请求"| ProvAPI

    ModelPage --> ModelTable
    ModelPage --> ModelDialog
    ModelPage -.->|"CRUD 请求"| ModelAPI
    ModelPage -.->|"获取渠道列表"| ProvAPI
```

**组件层级**：

| 层级 | 组件 | 职责 |
| :--- | :--- | :--- |
| 页面容器 | UnifiedEntryPage.vue | 页面状态管理、接口调用、向子组件分发数据 |
| 功能区组件 | CapabilitySection.vue | 渲染能力说明列表（纯展示） |
| 功能区组件 | ModelDiscoverySection.vue | 渲染模型列表多态（加载/正常/空/错误） |
| 展示组件 | ModelCard.vue | 单张模型卡片渲染 |
| 页面容器 | ProviderListPage.vue | 渠道管理页状态管理、CRUD 接口调用、对话框控制 |
| 功能区组件 | ProviderTable.vue | 渲染渠道列表表格（含状态指示灯、脱敏凭证、操作按钮） |
| 功能区组件 | ProviderFormDialog.vue | 创建/编辑渠道表单对话框（含表单校验） |
| 功能区组件 | SyncModelsDialog.vue | 同步结果弹窗：展示模型列表、关键词过滤、勾选、一键全选、确认注册 |
| 页面容器 | ModelListPage.vue | 模型管理页状态管理、CRUD 接口调用、筛选逻辑 |
| 功能区组件 | ModelTable.vue | 渲染模型列表表格（含分页、状态、操作） |
| 功能区组件 | ModelFormDialog.vue | 注册/编辑模型表单对话框（含名称唯一性校验） |

**路由配置**：

```typescript
{
  path: '/access/unified-entry',
  name: 'UnifiedEntry',
  component: () => import('@/views/access/UnifiedEntryPage.vue'),
  meta: { title: '统一接入', icon: 'Connection' }
},
{
  path: '/admin/providers',
  name: 'ProviderList',
  component: () => import('@/views/admin/ProviderListPage.vue'),
  meta: { title: '渠道管理', icon: 'Office' }
},
{
  path: '/admin/models',
  name: 'ModelList',
  component: () => import('@/views/admin/ModelListPage.vue'),
  meta: { title: '模型管理', icon: 'Cpu' }
}
```

---

## 6. 关键流程设计

### 6.1 页面加载主流程

```mermaid
sequenceDiagram
    participant User as 平台管理员
    participant Nav as Sidebar 导航
    participant Router as Vue Router
    participant Page as UnifiedEntryPage.vue
    participant Cap as CapabilitySection.vue
    participant Model as ModelDiscoverySection.vue
    participant GW as gateway-api

    User->>Nav: 点击"统一接入"菜单
    Nav->>Router: push('/access/unified-entry')
    Router->>Page: 加载页面组件
    Page->>Cap: 传入静态配置 props
    Cap-->>User: 渲染能力说明（支持项 + 不做项）
    Page->>Model: 传入 loading=true
    Model-->>User: 展示骨架屏加载态
    Page->>GW: GET /v1/models（JWT Cookie 自动携带）
    alt 请求成功且有数据
        GW-->>Page: 200 返回模型列表
        Page->>Model: 传入 models 数据，loading=false
        Model-->>User: 渲染模型卡片列表
    else 请求成功但无数据
        GW-->>Page: 200 返回空列表
        Page->>Model: 传入空数组，loading=false
        Model-->>User: 展示"暂无可用模型信息"空态
    else 认证失败
        GW-->>Page: 401 Unauthorized
        Page->>Router: 触发后台统一 401 拦截器
        Router-->>User: 重定向到登录页
    else 请求失败
        GW-->>Page: 错误响应或网络异常
        Page->>Model: 传入 error 信息，loading=false
        Model-->>User: 展示错误提示与刷新按钮
    end
```

### 6.2 手动刷新流程

```mermaid
sequenceDiagram
    participant User as 平台管理员
    participant Model as ModelDiscoverySection.vue
    participant Page as UnifiedEntryPage.vue
    participant GW as gateway-api

    User->>Model: 点击"刷新"按钮
    Model->>Page: emit('refresh')
    Page->>Page: 重置 error=null, loading=true
    Page->>Model: 更新 loading=true
    Model-->>User: 展示骨架屏
    Page->>GW: GET /v1/models
    GW-->>Page: 响应结果
    Page->>Model: 更新 models/error/loading
    Model-->>User: 渲染最终状态
```

### 6.3 渠道 CRUD 流程

```mermaid
sequenceDiagram
    participant User as 平台管理员
    participant Page as ProviderListPage.vue
    participant Dialog as ProviderFormDialog.vue
    participant CP as control-plane

    Note over User,CP: 查看渠道列表
    User->>Page: 进入渠道管理页
    Page->>CP: GET /api/v1/providers
    CP-->>Page: 200 渠道列表（凭证已脱敏）
    Page-->>User: 渲染渠道表格

    Note over User,CP: 创建渠道
    User->>Page: 点击"创建渠道"
    Page->>Dialog: 打开空白表单对话框
    User->>Dialog: 填写名称/类型/Endpoint/凭证，开启启用
    User->>Dialog: 点击"确认提交"
    Dialog->>Dialog: 前端表单校验（必填项）
    Dialog->>CP: POST /api/v1/providers { name, type, endpoint, credential, enabled }
    alt 创建成功
        CP-->>Dialog: 201 Created
        Dialog->>Page: emit('success')
        Page->>CP: GET /api/v1/providers（刷新列表）
        Page-->>User: ElMessage.success('渠道创建成功')
    else 名称重复
        CP-->>Dialog: 409 Conflict
        Dialog-->>User: 表单名称字段错误提示"渠道名称已存在"
    end

    Note over User,CP: 编辑渠道
    User->>Page: 点击某行"编辑"
    Page->>Dialog: 打开对话框，回填数据（凭证显示 ****尾4位）
    User->>Dialog: 修改字段（凭证留空则不修改）
    User->>Dialog: 点击"确认提交"
    Dialog->>CP: PUT /api/v1/providers/{id} { ...changes }
    CP-->>Dialog: 200 OK
    Dialog->>Page: emit('success')
    Page-->>User: ElMessage.success('渠道更新成功')

    Note over User,CP: 禁用渠道
    User->>Page: 点击某行"禁用"
    Page-->>User: 弹出确认框"禁用后其下所有模型将不可用"
    User->>Page: 点击"确认禁用"
    Page->>CP: PUT /api/v1/providers/{id} { enabled: false }
    CP-->>Page: 200 OK
    Page-->>User: 刷新列表，状态变为🔴禁用

    Note over User,CP: 删除渠道
    User->>Page: 点击某行"删除"
    Page->>CP: GET /api/v1/providers/{id}/models（检查关联模型）
    alt 存在关联模型
        CP-->>Page: 200 { count: n }
        Page-->>User: 弹窗阻断"该渠道下仍有 n 个模型，请先删除"
    else 无关联模型
        CP-->>Page: 200 { count: 0 }
        Page-->>User: 弹出确认框"确定删除渠道？不可恢复"
        User->>Page: 点击"确认删除"
        Page->>CP: DELETE /api/v1/providers/{id}
        CP-->>Page: 204 No Content
        Page-->>User: 刷新列表，ElMessage.success('渠道已删除')
    end
```

### 6.4 模型 CRUD 流程

```mermaid
sequenceDiagram
    participant User as 平台管理员
    participant Page as ModelListPage.vue
    participant Dialog as ModelFormDialog.vue
    participant CP as control-plane

    Note over User,CP: 查看模型列表（含筛选）
    User->>Page: 进入模型管理页
    Page->>CP: GET /api/v1/providers（加载渠道下拉选项）
    Page->>CP: GET /api/v1/models?page=1&size=20
    CP-->>Page: 200 模型分页列表
    Page-->>User: 渲染模型表格 + 渠道筛选下拉

    User->>Page: 选择渠道筛选"OpenAI生产"
    Page->>CP: GET /api/v1/models?provider_id={id}&page=1&size=20
    CP-->>Page: 200 筛选后的模型列表
    Page-->>User: 刷新表格

    Note over User,CP: 注册模型
    User->>Page: 点击"注册模型"
    Page->>Dialog: 打开空白表单对话框（传入渠道列表）
    User->>Dialog: 输入模型名称
    Dialog->>CP: GET /api/v1/models/check-name?name={input}（防抖 300ms）
    alt 名称可用
        CP-->>Dialog: 200 { available: true }
        Dialog-->>User: ✓ 名称可用（绿色提示）
    else 名称已存在
        CP-->>Dialog: 200 { available: false }
        Dialog-->>User: ✗ 名称已存在（红色提示，禁用提交按钮）
    end
    User->>Dialog: 选择归属渠道、类型、启用状态
    User->>Dialog: 点击"确认提交"
    Dialog->>CP: POST /api/v1/models { name, provider_id, type, enabled }
    CP-->>Dialog: 201 Created
    Dialog->>Page: emit('success')
    Page-->>User: 刷新列表，ElMessage.success('模型注册成功')

    Note over User,CP: 编辑模型
    User->>Page: 点击某行"编辑"
    Page->>Dialog: 打开对话框，回填数据
    User->>Dialog: 修改字段
    Dialog->>CP: PUT /api/v1/models/{id} { ...changes }
    CP-->>Dialog: 200 OK
    Dialog->>Page: emit('success')
    Page-->>User: ElMessage.success('模型更新成功')

    Note over User,CP: 删除模型
    User->>Page: 点击某行"删除"
    Page-->>User: 弹出确认框"确定删除模型 {name}？不可恢复"
    User->>Page: 点击"确认删除"
    Page->>CP: DELETE /api/v1/models/{id}
    CP-->>Page: 204 No Content
    Page-->>User: 刷新列表，ElMessage.success('模型已删除')
```

### 6.5 同步模型流程

```mermaid
sequenceDiagram
    participant User as 管理员
    participant Btn as 同步按钮
    participant API as control-plane
    participant Dlg as SyncModelsDialog

    User->>Btn: 点击"同步模型"
    Btn->>API: POST /providers/:id/sync-models
    API-->>Btn: SyncResult
    Btn->>Dlg: 打开弹窗(result)
    User->>Dlg: 输入关键词过滤
    User->>Dlg: 一键全选 / 手动勾选
    User->>Dlg: 确认注册
    Dlg->>API: POST /providers/:id/sync-models/confirm
    API-->>Dlg: {registered: 5}
    Dlg-->>User: 成功提示 + 关闭弹窗
```

---

## 7. 数据流转

### 7.1 数据源分类

| 数据 | 来源 | 获取方式 | 更新频率 |
| :--- | :--- | :--- | :--- |
| 能力说明配置 | src/config/capability.json | 构建时打包，import 静态读取 | 仅随前端发版更新 |
| 可用模型列表 | gateway-api GET /v1/models | 页面 onMounted 异步请求 | 每次进入页面实时获取 |

### 7.2 数据流向图

```
静态 JSON 配置 ──import──→ UnifiedEntryPage ──props──→ CapabilitySection ──渲染──→ 能力列表 UI

gateway-api ──HTTP GET──→ UnifiedEntryPage ──映射──→ ModelInfo[] ──props──→ ModelDiscoverySection ──循环──→ ModelCard ──渲染──→ 卡片 UI
```

### 7.3 API 响应映射

| 原始字段（snake_case） | 前端字段（camelCase） | 说明 |
| :--- | :--- | :--- |
| data[].id | ModelInfo.id | 模型唯一标识 |
| data[].owned_by | ModelInfo.ownedBy | 归属厂商 |
| data[].object | 不映射 | 前端不使用 |
| data[].created | 不映射 | 前端不使用 |

---

## 8. 状态管理设计

### 8.1 状态管理方案选择

本页面状态简单（一个模型列表 + 加载状态 + 错误状态），采用组件内 `ref`/`reactive` 管理，不引入 Pinia store。

**不使用 Pinia 的理由**：
- 页面为纯展示型，无跨组件/跨页面状态共享需求
- 数据不需要持久化或缓存
- 组件层级仅 2 层，props 传递足够

### 8.2 页面状态定义

```typescript
interface UnifiedEntryPageState {
  models: ModelInfo[]
  loading: boolean
  error: string | null
}

// 页面组件内部
const state = reactive<UnifiedEntryPageState>({
  models: [],
  loading: false,
  error: null
})
```

### 8.3 状态流转图

```mermaid
stateDiagram-v2
    [*] --> Idle
    Idle --> Loading : onMounted 触发加载
    Loading --> Success : 请求成功（有数据）
    Loading --> Empty : 请求成功（空数组）
    Loading --> Error : 请求失败
    Error --> Loading : 用户点击刷新
    Success --> [*]
    Empty --> [*]
```

### 8.4 Props 传递关系

| 父组件 | 子组件 | Props | 类型 |
| :--- | :--- | :--- | :--- |
| UnifiedEntryPage | CapabilitySection | config | CapabilityConfig |
| UnifiedEntryPage | ModelDiscoverySection | models | ModelInfo[] |
| UnifiedEntryPage | ModelDiscoverySection | loading | boolean |
| UnifiedEntryPage | ModelDiscoverySection | error | string \| null |
| ModelDiscoverySection | ModelCard | model | ModelInfo |
| ProviderListPage | SyncModelsDialog | syncResult | SyncResult |
| ProviderListPage | SyncModelsDialog | visible | boolean |

### 8.5 SyncModelsDialog 状态管理

组件内 ref 管理，无需 Pinia store。

| 状态 | 类型 | 说明 |
| :--- | :--- | :--- |
| syncResult | SyncResult \| null | 同步结果数据 |
| filterKeyword | string | 搜索关键词 |
| selectedModels | string[] | 勾选的模型名列表 |
| confirming | boolean | 确认注册 loading |

---

## 9. 错误处理与降级策略

| 异常场景 | 影响范围 | UI 降级策略 | 技术实现 |
| :--- | :--- | :--- | :--- |
| /v1/models 网络超时 | 仅模型发现区 | 展示告警"网络异常，请刷新重试"（背景 #3A1C1C，文字 #FF453A，左边框 4px solid #FF453A），附刷新按钮 | try/catch 捕获 axios 网络错误，设置 error 状态 |
| /v1/models 返回 401 | 全局 | 后台统一拦截器重定向到登录页 | 复用后台 axios 拦截器，401 自动跳转 |
| /v1/models 返回 403 | 仅模型发现区 | 展示告警"暂无权限查看模型信息" | 根据 HTTP 状态码设置对应 error 文案 |
| /v1/models 返回 500/503 | 仅模型发现区 | 展示告警"服务暂时不可用，请稍后重试" | 区分服务端错误码给出对应提示 |
| /v1/models 返回空列表 | 仅模型发现区 | 展示空态"暂无可用模型信息" | 判断 data 数组长度为 0 |
| 能力配置 JSON 格式异常 | 仅能力说明区 | 不展示能力说明区内容，控制台输出警告 | 配置读取增加 try/catch，异常时降级为空配置 |
| 页面静态资源加载失败 | 全局 | 复用后台统一 CDN 降级和错误页 | 后台整体部署策略保障 |

**降级原则**：
- 错误隔离：模型发现区的错误不影响能力说明区的正常展示
- 不自动重试：提供手动刷新按钮由用户决定是否重试
- 认证统一处理：401 由后台拦截器全局处理，页面无需额外逻辑

### 9.2 渠道管理页错误处理

| 异常场景 | 影响范围 | UI 降级策略 | 技术实现 |
| :--- | :--- | :--- | :--- |
| GET /api/v1/providers 网络超时 | 渠道列表区 | 表格区展示告警"网络异常，请刷新重试"，附刷新按钮 | try/catch + error 状态驱动 |
| GET /api/v1/providers 返回 401 | 全局 | 后台统一拦截器重定向到登录页 | 复用 axios 拦截器 |
| POST 创建渠道返回 409 | 创建对话框 | 表单名称字段下方红色提示"渠道名称已存在" | 根据响应码设置字段级错误 |
| POST/PUT 请求返回 422 | 对话框 | 解析 errors 字段，在对应表单项下方展示校验错误 | 后端返回字段级错误映射到 el-form-item |
| DELETE 渠道返回 409（仍有模型） | 删除弹窗 | 阻断提示"该渠道下仍有模型，请先删除相关模型" | 前置检查 + 后端双重保护 |
| PUT 禁用渠道返回 500 | 操作反馈 | ElMessage.error('操作失败，请稍后重试') | catch 中统一 toast 提示 |
| POST sync-models 返回 400 (sync_not_supported) | 同步弹窗 | 弹窗显示"该渠道类型不支持自动同步，请手动注册或使用预置模型" | 根据 error.type 判断弹窗替换为提示信息 |
| POST sync-models 返回 400 (provider_disabled) | 同步按钮 | ElMessage.warning('请先启用渠道') | 渠道禁用时按钮置灰，后端双重保护 |
| POST sync-models 返回 502 (upstream_error) | 同步按钮 | ElMessage.error('厂商接口调用失败：' + message) | catch 中统一 toast 提示 |
| POST sync-models/confirm 返回 500 | 同步弹窗 | ElMessage.error('注册失败，请稍后重试') | catch 中统一 toast 提示，弹窗不关闭 |

### 9.3 模型管理页错误处理

| 异常场景 | 影响范围 | UI 降级策略 | 技术实现 |
| :--- | :--- | :--- | :--- |
| GET /api/v1/models 网络超时 | 模型列表区 | 表格区展示告警"网络异常，请刷新重试"，附刷新按钮 | try/catch + error 状态驱动 |
| GET /api/v1/models 返回 401 | 全局 | 后台统一拦截器重定向到登录页 | 复用 axios 拦截器 |
| 模型名称唯一性校验网络异常 | 对话框内名称字段 | 校验提示变为"无法验证，请稍后重试"，不阻断提交 | 校验请求 catch 后降级为允许提交，由后端兜底 |
| POST 注册模型返回 409 | 注册对话框 | 表单名称字段下方红色提示"模型名称已存在" | 响应码 409 映射到字段级错误 |
| POST/PUT 请求返回 422 | 对话框 | 解析 errors 字段，在对应表单项下方展示校验错误 | 后端返回字段级错误映射到 el-form-item |
| DELETE 模型返回 500 | 操作反馈 | ElMessage.error('删除失败，请稍后重试') | catch 中统一 toast 提示 |
| 渠道下拉加载失败 | 筛选区 + 对话框 | 筛选下拉显示"加载失败"禁用态；对话框渠道选择显示"加载失败，请关闭重试" | 独立 error 状态控制渠道下拉 |

---

## 10. 性能设计

| 优化点 | 方案 | 预期效果 |
| :--- | :--- | :--- |
| 页面组件懒加载 | 路由级别使用 `() => import()` 动态导入 | 不访问该页面时不加载对应 JS |
| 静态配置零请求 | capability.json 构建时内联打包 | 能力说明区无需额外网络请求 |
| 骨架屏体验 | 模型列表加载期间展示 3 个卡片骨架 | 减少用户感知的白屏时间 |
| 请求无缓存 | 每次进入页面实时请求最新模型列表 | 保证数据实时性，接口轻量无需缓存 |
| 资源体积控制 | Element Plus 按需导入，仅引入使用到的组件 | 减小打包体积 |

**性能预期**：
- 页面首次渲染（能力说明区）：< 200ms（静态数据，无网络依赖）
- 模型列表加载完成：< 1s（/v1/models 接口轻量，预期响应 < 500ms）
- 页面 JS 体积：< 50KB gzipped（仅含展示逻辑，无复杂交互）

---

## 11. i18n 与 a11y

本次仅中文，不做国际化；无障碍要求仅菜单和标题可辨。

---

## 12. 测试矩阵

| 测试类型 | 测试范围 | 验证内容 |
| :--- | :--- | :--- |
| 单元测试 | ModelCard.vue | 正确渲染模型名称和厂商 |
| 单元测试 | CapabilitySection.vue | 正确渲染支持项和不做项列表 |
| 单元测试 | ModelDiscoverySection.vue | 正确展示加载态/正常态/空态/错误态 |
| 单元测试 | mapModelsResponse 函数 | snake_case 正确映射为 camelCase |
| 集成测试 | UnifiedEntryPage.vue | 页面加载后能力区即时渲染、模型区异步加载完成 |
| 集成测试 | 错误处理 | 模拟接口 500/403/网络异常，验证错误态展示 |
| 集成测试 | 刷新操作 | 错误态点击刷新后重新请求并正确切换状态 |
| E2E 测试 | 完整页面流程 | 从 Sidebar 点击进入页面，验证能力区和模型区正常展示 |

---

## 13. 设计决策记录

### 决策 4：能力说明驱动方式

| 项 | 内容 |
| :--- | :--- |
| 问题 | 能力范围和不做项信息如何提供给前端页面展示？ |
| 选择 | 能力范围和不做项作为页面内嵌静态配置（JSON），模型发现区调用 gateway-api /v1/models 动态加载 |
| 理由 | 能力说明属于产品定义范畴，变更频率极低，无需引入后端接口增加复杂度；模型发现信息来自 gateway 实时数据，必须动态获取保证准确性 |
| 备选方案 | 全部通过后端接口获取——增加后端工作量且能力说明变更仍需发版，不如直接前端配置简洁 |

### 决策 5：模型发现展示形态

| 项 | 内容 |
| :--- | :--- |
| 问题 | 模型发现区以何种形态展示模型信息？ |
| 选择 | 模型卡片列表，每张卡片展示模型名称和归属厂商；空态时显示"暂无可用模型信息" |
| 理由 | 卡片形态信息密度适中、视觉辨识度高，适合展示少量关键属性；模型名称和厂商是接入者最关心的核心信息 |
| 备选方案 | 表格列表形式——信息展示更紧凑但视觉吸引力不足，不适合承接展示场景 |

### 决策 6：不引入 Pinia store

| 项 | 内容 |
| :--- | :--- |
| 问题 | 页面状态管理是否需要使用 Pinia？ |
| 选择 | 使用组件内 ref/reactive 管理，不引入 Pinia store |
| 理由 | 页面纯展示、无跨组件共享、无数据持久化需求，组件层级仅 2 层 props 足够 |
| 备选方案 | 使用 Pinia store——增加复杂度但无实际收益 |


---

## 14. 变更记录 — CHG-20260523-001

## 变更摘要

新增 OIDC 登录回调与员工会话管理（LoginCallback.vue + AuthStore）、Key 自助管理页面与创建弹窗（KeyListPage.vue + KeyCreateDialog.vue）、模型可用列表页面（ModelListPage.vue）、首页欢迎与使用文档页（HomePage.vue）；引入 auth.ts API 模块；控制平面 API 模块新增 Key 管理方法；路由新增员工端入口；导航栏按认证状态条件渲染。

---

## 新增页面

### HomePage.vue — 首页欢迎与使用文档

**路由**：`/home`（替代原有 `/` 作为员工登录后落地页）

**职责**：展示欢迎信息（当前登录员工姓名 + 所属部门）+ API 使用文档（curl 示例 + OpenAI SDK 示例）+ 快捷跳转至"我的 Key"和"可用模型"入口。

```
┌─────────────────────────────────────────────────────────────────────┐
│  你好，{name}                                Inter Semi Bold 24px    │
│  部门：{departmentName}                        #98989D 14px          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─── 快速入口 ─────────────────────────────────────────────────┐  │
│  │  [我的 Key →]                          [可用模型 →]          │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌─── 使用文档 ─ 背景 #1E1E1E radius:16px ──────────────────────┐  │
│  │                                                                  │
│  │  curl 示例：                                                     │
│  │  ┌────────────────────────────────────────────────────────────┐│
│  │  │ curl https://api.example.com/v1/chat/completions \        ││
│  │  │   -H "Content-Type: application/json" \                   ││
│  │  │   -H "Authorization: Bearer $API_KEY" \                   ││
│  │  │   -d '{ "model": "gpt-4o", "messages": [...] }'           ││
│  │  └────────────────────────────────────────────────────────────┘│
│  │                                                                  │
│  │  OpenAI SDK 示例：                                               │
│  │  ┌────────────────────────────────────────────────────────────┐│
│  │  │ import OpenAI                                              ││
│  │  │ client = OpenAI(base_url="https://api.example.com/v1",    ││
│  │  │                 api_key="$API_KEY")                        ││
│  │  │ response = client.chat.completions.create(                 ││
│  │  │     model="gpt-4o", messages=[{"role":"user","content":".."}]│
│  │  │ )                                                          ││
│  │  └────────────────────────────────────────────────────────────┘│
│  └──────────────────────────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────────────┘
```

**边界态**：
- 员工无 Key → 文档区上方展示提示："请先前往我的 Key 页面创建 API Key"
- 员工无可用模型 → 文档区上方展示提示："当前身份暂无可用模型，模型名称需从可用模型页面获取"

**数据依赖**：
- `AuthStore.identity`（姓名、部门信息）
- `keys.list()`（判断是否有 Key，仅计数不展示内容）
- `models.me()`（判断是否有可用模型，仅计数不展示内容）

---

### LoginCallback.vue — OIDC 回调处理页

**路由**：`/auth/callback`

**职责**：接收 SSO 回调 code → 调 control-plane 换取身份与 token → 写入 AuthStore → 跳转首页；失败时展示明确错误提示。

```
┌───────────────────────────────────────────────────┐
│                                                   │
│         ┌─────────────┐                           │
│         │  处理中...   │  加载态                   │
│         └─────────────┘                           │
│                                                   │
│   ────────── 或 ───────────────                   │
│                                                   │
│         ┌─────────────────────────────┐           │
│         │ ⚠️ 登录失败                  │           │
│         │ 认证服务暂时不可用，          │  错误态   │
│         │ 请稍后重试                    │           │
│         │         [重新登录]            │           │
│         └─────────────────────────────┘           │
│                                                   │
└───────────────────────────────────────────────────┘
```

**状态流转**：

| 状态 | 条件 | UI |
| :--- | :--- | :--- |
| 加载中 | onMounted，正在解析 code 并请求 | 居中 Loading 动画 + "正在登录..." |
| 成功 | control-plane 返回 valid identity + token | 无感知 → router.push('/home') |
| 失败-code缺失 | URL 无 code 参数 | 错误提示："登录失败，缺少认证参数，请重新登录" + [重新登录] 按钮 |
| 失败-认证无效 | control-plane 返回错误 | 错误提示："登录失败，请重新尝试" + [重新登录] 按钮 |
| 失败-网络异常 | 请求超时或连接失败 | 错误提示："认证服务暂时不可用，请稍后重试" + [重新登录] 按钮 |

---

### KeyListPage.vue — 我的 Key 管理页

**路由**：`/keys`

**职责**：展示当前员工名下 Key 列表（脱敏 + 状态 + 时间）；提供创建、禁用、启用、删除操作入口。

```
┌─────────────────────────────────────────────────────────────────────┐
│  我的 Key                                      Inter Semi Bold 24px  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  [+ 创建 Key]  #00E5FF 主按钮                                        │
│                                                                      │
│  ┌─── Key 列表  #1E1E1E  radius:16px  border:#2C2C2E ─────────────┐│
│  │  Key 名称      │ 脱敏值       │ 状态     │ 创建时间     │ 操作    ││
│  ├────────────────┼──────────────┼──────────┼──────────────┼─────────┤│
│  │ 生产环境 Key    │ sk-****abcdef │ 🟢 启用  │ 2026-05-20   │ 禁用    ││
│  │ 测试 Key       │ sk-****1234  │ 🔴 禁用  │ 2026-05-18   │ 启用│删除││
│  └────────────────┴──────────────┴──────────┴──────────────┴─────────┘│
│                                                                      │
│  空态：  暂无 Key，点击创建按钮生成你的第一个 Key                      │
└─────────────────────────────────────────────────────────────────────┘
```

**脱敏展示规则**：仅显示末尾 4 位，前缀为 `sk-****`。

**状态指示灯**：

| 状态 | 颜色 | 文字 | 可用操作 |
| :--- | :--- | :--- | :--- |
| 启用 | #32D74B | 启用 | [禁用] |
| 禁用 | #FF453A | 禁用 | [启用] [删除] |

**边界态**：
- 列表为空 → "暂无 Key，点击创建按钮生成你的第一个 Key"
- Key 数量达上限 → "[+ 创建 Key]" 按钮置灰 + tooltip："已达 Key 数量上限"
- 删除操作 → 弹出 ElMessageBox.confirm："确定删除 Key「{name}」？此操作不可恢复。"

---

### KeyCreateDialog.vue — 创建 Key 弹窗

**组件位置**：`components/key/KeyCreateDialog.vue`

**职责**：提供 Key 名称输入 → 调 POST /api/v1/keys → 展示 Key 明文（仅一次）+ 复制引导 → 关闭后刷新列表。

```
┌───────────────────────────────────────────────┐
│  创建 API Key                      [×]        │
├───────────────────────────────────────────────┤
│                                               │
│  Key 名称 *  [____________________________]  │
│                                               │
├───────────────────────────────────────────────┤
│              [取消]     [确认创建] #00E5FF     │
└───────────────────────────────────────────────┘

          ↓ 点击"确认创建"成功后

┌───────────────────────────────────────────────┐
│  创建成功                          [×]        │
├───────────────────────────────────────────────┤
│                                               │
│  ⚠️ 此明文仅展示一次，请立即复制保存            │
│                                               │
│  ┌─────────────────────────────────────────┐ │
│  │ sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx      │ │
│  │                              [📋 复制]   │ │
│  └─────────────────────────────────────────┘ │
│                                               │
├───────────────────────────────────────────────┤
│                    [关闭]                      │
└───────────────────────────────────────────────┘
```

**交互规则**：
- 弹窗关闭后 Key 明文不再展示，页面刷新 Key 列表（脱敏状态）
- 明文展示区提供一键复制按钮，使用 `navigator.clipboard.writeText()`，复制后按钮变为"已复制"
- 明文不在 localStorage / sessionStorage / Pinia store 中缓存
- 表单校验：Key 名称必填，最多 64 字符

---

### ModelListPage.vue — 可用模型列表页

**路由**：`/models`

**职责**：调 GET /v1/me/models 展示当前身份可用模型（名称、类型、状态）。

```
┌─────────────────────────────────────────────────────────────────────┐
│  可用模型                                      Inter Semi Bold 24px  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─── 模型列表  #1E1E1E  radius:16px  border:#2C2C2E ─────────────┐│
│  │ 模型名称        │ 模型类型          │ 状态                      ││
│  ├─────────────────┼───────────────────┼────────────────────────────┤│
│  │ gpt-4o          │ 对话补全 (chat)    │ 🟢 可用                    ││
│  │ qwen-turbo      │ 对话补全 (chat)    │ 🟢 可用                    ││
│  │ text-embedding  │ 向量嵌入 (embedding)│ 🟢 可用                   ││
│  └─────────────────┴───────────────────┴────────────────────────────┘│
│                                                                      │
│  空态：  当前身份暂无可用模型                                         │
│  错误态：模型列表加载失败，请刷新重试 [刷新]                           │
└─────────────────────────────────────────────────────────────────────┘
```

**数据来源**：`GET /v1/me/models`（gateway-api），基于员工身份（User → Department）过滤后返回。

**边界态**：
- 空态 → "当前身份暂无可用模型"
- 加载失败 → "模型列表加载失败，请刷新重试" + [刷新] 按钮
- 不展示过期缓存数据，每次进入页面实时请求

---

## 新增 Store

### AuthStore（Pinia）— `stores/auth.ts`

**职责**：管理员工登录态、JWT Token 生命周期、用户身份信息。

**状态定义**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| accessToken | string \| null | JWT access token（Bearer 认证用） |
| refreshToken | string \| null | JWT refresh token |
| user | User \| null | 当前登录员工身份信息 |
| role | 'employee' \| 'admin' \| null | 用户角色 |

**计算属性**：

| Getter | 返回类型 | 说明 |
| :--- | :--- | :--- |
| isLoggedIn | boolean | accessToken 非空且未过期 |
| identity | { name, departmentName } \| null | 用于页面展示的快捷身份信息 |

**关键 Action**：

| Action | 说明 |
| :--- | :--- |
| handleOidcCallback(code) | 调 auth API 换取 token 与 identity，写入 store |
| refreshAccessToken() | 使用 refreshToken 续期 accessToken |
| logout() | 清空所有状态，跳转 SSO 登录页 |
| restoreSession() | 从 localStorage 恢复 token（页面刷新后保持登录） |

**持久化**：accessToken + refreshToken + user + role 写入 localStorage，页面刷新后通过 `restoreSession()` 恢复。登出时清除。

**路由守卫**：

```typescript
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()
  const publicRoutes = ['/auth/callback', '/login']
  if (!authStore.isLoggedIn && !publicRoutes.includes(to.path)) {
    next('/login')
  } else {
    next()
  }
})
```

---

## 新增 API 模块

### `api/auth.ts` — 认证服务 API

| 方法 | 说明 |
| :--- | :--- |
| oidcCallback(code: string) | POST /auth/oidc/callback → { accessToken, refreshToken, user, role } |
| refreshToken(refreshToken: string) | POST /auth/refresh → { accessToken, refreshToken } |
| logout() | POST /auth/logout → void（服务端注销 + 前端清空 store） |

### `api/control-plane.ts` — 新增 Key 管理方法

| 方法 | 说明 |
| :--- | :--- |
| listKeys() | GET /api/v1/keys → ApiKey[] |
| createKey(name: string) | POST /api/v1/keys → { id, name, plaintext } |
| disableKey(id: string) | PATCH /api/v1/keys/:id/status { enabled: false } → void |
| enableKey(id: string) | PATCH /api/v1/keys/:id/status { enabled: true } → void |
| deleteKey(id: string) | DELETE /api/v1/keys/:id → void |

**认证头**：所有 Key 管理 API 调用携带 `Authorization: Bearer {accessToken}`，由 axios 请求拦截器统一注入。

---

## 关键流程设计

### OIDC 登录流程

```mermaid
sequenceDiagram
    participant User as 员工
    participant SSO as 公司 SSO
    participant Callback as LoginCallback.vue
    participant AuthAPI as api/auth.ts
    participant Store as AuthStore
    participant Router as Vue Router
    participant Home as HomePage.vue
    participant API as api/control-plane.ts
    participant CP as control-plane

    User->>SSO: 点击"登录"按钮
    SSO->>SSO: 员工完成认证
    SSO->>Callback: 回调 /auth/callback?code=xxx&state=yyy
    Callback->>Callback: 解析 URL query 参数
    alt code 缺失或无效
        Callback-->>User: 展示"登录失败，缺少认证参数"
    else code 有效
        Callback->>AuthAPI: POST /auth/oidc/callback { code, state }
        AuthAPI->>CP: HTTP POST
        alt 认证成功
            CP-->>AuthAPI: { accessToken, refreshToken, user, role }
            AuthAPI-->>Callback: 身份 + token
            Callback->>Store: handleOidcCallback(response)
            Store->>Store: 写入 accessToken, refreshToken, user, role
            Store->>Store: 持久化到 localStorage
            Callback->>Router: push('/home')
            Router->>Home: 加载首页
            Home->>API: 设置 axios 拦截器注入 Bearer token
        else 认证失败
            CP-->>AuthAPI: 400/401 error
            AuthAPI-->>Callback: throw error
            Callback-->>User: 展示"登录失败，请重新尝试"
        end
    end
```

### Key 创建流程

```mermaid
sequenceDiagram
    participant User as 员工
    participant Page as KeyListPage.vue
    participant Dialog as KeyCreateDialog.vue
    participant API as api/control-plane.ts
    participant CP as control-plane

    User->>Page: 点击"+ 创建 Key"
    Page->>Dialog: 打开空白表单弹窗 (visible=true)
    User->>Dialog: 输入 Key 名称
    Dialog->>Dialog: 表单校验（名称必填，≤64字符）
    User->>Dialog: 点击"确认创建"
    Dialog->>Dialog: 按钮 loading=true
    Dialog->>API: createKey(name)
    API->>CP: POST /api/v1/keys { name }
    alt 创建成功
        CP-->>API: 201 { id, name, plaintext: "sk-xxx..." }
        API-->>Dialog: { id, name, plaintext }
        Dialog->>Dialog: 切换到"明文展示"视图
        Dialog-->>User: 展示 plaintext + "仅展示一次"警告
        User->>Dialog: 点击"复制"按钮
        Dialog->>Dialog: navigator.clipboard.writeText(plaintext)
        Dialog-->>User: 按钮变为"已复制"
        User->>Dialog: 点击"关闭"
        Dialog->>Dialog: plaintext 置空，不存储
        Dialog->>Page: emit('created'), visible=false
        Page->>API: listKeys() 重新拉取列表
        API->>CP: GET /api/v1/keys
        CP-->>API: ApiKey[]（脱敏）
        API-->>Page: Key 列表
        Page-->>User: 刷新列表，新增 Key 脱敏展示
    else 创建失败
        CP-->>API: 4xx/5xx error
        API-->>Dialog: throw error
        Dialog-->>User: ElMessage.error("创建失败，请稍后重试")
    end
```

### Key 列表加载与操作流程

```mermaid
sequenceDiagram
    participant User as 员工
    participant Page as KeyListPage.vue
    participant API as api/control-plane.ts
    participant CP as control-plane

    User->>Page: 进入 /keys
    Page->>Page: loading=true, 展示骨架屏
    Page->>API: listKeys()
    API->>CP: GET /api/v1/keys (Authorization: Bearer xxx)

    alt 成功且有数据
        CP-->>API: 200 ApiKey[]
        API-->>Page: Key 列表
        Page-->>User: 渲染 Key 表格（脱敏 + 状态 + 时间）
    else 成功但空列表
        CP-->>API: 200 []
        API-->>Page: 空数组
        Page-->>User: 展示空态"暂无 Key"
    else 认证失败
        CP-->>API: 401
        API-->>Page: throw 401
        Page->>Page: 路由守卫拦截，跳转登录
    else 请求失败
        CP-->>API: 5xx / 网络异常
        API-->>Page: throw error
        Page-->>User: 展示"加载失败，请刷新重试"
    end

    Note over User,CP: 禁用 Key 操作
    User->>Page: 点击某 Key 的"禁用"按钮
    Page->>API: disableKey(id)
    API->>CP: PATCH /api/v1/keys/:id/status { enabled: false }
    CP-->>API: 200 OK
    API-->>Page: success
    Page->>API: listKeys() 刷新
    Page-->>User: 该 Key 状态变为 🔴 禁用，操作切换为"启用"和"删除"

    Note over User,CP: 删除 Key 操作
    User->>Page: 点击某 Key 的"删除"按钮
    Page-->>User: ElMessageBox.confirm("确定删除？不可恢复")
    User->>Page: 确认
    Page->>API: deleteKey(id)
    API->>CP: DELETE /api/v1/keys/:id
    CP-->>API: 204 No Content
    API-->>Page: success
    Page->>API: listKeys() 刷新
    Page-->>User: 列表中该 Key 消失
```

---

## 路由变更

### 新增路由

```typescript
// 员工端路由
{
  path: '/auth/callback',
  name: 'LoginCallback',
  component: () => import('@/views/auth/LoginCallback.vue'),
  meta: { title: '登录回调', public: true }
},
{
  path: '/home',
  name: 'Home',
  component: () => import('@/views/home/HomePage.vue'),
  meta: { title: '首页', requiresAuth: true }
},
{
  path: '/keys',
  name: 'KeyList',
  component: () => import('@/views/key/KeyListPage.vue'),
  meta: { title: '我的 Key', requiresAuth: true }
},
{
  path: '/models',
  name: 'ModelList',
  component: () => import('@/views/model/ModelListPage.vue'),
  meta: { title: '可用模型', requiresAuth: true }
}
```

### 已有路由

保留 `/access/unified-entry`、`/admin/providers`、`/admin/models`，属于管理员入口，由角色路由守卫保护。

---

## 导航更新

侧边栏菜单按 `AuthStore.isLoggedIn` 和 `AuthStore.role` 条件渲染：

| 条件 | 可见菜单项 |
| :--- | :--- |
| 未登录 | 仅显示"登录"入口（跳转 SSO） |
| role === 'employee' | 首页、我的 Key、可用模型 |
| role === 'admin' | 首页、我的 Key、可用模型、统一接入、渠道管理、模型管理 |

---

## 组件层级变更

```
App.vue
├── SidebarNav.vue（已有，条件渲染逻辑新增）
├── AuthStore（新增 Pinia Store）
├── Vue Router
│   ├── /auth/callback → LoginCallback.vue（新增）
│   ├── /home → HomePage.vue（新增）
│   ├── /keys → KeyListPage.vue（新增）
│   │   └── KeyCreateDialog.vue（新增）
│   ├── /models → ModelListPage.vue（新增）
│   ├── /access/unified-entry → UnifiedEntryPage.vue（已有）
│   │   ├── CapabilitySection.vue（已有）
│   │   └── ModelDiscoverySection.vue（已有）
│   │       └── ModelCard.vue（已有）
│   ├── /admin/providers → ProviderListPage.vue（已有）
│   │   ├── ProviderTable.vue（已有）
│   │   ├── ProviderFormDialog.vue（已有）
│   │   └── SyncModelsDialog.vue（已有）
│   └── /admin/models → ModelListPage.vue（已有，管理端）
│       ├── ModelTable.vue（已有）
│       └── ModelFormDialog.vue（已有）
├── api/
│   ├── auth.ts（新增）
│   └── control-plane.ts（已有，新增 Key 管理方法）
└── components/
    └── key/
        └── KeyCreateDialog.vue（新增）
```

---

## 关键交互设计

### Key 明文一次性展示

- KeyCreateDialog 调用 POST /api/v1/keys 成功后，plaintext 仅在弹窗内展示
- 弹窗标题切换为"创建成功"，内容区展示明文 + 警告文案
- 明文使用 monospace 字体，背景 #252525，可选中不可编辑
- 明文不在 localStorage / sessionStorage / Pinia / Vuex 任何地方缓存
- 弹窗关闭（无论点击关闭按钮、按 ESC、点击遮罩层）后 plaintext 置空，后续无法再次获取

### 剪贴板复制

- 明文展示区右侧 [复制] 按钮，调用 `navigator.clipboard.writeText(plaintext)`
- 复制成功后按钮文案变为"已复制"，颜色变为 #32D74B，2s 后恢复原状
- 若 `navigator.clipboard` 不可用（非 HTTPS 环境），降级为选中明文 + 提示"请手动复制"

### Key 状态 Badge

- 启用 Key：绿色圆点 (#32D74B) + "启用" 文字
- 禁用 Key：红色圆点 (#FF453A) + "禁用" 文字
- Badge 位于表格"状态"列，使用 Element Plus `<el-tag>` 组件

### 模型类型展示

| 后端 type 值 | 前端展示 |
| :--- | :--- |
| chat | 对话补全 (chat) |
| completion | 文本补全 (completion) |
| embedding | 向量嵌入 (embedding) |
| image | 图像生成 (image) |
| audio | 语音处理 (audio) |

---

## 设计决策记录

### 决策 7：引入 Pinia AuthStore

| 项 | 内容 |
| :--- | :--- |
| 问题 | 员工登录态和身份信息需要跨页面共享（首页、Key 管理、模型列表、导航栏） |
| 选择 | 引入 Pinia AuthStore 管理认证状态，替代原有"复用后台 JWT Cookie"方案 |
| 理由 | 员工端新增多个页面需访问登录态，组件内 ref 跨页面无法共享；Pinia 支持 getter 派生状态（isLoggedIn、identity），支持持久化插件，适合单页应用会话管理 |
| 备选方案 | 继续仅使用 Cookie + 后端拦截器——无法在纯前端实现条件导航和路由守卫，需额外请求 /me 接口判断登录态 |

### 决策 8：Key 明文仅展示一次不禁缓存

| 项 | 内容 |
| :--- | :--- |
| 问题 | Key 明文在创建后如何安全展示 |
| 选择 | 仅在 KeyCreateDialog 弹窗内展示，关闭后置空，任何持久化存储均不写入 plaintext |
| 理由 | 安全策略要求——Key 明文仅在创建时返回，关闭弹窗后后端不再提供明文查询能力；前端不缓存是防御纵深，防止 XSS 或浏览器扩展窃取 |
| 备选方案 | 写入 Pinia store 以便后续页内查看——安全风险高，违反"仅展示一次"的安全设计原则 |

### 决策 9：模型可用列表使用 /v1/me/models 而非 /v1/models

| 项 | 内容 |
| :--- | :--- |
| 问题 | 员工端模型列表应展示所有注册模型还是仅展示其身份可用的模型 |
| 选择 | 使用 /v1/me/models（基于 User → Department 过滤），而非通用 /v1/models |
| 理由 | 业务需求明确"展示当前员工经身份鉴权后可用的模型"，/v1/me/models 由 gateway-api 根据请求方 JWT 身份做部门级过滤，前端无需自行比对；/v1/models 返回全量注册模型，不适合员工端场景 |
| 备选方案 | 使用 /v1/models + 前端自行过滤——增加前端复杂度且无法感知后端权限策略变更 |


## 15. 变更记录 — CHG-20260525-001

## 变更摘要

新增模型组管理页面及三个核心组件（GroupListPage、GroupEditDialog、ModelGroupSelector），在导航菜单新增"模型组"入口。所有组件消费 control-plane 的模型组 CRUD 接口。

---

## 1. 设计决策

### 决策 1：策略切换提示

| 项 | 内容 |
| :--- | :--- |
| 问题 | 编辑模型组时用户切换路由策略类型，已有策略参数如何处理？ |
| 选择 | 切换策略类型时弹出警告提示"切换策略将清除已有策略参数"，确认后清空 priority/weight 值 |
| 理由 | 不同策略类型的参数语义不同（priority vs weight），直接保留会导致数据不一致；提示可防止误操作 |
| 备选方案 | 静默清除参数——用户可能未察觉参数丢失，体验不佳 |

**实现要点**：
- 使用 `watch` 监听 `form.strategy` 变化
- 仅在编辑模式（mode === 'edit'）且已有模型列表非空时触发警告
- 使用 `ElMessage.warning('切换策略将清除已有策略参数')` 展示提示
- 清空所有已选模型的 `priority` 和 `weight` 字段

### 决策 2：删除确认方式

| 项 | 内容 |
| :--- | :--- |
| 问题 | 模型组删除操作如何做二次确认？ |
| 选择 | 使用 Element Plus `ElMessageBox.confirm`，设置 `type: 'warning'`，确认按钮样式 `confirmButtonClass: 'el-button--danger'` |
| 理由 | 与渠道管理页、模型管理页的删除确认交互保持一致；删除不可逆，危险样式可强化警示 |
| 备选方案 | 自定义弹窗组件——增加开发量但无额外收益 |

**确认文案**：
```
确定删除模型组「{name}」？删除后组内模型配置将一并清除，此操作不可恢复。
```

### 决策 3：模型选择器数据来源与多选逻辑

| 项 | 内容 |
| :--- | :--- |
| 问题 | 模型选择器中的模型列表从哪里获取？已加入组的模型如何展示？ |
| 选择 | 从 control-plane `GET /api/v1/models` 加载全量已注册模型；已加入当前组的模型在前端标记为 `checked + disabled`（不可取消勾选） |
| 理由 | 同一模型允许出现在多个组中，因此选择器展示全量模型而非排除已选模型；disabled 标记使已选关系可读但不可通过选择器移除（移除操作在组内模型列表中完成） |
| 备选方案 | 选择器中隐藏已在组内的模型——管理员无法看到组内模型的全量列表，不利于认知当前组的完整配置 |

**交互细节**：
- 已加入组的模型显示为勾选状态且置灰（`:disabled="isModelInGroup(model.id)"`）
- 搜索过滤仅作用于显示层面，不影响已选模型的 disabled 状态
- 确认后将新增勾选的模型 emit 给父组件

### 决策 4：策略参数输入控件

| 项 | 内容 |
| :--- | :--- |
| 问题 | 不同策略类型下策略参数用什么输入控件？ |
| 选择 | 优先级策略：`el-input-number` 范围 0-999，整数，step=1；加权策略：`el-input-number` 范围 1-100，整数，step=1；轮询策略：不展示参数输入 |
| 理由 | 数字输入组件限制输入范围，防止无效值提交；按策略类型条件渲染，减少无关 UI 元素 |
| 备选方案 | 统一使用 `el-input`——需要额外校验逻辑，不如 `el-input-number` 语义明确 |

**条件渲染规则**：

| 策略 | 优先级输入 (0-999) | 权重输入 (1-100) | 说明 |
| :--- | :---: | :---: | :--- |
| round_robin | 不展示 | 不展示 | 无策略参数 |
| priority | 展示 | 不展示 | 每模型填写优先级数字 |
| weighted | 不展示 | 展示 | 每模型填写权重值 |

### 决策 5：不引入 Pinia Store

| 项 | 内容 |
| :--- | :--- |
| 问题 | 模型组管理页面状态是否需要使用 Pinia？ |
| 选择 | 使用组件内 `ref`/`reactive` 管理，不引入 Pinia store |
| 理由 | 页面状态仅限组件树内部使用，无跨页面共享需求；与现有 `UnifiedEntryPage`、`ModelListPage` 状态管理方式一致 |
| 备选方案 | 使用 Pinia store——增加复杂度但无实际收益 |

---

## 2. 组件详细设计

### 2.1 GroupListPage — 模型组列表页

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 路由 | `/groups` |
| 组件文件 | `src/views/admin/GroupListPage.vue` |
| 职责 | 模型组列表展示、关键词搜索、打开创建/编辑弹窗、触发删除 |

**Vue 3 Composition API 结构**：

```typescript
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useDebounceFn } from '@vueuse/core'
import GroupEditDialog from '@/components/admin/GroupEditDialog.vue'

interface GroupsState {
  groups: GroupListItem[]
  loading: boolean
  error: string | null
  searchKeyword: string
}

const state = reactive<GroupsState>({
  groups: [],
  loading: false,
  error: null,
  searchKeyword: ''
})

const editDialogVisible = ref(false)
const editDialogMode = ref<'create' | 'edit'>('create')
const editingGroupId = ref<string | null>(null)

const debouncedSearch = useDebounceFn((keyword: string) => {
  state.searchKeyword = keyword
  fetchGroups()
}, 300)

async function fetchGroups(): Promise<void> { /* GET /api/v1/groups?search= */ }
async function handleDelete(group: GroupListItem): Promise<void> { /* 二次确认 → API 调用 → 刷新 */ }
function handleCreate(): void { editDialogMode.value = 'create'; editingGroupId.value = null; editDialogVisible.value = true }
function handleEdit(group: GroupListItem): void { editDialogMode.value = 'edit'; editingGroupId.value = group.id; editDialogVisible.value = true }
function onDialogSuccess(): void { editDialogVisible.value = false; fetchGroups() }

onMounted(() => { fetchGroups() })
```

**el-table 列定义**：

| 列名 | 字段 | 宽度 | 渲染方式 |
| :--- | :--- | :--- | :--- |
| 组名称 | name | auto | 文本 |
| 路由策略 | strategy_label | 100px | el-tag（轮询=info / 优先级=warning / 加权=success） |
| 模型数量 | model_count | 100px | 数字 |
| 状态 | enabled | 80px | el-tag（启用=success / 禁用=danger） |
| 操作 | — | 160px | el-button-group：编辑 + 删除 |

**搜索**：

```
┌──────────────────────────────────────────────────────────────┐
│  [🔍 搜索模型组名称...              ]    [+ 创建模型组]      │
│  debounce 300ms，输入时实时过滤                               │
└──────────────────────────────────────────────────────────────┘
```

- el-input 绑定 `@input` 触发 `debouncedSearch`
- 搜索为空时加载全量列表

**多态状态**：

| 状态 | 条件 | UI 表现 |
| :--- | :--- | :--- |
| 加载中 | `loading === true` | el-table 显示 `v-loading`，骨架占位 |
| 正常 | `groups.length > 0` | 渲染表格数据 |
| 空态 | `groups.length === 0 && !loading && !error` | el-empty description="暂无模型组，点击创建第一个模型组" |
| 错误 | `error !== null` | el-alert type="error" title="加载失败，请刷新重试" + 刷新按钮 |

**删除确认流程**：
```typescript
async function handleDelete(group: GroupListItem) {
  try {
    await ElMessageBox.confirm(
      `确定删除模型组「${group.name}」？删除后组内模型配置将一并清除，此操作不可恢复。`,
      '确认删除',
      {
        confirmButtonText: '确认删除',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger'
      }
    )
    await groupsApi.delete(group.id)
    ElMessage.success('模型组已删除')
    fetchGroups()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('操作失败，请重试')
    }
  }
}
```

---

### 2.2 GroupEditDialog — 创建/编辑弹窗

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 组件文件 | `src/components/admin/GroupEditDialog.vue` |
| 职责 | 模型组的创建/编辑表单，包含基本信息区与组内模型列表区 |
| Props | `visible: boolean`, `mode: 'create' | 'edit'`, `groupId: string | null` |
| Emits | `update:visible`, `success` |

**Vue 3 Composition API 结构**：

```typescript
import { ref, reactive, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import ModelGroupSelector from '@/components/admin/ModelGroupSelector.vue'
import type { GroupForm, GroupModel, Strategy } from '@/types/group'

interface GroupEditState {
  form: GroupForm
  submitting: boolean
  selectedModels: GroupModel[]
}

const props = defineProps<{ visible: boolean; mode: 'create' | 'edit'; groupId: string | null }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void; (e: 'success'): void }>()

const state = reactive<GroupEditState>({
  form: { name: '', description: '', strategy: 'round_robin' },
  submitting: false,
  selectedModels: []
})

const modelSelectorVisible = ref(false)
const formRef = ref<FormInstance>()

const dialogTitle = computed(() => props.mode === 'create' ? '创建模型组' : '编辑模型组')

// 策略切换警告
watch(() => state.form.strategy, (newVal, oldVal) => {
  if (props.mode === 'edit' && oldVal && newVal !== oldVal && state.selectedModels.length > 0) {
    ElMessage.warning('切换策略将清除已有策略参数')
    state.selectedModels.forEach(m => { m.priority = undefined; m.weight = undefined })
  }
})

// 编辑模式加载已有数据
watch(() => props.visible, async (val) => {
  if (val && props.mode === 'edit' && props.groupId) {
    // GET /api/v1/groups/:id 加载组详情
    // GET /api/v1/groups/:id/models 加载组内模型
  } else if (val && props.mode === 'create') {
    resetForm()
  }
})
```

**模板结构**：

```
┌──────────────────────────────────────────────────────┐
│  创建模型组 / 编辑模型组                  [×]         │
├──────────────────────────────────────────────────────┤
│                                                      │
│  ── 基本信息 ────────────────────────────────────── │
│                                                      │
│  组名称 *     [______________________________]       │
│               必填，最多 64 字符                      │
│                                                      │
│  组描述       [______________________________]       │
│               选填                                     │
│                                                      │
│  路由策略 *   ○ 轮询 (round_robin)                   │
│               ○ 优先级 (priority)                    │
│               ○ 加权 (weighted)                      │
│               ↓ 切换时警告                            │
│               ↓ 提示："添加模型时需指定优先级数字"     │
│               ↓ 或 "添加模型时需指定权重值"           │
│                                                      │
│  ── 组内模型 ────────────────────────────────────── │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │ 模型名称      策略参数        操作            │   │
│  ├──────────────────────────────────────────────┤   │
│  │ gpt-4o        优先级: 1        [移除]        │   │
│  │ qwen-turbo    权重: 80         [移除]        │   │
│  └──────────────────────────────────────────────┘   │
│  [+ 添加模型]                                        │
│                                                      │
├──────────────────────────────────────────────────────┤
│                  [取消]      [保存]                   │
└──────────────────────────────────────────────────────┘
```

**el-form 校验规则**：

```typescript
const rules = {
  name: [
    { required: true, message: '请输入组名称', trigger: 'blur' },
    { max: 64, message: '组名称不能超过 64 个字符', trigger: 'blur' },
    { whitespace: true, message: '组名称不能为纯空格', trigger: 'blur' }
  ],
  strategy: [
    { required: true, message: '请选择路由策略', trigger: 'change' }
  ]
}

// 提交时额外校验
async function handleSubmit() {
  await formRef.value?.validate()
  if (state.selectedModels.length === 0) {
    ElMessage.warning('请至少添加一个模型')
    return
  }
  // 策略参数校验
  if (state.form.strategy === 'priority') {
    const hasInvalidPriority = state.selectedModels.some(
      m => m.priority == null || m.priority < 0 || m.priority > 999
    )
    if (hasInvalidPriority) { ElMessage.warning('请为每个模型设置有效的优先级 (0-999)'); return }
  }
  if (state.form.strategy === 'weighted') {
    const hasInvalidWeight = state.selectedModels.some(
      m => m.weight == null || m.weight < 1 || m.weight > 100
    )
    if (hasInvalidWeight) { ElMessage.warning('请为每个模型设置有效的权重 (1-100)'); return }
  }

  state.submitting = true
  try {
    if (props.mode === 'create') {
      await groupsApi.create(state.form, state.selectedModels)
    } else {
      await groupsApi.update(props.groupId!, state.form, state.selectedModels)
    }
    ElMessage.success(props.mode === 'create' ? '模型组创建成功' : '模型组更新成功')
    emit('success')
  } catch (err: any) {
    // 错误处理见 contracts.delta.md
  } finally {
    state.submitting = false
  }
}
```

**策略提示文案**（form.strategy 下方）：

| 策略 | 提示文案 |
| :--- | :--- |
| priority | "添加模型时需指定优先级数字（0-999）" |
| weighted | "添加模型时需指定权重值（1-100）" |
| round_robin | 无提示 |

**组内模型列表**：
- 编辑模式下，打开弹窗时通过 `GET /api/v1/groups/:id/models` 加载已有组内模型
- 创建模式下，列表初始为空
- 每个模型条目显示：模型名称 + 策略参数（按策略类型条件渲染）+ 移除操作
- 移除操作：`state.selectedModels.splice(index, 1)`

---

### 2.3 ModelGroupSelector — 模型选择器

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 组件文件 | `src/components/admin/ModelGroupSelector.vue` |
| 职责 | 从全量已注册模型中多选要加入当前组的模型 |
| 展示形式 | `el-drawer` 或 `el-dialog` |
| Props | `visible: boolean`, `existingModelIds: string[]`, `strategy: Strategy` |
| Emits | `update:visible`, `confirm(selected: GroupModel[])` |

**Vue 3 Composition API 结构**：

```typescript
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { ModelOption, GroupModel, Strategy } from '@/types/group'

const props = defineProps<{
  visible: boolean
  existingModelIds: string[]
  strategy: Strategy
}>()
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'confirm', models: GroupModel[]): void
}>()

interface SelectorState {
  allModels: ModelOption[]
  searchKeyword: string
  selectedIds: Set<string>
  loading: boolean
}

const state = reactive<SelectorState>({
  allModels: [],
  searchKeyword: '',
  selectedIds: new Set(),
  loading: false
})

const filteredModels = computed(() =>
  state.allModels.filter(m =>
    m.name.toLowerCase().includes(state.searchKeyword.toLowerCase())
  )
)

function isModelInGroup(modelId: string): boolean {
  return props.existingModelIds.includes(modelId)
}

async function loadModels() {
  state.loading = true
  try {
    const models = await modelsApi.list() // GET /api/v1/models
    state.allModels = models
    state.selectedIds = new Set(props.existingModelIds)
  } catch {
    ElMessage.error('模型列表加载失败')
  } finally {
    state.loading = false
  }
}

function handleConfirm() {
  const result: GroupModel[] = state.allModels
    .filter(m => state.selectedIds.has(m.id) && !isModelInGroup(m.id))
    .map(m => ({
      group_id: '',
      model_id: m.id,
      model_name: m.name,
      priority: props.strategy === 'priority' ? undefined : undefined,
      weight: props.strategy === 'weighted' ? undefined : undefined
    }))
  emit('confirm', result)
}

watch(() => props.visible, (val) => { if (val) loadModels() })
```

**模板结构**：

```
┌──────────────────────────────────────────────────────┐
│  选择模型                               [×]          │
├──────────────────────────────────────────────────────┤
│                                                      │
│  [🔍 搜索模型名称...              ]                  │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │ ☑ gpt-4o (已禁用/已选)                       │   │
│  │ ☐ gpt-4o-mini                                │   │
│  │ ☑ qwen-turbo (已禁用/已选)                    │   │
│  │ ☐ qwen-max                                   │   │
│  │ ☐ text-embedding-v3                          │   │
│  │ ...                                           │   │
│  └──────────────────────────────────────────────┘   │
│                                                      │
├──────────────────────────────────────────────────────┤
│                  [取消]      [确认]                   │
└──────────────────────────────────────────────────────┘
```

**空态处理**：
- `allModels.length === 0 && !loading` → el-empty description="当前无已注册模型，请先在模型管理中注册模型"
- 空态时"确认"按钮置灰

**checkbox 状态逻辑**：

```html
<el-checkbox
  v-for="model in filteredModels"
  :key="model.id"
  :model-value="state.selectedIds.has(model.id)"
  :disabled="isModelInGroup(model.id)"
  @change="(val) => toggleModel(model.id, val)"
>
  {{ model.name }}
  <el-tag v-if="isModelInGroup(model.id)" size="small" type="info">已在组内</el-tag>
</el-checkbox>
```

- 已在组内的模型：`checked=true, disabled=true`，显示"已在组内"标签
- 新勾选的模型：`checked=true, disabled=false`
- 未勾选的模型：`checked=false, disabled=false`

---

### 2.4 导航变更

**Sidebar 导航新增菜单项**：

```typescript
// 在现有导航配置中新增
{
  path: '/groups',
  name: 'GroupList',
  component: () => import('@/views/admin/GroupListPage.vue'),
  meta: { title: '模型组', icon: 'Collection' }  // Element Plus Icons: Collection
}
```

**路由注册**：

```typescript
{
  path: '/groups',
  name: 'GroupList',
  component: () => import('@/views/admin/GroupListPage.vue'),
  meta: { title: '模型组', icon: 'Collection' }
}
```

**导航菜单位置**（建议放在"模型管理"下方）：

```
Sidebar
  ├── 统一接入
  ├── 渠道管理
  ├── 模型管理
  └── 模型组          ← 新增
```

---

## 3. 关键流程

### 3.1 模型组创建流程

```mermaid
sequenceDiagram
    participant ADM as 管理员
    participant GLP as GroupListPage
    participant GED as GroupEditDialog
    participant MGS as ModelGroupSelector
    participant CP as control-plane

    ADM->>GLP: 点击"创建模型组"
    GLP->>GED: 打开弹窗 (mode=create)
    GED-->>ADM: 展示空白表单

    ADM->>GED: 填写组名称、选择策略
    ADM->>GED: 点击"添加模型"
    GED->>MGS: 打开模型选择器 (strategy, existingModelIds=[])
    MGS->>CP: GET /api/v1/models
    CP-->>MGS: 全量已注册模型列表
    MGS-->>ADM: 展示模型多选列表
    ADM->>MGS: 勾选模型 (可多选)
    ADM->>MGS: 点击"确认"
    MGS->>GED: emit confirm(selectedModels)
    GED-->>ADM: 组内模型列表更新
    GED-->>ADM: 策略=priority → 展示优先级输入 / 策略=weighted → 展示权重输入

    ADM->>GED: 填写策略参数
    ADM->>GED: 点击"保存"
    GED->>GED: 表单校验 + 策略参数校验
    GED->>CP: POST /api/v1/groups { name, description, strategy, models: [...] }
    CP-->>GED: 201 Created
    GED->>GLP: emit success
    GLP->>CP: GET /api/v1/groups (刷新列表)
    GLP-->>ADM: ElMessage.success("模型组创建成功")
```

### 3.2 编辑模型组流程

```mermaid
sequenceDiagram
    participant ADM as 管理员
    participant GLP as GroupListPage
    participant GED as GroupEditDialog
    participant CP as control-plane

    ADM->>GLP: 点击某行"编辑"
    GLP->>GED: 打开弹窗 (mode=edit, groupId)
    GED->>CP: GET /api/v1/groups/:id
    CP-->>GED: 组基本信息 (name, description, strategy...)
    GED->>CP: GET /api/v1/groups/:id/models
    CP-->>GED: 组内模型列表 (含策略参数)
    GED-->>ADM: 回填表单数据 + 组内模型列表

    ADM->>GED: 修改组名称/描述/策略
    ADM->>GED: 添加/移除模型 → ModelGroupSelector
    ADM->>GED: 点击"保存"
    GED->>CP: PUT /api/v1/groups/:id { ... }
    CP-->>GED: 200 OK
    GED->>GLP: emit success
    GLP-->>ADM: 刷新列表
```

### 3.3 策略切换流程

```mermaid
flowchart TD
    A[用户切换策略类型] --> B{是否为编辑模式?}
    B -->|是| C{已有模型列表?}
    B -->|否(创建模式)| D[直接切换策略类型]
    C -->|是| E[ElMessage.warning: 切换策略将清除已有策略参数]
    C -->|否| D
    E --> F[清空所有模型的 priority 和 weight]
    F --> D
    D --> G{切换至什么策略?}
    G -->|priority| H[模型列表：展示优先级输入 0-999]
    G -->|weighted| I[模型列表：展示权重输入 1-100]
    G -->|round_robin| J[模型列表：隐藏策略参数列]
```

---

## 4. 状态管理

### 4.1 GroupsState — 列表页状态

```typescript
interface GroupsState {
  groups: GroupListItem[]
  loading: boolean
  error: string | null
  searchKeyword: string
}

const initialGroupsState: GroupsState = {
  groups: [],
  loading: false,
  error: null,
  searchKeyword: ''
}
```

**状态变更场景**：

| 场景 | groups | loading | error | searchKeyword |
| :--- | :--- | :--- | :--- | :--- |
| 初始状态 | [] | false | null | '' |
| 进入页面 / 发起请求 | 保持 | true | null | — |
| 请求成功 | GroupListItem[] | false | null | — |
| 请求成功但无数据 | [] | false | null | — |
| 请求失败 | [] | false | "加载失败，请刷新重试" | — |
| 用户搜索（防抖） | — | true | null | 输入值 |
| 创建/编辑/删除成功 | 触发 fetchGroups | — | — | — |

### 4.2 GroupEditState — 编辑弹窗状态

```typescript
interface GroupEditState {
  form: GroupForm
  submitting: boolean
  selectedModels: GroupModel[]
}

const initialGroupEditState: GroupEditState = {
  form: { name: '', description: '', strategy: 'round_robin' },
  submitting: false,
  selectedModels: []
}
```

**重置逻辑**：
```typescript
function resetForm() {
  state.form = { name: '', description: '', strategy: 'round_robin' }
  state.selectedModels = []
  state.submitting = false
}
```

### 4.3 ModelGroupSelector 状态

```typescript
interface ModelGroupSelectorState {
  allModels: ModelOption[]
  searchKeyword: string
  selectedIds: Set<string>
  loading: boolean
}
```

### 4.4 组件间数据流

```
GroupListPage
  │  state: groups[], loading, error, searchKeyword
  │
  ├──→ GroupEditDialog (props: visible, mode, groupId)
  │      │  state: form, submitting, selectedModels[]
  │      │
  │      └──→ ModelGroupSelector (props: visible, existingModelIds, strategy)
  │             │  state: allModels[], searchKeyword, selectedIds, loading
  │             │
  │             └── emit confirm(selected: GroupModel[])
  │
  │  emit: success → GroupListPage.fetchGroups()
  │
  └── API calls:
       GET /api/v1/groups?search=
       DELETE /api/v1/groups/:id
```

**Props 传递关系**：

| 父组件 | 子组件 | Props | 类型 |
| :--- | :--- | :--- | :--- |
| GroupListPage | GroupEditDialog | visible | boolean |
| GroupListPage | GroupEditDialog | mode | 'create' \| 'edit' |
| GroupListPage | GroupEditDialog | groupId | string \| null |
| GroupEditDialog | ModelGroupSelector | visible | boolean |
| GroupEditDialog | ModelGroupSelector | existingModelIds | string[] |
| GroupEditDialog | ModelGroupSelector | strategy | Strategy |