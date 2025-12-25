package service

import (
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/repository"
)

// PerformanceService 性能看板服务
type PerformanceService struct {
	callLogRepo *repository.CallLogRepo
	groupRepo   *repository.GroupRepo
}

// NewPerformanceService 创建 PerformanceService 实例
func NewPerformanceService(callLogRepo *repository.CallLogRepo, groupRepo *repository.GroupRepo) *PerformanceService {
	return &PerformanceService{callLogRepo: callLogRepo, groupRepo: groupRepo}
}

// PerformanceMember 模型组内成员性能数据
type PerformanceMember struct {
	ModelName   string  `json:"model_name"`
	Provider    string  `json:"provider"`
	TrafficPct  float64 `json:"traffic_pct"`
	ReqCount    int     `json:"req_count"`
	P50Ms       int     `json:"p50_ms"`
	P95Ms       int     `json:"p95_ms"`
	SuccessRate float64 `json:"success_rate"`
}

// PerformanceItem 性能看板单项数据
type PerformanceItem struct {
	Name          string              `json:"name"`
	Strategy      string              `json:"strategy,omitempty"`
	ModelCount    int                 `json:"model_count,omitempty"`
	UsedByGroups  int                 `json:"used_by_groups,omitempty"`
	ReqCount      int                 `json:"req_count"`
	SuccessRate   float64             `json:"success_rate"`
	P50Ms         int                 `json:"p50_ms"`
	P95Ms         int                 `json:"p95_ms"`
	TTFTP50Ms     int                 `json:"ttft_p50_ms"`
	TTFTP95Ms     int                 `json:"ttft_p95_ms"`
	FailoverCount int                 `json:"failover_count"`
	PctIn24hP50   *int                `json:"pct_in_24h_p50,omitempty"`
	Err429        int                 `json:"err_429"`
	Err5xx        int                 `json:"err_5xx"`
	Members       []PerformanceMember `json:"members,omitempty"`
}

// PerformanceResult 性能看板响应
type PerformanceResult struct {
	Window      string            `json:"window"`
	GeneratedAt time.Time         `json:"generated_at"`
	Items       []PerformanceItem `json:"items"`
	Total       int               `json:"total"`
}

// GetPerformance 获取性能看板数据
func (s *PerformanceService) GetPerformance(window, view, search string, limit, offset int) (*PerformanceResult, error) {
	rows, err := s.callLogRepo.GetPerformanceStats(window, view, search, offset, limit)
	if err != nil {
		return nil, err
	}

	total, err := s.callLogRepo.CountPerformanceItems(window, view, search)
	if err != nil {
		slog.Warn("count performance items failed", "error", err)
		total = len(rows)
	}

	items := make([]PerformanceItem, 0, len(rows))
	for _, row := range rows {
		item := PerformanceItem{
			Name:          row.Name,
			ReqCount:      row.ReqCount,
			SuccessRate:   row.SuccessRate,
			P50Ms:         row.P50Ms,
			P95Ms:         row.P95Ms,
			TTFTP50Ms:     row.TTFTP50Ms,
			TTFTP95Ms:     row.TTFTP95Ms,
			FailoverCount: row.FailoverCount,
			Err429:        row.Err429,
			Err5xx:        row.Err5xx,
		}
		items = append(items, item)
	}

	if view == "group" {
		s.enrichGroupView(items, window)
	} else {
		s.enrichModelView(items, rows, window)
	}

	return &PerformanceResult{
		Window:      window,
		GeneratedAt: time.Now(),
		Items:       items,
		Total:       total,
	}, nil
}

func (s *PerformanceService) enrichGroupView(items []PerformanceItem, window string) {
	if len(items) == 0 {
		return
	}

	// 获取组元数据（策略 + 成员数）
	names := make([]string, 0, len(items))
	for i := range items {
		names = append(names, items[i].Name)
	}

	metaMap, err := s.groupRepo.GetGroupMetadata(names)
	if err != nil {
		slog.Warn("get group metadata failed", "error", err)
	} else {
		for i := range items {
			if meta, ok := metaMap[items[i].Name]; ok {
				items[i].Strategy = meta.Strategy
				items[i].ModelCount = meta.ModelCount
			}
		}
	}

	// 24h 基线
	baselines, err := s.callLogRepo.Get24hHourlyP50()
	if err != nil {
		slog.Warn("get 24h baselines failed", "error", err)
	} else {
		for i := range items {
			if bl, ok := baselines[items[i].Name]; ok {
				items[i].PctIn24hP50 = computePercentilePosition(items[i].P50Ms, bl)
			}
		}
	}

	// 成员数据（window != "24h" 时加载）
	if window != "24h" {
		for i := range items {
			members, err := s.callLogRepo.GetGroupMembers(window, items[i].Name)
			if err != nil {
				slog.Warn("get group members failed", "group", items[i].Name, "error", err)
				continue
			}
			memberList := make([]PerformanceMember, 0, len(members))
			for _, m := range members {
				memberList = append(memberList, PerformanceMember{
					ModelName:   m.ModelName,
					Provider:    m.Provider,
					TrafficPct:  m.TrafficPct,
					ReqCount:    m.ReqCount,
					P50Ms:       m.P50Ms,
					P95Ms:       m.P95Ms,
					SuccessRate: m.SuccessRate,
				})
			}
			items[i].Members = memberList
		}
	}
}

func (s *PerformanceService) enrichModelView(items []PerformanceItem, rows []repository.PerformanceGroupRow, window string) {
	if len(items) == 0 {
		return
	}

	// UsedByGroups
	usedByMap, err := s.callLogRepo.GetModelUsedByGroups(window)
	if err != nil {
		slog.Warn("get model used by groups failed", "error", err)
	} else {
		for i := range items {
			if i < len(rows) {
				if cnt, ok := usedByMap[rows[i].Model]; ok {
					items[i].UsedByGroups = cnt
				}
			}
		}
	}

	// 24h 基线（model 视图）
	baselines, err := s.callLogRepo.Get24hHourlyP50Model()
	if err != nil {
		slog.Warn("get 24h model baselines failed", "error", err)
	} else {
		for i := range items {
			if bl, ok := baselines[items[i].Name]; ok {
				items[i].PctIn24hP50 = computePercentilePosition(items[i].P50Ms, bl)
			}
		}
	}
}

func computePercentilePosition(current int, baselines []int) *int {
	if len(baselines) < 3 {
		return nil
	}
	sorted := make([]int, len(baselines))
	copy(sorted, baselines)
	sort.Ints(sorted)
	below := 0
	for _, v := range sorted {
		if current > v {
			below++
		}
	}
	pct := int(math.Round(float64(below+1) / float64(len(sorted)+1) * 100))
	return &pct
}
