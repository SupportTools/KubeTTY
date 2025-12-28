package config

import (
	"fmt"
	"os"
	"time"

	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// ProjectConfig holds configuration specific to the project component.
// Project mode is a stateless PTY server - NO database dependency.
type ProjectConfig struct {
	Port           string
	SessionID      string
	DeploymentID   string
	Shell          string
	KubettyUser    string
	KubettyProject string

	// PTY logging configuration for Loki capture
	PTYLogEnabled    bool
	PTYLogMaxLineLen int

	// Terminal output buffering configuration
	OutputBufferSize int // Ring buffer size for PTY output replay (default 8MB)
	PauseBufferSize  int // Per-client flow control buffer size (default 256KB)

	// PTY file logging for Loki integration (via sidecar)
	PTYFileLogEnabled       bool          // Enable file-based PTY logging
	PTYFileLogPath          string        // Path to JSONL log file
	PTYFileLogMaxSize       int64         // Max file size before rotation (bytes)
	PTYFileLogMaxBackups    int           // Number of rotated files to keep
	PTYFileLogBufferSize    int           // Write buffer size (bytes)
	PTYFileLogFlushInterval time.Duration // How often to flush buffer to disk
	PTYFileLogIncludeRaw    bool          // Include base64-encoded raw bytes
}

// LoadProjectConfig reads environment variables and builds a ProjectConfig.
// This is used by the project binary which provides PTY terminal functionality.
// NOTE: Project mode does NOT require database - it's a stateless PTY server.
func LoadProjectConfig() (ProjectConfig, error) {
	sessionID := os.Getenv("SESSION_ID")
	if sessionID == "" {
		return ProjectConfig{}, fmt.Errorf("SESSION_ID is required")
	}

	deploymentID := sharedconfig.GetEnv("DEPLOYMENT_ID", sessionID)

	cfg := ProjectConfig{
		Port:           sharedconfig.GetEnv("PORT", "8080"),
		SessionID:      sessionID,
		DeploymentID:   deploymentID,
		Shell:          sharedconfig.GetEnv("SHELL", "/bin/bash"),
		KubettyUser:    sharedconfig.GetEnv("KUBETTY_USER", os.Getenv("USER")),
		KubettyProject: sharedconfig.GetEnv("KUBETTY_PROJECT", deploymentID),

		// PTY logging for Loki capture (default: disabled)
		PTYLogEnabled:    sharedconfig.GetEnvBool("PTY_LOG_ENABLED", false),
		PTYLogMaxLineLen: sharedconfig.GetEnvInt("PTY_LOG_MAX_LINE", 4096),

		// Terminal output buffering (default: 8MB output, 256KB pause buffer)
		OutputBufferSize: sharedconfig.GetEnvInt("OUTPUT_BUFFER_SIZE", 8*1024*1024),
		PauseBufferSize:  sharedconfig.GetEnvInt("PAUSE_BUFFER_SIZE", 256*1024),

		// PTY file logging for Loki integration (via sidecar)
		PTYFileLogEnabled:       sharedconfig.GetEnvBool("PTY_FILE_LOG_ENABLED", false),
		PTYFileLogPath:          sharedconfig.GetEnv("PTY_FILE_LOG_PATH", "/var/log/kubetty/pty-session.jsonl"),
		PTYFileLogMaxSize:       int64(sharedconfig.GetEnvInt("PTY_FILE_LOG_MAX_SIZE", 104857600)), // 100MB
		PTYFileLogMaxBackups:    sharedconfig.GetEnvInt("PTY_FILE_LOG_MAX_BACKUPS", 3),
		PTYFileLogBufferSize:    sharedconfig.GetEnvInt("PTY_FILE_LOG_BUFFER_SIZE", 65536), // 64KB
		PTYFileLogFlushInterval: sharedconfig.GetEnvDuration("PTY_FILE_LOG_FLUSH_INTERVAL", 5*time.Second),
		PTYFileLogIncludeRaw:    sharedconfig.GetEnvBool("PTY_FILE_LOG_INCLUDE_RAW", true),
	}

	return cfg, nil
}
