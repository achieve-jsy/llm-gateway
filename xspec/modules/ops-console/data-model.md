# ops-console 数据模型

## 1. 文档概述

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
| v1.1 | 2026-05-23 | 新增 SyncResult / ConfirmSyncPayload 等同步模型类型（CHG-20260521-002） | OpenCode |
| v1.2 | 2026-05-25 | 新增 AuthState / KeysState / MeModelsState 前端状态 — CHG-20260523-001 | OpenCode |
| v1.3 | 2026-05-26 | 新增 Group / GroupModel / Strategy 等 TypeScript 类型 — CHG-20260525-001 | OpenCode |

本文档定义 ops-console 统一接入页面的前端数据结构，包括静态配置实体、API 响应类型、以及页面本地状态类型。所有类型均以 TypeScript interface 形式描述，作为前端开发的类型约束依据。

---

## 2. 核心实体

### 2.1 能力配置（CapabilityConfig）

```typescript
/** 能力说明完整配置，对应 src/config/capability.json */
interface CapabilityConfig {
  /** 本次支持的能力列表 */
  supported: CapabilityItem[]
  /** 明确不做项列表 */
  excluded: ExcludeItem[]
}
```

### 2.2 能力项（CapabilityItem）

```typescript
/** 单项支持能力 */
interface CapabilityItem {
  /** 能力名称，如"OpenAI 兼容接口" */
  name: string
  /** 能力描述，简述该能力涵盖的内容 */
  description: string
}
```

### 2.3 不做项（ExcludeItem）

```typescript
/** 单项不做项 */
interface ExcludeItem {
  /** 不做项名称，如"Prompt 管理" */
  name: string
  /** 不做的原因说明 */
  reason: string
}
```

### 2.4 模型信息（ModelInfo）

```typescript
/** 前端展示用模型信息，从 /v1/models 响应映射而来 */
interface ModelInfo {
  /** 模型唯一标识，即模型名称，如"gpt-4o"、"qwen-turbo" */
  id: string
  /** 归属厂商，如"openai"、"alibaba" */
  ownedBy: string
}
```

### 2.5 同步模型条目（SyncModelItem）

```typescript
/** 同步结果中的模型条目 */
interface SyncModelItem {
  id: string
  owned_by: string
}
```

### 2.6 同步结果（SyncResult）

```typescript
/** 同步模型接口返回结果 */
interface SyncResult {
  added: SyncModelItem[]
  existing: SyncModelItem[]
  removed: SyncModelItem[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}
```

### 2.7 确认注册请求体（ConfirmSyncPayload）

```typescript
/** 确认注册请求体 */
interface ConfirmSyncPayload {
  models_to_add: string[]
  disable_removed?: boolean
}
```

### 2.8 确认注册结果（ConfirmSyncResult）

```typescript
/** 确认注册返回结果 */
interface ConfirmSyncResult {
  registered: number
  skipped: number
  disabled: number
}
```

---

## 3. 数据关系

```mermaid
erDiagram
    UnifiedEntryPageState ||--o{ ModelInfo : "包含模型列表"
    CapabilityConfig ||--o{ CapabilityItem : "包含支持能力"
    CapabilityConfig ||--o{ ExcludeItem : "包含不做项"

    UnifiedEntryPageState {
        ModelInfo[] models
        boolean loading
        string error
    }

    ModelInfo {
        string id PK
        string ownedBy
    }

    CapabilityConfig {
        CapabilityItem[] supported
        ExcludeItem[] excluded
    }

    CapabilityItem {
        string name
        string description
    }

    ExcludeItem {
        string name
        string reason
    }

    SyncResult ||--o{ SyncModelItem : "新增模型"
    SyncResult ||--o{ SyncModelItem : "已存在模型"
    SyncResult ||--o{ SyncModelItem : "已下架模型"

    SyncResult {
        SyncModelItem[] added
        SyncModelItem[] existing
        SyncModelItem[] removed
        number total_upstream
        number total_local
        boolean unsupported
    }

    SyncModelItem {
        string id PK
        string owned_by
    }

    ConfirmSyncPayload {
        string[] models_to_add
        boolean disable_removed
    }

    ConfirmSyncResult {
        number registered
        number skipped
        number disabled
    }
```

**关系说明**：

| 主体 | 关系 | 客体 | 基数 |
| :--- | :--- | :--- | :--- |
| UnifiedEntryPageState | 包含 | ModelInfo | 0..N |
| CapabilityConfig | 包含 | CapabilityItem | 1..N |
| CapabilityConfig | 包含 | ExcludeItem | 1..N |
| SyncResult | 包含 | SyncModelItem (added) | 0..N |
| SyncResult | 包含 | SyncModelItem (existing) | 0..N |
| SyncResult | 包含 | SyncModelItem (removed) | 0..N |
| ConfirmSyncPayload | 引用 | SyncModelItem.id | 0..N |

---

## 4. 请求/响应类型

### 4.1 GET /v1/models 请求

本接口无请求参数，认证通过浏览器自动携带的 JWT Cookie 完成。

```typescript
/** GET /v1/models 请求配置（无 body/params） */
interface FetchModelsRequest {
  method: 'GET'
  url: '/v1/models'
  headers: {
    Accept: 'application/json'
    // Cookie 由浏览器自动携带，无需显式设置
  }
}
```

### 4.2 GET /v1/models 成功响应

遵循 OpenAI Models List API 格式：

```typescript
/** /v1/models 接口原始响应 */
interface ModelsListResponse {
  /** 固定值 "list" */
  object: 'list'
  /** 模型对象数组 */
  data: ModelsListItem[]
}

/** 单个模型原始数据（OpenAI 格式） */
interface ModelsListItem {
  /** 模型唯一标识（模型名称） */
  id: string
  /** 固定值 "model" */
  object: 'model'
  /** 模型创建时间戳（Unix 秒） */
  created: number
  /** 归属厂商标识（snake_case） */
  owned_by: string
}
```

### 4.3 GET /v1/models 错误响应

```typescript
/** 错误响应结构 */
interface ModelsErrorResponse {
  error: {
    /** 错误描述信息 */
    message: string
    /** 错误类型标识 */
    type: 'auth_error' | 'permission_error' | 'server_error'
  }
}
```

### 4.4 响应映射函数

```typescript
/** 将 API 原始响应映射为前端 ModelInfo 数组 */
function mapModelsResponse(response: ModelsListResponse): ModelInfo[] {
  return response.data.map(item => ({
    id: item.id,
    ownedBy: item.owned_by
  }))
}
```

**字段映射规则**：

| 原始字段路径 | 目标字段 | 映射方式 |
| :--- | :--- | :--- |
| data[].id | ModelInfo.id | 直接赋值 |
| data[].owned_by | ModelInfo.ownedBy | snake_case 转 camelCase |
| data[].object | 不映射 | 前端不使用 |
| data[].created | 不映射 | 前端不使用 |

---

## 5. 本地状态类型

### 5.1 页面状态（UnifiedEntryPageState）

```typescript
/** 统一接入页面整体状态 */
interface UnifiedEntryPageState {
  /** 模型列表数据，初始为空数组 */
  models: ModelInfo[]
  /** 是否正在加载模型列表，初始为 false */
  loading: boolean
  /** 错误信息，null 表示无错误，初始为 null */
  error: string | null
}
```

### 5.2 状态初始值

```typescript
const initialState: UnifiedEntryPageState = {
  models: [],
  loading: false,
  error: null
}
```

### 5.3 状态变更场景

| 场景 | models | loading | error |
| :--- | :--- | :--- | :--- |
| 初始状态 | [] | false | null |
| 发起请求 | [] | true | null |
| 请求成功（有数据） | ModelInfo[] | false | null |
| 请求成功（无数据） | [] | false | null |
| 请求失败（403） | [] | false | "暂无权限查看模型信息" |
| 请求失败（500/503） | [] | false | "服务暂时不可用，请稍后重试" |
| 请求失败（网络异常） | [] | false | "网络异常，请刷新重试" |
| 点击刷新 | [] | true | null |


---

## 6. 变更记录 — CHG-20260523-001

## 变更摘要

新增员工身份认证相关类型（AuthState、User、LoginRequest、LoginResponse、RefreshRequest、RefreshResponse）、Key 管理相关类型（ApiKey、CreateKeyResponse）、模型可用查询响应类型（ModelInfo 扩展 /v1/me/models 适配）。所有类型以 TypeScript interface 形式定义，作为前端类型约束依据。

---

## 新增实体

### AuthState — 认证状态

```typescript
/** Pinia AuthStore 状态 */
interface AuthState {
  /** JWT access token，用于 Bearer 认证 */
  accessToken: string | null
  /** JWT refresh token，用于续期 accessToken */
  refreshToken: string | null
  /** 当前登录用户身份信息 */
  user: User | null
  /** 用户角色 */
  role: 'employee' | 'admin' | null
}
```

### User — 用户身份

```typescript
/** 员工身份信息 */
interface User {
  /** 员工唯一标识 */
  id: string
  /** 员工姓名 */
  name: string
  /** 员工邮箱 */
  email: string
  /** 所属部门 ID */
  departmentId: string
  /** 所属部门名称 */
  departmentName: string
}
```

### ApiKey — API Key（列表展示用，已脱敏）

```typescript
/** Key 列表条目，从 GET /api/v1/keys 响应映射 */
interface ApiKey {
  /** Key 唯一标识 */
  id: string
  /** Key 名称（用户自定义） */
  name: string
  /** Key 脱敏提示，仅末尾 4 位可见，格式："****abcd" */
  keyHint: string
  /** 是否启用 */
  enabled: boolean
  /** 创建时间（ISO 8601 字符串） */
  createdAt: string
}
```

### CreateKeyResponse — 创建 Key 响应

```typescript
/** POST /api/v1/keys 成功响应，包含 Key 明文 */
interface CreateKeyResponse {
  code: number
  message: string
  data: {
    /** 新创建的 Key 唯一标识 */
    id: string
    /** Key 名称 */
    name: string
    /** Key 明文，以 sk- 开头，仅此次返回，后续不可查询 */
    key: string
    /** Key 末尾 4 位 */
    key_hint: string
    enabled: boolean
    created_at: string
    updated_at: string
  }
  request_id: string
}
```

### LoginRequest — OIDC 回调请求

```typescript
/** POST /auth/oidc/callback 请求体 */
interface LoginRequest {
  /** SSO 回调携带的授权码 */
  code: string
  /** SSO 回调携带的 state 参数（防 CSRF） */
  state: string
}
```

### LoginResponse — OIDC 回调响应

```typescript
/** POST /auth/oidc/callback 成功响应 */
interface LoginResponse {
  /** JWT access token */
  accessToken: string
  /** JWT refresh token */
  refreshToken: string
  /** 员工身份信息 */
  user: User
  /** 用户角色 */
  role: 'employee' | 'admin'
}
```

### RefreshRequest — Token 刷新请求

```typescript
/** POST /auth/refresh 请求体 */
interface RefreshRequest {
  /** 当前有效的 refresh token */
  refreshToken: string
}
```

### RefreshResponse — Token 刷新响应

```typescript
/** POST /auth/refresh 成功响应 */
interface RefreshResponse {
  /** 新的 JWT access token */
  accessToken: string
  /** 新的 JWT refresh token（滚动刷新） */
  refreshToken: string
}
```

### ModelInfo（扩展）— 模型信息（/v1/me/models 适配）

```typescript
/**
 * 可用模型信息，从 /v1/me/models 响应映射
 * 相比已有 ModelInfo（/v1/models），新增 type 字段描述模型类型
 */
interface ModelInfo {
  /** 模型唯一标识 */
  id: string
  /** 模型类型：chat / completion / embedding / image / audio */
  type: 'chat' | 'completion' | 'embedding' | 'image' | 'audio'
  /** 是否可用（当前身份可调用） */
  enabled: boolean
}
```

### /v1/me/models 响应

```typescript
/** GET /v1/me/models 原始响应 */
interface MeModelsResponse {
  /** 固定为 list */
  object: 'list'
  /** 当前身份可用的模型列表 */
  data: MeModelItem[]
}

interface MeModelItem {
  /** 模型名称（唯一标识） */
  id: string
  /** OpenAI 兼容固定值 */
  object: 'model'
  /** 提供方标识 */
  owned_by: string
  /** 模型类型 */
  type: string
  /** 是否启用 */
  enabled: boolean
}
```

### /api/v1/keys 列表响应映射函数

```typescript
interface ListKeysResponse {
  code: number
  message: string
  data: {
    items: RawKeyItem[]
    total: number
    page: number
    page_size: number
  }
  request_id: string
}

/** 将 control-plane 原始响应映射为前端 ApiKey 数组 */
function mapKeysResponse(response: ListKeysResponse): ApiKey[] {
  return response.data.items.map(item => ({
    id: item.id,
    name: item.name,
    keyHint: item.key_hint,
    enabled: item.enabled,
    createdAt: item.created_at
  }))
}

interface RawKeyItem {
  id: string
  name: string
  masked_key: string
  key_hint: string
  enabled: boolean
  created_at: string
  updated_at: string
}
```

### 字段映射规则（新增）

| 原始字段路径 | 目标字段 | 映射方式 |
| :--- | :--- | :--- |
| data.items[].id | ApiKey.id | 直接赋值 |
| data.items[].name | ApiKey.name | 直接赋值 |
| data.items[].key_hint | ApiKey.keyHint | snake_case 转 camelCase |
| data.items[].enabled | ApiKey.enabled | 直接赋值 |
| data.items[].created_at | ApiKey.createdAt | snake_case 转 camelCase |

---

## 数据关系

```mermaid
erDiagram
    AuthState ||--|| User : "包含员工身份"
    AuthState ||--o| Token : "持有 JWT Token"

    AuthState {
        string accessToken
        string refreshToken
        User user
        string role
    }

    User {
        string id PK
        string name
        string email
        string departmentId
        string departmentName
    }

    Token {
        string accessToken
        string refreshToken
    }

    CreateKeyResponse ||--|| ApiKey : "创建后转为列表条目"

    CreateKeyResponse {
        number code
        string message
        string request_id
    }

    ApiKey {
        string id PK
        string name
        string keyHint
        boolean enabled
        string createdAt
    }

    LoginRequest {
        string code
        string state
    }

    LoginResponse {
        string accessToken
        string refreshToken
        User user
        string role
    }

    RefreshRequest {
        string refreshToken
    }

    RefreshResponse {
        string accessToken
        string refreshToken
    }

    MeModelsResponse ||--o{ MeModelItem : "包含可用模型"

    MeModelsResponse {
        string object
        MeModelItem[] data
    }

    MeModelItem {
        string id PK
        string object
        string owned_by
        string type
        boolean enabled
    }

    ModelInfo {
        string id PK
        string type
        boolean enabled
    }
```

**新增关系说明**：

| 主体 | 关系 | 客体 | 基数 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| AuthState | 包含 | User | 1..1 | 每个登录态对应唯一用户身份 |
| AuthState | 持有 | Token | 1..1 | accessToken + refreshToken 配对 |
| CreateKeyResponse | 创建后转为 | ApiKey | 1..1 | 创建成功后丢弃 `data.key` 明文，仅保留脱敏条目 |
| MeModelsResponse | 包含 | MeModelItem | 0..N | 当前身份可用的模型列表 |
| LoginRequest | 兑换为 | LoginResponse | 1..1 | OIDC code → 身份 + token |
| RefreshRequest | 兑换为 | RefreshResponse | 1..1 | refreshToken → 新 token 对 |

---

## 本地状态类型（新增）

### AuthStore 持久化存储结构

```typescript
/** localStorage 中 AuthStore 的持久化键 */
const AUTH_STORAGE_KEY = 'ops-console:auth'

/** 持久化到 localStorage 的数据子集 */
interface AuthPersistedState {
  accessToken: string
  refreshToken: string
  user: User
  role: 'employee' | 'admin'
}
```

### KeyListPage 状态

```typescript
interface KeyListPageState {
  keys: ApiKey[]
  loading: boolean
  error: string | null
}
```

### KeyCreateDialog 状态

```typescript
interface KeyCreateDialogState {
  visible: boolean
  name: string
  submitting: boolean
  createdKey: CreateKeyResponse | null
  plaintextCopied: boolean
}
```

### ModelListPage 状态

```typescript
interface ModelListPageState {
  models: ModelInfo[]
  loading: boolean
  error: string | null
}
```

### HomePage 状态

```typescript
interface HomePageState {
  hasKeys: boolean
  hasModels: boolean
  keysLoading: boolean
  modelsLoading: boolean
}
```

### 状态初始值

```typescript
const initialKeyListPageState: KeyListPageState = {
  keys: [],
  loading: false,
  error: null
}

const initialKeyCreateDialogState: KeyCreateDialogState = {
  visible: false,
  name: '',
  submitting: false,
  createdKey: null,
  plaintextCopied: false
}

const initialModelListPageState: ModelListPageState = {
  models: [],
  loading: false,
  error: null
}

const initialHomePageState: HomePageState = {
  hasKeys: false,
  hasModels: false,
  keysLoading: true,
  modelsLoading: true
}
```


## 7. 变更记录 — CHG-20260525-001

## 变更摘要

新增模型组管理相关的前端类型定义，包括核心实体类型（Group、GroupForm、GroupModel 等）、请求/响应类型、前端状态类型、以及表单验证规则。所有类型以 TypeScript interface 形式定义。

---

## 1. 新增类型

### 1.1 策略枚举 — Strategy

```typescript
/** 模型组路由策略类型 */
type Strategy = 'round_robin' | 'priority' | 'weighted'
```

### 1.2 模型组 — Group

```typescript
/** 模型组实体（来自 GET /api/v1/groups 响应） */
interface Group {
  /** 模型组唯一标识 */
  id: string
  /** 组名称 */
  name: string
  /** 组描述 */
  description: string
  /** 路由策略 */
  strategy: Strategy
  /** 是否启用 */
  enabled: boolean
  /** 组内模型数量 */
  model_count: number
  /** 创建时间（ISO 8601） */
  created_at: string
  /** 更新时间（ISO 8601） */
  updated_at: string
}
```

### 1.3 模型组列表项 — GroupListItem

```typescript
/**
 * 列表页展示用的模型组条目
 * 继承 Group，新增 computed 展示字段
 */
interface GroupListItem extends Group {
  /** 策略类型的中文标签 */
  strategy_label: string
}

/** 策略标签映射 */
const STRATEGY_LABEL_MAP: Record<Strategy, string> = {
  round_robin: '轮询',
  priority: '优先级',
  weighted: '加权'
}
```

### 1.4 模型组表单 — GroupForm

```typescript
/** 创建/编辑模型组的表单数据 */
interface GroupForm {
  /** 组名称（必填，最多 64 字符） */
  name: string
  /** 组描述（选填） */
  description: string
  /** 路由策略 */
  strategy: Strategy
}
```

### 1.5 组内模型 — GroupModel

```typescript
/** 组内模型条目 */
interface GroupModel {
  /** 所属模型组 ID（提交时由上下文确定） */
  group_id: string
  /** 模型唯一标识 */
  model_id: string
  /** 模型名称（展示用） */
  model_name: string
  /**
   * 优先级数值
   * - strategy=priority 时必填，范围 0-999，整数
   * - 其他策略时为 undefined
   */
  priority?: number
  /**
   * 权重值
   * - strategy=weighted 时必填，范围 1-100，整数
   * - 其他策略时为 undefined
   */
  weight?: number
}
```

### 1.6 模型选择器选项 — ModelOption

```typescript
/** 模型选择器中的模型选项（来自 GET /api/v1/models） */
interface ModelOption {
  /** 模型唯一标识 */
  id: string
  /** 模型名称（展示用） */
  name: string
}
```

---

## 2. 请求/响应类型

### 2.1 CreateGroupRequest — 创建模型组请求

```typescript
/** POST /api/v1/groups 请求体 */
interface CreateGroupRequest {
  /** 组名称 */
  name: string
  /** 组描述 */
  description?: string
  /** 路由策略 */
  strategy: Strategy
  /** 关联的模型 ID 列表 */
  model_ids: string[]
  /**
   * 优先级映射表：model_id → priority
   * strategy=priority 时必传
   */
  priorities?: Record<string, number>
  /**
   * 权重映射表：model_id → weight
   * strategy=weighted 时必传
   */
  weights?: Record<string, number>
}
```

**JSON 示例**（priority 策略）：
```json
{
  "name": "日常推理",
  "description": "日常推理场景模型组",
  "strategy": "priority",
  "model_ids": ["model_001", "model_002"],
  "priorities": {
    "model_001": 0,
    "model_002": 1
  }
}
```

**JSON 示例**（weighted 策略）：
```json
{
  "name": "高负载场景",
  "description": "高负载场景加权模型组",
  "strategy": "weighted",
  "model_ids": ["model_001", "model_002"],
  "weights": {
    "model_001": 70,
    "model_002": 30
  }
}
```

**JSON 示例**（round_robin 策略）：
```json
{
  "name": "默认轮询组",
  "description": "",
  "strategy": "round_robin",
  "model_ids": ["model_001", "model_002", "model_003"]
}
```

### 2.2 UpdateGroupRequest — 更新模型组请求

```typescript
/** PUT /api/v1/groups/:id 请求体 */
interface UpdateGroupRequest {
  /** 组名称 */
  name?: string
  /** 组描述 */
  description?: string
  /** 路由策略 */
  strategy?: Strategy
}
```

### 2.3 AddModelToGroupRequest — 添加模型到组请求

```typescript
/** POST /api/v1/groups/:id/models 请求体 */
interface AddModelToGroupRequest {
  /** 要添加的模型 ID 列表 */
  model_ids: string[]
  /**
   * 优先级映射表
   * strategy=priority 时必传
   */
  priorities?: Record<string, number>
  /**
   * 权重映射表
   * strategy=weighted 时必传
   */
  weights?: Record<string, number>
}
```

### 2.4 GroupListResponse — 模型组列表响应

```typescript
/** GET /api/v1/groups 成功响应 */
interface GroupListResponse {
  code: number
  message: string
  data: {
    items: Group[]
    total: number
  }
  request_id: string
}
```

**JSON 示例**：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "group_001",
        "name": "日常推理",
        "description": "日常推理场景模型组",
        "strategy": "priority",
        "enabled": true,
        "model_count": 3,
        "created_at": "2026-05-20T10:00:00Z",
        "updated_at": "2026-05-20T10:00:00Z"
      }
    ],
    "total": 1
  },
  "request_id": "req-uuid"
}
```

### 2.5 GroupDetailResponse — 模型组详情响应

```typescript
/** GET /api/v1/groups/:id 成功响应 */
interface GroupDetailResponse {
  code: number
  message: string
  data: Group
  request_id: string
}
```

### 2.6 GroupModelsResponse — 组内模型列表响应

```typescript
/** GET /api/v1/groups/:id/models 成功响应 */
interface GroupModelsResponse {
  code: number
  message: string
  data: {
    items: GroupModel[]
  }
  request_id: string
}
```

**JSON 示例**：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "group_id": "group_001",
        "model_id": "model_001",
        "model_name": "gpt-4o",
        "priority": 0
      },
      {
        "group_id": "group_001",
        "model_id": "model_002",
        "model_name": "qwen-turbo",
        "priority": 1
      }
    ]
  },
  "request_id": "req-uuid"
}
```

### 2.7 ModelListForSelector — 模型选择器响应

```typescript
/** GET /api/v1/models 选择器用响应 */
interface ModelListForSelectorResponse {
  code: number
  message: string
  data: {
    items: ModelOption[]
  }
  request_id: string
}
```

### 2.8 通用错误响应

```typescript
/** 通用错误响应 */
interface ErrorResponse {
  code: number
  message: string
  data: null
  request_id: string
}

/** 409 冲突错误响应 */
interface ConflictErrorResponse extends ErrorResponse {
  code: 409
  message: string  // e.g. "模型组名称已存在"
}
```

---

## 3. 表单验证规则

### 3.1 组名称 (name)

```typescript
const nameRules = [
  { required: true, message: '请输入组名称', trigger: 'blur' },
  { max: 64, message: '组名称不能超过 64 个字符', trigger: 'blur' },
  {
    validator: (_rule: any, value: string, callback: Function) => {
      if (value && value.trim() === '') {
        callback(new Error('组名称不能为纯空格'))
      } else {
        callback()
      }
    },
    trigger: 'blur'
  }
]
```

| 规则 | 类型 | 值 | 错误提示 |
| :--- | :--- | :--- | :--- |
| 必填 | required | true | "请输入组名称" |
| 最大长度 | max | 64 | "组名称不能超过 64 个字符" |
| 去空格 | whitespace | — | "组名称不能为纯空格" |
| 提交前 trim | transform | `value.trim()` | 自动去除首尾空格 |

### 3.2 路由策略 (strategy)

```typescript
const strategyRules = [
  { required: true, message: '请选择路由策略', trigger: 'change' }
]
```

| 规则 | 类型 | 值 | 错误提示 |
| :--- | :--- | :--- | :--- |
| 必填 | required | true | "请选择路由策略" |

### 3.3 已选模型 (selectedModels)

```typescript
// 提交时手动校验（非 el-form rules）
function validateSelectedModels(): boolean {
  return selectedModels.length > 0
}
// 失败时 ElMessage.warning('请至少添加一个模型')
```

| 规则 | 校验时机 | 条件 | 错误提示 |
| :--- | :--- | :--- | :--- |
| 至少一个 | submit | `selectedModels.length === 0` | "请至少添加一个模型" |

### 3.4 优先级 (priority) — strategy=priority 时

```typescript
function validatePriority(value: number | undefined): boolean {
  return value != null
    && Number.isInteger(value)
    && value >= 0
    && value <= 999
}
```

| 规则 | 条件 | 错误提示 |
| :--- | :--- | :--- |
| 非空 | `value == null` | "请为每个模型设置有效的优先级" |
| 整数 | `!Number.isInteger(value)` | "优先级必须为整数" |
| 最小值 | `value < 0` | "优先级不能小于 0" |
| 最大值 | `value > 999` | "优先级不能大于 999" |

### 3.5 权重 (weight) — strategy=weighted 时

```typescript
function validateWeight(value: number | undefined): boolean {
  return value != null
    && Number.isInteger(value)
    && value >= 1
    && value <= 100
}
```

| 规则 | 条件 | 错误提示 |
| :--- | :--- | :--- |
| 非空 | `value == null` | "请为每个模型设置有效的权重" |
| 整数 | `!Number.isInteger(value)` | "权重必须为整数" |
| 最小值 | `value < 1` | "权重不能小于 1" |
| 最大值 | `value > 100` | "权重不能大于 100" |

---

## 4. 数据关系

```mermaid
erDiagram
    Group ||--o{ GroupModel : "包含模型"
    GroupForm ||--|| Group : "映射为"
    GroupListItem ||--|| Group : "继承"

    Group {
        string id PK
        string name
        string description
        string strategy
        boolean enabled
        number model_count
        string created_at
        string updated_at
    }

    GroupForm {
        string name
        string description
        string strategy
    }

    GroupListItem {
        string id PK
        string name
        string description
        string strategy
        boolean enabled
        number model_count
        string strategy_label
        string created_at
        string updated_at
    }

    GroupModel {
        string group_id FK
        string model_id FK
        string model_name
        number priority
        number weight
    }

    ModelOption {
        string id PK
        string name
    }

    CreateGroupRequest {
        string name
        string description
        string strategy
        string model_ids
        object priorities
        object weights
    }

    UpdateGroupRequest {
        string name
        string description
        string strategy
    }

    AddModelToGroupRequest {
        string model_ids
        object priorities
        object weights
    }
```

**关系说明**：

| 主体 | 关系 | 客体 | 基数 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| Group | 包含 | GroupModel | 0..N | 一个模型组可包含多个模型 |
| GroupListItem | 继承 | Group | 1..1 | 列表项基于实体增加计算字段 |
| GroupForm | 映射为 | CreateGroupRequest / UpdateGroupRequest | 1..1 | 表单数据序列化为 API 请求体 |
| GroupModel | 映射为 | AddModelToGroupRequest | 0..N | 模型列表序列化为入组请求 |

---

## 5. 字段映射规则

| 原始字段（API snake_case） | 前端字段（camelCase） | 说明 |
| :--- | :--- | :--- |
| data.items[].id | Group.id | 直接赋值 |
| data.items[].name | Group.name | 直接赋值 |
| data.items[].description | Group.description | 直接赋值 |
| data.items[].strategy | Group.strategy | 直接赋值（枚举值一致） |
| data.items[].enabled | Group.enabled | 直接赋值 |
| data.items[].model_count | Group.model_count | 保持 snake_case（前端列表展示字段无映射必要） |
| data.items[].created_at | Group.created_at | 保持 snake_case |
| data.items[].updated_at | Group.updated_at | 保持 snake_case |
| items[].group_id | GroupModel.group_id | 保持 snake_case |
| items[].model_id | GroupModel.model_id | 保持 snake_case |
| items[].model_name | GroupModel.model_name | 保持 snake_case |
| items[].priority | GroupModel.priority | 直接赋值 |
| items[].weight | GroupModel.weight | 直接赋值 |

> 注：本次新增类型中大部分字段与 API 返回的 snake_case 一致（如 `model_count`、`created_at` 等），未做 snake_case → camelCase 转换，以保持与 control-plane 响应格式的一致性和最小的映射层复杂度。仅在需要与其他已有前端类型对齐时做转换。

---

## 6. 状态类型

### 6.1 GroupsState — 列表页状态

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

### 6.2 GroupEditState — 编辑弹窗状态

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

### 6.3 ModelGroupSelectorState — 选择器状态

```typescript
interface ModelGroupSelectorState {
  allModels: ModelOption[]
  searchKeyword: string
  selectedIds: Set<string>
  loading: boolean
}

const initialSelectorState: ModelGroupSelectorState = {
  allModels: [],
  searchKeyword: '',
  selectedIds: new Set(),
  loading: false
}
```

### 6.4 状态变更矩阵

**GroupsState**：

| 场景 | groups | loading | error | searchKeyword |
| :--- | :--- | :--- | :--- | :--- |
| 初始 | [] | false | null | '' |
| 请求发起 | 保持 | true | null | — |
| 请求成功 | GroupListItem[] | false | null | — |
| 空列表 | [] | false | null | — |
| 网络异常 | [] | false | "网络异常，请刷新重试" | — |
| 服务端错误 | [] | false | "操作失败，请重试" | — |
| 搜索触发 | — | true | null | 用户输入 |
| 删除/创建成功 | 触发刷新 | — | — | — |

**GroupEditState**：

| 场景 | form | submitting | selectedModels |
| :--- | :--- | :--- | :--- |
| 创建模式打开 | 空表单 | false | [] |
| 编辑模式打开 | 回填数据 | false | 已加载模型列表 |
| 提交中 | — | true | — |
| 提交成功 → 关闭 | resetForm() | false | [] |
| 提交失败 | 保持 | false | 保持 |
| 策略切换 | strategy 更新 | — | priority/weight 清空 |
| 添加模型 | — | — | push |
| 移除模型 | — | — | splice |
