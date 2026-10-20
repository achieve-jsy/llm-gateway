r"""计划 4 / 任务 5 的变异集合 D：构建契约（`Makefile`）。

主题：**`make build` 必须真的产出可在 Linux 宿主上运行的 sealer**。sealer 不进任何容器
镜像（它直接用宿主机 `/usr/bin/zstd` 与宿主机上的 `/datalog`），而开发机是 darwin ⇒
不交叉编译就会把一个 Mach-O 传上去（`exec format error`）；`CGO_ENABLED=0` 保证静态链接，
不受目标机 glibc 版本影响。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：一处**跨文件拆分**、零削弱）

计划任务 5 步骤 6 的 C 集合表格把 m04（CI 的 install 目标）写在 build 集合里，但
`TARGET` 是单值（全局约束第 42 条）⇒ m04 就地落到 `p4-task-5-ci.py`
（`TARGET = ".gitlab-ci.yml"`，与 `p4-task-4-lock.py` / `p4-task-4-runner.py` 同形）。

## 变异测不到、但有静态检查行（CHECKS）

- **`SEALER_BIN` / `VERIFY_BIN` 必须定义**：仓库原有 Makefile 只有 `GATEWAY_BIN` /
  `CP_BIN`。缺定义时 `go build -o  ./cmd/...` 会以「`-o` 后面是空串」报错 ——
  **不是**安静地成功。这是取值要求 ⇒ 静态检查（m01/m03 覆盖的是 recipe 内容，
  覆盖不到变量行本身）。
- **验收工具不得进 `build`**：`build-trace-verify` 只在本机/测试机跑，不进任何镜像、
  不参与生产上线 ⇒ **不存在性**要求（出现在 `build:` 的依赖里就是回归）⇒ 静态检查；
  行为面由 `TestMakefileBuildTargetsIncludeSealer` 承载。
"""

NAME = "p4-task-5-build"
TARGET = "Makefile"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_build_contract_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_build_contract_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：不执行 make、不编译任何二进制（变异批次有 30s 上限，编译一次就吃掉大半预算）
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 删掉 GOOS=linux ⇒ 本机（darwin）编出的 Mach-O 被传到 Linux 宿主上：exec format error
#     杀手：TestMakefileBuildsSealerForLinux
MUTATIONS["m01_不交叉编译"] = [
    ("sub", "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build",
            "CGO_ENABLED=0 GOARCH=amd64 go build"),
]
# m02 `build` 依赖里去掉 build-sealer ⇒ 走默认目标时悄悄漏掉 sealer（CI 里就是「没上线」）
#     杀手：TestMakefileBuildTargetsIncludeSealer
MUTATIONS["m02_build漏掉sealer"] = [
    ("sub", "build: build-gateway build-cp build-sealer", "build: build-gateway build-cp"),
]
# m03 编译错包（./cmd/trace-sealer → ./cmd/gateway）⇒ 目标名字还对，产物却是网关；
#     再叠加 `-o $(SEALER_BIN)` 就会把网关二进制安装成 /usr/local/bin/trace-sealer
#     杀手：TestMakefileBuildsSealerForLinux
MUTATIONS["m03_编译错包"] = [
    ("sub", "./cmd/trace-sealer", "./cmd/gateway"),
]

CHECKS = [
    {
        "name": "SEALER_BIN必须定义",
        "file": "Makefile",
        "pattern": r"(?m)^SEALER_BIN := bin/trace-sealer$",
        "expect": "present",
        "why": ("仓库原有 Makefile 只有 GATEWAY_BIN / CP_BIN。缺定义时 `go build -o  "
                "./cmd/...` 会以「`-o` 后面是空串」报错（不是安静成功）；而安装路径与 "
                "unit 的 `ExecStart=/usr/local/bin/trace-sealer` 是两条独立事实，变量行是 "
                "前者的唯一落点 ⇒ 取值要求 ⇒ 静态检查。"),
    },
    {
        "name": "VERIFY_BIN必须定义",
        "file": "Makefile",
        "pattern": r"(?m)^VERIFY_BIN := bin/trace-verify$",
        "expect": "present",
        "why": "同 SEALER_BIN：缺定义会让 `build-trace-verify` 展开成 `-o` 空串。",
    },
    {
        "name": "验收工具不进build",
        "file": "Makefile",
        "pattern": r"(?m)^build:.*build-trace-verify",
        "expect": "absent",
        "why": ("验收工具不进任何镜像、不参与生产上线 ⇒ 出现在 `build:` 的依赖里即回归。"
                "**不存在性**要求 ⇒ 静态检查；行为面由 TestMakefileBuildTargetsIncludeSealer "
                "承载。"),
    },
]

# 本集合**没有任何**允许的绿：m01–m03 全部断言红。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "`make build-sealer` 必须交叉编译到 Linux（本机 darwin）+ CGO_ENABLED=0，且产物来自 ./cmd/trace-sealer",
        "mutations": ["m01_不交叉编译", "m03_编译错包"],
        "checks": [],
        "note": "杀手 = TestMakefileBuildsSealerForLinux（同时反向断言 recipe 里没有 ./cmd/gateway）。",
    },
    {
        "plan": "`make build` 必须包含 build-sealer；SEALER_BIN / VERIFY_BIN 必须定义；验收工具不进 build",
        "mutations": ["m02_build漏掉sealer"],
        "checks": ["SEALER_BIN必须定义", "VERIFY_BIN必须定义", "验收工具不进build"],
        "note": ("杀手 = TestMakefileBuildTargetsIncludeSealer。变量行与「不进 build」是"
                 "不存在性/取值要求 ⇒ 三条静态检查。"),
    },
]
