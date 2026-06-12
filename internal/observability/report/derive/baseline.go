package derive

// baseline.go —— 28 天稳健基线 + 同岗位匿名分位（判断层，可复算）。
//
// 铁律：
//   - 只用 active（非空/样本>=下限）日；样本不足 → supported=false，不产出判断。
//   - σ_rob = 1.4826 × MAD；dev_sigma = (current - median)/σ_rob；除零取 0（无偏离）。
//   - 同岗只给匿名分位，最低样本 min_sample；不足按优先级回退，仍 <min_sample 则 supported=false。

import (
	"math"
	"sort"
)

// ---- 统计原语 ----

func Median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func MAD(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	med := Median(vals)
	devs := make([]float64, len(vals))
	for i, v := range vals {
		devs[i] = math.Abs(v - med)
	}
	return Median(devs)
}

func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// PercentRank 返回 value 在 sortedVals（升序）分布中的分位 0..1（逆 ECDF）。
// 语义：value 超过池内多大比例（值越高 rank 越高）。
func PercentRank(sortedVals []float64, value float64) float64 {
	if len(sortedVals) == 0 {
		return 0
	}
	le := 0
	for _, v := range sortedVals {
		if v <= value {
			le++
		}
	}
	// 为避免全等值误标，用 (le-1)/(n-1)（n>1）更接近"严格超过"；此处用 le/n 保守。
	return float64(le) / float64(len(sortedVals))
}

func RobustZ(value, median, mad float64) float64 {
	sig := 1.4826 * mad
	if sig == 0 {
		if value == median {
			return 0
		}
		if value > median {
			return math.Inf(1)
		}
		return math.Inf(-1)
	}
	return (value - median) / sig
}

// ---- 28 天稳健基线 ----

// Baseline 计算 28 天稳健基线。
func Baseline(vals []float64, current float64, windowDays, minSamples int, currentWindow string) BaseResult {
	if len(vals) < minSamples {
		return BaseResult{Supported: false, Note: "基线有效样本 <" + itoa(minSamples) + " 天，无依据不做基线判定（基线窗=" + currentWindow + "）。", WindowDays: windowDays, SampleDays: len(vals), Current: current, CurrentWindow: currentWindow, Trend: "flat"}
	}
	sort.Float64s(vals)
	med := Median(vals)
	mad := MAD(vals)
	sig := 1.4826 * mad
	dev := RobustZ(current, med, mad)
	rank := PercentRank(vals, current)

	trend := "flat"
	if dev > 0.5 {
		trend = "up"
	} else if dev < -0.5 {
		trend = "down"
	}
	return BaseResult{
		Supported:     true,
		WindowDays:    windowDays,
		SampleDays:    len(vals),
		Current:       current,
		Median:        med,
		MAD:           mad,
		SigmaRob:      sig,
		DevSigma:      dev,
		Percentile:    rank,
		Trend:         trend,
		CurrentWindow: currentWindow,
		Note:          "28 天稳健基线：中位数 + 1.4826×MAD；分位为当前值在历史分布中的位次；σ 为稳健 z。基线窗=" + currentWindow + "。",
	}
}

// BaseResult 28 天基线结果（report.Baseline 的对齐物；由 derive.Derive 装配为 contract 类型）。
type BaseResult struct {
	Supported     bool
	WindowDays    int
	SampleDays    int
	Current       float64
	Median        float64
	MAD           float64
	SigmaRob      float64
	DevSigma      float64
	Percentile    float64
	Trend         string
	CurrentWindow string
	Note          string
}

// ---- 同岗位匿名分位 ----

// PeerResult 同岗分位结果（report.Peer 的对齐物）。
type PeerResult struct {
	Supported          bool
	Metric             string
	PoolType           string
	PoolN              int
	SampleN            int
	Current            float64
	PercentileRank     float64
	Band               string
	P25, P50, P75, P90 float64
	VsPeer             string
	Anonymized         bool
	Note               string
}

// PeerPercentile 用 cohort 中有效成员计算 current 的同岗分位。
// direction: "cost"（值越高越差/偏贵）| "efficiency"（值越高越好）。
func PeerPercentile(metric, poolType, direction string, members []PortfolioMember, current float64, minSample int) PeerResult {
	_ = direction
	vals := make([]float64, 0, len(members))
	for _, m := range members {
		if m.Valid {
			vals = append(vals, m.Value)
		}
	}
	res := PeerResult{Metric: metric, PoolType: poolType, PoolN: len(members), SampleN: len(vals), Current: current, Anonymized: true}
	if len(vals) < minSample {
		res.Supported = false
		res.Note = "同岗样本 <" + itoa(minSample) + "，按文档回退后仍不足，不产出分位（避免跨岗位比较）。"
		return res
	}
	sort.Float64s(vals)
	res.Supported = true
	res.P25 = Percentile(vals, 25)
	res.P50 = Percentile(vals, 50)
	res.P75 = Percentile(vals, 75)
	res.P90 = Percentile(vals, 90)
	res.PercentileRank = PercentRank(vals, current)
	res.Band = BandOf(res.PercentileRank)
	if res.P50 != 0 {
		res.VsPeer = pctSigned((current - res.P50) / res.P50)
	}
	res.Note = "匿名分位，不指向具体人、不跨岗位排名。"
	return res
}

// PortfolioMember 对标池成员（匿名）。
type PortfolioMember struct {
	Valid bool
	Value float64
}

// BandOf 将 0..1 分位映射到 P 段。
func BandOf(r float64) string {
	switch {
	case r < 0.25:
		return "P0-P25"
	case r < 0.5:
		return "P25-P50"
	case r < 0.75:
		return "P50-P75"
	case r < 0.9:
		return "P75-P90"
	default:
		return "P90-P100"
	}
}

func itoa(i int) string {
	return strconvItoa(i)
}

func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

func pctSigned(f float64) string {
	const digits = "0123456789"
	abs := f
	sign := "+"
	if abs < 0 {
		sign = "-"
		abs = -abs
	}
	// 百分数保留 0 位
	pct := int(math.Round(abs * 100))
	var b [12]byte
	p := len(b)
	if pct == 0 {
		p--
		b[p] = '0'
	}
	for pct > 0 {
		p--
		b[p] = digits[pct%10]
		pct /= 10
	}
	p--
	b[p] = '%'
	p--
	b[p] = byte(sign[0])
	return string(b[p:])
}
