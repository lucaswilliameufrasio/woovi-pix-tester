package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadDotEnv(t *testing.T) {
	t.Run("Should load quoted settings from a dotenv file", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(configPath, []byte("WOOVI_APP_ID=\"sandbox-app-id\"\nPORT=9090\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		clearEnvironmentVariable(t, "WOOVI_APP_ID")
		clearEnvironmentVariable(t, "PORT")

		if err := loadDotEnv(configPath); err != nil {
			t.Fatalf("expected dotenv file to load: %v", err)
		}
		if appID := os.Getenv("WOOVI_APP_ID"); appID != "sandbox-app-id" {
			t.Fatalf("unexpected AppID setting: %q", appID)
		}
		if port := os.Getenv("PORT"); port != "9090" {
			t.Fatalf("unexpected port setting: %q", port)
		}
	})

	t.Run("Should preserve settings already present in the environment", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(configPath, []byte("WOOVI_APP_ID=file-app-id\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("WOOVI_APP_ID", "environment-app-id")

		if err := loadDotEnv(configPath); err != nil {
			t.Fatalf("expected dotenv file to load: %v", err)
		}
		if appID := os.Getenv("WOOVI_APP_ID"); appID != "environment-app-id" {
			t.Fatalf("environment setting was not preserved: %q", appID)
		}
	})

	t.Run("Should reject malformed settings", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(configPath, []byte("not-a-setting\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadDotEnv(configPath); err == nil || !strings.Contains(err.Error(), "linha") {
			t.Fatalf("expected malformed setting error, got %v", err)
		}
	})
}

func clearEnvironmentVariable(t *testing.T, key string) {
	t.Helper()
	previousValue, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("expected environment variable to be cleared: %v", err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, previousValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
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
