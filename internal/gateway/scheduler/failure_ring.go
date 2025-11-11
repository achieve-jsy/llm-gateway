package scheduler

import "time"

const defaultRingCapacity = 100

// NewFailureRingBuffer creates a failure ring buffer with the given capacity.
// If capacity <= 0, the default capacity of 100 is used.
func NewFailureRingBuffer(capacity int) *FailureRingBuffer {
	if capacity <= 0 {
		capacity = defaultRingCapacity
	}
	return &FailureRingBuffer{
		buf:      make([]time.Time, capacity),
		capacity: capacity,
	}
}

// Record records a failure timestamp into the circular buffer.
// When the buffer is full, the oldest entry is overwritten.
func (f *FailureRingBuffer) Record(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.buf[f.head] = t
	f.head = (f.head + 1) % f.capacity
	if f.size < f.capacity {
		f.size++
	}
}

// RecentFailures returns the number of recorded failures within the given window
// from now, iterating from newest to oldest. It stops counting when it encounters
// the first timestamp outside the window.
func (f *FailureRingBuffer) RecentFailures(window time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	cutoff := time.Now().Add(-window)
	count := 0

	for i := 1; i <= f.size; i++ {
		idx := (f.head - i + f.capacity) % f.capacity
		if f.buf[idx].Before(cutoff) {
			break
		}
		count++
	}
	return count
}
