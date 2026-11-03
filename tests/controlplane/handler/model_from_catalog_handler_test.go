package handler_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A12 的 API 语义补充测试：catalog mode 无枚举落点时的拒绝必须映射成 400 bad_request，
// 而不是 500 internal_error（这是请求数据问题，且要保证不会被 containsCatalogNotFound
// 的字符串启发式误判成 404）。
//
// 复用同包 model_batch_create_handler_test.go 里已实现全部方法的 fake，只覆盖目标方法。

type fakeFromCatalogSvc struct {
	*fakeModelSvcForBatch
	createFromCatalogErr error
}

func (f *fakeFromCatalogSvc) CreateModelFromCatalog(uuid.UUID, uuid.UUID, string) (*model.Model, error) {
	return nil, f.createFromCatalogErr
}

func TestCreateModelFromCatalogHandler_UnsupportedCatalogModeIsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catID := uuid.New()
	fake := &fakeFromCatalogSvc{
		fakeModelSvcForBatch: &fakeModelSvcForBatch{},
		// 与 service 层实际返回一致：哨兵错误被包了一层 catalog 上下文
		createFromCatalogErr: fmt.Errorf("catalog %s: %w", catID, service.ErrCatalogModeUnsupported),
	}
	h := handler.NewModelHandler(fake)
	router := gin.New()
	router.POST("/api/v1/models/from-catalog", h.CreateModelFromCatalog)

	body := fmt.Sprintf(`{"catalog_id":%q,"provider_id":%q,"name":"whisper-1"}`,
		catID.String(), uuid.New().String())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/from-catalog", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "哨兵错误必须映射为 400，而不是 500/404：%s", w.Body.String())
	require.Contains(t, w.Body.String(), handler.CodeBadRequest)
	require.NotContains(t, w.Body.String(), handler.CodeInternalError)
	require.NotContains(t, w.Body.String(), handler.CodeCatalogNotFound)
}
