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

// discoveryPluginV2JS / discoveryPluginV2VersionRaw 是同一插件的 OpenCode 2.x 版本。
// 2.x 换掉了整套插件契约（V1 插件在 2.x 里不会被加载），所以单独打一份：
// 启动时向网关 /v1/models 动态发现模型并注入 provider，无需重跑安装脚本。
// 源码 tools/opencode-models-discovery/src/v2/index.ts，由同一条 build:bundle 生成。
//
//go:embed assets/opencode-models-discovery-v2.js
var discoveryPluginV2JS []byte

//go:embed assets/opencode-models-discovery-v2.version
var discoveryPluginV2VersionRaw string

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
	rdb         *redis.Client
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

	// 可选的版本覆盖 ?ver=v1|v2：正常由脚本自行探测 `opencode --version` 的主版本，
	// 这里只留一个逃生口（自制 wrapper 吃掉 --version、或用户明确要求按某个格式写入时）。
	forceVersion := strings.ToLower(strings.TrimSpace(c.Query("ver")))
	if forceVersion != "v1" && forceVersion != "v2" {
		forceVersion = ""
	}

	params := scriptParams{
		Token:        token,
		ProviderID:   rec.ProviderID,
		BaseURL:      rec.BaseURL,
		APIKey:       rec.APIKey,
		CallbackURL:  callbackURL,
		PluginURL:    pluginURL,
		SupplierName: supplierName,
		ForceVersion: forceVersion,
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
// `?ver=v2` 返回 OpenCode 2.x 版本（V1/V2 插件契约不兼容，不能混用）。
func (h *OpencodeConfigHandler) GetPlugin(c *gin.Context) {
	body, version := discoveryPluginJS, discoveryPluginVersionRaw
	if strings.EqualFold(c.Query("ver"), "v2") {
		body, version = discoveryPluginV2JS, discoveryPluginV2VersionRaw
	}
	c.Header("Content-Type", "application/javascript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Plugin-Version", strings.TrimSpace(version))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(body)
}

// GetPluginVersion GET /api/v1/opencode-config/plugin/version
// 返回当前内嵌插件 bundle 的版本号（纯文本，无需 JWT），供插件自更新比对。
// `?ver=v2` 返回 OpenCode 2.x 插件版本。
func (h *OpencodeConfigHandler) GetPluginVersion(c *gin.Context) {
	version := discoveryPluginVersionRaw
	if strings.EqualFold(c.Query("ver"), "v2") {
		version = discoveryPluginV2VersionRaw
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, strings.TrimSpace(version))
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
	Token        string
	ProviderID   string
	BaseURL      string
	APIKey       string
	CallbackURL  string
	PluginURL    string
	SupplierName string
	ForceVersion string // "" = 自动探测；"v1" / "v2" = 强制
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
# 服务端可强制指定版本（?ver=v1|v2），留空则由本脚本自动探测。
FORCE_VERSION="{{.ForceVersion}}"
CONFIG_FILE="$HOME/.config/opencode/opencode.json"
# opencode 标准插件目录为复数 plugins/（单数 plugin/ 在新版可能不被加载）。
PLUGIN_DIR="$HOME/.config/opencode/plugins"
PLUGIN_FILE="$PLUGIN_DIR/opencode-models-discovery.js"
# 旧版脚本曾下到单数目录，清理掉以免重复加载或加载到旧副本。
LEGACY_PLUGIN_FILE="$HOME/.config/opencode/plugin/opencode-models-discovery.js"
# V2 插件与 V1 契约不兼容，文件名区分开，装哪版就清掉另一版。
PLUGIN_V2_FILE="$PLUGIN_DIR/opencode-models-discovery-v2.js"
LEGACY_PLUGIN_V2_FILE="$HOME/.config/opencode/plugin/opencode-models-discovery-v2.js"
# V2 插件从同目录的 sidecar 读网关地址/key（插件执行时 opencode.json 还没进注册表）。
PLUGIN_SIDECAR="$PLUGIN_DIR/opencode-models-discovery.json"
DISCOVERY_PLUGIN="opencode-models-discovery"
# BASE_URL 形如 https://host/v1；去掉尾斜杠后拼 /models、/model/info。
API_BASE="${BASE_URL%/}"

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

# ── 探测本地 OpenCode 大版本 ──────────────────────────────────────────────────
# V1 打 1.x.y，V2 打 2.x.y，命令名都是 opencode。探测不到（未安装/输出异常）
# 时按 V1 处理，保持原有行为；cli.json 是 V2 终端客户端首次启动才生成的，
# 只能当「确认是 V2」的正向信号，不能反推。
OC_BIN="$(command -v opencode 2>/dev/null || true)"
# curl 安装的 V2 落在 ~/.opencode/bin，非登录 shell 里可能不在 PATH。
if [ -z "$OC_BIN" ] && [ -x "$HOME/.opencode/bin/opencode" ]; then
  OC_BIN="$HOME/.opencode/bin/opencode"
fi
OC_VERSION=""
if [ -n "$OC_BIN" ]; then
  OC_VERSION="$("$OC_BIN" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
fi
OC_MAJOR="${OC_VERSION%%.*}"

MODE="v1"
if [ -n "$FORCE_VERSION" ]; then
  MODE="$FORCE_VERSION"
elif [ -n "$OC_MAJOR" ] && [ "$OC_MAJOR" -ge 2 ] 2>/dev/null; then
  MODE="v2"
elif [ -z "$OC_MAJOR" ] && [ -f "$HOME/.config/opencode/cli.json" ]; then
  MODE="v2"
fi

info "环境检验通过（python3 ✓  curl ✓  配置目录可写 ✓）"
if [ "$MODE" = "v2" ]; then
  info "检测到 OpenCode ${OC_VERSION:-未知} —— V2 模式（写入原生 V2 配置 + 静态模型清单）"
else
  info "检测到 OpenCode ${OC_VERSION:-未知} —— V1 模式（下发发现插件，模型动态发现）"
  if [ -z "$OC_BIN" ]; then
    warn "未找到 opencode 命令；若你用的是 OpenCode 2，请安装后重新执行本脚本"
  fi
fi
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 2：下载模型发现插件（由网关托管，无需 npm）
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 2/4  下载模型发现插件..."

PLUGIN_INSTALLED=0
if [ "$MODE" = "v2" ]; then
  # V2 用另一套插件契约：下发 V2 插件 + sidecar，让它启动时自己发现模型。
  # V1 插件在 2.x 里不会被加载，顺手清掉免得留垃圾。
  rm -f "$PLUGIN_FILE" "$LEGACY_PLUGIN_FILE" "$LEGACY_PLUGIN_V2_FILE" 2>/dev/null || true
  if mkdir -p "$PLUGIN_DIR" 2>/dev/null && \
     curl -fsSL "$PLUGIN_URL?ver=v2" -o "$PLUGIN_V2_FILE.tmp" 2>/dev/null && [ -s "$PLUGIN_V2_FILE.tmp" ]; then
    mv "$PLUGIN_V2_FILE.tmp" "$PLUGIN_V2_FILE"
    # sidecar 走环境变量 + 独立 heredoc：不再把 key/供应商名拼进代码，避免引号把 JSON 打坏。
    if OC_SIDECAR_PROVIDER_ID="$PROVIDER_ID" \
       OC_SIDECAR_PROVIDER_NAME="$SUPPLIER_NAME" \
       OC_SIDECAR_BASE_URL="$BASE_URL" \
       OC_SIDECAR_API_KEY="$API_KEY" \
       python3 - "$PLUGIN_SIDECAR" <<'PY' 2>/dev/null
import json, os, sys

payload = {
    "providerID": os.environ["OC_SIDECAR_PROVIDER_ID"],
    "providerName": os.environ["OC_SIDECAR_PROVIDER_NAME"],
    "baseURL": os.environ["OC_SIDECAR_BASE_URL"],
    "apiKey": os.environ["OC_SIDECAR_API_KEY"],
}
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(payload, handle, ensure_ascii=False, indent=2)
    handle.write("\n")
PY
    then
      PLUGIN_INSTALLED=1
      info "V2 插件已下载: ${PLUGIN_V2_FILE}（启动时自动发现模型）"
    else
      rm -f "$PLUGIN_V2_FILE" "$PLUGIN_SIDECAR" 2>/dev/null || true
      PLUGIN_INSTALLED=0
      warn "V2 插件安装失败（配置里的静态模型清单仍可用），可稍后重试"
    fi
  else
    rm -f "$PLUGIN_V2_FILE.tmp" 2>/dev/null || true
    PLUGIN_INSTALLED=0
    warn "V2 插件下载失败（不影响配置写入），可稍后重试"
  fi
else
  # 清理旧的单数目录副本，避免重复加载或加载旧版本。
  rm -f "$LEGACY_PLUGIN_FILE" "$PLUGIN_V2_FILE" "$LEGACY_PLUGIN_V2_FILE" "$PLUGIN_SIDECAR" 2>/dev/null || true
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
fi

echo ""

# ══════════════════════════════════════════════════════════════════════════════
# 阶段 3：写入 OpenCode 配置（V1 只写 provider 元数据；V2 额外写静态模型清单）
# ══════════════════════════════════════════════════════════════════════════════
step "阶段 3/4  写入 OpenCode 配置..."

WRITE_STATUS=0
if [ "$MODE" = "v2" ]; then
if ! python3 - << PYEOF
# V2：写原生形状 providers.<id>，并把模型清单/能力在安装时抓一次、静态写进去。
# 原因是 V1 插件实现在 V2 里不运行，也没有等价的"启动时注入模型"的免费路径。
import json, os, sys, urllib.request

config_path   = os.path.expanduser("$CONFIG_FILE")
provider_id   = "$PROVIDER_ID"
base_url      = "$BASE_URL"
api_base      = "$API_BASE"
api_key       = "$API_KEY"
supplier_name = "$SUPPLIER_NAME"

def api_get(path):
    req = urllib.request.Request(api_base + path, headers={
        "Authorization": "Bearer " + api_key,
        "Content-Type": "application/json",
    })
    with urllib.request.urlopen(req, timeout=15) as resp:
        return json.load(resp)

# 1) 模型清单（对外是模型组名）
model_ids = []
try:
    for m in api_get("/models").get("data", []):
        mid = m.get("id")
        if isinstance(mid, str) and mid:
            model_ids.append(mid)
except Exception:
    model_ids = []

if not model_ids:
    sys.exit("无法从 " + api_base + "/models 获取模型清单")

# 2) 能力（LiteLLM /model/info），拿不到就退化成 OpenCode 的默认假设
info_map = {}
try:
    for e in api_get("/model/info").get("data", []):
        mi = e.get("model_info") or {}
        key = e.get("model_name") or mi.get("key")
        if isinstance(key, str) and key:
            info_map[key] = mi
except Exception:
    info_map = {}

def usable(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool) and v > 0

def reasoning_efforts(mi):
    # 与 V1 插件同一套判定：只有暴露了 reasoning_effort 才给 effort 变体。
    if mi.get("supports_reasoning") is not True:
        return None
    if "reasoning_effort" not in (mi.get("supported_openai_params") or []):
        return None
    efforts = []
    if mi.get("supports_none_reasoning_effort") is True:
        efforts.append("none")
    if mi.get("supports_minimal_reasoning_effort") is True:
        efforts.append("minimal")
    if mi.get("supports_low_reasoning_effort") is not False:
        efforts.append("low")
    efforts.extend(["medium", "high"])
    if mi.get("supports_xhigh_reasoning_effort") is True:
        efforts.append("xhigh")
    if mi.get("supports_max_reasoning_effort") is True:
        efforts.append("max")
    return efforts

models = {}
for mid in model_ids:
    mi = info_map.get(mid) or info_map.get(mid.lower()) or {}
    mode = mi.get("mode")
    # 只写 chat 模型：V2 只把 chat 模型提供进会话。
    if isinstance(mode, str) and mode and mode != "chat":
        continue

    entry = {"name": mid}
    caps = {}
    if mi.get("supports_function_calling") is True or mi.get("supports_tool_choice") is True:
        caps["tools"] = True

    inputs = mi.get("input_modalities") or mi.get("supported_modalities")
    if isinstance(inputs, list) and inputs:
        inputs = [str(x).lower() for x in inputs]
        if "text" not in inputs:
            inputs = ["text"] + inputs
    elif mi.get("supports_vision") is True:
        inputs = ["text", "image"]
    elif mi.get("supports_vision") is False:
        inputs = ["text"]
    else:
        inputs = None
    if inputs:
        caps["input"] = inputs

    outputs = mi.get("output_modalities")
    if isinstance(outputs, list) and outputs:
        caps["output"] = [str(x).lower() for x in outputs]

    efforts = reasoning_efforts(mi)
    if efforts:
        caps["reasoning"] = {"supported": True, "effort": efforts}

    if caps:
        entry["capabilities"] = caps

    ctx_limit = mi.get("max_input_tokens") if usable(mi.get("max_input_tokens")) else mi.get("max_tokens")
    out_limit = mi.get("max_output_tokens") if usable(mi.get("max_output_tokens")) else mi.get("max_tokens")
    if usable(ctx_limit) and usable(out_limit):
        entry["limit"] = {"context": int(ctx_limit), "output": int(out_limit)}

    models[mid] = entry

# 3) 写入配置
cfg = {}
if os.path.exists(config_path):
    try:
        with open(config_path) as f:
            cfg = json.load(f)
    except json.JSONDecodeError as e:
        # 配置不是合法 JSON：不猜、也不覆盖，直接失败让用户先修好文件。
        print(f"RESULT_PARSE_ERROR={e}")
        raise SystemExit(1)

if not isinstance(cfg.get("providers"), dict):
    cfg["providers"] = {}
providers = cfg["providers"]
action = "更新（覆盖）" if provider_id in providers else "新增"
providers[provider_id] = {
    "name": supplier_name,
    "package": "aisdk:@ai-sdk/openai-compatible",
    "settings": {"baseURL": base_url, "apiKey": api_key},
    "models": models,
}

# 删掉旧版 V1 形状（provider 单数）里的同名条目：同一个 provider 条目不能混两种格式。
legacy = cfg.get("provider")
if isinstance(legacy, dict):
    legacy.pop(provider_id, None)
    if not legacy:
        cfg.pop("provider", None)

# 清掉插件声明：V1 插件实现不会在 V2 里运行。
for key in ("plugin", "plugins"):
    entries = cfg.get(key)
    if isinstance(entries, list):
        kept = []
        for p in entries:
            if isinstance(p, list) and p:
                name = p[0]
            elif isinstance(p, dict):
                name = p.get("package")
            else:
                name = p
            if name == "$DISCOVERY_PLUGIN":
                continue
            kept.append(p)
        if kept:
            cfg[key] = kept
        else:
            cfg.pop(key, None)

with open(config_path, "w") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")

print(f"RESULT_TARGET_ID={provider_id}")
print(f"RESULT_ACTION={action}")
print(f"RESULT_MODEL_COUNT={len(models)}")
PYEOF
then
  WRITE_STATUS=1
fi
else
if ! python3 - << PYEOF
import json, os

config_path      = os.path.expanduser("$CONFIG_FILE")
provider_id      = "$PROVIDER_ID"
base_url         = "$BASE_URL"
api_key          = "$API_KEY"
discovery_plugin = "$DISCOVERY_PLUGIN"
supplier_name    = "$SUPPLIER_NAME"

# 读取现有配置
# 解析失败不能当成空配置继续写：那会清掉用户其它的 provider/plugin。
cfg = {}
if os.path.exists(config_path):
    with open(config_path) as f:
        try:
            cfg = json.load(f)
        except json.JSONDecodeError as e:
            print(f"RESULT_PARSE_ERROR={e}")
            raise SystemExit(1)

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
# 老版本写 plugin 数组，新版本读 plugins，两套都清掉，否则仍会重复加载。
for key in ("plugin", "plugins"):
    entries = cfg.get(key)
    if not isinstance(entries, list):
        continue
    kept = []
    for p in entries:
        if isinstance(p, list) and p:
            name = p[0]
        elif isinstance(p, dict):
            name = p.get("package")
        else:
            name = p
        if name == discovery_plugin:
            continue
        kept.append(p)
    if kept:
        cfg[key] = kept
    else:
        cfg.pop(key, None)

with open(config_path, "w") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")

print(f"RESULT_TARGET_ID={target_id}")
print(f"RESULT_ACTION={action}")
PYEOF
then
  WRITE_STATUS=1
fi
fi
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

MODEL_COUNT=0
if [ "$MODE" = "v2" ]; then
  MODEL_COUNT="$(python3 -c "
import json
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
provider = (cfg.get('providers') or {}).get('$PROVIDER_ID') or {}
print(len(provider.get('models') or {}))
" 2>/dev/null || echo 0)"
  if [ "${MODEL_COUNT:-0}" -gt 0 ] 2>/dev/null; then
    info "验证通过：providers.$PROVIDER_ID 已写入 ${MODEL_COUNT} 个模型"
  else
    err "验证失败：配置文件中未找到 providers.$PROVIDER_ID 的模型"
    send_callback "failed" "配置验证失败"
    exit 1
  fi
elif python3 -c "
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

if [ "$MODE" = "v2" ]; then
  info "模型：${MODEL_COUNT} 个（安装时快照，已带上图像输入/工具调用/上下文上限）"
  if [ "$PLUGIN_INSTALLED" = "1" ]; then
    info "插件：V2 版已安装（每次启动自动发现，网关后台新增模型无需重跑本脚本）"
  else
    warn "插件：V2 版未安装，当前只有安装时快照；网关后台新增模型需重新执行本脚本"
  fi
else
  if [ "$PLUGIN_INSTALLED" = "1" ]; then
    info "插件：已下载到 ${PLUGIN_FILE}（每次启动按 key 动态发现并富化模型能力）"
  else
    warn "插件：下载失败（配置已写入），请确认能访问 ${PLUGIN_URL} 后重试"
  fi
fi

echo ""
echo -e "  ${CYAN}下一步：${NC}重启 opencode，即可在模型列表中看到 ${SUPPLIER_NAME} 的模型（含图像输入等能力）。"
echo ""
divider

command -v osascript >/dev/null 2>&1 && \
  osascript -e "display notification \"OpenCode 配置已完成，请重启 opencode\" with title \"${SUPPLIER_NAME}\"" 2>/dev/null || true
`
