package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// WriteError writes a standardized JSON error response to the http.ResponseWriter.
// It sets the Content-Type header to application/json and writes the appropriate
// HTTP status code before encoding the error response as JSON.
//
// This function should be used for all API error responses to ensure consistency
// across the application.
//
// Example usage:
//
//	err := errors.NotFound("session not found", "")
//	errors.WriteError(w, err)
//
// The response will be formatted as:
//
//	{
//	  "status": 404,
//	  "error": "not_found",
//	  "message": "session not found",
//	  "details": ""
//	}
//
// Note: The details field is omitted from the JSON response when empty due to
// the omitempty tag on the ErrorResponse struct.
//
// Security: This function validates the ErrorResponse to prevent panics and
// attempts to detect double-write scenarios by checking Content-Type header.
func WriteError(w http.ResponseWriter, errResp ErrorResponse) error {
	// Validate HTTP status code to prevent panics
	if errResp.Status < 100 || errResp.Status > 599 {
		// Fallback to 500 for invalid status codes
		errResp.Status = http.StatusInternalServerError
		errResp.Error = CodeInternalServerError
		errResp.Message = "internal server error"
		errResp.Details = ""
	}

	// Check if Content-Type is already set (indicates response may have started)
	// Note: This is a best-effort check, not foolproof
	if ct := w.Header().Get("Content-Type"); ct != "" && ct != "application/json" {
		return fmt.Errorf("response already started with Content-Type: %s", ct)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(errResp.Status)
	return json.NewEncoder(w).Encode(errResp)
}
