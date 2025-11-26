package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	sharedconfig "github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// ExecModeType defines how the gateway connects to project pods.
type ExecModeType string

const (
	// ExecModeWebSocket uses WebSocket relay to project pods (default, legacy)
	ExecModeWebSocket ExecModeType = "websocket"
	// ExecModeExec uses kubectl exec via Kubernetes remotecommand API
	ExecModeExec ExecModeType = "exec"
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
	// Leader election configuration for multi-replica deployments
	LeaderElection LeaderElectionConfig
	// Recommended image tag for project upgrades (default: "latest")
	RecommendedImageTag string
	// ExecMode controls how the gateway connects to project pods (default: "websocket")
	// - "websocket": relay WebSocket connections to project pods (legacy)
	// - "exec": use kubectl exec via Kubernetes remotecommand API (more stable)
	ExecMode ExecModeType
}

// LeaderElectionConfig holds configuration for leader election.
type LeaderElectionConfig struct {
	Enabled       bool          // Enable leader election (default: true when controller is enabled)
	LeaseName     string        // Name of the Lease resource (default: "kubetty-gateway-leader")
	LeaseDuration time.Duration // Duration that non-leaders wait before acquiring leadership (default: 15s)
	RenewDeadline time.Duration // Duration leader retries refreshing leadership (default: 10s)
	RetryPeriod   time.Duration // Duration between leadership acquire/renew attempts (default: 2s)
}

// ControllerConfig holds configuration for the single-namespace project controller.
type ControllerConfig struct {
	Enabled             bool          // Enable project controller (default: false)
	ProjectsNamespace   string        // Target namespace for projects (e.g., "kubetty-projects-dev")
	ResourcePrefix      string        // Prefix for all resources (default: "kubetty-project-")
	ReconcileInterval   time.Duration // Reconciliation interval (default: 30s)
	HealthCheckInterval time.Duration // Health check interval (default: 60s)
	EnvSecretName       string        // Name of secret containing project env vars (default: "env-secrets")
	ImagePullSecrets    []string      // List of image pull secret names (default: ["harbor-supporttools"])
	TemplatePVCName     string        // Name of template PVC for base file sync (optional, empty disables sync)

	// Storage monitoring configuration
	StorageMonitorEnabled  bool          // Enable automatic PVC expansion when storage is low (default: true)
	StorageMonitorInterval time.Duration // How often to check storage usage (default: 60s)
	StorageExpandThreshold float64       // Expand PVC when usage >= this fraction (default: 0.70)
	StorageExpandAmount    string        // Fixed amount to expand by (default: "10Gi")
	StorageExpandCooldown  time.Duration // Minimum time between expansions (default: 5m)
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
			Enabled:                sharedconfig.GetEnvBool("CONTROLLER_ENABLED", false),
			ProjectsNamespace:      os.Getenv("PROJECTS_NAMESPACE"),
			ResourcePrefix:         sharedconfig.GetEnv("RESOURCE_PREFIX", "kubetty-project-"),
			ReconcileInterval:      sharedconfig.GetEnvDuration("RECONCILE_INTERVAL", 30*time.Second),
			HealthCheckInterval:    sharedconfig.GetEnvDuration("HEALTH_CHECK_INTERVAL", 60*time.Second),
			EnvSecretName:          sharedconfig.GetEnv("ENV_SECRET_NAME", "env-secrets"),
			ImagePullSecrets:       parseImagePullSecrets(sharedconfig.GetEnv("IMAGE_PULL_SECRETS", "harbor-supporttools")),
			TemplatePVCName:        os.Getenv("TEMPLATE_PVC_NAME"),
			StorageMonitorEnabled:  sharedconfig.GetEnvBool("STORAGE_MONITOR_ENABLED", true),
			StorageMonitorInterval: sharedconfig.GetEnvDuration("STORAGE_MONITOR_INTERVAL", 60*time.Second),
			StorageExpandThreshold: sharedconfig.GetEnvFloat64("STORAGE_EXPAND_THRESHOLD", 0.70),
			StorageExpandAmount:    sharedconfig.GetEnv("STORAGE_EXPAND_AMOUNT", "10Gi"),
			StorageExpandCooldown:  sharedconfig.GetEnvDuration("STORAGE_EXPAND_COOLDOWN", 5*time.Minute),
		},
		LeaderElection: LeaderElectionConfig{
			Enabled:       sharedconfig.GetEnvBool("LEADER_ELECTION_ENABLED", true),
			LeaseName:     sharedconfig.GetEnv("LEADER_ELECTION_LEASE_NAME", "kubetty-gateway-leader"),
			LeaseDuration: sharedconfig.GetEnvDuration("LEADER_ELECTION_LEASE_DURATION", 15*time.Second),
			RenewDeadline: sharedconfig.GetEnvDuration("LEADER_ELECTION_RENEW_DEADLINE", 10*time.Second),
			RetryPeriod:   sharedconfig.GetEnvDuration("LEADER_ELECTION_RETRY_PERIOD", 2*time.Second),
		},
		RecommendedImageTag: sharedconfig.GetEnv("RECOMMENDED_IMAGE_TAG", "latest"),
		ExecMode:            parseExecMode(sharedconfig.GetEnv("KUBETTY_EXEC_MODE", "websocket")),
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

// parseImagePullSecrets parses a comma-separated list of image pull secret names.
func parseImagePullSecrets(s string) []string {
	if s == "" {
		return nil
	}
	secrets := strings.Split(s, ",")
	result := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		sec = strings.TrimSpace(sec)
		if sec != "" {
			result = append(result, sec)
		}
	}
	return result
}

// parseExecMode parses the exec mode string and returns the appropriate ExecModeType.
// Defaults to ExecModeWebSocket for unrecognized values.
func parseExecMode(s string) ExecModeType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "exec", "kubectl":
		return ExecModeExec
	case "websocket", "ws", "":
		return ExecModeWebSocket
	default:
		return ExecModeWebSocket
	}
}
