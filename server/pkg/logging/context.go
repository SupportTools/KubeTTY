package logging

import (
	"context"
)

// contextKey is the type for context keys used by this package.
type contextKey string

const (
	// requestIDKey is the context key for request ID.
	requestIDKey contextKey = "request_id"

	// loggerKey is the context key for a Logger instance.
	loggerKey contextKey = "logger"
)

// WithRequestID returns a new context with the request ID set.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request ID from context.
// Returns empty string if not found.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithLogger returns a new context with the Logger set.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext extracts a Logger from context.
// Returns nil if not found.
func LoggerFromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerKey).(*Logger); ok {
		return logger
	}
	return nil
}

// LoggerFromContextOrDefault extracts a Logger from context,
// or returns a default logger with the given component and domain.
func LoggerFromContextOrDefault(ctx context.Context, component, domain string) *Logger {
	if logger := LoggerFromContext(ctx); logger != nil {
		return logger
	}
	return NewLogger(component, domain)
}
