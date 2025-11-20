package util

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// WriteJSON writes a JSON response with the specified status code.
// It sets the Content-Type header and encodes the payload.
// Returns any encoding error encountered.
func WriteJSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(payload)
}

// ClientIPFromRequest extracts the client IP address from an HTTP request.
// It first checks the X-Forwarded-For header (for proxied requests),
// then falls back to RemoteAddr. Returns empty string for nil requests.
func ClientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	// Check X-Forwarded-For header (proxy support)
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// WebSocketScheme determines the appropriate WebSocket scheme (ws or wss)
// based on the request's TLS configuration and X-Forwarded-Proto header.
func WebSocketScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "wss"
	}
	return "ws"
}
