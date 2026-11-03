package infra

import (
	"sync/atomic"
	"time"
)

// Package-level metrics for the usage collection pipeline.
// Counters and gauge are Prometheus-named, histogram exposes summary stats.
//
// Usage:
//
//	UsageMetricsInstance.Emitted.Inc()
//	UsageMetricsInstance.QueueDepth.Set(42)
//	UsageMetricsInstance.BatchWriteDuration.Observe(0.123)

// UsageMetrics holds all usage collection pipeline metrics.
// Export as a single struct for clean registration and snapshot extraction.
type UsageMetrics struct {
	Emitted                 Counter // usage_events_emitted_total
	Dropped                 Counter // usage_events_dropped_total
	Written                 Counter // usage_events_written_total
	BatchWriteErrors        Counter // batch_write_errors_total
	PriceMissing            Counter // price_missing_total
	UsageMissing            Counter // usage_missing_total
	IdentityMissing         Counter // identity_missing_total
	PricingCacheRefreshErrs Counter // pricing_cache_refresh_errors_total
	StreamUsageMissing      Counter // stream_usage_missing_total

	QueueDepth Gauge // queue_depth

	BatchWriteDuration Histogram // batch_write_duration_seconds
}

// UsageMetricsInstance is the singleton accessible by the entire infra package.
var UsageMetricsInstance UsageMetrics

// Counter is an atomic cumulative counter.
type Counter struct {
	v atomic.Int64
}

func (c *Counter) Inc()            { c.v.Add(1) }
func (c *Counter) Add(n int64)     { c.v.Add(n) }
func (c *Counter) Value() int64    { return c.v.Load() }

// Gauge is an atomic gauge.
type Gauge struct {
	v atomic.Int64
}

func (g *Gauge) Set(v int64)      { g.v.Store(v) }
func (g *Gauge) Inc()             { g.v.Add(1) }
func (g *Gauge) Dec()             { g.v.Add(-1) }
func (g *Gauge) Add(n int64)      { g.v.Add(n) }
func (g *Gauge) Value() int64     { return g.v.Load() }

// Histogram records observations and exposes summary stats (count, sum, min, max).
// Not a full bucketed histogram; callers wanting Prometheus native histograms
// should register a prometheus.Histogram alongside this struct.
type Histogram struct {
	count atomic.Int64
	sumNs atomic.Int64
	minNs atomic.Int64
	maxNs atomic.Int64
}

func (h *Histogram) Observe(seconds float64) {
	ns := int64(seconds * 1e9)
	if ns < 0 {
		ns = 0
	}
	h.count.Add(1)
	h.sumNs.Add(ns)
	for {
		cur := h.minNs.Load()
		if cur != 0 && cur <= ns {
			break
		}
		if h.minNs.CompareAndSwap(cur, ns) {
			break
		}
	}
	for {
		cur := h.maxNs.Load()
		if cur >= ns {
			break
		}
		if h.maxNs.CompareAndSwap(cur, ns) {
			break
		}
	}
}

func (h *Histogram) ObserveDuration(start time.Time) {
	h.Observe(time.Since(start).Seconds())
}

// Snapshot returns a stats summary; all durations are in seconds.
func (h *Histogram) Snapshot() HistogramSnapshot {
	c := h.count.Load()
	sum := float64(h.sumNs.Load()) / 1e9
	min := float64(h.minNs.Load()) / 1e9
	max := float64(h.maxNs.Load()) / 1e9
	avg := 0.0
	if c > 0 {
		avg = sum / float64(c)
	}
	return HistogramSnapshot{Count: c, Sum: sum, Min: min, Max: max, Avg: avg}
}

// HistogramSnapshot is a point-in-time summary of histogram observations.
type HistogramSnapshot struct {
	Count int64   `json:"count"`
	Sum   float64 `json:"sum"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Avg   float64 `json:"avg"`
}

// Snapshot returns a point-in-time copy of all usage metrics.
func (m *UsageMetrics) Snapshot() UsageMetricsSnapshot {
	return UsageMetricsSnapshot{
		Emitted:                 m.Emitted.Value(),
		Dropped:                 m.Dropped.Value(),
		Written:                 m.Written.Value(),
		BatchWriteDuration:      m.BatchWriteDuration.Snapshot(),
		BatchWriteErrors:        m.BatchWriteErrors.Value(),
		QueueDepth:              m.QueueDepth.Value(),
		PriceMissing:            m.PriceMissing.Value(),
		UsageMissing:            m.UsageMissing.Value(),
		IdentityMissing:         m.IdentityMissing.Value(),
		PricingCacheRefreshErrs: m.PricingCacheRefreshErrs.Value(),
		StreamUsageMissing:      m.StreamUsageMissing.Value(),
	}
}

// UsageMetricsSnapshot is a point-in-time copy of all usage metrics.
type UsageMetricsSnapshot struct {
	Emitted                 int64              `json:"usage_events_emitted_total"`
	Dropped                 int64              `json:"usage_events_dropped_total"`
	Written                 int64              `json:"usage_events_written_total"`
	BatchWriteDuration      HistogramSnapshot  `json:"batch_write_duration_seconds"`
	BatchWriteErrors        int64              `json:"batch_write_errors_total"`
	QueueDepth              int64              `json:"queue_depth"`
	PriceMissing            int64              `json:"price_missing_total"`
	UsageMissing            int64              `json:"usage_missing_total"`
	IdentityMissing         int64              `json:"identity_missing_total"`
	PricingCacheRefreshErrs int64              `json:"pricing_cache_refresh_errors_total"`
	StreamUsageMissing      int64              `json:"stream_usage_missing_total"`
}

// Reset zeroes all metrics. Exposed for tests; not intended for production use.
func (m *UsageMetrics) Reset() {
	m.Emitted = Counter{}
	m.Dropped = Counter{}
	m.Written = Counter{}
	m.BatchWriteErrors = Counter{}
	m.PriceMissing = Counter{}
	m.UsageMissing = Counter{}
	m.IdentityMissing = Counter{}
	m.PricingCacheRefreshErrs = Counter{}
	m.StreamUsageMissing = Counter{}
	m.QueueDepth = Gauge{}
	m.BatchWriteDuration = Histogram{}
}
