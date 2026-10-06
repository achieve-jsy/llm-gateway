r"""计划 4 / 任务 5 的变异集合 E：CI 分发契约（`.gitlab-ci.yml`）。

主题：**上线作业要装 sealer，但**不许**顺手启用它**。sealer 是宿主机常驻进程（不进任何
容器镜像），因此它必须在「真的会把二进制放到目标主机」的那个作业里被编译并安装到
`/usr/local/bin/trace-sealer`（unit 的 `ExecStart` 就是这个绝对路径），并把 unit 文件同步
过去 + `daemon-reload`；而 `systemctl enable/start` 属于**人工门**（读侧就绪 D73 + 挂载
在场），CI 不得代劳 —— 否则一个「还没被允许上线」的进程会在每台部署机上自己起来。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 落点选择（**执行者按计划的 grep 规则自行判定**，如实登记）

计划任务 5 步骤 4 要求：先 `grep -n "CGO_ENABLED=0 go build" .gitlab-ci.yml` 看清有哪几处，
**只改「真的会把二进制放到目标主机」的那一处**。实测三处：

| 位置 | 作业 | 产物去向 | 是不是「放到目标主机」 |
|---|---|---|---|
| `:99` | `build-backend-test` | `.backend-dist/` → Docker 镜像 | ❌（进的是**容器镜像**，而 sealer 必须在宿主机上跑） |
| `:196` | `build-backend-prod` | `.backend-dist/` → Docker 镜像 | ❌（同上） |
| `:479-480` | `deploy-app-test-fast` | `scp` 到 `${TEST_HOST}` + `docker cp` 进容器 | ✅（唯一一处把**宿主机二进制**放到目标主机） |

⇒ 本集合的 `TARGET` 是 `.gitlab-ci.yml`，变异只打在 `deploy-app-test-fast` 里追加的那几行。
**跨文件拆分**：计划表格的 m04 就地落在这里（`TARGET` 是单值，全局约束第 42 条）。

## 变异测不到、但有静态检查行（CHECKS）

- **CI 不得 `enable/start/restart` trace-sealer**：这是**不存在性**要求（写进去就是回归），
  变异无法证明「没写」⇒ 静态检查；行为面由 `TestCICIDoesNotEnableSealerUnit` 承载
  （m02 就是它的断言红杀手，所以这条要求的两个面都在册）。

## 如实登记的缺口（COVERAGE_GAPS）

计划步骤 4 的括号里写着「生产是发布作业里的同类步骤」。按上面的 grep 规则实测：
**生产侧不存在这样的步骤** —— `deploy-prod` 用 `${REGISTRY}/deploy-tools:latest` 镜像
（无 Go 工具链），它的分发动作是 `docker pull` 两个**容器镜像**，整个作业里没有
`CGO_ENABLED=0 go build`。因此「生产作业里追加 sealer 的编译与安装」这条**在现结构下
没有落点**（不是「本平台不可达」的开脱：落点是可指认的、只是不存在）。本集合
**不**在 `deploy-prod` 里塞一条编不过的 `go build`；生产机上的 sealer 就位属于
**人工上线序列**（`deploy/systemd/*` + 运行手册的硬门），见 `COVERAGE_GAPS`。
"""

NAME = "p4-task-5-ci"
TARGET = ".gitlab-ci.yml"
PACKAGES = ["./tests/deploy/"]
MANIFEST_FILES = ["tests/deploy/trace_build_contract_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/deploy/trace_build_contract_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯文本断言：CI 文件只读，绝不触发流水线
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m04a 安装到 /tmp（而不是 /usr/local/bin/trace-sealer）⇒ unit 的 ExecStart 指向一个
#       不存在的文件：部署「成功」，服务启动即失败（而且失败面在下一次 systemctl start）
#       杀手：TestCICIBuildsAndInstallsSealer
MUTATIONS["m04a_装到tmp"] = [
    ("sub", "install -m 0755 /tmp/trace-sealer.new /usr/local/bin/trace-sealer",
            "install -m 0755 /tmp/trace-sealer.new /tmp/trace-sealer"),
]
# m04b CI 顺手启用 unit ⇒ 绕过人工门（读侧 D73 未就绪 + 挂载未核对就会被拉起）
#       杀手：TestCICIDoesNotEnableSealerUnit
MUTATIONS["m04b_CI启用unit"] = [
    ("sub", '      ssh ${SSH_OPTS} ${TEST_USER}@${TEST_HOST} "systemctl daemon-reload"',
            '      ssh ${SSH_OPTS} ${TEST_USER}@${TEST_HOST} "systemctl daemon-reload"\n'
            '      ssh ${SSH_OPTS} ${TEST_USER}@${TEST_HOST} "systemctl enable --now trace-sealer"'),
]

CHECKS = [
    {
        "name": "CI不得enable-sealer",
        "file": ".gitlab-ci.yml",
        "pattern": r"systemctl (enable|start|restart)[^\n]*trace-sealer",
        "expect": "absent",
        "why": ("启用 sealer 是**人工门**（读侧就绪 D73 + 挂载在场）；CI 只安装 + "
                "daemon-reload。这是**不存在性**要求 ⇒ 只能静态检查（变异无法证明「没写」）；"
                "行为面由 TestCICIDoesNotEnableSealerUnit 承载（m04b 是它的断言红杀手）。"),
    },
]

# 本集合**没有任何**允许的绿：m04a/m04b 全部断言红。
EXPECTED_GREEN = {}

COVERAGE_GAPS = [
    {
        "id": "gap-1-生产侧无CGO_ENABLED=0-go-build落点",
        "title": ("计划步骤 4 说的「生产是发布作业里的同类步骤」实测**不存在**："
                  "`deploy-prod` 用 deploy-tools 镜像（无 Go 工具链）、分发动作是 "
                  "`docker pull` 容器镜像"),
        "where": [
            ".gitlab-ci.yml  deploy-prod（第 592 行起；`image: ${REGISTRY}/deploy-tools:latest`）",
            "该作业体内 `docker pull ${REGISTRY}/${GATEWAY_IMAGE}:${IMAGE_TAG}` 与 "
            "${CONTROL_PLANE_IMAGE}，没有任何 `go build`",
            "计划任务 5 步骤 4 的括号句：「生产是发布作业里的同类步骤」",
        ],
        "reachable": (
            "**落点不存在，但这不是「平台不可达」**：三处 `CGO_ENABLED=0 go build` 里"
            "（`:99` `build-backend-test`、`:196` `build-backend-prod`、`:479-480` "
            "`deploy-app-test-fast`），只有最后一处把宿主机二进制放到目标主机；前两处把产物"
            "打进 **Docker 镜像**，而 sealer 明确**必须**在宿主机上跑（不进容器、无挂载）。"
            "生产侧的 sealer 就位因此在人工上线序列里（`deploy/systemd/*` + 运行手册硬门），"
            "不在 `deploy-prod` 的脚本里。"
        ),
        "no_killer": (
            "**没有变异杀手，因为没有可变异的目标**：`deploy-prod` 里不存在 `go build` 行，"
            "本集合的 `MUTATIONS` 无法「删掉一处不存在的编译」。这不是缺口被掩盖，而是"
            "缺口本身（生产侧无自动分发）被**如实登记**。"
        ),
        "falsifiability": (
            "**判据可复现**：`grep -n 'CGO_ENABLED=0 go build' .gitlab-ci.yml` 的三行位置"
            "与上面的表逐字一致；`deploy-prod` 体内 `grep -c 'go build'` = 0。"
        ),
        "owner": (
            "本轮**只登记**（改 `deploy-prod` 需要给该作业加 Go 工具链或 `artifacts` 传递，"
            "超出任务 5 的 +14 行预算与「只改真的会把二进制放到目标主机的那一处」的裁定）。"
            "后续若要求生产自动分发，应给 `build-backend-prod` 加一个 sealer artifact 并让 "
            "`deploy-prod` 在 ssh 块里 scp，届时本集合补一条断言红。"
        ),
    },
]

REQUIREMENTS = [
    {
        "plan": "CI 的上线作业必须编译 `./cmd/trace-sealer` 并 `install -m 0755 … /usr/local/bin/trace-sealer`（unit 的 ExecStart 就是这个绝对路径）",
        "mutations": ["m04a_装到tmp"],
        "checks": [],
        "note": "杀手 = TestCICIBuildsAndInstallsSealer（同时断言 unit 文件同步与 daemon-reload）。",
    },
    {
        "plan": "CI **不得** enable/start/restart trace-sealer（启用是人工门：读侧就绪 + 挂载在场）",
        "mutations": ["m04b_CI启用unit"],
        "checks": ["CI不得enable-sealer"],
        "note": ("杀手 = TestCICIDoesNotEnableSealerUnit；不存在性面由同名静态检查承载"
                 "（两者互补，不存在只靠静态检查的缺口）。"),
    },
]
