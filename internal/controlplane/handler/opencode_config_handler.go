package handler

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

//go:embed opencode_windows.ps1.tmpl
var windowsTmplRaw string

// discoveryPluginJS 是二开后的 opencode-models-discovery 插件单文件 bundle。
// 源码位于 tools/opencode-models-discovery/，通过 `npm run build:bundle` 重新生成
// 到 assets/opencode-models-discovery.js（提交入库），由网关在配置脚本中下发，
// 避免依赖 npm 公网安装。
//
//go:embed assets/opencode-models-discovery.js
var discoveryPluginJS []byte

// discoveryPluginVersion 是上面 bundle 对应的版本号（构建脚本与 .js 同源生成），
// 供插件自更新时比对。
//
//go:embed assets/opencode-models-discovery.version
var discoveryPluginVersionRaw string

var windowsTmpl = template.Must(template.New("win").Parse(windowsTmplRaw))
var macTmpl = template.Must(template.New("mac").Parse(macTmplRaw))

const (
	opencodeTokenTTL    = 10 * time.Minute
	opencodeTokenPrefix = "opencode_cfg_token:"
)

type SettingsServiceProvider interface {
	GetSupplierName(ctx context.Context) string
}

type OpencodeConfigHandler struct {
	rdb        *redis.Client
	settingsSvc SettingsServiceProvider
}

func NewOpencodeConfigHandler(rdb *redis.Client, settingsSvc SettingsServiceProvider) *OpencodeConfigHandler {
	return &OpencodeConfigHandler{rdb: rdb, settingsSvc: settingsSvc}
}

type tokenRecord struct {
	KeyID        string `json:"key_id"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name,omitempty"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	Status       string `json:"status"` // pending | success | failed
	Msg          string `json:"msg,omitempty"`
}

// GenerateToken POST /api/v1/keys/:id/opencode-config/token  (JWT required)
// 前端在展示对话框「确认执行」后调用，生成一次性 token
func (h *OpencodeConfigHandler) GenerateToken(c *gin.Context) {
	keyID := c.Param("id")

	var req struct {
		ProviderID   string `json:"provider_id"`   // 如 "llm-gateway"
		ProviderName string `json:"provider_name"` // 用户设置的名称
		BaseURL      string `json:"base_url" binding:"required"`
		APIKey       string `json:"api_key"  binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.ProviderID == "" {
		req.ProviderID = "biangfeng-gateway"
	}

	token := uuid.New().String()
	rec := tokenRecord{
		KeyID:        keyID,
		ProviderID:   req.ProviderID,
		ProviderName: req.ProviderName,
		BaseURL:      req.BaseURL,
		APIKey:       req.APIKey,
		Status:       "pending",
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

// externalOrigin 计算对外访问 origin（scheme://host[:port]）。
// 优先用 baseURL（管理员配置的网关公开地址，端口/scheme 可靠），
// 解析失败时回退到请求头（X-Forwarded-Proto/Host），最后回退 c.Request.Host。
func externalOrigin(c *gin.Context, baseURL string) string {
	if u, err := url.Parse(strings.TrimSpace(baseURL)); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	host := c.Request.Host
	if fwdHost := c.GetHeader("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	return scheme + "://" + host
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

	// 外部访问地址（含端口）以 token 里存的 BaseURL 为准 —— 它是管理员配置、
	// opencode 实际用来访问网关的公开地址，scheme/host/端口都正确。回调与插件下载
	// 走同一个对外 origin（nginx 按路径转发到控制面）。
	// 反代场景下 c.Request.Host / X-Forwarded-* 可能丢端口或改写 scheme，故不优先使用。
	origin := externalOrigin(c, rec.BaseURL)
	callbackURL := origin + "/api/v1/opencode-config/callback"
	pluginURL := origin + "/api/v1/opencode-config/plugin"

	supplierName := rec.ProviderName
	if supplierName == "" {
		supplierName = h.settingsSvc.GetSupplierName(ctx)
	}

	params := scriptParams{
		Token:        token,
		ProviderID:   rec.ProviderID,
		BaseURL:      rec.BaseURL,
		APIKey:       rec.APIKey,
		CallbackURL:  callbackURL,
		PluginURL:    pluginURL,
		SupplierName: supplierName,
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

// GetPlugin GET /api/v1/opencode-config/plugin
// 返回二开后的 opencode-models-discovery 插件单文件 bundle（纯 JS，无需 JWT）。
// 配置脚本会下载它到 ~/.config/opencode/plugin/ 供 opencode 本地加载。
func (h *OpencodeConfigHandler) GetPlugin(c *gin.Context) {
	c.Header("Content-Type", "application/javascript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Plugin-Version", strings.TrimSpace(discoveryPluginVersionRaw))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(discoveryPluginJS)
}

// GetPluginVersion GET /api/v1/opencode-config/plugin/version
// 返回当前内嵌插件 bundle 的版本号（纯文本，无需 JWT），供插件自更新比对。
func (h *OpencodeConfigHandler) GetPluginVersion(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, strings.TrimSpace(discoveryPluginVersionRaw))
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
	CallbackURL  string
	PluginURL    string
	SupplierName string
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
PLUGIN_URL="{{.PluginURL}}"
SUPPLIER_NAME="{{.SupplierName}}"
CONFIG_FILE="$HOME/.config/opencode/opencode.json"
# opencode 标准插件目录为复数 plugins/（单数 plugin/ 在新版可能不被加载）。
PLUGIN_DIR="$HOME/.config/opencode/plugins"
PLUGIN_FILE="$PLUGIN_DIR/opencode-models-discovery.js"
# 旧版脚本曾下到单数目录，清理掉以免重复加载或加载到旧副本。
LEGACY_PLUGIN_FILE="$HOME/.config/opencode/plugin/opencode-models-discovery.js"
DISCOVERY_PLUGIN="opencode-models-discovery"

# ── 颜色 & 输出函数 ───────────────────────────────────────────────────────────
GREEN='\033[0;32m'; CYAN='\033[0;36m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()    { echo -e "${GREEN}[✓]${NC} $1"; }
step()    { echo -e "${CYAN}[..]${NC} $1"; }
warn()    { echo -e "${YELLOW}[!]${NC} $1"; }
err()     { echo -e "${RED}[✗]${NC} $1"; }
divider() { echo -e "${CYAN}────────────────────────────────────────${NC}"; }

send_callback() {
  curl -s -X POST "$CALLBACK_URL" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$TOKEN\",\"status\":\"$1\",\"msg\":\"${2:-}\"}" \
    --max-time 8 >/dev/null 2>&1 || true
}

divider
echo -e "  ${SUPPLIER_NAME} — OpenCode 配置助手"
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

if [ -n "$MISSING" ]; then
  err "缺少必要依赖，无法继续："
  echo -e "$MISSING"
  send_callback "failed" "缺少依赖: python3 或 curl"
  exit 1
fi

CONFIG_DIR="$(dirname "$CONFIG_FILE")"
if ! mkdir -p "$CONFIG_DIR" 2>/dev/null; then
  err "无法创建配置目录: ${CONFIG_DIR}（请检查权限）"
  send_callback "failed" "无法创建配置目录"
  exit 1
fi

info "环境检验通过（python3 ✓  curl ✓  配置目录可写 ✓）"
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 2：下载模型发现插件（由网关托管，无需 npm）
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 2/4  下载模型发现插件..."

PLUGIN_INSTALLED=0
# 清理旧的单数目录副本，避免重复加载或加载旧版本。
rm -f "$LEGACY_PLUGIN_FILE" 2>/dev/null || true
if mkdir -p "$PLUGIN_DIR" 2>/dev/null && \
   curl -fsSL "$PLUGIN_URL" -o "$PLUGIN_FILE.tmp" 2>/dev/null && [ -s "$PLUGIN_FILE.tmp" ]; then
  mv "$PLUGIN_FILE.tmp" "$PLUGIN_FILE"
  PLUGIN_INSTALLED=1
  info "插件已下载: $PLUGIN_FILE"
else
  rm -f "$PLUGIN_FILE.tmp" 2>/dev/null || true
  PLUGIN_INSTALLED=0
  warn "插件下载失败（不影响配置写入），可稍后重试"
fi

echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 3：写入 OpenCode 配置（模型交给插件按 key 动态发现+富化）
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 3/4  写入 OpenCode 配置..."

python3 - << PYEOF
import json, os

config_path      = os.path.expanduser("$CONFIG_FILE")
provider_id      = "$PROVIDER_ID"
base_url         = "$BASE_URL"
api_key          = "$API_KEY"
discovery_plugin = "$DISCOVERY_PLUGIN"
supplier_name    = "$SUPPLIER_NAME"

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

# 同名 provider 直接覆盖（不再按 URL 不同新建带时间戳的副本）
target_id = provider_id
action    = "更新（覆盖）" if provider_id in providers else "新增"

# 不再静态写死模型：交给插件按 key 动态发现，并通过 /v1/model/info 富化能力
# （attachment/图像输入、tool_call、上下文/输出上限等）。
# modelsDiscovery.enabled=true 仅对本 provider 开启发现；其它 OpenAI 兼容渠道
# 因插件默认 discovery 关闭而不会被同步。
providers[target_id] = {
    "npm":  "@ai-sdk/openai-compatible",
    "name": supplier_name,
    "options": {
        "baseURL": base_url,
        "apiKey":  api_key,
        "modelsDiscovery": {
            "enabled": True,
            "endpoint": "/v1/models",
            "modelInfoEndpoint": "/v1/model/info",
            "modelInfoFormat": "litellm",
        },
    },
}

# 本地文件插件已下发到 plugin 目录（opencode 自动加载），移除 plugin 数组里旧的
# npm 引用，避免重复加载或联网安装旧版本（旧版默认会同步所有渠道）。
plugins = cfg.get("plugin", [])
kept = []
for p in plugins:
    name = p[0] if isinstance(p, list) and p else p
    if name == discovery_plugin:
        continue
    kept.append(p)
if kept:
    cfg["plugin"] = kept
elif "plugin" in cfg:
    del cfg["plugin"]

with open(config_path, "w") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")

print(f"RESULT_TARGET_ID={target_id}")
print(f"RESULT_ACTION={action}")
PYEOF

WRITE_STATUS=$?
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 4：验证写入结果
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 4/4  验证配置写入..."

if [ $WRITE_STATUS -ne 0 ]; then
  err "写入失败（python3 执行出错）"
  send_callback "failed" "python3 执行失败"
  exit 1
fi

if python3 -c "
import json, sys
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
found = any('$PROVIDER_ID' in k for k in cfg.get('provider', {}).keys())
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

if [ "$PLUGIN_INSTALLED" = "1" ]; then
  info "插件：已下载到 ${PLUGIN_FILE}（每次启动按 key 动态发现并富化模型能力）"
else
  warn "插件：下载失败（配置已写入），请确认能访问 ${PLUGIN_URL} 后重试"
fi

echo ""
echo -e "  ${CYAN}下一步：${NC}重启 opencode，即可在模型列表中看到 ${SUPPLIER_NAME} 的模型（含图像输入等能力）。"
echo ""
divider

command -v osascript >/dev/null 2>&1 && \
  osascript -e "display notification \"OpenCode 配置已完成，请重启 opencode\" with title \"${SUPPLIER_NAME}\"" 2>/dev/null || true
`
