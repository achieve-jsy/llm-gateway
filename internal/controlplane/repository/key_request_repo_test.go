package repository

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListHistoryPaged_respectsPageSize(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.KeyRequest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewKeyRequestRepo(db)

	for i := 0; i < 5; i++ {
		status := model.KeyRequestStatusApproved
		if i%2 == 0 {
			status = model.KeyRequestStatusRejected
		}
		if err := db.Create(&model.KeyRequest{
			ID:          uuid.New(),
			RequesterID: uuid.New(),
			KeyName:     "k",
			Status:      status,
			DeptID:      "dept-1",
		}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	page1, total, err := repo.ListHistoryPaged(true, nil, "", "", 1, 2)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total want 5 got %d", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len want 2 got %d", len(page1))
	}

	page2, _, err := repo.ListHistoryPaged(true, nil, "", "", 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len want 2 got %d", len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Fatalf("page1 and page2 should not overlap")
	}
}
