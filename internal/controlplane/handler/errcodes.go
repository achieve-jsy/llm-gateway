package handler

// Error code constants for all control-plane handler responses.
// Team F keys off these strings for user-facing copy.
// Team I should update contracts.json error_codes blocks to match.
const (
	// 400 invalid_request
	CodeBadRequest          = "bad_request"
	CodeInvalidTagID        = "invalid_tag_id"
	CodeInvalidModelID      = "invalid_model_id"
	CodeModelProviderInvalid = "model_provider_invalid"
	CodeSyncNotSupported    = "sync_not_supported"
	CodeProviderDisabled    = "provider_disabled"
	CodeKeyLimitExceeded    = "key_limit_exceeded"
	CodeInvalidStrategy     = "invalid_strategy"

	// 404 not_found
	CodeProviderNotFound = "provider_not_found"
	CodeModelNotFound    = "model_not_found"
	CodeCatalogNotFound  = "catalog_not_found"
	CodeKeyNotFound      = "key_not_found"
	CodeGroupNotFound    = "group_not_found"

	// 400 invalid_request (catalog)
	CodeCatalogNotBound      = "catalog_not_bound"
	CodeAlreadyBoundToSame   = "already_bound_to_same"

	// 409 conflict
	CodeProviderNameConflict = "provider_name_conflict"
	CodeModelNameConflict    = "model_name_conflict"
	CodeProviderHasModels    = "provider_has_models"
	CodeSyncInProgress       = "sync_in_progress"
	CodeKeyNameConflict      = "key_name_conflict"
	CodeGroupNameConflict    = "group_name_conflict"

	// 422 unprocessable
	CodeStrategySwitchWarning = "strategy_switch_warning"

	// 400 key group binding errors
	CodeGroupAlreadyBound  = "group_already_bound"
	CodeGroupNotBound      = "group_not_bound"

	// 403 forbidden
	CodeKeyPermission = "key_permission_denied"

	// 502 upstream_error
	CodeUpstreamError = "upstream_error"

	// 500 internal_error
	CodeInternalError = "internal_error"
)
