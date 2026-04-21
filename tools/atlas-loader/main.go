package main

import (
	"fmt"
	"io"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	// 加载全部 GORM model 包
	authmodel "github.com/example/llm-gateway/internal/auth/model"
	"github.com/example/llm-gateway/internal/controlplane/keyactivity"
	cpmmodel "github.com/example/llm-gateway/internal/controlplane/model"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	obsrvmodel "github.com/example/llm-gateway/internal/observability/model"
)

func main() {
	loaded, err := gormschema.New("postgres").Load(
		// controlplane/model (21 个表，v0.7 新增 6 张)
		&cpmmodel.Announcement{},
		&cpmmodel.AnnouncementRead{},
		&cpmmodel.Group{},
		&cpmmodel.GroupModel{},
		&cpmmodel.GroupEntitlement{},
		&cpmmodel.HealthCheckConfig{},
		&cpmmodel.HealthCheckLog{},
		&cpmmodel.KeyGroupBinding{},
		&cpmmodel.ApiKey{},
		&cpmmodel.Model{},
		&cpmmodel.ModelStatusLog{},
		&cpmmodel.ModelCatalog{},
		&cpmmodel.ModelTag{},
		&cpmmodel.ModelTagRelation{},
		&cpmmodel.ProviderDefinition{},
		&cpmmodel.Provider{},
		&cpmmodel.SystemConfig{},
		// v0.7 预算模块
		&cpmmodel.BudgetAllocation{},
		&cpmmodel.BudgetConsumption{},
		&cpmmodel.BudgetAlertRecord{},
		&cpmmodel.ModelTier{},
		&cpmmodel.BudgetTierLimit{},
		&cpmmodel.BudgetTemporaryOverride{},
		&cpmmodel.BudgetLimitSchedule{},
		// v0.7 扩容审批
		&cpmmodel.ExpansionRequest{},
		&cpmmodel.ExpansionApprovalStep{},
		// 站内通知（独立于公告）
		&cpmmodel.Notification{},
		&cpmmodel.NotificationOutbox{},
		&cpmmodel.NotificationDelivery{},
		// v0.8 审计日志
		&cpmmodel.AuditLog{},
		// Seedance 视频任务记录
		&cpmmodel.VideoTask{},
		&cpmmodel.VideoTaskEvent{},
		// 配额紧急豁免（原 migrations/058，补入 Atlas）
		&corequota.QuotaExemption{},
		&cpmmodel.SystemLog{},
		&cpmmodel.AlertRule{},
		&cpmmodel.AlertEvent{},
		&cpmmodel.AlertSilence{},
		&cpmmodel.AlertJobHeartbeat{},
		&cpmmodel.DeptPolicy{},
		&cpmmodel.KeyRequest{},
		&cpmmodel.ChannelMonitor{},
		&cpmmodel.ChannelMonitorHistory{},
		&cpmmodel.ChannelMonitorDailyRollup{},
		&cpmmodel.ChannelMonitorRequestTemplate{},
		&cpmmodel.ChannelMonitorAggregationWatermark{},
		&keyactivity.KeySourceSeen{},
		&keyactivity.KeyActivityState{},
		&keyactivity.KeyActivityAlert{},

		// auth/model (3 个表)
		&authmodel.Department{},
		&authmodel.DevUser{},
		&authmodel.User{},

		// observability/model (3 个表)
		&obsrvmodel.CallLog{},
		&obsrvmodel.CallTrace{},
		&obsrvmodel.UsageDaily{},
		&obsrvmodel.ModelDailyStats{},
		&obsrvmodel.UserDailyStats{},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load gorm schema: %v\n", err)
		os.Exit(1)
	}
	io.WriteString(os.Stdout, loaded)
}
