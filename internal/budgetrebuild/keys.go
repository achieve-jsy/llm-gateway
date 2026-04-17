package budgetrebuild

import "strings"

const (
	LockKey             = "budget:v2:rebuild:lock"
	BillableInflightKey = "budget:v2:billable:inflight"
	ActiveGenerationKey = "budget:v2:active_generation"
	LegacyGeneration    = "legacy"
)

func GenerationKey(generation, key string) string {
	if generation == "" || generation == LegacyGeneration {
		return key
	}
	const prefix = "budget:v2:"
	if !strings.HasPrefix(key, prefix) {
		return key
	}
	return prefix + "gen:" + generation + ":" + strings.TrimPrefix(key, prefix)
}
