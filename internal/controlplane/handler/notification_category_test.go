package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// categoryRecorderRepo 记录 ListAllPaged 收到的 types 参数，其余方法返回空值。
type categoryRecorderRepo struct {
	gotTypes []string
}

func (r *categoryRecorderRepo) GetUnread(userID uuid.UUID) ([]model.Notification, error) {
	return nil, nil
}
func (r *categoryRecorderRepo) GetUnreadCount(userID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *categoryRecorderRepo) MarkRead(userID, notificationID uuid.UUID) error {
	return nil
}
func (r *categoryRecorderRepo) MarkAllRead(userID uuid.UUID) error {
	return nil
}
func (r *categoryRecorderRepo) GetHistory(userID uuid.UUID) ([]model.Notification, error) {
	return nil, nil
}
func (r *categoryRecorderRepo) ListAllPaged(channel, notifType, level, keyword, user string, types []string, startTime, endTime *time.Time, page, pageSize int) ([]model.AdminNotificationRecord, int64, error) {
	r.gotTypes = types
	return nil, 0, nil
}

func doAdminHistoryQuery(t *testing.T, repo NotificationRepoInterface, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewNotificationHandler(repo)
	r.GET("/api/v1/notifications/admin/history", h.GetAdminHistory)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/admin/history?"+query, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestGetAdminHistory_CategoryResolvesToTypes(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantTypes []string
	}{
		{"ops category", "category=ops", []string{"ops_channel_down", "ops_channel_recovered"}},
		{"notification category", "category=notification", []string{"ops_delivery_failed"}},
		{"budget category", "category=budget", []string{"budget_alert", "budget_owner_attention", "budget_schedule_applied", "budget_schedule_failed"}},
		{"unknown category", "category=nonexistent", nil},
		{"no category", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &categoryRecorderRepo{}
			doAdminHistoryQuery(t, repo, tc.query)
			if !reflect.DeepEqual(repo.gotTypes, tc.wantTypes) {
				t.Fatalf("types: want %v, got %v", tc.wantTypes, repo.gotTypes)
			}
		})
	}
}

func TestGetAdminHistory_CategoryAndTypeMutuallyExclusive(t *testing.T) {
	// 同时传 category 与 type：handler 仍解析 category→types，repo 层单类型优先。
	repo := &categoryRecorderRepo{}
	doAdminHistoryQuery(t, repo, "category=ops&type=budget_alert")
	want := []string{"ops_channel_down", "ops_channel_recovered"}
	if !reflect.DeepEqual(repo.gotTypes, want) {
		t.Fatalf("types: want %v, got %v", want, repo.gotTypes)
	}
}
