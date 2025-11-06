# ops-console 前端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |

## 任务格式说明

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | 模块内唯一编号，从 T001 起编 |
| `[P]` | 可并行：操作不同文件、无依赖未完成任务 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@control-plane`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

- [x] T001 [P] P0 初始化 Vite + Vue 3 + TypeScript 项目骨架 `package.json, vite.config.ts, tsconfig.json, index.html, src/main.ts, src/App.vue`
- [x] T002 P0 安装并配置 Element Plus + Pinia + Vue Router `package.json, src/main.ts, src/router/index.ts, src/stores/index.ts`
  depends: T001
- [x] T003 P0 注册路由与页面目录结构 `src/router/index.ts, src/views/access/UnifiedEntryPage.vue, src/views/admin/ProviderListPage.vue, src/views/admin/ModelListPage.vue`
  depends: T002
- [x] T004 [P] P0 深色主题变量与全局样式 `src/styles/variables.scss, src/styles/global.scss, src/styles/element-overrides.scss`
  depends: T001
- [x] T005 [P] P0 API 服务层封装（axios 实例 + 拦截器） `src/services/http.ts, src/services/gateway-api.ts, src/services/control-plane.ts`
  depends: T001
- [x] T006 [P] P0 类型定义 `src/types/capability.ts, src/types/model.ts, src/types/provider.ts, src/types/api.ts`
  depends: T001

**检查点：** `pnpm dev` 启动正常，三个路由可访问，深色主题生效，API 层 TypeScript 编译通过。

---

## 阶段 2：数据层

- [x] T007 P0 TypeScript 接口定义（CapabilityConfig, ModelInfo） `src/types/capability.ts, src/types/model.ts`
  depends: T006
- [x] T008 P0 TypeScript 接口定义（Provider, Model CRUD） `src/types/provider.ts, src/types/model.ts`
  depends: T006
- [x] T009 P0 API 封装：gateway-api GET /v1/models `src/services/gateway-api.ts`
  depends: T005, T007
- [x] T010 P0 API 封装：control-plane 渠道 CRUD `src/services/control-plane.ts`
  depends: T005, T008
- [x] T011 P0 API 封装：control-plane 模型 CRUD `src/services/control-plane.ts`
  depends: T005, T008
- [x] T012 P0 静态配置文件 capability.json `src/assets/capability.json`
  depends: T007

**检查点：** 所有 API 方法可被组件导入调用，TypeScript 编译零错误，静态配置可正常加载。

---

## 阶段 3：功能实现

### US-01 — 统一接入能力页 (P0)

- [x] T013 P0 UnifiedEntryPage.vue 页面容器 `src/views/access/UnifiedEntryPage.vue`
  depends: T003, T004
- [x] T014 P0 CapabilitySection.vue 能力说明区 `src/components/access/CapabilitySection.vue`
  depends: T012, T013
- [x] T015 P0 ModelDiscoverySection.vue 模型发现区 `src/components/access/ModelDiscoverySection.vue`
  depends: T009, T013
- [x] T016 P0 ModelCard.vue 模型卡片组件 `src/components/access/ModelCard.vue`
  depends: T015
- [x] T017 P0 统一接入页空态与错误态处理 `src/components/access/ModelDiscoverySection.vue`
  depends: T015

### US-02 — 渠道管理页 (P0)

- [x] T018 P0 ProviderListPage.vue 渠道列表页 `src/views/admin/ProviderListPage.vue`
  depends: T004, T010
- [x] T019 P0 ProviderDialog.vue 创建/编辑对话框 `src/components/admin/ProviderDialog.vue`
  depends: T010, T018
- [x] T020 P0 渠道启用/禁用交互 `src/views/admin/ProviderListPage.vue`
  depends: T018
- [x] T021 P0 渠道删除交互（含模型阻断提示） `src/views/admin/ProviderListPage.vue`
  depends: T018
- [x] T022 P0 凭证脱敏展示 `src/views/admin/ProviderListPage.vue, src/utils/mask.ts`
  depends: T018

### US-03 — 模型管理页 (P0)

- [x] T023 P0 ModelListPage.vue 模型列表页（含渠道筛选） `src/views/admin/ModelListPage.vue`
  depends: T004, T011
- [x] T024 P0 ModelDialog.vue 注册/编辑对话框 `src/components/admin/ModelDialog.vue`
  depends: T011, T023
- [x] T025 P0 模型名称唯一性实时校验 `src/components/admin/ModelDialog.vue`
  depends: T024
- [x] T026 P0 模型删除确认 `src/views/admin/ModelListPage.vue`
  depends: T023

**检查点：** 三个页面功能完整，组件渲染正确，深色主题一致，表单交互流畅，TypeScript 编译零错误。

---

## 阶段 4：集成与收尾

- [x] T027 P0 前后端联调（统一接入页） `src/services/gateway-api.ts, src/components/access/ModelDiscoverySection.vue`
  depends: T017, T040@gateway-api
- [x] T028 P0 前后端联调（渠道管理 + 模型管理） `src/services/control-plane.ts, src/views/admin/ProviderListPage.vue, src/views/admin/ModelListPage.vue`
  depends: T021, T026, T040@control-plane
- [x] T029 P0 响应式验证（桌面端） `src/styles/global.scss`
  depends: T027, T028
- [x] T030 P0 错误处理完整性验证 `src/services/http.ts`
  depends: T027, T028

**检查点：** 全部页面与后端联调通过，桌面端响应式正常，错误处理覆盖完整，可交付验收。

---

## 交付策略

| 项目 | 说明 |
| :--- | :--- |
| 交付顺序 | 阶段 1 → 阶段 2 → 阶段 3（US-01/02/03 可并行） → 阶段 4 |
| 并行度 | 阶段 3 中三个 US 可由不同开发者并行实施 |
| 跨模块依赖 | T027 depends: T040@gateway-api；T028 depends: T040@control-plane |
| Mock 策略 | 阶段 3 开发阶段使用 MSW 或 Vite proxy mock，阶段 4 切换真实后端 |
| 总预估工时 | 约 20h |
