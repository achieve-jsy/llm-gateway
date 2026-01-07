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
