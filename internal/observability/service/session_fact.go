package service

import (
	"fmt"
	"sort"
	"time"
)

// 本文件实现 docs/token-usage-context-analysis-design.md §1-§1.4 的会话事实（SessionFact）装配器，
// 并按 docs/context-semantic-analysis-integrated-final.md §3（修复 R1/R2/R3）叠加前缀链证据聚类，
// 不做纯 30 分钟静默切分。
//
// 现状硬约束（两份设计文档均已标注）：call_logs 当前没有 conversation_id / session_id /
// turn_index / reset_flag / task_kind（P2 采集补齐前不可用），也不读请求正文（CSA 语义分析属独立
// 后续阶段）。因此本装配器只能用「元数据」证明前缀链的存在——具体使用 CachedTokens：当后一请求的
// cached_tokens 接近前一请求的 prompt_tokens 时，说明网关/上游的 Prompt Caching 命中了上一轮的完整
// 上下文，这本身就是「后请求的 prompt 包含前请求历史前缀」的可观测证据，等价于设计文档 §3 描述的
// 「前缀哈希链」在不读正文场景下的元数据代理信号。
//
// 三个修复目标（对应设计文档标注的 R1/R2/R3，也是本文件单测覆盖的核心场景）：
//   R1 错切：长任务中途停顿 > 30min，但前缀链证据完整 → 不应被纯超时切开，必须判同会话。
//   R2 错并：同一 (user_id, key_id) 上出现多设备/并发的互不包含累积链 → 不能因为时间接近就被
//            纯超时切分强行合并成一条会话，必须拆成互不穿插的多条会话。
//   R3 误并：批处理式请求（prompt 大小相近、互不累积、cache 几乎不命中）→ 不能因为间隔短就被
//            纯超时切分并成一条长会话，必须逐条标记为 batch，且不参与交互类语义判定。

// CallLogRow 是会话装配器的最小输入行（从 call_logs 投影而来，装配器本身不访问数据库）。
type CallLogRow struct {
	RequestID string
	UserID    string
	KeyID     string
	TS        time.Time
	Model     string

	PromptTokens     int64
	CachedTokens     int64
	CacheWriteTokens int64
	CompletionTokens int64
	ReasoningTokens  int64
	Status           int

	InputCostUSD     float64
	CacheReadCostUSD float64
	OutputCostUSD    float64
	EstCostUSD       float64

	UserAgent string
	IPAddress string
}

// SplitEvidence 是会话边界/成员归属的证据强度分级，对齐设计文档 §1.1 / §3。
type SplitEvidence string

const (
	// SplitEvidenceStrong 前缀链完整衔接（cache 命中证明后请求携带了前请求的历史前缀），
	// 即使间隔 > 30min 也判同会话；允许下游判定突破「疑似」上限到「定性」（§5.4 R14）。
	SplitEvidenceStrong SplitEvidence = "strong"
	// SplitEvidenceWeak 纯 30min 超时推断，无前缀佐证；下游判定强制封顶「疑似」。
	SplitEvidenceWeak SplitEvidence = "weak"
	// SplitEvidenceBatch 批处理式请求：prompt 前缀无重叠、大小相近且不累积；
	// 不参与交互类（3.2/3.3）语义判定。
	SplitEvidenceBatch SplitEvidence = "batch"
)

// SessionFact 是会话事实的内存表示（不落表），对齐设计文档 §1.3。
type SessionFact struct {
	UserID        string
	KeyID         string
	SessionID     string
	Inferred      bool // P1 阶段 call_logs 无 conversation_id，恒为 true
	SplitEvidence SplitEvidence

	Turns     int
	StartedAt time.Time
	EndedAt   time.Time

	ReqIDs    []string
	PromptSeq []int64 // 每轮 prompt_tokens（含缓存）
	CachedSeq []int64 // 每轮 cached_tokens
	ModelSeq  []string

	InputCostSeq  []float64
	CacheCostSeq  []float64
	OutputCostSeq []float64

	// 精确态字段（P2 采集后填充）；P1 近似态恒为零值/不可判定，见设计文档 §1.3 注释。
	ResetFlags []bool   // 恒 false：无法证明发生过 reset，保守判「未 reset」
	TaskKinds  []string // 恒空：任务类型不可判定
}

// IsBatch 返回该会话事实是否为批处理簇；批处理簇不参与交互类（3.2/3.3）语义判定。
func (s SessionFact) IsBatch() bool {
	return s.SplitEvidence == SplitEvidenceBatch
}

// InputMonotonicRatio 输入单调不降比例：相邻 prompt 非降的次数 / (轮次-1)。
// 轮次 < 2 时无相邻对可比，返回 0。
func (s SessionFact) InputMonotonicRatio() float64 {
	if len(s.PromptSeq) < 2 {
		return 0
	}
	nonDecrease := 0
	for i := 1; i < len(s.PromptSeq); i++ {
		if s.PromptSeq[i] >= s.PromptSeq[i-1] {
			nonDecrease++
		}
	}
	return float64(nonDecrease) / float64(len(s.PromptSeq)-1)
}

// CacheHitRate 会话缓存命中率：Σcached / Σprompt。Σprompt<=0 时返回 0（避免除零）。
func (s SessionFact) CacheHitRate() float64 {
	var sumPrompt, sumCached int64
	for _, p := range s.PromptSeq {
		sumPrompt += p
	}
	for _, c := range s.CachedSeq {
		sumCached += c
	}
	if sumPrompt <= 0 {
		return 0
	}
	return float64(sumCached) / float64(sumPrompt)
}

// ShortInputRatio 短输入轮占比：prompt < shortThreshold 的轮次 / 总轮次。总轮次为 0 时返回 0。
func (s SessionFact) ShortInputRatio(shortThreshold int64) float64 {
	if len(s.PromptSeq) == 0 {
		return 0
	}
	cnt := 0
	for _, p := range s.PromptSeq {
		if p < shortThreshold {
			cnt++
		}
	}
	return float64(cnt) / float64(len(s.PromptSeq))
}

// SnowballRatio 末轮/首轮 prompt 增长倍数（滚雪球度量）。轮次 < 2 或首轮为 0 时返回 0（不可判定）。
func (s SessionFact) SnowballRatio() float64 {
	if len(s.PromptSeq) < 2 {
		return 0
	}
	first := s.PromptSeq[0]
	if first <= 0 {
		return 0
	}
	last := s.PromptSeq[len(s.PromptSeq)-1]
	return float64(last) / float64(first)
}

// TaskSwitches 任务切换次数：相邻 TaskKinds 不同的次数；TaskKinds 为空（P1 近似态恒空）
// 返回 -1 表示不可判定，对齐设计文档 §1.4。
func (s SessionFact) TaskSwitches() int {
	if len(s.TaskKinds) == 0 {
		return -1
	}
	switches := 0
	for i := 1; i < len(s.TaskKinds); i++ {
		if s.TaskKinds[i] != s.TaskKinds[i-1] {
			switches++
		}
	}
	return switches
}

// HasReset 是否发生过 reset；P1 近似态 ResetFlags 恒空 → 保守返回 false（无法证明有 reset）。
func (s SessionFact) HasReset() bool {
	for _, r := range s.ResetFlags {
		if r {
			return true
		}
	}
	return false
}

// SessionSplitParams 会话切分/聚类参数，全部可调（默认值见 DefaultSessionSplitParams）。
type SessionSplitParams struct {
	// TimeoutGap 纯超时切分基线阈值（设计文档 §1.2，默认 30min）。
	TimeoutGap time.Duration

	// StrongCacheRatio 前缀链强证据门槛（下界）：后一轮 cached_tokens / 前一轮 prompt_tokens
	// 达到此比例，判定后一轮命中了前一轮的完整历史前缀（strong）。
	StrongCacheRatio float64
	// StrongCacheUpperRatio 前缀链强证据门槛（上界）：cached_tokens 不应大幅超出前一轮
	// prompt_tokens——否则说明该 cached 命中来自另一条更大的历史（并发场景下极易与错误的候选链
	// 撞出虚高比例，见 R2 单测），必须设上界防止把候选链匹配错到不相关的另一条链上。
	StrongCacheUpperRatio float64
	// MinPromptForCacheEvidence 前一轮 prompt_tokens 低于此值时，缓存比例噪声过大，不采信为强证据。
	MinPromptForCacheEvidence int64
	// PromptNonDecreaseTolerance 判定「输入未回落」的容差系数：next.PromptTokens >=
	// prev.PromptTokens * 该系数才算「累积」（允许 system prompt 微调等轻微回落噪声）。
	PromptNonDecreaseTolerance float64

	// BatchMaxGap 批处理判定的最大相邻间隔。
	BatchMaxGap time.Duration
	// BatchSizeTolerance 批处理判定的 prompt 大小相对偏差容差（「相近」的口径）。
	BatchSizeTolerance float64
	// BatchCacheMaxRatio 批处理判定的最大缓存占比（「不累积」的口径：几乎不复用历史）。
	BatchCacheMaxRatio float64
	// BatchMinRun 判定为批处理簇所需的最小连续请求数。
	BatchMinRun int
}

// DefaultSessionSplitParams 返回设计文档给出的默认参数。
func DefaultSessionSplitParams() SessionSplitParams {
	return SessionSplitParams{
		TimeoutGap:                 30 * time.Minute,
		StrongCacheRatio:           0.6,
		StrongCacheUpperRatio:      1.15,
		MinPromptForCacheEvidence:  50,
		PromptNonDecreaseTolerance: 0.95,
		BatchMaxGap:                5 * time.Minute,
		BatchSizeTolerance:         0.25,
		BatchCacheMaxRatio:         0.10,
		BatchMinRun:                3,
	}
}

// AssembleSessionFacts 按 (UserID, KeyID) 分组，装配近似会话事实。
//
// 算法（对齐设计文档 §1.1/§3）：
//  1. 组内按 ts 排序；
//  2. 批处理检测（§3 步骤5）：连续多条 prompt 大小相近、cache 几乎不命中的请求标记为 batch，
//     逐条独立成会话，不参与后续链聚类；
//  3. 前缀链聚类（§3 步骤2-4）：对非 batch 的剩余请求，维护多条「候选累积链」；每条新请求优先匹配
//     命中「强证据」（cache 命中体现前缀包含）的链，即使间隔 > 30min 也接续（修复 R1 错切）；
//     多条链并存时按各自证据独立判断，不因时间接近强行合并（修复 R2 错并）；
//     若无强证据匹配，退化为「弱证据」纯超时匹配（<=30min 且无冲突链）；否则新开一条链。
//  4. 每条链/批簇最终装配为一条 SessionFact，按 StartedAt 排序分配确定性 SessionID。
//
// 返回结果按 (UserID, KeyID, StartedAt) 全局确定性排序，便于测试断言与下游稳定处理。
func AssembleSessionFacts(rows []CallLogRow, params SessionSplitParams) []SessionFact {
	groups := make(map[string][]CallLogRow)
	var groupKeys []string
	for _, r := range rows {
		key := r.UserID + "\x00" + r.KeyID
		if _, ok := groups[key]; !ok {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], r)
	}
	sort.Strings(groupKeys)

	var out []SessionFact
	for _, key := range groupKeys {
		group := groups[key]
		sort.SliceStable(group, func(i, j int) bool {
			if !group[i].TS.Equal(group[j].TS) {
				return group[i].TS.Before(group[j].TS)
			}
			return group[i].RequestID < group[j].RequestID
		})
		out = append(out, assembleGroup(group, params)...)
	}
	return out
}

// assembleGroup 装配单个 (UserID, KeyID) 分组（已按 ts 升序排好）。
func assembleGroup(rows []CallLogRow, params SessionSplitParams) []SessionFact {
	if len(rows) == 0 {
		return nil
	}
	userID, keyID := rows[0].UserID, rows[0].KeyID

	isBatch := detectBatchRun(rows, params)

	var facts []SessionFact

	// 批处理簇：逐条独立成会话，不参与前缀链聚类，不并会话。
	for i, r := range rows {
		if isBatch[i] {
			facts = append(facts, singleTurnFact(userID, keyID, r, SplitEvidenceBatch))
		}
	}

	// 前缀链聚类：仅在非 batch 子序列（保持原有时序）上进行。
	var nonBatch []CallLogRow
	for i, r := range rows {
		if !isBatch[i] {
			nonBatch = append(nonBatch, r)
		}
	}
	facts = append(facts, clusterChains(userID, keyID, nonBatch, params)...)

	// 按 StartedAt 排序（同组内跨批处理/链混排后重新按时间线排列），再分配确定性 SessionID。
	sort.SliceStable(facts, func(i, j int) bool {
		if !facts[i].StartedAt.Equal(facts[j].StartedAt) {
			return facts[i].StartedAt.Before(facts[j].StartedAt)
		}
		// 时间相同则按首个请求 ID 排序，保证结果确定性。
		return firstReqID(facts[i]) < firstReqID(facts[j])
	})
	for seq := range facts {
		facts[seq].SessionID = fmt.Sprintf("inferred-%s-%s-%03d", userID, keyID, seq+1)
	}
	return facts
}

// differentDevice 判断两条请求是否来自明显不同的设备/来源（用于弱证据匹配时的并发保护）。
// 只有当双方都提供了 IP 或 UA 且取值不同才判「不同设备」；任一方缺失时不下判断（避免因数据缺失
// 导致过度拆分），保持与设计文档「单 IP/UA、无并发分叉」判据一致的谨慎口径。
func differentDevice(a, b CallLogRow) bool {
	if a.IPAddress != "" && b.IPAddress != "" && a.IPAddress != b.IPAddress {
		return true
	}
	if a.UserAgent != "" && b.UserAgent != "" && a.UserAgent != b.UserAgent {
		return true
	}
	return false
}

func firstReqID(f SessionFact) string {
	if len(f.ReqIDs) == 0 {
		return ""
	}
	return f.ReqIDs[0]
}

// singleTurnFact 把单条请求装配为一条单轮 SessionFact（用于 batch 簇的逐条独立会话）。
func singleTurnFact(userID, keyID string, r CallLogRow, evidence SplitEvidence) SessionFact {
	return SessionFact{
		UserID:        userID,
		KeyID:         keyID,
		Inferred:      true,
		SplitEvidence: evidence,
		Turns:         1,
		StartedAt:     r.TS,
		EndedAt:       r.TS,
		ReqIDs:        []string{r.RequestID},
		PromptSeq:     []int64{r.PromptTokens},
		CachedSeq:     []int64{r.CachedTokens},
		ModelSeq:      []string{r.Model},
		InputCostSeq:  []float64{r.InputCostUSD},
		CacheCostSeq:  []float64{r.CacheReadCostUSD},
		OutputCostSeq: []float64{r.OutputCostUSD},
	}
}

// openChain 是聚类过程中的候选累积链（尚未终结的会话）。
type openChain struct {
	rows      []CallLogRow
	lastRow   CallLogRow
	allStrong bool // 该链自第二轮起，所有衔接是否都是强证据
}

// clusterChains 对同一分组内的非 batch 请求做前缀链聚类，支持并发多链（修复 R2）。
func clusterChains(userID, keyID string, rows []CallLogRow, params SessionSplitParams) []SessionFact {
	if len(rows) == 0 {
		return nil
	}

	var chains []*openChain

	for _, r := range rows {
		bestStrongIdx := -1
		var bestStrongScore float64
		weakIdx := -1
		var weakGap time.Duration = -1

		for i, c := range chains {
			if isStrongLink(c.lastRow, r, params) {
				score := cacheRatioScore(c.lastRow, r)
				// 同分优先同 IP/UA（对齐设计文档 §1.1「单 IP/UA、无并发分叉」的并发判据）。
				if bestStrongIdx == -1 || score > bestStrongScore+1e-9 ||
					(score > bestStrongScore-1e-9 && c.lastRow.IPAddress == r.IPAddress && chains[bestStrongIdx].lastRow.IPAddress != r.IPAddress) {
					bestStrongIdx = i
					bestStrongScore = score
				}
				continue
			}
			gap := r.TS.Sub(c.lastRow.TS)
			if gap < 0 || gap > params.TimeoutGap {
				continue
			}
			if differentDevice(c.lastRow, r) {
				// 弱证据（纯超时）本身不足以驳倒「不同设备/IP」信号；对齐设计文档 §1.1
				// 「单 IP/UA、无并发分叉」的并发判据——设备不同时不应仅凭时间接近弱并会话，
				// 必须留给强证据（前缀链）去认领，否则会把并发多设备错并成一条会话（修复 R2）。
				continue
			}
			if weakIdx == -1 || gap < weakGap {
				weakIdx = i
				weakGap = gap
			}
		}

		switch {
		case bestStrongIdx != -1:
			c := chains[bestStrongIdx]
			c.rows = append(c.rows, r)
			c.lastRow = r
		case weakIdx != -1:
			c := chains[weakIdx]
			c.rows = append(c.rows, r)
			c.allStrong = false
			c.lastRow = r
		default:
			chains = append(chains, &openChain{rows: []CallLogRow{r}, lastRow: r, allStrong: true})
		}
	}

	facts := make([]SessionFact, 0, len(chains))
	for _, c := range chains {
		facts = append(facts, chainToFact(userID, keyID, c))
	}
	return facts
}

// chainToFact 把一条链终结为 SessionFact；证据分级见函数体注释。
func chainToFact(userID, keyID string, c *openChain) SessionFact {
	turns := len(c.rows)
	evidence := SplitEvidenceWeak
	if turns >= 2 && c.allStrong {
		evidence = SplitEvidenceStrong
	}
	// 单轮会话没有「衔接证据」可言，保守记 weak（不足以支撑突破疑似上限）。

	f := SessionFact{
		UserID:        userID,
		KeyID:         keyID,
		Inferred:      true,
		SplitEvidence: evidence,
		Turns:         turns,
		StartedAt:     c.rows[0].TS,
		EndedAt:       c.rows[turns-1].TS,
	}
	for _, r := range c.rows {
		f.ReqIDs = append(f.ReqIDs, r.RequestID)
		f.PromptSeq = append(f.PromptSeq, r.PromptTokens)
		f.CachedSeq = append(f.CachedSeq, r.CachedTokens)
		f.ModelSeq = append(f.ModelSeq, r.Model)
		f.InputCostSeq = append(f.InputCostSeq, r.InputCostUSD)
		f.CacheCostSeq = append(f.CacheCostSeq, r.CacheReadCostUSD)
		f.OutputCostSeq = append(f.OutputCostSeq, r.OutputCostUSD)
	}
	return f
}

// isStrongLink 判断 next 是否携带了 prev 的完整历史前缀作为证据（前缀链强证据的元数据代理，
// 见文件头注释）：prompt 未回落（允许容差）且 cached_tokens 相对 prev.PromptTokens 的占比达标。
func isStrongLink(prev, next CallLogRow, params SessionSplitParams) bool {
	if prev.PromptTokens <= 0 || next.PromptTokens <= 0 {
		return false
	}
	if prev.PromptTokens < params.MinPromptForCacheEvidence {
		return false
	}
	if float64(next.PromptTokens) < float64(prev.PromptTokens)*params.PromptNonDecreaseTolerance {
		return false
	}
	ratio := float64(next.CachedTokens) / float64(prev.PromptTokens)
	return ratio >= params.StrongCacheRatio && ratio <= params.StrongCacheUpperRatio
}

// cacheRatioScore 用于并发多链候选中挑选「最佳」强证据匹配（比例越高越可信）。
func cacheRatioScore(prev, next CallLogRow) float64 {
	if prev.PromptTokens <= 0 {
		return 0
	}
	return float64(next.CachedTokens) / float64(prev.PromptTokens)
}

// detectBatchRun 扫描分组（已按 ts 排序），标记满足「批处理」特征的连续请求。
// 判据（设计文档 §3 步骤5）：相邻间隔短 + prompt 大小相近 + 互不累积（cache 几乎不命中），
// 且该相邻对本身不构成前缀链强证据（避免把「无缓存但确实在累积」的真实会话误判为批处理）。
// 只有连续满足的run长度 >= BatchMinRun 才判定为批处理，避免偶发的 1-2 条噪声。
func detectBatchRun(rows []CallLogRow, params SessionSplitParams) []bool {
	n := len(rows)
	isBatch := make([]bool, n)
	if n == 0 {
		return isBatch
	}

	i := 0
	for i < n {
		j := i
		for j+1 < n && batchPairOK(rows[j], rows[j+1], params) {
			j++
		}
		runLen := j - i + 1
		if runLen >= params.BatchMinRun && batchRunSizeOK(rows[i:j+1], params) {
			for k := i; k <= j; k++ {
				isBatch[k] = true
			}
		}
		if j == i {
			i++
		} else {
			i = j + 1
		}
	}
	return isBatch
}

// batchPairOK 判断相邻两条请求是否符合「批处理」相邻特征。
func batchPairOK(a, b CallLogRow, params SessionSplitParams) bool {
	gap := b.TS.Sub(a.TS)
	if gap < 0 || gap > params.BatchMaxGap {
		return false
	}
	if isStrongLink(a, b, params) {
		return false // 真实累积链，不是批处理
	}
	if a.PromptTokens <= 0 || b.PromptTokens <= 0 {
		return false
	}
	maxP := a.PromptTokens
	if b.PromptTokens > maxP {
		maxP = b.PromptTokens
	}
	diff := a.PromptTokens - b.PromptTokens
	if diff < 0 {
		diff = -diff
	}
	if float64(diff)/float64(maxP) > params.BatchSizeTolerance {
		return false // 大小不相近（可能是真实累积或无关任务）
	}
	if float64(b.CachedTokens) > float64(b.PromptTokens)*params.BatchCacheMaxRatio {
		return false // 命中了历史前缀，不是「互不累积」
	}
	return true
}

// batchRunSizeOK 对整条候选批处理 run 做整体大小方差校验，避免逐对判断产生的漂移
// （每对相邻都在容差内，但首尾差异已超出容差）误判为批处理。
func batchRunSizeOK(run []CallLogRow, params SessionSplitParams) bool {
	if len(run) == 0 {
		return false
	}
	minP, maxP := run[0].PromptTokens, run[0].PromptTokens
	for _, r := range run {
		if r.PromptTokens < minP {
			minP = r.PromptTokens
		}
		if r.PromptTokens > maxP {
			maxP = r.PromptTokens
		}
	}
	if maxP <= 0 {
		return false
	}
	diff := maxP - minP
	return float64(diff)/float64(maxP) <= params.BatchSizeTolerance*2 // 整体容差略放宽（累计漂移）
}
