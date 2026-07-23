package service

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestBedrockProbeMaxTokensMeetsBedrockMinimum 锁定探针 max_tokens 的下界。
//
// Bedrock 上 OpenAI GPT 系列 / xAI Grok 系列要求 max_output_tokens >= 16，
// 取 1 会被上游拒绝（ValidationException: integer_below_min_value），
// 导致完全正常的模型被探针判为不可用并在连续失败后自动禁用。
func TestBedrockProbeMaxTokensMeetsBedrockMinimum(t *testing.T) {
	if BedrockProbeMaxTokens < 16 {
		t.Fatalf("BedrockProbeMaxTokens = %d，必须 >= 16，否则 OpenAI/Grok 模型会被误判为不可用", BedrockProbeMaxTokens)
	}
}

// TestBedrockProbeTimeoutMeetsSlowModelBudget 锁定探针超时的下界。
//
// 15s 对 gpt-6-astra 这类慢模型太紧：探针耗时会稳定超过 15s，检查结果变成
// context deadline exceeded，模型被反复误判为不可用并自动禁用。
func TestBedrockProbeTimeoutMeetsSlowModelBudget(t *testing.T) {
	if BedrockProbeTimeout < 30*time.Second {
		t.Fatalf("BedrockProbeTimeout = %v，对慢模型太紧（需 >= 30s），会被误判为不可用", BedrockProbeTimeout)
	}
}

// probeMaxTokensLiteralPattern 匹配把探针 max_tokens 写死成 1 的写法。
// 同一条 Bedrock 探针逻辑曾在 health_check_service（后台检测）、model_check_public（手动检测）、
// test_handler（渠道测试）三处各自写死过 max_tokens=1，这里做一次源码级回归防护。
var probeMaxTokensLiteralPattern = regexp.MustCompile(
	`(MaxTokens:\s*(int32Ptr|aws\.Int32|int32)\(\s*1\s*\))|(maxTokens\s*:=\s*int32\(\s*1\s*\))`,
)

// TestBedrockProbeMaxTokensNotHardcoded 扫描控制面源码，禁止再出现写死的探针 max_tokens=1。
func TestBedrockProbeMaxTokensNotHardcoded(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	// internal/controlplane/service -> internal/controlplane -> <repo root>
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	scanDir := filepath.Join(root, "internal", "controlplane")
	if _, err := os.Stat(scanDir); err != nil {
		t.Fatalf("扫描目录不存在: %s (%v)", scanDir, err)
	}

	var offenders []string
	err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if probeMaxTokensLiteralPattern.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描控制面源码失败: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("探针 max_tokens 不得写死为 1（OpenAI/Grok 要求 >= 16），请改用 service.BedrockProbeMaxTokens:\n%s",
			strings.Join(offenders, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
