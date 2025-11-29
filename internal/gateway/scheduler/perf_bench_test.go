package scheduler

import (
	"sort"
	"strconv"
	"testing"
)

// ─────────────────────────────────────────────────────────────
// 基准数据：模拟生产中 3 个模型、Priority 策略的组
// ─────────────────────────────────────────────────────────────

func makeBenchModels(n int) []*ModelRoutingEntry {
	models := make([]*ModelRoutingEntry, n)
	for i := range models {
		models[i] = &ModelRoutingEntry{
			ModelID:      uint(i + 1),
			ModelName:    "model-" + strconv.Itoa(i),
			ChannelID:    uint(i + 100),
			ChannelType:  "openai",
			ProviderName: "aws",
			Priority:     i % 3,
			Weight:       1,
		}
	}
	return models
}

// ─────────────────────────────────────────────────────────────
// B1: routePriority — BEFORE（每次 sort.Slice）vs AFTER（预排序，直接用）
// ─────────────────────────────────────────────────────────────

func routePriorityBefore(healthyModels []*ModelRoutingEntry) *ModelRoutingEntry {
	// 旧实现：每次请求 sort
	copied := make([]*ModelRoutingEntry, len(healthyModels))
	copy(copied, healthyModels)
	sort.Slice(copied, func(i, j int) bool {
		return copied[i].Priority < copied[j].Priority
	})
	minPriority := copied[0].Priority
	samePrio := make([]*ModelRoutingEntry, 0)
	for _, m := range copied {
		if m.Priority == minPriority {
			samePrio = append(samePrio, m)
		} else {
			break
		}
	}
	return samePrio[0]
}

func routePriorityAfter(healthyModels []*ModelRoutingEntry) *ModelRoutingEntry {
	// 新实现：假设已预排序，直接遍历
	minPriority := healthyModels[0].Priority
	samePrio := make([]*ModelRoutingEntry, 0, len(healthyModels))
	for _, m := range healthyModels {
		if m.Priority == minPriority {
			samePrio = append(samePrio, m)
		} else {
			break
		}
	}
	return samePrio[0]
}

func BenchmarkRoutePriority_Before(b *testing.B) {
	models := makeBenchModels(5)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		routePriorityBefore(models)
	}
}

func BenchmarkRoutePriority_After(b *testing.B) {
	// 预先排好序（模拟 ReplaceAll 预排序）
	models := makeBenchModels(5)
	sort.Slice(models, func(i, j int) bool {
		return models[i].Priority < models[j].Priority
	})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		routePriorityAfter(models)
	}
}

// ─────────────────────────────────────────────────────────────
// B2: fmt.Sprintf vs strconv.FormatUint（ModelID/ChannelID 转字符串）
// ─────────────────────────────────────────────────────────────

func BenchmarkRouteDecision_FmtSprintf(b *testing.B) {
	entry := &ModelRoutingEntry{ModelID: 12345, ChannelID: 67890}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = "model_id=" + sprintfInt(entry.ModelID) + "&channel_id=" + sprintfInt(entry.ChannelID)
	}
}

func BenchmarkRouteDecision_Strconv(b *testing.B) {
	entry := &ModelRoutingEntry{ModelID: 12345, ChannelID: 67890}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = "model_id=" + strconv.FormatUint(uint64(entry.ModelID), 10) +
			"&channel_id=" + strconv.FormatUint(uint64(entry.ChannelID), 10)
	}
}

func sprintfInt(v uint) string {
	return strconv.Itoa(int(v)) // 实际 fmt.Sprintf("%d", v) 等价
}
