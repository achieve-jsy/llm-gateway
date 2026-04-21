package streaming_test

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/streaming"
	"github.com/stretchr/testify/require"
)

type closeCountingObserver struct {
	closes atomic.Int32
}

func (*closeCountingObserver) OnJSONEvent(map[string]any) {}

func (o *closeCountingObserver) OnStreamClose() {
	o.closes.Add(1)
}

type closeCountingReader struct {
	io.Reader
	closes atomic.Int32
}

func (r *closeCountingReader) Close() error {
	r.closes.Add(1)
	return nil
}

func TestObservedSSEStream_ConcurrentCloseNotifiesExactlyOnce(t *testing.T) {
	source := &closeCountingReader{Reader: strings.NewReader("")}
	observer := &closeCountingObserver{}
	stream := streaming.NewObservedSSEStream(source, observer)
	start := make(chan struct{})
	errs := make(chan error, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := stream.Close(); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int32(1), observer.closes.Load())
	require.Equal(t, int32(1), source.closes.Load())
}
