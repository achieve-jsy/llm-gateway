package scheduler

import (
	"fmt"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

type ComplexityTier string

const (
	TierSimple    ComplexityTier = "SIMPLE"
	TierMedium    ComplexityTier = "MEDIUM"
	TierComplex   ComplexityTier = "COMPLEX"
	TierReasoning ComplexityTier = "REASONING"
)

const (
	simpleThreshold  = 0.15
	mediumThreshold  = 0.35
	complexThreshold = 0.60
	shortTokenCount  = 15
	longTokenCount   = 400
	followUpChars    = 50
	reasoningForce   = 2
	historyWindow    = 10
)

const (
	wToken     = 0.10
	wCode      = 0.30
	wReasoning = 0.25
	wTechnical = 0.25
	wSimple    = 0.10
	wMultiStep = 0.05
	wQuestion  = 0.05
)

type ComplexityResult struct {
	Tier             ComplexityTier
	Score            float64
	ReasoningMarkers int
	HardFloor        ComplexityTier
	BiasUp           bool
	Detail           string
}

type Classifier interface {
	Classify(*core.ChatRequest) ComplexityResult
}

type ComplexityClassifier struct{}

func NewComplexityClassifier() *ComplexityClassifier {
	return &ComplexityClassifier{}
}

type dimensions struct {
	tokenScore     float64
	codeScore      float64
	reasoningScore float64
	techScore      float64
	simpleScore    float64
	multiScore     float64
	questionScore  float64
	reasoningCount int
}

func (c *ComplexityClassifier) Classify(req *core.ChatRequest) ComplexityResult {
	if req == nil || len(req.Messages) == 0 {
		return ComplexityResult{Tier: TierSimple, Score: 0}
	}

	if tier, ok := c.detectFollowUp(req); ok {
		return ComplexityResult{Tier: tier, Detail: "follow-up-inherit"}
	}

	floor := c.detectHardSignals(req)
	if floor == TierReasoning {
		return ComplexityResult{Tier: TierReasoning, HardFloor: floor, Detail: "hard-reasoning-high"}
	}

	last := getLastUser(req.Messages)
	if last == nil {
		return ComplexityResult{Tier: maxTier(floor, TierSimple), HardFloor: floor, Detail: "no-user-msg"}
	}

	text := core.ExtractTextContent(last.Content)
	dims := scoreDimensions(text)
	baseScore := weightedSum(dims)

	if dims.reasoningCount >= reasoningForce {
		return ComplexityResult{
			Tier:             TierReasoning,
			Score:            baseScore,
			ReasoningMarkers: dims.reasoningCount,
			HardFloor:        floor,
			Detail:           fmt.Sprintf("reasoning-markers-%d", dims.reasoningCount),
		}
	}

	histBoost := c.historyBoost(req.Messages)
	combined := baseScore*0.8 + histBoost*0.2

	sysScore := c.systemScore(req.Messages)
	finalScore := combined*0.75 + sysScore*0.25
	finalScore = clamp01(finalScore)

	softTier := scoreToTier(finalScore)

	if hasBreadthKeywords(text) {
		softTier = maxTier(softTier, TierMedium)
	}

	finalTier := maxTier(floor, softTier)
	detail := fmt.Sprintf("hard=%s soft=%s score=%.3f hist=%.3f sys=%.3f",
		floor, softTier, finalScore, histBoost, sysScore)

	return ComplexityResult{
		Tier:             finalTier,
		Score:            finalScore,
		ReasoningMarkers: dims.reasoningCount,
		HardFloor:        floor,
		Detail:           detail,
	}
}

func (c *ComplexityClassifier) detectHardSignals(req *core.ChatRequest) ComplexityTier {
	floor := TierSimple

	if req.Reasoning != nil {
		switch req.Reasoning.Effort {
		case "high":
			return TierReasoning
		case "medium":
			floor = maxTier(floor, TierComplex)
		}
	}

	switch {
	case len(req.Tools) >= 5:
		floor = maxTier(floor, TierComplex)
	case len(req.Tools) >= 1:
		floor = maxTier(floor, TierMedium)
	}

	if hasImageContent(req.Messages) {
		floor = maxTier(floor, TierComplex)
	}

	if hasStructuredOutput(req) {
		floor = maxTier(floor, TierMedium)
	}

	return floor
}

func (c *ComplexityClassifier) detectFollowUp(req *core.ChatRequest) (ComplexityTier, bool) {
	msgs := req.Messages
	if len(msgs) < 3 {
		return "", false
	}
	last := getLastUser(msgs)
	if last == nil {
		return "", false
	}
	text := core.ExtractTextContent(last.Content)
	if len([]rune(text)) >= followUpChars {
		return "", false
	}
	if !isFollowUpWord(text) {
		return "", false
	}
	prevTier := c.classifyPreviousUser(msgs)
	return prevTier, prevTier != ""
}

func (c *ComplexityClassifier) classifyPreviousUser(msgs []core.Message) ComplexityTier {
	foundCurrent := false
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		if !foundCurrent {
			foundCurrent = true
			continue
		}
		text := core.ExtractTextContent(msgs[i].Content)
		dims := scoreDimensions(text)
		score := clamp01(weightedSum(dims))
		tier := scoreToTier(score)
		if hasBreadthKeywords(text) {
			tier = maxTier(tier, TierMedium)
		}
		if dims.reasoningCount >= reasoningForce {
			tier = TierReasoning
		}
		return tier
	}
	return ""
}

func (c *ComplexityClassifier) historyBoost(msgs []core.Message) float64 {
	var maxBoost float64
	seen := 0
	for i := len(msgs) - 1; i >= 0 && seen < historyWindow; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		seen++
		if seen == 1 {
			continue
		}
		text := core.ExtractTextContent(msgs[i].Content)
		dims := scoreDimensions(text)
		score := clamp01(weightedSum(dims))
		weight := 1.0 / (1.0 + float64(seen-1)*0.3)
		boosted := score * weight
		if boosted > maxBoost {
			maxBoost = boosted
		}
	}
	return maxBoost
}

func (c *ComplexityClassifier) systemScore(msgs []core.Message) float64 {
	for _, m := range msgs {
		if m.Role == "system" {
			text := core.ExtractTextContent(m.Content)
			if text == "" {
				return 0
			}
			dims := scoreDimensions(text)
			return clamp01(weightedSum(dims))
		}
	}
	return 0
}

func scoreDimensions(text string) dimensions {
	tokens := estimateTokens(text)
	lower := stringToLower(text)

	codeHits := countKeywordHits(lower, codeKeywords)
	reasoningHits := countKeywordHits(lower, reasoningKeywords)
	techHits := countKeywordHits(lower, technicalKeywords)
	simpleHits := countKeywordHits(lower, simpleKeywords)
	multiHits := countKeywordHits(lower, multiStepKeywords)
	questionCount := countRunes(text, '?') + countRunes(text, '？')

	var tokenScore float64
	switch {
	case tokens < shortTokenCount:
		tokenScore = 0
	case tokens > longTokenCount:
		tokenScore = 1
	default:
		tokenScore = float64(tokens-shortTokenCount) / float64(longTokenCount-shortTokenCount)
	}

	simpleScore := 1.0 - normalizeHits(simpleHits)

	return dimensions{
		tokenScore:     tokenScore,
		codeScore:      normalizeHits(codeHits),
		reasoningScore: normalizeHits(reasoningHits),
		techScore:      normalizeHits(techHits),
		simpleScore:    simpleScore,
		multiScore:     normalizeHits(multiHits),
		questionScore:  normalizeHits(questionCount),
		reasoningCount: reasoningHits,
	}
}

func weightedSum(d dimensions) float64 {
	return d.tokenScore*wToken +
		d.codeScore*wCode +
		d.reasoningScore*wReasoning +
		d.techScore*wTechnical +
		d.simpleScore*wSimple +
		d.multiScore*wMultiStep +
		d.questionScore*wQuestion
}

func stringToLower(s string) string {
	hasUpper := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}
	return strings.ToLower(s)
}

func countRunes(text string, target rune) int {
	n := 0
	for _, r := range text {
		if r == target {
			n++
		}
	}
	return n
}

func filterByTier(models []*ModelRoutingEntry, tier ComplexityTier) []*ModelRoutingEntry {
	out := make([]*ModelRoutingEntry, 0, len(models))
	for _, m := range models {
		t := ComplexityTier(m.ComplexityTier)
		if t == "" {
			t = TierMedium
		}
		if t == tier {
			out = append(out, m)
		}
	}
	return out
}
