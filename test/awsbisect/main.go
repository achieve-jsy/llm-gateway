// 临时诊断工具：用生产库中解密的 Bedrock 凭据，复现 2026-10-10 grok-4.6 的
// "The task request was rejected by the target" 并做变量二分。
// 不属于项目代码，用完即删。
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"
	_ "github.com/lib/pq"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/bedrock"
)

const (
	defaultModel = "global.xai.grok-4.6"
	region       = "us-east-1"
)

// dbConnStr 只从环境变量读取，禁止把数据库凭据写进仓库。
// 用法：AWSBISECT_DSN='postgres://user:pass@host:port/db?sslmode=disable' ./awsbisect
var dbConnStr = os.Getenv("AWSBISECT_DSN")

func aesDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

func loadDEK(db *sql.DB) ([]byte, error) {
	var kekB64 string
	if err := db.QueryRow(`SELECT value FROM system_configs WHERE key='encryption_kek'`).Scan(&kekB64); err != nil {
		return nil, fmt.Errorf("kek: %w", err)
	}
	kek, err := base64.StdEncoding.DecodeString(kekB64)
	if err != nil {
		return nil, err
	}
	var version int
	var ekB64 string
	if err := db.QueryRow(`SELECT version, encrypted_key FROM encryption_keys WHERE status='active' ORDER BY version DESC LIMIT 1`).Scan(&version, &ekB64); err != nil {
		return nil, fmt.Errorf("dek: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(ekB64)
	if err != nil {
		return nil, err
	}
	return aesDecrypt(kek, raw)
}

func decryptAPIKey(dek []byte, enc string) (string, error) {
	i := strings.Index(enc, ":")
	if i < 0 {
		return "", fmt.Errorf("not versioned format")
	}
	raw, err := base64.StdEncoding.DecodeString(enc[i+1:])
	if err != nil {
		return "", err
	}
	pt, err := aesDecrypt(dek, raw)
	if err != nil {
		return "", err
	}
	var shape map[string]string
	if err := json.Unmarshal(pt, &shape); err != nil {
		return "", fmt.Errorf("json: %w", err)
	}
	if v, ok := shape["api_key"]; ok && v != "" {
		return v, nil
	}
	return string(pt), nil
}

func loadAWSKey() (string, error) {
	if dbConnStr == "" {
		return "", errors.New("缺少环境变量 AWSBISECT_DSN（数据库连接串，禁止写进代码）")
	}
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		return "", err
	}
	defer db.Close()
	dek, err := loadDEK(db)
	if err != nil {
		return "", err
	}
	var enc string
	if err := db.QueryRow(`SELECT encrypted_credentials FROM providers WHERE definition_id='bedrock' AND name='aws'`).Scan(&enc); err != nil {
		return "", err
	}
	return decryptAPIKey(dek, enc)
}

func truncToolDescriptions(tools []map[string]any, n int) {
	for _, t := range tools {
		fn, ok := t["function"].(map[string]any)
		if !ok {
			continue
		}
		if d, ok := fn["description"].(string); ok && len(d) > n {
			fn["description"] = d[:n]
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

func main() {
	modelID := defaultModel
	if v := os.Getenv("MODEL"); v != "" {
		modelID = v
	}
	key, err := loadAWSKey()
	if err != nil {
		fmt.Println("load AWS key from prod DB failed:", err)
		os.Exit(1)
	}
	fmt.Println("AWS 凭据已解密（不打印），开始二分探针；model =", modelID)

	path := os.Getenv("REQ_FILE")
	if path == "" {
		path = "/tmp/fail_ns.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("read request:", err)
		os.Exit(1)
	}
	var a core.AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &a); err != nil {
		fmt.Println("parse request:", err)
		os.Exit(1)
	}

	p := bedrock.New(providers.ProviderConfig{Type: "bedrock", APIKey: key, BaseURL: region},
		providers.ProviderOptions{})

	if os.Getenv("ROUND") == "8" {
		rawControl(key, region)
		return
	}

	type mutation struct {
		name  string
		apply func(*core.ChatRequest)
	}
	var cases []mutation
	if os.Getenv("ROUND") == "6" {
		// P0-3 验收：超长 tool_use id（>64 字符、含非法字符）经网关清洗后应被上游接受。
		tok := 512
		fmt.Println("== raw control: 开始")
		longID := "call_" + strings.Repeat("x", 90) + "/bad?id"
		cases = []mutation{
			{"6-long-tool-use-id", func(r *core.ChatRequest) {
				r.Model = modelID
				r.MaxTokens = &tok
				r.Tools = []map[string]any{{"type": "function", "function": map[string]any{
					"name":        "get_weather",
					"description": "查询城市天气",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"city": map[string]any{"type": "string"}},
						"required":   []string{"city"},
					},
				}}}
				r.Messages = []core.Message{
					{Role: "user", Content: "北京天气如何？"},
					{Role: "assistant", ToolCalls: []core.ToolCall{{
						ID: longID, Type: "function",
						Function: core.FunctionCall{Name: "get_weather", Arguments: `{"city":"北京"}`},
					}}},
					{Role: "tool", ToolCallID: longID, Content: `{"temp_c":26,"desc":"晴"}`},
				}
			}},
		}
	} else if os.Getenv("ROUND") == "10" {
		// 定位「36 工具 + 完整上下文就卡住」的规模阈值：工具数 5 / 10 / 18。
		for _, n := range []int{5, 10, 18} {
			count := n
			cases = append(cases, mutation{
				name: fmt.Sprintf("10-tools-first-%d", count),
				apply: func(r *core.ChatRequest) {
					tok := 256
					r.Model = modelID
					r.MaxTokens = &tok
					if len(r.Tools) > count {
						r.Tools = r.Tools[:count]
					}
				},
			})
		}
	} else if os.Getenv("ROUND") == "9" {
		// 对照：同一条完整 CC 请求，只把输出预算压到 256，验证「慢」来自输出预算/思考长度。
		tok := 256
		cases = []mutation{
			{"9-full-repro-max256", func(r *core.ChatRequest) {
				r.Model = modelID
				r.MaxTokens = &tok
			}},
		}
	} else if os.Getenv("ROUND") == "5" {
		// P0-2 验收：Claude Code 那条完整的失败请求（36 工具，含 ArtifactData）。
		tok := 1024
		cases = []mutation{
			{"5-full-repro-nonstream", func(r *core.ChatRequest) {
				r.Model = modelID
				r.MaxTokens = &tok
			}},
		}
	} else if os.Getenv("ROUND") == "7" {
		// P0-1 验收：流式路径在上游于流内报错时必须把错误抛出来，而不是正常 EOF。
		tok := 1024
		cases = []mutation{
			{"7-stream-full-repro", func(r *core.ChatRequest) {
				r.Model = modelID
				r.MaxTokens = &tok
				r.Stream = true
			}},
		}
	} else if os.Getenv("ROUND") == "4" {
		tok := 256
		mk := func(name, pattern string) []map[string]any {
			prop := map[string]any{"type": "string"}
			if pattern != "" {
				prop["pattern"] = pattern
			}
			return []map[string]any{{"type": "function", "function": map[string]any{
				"name": "foo", "description": "test",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"a": prop}},
			}}}
		}
		// 去掉正则里的负向前瞻 (?!...) 分组
		stripLookahead := func(s string) string {
			for {
				i := strings.Index(s, "(?!")
				if i < 0 {
					return s
				}
				depth, j := 0, i
				for ; j < len(s); j++ {
					if s[j] == '(' {
						depth++
					} else if s[j] == ')' {
						depth--
						if depth == 0 {
							break
						}
					}
				}
				if j >= len(s) {
					return s
				}
				s = s[:i] + s[j+1:]
			}
		}
		var walk func(any) any
		walk = func(n any) any {
			switch v := n.(type) {
			case map[string]any:
				for k, vv := range v {
					if k == "pattern" {
						if str, ok := vv.(string); ok && strings.Contains(str, "(?!") {
							v[k] = stripLookahead(str)
							continue
						}
					}
					v[k] = walk(vv)
				}
				return v
			case []any:
				for i := range v {
					v[i] = walk(v[i])
				}
				return v
			}
			return n
		}
		cases = []mutation{
			{"r4-1 lookahead(?!..)", func(r *core.ChatRequest) {
				r.Tools = mk("foo", `^(?!\.\.?(?:\/|$))[A-Za-z0-9_]{1,200}$`)
				r.MaxTokens = &tok
			}},
			{"r4-2 same-without-lookahead", func(r *core.ChatRequest) {
				r.Tools = mk("foo", `^[A-Za-z0-9_]{1,200}$`)
				r.MaxTokens = &tok
			}},
			{"r4-3 ArtifactData minus (?!..)", func(r *core.ChatRequest) {
				for _, t := range r.Tools {
					fn, _ := t["function"].(map[string]any)
					if fn == nil || fn["name"] != "ArtifactData" {
						continue
					}
					fn["parameters"] = walk(fn["parameters"])
					r.Tools = []map[string]any{t}
					break
				}
				r.MaxTokens = &tok
			}},
		}
	} else if os.Getenv("ROUND") == "3" {
		tok := 256
		for i, tool := range a.Tools {
			idx, name, desc := i, tool.Name, tool.Description
			var schema map[string]any
			if len(tool.InputSchema) > 0 {
				_ = json.Unmarshal(tool.InputSchema, &schema)
			}
			cases = append(cases, mutation{
				name: fmt.Sprintf("solo-%02d-%s", idx, name),
				apply: func(r *core.ChatRequest) {
					r.Tools = []map[string]any{{"type": "function", "function": map[string]any{
						"name": name, "description": desc, "parameters": schema,
					}}}
					r.MaxTokens = &tok
				},
			})
		}
	} else if os.Getenv("ROUND") == "2" {
		tok := 256
		trivial := func(name string, extra map[string]any) []map[string]any {
			schema := map[string]any{
				"type":       "object",
				"properties": map[string]any{"a": map[string]any{"type": "string"}},
			}
			for k, v := range extra {
				schema[k] = v
			}
			return []map[string]any{{"type": "function", "function": map[string]any{
				"name": name, "description": "test tool", "parameters": schema,
			}}}
		}
		minimal := func(r *core.ChatRequest) {
			r.Messages = []core.Message{{Role: "user", Content: "hi"}}
			r.MaxTokens = &tok
		}
		cases = []mutation{
			{"r2-1 trivial tool +fullCtx", func(r *core.ChatRequest) {
				r.Tools = trivial("foo", nil)
				r.MaxTokens = &tok
			}},
			{"r2-2 trivial tool +minCtx", func(r *core.ChatRequest) {
				r.Tools = trivial("foo", nil)
				minimal(r)
			}},
			{"r2-3 no tools +minCtx", func(r *core.ChatRequest) { r.Tools = nil; minimal(r) }},
			{"r2-4 trivial+$schema +minCtx", func(r *core.ChatRequest) {
				r.Tools = trivial("foo", map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema"})
				minimal(r)
			}},
			{"r2-5 trivial+addlProps +minCtx", func(r *core.ChatRequest) {
				r.Tools = trivial("foo", map[string]any{"additionalProperties": false})
				minimal(r)
			}},
			{"r2-6 real Bash tool +minCtx", func(r *core.ChatRequest) {
				for _, t := range r.Tools {
					if fn, ok := t["function"].(map[string]any); ok && fn["name"] == "Bash" {
						r.Tools = []map[string]any{t}
						break
					}
				}
				minimal(r)
			}},
		}
	} else {
		cases = []mutation{
			{"1-full-repro", nil},
			{"2-no-tools", func(r *core.ChatRequest) { r.Tools = nil }},
			{"3-tools-desc-trunc-1024", func(r *core.ChatRequest) { truncToolDescriptions(r.Tools, 1024) }},
			{"4-first-10-tools", func(r *core.ChatRequest) {
				if len(r.Tools) > 10 {
					r.Tools = r.Tools[:10]
				}
			}},
		}
	}

	for _, c := range cases {
		req := core.AnthropicToChatRequest(&a)
		req.Model = modelID
		if c.apply != nil {
			c.apply(req)
		}
		ntools := len(req.Tools)
		mt := 0
		if req.MaxTokens != nil {
			mt = *req.MaxTokens
		}
		timeout := 5 * time.Minute
		if v := os.Getenv("TIMEOUT_SEC"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				timeout = time.Duration(n) * time.Second
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		t0 := time.Now()
		var detail string
		var ok bool
		if req.Stream {
			// 流式：上游在流内报错时，Read 必须返回错误而不是正常 EOF。
			stream, serr := p.StreamChatCompletion(ctx, req)
			if serr != nil {
				detail = "openStream error: " + errText(serr)
			} else {
				body, rerr := io.ReadAll(stream)
				stream.Close()
				hasDone := strings.Contains(string(body), "[DONE]")
				switch {
				case rerr != nil:
					ok = true
					detail = fmt.Sprintf("流内错误已透出(%d bytes): %s", len(body), errText(rerr))
				case hasDone && len(body) > 0:
					ok = true
					detail = fmt.Sprintf("流式正常结束(%d bytes, [DONE] 已发)", len(body))
				default:
					detail = fmt.Sprintf("空流静默 EOF(%d bytes, DONE=%v)", len(body), hasDone)
				}
			}
		} else {
			resp, err := p.ChatCompletion(ctx, req)
			ok = err == nil
			if err != nil {
				detail = errText(err)
			} else if resp != nil && len(resp.Choices) > 0 {
				detail = fmt.Sprintf("finish=%s toolCalls=%d contentLen=%d",
					resp.Choices[0].FinishReason, len(resp.Choices[0].Message.ToolCalls),
					len(fmt.Sprint(resp.Choices[0].Message.Content)))
			}
		}
		el := time.Since(t0)
		cancel()
		fmt.Printf("%-26s tools=%-3d max=%-6d %6.1fs  %s %s\n", c.name, ntools, mt, el.Seconds(),
			map[bool]string{true: "OK  ", false: "FAIL"}[ok], detail)
		os.Stdout.Sync()
	}
}

// docFromJSON 把 JSON 字符串转成 Bedrock 的 document 值。
func docFromJSON(raw string) brdoc.Interface {
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return brdoc.NewLazyDocument(v)
}

// 对照实验：绕开网关的兼容层，用 AWS SDK 直接打 Bedrock，观察上游原始行为。
func rawControl(token, region string) {
	cfg, _ := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(awssdk.AnonymousCredentials{}),
	)
	bp := bearer.TokenProviderFunc(func(context.Context) (bearer.Token, error) {
		return bearer.Token{Value: token}, nil
	})
	rt := bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		o.BearerAuthTokenProvider = bp
		o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
	})
	model := "global.xai.grok-4.6"

	// (a) 非流式：超长且含非法字符的 toolUseId（P0-3 的「改之前」）
	fmt.Println("== raw control: 开始")
	longID := "call_" + strings.Repeat("x", 90) + "/bad?id"
	spec := brtypes.ToolSpecification{
		Name:        awssdk.String("get_weather"),
		InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: docFromJSON(`{"type":"object","properties":{"city":{"type":"string"}}}`)},
	}
	ctxA, cancelA := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelA()
	_, err := rt.Converse(ctxA, &bedrockruntime.ConverseInput{
		ModelId: awssdk.String(model),
		Messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "hi"}}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberToolUse{Value: brtypes.ToolUseBlock{
					Name: awssdk.String("get_weather"), ToolUseId: awssdk.String(longID), Input: docFromJSON(`{"city":"北京"}`),
				}},
			}},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: awssdk.Int32(256)},
		ToolConfig:      &brtypes.ToolConfiguration{Tools: []brtypes.Tool{&brtypes.ToolMemberToolSpec{Value: spec}}},
	})
	fmt.Printf("raw-长toolUseId(未清洗)      err=%s\n", errText(err))

	// (b) 流式：工具 schema 含负向前瞻（P0-1 的「改之前」：HTTP 200 + 流内 error 事件）
	lookaheadSpec := brtypes.ToolSpecification{
		Name: awssdk.String("ArtifactData"),
		InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: docFromJSON(
			`{"type":"object","properties":{"collection":{"type":"string","pattern":"^(?!\\.\\.?(?:\\/|$))[A-Za-z0-9_]{1,200}$"}}}`)},
	}
	fmt.Println("== raw control: 流式用例开始")
	ctxB, cancelB := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancelB()
	sout, serr := rt.ConverseStream(ctxB, &bedrockruntime.ConverseStreamInput{
		ModelId:         awssdk.String(model),
		Messages:        []brtypes.Message{{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "hi"}}}},
		InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: awssdk.Int32(256)},
		ToolConfig:      &brtypes.ToolConfiguration{Tools: []brtypes.Tool{&brtypes.ToolMemberToolSpec{Value: lookaheadSpec}}},
	})
	if serr != nil {
		fmt.Printf("raw-流式+lookahead            openStream err=%s\n", errText(serr))
	} else {
		n := 0
		start := time.Now()
		for ev := range sout.GetStream().Events() {
			n++
			fmt.Printf("   [%5.1fs] event %d: %T\n", time.Since(start).Seconds(), n, ev)
			os.Stdout.Sync()
		}
		fmt.Printf("raw-流式+lookahead            事件数=%d 用时=%.1fs streamErr=%s\n", n, time.Since(start).Seconds(), errText(sout.GetStream().Err()))
	}
}
