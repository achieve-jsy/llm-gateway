package handler

// Error code constants for all control-plane handler responses.
// Team F keys off these strings for user-facing copy.
// Team I should update contracts.json error_codes blocks to match.
const (
	// 400 invalid_request
	CodeBadRequest           = "bad_request"
	CodeInvalidTagID         = "invalid_tag_id"
	CodeInvalidModelID       = "invalid_model_id"
	CodeModelProviderInvalid = "model_provider_invalid"
	CodeSyncNotSupported     = "sync_not_supported"
	CodeProviderDisabled     = "provider_disabled"
	CodeKeyLimitExceeded     = "key_limit_exceeded"
	CodeInvalidStrategy      = "invalid_strategy"

	// 404 not_found
	CodeProviderNotFound         = "provider_not_found"
	CodeModelNotFound            = "model_not_found"
	CodeCatalogNotFound          = "catalog_not_found"
	CodeKeyNotFound              = "key_not_found"
	CodeGroupNotFound            = "group_not_found"
	CodeAnnouncementNotFound     = "announcement_not_found"
	CodeExemptionNotFound        = "exemption_not_found"
	CodeModelTierNotFound        = "MODEL_TIER_NOT_FOUND"
	CodeBudgetAllocationNotFound = "BUDGET_ALLOCATION_NOT_FOUND"

	// 400 invalid_request (catalog)
	CodeCatalogNotBound    = "catalog_not_bound"
	CodeAlreadyBoundToSame = "already_bound_to_same"

	// 409 conflict
	CodeProviderNameConflict  = "provider_name_conflict"
	CodeModelNameConflict     = "model_name_conflict"
	CodeProviderHasModels     = "provider_has_models"
	CodeSyncInProgress        = "sync_in_progress"
	CodeKeyNameConflict       = "key_name_conflict"
	CodeGroupNameConflict     = "group_name_conflict"
	CodeModelTierNameConflict = "MODEL_TIER_NAME_CONFLICT"
	CodeModelTierReferenced   = "MODEL_TIER_REFERENCED"

	// 422 unprocessable
	CodeStrategySwitchWarning        = "strategy_switch_warning"
	CodeModelTierDisabled            = "MODEL_TIER_DISABLED"
	CodeInvalidModelTierOrder        = "INVALID_MODEL_TIER_ORDER"
	CodeInvalidBudgetPeriod          = "INVALID_BUDGET_PERIOD"
	CodeInvalidMoneyAmount           = "INVALID_MONEY_AMOUNT"
	CodePendingBudgetSchedule        = "PENDING_BUDGET_SCHEDULE_EXISTS"
	CodeBudgetLimitBelowUsed         = "BUDGET_LIMIT_BELOW_USED"
	CodeBudgetUsageUnavailable       = "BUDGET_USAGE_UNAVAILABLE"
	CodeBudgetConfigSyncPending      = "BUDGET_CONFIG_SYNC_PENDING"
	CodeInvalidExpansionDimension    = "INVALID_EXPANSION_DIMENSION"
	CodeDuplicatePendingExpansion    = "DUPLICATE_PENDING_EXPANSION"
	CodeExpansionApprovalUnsupported = "EXPANSION_APPROVAL_UNSUPPORTED"
	CodeExpansionReasonRequired      = "EXPANSION_REASON_REQUIRED"
	CodeExpansionDurationUnsupported = "EXPANSION_DURATION_UNSUPPORTED"

	// 400 key group binding errors
	CodeGroupAlreadyBound = "group_already_bound"
	CodeGroupNotBound     = "group_not_bound"

	// 403 forbidden
	CodeKeyPermission        = "key_permission_denied"
	CodeEntitlementForbidden = "entitlement_forbidden"
	// CodeAttachmentForbidden 反馈附件的「资源归属」拒绝：请求者既不是上传者本人，
	// 也不持有 PermFeedbackManage。刻意区别于 RBAC 的 org.forbidden。
	CodeAttachmentForbidden = "feedback_attachment_forbidden"

	// 404 not_found (feedback attachment)
	CodeAttachmentNotFound = "feedback_attachment_not_found"

	// 404 not_found (entitlement)
	CodeEntitlementNotFound = "entitlement_not_found"

	// 409 conflict (entitlement)
	CodeEntitlementDuplicate = "entitlement_duplicate"

	// 422 unprocessable (entitlement)
	CodeGroupNotControlled = "group_not_controlled"

	// 502 upstream_error
	CodeUpstreamError = "upstream_error"

	// 500 internal_error
	CodeInternalError = "internal_error"
)
