package util

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the specified status code.
// It sets the Content-Type header and encodes the payload.
// Returns any encoding error encountered.
func WriteJSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(payload)
}
