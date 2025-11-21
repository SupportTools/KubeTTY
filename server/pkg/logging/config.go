package logging

import (
	"os"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// Config holds logging configuration options.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error)
	Level string

	// Format is the output format (text, json)
	Format string

	// TraceFunctions is a comma-separated list of function names to trace
	// Example: "handleLogin,handleRefresh,connectToProject"
	TraceFunctions string
}

// DefaultConfig returns the default logging configuration.
func DefaultConfig() *Config {
	return &Config{
		Level:          "info",
		Format:         "text",
		TraceFunctions: "",
	}
}

// ConfigFromEnv creates a Config from environment variables.
//
// Environment variables:
//   - LOG_LEVEL: debug, info, warn, error (default: info)
//   - LOG_FORMAT: text, json (default: text)
//   - LOG_TRACE_FUNCTIONS: comma-separated function names to trace
func ConfigFromEnv() *Config {
	cfg := DefaultConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.Level = strings.ToLower(level)
	}

	if format := os.Getenv("LOG_FORMAT"); format != "" {
		cfg.Format = strings.ToLower(format)
	}

	if trace := os.Getenv("LOG_TRACE_FUNCTIONS"); trace != "" {
		cfg.TraceFunctions = trace
	}

	return cfg
}

// Apply applies the configuration to the global logrus logger.
func (c *Config) Apply() error {
	// Set log level
	level, err := log.ParseLevel(c.Level)
	if err != nil {
		// Log warning about invalid level before falling back to info
		log.WithFields(log.Fields{
			"invalid_level": c.Level,
			"fallback":      "info",
		}).Warn("Invalid log level specified, falling back to info")
		level = log.InfoLevel
	}
	log.SetLevel(level)

	// Set formatter
	switch c.Format {
	case "json":
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	default:
		log.SetFormatter(&log.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	// Store trace functions in package-level variable
	setTraceFunctions(c.TraceFunctions)

	return nil
}

// traceFunctions holds the set of function names to trace.
var (
	traceFunctions   = make(map[string]bool)
	traceFunctionsMu sync.RWMutex
)

// setTraceFunctions parses and stores trace function names.
func setTraceFunctions(funcs string) {
	traceFunctionsMu.Lock()
	defer traceFunctionsMu.Unlock()

	traceFunctions = make(map[string]bool)
	if funcs == "" {
		return
	}
	for _, fn := range strings.Split(funcs, ",") {
		fn = strings.TrimSpace(fn)
		if fn != "" {
			traceFunctions[fn] = true
		}
	}
}

// isTraceEnabled checks if tracing is enabled for a function.
func isTraceEnabled(funcName string) bool {
	traceFunctionsMu.RLock()
	defer traceFunctionsMu.RUnlock()

	// If no specific functions are configured, trace nothing
	if len(traceFunctions) == 0 {
		return false
	}
	// Check for wildcard
	if traceFunctions["*"] {
		return true
	}
	return traceFunctions[funcName]
}

// Init initializes the logging system with configuration from environment.
// Call this once at application startup.
func Init() {
	cfg := ConfigFromEnv()
	_ = cfg.Apply()
}
