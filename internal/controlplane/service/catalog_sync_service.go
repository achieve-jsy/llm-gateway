package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/seed"
	"github.com/google/uuid"
)

// CatalogRepoInterface 是 catalog_sync 依赖的最小 catalog 持久化接口。
// 抽出 interface 是为了：
//  1. 单测可以注入内存实现，不必拖 sqlite；
//  2. 未来引入 Redis cache 等装饰器时方便拦截。
type CatalogRepoInterface interface {
	BatchUpsert(items []*model.ModelCatalog) error
	GetByID(id uuid.UUID) (*model.ModelCatalog, error)
	GetBySourceModelID(source, sourceModelID string) (*model.ModelCatalog, error)
	ListAll() ([]model.ModelCatalog, error)
	Count() (int64, error)
}

// CatalogSyncModelRepo 抽 ModelRepo 子集（同步/自动绑定任务里需要"列出 catalog 关联/未关联的 model"
// 与 GetByID/Update/SetCatalogID 这几件事）。
type CatalogSyncModelRepo interface {
	ListByCatalogID() ([]model.Model, error)
	ListUnboundFromCatalog() ([]model.Model, error)
	GetByID(id uuid.UUID) (*model.Model, error)
	Update(m *model.Model) error
	SetCatalogID(id, catalogID uuid.UUID) error
}

// CatalogSyncOpts 注入式配置，避免在构造函数里塞太多入参。
type CatalogSyncOpts struct {
	// RemoteURL 默认走 LiteLLM raw GitHub。测试时换成 httptest.Server.URL。
	RemoteURL string
	// HTTPTimeout 默认 15s。
	HTTPTimeout time.Duration
	// MaxRetries 远程失败的最大重试次数（除首次外的"额外"次数）。默认 2。
	MaxRetries int
	// BundledBytes 兜底数据；nil 时仍可工作（仅 SyncFromBundled 会报错）。
	// 生产入口由 main.go 注入 seed.LiteLLMCatalogJSON；测试中可注入小 fixture。
	BundledBytes []byte
	// Source 默认 "litellm"。
	Source string
	// HTTPClient 测试可注入；不传则按 HTTPTimeout 自建。
	HTTPClient *http.Client
}

const (
	defaultLiteLLMURL    = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	defaultHTTPTimeout   = 15 * time.Second
	defaultRetry         = 2
	catalogUserAgent     = "llm-gateway-catalog-sync/1.0"
	defaultCatalogSource = "litellm"
)

// CatalogSyncService 负责把上游 catalog 落库 + 自动同步 models 表的 ctx/max_output。
type CatalogSyncService struct {
	repo      CatalogRepoInterface
	modelRepo CatalogSyncModelRepo
	opts      CatalogSyncOpts

	// mu 同时保护 syncing 标志 + last* 一组追踪字段，
	// 让 SyncStatus 读取与 SyncFromRemote 写入之间不会出现错位的中间状态。
	mu             sync.Mutex
	syncing        bool
	lastStartedAt  time.Time     // 零值表示从未跑过
	lastFinishedAt time.Time     // 零值表示从未完成过
	lastDuration   time.Duration // 最近一次耗时（成功 / 失败都计）
	lastSource     string        // "remote" / "bundled" / "" (尚未跑过 or 失败到无源)
	lastCount      int           // 最近一次写库的条目数；失败时为 0
	lastError      string        // 最近一次失败的错误消息；成功时为 ""
}

// SyncStatus 描述目录同步任务的当前状态 + 最近一次执行结果。
//
// 用途：前端 CatalogSyncPage 在"手动刷新"后用它轮询，显示
//
//	- 是否还在跑（loading 转圈）
//	- 上一次什么时候跑的、耗时多久、用的远程还是 bundled 兜底
//	- 上一次有没有报错
//	- 当前 catalog 里有多少条
//
// 时间字段用 *time.Time，"从未发生"序列化成 null 让前端能干净判定，
// 避免 time.Time 零值 "0001-01-01T00:00:00Z" 这种丑陋占位。
type SyncStatus struct {
	Running        bool       `json:"running"`
	LastStartedAt  *time.Time `json:"last_started_at,omitempty"`
	LastFinishedAt *time.Time `json:"last_finished_at,omitempty"`
	LastDurationMs int64      `json:"last_duration_ms"`
	LastSource     string     `json:"last_source"`
	LastCount      int        `json:"last_count"`
	LastError      string     `json:"last_error"`
	CatalogTotal   int64      `json:"catalog_total"`
}

// NewCatalogSyncService 构造。
func NewCatalogSyncService(repo CatalogRepoInterface, modelRepo CatalogSyncModelRepo, opts CatalogSyncOpts) *CatalogSyncService {
	if opts.RemoteURL == "" {
		opts.RemoteURL = defaultLiteLLMURL
	}
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = defaultHTTPTimeout
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = defaultRetry
	}
	if opts.Source == "" {
		opts.Source = defaultCatalogSource
	}
	// nil 时走 seed 兜底；调用方想明确"无兜底"应该传 []byte{}（非 nil 空切片）。
	if opts.BundledBytes == nil {
		opts.BundledBytes = seed.LiteLLMCatalogJSON
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.HTTPTimeout}
	}
	return &CatalogSyncService{repo: repo, modelRepo: modelRepo, opts: opts}
}

// IsSyncing 暴露当前是否正在同步，便于 handler 给前端做 UI 状态。
func (s *CatalogSyncService) IsSyncing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncing
}

// SyncStatus 返回同步任务的当前 / 最近一次执行状态，外加目录总数。
//
// 实现注意：先拿 mu 读全部 last* 字段（让它们彼此一致），再放锁后做 repo.Count()
// 的数据库 IO——避免持锁做 IO 阻塞别的同步触发。
func (s *CatalogSyncService) SyncStatus(_ context.Context) (*SyncStatus, error) {
	s.mu.Lock()
	st := SyncStatus{
		Running:        s.syncing,
		LastDurationMs: s.lastDuration.Milliseconds(),
		LastSource:     s.lastSource,
		LastCount:      s.lastCount,
		LastError:      s.lastError,
	}
	if !s.lastStartedAt.IsZero() {
		started := s.lastStartedAt
		st.LastStartedAt = &started
	}
	if !s.lastFinishedAt.IsZero() {
		finished := s.lastFinishedAt
		st.LastFinishedAt = &finished
	}
	s.mu.Unlock()

	total, err := s.repo.Count()
	if err != nil {
		return nil, fmt.Errorf("count catalog for sync status: %w", err)
	}
	st.CatalogTotal = total
	return &st, nil
}

// doTrackedSync 包了一层"互斥 + 记录 last* 字段"的逻辑，
// 让所有走它的同步路径（目前是 SyncFromRemote）都能自动暴露给 SyncStatus。
//
// fn 返回 (count, source, err)，其中 source 描述真正用了哪条链路：
// "remote"（远程拉取成功）/ "bundled"（远程失败回退到内置）/ ""（彻底失败）。
func (s *CatalogSyncService) doTrackedSync(fn func() (int, string, error)) (int, error) {
	s.mu.Lock()
	if s.syncing {
		s.mu.Unlock()
		return 0, errors.New("catalog sync already in progress")
	}
	s.syncing = true
	started := time.Now()
	s.lastStartedAt = started
	// 清掉旧的失败信息，避免 UI 把上次的报错继续挂在那。
	s.lastError = ""
	s.lastSource = ""
	s.mu.Unlock()

	count, source, err := fn()

	s.mu.Lock()
	finished := time.Now()
	s.syncing = false
	s.lastFinishedAt = finished
	s.lastDuration = finished.Sub(started)
	s.lastSource = source
	s.lastCount = count
	if err != nil {
		s.lastError = err.Error()
	}
	s.mu.Unlock()

	return count, err
}

// SyncFromRemote 从 RemoteURL 拉取 + retry，失败回退到 BundledBytes。
//
// 错误语义：
//   - 远程成功 → 返回 (n, nil)；SyncStatus.LastSource = "remote"
//   - 远程失败但 bundled 加载成功 → 返回 (n, nil)（fallback OK）；LastSource = "bundled"
//   - 远程失败且 bundled 也无 → 返回 (0, err)；LastError 保留
func (s *CatalogSyncService) SyncFromRemote(ctx context.Context) (int, error) {
	return s.doTrackedSync(func() (int, string, error) {
		body, err := s.fetchRemote(ctx)
		if err != nil {
			slog.Warn("catalog sync remote failed, falling back to bundled", "error", err)
			if len(s.opts.BundledBytes) == 0 {
				return 0, "", fmt.Errorf("remote fetch failed and no bundled fallback: %w", err)
			}
			n, e := s.applyBytes(s.opts.BundledBytes)
			return n, "bundled", e
		}
		n, e := s.applyBytes(body)
		return n, "remote", e
	})
}

// SyncFromBundled 直接走 bundled fixture，不发起任何 HTTP。
func (s *CatalogSyncService) SyncFromBundled() (int, error) {
	if len(s.opts.BundledBytes) == 0 {
		return 0, errors.New("no bundled catalog payload")
	}
	return s.applyBytes(s.opts.BundledBytes)
}

func (s *CatalogSyncService) fetchRemote(ctx context.Context) ([]byte, error) {
	var lastErr error
	tries := 1 + s.opts.MaxRetries
	for i := 0; i < tries; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.opts.RemoteURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", catalogUserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := s.opts.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("upstream HTTP %d", resp.StatusCode)
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

func (s *CatalogSyncService) applyBytes(payload []byte) (int, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return 0, fmt.Errorf("decode catalog payload: %w", err)
	}
	parsed, err := ParseLiteLLMJSON(raw)
	if err != nil {
		return 0, fmt.Errorf("parse catalog: %w", err)
	}

	now := time.Now()
	entries := make([]*model.ModelCatalog, 0, len(parsed))
	for _, p := range parsed {
		entries = append(entries, parsedToModel(p, s.opts.Source, now))
	}
	if err := s.repo.BatchUpsert(entries); err != nil {
		return 0, fmt.Errorf("upsert catalog: %w", err)
	}
	return len(entries), nil
}

// parsedToModel 把 ModelCatalogParsed 翻译为持久层 ModelCatalog。
func parsedToModel(p *ModelCatalogParsed, source string, syncedAt time.Time) *model.ModelCatalog {
	return &model.ModelCatalog{
		ID:                              uuid.New(),
		Source:                          source,
		SourceModelID:                   p.SourceModelID,
		DisplayName:                     p.DisplayName,
		Provider:                        p.Provider,
		Mode:                            p.Mode,
		ContextLength:                   p.ContextLength,
		MaxOutputTokens:                 p.MaxOutputTokens,
		InputPricePerMillion:            p.InputPricePerMillion,
		OutputPricePerMillion:           p.OutputPricePerMillion,
		CacheReadPrice:                  p.CacheReadPrice,
		CacheWritePrice:                 p.CacheWritePrice,
		TierThreshold:                   p.TierThreshold,
		InputPriceAboveTier:             p.InputPriceAboveTier,
		OutputPriceAboveTier:            p.OutputPriceAboveTier,
		SupportsFunctionCalling:         p.SupportsFunctionCalling,
		SupportsVision:                  p.SupportsVision,
		SupportsPromptCaching:           p.SupportsPromptCaching,
		SupportsResponseSchema:          p.SupportsResponseSchema,
		SupportsToolChoice:              p.SupportsToolChoice,
		SupportsParallelFunctionCalling: p.SupportsParallelFunctionCalling,
		DeprecationDate:                 p.DeprecationDate,
		RawPayload:                      p.RawPayload,
		SyncedAt:                        syncedAt,
	}
}

// ApplyAutoUpdates 遍历所有 catalog_id 非空的 model，对未被 override 的字段自动同步。
//
// 策略（D10-B）：
//   - context_length: ctx_overridden=false 时，复制 catalog 当前值
//   - max_output_tokens: max_output_overridden=false 时，复制 catalog 当前值
//   - 价格 / upstream_model_id：永远不动
func (s *CatalogSyncService) ApplyAutoUpdates(ctx context.Context) (int, error) {
	if s.modelRepo == nil {
		return 0, errors.New("model repo not configured")
	}
	models, err := s.modelRepo.ListByCatalogID()
	if err != nil {
		return 0, fmt.Errorf("list models with catalog_id: %w", err)
	}

	updated := 0
	for i := range models {
		m := models[i]
		if m.CatalogID == nil {
			continue
		}
		cat, err := s.repo.GetByID(*m.CatalogID)
		if err != nil || cat == nil {
			continue
		}
		dirty := false
		if !m.CtxOverridden && int64(m.ContextLength) != cat.ContextLength {
			m.ContextLength = int(cat.ContextLength)
			dirty = true
		}
		if !m.MaxOutputOverridden && int64(m.MaxOutputTokens) != cat.MaxOutputTokens {
			m.MaxOutputTokens = int(cat.MaxOutputTokens)
			dirty = true
		}
		if dirty {
			if err := s.modelRepo.Update(&m); err != nil {
				slog.Warn("apply auto update failed", "model_id", m.ID, "error", err)
				continue
			}
			updated++
		}
	}
	return updated, nil
}

// PriceDiffEntry 给 admin 看哪些 model 的价格和 catalog 不一致。
//
// 字段名严格对齐 contracts.json -> /api/v1/model-catalog/diff 的 response_item_fields，
// 前端 web/src/types/catalog.ts PriceDiffEntry 也据此生成。
type PriceDiffEntry struct {
	ModelID                uuid.UUID  `json:"model_id"`
	ModelName              string     `json:"model_name"`
	CatalogID              uuid.UUID  `json:"catalog_id"`
	CatalogSourceModelID   string     `json:"catalog_source_model_id"`
	LocalInputPrice        float64    `json:"local_input_price"`
	LocalOutputPrice       float64    `json:"local_output_price"`
	CatalogInputPrice      float64    `json:"catalog_input_price"`
	CatalogOutputPrice     float64    `json:"catalog_output_price"`
	IsOverridden           bool       `json:"is_overridden"`
	InputDeltaPct          float64    `json:"input_delta_pct"`
	OutputDeltaPct         float64    `json:"output_delta_pct"`
	CatalogDeprecationDate *time.Time `json:"catalog_deprecation_date"`
}

// ComputePriceDiff 仅返回与 catalog 有实际差异的 model：价格不一致 或 catalog 已标记 deprecation_date。
//
// 与 contracts.json $comment_diff_filter 对齐：无差异时 data 为空数组。
func (s *CatalogSyncService) ComputePriceDiff(ctx context.Context) ([]PriceDiffEntry, error) {
	if s.modelRepo == nil {
		return nil, errors.New("model repo not configured")
	}
	models, err := s.modelRepo.ListByCatalogID()
	if err != nil {
		return nil, fmt.Errorf("list models with catalog_id: %w", err)
	}
	out := make([]PriceDiffEntry, 0, len(models))
	for i := range models {
		m := models[i]
		if m.CatalogID == nil {
			continue
		}
		cat, err := s.repo.GetByID(*m.CatalogID)
		if err != nil || cat == nil {
			continue
		}
		priceMismatch := !floatEqual(m.InputPrice, cat.InputPricePerMillion) ||
			!floatEqual(m.OutputPrice, cat.OutputPricePerMillion)
		// 过滤掉无任何差异的 model（既无价差也无 deprecation 提醒）。
		if !priceMismatch && cat.DeprecationDate == nil {
			continue
		}
		out = append(out, PriceDiffEntry{
			ModelID:                m.ID,
			ModelName:              m.Name,
			CatalogID:              cat.ID,
			CatalogSourceModelID:   cat.SourceModelID,
			LocalInputPrice:        m.InputPrice,
			LocalOutputPrice:       m.OutputPrice,
			CatalogInputPrice:      cat.InputPricePerMillion,
			CatalogOutputPrice:     cat.OutputPricePerMillion,
			IsOverridden:           priceMismatch,
			InputDeltaPct:          deltaPct(m.InputPrice, cat.InputPricePerMillion),
			OutputDeltaPct:         deltaPct(m.OutputPrice, cat.OutputPricePerMillion),
			CatalogDeprecationDate: cat.DeprecationDate,
		})
	}
	return out, nil
}

// deltaPct 以 from 为基准计算到 to 的相对变化百分比，from==0 时返回 0 避免 NaN/Inf。
// 结果保留 2 位小数，便于前端直接展示。
func deltaPct(from, to float64) float64 {
	if from == 0 {
		return 0
	}
	pct := (to - from) / from * 100
	// 截断到 2 位小数。
	return math.Round(pct*100) / 100
}

func floatEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// ApplyPriceDiff 把传入 model_ids 的 input_price/output_price 同步为 catalog 当前值。
//
// 容错：任意一个 ID 找不到就立即返回错误（不做"部分成功"，admin 一目了然）。
func (s *CatalogSyncService) ApplyPriceDiff(ctx context.Context, ids []uuid.UUID) (int, error) {
	if s.modelRepo == nil {
		return 0, errors.New("model repo not configured")
	}
	if len(ids) == 0 {
		return 0, errors.New("no model ids provided")
	}
	updated := 0
	for _, id := range ids {
		m, err := s.modelRepo.GetByID(id)
		if err != nil || m == nil {
			return updated, fmt.Errorf("model %s not found", id)
		}
		if m.CatalogID == nil {
			return updated, fmt.Errorf("model %s has no catalog link", id)
		}
		cat, err := s.repo.GetByID(*m.CatalogID)
		if err != nil || cat == nil {
			return updated, fmt.Errorf("catalog for model %s not found", id)
		}
		m.InputPrice = cat.InputPricePerMillion
		m.OutputPrice = cat.OutputPricePerMillion
		if err := s.modelRepo.Update(m); err != nil {
			return updated, fmt.Errorf("update model %s: %w", id, err)
		}
		updated++
	}
	return updated, nil
}

// AutoBindMatch 一对一无歧义、已写入 catalog_id 的成功项。
type AutoBindMatch struct {
	ModelID              uuid.UUID `json:"model_id"`
	ModelName            string    `json:"model_name"`
	CatalogID            uuid.UUID `json:"catalog_id"`
	CatalogSourceModelID string    `json:"catalog_source_model_id"`
}

// AutoBindCandidate 一个 model 的候选 catalog 条目。
type AutoBindCandidate struct {
	CatalogID            uuid.UUID `json:"catalog_id"`
	CatalogSourceModelID string    `json:"catalog_source_model_id"`
	Provider             string    `json:"provider"`
}

// AutoBindAmbiguity 一个 model 命中多于 1 个 catalog 候选，需要 admin 选择。
type AutoBindAmbiguity struct {
	ModelID    uuid.UUID           `json:"model_id"`
	ModelName  string              `json:"model_name"`
	Candidates []AutoBindCandidate `json:"candidates"`
}

// AutoBindResult AutoBind 调用的汇总。
type AutoBindResult struct {
	Matched   []AutoBindMatch     `json:"matched"`
	Ambiguous []AutoBindAmbiguity `json:"ambiguous"`
	Unmatched int                 `json:"unmatched"`
}

// lastSegment 取以 '/' 分隔的最后一段，做大小写不敏感归一化用。
//   - "azure_ai/deepseek-r1"            → "deepseek-r1"
//   - "baseten/moonshotai/Kimi-K2.5"    → "kimi-k2.5"
//   - "deepseek-chat"（无 '/'）         → "deepseek-chat"
func lastSegment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// AutoBind 把本地未绑定 model 与 catalog 条目做"两层精确匹配"，并写库无歧义那部分。
//
// 匹配键收集顺序（每个 key 都做反查 catalog 索引）：
//  1. lower(name)                 — 严格全名（`gpt-4o` ↔ `gpt-4o`）
//  2. lastSegment(name)           — 去前缀同名（`vanchin/deepseek-r1` ↔ `azure_ai/deepseek-r1`）
//  3. lower(upstream_model_id)    — 严格上游 ID
//  4. lastSegment(upstream_model_id)
//
// 候选去重后：
//   - 唯一命中 → 自动写入 catalog_id（matched）
//   - 多个命中 → 不写库，列出候选交给 admin 选（ambiguous）
//   - 0 命中   → unmatched 计数
//
// LiteLLM 目录里同一个上游模型常常以多 provider 前缀重复出现（azure_ai/fireworks_ai/together_ai/…），
// 所以"去前缀"几乎总是命中多条 → 走 ambiguous 让 admin 二选一。
// 已经绑定（catalog_id 非空）的 model 不在处理范围内。
func (s *CatalogSyncService) AutoBind(ctx context.Context) (*AutoBindResult, error) {
	if s.modelRepo == nil {
		return nil, errors.New("model repo not configured")
	}
	models, err := s.modelRepo.ListUnboundFromCatalog()
	if err != nil {
		return nil, fmt.Errorf("list unbound models: %w", err)
	}
	cats, err := s.repo.ListAll()
	if err != nil {
		return nil, fmt.Errorf("list catalog entries: %w", err)
	}
	// 双索引：strict（lower(source_model_id) 整串）+ tail（lower(lastSegment(source_model_id))）。
	strict := make(map[string][]*model.ModelCatalog, len(cats))
	tail := make(map[string][]*model.ModelCatalog, len(cats))
	for i := range cats {
		full := strings.ToLower(strings.TrimSpace(cats[i].SourceModelID))
		if full == "" {
			continue
		}
		strict[full] = append(strict[full], &cats[i])
		t := lastSegment(cats[i].SourceModelID)
		if t != full {
			tail[t] = append(tail[t], &cats[i])
		}
	}

	res := &AutoBindResult{}
	for i := range models {
		m := models[i]
		seen := make(map[uuid.UUID]bool)
		var cands []*model.ModelCatalog
		add := func(c *model.ModelCatalog) {
			if !seen[c.ID] {
				seen[c.ID] = true
				cands = append(cands, c)
			}
		}
		// 严格优先，命中即足够（避免一开始就拉太多 fuzzy 噪声）。
		strictKeys := []string{
			strings.ToLower(strings.TrimSpace(m.Name)),
			strings.ToLower(strings.TrimSpace(m.UpstreamModelID)),
		}
		for _, k := range strictKeys {
			if k == "" {
				continue
			}
			for _, c := range strict[k] {
				add(c)
			}
		}
		// 严格未命中再降级到 tail（去前缀）。
		if len(cands) == 0 {
			tailKeys := []string{
				lastSegment(m.Name),
				lastSegment(m.UpstreamModelID),
			}
			for _, k := range tailKeys {
				if k == "" {
					continue
				}
				for _, c := range tail[k] {
					add(c)
				}
				// 严格匹配同样去看一下 strict 索引里去前缀后能否对应（兼容 catalog 是裸名时的命中）
				for _, c := range strict[k] {
					add(c)
				}
			}
		}

		switch len(cands) {
		case 0:
			res.Unmatched++
		case 1:
			c := cands[0]
			if err := s.modelRepo.SetCatalogID(m.ID, c.ID); err != nil {
				slog.Warn("auto-bind set catalog_id failed", "model_id", m.ID, "error", err)
				res.Unmatched++
				continue
			}
			res.Matched = append(res.Matched, AutoBindMatch{
				ModelID:              m.ID,
				ModelName:            m.Name,
				CatalogID:            c.ID,
				CatalogSourceModelID: c.SourceModelID,
			})
		default:
			list := make([]AutoBindCandidate, 0, len(cands))
			for _, c := range cands {
				list = append(list, AutoBindCandidate{
					CatalogID:            c.ID,
					CatalogSourceModelID: c.SourceModelID,
					Provider:             c.Provider,
				})
			}
			res.Ambiguous = append(res.Ambiguous, AutoBindAmbiguity{
				ModelID:    m.ID,
				ModelName:  m.Name,
				Candidates: list,
			})
		}
	}
	return res, nil
}

// BindingStats 描述「目录侧条目数」与「本地 model 绑定到目录的情况」。
//
// 这是给前端 CatalogSyncPage 空状态做语义判定用的：
//   - CatalogTotal == 0     → 目录是空的，提示去同步；
//   - ModelTotal   == 0     → 还没注册过本地模型，提示去模型管理；
//   - UnboundCount > 0      → 有未绑定的本地模型，提示点「智能绑定」；
//   - BoundCount   > 0
//     且 diff 为空          → 已绑定的模型与目录完全一致，无差异需要处理。
//
// 不掺 diff 行数：diff 仍走 ComputePriceDiff，避免一个接口承担过多语义。
type BindingStats struct {
	CatalogTotal int64 `json:"catalog_total"`
	ModelTotal   int64 `json:"model_total"`
	BoundCount   int64 `json:"bound_count"`
	UnboundCount int64 `json:"unbound_count"`
}

// BindingStats 聚合 catalog 总数 + 本地 model 绑定情况。
//
// 实现说明：bound / unbound 通过现有 List* 接口在内存里 len()，
// 没有为 stats 单独加 Count 方法是为了避免扩大 CatalogSyncModelRepo 接口；
// 本地 model 量级是「人工管理」的几十条，全量加载成本可以忽略。
// 如果未来 model 表膨胀到上千条，再换成 SQL count。
func (s *CatalogSyncService) BindingStats(ctx context.Context) (*BindingStats, error) {
	if s.modelRepo == nil {
		return nil, errors.New("model repo not configured")
	}
	catalogTotal, err := s.repo.Count()
	if err != nil {
		return nil, fmt.Errorf("count catalog: %w", err)
	}
	bound, err := s.modelRepo.ListByCatalogID()
	if err != nil {
		return nil, fmt.Errorf("list bound models: %w", err)
	}
	unbound, err := s.modelRepo.ListUnboundFromCatalog()
	if err != nil {
		return nil, fmt.Errorf("list unbound models: %w", err)
	}
	return &BindingStats{
		CatalogTotal: catalogTotal,
		ModelTotal:   int64(len(bound) + len(unbound)),
		BoundCount:   int64(len(bound)),
		UnboundCount: int64(len(unbound)),
	}, nil
}

// 编译期接口断言
var _ CatalogRepoInterface = (*repository.ModelCatalogRepo)(nil)
var _ CatalogSyncModelRepo = (*repository.ModelRepo)(nil)
