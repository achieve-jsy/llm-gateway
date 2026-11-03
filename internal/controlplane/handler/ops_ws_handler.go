package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// WebSocket 安全加固（gap#2 / WP14）
//
// 问题：原实现 CheckOrigin 无条件返回 true、升级前无任何鉴权（定义了
// extractTokenFromProtocol 却从不调用）、无连接数限制、realtime 监控禁用时
// 无致命关闭码导致前端死循环重连。
//
// 加固项（对齐参考仓 sub2api_final 的 WS 安全范式，不照抄其包结构）：
//  1. Origin 校验（isAllowedOpsWSOrigin）：默认严格同源，可信代理读
//     X-Forwarded-Host；可通过 env 切换宽松策略。
//  2. JWT 鉴权：复用项目 admin JWT 校验范式（HMAC + JWT_SECRET，校验
//     signing method / Valid / user_id claim），token 从 Sec-WebSocket-Protocol
//     的 jwt.<token> 子协议读取（与 middleware.JWTAuth 同源逻辑）。
//  3. 连接数限制：全局 MaxConns（默认 100）+ 每 IP MaxConnsPerIP（默认 20），
//     atomic CAS / mutex 计数，超限拒绝升级。
//  4. 4001 致命关闭：realtime 监控禁用时升级后立即发 close 4001，前端识别后
//     停止重连（开关 env OPS_REALTIME_MONITORING_ENABLED，默认 enabled）。
//
// 扩展性改进（共享缓存 payload：一 goroutine ≤1/5s 刷新，连接推缓存字节）
// 本次降级为 TODO，仅做安全加固。
// ============================================================================

// ---- 环境变量开关 ----

const (
	envOpsWSOriginPolicy       = "OPS_WS_ORIGIN_POLICY"        // strict | permissive，默认 strict
	envOpsWSTrustProxy         = "OPS_WS_TRUST_PROXY"          // bool，默认 false
	envOpsWSTrustedProxies     = "OPS_WS_TRUSTED_PROXIES"      // CIDR 逗号分隔，默认 127.0.0.0/8,::1/128
	envOpsWSAllowedOrigins     = "OPS_WS_ALLOWED_ORIGINS"      // 额外允许的 origin host（逗号分隔），默认空
	envOpsWSMaxConns           = "OPS_WS_MAX_CONNS"            // 全局最大连接数，默认 100
	envOpsWSMaxConnsPerIP      = "OPS_WS_MAX_CONNS_PER_IP"     // 每 IP 最大连接数，默认 20
	envOpsRealtimeEnabled      = "OPS_REALTIME_MONITORING_ENABLED" // realtime 监控开关，默认 enabled
)

const (
	originPolicyStrict     = "strict"
	originPolicyPermissive = "permissive"

	defaultMaxOpsWSConns      = 100
	defaultMaxOpsWSConnsPerIP = 20
)

// close code 语义：
//   4001 realtime_disabled —— realtime 监控关闭，前端收到后不应重连
//   4029 too_many_connections —— 连接数超限（应用自定义，参考 HTTP 429）
//   4401 unauthorized —— 鉴权失败升级后致命关闭（应用自定义）
const (
	opsWSCloseRealtimeDisabled = 4001
	opsWSCloseTooManyConns     = 4029
	opsWSCloseUnauthorized     = 4401
)

// opsWSConfig 在进程启动时从 env 读取一次；测试可通过 setOpsWSConfigForTest 临时覆盖。
type opsWSConfig struct {
	OriginPolicy   string
	TrustProxy     bool
	TrustedProxies []netip.Prefix
	AllowedOrigins map[string]struct{}
	MaxConns       int32
	MaxConnsPerIP  int32
}

var opsWSCfg = loadOpsWSConfigFromEnv()

// Realtime 监控开关：默认 enabled。settings key 尚未由 Agent A 定义，
// 此处先以 env 兜底（不依赖 settings 改动），后续可平移到 settings。
func opsRealtimeMonitoringEnabled() bool {
	v := strings.TrimSpace(os.Getenv(envOpsRealtimeEnabled))
	if v == "" {
		return true // 默认开启
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

func loadOpsWSConfigFromEnv() opsWSConfig {
	cfg := opsWSConfig{
		OriginPolicy:   originPolicyStrict,
		TrustProxy:     false,
		TrustedProxies: parseTrustedProxyList("127.0.0.0/8,::1/128"),
		AllowedOrigins: parseAllowedOrigins(os.Getenv(envOpsWSAllowedOrigins)),
		MaxConns:       defaultMaxOpsWSConns,
		MaxConnsPerIP:  defaultMaxOpsWSConnsPerIP,
	}

	if v := strings.TrimSpace(os.Getenv(envOpsWSOriginPolicy)); v != "" {
		switch strings.ToLower(v) {
		case originPolicyStrict, originPolicyPermissive:
			cfg.OriginPolicy = strings.ToLower(v)
		default:
			slog.Warn("ops_ws: invalid OPS_WS_ORIGIN_POLICY", "value", v, "fallback", cfg.OriginPolicy)
		}
	}

	if v := strings.TrimSpace(os.Getenv(envOpsWSTrustProxy)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.TrustProxy = b
		} else {
			slog.Warn("ops_ws: invalid OPS_WS_TRUST_PROXY", "value", v, "fallback", cfg.TrustProxy)
		}
	}

	if raw := strings.TrimSpace(os.Getenv(envOpsWSTrustedProxies)); raw != "" {
		cfg.TrustedProxies = parseTrustedProxyList(raw)
	}

	if v := strings.TrimSpace(os.Getenv(envOpsWSMaxConns)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConns = int32(n)
		} else {
			slog.Warn("ops_ws: invalid OPS_WS_MAX_CONNS", "value", v, "fallback", cfg.MaxConns)
		}
	}
	if v := strings.TrimSpace(os.Getenv(envOpsWSMaxConnsPerIP)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxConnsPerIP = int32(n)
		} else {
			slog.Warn("ops_ws: invalid OPS_WS_MAX_CONNS_PER_IP", "value", v, "fallback", cfg.MaxConnsPerIP)
		}
	}

	return cfg
}

func parseTrustedProxyList(raw string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, token := range strings.Split(raw, ",") {
		item := strings.TrimSpace(token)
		if item == "" {
			continue
		}
		var (
			p   netip.Prefix
			err error
		)
		if strings.Contains(item, "/") {
			p, err = netip.ParsePrefix(item)
		} else {
			var addr netip.Addr
			addr, err = netip.ParseAddr(item)
			if err == nil {
				addr = addr.Unmap()
				bits := 128
				if addr.Is4() {
					bits = 32
				}
				p = netip.PrefixFrom(addr, bits)
			}
		}
		if err != nil || !p.IsValid() {
			continue
		}
		prefixes = append(prefixes, p.Masked())
	}
	return prefixes
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, token := range strings.Split(raw, ",") {
		host := strings.ToLower(strings.TrimSpace(token))
		if host == "" {
			continue
		}
		m[host] = struct{}{}
	}
	return m
}

// ---- 连接计数 ----

var opsWSConnCount atomic.Int32

var opsWSConnByIPMu sync.Mutex
var opsWSConnByIP = make(map[string]int32)

func tryAcquireOpsWSTotalSlot(limit int32) bool {
	if limit <= 0 {
		return true
	}
	for {
		current := opsWSConnCount.Load()
		if current >= limit {
			return false
		}
		if opsWSConnCount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func tryAcquireOpsWSIPSlot(clientIP string, limit int32) bool {
	if strings.TrimSpace(clientIP) == "" || limit <= 0 {
		return true
	}
	opsWSConnByIPMu.Lock()
	defer opsWSConnByIPMu.Unlock()
	current := opsWSConnByIP[clientIP]
	if current >= limit {
		return false
	}
	opsWSConnByIP[clientIP] = current + 1
	return true
}

func releaseOpsWSIPSlot(clientIP string) {
	if strings.TrimSpace(clientIP) == "" {
		return
	}
	opsWSConnByIPMu.Lock()
	defer opsWSConnByIPMu.Unlock()
	current, ok := opsWSConnByIP[clientIP]
	if !ok {
		return
	}
	if current <= 1 {
		delete(opsWSConnByIP, clientIP)
		return
	}
	opsWSConnByIP[clientIP] = current - 1
}

// 计数器复位：测试在缩小 MaxConns 跑超限场景前后调用，避免跨用例串扰。
func resetOpsWSConnCountersForTest() {
	opsWSConnCount.Store(0)
	opsWSConnByIPMu.Lock()
	opsWSConnByIP = make(map[string]int32)
	opsWSConnByIPMu.Unlock()
}

// 测试专用配置注入（非线程安全，仅测试用）。
func setOpsWSConfigForTest(cfg opsWSConfig) func() {
	prev := opsWSCfg
	opsWSCfg = cfg
	return func() { opsWSCfg = prev }
}

// ---- upgrader ----

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return isAllowedOpsWSOrigin(r) },
	// Subprotocol negotiation: 前端传 ["ops-admin", "jwt.<token>"]；
	// 始终选择 "ops-admin"，token 不在握手响应中回显。
	Subprotocols: []string{"ops-admin"},
}

type OpsWSHandler struct {
	rdb *redis.Client
}

func NewOpsWSHandler(rdb *redis.Client) *OpsWSHandler {
	return &OpsWSHandler{rdb: rdb}
}

// HandleQPSWebSocket 处理 realtime QPS WebSocket 推送。
// 安全顺序：origin -> JWT -> 连接数限制 -> realtime 开关（4001）-> 升级 -> 推送。
// 浏览器 WS 无法携带 Authorization header，因此鉴权在 handler 内完成
//（与 admin JWTAuth 中间件同源逻辑，复用 HMAC + JWT_SECRET）。
func (h *OpsWSHandler) HandleQPSWebSocket(c *gin.Context) {
	// realtime 监控禁用：升级后发 close 4001，前端识别后停止重连。
	if !opsRealtimeMonitoringEnabled() {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, opsWSResponseHeader(c))
		if err != nil {
			return
		}
		defer conn.Close()
		writeOpsWSClose(conn, opsWSCloseRealtimeDisabled, "realtime_disabled")
		return
	}

	// JWT 鉴权：失败先尝试升级再致命关闭（浏览器 WS 升级前无法返回 401 body
	// 被前端稳定识别），降级为 close 4401；同时禁止继续。
	token := extractTokenFromProtocol(c)
	if token == "" {
		token = strings.TrimSpace(c.Query("token"))
	}
	claims, ok := validateOpsWSToken(token)
	if !ok {
		// 仍尝试升级以发送 close 帧（前端能拿到 4401），失败则直接 401。
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, opsWSResponseHeader(c))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		defer conn.Close()
		writeOpsWSClose(conn, opsWSCloseUnauthorized, "unauthorized")
		return
	}

	clientIP := opsWSClientIP(c.Request)

	// 全局连接数限制。
	if !tryAcquireOpsWSTotalSlot(opsWSCfg.MaxConns) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many connections"})
		return
	}
	defer opsWSConnCount.Add(-1)

	// 每 IP 连接数限制。
	if opsWSCfg.MaxConnsPerIP > 0 && clientIP != "" {
		if !tryAcquireOpsWSIPSlot(clientIP, opsWSCfg.MaxConnsPerIP) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many connections"})
			return
		}
		defer releaseOpsWSIPSlot(clientIP)
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, opsWSResponseHeader(c))
	if err != nil {
		return
	}
	defer conn.Close()

	// 注入鉴权身份供后续日志/审计（与 middleware.JWTAuth 设置的 key 一致）。
	if claims != nil {
		if uid, ok := claims["user_id"].(string); ok && uid != "" {
			c.Set("user_id", uid)
		}
	}

	h.serveQPSWebSocket(c.Request.Context(), conn)
}

// serveQPSWebSocket 推送循环。每 3s 轮询 Redis（扩展性 TODO：改为共享缓存 payload）。
func (h *OpsWSHandler) serveQPSWebSocket(ctx context.Context, conn *websocket.Conn) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	})

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			minKey := now.Format("200601021504")

			qpsCount, _ := h.rdb.Get(ctx, "ops:qps:"+minKey).Int64()
			tpsCount, _ := h.rdb.Get(ctx, "ops:tps:"+minKey).Int64()

			msg := map[string]any{
				"qps":       float64(qpsCount) / 60.0,
				"tps":       float64(tpsCount) / 60.0,
				"timestamp": now.Format(time.RFC3339),
				"sample":    qpsCount,
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---- JWT 鉴权 ----

// extractTokenFromProtocol 从 Sec-WebSocket-Protocol 的 jwt.<token> 子协议读取 token。
func extractTokenFromProtocol(c *gin.Context) string {
	protocols := c.Request.Header.Get("Sec-WebSocket-Protocol")
	for _, p := range strings.Split(protocols, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "jwt.") {
			return strings.TrimPrefix(p, "jwt.")
		}
	}
	return ""
}

// validateOpsWSToken 复用 admin JWTAuth 同源校验范式：HMAC + JWT_SECRET，
// 校验 signing method、Valid、user_id claim 存在。成功返回 claims。
// secret 解析与 middleware.JWTAuth 完全一致，保证 token 跨入口互通。
func validateOpsWSToken(token string) (jwt.MapClaims, bool) {
	if token == "" {
		return nil, false
	}
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		secret = "change-me-in-production"
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		return nil, false
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false
	}
	// 与 middleware.JWTAuth 一致：要求 user_id（或 passport）存在。
	uid, _ := claims["user_id"].(string)
	if uid == "" {
		if passport, ok := claims["passport"].(string); ok && passport != "" {
			claims["user_id"] = passport
		}
	}
	if uidAny, _ := claims["user_id"].(string); uidAny == "" {
		return nil, false
	}
	return claims, true
}

// ---- Origin 校验 ----

// isAllowedOpsWSOrigin：默认严格同源；可信代理读 X-Forwarded-Host；
// OPS_WS_ALLOWED_ORIGINS 额外放行白名单 host。
func isAllowedOpsWSOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// 无 Origin（非浏览器客户端）：strict 拒绝，permissive 放行。
		switch strings.ToLower(strings.TrimSpace(opsWSCfg.OriginPolicy)) {
		case originPolicyStrict:
			return false
		default:
			return true
		}
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	originHost := strings.ToLower(parsed.Hostname())

	// 白名单直配（运维显式放行的额外 origin host）。
	if _, ok := opsWSCfg.AllowedOrigins[originHost]; ok {
		return true
	}

	// 同源校验：取请求 Host（可信代理下用 X-Forwarded-Host 第一段）。
	reqHost := hostWithoutPort(r.Host)
	if opsWSCfg.TrustProxy && shouldTrustOpsWSProxyHeaders(r) {
		if xfHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); xfHost != "" {
			reqHost = hostWithoutPort(xfHost)
		}
	}
	reqHost = strings.ToLower(reqHost)
	if reqHost == "" {
		return false
	}
	return originHost == reqHost
}

func shouldTrustOpsWSProxyHeaders(r *http.Request) bool {
	if r == nil || !opsWSCfg.TrustProxy {
		return false
	}
	peer, ok := opsWSPeerIP(r)
	if !ok {
		return false
	}
	for _, p := range opsWSCfg.TrustedProxies {
		if p.Contains(peer) {
			return true
		}
	}
	return false
}

func opsWSPeerIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if host == "" {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func opsWSClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if shouldTrustOpsWSProxyHeaders(r) {
		if xff := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); xff != "" {
			xff = strings.TrimSuffix(strings.TrimPrefix(xff, "["), "]")
			if addr, err := netip.ParseAddr(xff); err == nil && addr.IsValid() {
				return addr.Unmap().String()
			}
		}
	}
	if peer, ok := opsWSPeerIP(r); ok && peer.IsValid() {
		return peer.String()
	}
	return ""
}

func hostWithoutPort(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.Trim(hostport, "[]")
	}
	return strings.Split(hostport, ":")[0]
}

// ---- 升级辅助 ----

// opsWSResponseHeader 回显子协议（浏览器 require：客户端传了 Sec-WebSocket-Protocol
// 时服务端必须回显选中协议，否则握手失败）。始终回显 "ops-admin"。
func opsWSResponseHeader(c *gin.Context) http.Header {
	h := http.Header{}
	if proto := c.GetHeader("Sec-WebSocket-Protocol"); proto != "" {
		h.Set("Sec-WebSocket-Protocol", "ops-admin")
	}
	return h
}

func writeOpsWSClose(conn *websocket.Conn, code int, reason string) {
	if conn == nil {
		return
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
}
