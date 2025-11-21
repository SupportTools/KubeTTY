package config

import (
	"fmt"
	"os"

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
	}

	return cfg, nil
}
