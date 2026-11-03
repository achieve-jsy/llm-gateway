package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ModelCatalogParsed 是 LiteLLM 单条目解析后的中间表示。
//
// 设计考量：parser 阶段只产出"业务字段 + 阶梯定价 + 原始 payload"，让 sync_service
// 决定是否落库（Upsert）；这样 parser 无 DB 依赖、可单测、可在测试中注入小 fixture，
// 也可以在批处理（CLI 工具）里复用。
type ModelCatalogParsed struct {
	SourceModelID                   string
	Provider                        string
	Mode                            string
	DisplayName                     string
	ContextLength                   int64
	MaxOutputTokens                 int64
	InputPricePerMillion            float64
	OutputPricePerMillion           float64
	CacheReadPrice                  *float64
	CacheWritePrice                 *float64
	ReasoningPrice                  *float64
	CachedPrice                     *float64
	TierThreshold                   *int64
	InputPriceAboveTier             *float64
	OutputPriceAboveTier            *float64
	SupportsFunctionCalling         bool
	SupportsVision                  bool
	SupportsPromptCaching           bool
	SupportsResponseSchema          bool
	SupportsToolChoice              bool
	SupportsParallelFunctionCalling bool
	DeprecationDate                 *time.Time
	RawPayload                      string
}

// 单价为 USD per token，乘以 1e6 得到 per million 的单位。
const tokenPriceScale = 1_000_000.0

// tierKeyRe 抓取阶梯定价的 threshold，例如 input_cost_per_token_above_200k_tokens
//
//	-> 第 1 个分组 = "input"
//	-> 第 2 个分组 = "200" (k 单位，需要乘以 1000)
var tierKeyRe = regexp.MustCompile(`^(input|output)_cost_per_token_above_(\d+)k_tokens$`)

// ParseLiteLLMJSON 解析 LiteLLM 上游 JSON map，返回零或多条 ModelCatalogParsed。
//
// 规则要点：
//   - 跳过 "sample_spec" 这条模板说明（它的 max_input_tokens 是字符串而非数值）；
//   - 跳过解码失败的条目（容错），但保留所有合法 entry；
//   - 单价 *_cost_per_token 字段统一乘 1_000_000 后落 ModelCatalogParsed.InputPricePerMillion；
//   - context_length = max_input_tokens；若不存在或解析失败，回退到 max_tokens；
//   - 阶梯定价：扫描所有 input_cost_per_token_above_<N>k_tokens / output_..._tokens；
//     threshold 取 N*1000；多个 threshold 共存时（例如 1hr）只取最小那条；
//   - 能力位：未声明的字段一律默认 false；
//   - DeprecationDate 支持 "YYYY-MM-DD" 与 RFC3339；解析失败则置 nil（容错）；
//   - 原始 entry 序列化为 JSON 字符串放入 RawPayload，便于以后扩字段不破坏 schema。
func ParseLiteLLMJSON(raw map[string]json.RawMessage) ([]*ModelCatalogParsed, error) {
	out := make([]*ModelCatalogParsed, 0, len(raw))
	for sourceID, payload := range raw {
		if sourceID == "sample_spec" {
			continue
		}
		entry, err := parseOne(sourceID, payload)
		if err != nil {
			// 单条失败不影响整体；调用方拿到的是"已成功解析"的子集
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func parseOne(sourceID string, payload json.RawMessage) (*ModelCatalogParsed, error) {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", sourceID, err)
	}

	entry := &ModelCatalogParsed{
		SourceModelID: sourceID,
		Provider:      stringField(m, "litellm_provider"),
		Mode:          stringField(m, "mode"),
		DisplayName:   sourceID, // LiteLLM 没有独立 display_name 字段，先用 ID
		RawPayload:    string(payload),
	}

	// 上下文 / 输出长度（max_input_tokens / max_output_tokens）
	if v, ok := numberField(m, "max_input_tokens"); ok {
		entry.ContextLength = v
	} else if v, ok := numberField(m, "max_tokens"); ok {
		entry.ContextLength = v
	}
	if v, ok := numberField(m, "max_output_tokens"); ok {
		entry.MaxOutputTokens = v
	} else if v, ok := numberField(m, "max_tokens"); ok {
		entry.MaxOutputTokens = v
	}

	// 单价（per token → per 1M）
	if v, ok := floatField(m, "input_cost_per_token"); ok {
		entry.InputPricePerMillion = v * tokenPriceScale
	}
	if v, ok := floatField(m, "output_cost_per_token"); ok {
		entry.OutputPricePerMillion = v * tokenPriceScale
	}
	if v, ok := floatField(m, "cache_read_input_token_cost"); ok {
		scaled := v * tokenPriceScale
		entry.CacheReadPrice = &scaled
		entry.CachedPrice = &scaled
	}
	if v, ok := floatField(m, "cache_creation_input_token_cost"); ok {
		scaled := v * tokenPriceScale
		entry.CacheWritePrice = &scaled
	}
	if v, ok := floatField(m, "output_cost_per_reasoning_token"); ok {
		scaled := v * tokenPriceScale
		entry.ReasoningPrice = &scaled
	}

	// 阶梯定价：扫一遍 keys
	tierThreshold := int64(-1) // -1 = 未发现
	for k, raw := range m {
		match := tierKeyRe.FindStringSubmatch(k)
		if len(match) != 3 {
			continue
		}
		nKilo, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			continue
		}
		threshold := nKilo * 1000
		// 取最小 threshold
		if tierThreshold == -1 || threshold < tierThreshold {
			tierThreshold = threshold
		}
		var price float64
		switch v := raw.(type) {
		case float64:
			price = v
		case json.Number:
			f, _ := v.Float64()
			price = f
		default:
			continue
		}
		scaled := price * tokenPriceScale
		if match[1] == "input" {
			entry.InputPriceAboveTier = &scaled
		} else {
			entry.OutputPriceAboveTier = &scaled
		}
	}
	if tierThreshold > 0 {
		entry.TierThreshold = &tierThreshold
	}

	// 能力位
	entry.SupportsFunctionCalling = boolField(m, "supports_function_calling")
	entry.SupportsVision = boolField(m, "supports_vision")
	entry.SupportsPromptCaching = boolField(m, "supports_prompt_caching")
	entry.SupportsResponseSchema = boolField(m, "supports_response_schema")
	entry.SupportsToolChoice = boolField(m, "supports_tool_choice")
	entry.SupportsParallelFunctionCalling = boolField(m, "supports_parallel_function_calling")

	// deprecation_date：支持 YYYY-MM-DD 或 RFC3339，否则忽略
	if s := stringField(m, "deprecation_date"); s != "" {
		if t, ok := tryParseDate(s); ok {
			entry.DeprecationDate = &t
		}
	}

	return entry, nil
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func boolField(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// numberField 返回整数（int64）；如果 JSON 数字是 float（带 .0 也行），向下取整。
// 字符串值（例如 sample_spec 的占位说明）会被认为不可解析。
func numberField(m map[string]any, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return i, true
		}
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func floatField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// tryParseDate 容错地把 LiteLLM 的 deprecation_date 解析为 time.Time。
// 已观察到的格式：YYYY-MM-DD（最常见），偶尔会出现 RFC3339；其他文本一律视为不可用。
func tryParseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	formats := []string{"2006-01-02", time.RFC3339, "2006/01/02"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
