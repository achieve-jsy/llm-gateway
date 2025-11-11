# 技术研究：OIDC Claims 角色映射 — admin/user 角色判定

> **用途**：单个研究课题的完整深度分析文件。
> 本文件由 `xf-research` subagent 写入，存放于 `xspec/changes/auth/research/003-oidc-role-mapping.md`。

---

## 1. 概览（必填）

**研究问题**：如何从 SSO OIDC claims 中判定用户角色（admin / user）？采用直接读取还是配置映射？

**决策截止**：auth 模块详细设计开始前

**决策者**：架构负责人 + auth 模块开发负责人

**上游依赖**：

| 文档 | 说明 |
| :--- | :--- |
| HLD | auth 模块概要设计 §5.4 授权模型（简单角色 admin/user）、§3.2 OIDC 登录流程 |
| Spec | CHG-20260523-001 身份闭环 |

---

## 2. 问题背景与约束条件（必填）

**为什么需要做这个决策？**

auth 模块 OIDC 登录流程中，`OIDCService.HandleCallback()` 解析 id_token claims 后需为用户赋角色（admin 或 user）。HLD §5.4 明确角色来源为"SSO 登录时由 OIDC claims 或配置映射决定"，但具体映射方式未确定。

不同的 SSO 提供商（Okta、Azure AD、自建 IdP 等）在 OIDC claims 中携带角色/组信息的方式各异：有的用标准 `groups` claim，有的用自定义 `role` claim，有的用命名空间前缀的 custom claim。映射规则需要与公司 SSO 实际提供的 claims 对齐，同时保持足够的灵活性应对 SSO 配置变更。

**硬约束（不可违反）：**

- 角色模型为简单二元：admin / user（HLD §5.4 已确定），不得引入多层级 RBAC
- 必须在 SSO 登录时确定角色（HLD §5.4 角色来源明确），不得依赖后续查询或手动赋值
- 开发模式下角色为固定值 `user`（HLD §3.5 已确定），不受 OIDC claims 影响
- V0.2.0 首先跑通身份闭环，RBAC/多角色延后到 V0.5（宪章"渐进演进"原则）

**软约束（有偏好）：**

- 优先选与 SSO 配置变更解耦的方案（SSO claims 结构变化不要求改代码） → 对应 §5「可维护性」维度
- 优先选 V0.2 可快速落地的方案 → 对应 §5「实现复杂度」维度
- 优先选可演进到 V0.5 RBAC 的方案 → 对应 §5「演进兼容性」维度

**不在本次研究范围内：**

- V0.5 的 RBAC 多角色/多层级设计——超出当前版本范围
- SSO 提供商选择——公司 IT 部门已决定
- OIDC claims 中除角色外的其他字段映射（sub/name/email/department 已在 HLD 中确定）

---

## 3. 前提假设（必填）

- 公司 SSO 的 OIDC id_token 中会包含某种角色/组信息（具体 claim 名和值需与 IT 部门确认，属 R-01 研究范围）
- 管理员人数极少（< 10 人），角色映射规则极简（一条规则即可判定 admin）
- 管理员名单稳定，变更频率极低（月级变更）
- V0.2 阶段仅需 admin / user 二元角色，V0.5 才引入多角色/权限矩阵

---

## 4. 候选方案（必填）

| 方案 | 简介 | 版本 / 来源 |
| :--- | :--- | :--- |
| **A：配置映射（YAML/Viper）** | auth 配置文件中定义角色映射规则：`admin_identifiers: ["group:llm-gateway-admins"]`。OIDCService 从 id_token claims 中读取指定 claim（如 groups），与配置中的 admin 标识匹配。匹配则 admin，否则 user。 | Viper 配置管理 |
| **B：直接硬编码 claim 读取** | OIDCService 代码中硬编码读取 id_token 的 `groups` claim，若包含 `"admin"` 字符串则赋角色 admin。SSO claims 结构变化需改代码。 | 最简实现 |
| **C：数据库角色覆盖表** | SSO 登录后查本地 `user_role_overrides` 表，覆盖 SSO claims 决定的角色。未覆盖时 fallback 到 claims 读取。 | 关系型持久化 |

**未纳入评估的方案：**

| 方案 | 排除原因 |
| :--- | :--- |
| 外部 RBAC 服务 | 违反宪章"渐进演进"原则：V0.2 不引入 RBAC 服务依赖 |
| 全量数据库角色表（每个用户一条记录） | 过度设计：V0.2 仅需判定 admin/user，维护全量角色表超出阶段目标 |

---

## 5. 评估维度与权重（必填）

| 维度 | 权重 | 衡量方式 |
| :--- | :---: | :--- |
| 可维护性 | 30% | SSO claims 结构变化时是否需要改代码；规则变更是否需要重新部署 |
| 实现复杂度 | 25% | 需多少新增代码/数据结构；是否引入新依赖 |
| 演进兼容性 | 20% | V0.5 引入 RBAC 时改动量；现有映射规则能否平滑扩展 |
| 运维灵活性 | 15% | 管理员名单变更时操作步骤；是否需要数据库操作 |
| 安全性 | 10% | 角色映射规则是否可被恶意篡改；配置变更是否需审批 |

---

## 6. 方案对比矩阵（必填）

| 维度（权重） | A：配置映射 | B：硬编码读取 | C：数据库覆盖表 |
| :--- | :---: | :---: | :---: |
| 可维护性（30%） | 5 | 1 | 4 |
| 实现复杂度（25%） | 4 | 5 | 2 |
| 演进兼容性（20%） | 4 | 2 | 3 |
| 运维灵活性（15%） | 3 | 1 | 5 |
| 安全性（10%） | 4 | 5 | 3 |
| **加权总分（满分 5.0）** | **4.10** | **2.25** | **3.25** |

**评分依据：**

- **可维护性**：A：映射规则在配置文件中，SSO claims 变化只需改配置（claim_path + match_value），无需改代码重新部署；B：硬编码 claim 名和匹配逻辑，SSO 变化必须改代码重部署；C：数据库覆盖规则运行时可改，但 SSO claims 字段变化仍需改 OIDCService 代码（claim 读取逻辑硬编码）
- **实现复杂度**：A：仅需在 OIDCService 中读取配置 + 比对 claim 值，约 20 行新增代码，无新依赖（Viper 已在技术栈）；B：最简，约 10 行代码；C：需新增数据库表 + Repository + 运维 CRUD 端点，实现量最大
- **演进兼容性**：A：V0.5 时将 YAML 映射扩展为多规则多角色映射即可，配置结构自然演进；B：V0.5 需重写角色判定逻辑，硬编码难以扩展；C：数据库表可支持多角色，但 V0.2 引入数据库角色表过度设计
- **运维灵活性**：A：管理员变更需改配置文件并重启（或热加载）；B：管理员变更需改代码重部署；C：管理员变更通过 ops-console 操作数据库，最灵活但 V0.2 不需要此灵活性
- **安全性**：A：配置文件通过环境变量/Secret 注入（Viper 支持），不易被运行时篡改；B：代码中硬编码最安全（不可运行时篡改），但可维护性代价太高；C：数据库记录需 admin 权限才能修改，但引入了数据库攻击面

---

## 7. 深度分析（适用时填）

### 关键差异：配置映射的 claim_path 设计

方案 A 的核心设计是：配置中声明从 id_token 哪个 claim 读取角色标识，以及什么样的值判定为 admin。这需要解决 OIDC claims 的多样性问题——不同 SSO 提供商用不同 claim 名携带角色信息。

**配置结构设计**：

```yaml
auth:
  oidc:
    role_mapping:
      claim_path: "groups"                    # 从 id_token 的哪个 claim 读取角色标识
      admin_match_values:                     # claim 值中包含以下任一值则判定为 admin
        - "llm-gateway-admins"
        - "platform-admin"
      default_role: "user"                    # 不匹配 admin 条件时的默认角色
```

**OIDCService 角色判定逻辑**：

```go
func (s *OIDCService) determineRole(claims map[string]interface{}) string {
    claimPath := s.config.OIDC.RoleMapping.ClaimPath
    adminValues := s.config.OIDC.RoleMapping.AdminMatchValues
    
    claimValue, exists := claims[claimPath]
    if !exists {
        return s.config.OIDC.RoleMapping.DefaultRole // claim 不存在则默认 user
    }
    
    // claim 值可能是字符串数组（如 groups）或单个字符串
    switch v := claimValue.(type) {
    case []interface{}:
        for _, item := range v {
            for _, adminVal := range adminValues {
                if fmt.Sprintf("%v", item) == adminVal {
                    return "admin"
                }
            }
        }
    case string:
        for _, adminVal := range adminValues {
            if v == adminVal {
                return "admin"
            }
        }
    }
    
    return s.config.OIDC.RoleMapping.DefaultRole
}
```

**claim_path 兼容常见 SSO 提供商**：

| SSO 提供商 | 常用角色 claim 名 | claim 值类型 | 配置示例 |
| :--- | :--- | :--- | :--- |
| Okta | `groups` | string[] | `claim_path: "groups", admin_match_values: ["LLM-Gateway-Admins"]` |
| Azure AD | `groups` (Object ID) 或自定义 `roles` | string[] | `claim_path: "roles", admin_match_values: ["Admin"]` |
| 自建 IdP | 自定义 claim（如 `role`） | string | `claim_path: "role", admin_match_values: ["admin"]` |

**V0.5 演进路径**：

当 V0.5 引入多角色 RBAC 时，配置结构自然扩展为：

```yaml
auth:
  oidc:
    role_mapping:
      claim_path: "groups"
      rules:                          # 从单条 admin 规则扩展为多规则
        - match_values: ["llm-gateway-admins"]
          role: "admin"
        - match_values: ["llm-gateway-ops"]
          role: "operator"
      default_role: "user"
```

OIDCService 的 `determineRole` 函数只需从单条规则改为遍历 rules 数组，改动量极小。

---

## 8. 最终结论（必填）

**选择方案**：A — 配置映射（YAML/Viper）

**核心理由**：
- SSO claims 结构变化时仅需改配置（claim_path / admin_match_values），无需改代码重新部署——可维护性最优
- V0.5 引入 RBAC 时配置结构自然扩展（从单条规则到多规则数组），演进兼容性好
- 实现量约 20 行代码，Viper 已在技术栈中，无新依赖

**被否决方案的关键原因**：

| 方案 | 否决原因 |
| :--- | :--- |
| B：硬编码读取 | SSO claims 变化必须改代码重部署，可维护性极差；V0.5 扩展需重写判定逻辑 |
| C：数据库覆盖表 | V0.2 引入数据库角色表超出阶段目标（宪章"渐进演进"原则）；管理员人数 < 10，配置文件足够；数据库方案需新增表/Repository/端点，实现量过大 |

---

## 9. 遗留风险与后续行动（必填）

**已知风险**：

| 飱险 | 可能性 | 影响 | 缓解措施 |
| :--- | :---: | :---: | :--- |
| 公司 SSO 不提供任何角色/组 claim（仅 sub/name/email） | 中 | 中 | 配置中 `claim_path` 缺失时 fallback 到 `default_role: "user"`；管理员通过 ops-console 手动在 users 表中修改 role 字段（V0.2 简单覆盖方案） |
| 配置映射规则被错误修改导致非预期用户获得 admin 角色 | 低 | 高 | 配置通过环境变量/Secret 注入（不进入代码库）；admin 规则变更需审批流程；users 表 role 字段有审计日志 |
| SSO claim 值格式与配置不匹配（如 groups 是 Object ID 而非组名） | 中 | 中 | R-01 联调时与 IT 部门确认 claim 结构和值格式；配置中可使用 Object ID 作为 admin_match_values |

**后续行动**：

- [ ] 在 R-01 SSO 联调时明确确认公司 SSO id_token 中的角色/组 claim 名称和值格式
- [ ] 在 auth HLD §5.4 授权模型中补充角色映射配置结构（claim_path + admin_match_values + default_role）
- [ ] 在 auth HLD §3.2 OIDC 登录流程中补充 `determineRole()` 步骤：OIDCService 解析 claims 后调用 determineRole 配置映射判定角色
- [ ] 在 auth 详细设计中实现 OIDCService.determineRole() 配置映射逻辑
- [ ] 在 auth 配置结构（Viper）中新增 `oidc.role_mapping` 配置块