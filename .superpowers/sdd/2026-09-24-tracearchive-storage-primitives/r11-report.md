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

---

## 5. R11 产生的发现汇总

| # | 发现 | 证据出处 | 处置 / 去向 |
|---|---|---|---|
| 1 | **Task 5 旧 30 条变异对 `brief:253-254` 覆盖为零**（旧 M10 删的是 `closeCurLocked` 的 nil 守卫，不是 `PendingSeals` 的）⇒ **旧证据强度被高估** | `task-5-evidence/r11-task-5-table.txt:37`（映射表说明栏）；`progress.md:3033` | **已由新增 `m33` 关闭**（权威表 `m33` = 红(崩溃/中止)，杀死 `TestWriterPendingSealsNilBeforeAnyAppend`，正是简报预测的 panic 杀法） |
| 2 | `NewWriter` 两个阈值默认值分支**零变异覆盖**（`internal/tracearchive/writer.go:196-198` 与 `:199-201`）：**可达**、已被 `TestWriterPendingSealsAppliesDefaultThresholds` 约束 ⇒ **缺的是变异证据而非覆盖** | `tools/trace-mutate/sets/task-5.py:390-431`（`COVERAGE_GAPS`，含 `where`/`reachable`/`how_to_close`/`aux_verified`）；`progress.md:3058-3074` | **本轮按裁决只登记、不扩范围**。`how_to_close` 的两条 `("cut", …)` pattern **已预实测**：各自唯一定位、编译通过、判定红、杀死用例恰为该用例。**这是计划缺陷/工具登记，不是本报告的变异数** |
| 3 | Task 2 的 `m01` 红靠**分配量阈值**（`alloc > 1<<20`，`index_test.go:66`）而非错误身份 | `task-2-evidence/r11-task-2-table.txt` 映射表 m01 说明（「简报里的用例名是旧名…它断言的是分配量而非错误身份，缺守卫时 varint 耗尽仍报同一个 `ErrTrailerCorrupt`」） | **登记，供终审考虑是否加强**（未在本轮改集合） |
| 4 | Task 3 的 `m03_C` **只在清单范围内成立**：清单内仅 `TestFooterGoldenBytes` 红；清单外另有一条 `tests/tracearchive/reader_test.go:597 TestReaderRejectsTrailerRowCountMismatch` 红在**它自己的前置断言**上 | `task-3-evidence/r11-section.md` 异常 4；`progress.md:2895-2901` | **已如实登记**，措辞被限制为「只在 task-3 清单范围内成立」，不许升级为「全仓唯一探针」 |
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

## 附：本报告的自证边界（如实声明）

1. 本报告**没有重新运行**任何变异集合；所有计数、判定、sha256 均逐条来自 §0 列出的 9 份片段与 9 份原始表（唯一例外：§0.2 与 §1.0 中标注为「本报告复算」的 sha256，由本报告执行 `shasum -a 256` 得出）。
2. 本报告**无法独立复核**每条变异的逐行 `go test` 输出（工具正常模式不归档，见 §4.4）；它能保证的上限是「证据文件之间、证据文件与台账最终账之间**逐格一致**」。
3. 「无漂移」是**对各集合原有旁证的一致性核对**，不是一次独立的抗竞争实验；task-8 的 9.632s 探针是其中唯一带量化余量的测量（§3.1）。
