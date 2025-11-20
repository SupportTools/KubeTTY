package metrics

import "net/http"

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
