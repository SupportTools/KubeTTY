// Package config provides shared configuration utilities for KubeTTY components.
//
// This package centralizes common configuration patterns used across gateway,
// project, and legacy KubeTTY binaries. It eliminates duplication by providing
// reusable helpers for environment variable parsing, connection string building,
// and configuration validation.
//
// Key features:
//   - Type-safe environment variable parsing (string, int, duration, bool)
//   - PostgreSQL connection string building
//   - Default value handling with fallbacks
//   - Consistent error handling and validation
//
// All environment variable parsing functions follow the pattern:
//   - Accept a key and default value
//   - Return the parsed value or default on error
//   - Log warnings for invalid values
//
// Example usage:
//
//	port := config.GetEnvInt("SERVER_PORT", 8080)
//	timeout := config.GetEnvDuration("REQUEST_TIMEOUT", 30*time.Second)
//	debug := config.GetEnvBool("DEBUG_MODE", false)
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// GetEnv retrieves a string environment variable with a default fallback.
// If the environment variable is not set or empty, the default value is returned.
//
// Example:
//
//	appName := config.GetEnv("APP_NAME", "kubetty")
//	// Returns "kubetty" if APP_NAME is not set
func GetEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

// GetEnvInt retrieves an integer environment variable with a default fallback.
// If the environment variable is not set, empty, or cannot be parsed as an integer,
// the default value is returned and a warning is logged.
//
// Example:
//
//	maxConns := config.GetEnvInt("MAX_CONNECTIONS", 100)
//	// Returns 100 if MAX_CONNECTIONS is not set or invalid
func GetEnvInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		log.WithFields(log.Fields{"key": key, "value": val, "default": def}).
			Warn("Invalid integer environment variable, using default")
		return def
	}
	return parsed
}

// GetEnvDuration retrieves a duration environment variable with a default fallback.
// The environment variable should be a string parseable by time.ParseDuration
// (e.g., "30s", "5m", "2h").
//
// If the environment variable is not set, empty, or cannot be parsed as a duration,
// the default value is returned and a warning is logged.
//
// Example:
//
//	timeout := config.GetEnvDuration("REQUEST_TIMEOUT", 30*time.Second)
//	// Returns 30s if REQUEST_TIMEOUT is not set or invalid
func GetEnvDuration(key string, def time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		log.WithFields(log.Fields{"key": key, "value": val, "default": def}).
			Warn("Invalid duration environment variable, using default")
		return def
	}
	return parsed
}

// GetEnvBool retrieves a boolean environment variable with a default fallback.
// The environment variable should be one of: "true", "false", "1", "0", "yes", "no"
// (case-insensitive).
//
// If the environment variable is not set, empty, or cannot be parsed as a boolean,
// the default value is returned and a warning is logged.
//
// Example:
//
//	debug := config.GetEnvBool("DEBUG_MODE", false)
//	// Returns false if DEBUG_MODE is not set or invalid
func GetEnvBool(key string, def bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		log.WithFields(log.Fields{"key": key, "value": val, "default": def}).
			Warn("Invalid boolean environment variable, using default")
		return def
	}
	return parsed
}

// BuildPostgresConnString constructs a PostgreSQL connection string from individual
// components. This is used to connect to CloudNativePG (CNPG) databases.
//
// The connection string uses sslmode=disable for compatibility with CNPG internal
// connections. For external connections or production deployments, consider using
// sslmode=require or sslmode=verify-full.
//
// Example:
//
//	connString := config.BuildPostgresConnString(
//	    "postgres-primary.default.svc.cluster.local",
//	    "5432",
//	    "kubetty",
//	    "kubetty_user",
//	    "secret_password",
//	)
//	// Returns: "host=postgres-primary.default.svc.cluster.local port=5432 dbname=kubetty user=kubetty_user password=secret_password sslmode=disable"
func BuildPostgresConnString(host, port, database, user, password string) string {
	return fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		host, port, database, user, password)
}
