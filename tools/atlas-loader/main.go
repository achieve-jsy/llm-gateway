package main

import (
	"fmt"
	"io"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	// 加载全部 GORM model 包
	authmodel "github.com/example/llm-gateway/internal/auth/model"
	cpmmodel "github.com/example/llm-gateway/internal/controlplane/model"
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
		&cpmmodel.GroupEntitlement{},
		&cpmmodel.SystemConfig{},
		// v0.7 预算模块
		&cpmmodel.BudgetAllocation{},
		&cpmmodel.BudgetConsumption{},
		&cpmmodel.BudgetAlertRecord{},
		// v0.7 扩容审批
		&cpmmodel.ExpansionRequest{},
		&cpmmodel.ExpansionApprovalStep{},
		// 站内通知（独立于公告）
		&cpmmodel.Notification{},
		// v0.8 审计日志
		&cpmmodel.AuditLog{},

		// EHR 组织同步：5 实体表
		&cpmmodel.EhrOrganization{},
		&cpmmodel.EhrCompany{},
		&cpmmodel.EhrDepartment{},
		&cpmmodel.EhrEmployee{},
		&cpmmodel.EhrPosition{},
		// EHR 同步观测：6 日志表
		&cpmmodel.EhrSyncExecutionLog{},
		&cpmmodel.EhrSyncChangeLog{},
		&cpmmodel.EhrSyncErrorLog{},
		&cpmmodel.EhrSyncDiffReport{},
		&cpmmodel.EhrSyncSkipLog{},
		&cpmmodel.EhrSyncStatSnapshot{},

		// auth/model (3 个表)
		&authmodel.Department{},
		&authmodel.DevUser{},
		&authmodel.User{},

		// observability/model (3 个表)
		&obsrvmodel.CallLog{},
		&obsrvmodel.CallTrace{},
		&obsrvmodel.UsageDaily{},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load gorm schema: %v\n", err)
		os.Exit(1)
	}
	io.WriteString(os.Stdout, loaded)
}
