#!/usr/bin/env python3
"""trace-mutate —— **分支级**的变异验证基础设施（通用，不专用于任何任务）。

它回答的问题是：**「这道防线被删掉，测试真的会红吗？」**，并且先自证
「这一轮的证据本身有效」——因为一个从没跑过任何用例的运行，和一次全绿的运行
在这个问题上是同一个答案（项目缺陷 D12 的形态）。

它不是 Task 6 的专用脚本：任务相关的三样东西全部由参数/集合文件提供

  --manifest <名字清单文件>     该任务**应该**被执行的顶层用例名（可从测试源重推）
  --mutation-set <集合文件>     该任务的变异集合（**必须 git-tracked**，见下）
  --packages <包...>            go test 的包列表（缺省取集合里的 PACKAGES）
  --target <被变异文件>         被变异的那个文件（缺省取集合里的 TARGET）

⇒ 分支收尾时「把每个任务的证据重跑一遍」= 每个任务一次调用、无交互。

典型调用（Task 6）：

  python3 tools/trace-mutate/trace_mutate.py \\
      --manifest .superpowers/sdd/<feature>/task-6-evidence/names.txt \\
      --mutation-set tools/trace-mutate/sets/task-6.py \\
      --repo .

## 三条判据（为什么这个工具是可信的）

1. **证据绑定提交**（严重项 ②）：pristine 树一律由 `git archive HEAD` 现取现解，
   **绝不复用任何 /tmp 旧副本**；工作树脏则拒绝运行。同时打印 `git rev-parse HEAD`、
   `git status --porcelain`、被读测试文件与被变异文件的 sha256 ⇒
   「这次跑的是哪棵树」是事实而非声明。
2. **工具/集合/清单三者出哈希**（严重项 ③ + 追加 F）：变异集合是**作者编写的产物**，
   无法从代码树重推，因此必须 git-tracked；名字清单可推导，允许留在证据目录，
   但工具会**实际执行推导命令**并与清单逐条比对，不符即判证据无效。
   「这张表是哪个集合产出的」因此可复核。
3. **子进程封闭环境**（严重项 ①）：`go test` 只拿到白名单环境变量，`GOFLAGS` 一类
   变量被剔除 —— 否则 `GOFLAGS=-exec=<wrapper>` 可以整体替换测试输出，
   把 4 条真红洗成全绿且自证串逐字同形。

## 三断言（每次运行都必须同时成立，否则「证据无效」）

  断言 1  执行到的顶层用例集合 == 名字清单（两侧相等，且 === RUN 行数 == 条数）
  断言 2  顶层 PASS + FAIL + SKIP == 清单条数
  断言 3  **全层级** SKIP == 0（含缩进的子测试行；白名单在集合里声明）

`[no tests to run]` 单独先判：**没有用例执行的运行不是「通过」，是「无效」**。

## 退出码

  0    全部变异转红（或在集合里登记了「允许的绿」理由）、静态检查通过、证据有效
  1    有证据无效 / 有未登记的绿 / 静态检查失败 / 清单推导不符 / 工作树脏
"""

import argparse
import hashlib
import importlib.util
import os
import re
import shutil
import subprocess
import sys
import tempfile

# ---------------------------------------------------------------------------
# 子进程环境：白名单（严重项 ①）
#
# 只有列在这里、且不在下面两张禁用表里的变量才会传给子进程。go test 采纳
# GOFLAGS 里的 -exec（可整体替换测试二进制），GOEXPERIMENT/GOTOOLCHAIN/GOOS/GOARCH
# 可改变构建产物，CC/CGO_* 可改变链接与实现，GOCACHE/GOMODCACHE/GOPATH/GOENV
# 可指向被篡改的缓存或把 GOFLAGS 藏在 GOENV 文件里 —— 一律不传。
#
# 【这是一个白名单，不是黑名单】新增变量必须显式加进 ENV_ALLOW，而不是
# 「默认继承、再删几个」：后者的默认值是「继承」，会随 Go 新版本悄悄变宽。
# ---------------------------------------------------------------------------
ENV_ALLOW = (
    "PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "USER", "LOGNAME", "SHELL",
)
# 禁用的具体变量名（不是前缀）：前缀 "GO" 会把 GOOGLE_* / GOLAND_* 这类与 Go
# 工具链无关的变量也算进来，打印出的「已剔除」清单会失真。真正的兜底是上面的
# 白名单 —— 没被显式批准的一律不传；这张表的价值是「文档 + 可断言的显式拒绝」。
ENV_DENY_PREFIX = ("CGO_",)
ENV_DENY_EXACT = {
    # go 工具链（-exec/-toolexec 类注入、构建目标、缓存与模块来源）
    "GO111MODULE", "GOAMD64", "GOARCH", "GOARM", "GOARM64", "GOAUTH", "GOBIN", "GOCACHE",
    "GODEBUG", "GOENV", "GOEXE", "GOEXPERIMENT", "GOFLAGS", "GOGCCFLAGS", "GOHOSTARCH",
    "GOHOSTOS", "GOINSECURE", "GOMIPS", "GOMIPS64", "GOMODCACHE", "GONOPROXY", "GONOSUMDB",
    "GONOSUMCHECK", "GOOS", "GOPATH", "GOPPC64", "GOPRIVATE", "GOPROXY", "GORISCV64", "GOROOT",
    "GOSUMDB", "GOTELEMETRY", "GOTMPDIR", "GOTOOLCHAIN", "GOVCS", "GOWASM", "GOWORK", "GO386",
    # C 工具链 / cgo
    "CC", "CXX", "CPP", "AR", "NM", "CFLAGS", "CPPFLAGS", "CXXFLAGS", "LDFLAGS", "PKG_CONFIG",
    "CGO_ENABLED",
}


def is_denied(key):
    return key.startswith(ENV_DENY_PREFIX) or key in ENV_DENY_EXACT


def build_env():
    """返回 (子进程环境, 父进程中被剔除的敏感变量名)。"""
    env = {k: v for k, v in os.environ.items() if k in ENV_ALLOW and not is_denied(k)}
    dropped = sorted(k for k in os.environ if is_denied(k))
    # 白名单 + 禁用表重叠时也必须以「禁用」为准：这里是最后一道自检。
    leaked = sorted(k for k in env if is_denied(k))
    if leaked:
        raise SystemExit("内部错误：封闭环境里仍残留 %s" % leaked)
    return env, dropped


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def sha256_text(text):
    return hashlib.sha256(text.encode()).hexdigest()


def git(repo, *args):
    return subprocess.run(["git", "-C", repo, *args], capture_output=True, text=True)


# ---------------------------------------------------------------------------
# 集合加载与变异执行
# ---------------------------------------------------------------------------

def load_set(path):
    spec = importlib.util.spec_from_file_location("trace_mutate_set", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    for attr in ("MUTATIONS", "TARGET", "PACKAGES"):
        if not hasattr(mod, attr):
            raise SystemExit("集合文件缺少 %s: %s" % (attr, path))
    if not mod.MUTATIONS:
        raise SystemExit("集合文件里没有任何变异: %s" % path)
    for attr, default in (("CHECKS", []), ("REQUIREMENTS", []), ("EXPECTED_GREEN", {}),
                          ("SKIP_WHITELIST", []), ("RETIRED", {}), ("MANIFEST_DERIVATION", None),
                          ("MANIFEST_FILES", []), ("NAME", os.path.basename(path))):
        if not hasattr(mod, attr):
            setattr(mod, attr, default)
    return mod


def resolve(setmod, arg):
    """允许用编号前缀（m02）或全名指定变异；前缀必须唯一。"""
    if arg in setmod.MUTATIONS:
        return arg
    hits = [k for k in setmod.MUTATIONS if k.startswith(arg)]
    if len(hits) == 1:
        return hits[0]
    raise SystemExit("变异名无法唯一解析: %r（候选: %s）" % (arg, hits or "无"))


def apply_mutation(name, parts, text):
    for kind, a, b in parts:
        n = text.count(a)
        if n != 1:
            raise SystemExit("PATTERN-MISS [%s]: count=%d for %r" % (name, n, a[:80]))
        if kind == "sub":
            text = text.replace(a, b)
        elif kind == "cut":
            i = text.index(a)
            j = text.index(b, i) + len(b)
            text = text[:i] + text[j:]
        else:
            raise SystemExit("未知的操作类型: %r（只支持 sub/cut）" % kind)
    return text


# ---------------------------------------------------------------------------
# 三断言
# ---------------------------------------------------------------------------

def verify(out, names, skip_whitelist):
    """合并正则三断言。返回 (ok, 自证串)。ok=False ⇒ 证据无效，绝不能算绿。

    【判定，不是打印（追加要求）】这个函数是**唯一**判据：返回值直接决定
    classify() 的判定与整轮的退出码。任何「把 SKIP/执行数打印一遍然后 exit 0」
    的实现都等于没做 —— 实测它会让「0 执行 / 全部 SKIP」两种输入都退 0。
    因此下面两条**先判**，而不是只打印：

      - 名字清单为空 ⇒ 执行数必然是 0 ⇒ 无效（项目判据「计数为 0 即为无效」）
      - no tests to run ⇒ 无效
    """
    if not names:
        return False, "证据无效：名字清单为空 ⇒ 执行数 == 0（计数为 0 即为无效）"
    if "no tests to run" in out:
        return False, "证据无效：no tests to run（-run 没匹配到任何用例）"
    ran = re.findall(r"^=== RUN   (Test[A-Za-z0-9_]+)$", out, re.M)
    ran_set = sorted(set(ran))
    want = sorted(names)
    if ran_set != want:
        return False, "证据无效：执行集合!=名字清单 缺=%s 多=%s" % (
            sorted(set(want) - set(ran_set)), sorted(set(ran_set) - set(want)))
    if len(ran) != len(want):
        return False, "证据无效：=== RUN 行数 %d != 清单条数 %d" % (len(ran), len(want))
    top_pass = len(re.findall(r"^--- PASS: Test[A-Za-z0-9_]+ ", out, re.M))
    top_fail = len(re.findall(r"^--- FAIL: Test[A-Za-z0-9_]+ ", out, re.M))
    top_skip = len(re.findall(r"^--- SKIP", out, re.M))
    if top_pass + top_fail + top_skip != len(want):
        return False, "证据无效：终判数 %d(PASS)+%d(FAIL)+%d(SKIP) != 清单条数 %d" % (
            top_pass, top_fail, top_skip, len(want))
    # 断言 3：**全层级** SKIP（重要项 ⑤）。只数顶层的 `^--- SKIP` 会漏掉
    # 缩进的 `    --- SKIP: TestX/sub` —— 真封块断言一旦挪进 t.Run，C-6 原样复发。
    all_skip = re.findall(r"^\s+--- SKIP: (\S+)", out, re.M) + re.findall(r"^--- SKIP: (\S+)", out, re.M)
    allowed = set(skip_whitelist or [])
    unlisted = sorted({s for s in all_skip if s not in allowed and s.split("/")[0] not in allowed})
    if unlisted:
        return False, "证据无效：SKIP=%d（含子测试，未在白名单内：%s）" % (
            len(all_skip), ", ".join(unlisted[:3]) + (" …" if len(unlisted) > 3 else ""))
    return True, "names=%d ran=%d diff=0 PASS=%d SKIP=%d FAIL=%d" % (
        len(want), len(ran_set), top_pass, len(all_skip), top_fail)


def classify(out, names, skip_whitelist):
    """返回 (判定, 自证串, 杀死的用例)。

    判定分层：
      - 「证据无效」只用于【拿不到判决】的运行：no tests to run（什么都没跑）。
      - 编译失败 / 挂死超时 / 测试进程崩溃中止都是**响亮的红**，不是无效。
      - 「绿」必须【完整】通过三断言：清单每一条都执行、都有终判、全层级 SKIP=0。
    """
    if "test timed out" in out:
        return "红(超时/挂死)", "—（挂死，三断言不适用）", "超时(用例未终判)"
    if "no tests to run" in out:
        return "**证据无效！**", "证据无效：no tests to run", "—"
    if "build failed" in out or "undefined:" in out:
        return "红(编译失败)", "—（未编译，无用例执行）", "(模式不合法)"
    ok, info = verify(out, names, skip_whitelist)
    fails = sorted(set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    top_fails = sorted(set(re.findall(r"^--- FAIL: (Test[A-Za-z0-9_]+) ", out, re.M)))
    if not ok:
        aborted = ("panic: " in out or "fatal error:" in out
                   or re.search(r"signal: (killed|abort|segmentation|bus)", out) is not None)
        if aborted or top_fails:
            missing = sorted(set(names) - set(re.findall(r"^=== RUN   (Test[A-Za-z0-9_]+)$", out, re.M)))
            note = "集合不完整：未执行 %d 条（%s%s）" % (
                len(missing), ", ".join(missing[:3]), " …" if len(missing) > 3 else "")
            return "红(崩溃/中止)", note, ", ".join(f.split("/")[0] for f in fails) or "(panic)"
        return "**证据无效！**", info, "—"
    if fails:
        return "红", info, ", ".join(f.split("/")[0] for f in fails)
    return "**绿（零覆盖！）**", info, "—"


# ---------------------------------------------------------------------------
# 证据绑定
# ---------------------------------------------------------------------------

def make_pristine(repo, dest, env):
    """pristine 树一律由 `git archive HEAD` 现取现解 —— 绝不复用旧副本。"""
    p1 = subprocess.Popen(["git", "-C", repo, "archive", "HEAD"],
                          stdout=subprocess.PIPE, env=env)
    p2 = subprocess.Popen(["tar", "-x", "-C", dest], stdin=p1.stdout, env=env)
    p1.stdout.close()
    rc2 = p2.wait()
    rc1 = p1.wait()
    if rc1 or rc2:
        raise SystemExit("!!! git archive HEAD | tar -x 失败（rc=%s/%s）" % (rc1, rc2))


def go_test_cmd(packages, names):
    for n in names:
        if not re.fullmatch(r"Test[A-Za-z0-9_]+", n):
            raise SystemExit("名字清单里有非法的用例名（会污染 -run 正则）: %r" % n)
    return ["go", "test"] + list(packages) + [
        "-run", "|".join(names), "-v", "-count=1", "-timeout", "30s"]


def run_tests(tree, cmd, env):
    p = subprocess.run(cmd, cwd=tree, capture_output=True, text=True, env=env, timeout=600)
    return p.stdout + p.stderr


def test_files_of(tree, packages):
    out = []
    for pkg in packages:
        rel = pkg[2:] if pkg.startswith("./") else pkg
        base = os.path.join(tree, rel.rstrip("/"))
        for dirpath, _dirs, files in os.walk(base):
            for f in sorted(files):
                if f.endswith("_test.go"):
                    full = os.path.join(dirpath, f)
                    out.append(os.path.relpath(full, tree))
    return sorted(set(out))


def print_binding(repo, head, status, env, dropped, tool_path, set_path, manifest_path,
                  manifest_names, derivation, derived_names, tree, target, test_files):
    print("## 证据绑定（这些是事实，不是声明）")
    print("git rev-parse HEAD       = %s" % head)
    print("git status --porcelain   = %s" % (repr(status.strip()) if status.strip() else "(空)"))
    print("工具自身 sha256          = %s  (%s)" % (sha256_file(tool_path), tool_path))
    print("变异集合 sha256          = %s  (%s)" % (sha256_file(set_path), set_path))
    print("名字清单 sha256          = %s  (%s)" % (sha256_file(manifest_path), manifest_path))
    print("名字清单条数             = %d" % len(manifest_names))
    print("清单推导命令             = %s" % derivation)
    if derivation:
        same = sorted(derived_names) == sorted(manifest_names)
        print("推导结果与清单比对       = %s（推导出 %d 条）" % ("一致" if same else "!!! 不一致 !!!", len(derived_names)))
        if not same:
            print("  清单多出: %s" % sorted(set(manifest_names) - set(derived_names)))
            print("  推导多出: %s" % sorted(set(derived_names) - set(manifest_names)))
    else:
        print("推导结果与清单比对       = (集合未声明 MANIFEST_DERIVATION，只能比对哈希)")
    print("pristine 来源            = git archive HEAD | tar -x  →  %s" % tree)
    print("被测文件 sha256          = %s  %s" % (sha256_file(os.path.join(tree, target)), target))
    for rel in test_files:
        print("测试文件 sha256          = %s  %s" % (sha256_file(os.path.join(tree, rel)), rel))
    print("子进程环境（白名单）     = %s" % " ".join(sorted(env)))
    print("父进程中被剔除的敏感变量 = %s" % (", ".join(dropped) if dropped else "（无）"))
    print()


def print_mapping(setmod):
    print("## 映射表：计划要求的变异 → 集合里的条目（追加 G）")
    print()
    print("| 计划要求 | 变异集合条目 | 静态检查 | 说明 |")
    print("|---|---|---|---|")
    gaps = []
    referenced = set()
    for req in setmod.REQUIREMENTS:
        muts = list(req.get("mutations") or [])
        checks = list(req.get("checks") or [])
        for m in muts:
            referenced.add(m)
        missing = [m for m in muts if m not in setmod.MUTATIONS]
        missing_chk = [c for c in checks if c not in {x["name"] for x in setmod.CHECKS}]
        if missing or missing_chk:
            gaps.append("%s → 引用了不存在的条目: %s%s" % (
                req["plan"], missing, missing_chk))
        if not muts and not checks:
            gaps.append("%s → **没有任何对应条目（GAP）**" % req["plan"])
        print("| %s | %s | %s | %s |" % (
            req["plan"],
            ", ".join("`%s`" % m for m in muts) or "—",
            ", ".join("`%s`" % c for c in checks) or "—",
            req.get("note", "")))
    print()
    orphans = [m for m in setmod.MUTATIONS if m not in referenced]
    print("计划要求总数 = %d；引用到的变异 = %d；未被任何计划要求引用的变异 = %d"
          % (len(setmod.REQUIREMENTS), len(referenced), len(orphans)))
    print("未被引用的变异（来自评审轮次裁决，不是缺口；它们同样在 git-tracked 集合里，"
          "删掉会被 git diff 看见）:")
    for m in orphans:
        print("  - %s" % m)
    if setmod.RETIRED:
        print("已退役编号（登记备查）:")
        for k in sorted(setmod.RETIRED):
            print("  - %s: %s" % (k, setmod.RETIRED[k]))
    if setmod.EXPECTED_GREEN:
        print("登记的「允许的绿」（零覆盖，附理由）:")
        for k in sorted(setmod.EXPECTED_GREEN):
            print("  - %s: %s" % (k, setmod.EXPECTED_GREEN[k]))
    print()
    if gaps:
        print("!!! 映射表存在缺口 !!!")
        for g in gaps:
            print("  - %s" % g)
        return False
    print("映射表核对：每条计划要求都有对应条目 ✓")
    return True


def print_checks(setmod, tree):
    print("## 静态检查（追加 G：变异测不到的要求必须有静态检查行）")
    print()
    ok_all = True
    for chk in setmod.CHECKS:
        path = os.path.join(tree, chk["file"])
        text = open(path, encoding="utf-8").read()
        hit = re.search(chk["pattern"], text)
        ok = (hit is None) if chk["expect"] == "absent" else (hit is not None)
        ok_all = ok_all and ok
        print("- [%s] %s" % ("PASS" if ok else "FAIL", chk["name"]))
        print("      文件=%s  正则=%s  期望=%s  实际=%s" % (
            chk["file"], chk["pattern"], chk["expect"],
            ("命中 %r" % hit.group(0)) if hit else "未命中"))
        print("      理由=%s" % chk.get("why", ""))
    print()
    return ok_all


def selftest(setmod, names):
    """自证「无效证据检测」本身有效：每一类都是**真的能骗过旧脚本**的形态。"""
    results = []

    def synth(run=(), passed=(), skipped=(), sub_skipped=()):
        lines = ["=== RUN   %s" % n for n in run]
        lines += ["--- PASS: %s (0.00s)" % n for n in passed]
        lines += ["--- SKIP: %s (0.00s)" % n for n in skipped]
        lines += ["    --- SKIP: %s (0.00s)" % s for s in sub_skipped]
        return "\n".join(lines) + "\n"

    def check(label, caught, detail, yes="✓ 已判无效", no="!!! 漏判 !!!"):
        print("  %-3s %s %s" % (label, yes if caught else no, detail))
        results.append(caught)

    print("### 自检 A：少跑一条（内嵌换行正则造成的形态）")
    out = synth(run=names[:-1], passed=names[:-1])
    ok, info = verify(out, names, setmod.SKIP_WHITELIST)
    check("A", not ok, info)

    print("### 自检 B：正则匹配不到任何用例（no tests to run）")
    ok, info = verify("testing: warning: no tests to run\nok  \tpkg\t0.005s [no tests to run]\n",
                      names, setmod.SKIP_WHITELIST)
    check("B", not ok, info)

    print("### 自检 C：%d 条全部 --- SKIP（骗法 C-6 的合成版：无 zstd 的机器）" % len(names))
    out = synth(run=names, skipped=names)
    ok, info = verify(out, names, setmod.SKIP_WHITELIST)
    check("C", not ok, info)
    v, ninfo, _ = classify(out, names, setmod.SKIP_WHITELIST)
    print("      classify() 判定 = %s（自证: %s）" % (v, ninfo))
    check("C'", "证据无效" in v and "绿" not in v, "全 SKIP 绝不能被判绿")

    print("### 自检 D：**子测试** SKIP（重要项 ⑤：只数 `^--- SKIP` 会漏判）")
    out = synth(run=names, passed=names, sub_skipped=["%s/子用例" % names[0]])
    ok, info = verify(out, names, setmod.SKIP_WHITELIST)
    check("D", not ok, info)

    print("### 自检 E：缩过/改过的 -run 正则（骗法 C-5：清单被改小）")
    ok, info = verify(synth(run=names[:2], passed=names[:2]), names, setmod.SKIP_WHITELIST)
    check("E", not ok, info)
    ok, info = verify(synth(run=names, passed=names), names + ["TestInjectedExtra"],
                      setmod.SKIP_WHITELIST)
    check("E'", not ok, info)

    print("### 自检 F：清单为空（执行数 == 0 绝不能算绿）")
    ok, info = verify("\n", [], setmod.SKIP_WHITELIST)
    check("F", not ok, info)

    print("### 自检 G：封闭环境必须剔掉 GOFLAGS 一类变量")
    saved = {k: os.environ.get(k) for k in ("GOFLAGS", "GOOS", "GOCACHE", "CC", "CGO_ENABLED")}
    os.environ["GOFLAGS"] = "-exec=/tmp/nope"
    os.environ["GOOS"] = "windows"
    os.environ["CC"] = "/tmp/fake-cc"
    try:
        env, dropped = build_env()
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
    leaked = sorted(k for k in env if is_denied(k))
    check("G", not leaked, "保留下来的敏感变量 = %s" % (leaked or "无"),
          yes="✓ 已剔除", no="!!! 封闭环境失效 !!!")
    check("G'", set(dropped) >= {"GOFLAGS", "GOOS", "CC"},
          "被剔除的敏感变量 = %s" % (", ".join(dropped) or "无"),
          yes="✓ 已剔除", no="!!! 未剔除 !!!")

    print()
    print("自检结论:", "全部被抓住 ⇒ 检测有效（三断言 + 冻结清单 + 封闭环境都在生效）"
          if all(results) else "!!! 有漏判 ⇒ 检测失效 !!!")
    print("名字清单条数 = %d" % len(names))
    return all(results)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser(
        description="分支级变异验证基础设施（通用）：逐条删守卫、跑测试、报告是否转红，并先自证证据有效。")
    ap.add_argument("--repo", default=".", help="仓库根（默认当前目录）")
    ap.add_argument("--mutation-set", required=True, help="变异集合文件（应 git-tracked）")
    ap.add_argument("--manifest", required=True, help="名字清单文件（每行一个顶层用例名）")
    ap.add_argument("--packages", nargs="*", default=None, help="go test 的包（缺省取集合里的 PACKAGES）")
    ap.add_argument("--target", default=None, help="被变异文件，相对仓库根（缺省取集合里的 TARGET）")
    ap.add_argument("--only", nargs="*", default=None, help="只跑这些变异（编号前缀或全名）")
    ap.add_argument("--skip", nargs="*", default=None, help="跳过这些变异")
    ap.add_argument("--check", action="store_true", help="只校验每条变异的匹配是否唯一定位（不跑测试）")
    ap.add_argument("--list", action="store_true", help="列出变异与静态检查，然后退出")
    ap.add_argument("--mapping", action="store_true", help="只打印「计划要求 → 集合条目」映射表")
    ap.add_argument("--selftest", action="store_true", help="自证「无效证据检测」本身有效")
    ap.add_argument("--side", action="store_true", help="对选中的变异打印**完整双侧**原始输出")
    ap.add_argument("--keep-tree", action="store_true", help="保留 pristine 临时树并打印路径（排障用）")
    args = ap.parse_args()

    env, dropped = build_env()
    head_proc = git(args.repo, "rev-parse", "--show-toplevel")
    if head_proc.returncode != 0:
        raise SystemExit("--repo 不是 git 仓库: %s" % args.repo)
    repo = head_proc.stdout.strip()
    head = git(repo, "rev-parse", "HEAD").stdout.strip()
    status = git(repo, "status", "--porcelain").stdout

    set_path = os.path.abspath(args.mutation_set)
    manifest_path = os.path.abspath(args.manifest)
    tool_path = os.path.abspath(__file__)
    setmod = load_set(set_path)
    target = args.target or setmod.TARGET
    packages = args.packages or list(setmod.PACKAGES)

    names = [n for n in open(manifest_path, encoding="utf-8").read().split() if n]
    if not names:
        raise SystemExit("名字清单为空 ⇒ 执行数会是 0，这绝不能算绿: %s" % manifest_path)

    derivation = setmod.MANIFEST_DERIVATION

    # ---- 纯诊断模式：不需要 pristine 树，也允许脏树（但它们不产出证据表）----
    if args.mapping:
        ok = print_mapping(setmod)
        print("工作树状态 = %s" % (status.strip() or "(空)"))
        sys.exit(0 if ok else 1)

    if args.selftest:
        sys.exit(0 if selftest(setmod, names) else 1)

    if args.list:
        print("集合 = %s（sha256 %s）" % (set_path, sha256_file(set_path)[:16]))
        print("目标 = %s；包 = %s；名字清单 = %d 条" % (target, " ".join(packages), len(names)))
        print()
        for name in setmod.MUTATIONS:
            print("  %s  (%d 个操作)%s" % (
                name, len(setmod.MUTATIONS[name]),
                "  [登记为允许的绿]" if name in setmod.EXPECTED_GREEN else ""))
        print()
        print("静态检查:")
        for chk in setmod.CHECKS:
            print("  - %s (%s: %s %s)" % (chk["name"], chk["file"], chk["pattern"], chk["expect"]))
        print()
        print_mapping(setmod)
        sys.exit(0)

    if args.check:
        # 用 HEAD 的内容做定位校验：不依赖工作树，也不需要解包。
        blob = git(repo, "show", "HEAD:%s" % target)
        if blob.returncode != 0:
            raise SystemExit("git show HEAD:%s 失败" % target)
        text = blob.stdout
        print("定位校验（基于 HEAD:%s，sha256=%s）" % (target, sha256_text(text)[:16]))
        for name in setmod.MUTATIONS:
            apply_mutation(name, setmod.MUTATIONS[name], text)
            print("OK  %s" % name)
        print("全部 %d 条变异的模式都在 HEAD 上唯一定位 ✓" % len(setmod.MUTATIONS))
        sys.exit(0)

    # ---- 证据模式：工作树必须干净（严重项 ②）----
    if status.strip():
        raise SystemExit(
            "!!! 工作树不干净，拒绝产出证据（严重项 ②）\n"
            "!!! git status --porcelain 输出:\n%s" % status)

    # ---- 变异选择 ----
    selected = list(setmod.MUTATIONS)
    if args.only:
        selected = [resolve(setmod, a) for a in args.only]
    if args.skip:
        drop = {resolve(setmod, a) for a in args.skip}
        selected = [m for m in selected if m not in drop]

    tree = tempfile.mkdtemp(prefix="trace-mutate-")
    try:
        make_pristine(repo, tree, env)

        derived = []
        if derivation:
            p = subprocess.run(["sh", "-c", derivation], cwd=tree,
                               capture_output=True, text=True, env=env)
            if p.returncode != 0:
                raise SystemExit("清单推导命令执行失败: %s" % p.stderr)
            derived = [x for x in p.stdout.split() if x]

        test_files = test_files_of(tree, packages)
        print_binding(repo, head, status, env, dropped, tool_path, set_path, manifest_path,
                      names, derivation, derived, tree, target, test_files)
        if derivation and sorted(derived) != sorted(names):
            raise SystemExit("!!! 名字清单与推导结果不一致 ⇒ 证据无效（清单可能被改小）")

        checks_ok = print_checks(setmod, tree)
        mapping_ok = print_mapping(setmod)
        if not checks_ok:
            raise SystemExit("!!! 静态检查失败 ⇒ 整轮失败")
        if not mapping_ok:
            raise SystemExit("!!! 映射表存在缺口 ⇒ 整轮失败")

        pristine_text = open(os.path.join(tree, target), encoding="utf-8").read()
        blob = git(repo, "show", "HEAD:%s" % target).stdout
        if pristine_text != blob:
            raise SystemExit("!!! 解包出的 %s 与 HEAD 里的内容不一致 ⇒ 证据无效" % target)

        go_cmd = go_test_cmd(packages, names)
        target_path = os.path.join(tree, target)

        if args.side:
            base = run_tests(tree, go_cmd, env)
            ok, info = verify(base, names, setmod.SKIP_WHITELIST)
            print("### 原始侧（pristine 树）完整输出 [%s] sha256=%s"
                  % (info, sha256_text(base)[:16]))
            print(base)
            bad = 0
            if not ok:
                print("!!! 原始侧证据无效 ⇒ 终止")
                raise SystemExit(1)
            for name in selected:
                mutated = apply_mutation(name, setmod.MUTATIONS[name], pristine_text)
                open(target_path, "w", encoding="utf-8").write(mutated)
                if open(target_path, encoding="utf-8").read() != mutated:
                    raise SystemExit("失败：变异未真正写入磁盘 [%s]" % name)
                try:
                    out = run_tests(tree, go_cmd, env)
                finally:
                    open(target_path, "w", encoding="utf-8").write(pristine_text)
                    if open(target_path, encoding="utf-8").read() != pristine_text:
                        raise SystemExit("失败：还原不一致 [%s]" % name)
                verdict, info, killers = classify(out, names, setmod.SKIP_WHITELIST)
                if "证据无效" in verdict:
                    bad += 1
                print("\n" + "=" * 70)
                print("### 变异侧 %s ⇒ %s [%s] 杀死: %s" % (name, verdict, info, killers))
                print("=" * 70)
                print(out)
            print("\n还原校验:", "一致" if open(target_path, encoding="utf-8").read() == pristine_text else "!!! 不一致 !!!")
            # 轻微项 ⑦：证据无效必须在退出码上体现（旧版打印「证据无效！」却 exit 0）
            if bad:
                raise SystemExit("!!! --side 下有 %d 条变异运行证据无效 ⇒ 非零退出" % bad)
            sys.exit(0)

        base = run_tests(tree, go_cmd, env)
        ok, info = verify(base, names, setmod.SKIP_WHITELIST)
        print("原始侧自证: %s %s" % ("OK" if ok else "!!! 无效 !!!", info))
        if not ok:
            print("原始侧证据无效 ⇒ 整轮失败")
            raise SystemExit(1)

        print()
        print("| 变异 | 判定 | 自证（名字/执行/diff/PASS/SKIP/FAIL） | 杀死的用例 |")
        print("|---|---|---|---|")
        buckets = {"red": [], "crash": [], "green_ok": [], "green_bad": [], "invalid": []}
        lines = []
        for name in selected:
            mutated = apply_mutation(name, setmod.MUTATIONS[name], pristine_text)
            open(target_path, "w", encoding="utf-8").write(mutated)
            if open(target_path, encoding="utf-8").read() != mutated:
                raise SystemExit("失败：变异未真正写入磁盘 [%s]" % name)
            try:
                out = run_tests(tree, go_cmd, env)
            finally:
                open(target_path, "w", encoding="utf-8").write(pristine_text)
                if open(target_path, encoding="utf-8").read() != pristine_text:
                    raise SystemExit("失败：还原不一致 [%s]" % name)
            verdict, info, killers = classify(out, names, setmod.SKIP_WHITELIST)
            if "证据无效" in verdict:
                buckets["invalid"].append(name)
            elif verdict == "红":
                buckets["red"].append(name)
            elif verdict.startswith("红"):
                buckets["crash"].append((name, verdict))
            elif name in setmod.EXPECTED_GREEN:
                buckets["green_ok"].append(name)
            else:
                buckets["green_bad"].append(name)
            line = "| %s | %s | %s | %s |" % (name, verdict, info, killers)
            lines.append(line)
            print(line)
            sys.stdout.flush()

        print()
        for name in selected:
            if name in setmod.EXPECTED_GREEN:
                print("[允许的绿] %s\n    理由: %s" % (name, setmod.EXPECTED_GREEN[name]))
        print()
        print("还原校验:", "一致" if open(target_path, encoding="utf-8").read() == pristine_text else "!!! 不一致 !!!")
        print("清单推导核对: %s" % ("一致" if (not derivation or sorted(derived) == sorted(names)) else "!!! 不一致 !!!"))
        print("静态检查: %s" % ("全部 PASS" if checks_ok else "!!! 有 FAIL !!!"))
        print("映射表: %s" % ("无缺口" if mapping_ok else "!!! 有缺口 !!!"))
        print()
        print("## 计数口径")
        print("变异总数                 = %d" % len(selected))
        # 【完整性自标识】权威表必须是**集合里的全部变异**。子集运行（--only/--skip）
        # 只该用于排障；这里让子集表自己带着醒目的标记，避免「拿一张子集表冒充权威表」
        # 这种既不需要篡改工具、也不需要篡改集合的「悄悄丢变异」。
        if len(selected) != len(setmod.MUTATIONS):
            print("!!! 本次只跑了 %d 条，而集合里有 %d 条 ⇒ **这是子集，不能作为权威表**"
                  % (len(selected), len(setmod.MUTATIONS)))
        else:
            print("集合里的变异总数          = %d（本次运行 == 全集 ✓）" % len(setmod.MUTATIONS))
        print("断言支撑的红             = %d 条（清单全部执行 + 全部终判 + SKIP=0）" % len(buckets["red"]))
        crash_names = ", ".join("%s(%s)" % (n, v) for n, v in buckets["crash"])
        print("崩溃/超时/编译红         = %d 条（**没有**「用例执行+终判」证据，"
              "但绝不可能是绿）%s" % (
                  len(buckets["crash"]), ("：" + crash_names) if crash_names else ""))
        print("允许的绿（集合里登记了理由） = %d 条" % len(buckets["green_ok"]))
        print("未登记的绿（必须为 0）     = %d 条%s" % (
            len(buckets["green_bad"]), ("：" + ", ".join(buckets["green_bad"])) if buckets["green_bad"] else ""))
        print("证据无效（必须为 0）       = %d 条%s" % (
            len(buckets["invalid"]), ("：" + ", ".join(buckets["invalid"])) if buckets["invalid"] else ""))
        print()
        print("合计: %d = %d 断言支撑 + %d 崩溃/超时/编译 + %d 允许的绿" % (
            len(selected), len(buckets["red"]), len(buckets["crash"]), len(buckets["green_ok"])))

        if buckets["invalid"] or buckets["green_bad"]:
            raise SystemExit("!!! 有 %d 条证据无效 / %d 条未登记的绿 ⇒ 整轮失败（绝不能算绿）"
                             % (len(buckets["invalid"]), len(buckets["green_bad"])))
    finally:
        if args.keep_tree:
            print("\n(pristine 树保留在 %s)" % tree)
        else:
            shutil.rmtree(tree, ignore_errors=True)


if __name__ == "__main__":
    main()
