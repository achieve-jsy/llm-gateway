package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// testScriptToken 是 opencode 配置脚本相关测试共用的 token。
const testScriptToken = "tok-test"

// tokenFixture 对应 handler 在 Redis 里存的 token 记录（GetScript 靠它渲染脚本）。
type tokenFixture struct {
	ProviderID   string
	ProviderName string
	BaseURL      string
	APIKey       string
	Status       string
}

// fakeSettings 顶掉 SettingsServiceProvider —— 脚本只需要一个供应商名称。
type fakeSettings struct {
	Name string
}

func (f fakeSettings) GetSupplierName(context.Context) string { return f.Name }

// newScriptEngine 起 miniredis、写入 token 记录，返回挂好 /script 路由的 gin 引擎。
// 返回的 miniredis 句柄供需要直接改 Redis（例如删掉 token）的用例使用。
func newScriptEngine(t *testing.T, f tokenFixture) (*gin.Engine, *miniredis.Miniredis) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})

	if f.ProviderID == "" {
		f.ProviderID = "biangfeng-gateway"
	}
	if f.Status == "" {
		f.Status = "pending"
	}
	rec := map[string]string{
		"key_id":        "key-1",
		"provider_id":   f.ProviderID,
		"provider_name": f.ProviderName,
		"base_url":      f.BaseURL,
		"api_key":       f.APIKey,
		"status":        f.Status,
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, mr.Set("opencode_cfg_token:"+testScriptToken, string(raw)))

	h := handler.NewOpencodeConfigHandler(rdb, fakeSettings{Name: "测试供应商"})

	r := gin.New()
	r.GET("/api/v1/opencode-config/script", h.GetScript)
	return r, mr
}

// fetchScript 取一次安装脚本；ver 为空时不带该查询参数（模拟前端默认行为）。
func fetchScript(t *testing.T, r *gin.Engine, osType, ver string) (int, string) {
	t.Helper()
	q := url.Values{}
	q.Set("token", testScriptToken)
	q.Set("os", osType)
	if ver != "" {
		q.Set("ver", ver)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/opencode-config/script?"+q.Encode(), nil)
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// ── 假网关：顶掉模型清单 / 能力 / 插件 / 回调四个端点 ─────────────────────────

// fakeGateway 模拟网关对外暴露的模型相关端点，端口随机、不出网。
type fakeGateway struct {
	Server *httptest.Server

	// Models / Info / InfoStatus 只在第一次发请求前设置，之后视为只读（handler 无锁读）。
	//
	// Models 是 /v1/models 返回的模型 id 列表。
	Models []string
	// Info 是 model id → LiteLLM model_info 的内容；缺失的模型不返回能力。
	Info map[string]map[string]any
	// InfoStatus 非 0 时 /v1/model/info 用该状态码失败，用来测降级。
	InfoStatus int
	// PluginBody 是 /api/v1/opencode-config/plugin 返回的内容；为空时返回假插件。
	// 用真实 opencode 二进制跑 E2E 时必须给真 bundle，否则 V1 加载插件会失败。
	PluginBody []byte

	mu        sync.Mutex
	callbacks []map[string]any
}

// newFakeGateway 起一个带默认夹具的假网关：4 个模型（1 个 embedding、1 个纯文本、
// 1 个带视觉+工具+reasoning 的、1 个只有 image 模态的）。
func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	g := &fakeGateway{
		Models: []string{"middle-level", "vision-pro", "embed-small", "plain-chat"},
		Info: map[string]map[string]any{
			"middle-level": {
				"key": "middle-level", "mode": "chat",
				"max_input_tokens": 1000000, "max_output_tokens": 65536,
				"supports_vision": true, "supports_function_calling": true, "supports_tool_choice": true,
				"supports_reasoning": true, "supported_openai_params": []string{"reasoning_effort", "tools"},
			},
			"vision-pro": {
				"key": "vision-pro", "mode": "chat",
				"max_input_tokens": 200000, "max_output_tokens": 32000,
				"supports_vision": true, "input_modalities": []string{"image"},
			},
			"embed-small": {"key": "embed-small", "mode": "embedding"},
			"plain-chat":  {"key": "plain-chat", "mode": "chat", "supports_vision": false},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		data := make([]map[string]any, 0, len(g.Models))
		for _, id := range g.Models {
			data = append(data, map[string]any{"id": id})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})
	mux.HandleFunc("/v1/model/info", func(w http.ResponseWriter, _ *http.Request) {
		if g.InfoStatus != 0 {
			writeJSON(w, g.InfoStatus, map[string]any{"error": "boom"})
			return
		}
		data := make([]map[string]any, 0, len(g.Info))
		for _, id := range g.Models {
			info, ok := g.Info[id]
			if !ok {
				continue
			}
			data = append(data, map[string]any{"model_name": id, "model_info": info})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})
	mux.HandleFunc("/api/v1/opencode-config/plugin", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		if len(g.PluginBody) > 0 {
			_, _ = w.Write(g.PluginBody)
			return
		}
		_, _ = w.Write([]byte("// fake plugin\n"))
	})
	mux.HandleFunc("/api/v1/opencode-config/callback", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		g.callbacks = append(g.callbacks, body)
		g.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"code": 0})
	})

	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Server.Close)
	return g
}

// BaseURL 返回可直接塞进 token 记录的 OpenAI 兼容基址（含 /v1）。
func (g *fakeGateway) BaseURL() string { return g.Server.URL + "/v1" }

// callbackStatuses 返回收到的回调状态（脚本上报 success/failed）。
func (g *fakeGateway) callbackStatuses() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.callbacks))
	for _, cb := range g.callbacks {
		s, _ := cb["status"].(string)
		out = append(out, s)
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ── 脚本执行辅助 ──────────────────────────────────────────────────────────────

// writeFakeOpencode 在 dir/bin/opencode 写一个只会打印版本号的假二进制，返回 bin 目录。
func writeFakeOpencode(t *testing.T, dir, version string) string {
	t.Helper()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	script := "#!/bin/sh\necho " + version + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0o755))
	return binDir
}

// requireTool 在缺少子进程依赖时跳过测试（CI 镜像可能没有 python3）。
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("缺少 %s，跳过脚本执行测试", name)
	}
}

// scriptPath 组装脚本执行所需的 PATH：测试指定的目录优先，之后补上 bash/python3/curl 所在目录。
func scriptPath(t *testing.T, pathDirs ...string) string {
	t.Helper()
	dirs := append([]string{}, pathDirs...)
	for _, tool := range []string{"bash", "python3", "curl"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("缺少 %s，跳过脚本执行测试", tool)
		}
		dirs = append(dirs, filepath.Dir(p))
	}
	dirs = append(dirs, "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	return strings.Join(dirs, ":")
}

// runMacScript 在指定 HOME 下用 bash 真跑一遍安装脚本，返回合并输出与退出码。
func runMacScript(t *testing.T, scriptText, home string, pathDirs ...string) (string, int) {
	t.Helper()
	return runMacScriptPath(t, scriptText, home, scriptPath(t, pathDirs...))
}

// runMacScriptPath 同 runMacScript，但 PATH 由调用方完全决定（用于构造"PATH 里没有 opencode"）。
func runMacScriptPath(t *testing.T, scriptText, home, path string) (string, int) {
	t.Helper()
	requireTool(t, "bash")

	dir := t.TempDir()
	scriptFile := filepath.Join(dir, "install.sh")
	require.NoError(t, os.WriteFile(scriptFile, []byte(scriptText), 0o755))
	require.NoError(t, os.MkdirAll(home, 0o755))

	// 脚本末尾会用 osascript 打开 opencode://；测试里换成 no-op，避免真弹系统通知。
	noopDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(noopDir, "osascript"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	cmd := exec.Command("bash", scriptFile)
	cmd.Env = mergedEnv(t, map[string]string{
		"HOME": home,
		"PATH": noopDir + ":" + path,
	})
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("执行脚本失败: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// mergedEnv 覆盖指定环境变量，其余沿用当前进程（避免同一个 key 出现两次）。
func mergedEnv(t *testing.T, overrides map[string]string) []string {
	t.Helper()
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, replaced := overrides[key]; replaced {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}

// readOpencodeConfig 读取 <home>/.config/opencode/opencode.json 并解析。
func readOpencodeConfig(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	require.NoError(t, err, "配置文件未生成")
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(raw, &cfg))
	return cfg
}

// seedOpencodeConfig 预置一份已存在的 opencode.json（模拟老用户升级）。
func seedOpencodeConfig(t *testing.T, home string, cfg map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".config", "opencode")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	raw, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode.json"), raw, 0o644))
}

// dig 按 key 路径取嵌套 JSON 值；取不到（或中途不是对象）返回 nil。
func dig(root any, keys ...string) any {
	cur := root
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// ── Windows 脚本执行辅助 ─────────────────────────────────────────────────────

// findPowerShell 找可用的 PowerShell；都没有就跳过（CI 的 Linux 镜像通常没有）。
func findPowerShell(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("缺少 PowerShell（pwsh/powershell），跳过 Windows 脚本测试")
	return ""
}

// runWinScript 在指定 USERPROFILE 下用 PowerShell 真跑一遍安装脚本。
func runWinScript(t *testing.T, scriptText, userProfile string, pathDirs ...string) (string, int) {
	t.Helper()
	shell := findPowerShell(t)

	dir := t.TempDir()
	scriptFile := filepath.Join(dir, "install.ps1")
	require.NoError(t, os.WriteFile(scriptFile, []byte(scriptText), 0o644))
	require.NoError(t, os.MkdirAll(userProfile, 0o755))

	dirs := append([]string{}, pathDirs...)
	dirs = append(dirs, "/usr/bin", "/bin", "/usr/sbin", "/sbin")

	cmd := exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptFile)
	cmd.Env = mergedEnv(t, map[string]string{
		"USERPROFILE": userProfile,
		"HOME":        userProfile,
		"PATH":        strings.Join(dirs, ":"),
	})
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("执行 PowerShell 脚本失败: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// isolatedToolPath 只用指向 bash/python3/curl 的软链接构成 PATH 前段，屏蔽宿主上
// 其它同名命令（例如真实 opencode），用来测"PATH 里找不到 opencode"这类分支。
func isolatedToolPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range []string{"bash", "python3", "curl"} {
		src, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("缺少 %s，跳过脚本执行测试", tool)
		}
		require.NoError(t, os.Symlink(src, filepath.Join(dir, tool)))
	}
	return dir
}
