package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type lifecycleOwnerStub struct {
	recipients []uuid.UUID
	calls      []uuid.UUID
}

func (s *lifecycleOwnerStub) ResolveOwnerRecipients(_ context.Context, allocationID uuid.UUID) ([]uuid.UUID, error) {
	s.calls = append(s.calls, allocationID)
	return s.recipients, nil
}

type lifecycleNotifierStub struct {
	mu     sync.Mutex
	counts map[string]int
}

func (s *lifecycleNotifierStub) Notify(_ context.Context, notification *model.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[notification.UserID.String()+":"+notification.Type]++
	return nil
}

func (s *lifecycleNotifierStub) count(userID uuid.UUID, notificationType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[userID.String()+":"+notificationType]
}

func newLifecycleObserverFixture(t *testing.T) (*service.BudgetLifecycleNotificationObserver, *lifecycleOwnerStub, *lifecycleNotifierStub, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE expansion_requests (id TEXT PRIMARY KEY, requester_id TEXT NOT NULL)`,
		`CREATE TABLE budget_limit_schedules (
			id TEXT PRIMARY KEY,
			created_by TEXT NOT NULL,
			dimension_type TEXT NOT NULL,
			period_type TEXT NOT NULL
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create fixture table: %v", err)
		}
	}
	if err := db.Exec(`CREATE TABLE notification_outbox (
		id TEXT PRIMARY KEY, dedupe_key TEXT NOT NULL UNIQUE, user_id TEXT NOT NULL,
		type TEXT NOT NULL, level TEXT NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL,
		ref_type TEXT NOT NULL DEFAULT '', ref_id TEXT NOT NULL DEFAULT '',
		template_data TEXT,
		status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
		next_attempt DATETIME NOT NULL, last_error TEXT NOT NULL DEFAULT '',
		locked_at DATETIME, lease_owner TEXT NOT NULL DEFAULT '', lease_expires_at INTEGER NOT NULL DEFAULT 0,
		delivered_at DATETIME, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("migrate notification outbox: %v", err)
	}
	if err := db.Exec(`CREATE TABLE notifications (
		id TEXT PRIMARY KEY, user_id TEXT NOT NULL, type TEXT NOT NULL, level TEXT NOT NULL,
		title TEXT NOT NULL, content TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'direct', dedupe_key TEXT,
		ref_type TEXT NOT NULL DEFAULT '', ref_id TEXT NOT NULL DEFAULT '', is_read BOOL NOT NULL DEFAULT false,
		read_at DATETIME, created_at DATETIME, UNIQUE(source,dedupe_key)
	)`).Error; err != nil {
		t.Fatalf("migrate notifications: %v", err)
	}
	owners := &lifecycleOwnerStub{}
	notifier := &lifecycleNotifierStub{counts: make(map[string]int)}
	observer := service.NewBudgetLifecycleNotificationObserver(
		db, repository.NewExpansionRepo(db), owners, notifier,
	)
	return observer, owners, notifier, db
}

func TestLifecycleObserverNotifiesRequesterAndCurrentOwnerOnce(t *testing.T) {
	observer, owners, notifier, db := newLifecycleObserverFixture(t)
	requestID, requesterID, ownerID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec(
		`INSERT INTO expansion_requests (id, requester_id) VALUES (?, ?)`,
		requestID.String(), requesterID.String(),
	).Error; err != nil {
		t.Fatalf("seed expansion request: %v", err)
	}
	owners.recipients = []uuid.UUID{ownerID}
	item := service.ExpiredBudgetOverride{
		ID: uuid.New(), BudgetAllocationID: allocationID,
		DimensionType: budgetdomain.DimensionTier, PeriodType: budgetdomain.PeriodMonthly,
		ExpiredAt: time.Now().UTC(), ExpansionRequestID: &requestID,
	}

	if err := observer.OnOverrideExpired(context.Background(), item); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if err := observer.OnOverrideExpired(context.Background(), item); err != nil {
		t.Fatalf("duplicate callback: %v", err)
	}
	secondInstance := service.NewBudgetLifecycleNotificationObserver(
		db, repository.NewExpansionRepo(db), owners, notifier,
	)
	if err := secondInstance.OnOverrideExpired(context.Background(), item); err != nil {
		t.Fatalf("cross-instance duplicate callback: %v", err)
	}
	// 异步契约：回调只入队，需显式排空才投递。
	if err := observer.RetryPending(context.Background(), 10); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := notifier.count(requesterID, model.NotificationTypeExpansionRestored); got != 1 {
		t.Fatalf("requester notifications = %d, want 1", got)
	}
	if got := notifier.count(ownerID, model.NotificationTypeExpansionRestored); got != 1 {
		t.Fatalf("owner notifications = %d, want 1", got)
	}
	if len(owners.calls) != 3 || owners.calls[0] != allocationID {
		t.Fatalf("owner resolver calls = %v", owners.calls)
	}
}

func TestLifecycleObserverNotifiesScheduleAppliedAndFailedOnce(t *testing.T) {
	observer, owners, notifier, db := newLifecycleObserverFixture(t)
	scheduleID, createdBy, ownerID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec(
		`INSERT INTO budget_limit_schedules (id, created_by, dimension_type, period_type) VALUES (?, ?, ?, ?)`,
		scheduleID.String(), createdBy.String(), "total", "monthly",
	).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	owners.recipients = []uuid.UUID{ownerID}
	now := time.Now().UTC()
	applied := service.AppliedBudgetSchedule{ID: scheduleID, BudgetAllocationID: allocationID, AppliedAt: now}
	failed := service.FailedBudgetSchedule{
		ID: scheduleID, BudgetAllocationID: allocationID,
		FailureReason: "parent limit changed", FailedAt: now,
	}
	for range 2 {
		if err := observer.OnScheduleApplied(context.Background(), applied); err != nil {
			t.Fatalf("applied callback: %v", err)
		}
		if err := observer.OnScheduleFailed(context.Background(), failed); err != nil {
			t.Fatalf("failed callback: %v", err)
		}
	}
	// 异步契约：回调只入队，需显式排空才投递。
	if err := observer.RetryPending(context.Background(), 10); err != nil {
		t.Fatalf("drain: %v", err)
	}
	for _, userID := range []uuid.UUID{createdBy, ownerID} {
		if got := notifier.count(userID, "budget_schedule_applied"); got != 1 {
			t.Fatalf("applied notifications for %s = %d, want 1", userID, got)
		}
		if got := notifier.count(userID, "budget_schedule_failed"); got != 1 {
			t.Fatalf("failed notifications for %s = %d, want 1", userID, got)
		}
	}
}

func TestLifecycleOutboxDeadlettersAfterMaxAttempts(t *testing.T) {
	observer, _, notifier, db := newLifecycleObserverFixture(t)
	userID := uuid.New()
	row := &model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "deadletter", UserID: userID, Type: "budget_schedule_applied",
		Level: model.NotificationLevelInfo, Title: "x", Content: "x",
		Status: "failed", Attempts: 99, NextAttempt: time.Now().Add(-time.Minute).UTC(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	if err := observer.RetryPending(context.Background(), 10); err != nil {
		t.Fatalf("retry pending: %v", err)
	}
	if got := notifier.count(userID, "budget_schedule_applied"); got != 0 {
		t.Fatalf("notifications = %d, want 0 (deadlettered, no dispatch)", got)
	}
	var persisted model.NotificationOutbox
	if err := db.First(&persisted, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	if persisted.Status != "deadletter" {
		t.Fatalf("status = %q, want deadletter", persisted.Status)
	}
}

func TestLifecycleOutboxRecoversExpiredProcessingLease(t *testing.T) {
	observer, _, notifier, db := newLifecycleObserverFixture(t)
	userID := uuid.New()
	row := &model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "crash-recovery", UserID: userID, Type: "budget_schedule_applied",
		Level: model.NotificationLevelInfo, Title: "recover", Content: "recover",
		Status: "processing", Attempts: 1, NextAttempt: time.Now().Add(-time.Minute),
		LockedAt:       func() *time.Time { value := time.Now().Add(-10 * time.Minute); return &value }(),
		LeaseOwner:     "crashed-worker",
		LeaseExpiresAt: time.Now().Add(-5 * time.Minute).UnixMicro(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	if err := observer.RetryPending(context.Background(), 10); err != nil {
		t.Fatalf("retry pending: %v", err)
	}
	if got := notifier.count(userID, "budget_schedule_applied"); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
	var persisted model.NotificationOutbox
	if err := db.First(&persisted, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	if persisted.Status != "delivered" || persisted.LeaseOwner != "" || persisted.LockedAt != nil {
		t.Fatalf("outbox not recovered: %+v", persisted)
	}
}

func TestLifecycleOutboxCrashAfterInboxInsertDoesNotDuplicateNotification(t *testing.T) {
	_, _, _, db := newLifecycleObserverFixture(t)
	userID, key := uuid.New(), "crash-after-inbox"
	repo := repository.NewNotificationRepo(db)
	require.NoError(t, repo.Create(&model.Notification{
		ID: uuid.New(), UserID: userID, Type: "expansion_approved", Level: "info",
		Title: "done", Content: "done", Source: "outbox", DedupeKey: key,
	}))
	expired := time.Now().Add(-time.Minute)
	require.NoError(t, db.Create(&model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: key, UserID: userID, Type: "expansion_approved", Level: "info",
		Title: "done", Content: "done", Status: "processing", Attempts: 1, NextAttempt: expired,
		LockedAt: &expired, LeaseOwner: "crashed", LeaseExpiresAt: expired.UnixMicro(),
	}).Error)
	observer := service.NewBudgetLifecycleNotificationObserver(
		db, repository.NewExpansionRepo(db), &lifecycleOwnerStub{},
		service.NewUnifiedNotifier(nil, nil, db, repo, nil, nil, nil, nil, nil),
	)
	require.NoError(t, observer.RetryPending(context.Background(), 10))
	var count int64
	require.NoError(t, db.Model(&model.Notification{}).Where("source = ? AND dedupe_key = ?", "outbox", key).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
