package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type flushTrackingResponseWriter struct {
	headers    http.Header
	flushes    chan struct{}
	writes     chan string
	status     int
	writeError error
}

func (w *flushTrackingResponseWriter) Header() http.Header {
	return w.headers
}

func (w *flushTrackingResponseWriter) Write(body []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	w.writes <- string(body)
	return len(body), nil
}

func (w *flushTrackingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *flushTrackingResponseWriter) Flush() {
	select {
	case w.flushes <- struct{}{}:
	default:
	}
}

func TestEventsSSEFlushesConnectionImmediately(t *testing.T) {
	s := &server{
		subs: make(map[chan webhookEvent]struct{}),
	}
	recorder := &flushTrackingResponseWriter{
		headers: make(http.Header),
		flushes: make(chan struct{}, 1),
		writes:  make(chan string, 1),
	}
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(requestContext)
	done := make(chan struct{})

	go func() {
		s.eventsSSE(recorder, request)
		close(done)
	}()

	select {
	case firstEvent := <-recorder.writes:
		if firstEvent != ": connected\n\n" {
			t.Fatalf("unexpected initial SSE event: %q", firstEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("expected an initial SSE comment before the first keepalive")
	}

	select {
	case <-recorder.flushes:
	case <-time.After(time.Second):
		t.Fatal("expected the initial SSE comment to be flushed")
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("unexpected SSE content type: %q", contentType)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected the SSE handler to stop after the request was canceled")
	}
}

func TestEventsSSEStopsWhenInitialWriteFails(t *testing.T) {
	s := &server{
		subs: make(map[chan webhookEvent]struct{}),
	}
	recorder := &flushTrackingResponseWriter{
		headers:    make(http.Header),
		writeError: errors.New("client disconnected"),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)

	s.eventsSSE(recorder, request)

	if len(s.subs) != 0 {
		t.Fatalf("expected failed SSE connection to be removed, got %d subscriptions", len(s.subs))
	}
}
