package orchestrator_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScheduler 模拟 Scheduler 接口，用于测试 group_dispatcher 鉴权逻辑
type fakeScheduler struct {
	decision *scheduler.RouteDecision
	err      error
}

func (f *fakeScheduler) Route(_ string) (*scheduler.RouteDecision, error) {
	return f.decision, f.err
}

func (f *fakeScheduler) Failover(_, _ string) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeScheduler) FailoverExcluding(_ string, _ []string) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig {
	return nil
}

func (f *fakeScheduler) NotifyFailure(_ string) {}

func (f *fakeScheduler) SetCooldown(_ string, _ time.Duration) {}

// newBindingServer 创建模拟控制面 binding API 的 httptest.Server。
// keyGroups 是 map[keyID] -> []BoundGroup，模拟不同 key 的绑定关系。
func newBindingServer(t *testing.T, keyGroups map[string][]binding.BoundGroup) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/keys/test-key/groups" {
			groups, ok := keyGroups["test-key"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(groups)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// setupDispatcher 创建带配置的 GroupAwareDispatcher。
// groupNames 是已知的模型组名列表（填充 GroupNameCache）。
// bindURL 为空则不注入 binding.Client（不启用 binding 校验）。
func setupDispatcher(groupNames []string, bindURL string) *orchestrator.GroupAwareDispatcher {
	sch := &fakeScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "group-a",
			ModelName: "model-1",
			ModelID:   "1",
		},
	}
	nc := cache.NewGroupNameCache("")
	nc.SetNames(groupNames)

	if bindURL != "" {
		bc := binding.NewClient(bindURL, "test-token")
		return orchestrator.NewGroupAwareDispatcherWithBinding(sch, nc, bc)
	}
	return orchestrator.NewGroupAwareDispatcher(sch, nc)
}

// makeContext 创建携带 identity 的 gin test context。
func makeContext(identity *model.UserIdentity) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/v1/chat/completions", nil)
	if identity != nil {
		c.Set("identity", identity)
	}
	return c, w
}

func TestDispatch_UsableGroupsCeiling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testGroups := []string{"g-public", "g-controlled", "g-other"}

	tests := []struct {
		name        string
		description string
		identity    *model.UserIdentity
		modelName   string
		bindGroups  map[string][]binding.BoundGroup // nil = 无 binding server
		wantErr     error                           // 期望返回的错误（nil 表示放行）
		wantNil     bool                            // 期望 decision 为 nil（非组模型时）
	}{
		{
			name:        "非组名跳过鉴权",
			description: "请求的 modelName 不在 GroupNameCache 中，应返回 nil, nil（非组模型，交给下游处理）",
			identity:    &model.UserIdentity{KeyID: "k1", UsableGroups: []string{"g-public"}},
			modelName:   "unknown-model",
			wantErr:     nil,
			wantNil:     true,
		},
		{
			name:        "usable_groups包含+access_all=true→放行",
			description: "目标组在 usable_groups 中，且 access_all_groups=true，应正常调度放行",
			identity:    &model.UserIdentity{KeyID: "k1", AccessAllGroups: true, UsableGroups: []string{"g-public", "g-controlled"}},
			modelName:   "g-public",
			wantErr:     nil,
		},
		{
			name:        "usable_groups不包含+access_all=true→403",
			description: "目标组不在 usable_groups 中（天花板拦截），即使 access_all_groups=true 也应返回 403",
			identity:    &model.UserIdentity{KeyID: "k1", AccessAllGroups: true, UsableGroups: []string{"g-public"}},
			modelName:   "g-controlled",
			wantErr:     model.ErrModelGroupUnauthorized,
		},
		{
			name:        "usable_groups为空+access_all=true→旧逻辑放行",
			description: "控制面未下发 usable_groups（旧版），access_all_groups=true 时直接放行走调度",
			identity:    &model.UserIdentity{KeyID: "k1", AccessAllGroups: true, UsableGroups: nil},
			modelName:   "g-public",
			wantErr:     nil,
		},
		{
			name:        "usable_groups包含+access_all=false+binding包含→放行",
			description: "双层鉴权均通过：天花板包含且 binding 也包含目标组",
			identity:    &model.UserIdentity{KeyID: "test-key", AccessAllGroups: false, UsableGroups: []string{"g-public", "g-controlled"}},
			modelName:   "g-public",
			bindGroups: map[string][]binding.BoundGroup{
				"test-key": {{ID: 1, Name: "g-public", Enabled: true}},
			},
			wantErr: nil,
		},
		{
			name:        "usable_groups包含+access_all=false+binding不包含→403",
			description: "天花板通过但 binding 不包含目标组，应返回 403 group_not_authorized",
			identity:    &model.UserIdentity{KeyID: "test-key", AccessAllGroups: false, UsableGroups: []string{"g-public", "g-controlled"}},
			modelName:   "g-controlled",
			bindGroups: map[string][]binding.BoundGroup{
				"test-key": {{ID: 1, Name: "g-public", Enabled: true}},
			},
			wantErr: model.ErrModelGroupUnauthorized,
		},
		{
			name:        "usable_groups不包含+access_all=false→天花板拦截",
			description: "天花板不包含目标组，即使后续 binding 可能包含也直接被拦截，应返回 403",
			identity:    &model.UserIdentity{KeyID: "test-key", AccessAllGroups: false, UsableGroups: []string{"g-other"}},
			modelName:   "g-public",
			bindGroups: map[string][]binding.BoundGroup{
				"test-key": {{ID: 1, Name: "g-public", Enabled: true}},
			},
			wantErr: model.ErrModelGroupUnauthorized,
		},
		{
			name:        "usable_groups为空+access_all=false+binding不包含→旧逻辑拒绝",
			description: "旧版控制面（usable_groups 为空），走旧 binding 逻辑，binding 不包含时返回 ErrModelGroupUnavailable",
			identity:    &model.UserIdentity{KeyID: "test-key", AccessAllGroups: false, UsableGroups: nil},
			modelName:   "g-public",
			bindGroups: map[string][]binding.BoundGroup{
				"test-key": {{ID: 2, Name: "g-other", Enabled: true}},
			},
			wantErr: model.ErrModelGroupUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bindURL string
			if tt.bindGroups != nil {
				ts := newBindingServer(t, tt.bindGroups)
				defer ts.Close()
				bindURL = ts.URL
			}

			d := setupDispatcher(testGroups, bindURL)
			c, _ := makeContext(tt.identity)
			decision, err := d.Dispatch(c, tt.modelName)

			if tt.wantErr != nil {
				require.Error(t, err, "期望返回错误: %v", tt.wantErr)
				assert.Equal(t, tt.wantErr, err)
				assert.Nil(t, decision)
			} else if tt.wantNil {
				assert.Nil(t, err)
				assert.Nil(t, decision)
			} else {
				require.NoError(t, err)
				require.NotNil(t, decision)
			}
		})
	}
}
