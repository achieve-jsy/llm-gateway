package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type blockingKeyRequestCreator struct {
	mu            sync.Mutex
	created       int
	firstStarted  chan struct{}
	secondStarted chan struct{}
	release       chan struct{}
}

func newBlockingKeyRequestCreator() *blockingKeyRequestCreator {
	return &blockingKeyRequestCreator{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		release:       make(chan struct{}),
	}
}

func (c *blockingKeyRequestCreator) CreateKey(_ context.Context, _ uuid.UUID, name string, _ *time.Time, _ []uint) (*model.ApiKey, string, []service.BoundGroupItem, error) {
	c.mu.Lock()
	c.created++
	created := c.created
	c.mu.Unlock()
	if created == 1 {
		close(c.firstStarted)
	}
	if created == 2 {
		close(c.secondStarted)
	}
	<-c.release
	return &model.ApiKey{ID: uuid.New(), Name: name}, "", nil, nil
}

func (c *blockingKeyRequestCreator) UpdateAccessAllGroups(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ bool) (*model.ApiKey, error) {
	return nil, nil
}

func (c *blockingKeyRequestCreator) createCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.created
}

func TestBatchApprovePendingForDept_ConcurrentRunsCreateOneKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:key-request-batch?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE key_requests (
		id TEXT PRIMARY KEY,
		requester_id TEXT NOT NULL,
		requester_name TEXT,
		dept_id TEXT,
		dept_name TEXT,
		key_name TEXT NOT NULL,
		key_type TEXT NOT NULL,
		purpose TEXT,
		group_ids TEXT,
		expires_at DATETIME,
		budget_monthly REAL DEFAULT 0,
		status TEXT NOT NULL,
		approver_id TEXT,
		approver_name TEXT,
		approve_comment TEXT,
		created_key_id TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)

	repo := repository.NewKeyRequestRepo(db)
	request := &model.KeyRequest{
		ID:            uuid.New(),
		RequesterID:   uuid.New(),
		RequesterName: "申请人",
		DeptID:        "dept-a",
		KeyName:       "shared-request",
		KeyType:       model.KeyTypePersonal,
		Status:        model.KeyRequestStatusPending,
	}
	require.NoError(t, repo.Create(request))

	creator := newBlockingKeyRequestCreator()
	first := service.NewKeyRequestService(repo, nil, nil)
	second := service.NewKeyRequestService(repo, nil, nil)
	first.SetKeyCreator(creator)
	second.SetKeyCreator(creator)

	resultErrors := make(chan error, 2)
	go func() {
		_, runErr := first.BatchApprovePendingForDept(context.Background(), "dept-a", uuid.New(), "负责人 A")
		resultErrors <- runErr
	}()

	select {
	case <-creator.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first batch did not start key creation")
	}

	go func() {
		_, runErr := second.BatchApprovePendingForDept(context.Background(), "dept-a", uuid.New(), "负责人 B")
		resultErrors <- runErr
	}()

	secondKeyCreated := false
	select {
	case <-creator.secondStarted:
		secondKeyCreated = true
	case <-time.After(150 * time.Millisecond):
	}
	close(creator.release)

	require.NoError(t, <-resultErrors)
	require.NoError(t, <-resultErrors)
	require.False(t, secondKeyCreated, "concurrent batches must not create two keys for one request")
	require.Equal(t, 1, creator.createCount())

	stored, err := repo.FindByID(request.ID.String())
	require.NoError(t, err)
	require.Equal(t, model.KeyRequestStatusApproved, stored.Status)
}
