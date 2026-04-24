package service

import (
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestApplySeedanceLogFilterMatchesVideoAndAssetRequests(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&obsmodel.CallLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	logs := []obsmodel.CallLog{
		{RequestID: "create", TS: now, Model: "seedance-2.0:CreateTask", Status: 200, InboundEndpoint: "/v1/contents/generations/tasks"},
		{RequestID: "get", TS: now, Model: "seedance-2.0:GetTask", Status: 200, InboundEndpoint: "/v1/contents/generations/tasks/cgt-1"},
		{RequestID: "asset", TS: now, Model: "seedance-asset:GetAsset", Status: 200, InboundEndpoint: "/v1/assets/GetAsset"},
		{RequestID: "chat", TS: now, Model: "gpt-4o", Status: 200, InboundEndpoint: "/v1/chat/completions"},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed logs: %v", err)
	}

	var count int64
	if err := ApplySeedanceLogFilter(db.Model(&obsmodel.CallLog{}), "call_logs").Count(&count).Error; err != nil {
		t.Fatalf("count seedance logs: %v", err)
	}
	if count != 3 {
		t.Fatalf("seedance log count=%d want 3", count)
	}
}
