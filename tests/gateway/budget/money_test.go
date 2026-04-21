package budget_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/stretchr/testify/require"
)

func TestParseMicroUSD_ROUND_HALF_UP(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{"six decimals", "1.234567", 1_234_567, false},
		{"seventh decimal rounds up", "1.2345675", 1_234_568, false},
		{"below half micro rounds down", "0.00000049", 0, false},
		{"half micro rounds up", "0.00000050", 1, false},
		{"negative rejected", "-0.000001", 0, true},
		{"invalid rejected", "12.3x", 0, true},
		{"overflow rejected", "9223372036854.775808", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := budget.ParseMicroUSD(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
