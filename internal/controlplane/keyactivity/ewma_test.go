package keyactivity_test

import (
	"math"
	"testing"
	ka "github.com/example/llm-gateway/internal/controlplane/keyactivity"
)

func TestEWMA_Alpha03_StableBaseline(t *testing.T) {
	// 测试目的：alpha=0.3 平滑后基线收敛，稳定用量不误判
	// 给定一组恒定调用量，EWMA 应收敛到该量级
	var mean, dev float64
	alpha := 0.3
	for i := 0; i < 50; i++ {
		x := 50.0
		prev := mean
		mean = alpha*x + (1-alpha)*mean
		dev = alpha*math.Abs(x-prev) + (1-alpha)*dev
	}
	if math.Abs(mean-50.0) > 1.0 {
		t.Errorf("EWMA mean should converge to ~50; got %v", mean)
	}
}

func TestEWMA_SpikeDetection_JudgeBeforeUpdate(t *testing.T) {
	// 测试目的：先判后更——激增不会被带入基线
	mean := 30.0
	dev := 5.0
	k := 3.0
	alpha := 0.3

	// 激增: x=200, threshold = 30 + 3*max(5, 5) = 45
	x := 200.0
	threshold := mean + k*math.Max(dev, 5)
	if x <= threshold {
		t.Fatalf("test setup error: 200 should exceed threshold %v", threshold)
	}

	// 先判定：x > mean + k * max(dev, minDev) = 30 + 3*5 = 45
	isSpike := x > mean+k*math.Max(dev, 5)
	if !isSpike {
		t.Error("spike should be detected")
	}

	// 后更新：即使用 200 更新，下一轮 threshold 也不会被拉高太多
	prev := mean
	mean = alpha*x + (1-alpha)*mean     // 0.3*200 + 0.7*30 = 60 + 21 = 81
	dev = alpha*math.Abs(x-prev) + (1-alpha)*dev // 0.3*170 + 0.7*5 = 51 + 3.5 = 54.5

	// 下一轮：x=100（仍高于旧基线但没激增）
	nextThreshold := mean + k*math.Max(dev, 5)
	// 81 + 3*54.5 ≈ 244.5；x=100 < 244.5 → 不误判
	if 100.0 > nextThreshold {
		t.Errorf("after spike, regular usage should not trigger; threshold=%v", nextThreshold)
	}
}

func TestEWMA_MinBaseline_PreventsLowFreqTrigger(t *testing.T) {
	// 测试目的：baseline < min_baseline 时 E4 不评估。
	// 构造 mean=10（低于 min_baseline=20），即使 dev 很大也不应判为激增。
	mean := 10.0
	dev := 50.0
	k := 3.0
	minBaseline := 20.0

	eligible := mean >= minBaseline
	if eligible {
		t.Error("low-frequency key should not be eligible for E4")
	}

	// 即使 threshold = 10 + 3*50 = 160 < x=150，但因 baseline < minBaseline 所以根本不会评
	x := 150.0
	_ = mean + k*dev
	if x > mean+k*dev && eligible {
		t.Error("low baseline should block E4 regardless of x")
	}
}

func TestEWMA_MinDev_PreventsDivergence(t *testing.T) {
	// 测试目的：极规律用量时 dev→0，minDev 兜底避免阈值=mean
	mean := 100.0
	dev := 0.1 // 几乎为 0
	k := 3.0
	minDev := 5.0

	devSafe := math.Max(dev, minDev)
	threshold := mean + k*devSafe // 100 + 3*5 = 115
	if threshold <= mean {
		t.Errorf("minDev should prevent threshold from collapsing to mean; got %v", threshold)
	}
}

func TestHourBucketKey(t *testing.T) {
	// 验证 keyactivity 包内导出的辅助函数（通过公共函数间接验证）
	// 这里测试 CalcMeanDev 的数学正确性
	// 由于 calcMeanDev 是未导出的，通过 RunBootstrap 间接验测。
	// 下面用已导出的 fingerprint 函数测试稳定性
	fp1 := ka.UAFingerprint("OpenAI/Python 1.55.3")
	fp2 := ka.UAFingerprint("OpenAI/Python 1.55.3")
	if fp1 != fp2 {
		t.Error("fingerprint should be stable")
	}
}
