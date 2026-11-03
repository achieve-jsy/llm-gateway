package repository

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVideoTaskScopedQueriesIsolateByUserAndOrgScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 表名随 C1（决策 D）由 video_tasks/video_task_events 改为 media_tasks/media_task_events，
	// GORM 的 INSERT 会写全部列（含 C1 delta 14 列），因此这里的裸 DDL 必须同步补齐。
	for _, stmt := range []string{
		`CREATE TABLE media_tasks (
			task_id TEXT PRIMARY KEY,
			api_key_id TEXT NOT NULL,
			api_key_name TEXT,
			api_key_type TEXT,
			user_id TEXT,
			user_name TEXT,
			model TEXT NOT NULL,
			provider_name TEXT NOT NULL,
			provider_type TEXT NOT NULL,
			status TEXT NOT NULL,
			video_url TEXT,
			request_body TEXT,
			response_body TEXT,
			error_message TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			kind TEXT,
			action TEXT,
			vendor_task_id TEXT,
			progress TEXT,
			usage TEXT,
			cost_micro_usd INTEGER,
			billed_at DATETIME,
			started_at DATETIME,
			finished_at DATETIME,
			poll_failures INTEGER,
			idempotency_key TEXT,
			private_data TEXT,
			dept_id TEXT,
			studio_id TEXT
		)`,
		`CREATE UNIQUE INDEX uniq_media_tasks_idem ON media_tasks (api_key_id, idempotency_key)`,
		`CREATE TABLE media_task_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			event_data TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE departments (id TEXT PRIMARY KEY, dept_id TEXT, studio_id TEXT)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, dept_id TEXT)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}

	seedVideoTask := func(taskID, userID, status string) {
		t.Helper()
		now := time.Now().UTC()
		if err := db.Create(&model.VideoTask{
			TaskID:       taskID,
			APIKeyID:     "key-" + userID,
			UserID:       userID,
			Model:        "seedance-2.0",
			ProviderName: "seedance",
			ProviderType: "seedance-relay",
			Status:       status,
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error; err != nil {
			t.Fatalf("seed video task: %v", err)
		}
	}

	requireExec(t, db, `INSERT INTO departments (id, dept_id, studio_id) VALUES ('dept-a-uuid', 'dept-a', 'studio-a'), ('dept-b-uuid', 'dept-b', 'studio-b')`)
	requireExec(t, db, `INSERT INTO users (id, dept_id) VALUES ('user-a', 'dept-a-uuid'), ('user-b', 'dept-b-uuid')`)
	seedVideoTask("task-a", "user-a", "succeeded")
	seedVideoTask("task-b", "user-b", "failed")

	repo := NewVideoTaskRepo(db)

	personal, total, err := repo.ListScoped(1, 20, "", "", "", "", VideoTaskScope{UserID: "user-a"})
	if err != nil {
		t.Fatalf("list personal: %v", err)
	}
	if total != 1 || len(personal) != 1 || personal[0].TaskID != "task-a" {
		t.Fatalf("personal scope leaked tasks: total=%d items=%v", total, personal)
	}
	if _, err := repo.GetByIDScoped("task-b", VideoTaskScope{UserID: "user-a"}); err == nil {
		t.Fatalf("personal scope should not read another user's task detail")
	}

	deptTasks, total, err := repo.ListScoped(1, 20, "", "", "", "", VideoTaskScope{DeptIDs: []string{"dept-a"}})
	if err != nil {
		t.Fatalf("list dept: %v", err)
	}
	if total != 1 || len(deptTasks) != 1 || deptTasks[0].TaskID != "task-a" {
		t.Fatalf("dept scope leaked tasks: total=%d items=%v", total, deptTasks)
	}

	distribution, err := repo.StatsDistributionScoped("", "", "", "", "", VideoTaskScope{StudioIDs: []string{"studio-b"}})
	if err != nil {
		t.Fatalf("distribution studio: %v", err)
	}
	if len(distribution.Statuses) != 1 || distribution.Statuses[0].Status != "failed" || distribution.Statuses[0].Count != 1 {
		t.Fatalf("studio distribution leaked tasks: %+v", distribution.Statuses)
	}
	if len(distribution.Models) != 1 || distribution.Models[0].Model != "seedance-2.0" || distribution.Models[0].Count != 1 {
		t.Fatalf("studio model distribution leaked tasks: %+v", distribution.Models)
	}
}

func requireExec(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
