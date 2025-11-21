package server

import (
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code before writing it.
func (lrw *loggingResponseWriter) WriteHeader(code int) {
	if !lrw.written {
		lrw.statusCode = code
		lrw.written = true
		lrw.ResponseWriter.WriteHeader(code)
	}
}

// Write ensures we capture the status code even if WriteHeader isn't called explicitly.
// If WriteHeader hasn't been called, this defaults to 200 OK.
func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if !lrw.written {
		lrw.statusCode = http.StatusOK
		lrw.written = true
	}
	return lrw.ResponseWriter.Write(b)
}

// LoggingMiddleware wraps an HTTP handler with request logging.
// It captures and logs the HTTP status code along with method, path, and duration.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log.WithFields(log.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
			"remote": r.RemoteAddr,
		}).Debug("Request received")

		// Wrap the ResponseWriter to capture status code
		lrw := &loggingResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default status
			written:        false,
		}

		next.ServeHTTP(lrw, r)

		log.WithFields(log.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"status":   lrw.statusCode,
			"duration": time.Since(start),
		}).Info("Request completed")
	})
}
