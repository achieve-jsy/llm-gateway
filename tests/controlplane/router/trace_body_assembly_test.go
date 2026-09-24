package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/router"
	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是计划 3/4 **任务 8** 承接的三条「只有静态证据」项（任务 5 报告 §6.5）的**行为**证据，
// 也就是计划 Task 8 顶部 **E33 承接块**要求的「真实 `router.Setup` 装配（两态）」：
//
//	① 真实 `router.Setup` 装配，`trace_body.root` **启用 / 禁用两态**：
//	   启用时带定位键的行经装配后的路由拿到归档正文；
//	   禁用时同样的行得到 **503 `trace_body_disabled`**（不是 500、不是 404、不是 jsonb 回退）。
//	② 权限门 `PermUsageViewAll`：装配后的路由上「有 token 但无该权限」被既有 `guard` 拒绝（403），
//	   「无 token」被既有 `JWTAuth` 拒绝（401）。
//	③ 「不新建鉴权中间件」的行为侧：认证/授权结果与既有中间件一致，
//	   **不出现第二套凭据解析**（管理面只认 `Authorization: Bearer`；内部端点走**既有**
//	   `InternalAuth`，它接受 `X-Internal-Token`，**也**接受以 `Authorization: Bearer`
//	   形式携带的**内部 token** —— 这是同一中间件的两条读取路径（`middleware.go:219-222`），
//	   不是第二套凭据解析）。
//
// 【本文件的性质】`router.Setup(lifecycleCtx, db, rdb)` 是**生产入口**（`cmd/control-plane/main.go`
// 调它）。本文件直接调它 —— 这是本仓库第一处 `router.Setup` 的测试（此前
// `grep -rln "router.Setup" tests/` 零命中，故任务 5 的 m06 只能降级为静态检查）。
//
// 【效力边界（必须随结论一起读，不得含糊）】
//   - **不覆盖真实 PG / 真实 Redis**：`db` 是**内存 sqlite**、`rdb` 指向一个**不可达地址**
//     （`127.0.0.1:1`）。`Setup` 对 Redis 只做惰性使用（连接失败只记日志、不 panic，实测），
//     因此它证明的是「装配后的**路由与中间件链**的行为」，**不是**「生产 PG/Redis 下的行为」。
//   - **不覆盖「不新建鉴权中间件」的**不存在性**：那条只有任务 5 的静态 CHECKS 能承重；
//     本文件只补**行为侧**（认证/授权结果与既有中间件一致）。
//   - `Setup` 会启动若干后台 goroutine / cron（审计落库、rollup、key_activity…）：sqlite 缺少
//     那些表，它们只会打日志失败。**它们不参与本文件的断言**，断言只看 HTTP 表现。
//   - **`trace_body.root` 的两态只能由环境变量驱动**：`Setup` 内部自己调
//     `cpconfig.Load()`（`router.go:42`），没有注入点。且按 E6 已核实的事实，
//     **`TRACE_BODY_ROOT=""` 不能覆盖 YAML**（vendored viper 跳过空串）⇒ 「禁用态」在
//     生产上由 YAML 侧表达；本文件里没有 YAML（测试 cwd 下 `./config`、`../config`、
//     `../../config` 都不存在），因此 `TRACE_BODY_ROOT=""`（或干脆不设）恰好落到同一禁用态。
//     这是**测试环境**的等价形态，不得据此推断生产 YAML 的行为。

const (
	// 必须是**非已知默认值**的强随机串：两者在既有中间件里都是 fail-closed
	// （`internal/controlplane/middleware/middleware.go:67-84` 与 `:89-107`）。
	assemblyJWTSecret     = "trace-body-assembly-test-secret-0f3a9c7d"
	assemblyInternalToken = "trace-body-assembly-internal-token-7b21e4"
	assemblyAdminUserID   = "u-assembly-admin"
	assemblyPlainUserID   = "u-assembly-plain"
)

// assemblyDB 建内存 sqlite 并 AutoMigrate 本文件用到的三张表。
//
//	call_traces —— 点读服务的数据源（定位键四列 + body_missing）
//	call_logs   —— **既有**归属反查（`LookupTraceSubject`，`usage_handler.go:202-219`）
//	user_roles  —— **既有** RBAC 判据（`Checker.buildActor` → `RoleRepo.GetByUserID`）
func assemblyDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}, &obsmodel.CallLog{}, &orgmodel.UserRole{}))
	return db
}

// assemblyEngine 起一次**真实** `router.Setup`。
//
// 必须在调用**之前**把所有环境变量设好：`Setup` 在构造期就把它们读进去了
// （`cpconfig.Load()` 读 `TRACE_BODY_ROOT`；`middleware.JWTAuth()` / `InternalAuth()`
// 在 Setup 时解析 JWT_SECRET / INTERNAL_TOKEN）。
func assemblyEngine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", assemblyJWTSecret)
	t.Setenv("INTERNAL_TOKEN", assemblyInternalToken)

	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // 不可达：Setup 对 Redis 是惰性的（只记日志）
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	return router.Setup(context.Background(), db, rdb)
}

// assemblyJWT 签一枚既有 `JWTAuth` 认得的 HS256 token（唯一凭据载体是 `user_id`）。
func assemblyJWT(t *testing.T, userID string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte(assemblyJWTSecret))
	require.NoError(t, err)
	return signed
}

// assemblySeedTraceRow 落一条「正文在归档」的 call_traces 行（四键非 NULL、body_missing=false）。
func assemblySeedTraceRow(t *testing.T, db *gorm.DB, requestID, dt, inst string, part, row int) {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", dt, time.UTC)
	require.NoError(t, err)
	p, r := part, row
	require.NoError(t, db.Create(&obsmodel.CallTrace{
		RequestID:   requestID,
		TS:          time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		BodyRefDT:   &day,
		BodyRefInst: &inst,
		BodyRefPart: &p,
		BodyRefRow:  &r,
		BodyMissing: false,
	}).Error)
}

// assemblySeedCallLog 落一条 call_logs 行 —— **既有**归属反查的数据源，缺它一律 404。
func assemblySeedCallLog(t *testing.T, db *gorm.DB, requestID, userID, keyType string) {
	t.Helper()
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: requestID,
		TS:        time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		KeyType:   keyType,
		UserID:    userID,
		Model:     "assembly-test-model",
	}).Error)
}

// assemblySeedRole 落一条 user_roles 行 —— **既有** RBAC 的数据源（Guard 只信服务端表）。
func assemblySeedRole(t *testing.T, db *gorm.DB, userID string, role orgmodel.RoleType) {
	t.Helper()
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID:    userID,
		Role:      role,
		ScopeType: orgmodel.ScopeSelf,
		ScopeID:   "",
		GrantedBy: "assembly-test",
	}).Error)
}

// assemblyWriteSpool 在真实 `<root>` 下按冻结包布局写一份 spool 明文（**不需要 zstd**）。
func assemblyWriteSpool(t *testing.T, root, dt, inst string, part int, line []byte) {
	t.Helper()
	path := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, append(append([]byte{}, line...), '\n'), 0o644))
}

// assemblySealProduct 用**真实 Sealer** 封一份产物（需要 C 版 zstd CLI；缺则子用例 SKIP）。
func assemblySealProduct(t *testing.T, root, dt, inst string, part int, line []byte) {
	t.Helper()
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd：真实封块产物的子用例需要 C 版 zstd CLI（本用例不在任何变异集合里）")
	}
	out, err := exec.Command(bin, "--version").Output()
	require.NoError(t, err)
	s := string(out)
	i := strings.Index(s, " v")
	require.GreaterOrEqual(t, i, 0, "无法解析 zstd 版本: %q", s)
	rest := s[i+2:]
	if j := strings.IndexAny(rest, ", \n"); j >= 0 {
		rest = rest[:j]
	}
	ver := strings.TrimSpace(rest)
	require.NotEmpty(t, ver)

	spool := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
	require.NoError(t, os.MkdirAll(filepath.Dir(spool), 0o755))
	require.NoError(t, os.WriteFile(spool, append(append([]byte{}, line...), '\n'), 0o644))

	sealer, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	require.NoError(t, err)
	require.NoError(t, sealer.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: 1,
	}), "封块失败（本机 zstd=%s）", ver)
}

// assemblyGet 发一次请求；返回状态码与响应体（管理面端点用 Bearer，内部端点用 X-Internal-Token）。
func assemblyGet(engine *gin.Engine, target, bearer string, headers map[string]string) (int, []byte) {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// ---------------------------------------------------------------------------
// ① 两态之一：启用 ⇒ 装配后的路由拿到归档正文（逐字节）
// ---------------------------------------------------------------------------

// TestTraceBodyRouterSetupEnabledServesArchivedBody 是 **E33 承接块第 ① 条**的启用态。
//
// 全链路：`router.Setup`（真实装配，`TRACE_BODY_ROOT` 指向真实 `<root>`）→
// 既有 `JWTAuth` → 既有 `guard(PermUsageViewAll)` → `TraceBodyHandler.TraceBody`
// （既有归属反查 `LookupTraceSubject` + 既有可见性 `rbac.CanViewPrompt`）→
// 服务层 → 冻结包 `Reader` → 产物 / spool。
//
// 断言：200 + 正文**逐字节**等于写入时那一行 + `Content-Length` 等于正文长度。
// 两个子用例分别走归档读取器的两条路径（未封块 spool / 已封块产物）。
func TestTraceBodyRouterSetupEnabledServesArchivedBody(t *testing.T) {
	const dt, inst, part, row = "2026-10-08", "blue", 0, 0

	t.Run("未封块spool", func(t *testing.T) {
		db := assemblyDB(t)
		root := t.TempDir()
		t.Setenv("TRACE_BODY_ROOT", root)

		line := []byte(`{"request_headers":{"x":"spool"},"request_body":{"msg":"装配后的路由"},"response_body":{"ok":true}}`)
		assemblyWriteSpool(t, root, dt, inst, part, line)
		assemblySeedTraceRow(t, db, "req-assembly-spool", dt, inst, part, row)
		assemblySeedCallLog(t, db, "req-assembly-spool", assemblyAdminUserID, "personal")
		assemblySeedRole(t, db, assemblyAdminUserID, orgmodel.RoleAdmin)

		engine := assemblyEngine(t, db)
		code, body := assemblyGet(engine,
			"/api/v1/trace/req-assembly-spool/body",
			assemblyJWT(t, assemblyAdminUserID), nil)

		require.Equal(t, http.StatusOK, code, "启用态必须 200，实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body),
			"响应体必须逐字节等于写入时的原字节（包壳 / 重新序列化都会立刻红）\nwant=%s\ngot =%s", line, body)
	})

	t.Run("封块产物", func(t *testing.T) {
		db := assemblyDB(t)
		root := t.TempDir()
		t.Setenv("TRACE_BODY_ROOT", root)

		line := []byte(`{"request_headers":{"x":"product"},"request_body":{"msg":"封块产物"},"response_body":{"ok":true}}`)
		assemblySealProduct(t, root, dt, inst, part, line)
		assemblySeedTraceRow(t, db, "req-assembly-product", dt, inst, part, row)
		assemblySeedCallLog(t, db, "req-assembly-product", assemblyAdminUserID, "personal")
		assemblySeedRole(t, db, assemblyAdminUserID, orgmodel.RoleAdmin)

		// 前置：产物确实存在（否则本子用例会静默退化到 spool 兜底）。
		_, statErr := os.Stat(ta.NewPaths(root).PartFile(ta.Ref{DT: dt, Inst: inst, Part: part}))
		require.NoError(t, statErr, "封块后产物必须存在（本子用例考的就是产物路径）")

		engine := assemblyEngine(t, db)
		code, body := assemblyGet(engine,
			"/api/v1/trace/req-assembly-product/body",
			assemblyJWT(t, assemblyAdminUserID), nil)

		require.Equal(t, http.StatusOK, code, "启用态（产物）必须 200，实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body),
			"响应体必须逐字节等于产物里的原字节\nwant=%s\ngot =%s", line, body)
	})
}

// ---------------------------------------------------------------------------
// ① 两态之二：禁用 ⇒ 503 trace_body_disabled（不是 500 / 404 / jsonb 回退）
// ---------------------------------------------------------------------------

// TestTraceBodyRouterSetupDisabledYields503 是 **E33 承接块第 ① 条**的禁用态。
//
// 布置：**不设** `TRACE_BODY_ROOT`（测试 cwd 下也没有任何 YAML），行带完整定位键
// ⇒ 元数据指向归档而读侧未配置 = 部署错误。
//
// 断言（三选一的排除法是承重的）：503 + `code=trace_body_disabled`，
// **不是** 500 `internal_error`（把部署错误伪装成存储故障）、
// **不是** 404（把部署错误伪装成「正文不存在」）、
// **也不是** 200 + jsonb（`fromJSONB` 会把空 jsonb 伪装成「正文就是空的」）。
func TestTraceBodyRouterSetupDisabledYields503(t *testing.T) {
	const dt, inst, part, row = "2026-10-08", "blue", 0, 0

	db := assemblyDB(t)
	t.Setenv("TRACE_BODY_ROOT", "") // 空串在此处等价于「未设置」（E6：viper 跳过空串）

	assemblySeedTraceRow(t, db, "req-assembly-disabled", dt, inst, part, row)
	assemblySeedCallLog(t, db, "req-assembly-disabled", assemblyAdminUserID, "personal")
	assemblySeedRole(t, db, assemblyAdminUserID, orgmodel.RoleAdmin)

	engine := assemblyEngine(t, db)
	code, body := assemblyGet(engine,
		"/api/v1/trace/req-assembly-disabled/body",
		assemblyJWT(t, assemblyAdminUserID), nil)

	require.Equal(t, http.StatusServiceUnavailable, code,
		"读侧禁用 + 行带定位键 ⇒ 必须 503（部署错误），实际 %d，响应体 %s", code, body)

	var detail obshandler.TraceBodyErrorResponse
	require.NoError(t, json.Unmarshal(body, &detail), "503 必须是既有错误形状 {\"error\":{…}}，实际 %s", body)
	require.Equal(t, "trace_body_disabled", detail.Error.Code)
	require.Equal(t, "unavailable", detail.Error.Type)
	require.False(t, detail.Error.BodyMissing, "禁用不是「正文从未落盘」⇒ body_missing 不得为 true")
}

// ---------------------------------------------------------------------------
// ② 权限门 PermUsageViewAll（装配后的真实路由）
// ---------------------------------------------------------------------------

// TestTraceBodyRouterSetupPermissionGate 是 **E33 承接块第 ② 条**。
//
// 三格（缺一不可 —— 只证「拒绝」证明不了门没被放宽，只证「放行」证明不了门没被绕过）：
//
//	无 token            ⇒ 401（既有 JWTAuth 拦下）
//	有 token但无该权限   ⇒ 403 `org.forbidden`（既有 guard 拦下，**不得更宽松**）
//	admin（有该权限）    ⇒ 200（门不是恒拒绝）
//
// 【工具位置】本用例用 spool 提供正文（**不需要 zstd**），因此它在本机无 zstd 时也必须全绿。
func TestTraceBodyRouterSetupPermissionGate(t *testing.T) {
	const dt, inst, part, row = "2026-10-08", "blue", 2, 0

	db := assemblyDB(t)
	root := t.TempDir()
	t.Setenv("TRACE_BODY_ROOT", root)

	line := []byte(`{"request_body":{"msg":"权限门"}}`)
	assemblyWriteSpool(t, root, dt, inst, part, line)
	assemblySeedTraceRow(t, db, "req-assembly-gate", dt, inst, part, row)
	assemblySeedCallLog(t, db, "req-assembly-gate", assemblyPlainUserID, "personal")
	assemblySeedRole(t, db, assemblyAdminUserID, orgmodel.RoleAdmin)
	assemblySeedRole(t, db, assemblyPlainUserID, orgmodel.RoleUser) // 有身份、无 PermUsageViewAll

	engine := assemblyEngine(t, db)
	const target = "/api/v1/trace/req-assembly-gate/body"

	t.Run("无token", func(t *testing.T) {
		code, body := assemblyGet(engine, target, "", nil)
		require.Equal(t, http.StatusUnauthorized, code,
			"无 token 必须被既有 JWTAuth 拦下（401），实际 %d，响应体 %s", code, body)
	})

	t.Run("有token但无该权限", func(t *testing.T) {
		code, body := assemblyGet(engine, target, assemblyJWT(t, assemblyPlainUserID), nil)
		require.Equal(t, http.StatusForbidden, code,
			"有 token 但无 PermUsageViewAll 必须被既有 guard 拒绝（403），实际 %d，响应体 %s", code, body)

		var detail struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		require.Equal(t, "org.forbidden", detail.Code, "拒绝必须来自既有 RBAC guard")
	})

	t.Run("admin有该权限", func(t *testing.T) {
		code, body := assemblyGet(engine, target, assemblyJWT(t, assemblyAdminUserID), nil)
		require.Equal(t, http.StatusOK, code, "平台 admin 必须放行（门不是恒拒绝），实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body), "admin 拿到的必须仍是正文原字节")
	})
}

// ---------------------------------------------------------------------------
// ③ 「不新建鉴权中间件」的行为侧
// ---------------------------------------------------------------------------

// TestTraceBodyRouterSetupReusesExistingAuthMiddleware 是 **E33 承接块第 ③ 条**的**行为侧**。
//
// 【本条不证明什么（必须逐字保留）】它**不**证明「不存在第二套凭据解析」（那是**不存在性**，
// 只有任务 5 的静态 CHECKS `不新建鉴权中间件` 能承重）。它只补行为侧：两条点读端点的
// 认证入口与既有中间件**完全一致**，没有多出任何一种凭据通道：
//
//	管理面端点：只有 `Authorization: Bearer <管理面 JWT>` 有效
//	            ⇒ `X-Internal-Token` 不能把它放行（否则就是第二套凭据解析）
//	内部端点：  走**既有** `InternalAuth`，接受 `X-Internal-Token` 与
//	            `Authorization: Bearer <内部 token>` 两条读取路径（`middleware.go:219-222`）
//	            ⇒ 一枚合法的**管理面 JWT** 不能把它放行（同上；JWT ≠ 内部 token）
func TestTraceBodyRouterSetupReusesExistingAuthMiddleware(t *testing.T) {
	const dt, inst, part, row = "2026-10-08", "blue", 1, 0

	db := assemblyDB(t)
	root := t.TempDir()
	t.Setenv("TRACE_BODY_ROOT", root)

	line := []byte(`{"request_body":{"msg":"既有中间件"}}`)
	assemblyWriteSpool(t, root, dt, inst, part, line)
	assemblySeedTraceRow(t, db, "req-assembly-auth", dt, inst, part, row)
	assemblySeedCallLog(t, db, "req-assembly-auth", assemblyAdminUserID, "personal")
	assemblySeedRole(t, db, assemblyAdminUserID, orgmodel.RoleAdmin)

	engine := assemblyEngine(t, db)
	adminJWT := assemblyJWT(t, assemblyAdminUserID)

	t.Run("管理面不认内部token", func(t *testing.T) {
		code, body := assemblyGet(engine, "/api/v1/trace/req-assembly-auth/body", "",
			map[string]string{"X-Internal-Token": assemblyInternalToken})
		require.Equal(t, http.StatusUnauthorized, code,
			"内部 token 不得放行管理面端点（那就是第二套凭据解析），实际 %d，响应体 %s", code, body)
	})

	t.Run("内部端点不认管理面JWT", func(t *testing.T) {
		code, body := assemblyGet(engine, "/internal/v1/trace-body?request_id=req-assembly-auth", adminJWT, nil)
		require.Equal(t, http.StatusForbidden, code,
			"管理面 JWT 不得放行内部端点，实际 %d，响应体 %s", code, body)
	})

	t.Run("内部端点认内部token", func(t *testing.T) {
		code, body := assemblyGet(engine, "/internal/v1/trace-body?request_id=req-assembly-auth", "",
			map[string]string{"X-Internal-Token": assemblyInternalToken})
		require.Equal(t, http.StatusOK, code, "既有 InternalAuth 通过后必须能取到正文，实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body), "内部端点返回的必须仍是正文原字节")
	})

	t.Run("内部端点也认Bearer携带的内部token", func(t *testing.T) {
		code, body := assemblyGet(engine, "/internal/v1/trace-body?request_id=req-assembly-auth", assemblyInternalToken, nil)
		require.Equal(t, http.StatusOK, code,
			"既有 InternalAuth 允许以 Authorization: Bearer 携带内部 token（middleware.go:219-222），实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body), "内部端点返回的必须仍是正文原字节")
	})

	t.Run("管理面认Bearer", func(t *testing.T) {
		code, body := assemblyGet(engine, "/api/v1/trace/req-assembly-auth/body", adminJWT, nil)
		require.Equal(t, http.StatusOK, code, "既有 JWTAuth + guard 通过后必须 200，实际 %d，响应体 %s", code, body)
		require.True(t, bytes.Equal(line, body))
	})
}
