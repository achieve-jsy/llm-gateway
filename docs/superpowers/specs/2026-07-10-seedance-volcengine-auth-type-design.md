# Seedance 官方渠道鉴权方式可选

## 目标

`seedance-volcengine` 渠道支持用户显式选择鉴权方式，与 Bedrock 渠道交互对齐：

- **方舟 API Key**（默认）：`Authorization: Bearer <api_key>`
- **AK/SK**：HMAC-SHA256 签名

选中后只展示对应字段；后端严格按所选方式鉴权，**不做静默回退**。

## 凭证 JSON

```json
{"auth_type":"api_key","api_key":"..."}
```

```json
{"auth_type":"aksk","access_key":"...","secret_key":"...","region":"cn-beijing"}
```

## 兼容旧数据

无 `auth_type` 时推断：

1. 有 `api_key`（或裸字符串）→ `api_key`
2. 否则有 `access_key` + `secret_key` → `aksk`
3. 推断后只保留对应字段参与鉴权

## 影响面

- 前端 `ProviderDialog`：专属单选 UI
- 共享解析：`internal/providers/seedance/volcengine` Creds
- Gateway adapter 构建、CP 探针 / 健康检查 / 模型检测 / 测试连接
- Provider definition 文案（可选迁移）

## 非目标

- 不拆成两个 provider type
- 不扩展通用 credential_schema 条件渲染框架
