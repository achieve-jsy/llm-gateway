package repository_test

import (
	"reflect"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/stretchr/testify/require"
)

func TestExpansionExistingAmountsKeepLegacyTypmod(t *testing.T) {
	modelType := reflect.TypeOf(model.ExpansionRequest{})
	current, ok := modelType.FieldByName("CurrentAmount")
	require.True(t, ok)
	requested, ok := modelType.FieldByName("RequestedAmount")
	require.True(t, ok)
	currentSnapshot, ok := modelType.FieldByName("CurrentAmountSnapshot")
	require.True(t, ok)
	usedSnapshot, ok := modelType.FieldByName("UsedAmountSnapshot")
	require.True(t, ok)

	require.Contains(t, current.Tag.Get("gorm"), "numeric(12,4)")
	require.Contains(t, requested.Tag.Get("gorm"), "numeric(12,4)")
	require.Contains(t, currentSnapshot.Tag.Get("gorm"), "numeric(18,6)")
	require.Contains(t, usedSnapshot.Tag.Get("gorm"), "numeric(18,6)")
}
