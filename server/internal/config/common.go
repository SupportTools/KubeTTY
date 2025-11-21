package config

import (
	"fmt"
	"os"

	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// CommonConfig holds configuration shared by both gateway and project components.
type CommonConfig struct {
	Port              string
	SessionID         string
	DeploymentID      string
	CNPGHost          string
	CNPGPort          string
	CNPGDatabase      string
	CNPGUser          string
	CNPGPassword      string
	LogRetentionHours int
	LogMaxPerSession  int
}

// loadCommonConfig reads environment variables common to both components.
func loadCommonConfig() (CommonConfig, error) {
	cfg := CommonConfig{
		Port:              sharedconfig.GetEnv("PORT", "8080"),
		SessionID:         os.Getenv("SESSION_ID"),
		CNPGHost:          os.Getenv("CNPG_HOST"),
		CNPGPort:          sharedconfig.GetEnv("CNPG_PORT", "5432"),
		CNPGDatabase:      os.Getenv("CNPG_DATABASE"),
		CNPGUser:          os.Getenv("CNPG_USER"),
		CNPGPassword:      os.Getenv("CNPG_PASSWORD"),
		LogRetentionHours: sharedconfig.GetEnvInt("SESSION_LOG_RETENTION_HOURS", 24*30),
		LogMaxPerSession:  sharedconfig.GetEnvInt("SESSION_LOG_MAX_ENTRIES", 5000),
	}
	cfg.DeploymentID = sharedconfig.GetEnv("DEPLOYMENT_ID", cfg.SessionID)

	// Validate required fields
	if cfg.SessionID == "" {
		return cfg, fmt.Errorf("SESSION_ID is required")
	}
	if cfg.CNPGHost == "" || cfg.CNPGDatabase == "" || cfg.CNPGUser == "" || cfg.CNPGPassword == "" {
		return cfg, fmt.Errorf("CNPG_* env vars are required")
	}

	return cfg, nil
}

// ConnString builds the pgx connection string using the shared config builder.
func (c CommonConfig) ConnString() string {
	return sharedconfig.BuildPostgresConnString(
		c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}
