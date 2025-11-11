package streaming

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OTelObserver is an Observer that records SSE stream metrics onto an OTel span.
// It counts every parsed JSON chunk and writes the final tally as an attribute
// when the stream closes.
type OTelObserver struct {
	span       trace.Span
	chunkCount int
}

// NewOTelObserver returns an OTelObserver that records metrics on span.
// If span is nil or a no-op span the observer is still safe to use.
func NewOTelObserver(span trace.Span) *OTelObserver {
	return &OTelObserver{span: span}
}

// OnJSONEvent increments the chunk counter for each parsed SSE JSON event.
func (o *OTelObserver) OnJSONEvent(_ map[string]any) {
	o.chunkCount++
}

// OnStreamClose records the final chunk count on the span as
// "stream.chunk_count".
func (o *OTelObserver) OnStreamClose() {
	if o.span == nil {
		return
	}
	o.span.SetAttributes(attribute.Int("stream.chunk_count", o.chunkCount))
}
