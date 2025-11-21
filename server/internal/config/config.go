package config

import (
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// Config captures runtime settings passed via environment variables.
type Config struct {
	Port               string
	SessionID          string
	DeploymentID       string
	Shell              string
	CNPGHost           string
	CNPGPort           string
	CNPGDatabase       string
	CNPGUser           string
	CNPGPassword       string
	LogRetentionHours  int
	LogMaxPerSession   int
	ProjectCatalogPath string
	ProjectCatalog     gatewayconfig.Catalog
	KubettyUser        string
	KubettyProject     string
	AuthMode           string
	AuthJWTSecret      string
	AuthAccessTTL      time.Duration
	AuthRefreshTTL     time.Duration
	AuthIssuer         string
	AuthCookieDomain   string
	AuthCookieSecure   bool
}

// Load reads environment variables and builds a Config.
func Load() (Config, error) {
	cfg := Config{
		Port:      sharedconfig.GetEnv("PORT", "8080"),
		SessionID: os.Getenv("SESSION_ID"),
		Shell:     sharedconfig.GetEnv("SHELL", "/bin/bash"),

		CNPGHost:           os.Getenv("CNPG_HOST"),
		CNPGPort:           sharedconfig.GetEnv("CNPG_PORT", "5432"),
		CNPGDatabase:       os.Getenv("CNPG_DATABASE"),
		CNPGUser:           os.Getenv("CNPG_USER"),
		CNPGPassword:       os.Getenv("CNPG_PASSWORD"),
		LogRetentionHours:  sharedconfig.GetEnvInt("SESSION_LOG_RETENTION_HOURS", 24*30),
		LogMaxPerSession:   sharedconfig.GetEnvInt("SESSION_LOG_MAX_ENTRIES", 5000),
		ProjectCatalogPath: os.Getenv("PROJECT_CATALOG_PATH"),
		AuthMode:           sharedconfig.GetEnv("AUTH_MODE", "disabled"),
		AuthJWTSecret:      os.Getenv("AUTH_JWT_SECRET"),
		AuthAccessTTL:      sharedconfig.GetEnvDuration("AUTH_ACCESS_TTL", 15*time.Minute),
		AuthRefreshTTL:     sharedconfig.GetEnvDuration("AUTH_REFRESH_TTL", 30*24*time.Hour),
		AuthIssuer:         sharedconfig.GetEnv("AUTH_ISSUER", "kubetty"),
		AuthCookieDomain:   os.Getenv("AUTH_COOKIE_DOMAIN"),
		AuthCookieSecure:   sharedconfig.GetEnvBool("AUTH_COOKIE_SECURE", true),
	}
	cfg.DeploymentID = sharedconfig.GetEnv("DEPLOYMENT_ID", cfg.SessionID)
	cfg.KubettyUser = sharedconfig.GetEnv("KUBETTY_USER", os.Getenv("USER"))
	cfg.KubettyProject = sharedconfig.GetEnv("KUBETTY_PROJECT", cfg.DeploymentID)

	if cfg.SessionID == "" {
		return cfg, fmt.Errorf("SESSION_ID is required")
	}
	if cfg.CNPGHost == "" || cfg.CNPGDatabase == "" || cfg.CNPGUser == "" || cfg.CNPGPassword == "" {
		return cfg, fmt.Errorf("CNPG_* env vars are required")
	}
	if cfg.ProjectCatalogPath != "" {
		catalog, err := gatewayconfig.LoadCatalog(cfg.ProjectCatalogPath)
		if err != nil {
			return cfg, err
		}
		cfg.ProjectCatalog = catalog
	}

	// Validate auth configuration using shared validator
	authCfg := sharedconfig.AuthConfig{
		Mode:         cfg.AuthMode,
		JWTSecret:    cfg.AuthJWTSecret,
		AccessTTL:    cfg.AuthAccessTTL,
		RefreshTTL:   cfg.AuthRefreshTTL,
		CookieDomain: cfg.AuthCookieDomain,
		CookieSecure: cfg.AuthCookieSecure,
	}
	if err := sharedconfig.ValidateAuth(authCfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// ConnString builds the pgx connection string using the shared config builder.
//
// DEPRECATED: Use ConnConfig() instead for better security and type safety.
// This method is maintained for backward compatibility but uses string concatenation
// which could lead to injection vulnerabilities if parameters come from untrusted sources.
func (c Config) ConnString() string {
	return sharedconfig.BuildPostgresConnString(c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}

// ConnConfig creates a secure, injection-proof PostgreSQL connection configuration
// using the shared BuildPostgresConfig function. This is the recommended method
// for creating database connections.
//
// Returns an error if the configuration parameters are invalid (e.g., invalid port range).
func (c Config) ConnConfig() (*pgxpool.Config, error) {
	return sharedconfig.BuildPostgresConfig(c.CNPGHost, c.CNPGPort, c.CNPGDatabase, c.CNPGUser, c.CNPGPassword)
}

// GetAuthMode returns the authentication mode for AuthConfig interface.
func (c Config) GetAuthMode() string {
	return c.AuthMode
}

// GetAuthCookieDomain returns the cookie domain for AuthConfig interface.
func (c Config) GetAuthCookieDomain() string {
	return c.AuthCookieDomain
}

// GetAuthCookieSecure returns the cookie secure flag for AuthConfig interface.
func (c Config) GetAuthCookieSecure() bool {
	return c.AuthCookieSecure
}
