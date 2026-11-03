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
	"strconv"
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
	Message      string `json:"message,omitempty"` // 成功时的语义说明（如 video skip）
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

	// 优先用组上声明的 model_type，否则取组内首个启用模型类型
	modelType := strings.TrimSpace(g.ModelType)
	if modelType == "" {
		modelType = s.getGroupModelType(g)
	}

	// 媒体组（video/image）**不走** /v1/chat/completions：媒体模型不实现对话补全，
	// 打过去上游必然报错 → 看板/健康检查必然误报失败。跳过且不计失败。
	plan, skipReason := PlanGroupProbe(modelType)
	if plan == GroupProbeSkip {
		cr.Success = true
		cr.Message = skipReason
		s.recordLog(g, true, 0, skipReason)
		return cr
	}

	apiKey, err := s.getProbeKey(ctx)
	if err != nil {
		cr.Error = "获取探针 key 失败: " + err.Error()
		s.recordLog(g, false, 0, cr.Error)
		return cr
	}

	success, elapsed, errMsg := s.probeGroup(ctx, g.Name, apiKey, plan)
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

// GroupProbePlan 组级存活探针的走向（E2-补充）。
type GroupProbePlan string

const (
	// GroupProbeChat 对话组：POST /v1/chat/completions。
	GroupProbeChat GroupProbePlan = "chat"
	// GroupProbeEmbedding 向量组：POST /v1/embeddings。
	GroupProbeEmbedding GroupProbePlan = "embedding"
	// GroupProbeRerank 重排组：POST /v1/rerank。
	GroupProbeRerank GroupProbePlan = "rerank"
	// GroupProbeSkip 媒体组 / 非对话组：**不发**任何组级探针（不计失败）。
	GroupProbeSkip GroupProbePlan = "skip"
)

// 跳过原因（写入 health_check_logs.error_message 与响应 message，供运维定位）。
//
// ⚠️ video 那句必须与既有实现**逐字一致**：它是既有看板/API 消费方已经看到过的文案。
const (
	groupProbeSkipVideoMessage = "skipped: video group has no chat probe (use provider connectivity+auth; not video create)"
	groupProbeSkipImageMessage = "skipped: image group has no chat probe (use provider connectivity+auth; not image create)"
)

// PlanGroupProbe 决定组级探针走哪个端点。
//
// **白名单语义（fail-safe）**：只有明确属于 chat / embedding / rerank 族的 model_type
// 才发探针，其余一律 Skip。这样后续新增的任何非对话媒体类型（image 之后的下一类）
// 不会再踩「拿 chat 探针打媒体组」的坑。
//
// 背景（A5 打开 `model_type=image` 后新暴露的路径）：
// 原实现只跳过 video 组（`isVideoGroupModelType`），image 组会落到
// buildProbeRequest 的 default 分支去打 `/v1/chat/completions` —— 图像模型不实现
// 对话补全，上游必然报错 → 看板/健康检查**必然误报失败**。
//
// 「媒体组没有组级探针」不是缺陷而是设计：媒体链路会产生上游生成费用，组级探活
// 只允许做**连通性 + 鉴权**（见 test_handler.go / seedance_relay_probe.go 的范式），
// 而这需要按渠道实现（Kling/Vidu 适配器就绪后再接）。在那之前，跳过（不计失败）
// 是唯一不与事实相悖的行为。
//
// model_groups.model_type 由迁移的 CHECK 约束限定在 7 值枚举内
// （chat/image/embedding/embedding_vl/rerank/rerank_vl/video），
// 因此 default 分支实际只在防御未知脏值时命中。
//
// 返回的第二个值是 skip 时给运维看的原因（非 skip 时为空串）。
func PlanGroupProbe(modelType string) (GroupProbePlan, string) {
	switch strings.ToLower(strings.TrimSpace(modelType)) {
	case "chat", "completion", "responses":
		return GroupProbeChat, ""
	case "embedding", "embedding_vl":
		return GroupProbeEmbedding, ""
	case "rerank", "rerank_vl":
		return GroupProbeRerank, ""
	case "video", "video_generation":
		return GroupProbeSkip, groupProbeSkipVideoMessage
	case "image", "image_generation", "image_edit":
		return GroupProbeSkip, groupProbeSkipImageMessage
	default:
		return GroupProbeSkip, "skipped: group model_type " + strconv.Quote(strings.TrimSpace(modelType)) +
			" has no chat probe (unknown non-chat group type; use provider connectivity+auth probe)"
	}
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
// 端点与请求体由 BuildGroupProbeRequest 按探针走向（chat/embedding/rerank）构建。
// 超时 60s：全量并发后消除了排队延迟，60s 足够覆盖上游高峰时段的推理耗时。
func (s *GroupCheckService) probeGroupOnce(ctx context.Context, groupName, apiKey string, plan GroupProbePlan) (bool, int, string) {
	endpoint, payload := BuildGroupProbeRequest(groupName, plan)
	if endpoint == "" {
		// 防御：skip 走向（或未知走向）**没有**可发的探针。绝不能退化成 chat 请求。
		return false, 0, "internal: no probe request for plan " + string(plan)
	}
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

// BuildGroupProbeRequest 按探针走向构建探针请求的端点和请求体。
//
// 这是**唯一**把「组类型 → 上游端点」映射出来的地方：Chat / Embedding / Rerank 三族
// 各有自己的端点；Skip（媒体组/未知类型）返回空端点 —— 调用方不得发请求。
// 返回空端点时调用方必须直接跳过，**不允许**把空端点兜底成 chat（那正是本函数要
// 消灭的缺陷：拿 chat 探针打图像/视频组）。
func BuildGroupProbeRequest(modelName string, plan GroupProbePlan) (string, map[string]interface{}) {
	switch plan {
	case GroupProbeEmbedding:
		return "/v1/embeddings", map[string]interface{}{
			"model": modelName,
			"input": "健康检查测试文本",
		}
	case GroupProbeRerank:
		return "/v1/rerank", map[string]interface{}{
			"model":     modelName,
			"query":     "健康检查查询",
			"documents": []string{"文档1", "文档2"},
		}
	case GroupProbeChat:
		return "/v1/chat/completions", map[string]interface{}{
			"model":      modelName,
			"messages":   []map[string]string{{"role": "user", "content": "健康检查"}},
			"max_tokens": 10,
		}
	default:
		// GroupProbeSkip 与任何未知走向：没有可发的探针请求。
		return "", nil
	}
}

// probeGroup 向 Gateway 发送探针请求，model 字段填组名。
// 按探针走向（chat/embedding/rerank）选择正确的端点和请求格式。
// 失败后自动重试最多 probeMaxRetries 次（含首次共 probeMaxRetries+1 次机会），
// 全部失败才返回错误，避免偶发超时或抖动被误判为健康异常。
func (s *GroupCheckService) probeGroup(ctx context.Context, groupName, apiKey string, plan GroupProbePlan) (bool, int, string) {
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

		ok, elapsed, errMsg := s.probeGroupOnce(ctx, groupName, apiKey, plan)
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
