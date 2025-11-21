// Package logging provides structured logging utilities for KubeTTY.
//
// This package wraps logrus to provide component-based logging with
// standardized field names and conditional trace logging support.
package logging

// Standard field names for structured logging.
// Use these constants to ensure consistent field names across the codebase.
const (
	// Session and connection fields
	FieldSessionUUID = "session_uuid"
	FieldClientID    = "client_id"
	FieldConnID      = "conn_id"
	FieldTabID       = "tab_id"

	// Project and component fields
	FieldProjectID = "project_id"
	FieldComponent = "component"
	FieldDomain    = "domain"

	// HTTP request fields
	FieldMethod     = "method"
	FieldPath       = "path"
	FieldStatus     = "status"
	FieldStatusCode = "status_code"
	FieldDuration   = "duration"
	FieldRemoteAddr = "remote_addr"
	FieldRequestID  = "request_id"

	// User and auth fields
	FieldUserID   = "user_id"
	FieldUsername = "username"

	// Error fields
	FieldError   = "error"
	FieldErrorID = "error_id"

	// Health check fields
	FieldFailureCount = "failure_count"
	FieldOldStatus    = "old_status"
	FieldNewStatus    = "new_status"

	// Config fields
	FieldKey     = "key"
	FieldValue   = "value"
	FieldDefault = "default"

	// Retry and attempt fields
	FieldAttempt    = "attempt"
	FieldMaxRetries = "max_retries"
)
