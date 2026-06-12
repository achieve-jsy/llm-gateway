package derive

// format.go —— 数字/时间格式化（小工具，纯展示用，不做语义）。

import (
	"strconv"
	"strings"
)

func formatFloat(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}

func fmtPct(v float64) string { // 0..1 -> "12.3%"
	return strconv.FormatFloat(v*100, 'f', 1, 64) + "%"
}

func trimTrail(s string) string { return strings.TrimRight(strings.TrimRight(s, "0"), ".") }
