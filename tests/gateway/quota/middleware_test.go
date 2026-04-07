package quota_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestQuotaMiddleware_BeforeUpstreamCanRunAfterAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := quota.NewSnapshotHolder()
	store := quota.NewRedisStore(rdb)
	gate := quota.NewMiddleware(holder, store)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("identity", &gwmodel.UserIdentity{UserID: "u-1", KeyType: "personal"})

	require.NoError(t, gate.BeforeUpstream(ctx, "premium"))
	require.NotEmpty(t, ctx.GetString("quota_subject_id"))
	gate.AfterUpstream(ctx)
	require.Equal(t, int64(0), rdb.Exists(context.Background(), quota.ConcurrencyKey("user", "u-1", "", "")).Val())
}
