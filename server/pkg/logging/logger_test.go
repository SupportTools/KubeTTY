package logging

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger("handlers", "auth")

	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}

	if logger.Component() != "handlers" {
		t.Errorf("Component() = %q, want %q", logger.Component(), "handlers")
	}

	if logger.Domain() != "auth" {
		t.Errorf("Domain() = %q, want %q", logger.Domain(), "auth")
	}
}

func TestLoggerLogLevels(t *testing.T) {
	// Use test hook to capture log entries
	hook := test.NewGlobal()
	defer hook.Reset()

	// Set to debug to capture all levels
	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	logger := NewLogger("test", "levels")

	tests := []struct {
		name     string
		logFunc  func(string, ...interface{})
		level    log.Level
		msg      string
		fields   []interface{}
		wantKeys []string
	}{
		{
			name:     "Debug",
			logFunc:  logger.Debug,
			level:    log.DebugLevel,
			msg:      "debug message",
			fields:   []interface{}{"key1", "value1"},
			wantKeys: []string{"key1"},
		},
		{
			name:     "Info",
			logFunc:  logger.Info,
			level:    log.InfoLevel,
			msg:      "info message",
			fields:   []interface{}{"key2", "value2"},
			wantKeys: []string{"key2"},
		},
		{
			name:     "Warn",
			logFunc:  logger.Warn,
			level:    log.WarnLevel,
			msg:      "warn message",
			fields:   []interface{}{"key3", "value3"},
			wantKeys: []string{"key3"},
		},
		{
			name:     "Error",
			logFunc:  logger.Error,
			level:    log.ErrorLevel,
			msg:      "error message",
			fields:   []interface{}{"key4", "value4"},
			wantKeys: []string{"key4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook.Reset()

			tt.logFunc(tt.msg, tt.fields...)

			if len(hook.Entries) != 1 {
				t.Fatalf("Expected 1 log entry, got %d", len(hook.Entries))
			}

			entry := hook.LastEntry()

			if entry.Level != tt.level {
				t.Errorf("Level = %v, want %v", entry.Level, tt.level)
			}

			if entry.Message != tt.msg {
				t.Errorf("Message = %q, want %q", entry.Message, tt.msg)
			}

			// Check component and domain fields
			if entry.Data[FieldComponent] != "test" {
				t.Errorf("Component field = %v, want %q", entry.Data[FieldComponent], "test")
			}

			if entry.Data[FieldDomain] != "levels" {
				t.Errorf("Domain field = %v, want %q", entry.Data[FieldDomain], "levels")
			}

			// Check custom fields
			for _, key := range tt.wantKeys {
				if _, ok := entry.Data[key]; !ok {
					t.Errorf("Missing field %q", key)
				}
			}
		})
	}
}

func TestLoggerWithFields(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("handlers", "auth")

	logger.WithFields(log.Fields{
		FieldSessionUUID: "test-uuid",
		FieldClientID:    "client-123",
	}).Info("Connection established")

	if len(hook.Entries) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(hook.Entries))
	}

	entry := hook.LastEntry()

	if entry.Data[FieldSessionUUID] != "test-uuid" {
		t.Errorf("SessionUUID = %v, want %q", entry.Data[FieldSessionUUID], "test-uuid")
	}

	if entry.Data[FieldClientID] != "client-123" {
		t.Errorf("ClientID = %v, want %q", entry.Data[FieldClientID], "client-123")
	}

	// Component and domain should still be present
	if entry.Data[FieldComponent] != "handlers" {
		t.Errorf("Component = %v, want %q", entry.Data[FieldComponent], "handlers")
	}
}

func TestLoggerWithField(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("gateway", "relay")

	logger.WithField(FieldProjectID, "project-abc").Info("Project connected")

	entry := hook.LastEntry()

	if entry.Data[FieldProjectID] != "project-abc" {
		t.Errorf("ProjectID = %v, want %q", entry.Data[FieldProjectID], "project-abc")
	}
}

func TestLoggerWithError(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("handlers", "auth")

	testErr := &testError{msg: "connection refused"}
	logger.WithError(testErr).Error("Failed to connect")

	entry := hook.LastEntry()

	if entry.Data[log.ErrorKey] != testErr {
		t.Errorf("Error = %v, want %v", entry.Data[log.ErrorKey], testErr)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestLoggerWithContext(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("handlers", "auth")

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-12345")

	ctxLogger := logger.WithContext(ctx)

	// Verify it's a new logger instance
	if ctxLogger == logger {
		t.Error("WithContext should return a new Logger instance")
	}

	// Component and domain should be preserved
	if ctxLogger.Component() != "handlers" {
		t.Errorf("Component = %q, want %q", ctxLogger.Component(), "handlers")
	}

	if ctxLogger.Domain() != "auth" {
		t.Errorf("Domain = %q, want %q", ctxLogger.Domain(), "auth")
	}
}

func TestLoggerFormatMethods(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	logger := NewLogger("test", "format")

	logger.Debugf("debug %s %d", "test", 1)
	logger.Infof("info %s %d", "test", 2)
	logger.Warnf("warn %s %d", "test", 3)
	logger.Errorf("error %s %d", "test", 4)

	if len(hook.Entries) != 4 {
		t.Fatalf("Expected 4 log entries, got %d", len(hook.Entries))
	}

	expected := []struct {
		level log.Level
		msg   string
	}{
		{log.DebugLevel, "debug test 1"},
		{log.InfoLevel, "info test 2"},
		{log.WarnLevel, "warn test 3"},
		{log.ErrorLevel, "error test 4"},
	}

	for i, exp := range expected {
		if hook.Entries[i].Level != exp.level {
			t.Errorf("Entry %d: Level = %v, want %v", i, hook.Entries[i].Level, exp.level)
		}
		if hook.Entries[i].Message != exp.msg {
			t.Errorf("Entry %d: Message = %q, want %q", i, hook.Entries[i].Message, exp.msg)
		}
	}
}

func TestShouldTrace(t *testing.T) {
	logger := NewLogger("test", "trace")

	// Initially, no functions should be traced
	if logger.ShouldTrace("handleLogin") {
		t.Error("ShouldTrace should return false when no trace functions configured")
	}

	// Configure trace functions
	setTraceFunctions("handleLogin,handleRefresh")

	if !logger.ShouldTrace("handleLogin") {
		t.Error("ShouldTrace should return true for configured function")
	}

	if !logger.ShouldTrace("handleRefresh") {
		t.Error("ShouldTrace should return true for configured function")
	}

	if logger.ShouldTrace("handleLogout") {
		t.Error("ShouldTrace should return false for unconfigured function")
	}

	// Test wildcard
	setTraceFunctions("*")

	if !logger.ShouldTrace("anyFunction") {
		t.Error("ShouldTrace should return true when wildcard is configured")
	}

	// Reset
	setTraceFunctions("")
}

func TestLoggerKeyValuePairs(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("test", "kvpairs")

	// Test multiple key-value pairs
	logger.Info("test message",
		"key1", "value1",
		"key2", 42,
		"key3", true,
	)

	entry := hook.LastEntry()

	if entry.Data["key1"] != "value1" {
		t.Errorf("key1 = %v, want %q", entry.Data["key1"], "value1")
	}

	if entry.Data["key2"] != 42 {
		t.Errorf("key2 = %v, want %d", entry.Data["key2"], 42)
	}

	if entry.Data["key3"] != true {
		t.Errorf("key3 = %v, want %v", entry.Data["key3"], true)
	}
}

func TestLoggerOddKeyValuePairs(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	logger := NewLogger("test", "oddpairs")

	// Test odd number of key-value pairs (last value ignored)
	logger.Info("test message",
		"key1", "value1",
		"orphan",
	)

	entry := hook.LastEntry()

	if entry.Data["key1"] != "value1" {
		t.Errorf("key1 = %v, want %q", entry.Data["key1"], "value1")
	}

	// "orphan" should not be present as a key
	if _, ok := entry.Data["orphan"]; ok {
		t.Error("Odd key should be ignored")
	}
}

func TestDefaultConfig(t *testing.T) {
	// Test default config
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("Default Level = %q, want %q", cfg.Level, "info")
	}

	if cfg.Format != "text" {
		t.Errorf("Default Format = %q, want %q", cfg.Format, "text")
	}

	if cfg.TraceFunctions != "" {
		t.Errorf("Default TraceFunctions = %q, want empty", cfg.TraceFunctions)
	}
}

func TestConfigApply(t *testing.T) {
	// Save original settings
	originalLevel := log.GetLevel()
	originalFormatter := log.StandardLogger().Formatter
	defer func() {
		log.SetLevel(originalLevel)
		log.SetFormatter(originalFormatter)
	}()

	tests := []struct {
		name       string
		config     *Config
		wantLevel  log.Level
		wantFormat string
	}{
		{
			name: "debug level text format",
			config: &Config{
				Level:  "debug",
				Format: "text",
			},
			wantLevel:  log.DebugLevel,
			wantFormat: "*logrus.TextFormatter",
		},
		{
			name: "warn level json format",
			config: &Config{
				Level:  "warn",
				Format: "json",
			},
			wantLevel:  log.WarnLevel,
			wantFormat: "*logrus.JSONFormatter",
		},
		{
			name: "invalid level defaults to info",
			config: &Config{
				Level:  "invalid",
				Format: "text",
			},
			wantLevel:  log.InfoLevel,
			wantFormat: "*logrus.TextFormatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Apply()
			if err != nil {
				t.Errorf("Apply() error = %v", err)
			}

			if log.GetLevel() != tt.wantLevel {
				t.Errorf("Level = %v, want %v", log.GetLevel(), tt.wantLevel)
			}

			// Verify formatter type via type assertion
			switch tt.wantFormat {
			case "*logrus.TextFormatter":
				if _, ok := log.StandardLogger().Formatter.(*log.TextFormatter); !ok {
					t.Errorf("Formatter is not TextFormatter")
				}
			case "*logrus.JSONFormatter":
				if _, ok := log.StandardLogger().Formatter.(*log.JSONFormatter); !ok {
					t.Errorf("Formatter is not JSONFormatter")
				}
			}
		})
	}
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	// Test WithRequestID and RequestIDFromContext
	ctx = WithRequestID(ctx, "req-abc123")

	if got := RequestIDFromContext(ctx); got != "req-abc123" {
		t.Errorf("RequestIDFromContext = %q, want %q", got, "req-abc123")
	}

	// Test empty context
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("RequestIDFromContext(empty) = %q, want empty", got)
	}

	// Test WithLogger and LoggerFromContext
	logger := NewLogger("test", "context")
	ctx = WithLogger(ctx, logger)

	got := LoggerFromContext(ctx)
	if got != logger {
		t.Error("LoggerFromContext did not return the same logger")
	}

	// Test LoggerFromContext with empty context
	if got := LoggerFromContext(context.Background()); got != nil {
		t.Error("LoggerFromContext(empty) should return nil")
	}

	// Test LoggerFromContextOrDefault
	defaultLogger := LoggerFromContextOrDefault(context.Background(), "default", "test")
	if defaultLogger == nil {
		t.Error("LoggerFromContextOrDefault should return a logger")
	}
	if defaultLogger.Component() != "default" {
		t.Errorf("Default logger Component = %q, want %q", defaultLogger.Component(), "default")
	}
}

func TestLogOutput(t *testing.T) {
	// Capture actual log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	log.SetFormatter(&log.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})

	logger := NewLogger("test", "output")
	logger.Info("test message", "custom_key", "custom_value")

	output := buf.String()

	// Verify key fields are present in output
	if !strings.Contains(output, "component=test") {
		t.Errorf("Output missing component field: %s", output)
	}

	if !strings.Contains(output, "domain=output") {
		t.Errorf("Output missing domain field: %s", output)
	}

	if !strings.Contains(output, "custom_key=custom_value") {
		t.Errorf("Output missing custom field: %s", output)
	}

	if !strings.Contains(output, "test message") {
		t.Errorf("Output missing message: %s", output)
	}
}

func TestFieldConstants(t *testing.T) {
	// Verify field constants are defined correctly
	fields := map[string]string{
		"FieldSessionUUID": FieldSessionUUID,
		"FieldClientID":    FieldClientID,
		"FieldConnID":      FieldConnID,
		"FieldTabID":       FieldTabID,
		"FieldProjectID":   FieldProjectID,
		"FieldComponent":   FieldComponent,
		"FieldDomain":      FieldDomain,
		"FieldMethod":      FieldMethod,
		"FieldPath":        FieldPath,
		"FieldStatus":      FieldStatus,
		"FieldStatusCode":  FieldStatusCode,
		"FieldDuration":    FieldDuration,
		"FieldRemoteAddr":  FieldRemoteAddr,
		"FieldRequestID":   FieldRequestID,
		"FieldUserID":      FieldUserID,
		"FieldUsername":    FieldUsername,
		"FieldError":       FieldError,
		"FieldErrorID":     FieldErrorID,
	}

	for name, value := range fields {
		if value == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	// Save original environment
	origLevel := os.Getenv("LOG_LEVEL")
	origFormat := os.Getenv("LOG_FORMAT")
	origTrace := os.Getenv("LOG_TRACE_FUNCTIONS")
	defer func() {
		os.Setenv("LOG_LEVEL", origLevel)
		os.Setenv("LOG_FORMAT", origFormat)
		os.Setenv("LOG_TRACE_FUNCTIONS", origTrace)
	}()

	tests := []struct {
		name       string
		envLevel   string
		envFormat  string
		envTrace   string
		wantLevel  string
		wantFormat string
		wantTrace  string
	}{
		{
			name:       "default values when env not set",
			envLevel:   "",
			envFormat:  "",
			envTrace:   "",
			wantLevel:  "info",
			wantFormat: "text",
			wantTrace:  "",
		},
		{
			name:       "custom level from env",
			envLevel:   "DEBUG",
			envFormat:  "",
			envTrace:   "",
			wantLevel:  "debug",
			wantFormat: "text",
			wantTrace:  "",
		},
		{
			name:       "custom format from env",
			envLevel:   "",
			envFormat:  "JSON",
			envTrace:   "",
			wantLevel:  "info",
			wantFormat: "json",
			wantTrace:  "",
		},
		{
			name:       "all custom values",
			envLevel:   "WARN",
			envFormat:  "json",
			envTrace:   "func1,func2",
			wantLevel:  "warn",
			wantFormat: "json",
			wantTrace:  "func1,func2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv("LOG_LEVEL")
			os.Unsetenv("LOG_FORMAT")
			os.Unsetenv("LOG_TRACE_FUNCTIONS")

			// Set test values
			if tt.envLevel != "" {
				os.Setenv("LOG_LEVEL", tt.envLevel)
			}
			if tt.envFormat != "" {
				os.Setenv("LOG_FORMAT", tt.envFormat)
			}
			if tt.envTrace != "" {
				os.Setenv("LOG_TRACE_FUNCTIONS", tt.envTrace)
			}

			cfg := ConfigFromEnv()

			if cfg.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q", cfg.Level, tt.wantLevel)
			}
			if cfg.Format != tt.wantFormat {
				t.Errorf("Format = %q, want %q", cfg.Format, tt.wantFormat)
			}
			if cfg.TraceFunctions != tt.wantTrace {
				t.Errorf("TraceFunctions = %q, want %q", cfg.TraceFunctions, tt.wantTrace)
			}
		})
	}
}

func TestInit(t *testing.T) {
	// Save original settings
	origLevel := log.GetLevel()
	origFormatter := log.StandardLogger().Formatter
	origEnvLevel := os.Getenv("LOG_LEVEL")
	origEnvFormat := os.Getenv("LOG_FORMAT")
	origEnvTrace := os.Getenv("LOG_TRACE_FUNCTIONS")

	defer func() {
		log.SetLevel(origLevel)
		log.SetFormatter(origFormatter)
		os.Setenv("LOG_LEVEL", origEnvLevel)
		os.Setenv("LOG_FORMAT", origEnvFormat)
		os.Setenv("LOG_TRACE_FUNCTIONS", origEnvTrace)
		setTraceFunctions("")
	}()

	// Set up environment
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_FORMAT", "json")
	os.Setenv("LOG_TRACE_FUNCTIONS", "testFunc")

	// Call Init
	Init()

	// Verify settings were applied
	if log.GetLevel() != log.DebugLevel {
		t.Errorf("Init did not set log level to debug, got %v", log.GetLevel())
	}

	if _, ok := log.StandardLogger().Formatter.(*log.JSONFormatter); !ok {
		t.Error("Init did not set JSON formatter")
	}

	// Verify trace functions
	logger := NewLogger("test", "init")
	if !logger.ShouldTrace("testFunc") {
		t.Error("Init did not configure trace functions")
	}
}

func TestConcurrentTraceAccess(t *testing.T) {
	// Test thread safety of trace functions
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			setTraceFunctions("func1,func2,func3")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		logger := NewLogger("test", "concurrent")
		for i := 0; i < 100; i++ {
			_ = logger.ShouldTrace("func1")
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// If we get here without a race condition, the test passes
}
