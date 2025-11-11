package handler

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

//go:embed opencode_windows.ps1.tmpl
var windowsTmplRaw string

var windowsTmpl = template.Must(template.New("win").Parse(windowsTmplRaw))
var macTmpl = template.Must(template.New("mac").Parse(macTmplRaw))

const (
	opencodeTokenTTL    = 10 * time.Minute
	opencodeTokenPrefix = "opencode_cfg_token:"
)

type OpencodeConfigHandler struct {
	rdb *redis.Client
}

func NewOpencodeConfigHandler(rdb *redis.Client) *OpencodeConfigHandler {
	return &OpencodeConfigHandler{rdb: rdb}
}

type tokenRecord struct {
	KeyID       string `json:"key_id"`
	ProviderID  string `json:"provider_id"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Status      string `json:"status"` // pending | success | failed
	Msg         string `json:"msg,omitempty"`
}

// GenerateToken POST /api/v1/keys/:id/opencode-config/token  (JWT required)
// 前端在展示对话框「确认执行」后调用，生成一次性 token
func (h *OpencodeConfigHandler) GenerateToken(c *gin.Context) {
	keyID := c.Param("id")

	var req struct {
		ProviderID string `json:"provider_id"` // 如 "llm-gateway"
		BaseURL    string `json:"base_url" binding:"required"`
		APIKey     string `json:"api_key"  binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.ProviderID == "" {
		req.ProviderID = "llm-gateway"
	}

	token := uuid.New().String()
	rec := tokenRecord{
		KeyID:      keyID,
		ProviderID: req.ProviderID,
		BaseURL:    req.BaseURL,
		APIKey:     req.APIKey,
		Status:     "pending",
	}
	data, _ := json.Marshal(rec)

	ctx := context.Background()
	if err := h.rdb.Set(ctx, opencodeTokenPrefix+token, data, opencodeTokenTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成 token 失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"token":       token,
			"ttl_seconds": int(opencodeTokenTTL.Seconds()),
		},
	})
}

// GetScript GET /api/v1/opencode-config/script?token=xxx&os=mac|windows
// 无需 JWT，返回动态生成的脚本内容（纯文本）
func (h *OpencodeConfigHandler) GetScript(c *gin.Context) {
	token := c.Query("token")
	osType := c.Query("os") // "mac" | "windows"
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "token required"})
		return
	}

	ctx := context.Background()
	raw, err := h.rdb.Get(ctx, opencodeTokenPrefix+token).Bytes()
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "token 无效或已过期"})
		return
	}

	var rec tokenRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "解析 token 失败"})
		return
	}

	// 回调地址：使用请求的 Host，支持反代
	scheme := "https"
	if c.Request.TLS == nil && c.GetHeader("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	host := c.Request.Host
	if fwdHost := c.GetHeader("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	callbackURL := fmt.Sprintf("%s://%s/api/v1/opencode-config/callback", scheme, host)

	params := scriptParams{
		Token:       token,
		ProviderID:  rec.ProviderID,
		BaseURL:     rec.BaseURL,
		APIKey:      rec.APIKey,
		CallbackURL: callbackURL,
	}

	var buf bytes.Buffer
	switch osType {
	case "windows":
		if err := windowsTmpl.Execute(&buf, params); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成脚本失败"})
			return
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
	default:
		if err := macTmpl.Execute(&buf, params); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成脚本失败"})
			return
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
	}

	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(buf.Bytes())
}

// Callback POST /api/v1/opencode-config/callback  (无需 JWT，token 即凭证)
func (h *OpencodeConfigHandler) Callback(c *gin.Context) {
	var req struct {
		Token  string `json:"token"  binding:"required"`
		Status string `json:"status" binding:"required"` // success | failed
		Msg    string `json:"msg"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	ctx := context.Background()
	key := opencodeTokenPrefix + req.Token
	raw, err := h.rdb.Get(ctx, key).Bytes()
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "token 无效或已过期"})
		return
	}

	var rec tokenRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "解析失败"})
		return
	}

	ttl, _ := h.rdb.TTL(ctx, key).Result()
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	rec.Status = req.Status
	rec.Msg = req.Msg
	data, _ := json.Marshal(rec)
	h.rdb.Set(ctx, key, data, ttl)

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// GetStatus GET /api/v1/keys/:id/opencode-config/status?token=xxx  (JWT required)
func (h *OpencodeConfigHandler) GetStatus(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "token required"})
		return
	}

	ctx := context.Background()
	raw, err := h.rdb.Get(ctx, opencodeTokenPrefix+token).Bytes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"status": "pending"}})
		return
	}

	var rec tokenRecord
	_ = json.Unmarshal(raw, &rec)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"status": rec.Status, "msg": rec.Msg},
	})
}

type scriptParams struct {
	Token       string
	ProviderID  string
	BaseURL     string
	APIKey      string
	CallbackURL string
}

// macTmplRaw macOS/Linux bash 脚本模板
const macTmplRaw = `#!/bin/bash
# LLM Gateway — OpenCode 配置脚本（自动生成，请勿修改）

set -euo pipefail

TOKEN="{{.Token}}"
PROVIDER_ID="{{.ProviderID}}"
BASE_URL="{{.BaseURL}}"
API_KEY="{{.APIKey}}"
CALLBACK_URL="{{.CallbackURL}}"
CONFIG_FILE="$HOME/.config/opencode/opencode.json"
DISCOVERY_PLUGIN="opencode-models-discovery"

# ── 颜色 & 输出函数 ───────────────────────────────────────────────────────────
GREEN='\033[0;32m'; CYAN='\033[0;36m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[✓]${NC} $1"; }
step()  { echo -e "${CYAN}[..]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
err()   { echo -e "${RED}[✗]${NC} $1"; }
divider() { echo -e "${CYAN}────────────────────────────────────────${NC}"; }

send_callback() {
  curl -s -X POST "$CALLBACK_URL" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$TOKEN\",\"status\":\"$1\",\"msg\":\"${2:-}\"}" \
    --max-time 8 >/dev/null 2>&1 || true
}

divider
echo -e "  LLM Gateway — OpenCode 配置助手"
divider
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 1：环境检验
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 1/4  检验本地环境..."

MISSING=""

if ! command -v python3 &>/dev/null; then
  MISSING="${MISSING}\n  - python3（未找到，请先安装）"
fi
if ! command -v curl &>/dev/null; then
  MISSING="${MISSING}\n  - curl（未找到，请先安装）"
fi
if ! command -v npm &>/dev/null && ! command -v node &>/dev/null; then
  warn "npm/node 未找到，将跳过插件自动安装（模型列表需手动配置）"
  SKIP_PLUGIN=1
else
  SKIP_PLUGIN=0
fi

if [ -n "$MISSING" ]; then
  err "缺少必要依赖，无法继续："
  echo -e "$MISSING"
  send_callback "failed" "缺少依赖: python3 或 curl"
  exit 1
fi

CONFIG_DIR="$(dirname "$CONFIG_FILE")"
if ! mkdir -p "$CONFIG_DIR" 2>/dev/null; then
  err "无法创建配置目录: $CONFIG_DIR（请检查权限）"
  send_callback "failed" "无法创建配置目录"
  exit 1
fi

info "环境检验通过（python3 ✓  curl ✓  配置目录可写 ✓）"
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 2：安装 opencode-models-discovery 插件
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 2/4  检测并安装模型发现插件..."

if [ "${SKIP_PLUGIN:-0}" = "1" ]; then
  warn "跳过插件安装（npm 未找到）"
else
  # 检查插件是否已全局安装
  PLUGIN_PATH=$(npm root -g 2>/dev/null)
  if [ -d "${PLUGIN_PATH}/${DISCOVERY_PLUGIN}" ]; then
    info "插件已安装: ${DISCOVERY_PLUGIN}"
  else
    step "正在安装 ${DISCOVERY_PLUGIN}..."
    if npm install -g "${DISCOVERY_PLUGIN}" --silent 2>/dev/null; then
      info "插件安装成功: ${DISCOVERY_PLUGIN}"
    else
      warn "全局安装失败，尝试用户级安装..."
      if npm install --prefix "$HOME/.local" "${DISCOVERY_PLUGIN}" --silent 2>/dev/null; then
        info "插件安装成功（用户级）"
      else
        warn "插件安装失败，将由 opencode 在启动时自动安装"
      fi
    fi
  fi
fi
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 3：写入 OpenCode 配置
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 3/4  写入 OpenCode 配置..."

python3 - << PYEOF
import json, os, time

config_path      = os.path.expanduser("$CONFIG_FILE")
provider_id      = "$PROVIDER_ID"
base_url         = "$BASE_URL"
api_key          = "$API_KEY"
discovery_plugin = "$DISCOVERY_PLUGIN"

# 读取现有配置
if os.path.exists(config_path):
    with open(config_path) as f:
        try:
            cfg = json.load(f)
        except json.JSONDecodeError:
            cfg = {}
else:
    cfg = {}

providers = cfg.setdefault("provider", {})

# 判断 provider_id 使用策略：
#   - 不存在           → 直接用 provider_id
#   - 存在且 URL 相同  → 只更新 apiKey（换 key 场景）
#   - 存在但 URL 不同  → 加时间戳避免覆盖用户已有配置
target_id = provider_id
action    = "新增"
if provider_id in providers:
    existing_url = providers[provider_id].get("options", {}).get("baseURL", "")
    if existing_url and existing_url.rstrip("/") != base_url.rstrip("/"):
        ts = int(time.time())
        target_id = f"{provider_id}-{ts}"
        action = f"重名（URL 不同），新建 {target_id}"
    else:
        action = "更新 API Key"

providers[target_id] = {
    "npm":  "@ai-sdk/openai-compatible",
    "name": "LLM Gateway",
    "options": {
        "baseURL": base_url,
        "apiKey":  api_key,
    },
}

# 写入插件（去重）
plugins = cfg.get("plugin", [])
if discovery_plugin not in plugins:
    plugins.append(discovery_plugin)
    cfg["plugin"] = plugins
    plugin_action = "已添加"
else:
    plugin_action = "已存在"

with open(config_path, "w") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")

print(f"RESULT_TARGET_ID={target_id}")
print(f"RESULT_ACTION={action}")
print(f"RESULT_PLUGIN={plugin_action}")
PYEOF

WRITE_STATUS=$?
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 4：验证写入结果
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 4/4  验证配置写入..."

if [ $WRITE_STATUS -ne 0 ]; then
  err "写入失败（python3 执行出错，请确认 python3 正常工作）"
  send_callback "failed" "python3 执行失败"
  exit 1
fi

# 验证文件存在且包含正确的 provider
if python3 -c "
import json, sys
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
providers = cfg.get('provider', {})
found = any('$PROVIDER_ID' in k for k in providers.keys())
sys.exit(0 if found else 1)
" 2>/dev/null; then
  info "验证通过：配置文件写入正确"
else
  err "验证失败：配置文件中未找到 Provider"
  send_callback "failed" "配置验证失败"
  exit 1
fi

send_callback "success"

# ══════════════════════════════════════════════════════════════════════════════
# 输出执行结论
# ══════════════════════════════════════════════════════════════════════════════
echo ""
divider
echo -e "  ${GREEN}执行完成${NC}"
divider
info "配置文件：$CONFIG_FILE"
info "Provider：${PROVIDER_ID}（已写入）"
info "插件：${DISCOVERY_PLUGIN}（模型列表将在 opencode 启动时自动发现）"
echo ""
echo -e "  ${CYAN}下一步：${NC}重启 opencode，即可在模型列表中看到 LLM Gateway 的所有模型。"
echo ""
divider

command -v osascript >/dev/null 2>&1 && \
  osascript -e "display notification \"OpenCode 配置已完成，请重启 opencode\" with title \"LLM Gateway\"" 2>/dev/null || true
`
