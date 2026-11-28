package metrics

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code before writing it.
func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

// Write ensures status is recorded even if WriteHeader isn't called explicitly.
func (rec *statusRecorder) Write(b []byte) (int, error) {
	return rec.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker so WebSocket upgrades work on instrumented
// routes. It delegates to the underlying ResponseWriter.
func (rec *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rec.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

// Flush implements http.Flusher so streaming responses work on instrumented
// routes. It is a no-op if the underlying ResponseWriter cannot flush.
func (rec *statusRecorder) Flush() {
	if flusher, ok := rec.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}
