package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

func TestExpansionTransitionRequiresFreshPrecheck(t *testing.T) {
	tests := []struct {
		from   string
		action string
		want   string
		ok     bool
	}{
		{model.ExpansionStatusPending, model.ExpansionActionApprove, model.ExpansionStatusApproved, true},
		{model.ExpansionStatusPending, model.ExpansionActionReprecheckRequired, model.ExpansionStatusPrecheckRequired, true},
		{model.ExpansionStatusPrecheckRequired, model.ExpansionActionConfirmPrecheck, model.ExpansionStatusPending, true},
		{model.ExpansionStatusApproved, model.ExpansionActionCancel, "", false},
		{model.ExpansionStatusExpired, model.ExpansionActionApprove, "", false},
	}
	for _, tt := range tests {
		got, err := model.NextExpansionStatus(tt.from, tt.action)
		if tt.ok {
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		} else {
			require.Error(t, err)
		}
	}
}

func TestPrecheckRejectsScopeOutsideActorManagement(t *testing.T) {
	svc := service.NewExpansionService(nil)
	_, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: "manager-1", Role: "dept_admin", DeptIDs: []string{"dept-a"},
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeDeptDaily, ScopeID: "dept-b",
		PeriodType: string(budgetdomain.PeriodMonthly), DimensionType: string(budgetdomain.DimensionTotal),
		DurationType: string(budgetdomain.DurationPermanent), TargetAmountUSD: "100.000000",
	})
	require.True(t, errors.Is(err, service.ErrExpansionForbidden), "got %v", err)
}

func TestNotifyOwnerRejectsCrossOrganizationTargetBeforeSending(t *testing.T) {
	svc := service.NewExpansionService(nil)
	err := svc.NotifyOwner(context.Background(), service.ActorScope{
		UserID: "user-a", Role: "user",
	}, service.NotifyOwnerInput{
		ScopeType: model.BudgetScopeMember, ScopeID: "user-b",
		PeriodType: string(budgetdomain.PeriodMonthly), DimensionType: string(budgetdomain.DimensionTotal),
		Message: "please raise this budget",
	})
	require.ErrorIs(t, err, service.ErrExpansionForbidden)
}

func TestVisibleScopesAreTrimmedForAllRoles(t *testing.T) {
	svc := service.NewExpansionService(nil)
	tests := []struct {
		role string
		want []string
	}{
		{"admin", []string{"company", "studio", "dept_daily", "dept_service", "member"}},
		{"studio_admin", []string{"studio", "dept_daily", "dept_service", "member"}},
		{"dept_admin", []string{"dept_daily", "dept_service", "member"}},
		{"user", []string{"member"}},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, svc.VisibleScopeTypes(tt.role))
	}
}
