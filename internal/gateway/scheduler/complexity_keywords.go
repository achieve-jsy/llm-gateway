package scheduler

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/example/llm-gateway/internal/core"
)

var codeKeywords = []string{
	"function", "class", "interface", "method", "module", "import", "export",
	"database", "api", "endpoint", "bug", "debug", "error", "exception",
	"code", "program", "algorithm", "compile", "runtime", "stacktrace",
	"函数", "类", "接口", "模块", "导入", "数据库", "报错", "调试", "代码",
	"重构", "异常", "栈", "函数名", "变量", "语法", "编译",
	"```", "github", "git", "npm", "pip",
}

var reasoningKeywords = []string{
	"step by step", "think through", "analyze", "reasoning", "deduce",
	"derive", "infer", "prove", "explain why", "compare and contrast",
	"pros and cons", "let's think", "chain of thought",
	"逐步", "一步步", "推理", "分析", "推导", "论证", "为什么", "如何解释",
	"请思考", "请推理", "深入", "详细拆解", "逻辑", "权衡", "辩证",
}

var technicalKeywords = []string{
	"architecture", "distributed", "encryption", "kubernetes", "microservice",
	"load balancer", "concurrency", "parallelism", "algorithm", "complexity",
	"throughput", "latency", "sharding", "consensus", "raft", "paxos",
	"oauth", "jwt", "tls", "websocket", "grpc", "protobuf",
	"架构", "分布式", "加密", "容器编排", "微服务", "负载均衡", "并发",
	"算法", "复杂度", "吞吐", "限流", "熔断", "一致性", "幂等",
}

var simpleKeywords = []string{
	"what is", "define", "hello", "hi", "thanks", "ok", "yes", "no",
	"good", "nice", "cool", "bye", "help me", "please",
	"什么是", "解释下", "你好", "谢谢", "好的", "嗯", "哦", "明白",
	"了解", "是的", "不是", "再见", "可以吗",
}

var multiStepKeywords = []string{
	"first", "then", "next", "after that", "finally", "secondly", "thirdly",
	"1.", "2.", "3.", "step 1", "step 2", "step 3",
	"首先", "其次", "然后", "最后", "第一步", "第二步", "接着", "接下来",
}

var followUpWords = []string{
	"continue", "retry", "go on", "next", "same", "yes", "no",
	"again", "more", "proceed",
	"继续", "再试", "下一步", "然后", "对的", "是的", "不是", "同上",
	"好的", "嗯", "再说一遍", "展开", "详细一点",
}

var breadthKeywords = []string{
	"list all", "all of", "every", "compare", "pros and cons",
	"differences between", "comprehensive", "exhaustive",
	"列出所有", "逐个", "全部", "对比", "比较", "优缺点",
	"全面", "详尽", "每个", "所有",
}

func extractFullText(msgs []core.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(core.ExtractTextContent(m.Content))
		sb.WriteByte(' ')
	}
	return sb.String()
}

func getLastUser(msgs []core.Message) *core.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return &msgs[i]
		}
	}
	return nil
}

const (
	// cjkTokenWeight 校准过的 CJK 每字符 token 系数，供流式 input_tokens 估算使用。
	// 旧值 1.0 会把中文高估约 1.5 倍（实测 517 中文估计 517 vs 真实 342），
	// 校准为 ≈0.66 后更贴近真实。
	cjkTokenWeight = 0.66
	// routingCJKTokenWeight 复杂度路由口径的 CJK 系数。保留 1.0 以保持既有路由
	// 行为与复杂度测试断言不变；仅流式 input_tokens 估算采用校准后的 cjkTokenWeight。
	routingCJKTokenWeight = 1.0
	// otherTokenChars 非 CJK 字符每 token 的字符数（贴近 BPE，English/Latin）。
	otherTokenChars = 4
	// minimalTokens 非空文本至少返回 1 token，避免 message_start 出现硬编码 0。
	minimalTokens = 1
)

// estimateTokensWithWeight 按给定 CJK 权重估算文本 token 数（CJK/非 CJK 分类的核心实现）。
func estimateTokensWithWeight(text string, cjkWeight float64) int {
	if text == "" {
		return 0
	}
	cjk := 0
	other := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		} else {
			other++
		}
	}
	return int(float64(cjk)*cjkWeight) + other/otherTokenChars
}

func estimateTokens(text string) int {
	return estimateTokensWithWeight(text, routingCJKTokenWeight)
}

// EstimateTokens 估算一段文本粗略的 token 数（复杂度路由口径：CJK 按 1 token/字，
// 其余按 4 字符 1 token）。提供给外部在缺少真实 usage 时生成一个非零的
// input_tokens 粗略值。内部实现与 estimateTokens 一致。
func EstimateTokens(text string) int {
	return estimateTokens(text)
}

// EstimateTokensCalibrated 为 /v1/messages 流式 message_start.input_tokens 提供更贴近
// 真实的估算：CJK 按 ≈0.66 token/字（校准值），其余按 4 字符 1 token，并保证非空文本
// 至少返回 1。相比 EstimateTokens（CJK=1.0）可把中文高估从 ~1.5 倍收窄到 ~1.0 倍。
func EstimateTokensCalibrated(text string) int {
	tokens := estimateTokensWithWeight(text, cjkTokenWeight)
	if tokens < minimalTokens {
		return minimalTokens
	}
	return tokens
}

func countKeywordHits(textLower string, keywords []string) int {
	hits := 0
	for _, kw := range keywords {
		if strings.Contains(textLower, kw) {
			hits++
		}
	}
	return hits
}

func hasImageContent(msgs []core.Message) bool {
	for _, m := range msgs {
		if core.HasNonTextContent(m.Content) {
			return true
		}
	}
	return false
}

func hasStructuredOutput(req *core.ChatRequest) bool {
	raw := req.ExtraFields.Lookup("response_format")
	if raw == nil {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return strings.Contains(string(raw), "json_object") ||
			strings.Contains(string(raw), "json_schema")
	}
	if t, ok := obj["type"]; ok {
		var typeStr string
		if err := json.Unmarshal(t, &typeStr); err == nil {
			return typeStr == "json_object" || typeStr == "json_schema"
		}
	}
	return false
}

func isFollowUpWord(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	for _, w := range followUpWords {
		if strings.HasPrefix(trimmed, w) {
			return true
		}
	}
	return false
}

func hasBreadthKeywords(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range breadthKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func maxTier(a, b ComplexityTier) ComplexityTier {
	if tierIndex(a) > tierIndex(b) {
		return a
	}
	return b
}

func tierIndex(t ComplexityTier) int {
	switch t {
	case TierSimple:
		return 0
	case TierMedium:
		return 1
	case TierComplex:
		return 2
	case TierReasoning:
		return 3
	default:
		return 0
	}
}

func scoreToTier(score float64) ComplexityTier {
	switch {
	case score < simpleThreshold:
		return TierSimple
	case score < mediumThreshold:
		return TierMedium
	case score < complexThreshold:
		return TierComplex
	default:
		return TierReasoning
	}
}

func upgradeTier(t ComplexityTier) ComplexityTier {
	switch t {
	case TierSimple:
		return TierMedium
	case TierMedium:
		return TierComplex
	case TierComplex:
		return TierReasoning
	default:
		return t
	}
}

func normalizeHits(hits int) float64 {
	if hits <= 0 {
		return 0
	}
	if hits >= 5 {
		return 1
	}
	return float64(hits) / 5.0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
