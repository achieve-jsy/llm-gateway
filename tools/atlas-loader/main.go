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
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	obsrvmodel "github.com/example/llm-gateway/internal/observability/model"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
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
		// 方案X 异常反馈闭环：异常记录落库（upsert + 人工反馈 + 读回）
		&cpmmodel.AnomalyRecord{},
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

		// Token 使用分析报告：存档 + 严重异常待核查 + 非工作时段备案
		&cpmmodel.ReportSnapshot{},
		&cpmmodel.AnomalyReview{},
		&cpmmodel.OffhoursFiling{},

		// 用户反馈
		&cpmmodel.Feedback{},
		&cpmmodel.FeedbackAttachment{},

		// auth/model (2 个表)
		&authmodel.DevUser{},
		&authmodel.User{},

		// orgservice/model：组织架构与 RBAC（studios / departments / dept_studio_map / user_roles）
		// departments 由 OrgDepartment 声明（含 status/studio_id/owner_id），
		// auth/model 里的同名 Department 只是子集，重复注册会让 Atlas 看到两张不同的表。
		&orgmodel.Studio{},
		&orgmodel.OrgDepartment{},
		&orgmodel.DeptStudioMap{},
		&orgmodel.UserRole{},
		&orgmodel.OrgMigrationLog{},

		// 凭证加密的 KEK/DEK（原先只靠运行时建表，迁移里没有）
		&crypto.EncryptionKey{},

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
