package service_test

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

// 本文件是 E2-补充（Batch 3）的回归守卫：**媒体模型组绝不能被打上 chat 探针**。
//
// 缺陷背景（已核实）：`group_check_service.go` 原实现只跳过 video 组
// （`isVideoGroupModelType` 只认 "video"/"video_generation"），于是 A5 打开
// `model_type=image` 之后，image 组会落到 `buildProbeRequest` 的 default 分支去发
// `POST /v1/chat/completions`。图像模型不实现对话补全 → 上游必然报错
// → **看板/健康检查把健康的 image 组报成故障**，而且是持续误报。
//
// 原 `internal/controlplane/service/group_check_video_test.go`（in-package，Q3 存量）
// 只测了 `isVideoGroupModelType` 这 6 个输入，覆盖不到 image，因此没能拦住这个缺陷。
// 本文件取而代之，并直接对**导出后的**接缝 `PlanGroupProbe` / `BuildGroupProbeRequest`
// 做表驱动覆盖（E-obs 导出这两个函数正是为了让测试能落在 tests/ 下，符合 Q3）。

// mediaTypeEnum 是 `model_groups.model_type` 的 CHECK 约束真值集（7 个），
// 逐字来自 atlas-migrations/20261016101000_add_model_type_image.sql（该迁移扩展了
// 20260710180000 的 6 值白名单）。**这是唯一权威来源**：新增媒体类型时必须同步
// 扩展这里，否则下面的「枚举全覆盖」断言会立刻失败，提醒补探针走向。
var mediaTypeEnum = []string{
	"chat", "embedding", "embedding_vl", "rerank", "rerank_vl", "video", "image",
}

// TestPlanGroupProbe_ImageGroupSkipsChatProbe 是本文件最核心的一条：**缺陷复现**。
// 若这条失败（image 组拿到 chat 走向），生产表现是 image 组在看板上恒为故障。
func TestPlanGroupProbe_ImageGroupSkipsChatProbe(t *testing.T) {
	for _, modelType := range []string{"image", "image_generation", "image_edit"} {
		plan, reason := service.PlanGroupProbe(modelType)

		require.Equal(t, service.GroupProbeSkip, plan,
			"model_type=%q 是媒体组，绝不能走 chat 探针（否则看板恒误报故障）", modelType)
		require.NotEqual(t, service.GroupProbeChat, plan)
		require.NotEmpty(t, reason, "跳过必须给出可读原因，供运维在 health_check_logs 里定位")
		require.Contains(t, reason, "image")
	}
}

// TestPlanGroupProbe_VideoMessageStaysByteIdentical 固化**向后兼容**：
// video 组的跳过文案是既有看板/API 消费方已经看到过的字符串，必须逐字不变。
// （E-obs 的注释已声明这一约束，这里把它钉死，避免将来「顺手美化文案」破坏消费方。）
func TestPlanGroupProbe_VideoMessageStaysByteIdentical(t *testing.T) {
	const legacyVideoMessage = "skipped: video group has no chat probe (use provider connectivity+auth; not video create)"

	plan, reason := service.PlanGroupProbe("video")
	require.Equal(t, service.GroupProbeSkip, plan)
	require.Equal(t, legacyVideoMessage, reason,
		"video 组的跳过文案是既有消费方已见的字符串，改动会造成无谓的看板文案漂移")

	_, legacyReason := service.PlanGroupProbe("video_generation")
	require.Equal(t, legacyVideoMessage, legacyReason, "legacy 别名必须与 video 同文案")
}

// TestPlanGroupProbe_RealProbeTypesArePreserved 确认修复**没有误伤**三类真实探针：
// chat / embedding / rerank 及其 _vl 变体必须仍然发出对应探针。
func TestPlanGroupProbe_RealProbeTypesArePreserved(t *testing.T) {
	cases := map[string]service.GroupProbePlan{
		"chat":         service.GroupProbeChat,
		"embedding":    service.GroupProbeEmbedding,
		"embedding_vl": service.GroupProbeEmbedding,
		"rerank":       service.GroupProbeRerank,
		"rerank_vl":    service.GroupProbeRerank,
	}

	for modelType, want := range cases {
		plan, reason := service.PlanGroupProbe(modelType)
		require.Equalf(t, want, plan, "model_type=%q 的探针走向被误改", modelType)
		require.Emptyf(t, reason, "model_type=%q 走真实探针时不应有跳过原因", modelType)
	}
}

// TestPlanGroupProbe_EnumCoverage 用 CHECK 约束真值集做全覆盖，
// 防止将来新增模型组类型时**静默**落到 default 分支。
// 每个枚举值都必须被显式决策过（要么真实探针、要么带原因的 Skip）。
func TestPlanGroupProbe_EnumCoverage(t *testing.T) {
	allowed := map[service.GroupProbePlan]bool{
		service.GroupProbeChat:      true,
		service.GroupProbeEmbedding: true,
		service.GroupProbeRerank:    true,
		service.GroupProbeSkip:      true,
	}

	for _, modelType := range mediaTypeEnum {
		require.NotPanics(t, func() {
			plan, reason := service.PlanGroupProbe(modelType)
			require.Truef(t, allowed[plan], "model_type=%q 返回了未知走向 %q", modelType, plan)
			if plan == service.GroupProbeSkip {
				require.NotEmptyf(t, reason, "model_type=%q 跳过时必须给原因", modelType)
			}
			// 媒体类型必须是 Skip —— 这是本文件存在的理由，单独再断言一次。
			if modelType == "image" || modelType == "video" {
				require.Equalf(t, service.GroupProbeSkip, plan, "媒体组 %q 必须跳过", modelType)
			}
		})
	}
}

// TestPlanGroupProbe_UnknownTypesFailSafe 固化 **fail-safe 白名单语义**：
// 认不出的类型一律 Skip，绝不能兜底成 chat。
// 这一点决定了「将来新增非对话媒体类型」不会重演本次缺陷——新类型默认不发 chat 探针，
// 而不是默认发。这是刻意选择的方向：漏跳的代价是持续误报，漏发的代价只是少了探针。
func TestPlanGroupProbe_UnknownTypesFailSafe(t *testing.T) {
	for _, modelType := range []string{"", "   ", "audio", "tts", "image_v2", "chatx", "vision", "unknown-media"} {
		plan, reason := service.PlanGroupProbe(modelType)

		require.Equalf(t, service.GroupProbeSkip, plan,
			"未知类型 %q 必须 fail-safe 跳过，绝不能兜底成 chat 探针", modelType)
		require.NotEmptyf(t, reason, "未知类型 %q 也必须给出原因（含实际取值，便于排查脏数据）", modelType)
	}
}

// TestPlanGroupProbe_NormalizesCaseAndSpace 固化大小写/空白归一：
// 数据库里出现 " VIDEO " 这类脏值时不能退化成「认不出 → 发 chat 探针」。
func TestPlanGroupProbe_NormalizesCaseAndSpace(t *testing.T) {
	plan, _ := service.PlanGroupProbe("  VIDEO  ")
	require.Equal(t, service.GroupProbeSkip, plan, "大写/带空白的 video 仍须识别为媒体组")

	plan, _ = service.PlanGroupProbe("Chat")
	require.Equal(t, service.GroupProbeChat, plan, "大小写不应影响 chat 识别")
}

// TestBuildGroupProbeRequest_MediaNeverFallsBackToChat 是本文件的**负向对照**：
// 对 Skip 走向，构建器必须返回空端点，而**不是**兜底一个 chat 请求。
//
// 这正是原缺陷的实现位置：`buildProbeRequest` 的 `default:` 分支返回 chat 请求，
// 于是任何未被前面 case 命中的类型都会被打上 chat 探针。若哪天有人把这里的
// `default: return "", nil` 改回 chat，这条测试会立刻失败。
func TestBuildGroupProbeRequest_MediaNeverFallsBackToChat(t *testing.T) {
	for _, plan := range []service.GroupProbePlan{
		service.GroupProbeSkip,
		"",           // 零值
		"not-a-plan", // 脏值
	} {
		endpoint, payload := service.BuildGroupProbeRequest("some-media-group", plan)

		require.Emptyf(t, endpoint, "plan=%q 不得构建出任何端点", plan)
		require.NotEqualf(t, "/v1/chat/completions", endpoint,
			"plan=%q 绝不能兜底成 chat 探针——这就是那个把 image 组报成故障的缺陷", plan)
		require.Nilf(t, payload, "plan=%q 不应构建出请求体", plan)
	}
}

// TestBuildGroupProbeRequest_RealEndpoints 确认三类真实探针的端点与请求体形状。
func TestBuildGroupProbeRequest_RealEndpoints(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		endpoint, payload := service.BuildGroupProbeRequest("gpt-4o-group", service.GroupProbeChat)
		require.Equal(t, "/v1/chat/completions", endpoint)
		require.Equal(t, "gpt-4o-group", payload["model"])
		require.NotNil(t, payload["messages"])
		require.NotNil(t, payload["max_tokens"])
	})

	t.Run("embedding", func(t *testing.T) {
		endpoint, payload := service.BuildGroupProbeRequest("bge-group", service.GroupProbeEmbedding)
		require.Equal(t, "/v1/embeddings", endpoint)
		require.Equal(t, "bge-group", payload["model"])
		require.NotNil(t, payload["input"])
	})

	t.Run("rerank", func(t *testing.T) {
		endpoint, payload := service.BuildGroupProbeRequest("rerank-group", service.GroupProbeRerank)
		require.Equal(t, "/v1/rerank", endpoint)
		require.Equal(t, "rerank-group", payload["model"])
		require.NotNil(t, payload["query"])
		require.NotNil(t, payload["documents"])
	})
}

// TestBuildGroupProbeRequest_GroupNameIsUsedVerbatim 固化探针的 model 字段填**组名**：
// 组级探针验证的是「对外模型名 → 调度 → 上游」这条端到端链路，
// 所以必须用组名而不是上游模型名；且不得对名字做任何改写。
func TestBuildGroupProbeRequest_GroupNameIsUsedVerbatim(t *testing.T) {
	const groupName = "my-group/with-weird.name"

	for _, plan := range []service.GroupProbePlan{
		service.GroupProbeChat, service.GroupProbeEmbedding, service.GroupProbeRerank,
	} {
		_, payload := service.BuildGroupProbeRequest(groupName, plan)
		require.Equalf(t, groupName, payload["model"], "plan=%q 必须原样使用组名", plan)
	}
}

// TestPlanGroupProbe_SkipReasonsAreHumanReadable 固化跳过原因的可运维性：
// 原因要进 health_check_logs.error_message 与 API 响应，必须是能看懂的说明，
// 而不是空串或错误码（空串会让看板上出现一行没有解释的「跳过」）。
func TestPlanGroupProbe_SkipReasonsAreHumanReadable(t *testing.T) {
	for _, modelType := range []string{"video", "image", "unknown-media", ""} {
		_, reason := service.PlanGroupProbe(modelType)
		require.NotEmptyf(t, reason, "model_type=%q 的跳过原因不得为空", modelType)
		require.Truef(t, strings.HasPrefix(reason, "skipped:"),
			"跳过原因应以 skipped: 开头（既有文案惯例），实际为 %q", reason)
		require.Greaterf(t, len(reason), 20,
			"跳过原因 %q 太短，运维无法据此判断该做什么", reason)
	}
}
