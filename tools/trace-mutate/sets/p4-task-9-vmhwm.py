r"""计划 4 / 任务 9 的变异集合 B：`internal/traceverify/vmhwm.go` 里
「峰值常驻内存解析 + decode-bench 的**点读负载**」（A6 / C5 的测量面）。

主题（三条来自计划任务 9 步骤 5 的 m01–m03，其余为 E70② 登记的**同文件追加**）：

  * m01 取成 `VmPeak`（峰值虚拟）而不是 `VmHWM`（峰值常驻）⇒ 读数偏大一个量级；
  * m02 kB → B 少乘 1024 ⇒ 读数小 1024 倍（内存预算看起来极宽裕）；
  * m03 缺字段时返回 `0, nil` ⇒ 把「没有证据」洗成「内存很小」；
  * m04 行号固定成 0（不随机）⇒ 「随机点读」名不副实；
  * m05 goroutine 数写成 K+1 ⇒ 档位与实测并发不符；
  * m06 点读错误被忽略 ⇒ 盘上有问题却报一次「成功的标定」；
  * m07 行数探测差一行 ⇒ 随机点读会落到不存在的行上；
  * m08 峰值采样失败被忽略 ⇒ 读不到 VmHWM 却继续报读数；
  * m09 峰值到阈值不中止 ⇒ GOGC=off 下进程被 OOM 杀掉、什么都报不出来。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 9 步骤 5 的 `p4-task-9-vmhwm.py` 给出 m01–m03 与各自杀手，本文件逐字采用。
**追加 m04–m09 的理由**（E70②，如实登记）：计划任务 9 步骤 3 要求 `decode-bench`
做「K 个 goroutine 各做 50 次**随机点读**（`tracearchive.NewReader` + `GetBody`）」，
而上一轮实现成了整流解码 ⇒ 本轮按计划落实点读负载。落实点读负载必然引入两类新代码：
① 负载本身（并发度 / 随机性 / 错误传播）；② 支撑它的两条真实操作 —— 用**公开 API 的
错误身份**探测块内行数、以及 GOGC=off 形态下的**到限中止**。按 **E62**（行为要求必须
由在册行为测试承载）这三组行为都各有在册用例；本文件为它们补上确定性杀手，
以免出现「有用例、零变异覆盖」的登记（E64② 的形态只用于**确实**没有杀手的断言）。
计划文本只列 m01–m03 是**最小集**，本文件的地面真相以本文件为准。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

**真实产物的解码路径本身**（`tracearchive.Reader` 在 512 MiB 窗口下解一个真实块）**不可能**
由本集合证明：它需要本机有 zstd（真实封块）与 `/proc`（VmHWM），按全局约束第 55 行属
「重活」，只能走 `cmd/trace-verify` + 手工剧本、**不得**进任何 `p4-task-*.py` 的清单。
本集合钉住的是**负载的形状**（并发 / 随机 / 错误 / 中止）与**解析的算术**；
真实读数按「待宿主执行」登记在 `p4-task-9-a6-deferral-at-*.txt`，**不**标 `EXPECTED_GREEN`。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

`PointReadLoad.Run` 的四条入口守卫里，只有 `Concurrency <= 0` 有变异杀手（m10）。
另外三条**没有**专属变异（`Read == nil` / `Rows <= 0` / `Rounds <= 0`），理由与承载方式：

  * **位置**：`internal/traceverify/vmhwm.go` 的 `Run` 开头四个 `if … { return error }`；
  * **断言什么**：`TestPointReadLoadRejectsInvalidArguments` 的四个子用例各自断言「报错」
    且「一次读都没发生」；
  * **为什么没有杀手**：把任一条守卫「放宽」都会让非法输入**流入负载**（`Rows=0` 让
    `Int63n(0)` panic、`Read=nil` 让调用直接 panic），于是变异红成**崩溃**而不是断言红 ——
    全局约束第 5 条要求变异必须红成断言红，故这三条**不写成变异**，改为人工探针；
  * **可失败性由谁承载**：人工探针（把 `if l.Read == nil { return error }` 的守卫体换成
    「注入一个恒返回 `nil, nil` 的桩」⇒ `Read为空` 子用例在 `require.Error` 处转红，
    形态是断言红而非崩溃）；探针输出落在 `p4-task-9-probe-at-*.txt`；
  * **谁来补**：后续若要给这三条补变异，正确形态是「把守卫体换成安全兜底」而不是「删掉守卫」。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。每个变异都必须红成**断言红**。
"""

NAME = "p4-task-9-vmhwm"
TARGET = "internal/traceverify/vmhwm.go"
PACKAGES = ["./tests/traceverify/"]
MANIFEST_FILES = ["tests/traceverify/vmhwm_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/traceverify/vmhwm_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部注入驱动（替身 reader / 替身 peak）：不依赖真实产物、/proc、zstd，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 取成 VmPeak（峰值虚拟）而不是 VmHWM（峰值常驻）⇒ C5 的内存读数偏大一个量级
#     （虚拟地址空间含未落页的映射）。
#     杀手：TestParseVmHWMReadsPeakResidentNotVirtual（+ 同族的其它 ParseVmHWM 用例）
MUTATIONS["m01_取VmPeak而不是VmHWM"] = [
    ("sub", 'fields[0] != "VmHWM:"', 'fields[0] != "VmPeak:"'),
]

# m02 单位换算错：kB → B 少乘 1024 ⇒ 所有内存读数缩小 1024 倍。
#     杀手：TestParseVmHWMConvertsKilobytesToBytes
MUTATIONS["m02_少乘1024"] = [
    ("sub", "return kb * bytesPerKB, nil", "return kb, nil"),
]

# m03 解析失败时返回 0 而不是错误 ⇒ 「没有证据」被洗成「内存很小」。
#     杀手：TestParseVmHWMErrorsOnMissingField
MUTATIONS["m03_缺字段返回0nil"] = [
    ("sub", 'return 0, fmt.Errorf("%w: 没有任何以 `VmHWM:` 起头的行", ErrVmHWMMissing)',
     "return 0, nil"),
]

# m04 行号固定成 0（不随机）⇒ 「K 个 goroutine 各做 50 次**随机**点读」名不副实：
#     所有并发都打在同一行上，缓存局部性让峰值失去代表性。
#     **刻意用 `rng.Int63n(1)`（恒 0）而不是字面量 `0`**：后者会让 `rng` 变成未使用的
#     变量 ⇒ **编译红**（本集合第一版正是这样，已被 `--list` 的表判为「红(编译失败)」并就地改写；
#     全局约束第 5 条要求每条变异都红成**断言红**）。
#     杀手：TestPointReadLoadRunsConcurrentlyAndRandomly（DistinctRows == 1）
MUTATIONS["m04_行号固定为0"] = [
    ("sub", "ref.Row = int(rng.Int63n(int64(l.Rows)))", "ref.Row = int(rng.Int63n(1))"),
]

# m05 多起一个 goroutine（K+1）⇒ 实测同时在飞峰值与档位不符，档位声明失真。
#     杀手：TestPointReadLoadRunsConcurrentlyAndRandomly（MaxInFlight == K+1）
MUTATIONS["m05_多起一个goroutine"] = [
    ("sub", "for slot := 0; slot < l.Concurrency; slot++", "for slot := 0; slot <= l.Concurrency; slot++"),
]

# m06 点读错误被忽略（& false 形态：保住 abortRequested 的引用，避免编译红）。
#     杀手：TestPointReadLoadPropagatesReadErrors
MUTATIONS["m06_点读错误被忽略"] = [
    ("sub", 'if err != nil {\n\t\t\t\t\tif atomic.LoadInt32(&abortRequested) == 1',
     'if err != nil && false {\n\t\t\t\t\tif atomic.LoadInt32(&abortRequested) == 1'),
]

# m07 行数探测返回 lo 而不是 lo+1（整体差一行）⇒ 随机点读会落到不存在的行上
#     （每个块的最后一行的读取都变成「元数据与存储不一致」）。
#     杀手：TestArtifactRowsBinaryProbesRowCount
MUTATIONS["m07_行数探测差一行"] = [
    ("sub", "return lo + 1, nil", "return lo, nil"),
]

# m08 峰值采样失败被忽略 ⇒ 读不到 VmHWM 却继续跑完并报一个「成功」的读数。
#     杀手：TestPointReadLoadPropagatesPeakErrors
MUTATIONS["m08_峰值采样失败被忽略"] = [
    ("sub", 'recordErr(fmt.Errorf("traceverify: 采样峰值常驻内存失败: %w", err))', "_ = err"),
]

# m09 峰值到阈值不中止（& false 形态）⇒ GOGC=off 下进程会一路涨到被 OOM 杀掉，
#     于是「峰值是多少」这条读数根本产不出来。
#     杀手：TestPointReadLoadAbortsWhenPeakReachesLimit
MUTATIONS["m09_到限不中止"] = [
    ("sub", "if l.AbortAtBytes > 0 && v >= l.AbortAtBytes {",
     "if l.AbortAtBytes > 0 && v >= l.AbortAtBytes && false {"),
]

# m10 并发档位 ≤ 0 的守卫放宽成「< 0」⇒ 档位 0 被放行（配置了一个 0 并发的档位）。
#     杀手：TestPointReadLoadRejectsInvalidArguments 的「Concurrency为零」子用例
MUTATIONS["m10_档位零被放行"] = [
    ("sub", "if l.Concurrency <= 0 {", "if l.Concurrency < 0 {"),
]

# m11 行数探测把「读不懂的错误」当成「越界」（default 分支返回 nil）⇒ 探测出一个偏小的
#     行数，随机点读随即落到不存在的行上。
#     杀手：TestArtifactRowsPropagatesNonRangeErrors
MUTATIONS["m11_未知错误当成越界"] = [
    ("sub", "default:\n\t\t\treturn false, err", "default:\n\t\t\treturn false, nil"),
]

CHECKS = []

REQUIREMENTS = [
    {
        "plan": "ParseVmHWM 取 VmHWM（峰值常驻），不是 VmPeak（峰值虚拟）",
        "mutations": ["m01_取VmPeak而不是VmHWM"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestParseVmHWMReadsPeakResidentNotVirtual`。",
    },
    {
        "plan": "kB → B 必须乘 1024（A6/C5 的预算与 peak_vmhwm_bytes 都按 bytes 比较）",
        "mutations": ["m02_少乘1024"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestParseVmHWMConvertsKilobytesToBytes`（1 kB 与 640 MiB 两个量级）。",
    },
    {
        "plan": "缺 VmHWM 行 / 读不懂 ⇒ 报错（绝不返回 0 冒充读数）",
        "mutations": ["m03_缺字段返回0nil"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestParseVmHWMErrorsOnMissingField`（四条缺字段形态 + 错误身份 `ErrVmHWMMissing`）。",
    },
    {
        "plan": "decode-bench：K 个 goroutine × 每 goroutine Rounds × PointsPerGoroutine 次**随机**点读",
        "mutations": ["m04_行号固定为0", "m05_多起一个goroutine"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestPointReadLoadRunsConcurrentlyAndRandomly`"
                 "（点数跑满 / 实测在飞峰值 == K / DistinctRows > 1 / 行号落在 [0, rows)）。"),
    },
    {
        "plan": "decode-bench：任一次点读失败 ⇒ 原样上抛（不返回半个读数）",
        "mutations": ["m06_点读错误被忽略"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestPointReadLoadPropagatesReadErrors`。",
    },
    {
        "plan": "decode-bench：用公开 API 的错误身份探测块内行数（越界 ⇒ ErrNotArchived、标记行 ⇒ ErrBodyMissing）",
        "mutations": ["m07_行数探测差一行"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestArtifactRowsBinaryProbesRowCount`"
                 "（1 / 2 / 3 / 5 / 1000 五档 + 中点为标记行）与 "
                 "`TestArtifactRowsPropagatesNonRangeErrors`（未知错误原样上抛）。"),
    },
    {
        "plan": "decode-bench：峰值读不到 ⇒ 报错（不许把「没有证据」洗成「内存很小」）",
        "mutations": ["m08_峰值采样失败被忽略"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestPointReadLoadPropagatesPeakErrors`。",
    },
    {
        "plan": "decode-bench：峰值到 AbortAtBytes ⇒ 中止负载并置 Aborted（不是失败），留下已观测峰值",
        "mutations": ["m09_到限不中止"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestPointReadLoadAbortsWhenPeakReachesLimit`"
                 "（`NoError` + `Aborted` + `PeakBytes ≥ 阈值` + 点数未跑满）。"),
    },
    {
        "plan": "参数非法 ⇒ 在任何点读之前报错（Read 为空 / Rows / Concurrency / Rounds ≤ 0）",
        "mutations": ["m10_档位零被放行"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestPointReadLoadRejectsInvalidArguments`（四个子用例，"
                 "并断言一次读都没发生）。另外三条守卫没有专属变异（会红成崩溃）⇒ 按 E64② 登记在"
                 "文末的 `COVERAGE_GAPS`，可失败性由人工探针承载。"),
    },
    {
        "plan": "行数探测：未知错误必须原样上抛（不许当成「行号越界」）",
        "mutations": ["m11_未知错误当成越界"],
        "checks": [],
        "note": "**行为**要求 ⇒ 在册用例 `TestArtifactRowsPropagatesNonRangeErrors`。",
    },
]

EXPECTED_GREEN = {}
