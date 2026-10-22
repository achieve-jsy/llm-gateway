package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	controlmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	controlpricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/example/llm-gateway/internal/core"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

// 自动触发的快照重建参数。
//
// 背景（2026-10-16 生产 OOM）：一轮模型健康检测里可能有几十个模型被自动禁用，
// 每个禁用都触发一次「全量 ListAvailable + 凭证解密 + 写 Redis」的重建，
// 72 路并发全量重建直接把 control-plane 的 2GiB cgroup 打爆。
// 因此自动禁用/恢复路径改为「单飞 + 合并去抖」调度，见 ScheduleRefresh。
const (
	// defaultSnapshotRefreshMinInterval 相邻两次自动快照重建之间的最小间隔。
	// 每次重建都是全量重建，天然会带上此刻的全部最新状态，所以错峰到达的禁用事件
	// 只需要合并进下一次重建；2s 的节流把一波突发限制在 1~2 次重建以内。
	// gateway 侧 ProviderLoader 还有 30s reconcile 兜底，2s 延迟不影响最终一致性。
	defaultSnapshotRefreshMinInterval = 2 * time.Second

	// snapshotRebuildTimeout 单次自动快照重建的硬超时。
	// 正常重建在秒级完成；超时说明 PG/Redis 异常，放弃本次重建（后续请求会再补一次），
	// 避免一条卡死的重建永久占住单飞位置。
	snapshotRebuildTimeout = 30 * time.Second
)

// ProviderSnapshotRefresher rebuilds the shared gateway provider snapshot.
type ProviderSnapshotRefresher struct {
	modelRepo ModelRepoInterface
	rdb       *redis.Client

	// 以下字段仅服务于 ScheduleRefresh 的合并去抖 + 单飞调度。
	mu           sync.Mutex
	rebuilding   bool          // 是否已有一次重建在飞（单飞闸门）
	pending      bool          // 在飞/节流等待期间是否又收到请求（合并为最多一次补跑）
	idleCh       chan struct{} // 由忙转闲时关闭，供 WaitIdle 等待
	autoRebuilds int64         // 自动调度实际执行的重建次数（可观测性 + 测试）
	lastRebuild  time.Time     // 上一次重建完成时刻（用于最小间隔节流）
	minInterval  time.Duration // 相邻两次自动重建的最小间隔，0 表示不节流
}

// NewProviderSnapshotRefresher creates a new snapshot refresher.
func NewProviderSnapshotRefresher(modelRepo ModelRepoInterface, rdb *redis.Client) *ProviderSnapshotRefresher {
	return &ProviderSnapshotRefresher{
		modelRepo:   modelRepo,
		rdb:         rdb,
		minInterval: defaultSnapshotRefreshMinInterval,
	}
}

// ScheduleRefresh 请求一次异步的全量快照重建（自动禁用/恢复后调用）。
//
// 语义（单飞 + 合并去抖）：
//   - 同一时刻最多只有 1 个重建在飞；重建本身是全量的，不需要按事件逐个重建；
//   - 在飞期间到来的请求只置 pending 标记，本轮结束后最多补跑 1 次，
//     因此一波突发（如一轮检测里 72 个模型被自动禁用）最多产生 2 次重建；
//   - 相邻两次重建之间有 minInterval 节流，避免禁用事件错峰到达时退化成逐个重建；
//   - 调度 goroutine 在没有任何待处理请求时立即退出，不会为每个事件常驻；
//   - ctx 仅用于「是否还需要重建」的判断（已取消则直接放弃）与节流等待的中断，
//     单次重建使用独立的 30s 超时上下文，不会被调用方的短生命周期 ctx 掐断，
//     避免「已经禁用但快照没更新」的状态残留。
func (r *ProviderSnapshotRefresher) ScheduleRefresh(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		// 调用方已取消（例如检测轮次被中止），本次重建不再有意义
		return
	}

	r.mu.Lock()
	if r.rebuilding {
		// 已有重建在飞：只登记「结束后再补跑一次」
		r.pending = true
		r.mu.Unlock()
		return
	}
	r.rebuilding = true
	if r.idleCh == nil {
		r.idleCh = make(chan struct{})
	}
	r.mu.Unlock()

	go r.runScheduledRefresh(ctx)
}

// runScheduledRefresh 串行执行重建，直到没有待处理请求后退出（唯一的调度 goroutine）。
func (r *ProviderSnapshotRefresher) runScheduledRefresh(ctx context.Context) {
	for {
		if wait := r.throttleDelay(); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				r.finishRefresh()
				return
			}
		}

		// 先把 pending 归零：重建期间新到的请求会重新置位，从而触发一次补跑
		r.mu.Lock()
		r.pending = false
		r.autoRebuilds++
		r.mu.Unlock()

		rebuildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), snapshotRebuildTimeout)
		_, err := r.RefreshAvailableSnapshot(rebuildCtx)
		cancel()
		if err != nil {
			slog.Warn("scheduled provider snapshot refresh failed", "error", err)
		}

		r.mu.Lock()
		r.lastRebuild = time.Now()
		done := !r.pending
		r.mu.Unlock()
		if done {
			r.finishRefresh()
			return
		}
	}
}

// throttleDelay 返回距离允许下一次重建还需等待的时间（无历史重建或已过间隔则返回 0）。
func (r *ProviderSnapshotRefresher) throttleDelay() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.minInterval <= 0 || r.lastRebuild.IsZero() {
		return 0
	}
	if wait := r.minInterval - time.Since(r.lastRebuild); wait > 0 {
		return wait
	}
	return 0
}

// finishRefresh 释放单飞闸门并唤醒 WaitIdle 等待者。
func (r *ProviderSnapshotRefresher) finishRefresh() {
	r.mu.Lock()
	r.rebuilding = false
	if r.idleCh != nil {
		close(r.idleCh)
		r.idleCh = nil
	}
	r.mu.Unlock()
}

// WaitIdle 等待自动调度队列空闲（测试与优雅停机使用）。
// 返回 true 表示在超时前已无在飞/待处理的重建。
func (r *ProviderSnapshotRefresher) WaitIdle(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	if !r.rebuilding {
		r.mu.Unlock()
		return true
	}
	ch := r.idleCh
	r.mu.Unlock()
	if ch == nil {
		// 理论上不会发生（rebuilding=true 时必有 idleCh），兜底避免误判
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

// AutoRebuildCount 返回 ScheduleRefresh 调度实际执行的全量重建次数（可观测性/测试用）。
func (r *ProviderSnapshotRefresher) AutoRebuildCount() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.autoRebuilds
}

// RefreshAvailableSnapshot rebuilds and stores the latest available providers snapshot.
func (r *ProviderSnapshotRefresher) RefreshAvailableSnapshot(ctx context.Context) (int64, error) {
	if r == nil || r.modelRepo == nil || r.rdb == nil {
		return 0, nil
	}

	models, err := r.modelRepo.ListAvailable()
	if err != nil {
		return 0, fmt.Errorf("list available models: %w", err)
	}

	entries, err := buildProviderEntries(models)
	if err != nil {
		return 0, err
	}

	version := time.Now().UnixNano()
	snapshot := &configsync.ProviderSnapshot{
		Version:   version,
		UpdatedAt: time.Now().UTC(),
		Entries:   entries,
	}

	if err := configsync.SaveProviderSnapshot(ctx, r.rdb, snapshot); err != nil {
		return 0, fmt.Errorf("save provider snapshot: %w", err)
	}

	return version, nil
}

func buildProviderEntries(models []controlmodel.Model) ([]gatewaymodel.ProviderEntry, error) {
	providerMap := make(map[string]*gatewaymodel.ProviderEntry)
	providerOrder := make([]string, 0)

	for _, m := range models {
		providerID := m.ProviderID.String()
		entry, ok := providerMap[providerID]
		if !ok {
			credentials, err := crypto.Decrypt(m.Provider.EncryptedCredentials)
			if err != nil {
				return nil, fmt.Errorf("decrypt provider credentials for %s: %w", providerID, err)
			}

			// api_version 从 ProviderDefinition.VendorOptions 继承，per-instance 可覆盖（暂不开放）
			var apiVersion string
			if def := m.Provider.Definition; def != nil {
				apiVersion = def.VendorOptions.APIVersion
			}

			entry = &gatewaymodel.ProviderEntry{
				ID:          providerID,
				Name:        m.Provider.Name,
				Type:        providerTypeForGateway(m.Provider),
				Endpoint:    m.Provider.Endpoint,
				Credentials: extractAPIKey(credentials),
				APIVersion:  apiVersion,
				Enabled:     true,
				Models:      make([]string, 0, 4),
				ModelsV2:    make([]gatewaymodel.ModelEntry, 0, 4),
			}
			providerMap[providerID] = entry
			providerOrder = append(providerOrder, providerID)
		}

		// 保留完整模型名（含 "{provider}/" 前缀）。
		// 某些上游（如 ZHIPU）期望收到带前缀的完整模型名；
		// Bedrock 等需要剥前缀的 provider 在各自的 adapter 中处理。
		entry.Models = append(entry.Models, m.Name)
		modelEntry := gatewaymodel.ModelEntry{Name: m.Name, UpstreamModelID: m.UpstreamModelID}
		if pricing := snapshotModelPricing(m); len(pricing.FieldSources("database")) > 0 {
			modelEntry.Pricing = &pricing
		}
		entry.ModelsV2 = append(entry.ModelsV2, modelEntry)
	}

	entries := make([]gatewaymodel.ProviderEntry, 0, len(providerOrder))
	for _, providerID := range providerOrder {
		entry := providerMap[providerID]
		sort.Strings(entry.Models)
		sort.Slice(entry.ModelsV2, func(i, j int) bool { return entry.ModelsV2[i].Name < entry.ModelsV2[j].Name })
		entries = append(entries, *entry)
	}

	return entries, nil
}

func snapshotModelPricing(m controlmodel.Model) core.ModelPricing {
	var inputPricePerMillion, outputPricePerMillion *float64
	var cacheReadPrice, cacheWritePrice *float64
	var tierThreshold *int64
	var inputPriceAboveTier, outputPriceAboveTier *float64
	var rawPayload string
	if m.Catalog != nil {
		inputPricePerMillion = &m.Catalog.InputPricePerMillion
		outputPricePerMillion = &m.Catalog.OutputPricePerMillion
		cacheReadPrice = m.Catalog.CacheReadPrice
		cacheWritePrice = m.Catalog.CacheWritePrice
		tierThreshold = m.Catalog.TierThreshold
		inputPriceAboveTier = m.Catalog.InputPriceAboveTier
		outputPriceAboveTier = m.Catalog.OutputPriceAboveTier
		rawPayload = m.Catalog.RawPayload
	}
	pricing := controlpricing.ResolveModelPricing(
		m.PriceMode, m.InputPrice, m.OutputPrice,
		m.CacheReadPrice, m.CacheWritePrice,
		inputPricePerMillion, outputPricePerMillion,
		cacheReadPrice, cacheWritePrice,
		tierThreshold, inputPriceAboveTier, outputPriceAboveTier,
	)
	usagePricing := controlpricing.ResolveUsagePricing(controlpricing.UsagePricingInput{
		ModelType: m.ModelType, PriceMode: m.PriceMode,
		InputPrice: m.InputPrice, CatalogRawPayload: rawPayload,
	})
	if usagePricing.RerankPerSearchUnit != nil {
		pricing.RerankPerSearchUnit = usagePricing.RerankPerSearchUnit
	}
	return pricing
}

func providerTypeForGateway(p controlmodel.Provider) string {
	if p.Type != "" {
		return p.Type
	}
	return p.DefinitionID
}
