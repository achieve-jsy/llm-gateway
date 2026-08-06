# 调用链正文存储格式与读写原语 实施计划（计划 1/4）

> **面向 Agent 执行者：** 必需子技能：使用 superpower-subagent-driven-development（推荐）或 superpower-executing-plans 按任务逐项执行本计划。步骤使用复选框（`- [ ]`）语法进行跟踪。

**目标：** 实现 `internal/tracearchive` 包——把正文按行追加到 spool、封块为带内嵌索引的 zstd 产物、并支持按 `(dt, inst, part_seq, row_seq)` 定位键原样读回。

**架构：** 写入侧只做顺序追加（请求路径上极快），**压缩由独立的 sealer 进程调用 C 版 zstd CLI（`-19 --long=29`）完成**（方案 C，规格 §5.3.1.1、§7.2）——网关内没有任何压缩动作，编码器内存不计入网关 2 GiB cgroup。产物文件的尾部内嵌「行号→未压缩偏移」索引与 32 字节定长 footer，因此读一行只需 `pread` 末尾 32 字节即可定位索引，无需扫描全文。**trailer 与 footer 永远由 Go 侧追加，zstd CLI 只产出 zstd 帧。** 索引与内容出自同一次封块操作，物理上不可能不同步。

**技术栈：** Go 1.26、`os/exec`（调用 **C 版 `zstd` 二进制**）、`github.com/klauspost/compress/zstd`（**仅解码**）、标准库 `encoding/binary`、仓库既有 `vendor/` 构建方式。

**规格：** `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md`（论证以规格为准，执行者需同时阅读规格与本计划）

## 全局约束

- 仓库使用 `vendor/` 构建，新增依赖后必须 `go mod vendor`，且 `git add vendor/`。
- 代码与注释以中文为主；提交信息用 Conventional Commits（`feat(...)` / `fix(...)` / `test(...)`）。
- **压缩参数终值**（规格 §5.3.3）：**编码器 = C 版 zstd CLI，参数 `-19 --long=29`**（`--long=29` ⇒ 窗口 512 MiB）、编码器并发 `1`（CLI 侧 `-T1`）、块大小 `1~2 GB`、时间阈值 `15 分钟`。Go 库 `klauspost/compress/zstd` **不再用于编码**，只用于解码。
- **方案 C 的三条硬性约束（违反即返工）**：
  1. **编码必须走子进程**：`os/exec` 调 `zstd -19 --long=29 -T1 -c`，不得退回 `zstd.NewWriter`（规格 §5.3.1.1：同参数下 Go 库产物是 CLI 的 1.90 倍，不达验收线）。
  2. **子进程非零退出码 = 封块失败**：保留 spool、删除 `.tmp`、不产生产物、可重试（规格 §7.2、§9.2）。
  3. **trailer 与 footer 由 Go 侧追加**：CLI 只写 zstd 帧；行偏移在喂给 CLI **之前**由 Go 侧顺序扫描 spool 采集（规格 §7.2）。
- **sealer 的发现机制**（规格 §7.2）：扫描 `spool/dt=*/inst=*/part-*.jsonl`，用 **mtime + 封块间隔** 判定「已停止追加」；该判断必须**幂等且可重启**（重启后重新发现同一个 spool 并重试；产物已存在则拒绝覆盖）。
- **测试需要外部二进制**：任务 6/7/8 的封块类用例需要本机有 **C 版 `zstd`（支持 `--long`）**；缺失时用例 skip，**CI 镜像必须安装 `zstd`**，否则核心断言形同虚设（这是方案 C 新引入的测试依赖，规格 §11.1）。另有测试用**假 `zstd` 脚本**（直通 `cat` 或非零退出）来固定失败语义与产物字节布局。
- **zstd 版本必须锁定**（规格 §11.1）：本次标定用的生产机 `/usr/bin/zstd` 是 **v1.4.8**，`10.26×` 是这个版本的实测值（`--long=29` 在该版本上实测可用）。**变更 zstd 版本必须重新标定压缩比并复核解压兼容性。**
- **部署形态（属于计划 4，此处只作为编码约束）**：sealer 是**宿主机 systemd unit**、用宿主机的 `/usr/bin/zstd`，**不进容器、不改 `Dockerfile`**；网关容器只需新增一个 `/datalog` bind mount（规格 §11.1）。因此 `SealerOptions.ZstdBinary` 必须可配置，不能写死在镜像路径上。
- **读取侧空间硬约束**（规格 §8.3 C1~C4）：流式解压命中即停；不落临时文件；不缓存已解压块；读 spool 直接 seek。**任意次读取对磁盘占用的增量必须为 0。**
- **读取侧内存约束**（规格 §8.4 C5）：解码器内存 ≈ 压缩窗口（512 MiB 窗口约需 512~600 MB），并发点读必须有上限。（编码器内存已随方案 C 移出网关。）
- **不变量**（规格 §10.1）：I1 元数据存在 ⇒ 产物 XOR spool 存在；I2 产物只经 `rename` 出现；I3 索引与内容同源；I4 单个 `(dt, inst, part_seq)` 只封块一次；I5 删 spool 前必须确认产物 fsync + 目录 fsync 成功。
- **错误身份是契约**（规格 §8.1）：所有「应当失败」的用例必须用 `errors.Is(err, <哨兵错误>)` 断言，不得只写 `err == nil`。理由：任务 7 的 Reader 依赖 `errors.Is` 区分「产物损坏」与「数据不存在」——把存储故障伪装成业务事实正是本设计要消灭的缺陷。**把实现里的 `%w` 改成 `%v` 后测试必须转红。**
- **每个拒绝/严格性守卫分支都要有能区分它的测试**（规格 §14.1「每道防线逐一摘除 → 对应用例必须转红」）：删掉该分支后必须有测试转红，否则就是覆盖缺口。
- **二进制格式契约用 golden 断言固定**：footer 的 32 字节布局（任务 3）、trailer 的差分 varint 字节（任务 2）、产物整体布局「帧 + trailer + footer」（任务 6）各有一条**逐字节比较**的 golden 测试。理由：编解码自对称会掩盖「字段偏移写错但读写一致」的对称 bug，这种 bug 在自往返测试里全绿。
- **测试文件摆放：混合方案（按可达性划分，不按任务划分）**：
  > **测试摆放判据**：若该测试需要访问未导出标识符，则放包内（`internal/tracearchive/`，`package tracearchive`）；否则一律放 `tests/tracearchive/`（`package tracearchive_test`，仅用公开 API）。**优先选择后者**——它遵守 `docs/guardrails.md` Q3 的意图（不测私有方法）。包内测试是例外，每处请在测试函数注释里说明为何必须触达未导出符号。
  - **为什么不能一刀切**：仓库有两条互相矛盾的约定。`docs/guardrails.md` Q3 明文要求测试放 `tests/` 并「**不要**在 `internal/` 下创建 `*_test.go`」；但实际做法相反——`internal/`+`cmd/` 下有 162 个 `_test.go`（其中 138 个是内部包测试，22 个是外部包），`tests/` 下有 178 个，最近 30 次提交新增的测试全部落在 `internal/`。而本计划的 codec（`encodeTrailer`/`decodeTrailer`/`encodeFooter`/`decodeFooter`/`footer`）是**刻意不导出**的，`package tracearchive_test` **无法访问未导出标识符**——这是硬约束，不是风格问题。
  - **同属一个 module**：`tests/tracearchive/` 与 `internal/tracearchive/` 在同一个 Go module 内，**不需要独立的 `go.mod`**；`go test ./tests/...` 会覆盖它。**不要改动 `Makefile` 或 `.gitlab-ci.yml`**（超出本子计划范围，已由控制者记录为遗留项：CI 目前不跑任何 `go test`，`make test` 只跑 `./tests/...`）。
  - **导入别名统一**：`ta "github.com/example/llm-gateway/internal/tracearchive"`（与 `tests/` 既有风格一致，如 `obsmodel "…/internal/observability/model"`）。
  - **测试辅助函数必须各写一份**：`internal/tracearchive/testhelpers_test.go`（`package tracearchive`）与 `tests/tracearchive/helpers_test.go`（`package tracearchive_test`）。Go 的 `_test.go` 辅助无法跨包共享，且两层里同名辅助（`writeSpool`/`requireZstd`/`fakeZstd`）签名相同、实现相同、**故意重复**。
  - 每个任务的 RED/GREEN 命令必须**同时覆盖两个包**：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run '…' -v`。
- **测试命令用组合 `-run`**：Go 的 `-run` 是**子串匹配**，`-run TestTrailer` 选不中 `TestDecodeTrailer*`（`TestFooter` 同理会漏掉全部 `TestDecodeFooterRejects*`），会让严格性断言全坏时依然绿。**每个任务的 RED/GREEN 命令必须显式列出该任务的全部测试名**（用 `|` 连接）。
- **⚠️ `-run` 合并正则的证据要求（每一个任务都适用；这是实现中发现的一条方法论缺陷，登记为 D12）**：用 `printf` 往一个以换行结尾的合并正则文件追加新用例名时，正则里会出现一个**内嵌换行**，最后一个分支变成 `"...MinAge\n"` ⇒ 该用例**静默不执行**，而 `ok` **照常打印**（实现者差点把「28 条全绿」当成结论）。

  **计数口径（正式口径，后续所有任务统一）**：「用例数」= **顶层测试函数数**，与 `=== RUN` 的**顶层**名字集合一致；**子用例单独计数**，汇报格式为「N 个顶层用例 + M 个子用例」。断言 2 匹配顶层行时必须带**尾随空格**（`--- PASS: TestName `），以排除 `--- PASS: TestName/sub`。

  因此每个任务的 RED/GREEN 证据必须同时满足三条（语义不得变）：
  1. `go test -v` 的 `=== RUN` **顶层**名字集合 与 正则里的名字集合，各自 `tr -d '[:space:]'` 后 **diff 为空**。**`tr -d '[:space:]'` 是这条断言的关键**：它同时消掉**尾部换行**与**内嵌空白**，因此「拼接时混入换行/空格」这类错误**过不了**它。若只 trim 尾部，就正好放过本次缺陷。
  2. 顶层 `--- PASS: TestName ` 行数 == 顶层名字数（**不排除子用例会误报**；例如 Task 6 本轮有 12 条子用例）。
  3. `SKIP == 0`（除简报明确允许跳过的用例，例如本机缺 `zstd` 时 `requireZstd` 跳过的真封块用例）。

  **底线（是上面三条的推论，但必须显式写下）：执行到的顶层用例总数必须 > 0。** 它单独挡的是退化情形「`-run` 正则本身为空 / 全部为空跑」——此时名字集合与 `=== RUN` 集合都是**空集**、断言 1 的 diff 为空、断言 2 又是 `0 == 0`，可能自洽通过。**计数为 0 的运行不是「通过」，是「无效」。** 统计口径固定为「**全部目标包合并后**」的名字集合与总数（不是逐包分开判断）。

  **判据：`ok` 只说明「跑了的都过了」，不说明「该跑的都跑了」。**（Task 6 报告 §11.3 备有可直接复制的三条断言命令与本轮范例原始输出。）

  **⚠️ 扩充（Task 6 评审判决）：三条断言同样适用于每一次【变异运行】，并新增「无用例执行 = 无效」。**

  **D12 的第二种形态（评审实测）**：变异脚本**只解析 `--- FAIL`**，没有采用上面三条断言。把归档的**逐行 names 文件**（而不是 `|` 连接的合并正则）当 `-run` 用时，`go test` 只输出 `ok … [no tests to run]`（实测：两个目标包都是 `[no tests to run]`、退出码 0），脚本于是一个**真为红**的变异（`m02_退回Go库编码器`）报成「**绿（零覆盖！）**」。⇒ 与 D12 同源：**验证工具本身没有被验证**，看起来完整，但「该跑的一个都没跑」被当成通过。

  1. **三条断言同样适用于每一次变异运行**（不只是任务的 RED/GREEN）：每条变异跑完后都要核 `=== RUN` 顶层名字数 / 顶层 `--- PASS` 行数 / `SKIP`，且必须与**同一套合并正则**的名字集合相符。
  2. **`[no tests to run]` / `no tests to run` 绝不能算绿**，且必须使整轮脚本以**非零退出码**失败。**准确形式是计数式，不是字符串式**——判据只有两句：**`计数不等即为无效；计数为 0 即为无效`**。落到本命令（**全部目标包合并后**统计）：
     - 顶层 `=== RUN   Test[A-Za-z]+$` 的名字集合与总数，对齐 `-run` 正则的名字集合；
     - **计数不等即无效**：顶层执行数 ≠ 正则名字数（或断言 1 的归一化 diff 非空）⇒ 无效；
     - **计数为 0 即无效**：顶层执行数 = 0 ⇒ 无效（此时输出通常正是 `[no tests to run]`）。
     为什么这样就够了：真正要守的不变量是「**正则里的每一个名字都真的跑了**」，它已被断言 1+2 完全覆盖——**全包空跑**（正则拼接坏了）⇒ 计数 0 ≠ 名字数 ⇒ 断言 2 红；**只在一个包命中、另一包空跑**⇒ 计数 < 名字数 ⇒ 断言 2 红。因此**单包 `[no tests to run]` 本身不判无效**（如 Task 5 的 `./internal/tracearchive/`）——只要计数仍相等，它就是合法证据；**不依赖**对「哪个包本该有用例」的人工判断。任何「只 grep `--- FAIL` 就下结论」的脚本都必须先补上这两句判据，否则它的绿是未经验证的。
  3. **变异证据的输出形式**：**逐条自证表**为必需，每行至少含 **名字数 / 实际执行数 / diff 是否为空 / PASS 数 / SKIP / 判定**六列（判定里出现零覆盖时必须如实写「绿（零覆盖！）」，不得写成「通过」）；**完整原始 dump 只需覆盖「评审点名的变异」与「每一条零覆盖条目」**，不必 45 条全贴——体积不构成证据，自证表才是。
- **⚠️ 表驱动 / 共享断言环的遮蔽效应（D15）：不得用「整条用例转红」证明新增条目承重。** 表驱动或共享断言循环**在第一个失败处就停止**，所以一个变异让整条用例转红，只说明「环里有**某个**条目失败了」，**不能**说明你关心的那条新条目失败了——环内更早的条目会**遮蔽**它（本轮实例：inst 环里的 `"red"`、dt 环里的 `""` 先失败，遮蔽了新增的 `"../../green"`、`"2026-09-24/../../x"`）。因此**新增一条用例条目或一条守卫时**，必须用**只含该条目的隔离探针**（或等效的独立用例）给出两截证据：
  1. **守卫在位 ⇒ PASS**（附原始输出）；
  2. **摘掉该守卫 ⇒ FAIL 于该条目**（不是「FAIL 于环里别的条目」）。

  **判据：「整条用例转红」证明的是环里有东西失败了，不是新加的这条承重。** 范例（本例的三侧探针）见缺陷登记 **D15**。
- **⚠️ 两类证据必须分开记：变异（行为）与静态检查（结构不变量）。** 有些要求**不是变异可测的**——本计划自己就写着「压缩比类断言抓不到『退回 Go 库编码器』」，因此那条要求只能靠 `grep -n "zstd.NewWriter" internal/tracearchive/sealer.go` **必须无输出**来证。**这类要求必须有自己的一行证据**（写明命令与真实输出），否则证据表会**暗示测试提供了实际没有的覆盖**。
- **⚠️ 作者编写的产物进版本库；可推导的产物随地方便（D17）。**
  - **变异集合**（「本该删掉哪些守卫」）**不可推导** ⇒ **必须 git-tracked**，路径固定为 `tools/trace-mutate/sets/task-<N>.py`。若放在 gitignored 空间，作者可以**悄悄删掉**一条变异而 `git diff` 看不到——**这与「可缩小的内嵌清单」是同一类失效，只是上移了一层**。
  - **名字清单**（用例名）**可推导** ⇒ 可放证据目录，**前提是工具打印出推导命令与该文件的哈希**。
- **⚠️ 证据必须绑定提交（D16）**：证据工具的输出里必须记录 `git rev-parse HEAD`、`git status --porcelain`（**脏则拒绝运行**）、被读测试文件的哈希、**工具自身哈希**，以及**两份输入（名字清单 + 变异集合）的哈希**。⇒ **「这张表是哪个变异集合在哪棵树上产出的」必须是事实，不是声明。**
- **⚠️ 测试子进程必须封闭环境：至少剔除 `GOFLAGS`。** **已实测**：`GOFLAGS=-exec=<wrapper>` 可把 `--- FAIL` 洗成 `--- PASS`、整轮 `exit 0`，而自证串与诚实运行**逐字同形**——已发生的后果是 4 条真红变异被判绿。（本轮另以最小样例独立复现该机制：`--- FAIL` → `--- PASS`、退出码 1 → 0。）
- **⚠️ `SKIP` 必须统计全部层级（含子测试的缩进行），不能只数顶层。** **已实测**：只数顶层时「32 条顶层 PASS + 一条缩进的 `--- SKIP: TestX/sub`」仍判绿（最小样例：顶层计数 = 0、全层级计数 = 1）；而本包真封块用例依赖 C 版 `zstd`，PATH 里没有就**静默跳过**——只数顶层正好放过它。
- **⚠️ 每份证据必须附「要求 → 变异条目」映射表。** 逐条列出「计划里要求的变异」→「变异集合里的条目名」，使「**悄悄丢掉一个变异**」可审计。**已实测的缺口**：Task 6 的要求清单是封闭 6 项（`task-6-brief.md:961-966`），逐条对账后发现 **`:962`（忽略 `cmd.Run()` 的 error）没有任何对应条目**——`m12` 只改错误包装、`m01` 只换二进制，**都不移除退出码检查**。**没有这张表谁都不会注意到。**
- 本计划**不涉及** PG schema、HTTP 接口、部署配置（属计划 3、4）。**sealer 进程的 main/循环与 systemd unit 也属计划 4**；本计划只产出 `internal/tracearchive` 的原语（含 `ListPendingSeals`）与其单元/组件测试。

---

## 文件结构

> ⚠️ **本表的「规模估计」是任务开工前的预估，不是实测值，勿据此判断规模或进度。**
> 已落地文件的实际行数（截至 Task 6 交付，`3ff5c0d0`）：`writer.go` **609**（预估 ~180）、
> `sealer.go` **576**（预估 ~260）、`ref.go` **98**（预估 ~60）、`index.go` **92**（预估 ~130）、
> `footer.go` **79**（预估 ~90）、`errors.go` **40**（预估 ~60）；
> 测试侧 `tests/tracearchive/writer_test.go` **863**、`sealer_test.go` **1148**。
> 仅 `reader.go`（任务 7）仍为未落地的预估。

| 文件 | 职责 | 规模估计 |
|---|---|---|
| `internal/tracearchive/ref.go` | 定位键 `Ref` 与路径推导 | ~60 行 |
| `internal/tracearchive/index.go` | trailer 索引编解码（差分 varint） | ~130 行 |
| `internal/tracearchive/footer.go` | 32 字节 footer 编解码与校验 | ~90 行 |
| `internal/tracearchive/writer.go` | spool 顺序追加 + 写指针 | ~180 行 |
| `internal/tracearchive/sealer.go` | 目录发现（`ListPendingSeals`）+ 行偏移扫描 + zstd CLI 子进程 + trailer/footer + fsync + rename | ~260 行 |
| `internal/tracearchive/reader.go` | 定位键取正文，含 spool 兜底 | ~190 行 |
| `internal/tracearchive/errors.go` | 错误分类（缺失/损坏/未归档/拒绝覆盖/封块失败） | ~60 行 |
| `internal/tracearchive/testhelpers_test.go` | **A 层**测试辅助（`writeSpool`/`requireZstd`/`fakeZstd`） | ~60 行 |
| `tests/tracearchive/helpers_test.go` | **B 层**测试辅助（同名同实现，故意重复） | ~70 行 |
| — 以下为测试文件，**每份都标注了 A 层（包内）/ B 层（外部包）** — | | |
| `internal/tracearchive/calib_test.go` | **A 层**：U7/U8 标定实测（任务 1 产出，证据留档） | ~140 行 |
| `internal/tracearchive/index_test.go` | **A 层**：索引往返、边界与 golden 字节（须触达 `encodeTrailer`/`decodeTrailer`/`appendVarint`） | ~150 行 |
| `internal/tracearchive/footer_test.go` | **A 层**：footer 往返、篡改检测与 golden 布局（须触达 `footer`/`encodeFooter`/`decodeFooter`） | ~120 行 |
| `internal/tracearchive/ref_internal_test.go` | **A 层**：`parsePartSeq` 严格解析（未导出） | ~50 行 |
| `tests/tracearchive/ref_test.go` | **B 层**：`Ref`/`Paths` 布局与补零（公开 API） | ~50 行 |
| `tests/tracearchive/writer_test.go` | **B 层**：追加、写指针、幂等（公开 API） | ~170 行 |
| `internal/tracearchive/sealer_internal_test.go` | **A 层**：崩溃注入（`failBeforeRename`）与 `parsePartSeq` 交叉验证（未导出） | ~50 行 |
| `tests/tracearchive/sealer_test.go` | **B 层**：封块、子进程失败、发现机制、golden 布局（公开 API） | ~250 行 |
| `internal/tracearchive/reader_internal_test.go` | **A 层**：`maxTrailerBytes` 上界（须触达未导出 const 与 `encodeFooter`/`footer` 构造损坏产物） | ~60 行 |
| `tests/tracearchive/reader_test.go` | **B 层**：点读、兜底、错误分类（公开 API） | ~200 行 |
| `tests/tracearchive/roundtrip_test.go` | **B 层**：端到端逐字节比对（公开 API） | ~180 行 |
| `tests/tracearchive/constraints_test.go` | **B 层**：空间零增量（公开 API） | ~120 行 |
| `cmd/trace-calib/main.go` | 标定实验工具（任务 1） | ~180 行 |

**每个文件单一职责。** `index.go` / `footer.go` 是纯函数，无 IO，最易测；`writer.go` / `sealer.go` / `reader.go` 各自独立成文件，避免合并成一个大 `store.go`。

---

### Task 1：引入 zstd 依赖并完成 U7/U8 标定

**文件：**
- 新建：`cmd/trace-calib/main.go`
- 新建：`internal/tracearchive/calib_test.go`——**A 层（包内，`package tracearchive`）**：必须引用未导出的 `defaultZstdLevel`/`defaultZstdWindowBytes`/`defaultBlockBytes`/`defaultSealInterval` 常量。
- 修改：`go.mod`、`go.sum`、`vendor/`（新增 `github.com/klauspost/compress`）

**接口：**
- 依赖输入：无（本任务是包的第一个任务）
- 对外产出：`const defaultZstdLevel = 19`、`const defaultZstdWindowBytes = 512 << 20`、`const defaultBlockBytes = 1536 << 20`、`const defaultSealInterval = 15 * time.Minute`——写在本任务新建的 `internal/tracearchive/defaults.go` 中，后续任务引用这些常量。
  - **方案 C 下这两个 zstd 常量的语义变了**：`defaultZstdLevel` 直接作为 CLI 的 `-N` 参数；`defaultZstdWindowBytes` 需换算成 CLI 的 `--long=<log2(窗口)>`（512 MiB ⇒ `--long=29`，换算在任务 6 的 `Sealer` 内完成）。**解码侧仍用 Go 库**，故依赖保留。

背景（规格 §5.3.3、§5.3.1.1、§15 U7/U8）：规格的压缩数据全部来自 **C 版 zstd CLI**，而原实现计划假设用 **Go 库**。两处未验证：U7 Go 库产物比 CLI 同级别大多少；U8 Go 库 `WithWindowSize` 的内存开销是否等同 CLI `--long`。**必须先实测钉死，再定参数与编码器。**

**⚠️ 本任务已完成（提交 `d436ab81`、`d847ef57`、`8ab755b8`），实测结论如下——它推翻了「网关内嵌 Go 库」的原假设：**

```
样本 = /datalog/emergency-archive/dt=2026-09-18 前 8 个 part，解压后 2078.7 MB
  U7：Go 库 19 / 512 MiB = 384.0 MB，C 版 CLI -19 --long=29 = 202.6 MB
      ⇒ Go 库是 CLI 的 1.90 倍，验收线是 ≤1.30 倍 ⇒ 【不成立】
  U8：Go 库峰值 RSS 1066.2 MB（≈窗口 × 2.08）
      C 版 CLI 同窗口  570 MB（≈窗口 × 1.08）
  ⇒ 裁决：编码器改用 C 版 zstd CLI（方案 C，规格 §5.3.1.1）；Go 库只留作解码
```

步骤 2~5 保留可复跑的标定证据（`cmd/trace-calib` + `calib_test.go`）；**生产编码路径在任务 6 用 CLI 子进程，不用 `zstd.NewWriter`。**

- [ ] **步骤 1：引入依赖**

```bash
cd /Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write
go get github.com/klauspost/compress@latest
go mod vendor
git add go.mod go.sum vendor/
```

- [ ] **步骤 2：新建参数常量文件**

新建 `internal/tracearchive/defaults.go`。**该文件已在实现中落地**（提交 `d847ef57`、`8ab755b8`）；若已存在则以仓库现状为准，只需确认下列**方案 C 语义**成立：

```go
package tracearchive

import "time"

// 压缩与封块参数终值（规格 §5.3.3）。
//
// 编码器已裁决为**方案 C**：封块移出网关，由独立 sealer 进程调用
// C 版 zstd CLI（-19 --long=29）完成（规格 §5.3.1.1）。
//
// 2026-09-24 标定（样本 = 09-18 前 8 个 part，解压后 2078.7 MB，最难压的真实数据）：
//
//	Go 库  级别 19 窗口 512MB  384.0 MB  5.41x   峰值 RSS 1066.2 MB（≈窗口 × 2.08）
//	CLI    级别 19 --long=29   202.6 MB 10.26x   峰值 RSS  570 MB（≈窗口 × 1.08）
//	⇒ Go 库产物是 CLI 的 1.90 倍，不达验收线（≤1.30 倍）
//
// 对本包的含义：
//   - defaultZstdLevel 直接作为 CLI 的 -N 参数
//   - defaultZstdWindowBytes 换算为 CLI 的 --long=<log2(窗口)> = 29
//     （换算在 Sealer 内完成，见本计划任务 6）
//   - 解码侧不变，仍用 Go 库 klauspost/compress/zstd，故依赖保留
const (
	// defaultZstdLevel zstd 压缩级别，作为 CLI 的 -19。
	// 实测：Go 库级别 19 与 22 产物完全相同（均 384.0 MB），22 只多耗 CPU，故取 19。
	defaultZstdLevel = 19

	// defaultZstdWindowBytes zstd 窗口 512 MiB，换算为 CLI 的 --long=29。
	// 规格 §5.3.2：难压数据上窗口是第一杠杆（128→512 MiB 值 -47.9%）；
	// 但 §5.3.1.1 结论 2 表明该杠杆只有 CLI 的 --long 能兑现
	// （Go 库 WithWindowSize 在同一级别下几乎只花内存）。
	// 读取端 WithDecoderMaxWindow 必须与本值一致（解码器内存 ≈ 窗口）。
	defaultZstdWindowBytes = 512 << 20

	// defaultBlockBytes 未压缩块大小阈值（1.5 GiB）。
	// 块越大，单次点读需要解压的字节越多；1.5 GiB 是「压缩比」与「读取尾部延迟」
	// 之间的暂定折中，最终值由规格 §15 U1 的实测决定。
	defaultBlockBytes = 1536 << 20

	// defaultSealInterval 时间触发封块阈值。
	// 若不按时间封块，低峰天永远达不到大小阈值 ⇒ 正文写了却读不到。
	// 该值是本包唯一凭直觉取的值：调小可缩短「刚写入的正文要等多久才能从产物读」，
	// 代价是块变小、压缩比下降。
	// 方案 C 下它同时是 sealer 的「已停止追加」判定窗口（规格 §7.2）。
	defaultSealInterval = 15 * time.Minute
)
```

> **不新增 `zstd` 路径常量**：`SealerOptions.ZstdBinary` 为空时在 `Sealer` 内回退到字面量 `"zstd"`（任务 6），避免再改本文件。

- [ ] **步骤 3：编写标定工具**

新建 `cmd/trace-calib/main.go`：

```go
// trace-calib 用生产真实样本标定 Go zstd 库与 C 版 CLI 的差异。
//
// 用法:
//
//	trace-calib -in sample.jsonl -window 512 -iter 1
//
// 输出: 每个级别的产物大小与峰值 RSS，用于钉死规格 §15 的 U7/U8。
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/klauspost/compress/zstd"
)

func main() {
	in := flag.String("in", "", "输入样本（未压缩 JSONL）")
	windowMB := flag.Int("window", 512, "窗口 MiB")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "必须指定 -in")
		os.Exit(2)
	}

	src, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读样本失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("样本: %s  未压缩 %.1f MB\n\n", *in, float64(len(src))/1048576)

	window := *windowMB << 20
	// U7: Go 库在各窗口下的产物大小
	// U8: Go 库在各窗口下的峰值 RSS
	for _, lvl := range []int{9, 19, 22} {
		for _, w := range []int{128 << 20, 256 << 20, window} {
			opts := []zstd.EOption{
				zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(lvl)),
				zstd.WithWindowSize(w),
				zstd.WithEncoderConcurrency(1),
			}
			enc, err := zstd.NewWriter(nil, opts...)
			if err != nil {
				fmt.Fprintf(os.Stderr, "级别 %d 窗口 %dMB 建编码器失败: %v\n", lvl, w>>20, err)
				os.Exit(1)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			t0 := time.Now()
			out := enc.EncodeAll(src, make([]byte, 0, len(src)/4))
			elapsed := time.Since(t0)
			enc.Close()

			runtime.ReadMemStats(&after)
			peak := after.TotalAlloc - before.TotalAlloc // 仅近似，真实峰值见下方说明
			fmt.Printf("级别 %-3d 窗口 %4dMB  产物 %9.1f MB  比值 %6.2fx  耗时 %6.1fs  Go堆增量 %6.1f MB\n",
				lvl, w>>20, float64(len(out))/1048576,
				float64(len(src))/float64(len(out)), elapsed.Seconds(),
				float64(peak)/1048576)
		}
	}
	fmt.Println("\n注：Go 库峰值 RSS 需用 /proc/self/status VmHWM 采样，见 internal/tracearchive/calib_test.go")
}
```

- [ ] **步骤 4：运行标定，记录 U7/U8 结论**

在**生产机**上用 09-18 的真实样本（embedding 为主，最难压）跑：

```bash
# 生产机上重建样本（2 GB）
ssh root@192.0.2.10 'zstd -d -c /datalog/emergency-archive/dt=2026-09-18/part-0000[0-7].jsonl.zst > /tmp/calib-sample.jsonl'

# 本地跑 Go 库标定（把样本取回后）
go run ./cmd/trace-calib -in /tmp/calib-sample.jsonl -window 512
```

**预期与判断规则（含 2026-09-24 的实测结果）：**
- **U7**：若 Go 库 `EncoderLevelFromZstd(19)` 在 512 MiB 窗口下的产物 **≤ CLI `zstd -19 --long=29` 产物 × 1.30**，则 Go 库可选；否则必须改用 CLI。
  **实测结果：384.0 MB vs 202.6 MB = 1.90 倍 > 1.30 ⇒ U7 不成立**，调 `EncoderOption` 也救不回来（`WithWindowSize` 在 Go 库上几乎只花内存，见结论 2）。**裁决：编码器改用 C 版 zstd CLI（方案 C，规格 §5.3.1.1）；本步骤保留为证据，不再据此调 `EncoderOption`。**
- **U8**：若 Go 库在 512 MiB 窗口下的峰值 RSS **≤ 1200 MB**，则窗口 512 MiB 在 Go 库上也可行；否则下调至 256 MiB。
  **实测结果：Go 库 1066.2 MB（≈窗口 × 2.08，未超 1200 MB）；C 版 CLI 同窗口 570 MB（≈窗口 × 1.08）。**
  **裁决：方案 C 采用 CLI，`defaultZstdWindowBytes = 512 << 20` 保持不动；原来的「网关 2 GiB 限额」约束随之消失（编码器不在网关内）。**

- [ ] **步骤 5：把标定结论写进 Go 单元测试（可回归）**

> **方案 C 下的定位**：下面的 Go 库回归测试是**证据留档**（记录「Go 库为什么不达标」），**不是生产断言**——生产编码路径在任务 6 用 CLI 子进程。**生产侧真正需要断言的是任务 6 的那几条**：CLI 参数为 `-19 --long=29`、trailer/footer 由 Go 侧追加、子进程非零退出即失败。因此本步骤的测试可继续保留，但**不得**把它们当成「编码器已确认」的证据。

新建 `internal/tracearchive/calib_test.go`：

```go
package tracearchive

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestGoEncoderPeakRSS 是 U8 的常驻回归测试。
// 它断言：在本包选定的窗口参数下，Go zstd 编码器的峰值堆占用不超过预算。
//
// 该测试的意义：规格 §8.4 的 C5 约束（点读并发上限）与网关 2 GiB 内存限额
// 都直接依赖这个数字。若换 zstd 版本或改窗口，此测试会立刻转红。
func TestGoEncoderPeakRSS(t *testing.T) {
	if testing.Short() {
		t.Skip("标定测试，-short 下跳过")
	}
	const budget = 1200 << 20 // 1.2 GB，规格 §5.3.3 的结论边界

	// 构造可压缩样本，大小略大于窗口以确保窗口被填满
	sample := bytes.Repeat([]byte(`{"request_id":"rid-0000","body":"`), 1)
	sample = append(sample, bytes.Repeat([]byte("abcdefgh"), (defaultZstdWindowBytes+64<<20)/8)...)
	sample = append(sample, []byte(`"}`)...)

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = enc.EncodeAll(sample, nil)
	runtime.ReadMemStats(&after)

	// HeapInuse 差值近似编码器常驻占用
	used := int64(after.HeapInuse) - int64(before.HeapInuse)
	if used > budget {
		t.Fatalf("编码器堆占用 %d MB 超出预算 %d MB（U8 结论失效，需下调 defaultZstdWindowBytes）",
			used>>20, budget>>20)
	}
	t.Logf("编码器堆占用 %d MB（预算 %d MB）", used>>20, budget>>20)
}

// TestGoEncoderRatioRegression 是 U7 的常驻回归测试。
// 它断言：对本包选定的参数，Go 库产物不超过给定上界，防止换版本后压缩比悄悄劣化。
func TestGoEncoderRatioRegression(t *testing.T) {
	// 用高重复度文本，保证压缩比稳定可断言
	line := []byte(`{"request_id":"` + strings.Repeat("a", 32) + `","ts":"2026-09-24T00:00:00+08:00","body":"` + strings.Repeat("x", 4096) + `"}` + "\n")
	var raw bytes.Buffer
	for i := 0; i < 4096; i++ {
		raw.Write(line)
	}

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	out := enc.EncodeAll(raw.Bytes(), nil)
	ratio := float64(raw.Len()) / float64(len(out))

	// 该样本重复度极高，Go 库实测应远超 50x；上界取保守值
	const minRatio = 50.0
	if ratio < minRatio {
		t.Fatalf("压缩比 %.1fx 低于下界 %.1fx（Go 库或参数发生变化，见规格 U7）", ratio, minRatio)
	}
	t.Logf("压缩比 %.1fx（下界 %.1fx），%d 字节 → %d 字节", ratio, minRatio, raw.Len(), len(out))
}
```

- [ ] **步骤 6：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run 'TestGoEncoderPeakRSS|TestGoEncoderRatioRegression' -v`
预期：PASS，日志中打印实际堆占用与压缩比。

> **为什么把 `-run` 写全**：Go 的 `-run` 是**子串匹配**。写 `-run 'TestGoEncoder'` 虽然这次恰好能选全两个用例，但一旦新增/改名就会静默漏选，而漏选的 GREEN 等于没有证据。**本计划所有 RED/GREEN 命令一律显式列全测试名。**

- [ ] **步骤 7：跑通全仓库构建**

运行：`go build ./...`
预期：无新增错误。**注意**：`test/bedrock_model_id_check.go:98:23: undefined: bedrock.NewListFoundationModelsPaginator` 是仓库既有问题（与本次改动无关），如仅此一条则视为通过。

- [ ] **步骤 8：提交**

```bash
git add go.mod go.sum vendor/ cmd/trace-calib/ internal/tracearchive/
git commit -m "feat(tracearchive): 引入 zstd 依赖并完成 U7/U8 编码器标定

- 新增 internal/tracearchive/defaults.go：级别 19 / 窗口 512MiB / 块 1.5GiB / 封块 15min
  （方案 C 下这些常量分别映射为 CLI 的 -19、--long=29）
- 新增 cmd/trace-calib 标定工具与 calib_test.go 常驻回归测试（证据留档）
- 钉死规格 §15 的 U7（Go 库 vs CLI 产物差异）与 U8（WithWindowSize 内存开销）：
  Go 库是 CLI 的 1.90 倍、峰值 RSS 是 2.08 倍窗口 ⇒ 编码器裁决为 C 版 zstd CLI"
```

---

### Task 2：内嵌索引 trailer 的编解码

**文件：**
- 新建：`internal/tracearchive/index.go`
- 新建：`internal/tracearchive/index_test.go`——**A 层（包内，`package tracearchive`）**：必须触达未导出的 `encodeTrailer`/`decodeTrailer`/`appendVarint`，且 `decodeTrailer` 刻意不导出（外部包无法访问）。

> **这是「A 层例外」，不是遗漏**：`encodeTrailer`/`decodeTrailer` 是内部格式原语，规格与计划都要求它们**不导出**（不构成 `internal/tracearchive` 的公共 API）。因此这份测试只能放包内——判据见「全局约束」的测试摆放条款。已交付的 Task 2/3 代码**不需要返工**。

**接口：**
- 依赖输入：无
- 对外产出：
  - `func encodeTrailer(offsets []int64) []byte`——把「行号→未压缩偏移」表编码为差分 varint
  - `func decodeTrailer(b []byte, n int) ([]int64, error)`——解码，`n` 为期望条目数；**任何长度/单调性/负值/尾部多余字节/`n` 越界的情况都返回 `ErrTrailerCorrupt`**
- **契约要点（承重）**：`n` 来自产物 footer 的 `row_count`（任务 7 传入），而 `row_count` **没有校验和**（任务 3 的 `decodeFooter` 只校验 magic/版本/长度/负值）。因此 `decodeTrailer` **必须自己拦住天文数字的 `n`**：`n > len(b)` 时立即返回 `ErrTrailerCorrupt`，**绝不能让 `n` 进入 `make`**——否则一个仅 `row_count` 损坏的 footer 就会触发 `makeslice` panic 或不可恢复的 OOM（规格 §8.1 的意图正是「损坏必须报成损坏」）。

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/index_test.go`：

```go
package tracearchive

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestTrailerRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		offsets []int64
	}{
		{"单行", []int64{0}},
		{"两行", []int64{0, 100}},
		{"等长多行", []int64{0, 10, 20, 30, 40}},
		{"长度悬殊", []int64{0, 5, 5000, 5001, 900000}},
		{"零长度行", []int64{0, 0, 0}},
		{"大偏移", []int64{0, 1 << 40, 1<<40 + 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := encodeTrailer(c.offsets)
			got, err := decodeTrailer(b, len(c.offsets))
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}
			if !reflect.DeepEqual(got, c.offsets) {
				t.Fatalf("往返不一致:\n want %v\n got  %v", c.offsets, got)
			}
		})
	}
}

// TestTrailerGoldenBytes 把 trailer 的字节布局钉死。
//
// 为什么必须有它：往返测试只证明「编码器与解码器自洽」，一个对称的 bug
// （例如把差分算成「与前一条的差值 +1」且解码端同样处理）会全绿通过。
// golden 用固定输入断言逐字节输出，是唯一能挡住这类 bug 的测试。
//
// offsets{0,3,8} 的编码 = 绝对偏移 0、差分 3、差分 5，各 1 字节 varint。
func TestTrailerGoldenBytes(t *testing.T) {
	got := encodeTrailer([]int64{0, 3, 8})
	want := []byte{0x00, 0x03, 0x05}
	if !bytes.Equal(got, want) {
		t.Fatalf("trailer 字节不符:\n want % x\n got  % x", want, got)
	}

	// 多字节 varint 的边界：300 的 LEB128 = 0xAC 0x02
	got = encodeTrailer([]int64{0, 300})
	want = []byte{0x00, 0xAC, 0x02}
	if !bytes.Equal(got, want) {
		t.Fatalf("多字节 varint 字节不符:\n want % x\n got  % x", want, got)
	}
}

func TestDecodeTrailerRejectsWrongCount(t *testing.T) {
	b := encodeTrailer([]int64{0, 10, 20})
	if _, err := decodeTrailer(b, 4); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("条目数不符应返回 ErrTrailerCorrupt，实际 %v", err)
	}
}

func TestDecodeTrailerRejectsNonMonotonic(t *testing.T) {
	// 手工构造一个偏移递减的 trailer：差分编码下即为负增量
	b := []byte{}
	b = appendVarint(b, 0)
	b = appendVarint(b, ^uint64(5-1)) // 负增量（补码表示）
	if _, err := decodeTrailer(b, 2); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("偏移非单调应返回 ErrTrailerCorrupt，实际 %v", err)
	}
}

// TestDecodeTrailerRejectsNegativeOffset 覆盖「首条偏移为负」这条守卫：
// 首条是绝对偏移，v >= 2^63 时 int64(v) 为负；差分相加也可能回绕成负值。
func TestDecodeTrailerRejectsNegativeOffset(t *testing.T) {
	b := appendVarint(nil, 1<<63) // 1<<63 作为 uint64 解出后 int64 为负
	if _, err := decodeTrailer(b, 1); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("首条偏移为负应返回 ErrTrailerCorrupt，实际 %v", err)
	}
}

// TestDecodeTrailerRejectsHugeCount 覆盖 `n > len(b)` 前置守卫。
//
// 真实调用方（任务 7 的 Reader）传的是 footer 里的 int32 row_count；
// 该字段无校验和，损坏时可达 2^31-1。若守卫缺失，make([]int64, 0, n)
// 会立刻申请约 16 GiB —— 进程级 OOM/panic，而不是一个错误。
func TestDecodeTrailerRejectsHugeCount(t *testing.T) {
	b := encodeTrailer([]int64{0, 10})
	if n := 1<<31 - 1; n <= len(b) {
		t.Fatalf("测试前提不成立：n=%d 应远大于 len(b)=%d", n, len(b))
	}
	if _, err := decodeTrailer(b, 1<<31-1); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("n 远超 len(b) 应返回 ErrTrailerCorrupt（不得进入 make），实际 %v", err)
	}
}

func TestDecodeTrailerRejectsTruncated(t *testing.T) {
	// 半截的多字节 varint：这条走「varint 耗尽」路径
	// （与 TrailingBytes 的 pos != len(b)、HugeCount 的 n > len(b) 分别覆盖三道守卫）
	b := encodeTrailer([]int64{0, 1000}) // 0x00 | 0xE8 0x07，共 3 字节
	if _, err := decodeTrailer(b[:2], 2); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("半截 varint 应返回 ErrTrailerCorrupt，实际 %v", err)
	}
}

// TestDecodeTrailerRejectsTrailingBytes 覆盖「尾部有多余字节」守卫（pos != len(b)）。
//
// 注意它与截断用例走的是不同分支：截断是 varint 解不出来，
// 这里是 n 条都解出来了但后面还剩字节。
func TestDecodeTrailerRejectsTrailingBytes(t *testing.T) {
	b := append(encodeTrailer([]int64{0, 10}), 0xFF)
	if _, err := decodeTrailer(b, 2); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("尾部多余字节应返回 ErrTrailerCorrupt，实际 %v", err)
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run 'TestTrailer|TestDecodeTrailer' -v`
预期：编译失败，提示 `undefined: encodeTrailer`、`undefined: appendVarint`、`undefined: ErrTrailerCorrupt`。

> `-run` 是子串匹配：`-run TestTrailer` 选不中三个 `TestDecodeTrailer*`，会让「拒绝路径全坏」的实现依然绿。**必须用上表的组合模式。**

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/index.go`：

```go
package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// trailer 是产物文件尾部的「行号 → 未压缩偏移」索引表。
//
// 编码方式：第一条存绝对偏移，其后每条存与上一条的差值（差分），
// 全部用 unsigned LEB128 varint 编码。理由：同一块内各行长度相近，
// 差分的数值分布集中在小整数区间，varint 比定长 8 字节省 4~6 倍。
var (
	// ErrTrailerCorrupt 表示 trailer 结构损坏（截断、条目数不符、偏移非单调、
	// 偏移为负、尾部多余字节、或调用方给的条目数越界）。
	// 读取端依赖它区分「产物损坏」与「数据不存在」（规格 §8.1）。
	ErrTrailerCorrupt = errors.New("tracearchive: trailer 损坏")
)

// appendVarint 追加一个 LEB128 varint。
// 负差分会以二补码形式编码，decodeTrailer 会据此识别非单调。
func appendVarint(dst []byte, v uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return append(dst, buf[:n]...)
}

// encodeTrailer 把「行号 → 未压缩偏移」表编码为差分 varint。
// offsets 必须按行号升序且单调不减；空表返回空切片。
func encodeTrailer(offsets []int64) []byte {
	if len(offsets) == 0 {
		return nil
	}
	out := make([]byte, 0, len(offsets)*3)
	out = appendVarint(out, uint64(offsets[0]))
	for i := 1; i < len(offsets); i++ {
		delta := offsets[i] - offsets[i-1]
		// 显式转为 uint64 以便负差分以二补码编码，decodeTrailer 依据高位判定
		out = appendVarint(out, uint64(delta))
	}
	return out
}

// decodeTrailer 解码 trailer，期望恰好 n 条。
// 任何长度不符、非单调、偏移为负、n 越界、或尾部有多余字节的情况都返回 ErrTrailerCorrupt。
func decodeTrailer(b []byte, n int) ([]int64, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: 非法条目数 %d", ErrTrailerCorrupt, n)
	}
	if n == 0 {
		if len(b) != 0 {
			return nil, fmt.Errorf("%w: 期望 0 条但剩余 %d 字节", ErrTrailerCorrupt, len(b))
		}
		return nil, nil
	}
	// n 来自产物 footer 的 row_count（任务 7 传入），该字段没有校验和：
	// 一个仅 row_count 损坏的 footer 会给出 2^31-1 这类天文数字。
	// 每条至少占 1 字节，故 n > len(b) 必为损坏。
	// 【必须在 make 之前拦截】直接用于定容量会造成进程级 OOM/panic，
	// 而不是返回一个错误 —— 那与「损坏必须报成损坏」的意图正好相反。
	if n > len(b) {
		return nil, fmt.Errorf("%w: 期望 %d 条但仅 %d 字节", ErrTrailerCorrupt, n, len(b))
	}
	// 条目数上界（读侧内存预算的另一半，见 reader.go 的 maxTrailerEntries）。
	// 【修复轮 F1 补入】n > len(b) 只挡「结构上不可能」，挡不住「结构上可能但超出预算」：
	// len(b) 被 maxTrailerBytes 限制在 64 MiB，而最坏情形（每个字节都是一个 1 字节
	// varint）给出 n = len(b) = 64 Mi 条 ⇒ make 分配 8n = 512 MiB。实测该输入会
	// 【成功解码】（err=nil，不是报错），故必须在此硬拦，且同样在 make 之前。
	// 超限一律 ErrTrailerCorrupt，【绝不截断】到上界条数 —— 截断会让「损坏」伪装成
	// 一次正常读取（D30；实测数字见 r11-report.md §7.9）。
	if n > maxTrailerEntries {
		return nil, fmt.Errorf("%w: 期望 %d 条超过条目数上界 %d", ErrTrailerCorrupt, n, maxTrailerEntries)
	}

	out := make([]int64, 0, n)
	pos := 0
	for i := 0; i < n; i++ {
		v, adv := binary.Uvarint(b[pos:])
		if adv <= 0 {
			return nil, fmt.Errorf("%w: 第 %d 条 varint 解不出（剩余 %d 字节）", ErrTrailerCorrupt, i, len(b)-pos)
		}
		pos += adv

		var off int64
		if i == 0 {
			off = int64(v) // 第一条存绝对偏移
		} else {
			delta := int64(v)
			if delta < 0 {
				return nil, fmt.Errorf("%w: 第 %d 条偏移非单调（差分 %d）", ErrTrailerCorrupt, i, delta)
			}
			off = out[i-1] + delta // 可能回绕，由下面的负值检查兜住
		}
		// 负偏移必须显式拒绝：i==0 时 v >= 2^63 会解成负数；
		// 差分相加也可能回绕成负值而不被 delta<0 检查发现。
		if off < 0 {
			return nil, fmt.Errorf("%w: 第 %d 条偏移为负（%d）", ErrTrailerCorrupt, i, off)
		}
		out = append(out, off)
	}
	if pos != len(b) {
		return nil, fmt.Errorf("%w: 解码后有 %d 字节多余", ErrTrailerCorrupt, len(b)-pos)
	}
	return out, nil
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run 'TestTrailer|TestDecodeTrailer' -v`
预期：PASS（含 `TestTrailerGoldenBytes`、`TestDecodeTrailerRejectsWrongCount` / `NonMonotonic` / `NegativeOffset` / `HugeCount` / `Truncated` / `TrailingBytes`）。

**变异自检（必须逐条做，这是「每道防线都要有测试」的判据）：**
- 删掉 `n > len(b)` 守卫 → `TestDecodeTrailerRejectsHugeCount` 必须转红（且可能 panic/OOM）；
- **删掉 `n > maxTrailerEntries` 守卫（修复轮 F1 补入的条目数上界）→ `TestReaderRejectsTrailerEntryCountOverBound` 必须转红**（该用例在 A 层 `internal/tracearchive/reader_internal_test.go`：真封块帧 + 条目数 = 长度 = max+1 的自洽 trailer/footer；实测 cut 守卫 ⇒ `err=<nil> line="{\"a\":1}"` **静默成功读出**）。**这是 task-2 的 `m06` 变异条目**；
- 删掉 `off < 0` 检查 → `TestDecodeTrailerRejectsNegativeOffset` 必须转红；
- 删掉 `pos != len(b)` 检查 → `TestDecodeTrailerRejectsTrailingBytes` 必须转红；
- 把任意一处 `%w` 改成 `%v` → 对应用例（`errors.Is`）必须转红。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/index.go internal/tracearchive/index_test.go
git commit -m "feat(tracearchive): 实现内嵌索引 trailer 的差分 varint 编解码

索引与产物内容出自同一次封块操作，物理上不可能不同步——
这从根上消灭了 v3 方案中外部 index.jsonl 丢失导致点读全部 miss 的缺陷。
含 n > len(b) 前置守卫（footer 的 row_count 无校验和，越界会 OOM）
与负偏移检查；错误一律用 %w 包装 ErrTrailerCorrupt 以便 errors.Is 分类。"
```

---

### Task 3：32 字节 footer 的编解码与校验

**文件：**
- 新建：`internal/tracearchive/footer.go`
- 新建：`internal/tracearchive/footer_test.go`——**A 层（包内，`package tracearchive`）**：必须触达未导出的 `footer` 结构体、`encodeFooter`/`decodeFooter`。**`FooterSize` 是导出的**，但 golden 布局测试要构造 `footer{...}` 字面量并逐字节比对，外部包做不到。

> **同样是「A 层例外」**：`footer` 与两个编解码函数刻意不导出；`FooterSize` 导出是给任务 7 的读取端用的。已交付代码**不需要返工**。

**接口：**
- 依赖输入：无
- 对外产出：
  - `const FooterSize = 32`
  - `type footer struct { TrailerOffset int64; TrailerLen int32; RowCount int32; FrameSHA [8]byte }`
  - `func encodeFooter(f footer) []byte`
  - `func decodeFooter(b []byte) (footer, error)`——长度非 32、magic 不符、版本未知、负 `TrailerOffset`/`TrailerLen`/`RowCount` 时返回 `ErrFooterCorrupt`

> **`RowCount` 是契约的一部分，不是可选项**：它编在 `[18:22)`，读取端（任务 7）据此向 `decodeTrailer` 传入期望条目数。漏了它，任务 6 写产物时不会写 `row_count`、任务 7 也就拿不到那个 `n` ——而那个 `n` 正是「被截断的 trailer 能被识别为损坏、而不是静默少读几行」的关键。

**布局**（规格 §5.2）：
```
magic(4)="TBA4" | format_version(2) | trailer_offset(8) | trailer_len(4)
| row_count(4) | frame_sha256[:8](8) | reserved(2)
= 32 字节
```

**`row_count` 的作用（不可省）**：读取端据此向 `decodeTrailer` 传入期望条目数。缺少它时，被截断的 trailer 会被静默解码为更少的条目而无法识别，读取端也就无法判断 `ref.Row` 是否越界。

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/footer_test.go`：

```go
package tracearchive

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestFooterRoundTrip(t *testing.T) {
	in := footer{
		TrailerOffset: 1234567,
		TrailerLen:    4096,
		RowCount:      2718,
		FrameSHA:      [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	}
	b := encodeFooter(in)
	if len(b) != FooterSize {
		t.Fatalf("footer 长度应为 %d，实际 %d", FooterSize, len(b))
	}
	got, err := decodeFooter(b)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got != in {
		t.Fatalf("往返不一致:\n want %+v\n got  %+v", in, got)
	}
}

// TestFooterGoldenLayout 把 32 字节布局逐字节钉死。
//
// 为什么必须有它：往返测试只证明编解码自洽，一个「两个字段偏移都写错但读写一致」
// 的对称 bug 会全绿通过 —— 而那种产物在别的实现/别的语言里读不出来。
// golden 断言的是格式契约本身。
func TestFooterGoldenLayout(t *testing.T) {
	f := footer{
		TrailerOffset: 0x0102030405060708,
		TrailerLen:    0x0A0B0C0D,
		RowCount:      0x0E0F1011,
		FrameSHA:      [8]byte{0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19},
	}
	got := encodeFooter(f)
	want := []byte{
		0x54, 0x42, 0x41, 0x34, // [0:4)   magic "TBA4"
		0x00, 0x01, // [4:6)   format_version = 1
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, // [6:14)  trailer_offset (BigEndian)
		0x0A, 0x0B, 0x0C, 0x0D, // [14:18) trailer_len
		0x0E, 0x0F, 0x10, 0x11, // [18:22) row_count
		0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, // [22:30) frame_sha256[:8]
		0x00, 0x00, // [30:32) reserved
	}
	if len(want) != FooterSize {
		t.Fatalf("golden 自身长度应为 %d，实际 %d", FooterSize, len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("footer 逐字节不符:\n want % x\n got  % x", want, got)
	}

	// 反向：逐字节构造的 golden 必须能解回原值（防止「只有单向正确」）
	back, err := decodeFooter(want)
	if err != nil {
		t.Fatalf("golden 应可解: %v", err)
	}
	if back != f {
		t.Fatalf("golden 解回不符:\n want %+v\n got  %+v", f, back)
	}

	// 显式钉住各字段的大端语义与保留位
	if binary.BigEndian.Uint64(want[6:14]) != f.TrailerOffset {
		t.Fatal("trailer_offset 不是大端")
	}
	if want[30] != 0 || want[31] != 0 {
		t.Fatal("保留位必须为 0")
	}
}

func TestFooterRowCountRoundTrip(t *testing.T) {
	// row_count 是读取端判断行号越界与 trailer 截断的唯一依据，必须无损往返
	for _, n := range []int32{0, 1, 255, 256, 65535, 1 << 20} {
		b := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: n})
		got, err := decodeFooter(b)
		if err != nil {
			t.Fatalf("row_count=%d 解码失败: %v", n, err)
		}
		if got.RowCount != n {
			t.Fatalf("row_count 往返不一致: want %d, got %d", n, got.RowCount)
		}
	}
}

func TestDecodeFooterRejectsNegativeRowCount(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: -1})
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("负 row_count 应返回 ErrFooterCorrupt，实际 %v", err)
	}
}

// TestDecodeFooterRejectsNegativeOffsetOrLen 覆盖「负 TrailerOffset / 负 TrailerLen」守卫。
// 这两个字段是 int64/int32 → uint64/uint32 的位转换，编码端不校验，
// 因此解码端的负值检查是唯一防线（缺了它，读取端会拿负数去 ReadAt）。
func TestDecodeFooterRejectsNegativeOffsetOrLen(t *testing.T) {
	if _, err := decodeFooter(encodeFooter(footer{TrailerOffset: -1, TrailerLen: 4, RowCount: 1})); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("负 trailer_offset 应返回 ErrFooterCorrupt，实际 %v", err)
	}
	if _, err := decodeFooter(encodeFooter(footer{TrailerOffset: 0, TrailerLen: -1, RowCount: 1})); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("负 trailer_len 应返回 ErrFooterCorrupt，实际 %v", err)
	}
}

func TestDecodeFooterRejectsWrongLength(t *testing.T) {
	if _, err := decodeFooter(make([]byte, FooterSize-1)); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("长度不足应返回 ErrFooterCorrupt，实际 %v", err)
	}
	if _, err := decodeFooter(make([]byte, FooterSize+1)); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("长度超出应返回 ErrFooterCorrupt，实际 %v", err)
	}
}

func TestDecodeFooterRejectsBadMagic(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[0] ^= 0xFF // 篡改 magic
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("magic 被篡改应返回 ErrFooterCorrupt，实际 %v", err)
	}
}

func TestDecodeFooterRejectsUnknownVersion(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[4] = 99 // 篡改 format_version
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("未知 format_version 应返回 ErrFooterCorrupt，实际 %v", err)
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -v`
预期：编译失败，提示 `undefined: footer`、`undefined: FooterSize`、`undefined: ErrFooterCorrupt`。

> `-run TestFooter` 只能选中 2/6 个用例（`TestFooterRoundTrip`、`TestFooterRowCountRoundTrip`），
> 会漏掉**全部四个** `TestDecodeFooterRejects*` —— 那样「拒绝路径全坏」也会绿。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/footer.go`：

```go
package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// FooterSize 是产物文件末尾定长 footer 的字节数。
	// 读取端只需 pread 末尾 32 字节即可得知索引位置，无需解压任何数据。
	FooterSize = 32

	// footerMagic 标识本格式。冷层合并若引入扩展索引键，应提升 formatVersion。
	footerMagic = "TBA4"

	// formatVersion 当前格式版本。冷层超级文件（规格 §5.5）将提升此值。
	formatVersion uint16 = 1
)

// ErrFooterCorrupt 表示 footer 缺失、长度不符、magic 不符或版本未知。
var ErrFooterCorrupt = errors.New("tracearchive: footer 损坏")

// footer 是产物文件末尾的定长元信息。
//
// 字段布局（共 32 字节）：
//
//	[0:4)   magic
//	[4:6)   format_version
//	[6:14)  trailer_offset
//	[14:18) trailer_len
//	[18:22) row_count
//	[22:30) frame_sha256[:8]
//	[30:32) reserved
type footer struct {
	TrailerOffset int64   // trailer 相对文件起始的偏移
	TrailerLen    int32   // trailer 字节长度
	RowCount      int32   // 块内行数。读取端据此校验 trailer 完整性并判断行号越界
	FrameSHA      [8]byte // zstd 帧字节（不含 trailer/footer）的 sha256 前 8 字节
}

// encodeFooter 编码为定长 32 字节。
func encodeFooter(f footer) []byte {
	b := make([]byte, FooterSize)
	copy(b[0:4], footerMagic)
	binary.BigEndian.PutUint16(b[4:6], formatVersion)
	binary.BigEndian.PutUint64(b[6:14], uint64(f.TrailerOffset))
	binary.BigEndian.PutUint32(b[14:18], uint32(f.TrailerLen))
	binary.BigEndian.PutUint32(b[18:22], uint32(f.RowCount))
	copy(b[22:30], f.FrameSHA[:])
	// b[30:32] 保留，恒为 0
	return b
}

// decodeFooter 解析并校验定长 footer。
func decodeFooter(b []byte) (footer, error) {
	var f footer
	if len(b) != FooterSize {
		return f, fmt.Errorf("%w: 长度应为 %d，实际 %d", ErrFooterCorrupt, FooterSize, len(b))
	}
	if string(b[0:4]) != footerMagic {
		return f, fmt.Errorf("%w: magic 不符（期望 %q）", ErrFooterCorrupt, footerMagic)
	}
	if v := binary.BigEndian.Uint16(b[4:6]); v != formatVersion {
		return f, fmt.Errorf("%w: 未知 format_version %d", ErrFooterCorrupt, v)
	}
	f.TrailerOffset = int64(binary.BigEndian.Uint64(b[6:14]))
	f.TrailerLen = int32(binary.BigEndian.Uint32(b[14:18]))
	f.RowCount = int32(binary.BigEndian.Uint32(b[18:22]))
	copy(f.FrameSHA[:], b[22:30])

	if f.TrailerOffset < 0 || f.TrailerLen < 0 {
		return f, fmt.Errorf("%w: 负偏移或负长度（off=%d len=%d）", ErrFooterCorrupt, f.TrailerOffset, f.TrailerLen)
	}
	if f.RowCount < 0 {
		return f, fmt.Errorf("%w: 负 row_count %d", ErrFooterCorrupt, f.RowCount)
	}
	return f, nil
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -v`
预期：PASS（含 golden 布局、row_count 往返、负 row_count/负 offset/负 len 拒绝、magic/版本/长度三类拒绝）。

**变异自检**：删掉 `decodeFooter` 里的负值检查 → `TestDecodeFooterRejectsNegativeOffsetOrLen` 必须转红；把五处 `%w` 任一改成 `%v` → 对应用例必须转红；调换 `[14:18)` 与 `[18:22)` 的写入/读取顺序 → `TestFooterGoldenLayout` 必须转红（往返测试不会）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/footer.go internal/tracearchive/footer_test.go
git commit -m "feat(tracearchive): 实现 32 字节定长 footer 的编解码与校验

读取端只需 pread 末尾 32 字节即可定位索引，不必解压任何数据。
含 magic 与 format_version 校验，为冷层扩展预留锚点。"
```

---

### Task 4：定位键与目录布局

**文件：**
- 新建：`internal/tracearchive/ref.go`
- 新建：`internal/tracearchive/errors.go`
- 新建：`tests/tracearchive/ref_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**：`Ref`/`Paths`/`NewPaths` 均导出，全部经公开 API 断言。
- 新建：`internal/tracearchive/ref_internal_test.go`——**A 层（`package tracearchive`）**：只因为有 `parsePartSeq` 这个**未导出**的严格解析函数。**若实现让 `parsePartSeq` 导出**（不推荐：它是实现细节），这份文件应整体并入上面的 B 层文件。

**接口：**
- 依赖输入：无
- 对外产出：
  - `type Ref struct { DT string; Inst string; Part int; Row int }`
  - `func (p Paths) PartFile(ref Ref) string`、`func (p Paths) SpoolFile(ref Ref) string`、`func (p Paths) SpoolDir(dt, inst string) string`、`func (p Paths) PartDir(dt, inst string) string`
  - `func NewPaths(root string) Paths`
  - 读取侧错误变量：`ErrPartNotFound`、`ErrBodyMissing`、`ErrNotArchived`
  - **封块侧错误变量（方案 C 新增，任务 6 用）**：`ErrPartExists`（产物已存在，拒绝覆盖）、`ErrRowCountMismatch`（spool 行数与调用方记录不符）、`ErrSealFailed`（封块失败，含 zstd 子进程非零退出）
  - 说明：这三个封块侧哨兵让 sealer 的调用方能区分「重复封块（幂等无害，降噪）」与「压缩失败（需告警/重试）」——这正是任务 6 测试用 `errors.Is` 断言的依据

**布局**（规格 §5.1）：
```
<root>/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl.zst
<root>/spool/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl
```

- [ ] **步骤 1：编写失败的测试**

新建两个测试文件（A/B 分层，见上面的文件清单）：

新建 `tests/tracearchive/ref_test.go`——**B 层，`package tracearchive_test`，只经公开 API**。这份文件同时是本次修订的**转换范例**（后面各任务的 B 层文件都按同一方式书写）：

```go
package tracearchive_test

import (
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

func TestPathsLayout(t *testing.T) {
	p := ta.NewPaths("/datalog/trace-archive-v4")
	ref := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 42, Row: 7}

	wantPart := filepath.Join("/datalog/trace-archive-v4", "dt=2026-09-24", "inst=blue", "part-000042.jsonl.zst")
	if got := p.PartFile(ref); got != wantPart {
		t.Fatalf("PartFile:\n want %s\n got  %s", wantPart, got)
	}
	wantSpool := filepath.Join("/datalog/trace-archive-v4", "spool", "dt=2026-09-24", "inst=blue", "part-000042.jsonl")
	if got := p.SpoolFile(ref); got != wantSpool {
		t.Fatalf("SpoolFile:\n want %s\n got  %s", wantSpool, got)
	}
}

func TestPartNameZeroPadsSixDigits(t *testing.T) {
	p := ta.NewPaths("/x")
	for part, want := range map[int]string{
		0:     "part-000000.jsonl.zst",
		1:     "part-000001.jsonl.zst",
		99999: "part-099999.jsonl.zst",
	} {
		got := filepath.Base(p.PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: part}))
		if got != want {
			t.Fatalf("part=%d: want %s, got %s", part, want, got)
		}
	}
}
```

新建 `internal/tracearchive/ref_internal_test.go`——**A 层，`package tracearchive`**，唯一理由是 `parsePartSeq` 未导出：

```go
package tracearchive

import "testing"

// 【为什么破例放包内】parsePartSeq 是未导出的实现细节（写指针靠它从文件名反解块序号），
// 外部测试包无法访问；而它的严格性直接决定「写指针是否会撞上已存在的块」。
func TestParsePartSeq(t *testing.T) {
	// 写指针依赖从文件名反解块序号，因此解析必须严格
	cases := map[string]int{
		"part-000000.jsonl.zst": 0,
		"part-000042.jsonl.zst": 42,
		"part-000007.jsonl":     7,
	}
	for name, want := range cases {
		got, ok := parsePartSeq(name)
		if !ok || got != want {
			t.Fatalf("%s: want (%d,true), got (%d,%v)", name, want, got, ok)
		}
	}
}

// TestParsePartSeqRejectsIllegalNames 逐个分支覆盖 parsePartSeq 的拒绝逻辑。
//
// 判据：删掉任一分支后，必须至少有一个子用例转红。
// 每个 bad 名字都标注了它守住的是哪个分支，避免「凑一个非法名」式的假覆盖。
func TestParsePartSeqRejectsIllegalNames(t *testing.T) {
	cases := []struct {
		name string
		why  string
	}{
		{"index.jsonl", "非 part- 前缀"},
		{"part-.jsonl.zst", "数字段为空"},
		{"part-abc.jsonl.zst", "数字段非数字（Atoi 失败）"},
		{"part-1234567.jsonl", "数字段 7 位 —— 只有长度检查能挡住它"},
		{"part-1234.jsonl", "数字段 4 位 —— 只有长度检查能挡住它"},
		{"part-000042.jsonl.zst.tmp", ".tmp 后缀（rename 前的半成品，绝不能当产物）"},
		{"part-000042.ZST", "大小写不符的扩展名"},
		{"part--00042.jsonl", "负号混在数字段里 —— 长度恰好 6 位且 Atoi 成功，只有 n<0 检查能挡住它"},
		{"part-000042.jsonl.gz", "其他压缩扩展名"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if seq, ok := parsePartSeq(c.name); ok {
				t.Fatalf("%s 不应被解析为合法块名（%s），实际得到 seq=%d", c.name, c.why, seq)
			}
		})
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestPathsLayout|TestPartNameZeroPadsSixDigits|TestParsePartSeq|TestParsePartSeqRejectsIllegalNames' -v`
预期：**两个包**都编译失败——B 层提示 `undefined: tracearchive.NewPaths`（或 `undefined: ta.NewPaths`），A 层提示 `undefined: parsePartSeq`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/ref.go`：

```go
package tracearchive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Ref 是正文行的定位键。
//
// 为什么用复合键而不是物理字节偏移（规格 §5.4）：
// 压缩后的字节偏移只有在封块结束那一刻才知道，若要在写入时就写进 PG，
// 就必须在封块后回填 UPDATE（每天约 9 万次）。这四个分量在写入追加时
// 即可确定，无需回填，且在冷层合并后依然有效（规格 §5.5）。
type Ref struct {
	DT   string // 归属天，如 "2026-09-24"
	Inst string // 实例标识，"blue" / "green"（蓝绿两实例并存，必须隔离）
	Part int    // 块序号
	Row  int    // 块内行序号，从 0 开始
}

// String 返回可读形式，便于日志与错误信息。
func (r Ref) String() string {
	return fmt.Sprintf("%s/%s/part-%06d#row=%d", r.DT, r.Inst, r.Part, r.Row)
}

// Paths 负责把定位键映射为文件系统路径。
type Paths struct{ Root string }

// NewPaths 构造路径解析器。
func NewPaths(root string) Paths { return Paths{Root: root} }

// PartDir 返回某天某实例的产物目录。
func (p Paths) PartDir(dt, inst string) string {
	return filepath.Join(p.Root, "dt="+dt, "inst="+inst)
}

// SpoolDir 返回某天某实例的未压缩暂存目录。
func (p Paths) SpoolDir(dt, inst string) string {
	return filepath.Join(p.Root, "spool", "dt="+dt, "inst="+inst)
}

// PartFile 返回定位键对应的产物文件路径。
func (p Paths) PartFile(ref Ref) string {
	return filepath.Join(p.PartDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl.zst"))
}

// SpoolFile 返回定位键对应的未压缩暂存文件路径。
// 未封块的近期正文从这里读，因此新增行不必等封块即可读回。
func (p Paths) SpoolFile(ref Ref) string {
	return filepath.Join(p.SpoolDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl"))
}

func partName(seq int, ext string) string {
	return fmt.Sprintf("part-%06d%s", seq, ext)
}

// parsePartSeq 从文件名反解块序号。写指针以目录内容为权威来源（规格 §7.3），
// 因此这里必须严格：带 .tmp 后缀、非 part- 前缀、位数不对的一律拒绝。
func parsePartSeq(name string) (int, bool) {
	if !strings.HasPrefix(name, "part-") {
		return 0, false
	}
	rest := strings.TrimPrefix(name, "part-")
	// 允许 .jsonl 与 .jsonl.zst，其余（含 .tmp）拒绝
	var digits string
	switch {
	case strings.HasSuffix(rest, ".jsonl.zst"):
		digits = strings.TrimSuffix(rest, ".jsonl.zst")
	case strings.HasSuffix(rest, ".jsonl"):
		digits = strings.TrimSuffix(rest, ".jsonl")
	default:
		return 0, false
	}
	if len(digits) != 6 {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
```

新建 `internal/tracearchive/errors.go`：

```go
package tracearchive

import "errors"

// 读取侧的三类失败必须严格区分（规格 §8.1）：
// 把「文件缺失」误报成「数据不存在」是 v3 方案的严重缺陷，必须避免。
var (
	// ErrPartNotFound 产物文件缺失：正文曾落盘，但文件不在（需人工介入）。
	// 对应 HTTP 410。
	ErrPartNotFound = errors.New("tracearchive: 产物文件缺失")

	// ErrBodyMissing 正文在写入时即失败，从未落盘。对应 HTTP 404。
	ErrBodyMissing = errors.New("tracearchive: 正文从未落盘")

	// ErrNotArchived 产物与 spool 都不存在，且无 body_missing 标记。
	// 说明元数据与存储不一致，属异常，对应 HTTP 500。
	ErrNotArchived = errors.New("tracearchive: 产物与暂存均不存在")
)

// 封块侧的错误（方案 C）。sealer 的调用方（计划 4 的 sealer 循环/告警）
// 需要把「重复封块」与「压缩失败」区分开：
//   - ErrPartExists 是幂等的、无害的（下轮扫描还会看到它，但不必告警）；
//   - ErrRowCountMismatch / ErrSealFailed 需要告警与重试。
// 三者都必须用 %w 包装，否则调用方的 errors.Is 分类失效。
var (
	// ErrPartExists 产物已存在，拒绝覆盖（规格 §7.3：宁可留下 spool，不可覆盖唯一副本）。
	ErrPartExists = errors.New("tracearchive: 产物已存在，拒绝覆盖")

	// ErrRowCountMismatch spool 的实际行数与调用方记录的行数不符
	//（spool 被截断，或写入侧与封块侧对不上）。此时拒绝封块、保留 spool。
	ErrRowCountMismatch = errors.New("tracearchive: 行数不符")

	// ErrSealFailed 封块失败：含 zstd 子进程非零退出、子进程无法启动（二进制缺失）、
	// 或压缩 I/O 失败。语义固定为「保留 spool、删除 .tmp、不产生产物、下轮重试」。
	ErrSealFailed = errors.New("tracearchive: 封块失败")
)
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestPathsLayout|TestPartNameZeroPadsSixDigits|TestParsePartSeq|TestParsePartSeqRejectsIllegalNames' -v`
预期：**两个包**都 PASS（含 `TestParsePartSeqRejectsIllegalNames` 的 9 个子用例，每个对应一个拒绝分支）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/ref.go internal/tracearchive/errors.go \
        internal/tracearchive/ref_internal_test.go tests/tracearchive/ref_test.go
git commit -m "feat(tracearchive): 定位键、目录布局与错误分类

定位键用 (dt, inst, part, row) 而非物理偏移，写入时即可确定、无需回填。
错误严格区分「产物缺失 / 正文从未落盘 / 未归档」，避免把文件缺失
误报成数据不存在；另加封块侧的 ErrPartExists / ErrRowCountMismatch /
ErrSealFailed 三个哨兵，供方案 C 的 sealer 调用方用 errors.Is 分类。
测试按可达性分层：公开 API 的用例放 tests/tracearchive/，
只有 parsePartSeq 严格解析（未导出）留在包内。"
```

---

### Task 5：spool 顺序追加写入器

**文件：**
- 新建：`internal/tracearchive/writer.go`
- 新建：`tests/tracearchive/writer_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**：`NewWriter`/`WriterOptions`/`Append`/`AppendFailed`/`PendingSeals`/`MarkSealed`/`Close`/`SealTask` 全部导出，本任务的主体测试进 B 层。
- 新建：`internal/tracearchive/writer_internal_test.go`——**A 层（`package tracearchive`）**，5 个用例。**这是交付时的实际形态，简报原先写的「本任务没有任何必须触达未导出符号的断言、整份测试进 B 层」是错的**（详见下方「落地修正」④）：写失败注入必须替换未导出的 `cur.w`，`MarkSealed` 的 fd 关闭回归必须持有未导出的 `cur.f`。

> **落地转换说明（本块与后续 B 层块通用）**：下面的代码按 `package tracearchive` 书写以便阅读；落盘到 `tests/tracearchive/writer_test.go` 时必须做三件事：
> 1. 头部改为 `package tracearchive_test`，并加导入 `ta "github.com/example/llm-gateway/internal/tracearchive"`（范例见任务 4 的 `tests/tracearchive/ref_test.go`）。
> 2. **所有导出标识符加 `ta.` 前缀**：`NewWriter`→`ta.NewWriter`、`WriterOptions`→`ta.WriterOptions`、`Ref`→`ta.Ref`、`NewPaths`→`ta.NewPaths`、`SealTask`→`ta.SealTask`。漏掉任何一个都会编译失败，这是自动可查的。
> 3. **有一处必须改写而不是加前缀**：`w.paths.SpoolFile(...)` 访问的是 `Writer` 的未导出字段 `paths`，外部包不可见——改为等价的公开表达 `ta.NewPaths(root).SpoolFile(ta.Ref{...})`（这本身就是更好的断言：它不依赖实现内部字段名）。
> 其余包内私有辅助（`countLines`）原样保留在本文件里。

**接口：**
- 依赖输入：
  - 任务 1：`defaultBlockBytes`、`defaultSealInterval`
  - 任务 4：`Ref`、`Paths`、`NewPaths`、`parsePartSeq`
- 对外产出：
  - `type WriterOptions struct { Root, Inst string; BlockBytes int64; SealInterval time.Duration }`
  - `type Writer struct{...}`
  - `func NewWriter(opts WriterOptions) (*Writer, error)`
  - `func (w *Writer) Append(dt string, line []byte) (Ref, error)`——返回定位键；**行内裸换行与「trim 后为空」的行一律拒绝，不转义**（`validateLine`，见下与「落地修正」①）。简报这里原先写的是「行内换行会被转义」，与代码不符——这是 D13，已按「拒绝」落地。
  - `func (w *Writer) AppendFailed(dt string) error`——记录「本行正文落盘失败」：**推进行号 + 写一个内容为 `null` 的标记行**，使「物理行序 == `row_seq`」恒成立。
    **不能「只推进行号不写字节」——这是格式强制，不是取舍**：footer 的 `row_count` 是产物内行数，trailer 是「每行一个差分 varint」的偏移表（任务 2 的 `encodeTrailer`），而该差分 varint 编码**没有表达「本行没有偏移」的能力**（它假定行稠密）；读取侧两条路径又都是「按行序取第 row 行」。写侧稀疏 + 格式要求稠密 + 读侧按行序三者不可能同时成立 ⇒ 该行之后的行整体前移一位，**`row=N` 会被读成第 `N+1` 行的正文**（静默返回错误的正文）。
    标记取 `null`（合法 JSON，4 字节 + 换行）：不用空行（产物是 `.jsonl`，每行应为合法 JSON，空行会让按行解析的工具炸掉）；不用 `{}`（与「合法的空正文」无法区分）；spool 每行都是正文对象（`request_headers`/`request_body`/`response_body` 那组键），故不可能与真实正文混淆。读取侧遇标记行必须返回 `ErrBodyMissing`（任务 7）。
  - `func (w *Writer) PendingSeals() ([]SealTask, error)`——返回达到封块条件的块。
    **返回前必须 flush 写缓冲**：封块器直接读 spool 文件，若缓冲未落盘，它会读到不完整的块。
    flush 失败必须以 error 返回，不可静默。
    **⚠️ flush 必须无条件执行**（放在 `cur == nil` 判断之后、所有阈值早退之前），**不是「只在产出封块任务时 flush」**。理由是生产理由而非测试理由：这是方案 C 下网关唯一的周期性 flush 入口，若未达阈值就跳过 flush，一个刚建立的块在其生命周期的前 `SealInterval`（15 分钟）内磁盘上始终是**空文件**、mtime 停在创建时刻 ⇒ sealer 的 minAge 判断会把这种「安静的活跃块」当成「已停止追加」，封出空产物并删掉 spool ⇒ 缓冲里的正文全部丢失且无任何报错（与 D3「忘了 flush」是同一类静默丢数据，只是触发条件变成「没到阈值」）。
  - `func (w *Writer) MarkSealed(dt string, part int)`——封块成功后清空当前块状态（任务 8 在 `Seal` 之后调用）。
    **必须连同旧文件句柄一起关闭**（flush + `close`），只把 `cur` 置 nil 会让该块的 `*os.File` 泄漏：每封一块泄漏一个 fd，长跑进程最终 EMFILE。调用点冻结为无返回值，因此 close 错误只能丢弃——此时 spool 已被 sealer 完整读取，未落盘的缓冲不再可能有读者，这与「`PendingSeals` 必须上抛 flush 错误」并不矛盾（那里是为了不把不完整的块交给 sealer）。
  - `func (w *Writer) Close() error`
  - **参数校验**（均为无哨兵的参数校验：启动期/调用期的致命错误，调用方无从分支处理，故沿用本包约定只断言「必须报错」，不新增 `ErrInvalidInst` 之类的哨兵）。
    - **`Inst` 白名单（现状，已落地在提交 `8cd873e7`；理由已在 `eaccd92b` 更正）**：只接受 `"blue"`/`"green"`。`inst` 直接拼进目录名，而 `filepath.Join` 的清理**只对「裸 `..` 元素」生效**——紧跟 `inst=` 前缀的 `..` 会被前缀粘成字面名字，所以**真正的穿越向量是含斜杠的值**：`Inst="../../green"` ⇒ `<root>/dt=X/green`（越出 `inst=` 层，把两个实例的正文混进同一目录）、`Inst="a/../../green"` ⇒ `<root>/green`。**反例（旧表述在这里是错的）**：`Inst="../green"` ⇒ `<root>/dt=X/inst=../green`、`Inst="blue/"` ⇒ `<root>/dt=X/inst=blue`——两者都**不折叠**；它们同样必须拒绝，但理由是「目录名里出现了与标识不符的段，`Ref` ⇄ 路径不再一一对应」，**不是「越出子树」**。**用一条白名单分支同时涵盖空值与非法值**（`""` 不在枚举里），不再单列「Inst 非空」分支——删掉那个独立分支没有任何用例转红，是零覆盖守卫。
    - **⚠️ `dt` 格式校验（现状，已落地在评审修正 `52ccc9b2`；理由已在 `eaccd92b` 更正）**：`Append`/`AppendFailed` 收到的 `dt` 由 `validateDT` 校验（`time.Parse("2006-01-02", dt)` 必须成功，日期形式常量 `dateLayout`）。理由与 `Inst` 白名单同类，**两者是同一个洞的两半**——`dt` 同样直接拼进目录名，**真正的向量是含斜杠的值**：`dt="2026-09-24/../.."` ⇒ `SpoolDir` 得 `<root>/inst=blue`、产物越出 `<root>` 本身；而反例 `dt="../x"` ⇒ `<root>/dt=../x/inst=blue`（**不折叠**），被拒的理由是「不是合法日期 + 目录名与标识不符」。`time.Parse` 一次挡住四类输入：空串与非法形态（`""`、`"../x"`）、长度不符（`"20260924"`）、**未补零（`"2026-9-24"`，会让同一天出现两个目录名而点读只认其中一个）**、非法日期（`"2026-13-01"`、`"2026-02-30"`），其中真穿越向量 `"2026-09-24/../.."` 也一并被挡。校验放在 `w.mu.Lock()` **之前**，先于任何文件系统副作用——非法 `dt` 不得留下任何目录；`AppendFailed` 复用 `Append`，因此两个写入入口共用同一道校验。同样不设哨兵。

  ```go
  type SealTask struct {
      DT   string
      Inst string
      Part int
      SpoolPath string
      Rows int              // 0 表示未知（任务 6 的 ListPendingSeals 扫目录时不知道行数）
      UncompressedBytes int64
  }
  ```

> **落地修正（Task 5 交付后追加：与最终实现对齐，以代码为准）**
>
> **提交链**：`8cd873e7`（写入器主体：`NewWriter`/`Append`/`AppendFailed`/`PendingSeals`/`MarkSealed`/`Close`/写指针）→ `52ccc9b2`（`dt` 路径分量校验、删除无消费者的 `ListParts`）→ `771d7985`（行校验 + 写失败隔离）→ `8359f4bc`（修正写失败注释对任务 6 残行行为的描述）→ `49475a78`（三处失实注释修正 + `truncateToLastNewline` 的清空分支用例）。
>
> **① `validateLine` / `appendLineLocked` 分层与理由（`771d7985`）**：公开 `Append` 的顺序是 `validateDT` → `trimNewline` → `validateLine` → `appendLineLocked`。**校验只放在公开入口上**：内部共享的 `appendLineLocked`（持锁：确保块 → 缓冲写入 → `rows++` → `bytes += n+1`）**既不做 dt/inst 校验、也不做行校验**——`AppendFailed` 复用这段共享代码（让标记行与真实正文由构造共享同一套不变量），守卫若放进共享路径，**失败标记行会被自己的守卫拒绝**。`validateLine` 拒绝两件事：正文（trim 后）内含裸 `\n`/`\r`；trim 后长度为 0。**为什么是拒绝而不是转义**：产物必须是**逐字节的原始 JSONL**；写入侧转义会把一个反转义变换强加给读取侧，并让产物不再是「原样 JSONL」（任何按行解析的工具都会读到非原字节）；而 JSON 序列化本身不会产生裸换行（`\n` 会被序列化成 `\\n`），所以含裸换行的输入只可能是**调用方 bug** ⇒ 快速失败优于静默改造。末尾换行是**分隔符不是内容**：`trimNewline` 去掉末尾整段 `\r`/`\n`（任意个），故 `"a\n"` 与 `"a"` 等价——**首轮 17 个用例全部传带尾换行的行，本轮没有一个被打红**，即该语义的实证。**不新增哨兵**（与 dt/inst 同属调用方无从分支处理的参数错误）。
>
> **② `null` 标记行的前置条件（未成文义务，必须写明）**：`Append` 的 `line` 必须是**单行 JSON 对象**，且**不得是字面量 `null`**——`null` 是 `AppendFailed` 的失败标记行，读取侧按整行相等识别它。生产侧正文恒为 `{...}`（结构体序列化）故实际安全；但这是**调用方义务**。**不做 `null` 校验**：那会连标记行一起禁掉；若将来允许裸 JSON 标量写进正文，这条必须重新评估。
>
> **③ 写失败隔离四步机制（`771d7985`；完整理由见规格 §7.1）**：
> - ① `truncateToLastNewline` 回退到**最后一个 `\n` 边界**（反向**有界**扫描，界 = 本次失败那一行的长度；整个文件无 `\n` ⇒ `Truncate(0)`）；
> - ② `quarantinePath` 隔离改名为 `part-NNNNNN.jsonl.zst`。**该名必须经 `partName` 生成，不能把后缀直接拼到 spool 名后**——首轮真 bug 是拼出 `part-NNNNNN.jsonl.jsonl.zst`，`parsePartSeq` 拒绝它 ⇒ 序号 N 被复用（旧 PG 的 `(part=N,row=r)` 会静默读到另一个 part 的正文，最坏的一类错）。安全性的两条依据：`ListPendingSeals` 只收 `.jsonl` 结尾 ⇒ sealer 看不到隔离件；`parsePartSeq` 接受 `.jsonl.zst` ⇒ N 被算作已用、永不复用；
> - ③ **毒化**该块：不再写入、`PendingSeals` 不报它、`Close` 只释放 fd；后续 `Append` 自动换到 N+1（调用方无需重置状态）；`ensureBlockLocked` 的毒化判断不可停用（停用即转红）；
> - ④ 改名失败 ⇒ `Truncate(0)`（0 字节产不出残行）；再失败则在错误里如实说明。
> - **【已知代价】（只记账、不修；已写进 `writer.go` 的注释，不只留在报告里）**：持续 ENOSPC 下每次失败 `Append` 都换新 part，而序号计算每次都重扫产物目录与 spool 目录 ⇒ 块序号与目录项**无界增长**，且每次 `Append` 付一次 O(n) 目录扫描。**刻意不加计时器或计数器**——正确修法是**背压**（R9，计划 2）。它不产生静默错正文。
> - **已知副作用**：隔离件内容是**明文 JSONL 而非 zstd 帧**，名字却与产物等价；点读按两条精确路径查找（产物目录的 `part-N.jsonl.zst` 与 spool 目录的 `part-N.jsonl`）都查不到它 ⇒ 返回 `ErrNotArchived`（响亮），**不是** `ErrFooterCorrupt`，**读取侧不新增分支或哨兵**。运维判据：spool 下出现「内容是明文 JSONL 的 `part-*.jsonl.zst`」即为隔离件，释放空间后改回 `part-NNNNNN.jsonl` 即可正常封块。
>
> **④ 新增的 A 层用例与「为什么必须触达未导出符号」（本节 文件 清单已同步更正）**：新增 `internal/tracearchive/writer_internal_test.go`（A 层，5 个用例），文件头逐条写明理由：
> - **写失败注入**：真正要防的形态是「`bufio` 直写 fd 时短写、留下没有换行收尾的残行」，要精确制造它**只能**替换未导出的 `cur.w`（`*bufio.Writer`）并给它一个「只接受 K 字节就报错」的底层 writer。ENOSPC 无法经公开 API 制造：`chmod` 不改已打开 fd 的写权限；把 spool 路径建成目录只在 `open` 阶段失败（那是「建块失败」，不产生残行）；`/dev/full` 在 macOS 上不存在。而盘满正是本子项目存在的那条路径，不能用「难以模拟」搪塞。
> - **`MarkSealed` 的 close 回归**：旧句柄是否真被关闭只能通过持有未导出的 `cur.f` 观察（fd 不经任何公开 API 暴露）。不写这条，把修复退回「只置 `cur = nil`」时其余用例仍全绿——即修复没有回归防线。
> - 用例名：`TestWriterWriteFailureLeavesNoResidualLine`、`TestWriterPoisonedBlockCannotBeSealed`、`TestWriterTruncatesWholeFileWhenNoLineBoundaryExists`、`TestWriterQuarantineRenameFailureEmptiesSpool`、`TestWriterMarkSealedClosesPreviousFileHandle`。B 层本轮新增 4 个：`TestWriterAppendRejectsInteriorNewline`、`TestWriterAppendRejectsEmptyLine`、`TestWriterMarkerLineCountsRowsAndBytes`、`TestWriterMarkSealedIgnoresNonMatchingPart`。
> - **`Truncate(0)` 分支用例（`49475a78`）**：`TestWriterTruncatesWholeFileWhenNoLineBoundaryExists` 先用包内 `ensureBlockLocked` 建出一个 0 字节空块（天然不含 `\n`），再注入短写，让**第一次触发 flush 的写入**就短写；断言隔离件被清空、块被毒化、下一次 `Append` 换 part。**它同样只能放包内**：用公开 API 走不到这个前置状态——首行写入要么成功（盘上出现 `\n`），要么在建块阶段就失败（没有文件）。判据：把 `truncateToLastNewline` 末尾的 `return os.Truncate(path, 0)` 改成 `return nil`，本用例转红（隔离件里会留下 2 字节残行 `"li"`）。
>
> **⑤ 最终用例规模**：B 层 writer 用例 21 个（19 个 `TestWriter*` + 2 个 `TestNewWriter*`），A 层 5 个。变异自检 14 项（`m19`~`m32`）**全部转红**，完整双侧原始输出见 Task 5 报告 §11.6.1。所有 RED/GREEN 证据必须满足全局约束的 `-run` 三条要求。

- [ ] **步骤 1：编写失败的测试**

新建 `tests/tracearchive/writer_test.go`——**B 层，`package tracearchive_test`**（转换方式见上方说明）：

```go
package tracearchive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterAppendAssignsDenseRowSeq(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 5; i++ {
		ref, err := w.Append("2026-09-24", []byte(`{"request_id":"r`+string(rune('0'+i))+`"}`+"\n"))
		if err != nil {
			t.Fatalf("第 %d 次追加失败: %v", i, err)
		}
		if ref.Row != i {
			t.Fatalf("行号应连续递增: want %d, got %d", i, ref.Row)
		}
		if ref.DT != "2026-09-24" || ref.Inst != "blue" || ref.Part != 0 {
			t.Fatalf("定位键其余分量不符: %+v", ref)
		}
	}
}

// TestWriterAppendFailedWritesMarkerLineAndAdvancesRow 锁定「正文落盘失败」的语义：
// 行号照常推进（与 PG 的 row_seq 对齐），并且必须落一个标记行 ——
// 产物格式（footer 的 row_count + trailer 的逐行差分 varint）要求行稠密，
// 只推进行号不写字节会让该行之后的所有行「row=N 读成第 N+1 行」，静默返回错误正文。
//
// 原用例名为 ...RowSeqWithoutBytes；控制者裁决后「不写字节」变成「写 null 标记行」，
// 因此改名（断言意图不变：行号推进 + 该行在文件里真实可见）。
func TestWriterAppendFailedWritesMarkerLineAndAdvancesRow(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("line0\n")); err != nil {
		t.Fatal(err)
	}
	// 模拟正文落盘失败：行号仍要推进，保证与 PG 元数据行的 row_seq 对齐
	if err := w.AppendFailed("2026-09-24"); err != nil {
		t.Fatalf("AppendFailed 失败: %v", err)
	}
	ref, err := w.Append("2026-09-24", []byte("line2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Row != 2 {
		t.Fatalf("落盘失败的行仍应占用行号: want row 2, got %d", ref.Row)
	}

	// 【读文件前必须先让写缓冲落盘】Writer 用 bufio 缓冲（这正是 D3 缺陷的成因），
	// 而 os.ReadFile 只看磁盘。PendingSeals 是本包唯一的「周期性 flush 入口」
	// （方案 C 下 gateway 的定时器就按 flush_interval 调它，规格 §7.1），
	// 故用它把缓冲刷下去；此刻两个阈值都远未达到，它返回 0 个任务，只承担 flush 职责。
	if _, err := w.PendingSeals(); err != nil {
		t.Fatalf("PendingSeals（flush 入口）失败: %v", err)
	}

	// 文件里应有 3 行：line0 / null（标记行） / line2 —— 行序与 row_seq 一一对应
	b, err := os.ReadFile(NewPaths(root).SpoolFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatalf("读 spool 失败: %v", err)
	}
	if got := countLines(b); got != 3 {
		t.Fatalf("spool 应有 3 行（含失败行的 null 标记），实际 %d（内容 %q）", got, b)
	}
	if string(b) != "line0\nnull\nline2\n" {
		t.Fatalf("spool 内容应为 %q，实际 %q", "line0\nnull\nline2\n", b)
	}
}

func TestWriterPendingSealsBySize(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 32})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		if _, err := w.Append("2026-09-24", []byte("0123456789\n")); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("PendingSeals 失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应产生 1 个封块任务，实际 %d", len(tasks))
	}
	if tasks[0].Rows != 4 {
		t.Fatalf("封块任务行数应为 4，实际 %d", tasks[0].Rows)
	}
	if _, err := os.Stat(tasks[0].SpoolPath); err != nil {
		t.Fatalf("封块任务的 spool 路径应存在: %v", err)
	}
}

// TestWriterPendingSealsFlushesBuffer 是 D3 缺陷的回归测试：
// 封块器直接读 spool 文件，若 Writer 未 flush 写缓冲，它会读到不完整的块。
func TestWriterPendingSealsFlushesBuffer(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30, SealInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 写入量远小于 bufio 缓冲（1 MiB），若未 flush 则文件为空
	line := []byte("0123456789")
	if _, err := w.Append("2026-09-24", line); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("PendingSeals 失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应产生 1 个封块任务，实际 %d", len(tasks))
	}
	b, err := os.ReadFile(tasks[0].SpoolPath)
	if err != nil {
		t.Fatal(err)
	}
	if countLines(b) != 1 {
		t.Fatalf("封块任务返回时 spool 必须已 flush，实际读到 %d 行（内容 %q）", countLines(b), b)
	}
}

func TestWriterPendingSealsByInterval(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{
		Root: root, Inst: "blue",
		BlockBytes:   1 << 30,             // 永不因大小触发
		SealInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if tasks, err := w.PendingSeals(); err != nil {
		t.Fatal(err)
	} else if len(tasks) != 0 {
		t.Fatal("未到时间阈值不应产生封块任务")
	}
	time.Sleep(15 * time.Millisecond)
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("超过时间阈值应产生 1 个封块任务（否则低峰天正文写了永远读不到），实际 %d", len(tasks))
	}
}

func TestWriterNextSeqResumesFromDirectory(t *testing.T) {
	root := t.TempDir()
	p := NewPaths(root)

	// 预置两个已封块产物与一个 spool，模拟进程重启
	dir := p.PartDir("2026-09-24", "blue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"part-000000.jsonl.zst", "part-000005.jsonl.zst"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	spoolDir := p.SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(spoolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spoolDir, "part-000007.jsonl"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	ref, err := w.Append("2026-09-24", []byte("z\n"))
	if err != nil {
		t.Fatal(err)
	}
	// 写指针以目录内容为权威（规格 §7.3）：max(5,7)+1 = 8
	if ref.Part != 8 {
		t.Fatalf("块序号应从目录恢复为 8，实际 %d", ref.Part)
	}
	if ref.Row != 0 {
		t.Fatalf("新块的行号应从 0 开始，实际 %d", ref.Row)
	}
}

// TestNewWriterRejectsEmptyRootAndInst 覆盖 NewWriter 的参数校验分支。
// 这两个 error 不是分类错误（没有定义哨兵），因此只断言「必须报错且不产生文件」。
// 空 Inst 由下面的白名单一条分支涵盖（"" 不在枚举里），不再单列「非空」分支。
func TestNewWriterRejectsEmptyRootAndInst(t *testing.T) {
	if w, err := NewWriter(WriterOptions{Root: "", Inst: "blue"}); err == nil {
		w.Close()
		t.Fatal("Root 为空应报错")
	}
	if w, err := NewWriter(WriterOptions{Root: t.TempDir(), Inst: ""}); err == nil {
		w.Close()
		t.Fatal("Inst 为空应报错")
	}
}

// TestNewWriterRejectsIllegalInst 锁定 Inst 白名单（"blue" / "green"）。
//
// 为什么必须有这道校验：inst 直接拼进目录名，而 filepath.Join 会做路径清理。
// 清理只对「裸 `..` 元素」生效——紧跟 "inst=" 前缀的 ".." 会被前缀粘成字面名字，
// 所以真正越出子树的是**含斜杠**的值（以下均实测，root="/r"）：
//   - inst="../../green"   ⇒ /r/dt=X/green（越出 inst= 层，把两个实例的正文混进
//     同一目录）；
//   - inst="a/../../green" ⇒ /r/green；
//   - inst="blue/"         ⇒ /r/dt=X/inst=blue（只是去掉了尾部分隔符，目录名与
//     「实例」标识不再一一对应）。
//
// 只查非空挡不住这些，必须白名单。
// （反例：inst="../green" 实测得 /r/dt=X/inst=../green —— **不折叠**；它被拒绝的
// 理由是「出现了与标识不符的段」，不是「越出子树」。）
func TestNewWriterRejectsIllegalInst(t *testing.T) {
	root := t.TempDir()
	for _, inst := range []string{"red", "../green", "blue/", "blue/../green", "../../green", "a/../../green", "BLUE", "blue "} {
		w, err := NewWriter(WriterOptions{Root: root, Inst: inst})
		if err == nil {
			w.Close()
			t.Fatalf("Inst=%q 应被拒绝（越出 inst= 子树或与标识不符）", inst)
		}
	}
	// 反向断言：白名单内的实例必须仍然可用。否则「一律拒绝」也会让上面的
	// 用例全绿，而那样的守卫是坏的。
	for _, inst := range []string{"blue", "green"} {
		w, err := NewWriter(WriterOptions{Root: root, Inst: inst})
		if err != nil {
			t.Fatalf("Inst=%q 应被接受: %v", inst, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Inst=%q Close 失败: %v", inst, err)
		}
	}
}

// TestWriterAppendRejectsIllegalDT 锁定 dt 的「路径分量」校验（已落地在 `52ccc9b2`）。
//
// dt 与 inst 是同一个洞的两半：两者都直接拼进目录名，而 filepath.Join 会做
// 路径清理。**清理只对「裸 `..` 元素」生效**——紧跟 "dt=" 前缀的 ".." 会被前缀
// 粘成字面名字，所以真正的向量是含斜杠的值（实测，root="/r"）：
// dt="2026-09-24/../.." ⇒ SpoolDir 得 /r/inst=blue、PartDir 得 /inst=blue，
// 【越出 <root>】。因此 dt 必须是严格日期 YYYY-MM-DD，time.Parse 一次挡住
// 路径穿越、空串、未补零、非法日期。
//
// 反例（别写成「dt="../x" 会被清成 <root>/x」）：实测得到 /r/dt=../x/inst=blue,
// **不折叠**；它被拒是因为不是合法日期 + 目录名与标识不符。
//
// 判据：删掉 Append 里的 validateDT 调用，本用例转红（第一类断言即 err == nil）。
func TestWriterAppendRejectsIllegalDT(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	bad := []string{
		"",                     // 空串
		"../x",                 // 非日期 + 目录名与标识不符（**不折叠**，见上方反例）
		"2026-9-24",            // 未补零：同一天会出现两个目录名
		"2026-13-01",           // 非法月份
		"2026-02-30",           // 非法日期
		"2026-09-24T00:00:00Z", // 带时间部分（多出文本）
		"2026-09-24/../..",     // 真穿越向量：裸 .. 被折叠 ⇒ 越出 <root>
		"2026-09-24/../../x",   // 同上，末尾多一段
		"20260924",             // 无分隔符
	}
	for _, dt := range bad {
		if _, err := w.Append(dt, []byte("x\n")); err == nil {
			t.Fatalf("Append 的 dt=%q 应被拒绝", dt)
		}
		if err := w.AppendFailed(dt); err == nil {
			t.Fatalf("AppendFailed 的 dt=%q 应被拒绝", dt)
		}
	}
	// 校验必须先于任何文件系统副作用：全部非法输入被拒后，归档根目录必须是空的。
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("非法 dt 不得产出任何文件/目录，实际在 %s 下看到 %v", root, ents)
	}

	// 反向断言：合法日期必须仍被接受（否则「一律拒绝」也会让上面的断言全绿）
	ref, err := w.Append("2026-09-24", []byte("x\n"))
	if err != nil {
		t.Fatalf("合法 dt 应被接受: %v", err)
	}
	if ref.DT != "2026-09-24" || ref.Part != 0 || ref.Row != 0 {
		t.Fatalf("合法 dt 的定位键不符: %s", ref)
	}
	if err := w.AppendFailed("2026-09-25"); err != nil {
		t.Fatalf("AppendFailed 的合法 dt 应被接受: %v", err)
	}
}

// TestWriterPendingSealsNilBeforeAnyAppend 覆盖「当前块不存在」守卫。
// 删掉该守卫会立刻 nil 解引用 panic —— 这个用例就是那道防线的回归保护。
func TestWriterPendingSealsNilBeforeAnyAppend(t *testing.T) {
	w, err := NewWriter(WriterOptions{Root: t.TempDir(), Inst: "blue", BlockBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("无当前块时不应报错: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("尚未追加任何行时不应产生封块任务，实际 %d", len(tasks))
	}
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestWriterAppendAssignsDenseRowSeq|TestWriterAppendFailedWritesMarkerLineAndAdvancesRow|TestWriterPendingSealsBySize|TestWriterPendingSealsFlushesBuffer|TestWriterPendingSealsByInterval|TestWriterNextSeqResumesFromDirectory|TestNewWriterRejectsEmptyRootAndInst|TestNewWriterRejectsIllegalInst|TestWriterPendingSealsNilBeforeAnyAppend|TestWriterAppendRejectsIllegalDT' -v`
预期：`./tests/tracearchive/` 编译失败，提示 `undefined: ta.NewWriter`、`undefined: ta.WriterOptions`、`undefined: ta.SealTask`；`./internal/tracearchive/` 无此任务的用例（任务 2~4 的 A 层用例因 `-run` 过滤而不执行）。

> 注意 `TestNewWriterRejectsEmptyRootAndInst` / `TestNewWriterRejectsIllegalInst` / `TestWriterPendingSealsNilBeforeAnyAppend` **不以 `TestWriter` 开头**，写完的 `-run` 组合里必须显式带上它们。

> **用例清单相对简报原文的变化（与提交 `8cd873e7` + 评审修正 `52ccc9b2` 对齐）**：`TestWriterAppendFailedAdvancesRowSeqWithoutBytes` 已按控制者裁决**改名并改期望**为 `TestWriterAppendFailedWritesMarkerLineAndAdvancesRow`（spool 由 2 行变为 3 行：`line0`/`null`/`line2`，`ref.Row == 2` 不变，读文件前先用 `PendingSeals()` 落缓冲）；新增 `TestNewWriterRejectsIllegalInst`（Inst 白名单）与 `TestWriterAppendRejectsIllegalDT`（dt 路径分量校验：8 个非法输入 + 「非法 dt 不得留下任何文件/目录」+ 合法日期反向断言），二者均**已落地**；`TestWriterListPartsFiltersIllegalNames` 随 `ListParts` 按 YAGNI 一并删除。交付的实际测试文件还补了若干简报未列的守卫用例（默认阈值、`SealTask` 全字段、末尾换行、跨天换块、spool 目录不可用、`Close` 幂等、`MarkSealed` 换块），与任务 4 交付时「用例超出简报」的既有做法一致，本节不再逐一列出。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/writer.go`：

```go
package tracearchive

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 路径分量校验（dt 与 inst 必须成对看待，一处漏掉就是同一个洞的另一半）。
//
// 两个分量都直接拼进目录名（<root>/dt=<dt>/inst=<inst>），而 filepath.Join 会做
// 路径清理。**清理只对「裸 `..` 元素」生效**：紧跟 "dt="/"inst=" 前缀的那个 ".."
// 会被前缀粘成字面名字（"inst=.." 不是父目录记号），所以真正的穿越向量是
// **含斜杠**的值（以下均实测，root="/r"）：
//   - dt="2026-09-24/../.." ⇒ SpoolDir 得 /r/inst=blue、PartDir 得 /inst=blue
//     —— 越出 <root> 本身；
//   - inst="../../green"    ⇒ /r/dt=X/green —— 越出 inst= 层，把两个实例的正文
//     混进同一目录；
//   - inst="a/../../green"  ⇒ /r/green。
//
// 反例（**别**写成「紧贴前缀的 .. 也会折叠」）：inst="../green" 实测得
// <root>/dt=X/inst=../green，dt="../x" 实测得 <root>/dt=../x/inst=blue —— 均不折叠。
// 它们同样必须拒绝，但理由是「目录名里出现了与标识不符的段，Ref ⇄ 路径不再
// 一一对应」，不是「越出子树」。
//
// 因此两者都用严格取值而不是「非空」：
//   - inst：枚举白名单（见 NewWriter）；
//   - dt：严格日期格式（见 validateDT）。
const (
	instBlue  = "blue"
	instGreen = "green"

	// dateLayout 是 dt 的唯一合法形式：YYYY-MM-DD，且必须补零。
	// 不补零（"2026-9-24"）会让同一天出现两个目录名，而点读只能按 Ref.DT
	// 拼出其中一个路径，另一个目录里的正文就永远读不到。
	dateLayout = "2006-01-02"
)

// validateDT 校验归属天分量（与 NewWriter 的 Inst 白名单同属「路径分量校验」）。
//
// time.Parse 一次挡住四类输入：
//   - 路径穿越与空串（"../x"、""）；
//   - 长度不符（"20260924"、"26-09-24"）；
//   - 未补零（"2026-9-24"）；
//   - 非法日期（"2026-13-01"、"2026-02-30"）。
//
// 不设哨兵错误：与 Inst 同理——这是调用方无从分支处理的参数错误，
// 加进本包的错误集合只会扩冻结接口。
func validateDT(dt string) error {
	if _, err := time.Parse(dateLayout, dt); err != nil {
		return fmt.Errorf("tracearchive: dt 必须是 %s 形式的合法日期，实际 %q", dateLayout, dt)
	}
	return nil
}

// WriterOptions 是追加写入器的配置。
type WriterOptions struct {
	Root         string        // 归档根目录，如 /datalog/trace-archive-v4
	Inst         string        // 实例标识，"blue" / "green"
	BlockBytes   int64         // 封块大小阈值；0 取 defaultBlockBytes
	SealInterval time.Duration // 封块时间阈值；0 取 defaultSealInterval
}

// SealTask 描述一个待封块的块。
type SealTask struct {
	DT                string
	Inst              string
	Part              int
	SpoolPath         string
	Rows              int
	UncompressedBytes int64
}

// curBlock 是内存中当前正在追加的块状态。
type curBlock struct {
	dt        string
	part      int
	rows      int
	bytes     int64
	startedAt time.Time
	f         *os.File
	w         *bufio.Writer
}

// Writer 负责把正文行顺序追加到 spool。
//
// 请求路径上只做顺序追加、不做压缩（规格 §7.1）。压缩由 Sealer 在后台完成。
// Writer 不是并发安全的：TraceWriter 单协程消费事件队列，因此无需加锁；
// 唯一需要互斥的是 PendingSeals 与 Append 都可能触碰 cur，故用 mu 保护。
type Writer struct {
	opts  WriterOptions
	paths Paths

	mu  sync.Mutex
	cur *curBlock
}

// NewWriter 构造写入器。
func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	// inst 是路径分量，与 dt 的校验成对（见文件头的「路径分量校验」与 validateDT）。
	// 空值也被这条白名单涵盖（"" 不在枚举里），因此不再单列「非空」分支：
	// 一个永远无法被测试区分的分支就是零覆盖的守卫。
	if opts.Inst != instBlue && opts.Inst != instGreen {
		return nil, fmt.Errorf("tracearchive: Inst 必须是 %q 或 %q，实际 %q", instBlue, instGreen, opts.Inst)
	}
	if opts.BlockBytes <= 0 {
		opts.BlockBytes = defaultBlockBytes
	}
	if opts.SealInterval <= 0 {
		opts.SealInterval = defaultSealInterval
	}
	return &Writer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Append 追加一行，返回该行的定位键。line 末尾的换行可有可无。
//
// dt 是路径分量（与 NewWriter 的 Inst 白名单成对），这里的校验是唯一的
// dt 入口：AppendFailed 复用本方法，因此两个写入入口共用同一道校验。
// 校验放在加锁之前——先于任何文件系统副作用，非法 dt 不得留下任何目录。
//
// 写入失败时不推进行号：bufio 的错误是粘性的，一次写失败后该块再也写不进
// 任何字节，调用方只能走 AppendFailed。因此不存在「行号已推进而字节没写」
// 的窗口（承重约束 3）。
func (w *Writer) Append(dt string, line []byte) (Ref, error) {
	if err := validateDT(dt); err != nil {
		return Ref{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	cur, err := w.ensureBlockLocked(dt)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{DT: dt, Inst: w.opts.Inst, Part: cur.part, Row: cur.rows}

	n, err := cur.w.Write(trimNewline(line))
	if err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加第 %s 行失败: %w", ref, err)
	}
	if err := cur.w.WriteByte('\n'); err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加换行失败: %w", err)
	}

	cur.rows++
	cur.bytes += int64(n) + 1
	return ref, nil
}

// failedRowMarker 是「本行正文从未落盘」的占位行内容（详见 AppendFailed）。
//
// 取 null 而不是空行或 {}：
//   - 空行会让任何按行解析 .jsonl 的工具炸掉（产物里每一行都应是合法 JSON）；
//   - {} 与「合法的空正文」无法区分；
//   - null 是合法 JSON，而 spool 的每一行都是正文对象
//     （request_headers/request_body/response_body 那组键），
//     因此它不可能与真实正文混淆。
var failedRowMarker = []byte("null")

// AppendFailed 登记「本行正文落盘失败」：推进行号并落一个标记行。
//
// 行号必须照常推进：PG 元数据行的 row_seq 由同一计数器分配，若这里不推进，
// 后续所有行的 row_seq 都会与产物文件错位，点读将静默取到错误的行。
//
// 【为什么必须写标记行，而不是只推进行号不写字节】产物格式强制行稠密：
// footer 的 row_count 是产物内行数，trailer 是「每行一个差分 varint」的
// 偏移表（index.go 的 encodeTrailer）—— varint 差分编码根本没有表达
// 「本行没有偏移」的能力，它假定行是稠密的。若这里不写字节，该行之后的
// 物理行序会比 row_seq 少 1，而读取侧无论走 spool 兜底（Reader.readFromSpool
// 取「第 row 行」）还是走产物（trailer 的第 row 条）都是按行取，
// 于是 row=N 会被读成第 N+1 行的正文 —— 静默返回错误的正文（也解释了任务 6
// 的行数交叉校验为何会以 ErrRowCountMismatch 拒绝封块：那是同一个矛盾的另一表征）。
//
// 读取侧约定：遇到标记行应返回 ErrBodyMissing（正文从未落盘），
// 不得把 "null" 当作正文返回。该约定由任务 7 的 Reader 落实。
//
// dt 的路径分量校验由 Append 承担（本方法复用它），因此非法 dt 在这里同样报错。
func (w *Writer) AppendFailed(dt string) error {
	// 刻意复用 Append，而不是自己再写一遍写入逻辑：
	// 标记行与真实正文在 trim/换行/行号推进/字节计数上共用同一段代码，
	// 「物理行序 == row_seq」由构造保证，而不是靠两处实现同步维护。
	if _, err := w.Append(dt, failedRowMarker); err != nil {
		return fmt.Errorf("tracearchive: 写正文缺失标记行失败: %w", err)
	}
	return nil
}

// PendingSeals 返回当前达到封块条件的块。
//
// 双触发条件（规格 §5.3.3）：达到大小阈值，或距块首行写入超过时间阈值。
// 时间条件不可省——低峰天若不按时间封块，块永远达不到大小阈值，
// 正文写了却读不到。
//
// flush 必须【无条件】执行，而不是只在「有任务要产出」时做：
// 这是方案 C 下网关唯一的周期性 flush 入口（定时器按 flush_interval 调它，
// 规格 §7.1）。若未达阈值就跳过 flush，一个刚建立的块在其生命周期的前
// SealInterval 内磁盘上始终是空文件，而 mtime 停留在它被创建的时刻 ——
// sealer 的 minAge 判断会把这种「安静的活跃块」当成「已停止追加」，
// 于是封出一个空产物并删掉 spool，缓冲里的正文全部丢失。
// 这与 D3（忘记 flush）是同一类静默丢数据，只是触发条件是「没到阈值」。
//
// 同理，flush 失败必须上抛而不是静默：sealer 会直接读 spool 文件，
// 缓冲没落盘它就把不完整的块当完整的封掉。
func (w *Writer) PendingSeals() ([]SealTask, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur := w.cur
	if cur == nil {
		return nil, nil
	}
	if err := cur.w.Flush(); err != nil {
		return nil, fmt.Errorf("tracearchive: flush 写缓冲失败: %w", err)
	}
	// 空块（建了文件但一行都没写进去）没有可封的内容。这条守卫只能通过
	// 「Append 写失败」触及，而写失败经公开 API 无法注入，故无对应用例；
	// 保留它是为了不让封块器拿到一个 0 行的 spool。
	if cur.rows == 0 {
		return nil, nil
	}
	bySize := cur.bytes >= w.opts.BlockBytes
	byTime := time.Since(cur.startedAt) >= w.opts.SealInterval
	if !bySize && !byTime {
		return nil, nil
	}
	return []SealTask{{
		DT:                cur.dt,
		Inst:              w.opts.Inst,
		Part:              cur.part,
		SpoolPath:         w.paths.SpoolFile(Ref{DT: cur.dt, Inst: w.opts.Inst, Part: cur.part}),
		Rows:              cur.rows,
		UncompressedBytes: cur.bytes,
	}}, nil
}

// MarkSealed 在封块成功后清空当前块状态。由封块者在 rename 完成后调用
// （任务 8 的端到端流程即如此）。
//
// 这里必须连同旧句柄一起关掉：只把 cur 置 nil 会让该块的 *os.File 泄漏
// （每封一块泄漏一个 fd，长跑进程最终 EMFILE）。调用点已冻结为无返回值，
// 因此 close 错误只能丢弃；但此时 spool 已被封块器完整读取，未落盘的缓冲
// 不再可能有读者 —— 这与 PendingSeals 必须上抛 flush 错误并不矛盾：
// 那里是为了不把不完整的块交给 sealer，这里已经晚于 sealer 读文件。
func (w *Writer) MarkSealed(dt string, part int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.dt == dt && w.cur.part == part {
		_ = w.closeCurLocked()
	}
}

// Close 关闭当前块文件。注意：这不等价于封块——未封块的正文由 spool 兜底读取。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeCurLocked()
}

func (w *Writer) closeCurLocked() error {
	if w.cur == nil {
		return nil
	}
	var firstErr error
	if err := w.cur.w.Flush(); err != nil {
		firstErr = err
	}
	if err := w.cur.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	w.cur = nil
	return firstErr
}

// ensureBlockLocked 确保存在可追加的当前块，必要时按目录内容恢复块序号。
func (w *Writer) ensureBlockLocked(dt string) (*curBlock, error) {
	if w.cur != nil && w.cur.dt == dt {
		return w.cur, nil
	}
	if err := w.closeCurLocked(); err != nil {
		return nil, err
	}

	next, err := w.nextSeqLocked(dt)
	if err != nil {
		return nil, err
	}

	spoolPath := w.paths.SpoolFile(Ref{DT: dt, Inst: w.opts.Inst, Part: next})
	if err := os.MkdirAll(filepath.Dir(spoolPath), 0o755); err != nil {
		return nil, fmt.Errorf("tracearchive: 建 spool 目录失败: %w", err)
	}
	// O_APPEND：进程重启后继续往同一个 spool 追加，不覆盖已写内容
	f, err := os.OpenFile(spoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tracearchive: stat spool 失败: %w", err)
	}

	w.cur = &curBlock{
		dt:        dt,
		part:      next,
		bytes:     info.Size(),
		startedAt: time.Now(),
		f:         f,
		w:         bufio.NewWriterSize(f, 1<<20),
	}
	return w.cur, nil
}

// nextSeqLocked 以目录内容为权威来源计算下一个块序号（规格 §7.3）。
//
// 不引入 PG 表或独立指针文件：目录内容本身即指针，少一个需要保持一致的状态。
// 已封块产物与未封块 spool 取较大者，+1。
func (w *Writer) nextSeqLocked(dt string) (int, error) {
	maxSeq := -1
	for _, dir := range []string{w.paths.PartDir(dt, w.opts.Inst), w.paths.SpoolDir(dt, w.opts.Inst)} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("tracearchive: 读目录 %s 失败: %w", dir, err)
		}
		for _, e := range ents {
			if seq, ok := parsePartSeq(e.Name()); ok && seq > maxSeq {
				maxSeq = seq
			}
		}
	}
	return maxSeq + 1, nil
}

// 【曾有 ListParts(dt string) ([]int, error)：返回某天某实例已存在的产物块序号（升序）】
// 控制者按 YAGNI 裁定移除：本包内没有消费者——任务 6 用自己的 ListPendingSeals
// 扫目录，任务 7 从 Ref 推导路径，后续计划也未用到。
// 已落地在评审修正 52ccc9b2：整段删除该方法、它唯一用到的 "sort" 导入，
// 以及它的两条分支用例（TestWriterListPartsFiltersIllegalNames）。

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestWriterAppendAssignsDenseRowSeq|TestWriterAppendFailedWritesMarkerLineAndAdvancesRow|TestWriterPendingSealsBySize|TestWriterPendingSealsFlushesBuffer|TestWriterPendingSealsByInterval|TestWriterNextSeqResumesFromDirectory|TestNewWriterRejectsEmptyRootAndInst|TestNewWriterRejectsIllegalInst|TestWriterPendingSealsNilBeforeAnyAppend|TestWriterAppendRejectsIllegalDT' -v`
预期：PASS（含 `AppendFailed` 行号推进 + `null` 标记行、双触发封块、目录恢复块序号、`Inst` 白名单与 `dt` 路径分量校验、无当前块五组）。
> 状态（以代码为准）：上面全部用例已在提交 `8cd873e7`（写入器主体）与评审修正 `52ccc9b2`（`dt` 校验、删除 `ListParts`）落地且 PASS。
>
> 状态（后续轮次，以代码为准）：行校验与写失败隔离（`771d7985`）之后，本节命令**不再是完整证据集**——完整用例为 **B 层 21 个 + A 层 5 个**（见上方「落地修正」④），A 层用例名必须显式加入 `-run` 组合，且证据必须满足全局约束的 `-run` 三条要求（顶层名字集合 diff 为空 / 顶层 PASS 数相符 / `SKIP == 0`）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/writer.go tests/tracearchive/writer_test.go
git commit -m "feat(tracearchive): spool 顺序追加写入器与写指针

- 请求路径只做顺序追加，压缩交给网关外的独立 sealer（方案 C）
- 双触发封块条件：大小阈值或时间阈值（后者保证低峰天正文可读）
- 写指针以目录内容为权威，进程重启后从 max(产物, spool)+1 恢复
- AppendFailed 推进行号并写 null 标记行：产物格式要求行稠密，
  只推进行号会让 row=N 读成第 N+1 行（静默返回错误正文）
- PendingSeals 无条件 flush（网关唯一的周期 flush 入口）：
  否则活跃块在磁盘上长时间为空、mtime 不动，sealer 会封出空产物丢数据
- MarkSealed 关闭旧文件句柄：只置 nil 会每封一块泄漏一个 fd（长跑 EMFILE）
- Inst 白名单（blue/green）：inst 直接拼进目录名，真正越出子树的是**含斜杠**的值
  （"../../green" ⇒ <root>/dt=X/green）；"../green"/"blue/" 不折叠，但仍须拒绝
  （目录名与标识不符），故一律白名单
- 测试放 tests/tracearchive/（全部经公开 API，不触达未导出符号）"
```

---

### Task 6：封块器（spool → zstd → trailer/footer → fsync → rename）

**文件：**
- 新建：`internal/tracearchive/sealer.go`
- 新建：`tests/tracearchive/sealer_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**：`NewSealer`/`SealerOptions`/`Seal`/`SealTask`/`ListPendingSeals`/`ErrPartExists`/`ErrRowCountMismatch`/`ErrSealFailed` 全部导出。本层放：产出与删 spool、拒绝覆盖、行数不符、footer 指向 trailer、子进程非零退出、zstd 缺失、golden 布局、`ListPendingSeals` 三例。
- 新建：`tests/tracearchive/helpers_test.go`——**B 层测试辅助**（`writeSpool`/`requireZstd`/`fakeZstd`，按下面范例用 `ta.` 限定）。
- 新建：`internal/tracearchive/sealer_internal_test.go`——**A 层（`package tracearchive`）**：`TestSealIsAtomicOnCrash`（注入 `s.failBeforeRename`，这是**未导出的测试钩子**）、`TestParsePartSeqUsedBySealer`（`parsePartSeq` 未导出）。
- 新建：`internal/tracearchive/testhelpers_test.go`——**A 层测试辅助**：与 B 层同名的 `writeSpool`/`requireZstd`/`fakeZstd` 各写一份（Go 的测试辅助不能跨包共享，**这里的重复是故意的**）。

> **为什么 `TestSealIsAtomicOnCrash` 必须在包内**：它靠注入 `s.failBeforeRename` 制造「rename 之前一步失败」，从而在**不真的崩溃**的前提下验证 I2（产物只经 rename 出现）。这个钩子若导出，就成了生产 API 的一部分——规格不要求它导出。因此这是 A 层例外，测试函数注释里要写明这一点。
>
> **落地转换**：B 层代码块按 `package tracearchive` 书写以便阅读；落盘时按任务 4/5 的同一方式转换（`package tracearchive_test` + `ta "…/internal/tracearchive"` + 导出标识符加 `ta.` 前缀）。本块涉及的导出标识符：`NewSealer`/`SealerOptions`/`SealTask`/`Ref`/`NewPaths`/`ErrPartExists`/`ErrRowCountMismatch`/`ErrSealFailed`/`ListPendingSeals`。辅助函数 `writeSpool`/`requireZstd`/`fakeZstd` 移到 `helpers_test.go`，其中 `writeSpool` 需改用公开 API：

```go
// tests/tracearchive/helpers_test.go
package tracearchive_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// requireZstd 返回 zstd 可执行文件路径；找不到就跳过。
//
// ⚠️ CI 镜像必须安装 zstd（C 版，支持 --long）：否则本包所有真封块用例
// 会静默跳过，「封块产物是否正确」就没有任何自动化证据。
func requireZstd(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd 二进制：方案 C 的封块路径需要 C 版 zstd CLI（CI 镜像必须安装）")
	}
	return bin
}

// fakeZstd 写一个可执行的假 zstd 脚本，用于固定失败语义与产物字节布局。
// script 是「#!/bin/sh」之后的内容，例如 "exec cat" 或 "exit 3"。
func fakeZstd(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zstd")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeSpool 辅助：把若干行写入指定块的 spool 文件，返回文件路径。
// 只用公开 API（ta.NewPaths/ta.Ref），因此不依赖实现内部字段。
func writeSpool(t *testing.T, root, dt, inst string, part int, lines [][]byte) string {
	t.Helper()
	path := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
```

A 层的 `internal/tracearchive/testhelpers_test.go` 是同样三个函数、但去掉 `ta.` 限定（`package tracearchive`）的版本；`writeSpool` 用 `NewPaths(root).SpoolFile(Ref{...})`。

**接口：**
- 依赖输入：
  - 任务 1：`defaultZstdLevel`、`defaultZstdWindowBytes`
  - 任务 2：`encodeTrailer`
  - 任务 3：`footer`、`encodeFooter`、`FooterSize`
  - 任务 4：`Ref`、`Paths`、`parsePartSeq`、`ErrPartExists`、`ErrRowCountMismatch`、`ErrSealFailed`
  - 任务 5：`SealTask`
- 对外产出：
  - `type SealerOptions struct { Root string; ZstdBinary string; ZstdLevel int; ZstdWindowBytes int }`——**最终实现另含两个字段（已落地在 `e0b0084f`）**：`MinAge time.Duration`（0 取 `defaultSealMinAge` = **3h**）与 `ExpectedZstdVersion string`（空取 `defaultZstdVersion` = `"1.4.8"`）
  - `type Sealer struct{...}`
  - `func NewSealer(opts SealerOptions) (*Sealer, error)`
  - `func (s *Sealer) Seal(ctx context.Context, task SealTask) error`
  - ~~`func ListPendingSeals(root string, minAge time.Duration) ([]SealTask, error)`~~ → **已更正（`e0b0084f`）为方法 `func (s *Sealer) ListPendingSeals() ([]SealTask, error)`**：扫描 `<root>/spool/dt=*/inst=*/part-*.jsonl`，返回 mtime 早于 `now-minAge` 的 spool（`minAge` 取自同一份 `SealerOptions`，默认 3h）。**包级形态已删除**：方法天然带上 `ZstdBinary`/`ExpectedZstdVersion`，计划 4 的循环构造一次 `Sealer` 即可发现并封块；包级签名允许 `root` 与 `minAge` 来自不同来源，会产生组合错误。**正文里凡出现 `ListPendingSeals(root, minAge)` 的测试片段，落盘时都要改成 `NewSealer(...)` + 调方法**（B 层经 `helpers_test.go` 的 `listPendingSeals(t, root, minAge)` 辅助）。

> **登记（Task 5 交付后追加）：行数交叉校验现已天然对齐，不要额外补偿。**
> 本任务的交叉校验是「spool 扫描出的行偏移条数 == `task.Rows`，不符则 `ErrRowCountMismatch` 拒绝封块」。在简报原文下（`AppendFailed` 只推进行号、不写字节）这个校验**必然失败**——因为 spool 的物理行数会比写入器推进的 `row_seq` 少，那是「产物格式要求行稠密而写侧稀疏」这同一矛盾的另一个表征，**不是需要在封块器里补的逻辑**。
> Task 5 改为写 `null` 标记行之后（见 Task 5 的 `AppendFailed` 与缺陷 D6），spool 的物理行数恒等于 `Rows`，该校验天然对齐：**封块器保持原样，不得为「跳号」添加任何补偿逻辑**（例如按 `row_seq` 填洞、跳过错位行、或放宽为 `len(offsets) <= task.Rows`——后者会把真正的截断/错位放过去，正是该校验要拦的）。

**方案 C 的编码方式（规格 §5.3.1.1、§7.2）——本任务与前五个版本最大的差异：**

| # | 约束 | 说明 |
|---|---|---|
| 1 | **用 `os/exec` 调 C 版 zstd CLI**：`zstd -19 --long=29 -T1 -c`，stdin = spool 文件，stdout = `.tmp` | **不得退回 `zstd.NewWriter`**：同数据/同级别/同窗口下 Go 库产物是 CLI 的 **1.90 倍**（384.0 MB vs 202.6 MB），验收线是 ≤1.30 倍 |
| 2 | `--long=<log2(窗口)>` 由 `defaultZstdWindowBytes` 换算（512 MiB ⇒ `29`） | 窗口的价值只有 CLI 的 `--long` 能兑现（规格 §5.3.1.1 结论 2） |
| 3 | **CLI 只产 zstd 帧**；trailer 与 footer **一律由 Go 侧追加** | 索引必须与内容出自同一次封块操作（I3） |
| 4 | **行偏移在喂给 CLI 之前**由 Go 侧顺序扫描 spool 采集 | CLI 是独立进程，Go 侧看不到压缩过程中的行边界；一次额外顺序读，成本可忽略 |
| 5 | **子进程非零退出 = 封块失败**：保留 spool、删除 `.tmp`、不产生产物、下轮重试 | 绝不接受「看起来有产物」的半成品（规格 §9.2） |
| 6 | **发现机制**：`ListPendingSeals` 按目录内容 + `mtime ≤ now-minAge` 判定「已停止追加」 | 网关无法给独立进程发信号；目录扫描幂等且可重启（规格 §7.2） |
| 7 | **zstd 版本必须锁定** | 本次标定用宿主机 v1.4.8；换版本须重新标定压缩比（规格 §11.1） |

**测试对子进程的两条依赖（写死在测试里）**：
- 真封块用例需要本机 `zstd`（**CI 镜像必须安装**）；缺失时 `t.Skip`，否则核心断言形同虚设。
- 失败语义与产物字节布局用例用**假 `zstd` 脚本**：`#!/bin/sh` + `exec cat`（直通，用于锁死布局）或 `exit 3`（用于锁死失败语义）。这样这两类断言不依赖真实 zstd 的版本。

> **落地修正（Task 6 交付后追加：与最终实现对齐，以代码为准）**
>
> **提交链**：`76042bcd`（首轮：C 版 zstd CLI + 行偏移扫描 + trailer/footer + 发现机制）→ `e0b0084f`（评审裁决落实：minAge 3h、版本探测、目录名校验、方法形态）→ `37f95c36`（第三轮裁决：删除版本探测记忆化、把解析校验做成可杀死的守卫）。
>
> **① `minAge` 默认 = 3 小时（`e0b0084f`）**：新增常量 `defaultSealMinAge`，**不再等于 `defaultSealInterval`（15 分钟）**。推导链（也写在常量注释上，便于审计）：实测摄取 ≈ 37 GiB/天 ≈ **1.54 GiB/h** → `defaultBlockBytes` = 1536 MiB 满流量下约 **1 小时**填满 → 网关滚动间隔应取 **~1h**（让**大小**阈值先触发，保住已标定的 10.26×；滚动过短会把块截成 ~400 MB，压缩比退到未标定区间）→ `minAge` 必须**严格大于**滚动间隔 ⇒ 3 × 1h = **3h**（3 倍余量）。成本：未封块 spool ≈ 3h × 1.54 GiB/h ≈ **4.6 GiB** 未压缩驻留，相对 `/datalog` 240 GiB 可忽略。**对端约束 R8**：网关滚动间隔 ≤ 1h（计划 2 落地）。现有 `defaultSealInterval = 15m` 已满足且对 3h 有 12 倍余量，故未改动 `defaults.go`；若计划 2 把它改到 > 1h，3h 就不再「严格大于」，必须同步调整。
>
> **② zstd 版本探测（`e0b0084f`）**：`SealerOptions.ExpectedZstdVersion`（空取 `"1.4.8"`）+ `verifyZstdVersion`。探测位置在**不依赖编码器的判定之后**（产物存在性检查、行数交叉校验）、**任何文件创建之前**：放在 `Seal` 最前面会让 `TestSealRefusesToOverwriteExistingArtifact` / `TestSealRejectsRowCountMismatch` 转红——**配错的编码器会掩盖这两类真实数据问题**（这是实现中一次真实的红→绿）。四条错误路径全部响亮失败、只用既有 `ErrSealFailed`：① 二进制缺失；② 探测非零退出（保留退出码）；③ 输出不可解析（含 `"1.5"`、`"1.5.7-beta"` 等非严格三段式）；④ 版本不符（错误里带上期望值与实际值）。**⚠️ 归一化的坑（D10）**：`exec.Command` 只对**不含路径分隔符**的 name 走 `LookPath`，绝对路径下 `cmd.Run()` 返回 `*fs.PathError(ENOENT)`，`%w` **换不来** `errors.Is(err, exec.ErrNotFound)`，必须显式归一化——简报的注释在这一点上是错的。**不做记忆化（`37f95c36`：已删除 `s.versionOK`，`Sealer` 没有运行期可变状态，只剩测试注入钩子）**：省下的只是一天几十次 `zstd --version`（约 5 ms/次，合计秒级），代价是给一个「看起来可共享的值类型」引入未同步的可变字段（并发调用 `Seal` 即数据竞争）。**错误文案是诊断接口**：「无法解析」与「版本不符」指向完全不同的处置（换构建 vs 重新标定），因此解析的三段式/数字校验被做成**可杀死的守卫**：`TestSealRejectsUnparsableZstdVersion` 含 4 个子用例（无版本号 / 两段 `"v1.5"` / 带后缀 `"v1.5.7-beta"` / 段内非数字），每个都断言错误文案含「无法解析」（放宽解析后文案会变成「版本不符」，用例即转红）。
>
> **③ 残行守卫（`76042bcd`）**：`scanSpoolOffsets` 把「spool 必须以换行符结尾」作为硬前置条件，遇未终止尾行 ⇒ 拒绝封块（保留 spool、删除 `.tmp`、不产生产物）。判据是「EOF 时仍带回了非空字节」——**0 字节空 spool 合法**（`row_count=0`，不得误判为残行）。理由：`Rows == 0` 的目录扫描路径**没有行数交叉校验可依赖**，这是那条路径**唯一的确定性保护**（D8：简报的扫描会把未终止尾行计作一行 ⇒ 残行被静默烤进产物）。
>
> **④ 目录名校验（`e0b0084f`）**：`ListPendingSeals` 用既有 `validateDT` 校验 `dt`、用白名单校验 `inst`（`blue`/`green`），非法即 `continue` **跳过该目录**——**不报错中断整轮扫描**（一个坏目录不该让当天的封块全停）。**⚠️ 理由已更正（Task 6 评审实测；完整更正见规格 §7.2）**：校验保留，但**理由不是防路径穿越**，而是**健壮性与对称性**——畸形目录名不应产出一个语义不通的 `SealTask`，且与写入侧的严格校验对称。原先写的「`filepath.Join` 的清理语义会把 `dt=..` / `inst=..` 带出 spool 子树」**不成立**：`filepath.Join(root, "dt=..", "inst=blue")` = `root/dt=../inst=blue`，**不折叠**（`dt=..` 是**字面目录名**而非父目录记号），且**目录名不可能含 `/`**。**精确表述（不得简化成「穿越根本不可能」）**：穿越在**一般情形**下确有可能——值里若含 `/../`，`Join` 会看到裸 `..` 元素并折叠（如 `dt="2026-09-24/../.."` ⇒ 产物落到 `root` **之外**；`Inst="../../green"` ⇒ 折叠成 `root/dt=X/green`）；但本路径的 `dt`/`inst` 取自目录名（不含 `/`），故不成立。**写入侧（`WriterOptions.Inst` 与 `Append` 的 `dt`）是调用方输入、可以含 `/`，那里的校验按路径分量理解成立，本节不改动。** 该结论**不是纯语义推导**：见缺陷登记 **D15** 的隔离探针 **B'**——只摘 `validateDT` 后**真的在 `root` 之外创建了** `…/T/x/inst=blue`。
>
> **⑤ API 形态**：只保留方法 `(*Sealer).ListPendingSeals()`，**删除包级函数**（上文的接口登记已更正）。方法形态也让「版本期望值/`MinAge`」随实例一起传递。
>
> **⑥ 另两处落地修正**：`MkdirAll` 提到创建 `.tmp` **之前**（`.tmp` 必须与产物同目录才能 `rename`；否则 10 条真封块用例全部 ENOENT，见 D9）；golden 自检的偏移改用格式契约表达 `want[footerStart+22:footerStart+30]`（`footerStart := len(want) - FooterSize`），简报的 `want[32:40]` **偏移 3 字节**且恒不成立（D11）。
>
> **⑦ 最终用例规模与证据**：**A 层 2 条**（`sealer_internal_test.go`：`TestSealIsAtomicOnCrash`、`TestParsePartSeqUsedBySealer`）**+ B 层 27 条**（`sealer_test.go`）**＝ 29 条顶层用例**（按本文的计数口径：用例数 = 顶层测试函数数，与 `grep -c '^func Test'` 一致；**29 是两层合计，不是 B 层**——原写「B 层 29 条」有误，已更正）。B 层 27 条含 5 条版本/目录名守卫用例：`TestSealRejectsMismatchedZstdVersion`、`TestSealRejectsUnparsableZstdVersion`、`TestSealAcceptsConfiguredZstdVersion`、`TestSealRejectsFailedVersionProbe`、`TestListPendingSealsRejectsMalformedDirNames`；**另有 12 条子用例**（单独计数，不计入 29）；真封块用例 `SKIP = 0`。变异自检见 Task 6 报告 §5.1/§10.6，**且每一次变异运行同样必须满足本文全局约束的三条断言与「无用例执行 = 无效」判据**（那一条本轮才扩充，原报告未采用）。所有 RED/GREEN 证据必须满足全局约束的 `-run` 三条要求（顶层名字集合 `tr -d '[:space:]'` 后 diff 为空 / 顶层 PASS 行数相符 / `SKIP == 0`）。

- [ ] **步骤 1：编写失败的测试**

新建 `tests/tracearchive/sealer_test.go`——**B 层，`package tracearchive_test`**（辅助函数在 `helpers_test.go`，见上）：
```go
// tests/tracearchive/sealer_test.go
package tracearchive_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 阅读约定：本块后面的代码按 package tracearchive 书写以省篇幅；
// 落盘时全部导出标识符加 ta. 前缀（NewSealer→ta.NewSealer、SealerOptions→ta.SealerOptions、
// SealTask→ta.SealTask、Ref→ta.Ref、NewPaths→ta.NewPaths、ErrPartExists→ta.ErrPartExists、
// ErrRowCountMismatch、ErrSealFailed 同理）。漏加会编译失败，自动可查。

func TestSealProducesArtifactAndRemovesSpool(t *testing.T) {
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("c")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, err := NewSealer(SealerOptions{Root: root, ZstdBinary: bin})
	if err != nil {
		t.Fatalf("建封块器失败: %v", err)
	}
	task := SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0,
		SpoolPath: spool, Rows: len(lines),
	}
	if err := s.Seal(context.Background(), task); err != nil {
		t.Fatalf("封块失败: %v", err)
	}

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
	// I5: 删 spool 必须发生在产物 rename 之后
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatal("封块成功后 spool 应被删除")
	}
	// I2: 不得残留 .tmp
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("不应残留 .tmp 文件")
	}
}

func TestSealRefusesToOverwriteExistingArtifact(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 3, lines)

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 3})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("已有产物，不可覆盖"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 本用例不需要 zstd：产物存在性检查发生在压缩之前
	s, _ := NewSealer(SealerOptions{Root: root})
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 3, SpoolPath: spool, Rows: 1,
	})
	if !errors.Is(err, ErrPartExists) {
		t.Fatalf("产物已存在时必须返回 ErrPartExists（宁可留下 spool，不可覆盖唯一副本），实际 %v", err)
	}
	// 原产物必须原封不动
	got, _ := os.ReadFile(partPath)
	if string(got) != "已有产物，不可覆盖" {
		t.Fatal("拒绝封块后原产物不应被改动")
	}
	// spool 必须保留
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("拒绝封块后 spool 应保留以便下轮重试")
	}
	// 不得留下 .tmp
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("拒绝封块不应产生 .tmp")
	}
}

// TestSealRejectsRowCountMismatch 覆盖「spool 行数与调用方记录不符」守卫。
// 该检查在启动子进程之前完成，因此本用例不需要 zstd。
func TestSealRejectsRowCountMismatch(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root})
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 3, // 实际 2 行
	})
	if !errors.Is(err, ErrRowCountMismatch) {
		t.Fatalf("行数不符应返回 ErrRowCountMismatch，实际 %v", err)
	}
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("行数不符时不得产生产物")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("行数不符时不得残留 .tmp（检查发生在压缩之前）")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("行数不符时必须保留 spool")
	}
}

// decodeVarints 在测试里**独立实现一遍** trailer 的差分 varint 解码。
//
// 为什么不用实现里的 decodeTrailer：一是它是未导出符号（B 层拿不到），
// 二是独立实现一遍本身就是更强的断言——若实现的 varint 编码写错，
// 这里的解码就对不上，而不是「自己验自己」。
func decodeVarints(b []byte, n int) ([]int64, error) {
	out := make([]int64, 0, n)
	var prev int64
	for i := 0; i < n; i++ {
		var (
			shift uint
			val   uint64
		)
		for {
			if len(b) == 0 {
				return nil, errors.New("varint 流提前结束")
			}
			c := b[0]
			b = b[1:]
			val |= uint64(c&0x7F) << shift
			if c < 0x80 {
				break
			}
			shift += 7
			if shift > 63 {
				return nil, errors.New("varint 过长")
			}
		}
		prev += int64(val)
		out = append(out, prev)
	}
	if len(b) != 0 {
		return nil, errors.New("trailer 存在多余字节")
	}
	return out, nil
}

// TestSealFooterPointsAtValidTrailer 只经**公开格式契约**（规格 §5.2）验证
// 「footer 指向的 trailer 能解出正确的行偏移」，不调用任何未导出符号。
func TestSealFooterPointsAtValidTrailer(t *testing.T) {
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("cc")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: bin})
	if err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < FooterSize {
		t.Fatalf("产物只有 %d 字节，装不下 %d 字节 footer", len(b), FooterSize)
	}

	// 按规格 §5.2 逐字段解析末尾 32 字节（外部可见的格式契约）
	fb := b[len(b)-FooterSize:]
	if string(fb[0:4]) != "TBA4" {
		t.Fatalf("magic 应为 TBA4，实际 %q", fb[0:4])
	}
	if v := binary.BigEndian.Uint16(fb[4:6]); v != 1 {
		t.Fatalf("format_version 应为 1，实际 %d", v)
	}
	trailerOff := int64(binary.BigEndian.Uint64(fb[6:14]))
	trailerLen := int64(binary.BigEndian.Uint32(fb[14:18]))
	rowCount := int32(binary.BigEndian.Uint32(fb[18:22]))

	// trailer 必须紧邻 footer 之前、且不越界
	if trailerOff < 0 || trailerOff+trailerLen != int64(len(b)-FooterSize) {
		t.Fatalf("trailer 应紧邻 footer 之前: off=%d len=%d footerStart=%d",
			trailerOff, trailerLen, len(b)-FooterSize)
	}
	if int(rowCount) != len(lines) {
		t.Fatalf("footer 的 row_count 应为 %d，实际 %d", len(lines), rowCount)
	}

	tb := append([]byte(nil), b[trailerOff:trailerOff+trailerLen]...)
	offs, err := decodeVarints(tb, len(lines))
	if err != nil {
		t.Fatalf("trailer 无法按公开格式解出 %d 条: %v", len(lines), err)
	}
	// 索引偏移必须精确指向各行首字节（未压缩偏移，与 zstd 产物无关）
	cum := int64(0)
	for i, l := range lines {
		if offs[i] != cum {
			t.Fatalf("第 %d 行偏移应 %d，实际 %d", i, cum, offs[i])
		}
		cum += int64(len(l)) + 1
	}
}

// TestSealFailsOnNonZeroExit 是方案 C 的核心失败语义：
// 子进程非零退出必须被当成封块失败——保留 spool、清理 .tmp、不产生产物。
// 用假 zstd 脚本（exit 3）固定这条断言，不依赖真实 zstd。
func TestSealFailsOnNonZeroExit(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exit 3\n")})
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	})
	if !errors.Is(err, ErrSealFailed) {
		t.Fatalf("子进程非零退出应返回 ErrSealFailed，实际 %v", err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("错误链里应能取到退出码 3（这要求用 %%w 包装），实际 %v", err)
	}

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("子进程失败时不得产生产物")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("子进程失败时应清理 .tmp")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("子进程失败时必须保留 spool 以便下轮重试")
	}
}

// TestSealFailsWhenZstdMissing 覆盖「zstd 二进制不存在」（部署事故）：
// 必须报错并保留 spool，而不是静默产出一个空/残缺产物。
func TestSealFailsWhenZstdMissing(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa")})

	missing := filepath.Join(t.TempDir(), "definitely-not-zstd")
	s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: missing})
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 1,
	})
	if !errors.Is(err, ErrSealFailed) {
		t.Fatalf("zstd 缺失应返回 ErrSealFailed，实际 %v", err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("错误链里应保留 exec.ErrNotFound（这要求用 %%w 包装），实际 %v", err)
	}
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("zstd 缺失时不得产生产物")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("zstd 缺失时必须保留 spool")
	}
}

// TestSealGoldenLayout 把【产物整体布局】逐字节钉死：帧 | trailer | footer。
//
// 为什么必须有它：帧 + trailer + footer 的拼接关系目前只由「解回来一样」保护，
// 一个对称的布局 bug（例如把 trailer 写进帧之前的位置但读端同样处理）会全绿。
// 这里用「直通」假 zstd（exec cat）让帧字节完全可控，因此整个产物可以逐字节比对，
// 且不依赖真实 zstd 的版本。
func TestSealGoldenLayout(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aa"), []byte("bbbb"), []byte("c")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, err := NewSealer(SealerOptions{Root: root, ZstdBinary: fakeZstd(t, "exec cat\n")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatal(err)
	}

	want := []byte{
		// ① zstd 帧（此处 = spool 原样，共 10 字节）"aa\nbbbb\nc\n"
		0x61, 0x61, 0x0A, 0x62, 0x62, 0x62, 0x62, 0x0A, 0x63, 0x0A,
		// ② trailer：行偏移 0/3/8 → 差分 0/3/5（各 1 字节 varint）
		0x00, 0x03, 0x05,
		// ③ footer：32 字节定长
		0x54, 0x42, 0x41, 0x34, // magic "TBA4"
		0x00, 0x01, // format_version = 1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A, // trailer_offset = 10
		0x00, 0x00, 0x00, 0x03, // trailer_len = 3
		0x00, 0x00, 0x00, 0x03, // row_count = 3
		// frame_sha256("aa\nbbbb\nc\n")[:8]
		0xE3, 0x76, 0xD8, 0xF8, 0xFF, 0x1F, 0x11, 0xE2,
		0x00, 0x00, // reserved
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("产物逐字节不符（长度 %d vs %d）:\n     want % x\n     got  % x", len(want), len(got), want, got)
	}
	// 交叉验证 sha 那一格：它必须等于帧字节的 sha256 前 8 字节
	sum := sha256.Sum256(want[:10])
	if !bytes.Equal(sum[:8], want[32:40]) {
		t.Fatalf("golden 自身不自洽：帧的 sha256[:8] = % x", sum[:8])
	}
}

// TestListPendingSealsSkipsFreshFiles：刚写完的 spool 不能被判定为「已停止追加」。
// 这是「不能封到正在写的 spool」的第一道保证。
func TestListPendingSealsSkipsFreshFiles(t *testing.T) {
	root := t.TempDir()
	writeSpool(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x")})

	tasks, err := ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("刚写入的 spool 不应进入待封块列表，实际 %d 个: %+v", len(tasks), tasks)
	}
}

// TestListPendingSealsReturnsStaleFiles：mtime 早于 now-minAge 才返回。
func TestListPendingSealsReturnsStaleFiles(t *testing.T) {
	root := t.TempDir()
	spool := writeSpool(t, root, "2026-09-24", "blue", 7, [][]byte{[]byte("x"), []byte("y")})
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(spool, old, old); err != nil {
		t.Fatal(err)
	}

	tasks, err := ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应返回 1 个待封块任务，实际 %d", len(tasks))
	}
	got := tasks[0]
	if got.DT != "2026-09-24" || got.Inst != "blue" || got.Part != 7 || got.SpoolPath != spool {
		t.Fatalf("SealTask 分量不符: %+v", got)
	}
	if got.Rows != 0 {
		t.Fatalf("扫目录时行数未知，Rows 应为 0（由 Seal 扫描后自证），实际 %d", got.Rows)
	}
	if want := int64(len("x\ny\n")); got.UncompressedBytes != want {
		t.Fatalf("UncompressedBytes 应为文件大小 %d，实际 %d", want, got.UncompressedBytes)
	}

	// 幂等且可重启：再扫一次结果相同（纯读，不修改任何状态）
	again, err := ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].SpoolPath != got.SpoolPath {
		t.Fatalf("重复扫描应得到同一结果（幂等），实际 %+v", again)
	}
}

// TestListPendingSealsIgnoresIllegalNames：.tmp、产物名、非 part- 前缀一律不进入列表。
func TestListPendingSealsIgnoresIllegalNames(t *testing.T) {
	root := t.TempDir()
	dir := NewPaths(root).SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{
		"part-000001.jsonl.tmp",   // rename 前的半成品
		"part-000002.jsonl.zst",   // 产物名混进了 spool 目录
		"index.jsonl",             // 非 part- 前缀
		"part-abc.jsonl",          // 数字段非法
		"part-000003.jsonl.gz",    // 其他压缩扩展名
	} {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := ListPendingSeals(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("非法文件名不应进入待封块列表，实际 %+v", tasks)
	}
}
```

**A 层测试文件**（`internal/tracearchive/sealer_internal_test.go`，`package tracearchive`）——同样写在步骤 1 里：

```go
// internal/tracearchive/sealer_internal_test.go
package tracearchive

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 【为什么破例放包内】本用例靠注入 s.failBeforeRename 制造「rename 之前一步失败」，
// 从而在不真的崩溃的前提下验证 I2（产物只经 rename 出现）。该钩子刻意不导出
// （规格不要求它成为生产 API），因此外部测试包无法表达这条断言。
func TestSealIsAtomicOnCrash(t *testing.T) {
	// 模拟压缩中途崩溃：产物目录不得出现半成品
	bin := requireZstd(t)
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: bin})
	// 注入错误：让 rename 之前的一步失败
	s.failBeforeRename = func() error { return os.ErrPermission }
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 2,
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("注入的失败应被原样传播（errors.Is 必须可识别），实际 %v", err)
	}
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("失败时不得出现产物文件")
	}
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("失败时应清理 .tmp")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("失败时 spool 必须保留以便重试")
	}
}

func TestParsePartSeqUsedBySealer(t *testing.T) {
	// 封块器同样依赖严格的块名解析，此处交叉验证一次
	if _, ok := parsePartSeq("part-000000.jsonl.zst.tmp"); ok {
		t.Fatal(".tmp 不应被当作合法产物名")
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestSealProducesArtifactAndRemovesSpool|TestSealRefusesToOverwriteExistingArtifact|TestSealRejectsRowCountMismatch|TestSealFooterPointsAtValidTrailer|TestSealIsAtomicOnCrash|TestSealFailsOnNonZeroExit|TestSealFailsWhenZstdMissing|TestSealGoldenLayout|TestListPendingSealsSkipsFreshFiles|TestListPendingSealsReturnsStaleFiles|TestListPendingSealsIgnoresIllegalNames|TestParsePartSeqUsedBySealer' -v`
预期：**两个包**都编译失败——`./tests/tracearchive/` 提示 `undefined: ta.NewSealer`、`undefined: ta.SealerOptions`、`undefined: ta.ListPendingSeals`；`./internal/tracearchive/` 提示 `s.failBeforeRename undefined`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/sealer.go`：

```go
package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SealerOptions 是封块器配置。
type SealerOptions struct {
	Root            string
	ZstdBinary      string // zstd 可执行文件；空则回退到 "zstd"（部署项，规格 §11.1）
	ZstdLevel       int    // 0 取 defaultZstdLevel
	ZstdWindowBytes int    // 0 取 defaultZstdWindowBytes；换算为 CLI 的 --long=<log2(窗口)>
}

// Sealer 把 spool 明文封块为带内嵌索引的产物文件。
//
// 【方案 C】编码由【C 版 zstd CLI 子进程】完成，不是 Go 库（规格 §5.3.1.1）：
// 同数据、同级别、同窗口下 Go 库产物是 CLI 的 1.90 倍（384.0 MB vs 202.6 MB），
// 且 Go 库峰值 RSS ≈ 窗口 × 2.08，CLI ≈ × 1.08。由此编码器内存完全不落在
// 网关的 2 GiB cgroup 内 —— sealer 是独立进程，跑在宿主机上。
//
// Go 侧只负责：扫描 spool 采集行偏移 → 把 spool 喂给 CLI → 追加 trailer/footer
// → fsync → rename → 删 spool。
// **trailer 与 footer 永远由 Go 侧追加，CLI 只产 zstd 帧。**
type Sealer struct {
	opts  SealerOptions
	paths Paths

	// failBeforeRename 仅供测试注入故障，生产恒为 nil。
	failBeforeRename func() error
}

// NewSealer 构造封块器。
func NewSealer(opts SealerOptions) (*Sealer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdBinary == "" {
		opts.ZstdBinary = "zstd"
	}
	if opts.ZstdLevel <= 0 {
		opts.ZstdLevel = defaultZstdLevel
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if _, err := zstdLongArg(opts.ZstdWindowBytes); err != nil {
		return nil, err
	}
	return &Sealer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Seal 执行一次封块。任何一步失败都保留 spool 并返回错误，绝不留下半个产物。
//
// 失败语义（规格 §7.2、§9.2）：
//   - 产物已存在           → ErrPartExists（幂等，拒绝覆盖）
//   - spool 行数与记录不符  → ErrRowCountMismatch（保留 spool）
//   - 子进程非零退出/缺失   → ErrSealFailed（保留 spool、删 .tmp）
func (s *Sealer) Seal(ctx context.Context, task SealTask) error {
	partPath := s.paths.PartFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})

	// I4: 同一个 (dt, inst, part) 只允许封块一次。
	// 若产物已存在，宁可留下 spool 也不能覆盖——那是唯一副本。
	if _, err := os.Stat(partPath); err == nil {
		return fmt.Errorf("%w: %s", ErrPartExists, partPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	// ① 先顺序扫描 spool，采集每行首字节的未压缩偏移。
	// 方案 C 下 CLI 是独立进程，Go 侧看不到压缩过程中的行边界，
	// 因此在喂数据之前先扫一遍（一次顺序读，成本可忽略）。
	offsets, _, err := scanSpoolOffsets(task.SpoolPath)
	if err != nil {
		return err
	}
	// ② 行数交叉校验放在启动子进程之前：不匹配就不必浪费一次压缩。
	// task.Rows == 0 表示调用方（ListPendingSeals 扫目录）不知道行数。
	if task.Rows > 0 && len(offsets) != task.Rows {
		return fmt.Errorf("%w: 扫描=%d 调用方=%d（拒绝封块，保留 spool）",
			ErrRowCountMismatch, len(offsets), task.Rows)
	}

	tmpPath := partPath + ".tmp"
	frameSHA, err := s.runZstd(ctx, task.SpoolPath, tmpPath)
	if err != nil {
		// 子进程非零退出 / 无法启动 / 压缩 I/O 失败：
		// 清理 .tmp、保留 spool、不产生产物（规格 §9.2）。
		os.Remove(tmpPath)
		return err
	}

	// ③ 帧已就绪，此刻追加 trailer 与 footer（CLI 只写了帧）
	if err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if s.failBeforeRename != nil {
		if err := s.failBeforeRename(); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	// I2: 产物只能通过 rename 出现，永不出现半个文件
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: 建产物目录失败: %w", err)
	}
	if err := os.Rename(tmpPath, partPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: rename 失败: %w", err)
	}
	if err := fsyncDir(filepath.Dir(partPath)); err != nil {
		return fmt.Errorf("tracearchive: fsync 目录失败: %w", err)
	}

	// I5: 到此才允许删 spool
	if err := os.Remove(task.SpoolPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 删除 spool 失败: %w", err)
	}
	return nil
}

// scanSpoolOffsets 顺序扫描 spool，返回每行首字节的未压缩偏移与总字节数。
//
// 索引记录的是「行首在未压缩流中的位置」。方案 C 下这份偏移必须在喂给 CLI
// 之前采集（见 Seal 注释），且必须与实际喂进去的字节流完全一致——
// 因此这里逐字节累加，不做任何行内改写。
func scanSpoolOffsets(path string) ([]int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	var offsets []int64
	var off int64
	for {
		line, readErr := br.ReadBytes('\n')
		if len(line) > 0 {
			offsets = append(offsets, off)
			off += int64(len(line))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
		}
	}
	return offsets, off, nil
}

// zstdLongArg 把窗口字节数换算为 CLI 的 --long=<log2(窗口)>。
// 512 MiB = 2^29 ⇒ "29"。zstd 的 --long 语义要求窗口是 2 的幂，故此处严格校验。
func zstdLongArg(windowBytes int) (string, error) {
	if windowBytes <= 0 || windowBytes&(windowBytes-1) != 0 {
		return "", fmt.Errorf("tracearchive: zstd 窗口必须是 2 的幂（实际 %d 字节）", windowBytes)
	}
	return strconv.Itoa(bits.Len(uint(windowBytes)) - 1), nil
}

// runZstd 用 C 版 zstd CLI 把 spoolPath 压缩为 tmpPath，
// 返回 zstd 帧字节的 sha256 前 8 字节。
//
// 命令形如：zstd -19 --long=29 -T1 -c
//
//	-19        级别 19（规格 §5.3.1.1：Go 库 19 与 22 产物相同，22 只多耗 CPU）
//	--long=29  窗口 512 MiB —— 与 Go 库的唯一实质差异（长距离匹配器）
//	-T1        单线程：多并发会把流切成独立块，显著削弱压缩比
//	-c         写 stdout（不让 zstd 自己命名文件）
//
// 退出码非 0 一律视为封块失败（ErrSealFailed），绝不接受「看起来有产物」的半成品。
func (s *Sealer) runZstd(ctx context.Context, spoolPath, tmpPath string) ([8]byte, error) {
	var empty [8]byte

	long, err := zstdLongArg(s.opts.ZstdWindowBytes)
	if err != nil {
		return empty, err
	}

	src, err := os.Open(spoolPath)
	if err != nil {
		return empty, fmt.Errorf("%w: 打开 spool 失败: %w", ErrSealFailed, err)
	}
	defer src.Close()

	// O_RDWR：子进程作为 stdout 写入，随后 Go 侧还要 seek 回起点算帧的 sha256
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return empty, fmt.Errorf("%w: 建临时产物失败: %w", ErrSealFailed, err)
	}
	defer dst.Close()

	args := []string{"-" + strconv.Itoa(s.opts.ZstdLevel), "--long=" + long, "-T1", "-c"}
	cmd := exec.CommandContext(ctx, s.opts.ZstdBinary, args...)
	cmd.Stdin = src  // 直接把文件交给子进程，不经内存中转
	cmd.Stdout = dst // 只写 zstd 帧
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, fmt.Errorf("%w: 封块上下文取消: %w", ErrSealFailed, ctxErr)
		}
		return empty, fmt.Errorf("%w: zstd 子进程失败（args=%v）: %w（stderr: %s）",
			ErrSealFailed, args, err, strings.TrimSpace(stderr.String()))
	}

	// 此刻 .tmp 的内容即纯 zstd 帧（trailer/footer 尚未追加），据此计算 frame_sha256
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return empty, fmt.Errorf("%w: 回绕临时产物失败: %w", ErrSealFailed, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, dst); err != nil {
		return empty, fmt.Errorf("%w: 计算帧摘要失败: %w", ErrSealFailed, err)
	}
	var sum [8]byte
	copy(sum[:], hasher.Sum(nil)[:8])
	return sum, nil
}

// appendTrailerAndFooter 在临时产物末尾追加索引与定长 footer，并 fsync。
//
// 【方案 C 的关键点】CLI 只写了 zstd 帧，索引与 footer 全部在这里由 Go 侧追加，
// 保证索引与内容出自同一次封块操作（I3）。
func (s *Sealer) appendTrailerAndFooter(tmpPath string, offsets []int64, frameSHA [8]byte) error {
	trailer := encodeTrailer(offsets)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("tracearchive: 打开临时产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	trailerOffset := info.Size()

	if _, err := f.Write(trailer); err != nil {
		return fmt.Errorf("tracearchive: 写 trailer 失败: %w", err)
	}
	footerBytes := encodeFooter(footer{
		TrailerOffset: trailerOffset,
		TrailerLen:    int32(len(trailer)),
		RowCount:      int32(len(offsets)),
		FrameSHA:      frameSHA,
	})
	if _, err := f.Write(footerBytes); err != nil {
		return fmt.Errorf("tracearchive: 写 footer 失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("tracearchive: fsync 临时产物失败: %w", err)
	}
	return nil
}

// fsyncDir 目录 fsync：保证 rename 本身落盘，否则崩溃后可能看不到产物。
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ListPendingSeals 扫描 <root>/spool/dt=*/inst=*/part-*.jsonl，
// 返回 mtime 早于 now-minAge 的 spool（即「已停止追加」的块）。
//
// 为什么用 mtime + minAge 而不是信号（规格 §7.2）：sealer 是独立进程，
// 网关无法给它发信号，而目录扫描不需要任何进程间通信，并且【幂等且可重启】——
// 任何一轮崩溃后，下一轮扫描会重新发现同一个 spool 并重试
//（产物已存在则由 Seal 以 ErrPartExists 拒绝覆盖）。
//
// 纯函数：只读文件系统元信息，不修改任何状态，因此可直接单测。
// 调用方必须保证 minAge 远大于网关的 flush 周期（规格 §7.1），
// 否则会把「缓冲还没落盘的活跃 spool」误判为「已停止追加」并封出残缺产物。
func ListPendingSeals(root string, minAge time.Duration) ([]SealTask, error) {
	spoolRoot := filepath.Join(root, "spool")
	cutoff := time.Now().Add(-minAge)

	dts, err := os.ReadDir(spoolRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 还没写过任何正文
		}
		return nil, fmt.Errorf("tracearchive: 读 spool 根目录失败: %w", err)
	}

	var out []SealTask
	for _, dtEnt := range dts {
		if !dtEnt.IsDir() || !strings.HasPrefix(dtEnt.Name(), "dt=") {
			continue
		}
		dt := strings.TrimPrefix(dtEnt.Name(), "dt=")
		dtDir := filepath.Join(spoolRoot, dtEnt.Name())

		insts, err := os.ReadDir(dtDir)
		if err != nil {
			return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dtDir, err)
		}
		for _, instEnt := range insts {
			if !instEnt.IsDir() || !strings.HasPrefix(instEnt.Name(), "inst=") {
				continue
			}
			inst := strings.TrimPrefix(instEnt.Name(), "inst=")
			dir := filepath.Join(dtDir, instEnt.Name())

			ents, err := os.ReadDir(dir)
			if err != nil {
				return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dir, err)
			}
			for _, e := range ents {
				// 只认 spool 名 part-NNNNNN.jsonl；
				// .tmp、产物名（.jsonl.zst）、非 part- 前缀一律忽略。
				if !strings.HasSuffix(e.Name(), ".jsonl") {
					continue
				}
				part, ok := parsePartSeq(e.Name())
				if !ok {
					continue
				}
				info, err := e.Info()
				if err != nil {
					return nil, fmt.Errorf("tracearchive: stat %s 失败: %w", e.Name(), err)
				}
				if info.ModTime().After(cutoff) {
					continue // 仍在追加（或刚写过），不碰
				}
				out = append(out, SealTask{
					DT:                dt,
					Inst:              inst,
					Part:              part,
					SpoolPath:         filepath.Join(dir, e.Name()),
					UncompressedBytes: info.Size(),
					// Rows 留 0：扫目录时不知道行数，交给 Seal 扫描后自证
				})
			}
		}
	}
	// 稳定顺序（dt/inst/part），便于测试断言与日志排查
	sort.Slice(out, func(i, j int) bool {
		if out[i].DT != out[j].DT {
			return out[i].DT < out[j].DT
		}
		if out[i].Inst != out[j].Inst {
			return out[i].Inst < out[j].Inst
		}
		return out[i].Part < out[j].Part
	})
	return out, nil
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestSealProducesArtifactAndRemovesSpool|TestSealRefusesToOverwriteExistingArtifact|TestSealRejectsRowCountMismatch|TestSealFooterPointsAtValidTrailer|TestSealIsAtomicOnCrash|TestSealFailsOnNonZeroExit|TestSealFailsWhenZstdMissing|TestSealGoldenLayout|TestListPendingSealsSkipsFreshFiles|TestListPendingSealsReturnsStaleFiles|TestListPendingSealsIgnoresIllegalNames|TestParsePartSeqUsedBySealer' -v`
预期：两个包都 PASS（真 zstd 用例在装有 zstd 的机器上通过；假 zstd 用例与 `ListPendingSeals` 用例在任何机器上通过）。

**变异自检（逐条做，这是「每道防线都要有测试」的判据）：**
- 把 `exec.CommandContext(...)` 换回 `zstd.NewWriter` → 压缩比类断言不会红（本任务不测比值），**因此必须另有一条人工检查**：`grep -n "zstd.NewWriter" internal/tracearchive/sealer.go` 必须**无输出**（sealer 不得用 Go 库编码）。
- 忽略 `cmd.Run()` 的 error（当成成功）→ `TestSealFailsOnNonZeroExit` / `TestSealFailsWhenZstdMissing` 必须转红。
- 把 `%w` 改成 `%v`（`ErrSealFailed` / `ErrPartExists` / `ErrRowCountMismatch`、或子进程原始 error）→ 对应的 `errors.Is` / `errors.As` 断言必须转红。
- 先跑 CLI 再写 trailer/footer 的顺序若被颠倒（即把 trailer 写进帧之前）→ `TestSealGoldenLayout` 与 `TestSealFooterPointsAtValidTrailer` 必须转红。
- 把 `info.ModTime().After(cutoff)` 判断删掉 → `TestListPendingSealsSkipsFreshFiles` 必须转红。
- 去掉 `.jsonl` 后缀过滤 → `TestListPendingSealsIgnoresIllegalNames` 必须转红。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/sealer.go internal/tracearchive/sealer_internal_test.go \
        internal/tracearchive/testhelpers_test.go \
        tests/tracearchive/sealer_test.go tests/tracearchive/helpers_test.go
git commit -m "feat(tracearchive): 封块器改用 C 版 zstd CLI（方案 C）

- 编码走 os/exec 子进程：zstd -19 --long=29 -T1 -c，不用 zstd.NewWriter
  （同参数下 Go 库产物是 CLI 的 1.90 倍，不达验收线；规格 §5.3.1.1）
- CLI 只产 zstd 帧，trailer/footer 一律由 Go 侧追加；行偏移在喂给 CLI 前
  由 Go 侧顺序扫描 spool 采集（规格 §7.2）
- 子进程非零退出/二进制缺失 = 封块失败：保留 spool、删 .tmp、不产生产物
- 新增纯函数 ListPendingSeals：按目录内容 + mtime/minAge 发现待封块（幂等可重启）
- 产物整体布局与失败语义各有测试；含 golden 逐字节布局断言
- 测试分层：公开 API 用例在 tests/tracearchive/，崩溃注入（未导出的
  failBeforeRename）与 parsePartSeq 交叉验证留在包内"
```

### Task 7：按定位键点读（含 spool 兜底）

**文件：**
- 新建：`internal/tracearchive/reader.go`
- 新建：`tests/tracearchive/reader_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**：`NewReader`/`ReaderOptions`/`GetBody`/`Close`/`FooterSize`/`ErrNotArchived`/`ErrBodyMissing`/`ErrFooterCorrupt` 全部导出。本层放：逐字节取回、spool 兜底、**标记行 ⇒ `ErrBodyMissing`（产物与 spool 两条路径）**、错误分类（产物内越界 + spool 内越界）、footer 篡改、并发上限、无临时文件。
- 追加：`tests/tracearchive/helpers_test.go`——**B 层辅助**，新增 `sealLines`（复用任务 6 已有的 `writeSpool`/`requireZstd`）。
- 追加：`internal/tracearchive/testhelpers_test.go`——**A 层辅助**，同样新增一份 `sealLines`（不带 `ta.` 限定），供本任务的 A 层用例使用。两份同名同实现、**故意重复**。
- 新建：`internal/tracearchive/reader_internal_test.go`——**A 层（`package tracearchive`）**：`TestReaderRejectsOversizedTrailerLen` 与 `TestReaderReleasesConcurrencySlots`。它必须引用**未导出的 `maxTrailerBytes`**（边界值就是它本身，不能内联字面量）并要用未导出的 `footer`/`encodeFooter` 构造「自洽但越界」的损坏产物 ⇒ 外部包无法表达。

**接口：**
- 依赖输入：任务 3（`FooterSize`、`decodeFooter`）、任务 2（`decodeTrailer`）、任务 4（`Ref`、`Paths`、错误变量）、任务 6（产物由 Sealer 生成）
- 对外产出：
  - `type ReaderOptions struct { Root string; ZstdWindowBytes int; MaxConcurrent int }`
  - `type Reader struct{...}`
  - `func NewReader(opts ReaderOptions) (*Reader, error)`
  - `func (r *Reader) GetBody(ctx context.Context, ref Ref) ([]byte, error)`——返回**不含换行**的原始行字节
    **⚠️ 遇到标记行必须返回 `ErrBodyMissing`（规格 §8.2）**：产物路径与 spool 路径**都要**识别 Task 5 的 `null` 标记行，不得把字符串 `"null"` 当正文递出去。读取侧是逐字返回行内容、不做 JSON 校验的，少了这道识别就会把「正文从未落盘」伪装成「正文就是 `null`」——与 `body_missing` 的 404 语义直接冲突。标记行的判据直接复用 Task 5 的未导出变量 `failedRowMarker`（同一个包，两边共用一份定义，不可能不同步）。
  - `func (r *Reader) Close() error`
  - `const maxTrailerBytes = 64 << 20`——trailer 大小的合理性上界；`TrailerLen` 超过它一律按 `ErrFooterCorrupt` 处理（防止损坏的 footer 让读取端分配数 GB 并 OOM）
  - **`const maxTrailerEntries = maxTrailerBytes / 8`（= 8,388,608）——trailer **条目数**（= 块内行数）的上界，`n` 超过它一律按 `ErrTrailerCorrupt` 处理。**字节侧上界不够**：`decodeTrailer` 的分配是 `8 × n`，而最坏情况下 trailer 的**每个字节**都编码出一个条目 ⇒ `n` 可达 `len(b)` = 64 Mi 条。**这是修复轮 F1 补入的一半**（实测：64 MiB 全 `0x00` 的 trailer ⇒ `n = 67,108,864`、`make` 分配 512 MiB、`decodeTrailer` **`err=nil` 成功返回**）；判定在 `decodeTrailer` 内、**在 `make` 之前**、**绝不截断**（D30、`r11-report.md` §7.9）

> **⚠️ R10：本任务必须额外实现的三项读取侧要求（`internal/tracearchive/reader.go` 尚未落地，以下是「要求 / 待落地」，**不是已完成的现状**；规格 §8.2）**
>
> 1. **`readFromSpool` 必须拒绝未以换行符结尾的末行**，**不得** trim 掉残字节后当正文返回。这是**真正的静默错正文路径**（封块侧的残行守卫只是响亮拒绝）：若末行不完整而写入器在其后继续追加，残行占掉一个行位，其后所有行整体错位 ⇒ `row=N` 静默读成第 `N+1` 行。判据与封块侧同源（「EOF 时仍带回了非空字节」；0 字节 spool 是合法状态）。**下文的 `readFromSpool` 代码块尚未包含该守卫，落盘时必须补上。**
> 2. **读取产物时必须校验 footer 的 `frame_sha256[:8]`**：重算 zstd 帧字节（`[0, trailer_offset)`）的 SHA-256 前 8 字节并与 footer 比对，不符 ⇒ `ErrFooterCorrupt`（500 + 诊断，**不得**静默返回「不存在」）。该字段目前**只写不读**，等于完整性检查从未执行——而「发现半写 / 位腐」正是它存在的全部理由。**下文的 `readFromPart` 代码块尚未包含该校验。**
> 3. **遇标记行返回 `ErrBodyMissing`**（404 + `body_missing=true`）：产物路径与 spool 路径**都**要识别，不得把字符串 `"null"` 当正文递出去（接口与用例已登记，保持不变）。
>
> 三条都必须有能区分它们的用例（全局约束的变异判据）：删掉末行终止符守卫、删掉 `frame_sha256` 校验、删掉任一标记行识别，分别都要有用例转红。

- [ ] **步骤 1：编写失败的测试**

**追加到 `tests/tracearchive/helpers_test.go`**（**B 层辅助**；任务 6 建好该文件，本任务加 `sealLines`）：

```go
// sealLines 辅助：写 spool 并封块，返回定位键起始点。
// 方案 C：封块路径需要 C 版 zstd CLI，故先 requireZstd（缺失则整组用例 skip）。
// 只用公开 API（ta.NewSealer/ta.SealerOptions/ta.SealTask），不触达未导出符号。
func sealLines(t *testing.T, root, dt, inst string, part int, lines [][]byte) {
	t.Helper()
	bin := requireZstd(t)
	spool := writeSpool(t, root, dt, inst, part, lines)
	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}
}
```

新建 `tests/tracearchive/reader_test.go`——**B 层，`package tracearchive_test`**：

```go
// tests/tracearchive/reader_test.go
package tracearchive_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 阅读约定：本块后面的代码按 package tracearchive 书写以省篇幅；
// 落盘时全部导出标识符加 ta. 前缀（NewReader→ta.NewReader、ReaderOptions→ta.ReaderOptions、
// Ref→ta.Ref、NewPaths→ta.NewPaths、FooterSize→ta.FooterSize、ErrNotArchived/ErrFooterCorrupt 同理）。

func TestReaderReturnsExactLine(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"r0","body":"短"}`),
		[]byte(`{"request_id":"r1","body":"` + string(bytes.Repeat([]byte("x"), 100000)) + `"}`),
		[]byte(`{"request_id":"r2","body":"结尾"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i, want := range lines {
		got, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i})
		if err != nil {
			t.Fatalf("第 %d 行读取失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("第 %d 行不一致:\n want %q\n got  %q", i, want, got)
		}
	}
}

func TestReaderFallsBackToSpoolForUnsealedRows(t *testing.T) {
	root := t.TempDir()
	// 只写 spool，不封块：模拟「近期正文尚未封块」
	lines := [][]byte{[]byte(`{"request_id":"hot0"}`), []byte(`{"request_id":"hot1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 1})
	if err != nil {
		t.Fatalf("未封块的行应从 spool 读到: %v", err)
	}
	if !bytes.Equal(got, lines[1]) {
		t.Fatalf("spool 兜底读到错误内容: %q", got)
	}
}

// TestReaderReportsBodyMissingForMarkerLine 锁定标记行的读取语义（规格 §8.2）：
// Task 5 的 AppendFailed 会写一行 null（正文从未落盘），两条读取路径
// **都必须**把它映射成 ErrBodyMissing，而不是把字符串 "null" 当正文返回 ——
// 后者会把「正文从未落盘」伪装成「正文就是 null」，与 body_missing 的 404 语义冲突。
func TestReaderReportsBodyMissingForMarkerLine(t *testing.T) {
	root := t.TempDir()
	// 标记行两侧都有真实正文，用来同时证明「按行序取行」没有被标记行带偏
	lines := [][]byte{[]byte(`{"request_id":"a"}`), []byte("null"), []byte(`{"request_id":"c"}`)}

	// ① 产物路径：spool 封块后，标记行进了产物
	sealLines(t, root, "2026-09-24", "blue", 0, lines)
	// ② spool 路径：同日同实例的另一个块不封块，直接兜底读
	writeSpool(t, root, "2026-09-24", "blue", 1, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, part := range []int{0, 1} {
		_, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: part, Row: 1})
		if !errors.Is(err, ErrBodyMissing) {
			t.Fatalf("part=%d 的标记行必须返回 ErrBodyMissing，实际 %v", part, err)
		}
		// 反向断言：标记行前后的真实正文必须照常读到，否则「一律报 missing」也会全绿
		for _, tc := range []struct {
			row  int
			want []byte
		}{{0, lines[0]}, {2, lines[2]}} {
			got, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: part, Row: tc.row})
			if err != nil {
				t.Fatalf("part=%d row=%d 应正常读到: %v", part, tc.row, err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("part=%d row=%d 内容不符: want %q, got %q", part, tc.row, tc.want, got)
			}
		}
	}
}

func TestReaderDistinguishesMissingFromNotArchived(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("only")})

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()

	// 块完全不存在（既无产物也无 spool）→ ErrNotArchived
	_, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 99, Row: 0})
	if !errors.Is(err, ErrNotArchived) {
		t.Fatalf("两者都不存在时应返回 ErrNotArchived，实际 %v", err)
	}

	// 行号越界 → 明确报错，不得静默返回空
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 5})
	if !errors.Is(err, ErrNotArchived) {
		t.Fatalf("产物内行号越界应返回 ErrNotArchived，实际 %v", err)
	}

	// spool 内行号越界 → 同样必须是 ErrNotArchived（readFromSpool 的守卫）
	spoolOnly := writeSpool(t, root, "2026-09-24", "green", 0, [][]byte{[]byte("only-one-row")})
	if spoolOnly == "" {
		t.Fatal("前置失败")
	}
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "green", Part: 0, Row: 9})
	if !errors.Is(err, ErrNotArchived) {
		t.Fatalf("spool 内行号越界应返回 ErrNotArchived，实际 %v", err)
	}
}

func TestReaderRejectsCorruptFooter(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("data")})

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-FooterSize] ^= 0xFF // 篡改 magic
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("footer 被篡改时应返回 ErrFooterCorrupt 而非「不存在」，实际 %v", err)
	}
}

func TestReaderConcurrencyIsBounded(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x"), []byte("y"), []byte("z")})

	r, err := NewReader(ReaderOptions{Root: root, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发读 %d 失败: %v", i, err)
		}
	}
	// 并发槽位是否**完全释放**要读内部状态（r.inFlight），外部包看不到，
	// 因此那条断言单独放在 A 层：internal/tracearchive/reader_internal_test.go
	// 的 TestReaderReleasesConcurrencySlots。
}

func TestReaderDoesNotLeaveTempFiles(t *testing.T) {
	// 规格 §8.3 C2：不落任何临时文件。此测试断言读取前后目录条目集合不变。
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa"), []byte("bbb")})

	dir := NewPaths(root).PartDir("2026-09-24", "blue")
	before := dirEntries(t, dir)

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()
	for i := 0; i < 50; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatal(err)
		}
	}
	after := dirEntries(t, dir)
	if len(before) != len(after) {
		t.Fatalf("读取不应改变目录内容: before=%v after=%v", before, after)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

var _ = filepath.Join
```

**A 层测试文件**（`internal/tracearchive/reader_internal_test.go`，`package tracearchive`）——同样写在步骤 1 里：

```go
// internal/tracearchive/reader_internal_test.go
package tracearchive

import (
	"os"
	"testing"
)

// TestReaderRejectsOversizedTrailerLen 覆盖 Reader 对 footer 的 trailer_len 上界校验。
//
// 为什么必须有这道闸：decodeTrailer 的容量上限是 trailer 字节数的 8 倍
//（len(b) 字节的 varint 流确实可能编码出 len(b) 个 int64 条目，这是格式固有的）。
// 当 trailer_len 可以接近文件大小时，一次损坏的 footer 就能让 control-plane
//（2 GiB 限额）尝试分配数 GB 并 OOM。trailer 紧邻 footer 的一致性校验只把
// trailer_len 绑在文件大小上，拦不住「1.5 GiB 的块 + 1.5 GiB 的 trailer」。
//
// 构造方式：稀疏文件，帧+trailer 区总长 = maxTrailerBytes+1，footer 自洽
//（trailer_offset=0，trailer_offset+trailer_len == size-FooterSize）。
// 断言 Reader 返回损坏类错误，而不是尝试分配。
func TestReaderRejectsOversizedTrailerLen(t *testing.T) {
	root := t.TempDir()
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}

	trailerLen := int64(maxTrailerBytes) + 1
	size := trailerLen + FooterSize

	// 稀疏文件：Truncate 不写实际数据，因此不会真的占用 64 MiB
	f, err := os.Create(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	fb := encodeFooter(footer{TrailerOffset: 0, TrailerLen: int32(trailerLen), RowCount: 1})
	if _, err := f.WriteAt(fb, size-FooterSize); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("trailer_len 超过上界应返回 ErrFooterCorrupt（产物损坏），实际 %v", err)
	}
}

// TestReaderReleasesConcurrencySlots 覆盖「并发槽位用后必须完全释放」。
//
// 【为什么破例放包内】断言要读 Reader 的内部状态 r.inFlight()（= len(r.sem)），
// 这是未导出的测试辅助方法。外部包只能观察到「读成功了」，观察不到「槽位还回来了」——
// 而槽位泄漏正是并发上限实现最容易出的 bug（跑满 MaxConcurrent 后永久卡死）。
// 对应的公开行为断言留在 B 层：tests/tracearchive/reader_test.go 的
// TestReaderConcurrencyIsBounded（8 个并发读必须全部成功）。
func TestReaderReleasesConcurrencySlots(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x"), []byte("y")})

	r, err := NewReader(ReaderOptions{Root: root, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := 0; i < 3; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
		if got := r.inFlight(); got != 0 {
			t.Fatalf("第 %d 次读取后并发槽位未释放，残留 %d", i, got)
		}
	}
}
```

> 上面两个 A 层用例都用到了 `sealLines`/`writeSpool`/`requireZstd`——它们由 **A 层辅助文件** `internal/tracearchive/testhelpers_test.go` 提供（任务 6 建立）；A 层版本里的 `sealLines` 用不带 `ta.` 限定的 `NewSealer`/`SealerOptions`/`SealTask`。B 层同名辅助在 `tests/tracearchive/helpers_test.go`，两份实现内容一致、**故意重复**。



- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestReaderReturnsExactLine|TestReaderFallsBackToSpoolForUnsealedRows|TestReaderReportsBodyMissingForMarkerLine|TestReaderDistinguishesMissingFromNotArchived|TestReaderRejectsOversizedTrailerLen|TestReaderReleasesConcurrencySlots|TestReaderRejectsCorruptFooter|TestReaderConcurrencyIsBounded|TestReaderDoesNotLeaveTempFiles' -v`
预期：**两个包**都编译失败——B 层提示 `undefined: ta.NewReader`、`undefined: ta.ReaderOptions`；A 层提示 `undefined: maxTrailerBytes`、`r.inFlight undefined`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/reader.go`：

```go
package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// maxTrailerBytes 是 trailer 大小的合理性上界。
//
// 真实 trailer 是「每行一个差分 varint」：1.5 GiB 的块按每行约 400 KB 估算只有
// 约 4000 行，编码后约 12 KB；即便每行只有 1 字节，也需要上千万行才能撑到这个量级。
//
// 不设上界时，一个损坏的 footer 可以让 trailer_len 接近文件大小，
// 进而让 decodeTrailer 试图分配其 8 倍的 int64 容量，在 control-plane 的
// 2 GiB 限额内 OOM。这是「把产物损坏挡成错误」的最后一道闸，
// 与规格 §8.1 的意图一致（损坏必须报成损坏，不能伪装成「数据不存在」）。
const maxTrailerBytes = 64 << 20

// maxTrailerEntries 是 trailer 条目数（= 块内行数）的合理性上界，
// 与 maxTrailerBytes 一起构成读侧内存预算的两半。【修复轮 F1 补入】
//
// 【为什么字节侧上界不够（实测，原始数字）】decodeTrailer 的分配是 8 × 条目数，
// 最坏情况下 trailer 的每个字节都编码出一个条目（单字节 varint）：64 MiB 全
// 0x00 的 trailer ⇒ n = 67,108,864 条，实测 decodeTrailer 【成功返回】
// （err=nil，不是报错）、make 分配 +512.0 MiB；装进真封块帧后单请求峰值堆
// 1090.0 MiB、稳态 2 并发 1666.0 MiB（见 r11-report.md §7.9）。
//
// 【取值与推导】条目数 × 8 字节 ≤ maxTrailerBytes ⇒ n ≤ 8 Mi = 8,388,608 条。
// 判定在 decodeTrailer 内、【在 make 之前】、超限返回明确错误、【绝不截断】。
//
// 【据实登记的代价（误报边界，D35；**措辞已按 D38 更正**）】真触发条件与块的
// 字节大小无关：只要**块内行数 > 8,388,608**（示例：8,388,609 行 × 每行 8 字节
// = **64.0 MiB**）合法产物就会超限 ⇒ GetBody 响亮误报 ErrTrailerCorrupt。
// 「块满 1.5 GiB 且平均每行不足 192 字节」只是**充分条件之一**，不是必要条件。
// 这是预算取舍（用块大小推上界会更宽 ⇒ 更弱的内存界），登记为计划 4 的验证项（N4，
// 核实口径是**每块的最高行数**而非「平均行大小」）。
const maxTrailerEntries = maxTrailerBytes / 8

// ReaderOptions 是点读器配置。
type ReaderOptions struct {
	Root            string
	ZstdWindowBytes int // 0 取 defaultZstdWindowBytes；必须与写入侧一致
	MaxConcurrent   int // 解码器内存 ≈ 窗口大小，故并发必须设上限（规格 §8.4 C5）；0 取 2
}

// Reader 按定位键取回正文行。
//
// 空间硬约束（规格 §8.3）：
//   C1 流式解压到内存，命中目标行立即停止 —— 峰值内存 ≈ 一行，与块大小无关
//   C2 不落任何临时文件
//   C3 不缓存已解压的块
//   C4 读 spool 直接顺序读，不复制
type Reader struct {
	opts  ReaderOptions
	paths Paths

	// sem 限制并发解码。解码器内存≈窗口（512 MiB 窗口约需 512~600 MB），
	// 不设上限会把 control-plane 撑爆。
	sem chan struct{}
}

// NewReader 构造点读器。
func NewReader(opts ReaderOptions) (*Reader, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	return &Reader{
		opts:  opts,
		paths: NewPaths(opts.Root),
		sem:   make(chan struct{}, opts.MaxConcurrent),
	}, nil
}

// Close 释放资源。当前实现无长驻资源，仅为接口完整性保留。
func (r *Reader) Close() error { return nil }

// inFlight 返回当前占用的并发槽位数，供测试断言锁已释放。
func (r *Reader) inFlight() int { return len(r.sem) }

// GetBody 返回定位键对应的正文行（不含换行）。
//
// 查找顺序（规格 §8.2）：产物 → spool。
// 两者不会同时存在：spool 只在产物 fsync + rename + 目录 fsync 全部成功后才删除
// （见 Sealer.Seal），因此这里不存在竞态。
func (r *Reader) GetBody(ctx context.Context, ref Ref) ([]byte, error) {
	partPath := r.paths.PartFile(ref)

	if _, err := os.Stat(partPath); err == nil {
		return r.readFromPart(ctx, ref, partPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	spoolPath := r.paths.SpoolFile(ref)
	if _, err := os.Stat(spoolPath); err == nil {
		return readFromSpool(spoolPath, ref.Row)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查 spool 失败: %w", err)
	}

	// 产物与 spool 都不存在：这是元数据与存储不一致，属异常。
	// 绝不能返回「数据不存在」——那会把存储故障伪装成业务事实（v3 方案的教训）。
	return nil, fmt.Errorf("%w: %s", ErrNotArchived, ref)
}

// readFromPart 从产物文件读取。全程流式，不落临时文件、不缓存。
func (r *Reader) readFromPart(ctx context.Context, ref Ref, partPath string) ([]byte, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f, err := os.Open(partPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size < FooterSize {
		return nil, fmt.Errorf("%w: 文件仅 %d 字节 < footer %d 字节", ErrFooterCorrupt, size, FooterSize)
	}

	// C1 前置：读 footer 只需末尾 32 字节，不必解压任何数据
	fb := make([]byte, FooterSize)
	if _, err := f.ReadAt(fb, size-FooterSize); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 footer 失败: %w", err)
	}
	ft, err := decodeFooter(fb)
	if err != nil {
		return nil, err
	}
	if ft.TrailerOffset+int64(ft.TrailerLen) != size-FooterSize {
		return nil, fmt.Errorf("%w: trailer 未紧邻 footer（off=%d len=%d size=%d）",
			ErrFooterCorrupt, ft.TrailerOffset, ft.TrailerLen, size)
	}
	// 行号越界必须在此明确拒绝，不能指望后续解码偶然失败
	if ref.Row < 0 || ref.Row >= int(ft.RowCount) {
		return nil, fmt.Errorf("%w: %s（块内共 %d 行）", ErrNotArchived, ref, ft.RowCount)
	}
	// trailer 大小的合理性上界（读侧内存预算的**字节侧**）。decodeTrailer 的容量上限是
	// trailer 字节数的 8 倍（这是格式固有的：len(b) 字节的 varint 流可能编码出 len(b) 个
	// int64 条目），而 trailer_len 只被「紧邻 footer」这一条绑在文件大小上 ⇒ 一个 1.5 GiB
	// 的块仍可给出接近 1.5 GiB 的 trailer，让 control-plane（2 GiB 限额）尝试分配数 GB。
	// 归入「产物损坏」，与 §8.1 的意图一致。
	// 【修复轮 F1 补入】字节侧上界【单独不够】：它压的是字节，而分配是 8 × 条目数。
	// **条目数一侧由 maxTrailerEntries 单独封住（判定在 decodeTrailer 内、make 之前）**
	// —— 两个上界缺一不可（D30、r11-report.md §7.9）。
	if int64(ft.TrailerLen) > maxTrailerBytes {
		return nil, fmt.Errorf("%w: trailer_len=%d 超过上界 %d", ErrFooterCorrupt, ft.TrailerLen, maxTrailerBytes)
	}

	tb := make([]byte, ft.TrailerLen)
	if _, err := f.ReadAt(tb, ft.TrailerOffset); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 trailer 失败: %w", err)
	}
	// 传入 footer 记录的期望条目数：这样被截断的 trailer 会被识别为损坏，
	// 而不是静默解码出更少的条目（那会让越界读取伪装成正常读取）。
	offsets, err := decodeTrailer(tb, int(ft.RowCount))
	if err != nil {
		// footer 与 trailer 不一致 ⇒ 产物损坏，绝不能报成「数据不存在」
		return nil, err
	}
	targetOff := offsets[ref.Row]

	// 流式解压：从头解压到目标偏移，命中即停
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(f,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(uint64(r.opts.ZstdWindowBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 建 zstd 解码器失败: %w", err)
	}
	defer dec.Close()

	br := bufio.NewReaderSize(dec, 1<<20)
	var read int64
	for read <= targetOff {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, readErr := br.ReadBytes('\n')
		if len(line) > 0 && read == targetOff {
			// 标记行（正文写入时即失败，规格 §7.1/§8.2）：绝不能把 "null" 当正文返回。
			// 复用 Task 5 的 failedRowMarker：同包共用一份定义。
			if isFailedRowMarker(line) {
				return nil, fmt.Errorf("%w: %s", ErrBodyMissing, ref)
			}
			return trimNewline(line), nil
		}
		if len(line) > 0 {
			read += int64(len(line))
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, fmt.Errorf("tracearchive: 解压读取失败: %w", readErr)
		}
	}
	return nil, fmt.Errorf("%w: %s（偏移 %d 未命中行首）", ErrNotArchived, ref, targetOff)
}

// readFromSpool 从未压缩的 spool 顺序读取第 row 行。
// C4：直接顺序读，不复制整文件。
func readFromSpool(spoolPath string, row int) ([]byte, error) {
	f, err := os.Open(spoolPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	for i := 0; ; i++ {
		line, err := br.ReadBytes('\n')
		if i == row && len(line) > 0 {
			// 与产物路径同一条约定：标记行 ⇒ ErrBodyMissing，不返回 "null"（规格 §8.2）。
			if isFailedRowMarker(line) {
				return nil, fmt.Errorf("%w: spool %s 第 %d 行", ErrBodyMissing, spoolPath, row)
			}
			return trimNewline(line), nil
		}
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("%w: spool 中不存在第 %d 行", ErrNotArchived, row)
			}
			return nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", err)
		}
	}
}

// isFailedRowMarker 判断一行是否是 Task 5 的 AppendFailed 留下的标记行
// （内容 "null"：该行正文从未落盘）。产物路径与 spool 路径共用它，
// 保证两条路径的判据只有一份（规格 §7.1/§8.2）。
//
// 只比较 trim 掉换行后的字节与 failedRowMarker：不解析 JSON、不做逐字节 JSON
// 等价（例如 "null " 或 "\u006eull"）——写入侧只可能产出这一种形态，
// 放宽判据只会把真实正文误报成缺失。
func isFailedRowMarker(line []byte) bool {
	return bytes.Equal(trimNewline(line), failedRowMarker)
}

var _ = errors.Is
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestReaderReturnsExactLine|TestReaderFallsBackToSpoolForUnsealedRows|TestReaderReportsBodyMissingForMarkerLine|TestReaderDistinguishesMissingFromNotArchived|TestReaderRejectsOversizedTrailerLen|TestReaderReleasesConcurrencySlots|TestReaderRejectsCorruptFooter|TestReaderConcurrencyIsBounded|TestReaderDoesNotLeaveTempFiles' -v`
预期：两个包都 PASS（含逐字节一致、spool 兜底、**标记行在两路径上都返回 `ErrBodyMissing`**、两类行号越界的错误身份、oversized trailer_len、并发槽位释放、footer 篡改、并发上限、无临时文件）。

> 这些用例经 `sealLines` → `Sealer` 走真实 `zstd`，因此**需要本机装 zstd**（缺失则 skip）。

**变异自检**：删掉 `maxTrailerBytes` 上界检查 → `TestReaderRejectsOversizedTrailerLen` 必须转红（且可能 OOM）；把 `ErrNotArchived` / `ErrFooterCorrupt` 的 `%w` 改成 `%v` → 对应用例必须转红；删掉行号越界检查 → `TestReaderDistinguishesMissingFromNotArchived` 必须转红；删掉 `readFromPart` 或 `readFromSpool` 里的标记行识别（任一处，规格 §8.2 要求两条路径都有）→ `TestReaderReportsBodyMissingForMarkerLine` 必须转红；把 `defer func() { <-r.sem }()` 删掉 → `TestReaderReleasesConcurrencySlots` 必须转红（B 层的并发用例不会红——它只断言「全部读成功」，所以这条断言必须留在 A 层）。**R10 的两条另加**：删掉 `readFromSpool` 的末行终止符守卫 → 该守卫的用例必须转红；删掉 `frame_sha256[:8]` 校验 → 该校验的用例必须转红（两条用例在落盘时必须一并补上，见上方 R10 块）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/reader.go internal/tracearchive/reader_internal_test.go \
        tests/tracearchive/reader_test.go tests/tracearchive/helpers_test.go
git commit -m "feat(tracearchive): 按定位键点读（含 spool 兜底与错误分类）

- 读 footer 只需末尾 32 字节，不必解压任何数据即可定位索引
- 流式解压命中即停，峰值内存与块大小无关；不落临时文件、不缓存
- 未封块的近期正文从 spool 直接读，无需等待封块
- 标记行（AppendFailed 写的 null，正文从未落盘）在产物与 spool 两条路径上
  都返回 ErrBodyMissing，不得把 "null" 当正文返回（规格 §8.2）
- 严格区分「产物缺失 / 从未归档 / footer 损坏」，杜绝把故障伪装成数据不存在
- trailer_len 设 64 MiB 合理性上界：损坏的 footer 不得让读取端分配数 GB
- 解码并发设上限（解码器内存≈窗口，规格 §8.4 C5）
- 测试分层：公开行为断言在 tests/tracearchive/；依赖未导出的
  maxTrailerBytes 上界与 r.inFlight() 槽位释放断言留在包内"
```

---

### Task 8：端到端往返与约束验证

**文件：**
- 新建：`tests/tracearchive/roundtrip_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**
- 新建：`tests/tracearchive/constraints_test.go`——**B 层（`package tracearchive_test`，仅用公开 API）**
- **本任务不需要任何 A 层文件**：端到端往返与空间约束都是「走公开 API 的行为」——`Writer`/`Sealer`/`Reader` 的导出方法 + 文件系统可观测结果，没有一条断言需要未导出符号。这正是判据里「优先选择 B 层」的标准情形。

**接口：**
- 依赖输入：任务 5（`Writer`）、任务 6（`Sealer`）、任务 7（`Reader`）
- 对外产出：无新导出符号（集成测试）

- [ ] **步骤 1：编写端到端往返测试**

新建 `tests/tracearchive/roundtrip_test.go`——**B 层，`package tracearchive_test`**：

```go
// tests/tracearchive/roundtrip_test.go
package tracearchive_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 阅读约定：本块代码按 package tracearchive 书写以省篇幅；
// 落盘时全部导出标识符加 ta. 前缀（NewWriter→ta.NewWriter、WriterOptions→ta.WriterOptions、
// NewSealer→ta.NewSealer、SealerOptions→ta.SealerOptions、SealTask→ta.SealTask、
// NewReader→ta.NewReader、ReaderOptions→ta.ReaderOptions、Ref→ta.Ref）。
// requireZstd 来自 tests/tracearchive/helpers_test.go（任务 6）。

// TestEndToEndRoundTripByteExact 是验收用例 A1 的缩小版：
// 写入 N 行 → 封块 → 随机抽 K 行读回 → 逐字节比对。
func TestEndToEndRoundTripByteExact(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端测试，-short 下跳过")
	}
	const (
		totalRows = 20000
		sampleK   = 200
	)
	root := t.TempDir()
	// 方案 C：封块走真实 zstd CLI（本用例需要本机有 zstd，缺失则 skip）
	zstdBin := requireZstd(t)

	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	rnd := rand.New(rand.NewSource(20260924))
	makeLine := func(i int) []byte {
		n := rnd.Intn(4096)
		return []byte(fmt.Sprintf(`{"request_id":"rid-%06d","ts":"2026-09-24T00:00:00+08:00","body":"%s"}`,
			i, string(bytes.Repeat([]byte("x"), n))))
	}

	want := make(map[Ref][]byte, totalRows)
	// 逐行追加，并在达到阈值时封块。
	// 这里用 Writer.PendingSeals 直接驱动，等价于生产里 sealer 的
	//「ListPendingSeals 发现 → Seal 封块」循环（方案 C：封块在网关外）。
	for i := 0; i < totalRows; i++ {
		line := makeLine(i)
		ref, err := w.Append("2026-09-24", line)
		if err != nil {
			t.Fatalf("第 %d 行追加失败: %v", i, err)
		}
		want[ref] = line
		tasks, err := w.PendingSeals()
		if err != nil {
			t.Fatalf("第 %d 行后 PendingSeals 失败: %v", i, err)
		}
		for _, task := range tasks {
			s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: zstdBin})
			if err := s.Seal(context.Background(), task); err != nil {
				t.Fatalf("封块失败: %v", err)
			}
			w.MarkSealed(task.DT, task.Part)
		}
	}
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		s, _ := NewSealer(SealerOptions{Root: root, ZstdBinary: zstdBin})
		if err := s.Seal(context.Background(), task); err != nil {
			t.Fatalf("收尾封块失败: %v", err)
		}
		w.MarkSealed(task.DT, task.Part)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 随机抽样，逐字节比对
	refs := make([]Ref, 0, len(want))
	for ref := range want {
		refs = append(refs, ref)
	}
	checked := 0
	for checked < sampleK && len(refs) > 0 {
		i := rnd.Intn(len(refs))
		ref := refs[i]
		refs = append(refs[:i], refs[i+1:]...)

		got, err := r.GetBody(context.Background(), ref)
		if err != nil {
			t.Fatalf("%s 读取失败: %v", ref, err)
		}
		if !bytes.Equal(got, want[ref]) {
			t.Fatalf("%s 内容不一致:\n want %d 字节\n got  %d 字节", ref, len(want[ref]), len(got))
		}
		checked++
	}
	t.Logf("逐字节比对通过: %d/%d 行，共写入 %d 行", checked, sampleK, totalRows)
}
```

- [ ] **步骤 2：编写约束验证测试**

新建 `tests/tracearchive/constraints_test.go`——**B 层，`package tracearchive_test`**：

```go
// tests/tracearchive/constraints_test.go
package tracearchive_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 阅读约定：同 roundtrip_test.go——导出标识符落盘时加 ta. 前缀；
// sealLines / writeSpool 来自 tests/tracearchive/helpers_test.go。

// dirUsage 统计目录下所有文件的总字节数。
func dirUsage(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// TestReadsDoNotGrowDiskUsage 是验收用例 A4：
// 规格 §8.3 的 C1~C4 是强制性约束，读取千次后磁盘占用增量必须为 0。
//
// 若实现被改成「解压到 /tmp 再读」或「缓存已解压块」，此测试会立刻转红。
func TestReadsDoNotGrowDiskUsage(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"a"}`),
		[]byte(`{"request_id":"b"}`),
		[]byte(`{"request_id":"c"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 预热一次，排除首次读取可能的一次性分配
	if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)

	for i := 0; i < 1000; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("读取 1000 次后磁盘占用从 %d 变为 %d（违反 §8.3 C1~C4）", before, after)
	}
}

// TestUnsealedReadsAlsoDoNotGrow 覆盖 spool 兜底路径的空间约束。
func TestUnsealedReadsAlsoDoNotGrow(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte(`{"request_id":"h0"}`), []byte(`{"request_id":"h1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)
	for i := 0; i < 500; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("spool 读取后磁盘占用从 %d 变为 %d", before, after)
	}
}
```

- [ ] **步骤 3：运行测试并确认通过**

运行：`go test ./tests/tracearchive/ -run 'TestEndToEndRoundTripByteExact|TestReadsDoNotGrowDiskUsage|TestUnsealedReadsAlsoDoNotGrow' -v`
预期：全部 PASS。特别注意 `TestReadsDoNotGrowDiskUsage` 与 `TestEndToEndRoundTripByteExact`。

> 需要本机 `zstd`（两者都经 `Sealer` 真实封块）；缺失则 skip。

- [ ] **步骤 4：跑竞态检测**

运行：`go test -race ./internal/tracearchive/ ./tests/tracearchive/ -count=1`
预期：PASS，无 data race 报告。（A 层与 B 层**都要跑**：并发相关断言分居两层。）

- [ ] **步骤 5：全仓构建与测试**

运行：
```bash
go build ./...
go test ./internal/... ./tests/tracearchive/... -count=1
```
预期：本包全绿；仅 `test/bedrock_model_id_check.go` 的既有编译问题可忽略。
**方案 C 前置**：跑之前确认本机已装 `zstd`（`zstd --version`），否则任务 6/7/8 的封块类用例会全部 skip —— 那是「假绿」，不是通过。

> **注意**：`make test` 只跑 `./tests/...`，**不会**跑包内（A 层）测试；而 `.gitlab-ci.yml` 里没有任何 `go test`。两层都要靠上面这条显式命令覆盖（改动 `Makefile`/CI 超出本子计划范围，已记录为遗留项）。

- [ ] **步骤 6：提交**

```bash
git add tests/tracearchive/roundtrip_test.go tests/tracearchive/constraints_test.go
git commit -m "test(tracearchive): 端到端逐字节往返与空间零增量约束验证

- 2 万行写入 → 封块 → 随机抽 200 行读回，逐字节比对（验收 A1 缩小版）
- 封块经 C 版 zstd CLI（requireZstd 保证不是静默 skip 的假绿）
- 读取 1000 次后磁盘占用增量断言为 0（规格 §8.3 C1~C4 的断言级验证，A4）
- spool 兜底路径单独覆盖空间约束
- 两条都是「走公开 API 的行为」，因此整体落在 tests/tracearchive/（B 层）"
```

---

## 计划缺陷登记（Task 5 交付后追加：D4~D6）

编号延续执行期已有的计划缺陷序列（**D1** `cmd/trace-calib` 的 `Fprintf` 括号错位、**D2** `decodeTrailer` 的期望条目数、**D3** `PendingSeals` 未 flush 写缓冲；三者在飞行前扫描/执行记录中登记并已修复）。以下三条由 **Task 5 实现者在提交前报备**，控制者逐条裁决，**均已落地在提交 `8cd873e7`**，此处登记以校正后续任务（尤其任务 6/7）的简报依据。

**发现者：Task 5 实现者（提交前报备）。**

| 编号 | 缺陷 | 承重性 | 修法 |
|---|---|---|---|
| D4 | `PendingSeals` 只在**产出封块任务时**才 flush ⇒ 一个块在其生命周期的前 `SealInterval`（15 分钟）内磁盘上始终是**空文件**、mtime 停在创建时刻 ⇒ sealer 的 minAge 判断把活跃块当成「已停止追加」，封出空产物并删掉 spool ⇒ **静默丢数据**（与 D3 同类，触发条件从「忘了 flush」变成「没到阈值」） | **承重** | **无条件 flush**：flush 提到 `cur == nil` 判断之后、所有阈值早退之前；flush 失败仍以 error 上抛（sealer 会直接读 spool 文件，不能把不完整的块当完整的封掉） |
| D5 | `MarkSealed` 只把 `cur` 置 nil、不 close 旧句柄 ⇒ 每个封块泄漏一个 `*os.File`；按 `BlockBytes=1.5 GiB` + 15 min 估算，每实例每天上百个 fd ⇒ 长跑进程最终 **EMFILE**（任务 8 的端到端流程正是在 `Seal` 之后调它，路径可达） | 资源泄漏 | `_ = w.closeCurLocked()`（flush + close），签名不变。close 错误只能丢弃（调用点冻结为无返回值），这与「`PendingSeals` 必须上抛 flush 错误」不矛盾：后者是为了不把不完整的块交给 sealer，此处已晚于 sealer 读完文件 |
| D6 | `AppendFailed` 只推进行号、不写字节 ⇒ 产物行序与 `row_seq` 错位，读取侧两条路径都按行序取 ⇒ **`row=N` 读成第 `N+1` 行（静默返回错误的正文）**。根因是与产物格式的**稠密性不相容**：footer 的 `row_count` 是产物内行数，trailer 是「每行一个差分 varint」的偏移表，而该 varint 编码没有表达「本行没有偏移」的能力（它假定行稠密）；写侧稀疏 + 格式要求稠密 + 读侧按行序三者不可能同时成立 | **承重**（本项目最不能接受的一类缺陷） | `AppendFailed(dt)` **推进行号的同时写一个内容为 `null` 的标记行**（`AppendFailed` 复用 `Append(dt, failedRowMarker)`，让标记行与真实正文共用同一段写入代码，「物理行序 == `row_seq`」由构造保证）。标记取 `null`：合法 JSON、spool 每行都是正文对象故不可能与真实正文混淆；不用空行（产物是 `.jsonl`，空行会让按行解析的工具炸掉）、不用 `{}`（与「合法的空正文」无法区分） |

**D6 的下游约定**：任务 6 的行数交叉校验由此**天然对齐**，不得为「跳号」添加补偿逻辑（见任务 6 的登记说明）；任务 7 的两条读取路径（产物 / spool）遇到标记行必须返回 **`ErrBodyMissing`**，不得把 `"null"` 当正文递出去（见任务 7 的接口与用例）。

**同批报备但不在上表的两条**（登记以免后人误判为遗漏），**均已在评审修正 `52ccc9b2` 落地**：

- **`Inst` 白名单（`8cd873e7`）与 `dt` 路径分量校验（`52ccc9b2`）成对**：两者是同一个洞的两半——`inst` 与 `dt` 都直接拼进目录名，而 `filepath.Join` 的清理**只对「裸 `..` 元素」生效**，因此**真正的穿越向量是含斜杠的值**（`Inst="../../green"` ⇒ `<root>/dt=X/green`；`dt="2026-09-24/../.."` ⇒ 产物**越出 `<root>`**，已由 D15 的隔离探针 B' 实测**真在 `root` 之外落盘**）；紧贴前缀的 `"../green"`、`"../x"`、`"blue/"` 一律**不折叠**，被拒的理由是「不是合法日期 / 目录名与标识不符」。`Inst` 用一条枚举分支（同时涵盖空值与非法值，取代简报里的独立「非空 Inst」分支，理由是删掉它没有任何用例转红）；`dt` 用 `validateDT`（`time.Parse`）挡住空串与非法形态、长度不符、未补零、非法日期（真穿越向量一并被挡），且校验先于任何文件系统副作用。两条都不设哨兵（调用方无从分支处理的参数错误）。
- **移除 `ListParts`（`52ccc9b2`）**：控制者按 YAGNI 裁定移除（本包内无消费者：任务 6 用 `ListPendingSeals` 扫 spool，任务 7 从 `Ref` 推导路径），已连同它唯一用到的 `sort` 导入与用例 `TestWriterListPartsFiltersIllegalNames` 一并删除。

> **说明（本节的时效）**：Task 5 的报告（`.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-5-report.md`）§7.B/§7.C 把 `ListParts` 记为「保留并补了用例」、把 `dt` 校验记为「建议，未实现」；那是提交 `8cd873e7` 时点的状态，已被评审修正 `52ccc9b2` 取代。**以代码为准**：截至 `52ccc9b2`，两条修正都已落地。

### 追加缺陷登记（本轮：D7~D17；R11 收尾追加：D18~D27；终审 A 修复轮追加：D28~D36；聚焦复审（终审 A 第二轮）修复轮 F3 追加：D37~D40）

编号续 D6。**发现者**逐条注明。D8~D12 由 **Task 6 实现者**在实现与自检中报备；D13、D14 由 **Task 5 评审者**报备；D7 由 **Task 5 评审者**在范围外观察时发现；**D15 由 Task 6 实现者在执行控制者裁决 (a) 的核实过程中自查发现**（非被要求）；**D16、D17 由修复轮 3 的实现者报备**（分别在开工前的前置检查中、与「要求 → 变异条目」映射表的成本核算过程中发现）。

**D18~D25 由 R11 收尾（阶段② 的独立 runner 与阶段③ 汇总，2026-09-24）登记**：D18、D19 由 **Task 2 / Task 3 实现者各自独立发现**（R11 阶段③ 复核确认）；**D20 由 R11 控制者**在「原遗留项 11」的映射表成本核算中裁定，由 Task 3 实现者拆分落地；**D21 由 R11 阶段② runner `5f7e23fe`** 在跑 task-2 权威表时发现；**D22 由 runner `25dd2809`**（task-5）、**D23 由 runner `12499504`**（task-7）在权威表工作中发现；**D24、D25 由 R11 控制者**在处置 `PENDING` / `COVERAGE_GAPS` 时发现。**D26、D27 由终审 B（证据链审计轴）** 在逐格复核 `r11-report.md` 时发现（分别是「报告出处指错」与「强推断被写成直接证据」）。全部发现及其证据出处汇总于 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/r11-report.md` §5。

**D28~D35 由终审 A（代码↔规格一致性轴）** 在独立评审 `c602e71d` 时发现，由**修复轮 F1**（`cb3253f5` + `221f8815`）与**修复轮 F2**（F2a 重跑、F2b 文档）处置；**D34、D35 属「登记不修」**（规格级残余 / 调用方义务 / 预算取舍）。**D36 由修复轮 F2a 在实测 `m54`/`m55` 的子例级归因时发现**（工具「杀死用例」列只到顶层用例名）。终审 A 报告原文 + 控制者逐条裁定见 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/final-review-A-code-spec.md`；R11 v2 重跑结果见同目录 `r11-report.md` §7。

**D37~D39 由聚焦复审（终审 A 的第二轮，只读）** 在 F1/F2 交付后复审 `21ffec3f` 时发现，由**修复轮 F3**（`949c3a27` + `ea962a70`）一处修复（D37 数据丢失缺陷 + D38 措辞更正 + D39 测试加固）；**D40 由修复轮 F2b 自报**（提交信息引用的行号与实际落点不符，**`amend` 已禁 ⇒ 无法修正，只能留痕**，不靠沉默）。D37 的独立复现、D37~D39 的修前/修后实测与 F3 的隔离探针结果见 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/r11-report.md` §8。**为什么必须写进本文件**：台账 `progress.md` 是 **gitignored** 的（会随会话消失），而 CI **完全不跑 `go test`** ⇒ **变异证据是本分支唯一的验证**，这些结论不能只留在台账里。

| 编号 | 缺陷 | 承重性 | 处置 |
|---|---|---|---|
| **D7** | **跨进程封块竞态**：网关侧没有「按年龄/大小滚动 part」的实现（`MarkSealed` 已交付但无人调用，`defaultSealInterval` 只是一个未被消费的常量），而 sealer 封块后会**删除 spool**；网关若继续往同一 part 追加，字节会写进**已 unlink 的 inode** ⇒ 落盘却永久不可达（其 `Ref` 仍指向行数更少的产物）⇒ **永久丢失正文**。**缓解不充分之处**：流量停 15 分钟后恢复正是触发条件 | **承重** | 属**计划 2**（网关滚动 part，间隔 ≤ 1h，见下方 R8）。本计划只交付了 `MarkSealed` 与 `minAge` 推导；在计划 2 落地前，3h 的安全性依赖 `defaultSealInterval = 15m` 这个现有常量 |
| **D8** | sealer 简报的 `scanSpoolOffsets` 把**未终止尾行计作一行** ⇒ `Rows == 0` 的目录扫描路径会把残行**静默烤进产物**（其后行边界全部错位） | **承重**（静默错正文） | 落地实现改为**拒绝**：spool 必须以换行符结尾，否则以 `ErrSealFailed` 保留 spool、不产生产物（残行守卫）。判据是「EOF 时仍带回了非空字节」（0 字节空 spool 合法） |
| **D9** | Task 6 简报的 `MkdirAll` 放在创建 `.tmp` **之后**，而 `.tmp` 就落在产物目录里 ⇒ 目录不存在时 `os.OpenFile(.tmp)` 直接 ENOENT，**10 条真封块用例全部失败** | 实现缺陷（非生产缺陷） | 建目录提到创建 `.tmp` **之前**（`.tmp` 必须与产物同目录才能 `rename`） |
| **D10** | Task 6 简报注释「`%w` 就能拿到 `exec.ErrNotFound`」**不成立**：`exec.Command` 只对**不含路径分隔符**的 name 走 `LookPath`，绝对路径下 `cmd.Run()` 返回 `*fs.PathError(ENOENT)`，`errors.Is(err, exec.ErrNotFound)` 恒为 false | 诊断保真度 | 在探测/运行处显式把「文件不存在」这一支归一化到 `exec.ErrNotFound`（同时保留原始错误）；用例的身份断言不变 |
| **D11** | golden 自检 `want[32:40]` **偏移 3 字节**（应为 `[35,43)`，即 `footerStart + [22,30)`）⇒ 该自检读到的是 `trailer_len`/`row_count` 与摘要前 5 字节的混合，**恒不成立** | 测试常量错误 | 改用格式契约表达：`footerStart := len(want) - FooterSize`，比较 `want[footerStart+22:footerStart+30]` |
| **D12** | **`-run` 合并正则内嵌换行导致用例静默不执行**：用 `printf` 往一个以换行结尾的正则文件追加用例名，最后一个分支变成 `"...MinAge\n"` ⇒ 该用例不再被执行，而 `ok` 照常打印（实现者差点把「28 条全绿」当成结论） | **方法论缺陷（影响每一个任务）** | 已写进本计划的**全局约束**：每条证据必须满足三条（顶层 `=== RUN` 名字集合与正则名字集合各自 `tr -d '[:space:]'` 后 diff 为空 —— 该命令同时消掉尾部换行与内嵌空白，只 trim 尾部的版本会放过本缺陷；顶层 `--- PASS: TestName ` 行数 == 顶层名字数，**不排除子用例会误报**；`SKIP == 0`）。**第二种形态（Task 6 评审实测，同属 D12）**：变异脚本只解析 `--- FAIL`、不校验「该跑的跑了没有」，遇到 `[no tests to run]` 把一个真为红的变异报成「绿（零覆盖！）」⇒ 全局约束已扩充为：三条断言**同样适用于每一次变异运行**，且**没有用例执行的运行判为「无效」而非「通过」**（详见全局约束第 2 条与自证表要求） |
| **D13** | `Append` 对行内换行**既无转义也无校验**，而简报的接口行明写「行内换行会被转义」（**文档说了代码不做的事**）；这是承重不变量的**唯一无守卫入口** | **承重**（静默错正文） | `validateLine` 拒绝行内裸 `\n`/`\r` 与 trim 后为空的行；校验只放公开 `Append`，落盘逻辑抽到 `appendLineLocked`（不校验，`AppendFailed` 复用）。**选择拒绝而非转义**的理由见规格 §7.1 |
| **D14** | 写失败后 `bufio` 错误**粘性**使标记行写不进去，且短写可能留下**没有 `row_seq` 的残行**；简报的「写失败只不推进行号」既救不回该块，又会让残行留在 sealer 的发现范围里 | **承重**（静默错正文） | 写失败隔离四步机制（回退行边界 / 改名隔离 / 毒化 / 退路清空），见规格 §7.1 与 Task 5 的「落地修正」③ |
| **D15** | **表驱动/共享断言环的遮蔽效应**：表驱动或共享断言循环**在第一个失败处就停止**，环内更早的条目会**遮蔽**新增条目——inst 环里的 `"red"`、dt 环里的 `""` 先失败，于是「摘掉守卫 ⇒ 整条用例转红」看似证明新增的 `"../../green"`、`"2026-09-24/../../x"` 承重，实则只证明环里有**某个**条目失败了 | **方法论缺陷（证据自证性；影响每一个表驱动/共享断言环用例）** | 新增条目或守卫的承重证据必须来自**只含该条目的隔离探针**（或等效的独立用例）：**守卫在位 ⇒ PASS**；**摘掉守卫 ⇒ FAIL 于该条目**。已写进本计划的**全局约束**（表驱动/共享断言环的遮蔽效应条）。**三侧探针范例（发现者＝Task 6 实现者，自查发现）**：**A** 守卫在位 ⇒ PASS（日志显示 `../../green`、`a/../../green` 各被 inst 白名单拒、`"2026-09-24/../../x"` 被 `validateDT` 拒）；**B** 摘掉 inst 白名单 ⇒ **FAIL 于 `../../green`**（不是 FAIL 于 `"red"`）；**B'** 只摘 `validateDT` ⇒ **FAIL 于新增的 dt 向量**，且**实测真的在 `root` 之外创建了** `…/T/x/inst=blue` ⇒ 这是「**含 `/` 的值确实穿越**」的实测反证，比引用 `filepath.Join` 的语义推导更硬（规格 §7.2 与本计划 Task 6 落地修正④ 的精确表述均以它为准） |
| **D16** | **证据绑定不到提交**：证据表不记录它所依据的 `HEAD`、工作树状态与各输入哈希 ⇒ 「这张表是哪棵树的产物」只是**声明**而非事实。**已发生的事故**：某任务的证据绑定在一个后来被 `amend` 掉的提交上（Task 7：reflog `f7033774` → `2103d836`，分支随后 reset 回 `f7033774`）⇒ amend 后「**报告 / 归档 / 清单 / 提交**」四者已无法自洽，绑定旧哈希的证据全部作废 | **方法论缺陷（证据自证性）** | 证据工具的输出必须记录 `git rev-parse HEAD`、`git status --porcelain`（**脏则拒绝运行**）、被读测试文件的哈希、**工具自身哈希**，以及**两份输入（名字清单 + 变异集合）的哈希**。并配合「**分支收尾：证据重跑（R11，必需）**」在收尾时把全部任务的证据重跑一遍 |
| **D17** | **作者编写的变异集合可被静默删减**：变异集合（「本该删掉哪些守卫」）**不可推导**；若放在 gitignored 空间，作者可以**悄悄删掉**一条变异而 `git diff` 看不到 ⇒ **与「可缩小的内嵌清单」是同一类失效，只是上移了一层**。**已实测的实例**：Task 6 的封闭 6 项要求（`task-6-brief.md:961-966`）中，**`:962`（忽略 `cmd.Run()` 的 error）没有任何对应条目**——`m12` 只改错误包装、`m01` 只换二进制，**都不移除退出码检查**；**没有「要求 → 变异条目」映射表，谁都不会注意到** | **方法论缺陷（覆盖可被静默缩小）** | 变异集合**必须 git-tracked**（`tools/trace-mutate/sets/task-<N>.py`）；每份证据附**「要求 → 变异条目」映射表**，使丢条目可审计；名字清单可放证据目录（可推导），但工具必须打印**推导命令**与该文件**哈希** |
| **D18** | **简报里的符号名未在树上验证**：`task-3-brief.md:267`（计划正文同一句在 `:963`）写「调换 `[14:18)` 与 `[18:22)` ⇒ `TestFooterGoldenLayout` 必须转红」，而该名字**在本树不存在**（那是 Task 6 的 `TestSealGoldenLayout`）；真实名是 **`TestFooterGoldenBytes`**（`internal/tracearchive/footer_test.go:35`） | **方法论缺陷（简报可执行性）** | 已按真实名建立条目（`m03_C` 的权威表杀死用例 = `TestFooterGoldenBytes`）；**规则**：简报、计划、侦察里出现的**符号名与行号必须在树上验证后才能依赖**（本报告 §5#5，出处 `progress.md:2888-2891`） |
| **D19** | **派发参数未在树上验证**：派发给 Task 2 / Task 3 的 `PACKAGES` 含 `./tests/tracearchive/`，而该包对清单里的用例名**无匹配** ⇒ 打印 `[no tests to run]`，工具**先判为「证据无效」**（`trace_mutate.py:188-189` / `:226-227`）⇒ 照抄的集合**永远拿不到权威表**（源头是侦察报告的建议） | **方法论缺陷（派发参数未经树验证）** | Task 2、Task 3 两个作者**各自独立发现**并收窄为只列 `./internal/tracearchive/`，把实测输出抄进集合文件头；R11 权威表确认两集合 `PACKAGES` 已收窄、无「证据无效」（本报告 §5#6，出处 `progress.md:2881-2886`） |
| **D20** | **并集变异导致虚假的要求满足**：Task 3 的 `m02_B_五处w改v错误身份退化` 一次性把五处 `%w` 全改 `%v`（**并集**），只证明「全都退化时 5 条用例全红」，对「**任意一处**退化是否也有用例承重」零信息；`REQUIREMENTS` 却把该要求映射到它 ⇒ 映射表声称有对应条目、实际该要求未被验证（**与 D17 同一类失败**，只是发生在映射侧） | **方法论缺陷（映射可被静默稀释）** | **已拆为 `m02_B1..m02_B5`**，每条只改一处；权威表 **5/5 断言红、互不代偿**（提交 `580fe5d9`）。**规则**：要求是「任意一处」时，证据必须是**逐处独立**的条目，不得用并集变异代偿（本报告 §5#7，出处 `progress.md:2903-2915`） |
| **D21** | **报告散文主张未经实测**：`task-2-report.md:130-133` 断言「把 `if delta < 0` 改成 `if false && delta < 0` ⇒ `TestDecodeTrailerRejectsNonMonotonic` 应 FAIL」；实测该改写被紧随的 `out[i] < 0` 守卫接住（差分 -5 使 `out[1]<0`），`errors.Is` 断言仍通过 ⇒ 该主张**不成立** | **方法论缺陷（报告结论未由可重跑证据支撑）** | 本轮补**表驱动子用例 `{10,5}`**（差分为负但偏移仍非负），`m02` 成为**普通红**、`EXPECTED_GREEN` 清空（提交 `20032b9a`）；`task-2-report.md` 已**保留原文 + 附显式更正**（R11 阶段③）。**规则**：报告里任何「删 X ⇒ 用例转红」的主张都必须以**实际运行（含子用例）**为准（本报告 §5#8） |
| **D22** | **用于对齐的预期数字来自非权威产物**：task-5 的控制者预期「断言红 24 + 允许绿 1」抄自 `task-5-evidence/aux-NOT-authoritative-verdict-prediction.txt` —— 该文件绑定**另一个 HEAD**（`20032b9a…`）、**脏树**、集合 sha256 `c6713d9e…`、**无提交绑定**；权威表实为 **22 红 + 2 崩溃 + 1 绿**（该预测把「崩溃/中止」并入了「红」） | **流程缺陷（预期与权威证据混淆）** | 裁决**以权威表为准、控制者预期作废**；执行者**按事实报 22 而非向预期对齐，处置正确**（本报告 §4.7，出处 `progress.md:3129-3137`）。**规则**：任何用于对齐的预期数字必须来自**绑定当前 HEAD 的权威表**；非权威预测只能用于排障 |
| **D23** | **旧证据归档在「超时」判定下仍点名「杀死的用例」**：task-7 旧 `mutation-table.txt` 给 `m27`（`红(超时/挂死)`）的「杀死的用例」列写 `TestReaderReleasesConcurrencySlots`；超时意味着该用例**没有做出终判**，点名它会让人误以为该用例已被判定为红 | **证据表述缺陷（可误读）** | 新权威表统一写 `超时(用例未终判)`；判定桶不变（`红(超时/挂死)`），**不构成漂移**（本报告 §3.2，出处 `progress.md:3383-3385`）。**规则**：**未终判的运行不得产生「杀死的用例」** |
| **D24** | **工具读不到的登记会随扫描结束而消失**：`tools/trace-mutate/sets/task-6.py:387-401` 的 `PENDING` 与 `task-5.py:390-431` 的 `COVERAGE_GAPS` —— 工具**根本不读**（`load_set()` 只强制 `MUTATIONS`/`TARGET`/`PACKAGES`），它们只存在于被跟踪的集合里；若无人在收尾时显式处置，这两条发现就会**随着本轮扫描结束而静默消失** | **方法论缺陷（散文登记不可判定）** | R11 阶段③ **显式结案**：`PENDING`（显式 `sort.Slice` 冗余）结案——冗余成立（`m34` 绿 + `os.ReadDir` 契约）、保留排序为**有意的显式化**；`COVERAGE_GAPS`（`NewWriter` 两个阈值默认值分支零变异覆盖）按裁决**只登记、不扩范围**，其 `how_to_close` 两条 pattern 已预实测。**规则**：工具读不到的登记**必须在收尾时被显式处置**（结案 / 转成可判定条目 / 明确移交）（本报告 §5#2、§5#11，出处 `progress.md:3058-3074`、`3156-3185`） |
| **D25** | **未审阅的推论被当作结论**：上述 `PENDING` 的 `decision` 括号里写「但它既然是冗余的，一个断言返回顺序的用例会让它不再冗余」——该推论**未实测且不成立**：删掉 `sort.Slice` 后 `os.ReadDir` 的顺序**仍然是对的**，那种用例**照样通过** ⇒ 冗余并未被消除（要杀死 `m34` 必须 mock 掉 `os.ReadDir`） | **方法论缺陷（推理未经核实）** | 裁定：加该用例只是**可选文档，不是修复**；并**更正**原推论。**规则**：处置一条登记时**必须审阅它的推论**，推论同样需要证据（本报告 §5#11，出处 `progress.md:3173-3181`） |
| **D26** | **报告出处指错（数字对、引用错）**：R11 `r11-report.md` 初稿有三类 —— (a) §4.3 引用 `task-{5,6,7}-evidence/r11-table.txt`，**该文件名不存在**（真名为 `r11-task-N-table.txt`，如 `task-5-evidence/r11-task-5-table.txt`）；(b) §3.2 把 counterproof 的「仅第 10/28 行不同」误记为 **task-6** 归档，实为 **task-7** 归档；(c) §1.0 把 task-7 的归档 sha256 说成「抄自片段」，而 **task-7 片段并无该值**（8/9 抄录成立，该值是 `shasum` 复算） | **方法论缺陷（可复核性：读者按错引用无法解析）** | 已用**一个路径限定**的更正提交逐处改正；更正时对 4 处引用逐一 `ls` / `sed -n '<行号>p'` 自检可解析（含新写的文件名），并**全文 grep 同类错名**（零命中）。**规则**：报告里每一处引用都必须能被读者解析（文件名存在、行号内容对得上）；「**抄录**」与「**复算**」必须分别标注（本报告 §0.2/§1.0 已改）。**发现者：终审 B（证据链审计轴）** |
| **D27** | **强推断被写成直接证据**：`r11-report.md` §0 把「**9/9 退出码 0**」列为与 HEAD / 工作树同级的**直接事实**；而**原始归档不含工具退出码**（task-6 归档末行的 `[status: completed, exit code: 0]` 是**作业框架追加的页脚**，不是工具 stdout），该信息只来自各 `r11-section.md` 的**自述** | **方法论缺陷（证据分级：推断不得冒充记录）** | 已在 §0 标注为「**强推断，非机器记录**」并写明推断依据（9/9 归档完整 + 9/9 无 `!!!` + stderr 0 字节）。**规则**：凡不是原始归档字节可直接读出的事实，**必须标注证据等级**（记录 / 强推断 / 推论）。**发现者：终审 B（证据链审计轴）** |
| **D28** | **`Seal` 不校验 DT/Inst**：`Append` 有 `validateDT` + Inst 白名单、`ListPendingSeals` 也校验，而**导出 API `Seal` 没有**。实测 `Seal(DT="2026-09-24/../..")` 返回 **nil**，产物落到 `<root>/inst=blue/part-000000.jsonl.zst` ⇒ **越出 `dt=`/`inst=` 布局**，且与 spool 目录**塌缩为同一目录** | **承重**（越出根目录 / 静默错路径） | **已修**（`cb3253f5`）：`sealer.go:154-159` 复用 `validateDT`/`validateInst`（错误沿用 `Append` 风格、无哨兵），新增用例 `TestSealRejectsIllegalDTAndInst`（10 子例：断言错误**文案** + **未产生任何产物** + spool 未动 + 反向断言合法值仍可封块）；F2a 补变异 `m54`/`m55`（均断言红；子例级归因 = 6 个 `dt_*` / 4 个 `inst_*`）。**发现者：终审 A §3②** |
| **D29** | **`MarkSealed` 的次序陷阱 + 文档假前提**：若在 sealer **删掉 spool 之后**调用，`closeCurLocked` 会把 bufio 缓冲 Flush 进**已 unlink 的 inode** ⇒ **静默丢正文**（实测 `Append` 返回成功并给出 `part-000000#row=1`，读它才得 `ErrNotArchived`）；原文档的「未落盘的缓冲不再可能有读者」是**假前提** | **承重**（静默丢正文） | **本轮只改契约文档**（`cb3253f5` + `221f8815`）：`writer.go:395-419` 写明「**只可用于封块前的滚动**」，并写明**不加**「stat spool 是否存在」守卫的理由（正常流程里 sealer 确实已删 spool ⇒ 该守卫会在正常路径误报）；**真实修法 = 计划 2「先滚动、后封块」**（见下方 **N1**）。`MarkSealed` 的次序目前**无用例覆盖**（构造需在 `Append` 前删 spool）。**发现者：终审 A §3①** |
| **D30** | **trailer 条目数无硬上限**：`maxTrailerBytes` 只压 trailer **字节数**，而 `decodeTrailer` 的 `make(..., n)` 上限是 **`n = len(b)` = 64 Mi 条** ⇒ 最坏输入（64 MiB 全 `0x00`，每字节都是合法单字节 varint）给出 `n = 67,108,864`、`make` 分配 **512 MiB**，叠加 `tb`（64 MiB）与解码窗口（512 MiB）≈ **1.09 GiB/请求**；`MaxConcurrent=2` ⇒ ≈2.2 GiB，**超规格 §8.4 C5 的 2 GiB 预算**。实测该输入 `decodeTrailer` **`err=nil` 成功返回**（不是报错） | **承重**（资源预算 / 可达的堆放大） | **已修**（`cb3253f5`）：上界施加在 **`n`** 上 —— `index.go:66-69` `if n > maxTrailerEntries`（常量 `reader.go:55` `= maxTrailerBytes/8` = 8,388,608），超限返回明确 `ErrTrailerCorrupt`、**绝不截断**；新增用例 `TestReaderRejectsTrailerEntryCountOverBound`；F2a 补变异 `m06`（断言红）。**测量数字见 `r11-report.md` §7.9**。**发现者：终审 A §3③** |
| **D31** | **规格 §5.2 自相矛盾**：`spec:154-170`（尤其 `:168`）写 `reserved(6)` 且**无 `row_count`**，实现是 `[18:22)` row_count + `reserved(2)`，而 `spec:459`（§7.1）与 §8.2 **又引用** footer 的 `row_count` ⇒ 规格三处互斥 | 规格缺陷（文档自相矛盾） | **本轮更正规格**（`221f8815`；**不改代码**，实现站在 §7.1/§8.2 与 `plan:715-722` 一侧）：图 `:168` 改为 `row_count(4) \| frame_sha256[:8](8) \| reserved(2)`，补字段定义，并保留「本处曾与实现不一致、**由终审 A 发现**」的说明。**发现者：终审 A §2** |
| **D32** | **5 处注释/说明与实现不符**：① `writer.go`（`O_APPEND` 注释失实 —— `nextSeqLocked` 恒取 `max+1` ⇒ 打开的几乎总是**新**文件，`O_APPEND` 只在 seq > 999999 溢出时兜底）② `errors.go`（`ErrNotArchived` 描述为「产物与 spool 都不存在」，但 `reader.go` 在文件**存在**时也用：行号越界 / 残行 / 未命中行首）③ `footer.go`（字段说明**漏列** frame_sha 不符与 trailer 非邻接）④ `defaults.go`（仍以「本包当前使用的 **Go 库**」讨论窗口 —— 方案 C 后写侧**已无 Go 编码器**）⑤ `sealer.go`（`ErrNotFound` 分支只对含 `/` 的 `ZstdBinary` 可达 ⇒ **非唯一防线**，注释过强） | 文档保真度（各自非承重） | **本轮只改文字、不改行为、不拆 API**，**跨两笔提交**：**`cb3253f5`**（与同文件的代码修复一并落地）① `writer.go:492-501` ⑤ `sealer.go:348-355`；**`221f8815`** ② `errors.go:18-26` ③ `footer.go:21-30` ④ `defaults.go:49-57`。**发现者：终审 A §5** |
| **D33** | **R11 报告两处失实**：(a) §5#4 说 `tests/tracearchive/reader_test.go:597` 的 `TestReaderRejectsTrailerRowCountMismatch`「红在**它自己的前置断言**上」；(b) §4 漏了三处（逐行零覆盖守卫只列 `writer.go:196-201`、未列 `scanSpoolOffsets`→`runZstd` 窗口、未提多个集合 `EXPECTED_GREEN` 为空） | 方法论缺陷（报告结论未经实测 / 遗漏） | **已更正**（F2b；**不改集合、不改代码**）：(a) 实测两法（删 `index.go` 的 `pos != len(b)` 守卫；按注册的 `m17` 换 lenient 解码）都让它在 **`:625` 的真断言**上红（`实际 <nil>`），**不在** `:607`/`:608`；`m03_C` 下它死于 `reader.go:140`「trailer 未紧邻 footer」（**另一守卫**）⇒ **告警项保留、理由更正**（`r11-report.md` §5#4）。(b) 补进 `r11-report.md` §4.11（`reader.go:90/:97/:277` 三处 IO 包装分支零覆盖）/ §4.12（`scanSpoolOffsets`→`runZstd` 窗口静默丢正文）/ §4.13（`EXPECTED_GREEN` 空缺的阅读提示）。(a) 的行号是终审 A 的评审 HEAD `c602e71d`；**当前 `21ffec3f` 上为 `reader_test.go:626` 与 `reader.go:170`**（F1 的行插入使行号平移）。**发现者：终审 A §4、§6** |
| **D34** | **`stat` → `os.Rename` 非原子** ⇒ I4「拒绝覆盖」存在 **TOCTOU**（§7.2 只要求 rename **前**检查）；且 `Seal` **无进程内 `(dt,inst)` 互斥**（§7.2 的「进程内互斥」被降级为**调用方义务**） | **规格级残余**（不修） | **不修**：§7.2 只要求 rename **前**检查 ⇒ 属规格允许的残余；互斥已由 `sealer.go:91-94` **自述为调用方义务**。登记为对**计划 2/4** 的约束（见下方 **N2**）。**发现者：终审 A §3④** |
| **D35** | **`maxTrailerEntries` 的误报边界（措辞已于 F3 更正，见 D38）**：**真触发条件与块的字节大小无关** —— 只要**块内行数 > `maxTrailerEntries`（8,388,608）**，**合法**产物的条目数就会超过上限 ⇒ `GetBody` **响亮误报** `ErrTrailerCorrupt`（**非静默**）。**原条目把触发条件写成「块满 1.5 GiB 且平均每行不足 192 字节」，那是充分条件之一，不是必要条件**（该形态必然超限，但超限不要求该形态）；已被 **64 MiB 反例**证伪：8,388,609 行 × 每行 8 字节 = **64.0 MiB** 即触发（`reader.go:50-56` 的注释已按此更正）。2026-09-24 标定的真实样本约 400 KB/行、每块约 4000 行 ⇒ 余量约 **2000 倍** | **预算取舍**（不修） | **不为它在规格层临时改预算或块大小**（用块大小推上限会更宽 ⇒ **更弱的内存界**，F1 的选择是对的）。登记为**计划 4 的验证项**：在生产/样本上量**每块的最高行数**（而非「平均行大小」），若确有超限的块再回规格层（见下方 **N4**）。**发现者：F1（实测）+ 终审 A §3③ 的推理；边界措辞由聚焦复审（终审 A 第二轮）证伪、F3 更正（D38）** |
| **D36** | **工具「杀死用例」列只到顶层用例名**（`-run` 是**顶层**）：`TestSealRejectsIllegalDTAndInst` 是 10 子例的表驱动用例，工具只写整个顶层名 ⇒ **表里读不出是 `dt_*` 还是 `inst_*` 子例承重**；且该用例断言错误**文案** ⇒ 凡改变失败原因的变异都会踩它（task-6 `m11` FAIL 12→13、`m40` FAIL 5→6），**「杀死该用例」不等于「是该用例的目标守卫」** | **方法论缺陷（证据粒度：子例级归因不可从表读出）** | **规则**：子例级归因**只能靠隔离探针**（F2a 实测：只 cut `validateDT` ⇒ 6 个 `dt_*` 子例红；只 cut `validateInst` ⇒ 4 个 `inst_*` 子例红 ⇒ `m54`/`m55` 的**判别力互相独立**）。已写进 `r11-report.md` §7.6。**发现者：修复轮 F2a** |
| **D37** | **条目数上界原先只在读侧 ⇒ `Seal` 删掉 spool 后正文永久不可达**（本轮最严重）：修前写侧**没有**同一上界（`grep -c 'maxTrailerEntries' internal/tracearchive/sealer.go` = **0**）⇒ 超限形态下 `Seal` 照样返回 **nil**、删除**唯一**的 spool、产物落地；随后 `GetBody` 得 `ErrTrailerCorrupt`（「期望 8388609 条超过条目数上界 8388608」）⇒ **正文永久不可达**（spool 已删、产物读不出来），违反规格 **§7.3「宁可留下 spool」**；诊断方向也错（把「合法但超预算的块」说成「产物损坏」） | **承重**（**数据丢失，不可逆**） | **已修**（`949c3a27`，F3）：`sealer.go:226-229` 的 `if len(offsets) > maxTrailerEntries`（注释 `:206-225`），位于 ② 行数交叉校验**之后**、版本探测 / `MkdirAll`(`:246`) / `Rename`(`:273`) / **删 spool(`:282`)** **之前** ⇒ 被拒时**不建目录、不删 spool、无任何产物**。新增哨兵 **`ErrTooManyRows`**（`errors.go:44-55`）：**不复用** `ErrTrailerCorrupt`（读侧语义＝「产物损坏」，对写侧是**错误诊断**）也**不复用** `ErrRowCountMismatch`（本形态下 `Rows == len(offsets)`，「不符」同样是**错误诊断**）；语义固定为「拒绝封块 + 保留 spool + 无产物（含 `.tmp` 与目录）+ **需人工介入**」，与前两者不同，**单纯重试永远不会成功**。用例 `TestSealRefusesSpoolOverTrailerEntryBound`（`sealer_internal_test.go:77`，A 层，含「恰好等于上界可封块」的反向断言）；变异 `m56`/`m57`（均断言红）。**可达性低**（单块 > 838 万行；今天约 90k 行/天，需约 9,300 行/秒且行极短）但**修的理由成立**：①数据丢失类后果不可逆 ②§7.3 是**明文规格**，违反即缺陷 ③修法很小（同一上界 + 保 spool）。**发现者：聚焦复审（终审 A 第二轮）；修复：F3** |
| **D38** | **边界措辞失实**：D35 与 `reader.go` 的注释把误报触发条件写成「块满 1.5 GiB 且平均每行不足 192 字节」，读起来像**必要条件**；**实测 64.0 MiB 的块即触发**（8,388,609 行 × 8 字节）⇒ 真触发条件是「**块内行数 > `maxTrailerEntries`（8,388,608）**」，**与块的字节大小无关** | 文档保真度（会误导**计划 4 的验证面**：量错对象就漏掉真触发集） | **已更正**（`949c3a27`，F3）：`reader.go:50-56` 改为真触发条件 + **64 MiB 反例**，并把「1.5 GiB / 平均每行 <192 B」降为**充分条件之一**；**D35 与下方 N4 同步更正**；`r11-report.md` §7.9 的旧措辞已加指向 §8.6 的更正标记。**发现者：聚焦复审（终审 A 第二轮）；更正：F3** |
| **D39** | **新用例缺「不得留下目录」断言**：`TestSealRejectsIllegalDTAndInst` 原先只断言「无 `.jsonl.zst` / 无 `.tmp`」⇒ 把入口守卫搬到 `MkdirAll`（`sealer.go:246`）**之后**时用例**仍然全绿**，而越出 `root` 的目录**已经被创建**（正是 `plan:1292` 对 `Append` 明令禁止的「留下目录」） | **测试盲区**（承重不变量无用例约束） | **已修**（`949c3a27`，F3）：加「**调用前后目录集合逐字相同**」断言（`tests/tracearchive/sealer_test.go:1501` 取快照 + `:1531-1534` 比对，红在 `:1532`）；该变体实测 **10/10 子例转红**（**死在目录断言，不是 `err == nil`**）。变异 `m58`（断言红）与 `m54`（6 个 `dt …` 子例）/`m55`（4 个 `inst …` 子例）**互不替代**（判在**不同断言**）。**发现者：聚焦复审（终审 A 第二轮）；修复：F3** |
| **D40** | **提交信息引用的行号与实际落点不符**：提交 `e2cc2207` 的信息写「`plan:3120` 的 `maxTrailerBytes` 说明补条目数上界」，而该提交在**同一文件更早处插入了 11 行** ⇒ `maxTrailerBytes` 说明的实际落点是 **3121**（`git show e2cc2207:docs/superpowers/plans/2026-09-24-tracearchive-storage-primitives.md \| grep -n maxTrailerBytes` ⇒ 3121） | 可复核性（引用行号指错 ⇒ 读者按错引用无法解析） | **无法修正**（`amend` 已禁 ⇒ **留痕**，不靠沉默）。规则同 D16/D26：报告与提交信息里的**行号引用**必须能在被引用的树上解析。**发现者：修复轮 F2b（自报）** |

> **D7 的时效说明**：Task 6 报告 §7.1/§8.1 把「网关 part 滚动」记为待办、把 `minAge` 记为「默认等于 `defaultSealInterval`」；那是 `76042bcd` 时点的状态，已被裁决落实 `e0b0084f`（`minAge` = 3h）与随后的计划 2 要求（R8）取代。**以代码为准。**

---

## 计划 2（网关接入）的对端要求（Task 5/6 交付后追加）

以下三条**不是本计划的交付物**，而是本计划已交付的原语对**计划 2（网关接入 / 请求路径）**提出的要求。登记它们是因为 Task 5/6 的实现在代码注释里已依赖这些前提，若计划 2 不满足，本计划的安全性结论不成立。

| # | 要求 | 依据与后果 |
|---|---|---|
| **R8** | 网关必须**自己周期滚动 part**（拿到新 part 即关闭旧 fd），且**滚动间隔 ≤ 1h**（**严格小于** sealer 的 `minAge` = 3h） | `minAge`（`defaultSealMinAge`）是 sealer 侧唯一的保护，其推导链见 Task 6 的落地修正①。**D7 缺口**：网关与 sealer 之间**没有信号通道**，sealer 封块后会**删除 spool**，而网关若继续往同一 part 追加，字节会写进**已 unlink 的 inode** ⇒ **永久丢失正文**（它们的 `Ref` 还指向一个行数更少的产物）。**缓解不充分之处**：流量停 15 分钟后恢复正是触发条件（spool 的 mtime 早已超过 `minAge`，sealer 认为「已停止追加」，而网关仍持有旧 fd 且未滚动）。注意 `defaultSealInterval = 15m` 只是一个**与滚动间隔同名、本身不滚动任何 part** 的常量——滚动动作必须由计划 2 实现 |
| **R9** | **背压**：磁盘满时网关应**放弃正文并标 `body_missing=true` 继续服务**，而不是持续重试写盘 | §7.1 的【已知代价】正是这一条的依据：否则每个请求都要走一遍「写失败 + 反向扫描 + 改名」，且块序号与目录项**无界增长**、每次 `Append` 付一次 O(n) 目录扫描。这也是本计划**刻意不加计时器/计数器**的原因——正确修法属计划 2 |
| 可选优化（非要求） | `PendingSeals` 持锁做最多 1 MiB 的 `Flush`，与请求路径的 `Append` 争**同一把锁**（`w.mu`） | 慢盘 / ENOSPC 时会把这 1 MiB flush 的延迟加到请求路径上。要彻底消除需把 flush 与请求路径解耦（独立 flush 协程或双缓冲）。**不在本计划内**，登记备查 |

> 与 R8 配套的部署事实：sealer 的 `minAge` 为 3h、块目标 1536 MiB ⇒ 满流量下网关每约 1 小时就该因**大小阈值**滚动一次（这也正是「滚动间隔 ≤ 1h」的来源）；滚动间隔上限同时决定了未封块 spool 的驻留量（约 4.6 GiB，规格 §7.1）。

### 对计划 2 / 3 / 4 的追加约束（终审 A 修复轮 F1/F2 登记 ＋ 聚焦复审修复轮 F3 顺延登记 N5~N7，2026-09-24）

> **为什么要写进本文件**：这些约束来自终审 A 与聚焦复审（终审 A 第二轮）的发现（**D28~D40**），而**评审报告与台账不在一个可长期存活的位置**（`progress.md` 是 gitignored 的，会随会话消失）。**约束必须留在 tracked 的计划里**，否则下一个计划看不到它们。

| # | 目标计划 | 约束 | 依据 / 后果 |
|---|---|---|---|
| **N1** | **计划 2** | **`MarkSealed` 必须先滚动、后封块**：调用方在把块交给 sealer **之前**先调 `MarkSealed` 滚动，**封块期间不再往该 part 追加**。**`MarkSealed` 目前无用例覆盖该次序**（构造需在 `Append` 前删 spool ⇒ 写入侧的 `Append` 会返回成功、正文却进了已 unlink 的 inode） | `writer.go:395-419` 的契约 + `sealer.go` 的 `scanSpoolOffsets`→`runZstd`→`os.Remove(spool)` 流水线（`sealer.go:195/225/256`）。**不满足 ⇒ 静默丢正文**（D29、`r11-report.md` §4.12） |
| **N2** | **计划 2 / 4** | `Seal` 的**进程内 `(dt,inst)` 互斥**是**调用方义务**（`sealer.go:91-94` 自述）；`stat` → `os.Rename` **非原子** ⇒ I4「拒绝覆盖」有 **TOCTOU**（§7.2 只要求 rename **前**检查） | D34；终审 A §3④。**本计划不修**（规格级残余 + 调用方义务） |
| **N3** | **计划 3** | `GetBody` / `Seal` 的**输入校验缺口要一并收口**：`Seal` 的缺口已由 F1 修（D28）；**`GetBody` 的同类缺口**已登记在 `progress.md:1882`，**仍未修** | D28；`plan:4031` 承认 `SealTask` 属调用方输入；终审 A §3② |
| **N4** | **计划 4** | ① **生产 zstd v1.4.8 的压缩比**与 **512 MiB 解码侧内存** ⇒ 验收 §16 第 7 项与 **U1 块大小定稿未闭合**（本机 zstd 是 v1.5.7，测的不是生产版本）② **真 ENOSPC** 与**跨进程并发封块**未构造 ③ **`maxTrailerEntries` 的核实口径必须是「每块的最高行数」，不是「平均行大小」**：在生产/样本上量**每个块的行数最大值**，确认它 ≤ 8,388,608。**触发面是「写入速率 × 15m 时间窗」（`defaultSealInterval`），不是 1536 MiB 的块大小** —— 两者独立，D38 的 **64 MiB 反例**即证。**特别警惕 ENOSPC 期的 5 字节 `null` 标记行**（§7.1 的 `AppendFailed` 标记行极短 ⇒ **小字节量也能堆到高行数**）；若确有超限的块再回规格层（D35） | 终审 A §7；F1 的建议登记 2；`r11-report.md` §7.9 的误报边界 + **§8.6（D38 的更正）** |
| **N5** | **计划 4** | **分类 `Seal` 错误的 `switch` 必须包含 `ErrTooManyRows`**：该哨兵**目前没有生产调用方**（计划 4 的 sealer 循环还不存在；当前全仓唯一的使用点是 A 层用例 `sealer_internal_test.go` 与哨兵互异表 `ref_test.go:91`）⇒ 它的语义**只落在 `errors.go:44-55` 的注释里**。计划 4 若漏掉这一支，超限会被归入「**未知错误**」，丢掉「**需人工介入 / 单纯重试永不成功**」这一唯一的处置信号 | D37；`r11-report.md` §8.6。**不满足 ⇒ 数据丢失类错误被降级为可重试错误** |
| **N6** | **计划 2 / 4** | **缺陷 C 的断言口径有限**：F3 补的是「**调用前后目录集合逐字相同**」，**不是**「某个具体目录不存在」⇒ 它只覆盖 `Seal` **实际走过**的路径。对**本轮变体实测有效**（`m58` ⇒ 10/10 子例红），但**覆盖不到 `Dir(partPath)` 之外**的越界写；计划 2/4 若**扩大写入面**（新增写入点/目录），必须为新的落点**另行加固同类断言** | D39；`tests/tracearchive/sealer_test.go:1501`/`:1531-1534`。**不满足 ⇒ 新落点的越界写无声无息** |
| **N7** | **计划 2 / 4** | **`m03` 重新定点的等价性残余**：v2 的 `m03`（task-5）定点在 `NewWriter` 的**调用点** `if err := validateInst(opts.Inst); err != nil {` → `… && false {`，它**仍然调用** `validateInst` **并丢弃错误** ⇒ 该变异的等价性依赖 `validateInst` **保持纯函数**（**当前成立**：只做白名单判定、无副作用）。**若日后它产生副作用**（归一化入参、写状态、任何状态变更），`m03` 的语义就会**漂移** —— 届时它测的不再是「不校验」 | `r11-report.md` §7.7；`writer.go:74-79`（`validateInst` 的定义）/ `writer.go:211`（调用点）。**不满足 ⇒ task-5 的变异证据静默失效** |

> **与 R8/R9 的关系**：N1 是 R8 在**代码契约层**的具体化（R8 要求网关周期滚动；N1 要求**滚动与封块的先后次序**）；
> N2 是 R8 所述「没有信号通道」的另一面（互斥与 TOCTOU）。**R8/R9 仍然有效**，N1~N7 不替代它们。
> **N5~N7 是 F3 的交接约束**（分别对应 D37 / D39 / §7.7 的 `m03` 残余），它们**不改变 N1~N4 的任何结论**，只是把「F3 本轮已知的能力边界」交给后续计划。

---

## 分支收尾：证据重跑（R11，必需）

> **R11 是必需的收尾步骤，不是可选优化。** 它与「最终全分支复审」「分支收尾」并列执行。

**为什么必需**：`.gitlab-ci.yml` **完全不跑 `go test`**（只有 `go build`）⇒ **变异证据是本分支唯一的验证**，没有任何独立复算。唯一的证据既然由我们自己产出，就必须在收尾时**被重新产出一次**，并且能对得上提交。

**做法**：收尾前用**共享工具** `tools/trace-mutate/`（**通用、非某任务专用**，参数化 `--manifest` / `--mutation-set` / `--packages` / `--target`）**把所有任务的证据重跑一遍**，每个任务归档**一份**权威表；一次非交互调用跑一个任务。

**为什么是必需而非可选（已发生的事故）**：Task 7 发生过 `amend`（reflog 记录 `f7033774` → `2103d836`，分支随后 reset 回 `f7033774`）⇒ **绑定旧哈希的证据全部作废**，而报告与归档在**清单条数**上仍互相矛盾 ⇒ **重跑是让「报告 / 归档 / 清单 / 提交」四者一致的唯一途径。**

**重跑必须满足**（即本计划全局约束里关于证据的全部要求，缺一不可）：`-run` 三条断言与计数判据（计数不等 / 为 0 即为无效）、`SKIP` **全层级**统计、**绑定提交**（HEAD + 工作树干净 + 工具与两份输入的哈希）、**封闭环境**（剔除 `GOFLAGS`）、静态检查类要求各占一行证据、「**要求 → 变异条目**」映射表、以及逐条自证表。

**分支外交付建议（登记备查，非本计划交付物）**：CI 至少应跑一次 `go test ./tests/tracearchive/ -count=1`；否则 8 个任务的变异证据是本分支的**唯一验证且无任何独立复算**。

### R11 执行结果（2026-09-24，冻结 HEAD `c13b64bcc2a344193f854063adbf5a94c0e86760`）

- **范围**：9 个集合（task-2 / task-3 / task-4 / task-4-errors / task-5 / task-6 / task-7 / task-8 / task-8-reader）= **136 条变异**，全部用共享工具在冻结 HEAD 上重跑；每次运行跑**全集**（未传 `--only`/`--skip`），9/9 无「这是子集，不能作为权威表」横幅。
- **最终账**：**124 断言支撑红 + 9 崩溃·超时红 + 3 允许的绿 + 0 未登记的绿 + 0 证据无效 = 136**（`124 + 9 + 3 = 136`，且等于 9 个集合合计之和）；**0 编译红**；9/9 无 `!!!`；`EXPECTED_GREEN` **逐键核对 3/3 通过**。3 条允许的绿 = task-5 `m13`、task-6 `m15`、task-6 `m34`。
- **绑定**：9/9 跑前/跑后 HEAD 一致、工作树跑前跑后均空、工具 sha256 统一 `10dea750…`、退出码 0、stderr 0 字节；权威表归档在 gitignored 的 `task-N-evidence/r11-*.txt`（**证据归档不进版本库**，只汇总进 `r11-report.md`）。
- **无漂移**：9/9 集合均报「并发运行未造成判定漂移」；task-8 有**量化余量** —— 独立时序探针整包 **9.632s vs 固定 30s ≈ 3.1×**（最慢单条 5.01s）。两处**非漂移**差异已如实登记（task-7 `m27` 的「杀死用例」辅助列、task-6 归档第 10/28 行）。
- **反证结论**：在父进程注入 `GOFLAGS=-exec=<伪造 wrapper>` 后重跑 task-7 全集 ⇒ 剔除行列 `GOEXPERIMENT, GOFLAGS`（干净跑为 `（无）`）、wrapper 调用 **0 次**、32 行判定表**逐字节相同** ⇒ **迁移关闭了 `GOFLAGS` 伪造路径（已测量）**（机制：`trace_mutate.py:97-105` 白名单 + `:272` `run_tests(env=env)`）。
- **Task 1 豁免**：按设计无变异证据（产物仅几个常量；唯一候选是对 GC/噪声敏感的 `runtime.MemStats.HeapInuse` 标定断言且**未实测**；工具拒绝「只有 `CHECKS` 没有 `MUTATIONS`」的集合）。
- **阶段③ 产出清单**：`r11-report.md`（六节：逐集合权威表 / 总计与算术闭合 / 并发漂移核对 / 已知限制与阅读陷阱 / R11 发现汇总 **15 条**（13 条本体 + 终审 B 发现的 2 条出处/证据分级缺陷） / Task 1 豁免登记）＋ 本缺陷登记表的 **D18~D27** ＋ Task 1–6 报告入库与 `task-2-report.md:130-133` 的显式更正 ＋ 本节执行结果。
- **仍然成立的外部风险**：CI 依旧**不跑 `go test`** ⇒ 上述 9 张表仍无任何独立复算；`r11-report.md` 的 §4 已逐条列出读表时必须注意的陷阱（含 `CHECKS = []` 时「静态检查: 全部 PASS」是**空真**、`names.txt` **未由提交内容锚定**）。

### R11 v2（修复轮后重跑，2026-09-24，冻结 HEAD `21ffec3f42eb22981f67ba2cf4de90d54ee4ba64`）

> **完整汇总见 `r11-report.md` §7**（新增一节，与 §0–§6 的 v1 并列）。**v1 的九张表仍保留在 `c13b64bc` 上，未被覆盖**。

- **为什么重跑**：F1（`cb3253f5`）改了 `sealer.go` / `writer.go` / `index.go` / `reader.go` 与两个测试文件 ⇒ 按本项目**证据绑定文件哈希**的原则（D16）**全部 v1 证据失效**；**注释类改动也重跑**（**F1 两笔提交都含纯文字改动**：`221f8815` 全篇只改文字；`cb3253f5` 因同文件**内联**了 `writer.go` 的两处注释更正与 `sealer.go` 的注释收敛 —— **文本变了哈希绑定就断了**，task-3 / task-4-errors / task-8-reader 就是「行为零漂移、绑定层确实变了」的直接样本）。
- **新冻结点**：`21ffec3f…` = `221f8815` + `62de2d22`（补 `sets/task-2.py` 的 `m06`、`sets/task-6.py` 的 `m54`/`m55`）+ `21ffec3f`（`sets/task-5.py` 的 `m03` 重新定点）。工具 sha256 与 v1 相同（`10dea750…`）。
- **v2 九表**：task-2 **6** / task-3 7 / task-4 6 / task-4-errors 1 / task-5 **22+2+1=25** / task-6 **48+3+2=53** / task-7 28+4=32 / task-8 3 / task-8-reader 6 ⇒ **127 断言红 + 9 崩溃·超时 + 3 允许绿 + 0 未登记绿 + 0 无效 = 139 = 136（v1）+ 3 条新变异**。
- **零漂移**：全 9 表 **0 编译失败、0 子集横幅、0 `!!!`、0 未登记绿、0 证据无效**；`EXPECTED_GREEN` 逐键核对全部落在允许的绿（task-5 `m13`；task-6 `m15`/`m34`）；崩溃/超时成员与 v1 **同名同数**；其余集合**逐条零判定漂移**。
- **三个新条目**（全为**断言支撑红**）：task-2 `m06_删除条目数上界守卫`（杀 `TestReaderRejectsTrailerEntryCountOverBound`）、task-6 `m54_删除DT入参校验` 与 `m55_删除Inst入参校验`（均杀 `TestSealRejectsIllegalDTAndInst`）。**子例级归因靠隔离探针**：`m54` ⇒ 6 个 `dt_*` 子例红、`m55` ⇒ 4 个 `inst_*` 子例红（⇒ 两处调用各自承重，故不合并成一条）。
- **`m03` 重新定点规则**（F2a 唯一超出「补三条」的改动，单独提交 `21ffec3f`）：F1 把 Inst 白名单抽成 `validateInst` 并让 `NewWriter` 调用 ⇒ `m03` 原模式在 HEAD 上 count=0 ⇒ `--check` **PATTERN-MISS** ⇒ **task-5 整轮无法出表**。改定点到 `NewWriter` 的**调用点**（`… && false {`），**刻意不动 `validateInst` 本身**（动它会连带停用 `Seal` 入口校验 = task-6 `m55` 的范围 ⇒ **跨集合证据串味**）。**控制者固化规则 —— 五个条件全部成立才可重定点**：①守卫**被重构/搬家**（不是「跑不通就改到能跑通」）②新定点**保持语义与 blast radius**（并说明为何不能改别处）③`--check` 在 HEAD 上**唯一定位** ④**v1↔v2 的判定/杀死用例/自证三列逐字相同** ⑤**写进集合文件头 + 条目注释 + 单独提交**；**任一不成立（尤其 ②④）⇒ 停下报告，不要重定点**。
- **两条方法学规则**（本轮形成）：①表的「杀死用例」列是**顶层用例名**（工具的 `-run` 是顶层）⇒ **子例级归因只能靠隔离探针**；②`m54`/`m55` **不互斥于** `m11`/`m40`（新用例断言错误**文案** ⇒ 凡改变失败原因的变异都会踩它），但**判别力互相独立**（6 dt / 4 inst 子例）。
- **仍成立的外部风险**：CI 依旧**不跑 `go test`** ⇒ 两套表（v1 与 v2）都**没有独立复算**。

### R11 v3（写侧上界修复轮 F3 后重跑，2026-09-24，冻结 HEAD `ea962a70bf18016842886c6a9a35756191167d79`）

> **完整汇总见 `r11-report.md` §8**（新增一节，与 §0–§6 的 v1、§7 的 v2 并列）。**v1 与 v2 的九张表**分别仍保留在 `c13b64bc`、`21ffec3f` 上，**均未被覆盖**（v1 / v2 / v3 各 9 张表，共 27 张）。

- **为什么重跑**：F3 改了 `internal/tracearchive/{errors,reader,sealer,sealer_internal_test}.go` 与 `tests/tracearchive/{ref,sealer}_test.go`（`949c3a27`）以及 `tools/trace-mutate/sets/task-6.py`（`ea962a70`）⇒ 按 D16 的**证据绑定文件哈希**原则，v1/v2 两套表全部失效；**注释与测试改动同样重跑**（D38 只改 `reader.go` 注释、D39 只改测试断言，都断了绑定）。
- **新冻结点**：`ea962a70…` = `949c3a27`（F3 阶段一：写侧守卫 + 新哨兵 + 测试加固）+ `ea962a70`（F3 阶段二：补 `sets/task-6.py` 的 `m56`/`m57`/`m58`）。工具 sha256 与 v1/v2 相同（`10dea750…`）。
- **v3 九表**：task-2 **6** / task-3 7 / task-4 6 / task-4-errors 1 / task-5 **22+2+1=25** / task-6 **51+3+2=56** / task-7 28+4=32 / task-8 3 / task-8-reader 6 ⇒ **130 断言红 + 9 崩溃·超时 + 3 允许绿 + 0 未登记绿 + 0 无效 = 142 = 139（v2）+ 3 条新变异**。
- **零异常**：全 9 表 **0 编译失败、0 子集横幅、0 `!!!`、0 未登记绿、0 证据无效**；`EXPECTED_GREEN` 逐键核对全部落在允许的绿（task-5 `m13`；task-6 `m15`/`m34`）；崩溃/超时成员与 v1/v2 **同名同数**（task-5 `{m10,m33}`、task-6 `{m16,m17,m21}`、task-7 `{m10,m11,m27,m28}`）。
- **零漂移**：**8 个集合逐条零漂移**（task-2 / task-3 / task-4 / task-4-errors / task-5 / task-7 / task-8 / task-8-reader）；task-6 53→56，共同 53 条**零判定漂移**，仅 `m11` 的 FAIL 数 13→14、`m11`/`m17` 的杀手列各多一条新用例，清单 35→**36**（推导驱动）。
- **三个新条目**（全为**断言支撑红**）：`m56_删除Seal行数上界守卫` 与 `m57_行数上界改用行数不符哨兵`（均杀 `TestSealRefusesSpoolOverTrailerEntryBound`）、`m58_入口守卫搬到MkdirAll之后`（杀 `TestSealRejectsIllegalDTAndInst`）。**隔离探针**：`m56`/`m57` 只红新用例的 **2 个「超限」子例**（「恰好等于上界/可封块」子例仍绿）；`m58` 让 10 个子例全红，但**死在目录断言 `tests/tracearchive/sealer_test.go:1532` 而非 `err == nil`** ⇒ 与 `m54`（6 个 `dt …` 子例）/`m55`（4 个 `inst …` 子例）**互不替代**。
- **本轮修复**：D37 / D39 已修、D38 已更正（提交 `949c3a27`）；D40 无法修正 ⇒ 留痕。D35 与 N4 的失实边界措辞已按 D38 同步更正。
- **仍成立的外部风险**：CI 依旧**不跑 `go test`** ⇒ 三套表（v1 / v2 / v3）都**没有独立复算**。

---

## 自检

**1. 规格覆盖度**（本计划范围内的规格条目逐项核对）：

| 规格条目 | 对应任务 |
|---|---|
| §4.2 `internal/tracearchive` 包 | 任务 1~8（全包） |
| §5.1 目录布局 `dt=/inst=/spool/` | 任务 4 |
| §5.2 产物物理布局（帧+trailer+footer）| 任务 2、3、6（含 golden 逐字节断言）|
| §5.3.1.1 编码器裁决为方案 C（C 版 zstd CLI）| 任务 1（标定证据）、任务 6（`os/exec` 调 CLI，参数 `-19 --long=29`）|
| §5.3.3 参数终值（19 / 512MiB = `--long=29` / 1.5GiB / 15min）| 任务 1（`defaults.go`）|
| §5.3.3 内存口径（CLI ≈ 窗口 × 1.08）| 任务 1（证据）、任务 6（编码内存不落网关）|
| §5.4 定位键为复合键而非偏移 | 任务 4 |
| §5.5 冷层格式预留（format_version）| 任务 3 |
| §7.1 请求路径只追加不压缩、周期性 flush、`AppendFailed` 写 `null` 标记行 | 任务 5 |
| §7.2 封块流程（CLI 子进程 + trailer/footer 由 Go 追加 + mtime 发现机制）| 任务 6 |
| §7.3 写指针以目录内容为权威 | 任务 4（`parsePartSeq`）、任务 5（`nextSeqLocked`）、任务 6（`ErrPartExists` 拒绝覆盖）|
| §8.1 错误分类必须可 `errors.Is` 区分 | 任务 2/3/4/6/7（哨兵错误 + 全量身份断言）|
| §8.2 读取算法（产物→spool）| 任务 7 |
| §8.3 C1~C4 空间硬约束 | 任务 7（实现）、任务 8（断言）|
| §8.4 C5 并发上限 | 任务 7（`sem`）、任务 7 的 `TestReaderConcurrencyIsBounded` |
| §9.2 块级失败语义（子进程非零退出 = 封块失败）| 任务 6（`ErrSealFailed` + 子进程失败用例）|
| §10.1 I1~I5 不变量 | 任务 5/6（实现）、任务 6 的原子性与拒绝覆盖测试 |
| §10.2 崩溃窗口 | 任务 5（`nextSeqLocked` 恢复）、任务 6（失败清理 .tmp）|
| §11.1 sealer 为宿主机 unit / 网关镜像零改动 | 不在本计划范围（计划 4）；本计划只保证 `SealerOptions.ZstdBinary` 可配置 |
| §11.1 zstd 版本锁定（v1.4.8）| 任务 6（**已由代码强制执行**：`SealerOptions.ExpectedZstdVersion` + 封块前探测，四条错误路径全部以 `ErrSealFailed` 响亮失败，已落地在 `e0b0084f`）|
| §7.2 残行守卫 / `minAge`=3h / `ListPendingSeals` 目录名校验与方法形态 | 任务 6（均已在 `76042bcd`/`e0b0084f` 落地）|
| §7.1 行校验（拒绝而非转义）与写失败隔离四步 | 任务 5（已落地在 `771d7985`）|
| §8.2 R10（`frame_sha256` 校验、spool 末行终止符守卫）| 任务 7（**要求/待落地**：`reader.go` 尚未实现，见任务 7 的 R10 块）|
| §15.2 生产侧验收清单（P1~P3）| 不在本计划范围（生产机验收）|
| §14.1 变异测试（每道防线摘除必须转红）| 任务 2/3/5/6/7 的「变异自检」小节 |
| §14.2 A1 逐字节往返 | 任务 8 |
| §14.2 A4 零空间增量 | 任务 8 |
| §15 U7/U8 标定（已实测、已裁决）| 任务 1（证据留档）、任务 6（采用 CLI）|

**不在本计划范围**（属其它计划，此处仅作说明，避免执行者误判为遗漏）：
- §6 PG schema 变更 → 计划 3
- §8.1 HTTP 接口、CLI → 计划 3
- §11 部署变更（sealer 的 systemd unit、网关容器的 `/datalog` bind mount、`Dockerfile` 不动）→ 计划 4
- **sealer 进程的 `main` 与循环入口** → 计划 4（本计划只产出 `Sealer`、`ListPendingSeals` 与 CLI 子进程调用；任务 8 的端到端测试扮演该循环）
- §5.3.4 块大小 U1 实验 → 需真实生产数据与延迟测量，属计划 4 的灰度前验证（本计划取 `1.5 GiB` 为暂定值）
- 规格 §8.4 C6（读 spool 不受并发限制）→ 已由任务 7 的实现顺序自然满足（spool 路径不占用 `sem`）

**2. 占位符扫描**：全文无 TBD/TODO/FIXME；每个代码步骤均含可编译的完整代码块。

**3. 测试命令与断言自检（方案 C 修订新增）**：
- 全文不再出现 `-run TestXxx` 单模式命令；每个任务的 RED/GREEN 命令都显式列出该任务的全部测试名（`|` 连接）。理由：Go 的 `-run` 是**子串匹配**，`-run TestTrailer` 会漏掉三个 `TestDecodeTrailer*`、`-run TestFooter` 会漏掉四个 `TestDecodeFooterRejects*`、`-run TestReader` 会漏掉新增的 `TestReaderRejectsOversizedTrailerLen`。
- 每个「应当失败」的断言都带**错误身份**：`ErrTrailerCorrupt`（任务 2）、`ErrFooterCorrupt`（任务 3、7）、`ErrPartExists` / `ErrRowCountMismatch` / `ErrSealFailed`（任务 4 定义、任务 6 使用）、`ErrNotArchived` / **`ErrBodyMissing`**（任务 4 定义、任务 7 使用：标记行 ⇒ `ErrBodyMissing`，规格 §8.2）。无哨兵的参数校验错误（`NewWriter` 的空 Root、**`Inst` 白名单**、**`Append`/`AppendFailed` 的 `dt` 路径分量校验**、`NewSealer` 的空 Root、`zstdLongArg` 的非 2 的幂窗口）只断言「必须报错」，已在计划中注明理由。
- 三条 **golden 逐字节**测试各覆盖一处格式契约：trailer 差分 varint（任务 2）、footer 32 字节字段布局（任务 3）、产物整体「帧+trailer+footer」（任务 6）。它们与「往返测试」互补——往返测试对**对称的**布局 bug 是盲的。
- **方案 C 的新增测试依赖**：任务 6/7/8 的封块类用例需要本机 `zstd`（`requireZstd` 缺失即 skip），CI 镜像必须安装；失败语义与字节布局用例用假 `zstd` 脚本，不依赖真实版本。

**4. 类型一致性核对**：
- `Ref` 字段名 `DT`/`Inst`/`Part`/`Row` 在任务 4 定义，任务 5、6、7、8 使用一致。
- `SealTask` 字段 `DT`/`Inst`/`Part`/`SpoolPath`/`Rows`/`UncompressedBytes` 任务 5 定义，任务 6 使用一致；**`Rows == 0` 表示行数未知**（任务 6 的 `ListPendingSeals` 不填行数），任务 6 据此跳过行数交叉校验。
- `footer` 字段 `TrailerOffset`/`TrailerLen`/**`RowCount`**/`FrameSHA` 任务 3 定义（`RowCount` 编在 `[18:22)`），任务 6、7 使用一致；`FooterSize` 同一任务定义，任务 6、7 引用。**任务 3 的「对外产出」必须写出 `RowCount`**：漏了它，任务 6 不会写 `row_count`，任务 7 也拿不到传给 `decodeTrailer` 的期望条目数（那是识别 trailer 截断的唯一依据）。
- `WriterOptions` 含 `Root`/`Inst`/`BlockBytes`/`SealInterval`；`SealerOptions` 含 `Root`/**`ZstdBinary`**/`ZstdLevel`/`ZstdWindowBytes`/**`MinAge`（0 取 `defaultSealMinAge` = 3h，已落地在 `e0b0084f`）**/**`ExpectedZstdVersion`（0 取 `defaultZstdVersion` = `"1.4.8"`，已落地在 `e0b0084f`）**；`ReaderOptions` 含 `Root`/`ZstdWindowBytes`/`MaxConcurrent`——三者字段名不冲突。
- `Writer.MarkSealed(dt string, part int)` 任务 5 定义，任务 8 使用一致。
- `Paths` 方法 `PartFile`/`SpoolFile`/`PartDir`/`SpoolDir` 任务 4 定义，任务 5、6、7 使用一致。
- 读取侧错误变量 `ErrPartNotFound`/`ErrBodyMissing`/`ErrNotArchived` 任务 4 定义；任务 7 使用 `ErrNotArchived`、**`ErrBodyMissing`（标记行 ⇒ 正文从未落盘，规格 §8.2）** 与 `ErrFooterCorrupt`（任务 3 定义），均以 `%w` 包装，与 `errors.Is` 兼容。
  - **已知留白的更新（Task 5 交付后）**：原先这里写着「`ErrPartNotFound` 与 `ErrBodyMissing` 在任务 7 的 `Reader` 内**未被使用**……两个哨兵都给计划 3 的服务层」。现在只对了一半：`ErrBodyMissing` 的定义注释是「正文在写入时即失败，从未落盘。对应 HTTP 404」——**这正是 `AppendFailed` 的语义**，标记行落地后任务 7 的 `Reader` 必须直接使用它（产物路径与 spool 路径遇到 `null` 标记行都返回 `ErrBodyMissing`，规格 §8.2），不能把它留给服务层去组合判定。**`ErrPartNotFound` 仍未被任务 7 使用**，它（配合 `body_missing` 元数据列实现规格 §8.1 的 410 语义）留待计划 3 的服务层。这不是本计划缺陷。
- 封块侧错误变量 `ErrPartExists`/`ErrRowCountMismatch`/`ErrSealFailed` 任务 4 定义，任务 6 使用一致（全部 `%w` 包装；测试用 `errors.Is` 断言身份，`TestSealFailsOnNonZeroExit` 另有 `errors.As` 取 `*exec.ExitError` 断言退出码）。
- ~~`ListPendingSeals(root string, minAge time.Duration) ([]SealTask, error)`~~ → **最终形态是方法 `(*Sealer).ListPendingSeals() ([]SealTask, error)`（任务 6 定义，已落地在 `e0b0084f`）**：`minAge` 取自构造时的 `SealerOptions`；它填的 `SpoolPath` 是绝对路径，同包的 `Seal` 直接使用。包级函数**已删除**（详见任务 6 的「落地修正」⑤），本节所有提到包级签名的测试片段落盘时都要改成「构造 `Sealer` + 调方法」。
- `maxTrailerBytes` 任务 7 定义、仅任务 7 使用；`TestReaderRejectsOversizedTrailerLen` 直接引用它构造边界值，因此该 const 不能内联为字面量。

**5. 测试摆放分层核对（混合方案，逐任务）**：

| 任务 | A 层（`internal/tracearchive/`，`package tracearchive`） | B 层（`tests/tracearchive/`，`package tracearchive_test`） | A 层的理由（必须触达未导出符号） |
|---|---|---|---|
| 1 | `calib_test.go` | 无 | 引用 `defaultZstdLevel`/`defaultZstdWindowBytes`/`defaultBlockBytes`/`defaultSealInterval` 常量 |
| 2 | `index_test.go` | 无 | `encodeTrailer`/`decodeTrailer`/`appendVarint` 均未导出 |
| 3 | `footer_test.go` | 无 | `footer` 结构体与 `encodeFooter`/`decodeFooter` 未导出（`FooterSize` 导出） |
| 4 | `ref_internal_test.go` | `ref_test.go` | `parsePartSeq` 未导出 |
| 5 | `writer_internal_test.go`（**交付时新增，见任务 5「落地修正」④**） | `writer_test.go` | 写失败注入必须替换未导出的 `cur.w`；`MarkSealed` 的 fd 关闭回归必须持有未导出的 `cur.f`（原本记的「无 A 层」是错的） |
| 6 | `sealer_internal_test.go` + `testhelpers_test.go` | `sealer_test.go` + `helpers_test.go` | `s.failBeforeRename` 测试钩子、`parsePartSeq` 交叉验证 |
| 7 | `reader_internal_test.go` + 追加 `testhelpers_test.go` | `reader_test.go` + 追加 `helpers_test.go` | `maxTrailerBytes` 上界、`footer`/`encodeFooter` 构造损坏产物、`r.inFlight()` |
| 8 | 无 | `roundtrip_test.go` + `constraints_test.go` | —— |

- **A 层共 6 份测试文件**（任务 1/2/3/4/5/6/7 里标 A 的那些；任务 8 完全没有 A 层文件——**任务 5 的 `writer_internal_test.go` 是交付时新增的，原表漏记**），**B 层共 6 份**（任务 4/5/6/7/8）。每一处 A 层例外都在对应的测试函数注释里写明了「为什么必须触达未导出符号」。
- 任务 2/3 的代码**已经交付**且测试留在包内：这符合上面的 A 层判据，**不需要返工**，仅在此登记为「有意为之」。
- **测试辅助在两层的重复是设计决定**：`writeSpool`/`requireZstd`/`fakeZstd`（任务 6）、`sealLines`（任务 7）在 `internal/tracearchive/testhelpers_test.go` 与 `tests/tracearchive/helpers_test.go` 各有一份。Go 无法跨包共享 `_test.go` 辅助；若发现同层内重复定义，合并进该层的 helper 文件即可。
- 转换纪律：每个 B 层代码块顶部都写了「落盘时加 `ta.` 前缀」的清单；**漏加会直接编译失败**，所以这条规则是可自动校验的，不靠人工记忆。

**已知的实现细节留白（非计划缺陷，属执行者的正常判断）**：测试中使用的 `writeSpool`、`sealLines`、`countLines`、`dirUsage`、`requireZstd`、`fakeZstd`、`decodeVarints` 是测试辅助函数：前四个跨层重复（见上），`countLines` 只属任务 5 的 B 层文件，`decodeVarints` 只属任务 6 的 B 层文件（在测试里独立实现一遍 varint 解码，用于不依赖未导出符号地校验 trailer）。
