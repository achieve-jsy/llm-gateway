package scheduler

import (
	"sync"
	"time"
)

// HealthEvent represents a health status transition event.
type HealthEvent struct {
	ModelID   string
	GroupID   string
	ModelName string
	OldStatus HealthStatus
	NewStatus HealthStatus
	Reason    string
	Timestamp time.Time
}

// EventPublisher publishes HealthEvents to registered subscribers.
// Subscribers receive events via buffered channels. If a subscriber's
// channel is full, the event is dropped for that subscriber (non-blocking).
type EventPublisher struct {
	mu   sync.RWMutex
	subs []chan HealthEvent
}

// Publish sends a HealthEvent to all subscribers asynchronously.
// If a subscriber's channel is full, the event is dropped.
func (ep *EventPublisher) Publish(event HealthEvent) {
	ep.mu.RLock()
	defer ep.mu.RUnlock()

	for _, ch := range ep.subs {
		select {
		case ch <- event:
		default:
			// drop if subscriber channel is full
		}
	}
}

// Subscribe creates a new buffered channel (capacity 16) and registers it.
// Returns the receive-only channel for consuming events.
func (ep *EventPublisher) Subscribe() <-chan HealthEvent {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	ch := make(chan HealthEvent, 16)
	ep.subs = append(ep.subs, ch)
	return ch
}

// Unsubscribe removes a previously subscribed channel.
// If the channel is not found, this is a no-op.
func (ep *EventPublisher) Unsubscribe(ch <-chan HealthEvent) {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	for i, sub := range ep.subs {
		if sub == ch {
			ep.subs = append(ep.subs[:i], ep.subs[i+1:]...)
			close(sub)
			return
		}
	}
}
