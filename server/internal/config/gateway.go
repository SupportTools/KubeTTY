package config

import (
	"fmt"
	"os"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
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
		TabIdleTimeout:     sharedconfig.GetEnvDuration("TAB_IDLE_TIMEOUT", 2*time.Hour),
		AuthMode:           sharedconfig.GetEnv("AUTH_MODE", "disabled"),
		AuthJWTSecret:      os.Getenv("AUTH_JWT_SECRET"),
		AuthAccessTTL:      sharedconfig.GetEnvDuration("AUTH_ACCESS_TTL", 15*time.Minute),
		AuthRefreshTTL:     sharedconfig.GetEnvDuration("AUTH_REFRESH_TTL", 30*24*time.Hour),
		AuthIssuer:         sharedconfig.GetEnv("AUTH_ISSUER", "kubetty"),
		AuthCookieDomain:   os.Getenv("AUTH_COOKIE_DOMAIN"),
		AuthCookieSecure:   sharedconfig.GetEnvBool("AUTH_COOKIE_SECURE", true),
	}

	// Load project catalog if path is provided
	if cfg.ProjectCatalogPath != "" {
		catalog, err := gatewayconfig.LoadCatalog(cfg.ProjectCatalogPath)
		if err != nil {
			return cfg, fmt.Errorf("load project catalog: %w", err)
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
