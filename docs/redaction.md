# 可逆脱敏（Privacy Redaction）

> 目的：把用户消息里的凭证 / 个人信息替换成占位符后再发给上游厂商，模型返回的占位符在网关侧还原成原文。
> 组件：`internal/gateway/privacy`（检测 + 占位符引擎，自带本地模型/规则集）、`internal/gateway/redact`（把引擎接到
> `core.ChatRequest` / `core.AnthropicMessagesRequest` 的适配层）。

## 一、三档模式

`privacy.mode`（环境变量 `PRIVACY_MODE`）取值：

| 模式 | 请求是否改写 | 响应是否还原 | 典型用途 |
|------|--------------|--------------|----------|
| `off`（默认） | 否 | 否 | 未接入；网关行为与接入前逐字节一致 |
| `detect` | **否** | 否（无占位符可还原） | 先摸清命中率再决定是否掩盖；只统计命中 |
| `mask` | **是** | 是 | 真正脱敏；原文不出网 |

- `detect` 下引擎强制 `RulesOnly` 且清空 actions，即「只报不拦」：任何规则都不会中断请求。
- `off` 时 `Engine` 仍可用，所有方法安全 no-op（`NewSession()` 返回空会话）。
- 引擎版本由 `privacy.Version` 标识（当前 `2026-10-28.1`），启动日志与本文档保持一致，便于排障时确认
  「跑的是哪个版本的检测规则」。

## 二、fail-closed 语义（mask 模式）

铁律：**宁可请求失败，也不能把原文发给上游**。

1. 请求侧 `ProtectChat` / `ProtectAnthropic` 返回错误 → handler 必须终止请求（5xx），**不得**用原文重试。
2. 同一次请求**不得 failover**：不能换渠道/换 Key 重试——否则等于把原文又发了一遍。
3. 启动期同样 fail-closed：`redact.New` 失败（模式非法、规则非法、模型下载失败等）→ `log.Fatal`，
   网关不启动，不会静默降级成 `off`。注入本身是编译期强约束（router 里直接调
   `chatHandler.SetRedactor` / `messagesHandler.SetRedactor`），不存在「配置开了但没挂上」的运行时状态。
4. 单次输入超过 `privacy.max_input_bytes` 时，mask 模式按错误处理（走第 1 条），不会悄悄放行。
5. 失败响应统一用专用错误码：HTTP 500 + `error.code = "redaction_failed"`
   （两条链路一致）。客户端可据此区分「隐私保护失败」与上游/网关故障，且该错误**不触发**任何重试或 failover。

## 三、范围限制：只碰纯文本

处理范围（适配层 `redact` 已经实现，调用方不要绕开它自己写正则）：

| 对象 | 字段 | 处理 |
|------|------|------|
| OpenAI Chat | `messages[].content` 为字符串 | 脱敏 / 还原 |
| OpenAI Chat | `messages[].content` 为 `[{type:"text", text}]` | 只处理 `type=="text"` 的 part |
| Anthropic Messages | `system`（字符串或 text block 数组） | 脱敏 / 还原 |
| Anthropic Messages | `messages[].content` | 同上，只处理 text |
| OpenAI Chat 响应 | `choices[].message.content`、`tool_calls[].function.arguments` | 还原 |
| Anthropic 响应 | `content[].text`、`content[].input` | 还原 |

**不处理**（刻意的，改它们会破坏协议语义或使缓存/路由错乱）：

- 工具定义 `tools` / `functions`（JSON Schema 里的示例可能含高熵串，但改写会改变模型能力边界）；
- `tool_call_id` / `tool_use.id`（是上游与客户端之间的不透明 ID，必须原样透传）；
- 图片、音频、视频 part（`image_url`、`input_audio` 等；base64 不扫描）；
- Anthropic `tool_use.input` 的**请求侧**内容（响应侧会还原占位符，见上表）。

## 四、会话生命周期

- 每个请求 `engine.NewSession()`，**必须** `defer session.Close()`；占位符 → 原文的映射只存在于该 Session。
- 同一个 Session 不得跨请求复用（否则会把 A 用户的映射泄漏给 B 用户）。
- 流式响应必须通过 `session.RestoringWriter(w)` 写出并在结束时 `Finish`，否则跨 chunk 的占位符无法还原。

## 五、配置

`config/{env}.yaml` 的 `privacy` 段（环境变量优先级更高）：

| YAML key | 环境变量 | 默认 | 说明 |
|----------|----------|------|------|
| `privacy.mode` | `PRIVACY_MODE` | `off` | `off` / `detect` / `mask` |
| `privacy.rules_only` | `PRIVACY_RULES_ONLY` | `true` | `true` 只用规则、不下载本地模型；`false` 才加载模型 |
| `privacy.actions` | —— | 空（等价全 `mask`） | 规则类型 → `mask` / `warn` / `block`；map 只能写 YAML |
| `privacy.max_input_bytes` | `PRIVACY_MAX_INPUT_BYTES` | `8388608` | 单次脱敏文本上限 |
| `privacy.cache_dir` | `PRIVACY_CACHE_DIR` | 空 | 本地模型缓存目录，`rules_only=true` 时无意义 |
| `privacy.key_path` | `PRIVACY_KEY_PATH` | 空 | 占位符 HMAC 密钥文件；见 5.3，留空会打散上游前缀缓存 |

启动日志会打印一行 `privacy redaction configured`，含 `mode` 与 `version`（`privacy.Version`，当前
`2026-10-28.1`），用于确认线上实际生效的模式与引擎版本。

### 5.1 `privacy.actions`：key 是命中 Kind，不是规则 id

- key 必须填引擎报告的 **Kind**（大写 + 下划线），例如 `API_KEY`、`EMAIL`、`PHONE`、`PRIVATE_KEY`、
  `GITLAB_TOKEN`。凭证类规则的 Kind 由规则 ID 转大写得到；不是规则的小写 id，也不是自然语言描述。
  **写错的 key 不会报错，只会静默不生效**——上线前用 `detect` 模式看一遍实际 Kind 再配。
  YAML 里写成小写（`gitlab_token`）也可以：读配置时 key 会被 Viper 小写化，适配层再统一转成大写 Kind。
  这条曾经是个坑——不归一化时 `block` 会静默退化成默认的 `mask`，策略比配置的弱一档。
- 值只有三种：
  - `mask`（缺省）：原文换成占位符，请求继续；
  - `block`：同样换成占位符，但整条请求**被拦下**（引擎返回 `ErrBlocked`，handler 按 fail-closed 终止）；
  - `warn`：**原文照发给上游**，只记录命中。这是 fail-open 的放行语义，只在确认该类型可接受时才用。
- `detect` 模式会清空 actions（不论配置写了什么，只报不拦）。

示例：私钥类直接拒绝、其余照常打码：

```yaml
privacy:
  mode: mask
  actions:
    PRIVATE_KEY: block
    GITLAB_TOKEN: block
    EMAIL: mask
```

### 5.2 语义模型（`rules_only=false`）

- 默认 `rules_only=true`：只跑规则（正则 + 凭证 shape 检测），**不触发任何下载**，也没有模型依赖。
- 改成 `false` 后进入语义模型路径：首次检测时按需把固定版本（pin 过 SHA-256）的 ONNX / CoreML 产物
  下载到 `privacy.cache_dir`（留空则用 `os.UserCacheDir()/leaker/privacy`），之后本地离线推理，提示词不出网。
  产物约 1.6GB（privacy-filter）外加 Guard 资源，首次请求会明显变慢。
- 下载 / 校验 / 加载失败 → `ErrModel` → `Protect` 返回错误 → **mask 模式下该请求失败**（fail-closed），
  不会退回「纯规则」或原文。因此生产上要么预热 cache 目录，要么先保持 `rules_only=true`。

### 5.3 占位符稳定性与上游前缀缓存

占位符形如 `{{GITLAB_TOKEN_7jjnmwmy}}`，后缀是 `HMAC-SHA256(key, kind + value)` 的截断：

- **同一会话内**：同一凭证永远得到同一占位符（引擎的 known 表），所以多轮对话里模型看到的是同一个符号，
  推理连贯、不会「同一个 key 一会儿一个样」。
- **跨请求**：占位符由密钥决定。`privacy.key_path` 为空时**每个请求都会生成新密钥**，同一凭证在不同请求里
  得到不同占位符，上游的 prefix cache（含 Anthropic `cache_control` 前缀缓存）会全部落空——
  这是真实的账单影响。把 `key_path` 指到一个固定文件后，同一凭证跨请求、跨重启都得到同一占位符，
  前缀缓存不被打散。
- 该密钥文件同时决定了「同一凭证的指纹是否可比」，**不要放进输出目录、不要提交仓库**，权限保持 0600。
- **多副本**必须共享同一份密钥文件（同一个挂载卷 / Secret），否则同一凭证在 A 副本与 B 副本得到不同占位符，
  跨副本命中的前缀缓存依然会失效；网关启动时若 `mode=mask` 且没配 `key_path`，会打一条 WARN 提醒。
- 还原依赖产生占位符的那个 Session 的映射表，跨请求不共享映射；网关每次请求都是「打入 → 上游 → 还原」闭环，
  客户端拿到的始终是原文，历史里带回来的占位符会被重新识别成同一占位符。

## 六、已知缺口

### 6.1 端点覆盖

已接入（5 条，都在 `router.setup()` 里注入，改造方式一致）：

| 端点 | 请求侧 | 响应侧 |
|------|--------|--------|
| `POST /v1/chat/completions` | messages / 多模态文本 part | 非流式就地还原；流式逐 event 还原 |
| `POST /v1/messages` | Anthropic messages / system | 同上（Anthropic 事件格式） |
| `POST /v1/responses` | `input`（string 或 `input_text` 元素）+ `instructions` | `output[].content[].text` / `summary[].text` |
| `POST /v1/embeddings` | `input` 的字符串（含 `[]string` / 混合数组里的字符串） | 无文本（向量）⇒ 不还原 |
| `POST /v1/rerank` | `query` 与 `documents[].text` | 回显文档的 `document.text` |

**明确未脱敏**（在 mask 模式下调用它们等于绕过策略，部署层面要自己拦住入口）：

- `/v1/p/:provider/*path`（provider passthrough 直通）：请求体是任意字节流，可能含二进制/多模态，无法安全判别「哪里是文本」。
- `/api/event_logging/batch`、视频/素材接口：不是对话内容；batch 的响应发生在异步轮询阶段，Session 无法闭环。
- `/v1/responses/input_tokens`、`/v1/responses/{id}`、`/compact`：本 worktree 只注册了 `POST /v1/responses`。

新增链路时：照现有 handler 的结构接（每请求一个 Session、failover 外脱敏一次、fail-closed），并在
`router.setup()` 补一行 `SetRedactor`，否则不会生效（注入是编译期强约束，漏了会立刻编译失败）。

### 6.2 字段级缺口

- 工具定义（`tools` / `tool_choice`）、`function_call` 的 `call_id`/`name`/`arguments`、`tool_use.input` 一律不动：
  这些是协议字段，改写会让模型与客户端对不上号。
- 媒体（image / audio / video / base64 / URL）一律不动，即使 URL 里长得像密钥。
- `metadata`、`annotations`、`error.message`、usage 等非正文文本不改写（`error.message` 若是上游回显占位符，
  客户端会看到 `{{..}}` 而不是原文——不是原文泄漏）。
- **只做单轮文本替换**：占位符跨请求是否一致取决于 `privacy.key_path`（见 5.3）；上游若自行改写占位符
  （例如加空格、换行），还原会失败并原样返回占位符（不报错、不泄漏原文）。
- **模型可能伪造占位符**：模型学会了占位符格式后可能自己编一个（`{{EMAIL_xxxxxxxx}}`）。引擎只还原自己映射表里
  有的占位符，其余原样返回，客户端会看到假占位符文本。这是这类方案的固有副作用，靠观测（restore 未命中）发现。
- **日志落库的正文也脱敏了（仅 mask 模式）**：`TraceCapture` 是中间件，在 handler 之前就把请求体抓进了
  gin context，所以 handler 里用同一个 session 把那份字节也过一遍（`protectTraceBody` → `Session.ProtectTraceBody`），
  落库内容与上游一致。代价是 JSON 层会把 typed 层刻意不动的字段（工具参数、`metadata` 等）也遮上——
  宁可多遮，不能把凭证写进自己的库。detect/off 模式不改写：那两个模式下上游拿到的本来就是原文，trace 与之一致才是事实。
  E2E 已断言：mask 期间 `call_traces` 含占位符、明文 0 条。
  注意这只覆盖 `/v1/chat/completions` 等 5 个接线过的端点；`/p/:provider/*` 透传等路径仍由上游自行记录。
- **命中只进日志**：适配层每次请求会打一行 `privacy_hits`（含 `mode`、`engine_version`、`count` 与按 Kind
  聚合的 `kinds`，**不含任何原文**）。要进指标/审计库需要另外接 `Session.HitsByKind()`，目前没有埋点看板。

## 七、测试与验证

### 7.1 三层测试，各管一段

| 层 | 位置 | 验的是什么 | 需要什么 |
|---|---|---|---|
| 单元 / 矩阵 | `internal/gateway/redact/*_test.go` | 引擎接线：模式矩阵、fail-closed、SSE 跨 chunk、字段级（改哪些不改哪些） | `go test` 即可 |
| 进程内 E2E | `tests/integration/privacy_redaction_e2e_test.go` | 真实 `http.Client` → gin → orchestrator → providers → 假上游；9 个用例覆盖 5 个端点 | `make test` |
| 双进程 E2E | `tests/e2e/privacy/run.sh` | 两个真实进程 + 真实 API Key 鉴权 + 控制面下发 + Redis 调度 | 本机 PG、Redis、`atlas`、python3 |

### 7.2 怎么跑双进程 E2E

```bash
bash tests/e2e/privacy/run.sh
```

脚本会：重建 `llm_gateway_priv_e2e` 库（**不会碰 `llm_gateway`**，库名可用 `E2E_DB_NAME` 改）→
`atlas migrate apply` → `tests/e2e/privacy/seed` 写最小数据集（admin 用户、固定 API Key、openai 兼容渠道、
chat 模型、模型组）→ 起假上游 → 起 control-plane → 起网关 → 断言 → 收尾。

**顺序不能反**：control-plane 只在启动那一刻把「可用模型快照」写一次 Redis，之后要等 provider 变更才再写。
先起 CP 再灌数据，网关拿到的就是空快照，请求会 404 `unknown_model`。之前脚本能过，只是因为共用了一个长期
存活的 Redis，里面残留着上一轮的快照——换台干净机器或 Redis 被清过就必炸。

用例断言的都是**字节**，不是"函数被调用过"：

1. **mask 非流式**：上游 body 里只有 `{{GITLAB_TOKEN_xxxxxxxx}}`，客户端拿回原文；
2. **跨请求占位符稳定**：两次请求同一凭证得到同一占位符（`privacy.key_path` 生效，上游前缀缓存不被打散）；
3. **流式跨 chunk**：假上游故意把占位符从中间切成两个 SSE chunk，客户端仍拼回完整原文；
4. **detect**：原文照发上游，同时落一行 `privacy_hits`；
5. **block**：HTTP 500 `redaction_failed`，且上游请求计数不增加（fail-closed 真的没发出去）；
6. **trace 落库**（夹在 mask 用例之后）：`call_traces.request_body` 含占位符、含明文 0 条——落库的正文
   与发往上游的一致（trace 落库是异步的，脚本会轮询等它写完）。

这条 E2E 也顺手逼出了一个独立问题：`atlas-migrations` 相对代码缺表缺列（orgservice 的 `studios` /
`user_roles` / `dept_studio_map`、配额列 `quota_rpm`、`encryption_keys` 等），从零建库后服务一启动就报
`column does not exist`。已由 `atlas-migrations/20261030062033_add_org_quota_and_crypto_tables.sql` 补齐，
`seed` 不再做任何 `AutoMigrate`——本地库与生产库都只认 Atlas。

## 八、环境启用步骤（test / prod）

脱敏由两个环境变量控制，均由 CI 在部署时注入（非密钥，不进 GitLab 变量也能跑）：

| 变量 | compose 透传 | 默认 | 说明 |
|---|---|---|---|
| `PRIVACY_MODE` | `PRIVACY_MODE: ${PRIVACY_MODE:-off}` | test `detect` / prod `mask` | `off` 不启用 / `detect` 只观测 / `mask` 改写并还原 |
| `PRIVACY_KEY_PATH` | `PRIVACY_KEY_PATH: ${PRIVACY_KEY_PATH:-}` | `/datalog/privacy-key` | 仅 `mask` 需要；`/datalog` 已 bind mount 进容器，蓝绿两色共用同一份 |
| `PRIVACY_MAX_INPUT_BYTES` | `PRIVACY_MAX_INPUT_BYTES: ${...:-8388608}` | prod `33554432` | 单请求体上限，超限 fail-closed（500 `redaction_failed`）；实测 9MB 正常、36MB 被拒 |

CI 侧用可覆盖变量做开关：`TEST_PRIVACY_MODE` / `PROD_PRIVACY_MODE`（缺省 test `detect`、prod `mask`），
路径同理 `TEST_PRIVACY_KEY_PATH` / `PROD_PRIVACY_KEY_PATH`，上限 `PROD_PRIVACY_MAX_INPUT_BYTES`。

### 8.1 开启 detect（默认，零行为变更）

打 tag 后手动依次执行 `migrate-prod` → `deploy-prod`。网关日志会出现：

```
privacy redaction configured mode=detect ... key_path_set=false
```

请求照原样发往上游（**不改写、不影响前缀缓存**），命中时多一行：

```
privacy_hits mode=detect count=1 kinds={"GITLAB_TOKEN":1}
```

用真实流量观察 1~3 天：确认命中率与 `kinds` 分布合理、无误报风暴（误报指标 = 同一用户反复命中
普通字符串）。`rules_only=true` 时只跑规则，不加载语义模型。

### 8.2 切 mask（真改写）

前置：**密钥文件必须先就位**。在两色共用、且已挂载进容器的目录里生成一次（不要靠首启自动生成，
蓝绿两色同时首启会各自生成不同的 key，占位符不一致 → 上游前缀缓存全废）：

```bash
# prod 主机上（容器内路径 /datalog/privacy-key ↔ 宿主 /datalog/privacy-key）
umask 077 && head -c 32 /dev/urandom | xxd -p -c 64 > /datalog/privacy-key
chmod 600 /datalog/privacy-key
```

然后建 GitLab 变量 `PROD_PRIVACY_MODE=mask`（`protected=false` 仅当部署 ref 未受保护；prod 由 tag
触发，保持默认 protected 即可），重跑 `deploy-prod`。启动日志应显示
`mode=mask key_path_set=true`；若 `key_path_set=false` 会出现 placeholder churn 的 WARN。

### 8.3 回滚

- 只回代码：`rollback-prod` 手动任务（旧颜色容器还在，秒级切回）。
- 关掉脱敏：把 `PROD_PRIVACY_MODE` 改回 `detect`（或 `off`）重跑 `deploy-prod`。
- **不要删密钥文件**：mask 期间客户端历史里已经存在还原后的原文，文件丢失只影响新请求的占位符
  稳定性（变成新映射），不会损坏历史数据，但会让上游缓存整体失效一轮。

### 8.4 上线顺序与已知约束

1. `migrate-prod`（加法迁移，`IF NOT EXISTS`；`departments` 两个索引建在既有小表上）→ 再 `deploy-prod`，
   `deploy-prod` 的 `needs` 已强制这个顺序。
2. 蓝绿期间新旧两色同时在线，**必须共用同一份 `/datalog/privacy-key`**，否则同一条 token 在两色得到
   不同占位符：还原仍然正确（各自内存映射），但上游前缀缓存命中率归零。
3. 覆盖范围仍受第六节限制：`/p/:provider/*` 透传链路未接脱敏，别把它当"全链路已保护"。
4. `privacy_hits` 目前只有日志，没有指标/看板；量级评估靠 `docker logs | grep -c privacy_hits`。

