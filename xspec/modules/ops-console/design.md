# ops-console 详细设计

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

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
