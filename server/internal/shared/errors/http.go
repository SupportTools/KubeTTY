package errors

import (
	"encoding/json"
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
func WriteError(w http.ResponseWriter, errResp ErrorResponse) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(errResp.Status)
	return json.NewEncoder(w).Encode(errResp)
}
