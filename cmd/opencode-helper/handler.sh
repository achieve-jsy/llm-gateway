#!/bin/bash
# LLM Gateway OpenCode 配置 Handler
# 通过 URL Scheme 接收配置参数，写入 opencode 配置文件，并回调服务端

set -euo pipefail

URL="${1:-}"
if [ -z "$URL" ]; then
    echo "错误: 未提供 URL" >&2
    exit 1
fi

CONFIG_FILE="$HOME/.config/opencode/opencode.json"
mkdir -p "$(dirname "$CONFIG_FILE")"

# ── 1. 提取并解码 base64 payload ──────────────────────────────────────
RAW_BASE64=$(echo "$URL" | sed -n 's/.*[?&]base64=\([^&]*\).*/\1/p')
if [ -z "$RAW_BASE64" ]; then
    echo "错误: URL 中未找到 base64 参数" >&2
    exit 1
fi

PAYLOAD=$(python3 -c "
import sys, urllib.parse, base64
raw = sys.argv[1]
decoded = urllib.parse.unquote(raw)
padding = 4 - len(decoded) % 4
if padding != 4:
    decoded += '=' * padding
print(base64.b64decode(decoded).decode('utf-8'))
" "$RAW_BASE64") || {
    echo "错误: base64 解码失败" >&2
    exit 1
}

# ── 2. 解析 callback 信息（可选） ──────────────────────────────────────
CALLBACK_URL=$(python3 -c "
import json, sys
p = json.loads(sys.stdin.read())
cb = p.get('_callback', {})
print(cb.get('url', ''))
" <<< "$PAYLOAD" 2>/dev/null || echo "")

CALLBACK_TOKEN=$(python3 -c "
import json, sys
p = json.loads(sys.stdin.read())
cb = p.get('_callback', {})
print(cb.get('token', ''))
" <<< "$PAYLOAD" 2>/dev/null || echo "")

# ── 辅助函数：发送回调 ──────────────────────────────────────────────────
send_callback() {
    local status="$1"
    local msg="${2:-}"
    if [ -n "$CALLBACK_URL" ] && [ -n "$CALLBACK_TOKEN" ]; then
        curl -s -X POST "$CALLBACK_URL" \
            -H "Content-Type: application/json" \
            -d "{\"token\":\"$CALLBACK_TOKEN\",\"status\":\"$status\",\"msg\":\"$msg\"}" \
            --max-time 5 >/dev/null 2>&1 || true
    fi
}

# ── 3. 检查是否是 ping 探测（只回调 installed，不写文件）──────────────────
IS_PING=$(python3 -c "
import json, sys
p = json.loads(sys.stdin.read())
print('1' if p.get('_ping') else '0')
" <<< "$PAYLOAD" 2>/dev/null || echo "0")

if [ "$IS_PING" = "1" ]; then
    send_callback "installed"
    exit 0
fi

# ── 4. 写入 opencode 配置 ──────────────────────────────────────────────
python3 -c "
import json, os, sys

new_config = json.loads(sys.stdin.read())
config_path = os.path.expanduser('~/.config/opencode/opencode.json')
os.makedirs(os.path.dirname(config_path), exist_ok=True)

# 读取已有配置
if os.path.exists(config_path):
    with open(config_path) as f:
        try:
            cfg = json.load(f)
        except json.JSONDecodeError:
            cfg = {}
else:
    cfg = {}

# 合并 provider（保留已有其他 provider）
if 'provider' in new_config:
    cfg.setdefault('provider', {}).update(new_config['provider'])

# 设置默认模型（如果 payload 中有）
if 'model' in new_config:
    cfg['model'] = new_config['model']
if 'small_model' in new_config:
    cfg['small_model'] = new_config['small_model']

with open(config_path, 'w') as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write('\n')

print(f'配置已写入: {config_path}')
" <<< "$PAYLOAD" || {
    send_callback "failed" "配置文件写入失败"
    osascript -e "display notification \"配置写入失败\" with title \"LLM Gateway\"" 2>/dev/null || true
    exit 1
}

# ── 4. 回调服务端 ──────────────────────────────────────────────────────
send_callback "success"

# ── 5. 系统通知（macOS） ───────────────────────────────────────────────
osascript -e "display notification \"OpenCode 配置已更新\" with title \"LLM Gateway Helper\"" 2>/dev/null || true

echo "✅ OpenCode 配置完成！请重启 opencode 以应用新配置。"
