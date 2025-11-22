package config

import (
	"fmt"
	"os"
	"strings"
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
	// Metrics collection configuration
	MetricsEnabled  bool          // Enable tab resource metrics collection (default: true)
	MetricsInterval time.Duration // Metrics collection interval (default: 15s)
	// Controller configuration for single-namespace project management
	Controller ControllerConfig
}

// ControllerConfig holds configuration for the single-namespace project controller.
type ControllerConfig struct {
	Enabled             bool          // Enable project controller (default: false)
	ProjectsNamespace   string        // Target namespace for projects (e.g., "kubetty-projects-dev")
	ResourcePrefix      string        // Prefix for all resources (default: "kubetty-project-")
	ReconcileInterval   time.Duration // Reconciliation interval (default: 30s)
	HealthCheckInterval time.Duration // Health check interval (default: 60s)
}

// ParseEnvironment extracts the environment suffix from ProjectsNamespace.
// For example, "kubetty-projects-dev" returns "dev", "kubetty-projects-prd" returns "prd".
// Returns empty string if namespace is empty or has no suffix.
func (c ControllerConfig) ParseEnvironment() string {
	if c.ProjectsNamespace == "" {
		return ""
	}
	parts := strings.Split(c.ProjectsNamespace, "-")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// LoadGatewayConfig reads environment variables and builds a GatewayConfig.
// This is used by the gateway binary which handles authentication and routes
// WebSocket connections to project pods.
func LoadGatewayConfig() (GatewayConfig, error) {
	common, err := loadCommonConfig()
	if err != nil {
		return GatewayConfig{}, err
	}

	// Gateway requires database connection
	if common.CNPGHost == "" || common.CNPGDatabase == "" || common.CNPGUser == "" || common.CNPGPassword == "" {
		return GatewayConfig{}, fmt.Errorf("CNPG_* env vars are required for gateway mode")
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
		MetricsEnabled:     sharedconfig.GetEnvBool("METRICS_ENABLED", true),
		MetricsInterval:    sharedconfig.GetEnvDuration("METRICS_INTERVAL", 15*time.Second),
		Controller: ControllerConfig{
			Enabled:             sharedconfig.GetEnvBool("CONTROLLER_ENABLED", false),
			ProjectsNamespace:   os.Getenv("PROJECTS_NAMESPACE"),
			ResourcePrefix:      sharedconfig.GetEnv("RESOURCE_PREFIX", "kubetty-project-"),
			ReconcileInterval:   sharedconfig.GetEnvDuration("RECONCILE_INTERVAL", 30*time.Second),
			HealthCheckInterval: sharedconfig.GetEnvDuration("HEALTH_CHECK_INTERVAL", 60*time.Second),
		},
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

	// Validate controller configuration using shared validator
	controllerCfg := sharedconfig.ControllerConfig{
		Enabled:             cfg.Controller.Enabled,
		ProjectsNamespace:   cfg.Controller.ProjectsNamespace,
		ResourcePrefix:      cfg.Controller.ResourcePrefix,
		ReconcileInterval:   cfg.Controller.ReconcileInterval,
		HealthCheckInterval: cfg.Controller.HealthCheckInterval,
	}
	if err := sharedconfig.ValidateController(controllerCfg); err != nil {
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
