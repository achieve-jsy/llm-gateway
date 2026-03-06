# llm-gateway 错误码体系分析报告

> 评审范围: 全仓 `internal/` + `web/src/services/http.ts` + `tests/contract/contracts.json`
> 评审方式: rg/grep 全量扫描 + 关键文件逐行阅读 + 业内项目(LiteLLM/OpenAI/Anthropic/One API/New API)实测对比
> 评审日期: 2026-07-02
> 分支: feat/layered-quota
> 约束: 纯分析,不改生产代码;证据均为 file:line 实测

---

## 0. TL;DR

llm-gateway 的错误码体系目前是**多套并行、各自为政**的状态:网关热路径、控制面、认证、配额、组织服务、可观测性各自定义错误码,命名风格混杂(snake_case / `E<HTTP><seq>` / 中文 / 裸字符串字面量),响应体信封形状至少有 6 种,且存在 `internal` vs `internal_error` 同语义不同码、`ErrGroupNotFound` 两处重复定义等冲突。**热路径(/v1/chat, /v1/messages)的码设计本身接近 Anthropic/OpenAI 规范**,是全仓做得最好的部分;**控制面有一套还算完整的 `Code*` 常量块**,但被多处字面量绕过;**配额、认证、组织服务的码则是各自硬编码,无统一注册表**。

核心问题(详见 §3):
- **P0 × 3**: "internal" 与 `internal_error` 同义冲突;Anthropic 路径错误响应**丢失 `code` 字段**(机器码缺失,前端无法分流);上游错误归一化靠字符串匹配(`strings.Contains(msg,"404")`),脆弱且会误判。
- **P1 × 6**: `request_id` 不进响应体(违背 Anthropic 规范);6 种信封形状并存;`ErrGroupNotFound` 两处重复定义;`ErrModelGroupUnavailable` 用 400 描述授权问题(status 语义错配);流式中途错误 zero-copy 透传未归一化;前端 `ERROR_MESSAGES` 仅覆盖控制面 13 个码,热路径码零覆盖。
- **P2 × 4**: `E4015` 编码与 snake_case 并存;`errorCodes` 内部 map(`QUOTA_RPM_REJECTED`)与对外 `errorCode()`(`quota_rpm_exceeded`)分裂;契约测试不校验错误码;无错误码字典文档。

评分摘要(详见 §4):9 项中 **0 项好,4 项一般,5 项差**。总体处于"能跑但不可运维"阶段:码够用,但出问题时无法从码快速定位是哪一层。

---

## 1. 现状全量梳理

### 1.1 错误码定义机制分布(共 10 套并行机制)

| # | 机制 | 定义位置 | 覆盖面 |
|---|------|---------|--------|
| 1 | `model.UnifiedError` 结构体 + Err\* 哨兵 | `internal/gateway/model/error.go:8-95` | 网关热路径(/chat /embeddings /messages) |
| 2 | `core.GatewayError` 结构体(OpenAI 兼容信封) | `internal/core/errors.go:13-237` | provider 上游错误解析、failover 判定 |
| 3 | `errors.AuthError` + `E<HTTP><seq>` 数字码 | `internal/auth/errors/errors.go:4-17` | 认证子服务内部 |
| 4 | controlplane `Code*` 字符串常量块 | `internal/controlplane/handler/errcodes.go:6-62` | 控制面 handler(241 处引用) |
| 5 | 配额 `errorCodes` map + `errorCode()`/`errorType()` helper | `internal/gateway/quota/reject.go:54-71` | 配额拒绝 429 |
| 6 | orgservice `errorResp(errType,msg,code)` helper + `org.*` 命名空间 | `internal/orgservice/handler/org_handler.go:189` | 组织服务 handler |
| 7 | auth/handler、orgservice/handler 内联裸 `"code":` 字面量 | `internal/auth/handler/auth.go`,`token.go`,`internal.go`;`internal/orgservice/handler/*` | 登录回调/token/角色/映射 |
| 8 | 前端 `ERROR_MESSAGES` 机器码→中文 map | `web/src/services/http.ts:7-22` | 控制面前端(仅 13 个码) |
| 9 | 散落哨兵 `errors.New`/`fmt.Errorf`(中英混合) | `internal/orgservice/service/*`,`internal/controlplane/service/*`,`internal/gateway/scheduler/errors.go`,`internal/gateway/model/group_errors.go`,`internal/providers/router.go` | service 层内部(不直接对外) |
| 10 | `usage_collector.go` `te.ErrorCode` 字段赋值 | `internal/gateway/middleware/usage_collector.go:205,209` | 遥测/计费 |

> 机制 1 与机制 2 是**两个独立的 OpenAI 兼容错误模型**并存(见 §1.4),但只有机制 1 真正写出外部响应体;机制 2 仅用于内部 failover/429 判定,其 `ToJSON()` 唯一对外触达点是预算超限路径(经 `BudgetExceededError`,见下)。

### 1.2 对外错误码全量表(机器码维度)

#### A. 网关热路径 — `UnifiedError`(`internal/gateway/model/error.go`)

| code | type | HTTP | message | 定义处 | 用法点 |
|------|------|------|---------|--------|--------|
| `invalid_request_body` | `invalid_request` | 400 | request body is invalid | `error.go:32` | errormap `/messages.go` `/chat.go` `/embeddings.go` |
| `model_required` | `invalid_request` | 400 | model field is required | `error.go:36` | messages.go |
| `messages_required` | `invalid_request` | 400 | messages field is required... | `error.go:40` | messages.go |
| `unknown_model` | `model_not_found` | 404 | the requested model is not registered | `error.go:44` | chat.go group dispatch |
| `model_unavailable` | `model_unavailable` | 503 | the requested model is temporarily unavailable | `error.go:48` | chat.go failover |
| `provider_timeout` | `temporary_failure` | 504 | upstream provider did not respond in time | `error.go:53` | errormap |
| `provider_error` | `temporary_failure` | 502 | upstream provider returned an error | `error.go:57` | errormap 默认 |
| `invalid_api_key` | `authentication_error` | 401 | invalid or missing API key | `error.go:61` | auth middleware |
| `key_disabled` | `authentication_error` | 401 | the API key has been disabled | `error.go:65` | auth middleware |
| `auth_service_unavailable` | `service_unavailable`(*内联类型,非常量*) | 503 | auth service temporarily unavailable | `error.go:69` | auth middleware |
| `internal` | `internal_error` | 500 | internal server error | `error.go:73` | 兜底 |
| `model_unavailable`(*复用同名 code / 不同的 type+status*) | `model_unavailable` | **400** | 模型组不可用,请检查你的 API Key 是否有权使用该模型组(*中文*) | `error.go:76`(ErrModelGroupUnavailable) | authz/groups.go |
| `group_not_authorized` | `group_not_authorized`(*内联类型*) | 403 | your department does not have access to this model group | `error.go:82` | authz/groups.go |
| `request_canceled` | `temporary_failure` | 499 | request was canceled by client | `errormap/mapper.go:36` | errormap(动态构造) |
| `group_not_found` | `model_not_found` | 404 | (动态) | `chat.go:378` | chat.go respondGroupError(覆盖 ErrGroupNotFound) |

**问题点**:
- `ErrModelGroupUnavailable` 与 `ErrModelUnavailable` 共用 code=`model_unavailable`,但前者 HTTP=400+中文消息、后者 HTTP=503+英文消息。**同 code 不同语义/不同 status**,客户端无法区分(error.go:48 vs :76)。
- `ErrModelGroupUnavailable` 注释自陈"503/400 服务问题"(error.go:80),但实际是授权侧问题(由 `authz/groups.go` 在 binding 校验失败时返回),用 400 描述授权缺失语义错配(应 403)。
- `service_unavailable`、`group_not_authorized` 两个 type 是**内联字符串字面量**,未纳入 `ErrorType` 常量块(error.go:9-15),破坏枚举一致性。
- `ErrModelGroupUnavailable.Message` 是**中文**,与其余 UnifiedError 全英文不一致(error.go:77)。

#### B. 控制面 — `Code*` 常量块(`internal/controlplane/handler/errcodes.go`)

共 32 个常量,按 errcodes.go 注释分档:

| 档 | code 值 | HTTP | type(字面量) |
|----|---------|------|---------------|
| 400 invalid_request | `bad_request` `invalid_tag_id` `invalid_model_id` `model_provider_invalid` `sync_not_supported` `provider_disabled` `key_limit_exceeded` `invalid_strategy` `catalog_not_bound` `already_bound_to_same` `group_already_bound` `group_not_bound` | 400/422 | `invalid_request` |
| 404 not_found | `provider_not_found` `model_not_found` `catalog_not_found` `key_not_found` `group_not_found` `announcement_not_found` `entitlement_not_found` | 404 | `not_found` |
| 409 conflict | `provider_name_conflict` `model_name_conflict` `provider_has_models` `sync_in_progress` `key_name_conflict` `group_name_conflict` `entitlement_duplicate` | 409 | `conflict` |
| 422 unprocessable | `strategy_switch_warning` `group_not_controlled` | 422 | (handler 内联) |
| 403 forbidden | `key_permission_denied` `entitlement_forbidden` | 403 | `forbidden` |
| 502 upstream | `upstream_error` | 502 | `upstream_error` |
| 500 internal | `internal_error` | 500 | `internal_error` |

**问题点**:
- `type` 字符串(`invalid_request`/`not_found`/`conflict`/`forbidden`/`upstream_error`/`internal_error`)**散落在每个 handler 的 gin.H 字面量里**,未与 `Code*` 常量绑定(errcodes.go 只定义 code 不定义 type)。同一码在不同 handler 可能配不同 type。
- 字面量绕过:`provider_handler.go:66` 用 `"code": "internal"`(短码),与常量 `CodeInternalError="internal_error"` 不一致;`model_tag_handler.go:66,100,122` 用 `name_conflict`/`not_found` 字面量,不在 errcodes.go 表中;`key_handler.go:184` 用 `"code": "unauthorized"` 字面量。

#### C. 认证子服务 — `AuthError` `E<HTTP><seq>`(`internal/auth/errors/errors.go`)

| code | HTTP | message |
|------|------|---------|
| `E4015` | 401 | key not found |
| `E4016` | 401 | key disabled |
| `E4017` | 401 | invalid key format |
| `E4043` | 404 | user not found |
| `E5031` | 503 | upstream unavailable |

**问题点**: 采用 `E<HTTP><seq>` 数字编码,与全仓其余 snake_case 风格完全异质。且这些 E 码**仅存在于 auth 子服务内部**,实际对外响应由 auth/handler 的内联字面量(`invalid_request`/`service_unavailable`/`callback_failed`/`forbidden`/`internal`/`internal_error` 等)另行输出,两套码互不引用。

#### D. 配额拒绝(`internal/gateway/quota/reject.go`)

| 对外 code | 对外 type | HTTP | 内部日志码(map) | message |
|-----------|-----------|------|------------------|---------|
| `quota_rpm_exceeded` | `rate_limit_error` | 429 | `QUOTA_RPM_REJECTED` | 速率超限:<层> <门> 已达上限(N) |
| `quota_concurrency_exceeded` | `concurrency_limit` | 429 | `QUOTA_CONCURRENCY_REJECTED` | 同上 |

**问题点**: 内部日志码(`QUOTA_RPM_REJECTED`,SCREAMING_SNAKE)与对外 HTTP 码(`quota_rpm_exceeded`,snake_case)**双轨分裂**,无映射文档。type `concurrency_limit` 与全仓其余 type 命名(均以 `_error` 结尾)不一致。

#### E. 预算超限(`internal/gateway/middleware/budget_reserve.go:36-44`)

| code | type | HTTP | 额外字段 |
|------|------|------|----------|
| `budget_exceeded` | `budget_exceeded` | 402 | `param`=label, `gate`="budget" |

402 Payment Required 是全仓唯一使用,Anthropic 也用 402 billing_error,方向正确。

#### F. 组织检查中间件(`internal/gateway/middleware/org_check.go`)

| code | type | HTTP |
|------|------|------|
| `budget_not_configured` | `organization_error` | 403 |
| `org_pending_studio` | `organization_error` | 403 |
| `passthrough_forbidden` | `authorization_error` | 403 |

**问题点**: 又一种 type 命名(`organization_error`/`authorization_error`,与热路径 `authentication_error`/`group_not_authorized` 并存);`budget_not_configured` 用 `organization_error` type 但 code 名带 budget,混淆维度。

#### G. orgservice handler(`errorResp` 调用点 + 内联)

出现的 code 值(摘):`bad_request` `internal` `org.duplicate_name` `org.studio.not_found` `org.studio.has_departments` `not_found` `forbidden` `org.forbidden` `org.redundant_role` `org.import.file_too_large` `org.import.too_many_rows` `org.import.invalid_studio` `rbac.unknown_perm`(rbac/checker.go:83)。采用**点号分层**命名(`org.*` / `rbac.*`),与控制面 snake_case 又不同。

#### H. auth/handler 内联字面量(`internal/auth/handler/auth.go`,`token.go`,`internal.go`)

code 值:`service_unavailable` `invalid_request` `invalid_state` `callback_failed` `invalid_claims` `persist_failed` `token_sign_failed` `forbidden` `internal` `internal_error` `unauthorized`。
**问题点**: `internal`(auth.go:121,126, role_handler.go, org_handler.go 多处)vs `internal_error`(token.go:58, errcodes.go CodeInternalError)**同语义双码并存**(P0)。

#### I. usage_collector 遥测(`internal/gateway/middleware/usage_collector.go:205,209`)

`te.ErrorCode` 被赋值为 `upstream_error` 或 `http_error`,仅入遥测,不对外。注意 `http_error` 这个码全仓仅此一处,与 errcodes.go 的 `upstream_error` 并列。

### 1.3 对外响应信封形状(共 6 种)

| 路径 | 信封形状 | 定义处 |
|------|----------|--------|
| /v1/chat /v1/embeddings(OpenAI 兼容) | `{"error":{"type","code","message"}}` | `chat.go:400-410` respondError |
| /v1/messages(Anthropic 兼容) | `{"type":"error","error":{"type","message"}}`(**无 code**) | `messages.go:361-371` respondAnthropicError |
| 控制面 handler | `{"error":{"type","message","code"}}` | 各 handler gin.H |
| 配额拒绝 | `{"error":{"type","code","message","param","gate","layer","subject_type","limit","retry_after_seconds"}}` | `reject.go:39-51` |
| orgservice errorResp | `{"code","message","error":{"type","message","code"}}`(**顶层 code 冗余**) | `org_handler.go:189` |
| ops/observability 裸字符串 | `{"error":"failed to query ..."}`(**无信封**) | `ops_handler.go:189,224,403` 等 |

**Anthropic 路径无 `code` 字段是真规范兼容(messages.go:362-368 与 Anthropic 官方 `{type:"error",error:{type,message},request_id}` 形状一致),但代价是**:机器码丢失,经 Anthropic 端点报错的客户端拿不到 `invalid_api_key`/`model_unavailable` 等码用于分流**(P0)。Anthropic 官方虽不带 code,但行业实践(LiteLLM proxy)会在 Anthropic 形状里补 code 字段以利运维。

### 1.4 两个 OpenAI 兼容错误模型并存(`core.GatewayError` vs `model.UnifiedError`)

- `core.GatewayError`(`internal/core/errors.go`):OpenAI 兼容,带 `Param`/`Code` 指针、`Provider` 字段、`ParseProviderError` 上游解析、`WithCode` 链式。**设计完整度高**,含 type→status switch(errors.go:67-86),含上游 error.code 透传(errors.go:233)。
- `model.UnifiedError`(`internal/gateway/model/error.go`):更轻,无 `Param`/`Provider`,无上游码透传。

但**实际写出外部 OpenAI 兼容响应体的只有 `UnifiedError`**(`chat.go respondError`)。`core.GatewayError` 在 chat.go 仅用于 `errors.As` 判定 failover/429(chat.go:342,359),其 `ToJSON()` 唯一对外触达是 `BudgetExceededError`(经转换)。** feud:**一个"更完整"的错误模型被闲置,实际的对外模型反而缺字段(无 param/provider/上游码透传)。

### 1.5 上游错误归一化策略(`errormap/mapper.go`)

`MapProviderError` 用 `strings.Contains(strings.ToLower(err.Error()), ...)` 匹配关键字(`"404"`/`"timeout"`/`"unauthorized"`/`"invalid"`/`"bad request"`/`"not found"`/`"unavailable"`/`"503"`)归类,默认 502(mapper.go:24-61)。

**问题点**:
- 纯字符串匹配:上游消息中任何含 "timeout" 的正常文本都会被误归为 provider_timeout;含 "404" 的 URL 片段会误判 not_found。
- **不消费 HTTP status code**:对端返回 429 时,因消息可能不含 "429"/"rate" 关键字,会被默认归为 502 provider_error,**丢失 429 语义**(P0,与 §4 覆盖完整性挂钩)。
- 与 `core.ParseProviderError`(errors.go:178-237)**职责重叠**:后者按 status code 分流且会透传上游 code,但热路径用的是前者(mapper.go),把后者闲置。

---

## 2. 业内项目对比

| 维度 | llm-gateway(本仓) | LiteLLM | OpenAI API | Anthropic API | One API(songquanpeng) | New API(QuantumNous) |
|------|-------------------|---------|------------|---------------|----------------------|----------------------|
| **码结构** | snake_case + `E<HTTP><seq>` + `org.*` 点号 + 中文,4 套并存 | 异常类继承 OpenAI SDK,type 隐含在类名 | `error.type` + `error.code`(字符串)+`error.param` | `error.type` + `request_id`(无 code 字段) | `Error{Message,Type,Param,Code any}`,无集中码表 | 集中 `ErrorCode` 字符串枚举 + `ErrorType` 枚举 + `NewAPIError` 包装 |
| **分类分层** | 无统一 type 体系;type 散落字面量 | 按 HTTP 异常类(400/401/403/404/408/422/429/500/503) | type 与 HTTP 一一对应(invalid_request_error/authentication_error/...) | type 与 HTTP 一一对应(invalid_request_error/authentication_error/permission_error/not_found_error/rate_limit_error/api_error/timeout_error/overloaded_error/billing_error/request_too_large) | type 仅 `upstream_error`/`bad_response`,极简 | 双层:ErrorType(`new_api_error`/`openai_error`/`claude_error`/`upstream_error`/...) + ErrorCode(`invalid_request`/`channel:*`/`insufficient_user_quota`/...) |
| **透传策略** | 字符串匹配归一化,丢弃上游码(Anthropic 路径丢 code) | 透传 OpenAI/Azure 原异常 + `llm_provider` 属性;其他 provider 映射到最近 OpenAI 异常类,原细节入 `provider_specific_fields` | 不透传(自身即源头) | 不透传(自身即源头),带 request_id | 透传上游 OpenAI 格式 error;非 OpenAI 用 `GeneralErrorResponse.ToMessage()` 取 message,统一标 `upstream_error`/`bad_response_status_code` | `RelayErrorHandler` 优先 TryToOpenAIError 透传上游结构,否则 `WithOpenAIError` 包一层,带 ErrorCode |
| **可观测性(从码定位)** | 差:同码异义(`internal`/`model_unavailable`)、Anthropic 路径无码 | 好:异常类即定位 | 好:type+code 双维度 | 好:type+request_id | 中:只知 upstream,不知具体 | 好:ErrorCode 命名空间(`channel:*`)/ErrorType 双维度 |
| **429 处理** | 配额 429 有 `quota_rpm_exceeded`/`quota_concurrency_exceeded` + Retry-After;但上游 429 经 errormap 可能丢成 502 | `RateLimitError`(429),`_should_retry` | `RateLimitError`,区分 rate limit vs quota exceeded | `rate_limit_error`(429),区分 acceleration limit | 透传上游 429 | 透传 + `insufficient_user_quota` 等本地码 |
| **流式中途错误** | zero-copy 透传上游字节,不归一化 | 异常类化,可捕获 | 标准 SSE error 事件 | `event: error` + `{type:"error",error:{...}}` | 透传 | 透传 |
| **request_id 暴露** | 仅日志/响应头,不入错误体 | 有 | 无 | **响应体 `request_id`** | 无 | 无 |

来源:
- LiteLLM: https://docs.litellm.ai/docs/exception_mapping
- OpenAI: https://developers.openai.com/api/docs/guides/error-codes
- Anthropic: https://platform.claude.com/docs/en/api/errors
- One API: https://github.com/songquanpeng/one-api (`relay/controller/error.go`, `relay/model/misc.go`)
- New API: https://github.com/Calcium-Ion/new-api (`types/error.go`, `service/error.go`)

**对比结论**:
1. llm-gateway 的**热路径 UnifiedError + Anthropic 形状**已与 OpenAI/Anthropic 规范对齐,这部分**优于 One API**(One API 仅 `upstream_error`/`bad_response` 两个 type,粒度粗)。
2. 但 llm-gateway **缺 New API 那样的集中 `ErrorCode` 枚举 + 双层 type/code 体系**;New API 的 `channel:*`/`invalid_request`/`insufficient_user_quota` 分层正是本仓控制面 `Code*` 块 + 热路径码"应当合并成"的形态。
3. llm-gateway 的上游归一化(string match)**落后于** LiteLLM(异常类映射 + provider_specific_fields)和 One/New API(按 HTTP status code 分流 + 透传上游结构)。
4. llm-gateway 不暴露 `request_id` 于错误体,**落后于** Anthropic 规范。
5. llm-gateway 的 6 种信封形状是**业内最分散的**(对比项中最多的;One API 全仓统一一种 OpenAI 形状,New API 统一经 NewAPIError 转换)。

---

## 3. 问题清单(按严重度)

### P0(阻断运维/客户端正确分流)

**P0-1. `internal` 与 `internal_error` 同语义双码并存**
- 证据:`internal/auth/handler/auth.go:121,126`(`"code":"internal"`)、`internal/orgservice/handler/org_handler.go:34,45,72,...`(`errorResp(...,"internal")`,≥8 处)、`internal/orgservice/handler/role_handler.go:76,111,122,137`;对照 `internal/auth/handler/token.go:58`(`"code":"internal_error"`)、`internal/controlplane/handler/errcodes.go:61`(`CodeInternalError="internal_error"`)、`web/src/services/http.ts:21`(`internal_error:'服务器开小差了'`)。
- 影响:前端 `ERROR_MESSAGES` 只映射 `internal_error`,所有 `internal` 码走 fallback null → 用户只看到通用提示,无法区分"内部错误"与"未知错误"。运维按码过滤日志时,`internal` 与 `internal_error` 两路需分别查,易漏。

**P0-2. Anthropic 路径(/v1/messages)错误响应丢失 `code` 字段**
- 证据:`internal/gateway/handler/messages.go:361-371` `respondAnthropicError` 仅写 `{type:"error",error:{type,message}}`,`mapAnthropicErrorType`(messages.go:406-423)只映射 type,丢弃 UnifiedError.Code。
- 影响:虽符合 Anthropic 官方形状(官方无 code),但实际下游客户端(经本网关走 Anthropic 端点)无法用机器码做认证失败/超时/不可用的自动分流与重试决策,只能 string-match message。LiteLLM proxy 在 Anthropic 形状里补 code 字段正是为此。

**P0-3. 上游错误归一化靠字符串匹配,会误判并丢失 429**
- 证据:`internal/gateway/errormap/mapper.go:24-61` 用 `strings.Contains(msg,"404"/"timeout"/"unauthorized"/...)`,默认 502;无 status code 参数。上游返回 429 但消息无 "429"/"rate" 关键字时 → 默认 `provider_error`(502)。
- 影响:429 是限流信号,误判成 502 会触发 failover(本不该)而非退避;日志/指标中 429 被记成 502,失真。同仓已有 `core.ParseProviderError`(errors.go:178)按 status code 分流且透传上游码,却未被热路径采用。

### P1(语义错配/重复/覆盖缺口)

**P1-1. `request_id` 不进错误响应体**
- 证据:`internal/gateway/handler/chat.go:200-317`、`messages.go:200-305` 中 `request_id` 仅作 slog 字段;`respondError`(chat.go:400-410)与 `respondAnthropicError`(messages.go:361-371)响应体均不含 request_id。
- 影响:用户报错时无法提供 request_id 供后端定位(Anthropic 规范要求错误体带 `request_id`)。响应头虽有,但客户端 SDK 通常不暴露头。

**P1-2. 6 种信封形状并存**
- 证据:见 §1.3 表(openai 形 / anthropic 形 / 控制面形 / 配额形 / orgservice 形 / ops 裸字符串)。
- 影响:前端需为每条路径写不同解析;`web/src/services/http.ts:29` `resolveErrorMessage` 只读 `error.response.data.error.code`,对 ops 裸字符串形(`{error:"string"}`)直接拿不到 code,对 orgservice 顶层 code 也漏读。

**P1-3. `ErrGroupNotFound` 两处重复定义**
- 证据:`internal/gateway/model/group_errors.go:6-8`(ErrGroupNotFound/ErrNoAvailableModel/ErrNoOtherModelAvailable)与 `internal/gateway/scheduler/errors.go:6-8`(同名同消息)完全重复。chat.go:375 用 `model.ErrGroupNotFound`,scheduler 用自己的。
- 影响:两包各持一份,scheduler 返回的 ErrGroupNotFound 经 `respondGroupError`(chat.go:374)需特判 `err == model.ErrGroupNotFound` 才能映射——若 scheduler 误返 scheduler.ErrGroupNotFound 则特判失败,兜底成 ErrInternal。

**P1-4. `ErrModelGroupUnavailable` HTTP/status 语义错配**
- 证据:`internal/gateway/model/error.go:76-78` ErrModelGroupUnavailable HTTPStatus=400 + 中文消息"模型组不可用,请检查你的 API Key 是否有权使用该模型组",type=`model_unavailable`;但调用方 `internal/gateway/authz/groups.go:78,93,105,108` 是授权/binding 校验场景。
- 影响:授权问题用 400 而非 403,且消息暗示"检查 API Key 权限"却返回 400 invalid_request,客户端 retry 逻辑误判为可重试的输入错误。注释(error.go:80)自承"503/400 服务问题",与授权语义矛盾。

**P1-5. 流式中途错误 zero-copy 透传未归一化**
- 证据:`internal/gateway/handler/chat_stream.go:12-41` 直接 `io` 拷贝上游 SSE 字节,中途上游错误原样透传,无 type/code 归一化、无 request_id 注入。
- 影响:客户端收到异构上游错误体,与本网关 UnifiedError 形状不一致,无法统一处理;失联的 200+deltachunk 后错误无法被前端识别为"本网关已受理但上游失败"。

**P1-6. 前端 `ERROR_MESSAGES` 仅覆盖控制面 13 个码,热路径码零覆盖**
- 证据:`web/src/services/http.ts:7-22` 13 个键全为控制面 Code\* 值;`invalid_api_key`/`model_unavailable`/`provider_timeout`/`quota_rpm_exceeded`/`budget_exceeded`/`group_not_authorized` 等热路径码均缺;fallback 为 null(http.ts:29)。
- 影响:用户在调用 /v1/chat 等热路径报错时,前端只能显示 axios 默认错误或空,体验差;无法提示"额度超限,请稍后重试"等可操作信息。

### P2(风格/文档/契约)

**P2-1. `E<HTTP><seq>` 编码与 snake_case 并存**
- 证据:`internal/auth/errors/errors.go:4-8`(`E4015`..`E5031`)。该编码与全仓其余 snake_case 完全异质,且不对外(对外由 auth/handler 字面量另行输出)。
- 影响:风格不一致,新人困惑;E 码与 snake 码无映射表。

**P2-2. 配额内部日志码与对外 HTTP 码双轨分裂**
- 证据:`internal/gateway/quota/reject.go:54-57` map(`QUOTA_RPM_REJECTED`)用于 slog;`errorCode()`(reject.go:66-71)输出 `quota_rpm_exceeded` 用于 HTTP。两套码无文档映射。
- 影响:日志排查需知道 `QUOTA_RPM_REJECTED` == HTTP `quota_rpm_exceeded`;指标 `gateway_quota_rejected_total` 用 layer+gate 维度,与码又不同维度。

**P2-3. 契约测试不校验错误码**
- 证据:`tests/contract/contracts.json` grep `error_code`/`"code":` 零命中;仅声明 `response_fields:["code","message"]`,无具体码值断言。文件头 `$comment` 写"Team I should update contracts.json error_codes blocks to match"(errcodes.go:5 同款 TODO),但 block 始终未建。
- 影响:错误码改了不会触发契约失败,前后端约定靠口头/注释,易漂移。

**P2-4. 无错误码字典文档**
- 证据:`docs/` 下无 error 相关文件;`rg '错误码|错误代码' docs/ README.md` 零命中。errcodes.go 注释是唯一"字典",但仅控制面。
- 影响:热路径/配额/认证/组织服务的码无文档,前端、运维、新成员无据可依。

---

## 4. 评分总结(9 项)

| # | 维度 | 评级 | 依据 |
|---|------|------|------|
| 1 | 命名一致性 | **差** | snake_case(`internal_error`)、`E4015`、`org.*` 点号、中文消息、裸字面量(`internal`)并存;同语义 `internal`/`internal_error` 双码(P0-1)。 |
| 2 | 分类体系 | **差** | 无统一 type 注册表;type 散落字面量(热路径 6 个 ErrorType 常量 + `service_unavailable`/`group_not_authorized`/`organization_error`/`authorization_error`/`concurrency_limit`/`conflict`/`forbidden` 等内联);无 invalid_request/auth/rate_limit/not_found/internal/upstream 的清晰分层(对比 New API 双层枚举)。 |
| 3 | HTTP 状态映射准确性 | **一般** | 多数映射正确(401/404/429/502/504/503/402);但 `ErrModelGroupUnavailable`=400(应 403)(P1-4)、`ErrModelGroupUnauthorized`=403 正确但与前者同语义区隔仅靠注释;499 非标准但合理;errormap 丢 429(P0-3)。 |
| 4 | 覆盖完整性 | **差** | 缺:流式中途错误归一化(P1-5)、上游 429 透传(P0-3)、failover 穷尽后的专用码(兜底 ErrInternal)、内容策略违规(content_policy,LiteLLM 有专门类)、context window exceeded(LiteLLM 有专门类)。 |
| 5 | 对外 vs 内部码区分 | **一般** | 配额做了区分(QUOTA_RPM_REJECTED 内 vs quota_rpm_exceeded 外)但无映射文档(P2-2);AuthError E 码"仅内部"却从未对外,实际对外由另一套字面量承担,两套无关联;统一来看分裂不必要。 |
| 6 | 可扩展性 | **差** | 新增码成本高:热路径加码需改 error.go 常量+type 常量+errormap;控制面加码需改 errcodes.go + handler 字面量 + 前端 map + (理想)契约;无集中注册表,易冲突(`model_unavailable` 已撞码)。 |
| 7 | 前端可用性 | **差** | ERROR_MESSAGES 仅 13 码全控制面;热路径码零覆盖(P1-6);fallback 返回 null(无通用兜底文案);多信封形状致解析路径不统一(P1-2)。 |
| 8 | 文档/契约 | **差** | 无错误码字典文档(P2-4);契约不校验码值(P2-3);errcodes.go 注释自承 TODO 未做。 |
| 9 | 上游透传策略 | **一般** | `core.ParseProviderError`(errors.go:178-237)设计良好(按 status 分流+透传上游 code+param),但**热路径未用**,改用脆弱的 string-match errormap(P0-3);流式 zero-copy 透传不归一化(P1-5)。设计了好的没用,用的不好。 |

**0 好 / 4 一般 / 5 差**。相对业内:热路径形状设计接近 OpenAI/Anthropic(优于 One API),但归一化、集中化、可观测性落后于 LiteLLM 与 New API。

---

## 5. 改进建议

### 5.1 短期补强(1-2 周,低风险)

1. **统一 `internal`**:全仓将 `"internal"` 字面量替换为 `internal_error`(auth/handler/auth.go:121,126;orgservice org_handler.go/role_handler.go/mapping_handler.go 共 ~12 处)。前端 ERROR_MESSAGES 已有 `internal_error`,无需改前端。
2. **Anthropic 路径补 `code` 字段**:在 `respondAnthropicError`(messages.go:361)的 error 对象内增加可选 `code` 字段(非破坏性扩展,Anthropic 官方允许字段扩展),值取自 UnifiedError.Code。同时补 `request_id`(P1-1)。
3. **errormap 改用 status code + 上游 code 透传**:让 `MapProviderError` 接收 HTTP status(mapper.go 签名扩展),优先按 status 分流(429→RateLimit、5xx→Provider、4xx→InvalidRequest),string-match 仅作兜底;对 status 未知(网络错误)才走关键字。同时把 `core.ParseProviderError` 的上游 code 透传接到 UnifiedError(给 UnifiedError 加 `Provider`/`UpstreamCode` 可选字段)。
4. **前端 ERROR_MESSAGES 扩充热路径码**:补 `invalid_api_key`/`key_disabled`/`model_unavailable`/`provider_timeout`/`provider_error`/`quota_rpm_exceeded`/`quota_concurrency_exceeded`/`budget_exceeded`/`group_not_authorized`/`auth_service_unavailable`,并给 resolveErrorMessage 加通用 fallback("操作失败,请稍后重试")。
5. **修 `ErrModelGroupUnavailable` 语义**:区分两种场景——binding 配置缺失(503 model_unavailable)与授权缺失(403 group_not_authorized);当前 error.go:76 用 400 表授权,改为 403 或拆成两个码。
6. **契约补码值断言**:在 contracts.json 为每个错误路由加 `error_codes` 块(如 SetUserQuota 429 → `quota_rpm_exceeded`),消除 errcodes.go:5 的 TODO。

### 5.2 长期重构(目标错误码体系草案)

**目标**:全仓一套集中 `ErrorCode` 枚举 + 双层 type/errorCode(对标 New API,但保留本仓热路径的 OpenAI/Anthropic 形状兼容)。

```
internal/errors/  (新包,单一真源)
  ├─ codes.go     ErrorCode 枚举(命名空间式分层)
  ├─ types.go     ErrorType 枚举
  ├─ error.go     GatewayError{Type,Code,Message,HTTPStatus,Param,Provider,UpstreamCode,RequestID}
  ├─ registry.go  code→(type,httpstatus) 映射注册表(单一真源)
  └─ render.go    统一渲染: renderOpenAI / renderAnthropic / renderControl(形不同,源同一)
```

**ErrorCode 命名空间(草案)**:
- `invalid_request.*`(invalid_request.body/model_required/messages_required)
- `auth.*`(auth.invalid_api_key/auth.key_disabled/auth.service_unavailable)
- `authz.*`(authz.group_not_authorized/authz.group_not_found)
- `model.*`(model.not_found/model.unavailable)
- `upstream.*`(upstream.timeout/upstream.error/upstream.rate_limited)
- `quota.*`(quota.rpm_exceeded/quota.concurrency_exceeded)
- `budget.*`(budget.exceeded)
- `org.*`(org.budget_not_configured/org.pending_studio/org.forbidden)
- `internal.*`(internal.error/internal.config)

**ErrorType(顶层,≤10 个,与 HTTP 一一对应)**:
`invalid_request_error`(400)/`authentication_error`(401)/`permission_error`(403)/`not_found_error`(404)/`rate_limit_error`(429)/`overloaded_error`(503)/`upstream_error`(502)/`timeout_error`(504)/`billing_error`(402)/`internal_error`(500)。

**收益**:
- 单一真源:新增码只改 registry.go,自动派生 type/httpstatus,杜绝 `internal`/`internal_error` 双码与 `model_unavailable` 撞码。
- 形状归一:render.go 统一 6 种信封→3 种(OpenAI/Anthropic/Control),ops 裸字符串并入 Control。
- 前端映射:一份 ERROR_MESSAGES 覆盖全部命名空间码。
- 契约:registry.go 即字典,contracts.json 可由其生成。
- 上游透传:GatewayError.UpstreamCode 保留上游原码,type 归一化用于分流,定位时双维度。
- request_id:渲染层统一注入错误体。

**迁移路径**:先建 errors 包 + registry,逐子系统替换(热路径最先,控制面次之,orgservice/auth 最后),旧码作 alias 保留一版过渡期,契约同步更新。

---

## 附录 A:扫描方法与证据可复现性

- 错误码定义点:`rg 'Err[A-Z].*=.*(UnifiedError|errors\.New|fmt\.Errorf)' internal/`
- E 码:`rg 'E[0-9]{3}[0-9]' internal/`(仅 auth/errors/errors.go)
- 控制面 Code*:`rg 'CodeBadRequest|CodeInternalError|...' internal/controlplane/handler/`(定义点 errcodes.go:6-62,引用 241 处全在 handler 包内)
- 内联字面量:`rg '"code":' internal/auth/handler internal/orgservice/handler internal/gateway/handler`(逐文件)
- 中文 errors.New/fmt.Errorf:`rg 'errors\.New\("[^"]*[\x{4e00}-\x{9fff}]' internal/`
- 业内:WebFetch LiteLLM exception_mapping / OpenAI error-codes / Anthropic errors;gh clone one-api & new-api 取 `relay/controller/error.go`、`relay/model/misc.go`、`types/error.go`、`service/error.go`。

## 附录 B:做得好的地方(客观记录)

1. **热路径 UnifiedError 的 type→HTTP 映射基本正确**(error.go:9-15 + 各 Err\* 的 HTTPStatus),形状对齐 OpenAI/Anthropic,这部分**领先于 One API**。
2. **`core.GatewayError` 设计完整**(errors.go:含 Param/Code/Provider/Unwrap/HTTPStatusCode switch/ParseProviderError 上游透传),虽热路径未用,但已是可复用资产。
3. **配额拒绝响应信息密度高**(reject.go:39-51:type/code/message/param/gate/layer/subject_type/limit/retry_after_seconds + Retry-After 头),远超业内最小实现,利于客户端退避决策。
4. **控制面 `errcodes.go` 常量块 + 注释分档**(errcodes.go:6-62)是全仓最接近"集中注册表"的尝试,32 个码覆盖控制面常见场景,方向正确,只是未被全部 handler 遵守。
5. **`shouldTriggerFailover`(chat.go:335-355)按 status 决定是否 failover**(400/401/403/413 不触发),是错误的正确"用法"——错误码不仅给人看,也驱动网关行为,这是好设计。
6. **错误类型与 failover/429 判定解耦**用 `errors.As` 而非 string-match(chat.go:342,358),热路径内部是规范的。
