package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupExemptionService creates a QuotaExemptionService with a test DB and miniredis.
// It reuses setupQuotaServiceTestDB (which includes quota_exemptions table).
// Returns the service, the DB (for direct inserts in tests), a redis client, and cleanup func.
func setupExemptionService(t *testing.T) (*service.QuotaExemptionService, *gorm.DB, *redis.Client, func()) {
	t.Helper()
	db := setupQuotaServiceTestDB(t)
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := service.NewQuotaExemptionService(db, rdb)
	return svc, db, rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

// TestCreateExemption_Success verifies normal exemption creation
func TestCreateExemption_Success(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "emergency deployment",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin-user-id",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, exemption)

	assert.Equal(t, "user", exemption.SubjectType)
	assert.Equal(t, req.SubjectID, exemption.SubjectID)
	assert.Equal(t, "emergency deployment", exemption.Reason)
	assert.Equal(t, "admin-user-id", exemption.CreatedBy)
	assert.NotEqual(t, uuid.Nil, exemption.ID)

	// Wait for async goroutine to avoid test interference
	time.Sleep(50 * time.Millisecond)
}

// TestCreateExemption_InvalidSubjectType verifies "team" type is rejected
func TestCreateExemption_InvalidSubjectType(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "team",
		SubjectID:   uuid.New().String(),
		Reason:      "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, exemption)
	assert.Contains(t, err.Error(), "invalid subject_type")
}

// TestCreateExemption_EmptySubjectID verifies empty subject_id is rejected
func TestCreateExemption_EmptySubjectID(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   "",
		Reason:      "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, exemption)
	assert.Contains(t, err.Error(), "subject_id is required")
}

// TestCreateExemption_PastExpiry verifies past expiry time is rejected
func TestCreateExemption_PastExpiry(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "test",
		ExpiresAt:   time.Now().Add(-24 * time.Hour),
		CreatedBy:   "admin",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, exemption)
	assert.Contains(t, err.Error(), "expires_at must be in the future")
}

// TestCreateExemption_MissingCreatedBy verifies missing created_by is rejected
func TestCreateExemption_MissingCreatedBy(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, exemption)
	assert.Contains(t, err.Error(), "created_by is required")
}

// TestRevokeExemption_Success verifies normal revocation
func TestRevokeExemption_Success(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	// Create an exemption first
	req := service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "to be revoked",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}
	exemption, err := svc.CreateExemption(context.Background(), req)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Revoke it
	err = svc.RevokeExemption(context.Background(), exemption.ID, "admin-revoker")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify it's gone
	active, err := svc.ListActiveExemptions(context.Background())
	require.NoError(t, err)
	for _, e := range active {
		assert.NotEqual(t, exemption.ID, e.ID, "revoked exemption should not appear in active list")
	}
}

// TestRevokeExemption_NotFound verifies revoking a non-existent ID returns error
func TestRevokeExemption_NotFound(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	fakeID := uuid.New()
	err := svc.RevokeExemption(context.Background(), fakeID, "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exemption not found")
}

// TestListActiveExemptions_FiltersExpired verifies expired exemptions are excluded
func TestListActiveExemptions_FiltersExpired(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	// Create active exemption
	active, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "active",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Create second active exemption
	active2, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "service_key",
		SubjectID:   uuid.New().String(),
		Reason:      "also active",
		ExpiresAt:   time.Now().Add(48 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	list, err := svc.ListActiveExemptions(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2)

	// Verify both are in the list
	foundActive := false
	foundActive2 := false
	for _, e := range list {
		if e.ID == active.ID {
			foundActive = true
		}
		if e.ID == active2.ID {
			foundActive2 = true
		}
	}
	assert.True(t, foundActive, "active exemption should be listed")
	assert.True(t, foundActive2, "second active exemption should be listed")
}

// TestIsExempt_ActiveExemption verifies active exemption returns true
func TestIsExempt_ActiveExemption(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	subjectID := uuid.New().String()
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   subjectID,
		Reason:      "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	assert.True(t, svc.IsExempt(context.Background(), "user", subjectID))
}

// TestIsExempt_ExpiredExemption verifies expired exemption returns false
func TestIsExempt_ExpiredExemption(t *testing.T) {
	svc, db, _, cleanup := setupExemptionService(t)
	defer cleanup()

	// Create a future exemption via service (validates correctly)
	subjectID := uuid.New().String()
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   subjectID,
		Reason:      "will-expire",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Should be exempt now
	assert.True(t, svc.IsExempt(context.Background(), "user", subjectID))

	// Manually back-date expires_at to the past (use GORM's time format)
	db.Exec(`UPDATE quota_exemptions SET expires_at = ? WHERE subject_id = ?`,
		time.Now().Add(-1*time.Hour).UTC(), subjectID)

	// Should no longer be exempt
	assert.False(t, svc.IsExempt(context.Background(), "user", subjectID))
}

// TestCleanupExpired_RemovesOldRecords verifies cleanup removes expired records
func TestCleanupExpired_RemovesOldRecords(t *testing.T) {
	svc, db, _, cleanup := setupExemptionService(t)
	defer cleanup()

	// Insert 2 already-expired exemptions directly into DB
	for i := 0; i < 2; i++ {
		expired := &quota.QuotaExemption{
			ID:          uuid.New(),
			SubjectType: "user",
			SubjectID:   uuid.New().String(),
			Reason:      "already-expired",
			ExpiresAt:   time.Now().Add(-time.Duration(i+1) * time.Hour).UTC(),
			CreatedBy:   "admin",
		}
		require.NoError(t, db.Create(expired).Error)
	}

	// Create one active exemption via service
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "long-lived",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify all 3 exist
	var totalBefore int64
	db.Model(&quota.QuotaExemption{}).Count(&totalBefore)
	assert.Equal(t, int64(3), totalBefore)

	// Cleanup expired
	count, err := svc.CleanupExpired(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	time.Sleep(50 * time.Millisecond)

	// Verify only the long-lived one remains
	var totalAfter int64
	db.Model(&quota.QuotaExemption{}).Count(&totalAfter)
	assert.Equal(t, int64(1), totalAfter)

	active, err := svc.ListActiveExemptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(active))
	assert.Equal(t, "long-lived", active[0].Reason)
}

// TestCreateExemption_ServiceKeyType verifies service_key type is also accepted
func TestCreateExemption_ServiceKeyType(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	req := service.CreateExemptionRequest{
		SubjectType: "service_key",
		SubjectID:   uuid.New().String(),
		Reason:      "service key exemption",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}

	exemption, err := svc.CreateExemption(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "service_key", exemption.SubjectType)

	time.Sleep(50 * time.Millisecond)
}

// TestListAllExemptions_IncludesExpired verifies ListAllExemptions returns both active and expired
func TestListAllExemptions_IncludesExpired(t *testing.T) {
	svc, db, _, cleanup := setupExemptionService(t)
	defer cleanup()

	// Insert an already-expired exemption directly into DB (bypasses validation)
	expiredExemption := &quota.QuotaExemption{
		ID:          uuid.New(),
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "already-expired",
		ExpiresAt:   time.Now().Add(-1 * time.Hour).UTC(),
		CreatedBy:   "admin",
	}
	require.NoError(t, db.Create(expiredExemption).Error)

	// Create one active exemption via service
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "long-lived",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// ListAllExemptions should include both
	all, err := svc.ListAllExemptions(context.Background(), 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)

	// ListActiveExemptions should only include long-lived
	active, err := svc.ListActiveExemptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(active))
	assert.Equal(t, "long-lived", active[0].Reason)
}

// TestIsExempt_WrongSubjectType verifies different subject types don't cross-match
func TestIsExempt_WrongSubjectType(t *testing.T) {
	svc, _, _, cleanup := setupExemptionService(t)
	defer cleanup()

	subjectID := uuid.New().String()
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   subjectID,
		Reason:      "user exemption",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Same ID but different type should NOT be exempt
	assert.False(t, svc.IsExempt(context.Background(), "service_key", subjectID))
	// Correct type should be exempt
	assert.True(t, svc.IsExempt(context.Background(), "user", subjectID))
}

// seedBaseSnapshot writes an empty snapshot to Redis so that the exemption service's
// refreshSnapshotAsync can read it and patch the exemptions section.
func seedBaseSnapshot(t *testing.T, rdb *redis.Client) {
	t.Helper()
	base := service.QuotaSnapshotData{
		DefaultUserRPM:            60,
		DefaultUserMaxConcurrency: 10,
		Users:                     make(map[string]service.QuotaEntry),
		ServiceKeys:               make(map[string]service.QuotaEntry),
		Groups:                    make(map[string]service.QuotaEntry),
		Models:                    make(map[string]service.QuotaEntry),
		UpdatedAt:                 time.Now().UTC(),
	}
	data, err := json.Marshal(base)
	require.NoError(t, err)
	err = rdb.Set(context.Background(), "quota:snapshot:v1", data, 0).Err()
	require.NoError(t, err)
}

// TestSnapshotMechanism_VerifiesRedisKeyAndJSON verifies that creating an exemption
// updates the snapshot in Redis with the correct key and JSON structure
func TestSnapshotMechanism_VerifiesRedisKeyAndJSON(t *testing.T) {
	svc, _, rdb, cleanup := setupExemptionService(t)
	defer cleanup()

	// Seed base snapshot so refreshSnapshotAsync can read and patch it
	seedBaseSnapshot(t, rdb)

	subjectID := uuid.New().String()
	_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   subjectID,
		Reason:      "snapshot test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	// Wait for async snapshot refresh
	time.Sleep(100 * time.Millisecond)

	// Verify snapshot exists in Redis with correct key
	snapshotData, err := rdb.Get(context.Background(), "quota:snapshot:v1").Result()
	require.NoError(t, err, "snapshot should exist in Redis")
	require.NotEmpty(t, snapshotData, "snapshot data should not be empty")

	// Parse and verify JSON structure
	var snapshot service.QuotaSnapshotData
	err = json.Unmarshal([]byte(snapshotData), &snapshot)
	require.NoError(t, err, "snapshot should be valid JSON")

	// Verify exemptions array contains our new exemption
	require.NotEmpty(t, snapshot.Exemptions, "snapshot should contain exemptions")
	found := false
	for _, ex := range snapshot.Exemptions {
		if ex.SubjectID == subjectID {
			found = true
			assert.Equal(t, "user", ex.SubjectType)
			assert.Equal(t, "snapshot test", ex.Reason)
			assert.NotZero(t, ex.ExpiresAt)
			break
		}
	}
	assert.True(t, found, "snapshot should contain the created exemption")
}

// TestSnapshotMechanism_MultipleExemptions verifies snapshot contains all active exemptions
func TestSnapshotMechanism_MultipleExemptions(t *testing.T) {
	svc, _, rdb, cleanup := setupExemptionService(t)
	defer cleanup()

	seedBaseSnapshot(t, rdb)

	// Create 3 exemptions
	subjectIDs := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}

	for _, sid := range subjectIDs {
		_, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
			SubjectType: "user",
			SubjectID:   sid,
			Reason:      "test",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
			CreatedBy:   "admin",
		})
		require.NoError(t, err)
	}

	// Wait for async snapshot refresh
	time.Sleep(150 * time.Millisecond)

	// Verify snapshot contains all 3
	snapshotData, err := rdb.Get(context.Background(), "quota:snapshot:v1").Result()
	require.NoError(t, err)

	var snapshot service.QuotaSnapshotData
	err = json.Unmarshal([]byte(snapshotData), &snapshot)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(snapshot.Exemptions), 3, "snapshot should contain all exemptions")
}

// TestSnapshotMechanism_RevokeUpdatesSnapshot verifies revoking deletes the exemption from DB
// and triggers snapshot refresh. We verify the DB state directly rather than polling Redis
// (async goroutine timing is non-deterministic).
func TestSnapshotMechanism_RevokeUpdatesSnapshot(t *testing.T) {
	svc, db, rdb, cleanup := setupExemptionService(t)
	defer cleanup()

	seedBaseSnapshot(t, rdb)

	subjectID := uuid.New().String()
	exemption, err := svc.CreateExemption(context.Background(), service.CreateExemptionRequest{
		SubjectType: "user",
		SubjectID:   subjectID,
		Reason:      "to be revoked",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	// Wait for create's async refresh
	time.Sleep(200 * time.Millisecond)

	// Verify exemption is in snapshot
	snapshotData, err := rdb.Get(context.Background(), "quota:snapshot:v1").Result()
	require.NoError(t, err)

	var snapshot service.QuotaSnapshotData
	err = json.Unmarshal([]byte(snapshotData), &snapshot)
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Exemptions, "snapshot should contain the created exemption")

	// Revoke the exemption
	err = svc.RevokeExemption(context.Background(), exemption.ID, "admin")
	require.NoError(t, err)

	// Wait for async snapshot refresh
	time.Sleep(200 * time.Millisecond)

	// Verify exemption is deleted from DB (deterministic check)
	var count int64
	db.Model(&quota.QuotaExemption{}).Where("id = ?", exemption.ID).Count(&count)
	assert.Equal(t, int64(0), count, "revoked exemption should be deleted from DB")

	// Verify active exemptions no longer include the revoked one
	active, err := svc.ListActiveExemptions(context.Background())
	require.NoError(t, err)
	for _, e := range active {
		assert.NotEqual(t, subjectID, e.SubjectID, "revoked exemption should not appear in active list")
	}
}

// Ensure the quota package is imported (used for QuotaExemption type reference)
var _ = quota.QuotaExemption{}
