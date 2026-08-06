# R11 权威表汇总报告（分支收尾：证据重跑）

> **性质**：R11 阶段③ 产出 1。本文是**冻结 HEAD 上 9 张权威表的汇总**，不是新一轮运行。
> **数据来源（逐条抄录，禁止凭印象）**：
> - 9 份片段 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-N-evidence/r11-section.md`
>   （`task-4-evidence/r11-section.md` 内含**两个**片段：`## task-4` 与 `## task-4-errors`）
> - 9 份原始表 `task-N-evidence/r11-*-table.txt`（含证据绑定头部与计数口径）
> - 台账 `progress.md` 的阶段② 各条（用于定位发现与裁决，不用于产生计数）
> **校对基线**：本报告的最终账与台账阶段② 关闭时的最终账（`progress.md:3350-3361`）逐格一致。

---

## 0. 冻结基线与公共事实

| 项 | 值 | 出处 |
|---|---|---|
| 仓库 | `/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write` | 各表证据绑定头部 |
| 分支 | `feat/trace-body-direct-write` | `task-8-evidence/r11-section.md` 前言 |
| **冻结 HEAD（跑前 / 跑后）** | `c13b64bcc2a344193f854063adbf5a94c0e86760` / **同值** | 9/9 表的 `git rev-parse HEAD` 行 |
| 工作树 | 跑前 `git status --porcelain` 空 / 跑后仍空 | 9/9 表的 `git status --porcelain` 行 |
| 工具 sha256 | `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`（`tools/trace-mutate/trace_mutate.py`） | **9/9 表逐位相同** |
| 调用参数 | 未传 `--only` / `--skip`（即每次都是全集运行） | 各片段「命令」条 |
| 退出码 / stderr | 9/9 退出码 0 = **强推断，非机器记录**（原始归档**不含**工具退出码；task-6 归档末行的 `[status: completed, exit code: 0]` 是作业框架追加的页脚，不是工具 stdout。推断依据：9/9 归档完整 + 9/9 无 `!!!` + stderr 0 字节）；stderr **0 字节**（`e3b0c442…`，即空文件 sha256） | 各 `r11-section.md` **自述**；`task-7-evidence/r11-task-7-table.stderr.txt` |
| 证据归档 | 全部写入 gitignored 的 `task-N-evidence/`（`.superpowers/sdd/.gitignore` 内容为 `*`）⇒ 写证据不弄脏树 | `progress.md:2977-2982` |

### 0.1 计数口径（工具定义，9 张表共用）

| 桶 | 含义 |
|---|---|
| 断言支撑红 | 清单全部执行 + 全部终判 + `SKIP=0`，且有用例 FAIL |
| 崩溃 · 超时 · 编译红 | 「没有『用例执行 + 终判』证据，但绝不可能是绿」的响亮红；**编译失败若要单列是更弱的一档** |
| 允许的绿 | 集合在 `EXPECTED_GREEN` 里登记了零覆盖/等价理由 |
| 未登记的绿 | **必须为 0**，否则整轮失败 |
| 证据无效 | `[no tests to run]` 等拿不到判决的运行；**必须为 0** |

### 0.2 证据目录内文件 sha256（本报告复算，`shasum -a 256`）

| 文件 | sha256 |
|---|---|
| `task-2-evidence/r11-section.md` | `0ca638e3adde8f0f6ffdcf666b19e7e03b15f051dc164d3027872302baf81342` |
| `task-3-evidence/r11-section.md` | `38c0f1c5efe407a3ca5863f204f66b1390d9b22e286c764a7db1e8b9e8b6cc27` |
| `task-4-evidence/r11-section.md` | `7083fcfa6c4e8fa8c3b40ccd786844a8c8f2b0a92c2f24bd437ebbdb95d6d05b` |
| `task-5-evidence/r11-section.md` | `9d5ed7622d497d9be6fab665cdc67bad9171e77e667121ec626097c2b2b2e6c6` |
| `task-6-evidence/r11-section.md` | `b76489c15566094f5822c5f1484b9b5233020796585901ccfd9f6ae26175e454` |
| `task-7-evidence/r11-section.md` | `31d708cce9416dff20a72a05031ccb688f626076d2f8d47f3f80510e292d5b4d` |
| `task-8-evidence/r11-section.md` | `8a1f7e6147a0eaa2af82f9be96427515a2a07234d71e5ddfd9a7b57da310ffb1` |
| `task-7-evidence/r11-task-7-goflags-counterproof.txt` | `162d72e86edf324668d9ec9f0620ee234958906362c3dbd0f93c38c2a988153d` |
| `task-7-evidence/r11-task-7-goflags-counterproof-record.md` | `967035bdee7319101ec38b0c0dbf049d4a187322fc5696630e5b176783dfc4ee` |
| `task-8-evidence/r11-timing-probe.txt` | `3316c553ae369be2fca7436626c609b9bd00ca579333ee57045e80754ca6ef05` |

### 0.3 关于锚定：9 张表**跑在** `c13b64bc`，本报告**归档在**其后的文档树上

- 冻结 HEAD `c13b64bcc2a344193f854063adbf5a94c0e86760` 是当前 HEAD 的**祖先**：9 张权威表**跑在** `c13b64bc`，而 `r11-report.md` 与 Task 1–6 报告**归档在**其后的提交树上。
- **为什么判据仍然可比**：`git diff --name-status c13b64bc..HEAD` 的结果**只有 8 个文档**，且**没有任何** `internal/`、`tests/`、`tools/trace-mutate/sets/` 或 `tools/trace-mutate/trace_mutate.py` 的改动：
  - `A` `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/r11-report.md`
  - `A` 同目录 `task-1-report.md` ~ `task-6-report.md`（6 个）
  - `M` `docs/superpowers/plans/2026-09-24-tracearchive-storage-primitives.md`
  ⇒ 表的判据（被测代码、测试文件、变异集合、工具）与归档树**逐字节可比**；后续提交（含本更正提交）只再修改这些**已列出的文档**，不引入代码/测试/集合改动。
- 出处：本报告实测 `git diff --name-status c13b64bc..HEAD`（终审 B 提出并要求显式写明）。

---

## 1. 逐集合权威表

### 1.0 汇总（计数与归档）

| 集合 | 清单条数 | 断言红 | 崩溃·超时 | 允许绿 | 未登记绿 | 证据无效 | 合计 | 编译红 | 子集横幅 | `!!!` | 归档文件 | 归档 sha256 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| task-2 | 7 | 5 | 0 | 0 | 0 | 0 | 5 | 0 | 无 | 无 | `r11-task-2-table.txt` | `afe7b2a364e52b7e9f418efdc564d68f8187024698c9a90367159ad47a2c9441` |
| task-3 | 8 | 7 | 0 | 0 | 0 | 0 | 7 | 0 | 无 | 无 | `r11-task-3-table.txt` | `6467afcd371c1db4fb7208ac1376f0afe7ccf1f573ac15b544f73b46339b6e05` |
| task-4 | 8 | 6 | 0 | 0 | 0 | 0 | 6 | 0 | 无 | 无 | `r11-task-4-table.txt` | `8912fc8d846d473f8ed46b0237b45463977c0d1e6cae7a976e514ca1190d32dd` |
| task-4-errors | 8 | 1 | 0 | 0 | 0 | 0 | 1 | 0 | 无 | 无 | `r11-task-4-errors-table.txt` | `88116596b9274bc8e5ae05ee18b4800d38209c283c89d44736fb8c5f7f0bc88c` |
| task-5 | 26 | 22 | 2 | 1 | 0 | 0 | 25 | 0 | 无 | 无 | `r11-task-5-table.txt` | `5f30e35e737cfcfefc68936284891f83b55e3d7892373fa2ff8d04b0c5f9f429` |
| task-6 | 34 | 46 | 3 | 2 | 0 | 0 | 51 | 0 | 无 | 无 | `r11-task-6-table.txt` | `0c436137dfb40633714952eb7f5d2b68ae2c0ff27435731fa05cbd933b30248a` |
| task-7 | 22 | 28 | 4 | 0 | 0 | 0 | 32 | 0 | 无 | 无 | `r11-task-7-table.txt` | `f0daaf7244872ff5203b4e6ad68d389c5634059fb171f6eea0deba3eed27f688` |
| task-8 | 7 | 3 | 0 | 0 | 0 | 0 | 3 | 0 | 无 | 无 | `r11-task-8-table.txt` | `61639863576c6fcdc78fb004fd2015cf82aaa7c981f27d02ccef6d4960e564f5` |
| task-8-reader | 7 | 6 | 0 | 0 | 0 | 0 | 6 | 0 | 无 | 无 | `r11-task-8-reader-table.txt` | `b1f2ad30945e0c912d8d954139cdf25d294af547cbefbfd80e71429801d35691` |
| **合计** | — | **124** | **9** | **3** | **0** | **0** | **136** | **0** | 9/9 无 | 9/9 无 | — | — |

> 上表 sha256 的**抄录 / 复算来源必须分开标注**（终审 B 复核结论）：
> - **8/9 抄自片段**：`task-2` / `task-3` / `task-4` / `task-4-errors` / `task-5` / `task-6` / `task-8` / `task-8-reader` 的 `r11-section.md`「归档: … sha256 = …」行。
> - **1/9 由本报告复算**：**`task-7` 的片段没有给出该 sha256**（其「归档:」行只有文件名），`f0daaf72…` 是本报告执行 `shasum -a 256 .superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-evidence/r11-task-7-table.txt` 复算所得。
> - 注意：归档文件自己的 sha256 **不可能**出现在它自身的「## 证据绑定」头部（自指），头部记录的是 HEAD、工具/集合/清单/被测文件的哈希。
> - 本报告已用 `shasum -a 256` 对 9 个归档文件**全部复算**，结果与上表逐位一致。
> `!!!` 计数亦由本报告对 9 个归档全文 `grep -c '!!!'` 复算，均为 0；「这是子集」横幅 grep 零命中。

### 1.1 task-2（`index.go`，5 条）

- **命令（逐字）**：
  `python3 tools/trace-mutate/trace_mutate.py --repo . \`
  `  --manifest .superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-2-evidence/names.txt \`
  `  --mutation-set tools/trace-mutate/sets/task-2.py`
- **HEAD（跑前 / 跑后）**：`c13b64bc…` / `c13b64bc…`（一致；跑前跑后工作树空）
- **sha256**：工具 `10dea750…` / 集合 `57c6abdcee6bc15dad4c3ebbd4ced7451e9bcb35a41106775d3b85b81edb3f4b` / 清单 `36e6ecb4b7fdadb9f7d2d15f607cfe30cbc5cabab1dfe89c9669a2cedca1686e`
- **清单条数 / 推导比对**：**7** / 一致（推导命令 `grep -h '^func Test' internal/tracearchive/index_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort`，pristine 树内实跑推导出 7 条）
- **计数五元组**：**5 / 0 / 0 / 0 / 0 = 5**（自标识「集合里的变异总数 = 5（本次运行 == 全集 ✓）」）
- **编译红**：0（5 条全部「红」，自证串均 `names=7 ran=7 diff=0`）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；计划要求总数 5、引用到的变异 5、**未被引用 0**
- **归档**：`task-2-evidence/r11-task-2-table.txt`，sha256 `afe7b2a3…`；stderr 0 字节
- **备注**：`PACKAGES = ["./internal/tracearchive/"]`（**收窄**，未含 `./tests/tracearchive/`）；`CHECKS = []`。

### 1.2 task-3（`footer.go`，7 条）

- **命令（逐字）**：同 task-2 形态，`--manifest …/task-3-evidence/names.txt`、`--mutation-set tools/trace-mutate/sets/task-3.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `a1c422bee9fddcb04eaba74a732a61f8a05c08862d34ca57eb57d97f9070dbc3` / 清单 `f11691b9ab9a4695e5acf42f60724b2aa7f2d852a55bd77e18f3d216319db16b`
- **清单条数 / 推导比对**：**8** / 一致（`grep -h '^func Test' internal/tracearchive/footer_test.go | … | sort`）
- **计数五元组**：**7 / 0 / 0 / 0 / 0 = 7**
- **编译红**：0（m01_A 为 PASS=6/FAIL=2，其余 6 条 PASS=7/FAIL=1）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 3、引用到的变异 7、**未被引用 0**
- **归档**：`task-3-evidence/r11-task-3-table.txt`，sha256 `6467afcd…`；stderr 0 字节
- **备注**：本集合有 **2 条真实静态检查** `[PASS]`（`footer-golden-bytes锚点存在`、`reader-row_count原始偏移锚点存在`）；本轮共 3 个集合有真实静态检查（另两个：task-6 的 1 条、task-8 的 3 条，见 §4.1）；`m02_B` 已拆为 `m02_B1..B5`（5/5 断言红）。

### 1.3 task-4（`ref.go`，6 条）

- **命令（逐字）**：同形态，`--manifest …/task-4-evidence/names.txt`、`--mutation-set tools/trace-mutate/sets/task-4.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `c0dca9a561457e5e66dc85889ac1966e522bfc39b7eff1bc3ceb8415b6bb7f81` / 清单 `fb44e5845dff6b8b4e1614f438d78d134ed5d89d15efebdf97524715a574feee`
- **清单条数 / 推导比对**：**8** / 一致（推导命令 = `{ grep -h '^func Test' internal/tracearchive/ref_internal_test.go tests/tracearchive/ref_test.go; grep -h '^func TestParsePartSeqUsedBySealer(' internal/tracearchive/sealer_internal_test.go; } | … | sort`）
- **计数五元组**：**6 / 0 / 0 / 0 / 0 = 6**
- **编译红**：0（6 条自证均 `names=8 ran=8 diff=0 PASS=7 SKIP=0 FAIL=1`）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 1、引用到的变异 6、**未被引用 0**
- **归档**：`task-4-evidence/r11-task-4-table.txt`，sha256 `8912fc8d…`；stderr 0 字节
- **备注**：`PACKAGES` **含两包**（`./internal/tracearchive/` + `./tests/tracearchive/`）⇒ 红覆盖大包用例；映射表如实登记 3 条已退役编号（M5/M6/M7，**不是缺口**）；`CHECKS = []`。

### 1.4 task-4-errors（`errors.go`，1 条）

- **命令（逐字）**：同形态，`--manifest …/task-4-evidence/names.txt`（**与 task-4 共用同一份清单**）、`--mutation-set tools/trace-mutate/sets/task-4-errors.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `7c720eb14b51b7732b661660c7e8072c240c8abf38e2e1003eaa2f61011a8b9d` / 清单 `fb44e584…`（同 task-4）
- **清单条数 / 推导比对**：**8** / 一致（推导命令同 task-4）
- **计数五元组**：**1 / 0 / 0 / 0 / 0 = 1**
- **编译红**：0（唯一变异自证 `names=8 ran=8 diff=0 PASS=7 SKIP=0 FAIL=1`）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 1、引用到的变异 1、**未被引用 0**（映射来源如实标注为 `task-4-report.md:380-400 F3`，不是 `brief:96` 的泛判据）
- **归档**：`task-4-evidence/r11-task-4-errors-table.txt`，sha256 `88116596…`；stderr 0 字节
- **备注**：虽然只变异 1 条，`-run` 仍执行全部 8 条清单用例（原始侧 `ran=8 PASS=8`）；`CHECKS = []`。

### 1.5 task-5（`writer.go`，25 条）

- **命令（逐字）**：同形态，`--manifest …/task-5-evidence/names.txt`、`--mutation-set tools/trace-mutate/sets/task-5.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `90c8333a6b1259354f6ade5920d1e038901106d324ced78b91414b8ace83efc6` / 清单 `9116e8c3d3c347bfcdeec17e7b7a075f343cd26469c6166163f9d531fce2c05a`
- **清单条数 / 推导比对**：**26** / 一致（`grep -h '^func Test' internal/tracearchive/writer_internal_test.go tests/tracearchive/writer_test.go | … | sort`）
- **计数五元组**：**22 / 2 / 1 / 0 / 0 = 25**
- **编译红**：**0**（无 `undefined:` / `build failed`；上一轮作者自检抓到的 `undefined: notes` 已消除，m22 现为断言红 PASS=24/FAIL=2）
- **崩溃 / 超时红**：**2 条** —— `m10_删除closeCurLocked的nil守卫`（红(崩溃/中止)，未执行 24 条）、`m33_删除PendingSeals的当前块nil守卫`（红(崩溃/中止)，未执行 12 条）
- **允许的绿**：**1 条** —— `m13_删除PendingSeals的空块守卫`（`绿（零覆盖！）`，PASS=26 FAIL=0；经公开 API 不可达的纵深防御，有意保留）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 1、引用到的变异 1、**未被引用 24**
- **归档**：`task-5-evidence/r11-task-5-table.txt`，sha256 `5f30e35e…`
- **备注**：`CHECKS = []`；合并登记结构 `COVERAGE_GAPS`（`task-5.py:390-431`）工具读不到，不影响判定（变异仍 25 条）。

### 1.6 task-6（`sealer.go`，51 条）

- **命令（逐字）**：同形态，`--manifest …/task-6-evidence/names.txt`、`--mutation-set tools/trace-mutate/sets/task-6.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `81070df4068a96b7dfa11fccbaaea0a45cd704f7e8cfd5b4a6e49a3d4a35809d` / 清单 `c27020f20e00916b50a276e808e9382583209bf84fc2dc6b2a1bd6c6ae53b893`
- **清单条数 / 推导比对**：**34** / 一致（`grep -h '^func Test' tests/tracearchive/sealer_test.go internal/tracearchive/sealer_internal_test.go | … | sort`；原始侧自证 `names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0`）
- **计数五元组**：**46 / 3 / 2 / 0 / 0 = 51**
- **编译红**：**0**（3 条非断言红均为运行期非终判：m16 超时/挂死、m17 崩溃/中止未执行 10 条、m21 崩溃/中止未执行 31 条）
- **允许的绿**：**2 条** —— `m15_删除打开spool失败守卫`（零覆盖，`TestSealFailsWhenSpoolMissing` 更早失败）、`m34_删除sort顺序`（等价变异，`os.ReadDir` 契约保证已排序）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 8、引用到的变异 10、**未被引用 41**；静态检查 1 项真实 `[PASS]`（`sealer-不得使用-Go-库编码器`）
- **归档**：`task-6-evidence/r11-task-6-table.txt`，sha256 `0c436137…`（175 行 / 20677 字节）
- **备注（阅读陷阱）**：该归档是 `job_output` 回传的 stdout 逐字字节，**最后一行 `[status: completed, exit code: 0]` 是作业框架页脚，不是工具 stdout**；工具 stdout 结束于 `合计: 51 = 46 断言支撑 + 3 崩溃/超时/编译 + 2 允许的绿`。

### 1.7 task-7（`reader.go`，32 条）

- **命令（逐字）**：`python3 tools/trace-mutate/trace_mutate.py --repo . --manifest .superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-evidence/names.txt --mutation-set tools/trace-mutate/sets/task-7.py`（未传 `--only`/`--skip`）
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `4068e6342fb483e2c8627352bb9f500c8fa299b0891dd8392e50358d52d393b7` / 清单 `8df403eacbb71480b5c462967607d683f26e1ae0b4db0641d57b71cbf11637d5`
- **清单条数 / 推导比对**：**22** / 一致（`grep -h '^func Test' tests/tracearchive/reader_test.go internal/tracearchive/reader_internal_test.go | … | sort`）
- **计数五元组**：**28 / 4 / 0 / 0 / 0 = 32**（自证行 `合计: 32 = 28 断言支撑 + 4 崩溃/超时/编译 + 0 允许的绿`）
- **编译红**：**0**（4 条非断言红 = `红(崩溃/中止)` ×2（m10、m11）+ `红(超时/挂死)` ×2（m27、m28））
- **子集横幅**：无 ｜ **`!!!`**：无（stdout 与 stderr 均零命中；stderr 0 字节）
- **映射**：无缺口；要求总数 10、引用到的变异 20、**未被引用 12**
- **归档**：`task-7-evidence/r11-task-7-table.txt`，sha256 `f0daaf72…`；stderr `e3b0c442…`（0 字节）
- **附加反证（不替代本表）**：`r11-task-7-goflags-counterproof.txt`（116 行，`162d72e8…`）与 `…-record.md`（`967035bd…`）——在父进程注入 `GOFLAGS=-exec=<伪造 wrapper>` 后重跑全集，剔除行列出了 `GOEXPERIMENT, GOFLAGS`（干净跑为 `（无）`）、wrapper 调用 **0 次**、32 行判定表 **ALL-32-ROWS-BYTE-IDENTICAL**、整份归档仅 2 行不同且均与判定无关。

### 1.8 task-8（`writer.go`，3 条）

- **命令（逐字）**：`python3 tools/trace-mutate/trace_mutate.py --repo . --manifest …/task-8-evidence/names.txt --mutation-set tools/trace-mutate/sets/task-8.py`（实际以 `\` 续行分三行执行；无 `--only`/`--skip`）
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `5c9511bd5424b7cf4d83822a0d88eb14199e355da26709344482123ccc49ec09` / 清单 `eb0cfda7dbf9ac9916f6499658aac06c81a60a56102d9379954d5476ebed1eb5`
- **清单条数 / 推导比对**：**7** / 一致（`grep -h '^func Test' tests/tracearchive/roundtrip_test.go tests/tracearchive/constraints_test.go | … | sort`；runner 独立复算 `diff` 为空）
- **计数五元组**：**3 / 0 / 0 / 0 / 0 = 3**
- **编译红**：0（m01/m02/m03 均断言红，分别杀死 3/2/3 条用例）
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 4、引用到的变异 3、**未被引用 0**
- **归档**：`task-8-evidence/r11-task-8-table.txt`，sha256 `61639863…`
- **备注**：本集合有 **3 条真实静态检查** 全 `[PASS]`（`B层-roundtrip`、`B层-constraints`、`真封块路径走本机zstd`）；与 task-8-reader **在同一个后台作业内串行**执行（非并发）。

### 1.9 task-8-reader（`reader.go`，6 条）

- **命令（逐字）**：同 task-8 形态，`--mutation-set tools/trace-mutate/sets/task-8-reader.py`
- **HEAD**：`c13b64bc…` / `c13b64bc…`
- **sha256**：工具 `10dea750…` / 集合 `c70d7a2ffca68b41717fbde8b19659bf013232dc90de3b66394f973e869ec921` / 清单 `eb0cfda7…`（与 task-8 共用）
- **清单条数 / 推导比对**：**7** / 一致（推导命令同 task-8）
- **计数五元组**：**6 / 0 / 0 / 0 / 0 = 6**（m01–m06 均断言红）
- **编译红**：0
- **子集横幅**：无 ｜ **`!!!`**：无
- **映射**：无缺口；要求总数 4、引用到的变异 6、**未被引用 0**
- **归档**：`task-8-evidence/r11-task-8-reader-table.txt`，sha256 `b1f2ad30…`
- **备注**：`CHECKS = []`（`tools/trace-mutate/sets/task-8-reader.py:123`）⇒ 其「静态检查: 全部 PASS」为空真；映射表「静态检查」列全为 `—` ⇒ **未声称**任何要求由静态检查覆盖，故**不构成缺口**。

---

## 2. 总计

| 集合 | 断言支撑红 | 崩溃·超时红 | 允许的绿 | 未登记的绿 | 证据无效 | 合计 |
|---|---|---|---|---|---|---|
| task-2 | 5 | 0 | 0 | 0 | 0 | 5 |
| task-3 | 7 | 0 | 0 | 0 | 0 | 7 |
| task-4 | 6 | 0 | 0 | 0 | 0 | 6 |
| task-4-errors | 1 | 0 | 0 | 0 | 0 | 1 |
| task-5 | 22 | 2 | 1 | 0 | 0 | 25 |
| task-6 | 46 | 3 | 2 | 0 | 0 | 51 |
| task-7 | 28 | 4 | 0 | 0 | 0 | 32 |
| task-8 | 3 | 0 | 0 | 0 | 0 | 3 |
| task-8-reader | 6 | 0 | 0 | 0 | 0 | 6 |
| **合计** | **124** | **9** | **3** | **0** | **0** | **136** |

### 2.1 算术闭合（显式）

```
断言支撑红     5 + 7 + 6 + 1 + 22 + 46 + 28 + 3 + 6 = 124
崩溃·超时红    0 + 0 + 0 + 0 +  2 +  3 +  4 + 0 + 0 =   9
允许的绿       0 + 0 + 0 + 0 +  1 +  2 +  0 + 0 + 0 =   3
未登记的绿     0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
证据无效       0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
----------------------------------------------------------
124 + 9 + 3 = 136
```

- 136 = 9 个集合各自「合计」之和：`5+7+6+1+25+51+32+3+6 = 136` ✓
- 136 = 9 个集合 `MUTATIONS` 长度之和（与冻结基线一致）；9/9 表均带 `本次运行 == 全集 ✓` 自标识，**无子集运行** ✓
- **编译红 = 0、未登记的绿 = 0、证据无效 = 0**（三者任一非零都会使整轮失败；`trace_mutate.py:697-698` 对后两者直接 `SystemExit`）✓
- 3 条允许的绿 = task-5 `m13`、task-6 `m15`、task-6 `m34`（`progress.md:3364-3365`）✓
- 9 条崩溃/超时 = task-5 `m10,m33` + task-6 `m16,m17,m21` + task-7 `m10,m11,m27,m28`（`progress.md:3366`）✓

---

## 3. 并发漂移核对

**结论：9/9 集合均报「无漂移」。**

| 集合 | 漂移核对结论与依据 | 出处 |
|---|---|---|
| task-2 / task-3 / task-4 / task-4-errors | 0 崩溃 / 0 超时（**无可漂移对象**）；runner 特意把四个集合**顺序跑**以「避免自造 CPU 竞争触发 `-timeout 30s` 假超时污染权威表」 | `progress.md:3212-3216`、`3374` |
| task-5 | 2 条崩溃/中止的杀法与简报 `task-5-brief.md:253-254` 的预测一致（`m33` 正是预测的 nil 解引用 panic）；逐行比对非权威预测文件：**25 条判定 / 自证串 / 杀死用例 0 行差异** | `task-5-evidence/r11-section.md` 异常 1、2 |
| task-6 | 崩溃/超时集合 `{m16, m17, m21}` 与旧 51 行归档 `round3-mutation-table.txt` **逐条同名**；**51/51 全部键的判定差异 0 处、杀死用例差异 0 处**；旧归档跑在**另一 HEAD `7bf7d112`** 与**另一集合版本 `6d20f5d6…`** 上 ⇒ 结论更强 | `task-6-evidence/r11-section.md` 异常 1 |
| task-7 | 崩溃/超时集合 `{m10, m11, m27, m28}` 与机器空闲时的旧归档 `mutation-table.txt`（`5664c858…`）**逐条相同且桶归属相同**；28 条断言红逐一对应 ⇒ **无一条被 CPU 竞争压成超时红** | `task-7-evidence/r11-section.md`「并发漂移核对」 |
| task-8 / task-8-reader | 9/9 与旧提交 `0243b1df` 判定一致；两轮的工具/集合/清单 sha256 **逐位相同**且 `git diff --quiet 0243b1df HEAD -- internal/tracearchive/{writer,reader}.go tests/tracearchive/` **无差异** ⇒ 同一份输入的重跑 | `task-8-evidence/r11-section.md`「并发漂移核对」1–2 |

### 3.1 task-8 的量化余量（本轮最硬的时序证据）

独立时序探针在 `c13b64bc` 的 pristine 临时树上跑同一 7 条清单（`task-8-evidence/r11-timing-probe.txt`，sha256 `3316c553…`）：

- `go test … -v -count=1 -timeout 30s` **exit 0**，7 PASS / 0 SKIP / 0 FAIL，**无 `test timed out`**
- **整包耗时 9.632s，对固定 30s 上限约 3.1 倍余量**
- 单条最慢 `TestReadsDoNotGrowDiskUsage` **5.01s**；端到端往返 `TestEndToEndRoundTripByteExact` **4.16s**
- 口径提示：Go 的 `-timeout` 约束的是**整个测试二进制**，故承重数字是整包 **9.632s**，而非单条用例 ⇒ 即使并发负载下距上限仍有约 20.4s 空间

### 3.2 两处**非漂移**的差异（如实登记，避免误读）

1. **task-7 `m27` 的辅助列**：新表写 `超时(用例未终判)`，旧归档写 `TestReaderReleasesConcurrencySlots`；**判定桶两者同为 `红(超时/挂死)`** ⇒ 不构成漂移，且**新表描述才准确**（超时意味着该用例**没有**做出终判，旧归档点名「杀死它的用例」是错的）。出处：`task-7-evidence/r11-section.md`、`progress.md:3383-3385`。
2. **task-7 归档的第 10/28 行**（counterproof 轮；终审 B 已独立 diff 复核：**仅第 10、28 行不同，32 行判定表逐字节相同**）：分别是 pristine 临时树随机路径与被剔除的敏感变量，**均与判定无关**。出处：`task-7-evidence/r11-section.md`（counterproof 小节）。

---

## 4. 已知限制与阅读陷阱（逐条独立，各带出处）

### 4.1 🔴 `静态检查: 全部 PASS` 在 `CHECKS = []` 时是**空真**，不得读作正面证据

- **事实**：`CHECKS` 为空列表时，工具的静态检查段是**空循环、默认 `True`** ⇒ 末行仍打印 `静态检查: 全部 PASS`。
- **本轮空真集合（6 个）**：task-2、task-4、task-4-errors、task-5、task-7、task-8-reader。
  源文件行：`sets/task-2.py:142`、`sets/task-4.py:176`、`sets/task-4-errors.py:76`、`sets/task-5.py:444`、`sets/task-7.py:331`、`sets/task-8-reader.py:123` 均为 `CHECKS = []`。
- **系统性**：**三个 runner 独立报出**同一现象（`25dd2809`/task-5、`5f7e23fe`/task-2+4+4-errors、`bfbbca41`/task-8-reader）。
- **有真实静态检查的只有**：task-3（2 条 `[PASS]`，`sets/task-3.py:185` 起）、task-6（1 条 `[PASS]`，`sets/task-6.py:407` 起）、task-8（3 条 `[PASS]`，`sets/task-8.py:80` 起）。
- **正确读法**：空真集合的映射表「静态检查」列均为 `—` ⇒ 它们**没有声称**任何要求由静态检查覆盖 ⇒ **非缺口**，但**也绝不是证据**。
- 出处：`task-2`/`task-4`/`task-4-errors`/`task-5` 各 `r11-section.md` 的「异常与如实说明」；`task-8-evidence/r11-section.md` 异常 2；`progress.md:3146-3150`、`3320-3331`。

### 4.2 🔴 `names.txt` **未由提交内容锚定**（但**事后替换清单无法产出一张无 `!!!` 的表**）

- **事实**：清单位于 gitignored 的 `task-N-evidence/` ⇒ `git show HEAD:…names.txt` **取不到**（本报告实测：exit 128，`致命错误：路径 … 在磁盘上，但是不在 'HEAD' 中`）。
- **控制者已读源码核实的一半（不成立的那一半）**：工具的 `MANIFEST_DERIVATION` **不是**纯声明 —— 它在 **pristine 树里实际执行**推导命令（`trace_mutate.py:562-565`，`cwd=tree`），且当**推导集合 ≠ 清单集合**时直接 `SystemExit`（`:571-572`，原文即「**清单可能被改小**」）。推导命令住在**被跟踪的集合文件**里（如 task-2 的 `grep -h '^func Test' internal/tracearchive/index_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort`），比对的是**集合**而非计数 ⇒ **事后替换 `names.txt` 无法产出一张无 `!!!` 的表**。
- **仍然成立的一半（必须写明）**：**脱离「提交的树 + 工具 + 被跟踪的集合」无法独立复核归档** —— 归档**不自足**。这是**有意设计**：审计链 = **计划 + 集合 + 工具 + 报告**，原始归档可由前三者重新生成。读者不应把 `task-N-evidence/` 当作可独立取信的成品。
- **本轮状态**：9/9 集合的推导比对均「一致」，且推导命令内嵌于被跟踪的集合 ⇒ 清单完整性由「本轮 sha256 + 集合内的推导命令在 pristine 树上的实跑比对」共同锚定；**不由提交内容锚定**。
- 出处：`task-8-evidence/r11-section.md` 异常 3；`progress.md:3333-3337`；工具机制 `trace_mutate.py:562-572`（终审 B 复核）。

### 4.3 🔴 「未被引用的变异」**不是缺陷**

- **事实**：`REQUIREMENTS`（映射表）的用途是抓「**要求没有条目**」的**缺口**（gap），**不是**要求每条变异都能追到一行简报。
- **本轮数字**：task-6 未被引用 **41**、task-5 **24**、task-7 **12**（合计 77）——均属正常；它们同样在 **git-tracked** 的集合里，删掉会被 `git diff` 看见。
- **对比**：本轮真正由映射表抓出的缺口是 task-6 `brief:962`（忽略 `cmd.Run()` 的 error）**没有任何对应条目**，已由新增 `m50` 关闭。
- 出处：`task-6-evidence/r11-task-6-table.txt:49`、`task-5-evidence/r11-task-5-table.txt:39`、`task-7-evidence/r11-task-7-table.txt:48`；`progress.md:3281-3286`。

### 4.4 ⚠️ 工具正常模式**不归档每条变异的原始 `go test` 输出**（需 `--side`）

- **后果**：各 runner 只能依赖自证串（`names/ran/diff/PASS/SKIP/FAIL`）与判定列、杀死的用例列，**无法独立复核每条变异的逐行测试输出**。
- **本轮影响**：task-5 的 2 条崩溃红**无法进一步区分** `panic:` / `fatal error:` / `signal:` 中的哪一种（但已确认**不是编译失败**，否则会归入 `红(编译失败)`）。
- 出处：`task-2`/`task-4`/`task-4-errors`/`task-5` 各 `r11-section.md` 的核对局限条；`progress.md:3229-3230`。

### 4.5 ⚠️ 工具的 `EXPECTED_GREEN` **实际转红不算失败**（计入红，但仍打印那行理由）

- **机制**：`classify()` 先判「红」，只有落在绿分支时才查 `EXPECTED_GREEN`；而 `[允许的绿]` 理由行是对 `selected` 全量打印的，与判定无关。⇒ **「已登记的允许绿实际转红」不会报错**，必须人工逐键核对。
- **本轮核对**：**3/3 通过** —— task-5 `m13`、task-6 `m15`、task-6 `m34` 均**真实落在「允许的绿」计数**里（task-5 绿=1、task-6 绿=2，与键数 1:1 对齐）。
- 出处：`task-5`/`task-6` 的 `r11-section.md`「EXPECTED_GREEN 一致性」；`trace_mutate.py:653-665`、`:687`；`progress.md:3018-3019`、`3102`。

### 4.6 ⚠️ 工具对每包固定 `-timeout 30s`；超时归入「崩溃/超时红」（弱一档，但**整轮仍会通过**）

- **机制**：`go_test_cmd()` 固定 `-timeout 30s`（`trace_mutate.py:268`）。超时/崩溃是「响亮但弱一档」的红，**不影响退出码**。
- **风险**：并发 CPU 竞争可能把一条本该「断言支撑的红」压成「超时红」，而**没有任何判据能拦住**。
- **本轮的应对**：task-2/3/4/4-errors **顺序跑**；task-8 两集合**同一作业内串行**；task-5/6/7 逐个核对崩溃集合与旧归档是否漂移（见 §3）。
- 出处：`progress.md:3212-3216`；`task-8-evidence/r11-section.md`（前置把关段）。

### 4.7 ⚠️ **如实记录控制者自己的一处错误**：task-5 的预期来自非权威文件

- **控制者预期**（「断言红 24 + 允许绿 1」）来自同目录 `aux-NOT-authoritative-verdict-prediction.txt` —— 该文件绑定 HEAD `20032b9a…`、**工作树脏**、集合 sha256 `c6713d9e…`、**无提交绑定**，明确标注非权威。
- **权威表实为 22 红 + 2 崩溃 + 1 绿 = 25**。口径差异已定位：预测文件把 `红(崩溃/中止)` **并入**了「红」。
- **裁决**：以权威表为准，控制者预期作废；**执行者按事实报 22 而不是向预期对齐，是正确处置**。
- 出处：`task-5-evidence/r11-section.md` 异常 1；`progress.md:3129-3137`。

### 4.8 ⚠️ 逐条耗时不可证（读表时的能力边界）

**能证明**的是「判定与杀死用例集合零漂移」；**不能证明**逐条耗时未受 CPU 竞争影响（工具不输出每条耗时）。task-8 用独立时序探针补上了整包余量（§3.1），其余集合没有等价测量。出处：`task-6-evidence/r11-section.md` 异常 5；`progress.md:3276-3279`。

### 4.9 ⚠️ task-6 归档末行是**作业框架页脚**，不是工具输出

`[status: completed, exit code: 0]` 由 `job_output` 框架追加；工具 stdout 结束于 `合计: 51 = …`。引用该归档时不应把页脚当作工具自证。出处：`task-6-evidence/r11-section.md`「归档」条。

### 4.10 ⚠️ counterproof 是**附加反证**，不替代权威表

`r11-task-7-table.txt`（干净跑）仍是 task-7 的权威表；`r11-task-7-goflags-counterproof.txt` 是在父进程刻意注入被测变量的**附加测量**。出处：`task-7-evidence/r11-section.md` 异常 6；`progress.md:3492`。

### 4.11 🔴 逐行零覆盖守卫**不止一处**：除 `writer.go:196-201` 外还有三处

- **事实**：§5#2 只登记了 `NewWriter` 两个阈值默认值分支（`internal/tracearchive/writer.go:196-201`）零变异覆盖。实测**还有三处 IO 包装分支同样零覆盖**：把它们的 `%w` 换成 `%v`（即退掉错误身份）之后**22 条全绿 ⇒ 没有任何用例承重**。
- **三处位置**（台账实测时的 HEAD `f7033774`，本报告用 `git show` 复算）：`internal/tracearchive/reader.go:90`（`检查产物失败`）、`:97`（`检查 spool 失败`）、`:277`（`打开 spool 失败`）。在**当前 R11 v2 冻结点 `21ffec3f`** 上，同样三处因 F1 在文件前段插入了 `maxTrailerEntries` 的注释与常量而**行号平移**到 `reader.go:120` / `:127` / `:310`。
- **为什么容易被漏掉**：它们是**错误包装**分支而非判定分支 —— 删/改不改变控制流，只有断言「错误身份（`errors.Is`）」的用例才会红，而这类用例只覆盖了其中一部分分支。
- 出处：`progress.md:2282`、`progress.md:2313`（上一版 `task-7-report.md` §4.2 只报了两处；此处按实测更正为三处）。R11 的两份文本（v1 与 v2）此前**都未提**。

### 4.12 🔴 `scanSpoolOffsets` → `runZstd` 之间的窗口会**静默丢正文**（§7.2 只用 `minAge` 兜）

- **流水线顺序是实测的**（`21ffec3f` 的 `internal/tracearchive/sealer.go`）：① `scanSpoolOffsets` 采集每行首字节的未压缩偏移（`:195`）→ ② 行数交叉校验（`:197-201`，`task.Rows > 0` 时与 `len(offsets)` 比对）→ ③ `runZstd` 把**整个 spool** 压成帧（`:225`）→ ④ `appendTrailerAndFooter` 写入 trailer/footer → ⑤ `os.Rename` → ⑥ `os.Remove(spool)`（`:256`）。
- **窗口与后果**：**①与③之间新追加的字节会进帧，却没有对应的 trailer 偏移条目** —— 偏移采集与行数交叉校验都在压缩**之前**，它们看不见窗口内的追加。随后 ⑥ 删掉 spool ⇒ 这些字节**永久留在帧里但不可寻址**：`row_count` 只等于 `len(offsets)`，故后续点读会在 `ref.Row` 越界分支上**响亮报 `ErrNotArchived`**，但**正文已经丢了**，且不留「丢了多少」的痕迹。
- **为什么算静默**：写入侧 `Append` 全程返回成功；`task.Rows` 也是**扫描时点**的值，窗口内的追加不在任何判据的视野里。⇒ 这是「**静默丢正文** + 后续点读响亮越界」的组合。
- **§7.2 的兜底只有 `minAge`**：要求「spool 的 mtime 早于 `now - minAge`（3h）才可封块」，也就是把「封块期间不再追加」**委托给调用方**（计划 2 的滚动逻辑）。本报告 §4.6 只谈 `-timeout 30s` 的超时风险，**未提这个窗口**。
- 出处：本节在 `21ffec3f` 上对 `sealer.go:195/197-201/225/256` 的实测复算；终审 A §6.2（`final-review-A-code-spec.md:108-110`）；`progress.md:3741-3743`。

### 4.13 ⚠️ 多个集合 `EXPECTED_GREEN` **为空** ⇒ 读者无从得知哪些守卫行**完全没有变异证据**

- **事实**：9 个集合里 **7 个**的 `EXPECTED_GREEN` 是**空字典**（task-2、task-3、task-4、task-4-errors、task-7、task-8、task-8-reader）；只有 task-5（1 键 `m13`）与 task-6（2 键 `m15`/`m34`）非空。
- **阅读陷阱**：空字典**不等于**「本集合没有零覆盖守卫」，只等于「本集合**没有登记**零覆盖 / 等价理由」。真正的零覆盖守卫会以两种形态**不在表里**：(a) 落在别的集合的 `COVERAGE_GAPS`（task-5 的 `NewWriter` 两个阈值默认值分支，见 §5#2）；(b) 压根没人登记（见 §4.11 的三处）。⇒ **只看 `EXPECTED_GREEN` 列无法回答「哪个守卫没有变异证据」**，必须结合 §4.11、§5#2 与集合文件里**工具读不到**的登记（D24）。
- 出处：9 份 `r11-section.md` / `r11v2-section.md` 的「EXPECTED_GREEN 一致性」条；终审 A §6.3（`final-review-A-code-spec.md:111-112`）；`progress.md:3744`。

---

## 5. R11 产生的发现汇总

| # | 发现 | 证据出处 | 处置 / 去向 |
|---|---|---|---|
| 1 | **Task 5 旧 30 条变异对 `brief:253-254` 覆盖为零**（旧 M10 删的是 `closeCurLocked` 的 nil 守卫，不是 `PendingSeals` 的）⇒ **旧证据强度被高估** | `task-5-evidence/r11-task-5-table.txt:37`（映射表说明栏）；`progress.md:3033` | **已由新增 `m33` 关闭**（权威表 `m33` = 红(崩溃/中止)，杀死 `TestWriterPendingSealsNilBeforeAnyAppend`，正是简报预测的 panic 杀法） |
| 2 | `NewWriter` 两个阈值默认值分支**零变异覆盖**（`internal/tracearchive/writer.go:196-198` 与 `:199-201`）：**可达**、已被 `TestWriterPendingSealsAppliesDefaultThresholds` 约束 ⇒ **缺的是变异证据而非覆盖** | `tools/trace-mutate/sets/task-5.py:390-431`（`COVERAGE_GAPS`，含 `where`/`reachable`/`how_to_close`/`aux_verified`）；`progress.md:3058-3074` | **本轮按裁决只登记、不扩范围**。`how_to_close` 的两条 `("cut", …)` pattern **已预实测**：各自唯一定位、编译通过、判定红、杀死用例恰为该用例。**这是计划缺陷/工具登记，不是本报告的变异数** |
| 3 | Task 2 的 `m01` 红靠**分配量阈值**（`alloc > 1<<20`，`index_test.go:66`）而非错误身份 | `task-2-evidence/r11-task-2-table.txt` 映射表 m01 说明（「简报里的用例名是旧名…它断言的是分配量而非错误身份，缺守卫时 varint 耗尽仍报同一个 `ErrTrailerCorrupt`」） | **登记，供终审考虑是否加强**（未在本轮改集合） |
| 4 | Task 3 的 `m03_C` **只在清单范围内成立**：清单内仅 `TestFooterGoldenBytes` 红；清单外另有一条 `tests/tracearchive/reader_test.go:597 TestReaderRejectsTrailerRowCountMismatch` 也红 —— **但理由不是它的前置断言**：实测两法（删 `index.go` 的 `pos != len(b)` 守卫；按注册的 `m17` 换 lenient 解码）都让它在 **`:625` 的真断言**上红（`实际 <nil>`），**不在** `:607`/`:608`；`:607` 的前置（`row_count == 3`）只是被夹具（3 行 `"rc-N"` ⇒ `TrailerLen == RowCount == 3`）**恰巧遮蔽**而未失败。⇒ `m03_C` 下它红在 `:625`，死于 `reader.go:140`「trailer 未紧邻 footer」（**另一守卫**） | `task-3-evidence/r11-section.md` 异常 4；**终审 A §4**（`final-review-A-code-spec.md:79-86`）；`progress.md:3730-3734` | **已如实登记**：措辞仍限制为「只在 task-3 清单范围内成立」，不许升级为「全仓唯一探针」；**理由按终审 A 的实测更正**（**告警项本身保留** —— 实质对，理由错）。行号是终审 A 的评审 HEAD `c602e71d`：该 HEAD 上真断言在 `reader_test.go:625`、`reader.go` 的 `if` 守卫在 `:140`（错误串在 `:141`）；在**当前 R11 v2 冻结点 `21ffec3f`** 上同样两处分别位于 `reader_test.go:626` 与 `reader.go:170`（本报告用 `git show` 在 `c602e71d`、`git grep -n` 在 `21ffec3f` 各自复算，F1 的行插入使行号平移） |
| 5 | **简报缺陷**：`task-3-brief.md:267` 写的 `TestFooterGoldenLayout` **在本树不存在**（真实名 `TestFooterGoldenBytes`，`internal/tracearchive/footer_test.go:35`） | `progress.md:2888-2891`；本报告实测 `grep -rn 'func TestFooterGolden'` 只命中 `TestFooterGoldenBytes` | **记入计划缺陷登记表（D18）** |
| 6 | **派发缺陷**：给 Task 2/3 的 `PACKAGES` 含 `./tests/tracearchive/` ⇒ 该包 `[no tests to run]` ⇒ 会判「证据无效」 | `progress.md:2881-2886`；`trace_mutate.py:188-189`/`:226-227` | 两作者各自收窄到 `./internal/tracearchive/` 并把实测输出抄进集合文件头；**记入计划缺陷登记表（D19）** |
| 7 | **映射缺陷**：Task 3 的 `m02_B` 并集变异（五处 `%w` 一次性全改 `%v`）= **虚假的要求满足**（映射表声称有对应条目，而「任意一处」要求实际未被验证，与 D17 同类） | `task-3-evidence/r11-task-3-table.txt:43`；`progress.md:2903-2915` | **已拆为 `m02_B1..m02_B5`**，5/5 断言红、互不代偿（提交 `580fe5d9`）；**记入计划缺陷登记表（D20）** |
| 8 | **Task 2 报告 `:130-133` 的主张失实**：称删 `delta < 0` 该 FAIL；实际被紧随的 `out[i] < 0` 守卫接住，`errors.Is` 断言仍通过 | `task-2-evidence/r11-section.md` 异常 2；`task-2-evidence/r11-task-2-table.txt` 映射表 | 本轮已补**表驱动子用例 `{10,5}`**（提交 `20032b9a`），`m02` 成为普通红、`EXPECTED_GREEN` 清空；**报告侧正式更正见本报告产出 3（D21）** |
| 9 | **Task 4 的 `M7` 退役**：不同于 M5/M6（守卫已不存在），M7 **只有散文、无脚本**，不可逐字重跑 | `task-4-evidence/r11-section.md` 异常 4；`progress.md:3232-3235` | 归档标注「**这是一处判断，不是事实**」；若终审要保留其语义，应作为**新增条目**重新设计并双侧验证 |
| 10 | **task-7 旧归档的 `m27`「杀死用例」列是错的**（超时 ⇒ 用例未终判，不能点名用例） | `task-7-evidence/r11-section.md`「并发漂移核对」；`progress.md:3383-3385` | **新表描述为准**（`超时(用例未终判)`）；记录在案（D23） |
| 11 | **task-6 的 `PENDING`（显式 `sort.Slice` 冗余）⇒ 结案**：冗余成立（`m34` 绿证实 + `os.ReadDir` 排序契约 `$GOROOT/src/os/dir.go:109-126`）；保留排序是**有意的显式化** | `task-6-evidence/r11-task-6-table.txt` 的 `[允许的绿] m34` 理由行；`progress.md:3156-3185` | **并更正**它自己那句「加一个断言顺序的用例会让它不再冗余」——删掉 `sort.Slice` 后顺序仍对 ⇒ 那种用例照样通过 ⇒ **只算可选文档，不是修复**（D24） |
| 12 | **GOFLAGS `-exec` 伪造路径已测量关闭** | `task-7-evidence/r11-task-7-goflags-counterproof.txt`（`162d72e8…`）、`…-record.md`（`967035bd…`）；机制在 `trace_mutate.py:97-105`（`build_env()`）与 `:272`（`run_tests(env=env)`）；`progress.md:3464-3496` | 三问全符合：剔除行列 `GOEXPERIMENT, GOFLAGS`、wrapper 调用 **0 次**、32 行判定表**逐字节相同**、整份归档仅 2 行不同且均与判定无关 ⇒ 结论「**迁移关闭了 `GOFLAGS` 伪造路径**」。边界：只证伪 `GOFLAGS=-exec` 一条向量，`GOEXPERIMENT` 未单独构造行为差异测量 |
| 13 | Task 6 的 `brief:962`（忽略 `cmd.Run()` 的 error）在映射表首次生成时**没有对应条目**（D17 的实测实例） | `task-6-evidence/r11-task-6-table.txt:41` | **已由新增 `m50_忽略cmd.Run错误` 关闭** |
| 14 | **（终审 B 发现）本报告初稿的出处指错**：引用不存在的文件名 `task-{5,6,7}-evidence/r11-table.txt`、把 counterproof 的两行差异指错为 task-6 归档、把 task-7 的归档 sha256（实为 `shasum` 复算）说成「抄自片段」；**数字本身全部正确** | 终审 B（证据链审计轴）逐格复核；更正落点见本报告 §1.0 注、§3.2、§4.3 | **已更正**（路径限定的更正中提交；4 处引用逐一 `ls`/`sed -n` 自检）；登记为计划缺陷 **D26** |
| 15 | **（终审 B 发现）强推断被写成直接证据**：§0 的「9/9 退出码 0」原始归档并不含（仅各片段自述；task-6 归档末行是作业框架页脚） | 终审 B；更正落点见本报告 §0 表「退出码 / stderr」行 | **已标注为「强推断，非机器记录」**并写明依据；登记为计划缺陷 **D27** |

---

## 6. Task 1 豁免登记（按设计**无变异证据**）

| 项 | 内容 | 出处 |
|---|---|---|
| 产物 | `internal/tracearchive/defaults.go`，仅几个常量：`defaultZstdLevel = 19`、`defaultZstdWindowBytes = 512 << 20`、`defaultBlockBytes = 1536 << 20`、`defaultSealInterval = 15 * time.Minute` | `internal/tracearchive/defaults.go:43-66` |
| 唯一候选变异 | 标定断言（`runtime.MemStats.HeapInuse` 差值）—— `internal/tracearchive/calib_test.go:38-45` | 同上；`progress.md:3447-3448` |
| 为何不得充当权威证据 | 该断言**对 GC / 噪声敏感**，且本轮**未实测** | `progress.md:3447-3448` |
| 工具侧的结构性原因 | 工具 `load_set()` 强制要求 `MUTATIONS` 非空，否则 `SystemExit("集合文件里没有任何变异")` ⇒ **「只有 `CHECKS` 没有 `MUTATIONS`」的集合根本无法运行** | `trace_mutate.py:135-137` |
| **裁定** | **按设计豁免**：Task 1 的产物是常量与标定结论，没有可删的运行时守卫；其正确性由后续任务（2–8）的变异证据与常量被消费的方式间接约束。豁免不等于「已证明」，而是**明确不作为权威证据使用** | 本报告；`progress.md:3447-3448` |

---

## 7. R11 v2（修复轮后重跑）

> **性质**：本节是**修复轮 F1 + F2a 之后**在同一分支上的**第二次全量重跑**，与 §0–§6（v1，冻结 `c13b64bc`）**并列，不替代**。
> **数据来源**：9 份 `task-N-evidence/r11v2-section.md` 与 9 份 `task-N-evidence/r11v2-*-table.txt`；本节对 9 份 v2 归档的 sha256 **全部用 `shasum -a 256` 复算**，与各片段自报值逐位一致；`!!!` 与子集横幅由本节对 9 份归档逐份 `grep` 复算。

### 7.1 为什么必须重跑

1. **代码变了 ⇒ 全部 v1 证据失效**。本项目的原则是**证据绑定文件哈希**（见 D16 与 §0 的绑定头部口径）：一张权威表绑定「HEAD + 工具哈希 + 集合哈希 + 清单哈希 + 被测/测试文件哈希」。F1（`cb3253f5`）改动了 `sealer.go`、`writer.go`、`index.go`、`reader.go`、`internal/tracearchive/reader_internal_test.go`、`tests/tracearchive/sealer_test.go` ⇒ **旧表对新树不再成立**。
2. **注释类改动同样重跑**。F1 的两笔提交里**都有纯文字改动**：`cb3253f5`（`fix`）**因同文件一并落地**了 `writer.go` 的两处注释更正（`O_APPEND` 失实、`MarkSealed` 假前提）与 `sealer.go` 的 `ErrNotFound` 注释收敛（该提交自述「未做跨提交的部分暂存」）；`221f8815`（`docs`）**只改文字** —— `errors.go` / `footer.go` / `defaults.go` / `tests/tracearchive/reader_test.go` 的注释 + **规格 §5.2**。⇒ **文本变了，哈希绑定就断了**，与行为是否改变无关。§7.3 的 task-3 / task-4-errors / task-8-reader 正是这条原则的直接样本：**行为证据零漂移，但绑定层确实变了**。
3. ⇒ **9 个集合全部重跑**，而不是只跑被 F1 触及的那几个。

### 7.2 新冻结点 `21ffec3f…`；**v1 的表未被覆盖**

| 项 | v1 | v2 |
|---|---|---|
| 冻结 HEAD（跑前 / 跑后） | `c13b64bcc2a344193f854063adbf5a94c0e86760` | **`21ffec3f42eb22981f67ba2cf4de90d54ee4ba64`** |
| 该冻结点由什么组成 | — | `221f8815`（F1 第二笔）＋ `62de2d22`（补 `sets/task-2.py` 的 `m06`、`sets/task-6.py` 的 `m54`/`m55`）＋ `21ffec3f`（`sets/task-5.py` 的 `m03` 重新定点） |
| 归档文件名 | `task-N-evidence/r11-*-table.txt` | `task-N-evidence/r11v2-*-table.txt`（**新文件名**） |
| 工具 sha256 | `10dea750…` | `10dea750…`（**与 v1 相同**；`trace_mutate.py` 零改动） |

- **v1 的九张表原样保留在 `c13b64bc` 上**：本节**不改写 §1/§2 的任何一行**，也不移动其归档；两套表各说各的冻结点（§0.3 关于「v1 跑在 `c13b64bc`、报告归档在其后」的说明，对 §7 同样成立 —— v2 跑在 `21ffec3f`，本节归档在其后的文档树上）。
- 任务书模板把冻结点写作 `(v2 @ 221f8815)`；但**补条目的两笔集合提交必须先于重跑**（否则新增条目不在锚定范围内），故实际冻结点前移到 `21ffec3f`。9/9 表的 `git rev-parse HEAD` 行自报的也是它。

### 7.3 v2 九表（计数 + 与 v1 差异 + 归档 sha256）

| 集合 | 断言红 | 崩溃/超时 | 允许绿 | 未登记绿 | 无效 | 与 v1（冻结 `c13b64bc`）差异 | 归档 sha256 |
|---|---|---|---|---|---|---|---|
| task-2 | 6 | 0 | 0 | 0 | 0 | 5→6：新增 `m06_删除条目数上界守卫`（红）；`m01`–`m05` 判定/杀手逐条不变；清单 7→8 | `3bdf3c3e…` |
| task-3 | 7 | 0 | 0 | 0 | 0 | 逐条零漂移（`footer.go` 是 F1 的注释改动） | `f1249632…` |
| task-4 | 6 | 0 | 0 | 0 | 0 | 零漂移（`ref.go` sha **未变**，但测试文件哈希被 F1 改动 ⇒ 仍须重跑） | `cbf8a320…` |
| task-4-errors | 1 | 0 | 0 | 0 | 0 | 零漂移（`errors.go` 注释改动） | `95899de9…` |
| task-5 | 22 | 2 | 1 | 0 | 0 | 25 条判定/杀手/自证与 v1 **逐字零漂移**（含重定点的 `m03`） | `74689753…` |
| task-6 | 48 | 3 | 2 | 0 | 0 | 51→53：新增 `m54`/`m55`（均红）；三桶成员逐条相同；`m11`/`m40` 各多一个杀手 | `68de1e19…` |
| task-7 | 28 | 4 | 0 | 0 | 0 | 清单 22→23（**推导驱动**）；桶成员不变；`m03`/`m17` 各多一个杀手 | `ca297f09…` |
| task-8 | 3 | 0 | 0 | 0 | 0 | 零漂移 | `6a641df9…` |
| task-8-reader | 6 | 0 | 0 | 0 | 0 | 零漂移 | `723fcfc2…` |
| **合计** | **127** | **9** | **3** | **0** | **0** | **139 = 136 + 3 条新变异** | — |

> 上表 sha256 为前 8 位；完整值（本节 `shasum -a 256` 复算，与各 `r11v2-section.md` 自报值逐位一致）：
> `3bdf3c3ebfc15141be804c12352443910c52e1544b0329812038d46d1b70b90d`（task-2）、
> `f12496329d1789af97b0185bdae8cf0fac2c11c624a691bc0c07baaae325bfbf`（task-3）、
> `cbf8a3201e620c9e6c0afca3d1eb03f1812b59c7b4b7bf7e9123dad1652b336a`（task-4）、
> `95899de994b87c6b15dae3c78bba12b0fbdd6606cfdfc8e639d758192b76f9ce`（task-4-errors）、
> `746897531862bec0bf16494699abb0fa8c285a02c1f2c271e20a11675fc185db`（task-5）、
> `68de1e19388fd5354b927a0f380c37b8dac8d204e69748a08968be7870b0c160`（task-6）、
> `ca297f097f771b2827fe57506752811d4ed90e5ef8b8f885122c7fa91a92a7ea`（task-7）、
> `6a641df9ffd0bd557e778ed8e43dc46b278afb4041278a2c3992ef2e1adb6853`（task-8）、
> `723fcfc22cd9f3b81ad75d650857063569a04ed4eb98328764821c9974e13c80`（task-8-reader）。

**算术闭合（v2，逐格与 9 份归档的 `合计:` 行一致）**：

```
断言支撑红     6 + 7 + 6 + 1 + 22 + 48 + 28 + 3 + 6 = 127
崩溃·超时红    0 + 0 + 0 + 0 +  2 +  3 +  4 + 0 + 0 =   9
允许的绿       0 + 0 + 0 + 0 +  1 +  2 +  0 + 0 + 0 =   3
未登记的绿     0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
证据无效       0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
----------------------------------------------------------
127 + 9 + 3 = 139 = 136（v1） + 3 条新变异
```

- 139 也等于 9 个集合各自 `集合里的变异总数` 之和：`6+7+6+1+25+53+32+3+6 = 139`。
- **与 v1 的 136 之差 = 新增 3 条变异**：task-2 `m06`、task-6 `m54`、task-6 `m55`；清单条数同步 7→8、34→35、22→23。注意 **task-7 的清单 +1 不产生新变异条目**（只是被推导多带进一条测试名）⇒ 是 136 + 3 = 139，**不是** 140。

### 7.4 零异常（9/9）

| 项 | v2 结果 |
|---|---|
| 编译失败行（`红(编译失败)`） | **0** —— 9 份归档全文 grep 零命中 |
| 子集横幅（「这是子集，不能作为权威表」） | **0** —— 9 份归档零命中；9/9 带 `集合里的变异总数 = N（本次运行 == 全集 ✓）` |
| `!!!` 行 | **0** —— 9 份归档逐份 `grep -c '!!!'` 均为 0 |
| 未登记的绿 | **0** |
| 证据无效 | **0** |
| stderr | 9/9 `.stderr` 均为 **0 字节** |
| HEAD 跑前 / 跑后 | 9/9 一致（`21ffec3f42eb22981f67ba2cf4de90d54ee4ba64`） |
| 工作树 | 9 个集合**串行**跑完后统一核对，跑前跑后均空 |

- **`EXPECTED_GREEN` 逐键核对**：全部落在允许的绿 —— task-5 `m13`（1 键 ⇒ 允许绿 1 条）、task-6 `m15`/`m34`（2 键 ⇒ 允许绿 2 条）；其余 7 个集合为**空字典**（见 §4.13 的阅读提示）。与 v1 相同，工具对「已登记却转红」**不报错**（§4.5），故这一步是**人工逐键核对**，不是工具自报。
- 崩溃/超时红的成员与 v1 **同名同数**：task-5 `{m10, m33}`、task-6 `{m16, m17, m21}`、task-7 `{m10, m11, m27, m28}` —— 无任何条目从断言红降级为超时红（9 个集合**串行**运行，无并发 CPU 竞争）。

### 7.5 三个新条目：全为**断言支撑红**（非崩溃 / 超时 / 编译）

| 条目 | 变异位置（`21ffec3f`） | 判定 | 自证串 | 杀死的用例（顶层） |
|---|---|---|---|---|
| task-2 `m06_删除条目数上界守卫` | cut `index.go:66-69` 的 `if n > maxTrailerEntries` | **红** | `names=8 ran=8 diff=0 PASS=7 SKIP=0 FAIL=1` | `TestReaderRejectsTrailerEntryCountOverBound` |
| task-6 `m54_删除DT入参校验` | cut `sealer.go:154-156` 的 `validateDT(task.DT)` | **红** | `names=35 ran=35 diff=0 PASS=34 SKIP=0 FAIL=1` | `TestSealRejectsIllegalDTAndInst` |
| task-6 `m55_删除Inst入参校验` | cut `sealer.go:157-159` 的 `validateInst(task.Inst)` | **红** | `names=35 ran=35 diff=0 PASS=34 SKIP=0 FAIL=1` | `TestSealRejectsIllegalDTAndInst` |

- **子例级归因只能靠隔离探针**：F2a 在提交前实测 —— 只 cut `validateDT` ⇒ 顶层 FAIL=1 且 **6 个 `dt_*` 子例红**（子例 PASS=4）；只 cut `validateInst` ⇒ 顶层 FAIL=1 且 **4 个 `inst_*` 子例红**（子例 PASS=6）。两处调用**各自承重**，故 `m54`/`m55` **分开成两条**：合并成「整体 cut ⇒ 10/10 红」无法分别证明两处。
- `m06` 的守卫在 `index.go`（task-2 的 `TARGET`），杀手用例在 `reader_internal_test.go`（**同一 Go 包**）。工具按**单文件 TARGET** 施加变异、按**用例名**拼 `-run`，故正确归属是**把杀手名字并进 task-2 的清单**（定点 grep 只并那一条，避免整段 grep 多选 15 条而判「证据无效 多=[…]」）—— 同 `task-4` 对 `TestParsePartSeqUsedBySealer` 的先例。清单 7→8、推导比对「一致」。
- **要求出处不在简报里**（如实登记，**不编造要求编号**）：`task-2-brief.md` 与 `task-6-brief.md` 都写于这两道守卫存在之前；`REQUIREMENTS` 的 plan 字段直接引用终审 A 的缺陷 ②/③、控制者裁定与 F1 落成处。

### 7.6 本轮形成的两条方法学规则

1. **表的「杀死用例」列是顶层用例名**（工具的 `-run` 是**顶层**）⇒ **子例级归因只能靠隔离探针**。`TestSealRejectsIllegalDTAndInst` 是一条 **10 子例**的表驱动用例，工具只会把整个顶层名写进「杀死的用例」列 —— 它**不区分**是 `dt_*` 还是 `inst_*` 子例红。要知道「哪几个子例承重」，必须像 §7.5 那样**单独构造隔离探针**（`m54` ⇒ 6 个 dt 子例、`m55` ⇒ 4 个 inst 子例）。
2. **`m54`/`m55` 与 `m11`/`m40` 并不互斥**：新用例断言的是错误**文案**（「dt 必须是…」/「Inst 必须是…」）⇒ **凡改变失败原因的变异都会踩它**：task-6 `m11_删除建目录` 的 FAIL 由 12 → 13、`m40_期望版本写死默认值` 的 FAIL 由 5 → 6，正是同一条新用例。所以「某变异杀死了该用例」**不等于**「该变异是该用例的目标守卫」。但两条变异对**子例**的判别力仍然**互相独立**（`m54` 只红 6 个 dt 子例、`m55` 只红 4 个 inst 子例）⇒ **判别力成立**。

### 7.7 `m03` 的重新定点及其规则（F2a 唯一超出「补三条」的改动，单独提交 `21ffec3f`）

- **为什么必须改**：F1 把 Inst 白名单抽成 `writer.go:74-79` 的**唯一一份** `validateInst`，`NewWriter` 改为调用它（`writer.go:211`）。于是 `m03` 的原模式 `if opts.Inst != instBlue && opts.Inst != instGreen {` 在 HEAD 上 **count=0** ⇒ 工具的 `--check` 报 **PATTERN-MISS** ⇒ **task-5 整个集合无法产出权威表**（比重新定点严重得多）。
- **怎么改**：定点改到 `NewWriter` 的**调用点** —— `if err := validateInst(opts.Inst); err != nil {` → `… && false {`。**刻意不动 `validateInst` 本身**：动它会连带停用 `Seal` 的入口校验，那是 **task-6 `m55` 的范围** ⇒ 会造成**跨集合证据串味**。
- **未改变任何判定（旁证）**：v1↔v2 的 `m03` 三列**逐字相同** —— `红 / names=26 ran=26 diff=0 PASS=24 SKIP=0 FAIL=2 / TestNewWriterRejectsEmptyRootAndInst, TestNewWriterRejectsIllegalInst`。这是「守卫搬家后重新定点」**而非**「为了让表好看而放宽变异」的证据。
- **控制者固化规则：五个条件全部成立才可重定点**
  1. 守卫**被重构/搬家**（不是「跑不通就改到能跑通」）；
  2. 新定点**保持语义与 blast radius**（并说明为什么不能改别处）；
  3. `--check` 在 HEAD 上**唯一定位**；
  4. **v1↔v2 的判定 / 杀死用例 / 自证三列逐字相同**；
  5. **写进集合文件头 + 条目注释 + 单独提交**。

  ⇒ **任一不成立（尤其 ②④）⇒ 停下报告，不要重定点。**

### 7.8 本轮修了什么（指向提交）

| # | 内容 | 提交 |
|---|---|---|
| 1 | `Seal` 入口补 `validateDT` / `validateInst`（`sealer.go:154-159`），新增用例 `TestSealRejectsIllegalDTAndInst`（10 子例：断言错误**文案** + **未产生任何产物** + spool 未动 + 反向断言合法值仍可封块且落在 `dt=`/`inst=` 内） | `cb3253f5` |
| 2 | trailer 条目数**硬上限**：`index.go:66-69` `if n > maxTrailerEntries`（常量 `reader.go:55` `= maxTrailerBytes / 8`），超限返回明确 `ErrTrailerCorrupt`、**绝不截断**；新增用例 `TestReaderRejectsTrailerEntryCountOverBound` | `cb3253f5` |
| 3 | `MarkSealed` 的次序陷阱 —— **只改契约文档**（`writer.go:395-419`，写明「**只可用于封块前**的滚动」+ 说明为何**不加** stat spool 守卫），**零行为改动**；真实修法属**计划 2** | `cb3253f5`（文档，与同文件的代码修复一并落地） |
| 4 | **6 处**文档/注释更正（含规格 §5.2），**跨两笔提交**：**`cb3253f5`**（因同文件一并落地）—— ① `writer.go`（`O_APPEND` 注释失实）⑤ `sealer.go`（`ErrNotFound` 分支按实测收敛）；**`221f8815`**（只改文字）—— ② `errors.go`（`ErrNotArchived` 实际用法）③ `footer.go`（补 frame_sha / 非邻接 / 越界）④ `defaults.go`（方案 C 后写侧无 Go 编码器）⑥ **规格 §5.2**（原写 `reserved(6)` 且**无 `row_count`** ⇒ 与实现 + §7.1/§8.2 自相矛盾；改为 `row_count(4) \| frame_sha256[:8](8) \| reserved(2)`，附「由终审 A 发现」说明） | `cb3253f5`（①⑤）+ `221f8815`（②③④⑥） |
| 5 | 为新增守卫补变异条目 `m06` / `m54` / `m55`，并在新冻结点 `21ffec3f` **重跑全部 9 个集合** | `62de2d22`（补条目）+ 本节（重跑归档） |

### 7.9 F1 **测量过的**原始数字（可引用）

| 项 | 值 |
|---|---|
| `maxTrailerBytes` | **67,108,864 B**（= 64 MiB，`reader.go:28`） |
| 最坏输入 | 64 MiB **全 `0x00`** 的 trailer（每字节都是合法单字节 varint） |
| 该输入的条目数 | **`n = 67,108,864`** 条 |
| 修复前的行为 | `decodeTrailer` **`err=nil` 成功返回**（不是报错）；`make` 分配 **+512.0 MiB**（536,876,232 B / 213 ms） |
| 装进真封块帧（`--long=29`）后 | 单请求 `GetBody` **成功**，峰值堆 **1090.0 MiB** |
| 稳态 2 并发 | **1666.0 MiB** |
| 连续 4 轮 | **3268.0 MiB** |
| 新上限 | **`maxTrailerEntries = 8,388,608`**（`reader.go:55`，`= maxTrailerBytes / 8`）；⇒ 索引分配的**上限** = 8,388,608 × 8 B = **64 MiB**；叠加 `tb`（≤ 64 MiB）与解码窗口（512 MiB）≈ **640 MiB/并发**（该项为**三项算术组合**，非单点实测） |
| 变异自检 | cut 守卫 ⇒ `err=<nil> line="{\"a\":1}"`（**静默成功读出**）⇒ 红 |

- 出处：`progress.md:3797`（F1 报告）。
- **未做真实 cgroup / OOM 观测**：2.2 GiB 是 `1090 × 2` 的**算术最坏值**，**不是**实测崩溃；实测稳态是 1666 MiB。
- **据实登记的代价（误报边界）**：块内的**行数**超过 `maxTrailerEntries` 时，合法产物的条目数会超过上限 ⇒ `GetBody` **响亮误报** `ErrTrailerCorrupt`（**非静默**）。这是**预算取舍**（用块大小推上限会更宽 ⇒ 更弱的内存界），登记为**计划 4 的验证项**，**不为它临时改预算或块大小**。
  - ⚠️ **本条初稿的边界措辞已被证伪，见 §8.6 缺陷 B**：初稿把它写成「块满 1.5 GiB **且**平均每行不足 192 字节」才触发 —— 那是**充分条件之一**而非必要条件。**真触发条件与块的字节大小无关**（实测 64 MiB 的块即触发，见 §8.6）。**以 §8.6 与计划 D35 的更正版为准。**

---

## 8. R11 v3（写侧上界修复轮 F3 后重跑）

> **性质**：本节是**修复轮 F3（`949c3a27` + `ea962a70`）之后**在同一分支上的**第三次全量重跑**，与 §0–§6（v1，冻结 `c13b64bc`）、§7（v2，冻结 `21ffec3f`）**并列，不替代**。
> **数据来源**：9 份 `task-N-evidence/r11v3-*-table.txt` 与承载它们的 **7 份** `task-N-evidence/r11v3-section.md`（`task-4` 与 `task-8` 的**共享证据目录各含两节**，故 7 份文件承载 9 个集合 —— 同 v1/v2 的 7 份情形）；本节对 9 份 v3 归档的 sha256 **全部用 `shasum -a 256` 复算**，与各片段自报值逐位一致；`!!!`、子集横幅、编译失败行也由本节对 9 份 v3 归档逐份 `grep` 复算。

### 8.1 为什么必须再跑

1. **代码又变了 ⇒ v1/v2 证据再次失效**。F3 在**写侧**新增一条守卫、一个新哨兵并加固两处测试、更正一处注释，落在 `internal/tracearchive/{errors,reader,sealer,sealer_internal_test}.go` 与 `tests/tracearchive/{ref,sealer}_test.go`（阶段一 `949c3a27`，恰 6 个文件），以及 `tools/trace-mutate/sets/task-6.py`（阶段二 `ea962a70`）。按 D16 的**证据绑定文件哈希**原则，v1/v2 两套表对新树都不再成立。
2. **注释改动同样重跑**（与 §7.1 同一条原则）：**缺陷 B 的修复只改 `reader.go:50-56` 的注释措辞**（零行为改动）；**缺陷 C 的修复在 `tests/tracearchive/sealer_test.go` 补了「调用前后目录集合逐字相同」断言**（测试文件哈希随之改变）⇒ **文本/测试变了，哈希绑定就断了**。
3. ⇒ **9 个集合全部重跑**，而不是只跑被 F3 触及的 task-6。

### 8.2 新冻结点 `ea962a70…`；**v1/v2 的表未被覆盖**

| 项 | v1 | v2 | v3 |
|---|---|---|---|
| 冻结 HEAD（跑前 / 跑后） | `c13b64bcc2a344193f854063adbf5a94c0e86760` | `21ffec3f42eb22981f67ba2cf4de90d54ee4ba64` | **`ea962a70bf18016842886c6a9a35756191167d79`** |
| 该冻结点由什么组成 | — | `221f8815` ＋ `62de2d22` ＋ `21ffec3f` | `949c3a27`（F3 阶段一：`Seal` 写侧守卫 ＋ 新哨兵 `ErrTooManyRows` ＋ 目录断言/哨兵表加固 ＋ 注释更正）＋ `ea962a70`（F3 阶段二：`sets/task-6.py` 补 `m56`/`m57`/`m58`） |
| 归档文件名 | `task-N-evidence/r11-*-table.txt` | `task-N-evidence/r11v2-*-table.txt` | `task-N-evidence/r11v3-*-table.txt`（**新文件名**） |
| 工具 sha256 | `10dea750…` | `10dea750…` | `10dea750…`（**与 v1/v2 相同**；`trace_mutate.py` 零改动） |
| 承载片段 | 7 份 `r11-section.md` | 7 份 `r11v2-section.md` | 7 份 `r11v3-section.md` |

- **三套表并存（未被覆盖）**：`ls *-evidence/r11-*-table.txt` ⇒ **9 / 9 / 9**（v1 / v2 / v3 各 9 张）；承载片段同为 **7 / 7 / 7**。本节**不改写 §1/§2/§7 的任何计数行**，也不移动任何旧归档。
- **9/9 表的 `git rev-parse HEAD` 行**自报的也都是 `ea962a70bf18016842886c6a9a35756191167d79`；9 次重跑**串行**执行，跑前/跑后工作树均空、期间零提交（临时复现测试 `f3_repro_test.go` 已删）。

### 8.3 v3 九表（计数 + 与 v2 差异 + 归档 sha256）

| 集合 | 断言红 | 崩溃/超时 | 允许绿 | 未登记绿 | 无效 | 与 v2（冻结 `21ffec3f`）差异 | 归档 sha256 |
|---|---|---|---|---|---|---|---|
| task-2 | 6 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 6 条；判定/FAIL/杀手列漂移均 0） | `01d0b25f…` |
| task-3 | 7 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 7 条） | `21f40736…` |
| task-4 | 6 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 6 条） | `390a57bf…` |
| task-4-errors | 1 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 1 条） | `8232110e…` |
| task-5 | 22 | 2 | 1 | 0 | 0 | 逐条零漂移（共同 25 条；含 `m03` 重定点条目） | `074e91f4…` |
| **task-6** | **51** | **3** | **2** | 0 | 0 | **53→56：新增 `m56`/`m57`/`m58`（均断言红）**；共同 53 条**零判定漂移**；`m11` 的 FAIL 数 13→14；`m11`/`m17` 的杀手列各多一条新用例；清单 35→**36**（推导驱动） | `814c815c…` |
| task-7 | 28 | 4 | 0 | 0 | 0 | 逐条零漂移（共同 32 条） | `e3a5b298…` |
| task-8 | 3 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 3 条） | `df5d5467…` |
| task-8-reader | 6 | 0 | 0 | 0 | 0 | 逐条零漂移（共同 6 条） | `c8d2efaf…` |
| **合计** | **130** | **9** | **3** | **0** | **0** | **142 = 139（v2） + 3 条新变异** | — |

> 上表 sha256 为前 8 位；完整值（本节 `shasum -a 256` 复算，与各 `r11v3-section.md` 自报值逐位一致）：
> `01d0b25fe48dbb1a5d8b4428334758fc5d1120b0908c88dc2c47528b278c8e2d`（task-2）、
> `21f407361bd76a621d2e6729be548b38102bc9798684ac9a3966820e6f9f5b92`（task-3）、
> `390a57bf651b6d77c152de2d10a77f6d5256539446617e6bbefaffb6611e20df`（task-4）、
> `8232110eaa1d37d0bbd7cb1a146fafa6c566862f46c215cfb32cbb9408218866`（task-4-errors）、
> `074e91f4a63cac4c355ae12e8d7c1380672860f34c784786d6d8deb342aea6a3`（task-5）、
> `814c815c0b74a402892aec20c87298d09efcd8f2844a579fff8cfa1b0967318f`（task-6）、
> `e3a5b298b78a105a1abbaca9aa0fe9fc944ce4fb1b287e38453000804a5c15d9`（task-7）、
> `df5d5467c2cdfdde5df6ded7eb60de1c0597e088a88c8076b571b196e55010b3`（task-8）、
> `c8d2efaf2f5fca6d37f62ea8e21f5ce891dc67fc466c5f7bf53cee5e0b5a832d`（task-8-reader）。

**算术闭合（v3，逐格与 9 份归档的 `合计:` 行一致）**：

```
断言支撑红     6 + 7 + 6 + 1 + 22 + 51 + 28 + 3 + 6 = 130
崩溃·超时红    0 + 0 + 0 + 0 +  2 +  3 +  4 + 0 + 0 =   9
允许的绿       0 + 0 + 0 + 0 +  1 +  2 +  0 + 0 + 0 =   3
未登记的绿     0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
证据无效       0 + 0 + 0 + 0 +  0 +  0 +  0 + 0 + 0 =   0
----------------------------------------------------------
130 + 9 + 3 = 142 = 139（v2） + 3 条新变异
```

- 142 也等于 9 个集合各自 `集合里的变异总数` 之和：`6+7+6+1+25+56+32+3+6 = 142`。
- **与 v2 的 139 之差 = 新增 3 条变异，且全部为断言支撑红**：task-6 `m56`/`m57`/`m58`（`m56` 删守卫、`m57` 只换哨兵、`m58` 入口守卫搬家）。**8 个集合逐条零漂移**（task-2 / task-3 / task-4 / task-4-errors / task-5 / task-7 / task-8 / task-8-reader 的共同条目判定、FAIL 数、杀手列全部与 v2 一致）。

### 8.4 零异常（9/9）

| 项 | v3 结果 |
|---|---|
| 编译失败行（`红(编译失败)`） | **0** —— 9 份归档全文 grep 零命中 |
| 子集横幅（「这是子集，不能作为权威表」） | **0** —— 9 份归档零命中；9/9 带 `集合里的变异总数 = N（本次运行 == 全集 ✓）` |
| `!!!` 行 | **0** —— 9 份归档逐份零命中 |
| 未登记的绿 | **0** |
| 证据无效 | **0** |
| stderr | 9/9 `.stderr` 均为 **0 字节** |
| HEAD 跑前 / 跑后 | 9/9 一致（`ea962a70bf18016842886c6a9a35756191167d79`） |
| 工作树 | 9 个集合**串行**跑完后统一核对，跑前跑后均空 |

- **`EXPECTED_GREEN` 逐键核对**：全部落在允许的绿 —— task-5 `m13`（1 键 ⇒ 允许绿 1 条）、task-6 `m15`/`m34`（2 键 ⇒ 允许绿 2 条）；其余 7 个集合为**空字典**（见 §4.13 的阅读提示）。与 v1/v2 相同，工具对「已登记却转红」**不报错**（§4.5），故这一步是**人工逐键核对**，不是工具自报。
- 崩溃/超时红的成员与 v1/v2 **同名同数**：task-5 `{m10, m33}`、task-6 `{m16, m17, m21}`、task-7 `{m10, m11, m27, m28}` —— 无条目从断言红降级为超时红（9 个集合串行运行，无并发 CPU 竞争）。

### 8.5 三个新条目（全为**断言支撑红**）与隔离探针

| 条目（task-6 v3） | 变异位置（`ea962a70`） | 判定 | 自证串 | 杀死的用例（顶层） |
|---|---|---|---|---|
| `m56_删除Seal行数上界守卫` | cut `sealer.go:226-229` 的 `if len(offsets) > maxTrailerEntries` | **红** | `names=36 ran=36 diff=0 PASS=35 SKIP=0 FAIL=1` | `TestSealRefusesSpoolOverTrailerEntryBound` |
| `m57_行数上界改用行数不符哨兵` | 只把该守卫的 `ErrTooManyRows` 换成 `ErrRowCountMismatch`（守卫仍拦） | **红** | `names=36 ran=36 diff=0 PASS=35 SKIP=0 FAIL=1` | `TestSealRefusesSpoolOverTrailerEntryBound` |
| `m58_入口守卫搬到MkdirAll之后` | 把 `Seal` 入口的 `validateDT`/`validateInst` 搬到 `MkdirAll`（`sealer.go:246`）**之后** | **红** | `names=36 ran=36 diff=0 PASS=35 SKIP=0 FAIL=1` | `TestSealRejectsIllegalDTAndInst` |

**隔离探针（子例级归因，工具的表读不出这一层 —— 见 §7.6 规则 1）**：

- `m56`/`m57` ⇒ **只杀新用例** `TestSealRefusesSpoolOverTrailerEntryBound`（`internal/tracearchive/sealer_internal_test.go:77`，A 层）：该用例的 **2 个「超限/…」子例转红**（可用编码器 / 编码器不可用 —— 后者同时钉住守卫必须先于任何编码器接触），**「恰好等于上界/可封块」子例仍绿**（`==max` 的合法形态必须放行）。
- `m58` ⇒ 杀 `TestSealRejectsIllegalDTAndInst`（`tests/tracearchive/sealer_test.go:1449`；F3 的目录断言在 `:1501`/`:1531-1534`）的 **10 个子例全部转红**，但**全部死在目录集合断言 `:1532`**（`if dirsAfter := listDirs(base); !slices.Equal(dirsBefore, dirsAfter)`），**不是**死在 `err == nil` —— 错误文案断言仍通过（守卫只是被搬到了建目录之后）。
- **与 `m54`/`m55` 互不替代**：`m54`（cut `validateDT`）⇒ **6 个 `dt …` 子例**红、`m55`（cut `validateInst`）⇒ **4 个 `inst …` 子例**红（均死在 `err == nil` 与文案断言上）；`m58` 判在**另一处断言**（目录集合）⇒ 两条证据面独立，**修前 `m58` 会判绿**（旧用例只断言无 `.jsonl.zst`/`.tmp`），补断言后才转红。
- 三个提交的文件面（控制者逐行核实）：`949c3a27` 恰为 6 个文件；`ea962a70` = `sets/task-6.py`。

### 8.6 缺陷 A / B / C 的修前 / 修后实测（F3 复现 + 控制者核实）

> **出处与证据等级**：**输入形态与代码落点**可在 **git-tracked 文件**里逐字读到（`internal/tracearchive/sealer.go:206-229`、`internal/tracearchive/reader.go:50-56`、`internal/tracearchive/sealer_internal_test.go:64-76`、`tests/tracearchive/sealer_test.go:1451-1470`）；但**秒级耗时**（784ms / 188ms）**只存在于 gitignored 的台账** `progress.md:4115-4118` **与 F3 报告**，**不在任何 v3 归档里** ⇒ 该两数属**台账自述，非本报告复算**，读者不得把它当权威表数据引用。

| 阶段 | 输入（8,388,609 行 × `{"a":1}`，块仅 **64.0 MiB**） | `Seal` 结果 | spool | 产物 | 随后 `GetBody` |
|---|---|---|---|---|---|
| **修前**（`949c3a27` 之前） | 同上 | `err = **nil**`（约 784ms，**台账自述**） | **唯一 spool 被删** | 产物落地 | `ErrTrailerCorrupt`（「期望 8388609 条超过条目数上界 8388608」）⇒ **正文永久不可达** |
| **修后**（`949c3a27` 起） | 同上 | `ErrTooManyRows`（约 188ms，**台账自述**） | **spool 保留** | **无任何产物（含 `.tmp` 与目录）** | —（spool 仍在，正文仍可经 spool 路径取回） |

- **缺陷 A（严重：数据丢失）**：条目数上界原先**只在读侧**（`grep -c 'maxTrailerEntries' internal/tracearchive/sealer.go` 在修前 = 0）⇒ `Seal` 封出「读不回」的产物**并删掉唯一 spool**，违反规格 §7.3「**宁可留下 spool**」。修后守卫在 `sealer.go:226-229`（注释 `:206-225`），位于 ② 行数交叉校验**之后**、版本探测 / `MkdirAll`(`:246`) / `Rename`(`:273`) / **删 spool(`:282`)** **之前** ⇒ **不建目录、不删 spool**。
- **新哨兵 `ErrTooManyRows`**（`errors.go:44-55`）：**不复用** `ErrTrailerCorrupt`（读侧语义＝「产物损坏」，对写侧是**错误诊断**）也不复用 `ErrRowCountMismatch`（在本形态下 `Rows == len(offsets)`，「不符」同样是**错误诊断**）。语义固定为「拒绝封块 + 保留 spool + 无任何产物 + **需人工介入**」——与前两者不同，**单纯重试永远不会成功**。用例 `TestSealRefusesSpoolOverTrailerEntryBound`（`sealer_internal_test.go:77`，体至 `:229`）。
- **缺陷 B（措辞失实）**：真触发条件＝**块内行数 > `maxTrailerEntries`**，**与块的字节大小无关**；64 MiB 反例（8,388,609 行 × 8 字节）即证伪「必须块满 1.5 GiB」的说法。1.5 GiB / 平均每行不足 192 字节只是**充分条件之一**。注释已改在 `reader.go:50-56`。
- **缺陷 C（测试盲区）**：`TestSealRejectsIllegalDTAndInst` 原先只断言无产物、**不断言无目录** ⇒ 把入口守卫搬到 `MkdirAll` 之后用例仍 `ok`，而越出 root 的目录已被创建。F3 加「调用前后目录集合逐字相同」断言（`tests/tracearchive/sealer_test.go:1501` 取快照 + `:1531-1534` 比对，失败于 `:1532`）；该变体实测 **10/10 子例转红**。
- **哨兵互异表**：`tests/tracearchive/ref_test.go:91` 把 `ErrTooManyRows` 登记进 `TestSentinelErrorsAreDistinct`（**6 → 7**）；注释写明「退化成 `ErrRowCountMismatch` 时本表是唯一的拦截点」。

### 8.7 本轮修了什么（指向提交）

| # | 内容 | 提交 |
|---|---|---|
| 1 | `Seal` 侧行数上界守卫 + 新哨兵 `ErrTooManyRows`（`sealer.go:226-229`、`errors.go:44-55`），新增 A 层用例 `TestSealRefusesSpoolOverTrailerEntryBound` | `949c3a27` |
| 2 | `reader.go:50-56` 边界措辞更正（真触发 = 行数超上界，与字节大小无关，附 64 MiB 反例） | `949c3a27` |
| 3 | `TestSealRejectsIllegalDTAndInst` 补「调用前后目录集合逐字相同」断言（`sealer_test.go:1501`/`:1531-1534`）+ `ref_test.go:91` 哨兵表 6→7 | `949c3a27` |
| 4 | 为上述守卫补变异条目 `m56`/`m57`/`m58`（含隔离探针与 `m54`/`m55` 互不替代） | `ea962a70` |
| 5 | 在新冻结点 `ea962a70` **重跑全部 9 个集合**并归档 `r11v3-*` | 本节（F4 归档） |

---

## 附：本报告的自证边界（如实声明）

1. 本报告**没有重新运行**任何变异集合。§0–§6（**v1**）的所有计数、判定、sha256 均逐条来自 §0 列出的 9 份 v1 片段与 9 份 v1 原始表（唯一例外：§0.2 与 §1.0 中标注为「本报告复算」的 sha256，由本报告执行 `shasum -a 256` 得出）；**§7（v2）**与 **§8（v3）**的计数与判定逐条来自对应的 `r11v2-*` / `r11v3-*` 片段与原始表，其中 **9 份 v2 与 9 份 v3 归档的 sha256 由本报告全部复算**（与片段自报值逐位一致），`!!!` / 子集横幅 / 编译失败行也由本报告对两套归档逐份 `grep` 复算。**§8.6 的秒级耗时（784ms / 188ms）是唯一例外**：它只在 gitignored 的台账里，已在该处显式标注为**台账自述、非本报告复算**。
2. 本报告**无法独立复核**每条变异的逐行 `go test` 输出（工具正常模式不归档，见 §4.4）；它能保证的上限是「证据文件之间、证据文件与台账最终账之间**逐格一致**」。
3. 「无漂移」是**对各集合原有旁证的一致性核对**，不是一次独立的抗竞争实验；task-8 的 9.632s 探针是其中唯一带量化余量的测量（§3.1）。
