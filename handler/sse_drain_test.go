package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
)

// failingWriter accepts the headers and the first event, then fails every
// later write, the way a disconnected client's connection does.
type failingWriter struct {
	http.ResponseWriter
	writes int
}

func (f *failingWriter) Write(b []byte) (int, error) {
	f.writes++
	if f.writes > 1 {
		return 0, errors.New("client gone")
	}
	return f.ResponseWriter.Write(b)
}

func (f *failingWriter) Flush() {}

func TestRootSSE_BareProducerFinishesAfterClientDisconnect(t *testing.T) {
	producerDone := make(chan struct{})

	// Sends without ever selecting on ctx.Done(), like the framework's own examples.
	fn := func(_ context.Context, _ *metadata.AuthInfo, _ *http.Request, events chan<- handler.SSEEvent) error {
		defer close(producerDone)
		for i := range 20 {
			events <- handler.SSEEvent{Event: "tick", Data: i}
		}
		return nil
	}

	w := &failingWriter{ResponseWriter: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/events", nil)

	handlerDone := make(chan struct{})
	go func() {
		handler.RootSSE(fn).ServeHTTP(w, req)
		close(handlerDone)
	}()

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after the client disconnected")
	}

	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("producer goroutine leaked: still blocked sending after the client disconnected")
	}
}
