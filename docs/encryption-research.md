# 密钥加密方案调研报告

## 一、当前现状

项目使用 **AES-256-GCM** 加密 Provider 凭证，密钥通过环境变量 `ENCRYPTION_KEY`（32 字节）传入。

**现有问题：**
1. 密钥硬编码在环境变量中，无法轮换
2. 所有凭证用同一把密钥加密，密钥泄露 = 全部泄露
3. 无密钥版本管理，更换密钥需要重新加密所有数据
4. 无审计记录（谁访问了密钥）

---

## 二、常见加密方案对比

### 方案 A：应用层 AES-256-GCM + 数据库密钥管理表

**原理：** 在数据库中创建 `encryption_keys` 配置表，存储加密后的 DEK（数据加密密钥），应用启动时用 KEK（密钥加密密钥，来自环境变量或文件）解密 DEK。

**架构：**
```
KEK (环境变量/文件) → 解密 → DEK (存储在 DB) → 加密/解密凭证
```

**优点：**
- 实现简单，无外部依赖
- 支持密钥轮换（新增 DEK 版本，旧数据逐步迁移）
- 密文中携带 key_version，解密时自动选择对应密钥
- 适合中小规模部署

**缺点：**
- KEK 仍需外部保护（环境变量/文件/K8s Secret）
- 密钥审计需要自建
- 不满足某些合规要求（如 FIPS 140-2 Level 3）

**适用场景：** 中小企业内部平台、私有化部署

---

### 方案 B：信封加密（Envelope Encryption）+ 外部 KMS

**原理：** 每条凭证用唯一的 DEK 加密，DEK 由外部 KMS 加密后存储。解密时先调 KMS 解密 DEK，再用 DEK 解密凭证。

**架构：**
```
KMS (AWS/GCP/Azure) → 生成+加密 DEK → 存 DB
读取时: KMS.Decrypt(encrypted_DEK) → DEK → 解密凭证
```

**优点：**
- 密钥从不离开 KMS 硬件边界（HSM 级别保护）
- 完整审计日志（CloudTrail/Stackdriver）
- 自动密钥轮换
- 满足 SOC2/ISO27001/FIPS 合规

**缺点：**
- 依赖云厂商，增加网络调用延迟（~10-50ms/次）
- 本地开发需要 mock 或降级方案
- 成本（KMS API 调用费）
- 增加系统复杂度

**适用场景：** 企业级 SaaS、强合规需求、云原生部署

---

### 方案 C：HashiCorp Vault Transit Engine

**原理：** Vault 提供 "Encryption as a Service"，应用把明文发给 Vault，Vault 返回密文。密钥完全由 Vault 管理。

**架构：**
```
App → POST /transit/encrypt → Vault → 返回密文
App → POST /transit/decrypt → Vault → 返回明文
```

**优点：**
- 密钥从不暴露给应用
- 完整的密钥生命周期管理（创建/轮换/禁用/销毁）
- 多租户密钥隔离
- 完善的审计日志
- 支持动态凭证（Database Secret Engine）

**缺点：**
- 需要运维 Vault 集群（单点故障风险）
- 系统依赖增加
- 开发环境搭建成本高
- 每次加解密都是网络调用

**适用场景：** 大型企业、多团队共享平台、强安全需求

---

### 方案 D：PostgreSQL pgcrypto + 列级加密

**原理：** 直接在数据库层面用 `pgp_sym_encrypt()` 加密列数据。

**优点：** 零应用代码改动
**缺点：** 密钥在 SQL 中明文传递、性能差、无审计、不推荐

---

## 三、方案对比矩阵

| 维度 | 方案 A (DB管理DEK) | 方案 B (云KMS) | 方案 C (Vault) |
|------|-------------------|---------------|----------------|
| 实现复杂度 | ★★☆☆☆ | ★★★☆☆ | ★★★★☆ |
| 安全等级 | 中 | 高 | 高 |
| 密钥轮换 | ✅ 手动/定时 | ✅ 自动 | ✅ 自动 |
| 审计日志 | ❌ 需自建 | ✅ 原生 | ✅ 原生 |
| 外部依赖 | 无 | 云厂商 | Vault 服务 |
| 私有化部署 | ✅ 完美 | ❌ 需要云 | ✅ 可私有化 |
| 性能影响 | 无（内存缓存DEK） | 微（缓存+批量） | 微（缓存） |
| 合规 | 一般 | SOC2/FIPS | SOC2/FIPS |
| 开发成本 | 1-2 天 | 3-5 天 | 5-7 天 |
| 本项目适配度 | ★★★★★ | ★★★☆☆ | ★★☆☆☆ |

---

## 四、推荐方案

### 推荐：方案 A（应用层 AES-256-GCM + 数据库密钥版本管理）

**理由：**
1. 项目当前阶段是**私有化部署的内部平台**，不需要云 KMS
2. 已有 AES-256-GCM 实现，改造成本最低
3. 通过 `encryption_keys` 表实现密钥版本管理，支持轮换
4. 密文前缀携带版本号（如 `v1:base64...`），解密自动选择对应密钥
5. 未来如需升级到 KMS/Vault，接口抽象层可平滑切换

### 设计方案

#### 数据库表：`system_configs`（通用配置表）

```sql
CREATE TABLE system_configs (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    encrypted BOOLEAN NOT NULL DEFAULT false,
    description VARCHAR(500) DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
```

#### 数据库表：`encryption_keys`（密钥版本管理）

```sql
CREATE TABLE encryption_keys (
    version INT PRIMARY KEY,
    encrypted_key TEXT NOT NULL,      -- KEK 加密后的 DEK
    algorithm VARCHAR(50) NOT NULL DEFAULT 'AES-256-GCM',
    status VARCHAR(20) NOT NULL DEFAULT 'active',  -- active/rotating/retired
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    retired_at TIMESTAMP WITH TIME ZONE
);
```

#### 加密流程

```
1. 启动时：读取 KEK（环境变量） → 解密 encryption_keys 表中 active 的 DEK → 缓存到内存
2. 加密时：用 active DEK 加密 → 密文格式: "v{version}:{base64_ciphertext}"
3. 解密时：从密文提取 version → 查找对应 DEK → 解密
4. 轮换时：生成新 DEK → 加密存入 DB → 标记为 active → 旧 key 标记 rotating
```

#### 前端回显

- 凭证字段显示脱敏值：`sk-****6789`
- 编辑时显示 placeholder "留空不修改"
- 新增"重新生成密钥"管理入口（仅超管可见）

---

## 五、实施计划

1. 创建 `system_configs` + `encryption_keys` 迁移
2. 重构 `crypto` 包：支持多版本 DEK、密文带版本前缀
3. 启动时初始化：检查 encryption_keys 表，无记录则自动生成首个 DEK
4. 前端凭证回显脱敏处理
5. 编写 TDD 测试覆盖加解密、轮换、降级等场景
