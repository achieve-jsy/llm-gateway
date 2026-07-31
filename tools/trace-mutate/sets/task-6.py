"""task-6 的变异集合 —— **这是作者编写的产物，因此必须 git-tracked**。

为什么它必须进版本库（追加 F 的裁决）：与「名字清单」不同，「哪些守卫本该被删」
**无法从代码树里重推**。集合若留在 gitignored 的证据目录，作者可以悄悄删掉一个
变异（例如承载「实际多于声明」覆盖的那条），而 `git diff` 什么都看不到。
名字清单是可推导的，因此允许留在证据目录 —— 前提是调用方打印推导命令与哈希。

集合里每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

编号一经发布就不再改动（历史报告表可直接对照），因此有两个**已退役**编号，
在此显式登记，读者不需要问「22 去哪了」。
"""

NAME = "task-6"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/sealer.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）
PACKAGES = ["./internal/tracearchive/", "./tests/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = [
    "tests/tracearchive/sealer_test.go",
    "internal/tracearchive/sealer_internal_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracearchive/sealer_test.go "
    "internal/tracearchive/sealer_internal_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 未装 zstd 时那 4 条真封块用例会 SKIP，必须判无效
# （CI/本机必须装 zstd；见 tests/tracearchive/helpers_test.go:38-39 的警告）。
SKIP_WHITELIST = []

# 已退役编号（见上方说明）
RETIRED = {
    "m22": "默认值常量由 defaultSealInterval 改为 defaultSealMinAge 后，旧变异与新 m35 是同一个守卫，重复项删除，由 m35 取代。",
    "m44": "裁决 4 直接删除了记忆化分支（字段 s.versionOK），无分支可删。",
}

# 单一事实源：本任务的全部变异
MUTATIONS = {}

# ---- 约束 1：必须走外部 C 版 zstd CLI ----
MUTATIONS["m01_不调用配置的二进制"] = [
    ("sub", 'exec.CommandContext(ctx, s.opts.ZstdBinary, args...)',
            'exec.CommandContext(ctx, "true", args...)'),
]
MUTATIONS["m02_退回Go库编码器"] = [
    ("sub", '\t"bytes"\n', ''),
    # 不能动 "os/exec"：版本探测仍要用它（这正是本次新增守卫带来的约束）
    ("sub", '\t"os/exec"\n', '\t"os/exec"\n\tgzstd "github.com/klauspost/compress/zstd"\n'),
    ("sub", '\tlong, err := zstdLongArg(s.opts.ZstdWindowBytes)\n\tif err != nil {',
            '\tif _, err := zstdLongArg(s.opts.ZstdWindowBytes); err != nil {'),
    ("cut", '\targs := []string{"-" + strconv.Itoa(s.opts.ZstdLevel)',
            'ErrSealFailed, args, err, strings.TrimSpace(stderr.String()))\n\t}'),
    ("sub", '\t// 此刻 .tmp 的内容即纯 zstd 帧（trailer/footer 尚未追加），据此计算 frame_sha256',
            '\tenc, err := gzstd.NewWriter(dst,\n'
            '\t\tgzstd.WithEncoderLevel(gzstd.EncoderLevelFromZstd(s.opts.ZstdLevel)),\n'
            '\t\tgzstd.WithWindowSize(s.opts.ZstdWindowBytes),\n'
            '\t\tgzstd.WithEncoderConcurrency(1))\n'
            '\tif err != nil {\n'
            '\t\treturn empty, fmt.Errorf("%w: 建编码器失败: %w", ErrSealFailed, err)\n'
            '\t}\n'
            '\tif _, err := io.Copy(enc, src); err != nil {\n'
            '\t\t_ = enc.Close()\n'
            '\t\treturn empty, fmt.Errorf("%w: 编码失败: %w", ErrSealFailed, err)\n'
            '\t}\n'
            '\tif err := enc.Close(); err != nil {\n'
            '\t\treturn empty, fmt.Errorf("%w: 编码收尾失败: %w", ErrSealFailed, err)\n'
            '\t}\n'
            '\t// 此刻 .tmp 的内容即编码结果（trailer/footer 尚未追加），据此计算 frame_sha256'),
]

# ---- I4：拒绝覆盖 ----
MUTATIONS["m03_删除产物存在检查"] = [
    ("cut", '\tif _, err := os.Stat(partPath); err == nil {',
            '\t\treturn fmt.Errorf("tracearchive: 检查产物失败: %w", err)\n\t}'),
]
MUTATIONS["m04_删除无法判断分支"] = [
    ("sub", '\t} else if !os.IsNotExist(err) {\n'
            '\t\treturn fmt.Errorf("tracearchive: 检查产物失败: %w", err)\n\t}\n',
            '\t}\n'),
]
MUTATIONS["m05_ErrPartExists改v"] = [
    ("sub", 'fmt.Errorf("%w: %s", ErrPartExists, partPath)',
            'fmt.Errorf("%v: %s", ErrPartExists, partPath)'),
]

# ---- 行数交叉校验 ----
MUTATIONS["m06_删除Rows等于0的豁免"] = [
    ("sub", 'if task.Rows > 0 && len(offsets) != task.Rows {',
            'if len(offsets) != task.Rows {'),
]
MUTATIONS["m07_放宽为只拦多于"] = [
    ("sub", 'if task.Rows > 0 && len(offsets) != task.Rows {',
            'if task.Rows > 0 && len(offsets) > task.Rows {'),
]
MUTATIONS["m08_ErrRowCountMismatch改v"] = [
    ("sub", 'fmt.Errorf("%w: 扫描=%d 调用方=%d（拒绝封块，保留 spool）",',
            'fmt.Errorf("%v: 扫描=%d 调用方=%d（拒绝封块，保留 spool）",'),
]

# ---- 残行守卫 ----
MUTATIONS["m09_删除残行守卫"] = [
    ("cut", '\t\t// ReadBytes 在 EOF 前找不到',
            'ErrSealFailed, len(line), path)\n\t\t}'),
]

# ---- 失败清理与传播 ----
MUTATIONS["m10_删除zstd失败时的tmp清理"] = [
    ("cut", '\t\t// 子进程非零退出 / 无法启动 / 压缩 I/O 失败：',
            '\t\tos.Remove(tmpPath)\n'),
]
MUTATIONS["m11_删除建目录"] = [
    ("cut", '\t// 【落地修正 1】',
            '\t\treturn fmt.Errorf("tracearchive: 建产物目录失败: %w", err)\n\t}\n'),
]
MUTATIONS["m12_ErrSealFailed改v"] = [
    ("sub", 'return empty, fmt.Errorf("%w: zstd 子进程失败',
            'return empty, fmt.Errorf("%v: zstd 子进程失败'),
]
MUTATIONS["m13_删除ErrNotFound归一化"] = [
    # 归一化现在只在版本探测一处（runZstd 的重复分支已删）：删掉它 + errors 导入
    ("sub", '\t\tif errors.Is(err, os.ErrNotExist) {\n'
            '\t\t\treturn fmt.Errorf("%w: 探测 zstd 版本失败，找不到可执行文件 %q（部署事故，规格 §11.1）: %w: %w",\n'
            '\t\t\t\tErrSealFailed, s.opts.ZstdBinary, exec.ErrNotFound, err)\n'
            '\t\t}\n', ''),
    ("sub", '\t"errors"\n', ''),
]
MUTATIONS["m14_删除failBeforeRename钩子"] = [
    ("cut", '\tif s.failBeforeRename != nil {',
            '\t\t\treturn err\n\t\t}\n\t}\n'),
]
MUTATIONS["m15_删除打开spool失败守卫"] = [
    ("sub", '\tsrc, err := os.Open(spoolPath)\n\tif err != nil {\n'
            '\t\treturn empty, fmt.Errorf("%w: 打开 spool 失败: %w", ErrSealFailed, err)\n\t}\n',
            '\tsrc, _ := os.Open(spoolPath)\n'),
]
MUTATIONS["m16_删除读spool失败守卫"] = [
    ("sub", '\t\tif readErr != nil {\n'
            '\t\t\treturn nil, 0, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)\n\t\t}\n',
            ''),
]
MUTATIONS["m17_trailer先于帧"] = [
    ("sub", '\ttmpPath := partPath + ".tmp"\n\tframeSHA, err := s.runZstd(ctx, task.SpoolPath, tmpPath)\n'
            '\tif err != nil {\n'
            '\t\t// 子进程非零退出 / 无法启动 / 压缩 I/O 失败：\n'
            '\t\t// 清理 .tmp、保留 spool、不产生产物（规格 §9.2）。\n'
            '\t\tos.Remove(tmpPath)\n\t\treturn err\n\t}\n'
            '\n'
            '\t// ③ 帧已就绪，此刻追加 trailer 与 footer（CLI 只写了帧）\n'
            '\tif err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {\n'
            '\t\tos.Remove(tmpPath)\n\t\treturn err\n\t}\n',
            '\ttmpPath := partPath + ".tmp"\n'
            '\tvar frameSHA [8]byte\n'
            '\t// 变异：把 trailer/footer 写到帧【之前】。CLI 以 O_TRUNC 打开 .tmp，\n'
            '\t// 于是索引被它冲掉 —— 产物只剩一个裸帧。\n'
            '\tif err := os.WriteFile(tmpPath, nil, 0o644); err != nil {\n'
            '\t\treturn err\n\t}\n'
            '\tif err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {\n'
            '\t\tos.Remove(tmpPath)\n\t\treturn err\n\t}\n'
            '\tif _, err := s.runZstd(ctx, task.SpoolPath, tmpPath); err != nil {\n'
            '\t\tos.Remove(tmpPath)\n\t\treturn err\n\t}\n'),
]

# ---- NewSealer 构造期守卫 ----
MUTATIONS["m18_删除Root为空守卫"] = [
    ("cut", '\tif opts.Root == "" {', '\t\treturn nil, fmt.Errorf("tracearchive: Root 不能为空")\n\t}\n'),
]
MUTATIONS["m19_删除ZstdBinary默认值"] = [
    ("cut", '\tif opts.ZstdBinary == "" {', '\t\topts.ZstdBinary = "zstd"\n\t}\n'),
]
MUTATIONS["m20_删除ZstdLevel默认值"] = [
    ("cut", '\tif opts.ZstdLevel <= 0 {', '\t\topts.ZstdLevel = defaultZstdLevel\n\t}\n'),
]
MUTATIONS["m21_删除ZstdWindowBytes默认值"] = [
    ("cut", '\tif opts.ZstdWindowBytes <= 0 {', '\t\topts.ZstdWindowBytes = defaultZstdWindowBytes\n\t}\n'),
]
MUTATIONS["m23_删除窗口2的幂校验"] = [
    ("sub", 'if windowBytes <= 0 || windowBytes&(windowBytes-1) != 0 {',
            'if windowBytes <= 0 {'),
]

# ---- 上下文取消 ----
MUTATIONS["m24_删除ctx取消分支"] = [
    ("cut", '\t\tif ctxErr := ctx.Err(); ctxErr != nil {',
            'ErrSealFailed, ctxErr)\n\t\t}\n'),
]

# ---- ListPendingSeals ----
MUTATIONS["m25_删除根目录不存在分支"] = [
    ("cut", '\t\tif os.IsNotExist(err) {', '\t\t\treturn nil, nil // 还没写过任何正文\n\t\t}\n'),
]
MUTATIONS["m26_删除根目录读失败上抛"] = [
    ("sub", '\t\treturn nil, fmt.Errorf("tracearchive: 读 spool 根目录失败: %w", err)\n', ''),
]
MUTATIONS["m27_删除dt前缀过滤"] = [
    ("sub", 'if !dtEnt.IsDir() || !strings.HasPrefix(dtEnt.Name(), "dt=") {',
            'if !dtEnt.IsDir() || false {'),
]
MUTATIONS["m28_删除dt的IsDir过滤"] = [
    ("sub", 'if !dtEnt.IsDir() || !strings.HasPrefix(dtEnt.Name(), "dt=") {',
            'if !strings.HasPrefix(dtEnt.Name(), "dt=") {'),
]
MUTATIONS["m29_删除inst前缀过滤"] = [
    ("sub", 'if !instEnt.IsDir() || !strings.HasPrefix(instEnt.Name(), "inst=") {',
            'if !instEnt.IsDir() || false {'),
]
MUTATIONS["m30_删除inst的IsDir过滤"] = [
    ("sub", 'if !instEnt.IsDir() || !strings.HasPrefix(instEnt.Name(), "inst=") {',
            'if !strings.HasPrefix(instEnt.Name(), "inst=") {'),
]
MUTATIONS["m31_删除jsonl后缀过滤"] = [
    ("sub", '\t\t\t\tif !strings.HasSuffix(e.Name(), ".jsonl") {\n\t\t\t\t\tcontinue\n\t\t\t\t}\n', ''),
]
MUTATIONS["m32_删除parsePartSeq过滤"] = [
    ("sub", '\t\t\t\tpart, ok := parsePartSeq(e.Name())\n'
            '\t\t\t\tif !ok {\n\t\t\t\t\tcontinue\n\t\t\t\t}\n',
            '\t\t\t\tpart, _ := parsePartSeq(e.Name())\n'),
]
MUTATIONS["m33_删除mtime门槛"] = [
    ("sub", 'if info.ModTime().After(cutoff) {', 'if false && info.ModTime().After(cutoff) {'),
]
MUTATIONS["m34_删除sort顺序"] = [
    ("sub", '\t"sort"\n', ''),
    ("cut", '\t// 稳定顺序（dt/inst/part），便于测试断言与日志排查\n\tsort.Slice(out, func(i, j int) bool {',
            '\t\treturn out[i].Part < out[j].Part\n\t})\n'),
]


# ---- 评审裁决 1/3a/3b 新增守卫 ----
MUTATIONS["m35_删除minAge默认值"] = [
    ("sub", '\tif opts.MinAge <= 0 {\n\t\topts.MinAge = defaultSealMinAge\n\t}\n', ''),
]
MUTATIONS["m36_minAge默认值退回15分钟"] = [
    ("sub", 'opts.MinAge = defaultSealMinAge', 'opts.MinAge = defaultSealInterval'),
]
MUTATIONS["m37_删除版本探测调用"] = [
    ("sub", '\tif err := s.verifyZstdVersion(); err != nil {\n\t\treturn err\n\t}\n', ''),
]
MUTATIONS["m38_版本比对恒通过"] = [
    ("sub", 'if got != s.opts.ExpectedZstdVersion {', 'if false {'),
]
MUTATIONS["m39_无法解析时放行"] = [
    ("sub", '\tif !ok {\n\t\treturn fmt.Errorf("%w: 无法解析 zstd 版本输出 %q',
            '\tif !ok {\n\t\tgot = s.opts.ExpectedZstdVersion\n\t\t_ = fmt.Errorf("%w: 无法解析 zstd 版本输出 %q'),
]
MUTATIONS["m40_期望版本写死默认值"] = [
    ("sub", 'if got != s.opts.ExpectedZstdVersion {', 'if got != defaultZstdVersion {'),
]
MUTATIONS["m41_忽略探测失败"] = [
    ("sub", '\tout, err := exec.Command(s.opts.ZstdBinary, "--version").Output()\n\tif err != nil {',
            '\tout, err := exec.Command(s.opts.ZstdBinary, "--version").Output()\n\tif err != nil && false {'),
]
MUTATIONS["m42_删除dt目录名校验"] = [
    ("sub", '\t\tif err := validateDT(dt); err != nil {\n\t\t\tcontinue\n\t\t}\n', ''),
]
MUTATIONS["m43_删除inst目录名校验"] = [
    ("sub", '\t\t\tif inst != instBlue && inst != instGreen {\n\t\t\t\tcontinue\n\t\t\t}\n', ''),
]
# m44 已删除：评审裁决 1 直接移除了记忆化分支（s.versionOK），该分支不存在，
# 因此没有「删除它」这种变异；对应的并发隐患也随字段一起消失。
MUTATIONS["m45_解析放宽为不校验三段"] = [
    # 放过 "1.5" 这类两段输出：会被当成版本号 1.5 送去比对，于是错误从
    # 「无法解析」变成「版本不符」——TestSealRejectsUnparsableZstdVersion
    # 断言了错误文案，因此本变异转红（裁决 2 要求的「可被杀死」）。
    ("sub", '\tparts := strings.Split(v, ".")\n\tif len(parts) != 3 {\n\t\treturn "", false\n\t}',
            '\tparts := strings.Split(v, ".")\n\tif len(parts) == 0 {\n\t\treturn "", false\n\t}'),
]
MUTATIONS["m46_删除版本号数字校验"] = [
    # 放过 "1.5.7-beta" / "1.x.7"：同样把「无法解析」变成「版本不符」，文案断言转红。
    ("sub", '\tfor _, p := range parts {\n\t\tif p == "" {\n\t\t\treturn "", false\n\t\t}\n'
            '\t\tfor k := 0; k < len(p); k++ {\n\t\t\tif p[k] < \'0\' || p[k] > \'9\' {\n'
            '\t\t\t\treturn "", false\n\t\t\t}\n\t\t}\n\t}\n', '\t_ = parts\n'),
]
MUTATIONS["m47_版本不符信息省略期望值"] = [
    # 错误信息只报实际版本、不报期望版本：运维无法判断「该退回去还是该重新标定」。
    ("sub", '"%w: zstd 版本不符：期望 %s，实际 %s。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开",\n'
            '\t\t\tErrSealFailed, s.opts.ExpectedZstdVersion, got)',
            '"%w: zstd 版本不符：实际 %s。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开",\n'
            '\t\t\tErrSealFailed, got)'),
]

# ---- 评审第 ⑤ 条：SpoolPath 必须与 Ref 对应 ----
MUTATIONS["m48_删除SpoolPath与Ref一致性校验"] = [
    ("sub", '\twantSpool := s.paths.SpoolFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})\n'
            '\tif filepath.Clean(task.SpoolPath) != wantSpool {\n'
            '\t\treturn fmt.Errorf("%w: task.SpoolPath=%q 与 Ref{%s, %s, %d} 对应的 %q 不一致（拒绝封块：产物会以 Ref 寻址却装进另一文件的内容）",\n'
            '\t\t\tErrSealFailed, task.SpoolPath, task.DT, task.Inst, task.Part, wantSpool)\n'
            '\t}\n', ''),
]

# ---- 复审「轻微 3」：与规范路径等价但拼写不同的 SpoolPath 必须被接受 ----
MUTATIONS["m49_删除路径Clean"] = [
    ("sub", 'if filepath.Clean(task.SpoolPath) != wantSpool {',
            'if task.SpoolPath != wantSpool {'),
]

# ---- 修复轮 3 新增：简报 :962「忽略 cmd.Run() 的 error」----
# 映射表（--mapping）暴露出这是唯一一条「计划明确要求、集合里却没有对应条目」的要求，
# 见本文件末尾的 REQUIREMENTS。此变异由既有用例杀死。
MUTATIONS["m50_忽略cmd.Run错误"] = [
    ("sub", '\tif err := cmd.Run(); err != nil {',
            '\tif err := cmd.Run(); err != nil && false {'),
]

# ---- 修复轮 3 新增：让 ⑥ 的修法**可被重跑**，而不是只写在报告里 ----
# 复审者自设计的 x1：把行数交叉校验的 != 放宽为 <（只拦「少于声明」）。
# 修复前的 fixture 是「声明 3、实际 2」⇒ 2 < 3 仍成立 ⇒ 该变异**判绿**（零覆盖）。
# 修复后新增「声明 1、实际 2」的用例 ⇒ 2 < 1 不成立 ⇒ 守卫被绕过 ⇒ 转红。
MUTATIONS["m51_行数校验放宽为只拦少于"] = [
    ("sub", 'if task.Rows > 0 && len(offsets) != task.Rows {',
            'if task.Rows > 0 && len(offsets) < task.Rows {'),
]

# ---- 修复轮 3 新增：④ 的两次「搬家」各自入集合（让「两次独立验证」可复核）----
# 旧用例把 (a)(b) 塞在一起，于是 relocA 与 relocB 都红在同一行，(a) 的判别断言
# 从未被执行 —— 「两次搬家验证」不是两次独立验证。拆成两条用例后：
#   relocA ⇒ (a) 与 (b) 都红（两个用例各死在自己的判别断言上）
#   relocB ⇒ **只有 (b) 红**，(a) 绿 ⇒ 这是两次**独立**验证的直接证据
_GUARD_BLOCK = (
    '\twantSpool := s.paths.SpoolFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})\n'
    '\tif filepath.Clean(task.SpoolPath) != wantSpool {\n'
    '\t\treturn fmt.Errorf("%w: task.SpoolPath=%q 与 Ref{%s, %s, %d} 对应的 %q 不一致（拒绝封块：产物会以 Ref 寻址却装进另一文件的内容）",\n'
    '\t\t\tErrSealFailed, task.SpoolPath, task.DT, task.Inst, task.Part, wantSpool)\n'
    '\t}\n'
)
_VERIFY_BLOCK = '\tif err := s.verifyZstdVersion(); err != nil {\n\t\treturn err\n\t}\n'
_PARTEXISTS_BLOCK = ('\t} else if !os.IsNotExist(err) {\n'
                     '\t\treturn fmt.Errorf("tracearchive: 检查产物失败: %w", err)\n\t}\n')

MUTATIONS["m52_守卫搬到版本探测之后"] = [
    ("cut", '\twantSpool := s.paths.SpoolFile(',
            '\t\t\tErrSealFailed, task.SpoolPath, task.DT, task.Inst, task.Part, wantSpool)\n\t}\n'),
    ("sub", _VERIFY_BLOCK, _VERIFY_BLOCK + "\n" + _GUARD_BLOCK),
]
MUTATIONS["m53_守卫搬到产物存在检查之后"] = [
    ("cut", '\twantSpool := s.paths.SpoolFile(',
            '\t\t\tErrSealFailed, task.SpoolPath, task.DT, task.Inst, task.Part, wantSpool)\n\t}\n'),
    ("sub", _PARTEXISTS_BLOCK, _PARTEXISTS_BLOCK + "\n" + _GUARD_BLOCK),
]

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目。每条都必须带**分类标签**并写明依据；
# 未登记的绿 ⇒ 整轮失败。分类必须显式，因为「绿」有两种性质完全不同的来源，
# 混为一谈会污染这份集合（它就是信任根）：
#
#   [零覆盖]   某行为**存在**但**没有用例约束**它 ⇒ **测试缺陷**，必须有保留理由
#   [等价变异] 该行代码在框架/标准库**契约**下**冗余** ⇒ **关于代码本身的发现**，
#              不是测试缺陷（「杀不死」的原因不是没测，而是无可观测差异）
#
# 这条机制存在的意义：两种绿都不能靠报告里的散文解释，必须在集合里显式登记并分类，
# 第三方执行同一份集合时会得到同一份「允许的绿」清单与同一份分类。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {
    "m15_删除打开spool失败守卫": (
        "[零覆盖] 测试不可达的纵深防御：把 os.Open 的 error 丢掉后，空句柄在读时返回 ErrInvalid，"
        "而 TestSealFailsWhenSpoolMissing 在 scanSpoolOffsets 的打开阶段就以"
        " os.ErrNotExist 失败 —— 早于 runZstd 的这次打开。"
        "守卫只在「扫描与打开之间 spool 被删」的 TOCTOU 窗口内可达，"
        "现有用例无法构造该窗口。sealer.go:392-399 已声明它「测试不可达」并说明保留理由。"
    ),
    "m34_删除sort顺序": (
        "[等价变异] 【依据：标准库契约，可逐字复核】os.ReadDir 的文档写着"
        "「ReadDir reads the named directory, returning all its directory entries sorted by "
        "filename.」（$GOROOT/src/os/dir.go:109-110），其实现就是 "
        "slices.SortFunc(dirs, func(a,b DirEntry) int { return bytealg.CompareString(a.Name(), b.Name()) })"
        "（同文件 :114-126）⇒ 本函数三层嵌套读目录（dt → inst → part-NNNNNN.jsonl）**拿到的顺序"
        "已经**是 sort.Slice 的比较键序：dt=/inst= 按文件名即字典序，part 名由 ref.go:83-84 强制"
        "「恰好 6 位」⇒ 零填充 ⇒ 文件名字典序 == 数值序。因此显式 sort.Slice **冗余**，"
        "删掉它不存在可观测差异。"
        "【不是覆盖缺口】「杀不死」的原因不是没测，而是**无可观测差异** —— "
        "这是**关于代码本身的发现**，与 m15 那种「行为存在但无用例约束」的测试缺陷不同类。"
        "登记为**等价变异**；保留显式排序是【有意把顺序从 ReadDir 的实现细节升格为本函数的"
        "契约】的选择（建议保留）。待决见本文件末尾 PENDING。"
    ),
}

# ---------------------------------------------------------------------------
# 待决项（交 R11 / 终审）—— **本轮不扩大范围**，只登记。
#
# 放在**被跟踪的集合**里而不是只写在 gitignored 的报告里：这是关于代码本身的发现，
# 删掉它会留下 git diff 痕迹。
# ---------------------------------------------------------------------------
PENDING = [
    {
        "id": "finding-1-显式排序在-os.ReadDir-契约下冗余",
        "title": "(*Sealer).ListPendingSeals 末尾的 sort.Slice 在 os.ReadDir 契约下是冗余的",
        "evidence": (
            "os.ReadDir 的文档与实现都保证按文件名排序（$GOROOT/src/os/dir.go:109-126）；"
            "part 名被 ref.go:83-84 强制为恰好 6 位 ⇒ 文件名字典序 == 数值序 ⇒ "
            "三层 os.ReadDir 的拼接顺序已经等于 sort.Slice 的比较键序。"
            "实测旁证：变异 m34（删掉 sort.Slice + \"sort\" import）**不转红**。"
        ),
        "decision": "保留显式排序（有意显式化不变量）；但它既然是冗余的，**一个断言返回顺序的用例会让它不再冗余**。",
        "owner": "R11 / 终审",
        "this_round": "不补用例（控制者裁决：本轮不扩大范围）",
    },
]

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# 每项在 pristine 树上执行，expect 取 "absent"（不许出现）或 "present"（必须出现）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "sealer-不得使用-Go-库编码器",
        "file": "internal/tracearchive/sealer.go",
        "pattern": r"klauspost/compress/zstd|zstd\.NewWriter",
        "expect": "absent",
        "why": (
            "简报 task-6-brief.md:961：把 exec.CommandContext 换回 zstd.NewWriter 时，"
            "压缩比类断言**不会红**（本任务不测比值），因此计划自己就写明「必须另有一条"
            "人工检查」。这里把它做成可执行的静态检查行，而不是靠人工 grep。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划写的每条变异要求 → 集合/静态检查里的对应条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有任何 requirement 引用」的变异。
# 「悄悄丢掉一个变异」在这里会表现为一行 GAP。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-6-brief.md:961 不得把 exec.CommandContext 换回 zstd.NewWriter（压缩比断言抓不到）",
        "mutations": [],
        "checks": ["sealer-不得使用-Go-库编码器"],
        "note": "计划自己要求「另有一条人工检查」⇒ 本工具把它落地为静态检查行。",
    },
    {
        "plan": "task-6-brief.md:962 忽略 cmd.Run() 的 error（当成成功）",
        "mutations": ["m50_忽略cmd.Run错误"],
        "checks": [],
        "note": "映射表首次生成时这条**没有**对应条目（集合缺口）；本轮补 m50 关闭。",
    },
    {
        "plan": "task-6-brief.md:963 把 %w 改成 %v（ErrSealFailed / ErrPartExists / ErrRowCountMismatch）",
        "mutations": ["m05_ErrPartExists改v", "m08_ErrRowCountMismatch改v", "m12_ErrSealFailed改v"],
        "checks": [],
        "note": "三条哨兵各一条；errors.Is/As 断言必须转红。",
    },
    {
        "plan": "task-6-brief.md:964 颠倒「先跑 CLI 再写 trailer/footer」的顺序",
        "mutations": ["m17_trailer先于帧"],
        "checks": [],
        "note": "把 trailer/footer 写到帧之前 ⇒ 被 CLI 的 O_TRUNC 冲掉。",
    },
    {
        "plan": "task-6-brief.md:965 删掉 info.ModTime().After(cutoff) 判断",
        "mutations": ["m33_删除mtime门槛"],
        "checks": [],
        "note": "TestListPendingSealsSkipsFreshFiles 必须转红。",
    },
    {
        "plan": "task-6-brief.md:966 去掉 .jsonl 后缀过滤",
        "mutations": ["m31_删除jsonl后缀过滤"],
        "checks": [],
        "note": "TestListPendingSealsIgnoresIllegalNames 必须转红。",
    },
    {
        "plan": "task-6-fix3-items.md 重要 ⑥ 行数交叉校验缺「实际多于声明」方向（复审自设计变异 x1）",
        "mutations": ["m51_行数校验放宽为只拦少于"],
        "checks": [],
        "note": (
            "修复前该变异**判绿**（fixture 是「声明 3、实际 2」，2 < 3 仍成立）；"
            "新增「声明 1、实际 2」的用例后转红 ⇒ 缺口已闭合且可复核。"
        ),
    },
    {
        "plan": "task-6-fix3-items.md 重要 ④ 把 (a)/(b) 做成两次**独立**的搬家验证",
        "mutations": ["m52_守卫搬到版本探测之后", "m53_守卫搬到产物存在检查之后"],
        "checks": [],
        "note": (
            "relocA ⇒ (a)(b) 两条用例各死在自己的判别断言；relocB ⇒ **只有 (b) 红、(a) 绿**。"
            "「两次搬家验证不是两次独立验证」由此可复核（旧用例两处都红在同一行）。"
        ),
    },
]
