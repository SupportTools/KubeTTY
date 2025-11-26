package errors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestErrorConstructors verifies that all error constructor functions
// return ErrorResponse structs with the correct status codes and error codes.
func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name       string
		fn         func(string, string) ErrorResponse
		wantStatus int
		wantError  string
	}{
		{
			name:       "BadRequest",
			fn:         BadRequest,
			wantStatus: http.StatusBadRequest,
			wantError:  CodeBadRequest,
		},
		{
			name:       "Unauthorized",
			fn:         Unauthorized,
			wantStatus: http.StatusUnauthorized,
			wantError:  CodeUnauthorized,
		},
		{
			name:       "Forbidden",
			fn:         Forbidden,
			wantStatus: http.StatusForbidden,
			wantError:  CodeForbidden,
		},
		{
			name:       "NotFound",
			fn:         NotFound,
			wantStatus: http.StatusNotFound,
			wantError:  CodeNotFound,
		},
		{
			name:       "Conflict",
			fn:         Conflict,
			wantStatus: http.StatusConflict,
			wantError:  CodeConflict,
		},
		{
			name:       "ValidationError",
			fn:         ValidationError,
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  CodeValidationError,
		},
		{
			name:       "RateLimitExceeded",
			fn:         RateLimitExceeded,
			wantStatus: http.StatusTooManyRequests,
			wantError:  CodeRateLimitExceeded,
		},
		{
			name:       "InternalServerError",
			fn:         InternalServerError,
			wantStatus: http.StatusInternalServerError,
			wantError:  CodeInternalServerError,
		},
		{
			name:       "ServiceUnavailable",
			fn:         ServiceUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantError:  CodeServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := "test message"
			details := "test details"

			got := tt.fn(message, details)

			if got.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", got.Status, tt.wantStatus)
			}
			if got.Error != tt.wantError {
				t.Errorf("Error = %q, want %q", got.Error, tt.wantError)
			}
			if got.Message != message {
				t.Errorf("Message = %q, want %q", got.Message, message)
			}
			if got.Details != details {
				t.Errorf("Details = %q, want %q", got.Details, details)
			}
		})
	}
}

// TestErrorConstructorsWithEmptyDetails verifies that error constructors
// properly handle empty details strings.
func TestErrorConstructorsWithEmptyDetails(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string, string) ErrorResponse
	}{
		{"BadRequest", BadRequest},
		{"Unauthorized", Unauthorized},
		{"Forbidden", Forbidden},
		{"NotFound", NotFound},
		{"Conflict", Conflict},
		{"ValidationError", ValidationError},
		{"RateLimitExceeded", RateLimitExceeded},
		{"InternalServerError", InternalServerError},
		{"ServiceUnavailable", ServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("test message", "")

			if got.Details != "" {
				t.Errorf("Details = %q, want empty string", got.Details)
			}
		})
	}
}

// TestWriteError verifies that WriteError correctly writes a JSON response
// with the proper Content-Type header and status code.
func TestWriteError(t *testing.T) {
	tests := []struct {
		name           string
		errResp        ErrorResponse
		wantStatus     int
		wantError      string
		wantMessage    string
		wantDetails    string
		detailsPresent bool
	}{
		{
			name:           "BadRequest with details",
			errResp:        BadRequest("invalid input", "username is required"),
			wantStatus:     http.StatusBadRequest,
			wantError:      CodeBadRequest,
			wantMessage:    "invalid input",
			wantDetails:    "username is required",
			detailsPresent: true,
		},
		{
			name:           "NotFound without details",
			errResp:        NotFound("session not found", ""),
			wantStatus:     http.StatusNotFound,
			wantError:      CodeNotFound,
			wantMessage:    "session not found",
			wantDetails:    "",
			detailsPresent: false,
		},
		{
			name:           "InternalServerError",
			errResp:        InternalServerError("internal error", ""),
			wantStatus:     http.StatusInternalServerError,
			wantError:      CodeInternalServerError,
			wantMessage:    "internal error",
			wantDetails:    "",
			detailsPresent: false,
		},
		{
			name:           "Conflict with details",
			errResp:        Conflict("session already attached", "only one client allowed"),
			wantStatus:     http.StatusConflict,
			wantError:      CodeConflict,
			wantMessage:    "session already attached",
			wantDetails:    "only one client allowed",
			detailsPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a response recorder
			w := httptest.NewRecorder()

			// Write the error
			err := WriteError(w, tt.errResp)
			if err != nil {
				t.Fatalf("WriteError() error = %v, want nil", err)
			}

			// Check status code
			if w.Code != tt.wantStatus {
				t.Errorf("HTTP status = %d, want %d", w.Code, tt.wantStatus)
			}

			// Check Content-Type header
			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
			}

			// Decode and verify response body
			var got map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatalf("Failed to decode response body: %v", err)
			}

			// Verify status field
			if status, ok := got["status"].(float64); !ok || int(status) != tt.wantStatus {
				t.Errorf("Response status = %v, want %d", got["status"], tt.wantStatus)
			}

			// Verify error field
			if errCode, ok := got["error"].(string); !ok || errCode != tt.wantError {
				t.Errorf("Response error = %v, want %q", got["error"], tt.wantError)
			}

			// Verify message field
			if message, ok := got["message"].(string); !ok || message != tt.wantMessage {
				t.Errorf("Response message = %v, want %q", got["message"], tt.wantMessage)
			}

			// Verify details field presence/absence
			if tt.detailsPresent {
				if details, ok := got["details"].(string); !ok || details != tt.wantDetails {
					t.Errorf("Response details = %v, want %q", got["details"], tt.wantDetails)
				}
			} else {
				if _, exists := got["details"]; exists {
					t.Errorf("Response should not contain details field, but got %v", got["details"])
				}
			}
		})
	}
}

// TestWriteErrorJSONFormat verifies that the JSON output matches the exact
// format specified in the error handling guide.
func TestWriteErrorJSONFormat(t *testing.T) {
	w := httptest.NewRecorder()
	errResp := BadRequest("test message", "test details")

	err := WriteError(w, errResp)
	if err != nil {
		t.Fatalf("WriteError() error = %v, want nil", err)
	}

	// Decode to verify structure
	var got ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if got.Status != errResp.Status {
		t.Errorf("Status = %d, want %d", got.Status, errResp.Status)
	}
	if got.Error != errResp.Error {
		t.Errorf("Error = %q, want %q", got.Error, errResp.Error)
	}
	if got.Message != errResp.Message {
		t.Errorf("Message = %q, want %q", got.Message, errResp.Message)
	}
	if got.Details != errResp.Details {
		t.Errorf("Details = %q, want %q", got.Details, errResp.Details)
	}
}

// TestErrorCodesMatchConstants verifies that error constructor functions
// use the correct error code constants.
func TestErrorCodesMatchConstants(t *testing.T) {
	tests := []struct {
		name      string
		errResp   ErrorResponse
		wantError string
	}{
		{"BadRequest", BadRequest("msg", ""), CodeBadRequest},
		{"Unauthorized", Unauthorized("msg", ""), CodeUnauthorized},
		{"Forbidden", Forbidden("msg", ""), CodeForbidden},
		{"NotFound", NotFound("msg", ""), CodeNotFound},
		{"Conflict", Conflict("msg", ""), CodeConflict},
		{"ValidationError", ValidationError("msg", ""), CodeValidationError},
		{"RateLimitExceeded", RateLimitExceeded("msg", ""), CodeRateLimitExceeded},
		{"InternalServerError", InternalServerError("msg", ""), CodeInternalServerError},
		{"ServiceUnavailable", ServiceUnavailable("msg", ""), CodeServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errResp.Error != tt.wantError {
				t.Errorf("Error code = %q, want %q", tt.errResp.Error, tt.wantError)
			}
		})
	}
}

// TestSpecialCharactersInMessages verifies that error messages and details
// with special characters are properly encoded in JSON.
func TestSpecialCharactersInMessages(t *testing.T) {
	tests := []struct {
		name    string
		message string
		details string
	}{
		{
			name:    "Quotes",
			message: `message with "quotes"`,
			details: `details with "quotes"`,
		},
		{
			name:    "Newlines",
			message: "message with\nnewline",
			details: "details with\nnewline",
		},
		{
			name:    "Tabs",
			message: "message with\ttab",
			details: "details with\ttab",
		},
		{
			name:    "Backslashes",
			message: `message with \ backslash`,
			details: `details with \ backslash`,
		},
		{
			name:    "Unicode",
			message: "message with 中文 characters",
			details: "details with émojis 🚀",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			errResp := BadRequest(tt.message, tt.details)

			err := WriteError(w, errResp)
			if err != nil {
				t.Fatalf("WriteError() error = %v, want nil", err)
			}

			var got ErrorResponse
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if got.Message != tt.message {
				t.Errorf("Message = %q, want %q", got.Message, tt.message)
			}
			if got.Details != tt.details {
				t.Errorf("Details = %q, want %q", got.Details, tt.details)
			}
		})
	}
}

// TestWriteError_InvalidStatusCode verifies that WriteError handles invalid
// status codes by falling back to 500 Internal Server Error.
func TestWriteError_InvalidStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantStatus int
		wantError  string
	}{
		{
			name:       "status code below 100",
			status:     50,
			wantStatus: http.StatusInternalServerError,
			wantError:  CodeInternalServerError,
		},
		{
			name:       "status code above 599",
			status:     600,
			wantStatus: http.StatusInternalServerError,
			wantError:  CodeInternalServerError,
		},
		{
			name:       "status code zero",
			status:     0,
			wantStatus: http.StatusInternalServerError,
			wantError:  CodeInternalServerError,
		},
		{
			name:       "status code negative",
			status:     -100,
			wantStatus: http.StatusInternalServerError,
			wantError:  CodeInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			errResp := ErrorResponse{
				Status:  tt.status,
				Error:   "custom_error",
				Message: "custom message",
				Details: "custom details",
			}

			err := WriteError(w, errResp)
			if err != nil {
				t.Fatalf("WriteError() error = %v, want nil", err)
			}

			// Should fallback to 500
			if w.Code != tt.wantStatus {
				t.Errorf("HTTP status = %d, want %d", w.Code, tt.wantStatus)
			}

			var got ErrorResponse
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if got.Status != tt.wantStatus {
				t.Errorf("Response status = %d, want %d", got.Status, tt.wantStatus)
			}
			if got.Error != tt.wantError {
				t.Errorf("Response error = %q, want %q", got.Error, tt.wantError)
			}
		})
	}
}

// TestWriteError_ContentTypeAlreadySet verifies that WriteError returns an error
// when Content-Type is already set to a non-JSON value.
func TestWriteError_ContentTypeAlreadySet(t *testing.T) {
	w := httptest.NewRecorder()
	// Set a non-JSON Content-Type first
	w.Header().Set("Content-Type", "text/html")

	errResp := BadRequest("test message", "")

	err := WriteError(w, errResp)
	if err == nil {
		t.Fatal("WriteError() should return error when Content-Type already set to non-JSON")
	}

	if err.Error() != "response already started with Content-Type: text/html" {
		t.Errorf("Error message = %q, unexpected error message", err.Error())
	}
}

// TestWriteError_ContentTypeAlreadyJSON verifies that WriteError does not error
// when Content-Type is already set to application/json.
func TestWriteError_ContentTypeAlreadyJSON(t *testing.T) {
	w := httptest.NewRecorder()
	// Set JSON Content-Type first - this should be allowed
	w.Header().Set("Content-Type", "application/json")

	errResp := BadRequest("test message", "")

	err := WriteError(w, errResp)
	if err != nil {
		t.Fatalf("WriteError() should succeed when Content-Type is already JSON, got error = %v", err)
	}
}

// TestErrorResponseStruct verifies ErrorResponse struct fields directly.
func TestErrorResponseStruct(t *testing.T) {
	resp := ErrorResponse{
		Status:  400,
		Error:   "bad_request",
		Message: "invalid input",
		Details: "field required",
	}

	if resp.Status != 400 {
		t.Errorf("Status = %d, want 400", resp.Status)
	}
	if resp.Error != "bad_request" {
		t.Errorf("Error = %q, want 'bad_request'", resp.Error)
	}
	if resp.Message != "invalid input" {
		t.Errorf("Message = %q, want 'invalid input'", resp.Message)
	}
	if resp.Details != "field required" {
		t.Errorf("Details = %q, want 'field required'", resp.Details)
	}
}

// TestErrorResponseZeroValue verifies ErrorResponse zero value behavior.
func TestErrorResponseZeroValue(t *testing.T) {
	var resp ErrorResponse

	if resp.Status != 0 {
		t.Errorf("Zero Status = %d, want 0", resp.Status)
	}
	if resp.Error != "" {
		t.Errorf("Zero Error = %q, want empty", resp.Error)
	}
	if resp.Message != "" {
		t.Errorf("Zero Message = %q, want empty", resp.Message)
	}
	if resp.Details != "" {
		t.Errorf("Zero Details = %q, want empty", resp.Details)
	}
}

// TestErrorCodes verifies all error code constants are defined correctly.
func TestErrorCodes(t *testing.T) {
	codes := map[string]string{
		"CodeBadRequest":          CodeBadRequest,
		"CodeUnauthorized":        CodeUnauthorized,
		"CodeForbidden":           CodeForbidden,
		"CodeNotFound":            CodeNotFound,
		"CodeConflict":            CodeConflict,
		"CodeValidationError":     CodeValidationError,
		"CodeRateLimitExceeded":   CodeRateLimitExceeded,
		"CodeInternalServerError": CodeInternalServerError,
		"CodeServiceUnavailable":  CodeServiceUnavailable,
	}

	// Verify all codes are non-empty
	for name, code := range codes {
		if code == "" {
			t.Errorf("%s is empty", name)
		}
	}

	// Verify codes are unique
	seen := make(map[string]string)
	for name, code := range codes {
		if prev, exists := seen[code]; exists {
			t.Errorf("Duplicate code %q: %s and %s", code, prev, name)
		}
		seen[code] = name
	}
}

// TestErrorCodesSnakeCase verifies all error codes follow snake_case convention.
func TestErrorCodesSnakeCase(t *testing.T) {
	codes := []string{
		CodeBadRequest,
		CodeUnauthorized,
		CodeForbidden,
		CodeNotFound,
		CodeConflict,
		CodeValidationError,
		CodeRateLimitExceeded,
		CodeInternalServerError,
		CodeServiceUnavailable,
	}

	for _, code := range codes {
		// Snake case should only contain lowercase letters and underscores
		for _, c := range code {
			if !((c >= 'a' && c <= 'z') || c == '_') {
				t.Errorf("Code %q contains invalid character %q (should be snake_case)", code, c)
			}
		}
	}
}
