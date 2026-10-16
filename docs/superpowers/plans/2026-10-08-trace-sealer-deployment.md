# 正文封块器（sealer）与生产部署实施计划（计划 4/4）

> **面向 Agent 执行者：** 必需子技能：使用 superpower-subagent-driven-development（推荐）或 superpower-executing-plans 按任务逐项执行本计划。步骤使用复选框（`- [ ]`）语法进行跟踪。**动手前必须先读 `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md` 的 §5.3.3 / §5.5 / §7.2 / §7.3 / §8.3 / §8.4 / §11 / §14.2 / §15.1 / §15.2 / §16，以及本计划文末的「规格缺陷登记 D64–D74」与「交接清单（计划 3 读侧 / 计划 5 收尾）」。**

**目标：** 把 `/datalog` 上的 spool 变成不可变产物：新增宿主进程 `trace-sealer`（复用冻结包 `internal/tracearchive` 的 `Sealer`，调用宿主机 C 版 `zstd -19 --long=29 -T1`），把它装成 systemd unit，给生产编排补上 `/datalog` bind mount，给出蓝绿切换与旧归档定时器的隔离/回滚剧本，并完成 A3 / A6 / A7 / A8 与「撤掉挂载 ≤1 tick 闩锁」四项生产验收。

**架构：** 网关（计划 2）只做「顺序追加 spool + 周期 flush + 到阈值滚动」，**网关内不压缩**；本计划的 sealer 是**宿主机上的独立进程**（不进容器、不改任何 Dockerfile），它每轮 `ListPendingSeals()`（mtime + `minAge=3h` 判定「已停止追加」）→ 逐个 `Seal()` → 由冻结包产出 `part-NNNNNN.jsonl.zst` 并删除 spool。**次序不变量是承重的**：网关必须先 `MarkSealed` 滚掉旧 part（关 fd），sealer 才可能遇到它；`minAge(3h) > 网关滚动上界(1h1m)` 是跨进程之间**唯一**的保护（规格 §15.1 R8 / D7）。进程形态选 **`Type=simple` 常驻循环**（`--once` 供离线标定与排障），因为规格 §11.1 给的 `Restart=on-failure` 只有在 simple 形态下才有意义，且常驻进程天然不产生「定时器在重启后漏拍」这一类问题。

**技术栈：** Go 1.26、`github.com/example/llm-gateway/internal/tracearchive`（计划 1，**只读复用**）、`log/slog`、`syscall.Flock`、`/proc/self/mountinfo`、systemd（`systemd-analyze verify`）、docker compose v2、回环 ext4（验收用）、`tools/trace-mutate/trace_mutate.py`（计划 1 的变异工具）。

**规格：** `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md`（论证以规格为准；**规格与本计划冲突处以本计划的「规格缺陷登记」裁定为准**，并把裁定理由写进交付报告）。

---

## 全局约束

- **范围（本计划交付什么、不交付什么）**：本计划 = ① `cmd/trace-sealer` + `internal/tracesealer`；② systemd unit 与旧归档定时器隔离；③ `/datalog` bind mount 与编排变更；④ 蓝绿切换剧本；⑤ 验收 A3 / A6 / A7 / A8 + 「撤掉挂载 ≤1 tick 闩锁」；⑥ 运行手册条目。**不交付**：读侧（点读服务，计划 3）、冷层合并（§5.5）、A2/A4/A5（依赖读侧）、任何 PG schema 变更、`docs/guardrails.md` 的订正；**A1 的读回半部（n = 1e6）物理测量**按发起者裁定（计划 3 的 **E52**）归**任务 9**，口径见计划 3 的交接 **H19**，环境 = 生产宿主 + 真实 PG + 真实 `zstd` **1.4.8** ⇒ 本计划只把它登记为「待宿主执行」（见 **E71**）。
- **冻结包零改动（铁律）**：`internal/tracearchive/` 附有 142 条变异证据（R11 v3，冻结提交 `ea962a70`）。本计划**不得**新增/改名/改行为其中任何文件；只以公开 API 复用（`NewSealer` / `SealerOptions` / `Seal` / `ListPendingSeals` / `SealTask` / `ErrPartExists` / `ErrRowCountMismatch` / `ErrTooManyRows` / `ErrSealFailed` / `Ref` / `Paths` / `Writer` / `Reader`）。交付报告必须以 `git diff --stat 3a2301ad..HEAD -- internal/tracearchive/` **输出为空**为证（命令与真实输出一起抄）。
- **计划 2 的代码同样「非必要不改」**：`internal/gateway/observability/{trace_line.go,spool_gate.go,spooler.go,trace_event.go}` 与 `internal/gateway/config/trace_body.go` 是计划 2 变异集合（`p2-task-2..8`）的被测文件。本计划**默认不改**它们；**只有**任务 10 的 A8 决策门被触发（实测证明「块内行数会超 `maxTrailerEntries`」）时才允许改 `spooler.go`，届时 `p2-task-5` / `p2-task-8-spooler` 的绑定立即失效 ⇒ **必须重跑这两个集合并重新记录 sha256**（见全局约束「变异证据绑定提交」）。
- **跨进程次序不变量（R8 / D7）**：网关先滚动（`MarkSealed`，关 fd）→ sealer 才可能封块。网关滚动间隔 `TRACE_BODY_ROLL_INTERVAL=1h`（计划 2 已强制 ≤1h 且 <3h），实际滚动上界 = `1h + flush(1m) = 1h1m`。sealer 侧唯一的保护是 `minAge`，因此 `internal/tracesealer` **必须**对 `--min-age` 施加强制下界：`min-age > 1h`（R8 的网关滚动上界），违反即启动失败；默认值 `3h`（= 冻结包 `defaultSealMinAge`）。
- **sealer 的失败语义（逐条，规格 §7.2/§9.2 + 冻结包 `errors.go`）**：
  - `ErrPartExists` → **幂等无害**：不告警（或仅 INFO 计数），下轮仍会看到同一个 spool，不做任何补救动作；
  - `ErrRowCountMismatch` / `ErrSealFailed` → **告警 + 保留 spool + 下轮重试**（`ErrSealFailed` 覆盖 zstd 二进制缺失、版本不符、子进程非零退出、残行、路径不一致）；
  - `ErrTooManyRows` → **永久拒绝**：重试**永远不会**成功（同一 spool 重扫多少次行数都一样），必须告警 + 明确标注「需人工介入」，且**永不封、永不删**（N5 义务）。它的 spool 会**永久**留在磁盘上（点读仍可从 spool 读到，属响亮降级）。
  - `ErrPartNotFound` / `ErrNotArchived` / `ErrBodyMissing` 是**读侧**哨兵，sealer 侧不得使用、不得包装。
- **越界块的上界是镜像常量，必须被钉住**：`maxTrailerEntries = maxTrailerBytes/8 = 8,388,608`（冻结包 `reader.go:62`，**未导出**）。`internal/tracesealer` 只能镜像它，**必须**用测试从 `internal/tracearchive/reader.go` 现读源码、正则取出 `maxTrailerBytes` 与 `maxTrailerEntries` 并断言与镜像常量相等。同理钉住 `defaultSealMinAge=3h` 与网关滚动上界 1h。**镜像常量一旦与冻结包漂移，测试必须红**。
- **sealer 绝不建目录**（D70 / 2026-09-24 事故同类）：`internal/tracesealer/` 与 `cmd/trace-sealer/` 不得出现任何建目录调用形态（静态检查：正则 `\bMkdir[A-Za-z]*\s*\(` 必须 absent，注释里也不写该形态）。冻结包自己在**每次建块**时会 `os.MkdirAll`（`writer.go:489`），所以「用建目录来修复缺失的 root」会把消失的挂载点在**宿主根文件系统**上重建出来 —— 这正是 `/` 被写满那一类事故。根目录在本进程启动时不存在 ⇒ **启动失败**；运行中消失 ⇒ **本轮中止、不调用 Seal**。
- **功能开关语义（计划 2 已实现，本计划只是部署它）**：`TRACE_BODY_ROOT` 为空或全空白 ⇒ 功能关闭 ⇒ 正文仍写 PG 的 jsonb 列、`body_missing=false`、行为与本计划之前**逐字节相同**。默认值为空。
- **两种降级不对称（承重口径，全计划一致；写反了就会把「丢正文」当「无损」）**：
  - `sink == nil`（功能关闭，或 `NewTraceSpooler` 构造期失败）⇒ 正文走既有路径落 PG 的 jsonb 列 ⇒ **无损**；
  - `sink != nil` 但 spool 写入失败 / 根身份闩锁（挂载消失、换盘、写失败）⇒ 该行 `body_missing=true`、四个定位键全 NULL，正文**既不进 `/datalog` 也不回退 jsonb**（`internal/gateway/observability/trace_event.go` 的 `buildTraceModels` ① 失败分支）⇒ **丢正文**。
  - 因此「把写侧关掉让正文回落 jsonb」是**事后补救动作**（只对之后的行生效，闩锁窗口内已丢的行找不回来），**不是**「切流/撤挂载无损」。任何地方（运行手册、剧本、报告）都不得把它写成无损降级。把回退 jsonb 做成默认行为属**候选改进 I1**（登记在「规格缺陷与改进登记」一节，本计划不实现）。
- **启用写路径的硬门前置（D73，本计划最重要的一条）**：**同一 root 的读侧可用之前，不得设置 `TRACE_BODY_ROOT`**（读侧 = 计划 3，其计划文档已存在于 `docs/superpowers/plans/2026-09-25-trace-point-read-service.md`，但**尚未实现**）。理由（已实测）：全仓 `tracearchive.Reader` / `GetBody` **只被测试引用**，没有任何生产调用者 ⇒ 一旦开启写侧，新行的正文只进 `/datalog`，管理面与任何 API 都取不到正文（`body_ref_*` 有值、jsonb 列为 NULL）⇒ **静默的用户可见丢失**。本计划因此把启用拆成「挂载与进程就位（功能关闭）」与「开启（读侧就绪后）」两个阶段，并把后者写成带确认点的独立步骤。
- **挂载矩阵（D69 收口）**：gateway **读写**挂 `/datalog`（写 spool 必需，与读侧路由无关）；control-plane **只读**挂 `/datalog`（点读）—— 冻结包 `reader.go` 全程只 `os.Open` / `pread`，不写任何文件（已核：`reader.go` 内 0 处 `Create`/`OpenFile`/`Mkdir`），因此只读挂载是安全的；sealer 在**宿主机**直接使用 `/datalog`（无容器、无挂载）。`network_mode: host` **只**共享网络命名空间，文件系统仍隔离 ⇒ bind mount 不可省。
- **挂载点必须由运维预先创建，绝不让 Docker 代建（D70）**：部署前 `findmnt -T /datalog --output TARGET,SOURCE,FSTYPE` 必须显示 `TARGET=/datalog` 且 `FSTYPE` 是真实文件系统（不是容器/宿主根 FS）。compose 一律用**长语法** `type: bind` + `source`/`target`，并且**绝不**依赖 `-v` 的隐式建目录。
- **旧定时器先隔离（D65）**：生产机上 `trace-archive.timer` 每天 **04:30** 调用 `/root/daily-trace-archive.sh`（`RETAIN_DAYS=5`）。启用新写路径**之前**必须先 `systemctl stop` + `disable` + `mask`，并**先实测**该脚本的删除谓词是否会命中 `/datalog/trace-archive-v4`（若命中，它会删掉我们的新树 ⇒ **停止上线**）。隔离与恢复各有脚本，**默认 dry-run**，必须显式 `--confirm` 才动手。
- **编码器参数终值（逐字，规格 §5.3.3 / §11.1）**：C 版 zstd CLI、`-19 --long=29 -T1`（级别 19、窗口 512 MiB、并发 1）；块大小阈值 `TRACE_BODY_BLOCK_MB=1536`（= 字节数 1536 × 1 MiB）；网关滚动间隔 1h。sealer 的 `--zstd-level 19 --zstd-window-mb 512` 必须与之逐字一致，并由静态检查钉住（正则断言 unit 里出现 `--zstd-level 19` 与 `--zstd-window-mb 512`）。
- **zstd 版本锁定（承重，规格 §11.1 / §15.2 P1/P2）**：生产 `/usr/bin/zstd` = **v1.4.8**。unit 必须显式给 `--zstd-binary /usr/bin/zstd --expected-zstd-version 1.4.8`；10.26× 的标定是 **v1.4.8 专属**，换版本必须重新标定。**本机开发环境是 v1.5.7（本机与生产不同）** ⇒ 本机只能做功能验证，压缩比与版本探测的结论必须在生产上取（§15.2 P1/P2）。
- **C5 预算取 2 GiB（D72）**：control-plane 的内存限额以**蓝绿生产编排**（`deploy/blue-green/docker-compose.{blue,green}.yml`，两者都是 `memory: 2g`）为准，与规格 §8.4 的 C5 一致。解码内存预算算术落在 `internal/traceverify`，读侧（计划 3）必须用它取并发上限。`docs/deployment.md` 的「gateway 1GB / cp 512MB」与根 `docker-compose.yml` 的 cp 512m **均不是**生产口径（CI 生产上线走 `deploy/blue-green/*`）。
- **生产实测事实（原样引用，不得拿旧文档的数字替代）**：生产 `/datalog` 当前 **407G 已用 / 153G 可用**（合计 560G，已用 **72.7%**）；**生产日期 2026-10-08**；旧定时器**每天 04:30** 运行；生产宿主 `/usr/bin/zstd` = **v1.4.8**；本仓库 HEAD 的实测值必须在执行当天用 `df -h /datalog` / `findmnt` / `zstd --version` 重新取，并把真实输出抄进报告（**报告里的数字必须是当天实测值**）。
- **证据制度（本仓库 CI **完全不跑** `go test`，`.gitlab-ci.yml` 里只有 `CGO_ENABLED=0 go build` ⇒ 变异证据是唯一验证手段）**：沿用 `tools/trace-mutate/README.md` 与 `tools/trace-mutate/trace_mutate.py`。要点：
  1. 集合**必须 git-tracked**。**工具一次只能变异一个文件**（`TARGET` 是单值，`--check` 也只核这一个文件）⇒ **一个任务按「被变异文件」拆成多个集合**，命名 `tools/trace-mutate/sets/p4-task-<N>-<文件短名>.py`（先例：`p2-task-8.py` / `p2-task-8-spooler.py` / `p2-task-8-dt-zone.py`）。同一任务的**所有**集合必须全部 exit=0，且每个集合的 `REQUIREMENTS` 只声明它自己能证明的那部分要求（不许把别的集合的条目抄进来充数）。
  2. 集合必须声明全部字段：`NAME` / `TARGET` / `PACKAGES` / `MANIFEST_FILES` / `MANIFEST_DERIVATION` / `SKIP_WHITELIST` / `MUTATIONS` / `CHECKS` / `REQUIREMENTS` / `EXPECTED_GREEN`（+ 可选 `RETIRED`）。
  3. 每家变异跑完必须核**三断言**：顶层 `=== RUN` 名字集合 == 清单（去空白后 diff 为空）；顶层 `PASS+FAIL+SKIP` 计数 == 清单条数；**全层级** SKIP == 0。**计数不等/为 0 即为无效**；`[no tests to run]` 绝不能算绿。
  4. **杀手必须是具体的顶层测试函数名**（不允许「整片用例转红」而不点名）。表驱动/共享断言环会**遮蔽**，因此每个承重属性原则上对应**一个顶层用例**；无法避免环时必须补隔离探针。
  5. 每条变异必须红成**断言红**（顶层用例 FAIL）；**编译红 / panic / 30s 超时红**必须改写变异字符串（保住被引用符号的引用，例如把 `if x != nil` 改成 `if x != nil && false`）后重跑，不得记成通过。
  6. 变异测不到的要求（结构不变量、产物文本）必须有 `CHECKS` 行，写明命令与真实输出。
  7. `REQUIREMENTS` 必须把「本任务对应的每条计划/规格要求」映射到变异或检查行；**有要求无条目 ⇒ 整轮失败**；零覆盖必须登记进 `EXPECTED_GREEN` 并写理由。
- **变异证据绑定提交（本仓库特有，必须逐字执行）**：`trace_mutate.py` 的证据模式**拒绝脏树**（`git status --porcelain` 非空即 `SystemExit`），且 `--check` 用 `git show HEAD:<target>`、pristine 树用 `git archive HEAD` 现取现解 ⇒ **权威表必须在「实现 + 测试 + 集合」提交之后运行**。因此每个任务的步骤次序是：写实现/测试/集合 → **提交** → 跑 `--check` → 跑权威表 → 把工具头部（`git rev-parse HEAD`、工具/集合/清单/被测文件 sha256）原样抄进报告。**任何后续「实现 / 测试 / 集合」内容的变更都会使此前所有绑定失效**（绑定的实质是**内容哈希**，表头的 `git rev-parse HEAD` 只记**产出时点**；**纯计划文档或纯证据文本的新提交不使绑定失效** —— 见计划 3 的 **E56** 与本计划 **E59**）⇒ ①每个任务必须在自己那次提交后重跑；②任务 12 在**最终 HEAD** 上重跑全部 `p4-task-*.py`；③报告里禁止复用上一轮的表。
- **用例名必须「互不为子串」，批次必须快（两条都是从工具实现里读出来的硬约束）**：工具把清单拼成**未锚定**的 `-run 'TestA|TestB|…'`（Go 的 `-run` 逐段做**子串**正则匹配），因此同一包内若 `TestFoo` 是 `TestFooBar` 的子串，只列 `TestFoo` 也会**连带**跑出 `TestFooBar` ⇒ 执行集合 != 清单 ⇒ **证据无效**（工具的自检 E 就是钉这个骗法的）。**动手前必须自证同包内任两个顶层用例名互不为子串**：
```bash
grep -h '^func Test' tests/<pkg>/*_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort | \
python3 -c "import sys;ns=[l.strip() for l in sys.stdin];bad=[(a,b) for a in ns for b in ns if a!=b and a in b];print(bad or 'OK: 无子串关系')"
```
（本计划已经按这条约束挑过名字；执行者若新增/改名必须重跑上面这条。）另外工具用 `-count=1 -timeout 30s` 跑整个批次，所以被变异的用例里**禁止**出现：真实 zstd 封块、真实填盘、`go build`、单个 >3s 的 sleep、>1MiB 的随机数据循环 —— 一律会红成「超时/挂死」，那是**无效证据**而不是通过。重活（A3 真实盘满、A6 解码内存、A7 进程级并发、1 GiB 级产物）只能走 `cmd/trace-verify` 与手工剧本，**不得**进任何 `p4-task-*.py` 的清单。
- **生产操作纪律**：一切破坏性/不可逆步骤写成**逐步**、含**显式确认点**与**回滚**；确认点用「停止点」措辞（`⛔ 停止点 N：必须人工确认后再继续`）。生产上**不得**填满 `/datalog` 或 `/`（见 D66）；一切「填满磁盘」的动作只允许发生在**专用回环文件系统**上，并由 `internal/traceverify.GuardScratchTarget` 在动手前证明「目标与 `/`、`/datalog` 不是同一个设备」。
- **关停编排（不得复制旧骨架）**：**禁止**写出 `case <-ctx.Done(): _ = s.Close()` 这种「Run 自己 Close」的形态。网关的关停次序已冻结为 `cancel()` → 等 `(*TraceWriter).Done()`（上限 `traceSpoolCloseWait = 6s`）→ `TraceSpooler.Close()`（`cmd/gateway/main.go:386-409`），本计划**不改**它，只把它写进运行手册。sealer 是独立进程，unit 的 `TimeoutStopSec=60s` + `KillMode=control-group` 让「封块中途被 SIGTERM」走 ctx 取消路径（`Seal` 会删 `.tmp`、保留 spool），不依赖任何自建关停逻辑。
- **部署产物即契约**：unit、compose、脚本、运行手册的**内容**由 Go 测试钉住（`tests/deploy/`，`package deploy_test`，只读仓库里的文件，不需要触达未导出符号 ⇒ 满足 `docs/guardrails.md` Q3）。变异目标就是**产物文件本身**（例如把 `Restart=on-failure` 改成 `Restart=no`，期望对应用例转红）。改产物就必须同步改测试，或让测试红。
- **测试分层与命名（Q3）**：不需要触达未导出符号的测试一律放 `tests/tracesealer/`、`tests/traceverify/`、`tests/traceverify/`、`tests/deploy/`（`package ..._test`，仅公开 API）。本计划新增的类型/函数**一律导出**。只有确需未导出符号时才放包内，并在函数注释里写明「为什么必须触达未导出符号」。
- **零迁移**：本计划**不新增任何 `atlas-migrations/`**。若执行中被迫新增（例如读侧需要索引），**先停下**，按 D52 / H7 做全库对账，再单独决策。
- **不碰的东西**：不改 `config/prod.yaml`（`inst` 必须来自**每个颜色各自的 compose env**，不能是全局 YAML —— 否则两个颜色会写进同一个 `inst=` 目录）；不改 `atlas.hcl`；不改 nginx 配置；不改 `docs/guardrails.md`（其 C3 与生产的 Redis DB 不一致只登记，见 D72）；不改 `internal/tracearchive/`。
- **测试环境变量注入（触发 `test-env-config` skill）**：要在**测试环境**启用时，执行者必须先读该 skill。判定结论：`TRACE_BODY_ROOT` / `TRACE_BODY_INST` / `TRACE_BODY_BLOCK_MB` / `TRACE_BODY_FLUSH_INTERVAL` / `TRACE_BODY_ROLL_INTERVAL` / `TRACE_BODY_DISK_WATERMARK_PERCENT` 都是**非密钥、非环境耦合**的静态值 ⇒ 写在**每个颜色各自的 compose**（skill §0.3 允许）；**任何密钥/连接串**仍只走 GitLab `TEST_*` 变量并在 `deploy-app-test` 的 ssh 块里 `export`（skill §3 第 5 步，最容易漏）。gateway 经 Viper `AutomaticEnv` + `SetEnvKeyReplacer(".","_")` 读 `TRACE_BODY_*`（计划 2 的 `TestTraceBodyConfigEnvOverrides` 已证），**无需**额外 `BindEnv`。

---

## 文件结构

| 文件 | 职责 | 规模估计 |
|---|---|---|
| `internal/tracesealer/options.go` | **新建**：`Options` + 校验/默认值 + 全部常量（含镜像冻结包的契约常量） | ~150 行 |
| `internal/tracesealer/classify.go` | **新建**：`FailureKind` + `Classify(err)`（纯函数，`errors.Is`） | ~70 行 |
| `internal/tracesealer/exit.go` | **新建**：退出码契约（纯函数 `ExitCode`，0/1/2） | ~35 行 |
| `internal/tracesealer/alert.go` | **新建**：`RoundResult` → 告警级别 / 稳定 `reason` / 稳定字段集 | ~60 行 |
| `internal/tracesealer/rootguard.go` | **新建**：根目录身份护栏（绝对路径 / 非 `/` / 存在 / mountinfo 身份基线） | ~150 行 |
| `internal/tracesealer/lock.go` | **新建**：`syscall.Flock` 单实例锁 | ~60 行 |
| `internal/tracesealer/footer.go` | **新建**：产物 footer 只读解析（`RowCount`，A8 的行数来源） | ~60 行 |
| `internal/tracesealer/runner.go` | **新建**：`Runner` / `Round` / `Loop` / `RoundResult` / 逐 part 与逐轮观测日志 | ~300 行 |
| `cmd/trace-sealer/main.go` | **新建**：flag 薄壳 + 装配 + `ExitCode` 转发（**不自己决定退出码**） | ~140 行 |
| `internal/traceverify/budget.go` | **新建**：C5 解码内存预算算术（`PerReadPeakBytes` / `MaxConcurrent`） | ~90 行 |
| `internal/traceverify/guard.go` | **新建**：破坏性操作目标护栏（拒绝 `/`、拒绝与 `/datalog` 同设备） | ~110 行 |
| `internal/traceverify/vmhwm.go` | **新建**：`VmHWM` 解析与峰值采样 | ~80 行 |
| `cmd/trace-verify/main.go` | **新建**：`scratch-prepare` / `scratch-cleanup` / `decode-bench`（**仅验收用；不进任何镜像、不进 `make build`**） | ~220 行 |
| `deploy/systemd/trace-sealer.service` | **新建**：宿主机 unit（常驻循环形态 + 资源限额 + 可写面收窄） | ~40 行 |
| `deploy/systemd/isolate-old-trace-archive.sh` | **新建**：隔离旧 `trace-archive.timer`（默认 dry-run，必须 `--confirm`，含删除谓词审查） | ~90 行 |
| `deploy/systemd/restore-old-trace-archive.sh` | **新建**：回滚隔离（默认 dry-run，必须 `--confirm`） | ~50 行 |
| `deploy/blue-green/README.md` | **新建**：蓝绿切换/回滚/降级/硬门剧本（随 `scp -r deploy/blue-green/*` 落到宿主） | ~120 行 |
| `deploy/verify/trace-a3/docker-compose.yml` | **新建**：A3/A7 的隔离实例（回环 FS + 测试环境 Redis/PG + 独立端口） | ~40 行 |
| `deploy/verify/trace-a3/run.sh` | **新建**：A3 的准备/填满/观察/恢复/清理全流程（含停止点与 `trap` 清理） | ~140 行 |
| `deploy/blue-green/docker-compose.{blue,green}.yml` | **修改**：gateway 读写挂 `/datalog` + per-color `TRACE_BODY_INST`；cp 只读挂 | 各 +10 行 |
| `docker-compose.yml` | **修改**：两个服务都挂 `/datalog`，但**不设**任何 `TRACE_BODY_*`（根编排没有颜色概念 ⇒ 不开功能） | +12 行 |
| `Makefile` | **修改**：新增 `build-sealer`（`GOOS=linux` 交叉编译）与 `build-trace-verify` | +8 行 |
| `.gitlab-ci.yml` | **修改**：上线路径编译并 `install` `trace-sealer` 到 `/usr/local/bin`（**不** enable unit） | +14 行 |
| `docs/deployment.md` | **修改**：新增「调用链正文（/datalog）运行手册」一节（任务 11 的全部条目） | +120 行 |
| `tests/tracesealer/*_test.go` | **B 层**（9 个文件）：分类 / 选项 / 退出码 / 轮次 / 告警 / 护栏 / 循环 / 锁与 A7 / footer / 常量契约 / 观测 | ~1100 行 |
| `tests/traceverify/*_test.go` | **B 层**（3 个文件）：目标护栏 / `VmHWM` 解析 / C5 预算算术 | ~400 行 |
| `tests/deploy/*_test.go` | **B 层**（8 个文件）：unit 文本 / 隔离与恢复脚本 / 构建（Makefile+CI）/ 三个生产 compose / `trace_a3_compose_test.go`（A3 隔离实例编排）/ 蓝绿剧本 / 运行手册 | ~1300 行 |
| `tools/trace-mutate/sets/p4-task-*.py` | **新建**（git-tracked）：**27** 个变异集合（一个集合只变异**一个**文件，见全局约束；**E66⑥ 订正**：原写 26 —— 任务 4 已按 **E65(d)** 拆成 `p4-task-4-lock.py` ＋ `p4-task-4-runner.py` 两个集合，「26」少算了它） | 每个 120–280 行 |

## 验收项归属（本计划承担哪几项、在哪做、证据是什么）

> A 表以**计划 2 任务 8 / 计划 3 采用的那张表**为准（计划 3 的 D56 已裁定；见本计划 D68）。**本计划不承担 A1/A2/A4/A5，也不承担 A9（冷热分层）。**

| 验收项（A 表口径） | 本计划的任务 | 在哪执行 | 证据形态 | 不通过时的处置 |
|---|---|---|---|---|
| **A3** 真实盘满 | 任务 8（+任务 2 的告警分级） | **测试环境**的专用回环 ext4（`/srv/trace-a3`，1 GiB），隔离网关实例 | `df -h` 前后、单条闩锁 ERROR、`body_missing=true` 计数、`/health`+`/ready` 仍 200、延迟直方图、恢复后新 spool | 若闩锁没出现或进程崩了 ⇒ 停下修，**不得**改到生产上复现 |
| **A6** 压缩比与解码内存 | 任务 9（+任务 10 的每 part ratio 日志） | 生产宿主（有 `/usr/bin/zstd` **v1.4.8**）取比；本机/测试机取解码内存 | `trace-sealer --once` 打出的 `ratio` 逐 part 值；`decode-bench` 的 `peak_vmhwm_bytes` vs `PerReadPeakBytes()` | 比值低于门槛 ⇒ 登记并**不得**换参数「凑」；解码内存超 2 GiB ⇒ 降并发或登记提高控制面限额 |
| **A7** 跨进程验证 | 任务 4（自动化：次序/锁/串行）+ 任务 8 run.sh 步骤 10（进程级：两个 `--once` 撞锁 + 边写边封） | 本机（自动化）/ A3 隔离实例（进程级） | 用例通过的清单 + 第二个 `--once` 的退出码 1 与 stderr | 出现「ref 无落点」或两个 sealer 同时封同一 part ⇒ **停止上线** |
| **A8** 行数分布 | 任务 10（footer 解析 + 契约钉住 + 决策门） | 测试环境/生产（只读日志） | 每个 part 的 `rows` 分布（min/p50/p95/max）与 `bytes_per_row` | 越界或落到 50%–100% ⇒ 按决策门三级处置（含改 `spooler.go` 并重跑 p2 集合） |
| **新增：撤掉挂载 ≤1 tick 闩锁** | 任务 11（先在测试环境/A3 实例做，再在**已摘流**的非活跃颜色上确认） | **只允许**测试环境，或**无真实流量**的生产窗口（非活跃且已摘流）；**该实验本身会持续丢正文**（见下），禁止在有正常流量的生产颜色上执行 | ≤1 个 tick 内的 ERROR + 不再出现新文件 + 活跃颜色无影响 + 重启后恢复 + 「流量为零」留证 + **闩锁期间累计丢失行数（期望 0）** | 未在 ≤1 tick 内闩锁 ⇒ 停下来查 `checkRoot` 的调用点（计划 2 的 `FlushAndRoll`）；丢失行数非 0 ⇒ 停手并按事故复盘 |
| A1 / A2 / A4 / A5 | **A1 的读回半部（n = 1e6）物理测量 = 任务 9**（口径 = 计划 3 的交接 **H19**；环境 = 生产宿主 + 真实 PG + 真实 `zstd` **1.4.8**）；**A2 / A5** 的对外映射归计划 3；**A4** 由计划 1 覆盖（计划 3 的 E52③ 欠账在本轮销账，见 **E71**） | 任务 9 / 生产宿主 | 抽样 1,000 行逐字节比对表 + `zstd --version` | 任一格不成立 ⇒ 未通过（不得降低抽样数或改写用例） |
| A9 冷热分层 | ❌ 不在本计划（D68） | — | — | — |

> ⚠️ **「撤掉挂载 ≤1 tick 闩锁」的前置条件（不可省，否则该验收恒空）**：本项只有在**写侧已启用**时才有意义 —— 即 `TRACE_BODY_ROOT` 非空**且**该 root 目录已存在（`internal/gateway/observability/spooler.go:144` 的 `rootExists` 在**构造期**就要求目录在场）。功能关闭时 `cmd/gateway/main.go:275` 的 `sink` 为 **nil**、`TraceSpooler` **根本不会构造** ⇒ 它的根身份护栏（`spooler.go` 的 `checkRoot`）从不运行 ⇒ 期望的 `root_unavailable` / `root-identity-mismatch` ERROR **永不出现**，断言恒真（空过）。因此本项**不得**排在写侧启用之前，也不是计划 4 在「功能关闭」阶段就能交付的验收；它的编排位置是：**读侧就绪（D73 硬门）→ 开启写侧 → 立即执行本门禁**（见任务 11 步骤 4 与文末「生产上的执行次序」）。另有两个不可省的约束：**① 该实验本身在持续丢正文**（闩锁期间正文既不进 `/datalog` 也不回退 jsonb，只落 `body_missing=true`）⇒ 只允许在测试环境或**无真实流量**的生产窗口执行；**② 证据里必须含「闩锁期间累计丢失行数」（期望 0）**。

**每个文件单一职责。** `runner.go` 只做「一轮怎么走」；`classify.go` 是纯函数（最易测）；`rootguard.go` 只回答「这个 root 还能不能用」；`lock.go` 只做互斥；`footer.go` 只读 32 字节。**不要把四者合并**：它们的失败面完全不同（分类 = 纯逻辑；护栏 = 与挂载状态有关；锁 = 与并发有关；footer = 与冻结包的磁盘格式有关）。`cmd/trace-sealer` 是薄壳（flag + 装配 + 退出码），一切可测逻辑都在 `internal/tracesealer`。

---

### Task 1：`internal/tracesealer` 单轮封块与错误分类

**文件：**
- 新建：`internal/tracesealer/options.go`、`internal/tracesealer/classify.go`、`internal/tracesealer/exit.go`、`internal/tracesealer/runner.go`（本轮只实现 `Round`，不含 `Loop`/锁/护栏）
- 新建：`cmd/trace-sealer/main.go`（薄壳：flag → `Round` → `ExitCode`）
- 测试：`tests/tracesealer/classify_test.go`、`tests/tracesealer/round_test.go`、`tests/tracesealer/exit_test.go`、`tests/tracesealer/options_test.go`（B 层）

**接口：**
- 依赖输入：冻结包公开 API `NewSealer(SealerOptions) (*Sealer, error)`、`(*Sealer).ListPendingSeals() ([]SealTask, error)`、`(*Sealer).Seal(ctx, SealTask) error`、`SealerOptions{Root, ZstdBinary, ZstdLevel, ZstdWindowBytes, MinAge, ExpectedZstdVersion}`、`SealTask{DT, Inst, Part, SpoolPath, Rows, UncompressedBytes}`。
- 对外产出（后续任务直接使用，**名字与签名不得漂移**）：
  ```go
  // internal/tracesealer/options.go
  const (
      DefaultZstdLevel     = 19                 // 规格 §5.3.3
      DefaultZstdWindowMB  = 512                // 规格 §5.3.3（→ --long=29）
      DefaultExpectedZstd  = "1.4.8"            // 冻结包 defaultZstdVersion；生产实测版本
      DefaultMinAge        = 3 * time.Hour      // = 冻结包 defaultSealMinAge
      DefaultPollInterval  = 5 * time.Minute    // 规格 §11.1 的 timer 示例即 5min
      GatewayMaxRollInterval = time.Hour        // R8：网关滚动间隔上界（1h）
      MaxTrailerEntries    = 8_388_608          // = 冻结包 maxTrailerEntries（镜像，见契约测试）
      MinAgeFloor          = time.Hour          // 强制下界：min-age 必须 > 1h
  )

  type Options struct {
      Root                string
      ZstdBinary          string        // 空 ⇒ "zstd"（PATH）；生产 unit 必须显式给 /usr/bin/zstd
      ZstdLevel           int           // 0 ⇒ DefaultZstdLevel
      ZstdWindowMB        int           // 0 ⇒ DefaultZstdWindowMB
      MinAge              time.Duration // 0 ⇒ DefaultMinAge；必须 > MinAgeFloor
      ExpectedZstdVersion string        // 空 ⇒ DefaultExpectedZstd
      PollInterval        time.Duration // 0 ⇒ DefaultPollInterval（Task 3 用）
      SealTimeout         time.Duration // 0 ⇒ 30m（单个 part 的封块上限）
      LockFile            string        // 空 ⇒ filepath.Join(Root, ".sealer.lock")（Task 4 用）
      Now                 func() time.Time                       // 可注入
      Logf                func(format string, args ...any)        // 可注入，默认 log.Printf
      NewSealer           func(tracearchive.SealerOptions) (Sealer, error) // 可注入（测试替身）
  }

  type Sealer interface {
      ListPendingSeals() ([]tracearchive.SealTask, error)
      Seal(ctx context.Context, task tracearchive.SealTask) error
  }

  func NewRunner(opts Options) (*Runner, error)
  func (r *Runner) Round(ctx context.Context) (RoundResult, error)

  // internal/tracesealer/classify.go
  type FailureKind string
  const (
      KindPartExists   FailureKind = "part_exists"        // 幂等无害
      KindRowMismatch  FailureKind = "row_count_mismatch" // 重试
      KindSealFailed   FailureKind = "seal_failed"        // 重试
      KindTooManyRows  FailureKind = "too_many_rows"      // 永久，需人工介入
      KindUnknown      FailureKind = "unknown"            // 未分类 ⇒ 按重试处理但响亮告警
  )
  func Classify(err error) FailureKind
  func (k FailureKind) Retryable() bool
  func (k FailureKind) Permanent() bool

  // internal/tracesealer/exit.go —— 退出码是**对外契约**，因此是纯函数（可测、可变异），
  // 不是埋在 main 里的 switch（main 里埋 switch 就只能靠 go build 端到端测，而
  // 变异批次有 30s 上限，编译一次二进制就吃掉了）。
  const (
      ExitOK       = 0 // 正常结束
      ExitFatal    = 1 // 启动期致命错误：配置非法 / 根目录不可用 / 锁被占用
      ExitFailed   = 2 // --once 跑完但本轮有可重试或永久失败（供 oneshot/timer 形态判断）
  )
  func ExitCode(res RoundResult, err error) int
  ```
  ```go
  // internal/tracesealer/runner.go
  type RoundResult struct {
      Eligible, Sealed int
      PartExists, RowMismatch, SealFailed, TooManyRows, UnknownFailed int
      MaxRows          int           // 本轮成功封出的 part 的最大行数（A8 的核心观测量）
      MinBytesPerRow   int64         // 本轮成功封出的 part 的最小「未压缩字节/行」（A8 的预警量）
      MinRatio         float64       // 本轮最小压缩比（A6 的连续观测量；0 = 无样本）
      Duration         time.Duration
  }
  func (r RoundResult) Failed() int
  ```

- [ ] **步骤 1：写失败的测试（分类，纯函数）**

新建 `tests/tracesealer/classify_test.go`。**每个承重属性一个顶层用例**（避免表驱动遮蔽）：

```go
package tracesealer_test

import (
	"errors"
	"fmt"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestClassifyPartExistsIsIdempotent PartExists 是幂等无害的：
// 下轮扫描仍会看到同一个 spool，运维动作是「什么都不做」，因此**不得告警**。
func TestClassifyPartExistsIsIdempotent(t *testing.T) {
	err := fmt.Errorf("%w: /datalog/x/part-000001.jsonl.zst", ta.ErrPartExists)
	require.Equal(t, tracesealer.KindPartExists, tracesealer.Classify(err))
	require.False(t, tracesealer.Classify(err).Retryable(), "幂等无害 ⇒ 不算「重试」")
	require.False(t, tracesealer.Classify(err).Permanent())
}

// TestClassifyRowMismatchIsRetryable 行数不符 ⇒ 保留 spool、下轮重试、必须告警。
func TestClassifyRowMismatchIsRetryable(t *testing.T) {
	err := fmt.Errorf("%w: 扫描=10 调用方=9", ta.ErrRowCountMismatch)
	require.Equal(t, tracesealer.KindRowMismatch, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Retryable())
}

// TestClassifySealFailedIsRetryable 子进程非零退出 / 版本不符 / 残行都归这一类。
func TestClassifySealFailedIsRetryable(t *testing.T) {
	err := fmt.Errorf("%w: zstd 版本不符：期望 1.4.8，实际 1.5.7", ta.ErrSealFailed)
	require.Equal(t, tracesealer.KindSealFailed, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Retryable())
}

// TestClassifyTooManyRowsIsPermanent 超限块**重试永远不会成功**（同一个 spool 重扫多少次
// 行数都一样）⇒ 必须与「可重试」分开，且标成需要人工介入。
func TestClassifyTooManyRowsIsPermanent(t *testing.T) {
	err := fmt.Errorf("%w: spool 行数=8388609 超过上界 8388608", ta.ErrTooManyRows)
	require.Equal(t, tracesealer.KindTooManyRows, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Permanent())
	require.False(t, tracesealer.Classify(err).Retryable())
}

// TestClassifyUnknownIsLoudAndRetryable 未分类错误不得被当成「幂等无害」吞掉
// （那等于静默丢数据），也不得被当成永久（那会让一次瞬时故障永久停摆）。
func TestClassifyUnknownIsLoudAndRetryable(t *testing.T) {
	require.Equal(t, tracesealer.KindUnknown, tracesealer.Classify(errors.New("磁盘抖动")))
	require.True(t, tracesealer.Classify(errors.New("磁盘抖动")).Retryable())
}

// TestClassifyUsesErrorsIsNotEquality 冻结包用 %w 包装三类哨兵；用 == 比较会全部
// 落进 unknown（这条用例是「必须用 errors.Is」的杀手）。
func TestClassifyUsesErrorsIsNotEquality(t *testing.T) {
	wrapped := fmt.Errorf("外层上下文: %w", fmt.Errorf("中间层: %w", ta.ErrSealFailed))
	require.Equal(t, tracesealer.KindSealFailed, tracesealer.Classify(wrapped))
}
```

- [ ] **步骤 2：运行并确认它失败**

运行：`go test ./tests/tracesealer/ -run 'TestClassify' -v`
预期：**FAIL（编译失败）**——`tracesealer` 包尚不存在。这一步的「失败」是编译红，因此**不算**证据；本任务的证据在步骤 8（变异集合）之后。

- [ ] **步骤 3：实现分类与选项（最小实现）**

`internal/tracesealer/classify.go`：

```go
package tracesealer

import (
	"errors"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// FailureKind 是封块失败的分类。
//
// 【为什么必须有分类，而不是「失败就重试」】冻结包 errors.go 已经把三类失败
// 的语义写死：ErrPartExists 幂等无害、ErrRowCountMismatch/ErrSealFailed 保留 spool
// 并可重试、ErrTooManyRows 重试**永远不会**成功（需人工介入）。若 sealer 把三者
// 揉成一个「失败 ⇒ 告警 ⇒ 下轮重试」，运维看到的将是一堆永远不消失的告警，
// 而真正需要人拆块的那一块被淹没 —— 这是「把可诊断故障变成噪音」的典型形态。
type FailureKind string

const (
	KindPartExists  FailureKind = "part_exists"
	KindRowMismatch FailureKind = "row_count_mismatch"
	KindSealFailed  FailureKind = "seal_failed"
	KindTooManyRows FailureKind = "too_many_rows"
	KindUnknown     FailureKind = "unknown"
)

// Classify 用 errors.Is 判定分类（**不得用 ==**：冻结包一律用 %w 包装，
// 见 errors.go 的注释「三者都必须用 %w 包装，否则调用方的 errors.Is 分类失效」）。
//
// 【判定次序】先判「永久」再判「幂等」再判「可重试」：三类哨兵互不重叠，
// 次序只在 KindUnknown 的兜底上与实现耦合 —— 兜底必须是最后一个 else，
// 绝不能让某个未知错误被某个具体分支抢先吞掉。
func Classify(err error) FailureKind {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ta.ErrTooManyRows):
		return KindTooManyRows
	case errors.Is(err, ta.ErrPartExists):
		return KindPartExists
	case errors.Is(err, ta.ErrRowCountMismatch):
		return KindRowMismatch
	case errors.Is(err, ta.ErrSealFailed):
		return KindSealFailed
	default:
		return KindUnknown
	}
}

// Retryable 表示「保留 spool、下轮重试」。
// KindPartExists **不算**重试：它是「已经做好了，什么都不用做」。
func (k FailureKind) Retryable() bool {
	return k == KindRowMismatch || k == KindSealFailed || k == KindUnknown
}

// Permanent 表示「重试永远不会成功，需人工介入」。
func (k FailureKind) Permanent() bool { return k == KindTooManyRows }
```

`internal/tracesealer/options.go`（本轮只给 `Options`、默认值与校验；`LockFile`/`PollInterval`/`SealTimeout` 先给默认值，供 Task 3/4 使用）：

```go
package tracesealer

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

const (
	DefaultZstdLevel    = 19
	DefaultZstdWindowMB = 512
	DefaultExpectedZstd = "1.4.8"
	DefaultMinAge       = 3 * time.Hour
	DefaultPollInterval = 5 * time.Minute
	DefaultSealTimeout  = 30 * time.Minute

	// GatewayMaxRollInterval 是网关滚动间隔的上界（规格 §15.1 R8）。
	// 冻结包的 minAge 是 sealer 侧**唯一**的保护：网关与 sealer 之间没有信号通道，
	// sealer 封块后会删除 spool，而网关若仍持有同一 part 的 fd，追加会写进已被
	// unlink 的 inode —— 落盘成功但永久不可达（D7）。因此 min-age 必须**严格大于**
	// 这个上界（实际还留了 3 倍余量：3h 对 1h）。
	GatewayMaxRollInterval = time.Hour

	// MaxTrailerEntries 是冻结包 maxTrailerEntries 的镜像值。
	// 【必须由测试钉住】它是未导出的，本包只能复制；一旦冻结包改了常量而这里没跟，
	// A8 的「不超上界」判断就会静默失真 ⇒ tests/tracesealer/constants_contract_test.go
	// 现读 reader.go 的源码比对。镜像值：maxTrailerBytes(64<<20) / 8 = 8,388,608。
	MaxTrailerEntries = 8_388_608
)

type Options struct {
	Root                string
	ZstdBinary          string
	ZstdLevel           int
	ZstdWindowMB        int
	MinAge              time.Duration
	ExpectedZstdVersion string
	PollInterval        time.Duration
	SealTimeout         time.Duration
	LockFile            string
	Now                 func() time.Time
	Logf                func(format string, args ...any)
	NewSealer           func(ta.SealerOptions) (Sealer, error)
}

// normalize 填默认值并校验。校验一律**响亮失败**（返回 error）：配置错误静默接受
// 只会把现场推到几小时后的封块失败上，而那时运维已经不知道是谁配错的。
func (o *Options) normalize() error {
	if o.ZstdBinary == "" {
		o.ZstdBinary = "zstd"
	}
	if o.ZstdLevel <= 0 {
		o.ZstdLevel = DefaultZstdLevel
	}
	if o.ZstdWindowMB <= 0 {
		o.ZstdWindowMB = DefaultZstdWindowMB
	}
	if o.MinAge <= 0 {
		o.MinAge = DefaultMinAge
	}
	if o.ExpectedZstdVersion == "" {
		o.ExpectedZstdVersion = DefaultExpectedZstd
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.SealTimeout <= 0 {
		o.SealTimeout = DefaultSealTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	if o.LockFile == "" {
		o.LockFile = filepath.Join(o.Root, ".sealer.lock")
	}
	if o.MinAge <= MinAgeFloor {
		return fmt.Errorf(
			"tracesealer: min-age=%s 必须严格大于网关滚动间隔上界 %s（规格 §15.1 R8）："+
				"sealer 是独立进程、收不到任何信号，它与网关之间唯一的保护就是这个时间差；"+
				"min-age 不大于它就会去封一个网关仍在追加的块（字节写进已 unlink 的 inode ⇒ 永久丢失）",
			o.MinAge, GatewayMaxRollInterval)
	}
	// 窗口必须是 2 的幂（冻结包 zstdLongArg 会拒绝）；先在这里给出更可读的错误，
	// 免得运维在几小时后看到「zstd 窗口必须是 2 的幂」而想不起是哪个 flag。
	if o.ZstdWindowMB&(o.ZstdWindowMB-1) != 0 {
		return fmt.Errorf("tracesealer: zstd-window-mb=%d 必须是 2 的幂（换算为 CLI 的 --long=<log2>）", o.ZstdWindowMB)
	}
	return nil
}
```

`internal/tracesealer/runner.go`（本轮只实现 `Round`；`NewRunner` 先不含护栏与锁，Task 3/4 补）：

```go
package tracesealer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

type Sealer interface {
	ListPendingSeals() ([]ta.SealTask, error)
	Seal(ctx context.Context, task ta.SealTask) error
}

type RoundResult struct {
	Eligible, Sealed                                              int
	PartExists, RowMismatch, SealFailed, TooManyRows, UnknownFailed int
	MaxRows        int
	MinBytesPerRow int64
	MinRatio       float64
	Duration       time.Duration
}

func (r RoundResult) Failed() int {
	return r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed
}

type Runner struct {
	opts   Options
	sealer Sealer
}

func NewRunner(opts Options) (*Runner, error) {
	if opts.Root == "" {
		return nil, errors.New("tracesealer: Root 不能为空")
	}
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	if opts.NewSealer == nil {
		opts.NewSealer = func(so ta.SealerOptions) (Sealer, error) { return ta.NewSealer(so) }
	}
	s, err := opts.NewSealer(ta.SealerOptions{
		Root:                opts.Root,
		ZstdBinary:          opts.ZstdBinary,
		ZstdLevel:           opts.ZstdLevel,
		ZstdWindowBytes:     opts.ZstdWindowMB << 20,
		MinAge:              opts.MinAge,
		ExpectedZstdVersion: opts.ExpectedZstdVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("构造封块器失败: %w", err)
	}
	return &Runner{opts: opts, sealer: s}, nil
}

// Round 跑一轮：发现 → 逐个封块 → 分类计数 → 汇总。
//
// 【为什么一个 part 失败不能中断整轮】冻结包的发现路径是纯目录扫描、天然幂等；
// 一个坏块（例如超限块）应当只影响它自己。若在第一个失败处 return，
// 后面所有健康块都会被无限期拖延 —— 而它们本来可以正常封块读回。
func (r *Runner) Round(ctx context.Context) (RoundResult, error) {
	start := r.opts.Now()
	var res RoundResult

	tasks, err := r.sealer.ListPendingSeals()
	if err != nil {
		res.Duration = r.opts.Now().Sub(start)
		return res, fmt.Errorf("列举待封块失败: %w", err)
	}
	res.Eligible = len(tasks)

	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			res.Duration = r.opts.Now().Sub(start)
			return res, err
		}
		sealCtx, cancel := context.WithTimeout(ctx, r.opts.SealTimeout)
		err := r.sealer.Seal(sealCtx, task)
		cancel()

		kind := Classify(err)
		switch kind {
		case "":
			res.Sealed++
			r.observeSealedPart(&res, task)
		case KindPartExists:
			res.PartExists++
			// 幂等无害：不告警。下轮扫描仍会看到同一 spool（它不会被删），
			// 这是**预期**状态（产物已存在、spool 保留），不需要任何动作。
		case KindRowMismatch:
			res.RowMismatch++
			r.opts.Logf("[trace-sealer] ERROR 封块失败 kind=%s ref=%s/%s/part-%06d spool=%s err=%v",
				kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		case KindSealFailed:
			res.SealFailed++
			r.opts.Logf("[trace-sealer] ERROR 封块失败 kind=%s ref=%s/%s/part-%06d spool=%s err=%v",
				kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		case KindTooManyRows:
			res.TooManyRows++
			// 永久：重试永远不会成功。文案必须点明「需人工介入」，否则运维会
			// 一直等下一轮自己好 —— 它不会好。
			r.opts.Logf("[trace-sealer] ERROR 封块被永久拒绝（需人工介入，重试不会成功）kind=%s "+
				"ref=%s/%s/part-%06d spool=%s err=%v", kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		default:
			res.UnknownFailed++
			r.opts.Logf("[trace-sealer] ERROR 封块失败（未分类，按可重试处理）ref=%s/%s/part-%06d spool=%s err=%v",
				task.DT, task.Inst, task.Part, task.SpoolPath, err)
		}
	}
	res.Duration = r.opts.Now().Sub(start)
	r.opts.Logf("[trace-sealer] INFO 轮次结束 eligible=%d sealed=%d part_exists=%d row_mismatch=%d "+
		"seal_failed=%d too_many_rows=%d unknown=%d max_rows=%d min_bytes_per_row=%d min_ratio=%.2f duration=%s",
		res.Eligible, res.Sealed, res.PartExists, res.RowMismatch, res.SealFailed,
		res.TooManyRows, res.UnknownFailed, res.MaxRows, res.MinBytesPerRow, res.MinRatio, res.Duration)
	return res, nil
}
```

`observeSealedPart`（本任务先留最小实现：只更新 `MaxRows` / `MinBytesPerRow` / `MinRatio`；行数与比值来自 Task 10 的 footer 解析，本轮先用 `UncompressedBytes` 与 0 占位——**不得**留下 TODO，见步骤 4 的说明）：

```go
// observeSealedPart 采集本轮成功封出的 part 的观测量（A8 行数、A6 压缩比）。
//
// 【为什么占位而不是 TODO】Task 10 会用 footer 里的真实 row_count 替换 rows；在
// 那之前 rows 未知 ⇒ 本函数只用 UncompressedBytes 更新 MinBytesPerRow 的**分母已知**
// 那一半：它必须显式表达「未知」，而不是拿 0 当分母算出 Inf/NaN 混进告警。
// 这就是本任务交付的形态：A8 的观测量在 Task 10 才成立，本任务只承重「计数与分类」。
func (r *Runner) observeSealedPart(res *RoundResult, task ta.SealTask) {
	if task.UncompressedBytes <= 0 {
		return
	}
	rows, err := RowCount(filepath.Join(NewPathsFor(r.opts.Root).PartDir(task.DT, task.Inst),
		partFileName(task.Part)))
	_ = err
	_ = rows
	return
}
```

> ⛔ **执行者注意**：上面这段「占位」是**故意**保留的接口说明，它**不能**留在代码里（禁止 TODO/占位）。本任务步骤 4 的实际实现是：`Round` 只累加 `Sealed`，`MaxRows/MinBytesPerRow/MinRatio` 保持零值，并**在 `RoundResult` 的字段注释里写明「零值 = 本轮无样本或该观测量在 Task 10 才落地」**。Task 10 再实现 `RowCount` 与观测。**这一段（含本注记）在 Task 10 落地后必须删除**，不得出现在最终代码里。

- [ ] **步骤 4：按步骤 3 的注记实现「零值 + 字段注释」形态，然后跑测试**

```go
// RoundResult
//   MaxRows / MinBytesPerRow / MinRatio 在 Task 10 之前**恒为零值**：
//   行数只有从产物 footer 才拿得到（Task 10 的 RowCount），比值需要行数作分母。
//   零值 = 「本轮无样本」，而不是「0 行」/「0 字节每行」——消费方必须先判 Sealed > 0。
```

运行：`go test ./tests/tracesealer/ -run 'TestClassify' -v`
预期：**PASS**（6 个顶层用例）。

- [ ] **步骤 5：写失败的测试（一轮的编排）**

新建 `tests/tracesealer/round_test.go`。`fakeSealer` 记录调用次序并按 `(dt,inst,part)` 返回预置错误：

```go
package tracesealer_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// fakeSealer 让「同一批任务里既有成功又有四类失败」这种组合可确定复现 ——
// 真实冻结包无法为同一 Ref 报不同错误（它的失败是环境的函数）。
type fakeSealer struct {
	tasks   []ta.SealTask
	byPart  map[int]error
	calls   []int
	listErr error
}

func (f *fakeSealer) ListPendingSeals() ([]ta.SealTask, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tasks, nil
}

func (f *fakeSealer) Seal(_ context.Context, task ta.SealTask) error {
	f.calls = append(f.calls, task.Part)
	return f.byPart[task.Part]
}

func mkTask(part int) ta.SealTask {
	return ta.SealTask{
		DT: "2026-10-08", Inst: "blue", Part: part,
		SpoolPath: fmt.Sprintf("/datalog/trace-archive-v4/spool/dt=2026-10-08/inst=blue/part-%06d.jsonl", part),
		UncompressedBytes: 1 << 20,
	}
}

func newTestRunner(t *testing.T, f *fakeSealer) *tracesealer.Runner {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "spool"), 0o755))
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: root,
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	return r
}

// TestRoundSealsEveryEligiblePart 一轮必须走完所有可封块（一个失败不能中断整轮）。
func TestRoundSealsEveryEligiblePart(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0), mkTask(1), mkTask(2), mkTask(3)},
		byPart: map[int]error{1: fmt.Errorf("%w: 子进程退出 37", ta.ErrSealFailed)},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 2, 3}, f.calls, "part 1 失败后必须继续封 2 与 3")
	require.Equal(t, 4, res.Eligible)
	require.Equal(t, 3, res.Sealed)
	require.Equal(t, 1, res.SealFailed)
}

// TestRoundTreatsPartExistsAsIdempotentNoop 产物已存在 ⇒ 不计入失败、不告警。
func TestRoundTreatsPartExistsAsIdempotentNoop(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0), mkTask(1)},
		byPart: map[int]error{0: fmt.Errorf("%w: 已存在", ta.ErrPartExists)},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.PartExists)
	require.Equal(t, 0, res.Failed(), "PartExists 不是失败")
	require.Equal(t, 1, res.Sealed)
}

// TestRoundSeparatesPermanentFromRetryable 超限块必须与可重试失败分开计数
// （否则运维无法从汇总里区分「等一等就好」与「必须有人动手」）。
func TestRoundSeparatesPermanentFromRetryable(t *testing.T) {
	f := &fakeSealer{
		tasks: []ta.SealTask{mkTask(0), mkTask(1)},
		byPart: map[int]error{
			0: fmt.Errorf("%w: 行数=8388609", ta.ErrTooManyRows),
			1: fmt.Errorf("%w: 行数不符", ta.ErrRowCountMismatch),
		},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.TooManyRows)
	require.Equal(t, 1, res.RowMismatch)
	require.Equal(t, 2, res.Failed())
}

// TestRoundCountsUnknownFailuresAsFailures 未分类失败也必须计入 Failed()：
// 它是「我们还不知道这是什么」，不是「没问题」；漏计会让退出码与告警一起沉默。
func TestRoundCountsUnknownFailuresAsFailures(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0)},
		byPart: map[int]error{0: errors.New("EIO：底层 IO 抖动")},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.UnknownFailed)
	require.Equal(t, 1, res.Failed())
}

// stubSealer 是 options_test.go / exit_test.go 共用的替身构造器
// （它们只关心启动期校验与退出码，不需要真实的发现结果）。
func stubSealer(ta.SealerOptions) (tracesealer.Sealer, error) { return &fakeSealer{}, nil }

// TestRoundCancellationStopsBeforeNextSeal ctx 取消后**不得**再开始新的一块：
// 关机时「少封一块」是无害的（下轮会重新发现），「封到一半被杀」才会留下 .tmp。
func TestRoundCancellationStopsBeforeNextSeal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeSealer{tasks: []ta.SealTask{mkTask(0), mkTask(1), mkTask(2)}, byPart: map[int]error{}}
	f.onSeal = func(int) { cancel() }
	r := newTestRunner(t, f)
	_, err := r.Round(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []int{0}, f.calls)
}

// TestRoundDoesNotSwallowListError 发现阶段失败必须上抛（它是「根目录读不了」这类
// 环境级问题，与「某一块封不了」不同 —— 前者会让本轮所有块都被跳过）。
func TestRoundDoesNotSwallowListError(t *testing.T) {
	f := &fakeSealer{listErr: errors.New("读 spool 根目录失败: permission denied")}
	r := newTestRunner(t, f)
	_, err := r.Round(context.Background())
	require.ErrorContains(t, err, "列举待封块失败")
}
```

（`fakeSealer` 需要加一个 `onSeal func(int)` 字段，在 `Seal` 里回调；上面 `TestRoundCancellationStopsBeforeNextSeal` 用到它。）

- [ ] **步骤 6：运行并确认它失败**

运行：`go test ./tests/tracesealer/ -run 'TestRound' -v`
预期：**FAIL**（`res.Failed()` 未定义 / `onSeal` 未定义 ⇒ 编译红）。补齐最小实现（`Failed()` 已在步骤 3 给出、`onSeal` 加进测试的 fake）后重跑，预期 **PASS（5 个顶层用例）**。

- [ ] **步骤 7：`cmd/trace-sealer` 薄壳 + 退出码契约**

`cmd/trace-sealer/main.go`：

```go
// trace-sealer 是调用链正文的封块器（方案 C：网关内不压缩，压缩由本进程调
// C 版 zstd CLI 完成）。它**必须在宿主机上运行**（不进容器），用宿主机的
// /usr/bin/zstd；详见 deploy/systemd/trace-sealer.service 与
// docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md。
//
// 退出码契约（unit 与 CI 都依赖它）：见 internal/tracesealer/exit.go 的 ExitOK /
// ExitFatal / ExitFailed。**本文件不得自己决定退出码**，一律 os.Exit(tracesealer.ExitCode(...))。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/llm-gateway/internal/tracesealer"
)

func main() {
	var (
		root        = flag.String("root", "", "归档根目录（必填，必须绝对路径且不是 /）")
		zstdBinary  = flag.String("zstd-binary", "", "zstd 可执行文件（生产必须显式给 /usr/bin/zstd）")
		zstdLevel   = flag.Int("zstd-level", 0, "zstd 级别（0 ⇒ 19，规格 §5.3.3）")
		zstdWinMB   = flag.Int("zstd-window-mb", 0, "zstd 窗口 MiB（0 ⇒ 512 ⇒ --long=29）")
		minAge      = flag.Duration("min-age", 0, "「已停止追加」判定窗口（0 ⇒ 3h；必须 > 1h）")
		expectZstd  = flag.String("expected-zstd-version", "", "期望的 zstd 版本（0 ⇒ 1.4.8）")
		pollEvery   = flag.Duration("poll-interval", 0, "常驻循环的轮询周期（0 ⇒ 5m，未提供的 0 值即默认）")
		sealTimeout = flag.Duration("seal-timeout", 0, "单个 part 的封块上限（0 ⇒ 30m）")
		lockFile    = flag.String("lock-file", "", "单实例锁文件（默认 <root>/.sealer.lock）")
		once        = flag.Bool("once", false, "只跑一轮然后退出（离线标定与排障用）")
		version     = flag.Bool("version", false, "打印版本并退出")
	)
	flag.Parse()
	if *version {
		fmt.Println("trace-sealer (plan 4/4)")
		return
	}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: *root, ZstdBinary: *zstdBinary, ZstdLevel: *zstdLevel,
		ZstdWindowMB: *zstdWinMB, MinAge: *minAge, ExpectedZstdVersion: *expectZstd,
		PollInterval: *pollEvery, SealTimeout: *sealTimeout, LockFile: *lockFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace-sealer: %v\n", err)
		os.Exit(tracesealer.ExitFatal)
	}
	// Task 4 会在这里加 defer r.Close()（释放单实例锁）；本任务还没有 Close。

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if !*once {
		fmt.Fprintln(os.Stderr, "trace-sealer: 常驻循环在计划 4/4 任务 3 落地；当前只支持 --once")
		os.Exit(tracesealer.ExitFatal)
	}
	res, err := r.Round(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace-sealer: %v\n", err)
		os.Exit(tracesealer.ExitCode(res, err))
	}
	os.Exit(tracesealer.ExitCode(res, nil))
}
```

> **本任务只交付 `--once` 路径**：`Loop` 在 Task 3 实现。本任务里 `main` 对「非 `--once`」显式报错退出（上面代码里的 `if !*once`），Task 3 把它替换成 `r.Loop(ctx)` —— **Task 3 的步骤里会明确要求删掉这段临时分支。**

`internal/tracesealer/exit.go`（退出码是**对外契约**，所以做成纯函数；`main` 只负责转发）：

```go
package tracesealer

// 退出码契约（systemd unit、CI、以及将来若改用 oneshot/timer 形态的运维脚本都依赖它）：
//
//	ExitOK     一轮/常驻循环正常结束
//	ExitFatal  启动期致命错误：配置非法、根目录不可用、锁被别的 sealer 持有
//	ExitFailed --once 跑完但本轮存在「可重试」或「永久」失败
//
// 【为什么是纯函数而不是 main 里的 switch】main 里的 switch 只能靠「编译出二进制
// 再跑」端到端验证，而变异批次有 30s 上限（工具用 -test.timeout 30s），编译一次
// 二进制就吃掉了大半预算。搬进纯函数后，三个顶层用例就能钉死它，而且可被变异杀死。
const (
	ExitOK     = 0
	ExitFatal  = 1
	ExitFailed = 2
)

// ExitCode 把「一轮的结果 + 错误」映射成退出码。
//
// 【为什么「有失败」必须退 2 而不是 0】常驻 unit（Type=simple）不看退出码（它会
// Restart），但运维手工跑 `--once`、以及将来若改用 timer 形态时，退出码是脚本**唯一**
// 能看见的信号：把「这一轮有东西没封成」报成 0，等于让「定时跑一次、失败就告警」的
// 脚本永远沉默 —— 而 sealer 的失败恰恰是那种「不喊就不会有人知道」的失败。
func ExitCode(res RoundResult, err error) int {
	if err != nil {
		return ExitFatal
	}
	if res.Failed() > 0 {
		return ExitFailed
	}
	return ExitOK
}
```

`tests/tracesealer/exit_test.go`（**纯函数测，不编译二进制** —— 见上面那条「30s 批次预算」的理由）：

```go
package tracesealer_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestExitCodeZeroOnCleanRound 全绿轮次退 0。
func TestExitCodeZeroOnCleanRound(t *testing.T) {
	require.Equal(t, tracesealer.ExitOK, tracesealer.ExitCode(tracesealer.RoundResult{Sealed: 2}, nil))
}

// TestExitCodeOneOnFatalError 环境级/启动级错误退 1（与「某一轮有失败」区分开：
// 前者的运维动作是修环境，后者是盯那块 spool）。
func TestExitCodeOneOnFatalError(t *testing.T) {
	require.Equal(t, tracesealer.ExitFatal,
		tracesealer.ExitCode(tracesealer.RoundResult{}, errors.New("根目录不可用")))
}

// TestExitCodeTwoWhenOnceHasFailures 有失败（含幂等之外的任一计数）退 2。
func TestExitCodeTwoWhenOnceHasFailures(t *testing.T) {
	require.Equal(t, tracesealer.ExitFailed, tracesealer.ExitCode(tracesealer.RoundResult{TooManyRows: 1}, nil))
	require.Equal(t, tracesealer.ExitFailed, tracesealer.ExitCode(tracesealer.RoundResult{SealFailed: 1}, nil))
	// PartExists 是幂等无害 ⇒ 不得退 2（否则每天都会有一次「失败」告警）。
	require.Equal(t, tracesealer.ExitOK, tracesealer.ExitCode(tracesealer.RoundResult{PartExists: 3}, nil))
}
```

`tests/tracesealer/options_test.go`（R8 的强制下界必须被钉住 —— 它是 sealer 与网关之间**唯一**的保护）：

```go
package tracesealer_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestNewRunnerDefaultsMinAgeToThreeHours 默认 min-age 必须等于冻结包的
// defaultSealMinAge（3h）：镜像值偏小就等于缩短了与网关滚动之间的安全差。
func TestNewRunnerDefaultsMinAgeToThreeHours(t *testing.T) {
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root:      t.TempDir(),
		NewSealer: stubSealer,
	})
	require.NoError(t, err)
	require.Equal(t, 3*time.Hour, r.Options().MinAge)
}

// TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound R8：min-age 必须**严格**
// 大于网关滚动上界（1h）。min-age == 1h 也拒绝 —— 边界相等意味着「sealer 可能在网关
// 仍在追加的最后一刻动手」，这不是余量，这是赛跑。
func TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound(t *testing.T) {
	for _, d := range []time.Duration{time.Minute, 30 * time.Minute, time.Hour} {
		_, err := tracesealer.NewRunner(tracesealer.Options{
			Root: t.TempDir(), MinAge: d, NewSealer: stubSealer,
		})
		require.ErrorContains(t, err, "必须严格大于网关滚动间隔上界", "min-age=%s 必须被拒", d)
	}
}

// TestNewRunnerRejectsNonPowerOfTwoWindow 窗口要换算成 zstd 的 --long=<log2>，
// 非 2 的幂没有对应参数 ⇒ 必须在这里（启动时）拒绝，而不是几小时后封块时才炸。
//
// 【MinAge 必须显式给值，不能留零值去继承 DefaultMinAge】normalize 里 min-age 下界
// 的判定在窗口 2 的幂判定**之前**，所以若本用例让 MinAge 留零值，它就同时依赖两条
// 互不相干的契约：把 DefaultMinAge 从 3h 改小（变异 D-m02）会让它在 min-age 那一关
// 先失败 ⇒ 这条「窗口」用例被连带杀红，集合 D 的 m02 就不再只杀点名的
// TestNewRunnerDefaultsMinAgeToThreeHours，而多出一个非点名的连带牺牲者（实测 FAIL=2）。
// 显式给 3h（> MinAgeFloor）把本用例钉回「只测窗口的 2 的幂」这一件事上。
func TestNewRunnerRejectsNonPowerOfTwoWindow(t *testing.T) {
	_, err := tracesealer.NewRunner(tracesealer.Options{
		Root: t.TempDir(), ZstdWindowMB: 300, MinAge: 3 * time.Hour, NewSealer: stubSealer,
	})
	require.ErrorContains(t, err, "2 的幂")
}
```

（`stubSealer` 是这三个用例共用的替身构造器，定义在 `tests/tracesealer/round_test.go`：
`func stubSealer(tracearchive.SealerOptions) (tracesealer.Sealer, error) { return &fakeSealer{}, nil }`。
`r.Options()` 是本任务要新增的只读访问器：`func (r *Runner) Options() Options { return r.opts }`
（返回**副本**；Options 里有函数字段，调用方拿到的是同一批函数值，这是可接受的）。

- [ ] **步骤 8：跑全部测试**

运行：`go test ./tests/tracesealer/ -count=1 -v`
预期：PASS（顶层 6（分类）+ 6（轮次）+ 3（退出码）+ 3（选项）= 18 个用例；整批远在 30s 之内 —— 不依赖 zstd 二进制、不碰真实盘、不编译二进制）。

- [ ] **步骤 9：写本任务的变异集合（四个 —— 工具一次只能变异一个文件）**

**集合 A：`tools/trace-mutate/sets/p4-task-1-classify.py`**

```python
NAME = "p4-task-1-classify"
TARGET = "internal/tracesealer/classify.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/classify_test.go"]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/classify_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []   # 纯函数用例：不依赖数据库/zstd/真实盘，没有可 SKIP 的理由
RETIRED = {}

MUTATIONS = {
    # 超限块必须被认成「永久」⇒ 杀手 TestClassifyTooManyRowsIsPermanent
    "m01_不识别TooManyRows": [("sub", "case errors.Is(err, ta.ErrTooManyRows):", "case false:")],
    # 三类哨兵一律用 %w 包装，== 比较会全部落进 unknown ⇒ 杀手 TestClassifyUsesErrorsIsNotEquality
    "m02_改用相等比较": [("sub", "case errors.Is(err, ta.ErrSealFailed):", "case err == ta.ErrSealFailed:")],
    # 未分类不得被吞成「幂等无害」（那等于静默丢数据）⇒ 杀手 TestClassifyUnknownIsLoudAndRetryable
    "m03_未分类吞成PartExists": [("sub", "default:\n\t\treturn KindUnknown", "default:\n\t\treturn KindPartExists")],
    # 行数不符必须自成一类（否则运维分不清「等一下」和「要人工拆块」）⇒ 杀手 TestClassifyRowMismatchIsRetryable
    "m04_不识别RowMismatch": [("sub", "case errors.Is(err, ta.ErrRowCountMismatch):", "case false:")],
    # 幂等无害必须被认出来 ⇒ 杀手 TestClassifyPartExistsIsIdempotent
    "m05_不识别PartExists": [("sub", "case errors.Is(err, ta.ErrPartExists):", "case false:")],
    # Permanent 恒 false ⇒ 永久失败会被当成可重试 ⇒ 杀手 TestClassifyTooManyRowsIsPermanent
    "m06_Permanent恒false": [("sub", "func (k FailureKind) Permanent() bool { return k == KindTooManyRows }",
                              "func (k FailureKind) Permanent() bool { return false }")],
    # Retryable 漏掉 RowMismatch ⇒ 行数不符不再重试 ⇒ 杀手 TestClassifyRowMismatchIsRetryable
    "m07_Retryable漏掉RowMismatch": [
        ("sub", "return k == KindRowMismatch || k == KindSealFailed || k == KindUnknown",
                "return k == KindSealFailed || k == KindUnknown")],
}

CHECKS = [
    {
        "name": "分类只认哨兵常量，不认裸字面量",
        "file": "internal/tracesealer/classify.go",
        "pattern": r"KindTooManyRows\s*FailureKind\s*=\s*\"too_many_rows\"",
        "expect": "present",
        "why": ("这些串会被写进日志与看板/告警规则（轮次汇总里的 kind=…），"
                "是**跨进程契约**；改成任意串就等于把告警规则悄悄打断。"),
    },
]

REQUIREMENTS = [
    {"plan": "冻结包 errors.go：ErrPartExists 幂等无害（不告警）", "mutations": ["m05_不识别PartExists"], "checks": []},
    {"plan": "冻结包 errors.go：ErrRowCountMismatch 可重试、需告警", "mutations": ["m04_不识别RowMismatch", "m07_Retryable漏掉RowMismatch"], "checks": []},
    {"plan": "冻结包 errors.go：ErrSealFailed 可重试、需告警", "mutations": ["m02_改用相等比较"], "checks": []},
    {"plan": "冻结包 errors.go + N5：ErrTooManyRows 永久拒绝、需人工介入", "mutations": ["m01_不识别TooManyRows", "m06_Permanent恒false"], "checks": []},
    {"plan": "计划 4 全局约束：未分类错误不得被吞", "mutations": ["m03_未分类吞成PartExists"], "checks": []},
    {"plan": "计划 4 全局约束：FailureKind 字符串是日志/告警的稳定面", "mutations": [], "checks": ["分类只认哨兵常量，不认裸字面量"]},
]

EXPECTED_GREEN = {}
```

**集合 B：`tools/trace-mutate/sets/p4-task-1-round.py`**（`TARGET = "internal/tracesealer/runner.go"`，
`MANIFEST_FILES = ["tests/tracesealer/round_test.go"]`，`SKIP_WHITELIST = []`，`RETIRED = {}`）

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | `KindPartExists` 分支把 `res.PartExists++` 改成 `res.SealFailed++` | `TestRoundTreatsPartExistsAsIdempotentNoop` |
| m02 | `KindSealFailed` 分支的**自有语句之后、该分支日志之前**补一句 `return res, err`（匹配串是 `case KindSealFailed:` 紧接 `res.SealFailed++`，插入点在这两行之后 —— 这两行是该分支的起始两行、各自唯一；**E61③**：任务 2 已把该分支的日志从 `r.opts.Logf(` 换成 slog，故不再用日志行做锚点；语义 = 一个坏块中断整轮） | `TestRoundSealsEveryEligiblePart` |
| m03 | 把 `if err := ctx.Err(); err != nil {` 改成 `if false {`（忽略取消） | `TestRoundCancellationStopsBeforeNextSeal` |
| m04 | `return res, fmt.Errorf("列举待封块失败: %w", err)` 改成 `return res, nil` | `TestRoundDoesNotSwallowListError` |
| m05 | `KindTooManyRows` 分支的 `res.TooManyRows++` 改成 `res.RowMismatch++`（永久与可重试混在一起） | `TestRoundSeparatesPermanentFromRetryable` |
| m06 | `Failed()` 的求和里删掉 `+ r.UnknownFailed`（**接收者名是 `r` 不是 `res`**：`func (r RoundResult) Failed() int`，`runner.go:32-34`；实现里求和写在**一行**上 —— `r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed`） | `TestRoundCountsUnknownFailuresAsFailures` |

`REQUIREMENTS`：上表逐条 + 「一个坏块不得中断整轮」（m02）+ 「ctx 取消后不得再开新块」（m03）+ 「未分类失败也计入失败」（m06）；
`CHECKS`：`internal/tracesealer/runner.go` 内 `\bMkdir[A-Za-z]*\s*\(` **absent**（理由见下文全局约束的 D70 条；承重证据是 Task 3 的 `TestRootGuardNeverCreatesRoot`，这里只是启发式旁证）。

**集合 C：`tools/trace-mutate/sets/p4-task-1-exit.py`**（`TARGET = "internal/tracesealer/exit.go"`，
`MANIFEST_FILES = ["tests/tracesealer/exit_test.go"]`）

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | `if res.Failed() > 0 {` 改成 `if false {`（有失败也退 0） | `TestExitCodeTwoWhenOnceHasFailures` |
| m02 | `if err != nil { return ExitFatal }` 改成 `if false {`（致命错误退 0） | `TestExitCodeOneOnFatalError` |

`CHECKS`：`cmd/trace-sealer/main.go` 内 `os\.Exit\([0-9]` **absent**（退出码只能来自 `ExitCode`；`main` 里出现字面量就意味着契约被复制成了两份）。

**集合 D：`tools/trace-mutate/sets/p4-task-1-options.py`**（`TARGET = "internal/tracesealer/options.go"`，
`MANIFEST_FILES = ["tests/tracesealer/options_test.go"]`）

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | `if o.MinAge <= MinAgeFloor {` 改成 `if false {`（R8 强制下界消失） | `TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound` |
| m02 | `DefaultMinAge       = 3 * time.Hour` 改成 `= 30 * time.Minute` | `TestNewRunnerDefaultsMinAgeToThreeHours` |
| m03 | 窗口 2 的幂检查改成 `if false && o.ZstdWindowMB&(o.ZstdWindowMB-1) != 0 {` | `TestNewRunnerRejectsNonPowerOfTwoWindow` |

`REQUIREMENTS`：R8 下界（m01）、默认 3h = 冻结包 `defaultSealMinAge`（m02）、`--long=<log2>` 的 2 的幂前提（m03）；
`CHECKS`：`internal/tracesealer/options.go` 内 `min-age=%s 必须严格大于网关滚动间隔上界` **present**（错误文案本身是运维可见面：运维看到这句话才知道是哪个契约被违反了）。

- [ ] **步骤 10：提交（证据在提交之后跑）**

```bash
go build ./... && go vet ./internal/tracesealer/... ./cmd/trace-sealer/...
git add internal/tracesealer/options.go internal/tracesealer/classify.go internal/tracesealer/exit.go \
        internal/tracesealer/runner.go cmd/trace-sealer/main.go tests/tracesealer/ \
        tools/trace-mutate/sets/p4-task-1-classify.py tools/trace-mutate/sets/p4-task-1-round.py \
        tools/trace-mutate/sets/p4-task-1-exit.py tools/trace-mutate/sets/p4-task-1-options.py
git commit -m "feat(trace): trace-sealer 单轮封块、错误分类与退出码（计划 4/4 任务 1）

复用冻结包 internal/tracearchive 的 Sealer（零改动）：ListPendingSeals → Seal。
分类用 errors.Is：ErrPartExists 幂等无害（不计失败）、ErrRowCountMismatch/ErrSealFailed
保留 spool 并可重试、ErrTooManyRows 永久拒绝且标注需人工介入。一个坏块不中断整轮。
min-age 强制 > 1h（R8：min-age 是 sealer 侧唯一保护），默认 3h。
退出码 0/1/2 做成纯函数 ExitCode，便于用例钉住。sealer 侧零建目录调用（D70）。"
```

- [ ] **步骤 11：跑本任务的四个变异集合（**必须在步骤 10 的提交之后**）**

```bash
# ① 四个集合各自的名字清单（必须与集合里的 MANIFEST_DERIVATION 逐字一致）
derive() { grep -h '^func Test' "$1" | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort; }
derive tests/tracesealer/classify_test.go > /tmp/p4-t1-classify-names.txt
derive tests/tracesealer/round_test.go    > /tmp/p4-t1-round-names.txt
derive tests/tracesealer/exit_test.go     > /tmp/p4-t1-exit-names.txt
derive tests/tracesealer/options_test.go  > /tmp/p4-t1-options-names.txt

# ② 逐集合：先 --check（每条变异在 HEAD 上必须唯一定位），再跑权威表
for s in classify round exit options; do
  echo "=== p4-task-1-$s ==="
  python3 tools/trace-mutate/trace_mutate.py --repo . \
    --manifest /tmp/p4-t1-$s-names.txt \
    --mutation-set tools/trace-mutate/sets/p4-task-1-$s.py --check || exit 1
  python3 tools/trace-mutate/trace_mutate.py --repo . \
    --manifest /tmp/p4-t1-$s-names.txt \
    --mutation-set tools/trace-mutate/sets/p4-task-1-$s.py || exit 1
done
```
预期：四个集合 `--check` 全部「唯一定位」（A=7、B=6、C=2、D=3，共 18 条变异）；四张权威表都 `exit=0`，每条**断言红**且杀手名与上表一致，三断言全过，无未登记的绿，静态检查 PASS。
**若某条报「定位到 0 处/多处」⇒ 就地修正变异字符串（不要改实现去迁就）**；若某条红成编译红/超时红 ⇒ 按全局约束改成断言红后重跑；若清单推导与实际执行集合不一致（多/少），先查是不是用例名互为前缀。

**提交后必须记入报告的绑定**（原样抄四份工具头部）：`git rev-parse HEAD`；四个集合、四份清单、工具本身的 sha256；`internal/tracesealer/*.go` 与 `tests/tracesealer/*_test.go` 的 sha256。

---

### Task 2：轮次汇总与告警（结构化日志、心跳、永久失败的上报面）

**文件：**
- 修改：`internal/tracesealer/runner.go`（把 `r.opts.Logf` 换成 `slog` 结构化字段；接入 `Summarize` —— 它的本体在新建的 `alert.go`）
- 修改：`internal/tracesealer/options.go`（新增 `Options.Slog *slog.Logger` —— 字段只能落在 `Options` 所在的文件；**E61①**：原清单漏列本文件）
- 新建：`internal/tracesealer/alert.go`（把 `RoundResult` 映射成「该不该告警、告警级别、稳定字段」）
- 测试：`tests/tracesealer/alert_test.go`

**接口：**
- 依赖输入：`RoundResult`（Task 1）、`Options.Now/Logf`
- 对外产出：
  ```go
  // internal/tracesealer/alert.go
  type Severity int
  const (
      SeverityNone   Severity = iota // 无需告警（含 PartExists）
      SeverityWarn                   // 可重试失败：等下一轮
      SeverityError                  // 永久失败（TooManyRows）或未分类失败
  )
  type Alert struct {
      Severity  Severity
      Reason    string            // 稳定串：round_failed / too_many_rows / unknown_failure
      Fields    map[string]any    // 稳定字段集（供看板/告警规则匹配）
  }
  func Summarize(res RoundResult) Alert
  ```

- [ ] **步骤 1：写失败的测试**

新建 `tests/tracesealer/alert_test.go`：下面是 `Summarize` 的 6 个顶层用例；`Round` 的**运行时告警线**另有 3 条行为用例（注入 `Options.Slog` 捕获 sink，钉具体级别与 reason 串），见 **E62**：

```go
package tracesealer_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestSummarizeCleanRoundIsSilent 全绿轮次**不得**产生任何告警
// （否则每天 288 轮的 INFO 会把真正的故障埋掉）。
func TestSummarizeCleanRoundIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 3, Sealed: 3})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizePartExistsAloneIsSilent ErrPartExists 是预期状态 ⇒ 不得告警。
func TestSummarizePartExistsAloneIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, PartExists: 1})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizeRetryableFailureIsWarn 可重试失败 = WARN（等下一轮），不是 ERROR。
func TestSummarizeRetryableFailureIsWarn(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, SealFailed: 1})
	require.Equal(t, tracesealer.SeverityWarn, a.Severity)
}

// TestSummarizeTooManyRowsIsErrorAndNamed 永久失败必须是 ERROR，且 reason 是稳定串
// （alert 规则按它匹配，不能靠错误文本）。
func TestSummarizeTooManyRowsIsErrorAndNamed(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, TooManyRows: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "too_many_rows", a.Reason)
}

// TestSummarizeUnknownFailureIsError 未分类失败按 ERROR 处理（宁可吵，不可静默）。
func TestSummarizeUnknownFailureIsError(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, UnknownFailed: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "unknown_failure", a.Reason)
}

// TestSummarizeFieldsAreStable 字段集必须固定（看板/告警规则依赖它），
// 且零值也要出现（缺失字段会被误读成「没问题」）。
func TestSummarizeFieldsAreStable(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{})
	for _, k := range []string{"eligible", "sealed", "part_exists", "row_mismatch",
		"seal_failed", "too_many_rows", "unknown_failed", "max_rows", "min_bytes_per_row", "min_ratio"} {
		require.Contains(t, a.Fields, k, "字段 %s 必须恒在", k)
	}
}
```

- [ ] **步骤 2：运行并确认失败**

运行：`go test ./tests/tracesealer/ -run 'TestSummarize' -v`
预期：FAIL（`tracesealer.Summarize` 未定义 ⇒ 编译红）。补齐后 PASS。

- [ ] **步骤 3：实现**

`internal/tracesealer/alert.go`：`Summarize` 用 `res.TooManyRows > 0 ⇒ SeverityError/too_many_rows`，否则 `res.UnknownFailed > 0 ⇒ SeverityError/unknown_failure`，否则 `res.RowMismatch+res.SealFailed > 0 ⇒ SeverityWarn/round_failed`，否则 `SeverityNone`。`Fields` 恒含上面 10 个键（用 `map[string]any` 显式字面量，不用反射）。

`runner.go` 的日志换成 `slog`（`slog.Info`/`Warn`/`Error`，`"reason"` 用 `Alert.Reason`），**保留** `opts.Logf` 供测试注入：实现方式是新增 `Options.Slog *slog.Logger`（与 `Logf` 都缺省 ⇒ `slog.Default()`；`Logf` 由 `normalize` 默认成 `log.Printf` ⇒ 正常路径上 `Slog == nil` 就是走 `Logf` —— **E61②**：三级优先级 `Slog > Logf > slog.Default()`），`Logf` 仅在 `Slog == nil` 时使用。**Round 每轮结束必须打**一条 `slog.Info("trace-sealer round", ...)`——这条是**心跳**：运维侧的告警规则是「>3 × poll-interval 没有看到这条」⇒ 进程挂了/被 mask 了。

- [ ] **步骤 4：跑测试**

运行：`go test ./tests/tracesealer/ -count=1 -v`
预期：PASS（新增 9 个顶层用例 = `Summarize` 6 条 + 运行时告警线 3 条，见 **E62**）。

- [ ] **步骤 5：写本任务的变异集合**

新建 `tools/trace-mutate/sets/p4-task-2-alert.py`：`TARGET = "internal/tracesealer/alert.go"`（**一个集合只能变异一个文件**；`runner.go` 的心跳日志没有可被杀死的用例，因此它只能是 CHECKS 项，见下）、`PACKAGES = ["./tests/tracesealer/"]`。变异至少含：

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | 全绿轮次也报 WARN（`SeverityWarn` 无条件返回） | `TestSummarizeCleanRoundIsSilent` |
| m02 | `PartExists` 计入告警（把 `res.PartExists` 加到失败条件里） | `TestSummarizePartExistsAloneIsSilent` |
| m03 | 可重试失败升级成 ERROR | `TestSummarizeRetryableFailureIsWarn` |
| m04 | `TooManyRows` 降级成 WARN（= 永久失败被当成「等一等就好」） | `TestSummarizeTooManyRowsIsErrorAndNamed` |
| m05 | `Reason` 改成不可匹配的串（例如拼接错误文本） | `TestSummarizeTooManyRowsIsErrorAndNamed` |
| m06 | `Fields` 少一个键（零值时不写） | `TestSummarizeFieldsAreStable` |
| m07 | 未分类失败不再报 ERROR | `TestSummarizeUnknownFailureIsError` |

`CHECKS`：`reason` 字面量必须是 `too_many_rows`/`unknown_failure`/`round_failed` 三个稳定串（正则 present）；`internal/tracesealer/alert.go` 不得出现 `fmt.Sprintf` 拼 reason。
`REQUIREMENTS`：每条计划要求 → 上表变异；「心跳日志每轮必打」→ CHECKS（正则断言 `runner.go` 里 `trace-sealer round` 出现一次且不在任何 `if` 内）+ 运行时「每轮恰好一次」由行为用例 `TestRoundCleanRoundEmitsOnlyHeartbeat` 承载；「告警线级别随 `Severity`、reason 取自 `Alert.Reason`」→ `m01/m03/m04/m05`（两者见 **E62**）。

- [ ] **步骤 6：提交 → 跑证据（次序同 Task 1 步骤 10/11）**

```bash
git add internal/tracesealer/alert.go internal/tracesealer/options.go internal/tracesealer/runner.go \
        tests/tracesealer/alert_test.go tools/trace-mutate/sets/p4-task-2-alert.py
git commit -m "feat(trace): sealer 轮次汇总与告警分级（计划 4/4 任务 2）

PartExists 幂等无害不告警；可重试失败 WARN；TooManyRows/未分类 ERROR。
每轮一条 round 心跳，供「>3×poll 无心跳」的告警规则使用。
reason 为稳定串，禁止用错误文本当告警匹配面。"
```
然后按 Task 1 步骤 11 的命令跑 `p4-task-2-alert.py`（换集合/清单名），记录绑定。

---

### Task 3：常驻循环与根目录护栏（「撤掉挂载必须被看见」）

**文件：**
- 新建：`internal/tracesealer/rootguard.go`（身份基线 + 每轮复检；**零建目录**）
- 修改：`internal/tracesealer/options.go`（新增可注入的 `Identity func(string) (string, error)`）
- 修改：`internal/tracesealer/runner.go`（装配 `RootGuard`、`Round` 前置检查、`RoundResult.RootUnavailable`、`Loop`）
- 修改：`cmd/trace-sealer/main.go`（**删掉 Task 1 的临时 `if !*once` 分支**，改成 `if err := r.Loop(ctx); err != nil { … }`）
- 测试：`tests/tracesealer/rootguard_test.go`、`tests/tracesealer/loop_test.go`

**接口：**
```go
// internal/tracesealer/rootguard.go
var (
    // 根目录不存在/不是目录/不可访问。
    ErrRootUnavailable = errors.New("tracesealer: 根目录不可用")
    // 根目录还在、但背后的挂载已经不是启动时那一个（换盘、umount 后露出空目录）。
    ErrRootIdentityMismatch = errors.New("tracesealer: 根目录身份与启动基线不一致")
)

// Identity 返回路径背后挂载的身份串："<挂载点>|<设备号>|<文件系统类型>"。
// 数据来源 /proc/self/mountinfo；取**最长前缀覆盖** path 的那个挂载点。
func Identity(path string) (string, error)

type RootGuard struct{ /* unexported fields */ }
func NewRootGuard(root string, identity func(string) (string, error)) (*RootGuard, error)
func (g *RootGuard) Check() error      // 每轮调用
func (g *RootGuard) Baseline() string  // 供日志/排障抄

// internal/tracesealer/runner.go（其余不变）
func (r *Runner) Loop(ctx context.Context) error
// RoundResult 新增字段：
//   RootUnavailable bool  // true = 本轮因根目录不可用而**完全没有**调用 Seal
```

- [ ] **步骤 1：写失败的测试（护栏）**

新建 `tests/tracesealer/rootguard_test.go`：

```go
package tracesealer_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestRootGuardRejectsFilesystemRoot 根目录是 `/` 必须被拒。
// 【为什么这是第一条】2026-09-24 的事故就是往 `/` 上写；任何形式的「默认值兜底成 /」
// 都会把同一类事故带回来，所以这一条必须在构造期就响亮失败。
func TestRootGuardRejectsFilesystemRoot(t *testing.T) {
	_, err := tracesealer.NewRootGuard("/", tracesealer.Identity)
	require.ErrorContains(t, err, "文件系统根")
}

// TestRootGuardRejectsRelativePath 相对路径必须被拒：sealer 的工作目录由 systemd
// 决定（WorkingDirectory 默认 /），相对路径会把正文写到谁都没想到的地方。
func TestRootGuardRejectsRelativePath(t *testing.T) {
	_, err := tracesealer.NewRootGuard("datalog/trace-archive-v4", tracesealer.Identity)
	require.ErrorContains(t, err, "绝对路径")
}

// TestRootGuardNeverCreatesRoot 根目录不存在时**必须**失败，且**不得**把它创建出来。
//
// 【这条是本计划最重要的安全断言】一旦用建目录「修复」缺失的根目录，消失的挂载
// 就会被重建到宿主根文件系统上（umount 之后 mountpoint 目录仍然存在，只是变成了
// 宿主根 FS 上的一个普通目录），正文随后写进那片没人会去看的空间。
func TestRootGuardNeverCreatesRoot(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "trace-archive-v4")
	_, err := tracesealer.NewRootGuard(missing, tracesealer.Identity)
	require.ErrorIs(t, err, tracesealer.ErrRootUnavailable)
	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr), "护栏绝不能把根目录创建出来")
}

// TestRootGuardCheckDetectsIdentityChange 目录还在、但背后的挂载换了 ⇒ 必须报
// 身份不符（这就是「撤掉挂载」的机器可判定形态：umount 后 mountinfo 里那条记录没了，
// 最长前缀覆盖退化成上一层挂载 ⇒ 身份串必然变化）。
func TestRootGuardCheckDetectsIdentityChange(t *testing.T) {
	root := t.TempDir()
	cur := "baseline-identity"
	g, err := tracesealer.NewRootGuard(root, func(string) (string, error) { return cur, nil })
	require.NoError(t, err)
	require.NoError(t, g.Check(), "身份未变时必须是绿的")
	cur = "/datalog|8:1|ext4" // 模拟：挂载点被撤掉后身份退化成宿主根 FS
	err = g.Check()
	require.ErrorIs(t, err, tracesealer.ErrRootIdentityMismatch)
	require.ErrorContains(t, err, "启动基线")
}

// TestRootGuardCheckReportsVanishedRoot 运行中根目录整个消失 ⇒ 报「不可用」。
func TestRootGuardCheckReportsVanishedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "r")
	require.NoError(t, os.Mkdir(root, 0o755))
	g, err := tracesealer.NewRootGuard(root, func(string) (string, error) { return "x", nil })
	require.NoError(t, err)
	require.NoError(t, os.Remove(root))
	require.ErrorIs(t, g.Check(), tracesealer.ErrRootUnavailable)
}

// TestRootGuardBaselineIsNonEmpty 基线必须可抄进日志（排障时要能对比前后值）。
func TestRootGuardBaselineIsNonEmpty(t *testing.T) {
	g, err := tracesealer.NewRootGuard(t.TempDir(), func(string) (string, error) { return "dev=42", nil })
	require.NoError(t, err)
	require.Equal(t, "dev=42", g.Baseline())
}
```

`tests/tracesealer/loop_test.go`：

```go
package tracesealer_test

import (
	"context"
	"sync"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestLoopRunsFirstRoundImmediately 启动即跑第一轮（不是先等一个 poll 周期）：
// 否则「配置写错了」这件事要等到下一个周期才会被说出来，而积压也白等一轮。
func TestLoopRunsFirstRoundImmediately(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, time.Hour) // poll 1h：只有「立即跑」才能观察到第一轮
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, f.rounds(), 1)
}

// TestLoopRepeatsRoundsOnPollInterval 周期到点必须继续跑。
func TestLoopRepeatsRoundsOnPollInterval(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(120 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, f.rounds(), 3, "5ms 周期跑 120ms 至少应有 3 轮")
}

// TestLoopStopsOnContextCancel 取消后必须干净返回，且**不再**有新轮次。
func TestLoopStopsOnContextCancel(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	n := f.rounds()
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, n, f.rounds(), "取消之后不得再跑轮次")
}

// TestLoopKeepsRunningWhenRootIsUnavailable 根目录不可用是**环境级**问题：
// 进程必须继续活着（退出会被 systemd 按 RestartSec 反复拉起，重试更密，
// 而重启不会把挂载变回来），靠「每轮一条汇总」的心跳让运维看见。
func TestLoopKeepsRunningWhenRootIsUnavailable(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	r.BreakRootForTest() // 用注入的 Identity 让复检失败（见下）
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, f.rounds(), 3, "根目录不可用时循环仍必须继续（且不 Seal）")
	require.Zero(t, f.sealCalls(), "根目录不可用时**不得**调用 Seal")
}

// TestLoopNeverOverlapsRounds 轮次必须串行：并发的两个 Round 会对同一 (dt,inst,part)
// 同时 Seal，而冻结包明确要求同一个三元组串行（共用同一个 .tmp）。
func TestLoopNeverOverlapsRounds(t *testing.T) {
	f := &countingSealer{sealDelay: 20 * time.Millisecond}
	r := newLoopRunner(t, f, 1*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.Equal(t, 1, f.maxInFlight(), "同时只能有一轮在跑")
}
```

**执行者注意**：`r.BreakRootForTest()` 与 `countingSealer` 是需要自建的测试辅助（`BreakRootForTest` 只在 `_test.go` 里通过导出方法暴露是不行的 —— `Runner` 在非测试包里）。**正确做法**：不要给 `Runner` 加测试专用方法。让 `newLoopRunner` 通过 `Options.Identity` 注入一个「第 N 次调用后开始返回不同身份」的闭包即可：
```go
	identity := func(string) (string, error) {
		if atomic.LoadInt32(&broken) == 1 { return "broken", nil }
		return "baseline", nil
	}
```
`testLoopKeepsRunningWhenRootIsUnavailable` 用这个闭包造「运行中挂载消失」，**删掉 `BreakRootForTest` 那一行**。

- [ ] **步骤 2：跑并确认失败**

运行：`go test ./tests/tracesealer/ -run 'TestRootGuard|TestLoop' -v`
预期：FAIL（编译红：`NewRootGuard`/`Loop`/`Options.Identity` 未定义）。**编译红不算证据**。

- [ ] **步骤 3：实现 `rootguard.go`**

```go
package tracesealer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrRootUnavailable 根目录不存在/不是目录/不可访问。
// ErrRootIdentityMismatch 根目录还在，但背后的挂载已经不是启动时那一个。
//
// 【为什么必须分成两个哨兵】两者的运维动作完全不同（「挂载掉了，去 remount」对
// 「挂载换了，去查为什么换了盘/是不是有人动了 fstab」，以及「目录没了，去看是不是
// 被清理脚本删了」），而这个区分会进 ERROR 日志与看板；靠错误文本匹配是脆的。
// 两者都**不是**冻结包的哨兵：本包绝不新增或复用冻结包的读侧哨兵（草案 D50 同族）。
var (
	ErrRootUnavailable      = errors.New("tracesealer: 根目录不可用")
	ErrRootIdentityMismatch = errors.New("tracesealer: 根目录身份与启动基线不一致")
)

const mountInfoPath = "/proc/self/mountinfo"

// Identity 返回路径背后挂载的身份串："<挂载点>|<设备号>|<文件系统类型>"。
//
// 【为什么不能只看「目录在不在」】umount 之后挂载点目录**仍然存在**（它只是退化成
// 宿主根 FS 上的一个普通目录），于是 os.Stat 照旧成功 —— 「根目录还在」这个判断
// 完全失效，正文会被写进另一块盘。/proc/self/mountinfo 里那条挂载记录消失后，
// 覆盖该路径的最长前缀挂载点变成上一层，身份串必然变化，这个变化是**机器可判定**的。
// 网关侧对同一问题的分析见 internal/gateway/observability/spooler.go（D49），
// 两侧都用同一套思路、各自实现（不改那一侧是为了不使它的变异绑定失效）。
//
// 非 Linux 读不到 mountinfo ⇒ 退化为「设备号」，此时只能发现换设备。
func Identity(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return fallbackIdentity(abs)
	}
	best, bestLen := "", -1
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		halves := strings.SplitN(line, " - ", 2)
		if len(halves) != 2 {
			continue
		}
		left := strings.Fields(halves[0])
		right := strings.Fields(halves[1])
		if len(left) < 5 || len(right) < 1 {
			continue
		}
		mp := unescapeMountField(left[4])
		if !covers(mp, abs) {
			continue
		}
		if len(mp) > bestLen {
			// left[2] 是 major:minor，right[0] 是文件系统类型。
			bestLen = len(mp)
			best = mp + "|" + left[2] + "|" + right[0]
		}
	}
	if best == "" {
		return "", fmt.Errorf("mountinfo 里没有任何挂载覆盖 %s", abs)
	}
	return best, nil
}

// covers 判断挂载点 mp 是否覆盖路径 p（两者都必须是已 Clean 的绝对路径）。
//
// 【"/" 必须特判】根挂载点是唯一的以 "/" 结尾的挂载点；不特判就会走
// strings.HasPrefix(p, "//") 这条永远为假的分支，于是「路径其实就在根文件系统上」
// 这种**最危险**的情形反而取不到身份。
func covers(mp, p string) bool {
	if mp == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == mp || strings.HasPrefix(p, mp+"/")
}

// unescapeMountField 还原 mountinfo 的八进制转义（空格/Tab/换行/反斜杠）。
func unescapeMountField(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// fallbackIdentity 非 Linux 上的退化实现：只有设备号。
func fallbackIdentity(path string) (string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return "", err
	}
	return fmt.Sprintf("dev=%d", uint64(st.Dev)), nil
}

// RootGuard 守住「本轮还能不能封块」。
type RootGuard struct {
	root     string
	baseline string
	identity func(string) (string, error)
}

// NewRootGuard 建立启动基线。**本函数（以及本文件）没有任何建目录调用** —— 这是
// 刻意的：替运维「补一个」根目录，就等于把已经消失的挂载点在宿主根 FS 上重建，
// 正文随后写进那片没人会去看的空间（2026-09-24 事故的同一类）。
// 静态检查（p4-task-3-rootguard.py 的 CHECKS）会钉住这一点。
func NewRootGuard(root string, identity func(string) (string, error)) (*RootGuard, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("根目录必须是绝对路径，实际 %q（systemd 的工作目录不等于你以为的那个）", root)
	}
	if filepath.Clean(root) == "/" {
		return nil, fmt.Errorf("根目录不得是文件系统根 %q（这正是 `/` 被写满那一类事故）", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("%w: stat %s 失败: %w（本进程**不会**替你创建它）", ErrRootUnavailable, root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s 不是目录", ErrRootUnavailable, root)
	}
	if identity == nil {
		identity = Identity
	}
	id, err := identity(root)
	if err != nil {
		return nil, fmt.Errorf("%w: 取启动基线失败: %w", ErrRootIdentityMismatch, err)
	}
	return &RootGuard{root: root, baseline: id, identity: identity}, nil
}

// Check 每轮调用一次；任何失败都**不得**触发建目录、**不得**触发 Seal。
func (g *RootGuard) Check() error {
	info, err := os.Stat(g.root)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRootUnavailable, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s 不再是目录", ErrRootUnavailable, g.root)
	}
	id, err := g.identity(g.root)
	if err != nil {
		return fmt.Errorf("%w: 复检身份失败: %w", ErrRootIdentityMismatch, err)
	}
	if id != g.baseline {
		return fmt.Errorf("%w: 启动基线=%s 现在=%s", ErrRootIdentityMismatch, g.baseline, id)
	}
	return nil
}

// Baseline 供排障时对比（运维看到「启动基线 vs 现在」才知道是换盘还是被 umount）。
func (g *RootGuard) Baseline() string { return g.baseline }
```

- [ ] **步骤 4：把护栏与循环接进 `runner.go`**

`Round` 的**第一件事**是 `r.guard.Check()`；失败时 `res.RootUnavailable = true`、打 ERROR（`reason=root_unavailable` 或 `reason=root-identity-mismatch`）、**直接返回，不调用 `ListPendingSeals` 更不调用 `Seal`**。`NewRunner` 在 `normalize()` 之后立刻 `NewRootGuard(opts.Root, opts.Identity)`。`Loop`：

```go
// Loop 常驻循环：跑一轮 → 等一个周期 → 再跑一轮。**轮次严格串行**。
//
// 【为什么先跑一轮再等】启动即处理积压；更重要的是让「根目录配错了」在启动那一秒
// 就出现在日志里，而不是等一个 poll 周期（默认 5 分钟）之后。
//
// 【为什么环境级失败不退出】退出会被 systemd 按 RestartSec 反复拉起，形成更密的重试；
// 而根目录不可用是环境问题，重启不会修好它。心跳（每轮一条汇总）才是它的告警面。
func (r *Runner) Loop(ctx context.Context) error {
	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := r.Round(ctx); err != nil {
			if ctx.Err() != nil {
				return nil // 收到 SIGTERM：干净退出，封到一半的那块由 ctx 取消路径处理
			}
			r.logger.Error("trace-sealer 本轮失败，下一个周期重试", "err", err, "poll_interval", r.opts.PollInterval.String())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
```

并把 `cmd/trace-sealer/main.go` 里 Task 1 留下的那段临时代码**删掉**，改成：

```go
	if err := r.Loop(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "trace-sealer: %v\n", err)
		os.Exit(tracesealer.ExitFatal)
	}
```

- [ ] **步骤 5：跑测试**

运行：`go test ./tests/tracesealer/ -count=1 -v`
预期：PASS（新增 7 + 7 = 14 个顶层用例；整批仍在 30s 之内）。
**顺便验证 `--once` 与常驻两种形态都还在**：`go run ./cmd/trace-sealer --root /tmp/does-not-exist --once; echo $?` ⇒ 期望 `1`（根目录不存在 + 启动期致命），这正是「不创建目录」的外部可见行为。

- [ ] **步骤 6：写变异集合（两个）**

**`p4-task-3-rootguard.py`**（`TARGET = "internal/tracesealer/rootguard.go"`，`PACKAGES = ["./tests/tracesealer/", "./internal/tracesealer/"]`，`MANIFEST_FILES = ["tests/tracesealer/rootguard_test.go", "internal/tracesealer/covers_test.go"]`；**E64① 订正**：`covers` 不是平台不可达，包内用例进名字清单与证据绑定）：

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | 去掉 `filepath.Clean(root) == "/"` 的拒绝（`if filepath.Clean(root) == "/" {` → `if false {`） | `TestRootGuardRejectsFilesystemRoot` |
| m02 | 去掉绝对路径校验（`if !filepath.IsAbs(root) {` → `if false {`） | `TestRootGuardRejectsRelativePath` |
| m03 | `Check()` 的身份比较改成 `if false && id != g.baseline {`（撤挂载不再被发现） | `TestRootGuardCheckDetectsIdentityChange` |
| m04 | `covers` 去掉 `mp == "/"` 特判（根挂载点覆盖不到任何路径） | `TestCoversRootMountCoversPathsUnderRoot`（**E64① 订正**：`covers` 是**纯字符串函数**、与 `/proc/self/mountinfo` 无关，按 Q3 在**包内**新增 `internal/tracesealer/covers_test.go` 直调它 ⇒ 本变异为**断言红**；原登记的两半「darwin 不可达」与「在 Linux 上会杀掉依赖真实 `Identity` 的用例」均已证伪，`EXPECTED_GREEN` 清空） |
| m05 | `NewRootGuard` 的身份取失败时不再报错（`return nil, fmt.Errorf(...)` → `return &RootGuard{...}, nil`） | `TestRootGuardRejectsWhenBaselineCannotBeTaken`（E63④ 新增：本行原点名的 `TestRootGuardBaselineIsNonEmpty` 注入的 identity **成功**返回 ⇒ 走不到本变异动的分支） |
| m06 | **（唯一允许的「建目录」变异，用来证明杀手真的能看见）** 在 `os.Stat(root)` 失败分支前插入 `_ = os.MkdirAll(root, 0o755)` | `TestRootGuardNeverCreatesRoot` |
| m07 | **（E63⑤ 新增）** `Check()` 的 stat 失败被静默放行（`return fmt.Errorf("%w: %w", ErrRootUnavailable, err)` → `return nil`） | `TestRootGuardCheckReportsVanishedRoot`（本行原点名它却没有杀手：m03 只动身份比对） |
| m08 | **（E63⑤ 新增）** `Baseline()` 返回 `g.root` 而不是 `g.baseline`（排障抄下来的「启动基线」永远是根路径） | `TestRootGuardBaselineIsNonEmpty`（本行原点名它是 m04 的杀手，实测 m04 杀不到它） |

> **m06 的说明（重要）**：这是本计划唯一一处**故意**把违规代码作为变异注入，用来证明「护栏不得建目录」这条要求真的被测到了（否则那条断言只是「碰巧没过那段代码」）。它的变异串必须包含 `os.MkdirAll`，而这恰好也是本集合 `CHECKS` 里那条 `absent` 正则要抓的形态 —— 变异是在 pristine 树的**临时副本**上做的，所以 `CHECKS` 仍然对 `HEAD` 原文生效，两者不冲突。

`CHECKS`（本集合）：
- `internal/tracesealer/rootguard.go` 内 `\bMkdir[A-Za-z]*\s*\(` **absent**（理由同上；承重证据是 `TestRootGuardNeverCreatesRoot`）。
- `internal/tracesealer/options.go` 内 `Identity *func` / `Identity func` **present**（护栏的可注入点是它可测的前提）。
- `internal/tracesealer/rootguard.go` 内 `(?m)^\tif mp == "/" \{$` **present**（**E64① 订正**：`covers` 的根挂载点特判**不是**平台不可达 —— 它的**行为**证据是包内用例 `TestCoversRootMountCoversPathsUnderRoot`、变异 m04 是**断言红**；本行静态检查**另外**钉住该分支的**存在性**；删掉整段特判即红，已实测）。

**`p4-task-3-loop.py`**（`TARGET = "internal/tracesealer/runner.go"`，`MANIFEST_FILES = ["tests/tracesealer/loop_test.go"]`）：

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | `Loop` 改成先等再跑（把 `select` 挪到 `Round` 之前） | `TestLoopRunsFirstRoundImmediately` |
| m02 | 环境级失败改成 `return err`（退出而不是继续） | `TestLoopKeepsRunningWhenRootIsUnavailable` |
| m03 | `Round` 的护栏失败分支不再 `return`（继续往下走 ⇒ 根目录不可用时仍会 Seal） | `TestLoopKeepsRunningWhenRootIsUnavailable` |
| m04 | `Round` 内的 `ctx.Err()` 检查删掉（取消后仍开新块 —— Task 1 已有杀手，这里钉住 `Loop` 侧） | `TestLoopStopsSealingAfterCancel`（E63⑦ 新增：本行原点名的 `TestLoopStopsOnContextCancel` 实测在 m04 下**仍绿**，且 Task 1 的 `TestRoundCancellationStopsBeforeNextSeal` **不在本集合的名字清单里**） |
| m05 | `Round` 去掉 `res.RootUnavailable = true` | `TestLoopReportsRootUnavailable`（E63⑧：补了在册行为用例后，本行原登记的 `EXPECTED_GREEN` **取消** ⇒ 本集合没有任何允许的绿） |
| m06 | **（E63⑥ 新增）** `Loop` 跑完一轮就退出（删掉 `select`/`ticker` 那一段，`return nil`） | `TestLoopRepeatsRoundsOnPollInterval`（本行原点名的杀手都没有：m01 只是「先等再跑」，5ms 周期照样跑很多轮） |
| m07 | **（E63⑥ 新增）** `Loop` 每轮用 goroutine 启动（轮次并行：同一 `(dt,inst,part)` 会被并发 Seal） | `TestLoopNeverOverlapsRounds`（本行原点名它却有杀手：串行是冻结包对同一三元组的硬要求） |

- [ ] **步骤 7：提交 → 跑两个集合（次序同 Task 1 步骤 10/11）**

```bash
git add internal/tracesealer/rootguard.go internal/tracesealer/options.go internal/tracesealer/runner.go \
        cmd/trace-sealer/main.go tests/tracesealer/ tools/trace-mutate/sets/p4-task-3-rootguard.py \
        tools/trace-mutate/sets/p4-task-3-loop.py
git commit -m "feat(trace): sealer 常驻循环与根目录身份护栏（计划 4/4 任务 3）

护栏：绝对路径 / 非 / / 存在 / 启动基线与每轮复检（/proc/self/mountinfo 的最长前缀
挂载点+设备号+文件系统类型）。umount 之后挂载点目录仍在，os.Stat 照旧成功，
只有身份比对能发现「挂载没了」。
护栏侧零建目录调用：替运维补一个根目录 = 把消失的挂载重建到宿主根 FS 上。
循环：先跑一轮再等；轮次严格串行；环境级失败不退出（心跳即告警面）。"
```

---

### Task 4：单一实例锁与 A7 的自动化部分

**文件：**
- 新建：`internal/tracesealer/lock.go`
- 修改：`internal/tracesealer/runner.go`（`NewRunner` 取锁、新增 `Close()`）
- 修改：`cmd/trace-sealer/main.go`（加 `defer r.Close()`）
- 测试：`tests/tracesealer/lock_test.go`、`tests/tracesealer/concurrent_test.go`

**接口：**
```go
// internal/tracesealer/lock.go
var ErrAnotherSealerRunning = errors.New("tracesealer: 已有另一个 sealer 持有锁")
type Lock struct{ /* unexported */ }
func AcquireLock(path string) (*Lock, error)
func (l *Lock) Release() error   // 幂等
// internal/tracesealer/runner.go
func (r *Runner) Close() error   // 释放锁；幂等；进程退出前必须调用
```

- [ ] **步骤 1：写失败的测试（锁）**

```go
package tracesealer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestLockIsExclusive 第二个持有者必须失败。**同一个进程里两次独立 open 也够**：
// flock 锁属于 open file description，两次 open 得到两个不同的 description，
// 因此非阻塞加锁会拿到 EWOULDBLOCK —— 不需要起第二个进程就能测。
func TestLockIsExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l1, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	defer l1.Release()
	_, err = tracesealer.AcquireLock(p)
	require.ErrorIs(t, err, tracesealer.ErrAnotherSealerRunning)
}

// TestLockReleasedAllowsReacquire 释放之后必须能被重新获取（否则一次排障之后
// sealer 就永久起不来了 —— 而这需要重启宿主机才能修）。
func TestLockReleasedAllowsReacquire(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l1, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l1.Release())
	l2, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l2.Release())
}

// TestLockReleaseIsIdempotent 退出路径（defer + 显式）会调两次，必须无害。
func TestLockReleaseIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l.Release())
	require.NoError(t, l.Release())
}

// TestNewRunnerRefusesWhenLockHeld 装配期就必须挡住第二个实例（而不是等它
// 真的开始封块才发现）—— 这是「运维手工 --once 撞上常驻循环」的防线。
func TestNewRunnerRefusesWhenLockHeld(t *testing.T) {
	root := t.TempDir()
	r1, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	defer r1.Close()
	_, err = tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.ErrorIs(t, err, tracesealer.ErrAnotherSealerRunning)
}

// TestNewRunnerReleasesLockOnClose Close 之后同一 root 必须能再起一个。
func TestNewRunnerReleasesLockOnClose(t *testing.T) {
	root := t.TempDir()
	r1, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	require.NoError(t, r1.Close())
	r2, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	require.NoError(t, r2.Close())
}

// TestNewRunnerNeverCreatesRoot 装配失败路径也不得创建根目录（与 Task 3 同一条铁律，
// 这里守的是「先建目录再取锁」的实现顺序）。
func TestNewRunnerNeverCreatesRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	_, err := tracesealer.NewRunner(tracesealer.Options{Root: missing, NewSealer: stubSealer})
	require.Error(t, err)
	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr))
}
```

- [ ] **步骤 2：跑并确认失败** → `go test ./tests/tracesealer/ -run 'TestLock|TestNewRunner' -v`：编译红（`AcquireLock`/`Close` 未定义）。

- [ ] **步骤 3：实现 `lock.go` + 装配**

```go
package tracesealer

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrAnotherSealerRunning 已有另一个 sealer 持有这个 root 的锁。
var ErrAnotherSealerRunning = errors.New("tracesealer: 已有另一个 sealer 持有锁")

// Lock 是**跨进程**单实例锁（flock）。
//
// 【为什么必须有它，而不是「systemd 保证只起一个」】systemd 只保证**它自己**起一个。
// 运维手工跑一次 `--once`（排障、离线标定）与常驻循环撞在一起时，两个进程会对同一个
// (dt, inst, part) 并发调用 Seal —— 冻结包在 writer.go 的 Seal 路径上明确写着
// 「同一个 (dt, inst, part) 必须串行：它们共用同一个 <partPath>.tmp，并发跑会互相
// 交错/覆盖同一个临时文件」。而发现路径的 os.Stat(partPath) 检查也不是原子的：
// 两个进程可以同时通过，然后同时往同一个 .tmp 里写。
// 因此互斥必须是**内核级**的、覆盖整个发现+封块过程，而不是靠「启动时看一眼」。
type Lock struct{ f *os.File }

func AcquireLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开锁文件失败: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w（锁文件 %s）", ErrAnotherSealerRunning, path)
		}
		return nil, fmt.Errorf("加锁失败: %w", err)
	}
	return &Lock{f: f}, nil
}

// Release 解锁并关闭；幂等（defer + 显式调用两条路径都会走到）。
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	// 先显式解锁再关闭：即使 Close 失败，锁也已经放开，不会把下一个 sealer 挡在外面。
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
```

`NewRunner` 的装配次序（**次序本身是契约**）：`normalize()` → `NewRootGuard()`（会 stat 根目录，**不建目录**）→ `AcquireLock(filepath.Join(root, ".sealer.lock"))`。
**锁文件必须落在 root 之内**：root 之外没有可写保证（`ProtectSystem=strict` + `ReadWritePaths=/datalog`），而且放在 root 里才能被「同一个 root 只有一个写者」这条语义覆盖。`NewRunner` 失败时必须把已取得的资源释放干净（先锁后守卫的错误路径也是）。

- [ ] **步骤 4：写 A7 的自动化测试**（真实冻结包 `Writer`，替身 sealer 按真实 `Seal` 的**文件操作次序**收尾；**不依赖 zstd**，因为真封块需要本机有 zstd 而 SKIP 会让变异证据无效）

```go
package tracesealer_test

// ... imports: ta, tracesealer, os, path/filepath, fmt, strings, time, sync/atomic

// handoff sealer：模拟冻结包 Seal 成功后的**两个副作用**（写产物、删 spool），
// 顺序与冻结包一致（先建产物再删 spool：反过来会让中途崩溃时两边都没有）。
type handoffSealer struct {
	root     string
	handed   map[string]int // "dt/inst/part" -> 交给它时的最高行号
	inflight int32
	maxIn    int32
}

// TestA7NoRefPointsToNothing 每一个已返回的 Ref 都必须有落点：产物在场（已交接）
// 或 spool 在场且该行存在（未交接）。
// 【为什么用替身】真封块要 zstd；而本仓库的变异工具要求 SKIP == 0，
// requireZstd 式的跳过会让整批证据判为无效。次序（D7 的机制）在这里钉住；
// 真实 zstd 路径由计划 1 的 tests/tracearchive/ 覆盖，进程级由任务 8 的剧本覆盖。
func TestA7NoRefPointsToNothing(t *testing.T) { /* 见下一个用例的骨架 */ }

// TestA7WriterNeverAppendsToAHandedOffPart 一旦某个 part 被交给 sealer（= 网关已
// MarkSealed 关掉 fd），后续 Append **绝不允许**再落到这个 part 上。
// 这正是 N1/R8 的次序不变量：先 MarkSealed、后交接。反过来写就是 D7
// （数据写进已被 unlink 的 inode：落盘成功、永久不可达）。
func TestA7WriterNeverAppendsToAHandedOffPart(t *testing.T) { … }

// TestA7RestartAfterSealStartsAtNextPart 进程重启（新 Writer，同一 root）在
// **产物已存在、spool 已删**之后必须从 part+1 继续（冻结包的目录扫描发现逻辑）。
func TestA7RestartAfterSealStartsAtNextPart(t *testing.T) { … }

// TestA7RoundIsSerializedWithLock 常驻循环持锁期间，第二个 Runner（模拟手工 --once）
// 必须被拒。
func TestA7RoundIsSerializedWithLock(t *testing.T) { … }

// TestA7NoPartIsSealedTwiceConcurrently 一个轮次内对同一 part 只能有一次 Seal 调用
// （冻结包要求同一个三元组串行）。
func TestA7NoPartIsSealedTwiceConcurrently(t *testing.T) { … }
```

**骨架（执行者按此补全，注意 30s 批次预算：总循环 200 次 Append 足够，不要再加）**：

```go
func TestA7WriterNeverAppendsToAHandedOffPart(t *testing.T) {
	root := t.TempDir()
	// BlockBytes=1 ⇒ PendingSeals 的「按大小滚动」条件恒真（Append **不会**自己滚动：
	// 滚动只由调用方的 PendingSeals + MarkSealed 触发）；SealInterval=1h 只是让它
	// 不因时间阈值滚动而干扰本用例的确定性。
	// 【冻结契约】WriterOptions 只有 Root / Inst / BlockBytes / SealInterval 四个字段
	//（internal/tracearchive/writer.go:113-118）——**没有** flush 周期这类配置项
	//（周期由网关配置层的 `TRACE_BODY_FLUSH_INTERVAL` 决定）；flush 是
	// PendingSeals 的职责，不是 Writer 的配置项。
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1,
		SealInterval: time.Hour})
	require.NoError(t, err)
	hs := &handoffSealer{root: root, handed: map[string]int{}}
	dt := "2026-10-08"
	sealed := map[int]bool{}
	for i := 0; i < 200; i++ {
		ref, err := w.Append(dt, []byte(fmt.Sprintf(`{"i":%d,"pad":"%s"}`, i, strings.Repeat("x", 64))))
		require.NoError(t, err)
		require.False(t, sealed[ref.Part], "part %d 已交接给 sealer，不得再被追加（D7）", ref.Part)
		if i%40 == 39 {
			// ① PendingSeals 是「把块交给 sealer」的入口，它**无条件 flush** 当前块
			//    （冻结包 writer.go:353-380）：漏 flush 会让 sealer 少读行，因此
			//    flush 失败必须上抛 ≠ nil 返回，这里 require.NoError 就是在钉这一点。
			tasks, err := w.PendingSeals()
			require.NoError(t, err)
			require.Len(t, tasks, 1, "BlockBytes=1 ⇒ 每轮必有一块待封")
			task := tasks[0]
			// ② 次序不变量（N1/R8）：先 MarkSealed 滚掉旧 part（关 fd），sealer 才可能
			//    遇到它。反过来就是 D7 —— sealer 删掉 spool 后网关仍持有 fd，追加写进
			//    已 unlink 的 inode（落盘成功、永久不可达）。
			//    【冻结契约】MarkSealed(dt string, part int) **无返回值**（writer.go:420，
			//    注释明确「调用点已冻结为无返回值」）⇒ 不得写成 require.NoError(t, ...)。
			w.MarkSealed(task.DT, task.Part)
			// ③ 最后才交接（替身模拟冻结包 Seal 成功路径的两个副作用）。
			require.NoError(t, hs.handoff(task))
			sealed[task.Part] = true
		}
	}
	require.NoError(t, w.Close())
	require.NoError(t, hs.assertEveryRefLands())
}
```

`handoffSealer.handoff(task ta.SealTask)` 的动作（**与冻结包 `Seal` 成功路径同序**）：`os.MkdirAll` 出产物目录（**这是替身，不是 sealer 实现**；真实 MkdirAll 在冻结包里）→ 写一个占位 `part-NNNNNN.jsonl.zst`（路径由 `task.SpoolPath` 同目录的产物路径得出）→ `os.Remove(task.SpoolPath)`。`assertEveryRefLands` 逐个 Ref 判定：产物文件存在 ⇒ OK；否则 spool 文件存在且行数 > row ⇒ OK；两者皆无 ⇒ 报「Ref 丢了」。

- [ ] **步骤 5：跑测试** → `go test ./tests/tracesealer/ -count=1 -v`。预期 PASS（新增 8 + 5 = 13 个用例；**E65 订正**：原写 6 + 5 = 11，本轮按步骤 3 自己的两条要求另补了 `TestNewRunnerCloseIsIdempotent` 与 `TestNewRunnerReleasesLockWhenSealerFails`）。若 A7 用例超过 5s，先减 Append 次数（30s 批次上限是硬约束）。

- [ ] **步骤 6：写变异集合与静态检查**

**`p4-task-4-lock.py`**（`TARGET = "internal/tracesealer/lock.go"`，`MANIFEST_FILES = ["tests/tracesealer/lock_test.go"]`）：下表 **m02–m05**。
**`p4-task-4-runner.py`**（`TARGET = "internal/tracesealer/runner.go"`，`MANIFEST_FILES = ["tests/tracesealer/lock_test.go", "tests/tracesealer/concurrent_test.go"]`）：下表 **m06–m09**。**E66⑤**：工具一次只能变异一个文件（全局约束第 42 条）⇒ 原先把 `runner.go` 的匹配串塞进 `TARGET = lock.go` 的集合，只会在 `--check` 得到 `PATTERN-MISS: count=0`；E65(d) 已裁定拆分，本处**文本**同步（m01–m09 编号原样保留，`runner.go` 的 m06–m09 共用同一份名字清单）。

| 编号 | 变异 | 杀手（顶层用例） |
|---|---|---|
| m01 | 加锁改用阻塞模式（去掉 `LOCK_NB`） | 无 —— **不得**这样变异：阻塞会让整批挂死，红成「超时」= 无效证据。改用下面 m02/m03 |
| m02 | `LOCK_EX` 改成 `LOCK_SH`（排他变共享：两个 sealer 都能进） | `TestLockIsExclusive` |
| m03 | `EWOULDBLOCK` 分支不再返回哨兵错误（`return nil, fmt.Errorf(...)` → `return nil, nil`） | `TestLockIsExclusive` |
| m04 | `Release` 不再解锁/关文件（函数体直接 `return nil`） | `TestLockReleasedAllowsReacquire` |
| m05 | `Release` 里 `f := l.f; l.f = nil` 去掉（第二次调用 panic 或重复关） | `TestLockReleaseIsIdempotent` |
| m06 | `NewRunner` 不再取锁（删掉那段） | `TestNewRunnerRefusesWhenLockHeld` |
| m07 | `NewRunner` 先建 root 再取锁（顺序颠倒，插入 `os.MkdirAll`；实现形态是先补 `"os"` 导入再插入 —— 见 E65「如实登记」第 2 条） | `TestNewRunnerNeverCreatesRoot` |
| m08 | `Close` 不释放锁（删掉 `Release` 调用） | `TestNewRunnerReleasesLockOnClose` |
| m09 | `NewRunner` 装配失败时不释放已取得的锁 | `TestNewRunnerReleasesLockWhenSealerFails` |

> m01 的说明必须原样保留在集合里：**「让批次挂死的变异」不是好变异**，它产出的红是超时红（无效证据）。这条写进集合注释，是为了让后来者不要「顺手补上一条更强的变异」而把证据质量弄坏。

`CHECKS`（本集合）：
- `internal/tracesealer/lock.go` 内 `syscall\.Flock` **present** 且 `LOCK_NB` **present**（阻塞加锁会让整个进程无法排障）。
- `internal/tracesealer/lock.go` 内 `\bMkdir[A-Za-z]*\s*\(` **absent**（D70「整包零建目录」；**E66③** 补齐 —— 原先只有 `runner.go`（集合 B）与 `rootguard.go`（集合 A）各有该行）。
- `internal/tracesealer/runner.go` 内 `syscall\.Flock|os\.Mkdir` **absent**（互斥只能有一处实现；禁止在 runner 里再补一层「文件存在即认为有实例」的家酿锁 —— 那种锁在崩溃后留下的陈旧文件会让 sealer 永久起不来）。
- `tests/tracesealer/concurrent_test.go` 内 `TestA7WriterNeverAppendsToAHandedOffPart` **present**（A7 的次序不变量有可执行载体；这条只能静态检查，因为它的被测对象是冻结包的次序，本计划不能变异冻结包 —— 如实登记）。

- [ ] **步骤 7：提交 → 跑集合**（命令同 Task 1 步骤 10/11；集合名换 `p4-task-4-lock` **与** `p4-task-4-runner` —— **两个**集合各跑一次 `--check` 与一张权威表（E66⑤ 的文本同步）；并**额外**登记 A7 的零覆盖说明）。

---

### Task 5：systemd unit、构建与 CI、旧归档定时器隔离

**文件：**
- 新建：`deploy/systemd/trace-sealer.service`
- 新建：`deploy/systemd/isolate-old-trace-archive.sh`、`deploy/systemd/restore-old-trace-archive.sh`
- 修改：`Makefile`（`build-sealer`、`build-trace-verify`）
- 修改：`.gitlab-ci.yml`（编译 + 分发 sealer 二进制）
- 测试：`tests/deploy/trace_sealer_unit_test.go`、`tests/deploy/trace_isolate_script_test.go`、`tests/deploy/trace_build_contract_test.go`

**接口：** 交付物是**文件内容契约**：unit 的每个关键指令、脚本的默认 dry-run、Makefile 的目标名、CI 里的编译与安装命令，全部由 Go 测试（`package deploy_test`，只读文件）钉住。

- [ ] **步骤 1：写 unit 与脚本（先写产物）**

`deploy/systemd/trace-sealer.service`：

```ini
# 调用链正文封块器（方案 C：网关只写 spool，压缩由本进程调 C 版 zstd 完成）。
# 部署路径：/etc/systemd/system/trace-sealer.service（由 CI 或运维从本仓库同步）。
# 上线/回滚剧本见 deploy/blue-green/README.md 与 docs/deployment.md 的运行手册一节。
# ⚠️ 本 unit **不得**在「读侧（计划 3）就绪 + 挂载在场」之前 enable（见运行手册的硬门）。
[Unit]
Description=调用链正文封块器（spool → zstd -19 --long=29 产物）
Documentation=file:///opt/llm-gateway/docs/deployment.md
After=local-fs.target
# 挂载不在场就不要启动；挂载被 umount 时本单元随之下线（纵深防御，
# 主防线是进程内的根目录身份护栏 —— 它每轮复检并把结论写进日志）。
RequiresMountsFor=/datalog

[Service]
Type=simple
User=root
# 宿主机自带的 zstd；版本是压缩比标定的前提（10.26× 是 v1.4.8 专属），
# 因此显式给出绝对路径与期望版本，**不许**走 PATH 漂移。
ExecStart=/usr/local/bin/trace-sealer \
    --root /datalog/trace-archive-v4 \
    --zstd-binary /usr/bin/zstd \
    --zstd-level 19 \
    --zstd-window-mb 512 \
    --expected-zstd-version 1.4.8 \
    --min-age 3h \
    --poll-interval 5m \
    --lock-file /datalog/trace-archive-v4/.sealer.lock
# 致命错误后重试；**不要**用 always：锁冲突（人为再起一个 --once）会变成重启风暴。
Restart=on-failure
RestartSec=30s
# 关停：SIGTERM 交给整个 cgroup（含 zstd 子进程）⇒ Seal 走 ctx 取消路径
# （删 .tmp、保留 spool，下轮重新发现）。60s 足够；**不必**等一个 -19 的块压完。
KillMode=control-group
KillSignal=SIGTERM
TimeoutStopSec=60s
# 日志全部进 journald；Unit 的可观测性就是它的失败面。
StandardOutput=journal
StandardError=journal
SyslogIdentifier=trace-sealer

# ── 资源限制（规格 §11.2 V1：编码器内存落在本机，不再进网关的 2 GiB）──
# 口径：C 版 CLI 的 RSS ≈ 窗口 × 1.08 ≈ 570 MB（512 MiB 窗口）；Go 侧偏移表
# 8 B/行 × 上界 8,388,608 行 ≤ 64 MB。2G 给了 ~2.5× 余量；A6（任务 9）必须
# 实测本进程与 zstd 子进程的 VmHWM 并回填/修正这两个值。
MemoryMax=2G
MemoryHigh=1500M
# 相对权重减半：sealer 有 3h 的余量，不该和网关抢 IO/CPU。
CPUWeight=50
IOWeight=50
IOSchedulingClass=best-effort
IOSchedulingPriority=6
Nice=10
LimitNOFILE=4096

# ── 收窄可写面（承重）──
# 除 /datalog 之外整个文件系统只读。这条把「因为建目录/写日志把宿主根文件系统写满」
# 这类事故直接堵死（2026-09-24 `/` 被写满的同一类）。
ProtectSystem=strict
ReadWritePaths=/datalog
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
UMask=0022
```

> **为什么是 `Type=simple` 常驻循环而不是 `.timer`**：① 规格 §11.1 给的 `Restart=on-failure` 只有在常驻形态下才成立（oneshot+timer 形态下它几乎无意义）；② 常驻进程的「每轮一条汇总」是天然心跳，timer 形态下「timer 被 mask 了」只能靠 `systemctl list-timers` 才发现；③ 封块是幂等的目录扫描，常驻不会带来重复劳动。`--once` 保留给手工排障与离线标定。

`deploy/systemd/isolate-old-trace-archive.sh`（**默认 dry-run**）：

```bash
#!/bin/bash
# 隔离旧的事后归档定时器（生产：trace-archive.timer，每天 04:30 调
# /root/daily-trace-archive.sh，脚本内 RETAIN_DAYS=5）。
#
# ⚠️ 本脚本**默认 dry-run**；真正动手必须 `--confirm`。
# 回滚：bash restore-old-trace-archive.sh --confirm
set -euo pipefail

OLD_TIMER=trace-archive.timer
OLD_SCRIPT=/root/daily-trace-archive.sh
NEW_ROOT=/datalog/trace-archive-v4
CONFIRM=0
[ "${1:-}" = "--confirm" ] && CONFIRM=1

echo "== 1) 现状留证（抄进上线记录）=="
systemctl is-enabled "$OLD_TIMER" || true
systemctl is-active  "$OLD_TIMER" || true
systemctl list-timers "$OLD_TIMER" --all --no-pager || true
echo "--- $OLD_SCRIPT（人工过目：它的删除谓词到底会删什么）---"
sed -n '1,200p' "$OLD_SCRIPT"

echo "== 2) 删除谓词审查：旧脚本绝不能删到新树 =="
# 只看删除类语句；命中新树或 /datalog 顶层 ⇒ 直接 STOP（这属于「先核实再上线」）。
HITS="$(grep -nE '(^|[^[:alnum:]_])(rm[[:space:]]+-[[:alnum:]]*[rf]|find[[:space:]].*-delete|rsync[[:space:]].*--delete)' "$OLD_SCRIPT" || true)"
echo "${HITS:-（未发现删除类语句）}"
if printf '%s\n' "$HITS" | grep -qE "$NEW_ROOT|/datalog([^/]|$)"; then
  echo "STOP: 旧脚本的删除谓词可能命中 $NEW_ROOT（或 /datalog 顶层）⇒ 禁止上线，先人工核对" >&2
  exit 3
fi

if [ "$CONFIRM" != "1" ]; then
  echo "[dry-run] 将要执行： systemctl disable --now $OLD_TIMER ; systemctl mask $OLD_TIMER"
  echo "[dry-run] 加 --confirm 才会真的动手"
  exit 0
fi

echo "== 3) 停用并 mask（幂等）=="
systemctl disable --now "$OLD_TIMER"
systemctl mask "$OLD_TIMER"
if systemctl is-active --quiet "$OLD_TIMER"; then
  echo "STOP: $OLD_TIMER 仍处于 active" >&2; exit 4
fi
systemctl list-timers --all --no-pager | grep -F "$OLD_TIMER" || echo "OK: 已从 list-timers 消失"
echo "完成。回滚： bash restore-old-trace-archive.sh --confirm"
```

`restore-old-trace-archive.sh`：同结构，`unmask` → `enable --now` → 校验 `is-enabled`/`list-timers` 又能看到它，默认 dry-run。

- [ ] **步骤 2：写失败测试**

`tests/deploy/trace_sealer_unit_test.go`（**一个属性一个顶层用例**，全部读文件、纯文本断言）：

```go
package deploy_test

// TestSealerUnitIsResidentLoopWithRestart 形态与重启策略。
func TestSealerUnitIsResidentLoopWithRestart(t *testing.T) {
	u := readUnit(t, "trace-sealer.service")
	require.Contains(t, u, "Type=simple")
	require.Contains(t, u, "Restart=on-failure")
	require.Contains(t, u, "RestartSec=")
	require.Contains(t, u, "ExecStart=/usr/local/bin/trace-sealer")
	require.NotContains(t, u, "Type=oneshot", "选了常驻形态；oneshot+timer 会让 Restart=on-failure 失去意义")
}

// TestSealerUnitPinsZstdBinaryLevelAndVersion 编码器参数逐字锁定（换 zstd 版本
// 必须重新标定压缩比，所以版本是承重配置，不许走 PATH 漂移）。
func TestSealerUnitPinsZstdBinaryLevelAndVersion(t *testing.T) { … --zstd-binary /usr/bin/zstd / --zstd-level 19 / --zstd-window-mb 512 / --expected-zstd-version 1.4.8 … }

// TestSealerUnitPinsMinAgeAboveGatewayRollBound R8 在 unit 层的落点：3h 且不可能
// 被静默改小（进程里还有一道强制下界）。
func TestSealerUnitPinsMinAgeAboveGatewayRollBound(t *testing.T) { … --min-age 3h … }

// TestSealerUnitKnowsItsRootAndLockFile 根目录与锁文件必须在同一个 root 里
// （锁在 root 之外没有可写保证）。
func TestSealerUnitKnowsItsRootAndLockFile(t *testing.T) { … --root /datalog/trace-archive-v4 / --lock-file /datalog/trace-archive-v4/.sealer.lock … }

// TestSealerUnitRequiresDatalogMount 挂载依赖（纵深防御）。
func TestSealerUnitRequiresDatalogMount(t *testing.T) { … RequiresMountsFor=/datalog / After=local-fs.target … }

// TestSealerUnitConfinesWritesToDatalog 可写面收窄（承重）：除 /datalog 外只读。
func TestSealerUnitConfinesWritesToDatalog(t *testing.T) { … ProtectSystem=strict / ReadWritePaths=/datalog / NoNewPrivileges=true / PrivateTmp=true / ProtectHome=true … }

// TestSealerUnitLimitsMemoryCpuAndIO 资源限额（规格 §11.2 V1）。
func TestSealerUnitLimitsMemoryCpuAndIO(t *testing.T) { … MemoryMax= / MemoryHigh= / CPUWeight= / IOSchedulingClass=best-effort / IOSchedulingPriority= / Nice= … }

// TestSealerUnitLogsToJournal 日志进 journald 且标识固定（告警规则按它匹配）。
func TestSealerUnitLogsToJournal(t *testing.T) { … StandardOutput=journal / StandardError=journal / SyslogIdentifier=trace-sealer … }

// TestSealerUnitNeverUsesSealIntervalKey D64/D71：规格 §11.1 的 TRACE_BODY_SEAL_INTERVAL
// **不是有效键名**（同一概念在网关侧叫 TRACE_BODY_ROLL_INTERVAL，生产取 1h），
// sealer 也没有 --seal-interval —— 它绝不能出现在 unit 里冒充有效配置。
func TestSealerUnitNeverUsesSealIntervalKey(t *testing.T) {
	require.NotContains(t, readUnit(t, "trace-sealer.service"), "SEAL_INTERVAL")
	require.NotContains(t, readUnit(t, "trace-sealer.service"), "--seal-interval")
}
```

`tests/deploy/trace_isolate_script_test.go`：

```go
// TestIsolateScriptDefaultIsDryRun 脚本默认必须不动手（`[ "$CONFIRM" != "1" ]` 分支
// 必须在任何 systemctl 变更命令**之前**）。
// TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot 删除谓词审查必须存在且
// 命中新树时 exit 3（这是 D65 的机器可判定部分）。
// TestIsolateScriptStopsAndMasksTimer 必须 disable --now + mask + 复核 is-active +
// 打印回滚命令。
// TestRestoreScriptUnmasksAndEnables 回滚脚本必须 unmask + enable --now + 复核。
// TestIsolateAndRestoreScriptsAreExecutable 两个脚本必须带可执行位
// （仓库里 chmod +x 过；用一个只读文件系统权限位断言）。
```

`tests/deploy/trace_build_contract_test.go`：

```go
// TestMakefileBuildsSealerForLinux CGO_ENABLED=0 + GOOS=linux + GOARCH=amd64 +
// 目标 ./cmd/trace-sealer（本机是 darwin，不交叉编译就会把 Mach-O 传上去）。
// TestMakefileBuildTargetsIncludeSealer `make build` 必须包含 sealer（否则 CI 走
// 默认目标时悄悄漏掉它）。
// TestCICIBuildsAndInstallsSealer 上线作业里必须出现 `go build` ./cmd/trace-sealer
// 与 `install -m 0755 … /usr/local/bin/trace-sealer`。
// TestCICIDoesNotEnableSealerUnit CI **不得** enable/start trace-sealer.service
// （启用是人工门，见运行手册的硬门）。
```

- [ ] **步骤 3：跑并确认失败** → `go test ./tests/deploy/ -v`：FAIL（产物文件不存在 ⇒ 读文件失败）。补齐 `Makefile` / `.gitlab-ci.yml` 改动后 PASS。

- [ ] **步骤 4：Makefile 与 CI**

```make
# ── 变量（放在既有 Makefile:6-9 的「变量」区，紧跟 GATEWAY_BIN / CP_BIN 的写法）──
# 仓库现有 Makefile **没有** SEALER_BIN / VERIFY_BIN（只有 GATEWAY_BIN := bin/gateway
# 与 CP_BIN := bin/control-plane，Makefile:8-9）⇒ 必须补定义，否则下面两个目标展开成
# `go build -o  ./cmd/...`，会以「-o 后面是空串」报错（而不是安静地成功）。
SEALER_BIN := bin/trace-sealer
VERIFY_BIN := bin/trace-verify

# 新增目标；`build-trace-verify` **不进** build（验收工具不进任何镜像、不参与生产上线）。
build: build-gateway build-cp build-sealer

# sealer 跑在 Linux 宿主上；本机是 darwin，所以必须交叉编译。
build-sealer:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(SEALER_BIN) ./cmd/trace-sealer

# 验收工具：只在本机/测试机跑，不进任何镜像、不参与生产上线。
build-trace-verify:
	go build -o $(VERIFY_BIN) ./cmd/trace-verify
```

同时把 `build-sealer` / `build-trace-verify` 加进 `Makefile:1` 的 `.PHONY`（既有写法是 `swagger dev clean remove-bg` 那种空格分隔的一行）。

`.gitlab-ci.yml`：在**已经编译 Go 二进制并分发到目标主机**的那个作业里（测试环境是 `deploy-app-test-fast` 附近那处 `CGO_ENABLED=0 go build`；生产是发布作业里的同类步骤）追加 sealer 的编译与安装。**执行者必须先 `grep -n "CGO_ENABLED=0 go build" .gitlab-ci.yml` 看清有哪几处**，只改「真的会把二进制放到目标主机」的那一处，并且**不**在生产作业里 `systemctl enable/start`：

```yaml
        # 在既有 go build 步骤之后追加（缩进对齐该作业现有的 script 块）
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /tmp/trace-sealer ./cmd/trace-sealer
        ssh ${SSH_OPTS} ${DEPLOY_USER}@${DEPLOY_HOST} "mkdir -p /usr/local/bin"
        scp ${SSH_OPTS} /tmp/trace-sealer ${DEPLOY_USER}@${DEPLOY_HOST}:/tmp/trace-sealer.new
        ssh ${SSH_OPTS} ${DEPLOY_USER}@${DEPLOY_HOST} "install -m 0755 /tmp/trace-sealer.new /usr/local/bin/trace-sealer && rm -f /tmp/trace-sealer.new && /usr/local/bin/trace-sealer --version"
        # unit 文件也一起同步（但**不** enable/start —— 启用是人工门）
        scp ${SSH_OPTS} deploy/systemd/trace-sealer.service ${DEPLOY_USER}@${DEPLOY_HOST}:/etc/systemd/system/trace-sealer.service
        ssh ${SSH_OPTS} ${DEPLOY_USER}@${DEPLOY_HOST} "systemctl daemon-reload"
```

> ⚠️ **变量名必须用该作业里已有的名字**（`DEPLOY_USER`/`DEPLOY_HOST` 只是示意；测试作业用的是 `TEST_USER`/`TEST_HOST`）。执行者按实际作业里的变量替换，并用 `git diff` 自查没有引入未定义变量。
> ⚠️ 测试环境的任何**配置/变量**改动都要先过 `test-env-config` skill：本任务只新增「二进制的编译与分发」，**不新增任何变量、不动 compose**（compose 在任务 6）。若执行者发现不得不新增变量 ⇒ 先停下读 skill，再按它的 9 步走。

- [ ] **步骤 5：隔离旧定时器（**生产机上的破坏性步骤**，逐步 + 停止点 + 回滚）**

> 本步骤**只在真正上线时执行**，执行前必须已核对任务 6 的挂载在场。**生产次序（与文末「生产上的执行次序」逐字一致）**：先做任务 6 的 `/datalog` 挂载与「挂载在场、功能关闭」的分阶段上线（含它的停止点 1–3），**确认挂载在场之后**才执行本步骤的隔离 —— 反过来（先 mask 旧 timer、后挂载）只会把「`/datalog` 没有任何 retention」的窗口拉得更长（见「风险登记 R1」）。产线命令一律逐步执行并抄录输出。

1. `sudo bash deploy/systemd/isolate-old-trace-archive.sh`（**dry-run**）。抄录：`systemctl is-enabled/is-active`、`list-timers`、以及第 2 步打印出来的删除类语句。
2. ⛔ **停止点 1**：人工读完 `/root/daily-trace-archive.sh` 的删除类语句，确认它们的路径**不可能**匹配 `/datalog/trace-archive-v4`（特别注意 `RETAIN_DAYS=5` 与任何 `find <根> -mtime +5 -delete`：若 `<根>` 是 `/datalog` 或其上层，就会删到新树）。**若无法确认 ⇒ 停止上线**，先去核实。
3. ⛔ **停止点 1.5（R1 的量化门，必须先于 `--confirm`）**：`mask` 会把旧 timer 的**归档与保留/清理一起**停掉，而写侧启用被 D73 硬门无限期推迟 ⇒ 隔离窗口内 `/datalog` **没有任何 retention**。因此在 mask 之前必须**量化**：取两次 `du -sb /datalog`（间隔 ≥24h；不足 24h 时按小时外推并在记录里注明外推），算出增长速率（GB/天）与剩余可用空间（当天 `df -h /datalog` 的可用值）能撑的天数；若「可撑天数 < 预计隔离窗口 + 5 天余量」（**规格未规定该阈值；「+5 天余量」是本计划提出的建议默认值**，可依实际风险偏好调整，但必须写明取值理由）⇒ **不得 mask**，改走 R1 的选项 1（只停归档动作、保留清理动作）或选项 2（另设只做保留清理的 unit）。量化命令与原始输出必须抄录进上线记录与交付报告。
4. `sudo bash deploy/systemd/isolate-old-trace-archive.sh --confirm`。
5. 抄录复核：`systemctl is-active trace-archive.timer`（期望 `inactive`）、`systemctl is-enabled trace-archive.timer`（期望 `masked`）、`systemctl list-timers --all | grep trace-archive`（期望无输出）。
6. ⛔ **停止点 2**：确认第 5 步全部符合预期，才继续任务 6/7。
7. **回滚**（任何时候不想要这次隔离）：`sudo bash deploy/systemd/restore-old-trace-archive.sh --confirm`，复核 `is-enabled` = `enabled` 且 `list-timers` 又能看到它。

- [ ] **步骤 6：写变异集合（**五个**：unit / isolate / restore / build / ci）**

**`p4-task-5-unit.py`**（`TARGET = "deploy/systemd/trace-sealer.service"`，`PACKAGES = ["./tests/deploy/"]`，`MANIFEST_FILES = ["tests/deploy/trace_sealer_unit_test.go"]`）：每条变异改 unit 的**一行**，杀手对应上表用例。至少：

| 编号 | 变异 | 杀手 |
|---|---|---|
| m01 | `Type=simple` → `Type=oneshot` | `TestSealerUnitIsResidentLoopWithRestart` |
| m02 | `Restart=on-failure` → `Restart=no` | `TestSealerUnitIsResidentLoopWithRestart` |
| m03 | `--expected-zstd-version 1.4.8` → `1.5.7` | `TestSealerUnitPinsZstdBinaryLevelAndVersion` |
| m04 | `--zstd-binary /usr/bin/zstd` → 删掉该行（走 PATH） | `TestSealerUnitPinsZstdBinaryLevelAndVersion` |
| m05 | `--min-age 3h` → `--min-age 30m` | `TestSealerUnitPinsMinAgeAboveGatewayRollBound` |
| m06 | `RequiresMountsFor=/datalog` → `RequiresMountsFor=/datalog/other` | `TestSealerUnitRequiresDatalogMount` |
| m07 | `ReadWritePaths=/datalog` → `ReadWritePaths=/` | `TestSealerUnitConfinesWritesToDatalog` |
| m08 | `ProtectSystem=strict` → `ProtectSystem=no` | `TestSealerUnitConfinesWritesToDatalog` |
| m09 | `MemoryMax=2G` → 删掉该行 | `TestSealerUnitLimitsMemoryCpuAndIO` |
| m10 | `SyslogIdentifier=trace-sealer` → `trace-sealer2` | `TestSealerUnitLogsToJournal` |
| m11 | 插入 `Environment=TRACE_BODY_SEAL_INTERVAL=15m` | `TestSealerUnitNeverUsesSealIntervalKey` |
| m12 | `--lock-file /datalog/trace-archive-v4/.sealer.lock` → `/tmp/sealer.lock` | `TestSealerUnitKnowsItsRootAndLockFile` |

`CHECKS`：unit 里**不得**出现 `ExecStartPre`（任何「启动前先建目录」的形态都会把消失的挂载重建到宿主根 FS 上）；unit 里不得出现 `TRACE_BODY_ROOT`（sealer 用 `--root`，环境变量是网关的接口）。

**`p4-task-5-isolate.py`**（`TARGET = "deploy/systemd/isolate-old-trace-archive.sh"`，`MANIFEST_FILES = ["tests/deploy/trace_isolate_script_test.go"]`）：m01 把 `[ "$CONFIRM" != "1" ]` 改成 `[ "$CONFIRM" = "1" ]`（默认真的动手）→ 杀手 `TestIsolateScriptDefaultIsDryRun`；m02 删掉删除谓词审查的 `exit 3` → 杀手 `TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot`；m03 `systemctl mask` → `systemctl unmask` → 杀手 `TestIsolateScriptStopsAndMasksTimer`；m04 删掉回滚提示 → 同上；m05（**跨文件，因此另起集合** `p4-task-5-restore.py`，`TARGET = "deploy/systemd/restore-old-trace-archive.sh"`）`unmask` → `mask` → 杀手 `TestRestoreScriptUnmasksAndEnables`。

**`p4-task-5-build.py`**（`TARGET = "Makefile"`，`MANIFEST_FILES = ["tests/deploy/trace_build_contract_test.go"]`）：m01 删掉 `GOOS=linux` → 杀手 `TestMakefileBuildsSealerForLinux`；m02 `build:` 依赖里去掉 `build-sealer` → 杀手 `TestMakefileBuildTargetsIncludeSealer`；m03 `./cmd/trace-sealer` → `./cmd/gateway` → 杀手 `TestMakefileBuildsSealerForLinux`；m04 **（跨文件，另起 `p4-task-5-ci.py`，`TARGET = ".gitlab-ci.yml"`）** 把 `install -m 0755 /tmp/trace-sealer.new /usr/local/bin/trace-sealer` 改成安装到 `/tmp`，或加一行 `systemctl enable --now trace-sealer` → 杀手 `TestCICIBuildsAndInstallsSealer` / `TestCICIDoesNotEnableSealerUnit`。

- [ ] **步骤 7：提交 → 跑五个集合**（unit / isolate / restore / build / ci —— 实为**五**个，与步骤 6 的标题一致）。**p4 集合总数核对**：任务 1（4）+ 任务 2（1）+ 任务 3（2）+ 任务 4（**2**）+ 任务 5（**5**）+ 任务 6（3）+ 任务 7（1）+ 任务 8（3）+ 任务 9（2）+ 任务 10（3）+ 任务 11（1）= **27**，与「文件结构」表里的 27 个 `p4-task-*.py` 一致；任务 12 的全量重跑必须覆盖这 27 个。（**E66⑥ 订正**：原写「任务 4（1）…= 26」—— **E66⑤** 只同步了任务 4 步骤 6/7 的集合**文本**，没同步这两处**计数**；本轮以「计划自己声明的集合名」为判据逐一点名复核：任务 1 `classify/exit/options/round`、任务 2 `alert`、任务 3 `loop/rootguard`、任务 4 `lock/runner`、任务 5 `unit/isolate/restore/build/ci`、任务 6 `blue/green/root`、任务 7 `bluegreen`、任务 8 `compose/guard/runsh`、任务 9 `budget/vmhwm`、任务 10 `contract/footer/observability`、任务 11 `runbook` = **27**；任务 5 收口时磁盘上实有 14 个 `p4-task-*.py`（任务 1–5），与算式的前五项 4+1+2+2+5 = 14 逐字一致。）

---

### Task 6：`/datalog` bind mount 与三个编排文件

**文件：**
- 修改：`deploy/blue-green/docker-compose.blue.yml`、`deploy/blue-green/docker-compose.green.yml`、`docker-compose.yml`
- 测试：`tests/deploy/trace_compose_mount_test.go`

**接口：** 交付物是三个 compose 的**挂载与 env 契约**：gateway 读写挂 `/datalog`；control-plane 只读挂；蓝/绿各自注入 `TRACE_BODY_INST`；根 compose **不启用**功能（**门禁口径 = 非注释生效行 = 0**，见 **E68①**）；`TRACE_BODY_ROOT` 一旦出现在某个文件里，它的父目录必须正好是该文件里的某个挂载目标。

- [ ] **步骤 1：写失败的测试**

`tests/deploy/trace_compose_mount_test.go`（自带一个按缩进切「服务块」的小工具函数，避免对整文件做宽松 grep）：

```go
package deploy_test

// serviceBlock 从 compose 文本里按缩进切出某个顶层服务的块。
// 【真实 service 键（已核实 deploy/blue-green/docker-compose.blue.yml:10,45）
// 与 docker-compose.green.yml 同构】两个蓝绿文件与根 docker-compose.yml 的顶层
// services 键**只有** `gateway` 与 `control-plane` —— **没有**任何把颜色缀进 service
// 键的形态（不存在 `-blue` / `-green` 后缀的 service 键；那类名字是本计划早期版本的
// 编造，已在本次订正中全部改成真实值）。颜色只体现在两处：
//   ① `container_name: llm-blue-gateway` / `llm-blue-control-plane`（green 同理）；
//   ② compose 项目名 `-p llm-blue` / `-p llm-green`（deploy-blue-green.sh:366）。
// 因此调用一律写成
//   serviceBlock(t, "deploy/blue-green/docker-compose.blue.yml", "gateway")
//   serviceBlock(t, "deploy/blue-green/docker-compose.blue.yml", "control-plane")
// 断言里若要指向容器，用 container_name（`llm-blue-gateway` 等），不要用 service 键拼颜色。
//
// 【为什么不用 YAML 库】引入 yaml 直接依赖会让 go.mod/go.sum 变动，
// 而本任务的被测对象只是「某服务下有没有这几行」；按缩进切块足够且更抗重排。
func serviceBlock(t *testing.T, file, service string) string { … }

// TestComposeGatewayMountsDatalogReadWrite 三个编排里 gateway 都必须读写挂载
// /datalog（写 spool 是功能开启的必要条件；与读侧路由无关 —— D69）。
// 三个文件各调一次 serviceBlock(t, <file>, "gateway") 后断言。
func TestComposeGatewayMountsDatalogReadWrite(t *testing.T) { … 对三个文件各断言 … }

// TestComposeControlPlaneMountsDatalogReadOnly 控制面点读：只读挂载即可
// （冻结包 reader.go 全程不写文件，已核 0 处 Create/OpenFile/Mkdir）。
// 服务键是 `control-plane`（**不是**把颜色缀进键名的那种编造形态），三个文件各断言一次。
func TestComposeControlPlaneMountsDatalogReadOnly(t *testing.T) { … read_only: true … }

// TestComposeBluePinsInstBlue 蓝文件的 inst 必须是 blue（颜色↔inst 一一对应；
// inst 绝不能来自全局 YAML，否则两个颜色会写进同一个 inst= 目录）。
func TestComposeBluePinsInstBlue(t *testing.T) { … TRACE_BODY_INST: blue，且不得出现 green … }

// TestComposeGreenPinsInstGreen 同上，green。
func TestComposeGreenPinsInstGreen(t *testing.T) { … }

// TestComposeColorFilesCarrySpecTraceBodyValues 蓝/绿的四个非开关参数必须与规格/
// 计划 2 的默认值**一致地显式写出**（C2：生产配置不靠代码默认值）。
func TestComposeColorFilesCarrySpecTraceBodyValues(t *testing.T) { … BLOCK_MB 1536 / FLUSH 1m / ROLL 1h / WATERMARK 90 … }

// TestComposeRootComposeDoesNotEnableTraceBody 根 compose 没有「颜色」概念：
// 一旦在那里设了 TRACE_BODY_ROOT，`docker compose up` 就会再造一个写者到
// inst 未定义的配置文件状态（TRACE_BODY_INST 缺失 ⇒ 启动直接失败）。
// 因此根 compose 只给挂载、不给开关 —— 也不给 inst。
func TestComposeRootComposeDoesNotEnableTraceBody(t *testing.T) { … }

// TestComposeTraceBodyRootParentIsMounted 承重的耦合：**任何**文件里若存在生效的
// TRACE_BODY_ROOT=<p>，则同文件里必须有 mount target == dirname(p)。
// 【这条是 D70 的机器可判定形态】root 的父目录没挂 ⇒ Docker 会在宿主根 FS 上
// 现建一个目录，正文就此写到根盘上（2026-09-24 事故的同一类）。
func TestComposeTraceBodyRootParentIsMounted(t *testing.T) { … }

// TestComposeNeverCarriesSealIntervalKey D64/D71：这个键名无效（网关侧叫
// TRACE_BODY_ROLL_INTERVAL），出现在编排里只会让人以为它有用。
func TestComposeNeverCarriesSealIntervalKey(t *testing.T) { … 三个文件都不得出现 TRACE_BODY_SEAL_INTERVAL … }
```

- [ ] **步骤 2：跑并确认失败** → `go test ./tests/deploy/ -run 'TestCompose' -v`：FAIL（**断言红**：三个文件里还没有挂载）。这是本任务的第一条真实失败证据。

- [ ] **步骤 3：改三个 compose**

`deploy/blue-green/docker-compose.blue.yml` 的 **`gateway`** 服务（真实 service 键就是 `gateway`，见该文件 `:10`；`green` 文件同构、`inst` 改成 `green`；容器名是 `llm-blue-gateway` / `llm-green-gateway`）：

```yaml
    environment:
      # ……原有变量不动……
      # ── 调用链正文直写（方案 C，计划 2/4）──
      # inst 必须与颜色一一对应，且**不能**放进 config/prod.yaml：全局 YAML 会让
      # 两个颜色写进同一个 inst= 目录（part 序号与 spool 会互相踩）。
      TRACE_BODY_INST: blue
      TRACE_BODY_BLOCK_MB: "1536"
      TRACE_BODY_FLUSH_INTERVAL: 1m
      TRACE_BODY_ROLL_INTERVAL: 1h
      TRACE_BODY_DISK_WATERMARK_PERCENT: "90"
      # 功能开关：取消注释即开启（前置条件见任务 7 的硬门：读侧就绪 + 挂载在场）。
      # TRACE_BODY_ROOT: /datalog/trace-archive-v4
    volumes:
      # 长语法（不是 `-v`）：source/target 显式，且**绝不**让 Docker 去创建宿主路径。
      # 部署前必须先 `findmnt -T /datalog` 证明挂载在场（见任务 6 步骤 5）。
      - type: bind
        source: /datalog
        target: /datalog
```

同一个 `deploy/blue-green/docker-compose.blue.yml` 里的 **`control-plane`** 服务（service 键见该文件 `:45`；容器名 `llm-blue-control-plane`）：

```yaml
    volumes:
      - type: bind
        source: /datalog
        target: /datalog
        read_only: true
```

`docker-compose.yml`（根编排）：给两个服务同样的挂载（gateway 读写、cp 只读），但**不加任何生效的** `TRACE_BODY_*`（连 `TRACE_BODY_INST` 也不加 —— 根编排没有颜色概念，加了反而暗示“根编排也能开功能”）。**门禁口径（E68①）**：真正的门禁是**非注释生效行 = 0**（语义测试 `TestComposeRootComposeDoesNotEnableTraceBody` 的 `effectiveAssignments` 只收非注释行），**不是** `grep -c TRACE_BODY docker-compose.yml`（注释里提到该键族前缀是允许的，文本 grep 只是辅助读数）；注释措辞统一用 `TRACE_BODY_` 前缀，**不写完整键名**。

- [ ] **步骤 4：跑测试** → `go test ./tests/deploy/ -run 'TestCompose' -count=1 -v` ⇒ PASS。

- [ ] **步骤 5：宿主机准备与「挂载就位、功能关闭」的分阶段上线（逐步 + 停止点）**

> 这是**生产机上的第一步改动**，顺序本身是安全设计：先证明挂载正确，再启用功能。中间任何一步失败都可以原地回退（这一步根本还没开功能）。

1. 抄录现状：`df -h /datalog`、`findmnt -T /datalog --output TARGET,SOURCE,FSTYPE`、`ls -la /datalog | head`。（2026-10-08 的实测值：`/datalog` 407G 已用 / 153G 可用；执行当天必须重取并抄录真实输出。）
2. ⛔ **停止点 1**：`findmnt -T /datalog` 的 `TARGET` 必须**正好**是 `/datalog`（不是 `/`）。若显示 `/` ⇒ 说明 `/datalog` 只是宿主根 FS 上的一个普通目录 ⇒ **停止**，先把盘/挂载修好。
3. 在**已确认的挂载内**建根目录：`install -d -m 0755 /datalog/trace-archive-v4`，然后 `findmnt -T /datalog/trace-archive-v4` 复核它落在同一挂载上。（**顺序不能反**：先证明挂载、再建目录；这一步是整个计划里唯一允许「创建归档根」的地方，因为它带人工确认。）
4. 抄录 inode/设备作为后续比对的基线：`stat -c '%d %i %n' /datalog/trace-archive-v4`。
5. 走**测试环境**先验一遍（`push test`，或按 `test-env-config` skill 的规范发布）：测试机上同样先 `findmnt -T /datalog`，然后确认 `deploy-app-test` 的 `--wait` 健康检查通过、`verify-test` 三项全过。此时功能**仍关闭**（`TRACE_BODY_ROOT` 是注释掉的）⇒ 行为应与改动前逐字节相同。
6. ⛔ **停止点 2**：测试环境确认「挂载在场 + 容器起来了 + 功能仍关闭」之后，才允许上生产。
7. 生产上线：按正常发版流程（CI 的 `scp -r deploy/blue-green/*` + `docker compose -f ${DEPLOY_DIR}/docker-compose.$COLOR.yml -p llm-$COLOR up -d --remove-orphans`，见 `deploy/blue-green/deploy-blue-green.sh:366`；落在**非活跃颜色**上）。抄录：`docker inspect` 里的 `Mounts`（必须有 `/datalog` 且 gateway 为 `RW=true`、control-plane 为 `RW=false`；容器名是 `llm-$COLOR-gateway` / `llm-$COLOR-control-plane`）、`docker exec llm-$COLOR-gateway ls -la /datalog/trace-archive-v4`。
8. ⛔ **停止点 3**：`docker inspect` 的挂载与 `RW` 与预期一致才继续；否则回退（=`docker compose ... up -d` 回上一个 commit 的编排）。
9. **回滚本步骤**：`git revert` 编排改动 → 重新发版（`up -d` 会重建容器，挂载随之消失）。**注**：即使挂载消失，功能仍关闭 ⇒ 无数据损失。

- [ ] **步骤 6：写变异集合（三个：blue / green / root）**

`p4-task-6-blue.py`（`TARGET = "deploy/blue-green/docker-compose.blue.yml"`）、`p4-task-6-green.py`（同 green 文件）、`p4-task-6-root.py`（`TARGET = "docker-compose.yml"`），每个的 `PACKAGES = ["./tests/deploy/"]`、`MANIFEST_FILES = ["tests/deploy/trace_compose_mount_test.go"]`。变异与杀手：

| 编号 | 变异（在哪个文件） | 杀手 |
|---|---|---|
| m01 | blue：`target: /datalog` → `target: /datalog2` | `TestComposeGatewayMountsDatalogReadWrite` |
| m02 | blue：cp 的 `read_only: true` 删掉 | `TestComposeControlPlaneMountsDatalogReadOnly` |
| m03 | blue：`TRACE_BODY_INST: blue` → `green` | `TestComposeBluePinsInstBlue` |
| m04 | blue：`TRACE_BODY_ROLL_INTERVAL: 1h` → `2h` | `TestComposeColorFilesCarrySpecTraceBodyValues` |
| m05 | green：`TRACE_BODY_INST: green` → `blue` | `TestComposeGreenPinsInstGreen` |
| m06 | root：插入生效的 `TRACE_BODY_ROOT: /datalog/trace-archive-v4` | `TestComposeRootComposeDoesNotEnableTraceBody` |
| m07 | blue：把 `TRACE_BODY_ROOT` 取消注释成 `source: /datalog-typo` 对应的路径（`TRACE_BODY_ROOT: /datalog-typo/trace-archive-v4`） | `TestComposeTraceBodyRootParentIsMounted` |
| m08 | root：删掉 cp 的挂载 | `TestComposeControlPlaneMountsDatalogReadOnly` |
| m09 | blue：插入 `TRACE_BODY_SEAL_INTERVAL: 15m` | `TestComposeNeverCarriesSealIntervalKey` |

`CHECKS`：三个文件里都不得出现 `-v /datalog` 或 `volumes: ... :/datalog` 的短语法形式（长语法是「不许 Docker 代建宿主路径」的可读证据链之一）。**另（E68①）**：根编排的「**非注释生效行 = 0**」在 `p4-task-6-root.py` 里补一行静态检查 —— 正则 `(?m)^\s*TRACE_BODY_[A-Z0-9_]+\s*:` **absent**（注释行以 `#` 起头、不匹配 ⇒ 这一行量的正是「生效赋值」；真正的门禁仍是语义测试，文本读数只是辅助）。

- [ ] **步骤 7：提交 → 跑三个集合**。

---

### Task 7：蓝绿切换与回滚剧本（`deploy/blue-green/README.md`）

**文件：**
- 新建：`deploy/blue-green/README.md`（上线/切换/回滚/降级剧本；**它随 `scp -r deploy/blue-green/*` 一起落到宿主**，因此运维在机器上就能读到）
- 测试：`tests/deploy/trace_bluegreen_runbook_test.go`

**接口：** 交付物是**剧本文本契约**：关键步骤（切换、回滚、功能降级、旧颜色处置、硬门）必须逐条出现在该文件里，且**不得**出现已被裁定的无效键。

> **为什么剧本单独一个文件而不是写进 `docs/deployment.md`**：① 它会随 `scp -r deploy/blue-green/*` 落到生产宿主，运维在机器上直接能读；② 与任务 11 的运行手册分开，两者的变异集合就不会互相干扰（同一文件被两个集合变异时，一个变异可能连带触发另一个集合的用例，报告会变脏）。

- [ ] **步骤 1：写失败的测试**

```go
package deploy_test

// TestBlueGreenRunbookCoversSwitch 切换剧本必须含：颜色↔inst 的静态对应说明、
// 「重启后新颜色接管 inst」、以及「切换期间两个 inst 并存 ⇒ 读侧必须用 body_ref_inst
// 而不是配置里的 inst」（跨计划契约 H2 的落点）。
// TestBlueGreenRunbookCoversRollback 回滚必须含：nginx 切回旧颜色、旧 inst 的数据
// 仍可读、**sealer 不随功能回滚而停止**（未封 spool 必须继续被封，否则已写出的
// body_ref 会永久停在 spool 上）。
// TestBlueGreenRunbookCoversFeatureOffByteIdentical 功能关闭时必须逐字节等价：
// body_ref_* 全 NULL + body_missing=false + 正文在 jsonb 列；给出可粘贴的 SQL 比对。
// TestBlueGreenRunbookHasReadSideHardGate 硬门：同一 root 的读侧未就绪前不得
// 取消注释 TRACE_BODY_ROOT（D73）。
// TestBlueGreenRunbookCoversOldColorDrain 旧颜色在 drain 期间功能仍是关闭的，
// 正文继续进 jsonb ⇒ 无数据损失（这是「两个 inst 并存也安全」的另一半）。
// TestBlueGreenRunbookNeverMentionsSealInterval D71 的无效键不得出现在剧本里。
```

- [ ] **步骤 2：跑并确认失败**（文件不存在 ⇒ 断言红）。

- [ ] **步骤 3：写剧本**。内容至少包含：

1. **颜色↔inst 的静态对应**：blue 文件 ⇒ `TRACE_BODY_INST=blue`；green ⇒ `green`。`deploy-blue-green.sh` **不需要任何改动**（它只管容器与 nginx）；这条以 CHECKS 钉住：`grep -c TRACE_BODY deploy/blue-green/deploy-blue-green.sh` 必须为 0。
2. **切换**：`deploy-blue-green.sh`（启动新颜色 → `/health`+`/ready` → nginx 原子切流 → drain 旧颜色 → 停旧颜色）。抄录切换前后的 `.active-color`。
3. **回滚**：`deploy-blue-green.sh --rollback`（nginx 切回旧颜色）。前提：**旧容器仍在**（`KEEP_OLD_ENV=true`）与**旧 inst 的数据仍在**（同一 root，不会被新颜色动）。不需要重建任何数据：读侧按 `body_ref_inst` 定位。
4. **功能降级（最轻的回滚）**：把 `TRACE_BODY_ROOT` 重新注释掉并重建容器 ⇒ 网关立刻回到「写 jsonb」模式；**已写出的 ref 不受影响**（它们指向的产物/spool 都还在），**sealer 必须继续运行**直到所有 spool 封完。
5. **硬门（⛔ 停止点）**：取消注释 `TRACE_BODY_ROOT` 之前必须同时满足 ① 同一 root 的读侧已可用（计划 3 落地并且在 nginx 后面的管理面能取到正文）；② `findmnt -T /datalog` 正常；③ `/datalog` 剩余空间 ≥ 5 天保留量（口径见任务 11 的运行手册）。
6. **旧颜色处置**：drain 结束后旧容器被停；下一次发版会把它按**新编排**重建（那时它也有挂载与 `TRACE_BODY_ROOT`）⇒ 两个颜色最终都按新路径写，`inst` 各自独立。
7. **禁止事项**：不得在两个颜色里设同一个 `inst`；不得把 `TRACE_BODY_*` 放进 `config/prod.yaml`；不得在功能开启后停止 sealer。

- [ ] **步骤 4：跑测试** ⇒ PASS。

- [ ] **步骤 5：写变异集合** `p4-task-7-bluegreen.py`（`TARGET = "deploy/blue-green/README.md"`，`MANIFEST_FILES = ["tests/deploy/trace_bluegreen_runbook_test.go"]`）：m01 删掉硬门段落 → `TestBlueGreenRunbookHasReadSideHardGate`；m02 把「sealer 必须继续运行」改成「sealer 可随功能一并停掉」→ `TestBlueGreenRunbookCoversRollback`；m03 删掉 SQL 比对 → `TestBlueGreenRunbookCoversFeatureOffByteIdentical`；m04 把 `body_ref_inst` 改成「配置里的 inst」→ `TestBlueGreenRunbookCoversSwitch`；m05 插入 `TRACE_BODY_SEAL_INTERVAL` → `TestBlueGreenRunbookNeverMentionsSealInterval`。`CHECKS`：`deploy/blue-green/deploy-blue-green.sh` 内 `TRACE_BODY` **absent**；**E69③（计划↔集合逐条对照）**：本集合的 `CHECKS` 共 **6 行** —— 上面这一行是本步骤明列的，另 **5 行**（剧本必须钉住颜色↔inst 静态对应、剧本必须含读侧硬门 D73、剧本必须含功能关闭的 SQL 比对、剧本必须含旧颜色 drain 无损、剧本必须含禁止事项）是按 **E62** 追加的**存在性**静态检查（静态检查只承载**存在 / 不存在**类要求）；它们的**可失败性**由交付报告的「改坏→转红→还原」探针逐条实测（输出落盘 `p4-task-7-bluegreen-probe-at-*.txt`），集合 docstring 同文登记。

- [ ] **步骤 6：提交 → 跑集合**。

---

### Task 8：A3 真实盘满（安全做法）与进程级 A7

**文件：**
- 新建：`internal/traceverify/guard.go`（破坏性操作的目标护栏）、`internal/traceverify/vmhwm.go`（`VmHWM` 解析）
- 新建：`cmd/trace-verify/main.go`（`scratch-prepare` / `scratch-cleanup` / `decode-bench`）
- 新建：`deploy/verify/trace-a3/docker-compose.yml`、`deploy/verify/trace-a3/run.sh`
- 测试：`tests/traceverify/guard_test.go`、`tests/traceverify/vmhwm_test.go`、`tests/deploy/trace_a3_compose_test.go`

**接口：**
```go
// internal/traceverify/guard.go
// GuardScratchTarget 拒绝任何「落在文件系统根」或「与 forbidden 之一同一个设备」的目标，
// 并要求调用方显式确认（confirm）。stat 可注入，便于用例造设备号。
func GuardScratchTarget(target string, confirm bool, stat func(string) (dev uint64, err error), forbidden ...string) error
var ErrScratchTargetUnsafe = errors.New("traceverify: 破坏性操作的目标不安全")

// internal/traceverify/vmhwm.go
// ParseVmHWM 从 /proc/<pid>/status 的文本里取 VmHWM（峰值常驻内存，单位 bytes）。
func ParseVmHWM(statusText string) (int64, error)
// PeakVmHWM 轮询若干进程的 VmHWM 并返回峰值（采样间隔 50ms，采样期间由调用方决定）。
func PeakVmHWM(pids []int, samples int, interval time.Duration) (int64, error)
```

- [ ] **步骤 1：先写「不许把自己盘填满」的护栏测试**

```go
package traceverify_test

// TestGuardRejectsFilesystemRoot 目标就是 `/` ⇒ 拒。
// TestGuardRejectsSameDeviceAsForbidden 目标与 `/datalog`（或 `/`）同设备 ⇒ 拒。
//   这是 D66 的机器可判定形态：A3 要「真的把盘写满」，唯一可接受的目标是一块
//   **专用**文件系统（回环 ext4）。若它与 / 或 /datalog 同设备，本命令会拒绝执行。
// TestGuardRequiresExplicitConfirm 没有 confirm ⇒ 拒（连准备动作都不许做）。
// TestGuardAcceptsDedicatedFilesystem 专用设备 + confirm ⇒ 通过。
// TestGuardRejectsTargetOnSameDeviceAsForbiddenSelf 若干 forbidden 里包含目标自己 ⇒ 拒。
//
// 【调用时机也是契约】GuardScratchTarget 必须由 scratch-prepare 在 `mount -o loop`
//   **成功之后**、任何真正破坏性动作（往回环 FS 里填盘 / scratch-cleanup）**之前**调用：
//   挂载前 `/srv/trace-a3` 是宿主根 FS 上的目录（与 `/` 同设备）⇒ 校验必然误拒。
//   该次序由 run.sh 的顺序断言（TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep）钉住。
```

- [ ] **步骤 2：跑并确认失败** → 编译红；实现后 PASS。

- [ ] **步骤 3：实现护栏 + `cmd/trace-verify` 的三个子命令**

`scratch-prepare --image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G --confirm`
依次做：容量预检（`statfs` 断言镜像所在文件系统可用空间 ≥ `size` + 1 GiB 余量；见风险登记 R2）→ `fallocate`（或 `dd`）建镜像 → `mkfs.ext4 -F -m 0` → `install -d` 挂载点（**挂载点在宿主根 FS 上，是允许的**：它不承载数据）→ `mount -o loop` → **挂载成功之后**才调 `GuardScratchTarget` 复核「目标设备与 `/`、`/datalog` **不同设备**」→ **在回环 FS 内**建归档根 `install -d -m 0755 /srv/trace-a3/trace-archive-v4` → 打印 `df -h` 与 `findmnt`。

> ⛔ **护栏必须在 `mount` 之后（原先的次序是错的，A3 会必然失败）**：`GuardScratchTarget` 的判据是「目标与 `/`、`/datalog` **不同设备**」。在 loop 挂载**之前**，`/srv/trace-a3` 要么还不存在、要么就是宿主根 FS 上的一个普通目录（与 `/` 同设备）⇒ 护栏**必然拒绝**，A3 一步都跑不了。次序改为「先建镜像并 `mount -o loop`，再校验」，护栏看到的才是**回环 ext4 的设备号** —— 这正是「只允许在测试环境专用回环 ext4 上做真实 ENOSPC」这条安全边界的机器可判定形态，安全边界本身**不变**。`GuardScratchTarget` 依旧带 `--confirm`，`scratch-cleanup` 依旧是唯一允许删镜像/目录的入口。
> ⚠️ 镜像文件本身（`/srv/trace-a3.img`，约 1 GiB）落在 `/srv` 所在文件系统，**不受**该护栏保护（它只护挂载点）⇒ 容量预检与设备检查见**风险登记 R2**，必须在同一步做掉。

> ⚠️ **归档根必须显式创建，且只能在挂载之后创建**：网关的 `NewTraceSpooler` 在**构造期**就要求 root 存在（`internal/gateway/observability/spooler.go:144` 的 `rootExists`），而 sealer 与网关都**不会**替它创建（D70）。`run.sh` 原先的步骤里没有这一步 ⇒ A3 的 `docker compose up -d --wait` 会直接以「正文根目录 … 不可用（未挂载？）」失败。因此 `scratch-prepare`（或 `run.sh` 的第 1 步之后、第 2 步之前）必须执行 `install -d -m 0755 /srv/trace-a3/trace-archive-v4`；它映射到容器里的 `TRACE_BODY_ROOT=/datalog-a3/trace-archive-v4`。在挂载之前建这个目录会在宿主根 FS 上造出一个同名的空目录（D70 的同一类事故），所以次序不可反。

`scratch-cleanup --image … --mount … --confirm`：`umount` → `losetup -d`（若仍挂着）→ 删镜像与目录。

`decode-bench --artifact <path> --concurrency 1,2,3 --json`（任务 9 用）。

- [ ] **步骤 4：写 A3 的隔离实例编排与它的契约测试**

`deploy/verify/trace-a3/docker-compose.yml`：

```yaml
# 仅用于 A3（真实 ENOSPC）与 A7（进程级并发）的隔离验证实例。
# ⛔ 不得用于生产：
#   · 它挂的是回环文件系统 /srv/trace-a3（与 /datalog 不是同一个设备）
#   · 它复用测试环境的 Redis/PG（刻意省掉造配置的成本）⇒ **只能在测试环境跑**
#   · 端口 8090（gateway）与活跃颜色（8080/8082）错开；本实例**只跑 gateway**
#     （复用测试环境已有的控制面/Redis/PG，刻意不再定义 cp 服务 ⇒ 不存在 8091）
services:
  gateway-a3:
    image: ${REGISTRY}/llm-gateway:${IMAGE_TAG:?IMAGE_TAG required}
    container_name: llm-a3-gateway
    restart: "no"
    network_mode: host
    environment:
      APP_ENV: test
      GATEWAY_PORT: "8090"
      # 连接串一律来自环境（无默认值）：写错就没有默认值可以掩盖，
      # 也保证「不可能不小心连到生产」这件事是靠结构而不是靠自觉。
      DATABASE_URL: ${A3_DATABASE_URL:?A3_DATABASE_URL required（必须是测试库）}
      REDIS_ADDR: ${A3_REDIS_ADDR:?A3_REDIS_ADDR required}
      REDIS_PASSWORD: ${A3_REDIS_PASSWORD:-}
      REDIS_DB: ${A3_REDIS_DB:?A3_REDIS_DB required（必须选一个测试环境没人用的库号）}
      # ── 正文直写：故意把水位压到 99，让**真实 ENOSPC** 先于水位闩锁发生 ──
      TRACE_BODY_ROOT: /datalog-a3/trace-archive-v4
      TRACE_BODY_INST: blue
      TRACE_BODY_BLOCK_MB: "64"
      TRACE_BODY_FLUSH_INTERVAL: 5s
      TRACE_BODY_ROLL_INTERVAL: 10s
      TRACE_BODY_DISK_WATERMARK_PERCENT: "99"
    volumes:
      - type: bind
        source: /srv/trace-a3
        target: /datalog-a3
```

`deploy/verify/trace-a3/run.sh`（**每步都打印命令与输出；破坏性步骤要 `--confirm`；`trap` 里做清理**）流程：

1. `go run ./cmd/trace-verify scratch-prepare --image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G --confirm`（容量预检 → 建镜像 → `mkfs.ext4` → `mount -o loop` → **挂载之后**才做 `GuardScratchTarget` 设备号校验 → 在回环 FS 内建归档根；见步骤 3 的两条注记）。
2. 复核归档根确实落在回环 FS 上：`findmnt -T /srv/trace-a3/trace-archive-v4 --output TARGET,SOURCE,FSTYPE`（`TARGET` 必须是 `/srv/trace-a3`，**不是** `/`）与 `stat -c '%d %n' /srv/trace-a3/trace-archive-v4`（设备号与 `/`、`/datalog` 都不同）。**缺这一步 A3 起不来** —— `NewTraceSpooler` 构造期就要求该目录存在（`spooler.go:144` 的 `rootExists`）。
3. `docker compose -f deploy/verify/trace-a3/docker-compose.yml up -d --wait`。
4. 记录基线：`/health`、`/ready`、`stat` 的 `/datalog-a3/trace-archive-v4` 设备号。
5. 造流量（**用会失败的请求**，让 trace 事件照样产生）：把上游指到一个必然拒连的端口（例如临时加一个 `base_url: http://127.0.0.1:9` 的 channel/model），用有效 key 打 `POST /v1/chat/completions`；并发 `xargs -P8` 打 2000 次。
6. 观察闩锁：`docker logs llm-a3-gateway | grep '\[trace-body\] ERROR'`（期望一条**只报一次**的 ERROR，reason 是写失败一类）；`/health`、`/ready` 仍然 200；`curl` 的 P99 与第 5 步之前对比（本脚本打印直方图，**不做自动判定** —— 判定由人看数字，见交付报告）。
7. 观察降级：在 scratch PG 上 `SELECT count(*) FROM call_traces WHERE body_missing = true AND body_ref_part IS NULL` 与总数对比。
8. 观察 spool 保留：`ls -la /srv/trace-a3/trace-archive-v4/spool/dt=*/inst=blue/ | head`。
9. 释放空间并观察恢复：`sudo rm -f /srv/trace-a3/fill.bin` → 等 1–2 个 tick → 断言新 spool 出现、闩锁日志出现「已恢复」。
10. **A7 进程级**：让 sealer 对同一 root 跑一次 `--once`，同时另一个 shell 里再跑一次 `--once`，断言第二次以退出码 1 失败且 stderr 含「已有另一个 sealer 持有锁」；然后只留一个 sealer 在跑，同时用网关持续写入，断言最终所有 ref 都能在读侧找到落点（读侧未就绪时用第 9 步的文件系统断言替代）。
11. 清理（`trap` 保证即使中途失败也清理）：`docker compose down` → `scratch-cleanup --confirm`。

> 第 5 步「填满」要真正触发 ENOSPC：先 `fallocate -l 980M /srv/trace-a3/fill.bin` 把 1 GiB 的文件系统占到只剩几十 MB，再靠写入把最后的空间吃掉。**这一整套只允许在 `/srv/trace-a3` 上执行**（镜像与挂载点都在 `/srv` 下，镜像本身的容量边界见风险登记 R2），护栏会拒绝任何与 `/` 或 `/datalog` 同设备的目标（注意：护栏校验发生在 `mount -o loop` **之后**，此时 `/srv/trace-a3` 的设备号已经是回环 ext4）。

**本步骤还必须真正创建 `tests/deploy/trace_a3_compose_test.go`**（步骤 6 的 `p4-task-8-compose.py` 的 `MANIFEST_FILES` 指的就是它 —— **清单文件不存在 = 空清单 = 无效证据**）。它是只读文件文本的 B 层用例（`package deploy_test`，用第一个 `serviceBlock` 式的缩进切块工具，不引 YAML 直接依赖），至少包含步骤 6 表格里出现的四个顶层用例：`TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst`、`TestA3ComposeHasNoConnectionDefaults`、`TestA3ComposeUsesIsolatedNamesAndPorts`、`TestA3ComposeNeverMountsProductionDatalog`。写完后 `go test ./tests/deploy/ -run 'TestA3Compose' -count=1 -v` 必须全绿（此时四个用例都已能通过，它们是**契约钉住**而不是 TDD 红绿：被钉的是上面这段 compose 文本）。**验收方式**：`grep -c '^func Test' tests/deploy/trace_a3_compose_test.go` 必须 ≥4，且与 `p4-task-8-compose.py` 的 `MANIFEST_FILES` 一致。

- [ ] **步骤 5：抄录 A3 的证据**（报告里必须逐条列出）：填盘前后的 `df -h /srv/trace-a3`；闩锁 ERROR 原文（**只出现一次**）；`body_missing` 计数；`/health`、`/ready` 的返回码；延迟直方图前后对比；恢复后的新 spool 列表；`scratch-cleanup` 之后 `findmnt /srv/trace-a3` 为空、镜像已删。

- [ ] **步骤 6：写变异集合（三个：guard / a3-compose / runsh）**

**`p4-task-8-guard.py`**（`TARGET = "internal/traceverify/guard.go"`，`MANIFEST_FILES = ["tests/traceverify/guard_test.go"]`）：m01 去掉「目标是 `/`」的判断 → `TestGuardRejectsFilesystemRoot`；m02 去掉设备号比较 → `TestGuardRejectsSameDeviceAsForbidden`；m03 去掉 `confirm` 检查 → `TestGuardRequiresExplicitConfirm`；m04 设备号比较用 `!=` 反向 → `TestGuardAcceptsDedicatedFilesystem`。

**`p4-task-8-compose.py`**（`TARGET = "deploy/verify/trace-a3/docker-compose.yml"`，`MANIFEST_FILES = ["tests/deploy/trace_a3_compose_test.go"]`）：m01 `TRACE_BODY_DISK_WATERMARK_PERCENT` 改成 `90`（真实 ENOSPC 会被水位抢在前面 ⇒ A3 的语义被偷换）→ `TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst`；m02 把 `A3_DATABASE_URL` 的必填改成给一个默认值 → `TestA3ComposeHasNoConnectionDefaults`；m03 `container_name` 改成 `llm-gw-control-plane`（撞名会让 `docker compose down` 误伤生产组件）→ `TestA3ComposeUsesIsolatedNamesAndPorts`；m04 把 `source: /srv/trace-a3` 改成 `/datalog` → `TestA3ComposeNeverMountsProductionDatalog`。

**`p4-task-8-runsh.py`**（`TARGET = "deploy/verify/trace-a3/run.sh"`）：m01 删掉 `trap` 清理 → `TestA3RunScriptAlwaysCleansUp`；m02 删掉护栏调用 → `TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep`；m03 把 `--confirm` 从 `scratch-prepare` 上去掉 → 同上（护栏调用行必须带 `--confirm`）。**注**：用例名已从 `…BeforeAnyDestructiveStep` 改为 `…BeforeAnyFillOrCleanupStep`（原名会被误读成「护栏在任何破坏性动作之前」，而 `mount -o loop` 也是破坏性动作之一）—— 它的实际守卫范围是**填盘与 cleanup 之前**，**不**包含 `mount -o loop`：护栏按步骤 3 的次序必须在挂载**之后**调用（挂载前校验必然误拒），因此 run.sh 的顺序断言应当是「`scratch-prepare`（含其内部的挂载后校验）→ 复核归档根 → `up -d` → 填盘」，并在用例注释里写清这条边界。

---

### Task 9：A6 压缩比复测与 C5 解码内存预算

**文件：**
- 新建：`internal/traceverify/budget.go`
- 修改：`cmd/trace-verify/main.go`（`decode-bench` 子命令）
- 测试：`tests/traceverify/budget_test.go`、`tests/traceverify/vmhwm_test.go`

**接口：**
```go
// internal/traceverify/budget.go
const (
    // C5 预算：控制面容器的内存上限。取**蓝绿生产编排**的值（D72）：
    // deploy/blue-green/docker-compose.{blue,green}.yml 两个文件都是 memory: 2g。
    ControlPlaneLimitBytes = 2 << 30
    // 解码窗口 = 冻结包 defaultZstdWindowBytes（512 MiB）：点读必须把整个窗口载入。
    DecodeWindowBytes = 512 << 20
    // 单次点读的固定缓冲：冻结包 reader.go 里 tb 的 64 MiB 上界。
    PerReadFixedBytes = 64 << 20
    // 索引上界：8 B × maxTrailerEntries = 64 MiB。
    MaxIndexBytes = 64 << 20
)
func PerReadPeakBytes() int64
func MaxConcurrent(limitBytes, baselineBytes, safetyBytes, perReadBytes int64) int
```

- [ ] **步骤 1：写失败的测试**

```go
// TestPerReadPeakMatchesSpecBudget 640 MiB = 512（窗口）+ 64（索引）+ 64（tb）。
//   killer：把任一项改错，或把加法改成乘法。
// TestMaxConcurrentMatchesControlPlaneBudget 2 GiB 限额、基线 300 MiB、安全垫 256 MiB
//   ⇒ (2048-300-256)/640 = 2.33 ⇒ **2**。killer：floor 改 ceil（会算成 3 ⇒ 超额）；
//   或把安全垫减掉改成不减（会算成 2.73 ⇒ 2，看起来一样 —— 所以要同时钉「减法存在」
//   这一条用另一条用例：把一个刚好卡边的输入喂进去）。
// TestMaxConcurrentIsAtLeastOne 算出来 <1 时返回 1，并明确「1 是下限，不是安全保证」；
//   这条保证调用方（计划 3 的读侧）永远拿到一个可用上限，而不是 0 导致功能全关。
// TestMaxConcurrentRejectsNonPositivePerRead perRead <= 0 ⇒ 返回 1（不许除零/负值，
//   也不许 panic）。
```

- [ ] **步骤 2：跑、实现、跑到绿。**

- [ ] **步骤 3：实现 `decode-bench`（真实解码内存的来源）**

`cmd/trace-verify/main.go` 的 `decode-bench --artifact <单个 .jsonl.zst> --concurrency 1,2,3 --json`：对每个并发度 K，起 K 个 goroutine 各做 50 次随机点读（`tracearchive.NewReader` + `GetBody`），采样 `VmHWM`（本进程 + 自身子进程）求峰值，输出 `{"concurrency":K,"peak_vmhwm_bytes":N,"points":50}`。它**只做测量与打印**，判定留给报告 —— 因为「K 是否安全」的判据来自 `internal/traceverify`，而 `budget_test.go` 已经把那个算术钉住了。

- [ ] **步骤 4：A6 的两次真实测量（**这是本任务的核心交付**）**

1. **压缩比（生产宿主，v1.4.8）**：把测试环境 A3 产出的一棵**真实 spool/产物树**复制到有 `/usr/bin/zstd` v1.4.8 的机器上（生产宿主 `/usr/bin/zstd` 就是 v1.4.8；**不要**在生产上造流量，只复制文件）。让 spool 变「合格」用 `touch -d '5 hours ago'`，**不要**改 `--min-age`（那是 R8 的承重参数）。然后：`trace-sealer --root <copy> --zstd-binary /usr/bin/zstd --expected-zstd-version 1.4.8 --min-age 3h --once`，抄录它打出的每个 part 的 `rows/uncompressed_bytes/artifact_bytes/ratio`。
   - 对照基线：`go test ./internal/tracearchive/ -run TestGoEncoderRatioRegression -v`（计划 1 的 ≥50× 回归门）+ 规格 §5.3.3 / 计划 1 标定的 **10.26×**。
   - **方法必须复用计划 1 的标定做法**（`internal/tracearchive/calib_test.go`：同一份代表性正文语料、同一条 `-19 --long=29 -T1` 参数路径），且**只使用 C 侧与 Go 侧都支持的参数**——`-19` / `--long=29`（512 MiB）/ `-T1` 是两侧都有的；任何只有一侧支持的开关（例如某些版本的 `--ultra` 组合、`--no-check`、`--adapt`）**不得**引入，否则「两侧同参数」这个前提就断了，标定值也不能互相对照。
   - **验收门槛必须逐字取自规格 §16 与 `internal/tracearchive/calib_test.go` 的注释**（本计划不替它定门槛，只要求：执行者把原文抄进报告，并在同一张表里给出实测值）。
2. **解码内存（C5）**：`go run ./cmd/trace-verify decode-bench --artifact <真实产物> --concurrency 1,2,3 --json`，抄录三个 K 的 `peak_vmhwm_bytes`，与 `traceverify.PerReadPeakBytes()` / `MaxConcurrent(2GiB, baseline, safety, …)` 的结果并列。基线取实测的空转 RSS（同一进程跑 K=0 的空读）。
   - **640 MiB/并发的来源必须引用冻结包自己的推导**（`internal/tracearchive/reader.go:30-62` 的注释）：条目数上界把「trailer 字节 + 解码索引」压到 ≤128 MiB，叠加 512 MiB 解码窗口 ⇒ ≈640 MiB/并发，回到 C5 的 600 MB 量级；正是这个上界（`maxTrailerEntries`）把更早的实测病态值挡在门外。
   - ⚠️ **必须复测「连续多轮不做 GC」的形态**：同一个注释里登记了**未加上界之前**的实测——单次病态点读峰值堆 1090 MiB、稳态 2 并发 1666 MiB、**连续多轮不做 GC 时 3268 MiB**（已超 2 GiB）。加上界之后这些数字**理论上**不再出现，但这是**未复测的推定**。因此 `decode-bench` 必须做到：连续跑 ≥5 轮（不是每轮新起进程），用 `GODEBUG=gctrace=1` 观察是否发生 GC，并**同时**报告「有 GC」与「禁掉 GC（`GOGC=off`）」两种模式下的峰值。
   - ⛔ **停止点**：若 `GOGC=off` 下 2 并发的峰值 ≥ 2 GiB ⇒ **不得**宣称 2 并发安全：`MaxConcurrent` 的结论取 **1**，并把「计划 3 的 `TRACE_BODY_READ_MAX_CONCURRENT` 默认值必须从 2 改成 1（或控制面限额提高）」写进报告与交接 H5。
3. ⛔ **停止点**：若实测 K=2 的峰值超过 **2 GiB**（C5），**不得**宣称 A6 通过：要么把 `MaxConcurrent` 的返回值降到实测支持的 K，要么在运行手册里登记「控制面内存限额必须提高」，二选一并写进报告。

- [ ] **步骤 5：写变异集合**

**`p4-task-9-budget.py`**（`TARGET = "internal/traceverify/budget.go"`，`MANIFEST_FILES = ["tests/traceverify/budget_test.go"]`）：m01 `PerReadPeakBytes` 少加一项（`DecodeWindowBytes + MaxIndexBytes`）→ `TestPerReadPeakMatchesSpecBudget`；m02 `MaxConcurrent` 改成向上取整 → `TestMaxConcurrentMatchesControlPlaneBudget`；m03 下限 `if n < 1 { n = 1 }` 删掉 → `TestMaxConcurrentIsAtLeastOne`；m04 `perReadBytes <= 0` 的兜底删掉 → `TestMaxConcurrentRejectsNonPositivePerRead`。

**`p4-task-9-vmhwm.py`**（`TARGET = "internal/traceverify/vmhwm.go"`，`MANIFEST_FILES = ["tests/traceverify/vmhwm_test.go"]`）：m01 取成 `VmPeak` 而不是 `VmHWM` → `TestParseVmHWMReadsPeakResidentNotVirtual`；m02 单位换算错（kB → B 少乘 1024）→ `TestParseVmHWMConvertsKilobytesToBytes`；m03 解析失败时返回 0 而不是错误 → `TestParseVmHWMErrorsOnMissingField`。

- [ ] **步骤 6：提交 → 跑集合 → 报告里附 A6 的两次实测输出。**

---

### Task 10：A8 行数分布、`maxTrailerEntries` 契约钉住与 footer 只读解析

**文件：**
- 新建：`internal/tracesealer/footer.go`
- 修改：`internal/tracesealer/runner.go`（`observeSealedPart` 的真实实现：填 `MaxRows` / `MinBytesPerRow` / `MinRatio`，并在 50% 阈值处 WARN）
- 修改：`internal/tracesealer/options.go`（`MaxTrailerEntries` 的注释订正：任务 10 的 `observeSealedPart` 已使用它 —— 3 行注释，行为零改动；**E73③ 补登记**：原清单漏了这一项，而它在本轮的 TARGET 变更面里）
- 测试：`tests/tracesealer/footer_test.go`、`tests/tracesealer/constants_contract_test.go`、`tests/tracesealer/observability_test.go`、`tests/tracesealer/footer_artifact_test.go`（**E73① 补登记**：在**已提交的真实产物**上解析，**不依赖 zstd**；该文件进 `p4-task-10-footer` 的 `MANIFEST_FILES`，使「冻结包真按此布局写」这条行为要求进入自动门禁）
- 测试数据：`tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst`（**E73① 补登记**：**冻结 Sealer 预产**的 104 字节真实产物，sha256 = `7f4291c592b71182a90bfa25953c648b3db5fd64383fc85af14752430e2af75e`）

**接口：**
```go
// internal/tracesealer/footer.go
// RowCount 只读解析产物末尾的固定长度 footer，返回其 row_count。
// ⚠️ 冻结包的 footer 编码是未导出的；本函数是**镜像实现**，因此必须被
//   tests/tracesealer/constants_contract_test.go 与一个真实产物往返测双重钉住。
func RowCount(artifactPath string) (int, error)
const FooterSize = 32 // 必须等于冻结包 footer.go 的 footerSize
```

- [ ] **步骤 1：写失败的测试（契约钉住 + 解析）**

```go
// TestSealerMaxTrailerEntriesMatchesFrozenReader 现读 `internal/tracearchive/reader.go`，
// 正则取出 `maxTrailerBytes = 64 << 20`（:28）与 `maxTrailerEntries = maxTrailerBytes / 8`（:62），
// 断言 tracesealer.MaxTrailerEntries 与它相等。
// 【为什么必须这样钉】它是**未导出**常量，本包只能复制；漂移之后 A8 的
// 「行数不超上界」判断会静默失真（不会有人发现，因为判断本身还在跑）。
// TestSealerGatewayRollBoundMatchesConfig 钉住 GatewayMaxRollInterval == 1h
//   == `internal/gateway/config/trace_body.go:24` 的 `traceBodyMaxRollInterval`。
// TestSealerDefaultMinAgeMatchesFrozenDefault 钉住三处 3h 必须一致：
//   tracesealer.DefaultMinAge == `internal/tracearchive/sealer.go:56` 的 defaultSealMinAge
//   == `internal/gateway/config/trace_body.go:43` 的 traceBodyMinAge（规格 §7.2 的镜像）。
//   【注意】网关侧那条判据在现有校验次序下不可达（它自己的注释已登记），所以本用例钉的是
//   **常量一致性**，不得声称「网关的那条检查承重」。
// TestFooterRowCountOnHandBuiltFooter 用按冻结包 footer 布局手工拼出的 32 字节
//   断言解析结果（纯函数证据，无需 zstd）。
// TestFooterRejectsShortFile 文件不足 32 字节 ⇒ 报错（不是返回 0）。
```

（**真实产物往返**：`TestFooterRoundTripOnRealArtifact` 用冻结 `Sealer` 真封一块再解析 —— 它需要本机有 zstd，因此**故意不进任何 MANIFEST_FILES**，只作为人工复核项：`go test ./tests/tracesealer/ -run TestFooterRoundTripOnRealArtifact -v`。若本机没有 zstd，运行时证据改由任务 8 的 A3 环境补（那里有真实产物），并在报告里如实标注。）

- [ ] **步骤 2：跑并确认失败**（编译红）→ 实现 `footer.go`（**先读 `internal/tracearchive/footer.go` 确认字段次序与编码字节序，再写**；`FooterSize=32` 只作为镜像常量，真值以冻结包为准）。

- [ ] **步骤 3：`observeSealedPart` 的真实实现**

每个成功封出的 part：`rows, err := RowCount(partPath)`；`bytesPerRow = task.UncompressedBytes / rows`；`ratio = float64(task.UncompressedBytes) / float64(artifactSize)`；更新 `MaxRows` / `MinBytesPerRow` / `MinRatio`；对 `rows > MaxTrailerEntries/2` 打 WARN（`rows`、`dt/inst/part`、`上界的百分比`）。每轮的汇总日志里带上这三个量（这是 A8 与 A6 的**持续**观测面：每个 part 一行 INFO，每轮一行汇总）。

- [ ] **步骤 4：A8 的行数分布测量与判定**

1. 采集：在 A3 环境（任务 8）或测试环境开启写路径后，跑一段真实流量，然后 `grep 'sealed part' <journal/日志>` 抽出所有 `rows`，算分布（min/p50/p95/max）与 `bytes_per_row` 的分布。
2. 判定门槛：`max(rows) < MaxTrailerEntries`（8,388,608）。理论可达性：`rows ≈ BlockMB × 1 MiB / 平均行字节`，因此只有当平均行 < `BlockMB × 1MiB / 8388608 = 192 B`（BlockMB=1536）时才可能越界。
3. ⛔ **决策门**：
   - 若实测 `max(rows)` 距上界 **> 2 倍余量**（即 < 4,194,304）⇒ 记「余量充足」，**不改**计划 2 的代码。
   - 若实测落在 **50%–100%** 之间 ⇒ 在运行手册里登记「需要按行数滚动」的后续项，并给出建议参数（`TRACE_BODY_BLOCK_MB` 下调到 X 使 rows < 上界/2）。
   - 若实测 **≥ 上界**（即真的出现 `ErrTooManyRows`）⇒ 必须在 `internal/gateway/observability/spooler.go` 增加**按行数滚动**（`Ref.Row` 已经带了行号，可实现），并在同一提交里重跑 `p2-task-5` / `p2-task-8-spooler` 两个集合、重新记录 sha256（全局约束里已经写明这条代价）。**此时不得**只调大 `BLOCK_MB` 了事（那只是把问题推远）。
4. 告警面：确认 `too_many_rows` 会以 `permanent=true` 语义出现在日志与轮次汇总里（Task 2 的 `SeverityError` + `reason=too_many_rows`），并抄一条**人工构造**的样本进报告（用 A3 环境把 `TRACE_BODY_BLOCK_MB` 调到极小 + 平均行做到 <192 B 来复现；**不允许**在生产上复现）。

- [ ] **步骤 5：写变异集合（两个）**

**`p4-task-10-footer.py`**（`TARGET = "internal/tracesealer/footer.go"`）：m01 `FooterSize` 32 → 24 → `TestFooterRowCountOnHandBuiltFooter`；m02 读取偏移改一个字节 → 同上；m03 短文件返回 0 而不是错误 → `TestFooterRejectsShortFile`。

**`p4-task-10-contract.py`**（`TARGET = "internal/tracesealer/options.go"`，`MANIFEST_FILES = ["tests/tracesealer/constants_contract_test.go"]`）：m01 `MaxTrailerEntries` 改成 `8_388_607` → `TestSealerMaxTrailerEntriesMatchesFrozenReader`；m02 `GatewayMaxRollInterval` 改成 `2 * time.Hour` → `TestSealerGatewayRollBoundMatchesConfig`；m03 `DefaultMinAge` 改成 `2 * time.Hour` → `TestSealerDefaultMinAgeMatchesFrozenDefault`。

**`p4-task-10-observability.py`**（`TARGET = "internal/tracesealer/runner.go"`，`MANIFEST_FILES = ["tests/tracesealer/observability_test.go"]`）：m01 WARN 阈值从 50% 改成 100%（等于没有预警）→ `TestObserveWarnsAtHalfOfRowCap`；m02 `MinBytesPerRow` 用 `>` 而不是 `<` 更新（变成最大值）→ `TestObserveTracksMinimumBytesPerRow`；m03 `bytesPerRow` 分母用 `rows+1` → `TestObserveTracksMinimumBytesPerRow`。

- [ ] **步骤 6：提交 → 跑三个集合。**

---

### Task 11：撤掉挂载后 ≤1 tick 闩锁（生产验证）与运行手册

**文件：**
- 修改：`docs/deployment.md`（新增「调用链正文（/datalog）运行手册」一节）
- 测试：`tests/deploy/trace_runbook_test.go`

> ⛔ **前置条件（本任务唯一的硬前提前置，不可省）**：本任务验证的「撤掉挂载 ≤1 tick 闩锁」只在**写侧已启用**时存在。
> - 需要的状态：`TRACE_BODY_ROOT` **非空**（即 compose 里那行已取消注释）**且**该 root 目录在场 —— `NewTraceSpooler` 在构造期就用 `rootExists` 要求目录存在（`internal/gateway/observability/spooler.go:144`）。
> - 功能关闭时的实情：`cmd/gateway/main.go:275` 的 `sink` 是 **nil**，`TraceSpooler` **根本不构造** ⇒ 根身份护栏（`checkRoot`）从不运行 ⇒ 期望的 `root_unavailable` / `root-identity-mismatch` ERROR **永不产生**，本任务的所有观察断言**恒真（空过）**。把这一项排在写侧启用之前，就是一条**永远绿、什么都没证明**的验收。
> - **与 D73 硬门的先后关系（如实写明）**：D73 规定「同一 root 的读侧可用之前不得设置 `TRACE_BODY_ROOT`」，而读侧（计划 3）**尚未实现**（`tracearchive.Reader` 当前只被测试引用）。因此本任务**不是**计划 4 在「挂载就位 + sealer 就位 + 功能关闭」阶段就能交付的验收；它的位置只能是：**读侧就绪（D73 硬门满足）→ 任务 7 硬门之后开启写侧 → 立即执行本门禁（在非活跃颜色上验证）**。在写侧启用之前，本任务只能交付**步骤 3 的运行手册文本**（那是文本契约，与功能开关无关），步骤 4 的观察部分必须挂起。**（E73 订正（本轮实测，只改事实不改规格）**：读侧**代码**在 base（`main`）上**已经存在**并在 `cmd` 里接线 —— `internal/observability/service/trace_body_service.go` ＋ `cmd/trace-body/main.go`（`git merge-base --is-ancestor <该文件引入提交> 0bfb130f` ⇒ **base 已含**）⇒ 上面「读侧尚未实现 / `Reader` 当前只被测试引用」**不成立**；**不变**的是 D73 是**部署**硬门：读侧**代码**就绪 ≠ 生产读侧**已就绪可用**，且写侧在部署配置里仍是**关闭**的（`deploy/blue-green/docker-compose.{blue,green}.yml:42` 的 `TRACE_BODY_ROOT` 都注释着，`TRACE_BODY_FLUSH_INTERVAL: 1m` 在 `:38`）⇒ 步骤 4 仍然**挂起**，理由与读数登记见 E73 节与 `p4-task-11-host-deferral-at-e854528c.txt`。）
> - 若发起者决定在读侧就绪之前不开启写侧（照 D73 的字面要求，这是默认），则本任务在计划 4 内**只交付运行手册**，并把「≤1 tick 闩锁」登记为**读侧就绪后的启用门禁**（交付报告必须如实如此陈述，不得宣称该项已通过）。

- [ ] **步骤 1：写失败的测试（运行手册条目的契约）**

```go
// TestRunbookDocumentsMountLossPrecondition 必须写出本项的前置条件：`TRACE_BODY_ROOT`
//   非空**且** root 目录在场（spooler.go:144 的 rootExists）；否则 sink 为 nil
//   （cmd/gateway/main.go:275）、TraceSpooler 根本不构造 ⇒ 期望的 ERROR 永不出现、
//   验收恒真。并写明它的位置是「读侧就绪（D73）→ 启用写侧 → 立即执行的门禁」。
// TestRunbookDocumentsMountLossLatchesWithinOneTick 必须写出：撤掉挂载后网关在
//   ≤1 个 tick（≤ TRACE_BODY_FLUSH_INTERVAL，生产 1m）内闩锁、不再写盘、正文降级为
//   body_missing=true；并给出**可粘贴的观察命令**（日志正则 + 文件系统断言）。
//   同时必须写出：① 验证窗口的 ⛔ 停止点「禁止切换蓝绿流量」是**丢正文防护**（不是
//   仅降级），② 「功能关闭 ⇒ 回落 jsonb ⇒ 无损」只是**补救动作**，③ 两种降级不对称，
//   ④ 该实验只允许在测试环境或无真实流量的生产窗口执行，并留证「闩锁期间累计丢失行数」。
// TestRunbookDocumentsDiskSwapNeedsGatewayRestart 换盘（dev/fstype 变化）⇒ 身份基线
//   **永久**不匹配 ⇒ 网关会一直闩锁到重启为止（这是**已知**形态，不是 bug）：
//   处置 = 蓝绿切到对端颜色（零宕机重启）或重启容器；并写明必须重启后才能恢复写盘。
// TestRunbookDocumentsShutdownSixSecondBound 关停时 6s（traceSpoolCloseWait）内没排空
//   ⇒ 该批降级为 body_missing=true + 一条 ERROR；处置与观察命令。
// TestRunbookDocumentsFeatureOffIsByteIdentical 功能关闭时路径与新部署前**逐字节相同**，
//   并给出 SQL：`body_ref_part IS NULL AND body_missing = false` 的行数应等于总行数、
//   且正文出现在 jsonb 列。
// TestRunbookDocumentsRetentionAndSpaceBudget 必须写出保留口径与空间预算：
//   minAge 3h × 写入速率 + 产物 + 余量；并给出 `/datalog` 的余量检查命令。
//   （2026-10-08 实测基线：407G 已用 / 153G 可用 —— 引用它时必须标注日期。）
// TestRunbookDocumentsSealerHeartbeatAndPermanentFailure 心跳缺失（>3×poll 无
//   「轮次结束」）⇒ 告警；`too_many_rows` ⇒ 人工介入的具体动作（拆块/调小 BLOCK_MB
//   并重新标定）；隔离件（spool 目录里出现的明文 `.jsonl.zst`）如何处理。
// TestRunbookNeverMentionsSealInterval D71 的无效键不得出现在手册里。
```

- [ ] **步骤 2：跑并确认失败**（节不存在 ⇒ 断言红）。

- [ ] **步骤 3：写手册一节**（内容对应上表；每条都要给出**可粘贴命令**，而不是描述）。核心条目示例：

```
### 撤掉挂载（≤1 个 tick 闩锁）
前置（缺一不可）：写侧已启用 —— TRACE_BODY_ROOT 非空（compose 里已取消注释）且
  /datalog/trace-archive-v4 目录在场；功能关闭时 sink==nil、TraceSpooler 不构造，
  下面的 ERROR 永远不会出现（断言会恒真）。本项是「读侧就绪 → 启用写侧」之后的门禁。
观察（生产，假设活跃颜色 blue、容器名 llm-blue-gateway / llm-green-gateway）：
  docker logs llm-blue-gateway --since 5m 2>&1 | grep -E 'trace-spool.*(root_unavailable|root-identity-mismatch)'
  docker exec llm-blue-gateway ls -l /datalog/trace-archive-v4/spool | tail   # 不应再出现新文件
判定：日志出现 ERROR（reason=root_unavailable 或 root-identity-mismatch）**且**不再有新 spool 文件
      ⇒ 已闩锁（≤1 个 tick = ≤1m）。正文路由此后全部 body_missing=true —— 这是**丢正文**，
        不是「回退 jsonb」（见下「两种降级不对称」）；网关本身继续服务。
恢复：remount 后**仍需重启该颜色容器**（身份基线是启动时取的；换盘/换挂载点必然不匹配）。

### 两种降级不对称（背下来，别记反）
  · sink == nil（TRACE_BODY_ROOT 为空，或构造期失败）⇒ 正文走既有路径落 jsonb 列 ⇒ **无损**；
  · sink != nil 但 spool 写入失败 / 根身份闩锁（例如挂载消失）⇒ 该行 body_missing=true、
    四个定位键全 NULL，正文**既不进 /datalog 也不回退 jsonb** ⇒ **丢正文**。
  所以「把写侧关掉让正文回落 jsonb」是**事后补救动作**，不是「切流无损」。
```

- [ ] **步骤 4：挂载消失的生产验证（**该实验在写侧启用状态下会持续丢正文；只在测试环境、或无真实流量的生产窗口执行**）**

0. ⛔ **门槛一（前置条件，缺则本步骤无意义）**：确认写侧**已启用** —— `docker exec llm-$COLOR-gateway env | grep '^TRACE_BODY_ROOT='` 非空，且 `docker exec llm-$COLOR-gateway ls -d /datalog/trace-archive-v4` 成功（`spooler.go:144` 的 `rootExists` 是**构造期**要求）。任一不满足 ⇒ **本步骤不执行**（此时能观察到的只有「什么都没发生」，那不是通过而是空过）。写侧启用本身受 D73 硬门约束（读侧就绪之前不得开启）⇒ 本步骤的合理位置是「读侧就绪 → 任务 7 硬门 → 取消注释 `TRACE_BODY_ROOT` → 重建非活跃颜色容器 → **立即**执行本步骤」。
0b. ⛔ **门槛二（执行环境，承重）：只允许在没有真实流量的地方做**。被 `umount` 的颜色进入闩锁后，每一行正文都会被记成 `body_missing=true` 且**永久丢失**（见第 2 步的「两种降级不对称」）—— 也就是说**这个实验本身在丢正文**。因此只允许：① **测试环境**（任务 8 的 A3 专用回环实例，或测试环境 compose）执行；或 ② 生产上**已确认没有任何真实流量**的颜色/窗口（例如该颜色已摘流、且处在维护窗口）。**禁止**在有正常流量的生产颜色上执行。
   - **执行前必须留证「流量为零」**：抄录窗口内落向该颜色端口（gateway 8080/8082）的 nginx access 请求计数为 0，或抄录该颜色处于「未接流量」（非活跃且已摘流）的部署状态；一并抄录闩锁开始前的 `body_missing` 基线计数。**无法留证 ⇒ 不执行。**
1. 确认 `.active-color`（假设 blue 活跃、green 非活跃）：`cat deploy/blue-green/.active-color`。
2. ⛔ **停止点 A：验证窗口内禁止切换蓝绿流量 —— 这是「丢正文防护」，不是「仅降级」**。从此刻到第 7 步「停止点 B」之间：**不得**运行 `deploy/blue-green/deploy-blue-green.sh`（含 `--rollback`）、**不得**改 nginx 的 `NGINX_UPSTREAM` / `upstream-*.conf`、**不得**让任何业务请求落到本次被 `umount` 的颜色上。窗口内必须有人值守（典型时长 ≤10 分钟）。
   - **两种降级不对称（承重口径，别记反）**：① `sink == nil`（功能关闭 / 构造失败）⇒ 正文走既有路径落 PG 的 jsonb 列 ⇒ **无损**；② `sink != nil` 但 spool 写入失败或根身份闩锁 ⇒ 该行 `body_missing=true`、四个定位键全 NULL，正文**既不进 `/datalog` 也不回退 jsonb**（`internal/gateway/observability/trace_event.go` 的 `buildTraceModels` ① 失败分支）⇒ **丢正文**。被 `umount` 的颜色此刻正处在 ② ⇒ 切流到它 = **按行丢正文**。
   - **补救动作（不是「切流无损」）**：若切流已经发生，立刻把该颜色的功能**关掉**（compose 里的 `TRACE_BODY_ROOT` 重新注释掉 → `up -d --force-recreate` 重建容器）⇒ 构造期 `Enabled=false` ⇒ `sink` 为 nil（`cmd/gateway/main.go:275`）⇒ 从那一刻起正文**回落 jsonb、恢复无损语义**。但闩锁窗口内已经写下的行**无法找回**（正文从未落盘），且这是一次**静默降级**（不报错、`body_ref_*` 全 NULL）⇒ 必须记进上线记录并按事故复盘。
3. 在 green 容器里：`nsenter -m -t $(docker inspect -f '{{.State.Pid}}' llm-green-gateway) umount /datalog`。
4. 观察 ≤1 个 tick：`docker logs llm-green-gateway` 必须出现 `root_unavailable` / `root-identity-mismatch` 的 ERROR；`docker exec llm-green-gateway ls /datalog/trace-archive-v4/spool` 必须看到**空目录**（挂载点目录还在 ⇒ 这正是「只看目录在不在」不够用的实证）；且不再出现新文件。
4b. **必须留证的「累计丢失行数」指标**：在 `umount` 前、恢复后各取一次闩锁期间的 `body_missing` 增量 —— ① 网关日志里 `body_missing_rows=<n>` 的增量（与 `spool_failures` 同一条汇总）；或 ② 在测试/A3 库上 `SELECT count(*) FROM call_traces WHERE body_missing = true AND created_at >= '<umount 时刻>' AND created_at < '<恢复时刻>'`。两者都抄录进报告。**期望增量 = 0**（因为已按门槛二摘流）；非 0 ⇒ 说明实验在真实流量下执行，必须按事故复盘并写明丢失行数，且该次实验**不得**作为验收证据。
5. 验证不影响用户：`curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9080/health`（走 nginx ⇒ 活跃颜色）仍 200；活跃颜色（`docker logs llm-blue-gateway`）**无**相关 ERROR。
6. 恢复：`docker compose -f deploy/blue-green/docker-compose.green.yml -p llm-green up -d --force-recreate gateway`（service 键是 `gateway`；容器名 `llm-green-gateway`）并确认日志恢复为正常轮次。
7. ⛔ **停止点 B**：确认第 4–6 步全部符合预期、活跃颜色始终无异常、第 4b 步的丢失行数为 0 之后，才**解除**第 2 步的「禁止切换蓝绿流量」约束。
8. 抄录第 3–7 步的全部命令与输出。**在测试环境先做一遍**（任务 8 的 A3 实例上更安全：直接 `umount /srv/trace-a3`），生产只作为确认。

- [ ] **步骤 5：写变异集合** `p4-task-11-runbook.py`（`TARGET = "docs/deployment.md"`，`MANIFEST_FILES = ["tests/deploy/trace_runbook_test.go"]`）：m01 删掉「需要重启才恢复」一句 → `TestRunbookDocumentsDiskSwapNeedsGatewayRestart`；m02 把「≤1 个 tick」改成「≤10 分钟」→ `TestRunbookDocumentsMountLossLatchesWithinOneTick`；m03 删掉 SQL 比对 → `TestRunbookDocumentsFeatureOffIsByteIdentical`；m04 把 6s 改成 60s → `TestRunbookDocumentsShutdownSixSecondBound`；m05 插入 `TRACE_BODY_SEAL_INTERVAL` → `TestRunbookNeverMentionsSealInterval`；m06 删掉前置条件段落（`TRACE_BODY_ROOT` 非空 + root 在场 + 「读侧就绪后启用」）→ `TestRunbookDocumentsMountLossPrecondition`；m07 删掉「验证窗口内禁止切换蓝绿流量（丢正文防护）」的停止点 → `TestRunbookDocumentsMountLossLatchesWithinOneTick`。

> **E73 补登记的静态检查行（3 行；E69② 的计划↔集合可对照性）**：上面只登记了 m01–m07，而步骤 1 另有两条**在册用例**（`TestRunbookDocumentsRetentionAndSpaceBudget` / `TestRunbookDocumentsSealerHeartbeatAndPermanentFailure`）与一条「两种降级不对称 / 关掉写侧只是补救动作」的文本要求没有对应变异（七条编号已占满）⇒ 集合里按**约束 6** 各补一行 `expect=present` 的静态检查（`df -h /datalog` / `too_many_rows` / `两种降级不对称`）。它们只承载**存在性**，**行为**要求由那两条在册用例与 `TestRunbookDocumentsMountLossLatchesWithinOneTick` 承载（E62）；三行的可失败性由「改坏→转红→还原」探针实测（`p4-task-11-runbook-probe-at-*.txt`）。集合的 `REQUIREMENTS` 里每条计划要求都有对应条目（`--mapping` 输出「无缺口」），表格见 E73 节的登记。

> **E73① 的连带登记（同轮）**：本步骤的 `MANIFEST_FILES` 只列 `tests/deploy/trace_runbook_test.go`；任务 10 的那条**行为**要求（「冻结包真按此布局写」）改由 `tests/tracesealer/footer_artifact_test.go` 的在册用例承载 ⇒ 它被登记进 **`p4-task-10-footer` 的 `MANIFEST_FILES`**（见 Task 10 文件清单的 E73① 补登记与 E73 节）。
  > **m06/m07 的效力边界（如实标注，写进 `why`）**：这两条验证的是**计划/手册文本里的门禁陈述是否存在**（`docs/deployment.md` 中必须出现「前置条件」与「禁止切流」两段），**不是**运行时行为 —— 自动用例钉不住「切流会被阻止」，只能钉住「这句话还在不在」。因此 `why` 必须写成「门禁声明的存在性」，**不得**声称它证明了运行时防护；运行时防护由门槛二（摘流留证）与停止点 A（人工值守）承担。删掉 m06/m07 会让这两条门禁失去唯一的自动覆盖 ⇒ 保留。

- [ ] **步骤 6：提交 → 跑集合。**

---

### Task 12：最终 HEAD 全量重跑、跨计划契约与交付报告

**文件：** 可能微调（若前面的任务留下未清的临时分支/TODO）；交付物是**报告**（写进 PR 描述或 `docs/superpowers/plans/2026-10-08-trace-sealer-deployment-report.md`，由执行者与发起者约定；**不得**顺手改别的计划文档）。

- [ ] **步骤 1：机械核对「没有欠账」**

```bash
# ① 冻结包零改动（本计划最重要的边界之一）
git diff --stat 3a2301ad..HEAD -- internal/tracearchive/     # 期望：空
# ② 计划 2 的写侧代码是否被本计划动过（动了就必须重跑 p2 集合）
git diff --stat 3a2301ad..HEAD -- internal/gateway/observability/ internal/gateway/config/trace_body.go
# ③ sealer 侧没有建目录调用形态
grep -rnE '\bMkdir[A-Za-z]*\s*\(' internal/tracesealer/ cmd/trace-sealer/ || echo "OK: 无"
# ④ 零迁移
git diff --stat 3a2301ad..HEAD -- atlas-migrations/          # 期望：空
# ⑤ 未清的 TODO/占位
grep -rn 'TODO\|FIXME\|占位' internal/tracesealer/ cmd/trace-sealer/ || echo "OK: 无"
# ⑥ 无效键不得出现在任何部署产物里（D71）
grep -rn 'TRACE_BODY_SEAL_INTERVAL\|SEAL_INTERVAL' deploy/ docs/deployment.md || echo "OK: 无"
```

- [ ] **步骤 2：在最终 HEAD 上重跑**每个 `p4-task-*.py`（**全部**，含 `--check`），并**重新生成**每份清单（`MANIFEST_DERIVATION` 现推）与 sha256 绑定。**禁止**复用之前任何一轮的表格：新提交会让旧绑定全部失效。
  建议用一条循环（集合名按实际生成）：
```bash
# 每个集合用**它自己的** MANIFEST_DERIVATION 现推清单（空清单会被判「无效证据」）。
python3 - <<'PY' > /tmp/p4-rerun.sh
import glob, importlib.util
for f in sorted(glob.glob("tools/trace-mutate/sets/p4-task-*.py")):
    spec = importlib.util.spec_from_file_location("m", f); m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
    names = f"/tmp/{m.NAME}-names.txt"
    print(f"echo '=== {m.NAME} ==='")
    print(f"{m.MANIFEST_DERIVATION} > {names}")
    for flag in ("--check", ""):
        print(f"python3 tools/trace-mutate/trace_mutate.py --repo . --manifest {names} "
              f"--mutation-set {f} {flag} || echo '!!! 失败: {f} {flag}'")
PY
bash /tmp/p4-rerun.sh 2>&1 | tee /tmp/p4-rerun.log
grep -c '!!! 失败' /tmp/p4-rerun.log   # 期望 0
```

- [ ] **步骤 3：跑全套本地测试并抄录**：`go build ./... && go vet ./... && go test ./tests/tracesealer/... ./tests/traceverify/... ./tests/traceverify/... ./tests/deploy/... -count=1 -v`（并把 `go test ./internal/tracearchive/ -count=1` 的输出一并抄录，用来证明**没有改动**冻结包的行为）。

- [ ] **步骤 4：写报告**，必须包含（缺一条即未完成）：
1. 每个任务的**提交哈希 + 该任务全部集合的 sha256 绑定表**（工具头部原样抄；任务 5 是 5 个集合，全文共 **27** 个 —— **E66⑥ 订正**：原写 26，少算了任务 4 拆出的 `p4-task-4-runner.py`）。
2. A3 / A6 / A7 / A8 + 「挂载消失 ≤1 tick」五项验收的**真实输出**（命令 + 输出原样粘贴，含日期）。**其中「挂载消失 ≤1 tick」必须写明它是何时、在哪种环境下执行的**：① 若执行时写侧尚未启用（D73 硬门仍在等读侧），必须写成「未执行：前置条件（`TRACE_BODY_ROOT` 非空）不满足」并登记为读侧就绪后的启用门禁 —— **不得**因为观察命令没报错就宣称通过（功能关闭时那些命令本来就什么都不会报）；② 必须抄录「执行环境的流量为零」留证与**闩锁期间累计丢失行数**（期望 0；非 0 ⇒ 按事故复盘，且不得作为验收证据）。
3. **跨计划契约**（下一节的三条，逐条写清「谁负责、怎么验证」）。
4. 规格缺陷登记 D64–D74 的**实际处置**（照做了还是改了主意，为什么）。
5. **诚实的覆盖缺口**：哪些要求只被静态检查覆盖、哪些用例靠替身而非真实 zstd、哪些结论只能在生产上取（因本机是 zstd v1.5.7）、哪些路径没有被任何自动用例覆盖。
6. 生产实测事实（当天重取的 `df -h` / `findmnt` / `zstd --version` / `.active-color`）与旧定时器的处置记录（含 `is-active`/`is-enabled` 输出）。
7. A8 决策门的结论（改不改 `spooler.go`，以及若改了，`p2-task-5`/`p2-task-8-spooler` 的重跑结果）。
8. **风险登记 R1 / R2 的处置证据**：R1 = 「停止点 1.5」的两次 `du -sb /datalog` 原始输出 + 外推出的增长速率与可撑天数 + 最终选了哪个选项（mask / 保留清理 / 另设 unit）；R2 = `scratch-prepare` 的容量预检与事后 `df -h` 输出、镜像路径的设备号断言。

---

## 跨计划契约（写侧 ↔ 封块侧 ↔ 读侧；**必须写进报告，且读侧必须照此实现**）

> 读侧的权威计划文档是 `docs/superpowers/plans/2026-09-25-trace-point-read-service.md`（计划 3/4，已存在、尚未实现）。它的任务 2 已经把下面 C2 的判定写进实现要求（它的 D55），任务 1 已经把输入校验收口（它的 D59）。**本计划的 C1–C3 是与它的一致性复核，不是新的要求**；若执行中发现两者不一致，以**发起者裁定**为准并写进报告，不要单方面改任何一方的计划。

| # | 契约 | 谁产生 | 谁消费 | 怎么验证 |
|---|---|---|---|---|
| C1 | **写失败 ⇒ 当前 part 被改名成隔离件**（同目录下、后缀仍是 `.jsonl.zst`、内容是明文 JSONL chunks），随后**读侧必须报 `ErrNotArchived`**（500），而不是把它当产物去解码 | 网关（计划 2 的 `trace_event.go`） | 读侧（计划 3） | 计划 2 已有用例；本计划只要求运行手册写明「隔离件 = 需要人工处理的响亮降级」 |
| C2 | **构造失败或功能关闭 ⇒ sink 为 nil ⇒ 正文回落进 PG 的 jsonb 列，且 `body_missing=false`**；因此 **`body_ref_*` 全 NULL + `body_missing=false` 必须被读侧解释为「正文在 jsonb 列」**，而不是「找不到正文」 | 网关（计划 2） | 读侧（计划 3） | 本计划：功能关闭时的逐字节等价验证（任务 11 的 SQL）；读侧：必须有一条用例覆盖该组合 |
| C3 | **所有未封块的 part 必须从 spool 读**；`ErrTooManyRows` 的块会**永久**留在 spool（sealer 永不封、永不删，N5）⇒ 读侧不得假定「有 ref 就有产物」 | sealer（本计划） | 读侧（计划 3） | 本计划：任务 10 的分类与「永不删」；读侧：spool 兜底分支 |

另有两条给读侧的**实现约束**（不满足就会静默出错）：

- 读侧必须用 **`body_ref_inst`** 定位产物，**不得**用配置里的 `inst`（蓝绿切换期间两个 inst 并存；`body_ref_inst` 才是那一行的真实来源）。
- 读侧并发上限必须用 `internal/traceverify.MaxConcurrent`（控制面限额取蓝绿编排的 **2 GiB**，D72）。
- **不得把 C2 的判据外推到「闩锁」路径**（两种降级不对称，见全局约束与任务 11）：C2 只覆盖 `sink == nil`（功能关闭 / 构造期失败）。当 `sink != nil` 而 spool 写入失败或根身份闩锁时，正文**不会**回落 jsonb，该行是 `body_missing=true` + 四个定位键全 NULL ⇒ 「`body_ref_*` 全 NULL 且 `body_missing=false` ⇒ 正文在 jsonb」**只对 sink==nil 的行成立**。若要让闩锁也回退 jsonb，那是候选改进 **I1**，需要读侧判据同步修订（本计划不实现）。

## 规格缺陷与改进登记（D64–D74；**只登记，除注明外不在本计划修改**）

> 编号顺延自现有计划：计划 1 用到 D41、计划 2 用到 D52、**计划 3（读侧，`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）用到 D53–D63**，本计划从 **D64** 起。计划 3 已先于本计划登记了同一批冲突里的读侧部分（例如它的 D55 = 本计划 C2、它的 D56 = 本计划的 A 表冲突、它的 H15 = 本计划任务 9 要回填的解码内存实测）⇒ **本计划沿用其裁定，不重复登记、不另立反对结论**。

| 编号 | 位置 | 问题 | 本计划的裁定 |
|---|---|---|---|
| D64 | 规格 §11.1 sealer unit 示例 | 示例把 `--block-mb 1536 --seal-interval 15m` 放在 **sealer 的 ExecStart** 上，但这两个是**网关侧**的参数：冻结包的 `WriterOptions.BlockBytes` / `WriterOptions.SealInterval`（后者默认 `internal/tracearchive/defaults.go:68` 的 `defaultSealInterval = 15 * time.Minute`，`writer.go:218` 取默认值；生产由计划 2 的 `TRACE_BODY_ROLL_INTERVAL=1h` 覆盖）。冻结 `SealerOptions` **没有**这两个字段：sealer 不做大小/时间阈值判定，它只按 mtime + `MinAge` 发现「已停止追加」的 spool。照抄示例会让 sealer 以未知 flag 直接退出（启动即失败），或误导实现者去实现一个不存在的「封块间隔」 | sealer 的 flag 只暴露 `--root/--zstd-binary/--zstd-level/--zstd-window-mb/--min-age/--expected-zstd-version/--poll-interval/--once/--lock-file`；**不出现** `--block-mb` / `--seal-interval`。规格 §11.1 的 `TRACE_BODY_SEAL_INTERVAL` **不是有效键名**（同一概念在网关侧叫 `TRACE_BODY_ROLL_INTERVAL`，生产取 `1h`）⇒ unit / compose / 剧本 / 运行手册一律不出现该键（见本表 D71） |
| D65 | 规格 §12 旧归档定时器 | 只「建议显式停用」，没给顺序、回滚，也没说它的 retention（`RETAIN_DAYS=5`）会不会删到新树 | 隔离旧定时器是**启用写路径的前置条件**；脚本默认 dry-run + 删除谓词审查（命中新树即 exit 3）+ 独立的回滚脚本。**隔离的代价**：`mask` 会连旧树的保留/清理一起停掉，而写侧启用被 D73 硬门无限期推迟 ⇒ 隔离窗口内 `/datalog` **没有任何 retention**；因此 `--confirm` 之前必须先过任务 5 步骤 5 的**停止点 1.5**（量化增长速率与可撑天数），完整处置见**风险登记 R1** |
| D66 | 规格 §14.2 A3 | 「把 /datalog 填满」在生产上会危及正在服务的网关（`/` 也曾被写满，2026-09-24 事故），规格没给安全做法 | A3 的**真实盘满**只在**专用回环 ext4** 上做，主机优先测试环境；`GuardScratchTarget` 在**回环挂载成功之后**证明目标与 `/`、`/datalog` 不同设备（挂载前校验必然误拒，见任务 8 步骤 3）。生产侧只做水位闩锁与挂载消失两项非破坏性验证。**护栏只覆盖挂载点、不覆盖镜像文件本身** ⇒ 镜像的容量/设备护栏见**风险登记 R2** |
| D67 | 规格 §14.2 vs §16 | A8 在两处措辞不同：一处是「读 1,000 次后目录占用增量 == 0」（与 A4 重复），一处把「行数分布」并入 A8 | 本计划按**行数分布 + 越界分类与告警**实现 A8；「目录占用增量 == 0」属读侧（计划 3），不在本计划 |
| D68 | 规格 §16 第 2 条 vs 计划 2 任务 8 的 A 表 | 规格 §16 要求 A1–A9 全过；但「A 表」有两份语义不同的版本：规格 §14.2（A2=杀掉 sealer 中途、A6=2 GB 单块 P99、A7=超并发点读、A8=读取 1000 次目录零增量、A9=移走 zstd 二进制）与计划 2 任务 8 的表（A2=封块产物可点读、A6=压缩比与解码内存、A7=跨进程验证、A8=行数分布、A9=冷热分层）。计划 3 的 D56 已裁定**以计划 2 任务 8 的 A 表为准** | **本计划沿用同一裁定**：承担 **A3（真实盘满）/ A6（压缩比与解码内存）/ A7（跨进程验证）/ A8（行数分布）** + 新增「挂载消失 ≤1 tick」；**A9 = 冷热分层 ⇒ 明确不在本计划范围**（与计划 3 的 H13 口径一致），也不在计划 3 范围。规格 §14.2 的 A9 语义（移走 zstd ⇒ 封块失败、spool 保留、下一轮成功）与本计划的 `ErrSealFailed` 重试路径**同源**，任务 8 run.sh 步骤 9（释放空间后观察恢复）顺带覆盖它，但**不改名、不算作 A9**。**验收报告不得宣称 A1/A2/A4/A5 或冷热分层已通过。** |
| D69 | 规格 §11.1 挂载矩阵 | 只写「gateway spool 可写 / sealer 读写 / cp 只读」，没回答 gateway 要不要读；而简报里「gateway 是否挂取决于读侧是否经 control-plane 转发」是**因果错位** | gateway **永远**读写挂 `/datalog`（写 spool 必需，与读侧路由无关）；cp 只读挂（`reader.go` 全程无写操作）；sealer 在宿主机直接用，无容器挂载 |
| D70 | 规格 §11.1 | 声称「gateway 容器唯一变更是新增 bind mount」，但没说挂载点不存在时 Docker 会以 root 在**宿主根 FS** 上创建它（与 2026-09-24 事故同类），也没说 `TRACE_BODY_ROOT` 目录由谁创建 | 部署前 `findmnt -T /datalog` 必须显示 `TARGET=/datalog`；`install -d /datalog/trace-archive-v4` **只在确认挂载之后**人工执行；compose 一律用长语法 `type: bind`；sealer 与网关**都不得**创建归档根 |
| D71 | 规格 §11.1 / §5.3.3 | `TRACE_BODY_SEAL_INTERVAL: "15m"`（及正文里的「时间阈值 15 分钟」）**不是有效键名**：同一概念在网关侧是 `WriterOptions.SealInterval`（冻结默认 `defaultSealInterval = 15m`），生产由计划 2 的 `TRACE_BODY_ROLL_INTERVAL` 覆盖为 `1h`；sealer **没有**该参数（它只按 `MinAge` 发现，不看时间阈值）。照抄会写出一个**静默无效**的配置项：它不报错、也不生效 | unit / compose / 剧本 / 运行手册一律不出现该键；并由 `TestSealerUnitNeverUsesSealIntervalKey`、`TestComposeNeverCarriesSealIntervalKey`、`TestBlueGreenRunbookNeverMentionsSealInterval`、`TestRunbookNeverMentionsSealInterval` 四处钉住 |
| D72 | `docs/deployment.md` / `docs/guardrails.md` / `config/prod.yaml` 三方不一致；**以及 C5 的估算口径** | ① 内存：文档写 gateway 1GB / cp 512MB，实际蓝绿编排是 2g/2g（根编排 2g/512m）；② Redis DB：guardrails C3 写生产 =2，蓝绿编排默认 3；③ `config/prod.yaml` 的 `redis.db: 2` | **以 `deploy/blue-green/docker-compose.{blue,green}.yml` 为准**（CI 生产上线走它）；C5 预算取 **control-plane 2 GiB**。三处不一致只登记，**不在本计划修** |
| D73 | 规格的计划次序（写侧 → sealer → 读侧） | **没有任何一处写明「写侧一开启，新行的正文在管理面读不到」**：全仓 `tracearchive.Reader`/`GetBody` **只被测试引用**，无生产调用者（计划 3 正是要实现它，但**当前 `main` 上它还不存在**） ⇒ 先开写侧会让正文只进 `/datalog`、UI 取正文为空，**静默的用户可见丢失** | **功能开启（设置 `TRACE_BODY_ROOT`）以「同一 root 的读侧可用」为硬门前置**；本计划只能交付到「挂载在场 + sealer 就位 + 功能关闭」，开启是任务 7 硬门之后的人工动作 |
| D74 | 规格 §11.2 V4/V5 | 容器内 uid/gid 与 `/datalog` 属主、sealer 以哪个用户运行未定 | 首轮 sealer 以 **root** 运行（与容器内 gateway 同为 root，避开设错 uid 导致的 `ErrSealFailed` 噪声），并用 `ProtectSystem=strict` + `ReadWritePaths=/datalog` 限制可写面；「改用专用用户」登记为后续项（需先实测容器 uid） |

### 候选改进登记（不占用 D 编号；**后续计划**再评估，本计划与计划 2 **都不实现**）

**I1：闩锁后回退 jsonb —— 把「丢正文」变成「无损降级」**

- **现状（本计划采用的事实口径，已写进任务 11）**：`sink != nil` 时 spool 写入失败或根身份闩锁 ⇒ 该行 `body_missing=true`、四个定位键全 NULL，正文**既不进 `/datalog` 也不回退 jsonb** ⇒ **丢正文**。只有 `sink == nil`（功能关闭 / 构造期失败）才走 jsonb ⇒ 无损。这两种降级**不对称**。
- **候选改法**：闩锁期间把 sink 视作 nil（或在 `buildTraceModels` 的 ① 失败分支改走 `traceEventToModel`），让正文回落 PG 的 jsonb 列 ⇒ 任意时刻都无损。
- **代价（必须先设计再实现，不能只改一行）**：一旦允许回退，PG 里会出现**三种形态并存** —— ① `body_ref_*` 非 NULL（正文在 `/datalog`）；② `body_ref_*` 全 NULL 且 `body_missing=false`（正文在 jsonb）；③ `body_missing=true`（历史丢失行）。读侧**必须**能区分三态，**不能**再用「`body_ref_*` 全 NULL + `body_missing=false` ⇒ 正文在 jsonb」这一条判据去覆盖所有 `sink != nil` 的场景。
- **因此**：该改进**必须与读侧（计划 3）的 `body_missing` / jsonb 回退判据一起设计** —— 要么在行上增加显式的「正文来源」标记（需要 PG 变更，本计划不做），要么让读侧按「闩锁状态 + 时间窗口」重建判据。无论走哪条，都属**跨计划契约**（本计划「跨计划契约」一节的 C2 与 H1）的修订 ⇒ 需发起者裁定，并同步修订计划 3 的 D55 / H1。
- **编号**：留给后续计划（缺陷编号仍从 **D75** 起，见 H8；本条用 `I` 前缀，**不占用**编号段）。

## 风险登记（R1–R2；**不是建议，是交付条件**）

> 这两条是评审后补的**显式风险登记**（编号用 `R` 前缀，**不占用** D 表的编号段 —— 下一份计划的缺陷编号仍从 **D75** 起，见 H8）。它们的共同点是：**默认路径会静默地留下一个没人看的口子**，所以必须在对应任务里产出**可抄录的证据**，否则那个任务不算完成。

### R1 `mask trace-archive.timer` 会让 `/datalog` 在隔离窗口内**没有任何 retention**（承重；落在任务 5 步骤 5 与任务 7 硬门之间）

- **事实**：生产机上的 `trace-archive.timer`（每天 **04:30**）调 `/root/daily-trace-archive.sh`（`RETAIN_DAYS=5`）。这个脚本**同时**承担两件事：① 归档；② **保留/清理**（`find ... -mtime +5 -delete` 一类的删除谓词）。任务 5 步骤 5 的隔离动作是 `systemctl disable --now` + `systemctl mask` —— 它会把这两件事**一起**停掉。
- **为什么危险**：写侧的启用被 **D73 硬门**无限期推迟（读侧 = 计划 3，其计划文档已存在但**尚未实现**）⇒ 从「隔离完成」到「写侧启用」之间的窗口可能长达**数周**。这段窗口里 `/datalog` **没有任何 retention**，是**唯一**可能让 `/datalog` 增长失控的路径。基线（2026-10-08 实测）：**407G 已用 / 153G 可用**（合计 560G，72.7%）。
- **可执行选项（执行者必须在 `--confirm` 之前选定一个并写进上线记录）**：
  1. **保留旧 timer 的保留动作、只停它的归档动作** —— 不改 timer，而是给 `daily-trace-archive.sh` 的归档段加开关（例如 `ARCHIVE_ENABLED=0` 时归档段空操作），**保留**其 `find ... -mtime +$RETAIN_DAYS -delete` 段。风险：它的删除谓词必须通过**停止点 1** 的审查（不得命中 `/datalog/trace-archive-v4`）。
  2. **另设一个只做保留清理的 unit**（例如 `trace-retention.timer` + 一个只做保留清理的脚本），与旧归档彻底解耦；该脚本的删除谓词**必须**显式排除 `/datalog/trace-archive-v4`（否则会删掉新树）。
  3. **接受窗口，但先量化**（见下）。默认不推荐；只有在量化显示余量远超窗口时才可用。
- **量化要求（硬性，必须在任务 5 步骤 5 的「停止点 1.5」完成）**：取两次 `du -sb /datalog`（间隔 ≥24h；不足 24h 则按小时外推并在记录里注明外推），算出**当前增长速率**（GB/天）与**剩余可用空间能撑的天数**（余量取当天 `df -h /datalog` 的可用值）。若「可撑天数 < 预计隔离窗口 + 5 天余量」（**规格未规定该阈值 —— 全文没有任何条款给出「余量天数」；「+5 天」是本计划提出的建议默认值（提案）**，执行者可调整，但必须在记录里写明取值理由）⇒ **不得 mask**，改走选项 1 或 2。命令与原始输出必须抄录进上线记录与交付报告（任务 12 步骤 4 第 6 条已要求「旧定时器的处置记录」）。
- **回滚**：若量化后发现撑不住，先执行 `restore-old-trace-archive.sh --confirm` 解除隔离，再改方案；**不得**靠「先 mask 再说」。

### R2 A3 的镜像约 1 GiB 落在 `/srv` 所在文件系统，`GuardScratchTarget` **只护挂载点、不护镜像本身**（落在任务 8 步骤 3）

- **事实**：`scratch-prepare --image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G` 会在 `/srv`（宿主根 FS 上的普通目录）写下约 **1 GiB** 的镜像文件。`GuardScratchTarget` 校验的是**挂载点**（挂载完成后它是回环 ext4，设备号与 `/`、`/datalog` 都不同）；**镜像路径本身不在它的保护范围内**。
- **风险**：若 `--size` 被调大、或上一次 `scratch-cleanup` 失败留下残片后重复创建，`/srv`（其所在文件系统通常就是 `/`）会被写满 —— 正是 D66 要防的「把宿主盘写满」的同一类事故，只是换了个入口。
- **必须补的护栏（任务 8 步骤 3，与 `GuardScratchTarget` 同一步序）**：
  1. **容量预检**：`fallocate` **之前**对镜像所在文件系统取 `statfs`，断言 `可用空间 >= size + 1 GiB 余量`；不足 ⇒ 拒绝执行（**不得**用 `dd` 兜底把盘写满）。
  2. **设备检查**：对**镜像路径**也做一次「不得与 `/` 同设备」的判断 —— 或显式承认「镜像允许在 `/` 上（它就是建在宿主根 FS 的普通文件）」，但必须在记录里写明 `size` 的**上界**与该文件系统当前的可用空间。
  3. **容量检查（执行后）**：`fallocate` 之后打印 `df -h $(dirname /srv/trace-a3.img)`，抄录进 A3 证据（任务 8 步骤 5）。
  4. **不得**让 `/srv` 与 `/datalog` 同设备：若同设备，1 GiB 镜像会直接挤占生产归档盘，且「专用回环」这条安全边界同时失效 ⇒ 拒绝执行。
- **处置**：这条护栏由任务 8 的 `p4-task-8-guard.py` 的 `CHECKS` 钉住（镜像路径的设备/容量断言必须在 `scratch-prepare` 的实现里 present）。

## 交接清单（计划 3 读侧 / 计划 5 收尾）

- **H1**（承重，**计划 3 已在其 D55 定了同一条裁定**，本计划只是复核一致）：把「`body_ref_*` 全 NULL 且 `body_missing=false`」解释为**正文在 PG 的 jsonb 列**，不是「找不到正文」（C2）。
- **H2**：按 `body_ref_inst` 定位，**不得**用配置里的 `inst`（蓝绿并存）。
- **H3**：跳过 `body_missing=true` 的行；**不得**假定 `body_ref_row` 连续（D51）。
- **H4**：未封块的 part 必须从 spool 读；`ErrTooManyRows` 的块**永久**留在 spool。
- **H5**：并发上限用 `internal/traceverify.MaxConcurrent` 的**实测结论**回填计划 3 的 `TRACE_BODY_READ_MAX_CONCURRENT`（即计划 3 的 **H15**）；C5 预算取蓝绿编排的 2 GiB（D72）。
- **H6**：隔离件（`part-NNNNNN.jsonl.zst` 内容是明文 JSONL）必须报 `ErrNotArchived`（500），**不得**新增哨兵（D47/D50 同族）。
- **H7**：不得新增 `atlas-migrations/` 而不先做全库对账（D52）。
- **H8**：缺陷编号从 **D75** 起（计划 1 用到 D41、计划 2 用到 D52、计划 3 用到 D63、本计划用到 D74）。
- **H9**：**A1 的读回半部（n = 1e6）物理测量归本计划任务 9**（口径 = 计划 3 的交接 **H19**；环境 = 生产宿主 + 真实 PG + 真实 `zstd` **1.4.8**；本轮只登记「待宿主执行」）；A2 / A5 的对外映射归计划 3、A4 由计划 1 覆盖；A9（冷热分层，见 D68）与 §5.5 冷层合并、`/datalog/emergency-archive` 存量点读（计划 3 的 D58/H13）、管理后台 UI 归计划 5/后续。
- **H10**：本计划的 A6 解码内存实测是计划 3 的 **H15 的唯一输入**（它的默认并发 2 就是按 600 MB/并发估的）⇒ 任务 9 完成后必须把结论回填给读侧，而不是只写在本计划的报告里。

## 自检结果

- **能否在不开写侧的情况下交付**：**代码与产品部分能**：任务 1–5 与任务 8–10 的自动化部分**完全不需要**触碰 `/datalog` 与生产；任务 6/7 的挂载与 sealer 就位也刻意在**功能关闭**下完成。开启写侧被收敛成任务 7 硬门之后的一个显式人工动作。**但有一项例外必须如实说明**：任务 11 的「撤掉挂载 ≤1 tick 闩锁」**观察**部分在功能关闭时恒真（sink 为 nil ⇒ `TraceSpooler` 不构造 ⇒ 期望的 ERROR 永不出现），因此它**不是**功能关闭阶段可交付的验收 —— 计划 4 在不开写侧时只交付它对应的**运行手册文本**，并把该验收登记为「读侧就绪后的启用门禁」。
- **本计划零未决的设计选择**：sealer 形态（simple 常驻）、锁（flock）、根护栏（mountinfo 身份）、A3 的安全做法（回环 ext4）、A6 的门槛来源（规格 + 标定注释）、A8 的决策门（阈值 + 三级处置）、挂载矩阵（gateway 读写 / cp 只读）、旧定时器处置（隔离脚本 + 回滚）—— 全部在本计划内定完。
- **已知的、被如实登记的弱覆盖**：① 「A7 的次序不变量」与「心跳每轮必打」只有静态检查（被测对象是冻结包与日志，本计划不能变异冻结包）；② `covers` 的根挂载点特判曾以「本机（darwin，无 `/proc/self/mountinfo`）不可达」为由把变异 `m04` 登记为 `EXPECTED_GREEN` —— **E64① 已订正并删除该登记**：`covers` 是**纯字符串函数**，包内行为用例 `TestCoversRootMountCoversPathsUnderRoot` 直调它、m04 为**断言红**（存在性另由静态检查 `covers必须特判根挂载点` 承载；`RoundResult.RootUnavailable` 已由 E63⑧ 新增的行为用例 `TestLoopReportsRootUnavailable` 承载，**不再是**弱覆盖）；③ 真实 zstd 路径的证据依赖本机是否装了 zstd，缺失时以 A3 环境的真实产物补；④ 生产门槛值（压缩比、水位换算）必须逐字取自规格，本计划不代抄；⑤ 「撤掉挂载 ≤1 tick」在写侧启用之前**不可执行**（见上一条与「验收项归属」表下方的说明）。
- **已知风险（不隐藏）**：① 换盘/换挂载点后网关会**永久**闩锁到重启为止（已写进运行手册，未修）；② `RequiresMountsFor=` 在「手工 umount」时是否可靠地停掉 unit 属纵深防御、未验证，主防线是进程内护栏；③ 容器 uid 与 `/datalog` 属主未实测（首轮用 root，已登记 D74）；④ **隔离旧 timer 期间 `/datalog` 没有任何 retention**（R1，必须量化增长速率）；⑤ **A3 的 1 GiB 镜像落在 `/srv` 所在 FS，不受 `GuardScratchTarget` 保护**（R2，必须补容量/设备护栏）。

## 执行顺序与依赖

```
任务 1（单轮封块+分类+退出码）
  └─ 任务 2（汇总/告警/心跳）
       └─ 任务 3（常驻循环+根护栏）
            └─ 任务 4（锁 + A7 自动化）
                 ├─ 任务 5（unit/构建/CI/旧定时器隔离）  ─┐
                 └─ 任务 6（挂载+编排）                    ├─ 任务 11（运行手册 ＋
                      └─ 任务 7（蓝绿剧本 + 硬门）        ─┘   写侧启用后的挂载消失验证）
                              └─ 读侧就绪（D73）→ 开启写侧 ────────▲
任务 8（A3 + 进程级 A7）────────────┐
  └─ 任务 9（A6 + 预算）            ├─ 任务 12（全量重跑 + 报告）
  └─ 任务 10（A8 + 契约钉住）───────┘
```

（任务 11 的**运行手册**部分只依赖任务 5/6/7；它箭头所指的**挂载消失验证**另有一个硬前提：写侧已启用 —— 见下方「生产上的执行次序」第 6–7 步。）
- 任务 8/9/10 只依赖任务 1–4 的代码，可与任务 5/6/7 并行（不同文件、不同集合）；但**任务 6 的挂载必须在任务 5 的旧定时器隔离之前完成核对**（隔离脚本的停止点 1 要求已核对挂载在场），且**任务 6 的挂载必须在任务 11 的验证之前**（两者都在生产上改状态）。
- **生产上的执行次序（不可并行，唯一一份，与任务 5 步骤 5、任务 7、任务 11 的正文逐字一致）**：
  1. **任务 6 的挂载**（非活跃颜色，**功能关闭**；含它的停止点 1–3）；
  2. **任务 5 的旧定时器隔离**（前置：已核对第 1 步的挂载在场；`--confirm` 之前必须先过**停止点 1.5 的 R1 量化门**）；
  3. 测试环境验证（`push test`，功能仍关闭）；
  4. **任务 7 的蓝绿切换**（`deploy-blue-green.sh`，功能仍关闭 ⇒ 逐字节等价）；
  5. **读侧就绪（D73 硬门：计划 3 落地且管理面能取到正文）+ 任务 7 硬门的其余条件（`findmnt` 正常、`/datalog` 余量 ≥5 天保留量）**；
  6. **开启写侧**（取消注释 `TRACE_BODY_ROOT` → 重建非活跃颜色容器）；
  7. **任务 11 的「挂载消失 ≤1 tick」验证**（**只允许**测试环境，或**无真实流量**的生产窗口：非活跃且已摘流；该实验本身会持续丢正文 ⇒ 禁止在有正常流量的颜色上做。**必须**在第 6 步之后执行，否则 sink 为 nil、断言恒真；验证窗口内遵守它的「停止点 A：禁止切换蓝绿流量」，并留证「累计丢失行数」）；
  8. 运行手册的其余条目与任务 12 的收尾。
- **注意次序的方向**：把任务 11 的观察部分排在写侧启用之前，得到的是一条**永远绿、什么都没证明**的验收（见「验收项归属」表下方的前置条件说明）。因此若第 5 步的读侧迟迟不就绪，本计划就**如实**只交付任务 11 的运行手册文本，并把该验收登记为「读侧就绪后的启用门禁」，**不得**宣称它已通过。
- 任务 12 必须**在最终 HEAD 上**执行，且在它之后再有任何提交 ⇒ 全部变异绑定失效，必须重跑重记。
- **基线（登记事实；E68②）**：本分支（`feat/trace-sealer-deployment`）的基点写的是 `main=0bfb130f`，但 `main` 引用**已前进到 `43af78b0`**（领先 `0bfb130f` **2 个提交** = `d8875bda` ＋ 合并提交 `43af78b0`），只动 **3 个文件**：`internal/gateway/authz/groups.go`、`tests/gateway/authz/groups_test.go`、`tests/gateway/orchestrator/group_auth_test.go`。本分支的改动面是 `internal/tracesealer/**` / `cmd/trace-sealer/**` / `tests/tracesealer/**` / `tests/deploy/**` / `deploy/**` / `docker-compose.yml` / `Makefile` / `.gitlab-ci.yml` / `tools/trace-mutate/**` / `docs/**` ⇒ 两者**零交集**，因此**不影响绑定与合并**（绑定的实质是**内容哈希**，见全局约束「变异证据绑定提交」与 **E59**）。本条**只登记事实**，不改任何门禁。

---

## 过程失误登记（E57–E58；**只登记与订正文本，不改代码**）

> 编号续计划 3（`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）的 **E56**（下一份计划的 E 编号从 **E59** 起）。本表与 D 表（D64–D74）同一纪律：逐条写清「位置 / 问题 / 处置」，并写明本轮对**证据承载文件**的实际改动面。
>
> **本轮改动面（已核实）**：对 `internal/**`、`cmd/**`、`tools/trace-mutate/sets/**`、`atlas-migrations/**`、`migrations/**`、`internal/tracearchive/**`、`go.mod`、`go.sum` 的改动**全部为零**；`tests/**` 只改了 `tests/tracesealer/options_test.go` 一处（测试隔离，见文末「测试隔离」栏）⇒ 按全局约束「变异证据绑定提交」，`p4-task-1` 的**四个集合全部重跑重出**，绑定提交 **`651d7c2b`**，四张权威表落盘于 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/`；**上一轮的绑定（若有）一律作废、不得引用**。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E57** | 上一轮任务 1 的交付报告（其结论为「4 张 `p4-task-1` 权威表 exit=0」）；评审的独立复核（遍历 5 个工作树的 `.superpowers/sdd/*/`、全仓 `find -name '*authoritative*'`、`git status --ignored`、`TMPDIR`） | **报告声称的产物在磁盘上不存在**：`grep -rl p4-task-1` 只命中本计划与台账里的**一段摘要**，该摘要**不含** `names=… ran=… diff=… SKIP=…` 自证串，也**不含**清单 / 被测文件 / 测试文件的 sha256 ⇒ 变异这个**结论**虽被评审独立复算为真（`--check` 7/6/2/3 = 18 条唯一定位；18 个杀手具体且用例名互不为子串），但**证据没有落盘成可复核工件**。而全局约束写明 CI **完全不跑** `go test`、**变异证据是唯一验证手段** ⇒ 证据不可复核时，任务 1 在制度上等于没有证据。**这是过程失误，不是技术结论错误**（变异本身为真） | ① **本轮把四张权威表原样落盘**到 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/`，文件名 `p4-task-1-<classify\|round\|exit\|options>-authoritative-at-651d7c2b.txt`，内容 = 工具的**完整 stdout 原样**（证据绑定头 `git rev-parse HEAD` / `git status --porcelain` / 工具·集合·清单·被测文件·测试文件 sha256、清单推导命令与比对、映射表、静态检查、逐条自证串、分类计数口径），**不得删改、不得只留摘要**；与表**同目录**另有四份名字清单 `p4-task-1-<集合>-names.txt`（使表头的「名字清单 sha256」在磁盘上有可 `sha256sum` 的对应文件）与四份 `--check` 定位校验输出 `p4-task-1-<集合>-check-at-651d7c2b.txt`。② **新立一条硬规则（今后所有轮次适用）**：**任何声称的产物必须先在磁盘上可 `ls`、可复算，报告里必须逐字给出其确切路径**（相对仓库根、可直接粘贴）；只在下游结论里写「已完成 / `exit=0`」而不给路径的，**视为未完成**。③ 该规则同时约束「抄进报告」这一动作：全局约束第 5 条要求的「把工具头部原样抄进报告」是**报告内的副本**，它**不替代**磁盘上的表 —— **两者都要有**。④ **已核实的事实（本轮实测，可重跑）**：`ls -l` 见 12 个文件；四张权威表逐一 `exit=0`；分类计数 **7 / 6 / 2 / 3 = 18**，**全部为断言红**，0 崩溃/超时/编译红、0 允许的绿（`EXPECTED_GREEN` 为空）、未登记的绿 0 条、**证据无效 0 条**；`--check` 因本轮**未改任何被测实现文件**而保持 A=7 / B=6 / C=2 / D=3 |
| **E58** | 本计划 **`:124`**、**`:1023`**、**`:1027`**、**`:931-945`**（行号为本轮订正**之后**的实时值，见本行 ⑥ 的行号位移栏）；`tools/trace-mutate/sets/p4-task-1-round.py` 的 `## 与计划文本的一致性` 文档串 | **四处「计划文本 ≠ 实现」的文本事实性缺陷**（评审点了 ②③，① 是评审同批指出的清单漏列，④ 由本轮测试隔离修正**新引入**）：① **`:124`** 任务 1 顶部文件清单只列 `classify_test.go` / `round_test.go` / `exit_test.go`，**漏列** `tests/tracesealer/options_test.go` —— 而 `:895` 的正文明确要求该文件（任务 1 步骤 7）⇒ 清单与正文自相矛盾。② **`:1023`**（集合 B 的 m02）计划只写「`KindSealFailed` 分支后补一句 `return res, err`」，**未给插入点** ⇒ 与集合里实际的匹配串**非逐字一致**（集合用 `res.SealFailed++` 紧接 `r.opts.Logf(` 做唯一定位，插入点在两者之间）。③ **`:1027`**（集合 B 的 m06）计划写 `+ res.UnknownFailed`，而实现 `runner.go:32-34` 的接收者是 **`r`**（`func (r RoundResult) Failed() int`）⇒ 计划里的串 `res.UnknownFailed` 在实现中**根本不存在**，照计划抄会「定位到 0 处」。④ **`:931-945`** 是本轮**自己造成**的：为修测试隔离给 `TestNewRunnerRejectsNonPowerOfTwoWindow` 补了 `MinAge: 3 * time.Hour`，若不同步计划里的该用例代码块，就会**新造**一处文本与实现不一致（与 ②③ 同类，只是方向相反） | ① **`:124` 就地补上** `tests/tracesealer/options_test.go`（**行数不变**）。② **`:1023` 就地写清插入点与匹配串**（`res.SealFailed++` 紧接 `r.opts.Logf(`，插入点在两者之间；并注明该分支日志与另两处 `res.Xxx++` 分支共用同一形态，故用这两段做唯一定位），使计划文本与集合**逐字一致**。③ **`:1027` 就地订正接收者**为 `r`，并把实现的**单行**求和式逐字写入（`r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed`），并注明 `Failed()` 在 `runner.go:32-34`。④ **`:931-945` 的用例代码块与本轮实现逐字同步**（补「MinAge 必须显式给值」的注释并补字段）；已实测该块与 `tests/tracesealer/options_test.go` **逐字节相同**（`diff` 为空）—— 这是**测试隔离修正在计划文本侧的必然同步**，不是新要求。⑤ **`tools/trace-mutate/sets/p4-task-1-round.py` 本轮一字未改（故意）**：它文档串里记的「两处**如实**改动（m02 插入点 / m06 匹配串）」在本行 ②③ 把计划订正为逐字一致之后**已变成历史陈述**；但该集合是**证据承载文件** —— 它的 sha256（`f0b0c059dc737039…`）正被本轮四张表绑定，改它就必须连带重跑四张表。⇒ **本轮保持该集合逐字节不变**，并把这条口径留在**此处**（而非集合内）：该文档串描述的分歧已由本计划文本订正**关闭**；后续轮次若因故重写该文档串，**必须**重跑并重记 `p4-task-1` 的全部四个集合。⑥ **行号位移（E54/E55 同一纪律）**：本轮在 `:931` 处插入 7 行注释 ⇒ **其后所有行号 `+7`**：评审所引的集合 B **m02 `:1016` → `:1023`**、**m06 `:1020` → `:1027`**；本表引用的一律是**订正后的实时行号**。已核实本计划**不含任何指向自身的行号引用**（`grep -cE '本计划 ?:[0-9]'` = **0**）⇒ 本次 `+7` 位移**不产生**新的行号自引用欠账（其余 `:NNN` 引用全部指向 `.go` / `.sh` / `deploy/` 等**其它文件**，本轮未改那些文件，故不受影响）。**⚠️ 一处需登记的例外**：`:1027` 行内引用的 `runner.go:32-34` 是**被实现文件**的行号（`runner.go` 本轮未改）⇒ 同样不受本次 `+7` 影响 |

### 测试隔离（E58 ④ 的技术内容，独立成栏以免与文本订正混淆；**本轮唯一的 `tests/**` 改动**）

- **缺陷**：`TestNewRunnerRejectsNonPowerOfTwoWindow` 原先只给 `Root` / `ZstdWindowMB` / `NewSealer`，让 `MinAge` 留零值去继承 `DefaultMinAge`。而 `Options.normalize` 里 **min-age 下界的判定（`options.go:99`）在窗口 2 的幂判定（`options.go:108`）之前** ⇒ 该用例**同时**依赖两条互不相干的契约，违反「每个承重属性一个顶层用例」的隔离要求。
- **实测（修前，HEAD `12c8bd65`；命令 = 集合 D 加 `--only m02`）**：`m02_默认min-age缩到30m` 判定 = 红，自证 **`names=3 ran=3 diff=0 PASS=1 SKIP=0 FAIL=2`**，杀死的用例 = `TestNewRunnerDefaultsMinAgeToThreeHours, TestNewRunnerRejectsNonPowerOfTwoWindow` ⇒ **多出一个非点名的连带牺牲者**，与集合 D 的 `REQUIREMENTS`「m02 的杀手 = `TestNewRunnerDefaultsMinAgeToThreeHours`」（单一点名）**不符**，违反全局约束第 4 条「杀手必须是具体的顶层测试函数名，不允许不点名」。
- **修法**：给该用例显式加 `MinAge: 3 * time.Hour`（严格大于 `MinAgeFloor = 1h`），把它钉回「只测窗口的 2 的幂」这一件事上。
- **实测（修后，HEAD `651d7c2b`；权威表 `p4-task-1-options-authoritative-at-651d7c2b.txt`）**：`m02` 自证 **`names=3 ran=3 diff=0 PASS=2 SKIP=0 FAIL=1`**，杀死的用例 = **`TestNewRunnerDefaultsMinAgeToThreeHours`**（**FAIL 2 → 1，只剩点名杀手**）；同表的 `m03_不校验2的幂` 仍是**红**且杀手仍是 `TestNewRunnerRejectsNonPowerOfTwoWindow` ⇒ **隔离没有把 m03 的杀手弄丢**，集合 D 仍是 `3 = 3 断言支撑`。
- **重绑**：`tests/tracesealer/options_test.go` 是**证据承载文件** ⇒ 四张 `p4-task-1-*` 权威表**全部重出**（表头「测试文件 sha256」四项中该项变为 `64d07b52eb0b863d…`；另三项与上一轮相同），旧表**作废、不得引用**。

---

## 过程失误登记与新增约定（E59–E60；**Task 1 收口轮**）

> 编号续本计划 **E58**（上一节的 E57–E58 是「声称产物未落盘」轮）。与 E57–E58、D 表（D64–D74）同一纪律：逐条写清「位置 / 问题 / 处置」，并如实登记本轮对**证据承载文件**的实际改动面。本轮的两条都来自任务 1 独立复审的三条**非阻断项**（复审结论是「阻断项已闭合、可现在合并」）。
>
> **本轮改动面（已核实）**：对 `internal/**`（含冻结包 `internal/tracearchive/**`）、`tests/**`、`cmd/**`、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` 的改动**全部为零**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）；`tools/trace-mutate/sets/**` 只改 `sets/p4-task-1-exit.py` 一处（②：`PACKAGES` 纳入薄壳包 `./cmd/trace-sealer/`，用**既有字段**、不新增字段名）；`docs/**` 只改本计划。因改了集合 ⇒ 按全局约束第 8 条四张 `p4-task-1-*` 权威表**全部重出**（新表 `*-at-<sha>.txt`，表末含 `table_exit=`；`--check` 四份同理含 `check_exit=`，见 E60），`-at-651d7c2b` 的四张表与四份 `--check` **作废、不得引用**（文件保留在证据目录里备查）。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E59** | 本计划 **`:49`**（本轮订正**前**的实时值；订正为**原地改写**——该行仍是 1 行 ⇒ 订正**后**的实时值同样是 `:49`） | **绑定口径自相矛盾**：`:49` 原文的加粗句「**任何后续新提交都会使此前所有绑定失效**」与计划 3（`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）的 **E56** 直接冲突 —— E56 的口径是「绑定跟**内容**走：绑定的实质是**内容哈希**、表头 `git rev-parse HEAD` 只记**产出时点**、**纯计划文档 / 纯证据文本提交不使绑定失效**」。按 `:49` 的字面口径，本计划从 E54 起那些「只改计划文本的提交」都会把上一轮的绑定判成失效（而实际执行口径是**不失效**），且会推出「证据表永远无法在任何 tip 上成立」的结论（表落盘本身也是提交）。触发点 = 任务 1 独立复审的非阻断项 ③ | **把 `:49` 的「提交」改写为「实现 / 测试 / 集合内容的变更」**，并把「纯文档/纯证据提交不使绑定失效」写进同一句，与计划 3 **E56** 显式对齐。**改前**（逐字）：`**任何后续新提交都会使此前所有绑定失效** ⇒ ①…`；**改后**（逐字）：`**任何后续「实现 / 测试 / 集合」内容的变更都会使此前所有绑定失效**（绑定的实质是**内容哈希**，表头的 \`git rev-parse HEAD\` 只记**产出时点**；**纯计划文档或纯证据文本的新提交不使绑定失效** —— 见计划 3 的 **E56** 与本计划 **E59**）⇒ ①…`。该行末尾的 ①②③ 三条执行纪律与其余字面**未动**。**无行号级联（已实测）**：改前 `grep -cE '本计划 ?:[0-9]' docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md` = **0**；且本轮只在 `:49` 原地改写、在文件末尾追加本节 ⇒ 全部既有行号引用（E58 的 `:124` / `:1023` / `:1027` / `:931-945` 等）**零位移** |
| **E60** | 本计划的证据制度（全局约束第 8 条 + 任务 1 步骤 11 的命令块）与四张 `p4-task-1-*` 权威表；触发证据 = 本轮实测 `grep -rc 'table_exit\|check_exit' .superpowers/sdd/2026-10-08-trace-sealer-deployment/` ⇒ **12 个文件全为 0**（`TOTAL=0`） | **报告里的数字在工件里查不到**（同族于 E57「声称的产物未落盘」）：`trace_mutate.py` 的 stdout **不自印退出码**（它只保证「0 = 全部转红 + 静态检查通过 + 清单推导一致 + 证据有效」）。原先四张表（`-at-651d7c2b`）里 `table_exit=` / `check_exit=` **一次都没出现**，而交付报告声称「四表 `table_exit=0` / `check_exit=0`」⇒ 这句话**无法由工件自证**，读者只能选择相信报告。这不是工具缺陷，而是**表落盘时的记录口径缺失** | **新增约定（此后每张权威表、每份 `--check` 输出都必须遵守）**：① **权威表的表末必须记录 `table_exit=`、`--check` 输出的表末必须记录 `check_exit=`**，取的是**工具进程的真实退出码**、紧跟在工具命令之后写：`python3 tools/trace-mutate/trace_mutate.py --repo . --manifest <清单> --mutation-set <集合> > <表>.txt 2>&1; echo "table_exit=$?" >> <表>.txt`（`--check` 同理记 `check_exit=$?`）；② **报告里引用的任何退出码或计数都必须能在工件（表）内查到** —— 退出码 = 表末的 `table_exit=` / `check_exit=` 行，计数 = 表内「自证串」/「计数口径」/「合计」段；**查不到就不得声称**。③ **表内其余段一律原样保留**（逐条自证串、映射表、静态检查、sha256 段），本约定**只追加、不改写**工具输出。④ **不改工具**：`trace_mutate.py` 是被权威表绑定的**证据承载文件**，「字段名不许自创」的纪律同样适用于集合 ⇒ 本条只用 `echo` 追加，不新增字段、不改工具代码。⑤ **本轮已按此执行**：四张 `p4-task-1-*-authoritative-at-<sha>.txt` 与四份 `p4-task-1-*-check-at-<sha>.txt` 全部重出，表末均带 `table_exit=` / `check_exit=`。**已知残余（如实登记）**：任务 1 步骤 11 的命令块**仍是没有 `echo` 的旧形态** —— 本轮**未**改它（避免把「新增约定」扩写成对执行步骤的重写）；执行者以本条为准，后续轮次若重排步骤 11，应把 `echo` 一并补进命令块 |

## 过程失误登记与新增约定（E61；**Task 2 收口轮**）

> 编号续本计划 **E60**。与 E57–E58、E59–E60、D 表（D64–D74）同一纪律：逐条写清「位置 / 问题 / 处置」，并如实登记本轮对**证据承载文件**与**计划文本**的实际改动面。
>
> **本轮改动面（已核实）**：`internal/tracearchive/**`（冻结包）、`migrations/**`、`go.mod`、`go.sum`、`tools/trace-mutate/trace_mutate.py` 的改动**全部为零**；实现面只动 `internal/tracesealer/{alert.go(新建),options.go,runner.go}` 与 `tests/tracesealer/alert_test.go(新建)`；集合面新增 `tools/trace-mutate/sets/p4-task-2-alert.py`、并按本表 ③ 重新锚定 `tools/trace-mutate/sets/p4-task-1-round.py` 的 m02。本轮共跑 5 个集合（`p4-task-2-alert` + `p4-task-1` 全部四个）的 `--check` 与权威表，**5 个 `check_exit=0` / 5 个 `table_exit=0`**，25 条变异**全部断言红**（0 崩溃/超时/编译红、0 未登记的绿、0 证据无效）。**③ 的连带后果**：按 E58⑤ 的口径，改 `p4-task-1-round.py` 必须重跑并重记 `p4-task-1` 的**全部四个**集合 ⇒ 本轮已重出四表四 check（`-at-e73a3091`），上一轮的 `-at-d05978f6` 版本按全局约束第 49 条作废（绑定是内容哈希）。**E60 口径的本轮执行**：五张表与五份 check 的表末都带 `table_exit=` / `check_exit=`。**行号位移（E54/E55/E58⑥ 同一纪律）**：本轮在 **`:1102`** 之前净增 1 行（把一个「修改：」拆成两行）⇒ **其后所有行号 `+1`**：任务 2 步骤 3 正文 **`:1193` → `:1194`**、步骤 6 的 `git add` 块 **`:1220-1221` → `:1221-1222`**；本表引用的一律是**订正后的实时行号**（并在括号里给出订正前的值）。已复核本计划**不含任何指向自身的行号引用**（`grep -cE '本计划 ?:[0-9]'` = **0**）⇒ 本次 `+1` 位移不产生新的自引用欠账；任务 1 的 `:1023`（集合 B 的 m02）在本轮**之前**，不受位移影响。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E61①** | 本计划 **`:1102`**（任务 2 的文件清单）与 **`:1221-1222`**（步骤 6 的 `git add` 命令块；订正前为 `:1220-1221`，见本节行号位移栏） | 两处都**漏列** `internal/tracesealer/options.go`，而 **`:1194`**（步骤 3 正文；订正前为 `:1193`，见本节行号位移栏）明确要求「新增 `Options.Slog *slog.Logger`」—— Go 的字段只能声明在 `Options` 所在的文件里 ⇒ 照清单执行**无法**产出要求的 `Options.Slog`（清单与正文自相矛盾，与 **E58①** 同族）。同一行还有一处措辞不实：`runner.go` 的改动写作「新增 `Summarize`」，而 `Summarize` 按本任务的接口块与 `alert.go` 的定义**只能是新建文件**，`runner.go` 只是它的调用方 | **`:1102` 就地补一行**「修改：`internal/tracesealer/options.go`（新增 `Options.Slog`）」，并把 `runner.go` 那句的「新增 `Summarize`」订正为「接入 `Summarize` —— 它的本体在新建的 `alert.go`」（**净增 1 行**，其后行号 `+1`）；**`:1220-1221` 的命令块就地补上** `internal/tracesealer/options.go`。本轮提交 `959f5441` 已按订正后的清单落盘（`git show --name-only` 含该文件） |
| **E61②** | 本计划 **`:1194`**（任务 2 步骤 3；订正前为 `:1193`） | **口径自相矛盾**：「（nil ⇒ `slog.Default()`）」与同一句的「`Logf` 仅在 `Slog == nil` 时使用」**不能同时按字面成立**：`Options.normalize`（任务 1）把 `Logf` 默认成 `log.Printf` ⇒ 正常路径上 `Logf` 恒非 nil ⇒ 若 `Slog == nil` 就回落 `Logf`，则 `slog.Default()` 永远到不了；若 `Slog == nil` 就回落 `slog.Default()`，则 `Logf` 永远用不上（等于删掉了任务 1 的注入面）。**实测**：本轮实现按三级优先级取值，`resolveLogger` 的第三级确实是**防御分支**（走 `NewRunner` 的正常路径到不了，因为 normalize 已保证 `Logf` 非 nil） | **`:1194` 的就地订正**：把「（nil ⇒ `slog.Default()`）」改写为「（与 `Logf` 都缺省 ⇒ `slog.Default()`；`Logf` 由 `normalize` 默认成 `log.Printf` ⇒ 正常路径上 `Slog == nil` 就是走 `Logf` —— **E61②**：三级优先级 `Slog > Logf > slog.Default()`）」，使计划文本与本轮实现（`runner.go` 的 `resolveLogger`）**逐字一致**；实现侧把第三级的「正常路径到不了」这一事实写在 `resolveLogger` 的注释里，并为它加了两条静态检查（`Slog优先于Logf` / `缺省回落slog.Default`） |
| **E61③** | 本计划 **`:1023`**（任务 1 集合 B 的 m02）与 `tools/trace-mutate/sets/p4-task-1-round.py` | **跨任务耦合**：任务 1 的 m02 用「`res.SealFailed++` 紧接 `r.opts.Logf(`」做唯一定位，而任务 2 步骤 3 要求把**同一个分支**的日志从 `r.opts.Logf(...)` 换成 slog ⇒ 该匹配串在任务 2 的实现里**不再存在**（本轮实测 `grep -c 'r.opts.Logf(' internal/tracesealer/runner.go` = **0**）⇒ 集合 B 会以 `PATTERN-MISS`（`定位到 0 处`）失败，且任务 12 的「最终 HEAD 全量重跑」必然踩到。原集合文档串里那句「匹配串 `res.SealFailed++` 紧接 `r.opts.Logf(`」也随之失真 | **集合 B 的 m02 就地重新锚定**（**语义一字未改**）：匹配串改为「`case KindSealFailed:` 紧接 `res.SealFailed++`」（该分支的起始两行、各自唯一，在任务 2 **前后**的实现上都唯一、都红成 `TestRoundSealsEveryEligiblePart`——两侧均已实测），并在集合文档串里按 E58⑤ 的纪律如实登记这次改动与「必须重跑四个集合」的后果；本计划 **`:1023` 同步订正**为新的匹配串描述并指向本条。**如实登记**：改集合即改证据承载文件 ⇒ 本轮重跑了 `p4-task-1` 全部四个集合（见本轮改动面） |

---

## 过程失误登记与新增约定（E62；**Task 2 行为证据收口轮**）

> 编号续 **E61**。与 E57–E58、E59–E60、E61 同一纪律：逐条写清「位置 / 问题 / 处置」，并如实登记本轮对**证据承载文件**与**计划文本**的实际改动面。

> **本轮改动面（已核实）**：`internal/**`（含冻结包 `internal/tracearchive/**`）与 `cmd/**` **零改动**；`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` **零改动**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）；测试面只改 `tests/tracesealer/alert_test.go`（新增三条 `Round` 的**行为**用例）；集合面只改 `tools/trace-mutate/sets/p4-task-2-alert.py`（docstring 订正 + `REQUIREMENTS` 新增第 10 条）；清单 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/p4-task-2-alert-names.txt` **6 → 9**。因改了**测试与集合内容** ⇒ 按 **E59**（绑定跟内容哈希走）**重跑重绑**：新表 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/p4-task-2-alert-authoritative-at-df0b9d06.txt`（表末 `table_exit=0`）与 `p4-task-2-alert-check-at-df0b9d06.txt`（表末 `check_exit=0`）；`-at-e73a3091` 版本按内容哈希口径**作废**，但**留档不删**。**另（同一口径的连带面）**：`tests/tracesealer/alert_test.go` 同时出现在**全部** `p4-task-1-*` 表的「测试文件 sha256」段（工具对包内所有 `*_test.go` 取哈希）⇒ 该文件的变更使那四张表的绑定一并失效，因此四张 `p4-task-1-*` 表与四份 check 也**已重出**（`-at-ee4a60e4`，八个产物 `table_exit=0` / `check_exit=0`；分类计数 7/6/2/3 = 18 条全部断言红，0 崩溃/超时/编译红），`-at-e73a3091` 同样留档不删。分类计数 **7 = 7 断言支撑红 + 0 崩溃/超时/编译红 + 0 允许的绿**，未登记的绿 0、证据无效 0；pristine 侧 `names=9 ran=9 diff=0 PASS=9 SKIP=0 FAIL=0`。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E62** | `tools/trace-mutate/sets/p4-task-2-alert.py` 的 docstring（原句：「承重的运行时证据是 Task 2 的人工核验……该核验用的是**临时**测试文件，未进提交，故不在此作为杀手登记」）与它的 `REQUIREMENTS`；触发 = 计划 4 任务 2 的独立评审（结论「可合并」）留下的那条实质缺口，发起者裁定**成立、必须修** | `runner.go` 的「按 `Alert.Severity` 打 WARN/ERROR 告警线、`reason` 取自 `Alert.Reason`」是一条**运行时行为**要求，而原 9 条 `REQUIREMENTS` 只覆盖 `Summarize`、心跳（静态）、`Slog`/`Logf` 接线（静态）⇒ 这条行为在册**没有任何证据**；集合 docstring 自述靠**未进提交的临时测试**核验。按全局约束第 7 条「有要求无条目 ⇒ 整轮不合格」，这必须升级为修复项（「临时测试已验」不构成证据：它不在任何权威表的名字清单里，第三方无法复算） | ① 在 `tests/tracesealer/alert_test.go` 新增三条注入 `Options.Slog` 捕获 sink 的**行为**用例：`TestRoundTooManyRowsEmitsErrorAlertLine`（`slog.LevelError` + `reason=too_many_rows`）、`TestRoundRetryableFailureEmitsWarnAlertLine`（`slog.LevelWarn` + `reason=round_failed`）、`TestRoundCleanRoundEmitsOnlyHeartbeat`（**零**告警线 + 心跳恰好一次）⇒ 断言的是**具体级别与具体 reason 串**，不是「有输出」；② 它们**不需要新增变异**：现有变异逐条杀死它们（`m01` ⇒ 全绿轮出现告警线、`m03` ⇒ 可重试线变 ERROR、`m04`/`m05` ⇒ 永久失败的级别/reason 变），实测 `FAIL` 分布与重出表一致（3/1/2/2/2/1/1）；③ 集合 docstring 删去「临时测试」口径并新增「运行时告警线的在册行为证据」一节，`REQUIREMENTS` 新增第 10 条（运行时行为 → `m01/m03/m04/m05`）；④ 名字清单 6 → 9；⑤ 按 E60 重出表与 `--check`（见本轮改动面）。**如实登记**：唯一**没有变异杀手**的断言是「心跳恰好一次」（它需要 `runner.go` 的变异，而该集合 `TARGET` 是 `alert.go`）⇒ 它由该集合的两行静态检查 + 一次人工实测承载（新增第二次心跳 ⇒ 该用例在 `require.Len(beats, 1)` 处转红，已实测 `--- FAIL`；`internal/tracesealer/runner.go` 还原后 sha256 与改前逐字节一致）；该集合的两行静态检查**本身可失败**亦已实测：改动缩进（把心跳挪进 `if`）或重命名消息串 ⇒ 顶层锚定由命中变未命中；新增第二次心跳 ⇒「至多一次」由未命中变命中 —— 按本轮裁定**不另建元测试**。**行号纪律**：本节**追加在文件末尾**，任务 2 步骤 1/4/5 的三处订正**逐行原地改写、行数不变** ⇒ 既有行号引用**零位移**；改前 `grep -cE '本计划 ?:[0-9]' docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md` = **0**，改后复测仍为 **0**。**如实登记（文本一致性）**：任务 2 步骤 1 的用例代码块仍只抄录 6 条 `Summarize` 用例（上面已就地改成「另有 3 条行为用例，见 E62」），步骤 4 的「新增 6 个顶层用例」已就地改为 9 条 —— **代码块未逐字抄录那三条是有意的**：逐字抄进去会让其后所有行号 `+100` 以上，冲掉 E61 记的 `:1102`/`:1194`/`:1221-1222`；因此**在册源以 `tests/tracesealer/alert_test.go` @ `df0b9d06` 为准** |

### 新增约定（E62 起，**今后所有轮次适用**）

1. **行为要求必须由在册行为测试承载**：任何描述「运行时会打出什么 / 会调用什么 / 会返回什么」的要求，必须在某张权威表的**名字清单**里找得到承载它的顶层用例；只有静态检查、没有用例的要求，视为**未被验证**。
2. **禁止以「临时测试已验」作为证据**：未进提交、因而不在任何权威表名字清单里的测试，等于没有证据；写进报告或集合 docstring 的「人工核验过」不改变这一判定（同族于 **E57**「声称的产物必须可复算」）。
3. **静态检查只承载「存在 / 不存在」类要求**：结构不变量、字面量取值、产物文本可以用 `CHECKS` 钉住；但**行为**要求不得用静态检查替代 —— 静态检查通过只证明「这行文字在」，不证明「这条路径真的会被走到」。**例外（本轮已用）**：当被要求的行为与某个结构不变量**逐字等价**（例如「心跳恰好一次」≡「消息串出现一次且不在任何 `if` 内」）时，`CHECKS` 可以承载该要求，但必须同时给出该检查**可失败**的证明（改动缩进 / 重命名 / 新增第二次即红）。

---

## 过程失误登记与新增约定（E63；**Task 3 收口轮**）

> 编号续 **E62**。与 E57–E62 同一纪律：逐条写清「位置 / 问题 / 处置」。

> **本轮改动面（已核实）**：实现面 `internal/tracesealer/{rootguard.go(新建),options.go,runner.go}` 与 `cmd/trace-sealer/main.go`；测试面 `tests/tracesealer/{rootguard_test.go,loop_test.go}(均新建)`；集合面新增 `tools/trace-mutate/sets/p4-task-3-rootguard.py` 与 `p4-task-3-loop.py`。`internal/tracearchive/**`（冻结包）、`migrations/**`、`go.mod`、`go.sum`、`tools/trace-mutate/trace_mutate.py` 的改动**全部为零**。**跨任务连带（E59③ 的执行）**：共享测试目录 `tests/tracesealer/` 新增两个 `*_test.go` ⇒ 工具证据头里的「测试文件 sha256」段对**所有**把该目录列入 `PACKAGES` 的集合都变了 ⇒ `p4-task-1` 的四个集合与 `p4-task-2-alert` **一并重跑重出**（`p4-task-1-round`/`options` 另因 TARGET `runner.go`/`options.go` 变更、`p4-task-1-exit` 另因 `main.go` 变更而必须重出）。本轮共跑 **7 个集合**（两个新集合 + `p4-task-1` 四个 + `p4-task-2-alert`）的 `--check` 与权威表：**7 个 `check_exit=0` / 7 个 `table_exit=0`**，40 条变异 = **39 断言红 + 1 允许的绿 + 0 崩溃/超时/编译红 + 0 未登记的绿 + 0 证据无效**。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E63** | 本计划 Task 3 的文本（`:1608`、`:1617`、`:1640`、`:1652-1656`、`:1660-1663`、`:1672-1675`、`:2960`）与两个新集合 `tools/trace-mutate/sets/p4-task-3-{rootguard,loop}.py`；触发 = Task 3 实现与变异收口实测 | **八处「计划文本 ≠ 实测」的缺陷**：① `:1617` 的 `r.logf("[trace-sealer] ERROR 本轮失败，%s 后重试: %v", …)` 在实现里**不存在这个符号**（Task 2 把日志统一成 slog 后 `Runner` 没有 `logf` 方法）⇒ 照抄**编译不过**；② `:1608` 错字「心（每轮一条汇总）」缺「跳」；③ `:1640` 的「新增 6 + 5 = 11 个顶层用例」与实测 **7 + 7 = 14** 不符；④ `:1652`（m04）点名的两个杀手 `TestRootGuardCheckDetectsIdentityChange` 与 `TestRootGuardBaselineIsNonEmpty` **都注入 identity 闭包**、根本不经过 `covers`，且本机是 darwin（无 `/proc/self/mountinfo`）⇒ 真实 `Identity` 走 `fallbackIdentity` ⇒ m04 **没有任何杀手**；⑤ `:1653`（m05）点名的 `TestRootGuardBaselineIsNonEmpty` 注入的 identity **成功**返回 ⇒ 走不到 m05 动的那个错误分支，同时 `TestRootGuardCheckReportsVanishedRoot` 与 `TestRootGuardBaselineIsNonEmpty` 在原六条变异里**没有杀手**（m03 只动身份比对）；⑥ `:1672`（loop m04）点名的 `TestLoopStopsOnContextCancel` 实测在该变异下**仍绿**，而 Task 1 的 `TestRoundCancellationStopsBeforeNextSeal` **不在本集合的名字清单里**（该集合的 `-run` 只跑 `loop_test.go` 的用例）⇒ m04 在本集合里没有杀手；`TestLoopRepeatsRoundsOnPollInterval` 与 `TestLoopNeverOverlapsRounds` 在原五条变异里同样**没有杀手**；⑦ `:1673`（loop m05）登记 `EXPECTED_GREEN` 的理由是「`RoundResult.RootUnavailable` 无用例断言」—— 按 **E62** 的口径这正是「行为要求无在册证据」，应当补用例而不是登记弱覆盖；⑧ `:2960` 的弱覆盖清单 ② 与 ⑦ 同源，且 `:1660-1662` 的 `CHECKS` 只有两行 ⇒ m04 那条不可达分支连**存在性**旁证都没有 | ① 就地改写 `:1617` 为 `r.logger.Error("trace-sealer 本轮失败，下一个周期重试", "err", err, "poll_interval", r.opts.PollInterval.String())`（**行数不变**，与 Task 2 的单一 slog 路径一致；该行语义 —— 轮次级失败打 ERROR —— 未变）；②③ 就地改写（行数不变）；④ 就地订正 `:1652` 的杀手栏为「无，登记 `EXPECTED_GREEN`」并写明 darwin 不可达的理由，另在 `:1663` **新增一行 `CHECKS`** `covers必须特判根挂载点`（`(?m)^\tif mp == "/" \{$` present）承载该分支的**存在性**；并在**临时克隆**（`/tmp/p4t3-chk`，工具 `--repo` 指向它）里把三行静态检查各改坏一次（补 `os.MkdirAll` / 特判条件恒假 / 改 `Options.Identity` 字段名），实测三行**各自转红**且工具以「静态检查失败 ⇒ 整轮失败」退出 **1**；⑤⑥ 就地订正两处杀手栏，并在两张表里**各新增两行变异**（rootguard：m07 `Check()` 的 stat 失败静默放行、m08 `Baseline()` 返回 `g.root`；loop：m06 跑完一轮就退出、m07 轮次并行）⇒ 其后实时行号 `+5`，但本计划不含指向 `:1233` 之后行号的任何自引用（改前 `grep -cE '本计划 ?:[0-9]'` = **0**，改后复测仍为 **0**）⇒ **零级联**，且 `:1233` 之前的行本轮**一行未动**；⑦⑧ 在 `tests/tracesealer/rootguard_test.go` 新增 `TestRootGuardRejectsWhenBaselineCannotBeTaken`、在 `tests/tracesealer/loop_test.go` 新增 `TestLoopStopsSealingAfterCancel` 与 `TestLoopReportsRootUnavailable`（三条都是**在册行为用例**，进各自名字清单）⇒ `p4-task-3-rootguard` 的 m05 与 `p4-task-3-loop` 的 m04/m05 三个变异都拿到在册杀手，loop 集合的 `EXPECTED_GREEN` **清空**；`:2960` 就地改写。**如实登记（残余弱覆盖，唯一一处）**：`p4-task-3-rootguard` 的 **m04** 是唯一登记进 `EXPECTED_GREEN` 的变异（darwin 不可达；在 Linux 上它会杀掉依赖真实 `Identity` 的用例），其存在性由静态检查行承载（**E64① 已订正**：m04 由包内行为用例 `TestCoversRootMountCoversPathsUnderRoot` 承载、为**断言红**，`EXPECTED_GREEN` 已清空；原登记的两半理由均已证伪） ⇒ 本轮 40 条变异 = **40 断言红 + 0 允许的绿 + 0 崩溃/超时/编译红 + 0 未登记的绿 + 0 证据无效**（**E65(a) 订正**：原记「39 断言红 + 1 允许的绿」是 **E64① 订正之前**的 `-at-0240d4b5` 事实；订正后 `p4-task-3-rootguard` 的 8 条 = **8 断言红 + 0 允许的绿**、`EXPECTED_GREEN` 两项皆空，见 `-at-055c28c1` / `-at-12bdf706` 与 `-at-d1716d21`）。**如实登记（隔离探针）**：`TestLoopStopsOnContextCancel` 仍**没有变异杀手**（它断言「取消后干净返回」与「返回后没有遗留后台轮次」，而 m07 的泄漏 goroutine 不保证在 50ms 采样窗内留下痕迹）⇒ 按「隔离探针」口径登记，其两半的**可失败性**已人工实测：把 `Loop` 的 `case <-ctx.Done():` 分支改成 `return context.Canceled` ⇒ 该用例在 `require.NoError` 处转红（**E64② 复测订正**：只改 `select` 里的 `case <-ctx.Done():` **不**转红 —— 本用例实际生效的退出路径是同函数里 `if ctx.Err() != nil { … return nil }` 那一条，它遮蔽了前者；按该分支实测才转红，见集合 `COVERAGE_GAPS`）；在该分支注入 `go func() { for { r.Round(ctx) } }()` ⇒ 该用例在 `require.Equal(t, n, r.rounds())` 处转红（实测 `expected: 11 / actual: 25305`）；两次都按 `cp` 还原，`internal/tracesealer/runner.go` 还原后 sha256 = `d9f6630b475aab00e3020459f96aaf289795e342b5a0c59e39efbe87dbd4c5e5`（与提交版本逐字节一致），`git status --porcelain` 为空。**如实登记（文本一致性）**：步骤 3 的 `rootguard.go` 代码块与本轮实现**逐字节相同**（`diff` 为空）；步骤 1 的 `loop_test.go` 代码块**未逐字同步**（计划的示意块自己写着测试辅助要执行者自建，本轮按 E63④ 的理由把轮次计数改挂在 `loopHarness` 的身份探针上 —— 计划的 `f.rounds()` 计法在「根目录不可用」那一轮恒为 0）；**在册源以 `tests/tracesealer/{rootguard,loop}_test.go` 与两个集合文件 @ `0240d4b5` 为准**。 |

---

## 过程失误登记与新增约定（E64；**Task 3 评审修订轮**）

> 编号续 **E63**。与 E57–E63 同一纪律：逐条写清「位置 / 问题 / 处置」。本轮**只做 Task 3 收口**，不做 Task 4+。

> **本轮改动面（已核实）**：实现面 `internal/tracesealer/**` **零改动**（只复核 sha256：`rootguard.go` = `ff0c9e85b8406b72f4221b8ecd82079e5aafa90be77a6f9d68695b1b34923699`、`runner.go` = `d9f6630b475aab00e3020459f96aaf289795e342b5a0c59e39efbe87dbd4c5e5`，与 `0240d4b5` 逐字节一致）；冻结包 `internal/tracearchive/**`、`cmd/**`、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` **零改动**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）；测试面**新增** `internal/tracesealer/covers_test.go`（包内，1 条顶层用例）；集合面改 `tools/trace-mutate/sets/p4-task-3-rootguard.py` 与 `p4-task-3-loop.py`；清单面 `p4-task-3-rootguard-names.txt` 7 → 8 条；证据面新增四份文件（两表 + 两 `--check`，`-at-055c28c1` / `-at-12bdf706`），旧表 `-at-0240d4b5` **留档不删**。
>
> **跨任务连带自查（如实报告）**：遍历全部集合的 `PACKAGES`，**没有任何集合**含 `./internal/tracesealer/`（也没有 `./...` 一类父目录）⇒ 新增的包内 `*_test.go` **不落进**任何其它集合的「测试文件 sha256」段；另对 `p4-task-1-classify/round/exit/options` 与 `p4-task-2-alert` 五张 `-at-0240d4b5` 的表**逐行复算**其绑定的测试文件与被测文件 sha256（每张 8 条绑定行）⇒ **内容全未变、绑定仍有效**，故未重出这五张表。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E64** | ① 本计划 Task 3 步骤 6 的 A 集合表里 m04 那一行的「杀手」栏；② 本计划全局「已知的、被如实登记的弱覆盖」第 ② 条；③ 计划 **E63** 行末「如实登记（残余弱覆盖，唯一一处）」；④ `tools/trace-mutate/sets/p4-task-3-rootguard.py` 的 `EXPECTED_GREEN`（唯一一条 = m04）与其 `CHECKS` 的 why、`REQUIREMENTS` 的 note、docstring 第 4 条；⑤ 计划 **E63** 行末「如实登记（隔离探针）」把「无变异杀手」只写在**计划文本**里；⑥ `tools/trace-mutate/sets/p4-task-3-loop.py` 的 **m02**；触发 = 计划 4 任务 3 的独立评审（结论「修复后可合并」）的 **B1**，与发起者裁定的 **N1 / N2** | **B1（必做）**：那条「允许的绿」掩盖了一条**未被验证的行为要求**。「根挂载点必须覆盖其下路径」是**行为**要求，而它当时只由静态检查 `covers必须特判根挂载点`（存在性）承载 ⇒ 违反 **E62**。登记 m04 为 `EXPECTED_GREEN` 的理由**两半都错**：① `covers` 是**纯字符串函数**、不读 `/proc/self/mountinfo`，而计划 Q3 明文允许「确需触达未导出符号 ⇒ 放包内测试」⇒ 它**不是**平台不可达；② 在册清单 7 条里唯一传真实 `Identity` 的三条（`/`、相对路径、缺失目录）**都在调用 `Identity` 之前就返回错误** ⇒ 在 Linux 上 m04 **同样不会红**，「在 Linux 上会杀掉依赖真实 `Identity` 的用例」是**无据子句**。**N1（必做）**：`TestLoopStopsOnContextCancel` 的「隔离探针」登记**只写在计划文本**里，集合的 `EXPECTED_GREEN` / `CHECKS` / `REQUIREMENTS` 一行都没有 ⇒ 工件里查不到（同族于 **E57**）。**N2（必做）**：loop 的 m02 在 logger 调用**之前插入** `return err`，造出一条**不可达语句** —— Go 编译器放行、`go test` 的默认 vet 子集（不含 `unreachable`）也放行 ⇒ 侥幸通过，且不是「放宽 / 删除判定」的可解释形态 | **B1**：① 按 Q3 在**包内**新增 `internal/tracesealer/covers_test.go` 的 `TestCoversRootMountCoversPathsUnderRoot`（断言 `covers("/", "/tmp/x") == true`，另有 `covers("/","/")`、`covers("/a","/a/b")` 与不相干路径为 false 等边界），并把 `./internal/tracesealer/` 加进 `PACKAGES`、`MANIFEST_FILES` 与 `MANIFEST_DERIVATION` 扩到两个文件（名字清单 7 → 8）—— 该测试文件因此进证据绑定的「测试文件 sha256」段；② **删除** `EXPECTED_GREEN` 中 m04 那一条（`EXPECTED_GREEN` 清空），把 `CHECKS` 的 why、`REQUIREMENTS` 的 note、docstring 第 4 条与上面 ①②③ 三处计划文本一并改写为**事实陈述**（m04 由包内行为用例承载、为**断言红**）；③ 按 **E59** 重跑重绑：`p4-task-3-rootguard` 8 条**全断言红**、`p4-task-3-loop` 7 条**全断言红**（表 `-at-055c28c1` / `-at-12bdf706`，均 `table_exit=0`，两份 `--check` 均 `check_exit=0`）。**N1**：把该登记搬进**集合工件内**并复用**既有字段名** `COVERAGE_GAPS`（与 `task-5.py` 同形；不写进 `EXPECTED_GREEN`，因为它的键被工具当作**变异名**，也不放进 `REQUIREMENTS`，因为「mutations 与 checks 都为空」会被工具判 GAP ⇒ 整轮失败），另在该集合「ctx 取消后不得再开新块」的 `REQUIREMENTS` note 里复述实测结论（该 note 会进权威表）：两半的可失败性本轮**重新实测**（**E66①：以下 `loop_test.go:NNN` 均为「改动后行号」**）—— ① 退出分支 `return nil` 改成 `return ctx.Err()` ⇒ FAIL 于 `loop_test.go:171`；② 同一分支注入泄漏 goroutine ⇒ FAIL 于 `loop_test.go:174`（expected 11 / actual 25341，连跑 3 次 25341 / 25816 / 25735）。**N2**：把 m02 改成「把『记日志后继续』**整条语句**替换成 `return err`」——语义仍是「环境级失败 ⇒ 退出」，不留任何不可达代码，`go vet ./internal/tracesealer/ ./tests/tracesealer/` 全量通过，m02 仍为**断言红**（杀手 `TestLoopKeepsRunningWhenRootIsUnavailable`）。**如实登记（本轮新发现的一处竞态）**：loop 的 m07 会**偶发**把 `TestLoopStopsOnContextCancel` 一起打红（本轮两次工具运行都出现，上一轮 `-at-0240d4b5` 的表里没有，单项单跑 5 次里 1 次红）⇒ 它是**竞态**而非杀手，**不得**据此把该用例登记成 m07 的杀手；该事实已写进集合的 `COVERAGE_GAPS` 与 m07 条目，登记口径由「无变异杀手」订正为「无**可依赖的**变异杀手」 |

### 新增约定（E64 起，**今后所有轮次适用**）

1. **不允许用「本平台不可达」为在册清单从未触达的路径开脱**：若某条变异据称在某些平台上不可达，**必须给出在册证据**（哪个用例、哪一行、因哪条平台事实而不可达）；**给不出证据的一律按可杀处理并补测试**，且补在能**确定性**触达它的那一层 —— 纯函数（本例的 `covers`）就该由包内用例直调，而不是靠间接路径「碰巧走到」。
2. **「无变异杀手」的登记必须落在集合工件内**，不能只写在计划文本里：登记到集合里一个**既有的、工具读不到的**人类可读字段（本轮用 `COVERAGE_GAPS`，**不新造字段名**），写清「位置 / 断言什么 / 为什么没有杀手 / 可失败性由谁承载 / 谁来补」；计划文本可以复述，但**不得**成为唯一落点。

---

## 过程失误登记与新增约定（E65；**Task 4 收口轮**）

> 编号续 **E64**。与 E57–E64 同一纪律：逐条写清「位置 / 问题 / 处置」。

> **本轮改动面（已核实）**：实现面 `internal/tracesealer/lock.go`（**新建**，sha256 `2835c163e3dda8d1…`）、`internal/tracesealer/runner.go`（`dd80de132255fafb…`）、`cmd/trace-sealer/main.go`（`d83e628462358fcc…`）；测试面 `tests/tracesealer/lock_test.go`（**新建**，`7bd98c1223b58280…`，8 条顶层用例）与 `tests/tracesealer/concurrent_test.go`（**新建**，`6ec687db153fbdeb…`，5 条）；集合面**新增** `tools/trace-mutate/sets/p4-task-4-lock.py`（`c3bbbeaefdea569e…`）与 `p4-task-4-runner.py`（`8a830be68383c7c0…`）。**七个既有集合因绑定失效而重跑重出**（集合文件**一字未改**）：`p4-task-1-{classify,round,exit,options}`、`p4-task-2-alert`、`p4-task-3-{rootguard,loop}`。`internal/tracearchive/**`（冻结包）、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` 的改动**全部为零**（`git diff --stat 00e5c1ba..HEAD -- internal/tracearchive/` 与同一范围的 `migrations atlas-migrations go.mod go.sum` 均**无输出**）；`tools/trace-mutate/trace_mutate.py` **未动**，sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`。

> **跨任务连带（E59③ 的执行，先自查后报告）**：`grep -n 'PACKAGES' tools/trace-mutate/sets/p4-task-*.py` 显示**七个**集合把 `./tests/tracesealer/` 列入 `PACKAGES`（`p4-task-3-rootguard` 另含 `./internal/tracesealer/`）；工具对包内**全部** `*_test.go` 取哈希 ⇒ 本轮新增两个测试文件使这七个集合的「测试文件 sha256」段**全部失效**。另 `p4-task-1-round` / `p4-task-3-loop` 的 TARGET `runner.go`、`p4-task-1-exit` 的 `main.go` 本轮被改 ⇒ 其「被测文件 sha256」段同时失效。⇒ 本轮共跑 **9 个集合**（两个新集合 + 七个重出）的 `--check` 与权威表：**9 个 `check_exit=0` / 9 个 `table_exit=0`**，**48 条变异 = 48 断言红 + 0 允许的绿 + 0 崩溃/超时/编译红 + 0 未登记的绿 + 0 证据无效**（`p4-task-1` 四集合 7/6/2/3、`p4-task-2-alert` 7、`p4-task-3-rootguard` 8、`p4-task-3-loop` 7、`p4-task-4-lock` 4、`p4-task-4-runner` 4；`--check` 唯一定位数同为 48）；绑定 **`-at-d1716d21`**，18 份产物（9 表 + 9 check）落盘于 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/`，旧表（`-at-0240d4b5` / `-at-055c28c1` / `-at-12bdf706` / `-at-ee4a60e4` 等）留档不删。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E65** | ① 计划 **E63** 行内的计数句「本轮 40 条变异 = **39 断言红 + 1 允许的绿**」；② Task 4 步骤 5 的「新增 6 + 5 = 11 个用例」；③ 全仓一切「零允许的绿」类表述的**范围**；④ `tools/trace-mutate/sets/p4-task-3-loop.py` 的 m07 与 `COVERAGE_GAPS`；⑤ Task 4 步骤 6 的集合拆分与 m07 形态；触发 = 上一轮（Task 3 评审修订轮）复审点出的三处精度问题 | **(a)** E63 行在其「**E64① 已订正**」标记**之后**仍留旧计数「40 条变异 = 39 断言红 + 1 允许的绿」，与各集合 `EXPECTED_GREEN` 已清空**自相矛盾**（同一行内前半说清空、后半仍算 1 条）。**(b)** 缺一条约定：**偶发杀手（竞态类）不得作为某变异为红的唯一依据** —— 实测实例：`p4-task-3-loop` 的 **m07**（轮次并行）在 `-at-d1716d21` 的表里把 `TestLoopStopsOnContextCancel` 一起打红（`-at-0240d4b5` 的表里没有；单项单跑 5 次里 1 次红），它的**确定性**杀手是 `TestLoopNeverOverlapsRounds`。**(c)** 「零允许的绿」被读成**全项目**事实 —— **不字面成立**：本计划与全部集合工件里**没有**这句字面陈述（`grep -rn '全项目'` 与 `grep -rn '零允许的绿'` 在 `--include='*.md' --include='*.py' --include='*.txt'`、排除 `vendor/` 后**在「改动前」**均 0 命中）—— **E66④ 订正**：该观测**只在改动前**成立；E65 本节自己的「新增约定（E65 起）」第 1 条引入了这两串 ⇒ **提交后（`9954edc9`）各 2 命中，且都落在 E65 本节自身（E65 行 ＋ 紧随其后的「新增约定（E65 起）」第 1 条 —— 自指）**，不构成对「全项目」读法的反驳（本 E66 节同样含这两串 ⇒ 自指命中数还会再增；判据以**改动前**（`00e5c1ba`，实测 0 命中）为准），但按「全项目」的读法它是**假的** —— `tools/trace-mutate/sets/p2-task-2.py:278`（`m03_删除minAge严格小于校验`、`m16_删除roll_interval正数校验`）、`task-5.py:387`（`m13_删除PendingSeals的空块守卫`）、`task-6.py:431`（`m15_删除打开spool失败守卫`、`m34_删除sort顺序`）**仍非空**（阶段 2 / legacy，不属本计划范围）。**(d)** Task 4 步骤 6 把**两个不同文件**的变异塞进一个 `TARGET` 单值的集合（与全局约束第 42 条冲突）。**(e)** 步骤 5 用例条数写错（实测 13 不是 11）。**(f)** 步骤 4 的 A7 示意图有三处不可照抄 | **(a)** 就地把该计数改写为事实：**40 条变异 = 40 断言红 + 0 允许的绿**，其中 `p4-task-3-rootguard` 8 条 = **8 断言红 + 0 允许的绿**（并写明旧数字属 `-at-0240d4b5`，订正后以 `-at-055c28c1` / `-at-12bdf706` / `-at-d1716d21` 为准；**行数不变**）。**(b)** 新增约定（见下「新增约定（E65 起）」第 2 条）；「m07 的确定性杀手是 `TestLoopNeverOverlapsRounds`、`TestLoopStopsOnContextCancel` 是竞态不是杀手」这一事实保留在集合的 `COVERAGE_GAPS` 与 m07 条目里（表仍**如实**列出两个红名 —— 表是事实记录，登记口径在集合工件内）。**(c)** 本轮一切「零允许的绿」类表述**一律限定为「计划 4 各集合」**；三处反例**只登记、本轮不改**（阶段 2 / legacy）。**(d)** 就地把 Task 4 的变异集合拆成两份：`p4-task-4-lock.py`（`lock.go` 的 m02–m05）与 `p4-task-4-runner.py`（`runner.go` 的 m06–m09），计划表格编号原样保留。**(e)** 就地把步骤 5 的条数改为 **8 + 5 = 13**（**行数不变**）。**(f)** 见下方「如实登记（Task 4 文本一致性）」第 3 条，**不改代码块**（计划自己在步骤 4 写着辅助由执行者补全；在册源以 `tests/tracesealer/{lock,concurrent}_test.go` 与两份集合 @ `d1716d21` 为准）。**未动任何自引用行号**：动计划文本前 `grep -cE '本计划 ?:[0-9]'` = **0**，改后复测仍为 **0** ⇒ **零级联**（本轮两处就地改写都是**单行内**替换，全文行数 3082 → 3082；本 E65 节追加在文末） |

### 新增约定（E65 起，**今后所有轮次适用**）

1. **「零允许的绿」必须写清范围**：任何「零允许的绿 / 没有允许的绿」类表述**必须限定到具体集合或批次**（本例一律写「**计划 4 各集合**」）。反例已经存在：阶段 2 / legacy 的三份集合（`p2-task-2.py:278`、`task-5.py:387`、`task-6.py:431`）的 `EXPECTED_GREEN` 非空 —— 它们是**历史事实**、不是缺陷；把范围写成「全项目」就是失真陈述。
2. **偶发杀手（竞态类）不得作为某变异为红的唯一依据**：若某变异在某张表里把一条**竞态**用例（同一条变异反复跑时红/绿不定）打红，它**不得**被登记为该变异的杀手；该变异必须另有**确定性**杀手，否则按「无杀手」处理（登记进集合既有的 `COVERAGE_GAPS` 并补人工探针）。判据是「同一条变异重复跑 N 次是否稳定红」。本轮实例：`p4-task-3-loop` 的 m07 —— 竞态红名 `TestLoopStopsOnContextCancel`，确定性杀手 `TestLoopNeverOverlapsRounds`。表的红名清单**照实记录**，登记口径由集合工件承载（E64②）。

**如实登记（Task 4 文本一致性：四处与实现不一致 —— 两处在**集合侧**就地订正、一处在**计划文本**就地订正、一处**只登记**）**：

1. **集合拆分（已就地订正）**：计划步骤 6 的 `p4-task-4-lock.py` 同时含 `lock.go` 的 m02–m05 与 `runner.go` 的 m06/m07，而 `TARGET = "internal/tracesealer/lock.go"` ⇒ 后两条匹配串在 `--check` 阶段必然 `count=0`。工具一次只能变异一个文件（全局约束第 42 条），故按先例（`p2-task-8.py` / `p2-task-8-spooler.py`）拆成两份集合。
2. **m07 的形态（如实订正）**：计划的「插入 `os.MkdirAll`」在 `runner.go`（**没有** `os` 导入）会红成**编译红**（无效证据，违反全局约束第 46 条）⇒ 集合用**两个操作**（先补 `"os"` 导入、再插 `os.MkdirAll(opts.Root, 0o755)`），语义与计划逐字相同。另新增 **m08**（`Close` 不释放锁）与 **m09**（装配失败不释放已取得的锁）：后者是计划步骤 3 自己写明的义务（「`NewRunner` 失败时必须把已取得的资源释放干净」），按 **E62** 必须由在册行为用例承载 ⇒ 本轮新增 `TestNewRunnerReleasesLockWhenSealerFails` 与 `TestNewRunnerCloseIsIdempotent`（后者幂等由两层保证、runner.go 侧单点变异杀不掉，按 E64② 登记进集合的 `COVERAGE_GAPS`）。
3. **A7 示意图（未逐字同步，三处）**：① 计划的 `if i%40 == 39` 会在**最后一次** Append 之后也交接 ⇒ 只剩「产物在场」一条落点分支被走到；本轮改成 `if i%40 == 39 && i < 160`（4 次交接、最后一块留在 spool），使「产物在场」与「spool 行数 > row」**两条**分支都被走到。② 计划的 `hs.assertEveryRefLands()`（无参）**不可实现**：逐个 Ref 判定必须拿到 `refs` ⇒ 实现为 `hs.assertRefsLand(refs)`（返回 error）。③ 计划把 `require.False(sealed[ref.Part], …)` 与 `assertEveryRefLands()` 写在同一个示意块里；实现把**次序断言**落进 `TestA7WriterNeverAppendsToAHandedOffPart` 的**自身函数体**（`sealOnce` 才是共享辅助），以保证「一条承重性质 = 一个顶层用例」。
4. **用例条数（已就地订正）**：步骤 5 的「新增 6 + 5 = 11」与实测 **8 + 5 = 13** 不符（多出的两条见上 2）。

**如实登记（Task 4 的人工探针：四条「无变异杀手」用例与两条断言的可失败性）**：`p4-task-4-{lock,runner}` 的 `COVERAGE_GAPS` 登记了这些用例，其可失败性由人工探针承载（均按 `cp` 还原；还原后 sha256 与提交逐字节一致、`git status --porcelain` 为空；**E66①：本条内全部 `*.go:NNN` 均为「改动后行号」**）：① 把 `sealOnce` 里的 `w.MarkSealed(task.DT, task.Part)` 删掉 ⇒ `TestA7WriterNeverAppendsToAHandedOffPart` FAIL 于 `concurrent_test.go:161`（次序断言）；② 删掉 `handoff` 的 `os.WriteFile` ⇒ `TestA7NoRefPointsToNothing` FAIL 于 `:141`、`TestA7RestartAfterSealStartsAtNextPart` FAIL 于 `:188`；③ 交接完成后删掉产物目录再调 `assertRefsLand` ⇒ `TestA7NoRefPointsToNothing` FAIL 于 `:146`（「Ref 丢了：产物与 spool 都不在」）；④ 在 `Round` 里对同一 task 重复调用一次 `Seal` ⇒ `TestA7NoPartIsSealedTwiceConcurrently` FAIL 于 `:277`（perPart=2）；⑤ 用例侧并发跑两个 `Round` ⇒ 同一条 FAIL 于 `:281`（maxInFlight=2）。`internal/tracesealer/runner.go` = `dd80de132255fafb…`、`tests/tracesealer/concurrent_test.go` = `6ec687db153fbdeb…`（与提交一致）。

---

## 过程失误登记与新增约定（E66；**Task 5 开工前的上轮评审收口轮**）

> 编号续 **E65**。与 E57–E65 同一纪律：逐条写清「位置 / 问题 / 处置」。本轮由上一轮（Task 4 收口轮）独立评审留下的 **5 条**触发，**只做这 5 条**；Task 5 本体是同一分支上紧随其后的独立提交。

> **本轮改动面（已核实）**：实现面 `internal/tracesealer/**`、`cmd/**`、`tests/**` **零改动**；冻结包 `internal/tracearchive/**`、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` **零改动**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）；集合面改五个 —— `p4-task-1-exit.py` / `p4-task-1-options.py` / `p4-task-4-lock.py` **各补一行** `Mkdir` absent 静态检查（**E66③**），`p4-task-3-loop.py` **补 m07 形态登记**（**E66②**），`p4-task-3-loop.py` / `p4-task-4-lock.py` / `p4-task-4-runner.py` **补探针行号版本标记**（**E66①**）；计划面改三处 —— Task 4 步骤 6/7 的集合拆分文本同步（**E66⑤**）、E65(c) 的范围订正（**E66④**）、计划 E64 行与 Task 4 探针段的「改动后行号」标记（**E66①**）。

> **跨任务连带（E59③ 自查，先自查后报告）**：`grep -n 'PACKAGES' tools/trace-mutate/sets/p4-task-*.py` ⇒ 本轮**没有任何 `*_test.go` 被改**，因此各集合的「测试文件 sha256」段**全部未变**；失效的只是被改那五个集合的「变异集合 sha256」与 `CHECKS` / 映射表段 ⇒ 按 **E59** **只重跑重出这五个集合**（`--check` ＋ 权威表），其余集合的集合文件内容与测试文件哈希都没变、**不牵连**。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E66** | ① 计划 E64 行与 Task 4 探针段、以及 `p4-task-3-loop.py` / `p4-task-4-lock.py` / `p4-task-4-runner.py` 的 `COVERAGE_GAPS` 里引用的探针行号；② `tools/trace-mutate/sets/p4-task-3-loop.py` 的 **m07**；③ `p4-task-1-round.py:93`（`runner.go`）与 `p4-task-3-rootguard.py:133`（`rootguard.go`）之外的全部 sealer 源文件；④ 计划 **E65** 行 (c) 的计数句；⑤ 计划 Task 4 步骤 6/7 的集合文本；触发 = 上一轮（Task 4 收口轮）独立评审留下的 **5 条** | **文档①**：探针行号**没有标注版本** —— 同一行号在「改动前 / 改动后」指向不同位置，读者无法复核。**文档②**：loop 的 **m07** 是**插入型（顺序颠倒）**变异（串行 → 并行），与 `tools/trace-mutate/README.md:5` 的「再逐条把守卫删掉/放宽」措辞**不同形**，而集合里**没有这一条形态登记**。**必做③**：D70「整包零建目录」的静态检查当时只覆盖 `runner.go`（`p4-task-1-round.py:93`）与 `rootguard.go`（`p4-task-3-rootguard.py:133`），`lock.go` / `options.go` / `cmd/trace-sealer/main.go` **缺该行** ⇒「整包」名不副实，而「启动时 root 不在就先建出来」正是这类事故（2026-09-24 `/` 被写满）最自然的入口。**必做④**：E65(c) 写「`grep -rn '全项目'` 与 `grep -rn '零允许的绿'` …… 均 0 命中」，但 E65 自己新增的「新增约定（E65 起）」第 1 条引入了这两串 ⇒ **提交后各 1 命中、且都落在该约定自身（自指）**；该观测**只在「改动前」成立**。**文档⑤**：计划 Task 4 步骤 6/7 仍只写一个集合、`TARGET` 仍写 `lock.go`，与 **E65(d)** 的裁定（拆成 `p4-task-4-lock` ＋ `p4-task-4-runner`）**不同步** | **①** 在计划 E64 行、Task 4 探针段与三个集合的 `COVERAGE_GAPS` 里统一标注「**改动后行号**」（本轮五处；判据 = 行号取自本轮提交后的文件，E66 本身不改任何 `.go` ⇒ 与提交内容一致）。**②** 在 m07 条目上方补【E66② 形态登记】，写明：它是插入型、与 `README.md:5` 的张力；依据两条 —— 计划 Task 4 步骤 6 的 m07 行**原文自己**就要求「顺序颠倒，插入 `os.MkdirAll`」，以及**同形先例** = Task 3 的 **m06**（「跑完一轮就退出」，在循环体里插入 `return nil`，已在权威表里为**断言红**）；并要求插入型**必须**另有**确定性**杀手（本条 = `TestLoopNeverOverlapsRounds`；`TestLoopStopsOnContextCancel` 的偶发红**不**计入，见本集合 `COVERAGE_GAPS`）。**③** 在 `p4-task-1-exit.py`（`cmd/trace-sealer/main.go`）、`p4-task-1-options.py`（`options.go`）、`p4-task-4-lock.py`（`lock.go`）各补一行 `\bMkdir[A-Za-z]*\s*\(` **absent**，使 D70 覆盖两个目录的**每一个**源文件；它**不可能**由变异承载（工具只在 pristine 树求值 `CHECKS`，`trace_mutate.py:574`）⇒ 可失败性由**临时仓库探针**实测（三个临时克隆各插入一处建目录调用并提交到该克隆 ⇒ 对应那一行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」退出 **1**；三份输出落盘为证据目录里的 `e66-mkdir-check-probe-*.txt`，表末记 `probe_exit=`）。**④** 就地改写为「**在改动前**均 0 命中」，并写明「提交后各 1 命中、落在 E65 本节自身的约定行（自指）」，判据以**改动前**的 0 为准。**⑤** 就地把 Task 4 步骤 6 的集合文本同步成两份（新增 `p4-task-4-runner.py` 那一行、m07 行补形态依据）、步骤 7 改成「两个集合各跑一次」（**E66⑤**）。**⑥（任务 5 步骤 7 的强制核对自己发现的，不是上一轮评审的条目）**：计划里的 **p4 集合总数「26」是错的** —— 任务 4 已按 **E65(d)** 拆成 `p4-task-4-lock.py` ＋ `p4-task-4-runner.py`（计划 `:1946-1947` 自己也写着这是两个文件），而 `:97` 的「26 个变异集合」与任务 5 步骤 7 的算式「任务 4（1）… = **26**」都还是**拆分前**的数。**就地订正为 27**（算式其余项不变，任务 5 = 5），判据 = **按计划自己声明的集合名逐一点名复核**（task1 `classify/exit/options/round`、task2 `alert`、task3 `loop/rootguard`、task4 `lock/runner`、task5 `unit/isolate/restore/build/ci`、task6 `blue/green/root`、task7 `bluegreen`、task8 `compose/guard/runsh`、task9 `budget/vmhwm`、task10 `contract/footer/observability`、task11 `runbook`）⇒ **27**；而任务 5 收口时磁盘上实有 **14** 个 `p4-task-*.py`，恰等于算式前五项 4+1+2+2+5。**不改任何集合/测试/实现**（纯计划文本）⇒ 按 **E59**，五个 `p4-task-5-*` 集合的绑定不受影响（纯计划与证据提交不使绑定失效）。**自引用级联**：动计划文本前 `grep -cE '本计划 ?:[0-9]'` = **0**，本轮（含⑥的两处改写）动后复测仍为 **0** ⇒ 零级联（⑤/⑥的改写会改变后续行的**绝对行号**，但全文**不含**指向绝对行号的自引用；唯一一处行号对映（集合里的「原 `:1956` → `:1958`」）已在写入时按改后实际值给出）。 |
| **E67** | ① 计划 **E66** 行「必做③」的收口句与「新增约定（E66 起）」第 1 条（「覆盖该包**每一个**源文件」）；② `tools/trace-mutate/sets/p4-task-1-classify.py`（`classify.go`）、`p4-task-1-exit.py`（`exit.go`）、`p4-task-2-alert.py`（`alert.go`）、`p4-task-3-rootguard.py`（`covers_test.go`）四个集合的 `CHECKS`；触发 = Task 5 收口轮独立评审留下的 **1 条必做** | **E66③ 名不副实**：它声称 Mkdir-absent 静态检查覆盖两个目录**每一个**源文件，实测只覆盖 `runner.go`（`p4-task-1-round.py`）/ `rootguard.go`（`p4-task-3-rootguard.py`）/ `lock.go`（`p4-task-4-lock.py`）/ `options.go`（`p4-task-1-options.py`）/ `main.go`（`p4-task-1-exit.py`），而 `classify.go` / `alert.go` / `exit.go` **没有该行**（复核时另发现包内测试文件 `covers_test.go` 也没有）⇒ 与 D70「**整包**零建目录」不符；而「启动时 root 不在就先建出来」正是 2026-09-24 `/` 被写满那类事故最自然的入口 | **扩到全部 9 个 `.go` 文件（不是改窄措辞）**：在四个集合各补一行 `\bMkdir[A-Za-z]*\s*\(` **absent** —— `p4-task-1-classify.py`→`classify.go`、`p4-task-1-exit.py`→`exit.go`、`p4-task-2-alert.py`→`alert.go`、`p4-task-3-rootguard.py`→`covers_test.go`；与既有五行合起来，`internal/tracesealer/` ＋ `cmd/trace-sealer/` 下**每个** `.go` 文件（**9 个** = `internal/tracesealer/` 的 `alert.go` / `classify.go` / `covers_test.go` / `exit.go` / `lock.go` / `options.go` / `rootguard.go` / `runner.go` ＋ `cmd/trace-sealer/main.go`）各有一行。**覆盖到的文件清单已落盘**（`e67-mkdir-coverage-at-*.txt`：两个目录全部 `.go` 的 `ls` 原样输出 ＋ 逐文件「file → 集合:检查名」对映表 ＋ 「全部覆盖: True」判定）；`covers_test.go` 是 **E64①** 新增的**包内测试文件**，同样被 `go test` 编译执行、同样能用建目录「修复」消失的挂载点 ⇒ 一并纳入，使「每一个 `.go` 文件一行」字面成立。它**不可能**由变异承载（工具只在 pristine 树求值 `CHECKS`，`trace_mutate.py:574`）⇒ 可失败性由**临时仓库探针**实测（`e67-mkdir-probe-at-*.txt`：把每个文件对应集合的「纯 pristine 侧」换成「插入 `os.MkdirAll(root, 0o755)` 并提交」的临时克隆 ⇒ 对应那一行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」退出 **1**，表末记 `probe_exit=`）。**E59 连带**：本项改的是**四个集合文件** ⇒ 按「动了集合就重跑重绑」，这四个集合的权威表与 `--check` 在本轮重出（表头 `-at-<HEAD8>` 更新为新时点）。**自引用级联**：动计划文本前 `grep -cE '本计划 ?:[0-9]'` = **0**，改后复测仍为 **0**（本轮计划面只做「追加一行 + 就地改写一条约定」，不含指向绝对行号的自引用） |

### 新增约定（E66 起，**今后所有轮次适用**）

1. **D70「整包零建目录」的静态检查必须覆盖该包**每一个**源文件**：`internal/tracesealer/` 与 `cmd/trace-sealer/` 下每新增一个 `.go` 源文件（**含包内 `*_test.go`**），都必须在某个集合的 `CHECKS` 里有一行 `\bMkdir[A-Za-z]*\s*\(` **absent**；只覆盖「本轮动过的那几个文件」不算覆盖「整包」。**`E66③` 的「每一个源文件」曾被实测证伪（只覆盖 5 个，缺 `classify.go` / `alert.go` / `exit.go` / `covers_test.go`）⇒ 本条按 `E67` 要求把覆盖做成可复核的**逐文件清单****：清单落盘为 `e67-mkdir-coverage-at-*.txt`，判据是「`ls` 两个目录全部 `.go` 的集合」与「`CHECKS` 里 `Mkdir` absent 行的 `file` 集合」逐元素相等（`全部覆盖: True`）。可失败性由**临时仓库探针**承载（工具只在 pristine 树求值 `CHECKS`，变异**不可能**让它变红）。
2. **插入型（顺序颠倒）变异必须逐条登记，且必须另有确定性杀手**：`README.md:5` 把变异表述为「把守卫删掉/放宽」；「插入一条提前返回 / 把串行改成并行」这类**语义反转**与之不同形 ⇒ 必须在集合里写明「依据（计划原文 / 同形先例）」＋「与铁律 5 的张力」＋「该变异的**确定性**杀手」。无**确定性**杀手者不得以插入型存在（与「新增约定（E65 起）」第 2 条配合）。
3. **探针行号必须标注版本**：凡在计划文本或集合工件里以 `*.go:NNN` 作为「实测红在哪一行」的证据，必须写明它是**改动前**还是**改动后**行号；默认口径 = 「**改动后行号**」并逐处标注。

---

## 过程失误登记与新增约定（E68；**Task 7 轮**）

> 编号续 **E67**。与 E57–E67 同一纪律：逐条写清「位置 / 问题 / 处置」。

> **本轮改动面（已核实）**：交付面**新增** `deploy/blue-green/README.md`（蓝绿切换/回滚/降级/硬门剧本）、`tests/deploy/trace_bluegreen_runbook_test.go`（6 条顶层用例）、`tools/trace-mutate/sets/p4-task-7-bluegreen.py` 与名字清单 `p4-task-7-bluegreen-names.txt`；**订正面**改三处 —— `docker-compose.yml` 的注释措辞（**E68①**：只用 `TRACE_BODY_` 前缀、不写完整键名）、`tools/trace-mutate/sets/p4-task-6-root.py`（**E68①**：`CHECKS` 新增一行 `root不得有生效TRACE_BODY_赋值`）、`tests/deploy/trace_isolate_script_test.go` 与 `tests/deploy/trace_sealer_unit_test.go`（**E68③**：task-5 遗留的 `gofmt -w`，非本轮引入）；计划面改四处 —— Task 6 的「接口」段、步骤 3 的根编排段、步骤 6 的 `CHECKS` 段（**E68①**：门禁改写为「**非注释生效行 = 0**」）、「执行顺序与依赖」段末（**E68②**：登记 `main` 已前进到 `43af78b0`）。`internal/tracesealer/**`、`cmd/**`、`internal/tracearchive/**`（冻结包）、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum` **零改动**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）。**零迁移**：本任务不涉及任何 PG schema 变更（剧本文本契约 + 只读 SQL 比对），因此**未触发** `atlas-db-migration` skill（全局约束「零迁移」）。

> **跨任务连带（E59③ 自查，先自查后报告）**：`grep -n 'PACKAGES' tools/trace-mutate/sets/p4-task-*.py` 显示**八个**既有集合把 `./tests/deploy/` 列入 `PACKAGES`（`p4-task-5-{unit,isolate,restore,build,ci}` ＋ `p4-task-6-{blue,green,root}`）；工具对包内**全部** `*_test.go` 取哈希 ⇒ **E68③** 的 `gofmt -w` 使这八个集合的「测试文件 sha256」段**全部失效**。另 **E68①** 改了 `p4-task-6-root` 的 TARGET `docker-compose.yml` **与集合文件本身** ⇒ 它的「被测文件 sha256」段与「变异集合 sha256」段同时失效。⇒ 本轮共跑 **9 个集合**（新集合 `p4-task-7-bluegreen` ＋ 八个重出）的 `--check` 与权威表，全部 `check_exit=0` / `table_exit=0`，详情见交付报告。`p4-task-1..4`（`./tests/tracesealer/` 包）与 `p4-task-5/6` 之外的一切集合**不受牵连**（测试文件哈希与集合内容都没变）。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E68** | ① 计划 Task 6 的「接口」段与步骤 3 的根编排段（旧措辞「不加任何 `TRACE_BODY_*`」，易被读成「`grep -c TRACE_BODY docker-compose.yml` 必须为 0」）、步骤 6 的 `CHECKS` 段，以及 `docker-compose.yml:39-40`（订正前）的两行注释；② 计划的基线说明（全局约束与「执行顺序与依赖」段）只写到 `main=0bfb130f`；③ `tests/deploy/trace_isolate_script_test.go` 与 `tests/deploy/trace_sealer_unit_test.go` 的 `gofmt` 漂移（task-5 遗留、非本轮引入）。触发 = Task 6 收口轮的独立评审留下的 **3 条**（①判据口径 / ②登记事实 / ③格式化遗留） | **①判据口径（字面不成立）**：root compose 的 `grep -c TRACE_BODY docker-compose.yml` 实测 **= 2**（订正前，两处都在 `:39`/`:40` 的**注释**里：`TRACE_BODY_*` 与 `TRACE_BODY_ROOT`/`TRACE_BODY_INST`），而**非注释生效行 = 0** ⇒「不加任何 `TRACE_BODY_*`」若被读成「文本 grep 计数为 0」，这个门禁**字面不成立**（注释里提到键族是合法且必要的：不讲清楚「设了写侧开关而实例名缺失会启动失败」，运维反而会照抄错误做法）。真正的门禁是**语义**测试 `TestComposeRootComposeDoesNotEnableTraceBody`（`effectiveAssignments` 只收非注释行）。**②登记事实**：`main` 引用已前进到 `43af78b0`（领先基点 `0bfb130f` **2 个提交** = `d8875bda` ＋ 合并 `43af78b0`，只动 `internal/gateway/authz/groups.go` / `tests/gateway/authz/groups_test.go` / `tests/gateway/orchestrator/group_auth_test.go` **3 个文件**），与本分支**零交集** ⇒ **不影响绑定与合并**；此事实此前只存在于分支外，计划文本里查不到。**③格式化遗留**：两个 `tests/deploy/` 测试文件不合 `gofmt`（task-5 遗留），而 `tests/deploy/` 被**八个**既有集合列入 `PACKAGES` ⇒ 一改就会连带使它们的绑定失效，必须一并重跑重绑 | **①**：计划 Task 6 的「接口」段、步骤 3 的根编排段、步骤 6 的 `CHECKS` 段**就地改写**为「**非注释生效行 = 0**」，并显式写明**文本 `grep -c TRACE_BODY docker-compose.yml` 不是门禁、只是辅助读数**（真正的门禁是语义测试 `TestComposeRootComposeDoesNotEnableTraceBody`）；同时在 `p4-task-6-root.py` 的 `CHECKS` **新增一行** `root不得有生效TRACE_BODY_赋值`（正则 `(?m)^\s*TRACE_BODY_[A-Z0-9_]+\s*:` **absent**；注释行以 `#` 起头、不匹配 ⇒ 这一行量的正是「生效赋值」），它**不可能**由变异承载（工具只在 pristine 树求值 `CHECKS`，`trace_mutate.py:574`）⇒ 可失败性由**临时仓库探针**实测（临时克隆插入一行生效赋值并提交 ⇒ 该行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」退出 **1**；输出落盘 `p4-task-6-rootcheck-probe-at-*.txt`，表末记 `probe_exit=`）。**统一注释措辞**：`docker-compose.yml` 的注释改成只用 `TRACE_BODY_` **前缀**、**不写完整键名** ⇒ `grep -c TRACE_BODY docker-compose.yml` 由 **2 → 1**（`grep -cE 'TRACE_BODY_[A-Z]'` = **0**），而**生效行仍是 0**（`grep -cE '^\s*TRACE_BODY_[A-Z0-9_]+\s*:'` = **0**）。**②**：在「执行顺序与依赖」段末新增一条**基线登记**（只登记事实、不改任何门禁）：写明 `main` 已前进到 `43af78b0`、领先 `0bfb130f` 两个提交、只动三个 `authz` 文件、与本分支零交集 ⇒ 不影响绑定（绑定的实质是**内容哈希**，见全局约束与 **E59**）与合并。**③**：执行 `gofmt -w tests/deploy/trace_isolate_script_test.go tests/deploy/trace_sealer_unit_test.go`（`gofmt -l tests/deploy/ tests/tracesealer/ internal/tracesealer/ cmd/trace-sealer/` 复测**为空**）；因这两个文件属于全部把 `./tests/deploy/` 列入 `PACKAGES` 的集合，按 **E59** 与 Task 7 的新集合**一并重跑重绑**（**9 个集合**：`p4-task-7-bluegreen` 新出 ＋ `p4-task-5-{unit,isolate,restore,build,ci}` 与 `p4-task-6-{blue,green,root}` 重出）。**自引用级联**：动计划文本前 `grep -cE '本计划 ?:[0-9]'` = **0**（已实测），改后复测仍为 **0**（本轮计划面的四处改动都是**段内就地改写**或**段末追加**，不含指向绝对行号的自引用） |

### 新增约定（E68 起，**今后所有轮次适用**）

1. **门禁判据必须写成语义判据，文本 grep 只能当辅助读数**：任何「产物里不得出现 X」类门禁，必须写明它的**语义**判据（例：`effectiveAssignments` 的「**非注释生效行 = 0**」，或行为用例名），**不得**把「某条 `grep -c <键名> <文件>` = 0」写成门禁 —— 注释里合法出现键名前缀会让文本计数**恒非零**，于是门禁要么字面不成立、要么逼着作者把必要的注释删掉。文本 grep 若要保留，必须**显式标注为辅助**，并给出它的可失败性证据（临时仓库探针）。

---

## 过程失误登记与新增约定（E69；**Task 8 开工前的上轮评审收口轮**）

> 编号续 **E68**。与 E57–E68 同一纪律：逐条写清「位置 / 问题 / 处置」。

> **本轮改动面（已核实）**：计划面**两处** —— Task 7 步骤 5 段末**就地追加一句**（E69③，**行数不变**）＋ 文件末尾**追加**本节；**实现/测试/产物/集合/证据面零改动** —— `internal/**`（含冻结包 `internal/tracearchive/**`）、`cmd/**`、`tests/**`、`deploy/**`、`atlas-migrations/**`、`migrations/**`、`go.mod`、`go.sum`、`tools/trace-mutate/**`、`.superpowers/sdd/**` 全部**未动**；`tools/trace-mutate/trace_mutate.py` **未动**（sha256 仍是 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）。**零迁移**：本轮不涉及任何 PG schema 变更 ⇒ **未触发** `atlas-db-migration` skill（全局约束「零迁移」）。

> **跨任务连带（E59③ 自查，先自查后报告）**：`grep -n '^PACKAGES' tools/trace-mutate/sets/p4-task-*.py` 显示**没有任何**集合把 `docs/**` 列入 `PACKAGES`，也没有任何集合的 `TARGET` 指向计划文档 ⇒ 本轮**唯一**的改动面是计划文本 ⇒ 按 **E59**「纯计划文档提交不使绑定失效」，**零个集合需要重出**（这与 E59③ 的连带面判定一致：只有实现 / 测试 / 集合 / 被测产物的**内容**变更才使绑定失效）。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E69** | ① 计划 / 证据文本里若有「git reflog 无 amend 记录」一类措辞（**实测：本计划与本任务的证据目录里都没有这种措辞** —— 可复算命令 `grep -rniE 'amend\|reflog' docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md .superpowers/sdd/2026-10-08-trace-sealer-deployment/ tools/trace-mutate/`：**追加本节之前为 0 行**；追加之后同一命令只命中**本节自身**（本节逐字引用了该措辞与 `amend` / `reflog` 两个词）⇒ **除本节外仍为 0 行**；`grep -rniE '无.*amend\|未.*amend' docs/ .superpowers/sdd/2026-10-08-trace-sealer-deployment/ tools/trace-mutate/` 只命中**另一份计划** `docs/superpowers/plans/2026-09-24-tracearchive-storage-primitives.md:4116`（D40，措辞是「`amend` 已禁 ⇒ 留痕」，**不是**「reflog 无 amend 记录」）＋本节自身 ⇒ 本计划与本任务证据目录仍为 **0 行**）；② `45e39d92` 的提交说明尾部；③ `tools/trace-mutate/sets/p4-task-7-bluegreen.py` 的 **6 行** `CHECKS` 与计划 Task 7 步骤 5 的**明列范围**（只列了 `deploy-blue-green.sh` 内 `TRACE_BODY` **absent** 一行）之间的差 | **①（订正事实）**：`git reflog` **有 1 条** `commit (amend)` = **`01de7150`**（`HEAD@{13}`，内容 = `docs(plan): E66⑥ —— p4 集合总数 26 → 27`）—— 它属 **Task 5 轮**，**与 Task 7 的提交及当前 tip（`45e39d92`）无关**（`git reflog -15` 实测：`45e39d92`/`1b6c7d6e`/`957c8ed2`/`38ebcf7e` 四条全部是 `commit:`，无 `amend`）⇒ 正确表述是「**Task 7 的提交未 amend；reflog 中唯一那条 amend 来自 Task 5 轮**」；「reflog 无 amend 记录」**不实**，此后不得出现。**②（登记伪影）**：`45e39d92` 的提交说明**尾部**有残缺 UTF-8 伪影（字节 `\xc2\xbc\xc2\x89`，实测命令 `git log -1 --format=%B 45e39d92 \| xxd \| tail -2`；正确字符应为 `）` + 换行）⇒ **禁止 amend**（会改写已发布的证据绑定时点），本项**只登记**：**不修**、保留原始提交说明与其字节，处置方式写明为「随分支收尾的合并 / squash 顺手修正，或在收尾报告里另立登记」。**③（对齐范围）**：该集合的 `CHECKS` 有 **6 行** —— 1 行是计划步骤 5 明列的（`deploy-blue-green.sh` 内 `TRACE_BODY` **absent**），另 **5 行**是本轮按 **E62** 追加的**存在性**静态检查（颜色↔inst 静态对应、硬门 D73、功能关闭的 SQL 比对、旧颜色 drain 无损、禁止事项）；集合 docstring 已写明理由与「静态检查只承载存在性、可失败性由探针实测」，但**计划文本里没有对应句子** ⇒ 计划与集合**无法逐条对照** | **①**：本节把事实写成**可复核的判据**（命令 + 结论），供后续轮次直接引用；`git reflog` 的当前事实同时登记为「1 条 amend / 属 Task 5 轮 / 与 Task 7 无关」。**②**：只登记、不动历史（**禁止 amend**，与全局约束「禁止 `--amend`」一致）。**③**：在 Task 7 **步骤 5 段末就地追加一句**（**行数不变**）写明那 5 行 `CHECKS` 的存在与理由（E62：静态检查只承载**存在 / 不存在**类要求；它们的**可失败性**由交付报告的「改坏→转红→还原」探针逐条实测，输出落盘 `p4-task-7-bluegreen-probe-at-*.txt`）⇒ 计划与集合此后可逐条对照。**自引用级联**：动计划文本前 `grep -cE '本计划 ?:[0-9]' docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md` = **0**（已实测），改后复测仍为 **0**（本轮两处改动都是**段末追加/就地追加**，不含指向绝对行号的自引用）。

### 新增约定（E69 起，**今后所有轮次适用**）

1. **提交历史的「事实陈述」必须带可复算命令，且禁止改写已发布提交**：凡在报告或计划里陈述 `amend` / `reset` / `rebase` 一类历史事实，必须同时给出可复算命令（如 `git reflog -15`）与结论；**禁止 `--amend`**（它会改写绑定证据的产出时点）。已发生的提交说明伪影（残字节）**只登记不修**，修正留给分支收尾的合并 / squash 或另立登记。
2. **集合的 `CHECKS` 行数必须与计划文本逐条可对照**：集合可以按 E62 追加**存在性**静态检查，但每追加一行都必须在**计划文本**里能找到对应句子（写明存在与理由）；只写在集合 docstring 里不算 —— 否则计划与集合无法逐条对照，读者无法从计划推出集合的应然范围。

---

## 过程失误登记与新增约定（E70–E71；**Task 9 交付轮**）

> 编号续 **E69**。与 E57–E69 同一纪律：逐条写清「位置 / 问题 / 处置」。

> **本轮改动面（已核实）**：**实现面** = `internal/traceverify/budget.go`（新）＋ `internal/traceverify/vmhwm.go` ＋ `cmd/trace-verify/main.go` ＋ `tests/traceverify/budget_test.go`（新）＋ `tests/traceverify/vmhwm_test.go`；**集合面** = 新增 `p4-task-9-{budget,vmhwm}.py` 与两份名字清单，并**就地补齐** `p4-task-8-{guard,runsh}.py` 的删除面判据（E70①）；**计划面** = 范围段（`:17`）/ A 表尾行（`:110`）/ H9（`:2953`）三处**同行就地改写**（E71①）＋ 文件末尾**追加本节**；**证据面** = 新增 `p4-task-9-*` 六份与重出的 `p4-task-8-{guard,runsh}-*` 四份。**冻结包 `internal/tracearchive/**` 零改动**、`atlas-migrations/**` 与 `migrations/**` 零改动、`tools/trace-mutate/trace_mutate.py` 零改动（sha256 见交付报告）、`deploy/verify/trace-a3/run.sh` 零改动。

> **跨任务连带（E59③ 自查，先自查后报告）**：`grep -n '^PACKAGES' tools/trace-mutate/sets/p4-task-*.py` ⇒ 本轮改了 `tests/traceverify/*_test.go`（被 `p4-task-8-guard` 与两个新集合列入 `PACKAGES`）与 `cmd/trace-verify/main.go`（**`p4-task-8-guard` 的 `CHECKS` 静态检查文件**，**不是**它的 `TARGET` —— 该集合的 `TARGET` 是 `internal/traceverify/guard.go`，见 `tools/trace-mutate/sets/p4-task-8-guard.py:33`）⇒ **必须重出** `p4-task-8-guard`；改了 `p4-task-8-runsh.py` **自身**（判据正则，E70①）⇒ **必须重出** `p4-task-8-runsh`。另 `grep -l '^PACKAGES.*tests/deploy' tools/trace-mutate/sets/p4-task-*.py | wc -l` = **11** 个集合与 `./tests/deploy/` 包**耦合**，但本轮 `tests/deploy/**` 与 `deploy/verify/trace-a3/run.sh` **零改动** ⇒ 只有 `p4-task-8-runsh`（集合文件变了）需要重出，其余 **10 个不受牵连**。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E70** | ① `tools/trace-mutate/sets/p4-task-8-guard.py` 的 `trace-verify破坏性删除面不得递归`、`tools/trace-mutate/sets/p4-task-8-runsh.py` 的 `run.sh不得递归删除`（上一轮 Task 8 收口轮的独立评审第 ① 条）；② 上一轮 `cmd/trace-verify/main.go` 的 `decode-bench` 实现面与 CLI 面（评审第 ② 条）；③ 上一轮报告 / 计划文本里的两处表述（评审第 ③ 条） | ① 判据只钉 `-rf`（`r` **紧跟** `f`）⇒ `rm -r X`、`runCmd("rm","-r",…)`、`rm --recursive`、`rm -fr`、`rm -v -r` **全部漏网** ⇒ 门禁退化成「对某一种拼写的巧合」，与 **E68** 的「判据必须是**语义**判据」相悖。② 上一轮的 `decode-bench` 做的是**整流解码**（`DecodeArtifact`：把整块解压一遍再丢弃），而计划任务 9 步骤 3 要求「对每个并发度 K，起 K 个 goroutine 各做 50 次**随机点读**（`tracearchive.NewReader` + `GetBody`）」—— 两者测的不是同一个动作：整流解码共享解码器、**少了 trailer/索引那条路径**（而那正是 `maxTrailerEntries` 上界要压的东西）⇒ 整流读数**不能**当 C5 的判据。同时 CLI 面（`--rounds/--samples/--interval/--window-bytes/--pid/--proc-root`）超出计划只给出的 `--artifact/--concurrency/--json`。③ 两处表述不实：「含 `./tests/deploy/` 的集合恰为 9」只数了**本轮连带重出的** 9 个；`git reflog \| grep -i amend` 会因**提交说明里含 `amend` 二字**而多出一行。 | ① **本轮选择「把正则补全」**（而不是「把 `-r` 边界登记进 why」）：E68 要求门禁是语义判据（「删除面有界」= **任何**递归删除形态都不许出现），只钉一种拼写下一轮换个写法就能绕过。新判据（两集合同一形态）= `(?m)^(?!\s*(?://\|#))(?=.*\brm\b)(?=.*(?:--recursive\b\|(?<!-)-[a-zA-Z]*[rR][a-zA-Z]*\b)).*`（同一行同时出现 `rm` 与递归开关；`(?<!-)` 把 `--force` 这类名字里含 `r` 的长开关排除在外），仍是 `absent`；**旧判据 vs 新判据对同一段插入文本的判定差**（旧 0 命中 / 新 2 命中）与「插入 ⇒ 该行 FAIL ⇒ 工具以『静态检查失败 ⇒ 整轮失败』退出 1」的实测见 `p4-task-9-probe-at-*.txt`；补全后在两个真实目标文件上实测 **0 命中**（不引入误报，见 `p4-task-8-{guard,runsh}-authoritative-at-*.txt` 的 `[PASS]`）。② **本轮按计划落实点读负载**（`PointReadLoad`：K 个 goroutine × 连续 ≥5 轮 × 每 goroutine 每轮 50 次随机点读、实测同时在飞峰值、首错即取消并原样上抛、峰值到 `AbortAtBytes` 即中止并留下已观测峰值），**偏差就地消解**（整流解码保留为独立的 `DecodeArtifact`，其用例 `TestDecodeArtifactRoundTripsRealArtifact` 仍在册）。CLI 面**仍是计划的超集**，**如实登记**为**已登记偏差**：新增 `--rows`（跳过行数探测——探测本身会推高 VmHWM）/ `--gc-modes`（承载步骤 4.2 的「同时报告有 GC 与 GOGC=off 两种形态」）/ `--abort-at-bytes`（承载步骤 4.2 停止点的**可观测性**：`GOGC=off` 下进程会被 OOM 杀掉、什么都报不出来），`--samples` 的语义改为「采样次数上限，0 ⇒ 采到负载结束」；计划给出的输出形状 `{"concurrency":K,"peak_vmhwm_bytes":N,"points":50}` 三个键**逐字保留**在 JSON 里（本实现是它的**超集**）。③ 本轮把两处事实写成**可复算判据**：`grep -l '^PACKAGES.*tests/deploy' tools/trace-mutate/sets/p4-task-*.py \| wc -l` = **11**（第 10、11 个是 `p4-task-8-compose` 与 `p4-task-8-runsh`，它们随后一轮才新增）；`git reflog \| grep -c 'commit (amend)'` = **1**，而 `git reflog \| grep -i amend \| wc -l` = **2**（第 2 条是 `c9e500a9` 的**提交说明**里含「amend」二字，不是一次 amend）⇒ **今后一律用前者**。 |
| **E71** | 计划 3（`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）的 **E52③** 登记的「**计划 4 实现轮欠账**」：计划 4 的 A 表尾行与 **H9** 仍把 A1 划给「计划 3 读侧」（计划 3 的 E52 写的位置是 `:101` / `:2936`） | 发起者已裁定（计划 3 **E52**）：规格 §14.2 **A1** 的百万级（n = 1e6）读回的**物理测量归计划 4 任务 9**（环境 = 生产宿主 + 真实 PG + 真实 `zstd` **1.4.8**），**口径**（测什么 / 怎么测 / 通过标准）由计划 3 的交接 **H19** 给出；而计划 4 的两处文本仍写「A1 归计划 3」⇒ 两处分工表**互指对方、无人执行**。**行号漂移（已核实，不是推测）**：计划 3 的 E52 引用计划 4 的 `:101` / `:2936`，在计划 4 **当前**版本里实为 **`:110`** / **`:2953`**（计划 4 此后追加过文本；计划 3 按 H18「只引用不改」纪律当时未动计划 4）⇒ 本条一律以**当前实时行号**为准（E54/E55 的同一纪律）。 | ① **就地订正计划 4 的三处**（范围段 `:17`、A 表尾行 `:110`、H9 `:2953`）：写明「**A1 的读回半部（n = 1e6）物理测量归任务 9**（环境 = 生产宿主 + 真实 PG + 真实 zstd **1.4.8**）；**口径**由计划 3 的交接 **H19** 给出；**A2 / A5** 的对外映射归计划 3；**A4** 由计划 1 覆盖」。三处都是**同行就地改写**（行数不变 ⇒ 不产生行号级联）。② 本轮**不产出** A1 的物理测量数字（本机 = darwin/arm64、无真实 PG、本机 `zstd` v1.5.7 ≠ 1.4.8）⇒ 按任务 8 的既有形态登记「**待宿主执行**」（`p4-task-9-host-deferral-at-*.txt`），**不得**写成「已覆盖 / 已承接」。③ **自引用级联**：动计划文本前 `grep -cE '本计划 ?:[0-9]' docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md` = **0**（已实测），改后复测仍为 **0**（三处都是**段内就地改写**，不含指向绝对行号的自引用）。 |

### 新增约定（E70 起，**今后所有轮次适用**）

1. **「不得出现 X」类判据必须覆盖 X 的**全部**常见形态，并给出「旧判据漏网 / 新判据命中」的同文本对照**：只钉一种拼写等于把门禁退化成对某一行文本的巧合（E70① 的实例：`-rf` 漏掉 `-r` / `--recursive` / `-fr` / `-v -r`）。补齐后必须把**两个目标文件上的 0 命中**与**插入形态后的 FAIL + 退出 1**都落盘（探针表）。
2. **变异必须红成断言红**：编译红 / panic / 超时一律**就地改写变异字符串**，不得登记为「允许的绿」（E70② 的实例：把行号变异成字面量 `0` 会让 `rng` 成为未使用变量 ⇒ 编译红；改成 `rng.Int63n(1)`（恒 0、仍引用 `rng`）后才红成 `TestPointReadLoadRunsConcurrentlyAndRandomly` 的断言）。
3. **跨计划行号引用必须以「当前实时行号」为准**：跨计划引用别处行号时必须**当场复算**（如 `grep -n`），并在登记里同时写出「对方当时写的行号」与「当前实时行号」（E71 的实例：`:101` → `:110`、`:2936` → `:2953`）。

---

## 过程失误登记与新增约定（E72–E73；任务 10 收尾轮 ＋ 任务 11 交付轮）

> 编号续 **E71**。与 E57–E71 同一纪律：逐条写清「位置 / 问题 / 处置」。
>
> **两份登记的读法（先声明，免得再被读成自相矛盾）**：**E72** = **任务 10 收尾轮**（提交 `d058b1ba`）的三处订正
> —— 那一轮只写了提交说明、**没有**把登记节写进计划（`grep -n 'E72' <本计划>` 在 E73 之前 = **0 命中**），
> 本轮**补登记**（内容逐条取自 `d058b1ba` 的提交说明与其实际改动面，数字**本轮自己从表里重算过**）；
> **E73** = **本（任务 11 交付）轮**的五条独立复审项 ＋ 本轮自查发现的一条事实订正。

### E72（补登记：任务 10 收尾轮的三处订正）

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E72①** | `.superpowers/sdd/2026-10-08-trace-sealer-deployment/p4-task-9-host-deferral-at-7740bdbf.txt` 第 28–30 行 | 把「**15**」当成**四张表**的断言红总数：四表 = `p4-task-9-{budget,vmhwm}` ＋ 重出的 `p4-task-8-{guard,runsh}`。「15」只等于**本任务新增的两张表**（budget ＋ vmhwm），四表总数是 **22**。 | 已在 `d058b1ba` 就地改写为「**四表 22 = 4 + 11 + 4 + 3 断言红**」＋「本任务新增的两张表是 **15 = budget 4 + vmhwm 11**」，并写明此前是**算错**。**本轮自行重算核对**：`p4-task-9-budget-authoritative-at-3777d04c.txt` 的 `合计: 4 = …`、`p4-task-9-vmhwm-authoritative-at-7740bdbf.txt` 的 `合计: 11 = …`、`p4-task-8-guard-authoritative-at-3777d04c.txt` 的 `合计: 4 = …`、`p4-task-8-runsh-authoritative-at-3777d04c.txt` 的 `合计: 3 = …` ⇒ 4 + 11 = **15**，15 + 4 = **19**，19 + 3 = **22** ✓（加法全程写出，E60）。 |
| **E72②** | 本计划的「跨任务连带（E59③ 自查）」段（**当前实时行号 `:3189`**） | 把 `p4-task-8-guard` 的 `TARGET` 说成 `cmd/trace-verify/main.go` —— 事实是：那份集合的 `TARGET` 是 `internal/traceverify/guard.go`（`tools/trace-mutate/sets/p4-task-8-guard.py:33`），`cmd/trace-verify/main.go` 只是它的 **`CHECKS` 静态检查文件**（`TARGET` 单值约束第 42 条 ⇒ 一份集合只能有一个被测文件）。 | 已在 `d058b1ba` **同行就地改写**（行数不变）：写明「**不是**它的 `TARGET` —— 该集合的 `TARGET` 是 `internal/traceverify/guard.go`」。**本轮复算行号**：`grep -n '跨任务连带（E59③ 自查'` ⇒ 该段现在是 `:3189`（E72② 写得对：这是**当前实时行号**，E71③ 的纪律）。 |
| **E72③** | 同一份宿主挂起登记的**第 58 行**（「点读负载的形状」行） | 该行写「表里的 **m04–m11 共 8 条** … 全部断言红」，但「8」的**范围**没说清（读者可能把它当成整张表的条数 —— 那张表是 **11** 条），且表里**没有**任何一行写「8」。 | 已在 `d058b1ba` 就地补上范围：「**本子集**（表里的 m04–m11 共 8 条）」＋ 明确「该表的 m01–m03（`VmHWM` 解析三条）**不属于**本行这个子集」＋ 证据列写明「表里**没有**任何一行写『8』」。**本轮复算**：`grep -c '^| m' p4-task-9-vmhwm-authoritative-at-7740bdbf.txt` 的条数与该表 `变异总数 = 11` 一致 ⇒ 「8」确实只能是子集口径。 |

### E73（任务 11 交付轮：五条复审项 ＋ 一条自查订正）

> **本轮改动面（已核实，命令给出）**：**实现/测试面** = `tests/deploy/trace_runbook_test.go`（新，8 条顶层用例）＋ `docs/deployment.md`（追加运行手册一节）＋ `tests/tracesealer/footer_artifact_test.go`（新，E73① 的在册解析用例）＋ `tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst`（新，104 字节真实产物 fixture）＋ `tests/tracesealer/footer_roundtrip_test.go`（**只改文件头注释**：降级为产出方向人工复核项，E73①）；**集合面** = 新增 `tools/trace-mutate/sets/p4-task-11-runbook.py`，并就地改 `p4-task-10-footer.py`（`MANIFEST_FILES` / 推导命令 / `REQUIREMENTS` / `COVERAGE_GAPS`，E73①）与 `p4-task-4-runner.py`（m07 单操作化，E73②）；**计划面** = Task 10 文件清单补登记（E73③）＋ Task 11 步骤 5 段末补登记（E69②）＋ Task 11 的 D73 前置句在**同行就地补**一条事实订正（E73 自查⑥，**行数不变**）＋ 文末追加本节；**证据面** = `p4-task-11-runbook-*`（名单 / 先行红 / `--check` 定位表 / 权威表，各含 `4a9ffbf4` 与 `e854528c` 两个时点）＋ `p4-task-11-runbook-probe-at-e854528c.txt` ＋ **27 张**权威表 ＋ **27 份** `--check` 定位表（`e854528c` 上**全部** `p4-task-*` 集合各一份）＋ 探针 ＋ build/vet/test ＋ 宿主挂起登记（提交 `cc8c23b0` / `abad98ce`）。**冻结包 `internal/tracearchive/**` 零改动**（E73① 只调用它的导出 API）；`tools/trace-mutate/trace_mutate.py` 零改动（sha256 仍为 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`）；`atlas-migrations/**` 零改动（零迁移）。**提交数（自己数的）**：`git log --oneline 0bfb130f..HEAD | wc -l` = **91**；本轮 `git log --oneline 96a85eb0..HEAD | wc -l` = **5**（本节的提交是第 5 个）；本轮改动文件 = **9** 个（不含 `.superpowers/sdd/**` 的证据），含证据 = **71** 个（命令：`git diff --name-only 96a85eb0..HEAD [-- . ':(exclude).superpowers/sdd/*'] | wc -l`）。

| 编号 | 位置 | 问题 | 处置 |
|---|---|---|---|
| **E73①** | `tests/tracesealer/footer_roundtrip_test.go` 的 `TestFooterRoundTripOnRealArtifact` ＋ `tools/trace-mutate/sets/p4-task-10-footer.py` 的 `MANIFEST_FILES` / `COVERAGE_GAPS` | **E62 阻断（must-fix）**：那条用例承载的**行为**要求（「冻结包真的按这个布局写」）**不在任何 `MANIFEST_FILES` 里** ⇒ 自动门禁里没有它的名字。上一轮给的理由是「它要当场用 zstd 封一块，缺 zstd 的机器会 Skip ⇒ 塞进清单会让整张表判证据无效」—— **发起者已裁定：这不是正当理由**：**解析产物不需要 zstd**（冻结读侧用 Go 库 `klauspost/compress/zstd`），只有**产出**产物需要。 | **按 E62 修好，不给豁免**：① 用**冻结包的真 Sealer** 预产一份产物并**提交**（`tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst`，**104 字节**，sha256 `7f4291c592b71182a90bfa25953c648b3db5fd64383fc85af14752430e2af75e`，产出时点 `4a9ffbf4`、产出机 zstd v1.5.7，`3` 行正文，定位键 `dt=2026-10-08 / inst=blue / part=7`）；② 新增**不依赖 zstd** 的在册用例 `TestFooterParsesCommittedRealArtifact`（`tests/tracesealer/footer_artifact_test.go`）：钉 fixture 的 **sha256 与字节数**、用镜像 `tracesealer.RowCount` 读出封块时的行数、用**冻结读侧** `ta.NewReader.GetBody` **逐字节**取回三行原文、越界行号必须报 `ta.ErrNotArchived`；③ 把该文件登记进 **`p4-task-10-footer`** 的 `MANIFEST_FILES` 与推导命令（**更合适的集合**：它的 `TARGET` 就是承载这条镜像的 `internal/tracesealer/footer.go`；实测 m01/m02 改坏后该用例**出现在红名单里**，见 `p4-task-10-footer-authoritative-at-e854528c.txt`：`FAIL=2 | TestFooterParsesCommittedRealArtifact, TestFooterRowCountOnHandBuiltFooter`，names 7 → **8**）；④ 原 `TestFooterRoundTripOnRealArtifact` **保留**但降级为**产出方向**的人工复核项（文件头注释已改写，`COVERAGE_GAPS` gap-1 同步改写：**仍留在清单外的是产出方向**，它不承载镜像侧的任何判定 ⇒ **不再是 E62 缺口**）；⑤ 缺失/读不到产物必须 **FAIL 不许 SKIP**：探针 P4 把 fixture 移走后 `go test` 退出码 **1**、`--- FAIL`、**SKIP 行数 = 0**，写回后 sha256 与改前一致。 |
| **E73②** | `tools/trace-mutate/sets/p4-task-4-runner.py`（m07 条目 ＋ 文件头第 4 条 ＋ `REQUIREMENTS`） | **自相矛盾（must-fix）**：m07 的注释把杀手写成 `TestNewRunnerNeverCreatesRoot`，而同文件的记录说该变异「红成**编译失败**、未编译、**没有用例执行**」—— 按 **E64**「死于编译失败者不得被安杀手名」，这两句话不能同时成立。根因可复核：该集合原来用**两个操作**（先往导入块插 `"os"`，再插 `os.MkdirAll(...)`），前提是「`runner.go` 没有 `os` 导入」；而任务 10 的 `observeSealedPart` 让 `runner.go` **已经**导入 `os` ⇒ 再插一行 `"os"` 是**重复导入** ⇒ 编译红。 | **就地订正为「断言红 ＋ 真杀手」**：m07 恢复成**单个**插入操作（`_ = os.MkdirAll(opts.Root, 0o755)` 插在 `NewRootGuard` 之前），语义与计划逐字相同（「先建 root 再取锁」）；文件头第 4 条与 `REQUIREMENTS` 的 `note` 都写明这次订正与原因。**实测**（`p4-task-4-runner-authoritative-at-e854528c.txt`）：`m07_先建根目录再取锁 \| 红 \| names=13 ran=13 diff=0 PASS=12 SKIP=0 FAIL=1 \| TestNewRunnerNeverCreatesRoot` ⇒ **断言红 + 点名杀手**，且该表 `崩溃/超时/编译红 = 0 条`。 |
| **E73③** | 本计划 Task 10 的**文件清单**（**当前实时行号 `:2664-2669`**：`**文件：**` 在 `:2664`，清单最后一行「测试数据：…」在 `:2669`；补登记后本条不再随行号漂移） | 清单漏了 `internal/tracesealer/options.go` —— 任务 10 那一轮改了它（`MaxTrailerEntries` 的注释订正 **3 行**，行为零改动，见 `git show 2bba38ab -- internal/tracesealer/options.go`），而它在该轮的改动面里（其 `TARGET` 是 `p4-task-1-options`）。 | **就地补登记**：清单加一行「修改：`internal/tracesealer/options.go`（`MaxTrailerEntries` 注释订正，3 行，行为零改动）」；同时把 E73① 的两项产出（`footer_artifact_test.go` 与 `testdata/footer_roundtrip_real.jsonl.zst`）一并登记进该清单（**行数 +2**，见「自引用级联」）。 |
| **E73④** | 本计划末尾的登记节 | 计划里**没有 E72 节**（`grep -n 'E72'` = 0 命中）：任务 10 收尾轮（`d058b1ba`）只把三处订正写进**提交说明**，没写进计划 ⇒ 后续轮次无法从计划里逐条对照「位置 / 问题 / 处置」。 | **本轮补登记**：上面 **E72①–③** 三行（内容逐条来自 `d058b1ba`，**数字本轮自己从四张表重算**）。 |
| **E73⑤** | 本轮及此后一切含**计数**的陈述（提交数、表数、用例数、变异数、允许的绿数…） | 上一轮出现过「没自己数就写数」的失误（E72① 的「15 挂到四张表上」是同一族）。 | **订立约定（本轮起强制）**：凡报告/登记里出现的计数，**必须**先跑命令再写数字，并把命令与结果一并落盘（本轮的实例：`git log --oneline 0bfb130f..HEAD \| wc -l` = **91**、`git log --oneline 96a85eb0..HEAD \| wc -l` = **5**、27 张表的 `变异总数` 逐表相加 = **136 = 136 断言红 + 0 编译红 + 0 允许的绿 + 0 证据无效**（加法：7+2+3+6+3+8+5+7+7+7+8+4+4+3+2+4+1+14+6+2+2+5+4+4+3+4+11 = 136，其中第 8 项 7 是 `p4-task-11-runbook`）、`check_exit=0` 与 `table_exit=0` 各 **27 / 27**，见 `p4-task-11-build-vet-test-at-e854528c.txt` 与 `*-at-e854528c.txt`）。**歧义公开 ＋ 一处自查勘误**：`e854528c` 上的表是 **27 张**（= E59③ 要求重出的 **23** 张：12 个 `./tests/tracesealer/` 耦合 ＋ 11 个 `./tests/deploy/` 耦合；＋ 输入本轮零改动但仍重跑的 **3** 张：`p4-task-8-guard`、`p4-task-9-budget`、`p4-task-9-vmhwm`；＋ `p4-task-11-runbook` 自己在最终 HEAD 上的重出 1 张）。如实登记为「多跑」，**不**说成「按需最小重跑」。**勘误**：提交 `abad98ce` 的提交说明写的是「26 张权威表 ＋ 26 份 `--check` ＝ 129 = 129 断言红」—— 那是我漏数了同一次提交里一并落盘的 `p4-task-11-runbook` 那一张：**实际 = 27 张 / 27 份 / 136 = 136 断言红 + 0 编译红 + 0 允许的绿**（129 + 7 = 136）。提交说明**不改**（禁止 `--amend`）⇒ 勘误写在这里，并以 `*-at-e854528c.txt` 的 27 份落盘文件为准。 |
| **E73⑥（本轮自查发现）** | Task 11 背景第 2 条（当前实时行号 `:2740`）：D73 前置句「读侧（计划 3）**尚未实现**（`tracearchive.Reader` 当前只被测试引用）」 | **事实不成立**：读侧**代码**在 base（`main`）上**已经存在**并在 `cmd` 里接线 —— `internal/observability/service/trace_body_service.go`（计划 3/4 任务 2 引入）＋ `cmd/trace-body/main.go`；`git merge-base --is-ancestor <该文件引入提交> 0bfb130f` ⇒ **base 已含**。 | **只改事实、不改规格**：在该行**同行就地**补一条 E73 订正（**行数不变**）：读侧**代码**已就绪 ≠ 读侧**部署**可用（D73 是**部署**硬门），且写侧在部署配置里仍是**关闭**的（`deploy/blue-green/docker-compose.{blue,green}.yml:42` 的 `TRACE_BODY_ROOT` 都注释着；tick = `:38` 的 `TRACE_BODY_FLUSH_INTERVAL: 1m`）⇒ **步骤 4 仍然挂起**，理由与读数登记见 `p4-task-11-host-deferral-at-e854528c.txt`。**附带**：该文件里「解锁探针不会把闸门解开」这一点是**读码**推演（`spooler.go:206` 的 `guardProbe` 先验身份再试写 ⇒ 身份基线不匹配时探针直接返回身份错误 ⇒ 不会因「容器可写层能写」而解锁），**明确标注为读码、不是实跑**，真正的判定仍留给宿主。 |

> **跨任务连带（E59③ 自查，先自查后报告）**：`grep -n '^PACKAGES' tools/trace-mutate/sets/p4-task-*.py` ⇒ 本轮改了 `tests/tracesealer/**`（新 fixture ＋ 新用例 ＋ 一处文件头注释）与 `tests/deploy/**`（新用例文件）⇒ **与 `./tests/tracesealer/` 耦合的 12 个集合**（`p4-task-1-{classify,exit,options,round}`、`p4-task-10-{contract,footer,observability}`、`p4-task-2-alert`、`p4-task-3-{loop,rootguard}`、`p4-task-4-{lock,runner}`）＋ **与 `./tests/deploy/` 耦合的 11 个集合**（`p4-task-5-{build,ci,isolate,restore,unit}`、`p4-task-6-{blue,green,root}`、`p4-task-7-bluegreen`、`p4-task-8-{compose,runsh}`）**全部必须重出**（工具对包内**全部** `*_test.go` 取哈希）；改了两份集合文件自身（`p4-task-10-footer` / `p4-task-4-runner`）—— 它们已在这 23 个里。**共 23 个**（`grep -l '^PACKAGES.*tests/deploy' tools/trace-mutate/sets/p4-task-*.py | wc -l` = **11**，`grep -l '^PACKAGES.*tests/tracesealer' … | wc -l` = **12**，11 + 12 = **23**，与 E73⑤ 的「多跑 3 个」一起构成 26）。**新集合** `p4-task-11-runbook` 的输入（`docs/deployment.md` ＋ `tests/deploy/trace_runbook_test.go` ＋ 集合文件）在本轮**内容未变** ⇒ 它在 `4a9ffbf4` 的表仍然有效，但为口径统一**在最终 HEAD 上重出**了一份（`-at-e854528c`），两份都留在证据目录里。

### 新增约定（E73 起，**今后所有轮次适用**）

1. **一切计数先跑命令再落笔**：提交数用 `git log --oneline <base>..HEAD | wc -l`，表数用 `ls` / 逐表求和，用例数用 `go test -v | grep -c '^--- PASS'`，变异/断言红/允许的绿用**每张表的 `计数口径` 块**逐表相加并把加法写出（E73⑤）。
2. **「缺依赖」不是把行为要求留在自动门禁外的理由**：若一条行为要求只能由「需要某外部工具」的用例承载，正确做法是**把该工具产出的工件预先产好并提交**，再写一条**不依赖该工具**的用例去解析它，并把这个新用例登记进某份集合的 `MANIFEST_FILES`（E73① 的形态：`testdata/*.zst` ＋ `TestFooterParsesCommittedRealArtifact` ＋ `p4-task-10-footer` 的推导命令）。**只依赖该工具的「产出方向」可以继续留在人工复核项**，但**不得**让「解析正确」这类要求失去在册承载者。
3. **变异必须红成断言红；死于编译失败者不得安杀手名**：改任何变异字符串之前，先核对目标文件**当前**的导入与符号现状（E73② 的实例：任务 10 之后 `runner.go` 已导入 `os`，旧的两操作变异会重复导入 ⇒ 编译红）。交付报告里凡写「杀手 = X」，必须在同一轮的权威表里**能看到 X 出现在该变异的 FAIL 名单里**。
