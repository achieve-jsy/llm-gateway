package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ReadinessHandler 返回控制面的**就绪**探针（依赖检查），与 /healthz（进程存活）区分：
//   - /healthz：只证明 HTTP 服务能响应，进程活着；
//   - /readyz：额外 ping Postgres 与 Redis，证明「能不能读库、能不能连缓存」；
//   - /readyz?deep=1：再校验 api_keys 里存在可用 Key（切流门禁用）。
//
// 存在的理由：网关鉴权链路是 gateway -> CP /internal/v1/auth/verify-key -> PG api_keys。
// 蓝绿切流门禁此前只看网关 /ready 与 CP /healthz，于是「CP 进程活着但读不了库」的新颜色
// 也能通过健康检查被切上流量，用户侧则表现为 API Key 全部报错（503 auth_service_unavailable
// 或 401 invalid_api_key）。
//
// deep 模式的存在理由：单纯 ping 通不能排除「连到了另一个空库/错库」——那种情况下
// verify 会正常返回 key_not_found，冒烟看起来是通的，但所有真实 Key 都会 401。
// 因此切流门禁额外要求「库里至少有一个可用 Key」。
func ReadinessHandler(db *gorm.DB, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		checks := gin.H{}
		ready := true

		if err := pingPostgres(ctx, db); err != nil {
			checks["postgres"] = "error: " + err.Error()
			ready = false
		} else {
			checks["postgres"] = "ok"
		}

		if rdb == nil {
			// 未配置 Redis 的部署形态（例如纯管理面单元测试）不算故障，但要显式标注
			checks["redis"] = "disabled"
		} else if err := rdb.Ping(ctx).Err(); err != nil {
			checks["redis"] = "error: " + err.Error()
			ready = false
		} else {
			checks["redis"] = "ok"
		}

		if c.Query("deep") == "1" && ready {
			n, err := countUsableKeys(ctx, db)
			switch {
			case err != nil:
				checks["usable_keys"] = "error: " + err.Error()
				ready = false
			case n == 0:
				checks["usable_keys"] = "none: 库中没有可用 API Key，切流后所有请求都会 401"
				ready = false
			default:
				checks["usable_keys"] = fmt.Sprintf("%d", n)
			}
		}

		status := "ready"
		code := http.StatusOK
		if !ready {
			status = "not_ready"
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"status": status, "checks": checks})
	}
}

// countUsableKeys 统计「未禁用且未过期」的 Key 数量，与 VerifyKey 的判定口径一致。
func countUsableKeys(ctx context.Context, db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, errNoDB
	}
	var n int64
	err := db.WithContext(ctx).
		Raw("SELECT count(*) FROM api_keys WHERE enabled = ? AND (expires_at IS NULL OR expires_at > ?)", true, time.Now()).
		Scan(&n).Error
	return n, err
}

func pingPostgres(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errNoDB
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

type readinessError string

func (e readinessError) Error() string { return string(e) }

const errNoDB readinessError = "database handle not configured"
