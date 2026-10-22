package admin

import (
	"expvar"
	"net/http"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// processStart 进程启动时刻的近似值（本包在 main 包启动阶段即被导入，误差为进程初始化耗时）。
// 用于输出 process_uptime_seconds / admin_start_duration_seconds。
var processStart = time.Now()

// adminListenDoneAt 观测端点完成监听的时刻（UnixNano）；0 表示尚未启动。
var adminListenDoneAt atomic.Int64

// adminExpvarName expvar 变量名，会出现在 /debug/vars 中。
const adminExpvarName = "admin"

// runtimeMetricNames 额外采集的 runtime/metrics 指标名，覆盖 OOM 排查最关心的几类：
// GC 周期数、GOGC/GOMEMLIMIT 阈值、堆对象与已归还内存、goroutine/GOMAXPROCS。
var runtimeMetricNames = []string{
	"/gc/cycles/total:gc-cycles",
	"/gc/gogc:percent",
	"/gc/gomemlimit:bytes",
	"/gc/heap/allocs:bytes",
	"/gc/heap/frees:bytes",
	"/gc/heap/goal:bytes",
	"/sched/goroutines:goroutines",
	"/sched/gomaxprocs:threads",
	"/memory/classes/heap/objects:bytes",
	"/memory/classes/heap/released:bytes",
	"/memory/classes/total:bytes",
}

func init() {
	// expvar 默认只注册 cmdline / memstats，这里补一条一眼能看懂的进程概览。
	// 加判空保护：重复导入或测试重复初始化时不会 panic（expvar 同名重复注册会 panic）。
	if expvar.Get(adminExpvarName) == nil {
		expvar.Publish(adminExpvarName, expvar.Func(func() any {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return map[string]any{
				"go_version":     runtime.Version(),
				"goroutines":     runtime.NumGoroutine(),
				"start_time":     processStart.UTC().Format(time.RFC3339),
				"uptime_seconds": time.Since(processStart).Seconds(),
				"heap_alloc":     m.HeapAlloc,
				"heap_inuse":     m.HeapInuse,
				"gc_cycles":      m.NumGC,
			}
		}))
	}
}

// runtimeMetricSample 单个 runtime/metrics 采样值（直方图类指标不采集）。
type runtimeMetricSample struct {
	Name  string
	IsInt bool
	Int   uint64
	Float float64
}

// readRuntimeMetricSamples 采集 names 指定的 runtime/metrics 指标。
// 未在当前 Go 版本注册（KindBad）或类型不是 uint64/float64 的条目会被跳过，不会 panic。
func readRuntimeMetricSamples(names []string) []runtimeMetricSample {
	if len(names) == 0 {
		// metrics.Read 对空切片会 panic（内部会索引 samples[0]），这里提前返回
		return nil
	}
	samples := make([]metrics.Sample, len(names))
	for i, n := range names {
		samples[i].Name = n
	}
	metrics.Read(samples)

	out := make([]runtimeMetricSample, 0, len(samples))
	for i := range samples {
		switch samples[i].Value.Kind() {
		case metrics.KindUint64:
			out = append(out, runtimeMetricSample{Name: samples[i].Name, IsInt: true, Int: samples[i].Value.Uint64()})
		case metrics.KindFloat64:
			out = append(out, runtimeMetricSample{Name: samples[i].Name, Float: samples[i].Value.Float64()})
		}
	}
	return out
}

// handleMetrics 输出 Prometheus 文本格式（exposition format 0.0.4）的运行时指标。
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(renderPrometheusMetrics())); err != nil {
		s.log.Warn("admin 观测端点写出 metrics 失败", "error", err)
	}
}

// renderPrometheusMetrics 把 Go 运行时内存/GC/goroutine 与进程信息渲染成 Prometheus 文本。
func renderPrometheusMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	lines := make([]string, 0, 128)
	emit := func(name, help, typ, value string) {
		// 带标签的采样必须写成 name{label="v"} value，指标名与标签之间不能有空格
		sample := name + " " + value
		if strings.HasPrefix(value, "{") {
			sample = name + value
		}
		lines = append(lines,
			"# HELP "+name+" "+help,
			"# TYPE "+name+" "+typ,
			sample,
		)
	}

	// Go 运行时版本
	emit("go_info", "Go 运行时版本信息。", "gauge",
		"{version=\""+escapeLabelValue(runtime.Version())+"\"} 1")

	// goroutine 数量（OOM 场景下判断是否协程堆积）
	emit("go_goroutines", "当前存活的 goroutine 数量。", "gauge",
		strconv.Itoa(runtime.NumGoroutine()))

	// 堆内存：alloc（活跃对象）与 inuse（已分配 span）
	emit("go_memstats_alloc_bytes", "当前堆上活跃对象占用的字节数。", "gauge",
		formatUint(m.Alloc))
	emit("go_memstats_alloc_bytes_total", "进程启动至今累计分配的字节数（含已回收）。", "counter",
		formatUint(m.TotalAlloc))
	emit("go_memstats_heap_alloc_bytes", "堆上活跃对象占用的字节数（等价于 Alloc，GC 视角）。", "gauge",
		formatUint(m.HeapAlloc))
	emit("go_memstats_heap_inuse_bytes", "已分配 span 中至少包含一个活跃对象的字节数。", "gauge",
		formatUint(m.HeapInuse))
	emit("go_memstats_heap_idle_bytes", "已分配但未使用的 span 字节数（可被复用）。", "gauge",
		formatUint(m.HeapIdle))
	emit("go_memstats_heap_released_bytes", "已归还给操作系统的堆内存字节数。", "gauge",
		formatUint(m.HeapReleased))
	emit("go_memstats_heap_objects", "堆上活跃对象数量。", "gauge",
		formatUint(m.HeapObjects))
	emit("go_memstats_heap_sys_bytes", "从操作系统申请的堆内存字节数。", "gauge",
		formatUint(m.HeapSys))
	emit("go_memstats_stack_inuse_bytes", "goroutine 栈占用的字节数。", "gauge",
		formatUint(m.StackInuse))
	emit("go_memstats_sys_bytes", "Go 运行时从操作系统申请的内存总字节数（含堆/栈/运行时结构）。", "gauge",
		formatUint(m.Sys))
	emit("go_memstats_next_gc_bytes", "触发下一次 GC 的堆大小阈值（GOGC/GOMEMLIMIT 的生效结果）。", "gauge",
		formatUint(m.NextGC))
	emit("go_memstats_last_gc_time_seconds", "上一次 GC 结束的 Unix 时间戳（秒，0 表示尚未 GC）。", "gauge",
		formatFloat(float64(m.LastGC)/1e9))
	emit("go_memstats_gc_cpu_fraction", "启动至今 GC 消耗的 CPU 时间占进程总 CPU 时间的比例。", "gauge",
		formatFloat(m.GCCPUFraction))
	emit("go_memstats_gc_pause_total_seconds", "GC STW 暂停累计秒数。", "counter",
		formatFloat(float64(m.PauseTotalNs)/1e9))

	// GC 次数
	emit("go_gc_cycles_total", "已完成的 GC 周期数。", "counter",
		formatUint(uint64(m.NumGC)))

	// runtime/metrics 原始采样：直方图与未知指标会被自动跳过
	if samples := readRuntimeMetricSamples(runtimeMetricNames); len(samples) > 0 {
		lines = append(lines,
			"# HELP go_runtime_metric runtime/metrics 原始采样值（name 为指标全名，单位见指标名）。",
			"# TYPE go_runtime_metric gauge")
		for _, s := range samples {
			value := s.Float
			if s.IsInt {
				value = float64(s.Int)
			}
			lines = append(lines, "go_runtime_metric{name=\""+escapeLabelValue(s.Name)+"\"} "+formatFloat(value))
		}
	}

	// 进程维度
	if rss, source, ok := readProcessRSS(); ok {
		emit("process_resident_memory_bytes", "进程常驻内存字节数（source=statm 为 /proc 即时值，source=maxrss 为 getrusage 峰值近似）。", "gauge",
			"{source=\""+escapeLabelValue(source)+"\"} "+formatUint(rss))
	}
	emit("process_start_time_seconds", "进程启动的 Unix 时间戳（秒）。", "gauge",
		formatFloat(float64(processStart.UnixNano())/1e9))
	emit("process_uptime_seconds", "进程已运行时长（秒）。", "gauge",
		formatFloat(time.Since(processStart).Seconds()))

	if startedAt := adminListenDoneAt.Load(); startedAt > 0 {
		emit("admin_start_duration_seconds", "从进程启动到 admin 观测端点完成监听的耗时（秒）。", "gauge",
			formatFloat(float64(startedAt-processStart.UnixNano())/1e9))
	}

	// 容器维度（仅容器环境可读，用于对齐 cgroup 限额定位 OOM）
	if usage, limit, ok := containerMemory(); ok {
		emit("container_memory_current_bytes", "容器 cgroup 当前内存用量字节数（含 page cache）。", "gauge",
			formatUint(usage))
		if limit > 0 {
			emit("container_memory_limit_bytes", "容器 cgroup 内存上限字节数。", "gauge",
				formatUint(limit))
			emit("container_memory_usage_ratio", "容器内存用量 / 上限（接近 1 时 OOM 风险高）。", "gauge",
				formatFloat(float64(usage)/float64(limit)))
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// formatUint 以十进制输出无符号整数。
func formatUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// formatFloat 以最短往返表示输出浮点数（Prometheus 文本格式允许 1e+06 这类写法）。
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// escapeLabelValue 转义 Prometheus 标签值中的反斜杠、双引号与换行。
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return r.Replace(v)
}
