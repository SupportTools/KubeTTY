package logging

import (
	"context"

	log "github.com/sirupsen/logrus"
)

// Logger provides component-scoped structured logging.
//
// Create a Logger for each component/domain combination:
//
//	var authLogger = logging.NewLogger("handlers", "auth")
//	authLogger.Info("User authenticated", "user_id", userID)
//
// SECURITY NOTE: This logger does not automatically sanitize values.
// Never log sensitive data such as passwords, tokens, API keys, or PII.
// Callers are responsible for ensuring sensitive data is not passed to log methods.
type Logger struct {
	component string
	domain    string
	entry     *log.Entry
}

// NewLogger creates a new Logger for the given component and domain.
//
// Parameters:
//   - component: The top-level component (e.g., "handlers", "gateway", "relay")
//   - domain: The specific domain within the component (e.g., "auth", "health", "tabs")
//
// Example:
//
//	var authLogger = logging.NewLogger("handlers", "auth")
//	var relayLogger = logging.NewLogger("gateway", "relay")
func NewLogger(component, domain string) *Logger {
	entry := log.WithFields(log.Fields{
		FieldComponent: component,
		FieldDomain:    domain,
	})

	return &Logger{
		component: component,
		domain:    domain,
		entry:     entry,
	}
}

// WithFields returns a logrus Entry with additional fields.
// This allows chaining with logrus methods.
//
// Example:
//
//	logger.WithFields(log.Fields{
//	    "session_uuid": uuid,
//	    "client_id":    clientID,
//	}).Info("Client connected")
func (l *Logger) WithFields(fields log.Fields) *log.Entry {
	return l.entry.WithFields(fields)
}

// WithField returns a logrus Entry with a single additional field.
func (l *Logger) WithField(key string, value interface{}) *log.Entry {
	return l.entry.WithField(key, value)
}

// WithError returns a logrus Entry with the error field set.
func (l *Logger) WithError(err error) *log.Entry {
	return l.entry.WithError(err)
}

// WithContext returns a new Logger with context attached.
// This enables extracting request IDs or other context values.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		component: l.component,
		domain:    l.domain,
		entry:     l.entry.WithContext(ctx),
	}
}

// ShouldTrace returns true if tracing is enabled for the given function.
// Use this to conditionally log verbose debug information.
//
// Example:
//
//	if logger.ShouldTrace("handleLogin") {
//	    logger.Debug("Entering handleLogin", "request_id", reqID)
//	}
func (l *Logger) ShouldTrace(funcName string) bool {
	return isTraceEnabled(funcName)
}

// Debug logs a message at Debug level with optional key-value pairs.
//
// Example:
//
//	logger.Debug("Processing request", "request_id", reqID, "path", path)
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.log(log.DebugLevel, msg, keysAndValues...)
}

// Info logs a message at Info level with optional key-value pairs.
//
// Example:
//
//	logger.Info("User authenticated", "user_id", userID)
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.log(log.InfoLevel, msg, keysAndValues...)
}

// Warn logs a message at Warn level with optional key-value pairs.
//
// Example:
//
//	logger.Warn("Rate limit approaching", "current", current, "limit", limit)
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.log(log.WarnLevel, msg, keysAndValues...)
}

// Error logs a message at Error level with optional key-value pairs.
//
// Example:
//
//	logger.Error("Failed to connect", "error", err.Error(), "host", host)
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.log(log.ErrorLevel, msg, keysAndValues...)
}

// log is the internal logging method that converts key-value pairs to fields.
func (l *Logger) log(level log.Level, msg string, keysAndValues ...interface{}) {
	entry := l.entry

	// Convert key-value pairs to fields
	if len(keysAndValues) > 0 {
		fields := make(log.Fields)
		for i := 0; i < len(keysAndValues)-1; i += 2 {
			key, ok := keysAndValues[i].(string)
			if !ok {
				continue
			}
			fields[key] = keysAndValues[i+1]
		}
		entry = entry.WithFields(fields)
	}

	entry.Log(level, msg)
}

// Debugf logs a formatted message at Debug level.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.entry.Debugf(format, args...)
}

// Infof logs a formatted message at Info level.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.entry.Infof(format, args...)
}

// Warnf logs a formatted message at Warn level.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.entry.Warnf(format, args...)
}

// Errorf logs a formatted message at Error level.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.entry.Errorf(format, args...)
}

// Component returns the logger's component name.
func (l *Logger) Component() string {
	return l.component
}

// Domain returns the logger's domain name.
func (l *Logger) Domain() string {
	return l.domain
}
