package observability_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// 测试目的：钉住 call_type 的**落库接线**（A4 只做了纯函数 DeriveCallType，A11 负责让它真正落库）。
// 断言三件事：
//  1. 采集侧 NewUsageEvent 用「入站方法 + 入站路径」算出 call_type（不是采信 handler 自报的值）；
//  2. 该值经 UsageEvent.ToCallLog → call_logs 持久化后，列里读回来的就是它；
//  3. **未登记端点/方法不匹配写空串**（''），既不是 NULL 也不是 "unknown" 之类的兜底值。

// TestUsageEvent_ToCallLog_CarriesCallType
//
// 测什么：UsageEvent → CallLog 的字段映射是否带上 CallType。
// 怎么测：直接构造 UsageEvent 调 ToCallLog。
// 期望：CallLog.CallType 与事件一致（漏掉这行映射时，采集侧算得再对也落不了库）。
func TestUsageEvent_ToCallLog_CarriesCallType(t *testing.T) {
	log, err := gwobs.UsageEvent{RequestID: "r1", CallType: "video.asset.CreateAssetGroup"}.ToCallLog()
	require.NoError(t, err)
	require.Equal(t, "video.asset.CreateAssetGroup", log.CallType)
}

// TestCollector_NewUsageEvent_DerivesCallType
//
// 测什么：call_type 由采集侧从真实请求的 method + URL.Path 归一化得出。
// 怎么测：gin 测试上下文里构造请求，调 NewUsageEvent，比对 CallType。
// 期望：已登记端点得到约定取值；未登记路径与方法不匹配一律空串（不猜测、不写兜底值）。
func TestCollector_NewUsageEvent_DerivesCallType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 恰好填满列宽的最长素材 Action：64 - len("video.asset.") = 52
	maxAssetAction := strings.Repeat("A", callTypeColumnWidth-len(videoAssetPrefix))

	tests := []struct {
		name        string
		description string
		method      string
		path        string
		want        string
	}{
		{
			name: "chat", description: "POST /v1/chat/completions 落库值为 chat",
			method: http.MethodPost, path: "/v1/chat/completions", want: "chat",
		},
		{
			name: "Seedance 创建任务", description: "POST /v1/contents/generations/tasks 落库值为 video.create",
			method: http.MethodPost, path: "/v1/contents/generations/tasks", want: "video.create",
		},
		{
			name: "Seedance 查询任务", description: "同一前缀 + task_id 的 GET 落库值为 video.get（方法参与判定）",
			method: http.MethodGet, path: "/v1/contents/generations/tasks/cgt-1", want: "video.get",
		},
		{
			name: "素材动作", description: "POST /v1/assets/{Action} 落库值为 video.asset.{Action}",
			method: http.MethodPost, path: "/v1/assets/CreateAssetGroup", want: "video.asset.CreateAssetGroup",
		},
		{
			name: "素材动作刚好 64 字节", description: "拼接后正好等于列宽，应写入具体 Action 而不是桶值",
			method: http.MethodPost, path: "/v1/assets/" + maxAssetAction, want: videoAssetPrefix + maxAssetAction,
		},
		{
			name: "素材动作超长", description: "拼接后 65 字节 > varchar(64)，落库前就退回桶值 video.asset",
			method: http.MethodPost, path: "/v1/assets/" + maxAssetAction + "B", want: "video.asset",
		},
		{
			name: "视频提交", description: "POST /v1/videos 落库值为 video.submit",
			method: http.MethodPost, path: "/v1/videos", want: "video.submit",
		},
		{
			name: "未登记端点", description: "未登记路径落库值为空串，不得写成 unknown 之类的兜底值",
			method: http.MethodPost, path: "/v1/not-registered", want: "",
		},
		{
			name: "方法不匹配", description: "路径对但方法不对（GET /v1/chat/completions 不命中路由）落库值为空串",
			method: http.MethodGet, path: "/v1/chat/completions", want: "",
		},
	}

	collector := gwobs.NewCollector(gwobs.CollectorConfig{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(tt.method, tt.path, nil)

			event := collector.NewUsageEvent(c)
			require.Equal(t, tt.want, event.CallType, tt.description)
			// 顺带钉住 inbound_endpoint 仍是原始入站路径（call_type 的取值来源可复核）
			require.Equal(t, tt.path, event.InboundEndpoint, "inbound_endpoint 应保持原始入站路径")
		})
	}
}

// TestCallLog_CallTypePersistedToColumn
//
// 测什么：call_type 是否真的写进 call_logs 的**列**（而不仅是内存里的字段），
// 以及空串语义是否按「” ≠ NULL ≠ "unknown"」落库。
// 怎么测：sqlite 内存库 AutoMigrate 出 call_logs → 接上真实 CallLogRepository →
// 通过真实批量写入链路 Emit 事件（与生产同一条路）→ Eventually 等全部行落库 →
// 用裸 SQL 读回 call_type 与 `call_type IS NULL`（GORM 扫进 string 时 NULL 与 ” 无法区分，
// 必须用 IS NULL 才能证明空串语义）。
// 期望：已登记端点读回约定取值；未登记端点/方法不匹配读回 ” 且 IS NULL = false。
func TestCallLog_CallTypePersistedToColumn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// DSN 带纳秒后缀：sqlite 的 cache=shared 内存库在同一进程内**按名字复用**，固定名字会让
	// `-count>1` 的重复执行串到同一张表（并撞上上一轮 writer 持有的表锁）。
	dsn := fmt.Sprintf("file:call_type_persist_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))

	cfg := gwobs.DefaultCollectorConfig()
	cfg.Enabled = true
	cfg.BufferSize = 16
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	writerCtx, cancel := context.WithCancel(context.Background())
	collector.StartWriter(writerCtx, obsrepo.NewCallLogRepository(db))
	// 等 writer goroutine 真正退出再结束用例，避免它带着本轮库句柄阻塞下一轮（表锁）
	t.Cleanup(func() {
		cancel()
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer waitCancel()
		_ = collector.Wait(waitCtx)
	})

	maxAssetAction := strings.Repeat("A", callTypeColumnWidth-len(videoAssetPrefix))

	tests := []struct {
		name        string
		description string
		method      string
		path        string
		want        string
	}{
		{
			name: "chat 落列", description: "POST /v1/chat/completions 落库后 call_type 列 = chat",
			method: http.MethodPost, path: "/v1/chat/completions", want: "chat",
		},
		{
			name: "视频创建落列", description: "POST /v1/contents/generations/tasks 落库后 call_type 列 = video.create",
			method: http.MethodPost, path: "/v1/contents/generations/tasks", want: "video.create",
		},
		{
			name: "素材动作落列", description: "POST /v1/assets/{Action} 落库后 call_type 列 = video.asset.{Action}",
			method: http.MethodPost, path: "/v1/assets/CreateAssetGroup", want: "video.asset.CreateAssetGroup",
		},
		{
			name: "素材动作 64 字节落列", description: "varchar(64) 边界值应能整行写入且不被截断",
			method: http.MethodPost, path: "/v1/assets/" + maxAssetAction, want: videoAssetPrefix + maxAssetAction,
		},
		{
			name: "超长素材动作落列", description: "超过列宽的 Action 退回桶值，保证整行 call_logs 仍可写入",
			method: http.MethodPost, path: "/v1/assets/" + maxAssetAction + "B", want: "video.asset",
		},
		{
			name: "未登记端点落列", description: "未登记路径落库为**空串**（IS NULL=false），不得写成 unknown",
			method: http.MethodPost, path: "/v1/not-registered", want: "",
		},
		{
			name: "方法不匹配落列", description: "路径对方法不对同样落空串，不得猜成 chat",
			method: http.MethodGet, path: "/v1/chat/completions", want: "",
		},
	}

	// 先全部投递，再等落库——避免边投边断言引入的时序耦合
	requestIDs := make([]string, len(tests))
	for i, tt := range tests {
		requestIDs[i] = fmt.Sprintf("call-type-persist-%d", i)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(tt.method, tt.path, nil)
		c.Set("request_id", requestIDs[i])
		c.Set("key_type", "personal")
		c.Set("model", "some-group")

		collector.Emit(collector.NewUsageEvent(c))
	}

	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id IN ?", requestIDs).Count(&count).Error
		return count == int64(len(tests))
	}, 3*time.Second, 10*time.Millisecond, "call_logs 未能写满 %d 行", len(tests))

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				CallType string
				IsNull   int
			}
			res := db.Raw(
				`SELECT COALESCE(call_type, '') AS call_type, CASE WHEN call_type IS NULL THEN 1 ELSE 0 END AS is_null
				   FROM call_logs WHERE request_id = ?`, requestIDs[i],
			).Scan(&got)
			require.NoError(t, res.Error)
			require.Equal(t, int64(1), res.RowsAffected, "应恰好读回一行 call_logs")

			require.Equal(t, tt.want, got.CallType, tt.description)
			// 空串语义：未识别端点必须落 ''（列非 NULL），这样"已采集但不识别"与"迁移前未回填(NULL)"可区分
			require.Equal(t, 0, got.IsNull, "call_type 必须落 '' 而不是 NULL")
			require.NotEqual(t, "unknown", got.CallType, "不得写 unknown 之类的兜底值")
			// 列宽保护：任何落库值都不超过 varchar(64)（按字节判断比 varchar 的字符计数更严格）
			require.LessOrEqual(t, len(got.CallType), callTypeColumnWidth,
				"落库值超列宽会让整行 call_logs INSERT 失败")
		})
	}
}
