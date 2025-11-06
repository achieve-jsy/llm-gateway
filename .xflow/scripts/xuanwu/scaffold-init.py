#!/usr/bin/env python3
"""
.xflow/scripts/xuanwu/scaffold-init.py — 玄武项目脚手架初始化脚本

用法：
    python .xflow/scripts/xuanwu/scaffold-init.py [项目根目录] [--config 配置文件路径]

示例：
    python .xflow/scripts/xuanwu/scaffold-init.py
    python .xflow/scripts/xuanwu/scaffold-init.py /path/to/project
    python .xflow/scripts/xuanwu/scaffold-init.py . --config scaffold/config.yaml

说明：
    - 不传目录参数时，默认使用当前工作目录（PWD）作为项目根目录
    - 不传 --config 时，优先查找项目根目录下的 scaffold/config.yaml，
      其次使用脚本同级目录的模板 config.yaml
"""

import os
import sys
import json
import shutil
import subprocess
import argparse
from pathlib import Path

# 确保 PyYAML 已安装
try:
    import yaml
except ImportError:
    print("[INFO] 安装 PyYAML 依赖...")
    subprocess.run(
        [sys.executable, "-m", "pip", "install", "pyyaml",
         "-i", "https://pypi.tuna.tsinghua.edu.cn/simple"],
        check=True
    )
    import yaml


# =============================================================================
# 日志工具
# =============================================================================

def log(msg):
    print(f"[INFO] {msg}")

def ok(msg):
    print(f"[OK]   ✓ {msg}")

def warn(msg):
    print(f"[WARN] ⚠ {msg}")

def err(msg):
    print(f"[ERR]  ✗ {msg}", file=sys.stderr)

def skip(msg):
    print(f"[SKIP] - {msg}")

def header(title):
    print()
    print(f"{'='*60}")
    print(f"  {title}")
    print(f"{'='*60}")


# =============================================================================
# 配置加载
# =============================================================================

def load_config(config_path: Path) -> dict:
    """加载并验证 YAML 配置文件"""
    if not config_path.exists():
        err(f"配置文件不存在：{config_path}")
        sys.exit(1)
    try:
        with open(config_path, encoding="utf-8") as f:
            cfg = yaml.safe_load(f) or {}
        return cfg
    except yaml.YAMLError as e:
        err(f"配置文件解析失败：{e}")
        sys.exit(1)


def resolve_config_path(project_root: Path, cli_config: str | None) -> Path:
    """按优先级解析配置文件路径

    优先级：
      1. --config 命令行参数（绝对路径或相对项目根目录）
      2. 项目根目录下的 scaffold/config.yaml（用户已复制并定制的版本）
      3. 脚本同级目录的 config.yaml（.xflow/scripts/xuanwu/config.yaml，默认模板）
    """
    if cli_config:
        p = Path(cli_config)
        if not p.is_absolute():
            p = project_root / p
        return p.resolve()

    # 优先：项目根目录下用户定制版
    candidate = project_root / "scaffold" / "config.yaml"
    if candidate.exists():
        return candidate

    # 次优：脚本同级目录的默认模板（.xflow/scripts/xuanwu/config.yaml）
    script_dir = Path(__file__).resolve().parent
    default_cfg = script_dir / "config.yaml"
    if default_cfg.exists():
        return default_cfg

    err("未找到配置文件，请通过 --config 参数指定，或在项目根目录创建 scaffold/config.yaml")
    sys.exit(1)


# =============================================================================
# Git 工具
# =============================================================================

def check_git() -> bool:
    try:
        subprocess.run(["git", "--version"], capture_output=True, check=True)
        return True
    except (FileNotFoundError, subprocess.CalledProcessError):
        return False


def check_git_access(url: str) -> bool:
    """快速检查仓库访问权限（超时 15s）"""
    try:
        result = subprocess.run(
            ["git", "ls-remote", "--exit-code", url, "HEAD"],
            capture_output=True, timeout=15
        )
        return result.returncode == 0
    except Exception:
        return False


def _get_submodules(target: Path) -> dict:
    """读取 .gitmodules 返回 {sub_path: sub_name} 字典；无子模块返回空字典"""
    try:
        r = subprocess.run(
            ["git", "-C", str(target), "config", "--file", ".gitmodules", "--get-regexp", r"submodule\..*\.path"],
            capture_output=True, text=True, check=True
        )
        submodules = {}
        for line in r.stdout.strip().splitlines():
            if not line:
                continue
            parts = line.split(None, 1)
            if len(parts) >= 2:
                key = parts[0]
                path = parts[1].strip()
                key_parts = key.split(".")
                name = ".".join(key_parts[1:-1]) if len(key_parts) > 2 else key_parts[1]
                submodules[path] = name
        return submodules
    except (subprocess.CalledProcessError, FileNotFoundError):
        return {}


def _ensure_remote_branch_refs_shallow(repo: Path, depth: int = 1) -> None:
    """浅仓库：同步 origin 上各分支的远端引用（仍为浅层）；切换其它分支时 Git 再按需加深/拉对象。"""
    if not repo.exists() or not (repo / ".git").exists():
        return
    try:
        r = subprocess.run(
            ["git", "-C", str(repo), "remote", "set-branches", "origin", "*"],
            capture_output=True,
            text=True,
        )
        if r.returncode != 0:
            return
        r2 = subprocess.run(
            [
                "git",
                "-C",
                str(repo),
                "fetch",
                "--depth",
                str(depth),
                "--update-shallow",
                "origin",
            ],
            capture_output=True,
            text=True,
        )
        if r2.returncode != 0 and (r2.stderr or "").strip():
            warn(f"补充远端分支引用未完全成功：{(r2.stderr or r2.stdout or '').strip()[:300]}")
    except Exception:
        pass


def _init_submodules(target: Path, submodules: dict) -> tuple[int, int, int]:
    """初始化子模块：优先单次 submodule update + submodule.fetchJobs 并行克隆。

    返回 (新克隆数, 已存在数, 失败数)。
    """
    new_sub = 0
    existing_sub = 0
    fail_sub = 0
    to_init: list[tuple[str, str]] = []
    for sub_path, sub_name in submodules.items():
        sub_target = target / sub_path
        if sub_target.exists() and (sub_target / ".git").exists():
            existing_sub += 1
            continue
        to_init.append((sub_path, sub_name))

    if not to_init:
        return new_sub, existing_sub, fail_sub

    jobs = min(8, max(1, os.cpu_count() or 4))
    paths = [p for p, _ in to_init]
    log(
        f"初始化子模块（待拉取 {len(to_init)} 个，Git 并行度 submodule.fetchJobs={jobs}）…"
    )
    cmd = [
        "git",
        "-C",
        str(target),
        "-c",
        f"submodule.fetchJobs={jobs}",
        "submodule",
        "update",
        "--init",
        "--depth",
        "1",
        "--",
        *paths,
    ]
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0 and (r.stderr or "").strip():
        warn(f"子模块批量更新未完全成功：{(r.stderr or r.stdout or '').strip()[:400]}")

    for sub_path, sub_name in to_init:
        sub_target = target / sub_path
        if sub_target.exists() and (sub_target / ".git").exists():
            continue
        log(f"重试子模块 {sub_name}（{sub_path}）…")
        r2 = subprocess.run(
            [
                "git",
                "-C",
                str(target),
                "submodule",
                "update",
                "--init",
                "--depth",
                "1",
                "--",
                sub_path,
            ],
            capture_output=True,
            text=True,
        )
        if r2.returncode != 0 and (r2.stderr or "").strip():
            warn(f"  {r2.stderr.strip()[:300]}")

    for sub_path, sub_name in to_init:
        sub_target = target / sub_path
        if sub_target.exists() and (sub_target / ".git").exists():
            new_sub += 1
            _ensure_remote_branch_refs_shallow(sub_target)
        else:
            fail_sub += 1
            skip(f"子模块 {sub_name}（{sub_path}）（权限不足或失败，跳过）")

    return new_sub, existing_sub, fail_sub


def clone_repo(git_url: str, branch: str, target: Path) -> bool:
    try:
        repo_existed = target.exists() and (target / ".git").exists()

        if not repo_existed:
            # 浅克隆 + 非单分支：本地可见 origin/* 各分支引用；切换分支时再按需加深
            cmd = [
                "git",
                "clone",
                "--depth",
                "1",
                "--no-single-branch",
                "-b",
                branch,
                git_url,
                str(target),
            ]
            result = subprocess.run(cmd, capture_output=False)
            if result.returncode != 0:
                return False
            ok("主仓库克隆完成")
            _ensure_remote_branch_refs_shallow(target)

        # 获取子模块列表
        submodules = _get_submodules(target)
        if not submodules:
            ok("无子模块")
            return True

        log(f"发现 {len(submodules)} 个子模块，准备初始化…")
        new_sub, existing_sub, fail_sub = _init_submodules(target, submodules)

        if new_sub > 0:
            ok(f"子模块克隆：{new_sub} 成功")
        if existing_sub > 0 and fail_sub == 0:
            ok(f"子模块：{existing_sub} 已存在")
        elif fail_sub > 0:
            ok(f"子模块：{fail_sub} 跳过")
        return True
    except Exception:
        return False


# =============================================================================
# Docker 工具
# =============================================================================

def check_docker() -> bool:
    try:
        subprocess.run(["docker", "--version"], capture_output=True, check=True)
        return True
    except (FileNotFoundError, subprocess.CalledProcessError):
        return False


def docker_container_running(name: str) -> bool:
    try:
        r = subprocess.run(
            ["docker", "ps", "--filter", f"name=^/{name}$",
             "--filter", "status=running", "-q"],
            capture_output=True, text=True
        )
        return bool(r.stdout.strip())
    except Exception:
        return False


def docker_container_exists(name: str) -> bool:
    try:
        r = subprocess.run(
            ["docker", "ps", "-a", "--filter", f"name=^/{name}$", "-q"],
            capture_output=True, text=True
        )
        return bool(r.stdout.strip())
    except Exception:
        return False


def docker_login(registry: str, username: str, password: str) -> bool:
    try:
        r = subprocess.run(
            ["docker", "login", registry, "-u", username, "--password-stdin"],
            input=password, text=True, capture_output=True
        )
        return r.returncode == 0
    except Exception:
        return False


def docker_pull_image(image: str) -> bool:
    try:
        log(f"拉取镜像：{image}")
        r = subprocess.run(["docker", "pull", image])
        return r.returncode == 0
    except Exception:
        return False


def docker_start_container(name: str, image: str,
                            vol_host: str, vol_container: str) -> str:
    """返回 running / restarted / created / error"""
    try:
        if docker_container_running(name):
            return "running"
        if docker_container_exists(name):
            log(f"重启已存在容器：{name}")
            subprocess.run(["docker", "start", name], check=True)
            return "restarted"
        log(f"创建并启动容器：{name}")
        subprocess.run(
            ["docker", "run", "-itd", "--name", name,
             "-v", f"{vol_host}:{vol_container}", image],
            check=True
        )
        return "created"
    except subprocess.CalledProcessError:
        return "error"


def docker_verify_mount(name: str, vol_container: str) -> bool:
    try:
        r = subprocess.run(
            ["docker", "exec", name, "ls", vol_container],
            capture_output=True
        )
        return r.returncode == 0
    except Exception:
        return False


# =============================================================================
# OpenCode 工具
# =============================================================================

def check_opencode() -> bool:
    try:
        r = subprocess.run(["opencode", "--version"], capture_output=True, text=True)
        return r.returncode == 0
    except FileNotFoundError:
        return False


def run_shell(cmd: str) -> bool:
    try:
        r = subprocess.run(cmd, shell=True)
        return r.returncode == 0
    except Exception:
        return False


def create_opencode_config(config_dir: Path, model_cfg: dict) -> bool:
    """生成 .opencode/opencode.json（已存在则跳过）"""
    try:
        config_dir.mkdir(parents=True, exist_ok=True)
        out = config_dir / "opencode.json"
        if out.exists():
            return True

        models_raw = model_cfg.get("models", {})
        enabled_models: dict = {}

        if isinstance(models_raw, dict):
            for name, val in models_raw.items():
                enabled_models[name] = val if isinstance(val, dict) else {}
        elif isinstance(models_raw, list):
            for m in models_raw:
                if m.get("enabled", False):
                    enabled_models[m["name"]] = {"id": m["name"], "name": m["name"]}

        # 支持从环境变量覆盖 apikey
        apikey = os.environ.get("OPENCODE_APIKEY") or model_cfg.get("apikey", "")

        data = {
            "$schema": "https://opencode.ai/config.json",
            "model": "example/claude-opus-4-5",
            "provider": {
                "example": {
                    "name": "BIANFENG",
                    "options": {
                        "baseURL": model_cfg.get("url", ""),
                        "apiKey": apikey,
                    },
                    "models": enabled_models,
                },
            },
        }
        with open(out, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
        return True
    except Exception as e:
        err(f"生成 opencode.json 失败：{e}")
        return False


# =============================================================================
# 主初始化器
# =============================================================================

class ScaffoldInit:
    def __init__(self, config: dict, project_root: Path, config_path: Path):
        self.cfg = config
        self.root = project_root
        self.config_path = config_path
        self.cloned: list[str] = []
        self.skipped: list[str] = []
        self.failed: list[str] = []

    # -------------------------------------------------------------------------
    # Step 1: 必需模块
    # -------------------------------------------------------------------------
    def step_required(self):
        header("步骤 1 / 4  克隆必需模块")
        required = self.cfg.get("required", {})
        if not required:
            skip("配置中无 required 模块")
            return

        for name, mcfg in required.items():
            git_url = (mcfg or {}).get("git", "").strip()
            if not git_url:
                skip(f"{name}（未配置 git）")
                self.skipped.append(name)
                continue
            self._clone_module(name, mcfg)

    # -------------------------------------------------------------------------
    # Step 2: 可选依赖模块
    # -------------------------------------------------------------------------
    def step_optional(self):
        header("步骤 2 / 4  克隆可选依赖模块")
        required = self.cfg.get("required", {})
        optional = self.cfg.get("optional", {})

        # 收集所有被依赖的模块名（仅 git 非空的 required 模块产生依赖）
        deps: set[str] = set()
        for name, mcfg in required.items():
            if (mcfg or {}).get("git", "").strip():
                for dep in (mcfg or {}).get("depends", []):
                    deps.add(dep)

        if not deps:
            skip("没有需要自动拉取的可选模块")
            return

        log(f"需要拉取的依赖：{', '.join(sorted(deps))}")
        for dep in sorted(deps):
            if dep in optional:
                self._clone_module(dep, optional[dep])
            else:
                warn(f"依赖模块 '{dep}' 未在 optional 中定义，跳过")

    # -------------------------------------------------------------------------
    # Step 3: Docker 编译环境
    # -------------------------------------------------------------------------
    def step_docker(self):
        header("步骤 3 / 4  Docker 编译环境")
        required = self.cfg.get("required", {})
        env_cfgs = self.cfg.get("env", {})

        # 找出需要 Docker 的环境名集合（只看 git 非空的模块）
        needed_envs: set[str] = set()
        for name, mcfg in required.items():
            if (mcfg or {}).get("git", "").strip():
                dep_env = (mcfg or {}).get("depend_env", "")
                if dep_env:
                    needed_envs.add(dep_env)
                    log(f"模块 '{name}' 依赖 Docker 环境：{dep_env}")

        if not needed_envs:
            skip("没有模块依赖 Docker 编译环境")
            return

        if not check_docker():
            err("Docker 未安装，请先安装 Docker")
            print()
            print("  安装指南：")
            print("    macOS:   brew install --cask docker")
            print("    Ubuntu:  sudo apt-get install docker.io")
            print("    官网：   https://www.docker.com/get-started")
            sys.exit(1)

        ok("Docker 已安装")

        for env_name in sorted(needed_envs):
            if env_name not in env_cfgs:
                warn(f"Docker 环境 '{env_name}' 未在 env 配置中定义，跳过")
                continue
            self._setup_docker_env(env_name, env_cfgs[env_name])

    # -------------------------------------------------------------------------
    # Step 4: OpenCode
    # -------------------------------------------------------------------------
    def step_opencode(self):
        header("步骤 4 / 4  配置 OpenCode")
        tools = self.cfg.get("tools", {})
        model = self.cfg.get("model", {})
        oc_cfg = (tools or {}).get("opencode", {})

        if not (oc_cfg or {}).get("enabled", False):
            skip("OpenCode 未启用（tools.opencode.enabled = false）")
            return

        install_cmd = oc_cfg.get("install_cmd", "")
        config_path = Path(self.root) / oc_cfg.get("config_path", ".opencode")

        # 检测安装状态
        if check_opencode():
            ok("OpenCode 已安装")
        elif install_cmd:
            log("OpenCode 未安装，开始安装...")
            if run_shell(install_cmd):
                ok("OpenCode 安装成功")
            else:
                warn("OpenCode 安装失败，请手动执行：")
                print(f"    {install_cmd}")
        else:
            warn("OpenCode 未安装，且未配置 install_cmd，请手动安装")

        # 生成配置文件
        apikey = os.environ.get("OPENCODE_APIKEY") or (model or {}).get("apikey", "")
        if (model or {}).get("url") and apikey:
            config_file = config_path / "opencode.json"
            if config_file.exists():
                skip(f"opencode.json 已存在：{config_file}")
            else:
                log(f"生成 OpenCode 配置：{config_file}")
                if create_opencode_config(config_path, model):
                    ok(f"配置文件已写入：{config_file}")
                else:
                    warn("配置文件生成失败")
        else:
            warn("未配置 model.url 或 apikey，跳过 opencode.json 生成")
            print("  提示：设置环境变量 OPENCODE_APIKEY 或在 config.yaml 中填写 model.apikey")

    # -------------------------------------------------------------------------
    # 内部辅助
    # -------------------------------------------------------------------------
    def _clone_module(self, name: str, mcfg: dict):
        git_url = mcfg.get("git", "").strip()
        branch  = mcfg.get("branch", "main").strip() or "main"
        target  = self.root / mcfg["target"]

        repo_exists = target.exists() and (target / ".git").exists()

        # 主仓库已存在，检查子模块
        if repo_exists:
            submodules = _get_submodules(target)
            if submodules:
                log(f"{name}（主仓库已存在，检查子模块...）")
                new_sub, existing_sub, fail_sub = _init_submodules(target, submodules)
                if new_sub > 0:
                    ok(f"子模块新克隆：{new_sub} 成功")
                if existing_sub > 0 and new_sub == 0 and fail_sub == 0:
                    ok(f"所有子模块均已存在")
                if fail_sub > 0:
                    ok(f"子模块：{existing_sub} 已存在，{fail_sub} 失败")
            else:
                ok(f"{name}（主仓库已存在，无子模块）")
            self.skipped.append(name)
            return

        # 主仓库不存在但不是 git 仓库
        if target.exists():
            warn(f"{name} 目录已存在但不是 git 仓库：{target}")
            self.failed.append(name)
            return

        # 权限检查
        log(f"检查仓库权限：{git_url}")
        if not check_git_access(git_url):
            warn(f"{name} 无法访问仓库（权限不足或网络异常），跳过：{git_url}")
            self.failed.append(name)
            return

        # 克隆
        log(f"克隆 {name}: {git_url}  分支: {branch}")
        if clone_repo(git_url, branch, target):
            ok(f"{name} 克隆完成 → {target}")
            self.cloned.append(name)
        else:
            warn(f"{name} 克隆失败，跳过：{git_url}")
            self.failed.append(name)

    def _setup_docker_env(self, env_name: str, cfg: dict):
        image    = cfg.get("image", "")
        registry = cfg.get("registry", "")
        username = cfg.get("username", "")
        password = cfg.get("password", "")
        container = cfg.get("container", env_name)
        vol_host  = str(self.root)
        vol_cont  = cfg.get("volume", "/home")

        log(f"初始化 Docker 环境：{env_name}")

        # 登录镜像仓库
        if registry and username and password:
            log(f"登录镜像仓库：{registry}")
            if docker_login(registry, username, password):
                ok("镜像仓库登录成功")
            else:
                warn("镜像仓库登录失败，继续尝试拉取镜像...")

        # 拉取镜像
        if image:
            if not docker_pull_image(image):
                err(f"镜像拉取失败：{image}")
                sys.exit(1)

        # 启动容器
        state = docker_start_container(container, image, vol_host, vol_cont)
        if state == "running":
            ok(f"容器 {container} 已在运行")
        elif state == "restarted":
            ok(f"容器 {container} 已重启")
        elif state == "created":
            ok(f"容器 {container} 已创建并启动")
        else:
            err(f"容器启动失败：{container}")
            sys.exit(1)

        # 验证挂载
        if docker_verify_mount(container, vol_cont):
            ok(f"挂载验证通过：{vol_host} → {vol_cont}")
        else:
            warn("挂载验证失败，请检查容器挂载配置")

    # -------------------------------------------------------------------------
    # 打印摘要
    # -------------------------------------------------------------------------
    def print_summary(self):
        print()
        print("=" * 60)
        print("  [SUMMARY] 初始化完成")
        print("=" * 60)
        print(f"  项目根目录：{self.root}")
        print()

        print(f"  新克隆模块（{len(self.cloned)}）：")
        for name in self.cloned:
            print(f"    + {name}")
        if not self.cloned:
            print("    （无）")

        print(f"  已存在跳过（{len(self.skipped)}）：")
        for name in self.skipped:
            print(f"    ✓ {name}")
        if not self.skipped:
            print("    （无）")

        if self.failed:
            print(f"  失败跳过（{len(self.failed)}）：")
            for name in self.failed:
                print(f"    ✗ {name}")
            print()
            print("  ⚠ 部分模块初始化失败（权限不足或网络异常），已跳过")

        print("=" * 60)

    # -------------------------------------------------------------------------
    # 入口
    # -------------------------------------------------------------------------
    def run(self):
        log(f"项目根目录：{self.root}")
        log(f"配置文件：  {self.config_path}")

        if not check_git():
            err("Git 未安装，请先安装 Git")
            sys.exit(1)

        self.step_required()
        self.step_optional()
        self.step_docker()
        self.step_opencode()
        self.print_summary()


# =============================================================================
# CLI 入口
# =============================================================================

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="玄武项目脚手架初始化",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python scaffold-init.py
  python scaffold-init.py /path/to/project
  python scaffold-init.py . --config scaffold/config.yaml
        """
    )
    parser.add_argument(
        "project_dir", nargs="?", default=None,
        help="项目根目录（默认：当前工作目录）"
    )
    parser.add_argument(
        "--config", default=None,
        help="指定配置文件路径（默认：自动查找 scaffold/config.yaml）"
    )
    return parser.parse_args()


def main():
    args = parse_args()

    # 解析项目根目录
    if args.project_dir:
        project_root = Path(args.project_dir).resolve()
    else:
        project_root = Path.cwd()

    if not project_root.exists():
        err(f"项目目录不存在：{project_root}")
        sys.exit(1)

    # 解析配置文件
    config_path = resolve_config_path(project_root, args.config)
    log(f"使用配置文件：{config_path}")

    config = load_config(config_path)

    # 执行初始化
    initializer = ScaffoldInit(config, project_root, config_path)
    initializer.run()


if __name__ == "__main__":
    main()
