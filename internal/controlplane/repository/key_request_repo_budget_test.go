package repository

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func openKeyRequestTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: false},
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// sqlite 测试库：去掉 Postgres 专属 default
	if err := db.Exec(`CREATE TABLE key_requests (
		id TEXT PRIMARY KEY,
		requester_id TEXT NOT NULL,
		key_name TEXT NOT NULL,
		key_type TEXT NOT NULL DEFAULT 'personal',
		status TEXT NOT NULL DEFAULT 'pending',
		dept_id TEXT,
		budget_monthly REAL DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestCountPendingScoped_budgetPending(t *testing.T) {
	db := openKeyRequestTestDB(t)
	repo := NewKeyRequestRepo(db)

	for _, row := range []struct {
		id     string
		name   string
		status string
		dept   string
		budget float64
	}{
		{uuid.New().String(), "a", model.KeyRequestStatusPending, "d1", 100},
		{uuid.New().String(), "b", model.KeyRequestStatusPending, "d1", 0},
		{uuid.New().String(), "c", model.KeyRequestStatusApproved, "d1", 50},
	} {
		if err := db.Exec(
			`INSERT INTO key_requests (id, requester_id, key_name, key_type, status, dept_id, budget_monthly) VALUES (?, ?, ?, 'personal', ?, ?, ?)`,
			row.id, uuid.New().String(), row.name, row.status, row.dept, row.budget,
		).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	total, budget, err := repo.CountPendingScoped(false, []string{"d1"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2 {
		t.Fatalf("total want 2 got %d", total)
	}
	if budget != 1 {
		t.Fatalf("budget want 1 got %d", budget)
	}
}
