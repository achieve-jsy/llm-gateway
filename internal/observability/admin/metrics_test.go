package admin

import (
	"strconv"
	"strings"
	"testing"
)

func TestRenderPrometheusMetricsMemoryAndGC(t *testing.T) {
	text := renderPrometheusMetrics()
	values := validatePrometheusExposition(t, text)

	// 任务要求：Go 运行时内存（heap alloc/inuse）、GC 次数、goroutine、RSS、启动时长
	for _, name := range []string{
		"go_memstats_alloc_bytes",
		"go_memstats_heap_alloc_bytes",
		"go_memstats_heap_inuse_bytes",
		"go_memstats_heap_objects",
		"go_memstats_sys_bytes",
		"go_gc_cycles_total",
		"go_memstats_gc_pause_total_seconds",
		"go_goroutines",
		"process_uptime_seconds",
		"process_start_time_seconds",
		"go_runtime_metric",
	} {
		if _, ok := values[name]; !ok {
			t.Errorf("缺少指标 %q", name)
		}
	}

	for _, name := range []string{"go_memstats_heap_alloc_bytes", "go_memstats_heap_inuse_bytes", "go_memstats_sys_bytes"} {
		v, err := strconv.ParseFloat(values[name], 64)
		if err != nil || v <= 0 {
			t.Errorf("%s = %q, 期望 > 0（err=%v）", name, values[name], err)
		}
	}
	if v, err := strconv.ParseFloat(values["process_uptime_seconds"], 64); err != nil || v <= 0 {
		t.Errorf("process_uptime_seconds = %q, 期望 > 0（err=%v）", values["process_uptime_seconds"], err)
	}
	if v, err := strconv.ParseFloat(values["go_goroutines"], 64); err != nil || v < 1 {
		t.Errorf("go_goroutines = %q, 期望 >= 1（err=%v）", values["go_goroutines"], err)
	}

	// runtime/metrics 采样必须以 go_runtime_metric{name="..."} 形式出现
	if !strings.Contains(text, "go_runtime_metric{name=\"/gc/cycles/total:gc-cycles\"}") {
		t.Error("缺少 /gc/cycles/total:gc-cycles 采样")
	}
	if !strings.Contains(text, "go_runtime_metric{name=\"/sched/goroutines:goroutines\"}") {
		t.Error("缺少 /sched/goroutines:goroutines 采样")
	}
	if !strings.Contains(text, "go_runtime_metric{name=\"/gc/gomemlimit:bytes\"}") {
		t.Error("缺少 /gc/gomemlimit:bytes 采样（OOM 排查关键：GOMEMLIMIT 生效值）")
	}

	if !strings.HasSuffix(text, "\n") {
		t.Error("输出必须以换行结尾")
	}
}

func TestRenderPrometheusMetricsRSSWhenAvailable(t *testing.T) {
	rss, source, ok := readProcessRSS()
	if !ok {
		t.Skip("当前平台不支持读取 RSS")
	}
	if rss == 0 {
		t.Fatal("RSS 不应为 0")
	}
	if source == "" {
		t.Fatal("RSS 数据来源不应为空")
	}
	text := renderPrometheusMetrics()
	if !strings.Contains(text, "process_resident_memory_bytes{source=\""+source+"\"}") {
		t.Errorf("缺少 process_resident_memory_bytes{source=%q}", source)
	}
}

func TestRenderPrometheusMetricsOmitsStartDurationBeforeStart(t *testing.T) {
	prev := adminListenDoneAt.Swap(0)
	defer adminListenDoneAt.Store(prev)

	if strings.Contains(renderPrometheusMetrics(), "admin_start_duration_seconds") {
		t.Error("未启动时不应输出 admin_start_duration_seconds")
	}
}

func TestReadRuntimeMetricSamplesSkipsUnknown(t *testing.T) {
	samples := readRuntimeMetricSamples([]string{
		"/nonexistent/metric:bytes",
		"/gc/cycles/total:gc-cycles",
		"/sched/goroutines:goroutines",
	})

	if len(samples) != 2 {
		t.Fatalf("期望采集到 2 个指标, 实际 %d: %+v", len(samples), samples)
	}
	for _, s := range samples {
		if s.Name == "/nonexistent/metric:bytes" {
			t.Error("未知指标不应被采集")
		}
		if !s.IsInt {
			t.Errorf("%s 期望是整型采样", s.Name)
		}
	}
}

func TestReadRuntimeMetricSamplesEmpty(t *testing.T) {
	if got := readRuntimeMetricSamples(nil); len(got) != 0 {
		t.Errorf("空指标列表应返回空结果, 实际 %+v", got)
	}
}

func TestRuntimeMetricNamesAreAllSupported(t *testing.T) {
	// 防止后续升级 Go 版本后指标名失效却静默丢指标
	got := readRuntimeMetricSamples(runtimeMetricNames)
	if len(got) != len(runtimeMetricNames) {
		found := map[string]bool{}
		for _, s := range got {
			found[s.Name] = true
		}
		var missing []string
		for _, n := range runtimeMetricNames {
			if !found[n] {
				missing = append(missing, n)
			}
		}
		t.Errorf("以下 runtime/metrics 指标在当前 Go 版本不可用: %v", missing)
	}
}

func TestEscapeLabelValue(t *testing.T) {
	tests := map[string]string{
		"go1.26.1":     "go1.26.1",
		"a\\b":         "a\\\\b",
		"say \"hi\"":   "say \\\"hi\\\"",
		"line1\nline2": "line1\\nline2",
	}
	for in, want := range tests {
		if got := escapeLabelValue(in); got != want {
			t.Errorf("escapeLabelValue(%q) = %q, 期望 %q", in, got, want)
		}
	}
}
