#!/usr/bin/env bash
# scaffold.sh — 玄武项目脚手架入口脚本 (macOS/Linux)
# 用法：./scaffold.sh [项目目录] [--config 配置文件路径]
# 未带 --config 时：交互选择（1=广州玄武-活动 2=杭州玄武-活动），再传入对应 YAML。
# Note: the default xuanwu config clones xwRef together with client.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON_SCRIPT="$SCRIPT_DIR/../scripts/xuanwu/scaffold-init.py"

if [ ! -f "$PYTHON_SCRIPT" ]; then
    echo "[ERROR] Python 脚本不存在：$PYTHON_SCRIPT"
    exit 1
fi

find_python() {
    for cmd in "python3.11" "python3.10" "python3.9" "python3" "python"; do
        if command -v "$cmd" &> /dev/null; then
            echo "$cmd"
            return 0
        fi
    done
    return 1
}

PYTHON_CMD=$(find_python)

if [ -z "$PYTHON_CMD" ]; then
    echo "============================================================"
    echo " [ERROR] 未找到 Python 环境"
    echo "============================================================"
    echo ""
    echo "  请先安装 Python 3.9+"
    echo ""
    echo "  安装方式："
    echo "    macOS:"
    echo "      brew install python@3.11"
    echo "      或访问 https://www.python.org/downloads/"
    echo ""
    echo "    Ubuntu/Debian:"
    echo "      sudo apt-get install python3 python3-pip"
    echo ""
    echo "    使用 pyenv (推荐):"
    echo "      brew install pyenv"
    echo "      pyenv install 3.11.0"
    echo "      pyenv global 3.11.0"
    echo ""
    echo "============================================================"
    exit 1
fi

echo "[INFO] 使用 Python: $PYTHON_CMD"

args_have_config() {
    local a
    for a in "$@"; do
        if [[ "$a" == --config || "$a" == --config=* ]]; then
            return 0
        fi
    done
    return 1
}

PROFILE_CFG=""

select_xw_profile() {
    local cfg_dir XW_SEL
    cfg_dir="$(cd "$SCRIPT_DIR/../scripts/xuanwu" && pwd)"
    echo ""
    echo "============================================================"
    echo "  请选择玄武-活动区域配置"
    echo "============================================================"
    echo "  1. 广州玄武-活动 (默认)"
    echo "  2. 杭州玄武-活动"
    echo "============================================================"
    read -r -p "请输入编号 [1|2]，回车默认 1: " XW_SEL || true
    XW_SEL=${XW_SEL:-1}
    if [[ "$XW_SEL" == "2" ]]; then
        PROFILE_CFG="$cfg_dir/config-hangzhou-activity.yaml"
    else
        XW_SEL=1
        PROFILE_CFG="$cfg_dir/config.yaml"
    fi
    if [ ! -f "$PROFILE_CFG" ]; then
        echo "[ERROR] 配置文件不存在: $PROFILE_CFG"
        exit 1
    fi
    echo "[INFO] 已选: $XW_SEL — 使用配置 $PROFILE_CFG"
}

if args_have_config "$@"; then
    exec "$PYTHON_CMD" "$PYTHON_SCRIPT" "$@"
fi

select_xw_profile
exec "$PYTHON_CMD" "$PYTHON_SCRIPT" "$@" --config "$PROFILE_CFG"
