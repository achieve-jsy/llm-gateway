package handler_test

// Note: Expansion handler tests that require full RBAC integration are tested
// at the service level in tests/controlplane/service/expansion_*_test.go
// The service-level tests provide comprehensive coverage of:
// - Precheck logic with parent constraints
// - Approval workflows with state transitions
// - Batch operations
// - Notification dispatch
//
// Handler-level tests would require mocking the RBAC middleware which
// is tightly coupled to database queries in BuildAndInjectActor().
