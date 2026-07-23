package quota_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corequota "github.com/example/llm-gateway/internal/core/quota"
)

// 跨进程共享的配额键格式契约。
//
// 网关（写入方）与控制面（只读展示方）通过本包常量共享键格式。这些用例是
// 「键格式漂移」的回归闸门：历史上并发键前缀迁移时控制面读路径未同步，
// 导致后台「用户当前并发使用量」恒为 0 并持续误报告警。

func TestRPMKey_Format(t *testing.T) {
	tests := []struct {
		name                                      string
		subjectType, subjectID, layer, resourceID string
		want                                      string
	}{
		{"主体层 user", "user", "u1", "", "", "quota:rpm:user:u1"},
		{"主体层 service_key", "service_key", "k1", "", "", "quota:rpm:service_key:k1"},
		{"group 层", "user", "u1", "group", "gpt-4", "quota:rpm:user:u1:group:gpt-4"},
		{"model 层", "user", "u1", "model", "m-uuid", "quota:rpm:user:u1:model:m-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := corequota.RPMKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
			if got != tt.want {
				t.Fatalf("RPMKey = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConcurrencyKey_Format(t *testing.T) {
	tests := []struct {
		name                                      string
		subjectType, subjectID, layer, resourceID string
		want                                      string
	}{
		{"主体层 user", "user", "u1", "", "", "quota:conc:lease:user:u1"},
		{"group 层", "user", "u1", "group", "gpt-4", "quota:conc:lease:user:u1:group:gpt-4"},
		{"model 层", "user", "u1", "model", "m-uuid", "quota:conc:lease:user:u1:model:m-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := corequota.ConcurrencyKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
			if got != tt.want {
				t.Fatalf("ConcurrencyKey = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConcurrencyKeyPrefix_NotLegacyPrefix 并发键必须使用租约前缀：
// 旧前缀（quota:conc:<subject>:<id>，无 lease 段）是「字符串计数」时代的键，
// 新实现若回退到该前缀会与灰度期旧代码互撞（WRONGTYPE），且控制面读数会再次失真。
func TestConcurrencyKeyPrefix_NotLegacyPrefix(t *testing.T) {
	key := corequota.ConcurrencyKey("user", "u1", "", "")
	if !strings.HasPrefix(key, corequota.ConcurrencyKeyPrefix) {
		t.Fatalf("并发键 %q 必须以共享前缀 %q 开头", key, corequota.ConcurrencyKeyPrefix)
	}
	const legacyPrefix = "quota:conc:"
	if !strings.HasPrefix(key, legacyPrefix) {
		t.Fatalf("前缀 %q 应仍落在 %q 命名空间内（便于按前缀巡检）", corequota.ConcurrencyKeyPrefix, legacyPrefix)
	}
	legacyKey := legacyPrefix + "user:u1"
	if key == legacyKey {
		t.Fatalf("并发键不得沿用旧字符串计数键 %q", legacyKey)
	}
	if !strings.HasPrefix(corequota.ConcurrencyKeyPrefix, legacyPrefix) {
		t.Fatal("共享前缀常量必须显式声明为 quota:conc: 的子命名空间")
	}
}

// TestRPMAndConcurrencyPrefixes_Distinct 两类键前缀不得互相匹配，避免巡检/清理串扰。
func TestRPMAndConcurrencyPrefixes_Distinct(t *testing.T) {
	if strings.HasPrefix(corequota.ConcurrencyKeyPrefix, corequota.RPMKeyPrefix) ||
		strings.HasPrefix(corequota.RPMKeyPrefix, corequota.ConcurrencyKeyPrefix) {
		t.Fatalf("RPM 前缀 %q 与并发前缀 %q 不得互相包含",
			corequota.RPMKeyPrefix, corequota.ConcurrencyKeyPrefix)
	}
}

// TestConcurrencyKeySubject_EqualsSubjectScope 便捷写法必须与显式空 layer/resourceID 等价。
func TestConcurrencyKeySubject_EqualsSubjectScope(t *testing.T) {
	for _, subjectType := range []string{"user", "service_key"} {
		got := corequota.ConcurrencyKeySubject(subjectType, "abc")
		want := corequota.ConcurrencyKey(subjectType, "abc", "", "")
		if got != want {
			t.Fatalf("ConcurrencyKeySubject(%q) = %q, want %q", subjectType, got, want)
		}
	}
}

// TestConcurrencyInFlightRange_StrictlyGreaterThanNow 读侧区间必须左开（严格大于 now）：
// 写入侧按 `ZREMRANGEBYSCORE key -inf now` 清理，score == now 的条目视为已到期，
// 两侧边界必须一致，否则读到的在飞数会偏大。
func TestConcurrencyInFlightRange_StrictlyGreaterThanNow(t *testing.T) {
	now := time.UnixMilli(1_789_000_000_123)
	min, max := corequota.ConcurrencyInFlightRange(now)

	if want := "(1789000000123"; min != want {
		t.Fatalf("min = %q, want %q（必须左开括号，严格大于 now）", min, want)
	}
	if max != "+inf" {
		t.Fatalf("max = %q, want \"+inf\"", max)
	}
	if !strings.HasPrefix(min, "(") {
		t.Fatal("区间必须左开：score == now 属已到期，不能计入在飞")
	}
}

// TestConcurrencyInFlightRange_DoesNotDependOnWallClock 同一时刻多次调用必须稳定，
// 且只依赖传入时刻（便于两侧复用同一判定）。
func TestConcurrencyInFlightRange_DoesNotDependOnWallClock(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	firstMin, firstMax := corequota.ConcurrencyInFlightRange(now)
	time.Sleep(2 * time.Millisecond)
	secondMin, secondMax := corequota.ConcurrencyInFlightRange(now)
	if firstMin != secondMin || firstMax != secondMax {
		t.Fatalf("同一时刻应得到稳定区间: (%q,%q) vs (%q,%q)", firstMin, firstMax, secondMin, secondMax)
	}
}

// TestRPMInFlightRange_WindowAligned 读侧 RPM 区间必须与写入侧窗口口径一致：
// 写入侧按 `ZREMRANGEBYSCORE key -inf (now - RPMWindowMS)` 丢弃窗口外条目，
// 因此窗口内 = score 严格大于 (now - RPMWindowMS)，区间必须左开。
func TestRPMInFlightRange_WindowAligned(t *testing.T) {
	now := time.UnixMilli(1_789_000_000_000)
	min, max := corequota.RPMInFlightRange(now)

	wantCutoff := now.Add(-time.Duration(corequota.RPMWindowMS) * time.Millisecond).UnixMilli()
	if want := fmt.Sprintf("(%d", wantCutoff); min != want {
		t.Fatalf("min = %q, want %q（窗口下界必须左开且等于 now-window）", min, want)
	}
	if max != "+inf" {
		t.Fatalf("max = %q, want \"+inf\"", max)
	}
	if corequota.RPMWindowMS != 60*1000 {
		t.Fatalf("RPM 窗口 = %dms，窗口语义应与网关一致（60s）", corequota.RPMWindowMS)
	}
}
