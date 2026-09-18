# 点读服务（读侧）实施计划（计划 3/4）

> **面向 Agent 执行者：** 必需子技能：使用 superpower-subagent-driven-development（推荐）或 superpower-executing-plans 按任务逐项执行本计划。步骤使用复选框（`- [ ]`）语法进行跟踪。

**目标：** 让「给定 `request_id` 取回正文」在生产形态下可用：读 `call_traces` 拿到定位键 `(dt, inst, part, row)`，经冻结包 `internal/tracearchive.Reader` 定位 spool 或封块产物并**逐字节**取回正文行；对「功能关闭（正文在 jsonb 列）」「写入时即失败（`body_missing`）」「定位不到（隔离件 / 行号越界 / 文件缺失）」「产物损坏」四类状态给出**可判定且互不混淆**的对外表现。

**架构（一句话）：** 读侧**不新建服务、不进网关**——它是 **control-plane 进程内**的一个服务（`internal/observability/service/`）+ 两个 HTTP 端点（`internal/observability/handler/` + `internal/controlplane/router/`）+ 一个运维 CLI（`cmd/trace-body/`）。**存储层读原语（footer/trailer 编解码、按定位键解压定位、spool 兜底、错误分类）计划 1 已完整交付并冻结**，本计划只做**接线、语义映射与生产形态验收**（见「既有能力基线」）。

**技术栈：** Go 1.26、`github.com/example/llm-gateway/internal/tracearchive`（计划 1，**只读复用、不改一个字节**）、`github.com/example/llm-gateway/internal/observability/{service,handler,model}`、`gin`（既有 `JWTAuth` / `InternalAuth`）、`gorm.io/gorm` + `gorm.io/driver/sqlite`（测试用内存库）、`github.com/stretchr/testify`、`vitess.io`/无新增依赖（**不新增任何第三方依赖**，只复用 `klauspost/compress/zstd`，它已在 `go.mod` 且被冻结包使用）。

**规格：** `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md`（论证以规格为准，执行者需同时阅读规格与本计划）。

---

## 全局约束

- **范围**：本计划 = ① 读侧配置 + 定位键入口校验；② 点读服务（PG → 优先级判定 → 归档取回 → 错误分类）；③ 哨兵 → HTTP 表现映射；④ 隔离件与边界回归；⑤ 规格 §8.1 两条端点；⑥ **既有** trace 端点的正文改由点读服务提供（计划 2 造成的回归修复）；⑦ CLI `cmd/trace-body`；⑧ A2 生产形态验收；⑨ 分支级证据重跑。**封块/sealer 进程、systemd unit、`/datalog` bind mount、蓝绿切换、压缩比实测、行数分布核实属计划 4**，本计划**一行都不写**。
- **计划 1 的包已冻结（本计划的第一硬约束）**：`internal/tracearchive/` 共 **16** 个文件、3627 行（复核：`ls internal/tracearchive/ \| wc -l` ⇒ 16；`wc -l internal/tracearchive/*.go \| tail -1` ⇒ 3627），已附 **R11 v3 的 142 条变异证据**（冻结提交 `ea962a70`）。**本计划不得修改其中任何文件的任何字节**（含注释与测试文件），**也不得新增/改名/删除其中任何文件**。R11 证据绑定**文件哈希**（计划 1 的 D16）——改一个字节就断绑定，整套证据作废。**本计划的验收项之一就是 `git diff` 对这条路径必须为空**（见任务 9 步骤 4）。
- **既有能力基线（不可重写）**：读侧的存储原语**已经存在**，本计划**不重新实现**任何 footer/trailer 编解码、任何解压定位、任何 spool 兜底。逐符号清单见下一节「既有能力基线」。**若某执行者发现自己在写 `decodeFooter` / `decodeTrailer` / zstd 解码循环之类的东西，立即停止**——那说明走错了任务。
- **读侧的分层归属（本计划的核心裁定，登记 D54）——读侧落在 control-plane。**
  **仓库明文约束（`AGENTS.md`「约定与注意事项」逐字）**：
  > **gateway 不要直连数据库**——它只通过 Redis snapshot / 控制面内部 API 获取配置。新增需要持久化的能力放控制面，gateway 经 `internal/gateway/binding` 调内部接口。
  ① **本计划选哪一侧：control-plane**（`internal/controlplane/` + `internal/observability/{service,handler}`），**不是**网关。依据三条，缺一不可：
  1. 读侧**必须**查 PG（`request_id` → 定位键 `(dt,inst,part,row)` + `body_missing`）。control-plane 本来就持有 `*gorm.DB`（`cmd/control-plane/main.go:49` 把它交给 `router.Setup`），这条能力是它现有的职责；而 gateway 按 `AGENTS.md` **没有** DB 通道。
  2. 规格 §8.4/C5 明确把**解码内存**（≈ 窗口 512 MiB/并发）算在 **control-plane 的 2 GiB 限额**内 ⇒ 规格自己就把点读放在 control-plane 侧。
  3. 规格 §11.1 的部署矩阵给 **control-plane** 容器挂 `/datalog`（只读），用途逐字是「点读服务需要读正文」；gateway 只需要 `spool` 目录**可写**（且不含产物目录的解压读取需求）。
  ② **为什么不与写入侧对称（即不选「网关直连」）**：写入侧的 `cmd/gateway/main.go` 直连 PG 写 `call_traces` 是**已登记的既成矛盾（计划 2 的 D46，本计划沿用其编号）**。读侧**不得**默认扩大这条偏离——理由不是「对称更整齐」，而是**代价不对称**：写入侧直连是**既有代码的既成事实**（计划 2 只改了写入的字段，未新增 DB 依赖），而读侧**新增**直连会同时新增三件事：gateway 进程内的 PG 连接池与凭据（`AGENTS.md` 明令不做）、gateway 对 `/datalog` 产物目录的**读**权限（§11.1 只给它 spool 可写）、以及 gateway 2 GiB cgroup 内的**解码器内存**（与 §8.4 的归属直接冲突）。⇒ **本计划登记的结论：读侧不构成新增偏离，偏离总数不增加**（新增的是 control-plane 侧的常规职责，不是 gateway 侧的越界）。这条必须在交付报告里**逐字**说明，不得被读成「网关又多了一处直连」。
  ③ **若将来确实需要「网关对外暴露点读」**：走 `AGENTS.md` 指定的合规链路，**不在本计划内**，形态是：
  ```
  入口（将来）：gateway 路由 → internal/gateway/binding 的 HTTP 客户端
                → control-plane 的内部端点 GET /internal/v1/trace-body（本计划任务 5 已交付）
                → TraceBodyService（本计划的读侧）
  鉴权：既有 InternalAuth（X-Internal-Token 常量时间比较，middleware.go:216-241）
        ——由 gateway 的 binding 客户端在请求头带上内部 token，不新建鉴权机制
  延迟影响（如实标注，未实测）：多一跳本机/同宿主 HTTP（生产是 network_mode: host，
        预估亚毫秒级）+ 归档解码本身（大块解压是秒级，规格 D3/R1 已接受 ⇒ 多一跳不是瓶颈）；
        【但必须实测】：本计划的 CLI（任务 7）与领域内直连路径可作对照实验的基线
        ⇒ 若将来要做，延迟实测是必做项，不得凭估算下结论
  ```
  **本计划只交付那条内部端点**（任务 5），**不交付** gateway 侧的 binding 客户端与网关路由——后者取决于是否要把点读暴露给外部调用方（规格 §8.1 的两条端点都在 control-plane 侧，没有要求 gateway 暴露）。
- **不新增迁移、不改 schema**：读侧只**读**计划 2 已加的 6 列（`body_ref_dt` / `body_ref_inst` / `body_ref_part` / `body_ref_row` / `body_missing` / `body_bytes`）与既有的 3 个 jsonb 列。**本计划对 `atlas-migrations/` 零改动**（`git diff` 必须为空）。登记 D52 的漂移处置在本计划**不适用**（因为没有迁移）。
  **⚠️ 若执行者认为必须新增迁移（本计划判定为「不需要」），铁律照旧**：必须走 `atlas-migrations/` + `atlas-db-migration` skill 的实际流程（`atlas migrate diff` 生成、`atlas migrate hash` + `validate`、`atlas.sum` 一并提交），**不得手写 SQL、不得手写时间戳**。**注意 `AGENTS.md` 在此处已过期**：它写「新增 `migrations/NNN_*.sql`（递增序号）」，而仓库实际使用 **`atlas-migrations/<timestamp>_*.sql`**（`AGENTS.md` 自己在「数据库变更」一条里写的是 Atlas）⇒ **以 `atlas-migrations/` + skill 为准**。这条矛盾登记为 **D62**。
- **压缩与解压路径（方案 C 的读侧结论）**：网关内不压缩（方案 C），读侧必须能读**未压缩的 spool 行**——`Reader.GetBody` 的两条路径（产物 `.jsonl.zst` → spool `.jsonl`）由计划 1 冻结实现，**本计划只调用**。**「为计划 4 的压缩产物预留解压路径」在本计划里不需要任何新接口**，理由是：计划 4 的 sealer 必须使用**同一个冻结的 `internal/tracearchive.Sealer`** 产出**同一种**产物（`format_version = 1`，见 `footer.go` 的 `formatVersion`），因此读侧不需要新的分派。**唯一的版本耦合点**是 `decodeFooter` 对未知 `format_version` **响亮拒绝**（`ErrFooterCorrupt`）——若计划 4 提升格式版本，**必须同时**在冻结包侧受控变更 + 重跑 R11 v4，并同步本计划任务 3 的错误映射语义。这条写进交接 **H9**。
- **行号稠密性假设（承重；来自计划 1 与计划 2 的裁定）**：本计划**假定**「有定位键的行 ↔ 该 part 内的物理行序一一对应」。依据是计划 2 的 **D51**：写入侧**有意不使用** `AppendFailed` 标记行，失败行写 NULL 定位键 + `body_missing=true`，且 `Append` 失败**不推进行号**（`writer.go:275-286` 只在两次写入都成功后 `cur.rows++`）⇒ 不存在行号空洞。**但不得反向推断「行号连续」**：全局看，`body_ref_row` 序列**有空洞**（降级行没有键）。因此读侧**只做随机访问**（`(part, row)` 直接定位），**绝不新增按 `body_ref_row` 顺序遍历的代码**（那是计划 1 D51 与计划 2 H5 明文禁止的）。核对 H14。
- **读侧零日期计算（本计划的裁定，登记 E2）**：定位键 `(dt, inst, part, row)` 的 `dt` **一律取自行内已存的 `body_ref_dt` 列**（写入侧算好并落库），读侧只做「查表 + 取回」，**不做任何日期计算、不注入任何日期口径**：
  - **不得**注入 `DayOf` 之类的日期换算函数（原设计在服务选项里注入日期换算函数、并由 router 传入 control-plane 侧的同名符号，本次已按本条**整体删除**——它既没有落点，也与任务 4 的 CHECKS「读侧不自算日期」互斥）；
  - **不得**调用 `time.Now` / `time.Local` / `time.Date` / `FixedZone` / `LoadLocation` 去重算日期；
  - 唯一允许的转换是**把 `*time.Time` 的 date 列值按目录名布局格式化**（`trace.BodyRefDT.Format("2006-01-02")`）——它是**格式化，不是日期换算**，不引入任何时区/日历日口径；
  - 这条同时消解了「同一天口径两处实现」的风险：口径的唯一事实源仍在写入侧（计划 2 的 `DayStart`/`DayOf`），读侧只是**读取它算好的结果**。
  可验证形式：任务 4 的 CHECKS「读侧不自算日期」（静态钉住 `FixedZone|LoadLocation|time.Date(`）＋ 任务 4 的跨日用例（两个 dt 目录各取回自己的行）。
- **两类状态必须用「先判据、后回退」的顺序区分（本计划的核心语义，登记 D55）**：
  - **正文在 jsonb 列**：4 个定位键**全为 NULL** 且 `body_missing = false`。成因是**功能关闭**（`TRACE_BODY_ROOT` 留空 ⇒ 写入侧走既有路径，正文仍进 jsonb）或**构造失败**（`sink == nil`）。**必须**解释为「正文在 jsonb 列」，**不是**「缺失」。
  - **正文从未落盘**：`body_missing = true`（此时定位键**应为**全 NULL，见计划 2 的 A1/A3 用例）。
  - **正文在归档**：4 个定位键**全非 NULL**。
  - **四者之外的形态**（例如「定位键部分非 NULL」或「`body_missing = true` 且定位键非 NULL」）⇒ **500 + 明确诊断**。**绝不静默回退到 jsonb**（会返回空正文并把「不一致」伪装成「就是空的」），**绝不报 404**（会把存储不一致伪装成业务事实——这正是规格 §8.1 反复强调要消灭的一类错误）。
- **错误身份是契约（来自冻结包 `errors.go` 的自述，必须原样遵守）**：调用方一律用 `errors.Is` 分类，对外包装必须 `%w`。
  - `ErrBodyMissing` ⇒ **404** + `body_missing=true`（正文在写入时即失败，从未落盘）；
  - `ErrNotArchived` ⇒ **500** + 诊断（元数据与存储不一致；**覆盖不止「两个文件都不存在」**，还包含**行号越界**、目标偏移未命中行首、spool 第 row 行是残行）；
  - `ErrFooterCorrupt` / `ErrTrailerCorrupt` ⇒ **500** + 诊断「产物损坏」；
  - `ErrPartNotFound` ⇒ **本计划不映射任何状态码**，见 D53（该哨兵**全仓无生产者**）。
  **不得新增哨兵**（规格 §7.1 的隔离件一条与冻结包的注释都明说「读取侧不为它新增分支或哨兵」）。
- **隔离件的命名/发现规则（规格 §7.1 要求本计划定义，登记 D60）**：规则**只作为文档 + 测试 + 运维交接存在，不落成生产分支**。
  ```
  命名：<root>/spool/dt=<dt>/inst=<inst>/part-NNNNNN.jsonl.zst
        （由 writer.go 的 partName(part, quarantineSuffix) 生成，quarantineSuffix = ".jsonl.zst"）
  发现：它【不在】产物目录（产物在 <root>/dt=D/inst=I/），名字【也不是】.jsonl（spool 兜底只认 .jsonl）
        ⇒ Reader.GetBody 的两条精确路径都不命中它 ⇒ 返回 ErrNotArchived
  禁止：把它当产物打开（内容是明文 JSONL，不是 zstd 帧 ⇒ 会给 ErrFooterCorrupt，是【错误诊断】）
  禁止：把它当 spool 读（会给 ErrNotArchived，但路径判断会掩盖真实成因）
  含义：该 part 因写入失败被隔离，其中【已被拒绝行之前】的正文已不能通过正常路径读取
  运维：释放空间后把文件名【改回】part-NNNNNN.jsonl 即可让计划 4 的 sealer 正常封块
        （文件已回退到行边界，产物行数 = 真实完整行数；尾部丢失的行读出来是行号越界，仍是响亮报错）
  永久禁令：计划 4 的 sealer 必须永不封块它（ListPendingSeals 只收 .jsonl 结尾，这是已冻结的规则）
  ```
  本计划的**可验证形式**（任务 4）：在 **spool 目录**放一个内容是明文 JSONL 的 `part-NNNNNN.jsonl.zst`，断言 `GetBody` 返回 **`ErrNotArchived`**（**不是** `ErrFooterCorrupt`、**不是** 200 明文、**不是** 行号越界以外的错误）。
- **对外响应形状统一（本计划裁定，登记 D57 的必要配套）**：归档里的每一行是**一个 JSON 对象**，键序由计划 2 的 `trace_line.go` 固定为 `request_headers` / `request_body` / `response_body`（`omitempty`）。**jsonb 回退路径必须重建同一形状**，否则调用方无法用同一套解析代码处理两个来源。⇒ `TraceBody` 的 JSON 形状就是这三个键。
  **⚠️ 逐字节保证的适用边界（登记 D57）**：规格 G3「正文可原样读回、逐字节等于写入前」**只对归档路径成立**（归档行就是写入时那一行的原字节）。jsonb 回退路径是**重新序列化**（`datatypes.JSON` 的列值 → 三个键的对象），**键序与空白不保证与请求当时的字节相同**——这是 jsonb 存储本身的代价（计划 2 之前的所有行都走这条路）。报告里必须写明，**不得**声称 jsonb 路径逐字节。
- **解码侧内存约束（规格 §8.4 C5/C6，必须接线到配置）**：`ReaderOptions.MaxConcurrent` 默认 **2**（按 600 MB/并发 × 2 GiB 限额估），`ReaderOptions.ZstdWindowBytes` 默认 **512 MiB**（必须与写入侧一致，否则解码失败）。**读 spool 明文路径不占并发槽位**（C6），这是 `Reader` 的既有行为，本计划只做**配置暴露 + 校验**（见任务 1）。
- **鉴权与越权（安全承重，不得省略）**：新增的管理面端点走**既有 `JWTAuth`**，内部端点走**既有 `InternalAuth`**，**不新建鉴权机制**（规格 §2.2 明文）。**并且必须沿用既有 trace 端点的可见性规则**（`internal/observability/handler/usage_handler.go:102-125` 的既有行为）：
  - 提示词正文按 `rbac.CanViewPrompt(c, ownerUserID, isProgramKey)` 裁剪（`internal/orgservice/rbac/prompt_visibility.go:27`）；
  - `/my/*` 形态：`ownOnly=true`，他人记录**按不存在处理**（404，不泄露存在性）；
  - `/usage/scoped/*` 形态：按 `policy.UsagePolicy.QueryScope` 校验管辖范围，范围外**按不存在处理**。
  **正文点读是比对 trace 元数据更强的越权面**（它直接给出提示词原字节）⇒ **任何新端点都必须先做归属判定再做取正文**，判据与既有端点**同一套**，不得各写一套。
- **读侧配置的键名与作用域（本计划新增）**：control-plane 侧新增 viper 键 `trace_body.*`（与网关侧**同名同前缀**，因为两侧读的是**同一个** `TRACE_BODY_ROOT`）。**读侧只读 `trace_body.root`**，**不读**任何写入侧参数（`block_mb` / `flush_interval` / `roll_interval` / `disk_watermark_percent` / `inst`）——它们对读侧**没有消费者**。这条由静态检查（`CHECKS`）钉死。
- **功能关闭的语义（读侧）**：`TRACE_BODY_ROOT` `TrimSpace` 后为空 ⇒ **读侧禁用**。此时：
  - 「定位键全 NULL + `body_missing=false`」**照常**走 jsonb 回退并返回 **200**（这是功能关闭下的**正常**路径，不是错误）；
  - 「定位键全非 NULL」⇒ **503 + 诊断**（元数据指向归档但读侧未配置，属部署错误，需人工介入）；
  - **绝不**因为读侧禁用就把有归档键的行报成 404/500-不是-503。
  理由：读侧禁用**不等于**「正文不存在」。
- **读侧键的存在性 / 空串语义（已核实的行为；2026-10-08 在 HEAD `71766032` 上复核，订正 E6）**——**任务 3 / 任务 5 / 任务 8 的 200/503 判据直接依赖它**，不得按直觉改写：
  - **`SetDefault` 让 `IsSet` 恒真（已核实）**：`internal/controlplane/config/config.go:111-114` 为 `trace_body.root` / `trace_body.read_max_concurrent` / `trace_body.read_zstd_window_mb` 三个键都注册了默认值 ⇒ 生产路径（`config.Load()`）上 `v.IsSet("trace_body.read_max_concurrent")` **恒为 true**，`internal/controlplane/config/trace_body_read.go:97-99` 的「键未出现 ⇒ 默认 2」回落分支在**生产上是死代码**（无害：回落值与默认值同为 2）。它只在测试的裸 `viper.New()` 路径上被执行。**这不改变 E5 的裁定**：显式配置的 0/-1 仍必须被拒（`trace_body_read.go:100` 的 `c < 1`）。
  - **空串环境变量被 vendored viper 跳过（已核实）**：`vendor/github.com/spf13/viper/viper.go:444` 是 `return val, ok && (v.allowEmptyEnv || val != "")`，而 `allowEmptyEnv` 默认 false、仓库未调用 `AllowEmptyEnv`（`internal/controlplane/config/config.go:132-133` 只做 `SetEnvKeyReplacer` + `AutomaticEnv`）⇒ `TRACE_BODY_READ_MAX_CONCURRENT=""` **不覆盖** YAML/默认值（仍落到 2，与 E5 不冲突）。
  - **`TRACE_BODY_ROOT=""` 无法覆盖 YAML 里的 `trace_body.root`（已核实，同属上一条的同一代码事实）** ⇒ **「功能关闭」在生产上只能由 YAML 一侧表达**（缺 `trace_body.root` 键 ⇒ 默认空串，或该键在 YAML 里显式留空；编排里把该变量**注释掉**——正是计划 4 任务 11 的补救动作写法；写成 `TRACE_BODY_ROOT=` 是**无效**的）。**推论（由上述代码事实推出、本轮未实测，标注为推论）**：`TRACE_BODY_ROOT=" "`（纯空白）**能**覆盖（非空 ⇒ 过 `getEnv`）且 `TrimSpace` 后得到禁用态——这是唯一「用环境变量关功能」的形式，但**不得**写进部署文档（易被当笔误），只作排障知识。
  - **对 200/503 的影响**：「读侧禁用」的判据是 `Disabled()` ⟺ `Root == ""`（`trace_body_read.go:56-58`）。若 YAML 里**还留着** `trace_body.root`，则读侧**是启用的**：定位键全 NULL ⇒ 仍 200（jsonb）；定位键全非 NULL ⇒ **走归档**（既不是 503，也不是 jsonb-200）。⇒ **排查「设了空 root 但拿到的不是 503」时，先看 YAML，不要看 `.env`。**
- **证据制度（本仓库 CI **完全不跑** `go test`，`.gitlab-ci.yml` 里只有三处 `CGO_ENABLED=0 go build` ⇒ 变异证据是唯一验证手段）**：沿用计划 1/2 的工具与判据（`tools/trace-mutate/README.md`、`tools/trace-mutate/trace_mutate.py`）。要点：
  1. 变异集合**必须 git-tracked**，本计划的集合命名 `tools/trace-mutate/sets/p3-task-<N>.py`（`p2-task-*` 是计划 2 的命名空间，已占用，**不得复用**）。
  2. 集合必须声明 10 个字段：`NAME` / `TARGET` / `PACKAGES` / `MANIFEST_FILES` / `MANIFEST_DERIVATION` / `SKIP_WHITELIST` / `MUTATIONS` / `CHECKS` / `REQUIREMENTS` / `EXPECTED_GREEN`。
  3. **`PACKAGES` 只列 `./tests/...`（B 层）**：含包内（A 层）包会让某条 `-run` 出现 `[no tests to run]` ⇒ 证据无效。本计划的所有用例**都放 B 层**（`tests/observability/...`、`tests/controlplane/...`），因为读侧服务的公开类型**全部导出**（`internal/` 下，导出不扩大对外 API）。
  4. 每条变异跑完必须核**三断言**：顶层 `=== RUN` 名字集合 == 清单（去掉所有空白后 diff 为空）；顶层 `PASS+FAIL+SKIP` 计数 == 清单条数；**全层级** SKIP == 0。**计数不等即为无效；计数为 0 即为无效**；`[no tests to run]` 绝不能算绿。
  5. **变异必须是「断言红」**：编译失败、panic、30s 超时都**不算**通过。因此本计划**禁止**用「把某个导出符号改名/删掉」这类会让编译失败的变异；变异一律是**放宽或删除某个判定**，让**既有用例的断言**失败。每条变异都必须写明它杀死的**具体顶层测试函数名**。
  6. **表驱动/共享断言环的遮蔽效应**：新增一条守卫时，「整条用例转红」**不能**证明新守卫承重（环里更早的条目会遮蔽）⇒ 必须用**只含该条目的隔离探针**给出两截证据（守卫在位 ⇒ PASS；摘掉 ⇒ FAIL **于该条目**）。计划 1 的 **D15/D36** 已实证过这一点。
  7. **两类证据分开记**：变异（行为）与静态检查（结构不变量）。变异测不到的要求（例如「读侧不读 `TRACE_BODY_INST`」、「读侧无 `quarantine` 分支」、「`grep -c 'internal/tracearchive'` 的改动为 0」）必须有 `CHECKS` 行，写明命令与真实输出。
  8. **证据绑定提交**：工具的头部会打印 `git rev-parse HEAD` / `git status --porcelain`（脏树拒绝运行）/ 工具与集合与清单的 sha256 / 被测文件 sha256 ⇒ 汇报时必须原样引用。
  9. **子例级归因只能靠隔离探针**（工具的「杀死用例」列只到**顶层**用例名，计划 1 D36 的既有规则）。
  10. **名字清单必须先由集合自己声明的 `MANIFEST_FILES` + `MANIFEST_DERIVATION` 生成**：工具的 `--manifest` **不做生成**，文件不存在时直接 `FileNotFoundError`（工作树干净门之前就崩）。本计划的证据目录是 `.superpowers/sdd/2026-09-25-trace-point-read-service/`，每个集合一个清单文件；**任务 9 步骤 1 给出了逐集合的生成命令，任务是先决条件**。清单内容只由测试源码推导（`grep '^func Test'`），因此它随实现完成而稳定，且工具会**实际重跑推导命令**与清单比对（不一致即证据无效）。
  11. **本计划的集合清单（恰好 9 个，与任务 9 的清单逐字一致）**：`p3-task-1` / `p3-task-2` / `p3-task-3` / `p3-task-4` / `p3-task-5` / `p3-task-6` / `p3-task-7` / `p3-task-8` / `p3-task-8-handler`。**本计划不产出 `p3-task-4-handler` 与 `p3-task-5-router`**：任务 4 不覆盖映射行为（产出它必为零覆盖）；任务 5 的权限门目标在 `router.go`，而仓库**不存在**任何 `router.Setup` 的测试包（`grep -rln "router.Setup" tests/` ⇒ 0 命中），造一条只为变异的 DB/Redis/rbac 全栈路由测试包不成比例 ⇒ 该要求改为 CHECKS 行（见任务 5 步骤 5）。
  12. **`CHECKS` 的写法硬要求（工具的 `print_checks` 会直接 `open(file)` 并对全文 `re.search(pattern)`）**：① `file` **必须是真实存在的具体文件**，**绝不能是目录**（写成 `internal/tracearchive` 会抛 `IsADirectoryError`，而调用点在跑变异**之前** ⇒ 整轮零产出：原稿 6 个集合就是这样废掉的）；② pattern/expect 必须**确实可被违反** —— 恒 FAIL（例如在必须引用 `gorm.ErrRecordNotFound` 的文件上写 `gorm\.` + `absent`）与恒 PASS（`r"."` + `absent`）都不合格；③ 断言文档/注释类文本时，正则要避开 `fmt.Fprintln` 这类**包含 `Print` 子串**的标识符（否则用法文本里的 `--dsn` 会被误判为「打印 DSN」）。
- **本计划登记的规格缺陷（编号自成一套，从 D53 起；计划 1 用到 D41、计划 2 用到 D52）**：见文末「规格缺陷登记」。**执行者不得因为「规格这么写」而按缺陷描述实现**——每条缺陷都给了本计划的裁定。

---

## 既有能力基线（计划 1 冻结包盘点，2026-10-08 在 `main@2cf14cb9` 上只读核实）

> **本节是计划的输入契约，不是交付物。** 盘点方式：`grep -rn "^func \|^type \|^var \|^const " internal/tracearchive/*.go`（去 `_test.go`）逐符号读定义 + 读 `reader.go` 全文 360 行。**结论：读侧原语已完整实现，本计划不重写任何一条。**

### A. 已实现且本计划**必须复用**的导出符号

| 文件 | 符号（导出） | 语义（读侧用途） | 本计划怎么用 |
|---|---|---|---|
| `errors.go` | `ErrPartNotFound` / `ErrBodyMissing` / `ErrNotArchived` | 错误身份契约，`errors.Is` 分类 | 任务 3 映射状态码（`ErrPartNotFound` 见 D53） |
| `errors.go` | `ErrFooterCorrupt`(footer.go) / `ErrTrailerCorrupt`(index.go) | 产物损坏 | 任务 3 映射 500 + 诊断 |
| `errors.go` | `ErrPartExists` / `ErrRowCountMismatch` / `ErrTooManyRows` / `ErrSealFailed` | **封块侧**哨兵 | **读侧不使用**（属计划 4） |
| `reader.go` | `ReaderOptions{Root, ZstdWindowBytes, MaxConcurrent}` | 点读器配置 | 任务 1 从配置装配 |
| `reader.go` | `NewReader(ReaderOptions) (*Reader, error)` | 构造（`Root` 空 ⇒ 报错） | 任务 2 构造并注入服务 |
| `reader.go` | `(*Reader).GetBody(ctx, Ref) ([]byte, error)` | **点读闭环本体**：产物 → spool，逐字节、不含换行；含 R10 三项 | **任务 2 唯一调用点** |
| `reader.go` | `(*Reader).Close() error` | 释放（当前无长驻资源） | 任务 2 / 任务 9 收尾 |
| `reader.go`（未导出） | `maxTrailerBytes = 64<<20`、`maxTrailerEntries = 64<<20/8 = 8,388,608` | 读侧内存预算两半 | **不可直接引用**（未导出）⇒ 只在测试里用**行为**断言（见任务 4/D61） |
| `ref.go` | `Ref{DT, Inst, Part, Row}` / `Ref.String()` | 定位键与诊断文本 | 任务 2 构造、任务 3 诊断 |
| `ref.go` | `Paths` / `NewPaths(root)` / `PartDir` / `PartFile` / `SpoolDir` / `SpoolFile` | 目录布局（规格 §5.1） | 任务 2 拼路径供诊断；任务 4 布置隔离件 |
| `footer.go` | `FooterSize = 32` | 产物最小合法尺寸 | 任务 2 前置检查（`size < FooterSize`） |
| `sealer.go` | `SealerOptions` / `NewSealer` / `(*Sealer).Seal` / `(*Sealer).ListPendingSeals` | **封块器（已存在）** | **本计划不使用**（计划 4） |
| `writer.go` | `Writer` / `NewWriter` / `Append` / `PendingSeals` / `MarkSealed` / `Close` | 写入原语 | **本计划不使用**（计划 2 已用） |

**已实现的规格要求（逐条核对，不是推测）：**

- **规格 §8.2 读取算法第 3 步（`frame_sha256[:8]` 校验）**：`reader.go:274-291` 的 `verifyFrameSHA`，在 `readFromPart` 的 `:211` 调用，范围**限死在 `[0, TrailerOffset)`**（`:281` 的 `io.LimitReader`），不符返回 `ErrFooterCorrupt`。**R10 第 2 项已落地。**
- **规格 §8.2 读取算法第 4 步（spool 残行守卫）**：`reader.go:314-349` 的 `readFromSpool`，在 `:329` 判 `errors.Is(readErr, io.EOF)` 且有非空字节 ⇒ `ErrNotArchived`。**R10 第 1 项已落地。**（有一条**已登记的与规格文字的差异**：拒绝范围是**被请求的那一行**而不是整份 spool，理由写在 `:306-313`，本计划**沿用**并写进任务 4 的断言。）
- **标记行 ⇒ `ErrBodyMissing`（产物与 spool 两条路径）**：`reader.go:251`（产物路径）与 `:337`（spool 路径）都调 `isFailedRowMarker`（`:358-360`，与 `writer.go:298` 的 `failedRowMarker` 同包共用一份定义）。**R10 第 3 项已落地。**
- **行号越界必须在读 footer 后立即拒绝**：`reader.go:182-184`（`ref.Row < 0 || ref.Row >= int(ft.RowCount)` ⇒ `ErrNotArchived`，**不是** 404）。
- **空间硬约束 C1/C2/C3/C4**：`readFromPart` 流式解压 + 命中即停（`:241-266`），无临时文件、无块缓存；`readFromSpool` 顺序读（C4）。C5 由 `sem` 并发槽位实现（`:146-151`），**spool 路径不占槽位**（C6，`:132` 直调 `readFromSpool`）。
- **并发槽位超时/取消**：`:149-151` 的 `case <-ctx.Done()` ⇒ 用例 `TestReaderHonorsContextCancelWhileWaitingForSlot` 已存在。
- **读侧内存闸门（F1 补入）**：`maxTrailerBytes`（`reader.go:193-195`）+ `maxTrailerEntries`（`index.go` 的 `decodeTrailer` 内，**在 `make` 之前**）⇒ 用例 `TestReaderRejectsOversizedTrailerLen`、`TestReaderRejectsTrailerEntryCountOverBound` 已存在。

### B. 缺口清单（**确实还不存在，必须由本计划新增**）

| # | 缺口 | 证据（怎么核实它不存在） | 落在哪个任务 |
|---|---|---|---|
| G-1 | **读侧配置**：control-plane 侧没有任何 `trace_body.*` / `TRACE_BODY_ROOT` 的消费者 | 复核命令（2026-10-08 实跑）：`grep -rn "TRACE_BODY\|TraceBody\|trace_body" --include=*.go internal/controlplane/ internal/observability/ cmd/control-plane/ \| wc -l` ⇒ **0**（即「control-plane 侧 0 命中」）。**注意**：非 0 命中确实存在，但全在 `internal/gateway/`、`cmd/gateway/`、`tests/gateway/` —— 原表述「`internal/gateway/` 之外 0 命中」**是错的**，已按本条订正 | 任务 1 |
| G-2 | **`request_id` → 正文的服务**：没有把 PG 行与 `Reader` 接起来的类型 | `grep -rn "tracearchive" internal/observability/ internal/controlplane/` ⇒ **0 命中**（全仓 `internal/tracearchive` 的引用只在 `internal/tracearchive` 自身与 `internal/gateway/observability/`） | 任务 2 |
| G-3 | **优先级判定**：没有「定位键全 NULL + `body_missing=false` ⇒ jsonb」这条规则 | 同上 0 命中；规格 §8.2 的读取算法**没有这一步**（登记 D55） | 任务 2 |
| G-4 | **哨兵 → HTTP 表现映射**：没有 `errors.Is` 分类到状态码的代码 | `grep -rn "ErrNotArchived\|ErrBodyMissing\|ErrFooterCorrupt" internal/observability/ internal/controlplane/` ⇒ **0 命中** | 任务 3 |
| G-5 | **规格 §8.1 的两条端点** | `grep -n "trace-body\|/:request_id/body" internal/controlplane/router/router.go` ⇒ **0 命中** | 任务 5 |
| G-6 | **既有 trace 端点的归档感知** | `usage_handler.go:137`（不是 `:134`）的 `h.db.First(&trace, …)` 直接返回模型；**当写侧功能开启（`TRACE_BODY_ROOT` 非空）后**，新行的 jsonb 列恒 NULL ⇒ 管理面对**所有新 trace** 显示空正文。**这是一条尚未暴露的缺陷，不是当下 `main` 上的回归**（依据见任务 6 的「为什么必须有这一步」） | 任务 6 |
| G-7 | **CLI `cmd/trace-body`** | `ls cmd/` = `bedrock_test` / `control-plane` / `gateway` / `opencode-helper` / `report-gen` / `trace-calib`；`trace-calib` 是 **zstd 标定工具**（读 `-in` 样本），**不是点读工具** | 任务 7 |
| G-8 | **`GetBody` 的输入校验（计划 1 的 N3，仍未修）** | `reader.go:121-140` 直接使用 `ref.DT` / `ref.Inst` 拼目录名（经 `Paths.PartFile`），**无任何校验**；而 `writer.go` 的 `Append`（`:243`）与 `sealer.go` 的 `Seal`（`:154-159`）都有 `validateDT`/`validateInst`。计划 1 的 N3 明文写着「`GetBody` 的同类缺口仍未修」 | **任务 1（调用侧收口）+ D59**（不改冻结包） |

**G-8 的处置（本计划最重要的取舍之一，登记 D59）**：计划 1 的 **N3 要求计划 3 收口 `GetBody` 的输入校验**，而本计划的**第一硬约束禁止修改冻结包** ⇒ 两条同时满足的唯一方式是：**在调用侧（任务 1 的 `ValidateRef`）做等价校验，`GetBody` 保持原样**。代价是**同一判据两处实现**（`validateDT`/`validateInst` 在冻结包内，`ValidateRef` 在调用侧），**且导出的 `GetBody` 本身仍可被未来调用方误用**。这条残余必须**如实写进交付报告**，并把「导出版本校验（`ValidateRef`）或让 `GetBody` 自校验」作为**受控变更 + R11 v4** 的候选交给发起者决策——**不得**在本计划里偷偷改冻结包。

### C. A2 在计划 1 里已经被验证到什么程度（**引用既有证据，不重头验**）

| 计划 1 的交付 | 证据 | 覆盖了 A2 的哪一部分 |
|---|---|---|
| `reader.go` + `index.go` + `footer.go` 全套读侧原语 | 计划 1 Task 7 的 B 层 `tests/tracearchive/reader_test.go` + A 层 `reader_internal_test.go` | **索引正确性**：逐字节取回、越界分类、footer 篡改、`maxTrailerBytes`/`maxTrailerEntries` |
| 端到端往返 | `tests/tracearchive/roundtrip_test.go` 的 `TestEndToEndRoundTripByteExact`（**规格 A1 的缩小版**：写 N 行 → 封块 → 随机抽 K 行读回 → 逐字节） | **「封块产物可点读」的核心命题**（库层） |
| footer 定长字节布局 | `TestFooterGoldenBytes` | 格式与规格 §5.2 逐字节一致 |
| 空间约束 | `tests/tracearchive/constraints_test.go`（读取 1000 次后目录占用增量 == 0） | **规格 A4** |
| **变异证据（R11 v3，冻结 HEAD `ea962a70`）** | `tools/trace-mutate/sets/task-7.py` **32 条** + `task-8-reader.py` **6 条**（均 git-tracked）；总计 142 条 = 130 断言红 + 9 崩溃/超时红 + 3 允许绿 + 0 未登记绿 + 0 无效 | 读侧守卫（残行、`frame_sha256`、标记行、越界、上界）**逐条有「摘掉就红」的证据** |

**结论（写进任务 8，必须逐字保留）**：**A2 的「格式与索引正确性」在库层已被计划 1 覆盖**，本计划的 A2 **不是**从零重验，而是 = **引用既有证据** + **补生产形态的缺口**。生产形态缺口的**准确清单**（本计划任务 8 要补的，别多补也别少补）：

1. `request_id` → PG 行 → 定位键 → `GetBody` → **逐字节**的**完整链路**（库层只验过「直接构造 `Ref`」）；
2. **优先级判定**的四种状态（jsonb / `body_missing` / 归档 / 不一致）在同一批数据上互不混淆；
3. **索引正确性在跨 part / 跨 `dt` / 行号越界**三种边界上的对外表现；
4. **隔离件形态**下返回 `ErrNotArchived`（库层**没有**这个形态的用例——`reader_test.go` 不含隔离件）；
5. **HTTP 表现**（状态码 + 诊断体）与库层哨兵的对应关系。

### D. 冻结边界（**本计划不得触碰**，由任务 9 用 `git diff` 验证）

```bash
# 本计划合入后，这三条必须为「空 / 0」
git diff --stat <base>..HEAD -- internal/tracearchive/        # 必须为空
git diff --stat <base>..HEAD -- atlas-migrations/             # 必须为空
git diff --stat <base>..HEAD -- go.mod go.sum                 # 必须为空（不新增依赖）
```

**并且：`tools/trace-mutate/sets/task-*.py` 与 `p2-task-*.py` 一个字节都不许改**（它们绑定计划 1/2 的证据；本计划的新集合一律叫 `p3-task-*.py`）。

---

## 文件结构

| 文件 | 职责 | 规模估计 |
|---|---|---|
| `internal/controlplane/config/trace_body_read.go` | **新建**：`TraceBodyReadConfig` + `LoadTraceBodyReadConfig` + `ValidateRef`（N3 调用侧收口） | ~170 行 |
| `internal/controlplane/config/config.go` | **修改**：新增 `Config.TraceBodyRead` 字段 + `trace_body.root` / `trace_body.read_max_concurrent` / `trace_body.read_zstd_window_mb` 三个默认值 | +20 行 |
| `internal/observability/service/trace_body_service.go` | **新建**：`TraceBodyService`（PG 查询 → 优先级判定 → 归档取回 / jsonb 回退 → 错误分类）+ `TraceBody` 响应形状 + `tracearchiveReader` 窄接口 | ~260 行 |
| `internal/observability/handler/trace_body_handler.go` | **新建**：`TraceBodyHandler`（两条端点的 gin handler）。**不含 handler 层哨兵** —— 原稿的 `ErrTraceBodyRecordNotFound` 等**已作废**（全局约束逐字「**不得新增哨兵**」，见 `:61`；全仓无消费者，新增即死代码；404/403/400 由 handler 直接出响应。见 **E35**） | ~180 行 |
| `internal/observability/handler/trace_body_error.go` | **新建**：`MapTraceBodyError(err) (status int, code string, message string)`（纯函数，最易测） | ~110 行 |
| `internal/observability/handler/usage_handler.go` | **修改**：`trace()` / `ScopedTrace()` 在 jsonb 为 NULL 且有定位键时，用 `TraceBodyService` 填充正文（回归修复） | +70/-10 行 |
| `internal/controlplane/router/router.go` | **修改**：注册 `GET /api/v1/trace/:request_id/body`（JWTAuth 组）与 `GET /internal/v1/trace-body`（InternalAuth 组）；构造并注入 `TraceBodyService` / `TraceBodyHandler` | +35 行 |
| `cmd/control-plane/main.go` | **修改**（如需）：构造 `Reader` 并交给 `router.Setup`（若选择在 `Setup` 内构造，则此文件零改动——**优先后者**，见任务 5） | 0~25 行 |
| `cmd/trace-body/main.go` | **新建**：运维与验收 CLI（同一 service，只读） | ~190 行 |
| `tests/controlplane/config/trace_body_read_test.go` | **B 层**：默认值 / env 覆盖 / `ValidateRef` 的全部拒绝路径 | ~230 行 |
| `tests/observability/service/trace_body_service_test.go` | **B 层**：四种状态的优先级、jsonb 回退、错误分类 | ~300 行 |
| `tests/observability/service/trace_body_quarantine_test.go` | **B 层**：隔离件形态、spool 兜底、跨日/跨 part/越界边界 | ~240 行 |
| `tests/observability/handler/trace_body_error_test.go` | **B 层**：`MapTraceBodyError` 的表驱动映射 | ~150 行 |
| `tests/observability/handler/trace_body_handler_test.go` | **B 层**：端点鉴权/越权/状态码（`httptest` + 内存 sqlite） | ~280 行 |
| `tests/observability/handler/usage_trace_archive_test.go` | **B 层**：既有端点的归档感知与 RBAC 回归 | ~220 行 |
| `tests/observability/service/trace_body_acceptance_test.go` | **B 层**：**A2 生产形态往返 + 索引正确** | ~260 行 |
| `tools/trace-mutate/sets/p3-task-1.py` … `p3-task-8.py` + `p3-task-8-handler.py` | **新建**（git-tracked）：各任务的变异集合（共 **9** 个；清单与拆分理由见全局约束第 11 条） | 每个 ~120~260 行 |

**每个文件单一职责。** `trace_body_error.go` 是纯函数（无 IO、无 DB，最容易做变异证据）；`trace_body_service.go` 只做「查库 + 判定 + 取正文」，**不碰 HTTP**；`trace_body_handler.go` 只做「HTTP 入参出参 + 鉴权上下文」，**不碰文件系统**；`usage_handler.go` 的改动只做「正文来源替换」，**不改任何既有鉴权/裁剪逻辑**。**不要把三件事合并**——它们的失败面完全不同（映射 = 纯函数；服务 = 数据一致性；handler = 协议与越权）。

---

### Task 1：读侧配置（`TraceBodyReadConfig`）与定位键入口校验（N3 调用侧收口）

**文件：**
- 新建：`internal/controlplane/config/trace_body_read.go`
- 修改：`internal/controlplane/config/config.go`（新增字段 + 3 个默认值）
- 测试：`tests/controlplane/config/trace_body_read_test.go`（B 层）

**接口：**
- 依赖输入：无（本计划第一个任务）
- 对外产出：
  ```go
  // TraceBodyReadConfig 是读侧（control-plane）的点读配置。
  // 只覆盖读侧需要的东西：根目录、解码并发、解码窗口。
  // 【不包含】写入侧参数（block_mb / flush_interval / roll_interval /
  // disk_watermark_percent / inst）——它们对读侧没有消费者，由 CHECKS 钉死。
  type TraceBodyReadConfig struct {
      // Root 为空 ⇒ 读侧禁用（见全局约束「功能关闭的语义（读侧）」）。
      // 非空时必须 IsAbs 且 Clean != "/"（与网关侧同一判据，理由同计划 2）。
      Root string
      // MaxConcurrent 解码并发上限（规格 §8.4 C5）；默认 2。范围 1..16。
      MaxConcurrent int
      // ZstdWindowBytes 解码窗口，必须 >= 写入侧窗口（默认 512 MiB），
      // 否则解码失败。默认 512 << 20。
      ZstdWindowBytes int
  }

  // LoadTraceBodyReadConfig 从 viper 装配并校验。
  // 【订正 E4】返回 (TraceBodyReadConfig, error)：原稿写成单返回值，与本节
  // :289（用例 TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot 的注释
  //       「相对路径与 "/" 都必须让 Load 返回 error」）及变异杀手
  // m02/m03/m09 矛盾。调用侧按**网关侧同形先例**
  // （internal/gateway/config/config.go:198-206：`panic(fmt.Errorf("trace_body 配置非法: %w", err))`）
  // 处理 —— config.Load() 的签名不返回 error，非法配置在启动时响亮失败。
  func LoadTraceBodyReadConfig(v *viper.Viper) (TraceBodyReadConfig, error)

  // ValidateRef 校验定位键的四个分量（计划 1 的 N3：GetBody 自身不校验，
  // 冻结包不可改，故在调用侧收口）。任何不合法都必须返回 error 且
  // 【不调用 GetBody】——校验先于任何文件系统访问。
  func ValidateRef(ref ta.Ref) error

  // Disabled 报告读侧是否禁用（Root 为空）。调用方据此选择 200/503。
  func (c TraceBodyReadConfig) Disabled() bool
  ```
  **`ValidateRef` 的判据（逐条，必须与冻结包内 `validateDT`/`validateInst` 语义等价）：**
  ```
  DT   : time.Parse("2006-01-02", DT) 必须成功（挡住空串、长度不符、未补零、非法日期，
         以及含 "/" 的穿越向量 "2026-09-24/../.."）
  Inst : 必须 ∈ {"blue", "green"}（白名单，与写入侧同一枚举）
  Part : 必须 >= 0 且 <= 999999（partName 用 %06d，前置约束是 [0,999999]）
  Row  : 必须 >= 0
  ```
  **⚠️ 为什么 Part 上界是 999999**：`ref.go` 的 `partName` 文档明写「前置约束：seq ∈ [0, 999999]」；越界会产出 7 位文件名，而 `parsePartSeq` 拒绝它。把上界放在调用侧可以让错误**在拼路径之前**发生。
  **⚠️ 校验顺序必须在任何 IO 之前**：与 `Seal` 的入口校验同一理由（计划 1 的 D28 实测：畸形输入会产生越出 `<root>` 的产物）。**非法 `Ref` 不得让任何 `os.Stat` 发生**——由变异 `m05` 的隔离探针固定。

- [ ] **步骤 1：写失败的测试**

先建 `tests/controlplane/config/trace_body_read_test.go`（B 层，`package config_test`）。**核心用例（每条都必须能独立转红）：**

```go
// TestTraceBodyReadConfigDefaults —— 默认值：Root 空 ⇒ 禁用；并发 2；窗口 512 MiB。
func TestTraceBodyReadConfigDefaults(t *testing.T) {
    v := viper.New()
    // 【订正 E4 的配套】原稿这里写成单值赋值 `cfg := LoadTraceBodyReadConfig(v)`，
    // 与 :289（用例 TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot）「Load 必须返回 error」自相矛盾。
    cfg, err := cpconfig.LoadTraceBodyReadConfig(v)
    require.NoError(t, err)
    require.Empty(t, cfg.Root)
    require.True(t, cfg.Disabled(), "Root 留空必须等于读侧禁用（默认值，安全）")
    require.Equal(t, 2, cfg.MaxConcurrent, "规格 §8.4 C5 的默认并发是 2")
    require.Equal(t, 512<<20, cfg.ZstdWindowBytes, "默认窗口必须与写入侧一致（512 MiB）")
}

// TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot
//   —— 相对路径与 "/" 都必须让 Load 返回 error（与网关侧同一判据）。
// TestTraceBodyReadConfigWhitespaceRootIsDisabled
//   —— 全空白 Root 必须与未设置同义（Disabled），不是「启用了一个空白路径」。
// TestTraceBodyReadConfigMaxConcurrentBounds
//   —— 0 / -1 / 17 都必须被拒（0 会让 sem 容量为 0 ⇒ 死锁；17 会撑爆 2 GiB）。
// TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide
//   —— 小于 512 MiB 必须被拒并【在错误里带上期望下限】：
//      窗口小于写入侧时 zstd 解码会失败，那会把「配置错」伪装成「产物损坏」。
func TestValidateRefRejectsIllegalComponents(t *testing.T) {
    // 表驱动：【每个条目一个独立子例】，注释里点名它对应的变异编号。
    // 关键条目（不可省）：
    //   dt   : "", "2026-9-24"（未补零）, "20260924", "2026-13-01",
    //          "2026-02-30", "2026-09-24/../.."（真穿越向量）, "../x"
    //   inst : "", "red", "Blue"（大小写）, "../../green", "blue/"
    //   part : -1, 1000000
    //   row  : -1
    // 合法条目（反向断言，防止「一律拒绝」的假实现）：
    //   {DT:"2026-09-24", Inst:"blue",  Part:0,       Row:0}
    //   {DT:"2026-12-31", Inst:"green", Part:999999,  Row:8388607}
}

// TestValidateRefDoesNoFilesystemWork —— 关键：校验必须是纯内存判定。
// 用一个【不存在】的 Root 也能通过 ValidateRef（它不接触文件系统）。
// 这条是 m05 隔离探针的载体：摘掉校验后，非法的 dt 会拼出 <root> 之外的路径。
```

- [ ] **步骤 2：运行测试确认失败**

运行：`go test ./tests/controlplane/config/ -run 'TestTraceBodyReadConfig|TestValidateRef' -v`
预期：**编译失败**（`cpconfig.LoadTraceBodyReadConfig` / `cpconfig.ValidateRef` 尚未存在）。这是 TDD 的第一步，编译失败是**预期**的（不是「断言红」，此处允许）。

- [ ] **步骤 3：实现配置与校验**

`internal/controlplane/config/trace_body_read.go` 的要点（伪代码，**不是**可直接抄的实现）：

```go
// 唯一入口：LoadTraceBodyReadConfig。
// 顺序（每一步都要有对应用例）：
//   1. root := strings.TrimSpace(v.GetString("trace_body.root"))
//      空 ⇒ 直接返回 {Root:"", 默认并发/窗口}，Disabled()==true（不报错：禁用是合法状态）
//   2. 非空 ⇒ filepath.IsAbs(root) 必须成立；filepath.Clean(root) != "/"
//      否则 return error（与网关侧同判据；"Clean == /" 会让 spool 建在宿主根）
//   3. 并发：v.GetInt("trace_body.read_max_concurrent")；【订正 E5】键**未出现** ⇒ 默认 2；
//      显式配置的 0/-1 ⇒ 报错（0 会让 sem 容量为 0 ⇒ 等待槽位的请求永久阻塞）；
//      范围校验 1..16（>16 报错）。原稿写的「<=0 ⇒ 2」与本节用例
//      「0 / -1 / 17 都必须被拒」矛盾。
//   4. 窗口：v.GetInt("trace_body.read_zstd_window_mb")；<=0 ⇒ 512；单位 MiB
//      必须 >= 512（写入侧窗口），否则 error 且错误文本含「必须 >= 512 MiB」
//
// ValidateRef：
//   1. time.Parse("2006-01-02", ref.DT)（dateLayout 常量；与冻结包同一布局字符串）
//   2. ref.Inst ∈ {"blue","green"}
//   3. 0 <= ref.Part <= 999999
//   4. ref.Row >= 0
//   全部是纯内存判定；顺序固定（dt → inst → part → row），
//   这样隔离探针能按错误文本定位到具体是哪一条承重。
```

`internal/controlplane/config/config.go` 的改动（**三行默认值 + 一个字段**）：

```go
// 默认值（放在既有 v.SetDefault 序列里，与其它键同一风格）
v.SetDefault("trace_body.root", "")                    // 留空 = 读侧禁用
v.SetDefault("trace_body.read_max_concurrent", 2)      // 规格 §8.4 C5
v.SetDefault("trace_body.read_zstd_window_mb", 512)    // 必须与写入侧一致
```

并在 `Config` 结构体里加 `TraceBodyRead TraceBodyReadConfig`（**注意**：`Load()` 里赋值必须在 viper 设置完默认值之后）。

- [ ] **步骤 4：运行测试确认通过**

运行：`go test ./tests/controlplane/config/ -run 'TestTraceBodyReadConfig|TestValidateRef' -v`
预期：**PASS**，且日志里每条子例都被执行（子例可见性是隔离探针的前提）。
再跑：`go build ./...`

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p3-task-1.py`。`TARGET = "internal/controlplane/config/trace_body_read.go"`，`PACKAGES = ["./tests/controlplane/config/"]`，`MANIFEST_FILES = ["tests/controlplane/config/trace_body_read_test.go"]`，`MANIFEST_DERIVATION = "grep -h '^func Test' tests/controlplane/config/trace_body_read_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"`。

**变异条目（每条都必须红成「断言红」，并在注释里点名杀死的顶层测试函数）：**

```python
MUTATIONS = {}

# m01 禁用语义丢失：Root 留空也当成「启用了一个空路径」⇒ 不再 Disabled
#     杀手：TestTraceBodyReadConfigDefaults（断言 Disabled()==true 那一条）
MUTATIONS["m01_空白root不再视为禁用"] = [
    ("sub", 'if root == "" {', 'if false {'),
]
# m02 root 绝对路径校验被摘掉 ⇒ 相对路径被接受
#     杀手：TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot
MUTATIONS["m02_不校验root必须绝对"] = [
    ("sub", "if !filepath.IsAbs(root) {", "if false {"),
]
# m03 root == "/" 的第二道判据被摘掉
#     杀手：同上（该用例的 "/" 子例）
MUTATIONS["m03_不拒绝文件系统根"] = [
    ("sub", 'if filepath.Clean(root) == "/" {', "if false {"),
]
# m04 并发上界被放宽到不设限 ⇒ 17 被接受（会撑爆 2 GiB）
#     杀手：TestTraceBodyReadConfigMaxConcurrentBounds
MUTATIONS["m04_并发上界放宽"] = [
    ("sub", "if c > traceBodyReadMaxConcurrent {", "if false {"),
]
# m05 【隔离探针载体】ValidateRef 的 dt 校验被摘掉
#     杀手：TestValidateRefRejectsIllegalComponents 的 dt 子例（不是 inst 子例）
#     隔离探针：只摘这一条 ⇒ 必须 FAIL 于 dt 类子例，且 inst/part/row 子例仍绿
MUTATIONS["m05_不校验dt"] = [
    ("sub", "if err := validateReadDT(ref.DT); err != nil {", "if false {"),
]
# m06 inst 白名单被放宽成「非空即可」
#     杀手：TestValidateRefRejectsIllegalComponents 的 inst 子例（含 "../../green"）
MUTATIONS["m06_inst白名单放宽为非空"] = [
    ("sub", "if ref.Inst != instBlue && ref.Inst != instGreen {", "if ref.Inst == \"\" {"),
]
# m07 part 上界 999999 被摘掉
#     杀手：TestValidateRefRejectsIllegalComponents 的 part=1000000 子例
MUTATIONS["m07_不校验part上界"] = [
    ("sub", "ref.Part > tracePartSeqMax", "false"),
]
# m08 row 非负校验被摘掉
#     杀手：TestValidateRefRejectsIllegalComponents 的 row=-1 子例
MUTATIONS["m08_不校验row非负"] = [
    ("sub", "if ref.Row < 0 {", "if false {"),
]
# m09 窗口下限被摘掉 ⇒ 256 MiB 被接受（解码会失败，把配置错伪装成产物损坏）
#     杀手：TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide
MUTATIONS["m09_不校验窗口下限"] = [
    ("sub", "if mb < traceBodyWriteSideWindowMB {", "if false {"),
]
```

**静态检查行（变异测不到的要求）：**

```python
CHECKS = [
  # 读侧不得读写入侧参数（规格 §11.1 把这些参数划给网关；读侧无消费者）
  {"name": "读侧不读写入侧参数", "file": "internal/controlplane/config/trace_body_read.go",
   "pattern": r"TRACE_BODY_(BLOCK_MB|FLUSH_INTERVAL|ROLL_INTERVAL|DISK_WATERMARK_PERCENT|INST)|trace_body\.(block_mb|flush_interval|roll_interval|disk_watermark_percent|inst)",
   "expect": "absent",
   "why": "读侧只读 root + 解码参数；写入侧参数对读侧没有消费者，读了会漂移出「同名两个数」"},
  # 默认值必须留在 config.go（与仓库既有默认值同一处，便于审计）
  {"name": "默认值在config.go", "file": "internal/controlplane/config/config.go",
   "pattern": r'v\.SetDefault\("trace_body\.root", ""\)', "expect": "present",
   "why": "「留空 = 禁用」是默认值，变异测不到默认值 ⇒ 静态钉住"},
  # 冻结包复用点未被改（**必须指向一个真实存在的具体文件**：工具 print_checks 会直接
  # open(path)，`internal/tracearchive` 是目录 ⇒ IsADirectoryError，6 个集合会零产出）
  {"name": "冻结包GetBody签名未变", "file": "internal/tracearchive/reader.go",
   "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
   "expect": "present",
   "why": "任务 2 的窄接口 tracearchiveReader 依赖这一方法集；签名若变则接线失效。**「整目录零改动」的权威判据是任务 9 的 git diff**——本条只能钉住这个复用点（另见全局约束与 D59）"},
]
```

**映射表（`REQUIREMENTS`）**：每条要求 → 变异/检查，**有缺口（有要求无条目）整轮失败**：
`禁用默认值` → m01/CHECKS#2；`root 取值收窄` → m02/m03；**`并发上界` → m04（**只覆盖 `>16` 的一侧**）**；`dt 校验（N3）` → m05；`inst 白名单（N3）` → m06；`part/row 界（N3）` → m07/m08；`窗口下限` → m09；`不读写入侧参数` → CHECKS#1。

**⚠️ 本映射表的两处已知缺口（诚实登记；订正 E7）——不得把它读成「左列要求已被全覆盖」**；本轮处置一律是「只订正措辞 / 登记，不新增变异」：
1. **`c < 1`（显式 0/-1）目前只有测试承重、没有变异承重。** 守卫在 `trace_body_read.go:100`；集合 `tools/trace-mutate/sets/p3-task-1.py:179` 的 `REQUIREMENTS` note 已如实写明「0/-1 由 `c < 1` 守卫覆盖，本集合无对应变异（不在计划的 9 条内）」。**额外的一层遮蔽效应**：`TestTraceBodyReadConfigMaxConcurrentBounds` 是**共享断言环**（`bad := {-1, 0, 17, 64}` 同一个循环），m04 一旦被摘掉，整条用例会被 17/64 子例转红 ⇒ **从「整条红」里分辨不出 0/-1 子例是否承重**（全局约束第 6 条要求的就是这种隔离探针，此处没有）。**不得**据此新增 m10：`p3-task-1.py` 已随 HEAD `71766032` 提交，其变异权威表绑定该提交，动集合就必须重跑并重记绑定（属**下一轮**）。
2. **环境变量覆盖键名的用例没有任何变异/CHECKS 承重。** `tests/controlplane/config/trace_body_read_test.go:123-138` 的 `TestTraceBodyReadConfigEnvOverridesReadKeys` 是本轮新增的**偏离用例**（本计划任务 1 的用例清单里原本没有它），它固定的是「viper 的 `.` → `_` 键名（`TRACE_BODY_ROOT` / `TRACE_BODY_READ_MAX_CONCURRENT` / `TRACE_BODY_READ_ZSTD_WINDOW_MB`）与网关侧同名同前缀」。**键名漂移只由它自己发现**（生产侧的 `SetEnvKeyReplacer` 是既有代码，没有第二个消费者）⇒ 它的证据等级**低于**同包其它用例：它**不计入**任何一条 `REQUIREMENTS` 条目，报告里**不得**写成「有变异/CHECKS 承重」。**同样不得**为它新增变异（理由同上）；若要把它升级为承重证据，须在下一轮连同 `p3-task-1.py` 一起重做，并在集合文件头写明改动。

- [ ] **步骤 6：跑变异集合**

```bash
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest .superpowers/sdd/2026-09-25-trace-point-read-service/p3-task-1-names.txt \
  --mutation-set tools/trace-mutate/sets/p3-task-1.py
```
预期：9 条**全部断言红**、三断言过、无未登记绿。**若某条报「定位到 0 处或多处」**：说明实现与计划给的目标串不一致 ⇒ **修正变异字符串**（不要改测试），并在集合文件头**如实写明改了哪一条与为什么**（计划 2 的 `p2-task-2.py` 文件头是范例）。

- [ ] **步骤 7：提交**

```bash
git add internal/controlplane/config/trace_body_read.go \
        internal/controlplane/config/config.go \
        tests/controlplane/config/trace_body_read_test.go \
        tools/trace-mutate/sets/p3-task-1.py
git commit -m "feat(controlplane): 点读读侧配置与定位键入口校验（计划 3/4 任务 1）

- TraceBodyReadConfig：root（空=禁用）/ read_max_concurrent（默认 2，C5）/
  read_zstd_window_mb（默认 512，必须 >= 写入侧）
- ValidateRef：dt 严格日期 + inst 白名单 + part/row 界，纯内存判定，
  先于任何文件系统访问（计划 1 N3 的调用侧收口；冻结包不可改 ⇒ 登记 D59）
- 读侧不读任何写入侧参数（CHECKS 钉死）"
```

---

### Task 2：点读服务 `TraceBodyService`（PG 查询 + 优先级判定 + jsonb 回退 + 归档取回）

**文件：**
- 新建：`internal/observability/service/trace_body_service.go`
- 测试：`tests/observability/service/trace_body_service_test.go`（B 层，内存 sqlite）

**接口：**
- 依赖输入：任务 1（`TraceBodyReadConfig`、`ValidateRef`）、冻结包（`ta.Reader`、`ta.Ref`、哨兵）
- 对外产出：
  ```go
  // tracearchiveReader 是点读原语的【窄接口】——只声明本服务用到的唯一方法。
  // 目的：B 层可以用替身注入「特定哨兵」，不必真的造出损坏产物。
  // *ta.Reader 天然满足它（方法集一致），生产上直接传真实 Reader。
  type tracearchiveReader interface {
      GetBody(ctx context.Context, ref ta.Ref) ([]byte, error)
  }

  // TraceBodySource 标识正文的实际来源（对外可观测，便于排障与压缩比核算）。
  type TraceBodySource string

  const (
      SourceArchive TraceBodySource = "archive" // 封块产物
      SourceSpool   TraceBodySource = "spool"   // 未封块的明文 spool
      SourceJSONB   TraceBodySource = "jsonb"   // 功能关闭/构造失败 ⇒ 正文仍在 PG 列
  )

  // TraceBody 是对外统一的正文形状（三键，与计划 2 的 trace_line.go 同一形状）。
  type TraceBody struct {
      RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
      RequestBody    json.RawMessage `json:"request_body,omitempty"`
      ResponseBody   json.RawMessage `json:"response_body,omitempty"`
  }

  type TraceBodyResult struct {
      Body   []byte          // 归档路径下是【原字节】；jsonb 路径下是重新序列化的字节
      Source TraceBodySource
      // Ref 是本次取回【实际使用】的定位键（dt/inst/part/row 四分量的唯一导出途径）。
      // 【为什么必须有（任务 7 的前置，登记 E14）】任务 7 的 CLI `--ref` 要输出四分量
      // 且必须复用本服务；没有这个字段时 CLI 只能自己查 call_traces 的四列再拼
      // ⇒ 定位键判据出现第二份实现（D59 的形态）。
      // 【语义】只有经 Reader.GetBody 命中【归档 / 未封块 spool】的成功路径才有值，
      // 且与请求行四列一一对应（DT 是 body_ref_dt 的布局格式化，不是日期换算）；
      // jsonb 回退（四列全 NULL）⇒ **零值**（没有定位键可报，不得拼半个路径）；
      // 其余失败路径不返回结果 ⇒ 本字段无值可读。
      Ref ta.Ref
  }

  type TraceBodyServiceOptions struct {
      Config        cpconfig.TraceBodyReadConfig
      Reader        tracearchiveReader // nil ⇒ 归档路径不可用（见「归档不可用」语义）
      // 【没有 DayOf，也不得有】dt 一律取自行内 body_ref_dt 列，读侧零日期计算（全局约束 + E2）。
  }

  type TraceBodyService struct{ /* db, cfg, reader, paths */ }

  func NewTraceBodyService(db *gorm.DB, opts TraceBodyServiceOptions) (*TraceBodyService, error)

  // GetBody 是唯一的对外取正文入口。
  // 【错误身份】直接透传冻结包的哨兵（用 %w），调用方用 errors.Is 分类：
  //   ErrBodyMissing  ⇒ 404 + body_missing=true
  //   ErrNotArchived  ⇒ 500 + 诊断
  //   ErrFooterCorrupt / ErrTrailerCorrupt ⇒ 500 + 「产物损坏」
  // 本服务【不新增哨兵】（规格 §7.1）。记录不存在用 gorm.ErrRecordNotFound 透传。
  // 状态不一致（定位键部分为 NULL / body_missing 与定位键矛盾）用本包新哨兵
  //   ErrTraceBodyInconsistent（见下），它是【服务层】哨兵，不是存储层哨兵。
  func (s *TraceBodyService) GetBody(ctx context.Context, requestID string) (*TraceBodyResult, error)

  // ErrTraceBodyInconsistent 是【服务层】哨兵：PG 行的定位键/body_missing 组合在
  // 读侧语义下不可解释（例如 4 个键部分非 NULL；或 body_missing=true 却带定位键）。
  // 【为什么必须与 ErrNotArchived 分开】前者是「元数据自身矛盾」，后者是
  // 「元数据与存储不一致」——运维处置不同（一个是数据写坏，一个是文件不在/越界）。
  var ErrTraceBodyInconsistent = errors.New("tracebody: 定位键与 body_missing 组合不一致")

  // ErrTraceBodyDisabled 读侧禁用（Root 空）但行【带定位键】⇒ 部署错误（503）。
  var ErrTraceBodyDisabled = errors.New("tracebody: 读侧未配置（TRACE_BODY_ROOT 为空）")
  ```

  **`GetBody` 的判定顺序（**逐条都是承重的，任何一步被摘掉或换序都必须有变异红**）：**
  ```
  0. 记录不存在（gorm.ErrRecordNotFound）                ⇒ 透传（handler 映射 404）
  1. body_missing == true【且四个定位键全为 NULL】      ⇒ 返回 ErrBodyMissing
                                                           【必须早于定位键判定】
  1'. body_missing == true 却【带定位键】（数据写坏）    ⇒ 返回 ErrTraceBodyInconsistent
                                                           （500，**绝不报 404**，见 E9）
  2. 四个定位键【全为 NULL】
       2a. 若 Config.Disabled()                        ⇒ 200 + SourceJSONB（正常路径）
       2b. 否则                                        ⇒ 200 + SourceJSONB（功能关闭的历史行）
       （两者都是 jsonb 回退；Disabled 只影响 3c 的报错方式，不影响 2）
  3. 四个定位键【全非 NULL】
       3a. ValidateRef(ref) 失败（**理论上不该发生**：dt 来自 date 列、inst 来自白名单）
           ⇒ 返回 ErrTraceBodyInconsistent（**不调用 GetBody**，先于任何 IO）
       3b. Config.Disabled() ⇒ 返回 ErrTraceBodyDisabled（503：元数据指向归档但读侧未配置）
       3c. 否则调用 Reader.GetBody(ctx, ref)
             · nil error      ⇒ 200 + Body 原字节；Source 由【调用后探测】决定
             · ErrBodyMissing ⇒ 透传（标记行：正文写入时即失败）
             · 其它           ⇒ 透传（ErrNotArchived / ErrFooterCorrupt / ErrTrailerCorrupt）
  4. 定位键【部分非 NULL】                              ⇒ 返回 ErrTraceBodyInconsistent
  ```

  **`Source` 的判定（`archive` vs `spool`）——必须与 `GetBody` 的查找顺序一致**：`GetBody` 的顺序是**产物 → spool**（`reader.go:121-135`）。本服务在**`Reader.GetBody` 成功返回之后**探测（`os.Stat`）；**⚠️ 订正**：原稿写「在**调用前**」，实现是成功后探测 —— 两者**语义等价**（该 `stat` 只决定 `Source` 这个诊断字段，不参与任何判定、不改变正文内容），订正为实际语义并把偏离登记在交付报告 §4「`Source` 判定」：
  ```
  PartFile(ref) 存在   ⇒ SourceArchive
  否则 SpoolFile(ref) 存在 ⇒ SourceSpool
  否则（都不存在，GetBody 会返回 ErrNotArchived）⇒ 用 SourceArchive 占位（该结果一定带错误，Source 无语义）
  ```
  **⚠️ 这里刻意不复用 `GetBody` 的返回值来判断来源**（它不回报来源）⇒ 判定与 `GetBody` 内部是**两次 `stat`**，理论上存在「两次之间 spool 被 sealer 删除」的窗口。**这不是竞态缺陷**：`GetBody` 自身按「产物 → spool」在**同一次调用内**判定，两次 `stat` 只影响 `Source` 这个**诊断字段**；最坏情况是 `Source` 标成 spool 而实际读到产物（或反之），**正文内容仍然正确**。这条限制必须写进代码注释与报告，不得声称 `Source` 是原子的。

  **`dt` 的来源（本计划的裁定，登记 E2）**：`ref.DT` **只能**来自行内已存的 `body_ref_dt` 列值，**读侧不做任何日期计算、不注入任何日期口径**（原设计在服务选项里加日期换算字段、由 router 注入 control-plane 侧的同名函数的写法，**已整体删除**——`internal/controlplane/config` 里根本没有那个符号）。定位键构造（**任务 4 的 `m04` 以本字面量为变异目标，必须原样保留这一行**）：
  ```go
  ref := ta.Ref{
      DT:   trace.BodyRefDT.Format("2006-01-02"), // 只做布局格式化；不是日期换算
      Inst: *trace.BodyRefInst,
      Part: *trace.BodyRefPart,
      Row:  *trace.BodyRefRow,
  }
  ```
  写入侧（计划 2 的 `DayStart`/`DayOf`）仍是口径的**唯一事实源**，读侧只读它算好并落库的结果 ⇒ 不存在「两套口径」。登记 D57 的配套。

- [ ] **步骤 1：写失败的测试**

`tests/observability/service/trace_body_service_test.go`（B 层，`package service_test`）。**核心用例：**

```go
// TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing  ← m01 的杀手
//   构造：一行 {BodyRef*: 全 NULL, BodyMissing: false, RequestBody: `{"i":1}`}
//   断言：err==nil；Source==SourceJSONB；解析出的 request_body 与列值一致。
//   【为什么是核心】计划 2 的功能关闭路径与构造失败路径都落在这个形态；
//   把它读成「缺失」会让功能关闭与 sink 构造失败变成 404 —— 静默丢正文。

// TestTraceBodyServiceBodyMissingWinsOverEverything  ← m02 的杀手
//   构造：一行 {BodyMissing: true, BodyRef*: 全 NULL} ⇒ 必须 ErrBodyMissing（404 类）
//   再加一行 {BodyMissing: true 且带定位键} ⇒ 必须 ErrTraceBodyInconsistent（服务层哨兵）

// TestTraceBodyServiceReadsArchiveWhenRefsPresent  ← m04 的杀手
//   用替身 reader 返回固定字节；断言 Body 逐字节等于替身返回值、Source==archive/spool

// TestTraceBodyServicePartialRefsAreInconsistent  ← m03 的杀手
//   4 个键的非空子集（单键、双键、三键共 14 种）逐个断言
//   ⇒ ErrTraceBodyInconsistent；【绝不】jsonb 回退、【绝不】404
//   【覆盖的形态（订正 E17）】不得用「循环计数器 == 14」表达覆盖 —— 那是恒真断言。
//   期望集合由「4 个二值键的全部 16 种形态」推导（排除 0=全 NULL 与 15=全非 NULL），
//   并断言【实际驱动的 mask 集合】与期望集合逐元素相等（少跑/跳格/多跑即红）。

// TestTraceBodyServiceDisabledWithRefsIs503  ← m05 的杀手
//   Root 空 + 行带定位键 ⇒ ErrTraceBodyDisabled
//   （不是 404、不是 ErrNotArchived、不是 jsonb 回退）

// TestTraceBodyServiceRejectsIllegalRefBeforeIO  ← m06 的杀手+隔离探针
//   行的定位键列值非法（inst 穿越向量 / inst 空串 / part 超上界 / part 为负 / row 为负）⇒
//   必须 ErrTraceBodyInconsistent，且【替身 reader 的调用次数必须为 0】。
//   调用次数断言是承重的：它证明校验【先于】任何存储访问。
//   【为什么不用 dt 的非法值（订正 E12）】body_ref_dt 是 *time.Time（type:date），
//   `.Format("2006-01-02")` **恒**产出合法日期（实测：往该列写文本 "2026-09-24/../.."
//   读回来是零值时间，`.Format` 得到 "0001-01-01"，仍是合法日期）⇒ 服务层不可能构造出
//   非法 dt；dt 分支的守卫由任务 1 的配置层用例承担（不得为此改模型）。

// TestTraceBodyServiceRecordNotFound  ← 透传 gorm.ErrRecordNotFound 的杀手

// TestTraceBodyServicePassthroughSentinels  ← m07 的杀手
//   替身分别返回 ErrNotArchived / ErrBodyMissing / ErrFooterCorrupt / ErrTrailerCorrupt
//   ⇒ 断言 errors.Is 原样成立（不得被换成别的哨兵、不得被吞掉）

// TestTraceBodyServiceExportsRequestRefOnHit  ← m09 的杀手（任务 7 的前置，登记 E14）
//   ① 归档命中（含未封块 spool）⇒ 结果里的 Ref 四分量与请求行四列一一对应，
//      且**就是**送给 Reader 的那一个（不得另建 ref）；
//   ② jsonb 回退（四列全 NULL）⇒ Ref 为零值（没有定位键可报，不得拼半个路径）。
//   【为什么必须有】任务 7 的 CLI `--ref` 要输出四分量并复用服务层 ⇒ 服务层必须导出
//   定位键，否则 CLI 只能自己查 call_traces 四列（D59 的第二份判据）。
```

**⚠️ 用例清单的计数口径（订正 E14）**：原稿列 8 条核心用例；实现期另加
`TestTraceBodyServiceConstructorRejectsUnusableOptions`（第 9 条，构造期两道否决）与
`TestTraceBodyServiceExportsRequestRefOnHit`（第 10 条，本轮的导出途径）⇒ 本任务的
清单由测试源码推导（`grep '^func Test'`），权威表按**实际 10 条**核对，不得写死 8。

测试夹具（两份假实现，都放测试文件里）：
```go
// fakeReader 是窄接口替身：可编程返回 (bytes, error)，并统计调用次数。
type fakeReader struct{ calls int; body []byte; err error }
func (f *fakeReader) GetBody(ctx context.Context, ref ta.Ref) ([]byte, error) {
    f.calls++; return f.body, f.err
}
// newTraceBodyTestDB 建内存 sqlite + AutoMigrate(&obsmodel.CallTrace{})。
```

- [ ] **步骤 2：运行测试确认失败**

运行：`go test ./tests/observability/service/ -run 'TestTraceBodyService' -v`
预期：**编译失败**（`NewTraceBodyService` 等不存在）。

- [ ] **步骤 3：实现服务**

按上面的判定顺序逐条实现。**实现纪律：**
1. **先取 PG 行、再判定、最后才碰存储**——`ValidateRef` 与「部分 NULL」判定都**不得**在存储访问之后。
2. **jsonb 回退必须重建三键形状**：`TraceBody{RequestHeaders: trace.RequestHeaders, RequestBody: trace.RequestBody, ResponseBody: trace.ResponseBody}`，然后 `json.Marshal`。**放 `omitempty`** 与计划 2 的 `trace_line.go` 一致（NULL 列不出现键，而不是出现 `null`）。`json.Marshal` 的失败**必须**用带上下文的 `fmt.Errorf` 包装（见纪律 5），**不得**写成裸 `return nil, err`。
3. **错误一律 `%w` 包装**，检查一律 `errors.Is`。
4. **不要把 handler 的东西（状态码、gin.Context）引进服务**。
5. **【变异目标的唯一性约束，必须满足】** 为了让任务 2/4 的变异能唯一定位（工具一次只替换**恰好 1 处**），服务文件里必须满足：
   - `return nil, err` **只出现一次**，就是 `Reader.GetBody` 错误的原样透传（它保留冻结包的底层诊断文本，任务 3 要求不得替换成固定文案）；其余所有错误路径都要用带上下文的 `fmt.Errorf` 包装。**⚠️ 计数的口径（订正 E10）**：裸串 `return nil, err` 会被 `return nil, errors.New(...)` 这类构造期返回**顺带命中**（本实现 3 次）⇒ 变异/静态检查一律用**带缩进的精确行** `"\t\treturn nil, err\n"`（它在服务文件里唯一，且要求透传点位于**函数体第一层**，不得嵌进 `if` 块）。
   - `src := sourceOf(s.paths, ref)` **只出现一次**（`Source` 的来源判定），其返回语句 `return SourceSpool` 在**本文件里唯一**。
   - `return s.fromJSONB(trace)` **只出现一次**（判定顺序的 2a 与 2b 是同一结果 ⇒ 合并成**一处**返回；`fromJSONB` 内部**不得**再出现这一行，它自己要返回时用 `return &TraceBodyResult{Body: b, Source: SourceJSONB}, nil`）。**任务 2 的 `m01` 以它为变异目标**。
   - 成功返回**只有一处**：`return &TraceBodyResult{Body: body, Source: src, Ref: ref}, nil`
     （其中 `Ref: ref` 是定位键的**唯一导出途径**，任务 7 的 CLI `--ref` 直接读它 ——
     **不得**让 CLI 自己查 `call_traces` 四列，登记 E14）。
   - `Source` 判定用下面这个小函数（**任务 4 的 `m02` 以 `return SourceSpool` 为变异目标**）；它与 `GetBody` 的「产物 → spool」顺序一致，且**只影响诊断字段**（两次 stat 的窗口说明见上）：
     ```go
     // sourceOf 只用于诊断字段 Source；判定顺序与 Reader.GetBody 一致（产物 → spool）。
     func sourceOf(paths ta.Paths, ref ta.Ref) TraceBodySource {
         if _, err := os.Stat(paths.PartFile(ref)); err == nil { return SourceArchive }
         if _, err := os.Stat(paths.SpoolFile(ref)); err == nil { return SourceSpool }
         return SourceArchive // 都不存在：GetBody 必返回 ErrNotArchived，Source 无语义
     }
     ```
   - 定位键构造**原样**使用上面「`dt` 的来源」给出的 `ta.Ref` 字面量（**任务 4 的 `m04` 以 `DT:   trace.BodyRefDT.Format("2006-01-02")` 为变异目标**；⚠️ 这里的分量缩进是 **`DT:` 后 3 个空格**，与任务 4 的集合草稿逐字一致 —— 计划原稿在此写成单空格，与集合/实际行不符，已订正）。
   - **前两步的硬形状**（判定顺序 1 / 1' 与「部分 NULL」的入口；**已按 E9 订正**——原稿把
     `body_missing` 无条件判在定位键之前，会让「带定位键的 `body_missing`」返回 404，
     与全局约束「该形态 ⇒ 500，绝不报 404」互斥；订正后三种形态共用**一处**「不一致」出口）：
     ```go
     refs := refsOf(&trace)      // 四个定位键中「非 NULL」分量的文本形态（长度即非 NULL 分量数）
     allRefs := len(refs) == traceBodyRefKeyCount

     // 只有四个键全非 NULL 才构造定位键；其它形态下 ref 保持零值且不参与任何判定
     var ref ta.Ref
     if allRefs {
         ref = ta.Ref{DT: trace.BodyRefDT.Format("2006-01-02"), Inst: *trace.BodyRefInst,
                      Part: *trace.BodyRefPart, Row: *trace.BodyRefRow}
     }

     // 【不一致的唯一出口】部分 NULL（1..3）/ body_missing 却带定位键 / 列值非法
     badRefs := len(refs) > 0 && (!allRefs || trace.BodyMissing)
     if allRefs {
         if err := cpconfig.ValidateRef(ref); err != nil {
             badRefs = true
         }
     }
     if badRefs {
         return nil, fmt.Errorf("%w: %s", ErrTraceBodyInconsistent, requestID)
     }

     if trace.BodyMissing {
         return nil, ta.ErrBodyMissing
     }
     if len(refs) == 0 {
         return s.fromJSONB(trace)   // 四个键全为 NULL：2a 与 2b 共用这一处
     }
     ```
     `refs` 的作用是让「全 NULL / 部分 NULL / 全非 NULL」三种形态共用**一次**判定，
     **不得**改成三段 `if`（那样「部分 NULL」的入口与归档入口会各判一次，`m02`/`m03`
     的变异目标也随之不存在 ⇒ `PATTERN-MISS`）。
     **⚠️ 上面硬形状里的 `DT:` 是压缩写法**：实际实现必须逐字采用「`dt` 的来源」处的
     **3 空格**字面量（`DT:   trace.BodyRefDT.Format("2006-01-02"),`，且**成功返回处只有
     一处** `Ref: ref` 导出）—— 任务 4 的 `m04` 以该 3 空格串为定位串（见 E15）。

- [ ] **步骤 4：运行测试确认通过**

运行：`go test ./tests/observability/service/ -run 'TestTraceBodyService' -v`
预期：**PASS**。再跑 `go build ./...`。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p3-task-2.py`。`TARGET = "internal/observability/service/trace_body_service.go"`，`PACKAGES = ["./tests/observability/service/"]`，`MANIFEST_FILES = ["tests/observability/service/trace_body_service_test.go"]`。

```python
MUTATIONS = {}
# m01 jsonb 回退被摘掉 ⇒ 「全 NULL + 未标 missing」被当成缺失（最严重：功能关闭=静默丢正文）
#     杀手：TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing
MUTATIONS["m01_全NULL不再回退jsonb"] = [
    ("sub", "return s.fromJSONB(trace)", 'return nil, fmt.Errorf("%w: %s", ta.ErrNotArchived, requestID)'),
]
# m02 「body_missing=true 却带定位键」这条矛盾判据被摘掉 ⇒ 数据写坏被伪装成
#     「正文从未落盘」（404）。**替换串已按 E9/E10 订正**（原 `cut` 括号失衡 ⇒ 语法红）
#     杀手：TestTraceBodyServiceBodyMissingWinsOverEverything（第二格）
MUTATIONS["m02_body_missing与定位键的矛盾未判"] = [
    ("sub", "!allRefs || trace.BodyMissing", "!allRefs"),
]
# m03 部分 NULL 被静默回退成 jsonb（最阴的一类：一致性问题被伪装成「正文就是空的」）
#     杀手：TestTraceBodyServicePartialRefsAreInconsistent
MUTATIONS["m03_部分NULL静默回退jsonb"] = [
    ("sub", "return nil, fmt.Errorf(\"%w: %s\", ErrTraceBodyInconsistent, requestID)",
            "return s.fromJSONB(trace)"),
]
# m04 归档字节被换成 jsonb 序列化结果 ⇒ 逐字节保证丢失
#     **替换串已按 E10 订正**：原文会让 `src`/`body` 在替换后未被使用 ⇒ 编译红
#     （实测 `declared and not used: body` / `: src`）
#     **定位串已按 E14 随新增的 `Ref: ref` 导出更新**（旧串在树里已不存在 ⇒ 否则 PATTERN-MISS）
#     杀手：TestTraceBodyServiceReadsArchiveWhenRefsPresent（逐字节断言那一条）
MUTATIONS["m04_归档结果被重新序列化"] = [
    ("sub", "return &TraceBodyResult{Body: body, Source: src, Ref: ref}, nil",
            "_, _ = body, src\n\treturn s.fromJSONB(trace)"),
]
# m05 读侧禁用不再报 503，而是回落 jsonb ⇒ 部署错误被伪装成「正文在 jsonb 列」
#     杀手：TestTraceBodyServiceDisabledWithRefsIs503
MUTATIONS["m05_禁用时回落jsonb"] = [
    ("sub", "return nil, fmt.Errorf(\"%w: %s\", ErrTraceBodyDisabled, requestID)",
            "return s.fromJSONB(trace)"),
]
# m06 ValidateRef 的判据被摘掉 ⇒ 畸形定位键会真的去访问存储
#     （`if false {` 连 if-init 一起去掉 ⇒ 可编译；实测断言红，见 E10 第 4 条）
#     杀手：TestTraceBodyServiceRejectsIllegalRefBeforeIO（calls==0 断言）
MUTATIONS["m06_不校验定位键"] = [
    ("sub", "if err := cpconfig.ValidateRef(ref); err != nil {", "if false {"),
]
# m07 哨兵被吞掉/替换 ⇒ 上层无法分类（把存储故障伪装成业务事实）
#     **定位串已按 E10 订正**：裸串 `return nil, err` 在服务文件里出现 3 次
#     （构造期两条 `return nil, errors.New(...)` 也以它起手）⇒ 原文是 PATTERN-MISS
#     杀手：TestTraceBodyServicePassthroughSentinels
MUTATIONS["m07_吞掉reader哨兵"] = [
    ("sub", "\t\treturn nil, err\n",
            '\t\treturn nil, fmt.Errorf("%w: %s", ErrTraceBodyInconsistent, requestID)\n'),
]
# m08 记录缺失不再透传 gorm.ErrRecordNotFound（换成「正文从未落盘」）⇒ handler 会把
#     「记录不存在」映射成 404 body_missing，前端无法区分「没有这条 trace」与
#     「有这条 trace 但正文写入时失败」。**按 E11 补入**：计划把判定顺序 0 也声明为
#     承重，却没有给它任何变异。
#     杀手：TestTraceBodyServiceRecordNotFound
MUTATIONS["m08_记录缺失被吞掉"] = [
    ("sub", "return nil, fmt.Errorf(\"tracebody: %s 无对应 call_traces 记录: %w\", requestID, gorm.ErrRecordNotFound)",
            "return nil, ta.ErrBodyMissing"),
]
# m09 成功路径不再导出本次实际使用的定位键（Ref 恒零值）⇒ 任务 7 的 CLI `--ref`
#     拿不到四分量，只能自己查 call_traces 四列再拼 ⇒ 定位键判据出现第二份实现
#     （D59 的形态）。**按 E14 补入**：任务 7 要求 `--ref` 输出四分量并复用服务层，
#     而任务 2 原先没有任何导出途径。替换串只动导出字段（`ref` 仍在 GetBody 调用处
#     被使用 ⇒ 可编译）⇒ 只杀导出语义那一条断言（隔离探针）。
#     杀手：TestTraceBodyServiceExportsRequestRefOnHit（① 归档命中导出四分量）
MUTATIONS["m09_成功路径不导出定位键"] = [
    ("sub", "Source: src, Ref: ref}, nil", "Source: src, Ref: ta.Ref{}}, nil"),
]
```

**CHECKS（变异测不到）：**
```python
CHECKS = [
  {"name": "jsonb回退用omitempty三键", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r'json:"(request_headers|request_body|response_body),omitempty"',
   "expect": "present", "why": "形状必须与计划 2 的 trace_line.go 一致；变异测不到 JSON tag"},
  {"name": "服务层不引入HTTP类型", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r"gin\.|net/http", "expect": "absent",
   "why": "服务必须与协议解耦，否则 B 层无法在无 HTTP 的情况下测判定顺序"},
  {"name": "冻结包哨兵语义未变", "file": "internal/tracearchive/errors.go",
   "pattern": r'ErrNotArchived = errors\.New\("tracearchive: 产物与暂存均不存在"\)',
   "expect": "present",
   "why": "服务层「透传冻结包哨兵、不新增哨兵」的前提是哨兵定义不变；整目录零改动的权威判据是任务 9 的 git diff（D59）。**不得**把 `file` 写成 `internal/tracearchive`（目录 ⇒ 工具 `open()` 抛 IsADirectoryError，整轮零产出）"},
  # 【按 E11 补入的一行】判定顺序 0 的可验证形式：REQUIREMENTS 的「记录缺失」若既无变异
  # 也无静态检查，映射表会出现 GAP（工具判失败）。m08 已覆盖其行为 ⇒ 这一行钉住**包装形态**
  # 的结构不变量（去掉 %w 或换成固定文案都会命中失败）。
  {"name": "记录缺失保留gorm哨兵", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r'fmt\.Errorf\("tracebody: %s 无对应 call_traces 记录: %w", requestID, gorm\.ErrRecordNotFound\)',
   "expect": "present",
   "why": "记录缺失必须用 %w 包装 gorm.ErrRecordNotFound，handler 才能用 errors.Is 把它映射成 404 trace_not_found（而不是 body_missing）"},
]
```

- [ ] **步骤 6：跑变异集合**（命令同任务 1，换集合与清单）
预期：**9 条断言红**（原稿写「7 条」；按 **E11** 补入 `m08` 覆盖判定顺序 0 ⇒ 8 条；
按 **E14** 补入 `m09` 覆盖新增的定位键导出 ⇒ **9 条**）、
三断言过（清单 10 条）、无未登记绿、4 行静态检查全 PASS。

- [ ] **步骤 7：提交**

```bash
git add internal/observability/service/trace_body_service.go \
        tests/observability/service/trace_body_service_test.go \
        tools/trace-mutate/sets/p3-task-2.py
git commit -m "feat(observability): 点读服务与优先级判定（计划 3/4 任务 2）

判定顺序（承重）：记录缺失 → body_missing → 定位键全 NULL（jsonb 回退）→
全非 NULL（ValidateRef → 禁用判定 → GetBody → 哨兵透传）→ 部分 NULL（服务层不一致）。
jsonb 回退解释「ref 全 NULL + body_missing=false」为「正文在 jsonb 列」而非「缺失」。"
```

---

### Task 3：错误语义 → HTTP 表现与诊断契约（`MapTraceBodyError`）

**文件：**
- 新建：`internal/observability/handler/trace_body_error.go`（**纯函数，无 IO**）
- 测试：`tests/observability/handler/trace_body_error_test.go`（B 层）

**接口：**
- 依赖输入：任务 2（`ErrTraceBodyInconsistent` / `ErrTraceBodyDisabled`）
- 对外产出：
  ```go
  // TraceBodyErrorResponse 是对外诊断体（与仓库既有错误形状一致：{"error": {...}}）。
  type TraceBodyErrorResponse struct {
      Error TraceBodyErrorDetail `json:"error"`
  }
  type TraceBodyErrorDetail struct {
      Type    string `json:"type"`             // "not_found" / "gone" / "storage_error" / ...
      Code    string `json:"code"`             // 机器可读
      Message string `json:"message"`          // 人可读，含 Ref 与成因
      BodyMissing bool `json:"body_missing"`   // 仅 404 类为 true（规格 §8.1）
  }

  // MapTraceBodyError 把服务层/存储层的错误分类成对外表现。纯函数。
  // 【为什么单独成文件】它是本计划里唯一「纯粹的分类逻辑」——没有 IO、没有 DB、
  // 没有 gin.Context ⇒ 可以做到「每条哨兵一个用例、每条变异一个断言红」。
  func MapTraceBodyError(err error) (status int, resp TraceBodyErrorResponse)
  ```

  **完整映射表（本计划的**权威判据表**，必须逐行实现、逐行有用例、逐行有变异或 CHECKS）：**

  | 输入 `err`（`errors.Is` 判定） | HTTP | `code` | `body_missing` | 判据（**可判定的**，不是措辞） |
  |---|---|---|---|---|
  | `nil` | **200** | — | 不适用 | 已有正文（`TraceBodyResult`） |
  | `gorm.ErrRecordNotFound` | **404** | `trace_not_found` | `false` | `call_traces` 无该 `request_id` |
  | `ta.ErrBodyMissing` | **404** | `body_missing` | **`true`** | 命中标记行（产物或 spool 路径）⇒ 正文在写入时即失败 |
  | `ErrTraceBodyInconsistent` | **500** | `body_ref_inconsistent` | `false` | 定位键部分 NULL / `body_missing` 与定位键矛盾 / 列值非法 |
  | `ErrTraceBodyDisabled` | **503** | `trace_body_disabled` | `false` | 行带定位键但 `TRACE_BODY_ROOT` 为空（部署错误） |
  | `ta.ErrNotArchived` | **500** | `not_archived` | `false` | 元数据与存储不一致。**含三种可分辨的成因**（见下） |
  | `ta.ErrFooterCorrupt` | **500** | `part_corrupt` | `false` | `footer` 不符（含 `frame_sha256` 校验失败 = 半写/位腐） |
  | `ta.ErrTrailerCorrupt` | **500** | `part_corrupt` | `false` | trailer 结构损坏/条目数不符/**超 `maxTrailerEntries`** |
  | `ta.ErrPartNotFound` | **（不产生）** | — | — | **全仓无生产者**，见 D53；保留一个 `default` 分支落到它即可 |
  | `context.Canceled` | **499 语义 → 用 408** | `client_closed` | `false` | 客户端断开；**不写 500**（否则日志里全是假存储故障） |
  | `context.DeadlineExceeded` | **504** | `read_timeout` | `false` | 读超时（大块解压慢） |
  | 其它 | **500** | `internal_error` | `false` | 兜底；`message` 必须**不含**路径以外的内部细节 |

  **`ErrNotArchived` 的三种成因必须可分辨（本计划要求，登记 D60 的配套）**：`errors.go` 的自述列了它的覆盖范围，本计划的诊断文本**必须**能区分它们，因为运维处置不同：
  ```
  ① 产物与 spool 都不存在      → "无可读副本（产物与 spool 均不存在）"  → 检查挂载/清理/sealer
  ② 产物存在但 row >= row_count → "行号越界（块内共 N 行）"             → 元数据与产物行数不一致
  ③ 目标偏移未命中行首 / spool 残行 → "存储处于不可按行定位的状态"       → 产物损坏或写入事故
  ```
  **判定方式**：不新增哨兵（规格 §7.1 禁止），而是**在服务层拼装诊断前缀**（`GetBody` 返回的 error 文本已含这些信息，见 `reader.go:183/266/330`），并**原样 `%w` 包裹**。⇒ 实现要求：服务层在返回 `ErrNotArchived` 时**必须保留底层文本**（`fmt.Errorf("%w", err)` 形式），**不得**替换成自造的固定文案。

  **⚠️ 为什么 410 不出现（登记 D53）**：规格 §8.1 的响应表列了 `410 + body_missing=true`（「正文曾落盘但产物缺失」）。但**冻结包没有任何代码路径返回 `ErrPartNotFound`**（`grep -rn "ErrPartNotFound" --include=*.go .` ⇒ 只有 `errors.go:13` 的定义与 `tests/tracearchive/ref_test.go:83` 的哨兵互异表）。`Reader.GetBody` 在「两个文件都不存在」时返回的是 **`ErrNotArchived`**，而 `errors.go:18-27` 明确写它**对应 HTTP 500**（复核：`ErrNotArchived` 的注释块起于 18 行、定义在 27 行；原稿的 `15-25` 行号是错的，已订正）。⇒ **410 在当前实现下不可达**。本计划的裁定：**遵守冻结包的自述（`ErrNotArchived` ⇒ 500）**，把「410 行」登记为规格缺陷。**不得**为了让 410 可达而改冻结包，**不得**在 handler 里凭「产物不存在」反推 410（那需要在 handler 里复制路径布局知识，与读侧单一事实源冲突）。

- [ ] **步骤 1：写失败的测试**

`tests/observability/handler/trace_body_error_test.go`：**表驱动**，一行为一条映射，**每条都断言 `status` 与 `code` 两个字段**（`code` 是机器可读契约，`message` 只断言「非空且含关键子串」，不做逐字断言以免文案漂移把变异变脆）。

```go
// TestMapTraceBodyError 的表驱动条目（每行 = 一个子例）：
//   {name:"nil 不产生错误", in:nil,   wantStatus:200}
//   {name:"记录缺失", in:gorm.ErrRecordNotFound, wantStatus:404, wantCode:"trace_not_found", wantMissing:false}
//   {name:"body_missing 标记行", in:ta.ErrBodyMissing, wantStatus:404, wantCode:"body_missing", wantMissing:true}
//   {name:"定位键不一致", in:ErrTraceBodyInconsistent, wantStatus:500, wantCode:"body_ref_inconsistent"}
//   {name:"读侧未配置", in:ErrTraceBodyDisabled, wantStatus:503, wantCode:"trace_body_disabled"}
//   {name:"未归档", in:ta.ErrNotArchived, wantStatus:500, wantCode:"not_archived"}
//   {name:"footer 损坏", in:ta.ErrFooterCorrupt, wantStatus:500, wantCode:"part_corrupt"}
//   {name:"trailer 损坏", in:ta.ErrTrailerCorrupt, wantStatus:500, wantCode:"part_corrupt"}
//   {name:"客户端断开", in:context.Canceled, wantStatus:408, wantCode:"client_closed"}
//   {name:"读超时", in:context.DeadlineExceeded, wantStatus:504, wantCode:"read_timeout"}
//   {name:"未知错误兜底", in:errors.New("boom"), wantStatus:500, wantCode:"internal_error"}

// TestMapTraceBodyErrorRequiresWrapping —— 【承重】每一行都必须用 %w 包装的
// errors.Is 判定，而不是 == 比较：
//   用 fmt.Errorf("wrap: %w", ta.ErrNotArchived) 与 fmt.Errorf("wrap: %w", ta.ErrFooterCorrupt)
//   分别断言仍映射到正确的 status/code。这条是 m02 的杀手。

// TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText —— 【承重】
//   **【订正 E20】原文的向量与断言互斥**：原稿写「传入
//   `fmt.Errorf("%w: %s（块内共 %d 行）", ta.ErrNotArchived, "<ref>", 3)` ⇒ message 必须包含
//   「行号越界」「块内共 3 行」」——但该向量产出的文本是
//   `tracearchive: 产物与暂存均不存在: <ref>（块内共 3 行）`，**不含「行号越界」**
//   （那四个字是本计划正文的诊断说明，不是冻结包的错误文本；`reader.go:183` 的实际拼装
//   就是 `"%w: %s（块内共 %d 行）"`）⇒ 该断言恒红、不可满足。
//   改为**按冻结包 reader.go 的真实拼装取三个向量**，各自断言其**可分辨子串**原样透出：
//     ① `reader.go:139` 两副本都不存在   `fmt.Errorf("%w: %s", ta.ErrNotArchived, ref)` ⇒ 含 `产物与暂存均不存在`
//     ② `reader.go:183` 行号越界          `fmt.Errorf("%w: %s（块内共 %d 行）", …, ref, 3)` ⇒ 含 `块内共 3 行`
//     ③ `reader.go:266` 目标偏移未命中行首 `fmt.Errorf("%w: %s（偏移 %d 未命中行首）", …, ref, 4096)` ⇒ 含 `偏移 4096 未命中行首`
//   语义不变（仍是「底层文本必须被保留、不得替换成固定文案」），只是向量可达；三是
//   「三种成因可分辨」的可自动化形态（三格共用一处 `Message: err.Error(),`，由 m05 杀死）。

// TestMapTraceBodyErrorSuccessCarriesNoPayload  ← 【E21 新增，承重用例】
//   `MapTraceBodyError(nil)` ⇒ status 200 且返回的 resp 是**零值诊断体**：
//   200 的 body 是服务层返回的**正文原字节**（由任务 5 的 handler 直接写出），
//   映射层**不得**为成功路径产出任何诊断体/包壳。可失败：为 nil 臂填入任何响应体即红。
// TestTraceBodyErrorResponseKeySet  ← 【E21 新增，承重用例】
//   把一条错误行的响应 `json.Marshal` 后断言**键集合逐字固定**：
//   顶层只有 `error`，内层只有 `{body_missing, code, message, type}` 四键，
//   且**不存在** `ref`/`source`/`body`/`trace`/`dt`/`inst`/`part`/`row` 任一键
//   （防任务 2 新增的 `TraceBodyResult.Ref` 经诊断体泄漏）。可失败：给诊断体加任一
//   带 json tag 的字段（如 `ref`）即红。
```

- [ ] **步骤 2：运行测试确认失败**（编译失败：`MapTraceBodyError` 不存在）
- [ ] **步骤 3：实现 `MapTraceBodyError`**（按上表逐行；`errors.Is` 顺序：具体哨兵在前、`context` 与兜底在后）

**实现形状是硬要求（变异可定位的前提，必须逐字遵守）：**
- 用 **`switch { case …: }` + 每个臂直接 `return <status>, TraceBodyErrorResponse{…}`**，**不得**用 if-chain、也不得先赋值 `resp` 再统一 return（任务 3 的 **12** 条变异都以这个形态的字符串为变异目标；原稿写「7 条」，按 E21 补入后为 12 条）。
- **`Message: err.Error(),` 只能出现在 `ta.ErrNotArchived` 这一个臂**（它是唯一必须保留底层三种成因文本的臂）；其余臂（记录不存在 / body_missing / 损坏 / 禁用 / 取消 / 超时 / 兜底）一律用**固定文案**，**不得**用 `err.Error()`（否则会泄漏内部细节，也让 `m05` 无法唯一定位）。
- **`BodyMissing: true,` 只能出现在 `ta.ErrBodyMissing` 一个臂**（其余臂不写该字段或写 false）。
- `http.StatusServiceUnavailable, TraceBodyErrorResponse{` 这一串**只能出现在 `ErrTraceBodyDisabled` 一个臂**。
- **本文件（含注释）不得引用上述标识符**——逐字枚举两条「不存在性」CHECKS 的 pattern：
  `os.` / `io.Read` / `gorm.DB` / `gin.` / `json.Marshal` / `TraceBodyResult` / `result.Body` /
  `json.RawMessage` / `json.NewEncoder` / `ResponseWriter` / `WriteHeader`。**它们是这些字面标识符
  本身，不是「JSON 序列化调用」「服务层结果类型名」「响应写出调用」这类描述性类别**（措辞收窄，登记 E23）。
  理由：CHECKS 的 pattern 是对**整个文件文本（含注释）**做 `re.search` ⇒ 注释里抄了这些字样就会恒 FAIL、
  工具在跑变异**之前**终止（本轮实测踩过，见 E21）。
  **⚠️ 静态检查只是「没抄这些字样」的证据，不是「映射层没有包壳」的证明**：真正承重、真正能失败的
  是那**两条可失败用例** —— `TestMapTraceBodyErrorSuccessCarriesNoPayload`（nil ⇒ 零值诊断体）与
  `TestTraceBodyErrorResponseKeySet`（响应键集合逐字固定，无 ref/source/body/trace 键）；自己手拼 JSON、
  或换别名绕过正则的写法，只有这两条拦得住。

- [ ] **步骤 4：运行测试确认通过**
```bash
go test ./tests/observability/handler/ -run 'TestMapTraceBodyError|TestTraceBodyErrorResponseKeySet' -v   # PASS
go build ./internal/... ./cmd/...   # 【订正】`go build ./...` 在本仓库基线上就失败于 test/bedrock_model_id_check.go
```

- [ ] **步骤 5：写变异集合 `tools/trace-mutate/sets/p3-task-3.py`**

`TARGET = "internal/observability/handler/trace_body_error.go"`，`PACKAGES = ["./tests/observability/handler/"]`。

```python
NAME = "p3-task-3"
TARGET = "internal/observability/handler/trace_body_error.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_error_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/handler/trace_body_error_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"

MUTATIONS = {}
# m01 ErrNotArchived 被降级成 404 ⇒ 把存储故障伪装成「数据不存在」（v3 方案的原始教训）
#     杀手：TestMapTraceBodyError（not_archived 那一行）
MUTATIONS["m01_未归档降级为404"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):", "case errors.Is(err, ta.ErrNotArchived) && false:"),
]
# m02 用 == 比较代替 errors.Is ⇒ 包装过的哨兵全部落进兜底（500/internal_error）
#     杀手：TestMapTraceBodyErrorRequiresWrapping
MUTATIONS["m02_改用等值比较"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):", "case err == ta.ErrNotArchived:"),
]
# m03 body_missing 标志被摘掉 ⇒ 404 的 body_missing=true 契约破裂（规格 §8.1 明文）
#     杀手：TestMapTraceBodyError（body_missing 行的 wantMissing 断言）
MUTATIONS["m03_不再置body_missing标志"] = [
    ("sub", "BodyMissing: true,", "BodyMissing: false,"),
]
# m04 产物损坏被报成 404 ⇒ 半写/位腐被伪装成「不存在」
#     杀手：TestMapTraceBodyError（footer/trailer 两行）
MUTATIONS["m04_产物损坏降级为404"] = [
    ("sub", "case errors.Is(err, ta.ErrFooterCorrupt), errors.Is(err, ta.ErrTrailerCorrupt):",
            "case false:"),
]
# m05 底层文本被替换成固定文案 ⇒ 「三种成因」不可分辨
#     杀手：TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText
MUTATIONS["m05_丢失底层诊断文本"] = [
    ("sub", "Message: err.Error(),", 'Message: "未归档",'),
]
# m06 读侧未配置从 503 变成 500 ⇒ 部署错误与存储错误混淆
#     杀手：TestMapTraceBodyError（trace_body_disabled 行）
MUTATIONS["m06_禁用状态码变成500"] = [
    ("sub", "http.StatusServiceUnavailable, TraceBodyErrorResponse{", "http.StatusInternalServerError, TraceBodyErrorResponse{"),
]
# m07 上下文取消被报成 500 ⇒ 日志里全是假存储故障（掩盖真实故障）
#     杀手：TestMapTraceBodyError（client_closed 行）
MUTATIONS["m07_取消报成500"] = [
    ("sub", "case errors.Is(err, context.Canceled):", "case false:"),
]
# ---------------------------------------------------------------------------
# 以下 5 条为【E21 补入】：权威判据表有 12 行，原稿的 m01–m07 只覆盖 6 行
# （body_missing / 读侧禁用 / 未归档 / footer 损坏 / trailer 损坏 / 客户端取消），
# 余下 6 行（nil 200 / 记录缺失 / 定位键不一致 / ErrPartNotFound / 读超时 / 兜底）是**行为**要求，
# 按全局约束第 7 条应当用变异而不是静态检查覆盖（同任务 2 的 E11 处置）。
# ---------------------------------------------------------------------------
# m08 成功路径不再返回 200（nil 落进兜底）⇒ 调用方会把「无错误」当成内部错误。
#     杀手：TestMapTraceBodyError（nil 行）+ TestMapTraceBodyErrorSuccessCarriesNoPayload
MUTATIONS["m08_成功路径不再返回200"] = [
    ("sub", "case err == nil:", "case err == nil && false:"),
]
# m09 记录缺失不再单独分类 ⇒ 「没有这条 trace」与「正文从未落盘」混为一谈
#     杀手：TestMapTraceBodyError（记录缺失行）
MUTATIONS["m09_记录缺失不再分类"] = [
    ("sub", "case errors.Is(err, gorm.ErrRecordNotFound):",
            "case errors.Is(err, gorm.ErrRecordNotFound) && false:"),
]
# m10 定位键不一致（数据写坏）不再单独分类 ⇒ 运维无法从 code 看出该查数据还是该查代码
#     杀手：TestMapTraceBodyError（定位键不一致行）
MUTATIONS["m10_定位键不一致不再分类"] = [
    ("sub", "case errors.Is(err, service.ErrTraceBodyInconsistent):",
            "case errors.Is(err, service.ErrTraceBodyInconsistent) && false:"),
]
# m11 兜底臂被提前拦截（读超时臂的条件放宽成「一切非 nil」）⇒
#     「其它 ⇒ 500/internal_error」与「ErrPartNotFound ⇒ 落到兜底」两行不再可达。
#     选它而不是改 `Code: "internal_error",`：后者依赖 gofmt 的字段对齐（4 个空格），
#     一旦有人加更长的字段名就漂移成 PATTERN-MISS。
#     杀手：TestMapTraceBodyError（未知错误兜底行 / ErrPartNotFound 行）
MUTATIONS["m11_兜底被提前拦截"] = [
    ("sub", "case errors.Is(err, context.DeadlineExceeded):", "case err != nil:"),
]
# m12 读超时不再单独分类 ⇒ 大块解压超时被报成 500，客户端无从得知「等一等可能就好」
#     杀手：TestMapTraceBodyError（读超时行）
MUTATIONS["m12_超时不再分类"] = [
    ("sub", "case errors.Is(err, context.DeadlineExceeded):",
            "case errors.Is(err, context.DeadlineExceeded) && false:"),
]
```

**CHECKS：**
```python
CHECKS = [
  {"name": "不新增存储层哨兵", "file": "internal/observability/handler/trace_body_error.go",
   "pattern": r"errors\.New\(", "expect": "absent",
   "why": "规格 §7.1「读取侧不为它新增分支或哨兵」；只有服务层两个哨兵（任务 2）可以有"},
  {"name": "纯函数不引入IO", "file": "internal/observability/handler/trace_body_error.go",
   # 【注意】不能写成 `gorm\.`：本文件必须**合法引用** gorm.ErrRecordNotFound（它是哨兵，不是 IO）
   "pattern": r"os\.|io\.Read|gorm\.DB|gin\.", "expect": "absent",
   "why": "映射表必须可在无文件系统 / 无 DB / 无 gin 上下文 的情况下逐行测：`gorm.ErrRecordNotFound` 是纯哨兵、`net/http` 只用于状态码常量（http.StatusOK 等）⇒ 两者都是合法引用；被禁的是 gin 请求上下文、文件操作与 *gorm.DB 句柄（订正措辞「无协议类型」→「无 gin 上下文」，登记 E25）"},
  {"name": "冻结包哨兵语义未变", "file": "internal/tracearchive/errors.go",
   "pattern": r'ErrBodyMissing = errors\.New\("tracearchive: 正文从未落盘"\)',
   "expect": "present",
   "why": "本任务把 ta.ErrBodyMissing 映射为 404 + body_missing=true，前提是哨兵定义不变；整目录零改动的权威判据是任务 9 的 git diff（D59）"},
  # 【E21 补入的第 4 行】200 的 body 必须是正文 JSON 原字节（任务 5 的
  # `c.Data(http.StatusOK, "application/json", result.Body)` 直接写出）⇒ 映射层
  # **不得**把服务层结果序列化成包壳 —— 一旦包壳，任务 2 新增的 `TraceBodyResult.Ref`
  # （定位键四分量）就会泄漏进响应。该要求没有对应变异（设计层不变量，不是可摘掉的判定），
  # 由两条可失败用例承重：TestMapTraceBodyErrorSuccessCarriesNoPayload（零值诊断体）与
  # TestTraceBodyErrorResponseKeySet（响应键集合逐字固定）。
  # ⚠️ 该 pattern 会扫**整个文件文本（含注释）**：本文件注释里不得直接抄这些标识符
  # （本轮实测踩坑：注释里的字样让本行与上一行恒 FAIL，工具在跑变异前终止 ⇒ 零证据）。
  {"name": "映射层不包装正文结果", "file": "internal/observability/handler/trace_body_error.go",
   "pattern": r"json\.Marshal|TraceBodyResult|result\.Body|json\.RawMessage|json\.NewEncoder|ResponseWriter|WriteHeader", "expect": "absent",
   "why": "200 的 body 由调用方直接写服务层返回的正文原字节；映射层不得序列化服务层结果、也不得引用其结果类型。**正则已按 E24 增补 `json.NewEncoder` / `ResponseWriter` / `WriteHeader`**（既然本轮反正要重跑，加检查是免费的覆盖提升；已实测干净树上仍 PASS）。可违反：本文件出现上述任一标识符即 FAIL。**效力边界（E23）**：它只证明「没抄这些字样」，真正承重的是两条可失败用例 TestMapTraceBodyErrorSuccessCarriesNoPayload 与 TestTraceBodyErrorResponseKeySet"},
]
```

- [ ] **步骤 6：跑变异集合** → 预期 **12 条断言红**（原稿写 7 条；按 **E21** 补入 m08–m12 ⇒ 12 条）、三断言过、静态检查 4 行全 PASS、映射表无缺口。
- [ ] **步骤 7：提交**

```bash
git add internal/observability/handler/trace_body_error.go \
        tests/observability/handler/trace_body_error_test.go \
        tools/trace-mutate/sets/p3-task-3.py
git commit -m "feat(observability): 点读错误语义到 HTTP 表现的映射（计划 3/4 任务 3）

ErrBodyMissing→404+body_missing=true；ErrNotArchived→500（保留底层三种成因文本）；
ErrFooter/ErrTrailerCorrupt→500 part_corrupt；禁用→503；不一致→500；
取消/超时→408/504。410 不可达（ErrPartNotFound 无生产者）⇒ 登记 D53。
nil ⇒ 200 + 零值诊断体：200 的 body 是正文 JSON 原字节，映射层不得包壳
（防 TraceBodyResult.Ref 泄漏）。"
```

**⚠️ 提交次序（本轮实测确立，与任务 1/2 一致）**：变异工具的 `git status --porcelain` 干净树门
使「先跑工具再提交」在机制上不可能 ⇒ 次序是 **写集合 → 提交 → 出权威表**；若权威表因
**CHECKS 恒 FAIL** 之类的原因中止（工具在跑变异**之前**就退出），修正后必须**新开一个提交**
（不得 `--amend`）并**重跑并重记绑定**。

---

### Task 4：隔离件、spool 兜底与边界回归（**只写测试**；变异打在任务 2/3 的生产代码上）

**文件：**
- 新建：`tests/observability/service/trace_body_quarantine_test.go`（B 层，**真实文件系统** `t.TempDir()`，**真实 `ta.Reader`**）
- **不改任何生产代码**（与计划 2 任务 8 同一形态：变异打在既有生产代码上）

**为什么必须有这一步**：任务 2 的用例全部用**替身 reader**（快、可控），因此**完全没有覆盖真实 `ta.Reader` 的两条路径**。本任务补的是「服务 + 真实冻结包」这一层——这是**生产形态与库层之间的接缝**，也是隔离件规则的唯一可验证形式。

**范围（**本任务只做这四件，多一件都不做**）：**
1. **隔离件形态**（全局约束里的命名/发现规则）；
2. **spool 兜底**（未封块的近期正文）；
3. **行号稠密性与越界**（行号越界 vs 真实缺失的**不同**错误语义）；
4. **跨日 / 跨 part 边界**。

- [ ] **步骤 1：写用例**

```go
// TestTraceBodyServiceQuarantineYieldsNotArchived  ← m01 的杀手
//   布置：在【spool 目录】写 <root>/spool/dt=2026-10-08/inst=blue/part-000000.jsonl.zst，
//         内容是明文 JSONL（模拟 writer.go 的 quarantinePath 产物）。
//   断言：GetBody => errors.Is(err, ta.ErrNotArchived) == true
//         【并且】errors.Is(err, ta.ErrFooterCorrupt) == false  ← 关键：不能报成产物损坏
//         【并且】err != nil（绝不能把明文当正文返回）
//   理由：它是「响亮、需人工介入」的一类；隔离件里的正文【不能】通过正常路径读到。

// TestTraceBodyServiceQuarantineNameIsNotDoubleSuffixed  ← **无变异**（该形态的守卫在冻结包的
//   命名/路径精确性里，不在服务文件 ⇒ 它由集合的 CHECKS 行兜住，见步骤 3）
//   断言：隔离件路径【只能】由 partName(part, ".jsonl.zst") 生成 ⇒
//         文件名恰好是 part-000000.jsonl.zst，【不是】part-000000.jsonl.jsonl.zst。
//   做法：布置正确名字的文件 ⇒ ErrNotArchived；再布置 double-suffix 名字的文件 ⇒
//         服务必须【不】把它当 spool 读（它是 .jsonl.jsonl.zst，SpoolFile 是 .jsonl）
//         ⇒ 仍然是 ErrNotArchived（两条都 ErrNotArchived，但前者是「隔离件在正确位置」）。
//   这条同时固定了「序号 N 不被复用」的格式前提（writer.go 的既有注释）。

// TestTraceBodyServiceReadsUnsealedSpool  ← m02 的杀手
//   布置：<root>/spool/dt=D/inst=blue/part-000007.jsonl，含 3 行合法 JSON，每行以 \n 结尾。
//   断言：row=0/1/2 逐字节取回；Source==SourceSpool。
//   理由：规格 §8.2「未封块的近期 trace 直接读明文，无需等待封块」。

// TestTraceBodyServiceSpoolFallbackWhenProductAbsent  ← 同 m02
//   只在 spool 放行、不放产物 ⇒ 必须读到（ReadsPart 优先，但不强制产物存在）。

// TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing  ← m01/m03 的杀手（**需要真实 zstd**）
//   布置：产物存在且 footer.row_count=3（用真实 Sealer 封 3 行；本机需 zstd，见下）。
//   断言：row=3 与 row=999 => ErrNotArchived（【不是】ErrBodyMissing、【不是】404 类语义）。
//   判据：行号越界是「元数据与存储不一致」，不是「正文从未落盘」。
//   【本机 zstd 前置】封块需要 C 版 zstd CLI（方案 C）。用与计划 1 同一形态的
//   requireZstd(t) 守卫：缺失则 t.Skip —— 但 skip 会让三断言的 SKIP==0 失败，
//   因此本用例的封块部分必须【要么真跑、要么整包 skip 由 SKIP_WHITELIST 显式登记】。
//   ⚠️ 处置：本任务把「需要真实 zstd 的用例」与「不需要的用例」拆成【两个】测试函数，
//   并把前者登记进 SKIP_WHITELIST（附理由），后者必须真跑 —— 这样三断言依然有效。

// TestTraceBodyServiceMissingProductAndSpoolIsNotArchived  ← m01/m03 的杀手（**不需要 zstd**，两条变异都必须红在它身上）
//   布置：只写 PG 行（带定位键），文件系统上什么都没有
//   ⇒ ErrNotArchived（不是 404、不是 ErrBodyMissing、不是 ErrTraceBodyInconsistent）。

// TestTraceBodyServiceMarkerLineIsBodyMissingNotText  ← m05 的杀手
//   布置：spool 里第 1 行是字面量 null（AppendFailed 标记行形态）
//   ⇒ ErrBodyMissing（404 类），【绝不】返回字符串 "null"。
//   理由：读取侧逐字返回、不做 JSON 校验（reader.go:355-357 的注释），
//         少了识别就会把「从未落盘」伪装成「正文就是 null」。

// TestTraceBodyServiceCrossDayAndCrossPart  ← m04 的杀手
//   两个 dt（2026-10-08 / 2026-10-09）× 两个 inst（blue/green）× 两个 part（0/1）
//   共 8 个 spool；断言每个 (dt,inst,part,row) 取回的是【它自己】的那一行
//   （每行内容里编码唯一标识，逐字节比对）。
//   理由：定位键的 dt/inst/part 三分量任一被拼错/调换，都会读到另一个文件的行。

// 【已删除：原 TestTraceBodyServiceDayOfMatchesWriteSideZone】
//   原设计要注入一个 DayOf 做日期换算并断言跨日边界取到正确的目录；按全局约束
//   「读侧零日期计算」（E2），DayOf 注入已整体删除。dt 的口径正确性由上面这条
//   用例覆盖：行的 body_ref_dt 列值直接决定 dt= 目录，读侧没有任何换算可错。
//   相应地，CHECKS「读侧不自算日期」**保留**（现在表述为「读侧零日期计算」）。
```

- [ ] **步骤 2：运行测试**

```bash
zstd --version                     # 方案 C 前置：确认本机有 zstd（无则封块类用例走白名单 skip）
go test ./tests/observability/service/ -run 'TestTraceBodyService(Quarantine|ReadsUnsealed|SpoolFallback|RowOutOfRange|MissingProduct|MarkerLine|CrossDay)' -v
```
预期：**PASS**（需真实 zstd 的那条若本机无 zstd 则 SKIP 且在集合白名单内登记）。

- [ ] **步骤 3：写变异集合 `tools/trace-mutate/sets/p3-task-4.py`**

**本任务无新生产代码** ⇒ `TARGET` 指向**任务 2 的服务**（同计划 2 任务 8 的形态），**只有一个集合**（`p3-task-4.py`）。**不产出 `p3-task-4-handler.py`**：本任务不覆盖映射行为，产出它必为零覆盖（零覆盖的集合只能在 `EXPECTED_GREEN` 里空登记，不产生任何证据）。

**⚠️ 原稿的 m01–m08 目标串全部落在冻结包（`partPath := r.paths.PartFile(ref)`、`SpoolFile`、`isFailedRowMarker`、越界文案…）或网关侧（`t := ts.In(zone)`，该设计已按 E2 删除），而 `TARGET` 是服务文件 ⇒ 工具会 `PATTERN-MISS`（一条都跑不到），「8 条断言红」不可达。** 本版**全部改为服务文件里由任务 2 逐字钉住的构造**（任务 2 步骤 3 的「变异目标的唯一性约束」就是为此）；冻结包/网关侧的那些要求**降级为 CHECKS 行**（见下），并在报告里如实说明「该要求当前只有静态证据」。

```python
NAME = "p3-task-4"
TARGET = "internal/observability/service/trace_body_service.go"
PACKAGES = ["./tests/observability/service/"]
MANIFEST_FILES = ["tests/observability/service/trace_body_quarantine_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/service/trace_body_quarantine_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
# 需要真实 zstd（C 版 CLI）的用例；缺 zstd 时整包 SKIP，必须在此登记否则三断言失败
SKIP_WHITELIST = ["TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing"]

MUTATIONS = {}
# m01 冻结包哨兵被服务层换成 ErrBodyMissing ⇒ 隔离件 / 越界 / 缺文件三类「元数据与存储不一致」
#     被伪装成「正文从未落盘」（规格 §8.1 反复强调要消灭的一类错误）
#     杀手：TestTraceBodyServiceQuarantineYieldsNotArchived
#           （TestTraceBodyServiceMissingProductAndSpoolIsNotArchived 同根因也红；两条都
#            **不需要 zstd** ⇒ 本变异在无 zstd 的机器上也必须断言红）
MUTATIONS["m01_哨兵被换成body_missing"] = [
    ("sub", "\t\treturn nil, err\n", '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrBodyMissing, requestID)\n'),
]
# m02 Source 判定被写死成 archive ⇒ spool 兜底路径（未封块的近期正文）的来源诊断失真
#     杀手：TestTraceBodyServiceReadsUnsealedSpool（Source==SourceSpool 断言）
MUTATIONS["m02_source恒为archive"] = [
    ("sub", "return SourceSpool", "return SourceArchive"),
]
# m03 归档错误被降级成 jsonb 回退 ⇒ 越界 / 隔离件被伪装成「正文在 jsonb 列」
#     杀手：TestTraceBodyServiceMissingProductAndSpoolIsNotArchived（**不需要 zstd**）
MUTATIONS["m03_归档错误回退jsonb"] = [
    ("sub", "\t\treturn nil, err\n", "\t\treturn s.fromJSONB(trace)\n"),
]
# m04 定位键的 dt 分量被换成 inst 分量 ⇒ 拼出的目录名非法/指向别的目录（跨日串行）
#     杀手：TestTraceBodyServiceCrossDayAndCrossPart
MUTATIONS["m04_dt分量换成inst"] = [
    ("sub", 'DT:   trace.BodyRefDT.Format("2006-01-02"),', "DT:   *trace.BodyRefInst,"),
]
# m05 标记行（AppendFailed 的 null 行）的 ErrBodyMissing 被换掉 ⇒ 把「从未落盘」伪装成
#     「存储不一致」；同时证明服务层原样透传该哨兵、没有自己吞掉
#     杀手：TestTraceBodyServiceMarkerLineIsBodyMissingNotText
MUTATIONS["m05_标记行哨兵被换掉"] = [
    ("sub", "\t\treturn nil, err\n", '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrNotArchived, requestID)\n'),
]
```

**CHECKS（本任务的核心，规格 §7.1「不新增分支」的静态形式）：**
```python
CHECKS = [
  # 读侧生产代码里不得出现隔离件处理（规格 §7.1 明文：不为它新增分支或哨兵）
  {"name": "读侧无隔离件分支", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r"quarantine|\.jsonl\.jsonl\.zst", "expect": "absent",
   "why": "规格 §7.1：隔离件在 spool 目录、名字是 .jsonl.zst，两条精确路径都不命中它 ⇒ ErrNotArchived；不得为它加分支"},
  # 读侧零日期计算（dt 一律来自行内 body_ref_dt 列，E2）
  {"name": "读侧不自算日期", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r"FixedZone|LoadLocation|time\.Date\(|time\.Now\(", "expect": "absent",
   "why": "读侧零日期计算（全局约束 + E2）：dt 取自行内 body_ref_dt 列值，只做 Format(\"2006-01-02\") 布局格式化；自算日期会出现第二套口径"},
  # 冻结包侧的行为（隔离件发现规则、spool 兜底、双后缀不命中）**不在服务文件里** ⇒
  # 只能以静态行钉住冻结包的复用点；整目录零改动的权威判据是任务 9 的 git diff（D59）
  {"name": "冻结包spool兜底未被移除", "file": "internal/tracearchive/reader.go",
   "pattern": r"return readFromSpool\(spoolPath, ref\.Row\)", "expect": "present",
   "why": "任务 4 的 spool 兜底/隔离件形态依赖冻结包的这条精确路径（产物不命中 ⇒ 读 spool；隔离件名 .jsonl.zst 也不命中 ⇒ ErrNotArchived）。**该要求当前只有静态证据**（服务层无法为冻结包内部行为产出变异），报告必须逐条列出"},
  {"name": "冻结包标记行识别未被移除", "file": "internal/tracearchive/reader.go",
   "pattern": r"if isFailedRowMarker\(line\) \{", "expect": "present",
   "why": "标记行 ⇒ ErrBodyMissing 的识别在冻结包内（reader.go 两条路径共用）；服务层只透传该哨兵。**同样只有静态证据**，报告须列出"},
]
```

**⚠️ 已降级为 CHECKS 的原稿变异（逐条说明，不得静默丢弃）**：原 m01（隔离件回退读明文）、m02（隔离件路径拼成 double suffix）、m03（摘掉 spool 兜底）、m06（摘掉标记行识别）、m07（`Paths.SpoolFile` 的 dt/inst 调换）的目标串都在 `internal/tracearchive/` 内 —— 那是**冻结包**（本计划的第一硬约束），且工具一次只变异 `TARGET` 一个文件。其中：
- 隔离件分支 / double-suffix：**由「读侧无隔离件分支」这条 CHECKS 代替**（服务层本来就不允许有该分支）；
- spool 兜底 / 标记行识别 / `SpoolFile` 分量：**由上面两条冻结包 CHECKS ＋ 任务 2/4 的 `m02`（Source）与 `m05`（标记行哨兵）代替**——覆盖的是「读侧能不能正确用上」，而不是「冻结包内部是否实现」。
**不得**为了让这些变异可跑而改冻结包，也**不得**声称它们有变异证据（报告必须写明「只有静态证据」并逐条列出）。

- [ ] **步骤 4：跑变异集合**

```bash
python3 tools/trace-mutate/trace_mutate.py --repo . \
  --manifest .superpowers/sdd/2026-09-25-trace-point-read-service/p3-task-4-names.txt \
  --mutation-set tools/trace-mutate/sets/p3-task-4.py | tee /tmp/p3-task-4.out
```
预期：**5 条断言红**、三断言过、**无未登记绿**。**无 zstd 的机器**：`TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing` 按 `SKIP_WHITELIST` 整条 SKIP（不是「允许的绿」），m01/m03 的杀手仍落在**不需要 zstd** 的 `TestTraceBodyServiceMissingProductAndSpoolIsNotArchived` 上 ⇒ 5 条仍必须断言红；**权威表须在有 zstd 的机器上产出**，并在报告里注明本机 zstd 版本。**`EXPECTED_GREEN` 应为空**（原稿的「8 条」已按上一步的降级处置修正）。

- [ ] **步骤 5：提交**

```bash
git add tests/observability/service/trace_body_quarantine_test.go \
        tools/trace-mutate/sets/p3-task-4.py
git commit -m "test(observability): 隔离件/兜底/越界/跨日边界回归（计划 3/4 任务 4）

隔离件（spool 下的 part-N.jsonl.zst，内容是明文）⇒ ErrNotArchived，
且【不是】ErrFooterCorrupt、不返回明文；spool 兜底；行号越界 vs 真实缺失语义不同；
标记行 ⇒ ErrBodyMissing 不返回 \"null\"；跨 dt/inst/part 不串文件。
本任务只写测试；5 条变异全部打在任务 2 的服务文件上（冻结包侧要求 ⇒ 转 CHECKS，见集合头）。"
```

---

### Task 5：规格 §8.1 两条端点接线（`JWTAuth` + `InternalAuth`）

**文件：**
- 新建：`internal/observability/handler/trace_body_handler.go`
- 修改：`internal/controlplane/router/router.go`（注册两条路由 + 构造服务）
- 测试：`tests/observability/handler/trace_body_handler_test.go`（B 层，`httptest`）

**接口：**
- 依赖输入：任务 2（`TraceBodyService`）、任务 3（`MapTraceBodyError`）
- 对外产出：
  ```go
  type TraceBodyHandler struct {
      svc  *service.TraceBodyService
      // 归属判定与既有 trace 端点【同一套】：从 call_logs 反查 user_id/key_type，
      // 再用 rbac.CanViewPrompt 判可见性。注入成函数以便 B 层测越权，不真的连 rbac。
      subject  func(ctx context.Context, requestID string) (userID string, keyType string, found bool)
      canView  func(c *gin.Context, ownerUserID string, isProgramKey bool) bool
  }
  func NewTraceBodyHandler(svc *service.TraceBodyService, ...) *TraceBodyHandler

  // TraceBody        GET /api/v1/trace/:request_id/body          （JWTAuth，管理面）
  // InternalTraceBody GET /internal/v1/trace-body?request_id=... （InternalAuth，内部调用）
  func (h *TraceBodyHandler) TraceBody(c *gin.Context)
  func (h *TraceBodyHandler) InternalTraceBody(c *gin.Context)
  ```

  **两条端点的语义差异（必须写清，且各有用例）：**

  | 端点 | 鉴权 | 归属校验 | 可见性裁剪 | 响应 |
  |---|---|---|---|---|
  | `GET /api/v1/trace/:request_id/body` | `JWTAuth`（既有 `adminGroup` 形态） | **必须**（他人记录按不存在 404） | 提示词按 `rbac.CanViewPrompt`：**不可见时 403**（正文端点无法「部分裁剪」——它返回的就是提示词本身） | 200 = **正文 JSON 原文**（不是 Wrap；见下） |
  | `GET /internal/v1/trace-body?request_id=` | `InternalAuth`（`X-Internal-Token`） | **无**（服务间调用；沿用既有内部端点惯例） | 不裁剪 | 200 = **正文 JSON 原文** |

  **⚠️ 200 的响应体形状（本计划裁定，登记 D57 的配套）**：成功时返回**正文 JSON 原文**（`Content-Type: application/json`，body 就是 `TraceBody` 对象的字节），**不包一层 `{"body": ...}`**。理由：① 规格 §8.1 写「200 + 正文 JSON」；② 包一层会让调用方多一次解包，且与「逐字节」的验收叙事冲突（包一层就不再是原字节）。**这个决定必须有测试固定**（`Content-Length` 与 body 逐字节等于 service 返回值）。

  **⚠️ 403 vs 404 的裁定**：`ownOnly` 形态（`/my/*`）沿用既有端点的**404（不泄露存在性）**；但 `/api/v1/trace/:request_id/body` 是**全局**路由（不是 `/my/*`），按既有 `Trace` 端点的行为：**记录存在但提示词不可见 ⇒ 403**（既有 `MaskTracePrompt` 是「裁剪」，而本端点无法裁剪 ⇒ 必须 403 而不是返回空正文）。**不得**返回 200 + 空正文（那会让「无权看」与「正文为空」不可区分）。

  **⚠️ 200/503 的判据依赖「读侧禁用」的唯一来源（已核实，订正 E6；见全局约束「读侧键的存在性 / 空串语义」）**：`Disabled()` ⟺ `Root == ""`，而生产上它**只能**由 YAML 一侧产生（缺 `trace_body.root` 键 ⇒ 默认空串，或该键显式留空）—— `TRACE_BODY_ROOT=""` **不会**覆盖 YAML（空串被 vendored viper 跳过，`viper.go:444`）。因此本任务的两条端点必须按下面三格分别断言，且**不得**把第三格混进前两格：① 定位键全 NULL + `body_missing=false` ⇒ **200 + jsonb 正文**（功能关闭下的**正常**路径）；② 定位键全非 NULL + 读侧禁用 ⇒ **503 `trace_body_disabled`**；③ YAML 里仍留着 `root`（读侧启用）+ 定位键全非 NULL ⇒ **走归档**（200 或归档类错误，**既不是 503**，也**不是** jsonb 回退）。

  **⚠️ 本任务不做的事**：不改既有 `/usage/logs/:request_id/trace` 的行为（那是任务 6）；不新建鉴权中间件；不改 `tests/contract/contracts.json`（新增端点不在前端契约里——**但必须在任务 5 步骤 4 里核实 `contracts.json` 是否含 trace 端点**，若含则同步，见下）。

- [ ] **步骤 1：写失败的测试**

```go
// TestTraceBodyHandlerAdminRoute   ← m01 的杀手
//   httptest：JWTAuth 组内的 handler；注入假的 subject/canView（可见）⇒ 200，
//   且【逐字节】等于替身 reader 返回的正文；Content-Type=application/json。
// TestTraceBodyHandlerForbidsWhenPromptNotVisible  ← m02 的杀手
//   canView=false ⇒ 403，且响应体【不含】正文任何字节。
// TestTraceBodyHandlerNotFoundForUnknownRequest  ← m03 的杀手
//   subject 返回 found=false ⇒ 404（不泄露存在性），响应体不含正文。
// TestTraceBodyHandlerInternalRouteNeedsToken  ← m04 的杀手
//   不带 X-Internal-Token ⇒ 403（既有 InternalAuth 的行为）；带错 token ⇒ 403；带对 ⇒ 200。
// TestTraceBodyHandlerMapsErrors  ← m05 的杀手
//   让 service 返回每类错误（用替身 service 或真实服务 + 特定数据），
//   断言状态码 == MapTraceBodyError 的结果（含 404+body_missing=true、500、503）。
// TestTraceBodyHandlerQueryParamMissing  ← m06 的杀手
//   /internal/v1/trace-body 不带 request_id ⇒ 400（不是 500、不是空查询）。
```

- [ ] **步骤 2：运行测试确认失败** → 编译失败（handler 不存在）
- [ ] **步骤 3：实现 handler 与路由**

`internal/controlplane/router/router.go` 的改动（**接线位置**；`Setup` 内已有 `cfg := cpconfig.Load()`（`:41`）⇒ **不需要改 `cmd/control-plane/main.go`**）：
```go
// ① 构造（放在 usageH 附近，与既有 observability 接线同一段）
// 【符号事实】冻结包只有具体类型 *ta.Reader（reader.go:89 的 func NewReader(ReaderOptions) (*Reader, error)）；
// **冻结包没有「导出接口类型」可用**：它只导出具体类型（`ta.Reader`）—— 原稿写了一个不存在的 `ta.` 限定接口类型，接线片段无法编译。
// 【关键】Root 为空 ⇒ 读侧禁用：此时【不构造】Reader（NewReader 对空 Root 返回 error），
// 而是让 opts.Reader 保持 nil（服务在「有定位键 + 禁用」时报 ErrTraceBodyDisabled ⇒ 503）。
// 【接口细节】*ta.Reader 天然满足服务里未导出的窄接口 tracearchiveReader，直接赋值即可；
// **不要**写 `var tr *ta.Reader` 再无条件传进接口字段——nil 指针装进接口后 != nil，
// 服务会真的去调它并 panic。禁用时必须是「接口字段为 nil」。
tbOpts := obssvc.TraceBodyServiceOptions{Config: cfg.TraceBodyRead}
if !cfg.TraceBodyRead.Disabled() {
    r, rerr := ta.NewReader(ta.ReaderOptions{
        Root: cfg.TraceBodyRead.Root, ZstdWindowBytes: cfg.TraceBodyRead.ZstdWindowBytes,
        MaxConcurrent: cfg.TraceBodyRead.MaxConcurrent,
    })
    if rerr != nil {
        // 不新增 import：router.go 当前只 import context/log/slog/net/http/strings/time（无 fmt）
        panic("构造 tracearchive.Reader 失败: " + rerr.Error())
    }
    tbOpts.Reader = r
}
// 【注意】router.Setup(lifecycleCtx, db, rdb) *gin.Engine 的签名【不返回 error】
// ⇒ 上面用 panic 处理构造失败（与 Setup 现有的 panic("control-plane lifecycle context is required") 同风格），
//    **不得**写 `return err`（编译不过），也**不得**静默忽略（会让归档路径无声失效）。
tbSvc, err := obssvc.NewTraceBodyService(db, tbOpts)
if err != nil {
    panic("构造 TraceBodyService 失败: " + err.Error())
}
tbH := obshandler.NewTraceBodyHandler(tbSvc, /* subject/canView 注入既有实现 */)

// ② 注册（adminGroup 已在 JWTAuth 之下；internalGroup 已在 InternalAuth 之下）
adminGroup.GET("/trace/:request_id/body", guard(rbac.PermUsageViewAll), tbH.TraceBody)
internalGroup.GET("/trace-body", tbH.InternalTraceBody)
```
**⚠️ 无条件**：`TraceBodyServiceOptions` **没有** `DayOf` 字段（读侧零日期计算，全局约束 + E2）⇒ 接线里**不得**写「注入日期换算字段」的写法（`controlplane/config` 里没有那样的导出符号，写上去必然编译失败）。
**⚠️ 权限门（`guard`）的选择**：既有 `/usage/logs/:request_id/trace` 用的是 `guard(rbac.PermUsageViewAll)`（`router.go:759`）。本条端点**必须用同一个**（`PermUsageViewAll`），因为正文点读不比 trace 元数据更宽松。**不得**为了「好测」而放宽。`/my/*` 形态若将来需要，另开一条路由（本计划不做）。

- [ ] **步骤 4：运行测试与契约核实**
```bash
go test ./tests/observability/handler/ -run 'TestTraceBodyHandler' -v   # PASS
go build ./...
grep -n "trace" tests/contract/contracts.json | head   # 若含 trace 正文端点则同步；否则记录「无需更新」
```
**契约处置**：`tests/contract/contracts.json` 是 FE↔BE 契约参考（`AGENTS.md`）。本计划**新增**两条端点，其中管理面那条前端**尚未接入**（UI 属子项目 3）⇒ 步骤 4 的 `grep` 输出必须**原样抄进报告**，据以决定「是否同步」。**若既有契约里已有 `/usage/logs/:request_id/trace`，则任务 6 改动其响应语义时必须同步**（见任务 6 步骤 4）。

- [ ] **步骤 5：写变异集合 `tools/trace-mutate/sets/p3-task-5.py`**
`TARGET = "internal/observability/handler/trace_body_handler.go"`，`PACKAGES = ["./tests/observability/handler/"]`。

```python
NAME = "p3-task-5"
TARGET = "internal/observability/handler/trace_body_handler.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_handler_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/handler/trace_body_handler_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"

MUTATIONS = {}
# m01 归属校验被摘掉 ⇒ 任何人可凭 request_id 读任何人的提示词（IDOR）
#     杀手：TestTraceBodyHandlerNotFoundForUnknownRequest
MUTATIONS["m01_摘掉归属校验"] = [
    ("sub", "if !found {", "if false {"),
]
# m02 提示词不可见时不再 403，而是返回空正文 200 ⇒ 「无权看」与「正文为空」不可区分
#     杀手：TestTraceBodyHandlerForbidsWhenPromptNotVisible
MUTATIONS["m02_不可见时返回200空正文"] = [
    ("sub", "if !h.canView(c, userID, keyType == obsmodel.KeyTypeService) {", "if false {"),
]
# m03 内部端点漏掉 request_id 直接 500 ⇒ 入参错误被伪装成服务故障
#     杀手：TestTraceBodyHandlerQueryParamMissing
MUTATIONS["m03_缺参数不报400"] = [
    ("sub", "if requestID == \"\" {\n\t\tc.JSON(http.StatusBadRequest,", "if false {\n\t\tc.JSON(http.StatusBadRequest,"),
]
# m04 成功响应被包一层 {"body": ...} ⇒ 不再是原字节（逐字节契约破裂）
#     杀手：TestTraceBodyHandlerAdminRoute 的逐字节断言
#     ⚠️【字节级三断言的效力边界，登记 E31】m04 只替换 `c.Data` 那一行；紧邻的
#     `c.Header("Content-Length", strconv.Itoa(len(result.Body)))` 未动 ⇒ 断言①
#     （Content-Length == len 服务层返回值）**仍绿**，真正红的是「实际写出的字节数」
#     + 逐字节相等 + 顶层键集合三处 ⇒ **不得**把本条读成「三条字节级断言都被覆盖」。
MUTATIONS["m04_成功响应包一层"] = [
    ("sub", "c.Data(http.StatusOK, \"application/json\", result.Body)",
            "c.JSON(http.StatusOK, gin.H{\"body\": json.RawMessage(result.Body)})"),
]
# m05 错误映射被绕过（一律 500）⇒ 404/503 语义全丢
#     杀手：TestTraceBodyHandlerMapsErrors
MUTATIONS["m05_错误一律500"] = [
    ("sub", "status, resp := MapTraceBodyError(err)", "status, resp := 500, TraceBodyErrorResponse{}"),
]
# m06 权限门被放宽（管理面端点不再要求 PermUsageViewAll）
#     【已从 MUTATIONS 移除，降级为下面的 CHECKS 行】理由：该目标在 router.go，而本集合
#     TARGET 是 handler 文件 ⇒ 必然 PATTERN-MISS；且仓库没有任何 router.Setup 的测试
#     （`grep -rln "router.Setup" tests/` ⇒ 0 命中），没有可点名**顶层测试函数**的杀手
#     （工具的「杀死用例」列只到顶层用例名）。**不得**为造一条变异而新建 DB/Redis/rbac
#     全栈路由测试包（本任务不起真实 control-plane 进程）。
#
# ⚠️【登记 E34】上面 m01 / m02 / m04 三条的**替换串**已在实现轮按 E10 形态订正
#    （草稿的 `if false {` 会让 found / userID / keyType 变成「声明未使用」⇒ 编译红；
#     m04 草稿的 `json.RawMessage` 需要给「不序列化」的 handler 加 `encoding/json` 空引用）。
#    **目标串 / 变异名 / 语义一字未动**；实际集合以 `tools/trace-mutate/sets/p3-task-5.py`
#    头部逐字为准（m03 / m05 逐字采用草稿）。
```

**CHECKS：**
```python
CHECKS = [
  {"name": "复用既有鉴权中间件", "file": "internal/controlplane/router/router.go",
   "pattern": r"JWTAuth|InternalAuth", "expect": "present",
   "why": "规格 §2.2：不新建鉴权机制"},
  {"name": "不新建鉴权中间件", "file": "internal/observability/handler/trace_body_handler.go",
   "pattern": r"func .*Middleware|Authorization", "expect": "absent",
   "why": "鉴权只由既有中间件负责，handler 不解析 token"},
  # 原 m06 的静态形式：管理面点读端点必须带与既有 trace 端点同一个权限门
  {"name": "点读端点权限门未放宽", "file": "internal/controlplane/router/router.go",
   "pattern": r'adminGroup\.GET\("/trace/:request_id/body", guard\(rbac\.PermUsageViewAll\), tbH\.TraceBody\)',
   "expect": "present",
   "why": "既有 /usage/logs/:request_id/trace 用 guard(rbac.PermUsageViewAll)（router.go:759）；正文点读不得更宽松。**该要求当前只有静态证据**（仓库无 router.Setup 的测试，无法产出「摘掉就红」的变异），报告须逐条列出"},
  {"name": "冻结包GetBody签名未变", "file": "internal/tracearchive/reader.go",
   "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
   "expect": "present",
   "why": "本任务的服务由任务 2 注入冻结包 Reader；签名若变则接线失效。整目录零改动的权威判据是任务 9 的 git diff（D59）"},
]
```

- [ ] **步骤 6：跑变异集合** → 预期 **5 条断言红**、三断言过、无未登记绿（m06 已按上一步降级为 CHECKS，不再计入变异数）。
- [ ] **步骤 7：提交**

```bash
git add internal/observability/handler/trace_body_handler.go \
        internal/controlplane/router/router.go \
        tests/observability/handler/trace_body_handler_test.go \
        tools/trace-mutate/sets/p3-task-5.py
git commit -m "feat(controlplane): 点读两条端点接线（计划 3/4 任务 5）

GET /api/v1/trace/:request_id/body（JWTAuth + PermUsageViewAll + 归属判定 + 可见性 403）
GET /internal/v1/trace-body（InternalAuth）。200 = 正文 JSON 原字节（不包壳）。
鉴权机制零新增；读侧禁用时不构造 Reader（有定位键 ⇒ 503）。"
```

---

### Task 6：既有 trace 端点的正文改由点读服务提供（**计划 2 之后的归档形态修复**）

**文件：**
- 修改：`internal/observability/handler/usage_handler.go`（`trace()` 与 `ScopedTrace()`）
- 测试：`tests/observability/handler/usage_trace_archive_test.go`（B 层，`httptest` + 内存 sqlite）

**为什么必须有这一步（后果最直接、最容易被漏掉的一项）**：**当写侧功能开启（`TRACE_BODY_ROOT` 非空 ⇒ `cmd/gateway/main.go:275` 的 `sink` 非 nil）之后**，新行的 `request_headers` / `request_body` / `response_body` **恒为 NULL**（正文只进 `/datalog`）。而既有的三个 trace 端点（`router.go:759` / `:769` / `:1004`）**直接 `First(&trace)` 并把模型整体返回**（`usage_handler.go:137` 起）⇒ **管理面对所有新 trace 显示空正文**。

**⚠️ 这不是当下 `main` 上的回归（原稿此处是误述，已订正）**：
- 功能**关闭**时写入侧走既有路径，正文照旧填进 jsonb 三列，端点显示正常 —— 这也与本计划全局约束第 46 行「功能关闭 ⇒ 正文仍进 jsonb」自洽；
- 依据（已实测）：`internal/gateway/observability/trace_event.go:721-724` 是 `if w.sink == nil { traceEventToModel(e) }`（sink 为 nil 时走既有路径照填 jsonb）；`trace_body.go:82` 是 `Enabled = Root != ""`；`cmd/gateway/main.go:275-294` 只在 `Enabled` 时构造 sink。聚焦用例 `go test ./tests/gateway/observability -run TestTraceWriterLegacyPathKeepsBodiesInPG -count=1` ⇒ **PASS**（断言三列 NotNil、定位键全 Nil）；
- ⇒ 本条缺陷的**触发条件是「写侧已开启」**，而计划 4 的 **D73 硬门**又把开启推迟到「读侧可用」之后 ⇒ 本任务是**读侧上线前的必修项**（否则顺序颠倒会立刻变成用户可见的丢失），但**不得**在报告里宣称「`main` 上已经存在功能回归」。

**改动范围（**严格限定，不得扩大**）：**
- **只改**正文的来源：当「模型里的 3 个 jsonb 列都为 NULL」且「4 个定位键全非 NULL」时，用 `TraceBodyService` 取正文，并**回填到返回模型的 `RequestBody` / `ResponseBody` / `RequestHeaders` 字段** ⇒ **除 `body_missing` 的取值外，响应字段集合不变**（见下）。
- **绝不改**：归属判定（`traceSubject`）、`ownOnly` 语义、`rbac.CanViewPrompt` 的裁剪时机、`ScopedTrace` 的 `QueryScope` 校验、错误的既有形状。
- **正文取回失败时的行为（必须明确，本计划裁定）**：
  - `ErrBodyMissing` ⇒ **正文留空**，并**把既有的 `body_missing` 字段置 true**。**⚠️ 这不是新增响应字段**：`CallTrace.BodyMissing` **早已存在**（`internal/observability/model/call_trace.go:48`：`gorm:"column:body_missing;not null;default:false" json:"body_missing"`，**没有 `omitempty`**）⇒ **既有端点今天就已经在每条响应里返回该字段**（值为 `false`）。本任务改变的只是**它的取值语义**（归档形态下可能为 `true`），**响应字段集合逐字不变**。因此「前端零改动」成立，但**不成立**「响应逐字节不变」（原稿的两处表述都已按此订正，登记 E1）。
  - `ErrNotArchived` / 产物损坏 / 读侧禁用 ⇒ **正文留空 + 200**，并把**根因写进服务端日志**（`slog.Warn`，含 `request_id` 与 `Ref`）。**理由**：这个端点的用途是「看一条 trace 的全部信息」，元数据仍然有价值；把它整体变成 500 会让「正文读不到」升级成「trace 看不到」。**但日志必须是响亮的一条**（这是唯一可观测信号）。
    **⚠️ 这条取舍必须写进报告**：它意味着本端点**不**区分「正文缺失」与「存储故障」——需要精确语义的调用方**必须**用任务 5 的 `/api/v1/trace/:request_id/body`（那里 500/404 严格映射）。

- [ ] **步骤 1：写失败的测试**

```go
// TestUsageTraceHydratesBodyFromArchive  ← m01 的杀手
//   一行：jsonb 三列全 NULL + 定位键全非 NULL + 替身 reader 返回固定字节 ⇒
//   响应 JSON 的 request_body/response_body/request_headers 等于归档三键的内容。
// TestUsageTraceKeepsJSONBWhenNoRefs  ← m02 的杀手
//   一行：jsonb 有值 + 定位键全 NULL（功能关闭形态）⇒ 正文来自 jsonb，【不调用】reader
//   （断言替身调用次数 == 0 —— 这是 m02 的隔离探针形式）。
// TestUsageTraceMarksBodyMissing  ← m03 的杀手
//   一行：body_missing=true ⇒ 正文留空 + 响应含 "body_missing":true。
// TestUsageTraceArchiveFailureKeeps200  ← m04 的杀手
//   替身返回 ErrNotArchived ⇒ 仍然 200、正文留空、且【产生一条 slog.Warn】
//   （用 testlogger 捕获；日志存在性是承重的可观测信号）。
// TestUsageTracePreservesRBACOnArchivedBody  ← m05 的杀手
//   canViewPrompt=false ⇒ 归档正文【不得】出现在响应里（裁剪必须作用于新来源）。
//   这一条是安全承重：正文来源换了，裁剪时机若被挪到取正文之前就会漏。
// TestScopedTracePreservesScopeOnArchivedBody  ← m06 的杀手
//   范围外记录 ⇒ 仍是 404（不泄露存在性），且【不发生】任何 reader 调用。
// TestUsageTraceResponseKeySetUnchanged  ← 【E19 新增，承重用例】响应键集合不被改造动过
//   两格固定输入，逐字断言 200 响应的**顶层键集合**（对固定输入断言完整键列表）：
//     ① 归档形态行（jsonb 三列全 NULL + 定位键全非 NULL + 替身 reader 返回固定正文）⇒
//        {request_id, ts, request_headers, request_body, response_body,
//         body_ref_dt, body_ref_inst, body_ref_part, body_ref_row, body_missing}
//     ② 功能关闭形态行（jsonb 三列有值 + 定位键全 NULL + body_missing=false）⇒
//        {request_id, ts, request_headers, request_body, response_body, body_missing}
//   两格都额外断言**不存在** `ref` / `source` / `body` / `trace` 这些本计划可能引入的包装键。
//   判据：键集合只由 `obsmodel.CallTrace` 的既有 json tag 决定（omitempty 只决定「有值的键
//   是否出现」），本任务只改**取值**（归档正文回填），不引入任何新键。
//   【为什么必须有（登记 E19）】任务 2 新增了 `TraceBodyResult.Ref`（含 dt/inst/part/row
//   定位键）。若本任务的模型回填把 `res.Ref` 并进 `CallTrace`、或给模型加了 `ref` 字段、
//   或把响应换成包壳结构，定位键就会**泄漏进既有 trace 端点的响应**（前端与契约都会漂移）。
//   可失败：给 `CallTrace` 加任何带 json tag 的字段（如 `Ref ta.Ref `json:"ref,omitempty"``）、
//   把 `c.JSON(http.StatusOK, trace)` 换成包壳（`gin.H{"trace": trace}`）、或把 res.Ref/res.Source
//   并进模型 ⇒ 本用例红。**无对应变异**（原因见步骤 5 的说明）。
// TestLookupTraceSubjectRejectsUnknownOrEmptyOwner  ← 【E32 承接，m07 的杀手】
//   归属反查 `LookupTraceSubject` 的 **fail closed** 行为证据（此前**零用例零变异**：
//   `grep -rn "traceSubject\|LookupTraceSubject\|\.Trace(\|ScopedTrace" tests/` ⇒ 0 命中）。
//   两格 + 一条阳性对照：
//     ① call_logs 无该 request_id 行 ⇒ 既有端点（`MyTrace`，以全局 actor 走到底）404，
//        且 **hydrate 服务调用次数 == 0**（拒绝发生在取正文之前）；
//     ② call_logs 有行但 `user_id` 为空 ⇒ 同上（这是 fail closed 的第二个分支）；
//     ③ 阳性对照：call_logs 行完整（`user_id` 非空且等于 actor）⇒ `MyTrace` 200
//        且 hydrate 服务**恰被调用一次**（证明 ①② 的 404 来自归属判据，
//        而不是「该端点永远不 200」这种恒真形态）。
//   点读端点一侧用**生产装配**验证同一个判据：`subject` 注入
//   `func(ctx, id) { return obshandler.LookupTraceSubject(ctx, db, id) }`（与 router.go 逐字同形）
//   ⇒ 归属不满足时同样 404，且替身 reader 调用次数 == 0。
//   可失败：摘掉 `row.UserID == ""` 这条 fail-closed 判据（m07）⇒ ① 在有 call_logs 行时
//   转为 200、② 在全局 actor 下转为 200/200，本用例红。
```

- [ ] **步骤 2：运行测试确认失败** → 断言红（现有实现返回的正文为空）
- [ ] **步骤 3：实现（最小改动）**
```go
// usage_handler.go 的 trace() 内，在既有 First(&trace) 之后、
// MaskTracePrompt(&trace, canViewPrompt) 之【前】插入：
//   hydrateTraceBody(ctx, &trace) —— 只在 (3 列全 NULL && 4 键全非 NULL) 时调服务；
//   失败只记日志 + 置 body_missing 标志，【不改变状态码】。
// 关键：hydrate 必须在 MaskTracePrompt 之前（否则归档正文绕过裁剪）。
//      这条由 TestUsageTracePreservesRBACOnArchivedBody 固定，并由 m05 的变异红证明。
```
`UsageHandler` 需要新增一个**可选**依赖（`tbSvc`）与 setter（`SetTraceBodyService`），**不得**改 `NewUsageHandler` 的既有签名（**既有 2 处调用点**：`router.go:756`（管理面使用统计组）与 `router.go:992`（rollup 组）⇒ 改签名会波及无关代码）。**订正**：原稿写的「4 处调用点 `router.go:756/992/1022`」是错的 —— `:1022` 是 `/usage/analysis-report` 的 `NewReportHandler(db)`，**不是** `NewUsageHandler`（复核：`grep -n 'NewUsageHandler' internal/controlplane/router/router.go` ⇒ 仅 756/992 两处）。

- [ ] **步骤 4：运行测试 + 契约同步判定**
```bash
go test ./tests/observability/handler/ -run 'TestUsageTrace|TestScopedTrace' -v   # PASS
go build ./...
```
**契约（`tests/contract/contracts.json`）**：本任务**没有新增响应字段**（`body_missing` 早已存在于 `call_trace.go:48` 且无 `omitempty`，既有响应本来就每条都带它）——改变的只是**该字段的取值语义**（归档形态下 `true` 开始出现）。⇒ 复核命令：`grep -n "trace" tests/contract/contracts.json`；若该文件里含 `/usage/logs/:request_id/trace` 的响应 schema，**必须**同步「取值语义」这一句并在提交里一并带上；若不含（当前实测输出只有 `prompt_visibility` 一段说明文字，无端点 schema），把 `grep` 输出原样抄进报告并注明「无需更新」。**判定必须由命令输出支撑，不得只写结论。**

- [ ] **步骤 5：写变异集合 `tools/trace-mutate/sets/p3-task-6.py`**

```python
NAME = "p3-task-6"
TARGET = "internal/observability/handler/usage_handler.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/usage_trace_archive_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/handler/usage_trace_archive_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"

MUTATIONS = {}
# m01 归档回填被摘掉 ⇒ 新行的正文在管理面永远是空的（本任务要修的那个回归）
#     杀手：TestUsageTraceHydratesBodyFromArchive
MUTATIONS["m01_不回填归档正文"] = [
    ("sub", "if err := h.hydrateTraceBody(c.Request.Context(), &trace); err != nil {",
            "if err := error(nil); err != nil {"),
]
# m02 无条件回填 ⇒ 功能关闭/历史行被空结果覆盖（把 jsonb 里的正文抹掉）
#     杀手：TestUsageTraceKeepsJSONBWhenNoRefs
MUTATIONS["m02_无条件回填"] = [
    ("sub", "if !traceHasArchiveRef(&trace) {", "if false {"),
]
# m03 body_missing 标志不再下发 ⇒ 前端无法区分「空」与「从未落盘」
#     杀手：TestUsageTraceMarksBodyMissing
MUTATIONS["m03_不下发body_missing标志"] = [
    ("sub", "trace.BodyMissing = true", "trace.BodyMissing = trace.BodyMissing"),
]
# m04 读取失败时不再记日志 ⇒ 唯一的可观测信号消失
#     杀手：TestUsageTraceArchiveFailureKeeps200（日志断言）
MUTATIONS["m04_失败不记日志"] = [
    ("sub", "slog.Warn(\"trace body hydrate failed\"", "slog.Debug(\"trace body hydrate failed\""),
]
# m05 回填被挪到 MaskTracePrompt 之后 ⇒ 归档正文绕过提示词裁剪（越权）
#     杀手：TestUsageTracePreservesRBACOnArchivedBody
MUTATIONS["m05_回填绕过裁剪"] = [
    ("cut", "\tif err := h.hydrateTraceBody(c.Request.Context(), &trace); err != nil {",
            "\tMaskTracePrompt(&trace, canViewPrompt)"),
]
# m06 范围校验被挪到取正文之后 ⇒ 范围外请求也读了归档（信息泄露 + 无谓 IO）
#     杀手：TestScopedTracePreservesScopeOnArchivedBody（reader 调用次数 == 0）
MUTATIONS["m06_范围外也取正文"] = [
    ("sub", "if !inScope {", "if false {"),
]
```

**CHECKS（本任务的静态不变量）：**
```python
CHECKS = [
  # 回填必须发生在裁剪之前（安全承重）——变异 m05 只能证明「挪到 MaskTracePrompt 之后会红」；
  # 顺序是结构事实，无法用「摘掉某个判定」表达 ⇒ 静态钉住
  {"name": "回填在裁剪之前", "file": "internal/observability/handler/usage_handler.go",
   "pattern": r"if err := h\.hydrateTraceBody\(c\.Request\.Context\(\), &trace\); err != nil \{[\s\S]{0,300}?\n\tMaskTracePrompt\(&trace, canViewPrompt\)",
   "expect": "present",
   "why": "hydrate 必须在 MaskTracePrompt 之前，否则归档正文绕过提示词裁剪（越权）。**该要求当前只有静态证据**（顺序无法用单点替换表达），报告须列出"},
  {"name": "不改NewUsageHandler签名", "file": "internal/observability/handler/usage_handler.go",
   "pattern": r"func NewUsageHandler\(", "expect": "present",
   "why": "既有 2 处调用点（router.go:756/992）不得被波及；新增依赖走 SetTraceBodyService setter"},
  {"name": "冻结包GetBody签名未变", "file": "internal/tracearchive/reader.go",
   "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
   "expect": "present",
   "why": "回填经 TraceBodyService 调用冻结包 Reader；签名若变则接线失效。整目录零改动的权威判据是任务 9 的 git diff（D59）"},
  # 【E19 新增的两行】响应键集合不变（防 `TraceBodyResult.Ref` / 定位键泄漏进既有端点）。
  # 该要求**没有对应变异**：泄漏来自**设计层改动**（给模型加字段 / 换包壳结构），而不是某条
  # 可摘掉的判定；且两处 200 返回点的串 `c.JSON(http.StatusOK, trace)` 在同一文件里出现
  # **2 次**（`trace()` 与 `ScopedTrace()`）⇒ 无法做唯一目标的 `sub`。故只有静态检查 +
  # `TestUsageTraceResponseKeySetUnchanged` 的完整键列表断言（该断言确实可失败）。
  {"name": "trace响应仍是模型整体", "file": "internal/observability/handler/usage_handler.go",
   "pattern": r"c\.JSON\(http\.StatusOK, trace\)", "expect": "present",
   "why": "E19：200 的响应体是 `obsmodel.CallTrace` 本身（既有形态），**不得**换成包壳、也不得把 ref/source 并进去。可违反：改成 `gin.H{\"trace\": trace}` 或任何包装结构即 FAIL（两处返回点共用本行，命中一次即算通过）"},
  {"name": "模型未新增定位键泄漏字段", "file": "internal/observability/model/call_trace.go",
   "pattern": r"tracearchive|json:\"(ref|source|body|trace)\"", "expect": "absent",
   "why": "E19：定位键的唯一合法来源是四个**既有**的 `body_ref_*` 列（json tag 早已存在且带 omitempty）；模型**不得**引用冻结包定位键类型，也不得新增 `ref`/`source` 之类的 json 键 —— 否则 `TraceBodyResult.Ref` 会经模型泄漏进既有 trace 端点的响应。可违反：加 `Ref *ta.Ref `json:\"ref,omitempty\"`` 或任何新 json 键即 FAIL"},
]
```

**⚠️ 步骤 5 落地时的偏离登记口径以文末的 E37 行（含 E38 / E39）为准**：① `router.go` 的偏离**不在集合头部**，它登记在任务 6 报告 §9① 与代码注释 `internal/controlplane/router/router.go:473-484`；② `m01` 是**目标串**订正（替换串沿用草稿），写「替换串」是误称；③ `m05` 是**机制改动（`cut` → `sub` 交换）**而非替换串订正，理由是原 `cut` 会产出**恒绿**。另：`m03` 的 `ErrBodyMissing` 分支**可经真实服务命中**（不是「不可达 / 死代码」），依据见集合注记。

- [ ] **步骤 6：跑变异集合** → 预期 7 条断言红（**E37 订正**：原写「6 条」——集合按 E32 补入 `m07` 后为 7 条）、三断言过、无未登记绿。
- [ ] **步骤 7：提交**

```bash
git add internal/observability/handler/usage_handler.go \
        tests/observability/handler/usage_trace_archive_test.go \
        tests/contract/contracts.json \
        tools/trace-mutate/sets/p3-task-6.py
git commit -m "fix(observability): 既有 trace 端点正文改由点读服务提供（计划 3/4 任务 6）

写侧功能开启后新行 jsonb 恒为 NULL ⇒ 管理面对所有新 trace 显示空正文
（触发条件是「写侧已开启」，不是当下 main 的回归 —— 依据见任务正文）。
现在：jsonb 全 NULL 且定位键全非 NULL 时从归档回填，响应字段集合不变；
body_missing 早已存在（call_trace.go:48，无 omitempty），本任务只改其取值语义；
裁剪时机保持在回填之后（安全承重）。"
```

---

### Task 7：CLI `cmd/trace-body`（运维与验收工具）

**文件：**
- 新建：`cmd/trace-body/main.go`
- 测试：**新建包** `tests/tools/trace_body_cli/trace_body_cli_test.go`（B 层）。**⚠️ 订正**：原稿写 `tests/tools/trace_body_cli_test.go`，但 `tests/tools/` 下**没有 Go 包**（实测：该目录只有子目录 `usage_report_client/`，`ls tests/tools/*.go` ⇒ 无）⇒ `PACKAGES = ["./tests/tools/"]` 会跑不出任何用例（`[no tests to run]` ⇒ 证据无效）。本任务改为与既有 `tests/tools/usage_report_client/` 同一形态的新子包。CLI 逻辑不可导入时只测可导入的纯函数（放 `internal/observability/service/trace_body_cli.go`）

**接口：**
- 依赖输入：任务 2（`TraceBodyService`）
- 规格依据：§4.2 的「CLI：`cmd/trace-body/`（新）— 运维与验收工具」
- 对外形态：
  ```
  trace-body --root <dir> --dsn <postgres-url> --request-id <uuid> [--json|--raw|--ref]
    退出码：0 = 取到正文；3 = 正文缺失（body_missing）；4 = 未归档/需要人工介入；
            5 = 读侧未配置；1 = 用法/参数错误；2 = 其它错误
    输出：--raw（默认）逐字节写 stdout（可管道给 jq）；--json 输出
          {"source":"archive","bytes":1234,"sha256":"..."}；--ref 输出定位键四分量
          dt/inst/part/row（**取自服务导出的 `TraceBodyResult.Ref`**，见任务 2 的导出途径与 E14）
  ```
  **为什么要有 CLI**：① 规格 §4.2 明确要求；② **本计划的验收（任务 8）与生产排障需要一条不经过 HTTP/鉴权的直连路径**（否则「是归档读不出来」还是「是 HTTP 层的问题」无法分离）；③ 与既有 `cmd/trace-calib` 同一形态（仓库已有运维 CLI 先例）。
  **⚠️ CLI 的安全约束（必须写明）**：它**不做**任何归属/权限判定（它是运维工具，用 DSN 直连）⇒ **文档与 `--help` 必须写明「仅限运维/本机使用，不得暴露为服务」**；**不得**把 DSN 或正文写进日志。
  **⚠️ 实现约束**：CLI **必须复用 `TraceBodyService`**（同一判定顺序、同一哨兵映射），**不得**自己查库 + 自己拼路径（否则 CLI 会漂移成一个「第二个读实现」，验收就不再代表服务的行为）。这条由 CHECKS 钉死。
  **⚠️ `--ref` 的定位键也必须来自服务层导出（订正 E14）**：任务 7 原本要求 `--ref` 输出 dt/inst/part/row 四分量，而任务 2 的 `TraceBodyResult` 当时**只有 `Body`/`Source`** ⇒ 没有导出途径时，CLI 唯一能写出四分量的办法是**自己查 `call_traces` 的四列**（`body_ref_dt` / `body_ref_inst` / `body_ref_part` / `body_ref_row`）再拼 —— 这会让「什么算合法定位键」出现**第二份实现**，正是 **D59** 警告的形态。⇒ 任务 2 增加了导出途径 **`TraceBodyResult.Ref`**（归档/spool 命中路径填充四分量，jsonb 回退为零值）；**`--ref` 必须只读这个字段**，**不得**在 CLI 里出现那四个列名或 `tracearchive.` 的路径拼接。**⚠️ `--ref` 的打印/取值留在 `cmd/trace-body/main.go`**（CHECKS 的作用域是单文件）；若执行者把 ref 格式化抽进 `internal/observability/service/trace_body_cli.go`，必须把下面两条 CHECKS 行**镜像**到该文件，否则检查会空转。

- [ ] **步骤 1：写失败的测试**

```go
// TestTraceBodyCLIExitCodes  ← m01/m02 的杀手
//   表驱动：把「service 返回什么错误」映射到「退出码」，断言 3/4/5 分别对应
//   ErrBodyMissing / ErrNotArchived / ErrTraceBodyDisabled。
// TestTraceBodyCLIParsesFlags  ← m03 的杀手
//   --root 必填、--dsn 必填、--request-id 必填；缺任一 ⇒ 退出码 1（不是 panic）。
// TestTraceBodyCLIRawWritesExactBytes  ← m04 的杀手
//   --raw 下 stdout 逐字节等于 service 返回的 Body（不额外加换行）。
// TestTraceBodyCLIRefModePrintsAllFourComponents  ← m05 的杀手
//   --ref 输出必须含 dt/inst/part/row【四个】分量（少一个就没法排障），
//   且四分量**逐字等于服务返回的 `TraceBodyResult.Ref`**（订正 E14：设一份带非零
//   Ref 的结果，断言 CLI 打印的就是它 —— 只关心「有四个分量」会漏掉「取自哪里」，
//   而那正是绕行（自己查四列）会满足、却必须被拦住的部分）。
```

- [ ] **步骤 2：运行测试确认失败**
- [ ] **步骤 3：实现**（**复用 service**；把退出码映射抽成**可导入的纯函数** `ExitCodeFor(err error) int`，这样 B 层能测而不必 exec 进程）
- [ ] **步骤 4：运行测试确认通过**
```bash
go test ./tests/tools/trace_body_cli/ -run 'TestTraceBodyCLI' -v    # PASS
go build ./... && go vet ./cmd/trace-body/
```

- [ ] **步骤 5：写变异集合 `tools/trace-mutate/sets/p3-task-7.py`**

**⚠️ 前置**：若退出码映射放在 `cmd/trace-body/main.go`（`package main`，**不可导入**），则变异工具**无法**在 B 层用例上验证它（`PACKAGES` 只列 `./tests/...`）。**因此本计划的硬要求是：把 `ExitCodeFor` 与参数解析抽到可导入位置**（推荐 `internal/observability/service/trace_body_cli.go`），`main.go` 只做 `os.Exit(service.ExitCodeFor(err))`。**若执行者判断无法这样拆分，必须停下并报告**（否则本任务的变异证据无法产出 ⇒ 按全局约束，该任务无有效验证手段）。

```python
NAME = "p3-task-7"
TARGET = "internal/observability/service/trace_body_cli.go"
PACKAGES = ["./tests/tools/trace_body_cli/"]
MANIFEST_FILES = ["tests/tools/trace_body_cli/trace_body_cli_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/tools/trace_body_cli/trace_body_cli_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
MUTATIONS = {}
# m01 正文缺失与未归档的退出码被合并 ⇒ 排障无法区分「从未落盘」与「文件不在」
#     杀手：TestTraceBodyCLIExitCodes
MUTATIONS["m01_合并缺失与未归档退出码"] = [
    ("sub", "if errors.Is(err, ta.ErrBodyMissing) {\n\t\treturn 3\n\t}", "if false {\n\t\treturn 3\n\t}"),
]
# m02 读侧未配置不再单独退出码 ⇒ 部署错误被报成存储错误
#     杀手：同上
MUTATIONS["m02_禁用退出码合并"] = [
    ("sub", "if errors.Is(err, ErrTraceBodyDisabled) {\n\t\treturn 5\n\t}", "if false {\n\t\treturn 5\n\t}"),
]
# m03 缺必填参数变成 panic 而不是退出码 1
#     杀手：TestTraceBodyCLIParsesFlags
MUTATIONS["m03_缺参数panic"] = [
    ("sub", 'if *requestID == "" {\n\t\treturn nil, errors.New("--request-id 必填")',
            'if *requestID == "" {\n\t\tpanic("missing")'),
]
# m04 --raw 多写一个换行 ⇒ 管道给 jq 会失败（逐字节契约破裂）
#     杀手：TestTraceBodyCLIRawWritesExactBytes
MUTATIONS["m04_raw多写换行"] = [
    ("sub", "w.Write(body)", 'w.Write(append(body, \'\\n\'))'),
]
# m05 --ref 少输出一个分量 ⇒ 排障信息不完整
#     杀手：TestTraceBodyCLIRefModePrintsAllFourComponents
MUTATIONS["m05_ref少输出分量"] = [
    ("sub", 'fmt.Fprintf(w, "%s/%s/part-%06d#row=%d\\n", ref.DT, ref.Inst, ref.Part, ref.Row)',
            'fmt.Fprintf(w, "%s/%s/part-%06d\\n", ref.DT, ref.Inst, ref.Part)'),
]
```

**CHECKS（本任务最关键的静态要求）：**
```python
CHECKS = [
  # CLI 必须复用服务，不得自己查库/拼路径（否则会漂移成第二个读实现）。
  # 【订正 E14】原行的 `tracearchive\.` 只拦得住「直接引用冻结包拼路径」，
  # **拦不住** `--ref` 最可能走的旁路：自己查 call_traces 的四个定位键列再拼。
  # 【订正 E18·本行正则已被加强】E14 的禁用项仍拦不住「用 GORM 模型字段绕行」这一形态：
  #   var tr obsmodel.CallTrace
  #   db.Where("request_id = ?", id).First(&tr)
  #   return ta.Ref{DT: tr.BodyRefDT.Format("2006-01-02"), Inst: *tr.BodyRefInst,
  #                 Part: *tr.BodyRefPart, Row: *tr.BodyRefRow}
  # 这四行**三处全不命中**原正则：① 表名由模型推导 ⇒ 正文里没有 `call_traces` 字面量；
  # ② Go 字段名 `BodyRefDT` 与列名 `body_ref_dt` 不同形 ⇒ 四个列名也都命中不了；
  # ③ 别名 import（`obsmodel "…/internal/observability/model"`）之后正文里既没有
  #    `tracearchive.` 也没有 `call_traces`。它绕过了服务层的优先级判定 / ValidateRef，
  # 是 D59「同一判据两处实现」的正宗形态。⇒ 把「模型字段 / 别名限定符 / 直接构造
  # `ta.Ref{}`」一并列入禁用项。
  {"name": "CLI复用服务不自己拼路径也不直查定位键列", "file": "cmd/trace-body/main.go",
   "pattern": r"tracearchive\.|body_ref_dt|body_ref_inst|body_ref_part|body_ref_row|call_traces|obsmodel\.|BodyRefDT|BodyRefInst|BodyRefPart|BodyRefRow|ta\.Ref\{",
   "expect": "absent",
   "why": "CLI 必须经 TraceBodyService 取正文与定位键（`TraceBodyResult.Ref`）；直接引用冻结包、自己查 call_traces 的四列、**用 `obsmodel.CallTrace` 的 `BodyRefDT/BodyRefInst/BodyRefPart/BodyRefRow` 字段取四分量、或在 CLI 里直接构造 `ta.Ref{…}`**，都会绕过服务层的优先级判定/ValidateRef ⇒ 出现第二份判据（D59 的形态）。可违反：正文出现上述任一标识符即 FAIL。**残余（如实登记）**：若有人把模型字段再改名（如 `tr.RefDT`）或用 `Table(\"call_\"+\"traces\")` 之类的拼接隐藏表名，本行仍拦不住——那已属刻意规避，由下面的正向检查兜底"},
  # 【订正 E18·新增的正向检查】只留禁用项不够：下面的 `\.Ref\b` 会被自造的
  # `return ta.Ref{...}` **满足**（`ta.Ref{` 里就含 `.Ref`），因此它单独起来起不到
  # 「防随手构造」的作用。正途的唯一客观判据是「CLI 真的构造了服务」。
  {"name": "CLI必须调用服务构造", "file": "cmd/trace-body/main.go",
   "pattern": r"NewTraceBodyService\(", "expect": "present",
   "why": "正向钉住「取正文与 `--ref` 真的走了服务层」（任务 7 步骤 3 的硬要求：复用 TraceBodyService，不得自己查库 + 自己拼路径）。**这一行是 E18 的承重项**：禁用项只能证明「没用这些绕行写法」，只有它要求服务被真正构造出来；两者合起来才排除「CLI 自造 ta.Ref」。可违反：删掉服务构造、改为直接查库即 FAIL"},
  # `--ref` 的四分量必须取自服务导出，而不是自己拼（与上一行互为补充：上一行禁旁路，
  # 这一行钉住正途；只留禁行会让「打印一个随手构造的 ta.Ref」蒙混过关）。
  {"name": "CLI的ref取自服务导出", "file": "cmd/trace-body/main.go",
   "pattern": r"\.Ref\b", "expect": "present",
   "why": "钉住「--ref 打印的是服务导出的 ta.Ref 四分量」（任务 2 的 `TraceBodyResult.Ref`）。**⚠️ 本行的效力边界（订正 E18）**：`\.Ref\b` 会被自造的 `ta.Ref{…}` 满足（`ta.Ref{` 里就含 `.Ref`）⇒ 它单独**不能**证明「ref 取自服务」；真正承重的是上面两行（加强后的禁用项 + `NewTraceBodyService(`）。若 ref 格式化被抽到 `internal/observability/service/trace_body_cli.go`，本页前三行都必须镜像到该文件"},
  # 【原稿的 r'Print.*(dsn|DSN)|log.*(dsn|DSN)' 不合格】`fmt.Fprintln(os.Stderr, "usage: … --dsn …")`
  # 会同时命中 `Print` 与 `dsn` ⇒ 用法文本一写就恒 FAIL。改成只禁「把 dsn 的值送进日志」。
  {"name": "CLI不把DSN写进日志", "file": "cmd/trace-body/main.go",
   "pattern": r'"dsn"\s*,\s*\*?dsn|dsn\s*=\s*%[svq]', "expect": "absent",
   "why": "DSN 含凭据，不得进日志（命中形态：`slog.Info(..., \"dsn\", dsn)` 或 `Printf(\"dsn=%s\", dsn)`）。**注**：`--help`/usage 文本里出现 `--dsn` 字样是允许的（原稿的正则 `Print.*(dsn|DSN)` 会把它误判为违规 ⇒ 恒 FAIL）；usage 文本请用 `--dsn <url>` 形式，不要写 `--dsn=%s`"},
  {"name": "CLI写明仅限运维", "file": "cmd/trace-body/main.go",
   "pattern": r"仅限运维|不得暴露为服务", "expect": "present",
   "why": "CLI 不做归属/权限判定（用 DSN 直连）⇒ 必须自带这条使用告示（可被违反：删掉即 FAIL）"},
  {"name": "冻结包GetBody签名未变", "file": "internal/tracearchive/reader.go",
   "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
   "expect": "present",
   "why": "CLI 经 TraceBodyService 调用冻结包 Reader；签名若变则接线失效。整目录零改动的权威判据是任务 9 的 git diff（D59）"},
]
```

- [ ] **步骤 6：跑变异集合** → 预期 5 条断言红、三断言过、无未登记绿。
- [ ] **步骤 7：提交**

```bash
git add cmd/trace-body/main.go internal/observability/service/trace_body_cli.go \
        tests/tools/trace_body_cli/trace_body_cli_test.go tools/trace-mutate/sets/p3-task-7.py
git commit -m "feat(cmd): trace-body 点读 CLI（计划 3/4 任务 7）

复用 TraceBodyService（同一判定顺序与哨兵映射，CHECKS 禁止直接引用冻结包）。
退出码：0 取到 / 3 正文缺失 / 4 未归档 / 5 读侧未配置 / 1 用法错。
--raw 逐字节（可管道 jq）、--json 摘要、--ref 输出四分量。仅限运维使用。"
```

---

### Task 8：验收 —— **A2（封块产物可点读、索引正确）** 的生产形态往返

**文件：**
- 新建：`tests/observability/service/trace_body_acceptance_test.go`（B 层）
- 不改任何生产代码

**⚠️ 【E33 承接，与 A2 并列的新增项】真实 `router.Setup` 装配的行为验收（两态）**：任务 5 交付报告 §6.5 的三条「只有静态证据」项此前**既无行为证据、也无承接方**（任务 9 只做冻结包 `git diff` + 集合重跑；本任务原稿只做服务层验收）。⇒ **本任务承接**：
- **真实 `router.Setup` 装配，`trace_body.root` 启用 / 禁用两态**：读侧**启用**时，带定位键的行经装配后的路由拿到归档正文；读侧**禁用**时（YAML 缺 `trace_body.root`），同样的行得到 **503 `trace_body_disabled`**（不是 500、不是 jsonb 回退、不是 404）。
- **权限门 `PermUsageViewAll`**：装配后的路由上，「有 token 但无该权限」的 actor 被既有 `guard` 拒绝（**不得**更宽松）；「无 token」被既有 `JWTAuth` 拒绝。
- **不新建鉴权中间件**：其**不存在性**仍由任务 5 的 CHECKS 承重（本项只补行为侧：认证/授权结果与既有中间件一致，不出现第二套凭据解析）。
- **验收形式**：新增用例（文件与规模由本任务自行决定，仍**不改任何生产代码**）；`trace_body.root` 的两态由**注入的配置**分别驱动。
- **兜底 = 任务 9**：若本任务因 DB/Redis 依赖不可得而**未**产出该用例，任务 9 **必须**把该缺口逐条写进 `R11-v1.md` 的「覆盖缺口」，**不得静默**。⚠️ 本条**不是**复核任务 5 的 m06 降级裁定 —— m06 降级的是「为一个断言造变异」的**变异**形态，本条要的是**行为验收**。
- **残余（如实写进报告）**：本项**不**覆盖真实 PG/Redis（内存 sqlite 或注入替身），也**不**声称覆盖「未新建鉴权中间件」的不存在性（那只有 CHECKS）。

**验收范围（**本计划只承担 A2，其余显式排除**）：**

| 规格 §14.2（**按计划 2 Task 8 的 A 表**，见 D56） | 内容 | 归属 |
|---|---|---|
| A1 | 写 1,000,000 条 → 随机抽 1,000 条读回 → 逐字节一致 | **写入半部 = 计划 2 已完成**；**读回半部 = 本计划** |
| **A2** | **封块产物可点读、索引正确** | **本计划** |
| A3 | 把 `/datalog` 填满 ⇒ 退化到 `body_missing`、网关继续服务 | 计划 2（注入式）+ 计划 4（真实盘满） |
| **A4** | 读取 1000 次后目录占用增量 == 0 | **计划 1 已覆盖**（`tests/tracearchive/constraints_test.go`）；本计划**不重验、不声称**（见 E3） |
| **A5** | 篡改 footer 一个字节 ⇒ 读取返回 500 + 诊断，**不**返回「不存在」 | **本计划承担其对外映射与分类**：任务 2 的哨兵透传（`TestTraceBodyServicePassthroughSentinels` 用替身返回 `ErrFooterCorrupt`）＋ 任务 3 的 `ErrFooterCorrupt`/`ErrTrailerCorrupt` ⇒ **500 `part_corrupt`**（「诊断」由 `code` 承载；底层文本保真是 `ErrNotArchived` 臂的承重断言，损坏臂按任务 3 的形状处方用固定文案）＋ 任务 4 的「隔离件【不是】`ErrFooterCorrupt`」断言。**库层的「真篡改一个字节即报损坏」由计划 1 覆盖**（本计划不重验）。**⚠️ 明确的残余缺口**：**「用真实产物篡改一个 footer 字节 ⇒ 端到端 500」这一条用例不在本计划**（若发起者要求端到端 A5，需在本计划任务 4 之外另立用例）——报告不得把它写成已覆盖。**计划 2 任务 8 的 A 表漏了 A5 这一行** ⇒ 计划 4 把 A5 划给读侧而两边都没登记，登记 E3 |
| A6 | 压缩比与解码内存实测 | 计划 4 |
| A7 | 跨进程（sealer 与网关并发）验证 | 计划 4 |
| A8 | 行数分布（防止超 `maxTrailerEntries`） | 计划 4 |
| A9 | 冷热分层 | 未来计划 |

**⚠️ A2 的证据构成（必须逐字写进报告，不得含糊）**：
- **库层的「封块产物可点读 + 索引正确」已由计划 1 覆盖**：`TestEndToEndRoundTripByteExact`（A1 缩小版：写 N 行 → 封块 → 随机抽 K 行读回 → 逐字节）+ `TestFooterGoldenBytes` + `TestReaderReturnsExactLine` + R11 v3 的 `task-7.py`（32 条）与 `task-8-reader.py`（6 条）变异证据。**本计划不重验这些**（重验也不会更强，且会与冻结包的证据重复）。
- **本计划的 A2 = 生产形态缺口**（「既有能力基线」C 节的 5 条）：
  1. `request_id` → PG → 定位键 → `GetBody` 的**完整链路**逐字节；
  2. 四种状态**互不混淆**；
  3. 跨 part / 跨 `dt` / 行号越界的对外表现；
  4. **隔离件形态**（库层无此用例）；
  5. HTTP 表现与库层哨兵的对应。

**⚠️ 跨计划耦合：读侧窗口上限 vs sealer 窗口 —— 残余缺口如实登记，不新增端到端用例（订正 E8；交接 H18；任务 1 的 `traceBodyWriteSideWindowMB` 的直接后果）**

- **已核实的事实**：读侧的 `traceBodyWriteSideWindowMB = 512`（`internal/controlplane/config/trace_body_read.go:29`）是冻结包 `internal/tracearchive/defaults.go:57`（`defaultZstdWindowBytes = 512 << 20`）的**手抄副本**；**写侧（网关）目前没有窗口配置键**，而计划 4 的 sealer 走 `SealerOptions.ZstdWindowBytes` ⇔ `--zstd-window-mb`（计划 4 任务 1 的 `DefaultZstdWindowMB` 与 CLI flag，计划 4 `:349` / `:781`；unit 里的取值由计划 4 `:2112` 的静态检查钉住）。
- **风险**：若 sealer 用 ≥1 GiB 窗口而读侧仍是 512 MiB，**每一次归档点读都会失败** —— **这是纯粹的配置错**，而读侧的窗口下限守卫存在的全部理由就是不让配置错伪装成产物问题。
- **已核实的真实表现（比「报成 `ErrFooterCorrupt`」的描述更隐蔽，必须逐字登记）**：klauspost/compress 的**流式**解码在读完帧头后判 `frame.WindowSize > maxWindowSize` 并返回 `ErrDecoderSizeExceeded`（`vendor/github.com/klauspost/compress/zstd/decoder.go:861`；块层同族错误 `ErrWindowSizeExceeded` 见 `blockdec.go:149/181/346`），而冻结包在 `internal/tracearchive/reader.go:232-263` 把它包成 **「tracearchive: 解压读取失败」**且**不命中任何哨兵**（既不是 `ErrFooterCorrupt` / `ErrTrailerCorrupt`，也不是 `ErrNotArchived` / `ErrBodyMissing`）⇒ 任务 3 的映射表落到 `default` 臂，对外是 **500 `internal_error` + 固定文案**（只有 `ErrNotArchived` 臂保留 `err.Error()`）。**即：窗口不一致既没被报成「产物损坏」，也没被报成「配置错误」，而是一句无法归类的存储故障** —— 与「把配置错伪装成产物损坏」同向且更难排查。**本计划禁止**把任何窗口类失败（`ErrDecoderSizeExceeded` / `ErrWindowSizeExceeded`）映射成 `part_corrupt`、`not_archived` 或 404。
- **本计划的处置（残余缺口，与 A5 的残余同形，必须写进报告）**：**「用 ≥512 MiB 窗口的真实产物驱动校验，或断言窗口不一致被报成明确的配置错误」这一条用例不在本计划**。三条理由：① 要让 zstd **帧头真的声明** >512 MiB 窗口，输入必须 >512 MiB（单遍压缩的窗口随输入大小收缩 ⇒ 小样本复现不出来；本任务 `n = 2000` 行的规模不够），代价与计划 4 的 A6 同量级；② 冻结包**不提供**帧头窗口的读取接口（`WithDecoderMaxWindow` 是唯一入口），「用产物实际声明的窗口驱动校验」需要新增代码 ⇒ 属受控变更（＋ R11 v4）；③ 手写一个「帧头声明大窗口」的假产物需要在测试里复制 trailer/footer 的编码布局 ⇒ 与「格式知识单一事实源」冲突（与 D60 同一纪律）。
- **替代证据（本计划真正承担的部分，必须逐字写进报告）**：① **配置侧的显式错误已存在**：`TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide` 断言 `read_zstd_window_mb < 512` **在配置期**被拒，且错误文本含「必须 >= 512 MiB（与写入侧窗口一致）」（`tests/controlplane/config/trace_body_read_test.go:100-118`）；② 本任务新增一条 CHECKS 行（见步骤 3），钉住**冻结包默认窗口仍是 `512 << 20`** —— 任何把写入侧窗口提上去的受控变更都会让该行转红，从而强迫同一次改动回看读侧的手抄副本；③ 跨计划交接 **H18** 要求 sealer 的窗口参数与读侧上限**同源或对其可见**。**报告里不得**把这三点写成「窗口不一致的端到端行为已验收」。

- [ ] **步骤 1：写 A2 用例**

```go
// TestAcceptanceA2ProductionRoundTrip 是规格 §14.2 A2 的【生产形态】部分。
//
// 与计划 1 的 TestEndToEndRoundTripByteExact 的关系（必须在注释里写明）：
//   计划 1 直接构造 Ref 调 Reader.GetBody（库层，已覆盖且不重验）；
//   本用例经过【完整生产链路】：真实 Sealer 封块 → 真实 PG 行（含 body_ref_*）
//   → TraceBodyService.GetBody → 逐字节。
//   因此它补的是「定位键写入与读取是否对得上」这一【跨模块契约】——
//   这正是计划 2 A1 写入半部在写入侧无法自证的那一半。
func TestAcceptanceA2ProductionRoundTrip(t *testing.T) {
    // 规模：不必百万（那是 A1 的规格值，且封块需要真实 zstd）。
    // 取 n = 2000 行、块大小阈值调小以强制多 part（每 part 约 300 行 ⇒ ≥ 6 个 part），
    // 这样【索引的跨 part 正确性】在一次用例里被覆盖。
    //
    // 步骤：
    // ① 用真实 ta.Sealer 把 n 行封成多个产物（本机需 zstd ⇒ requireZstd 守卫，
    //    缺失则整包 SKIP 并登记 SKIP_WHITELIST —— 见任务 4 的同一处置）
    // ② 为每一行在内存 sqlite 写一条 CallTrace（body_ref_dt/inst/part/row 全非 NULL，
    //    body_missing=false，3 个 jsonb 列全 NULL）
    // ③ 随机抽 K = 200 行（固定 seed 保证可复现）：
    //    调 svc.GetBody(ctx, requestID)
    //    断言：bytes.Equal(got.Body, wantLine)；Source == SourceArchive
    // ④ 【索引正确的承重断言】抽样必须覆盖 ≥ 2 个不同的 part，且每个被抽 part 内
    //    的行序与写入序一致 —— 否则「索引正确」这个命题没有被测到。
    //    用 require.True(t, partsSeen >= 2) 固定它。
}
```

```go
// TestAcceptanceA2IndexBoundaries 覆盖索引的三个边界（库层已有分类，这里测【对外表现】）：
//   ① 跨日：两个 dt 目录各一个 part，抽样断言各自取回自己的行（不串目录）
//   ② 跨 part：同一 dt 的 part-000000 与 part-000001，抽样不串文件
//   ③ 行号越界：构造 row == row_count（恰好在界外一个）与 row == math.MaxInt32
//      断言 errors.Is(err, ta.ErrNotArchived) 且【不是】ErrBodyMissing
//      —— 后者是承重的：越界若被报成「正文从未落盘」，就把存储不一致伪装成了业务事实。
```

```go
// TestAcceptanceA2StateMatrixMutuallyExclusive  ← 本任务的「互不混淆」验收
// 同一批数据里构造四种状态各若干行，一次性断言四者的 (err, source) 组合【两两不同】：
//   jsonb        ⇒ (nil, SourceJSONB)
//   archive      ⇒ (nil, SourceArchive)
//   body_missing ⇒ (ErrBodyMissing, —)
//   未归档       ⇒ (ErrNotArchived, —)
// 【为什么要这条】规格 §8.1 的全部意图就是「不能让一种状态伪装成另一种」；
// 四条独立用例各自绿【不能】证明它们互不混淆（一个把所有错误都返回同一哨兵的实现
// 可以让四条都绿）⇒ 必须在同一个用例里做【互异性】断言。
```

**⚠️ 上面四格里的「jsonb ⇒ 200」在生产上成立的前提（订正 E6，见全局约束「读侧键的存在性 / 空串语义」）**：禁用态只能由 YAML 一侧产生（缺 `trace_body.root` 键，或该键显式留空）—— `TRACE_BODY_ROOT=""` **不会**覆盖 YAML（vendored viper 跳过空串，`viper.go:444`）。若 YAML 里还留着 `root`，同样的数据会走**归档**路径而不是 jsonb ⇒ 验收数据集的构造必须显式使用 `Disabled()==true` 的配置（而不是「把环境变量设成空」），否则这一格会被静默地测成另一条路径。

- [ ] **步骤 2：运行测试**

```bash
zstd --version      # 方案 C 前置
go test ./tests/observability/service/ -run 'TestAcceptanceA2' -v
```
预期：**PASS**（本机无 zstd 时，依赖真实封块的用例 SKIP 且必须在集合的 `SKIP_WHITELIST` 里逐条登记 —— **未登记的 SKIP 会让三断言失败**）。

- [ ] **步骤 3：写变异集合 `tools/trace-mutate/sets/p3-task-8.py` 与 `p3-task-8-handler.py`**

**本任务只写测试、无新生产代码** ⇒ 与计划 2 的任务 8 同一形态：变异打在任务 2（服务）与任务 3（映射）的生产代码上。因为工具一次一个 `--target`，拆两份（**两份各自的 `MANIFEST_FILES` / `PACKAGES` / 杀手都必须落在同一目标文件对应的测试包上**）：

```python
# ---------- tools/trace-mutate/sets/p3-task-8.py（TARGET = 服务）----------
NAME = "p3-task-8"
TARGET = "internal/observability/service/trace_body_service.go"
PACKAGES = ["./tests/observability/service/"]
MANIFEST_FILES = ["tests/observability/service/trace_body_acceptance_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/service/trace_body_acceptance_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
SKIP_WHITELIST = ["TestAcceptanceA2ProductionRoundTrip", "TestAcceptanceA2IndexBoundaries"]

MUTATIONS = {}
# m01 定位键的 part 分量被写死为 0 ⇒ 跨 part 串文件（索引正确性破裂）
#     杀手：TestAcceptanceA2ProductionRoundTrip（partsSeen >= 2 与逐字节断言）
#     【与任务 2 的 m04 同族，但杀手不同：这里由 A2 的跨 part 抽样固定】
MUTATIONS["m01_part分量写死为0"] = [
    ("sub", "Part: *trace.BodyRefPart,", "Part: 0,"),
]
# m02 定位键的 dt 分量被换成 inst 分量 ⇒ 跨日串目录 / 直接校验失败
#     杀手：TestAcceptanceA2IndexBoundaries（跨日那一条）
MUTATIONS["m02_dt分量换成inst"] = [
    ("sub", 'DT:   trace.BodyRefDT.Format("2006-01-02"),', "DT:   *trace.BodyRefInst,"),
]
# m03 行号越界（冻结包返回 ErrNotArchived）被换成 ErrBodyMissing
#     ⇒「元数据与存储不一致」被伪装成「正文从未落盘」（A2 最承重的一条）
#     杀手：TestAcceptanceA2IndexBoundaries 与 TestAcceptanceA2StateMatrixMutuallyExclusive
MUTATIONS["m03_越界报成body_missing"] = [
    ("sub", "\t\treturn nil, err\n", '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrBodyMissing, requestID)\n'),
]

CHECKS = [
  {"name": "读侧不自算日期", "file": "internal/observability/service/trace_body_service.go",
   "pattern": r"FixedZone|LoadLocation|time\.Date\(|time\.Now\(", "expect": "absent",
   "why": "A2 的跨日索引正确性以「dt 取自行内 body_ref_dt 列」为前提（E2）；读侧自算日期会让跨日断言失去意义"},
  {"name": "冻结包GetBody签名未变", "file": "internal/tracearchive/reader.go",
   "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
   "expect": "present",
   "why": "A2 的全链路逐字节以这个复用点为前提；整目录零改动的权威判据是任务 9 的 git diff（D59）"},
  # 【E8 / H18】读侧窗口上限是冻结包 defaultZstdWindowBytes 的手抄副本，而 sealer 的窗口
  # 由计划 4 独立声明 ⇒ 读侧唯一能做的「同源」证据是钉住**冻结包侧的值**：一旦写入侧窗口
  # 被提升（受控变更 + R11 v4），这一行必须转红，强迫同一次改动回看
  # trace_body_read.go:26/29 的镜像常量与读侧配置（否则会出现「sealer 写得出、读侧解不开」
  # 且被报成 500 internal_error 的静默不兼容）。这是**绊线**，不是「窗口一致」的证明 ——
  # 报告不得把它写成端到端验收（见本任务「跨计划耦合」块的残余缺口）。
  {"name": "冻结包默认窗口未被静默改动", "file": "internal/tracearchive/defaults.go",
   "pattern": r"defaultZstdWindowBytes = 512 << 20", "expect": "present",
   "why": "读侧 traceBodyWriteSideWindowMB=512（trace_body_read.go:29）是它唯一的手抄副本；改这个值而不动读侧 = 每一次归档点读都变成未分类的 500（E8）"},
]
```
```python
# ---------- tools/trace-mutate/sets/p3-task-8-handler.py（TARGET = 映射）----------
NAME = "p3-task-8-handler"
TARGET = "internal/observability/handler/trace_body_error.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_error_test.go"]
MANIFEST_DERIVATION = "grep -h '^func Test' tests/observability/handler/trace_body_error_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"

MUTATIONS = {}
# m01 「四种状态互不混淆」在对外映射层的对应变异：ErrNotArchived 与 ErrBodyMissing
#     被映射到同一个 code/status ⇒ 调用方无法区分「存储不一致」与「从未落盘」
#     杀手：TestMapTraceBodyError（**必须是顶层函数名**：原稿写「TestAcceptanceA2StateMatrix…」
#           是错的 —— 那条用例只断言服务层的 (err, source)，不会因映射改动转红）
MUTATIONS["m01_未归档与缺失同码"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):", "case errors.Is(err, ta.ErrBodyMissing):"),
]
```

**⚠️ 已订正的两处**：① 原稿「把 `Ref.DT` 换成 `time.Now().Format(...)`」既不可定位（目标不在服务文件里）又**与全局约束「读侧零日期计算」（E2）冲突** —— 现在改成把 `DT` 换成 `inst` 分量（同样是「跨日串目录」的证据形态）；② 原稿说「变异打在任务 2（服务）与任务 3（映射）」但没写 `PACKAGES`/`MANIFEST_FILES`，且 handler 集合的杀手不是顶层用例 —— 已按上面两份集合逐字补齐。

- [ ] **步骤 4：跑变异集合** → 预期：`p3-task-8.py` **3 条断言红**（无 zstd 时 m01/m02 的杀手按 `SKIP_WHITELIST` 整条 SKIP ⇒ 必须把结论标注为「权威表需在带 zstd 的机器上产出」）；`p3-task-8-handler.py` **1 条断言红**；三断言过、`EXPECTED_GREEN` 逐键核对（两份集合的 `EXPECTED_GREEN` 均应为空）。
- [ ] **步骤 5：提交**

```bash
git add tests/observability/service/trace_body_acceptance_test.go \
        tools/trace-mutate/sets/p3-task-8.py tools/trace-mutate/sets/p3-task-8-handler.py
git commit -m "test(observability): A2 生产形态往返与索引边界验收（计划 3/4 任务 8）

A2 的库层部分（封块产物可点读、索引正确）已由计划 1 覆盖（R11 v3 的
task-7/task-8-reader + TestEndToEndRoundTripByteExact），本用例不重验；
本计划补生产形态缺口：request_id→PG→定位键→GetBody 全链路逐字节、
四种状态互不混淆、跨日/跨 part/行号越界、隔离件形态。
A1 读回半部、A3 真实盘满、A6/A7/A8/A9 均不在本计划。"
```

---

### Task 9：分支级证据重跑（R11 式）

**文件：**
- 新建（证据目录，gitignored）：`.superpowers/sdd/2026-09-25-trace-point-read-service/R11-v1.md`
- 不改任何生产代码

**为什么必须有这一步**：CI 完全不跑 `go test`（`.gitlab-ci.yml` 只有三处 `CGO_ENABLED=0 go build`）⇒ **逐任务**的变异表是过程证据，而**同一棵树上的全任务重跑**才是最终结论——它同时自查「后续任务的改动有没有让前面任务的守卫失效」。

- [ ] **步骤 1：冻结提交 + 生成名字清单**

```bash
git status --porcelain          # 必须为空
git rev-parse HEAD              # 记下这个哈希，写进报告头部
git log --oneline -12
```

**清单文件必须先生成**（工具 `--manifest` **不做生成**；文件不存在会 `FileNotFoundError` 直接崩）。逐集合的推导命令来自各集合自己声明的 `MANIFEST_FILES` + `MANIFEST_DERIVATION`，与全局约束第 10 条一致：

```bash
D=.superpowers/sdd/2026-09-25-trace-point-read-service
mkdir -p "$D"
derive() { grep -h '^func Test' "$1" | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort > "$D/$2"; }
derive tests/controlplane/config/trace_body_read_test.go                        p3-task-1-names.txt
derive tests/observability/service/trace_body_service_test.go                   p3-task-2-names.txt
derive tests/observability/handler/trace_body_error_test.go                     p3-task-3-names.txt
derive tests/observability/service/trace_body_quarantine_test.go                p3-task-4-names.txt
derive tests/observability/handler/trace_body_handler_test.go                   p3-task-5-names.txt
derive tests/observability/handler/usage_trace_archive_test.go                  p3-task-6-names.txt
derive tests/tools/trace_body_cli/trace_body_cli_test.go                        p3-task-7-names.txt
derive tests/observability/service/trace_body_acceptance_test.go                p3-task-8-names.txt
derive tests/observability/handler/trace_body_error_test.go                     p3-task-8-handler-names.txt
wc -l "$D"/*-names.txt   # 每个都必须 > 0（空清单 ⇒ 执行数 0 ⇒ 证据无效）
```

- [ ] **步骤 2：逐集合跑权威表**

**本计划的集合（恰好 9 个，与全局约束第 11 条逐字一致）**：`p3-task-1` / `p3-task-2` / `p3-task-3` / `p3-task-4` / `p3-task-5` / `p3-task-6` / `p3-task-7` / `p3-task-8` / `p3-task-8-handler`。
（**任务 1 与任务 7 是否产出集合取决于实现**：任务 1 有独立生产文件 ⇒ 必须有；任务 7 若退出码映射无法抽到可导入位置 ⇒ **停下报告**，不得静默少一个集合。**不得**追加 `p3-task-4-handler` 或 `p3-task-5-router`——这两个集合按全局约束第 11 条**不产出**。）

```bash
: > /tmp/p3-task-all.out
for s in 1 2 3 4 5 6 7 8 8-handler; do
  set=tools/trace-mutate/sets/p3-task-$s.py
  [ -f "$set" ] || { echo "MISSING SET: $set"; exit 1; }   # 不得静默跳过
  python3 tools/trace-mutate/trace_mutate.py --repo . \
    --manifest .superpowers/sdd/2026-09-25-trace-point-read-service/p3-task-$s-names.txt \
    --mutation-set "$set" > /tmp/p3-task-$s.out 2>&1
  echo "task-$s exit=$?" | tee -a /tmp/p3-task-all.out
done
grep -c '^task-.* exit=0$' /tmp/p3-task-all.out   # 必须 = 9
```
**三个硬门（任一不过即本轮结论无效）**：① 9 个集合全部存在且都被跑过（`MISSING SET` 一次都不许出现）；② 每个集合的权威表 `exit=0`；③ 每个集合的三断言全过。
**另有 zstd 前置硬门**：`p3-task-4` 与 `p3-task-8` 的若干杀手依赖真实封块 ⇒ **权威表必须在带 C 版 `zstd` 的机器上产出**（`zstd --version` 的输出抄进报告）；无 zstd 的机器上这些用例整条 SKIP，结论只能标注为「未产出权威表」。
- [ ] **步骤 3：写报告 `.superpowers/sdd/2026-09-25-trace-point-read-service/R11-v1.md`**

报告固定结构（**事实，不是声明**）：
1. **绑定**：`git rev-parse HEAD`、`git status --porcelain` 为空、工具/集合/清单三者 sha256（从工具头部原样抄）、pristine 树来源。
2. **计数口径**：断言支撑的红 / 崩溃超时红 / 允许的绿 / 未登记的绿 / 证据无效，**逐集合**列出（**不要把崩溃红与断言红合并计数**）。
3. **三断言**：逐集合声明 `names==ran==清单`、`PASS+FAIL+SKIP == 清单条数`、全层级 `SKIP == 0`。
4. **静态检查**：逐条抄 `CHECKS` 的命令与真实输出。
5. **覆盖缺口（必须诚实列出，不得留白）**：
   - `GetBody` 输入校验只在**调用侧**（任务 1 的 `ValidateRef`）；**导出的 `GetBody` 自身仍不校验**（冻结包不可改 ⇒ D59）；
   - 真实盘满（A3）、跨进程并发（A7）、压缩比与解码内存（A6）、行数分布（A8）、冷热分层（A9）**均未覆盖**（属计划 4）；
   - `TraceBodyResult.Source` 的判定与 `GetBody` 内部查找是**两次 stat**（非原子），窗口内 `Source` 可能不符（正文内容仍正确）；
   - 隔离件的部分变异目标在冻结包内 ⇒ 已按要求转为 `CHECKS`，**该要求当前只有静态证据**（逐条列出是哪几条）；
   - 需要真实 zstd 的用例在本机无 zstd 时 SKIP（逐条列出用例名与 `SKIP_WHITELIST` 依据）。
   - **【E36，本轮承接】`p3-task-3` 的「测试文件 sha256」段**：任务 5 的提交 `eda06420` 在 `tests/observability/handler/` 新增了 `trace_body_handler_test.go`，任务 6 的提交 `d2bea971` 又新增 `usage_trace_archive_test.go`，而该集合的 `PACKAGES = ["./tests/observability/handler/"]` 是**目录级枚举**（工具 `test_files_of` 打印该目录下每个 `*_test.go` 的 sha256）⇒ 本轮重跑的 `p3-task-3` 权威表**必须出现 4 行**测试文件哈希（旧表 `-at-e0d32ce4.txt` / `-at-9c4fd450.txt` 都只有 `prompt_mask_test.go` 与 `trace_body_error_test.go` 两行；**原写「第三行」不成立，见 E38**）；少于 4 行即说明绑定没有覆盖当前树。
   - **【E33 兜底】任务 8 的「真实 `router.Setup` 装配（`trace_body.root` 启用/禁用两态）」用例**：若任务 8 已产出，抄其文件与结论；**若未产出**，逐条登记三条要求各自缺什么证据（**权限门 `PermUsageViewAll`** / **不新建鉴权中间件** / **真实装配两态**），**不得**留下无承接方的条目，也不得把「只有静态证据」写成已覆盖。
6. **与计划 1/2 证据的关系**：本计划**未修改** `internal/tracearchive/` 下任何文件（用命令输出为证，见步骤 4）⇒ R11 v3（142 条，冻结提交 `ea962a70`）的被测文件哈希未变，**其绑定仍然有效，无需重跑**；`tools/trace-mutate/sets/task-*.py` 与 `p2-task-*.py` 同样未被修改。
   **⚠️ 必须写明的限定**：R11 v3 报告头部记录的 `git rev-parse HEAD` 是**旧提交** `ea962a70`；本计划合入后**该字段本身已经失效**（HEAD 变了）。R11 v3 之所以仍然有效，**唯一依据是「被测文件哈希未变」**（它绑定的是文件 sha256，不是分支位置）——报告必须**照此措辞**，**不得**写成「R11 v3 的 HEAD 仍是当前 HEAD」（那是错的，且会让人误以为无需核对文件哈希）。

- [ ] **步骤 4：把报告与最终提交一起交付**
```bash
git diff --stat <base>..HEAD -- internal/tracearchive/   # 必须为空
git diff --stat <base>..HEAD -- atlas-migrations/        # 必须为空
git diff --stat <base>..HEAD -- go.mod go.sum            # 必须为空
git diff --stat <base>..HEAD -- tools/trace-mutate/sets/task-*.py tools/trace-mutate/sets/p2-task-*.py  # 必须为空
git log --oneline <base>..HEAD
```
（`.superpowers/` 是 gitignored 的证据目录，不进版本库；报告以文件路径交付给发起者。）

---

## 计划 4 交接清单（本计划**不实现**，但必须被下游看到）

| # | 交接项 | 依据 |
|---|---|---|
| **H8** | **compose 的 control-plane 服务必须挂载 `/datalog`（只读）并设置 `TRACE_BODY_ROOT`**（与 gateway 的写侧同一个 root）。挂载/变量不在场时读侧禁用 ⇒ 有定位键的行一律 **503**，jsonb 形态的历史行仍可读 | 全局约束「功能关闭的语义（读侧）」、规格 §11.1 |
| **H9** | **sealer 必须使用冻结的 `internal/tracearchive.Sealer`，产物格式 = `format_version 1`**。**若提升格式版本**，必须**同时**：① 在冻结包侧做受控变更 + 重跑 R11 v4；② 同步读侧（`decodeFooter` 对未知版本**响亮拒绝**，当前只接受 1，失败表现为 `ErrFooterCorrupt` ⇒ 500 `part_corrupt`）。**不得**让 sealer 写出读侧不认的版本而无人知 | 全局约束「压缩与解压路径」、`footer.go` 的 `formatVersion` |
| **H10** | **隔离件规则（供 sealer 与运维）**：sealer 必须**永不**封块 spool 目录下的 `part-NNNNNN.jsonl.zst`（`ListPendingSeals` 只收 `.jsonl` 结尾，这是已冻结的规则）；运维释放空间后把隔离件**改名回** `part-NNNNNN.jsonl` 即可让其被正常封块。**同时**：隔离件里的正文在改名之前**读不到**（读侧返回 `ErrNotArchived`），这部分**不是**「静默丢数据」但**必须**有人介入 | 规格 §7.1、全局约束「隔离件的命名/发现规则（D60）」 |
| **H11** | **A8 行数分布核实**：必须在生产/样本上量**每块的最高行数**（不是平均行大小），确认 ≤ 8,388,608（`maxTrailerEntries`）。超限时**合法**产物会让读侧响亮误报 `ErrTrailerCorrupt` ⇒ 500 `part_corrupt`（**非静默**）。**特别警惕 ENOSPC 期的 5 字节 `null` 标记行**（极短 ⇒ 小字节量也能堆到高行数） | 计划 1 的 D35/D38 与 **N4**、规格 §14.2 A8 |
| **H12** | 隔离件与孤儿正文（崩溃窗口产物）的**清理策略**；清理工具**必须**跳过 `body_missing=true` 的行、**不得**假定行号连续（沿用计划 2 的 H5） | 规格 §15.1 U6、计划 1 D51 / 计划 2 H5 |
| **H13** | **明确不在本计划的验收项**：真实盘满 A3、跨进程 A7、压缩比与解码内存 A6、行数分布 A8、冷热分层 A9、`/datalog/emergency-archive` 与 `/datalog/archive` 的存量点读（子项目 4）、管理后台 UI（子项目 3） | 规格 §14.2、§2.2、§12 |
| **H14** | **读侧只做随机访问**。计划 4 若新增任何「按 `body_ref_row` 顺序遍历某个 part」的校验/清理/压缩比工具，**必须**跳过 `body_missing=true` 的行，**不得**假定行号连续（计划 2 H5 的延续）。**读侧点读本身不假定连续**（它按 `(part,row)` 直接定位） | 计划 2 D51 / H5、全局约束「行号稠密性假设」 |
| **H15** | **P1/P2/P3 生产侧验收**（计划 1 §15.2 的清单）仍未被本计划覆盖：生产 zstd 版本探测、生产压缩比、512 MiB 窗口的**读侧**解码内存。**后者直接决定本计划的并发上限**（当前默认 2，按 600 MB/并发估）⇒ 实测后**必须**回填 `TRACE_BODY_READ_MAX_CONCURRENT` 的推荐值 | 规格 §8.4 C5、§15.2 P1–P3 |
| **H16** | **「网关对外暴露点读」是可选项，不在本计划**。若下游决定要做，唯一合规链路是 `gateway 路由 → internal/gateway/binding（带 X-Internal-Token）→ GET /internal/v1/trace-body（本计划任务 5 已交付）`，**不得**让 gateway 直连 PG 或直读 `/datalog` 产物（`AGENTS.md` 明文 + 规格 §8.4 的内存归属）。**并且必须实测那一跳的延迟**（本计划的 CLI 与领域内直连路径可作对照基线），不得凭估算 | `AGENTS.md`「gateway 不要直连数据库」、全局约束 ③、D63 |
| **H17** | **`AGENTS.md` 有两处过期表述需发起者订正**（本计划**只记录不改文档**，与 D46 同一纪律）：① 「数据库变更：新增 `migrations/NNN_*.sql`」——实际是 `atlas-migrations/<timestamp>_*.sql` + `atlas-db-migration` skill（**D62**）；② 「gateway 不要直连数据库」与 `cmd/gateway/main.go` 写入侧直连 PG 的矛盾（**D46**，计划 2 已登记）。①②都会让后续执行者走错路 | `AGENTS.md`、D46、D62 |
| **H18** | **sealer 的窗口参数必须与读侧上限同源、或至少对读侧可见（窗口不一致 = 静默不兼容）**。计划 4（`docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md`，**只引用不改它**）的 sealer 通过 `SealerOptions.ZstdWindowBytes` ⇔ `--zstd-window-mb` **独立地**声明窗口（该计划任务 1 的 `DefaultZstdWindowMB = 512`（`:349`）与 CLI flag（`:781`）；unit 里的取值由该计划任务 5 的静态检查钉住（`:2112`）），而读侧的 `trace_body.read_zstd_window_mb`（默认 512；守卫下界 `traceBodyWriteSideWindowMB = 512` 是冻结包 `internal/tracearchive/defaults.go:57` 的 `defaultZstdWindowBytes` 的**手抄副本**，`trace_body_read.go:26/29`）**没有任何通道能看到它** ⇒ 若 sealer 提升到 ≥1 GiB 而读侧不动，**每一次归档点读都会失败**，且失败**不命中任何哨兵**、对外是 500 `internal_error` **固定文案**（详见任务 8 的「跨计划耦合」块与 E8），即把配置错伪装成存储故障。**要求**：① 两侧窗口值必须写进同一份部署文档 / 编排（H8 的 compose 与该计划的 systemd unit）并**逐字相等**；② **任何**把 sealer 窗口提到 >512 MiB 的变更**必须与提升 `TRACE_BODY_READ_ZSTD_WINDOW_MB` 在同一次改动内完成**，并**同时**复核本条与 E8；③ 计划 4 若新增 / 改动窗口默认值，**必须**在它的静态检查里同时钉住读侧键，或逐字声明「读侧默认随冻结包，不得独立提升」。**本条与任务 8 新增的 CHECKS 行（`p3-task-8.py` 的「冻结包默认窗口未被静默改动」）是同一耦合的两端** | `internal/tracearchive/defaults.go:57`、`trace_body_read.go:26/29`、全局约束「解码侧内存约束」、E8、计划 4 任务 1（`DefaultZstdWindowMB` / `--zstd-window-mb`）与任务 5 的 unit 静态检查 |

---

## 规格缺陷登记（本计划自成一套编号：D53–D63）

> 每条给出：规格原文位置 → 冲突/缺口 → **本计划的裁定**。执行者按裁定实现；若认为裁定有误，写进交付报告，不要改动本计划。

| # | 位置 | 问题 | 本计划裁定 |
|---|---|---|---|
| **D53** | §8.1（响应表） | 规格列了 `410 + body_missing=true`（「正文曾落盘但产物缺失」）。但**冻结包没有任何代码路径返回 `ErrPartNotFound`**：`grep -rn "ErrPartNotFound" --include=*.go .` 只有 `errors.go:13` 的定义与 `tests/tracearchive/ref_test.go:83` 的哨兵互异表；`Reader.GetBody` 在「产物与 spool 都不存在」时返回 **`ErrNotArchived`**，而 `errors.go:18-27` 明确写它**对应 HTTP 500**（复核：`ErrNotArchived` 的注释块起于 18 行、定义在 27 行；原稿的 `15-25` 行号是错的，已订正）。⇒ **410 当前不可达**，且 410 行上的 `body_missing=true` 与该形态（定位键存在）自相矛盾 | **遵守冻结包的自述**：`ErrNotArchived` ⇒ **500**，`code = not_archived`，**不置** `body_missing`。**不得**为让 410 可达而改冻结包，**不得**在 handler 里复制路径布局去反推 410（那会与读侧单一事实源冲突）。规格的 410 行建议**删除或改述**为「500 + 诊断」（订正属发起者决策） |
| **D54** | §8.1 端点 vs 仓库既有路由；§4.2 组件位置；**读侧的分层归属**（`AGENTS.md`「gateway 不要直连数据库」） | ① 规格写 `GET /api/v1/trace/:request_id/body`，而仓库既有 trace 端点族是 `/api/v1/usage/logs/:request_id/trace`、`/api/v1/my/usage/logs/:request_id/trace`、`/api/v1/usage/scoped/logs/:request_id/trace`（`router.go:759/769/1004`）⇒ 命名族不一致（一条在 `/trace/` 下，三条在 `/usage/logs/` 下）。② 规格 §4.2 把「点读接口」放在 `internal/controlplane/handler/`，实际 observability 的 handler 在 `internal/observability/handler/`（control-plane router 引用它）。③ 规格 §4.2 把点读服务放 `internal/observability/service/`，但**没有**说明它属于哪个进程——而 `AGENTS.md` 明文「gateway 不要直连数据库」，读侧又必须查 PG | **按仓库现实与 `AGENTS.md` 落地**：**读侧归 control-plane**（服务 `internal/observability/service/`、handler `internal/observability/handler/`、路由注册在 `internal/controlplane/router/router.go`）；依据三条（DB 通道、§8.4 的内存归属、§11.1 的 `/datalog` 挂载矩阵）见全局约束「读侧的分层归属」。**端点路径按规格字面实现**（`/api/v1/trace/:request_id/body`，不与既有路径冲突），**并额外**修好既有端点族的归档感知（任务 6）。**不新增对 gateway 的偏离**（写入侧的 D46 偏离沿用但不扩大）；**不**在 gateway 里加 binding 客户端（详见 **D63**）。规格 §4.2 的路径建议订正为仓库实际位置，并建议补一句「读侧进程 = control-plane」 |
| **D55** | §8.2（读取算法） | 读取算法的 5 步**完全没有**处理「定位键全为 NULL 且 `body_missing=false`」这一形态——而它正是**功能关闭**（`TRACE_BODY_ROOT` 留空 ⇒ 写入侧走既有路径，正文进 jsonb）与**构造失败**（`sink == nil`）的必然结果。规格 §8.1 的响应表也没有这一行 | **本计划定死**（全局约束）：「4 键全 NULL + `body_missing=false`」⇒ **200 + 正文来自 jsonb 列**（`Source=jsonb`），**并在响应形状上重建三键对象**。**「部分为 NULL」⇒ 500 服务层不一致**，**绝不**静默回退、**绝不**报 404。规格建议补这一行 |
| **D56** | §14.2 的 A1–A9 vs 计划 2 Task 8 的 A 表 | **同一组字母，两种语义**。规格 §14.2：A2=杀掉 sealer 中途；A6=2 GB 单块 P99 延迟；A7=超并发点读；A8=读取 1000 次目录占用零增量；A9=移走 zstd 二进制。计划 2 的任务 8 表：A2=封块产物可点读、索引正确；A6=压缩比与解码内存实测；A7=跨进程验证；A8=行数分布；A9=冷热分层。**两者只有 A1/A3 对得上** | **本计划采用计划 2 的 A 表**（发起者的简报也按它下达），并在任务 8 的归属表里**逐字保留**该表。**冲突登记**：规格 §14.2 是「唯一需求来源」，而它的 A2（杀 sealer）与读侧验收无关 ⇒ 建议发起者**裁定以哪张表为准**并订正另一份。**本计划不声称覆盖规格 §14.2 的 A2 原文语义** |
| **D57** | §2.1 G3 vs jsonb 回退路径 | G3 写「给定 `request_id` 取回正文，**逐字节**等于写入前」。但**功能关闭/构造失败**时正文在 jsonb 列（`datatypes.JSON`），读侧只能**重新序列化**成三键对象 ⇒ 键序与空白**不保证**与请求当时的字节相同。另：§8.4 的 C5「2 GiB 限额下最多 2 个并发点读、按 600 MB/并发估算」，其中「control-plane 确是 2 GiB」与「解码器 ≈ 窗口」都**未经实测**（规格 §15.2 P3 自己登记了这点） | **逐字节保证只对归档路径成立**（jsonb 路径显式不保证，报告与文档必须写明）。C5 的默认并发取 **2**，但**必须可配置**（`TRACE_BODY_READ_MAX_CONCURRENT`）并**在 P3 实测后回填**（交接 H15）。**不得**声称 jsonb 路径逐字节 |
| **D58** | §12 vs §2.2 | §12 写「`/datalog/emergency-archive`（128 GiB，14 天）**完全不碰**。继续服务 09-09~09-22 的历史点读；**新旧两套读取路径按 `dt` 分流**」——这要求读侧**实现两套路径**。而 §2.2 非目标写「存量归一：`/datalog/emergency-archive` 与 `/datalog/archive` **完全不碰**」，且本计划的发起者简报未把它列入范围 | **本计划不实现 emergency-archive 分流**。理由：① 那是**遗留格式**（256 MB 分块 + 外部 `index.jsonl`，规格 §4.3 明说其读取前提「按 `request_id` 排序写 + 二分」在本设计下不成立）；② 它有**独立的数据源与格式**，属子项目 4「存量归一」。**登记为范围缺口**：在完成之前，09-09~09-22 的历史点读**只能**走旧运维手段。`dt` 分流点（若将来要做）应落在任务 2 的**判定顺序之前**，作为 `dt < 某个截止日` 的分派 |
| **D59** | 计划 1 的 **N3** 要求「计划 3 收口 `GetBody` 的输入校验缺口」 vs 本计划「不得修改 `internal/tracearchive/`」 | 两条约束**直接冲突**：N3 要求修的缺口在 `internal/tracearchive/reader.go:121-140`（`GetBody` 用 `ref.DT`/`ref.Inst` 拼目录名且不校验），而冻结包禁止改动（R11 v3 的 142 条证据绑定文件哈希，计划 1 D16） | **在调用侧收口**（任务 1 的 `ValidateRef`，纯内存判定、先于任何 IO），**`GetBody` 保持原样**。**如实登记的代价**：① 同一判据两处实现（冻结包的 `validateDT`/`validateInst` + 调用侧的 `ValidateRef`）；② **导出的 `GetBody` 仍可被未来调用方误用**（残余风险）。**建议**（交发起者决策）：把「导出 `ValidateRef` 或让 `GetBody` 自校验」作为**受控变更**，与 R11 v4 一起做——它必须改冻结包，**不在本计划内** |
| **D60** | §7.1（隔离件） | 规格说「**读取侧不为它新增分支或哨兵**」，并给了运维判据；但**没有规定读侧要不要在诊断里区分它**。而「定义隔离件的命名/发现规则」又是本计划发起者明确下发的任务 | **命名/发现规则写进计划正文（全局约束）+ 用测试固定可观测行为 + 写进计划 4 交接（H10）**，**不落成生产分支**：读侧对隔离件的**唯一**可观测表现是 `ErrNotArchived`（500 `not_archived`），诊断文本含 `Ref`。**不得**为它加分支、哨兵、或明文回退（由任务 4 的 `CHECKS` 静态钉死）。**测试是可验证形式**：在 spool 目录放明文 `part-N.jsonl.zst` ⇒ 断言 `ErrNotArchived` 且**不是** `ErrFooterCorrupt` |
| **D61** | §5.2 / 计划 1 的 D35+D38（`maxTrailerEntries`） | `maxTrailerEntries = 8,388,608` 是**读侧内存预算**推出的界。**合法**产物若块内行数超限，`GetBody` 会**响亮误报** `ErrTrailerCorrupt`（计划 1 D35 的「诚实标注」；D38 更正了触发条件的措辞：真触发条件是**块内行数**，与块字节大小**无关**，64 MiB 反例即证）。本计划把 `ErrTrailerCorrupt` 映射为「产物损坏」⇒ **诊断方向可能错**（真成因是「块太密、超出读侧预算」） | **不修**（属预算取舍，计划 1 已裁定）。**读侧必须**：① 映射为 500 `part_corrupt`（**绝不** 404）；② 诊断文本**必须**保留底层文本（`decodeTrailer` 的错误里已含「期望 N 条超过条目数上界 M」）⇒ 运维能读到真成因；③ 生产侧核实**交计划 4**（A8，见 H11）。**不得**把它报成「不存在」或「正文缺失」 |
| **D62** | `AGENTS.md`「数据库变更」 vs 仓库实际迁移目录 | `AGENTS.md` 的「约定与注意事项」写「**数据库变更**：新增 `migrations/NNN_*.sql`（递增序号），对应改 `internal/controlplane/model` 与 repository」，同一个 `AGENTS.md` 的「进一步阅读」与另一条里写的却是 **Atlas**（`atlas-migrations/<timestamp>_*.sql`）。**仓库实际是后者**：`migrations/` 是 legacy 冻结目录，`atlas-migrations/` 才是唯一迁移源（`migrations/` 里的 `quota_rpm` / 5 张 `channel_monitor*` 表从未移植进 `atlas-migrations/`，正是计划 2 **D52** 登记的漂移根因之一） | **以 `atlas-migrations/` + `atlas-db-migration` skill 为准**（本计划**零迁移**，因此这条只作为交接与后续计划的防坑提示）。**不得**有人按 `AGENTS.md` 那句去写 `migrations/NNN_*.sql`——那会让漂移进一步扩大，并让计划 2 D52 的「只取 `call_traces` 子集 + 绝不 apply 其余 + `comm -23` 证据」处置失效。**订正 `AGENTS.md` 属发起者决策，不在本计划内**（与 D46 同一条纪律：只记录，不改文档） |
| **D63** | 规格 §8.1 的两条端点 vs `AGENTS.md`「gateway 经 `internal/gateway/binding` 调内部接口」 | 规格 §8.1 的两条端点**都在 control-plane 侧**（`/api/v1/...` 与 `/internal/v1/...`），**没有**要求 gateway 对外暴露点读。而 `AGENTS.md` 的架构表述容易被读成「读侧也要经 binding 走一趟」⇒ 可能被过度实现（在 gateway 里再加一条路由 + binding 客户端），从而**把「网关直连 PG」的偏离换成「网关多一跳转发」的额外面** | **本计划只交付 control-plane 侧的两条端点**（任务 5），**不**新增 gateway 路由、**不**新增 binding 客户端。若将来确实要把点读暴露给外部调用方，按全局约束 ③ 的既定链路走（gateway → `internal/gateway/binding` → `GET /internal/v1/trace-body`，复用 `InternalAuth`），并**必须实测那一跳的延迟**（不得凭估算）。**不得**在本计划里预先实现它 |

---

### 追加登记（跨计划避让：计划 4 已占用 D64–D74，本计划不再续 D 编号）

> 这三条来自**独立评审（agent f785ce41）的「应登记」项**，落在本计划正文里；下游阅读时**不得**把它们误读成计划 4 的 D64–D74（计划 4 的 H8 写明「计划 3 用到 D63」，编号避让是刻意的）。

| 编号 | 位置 | 问题（评审已核实） | 本计划裁定 |
|---|---|---|---|
| **E1** | `internal/observability/model/call_trace.go:48` vs 任务 6 原稿 | 原稿称 `body_missing` 是**本计划新增**的可选字段（`omitempty`、「只在 true 时出现」）——**事实相反**：该字段**早已存在**且 `json:"body_missing"` **没有 `omitempty`**，模型的其它字段（`body_ref_*` / `body_bytes`）才有 `omitempty`。⇒ 既有 trace 端点**今天就已经每条响应都返回 `"body_missing": false`**，「响应逐字节不变」与「新增字段」两句自相矛盾 | 任务 6 的正文已逐字订正：**不新增任何响应字段**，只改该字段的**取值语义**（归档形态下 `true` 开始出现）；`contracts.json` 的同步判定改为「是否含该端点的响应 schema」而**不是**「新增字段」。**响应逐字节不变的措辞已删除**（除该字段取值外，字段集合不变） |
| **E2** | 全局约束「读侧的分层归属」/ 任务 2 原稿的 `DayOf` | 原稿要求服务从 router 拿到 control-plane 侧的日期换算函数（与写入侧同口径）。**但 `internal/controlplane/config` 里没有任何日期换算导出符号**（唯一实现在 `internal/gateway/config/trace_body.go:166`，而 control-plane **不 import** gateway/config）⇒ 无落点、接线片段不可编译；且它与任务 4 的 CHECKS「读侧不自算日期」**互斥**（一边禁止自算、一边要求注入一套换算） | **读侧零日期计算**（全局约束新增条目）：定位键的 `dt` **一律取自行内已存的 `body_ref_dt` 列**（写入侧算好），读侧**不做任何日期计算、不注入任何日期口径**。服务选项里的日期换算字段与接线里的注入写法**整体删除**；唯一保留的是 `trace.BodyRefDT.Format("2006-01-02")` 这一**布局格式化**（不是换算）。任务 4 的 CHECKS「读侧不自算日期」**保留**并改成「零日期计算」的口径（新增 `time.Now(` 到 pattern） |
| **E3** | 计划 4 `:106` / H9 vs 本计划任务 8 的 A 表 | **跨计划归属缺口**：计划 4 的 A 表写「A1 / A2 / A4 / A5 ❌ 不在本计划（**属计划 3 读侧**）」，H9 同样写「A1/A2/A4/A5 归计划 3」；而本计划任务 8 的原 A 表**从未登记 A5**（登记了 A1/A2/A3/A6/A7/A8/A9），A4（读 1000 次目录占用零增量）则**已由计划 1 的 `constraints_test.go` 覆盖**。**已复核规格**：§14.2 第 960-961 行**确有 A4 与 A5**（A5 = 篡改 footer 一个字节 ⇒ 500 + 诊断，不返回「不存在」）——缺的是**计划 2 任务 8 的 A 表**（它没有 A5 行），于是计划 4 按「计划 2 的 A 表」把 A5 划给读侧、而本计划原 A 表又没有这一行 ⇒ **A5 实际无人承担**（下游若按字面理解会以为它已被计划 3 覆盖） | 任务 8 的 A 表**补上 A4/A5 两行**并逐字写明边界：A4 = **计划 1 已覆盖、本计划不重验不声称**；A5 = **本计划承担对外映射与分类**（任务 2 的哨兵透传 + 任务 3 的损坏 ⇒ 500 `part_corrupt` + 任务 4 的「隔离件不是损坏」断言），库层由计划 1 覆盖；**且逐字写明「真实产物篡改一字节的端到端用例不在本计划」这一残余缺口**。本计划承担 **A2 + A5（对外映射，非端到端）+ A1 的读回半部**。**建议**发起者把计划 4 的 `:106`/H9 订正为「A1（读回半部）/A2/A5 归计划 3；A4 由计划 1 覆盖」——订正属发起者决策 |

### 追加登记（第二轮独立评审，2026-10-08：E6–E8）

> 与 E1–E3 同一条纪律：**只登记与订正文本，不新增变异、不改动任何被提交的集合**。任务 1 的 `internal/controlplane/config/trace_body_read.go`、`config.go`、`tests/controlplane/config/trace_body_read_test.go` 与 `tools/trace-mutate/sets/p3-task-1.py` 已随 HEAD `71766032` 提交，其**变异权威表绑定在该提交上** ⇒ 任何对被测代码或集合的改动都必须重跑并重记绑定，属**下一轮**（本轮对上述四个文件**零改动**）。

| 编号 | 位置 | 已核实的事实（本轮复核，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E6** | `internal/controlplane/config/config.go:111-114` + `trace_body_read.go:97-99` + `vendor/github.com/spf13/viper/viper.go:444` vs 任务 3 / 5 / 8 的 200/503 判据 | 三个 `trace_body.*` 键都注册了 `SetDefault` ⇒ 生产路径上 `IsSet` **恒为 true**，`trace_body_read.go:97-99` 的回落分支在生产上是**死代码**（无害，默认值与回落值同为 2）；`viper.go:444`（`allowEmptyEnv` 默认 false）让**空串环境变量被跳过** ⇒ `TRACE_BODY_ROOT=""` **无法**覆盖 YAML 里的 `root`，「功能关闭」只能靠 **YAML 缺键**实现 | 语义逐字写进**全局约束**的新增条目「读侧键的存在性 / 空串语义（已核实的行为）」；任务 5 的 200/503 段下加了一条三格判据，任务 8 的互异性矩阵下加了一条依赖提示（「设了空 root 仍走归档 ⇒ 先查 YAML」）。**明确标注为「已核实」，不是推测**；**不影响 E5 的裁定**（显式 0/-1 仍必须被拒） |
| **E7** | 任务 1 的 `REQUIREMENTS` 映射表左列 vs `trace_body_read.go:100` 的 `c < 1` 分支 | `c < 1`（显式 0/-1）**只有测试承重、没有变异承重**：集合 `tools/trace-mutate/sets/p3-task-1.py:179` 的 note 已如实登记「本集合无对应变异」；且 `TestTraceBodyReadConfigMaxConcurrentBounds` 是**共享断言环**（`{-1,0,17,64}` 同一循环）⇒ 0/-1 子例的承重无法从整条红里分辨（受 17/64 遮蔽）。原映射表左列 `并发上界 → m04` 读起来像已覆盖 | 映射表**措辞订正**为「`并发上界` → m04（**只覆盖 `>16` 的一侧**）」，并在表下增「已知缺口」第 1 条。**不新增 m10**（会改动已提交并绑定的集合，见本表表头） |
| **E8** | 任务 8 的窗口耦合：读侧上限 vs 计划 4 的 sealer 窗口（`trace_body_read.go:29` / `internal/tracearchive/defaults.go:57` / 计划 4 `:349` / `:781`） | `traceBodyWriteSideWindowMB = 512` 是冻结包 `defaultZstdWindowBytes` 的**手抄副本**；写侧无窗口键，计划 4 的 sealer **独立**声明窗口。窗口不一致时 klauspost 流式解码在帧头返回 `ErrDecoderSizeExceeded`（`decoder.go:861`），冻结包在 `reader.go:232-263` 包成「解压读取失败」且**不命中任何哨兵** ⇒ 对外是 500 `internal_error` **固定文案**（**不是**评审假设的 `ErrFooterCorrupt`，因此也不能靠「不是 ErrFooterCorrupt」这种恒绿断言来覆盖） | 任务 8 新增「**跨计划耦合**」块：残余缺口（端到端窗口不一致用例不在本计划，理由三条，与 A5 的残余同形）＋ 替代证据（任务 1 的配置期用例、`p3-task-8.py` 新增 CHECKS 行「冻结包默认窗口未被静默改动」、交接 **H18**）；并在计划里**禁止**把窗口类失败归入 `part_corrupt` / `not_archived` / 404。跨计划契约写进 **H18**（只引用计划 4 的任务 1 / 任务 5，不改它） |

---

### 追加登记（任务 2 实现轮，2026-10-08：E9–E12）

> 编号续 E8（**E6–E8 已被上一轮占用，故本轮的缺陷登记从 E9 起**；派单口令里的「从 E6 起」是在 E6–E8 落盘之前写下的）。与 E1–E3 同一条纪律：**只登记与订正文本**。本轮的订正随**单独提交**落盘（不混进任务 2 的实现提交）；任务 2 的实现提交 = `a2f024b2`（3 文件 1003 插入），其变异权威表绑定在该提交与随后的订正提交上（**新提交后已重跑并重记绑定**）。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E9** | 任务 2 步骤 3 的「硬形状」与判定顺序 1 vs 全局约束「四者之外的形态 ⇒ 500」 | 原稿的判定顺序 1（`body_missing == true ⇒ ErrBodyMissing`，**无条件**且「必须早于定位键判定」）与三处文本互斥：① 全局约束「四者之外的形态（含 `body_missing = true` 且定位键非 NULL）⇒ **500 + 明确诊断**，**绝不报 404**」；② 本节接口注释（`ErrTraceBodyInconsistent` 覆盖「`body_missing=true` 却带定位键」）；③ 步骤 1 的用例 `TestTraceBodyServiceBodyMissingWinsOverEverything` 第二格要求 `ErrTraceBodyInconsistent`。按原文字面实现，②③ 成为不可达的死代码，且「数据写坏」会被伪装成「正文从未落盘」（正是规格 §8.1 要消灭的一类错误） | **全局约束优先**：判定顺序 1 补上限定「**且四个定位键全为 NULL**」，并新增 **1'**「`body_missing=true` 却带定位键 ⇒ `ErrTraceBodyInconsistent`（500）」。步骤 3 的硬形状按此逐字订正（三种形态共用**一处**不一致出口，判据仍是纯内存 ⇒ 1' 不破坏「先于任何存储访问」）。实现取此裁定 |
| **E10** | 任务 2 步骤 5 的 `MUTATIONS`（m02 / m04 / m07） | 三条按原文逐字使用会让该集合**零产出**或产出**编译红**（都违反全局约束第 5 条「变异必须是断言红」）：① **m07** 的定位串 `return nil, err` 在服务文件里出现 **3** 次（构造期两条 `return nil, errors.New(...)` 也以它起手）⇒ 工具以 `count=3` 抛 **PATTERN-MISS**，整轮在跑变异之前就终止；② **m02** 的 `cut` 删掉 `if trace.BodyMissing {` … 其闭合 `}` … 与 **`if len(refs) > 0 {` 这个开括号**，却把后者的闭合 `}` 留下 ⇒ 括号失衡（**实测**：把该 cut 施于原硬形状片段后 `gofmt -e` 退出 2，`expected declaration, found 'return'`）；③ **m04** 的替换串把 `src`/`body` 的**唯一使用**换掉 ⇒ `go build` 退出 1，`declared and not used: body` + `: src` | 三条**改写为断言红**并逐字登记在集合头：m07 改用带缩进的精确行 `"\t\treturn nil, err\n"`（唯一；同时要求透传点位于函数体第一层）；m02 改为摘掉「`body_missing` 与定位键矛盾」这条判据（`!allRefs \|\| trace.BodyMissing` → `!allRefs`）；m04 的替换串用 `_, _ = body, src` 保留两个变量被引用。**m01/m03/m05/m06 逐字保留**（m06 的 `if false {` 实测**可编译**：它连 if-init 一起去掉，`err` 并未留下——与任务 1 的 m05「保留 init 而让判据恒假」是两种形态，别混为一谈） |
| **E11** | 任务 2 的判定顺序 0 vs 步骤 5 的 7 条变异 | 任务 2 明文「判定顺序 **0..4** 逐条都是承重的，**任何一步**被摘掉或换序都必须有变异红」，但步骤 5 的 7 条变异**没有步骤 0**（记录缺失 ⇒ 透传 `gorm.ErrRecordNotFound`）⇒ 「每一条承重」对步骤 0 无证据；且 `REQUIREMENTS` 映射表若为它留行会因「既无变异也无静态检查」被判 **GAP**（工具整轮失败） | **补入 `m08_记录缺失被吞掉`**（把透传换成 `ta.ErrBodyMissing` ⇒ 杀手 `TestTraceBodyServiceRecordNotFound` 转红，实测断言红），并把步骤 6 的预期由「7 条断言红」改为「**8 条**」；另补一行静态检查「记录缺失保留gorm哨兵」钉住 `%w` 包装形态。集合头如实登记「相对计划的一处补入」 |
| **E12** | 任务 2 步骤 1 的 `TestTraceBodyServiceRejectsIllegalRefBeforeIO` | 原稿要求「行的 dt 值是 `"2026-09-24/../.."`（模拟列被写坏）」。但 `body_ref_dt` 是 `*time.Time`（`type:date`；正是计划 2 为避免字符串往返有损而选定的类型，见 `call_trace.go` 的字段注释）⇒ `.Format("2006-01-02")` **恒**产出合法日期。**实测**：往该列写文本 `"2026-09-24/../.."`（raw INSERT 成功），读回来是**零值时间**、`.Format` 得 `"0001-01-01"`——仍是合法日期 ⇒ 服务层**不可能**构造出非法 dt，该子例不可能成立 | 该用例改用**可达**的非法向量（`inst` 穿越向量 / `inst` 空串 / `part` 超上界 / `part` 为负 / `row` 为负，共 5 格）并保留 `calls==0` 断言；**dt 的守卫由任务 1 的配置层用例承担**（`TestValidateRefRejectsIllegalComponents` 的 dt 子例）。**不得**为此改模型（把 `*time.Time` 换成 `*string` 会重新引入计划 2 已消除的往返有损） |

### 追加登记（任务 2 修复轮，2026-10-08：E14–E17）

> 编号续 E12。按派单口令**从 E14 起**（E13 在本计划正文中从未出现；为避让跨计划 / 跨轮编号，本计划不使用 E13）。与 E1–E3 同一条纪律：**只登记与订正文本**。本轮的**代码 / 测试 / 变异集合**改动随**单独提交**落盘（`fix(observability): 导出点读定位键并修正恒真断言`，3 文件），其变异权威表绑定在**该新提交**上（**已重跑并重记绑定**，旧表保留）；计划文本订正随**另一个提交**落盘（不混进代码提交）。依据是任务 2 的独立评审（可合并、无阻断项）与本轮实测。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E14** | 任务 7 的 `--ref` 输出要求 vs 任务 2 的 `TraceBodyResult`（跨任务缺口） | 任务 7 要求 CLI `--ref` 输出 dt/inst/part/row **四分量**且**复用 `TraceBodyService`**，但任务 2 的 `TraceBodyResult` 原先**只有 `{Body, Source}`** —— 服务层没有任何取回 `ta.Ref` 的导出途径。后果：CLI 若真要输出四分量，**只能自己查 `call_traces` 的 `body_ref_dt/inst/part/row` 四列再拼**，从而绕过服务层的定位键判据（`ValidateRef` 先于 IO / 部分 NULL ⇒ 不一致）⇒ 「什么算合法定位键」出现**第二份实现**，正是 **D59** 警告的「同一判据两处实现」形态（D59 本身登记的是冻结包 `GetBody` 与调用侧 `ValidateRef` 的重复，本条是它的第三种潜在重复） | ① 任务 2 增加**导出途径** `TraceBodyResult.Ref ta.Ref`：归档 / 未封块 spool **命中路径**填充本次实际使用的四分量，jsonb 回退（四列全 NULL）为**零值**，其余失败路径不返回结果（语义逐字写在字段注释里）；**不改动**任务 3/5/6 已依赖的既有签名。② 补承重用例 `TestTraceBodyServiceExportsRequestRefOnHit`（归档命中四分量一致 + jsonb 回退零值）。③ 变异集合补 **m09**（成功路径的 `Ref: ref` 换成零值 ⇒ 只杀该用例的归档格，隔离探针），并**随新返回串更新 m04 的定位串**（旧串已不存在）。④ 任务 7 的 `--ref` 措辞改为**点名** `TraceBodyResult.Ref`，CHECKS 由「只禁 `tracearchive.`」改为「同时禁四个定位键列名 / `call_traces`」并新增「ref 取自服务导出」的**正向**检查（详见任务 7）。⑤ 步骤 6 的预期由 8 条改为**9 条**断言红。**若将来执行者宁可不导出，则任务 7 的 `--ref` 必须降级为「只报服务层已有的信息」并在计划里显式删掉四分量要求** —— 不得两处各写一份定位键判据 |
| **E15** | 任务 2 步骤 3 的变异目标字面量（`DT:` 的缩进） | 计划 `:664`（原行号）写 `DT: trace.BodyRefDT.Format("2006-01-02")`（**单空格**），而任务 4 的集合草稿（`:1111`）与实现行都是 `DT:   trace.BodyRefDT.Format("2006-01-02")`（**3 空格**）。若执行者照计划把实现写成单空格，任务 4 的 `m04`（定位串含 3 空格）会 **PATTERN-MISS**，集合零产出 | 计划字面量统一为**与实际行一致的 3 空格**（与任务 4 的集合逐字对齐），并在该处写明「**必须原样保留这一行**」。语义不变（只是缩进），但它是变异定位串的一部分 |
| **E16** | 任务 2 的 `Source` 判定时机措辞 vs 实现 | 计划 `:567`（原行号）写「本服务在**调用前**用 `os.Stat` 判定」，实现是 `Reader.GetBody` **成功返回之后**再 `src := sourceOf(...)`（`Source` 与成功返回绑定在同一处更不易漂移）。**语义等价**：该 `stat` 只决定 `Source` 这个**诊断字段**，不参与任何判定、不改变正文内容；两次 `stat` 的窗口说明已写进代码注释（不得声称 `Source` 原子）。交付报告 §4 已如实登记该偏离 | 计划文本订正为**实际语义**（「`Reader.GetBody` 成功返回之后探测」），并保留「刻意不复用 `GetBody` 返回值判断来源」与窗口说明 |
| **E17** | 任务 2 步骤 1 的 `TestTraceBodyServicePartialRefsAreInconsistent` 的覆盖断言 | 实现用 `seen++` + `require.Equal(t, 14, seen)`：`seen` 每轮恰加一、与循环体**同构** ⇒ 循环少跑 / 跳格 / 提前退出**都不会红**，是一条**恒真断言**（违反全局约束第 6 条要求的证伪能力）。计划步骤 1 只写「4 个键的非空子集（共 14 种）逐个断言」，**没有**要求把「实际覆盖了哪 14 种」断言出来 ⇒ 该措辞允许落成恒真形态。另：同一用例注释把「dt 向量不可构造」的登记号写成 **E11**（应为 **E12**） | 测试改为：① 期望集合由「4 个二值定位键的**全部 16 种形态**」显式推导（排除 `0`=全 NULL 与 `15`=全非 NULL），② 断言**实际驱动的 mask 集合与期望集合逐元素相等**（少跑 / 跳格 / 多跑都红），③ 再用 `Len(…,14)` 钉住 `2^4-2` 的总数。计划步骤 1 的措辞同步补上「断言实际覆盖的形态集合」这一形态要求；登记号订正为 E12。**该恒真断言不影响任何既存变异的判定**（m03 仍由本用例的 `ErrorIs` 断言杀死） |

### 追加登记（任务 3 前的窄范围复审，2026-10-08：E18–E19）

> 编号续 E17。来源：Task 2 窄范围复审的两项发现（**只登记与订正计划文本**，不碰任何 `.go` / 变异集合）。本轮的文本订正随**单独提交**落盘（与任务实现提交分离，沿用 E4/E5、E14–E17 的同一条纪律）。任务 3 实现轮的追加登记见 **E20–E21**。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E18** | 任务 7 步骤 5 的 `CHECKS`（`CLI复用服务不自己拼路径也不直查定位键列` + `CLI的ref取自服务导出`） | **两条都不足以拦住「用 GORM 模型字段绕行」。** 反例（逐字）：`var tr obsmodel.CallTrace` / `db.Where("request_id = ?", id).First(&tr)` / `return ta.Ref{DT: tr.BodyRefDT.Format("2006-01-02"), Inst: *tr.BodyRefInst, Part: *tr.BodyRefPart, Row: *tr.BodyRefRow}` —— **三处全不命中** E14 加强后的原正则：① 表名由模型推导，正文里没有 `call_traces` 字面量（`call_traces` 不命中）；② Go 字段名 `BodyRefDT` ≠ 列名 `body_ref_dt`（四个列名都不命中）；③ 别名 import（`obsmodel "…/internal/observability/model"`）之后既没有 `tracearchive.` 也没有 `tracearchive` 字样。**同时**，正向行 `pattern: r"\.Ref\b"` 会被**自造的** `ta.Ref{…}` 满足（`ta.Ref{` 里就含 `.Ref`）⇒ 它单独起来起不到「防随手构造」的作用；两条合起来仍允许一个**完全绕开服务层**的 CLI 通过全部 5 行静态检查 | **加强（不是重写）两条 CHECKS**：① 禁用项**增补** `obsmodel\.|BodyRefDT|BodyRefInst|BodyRefPart|BodyRefRow|ta\.Ref\{`（直接命中该反例的模型字段、别名限定符与复合字面量），并在 `why` 里逐字写明该 GORM 反例与「为什么原正则需要加强」；② **新增一条正向 present 检查** `NewTraceBodyService\(`（`CLI必须调用服务构造`）—— 禁用项只能证明「没用这些绕行写法」，只有正向构造检查能证明「真的走了服务层」；③ 原 `\.Ref\b` 行**保留**但 `why` 里写明其**效力边界**（会被 `ta.Ref{…}` 满足，承重的是新增的两行）。**残余如实登记**：改名字段（`tr.RefDT`）或拼接表名（`Table("call_"+"traces")`）仍可规避 → 属刻意规避，由正向检查兜底 |
| **E19** | 任务 6 的用例清单 / `CHECKS`（`响应键集合不变` 从未被正面钉住） | 任务 2 新增了 `TraceBodyResult.Ref`（dt/inst/part/row 四分量，`call_trace.go` 的既有 `body_ref_*` 列已带 `omitempty` json tag ⇒ **定位键本来就会随行出现在既有端点响应里**，这是既有形状）。任务 6 若在**模型回填**时把 `res.Ref`（或 `res.Source`）并进 `CallTrace`、给模型加一个 `ref` json 键、或把 `c.JSON(http.StatusOK, trace)` 换成包壳结构，就会**把本计划新增的定位键/来源字段泄漏进既有 trace 端点的响应**（前端与 `tests/contract/contracts.json` 一起漂移）；而任务 6 原稿的 6 条用例与 3 条 CHECKS **都不能失败于**这种改动（m01–m06 全部打在其他判定上） | **补一条承重用例 + 两条 CHECKS**：① 步骤 1 新增 `TestUsageTraceResponseKeySetUnchanged`——两格固定输入（归档形态行 / 功能关闭形态行）**逐字断言 200 响应的顶层键集合**（完整键列表），并断言**不存在** `ref`/`source`/`body`/`trace` 这些本计划可能引入的包装键（该断言确实可失败：给模型加带 json tag 的字段、或换成包壳即红）；② `CHECKS` 补 `trace响应仍是模型整体`（`usage_handler.go` 的 `c\.JSON\(http\.StatusOK, trace\)` present）与 `模型未新增定位键泄漏字段`（`call_trace.go` 不得出现 `tracearchive` / `json:"(ref\|source\|body\|trace)"`）。**无对应变异**（泄漏来自设计层改动，且两处 200 返回点同串出现 2 次 ⇒ 无法做唯一目标的 `sub`），在步骤 5 的集合草稿里逐字注明 |

### 追加登记（任务 3 实现轮，2026-10-08：E20–E21）

> 编号续 E19。来源：任务 3 的实现与变异证据产出过程（**只登记与订正文本**；代码 / 测试 / 集合改动随**实现提交** `feat(observability): 点读错误语义到 HTTP 表现的映射` 与随后的**修复提交**落盘，计划文本订正随**另一个提交**落盘 —— 与 E4/E5、E9–E12、E14–E17 同一条纪律）。第一次权威表因**静态检查恒 FAIL**（断言无关的机械原因）中止，原始输出留档为 `p3-task-3-authoritative-aborted-checks-d0013852.txt`，修正后重跑并重记绑定。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E20** | 任务 3 步骤 1 的 `TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText` | 原稿的**向量与断言互斥**（该用例按原文实现必然恒红）：原稿传入 `fmt.Errorf("%w: %s（块内共 %d 行）", ta.ErrNotArchived, "<ref>", 3)`，却要求 message 包含「行号越界」与「块内共 3 行」。该向量的实际文本是 `tracearchive: 产物与暂存均不存在: <ref>（块内共 3 行）` —— **不含「行号越界」**：那四个字是**计划正文的诊断说明**，不是冻结包的文本；`reader.go:183` 的真实拼装就是 `"%w: %s（块内共 %d 行）"`（另两处：`reader.go:139` `"%w: %s"`、`reader.go:266` `"%w: %s（偏移 %d 未命中行首）"`） | 该用例**按冻结包的真实拼装取三个向量**（两副本都不存在 / 行号越界 / 偏移未命中行首），各自断言其**可分辨子串**原样透出（`产物与暂存均不存在` / `块内共 3 行` / `偏移 4096 未命中行首`）。语义不变（「底层文本必须保留、不得替换成固定文案」），只是向量可达；计划步骤 1 的文本已逐字订正 |
| **E21** | 任务 3 的**证据覆盖缺口**：步骤 5 的 7 条变异 vs 权威判据表 12 行；步骤 5 的 3 行 CHECKS vs 「200 的 body 是正文原字节」 | ① 判据表自己要求「**逐行实现、逐行有用例、逐行有变异或 CHECKS**」，而 m01–m07 只覆盖「未归档 / body_missing / 损坏 / 禁用 / 取消」五类（十二行里的 **6 行**：body_missing / 读侧禁用 / 未归档 / footer 损坏 / trailer 损坏 / 客户端取消），**nil(200) / 记录缺失(404) / 定位键不一致(500) / `ErrPartNotFound`（不产生状态码 ⇒ 落到兜底）/ 读超时(504) / 兜底(500 internal_error)** **六行**（原文写「五行」且漏了 `ErrPartNotFound` 那一行，已按 E22 订正）**既无变异也无 CHECKS**（原 3 行 CHECKS 全是结构不变量）⇒ 工具 `--mapping` 会出现 GAP（整轮失败）；这六行是**行为**要求 ⇒ 按全局约束第 7 条应当用变异覆盖（同任务 2 的 E11 处置）。【算式自洽】6 行已覆盖 + 6 行未覆盖 = 12 行；补入的 5 条变异里 m11 同时覆盖 `ErrPartNotFound` 与「兜底」两行。② 任务 3 是**建响应类型的地方**，而全计划唯一钉住「200 = 正文 JSON 原字节、不包壳」的判据在**任务 5**（`c.Data` + m04）；任务 3 原稿对映射层的 200 路径**没有任何正面约束** ⇒ 若映射层把服务层结果序列化成包壳，任务 2 新增的 `TraceBodyResult.Ref`（定位键四分量）会**直接泄漏进响应**。③ 实测踩坑：两条「不存在性」CHECKS 是对**整个文件文本（含注释）**做 `re.search`，实现首版注释里写了 `gin.Context` / `result.Body` 等字样 ⇒ 两条恒 FAIL，工具在跑变异**之前**就以「静态检查失败」终止（整轮零证据） | ① 补入 **m08–m12**（成功路径不再返回 200 / 记录缺失不再分类 / 定位键不一致不再分类 / 兜底被提前拦截 / 超时不再分类），步骤 6 的预期由 **7 条改为 12 条**断言红，并在步骤 5 里逐字写明补入理由；`ErrPartNotFound ⇒ 落到兜底`（D53 的 410 不可达）随 m11 获得**可失败**形式。② 补两条承重用例 `TestMapTraceBodyErrorSuccessCarriesNoPayload`（nil ⇒ 零值诊断体）与 `TestTraceBodyErrorResponseKeySet`（响应键集合逐字固定、无 ref/source/body/trace 键），并补第 4 行 CHECKS `映射层不包装正文结果`（本文件不得引用上述**字面标识符**：`json.Marshal` / `TraceBodyResult` / `result.Body` / `json.RawMessage` / `json.NewEncoder` / `ResponseWriter` / `WriteHeader` —— 不是「JSON 序列化调用 / 服务层结果类型名 / 响应写出调用」这类描述性类别，措辞已按 E23 收窄、正则已按 E24 增补）；**无对应变异**（属设计层不变量，与任务 6 的 E19 同形），在集合的 REQUIREMENTS 里如实登记为「静态检查 + 可失败断言」承重。③ 实现注释改为中文描述（**不是**放宽 CHECKS），并在步骤 3 的形状硬要求里新增一条「注释里也不得出现禁用标识符」；步骤 4 的 `go build ./...` 订正为 `go build ./internal/... ./cmd/...`（基线失败于 `test/bedrock_model_id_check.go`，见既有失败清单）。**残余**：`--ref`/映射层的「逐字节 Content-Length」断言仍只在任务 5（任务 3 是纯函数、无 HTTP 写出口）——不得声称任务 3 已覆盖字节级响应 |

---

### 追加登记（任务 3 独立评审订正轮，2026-10-08：E22–E25；欠账承接 E26–E27）

> 编号续 E21。来源：**任务 3 的独立评审**（可合并、无阻断项）与本轮实测。与 E1–E3、E9–E12、E14–E17、E18–E21 同一条纪律：**只登记与订正文本**；本轮的**计划文本订正 + `p3-task-3.py` 的注释/CHECKS 改动**随**单独提交**落盘（不混进任何 `.go` 实现提交），其**权威表绑定在该新提交上**（**已重跑并重记绑定**，旧表保留）。**⚠️ 本轮改动含集合（`p3-task-3.py` 的 CHECKS 正则与 why）**，因此按全局约束第 8 条**必须重跑权威表**：新表报 `集合 sha256`、清单 `sha256`、被测/测试文件 `sha256` 与分类计数（**仍须是 12 = 断言红 12 + 0 其余**）。下面 E26–E27 是**两份既有欠账的承接登记**（评审点②/后端交接项），**不得静默消失**。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E22** | 任务 3 的 E21 ① 与步骤 5 的补入注释里的**计数** | E21 ① 写「nil / 记录缺失 / 定位键不一致 / 读超时 / 兜底 **五行**既无变异也无 CHECKS」，但权威判据表有 12 行，m01–m07 覆盖 **6 行**（body_missing / 读侧禁用 / 未归档 / footer 损坏 / trailer 损坏 / 客户端取消）⇒ 未覆盖的是 **6 行**，其中**漏写了 `ErrPartNotFound` 那一行**（`ta.ErrPartNotFound ⇒ （不产生）⇒ 落到兜底`，D53）。原措辞「五行」与「12 行」在算术上不自洽（5 + 5 ≠ 12） | ① 计数订正为 **6 行（含 `ErrPartNotFound` 那一行）**，并**逐个列出六项**：nil(200)、记录缺失(404)、定位键不一致(500)、`ErrPartNotFound`（落到兜底）、读超时(504)、兜底(500 internal_error)；② 同步写明已覆盖的 6 行（算式 6 + 6 = 12 自洽）；③ 同步 `tools/trace-mutate/sets/p3-task-3.py` 头注的计数文字（**E22 这一条只改注释**；该集合的 `CHECKS` 另有 E24/E25 两处改动，`MUTATIONS` 12 条一条未动）。**语义不变**：仍是「这六行都是行为要求 ⇒ 用变异覆盖」，m11 一条同时覆盖 `ErrPartNotFound` 与「兜底」两行 |
| **E23** | 任务 3 步骤 3 的形状硬要求（禁用标识符那一句） | 原措辞写「不得出现 … **JSON 序列化调用** / 服务层结果类型名 / **响应写出调用**」，是**描述性类别**而不是可核对的字面量 ⇒ 执行者无法逐字判断「自己写的算不算」。同时，静态检查**只证明「没抄这些字样」**，证明不了「映射层没有包壳」（手拼 JSON、别名绕过正则都不命中）——原文没写这条效力边界，容易被读成「CHECKS 已证明不包壳」 | ① 措辞**收窄为字面标识符**并逐字枚举（`os.` / `io.Read` / `gorm.DB` / `gin.` / `json.Marshal` / `TraceBodyResult` / `result.Body` / `json.RawMessage` / `json.NewEncoder` / `ResponseWriter` / `WriteHeader`）＝「不得引用上述标识符」；② 逐字写明**真正的兜底是那两条可失败用例**（`TestMapTraceBodyErrorSuccessCarriesNoPayload` 与 `TestTraceBodyErrorResponseKeySet`）；③ E21 ② 的同一处描述性措辞同步收窄 |
| **E24** | 任务 3 步骤 5 的 `CHECKS` 第 4 行「映射层不包装正文结果」的正则 | 原正则 `json\.Marshal\|TraceBodyResult\|result\.Body\|json\.RawMessage` 拦不住「流式/直接写响应」这一族的写法：`json.NewEncoder(w).Encode(...)`、`c.Writer.(http.ResponseWriter)`、`w.WriteHeader(...)` 都不命中。**本轮反正要因 E22 重跑**（集合已改）⇒ 增补这三项是**免费的覆盖提升**（不改任何变异、不改被测代码） | 正则**增补** `json\.NewEncoder\|ResponseWriter\|WriteHeader`（计划正文与 `p3-task-3.py` **同步**）。**硬前置（修复轮的教训）**：这是**不存在性**检查、扫**整个文件文本（含注释）**⇒ 必须**实测干净树上仍 PASS**（否则工具在跑变异之前就中止、整轮零证据）；本轮已实测 4 行静态检查全 PASS |
| **E25** | `p3-task-3.py` 的 CHECKS「纯函数不引入IO」的 `why` 与 REQUIREMENTS 左列 | 原措辞把该要求写成「纯函数：无 IO / 无 DB / **无协议类型**」。但该文件**合法引用 `net/http` 的状态码常量**（`http.StatusOK` / `http.StatusServiceUnavailable` …）——`net/http` 就是协议包 ⇒ 「无协议类型」与实现的事实不符；被禁的其实只是 `gin.` 这一类**请求上下文**（pattern 里写的也是 `gin\.`） | `why` 与映射表左列订正为「无文件系统 / 无 DB / 无 **gin 上下文**（`net/http` 只用于状态码常量 ⇒ 合法引用）」。**只改措辞，不改 `pattern`**（被禁集合没变） |
| **E26**（欠账承接） | 任务 1 步骤 1 的 `TestValidateRefDoesNoFilesystemWork`（`tests/controlplane/config/trace_body_read_test.go` 末段） | 该用例证明「校验不触碰文件系统」的断言是 `_, statErr := os.Stat(root); require.True(t, os.IsNotExist(statErr))`，而 `root := filepath.Join(t.TempDir(), "trace-archive-v4")` **从未被创建**、且 `ValidateRef(ref)` 的签名里**根本没有 root** ⇒ 该子表达**恒真、不可能失败**（违反全局约束第 6 条的证伪能力要求）。「校验先于拼路径」的**结构性**证据（`escaped` 逃出 root）仍然有效，但「不触碰文件系统」目前**只有结构性证据** | **不由任务 4 承接**（任务 4 的范围被该节固定为「隔离件 / spool 兜底 / 行号稠密性与越界 / 跨日跨 part」四件，多一件都不做；且改任务 1 的测试文件会**使 `p3-task-1` 的权威表绑定失效**，必须连集合与清单一起重跑）。**承接方 = 任务 1 的修订轮**（沿用任务 2 修复轮、任务 3 修复轮的既有形态：改测试 → 单独提交 → 重跑并重记绑定）。在修订轮之前，**不得**声称「校验不触碰文件系统」已有可失败证据 |
| **E27**（欠账承接） | 任务 2 的变异集合（`p3-task-2.py`）对服务层 `dt` 分量的覆盖 | `p3-task-2.py` 的 m01–m09 **没有一条**打在服务文件构造定位键的 `DT:` 那一行（`refsOf` 与 `DT:` 构造均无变异）⇒ 「dt 分支在服务层无变异覆盖」。该行**落在任务 4 的 `TARGET`（同一个服务文件）范围内**，且任务 4 的 `m04`（`DT:   trace.BodyRefDT.Format("2006-01-02"),` ⇒ `DT:   *trace.BodyRefInst,`）的杀手正是任务 4 新增的 `TestTraceBodyServiceCrossDayAndCrossPart` | **由任务 4 承接**（不新增变异：任务 4 集合里已有的 `m04` 就是这条覆盖，其杀手 `TestTraceBodyServiceCrossDayAndCrossPart` 必须在任务 4 落地并实测**断言红**）。⇒ 在任务 4 交付前，该欠账保持「未承接」状态；交付后须在任务 4 的权威表里逐字出现 `m04 … 红 … TestTraceBodyServiceCrossDayAndCrossPart` |

---

### 追加登记（任务 4 独立评审订正轮，2026-10-08：E28–E30）

> 编号续 E27。来源：**任务 4 的独立评审**（三条**非阻断项**）与本轮实测。与 E1–E3、E9–E12、E14–E17、E18–E21、E22–E27 同一条纪律：**只登记与订正文本**，本轮的**计划文本改动**随**单独提交**落盘（不混进任何 `.go` 实现提交、**不碰任何变异集合**）。⚠️ **本轮不重跑任何权威表**：三条都没有改动集合、清单、被测文件或测试文件（E28 只登记行号、E29 明确不动 `p3-task-4.py`、E30 只登记覆盖质量注记）⇒ 既有绑定**全部原样有效**，每条绑定仍指其产出时的提交。下面 E29 是**新登记的一条遗留欠账**，承接方 = 任务 9，**不得静默消失**。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E28** | 任务 4 交付报告 §1③ 的行号；E24 的「补强正则」三处落点 | 任务 4 交付报告 §1③ 写「1080（CHECKS 第 4 行）→ 1087；集合 **201 → 201**」，**两处都不准**（实测，逐条给出）：① **旧集合**（`tools/trace-mutate/sets/p3-task-3.py` @ `f0376c4a`）该正则在 **`:201`**（`"pattern": r"json\.Marshal\|TraceBodyResult\|result\.Body\|json\.RawMessage",`）；② **新集合**（同文件 @ `9c4fd450` 起）同一行在 **`:228`**（已增补 `\|json\.NewEncoder\|ResponseWriter\|WriteHeader`）—— 报告写「201 → 201」漏掉了 **+27 行**的位移；③ **计划正文**（`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）该正则在 **`:1088`**（旧 `:1080`，即 E24 的 `"pattern": r"...\|json\.NewEncoder\|ResponseWriter\|WriteHeader", "expect": "absent",`）—— 报告写的「1087」既不是旧行号也不是新行号 | **登记三处落点，后续引用一律以此为准**：**旧集合 `:201` / 新集合 `:228` / 计划正文 `:1088`**。任务 4 交付报告是**留档产物**，其错误行号**不追改**；纠正在本条。⇒ 任何人再引用「E24 的补强正则在哪」时，**不得**沿用报告 §1③ 的 `1080/1087/201→201` 这套数字 |
| **E29**（欠账承接） | `tools/trace-mutate/sets/p3-task-4.py:45`（SKIP_WHITELIST 段的最后一句「权威表须注明本机版本」） | 该行要求把**本机 zstd 版本**写进**权威表**，但**工具生成的表里没有这一行**：`tools/trace-mutate/trace_mutate.py` 的输出是固定段落（`print_binding` / `print_checks` / `print_mapping` / `selftest`），集合模块里**可被打印**的只有 git-tracked 的静态字符串（`CHECKS[].why` / `REQUIREMENTS[].note` / `RETIRED` / `EXPECTED_GREEN`），**没有「运行时探测并打印」的出口**。实测：`p3-task-4-authoritative-at-e0d32ce4.txt` 全文 `grep -n "zstd\|版本"` 只命中 2 行**用例名与说明文字**（`（不需要 zstd）` / `需要真实 zstd`），**没有任何**「本机版本」行 ⇒ 该要求按字面**机制上无法满足**。两条「满足」路径都不可接受：① 改共享工具 `trace_mutate.py` 加探测段 ⇒ **所有集合**（R11 / p2 / p3 全部）的权威表绑定与留档表一起失效；② 把**机器相关**的版本号写进 git-tracked 集合的 `why`/`note` ⇒ 把宿主机状态提交进仓库。任务 4 实际把本机版本记在**交付报告 §3**（`zstd --version` = v1.5.7，与冻结包 `defaultZstdVersion = 1.4.8` 不同），**不是**权威表 | 登记为**遗留欠账**，**留给任务 9 分支级证据重跑统一处理**。任务 9 可在**不变量**之间二选一（由发起者裁定）：① 在权威表的**采集命令**里把 `zstd --version` 与表**一起**落盘成同名旁证文件（如 `<表名>.zstd-version`），**不改工具、不重跑任何既有绑定**；② 给共享工具加**可选的**运行时探测段，但**必须**同时评估「R11 + p2 + p3 全部绑定失效并全部重跑」的代价。**本轮不改 `p3-task-4.py`**：改集合会触发 p3-task-4 的**重跑与重绑**，为一句注释单开一轮不值得。⇒ 在任务 9 处置之前，**不得**声称该要求已满足，也**不得**把它计入「证据链完整」 |
| **E30** | `tools/trace-mutate/sets/p3-task-4.py` 的 `m04_dt分量换成inst` 与权威表 `p3-task-4-authoritative-at-e0d32ce4.txt` 的 m04 行（覆盖质量） | 权威表逐字：`\| m04_dt分量换成inst \| 红 \| names=8 ran=8 diff=0 PASS=0 SKIP=0 FAIL=8 \| …（8 条用例全列）`。该变异把 `DT: trace.BodyRefDT.Format("2006-01-02")` 换成 `DT: *trace.BodyRefInst` ⇒ **8 条用例全部变红（PASS=0）**。它证明的只是「定位键的 dt 分量被拼错 ⇒ 必读错文件」，**区分度最低**：它**不能**把「dt 与 inst 两个分量被互相调换」这一条语义从其余 7 条用例的行为里**隔离**出来（原稿的隔离形态是冻结包内 `Paths.SpoolFile` 的 dt/inst 调换，已按第一硬约束降级为 CHECKS）。⇒ 该条**不满足**「dt-vs-inst 隔离语义」所要求的**隔离**证明（它满足的是更弱的「三分量任一拼错都会读到别的文件」，计划 `:1182` 的措辞） | **不构成阻断**：计划对该条只要求「杀手必须是**具体顶层测试函数名**」——`m04` 已达标（点名 `TestTraceBodyServiceCrossDayAndCrossPart`，且该用例**确实**红，权威表逐字可查）。登记为**覆盖质量注记**：`PASS=0` 是「**广谱红**」而不是「**隔离红**」⇒ **不得**用它声称「dt-vs-inst 的隔离语义已被单独固定」。若后续要补真正的隔离证据，须新增一条**只让 `TestTraceBodyServiceCrossDayAndCrossPart` 红、其余 7 条仍绿**的变异（目标仍是同一个服务文件，代价很小），承接方 = **任务 9 或分支收尾轮**；在补出之前，本条的强度**以「广谱红」为准** |

---

### 追加登记（任务 5 独立评审订正轮，2026-10-08：E31–E36）

> 编号续 E30（**本计划不使用 E13**，沿用正文既有的避让）。来源：**任务 5 的独立评审**与本轮实测（E31/E32 是**报告的事实性订正**；E33/E36 是**承接方指派**；E34/E35 是**补登与失效文本订正**）。与 E1–E3、E9–E12、E14–E17、E18–E21、E22–E27、E28–E30 同一条纪律：**只登记与订正文本**；本轮**只改本计划文本**（不碰任何 `.go`、不碰任何变异集合与清单）。⚠️ **本轮不重跑任何权威表**：六条都没有改动集合 / 清单 / 被测文件 / 测试文件 —— E36 登记的是**任务 5 的提交 `eda06420` 已经造成**的绑定覆盖变化，其重跑承接方是**任务 9**，不在本轮。任务 5 交付报告（`.superpowers/sdd/2026-09-25-trace-point-read-service/task-5-report.md`）是**留档产物**，其不实表述**不追改**，纠正在本表。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E31** | 任务 5 交付报告 §5「字节级响应（m04 的承重）」；任务 5 步骤 5 的 m04 草稿（本计划正文）与 `tools/trace-mutate/sets/p3-task-5.py` 的 `m04_成功响应包一层` | 报告 §5 称 m04「实测**同时打红 ①②③**」；按「三条字节级断言都被这一条变异覆盖」读，**不实**。**实测**（`internal/observability/handler/trace_body_handler.go:127-130` 的 `writeTraceBodyResult` + `p3-task-5.py` 的 m04）：m04 **只替换** `c.Data(http.StatusOK, "application/json", result.Body)` 这一行，紧邻的**上一行** `c.Header("Content-Length", strconv.Itoa(len(result.Body)))` **未被替换** ⇒ 响应头仍是「服务层返回值的长度」⇒ `TestTraceBodyHandlerAdminRoute` 的**断言①**（`Content-Length == strconv.Itoa(len(tbWantBody))`）**仍然绿**。真正变红的是：该用例里 `require.Equal(t, len(tbWantBody), w.Body.Len())`（**实际写出的字节数**变长）＋ 断言②（逐字节相等）＋ 断言③（顶层键集合多出 `body`）；`TestTraceBodyHandlerInternalRouteNeedsToken` 里红的也只是它自己的逐字节相等那条（它自己的 `Content-Length` 断言**同样仍绿**） | 登记为**「字节级三断言的效力边界」注记**，并在任务 5 步骤 5 的 m04 草稿下加同义提示（已加）：**断言① 只固定「响应头由服务层返回值的长度算出」，不固定「实际写出的字节数」** ⇒ **单靠 Content-Length 一条不足以覆盖「包壳 / 改写响应体」这一类**。⇒ 后续任何人引用任务 5 的三条字节级断言时，**不得**写成「三条都被 m04 覆盖」。**结论不变**（逐字节契约由断言②承重）；报告**不追改** |
| **E32** | 任务 5 交付报告 §6.1 的「**行为逐字不变**（既有端点用例全绿）」；`LookupTraceSubject`（`internal/observability/handler/usage_handler.go:175` @ `eda06420`）的覆盖 | 报告 §6.1 的「既有端点用例全绿」**不实** —— 不是「绿」，而是**根本没有用例**。**实测**：① `grep -rn "traceSubject\|LookupTraceSubject\|\.Trace(\|ScopedTrace" tests/` ⇒ **0 命中**；② `tests/observability/handler/` 只有 `prompt_mask_test.go` / `trace_body_error_test.go` / `trace_body_handler_test.go`，无一触及 `UsageHandler.Trace/MyTrace/ScopedTrace` 或 `traceSubject`；③ 全仓唯一在测试里构造 `NewUsageHandler` 的是 `tests/integration/usage_dashboard_service_split_test.go:161`（`NewUsageHandler(nil, db)`），它**只调 `GetDashboard`**。⇒ 任务 5 的归属判据抽取目前**只有逐行等价的文本证据**（`traceSubject` 委托给包级 `LookupTraceSubject`，`git show eda06420 -- internal/observability/handler/usage_handler.go` 可查）⇒ **`LookupTraceSubject` 零用例、零变异** | 表述订正为「**无既有用例、仅逐行等价**」（**不得**再写「既有端点用例全绿」）。**覆盖缺口指派给任务 6**：**零用例零变异，须由任务 6 补** —— 至少一条**能失败**的行为用例（归属不满足 ⇒ 既有端点与点读端点**都拒绝**，且**拒绝发生在取正文之前**；含 fail-closed 的「查不到 / `user_id` 为空」两格与一条**阳性对照**；点读一侧的 `subject` 必须注入**生产实现** `LookupTraceSubject`），**并在集合纪律允许时为它补一条变异**（`LookupTraceSubject` 就在 `p3-task-6` 的 `TARGET` 文件内 ⇒ 该变异**落在目标文件里**，不构成新集合）。任务 6 落地后须在 `p3-task-6` 的权威表里逐字出现该变异的「**红** + 顶层杀手用例名」。任务 6 步骤 1 已按此补入 `TestLookupTraceSubjectRejectsUnknownOrEmptyOwner`（用例名以落地实现为准） |
| **E33** | 任务 5 交付报告 §6.5 的三条「只有静态证据」项；任务 8 / 任务 9 的范围 | 三条要求此前**既无行为证据、也无承接方**：① 管理面权限门 `PermUsageViewAll`（实测 `grep -rln "router.Setup" tests/` ⇒ **0 命中**，仓库没有任何 `router.Setup` 测试包）；② 不新建鉴权中间件（**不存在性**，只有 `p3-task-5.py` 的 CHECKS）；③ **真实 `router.Setup` 装配**（`trace_body.root` 启用/禁用两态）。**范围核对**：任务 9 只承接「冻结包零改动 + 集合重跑 + 分支级证据」（`git diff` 与 `trace_mutate.py`，不起真实进程）；任务 8 原稿明确「不改任何生产代码」且文件只有 `tests/observability/service/trace_body_acceptance_test.go`（服务层验收，**不碰路由 / Setup**）⇒ 按原文两条**都不承接**，三条会**静默悬空** | **逐条指派，不得留下无承接方的条目**：**「真实 `router.Setup` 装配（`trace_body.root` 启用 / 禁用两态）」= 任务 8（生产形态验收）承接，任务 9（分支级验收）兜底**；**权限门 `PermUsageViewAll` 与「不新建鉴权中间件」同挂任务 8**（同一条装配用例即可覆盖：装配后的路由上「无 token ⇒ 被既有 `JWTAuth` 拒」「有 token 但无 `PermUsageViewAll` ⇒ 被 `guard` 拒」「两态各自的点读表现」；「不新建鉴权中间件」的**不存在性**仍由任务 5 的 CHECKS 承重，装配用例补的是**行为侧**）。任务 8 的该承接项与 A2 **并列**（不是 A2 的子项），其文件与规模由任务 8 自行决定；**若任务 8 因 DB/Redis 依赖不可得而未产出该用例，任务 9 必须把该缺口逐条写进 `R11-v1.md` 的「覆盖缺口」**，不得静默。⚠️ **不得**把本条解释为「复核任务 5 的 m06 降级裁定」—— m06 降级的是「为一条断言造**变异**」的形态，本条要的是**行为验收**。任务 8 与任务 9 的正文已分别补入承接块与兜底条目 |
| **E34** | `tools/trace-mutate/sets/p3-task-5.py` 头部「相对计划的改动」段 vs 任务 5 步骤 5 的 `MUTATIONS` 草稿（本计划正文） | 任务 5 集合的 m01 / m02 / m04 三条**替换串**被订正过（**目标串、变异名、语义一字未动**），理由与 **E10** 同形（草稿按字面使用会产出**编译红**，违反全局约束第 5 条「变异必须是断言红」）：① **m01** 草稿 `("sub", "if !found {", "if false {")` 在实现上让 `found` 只剩这一处引用 ⇒ `declared and not used: found` ⇒ 编译红；实际用 `if !found && false {`（`found` 仍被引用 ⇒ 可编译，分支永不执行 ⇒ 语义不变）。② **m02** 草稿 `"if false {"` 同理让 `userID` / `keyType` 未被使用 ⇒ 编译红；实际用「原条件 `&& false`」。③ **m04** 草稿 `c.JSON(http.StatusOK, gin.H{"body": json.RawMessage(result.Body)})` 需要给 handler 文件引入 `encoding/json`，而该文件的设计约束是**不序列化、不包壳**（没有合法用点）⇒ 只能加空引用，属为工具而生的代码；实际改用**不需要新增 import** 的 `append` 拼出 `{"body":<原字节>}`。**m03 / m05 逐字采用草稿** | **补登**（此前只在集合头与任务 5 报告 §4 登记，**未进计划**）：实际集合以 `tools/trace-mutate/sets/p3-task-5.py` 头部**逐字为准**；**目标串 / 变异名 / 语义未动**，只订正替换串。任务 5 步骤 5 的草稿代码块**保留原样**（它是历史草稿），其后已加一行指向本条与集合头的提示 |
| **E35** | 本计划 `## 文件结构` 表的 handler 行（原 `:197`） | 该行写 handler 文件含「`ErrTraceBodyRecordNotFound` 等 handler 层哨兵」—— **已成失效文本**：① 全局约束逐字写「**不得新增哨兵**」；② 全仓**无任何消费者**（实测 `grep -rn "ErrTraceBodyRecordNotFound" --include=*.go .` ⇒ **0 命中**）；③ 任务 5 的两条端点语义表（404/403/400 各自由 handler 直接出响应）**不需要**它 ⇒ **不实现是正确的** | **订正该行**为「**不含** handler 层哨兵」（已改，并写明原因为全局约束的禁令 + 无消费者 ⇒ 新增即死代码），避免后续执行者照文件结构表去补一个被禁令排除的符号。**不得**为了对齐这张表而新增未使用的导出哨兵 |
| **E36** | `tools/trace-mutate/sets/p3-task-3.py` 的 `PACKAGES = ["./tests/observability/handler/"]` 与 `p3-task-3` 权威表的「测试文件 sha256」段 | 工具按 `PACKAGES` **目录级枚举**该包下全部 `*_test.go` 并逐个打印 sha256（`trace_mutate.py` 的 `test_files_of` + `print_binding`，实际是 `os.walk` 全目录）；**实测**旧表 `p3-task-3-authoritative-at-e0d32ce4.txt` 与 `…-at-9c4fd450.txt` 各只有 **2 行**测试文件哈希（`prompt_mask_test.go` / `trace_body_error_test.go`）。任务 5 的提交 `eda06420` 在该目录新增 `trace_body_handler_test.go`，任务 6 的提交 `d2bea971` 又新增 `usage_trace_archive_test.go` ⇒ 当前树上该集合的测试文件集合是 **4 个**（**E38 订正**：原文写「3 个」，并把验收写成「第三行」，两者在任务 6 落地后都不成立），**旧的「测试文件 sha256」段不再覆盖当前树** ⇒ 按严格口径该集合**需要重跑并重记绑定** | **承接方 = 任务 9**：任务 9 步骤 2 的全量重跑**本来就包含 `p3-task-3`** ⇒ 其新权威表的「测试文件 sha256」段**必须出现 4 行**（`prompt_mask_test.go` / `trace_body_error_test.go` / `trace_body_handler_test.go` / `usage_trace_archive_test.go`；**E38 订正**：原文写「第三行」），这就是本条的验收形式。**本轮不重跑**（第 1 步只改计划文本）；在任务 9 交付前**不得**声称 `p3-task-3` 的绑定覆盖了当前树。任务 9 步骤 3 的「覆盖缺口」清单已补入对应条目 |

---

### 追加登记（任务 6 独立评审订正轮，2026-10-08：E37–E39）

> 编号续 E36（**本计划不使用 E13**，沿用正文既有的避让）。来源：**任务 6 的独立评审**与本轮实测（E37 是**报告的事实性与口径订正**；E38 是**对 E36 计数的订正**；E39 是**两条效力边界的新登记**）。与 E1–E3、E9–E12、E14–E17、E18–E21、E22–E27、E28–E30、E31–E36 同一条纪律：**只登记与订正文本**。⚠️ **本轮对证据只有一处影响**：`tools/trace-mutate/sets/p3-task-6.py` 的 `m03` 注记失实 ⇒ 只订正其**文字**（**不改任何变异 / 静态检查 / 清单 / 被测代码**），并按证据制度**重跑 `p3-task-6` 并重记绑定**：新表 `.superpowers/sdd/2026-09-25-trace-point-read-service/p3-task-6-authoritative-at-1687ec86.txt`，旧表 `-at-d2bea971.txt` **保留**；集合 sha256 由 `6f63f2f4ae59da07a8c2c70be6560ec5456df17bfafa0dd820bb25a008be0999` 变为 `46fc4783e8d1075e97e5bcae201255e51aacf23a5e50a99b2557333c2b268658`，计数仍为 `7 = 7 断言支撑 + 0 崩溃/超时/编译 + 0 允许的绿`（`SKIP=0`；`原始侧自证: OK names=8 ran=8 diff=0 PASS=8 SKIP=0 FAIL=0`）。E38 / E39 **只改本计划文本**：E38 订正一处计数（**不重跑任何表**）；E39 **只登记、不实现**（不动任何测试与代码）。任务 6 交付报告（`.superpowers/sdd/2026-09-25-trace-point-read-service/task-6-report.md`）是**留档产物**，其不实表述**不追改**，纠正在本表。

| 编号 | 位置 | 已核实的事实（本轮实测，不是推测） | 本计划的裁定 / 登记 |
|---|---|---|---|
| **E37** | 任务 6 交付报告 §9 的首句「偏离与歧义（**均已在集合头部逐条登记**，无静默丢弃）」；`tools/trace-mutate/sets/p3-task-6.py` 头部「相对计划的改动」段的 **m01** 与 **m05** 两条 bullet；本计划 Task 6 `:1667` 的步骤 6 预期句 | ① **`router.go` 的偏离不在集合头部**：集合头部只有 `:158`（`CHECKS` 的 `why`）与 `:274`（`REQUIREMENTS` 的 note）两处**顺带**提到 `router.go`，两处都**不是**在登记这条偏离；该偏离**只**出现在报告 §9① 与代码注释 `internal/controlplane/router/router.go:473-484`（「为什么必须在 `adminGroup` 之前构造」）⇒ 「四项偏离**均已在集合头部**逐条登记」对 ① **不成立**。② **m01 的用词是误称**：集合头部 m01 逐字写「草稿的**替换串**是 `("sub", "if err := h.hydrateTraceBody(c.Request.Context(), &trace); err != nil {", ...)`」—— 引号里的是草稿的**目标串**（`("sub", a, b)` 的 `a`）；真正的**替换串**是 `b` = `"if err := error(nil); err != nil {"`，它在集合里**逐字沿用草稿、并未订正**（计划步骤 5 草稿 `:1604-1606` 与集合 m01 第三行同串）⇒ m01 的偏离是**目标串订正**（草稿 `&trace` → 实现 `trace`，原因是实现把「回填 + 裁剪」抽成唯一出口 `hydrateThenMaskTrace`），**不是**替换串订正。③ **m05 不是「替换串订正」而是机制改动**：草稿 `:1624-1627` 是 `("cut", "…hydrateTraceBody…", "\tMaskTracePrompt(&trace, canViewPrompt)")`，该 `cut` 会把**回填与裁剪两条语句一起删掉** ⇒ 归档正文根本不会被取回 ⇒ RBAC 的「不得出现」断言**恒绿**（不是断言红，违反全局约束第 5 条）；集合改为 `("sub", <回填+裁剪两行>, <裁剪+回填两行>)` 的**等价交换**（正是草稿注释的语义「回填被挪到裁剪之后」）⇒ **目标串、替换串、机制三者都变了**。④ 报告 §9③ 称「集合补入 m07（计划 6 条 → 7 条）」，而本计划 Task 6 步骤 6（**订正前**）写「预期 **6 条**断言红」、集合头部却逐字写「步骤 6 的预期由 **6 条改为 7 条**」⇒ **计划文本没有同步**（本轮已就地订正，见右列 ④） | **登记口径按以下写准（后续引用一律以此为准；报告 §9 首句与集合头部 m01 的用词不得再沿用）**：① **`router.go` 的偏离登记在报告 §9① 与代码注释 `router.go:473-484`，不在集合头部**（集合只登记集合内部的偏离）；② **m01 = 目标串订正**（替换串沿用草稿），写「替换串」是误称；③ **m05 = 机制改动（`cut` → `sub` 交换）**，理由是原 `cut` 产出**恒绿**；④ 步骤 6 的预期**已就地订正为 7 条**（**只改文字、不改集合**）。⇒ 集合头部实际登记的偏离是 **5 条**（m01/m02/m06 目标串订正 + m05 机制改动 + m07 补入），报告与代码注释另有 `router.go` 1 条 ⇒ 报告 §9 把偏离概括为「四项」并称「均已在集合头部」在**归类**与**载体**两层都不准确。**另**：报告 §9④ 称该 `ErrBodyMissing` 分支「在真实服务下**不可达**」**与事实相反**（`trace_body_service.go:211-216` **原样透传** Reader 的 `ErrBodyMissing`；`reader.go:252/338` 对**被精确定位的行**遇标记行即返回它；`trace_body_quarantine_test.go:69-83,370-381` 正是「四键全非 NULL + `body_missing=false`」的 PG 行 + 标记行并断言 `ErrorIs(ErrBodyMissing)`）⇒ 该分支**可经真实服务命中**，仅因今日写路径失败走 refs 全 NULL 路径（`trace_event.go:733-748`）而**在生产上罕见**；该条已随本轮在集合 m03 注记里订正为正确表述（**只改文字、不改代码**），本计划不为其单列编号 |
| **E38** | **E36**（上一节末行）与本计划「任务 9 步骤 3」的【E36，本轮承接】条 | E36 写「本轮重跑的 `p3-task-3` 权威表**必须出现第三行**（`trace_body_handler_test.go`）」，该计数**在任务 6 落地后已不成立**：任务 6 的提交 `d2bea971`（`git show --stat d2bea971`）在**同一目录**又新增 `tests/observability/handler/usage_trace_archive_test.go`（468 行）⇒ **实测**（`ls tests/observability/handler/*_test.go`）当前为 **4 个**：`prompt_mask_test.go` / `trace_body_error_test.go` / `trace_body_handler_test.go` / `usage_trace_archive_test.go`；而 `tools/trace-mutate/sets/p3-task-3.py:96` 的 `PACKAGES = ["./tests/observability/handler/"]` 是**目录级枚举**（`trace_mutate.py` 的 `test_files_of` 走 `os.walk` 全目录）⇒ 重跑表的「测试文件 sha256」段必然打印 **4 行** | **订正 E36 的验收形式**：任务 9 重跑的 `p3-task-3` 权威表的「测试文件 sha256」段应为 **4 行**（**不是**「第三行」），第四行为 `usage_trace_archive_test.go`；同理，**任何** `PACKAGES` 覆盖该目录的集合（`p3-task-3` / `p3-task-5` / `p3-task-6`）在本轮之后的重跑表都应为 **4 行** —— **实测佐证**：`p3-task-5-authoritative-at-d2bea971.txt` 与 `p3-task-6-authoritative-at-1687ec86.txt` 各已含 4 行测试文件哈希，而 `p3-task-3-authoritative-at-e0d32ce4.txt` / `-at-9c4fd450.txt` 仍是 **2 行**。⇒ 「第三行」的口径**不覆盖当前树**；在任务 9 产出 4 行表之前，**不得**声称 `p3-task-3` 的绑定覆盖当前树。**本条不重跑任何表**（E36 行与「任务 9 步骤 3」的对应句已**就地订正**为 4 行） |
| **E39** | ① `internal/observability/handler/usage_handler.go:300-305`（`traceHasArchiveRef` 的形态判据与其「两种都不该在这里静默回填」的注释）；② `tests/observability/handler/usage_trace_archive_test.go:385/399`（`TestUsageTraceResponseKeySetUnchanged` 两格的替身构造） | ① **效力边界（诊断性）**：`traceHasArchiveRef` 只认「3 个 jsonb 列全 NULL **且** 4 个定位键全非 NULL」；对「**部分**定位键非 NULL」的行（服务层会判 `ErrTraceBodyInconsistent` ⇒ 500）**直接跳过** ⇒ 既有 trace 端点在这类行上**既不取正文、也不留任何 WARN / 日志**（正文静默为空），该不一致形态在既有端点**没有任何可观测信号**。② **效力边界（证据强度）**：两格的替身逐字为 `result: &obssvc.TraceBodyResult{Body: utaArchiveBody, Source: obssvc.SourceArchive},` —— **未设 `Ref`**（零值）⇒ 若实现把 `res.Ref` 的**取值**并进**既有**键（例如覆盖 `RequestBody` / 写进既有的 `body_ref_*` 取值）而不是**新增**键，则 `TestUsageTraceResponseKeySetUnchanged` 只比对**顶层键名集合**，**看不出**这种**取值级**泄漏 | ① **只登记、不实现**：为「部分定位键非 NULL」补一条服务端诊断信号（WARN）属**诊断性改进**，登记为**后续可选**（承接方 = 计划 4 或分支收尾轮）；**本轮不改任何代码**，也**不**把它计成本任务的功能缺口。② **只登记、不改测试**：登记为**当前证据的效力边界** —— 该用例证明的是「**键名集合**未被改动」，**不证明**「`res.Ref` 的**取值**没有被并进既有键」⇒ **不得**把它读成「定位键泄漏已被完全覆盖」。**不得**为补强而翻动这条已产出的证据（改 `usage_trace_archive_test.go` 会让 `p3-task-6` 的清单 sha256 / 测试文件 sha256 与全部绑定失效）；若后续要闭合，须**新增**一条断言**取值**的用例（代价很小），而**不是**改现有断言 |

---

## 自检结果

**规格覆盖度（逐条对照规格 §2/§4/§8/§9/§11/§12/§14/§15/§16 的**读侧相关**条目）：**

| 规格条目 | 落在哪个任务 |
|---|---|
| §2.2 非目标（沿用既有 `InternalAuth` / `JWTAuth`，不重构鉴权） | 任务 5（复用既有中间件，CHECKS 禁止新建） |
| §4.2 组件划分（点读服务 / 点读接口 / CLI） | 任务 2（服务）+ 任务 5（接口）+ 任务 7（CLI）；**路径按仓库现实订正**（D54） |
| §5.2 产物格式（footer/trailer） | **不实现**（计划 1 冻结）；任务 3 映射其损坏语义，任务 8 验其对外表现 |
| §7.1 隔离件命名/发现规则 + 运维判据 | 任务 4（测试）+ 全局约束（规则）+ H10（交接） |
| §8.1 两条端点 + 响应表 | 任务 5（端点）+ 任务 3（状态码/body_missing）；410 行按 D53 处置 |
| §8.2 读取算法 1–5 步 | **库层已实现**（计划 1 `readFromPart`/`readFromSpool`，含 R10 三项）；本计划补第 2.5 步「jsonb 回退」（D55）与错误映射 |
| §8.3 空间约束 C1–C4 | **库层已实现**（计划 1 的 A4 用例）；本计划不重验 |
| §8.4 内存约束 C5/C6 | 任务 1（并发/窗口配置）+ H15（P3 实测回填）+ **H18 / E8**（窗口与 sealer 的窗口参数必须同源或对其可见；端到端不一致用例**不在本计划**，登记为残余缺口） |
| §9.1/§9.2 失败语义 | 任务 2（`body_missing` 判定）+ 任务 3（映射） |
| §9.3 可观测性（明确信号） | 任务 6（hydrate 失败的响亮日志）；读侧本身不产生降级信号（那是写侧） |
| §11.1 部署（control-plane 挂 `/datalog`） | **不实现**（计划 4 的 H8）；本计划实现 env 读取与禁用语义 |
| §12 与现有系统（旧归档分流） | **明确不做**（D58 / H13） |
| §14.1 测试分层 | 全部用例落 B 层 `tests/`（全局约束的证据制度第 3 条） |
| §14.2 **A2** | 任务 8（+ 引用计划 1 的 R11 v3 证据）；**A 表以计划 2 为准**（D56） |
| §15.1 U6 孤儿清理 / R8 / R9 | 不属读侧（写侧/计划 4）；U6 进 H12 |
| §15.2 P1–P3 生产验收 | 进 H15 |
| §16 验收（读侧可验部分） | A2 在任务 8；其余见 H13 |
| **`AGENTS.md`「gateway 不要直连数据库」** | 全局约束「读侧的分层归属」（读侧归 control-plane；不扩大 D46；不新增 binding 客户端）+ D54/D63 + H16/H17 |

**A2 归属的诚实性**：任务 8 **没有**声称覆盖规格 §14.2 原文的 A2（杀 sealer 中途）——那是写侧/sealer 的验收。本计划覆盖的是**计划 2 的 A 表**里的 A2（封块产物可点读、索引正确），并且**明确引用**计划 1 已覆盖的库层部分，只补生产形态缺口（D56 已登记这一冲突）。

**占位符扫描**：全文无「待定」「TODO」「类似任务 N」「参照上文」「按需补充」。每处引用都是**具体文件 + 行号 + 真实符号**（`reader.go:121-140` 的 `GetBody`、`:274-291` 的 `verifyFrameSHA`、`:314-349` 的 `readFromSpool`、`:182-184` 的越界判定、`errors.go:11-27` 的哨兵语义（`ErrPartNotFound` 在 11/13、`ErrNotArchived` 在 18-27）、`index.go` 的 `decodeTrailer` 上界、`footer.go:9-31` 的 `FooterSize`/`formatVersion`、`writer.go:568-580` 的 `quarantineSuffix`/`quarantinePath`、`writer.go:298` 的 `failedRowMarker`、`ref.go` 的 `PartFile`/`SpoolFile`/`partName`、`usage_handler.go:102-147` 的既有 `trace()`、`prompt_visibility.go:27` 的 `CanViewPrompt`、`router.go:41/756/759/769/778-788/992/1004/1022`、`config.go:87-107` 的默认值序列、`sealer.go:154-159` 的入口校验）。

**证据制度的自查**：每个任务都给了 ① 变异集合文件名（`p3-task-N.py`，全部 git-tracked）；② 可粘贴的验证命令；③ TDD 步骤（先红后绿）；④ 每条变异点名**具体顶层测试函数**（目标不在 `TARGET` 文件里、或没有顶层用例可点名的条目，已按要求**降级为 CHECKS 并逐条写明理由**）；⑤ 「必须断言红（非编译/崩溃/超时红）」的显式要求；⑥ `CHECKS` 覆盖变异测不到的结构不变量；⑦ `REQUIREMENTS` 映射表（有缺口整轮失败）；⑧ 冻结包零改动的重复声明与任务 9 的 `git diff` 复核。

---

## 执行顺序与依赖

```
任务 1（配置 + ValidateRef）
  └─▶ 任务 2（点读服务）          ← 依赖 1 的 Config/ValidateRef
        ├─▶ 任务 3（错误映射）     ← 只依赖 2 的两个服务层哨兵
        ├─▶ 任务 4（隔离件/边界回归，只写测试）
        │     └─▶ 需要 2 落地（5 条变异全部打在服务文件上；冻结包侧要求转 CHECKS）
        ├─▶ 任务 5（两条端点）      ← 依赖 2 + 3
        │     └─▶ 任务 6（既有端点回归修复）  ← 依赖 5 的接线（复用同一服务）
        ├─▶ 任务 7（CLI）           ← 只依赖 2
        └─▶ 任务 8（A2 验收）       ← 依赖 2 + 4
任务 9（证据重跑）  ← 依赖 1–8 全部提交
```

**串行约束**：任务 3 与任务 2 **必须**有明确先后（3 的文件引用 2 的哨兵）；任务 6 必须在任务 5 之后（它复用任务 5 建立的注入方式）。**任务 4 与任务 7 可以并行**（互不引用），但**都不得**在任务 2 冻结前开工。

**三个不可跳过的门**：
1. **任务 9 步骤 4 的三条 `git diff` 必须为空**（冻结包 / `atlas-migrations` / `go.mod`+`go.sum`）；
2. **每个任务的变异集合必须存在且 `exit=0`**（任务 9 步骤 2 的 `MISSING SET` 一次都不许出现）；
3. **每个任务的 `REQUIREMENTS` 映射表无缺口**（有要求无条目 ⇒ 整轮失败）。

**若执行者在任何一步发现「必须修改 `internal/tracearchive/` 才能完成」⇒ 立即停止并报告**，不要改动冻结包：本计划的所有要求要么能在调用侧实现（任务 1 的 `ValidateRef` 是先例），要么必须升级为**发起者决策的受控变更 + R11 v4**（D59 已为这一类留了口子）。
