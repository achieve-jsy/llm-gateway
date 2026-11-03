package service

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type DatabaseApprovalPoolResolver struct {
	db *gorm.DB
}

func NewDatabaseApprovalPoolResolver(db *gorm.DB) *DatabaseApprovalPoolResolver {
	return &DatabaseApprovalPoolResolver{db: db}
}

func (r *DatabaseApprovalPoolResolver) ResolveInTx(
	ctx context.Context,
	tx *gorm.DB,
	request *model.ExpansionRequest,
) ([]uuid.UUID, error) {
	return (&DatabaseApprovalPoolResolver{db: tx}).Resolve(ctx, request)
}

func (r *DatabaseApprovalPoolResolver) Resolve(ctx context.Context, request *model.ExpansionRequest) ([]uuid.UUID, error) {
	switch request.CurrentLevel {
	case model.ExpansionLevelDept:
		deptID, err := r.memberDeptID(ctx, request.ScopeID)
		if err != nil {
			return nil, err
		}
		return r.roleRecipients(ctx, "dept_admin", deptID)
	case model.ExpansionLevelStudio:
		studioID, err := r.scopeStudioID(ctx, request.ScopeType, request.ScopeID)
		if err != nil {
			return nil, err
		}
		return r.roleRecipients(ctx, "studio_admin", studioID)
	default:
		return r.roleRecipients(ctx, "admin", "")
	}
}

func (r *DatabaseApprovalPoolResolver) ResolveScopeOwnerRecipients(
	ctx context.Context,
	scopeType, scopeID string,
) ([]uuid.UUID, error) {
	switch scopeType {
	case model.BudgetScopeMember:
		deptID, err := r.memberDeptID(ctx, scopeID)
		if err != nil {
			return nil, err
		}
		return r.roleRecipients(ctx, "dept_admin", deptID)
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return r.roleRecipients(ctx, "dept_admin", scopeID)
	case model.BudgetScopeStudio:
		return r.roleRecipients(ctx, "studio_admin", scopeID)
	default:
		return r.roleRecipients(ctx, "admin", "")
	}
}

func (r *DatabaseApprovalPoolResolver) ResolveParentAdmins(
	ctx context.Context,
	request *model.ExpansionRequest,
) ([]uuid.UUID, error) {
	switch request.CurrentLevel {
	case model.ExpansionLevelDept:
		studioID, err := r.scopeStudioID(ctx, request.ScopeType, request.ScopeID)
		if err != nil {
			return nil, err
		}
		return r.roleRecipients(ctx, "studio_admin", studioID)
	default:
		return r.roleRecipients(ctx, "admin", "")
	}
}

func (r *DatabaseApprovalPoolResolver) roleRecipients(ctx context.Context, role, scopeID string) ([]uuid.UUID, error) {
	var rawIDs []string
	query := r.db.WithContext(ctx).Table("user_roles").
		Where("role = ?", role)
	if scopeID != "" {
		query = query.Where("CAST(scope_id AS TEXT) = ?", scopeID)
	}
	if err := query.Distinct("user_id").Pluck("user_id", &rawIDs).Error; err != nil {
		return nil, err
	}
	result := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		if id, err := uuid.Parse(raw); err == nil {
			result = append(result, id)
		}
	}
	return result, nil
}

func (r *DatabaseApprovalPoolResolver) memberDeptID(ctx context.Context, memberID string) (string, error) {
	var deptID string
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(d.dept_id::text, '')
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE u.id::text = ?
		LIMIT 1
	`, memberID).Scan(&deptID).Error
	return deptID, err
}

func (r *DatabaseApprovalPoolResolver) scopeStudioID(ctx context.Context, scopeType, scopeID string) (string, error) {
	if scopeType == model.BudgetScopeStudio {
		return scopeID, nil
	}
	deptID := scopeID
	if scopeType == model.BudgetScopeMember {
		var err error
		deptID, err = r.memberDeptID(ctx, scopeID)
		if err != nil {
			return "", err
		}
	}
	var studioID string
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(dsm.studio_id::text, d.studio_id::text, '')
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		WHERE d.dept_id::text = ?
		LIMIT 1
	`, deptID).Scan(&studioID).Error
	return studioID, err
}

type DatabaseBudgetOwnerResolver struct {
	db          *gorm.DB
	scopeOwners interface {
		ResolveScopeOwnerRecipients(context.Context, string, string) ([]uuid.UUID, error)
	}
}

func NewDatabaseBudgetOwnerResolver(
	db *gorm.DB,
	scopeOwners interface {
		ResolveScopeOwnerRecipients(context.Context, string, string) ([]uuid.UUID, error)
	},
) *DatabaseBudgetOwnerResolver {
	return &DatabaseBudgetOwnerResolver{db: db, scopeOwners: scopeOwners}
}

func (r *DatabaseBudgetOwnerResolver) ResolveOwnerRecipients(
	ctx context.Context,
	budgetAllocationID uuid.UUID,
) ([]uuid.UUID, error) {
	var allocation model.BudgetAllocation
	if err := r.db.WithContext(ctx).Select("scope_type", "scope_id").
		First(&allocation, "id = ?", budgetAllocationID).Error; err != nil {
		return nil, err
	}
	return r.scopeOwners.ResolveScopeOwnerRecipients(ctx, allocation.ScopeType, allocation.ScopeID)
}
