# ops-console Web 前端概要设计

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.0 | 2026-05-21 | 初稿创建 | OpenCode |
| v1.1 | 2026-05-23 | 新增 SyncModelsDialog 组件与同步模型交互流程（CHG-20260521-002） | OpenCode |

---

## 1. 技术范围界定

### 1.1 起点与终点

| 维度 | 说明 |
| :--- | :--- |
| **起点** | 平台管理员进入后台并看到导航菜单 |
| **终点** | 管理员进入统一接入能力页，看到本次支持能力、明确不做项和模型发现承接区 |
| **解决的问题** | 为统一接入主链路提供后台入口和可演示页面承接，同时严格不扩展为完整治理后台 |

### 1.2 技术边界

```mermaid
flowchart LR
    Admin["平台管理员"] -->|导航交互| Console["ops-console 统一接入页"]
    Console -->|模型发现请求| GW["gateway-api /v1/models"]
    Console -->|后续扩展| CP["control-plane（后续）"]
```

- **上游**：后台导航体系、管理员浏览器访问上下文
- **下游**：gateway-api 的 /v1/models 接口（展示可用模型发现）；后续 control-plane 提供更多治理数据
- **不涉及**：完整后台信息架构重构、权限限额配置页、日志分析看板、普通员工自服务扩展

---

## 2. 架构方向探索

| 方向 | 核心思路 | 适合场景 | 风险 |
| :--- | :--- | :--- | :--- |
| 菜单承接式页面架构 | 在现有后台结构中新增统一接入菜单和单主题页面 | 本次仅做入口承接和能力说明 | 无明显风险 |

**选定方向**：菜单承接式页面架构
**选择理由**：本次只需要为统一接入提供一个菜单入口和说明页，不需要拆复杂前端子域或引入新的架构范式

---

## 3. 架构方案

### 3.1 页面结构

```mermaid
flowchart TD
    subgraph "ops-console 统一接入模块"
        Nav["后台导航（统一接入菜单项）"]
        Container["统一接入页面容器"]
        CapSection["能力说明区"]
        ModelSection["模型发现承接区"]
    end
    Nav --> Container
    Container --> CapSection
    Container --> ModelSection
    ModelSection -->|按需请求| API["gateway-api /v1/models"]
```

### 3.2 核心交互流程

```mermaid
sequenceDiagram
    participant Admin as 平台管理员
    participant Nav as 后台导航
    participant Page as 统一接入能力页
    participant API as gateway-api

    Admin->>Nav: 点击"统一接入"菜单
    Nav->>Page: 路由跳转 /access/unified-entry
    Page->>Page: 渲染能力说明区（本次支持 + 不做项）
    Page->>API: GET /v1/models（获取可用模型）
    alt 获取成功
        API-->>Page: 模型列表
        Page->>Page: 渲染模型发现承接区
    else 获取失败或为空
        API-->>Page: 空结果或错误
        Page->>Page: 渲染空态提示
    end
    Admin->>Page: 查看页面内容
```

### 3.2b 同步模型交互流程

```mermaid
sequenceDiagram
    participant Admin as 管理员
    participant Page as ProviderListPage
    participant Dialog as SyncModelsDialog
    participant API as control-plane

    Admin->>Page: 点击"同步模型"按钮
    Page->>API: POST /providers/:id/sync-models
    API-->>Page: SyncResult
    Page->>Dialog: 打开弹窗(传入 result)
    Admin->>Dialog: 输入关键词过滤
    Dialog->>Dialog: 实时过滤列表
    Admin->>Dialog: 一键全选 / 手动勾选
    Admin->>Dialog: 点击"确认注册"
    Dialog->>API: POST /providers/:id/sync-models/confirm
    API-->>Dialog: 注册成功
    Dialog->>Page: 关闭弹窗，刷新模型列表
```

**新增组件**：

| 组件 | 位置 | 职责 |
| :--- | :--- | :--- |
| SyncModelsDialog.vue | components/admin/ | 同步结果弹窗：展示列表、过滤、勾选、确认注册 |

### 3.3 路由规划

| 路由 | 页面 | 说明 |
| :--- | :--- | :--- |
| /access/unified-entry | 统一接入能力页 | 承接统一接入菜单入口与能力说明 |

---

## 4. 技术选型与约束

| 领域 | 选型 | 理由 |
| :--- | :--- | :--- |
| 框架 | Vue 3 + TypeScript | 项目技术栈已确定 |
| 状态管理 | Pinia | 项目已选定，本次页面状态简单 |
| 样式方案 | Element Plus 组件 + 项目现有样式体系 | 项目已选定，保持后台视觉一致 |
| 构建工具 | Vite | 项目已选定 |
| UI 组件库 | Element Plus | 项目已选定，管理后台标准组件库 |

### 宪章合规性

- [x] 符合 `constitution.md` 渐进演进原则：本次只做菜单入口和说明页，不提前铺满后台功能
- [x] 符合核心聚焦原则：不涉及 Prompt/Agent/工作流等范围外能力
- [x] 符合可验证交付原则：菜单可见、页面可进入、说明可阅读即为本次验收基线

---

## 5. 认证与会话

| 项 | 方案 |
| :--- | :--- |
| 认证方式 | 复用后台已有认证体系（SSO + JWT Cookie） |
| 登录页 | 后台统一登录流程，本次不新增 |
| 会话管理 | 复用后台已有 JWT Cookie 机制 |
| 登出 | 复用后台统一登出 |
| Token 刷新 | 复用后台已有刷新机制 |

---

## 6. 安全设计

| 威胁 | 防御措施 |
| :--- | :--- |
| XSS | 使用 Vue 3 默认模板转义，不使用 v-html 渲染用户输入 |
| CSRF | 复用后台已有 SameSite Cookie 防护 |
| 敏感数据存储 | 本次页面不涉及敏感数据输入或持久化 |

---

## 7. 观测与运维

### 7.1 质量目标

| 指标 | 目标值 | 测试条件 |
| :--- | :---: | :--- |
| FCP（首次内容绘制） | ≤ 2000 ms | 桌面端标准网络 |
| LCP（最大内容绘制） | ≤ 3000 ms | 桌面端标准网络 |
| CLS（累积布局偏移） | ≤ 0.1 | 正常加载 |
| 可用性 | ≥ 99.5% | 试点阶段滚动 7 天 |

### 7.2 监控指标

| 指标 | 说明 |
| :--- | :--- |
| JS 错误率 | 运行时异常上报 |
| 模型列表请求成功率 | /v1/models 调用成功 / 总请求 |
| 页面加载时间分位 | P50 / P90 |

### 7.3 错误追踪

| 维度 | 方案 |
| :--- | :--- |
| 错误上报 | 复用后台已有前端错误上报机制 |
| 日志脱敏 | 不在日志中输出用户身份详细信息 |
| Source Map | 仅上传到错误追踪平台，不暴露到生产 |

---

## 8. 失败模式与降级策略

| 失败场景 | 影响 | 降级策略 | 恢复方式 |
| :--- | :--- | :--- | :--- |
| /v1/models 接口不可用 | 模型发现区无数据 | 保留能力说明区可用，模型发现区展示空态提示 | 接口恢复后刷新页面即可 |
| 页面静态资源加载失败 | 页面不可见 | 复用后台统一 CDN 降级策略 | 资源恢复后刷新 |
| 会话过期 | 页面操作被拒 | 重定向到后台登录页 | 重新登录 |
| 网络断开 | 请求失败 | 显示网络异常提示，不丢失已展示内容 | 网络恢复后刷新 |

---

## 9. 待研究事项

不适用

> 本次前端技术栈（Vue 3 + Element Plus + Vite + Pinia）在项目中已有明确选型，页面复杂度低，无需额外技术研究。

---

## 10. 风险与对策

| 风险 | 概率 | 影响 | 对策 |
| :--- | :--- | :--- | :--- |
| 后台导航体系尚未搭建完成 | 中 | 中 | 本次可先用独立路由入口承接，后续补挂到正式导航 |
| 模型发现区数据源不稳定 | 低 | 低 | 做好空态处理，确保页面主体可用 |

---

## 11. 概要设计确认

- [x] 技术范围已界定
- [x] 架构方向已探索并选定
- [x] 架构方案已确认
- [x] 技术选型符合宪章
- [x] 待研究事项已识别（无需研究）

👉 确认后进入详细设计阶段（`/xf/detail`）
