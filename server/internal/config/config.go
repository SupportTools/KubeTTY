package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config captures runtime settings passed via environment variables.
type Config struct {
	Port              string
	SessionID         string
	DeploymentID      string
	Shell             string
	CNPGHost          string
	CNPGPort          string
	CNPGDatabase      string
	CNPGUser          string
	CNPGPassword      string
	LogRetentionHours int
	LogMaxPerSession  int
}

// Load reads environment variables and builds a Config.
func Load() (Config, error) {
	cfg := Config{
		Port:      getEnv("PORT", "8080"),
		SessionID: os.Getenv("SESSION_ID"),
		Shell:     getEnv("SHELL", "/bin/bash"),

		CNPGHost:          os.Getenv("CNPG_HOST"),
		CNPGPort:          getEnv("CNPG_PORT", "5432"),
		CNPGDatabase:      os.Getenv("CNPG_DATABASE"),
		CNPGUser:          os.Getenv("CNPG_USER"),
		CNPGPassword:      os.Getenv("CNPG_PASSWORD"),
		LogRetentionHours: getEnvInt("SESSION_LOG_RETENTION_HOURS", 24*30),
		LogMaxPerSession:  getEnvInt("SESSION_LOG_MAX_ENTRIES", 5000),
	}
	cfg.DeploymentID = getEnv("DEPLOYMENT_ID", cfg.SessionID)

	if cfg.SessionID == "" {
		return cfg, fmt.Errorf("SESSION_ID is required")
	}
	if cfg.CNPGHost == "" || cfg.CNPGDatabase == "" || cfg.CNPGUser == "" || cfg.CNPGPassword == "" {
		return cfg, fmt.Errorf("CNPG_* env vars are required")
	}
	return cfg, nil
}

// ConnString builds the pgx connection string.
func (c Config) ConnString() string {
	return fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

func getEnvInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	if parsed, err := strconv.Atoi(val); err == nil {
		return parsed
	}
	return def
}
