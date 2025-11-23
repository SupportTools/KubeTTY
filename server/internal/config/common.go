package config

import (
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
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

	// Note: SESSION_ID validation moved to project config (not required for gateway mode)
	// CNPG validation moved to gateway config (not required for project mode)

	return cfg, nil
}

// ConnString builds the pgx connection string using the shared config builder.
//
// DEPRECATED: Use ConnConfig() instead for better security and type safety.
// This method is maintained for backward compatibility but uses string concatenation
// which could lead to injection vulnerabilities if parameters come from untrusted sources.
func (c CommonConfig) ConnString() string {
	return sharedconfig.BuildPostgresConnString(
		c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}

// ConnConfig creates a secure, injection-proof PostgreSQL connection configuration
// using the shared BuildPostgresConfig function. This is the recommended method
// for creating database connections.
//
// Returns an error if the configuration parameters are invalid (e.g., invalid port range).
func (c CommonConfig) ConnConfig() (*pgxpool.Config, error) {
	return sharedconfig.BuildPostgresConfig(
		c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}
