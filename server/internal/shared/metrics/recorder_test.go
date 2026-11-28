package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusRecorder_FlushAndUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	var _ http.Flusher = rec
	rec.Flush()
	if !inner.Flushed {
		t.Fatal("Flush was not delegated to the underlying writer")
	}
	if rec.Unwrap() != inner {
		t.Fatal("Unwrap must return the underlying writer")
	}
}

func TestStatusRecorder_HijackUnsupported(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Hijacker.
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, _, err := rec.Hijack(); err == nil {
		t.Fatal("expected error when underlying writer cannot hijack")
	}
}

func TestStatusRecorder_HijackDelegates(t *testing.T) {
	hijacked := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		conn, _, err := http.NewResponseController(rec).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		hijacked <- err
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
	}
	if err := <-hijacked; err != nil {
		t.Fatalf("hijack through statusRecorder failed: %v", err)
	}
}
