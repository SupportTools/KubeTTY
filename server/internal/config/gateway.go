package config

import (
	"fmt"
	"os"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
)

// GatewayConfig holds configuration specific to the gateway component.
type GatewayConfig struct {
	CommonConfig
	ProjectCatalogPath string
	ProjectCatalog     gatewayconfig.Catalog
	TabIdleTimeout     time.Duration // Idle timeout for gateway tabs (default: 2h)
	AuthMode           string
	AuthJWTSecret      string
	AuthAccessTTL      time.Duration
	AuthRefreshTTL     time.Duration
	AuthIssuer         string
	AuthCookieDomain   string
	AuthCookieSecure   bool
}

// LoadGatewayConfig reads environment variables and builds a GatewayConfig.
// This is used by the gateway binary which handles authentication and routes
// WebSocket connections to project pods.
func LoadGatewayConfig() (GatewayConfig, error) {
	common, err := loadCommonConfig()
	if err != nil {
		return GatewayConfig{}, err
	}

	cfg := GatewayConfig{
		CommonConfig:       common,
		ProjectCatalogPath: os.Getenv("PROJECT_CATALOG_PATH"),
		TabIdleTimeout:     getEnvDuration("TAB_IDLE_TIMEOUT", 2*time.Hour),
		AuthMode:           getEnv("AUTH_MODE", "disabled"),
		AuthJWTSecret:      os.Getenv("AUTH_JWT_SECRET"),
		AuthAccessTTL:      getEnvDuration("AUTH_ACCESS_TTL", 15*time.Minute),
		AuthRefreshTTL:     getEnvDuration("AUTH_REFRESH_TTL", 30*24*time.Hour),
		AuthIssuer:         getEnv("AUTH_ISSUER", "kubetty"),
		AuthCookieDomain:   os.Getenv("AUTH_COOKIE_DOMAIN"),
		AuthCookieSecure:   getEnvBool("AUTH_COOKIE_SECURE", true),
	}

	// Load project catalog if path is provided
	if cfg.ProjectCatalogPath != "" {
		catalog, err := gatewayconfig.LoadCatalog(cfg.ProjectCatalogPath)
		if err != nil {
			return cfg, fmt.Errorf("load project catalog: %w", err)
		}
		cfg.ProjectCatalog = catalog
	}

	// Validate auth configuration
	if err := validateAuthConfig(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// validateAuthConfig ensures auth settings are valid for the configured mode.
func validateAuthConfig(cfg GatewayConfig) error {
	switch cfg.AuthMode {
	case "", "disabled":
		return nil
	case "local":
		if cfg.AuthJWTSecret == "" {
			return fmt.Errorf("AUTH_JWT_SECRET is required when AUTH_MODE=local")
		}
		if cfg.AuthAccessTTL <= 0 {
			return fmt.Errorf("AUTH_ACCESS_TTL must be >0 when AUTH_MODE=local")
		}
		if cfg.AuthRefreshTTL <= 0 {
			return fmt.Errorf("AUTH_REFRESH_TTL must be >0 when AUTH_MODE=local")
		}
	default:
		return fmt.Errorf("unsupported AUTH_MODE %q", cfg.AuthMode)
	}
	return nil
}

// GetAuthMode returns the authentication mode for AuthConfig interface.
func (c GatewayConfig) GetAuthMode() string {
	return c.AuthMode
}

// GetAuthCookieDomain returns the cookie domain for AuthConfig interface.
func (c GatewayConfig) GetAuthCookieDomain() string {
	return c.AuthCookieDomain
}

// GetAuthCookieSecure returns the cookie secure flag for AuthConfig interface.
func (c GatewayConfig) GetAuthCookieSecure() bool {
	return c.AuthCookieSecure
}
