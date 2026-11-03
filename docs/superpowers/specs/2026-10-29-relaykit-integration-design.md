# RelayKit 协议转换层集成方案（待评审）

## 1. 决策与边界

用户已确认采用 new-api 的协议转换实现，商业授权已谈妥；具体交付、保留声明和使用义务按已签协议执行，不将授权材料或密钥入仓。

本次集成 github.com/QuantumNous/new-api/relaykit **v0.3.5**，优先使用版本化 Go module，不整体复制 new-api，不接它的 relay/channel、model、service 或管理前端。后续版本通过专项升级测试后再更新。

基线：origin/main 40a56bce；分支 feat/protocol-relaykit；工作区 .worktrees/protocol-relaykit。原 checkout 的 314 个未提交文件保持原样。本方案以这个 main 基线为准，之前基于其他分支的行数和缺陷数量不作为验收指标。

当前只编写文档。文档评审前不改产品代码、不部署、不提交或推送。

## 2. 需要纠正的前期结论

- RelayKit 是四种主要文本协议的转换库，不是多渠道 HTTP 转发 SDK。Chat、Responses、Claude Messages、Gemini 的数据模型、请求/响应/流式转换在它里面；传输、认证、调度和计费仍归我们。
- convmeta.Values 已实现 Meta，不再自行编写 17 个方法。
- 库存在转换器不代表目标模型支持对应功能；工具、文件、thinking 的最终支持仍要结合上游能力验证。
- 默认 ToolLossPolicy 是 allow，safe/strict 为请求阶段可选策略；响应与流式阶段并不按这个选项直接中止。完整内容丢失检查还需宿主补充。
- 同格式返回原 DTO 不等于原始字节透传；chat 现有路径也不是端到端零拷贝，只是流式字节转发避免重新生成 SSE。
- 跨协议未知字段不做无条件 merge。源协议的字段直接塞给另一种协议可能制造上游 400；只保留已验证的目标兼容字段，其他字段生成诊断。
- 没有 Prometheus endpoint 不等于没有观测；现有 OTel、usage collector、Trace 可复用。本次不把 Prometheus 改造设为强制前置。
- SDK 不会补出缺失的 usage，不保证降低延迟；性能和计费变化要实测。
- main 已有 Anthropic stream 工具参数/异常路径修复。集成要保住这些回归，不能用旧分支的 540 行状态机替换 main 的 798 行修复版而丢掉行为。

## 3. 目标架构

客户端 -> 现有 handler 与鉴权 -> 预算/配额/模型组调度 -> 实际渠道选择
  -> 保留路径：chat 到兼容渠道、原生 Responses、embeddings/rerank/video
  -> 转换路径：协议会话 -> RelayKit -> core 桥 -> 现有 orchestrator/provider
  -> 后续原生路径：受控原生执行 -> 字节转发 + 现有观测（独立阶段）

只新增 internal/protocol 包，不建设新的插件系统、独立调度器或自研转换图。入口和出口转换复用库的注册表。

依赖方向：handler/providers -> protocol -> core + relaykit；core 不反向 import protocol；protocol 不 import handler/providers/Gin，防止循环依赖。

阶段一保留 core.ChatRequest、core.ChatResponse、core.ResponsesRequest 和 Provider 接口；RelayKit DTO 仅在 protocol 包的边界内使用，避免所有渠道一起更换 canonical。

## 4. 两个发布阶段

### R1：Messages 和 Responses 桥接完整接入

1. Messages 请求在新路径直接从原始 JSON 解码到 dto.ClaudeRequest，避免先过 core.AnthropicMessagesRequest 丢字段。
2. 转为 OpenAI Chat DTO，再通过唯一桥接点构造 core.ChatRequest。
3. 保留现有预算、模型组、provider hint、超时、重试、故障转移。
4. core.ChatResponse 转库的 Chat DTO，再由同一个请求会话转回 Claude；stream 使用相同会话的 Meta 和工具状态。
5. ResponsesViaChat 与 StreamResponsesViaChat 的共用入口接入 RelayKit；原生 Responses provider 不改。
6. 默认 legacy；按请求/Key 开启 shadow 或 active，转换策略和诊断可观测。

R1 首版仅主动开放 messages->Chat 和 Responses->Chat 对经过验收的 OpenAI-compatible 出口。Anthropic/Gemini/Bedrock 等仍经自有出口翻译的组合保持 legacy 或 shadow，直到第二次转换的字段保留得到验证。

### R2：出口转换和同方言原生路径

- Anthropic、Gemini/Vertex 分别替换出口请求/响应/流转换，保留 URL、Header、认证、连接池、重试实现。
- Bedrock Converse 不在库格式里，继续现有实现；Anthropic over Bedrock 的传输与 Converse 是不同 API，不合并处理。
- 原生路径以实际渠道的 endpoint/能力判定，不按 provider family 常量认定所有兼容服务都支持 Responses。
- failover 每次重新选择执行路径，并从源请求重新转换；某次尝试的工具编码状态不得污染下一次尝试。
- 不开放任意 catch-all 路由；每个原生端点显式登记并接入组织/模型授权、预算、配额和用量归属。
- 附属端点、状态 ID 绑定、files/batches、count_tokens 另行设计，不因新增 raw 路径自动宣称支持。

R2 在 R1 验收后出独立细化计划，不在首版删除 provider 转换代码。

## 5. 新增文件与职责

- internal/protocol/session.go：每个请求/尝试一个 Session，包含 convmeta.Values、源格式、目标格式、诊断和工具编码状态。
- internal/protocol/request.go：解码源 DTO，调用 ConvertRequest；调用 Options 明确指定策略，不依赖零值行为。
- internal/protocol/bridge.go：ChatRequest/ChatResponse/ResponsesRequest/ResponsesResponse 与库 DTO 的唯一适配位置。
- internal/protocol/diagnostics.go：库诊断 + 宿主桥接损失，有限长度摘要和统一 ConversionError。
- internal/protocol/stream.go：io.ReadCloser 转换适配器，跨 chunk 状态、关闭传播、finalize 和结果快照。
- internal/protocol/sse.go：翻译用完整 SSE 帧读取器，保留 event 字段、多行 data、CRLF；不是把 ObservedSSEStream 旁路观察器冒充转换器。
- internal/protocol/policy.go：启动加载的不可变策略、legacy/shadow/active、Key 白名单、按入口及实际 provider 实例灰度。
- tests/protocol/：桥、协议转换、流式、策略和基准；tests/gateway/handler/：Messages 集成回归；tests/providers/：Responses 共用桥回归。

已有 core JSON helper、StreamBuffer、ObservedSSEStream、HandleStreamResponseAs 优先复用。

## 6. 桥接契约（上线阻断项）

- 采样参数保留零值与 nil 区别；top_p、stop 等走 core.ExtraFields 的目标字段，不误丢。
- Tool IDs、参数 JSON 字符串、is_error、null content、reasoning_content、thinking/signature 分别验证。不承载的语义生成桥接诊断。
- 当前 core.ContentPart 对 file 等未知类型有限制。首版给 file 定义显式结构与校验；不为集成全局放开所有未知类型。纯文本/PDF 支持以目标模型能力为准。
- thinking 中间态不保证能经 Chat 原样恢复 Claude 签名；同格式需求归 R2 原生路径。源配置、输出内容、签名的覆盖分别测试，避免将它们混为一个“thinking 已修复”。
- RelayKit 的诊断记录到实际目标协议；自有 provider 的后续裁剪也需入账，防止只记录第一次转换。
- model/provider/SessionKey 为本地路由语义，provider 字段不进入上游；调用前 model 改为实际选中模型，客户端输出保留对外模型名。
- 每次路由/故障转移后，按实际 provider 实例重新判定是否开放新路径；不可仅按入口Key放行未知目标。若 failover 跨到未验收目标，重建旧路径请求并记录 engine_changed，不混用不同尝试的 Session。
- URL 媒体解析默认不引入下载器；首版不安装全局 SetMediaResolver，不制造隐含网络访问。依赖媒体解析的输入返回明确转换错误；后续复用受控媒体实现后再开放。

## 7. 流式和计费契约

每条流一个 ResponseStreamState，调用 NewResponseStreamState -> ConvertStreamResponseChunk -> 正常结束 FinalizeStreamResponse。不能把每个 chunk 当非流式响应转。

- 读取 complete SSE frame 后才解析 JSON；单事件上限首版 2 MiB，超限报流错误，不静默扔掉再宣称完成。
- 正常 EOF/[DONE] finalize 一次；上游读错/坏 JSON/下游取消走错误路径，避免输出成功终态。上游有显式 finish 后无 [DONE] 的 EOF允许 finalize，并测试 usage-only 尾帧。
- 继续使用 io.ReadCloser + io.Pipe/背压；Close 同时释放上下游和协程，不缓存完整响应。
- 多工具交错、arguments 首 chunk 有值、finish 比 usage 早、空 delta、n>1 都有契约测试。Messages 只输出 choice 0，其他 choices 做低基数诊断，与 main 当前行为保持一致。
- usage 以现有上游 observer 作为结算入口；库 Usage 用于协议输出与对账，不再次扣费。missing 与真实 0 区分，估算标 estimated，本次不改估算结算规则。
- 原始 BillingUsage 保留协议来源；缓存读/写、5m/1h、reasoning token 分项对照，避免重复相加。
- 流中新增损失发生于响应头发送后：记录日志/OTel及结束快照，不尝试修改已发送响应头，不插入非标准 SSE 诊断事件。

## 8. 灰度、错误和观测

模式 legacy（默认）、shadow、active。首版从已有配置加载入口模式和 Key ID 白名单；启动时校验。不将环境变量开关描述为热更新。

首版只用启动配置，不新增管理 API 或配置订阅系统。请求开始固定策略，在途流不变；回滚通过 legacy 配置加蓝绿/滚动发版完成。后续如需全实例热切换，再复用既有 Redis snapshot 分发并单独验收，不宣称首版已有全局热回滚。

shadow 只对请求转换和测试/录制事件做离线回放，不重复请求上游，不消费真实生产流两次，不多计费。shadow 报告数量和诊断差异，不存原始敏感正文。

请求转换错误在上游调用前映射 HTTP 400，保持客户端入口的错误形状；上游错误仍沿用现有 errormap。请求损失摘要用 X-Protocol-Loss（最多 512 ASCII 字节、只放 code、不放正文），完整诊断留有脱敏的 trace；日志/OTel复用现有管线，指标标签不包含 Key、prompt、session。

宿主默认 safe：工具执行语义损失为请求错误；普通表现层差异报告诊断。未知必需内容/桥接数据丢失由宿主标为 error。请求带来的 safe/strict 400 属于预期行为变化，先在白名单验证。

## 9. 成本与收益（工程估算）

R1：依赖与契约基线 1–2 人日；Session/桥/诊断 3–5；SSE适配 3–5；Messages闭环 2–3；Responses共用桥 2–3；灰度/回归/压测 3–5。合计 **14–23 人日**，不含上游联调等待、授权交付和线上观察。

R2：Anthropic/Gemini出口与原生路径另估 **10–18 人日**；Bedrock专项、附属端点和新模态不在此数字内。此前“所有缺陷修 7/11”“迁移省几千行”“性能必然持平”均撤为待验证假设。

可交付收益：替代 Messages/Responses桥的手写翻译；复用工具状态机与诊断；新增上游版本按 module 升级验证；不触碰鉴权/数据库/调度核心。限制：能力受实际上游约束，跨方言仍有损，仍需维护桥和版本跟随。

## 10. 验收与生产影响

- legacy 默认下现有 Messages工具参数/错误/配额/provider hint用例全通过，chat兼容流不进入新解析器。
- 自建 golden 覆盖请求、非流、流、异常；库结果再过 core 桥后比较字段，不止测库本身。
- usage 相同录制输入下完全一致；未解释差异阻止上线；真实上游无usage保持 missing，不承诺计费覆盖率99%。
- 同一机器/相同fixture性能比较：记录 ns/op、B/op、allocs/op；mock上游压测记录TTFB与RSS。翻译路径P99相对基线增加10%触发复核，不把它当已达结果。
- 每入口分离发布，先内部Key，再小流量，观察一个峰值周期后扩大。错误率/账单口径异常立即回legacy。
- 无数据库schema迁移，无控制面业务模型替换，无前端升级；配置、内部灰度入口、gateway镜像会变。
- 协议首版只改指定入口的转换，新路径进程启动默认关闭。部署保留旧镜像，连接摘流后再终止进程，保留在途流的时间窗按现有网关超时设置。

## 11. 本轮不做

不引入 bifrost；不自研转换图；不替换所有 provider；不修改价格规则、鉴权缓存、budget fail-open；不顺带新增 images/audio/realtime；不将 Prometheus 和 body-limit 大改绑在协议集成里。相关稳定性加固作为独立项目，发布前核查反代与应用层现有保护即可。
