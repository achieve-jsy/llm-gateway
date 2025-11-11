#!/bin/bash
# LLM Gateway OpenCode 配置助手 - 安装脚本
# 无需 Go 环境，一键安装 macOS URL Scheme Handler
#
# 用法:
#   curl -fsSL <raw-url>/register-opencode-helper.sh | bash
#   或
#   bash register-opencode-helper.sh

set -euo pipefail

APP_NAME="LLMGatewayOpencodeHelper"
URL_SCHEME="llm-gateway-opencode"
APP_DIR="$HOME/Applications/$APP_NAME.app"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== LLM Gateway OpenCode 配置助手安装 ===${NC}"
echo ""

# 创建 .app 目录结构
echo "正在创建 App Bundle..."
mkdir -p "$APP_DIR/Contents/MacOS"
mkdir -p "$APP_DIR/Contents/Resources"

# 写入 handler 脚本
cat > "$APP_DIR/Contents/MacOS/handler" << 'HANDLER_EOF'
#!/bin/bash
set -euo pipefail

URL="${1:-}"
if [ -z "$URL" ]; then exit 0; fi

mkdir -p "$HOME/.config/opencode"

RAW_BASE64=$(echo "$URL" | sed -n 's/.*[?&]base64=\([^&]*\).*/\1/p')
if [ -z "$RAW_BASE64" ]; then exit 0; fi

PAYLOAD=$(python3 -c "
import sys, urllib.parse, base64
raw = sys.argv[1]
decoded = urllib.parse.unquote(raw)
padding = 4 - len(decoded) % 4
if padding != 4:
    decoded += '=' * padding
print(base64.b64decode(decoded).decode('utf-8'))
" "$RAW_BASE64" 2>/dev/null) || exit 0

CALLBACK_URL=$(python3 -c "import json,sys; p=json.loads(sys.stdin.read()); print(p.get('_callback',{}).get('url',''))" <<< "$PAYLOAD" 2>/dev/null || echo "")
CALLBACK_TOKEN=$(python3 -c "import json,sys; p=json.loads(sys.stdin.read()); print(p.get('_callback',{}).get('token',''))" <<< "$PAYLOAD" 2>/dev/null || echo "")

send_callback() {
    local status="$1" msg="${2:-}"
    if [ -n "$CALLBACK_URL" ] && [ -n "$CALLBACK_TOKEN" ]; then
        curl -s -X POST "$CALLBACK_URL" \
            -H "Content-Type: application/json" \
            -d "{\"token\":\"$CALLBACK_TOKEN\",\"status\":\"$status\",\"msg\":\"$msg\"}" \
            --max-time 5 >/dev/null 2>&1 || true
    fi
}

python3 -c "
import json, os, sys
new_config = json.loads(sys.stdin.read())
config_path = os.path.expanduser('~/.config/opencode/opencode.json')
os.makedirs(os.path.dirname(config_path), exist_ok=True)
if os.path.exists(config_path):
    with open(config_path) as f:
        try: cfg = json.load(f)
        except: cfg = {}
else:
    cfg = {}
if 'provider' in new_config:
    cfg.setdefault('provider', {}).update(new_config['provider'])
if 'model' in new_config: cfg['model'] = new_config['model']
if 'small_model' in new_config: cfg['small_model'] = new_config['small_model']
with open(config_path, 'w') as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write('\n')
" <<< "$PAYLOAD" 2>/dev/null && {
    send_callback "success"
    osascript -e "display notification \"OpenCode 配置已更新\" with title \"LLM Gateway Helper\"" 2>/dev/null || true
} || {
    send_callback "failed" "配置写入失败"
}
HANDLER_EOF

chmod +x "$APP_DIR/Contents/MacOS/handler"

# 写入 Info.plist
cat > "$APP_DIR/Contents/Info.plist" << PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.llm-gateway.opencode-helper</string>
	<key>CFBundleName</key>
	<string>LLM Gateway OpenCode Helper</string>
	<key>CFBundleVersion</key>
	<string>1.0</string>
	<key>CFBundleExecutable</key>
	<string>handler</string>
	<key>CFBundleURLTypes</key>
	<array>
		<dict>
			<key>CFBundleURLName</key>
			<string>LLM Gateway OpenCode Config URL</string>
			<key>CFBundleURLSchemes</key>
			<array>
				<string>$URL_SCHEME</string>
			</array>
		</dict>
	</array>
</dict>
</plist>
PLIST_EOF

# 注册 App 到 Launch Services
echo "正在注册到 Launch Services..."
LSREGISTER="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

if [ -f "$LSREGISTER" ]; then
    "$LSREGISTER" -f "$APP_DIR" 2>/dev/null
else
    open "$APP_DIR" 2>/dev/null || true
fi

echo ""
echo -e "${GREEN}✅ 安装完成！${NC}"
echo ""
echo -e "现在可以通过浏览器点击 ${YELLOW}${URL_SCHEME}://${NC} 链接来配置 OpenCode。"
echo ""
echo -e "卸载方法:"
echo -e "  rm -rf \"$APP_DIR\""
echo -e "  ${LSREGISTER} -u \"$APP_DIR\" 2>/dev/null"