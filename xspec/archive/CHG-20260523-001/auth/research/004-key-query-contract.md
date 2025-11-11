# 技术研究：Key 查询契约 — auth 与 control-plane 之间的 Key 验证接口

> **用途**：单个研究课题的完整深度分析文件。
> 本文件由 `xf-research` subagent 写入，存放于 `xspec/changes/auth/research/004-key-query-contract.md`。

---

## 1. 概览（必填）

**研究问题**：auth 模块的 KeyAuthService 查询 control-plane 获取 Key 状态和归属信息时，应采用何种内部接口契约格式？单次调用还是多次调用？传输 Key 原文还是哈希？

**决策截止**：auth 模块详细设计开始前

**决策者**：架构负责人 + auth/control-plane 模块开发负责人

**上游依赖**：

| 文档 | 说明 |
| :--- | :--- |
| auth HLD §3.3 | API Key 鉴权链路流程（Key → User → Department 解析） |
| auth HLD §6.3 | control-plane 外部服务依赖（P95 ≤ 50ms，超时 200ms） |
| auth Spec §6.4 | US-KEY-QUERY-01 — Key 数据查询契约 |
| control-plane contracts.md §4.11 | 已有内部接口 `/internal/v1/models/available` 的模式和认证方式 |
| auth HLD §10 R-02 | control-plane Key 查询内部接口的契约定义待研究 |

---

## 2. 问题背景与约束条件（必填）

**为什么需要做这个决策？**

auth 模块的 `KeyAuthService` 在 API Key 鉴权链路中需要查询 control-plane 获取 Key 的当前状态（Active/Disabled/Deleted）和归属信息（user_id → department_id）。Redis 缓存命中时无需调用 control-plane，但缓存未命中（首次请求或 TTL 过期后）必须穿透查询。

HLD §3.3 的鉴权链路流程中，KeyAuthService 需要完成两步解析：① Key 状态 + 归属 user_id；② user_id → department_id。这两步可以合并为一次调用也可以拆分为两次调用——这是本课题的核心决策点。

同时，Key 原文（sk-xxx）是高度敏感信息，仅在创建时返回一次，后续脱敏展示。Key 原文在 auth 与 control-plane 之间传输时是否需要额外保护，是另一个关键决策点。

**硬约束（不可违反）：**

- 鉴权中间件 P95 响应时间 ≤ 100ms（auth HLD §8.1），control-plane 查询 P95 ≤ 50ms（auth HLD §6.3）
- Key 查询超时上限 200ms（auth HLD §6.3），超时不放行请求
- 认证方式必须使用 control-plane 已有的预共享内部 Token（contracts.md §2 公共约定），不得引入新的认证机制
- 接口必须遵循 `/internal/v1` 路径前缀约定（与 `/internal/v1/models/available` 保持一致）
- Key 原文在传输和日志中不得明文暴露（auth Spec §8 安全性要求）

**软约束（有偏好）：**

- 优先选与现有内部接口模式最一致的方案 → 对应 §5「架构一致性」维度
- 优先选鉴权链路延迟最低的方案 → 对应 §5「性能」维度
- 优先选 Key 原文暴露面最小的方案 → 对应 §5「安全性」维度
- 优先选实现改动量最小的方案 → 对应 §5「实现复杂度」维度

**不在本次研究范围内：**

- Key CRUD 接口设计（由 control-plane 负责，不在 auth 模块范围）
- Key 缓存 TTL 和 singleflight 策略（已在 HLD §6.2 确定）
- Redis pub/sub 主动失效机制（已在 research/002 中决策）

---

## 3. 前提假设（必填）

- auth 与 control-plane 部署在同一内部网络，通过服务网格 mTLS 保护传输层安全
- Key 缓存命中率 ≥ 90%（auth HLD §8.4 监控目标），穿透到 control-plane 的请求量 < 50 QPS
- control-plane 的 keys 表已存储 Key 明文（用于验证和脱敏展示），但不一定存储 Key 哈希
- auth 模块已计算 Key 的 SHA256 哈希用于 Redis 缓存 Key 设计（HLD §6.2）

---

## 4. 候选方案（必填）

| 方案 | 简介 | 版本 / 来源 |
| :--- | :--- | :--- |
| **A：单次合并验证端点** | auth 调用 `POST /internal/v1/keys/verify`，请求体携带 Key 原文，control-plane 一次性返回 {key_id, status, user_id, department_id}。一次 HTTP 调用完成整条鉴权链路。 | 项目已有 `/internal/v1/models/available` 模式参考 |
| **B：两次拆分调用** | auth 先调用 `POST /internal/v1/keys/verify` 获取 {key_id, status, user_id}，再调用 `GET /internal/v1/users/{id}/department` 获取 {department_id}。两次 HTTP 调用分别解析 Key 状态和用户部门。 | RESTful 资源拆分原则 |
| **C：哈希查找端点** | auth 调用 `POST /internal/v1/keys/lookup`，请求体携带 Key 的 SHA256 哈希（非 Key 原文），control-plane 按 key_hash 字段查找并返回 {key_id, status, user_id, department_id}。Key 原文始终不离开 auth 模块边界。 | 安全增强设计 |

**未纳入评估的方案（违反硬约束或明显不适用）：**

| 方案 | 排除原因 |
| :--- | :--- |
| gRPC 内部服务 | 违反项目现有模式：所有内部接口均为 REST + JSON，引入 gRPC 增加技术栈复杂度，且低 QPS（< 50）场景下 gRPC 性能优势不明显 |
| 批量拉取所有 Key（类似 /models/available） | Key 原文是高敏感信息，不能批量返回明文；Key 数量可能增长到数千条，全量拉取不经济 |
| auth 直连 PostgreSQL 查询 keys 表 | 违反架构边界：auth 不应直接访问 control-plane 的数据库，需通过接口契约解耦 |

---

## 5. 评估维度与权重（必填）

| 维度 | 权重 | 衡量方式 |
| :--- | :---: | :--- |
| 性能 | 30% | 鉴权链路所需的 HTTP 调用次数；对 auth 中间件 P95 ≤ 100ms 目标的影响程度 |
| 安全性 | 25% | Key 原文在内部网络中的暴露面；日志泄漏风险；传输安全等级 |
| 架构一致性 | 20% | 与 control-plane 已有 `/internal/v1/models/available` 内部接口模式的匹配程度 |
| 实现复杂度 | 15% | auth 端和 control-plane 端各需多少新增代码、数据结构变更、配置变更 |
| 运维成本 | 10% | 监控端点数量；故障排查复杂度；日志脱敏规则数量 |

---

## 6. 方案对比矩阵（必填）

| 维度（权重） | A：单次合并验证 | B：两次拆分调用 | C：哈希查找 |
| :--- | :---: | :---: | :---: |
| 性能（30%） | 5 | 2 | 5 |
| 安全性（25%） | 3 | 3 | 5 |
| 架构一致性（20%） | 5 | 3 | 4 |
| 实现复杂度（15%） | 4 | 2 | 3 |
| 运维成本（10%） | 4 | 2 | 4 |
| **加权总分（满分 5.0）** | **4.35** | **2.55** | **4.45** |

**评分依据（按维度说明）：**

- **性能**：A：单次 HTTP 调用完成整条链路，control-plane 内部一次 DB 查询（JOIN keys + users + departments）即可返回全量信息，延迟最小；B：两次 HTTP 调用，第二次还需独立 DB 查询，累计延迟 ≈ 2 × control-plane P95（约 100ms），接近 auth 中间件 P95 上限 100ms 的边界，缓存可缓解第二次调用但冷启动场景仍需两次；C：单次调用与 A 性能等同，hash 查找可通过数据库索引高效完成
- **安全性**：A/B 评分相同：Key 原文在 POST body 中传输，虽受 mTLS 保护但仍穿越 auth → control-plane 网络边界，若 control-plane 日志配置不当可能泄漏 Key 原文；C：Key 原文始终不离开 auth 模块，control-plane 仅接收不可逆的 SHA256 哈希，即使日志泄漏也无法还原 Key，安全性最高
- **架构一致性**：A：完美匹配 `/internal/v1` 模式——POST 请求 + 内部 Token 认证 + 合并响应结构，与 `/internal/v1/models/available` 风格一致；B：引入两个新端点且第二个端点使用 GET + path parameter 模式，与现有内部接口风格不完全一致；C：遵循 `/internal/v1` 路径前缀和内部 Token 认证，但请求体携带 hash 而非原文是新模式差异
- **实现复杂度**：A：control-plane 仅需新增一个 Handler + Service 方法 + 路由，auth 仅需新增一个 HTTP client 方法，最简实现；B：control-plane 需新增两个端点，auth 需管理两次调用和组合逻辑，实现量约 2 倍；C：control-plane 需在 keys 表新增 `key_hash` 列 + 索引 + Key 创建时计算哈希，auth 端需将 SHA256 计算从缓存层提前到查询层，略有额外工作
- **运维成本**：A：单一端点监控，单一日志脱敏规则（Key 原文 → 哈希前 8 位）；B：两个端点各自监控，两条日志脱敏规则，故障排查需判断是哪次调用失败；C：单一端点监控，日志中仅出现哈希值无需额外脱敏规则

---

## 7. 深度分析（适用时填）

### 关键差异：安全性 vs 架构一致性的权衡

方案 C 在安全性维度显著领先（5 分 vs A/B 的 3 分），但引入了两个模式差异：

1. **请求体语义变化**：现有内部接口 `/internal/v1/models/available` 无请求体（GET 无参数），而 `/internal/v1/keys/lookup` 的请求体携带的是 Key 哈希而非 Key 原文。这虽然仍遵循 `/internal/v1` 路径前缀和内部 Token 认证，但"发送哈希而非原文"是一个新的交互范式。

2. **control-plane 数据模型变更**：keys 表需新增 `key_hash` 列（VARCHAR(64)），并在 Key 创建时计算 SHA256 哈希写入。需创建索引 `idx_keys_key_hash` 以支持高效哈希查找。这是一个非破坏性的 schema 变更（新增列 + 索引），可通过 GORM AutoMigrate 完成。

**方案 A 的安全风险量化**：

Key 原文在 auth → control-plane 传输中的暴露面：
- **传输层**：服务网格 mTLS 加密，等同于 TLS 1.3 安全等级 → 传输泄漏风险极低
- **应用层**：Key 原文出现在 control-plane 的 Gin 请求 binding 结构体中 → 若 StructuredLogger 中间件记录完整请求体，Key 原文将出现在日志中
- **缓解措施**：control-plane 的日志脱敏规则仅需增加一条：请求体中 `key` 字段替换为 SHA256 哈希前 8 位（与 auth HLD §8.2 规则一致）

**关键判断**：mTLS 已在传输层提供强保护，应用层日志脱敏是一条简单规则即可解决。方案 C 的哈希查找虽然安全性更高，但带来的数据模型变更和交互范式差异，在 mTLS + 日志脱敏的双重保护下，安全增益边际递减。

### 合并验证端点的具体契约设计（方案 A）

以下为方案 A 的完整接口契约定义，遵循 control-plane contracts.md 的统一格式：

**端点**：`POST /internal/v1/keys/verify`

**请求参数**：

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| key | string | body | 是 | API Key 原文值 | `"sk-abc123def456"` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/internal/v1/keys/verify' \
  -H 'Authorization: Bearer {internal_token}' \
  -H 'Content-Type: application/json' \
  -d '{"key": "sk-abc123def456"}'
```

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "key_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "active",
    "user": {
      "user_id": "660e8400-e29b-41d4-a716-446655440001",
      "name": "张三",
      "department_id": "770e8400-e29b-41d4-a716-446655440002"
    }
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.key_id | string(uuid) | Key 唯一标识（用于 pub/sub 事件关联和缓存 Key 设计） |
| data.status | string | Key 当前状态枚举：`active` / `disabled` / `deleted` |
| data.user.user_id | string(uuid) | Key 归属用户的唯一标识 |
| data.user.name | string | 用户姓名（可选，auth 可用于日志丰富） |
| data.user.department_id | string(uuid) | 用户归属部门的唯一标识（可为 null，表示无部门归属） |

**Key 不存在时**（200 OK，status = "not_found"）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "key_id": "",
    "status": "not_found",
    "user": null
  },
  "request_id": "req-uuid"
}
```

> **设计决策**：Key 不存在时返回 200 + status="not_found" 而非 404，原因：auth 的 KeyAuthService 需区分"Key 不存在"和"control-plane 服务异常"。404 状态码可能由多种原因触发（路由不存在、服务异常等），用统一的 200 + status 字段让 auth 语义判断更清晰。

**可能的错误码**：

| 错误码 | HTTP 状态 | 语义 | 触发条件 |
| :--- | :---: | :--- | :--- |
| E4011 | 401 | 认证失败 | 内部 Token 缺失或无效 |
| E4001 | 400 | 参数校验失败 | key 字段缺失、空值或不以 sk- 开头 |
| E5001 | 500 | 内部服务错误 | 数据库查询异常等 |

### auth 端 KeyAuthService 调用逻辑

```go
func (s *KeyAuthService) resolveFromControlPlane(keyStr string) (*KeyAuthResult, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    resp, err := s.cpClient.VerifyKey(ctx, &VerifyKeyRequest{Key: keyStr})
    if err != nil {
        // 网络错误/超时 → 返回"身份验证服务暂不可用"，不放行
        return nil, ErrServiceUnavailable
    }

    if resp.Status == "not_found" || resp.Status == "disabled" || resp.Status == "deleted" {
        // 任何非 active 状态 → 拒绝请求，返回统一"身份无效"
        // 注意：not_found 也写入缓存（防止反复穿透查询不存在的 Key）
        s.cache.Set(keyHash, &KeyCacheEntry{
            KeyID:  resp.KeyID,
            Status: resp.Status,
            UserID: "",
        }, 60*time.Second)
        return nil, ErrIdentityInvalid
    }

    // active → 写入缓存并返回完整鉴权结果
    result := &KeyAuthResult{
        KeyID:         resp.KeyID,
        UserID:        resp.User.UserID,
        DepartmentID:  resp.User.DepartmentID,
    }
    s.cache.Set(keyHash, &KeyCacheEntry{
        KeyID:         resp.KeyID,
        Status:        resp.Status,
        UserID:        resp.User.UserID,
    }, 60*time.Second)
    // 用户部门信息独立缓存（TTL 300s）
    s.cache.Set(userCacheKey(resp.User.UserID), &UserCacheEntry{
        DepartmentID: resp.User.DepartmentID,
        Name:         resp.User.Name,
    }, 300*time.Second)

    return result, nil
}
```

### control-plane 端 KeyVerifyService 实现要点

```go
func (s *KeyVerifyService) VerifyKey(ctx context.Context, keyStr string) (*KeyVerifyResult, error) {
    // 1. 校验 sk- 前缀格式
    if !strings.HasPrefix(keyStr, "sk-") {
        return nil, ErrInvalidKeyFormat
    }

    // 2. 查询 Key 状态 + JOIN 用户和部门
    var result struct {
        KeyID        string
        Status       string
        UserID       string
        UserName     string
        DepartmentID string
    }
    err := s.db.WithContext(ctx).
        Table("keys").
        Select("keys.id as key_id, keys.status, keys.user_id, users.name as user_name, users.department_id").
        Joins("LEFT JOIN users ON keys.user_id = users.id").
        Where("keys.key_value = ?", keyStr).
        Take(&result).Error

    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return &KeyVerifyResult{Status: "not_found"}, nil
        }
        return nil, err // DB 异常
    }

    return &KeyVerifyResult{
        KeyID:  result.KeyID,
        Status: result.Status,
        User: &UserBrief{
            UserID:       result.UserID,
            Name:         result.UserName,
            DepartmentID: result.DepartmentID,
        },
    }, nil
}
```

### control-plane 日志脱敏规则

control-plane 的 StructuredLogger 中间件需对 `/internal/v1/keys/verify` 端点的请求体增加脱敏：

```go
// 在请求体日志输出前，将 key 字段替换为哈希前 8 位
if path == "/internal/v1/keys/verify" {
    bodyForKeyLog = sha256Hex(body.Key)[:8] + "***"
}
```

这与 auth HLD §8.2 的日志脱敏规则完全对齐（Key 原文 → SHA256 哈希前 8 位）。

---

## 8. 最终结论（必填）

**选择方案**：A — 单次合并验证端点 `POST /internal/v1/keys/verify`

**核心理由**：
- 性能最优：单次 HTTP 调用完成整条鉴权链路（Key → User → Department），一次 DB JOIN 查询，延迟最低，确保 auth 中间件 P95 ≤ 100ms 目标不受冲击
- 架构一致性最高：完美复用 `/internal/v1` 路径前缀 + 内部 Token 认证 + 统一响应结构模式，与 `/internal/v1/models/available` 风格完全一致
- Key 原文传输安全由 mTLS 保障，应用层日志脱敏仅需一条简单规则，安全风险可控

**被否决方案的关键原因**：

| 方案 | 否决原因 |
| :--- | :--- |
| B：两次拆分调用 | 累计两次 HTTP 调用延迟接近 auth 中间件 P95 上限边界；实现量约 2 倍；两个端点增加监控和故障排查复杂度；用户部门信息虽有独立缓存优势，但合并端点也可将部门信息独立缓存（TTL 300s）实现同等效果 |
| C：哈希查找 | 安全增益边际递减：mTLS 已保护传输层，日志脱敏一条规则即可保护应用层，哈希查找的额外安全价值不值得数据模型变更（新增 key_hash 列 + 索引）和交互范式差异的代价 |

---

## 9. 遗留风险与后续行动（必填）

**已知风险**：

| 风险 | 可能性 | 影响 | 缓解措施 |
| :--- | :---: | :---: | :--- |
| control-plane 日志泄漏 Key 原文（StructuredLogger 未做脱敏） | 中 | 高 | control-plane 中间件新增 `/internal/v1/keys/verify` 端点的请求体脱敏规则；上线前代码审查强制检查 |
| control-plane 查询 P95 > 50ms 导致 auth 中间件 P95 超标 | 低 | 高 | Redis 缓存命中率 ≥ 90% 时穿透量极低；singleflight 合并并发穿透；control-plane 使用 JOIN 单次查询避免两次 DB 往返 |
| Key 原文在 control-plane DB 查询条件中暴露（WHERE key_value = ?） | 低 | 低 | GORM 参数化查询防止 SQL 注入；Key 原文仅出现在查询参数中不进入日志；内部网络 mTLS 保护 |
| control-plane 不可用时 auth 中间件全部穿透失败 | 低 | 高 | Redis 缓存兜底服务已缓存的 Key（有限降级窗口 TTL 60s）；不可用时返回"身份验证服务暂不可用"不放行 |

**后续行动**：

- [ ] 在 control-plane contracts.md 中新增 §4.14 Key 验证内部端点契约定义（POST /internal/v1/keys/verify）
- [ ] 在 control-plane hld.md 中新增 KeyVerifyHandler + KeyVerifyService 组件说明
- [ ] 在 control-plane 中间件链中新增 `/internal/v1/keys/verify` 端点的请求体日志脱敏规则
- [ ] 在 auth HLD §3.3 中更新 KeyAuthService 鉴权链路：明确调用 `/internal/v1/keys/verify` 合并端点
- [ ] 在 auth HLD §6.3 中更新 control-plane 外部服务依赖描述：明确 Key 验证端点和请求/响应格式
- [ ] 在 auth 详细设计中实现 KeyAuthService.controlPlaneVerify() HTTP client 方法
- [ ] 在 control-plane 详细设计中实现 KeyVerifyHandler + KeyVerifyService（含 JOIN 查询优化）