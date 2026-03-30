package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// GroupCheckResult 单个模型组检测结果
type GroupCheckResult struct {
	GroupID      uint   `json:"group_id"`
	GroupName    string `json:"group_name"`
	Success      bool   `json:"success"`
	ResponseTime int    `json:"response_time"`
	Error        string `json:"error,omitempty"`
}

// GroupCheckAllResult 批量组检测汇总
type GroupCheckAllResult struct {
	Total   int                `json:"total"`
	Success int                `json:"success"`
	Failed  int                `json:"failed"`
	Results []GroupCheckResult `json:"results"`
}

// GroupCheckService 模型组健康检测服务
type GroupCheckService struct {
	groupRepo    *repository.GroupRepo
	modelRepo    *repository.ModelRepo
	healthRepo   *repository.HealthCheckRepo
	systemKeySvc *SystemKeyService
	mu           sync.Mutex

	gatewayURL   string // e.g. http://localhost:8080
	probeKey     string // 缓存的探针 key
}

// NewGroupCheckService 创建 GroupCheckService
// gatewayURL 为空时自动从环境变量 GATEWAY_URL / GATEWAY_PORT 读取，最终兜底 http://localhost:8080
func NewGroupCheckService(
	groupRepo *repository.GroupRepo,
	modelRepo *repository.ModelRepo,
	healthRepo *repository.HealthCheckRepo,
	systemKeySvc *SystemKeyService,
	gatewayURL string,
) *GroupCheckService {
	if gatewayURL == "" {
		gatewayURL = os.Getenv("GATEWAY_URL")
	}
	if gatewayURL == "" {
		port := os.Getenv("GATEWAY_PORT")
		if port == "" {
			port = "8080"
		}
		gatewayURL = "http://localhost:" + port
	}
	return &GroupCheckService{
		groupRepo:    groupRepo,
		modelRepo:    modelRepo,
		healthRepo:   healthRepo,
		systemKeySvc: systemKeySvc,
		gatewayURL:   strings.TrimRight(gatewayURL, "/"),
	}
}

// getProbeKey 懒加载探针 key（线程安全）
func (s *GroupCheckService) getProbeKey(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probeKey != "" {
		return s.probeKey, nil
	}
	key, err := s.systemKeySvc.EnsureProbeKey(ctx)
	if err != nil {
		return "", err
	}
	s.probeKey = key
	return key, nil
}

// CheckOne 检测单个模型组（通过 Gateway 发真实请求）
func (s *GroupCheckService) CheckOne(ctx context.Context, g *model.Group) *GroupCheckResult {
	cr := &GroupCheckResult{GroupID: g.ID, GroupName: g.Name}

	// 获取组内第一个模型的类型
	modelType := s.getGroupModelType(g)

	apiKey, err := s.getProbeKey(ctx)
	if err != nil {
		cr.Error = "获取探针 key 失败: " + err.Error()
		s.recordLog(g, false, 0, cr.Error)
		return cr
	}

	success, elapsed, errMsg := s.probeGroup(ctx, g.Name, apiKey, modelType)
	cr.ResponseTime = elapsed

	if success {
		cr.Success = true
		s.recordLog(g, true, elapsed, "")
	} else {
		cr.Error = errMsg
		s.recordLog(g, false, elapsed, errMsg)
	}

	return cr
}

// CheckAll 并发检测所有启用的模型组
func (s *GroupCheckService) CheckAll(ctx context.Context) *GroupCheckAllResult {
	groups, err := s.groupRepo.ListEnabled()
	if err != nil {
		slog.Error("group check: failed to list groups", "error", err)
		return &GroupCheckAllResult{}
	}

	result := &GroupCheckAllResult{
		Results: make([]GroupCheckResult, 0, len(groups)),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	// 所有模型组全量并发探测，消除 semaphore 排队导致的时序差异。
	// 探针请求本身极轻量（max_tokens=5），不会对 Gateway 或上游产生显著压力。
	for i := range groups {
		g := &groups[i]
		result.Total++
		wg.Add(1)
		go func(grp *model.Group) {
			defer wg.Done()
			cr := s.CheckOne(ctx, grp)
			mu.Lock()
			result.Results = append(result.Results, *cr)
			if cr.Success {
				result.Success++
			} else {
				result.Failed++
			}
			mu.Unlock()
		}(g)
	}

	wg.Wait()

	// 检测完成后使时间线缓存失效
	s.invalidateGroupTimelineCache()

	slog.Info("group health check completed",
		"total", result.Total, "success", result.Success, "failed", result.Failed)
	return result
}

// CheckOneByID 通过 ID 查找组并检测
func (s *GroupCheckService) CheckOneByID(ctx context.Context, id uint) (*GroupCheckResult, error) {
	g, err := s.groupRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("group not found")
	}
	return s.CheckOne(ctx, g), nil
}

// probeGroupOnce 向 Gateway 发送一次非流式探针请求，验证端到端推理链路可用性。
// 根据 modelType 构建不同格式的请求：chat/embedding/rerank。
// 超时 60s：全量并发后消除了排队延迟，60s 足够覆盖上游高峰时段的推理耗时。
func (s *GroupCheckService) probeGroupOnce(ctx context.Context, groupName, apiKey, modelType string) (bool, int, string) {
	endpoint, payload := s.buildProbeRequest(groupName, modelType)
	body, _ := json.Marshal(payload)

	probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(probeCtx, http.MethodPost, s.gatewayURL+endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		return false, elapsed, fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, elapsed, fmt.Sprintf("HTTP %d: %s",
			resp.StatusCode, truncateStr(string(respBody), 200))
	}

	return true, elapsed, ""
}

// getGroupModelType 获取组内第一个启用模型的类型，默认为 chat
func (s *GroupCheckService) getGroupModelType(g *model.Group) string {
	groupModels, err := s.groupRepo.ListGroupModels(g.ID)
	if err != nil || len(groupModels) == 0 {
		return "chat"
	}

	// 获取模型 ID 列表
	modelIDs := make([]uuid.UUID, 0, len(groupModels))
	for _, gm := range groupModels {
		modelIDs = append(modelIDs, gm.ModelID)
	}

	// 批量获取模型详情
	models, err := s.modelRepo.GetByIDs(modelIDs)
	if err != nil {
		return "chat"
	}

	for _, m := range models {
		if m != nil && m.Enabled != nil && *m.Enabled {
			return m.ModelType
		}
	}
	return "chat"
}

// buildProbeRequest 根据模型类型构建探针请求的端点和请求体
func (s *GroupCheckService) buildProbeRequest(modelName, modelType string) (string, map[string]interface{}) {
	switch modelType {
	case "embedding", "embedding_vl":
		return "/v1/embeddings", map[string]interface{}{
			"model": modelName,
			"input": "健康检查测试文本",
		}
	case "rerank", "rerank_vl":
		return "/v1/rerank", map[string]interface{}{
			"model":     modelName,
			"query":     "健康检查查询",
			"documents": []string{"文档1", "文档2"},
		}
	default: // chat 或其他类型
		return "/v1/chat/completions", map[string]interface{}{
			"model":      modelName,
			"messages":   []map[string]string{{"role": "user", "content": "健康检查"}},
			"max_tokens": 10,
		}
	}
}

// probeGroup 向 Gateway 发送探针请求，model 字段填组名。
// 根据 modelType 选择正确的端点和请求格式（chat/embedding/rerank）。
// 失败后自动重试最多 probeMaxRetries 次（含首次共 probeMaxRetries+1 次机会），
// 全部失败才返回错误，避免偶发超时或抖动被误判为健康异常。
func (s *GroupCheckService) probeGroup(ctx context.Context, groupName, apiKey, modelType string) (bool, int, string) {
	const probeMaxRetries = 1 // 失败后最多重试次数（流式 TTFB 很快，1 次重试即可）

	var (
		lastElapsed int
		lastErr     string
	)

	for attempt := 0; attempt <= probeMaxRetries; attempt++ {
		// 若外层 context 已取消，不再继续重试
		if ctx.Err() != nil {
			return false, lastElapsed, fmt.Sprintf("context canceled before attempt %d", attempt+1)
		}

		ok, elapsed, errMsg := s.probeGroupOnce(ctx, groupName, apiKey, modelType)
		lastElapsed = elapsed
		lastErr = errMsg

		if ok {
			if attempt > 0 {
				slog.Info("group probe succeeded after retry",
					"group", groupName, "attempt", attempt+1, "elapsed_ms", elapsed)
			}
			return true, elapsed, ""
		}

		if attempt < probeMaxRetries {
			slog.Warn("group probe failed, will retry",
				"group", groupName, "attempt", attempt+1, "error", errMsg)
		}
	}

	return false, lastElapsed, lastErr
}

// recordLog 将组检测结果写入 health_check_logs（target_type='group'）
func (s *GroupCheckService) recordLog(g *model.Group, success bool, responseTimeMs int, errMsg string) {
	// 用确定性 UUID（基于组 ID）作为 target_id
	targetID := groupIDToUUID(g.ID)
	log := &model.HealthCheckLog{
		TargetType:     "group",
		TargetID:       targetID,
		TargetName:     g.Name,
		Success:        success,
		ResponseTimeMs: responseTimeMs,
		ErrorMessage:   errMsg,
	}
	if err := s.healthRepo.CreateLog(log); err != nil {
		slog.Error("group check: failed to record log", "group", g.Name, "error", err)
	}
	if !success {
		slog.Warn("group health check failed", "group", g.Name, "error", errMsg)
	}
}

// invalidateGroupTimelineCache 清除组时间线 Redis 缓存
func (s *GroupCheckService) invalidateGroupTimelineCache() {
	// 缓存 TTL 为 30s，自然过期；如需立即失效可注入 redis client 扩展
}

// groupIDToUUID 将 uint 类型的组 ID 确定性地转换为 UUID
// 格式：00000000-0000-4000-8000-{id padded}
func groupIDToUUID(id uint) uuid.UUID {
	var b [16]byte
	// version 4 bits + variant bits 简单填充
	b[6] = 0x40
	b[8] = 0x80
	b[15] = byte(id)
	b[14] = byte(id >> 8)
	b[13] = byte(id >> 16)
	b[12] = byte(id >> 24)
	return uuid.UUID(b)
}
