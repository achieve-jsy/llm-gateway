package scheduler_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/stretchr/testify/assert"
)

func TestNewFailureRingBuffer_DefaultCapacity(t *testing.T) {
	tests := []struct {
		description string
		name        string
		capacity    int
	}{
		{
			description: "NewFailureRingBuffer(0)使用默认容量100",
			name:        "容量为0时使用默认值",
			capacity:    0,
		},
		{
			description: "NewFailureRingBuffer(-1)使用默认容量100",
			name:        "负数容量使用默认值",
			capacity:    -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ring := scheduler.NewFailureRingBuffer(tc.capacity)
			assert.NotNil(t, ring)

			for i := 0; i < 100; i++ {
				ring.Record(time.Now())
			}
			count := ring.RecentFailures(1 * time.Minute)
			assert.Equal(t, 100, count)
		})
	}
}

func TestFailureRingBuffer_Record(t *testing.T) {
	tests := []struct {
		description string
		name        string
		capacity    int
		records     int
		window      time.Duration
		wantCount   int
		useOldTime  bool
	}{
		{
			description: "Record N次，RecentFailures返回N",
			name:        "正常记录和查询",
			capacity:    10,
			records:     5,
			window:      1 * time.Minute,
			wantCount:   5,
		},
		{
			description: "Record的时间戳在窗口外，RecentFailures返回0",
			name:        "窗口外不计数",
			capacity:    10,
			records:     5,
			window:      1 * time.Minute,
			wantCount:   0,
			useOldTime:  true,
		},
		{
			description: "Record超过容量次数，最旧的被覆盖",
			name:        "环形覆盖",
			capacity:    5,
			records:     8,
			window:      1 * time.Minute,
			wantCount:   5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ring := scheduler.NewFailureRingBuffer(tc.capacity)

			for i := 0; i < tc.records; i++ {
				if tc.useOldTime {
					ring.Record(time.Now().Add(-2 * time.Minute))
				} else {
					ring.Record(time.Now())
				}
			}

			count := ring.RecentFailures(tc.window)
			assert.Equal(t, tc.wantCount, count)
		})
	}
}

func TestFailureRingBuffer_RingOverwrite(t *testing.T) {
	t.Run("覆盖后仍能正确计数最近窗口", func(t *testing.T) {
		ring := scheduler.NewFailureRingBuffer(5)

		for i := 0; i < 5; i++ {
			ring.Record(time.Now().Add(-3 * time.Minute))
		}

		for i := 0; i < 3; i++ {
			ring.Record(time.Now())
		}

		count := ring.RecentFailures(1 * time.Minute)
		assert.Equal(t, 3, count)
	})
}
