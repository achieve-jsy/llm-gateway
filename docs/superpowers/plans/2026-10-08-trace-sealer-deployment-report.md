# 计划 4（正文封块器 sealer 与生产部署）交付报告

> 本报告是**计划 4 任务 12 的交付物**（计划：`docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md` 的「### Task 12」一节）。
> 分支 `feat/trace-sealer-deployment`；基点 `main=0bfb130f`；冻结提交 `3a2301ad`。
> **写作时点 HEAD = `73dae7c5`**（表头/哈希只记产出时点；E59：绑定 = 内容哈希）。
> 本仓库 CI **完全不跑** `go test` ⇒ **变异证据是唯一验证手段**。本报告里**每一个计数与退出码都能**在
> `.superpowers/sdd/2026-10-08-trace-sealer-deployment/` 的落盘工件里查到（E60）；查不到的一律不写（见 §11 的计数算式）。

## 0. 一句话结论

- **代码与契约面已交付、自动门禁全绿**：**27** 个变异集合在最终 HEAD 全量重出，`check_exit=0` / `table_exit=0`
  各 **27/27**；**136 = 136 断言支撑的红 + 0 崩溃/超时/编译红 + 0 允许的绿 + 0 未登记的绿 + 0 证据无效**；
  聚焦测试 **153 PASS / 0 FAIL / 0 SKIP**；冻结包 `internal/tracearchive/**` **零改动**。
  - **E76② 之后的当前值（与上面的 136 不矛盾）**：`p4-task-10-footer` 的变异数 **8 → 9**（新增 m09）
    ⇒ 全项目**当前**总量 = **137**（算式 136 − 8 + 9 = 137）。上面那个 `136` 是
    **Task 12 时点（`5650d28c` 的 27 张表）**的值，**不是**「当前值」—— 与 E73⑤ / E74① 同一纪律。
- **五项真实验收（A3 / A6 / A7 / A8 / 挂载消失 ≤1 tick）本轮一项都没有产生读数** —— 本机是 darwin/arm64，
  缺 Linux ＋ root ＋ 回环 ext4 ＋ docker 引擎 ＋ 真实 `/proc` ＋ 真实 PG ＋ zstd **1.4.8**。
  按 **E64** 口径全部**逐条登记为宿主执行清单**（§2、§6、§9），**不冒充、不用「形状」代替读数**。
- 本计划因此交付到「**挂载在场 ＋ sealer 就位 ＋ 功能关闭**」为止；**开启写侧**是 D73 硬门之后的**显式人工动作**。
- **「做完了代码」≠「验收通过」**：§9 的五条未关闭验收项**不得**因代码合并而勾掉。

## 1. 每个任务的提交哈希 ＋ 该任务全部集合的 sha256 绑定表

- **集合总数 = 27**（任务 5 是 5 个集合：build / ci / isolate / restore / unit）；
- 下表的 `set sha256` / `目标文件 sha256` 逐条取自本轮 27 张权威表的**证据绑定头原样**；
  完整头部（含每种 `*_test.go` 的 sha256、清单推导命令与比对、静态检查、映射表、逐条自证串、计数口径）
  见对应表文件本身（路径 = `.superpowers/sdd/2026-10-08-trace-sealer-deployment/<集合>-authoritative-at-5650d28c.txt`）。
- **本轮的集合与被测文件/测试文件哈希与上一轮（`-at-761c5a81`）逐条相同**（差异 **0** 条），
  因为本轮（任务 12）对实现/测试/集合的改动为 **0**（E59：纯计划文本提交不使绑定失效）；
  本轮重出是**任务 12 步骤 2 的独立要求**（在最终 HEAD 上全量重跑），不是「按需最小重跑」。

| 任务 | 实现提交（该任务落盘那一次） | 集合 | set sha256（前 12 位） | 变异数 | 目标文件 | 目标文件 sha256 |
|---|---|---|---|---|---|---|
| 1 | `12c8bd65` | `p4-task-1-classify` | `dfe471237134…` | 7 | `internal/tracesealer/classify.go` | `261fa923c60c5971def90f61d0c2b1eee0ee3a8976d0ffe94702b46cd36161e4` |
| 1 | `12c8bd65` | `p4-task-1-exit` | `69da6457f73e…` | 2 | `internal/tracesealer/exit.go` | `4204ee5e2684a97fbe95c8ac3f7179e5d417cee4eee4c7c9c7830bbc371b7cc4` |
| 1 | `12c8bd65` | `p4-task-1-options` | `2ab968c4210a…` | 3 | `internal/tracesealer/options.go` | `b3a0909d3169349a23b94e11289224c3de641fb1c67ce028bd7849593d1bfa2e` |
| 1 | `12c8bd65` | `p4-task-1-round` | `9535d64d3587…` | 6 | `internal/tracesealer/runner.go` | `dcf19c8e0491ff90fe223a38d1e5f539297bbdecec282ff090ae8e732945c9ab` |
| 2 | `959f5441` | `p4-task-2-alert` | `e34382604483…` | 7 | `internal/tracesealer/alert.go` | `4abf4a6ed0732d63c4a3457f60f8029b7ea8da3fbbd537de243b93ec743de115` |
| 3 | `0240d4b5` | `p4-task-3-loop` | `fc9894f6c6bc…` | 7 | `internal/tracesealer/runner.go` | `dcf19c8e0491ff90fe223a38d1e5f539297bbdecec282ff090ae8e732945c9ab` |
| 3 | `0240d4b5` | `p4-task-3-rootguard` | `818a1811de17…` | 8 | `internal/tracesealer/rootguard.go` | `ff0c9e85b8406b72f4221b8ecd82079e5aafa90be77a6f9d68695b1b34923699` |
| 4 | `fe70e02b` | `p4-task-4-lock` | `f107049422a6…` | 4 | `internal/tracesealer/lock.go` | `2835c163e3dda8d116ac8de72d496339bf85bbea211c099b97cf138482a281ff` |
| 4 | `fe70e02b` | `p4-task-4-runner` | `208cbc5e0059…` | 4 | `internal/tracesealer/runner.go` | `dcf19c8e0491ff90fe223a38d1e5f539297bbdecec282ff090ae8e732945c9ab` |
| 5 | `5d67595a` | `p4-task-5-build` | `a399e3fd134b…` | 3 | `Makefile` | `c1a1bbab40512e148fae98d0660d8608ce0fe1daa7261e6db09a0021880c3ec1` |
| 5 | `5d67595a` | `p4-task-5-ci` | `0930097fe9b3…` | 2 | `.gitlab-ci.yml` | `d6797599470656c4e79bc00f143c5240d991c20d5c349ecc0042545cd88eba7d` |
| 5 | `5d67595a` | `p4-task-5-isolate` | `8c32ae2c3ae9…` | 4 | `deploy/systemd/isolate-old-trace-archive.sh` | `3be89f11ef11ce3d21909cef85d79d1d7aefc307f019381485da95c59e72aba0` |
| 5 | `5d67595a` | `p4-task-5-restore` | `8dd06f372100…` | 1 | `deploy/systemd/restore-old-trace-archive.sh` | `50ea35c1ce5e6e98036a58a28ae7799dcd19513ef53781e43327380e09b8c57c` |
| 5 | `5d67595a` | `p4-task-5-unit` | `356e32be7257…` | 14 | `deploy/systemd/trace-sealer.service` | `6b64100976143211657f72f487ba33e28524259875cb278c8c58d78be5738ca2` |
| 6 | `35f9d42a` | `p4-task-6-blue` | `05dc5ca28e20…` | 6 | `deploy/blue-green/docker-compose.blue.yml` | `4de889fa98e4778928e2fa10f93f7ad54a91575e1842dfd9c0c62f82dd73e7f8` |
| 6 | `35f9d42a` | `p4-task-6-green` | `64914dc6e65e…` | 2 | `deploy/blue-green/docker-compose.green.yml` | `048ca0a7ddad893ad85259f57df549771c0fd19f8d5448e5166e8400630011e0` |
| 6 | `35f9d42a` | `p4-task-6-root` | `7a0875db89fd…` | 2 | `docker-compose.yml` | `7dd281034ceba32d56cc1ab6a88739b61b204e20b74008ba5cc3ef5877b18a44` |
| 7 | `38ebcf7e` | `p4-task-7-bluegreen` | `b62309b0de73…` | 5 | `deploy/blue-green/README.md` | `f4d9704ae50a79a09638e01074efa6fdd725a3c415a4980474589344593334a0` |
| 8 | `509c6c3b+ea8003b1` | `p4-task-8-compose` | `74fd5add7ff9…` | 4 | `deploy/verify/trace-a3/docker-compose.yml` | `9b4c93313522008341107fcca174669a75e8b3758bcf15324a1c98fb98952c48` |
| 8 | `509c6c3b+ea8003b1` | `p4-task-8-guard` | `2dbe4a5ce77c…` | 4 | `internal/traceverify/guard.go` | `d17822980703e86d89f7ab7899fab257cb6dbab61d1bc01f7eb820fcfd0a6695` |
| 8 | `509c6c3b+ea8003b1` | `p4-task-8-runsh` | `84f84b677b83…` | 3 | `deploy/verify/trace-a3/run.sh` | `c9aca492a69ab6e75b6bed8837024690ef75737cd79ed853189d618b9b3cce92` |
| 9 | `49baf6b2` | `p4-task-9-budget` | `ef39eb5dcf78…` | 4 | `internal/traceverify/budget.go` | `eef473b43d16bd68ee8473ed1ea58cb8d8e9207063c0bc705534c31069727e99` |
| 9 | `49baf6b2` | `p4-task-9-vmhwm` | `f7bdd9e3e939…` | 11 | `internal/traceverify/vmhwm.go` | `09726c96be4472d37c771fd502bbfdfb9cb643f0beb100e259a5aa70af7dbede` |
| 10 | `2bba38ab` | `p4-task-10-contract` | `10dfb5251428…` | 3 | `internal/tracesealer/options.go` | `b3a0909d3169349a23b94e11289224c3de641fb1c67ce028bd7849593d1bfa2e` |
| 10 | `2bba38ab` | `p4-task-10-footer` | `49a04f7f328e…` → **`035f45bf8870…`（E76② 重绑）** | 8 → **9** | `internal/tracesealer/footer.go` | `fa10f80dfc995b640c546566d8b501ab42a44af02101b3de6d1cf95b8c22cd15`（未变） |
| 10 | `2bba38ab` | `p4-task-10-observability` | `17582377948f…` | 5 | `internal/tracesealer/runner.go` | `dcf19c8e0491ff90fe223a38d1e5f539297bbdecec282ff090ae8e732945c9ab` |
| 11 | `4a9ffbf4` | `p4-task-11-runbook` | `c006add3a096…` | 7 | `docs/deployment.md` | `cde05ffbc546467ba881a343a3b2cdae5d5b525a96b8e3ba13a5880824c47fb2` |

（任务 12 自身不落任何实现/测试/集合：它的提交见 §11。）

> **E76② 重绑（E59）**：`p4-task-10-footer` 的集合文件在 **E76 收尾轮**被改（新增 `m09`）⇒ 按 E59
> **重跑重绑**，重出权威表与 `--check`：
> `.superpowers/sdd/2026-10-08-trace-sealer-deployment/p4-task-10-footer-authoritative-at-21df604a.txt`
> （`table_exit=0`；变异总数 **9** = 9 断言红 + 0 编译红 + 0 允许的绿 + 0 证据无效）与
> `p4-task-10-footer-check-at-21df604a.txt`（`check_exit=0`；9 条模式在 HEAD 上唯一定位）。
> 上表这一行的 `set sha256` / 变异数已按**新表**更新；`目标文件 sha256`（`footer.go`）**未变**
> （E76 对 `internal/**` / `cmd/**` **零改动**）。上表其余 **26** 行的 sha256 / 变异数**仍是
> `5650d28c` 表**的值（E59：只有内容变了的集合才需要重绑）。

## 2. 五项验收（A3 / A6 / A7 / A8 / 挂载消失 ≤1 tick）的真实输出

> **纪律**：任务 12 步骤 4 第 2 条要求「命令 ＋ 输出原样粘贴，含日期」。**本轮没有产生其中任何一项读数**，
> 因此这里**不粘贴任何数字**，只逐项登记「未执行 ＋ 前置条件 ＋ 谁在哪用哪条命令产出 ＋ 回填目标」
> （E64：不得用「本平台不可达」为**在册清单从未触达**的路径开脱；这里恰恰相反 —— 路径已全部在册，
> 缺的只是宿主任一次执行）。**逐条原文**见 §9 与四份 `*-deferral-*.txt`。

| 验收项 | 本轮状态 | 为什么（可复核） | 产出者与命令 | 回填目标 |
|---|---|---|---|---|
| **A3** 真实盘满（步骤 5 七项读数） | **未执行** | 需要 Linux ＋ root（`fallocate`/`mkfs.ext4`/`mount -o loop`/`losetup`）＋ docker 引擎；本机 darwin/arm64 全缺 | `deploy/verify/trace-a3/run.sh`（步骤 4/5/6/7/9） | §9-U1；`p4-task-8-a3-deferral-at-d1bc8e8d.txt` |
| **A7 进程级**（两个 `--once` 撞锁；边写边封） | **未执行**（**自动化半部已交付**：`p4-task-4-lock` / `p4-task-4-runner` 各 4 条断言红） | 进程级需要 A3 隔离实例 | `run.sh` 步骤 10（第二个 `--once` 必须退出码 1 ＋ 撞锁消息） | §9-U1 |
| **A6-①** 生产压缩比（zstd **v1.4.8**） | **未执行** | 本机 `/opt/homebrew/bin/zstd` = **v1.5.7**；规格 §11.1 明写 10.26× 是 v1.4.8 专属 | `trace-sealer --once --zstd-binary /usr/bin/zstd --expected-zstd-version 1.4.8`（先 `touch -d '5 hours ago'`，**不得**改 `--min-age`） | §9-U2；`p4-task-9-host-deferral-at-7740bdbf.txt` |
| **A6-②** Linux `/proc` 峰值标定（K=1/2/3 ＋ K=0 基线） | **未执行**（**形状**已由注入假 procfs 的 CLI 冒烟证过，**不是读数**） | 本机无 `/proc`；`decode-bench` 默认 procfs 路径在 darwin 上**响亮失败**（退出 1，刻意行为） | `go run ./cmd/trace-verify decode-bench --artifact <真实产物> --concurrency 1,2,3 --gc-modes on,off` | §9-U2/U5 |
| **A8** 行数分布（min/p50/p95/max ＋ `bytes_per_row`） | **未执行**（**观测面**已交付：每个成功块一行 INFO、每轮汇总带三个量、越过一半即 WARN） | 需要真实流量 ＋ 真实 zstd 1.4.8 | `grep 'sealed part' <日志>`（4.1）；人造 `too_many_rows` 样本用 A3 环境（4.4） | §9-U3；`p4-task-10-host-deferral-at-d0581bba.txt` |
| **挂载消失 ≤1 tick 闩锁** | **未执行 —— 前置条件不满足** | ① 写侧在部署配置里**关闭**（`deploy/blue-green/docker-compose.{blue,green}.yml:42` 的 `TRACE_BODY_ROOT` 两处都注释着）⇒ `sink == nil` ⇒ `TraceSpooler` **根本不构造** ⇒ 期望的 `root_unavailable`/`root-identity-mismatch` ERROR **恒不出现**，观察断言**恒真（空过）**；② 本机没有可 `umount` 的 `/datalog` | 撤挂载后连续观察 **≥3 个 tick**（`nsenter -m -t $(docker inspect -f '{{.State.Pid}}' llm-green-gateway) umount /datalog` ＋ `docker logs … | grep -E 'trace-spool.*(root_unavailable|root-identity-mismatch)'`） | §9-U4；`p4-task-11-host-deferral-at-e854528c.txt` |

**报告里必须与读数同现的两件留证（本轮**没有**读数，因此这两件也**没有**产出）**：

1. **「执行环境的流量为零」留证** —— 窗口内落向被验证颜色端口（8080/8082）的 nginx access 请求计数 = 0；
2. **闩锁期间累计丢失行数（期望 0）** —— 两种口径二选一或都抄：① 网关日志 `body_missing_rows=<n>` 的增量；
   ② 测试/A3 库上 `SELECT count(*) FROM call_traces WHERE body_missing IS TRUE AND created_at >= '<umount 时刻>' AND created_at < '<恢复时刻>'`。

**明确不声称**：本报告**不**声称 A1/A2/A4/A5 通过，**不**声称 A9（冷热分层）通过，**不**声称上述任一项「已验证」或「无问题」。
（D68 裁定：A2/A5 映射归计划 3、A4 归计划 1、A9 不在本计划范围。）

## 3. 跨计划契约 C1–C3（谁产生 / 谁消费 / 怎么验证）

> 读侧权威计划 = `docs/superpowers/plans/2026-09-25-trace-point-read-service.md`。本计划的 C1–C3 是**一致性复核**，不是新要求；
> 本轮**未发现**两者不一致（无需要发起者裁定之处）。

| # | 谁产生 | 谁消费 | 本计划侧的验证（在册承载者） | 诚实的效力边界 |
|---|---|---|---|---|
| **C1** 写失败 ⇒ 当前 part 改名成隔离件（同目录、后缀仍 `.jsonl.zst`、内容是明文 JSONL chunks）；读侧必须报 `ErrNotArchived`（500） | 网关（计划 2 `trace_event.go`） | 读侧（计划 3） | 运行手册一节 `TestRunbookDocumentsSealerHeartbeatAndPermanentFailure`（`tests/deploy/trace_runbook_test.go:249-257`）**逐字断言手册里出现 `ErrNotArchived`** ＋ 3 行静态检查；变异 `p4-task-11-runbook` m06/m07 | **只钉「门禁声明的存在性」**：自动用例钉不住「读侧真的会报 500」（那是计划 3 的用例）。运行时防护靠运行手册 ＋ 人工值守 |
| **C2** 构造失败/功能关闭 ⇒ `sink == nil` ⇒ 正文回落 PG jsonb 且 `body_missing=false` ⇒ **`body_ref_*` 全 NULL ＋ `body_missing=false` 必须被读侧解释为「正文在 jsonb」** | 网关（计划 2） | 读侧（计划 3） | `TestRunbookDocumentsFeatureOffIsByteIdentical`（`tests/deploy/trace_runbook_test.go:192-204`）：断言手册给出 `body_ref_part IS NULL AND body_missing = false` 的 SQL、必须同时给出 `total_rows`、必须写明 `jsonb` 与「`body_ref_*` 四列全 NULL」；变异 `p4-task-11-runbook` m03 | 同上：钉的是手册文本；**读侧必须有一条用例覆盖该组合**（计划 3 的 D55）—— 归计划 3 |
| **C3** 所有未封块 part 必须从 spool 读；`ErrTooManyRows` 的块**永久**留在 spool（永不封、永不删）⇒ 读侧不得假定「有 ref 就有产物」 | sealer（本计划） | 读侧（计划 3） | `TestClassifyTooManyRowsIsPermanent`（`classify.go` 的 `Permanent()`，变异 `p4-task-1-classify` m01/m06）＋ `TestRoundSeparatesPermanentFromRetryable`（`p4-task-1-round` m05）＋ `TestSummarizeTooManyRowsIsErrorAndNamed` / `TestRoundTooManyRowsEmitsErrorAlertLine`（`p4-task-2-alert` m04/m05）＋ `runner.go` 的 `KindTooManyRows` 分支只记日志、**不调用 Seal、不删 spool** | 这是**行为**承载（断言红，不是静态检查）。但「真实 spool 永不增长」仍需 A8 的真实读数（§9-U3）确认 |

**两条给读侧的实现约束（本计划只登记、不实现）**：① 读侧必须用 `body_ref_inst` 定位（**不得**用配置里的 `inst`）；
② 并发上限必须用 `internal/traceverify.MaxConcurrent`（控制面限额取蓝绿编排的 **2 GiB**，D72）。
**另**：**不得**把 C2 外推到闩锁路径（两种降级不对称，见 D73/I1）。

## 4. 规格缺陷登记 D64–D74 的实际处置

> 结论：**11 条全部照裁定执行，没有一条「改了主意」**。逐条给可复核证据；**唯一一处需要如实指出的偏差**见 D64。

| 编号 | 裁定 | 实际处置（可复核） |
|---|---|---|
| **D64** | sealer flag 只暴露 `--root/--zstd-binary/--zstd-level/--zstd-window-mb/--min-age/--expected-zstd-version/--poll-interval/--once/--lock-file`；**不出现** `--block-mb`/`--seal-interval` | **照做**：`cmd/trace-sealer/main.go` 的 flag 集**不含** `--block-mb`/`--seal-interval`（这二者是网关侧概念，生产由 `TRACE_BODY_ROLL_INTERVAL=1h` 覆盖）。**如实偏差**：实际 flag 集比 D64 的枚举**多两个**（`--seal-timeout`、`--version`）—— D64 的那句是**非穷举**举例，实质（不出现「封块间隔/块大小」这类会误导的伪参数）成立；本报告据实登记 |
| **D65** | 隔离旧定时器是启用写路径的前置条件；脚本默认 dry-run ＋ 删除谓词审查（命中新树即 exit 3）＋ 独立回滚；`--confirm` 前必须先过**停止点 1.5**（R1 量化） | **照做**：`deploy/systemd/isolate-old-trace-archive.sh` ＋ `restore-old-trace-archive.sh` 已落盘，由 `p4-task-5-{isolate,restore}` 共 **5** 条断言红钉住（含 dry-run 默认、`--confirm`、删除谓词命中新树 exit 3）。**停止点 1.5 本轮未执行**（需要生产机两次 `du -sb /datalog`，间隔 ≥24h）⇒ R1 量化仍挂起（§8） |
| **D66** | A3 的**真实盘满**只在专用回环 ext4 上做，主机优先测试环境；`GuardScratchTarget` 在**挂载成功之后**证明与 `/`、`/datalog` 不同设备；生产只做非破坏性验证 | **照做**：`internal/traceverify/guard.go` ＋ `deploy/verify/trace-a3/run.sh` 已落盘（`p4-task-8-guard` 4 条、`p4-task-8-runsh` 3 条断言红）；本机真的拒过 `--size 1T`（容量预检），真实填盘归宿主（§9-U1） |
| **D67** | A8 按**行数分布 ＋ 越界分类与告警**实现；「目录占用增量 == 0」属读侧 | **照做**：`runner.go` 的 `observeSealedPart`（每块 INFO ＋ 每轮汇总 ＋ 越过一半 WARN），`p4-task-10-observability` 5 条断言红 |
| **D68** | 以计划 2 任务 8 的 A 表为准；本计划承担 A3/A6/A7/A8 ＋ 新增挂载消失；**A9 明确不在范围**；不得宣称 A1/A2/A4/A5 | **照做**：本报告 §2 与 §0 明确列「不声称」清单 |
| **D69** | gateway **读写**挂 `/datalog`；control-plane **只读**挂；sealer 在宿主机直接用 | **照做**：三份编排都是长语法 bind，cp 侧多了 `read_only: true`（`p4-task-6-{blue,green,root}` 共 10 条断言红） |
| **D70** | 部署前 `findmnt -T /datalog` 必须显示 `TARGET=/datalog`；`install -d` 只在确认挂载之后人工执行；compose 一律长语法；sealer 与网关**都不得**创建归档根 | **照做**：`grep -rnE '\bMkdir[A-Za-z]*\s*\(' internal/tracesealer/ cmd/trace-sealer/` ⇒ **无命中**（步骤 1 ③）；`install -d` 的正确次序写进 `deploy/blue-green/README.md`，由 `p4-task-7-bluegreen` 5 条断言红钉住 |
| **D71** | unit/compose/剧本/运行手册一律不出现 `TRACE_BODY_SEAL_INTERVAL`；四处测试钉住 | **照做**：`grep -rn 'TRACE_BODY_SEAL_INTERVAL\|SEAL_INTERVAL' deploy/ docs/deployment.md` ⇒ **无命中**（步骤 1 ⑥）。该串在别处**故意**出现（集合里作为「变异插入以证明被拒」、测试里作为「断言不含」、计划/规格里作为 D64/D71 的登记正文、`.env.example:50` 是注释） |
| **D72** | 以 `deploy/blue-green/docker-compose.{blue,green}.yml` 为准（CI 生产上线走它）；C5 预算取 control-plane **2 GiB**；三处不一致只登记不修 | **照做**：两文件的两处 `memory: 2g`（`:59`、`:108`）为 C5 输入；`internal/traceverify/budget.go` 的 `MaxConcurrent` 取 2 GiB；`docs/deployment.md` / `docs/guardrails.md` / `config/prod.yaml` **未改** |
| **D73** | **功能开启（设置 `TRACE_BODY_ROOT`）以「同一 root 的读侧可用」为硬门前置**；本计划只交付到「挂载在场 ＋ sealer 就位 ＋ 功能关闭」 | **照做**：两个颜色的 `TRACE_BODY_ROOT` 在 compose 里**保持注释**（功能关闭）；开启步骤写成带确认点的独立步骤（`deploy/blue-green/README.md`），并由 `p4-task-11-runbook` 的 m06 钉住「前置条件段落必须存在」 |
| **D74** | 首轮 sealer 以 **root** 运行 ＋ `ProtectSystem=strict` ＋ `ReadWritePaths=/datalog`；「改用专用用户」登记为后续项 | **照做**：`deploy/systemd/trace-sealer.service:15` `User=root`、`:57` `ProtectSystem=strict`、`:58` `ReadWritePaths=/datalog`（`p4-task-5-unit` 14 条断言红钉住整份 unit 文本）；专用用户仍是**未关闭后续项**（需先实测容器 uid） |

**本轮没有发现「改了主意」的条目。** 唯一需要点名的偏差是 D64 的 flag 枚举**非穷举**（多出 `--seal-timeout` / `--version`），且**不改变** D64 的裁定实质。

## 5. 诚实的覆盖缺口

> 这一节是本报告里**最该被评审逐条核对**的部分。凡「只说静态检查」的都**不是**行为验证。

1. **只有静态检查承载的要求**（本计划自检已登记）：① 「A7 的**次序不变量**」与「**心跳每轮必打**」——
   被测对象是**冻结包**与**日志**，本计划**不能变异冻结包** ⇒ 只有 `CHECKS` 静态检查（`absent`/`present` 正则）。
   （`RoundResult.RootUnavailable` 已由 **E63⑧** 新增的**行为**用例 `TestLoopReportsRootUnavailable` 承载，**不再**是弱覆盖。）
2. **靠替身而非真实 zstd 的用例**：`tests/tracesealer/*` 的多数轮次/告警/观测用例用**注入的假 Sealer/假时钟**驱动，
   只证明**形状**（字段、级别、阈值、零值语义），**不**产生真实产物；真实 zstd 路径只在
   `TestFooterRoundTripOnRealArtifact`（**产出方向**，本机有 zstd 才跑；缺 zstd 会 Skip ⇒ **故意不进任何 `MANIFEST_FILES`**，
   只作人工复核项）与 `TestFooterParsesCommittedRealArtifact`（**解析方向**，在册，不依赖 zstd）出现。
3. **只能在生产取（因本机 zstd 是 v1.5.7）的结论**：A6-① 的压缩比、A1 的百万级读回、A8 的行数分布、
   R1 的 `/datalog` 增长速率、A3 的盘满读数、挂载消失的运行时读数。**本报告一处都没有给它们数字。**
4. **没有任何自动用例覆盖的路径**：① A3 真实填盘与恢复；② 进程级 A7（两个**真** `--once` 撞锁）；
   ③ 挂载消失的观察（≥3 tick）与「累计丢失行数」；④ 旧定时器隔离脚本的**真机执行**（只由契约用例 ＋ dry-run 钉住）；
   ⑤ D73 硬门本身（部署决策，非代码）。
5. **`fixture` 的事实登记（如实登记：一条真实缺口（E74②）＋ 一条**E76③ 已收窄/关闭**的旧登记）**：
   - **E74②：`tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst` 的「产出过程」不可自动复现。**
     仓库里没有任何脚本/输入能重建这 104 字节 —— 复现它需要「真 Sealer ＋ 与本机版本一致的 zstd ＋ 当时的输入行」，
     而这两样都不在仓库里。**可自动验证的只有解析方向**（在册用例 `TestFooterParsesCommittedRealArtifact`
     钉 sha256 `7f4291c592b71182a90bfa25953c648b3db5fd64383fc85af14752430e2af75e` ／ 104 字节，
     用镜像 `RowCount` 与冻结读侧 `GetBody` 读回 3 行、越界行报 `ErrNotArchived`）。冻结包不可变 ⇒ 该形态**可接受**，
     但**不得**暗示它可复现。**来源**：真 Sealer 预产、产出机 zstd **v1.5.7**、产出时点 `4a9ffbf4`、
     定位键 `dt=2026-10-08 / inst=blue / part=7`。
   - **本轮探针 P3 的负结果（**E76③ 收窄**）**：该 fixture 的 `trailer_len` 与 `row_count` **都等于 3**
     （末尾 32 字节：`… 0000 0003 0000 0003 …`）⇒ 把 `footer.go` 第 107 行的字段偏移由 `b[18:22]` 改成 `b[14:18]`，
     **仅在 `-run TestFooterParsesCommittedRealArtifact` 的单测口径下**读不出差别、**该单测**仍绿 ——
     即「**这个 fixture 的单测**区分不了 trailer_len 与 row_count」，**不是**「这条判据不可证伪」。
     **包级已由 `TestFooterRowCountOnHandBuiltFooter` 承重**：手工拼的 footer 里 trailer_len = 0
     ≠ row_count = 4242 ⇒ 换偏移后解析值 0 ≠ 4242 ⇒ **断言红**。评审口径的复核（`go test -overlay`
     把 `footer.go:107` 改成 `b[14:18]` 跑**整包**）⇒ **exit = 1、6 条顶层 FAIL**；本报告 E76② 以同口径
     复跑并**自己数了一遍**：整包 73 条顶层用例 ⇒ RUN=73 / PASS=67 / **FAIL=6** / SKIP=0 / 编译错误行=0
     （落盘 `e76-m09-wholepackage-at-21df604a.txt`，73 − 6 = 67 ✓）。
     E76② 已把这条判据**正式在册**（集合新增变异 `m09_row_count偏移改成trailer_len偏移` ＋ 点名杀手
     `TestFooterRowCountOnHandBuiltFooter`，见 `p4-task-10-footer-authoritative-at-21df604a.txt` 的 m09 行：
     红 / names=8 ran=8 diff=0 PASS=6 SKIP=0 FAIL=2）。⇒ **不再**把它称作「不可证伪的覆盖缺口」
     （此前**不**记成断言红、**不**安杀手名，是因为当时只跑了那条单测，不是因为该判据没有承载者）。
     （镜像与冻结包的其余一致性由手工拼 32 字节 footer 的用例 ＋ `FooterSize` 常量契约 ＋ 真产物解析共同钉住。）
6. **生产门槛值**：压缩比门槛（9× / 10.26×）与水位换算**逐字取自规格**，本计划**不代抄**、也不替判定人下结论。

## 6. 生产实测事实（当天重取）与旧定时器处置

> 任务 12 步骤 4 第 6 条要求「当天重取的 `df -h` / `findmnt` / `zstd --version` / `.active-color` ＋ 旧定时器
> `is-active`/`is-enabled` 输出」。**本节一条都没有产出** —— 本机没有生产宿主、没有 `/datalog`、没有 `/usr/bin/zstd`。
> **不得**把下面这些**计划登记的基线值**当作本轮实测：

| 项目 | 状态 | 要求 |
|---|---|---|
| `df -h /datalog` | **待宿主执行**（当天实测） | 报告里必须给出**当天**实测值；计划登记的基线是 `407G 已用 / 153G 可用`（合计 560G，**72.7%**，2026-10-08），**引用时必须标注日期** |
| `findmnt -T /datalog --output TARGET,SOURCE,FSTYPE` | **待宿主执行** | 必须显示 `TARGET=/datalog` 且 `FSTYPE` 是真实文件系统（D70） |
| `/usr/bin/zstd --version` | **待宿主执行** | 期望 **v1.4.8**（本机是 v1.5.7） |
| `.active-color` | **待宿主执行** | 蓝绿活跃颜色 |
| 旧定时器：`systemctl is-active trace-archive.timer` / `is-enabled` | **待宿主执行** | 隔离前后各一次；隔离脚本 `deploy/systemd/isolate-old-trace-archive.sh`（默认 dry-run，`--confirm` 才动手） |
| 旧定时器处置记录（选了哪个 R1 选项、为什么） | **待宿主执行** | 必须在 `--confirm` **之前**选定并写进上线记录（R1 的选项 1/2/3） |

`zstd --version`（本机，**不是**生产值，仅用于说明「为什么 A6-① 不能在本地做」）：

```
$ zstd --version
  *** Zstandard CLI (64-bit) v1.5.7, by Yann Collet ***
```

## 7. A8 决策门的结论

**结论：本轮**无法**给出结论 —— 三条决策门一条都没触发，因为**没有真实读数**（§2 的 A8 行）。**

- 判据的**观测面**已交付且可失败（`p4-task-10-observability` 5 条断言红：越过 `MaxTrailerEntries/2` 即 WARN、
  取最小 B/行、取最大行数/最小压缩比、读不出行数不得折成一个读数、每个成功块一行 INFO）。
- **未触发 ⇒ 本轮**没有**改 `internal/gateway/observability/spooler.go`**，也**没有**重跑 `p2-task-5` / `p2-task-8-spooler`
  （预防性修改会越过任务 10 的边界并与计划 2 的既有集合脱钩）。
- 三条门（逐字）：`max(rows) < 4,194,304`（> 2 倍余量）⇒ **不改**代码；`4,194,304 ≤ max(rows) < 8,388,608`（50%–100%）
  ⇒ 在运行手册登记「需要按行数滚动」并给建议参数；**`max(rows) ≥ 8,388,608`（≥ 上界）⇒ 必须改 `spooler.go`
  增加按行数滚动，并在同一提交里重跑 `p2-task-5` / `p2-task-8-spooler` 两个集合并重记 sha256**。
- 归属：§9-U3（任务 10 的 4.1 / 4.4）。

## 8. 风险登记 R1 / R2 的处置证据

### R1 —— `mask trace-archive.timer` 会让 `/datalog` 在隔离窗口内**没有任何 retention**

| 要求的证据 | 状态 | 说明 |
|---|---|---|
| 停止点 1.5 的**两次 `du -sb /datalog`**（间隔 ≥24h）＋ 外推的增长速率与可撑天数 | **待宿主执行** | 需要生产机两次采样；本机没有 `/datalog` |
| 最终选了哪个选项（mask / 保留清理 / 另设 unit）＋ 理由 | **待宿主执行** | 必须在 `--confirm` **之前**选定并写进上线记录 |
| 可执行面 | **已交付** | `deploy/systemd/isolate-old-trace-archive.sh`（默认 dry-run、删除谓词审查、命中新树 exit 3）＋ `restore-old-trace-archive.sh`（回滚）；`p4-task-5-{isolate,restore}` 共 **5** 条断言红；次序（任务 6 挂载核对 → 任务 5 隔离）写进 `deploy/blue-green/README.md`，由 `p4-task-7-bluegreen` 钉住 |

**R1 未关闭** —— 它是「隔离窗口内 `/datalog` 无 retention」这条**唯一可能失控的路径**，量化门（停止点 1.5）**必须在 `--confirm` 前过**。

### R2 —— A3 的镜像约 1 GiB 落在 `/srv` 所在文件系统，`GuardScratchTarget` 只护挂载点、不护镜像本身

| 要求的护栏 | 状态 | 证据 |
|---|---|---|
| ① **容量预检**：`fallocate` **之前**对镜像所在 FS 取 `statfs`，断言 `可用 >= size + 1 GiB`，不足即拒（**不得**用 `dd` 兜底） | **已交付** | `p4-task-8-guard` 的 `CHECKS` 钉住 `internal/traceverify/guard.go` 里的预检形态；本机 CLI 冒烟真的拒过 `--size 1T`（89 GiB 可用盘，退出码 1） |
| ② **设备检查**：镜像路径也要判「不得与 `/` 同设备」，或显式承认并写明 `size` 上界与可用空间 | **已交付** | 同上（guard 的 `CHECKS` ＋ `p4-task-8-compose` 的编排断言） |
| ③ **执行后**打印 `df -h $(dirname /srv/trace-a3.img)` 并抄进 A3 证据 | **待宿主执行** | 属 A3 宿主读数（§9-U1） |
| ④ **不得**让 `/srv` 与 `/datalog` 同设备 | **已交付**（断言在 guard 实现里 present） | `p4-task-8-guard` 4 条断言红（含「与 `/`、`/datalog` 同设备即拒」） |

**R2 的护栏已落盘并可失败；真实读数（③）随 A3 一起挂在宿主上。**

## 9. ★ 未关闭验收项（**只做代码不算完成**，必须在真实宿主补做）

> **本节与计划文末的「交接：未关闭验收项」逐条一致**。它们**不会**因为代码合并、门禁全绿或本报告而关闭。
> **不得**用「本机跑不了」代替「不需要跑」（E64）；本机能交付的只有**契约面**（改坏 ⇒ 转红），**不是**读数。

### U1　任务 8 —— A3 步骤 5 七项读数 ＋ **进程级 A7**
- **宿主**：**Linux ＋ root**（`fallocate`/`mkfs.ext4`/`mount -o loop`/`losetup`/`/proc`）＋ **可用 docker 引擎** ＋ 测试环境 PG/Redis ＋ 必然拒连的上游（`base_url: http://127.0.0.1:9`）；按 `deploy/verify/trace-a3/run.sh`。
- **待执行（7＋1）**：① 填盘前后 `df -h /srv/trace-a3`；② 单条闩锁 `ERROR` 原文（**只出现一次**）；③ `body_missing` 计数 vs 总数；④ `/health`、`/ready` 返回码；⑤ 延迟直方图前后对比（**由人判定**）；⑥ 恢复后的新 spool 列表；⑦ `scratch-cleanup` 后 `findmnt` 为空、镜像已删；⑧ **进程级 A7**：第二个 `--once` 退出码 **1** ＋ 撞锁消息。
- **回填目标**：本报告 §2 的 A3/A7 行 ＋ `p4-task-8-a3-deferral-at-d1bc8e8d.txt`。

### U2　任务 9 —— A6-① 生产（v1.4.8）压缩比、A6-② Linux `/proc` 峰值标定（K=1/2/3 ＋ K=0 基线）、A1 的 n=1e6 回读
- **宿主**：**生产宿主**（`/usr/bin/zstd --version` = **v1.4.8**）＋ **Linux `/proc/<pid>/status`** ＋ 真实 PG ＋ 真实封块产物。
- **待执行**：① A6-① 用 `touch -d '5 hours ago'`（**不得**改 `--min-age`）后 `trace-sealer --once`，抄 per-part `ratio` 与汇总，并与规格 §16 第 7 条门槛**并列**；② A6-② `decode-bench --concurrency 1,2,3 --gc-modes on,off`，**先**取 K=0 空读 RSS 作基线；③ A1 固定 seed 抽 **1,000** 行**逐字节**比对（比对源 = **封块前 spool 行的原字节**，D57），覆盖 ≥2 part / ≥2 dt / 1 越界（`ErrNotArchived`）/ 1 条 `body_missing=true`（`ErrBodyMissing`），**1000/1000 相等，不得 SKIP，不得降低抽样数**。
- **回填目标**：§2 的 A6/A1 行 ＋ 交接 **H5 / H10**（回填计划 3 的 `TRACE_BODY_READ_MAX_CONCURRENT`）；`p4-task-9-host-deferral-at-7740bdbf.txt`。

### U3　任务 10 —— 4.1 真实流量行数分布、4.4 A3 人工构造 `too_many_rows` 样本（三条决策门）
- **待执行**：① 4.1 `grep 'sealed part' <日志>` 抽全部 `rows` 算 min/p50/p95/max 与 `bytes_per_row`；② 4.4 在 A3 环境把 `TRACE_BODY_BLOCK_MB` 调到极小 ＋ 平均行 < 192 B，**人工构造**一条 `too_many_rows` 样本（**禁止**在生产复现）并抄日志原文；③ 按 §7 的三条门处置（第三条门触发时**必须**改 `spooler.go` 并重跑 `p2-task-5` / `p2-task-8-spooler`）。
- **回填目标**：§7 ＋ `p4-task-10-host-deferral-at-d0581bba.txt`。

### U4　任务 11 —— 4.3 挂载消失实测（**当前不可执行，不是通过**）
- **宿主**：生产（或 A3）宿主上**已启用写侧**（`TRACE_BODY_ROOT` 非空且 root 目录在场）的**非活跃、已摘流**颜色；**只允许**测试环境或**无真实流量**的生产窗口（该实验本身在持续丢正文）。
- **待执行**：撤挂载后连续观察 **≥3 个 tick**：① 闩锁 ERROR **只跳变一次**；② spool **始终没有**新文件；③ 此后每行 `body_missing=true` 且 `body_missing_rows` **单调**递增；④ **累计丢失行数**（期望 **0**；非 0 ⇒ 按事故复盘且不得作为验收证据）；⑤ 「窗口内流量为零」留证；⑥ 活跃颜色 `/health` 仍 200；⑦ 重启后恢复写盘；⑧ 停止点 A（禁止切换蓝绿流量）人工值守。
- **为什么现在不能做**：写侧功能关闭 ⇒ `sink == nil` ⇒ `TraceSpooler` 不构造 ⇒ 期望的 ERROR **恒不出现**，观察断言**恒真（空过）**。⇒ 登记为「**读侧就绪（D73 硬门）后的启用门禁**」，**不得**宣称已通过。
- **回填目标**：§2 的挂载消失行 ＋ `p4-task-11-host-deferral-at-e854528c.txt`。

### U5　跨计划依赖 —— A6-② 在 `GOGC=off` 下 K=2 的停止点
- **判据**：若 **K = 2** 的峰值 **≥ 2 GiB** ⇒ 结论取 **1**，且**必须**把**计划 3**（`docs/superpowers/plans/2026-09-25-trace-point-read-service.md`）的 `TRACE_BODY_READ_MAX_CONCURRENT` **由 2 改为 1**（或提高控制面限额）。
- **状态**：**未触发（也没有读数）**。本轮只有算术面：`PerReadPeakBytes() = 512 + 64 + 64 = 640 MiB`、`MaxConcurrent(2 GiB, 300 MiB, 256 MiB) = 2`（`(2048−300−256)/640 = 2.33` 向下取整）。**这两个数字不是结论**。
- **回填目标**：U2 之后 ＋ 计划 3 的 H15 / 本计划的 H5 / H10。

### U6　风险 R1 —— 停止点 1.5 未执行 ⇒ R1 的量化仍挂起（**唯一可能失控的路径**）
- **判据**：隔离旧定时器（`isolate-old-trace-archive.sh --confirm`）会让 `/datalog` 在隔离窗口内**没有任何 retention**（`mask trace-archive.timer`）⇒ **`--confirm` 之前必须先过停止点 1.5 的量化门**：在生产机对 `/datalog` 取**两次 `du -sb`（间隔 ≥ 24h）**，外推增长速率与「还能撑几天」，并在 R1 的三个选项（mask / 保留清理 / 另设 unit）里**显式选定一个**写进上线记录。
- **状态**：**未执行 ⇒ R1 的量化仍挂起**（§8）。本机没有生产 `/datalog` ⇒ 两次采样（间隔 ≥ 24h）只能在生产宿主机做；**可执行面已交付、量化门未过**（**不得**用「本机跑不了」代替「不需要跑」，E64）。
- **为什么单列**：它是**唯一可能失控的路径** —— 其余未关闭项是「读数未取」（U1/U2/U3、R2 的执行后 `df -h`）或「纵深防御未验证」（`RequiresMountsFor=` 在手工 `umount` 时是否可靠）；只有 R1 的量化门**不过就可能真的把盘写满**。已记载在 **§8（R1 未关闭）**、**§13 第 1 条**与 **§4 的 D65**。
- **回填目标**：§8 的 R1 表（两次 `du -sb` 读数 ＋ 选定选项与理由）＋ 上线记录；并把对应宿主挂起登记改为「已执行（附读数）」。

## 10. 本轮落盘工件索引（任务 12）

全部在 `.superpowers/sdd/2026-10-08-trace-sealer-deployment/`，**HEAD 后缀一律 `-at-5650d28c`**：

- **步骤 1（机械核对）**：`p4-task-12-step1-mechanical-checks-at-5650d28c.txt`
- **步骤 2（27 集合全量重跑）**：
  - 权威表 **27** 份：`p4-task-12-<集合>-authoritative-at-5650d28c.txt`（父指令命名）
    与同内容的规范名 **27** 份：`<集合>-authoritative-at-5650d28c.txt`；
  - `--check` 定位表 **27** 份：`p4-task-12-<集合>-check-at-5650d28c.txt` ＋ 规范名 **27** 份：
    `<集合>-check-at-5650d28c.txt`；
  - 每份都带 `check_exit=` / `table_exit=`（E60）。
- **步骤 3（构建/vet/测试）**：`p4-task-12-step3-build-vet-test-at-5650d28c.txt`
- **E62 探针**：`p4-task-12-probe-at-5650d28c.txt`
- **名单清单（仓内，非 /tmp）**：`<集合>-names.txt`（27 份，上一轮已落盘，本轮按各自 `MANIFEST_DERIVATION` 现推核对）

## 11. 计数（**全部先跑命令再落笔**；E60/E73⑤/E74①）

**A. 变异证据（本轮 27 张权威表逐表相加）**

```
变异总数逐表（**27 项**，项序 = 27 个集合名的 `ls` 字典序）：
  7+2+3+6+3+8+5+7+7+7+8+4+4+3+2+4+1+14+6+2+2+5+4+4+3+4+11
             = 136
  【E76① 订正】原串只有 **26** 项、实和 = **129**（136 − 7 = 129），却写成「= 136」——
  漏掉的是第 **10** 项 **`p4-task-3-loop` = 7**（第 8 项 `p4-task-11-runbook` = 7、
  第 9 项 `p4-task-2-alert` = 7、第 11 项 `p4-task-3-rootguard` = 8 三项都在场）。
  订正时点 = **E76 收尾轮**（本报告追加「## 14. E76」一节的那次提交）。
  订正依据（E60）：对 `p4-task-12-<集合>-authoritative-at-5650d28c.txt` 的 `变异总数` 逐表
  按 `ls` 字典序取值，machine 加法得 terms=27 / sum=136（落盘
  `e76-addition-derivation-at-5650d28c.txt`）。
  **结论不变**：headline **136** 本来就是对的 —— 错的是算式的项数与和数，不是总量。
断言支撑的红 = 136     崩溃/超时/编译红 = 0     允许的绿 = 0
未登记的绿   = 0       证据无效 = 0
check_exit=0 : 27 / 27   table_exit=0 : 27 / 27
--check 唯一定位：27 份定位表里「全部 N 条变异的模式都在 HEAD 上唯一定位」的 N 逐份相加 = 136
名字清单条数（每表 `名字清单条数` 逐表相加）= 191（27 份清单；清单列的是包内**全部**顶层用例，
   工具每次运行都真的执行它们 ⇒ 每次运行的断言面比变异数大）
清单推导比对「一致」 = 27 / 27
全集标记「本次运行 == 全集 ✓」 = 27 / 27；子集标记「这是子集」 = 0
本轮（任务 12）的提交（**枚举、不写「共 N 个」** —— 与 E74① 同一纪律：任何一次后续文档微调都会让计数失效，
   一律以当场 `git log --oneline 5650d28c~1..HEAD` 为准）：`5650d28c`（E74 计划收口）→ `73dae7c5`（证据）
   → 交付报告提交 → （若有）报告文本微调提交
```

> **E76② 更新（E76 收尾轮的当前值；与上面的 136 不矛盾）**：E76② 把 **`p4-task-10-footer`** 的变异数
> 由 **8 → 9**（新增 `m09_row_count偏移改成trailer_len偏移`，点名杀手 `TestFooterRowCountOnHandBuiltFooter`）。
> **当前**全项目总量 = **137**。两种算法互相印证（E60）：
> ① 逐表增量法：`136 − 8 + 9 = **137**`；
> ② 27 项重加法：把上面那个 27 项算式里的**第 6 项 `8`（= `p4-task-10-footer`）换成 `9`**，
>    machine 加法得 `terms=27 / sum=137`（落盘 `e76-addition-derivation-at-21df604a.txt`）。
> 该集合重绑后的权威表：`p4-task-10-footer-authoritative-at-21df604a.txt`（`table_exit=0`；
> **9 = 9 断言红 + 0 崩溃/超时/编译红 + 0 允许的绿 + 0 未登记的绿 + 0 证据无效**）与
> `p4-task-10-footer-check-at-21df604a.txt`（`check_exit=0`）。
> 上面那个 `136` 是 **Task 12 时点（`5650d28c` 的 27 张表）** 的值 —— `check_exit/table_exit 27/27`、
> `= 136`、`清单条数 191` 都**只对那个时点**成立；E76 之后这四项的当前值见本块与 §14。

**B. 测试**

```
go test ./tests/tracesealer/... ./tests/traceverify/... ./tests/deploy/... -count=1 -v
  顶层 PASS = 153    顶层 FAIL = 0    SKIP = 0    包级 ok = 3    包级 FAIL = 0    no tests to run = 0
  focused_test_exit = 0
go test ./internal/tracearchive/ -count=1 -v
  顶层 PASS = 31     顶层 FAIL = 0    tracearchive_test_exit = 0
go build ./internal/... ./cmd/...      build_exit = 0
go vet <5 聚焦包>                      vet_exit   = 0
gofmt -l <聚焦目录>                    空
```

**C. 基线失败（**不是**本计划造成）**

```
go build ./...  build_all_exit = 1  → test/bedrock_model_id_check.go:98:23: undefined: bedrock.NewListFoundationModelsPaginator
go vet   ./...  vet_all_exit   = 1  → tests/contract_test / tests/gateway/orchestrator_test /
                                      tests/gateway/handler_test / tests/controlplane/handler_test / test
归因：本计划区间 0bfb130f..HEAD 对这些包的改动 = 0 行（tests/contract / tests/gateway / test / tests/controlplane 全部 0）
```

**D. 范围与提交（写作时点 = `73dae7c5`，**不含本报告这一次提交**）**

```
git log --oneline 0bfb130f..73dae7c5 | wc -l                      = 99   （全计划）
git log --oneline 5650d28c..73dae7c5 | wc -l                      = 1    （本轮任务 12：证据提交；＋ 5650d28c=E74 ＋ 本报告 ⇒ 3）
git diff --name-only 0bfb130f..73dae7c5 | wc -l                   = 549  （含证据）
git diff --name-only 0bfb130f..73dae7c5 -- . ':(exclude).superpowers/sdd/*' | wc -l = 79
   同上再排除 docs/*                                               = 77   （代码/产物面）
本轮（任务 12）新落盘工件 = 111 个（步骤 1 ×1 ＋ 权威表 ×27×2 ＋ check ×27×2 ＋ 步骤 3 ×1 ＋ 探针 ×1）
   加法：1 + 54 + 54 + 1 + 1 = 111 ✓
```

> **E74① 的口径**：计划里那个「含证据 = 71 个」是**写作时值(761c5a81)**；上面这些数字是**写作时点 `73dae7c5` 的值**。
> 两者**都只是时点值**，**都不是**「最终值」—— 每次提交都会＋1（本报告这一次提交就不在其中）。**一律以当场命令输出为准。**

**E. 冻结包 / 工具 / 主检出 / 迁移（边界核对）**

```
git diff --stat 3a2301ad..HEAD -- internal/tracearchive/   → 空（0 行）
git diff --stat 0bfb130f..HEAD -- internal/tracearchive/   → 空（0 行）
git diff --stat 0bfb130f..HEAD -- atlas-migrations/ migrations/ → 空（0 行；本计划零迁移，H7）
   （注：raw 区间 3a2301ad..HEAD 里的 20260924074930_add_trace_body_ref.sql 由计划 2 的提交 7971b3c1
     引入，落在基线 3a2301ad..0bfb130f 内 ⇒ **不是**本计划新增的迁移）
git diff --stat 0bfb130f..HEAD -- internal/gateway/observability/ internal/gateway/config/trace_body.go → 空（0 行）
   ⇒ 本计划未触碰计划 2 的写侧代码 ⇒ **无需**重跑 p2-task-5 / p2-task-8-spooler
shasum -a 256 tools/trace-mutate/trace_mutate.py = 10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8（未变）
主检出 /Users/jsy/Public/self_code/llm-gateway：HEAD=08ecc9c5 分支=fix/budget-next-period-409 未提交改动=229 行（本轮**只读**，未触碰）
```

**F. E59③ 跨任务连带自查（先自查后报告）**

```
grep -l '^PACKAGES.*tests/tracesealer' tools/trace-mutate/sets/p4-task-*.py | wc -l = 12
grep -l '^PACKAGES.*tests/deploy'      tools/trace-mutate/sets/p4-task-*.py | wc -l = 12
grep -l '^PACKAGES.*tests/traceverify' tools/trace-mutate/sets/p4-task-*.py | wc -l = 3
12 + 12 + 3 = 27 = 全部集合数
```
本轮对 `tests/**`、`internal/**`、`cmd/**`、`tools/trace-mutate/sets/**` 的改动**为 0** ⇒ **没有**内容变更触发的连带重出；
但任务 12 步骤 2 要求「最终 HEAD 全量重跑」⇒ 27 个集合**全部**重出（**不是**「按需最小重跑」）。

## 12. E74（本轮对上轮评审两条的收口）

- **E74①**：计划 E73 前言块的「含证据 = **71** 个」**同行就地标注为「写作时值(761c5a81)」**（行数不变、无行号级联），
  并写明该计数随提交而变、本行不宣称任何「当前值」。**改前**：`含证据 = **71** 个（命令：…）`；
  **改后**：`含证据 = **71** 个（**E74① 标注：写作时值(761c5a81)** —— 该计数随提交而变，**本行不宣称任何「当前值」**，一律以当场命令为准）（命令：…）`。
  评审给出的漂移证据：在 `8716f067` 上实测为 **125**（`git diff --name-only 96a85eb0..8716f067 | wc -l`）。
- **E74②**：`tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst` 的**产出过程不可自动复现** ——
  已**如实登记**在计划文末 E74 节与**本报告 §5.5**（不再暗示可复现）；可自动验证的只有**解析方向**（E73① 的在册用例）。
  本轮**未改**任何实现/测试/集合文件（避免变成实现变更、触发额外重绑）。

## 13. 存疑与已知风险（不隐藏）

1. **R1 未关闭**：隔离旧 timer 期间 `/datalog` **没有任何 retention**；停止点 1.5 的量化门必须在 `--confirm` 前过（§8）。
2. **R2 的真实读数未取**：镜像容量/设备的**执行后** `df -h` 随 A3 一起挂在宿主上（§8）。
3. **`RequiresMountsFor=` 在「手工 umount」时是否可靠地停掉 unit 属纵深防御、未验证**；主防线是进程内护栏。
4. **容器 uid 与 `/datalog` 属主未实测**（首轮用 root，D74）。
5. **换盘/改挂载点后网关会永久闩锁到重启为止**（已写进运行手册，未修）。
6. **fixture 的产出过程不可自动复现**；该 fixture 的 **trailer_len == row_count == 3** ⇒ **它在单测口径下**
   区分不了这两个字段（**E76③ 收窄**：这只影响该 fixture 的分辨力，**不影响**该判据的承重 —— 包级已由
   `TestFooterRowCountOnHandBuiltFooter` 承重，且 E76② 已把该判据正式在册；见 §5.5）。
7. **本报告没有给出任何生产读数** —— 所有 `df -h` / `findmnt` / `zstd --version` / `is-active` / 压缩比 / 内存峰值
   **一律待宿主执行**（§2 / §6 / §9）。
