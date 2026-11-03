package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件针对 SQLSTATE 0A000 做静态回归防护：
// PostgreSQL 禁止聚合函数的入参里出现集合返回函数（SRF），例如
//   cardinality(array_agg(DISTINCT unnest(sub.model_list)))
// 会报 "aggregate function calls cannot contain set-returning function calls"。
// 正确写法是先把 SRF 放进子查询 / CTE / LATERAL FROM，再在外层聚合。

// srfCalls 在聚合函数入参里出现即触发 SQLSTATE 0A000 的集合返回函数（小写、含左括号）
var srfCalls = []string{
	"unnest(",
	"generate_series(",
	"generate_subscripts(",
	"jsonb_array_elements(",
	"jsonb_array_elements_text(",
	"json_array_elements(",
	"json_array_elements_text(",
	"jsonb_each(",
	"jsonb_each_text(",
	"json_each(",
	"json_each_text(",
	"jsonb_to_recordset(",
	"json_to_recordset(",
	"jsonb_to_record(",
	"json_to_record(",
	"regexp_split_to_table(",
	"regexp_matches(",
	"string_to_table(",
	"jsonb_path_query(",
}

// aggregateNames 聚合函数名（cardinality 等普通函数不算）
var aggregateNames = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
	"array_agg": true, "string_agg": true, "json_agg": true, "jsonb_agg": true,
	"json_object_agg": true, "jsonb_object_agg": true, "xmlagg": true,
	"bool_and": true, "bool_or": true, "every": true, "bit_and": true, "bit_or": true,
	"stddev": true, "variance": true, "percentile_cont": true, "percentile_disc": true,
	"mode": true, "corr": true, "covar_pop": true, "covar_samp": true,
}

func isSQLIdentChar(c byte) bool {
	return c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// stripLineComments 去掉以 marker 起始的行尾注释
func stripLineComments(s, marker string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, marker); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// stripSQLLineComments 去掉 SQL 的双横线行注释（本项目 SQL 注释内不出现该字面量）
func stripSQLLineComments(sql string) string { return stripLineComments(sql, "--") }

// stripGoLineComments 去掉 Go 源码的 // 行注释：注释里也可能出现「聚合套 SRF」的示例文本，
// 不应被当成违规 SQL（行内 URL 之类含 // 的字面量会被一并截断，对查找无影响）。
func stripGoLineComments(src string) string { return stripLineComments(src, "//") }

// enclosingAggregateName 从 open（某个函数调用的左括号位置）出发向外遍历各层函数调用，
// 若某一层是聚合函数则返回其名字。这样能识别 array_agg(x ORDER BY unnest(y))、
// FILTER (WHERE unnest(z) ...) 以及 sum(length(unnest(x))) 之外的常见写法。
func enclosingAggregateName(s string, open int) string {
	depth := 0
	for i := open - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			j := i - 1
			for j >= 0 && isSQLIdentChar(s[j]) {
				j--
			}
			name := strings.ToLower(strings.TrimSpace(s[j+1 : i]))
			if aggregateNames[name] {
				return name
			}
		}
	}
	return ""
}

// findSRFInsideAggregate 返回第一处「聚合函数入参里含 SRF」的片段，未发现则返回空串。
func findSRFInsideAggregate(sql string) string {
	lower := strings.ToLower(stripSQLLineComments(sql))
	for _, fn := range srfCalls {
		for from := 0; ; {
			pos := strings.Index(lower[from:], fn)
			if pos < 0 {
				break
			}
			abs := from + pos
			if agg := enclosingAggregateName(lower, abs+len(fn)-1); agg != "" {
				end := abs + len(fn)
				if end > len(sql) {
					end = len(sql)
				}
				return agg + "(" + strings.TrimSpace(sql[abs:end]) + "...)"
			}
			from = abs + len(fn)
		}
	}
	return ""
}

// TestBuildUserDailyStatsAggregateSQLNoSRFInAggregate 保证 user_daily_stats 的日聚合
// SQL 不再把 unnest 放进 array_agg（生产上报 SQLSTATE 0A000 的根因）。
func TestBuildUserDailyStatsAggregateSQLNoSRFInAggregate(t *testing.T) {
	sql := buildUserDailyStatsAggregateSQL()

	if bad := findSRFInsideAggregate(sql); bad != "" {
		t.Fatalf("user_daily_stats 聚合 SQL 的聚合函数入参里出现集合返回函数（PG 报 SQLSTATE 0A000）: %s", bad)
	}
	if !strings.Contains(sql, "CROSS JOIN LATERAL unnest(h.model_list)") {
		t.Fatalf("期望先用 LATERAL 展开每小时的 model_list 再聚合，实际 SQL 缺少 CROSS JOIN LATERAL unnest(h.model_list)")
	}
	if !strings.Contains(sql, "cardinality(array_agg(DISTINCT m.model))") {
		t.Fatalf("期望保持「跨小时去重模型数」口径 cardinality(array_agg(DISTINCT m.model))，实际 SQL 中缺失")
	}
}

// TestFindSRFInsideAggregateDetectsLegacyAndAcceptsFixed 交叉验证检查器本身：
// 旧写法必须被判违规，改好的写法不得误报。
func TestFindSRFInsideAggregateDetectsLegacyAndAcceptsFixed(t *testing.T) {
	legacy := []string{
		"SELECT cardinality(array_agg(DISTINCT unnest(sub.model_list))) AS model_count" +
			" FROM (SELECT array_agg(DISTINCT model) AS model_list FROM call_logs GROUP BY user_id) sub GROUP BY 1",
		"SELECT string_agg(generate_series(1, 3)::text, ',') FROM call_logs",
		"SELECT array_agg(m ORDER BY unnest(arr)) FROM t",
	}
	for _, sql := range legacy {
		if bad := findSRFInsideAggregate(sql); bad == "" {
			t.Fatalf("检查器漏报旧写法（用例失效）: %s", sql)
		}
	}

	fixed := []string{
		"SELECT cardinality(array_agg(DISTINCT m.model)) FROM hourly h" +
			" CROSS JOIN LATERAL unnest(h.model_list) AS m(model) GROUP BY h.user_id",
		"SELECT max(c) FROM (SELECT count(*) AS c FROM call_logs GROUP BY user_id) sub",
		"SELECT cardinality(array_agg(DISTINCT model)) FROM call_logs GROUP BY user_id",
	}
	for _, sql := range fixed {
		if bad := findSRFInsideAggregate(sql); bad != "" {
			t.Fatalf("检查器误报合法写法: %s (%s)", sql, bad)
		}
	}
}

// TestRollupPackageSQLNoSRFInAggregate 扫全包生产代码（不含 _test.go），
// 防止其它 rollup 查询重新引入「聚合套 SRF」。
func TestRollupPackageSQLNoSRFInAggregate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		scanned++
		if bad := findSRFInsideAggregate(stripGoLineComments(string(src))); bad != "" {
			t.Errorf("%s: 聚合函数入参里出现集合返回函数（PG 报 SQLSTATE 0A000）: %s", name, bad)
		}
	}
	if scanned == 0 {
		t.Fatalf("未扫描到任何生产代码文件，用例失效")
	}
}
