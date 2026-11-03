# PROVENANCE —— privacy 引擎来源与许可

| 项 | 值 |
|---|---|
| 上游 | [yetone/magpie](https://github.com/yetone/magpie)，MIT |
| 上游基线 | `3b2794cadd9e316fb5b9d30266f6ef3358d98d40`（2026-10-28） |
| 直接来源 | [c4bbage/leaker](https://github.com/c4bbage/leaker) 的 `leakscan/privacy`（MIT），提交 `049be0d` |
| 落地路径 | `internal/gateway/privacy`，相对直接来源**仅改动了模块导入路径** |

## 直接来源相对上游的增强（逐文件 diff 结论）

- 全局状态收进 `Engine` 实例（新增 `engine.go`），一个会话一份「占位符 ↔ 明文」映射，`Close()` 显式清空 —— 多租户必需。
- 新增 `Engine.Detect`：只报偏移、不落映射，支持「只告警不脱敏」。
- 新增 `Options.Extra` 钩子，接入 `credentials/` 的国内云与运维凭证规则（阿里云/腾讯云/GitLab/飞书/钉钉/K8s/数据库连接串等）。
- `privacy.go` 策略层：`RulesOnly`、fail-closed（`ErrModel`）、`mask/warn/block` 三档动作、`MaxInputBytes`。
- 移除了上游的网关应用层（设置页、账号切换、media/search 脱敏、archive/capture）。

## 本地接入

网关侧只应通过 `internal/gateway/redact` 使用本包，见 `docs/redaction.md`。

## 运行期依赖提醒

`internal/guard` 的语义模型需要运行期下载 ONNX/CoreML 产物到 `CacheDir`。
默认配置 `RulesOnly: true`，不触发任何下载，也不产生外部请求。
