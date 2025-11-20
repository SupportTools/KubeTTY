package server

import (
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// LoggingMiddleware wraps an HTTP handler with request logging.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log.WithFields(log.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
			"remote": r.RemoteAddr,
		}).Debug("Request received")

		next.ServeHTTP(w, r)

		log.WithFields(log.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start),
		}).Info("Request completed")
	})
}
