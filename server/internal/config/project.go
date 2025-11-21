package config

import (
	"os"

	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// ProjectConfig holds configuration specific to the project component.
type ProjectConfig struct {
	CommonConfig
	Shell          string
	KubettyUser    string
	KubettyProject string
}

// LoadProjectConfig reads environment variables and builds a ProjectConfig.
// This is used by the project binary which provides PTY terminal functionality.
func LoadProjectConfig() (ProjectConfig, error) {
	common, err := loadCommonConfig()
	if err != nil {
		return ProjectConfig{}, err
	}

	cfg := ProjectConfig{
		CommonConfig:   common,
		Shell:          sharedconfig.GetEnv("SHELL", "/bin/bash"),
		KubettyUser:    sharedconfig.GetEnv("KUBETTY_USER", os.Getenv("USER")),
		KubettyProject: sharedconfig.GetEnv("KUBETTY_PROJECT", common.DeploymentID),
	}

	return cfg, nil
}
