package authz_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/authz"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVisibleGroupNames_AccessAllWithinUsableGroups(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:           "k1",
		KeyType:         "service",
		AccessAllGroups: true,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo"},
	}

	names, err := authz.ResolveVisibleGroupNames(context.Background(), id, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek-v3", "qwen-turbo"}, names)
}

func TestResolveVisibleGroupNames_BoundSubsetWithinUsableGroups(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:           "k1",
		KeyType:         "service",
		AccessAllGroups: false,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo", "claude-opus"},
	}

	names, err := authz.ResolveVisibleGroupNames(context.Background(), id, nil)
	require.NoError(t, err)
	assert.Nil(t, names)
}

func TestAuthorizeGroupAccess_ServiceKeyInsideCeiling(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:        "svc-1",
		KeyType:      "service",
		UsableGroups: []string{"deepseek-v3"},
	}

	err := authz.AuthorizeGroupAccess(context.Background(), id, nil, "deepseek-v3")
	assert.NoError(t, err)
}

func TestAuthorizeGroupAccess_ServiceKeyOutsideCeiling(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:        "svc-1",
		KeyType:      "service",
		UsableGroups: []string{"deepseek-v3"},
	}

	err := authz.AuthorizeGroupAccess(context.Background(), id, nil, "claude-opus")
	assert.ErrorIs(t, err, gwmodel.ErrModelGroupUnauthorized)
}
