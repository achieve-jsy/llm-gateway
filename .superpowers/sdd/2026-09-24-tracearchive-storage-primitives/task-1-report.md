# Task 1 报告：引入 zstd 依赖并完成 U7/U8 标定

**状态：** DONE_WITH_CONCERNS
**执行者：** 子代理（运行中途失败，代码已完成；实测由其完成，收尾与结论由控制者补全）

## 实现了什么

| 文件 | 内容 |
|---|---|
| `go.mod` / `go.sum` / `vendor/` | 引入 `github.com/klauspost/compress`，已 `go mod vendor` |
| `internal/tracearchive/defaults.go` | 四个参数常量：级别 19、窗口 512 MiB、块 1.5 GiB、封块间隔 15 min |
| `internal/tracearchive/calib_test.go` | U7/U8 常驻回归测试（`TestGoEncoderPeakRSS` / `TestGoEncoderRatioRegression`） |
| `cmd/trace-calib/main.go` | 标定工具：支持 `-level` / `-window` / `-encodeall`，流式模式与生产封块路径一致 |

## 测试与结果

```
$ go test ./internal/tracearchive/ -run 'TestGoEncoder' -v
=== RUN   TestGoEncoderPeakRSS
    calib_test.go:50: 编码器堆占用 1058 MB（预算 1200 MB）
--- PASS: TestGoEncoderPeakRSS (3.34s)
=== RUN   TestGoEncoderRatioRegression
    calib_test.go:81: 压缩比 8097.3x（下界 50.0x），17158144 字节 → 2119 字节
--- PASS: TestGoEncoderRatioRegression (0.07s)
PASS
```

```
$ go build ./cmd/trace-calib/ ./internal/tracearchive/
（无输出）
```

## TDD 证据

- **RED**：步骤 2 先写 `defaults.go` 常量、步骤 5 写测试时，`internal/tracearchive` 包尚无实现——不适用经典的「先写测试看它失败」流程，因为本任务的主产物是**依赖 + 常量 + 实测数据**，而非业务逻辑。TDD 在本任务的真实体现是步骤 3→5 的顺序：先有标定工具（步骤 3），跑出实测数据（步骤 4），再据实测数据写断言（步骤 5）。
- **GREEN**：见上，两个测试均 PASS。

## 实测数据（U7/U8）

样本：`/datalog/emergency-archive/dt=2026-09-18` 前 8 个 part，解压后 **2078.7 MB**（2,179,635,659 字节），embedding 请求为主（最难压的真实数据）。生产机 8 核，逐点单进程运行，`VmHWM` 取峰值 RSS。

### Go 库（`klauspost/compress`，流式，与生产封块路径一致）

| 级别 | 窗口 | 产物 | 压缩比 | 耗时 | 峰值 RSS |
|---|---|---|---|---|---|
| 9 | 128 MB | 557.6 MB | 3.73x | 20.5s | 265.2 MB |
| 9 | 256 MB | 557.6 MB | 3.73x | 20.2s | 523.2 MB |
| 9 | 512 MB | 557.6 MB | 3.73x | 33.9s | 1035.6 MB |
| **19** | **128 MB** | **423.0 MB** | **4.91x** | 109.8s | 295.3 MB |
| 19 | 256 MB | 387.7 MB | 5.36x | 103.5s | 553.3 MB |
| **19** | **512 MB** | **384.0 MB** | **5.41x** | 212.9s | **1066.2 MB** |
| 22 | 128 MB | 423.0 MB | 4.91x | 146.1s | 295.2 MB |
| 22 | 512 MB | 384.0 MB | 5.41x | 239.8s | 1065.7 MB |

### C 版 zstd CLI 对照

| 级别 | 窗口参数 | 产物 | 压缩比 |
|---|---|---|---|
| 9 | `--long=29`（512 MiB） | 224.8 MB | 9.25x |
| **19** | **`--long=29`（512 MiB）** | **202.6 MB** | **10.26x** |
| 19 | `--long=27`（128 MiB） | 397.1 MB | 5.23x |
| 19 | 缺省窗口（无 `--long`） | 535.1 MB | 3.88x |

> 对照意义：CLI 的 `--long=27`（397.1 MB）与 `--long=29`（202.6 MB）说明 **C 版的长距离匹配器是收益来源**；而 CLI 缺省窗口的 535.1 MB 与 Go 库级别 9 的 557.6 MB 接近，说明两者的短距离匹配能力相当。

## 结论

### U7：**不成立**

Go 库级别 19/窗口 512 MiB = 384.0 MB，C 版 CLI 同条件 = 202.6 MB ⇒ **Go 库是 CLI 的 1.90 倍**，而步骤 4 写明的验收线是 **≤1.30 倍**。

### U8：**成立**

Go 库 512 MiB 窗口实测峰值 RSS **1066.2 MB ≤ 1200 MB 预算**。但需注意规格 §5.3.3 引用的「窗口 × 1.08」是 C 版 CLI 的数字；Go 库实为 **≈窗口 × 2.08**，接近两倍。

### 附带发现（规格未预料）

1. **`WithWindowSize` 在 Go 库上几乎无效。** 级别 9 下窗口 128→512 MiB 产物**一个字节都没变**（557.6→557.6 MB）；级别 19 只降 9.2%（423.0→384.0 MB）。同条件 C 版 CLI 降 **49.0%**。规格 §5.3.2「窗口是第一杠杆，值 -47.9%」的结论**只对 C 版 CLI 成立，对 Go 库不成立**。
2. **级别 19 与 22 在 Go 库上产物完全相同**（423.0/423.0 与 384.0/384.0），级别 22 只多耗 CPU。取 19 是对的。
3. **`EncoderLevelFromZstd` 在 Go 库上的实际档位可能少于名义档位**：级别 9/19/22 只表现出三档差异（557.6 / 384.0-423.0 / 同 19），不像 C 版有连续 22 档。

## 变更的文件

```
go.mod, go.sum, vendor/modules.txt, vendor/github.com/klauspost/compress/**
internal/tracearchive/defaults.go        （新增，含实测结论注释）
internal/tracearchive/calib_test.go      （新增）
cmd/trace-calib/main.go                  （新增）
```

## 自检发现

1. **`defaults.go` 原注释声称「Go 库侧已由本任务标定确认」**，与实测结论相反。已在提交前改写为诚实版本，逐条记录 1.90 倍缺口、窗口无效、内存 2 倍三件事。
2. **`TestGoEncoderRatioRegression` 是弱测试。** 它用合成高重复文本，实测比 8097x，下界 50x —— 这个断言无法捕捉真实压缩比的退化。属计划设计缺陷，建议由审查者裁定是否改为「小体量真实样本 + golden 比值」。

## 问题与顾虑

**`defaults.go` 的参数值建立在「用 Go 库」这一未裁决前提上。** 若改用 C 版 CLI（`--long=29`），压缩比从 5.41x 提升到 10.26x，峰值内存从 1066 MB 降到 570 MB —— 但会引入「网关容器内需要 `zstd` 二进制」这一新依赖。

这直接影响规格 §13.2（预期压缩比 9x）、§13.3（容量 29 天）与计划 4 的部署设计，**需由协作方裁决**。控制者已就该选择单独征询。

## 生产机现场

标定过程在生产机 `/datalog/calib/` 进行，**样本与进程已全部清理**，`/datalog` 剩余空间已回到 240 GB。保留了以下证据（共 213 MB）：

```
cli-19-512.zst      212,393,913 字节   ← C 版 CLI 最优产物，可用于逐字节校验未来实现
calib.log / calib2.log                  ← 全部实测原始输出
go22.log / go22-128.log / go22-512.log  ← 单点 VmHWM 采样原始输出
calib-run.sh / calib-targeted.sh / go22.sh
trace-calib                             ← 交叉编译的标定二进制（linux/amd64）
```
