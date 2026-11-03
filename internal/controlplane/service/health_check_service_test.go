package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// countingSnapshotter 计数型 snapshotRefresher：统计自动禁用/恢复触发了几次快照重建请求。
type countingSnapshotter struct {
	mu    sync.Mutex
	calls int
}

func (c *countingSnapshotter) ScheduleRefresh(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
}

func (c *countingSnapshotter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// healthCheckTestDDL 用 raw SQL 建表：避免 AutoMigrate 在 SQLite 上生成
// gen_random_uuid() 等 PG 专属默认值（与 tests/controlplane/handler 的做法一致）。
const healthCheckTestDDL = `
CREATE TABLE providers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	definition_id TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL DEFAULT '',
	endpoint TEXT NOT NULL DEFAULT '',
	encrypted_credentials TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	weight INTEGER NOT NULL DEFAULT 1,
	priority INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	test_model TEXT NOT NULL DEFAULT '',
	response_time INTEGER NOT NULL DEFAULT 0,
	balance REAL NOT NULL DEFAULT 0,
	remark TEXT NOT NULL DEFAULT '',
	max_retries INTEGER NOT NULL DEFAULT 3,
	timeout INTEGER NOT NULL DEFAULT 30,
	tag TEXT NOT NULL DEFAULT '',
	consecutive_failures INTEGER NOT NULL DEFAULT 0,
	last_check_error TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE models (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	provider_id TEXT NOT NULL,
	model_type TEXT NOT NULL DEFAULT 'chat',
	enabled INTEGER NOT NULL DEFAULT 1,
	status INTEGER NOT NULL DEFAULT 1,
	upstream_model_id TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	context_length INTEGER NOT NULL DEFAULT 0,
	max_output_tokens INTEGER NOT NULL DEFAULT 0,
	input_price REAL NOT NULL DEFAULT 0,
	output_price REAL NOT NULL DEFAULT 0,
	cache_read_price REAL,
	cache_write_price REAL,
	price_mode TEXT NOT NULL DEFAULT 'catalog',
	weight INTEGER NOT NULL DEFAULT 1,
	catalog_id TEXT,
	ctx_overridden INTEGER NOT NULL DEFAULT 0,
	max_output_overridden INTEGER NOT NULL DEFAULT 0,
	override_supports_video_input INTEGER,
	video_supported_resolutions TEXT NOT NULL DEFAULT '[]',
	video_supported_aspect_ratios TEXT NOT NULL DEFAULT '[]',
	video_min_duration_seconds INTEGER NOT NULL DEFAULT 0,
	video_max_duration_seconds INTEGER NOT NULL DEFAULT 0,
	video_supported_actions TEXT NOT NULL DEFAULT '[]',
	quota_rpm INTEGER,
	quota_max_concurrency INTEGER,
	consecutive_failures INTEGER NOT NULL DEFAULT 0,
	consecutive_recovery_successes INTEGER NOT NULL DEFAULT 0,
	last_check_error TEXT NOT NULL DEFAULT '',
	response_time INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE health_check_configs (
	id TEXT PRIMARY KEY,
	enabled INTEGER NOT NULL DEFAULT 0,
	interval_seconds INTEGER NOT NULL DEFAULT 300,
	failure_threshold INTEGER NOT NULL DEFAULT 3,
	failure_action TEXT NOT NULL DEFAULT 'auto_disable',
	check_providers INTEGER NOT NULL DEFAULT 1,
	check_models INTEGER NOT NULL DEFAULT 1,
	check_mode TEXT NOT NULL DEFAULT 'connectivity',
	timeout_seconds INTEGER NOT NULL DEFAULT 15,
	model_check_enabled INTEGER NOT NULL DEFAULT 1,
	model_check_interval_seconds INTEGER NOT NULL DEFAULT 300,
	model_check_failure_threshold INTEGER NOT NULL DEFAULT 3,
	group_check_enabled INTEGER NOT NULL DEFAULT 1,
	group_check_interval_seconds INTEGER NOT NULL DEFAULT 300,
	model_recovery_enabled INTEGER NOT NULL DEFAULT 1,
	model_recovery_success_threshold INTEGER NOT NULL DEFAULT 2,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE health_check_logs (
	id TEXT PRIMARY KEY,
	target_type TEXT NOT NULL,
	target_id TEXT NOT NULL,
	target_name TEXT NOT NULL DEFAULT '',
	provider_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
	provider_name TEXT NOT NULL DEFAULT '',
	success INTEGER NOT NULL,
	response_time_ms INTEGER NOT NULL DEFAULT 0,
	error_message TEXT DEFAULT '',
	checked_at DATETIME NOT NULL
);
`

type healthCheckTestEnv struct {
	svc *HealthCheckService
	db  *gorm.DB
	rdb *redis.Client
}

// newHealthCheckTestEnv 创建 SQLite 内存库 + miniredis 的 HealthCheckService 测试环境
func newHealthCheckTestEnv(t *testing.T, snapshotter snapshotRefresher) *healthCheckTestEnv {
	t.Helper()
	crypto.InitForTesting()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err, "open sqlite")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// 单连接内存库：确保后台检测 goroutine 与主测试线程共享同一个库
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec(healthCheckTestDDL).Error, "create tables")

	mr, err := miniredis.Run()
	require.NoError(t, err, "start miniredis")
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	svc := NewHealthCheckService(repository.NewHealthCheckRepo(db), repository.NewProviderRepo(db), repository.NewModelRepo(db), rdb)
	if snapshotter != nil {
		svc.snapshotter = snapshotter
	}
	return &healthCheckTestEnv{svc: svc, db: db, rdb: rdb}
}

// seedConfig 写入健康检测配置（单例）
func (e *healthCheckTestEnv) seedConfig(t *testing.T, failureThreshold, recoveryThreshold int) *model.HealthCheckConfig {
	t.Helper()
	cfg := &model.HealthCheckConfig{
		ID:                            uuid.New(),
		Enabled:                       false, // 不启动后台调度
		ModelCheckEnabled:             true,
		ModelCheckIntervalSeconds:     300,
		ModelCheckFailureThreshold:    failureThreshold,
		GroupCheckEnabled:             true,
		GroupCheckIntervalSeconds:     300,
		ModelRecoveryEnabled:          true,
		ModelRecoverySuccessThreshold: recoveryThreshold,
	}
	require.NoError(t, e.db.Create(cfg).Error, "seed config")
	return cfg
}

// seedProvider 写入渠道（凭证用测试 DEK 加密）
func (e *healthCheckTestEnv) seedProvider(t *testing.T, name, endpoint string, status, failures int) *model.Provider {
	t.Helper()
	creds, err := crypto.Encrypt(`{"api_key":"test-key"}`)
	require.NoError(t, err, "encrypt credentials")
	enabled := status == model.ProviderStatusEnabled
	p := &model.Provider{
		ID:                   uuid.New(),
		Name:                 name,
		DefinitionID:         "", // 空 → 走 OpenAI 兼容探针
		Type:                 "openai",
		Endpoint:             endpoint,
		EncryptedCredentials: creds,
		Enabled:              enabled,
		Weight:               1,
		Status:               status,
		ConsecutiveFailures:  failures,
	}
	require.NoError(t, e.db.Create(p).Error, "seed provider")
	return p
}

// seedModel 写入模型（status=3 时 enabled=false）
func (e *healthCheckTestEnv) seedModel(t *testing.T, name string, providerID uuid.UUID, status, failures, recoverySuccesses int) *model.Model {
	t.Helper()
	enabled := status == model.ModelStatusEnabled
	m := &model.Model{
		ID:                           uuid.New(),
		Name:                         name,
		ProviderID:                   providerID,
		ModelType:                    "chat",
		Enabled:                      &enabled,
		Status:                       status,
		ConsecutiveFailures:          failures,
		ConsecutiveRecoverySuccesses: recoverySuccesses,
	}
	require.NoError(t, e.db.Omit("Provider", "Catalog").Create(m).Error, "seed model")
	return m
}

func (e *healthCheckTestEnv) reloadModel(t *testing.T, id uuid.UUID) *model.Model {
	t.Helper()
	var m model.Model
	require.NoError(t, e.db.First(&m, "id = ?", id).Error)
	return &m
}

func (e *healthCheckTestEnv) reloadProvider(t *testing.T, id uuid.UUID) *model.Provider {
	t.Helper()
	var p model.Provider
	require.NoError(t, e.db.First(&p, "id = ?", id).Error)
	return &p
}

func (e *healthCheckTestEnv) logCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, e.db.Model(&model.HealthCheckLog{}).Count(&n).Error)
	return n
}

// newProbeServer 起一个固定返回 status 的假上游（模拟 OpenAI 兼容端点）
func newProbeServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestClassifyProbeFailure 失败分类表驱动测试：
// 覆盖生产事故里出现的 404 / 429 / 503 / 超时，以及凭证类确定性错误与无法归类的兜底。
func TestClassifyProbeFailure(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   probeFailureKind
	}{
		{"404 模型不存在", "HTTP 404: {\"error\":{\"message\":\"Model not exist.\"}}", probeFailureDeterministic},
		{"渠道转发检测 404（带前缀）", "model forward HTTP 404: Model not exist.", probeFailureDeterministic},
		{"401 未授权", "HTTP 401: Unauthorized", probeFailureDeterministic},
		{"403 拒绝访问", "HTTP 403: Model access denied", probeFailureDeterministic},
		{"400 参数错误", "HTTP 400: invalid model identifier", probeFailureDeterministic},
		{"429 配额耗尽", "HTTP 429: token-plan quota exhausted", probeFailureTransient},
		{"502 网关错误", "HTTP 502: <html>Bad Gateway</html>", probeFailureTransient},
		{"503 服务不可用", "HTTP 503: Service temporarily unavailable", probeFailureTransient},
		{"500 内部错误", "HTTP 500: internal error", probeFailureTransient},
		{"408 请求超时", "HTTP 408: request timeout", probeFailureTransient},
		{"无状态码超时", "请求失败: context deadline exceeded", probeFailureTransient},
		{"连接被拒", "connection failed: dial tcp 10.0.0.1:443: connect: connection refused", probeFailureTransient},
		{"上游限流异常", "api error ThrottlingException: Rate exceeded", probeFailureTransient},
		{"凭证解密失败", "解密凭证失败", probeFailureDeterministic},
		{"凭证解密失败（英文）", "decrypt credentials failed", probeFailureDeterministic},
		{"渠道不存在", "渠道不存在", probeFailureDeterministic},
		{"AWS 配置加载失败", "AWS 配置加载失败: failed to get credentials", probeFailureDeterministic},
		{"AWS 参数校验失败", "ValidationException: The provided model identifier is invalid.", probeFailureDeterministic},
		{"空错误信息（保守）", "", probeFailureTransient},
		{"无法归类的错误（保守）", "something weird happened", probeFailureTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyProbeFailure(tc.errMsg), "errMsg=%q", tc.errMsg)
		})
	}
}

// TestProbeHTTPStatusCode 验证状态码提取（只认探针自己写入的 "HTTP <code>" 形态）
func TestProbeHTTPStatusCode(t *testing.T) {
	cases := []struct {
		in     string
		code   int
		wantOK bool
	}{
		{"HTTP 404: not found", 404, true},
		{"model forward HTTP 429: quota", 429, true},
		{"azure chat probe: HTTP 503: unavailable", 503, true},
		{"http 500: lower case", 500, true},
		{"HTTP 99: invalid", 0, false},
		{"HTTP 999: invalid", 0, false},
		{"HTTP abc: invalid", 0, false},
		{"no status here", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		code, ok := probeHTTPStatusCode(tc.in)
		assert.Equalf(t, tc.wantOK, ok, "input=%q", tc.in)
		if tc.wantOK {
			assert.Equalf(t, tc.code, code, "input=%q", tc.in)
		}
	}
}

// TestTimelineCacheKeyVersioned 验证缓存 key 内嵌版本号（版本变化即整体失效）
func TestTimelineCacheKeyVersioned(t *testing.T) {
	assert.NotEqual(t, timelineCacheKey(1, "24h", "gpt"), timelineCacheKey(2, "24h", "gpt"))
	assert.Equal(t, "health:timeline:v3:24h:", timelineCacheKey(3, "24h", ""))
	assert.Equal(t, "health:group-timeline:v4:7d:gpt", groupTimelineCacheKey(4, "7d", "gpt"))
}

// redisCommandRecorder 记录发往 Redis 的命令名，用于断言失效路径不再使用 SCAN
type redisCommandRecorder struct {
	mu   sync.Mutex
	cmds []string
}

func (h *redisCommandRecorder) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisCommandRecorder) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		h.cmds = append(h.cmds, strings.ToLower(cmd.Name()))
		h.mu.Unlock()
		return next(ctx, cmd)
	}
}

func (h *redisCommandRecorder) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *redisCommandRecorder) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.cmds...)
}

// TestInvalidateTimelineCacheUsesVersionInsteadOfScan 回归测试（缺陷 3）：
// 失效必须退化为 O(1) 的版本号 INCR，不再 SCAN 整个 keyspace + 逐 key DEL。
func TestInvalidateTimelineCacheUsesVersionInsteadOfScan(t *testing.T) {
	env := newHealthCheckTestEnv(t, nil)
	rec := &redisCommandRecorder{}
	env.rdb.AddHook(rec)

	require.Equal(t, int64(0), env.svc.timelineCacheVersion(timelineCacheVersionKey), "初始无版本号按 0 处理")

	env.svc.invalidateTimelineCache()
	assert.Equal(t, int64(1), env.svc.timelineCacheVersion(timelineCacheVersionKey))
	env.svc.invalidateTimelineCache()
	assert.Equal(t, int64(2), env.svc.timelineCacheVersion(timelineCacheVersionKey))
	env.svc.invalidateGroupTimelineCache()
	assert.Equal(t, int64(1), env.svc.timelineCacheVersion(groupTimelineCacheVersionKey))

	cmds := rec.commands()
	assert.Contains(t, cmds, "incr", "失效应通过 INCR 版本号实现")
	assert.NotContains(t, cmds, "scan", "失效不允许再使用 SCAN（生产上必然 context deadline exceeded）")
	assert.NotContains(t, cmds, "del", "失效不再逐 key DEL")
}

// TestHandleModelCheckSuccessResetsFailuresAndRecovers 验证「成功即归零 + 自动恢复」语义保留
func TestHandleModelCheckSuccessResetsFailuresAndRecovers(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusAutoDisabled, 24642, 1)

	recovered := env.svc.handleModelCheckSuccess(m, 12, "", true, 2, context.Background())

	assert.True(t, recovered, "达到恢复阈值应自动恢复")
	got := env.reloadModel(t, m.ID)
	require.NotNil(t, got.Enabled)
	assert.True(t, *got.Enabled)
	assert.Equal(t, model.ModelStatusEnabled, got.Status)
	assert.Equal(t, 0, got.ConsecutiveFailures, "成功必须把连续失败清零")
	assert.Equal(t, 0, got.ConsecutiveRecoverySuccesses)
	assert.Equal(t, 1, snap.count(), "自动恢复应请求一次快照重建")
}

// TestHandleModelCheckSuccessBelowRecoveryThreshold 未达恢复阈值只累加成功计数
func TestHandleModelCheckSuccessBelowRecoveryThreshold(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusAutoDisabled, 0, 0)

	recovered := env.svc.handleModelCheckSuccess(m, 8, "", true, 2, context.Background())

	assert.False(t, recovered)
	got := env.reloadModel(t, m.ID)
	assert.Equal(t, model.ModelStatusAutoDisabled, got.Status)
	assert.Equal(t, 1, got.ConsecutiveRecoverySuccesses)
	assert.Equal(t, 0, snap.count(), "未达阈值不应请求快照重建")
}

// TestHandleModelCheckSuccessRecoveryDisabled 恢复开关关闭时不恢复（保持既有行为）
func TestHandleModelCheckSuccessRecoveryDisabled(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusAutoDisabled, 5, 1)

	recovered := env.svc.handleModelCheckSuccess(m, 8, "", false, 2, context.Background())

	assert.False(t, recovered)
	got := env.reloadModel(t, m.ID)
	assert.Equal(t, model.ModelStatusAutoDisabled, got.Status)
	assert.Equal(t, 1, got.ConsecutiveRecoverySuccesses, "关闭恢复时不累加恢复计数")
	assert.Equal(t, 0, got.ConsecutiveFailures, "成功仍应清零连续失败")
	assert.Equal(t, 0, snap.count())
}

// TestHandleModelCheckFailureAutoDisabledIsIdempotent 缺陷 2 回归：
// 已自动禁用（status=3）的模型再次失败时不得重复累加计数、不得重复禁用、不得再次触发快照。
func TestHandleModelCheckFailureAutoDisabledIsIdempotent(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
	}{
		{"404 确定性失败", "HTTP 404: Model not exist."},
		{"429 临时性失败", "HTTP 429: token-plan quota exhausted"},
		{"503 临时性失败", "HTTP 503: Service temporarily unavailable"},
		{"超时", "请求失败: context deadline exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &countingSnapshotter{}
			env := newHealthCheckTestEnv(t, snap)
			p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
			m := env.seedModel(t, "m1", p.ID, model.ModelStatusAutoDisabled, 24642, 1)

			disabled, _ := env.svc.handleModelCheckFailure(m, 5, tc.errMsg, 3, context.Background())

			assert.False(t, disabled, "已自动禁用对象不应再次触发禁用")
			got := env.reloadModel(t, m.ID)
			assert.Equal(t, 24642, got.ConsecutiveFailures, "不得重复累加 consecutive_failures")
			assert.Equal(t, model.ModelStatusAutoDisabled, got.Status)
			require.NotNil(t, got.Enabled)
			assert.False(t, *got.Enabled)
			assert.Equal(t, 0, got.ConsecutiveRecoverySuccesses, "失败应清零恢复成功计数")
			assert.Equal(t, 0, snap.count(), "不得再次触发快照重建")
			assert.Equal(t, int64(1), env.logCount(t), "检测日志仍要记录（可观测性不受影响）")
		})
	}
}

// TestHandleModelCheckFailureTransientKeepsModelEnabled 临时性失败不自动禁用
func TestHandleModelCheckFailureTransientKeepsModelEnabled(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusEnabled, 0, 0)

	disabled, kind := env.svc.handleModelCheckFailure(m, 5, "HTTP 503: Service temporarily unavailable", 3, context.Background())

	assert.False(t, disabled)
	assert.Equal(t, probeFailureTransient, kind)
	got := env.reloadModel(t, m.ID)
	assert.Equal(t, model.ModelStatusEnabled, got.Status)
	require.NotNil(t, got.Enabled)
	assert.True(t, *got.Enabled)
	assert.Equal(t, 0, got.ConsecutiveFailures, "临时性失败不计入连续失败，避免把可用模型误杀")
	assert.Equal(t, 0, snap.count())
}

// TestHandleModelCheckFailureDeterministicDisablesAtThreshold 确定性失败按阈值自动禁用
func TestHandleModelCheckFailureDeterministicDisablesAtThreshold(t *testing.T) {
	t.Run("未达阈值", func(t *testing.T) {
		snap := &countingSnapshotter{}
		env := newHealthCheckTestEnv(t, snap)
		p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
		m := env.seedModel(t, "m1", p.ID, model.ModelStatusEnabled, 1, 0)

		disabled, kind := env.svc.handleModelCheckFailure(m, 5, "HTTP 404: Model not exist.", 3, context.Background())

		assert.False(t, disabled)
		assert.Equal(t, probeFailureDeterministic, kind)
		got := env.reloadModel(t, m.ID)
		assert.Equal(t, 2, got.ConsecutiveFailures)
		assert.Equal(t, model.ModelStatusEnabled, got.Status)
		assert.Equal(t, 0, snap.count())
	})

	t.Run("达到阈值", func(t *testing.T) {
		snap := &countingSnapshotter{}
		env := newHealthCheckTestEnv(t, snap)
		p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
		m := env.seedModel(t, "m1", p.ID, model.ModelStatusEnabled, 2, 0)

		disabled, kind := env.svc.handleModelCheckFailure(m, 5, "HTTP 404: Model not exist.", 3, context.Background())

		assert.True(t, disabled)
		assert.Equal(t, probeFailureDeterministic, kind)
		got := env.reloadModel(t, m.ID)
		assert.Equal(t, 3, got.ConsecutiveFailures)
		assert.Equal(t, model.ModelStatusAutoDisabled, got.Status)
		require.NotNil(t, got.Enabled)
		assert.False(t, *got.Enabled)
		assert.Equal(t, 1, snap.count(), "自动禁用应请求一次快照重建")
	})
}

// TestRunBackgroundModelCheckAutoDisabledModelsDoNotChurn 缺陷 2 端到端回归：
// 一轮检测里 72 个已自动禁用（status=3）的模型全部失败，
// 不得累加计数、不得再次禁用、不得触发任何快照重建。
func TestRunBackgroundModelCheckAutoDisabledModelsDoNotChurn(t *testing.T) {
	srv := newProbeServer(t, http.StatusNotFound, `{"error":{"message":"Model not exist."}}`)
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	env.seedConfig(t, 3, 2)
	p := env.seedProvider(t, "p1", srv.URL, model.ProviderStatusEnabled, 0)
	const models = 72
	for i := 0; i < models; i++ {
		env.seedModel(t, fmt.Sprintf("m%02d", i), p.ID, model.ModelStatusAutoDisabled, 24642, 0)
	}

	env.svc.runBackgroundModelCheck(context.Background())

	assert.Equal(t, 0, snap.count(), "已自动禁用模型失败不得触发快照重建")
	var changed int64
	require.NoError(t, env.db.Model(&model.Model{}).Where("consecutive_failures <> ?", 24642).Count(&changed).Error)
	assert.Zero(t, changed, "不得重复累加 consecutive_failures")
	assert.Equal(t, int64(models), env.logCount(t), "每轮仍应记录检测日志")
}

// TestRunBackgroundModelCheckBurstDisablesCoalesceSnapshotRebuild 缺陷 1 端到端回归：
// 一轮检测里 72 个模型同时达到阈值被自动禁用，最多只触发 2 次全量快照重建。
func TestRunBackgroundModelCheckBurstDisablesCoalesceSnapshotRebuild(t *testing.T) {
	srv := newProbeServer(t, http.StatusNotFound, `{"error":{"message":"Model not exist."}}`)
	repo := &countingModelRepo{delay: 300 * time.Millisecond} // 模拟全量重建耗时
	refresher, _ := newTestSnapshotRefresher(t, repo)
	refresher.minInterval = 0 // 只验证单飞合并
	env := newHealthCheckTestEnv(t, nil)
	env.svc.SetSnapshotRefresher(refresher)
	env.seedConfig(t, 3, 2)
	p := env.seedProvider(t, "p1", srv.URL, model.ProviderStatusEnabled, 0)
	const models = 72
	for i := 0; i < models; i++ {
		// 已经是 2 次连续失败：本轮第 3 次确定性失败即触发自动禁用
		env.seedModel(t, fmt.Sprintf("m%02d", i), p.ID, model.ModelStatusEnabled, 2, 0)
	}

	env.svc.runBackgroundModelCheck(context.Background())
	require.True(t, refresher.WaitIdle(15*time.Second), "快照重建队列应在超时前空闲")

	var autoDisabled int64
	require.NoError(t, env.db.Model(&model.Model{}).Where("status = ?", model.ModelStatusAutoDisabled).Count(&autoDisabled).Error)
	assert.Equal(t, int64(models), autoDisabled, "72 个模型都应被自动禁用")

	var notExactlyOnce int64
	require.NoError(t, env.db.Model(&model.Model{}).Where("consecutive_failures <> ?", 3).Count(&notExactlyOnce).Error)
	assert.Zero(t, notExactlyOnce, "每个模型本轮只应累加一次")

	assert.GreaterOrEqual(t, repo.callCount(), int64(1))
	assert.LessOrEqual(t, repo.callCount(), int64(2), "72 次自动禁用最多触发 2 次全量快照重建")
	assert.LessOrEqual(t, refresher.AutoRebuildCount(), int64(2))
}

// TestTransientFailureRepeatedDoesNotDisableAndResetsOnSuccess 覆盖「429 反复失败 + 恢复」：
// 临时性失败永远不触发自动禁用，成功后连续失败计数保持 0。
func TestTransientFailureRepeatedDoesNotDisableAndResetsOnSuccess(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusEnabled, 0, 0)

	// 连续 5 轮 429（配额耗尽）：模型保持可用
	for i := 0; i < 5; i++ {
		disabled, kind := env.svc.handleModelCheckFailure(m, 5, "HTTP 429: token-plan quota exhausted", 3, context.Background())
		require.False(t, disabled)
		require.Equal(t, probeFailureTransient, kind)
		m = env.reloadModel(t, m.ID)
	}
	got := env.reloadModel(t, m.ID)
	assert.Equal(t, model.ModelStatusEnabled, got.Status)
	require.NotNil(t, got.Enabled)
	assert.True(t, *got.Enabled)
	assert.Equal(t, 0, got.ConsecutiveFailures)

	// 上游恢复：成功不触发快照（模型本来就可用）
	assert.False(t, env.svc.handleModelCheckSuccess(m, 5, "", true, 2, context.Background()))
	got = env.reloadModel(t, m.ID)
	assert.Equal(t, 0, got.ConsecutiveFailures)
	assert.Equal(t, model.ModelStatusEnabled, got.Status)
	assert.Equal(t, 0, snap.count())
}

// TestAutoDisabledModelRecoversAfterTransientFailures 验证幂等短路不会堵死恢复路径：
// 自动禁用模型在 429/503 期间不累加计数，上游恢复后按成功阈值自动恢复。
func TestAutoDisabledModelRecoversAfterTransientFailures(t *testing.T) {
	snap := &countingSnapshotter{}
	env := newHealthCheckTestEnv(t, snap)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusAutoDisabled, 3, 0)

	// 连续 3 轮临时性失败：保持禁用且计数不变
	for i := 0; i < 3; i++ {
		disabled, _ := env.svc.handleModelCheckFailure(m, 5, "HTTP 503: Service temporarily unavailable", 3, context.Background())
		require.False(t, disabled)
		m = env.reloadModel(t, m.ID)
	}
	got := env.reloadModel(t, m.ID)
	require.Equal(t, model.ModelStatusAutoDisabled, got.Status)
	assert.Equal(t, 3, got.ConsecutiveFailures, "禁用期间失败不得继续累加")

	// 第 1 次成功：仅累加恢复计数，不恢复
	m = env.reloadModel(t, m.ID)
	require.False(t, env.svc.handleModelCheckSuccess(m, 5, "", true, 2, context.Background()))

	// 第 2 次成功：达到恢复阈值，自动恢复并请求一次快照重建
	m = env.reloadModel(t, m.ID)
	require.True(t, env.svc.handleModelCheckSuccess(m, 5, "", true, 2, context.Background()))
	got = env.reloadModel(t, m.ID)
	assert.Equal(t, model.ModelStatusEnabled, got.Status)
	require.NotNil(t, got.Enabled)
	assert.True(t, *got.Enabled)
	assert.Equal(t, 0, got.ConsecutiveFailures)
	assert.Equal(t, 1, snap.count())
}

// TestScheduleSnapshotRefreshNilSnapshotter 未注入 snapshotter 时自动禁用不应 panic
func TestScheduleSnapshotRefreshNilSnapshotter(t *testing.T) {
	env := newHealthCheckTestEnv(t, nil)
	p := env.seedProvider(t, "p1", "http://127.0.0.1:1", model.ProviderStatusEnabled, 0)
	m := env.seedModel(t, "m1", p.ID, model.ModelStatusEnabled, 2, 0)

	assert.NotPanics(t, func() {
		disabled, _ := env.svc.handleModelCheckFailure(m, 3, "HTTP 404: Model not exist.", 3, context.Background())
		assert.True(t, disabled)
	})
}
