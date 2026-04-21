package modeldata_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/modeldata"
	"github.com/stretchr/testify/require"
)

func TestMergePricing_ProviderOverrideMergesFieldWise(t *testing.T) {
	inputBase := 2.0
	rerankBase := 0.1
	inputOverride := 3.0
	merged := modeldata.MergePricing(
		&core.ModelPricing{InputPerMtok: &inputBase, RerankPerSearchUnit: &rerankBase},
		&core.ModelPricing{InputPerMtok: &inputOverride},
	)
	require.Equal(t, 3.0, *merged.InputPerMtok)
	require.Equal(t, 0.1, *merged.RerankPerSearchUnit)

	rerankOverride := 0.2
	merged = modeldata.MergePricing(merged, &core.ModelPricing{RerankPerSearchUnit: &rerankOverride})
	require.Equal(t, 0.2, *merged.RerankPerSearchUnit)
	require.Equal(t, 3.0, *merged.InputPerMtok)
}
