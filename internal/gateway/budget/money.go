package budget

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const microsPerUSD int64 = 1_000_000

func ParseMicroUSD(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty USD amount")
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok || value.Sign() < 0 {
		return 0, fmt.Errorf("invalid non-negative USD amount %q", raw)
	}
	scaled := new(big.Rat).Mul(value, big.NewRat(microsPerUSD, 1))
	quotient := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	remainder := new(big.Int).Rem(scaled.Num(), scaled.Denom())
	if new(big.Int).Lsh(remainder, 1).Cmp(scaled.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("USD amount overflows int64 micro-USD")
	}
	return quotient.Int64(), nil
}

func MicroUSDFromFloat64(value float64) (int64, error) {
	return ParseMicroUSD(strconv.FormatFloat(value, 'f', -1, 64))
}
